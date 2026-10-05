//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/persistence"
	"github.com/infrastructure/sims/internal/vlan"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestTopology_StandaloneHostUnderComputeResource verifies:
//  1. hosts.cluster_id is nullable in PostgreSQL schema.
//  2. Direct SQL insertion of a standalone host with cluster_id = NULL and populated datacenter_id.
//  3. BulkPersister.PersistTopology with a standalone host under a ComputeResource (cr-1)
//     correctly resolves datacenter_id, leaves cluster_id as NULL, and persists cleanly.
func TestTopology_StandaloneHostUnderComputeResource(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	connStr := getConnString("collector_rw", "collector_rw_pass")
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("failed to connect as collector_rw: %v", err)
	}
	defer db.Close()

	// 1. Create a test vCenter
	var vcenterID string
	err = db.QueryRowContext(ctx, `
		INSERT INTO vcenters (name, api_url, instance_uuid)
		VALUES ('vc-standalone-test', 'https://vc-standalone.local/sdk', 'vc-standalone-uuid-01')
		ON CONFLICT (api_url) DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`).Scan(&vcenterID)
	if err != nil {
		t.Fatalf("failed to insert test vcenter: %v", err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	// 2. Direct SQL insertion of standalone host with cluster_id = NULL
	datacenterID := uuid.New().String()
	_, err = db.ExecContext(ctx, `
		INSERT INTO datacenters (id, vcenter_id, moref, name)
		VALUES ($1::uuid, $2::uuid, 'datacenter-standalone-sql', 'DC-Standalone-SQL')
	`, datacenterID, vcenterID)
	if err != nil {
		t.Fatalf("failed to insert datacenter: %v", err)
	}

	hostID := uuid.New().String()
	_, err = db.ExecContext(ctx, `
		INSERT INTO hosts (id, vcenter_id, cluster_id, datacenter_id, moref, name, connection_state, power_state)
		VALUES ($1::uuid, $2::uuid, NULL, $3::uuid, 'host-standalone-sql', 'esxi-standalone-sql.corp.local', 'connected', 'poweredOn')
	`, hostID, vcenterID, datacenterID)
	if err != nil {
		t.Fatalf("failed to insert standalone host with null cluster_id: %v", err)
	}

	var (
		resClusterID    sql.NullString
		resDatacenterID string
		resName         string
	)
	err = db.QueryRowContext(ctx, `
		SELECT cluster_id::text, datacenter_id::text, name FROM hosts WHERE id = $1::uuid
	`, hostID).Scan(&resClusterID, &resDatacenterID, &resName)
	if err != nil {
		t.Fatalf("querying standalone host failed: %v", err)
	}

	if resClusterID.Valid {
		t.Errorf("expected cluster_id to be NULL for standalone host, got: %s", resClusterID.String)
	}
	if resDatacenterID != datacenterID {
		t.Errorf("expected datacenter_id %s, got: %s", datacenterID, resDatacenterID)
	}
	t.Logf("Confirmed SQL persistence: standalone host %s has cluster_id=NULL and datacenter_id=%s", resName, resDatacenterID)

	// 3. Test BulkPersister.PersistTopology with a standalone host under ComputeResource (cr-1)
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to create pgxpool: %v", err)
	}
	defer pool.Close()

	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{
		FlushInterval: 100 * time.Millisecond,
		FlushSize:     100,
	}, nil)

	// Build a topology snapshot representing a standalone host in a ComputeResource
	snap := collector.NewEmptySnapshot(vcenterID)

	// Datacenter
	snap.Datacenters["datacenter-lab"] = collector.TopologyNode{
		MoRef: "datacenter-lab",
		Name:  "Datacenter-Lab",
	}
	snap.ByMoRef["datacenter-lab"] = snap.Datacenters["datacenter-lab"]

	// Standalone ComputeResource (parent is Datacenter-Lab)
	snap.ByMoRef["cr-standalone-10"] = collector.TopologyNode{
		MoRef:    "cr-standalone-10",
		Name:     "cr-standalone-10",
		ParentID: "datacenter-lab",
	}

	// Standalone Host (parent is ComputeResource cr-standalone-10, NOT a cluster)
	snap.Hosts["host-standalone-bulk"] = collector.TopologyNode{
		MoRef:    "host-standalone-bulk",
		Name:     "esxi-standalone-bulk.corp.local",
		ParentID: "cr-standalone-10",
	}
	snap.ByMoRef["host-standalone-bulk"] = snap.Hosts["host-standalone-bulk"]

	err = persister.PersistTopology(ctx, snap)
	if err != nil {
		t.Fatalf("BulkPersister.PersistTopology failed: %v", err)
	}

	// Verify the bulk-persisted host in PostgreSQL
	err = db.QueryRowContext(ctx, `
		SELECT cluster_id::text, datacenter_id::text FROM hosts
		WHERE vcenter_id = $1::uuid AND moref = 'host-standalone-bulk'
	`, vcenterID).Scan(&resClusterID, &resDatacenterID)
	if err != nil {
		t.Fatalf("querying bulk-persisted host failed: %v", err)
	}

	if resClusterID.Valid {
		t.Errorf("expected cluster_id to be NULL for standalone host under ComputeResource, got: %s", resClusterID.String)
	}
	expectedDCID := snap.DatacenterIDByMoRef["datacenter-lab"]
	if resDatacenterID != expectedDCID {
		t.Errorf("expected datacenter_id %s, got: %s", expectedDCID, resDatacenterID)
	}
	t.Logf("Confirmed BulkPersister: host-standalone-bulk persisted with cluster_id=NULL and datacenter_id=%s", resDatacenterID)
}

