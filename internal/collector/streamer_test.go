package collector_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/infrastructure/sims/internal/vsphere"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/vmware/govmomi/vim25/types"
)

func TestPropertyStreamer_SingleGoroutineOwnership(t *testing.T) {
	fakePC := vsphere.NewFakePropertyCollector()
	metrics := telemetry.NewMetrics("test", "0.1.0")
	cfg := collector.StreamerConfig{
		VCenterID:              "vc-1",
		MaxWaitSeconds:         1,
		MaxObjects:             10,
		MaxStreamBlockDuration: 5 * time.Second,
	}

	streamer := collector.NewPropertyStreamer(fakePC, types.PropertyFilterSpec{}, cfg, metrics, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	// Launch first runner
	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- streamer.Run(ctx, func(_ context.Context, _ []types.ObjectUpdate) error {
			return nil
		})
	}()

	// Wait briefly to ensure first runner has started
	time.Sleep(20 * time.Millisecond)

	// Attempt concurrent second runner
	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- streamer.Run(ctx, func(_ context.Context, _ []types.ObjectUpdate) error {
			return nil
		})
	}()

	// Second runner must fail immediately with ErrStreamerAlreadyRunning
	var receivedErr error
	select {
	case err := <-errCh:
		receivedErr = err
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for second runner to be rejected")
	}

	if !errors.Is(receivedErr, collector.ErrStreamerAlreadyRunning) {
		t.Fatalf("expected ErrStreamerAlreadyRunning, got: %v", receivedErr)
	}

	// Clean up first runner
	cancel()
	wg.Wait()
}

func TestPropertyStreamer_UpdateDeliveryAndVersionTracking(t *testing.T) {
	fakePC := vsphere.NewFakePropertyCollector()
	metrics := telemetry.NewMetrics("test", "0.1.0")
	cfg := collector.StreamerConfig{
		VCenterID:              "vc-test",
		MaxWaitSeconds:         1,
		MaxObjects:             100,
		MaxStreamBlockDuration: 5 * time.Second,
	}

	// Enqueue test updates
	update1 := &types.UpdateSet{
		Version: "version-2",
		FilterSet: []types.PropertyFilterUpdate{
			{
				Filter: types.ManagedObjectReference{Type: "PropertyFilter", Value: "filter-1"},
				ObjectSet: []types.ObjectUpdate{
					{
						Kind: types.ObjectUpdateKindEnter,
						Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-1"},
						ChangeSet: []types.PropertyChange{
							{
								Name: "name",
								Op:   types.PropertyChangeOpAssign,
								Val:  "web-server-1",
							},
						},
					},
					{
						Kind: types.ObjectUpdateKindModify,
						Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-2"},
						ChangeSet: []types.PropertyChange{
							{
								Name: "runtime.powerState",
								Op:   types.PropertyChangeOpAssign,
								Val:  types.VirtualMachinePowerStatePoweredOn,
							},
						},
					},
				},
			},
		},
	}
	fakePC.EnqueueUpdate(update1)

	streamer := collector.NewPropertyStreamer(fakePC, types.PropertyFilterSpec{}, cfg, metrics, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var receivedUpdates []types.ObjectUpdate
	var mu sync.Mutex

	go func() {
		_ = streamer.Run(ctx, func(_ context.Context, updates []types.ObjectUpdate) error {
			mu.Lock()
			receivedUpdates = append(receivedUpdates, updates...)
			mu.Unlock()
			cancel() // Stop after receiving the first batch
			return nil
		})
	}()

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for update delivery")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(receivedUpdates) != 2 {
		t.Fatalf("expected 2 object updates, got %d", len(receivedUpdates))
	}
	if receivedUpdates[0].Obj.Value != "vm-1" || receivedUpdates[0].Kind != types.ObjectUpdateKindEnter {
		t.Errorf("unexpected first update: %+v", receivedUpdates[0])
	}
	if receivedUpdates[1].Obj.Value != "vm-2" || receivedUpdates[1].Kind != types.ObjectUpdateKindModify {
		t.Errorf("unexpected second update: %+v", receivedUpdates[1])
	}

	if streamer.Version() != "version-2" {
		t.Errorf("expected version 'version-2', got %q", streamer.Version())
	}
}

