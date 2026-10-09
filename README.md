# SIMS: Scalable Inventory & Management Service

[![Go Version](https://img.shields.io/badge/Go-1.27.1-00ADD8?style=flat&logo=go)](https://go.dev/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-18.6-336791?style=flat&logo=postgresql)](https://www.postgresql.org/)
[![vSphere Support](https://img.shields.io/badge/vSphere-7.0%20%2B%20(No%20Ceiling)-0095D5?style=flat&logo=vmware)](docs/supported-versions.md)
[![OpenAPI](https://img.shields.io/badge/OpenAPI-3.1-6BA539?style=flat&logo=openapiinitiative)](api/openapi.yaml)
[![Next.js](https://img.shields.io/badge/Next.js-16.3-black?style=flat&logo=next.js)](web/)
[![Docker](https://img.shields.io/badge/Docker-29.x%2B-2496ED?style=flat&logo=docker)](deploy/)
[![License](https://img.shields.io/badge/License-Proprietary%20%2F%20Internal-lightgrey.svg)](#license)

**SIMS (Scalable Inventory & Management Service)** is an enterprise-grade, high-throughput, distributed VMware vSphere inventory, real-time ingestion, and search platform. Engineered to scale effortlessly from compact clusters to **1,000,000+ virtual machines across dozens of vCenter Server instances**, SIMS indexes virtual infrastructure into a normalized, highly queryable PostgreSQL store without overloading vCenter SOAP endpoints.

---

## Table of Contents

- [Overview & Key Capabilities](#overview--key-capabilities)
- [System Architecture](#system-architecture)
- [Repository Layout](#repository-layout)
- [Technology Stack](#technology-stack)
- [Quickstart & Local Development](#quickstart--local-development)
  - [Prerequisites](#prerequisites)
  - [Starting the Local Stack](#starting-the-local-stack)
  - [Service Ports & Default Credentials](#service-ports--default-credentials)
- [Query API & Search Engine](#query-api--search-engine)
  - [Authentication](#authentication)
  - [Keyset Pagination](#keyset-pagination)
  - [AST Filter & Search Capabilities](#ast-filter--search-capabilities)
  - [API Endpoints Summary](#api-endpoints-summary)
- [Configuration Reference](#configuration-reference)
- [Development, Testing & CI Workflow](#development-testing--ci-workflow)
  - [Essential Make Commands](#essential-make-commands)
  - [Performance & Chaos Gates](#performance--chaos-gates)
- [Core Invariants & Architectural Principles](#core-invariants--architectural-principles)
- [vSphere Version Support Policy](#vsphere-version-support-policy)
- [Production Deployment & Sizing](#production-deployment--sizing)
- [Documentation Index](#documentation-index)
- [License](#license)

---

## Overview & Key Capabilities

Large-scale enterprise virtualization environments frequently face API throttling, slow query performance, and lack of cross-vCenter inventory visibility. Querying vCenter Server directly via SOAP for search or reporting introduces latency and degrades hypervisor control-plane responsiveness.

SIMS solves this through a dedicated, asynchronous ingestion and indexing pipeline:

- **Distributed Collector Fleet with Lease Coordination**:
  - PostgreSQL-backed target worker leases with monotonic fencing tokens (`version`) prevent split-brain conditions and stale worker commits.
  - Automatic standby takeover if a primary worker fails to heartbeat within `LeaseTTL` (30s).
- **Single-Goroutine PropertyCollector Streaming**:
  - Enforces the strict VMware vSphere invariant: exactly one background goroutine owns `WaitForUpdatesEx` per vCenter session.
  - Proactive timeout guards (`CallDeadline: 90s`) terminate hung SOAP sockets during network partitions without freezing workers.
- **Independent Event Journaling & Deduplication**:
  - Continuous `EventHistoryCollector` streaming with deterministic SHA-256 event keys and windowed overlap replay for zero-loss ingestion.
  - Immutable event ownership: `created_at` and `created_by` identity timestamps are preserved across updates and reconciliations.
- **High-Throughput Bulk Ingestion**:
  - In-memory update compaction and deduplication.
  - Direct PostgreSQL ingestion via binary `COPY FROM` into staging tables, followed by atomic set-based `INSERT ... ON CONFLICT DO UPDATE`.
  - Non-destructive field masking (`nil` preserves existing children; explicit empty slices clear collections).
- **Cross-vCenter vMotion (xvMotion) & Tombstone Purging**:
  - Automatic tracking of virtual machines migrating across vCenter boundaries using global `instance_uuid` correlation and tombstone purgers.
- **Differential Reconciliation Engine**:
  - Periodic background integrity sweeps detect and reconcile discrepancies without disrupting real-time change streams.
- **High-Performance PostgreSQL 18 Engine**:
  - Normalized topology models: Datacenters, Folders, Clusters, Hosts, Datastores, DVS switches, Portgroups, Networks, VMs, NICs, Tags, and Custom Attributes.
  - Hierarchical folder trees powered by the PostgreSQL `ltree` extension with GiST indexing.
  - Fast substring and fuzzy name searching using GIN trigram indexes (`pg_trgm`).
  - Network indexing with PostgreSQL multirange (`int4multirange`) for VLAN allocations and GiST inet containment for IP addresses.
  - Keyset pagination `(name ASC, id ASC)` eliminating offset drift at 1,000,000+ VM scale.
- **Strict Query API & Security**:
  - Read-only least-privilege database role (`query_ro`) prevents accidental or malicious data mutations.
  - Abstract Syntax Tree (AST) query compiler & sanitizer.
  - Local JWT authentication and enterprise OIDC Single Sign-On (Active Directory, Okta, Keycloak) with Role-Based Access Control (RBAC).
  - Strict Server-Side Request Forgery (SSRF) policy validation on all registered vCenter endpoints.
  - Zero-leak credential policy across logs, error messages, and OpenTelemetry distributed traces.
- **Interactive Web Dashboard**:
  - Next.js 16 (React 19) management dashboard with real-time inventory search, filtering, and interactive topology graphs (React Flow / xyflow).

---

## System Architecture

```
                          +------------------------+        +------------------------+
                          |   vCenter Server A     |        |    vCenter Server B    |
                          | (vSphere 7.0 / 8.0 / 9)|        | (vSphere 7.0 / 8.0 / 9)|
                          +-----------+------------+        +-----------+------------+
                                      | SOAP / REST                     | SOAP / REST
                                      v                                 v
+----------------------------------------------------------------------------------------------------+
|                                    SIMS Collector Fleet Workers                                    |
|  - Distributed Lease Coordinator (PostgreSQL transaction-fenced leases & monotonic tokens)         |
|  - Dedicated PropertyCollector Streamer (Single goroutine per target, 90s CallDeadline guard)      |
|  - EventHistoryCollector Streamer (Sliding window overlap, SHA-256 deduplication)                 |
|  - vSphere Tags & Custom Attributes Sync (REST / SOAP non-destructive preservation)                |
|  - Differential Reconciliation Engine & xvMotion Tombstone Purger                                  |
|  - Bulk Ingestion Pipeline (In-memory compaction -> Binary COPY -> Set-based Upsert Merge)         |
+--------------------------------------------------+-------------------------------------------------+
                                                   |
                                                   v
+----------------------------------------------------------------------------------------------------+
|                                      PostgreSQL 18.6 Database                                      |
|  - Normalized Topology & VM Inventory (Datacenters, Folders, Clusters, Hosts, Datastores, Networks)|
|  - Advanced Spatial & Text Indexes (ltree GiST, pg_trgm GIN, int4multirange GiST, inet GiST)       |
|  - Keyset Pagination Ordering (name ASC, id ASC)                                                   |
|  - 4 Isolated Connection Pool Budgets (query_ro, collector_rw, reconciliation, migration_owner)    |
+--------------------------------------------------+-------------------------------------------------+
                                                   |
                                                   v
+----------------------------------------------------------------------------------------------------+
|                                      SIMS Query API Service                                        |
|  - Strict Read-Only Database Isolation (query_ro role)                                             |
|  - OpenAPI 3.1 REST Endpoints with Keyset Pagination                                               |
|  - AST Query Compiler & Sanitizer (Fuzzy search, folder path containment, CIDR matching)           |
|  - Local JWT & OIDC SSO Authentication with RBAC                                                   |
|  - OpenTelemetry Distributed Tracing (W3C context propagation, zero-leak credential scrubbing)     |
+--------------------------------------------------+-------------------------------------------------+
                          |                                                  |
                          v                                                  v
              +-----------------------+                          +-----------------------+
              |   Next.js 16 Web UI   |                          | External Integrations |
              |  - Inventory Explorer |                          |  - Automation Tools   |
              |  - Topology Visualizer|                          |  - CMDB Synchronizers |
              |  - vCenter Management |                          |  - Reporting Scripts  |
              +-----------------------+                          +-----------------------+
```

---

## Repository Layout

```
.
├── AGENTS.md                  # Development guidelines, boundaries, and architectural invariants
├── Makefile                   # Build, test, lint, format, and orchestration targets
├── SPRINTPLAN.md              # Complete sprint delivery plan and implementation roadmap
├── api/
│   └── openapi.yaml           # Strict OpenAPI 3.1 specification for Query API
├── cmd/
│   ├── collector/             # Collector fleet worker daemon entrypoint
│   ├── dataset-gen/           # Synthetic high-scale inventory generator (up to 1M VMs)
│   ├── query-api/             # Query API HTTP service entrypoint
│   ├── vcenter-spike/         # vCenter connectivity and capability verification tool
│   └── vcsim/                 # Configurable VMware vCenter simulator for testing
├── db/
│   ├── bootstrap_roles.sql    # Least-privilege PostgreSQL roles & permission provisioning
│   ├── migrations/            # Version-controlled idempotent SQL migrations (Goose)
│   └── queries/               # Strict SQL query definitions compiled by sqlc
├── deploy/
│   ├── Dockerfile.collector   # Multi-stage production container for collector
│   ├── Dockerfile.migration   # Init container for schema migrations
│   ├── Dockerfile.query-api   # Multi-stage production container for Query API
│   ├── Dockerfile.vcsim       # Container image for VMware simulator
│   ├── Dockerfile.web         # Multi-stage production container for Next.js frontend
│   ├── docker-compose.yml     # Complete local development and testing stack
│   ├── grafana/               # Pre-configured Grafana datasources and dashboards
│   └── prometheus/            # Prometheus scrape configuration
├── docs/                      # Comprehensive technical documentation & runbooks
│   ├── adr/                   # Architecture Decision Records (ADR-001 through ADR-018)
│   ├── architecture.md        # Deep-dive architecture and design specification
│   ├── api.md                 # Complete Query API specification and usage guide
│   ├── database.md            # Schema documentation, indices, and query access patterns
│   ├── deployment.md          # Production sizing, Kubernetes specs, and container guidelines
│   ├── ha-runbook.md          # High availability, failover, and lease coordination runbook
│   ├── local-development.md   # Step-by-step local developer setup guide
│   ├── supported-versions.md  # Official VMware vSphere version compatibility policy
│   └── troubleshooting.md     # Production anomaly triage and remediation guide
├── internal/                  # Core Go packages (collector, API, auth, db, search, etc.)
├── scripts/                   # Automated demo scripts, seeders, and migration helpers
├── tests/                     # Integration, chaos, soak, scale, and performance test suites
└── web/                       # Next.js 16 frontend application (React 19, Tailwind CSS 4)
```

---

## Technology Stack

| Layer | Component | Version | Rationale & Key Attributes |
| :--- | :--- | :--- | :--- |
| **Backend Core** | Go | `1.27.1` | High-performance, concurrent, low-latency microservices |
| **Database** | PostgreSQL | `18.6` | Operational store with extensions `ltree`, `pg_trgm`, `btree_gist` |
| **Database Access** | sqlc + pgx/v5 | `sqlc v1.31` | Type-safe SQL without ORM overhead; high-performance binary protocol |
| **Migrations** | Goose | `v3.28.0` | Sequential, forward-compatible, idempotent database migrations |
| **vSphere Client** | govmomi | `v0.56.0` | Official VMware vSphere SDK for Go (SOAP and REST Automation API) |
| **Frontend** | Next.js / React | `16.3 / 19.0` | React Server Components, Tailwind CSS 4, Radix UI, xyflow / React Flow |
| **Specification** | OpenAPI | `3.1.0` | Strict API contract for client generation and validation |
| **Observability** | OpenTelemetry / Prometheus | `v3.13` | Distributed W3C tracing, custom collector metrics, Grafana dashboards |
| **Containers** | Docker / Alpine | `Alpine 3.21` | Multi-stage minimal containers, non-root execution (`UID 10001`) |

---

## Quickstart & Local Development

### Prerequisites

Ensure your workstation has the following tools installed:

- **Go**: `1.27.1` (`go version`)
- **Docker & Docker Compose**: `Docker 29+` / `Docker Compose 5+`
- **Node.js & npm** (optional, for local web development): `Node 26.7+` / `npm 11+`

SIMS includes `.tool-versions` and `.nvmrc` for seamless management via `asdf`, `mise`, or `nvm`.

### Starting the Local Stack

1. **Clone the repository:**
   ```bash
   git clone git@github.com:NahomAnteneh/SIMS.git
   cd SIMS
   ```

2. **Install pinned CLI tools into `./bin`:**
   ```bash
   make tools
   ```
   *Installs `sqlc`, `goose`, `golangci-lint`, and `govulncheck` into `./bin` without polluting global GOPATH.*

3. **Start the complete development stack via Docker Compose:**
   ```bash
   make dev-up
   ```
   This automatically provisions:
   - PostgreSQL 18.6 with initialized roles and applied migrations
   - VMware vCenter Simulator (`vcsim`) pre-configured with vSphere 7.0+ inventory
   - SIMS Collector worker connected to `vcsim`
   - SIMS Query API connected to PostgreSQL
   - Next.js Web UI
   - Prometheus and Grafana for metrics and dashboards

4. **Verify running services:**
   ```bash
   docker compose -f deploy/docker-compose.yml ps
   ```

5. **Stop or reset the environment:**
   ```bash
   make dev-down     # Stops containers while preserving database volume
   make dev-reset    # Tears down containers and purges all volumes
   ```

---

### Service Ports & Default Credentials

| Service | Host Port | Protocol | Path / URL | Default Credentials |
| :--- | :--- | :--- | :--- | :--- |
| **Web Dashboard** | `3000` | HTTP | `http://localhost:3000` | None |
| **Query API** | `8080` | HTTP | `http://localhost:8080` | `admin` / `admin123` (Bearer JWT / Basic) |
| **Collector Metrics** | `8081` | HTTP | `http://localhost:8081/metrics` | None (internal metrics) |
| **PostgreSQL 18.6** | `5433` | TCP | `localhost:5433/sims` | `postgres` / `postgres` |
| **vCenter Simulator (vcsim)** | `8989` | HTTPS | `https://localhost:8989/sdk` | `user` / `pass` |
| **Prometheus** | `9090` | HTTP | `http://localhost:9090` | None |
| **Grafana** | `3001` | HTTP | `http://localhost:3001` | `admin` / `admin` |

#### Database Roles & Least Privileges
PostgreSQL is configured with 3 isolated application accounts:
- `migration_owner` (`migration_owner_pass`): Schema owner with DDL permissions.
- `collector_rw` (`collector_rw_pass`): Ingestion worker with DML (`SELECT`, `INSERT`, `UPDATE`, `DELETE`) only.
- `query_ro` (`query_ro_pass`): Query API service with strict read-only (`SELECT`) access.

---

## Query API & Search Engine

### Authentication

When `AUTH_ENABLED=true` (default), all API endpoints require authentication:

```bash
# 1. Obtain JWT Bearer Token
curl -s -X POST http://localhost:8080/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "admin123"}' | jq -r .token
```

Include the returned token in subsequent requests:
```bash
export TOKEN="<jwt-token-here>"
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8080/v1/vms | jq .
```

---

### Keyset Pagination

SIMS uses cursor-based keyset pagination ordered by `(name ASC, id ASC)` to guarantee consistent results without page drift at any scale:

```bash
# Request first page of 20 VMs
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/v1/vms?limit=20" | jq .

# Request next page using next_cursor
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/v1/vms?limit=20&cursor=ZXhhbXBsZS1jdXJzb3I=" | jq .
```

---

### AST Filter & Search Capabilities

The Query API features an Abstract Syntax Tree (AST) query compiler supporting rich, index-backed search expressions:

- **Fuzzy & Substring Search (`pg_trgm` GIN):**
  ```bash
  curl -s -H "Authorization: Bearer $TOKEN" \
    "http://localhost:8080/v1/vms?name=prod-db" | jq .
  ```
- **Hierarchical Folder Containment (`ltree` GiST):**
  ```bash
  curl -s -H "Authorization: Bearer $TOKEN" \
    "http://localhost:8080/v1/vms?folder_path=Datacenter.vm.Production" | jq .
  ```
- **IP & CIDR Subnet Containment (`inet` GiST):**
  ```bash
  curl -s -H "Authorization: Bearer $TOKEN" \
    "http://localhost:8080/v1/vms?ip=10.20.0.0/16" | jq .
  ```
- **VLAN Multirange Overlap (`int4multirange` GiST):**
  ```bash
  curl -s -H "Authorization: Bearer $TOKEN" \
    "http://localhost:8080/v1/networks?vlan=100" | jq .
  ```
- **Tag & Custom Attribute Lookups:**
  ```bash
  curl -s -H "Authorization: Bearer $TOKEN" \
    "http://localhost:8080/v1/vms?tag=Environment:Production" | jq .
  ```

---

### API Endpoints Summary

| Endpoint | Method | Role Required | Description |
| :--- | :--- | :--- | :--- |
| `/healthz` | `GET` | Public | Liveness probe (HTTP 200) |
| `/readyz` | `GET` | Public | Readiness probe (verifies database pool health) |
| `/metrics` | `GET` | Public | Prometheus telemetry metrics |
| `/v1/auth/login` | `POST` | Public | Authenticate and obtain JWT token |
| `/v1/vms` | `GET` | Viewer+ | Keyset paginated VM inventory with filters |
| `/v1/vms/{id}` | `GET` | Viewer+ | Detailed VM record including NICs, disks, tags |
| `/v1/vcenters` | `GET` | Viewer+ | List registered vCenter Server instances |
| `/v1/vcenters` | `POST` | Admin | Register new vCenter Server (validates connectivity & SSRF) |
| `/v1/vcenters/{id}` | `DELETE` | Admin | Unregister vCenter and mark target for removal |
| `/v1/topology/clusters` | `GET` | Viewer+ | List compute clusters across vCenters |
| `/v1/topology/hosts` | `GET` | Viewer+ | List ESXi host systems |
| `/v1/topology/datastores` | `GET` | Viewer+ | List datastores (VMFS, NFS, vSAN) |
| `/v1/networks` | `GET` | Viewer+ | List portgroups, DVS, and VLAN allocations |
| `/v1/events` | `GET` | Viewer+ | Query historical vCenter events with deduplication |
| `/v1/users` | `GET`, `POST` | Admin | User management and RBAC provisioning |

*For complete schema specifications and request bodies, see [`api/openapi.yaml`](api/openapi.yaml) or [`docs/api.md`](docs/api.md).*

---

## Configuration Reference

### Collector Service (`sims-collector`)

| Variable | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `PORT` | `int` | `8081` | HTTP metrics and readiness server port |
| `LOG_LEVEL` | `string` | `info` | Log severity (`debug`, `info`, `warn`, `error`) |
| `DATABASE_URL` | `string` | **Required** | PostgreSQL connection URL using `collector_rw` role |
| `VCENTER_URL` | `string` | Optional | Bootstrap vCenter URL (if not dynamically registered) |
| `VCENTER_USER` | `string` | Optional | Bootstrap vCenter username |
| `VCENTER_PASSWORD` | `string` | Optional | Bootstrap vCenter password |
| `VCENTER_INSECURE` | `bool` | `false` | Disable TLS certificate verification (development only) |
| `SIMS_CREDENTIAL_KEY` | `string` | **Required** | AES-256 key for encrypted vCenter credential storage |

### Query API Service (`sims-query-api`)

| Variable | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `PORT` | `int` | `8080` | HTTP API listen port |
| `LOG_LEVEL` | `string` | `info` | Log severity |
| `DATABASE_URL` | `string` | **Required** | PostgreSQL connection URL using `query_ro` role |
| `AUTH_ENABLED` | `bool` | `true` | Enforce authentication (JWT / OIDC) |
| `JWT_SECRET` | `string` | Optional | Secret key for signing local JWT tokens |
| `OIDC_ISSUER_URL` | `string` | Optional | Upstream OIDC provider URL (Active Directory, Okta) |
| `OIDC_AUDIENCE` | `string` | Optional | Expected JWT audience claim |
| `CORS_ALLOWED_ORIGINS`| `string` | `""` | Comma-delimited list of allowed CORS origins |
| `SIMS_CREDENTIAL_KEY` | `string` | **Required** | AES-256 key for encrypted vCenter credential storage |

---

## Development, Testing & CI Workflow

### Essential Make Commands

The project Makefile enforces strict formatting, linting, build verification, and automated testing:

```bash
# Code Quality & Formatting
make format                # Format all Go source files (go fmt)
make lint                  # Run golangci-lint
make vulncheck             # Scan dependencies for known CVEs via govulncheck

# Compilation & Codegen
make build                 # Compile all binaries into ./bin (collector, query-api, vcsim, etc.)
make sqlc-generate         # Generate type-safe Go database code from db/queries/
make sqlc-verify           # Verify that generated/db matches committed queries

# Testing
make test                  # Run fast unit tests
make test-race             # Run unit tests with Go data race detector enabled (-race)
make test-migration        # Verify database migration idempotency and rollback
make test-integration      # Execute integration test suite against PostgreSQL & vcsim
```

### Performance & Chaos Gates

SIMS includes extensive automated gates validating resilience, scale, and performance:

```bash
make test-gate-s5          # Run Sprint 5 Release Candidate Test Gate (v1.0.0 criteria)
make test-perf-query       # Validate query latency gate (P99 <= 5ms target)
make test-perf-ingest      # Validate ingestion latency gate (P95 batch <= 15ms, e2e <= 250ms)
make test-chaos            # Execute 9 fault injection scenarios (hung calls, dropped sockets, auth loss)
make test-soak             # Execute continuous ingestion and query soak test with leak detection
make test-scale-s4         # Run multi-target scale simulation across 3, 5, and 10 vCenters
```

---

## Core Invariants & Architectural Principles

All contributors and automation agents must adhere to the core system invariants outlined in [`AGENTS.md`](AGENTS.md):

1. **Strict Clean Boundaries (`web/` boundary):**
   - The backend service and frontend web interface are maintained with decoupled boundaries. Never modify, reformat, or delete files in `web/` when contributing backend changes. `git diff main --stat -- web/` must return 0 lines.
2. **SQL as Contract (No ORM):**
   - Direct PostgreSQL SQL with `sqlc` and `pgx/v5`. Never use an Object-Relational Mapper (ORM).
   - Never hand-edit files under `generated/db/`; always modify `db/queries/` and run `make sqlc-generate`.
3. **Zero Credentials in Code, Logs, or Traces:**
   - Passwords, API tokens, and vCenter session IDs are strictly write-only.
   - All OpenTelemetry spans, slog records, error outputs, and audit logs automatically sanitize sensitive fields.
4. **Single-Goroutine PropertyCollector Ownership:**
   - Exactly one background goroutine owns `WaitForUpdatesEx` per target session to prevent filter corruption.
5. **Non-Destructive Child Collection Updates:**
   - In update payloads, `nil` slices represent absence in the changeset and **preserve** existing child records in the database. Only explicit empty slices (`make([]T, 0)`) clear collections.
6. **Immutable Provenance:**
   - Primary VM creation metadata (`created_at`, `created_by`) belongs strictly to the Event ledger and is never overwritten by PropertyCollector updates or background reconciliations.

---

## vSphere Version Support Policy

SIMS is designed with a **forward-compatible, capability-driven version policy**:

- **Supported Versions:** VMware vCenter Server **7.0 and above, with no upper version ceiling** (validated on vSphere 7.0, 8.0, 9.0, and 9.1).
- **Unsupported Legacy Versions:** Releases prior to 7.0 (e.g., 6.0, 6.5, 6.7) are rejected during connection validation (`unsupported_version`).
- **Capability Detection:** Features (REST tag fetcher, session unification) are detected via runtime capability flags rather than fragile hardcoded version strings.
- **100% Read-Only Safety:** SIMS operates exclusively in read-only mode across all vSphere endpoints; it never alters or mutates hypervisor configurations.

*For complete details, see [`docs/supported-versions.md`](docs/supported-versions.md) and [`docs/vsphere-capability-matrix.md`](docs/vsphere-capability-matrix.md).*

---

## Production Deployment & Sizing

### Infrastructure Sizing Matrix

| Fleet Size | Active VMs | Collector Workers | Collector Sizing | Query API Instances | PostgreSQL Recommended Specs |
|:---|:---|:---|:---|:---|:---|
| **Small** | 10,000 | 1 replica | 1 vCPU / 1 GiB RAM | 1 replica (1 vCPU, 1 GiB) | 2 vCPU, 4 GiB RAM, 50 GiB SSD |
| **Medium** | 100,000 | 2 replicas | 2 vCPU / 2 GiB RAM | 2 replicas (2 vCPU, 2 GiB) | 4 vCPU, 16 GiB RAM, 250 GiB NVMe |
| **Large** | 500,000 | 4 replicas | 4 vCPU / 4 GiB RAM | 4 replicas (4 vCPU, 4 GiB) | 8 vCPU, 32 GiB RAM, 1 TB NVMe |
| **Enterprise** | 1,000,000+ | 6–8 replicas | 8 vCPU / 8 GiB RAM | 6 replicas (4 vCPU, 8 GiB) | 16 vCPU, 64 GiB RAM, 2 TB NVMe |

### Container Security Hardening

All production images (`deploy/Dockerfile.*`) adhere to security best practices:
- Run as non-root user `UID 10001` (`sims:sims`).
- Support read-only root filesystems (`readOnlyRootFilesystem: true`).
- Drop all Linux capabilities (`capabilities.drop: ["ALL"]`).
- Dedicated init container (`deploy/Dockerfile.migration`) executes database schema migrations before worker pods start.

*For detailed Kubernetes manifests, Helm patterns, and sizing recommendations, consult [`docs/deployment.md`](docs/deployment.md).*

---

## Documentation Index

- **Architecture & Design:**
  - [System Architecture Guide](docs/architecture.md)
  - [Database Schema & Indices Specification](docs/database.md)
  - [Architecture Decision Records (ADRs)](docs/adr/)
- **API & Integration:**
  - [Query API Specification & Usage Guide](docs/api.md)
  - [OpenAPI 3.1 YAML Contract](api/openapi.yaml)
  - [Frontend Integration Guide](docs/frontend-integration-guide.md)
- **Operations & Runbooks:**
  - [Production Deployment Guide](docs/deployment.md)
  - [High Availability & Lease Coordination Runbook](docs/ha-runbook.md)
  - [Production Troubleshooting & Anomaly Runbook](docs/troubleshooting.md)
  - [Backup & Restore Runbook](docs/backup-restore-runbook.md)
  - [PostgreSQL Performance Tuning Guide](docs/postgres-tuning.md)
  - [On-Call Operations Manual](docs/on-call.md)
- **vSphere Integration:**
  - [Supported vSphere Versions Policy](docs/supported-versions.md)
  - [vSphere Capability Matrix](docs/vsphere-capability-matrix.md)
  - [Real vCenter Test Runbook](docs/real-vcenter-test-runbook.md)

---

## License

This software is an internal infrastructure platform developed for scalable virtualization management. See project repository settings for licensing details and commercial usage terms.