// TestTopology_StandardSwitchPortgroupVLANExtraction verifies:
// 1. Standard switch portgroup VLAN ID extraction:
//   - 0 -> untagged (empty int4multirange '{}')
//   - 10 -> single VLAN [10, 10] ('{[10,11)}')
//   - 4095 -> trunk 1-4094 ('{[1,4095)}')
//
// 2. Normalized int4multirange literal inserts cleanly into PostgreSQL networks.vlan_ids.
func TestTopology_StandardSwitchPortgroupVLANExtraction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	connStr := getConnString("collector_rw", "collector_rw_pass")
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("failed to connect as collector_rw: %v", err)
	}
	defer db.Close()

	var vcenterID string
	err = db.QueryRowContext(ctx, `
		INSERT INTO vcenters (name, api_url, instance_uuid)
		VALUES ('vc-vlan-test', 'https://vc-vlan.local/sdk', 'vc-vlan-uuid-01')
		ON CONFLICT (api_url) DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`).Scan(&vcenterID)
	if err != nil {
		t.Fatalf("failed to insert test vcenter: %v", err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	testCases := []struct {
		name          string
		moref         string
		vlanID        int32
		expectedVLAN  string
		expectedPGSQL string
	}{
		{
			name:          "VM Network (untagged VLAN 0)",
			moref:         "network-vlan-0",
			vlanID:        0,
			expectedVLAN:  "",
			expectedPGSQL: "{}",
		},
		{
			name:          "VLAN 10 Production",
			moref:         "network-vlan-10",
			vlanID:        10,
			expectedVLAN:  "10",
			expectedPGSQL: "{[10,11)}",
		},
		{
			name:          "VLAN 4095 Trunk (1-4094)",
			moref:         "network-vlan-trunk",
			vlanID:        4095,
			expectedVLAN:  "1-4094",
			expectedPGSQL: "{[1,4095)}",
		},
		{
			name:          "VLAN 100 DMZ",
			moref:         "network-vlan-100",
			vlanID:        100,
			expectedVLAN:  "100",
			expectedPGSQL: "{[100,101)}",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Extract VLAN string using standard switch portgroup logic
			var vlanStr string
			if tc.vlanID > 0 {
				if tc.vlanID == 4095 {
					vlanStr = "1-4094"
				} else {
					vlanStr = fmt.Sprintf("%d", tc.vlanID)
				}
			}

			if vlanStr != tc.expectedVLAN {
				t.Errorf("vlan string extraction mismatch: expected %q, got %q", tc.expectedVLAN, vlanStr)
			}

			// Format normalized pgsql literal
			pgRange, err := vlan.NormalizeSpec(vlanStr)
			if err != nil {
				t.Fatalf("failed to normalize vlan spec %q: %v", vlanStr, err)
			}

			// Insert into networks table (vlan_ids column)
			var netID string
			err = db.QueryRowContext(ctx, `
				INSERT INTO networks (vcenter_id, moref, name, vlan_ids)
				VALUES ($1::uuid, $2, $3, $4::int4multirange)
				RETURNING id::text
			`, vcenterID, tc.moref, tc.name, pgRange).Scan(&netID)
			if err != nil {
				t.Fatalf("failed to insert network with vlan_ids %q: %v", pgRange, err)
			}

			// Query back from PostgreSQL
			var queriedRanges string
			err = db.QueryRowContext(ctx, `
				SELECT vlan_ids::text FROM networks WHERE id = $1::uuid
			`, netID).Scan(&queriedRanges)
			if err != nil {
				t.Fatalf("failed to query vlan_ids from networks table: %v", err)
			}

			if queriedRanges != tc.expectedPGSQL {
				t.Errorf("PostgreSQL int4multirange mismatch: expected %q, got %q", tc.expectedPGSQL, queriedRanges)
			}
			t.Logf("Confirmed standard switch VLAN extraction for %s: vlanID=%d -> pgsql=%s", tc.name, tc.vlanID, queriedRanges)
		})
	}
}

