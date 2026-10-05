package collector_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/vmware/govmomi/vim25/types"
)

type mockBatchApplier struct {
	mu           sync.Mutex
	applied      []types.ObjectUpdate
	failCount    int
	maxFailures  int
	callCount    int
	failErr      error
	delayPerCall time.Duration
}

func (m *mockBatchApplier) ApplyBatch(ctx context.Context, _ string, updates []types.ObjectUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++

	if m.delayPerCall > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.delayPerCall):
		}
	}

	if m.failCount < m.maxFailures {
		m.failCount++
		if m.failErr != nil {
			return m.failErr
		}
		return errors.New("simulated database connection refused")
	}

	m.applied = append(m.applied, updates...)
	return nil
}

func (m *mockBatchApplier) AppliedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.applied)
}

func TestStreamBatcher_NormalBatching(t *testing.T) {
	metrics := telemetry.NewMetrics("test", "0.1.0")
	queue := collector.NewBoundedQueue[types.ObjectUpdate]("vc-batch-test", 100, metrics)
	applier := &mockBatchApplier{}

	cfg := collector.BatcherConfig{
		VCenterID:     "vc-batch-test",
		MaxBatchSize:  5,
		FlushInterval: 10 * time.Millisecond,
		DrainTimeout:  1 * time.Second,
		Backoff: collector.BackoffConfig{
			BaseDelay: 10 * time.Millisecond,
			MaxDelay:  50 * time.Millisecond,
			Factor:    2.0,
		},
	}

	batcher := collector.NewStreamBatcher(cfg, queue, applier, metrics, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = batcher.Run(ctx)
	}()

	// Push 12 updates
	for i := 1; i <= 12; i++ {
		err := queue.Push(ctx, types.ObjectUpdate{
			Kind: types.ObjectUpdateKindModify,
			Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-test"},
		})
		if err != nil {
			t.Fatalf("push failed: %v", err)
		}
	}

	// Wait for batcher to process
	deadline := time.Now().Add(2 * time.Second)
	for applier.AppliedCount() < 12 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	batcher.Stop()
	wg.Wait()

	if applier.AppliedCount() != 12 {
		t.Fatalf("expected 12 applied updates, got %d", applier.AppliedCount())
	}
}

func TestStreamBatcher_RetryOnDatabaseFailure(t *testing.T) {
	metrics := telemetry.NewMetrics("test", "0.1.0")
	queue := collector.NewBoundedQueue[types.ObjectUpdate]("vc-fail-test", 10, metrics)
	applier := &mockBatchApplier{
		maxFailures: 3, // Fail 3 times before succeeding
		failErr:     errors.New("db outage: connection refused"),
	}

	cfg := collector.BatcherConfig{
		VCenterID:     "vc-fail-test",
		MaxBatchSize:  5,
		FlushInterval: 10 * time.Millisecond,
		DrainTimeout:  1 * time.Second,
		Backoff: collector.BackoffConfig{
			BaseDelay: 10 * time.Millisecond,
			MaxDelay:  50 * time.Millisecond,
			Factor:    1.5,
		},
	}

	batcher := collector.NewStreamBatcher(cfg, queue, applier, metrics, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = batcher.Run(ctx)
	}()

	// Push 3 updates
	for i := 1; i <= 3; i++ {
		_ = queue.Push(ctx, types.ObjectUpdate{
			Kind: types.ObjectUpdateKindEnter,
			Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-retry"},
		})
	}

	// Wait for retries to complete and succeed
	deadline := time.Now().Add(3 * time.Second)
	for applier.AppliedCount() < 3 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	batcher.Stop()
	wg.Wait()

	if applier.AppliedCount() != 3 {
		t.Fatalf("expected 3 applied updates after recovery, got %d", applier.AppliedCount())
	}
	if applier.callCount < 4 {
		t.Errorf("expected at least 4 attempts (3 failures + 1 success), got %d", applier.callCount)
	}
}

