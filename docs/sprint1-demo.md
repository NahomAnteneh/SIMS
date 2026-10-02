# SIMS Sprint 1 Demo Guide

## Overview

This guide describes how to run and verify the Sprint 1 deliverables: vCenter/vcsim inventory hydration, high-throughput bulk persistence into PostgreSQL 18, and the first slice of the Query API with keyset pagination and OpenAPI 3.1 contract compliance (Backend only).

---

## Prerequisites

1. **Docker & Docker Compose**: For PostgreSQL 18 and vcsim.
2. **Go 1.24+**: For compiling binaries and executing integration tests.
3. **Curl & jq**: For interacting with the Query API endpoints.

---

## Quick Start: One-Command Automated Demo

Execute the demo target:

```bash
make demo-s1
```

Or run the script directly:

```bash
./scripts/demo-s1.sh
```

### What `make demo-s1` Does:

1. **Pre-flight & Database Readiness**: Starts PostgreSQL 18 container and verifies healthy status.
2. **Database Migrations**: Applies goose migrations 000001 through 000005, verifying schema versions, tables, indexes, and least-privilege role permissions (`collector_rw` and `query_ro`).
3. **Binary Compilation**: Builds `bin/collector` and `bin/query-api`.
4. **Sprint 1 Test Gate**: Runs `TestGate_5000VMHydration` (hydrating 5,000 VMs from in-process simulator into PostgreSQL in ~4.8 seconds, testing duplicate-batch idempotency and ContainerView lifecycle).
5. **Collector Daemon Sync**: Runs `bin/collector` against the `vcsim` container to establish topology and inventory.
6. **Query API Launch**: Starts `bin/query-api` connecting strictly as the read-only database user `query_ro` on port 8080 with CORS middleware enabled.
7. **Live Endpoint Inspection**:
   - `GET /healthz` (liveness)
   - `GET /readyz` (readiness with database pool ping)
   - `GET /v1/vms?limit=3` (Page 1 keyset pagination)
   - `GET /v1/vms/{id}` (single VM details with attached datastores and NICs)
   - `GET /v1/vms?limit=3&cursor=...` (Page 2 continuation)
   - `GET /metrics` (Prometheus exposition format telemetry)
8. **Graceful Shutdown**: Sends SIGTERM to the background API process and confirms clean exit.

---

## Manual Verification & Curl Walkthrough

### 1. Start Services
```bash
docker compose -f deploy/docker-compose.yml up -d postgres vcsim
make tools build
bin/goose -dir db/migrations postgres "postgres://postgres:postgres@localhost:5433/sims?sslmode=disable" up
```

### 2. Run Collector Sync
```bash
PORT=8082 \
VCENTER_URL="https://localhost:8989/sdk" \
VCENTER_USER="user" \
VCENTER_PASSWORD="pass" \
VCENTER_INSECURE="true" \
DATABASE_URL="postgres://collector_rw:collector_rw_pass@localhost:5433/sims?sslmode=disable" \
SYNC_INTERVAL=0 \
timeout 5 bin/collector || true
```

### 3. Start Query API
```bash
PORT=8080 \
DATABASE_URL="postgres://query_ro:query_ro_pass@localhost:5433/sims?sslmode=disable" \
CORS_ALLOWED_ORIGINS="http://localhost:3000" \
bin/query-api
```

### 4. Query Endpoints
```bash
# Health & Readiness
curl -s http://localhost:8080/healthz | jq .
curl -s http://localhost:8080/readyz | jq .

# Keyset Pagination Page 1
curl -s "http://localhost:8080/v1/vms?limit=3" | jq .

# Sample Detail Query by ID
VM_ID=$(curl -s "http://localhost:8080/v1/vms?limit=1" | jq -r '.items[0].id')
curl -s "http://localhost:8080/v1/vms/${VM_ID}" | jq .

# Keyset Pagination Page 2 using Next Cursor
CURSOR=$(curl -s "http://localhost:8080/v1/vms?limit=3" | jq -r '.pagination.next_cursor')
curl -s "http://localhost:8080/v1/vms?limit=3&cursor=${CURSOR}" | jq .

# Prometheus Metrics
curl -s http://localhost:8080/metrics | grep sims_
```
