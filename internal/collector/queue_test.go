package collector_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestBoundedQueue_BasicPushPop(t *testing.T) {
	metrics := telemetry.NewMetrics("test", "0.1.0")
	q := collector.NewBoundedQueue[string]("vc-1", 5, metrics)

	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		if err := q.Push(ctx, fmt.Sprintf("item-%d", i)); err != nil {
			t.Fatalf("failed to push item-%d: %v", i, err)
		}
	}

	if q.Depth() != 5 {
		t.Errorf("expected depth 5, got %d", q.Depth())
	}
	if q.FillRatio() != 1.0 {
		t.Errorf("expected fill ratio 1.0, got %f", q.FillRatio())
	}

	// Verify FIFO order
	for i := 1; i <= 5; i++ {
		item, err := q.Pop(ctx)
		if err != nil {
			t.Fatalf("failed to pop item: %v", err)
		}
		expected := fmt.Sprintf("item-%d", i)
		if item != expected {
			t.Errorf("expected %s, got %s", expected, item)
		}
	}

	if q.Depth() != 0 {
		t.Errorf("expected empty depth 0, got %d", q.Depth())
	}
	if q.FillRatio() != 0.0 {
		t.Errorf("expected fill ratio 0.0, got %f", q.FillRatio())
	}
}

func TestBoundedQueue_BlocksWhenFullAndUnblocksOnPop(t *testing.T) {
	metrics := telemetry.NewMetrics("test", "0.1.0")
	capacity := 3
	q := collector.NewBoundedQueue[int]("vc-full", capacity, metrics)

	ctx := context.Background()

	// Fill queue to capacity
	for i := 1; i <= capacity; i++ {
		if err := q.Push(ctx, i); err != nil {
			t.Fatalf("push failed: %v", err)
		}
	}

	if q.Depth() != capacity {
		t.Fatalf("expected depth %d, got %d", capacity, q.Depth())
	}
	if q.BlockedProducers() != 0 {
		t.Errorf("expected 0 blocked producers initially, got %d", q.BlockedProducers())
	}

	// Start goroutine to push an extra item (must block)
	blockedPushed := make(chan error, 1)
	go func() {
		blockedPushed <- q.Push(ctx, 42)
	}()

	// Wait briefly and verify producer is blocked
	time.Sleep(50 * time.Millisecond)
	if q.BlockedProducers() != 1 {
		t.Errorf("expected 1 blocked producer, got %d", q.BlockedProducers())
	}
	blockedGauge := testutil.ToFloat64(metrics.QueueBlockedProducers.WithLabelValues("vc-full"))
	if blockedGauge != 1.0 {
		t.Errorf("expected metric sims_queue_blocked_producers=1, got %f", blockedGauge)
	}

	// Pop one item to unblock the producer
	item, err := q.Pop(ctx)
	if err != nil {
		t.Fatalf("pop failed: %v", err)
	}
	if item != 1 {
		t.Errorf("expected item 1, got %d", item)
	}

	// Wait for producer to unblock
	select {
	case err := <-blockedPushed:
		if err != nil {
			t.Fatalf("expected blocked push to succeed once space opened, got: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for blocked producer to unblock")
	}

	// Verify blocked producers returned to 0
	if q.BlockedProducers() != 0 {
		t.Errorf("expected 0 blocked producers after unblock, got %d", q.BlockedProducers())
	}
}

func TestBoundedQueue_SlowConsumerNeverDropsOrOverwrites(t *testing.T) {
	metrics := telemetry.NewMetrics("test", "0.1.0")
	totalItems := 500
	q := collector.NewBoundedQueue[int]("vc-slow", 10, metrics)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	received := make([]int, 0, totalItems)
	var mu sync.Mutex

	// Slow consumer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < totalItems; i++ {
			// Consumer introduces artificial latency
			time.Sleep(2 * time.Millisecond)
			val, err := q.Pop(ctx)
			if err != nil {
				t.Errorf("consumer pop failed: %v", err)
				return
			}
			mu.Lock()
			received = append(received, val)
			mu.Unlock()
		}
	}()

	// Fast producer
	for i := 0; i < totalItems; i++ {
		if err := q.Push(ctx, i); err != nil {
			t.Fatalf("producer push failed at index %d: %v", i, err)
		}
	}

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(received) != totalItems {
		t.Fatalf("expected %d items received, got %d", totalItems, len(received))
	}

	for i := 0; i < totalItems; i++ {
		if received[i] != i {
			t.Fatalf("data corruption or dropped update at index %d: expected %d, got %d", i, i, received[i])
		}
	}
}

