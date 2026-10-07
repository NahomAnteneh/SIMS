package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/infrastructure/sims/generated/db"
	"github.com/infrastructure/sims/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var (
	ErrLeaseHeld = errors.New("lease currently held by another worker")
	ErrLeaseLost = errors.New("lease lost or fencing version mismatch")
)

// LeaseConfig defines tuning parameters for PostgreSQL worker leases (S4.2)
type LeaseConfig struct {
	WorkerID          string
	LeaseTTL          time.Duration // Default 30s
	HeartbeatInterval time.Duration // Default 10s
	HeartbeatJitter   time.Duration // Default 2s
}

func (c *LeaseConfig) defaults() {
	if c.WorkerID == "" {
		c.WorkerID = fmt.Sprintf("worker-%s", uuid.NewString()[:8])
	}
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = 30 * time.Second
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 10 * time.Second
	}
	if c.HeartbeatJitter <= 0 {
		c.HeartbeatJitter = 2 * time.Second
	}
}

// ActiveLease holds runtime state for an acquired lease
type ActiveLease struct {
	TargetID   uuid.UUID
	HolderID   string
	Version    int64
	LeaseUntil time.Time
	LostChan   chan struct{}
	cancelFunc context.CancelFunc
	mu         sync.RWMutex
}

// IsLost checks if the lease was lost
func (al *ActiveLease) IsLost() bool {
	select {
	case <-al.LostChan:
		return true
	default:
		return false
	}
}

// Close stops the background heartbeat renewal loop for this lease
func (al *ActiveLease) Close() {
	if al.cancelFunc != nil {
		al.cancelFunc()
	}
}

// LeaseCoordinator manages transactional lease claims, renewals, and failover (S4.2)
type LeaseCoordinator struct {
	workerID string
	cfg      LeaseConfig
	pool     *pgxpool.Pool
	queries  *db.Queries
	logger   *slog.Logger

	mu     sync.RWMutex
	leases map[uuid.UUID]*ActiveLease
}

func NewLeaseCoordinator(pool *pgxpool.Pool, queries *db.Queries, cfg LeaseConfig, logger *slog.Logger) *LeaseCoordinator {
	cfg.defaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &LeaseCoordinator{
		workerID: cfg.WorkerID,
		cfg:      cfg,
		pool:     pool,
		queries:  queries,
		logger:   logger.With(slog.String("component", "lease-coordinator"), slog.String("worker_id", cfg.WorkerID)),
		leases:   make(map[uuid.UUID]*ActiveLease),
	}
}

// WorkerID returns this coordinator's unique worker identity
func (lc *LeaseCoordinator) WorkerID() string {
	return lc.workerID
}

// Acquire attempts to atomically acquire or takeover a lease for the given target
func (lc *LeaseCoordinator) Acquire(ctx context.Context, targetID uuid.UUID) (*ActiveLease, error) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if existing, ok := lc.leases[targetID]; ok && !existing.IsLost() {
		return existing, nil
	}

	pgTargetID := pgtype.UUID{Bytes: targetID, Valid: true}
	leaseUntil := time.Now().Add(lc.cfg.LeaseTTL)

	_, span := telemetry.StartSpan(ctx, "lease.acquire",
		trace.WithAttributes(
			attribute.String("target.id", targetID.String()),
			attribute.String("worker.id", lc.workerID),
		),
	)
	row, err := lc.queries.AcquireLease(ctx, db.AcquireLeaseParams{
		TargetID:   pgTargetID,
		HolderID:   lc.workerID,
		LeaseUntil: pgtype.Timestamptz{Time: leaseUntil, Valid: true},
	})
	if err != nil {
		telemetry.RecordSafeError(span, err)
		span.End()
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrLeaseHeld
		}
		return nil, fmt.Errorf("failed to acquire lease: %w", err)
	}
	span.SetAttributes(attribute.Int64("lease.version", row.Version))
	span.End()

	heartbeatCtx, cancel := context.WithCancel(context.Background())
	al := &ActiveLease{
		TargetID:   targetID,
		HolderID:   lc.workerID,
		Version:    row.Version,
		LeaseUntil: row.LeaseUntil.Time,
		LostChan:   make(chan struct{}),
		cancelFunc: cancel,
	}

	lc.leases[targetID] = al
	lc.logger.Info("lease acquired",
		slog.String("target_id", targetID.String()),
		slog.Int64("version", row.Version),
		slog.Time("lease_until", row.LeaseUntil.Time),
	)

	// Launch background heartbeat renewal
	// #nosec G118 -- heartbeat lifecycle outlives the Acquire request and is managed via ActiveLease.cancelFunc
	go lc.runHeartbeat(heartbeatCtx, al)

	return al, nil
}