func TestPropertyStreamer_TruncatedUpdatesHandling(t *testing.T) {
	fakePC := vsphere.NewFakePropertyCollector()
	metrics := telemetry.NewMetrics("test", "0.1.0")
	cfg := collector.StreamerConfig{
		VCenterID:              "vc-trunc",
		MaxWaitSeconds:         1,
		MaxObjects:             10,
		MaxStreamBlockDuration: 5 * time.Second,
	}

	// Enqueue 3 batches: batch1 (truncated=true), batch2 (truncated=true), batch3 (truncated=false)
	truncatedTrue := true
	truncatedFalse := false

	batch1 := &types.UpdateSet{
		Version:   "ver-trunc-1",
		Truncated: &truncatedTrue,
		FilterSet: []types.PropertyFilterUpdate{
			{
				Filter: types.ManagedObjectReference{Type: "PropertyFilter", Value: "filter-1"},
				ObjectSet: []types.ObjectUpdate{
					{
						Kind: types.ObjectUpdateKindEnter,
						Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-part1"},
					},
					{
						Kind: types.ObjectUpdateKindEnter,
						Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-part2"},
					},
				},
			},
		},
	}
	batch2 := &types.UpdateSet{
		Version:   "ver-trunc-2",
		Truncated: &truncatedTrue,
		FilterSet: []types.PropertyFilterUpdate{
			{
				Filter: types.ManagedObjectReference{Type: "PropertyFilter", Value: "filter-1"},
				ObjectSet: []types.ObjectUpdate{
					{
						Kind: types.ObjectUpdateKindModify,
						Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-part3"},
					},
				},
			},
		},
	}
	batch3 := &types.UpdateSet{
		Version:   "ver-trunc-3",
		Truncated: &truncatedFalse,
		FilterSet: []types.PropertyFilterUpdate{
			{
				Filter: types.ManagedObjectReference{Type: "PropertyFilter", Value: "filter-1"},
				ObjectSet: []types.ObjectUpdate{
					{
						Kind: types.ObjectUpdateKindLeave,
						Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-part4"},
					},
				},
			},
		},
	}

	fakePC.EnqueueUpdate(batch1)
	fakePC.EnqueueUpdate(batch2)
	fakePC.EnqueueUpdate(batch3)

	streamer := collector.NewPropertyStreamer(fakePC, types.PropertyFilterSpec{}, cfg, metrics, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var receivedUpdates []types.ObjectUpdate
	var mu sync.Mutex

	go func() {
		_ = streamer.Run(ctx, func(_ context.Context, updates []types.ObjectUpdate) error {
			mu.Lock()
			receivedUpdates = append(receivedUpdates, updates...)
			count := len(receivedUpdates)
			if count >= 4 {
				cancel()
			}
			mu.Unlock()
			return nil
		})
	}()

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for truncated batch processing")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(receivedUpdates) != 4 {
		t.Fatalf("expected 4 updates drained across truncated batches, got %d", len(receivedUpdates))
	}
	if receivedUpdates[0].Obj.Value != "vm-part1" ||
		receivedUpdates[1].Obj.Value != "vm-part2" ||
		receivedUpdates[2].Obj.Value != "vm-part3" ||
		receivedUpdates[3].Obj.Value != "vm-part4" {
		t.Fatalf("unexpected order or values of received updates: %+v", receivedUpdates)
	}
	if streamer.Version() != "ver-trunc-3" {
		t.Errorf("expected final version 'ver-trunc-3', got %q", streamer.Version())
	}

	// Verify sims_collector_property_updates_truncated_total metric
	truncatedCount := testutil.ToFloat64(metrics.PropertyUpdatesTruncatedTotal.WithLabelValues("vc-trunc"))
	if truncatedCount != 2 {
		t.Errorf("expected truncated counter to be 2, got %v", truncatedCount)
	}
}