func TestBoundedQueue_PushContextCancellation(t *testing.T) {
	metrics := telemetry.NewMetrics("test", "0.1.0")
	q := collector.NewBoundedQueue[int]("vc-timeout", 2, metrics)

	ctx := context.Background()
	_ = q.Push(ctx, 1)
	_ = q.Push(ctx, 2)

	// Attempt push with already-cancelled context
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	err := q.Push(cancelCtx, 3)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}

	if q.BlockedProducers() != 0 {
		t.Errorf("expected 0 blocked producers after cancellation, got %d", q.BlockedProducers())
	}
}

func TestBoundedQueue_PopBatch(t *testing.T) {
	metrics := telemetry.NewMetrics("test", "0.1.0")
	q := collector.NewBoundedQueue[int]("vc-batch", 100, metrics)
	ctx := context.Background()

	// Push 10 items
	for i := 1; i <= 10; i++ {
		_ = q.Push(ctx, i)
	}

	// Pop batch of max 5
	batch1, err := q.PopBatch(ctx, 5, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("PopBatch failed: %v", err)
	}
	if len(batch1) != 5 {
		t.Fatalf("expected batch size 5, got %d", len(batch1))
	}
	for i := 0; i < 5; i++ {
		if batch1[i] != i+1 {
			t.Errorf("expected %d, got %d", i+1, batch1[i])
		}
	}

	// Pop remaining 5 items
	batch2, err := q.PopBatch(ctx, 10, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("PopBatch failed: %v", err)
	}
	if len(batch2) != 5 {
		t.Fatalf("expected batch size 5, got %d", len(batch2))
	}
	for i := 0; i < 5; i++ {
		if batch2[i] != i+6 {
			t.Errorf("expected %d, got %d", i+6, batch2[i])
		}
	}
}

func TestBoundedQueue_CloseDrainsRemaining(t *testing.T) {
	metrics := telemetry.NewMetrics("test", "0.1.0")
	q := collector.NewBoundedQueue[string]("vc-close", 5, metrics)
	ctx := context.Background()

	_ = q.Push(ctx, "leftover-1")
	_ = q.Push(ctx, "leftover-2")
	q.Close()

	if !q.IsClosed() {
		t.Error("expected IsClosed()=true")
	}

	// Pushing after close must fail
	if err := q.Push(ctx, "too-late"); !errors.Is(err, collector.ErrQueueClosed) {
		t.Fatalf("expected ErrQueueClosed on push, got: %v", err)
	}

	// Popping remaining items before reporting closed
	item1, err := q.Pop(ctx)
	if err != nil || item1 != "leftover-1" {
		t.Errorf("expected leftover-1, got %v (%v)", item1, err)
	}
	item2, err := q.Pop(ctx)
	if err != nil || item2 != "leftover-2" {
		t.Errorf("expected leftover-2, got %v (%v)", item2, err)
	}

	// Now empty queue must return ErrQueueClosed
	_, err = q.Pop(ctx)
	if !errors.Is(err, collector.ErrQueueClosed) {
		t.Fatalf("expected ErrQueueClosed on empty pop, got: %v", err)
	}
}

func TestBoundedQueue_PushConcurrentCloseNoPanic(t *testing.T) {
	metrics := telemetry.NewMetrics("test", "0.1.0")
	for iter := 0; iter < 100; iter++ {
		q := collector.NewBoundedQueue[int]("vc-panic-test", 1, metrics)
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_ = q.Push(context.Background(), i)
			}
		}()

		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(iter%5) * time.Millisecond)
			q.Close()
		}()

		wg.Wait()
	}
}
