package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/vmware/govmomi/vim25/types"
)

var (
	// ErrBatcherAlreadyRunning indicates that Run was called while the batcher is already active.
	ErrBatcherAlreadyRunning = errors.New("stream batcher is already running")
)

// BatchApplier abstracts the batch application target (e.g. IncrementalApplier).
type BatchApplier interface {
	ApplyBatch(ctx context.Context, vcenterID string, updates []types.ObjectUpdate) error
}

// BatcherConfig defines operational parameters for StreamBatcher.
type BatcherConfig struct {
	VCenterID     string
	MaxBatchSize  int
	FlushInterval time.Duration
	DrainTimeout  time.Duration
	Backoff       BackoffConfig
}

// DefaultBatcherConfig returns production-safe defaults for stream batching.
func DefaultBatcherConfig(vcenterID string) BatcherConfig {
	return BatcherConfig{
		VCenterID:     vcenterID,
		MaxBatchSize:  100,
		FlushInterval: 50 * time.Millisecond,
		DrainTimeout:  5 * time.Second,
		Backoff: BackoffConfig{
			BaseDelay: 100 * time.Millisecond,
			MaxDelay:  5 * time.Second,
			Factor:    2.0,
			Jitter:    0.2,
		},
	}
}

// StreamBatcher pulls ObjectUpdates from BoundedQueue and flushes them to persistence in batches.
// On database errors, it retries with exponential backoff while holding the queue head,
// causing the bounded queue to fill up and backpressure the producer.
type StreamBatcher struct {
	cfg      BatcherConfig
	queue    *BoundedQueue[types.ObjectUpdate]
	applier  BatchApplier
	metrics  *telemetry.Metrics
	logger   *slog.Logger
	registry *CleanupRegistry

	running atomic.Bool
	stopCh  chan struct{}
	doneCh  chan struct{}
	drainID int
	mu      sync.Mutex
}

// NewStreamBatcher constructs a StreamBatcher.
func NewStreamBatcher(
	cfg BatcherConfig,
	queue *BoundedQueue[types.ObjectUpdate],
	applier BatchApplier,
	metrics *telemetry.Metrics,
	logger *slog.Logger,
	registry *CleanupRegistry,
) *StreamBatcher {
	if cfg.MaxBatchSize <= 0 {
		cfg.MaxBatchSize = 100
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 50 * time.Millisecond
	}
	if cfg.DrainTimeout <= 0 {
		cfg.DrainTimeout = 5 * time.Second
	}
	if cfg.Backoff.BaseDelay <= 0 {
		cfg.Backoff = DefaultBackoffConfig()
	}
	if logger == nil {
		logger = slog.Default()
	}

	b := &StreamBatcher{
		cfg:      cfg,
		queue:    queue,
		applier:  applier,
		metrics:  metrics,
		logger:   logger,
		registry: registry,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}

	if registry != nil {
		b.drainID = registry.RegisterDrain("stream-batcher-drain", b.Drain)
	}

	return b
}

// Run executes the consumer batching loop until ctx is canceled or Stop() is called.
func (b *StreamBatcher) Run(ctx context.Context) error {
	if !b.running.CompareAndSwap(false, true) {
		return ErrBatcherAlreadyRunning
	}
	defer func() {
		b.running.Store(false)
		close(b.doneCh)
	}()

	b.logger.Info("StreamBatcher started",
		slog.String("vcenter_id", b.cfg.VCenterID),
		slog.Int("max_batch_size", b.cfg.MaxBatchSize),
		slog.Duration("flush_interval", b.cfg.FlushInterval),
	)

	backoff := NewBackoff(b.cfg.Backoff, nil)

	for {
		select {
		case <-ctx.Done():
			b.logger.Info("StreamBatcher context canceled; stopping", slog.String("vcenter_id", b.cfg.VCenterID))
			return ctx.Err()
		case <-b.stopCh:
			b.logger.Info("StreamBatcher stop requested", slog.String("vcenter_id", b.cfg.VCenterID))
			return nil
		default:
		}

		// Pull batch from bounded queue
		batch, err := b.queue.PopBatch(ctx, b.cfg.MaxBatchSize, b.cfg.FlushInterval)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, ErrQueueClosed) {
				return nil
			}
			b.logger.Debug("error popping batch from queue", slog.String("error", err.Error()))
			continue
		}

		if len(batch) == 0 {
			continue
		}

		// Apply batch with retries on database failure
		if err := b.applyWithRetry(ctx, batch, backoff); err != nil {
			return err
		}
	}
}

// applyWithRetry executes applier.ApplyBatch, backing off upon database outage until success or cancellation.
func (b *StreamBatcher) applyWithRetry(ctx context.Context, batch []types.ObjectUpdate, backoff *Backoff) error {
	start := time.Now()
	attempt := 0

	for {
		applyErr := b.applier.ApplyBatch(ctx, b.cfg.VCenterID, batch)
		if applyErr == nil {
			duration := time.Since(start)
			if b.metrics != nil {
				b.metrics.BatchFlushDuration.WithLabelValues(b.cfg.VCenterID).Observe(duration.Seconds())
				b.metrics.BatchFlushTotal.WithLabelValues(b.cfg.VCenterID, "success").Inc()
			}
			return nil
		}

		if errors.Is(ctx.Err(), context.Canceled) {
			return ctx.Err()
		}

		if b.metrics != nil {
			b.metrics.BatchFlushTotal.WithLabelValues(b.cfg.VCenterID, "error").Inc()
		}

		delay := backoff.Delay(attempt)
		attempt++
		b.logger.Warn("batch persistence failed; retrying with backoff",
			slog.String("vcenter_id", b.cfg.VCenterID),
			slog.Int("batch_size", len(batch)),
			slog.Duration("retry_delay", delay),
			slog.String("error", applyErr.Error()),
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-b.stopCh:
			return nil
		case <-time.After(delay):
		}
	}
}

// Stop signals the batcher to stop receiving new batches.
func (b *StreamBatcher) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.stopCh:
	default:
		close(b.stopCh)
	}
}

// Drain waits for in-flight items to flush to persistence within the drain timeout (Stage 2).
func (b *StreamBatcher) Drain(ctx context.Context) error {
	drainCtx, cancel := context.WithTimeout(ctx, b.cfg.DrainTimeout)
	defer cancel()

	b.logger.Info("draining in-flight queue items",
		slog.String("vcenter_id", b.cfg.VCenterID),
		slog.Int("queue_depth", b.queue.Depth()),
	)

	// Close queue to unblock any pending producers and prevent new inputs
	b.queue.Close()

	// Drain remaining items
	for b.queue.Depth() > 0 {
		select {
		case <-drainCtx.Done():
			return fmt.Errorf("queue drain timed out with %d items remaining: %w", b.queue.Depth(), drainCtx.Err())
		default:
		}

		batch, err := b.queue.PopBatch(drainCtx, b.cfg.MaxBatchSize, 10*time.Millisecond)
		if err != nil && !errors.Is(err, ErrQueueClosed) {
			break
		}
		if len(batch) > 0 {
			if applyErr := b.applier.ApplyBatch(drainCtx, b.cfg.VCenterID, batch); applyErr != nil {
				b.logger.Warn("error applying remaining batch during drain",
					slog.String("vcenter_id", b.cfg.VCenterID),
					slog.String("error", applyErr.Error()),
				)
			}
		}
	}

	b.Stop()
	return nil
}
