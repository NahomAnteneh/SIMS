//go:build integration

package integration_test

import (
	"context"
	"testing"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/persistence"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/vmware/govmomi/vim25/types"
)

func TestNICUpdates_IPChange(t *testing.T) {
	ctx := context.Background()
	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-nic-ip-change")
	metrics := telemetry.NewMetrics("test", "0.1.0")
	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{FlushSize: 100}, metrics)

	initialVM := domain.VMRecord{
		VCenterID:       vcenterID,
		MoRef:           "vm-nic-1",
		Name:            "test-ip-change-vm",
		PowerState:      "poweredOn",
		ConnectionState: "connected",
		CPUCores:        2,
		MemoryMB:        4096,
		NetworkInterfaces: []domain.NetworkInterfaceRecord{
			{
				DeviceKey:  4000,
				MacAddress: "00:50:56:01:02:03",
				IPAddress:  "192.168.1.10",
			},
		},
	}

	_, err := persister.PersistVMBatch(ctx, []domain.VMRecord{initialVM})
	if err != nil {
		t.Fatalf("failed to persist initial VM: %v", err)
	}

	var vmID string
	err = pool.QueryRow(ctx, `SELECT id::text FROM virtual_machines WHERE vcenter_id = $1 AND moref = 'vm-nic-1'`, vcenterID).Scan(&vmID)
	if err != nil {
		t.Fatalf("failed to query VM id: %v", err)
	}

	// 1. IP change: update to 192.168.1.20 via IncrementalApplier (guest.net update)
	applier := collector.NewIncrementalApplier(persister, nil, nil)
	update := types.ObjectUpdate{
		Kind: types.ObjectUpdateKindModify,
		Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-nic-1"},
		ChangeSet: []types.PropertyChange{
			{
				Name: "guest.net",
				Op:   types.PropertyChangeOpAssign,
				Val: []types.GuestNicInfo{
					{
						DeviceConfigId: 4000,
						MacAddress:     "00:50:56:01:02:03",
						IpAddress:      []string{"192.168.1.20"},
					},
				},
			},
		},
	}

	if err := applier.ApplyObjectUpdate(ctx, vcenterID, update); err != nil {
		t.Fatalf("failed to apply IP change update: %v", err)
	}

	// Verify exactly 1 NIC row exists with updated IP
	var count int
	var ip string
	err = pool.QueryRow(ctx, `SELECT COUNT(*), host(ip_address) FROM vm_network_interfaces WHERE vm_id = $1 GROUP BY ip_address`, vmID).Scan(&count, &ip)
	if err != nil {
		t.Fatalf("failed to query updated NIC: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 NIC row, got: %d", count)
	}
	if ip != "192.168.1.20" {
		t.Errorf("expected IP '192.168.1.20', got: %s", ip)
	}
}

