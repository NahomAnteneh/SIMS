//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/persistence"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/vmware/govmomi/vim25/types"
)

func TestFieldOwnership_PropertyCollectorDoesNotTouchEventsOrTags(t *testing.T) {
	ctx := context.Background()
	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-ownership-1")
	metrics := telemetry.NewMetrics("test", "0.1.0")
	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{FlushSize: 100}, metrics)

	// 1. Initial hydration: PropertyCollector establishes VM
	initialVM := domain.VMRecord{
		VCenterID:       vcenterID,
		MoRef:           "vm-owner-1",
		Name:            "owner-test-vm",
		PowerState:      "poweredOn",
		ConnectionState: "connected",
		CPUCores:        2,
		MemoryMB:        4096,
		NetworkInterfaces: []domain.NetworkInterfaceRecord{
			{
				DeviceKey:  4000,
				MacAddress: "00:50:56:a1:b2:c3",
				IPAddress:  "192.168.1.50",
			},
		},
		CustomAttributes: []domain.CustomAttributeRecord{
			{Name: "Tier", Value: "Gold"},
		},
	}

	_, err := persister.PersistVMBatch(ctx, []domain.VMRecord{initialVM})
	if err != nil {
		t.Fatalf("failed to persist initial VM: %v", err)
	}

	var vmID string
	var firstSeenAt time.Time
	err = pool.QueryRow(ctx, `SELECT id::text, first_seen_at FROM virtual_machines WHERE vcenter_id = $1 AND moref = 'vm-owner-1'`, vcenterID).Scan(&vmID, &firstSeenAt)
	if err != nil {
		t.Fatalf("failed to query VM id: %v", err)
	}

	// 2. Event layer: sets created_at and created_by (S2.10 owner: Event layer)
	eventCreatedAt := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	eventCreatedBy := "domain\\cloud-provisioner"
	if err := persister.UpdateVMProvenance(ctx, vcenterID, "vm-owner-1", eventCreatedAt, eventCreatedBy); err != nil {
		t.Fatalf("failed to update VM provenance: %v", err)
	}

	// 3. Tag Synchronizer: sets vm_tags (S2.10 owner: Tag Synchronizer via REST)
	_, err = pool.Exec(ctx, `
		INSERT INTO vm_tags (vm_id, tag_id, category_name, tag_name)
		VALUES ($1, 'urn:vmomi:InventoryServiceTag:tag-owner-1', 'Environment', 'Production')
	`, vmID)
	if err != nil {
		t.Fatalf("failed to insert tag: %v", err)
	}

	// 4. PropertyCollector: incremental update modifies powerState (S2.10 owner: PropertyCollector)
	offState := "poweredOff"
	mask := collector.VMFieldMask{
		PowerState: &offState,
	}
	if err := persister.ApplyVMIncrementalUpdate(ctx, vcenterID, "vm-owner-1", mask); err != nil {
		t.Fatalf("incremental update failed: %v", err)
	}

	// 5. Verify field ownership boundaries
	var currentPower string
	var currentCreatedAt time.Time
	var currentCreatedBy string
	var currentFirstSeen time.Time
	err = pool.QueryRow(ctx, `
		SELECT power_state, created_at, created_by, first_seen_at
		FROM virtual_machines
		WHERE id = $1
	`, vmID).Scan(&currentPower, &currentCreatedAt, &currentCreatedBy, &currentFirstSeen)
	if err != nil {
		t.Fatalf("failed to query updated VM: %v", err)
	}

	// PropertyCollector updated power_state
	if currentPower != "poweredOff" {
		t.Errorf("expected power_state 'poweredOff', got: %s", currentPower)
	}

	// Event layer fields MUST NOT be modified
	if !currentCreatedAt.Equal(eventCreatedAt) {
		t.Errorf("expected created_at %v, got %v", eventCreatedAt, currentCreatedAt)
	}
	if currentCreatedBy != eventCreatedBy {
		t.Errorf("expected created_by %q, got %q", eventCreatedBy, currentCreatedBy)
	}

	// Persistence layer first_seen_at MUST NOT be modified
	if !currentFirstSeen.Equal(firstSeenAt) {
		t.Errorf("expected first_seen_at %v, got %v", firstSeenAt, currentFirstSeen)
	}

	// Tag synchronizer tags MUST NOT be touched
	var tagCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM vm_tags WHERE vm_id = $1`, vmID).Scan(&tagCount)
	if err != nil {
		t.Fatalf("failed to count tags: %v", err)
	}
	if tagCount != 1 {
		t.Errorf("expected 1 tag preserved, got: %d", tagCount)
	}

	// NIC rows MUST NOT be touched
	var nicCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM vm_network_interfaces WHERE vm_id = $1`, vmID).Scan(&nicCount)
	if err != nil {
		t.Fatalf("failed to count NICs: %v", err)
	}
	if nicCount != 1 {
		t.Errorf("expected 1 NIC preserved, got: %d", nicCount)
	}

	// Custom attributes MUST NOT be touched
	var attrVal string
	err = pool.QueryRow(ctx, `SELECT value FROM vm_custom_attributes WHERE vm_id = $1 AND name = 'Tier'`, vmID).Scan(&attrVal)
	if err != nil {
		t.Fatalf("failed to query custom attribute: %v", err)
	}
	if attrVal != "Gold" {
		t.Errorf("expected custom attribute 'Tier' = 'Gold', got: %q", attrVal)
	}
}