func TestPropertyStreamer_InvalidCollectorVersion(t *testing.T) {
	fakePC := vsphere.NewFakePropertyCollector()
	fakePC.InjectInvalidCollectorVersion()

	cfg := collector.StreamerConfig{
		VCenterID:              "vc-fault",
		MaxWaitSeconds:         1,
		MaxStreamBlockDuration: 5 * time.Second,
	}

	streamer := collector.NewPropertyStreamer(fakePC, types.PropertyFilterSpec{}, cfg, nil, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := streamer.Run(ctx, func(_ context.Context, _ []types.ObjectUpdate) error {
		return nil
	})

	if err == nil {
		t.Fatal("expected Run to return error on InvalidCollectorVersion fault, got nil")
	}
	if !errors.Is(err, collector.ErrInvalidCollectorVersion) {
		t.Fatalf("expected ErrInvalidCollectorVersion, got: %v", err)
	}
}

func TestPropertyStreamer_ProactiveHangDetection(t *testing.T) {
	fakePC := vsphere.NewFakePropertyCollector()
	fakePC.InjectHungWaitForUpdates(500 * time.Millisecond)

	cfg := collector.StreamerConfig{
		VCenterID:              "vc-hang",
		MaxWaitSeconds:         1,
		CallDeadline:           50 * time.Millisecond, // Per-call deadline below fake delay (hang detection)
		MaxStreamBlockDuration: 5 * time.Minute,
	}

	streamer := collector.NewPropertyStreamer(fakePC, types.PropertyFilterSpec{}, cfg, nil, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := streamer.Run(ctx, func(_ context.Context, _ []types.ObjectUpdate) error {
		return nil
	})

	if err == nil {
		t.Fatal("expected Run to abort on proactive hang detection, got nil")
	}
	if !errors.Is(err, collector.ErrStreamHung) {
		t.Fatalf("expected ErrStreamHung, got: %v", err)
	}
	if fakePC.CanceledWaits() == 0 {
		t.Error("expected CancelWaitForUpdates to be called upon proactive hang detection")
	}
}

func TestPropertyStreamer_CleanupRegistryIntegration(t *testing.T) {
	fakePC := vsphere.NewFakePropertyCollector()
	registry := collector.NewCleanupRegistry(nil, 2*time.Second)

	cfg := collector.StreamerConfig{
		VCenterID:      "vc-reg",
		MaxWaitSeconds: 1,
	}

	streamer := collector.NewPropertyStreamer(fakePC, types.PropertyFilterSpec{}, cfg, nil, nil, registry)
	ctx := context.Background()

	if err := streamer.InitFilter(ctx); err != nil {
		t.Fatalf("failed to init filter: %v", err)
	}

	if registry.Count() != 1 {
		t.Fatalf("expected 1 registered resource in CleanupRegistry, got %d", registry.Count())
	}

	// Teardown registry and verify filter was destroyed
	teardownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := registry.Teardown(teardownCtx); err != nil {
		t.Fatalf("expected successful teardown, got error: %v", err)
	}

	if len(fakePC.CreatedFilters()) == 0 {
		t.Fatal("expected at least one filter created")
	}
	if !fakePC.CreatedFilters()[0].Destroyed {
		t.Error("expected property filter to be destroyed during CleanupRegistry teardown")
	}
}

func TestPropertyStreamer_QueueBackpressureProactivelyResyncs(t *testing.T) {
	fakePC := vsphere.NewFakePropertyCollector()
	metrics := telemetry.NewMetrics("test", "0.1.0")

	// Pre-enqueue one update so WaitForUpdatesEx returns immediately
	fakePC.EnqueueUpdate(&types.UpdateSet{
		Version: "ver-bp-1",
		FilterSet: []types.PropertyFilterUpdate{
			{
				Filter: types.ManagedObjectReference{Type: "PropertyFilter", Value: "filter-1"},
				ObjectSet: []types.ObjectUpdate{
					{Kind: types.ObjectUpdateKindModify, Obj: types.ManagedObjectReference{Value: "vm-1"}},
				},
			},
		},
	})

	// Bounded queue with capacity 1, pre-filled
	queue := collector.NewBoundedQueue[types.ObjectUpdate]("vc-bp-stream", 1, metrics)
	_ = queue.Push(context.Background(), types.ObjectUpdate{Kind: types.ObjectUpdateKindModify, Obj: types.ManagedObjectReference{Value: "vm-existing"}})

	cfg := collector.StreamerConfig{
		VCenterID:              "vc-bp-stream",
		MaxWaitSeconds:         1,
		CallDeadline:           1 * time.Second,
		MaxStreamBlockDuration: 50 * time.Millisecond, // Proactive backpressure limit
	}

	streamer := collector.NewPropertyStreamer(fakePC, types.PropertyFilterSpec{}, cfg, metrics, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Handler attempts to push into full bounded queue with MaxStreamBlockDuration
	err := streamer.Run(ctx, func(handlerCtx context.Context, updates []types.ObjectUpdate) error {
		pushCtx, cancelPush := context.WithTimeout(handlerCtx, cfg.MaxStreamBlockDuration)
		defer cancelPush()

		pushErr := queue.PushAll(pushCtx, updates)
		if pushErr != nil {
			if errors.Is(pushErr, context.DeadlineExceeded) {
				return collector.ErrStreamHung
			}
			return pushErr
		}
		return nil
	})

	if err == nil {
		t.Fatal("expected streamer.Run to abort on backpressure block timeout, got nil")
	}
	if !errors.Is(err, collector.ErrStreamHung) {
		t.Fatalf("expected ErrStreamHung, got: %v", err)
	}

	// Broadcom invariant: while blocked, streamer did not loop back to call WaitForUpdatesEx again
	if fakePC.WaitForUpdatesCount() != 1 {
		t.Errorf("expected exactly 1 WaitForUpdatesEx call, got %d", fakePC.WaitForUpdatesCount())
	}
}
