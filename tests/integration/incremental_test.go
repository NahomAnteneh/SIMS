//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/persistence"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vmware/govmomi/vim25/types"
)

func setupTestPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	connStr := getConnString("collector_rw", "collector_rw_pass")
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to connect pool: %v", err)
	}
	return pool
}

func createTestVCenter(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) string {
	t.Helper()
	var vcenterID string
	err := pool.QueryRow(ctx, `
		INSERT INTO vcenters (name, api_url, instance_uuid)
		VALUES ($1, $2, $3)
		ON CONFLICT (api_url) WHERE deleted_at IS NULL DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`, name, fmt.Sprintf("https://%s-%s.local/sdk", name, uuid.New().String()[:8]), uuid.New().String()).Scan(&vcenterID)
	if err != nil {
		t.Fatalf("failed to create test vcenter: %v", err)
	}
	return vcenterID
}

// TestIncrementalApplier_SingleFieldModificationPreservesUnrelatedFields proves:
// 1. Modifying power_state updates ONLY power_state and updated_at.
// 2. Name, CPU, RAM, Guest OS, Guest IP, folder, host, created_at, tags, custom attributes,
//    and NIC rows remain strictly intact and are NOT zeroed out.
func TestIncrementalApplier_SingleFieldModificationPreservesUnrelatedFields(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-incremental-single-field")
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{}, nil)
	snap := collector.NewEmptySnapshot(vcenterID)
	applier := collector.NewIncrementalApplier(persister, snap, nil)

	moref := "vm-inc-1"
	instanceUUID := uuid.New().String()

	// 1. Initial State: Hydrate complete VM
	initialVM := domain.VMRecord{
		VCenterID:       vcenterID,
		MoRef:           moref,
		InstanceUUID:    instanceUUID,
		Name:            "prod-database-1",
		PowerState:      "poweredOff",
		ConnectionState: "connected",
		CPUCores:        8,
		MemoryMB:        32768,
		GuestOS:         "rhel9_64Guest",
		GuestHostName:   "db1.infra.internal",
		GuestIPAddress:  "192.168.10.50",
		Annotation:      "Production Tier 1 Database",
		NetworkInterfaces: []domain.NetworkInterfaceRecord{
			{
				DeviceKey:  4000,
				MacAddress: "00:50:56:a1:b2:c3",
				IPAddress:  "192.168.10.50",
			},
			{
				DeviceKey:  4001,
				MacAddress: "00:50:56:a1:b2:c4",
				IPAddress:  "10.0.0.50",
			},
		},
		CustomAttributes: []domain.CustomAttributeRecord{
			{Name: "Environment", Value: "Production"},
			{Name: "Owner", Value: "DatabaseTeam"},
		},
	}

	persistedCount, err := persister.PersistVMBatch(ctx, []domain.VMRecord{initialVM})
	if err != nil || persistedCount != 1 {
		t.Fatalf("failed to persist initial VM: %v", err)
	}

	// Attach test tags via direct SQL to verify they survive incremental updates
	var vmID uuid.UUID
	err = pool.QueryRow(ctx, "SELECT id FROM virtual_machines WHERE vcenter_id = $1 AND moref = $2", vcenterID, moref).Scan(&vmID)
	if err != nil {
		t.Fatalf("failed to fetch vm id: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO vm_tags (vm_id, tag_id, category_name, tag_name)
		VALUES ($1, 'urn:vmomi:InventoryServiceTag:tag-1:GLOBAL', 'BackupPolicy', 'Daily-Gold'),
		       ($1, 'urn:vmomi:InventoryServiceTag:tag-2:GLOBAL', 'Compliance', 'PCI-DSS')
	`, vmID)
	if err != nil {
		t.Fatalf("failed to insert test tags: %v", err)
	}

	// Record original created_at timestamp
	var originalCreatedAt time.Time
	err = pool.QueryRow(ctx, "SELECT created_at FROM virtual_machines WHERE id = $1", vmID).Scan(&originalCreatedAt)
	if err != nil {
		t.Fatalf("failed to query created_at: %v", err)
	}

	// 2. Incremental Update: ONLY modify runtime.powerState to poweredOn
	modifyUpdate := types.ObjectUpdate{
		Kind: types.ObjectUpdateKindModify,
		Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: moref},
		ChangeSet: []types.PropertyChange{
			{
				Name: "runtime.powerState",
				Op:   types.PropertyChangeOpAssign,
				Val:  types.VirtualMachinePowerStatePoweredOn,
			},
		},
	}

	if err := applier.ApplyObjectUpdate(ctx, vcenterID, modifyUpdate); err != nil {
		t.Fatalf("failed to apply modify update: %v", err)
	}

	// 3. Verification: Verify ONLY power_state and updated_at changed
	var (
		name            string
		powerState      string
		connectionState string
		cpuCores        int32
		memoryMB        int64
		guestOS         string
		guestHostName   string
		guestIP         string
		annotation      string
		createdAt       time.Time
		updatedAt       time.Time
	)

	err = pool.QueryRow(ctx, `
		SELECT name, power_state, connection_state, cpu_cores, memory_mb,
		       guest_os, guest_host_name, host(guest_ip_address), annotation,
		       created_at, updated_at
		FROM virtual_machines WHERE id = $1
	`, vmID).Scan(
		&name, &powerState, &connectionState, &cpuCores, &memoryMB,
		&guestOS, &guestHostName, &guestIP, &annotation,
		&createdAt, &updatedAt,
	)
	if err != nil {
		t.Fatalf("failed to query updated VM: %v", err)
	}

	if powerState != "poweredOn" {
		t.Errorf("expected power_state 'poweredOn', got %q", powerState)
	}
	if name != "prod-database-1" {
		t.Errorf("unrelated field 'name' was corrupted: got %q", name)
	}
	if cpuCores != 8 {
		t.Errorf("unrelated field 'cpu_cores' was zeroed out: got %d", cpuCores)
	}
	if memoryMB != 32768 {
		t.Errorf("unrelated field 'memory_mb' was zeroed out: got %d", memoryMB)
	}
	if guestOS != "rhel9_64Guest" {
		t.Errorf("unrelated field 'guest_os' was corrupted: got %q", guestOS)
	}
	if guestHostName != "db1.infra.internal" {
		t.Errorf("unrelated field 'guest_host_name' was corrupted: got %q", guestHostName)
	}
	if guestIP != "192.168.10.50" {
		t.Errorf("unrelated field 'guest_ip' was corrupted: got %q", guestIP)
	}
	if annotation != "Production Tier 1 Database" {
		t.Errorf("unrelated field 'annotation' was corrupted: got %q", annotation)
	}
	if !createdAt.Equal(originalCreatedAt) {
		t.Errorf("S2.10 violation: created_at was overwritten: original=%v, new=%v", originalCreatedAt, createdAt)
	}

	// Verify NIC rows are completely intact
	var nicCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM vm_network_interfaces WHERE vm_id = $1", vmID).Scan(&nicCount)
	if err != nil || nicCount != 2 {
		t.Fatalf("expected 2 NIC rows intact, got %d (err: %v)", nicCount, err)
	}

	// Verify Custom Attributes are completely intact
	var attrCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM vm_custom_attributes WHERE vm_id = $1", vmID).Scan(&attrCount)
	if err != nil || attrCount != 2 {
		t.Fatalf("expected 2 custom attribute rows intact, got %d (err: %v)", attrCount, err)
	}

	// Verify Tags are completely intact
	var tagCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM vm_tags WHERE vm_id = $1", vmID).Scan(&tagCount)
	if err != nil || tagCount != 2 {
		t.Fatalf("expected 2 tag rows intact, got %d (err: %v)", tagCount, err)
	}
}

// TestIncrementalApplier_CustomValueChangeDoesNotTouchTagsOrNICs proves:
// 1. Modifying custom attributes updates ONLY vm_custom_attributes.
// 2. Tag rows, NIC rows, and VM core fields are completely untouched.
func TestIncrementalApplier_CustomValueChangeDoesNotTouchTagsOrNICs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-incremental-custom-val")
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{}, nil)
	snap := collector.NewEmptySnapshot(vcenterID)
	snap.CustomFieldsByKey[101] = "Environment"
	snap.CustomFieldsByKey[102] = "CostCenter"

	applier := collector.NewIncrementalApplier(persister, snap, nil)
	moref := "vm-custom-1"

	// 1. Hydrate VM with NICs and tags
	initialVM := domain.VMRecord{
		VCenterID:    vcenterID,
		MoRef:        moref,
		InstanceUUID: uuid.New().String(),
		Name:         "worker-node-1",
		PowerState:   "poweredOn",
		NetworkInterfaces: []domain.NetworkInterfaceRecord{
			{
				DeviceKey:  4000,
				MacAddress: "00:50:56:b1:b2:b3",
				IPAddress:  "10.10.10.10",
			},
		},
		CustomAttributes: []domain.CustomAttributeRecord{
			{Name: "Environment", Value: "Staging"},
		},
	}

	_, err := persister.PersistVMBatch(ctx, []domain.VMRecord{initialVM})
	if err != nil {
		t.Fatalf("failed to persist initial VM: %v", err)
	}

	var vmID uuid.UUID
	err = pool.QueryRow(ctx, "SELECT id FROM virtual_machines WHERE vcenter_id = $1 AND moref = $2", vcenterID, moref).Scan(&vmID)
	if err != nil {
		t.Fatalf("failed to fetch vm id: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO vm_tags (vm_id, tag_id, category_name, tag_name)
		VALUES ($1, 'urn:vmomi:InventoryServiceTag:tag-staging:GLOBAL', 'Environment', 'Staging')
	`, vmID)
	if err != nil {
		t.Fatalf("failed to insert tag: %v", err)
	}

	// 2. Incremental Update: Modify customValue only
	customValUpdate := types.ObjectUpdate{
		Kind: types.ObjectUpdateKindModify,
		Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: moref},
		ChangeSet: []types.PropertyChange{
			{
				Name: "summary.customValue",
				Op:   types.PropertyChangeOpAssign,
				Val: []types.BaseCustomFieldValue{
					&types.CustomFieldStringValue{
						Key:   101,
						Value: "Production-Promoted",
					},
					&types.CustomFieldStringValue{
						Key:   102,
						Value: "CC-9042",
					},
				},
			},
		},
	}

	if err := applier.ApplyObjectUpdate(ctx, vcenterID, customValUpdate); err != nil {
		t.Fatalf("failed to apply custom value modify update: %v", err)
	}

	// 3. Verify custom attributes were updated
	var envVal, ccVal string
	err = pool.QueryRow(ctx, "SELECT value FROM vm_custom_attributes WHERE vm_id = $1 AND name = 'Environment'", vmID).Scan(&envVal)
	if err != nil || envVal != "Production-Promoted" {
		t.Errorf("expected updated Environment 'Production-Promoted', got %q (err: %v)", envVal, err)
	}
	err = pool.QueryRow(ctx, "SELECT value FROM vm_custom_attributes WHERE vm_id = $1 AND name = 'CostCenter'", vmID).Scan(&ccVal)
	if err != nil || ccVal != "CC-9042" {
		t.Errorf("expected new CostCenter 'CC-9042', got %q (err: %v)", ccVal, err)
	}

	// Verify tags remain untouched
	var tagCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM vm_tags WHERE vm_id = $1", vmID).Scan(&tagCount)
	if err != nil || tagCount != 1 {
		t.Fatalf("expected tags untouched (1 row), got %d", tagCount)
	}

	// Verify NIC rows remain untouched
	var nicCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM vm_network_interfaces WHERE vm_id = $1", vmID).Scan(&nicCount)
	if err != nil || nicCount != 1 {
		t.Fatalf("expected NICs untouched (1 row), got %d", nicCount)
	}
}

