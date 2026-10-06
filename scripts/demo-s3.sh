#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${PROJECT_ROOT}"

echo "======================================================================"
echo "    SIMS Sprint 3 Demo: Search Engine, Complete API, Auth & vCenter Mgmt"
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

# 3. Apply database migrations up to version 12
echo ""
echo "==> 3. Verifying database migrations (goose)..."
bin/goose -dir db/migrations postgres "postgres://postgres:postgres@localhost:5433/sims?sslmode=disable" up

# 4. Build binaries
echo ""
echo "==> 4. Building SIMS binaries..."
make build

# 5. Run Sprint 3 Test Gate Suite
echo ""
echo "==> 5. Running Sprint 3 Test Gate Suite..."
go test -v -tags=integration ./tests/integration -run TestGateS3

# 6. Seed demo inventory data directly into PostgreSQL
echo ""
echo "==> 6. Seeding multi-tenant demo inventory into PostgreSQL..."
docker exec -i sims-postgres psql -U postgres -d sims <<'EOF'
-- Clean demo fixtures
DELETE FROM audit_events;
DELETE FROM virtual_machines WHERE id IN ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', 'cccccccc-cccc-cccc-cccc-cccccccccccc');
DELETE FROM vcenters WHERE id IN ('11111111-1111-1111-1111-111111111111', '22222222-2222-2222-2222-222222222222');

-- Insert demo vCenters
INSERT INTO vcenters (id, name, api_url, product_version, api_version, build, status, priority, enabled)
VALUES 
  ('11111111-1111-1111-1111-111111111111', 'demo-vc-prod', 'https://vc-prod.corp.local:443/sdk', '8.0.3', '8.0.3.0', '24022515', 'READY', 100, true),
  ('22222222-2222-2222-2222-222222222222', 'demo-vc-dev', 'https://vc-dev.corp.local:443/sdk', '7.0.3', '7.0.3.0', '20150588', 'READY', 100, true)
ON CONFLICT (id) DO UPDATE SET updated_at = NOW();

-- Insert demo Datacenters
INSERT INTO datacenters (id, vcenter_id, moref, name)
VALUES
  ('11111111-1111-1111-1111-111111111111', '11111111-1111-1111-1111-111111111111', 'datacenter-1', 'Datacenter')
ON CONFLICT (vcenter_id, moref) DO NOTHING;

-- Insert demo Folders
INSERT INTO inventory_folders (id, vcenter_id, moref, name, type, path)
VALUES
  ('33333333-3333-3333-3333-333333333331', '11111111-1111-1111-1111-111111111111', 'group-v10', 'Production', 'vm', 'Datacenter.vm.Production'),
  ('33333333-3333-3333-3333-333333333332', '22222222-2222-2222-2222-222222222222', 'group-v20', 'Development', 'vm', 'Datacenter.vm.Development')
ON CONFLICT (vcenter_id, moref) DO NOTHING;

-- Insert demo Clusters
INSERT INTO clusters (id, vcenter_id, datacenter_id, moref, name)
VALUES
  ('44444444-4444-4444-4444-444444444441', '11111111-1111-1111-1111-111111111111', '11111111-1111-1111-1111-111111111111', 'domain-c10', 'prod-cluster')
ON CONFLICT (vcenter_id, moref) DO NOTHING;

-- Insert demo Networks
INSERT INTO networks (id, vcenter_id, moref, name, type, vlan_ids)
VALUES
  ('55555555-5555-5555-5555-555555555551', '11111111-1111-1111-1111-111111111111', 'network-10', 'VM Network Prod', 'Standard', '{[100, 200]}')
ON CONFLICT (vcenter_id, moref) DO NOTHING;

-- Insert demo Virtual Machines
INSERT INTO virtual_machines (
  id, vcenter_id, moref, instance_uuid, name, power_state, connection_state,
  cpu_cores, memory_mb, cluster_id, folder_path, guest_os, guest_host_name,
  guest_ip_address, created_by
) VALUES
  ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', '11111111-1111-1111-1111-111111111111', 'vm-101', '50123456-789a-bcde-f012-3456789abcde', 'db-prod-01', 'poweredOn', 'connected', 8, 32768, '44444444-4444-4444-4444-444444444441', 'Datacenter.vm.Production', 'rhel9_64Guest', 'db-prod-01.corp.local', '10.20.10.15', 'collector'),
  ('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', '11111111-1111-1111-1111-111111111111', 'vm-102', '50223456-789a-bcde-f012-3456789abcde', 'web-prod-01', 'poweredOn', 'connected', 4, 8192, '44444444-4444-4444-4444-444444444441', 'Datacenter.vm.Production', 'rhel9_64Guest', 'web-prod-01.corp.local', '10.20.10.20', 'collector'),
  ('cccccccc-cccc-cccc-cccc-cccccccccccc', '22222222-2222-2222-2222-222222222222', 'vm-201', '50323456-789a-bcde-f012-3456789abcde', 'dev-worker-01', 'poweredOff', 'connected', 2, 4096, NULL, 'Datacenter.vm.Development', 'ubuntu64Guest', 'dev-worker-01.local', '192.168.1.50', 'collector')
ON CONFLICT (instance_uuid) WHERE deleted_at IS NULL DO NOTHING;

-- Insert NICs
INSERT INTO vm_network_interfaces (id, vm_id, device_key, mac_address, ip_address, network_id, vlan_ids)
VALUES
  (gen_random_uuid(), 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 4000, '00:50:56:01:02:03', '10.20.10.15', '55555555-5555-5555-5555-555555555551', '{[100, 200]}'),
  (gen_random_uuid(), 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', 4000, '00:50:56:01:02:04', '10.20.10.20', '55555555-5555-5555-5555-555555555551', '{[100, 200]}')
ON CONFLICT DO NOTHING;

-- Insert Tags & Attributes
INSERT INTO vm_tags (vm_id, tag_id, category_name, tag_name)
VALUES
  ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'urn:vmomi:InventoryServiceTag:prod', 'Environment', 'Production')
