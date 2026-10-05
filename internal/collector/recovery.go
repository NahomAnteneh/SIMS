package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/telemetry"
)

// StreamFilterController manages the lifecycle and versioning of the PropertyCollector filter.
type StreamFilterController interface {
	InitFilter(ctx context.Context) error
	DestroyFilter(ctx context.Context) error
	SetVersion(v string)
	Version() string
}

// Rehydrator performs topology and inventory rehydration from vSphere.
type Rehydrator interface {
	HydrateTopology(ctx context.Context, vcenterID string) (*TopologySnapshot, error)
	HydrateInventory(ctx context.Context, vcenterID string, snap *TopologySnapshot, handler func([]domain.VMRecord) error) error
}

// ReconcilePersister persists topology, VM records, and performs stale VM reconciliation.
type ReconcilePersister interface {
	PersistTopology(ctx context.Context, snap *TopologySnapshot) error
	PersistVMBatch(ctx context.Context, batch []domain.VMRecord) (int, error)
	ReconcileStaleVMs(ctx context.Context, vcenterID string, syncStartTime time.Time) (int64, error)
}

// RecoveryManager orchestrates the recovery process when PropertyCollector streaming faults occur (S2.4).
type RecoveryManager struct {
	vcenterID string
	streamer  StreamFilterController
	hydrator  Rehydrator
	persister ReconcilePersister
	fsm       *WorkerFSM
	metrics   *telemetry.Metrics
	logger    *slog.Logger
}

// NewRecoveryManager creates a new RecoveryManager.
func NewRecoveryManager(
	vcenterID string,
	streamer StreamFilterController,
	hydrator Rehydrator,
	persister ReconcilePersister,
	fsm *WorkerFSM,
	metrics *telemetry.Metrics,
	logger *slog.Logger,
) *RecoveryManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &RecoveryManager{
		vcenterID: vcenterID,
		streamer:  streamer,
		hydrator:  hydrator,
		persister: persister,
		fsm:       fsm,
		metrics:   metrics,
		logger:    logger,
	}
}

// Recover executes the recovery pipeline according to SPRINTPLAN S2.4:
// STREAMING -> InvalidCollectorVersion/StreamHung -> RESYNCING -> destroy stale filter ->
// full/required rehydration -> reconcile -> create new filter -> new stream (DELTA_STREAMING).
func (r *RecoveryManager) Recover(ctx context.Context, faultErr error) error {
	if faultErr == nil {
		return nil
	}

	isInvalidCollectorVersion := errors.Is(faultErr, ErrInvalidCollectorVersion) || IsInvalidCollectorVersion(faultErr)
	isStreamHung := errors.Is(faultErr, ErrStreamHung)

	if !isInvalidCollectorVersion && !isStreamHung {
		return faultErr
	}

	reason := "invalid_collector_version"
	if isStreamHung {
		reason = "stream_hung"
	}

	r.logger.Warn("initiating PropertyCollector recovery pipeline",
		slog.String("vcenter_id", r.vcenterID),
		slog.String("reason", reason),
		slog.String("fault_error", faultErr.Error()),
	)

	start := time.Now()
	if r.metrics != nil {
		r.metrics.ResyncTotal.WithLabelValues(r.vcenterID, reason).Inc()
	}

	// 1. Transition to RESYNCING
	if r.fsm != nil {
		if err := r.fsm.Transition(WorkerStateResyncing); err != nil {
			return fmt.Errorf("failed to transition to RESYNCING: %w", err)
		}
	}

	// 2. Destroy stale filter
	if r.streamer != nil {
		if err := r.streamer.DestroyFilter(ctx); err != nil {
			r.logger.Warn("failed to destroy stale filter during recovery",
				slog.String("vcenter_id", r.vcenterID),
				slog.String("error", err.Error()),
			)
		}
	}

	// 3. Re-hydrate inventory baseline and reconcile
	syncStartTime := time.Now()
	var snap *TopologySnapshot
	var err error
	if r.hydrator != nil {
		snap, err = r.hydrator.HydrateTopology(ctx, r.vcenterID)
		if err != nil {
			return fmt.Errorf("topology hydration failed during recovery: %w", err)
		}
		if r.persister != nil {
			if err := r.persister.PersistTopology(ctx, snap); err != nil {
				return fmt.Errorf("topology persistence failed during recovery: %w", err)
			}
		}

		err = r.hydrator.HydrateInventory(ctx, r.vcenterID, snap, func(batch []domain.VMRecord) error {
			if r.persister != nil {
				_, err := r.persister.PersistVMBatch(ctx, batch)
				return err
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("inventory hydration failed during recovery: %w", err)
		}
	}

	// 4. Reconcile stale VMs that disappeared during disruption
	if r.persister != nil {
		reconciled, err := r.persister.ReconcileStaleVMs(ctx, r.vcenterID, syncStartTime)
		if err != nil {
			return fmt.Errorf("stale VM reconciliation failed during recovery: %w", err)
		}
		r.logger.Info("reconciliation completed",
			slog.String("vcenter_id", r.vcenterID),
			slog.Int64("reconciled_vms", reconciled),
		)
	}

	// 5. Initialize new filter and reset version token
	if r.streamer != nil {
		if err := r.streamer.InitFilter(ctx); err != nil {
			return fmt.Errorf("failed to initialize new property filter: %w", err)
		}
		r.streamer.SetVersion("")
	}

	// 6. Record recovery duration
	duration := time.Since(start)
	if r.metrics != nil {
		r.metrics.ResyncDuration.WithLabelValues(r.vcenterID).Observe(duration.Seconds())
	}
	r.logger.Info("PropertyCollector recovery completed successfully",
		slog.String("vcenter_id", r.vcenterID),
		slog.Duration("recovery_duration", duration),
	)

	// 7. Transition back to DELTA_STREAMING
	if r.fsm != nil {
		if err := r.fsm.Transition(WorkerStateDeltaStreaming); err != nil {
			return fmt.Errorf("failed to transition back to DELTA_STREAMING: %w", err)
		}
	}

	return nil
}
