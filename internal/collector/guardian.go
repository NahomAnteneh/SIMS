package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/infrastructure/sims/internal/vsphere"
)

// SessionState represents the health status of a vCenter session.
type SessionState int

const (
	// SessionStateDisconnected indicates consecutive keepalive failures reached the threshold (0).
	SessionStateDisconnected SessionState = 0
	// SessionStateConnected indicates successful heartbeats (1).
	SessionStateConnected SessionState = 1
	// SessionStateDegraded indicates 1 or more keepalive failures below the threshold (2).
	SessionStateDegraded SessionState = 2
)

// String returns a human-readable representation of SessionState.
func (s SessionState) String() string {
	switch s {
	case SessionStateConnected:
		return "connected"
	case SessionStateDegraded:
		return "degraded"
	default:
		return "disconnected"
	}
}

// SessionGuardianConfig defines parameters for proactive session health monitoring.
type SessionGuardianConfig struct {
	VCenterID              string
	Interval               time.Duration // Default: 60s (SIMS_COLLECTOR_KEEPALIVE_INTERVAL)
	Timeout                time.Duration // Default: 10s per probe (SIMS_COLLECTOR_KEEPALIVE_TIMEOUT)
	MaxConsecutiveFailures int           // Default: 2 (SIMS_COLLECTOR_KEEPALIVE_MAX_FAILURES)
	MaxStreamBlockDuration time.Duration // Default: 5m (SIMS_COLLECTOR_MAX_STREAM_BLOCK_DURATION)
}

// DefaultSessionGuardianConfig returns verified production defaults for SessionGuardian.
func DefaultSessionGuardianConfig(vcenterID string) SessionGuardianConfig {
	return SessionGuardianConfig{
		VCenterID:              vcenterID,
		Interval:               60 * time.Second,
		Timeout:                10 * time.Second,
		MaxConsecutiveFailures: 2,
		MaxStreamBlockDuration: 5 * time.Minute,
	}
}

// SessionGuardian continuously probes session health via lightweight CurrentTime SOAP calls,
// marks sessions degraded/disconnected on failures, and proactively signals the collector
// to initiate reconnection before PropertyCollector or Event streams crash.
type SessionGuardian struct {
	cfg     SessionGuardianConfig
	session vsphere.Session
	metrics *telemetry.Metrics
	logger  *slog.Logger

	mu            sync.RWMutex
	state         SessionState
	failures      int
	lastProbe     time.Time
	lastError     error
	deadCh        chan struct{}
	running       bool
	stopCh        chan struct{}
	doneCh        chan struct{}
	onSessionDead func()
}

// NewSessionGuardian constructs a new SessionGuardian instance.
func NewSessionGuardian(
	cfg SessionGuardianConfig,
	session vsphere.Session,
	metrics *telemetry.Metrics,
	logger *slog.Logger,
) *SessionGuardian {
	if cfg.Interval <= 0 {
		cfg.Interval = 60 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.MaxConsecutiveFailures <= 0 {
		cfg.MaxConsecutiveFailures = 2
	}
	if cfg.MaxStreamBlockDuration <= 0 {
		cfg.MaxStreamBlockDuration = 5 * time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}

	initialState := SessionStateConnected
	if session == nil {
		initialState = SessionStateDisconnected
	}

	g := &SessionGuardian{
		cfg:     cfg,
		session: session,
		metrics: metrics,
		logger:  logger,
		state:   initialState,
		deadCh:  make(chan struct{}, 1),
	}

	if metrics != nil {
		metrics.SessionState.WithLabelValues(cfg.VCenterID).Set(float64(initialState))
	}

	return g
}

// Config returns the guardian configuration.
func (g *SessionGuardian) Config() SessionGuardianConfig {
	return g.cfg
}

// State returns the current session health state.
func (g *SessionGuardian) State() SessionState {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.state
}

// ConsecutiveFailures returns the current number of sequential keepalive probe failures.
func (g *SessionGuardian) ConsecutiveFailures() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.failures
}

// LastError returns the error from the most recent probe failure, if any.
func (g *SessionGuardian) LastError() error {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.lastError
}

// Dead returns a receive-only channel that receives a signal when consecutive probe failures
// reach MaxConsecutiveFailures, marking the session dead.
func (g *SessionGuardian) Dead() <-chan struct{} {
	return g.deadCh
}

// SetOnSessionDead registers an optional callback invoked when the session transitions to disconnected.
func (g *SessionGuardian) SetOnSessionDead(fn func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.onSessionDead = fn
}