func TestNICUpdates_NICAdditionAndRemoval(t *testing.T) {
	ctx := context.Background()
	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-nic-add-remove")
	metrics := telemetry.NewMetrics("test", "0.1.0")
	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{FlushSize: 100}, metrics)

	// Start with 1 NIC
	initialVM := domain.VMRecord{
		VCenterID:       vcenterID,
		MoRef:           "vm-nic-add-remove",
		Name:            "test-add-remove-vm",
		PowerState:      "poweredOn",
		ConnectionState: "connected",
		CPUCores:        4,
		MemoryMB:        8192,
		NetworkInterfaces: []domain.NetworkInterfaceRecord{
			{
				DeviceKey:  4000,
				MacAddress: "00:50:56:aa:bb:01",
				IPAddress:  "10.10.1.10",
			},
		},
	}

	_, err := persister.PersistVMBatch(ctx, []domain.VMRecord{initialVM})
	if err != nil {
		t.Fatalf("failed to persist initial VM: %v", err)
	}

	var vmID string
	err = pool.QueryRow(ctx, `SELECT id::text FROM virtual_machines WHERE vcenter_id = $1 AND moref = 'vm-nic-add-remove'`, vcenterID).Scan(&vmID)
	if err != nil {
		t.Fatalf("failed to query VM id: %v", err)
	}

	applier := collector.NewIncrementalApplier(persister, nil, nil)

	// Step 1: NIC Addition (1 NIC -> 2 NICs)
	addUpdate := types.ObjectUpdate{
		Kind: types.ObjectUpdateKindModify,
		Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-nic-add-remove"},
		ChangeSet: []types.PropertyChange{
			{
				Name: "guest.net",
				Op:   types.PropertyChangeOpAssign,
				Val: []types.GuestNicInfo{
					{
						DeviceConfigId: 4000,
						MacAddress:     "00:50:56:aa:bb:01",
						IpAddress:      []string{"10.10.1.10"},
					},
					{
						DeviceConfigId: 4001,
						MacAddress:     "00:50:56:aa:bb:02",
						IpAddress:      []string{"10.10.2.20"},
					},
				},
			},
		},
	}

	if err := applier.ApplyObjectUpdate(ctx, vcenterID, addUpdate); err != nil {
		t.Fatalf("failed to apply NIC addition: %v", err)
	}

	var countAfterAdd int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM vm_network_interfaces WHERE vm_id = $1`, vmID).Scan(&countAfterAdd)
	if err != nil || countAfterAdd != 2 {
		t.Fatalf("expected 2 NICs after addition, got: %d (err: %v)", countAfterAdd, err)
	}

	// Step 2: NIC Removal (2 NICs -> 1 NIC)
	removeUpdate := types.ObjectUpdate{
		Kind: types.ObjectUpdateKindModify,
		Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-nic-add-remove"},
		ChangeSet: []types.PropertyChange{
			{
				Name: "guest.net",
				Op:   types.PropertyChangeOpAssign,
				Val: []types.GuestNicInfo{
					{
						DeviceConfigId: 4001,
						MacAddress:     "00:50:56:aa:bb:02",
						IpAddress:      []string{"10.10.2.20"},
					},
				},
			},
		},
	}

	if err := applier.ApplyObjectUpdate(ctx, vcenterID, removeUpdate); err != nil {
		t.Fatalf("failed to apply NIC removal: %v", err)
	}

	var countAfterRemove int
	var remainingMac string
	err = pool.QueryRow(ctx, `SELECT COUNT(*), mac_address FROM vm_network_interfaces WHERE vm_id = $1 GROUP BY mac_address`, vmID).Scan(&countAfterRemove, &remainingMac)
	if err != nil || countAfterRemove != 1 {
		t.Fatalf("expected 1 NIC after removal, got: %d (err: %v)", countAfterRemove, err)
	}
	if remainingMac != "00:50:56:aa:bb:02" {
		t.Errorf("expected remaining MAC '00:50:56:aa:bb:02', got: %s", remainingMac)
	}

	// Step 3: Total NIC removal (empty slice)
	removeAllUpdate := types.ObjectUpdate{
		Kind: types.ObjectUpdateKindModify,
		Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-nic-add-remove"},
		ChangeSet: []types.PropertyChange{
			{
				Name: "guest.net",
				Op:   types.PropertyChangeOpAssign,
				Val:  []types.GuestNicInfo{},
			},
		},
	}

	if err := applier.ApplyObjectUpdate(ctx, vcenterID, removeAllUpdate); err != nil {
		t.Fatalf("failed to apply remove all NICs: %v", err)
	}

	var countAfterTotalRemove int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM vm_network_interfaces WHERE vm_id = $1`, vmID).Scan(&countAfterTotalRemove)
	if err != nil || countAfterTotalRemove != 0 {
		t.Fatalf("expected 0 NICs after total removal, got: %d", countAfterTotalRemove)
	}
}