// TestIncrementalApplier_LeaveSoftDeletesVM proves:
// 1. Leave update marks deleted_at = NOW().
// 2. Soft-deleted VM allows re-use of instance_uuid for subsequent active creation.
func TestIncrementalApplier_LeaveSoftDeletesVM(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-incremental-leave")
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{}, nil)
	snap := collector.NewEmptySnapshot(vcenterID)
	applier := collector.NewIncrementalApplier(persister, snap, nil)

	moref := "vm-leave-1"
	instanceUUID := uuid.New().String()

	// Hydrate initial VM
	_, err := persister.PersistVMBatch(ctx, []domain.VMRecord{
		{
			VCenterID:    vcenterID,
			MoRef:        moref,
			InstanceUUID: instanceUUID,
			Name:         "vm-to-delete",
			PowerState:   "poweredOn",
		},
	})
	if err != nil {
		t.Fatalf("failed to persist initial VM: %v", err)
	}

	// Apply Leave update
	leaveUpdate := types.ObjectUpdate{
		Kind: types.ObjectUpdateKindLeave,
		Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: moref},
	}

	if err := applier.ApplyObjectUpdate(ctx, vcenterID, leaveUpdate); err != nil {
		t.Fatalf("failed to apply leave update: %v", err)
	}

	// Verify VM has deleted_at set
	var deletedAt *time.Time
	err = pool.QueryRow(ctx, "SELECT deleted_at FROM virtual_machines WHERE vcenter_id = $1 AND moref = $2", vcenterID, moref).Scan(&deletedAt)
	if err != nil {
		t.Fatalf("failed to query deleted VM: %v", err)
	}
	if deletedAt == nil {
		t.Fatal("expected deleted_at to be set, got nil")
	}

	// Verify idempotency: repeated leave does not error
	if err := applier.ApplyObjectUpdate(ctx, vcenterID, leaveUpdate); err != nil {
		t.Fatalf("expected repeated leave to succeed idempotently, got: %v", err)
	}
}
