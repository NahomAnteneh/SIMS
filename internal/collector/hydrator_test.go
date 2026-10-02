package collector_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/vsphere"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/types"
)

func TestHydrateInventoryAndContainerViewLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	model := simulator.VPX()
	defer model.Remove()
	if err := model.Create(); err != nil {
		t.Fatalf("failed to create vcsim model: %v", err)
	}

	server := model.Service.NewServer()
	defer server.Close()

	sm, err := vsphere.NewSessionManager(vsphere.SessionConfig{
		URL:            server.URL.String(),
		Username:       "user",
		Password:       "pass",
		Insecure:       true,
		RequestTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create session manager: %v", err)
	}

	if err := sm.Login(ctx); err != nil {
		t.Fatalf("session login failed: %v", err)
	}
	defer func() {
		_ = sm.Logout(ctx)
	}()

	registry := model.Service.Context.Map

	// 1. Initial ContainerView count must be 0
	initialViews := len(registry.AllReference("ContainerView"))
	if initialViews != 0 {
		t.Fatalf("expected 0 initial ContainerViews, got %d", initialViews)
	}

	// 2. Hydrate topology
	hydrator := collector.NewHydrator(sm, collector.HydratorConfig{
		MaxObjects: 2, // Small page size to force pagination
	})

	snap, err := hydrator.HydrateTopology(ctx, "vc-sim-01")
	if err != nil {
		t.Fatalf("HydrateTopology failed: %v", err)
	}

	// Verify ContainerView was destroyed after topology hydration
	viewsAfterTopo := len(registry.AllReference("ContainerView"))
	if viewsAfterTopo != 0 {
		t.Errorf("expected 0 ContainerViews after topology hydration, found %d leaked views", viewsAfterTopo)
	}

	// 3. Hydrate VM inventory with pagination
	var totalVMs []domain.VMRecord
	batchCount := 0

	err = hydrator.HydrateInventory(ctx, "vc-sim-01", snap, func(batch []domain.VMRecord) error {
		batchCount++
		totalVMs = append(totalVMs, batch...)
		return nil
	})
	if err != nil {
		t.Fatalf("HydrateInventory failed: %v", err)
	}

	if len(totalVMs) < 4 {
		t.Errorf("expected at least 4 VMs from VPX model, got %d", len(totalVMs))
	}
	if batchCount < 2 {
		t.Errorf("expected at least 2 pages due to MaxObjects=2, got %d batches", batchCount)
	}

	// Verify ContainerView was destroyed after successful inventory hydration
	viewsAfterInv := len(registry.AllReference("ContainerView"))
	if viewsAfterInv != 0 {
		t.Errorf("expected 0 ContainerViews after inventory hydration, found %d leaked views", viewsAfterInv)
	}

	// 4. Test Error Path: callback returns error -> ContainerView must still be destroyed!
	_ = hydrator.HydrateInventory(ctx, "vc-sim-01", snap, func(batch []domain.VMRecord) error {
		return errors.New("simulated error during batch processing")
	})

	viewsAfterError := len(registry.AllReference("ContainerView"))
	if viewsAfterError != 0 {
		t.Errorf("expected 0 ContainerViews after error exit path, found %d leaked views", viewsAfterError)
	}

	// 5. Test Context Cancellation Path -> ContainerView must still be destroyed!
	cancelledCtx, cancelNow := context.WithCancel(context.Background())
	cancelNow() // Cancel immediately

	_ = hydrator.HydrateInventory(cancelledCtx, "vc-sim-01", snap, func(batch []domain.VMRecord) error {
		return nil
	})

	viewsAfterCancel := len(registry.AllReference("ContainerView"))
	if viewsAfterCancel != 0 {
		t.Errorf("expected 0 ContainerViews after context cancellation, found %d leaked views", viewsAfterCancel)
	}
}

type failingTagFetcher struct{}

func (f *failingTagFetcher) FetchTagsForObjects(_ context.Context, _ []types.ManagedObjectReference) (map[string][]domain.TagRecord, error) {
	return nil, collector.ErrTaggingUnavailable
}

func (f *failingTagFetcher) Close(_ context.Context) error {
	return nil
}

type mockTagFetcher struct {
	tagsByMoRef map[string][]domain.TagRecord
}

func (m *mockTagFetcher) FetchTagsForObjects(_ context.Context, refs []types.ManagedObjectReference) (map[string][]domain.TagRecord, error) {
	res := make(map[string][]domain.TagRecord)
	for _, r := range refs {
		if t, ok := m.tagsByMoRef[r.Value]; ok {
			res[r.Value] = t
		}
	}
	return res, nil
}

func (m *mockTagFetcher) Close(_ context.Context) error {
	return nil
}

