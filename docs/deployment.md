# SIMS Production Deployment Guide (v1.0.0)

## 1. System Requirements

### 1.1 Infrastructure Sizing Matrix

| Fleet Size | Active VMs | Collector Workers | Collector CPU / RAM | Query API Instances | PostgreSQL Specs |
|:---|:---|:---|:---|:---|:---|
| **Small** | 10,000 | 1 replica | 1 vCPU / 1 GiB | 1 replica (1 vCPU, 1 GiB) | 2 vCPU, 4 GiB RAM, 50 GiB SSD |
| **Medium** | 100,000 | 2 replicas | 2 vCPU / 2 GiB | 2 replicas (2 vCPU, 2 GiB) | 4 vCPU, 16 GiB RAM, 250 GiB NVMe |
| **Large** | 500,000 | 4 replicas | 4 vCPU / 4 GiB | 4 replicas (4 vCPU, 4 GiB) | 8 vCPU, 32 GiB RAM, 1 TB NVMe |
| **Enterprise** | 1,000,000+ | 6–8 replicas | 8 vCPU / 8 GiB | 6 replicas (4 vCPU, 8 GiB) | 16 vCPU, 64 GiB RAM, 2 TB NVMe |

### 1.2 PostgreSQL Requirements
- **Version:** PostgreSQL 18.x
- **Required Extensions:**
  - `btree_gist` (for multidimensional range indexing)
  - `ltree` (for hierarchical folder and datacenter tree queries)
  - `pg_trgm` (for fast substring and fuzzy name matching)
- **Role Isolation:**
  - `migration_owner` (owns schema and applies migrations)
  - `collector_rw` (DML on inventory and collector tables)
  - `query_ro` (strictly read-only `SELECT` access)

---

## 2. Container Images & Security Hardening

All SIMS containers are built using minimal multi-stage Alpine images with zero root privileges:
- **Non-Root UID:** Run as `UID 10001` (`sims:sims`).
- **Read-Only Root Filesystem:** Query API and Collector run cleanly with `readOnlyRootFilesystem: true`.
- **Dropped Capabilities:** All Linux capabilities (`ALL`) should be dropped in Kubernetes security contexts.
- **Dedicated Migration Container:** Migrations are executed via `deploy/Dockerfile.migration` in an init container before application pods start.

### Kubernetes Security Context Example
```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 10001
  runAsGroup: 10001
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  capabilities:
    drop:
      - ALL
```

---

## 3. Environment Variable Reference

### Collector Service (`sims-collector`)

| Variable | Type | Default | Description |
|:---|:---|:---|:---|
| `PORT` | int | `8081` | HTTP metrics and readiness server port |
| `LOG_LEVEL` | string | `info` | Logging verbosity (`debug`, `info`, `warn`, `error`) |
| `DATABASE_URL` | string | **Required** | PostgreSQL connection URL (e.g. `postgres://collector_rw:pass@host:5432/sims?sslmode=verify-full`) |
| `VCENTER_URL` | string | Optional | Bootstrap single vCenter URL (if not dynamically registered via API) |
| `VCENTER_USERNAME` | string | Optional | Bootstrap vCenter username |
| `VCENTER_PASSWORD` | string | Optional | Bootstrap vCenter password |
| `VCENTER_INSECURE` | bool | `false` | Disable TLS certificate verification (development only) |

### Query API Service (`sims-query-api`)

| Variable | Type | Default | Description |
|:---|:---|:---|:---|
| `PORT` | int | `8080` | HTTP API listen port |
| `LOG_LEVEL` | string | `info` | Logging verbosity |
| `DATABASE_URL` | string | **Required** | Read-only connection URL (`postgres://query_ro:pass@host:5432/sims?sslmode=verify-full`) |
| `AUTH_ENABLED` | bool | `true` | Enforce JWT bearer authentication |
| `JWT_PUBLIC_KEY` | string | Optional | PEM-encoded RSA/ECDSA public key or JWKS endpoint |

---

## 4. Health & Readiness Probes

All SIMS services expose standardized probes conforming to the OpenAPI 3.1 specification:
- **Liveness Probe:** `GET /healthz` (returns `200 OK` if the process is alive).
- **Readiness Probe:** `GET /readyz` (returns `200 OK` only when downstream dependencies, database connection pools, and worker loops are verified healthy).
