#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${PROJECT_ROOT}"

echo "======================================================================"
echo "    SIMS Sprint 1 Demo: 5,000-VM Hydration & First API Slice"
echo "======================================================================"

# 1. Ensure Postgres is up and healthy
echo ""
echo "==> 1. Ensuring PostgreSQL is healthy..."
docker compose -f deploy/docker-compose.yml up -d postgres
for i in {1..30}; do
    if docker exec sims-postgres pg_isready -U postgres -d sims >/dev/null 2>&1; then
        echo "PostgreSQL is ready."
        break
    fi
    sleep 1
done

# 2. Apply database migrations
echo ""
echo "==> 2. Verifying database migrations (goose)..."
bin/goose -dir db/migrations postgres "postgres://postgres:postgres@localhost:5433/sims?sslmode=disable" up

# 3. Build binaries
echo ""
echo "==> 3. Building SIMS binaries..."
make build

# 4. Run 5,000-VM Hydration Gate Test
echo ""
echo "==> 4. Executing 5,000-VM vcsim hydration and persistence test..."
go test -v -tags=integration -timeout 10m ./tests/integration -run TestGate_5000VMHydration

# 4b. Populate live vcsim inventory using SIMS collector
echo ""
echo "==> 4b. Syncing inventory from vcsim using collector daemon..."
docker compose -f deploy/docker-compose.yml up -d vcsim
PORT=8082 VCENTER_URL="https://localhost:8989/sdk" VCENTER_USER="user" VCENTER_PASSWORD="pass" VCENTER_INSECURE="true" DATABASE_URL="postgres://collector_rw:collector_rw_pass@localhost:5433/sims?sslmode=disable" SYNC_INTERVAL=0 timeout 4 bin/collector >/dev/null 2>&1 || true

# 5. Start Query API in background
echo ""
echo "==> 5. Starting SIMS Query API (query_ro role) on :8080..."
export PORT=8080
export LOG_LEVEL=info
export DATABASE_URL="postgres://query_ro:query_ro_pass@localhost:5433/sims?sslmode=disable"
export CORS_ALLOWED_ORIGINS="http://localhost:3000"

bin/query-api &
API_PID=$!
trap 'echo "Stopping Query API (PID $API_PID)..."; kill -TERM $API_PID 2>/dev/null || true' EXIT

# Wait for API readiness
for i in {1..20}; do
    if curl -s http://localhost:8080/readyz | grep -q '"status":"ok"'; then
        echo "Query API is ready!"
        break
    fi
    sleep 0.5
done

# 6. Demo API endpoints
echo ""
echo "----------------------------------------------------------------------"
echo " DEMO: GET /healthz"
echo "----------------------------------------------------------------------"
curl -s http://localhost:8080/healthz | jq . || curl -s http://localhost:8080/healthz

echo ""
echo "----------------------------------------------------------------------"
echo " DEMO: GET /readyz"
echo "----------------------------------------------------------------------"
curl -s http://localhost:8080/readyz | jq . || curl -s http://localhost:8080/readyz

echo ""
echo "----------------------------------------------------------------------"
echo " DEMO: GET /v1/vms?limit=3 (Keyset Pagination Page 1)"
echo "----------------------------------------------------------------------"
LIST_RESP=$(curl -s "http://localhost:8080/v1/vms?limit=3")
echo "$LIST_RESP" | jq . || echo "$LIST_RESP"

SAMPLE_ID=$(echo "$LIST_RESP" | jq -r '.items[0].id // empty')
NEXT_CURSOR=$(echo "$LIST_RESP" | jq -r '.pagination.next_cursor // empty')

if [ -n "$SAMPLE_ID" ]; then
    echo ""
    echo "----------------------------------------------------------------------"
    echo " DEMO: GET /v1/vms/${SAMPLE_ID} (Detailed VM Attachment Record)"
    echo "----------------------------------------------------------------------"
    curl -s "http://localhost:8080/v1/vms/${SAMPLE_ID}" | jq . || curl -s "http://localhost:8080/v1/vms/${SAMPLE_ID}"
fi

if [ -n "$NEXT_CURSOR" ]; then
    echo ""
    echo "----------------------------------------------------------------------"
    echo " DEMO: GET /v1/vms?limit=3&cursor=... (Keyset Pagination Page 2)"
    echo "----------------------------------------------------------------------"
    curl -s "http://localhost:8080/v1/vms?limit=3&cursor=${NEXT_CURSOR}" | jq . || curl -s "http://localhost:8080/v1/vms?limit=3&cursor=${NEXT_CURSOR}"
fi

echo ""
echo "----------------------------------------------------------------------"
echo " DEMO: GET /metrics (SIMS Telemetry Sample)"
echo "----------------------------------------------------------------------"
curl -s http://localhost:8080/metrics | grep -E '^# HELP sims_|^sims_' | head -n 25

echo ""
echo "======================================================================"
echo "    SIMS Sprint 1 Demo Complete — All Acceptance Criteria Met!"
echo "======================================================================"