func TestFieldOwnership_CustomAttributeUpdateLeavesTagsAndNICsIntact(t *testing.T) {
	ctx := context.Background()
	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-ownership-2")
	metrics := telemetry.NewMetrics("test", "0.1.0")
	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{FlushSize: 100}, metrics)

	initialVM := domain.VMRecord{
		VCenterID:       vcenterID,
		MoRef:           "vm-owner-ca",
		Name:            "ca-owner-vm",
		PowerState:      "poweredOn",
		ConnectionState: "connected",
		CPUCores:        4,
		MemoryMB:        8192,
		NetworkInterfaces: []domain.NetworkInterfaceRecord{
			{
				DeviceKey:  4000,
				MacAddress: "00:50:56:ca:ca:01",
				IPAddress:  "10.0.1.100",
			},
		},
		CustomAttributes: []domain.CustomAttributeRecord{
			{Name: "CostCenter", Value: "1001"},
		},
	}

	_, err := persister.PersistVMBatch(ctx, []domain.VMRecord{initialVM})
	if err != nil {
		t.Fatalf("failed to persist initial VM: %v", err)
	}

	var vmID string
	err = pool.QueryRow(ctx, `SELECT id::text FROM virtual_machines WHERE vcenter_id = $1 AND moref = 'vm-owner-ca'`, vcenterID).Scan(&vmID)
	if err != nil {
		t.Fatalf("failed to query VM id: %v", err)
	}

	// Attach a tag
	_, err = pool.Exec(ctx, `
		INSERT INTO vm_tags (vm_id, tag_id, category_name, tag_name)
		VALUES ($1, 'urn:vmomi:InventoryServiceTag:tag-ca-1', 'Dept', 'Engineering')
	`, vmID)
	if err != nil {
		t.Fatalf("failed to insert tag: %v", err)
	}

	// Now update custom attribute only
	newAttrs := []domain.CustomAttributeRecord{
		{Name: "CostCenter", Value: "2002"},
		{Name: "Environment", Value: "Staging"},
	}
	mask := collector.VMFieldMask{
		CustomAttributes: newAttrs,
	}

	if err := persister.ApplyVMIncrementalUpdate(ctx, vcenterID, "vm-owner-ca", mask); err != nil {
		t.Fatalf("custom attribute update failed: %v", err)
	}

	// Verify custom attributes updated
	var ccVal string
	err = pool.QueryRow(ctx, `SELECT value FROM vm_custom_attributes WHERE vm_id = $1 AND name = 'CostCenter'`, vmID).Scan(&ccVal)
	if err != nil {
		t.Fatalf("failed to query CostCenter: %v", err)
	}
	if ccVal != "2002" {
		t.Errorf("expected CostCenter '2002', got: %q", ccVal)
	}

	// Verify tags remain completely intact
	var tagCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM vm_tags WHERE vm_id = $1`, vmID).Scan(&tagCount)
	if err != nil {
		t.Fatalf("failed to query tags: %v", err)
	}
	if tagCount != 1 {
		t.Errorf("expected 1 tag remaining, got: %d", tagCount)
	}

	// Verify NICs remain completely intact
	var nicCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM vm_network_interfaces WHERE vm_id = $1`, vmID).Scan(&nicCount)
	if err != nil {
		t.Fatalf("failed to query NICs: %v", err)
	}
	if nicCount != 1 {
		t.Errorf("expected 1 NIC remaining, got: %d", nicCount)
	}
}

