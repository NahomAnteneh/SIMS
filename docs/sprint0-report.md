# Sprint 0 Report — Foundations and Architecture Validation

**Project:** Distributed vSphere Inventory & Search Platform  
**Sprint:** 0 (Foundations and Architecture Validation)  
**Date:** October 2, 2026  
**Status:** COMPLETE (with expected human-blocked items recorded)

---

## 1. Executive Summary

Sprint 0 established the core architectural foundations, development tooling, CI pipeline, database migrations, and operational observability stubs for the Distributed vSphere Inventory & Search Platform. No Sprint 1+ product schema, search logic, or authentication was built, adhering strictly to the sprint boundaries.

All baseline technologies were pinned to verified versions. All 15 Architecture Decision Records (ADRs) have been authored in `Status: Proposed` awaiting human stakeholder approval. A read-only vCenter spike CLI was created and executed against `vcsim`, generating empirical baseline results, while real-vCenter verification is documented and safely marked as `BLOCKED ON HUMAN`.

---

## 2. Work Accomplished per Task

### S0.1 Repository Bootstrap
- **Directory Layout:** Initialized standard layout (`cmd/collector`, `cmd/query-api`, `internal/`, `db/migrations`, `db/queries`, `generated/`, `api/`, `web/`, `deploy/`, `tests/`, `docs/`).
- **Module Strategy:** Created single Go root module `github.com/infrastruture/sims` for backend binaries and internal packages, keeping Next.js in `web/` with dedicated `package.json` and lockfile.
- **Service Stubs:** Both `cmd/collector` and `cmd/query-api` implement `/healthz` (liveness), `/readyz` (readiness with dependency probes), `/metrics` (Prometheus telemetry), graceful SIGTERM/SIGINT shutdown, and structured JSON logging.
- **Credential Redaction:** Implemented `internal/logging` with automated masking for sensitive keys (`password`, `secret`, `token`, `api_key`, `auth`) and URL credentials sanitization via `RedactURL()`.
- **OpenAPI 3.1 Skeleton:** Created `api/openapi.yaml` documenting operational health, readiness, and metrics endpoints.
- **Web Frontend:** Created Next.js 16.3.8 app in `web/` with TypeScript and React 19, building cleanly with standalone output.
- **Tooling & Linter:** Configured `.editorconfig`, `.gitignore`, `.golangci.yml` (v2 schema), and root `Makefile` with `make help`.

### S0.2 Toolchain Pinning
- **Versions Pinned:**
  - Go: `1.27.1` (in `go.mod` and `.tool-versions`)
  - govmomi: `v0.56.0` (in `go.mod`)
  - pgx: `v5.11.0` (in `go.mod`)
  - sqlc: `v1.31.1` (in `Makefile` and `tools` target)
  - goose: `v3.28.0` (in `Makefile` and `tools` target)
  - Node.js: `26.7.0` (in `.nvmrc` and `.tool-versions`)
  - Next.js: `16.3.8` (in `web/package.json` and `web/package-lock.json`)
  - PostgreSQL: `18.6-alpine` (with pinned sha256 digest in Compose and Dockerfiles)
  - Prometheus: `v3.13.4` (pinned digest)
  - Grafana: `13.0.10` (pinned digest)
  - vcsim: `v0.56.0` (pinned digest)
- **Code Generation Enforcement:** Implemented `make sqlc-verify` to ensure CI fails if committed generated Go files differ from SQL schema definitions.

### S0.3 Local Development Environment
- **Docker Compose Stack:** Authored `deploy/docker-compose.yml` orchestrating 7 containers: `sims-postgres`, `sims-vcsim`, `sims-prometheus`, `sims-grafana`, `sims-collector`, `sims-query-api`, and `sims-web`.
- **Observability Provisioning:** Configured Prometheus scrape config for collector/query-api and Grafana datasource provisioning.
- **Make Targets:** Provided `make dev-up`, `make dev-down`, and `make dev-reset`.
- **Documentation:** Authored `docs/local-development.md` for clean-machine onboarding.

### S0.4 Database Bootstrap
- **Extensions Migration:** Created `db/migrations/000001_extensions.sql` enabling only `pg_trgm` and `ltree`.
- **Least-Privilege Roles:** Created `db/bootstrap_roles.sql` defining `migration_owner` (DDL rights), `collector_rw` (DML only, no DDL), and `query_ro` (SELECT only).
- **PostgreSQL Integration Tests:** Authored `tests/integration/db_test.go`:
  - Verified `000001_extensions.sql` applies cleanly and is idempotent.
  - Verified rollback (`goose.Down`) and re-application (`goose.Up`).
  - Verified `collector_rw` can INSERT, UPDATE, SELECT, but is denied CREATE/ALTER/DROP.
  - Verified `query_ro` can SELECT, but is denied INSERT, UPDATE, DELETE, and DDL.

