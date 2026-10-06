package persistence

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/infrastructure/sims/generated/db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PurgeOptions configures bounded batch tombstone purging.
type PurgeOptions struct {
	BatchSize           int32         // Max rows to delete in a single batch (default 500)
	MaxBatches          int           // Max number of batches to run per execution (0 for unbounded, up to all expired)
	RetentionPeriod     time.Duration // Time window for tombstones (e.g. 30 days)
	SleepBetweenBatches time.Duration // Throttling delay between batches to minimize lock contention
}

// DefaultPurgeOptions returns production-safe purge options.
func DefaultPurgeOptions() PurgeOptions {
	return PurgeOptions{
		BatchSize:           500,
		MaxBatches:          100,
		RetentionPeriod:     30 * 24 * time.Hour,
		SleepBetweenBatches: 25 * time.Millisecond,
	}
}

// PurgeReport captures operational statistics for a tombstone purge execution.
type PurgeReport struct {
	TotalRowsPurged int64         `json:"total_rows_purged"`
	BatchesRun      int           `json:"batches_run"`
	TotalDuration   time.Duration `json:"total_duration_ms"`
	LockDuration    time.Duration `json:"lock_duration_ms"` // Cumulative time spent in DELETE statements
}

// TombstonePurger executes bounded, batch-oriented deletion of tombstoned records
// to prevent table bloat without incurring massive table lock times or replication lag.
type TombstonePurger struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	logger  *slog.Logger
}

// NewTombstonePurger creates a new TombstonePurger.
func NewTombstonePurger(pool *pgxpool.Pool, queries *db.Queries, logger *slog.Logger) *TombstonePurger {
	if logger == nil {
		logger = slog.Default()
	}
	return &TombstonePurger{
		pool:    pool,
		queries: queries,
		logger:  logger,
	}
}

// PurgeBatch executes a single bounded batch delete for VMs soft-deleted before the cutoff.
// Returns the number of purged rows, the lock latency duration, and any error.
func (p *TombstonePurger) PurgeBatch(ctx context.Context, cutoff time.Time, batchSize int32) (int64, time.Duration, error) {
	if batchSize <= 0 {
		batchSize = 500
	}

	start := time.Now()
	rowsAffected, err := p.queries.PurgeTombstonedVMsBatch(ctx, db.PurgeTombstonedVMsBatchParams{
		DeletedAt: pgtype.Timestamptz{
			Time:  cutoff,
			Valid: true,
		},
		Limit: batchSize,
	})
	lockLatency := time.Since(start)

	if err != nil {
		return 0, lockLatency, fmt.Errorf("failed to purge tombstone batch: %w", err)
	}

	return rowsAffected, lockLatency, nil
}

// PurgeTombstones executes bounded purging across multiple batches until either
// all eligible records are purged or MaxBatches is reached.
func (p *TombstonePurger) PurgeTombstones(ctx context.Context, opts PurgeOptions) (*PurgeReport, error) {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 500
	}

	cutoff := time.Now().Add(-opts.RetentionPeriod)
	report := &PurgeReport{}
	overallStart := time.Now()

	for {
		if ctx.Err() != nil {
			report.TotalDuration = time.Since(overallStart)
			return report, ctx.Err()
		}

		if opts.MaxBatches > 0 && report.BatchesRun >= opts.MaxBatches {
			p.logger.Info("tombstone purge reached max batches limit",
				slog.Int("batches_run", report.BatchesRun),
				slog.Int64("total_rows_purged", report.TotalRowsPurged),
			)
			break
		}

		rowsPurged, lockLatency, err := p.PurgeBatch(ctx, cutoff, opts.BatchSize)
		if err != nil {
			report.TotalDuration = time.Since(overallStart)
			return report, err
		}

		report.BatchesRun++
		report.TotalRowsPurged += rowsPurged
		report.LockDuration += lockLatency

		p.logger.Debug("purged tombstone batch",
			slog.Int("batch", report.BatchesRun),
			slog.Int64("rows_purged", rowsPurged),
			slog.Duration("lock_latency", lockLatency),
		)

		// If this batch deleted fewer rows than requested, no more tombstones match
		if rowsPurged < int64(opts.BatchSize) {
			break
		}

		if opts.SleepBetweenBatches > 0 {
			select {
			case <-time.After(opts.SleepBetweenBatches):
			case <-ctx.Done():
				report.TotalDuration = time.Since(overallStart)
				return report, ctx.Err()
			}
		}
	}

	report.TotalDuration = time.Since(overallStart)
	p.logger.Info("tombstone purge completed",
		slog.Int64("total_rows_purged", report.TotalRowsPurged),
		slog.Int("batches_run", report.BatchesRun),
		slog.Duration("total_duration", report.TotalDuration),
		slog.Duration("total_lock_duration", report.LockDuration),
	)

	return report, nil
}
