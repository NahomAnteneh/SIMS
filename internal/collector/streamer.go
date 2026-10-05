package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/infrastructure/sims/internal/vsphere"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

var (
	// ErrStreamerAlreadyRunning is returned when Run is called while another streaming loop is active.
	ErrStreamerAlreadyRunning = errors.New("streamer is already running: single goroutine ownership invariant violated")

	// ErrInvalidCollectorVersion indicates that the PropertyCollector history rolled over or lost session state.
	ErrInvalidCollectorVersion = errors.New("invalid_collector_version")

	// ErrStreamHung indicates that WaitForUpdatesEx blocked beyond the configured maximum duration.
	ErrStreamHung = errors.New("stream_hung")

	// ErrNoFilter indicates the streaming loop was started without an active property filter.
	ErrNoFilter = errors.New("no property filter initialized")
)

// StreamerConfig defines operational parameters for PropertyCollector streaming.
type StreamerConfig struct {
	VCenterID              string
	MaxWaitSeconds         int32         // Default: 60s (server-side wait interval)
	MaxObjects             int32         // Default: 500
	CallDeadline           time.Duration // Default: 90s (per-call deadline for hang detection: MaxWaitSeconds + 30s)
	MaxStreamBlockDuration time.Duration // Default: 5m (how long backpressure may pause the stream before proactive RESYNCING)
}

// DefaultStreamerConfig returns a verified production-safe default configuration.
func DefaultStreamerConfig(vcenterID string) StreamerConfig {
	return StreamerConfig{
		VCenterID:              vcenterID,
		MaxWaitSeconds:         60,
		MaxObjects:             500,
		CallDeadline:           90 * time.Second,
		MaxStreamBlockDuration: 5 * time.Minute,
	}
}

// UpdateHandler receives batches of ObjectUpdates for processing.
type UpdateHandler func(ctx context.Context, updates []types.ObjectUpdate) error

// PropertyStreamer manages real-time PropertyCollector updates with single-goroutine ownership.
type PropertyStreamer struct {
	pc       vsphere.PropertyCollector
	filter   vsphere.PropertyFilter
	spec     types.PropertyFilterSpec
	cfg      StreamerConfig
	metrics  *telemetry.Metrics
	logger   *slog.Logger
	registry *CleanupRegistry

	mu       sync.Mutex
	running  atomic.Bool
	version  string
	filterID int
}

// NewPropertyStreamer constructs a PropertyStreamer instance.
func NewPropertyStreamer(
	pc vsphere.PropertyCollector,
	spec types.PropertyFilterSpec,
	cfg StreamerConfig,
	metrics *telemetry.Metrics,
	logger *slog.Logger,
	registry *CleanupRegistry,
) *PropertyStreamer {
	if cfg.MaxWaitSeconds <= 0 {
		cfg.MaxWaitSeconds = 60
	}
	if cfg.MaxObjects <= 0 {
		cfg.MaxObjects = 500
	}
	if cfg.CallDeadline <= 0 {
		cfg.CallDeadline = time.Duration(cfg.MaxWaitSeconds)*time.Second + 30*time.Second
	}
	if cfg.MaxStreamBlockDuration <= 0 {
		cfg.MaxStreamBlockDuration = 5 * time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &PropertyStreamer{
		pc:       pc,
		spec:     spec,
		cfg:      cfg,
		metrics:  metrics,
		logger:   logger,
		registry: registry,
	}
}

// InitFilter creates the server-side PropertyFilter and registers it with the CleanupRegistry.
func (s *PropertyStreamer) InitFilter(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.filter != nil {
		_ = s.filter.Destroy(ctx)
		if s.registry != nil && s.filterID > 0 {
			s.registry.Unregister(s.filterID)
			s.filterID = 0
		}
		s.filter = nil
	}

	filter, err := s.pc.CreateFilter(ctx, s.spec)
	if err != nil {
		return fmt.Errorf("failed to create property filter: %w", err)
	}

	s.filter = filter
	if s.registry != nil {
		s.filterID = s.registry.RegisterPropertyFilter(filter)
	}
	return nil
}

// DestroyFilter destroys the server-side PropertyFilter if active.
func (s *PropertyStreamer) DestroyFilter(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.filter != nil {
		err := s.filter.Destroy(ctx)
		if s.registry != nil && s.filterID > 0 {
			s.registry.Unregister(s.filterID)
			s.filterID = 0
		}
		s.filter = nil
		return err
	}
	return nil
}

// Version returns the current PropertyCollector version token.
func (s *PropertyStreamer) Version() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.version
}

// SetVersion sets the PropertyCollector version token (used during initial hydration or recovery reset).
func (s *PropertyStreamer) SetVersion(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.version = v
}

// IsRunning reports whether the streaming loop is actively running.
func (s *PropertyStreamer) IsRunning() bool {
	return s.running.Load()
}

