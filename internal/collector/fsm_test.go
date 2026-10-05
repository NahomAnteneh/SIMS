package collector_test

import (
	"errors"
	"testing"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestWorkerFSM_LifecycleAndTransitions(t *testing.T) {
	metrics := telemetry.NewMetrics("test", "0.1.0")
	fsm := collector.NewWorkerFSM("vc-1", metrics, nil)

	if fsm.Current() != collector.WorkerStateInit {
		t.Fatalf("expected initial state INIT, got: %s", fsm.Current())
	}

	// Normal lifecycle progression:
	// INIT -> CONNECTING -> SESSION_ACTIVE -> TOPOLOGY_PREFLIGHT -> FULL_INVENTORY_SYNC -> DELTA_STREAMING
	transitions := []collector.WorkerState{
		collector.WorkerStateConnecting,
		collector.WorkerStateSessionActive,
		collector.WorkerStateTopologyPreflight,
		collector.WorkerStateFullInventorySync,
		collector.WorkerStateDeltaStreaming,
	}

	for _, next := range transitions {
		if err := fsm.Transition(next); err != nil {
			t.Fatalf("failed transition to %s: %v", next, err)
		}
		if fsm.Current() != next {
			t.Fatalf("expected state %s, got: %s", next, fsm.Current())
		}
	}

	// Test Recovery transition: DELTA_STREAMING -> RESYNCING -> DELTA_STREAMING
	if err := fsm.Transition(collector.WorkerStateResyncing); err != nil {
		t.Fatalf("failed transition to RESYNCING: %v", err)
	}
	if fsm.Current() != collector.WorkerStateResyncing {
		t.Fatalf("expected state RESYNCING, got: %s", fsm.Current())
	}
	if err := fsm.Transition(collector.WorkerStateDeltaStreaming); err != nil {
		t.Fatalf("failed transition back to DELTA_STREAMING: %v", err)
	}

	// Test Degraded transition: DELTA_STREAMING -> DEGRADED -> CONNECTING
	if err := fsm.Transition(collector.WorkerStateDegraded); err != nil {
		t.Fatalf("failed transition to DEGRADED: %v", err)
	}
	if err := fsm.Transition(collector.WorkerStateConnecting); err != nil {
		t.Fatalf("failed transition from DEGRADED to CONNECTING: %v", err)
	}

	// Test Shutdown: CONNECTING -> STOPPING -> STOPPED
	if err := fsm.Transition(collector.WorkerStateStopping); err != nil {
		t.Fatalf("failed transition to STOPPING: %v", err)
	}
	if err := fsm.Transition(collector.WorkerStateStopped); err != nil {
		t.Fatalf("failed transition to STOPPED: %v", err)
	}

	// Verify transitions metric recorded
	initToConn := testutil.ToFloat64(metrics.CollectorStateTransitionsTotal.WithLabelValues("vc-1", "INIT", "CONNECTING"))
	if initToConn != 1 {
		t.Errorf("expected 1 INIT->CONNECTING transition, got: %v", initToConn)
	}
}

func TestWorkerFSM_IllegalTransitions(t *testing.T) {
	metrics := telemetry.NewMetrics("test", "0.1.0")
	fsm := collector.NewWorkerFSM("vc-illegal", metrics, nil)

	// Direct INIT -> DELTA_STREAMING is illegal
	err := fsm.Transition(collector.WorkerStateDeltaStreaming)
	if err == nil {
		t.Fatal("expected illegal transition error, got nil")
	}

	var illegalErr collector.ErrIllegalTransition
	if !errors.As(err, &illegalErr) {
		t.Fatalf("expected ErrIllegalTransition type, got: %T (%v)", err, err)
	}

	if illegalErr.From != collector.WorkerStateInit || illegalErr.To != collector.WorkerStateDeltaStreaming {
		t.Errorf("unexpected transition details: from=%s, to=%s", illegalErr.From, illegalErr.To)
	}
}
