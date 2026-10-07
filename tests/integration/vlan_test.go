//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestVLANMultirange(t *testing.T) {
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
		VALUES ('vc-vlan-test', 'https://vc-vlan-test.local/sdk', 'vc-vlan-uuid-01')
		ON CONFLICT (api_url) WHERE deleted_at IS NULL DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`).Scan(&vcenterID)
	if err != nil {
		t.Fatalf("failed to insert test vcenter: %v", err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	// 2. Test: Equivalent inputs normalize identically in PostgreSQL
	t.Run("equivalent VLAN inputs normalize identically in Postgres", func(t *testing.T) {
		// Input 1: "100-110,200,300-305"
		// Input 2: adjacent ranges "100-105, 106-110, 200, 300-305"
		// Input 3: out-of-order "300-305, 200, 100-110"
		var norm1, norm2, norm3 string
		err = db.QueryRowContext(ctx, "SELECT '{[100,110],[200,200],[300,305]}'::int4multirange::text").Scan(&norm1)
		if err != nil {
			t.Fatalf("failed to parse input 1: %v", err)
		}

		err = db.QueryRowContext(ctx, "SELECT '{[100,105],[106,110],[200,200],[300,305]}'::int4multirange::text").Scan(&norm2)
		if err != nil {
			t.Fatalf("failed to parse input 2: %v", err)
		}

		err = db.QueryRowContext(ctx, "SELECT '{[300,305],[200,200],[100,110]}'::int4multirange::text").Scan(&norm3)
		if err != nil {
			t.Fatalf("failed to parse input 3: %v", err)
		}

		if norm1 != norm2 || norm1 != norm3 {
			t.Errorf("expected canonical representations to match: norm1=%q, norm2=%q, norm3=%q", norm1, norm2, norm3)
		}
		expectedCanonical := "{[100,111),[200,201),[300,306)}"
		if norm1 != expectedCanonical {
			t.Errorf("expected %q, got %q", expectedCanonical, norm1)
		}
	})

	// 3. Test: Portgroup insertions with Single VLAN, Trunk range, and Multiple ranges
	t.Run("portgroup VLAN insertions and queries", func(t *testing.T) {
		// Single VLAN (100)
		_, err := db.ExecContext(ctx, `
			INSERT INTO portgroups (vcenter_id, moref, name, vlan_ids)
			VALUES ($1::uuid, 'pg-100', 'VLAN_100', '{[100,100]}'::int4multirange)
		`, vcenterID)
		if err != nil {
			t.Fatalf("failed to insert single VLAN portgroup: %v", err)
		}

		// Range (100-110)
		_, err = db.ExecContext(ctx, `
			INSERT INTO portgroups (vcenter_id, moref, name, vlan_ids)
			VALUES ($1::uuid, 'pg-range', 'VLAN_100_110', '{[100,110]}'::int4multirange)
		`, vcenterID)
		if err != nil {
			t.Fatalf("failed to insert range VLAN portgroup: %v", err)
		}

		// Trunk range (1-4094)
		_, err = db.ExecContext(ctx, `
			INSERT INTO portgroups (vcenter_id, moref, name, vlan_ids)
			VALUES ($1::uuid, 'pg-trunk', 'VLAN_Trunk', '{[1,4094]}'::int4multirange)
		`, vcenterID)
		if err != nil {
			t.Fatalf("failed to insert trunk VLAN portgroup: %v", err)
		}

		// Multiple ranges (100-110, 200, 300-305)
		_, err = db.ExecContext(ctx, `
			INSERT INTO portgroups (vcenter_id, moref, name, vlan_ids)
			VALUES ($1::uuid, 'pg-multi', 'VLAN_Multi', '{[100,110],[200,200],[300,305]}'::int4multirange)
		`, vcenterID)
		if err != nil {
			t.Fatalf("failed to insert multi VLAN portgroup: %v", err)
		}

		// Test Contains (@>): Search for portgroups containing VLAN 105
		rows, err := db.QueryContext(ctx, `
			SELECT moref FROM portgroups
			WHERE vcenter_id = $1::uuid AND vlan_ids @> 105
			ORDER BY moref ASC
		`, vcenterID)
		if err != nil {
			t.Fatalf("contains query failed: %v", err)
		}
		defer rows.Close()

		var containing []string
		for rows.Next() {
			var moref string
			if err := rows.Scan(&moref); err != nil {
				t.Fatalf("failed to scan moref: %v", err)
			}
			containing = append(containing, moref)
		}
		// Expect pg-range, pg-trunk, pg-multi (but NOT pg-100)
		expectedContaining := []string{"pg-multi", "pg-range", "pg-trunk"}
		if len(containing) != len(expectedContaining) {
			t.Fatalf("expected containing %v, got %v", expectedContaining, containing)
		}
		for i, m := range containing {
			if m != expectedContaining[i] {
				t.Errorf("containing[%d]: expected %q, got %q", i, expectedContaining[i], m)
			}
		}

		// Test Overlap (&&): Search for portgroups overlapping range 108-115
		rowsOverlap, err := db.QueryContext(ctx, `
			SELECT moref FROM portgroups
			WHERE vcenter_id = $1::uuid AND vlan_ids && '{[108,115]}'::int4multirange
			ORDER BY moref ASC
		`, vcenterID)
		if err != nil {
			t.Fatalf("overlap query failed: %v", err)
		}
		defer rowsOverlap.Close()

		var overlapping []string
		for rowsOverlap.Next() {
			var moref string
			if err := rowsOverlap.Scan(&moref); err != nil {
				t.Fatalf("failed to scan moref: %v", err)
			}
			overlapping = append(overlapping, moref)
		}
		// Expect pg-multi, pg-range, pg-trunk
		if len(overlapping) != len(expectedContaining) {
			t.Fatalf("expected overlapping %v, got %v", expectedContaining, overlapping)
		}
	})
}
