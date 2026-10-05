package collector_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/infrastructure/sims/internal/vsphere"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/vmware/govmomi/vim25/types"
)

type mockEventPersister struct {
	mu sync.Mutex

	watermarkKey  int64
	watermarkTime *time.Time

	savedWatermarkKey  int64
	savedWatermarkTime time.Time

	persistedEvents []collector.EventRecord
}

func (m *mockEventPersister) GetEventWatermark(ctx context.Context, vcenterID string) (int64, *time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.watermarkKey, m.watermarkTime, nil
}

func (m *mockEventPersister) SaveEventWatermark(ctx context.Context, vcenterID string, lastEventKey int64, lastEventTime time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.savedWatermarkKey = lastEventKey
	m.savedWatermarkTime = lastEventTime
	return nil
}

func (m *mockEventPersister) PersistEvent(ctx context.Context, vcenterID string, event collector.EventRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.persistedEvents = append(m.persistedEvents, event)
	return nil
}

func (m *mockEventPersister) GetPersistedEvents() []collector.EventRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make([]collector.EventRecord, len(m.persistedEvents))
	copy(res, m.persistedEvents)
	return res
}

func TestEventStreamer_V1EventCaptureAndNormalization(t *testing.T) {
	fakeEM := vsphere.NewFakeEventManager()
	persister := &mockEventPersister{}
	metrics := telemetry.NewMetrics("test", "0.1.0")

	cfg := collector.EventStreamerConfig{
		VCenterID:     "vc-event-test",
		PageSize:      10,
		PollInterval:  10 * time.Millisecond,
		OverlapWindow: 5 * time.Minute,
		CallDeadline:  1 * time.Second,
	}

	streamer := collector.NewEventStreamer(fakeEM, persister, cfg, metrics, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Pre-enqueue events via the manager by creating the collector first or starting runner
	now := time.Now()
	ev1 := &types.VmCreatedEvent{
		VmEvent: types.VmEvent{
			Event: types.Event{
				Key:                  101,
				ChainId:              101,
				CreatedTime:          now.Add(-2 * time.Minute),
				UserName:             "corp\\admin",
				FullFormattedMessage: "VM prod-db-1 created",
				Vm: &types.VmEventArgument{
					EntityEventArgument: types.EntityEventArgument{Name: "prod-db-1"},
					Vm:                  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-101"},
				},
			},
		},
	}
	ev2 := &types.VmRemovedEvent{
		VmEvent: types.VmEvent{
			Event: types.Event{
				Key:                  102,
				ChainId:              102,
				CreatedTime:          now.Add(-1 * time.Minute),
				UserName:             "svc-deployer",
				FullFormattedMessage: "VM legacy-app removed",
				Vm: &types.VmEventArgument{
					EntityEventArgument: types.EntityEventArgument{Name: "legacy-app"},
					Vm:                  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-102"},
				},
			},
		},
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = streamer.Run(ctx)
	}()

	// Wait for collector to be created
	var ec *vsphere.FakeEventCollector
	for i := 0; i < 50; i++ {
		collectors := fakeEM.CreatedCollectors()
		if len(collectors) > 0 {
			ec = collectors[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ec == nil {
		t.Fatal("expected FakeEventCollector to be created")
	}

	ec.EnqueueEvents(ev1, ev2)

	// Wait for persistence
	var events []collector.EventRecord
	for i := 0; i < 50; i++ {
		events = persister.GetPersistedEvents()
		if len(events) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	wg.Wait()

	if len(events) != 2 {
		t.Fatalf("expected 2 persisted events, got %d", len(events))
	}

	// Verify ev1 normalization
	if events[0].EventKey != 101 {
		t.Errorf("expected EventKey 101, got %d", events[0].EventKey)
	}
	if events[0].EventType != "VmCreatedEvent" {
		t.Errorf("expected EventType 'VmCreatedEvent', got %q", events[0].EventType)
	}
	if events[0].UserName != "corp\\admin" {
		t.Errorf("expected UserName 'corp\\admin', got %q", events[0].UserName)
	}
	if events[0].EntityMoref != "vm-101" {
		t.Errorf("expected EntityMoref 'vm-101', got %q", events[0].EntityMoref)
	}
	if events[0].EntityName != "prod-db-1" {
		t.Errorf("expected EntityName 'prod-db-1', got %q", events[0].EntityName)
	}

	// Verify ev2 normalization
	if events[1].EventKey != 102 {
		t.Errorf("expected EventKey 102, got %d", events[1].EventKey)
	}
	if events[1].EventType != "VmRemovedEvent" {
		t.Errorf("expected EventType 'VmRemovedEvent', got %q", events[1].EventType)
	}
	if events[1].EntityMoref != "vm-102" {
		t.Errorf("expected EntityMoref 'vm-102', got %q", events[1].EntityMoref)
	}

	// Verify telemetry counter incremented
	count := testutil.ToFloat64(metrics.EventsIngestedTotal.WithLabelValues("vc-event-test", "VmCreatedEvent"))
	if count != 1 {
		t.Errorf("expected metric sims_events_ingested_total to be 1 for VmCreatedEvent, got %f", count)
	}
}

func TestEventStreamer_RestartAndReplayLookbackWindow(t *testing.T) {
	fakeEM := vsphere.NewFakeEventManager()
	now := time.Now()
	lastSeenTime := now.Add(-30 * time.Minute)

	persister := &mockEventPersister{
		watermarkKey:  500,
		watermarkTime: &lastSeenTime,
	}
	metrics := telemetry.NewMetrics("test", "0.1.0")

	overlap := 5 * time.Minute
	cfg := collector.EventStreamerConfig{
		VCenterID:     "vc-replay",
		PageSize:      50,
		PollInterval:  10 * time.Millisecond,
		OverlapWindow: overlap,
		CallDeadline:  1 * time.Second,
	}

	streamer := collector.NewEventStreamer(fakeEM, persister, cfg, metrics, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = streamer.Run(ctx)
	}()

	// Wait for collector creation
	for i := 0; i < 50; i++ {
		if len(fakeEM.CreatedCollectors()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	wg.Wait()

	lastFilter := fakeEM.LastFilter()
	if lastFilter.Time == nil || lastFilter.Time.BeginTime == nil {
		t.Fatal("expected EventFilterSpec to have BeginTime set when resuming from existing watermark")
	}

	expectedBegin := lastSeenTime.Add(-overlap)
	if !lastFilter.Time.BeginTime.Equal(expectedBegin) {
		t.Errorf("expected BeginTime %v, got %v", expectedBegin, *lastFilter.Time.BeginTime)
	}

	// Verify V1EventTypes are filtered
	if len(lastFilter.EventTypeId) != len(collector.V1EventTypes) {
		t.Errorf("expected %d event types in filter, got %d", len(collector.V1EventTypes), len(lastFilter.EventTypeId))
	}
}

func TestEventStreamer_GapDetectionAndReconciliationTrigger(t *testing.T) {
	fakeEM := vsphere.NewFakeEventManager()
	now := time.Now()
	lastSeenTime := now.Add(-2 * time.Hour) // 2 hours ago

	persister := &mockEventPersister{
		watermarkKey:  100,
		watermarkTime: &lastSeenTime,
	}
	metrics := telemetry.NewMetrics("test", "0.1.0")

	reconcileInvoked := false
	var mu sync.Mutex

	cfg := collector.EventStreamerConfig{
		VCenterID:      "vc-gap",
		PageSize:       10,
		PollInterval:   10 * time.Millisecond,
		OverlapWindow:  5 * time.Minute,
		CallDeadline:   1 * time.Second,
		ReconcileOnGap: true,
		OnReconciliationRequested: func() {
			mu.Lock()
			defer mu.Unlock()
			reconcileInvoked = true
		},
	}

	streamer := collector.NewEventStreamer(fakeEM, persister, cfg, metrics, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = streamer.Run(ctx)
	}()

	// Wait for collector creation
	var ec *vsphere.FakeEventCollector
	for i := 0; i < 50; i++ {
		collectors := fakeEM.CreatedCollectors()
		if len(collectors) > 0 {
			ec = collectors[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ec == nil {
		t.Fatal("expected FakeEventCollector to be created")
	}

	// vCenter event retention expired; oldest event returned is 10 minutes ago (> 2 hours ago)
	gapEvent := &types.VmCreatedEvent{
		VmEvent: types.VmEvent{
			Event: types.Event{
				Key:         500,
				CreatedTime: now.Add(-10 * time.Minute),
				Vm: &types.VmEventArgument{
					EntityEventArgument: types.EntityEventArgument{Name: "gap-vm"},
					Vm:                  types.ManagedObjectReference{Value: "vm-500"},
				},
			},
		},
	}
	ec.EnqueueEvents(gapEvent)

	// Wait for processing
	for i := 0; i < 50; i++ {
		mu.Lock()
		invoked := reconcileInvoked
		mu.Unlock()
		if invoked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	wg.Wait()

	mu.Lock()
	if !reconcileInvoked {
		t.Error("expected OnReconciliationRequested callback to be triggered when gap is detected")
	}
	mu.Unlock()

	gapCount := testutil.ToFloat64(metrics.EventGapDetected.WithLabelValues("vc-gap"))
	if gapCount != 1 {
		t.Errorf("expected sims_collector_event_gap_detected to be 1, got %f", gapCount)
	}

	reconCount := testutil.ToFloat64(metrics.EventReconciliationRequested.WithLabelValues("vc-gap"))
	if reconCount != 1 {
		t.Errorf("expected sims_collector_event_reconciliation_requested to be 1, got %f", reconCount)
	}
}

func TestEventStreamer_CleanupRegistryTeardown(t *testing.T) {
	fakeEM := vsphere.NewFakeEventManager()
	persister := &mockEventPersister{}
	metrics := telemetry.NewMetrics("test", "0.1.0")
	reg := collector.NewCleanupRegistry(nil, 500*time.Millisecond)

	cfg := collector.EventStreamerConfig{
		VCenterID:     "vc-cleanup-test",
		PageSize:      10,
		PollInterval:  10 * time.Millisecond,
		OverlapWindow: 5 * time.Minute,
		CallDeadline:  1 * time.Second,
	}

	streamer := collector.NewEventStreamer(fakeEM, persister, cfg, metrics, nil, reg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = streamer.Run(ctx)
	}()

	// Wait for collector creation
	var ec *vsphere.FakeEventCollector
	for i := 0; i < 50; i++ {
		collectors := fakeEM.CreatedCollectors()
		if len(collectors) > 0 {
			ec = collectors[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ec == nil {
		t.Fatal("expected FakeEventCollector to be created")
	}

	// Teardown the registry (Stage 1 stops producer, Stage 3 destroys collector)
	err := reg.Teardown(context.Background())
	if err != nil {
		t.Fatalf("expected clean teardown, got: %v", err)
	}

	// Producer loop should exit cleanly
	wg.Wait()

	// Verify collector was destroyed on server
	if ec.DestroyCount() != 1 {
		t.Errorf("expected collector.Destroy() to be called once during teardown, got %d", ec.DestroyCount())
	}
}
