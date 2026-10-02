package collector_test

import (
	"errors"
	"testing"

	"github.com/infrastructure/sims/internal/collector"
)

func TestWorkerFSMValidTransitions(t *testing.T) {
	fsm := collector.NewWorkerFSM()
	if fsm.Current() != collector.StateInit {
		t.Fatalf("expected initial state %s, got %s", collector.StateInit, fsm.Current())
	}

	transitions := []collector.State{
		collector.StateConnecting,
		collector.StateSessionActive,
		collector.StateTopologyPreflight,
		collector.StateFullInventorySync,
		collector.StateDeltaStreaming,
		collector.StateResyncing,
		collector.StateFullInventorySync,
		collector.StateDegraded,
		collector.StateConnecting,
		collector.StateStopping,
	}

	for _, next := range transitions {
		err := fsm.Transition(next)
		if err != nil {
			t.Fatalf("failed transition to %s: %v", next, err)
		}
		if fsm.Current() != next {
			t.Fatalf("expected state %s, got %s", next, fsm.Current())
		}
	}
}

func TestWorkerFSMIllegalTransitions(t *testing.T) {
	fsm := collector.NewWorkerFSM()

	// Cannot transition directly from INIT to DELTA_STREAMING
	err := fsm.Transition(collector.StateDeltaStreaming)
	if err == nil {
		t.Fatalf("expected error on illegal transition INIT -> DELTA_STREAMING, got nil")
	}
	if !errors.Is(err, collector.ErrIllegalTransition) {
		t.Fatalf("expected ErrIllegalTransition, got: %v", err)
	}

	// Move to STOPPING (terminal)
	if err := fsm.Transition(collector.StateStopping); err != nil {
		t.Fatalf("transition to STOPPING failed: %v", err)
	}

	// Cannot transition from STOPPING to INIT or CONNECTING
	err = fsm.Transition(collector.StateConnecting)
	if err == nil {
		t.Fatalf("expected error on transition from terminal STOPPING, got nil")
	}
	if !errors.Is(err, collector.ErrIllegalTransition) {
		t.Fatalf("expected ErrIllegalTransition, got: %v", err)
	}
}