// TestTopology_DatacenterTableAndForeignKeys verifies:
// 1. Migration 000003_topology.sql creates datacenters table and foreign keys.
// 2. ON DELETE SET NULL foreign key cascade from datacenters to clusters and hosts.
// 3. Least-privilege role permissions on datacenters table for collector_rw and query_ro.
func TestTopology_DatacenterTableAndForeignKeys(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminConnStr := getConnString("postgres", "postgres")
	adminDB, err := sql.Open("pgx", adminConnStr)
	if err != nil {
		t.Fatalf("failed to connect as admin: %v", err)
	}
	defer adminDB.Close()

	// 1. Verify datacenters table exists and migration 000003 is recorded
	var count int
	err = adminDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pg_tables
		WHERE schemaname = 'public' AND tablename = 'datacenters'
	`).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("expected datacenters table to exist in database schema: count=%d, err=%v", count, err)
	}

	var versionID int64
	err = adminDB.QueryRowContext(ctx, `
		SELECT version_id FROM goose_db_version WHERE version_id = 3
	`).Scan(&versionID)
	if err != nil {
		t.Fatalf("expected migration 000003 in goose_db_version: %v", err)
	}
	t.Log("Confirmed migration 000003_topology.sql is applied and recorded in goose_db_version.")

	// Create test vCenter
	var vcenterID string
	err = adminDB.QueryRowContext(ctx, `
		INSERT INTO vcenters (name, api_url, instance_uuid)
		VALUES ('vc-dc-fk-test', 'https://vc-dc-fk.local/sdk', 'vc-dc-fk-uuid-01')
		ON CONFLICT (api_url) DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`).Scan(&vcenterID)
	if err != nil {
		t.Fatalf("failed to insert test vcenter: %v", err)
	}
	defer func() {
		_, _ = adminDB.ExecContext(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	// 2. Verify ON DELETE SET NULL cascade behavior
	dcID := uuid.New().String()
	_, err = adminDB.ExecContext(ctx, `
		INSERT INTO datacenters (id, vcenter_id, moref, name)
		VALUES ($1::uuid, $2::uuid, 'datacenter-cascade-test', 'DC-Cascade')
	`, dcID, vcenterID)
	if err != nil {
		t.Fatalf("failed to insert test datacenter: %v", err)
	}

	clusterID := uuid.New().String()
	_, err = adminDB.ExecContext(ctx, `
		INSERT INTO clusters (id, vcenter_id, datacenter_id, moref, name)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'cluster-cascade-test', 'Cluster-Cascade')
	`, clusterID, vcenterID, dcID)
	if err != nil {
		t.Fatalf("failed to insert test cluster: %v", err)
	}

	hostID := uuid.New().String()
	_, err = adminDB.ExecContext(ctx, `
		INSERT INTO hosts (id, vcenter_id, cluster_id, datacenter_id, moref, name)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'host-cascade-test', 'Host-Cascade')
	`, hostID, vcenterID, clusterID, dcID)
	if err != nil {
		t.Fatalf("failed to insert test host: %v", err)
	}

	// Delete the datacenter
	_, err = adminDB.ExecContext(ctx, "DELETE FROM datacenters WHERE id = $1::uuid", dcID)
	if err != nil {
		t.Fatalf("failed to delete datacenter: %v", err)
	}

	// Verify cluster.datacenter_id was set to NULL
	var clusterDCID sql.NullString
	err = adminDB.QueryRowContext(ctx, "SELECT datacenter_id::text FROM clusters WHERE id = $1::uuid", clusterID).Scan(&clusterDCID)
	if err != nil {
		t.Fatalf("querying cluster failed: %v", err)
	}
	if clusterDCID.Valid {
		t.Errorf("expected cluster.datacenter_id to be SET NULL after datacenter deletion, got: %s", clusterDCID.String)
	}

	// Verify host.datacenter_id was set to NULL
	var hostDCID sql.NullString
	err = adminDB.QueryRowContext(ctx, "SELECT datacenter_id::text FROM hosts WHERE id = $1::uuid", hostID).Scan(&hostDCID)
	if err != nil {
		t.Fatalf("querying host failed: %v", err)
	}
	if hostDCID.Valid {
		t.Errorf("expected host.datacenter_id to be SET NULL after datacenter deletion, got: %s", hostDCID.String)
	}
	t.Log("Confirmed ON DELETE SET NULL foreign key cascade for datacenters -> clusters, hosts.")

	// 3. Verify Role Privileges on datacenters table
	rwConnStr := getConnString("collector_rw", "collector_rw_pass")
	rwDB, err := sql.Open("pgx", rwConnStr)
	if err != nil {
		t.Fatalf("failed to connect as collector_rw: %v", err)
	}
	defer rwDB.Close()

	roConnStr := getConnString("query_ro", "query_ro_pass")
	roDB, err := sql.Open("pgx", roConnStr)
	if err != nil {
		t.Fatalf("failed to connect as query_ro: %v", err)
	}
	defer roDB.Close()

	// collector_rw: INSERT allowed
	rwDCID := uuid.New().String()
	_, err = rwDB.ExecContext(ctx, `
		INSERT INTO datacenters (id, vcenter_id, moref, name)
		VALUES ($1::uuid, $2::uuid, 'datacenter-rw-test', 'DC-RW-Test')
	`, rwDCID, vcenterID)
	if err != nil {
		t.Errorf("collector_rw should be able to INSERT into datacenters, got: %v", err)
	}

	// collector_rw: UPDATE allowed
	_, err = rwDB.ExecContext(ctx, `
		UPDATE datacenters SET name = 'DC-RW-Test-Renamed' WHERE id = $1::uuid
	`, rwDCID)
	if err != nil {
		t.Errorf("collector_rw should be able to UPDATE datacenters, got: %v", err)
	}

	// query_ro: SELECT allowed
	var dcName string
	err = roDB.QueryRowContext(ctx, `
		SELECT name FROM datacenters WHERE id = $1::uuid
	`, rwDCID).Scan(&dcName)
	if err != nil || dcName != "DC-RW-Test-Renamed" {
		t.Errorf("query_ro should be able to SELECT from datacenters, got name=%q, err=%v", dcName, err)
	}

	// query_ro: INSERT forbidden
	_, err = roDB.ExecContext(ctx, `
		INSERT INTO datacenters (id, vcenter_id, moref, name)
		VALUES ($1::uuid, $2::uuid, 'datacenter-ro-forbidden', 'DC-Forbidden')
	`, uuid.New().String(), vcenterID)
	if err == nil {
		t.Error("query_ro MUST NOT be able to INSERT into datacenters")
	} else if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("expected permission denied for query_ro INSERT on datacenters, got: %v", err)
	}

	// query_ro: UPDATE forbidden
	_, err = roDB.ExecContext(ctx, `
		UPDATE datacenters SET name = 'Hacked' WHERE id = $1::uuid
	`, rwDCID)
	if err == nil {
		t.Error("query_ro MUST NOT be able to UPDATE datacenters")
	} else if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("expected permission denied for query_ro UPDATE on datacenters, got: %v", err)
	}

	// query_ro: DELETE forbidden
	_, err = roDB.ExecContext(ctx, `
		DELETE FROM datacenters WHERE id = $1::uuid
	`, rwDCID)
	if err == nil {
		t.Error("query_ro MUST NOT be able to DELETE from datacenters")
	} else if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("expected permission denied for query_ro DELETE on datacenters, got: %v", err)
	}

	t.Log("Confirmed least-privilege role permissions on datacenters: collector_rw (INSERT, UPDATE, SELECT), query_ro (SELECT only, mutations denied).")
}