// Renew performs an explicit lease renewal heartbeat
func (lc *LeaseCoordinator) Renew(ctx context.Context, al *ActiveLease) error {
	al.mu.Lock()
	defer al.mu.Unlock()

	pgTargetID := pgtype.UUID{Bytes: al.TargetID, Valid: true}
	newUntil := time.Now().Add(lc.cfg.LeaseTTL)

	_, span := telemetry.StartSpan(ctx, "lease.renew",
		trace.WithAttributes(
			attribute.String("target.id", al.TargetID.String()),
			attribute.String("worker.id", lc.workerID),
			attribute.Int64("lease.version", al.Version),
		),
	)
	row, err := lc.queries.RenewLease(ctx, db.RenewLeaseParams{
		TargetID:   pgTargetID,
		HolderID:   lc.workerID,
		LeaseUntil: pgtype.Timestamptz{Time: newUntil, Valid: true},
		Version:    al.Version,
	})
	if err != nil {
		telemetry.RecordSafeError(span, err)
		span.End()
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLeaseLost
		}
		return fmt.Errorf("renew query failed: %w", err)
	}
	span.End()

	al.LeaseUntil = row.LeaseUntil.Time
	return nil
}

func (lc *LeaseCoordinator) runHeartbeat(ctx context.Context, al *ActiveLease) {
	consecutiveFailures := 0
	const maxRenewalFailures = 3

	for {
		// Calculate jittered heartbeat interval: interval +/- jitter
		jitterRange := int64(lc.cfg.HeartbeatJitter) * 2
		var jitterOffset time.Duration
		if jitterRange > 0 {
			// #nosec G404 -- non-cryptographic timing jitter for heartbeat desynchronization
			jitterOffset = time.Duration(rand.Int64N(jitterRange)) - lc.cfg.HeartbeatJitter
		}
		interval := lc.cfg.HeartbeatInterval + jitterOffset
		if interval < time.Second {
			interval = time.Second
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}

		renewCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := lc.Renew(renewCtx, al)
		cancel()

		if err != nil {
			consecutiveFailures++
			lc.logger.Warn("heartbeat lease renewal failed",
				slog.String("target_id", al.TargetID.String()),
				slog.Int("consecutive_failures", consecutiveFailures),
				slog.Int("threshold", maxRenewalFailures),
				slog.String("error", err.Error()),
			)
			if errors.Is(err, ErrLeaseLost) || consecutiveFailures >= maxRenewalFailures {
				lc.logger.Warn("heartbeat lease renewal failures exceeded threshold; marking lease lost",
					slog.String("target_id", al.TargetID.String()),
				)
				lc.mu.Lock()
				select {
				case <-al.LostChan:
				default:
					close(al.LostChan)
				}
				delete(lc.leases, al.TargetID)
				lc.mu.Unlock()
				return
			}
			continue
		}

		consecutiveFailures = 0

		lc.logger.Debug("heartbeat lease renewed successfully",
			slog.String("target_id", al.TargetID.String()),
			slog.Time("lease_until", al.LeaseUntil),
		)
	}
}

// Release gracefully gives up ownership of the lease for targetID
func (lc *LeaseCoordinator) Release(ctx context.Context, targetID uuid.UUID) error {
	lc.mu.Lock()
	al, ok := lc.leases[targetID]
	if !ok {
		lc.mu.Unlock()
		return nil
	}
	delete(lc.leases, targetID)
	lc.mu.Unlock()

	al.cancelFunc()
	select {
	case <-al.LostChan:
	default:
		close(al.LostChan)
	}

	pgTargetID := pgtype.UUID{Bytes: targetID, Valid: true}
	_, span := telemetry.StartSpan(ctx, "lease.release",
		trace.WithAttributes(
			attribute.String("target.id", targetID.String()),
			attribute.String("worker.id", lc.workerID),
		),
	)
	defer span.End()

	err := lc.queries.ReleaseLease(ctx, db.ReleaseLeaseParams{
		TargetID: pgTargetID,
		HolderID: lc.workerID,
	})
	if err != nil {
		telemetry.RecordSafeError(span, err)
		lc.logger.Warn("failed to release lease in database",
			slog.String("target_id", targetID.String()),
			slog.String("error", err.Error()),
		)
		return err
	}

	lc.logger.Info("lease gracefully released", slog.String("target_id", targetID.String()))
	return nil
}

// ReleaseAll gracefully releases all active leases owned by this worker
func (lc *LeaseCoordinator) ReleaseAll(ctx context.Context) error {
	lc.mu.Lock()
	targets := make([]uuid.UUID, 0, len(lc.leases))
	for tid := range lc.leases {
		targets = append(targets, tid)
	}
	lc.mu.Unlock()

	var errs []error
	for _, tid := range targets {
		if err := lc.Release(ctx, tid); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors releasing leases: %v", errs)
	}
	return nil
}

// GetActiveLease returns the active lease if held by this worker
func (lc *LeaseCoordinator) GetActiveLease(targetID uuid.UUID) (*ActiveLease, bool) {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	al, ok := lc.leases[targetID]
	if !ok || al.IsLost() {
		return nil, false
	}
	return al, true
}

// ListActiveLeases returns all active leases held by this worker
func (lc *LeaseCoordinator) ListActiveLeases() []*ActiveLease {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	res := make([]*ActiveLease, 0, len(lc.leases))
	for _, al := range lc.leases {
		if !al.IsLost() {
			res = append(res, al)
		}
	}
	return res
}

// ListAvailableTargets retrieves targets needing assignment or expired targets for takeover
func (lc *LeaseCoordinator) ListAvailableTargets(ctx context.Context) ([]db.ListAvailableTargetsRow, error) {
	return lc.queries.ListAvailableTargets(ctx)
}
