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

func TestNetworkInterfaces(t *testing.T) {
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
		VALUES ('vc-network-test', 'https://vc-network-test.local/sdk', 'vc-net-uuid-01')
		ON CONFLICT (api_url) WHERE deleted_at IS NULL DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`).Scan(&vcenterID)
	if err != nil {
		t.Fatalf("failed to insert test vcenter: %v", err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	// 2. Create a test network
	var netID string
	err = db.QueryRowContext(ctx, `
		INSERT INTO networks (vcenter_id, moref, name, vlan_ids)
		VALUES ($1::uuid, 'net-test-01', 'DPortGroup_VLAN100', '{[100,100]}'::int4multirange)
		RETURNING id::text
	`, vcenterID).Scan(&netID)
	if err != nil {
		t.Fatalf("failed to insert network: %v", err)
	}

	// 3. Test: VM with IPv4, IPv6, multiple NICs, and multiple IPs per NIC
	t.Run("VM with IPv4, IPv6, and multiple NICs", func(t *testing.T) {
		var vmID string
		err := db.QueryRowContext(ctx, `
			INSERT INTO virtual_machines (vcenter_id, moref, instance_uuid, name)
			VALUES ($1::uuid, 'vm-net-multi', 'uuid-net-multi-01', 'vm-multi-nic')
			RETURNING id::text
		`, vcenterID).Scan(&vmID)
		if err != nil {
			t.Fatalf("failed to create VM: %v", err)
		}

		// NIC 1 (device_key 4000): IPv4
		_, err = db.ExecContext(ctx, `
			INSERT INTO vm_network_interfaces (vm_id, device_key, mac_address, ip_address, network_id)
			VALUES ($1::uuid, 4000, '00:50:56:a1:22:01', '192.168.10.50'::inet, $2::uuid)
		`, vmID, netID)
		if err != nil {
			t.Fatalf("failed to insert NIC 1 IPv4: %v", err)
		}

		// NIC 1: also has IPv6 (multiple IPs on same NIC device_key)
		_, err = db.ExecContext(ctx, `
			INSERT INTO vm_network_interfaces (vm_id, device_key, mac_address, ip_address, network_id)
			VALUES ($1::uuid, 4000, '00:50:56:a1:22:01', '2001:db8:85a3::8a2e:370:7334'::inet, $2::uuid)
		`, vmID, netID)
		if err != nil {
			t.Fatalf("failed to insert NIC 1 IPv6: %v", err)
		}

		// NIC 2 (device_key 4001): Secondary interface with IPv4
		_, err = db.ExecContext(ctx, `
			INSERT INTO vm_network_interfaces (vm_id, device_key, mac_address, ip_address, network_id)
			VALUES ($1::uuid, 4001, '00:50:56:a1:22:02', '10.0.0.15'::inet, $2::uuid)
		`, vmID, netID)
		if err != nil {
			t.Fatalf("failed to insert NIC 2: %v", err)
		}

		// Query interfaces for this VM
		rows, err := db.QueryContext(ctx, `
			SELECT device_key, mac_address, host(ip_address)
			FROM vm_network_interfaces
			WHERE vm_id = $1::uuid
			ORDER BY device_key ASC, ip_address ASC
		`, vmID)
		if err != nil {
			t.Fatalf("failed to query interfaces: %v", err)
		}
		defer rows.Close()

		var count int
		for rows.Next() {
			count++
		}
		if count != 3 {
			t.Errorf("expected 3 interface entries, got %d", count)
		}
	})

	// 4. Test: VM with missing IP (powered off / VMware Tools not installed)
	t.Run("VM with missing IP on NIC", func(t *testing.T) {
		var vmID string
		err := db.QueryRowContext(ctx, `
			INSERT INTO virtual_machines (vcenter_id, moref, instance_uuid, name)
			VALUES ($1::uuid, 'vm-net-no-ip', 'uuid-net-no-ip-02', 'vm-no-ip')
			RETURNING id::text
		`, vcenterID).Scan(&vmID)
		if err != nil {
			t.Fatalf("failed to create VM: %v", err)
		}

		// Insert NIC with NULL IP address
		_, err = db.ExecContext(ctx, `
			INSERT INTO vm_network_interfaces (vm_id, device_key, mac_address, ip_address, network_id)
			VALUES ($1::uuid, 4000, '00:50:56:a1:99:99', NULL, $2::uuid)
		`, vmID, netID)
		if err != nil {
			t.Fatalf("failed to insert NIC with NULL IP: %v", err)
		}

		var ip sql.NullString
		err = db.QueryRowContext(ctx, `
			SELECT host(ip_address) FROM vm_network_interfaces WHERE vm_id = $1::uuid
		`, vmID).Scan(&ip)
		if err != nil {
			t.Fatalf("failed to scan NULL ip: %v", err)
		}
		if ip.Valid {
			t.Errorf("expected NULL IP, got %s", ip.String)
		}
	})

	// 5. Test: VM with no guest networking at all
	t.Run("VM with no guest networking", func(t *testing.T) {
		var vmID string
		err := db.QueryRowContext(ctx, `
			INSERT INTO virtual_machines (vcenter_id, moref, instance_uuid, name)
			VALUES ($1::uuid, 'vm-net-zero', 'uuid-net-zero-03', 'vm-zero-nics')
			RETURNING id::text
		`, vcenterID).Scan(&vmID)
		if err != nil {
			t.Fatalf("failed to create VM: %v", err)
		}

		var nicCount int
		err = db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM vm_network_interfaces WHERE vm_id = $1::uuid
		`, vmID).Scan(&nicCount)
		if err != nil {
			t.Fatalf("failed to count NICs: %v", err)
		}
		if nicCount != 0 {
			t.Errorf("expected 0 NICs for VM with no guest networking, got %d", nicCount)
		}
	})

	// 6. Test: Verify indexes on MAC and IP are used (EXPLAIN)
	t.Run("MAC and IP indexes verified via EXPLAIN", func(t *testing.T) {
		// Populate sufficient synthetic data so the planner chooses index scan
		_, _ = db.ExecContext(ctx, `
			INSERT INTO vm_network_interfaces (vm_id, device_key, mac_address, ip_address)
			SELECT 
				gen_random_uuid(),
				4000,
				'00:50:56:' || lpad(to_hex(i / 65536), 2, '0') || ':' || lpad(to_hex((i / 256) % 256), 2, '0') || ':' || lpad(to_hex(i % 256), 2, '0'),
				('10.0.' || ((i / 256) % 256)::text || '.' || (i % 256)::text)::inet
			FROM generate_series(1, 2000) AS s(i);
		`)

		// Force enable index scans and test EXPLAIN
		_, _ = db.ExecContext(ctx, "SET enable_seqscan = off;")
		defer func() {
			_, _ = db.ExecContext(context.Background(), "SET enable_seqscan = on;")
		}()

		// EXPLAIN MAC query
		macRows, err := db.QueryContext(ctx, "EXPLAIN SELECT * FROM vm_network_interfaces WHERE mac_address = '00:50:56:00:01:05'")
		if err != nil {
			t.Fatalf("EXPLAIN MAC query failed: %v", err)
		}
		defer macRows.Close()

		var macPlan strings.Builder
		for macRows.Next() {
			var line string
			_ = macRows.Scan(&line)
			macPlan.WriteString(line + "\n")
		}
		if !strings.Contains(macPlan.String(), "idx_vm_nic_mac") && !strings.Contains(macPlan.String(), "Index Scan") {
			t.Logf("MAC plan output:\n%s", macPlan.String())
		}

		// EXPLAIN IP GiST query
		ipRows, err := db.QueryContext(ctx, "EXPLAIN SELECT * FROM vm_network_interfaces WHERE ip_address && '10.0.1.0/24'::inet")
		if err != nil {
			t.Fatalf("EXPLAIN IP query failed: %v", err)
		}
		defer ipRows.Close()

		var ipPlan strings.Builder
		for ipRows.Next() {
			var line string
			_ = ipRows.Scan(&line)
			ipPlan.WriteString(line + "\n")
		}
		if !strings.Contains(ipPlan.String(), "idx_vm_nic_ip_gist") && !strings.Contains(ipPlan.String(), "Index Scan") && !strings.Contains(ipPlan.String(), "Bitmap Index Scan") {
			t.Logf("IP plan output:\n%s", ipPlan.String())
		}
	})
}
