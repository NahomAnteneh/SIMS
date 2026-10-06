#!/usr/bin/env bash
set -euo pipefail

# SIMS 72-Hour Soak Test Harness Runner (S5.10)
# Usage:
#   SOAK_DURATION=5m ./scripts/soak-test.sh    # Quick smoke soak
#   SOAK_DURATION=72h ./scripts/soak-test.sh   # Full 72-hour endurance run

SOAK_DURATION="${SOAK_DURATION:-5m}"
DB_PORT="${POSTGRES_PORT:-5433}"
DB_HOST="${POSTGRES_HOST:-localhost}"

echo "=========================================================="
echo "          SIMS Soak Test Harness (S5.10)"
echo "=========================================================="
echo "Duration:       ${SOAK_DURATION}"
echo "Database Host:  ${DB_HOST}:${DB_PORT}"
echo "Timestamp:      $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
echo "=========================================================="

SOAK_DURATION="${SOAK_DURATION}" \
POSTGRES_PORT="${DB_PORT}" \
POSTGRES_HOST="${DB_HOST}" \
go test -v -tags=integration -timeout 75h ./tests/soak -run TestSoak_ContinuousWorkload

echo "=========================================================="
echo "SUCCESS: Soak test completed within stability tolerances."
echo "=========================================================="
