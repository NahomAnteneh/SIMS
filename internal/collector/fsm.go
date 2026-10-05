package collector

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/infrastructure/sims/internal/telemetry"
)

// WorkerState represents the lifecycle state of a collector worker.
type WorkerState string

const (
	WorkerStateInit              WorkerState = "INIT"
	WorkerStateConnecting        WorkerState = "CONNECTING"
	WorkerStateSessionActive     WorkerState = "SESSION_ACTIVE"
	WorkerStateTopologyPreflight WorkerState = "TOPOLOGY_PREFLIGHT"
	WorkerStateFullInventorySync WorkerState = "FULL_INVENTORY_SYNC"
	WorkerStateDeltaStreaming    WorkerState = "DELTA_STREAMING"
	WorkerStateResyncing         WorkerState = "RESYNCING"
	WorkerStateDegraded          WorkerState = "DEGRADED"
	WorkerStateStopping          WorkerState = "STOPPING"
	WorkerStateStopped           WorkerState = "STOPPED"
)

// ErrIllegalTransition is returned when an illegal state transition is attempted.
type ErrIllegalTransition struct {
	From WorkerState
	To   WorkerState
}

func (e ErrIllegalTransition) Error() string {
	return fmt.Sprintf("illegal worker state transition from %s to %s", e.From, e.To)
}

// defaultValidTransitions defines the table-driven state machine transition rules.
var defaultValidTransitions = map[WorkerState]map[WorkerState]bool{
	WorkerStateInit: {
		WorkerStateConnecting: true,
		WorkerStateStopping:   true,
	},
	WorkerStateConnecting: {
		WorkerStateSessionActive: true,
		WorkerStateDegraded:      true,
		WorkerStateStopping:      true,
	},
	WorkerStateSessionActive: {
		WorkerStateTopologyPreflight: true,
		WorkerStateDegraded:          true,
		WorkerStateStopping:          true,
	},
	WorkerStateTopologyPreflight: {
		WorkerStateFullInventorySync: true,
		WorkerStateDegraded:          true,
		WorkerStateStopping:          true,
	},
	WorkerStateFullInventorySync: {
		WorkerStateDeltaStreaming: true,
		WorkerStateResyncing:      true,
		WorkerStateDegraded:       true,
		WorkerStateStopping:       true,
	},
	WorkerStateDeltaStreaming: {
		WorkerStateResyncing: true,
		WorkerStateDegraded:  true,
		WorkerStateStopping:  true,
	},
	WorkerStateResyncing: {
		WorkerStateDeltaStreaming:    true,
		WorkerStateFullInventorySync: true,
		WorkerStateTopologyPreflight: true,
		WorkerStateDegraded:          true,
		WorkerStateStopping:          true,
	},
	WorkerStateDegraded: {
		WorkerStateConnecting: true,
		WorkerStateResyncing:  true,
		WorkerStateStopping:   true,
	},
	WorkerStateStopping: {
		WorkerStateStopped: true,
	},
	WorkerStateStopped: {},
}

// WorkerFSM manages the state and table-driven transitions of a collector worker.
type WorkerFSM struct {
	mu          sync.RWMutex
	state       WorkerState
	vcenterID   string
	metrics     *telemetry.Metrics
	logger      *slog.Logger
	transitions map[WorkerState]map[WorkerState]bool
}

// NewWorkerFSM initializes a new WorkerFSM in WorkerStateInit.
func NewWorkerFSM(vcenterID string, metrics *telemetry.Metrics, logger *slog.Logger) *WorkerFSM {
	if logger == nil {
		logger = slog.Default()
	}
	fsm := &WorkerFSM{
		state:       WorkerStateInit,
		vcenterID:   vcenterID,
		metrics:     metrics,
		logger:      logger,
		transitions: defaultValidTransitions,
	}

	if metrics != nil {
		metrics.CollectorState.WithLabelValues(vcenterID).Set(float64(workerStateCode(WorkerStateInit)))
	}

	return fsm
}

// Current returns the current state of the worker.
func (f *WorkerFSM) Current() WorkerState {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.state
}

// Transition attempts to transition the worker to the target state.
// Returns ErrIllegalTransition if the transition is not permitted by the state table.
func (f *WorkerFSM) Transition(to WorkerState) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	from := f.state
	if !f.transitions[from][to] {
		return ErrIllegalTransition{From: from, To: to}
	}

	f.state = to

	if f.metrics != nil {
		f.metrics.CollectorStateTransitionsTotal.WithLabelValues(f.vcenterID, string(from), string(to)).Inc()
		f.metrics.CollectorState.WithLabelValues(f.vcenterID).Set(float64(workerStateCode(to)))
	}

	f.logger.Info("worker state transition",
		slog.String("vcenter_id", f.vcenterID),
		slog.String("from_state", string(from)),
		slog.String("to_state", string(to)),
	)

	return nil
}

// workerStateCode maps WorkerState to the numerical gauge code used in telemetry metrics.
// 0=stopped, 1=connecting, 2=hydrating, 3=streaming, 4=resyncing, 5=reconnecting, 6=paused
func workerStateCode(s WorkerState) int {
	switch s {
	case WorkerStateInit, WorkerStateStopped, WorkerStateStopping:
		return 0
	case WorkerStateConnecting:
		return 1
	case WorkerStateSessionActive, WorkerStateTopologyPreflight, WorkerStateFullInventorySync:
		return 2
	case WorkerStateDeltaStreaming:
		return 3
	case WorkerStateResyncing:
		return 4
	case WorkerStateDegraded:
		return 5
	default:
		return 0
	}
}