func TestStreamBatcher_BackpressureFillsQueueWhileDatabaseDown(t *testing.T) {
	metrics := telemetry.NewMetrics("test", "0.1.0")
	queueCapacity := 4
	queue := collector.NewBoundedQueue[types.ObjectUpdate]("vc-backpressure", queueCapacity, metrics)
	applier := &mockBatchApplier{
		maxFailures: 999999, // Persistent DB outage
		failErr:     errors.New("PostgreSQL unavailable"),
	}

	cfg := collector.BatcherConfig{
		VCenterID:     "vc-backpressure",
		MaxBatchSize:  2,
		FlushInterval: 10 * time.Millisecond,
		DrainTimeout:  1 * time.Second,
		Backoff: collector.BackoffConfig{
			BaseDelay: 50 * time.Millisecond,
			MaxDelay:  100 * time.Millisecond,
		},
	}

	batcher := collector.NewStreamBatcher(cfg, queue, applier, metrics, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = batcher.Run(ctx)
	}()

	// Push initial batch to trigger batcher and enter retry loop
	_ = queue.Push(ctx, types.ObjectUpdate{Kind: types.ObjectUpdateKindModify, Obj: types.ManagedObjectReference{Value: "vm-1"}})
	_ = queue.Push(ctx, types.ObjectUpdate{Kind: types.ObjectUpdateKindModify, Obj: types.ManagedObjectReference{Value: "vm-2"}})

	// Wait until batcher enters retry loop
	for i := 0; i < 50; i++ {
		applier.mu.Lock()
		calls := applier.callCount
		applier.mu.Unlock()
		if calls >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Fill remaining queue space to capacity (4 items)
	for queue.Depth() < queueCapacity {
		_ = queue.Push(ctx, types.ObjectUpdate{
			Kind: types.ObjectUpdateKindModify,
			Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-bp"},
		})
	}

	if queue.Depth() != queueCapacity {
		t.Fatalf("expected queue depth %d, got %d", queueCapacity, queue.Depth())
	}

	// Next push must block due to capacity
	producerBlocked := make(chan error, 1)
	go func() {
		producerBlocked <- queue.Push(ctx, types.ObjectUpdate{
			Kind: types.ObjectUpdateKindModify,
			Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-blocked"},
		})
	}()

	time.Sleep(50 * time.Millisecond)
	if queue.BlockedProducers() != 1 {
		t.Errorf("expected 1 blocked producer under DB outage, got %d", queue.BlockedProducers())
	}

	// Cancel context to cleanly shut down
	cancel()
	batcher.Stop()
}

func TestStreamBatcher_DrainFlushesRemainingItems(t *testing.T) {
	metrics := telemetry.NewMetrics("test", "0.1.0")
	queue := collector.NewBoundedQueue[types.ObjectUpdate]("vc-drain-test", 10, metrics)
	applier := &mockBatchApplier{}

	cfg := collector.BatcherConfig{
		VCenterID:     "vc-drain-test",
		MaxBatchSize:  5,
		FlushInterval: 10 * time.Millisecond,
		DrainTimeout:  1 * time.Second,
	}

	batcher := collector.NewStreamBatcher(cfg, queue, applier, metrics, nil, nil)
	ctx := context.Background()

	// Push 4 items without running batcher loop
	for i := 1; i <= 4; i++ {
		_ = queue.Push(ctx, types.ObjectUpdate{
			Kind: types.ObjectUpdateKindModify,
			Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-drain"},
		})
	}

	if queue.Depth() != 4 {
		t.Fatalf("expected queue depth 4, got %d", queue.Depth())
	}

	// Drain must flush all 4 items
	if err := batcher.Drain(ctx); err != nil {
		t.Fatalf("Drain failed: %v", err)
	}

	if queue.Depth() != 0 {
		t.Errorf("expected queue depth 0 after drain, got %d", queue.Depth())
	}
	if applier.AppliedCount() != 4 {
		t.Errorf("expected 4 applied items, got %d", applier.AppliedCount())
	}
}
