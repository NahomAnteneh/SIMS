package collector

import (
	"errors"
	"fmt"
	"sync"
)

// State represents the lifecycle state of a vCenter collector worker.
type State string

const (
	StateInit              State = "INIT"
	StateConnecting        State = "CONNECTING"
	StateSessionActive     State = "SESSION_ACTIVE"
	StateTopologyPreflight State = "TOPOLOGY_PREFLIGHT"
	StateFullInventorySync State = "FULL_INVENTORY_SYNC"
	StateDeltaStreaming    State = "DELTA_STREAMING"
	StateResyncing         State = "RESYNCING"
	StateDegraded          State = "DEGRADED"
	StateStopping          State = "STOPPING"
)

var ErrIllegalTransition = errors.New("illegal worker state transition")

// validTransitions defines the explicit state transition graph.
var validTransitions = map[State]map[State]bool{
	StateInit: {
		StateConnecting: true,
		StateStopping:   true,
	},
	StateConnecting: {
		StateSessionActive: true,
		StateDegraded:      true,
		StateStopping:      true,
	},
	StateSessionActive: {
		StateTopologyPreflight: true,
		StateDegraded:          true,
		StateStopping:          true,
	},
	StateTopologyPreflight: {
		StateFullInventorySync: true,
		StateDegraded:          true,
		StateStopping:          true,
	},
	StateFullInventorySync: {
		StateDeltaStreaming: true,
		StateResyncing:      true,
		StateDegraded:       true,
		StateStopping:       true,
	},
	StateDeltaStreaming: {
		StateResyncing: true,
		StateDegraded:  true,
		StateStopping:  true,
	},
	StateResyncing: {
		StateFullInventorySync: true,
		StateDeltaStreaming:    true,
		StateDegraded:          true,
		StateStopping:          true,
	},
	StateDegraded: {
		StateConnecting: true,
		StateStopping:   true,
	},
	StateStopping: {
		// Terminal state, no outbound transitions allowed
	},
}

// WorkerFSM enforces state transitions for a collector instance using a table-driven model.
type WorkerFSM struct {
	mu      sync.RWMutex
	current State
}

// NewWorkerFSM initializes the worker in the INIT state.
func NewWorkerFSM() *WorkerFSM {
	return &WorkerFSM{
		current: StateInit,
	}
}

// Current returns the current state safely under a read lock.
func (fsm *WorkerFSM) Current() State {
	fsm.mu.RLock()
	defer fsm.mu.RUnlock()
	return fsm.current
}

// Transition transitions the worker to the requested state if valid, or returns ErrIllegalTransition.
func (fsm *WorkerFSM) Transition(to State) error {
	fsm.mu.Lock()
	defer fsm.mu.Unlock()

	allowed, ok := validTransitions[fsm.current]
	if !ok || !allowed[to] {
		return fmt.Errorf("%w: from %s to %s", ErrIllegalTransition, fsm.current, to)
	}

	fsm.current = to
	return nil
}
