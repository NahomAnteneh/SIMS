SHELL := /bin/bash
BIN_DIR := $(CURDIR)/bin
PATH := $(BIN_DIR):$(PATH)

GO ?= go
GOLANGCI_LINT := $(BIN_DIR)/golangci-lint
SQLC := $(BIN_DIR)/sqlc
GOOSE := $(BIN_DIR)/goose
GOVULNCHECK := $(BIN_DIR)/govulncheck

.PHONY: all help format lint test test-unit test-race build clean
.PHONY: sqlc-generate sqlc-verify test-migration test-db-integration test-vcsim-integration
.PHONY: test-gate-s4 test-scale-s4 demo-s4
.PHONY: test-perf-query test-perf-ingest
.PHONY: vulncheck container-build package-release dev-up dev-down dev-reset tools

all: help

help: ## Display this help screen with target descriptions
	@echo "Distributed vSphere Inventory & Search Platform (SIMS)"
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-24s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

tools: ## Ensure required pinned CLI tools are installed in bin/
	@mkdir -p $(BIN_DIR)
	@which $(SQLC) >/dev/null 2>&1 || GOBIN=$(BIN_DIR) $(GO) install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
	@which $(GOOSE) >/dev/null 2>&1 || GOBIN=$(BIN_DIR) $(GO) install github.com/pressly/goose/v3/cmd/goose@v3.28.0
	@which $(GOLANGCI_LINT) >/dev/null 2>&1 || GOBIN=$(BIN_DIR) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
	@which $(GOVULNCHECK) >/dev/null 2>&1 || GOBIN=$(BIN_DIR) $(GO) install golang.org/x/vuln/cmd/govulncheck@v1.8.0

format: ## Format all Go source files and check formatting
	$(GO) fmt ./...

lint: tools ## Run golangci-lint on backend code
	$(GOLANGCI_LINT) run ./...

test: test-unit ## Alias for test-unit

test-unit: ## Run unit tests with standard timeout
	$(GO) test -v ./internal/... ./cmd/...

test-race: ## Run unit tests with data race detector enabled
	$(GO) test -v -race ./internal/... ./cmd/...

build: ## Build collector and query-api binaries into bin/
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/collector ./cmd/collector
	$(GO) build -o $(BIN_DIR)/query-api ./cmd/query-api
	$(GO) build -o $(BIN_DIR)/vcenter-spike ./cmd/vcenter-spike
	$(GO) build -o $(BIN_DIR)/dataset-gen ./cmd/dataset-gen
	$(GO) build -o $(BIN_DIR)/vcsim ./cmd/vcsim

sqlc-generate: tools ## Generate type-safe Go database code with sqlc
	$(SQLC) generate

sqlc-verify: sqlc-generate ## Ensure committed sqlc generated output matches source definitions
	@git diff --exit-code generated/ || (echo "ERROR: generated/db differs from committed files. Run 'make sqlc-generate' and commit." && exit 1)

test-migration: tools ## Run database migration idempotency verification
	$(GO) test -v -tags=integration ./tests/integration -run TestMigrations

test-db-integration: tools ## Run PostgreSQL integration tests (requires Postgres)
	$(GO) test -v -tags=integration ./tests/integration -run TestRolePrivileges

test-vcsim-integration: ## Run vcsim simulator integration tests
	$(GO) test -v -tags=integration ./tests/integration -run TestVCSim

test-integration: tools ## Run all integration tests (database, identity, topology, network, API)
	$(GO) test -v -tags=integration ./tests/integration -run 'Test(Migrations|RolePrivileges|Identity|Topology|Network|VLAN|BulkPersistence|API|OpenAPI|VCSim|LocalAuth|UserManagement|Security|Telemetry)'

test-gate: tools ## Run Sprint 1 Test Gate (5,000-VM vcsim hydration & 50,000 synthetic DB load)
	$(GO) test -v -tags=integration -timeout 10m ./tests/integration -run TestGate

