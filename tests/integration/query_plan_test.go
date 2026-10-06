//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type ExplainNode struct {
	NodeType  string        `json:"Node Type"`
	Relation  string        `json:"Relation Name"`
	IndexName string        `json:"Index Name"`
	Plans     []ExplainNode `json:"Plans"`
}

type ExplainPlanWrapper struct {
	Plan          ExplainNode `json:"Plan"`
	PlanningTime  float64     `json:"Planning Time"`
	ExecutionTime float64     `json:"Execution Time"`
}

func collectNodeTypes(node ExplainNode) []string {
	var types []string
	types = append(types, node.NodeType)
	for _, child := range node.Plans {
		types = append(types, collectNodeTypes(child)...)
	}
	return types
}

func TestQueryPlanRegression(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	connStr := getConnString("collector_rw", "collector_rw_pass")
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer db.Close()

	// 1. Ensure vCenter exists
	var vcenterID string
	err = db.QueryRowContext(ctx, `
		INSERT INTO vcenters (name, api_url, instance_uuid)
		VALUES ('vc-query-plan', 'https://vc-query-plan.local/sdk', 'uuid-query-plan-vc')
		ON CONFLICT (api_url) DO UPDATE SET name = EXCLUDED.name
		RETURNING id::text
	`).Scan(&vcenterID)
	if err != nil {
		t.Fatalf("failed to ensure vcenter: %v", err)
	}

	// 2. Populate synthetic data (2,500 VMs with varied names, IPs, hostnames, and folder paths)
	t.Log("Seeding synthetic inventory for query plan regression gate...")
	_, err = db.ExecContext(ctx, `
		INSERT INTO virtual_machines (
			vcenter_id, moref, instance_uuid, name, power_state, connection_state,
			cpu_cores, memory_mb, folder_path, guest_os, guest_host_name, guest_ip_address
		)
		SELECT
			$1::uuid,
			'vm-' || i::text,
			'uuid-plan-' || lpad(i::text, 6, '0'),
			'plan-vm-' || lpad(i::text, 6, '0'),
			CASE WHEN i % 2 = 0 THEN 'poweredOn' ELSE 'poweredOff' END,
			'connected',
			(i % 8) + 1,
			((i % 16) + 1) * 1024,
			('Datacenters.DC1.vm.Folder_' || ((i % 10) + 1)::text)::ltree,
			'rhel9_64Guest',
			'host-' || lpad(i::text, 6, '0') || '.corp.local',
			('10.20.' || ((i / 256) % 256)::text || '.' || (i % 256)::text)::inet
		FROM generate_series(1, 2500) AS s(i)
		ON CONFLICT (instance_uuid) WHERE deleted_at IS NULL DO NOTHING;
	`, vcenterID)
	if err != nil {
		t.Fatalf("failed to seed virtual_machines: %v", err)
	}

	// 3. Run ANALYZE to guarantee up-to-date planner statistics
	_, err = db.ExecContext(ctx, `ANALYZE virtual_machines;`)
	if err != nil {
		t.Fatalf("ANALYZE failed: %v", err)
	}

	// Test cases targeting hot indexed paths
	queries := []struct {
		name         string
		sql          string
		targetRel    string
		disallowScan string
		requireScan  []string
	}{
		{
			name: "MoRef lookup B-Tree index scan",
			sql: `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
				SELECT * FROM virtual_machines vm
				WHERE vm.vcenter_id = '` + vcenterID + `'::uuid AND vm.moref = 'vm-42' AND vm.deleted_at IS NULL;`,
			targetRel:    "virtual_machines",
			disallowScan: "Seq Scan",
			requireScan:  []string{"Index Scan", "Index Only Scan", "Bitmap Index Scan"},
		},
		{
			name: "Instance UUID lookup unique index scan",
			sql: `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
				SELECT * FROM virtual_machines vm
				WHERE vm.instance_uuid = 'uuid-plan-000042' AND vm.deleted_at IS NULL;`,
			targetRel:    "virtual_machines",
			disallowScan: "Seq Scan",
			requireScan:  []string{"Index Scan", "Index Only Scan", "Bitmap Index Scan"},
		},
		{
			name: "Hostname exact lookup B-Tree index scan",
			sql: `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
				SELECT * FROM virtual_machines vm
				WHERE vm.guest_host_name = 'host-000042.corp.local' AND vm.deleted_at IS NULL;`,
			targetRel:    "virtual_machines",
			disallowScan: "Seq Scan",
			requireScan:  []string{"Index Scan", "Index Only Scan", "Bitmap Index Scan"},
		},
		{
			name: "Trigram substring match GIN index scan",
			sql: `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
				SELECT * FROM virtual_machines vm
				WHERE vm.name ILIKE '%plan-vm-000042%' AND vm.deleted_at IS NULL;`,
			targetRel:    "virtual_machines",
			disallowScan: "Seq Scan",
			requireScan:  []string{"Bitmap Index Scan", "Bitmap Heap Scan", "Index Scan"},
		},
		{
			name: "Folder hierarchy descent GiST ltree index scan",
			sql: `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
				SELECT * FROM virtual_machines vm
				WHERE vm.folder_path <@ 'Datacenters.DC1.vm.Folder_1'::ltree AND vm.deleted_at IS NULL;`,
			targetRel:    "virtual_machines",
			disallowScan: "Seq Scan",
			requireScan:  []string{"Bitmap Index Scan", "Bitmap Heap Scan", "Index Scan"},
		},
		{
			name: "IP CIDR containment GiST inet index scan",
			sql: `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
				SELECT * FROM virtual_machines vm
				WHERE vm.guest_ip_address <<= '10.20.0.0/24'::inet AND vm.deleted_at IS NULL;`,
			targetRel:    "virtual_machines",
			disallowScan: "Seq Scan",
			requireScan:  []string{"Bitmap Index Scan", "Bitmap Heap Scan", "Index Scan"},
		},
		{
			name: "Keyset pagination (name, id) seek index scan",
			sql: `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
				SELECT * FROM virtual_machines vm
				WHERE vm.deleted_at IS NULL AND (vm.name, vm.id) > ('plan-vm-000100', '00000000-0000-0000-0000-000000000000'::uuid)
				ORDER BY vm.name ASC, vm.id ASC
				LIMIT 20;`,
			targetRel:    "virtual_machines",
			disallowScan: "Seq Scan",
			requireScan:  []string{"Index Scan", "Index Only Scan", "Bitmap Index Scan", "Bitmap Heap Scan"},
		},
	}

	for _, tc := range queries {
		t.Run(tc.name, func(t *testing.T) {
			var rawJSON string
			err := db.QueryRowContext(ctx, tc.sql).Scan(&rawJSON)
			if err != nil {
				t.Fatalf("failed to execute explain query: %v", err)
			}

			var wrappers []ExplainPlanWrapper
			if err := json.Unmarshal([]byte(rawJSON), &wrappers); err != nil {
				t.Fatalf("failed to parse explain json output: %v\nRaw: %s", err, rawJSON)
			}
			if len(wrappers) == 0 {
				t.Fatalf("empty explain wrappers returned")
			}

			plan := wrappers[0]
			nodeTypes := collectNodeTypes(plan.Plan)
			t.Logf("Query [%s] plan: planning=%.3fms, execution=%.3fms, nodeTypes=%v",
				tc.name, plan.PlanningTime, plan.ExecutionTime, nodeTypes)

			// Assert no sequential scan on hot filter path
			for _, nt := range nodeTypes {
				if nt == tc.disallowScan {
					t.Fatalf("regression detected: query [%s] used %s on %s (plan:\n%s)",
						tc.name, tc.disallowScan, tc.targetRel, rawJSON)
				}
			}

			// Assert at least one required index scan type is present
			hasRequired := false
			for _, nt := range nodeTypes {
				for _, req := range tc.requireScan {
					if strings.Contains(nt, req) {
						hasRequired = true
						break
					}
				}
				if hasRequired {
					break
				}
			}
			if !hasRequired {
				t.Fatalf("expected at least one index scan %v, but got node types %v", tc.requireScan, nodeTypes)
			}
		})
	}
}
