package collector_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type mockStreamController struct {
	destroyCount int
	initCount    int
	version      string
}

func (m *mockStreamController) InitFilter(_ context.Context) error {
	m.initCount++
	return nil
}

func (m *mockStreamController) DestroyFilter(_ context.Context) error {
	m.destroyCount++
	return nil
}

func (m *mockStreamController) SetVersion(v string) {
	m.version = v
}

func (m *mockStreamController) Version() string {
	return m.version
}

type mockRehydrator struct {
	hydrateTopologyCount  int
	hydrateInventoryCount int
}

func (m *mockRehydrator) HydrateTopology(_ context.Context, _ string) (*collector.TopologySnapshot, error) {
	m.hydrateTopologyCount++
	return &collector.TopologySnapshot{}, nil
}

func (m *mockRehydrator) HydrateInventory(_ context.Context, _ string, _ *collector.TopologySnapshot, handler func([]domain.VMRecord) error) error {
	m.hydrateInventoryCount++
	return handler([]domain.VMRecord{
		{Name: "vm-recovered", MoRef: "vm-100"},
	})
}

type mockRecoveryPersister struct {
	persistTopologyCount int
	persistVMBatchCount  int
	reconcileCount       int
}

func (m *mockRecoveryPersister) PersistTopology(_ context.Context, _ *collector.TopologySnapshot) error {
	m.persistTopologyCount++
	return nil
}

func (m *mockRecoveryPersister) PersistVMBatch(_ context.Context, batch []domain.VMRecord) (int, error) {
	m.persistVMBatchCount += len(batch)
	return len(batch), nil
}

func (m *mockRecoveryPersister) ReconcileStaleVMs(_ context.Context, _ string, _ time.Time) (int64, error) {
	m.reconcileCount++
	return 2, nil
}

func TestRecoveryManager_InvalidCollectorVersion(t *testing.T) {
	vcenterID := "vc-rec-1"
	metrics := telemetry.NewMetrics("test", "0.1.0")
	fsm := collector.NewWorkerFSM(vcenterID, metrics, nil)

	// Advance FSM to DELTA_STREAMING
	_ = fsm.Transition(collector.WorkerStateConnecting)
	_ = fsm.Transition(collector.WorkerStateSessionActive)
	_ = fsm.Transition(collector.WorkerStateTopologyPreflight)
	_ = fsm.Transition(collector.WorkerStateFullInventorySync)
	_ = fsm.Transition(collector.WorkerStateDeltaStreaming)

	streamer := &mockStreamController{version: "stale-version-token"}
	rehydrator := &mockRehydrator{}
	persister := &mockRecoveryPersister{}

	rm := collector.NewRecoveryManager(vcenterID, streamer, rehydrator, persister, fsm, metrics, nil)

	// Trigger recovery with ErrInvalidCollectorVersion
	err := rm.Recover(context.Background(), collector.ErrInvalidCollectorVersion)
	if err != nil {
		t.Fatalf("unexpected recovery failure: %v", err)
	}

	// Verify filter was destroyed and recreated
	if streamer.destroyCount != 1 {
		t.Errorf("expected 1 DestroyFilter call, got: %d", streamer.destroyCount)
	}
	if streamer.initCount != 1 {
		t.Errorf("expected 1 InitFilter call, got: %d", streamer.initCount)
	}
	if streamer.version != "" {
		t.Errorf("expected version to be reset to empty string, got: %q", streamer.version)
	}

	// Verify rehydration and reconciliation occurred
	if rehydrator.hydrateTopologyCount != 1 {
		t.Errorf("expected 1 HydrateTopology call, got: %d", rehydrator.hydrateTopologyCount)
	}
	if rehydrator.hydrateInventoryCount != 1 {
		t.Errorf("expected 1 HydrateInventory call, got: %d", rehydrator.hydrateInventoryCount)
	}
	if persister.reconcileCount != 1 {
		t.Errorf("expected 1 ReconcileStaleVMs call, got: %d", persister.reconcileCount)
	}

	// Verify FSM returned to DELTA_STREAMING
	if fsm.Current() != collector.WorkerStateDeltaStreaming {
		t.Errorf("expected final FSM state DELTA_STREAMING, got: %s", fsm.Current())
	}

	// Verify metrics
	resyncCount := testutil.ToFloat64(metrics.ResyncTotal.WithLabelValues(vcenterID, "invalid_collector_version"))
	if resyncCount != 1 {
		t.Errorf("expected resync counter 1 for invalid_collector_version, got: %v", resyncCount)
	}
}

func TestRecoveryManager_StreamHung(t *testing.T) {
	vcenterID := "vc-rec-2"
	metrics := telemetry.NewMetrics("test", "0.1.0")
	fsm := collector.NewWorkerFSM(vcenterID, metrics, nil)

	_ = fsm.Transition(collector.WorkerStateConnecting)
	_ = fsm.Transition(collector.WorkerStateSessionActive)
	_ = fsm.Transition(collector.WorkerStateTopologyPreflight)
	_ = fsm.Transition(collector.WorkerStateFullInventorySync)
	_ = fsm.Transition(collector.WorkerStateDeltaStreaming)

	streamer := &mockStreamController{version: "hung-version"}
	rehydrator := &mockRehydrator{}
	persister := &mockRecoveryPersister{}

	rm := collector.NewRecoveryManager(vcenterID, streamer, rehydrator, persister, fsm, metrics, nil)

	// Trigger recovery with ErrStreamHung
	err := rm.Recover(context.Background(), collector.ErrStreamHung)
	if err != nil {
		t.Fatalf("unexpected recovery failure: %v", err)
	}

	resyncCount := testutil.ToFloat64(metrics.ResyncTotal.WithLabelValues(vcenterID, "stream_hung"))
	if resyncCount != 1 {
		t.Errorf("expected resync counter 1 for stream_hung, got: %v", resyncCount)
	}
}

func TestRecoveryManager_IgnoresUnrelatedErrors(t *testing.T) {
	vcenterID := "vc-rec-3"
	metrics := telemetry.NewMetrics("test", "0.1.0")
	fsm := collector.NewWorkerFSM(vcenterID, metrics, nil)
	_ = fsm.Transition(collector.WorkerStateConnecting)

	rm := collector.NewRecoveryManager(vcenterID, nil, nil, nil, fsm, metrics, nil)

	unrelatedErr := errors.New("database network timeout")
	err := rm.Recover(context.Background(), unrelatedErr)
	if !errors.Is(err, unrelatedErr) {
		t.Errorf("expected unrelated error to pass through untouched, got: %v", err)
	}

	if fsm.Current() != collector.WorkerStateConnecting {
		t.Errorf("expected FSM to remain in CONNECTING, got: %s", fsm.Current())
	}
}
