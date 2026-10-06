#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${PROJECT_ROOT}"

echo "======================================================================"
echo "    SIMS Sprint 4 Demo: Multi-vCenter Scale, Leases & Reconciliation"
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

# 3. Apply database migrations up to version 13
echo ""
echo "==> 3. Verifying database migrations (goose)..."
bin/goose -dir db/migrations postgres "postgres://postgres:postgres@localhost:5433/sims?sslmode=disable" up

# 4. Build binaries
echo ""
echo "==> 4. Building SIMS binaries..."
make build

# 5. Run Sprint 4 Test Gate Suite
echo ""
echo "==> 5. Running Sprint 4 Test Gate Suite..."
go test -v -tags=integration ./tests/integration -run TestGateS4

# 6. Run Multi-Target Scale Suite
echo ""
echo "==> 6. Running Multi-Target Scale Simulation (3, 5, 10 vCenters)..."
go test -v -tags=integration ./tests/integration -run TestScaleS4

# 7. Seed demo multi-vCenter targets directly into PostgreSQL
echo ""
echo "==> 7. Seeding multi-vCenter fleet into PostgreSQL..."
docker exec -i sims-postgres psql -U postgres -d sims <<'EOF'
-- Clean fixtures
DELETE FROM collector_leases WHERE target_id IN (
    SELECT id FROM collector_targets WHERE vcenter_id IN (
        '11111111-aaaa-1111-aaaa-111111111111',
        '22222222-bbbb-2222-bbbb-222222222222',
        '33333333-cccc-3333-cccc-333333333333'
    )
);
DELETE FROM collector_targets WHERE vcenter_id IN (
    '11111111-aaaa-1111-aaaa-111111111111',
    '22222222-bbbb-2222-bbbb-222222222222',
    '33333333-cccc-3333-cccc-333333333333'
);
DELETE FROM virtual_machines WHERE vcenter_id IN (
    '11111111-aaaa-1111-aaaa-111111111111',
    '22222222-bbbb-2222-bbbb-222222222222',
    '33333333-cccc-3333-cccc-333333333333'
);
DELETE FROM vcenters WHERE id IN (
    '11111111-aaaa-1111-aaaa-111111111111',
    '22222222-bbbb-2222-bbbb-222222222222',
    '33333333-cccc-3333-cccc-333333333333'
);

-- Insert 3 vCenters across supported versions
INSERT INTO vcenters (id, name, api_url, product_version, api_version, build, status, priority, enabled)
VALUES 
  ('11111111-aaaa-1111-aaaa-111111111111', 'vc-prod-us-east', 'https://vceast.corp.local/sdk', '8.0.3', '8.0.3.0', '24022515', 'READY', 100, true),
  ('22222222-bbbb-2222-bbbb-222222222222', 'vc-prod-us-west', 'https://vcwest.corp.local/sdk', '7.0.3', '7.0.3.0', '20150588', 'READY', 100, true),
  ('33333333-cccc-3333-cccc-333333333333', 'vc-stage-eu-central', 'https://vceu.corp.local/sdk', '8.0.2', '8.0.2.0', '23305545', 'READY', 50, true);

-- Register collector targets
INSERT INTO collector_targets (id, vcenter_id, enabled, priority)
VALUES 
  ('a1a1a1a1-aaaa-1111-aaaa-111111111111', '11111111-aaaa-1111-aaaa-111111111111', true, 100),
  ('b2b2b2b2-b2b2-b2b2-b2b2-b2b2b2b2b2b2', '22222222-bbbb-2222-bbbb-222222222222', true, 100),
  ('c3c3c3c3-c3c3-c3c3-c3c3-c3c3c3c3c3c3', '33333333-cccc-3333-cccc-333333333333', true, 50);

-- Seed initial VM on vCenter A
INSERT INTO virtual_machines (id, vcenter_id, moref, instance_uuid, name, power_state, connection_state, cpu_cores, memory_mb, guest_os, annotation, first_seen_at)
VALUES (
  '99999999-9999-9999-9999-999999999999',
  '11111111-aaaa-1111-aaaa-111111111111',
  'vm-prod-web-01',
  'inst-uuid-corp-web-01',
  'prod-web-01',
  'poweredOn',
  'connected',
  8,
  32768,
  'rhel9_64Guest',
  'Mission-critical production web cluster',
  NOW() - INTERVAL '10 days'
);
EOF

# 8. Demonstrate dynamic worker lease acquisition
echo ""
echo "==> 8. Demonstrating dynamic worker lease acquisition (PostgreSQL row locking)..."
docker exec -i sims-postgres psql -U postgres -d sims <<'EOF'
-- Worker 1 acquires East and West
INSERT INTO collector_leases (target_id, holder_id, lease_until, version)
VALUES 
  ('a1a1a1a1-aaaa-1111-aaaa-111111111111', 'sims-worker-01', NOW() + INTERVAL '30 seconds', 1),
  ('b2b2b2b2-b2b2-b2b2-b2b2-b2b2b2b2b2b2', 'sims-worker-01', NOW() + INTERVAL '30 seconds', 1);