// UpdateSession updates the underlying session reference (e.g. after successful reconnection),
// resets failure counters, sets state to connected, and resets the dead notification channel.
func (g *SessionGuardian) UpdateSession(newSession vsphere.Session) {
	g.mu.Lock()
	g.session = newSession
	g.failures = 0
	g.lastError = nil
	g.state = SessionStateConnected
	vcenterID := g.cfg.VCenterID

	// Drain any existing signal from deadCh
	select {
	case <-g.deadCh:
	default:
	}
	g.mu.Unlock()

	if g.metrics != nil {
		g.metrics.SessionState.WithLabelValues(vcenterID).Set(float64(SessionStateConnected))
	}

	g.logger.Info("session guardian updated with new active session",
		"vcenter_id", vcenterID,
		"state", SessionStateConnected.String(),
	)
}

// Probe executes a single keepalive probe (CurrentTime) with an enforced deadline.
// It updates metrics and state transitions based on the outcome.
func (g *SessionGuardian) Probe(parentCtx context.Context) error {
	ctx, cancel := context.WithTimeout(parentCtx, g.cfg.Timeout)
	defer cancel()

	g.mu.RLock()
	sess := g.session
	g.mu.RUnlock()

	if sess == nil {
		err := errors.New("no active session configured")
		g.recordFailure(err)
		return err
	}

	_, err := sess.CurrentTime(ctx)
	if err != nil {
		g.recordFailure(err)
		return err
	}

	g.recordSuccess()
	return nil
}

func (g *SessionGuardian) recordSuccess() {
	g.mu.Lock()
	g.failures = 0
	g.lastProbe = time.Now()
	g.lastError = nil
	prevState := g.state
	g.state = SessionStateConnected
	vcenterID := g.cfg.VCenterID
	g.mu.Unlock()

	if g.metrics != nil {
		g.metrics.KeepaliveTotal.WithLabelValues(vcenterID).Inc()
		g.metrics.SessionState.WithLabelValues(vcenterID).Set(float64(SessionStateConnected))
	}

	if prevState != SessionStateConnected {
		g.logger.Info("vCenter session keep-alive healthy",
			"vcenter_id", vcenterID,
			"state", SessionStateConnected.String(),
		)
	}
}

func (g *SessionGuardian) recordFailure(err error) {
	g.mu.Lock()
	g.failures++
	g.lastProbe = time.Now()
	g.lastError = err
	curFailures := g.failures
	maxFailures := g.cfg.MaxConsecutiveFailures
	vcenterID := g.cfg.VCenterID

	var newState SessionState
	if curFailures >= maxFailures {
		newState = SessionStateDisconnected
	} else {
		newState = SessionStateDegraded
	}

	prevState := g.state
	g.state = newState
	onDead := g.onSessionDead
	deadCh := g.deadCh
	g.mu.Unlock()

	if g.metrics != nil {
		g.metrics.KeepaliveTotal.WithLabelValues(vcenterID).Inc()
		g.metrics.KeepaliveErrorsTotal.WithLabelValues(vcenterID).Inc()
		g.metrics.SessionState.WithLabelValues(vcenterID).Set(float64(newState))
	}

	g.logger.Warn("vCenter session keep-alive probe failed",
		"vcenter_id", vcenterID,
		"consecutive_failures", curFailures,
		"max_consecutive_failures", maxFailures,
		"state", newState.String(),
		"error", err,
	)

	if newState == SessionStateDisconnected && prevState != SessionStateDisconnected {
		select {
		case deadCh <- struct{}{}:
		default:
		}
		if onDead != nil {
			go onDead()
		}
	}
}

// Start spawns the background keep-alive loop. Returns an error if already running.
func (g *SessionGuardian) Start(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return fmt.Errorf("session guardian is already running")
	}
	g.running = true
	g.stopCh = make(chan struct{})
	g.doneCh = make(chan struct{})
	g.mu.Unlock()

	go g.loop(ctx)
	return nil
}

func (g *SessionGuardian) loop(ctx context.Context) {
	defer close(g.doneCh)

	ticker := time.NewTicker(g.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-g.stopCh:
			return
		case <-ticker.C:
			_ = g.Probe(ctx)
		}
	}
}

// Stop cleanly terminates the background keepalive goroutine.
func (g *SessionGuardian) Stop() {
	g.mu.Lock()
	if !g.running {
		g.mu.Unlock()
		return
	}
	g.running = false
	close(g.stopCh)
	g.mu.Unlock()

	<-g.doneCh
}
