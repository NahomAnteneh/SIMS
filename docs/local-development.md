# Local Development Environment Guide

This guide walks a developer through setting up, running, observing, and resetting the Distributed vSphere Inventory & Search Platform on a clean development machine.

---

## 1. Prerequisites

Ensure your host machine has the following tools installed:

| Tool | Version Baseline | Installation Verification |
| :--- | :--- | :--- |
| **Go** | `1.27.1` | `go version` |
| **Node.js** | `26.7.0` | `node -v` |
| **npm** | `11.x` | `npm -v` |
| **Docker** | `29.x`+ | `docker --version` |
| **Docker Compose** | `5.x`+ | `docker compose version` |

The repository includes `.tool-versions` and `.nvmrc` compatible with `asdf`, `mise`, and `nvm`.

---

## 2. Quickstart

1. **Clone the repository:**
   ```bash
   git clone <repo-url> sims
   cd sims
   ```

2. **Install pinned CLI tools into `./bin`:**
   ```bash
   make tools
   ```
   This installs `sqlc v1.31.1`, `goose v3.28.0`, `golangci-lint`, and `govulncheck`.

3. **Start the local stack:**
   ```bash
   make dev-up
   ```

4. **Verify running services:**
   ```bash
   docker compose -f deploy/docker-compose.yml ps
   ```

---

## 3. Service Endpoints & Credentials

All default service endpoints, ports, and credentials for local development are listed below:

| Service | Host Port | Protocol | Path / Target | Default Credentials |
| :--- | :--- | :--- | :--- | :--- |
| **Next.js UI** | `3000` | HTTP | `http://localhost:3000` | None |
| **Query API** | `8080` | HTTP | `http://localhost:8080/healthz` | None |
| **Collector** | `8081` | HTTP | `http://localhost:8081/healthz` | None |
| **PostgreSQL 18.6** | `5433` | TCP | `localhost:5433/sims` | `postgres` / `postgres` |
| **vcsim (Simulator)**| `8989` | HTTPS | `https://localhost:8989/sdk` | `user` / `pass` |
| **Prometheus** | `9090` | HTTP | `http://localhost:9090` | None |
| **Grafana** | `3001` | HTTP | `http://localhost:3001` | `admin` / `admin` |

### Database Roles & Least Privileges
PostgreSQL is pre-initialized with three discrete application roles:
- `migration_owner` (`password: migration_owner_pass`): Schema owner with DDL privileges for running migrations.
- `collector_rw` (`password: collector_rw_pass`): Data ingestion worker with DML (SELECT, INSERT, UPDATE, DELETE) access.
- `query_ro` (`password: query_ro_pass`): Read-only API role with SELECT-only access.

---

## 4. Operational Telemetry & Probes

Both the `collector` and `query-api` binaries expose standardized operational probes:

- **Liveness Probe:**
  ```bash
  curl -s http://localhost:8080/healthz | jq
  curl -s http://localhost:8081/healthz | jq
  ```
- **Readiness Probe:**
  ```bash
  curl -s http://localhost:8080/readyz | jq
  curl -s http://localhost:8081/readyz | jq
  ```
- **Prometheus Metrics:**
  ```bash
  curl -s http://localhost:8080/metrics
  curl -s http://localhost:8081/metrics
  ```

Prometheus automatically scrapes both services every 15 seconds. Grafana is pre-configured with Prometheus as its default datasource at `http://localhost:3001`.

---

## 5. Development Workflow & Commands

| Task | Command | Description |
| :--- | :--- | :--- |
| **Run Unit Tests** | `make test-unit` | Executes fast unit tests (`-short`) |
| **Run Race Detector** | `make test-race` | Executes tests with Go race detection enabled |
| **Run Linter** | `make lint` | Validates Go code with `golangci-lint` |
| **Format Code** | `make format` | Formats Go files |
| **Generate Database Code** | `make sqlc-generate` | Compiles SQL queries into type-safe Go in `generated/db` |
| **Verify Code Generation** | `make sqlc-verify` | Confirms committed Go files match SQL schema definitions |
| **Run Migration Tests** | `make test-migration` | Applies and validates schema migrations |
| **Run DB Integration Tests**| `make test-db-integration` | Verifies role privileges against PostgreSQL |
| **Run Simulator Tests** | `make test-vcsim-integration`| Executes in-process govmomi simulator tests |
| **Security Scan** | `make vulncheck` | Analyzes Go dependencies with `govulncheck` |
| **Build Containers** | `make container-build` | Builds production multi-stage Docker images |

---

## 6. Stopping & Resetting the Environment

- **Stop all services:**
  ```bash
  make dev-down
  ```
- **Reset all state (purge database, telemetry, and containers):**
  ```bash
  make dev-reset
  ```

---

## 7. Security & Credential Rules

1. **Never commit secrets:** Passwords, tokens, and keys must only be supplied via environment variables or git-ignored local files.
2. **Log Redaction:** All application log output passes through `internal/logging`, which automatically strips basic auth credentials from URLs and masks sensitive keys (`password`, `token`, `secret`, `api_key`).
