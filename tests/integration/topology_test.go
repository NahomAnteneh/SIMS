//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestTopologyRenameStability(t *testing.T) {
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
		VALUES ('vc-topology-test', 'https://vc-topology-test.local/sdk', 'vc-topo-uuid-01')
		ON CONFLICT (api_url) WHERE deleted_at IS NULL DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`).Scan(&vcenterID)
	if err != nil {
		t.Fatalf("failed to insert test vcenter: %v", err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	// 2. Insert Datastore and Network with stable UUIDs
	var dsID, netID string
	err = db.QueryRowContext(ctx, `
		INSERT INTO datastores (vcenter_id, moref, name, type, capacity_bytes, free_bytes)
		VALUES ($1::uuid, 'datastore-10', 'vsanDatastore_Original', 'vsan', 1000000000, 500000000)
		RETURNING id::text
	`, vcenterID).Scan(&dsID)
	if err != nil {
		t.Fatalf("failed to insert datastore: %v", err)
	}

	err = db.QueryRowContext(ctx, `
		INSERT INTO networks (vcenter_id, moref, name, vlan_ids)
		VALUES ($1::uuid, 'network-20', 'VM_Network_Original', '{[100,110]}'::int4multirange)
		RETURNING id::text
	`, vcenterID).Scan(&netID)
	if err != nil {
		t.Fatalf("failed to insert network: %v", err)
	}

	// 3. Attach 50 simulated VMs to this Datastore and Network
	const vmCount = 50
	vmIDs := make([]string, vmCount)
	for i := 0; i < vmCount; i++ {
		var vmID string
		err = db.QueryRowContext(ctx, `
			INSERT INTO virtual_machines (vcenter_id, moref, instance_uuid, name)
			VALUES ($1::uuid, $2, $3, $4)
			RETURNING id::text
		`, vcenterID, fmt.Sprintf("vm-topo-%d", i), fmt.Sprintf("uuid-topo-%d", i), fmt.Sprintf("test-vm-%d", i)).Scan(&vmID)
		if err != nil {
			t.Fatalf("failed to insert VM %d: %v", i, err)
		}
		vmIDs[i] = vmID

		// Attach to datastore
		_, err = db.ExecContext(ctx, `INSERT INTO vm_datastores (vm_id, datastore_id) VALUES ($1::uuid, $2::uuid)`, vmID, dsID)
		if err != nil {
			t.Fatalf("failed to attach datastore to VM %d: %v", i, err)
		}

		// Attach to network
		_, err = db.ExecContext(ctx, `
			INSERT INTO vm_network_interfaces (vm_id, device_key, mac_address, network_id)
			VALUES ($1::uuid, 4000, $2, $3::uuid)
		`, vmID, fmt.Sprintf("00:50:56:a1:00:%02x", i), netID)
		if err != nil {
			t.Fatalf("failed to attach network interface to VM %d: %v", i, err)
		}
	}

	// 4. Test: Rename datastore updates exactly ONE row in the datastores table
	t.Run("datastore rename updates exactly one row", func(t *testing.T) {
		res, err := db.ExecContext(ctx, `
			UPDATE datastores SET name = 'vsanDatastore_Renamed' WHERE id = $1::uuid
		`, dsID)
		if err != nil {
			t.Fatalf("failed to rename datastore: %v", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			t.Fatalf("failed to get rows affected: %v", err)
		}
		if affected != 1 {
			t.Fatalf("expected exactly 1 row affected by datastore rename, got %d", affected)
		}

		// Verify all 50 VM attachments remain valid without modification
		var attachedCount int
		err = db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM vm_datastores WHERE datastore_id = $1::uuid
		`, dsID).Scan(&attachedCount)
		if err != nil {
			t.Fatalf("failed to verify datastore attachments: %v", err)
		}
		if attachedCount != vmCount {
			t.Errorf("expected %d VM datastore attachments, got %d", vmCount, attachedCount)
		}
	})

	// 5. Test: Rename network updates exactly ONE row in the networks table
	t.Run("network rename updates exactly one row", func(t *testing.T) {
		res, err := db.ExecContext(ctx, `
			UPDATE networks SET name = 'VM_Network_Renamed' WHERE id = $1::uuid
		`, netID)
		if err != nil {
			t.Fatalf("failed to rename network: %v", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			t.Fatalf("failed to get rows affected: %v", err)
		}
		if affected != 1 {
			t.Fatalf("expected exactly 1 row affected by network rename, got %d", affected)
		}

		// Verify all 50 NIC attachments remain valid without modification
		var nicCount int
		err = db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM vm_network_interfaces WHERE network_id = $1::uuid
		`, netID).Scan(&nicCount)
		if err != nil {
			t.Fatalf("failed to verify network attachments: %v", err)
		}
		if nicCount != vmCount {
			t.Errorf("expected %d NIC network attachments, got %d", vmCount, nicCount)
		}
	})

	// 6. Test: Folder ltree path hierarchy queries
	t.Run("folder ltree hierarchy queries", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO inventory_folders (vcenter_id, moref, name, folder_type, path)
			VALUES 
				($1::uuid, 'group-v10', 'Datacenter1', 'datacenter', 'Datacenter1'::ltree),
				($1::uuid, 'group-v11', 'vm', 'vm', 'Datacenter1.vm'::ltree),
				($1::uuid, 'group-v12', 'Engineering', 'vm', 'Datacenter1.vm.Engineering'::ltree),
				($1::uuid, 'group-v13', 'Production', 'vm', 'Datacenter1.vm.Engineering.Production'::ltree),
				($1::uuid, 'group-v14', 'Staging', 'vm', 'Datacenter1.vm.Engineering.Staging'::ltree),
				($1::uuid, 'group-v15', 'Finance', 'vm', 'Datacenter1.vm.Finance'::ltree)
		`, vcenterID)
		if err != nil {
			t.Fatalf("failed to insert folders: %v", err)
		}

		// Query all folders under Engineering (<@ 'Datacenter1.vm.Engineering')
		rows, err := db.QueryContext(ctx, `
			SELECT name FROM inventory_folders
			WHERE path <@ 'Datacenter1.vm.Engineering'::ltree
			ORDER BY path ASC
		`)
		if err != nil {
			t.Fatalf("failed to query ltree subtree: %v", err)
		}
		defer rows.Close()

		var folderNames []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatalf("failed to scan folder name: %v", err)
			}
			folderNames = append(folderNames, name)
		}

		expected := []string{"Engineering", "Production", "Staging"}
		if len(folderNames) != len(expected) {
			t.Fatalf("expected folders %v, got %v", expected, folderNames)
		}
		for i, n := range folderNames {
			if n != expected[i] {
				t.Errorf("folder[%d]: expected %q, got %q", i, expected[i], n)
			}
		}
	})
}