func TestNICUpdates_MultipleNICsAndIPv6(t *testing.T) {
	ctx := context.Background()
	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-nic-multi-ipv6")
	metrics := telemetry.NewMetrics("test", "0.1.0")
	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{FlushSize: 100}, metrics)

	initialVM := domain.VMRecord{
		VCenterID:       vcenterID,
		MoRef:           "vm-nic-ipv6",
		Name:            "test-ipv6-vm",
		PowerState:      "poweredOn",
		ConnectionState: "connected",
		CPUCores:        4,
		MemoryMB:        8192,
	}

	_, err := persister.PersistVMBatch(ctx, []domain.VMRecord{initialVM})
	if err != nil {
		t.Fatalf("failed to persist initial VM: %v", err)
	}

	var vmID string
	err = pool.QueryRow(ctx, `SELECT id::text FROM virtual_machines WHERE vcenter_id = $1 AND moref = 'vm-nic-ipv6'`, vcenterID).Scan(&vmID)
	if err != nil {
		t.Fatalf("failed to query VM id: %v", err)
	}

	applier := collector.NewIncrementalApplier(persister, nil, nil)

	// Interface with multiple IPs: IPv4, IPv6 global, and IPv6 link-local
	multiIPUpdate := types.ObjectUpdate{
		Kind: types.ObjectUpdateKindModify,
		Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-nic-ipv6"},
		ChangeSet: []types.PropertyChange{
			{
				Name: "guest.net",
				Op:   types.PropertyChangeOpAssign,
				Val: []types.GuestNicInfo{
					{
						DeviceConfigId: 4000,
						MacAddress:     "00:50:56:fe:ed:01",
						IpAddress: []string{
							"10.0.0.5",
							"2001:db8::1",
							"fe80::250:56ff:fefe:ed01",
						},
					},
					{
						DeviceConfigId: 4001,
						MacAddress:     "00:50:56:fe:ed:02",
						IpAddress: []string{
							"172.16.0.10",
						},
					},
				},
			},
		},
	}

	if err := applier.ApplyObjectUpdate(ctx, vcenterID, multiIPUpdate); err != nil {
		t.Fatalf("failed to apply multi-IP update: %v", err)
	}

	// Verify all 4 IP entries were stored
	var totalNICs int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM vm_network_interfaces WHERE vm_id = $1`, vmID).Scan(&totalNICs)
	if err != nil || totalNICs != 4 {
		t.Fatalf("expected 4 NIC entries across multiple IPs, got: %d (err: %v)", totalNICs, err)
	}

	// Verify IPv6 GiST/exact query succeeds
	var ipv6Found string
	err = pool.QueryRow(ctx, `SELECT host(ip_address) FROM vm_network_interfaces WHERE vm_id = $1 AND ip_address = '2001:db8::1'::inet`, vmID).Scan(&ipv6Found)
	if err != nil {
		t.Fatalf("failed to query IPv6 address: %v", err)
	}
	if ipv6Found != "2001:db8::1" {
		t.Errorf("expected IPv6 '2001:db8::1', got: %s", ipv6Found)
	}
}

func TestNICUpdates_VMDeletionCascade(t *testing.T) {
	ctx := context.Background()
	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-nic-delete")
	metrics := telemetry.NewMetrics("test", "0.1.0")
	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{FlushSize: 100}, metrics)

	initialVM := domain.VMRecord{
		VCenterID:       vcenterID,
		MoRef:           "vm-nic-del",
		Name:            "test-del-vm",
		PowerState:      "poweredOn",
		ConnectionState: "connected",
		CPUCores:        2,
		MemoryMB:        4096,
		NetworkInterfaces: []domain.NetworkInterfaceRecord{
			{
				DeviceKey:  4000,
				MacAddress: "00:50:56:dd:ee:01",
				IPAddress:  "192.168.99.1",
			},
		},
	}

	_, err := persister.PersistVMBatch(ctx, []domain.VMRecord{initialVM})
	if err != nil {
		t.Fatalf("failed to persist initial VM: %v", err)
	}

	var vmID string
	err = pool.QueryRow(ctx, `SELECT id::text FROM virtual_machines WHERE vcenter_id = $1 AND moref = 'vm-nic-del'`, vcenterID).Scan(&vmID)
	if err != nil {
		t.Fatalf("failed to query VM id: %v", err)
	}

	// 1. Soft delete (ApplyVMLeave)
	applier := collector.NewIncrementalApplier(persister, nil, nil)
	leaveUpdate := types.ObjectUpdate{
		Kind: types.ObjectUpdateKindLeave,
		Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-nic-del"},
	}
	if err := applier.ApplyObjectUpdate(ctx, vcenterID, leaveUpdate); err != nil {
		t.Fatalf("failed to apply VM leave: %v", err)
	}

	// On soft delete, NIC rows remain linked for audit/history
	var nicCountSoft int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM vm_network_interfaces WHERE vm_id = $1`, vmID).Scan(&nicCountSoft)
	if err != nil || nicCountSoft != 1 {
		t.Fatalf("expected 1 NIC row preserved for soft-deleted VM, got: %d", nicCountSoft)
	}

	// 2. Physical delete (ON DELETE CASCADE)
	_, err = pool.Exec(ctx, `DELETE FROM virtual_machines WHERE id = $1`, vmID)
	if err != nil {
		t.Fatalf("failed to physical delete VM: %v", err)
	}

	// On physical delete, foreign key CASCADE automatically deletes all associated NIC rows
	var nicCountHard int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM vm_network_interfaces WHERE vm_id = $1`, vmID).Scan(&nicCountHard)
	if err != nil || nicCountHard != 0 {
		t.Fatalf("expected 0 NIC rows after CASCADE delete, got: %d", nicCountHard)
	}
}