// Run executes the continuous WaitForUpdatesEx streaming loop.
// Broadcom documentation invariant: exactly one goroutine owns the streaming loop.
func (s *PropertyStreamer) Run(ctx context.Context, handler UpdateHandler) error {
	if !s.running.CompareAndSwap(false, true) {
		return ErrStreamerAlreadyRunning
	}
	defer s.running.Store(false)

	s.mu.Lock()
	hasFilter := s.filter != nil
	s.mu.Unlock()
	if !hasFilter {
		if err := s.InitFilter(ctx); err != nil {
			return err
		}
	}

	s.logger.Info("PropertyCollector streaming loop started",
		slog.String("vcenter_id", s.cfg.VCenterID),
		slog.Int("max_wait_seconds", int(s.cfg.MaxWaitSeconds)),
	)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("PropertyCollector streaming loop stopping: context canceled",
				slog.String("vcenter_id", s.cfg.VCenterID),
			)
			return ctx.Err()
		default:
		}

		waitOpt := &types.WaitOptions{
			MaxWaitSeconds: &s.cfg.MaxWaitSeconds,
		}
		if s.cfg.MaxObjects > 0 {
			waitOpt.MaxObjectUpdates = s.cfg.MaxObjects
		}

		s.mu.Lock()
		curVersion := s.version
		s.mu.Unlock()

		start := time.Now()

		// Wrap with per-call deadline (hang detection)
		waitCtx := ctx
		var cancelWait context.CancelFunc
		if s.cfg.CallDeadline > 0 {
			waitCtx, cancelWait = context.WithTimeout(ctx, s.cfg.CallDeadline)
		}

		updateSet, err := s.pc.WaitForUpdatesEx(waitCtx, curVersion, waitOpt)
		if cancelWait != nil {
			cancelWait()
		}

		if err != nil {
			if errors.Is(err, context.Canceled) {
				return ctx.Err()
			}
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				// Proactive hang detection fired on per-call deadline
				s.logger.Warn("PropertyCollector WaitForUpdatesEx hung beyond call deadline; triggering recovery",
					slog.Duration("call_deadline", s.cfg.CallDeadline),
					slog.String("vcenter_id", s.cfg.VCenterID),
				)
				if cancelErr := s.pc.CancelWaitForUpdates(context.Background()); cancelErr != nil {
					s.logger.Debug("failed to cancel wait for updates", slog.String("error", cancelErr.Error()))
				}
				return fmt.Errorf("%w: call exceeded %v", ErrStreamHung, s.cfg.CallDeadline)
			}
			if IsInvalidCollectorVersion(err) {
				s.logger.Warn("PropertyCollector returned InvalidCollectorVersion; triggering re-sync",
					slog.String("error", err.Error()),
					slog.String("last_version", curVersion),
					slog.String("vcenter_id", s.cfg.VCenterID),
				)
				return fmt.Errorf("%w: %v", ErrInvalidCollectorVersion, err)
			}
			return err
		}

		if updateSet == nil {
			continue
		}

		// Update version token
		s.mu.Lock()
		s.version = updateSet.Version
		s.mu.Unlock()

		latency := time.Since(start)
		if s.metrics != nil {
			s.metrics.PropertyUpdateLatency.WithLabelValues(s.cfg.VCenterID).Observe(latency.Seconds())
		}

		// Handle Truncated updates (S2.3)
		isTruncated := updateSet.Truncated != nil && *updateSet.Truncated
		if isTruncated {
			if s.metrics != nil {
				s.metrics.PropertyUpdatesTruncatedTotal.WithLabelValues(s.cfg.VCenterID).Inc()
			}
			s.logger.Debug("PropertyCollector returned truncated updates; draining next batch immediately",
				slog.String("vcenter_id", s.cfg.VCenterID),
				slog.String("next_version", updateSet.Version),
			)
		}

		// Extract all ObjectUpdates across filter sets
		var allUpdates []types.ObjectUpdate
		for _, fu := range updateSet.FilterSet {
			for _, obj := range fu.ObjectSet {
				allUpdates = append(allUpdates, obj)
				if s.metrics != nil {
					s.metrics.PropertyUpdatesTotal.WithLabelValues(s.cfg.VCenterID, string(obj.Kind)).Inc()
				}
			}
		}

		if len(allUpdates) > 0 && handler != nil {
			if err := handler(ctx, allUpdates); err != nil {
				return fmt.Errorf("update handler failed: %w", err)
			}
		}

		// If truncated is true, the loop immediately calls WaitForUpdatesEx with the
		// new version without sleeping to drain remaining server deltas.
	}
}

// IsInvalidCollectorVersion inspects an error to determine whether it is an InvalidCollectorVersion fault.
func IsInvalidCollectorVersion(err error) bool {
	if err == nil {
		return false
	}
	if soap.IsSoapFault(err) {
		soapFault := soap.ToSoapFault(err)
		if soapFault != nil {
			switch soapFault.VimFault().(type) {
			case *types.InvalidCollectorVersion, types.InvalidCollectorVersion:
				return true
			}
			if strings.Contains(soapFault.String, "InvalidCollectorVersion") ||
				strings.Contains(fmt.Sprintf("%v", soapFault.Detail), "InvalidCollectorVersion") {
				return true
			}
		}
	}
	if soap.IsVimFault(err) {
		switch soap.ToVimFault(err).(type) {
		case *types.InvalidCollectorVersion:
			return true
		}
	}
	return strings.Contains(err.Error(), "InvalidCollectorVersion")
}

// BuildVMPropertyFilterSpec builds a production PropertyFilterSpec for streaming VM updates from a ContainerView.
func BuildVMPropertyFilterSpec(containerView types.ManagedObjectReference) types.PropertyFilterSpec {
	ts := &types.TraversalSpec{
		SelectionSpec: types.SelectionSpec{Name: "traverseContainerView"},
		Type:          "ContainerView",
		Path:          "view",
		Skip:          types.NewBool(false),
	}

	propSpec := types.PropertySpec{
		Type: "VirtualMachine",
		PathSet: []string{
			"name",
			"summary.config",
			"summary.runtime",
			"summary.guest",
			"summary.customValue",
			"customValue",
			"config.hardware.device",
			"guest.net",
			"parent",
			"runtime.host",
			"runtime.powerState",
			"runtime.connectionState",
			"runtime.bootTime",
		},
	}

	return types.PropertyFilterSpec{
		PropSet: []types.PropertySpec{propSpec},
		ObjectSet: []types.ObjectSpec{
			{
				Obj:       containerView,
				Skip:      types.NewBool(false),
				SelectSet: []types.BaseSelectionSpec{ts},
			},
		},
	}
}