-- Worker 2 acquires EU
INSERT INTO collector_leases (target_id, holder_id, lease_until, version)
VALUES 
  ('c3c3c3c3-c3c3-c3c3-c3c3-c3c3c3c3c3c3', 'sims-worker-02', NOW() + INTERVAL '30 seconds', 1);

SELECT 
  cl.target_id, 
  vc.name AS vcenter_name, 
  cl.holder_id, 
  cl.version AS fencing_version, 
  cl.lease_until 
FROM collector_leases cl
JOIN collector_targets ct ON cl.target_id = ct.id
JOIN vcenters vc ON ct.vcenter_id = vc.id;
EOF

# 9. Demonstrate standby takeover on lease expiration
echo ""
echo "==> 9. Demonstrating worker crash & standby takeover (fencing token increment)..."
docker exec -i sims-postgres psql -U postgres -d sims <<'EOF'
-- Simulate worker-01 crash: lease on vc-prod-us-east expires
UPDATE collector_leases
SET lease_until = NOW() - INTERVAL '5 seconds'
WHERE target_id = 'a1a1a1a1-aaaa-1111-aaaa-111111111111';

-- Standby worker-02 detects expired lease and acquires takeover atomically
UPDATE collector_leases
SET holder_id = 'sims-worker-02',
    lease_until = NOW() + INTERVAL '30 seconds',
    version = version + 1,
    updated_at = NOW()
WHERE target_id = 'a1a1a1a1-aaaa-1111-aaaa-111111111111' AND lease_until < NOW()
RETURNING target_id, holder_id, version AS new_fencing_version, lease_until;
EOF

# 10. Demonstrate cross-vCenter migration (xvMotion)
echo ""
echo "==> 10. Demonstrating cross-vCenter migration (xvMotion) with synthetic UUID preservation..."
docker exec -i sims-postgres psql -U postgres -d sims <<'EOF'
-- Before migration: VM is on vc-prod-us-east
SELECT id, vcenter_id, moref, instance_uuid, name, first_seen_at FROM virtual_machines WHERE instance_uuid = 'inst-uuid-corp-web-01';

-- xvMotion to vc-prod-us-west (vcenter_id: 22222222-bbbb-2222-bbbb-222222222222, new moref: vm-west-web-01)
UPDATE virtual_machines
SET vcenter_id = '22222222-bbbb-2222-bbbb-222222222222',
    moref = 'vm-west-web-01',
    deleted_at = NULL,
    updated_at = NOW()
WHERE instance_uuid = 'inst-uuid-corp-web-01'
RETURNING id, vcenter_id, moref, instance_uuid, name, first_seen_at, updated_at;
EOF

# 11. Demonstrate differential reconciliation
echo ""
echo "==> 11. Demonstrating differential reconciliation set math..."
docker exec -i sims-postgres psql -U postgres -d sims <<'EOF'
-- Seed stale VM to demonstrate tombstoning
INSERT INTO virtual_machines (id, vcenter_id, moref, instance_uuid, name, power_state, connection_state, cpu_cores, memory_mb, guest_os, annotation)
VALUES (
  '88888888-8888-8888-8888-888888888888',
  '22222222-bbbb-2222-bbbb-222222222222',
  'vm-stale-01',
  'inst-stale-01',
  'stale-vm',
  'poweredOff',
  'disconnected',
  2,
  4096,
  'otherGuest',
  'Decommissioned outside SIMS'
);

-- Differential set math: missing in vCenter -> Tombstone
UPDATE virtual_machines
SET deleted_at = NOW(), updated_at = NOW()
WHERE vcenter_id = '22222222-bbbb-2222-bbbb-222222222222' AND moref = 'vm-stale-01'
RETURNING id, moref, name, deleted_at;
EOF

# 12. Demonstrate bounded batch tombstone purging
echo ""
echo "==> 12. Demonstrating bounded batch tombstone purging (S4.9)..."
docker exec -i sims-postgres psql -U postgres -d sims <<'EOF'
-- Backdate deleted_at to 40 days ago (> 30 days retention)
UPDATE virtual_machines
SET deleted_at = NOW() - INTERVAL '40 days'
WHERE id = '88888888-8888-8888-8888-888888888888';

-- Execute bounded batch delete
DELETE FROM virtual_machines
WHERE id IN (
    SELECT vm.id
    FROM virtual_machines vm
    WHERE vm.deleted_at IS NOT NULL AND vm.deleted_at < NOW() - INTERVAL '30 days'
    ORDER BY vm.deleted_at ASC
    LIMIT 10
);
EOF

echo ""
echo "======================================================================"
echo "    SIMS Sprint 4 Demo Complete - All S4 Acceptance Gates Verified"
echo "======================================================================"