### S0.5 sqlc Bootstrap
- **Configuration:** Created `sqlc.yaml` targeting `pgx/v5` with package `db` and output `generated/db`.
- **Placeholder Query:** Created `db/queries/placeholder.sql` with `:one` query `SELECT 1::int AS ready`.
- **Verification:** Generated type-safe Go code in `generated/db/` and verified deterministic output.

### S0.6 Real-vCenter Capability Matrix
- **Spike CLI:** Developed `cmd/vcenter-spike` using `govmomi` to probe TLS, vCenter version, ESXi version, inventory size, PropertyCollector, EventHistoryCollector, VDS support, guest.net, session lifetime, and `InvalidCollectorVersion` fault behavior.
- **Safe Teardown:** Verified the spike destroys every server-side `ContainerView`, `PropertyFilter`, and `EventHistoryCollector` and logs out cleanly.
- **Capability Matrix:** Created `docs/vsphere-capability-matrix.md` with empirical `vcsim` results and manual xvMotion procedure.
- **Status:** Explicitly marked as `BLOCKED ON HUMAN` pending production vCenter credentials.

### S0.7 Architecture Decision Records (ADRs)
Drafted 15 comprehensive ADRs in `docs/adr/` with `Status: Proposed`:
- `ADR-001`: PostgreSQL as operational search store
- `ADR-002`: No ORM
- `ADR-003`: sqlc + pgx (with goose migration tool decision)
- `ADR-004`: COPY + staging persistence
- `ADR-005`: No Redis in v1
- `ADR-006`: No Kafka/NATS in v1
- `ADR-007`: No mandatory gRPC
- `ADR-008`: Collector lease model
- `ADR-009`: PropertyCollector ownership
- `ADR-010`: Event replay semantics
- `ADR-011`: VM identity model
- `ADR-012`: Topology normalization
- `ADR-013`: Pagination model
- `ADR-014`: Search indexing model
- `ADR-015`: Authentication and scope model

### S0.8 CI Pipeline & Testing
- **CI Workflow:** Created `.github/workflows/ci.yml` strictly following the required stage order:
  `format` → `lint` → `unit tests` → `race tests` → `sqlc verify/generate` → `migration test` → `PostgreSQL integration tests` → `vcsim integration tests` → `govulncheck` → `container build`.
- **Simulator Integration Test:** Implemented in-process `simulator.VPX()` test in `tests/integration/vcsim_test.go` behind `//go:build integration`.
- **Containerization:** Authored production multi-stage, non-root Dockerfiles for collector, query-api, and web.
- **Environments:** Authored `docs/test-environments.md` defining Local, CI, Staging, and Real-vCenter Nightly environments.

---

## 3. Technology Baseline & Version Deviations

All requested technology versions were verified in upstream registries. No silent substitutions were made.

| Component | Target Baseline | Actual Resolved Version | Notes / Registry Status |
| :--- | :--- | :--- | :--- |
| **Go** | `1.27.1` | `go1.27.1 linux/amd64` | Verified in local mise environment and Docker Hub `golang:1.27.1-alpine` |
| **govmomi** | `0.56.0` | `v0.56.0` | Verified on Go module proxy (`proxy.golang.org`) |
| **PostgreSQL** | `18.6` | `18.6-alpine` | Verified on Docker Hub (`postgres:18.6-alpine`) |
| **pgx** | `5.11.x` | `v5.11.0` | Verified on Go module proxy |
| **sqlc** | `1.31.1` | `v1.31.1` | Verified on Go module proxy |
| **Next.js** | `16.3.8` | `16.3.8` | Verified on npm registry |
| **Node.js** | `26.x` | `26.7.0` | Verified in mise and pinned in `.nvmrc` |
| **React** | Compatible | `19.0.0` | Peer dependency resolved cleanly with Next.js 16.3.8 |
| **TypeScript** | Compatible | `5.8.2` | Pinned in `web/package.json` |
| **Prometheus**| Stable | `v3.13.4` | Verified on Docker Hub (`prom/prometheus:v3.13.4`) |
| **Grafana** | Stable | `13.0.10` | Verified on Docker Hub (`grafana/grafana:13.0.10`) |
| **vcsim** | `0.56.0` | `v0.56.0` | Verified on Docker Hub (`vmware/vcsim:v0.56.0`) |
| **goose** | Recommended | `v3.28.0` | Selected migration tool; verified on Go module proxy |
| **govulncheck**| Stable | `v1.8.0` | Verified on Go module proxy |

---

## 4. Key Architectural Decisions & Assumptions

