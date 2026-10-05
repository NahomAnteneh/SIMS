package collector_test

import (
	"context"
	"testing"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/vmware/govmomi/vim25/types"
)

type mockPersister struct {
	persistedBatch  []domain.VMRecord
	appliedMasks    map[string]collector.VMFieldMask
	leaveCalls      map[string]bool
	failIncremental error
}

func newMockPersister() *mockPersister {
	return &mockPersister{
		appliedMasks: make(map[string]collector.VMFieldMask),
		leaveCalls:   make(map[string]bool),
	}
}

func (m *mockPersister) PersistVMBatch(_ context.Context, batch []domain.VMRecord) (int, error) {
	m.persistedBatch = append(m.persistedBatch, batch...)
	return len(batch), nil
}

func (m *mockPersister) ApplyVMIncrementalUpdate(_ context.Context, vcenterID, moref string, mask collector.VMFieldMask) error {
	if m.failIncremental != nil {
		return m.failIncremental
	}
	m.appliedMasks[vcenterID+":"+moref] = mask
	return nil
}

func (m *mockPersister) ApplyVMLeave(_ context.Context, vcenterID, moref string) error {
	m.leaveCalls[vcenterID+":"+moref] = true
	return nil
}

func TestIncrementalApplier_ModifyFieldMaskExtraction(t *testing.T) {
	persister := newMockPersister()
	snap := collector.NewEmptySnapshot("vc-1")
	applier := collector.NewIncrementalApplier(persister, snap, nil)

	ctx := context.Background()
	moref := "vm-test-1"

	update := types.ObjectUpdate{
		Kind: types.ObjectUpdateKindModify,
		Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: moref},
		ChangeSet: []types.PropertyChange{
			{
				Name: "runtime.powerState",
				Op:   types.PropertyChangeOpAssign,
				Val:  types.VirtualMachinePowerStatePoweredOn,
			},
			{
				Name: "name",
				Op:   types.PropertyChangeOpAssign,
				Val:  "new-vm-name",
			},
			{
				Name: "summary.config",
				Op:   types.PropertyChangeOpAssign,
				Val: types.VirtualMachineConfigSummary{
					NumCpu:       16,
					MemorySizeMB: 65536,
				},
			},
		},
	}

	if err := applier.ApplyObjectUpdate(ctx, "vc-1", update); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mask, ok := persister.appliedMasks["vc-1:"+moref]
	if !ok {
		t.Fatal("expected mask to be recorded for vc-1:vm-test-1")
	}

	if mask.PowerState == nil || *mask.PowerState != "poweredOn" {
		t.Errorf("expected PowerState 'poweredOn', got: %v", mask.PowerState)
	}
	if mask.Name == nil || *mask.Name != "new-vm-name" {
		t.Errorf("expected Name 'new-vm-name', got: %v", mask.Name)
	}
	if mask.CPUCores == nil || *mask.CPUCores != 16 {
		t.Errorf("expected CPUCores 16, got: %v", mask.CPUCores)
	}
	if mask.MemoryMB == nil || *mask.MemoryMB != 65536 {
		t.Errorf("expected MemoryMB 65536, got: %v", mask.MemoryMB)
	}
	// Untouched fields must be nil
	if mask.GuestOS != nil {
		t.Errorf("expected GuestOS to be nil, got: %v", mask.GuestOS)
	}
	if mask.GuestIPAddress != nil {
		t.Errorf("expected GuestIPAddress to be nil, got: %v", mask.GuestIPAddress)
	}
}

func TestIncrementalApplier_LeaveMapping(t *testing.T) {
	persister := newMockPersister()
	applier := collector.NewIncrementalApplier(persister, nil, nil)

	ctx := context.Background()
	moref := "vm-leave-99"

	update := types.ObjectUpdate{
		Kind: types.ObjectUpdateKindLeave,
		Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: moref},
	}

	if err := applier.ApplyObjectUpdate(ctx, "vc-1", update); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !persister.leaveCalls["vc-1:"+moref] {
		t.Error("expected ApplyVMLeave to be called for vc-1:vm-leave-99")
	}
}

func TestIncrementalApplier_EnterMapping(t *testing.T) {
	persister := newMockPersister()
	snap := collector.NewEmptySnapshot("vc-1")
	applier := collector.NewIncrementalApplier(persister, snap, nil)

	ctx := context.Background()
	moref := "vm-enter-100"

	update := types.ObjectUpdate{
		Kind: types.ObjectUpdateKindEnter,
		Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: moref},
		ChangeSet: []types.PropertyChange{
			{
				Name: "name",
				Op:   types.PropertyChangeOpAssign,
				Val:  "entered-vm",
			},
			{
				Name: "runtime.powerState",
				Op:   types.PropertyChangeOpAssign,
				Val:  types.VirtualMachinePowerStatePoweredOff,
			},
		},
	}

	if err := applier.ApplyObjectUpdate(ctx, "vc-1", update); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should try incremental first, which succeeded on mock
	if _, ok := persister.appliedMasks["vc-1:"+moref]; !ok {
		t.Fatal("expected incremental update or batch insert for Enter")
	}
}

func TestIncrementalApplier_NonVMObjectIgnored(t *testing.T) {
	persister := newMockPersister()
	applier := collector.NewIncrementalApplier(persister, nil, nil)

	ctx := context.Background()
	update := types.ObjectUpdate{
		Kind: types.ObjectUpdateKindModify,
		Obj:  types.ManagedObjectReference{Type: "HostSystem", Value: "host-1"},
	}

	if err := applier.ApplyObjectUpdate(ctx, "vc-1", update); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(persister.appliedMasks) != 0 || len(persister.leaveCalls) != 0 {
		t.Error("expected HostSystem update to be ignored by VM incremental applier")
	}
}
