#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${PROJECT_ROOT}"

echo "======================================================================"
echo "    SIMS Sprint 5: Performance, Security, Chaos & Release (v1.0.0)    "
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

# 3. Apply database migrations up to version 14
echo ""
echo "==> 3. Verifying database migrations (goose)..."
bin/goose -dir db/migrations postgres "postgres://postgres:postgres@localhost:5433/sims?sslmode=disable" up

# 4. Build binaries
echo ""
echo "==> 4. Building SIMS binaries..."
make build

# 5. Run Performance Dataset Generation & Query Performance Gate (P99 <= 5ms)
echo ""
echo "==> 5. Verifying Query Performance Gate (Hard SLO: DB P99 <= 5ms)..."
go test -v -tags=integration ./tests/integration -run TestQueryPerformanceGate

# 6. Run Ingestion Performance Gate (P95 <= 75ms)
echo ""
echo "==> 6. Verifying Ingestion Performance Gate (Batch Write P95 <= 75ms)..."
go test -v -tags=integration ./tests/integration -run TestIngestionPerformanceGate

# 7. Demonstrate OpenTelemetry Tracing with 0 Secret Leaks
echo ""
echo "==> 7. Verifying OpenTelemetry Distributed Tracing & Zero-Leak Invariant..."
go test -v -tags=integration ./tests/integration -run TestTelemetry_EndToEndTracePropagationWithZeroLeaks

# 8. Run Security Hardening & Secret-Leak Audit
echo ""
echo "==> 8. Running Security Hardening, Fuzzing & Least Privilege Audit..."
go test -v -tags=integration ./tests/integration -run TestSecurity_

# 9. Run Chaos Resilience Suite (9 fault injection scenarios)
echo ""
echo "==> 9. Running Chaos Resilience Suite (9 fault injection scenarios)..."
go test -v -tags=integration ./tests/integration -run TestChaos_

# 10. Run Soak Test Harness (Smoke validation)
echo ""
echo "==> 10. Running Soak Test Harness (Continuous Workload & Leak Detection)..."
SOAK_DURATION=5s go test -v -tags=integration ./tests/soak -run TestSoak_ContinuousWorkload

# 11. Run Final Sprint 5 Release Candidate Test Gate
echo ""
echo "==> 11. Running Final Sprint 5 Release Candidate Test Gate (v1.0.0)..."
make test-gate-s5

echo ""
echo "======================================================================"
echo "    SIMS SPRINT 5 DEMO COMPLETE: ALL v1.0.0 RELEASE GATES PASSED!    "
echo "======================================================================"
