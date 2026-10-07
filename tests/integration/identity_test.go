//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestVMIdentityConstraints(t *testing.T) {
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
		VALUES ('vc-identity-test', 'https://vc-identity-test.local/sdk', 'vc-uuid-test-01')
		ON CONFLICT (api_url) WHERE deleted_at IS NULL DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`).Scan(&vcenterID)
	if err != nil {
		t.Fatalf("failed to insert test vcenter: %v", err)
	}

	defer func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	instanceUUID := "50284e31-8e01-4474-9540-1e149363065a"
	moref := "vm-1001"

	// 2. Insert initial active VM
	var vmID1 string
	err = db.QueryRowContext(ctx, `
		INSERT INTO virtual_machines (vcenter_id, moref, instance_uuid, name)
		VALUES ($1::uuid, $2, $3, 'vm-active-primary')
		RETURNING id::text
	`, vcenterID, moref, instanceUUID).Scan(&vmID1)
	if err != nil {
		t.Fatalf("failed to insert initial active VM: %v", err)
	}

	// 3. Test: Duplicate instance UUID on another active VM (MUST FAIL)
	t.Run("duplicate instance_uuid fails on active VM", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO virtual_machines (vcenter_id, moref, instance_uuid, name)
			VALUES ($1::uuid, 'vm-different-moref', $2, 'vm-duplicate-uuid')
		`, vcenterID, instanceUUID)
		if err == nil {
			t.Errorf("expected unique constraint violation on duplicate instance_uuid, got nil")
		} else if !strings.Contains(err.Error(), "uq_vms_instance_uuid_active") && !strings.Contains(err.Error(), "unique") {
			t.Errorf("expected unique constraint error, got: %v", err)
		}
	})

	// 4. Test: Duplicate MoRef within same vCenter on active VM (MUST FAIL)
	t.Run("duplicate moref fails on active VM in same vcenter", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO virtual_machines (vcenter_id, moref, instance_uuid, name)
			VALUES ($1::uuid, $2, 'different-instance-uuid-999', 'vm-duplicate-moref')
		`, vcenterID, moref)
		if err == nil {
			t.Errorf("expected unique constraint violation on duplicate (vcenter_id, moref), got nil")
		} else if !strings.Contains(err.Error(), "uq_vms_vcenter_moref_active") && !strings.Contains(err.Error(), "unique") {
			t.Errorf("expected unique constraint error, got: %v", err)
		}
	})

	// 5. Test: Soft-deleted identity reuse (MUST SUCCEED)
	t.Run("soft-deleted identity reuse succeeds", func(t *testing.T) {
		// Soft-delete vmID1
		_, err := db.ExecContext(ctx, `
			UPDATE virtual_machines SET deleted_at = NOW() WHERE id = $1::uuid
		`, vmID1)
		if err != nil {
			t.Fatalf("failed to soft-delete vm1: %v", err)
		}

		// Insert new active VM with the recycled moref and new instance UUID
		var recycledVMID string
		err = db.QueryRowContext(ctx, `
			INSERT INTO virtual_machines (vcenter_id, moref, instance_uuid, name)
			VALUES ($1::uuid, $2, 'recycled-instance-uuid-888', 'vm-recycled-moref')
			RETURNING id::text
		`, vcenterID, moref).Scan(&recycledVMID)
		if err != nil {
			t.Errorf("reusing moref after soft-delete should succeed, got error: %v", err)
		}

		// Also verify inserting a new VM with the same instance UUID after soft delete succeeds
		var newVMWithSameUUID string
		err = db.QueryRowContext(ctx, `
			INSERT INTO virtual_machines (vcenter_id, moref, instance_uuid, name)
			VALUES ($1::uuid, 'vm-new-moref-777', $2, 'vm-reused-uuid')
			RETURNING id::text
		`, vcenterID, instanceUUID).Scan(&newVMWithSameUUID)
		if err != nil {
			t.Errorf("reusing instance_uuid after soft-delete should succeed, got error: %v", err)
		}
	})

	// 6. Test: xvMotion identity change (Cross-vCenter migration)
	t.Run("xvMotion identity change succeeds", func(t *testing.T) {
		// Create target vCenter 2
		var vcenterID2 string
		err = db.QueryRowContext(ctx, `
			INSERT INTO vcenters (name, api_url, instance_uuid)
			VALUES ('vc-target-test-2', 'https://vc-target-test-2.local/sdk', 'vc-uuid-test-02')
			ON CONFLICT (api_url) WHERE deleted_at IS NULL DO UPDATE SET updated_at = NOW()
			RETURNING id::text
		`).Scan(&vcenterID2)
		if err != nil {
			t.Fatalf("failed to insert target vcenter: %v", err)
		}
		defer func() {
			_, _ = db.ExecContext(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID2)
		}()

		xvMotionUUID := "xv-motion-instance-uuid-444"
		srcMoref := "vm-src-100"
		dstMoref := "vm-dst-200"

		// Source VM on vCenter 1
		var srcVMID string
		err = db.QueryRowContext(ctx, `
			INSERT INTO virtual_machines (vcenter_id, moref, instance_uuid, name)
			VALUES ($1::uuid, $2, $3, 'vm-migrating-app')
			RETURNING id::text
		`, vcenterID, srcMoref, xvMotionUUID).Scan(&srcVMID)
		if err != nil {
			t.Fatalf("failed to insert source VM: %v", err)
		}

		// Simulate migration: source VM gets marked deleted_at, target VM created on vCenter 2
		_, err = db.ExecContext(ctx, `UPDATE virtual_machines SET deleted_at = NOW() WHERE id = $1::uuid`, srcVMID)
		if err != nil {
			t.Fatalf("failed to soft-delete source VM during migration: %v", err)
		}

		var dstVMID string
		err = db.QueryRowContext(ctx, `
			INSERT INTO virtual_machines (vcenter_id, moref, instance_uuid, name)
			VALUES ($1::uuid, $2, $3, 'vm-migrated-app')
			RETURNING id::text
		`, vcenterID2, dstMoref, xvMotionUUID).Scan(&dstVMID)
		if err != nil {
			t.Fatalf("failed to insert migrated VM on target vCenter: %v", err)
		}

		// Verify both records exist: one historical (deleted), one active
		var activeCount, deletedCount int
		err = db.QueryRowContext(ctx, `
			SELECT 
				COUNT(*) FILTER (WHERE deleted_at IS NULL),
				COUNT(*) FILTER (WHERE deleted_at IS NOT NULL)
			FROM virtual_machines
			WHERE instance_uuid = $1
		`, xvMotionUUID).Scan(&activeCount, &deletedCount)
		if err != nil {
			t.Fatalf("failed to query xvMotion records: %v", err)
		}
		if activeCount != 1 || deletedCount != 1 {
			t.Errorf("expected 1 active and 1 deleted record for xvMotion VM, got active=%d, deleted=%d", activeCount, deletedCount)
		}
	})
}
