//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/persistence"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBulkPersistenceIdempotency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	connStr := getConnString("collector_rw", "collector_rw_pass")
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to connect pool: %v", err)
	}
	defer pool.Close()

	// 1. Create vCenter
	var vcenterID string
	err = pool.QueryRow(ctx, `
		INSERT INTO vcenters (name, api_url, instance_uuid)
		VALUES ('vc-persistence-test', 'https://vc-persistence-test.local/sdk', 'vc-pers-uuid-01')
		ON CONFLICT (api_url) DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`).Scan(&vcenterID)
	if err != nil {
		t.Fatalf("failed to insert test vcenter: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	// 2. Insert test datastore and network
	var dsID, netID string
	err = pool.QueryRow(ctx, `
		INSERT INTO datastores (vcenter_id, moref, name)
		VALUES ($1::uuid, 'ds-p-1', 'NFS_Datastore')
		ON CONFLICT (vcenter_id, moref) DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`, vcenterID).Scan(&dsID)
	if err != nil {
		t.Fatalf("failed to insert datastore: %v", err)
	}

	err = pool.QueryRow(ctx, `
		INSERT INTO networks (vcenter_id, moref, name, vlan_ids)
		VALUES ($1::uuid, 'net-p-1', 'VLAN_200_Net', '{[200,200]}'::int4multirange)
		ON CONFLICT (vcenter_id, moref) DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`, vcenterID).Scan(&netID)
	if err != nil {
		t.Fatalf("failed to insert network: %v", err)
	}

	metrics := telemetry.NewMetrics("sims-collector", "0.1.0")
	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{
		FlushInterval: 250 * time.Millisecond,
		FlushSize:     500,
	}, metrics)

	// 3. Build a test batch of 25 VMs with attachments
	const batchSize = 25
	records := make([]domain.VMRecord, batchSize)
	for i := 0; i < batchSize; i++ {
		records[i] = domain.VMRecord{
			VCenterID:       vcenterID,
			MoRef:           fmt.Sprintf("vm-pers-%d", i),
			InstanceUUID:    fmt.Sprintf("uuid-pers-%d", i),
			Name:            fmt.Sprintf("app-server-%02d", i),
			PowerState:      "poweredOn",
			ConnectionState: "connected",
			CPUCores:        4,
			MemoryMB:        8192,
			GuestIPAddress:  fmt.Sprintf("192.168.10.%d", 10+i),
			DatastoreIDs:    []string{dsID},
			NetworkInterfaces: []domain.NetworkInterfaceRecord{
				{
					DeviceKey:  4000,
					MacAddress: fmt.Sprintf("00:50:56:b1:00:%02x", i),
					IPAddress:  fmt.Sprintf("192.168.10.%d", 10+i),
					NetworkID:  netID,
				},
			},
		}
	}

	// 4. Initial write: Persist batch
	t.Log("Persisting initial batch...")
	compacted, err := persister.PersistVMBatch(ctx, records)
	if err != nil {
		t.Fatalf("initial PersistVMBatch failed: %v", err)
	}
	if compacted != batchSize {
		t.Fatalf("expected %d compacted records, got %d", batchSize, compacted)
	}

	// Verify row counts
	var initialVMCount, initialNICCount, initialDSCount int
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM virtual_machines WHERE vcenter_id = $1::uuid", vcenterID).Scan(&initialVMCount)
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM vm_network_interfaces WHERE network_id = $1::uuid", netID).Scan(&initialNICCount)
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM vm_datastores WHERE datastore_id = $1::uuid", dsID).Scan(&initialDSCount)

	if initialVMCount != batchSize {
		t.Errorf("expected %d VMs, got %d", batchSize, initialVMCount)
	}
	if initialNICCount != batchSize {
		t.Errorf("expected %d NICs, got %d", batchSize, initialNICCount)
	}
	if initialDSCount != batchSize {
		t.Errorf("expected %d DS attachments, got %d", batchSize, initialDSCount)
	}

	// 5. IDEMPOTENCY TEST: Re-run the EXACT same batch
	t.Log("Re-running exact same batch to test idempotency...")
	compacted2, err := persister.PersistVMBatch(ctx, records)
	if err != nil {
		t.Fatalf("second PersistVMBatch failed: %v", err)
	}
	if compacted2 != batchSize {
		t.Fatalf("expected %d compacted records on second run, got %d", batchSize, compacted2)
	}

	// Verify row counts after re-run
	var rerunVMCount, rerunNICCount, rerunDSCount int
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM virtual_machines WHERE vcenter_id = $1::uuid", vcenterID).Scan(&rerunVMCount)
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM vm_network_interfaces WHERE network_id = $1::uuid", netID).Scan(&rerunNICCount)
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM vm_datastores WHERE datastore_id = $1::uuid", dsID).Scan(&rerunDSCount)

	if rerunVMCount != initialVMCount {
		t.Errorf("idempotency violation: VM count changed from %d to %d", initialVMCount, rerunVMCount)
	}
	if rerunNICCount != initialNICCount {
		t.Errorf("idempotency violation: NIC count changed from %d to %d", initialNICCount, rerunNICCount)
	}
	if rerunDSCount != initialDSCount {
		t.Errorf("idempotency violation: DS count changed from %d to %d", initialDSCount, rerunDSCount)
	}
}

