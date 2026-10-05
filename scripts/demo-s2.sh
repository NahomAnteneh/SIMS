#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${PROJECT_ROOT}"

echo "======================================================================"
echo "    SIMS Sprint 2 Demo: Real-Time Ingestion, Events & Metadata Sync"
echo "======================================================================"

# 1. Ensure PostgreSQL is up and healthy
echo ""
echo "==> 1. Ensuring PostgreSQL is healthy on port 5433..."
docker compose -f deploy/docker-compose.yml up -d postgres
for i in {1..30}; do
    if docker exec sims-postgres pg_isready -U postgres -d sims >/dev/null 2>&1; then
        echo "PostgreSQL is ready."
        break
    fi
    sleep 1
done

# 2. Ensure vcsim is running
echo ""
echo "==> 2. Ensuring vcsim is running on port 8989..."
docker compose -f deploy/docker-compose.yml up -d vcsim
sleep 1

# 3. Apply database migrations
echo ""
echo "==> 3. Verifying database migrations (goose)..."
bin/goose -dir db/migrations postgres "postgres://postgres:postgres@localhost:5433/sims?sslmode=disable" up

# 4. Build binaries
echo ""
echo "==> 4. Building SIMS binaries..."
make build

# 5. Execute Real-Time Pipeline Churn Test
echo ""
echo "==> 5. Demonstrating Real-Time Streaming & Queue Ingestion Under Churn..."
CHURN_DURATION=10s go test -v -tags=integration ./tests/integration -run TestChurn_RealtimePipeline

# 6. Demonstrate Spike CLI Live Observation Mode
echo ""
echo "==> 6. Demonstrating Spike Tool Live Metadata Observation Mode..."
VCENTER_URL="http://localhost:8989/sdk" OBSERVE_DURATION=3s ./bin/vcenter-spike -observe

# 7. Start Query API in background
echo ""
echo "==> 7. Starting SIMS Query API (query_ro role) on :8080..."
export PORT=8080
export LOG_LEVEL=info
export DATABASE_URL="postgres://query_ro:query_ro_pass@localhost:5433/sims?sslmode=disable"
export CORS_ALLOWED_ORIGINS="http://localhost:3000"

./bin/query-api &
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

# 8. Query API Endpoints
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
echo " DEMO: GET /metrics (Real-Time Ingestion Gauges)"
echo "----------------------------------------------------------------------"
curl -s http://localhost:8080/metrics | grep -E "sims_collector|sims_queue|sims_tags" || echo "Metrics response received."

echo ""
echo "----------------------------------------------------------------------"
echo " DEMO: GET /v1/vms?limit=3 (Inventory List with Metadata)"
echo "----------------------------------------------------------------------"
LIST_RESP=$(curl -s "http://localhost:8080/v1/vms?limit=3")
echo "$LIST_RESP" | jq . || echo "$LIST_RESP"

SAMPLE_ID=$(echo "$LIST_RESP" | jq -r '.items[0].id // empty')
if [ -n "$SAMPLE_ID" ]; then
    echo ""
    echo "----------------------------------------------------------------------"
    echo " DEMO: GET /v1/vms/${SAMPLE_ID} (Detailed VM with Tags & Custom Attributes)"
    echo "----------------------------------------------------------------------"
    curl -s "http://localhost:8080/v1/vms/${SAMPLE_ID}" | jq . || curl -s "http://localhost:8080/v1/vms/${SAMPLE_ID}"
fi

echo ""
echo "======================================================================"
echo "    SIMS Sprint 2 Demo Completed Successfully!"
echo "======================================================================"
