package collector_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/vsphere"
	"github.com/vmware/govmomi/vim25/types"
)

func TestCleanupRegistry_Strict8StageTeardownOrder(t *testing.T) {
	reg := collector.NewCleanupRegistry(nil, 500*time.Millisecond)

	var mu sync.Mutex
	var executedStages []collector.ResourceKind

	record := func(kind collector.ResourceKind) {
		mu.Lock()
		defer mu.Unlock()
		executedStages = append(executedStages, kind)
	}

	// Deliberately register in randomized / non-sequential order
	reg.RegisterClientClose(func() {
		record(collector.ResourceKindClient)
	})
	reg.RegisterSOAP(vsphere.NewFakeSession()) // Stage 7
	reg.Register(collector.ResourceKindSOAP, "SOAP-Custom", func(ctx context.Context) error {
		record(collector.ResourceKindSOAP)
		return nil
	})
	reg.RegisterREST(func(ctx context.Context) error {
		record(collector.ResourceKindREST)
		return nil
	})
	reg.RegisterContainerView(func(ctx context.Context) error {
		record(collector.ResourceKindView)
		return nil
	})
	fakePC := vsphere.NewFakePropertyCollector()
	fakeFilter, err := fakePC.CreateFilter(context.Background(), types.PropertyFilterSpec{})
	if err != nil {
		t.Fatalf("failed to create fake filter: %v", err)
	}
	reg.RegisterPropertyFilter(fakeFilter) // Stage 4
	reg.Register(collector.ResourceKindFilter, "Filter-Custom", func(ctx context.Context) error {
		record(collector.ResourceKindFilter)
		return nil
	})
	fakeEvents := vsphere.NewFakeEventCollector()
	reg.RegisterEventCollector(fakeEvents) // Stage 3
	reg.Register(collector.ResourceKindEvent, "Event-Custom", func(ctx context.Context) error {
		record(collector.ResourceKindEvent)
		return nil
	})
	reg.RegisterDrain("drain-queue", func(ctx context.Context) error {
		record(collector.ResourceKindDrain)
		return nil
	})
	reg.RegisterProducer("producer-stream", func() {
		record(collector.ResourceKindProducer)
	})

	if err := reg.Teardown(context.Background()); err != nil {
		t.Fatalf("unexpected error during teardown: %v", err)
	}

	expectedOrder := []collector.ResourceKind{
		collector.ResourceKindProducer, // 1
		collector.ResourceKindDrain,    // 2
		collector.ResourceKindEvent,    // 3
		collector.ResourceKindFilter,   // 4
		collector.ResourceKindView,     // 5
		collector.ResourceKindREST,     // 6
		collector.ResourceKindSOAP,     // 7
		collector.ResourceKindClient,   // 8
	}

	if len(executedStages) != len(expectedOrder) {
		t.Fatalf("expected %d custom stages recorded, got %d: %v", len(expectedOrder), len(executedStages), executedStages)
	}

	for i, expected := range expectedOrder {
		if executedStages[i] != expected {
			t.Errorf("step %d: expected stage %v, got %v", i+1, expected, executedStages[i])
		}
	}
}

func TestCleanupRegistry_Idempotent(t *testing.T) {
	reg := collector.NewCleanupRegistry(nil, 500*time.Millisecond)

	count := 0
	reg.RegisterProducer("prod", func() {
		count++
	})

	if err := reg.Teardown(context.Background()); err != nil {
		t.Fatalf("first teardown failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}

	// Second teardown should do nothing
	if err := reg.Teardown(context.Background()); err != nil {
		t.Fatalf("second teardown failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count to stay 1, got %d", count)
	}
}

func TestCleanupRegistry_ErrorAccumulationDoesNotAbort(t *testing.T) {
	reg := collector.NewCleanupRegistry(nil, 500*time.Millisecond)

	var stagesCompleted []int
	errStage3 := errors.New("event collector destroy failed")
	errStage6 := errors.New("rest logout token revoked")

	reg.RegisterProducer("prod", func() {
		stagesCompleted = append(stagesCompleted, 1)
	})
	reg.Register(collector.ResourceKindEvent, "err-event", func(ctx context.Context) error {
		return errStage3
	})
	reg.Register(collector.ResourceKindFilter, "filter", func(ctx context.Context) error {
		stagesCompleted = append(stagesCompleted, 4)
		return nil
	})
	reg.RegisterREST(func(ctx context.Context) error {
		return errStage6
	})
	reg.RegisterClientClose(func() {
		stagesCompleted = append(stagesCompleted, 8)
	})

	err := reg.Teardown(context.Background())
	if err == nil {
		t.Fatal("expected teardown to return accumulated errors, got nil")
	}

	if !errors.Is(err, errStage3) {
		t.Errorf("expected error to wrap stage 3 error, got %v", err)
	}
	if !errors.Is(err, errStage6) {
		t.Errorf("expected error to wrap stage 6 error, got %v", err)
	}

	// Verify non-error stages completed despite intermediate failures
	expectedStages := []int{1, 4, 8}
	if len(stagesCompleted) != len(expectedStages) {
		t.Fatalf("expected completed stages %v, got %v", expectedStages, stagesCompleted)
	}
	for i, s := range expectedStages {
		if stagesCompleted[i] != s {
			t.Errorf("step %d: expected stage %d completed, got %d", i, s, stagesCompleted[i])
		}
	}
}

func TestCleanupRegistry_Unregister(t *testing.T) {
	reg := collector.NewCleanupRegistry(nil, 500*time.Millisecond)

	executed := false
	id := reg.Register(collector.ResourceKindFilter, "filter-to-remove", func(ctx context.Context) error {
		executed = true
		return nil
	})

	if reg.Count() != 1 {
		t.Fatalf("expected count 1, got %d", reg.Count())
	}

	reg.Unregister(id)

	if reg.Count() != 0 {
		t.Fatalf("expected count 0, got %d", reg.Count())
	}

	if err := reg.Teardown(context.Background()); err != nil {
		t.Fatalf("unexpected teardown error: %v", err)
	}

	if executed {
		t.Error("unregistered item should not have been executed")
	}
}

func TestCleanupRegistry_StepTimeout(t *testing.T) {
	// Step timeout 20ms
	reg := collector.NewCleanupRegistry(nil, 20*time.Millisecond)

	nextStepExecuted := false

	// Step that hangs
	reg.Register(collector.ResourceKindEvent, "hanging-event", func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
			return nil
		}
	})

	// Subsequent step that must execute after timeout
	reg.Register(collector.ResourceKindSOAP, "next-soap", func(ctx context.Context) error {
		nextStepExecuted = true
		return nil
	})

	err := reg.Teardown(context.Background())
	if err == nil {
		t.Fatal("expected teardown to return timeout error, got nil")
	}

	if !nextStepExecuted {
		t.Error("subsequent step should execute even after earlier step timed out")
	}
}

func TestCleanupRegistry_PanicRecovery(t *testing.T) {
	reg := collector.NewCleanupRegistry(nil, 500*time.Millisecond)

	cleanupRun := false
	reg.RegisterProducer("panic-prod", func() {
		cleanupRun = true
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic to be propagated")
		}
		if r != "simulated critical fault" {
			t.Fatalf("unexpected panic value: %v", r)
		}
		if !cleanupRun {
			t.Error("expected deferred RecoverAndCleanup to execute teardown")
		}
	}()

	func() {
		defer reg.RecoverAndCleanup(context.Background())
		panic("simulated critical fault")
	}()
}
