package collector

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// TriggerType indicates why a reconciliation was scheduled (S4.7)
type TriggerType string

const (
	TriggerBaselineScheduled TriggerType = "BASELINE_24H"
	TriggerPostRecovery      TriggerType = "POST_RECOVERY"
	TriggerManualResync      TriggerType = "MANUAL_RESYNC"
)

// ReconcileRequest represents a requested reconciliation run
type ReconcileRequest struct {
	VCenterID   uuid.UUID
	Trigger     TriggerType
	RequestedAt time.Time
}

// TriggerConfig defines timing rules for reconciliation triggers (S4.7)
type TriggerConfig struct {
	BaselineInterval time.Duration // Default 24h
	DebounceWindow   time.Duration // Minimum cooldown between runs (default 30s)
}

func (c *TriggerConfig) defaults() {
	if c.BaselineInterval <= 0 {
		c.BaselineInterval = 24 * time.Hour
	}
	if c.DebounceWindow <= 0 {
		c.DebounceWindow = 30 * time.Second
	}
}

// ReconciliationTriggerManager coordinates automatic and on-demand reconciliation triggers (S4.7)
type TriggerManager struct {
	cfg       TriggerConfig
	handler   func(ctx context.Context, req ReconcileRequest)
	logger    *slog.Logger
	queueChan chan ReconcileRequest

	mu           sync.Mutex
	lastRun      map[uuid.UUID]time.Time
	lastEnqueued map[uuid.UUID]time.Time
	targets      []uuid.UUID
	cancelLoop   context.CancelFunc
	wg           sync.WaitGroup
}

func NewTriggerManager(
	cfg TriggerConfig,
	handler func(ctx context.Context, req ReconcileRequest),
	logger *slog.Logger,
) *TriggerManager {
	cfg.defaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &TriggerManager{
		cfg:          cfg,
		handler:      handler,
		logger:       logger.With(slog.String("component", "reconcile-triggers")),
		queueChan:    make(chan ReconcileRequest, 50),
		lastRun:      make(map[uuid.UUID]time.Time),
		lastEnqueued: make(map[uuid.UUID]time.Time),
	}
}

// SetTargets updates known targets for baseline sweep
func (tm *TriggerManager) SetTargets(targets []uuid.UUID) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.targets = targets
}

// TriggerManual requests on-demand reconciliation (e.g. from POST /v1/vcenters/{id}/resync)
func (tm *TriggerManager) TriggerManual(vcenterID uuid.UUID) {
	tm.enqueue(ReconcileRequest{
		VCenterID:   vcenterID,
		Trigger:     TriggerManualResync,
		RequestedAt: time.Now(),
	})
}

// TriggerPostRecovery requests reconciliation after stream recovery or filter rebuild
func (tm *TriggerManager) TriggerPostRecovery(vcenterID uuid.UUID) {
	tm.enqueue(ReconcileRequest{
		VCenterID:   vcenterID,
		Trigger:     TriggerPostRecovery,
		RequestedAt: time.Now(),
	})
}

func (tm *TriggerManager) enqueue(req ReconcileRequest) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	last, exists := tm.lastEnqueued[req.VCenterID]
	if exists && time.Since(last) < tm.cfg.DebounceWindow && req.Trigger != TriggerManualResync {
		tm.logger.Debug("debouncing reconciliation request",
			slog.String("vcenter_id", req.VCenterID.String()),
			slog.String("trigger", string(req.Trigger)),
		)
		return
	}

	select {
	case tm.queueChan <- req:
		tm.lastEnqueued[req.VCenterID] = time.Now()
		tm.logger.Info("enqueued reconciliation request",
			slog.String("vcenter_id", req.VCenterID.String()),
			slog.String("trigger", string(req.Trigger)),
		)
	default:
		tm.logger.Warn("reconciliation request queue full; dropping request",
			slog.String("vcenter_id", req.VCenterID.String()),
		)
	}
}

// Start begins processing reconciliation triggers and baseline scheduled sweeps
func (tm *TriggerManager) Start(ctx context.Context) {
	loopCtx, cancel := context.WithCancel(ctx)
	tm.cancelLoop = cancel

	tm.wg.Add(1)
	go tm.runWorker(loopCtx)

	tm.wg.Add(1)
	go tm.runBaselineScheduler(loopCtx)
}

func (tm *TriggerManager) runWorker(ctx context.Context) {
	defer tm.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-tm.queueChan:
			if tm.handler != nil {
				tm.handler(ctx, req)
				tm.mu.Lock()
				tm.lastRun[req.VCenterID] = time.Now()
				tm.mu.Unlock()
			}
		}
	}
}

func (tm *TriggerManager) runBaselineScheduler(ctx context.Context) {
	defer tm.wg.Done()
	ticker := time.NewTicker(tm.cfg.BaselineInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tm.mu.Lock()
			targets := append([]uuid.UUID(nil), tm.targets...)
			tm.mu.Unlock()

			for _, targetID := range targets {
				tm.enqueue(ReconcileRequest{
					VCenterID:   targetID,
					Trigger:     TriggerBaselineScheduled,
					RequestedAt: time.Now(),
				})
			}
		}
	}
}

// Stop terminates background trigger processing
func (tm *TriggerManager) Stop() {
	if tm.cancelLoop != nil {
		tm.cancelLoop()
	}
	tm.wg.Wait()
}