ON CONFLICT DO NOTHING;

INSERT INTO vm_custom_attributes (vm_id, name, value)
VALUES
  ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'Owner', 'DBA')
ON CONFLICT DO NOTHING;
EOF

# 7. Start Query API in background
echo ""
echo "==> 7. Starting Query API on :8082..."
export PORT=8082
export LOG_LEVEL=info
export DATABASE_URL="postgres://collector_rw:collector_rw_pass@localhost:5433/sims?sslmode=disable"
export CORS_ALLOWED_ORIGINS="http://localhost:3000"

./bin/query-api &
API_PID=$!
trap 'echo "Stopping Query API (PID $API_PID)..."; kill -TERM $API_PID 2>/dev/null || true' EXIT

# Wait for API readiness
for i in {1..20}; do
    if curl -s http://localhost:8082/readyz | grep -q '"status":"ok"'; then
        echo "Query API is ready!"
        break
    fi
    sleep 0.5
done

# 8. Demonstrate Fast Search Classifier (GET /v1/search/vms?q=...)
echo ""
echo "======================================================================"
echo " DEMO: Fast Search Classifier (GET /v1/search/vms)"
echo "======================================================================"

echo "--> Search by VM Name (q=db-prod-01):"
curl -s "http://localhost:8082/v1/search/vms?q=db-prod-01" | jq .

echo ""
echo "--> Search by IP Address (q=10.20.10.15):"
curl -s "http://localhost:8082/v1/search/vms?q=10.20.10.15" | jq .

echo ""
echo "--> Search by MAC Address (q=00:50:56:01:02:03):"
curl -s "http://localhost:8082/v1/search/vms?q=00:50:56:01:02:03" | jq .

echo ""
echo "--> Search by MoRef (q=vm-101):"
curl -s "http://localhost:8082/v1/search/vms?q=vm-101" | jq .

# 9. Demonstrate Structured AST Search (POST /v1/vms/search)
echo ""
echo "======================================================================"
echo " DEMO: Structured Filter AST Search (POST /v1/vms/search)"
echo "======================================================================"

echo "--> Compound AST: (power_state EQ poweredOn AND folder_path DESCENDANTS_OF Datacenter.vm.Production)"
curl -s -X POST http://localhost:8082/v1/vms/search \
  -H "Content-Type: application/json" \
  -d '{
    "filter": {
      "op": "AND",
      "operands": [
        {"op": "EQ", "field": "power_state", "value": "poweredOn"},
        {"op": "DESCENDANTS_OF_FOLDER", "field": "folder_path", "value": "Datacenter.vm.Production"}
      ]
    }
  }' | jq .

echo ""
echo "--> Metadata Search: HAS_TAG Environment:Production AND HAS_ATTRIBUTE Owner:DBA"
curl -s -X POST http://localhost:8082/v1/vms/search \
  -H "Content-Type: application/json" \
  -d '{
    "filter": {
      "op": "AND",
      "operands": [
        {"op": "HAS_TAG", "category": "Environment", "tag": "Production"},
        {"op": "HAS_ATTRIBUTE", "name": "Owner", "value": "DBA"}
      ]
    }
  }' | jq .

# 10. Demonstrate Facets & Metadata API
echo ""
echo "======================================================================"
echo " DEMO: Inventory Facets (GET /v1/vms/facets)"
echo "======================================================================"
curl -s "http://localhost:8082/v1/vms/facets?fields=power_state,guest_os" | jq .

echo ""
echo "======================================================================"
echo " DEMO: Field Metadata Catalog (GET /v1/meta/vm-fields)"
echo "======================================================================"
curl -s "http://localhost:8082/v1/meta/vm-fields" | jq '.fields | map(.name) | .[0:6]'

# 11. Demonstrate Keyset Cursor Pagination
echo ""
echo "======================================================================"
echo " DEMO: Keyset Pagination with HMAC-Signed Cursor"
echo "======================================================================"
PAGE1=$(curl -s "http://localhost:8082/v1/vms?limit=1")
echo "Page 1 result:"
echo "$PAGE1" | jq .

CURSOR=$(echo "$PAGE1" | jq -r '.pagination.next_cursor')
echo ""
echo "Next Cursor Token: $CURSOR"

if [ -n "$CURSOR" ] && [ "$CURSOR" != "null" ]; then
    echo ""
    echo "Page 2 result using signed cursor:"
    curl -s "http://localhost:8082/v1/vms?cursor=${CURSOR}&limit=1" | jq .
fi

# 12. Demonstrate vCenter Connection Validation & SSRF Guard
echo ""
echo "======================================================================"
echo " DEMO: vCenter SSRF Guard & Connection Validation"
echo "======================================================================"

echo "--> Attempting SSRF connection to loopback 127.0.0.1 (Expected: Rejected with 400 address_not_allowed):"
curl -s -X POST http://localhost:8082/v1/vcenters/test-connection \
  -H "Content-Type: application/json" \
  -d '{
    "host": "127.0.0.1",
    "port": 443,
    "username": "admin",
    "password": "SuperSecretPassword123!",
    "tls": {"verify": false}
  }' | jq .

# 13. Demonstrate Audit Ledger (Zero Secrets)
echo ""
echo "======================================================================"
echo " DEMO: Audit Ledger (Zero Secrets Enforced)"
echo "======================================================================"
curl -s "http://localhost:8082/v1/audit-events" | jq .

echo ""
echo "======================================================================"
echo " [SUCCESS] Sprint 3 Demonstration Complete!"
echo "======================================================================"
