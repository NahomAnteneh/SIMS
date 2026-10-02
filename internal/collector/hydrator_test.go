package collector_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/vsphere"
	"github.com/vmware/govmomi/simulator"
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
