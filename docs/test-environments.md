# Test Environments Specification

This document defines the deployment tiers, execution models, and isolation boundaries across the testing lifecycle for the Distributed vSphere Inventory & Search Platform.

---

## 1. Environment Topology Matrix

| Attribute | Local Dev (`dev-up`) | CI Pipeline (GitHub Actions) | Staging / Lab | Real-vCenter Nightly |
| :--- | :--- | :--- | :--- | :--- |
| **Execution Trigger** | Developer manual invocation | Every Pull Request & merge to `main` | Continuous deployment from `main` | Scheduled nightly cron / manual dispatch |
| **Database** | PostgreSQL 18.6 container (`5433:5432`) | Service container `postgres:18.6-alpine` | Dedicated CloudNativePG 1.30.x cluster | Staging PostgreSQL instance |
| **vSphere Target** | `vmware/vcsim` container (`8989:8989`) | In-process Go `simulator.VPX()` | Clustered `vcsim` or Lab vCenter | Real VMware vSphere (7.0.x, 8.0.x U3, 9.0.x, 9.1.x) |
| **Telemetry** | Prometheus + Grafana containers | Telemetry endpoint validation tests | Prometheus + Grafana + OpenTelemetry Collector | Production-mirror observability stack |
| **Secret Management** | Local env vars / `.env.example` | GitHub Actions encrypted repository secrets | Kubernetes Secrets / HashiCorp Vault | Cloud Secret Manager / Vault |
| **Network Isolation**| Local Docker bridge network | Ephemeral container network | Dedicated VPC / Kubernetes namespace | Dedicated management VLAN with firewall rules |

---

## 2. Environment Details

### 2.1 Local Development Environment
- **Purpose:** Inner-loop feature implementation, query experimentation, and manual smoke testing.
- **Workflow:**
  1. Boot stack: `make dev-up`
  2. Modify code / SQL migrations / queries
  3. Validate: `make test-unit`, `make test-race`, `make lint`, `make sqlc-verify`
  4. Reset when needed: `make dev-reset`
- **Security:** Credential-redaction helper (`internal/logging`) prevents secrets from escaping into stdout/stderr.

### 2.2 Continuous Integration (CI) Environment
- **Platform:** GitHub Actions (`.github/workflows/ci.yml`).
- **Pipeline Stage Sequence:**
  1. **Format:** Verify all files conform to `gofmt` and `.editorconfig`.
  2. **Lint:** `golangci-lint` enforces clean code, error checking, and security scans.
  3. **Unit Tests:** Fast in-memory unit tests (`make test-unit`).
  4. **Race Detection:** Unit tests with Go thread sanitizer (`make test-race`).
  5. **sqlc Code Generation Verification:** `make sqlc-verify` runs `sqlc generate` and fails if committed Go code differs from SQL definitions.
  6. **Migration Verification:** Applies forward migrations, verifies idempotency, tests rollback and re-apply (`make test-migration`).
  7. **PostgreSQL Integration Tests:** Verifies least-privilege role boundaries (`collector_rw`, `query_ro`) on live PostgreSQL (`make test-db-integration`).
  8. **vcsim Integration Tests:** Executes in-process govmomi simulator tests (`make test-vcsim-integration`).
  9. **Vulnerability Scanning:** Runs `govulncheck` on all dependencies (`make vulncheck`).
  10. **Container Build:** Builds multi-stage non-root container images (`make container-build`).

### 2.3 Staging / Synthetic Scale Environment
- **Purpose:** End-to-end integration validation, multi-collector lease contention, and high-volume snapshot ingestion.
- **Target Workloads:**
  - High-cardinality inventory testing: 10,000 to 100,000 synthetic VMs generated in `vcsim`.
  - Ingestion throughput benchmarking (`COPY FROM STDIN` streaming speed).
  - Search query latency profiling with `pg_trgm` and `ltree`.
- **Fault Injection:** Testing collector failover when a worker pod is evicted while holding an active lease.

### 2.4 Real-vCenter Nightly Environment
- **Purpose:** Detecting behavioral drift between `vcsim` and production VMware vCenter Server instances.
- **Execution:** Automated scheduled run using `cmd/vcenter-spike` and end-to-end collector suites against isolated enterprise lab vCenters.
- **Validation Checklist:**
  - Proprietary SOAP fault handling (e.g. `InvalidCollectorVersion`).
  - Session eviction and re-authentication under 30-minute idle limits.
  - Accurate extraction of `guest.net` IP/MAC structures across heterogeneous guest OS types.
  - Verification of Cross-vCenter vMotion (xvMotion) event delivery.
- **Guardrails:** Strictly read-only test suites; all server-side ContainerViews and filters are destroyed immediately upon test completion.