func TestFieldOwnership_LeaveSoftDeletePreservesHistoricalData(t *testing.T) {
	ctx := context.Background()
	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-ownership-3")
	metrics := telemetry.NewMetrics("test", "0.1.0")
	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{FlushSize: 100}, metrics)

	initialVM := domain.VMRecord{
		VCenterID:       vcenterID,
		MoRef:           "vm-owner-leave",
		Name:            "leave-test-vm",
		PowerState:      "poweredOn",
		ConnectionState: "connected",
		CPUCores:        8,
		MemoryMB:        16384,
	}

	_, err := persister.PersistVMBatch(ctx, []domain.VMRecord{initialVM})
	if err != nil {
		t.Fatalf("failed to persist initial VM: %v", err)
	}

	var vmID string
	err = pool.QueryRow(ctx, `SELECT id::text FROM virtual_machines WHERE vcenter_id = $1 AND moref = 'vm-owner-leave'`, vcenterID).Scan(&vmID)
	if err != nil {
		t.Fatalf("failed to query VM id: %v", err)
	}

	// Set provenance
	createdAt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := persister.UpdateVMProvenance(ctx, vcenterID, "vm-owner-leave", createdAt, "root@domain.local"); err != nil {
		t.Fatalf("failed to set provenance: %v", err)
	}

	// Simulate ObjectUpdateKindLeave
	leaveUpdate := types.ObjectUpdate{
		Kind: types.ObjectUpdateKindLeave,
		Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-owner-leave"},
	}

	applier := collector.NewIncrementalApplier(persister, nil, nil)
	if err := applier.ApplyObjectUpdate(ctx, vcenterID, leaveUpdate); err != nil {
		t.Fatalf("ApplyObjectUpdate leave failed: %v", err)
	}

	// Verify active inventory query does NOT return the VM
	var activeCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM virtual_machines WHERE id = $1 AND deleted_at IS NULL`, vmID).Scan(&activeCount)
	if err != nil {
		t.Fatalf("failed to query active count: %v", err)
	}
	if activeCount != 0 {
		t.Errorf("expected 0 active VMs after leave, got: %d", activeCount)
	}

	// Verify historical row preserves all original data
	var deletedAt *time.Time
	var preservedPower string
	var preservedCreatedBy string
	err = pool.QueryRow(ctx, `
		SELECT deleted_at, power_state, created_by
		FROM virtual_machines
		WHERE id = $1
	`, vmID).Scan(&deletedAt, &preservedPower, &preservedCreatedBy)
	if err != nil {
		t.Fatalf("failed to query historical row: %v", err)
	}

	if deletedAt == nil {
		t.Fatal("expected deleted_at to be populated")
	}
	if preservedPower != "poweredOn" {
		t.Errorf("expected preserved power_state 'poweredOn', got: %s", preservedPower)
	}
	if preservedCreatedBy != "root@domain.local" {
		t.Errorf("expected preserved created_by 'root@domain.local', got: %s", preservedCreatedBy)
	}
}

func TestFieldOwnership_BulkUpsertPreservesProvenanceAndTags(t *testing.T) {
	ctx := context.Background()
	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-ownership-4")
	metrics := telemetry.NewMetrics("test", "0.1.0")
	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{FlushSize: 100}, metrics)

	initialVM := domain.VMRecord{
		VCenterID:       vcenterID,
		MoRef:           "vm-owner-bulk",
		Name:            "bulk-test-vm",
		PowerState:      "poweredOn",
		ConnectionState: "connected",
		CPUCores:        2,
		MemoryMB:        2048,
	}

	_, err := persister.PersistVMBatch(ctx, []domain.VMRecord{initialVM})
	if err != nil {
		t.Fatalf("failed to persist initial VM: %v", err)
	}

	var vmID string
	var firstSeenAt time.Time
	err = pool.QueryRow(ctx, `SELECT id::text, first_seen_at FROM virtual_machines WHERE vcenter_id = $1 AND moref = 'vm-owner-bulk'`, vcenterID).Scan(&vmID, &firstSeenAt)
	if err != nil {
		t.Fatalf("failed to query VM id: %v", err)
	}

	// Set provenance via Event layer
	eventCreatedAt := time.Date(2025, 3, 10, 14, 0, 0, 0, time.UTC)
	eventCreatedBy := "automation@internal"
	if err := persister.UpdateVMProvenance(ctx, vcenterID, "vm-owner-bulk", eventCreatedAt, eventCreatedBy); err != nil {
		t.Fatalf("failed to set provenance: %v", err)
	}

	// Attach a tag via Tag Synchronizer
	_, err = pool.Exec(ctx, `
		INSERT INTO vm_tags (vm_id, tag_id, category_name, tag_name)
		VALUES ($1, 'urn:vmomi:InventoryServiceTag:tag-bulk-1', 'App', 'Billing')
	`, vmID)
	if err != nil {
		t.Fatalf("failed to insert tag: %v", err)
	}

	// Re-run bulk baseline hydration with updated CPU/Memory
	updatedVM := domain.VMRecord{
		VCenterID:       vcenterID,
		MoRef:           "vm-owner-bulk",
		Name:            "bulk-test-vm",
		PowerState:      "poweredOn",
		ConnectionState: "connected",
		CPUCores:        8,
		MemoryMB:        16384,
		Tags: []domain.TagRecord{
			{TagID: "urn:vmomi:InventoryServiceTag:tag-bulk-1", CategoryName: "App", TagName: "Billing"},
		},
	}

	_, err = persister.PersistVMBatch(ctx, []domain.VMRecord{updatedVM})
	if err != nil {
		t.Fatalf("rehydration bulk persist failed: %v", err)
	}

	// Verify updated CPU/Memory
	var currentCPU int32
	var currentMemory int64
	var currentCreatedAt time.Time
	var currentCreatedBy string
	var currentFirstSeen time.Time
	var currentLastSeen time.Time
	err = pool.QueryRow(ctx, `
		SELECT cpu_cores, memory_mb, created_at, created_by, first_seen_at, last_seen_at
		FROM virtual_machines
		WHERE id = $1
	`, vmID).Scan(&currentCPU, &currentMemory, &currentCreatedAt, &currentCreatedBy, &currentFirstSeen, &currentLastSeen)
	if err != nil {
		t.Fatalf("failed to query rehydrated VM: %v", err)
	}

	if currentCPU != 8 || currentMemory != 16384 {
		t.Errorf("expected CPU=8, Mem=16384, got: CPU=%d, Mem=%d", currentCPU, currentMemory)
	}
	if !currentCreatedAt.Equal(eventCreatedAt) {
		t.Errorf("expected created_at %v preserved, got %v", eventCreatedAt, currentCreatedAt)
	}
	if currentCreatedBy != eventCreatedBy {
		t.Errorf("expected created_by %q preserved, got %q", eventCreatedBy, currentCreatedBy)
	}
	if !currentFirstSeen.Equal(firstSeenAt) {
		t.Errorf("expected first_seen_at %v preserved, got %v", firstSeenAt, currentFirstSeen)
	}
	if currentLastSeen.IsZero() {
		t.Error("expected last_seen_at to be non-zero after bulk upsert")
	}

	// Verify tag was preserved
	var tagCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM vm_tags WHERE vm_id = $1`, vmID).Scan(&tagCount)
	if err != nil {
		t.Fatalf("failed to query tags: %v", err)
	}
	if tagCount != 1 {
		t.Errorf("expected 1 tag preserved, got: %d", tagCount)
	}
}

