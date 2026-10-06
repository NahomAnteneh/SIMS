package collector

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTriggerManager_ManualAndPostRecovery(t *testing.T) {
	var mu sync.Mutex
	received := make([]ReconcileRequest, 0)

	tm := NewTriggerManager(TriggerConfig{
		BaselineInterval: 10 * time.Minute,
		DebounceWindow:   100 * time.Millisecond,
	}, func(ctx context.Context, req ReconcileRequest) {
		mu.Lock()
		received = append(received, req)
		mu.Unlock()
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tm.Start(ctx)
	defer tm.Stop()

	targetID := uuid.New()

	// 1. Manual trigger
	tm.TriggerManual(targetID)

	// 2. Post-recovery trigger
	tm.TriggerPostRecovery(targetID)

	// Wait for processing
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	count := len(received)
	mu.Unlock()

	if count < 1 {
		t.Fatalf("expected at least 1 reconciliation triggered, got %d", count)
	}
}

func TestTriggerManager_Debounce(t *testing.T) {
	var mu sync.Mutex
	count := 0

	tm := NewTriggerManager(TriggerConfig{
		BaselineInterval: 10 * time.Minute,
		DebounceWindow:   500 * time.Millisecond,
	}, func(ctx context.Context, req ReconcileRequest) {
		mu.Lock()
		count++
		mu.Unlock()
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tm.Start(ctx)
	defer tm.Stop()

	targetID := uuid.New()

	// Rapid succession of 5 post-recovery triggers
	for i := 0; i < 5; i++ {
		tm.TriggerPostRecovery(targetID)
	}

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	total := count
	mu.Unlock()

	// Due to debouncing, only 1 should have been executed within the debounce window
	if total != 1 {
		t.Fatalf("expected 1 execution due to debouncing, got %d", total)
	}
}