func TestStreamFlusher(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	connStr := getConnString("collector_rw", "collector_rw_pass")
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to connect pool: %v", err)
	}
	defer pool.Close()

	var vcenterID string
	err = pool.QueryRow(ctx, `
		INSERT INTO vcenters (name, api_url, instance_uuid)
		VALUES ('vc-flusher-test', 'https://vc-flusher-test.local/sdk', 'vc-flush-uuid-01')
		ON CONFLICT (api_url) DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`).Scan(&vcenterID)
	if err != nil {
		t.Fatalf("failed to insert test vcenter: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{
		FlushInterval: 100 * time.Millisecond,
		FlushSize:     10, // Small flush size
	}, nil)

	flusher := persistence.NewStreamFlusher(persister)
	defer flusher.Close()

	// Push 5 records (less than FlushSize 10)
	smallBatch := make([]domain.VMRecord, 5)
	for i := 0; i < 5; i++ {
		smallBatch[i] = domain.VMRecord{
			VCenterID:    vcenterID,
			MoRef:        fmt.Sprintf("vm-timed-%d", i),
			InstanceUUID: fmt.Sprintf("uuid-timed-%d", i),
			Name:         fmt.Sprintf("timed-vm-%d", i),
		}
	}

	err = flusher.Push(ctx, smallBatch)
	if err != nil {
		t.Fatalf("failed to push records: %v", err)
	}

	// Wait for ticker flush (100ms)
	time.Sleep(250 * time.Millisecond)

	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM virtual_machines WHERE vcenter_id = $1::uuid", vcenterID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count VMs: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5 VMs flushed by timer, got %d", count)
	}
}

func TestBulkPersistence_EmptyUUIDAndIPv6ZoneFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	connStr := getConnString("collector_rw", "collector_rw_pass")
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to connect pool: %v", err)
	}
	defer pool.Close()

	var vcenterID string
	err = pool.QueryRow(ctx, `
		INSERT INTO vcenters (name, api_url, instance_uuid)
		VALUES ('vc-real-edge-test', 'https://vc-real-edge.local/sdk', 'vc-real-edge-uuid-01')
		ON CONFLICT (api_url) DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`).Scan(&vcenterID)
	if err != nil {
		t.Fatalf("failed to insert test vcenter: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{
		FlushInterval: 250 * time.Millisecond,
		FlushSize:     500,
	}, nil)

	// Test case: 3 VMs with empty InstanceUUID (which real vCenter templates often have)
	// and IPv6 addresses with zone indexes (e.g., %eth0)
	edgeRecords := []domain.VMRecord{
		{
			VCenterID:      vcenterID,
			MoRef:          "vm-edge-1",
			InstanceUUID:   "", // Empty UUID!
			Name:           "template-vm-1",
			PowerState:     "poweredOff",
			GuestIPAddress: "fe80::1ff:fe00:1%eth0", // IPv6 with zone index
			NetworkInterfaces: []domain.NetworkInterfaceRecord{
				{
					DeviceKey:  4000,
					MacAddress: "00:50:56:01:02:03",
					IPAddress:  "fe80::1ff:fe00:1%eth0",
				},
			},
		},
		{
			VCenterID:      vcenterID,
			MoRef:          "vm-edge-2",
			InstanceUUID:   "", // Another VM with empty UUID!
			Name:           "template-vm-2",
			PowerState:     "poweredOff",
			GuestIPAddress: "fe80::2ff:fe00:2%12",
			NetworkInterfaces: []domain.NetworkInterfaceRecord{
				{
					DeviceKey:  4000,
					MacAddress: "00:50:56:01:02:04",
					IPAddress:  "fe80::2ff:fe00:2%12",
				},
			},
		},
		{
			VCenterID:      vcenterID,
			MoRef:          "vm-edge-3",
			InstanceUUID:   "", // Third VM with empty UUID!
			Name:           "template-vm-3",
			PowerState:     "poweredOff",
		},
	}

	count, err := persister.PersistVMBatch(ctx, edgeRecords)
	if err != nil {
		t.Fatalf("PersistVMBatch failed for edge records with empty UUIDs and IPv6 zone indexes: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 persisted records, got %d", count)
	}

	// Verify each VM received a unique, non-empty instance_uuid and valid inet
	rows, err := pool.Query(ctx, `
		SELECT moref, instance_uuid, host(guest_ip_address) 
		FROM virtual_machines 
		WHERE vcenter_id = $1::uuid 
		ORDER BY moref
	`, vcenterID)
	if err != nil {
		t.Fatalf("failed to query virtual_machines: %v", err)
	}
	defer rows.Close()

	seenUUIDs := make(map[string]bool)
	var rowCount int
	for rows.Next() {
		var moref, instUUID string
		var guestIP *string
		if err := rows.Scan(&moref, &instUUID, &guestIP); err != nil {
			t.Fatalf("failed to scan row: %v", err)
		}
		rowCount++
		if instUUID == "" {
			t.Errorf("VM %s has empty instance_uuid", moref)
		}
		if seenUUIDs[instUUID] {
			t.Errorf("VM %s duplicate instance_uuid %s", moref, instUUID)
		}
		seenUUIDs[instUUID] = true

		if moref == "vm-edge-1" {
			if guestIP == nil {
				t.Fatalf("expected non-nil guestIP")
			}
			t.Logf("actual guestIP is: %s", *guestIP)
			if *guestIP != "fe80::1ff:fe00:1" {
				t.Errorf("expected clean IPv6 without zone index 'fe80::1ff:fe00:1', got %s", *guestIP)
			}
		}
	}
	if rowCount != 3 {
		t.Errorf("expected 3 rows in database, got %d", rowCount)
	}
}