func TestHydrateInventory_TagFetcherDegradationAndEnrichment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	model := simulator.VPX()
	defer model.Remove()
	if err := model.Create(); err != nil {
		t.Fatalf("failed to create vcsim model: %v", err)
	}

	server := model.Service.NewServer()
	defer server.Close()

	sm, err := vsphere.NewSessionManager(vsphere.SessionConfig{
		URL:            server.URL.String(),
		Username:       "user",
		Password:       "pass",
		Insecure:       true,
		RequestTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create session manager: %v", err)
	}
	if err := sm.Login(ctx); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	defer func() { _ = sm.Logout(ctx) }()

	hydrator := collector.NewHydrator(sm, collector.HydratorConfig{MaxObjects: 100})
	snap, err := hydrator.HydrateTopology(ctx, "vc-sim-01")
	if err != nil {
		t.Fatalf("HydrateTopology failed: %v", err)
	}

	t.Run("tag fetcher failure does not fail inventory hydration", func(t *testing.T) {
		hydrator.SetTagFetcher(&failingTagFetcher{})
		batchCount := 0
		vmCount := 0
		err := hydrator.HydrateInventory(ctx, "vc-sim-01", snap, func(batch []domain.VMRecord) error {
			batchCount++
			vmCount += len(batch)
			for _, vm := range batch {
				if len(vm.Tags) != 0 {
					t.Errorf("expected no tags when tag fetcher fails, got %d", len(vm.Tags))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("HydrateInventory unexpectedly failed when TagFetcher degraded: %v", err)
		}
		if vmCount == 0 {
			t.Fatalf("expected VMs to be hydrated despite tag degradation")
		}
	})

	t.Run("tag fetcher successfully enriches VM records", func(t *testing.T) {
		vms := model.Service.Context.Map.All("VirtualMachine")
		if len(vms) == 0 {
			t.Fatalf("expected at least 1 VM")
		}
		targetMoref := vms[0].Reference().Value

		mockFetcher := &mockTagFetcher{
			tagsByMoRef: map[string][]domain.TagRecord{
				targetMoref: {
					{TagID: "tag-1", CategoryName: "Environment", TagName: "Production"},
					{TagID: "tag-2", CategoryName: "Tier", TagName: "Frontend"},
				},
			},
		}
		hydrator.SetTagFetcher(mockFetcher)

		var targetFound *domain.VMRecord
		err := hydrator.HydrateInventory(ctx, "vc-sim-01", snap, func(batch []domain.VMRecord) error {
			for i := range batch {
				if batch[i].MoRef == targetMoref {
					targetFound = &batch[i]
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("HydrateInventory failed: %v", err)
		}
		if targetFound == nil {
			t.Fatalf("target VM %s not found in hydrated batch", targetMoref)
		}
		if len(targetFound.Tags) != 2 {
			t.Fatalf("expected 2 tags enriched on VM, got %d", len(targetFound.Tags))
		}
		if targetFound.Tags[0].TagName != "Production" || targetFound.Tags[1].TagName != "Frontend" {
			t.Errorf("unexpected tags: %+v", targetFound.Tags)
		}
	})
}

func TestHydrateInventoryAndTopology_CustomFields(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	model := simulator.VPX()
	defer model.Remove()
	if err := model.Create(); err != nil {
		t.Fatalf("failed to create vcsim model: %v", err)
	}

	server := model.Service.NewServer()
	defer server.Close()

	sm, err := vsphere.NewSessionManager(vsphere.SessionConfig{
		URL:            server.URL.String(),
		Username:       "user",
		Password:       "pass",
		Insecure:       true,
		RequestTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create session manager: %v", err)
	}
	if err := sm.Login(ctx); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	defer func() { _ = sm.Logout(ctx) }()

	client := sm.Client()
	cfm, err := object.GetCustomFieldsManager(client.Client)
	if err != nil {
		t.Fatalf("failed to get CustomFieldsManager: %v", err)
	}

	fieldDef, err := cfm.Add(ctx, "CostCenter", "VirtualMachine", nil, nil)
	if err != nil {
		t.Fatalf("failed to add custom field: %v", err)
	}

	vms := model.Service.Context.Map.All("VirtualMachine")
	if len(vms) == 0 {
		t.Fatalf("expected at least 1 VM")
	}
	targetVMRef := vms[0].Reference()

	if err := cfm.Set(ctx, targetVMRef, fieldDef.Key, "Finance-101"); err != nil {
		t.Fatalf("failed to set custom field on VM: %v", err)
	}

	hydrator := collector.NewHydrator(sm, collector.HydratorConfig{MaxObjects: 100})
	snap, err := hydrator.HydrateTopology(ctx, "vc-sim-01")
	if err != nil {
		t.Fatalf("HydrateTopology failed: %v", err)
	}

	if snap.CustomFieldsByKey[fieldDef.Key] != "CostCenter" {
		t.Fatalf("expected CustomFieldsByKey to contain %d->CostCenter, got: %+v", fieldDef.Key, snap.CustomFieldsByKey)
	}

	var foundVM *domain.VMRecord
	err = hydrator.HydrateInventory(ctx, "vc-sim-01", snap, func(batch []domain.VMRecord) error {
		for i := range batch {
			if batch[i].MoRef == targetVMRef.Value {
				foundVM = &batch[i]
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("HydrateInventory failed: %v", err)
	}

	if foundVM == nil {
		t.Fatalf("target VM not found in batch")
	}
	if len(foundVM.CustomAttributes) != 1 {
		t.Fatalf("expected 1 custom attribute on VM, got %d", len(foundVM.CustomAttributes))
	}
	if foundVM.CustomAttributes[0].Name != "CostCenter" || foundVM.CustomAttributes[0].Value != "Finance-101" {
		t.Errorf("unexpected custom attribute: %+v", foundVM.CustomAttributes[0])
	}
}
