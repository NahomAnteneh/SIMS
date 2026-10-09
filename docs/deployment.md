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
| `DATABASE_URL` | string | **Required** | PostgreSQL connection URL |
| `AUTH_ENABLED` | bool | `true` | Enforce authentication (local username/password, Bearer JWT, or OIDC) |
| `JWT_SECRET` | string | Optional | HMAC-SHA256 signing secret for local JWT tokens |
| `OIDC_ISSUER_URL` | string | Optional | Upstream OIDC provider URL (for Active Directory / enterprise SSO integration) |
| `OIDC_AUDIENCE` | string | Optional | Expected JWT audience claim |

### Initial Administrator Provisioning (Local Authentication Mode)
When running without external Active Directory or OIDC integration, SIMS automatically provisions the initial administrator account on first startup:
- **Username:** `admin`
- **Password:** Set via `ADMIN_INITIAL_PASSWORD` in `/etc/sims/query-api.env`. If omitted, SIMS securely generates a random 16-character password and outputs it to the startup log.
- **Role:** `admin` (Full access)

Passwords can be updated or new users provisioned via the Web Console or the REST API (`POST /v1/users` and `PATCH /v1/users/{id}`).

---

## 4. Native Ubuntu VM Deployment (Bare-Metal / Virtual Machine)

SIMS supports production bare-metal deployment on Ubuntu 22.04 / 24.04 LTS without Docker or container runtimes, utilizing native systemd units, PostgreSQL 16+, Node.js 20+, and Nginx.

### 4.1 Architecture & Directory Layout

```
/opt/sims/
├── current -> /opt/sims/releases/v1.0.0      # Atomic release symlink
├── releases/
│   └── v1.0.0/                              # Staged release assets
│       ├── bin/                             # collector, query-api, goose
│       ├── migrations/                      # SQL migrations
│       └── web/                             # Next.js standalone build
├── shared/                                  # Persistent shared runtime data
└── logs/                                    # Service logs
```

### 4.2 Automated Host Installation

1. Generate the release package on your build host:
   ```bash
   make package-release VERSION=1.0.0
   ```
2. Copy the resulting tarball `dist/sims-v1.0.0-linux-amd64.tar.gz` to the target Ubuntu VM.
3. Run the installer as root:
   ```bash
   sudo ./scripts/install-ubuntu.sh 1.0.0 sims-v1.0.0-linux-amd64.tar.gz
   ```

The script automatically provisions PostgreSQL roles with strong random credentials, sets up restricted configuration files (`/etc/sims/*.env`, `0600`), installs systemd units, applies schema migrations, configures Nginx, and prints the generated `admin` password.

### 4.3 Zero-Downtime Release Upgrades

To upgrade to a new version (e.g., `v1.1.0`):
```bash
sudo ./scripts/upgrade-ubuntu.sh sims-v1.1.0-linux-amd64.tar.gz 1.1.0
```

The upgrade lifecycle executes atomically:
1. Extracts the new version to `/opt/sims/releases/v1.1.0/`.
2. Applies forward database migrations via Goose. If migrations fail, the upgrade aborts immediately and the running version is untouched.
3. Flips the `/opt/sims/current` symlink to `/opt/sims/releases/v1.1.0/`.
4. Restarts `sims-collector`, `sims-query-api`, and `sims-web` systemd services.
5. Polls `/readyz` health probes for 30 seconds.
6. **Automatic Rollback:** If the new release fails health checks, the script immediately reverts `/opt/sims/current` to the previous release and restarts services.

---

## 5. Health & Readiness Probes

All SIMS services expose standardized probes conforming to the OpenAPI 3.1 specification:
- **Liveness Probe:** `GET /healthz` (returns `200 OK` if the process is alive).
- **Readiness Probe:** `GET /readyz` (returns `200 OK` only when downstream dependencies, database connection pools, and worker loops are verified healthy).