demo-s1: tools ## Run Sprint 1 end-to-end demo (starts stack, hydrates 5,000 VMs, queries API)
	./scripts/demo-s1.sh

demo-s2: tools ## Run Sprint 2 end-to-end demo (real-time ingestion, events, metadata sync, query API)
	./scripts/demo-s2.sh

demo-s3: tools ## Run Sprint 3 end-to-end demo (search, AST filter, keyset pagination, scope, vcenter mgmt)
	./scripts/demo-s3.sh

test-gate-s3: tools ## Run Sprint 3 Test Gate (AST fuzzer, cursor tampering, SQL injection, scope bypass, SSRF, leak scan)
	$(GO) test -v -tags=integration ./tests/integration -run TestGateS3

demo-s4: tools ## Run Sprint 4 end-to-end demo (leases, takeover, xvMotion, reconciliation, scale)
	./scripts/demo-s4.sh

test-gate-s4: tools ## Run Sprint 4 Test Gate (worker takeover, lease race, xvMotion, differential reconciliation, tags, purger)
	$(GO) test -v -tags=integration ./tests/integration -run TestGateS4

test-gate-s5: tools ## Run Sprint 5 Release Candidate Test Gate (v1.0.0 criteria)
	$(GO) test -v -tags=integration ./tests/integration -run TestGateS5

demo-s5: tools ## Run Sprint 5 End-to-End Demo Script
	./scripts/demo-s5.sh

test-scale-s4: tools ## Run Sprint 4 Scale Suite (3, 5, 10 vCenters multi-target scale simulation)
	$(GO) test -v -tags=integration ./tests/integration -run TestScaleS4

test-perf-query: tools ## Run Sprint 5 Query Performance Gate (P99 <= 5ms target)
	$(GO) test -v -tags=integration ./tests/integration -run TestQueryPerformanceGate

test-perf-ingest: tools ## Run Sprint 5 Ingestion Performance Gate (batch write P95 <= 15ms, e2e P95 <= 250ms)
	$(GO) test -v -tags=integration ./tests/integration -run TestIngestionPerformanceGate

test-chaos: tools ## Run Sprint 5 Chaos Resilience Suite (9 fault injection scenarios)
	$(GO) test -v -tags=integration ./tests/integration -run TestChaos_

test-soak: tools ## Run Sprint 5 Soak Test Harness (continuous ingestion, query, leak detection)
	$(GO) test -v -tags=integration ./tests/soak -run TestSoak_ContinuousWorkload

test-churn: tools ## Run Sprint 2 real-time pipeline churn test (configure CHURN_DURATION env)
	$(GO) test -v -tags=integration ./tests/integration -run TestChurn_RealtimePipeline

vulncheck: tools ## Scan Go dependencies for known vulnerabilities
	$(GOVULNCHECK) ./...

container-build: ## Build production multi-stage container images for all services
	docker build -f deploy/Dockerfile.collector -t sims-collector:1.0.0 .
	docker build -f deploy/Dockerfile.query-api -t sims-query-api:1.0.0 .
	docker build -f deploy/Dockerfile.migration -t sims-migration:1.0.0 .
	docker build -f deploy/Dockerfile.vcsim -t sims-vcsim:1.0.0 .
	docker build -f deploy/Dockerfile.web -t sims-web:1.0.0 ./web

package-release: tools ## Package release tarball with binaries, Next.js standalone, and migrations
	@VERSION=$${VERSION:-1.0.0}; ./scripts/package-release.sh "$$VERSION"


dev-up: ## Start the full local development stack with Docker Compose
	docker compose -f deploy/docker-compose.yml up -d --build

dev-down: ## Stop all local development stack containers
	docker compose -f deploy/docker-compose.yml down

dev-reset: ## Stop local stack and purge persistent database and telemetry volumes
	docker compose -f deploy/docker-compose.yml down -v --remove-orphans

clean: ## Remove build artifacts and temporary files
	rm -rf bin/collector bin/query-api bin/vcenter-spike bin/dataset-gen bin/vcsim dist/ build/ coverage.txt