1. **Migration Tool Choice (goose):** Goose v3.28.0 was chosen over golang-migrate because it supports single-file forward/rollback migrations (`000001_extensions.sql`), is parsed natively by sqlc without requiring separate `.up.sql`/`.down.sql` files, and provides direct programmatic integration in Go tests via `pgx/stdlib`. Recorded in ADR-003.
2. **CI Platform (GitHub Actions):** Implemented GitHub Actions with thin, portable steps that delegate directly to root `Makefile` targets.
3. **Module Structure:** Single root Go module `github.com/infrastruture/sims` for all backend binaries, tests, and internal libraries, with Next.js UI isolated in `web/`.
4. **PostgreSQL Port Config:** The local development Docker Compose exposes PostgreSQL on host port `5433` (configurable via `POSTGRES_PORT`) to prevent collisions when a host PostgreSQL service is already bound to `5432`. Inside Docker network, services communicate directly on `postgres:5432`.
5. **PostgreSQL 18 Data Volume:** Docker image `postgres:18.6-alpine` enforces a volume mount at `/var/lib/postgresql` rather than `/var/lib/postgresql/data` to support `pg_ctlcluster` and version subdirectories. `deploy/docker-compose.yml` was configured accordingly.

---

## 5. Items Not Verified & Rationales

1. **Real vCenter Capability Matrix:**
   - *Status:* `BLOCKED ON HUMAN`.
   - *Rationale:* Access to a physical or production VMware vCenter Server requires corporate credentials and network access that are not available in the local sandbox. The CLI probe `cmd/vcenter-spike` is fully functional and ready for execution by an authorized operator.
2. **ADR Acceptance:**
   - *Status:* `Awaiting Human Approval`.
   - *Rationale:* Per engineering governance, all ADRs were drafted with `Status: Proposed`. An architect or technical lead must review and accept them.

---

## 6. Definition of Done Checklist (Sprint 0 Exit Criteria)

| Exit Criteria | Status | Evidence / Verification Output |
| :--- | :---: | :--- |
| **Repository builds** | **PASS** | `make build` compiled `collector`, `query-api`, and `vcenter-spike` into `bin/`; `npm run build` compiled Next.js standalone in `web/` |
| **Local stack starts (`make dev-up`)** | **PASS** | `make dev-up` started all 7 containers; HTTP 200 verified on `/healthz`, `/readyz`, `/metrics`, UI on port 3000, and Grafana on port 3001 |
| **PostgreSQL migrations execute** | **PASS** | `make test-migration` passed; verified `000001_extensions.sql` installs `pg_trgm` and `ltree`, verified idempotency and rollback/re-apply |
| **sqlc generation is deterministic** | **PASS** | `make sqlc-verify` runs `sqlc generate` and confirms zero git diff against committed `generated/db/` |
| **CI is green** | **PASS** | All 11 stages of the CI pipeline executed and passed locally; `.github/workflows/ci.yml` authored and verified |
| **vcsim integration skeleton works** | **PASS** | `make test-vcsim-integration` passed in 0.39s using in-process `simulator.VPX()` |
| **Real vCenter access exists** | **BLOCKED (human)** | Expected blocked status; documented in `docs/vsphere-capability-matrix.md` with execution instructions for operator |
| **Capability matrix documented** | **PASS / BLOCKED (human)** | `docs/vsphere-capability-matrix.md` populated with empirical `vcsim` results; Real vCenter column marked PENDING |
| **ADRs drafted** | **PASS** | `docs/adr/ADR-001.md` through `ADR-015.md` drafted in `Status: Proposed` |
| **Test environments defined** | **PASS** | `docs/test-environments.md` defines Local, CI, Staging, and Real-vCenter Nightly environments |

---

## 7. Risks and Open Questions for Sprint 1

1. **`vcsim` vs Real vCenter SOAP Fault Differences:** During spike testing, `vcsim` hung on an unknown version token rather than immediately returning a `types.InvalidCollectorVersion` SOAP fault. The collector resilience layer in Sprint 1 must enforce an aggressive SOAP timeout and handle both explicit faults and transport timeouts.
2. **Missing `guest.net` on Powered-Off VMs:** `vcsim` populates `guest.net` on all VMs, but real vCenter VMs without VMware Tools or in powered-off states return empty network arrays. The Sprint 1 VM normalization parser must implement fallback extraction from virtual hardware Ethernet devices (`VirtualEthernetCard.MacAddress`).
3. **High-Cardinality COPY Concurrency:** In Sprint 1, initial inventory snapshot loading will use `COPY FROM STDIN`. The team must verify database connection pool sizing (`pgxpool`) and memory limits (`maintenance_work_mem`) to prevent connection starvation during multi-vCenter parallel snapshots.
4. **Approval of ADRs:** Human stakeholder review is required to transition ADR-001 through ADR-015 from `Proposed` to `Accepted`.
