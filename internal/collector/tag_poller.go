package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/vmware/govmomi/vim25/types"
)

// TagPersister defines the persistence interface required by TagPoller.
type TagPersister interface {
	ListActiveVMMorefs(ctx context.Context, vcenterID string) ([]string, error)
	GetVMTags(ctx context.Context, vcenterID string) (map[string][]domain.TagRecord, error)
	UpdateVMTags(ctx context.Context, vcenterID string, moref string, tags []domain.TagRecord) error
	UpdateTagsState(ctx context.Context, vcenterID string, state string, lastSyncedAt *time.Time) error
}

// TagPollerConfig specifies configuration for the background REST tag synchronizer.
type TagPollerConfig struct {
	VCenterID    string
	PollInterval time.Duration // Default: 30s
	BatchSize    int           // Default: 500
	CallTimeout  time.Duration // Default: 15s
}

// DefaultTagPollerConfig returns production-safe default configurations.
func DefaultTagPollerConfig(vcenterID string) TagPollerConfig {
	return TagPollerConfig{
		VCenterID:    vcenterID,
		PollInterval: 30 * time.Second,
		BatchSize:    500,
		CallTimeout:  15 * time.Second,
	}
}

// TagPoller periodically polls the vSphere Automation REST API for tag assignments,
// synchronizing tags with replace-set semantics only for VMs whose tags have changed.
type TagPoller struct {
	fetcher    TagFetcher
	persister  TagPersister
	cfg        TagPollerConfig
	metrics    *telemetry.Metrics
	logger     *slog.Logger
	cleanupReg *CleanupRegistry

	mu             sync.RWMutex
	lastSuccessful *time.Time
	state          string // "ok", "degraded", "unavailable"
}

// NewTagPoller creates a new TagPoller instance.
func NewTagPoller(
	fetcher TagFetcher,
	persister TagPersister,
	cfg TagPollerConfig,
	metrics *telemetry.Metrics,
	logger *slog.Logger,
	cleanupReg *CleanupRegistry,
) *TagPoller {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 30 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 500
	}
	if cfg.CallTimeout <= 0 {
		cfg.CallTimeout = 15 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &TagPoller{
		fetcher:    fetcher,
		persister:  persister,
		cfg:        cfg,
		metrics:    metrics,
		logger:     logger.With("component", "tag_poller", "vcenter_id", cfg.VCenterID),
		cleanupReg: cleanupReg,
		state:      "unavailable",
	}
}

// State returns the current tag synchronization state ("ok", "degraded", "unavailable") and last sync time.
func (p *TagPoller) State() (string, *time.Time) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state, p.lastSuccessful
}

// SyncOnce executes a single tag synchronization cycle:
// 1. Fetches active VM morefs
// 2. Retrieves tags from REST API
// 3. Diff checks against existing database state
// 4. Updates database only for VMs whose tag assignments actually changed
// 5. On failure, preserves existing tags, marks tags degraded, and exposes staleness
func (p *TagPoller) SyncOnce(ctx context.Context) error {
	p.logger.Debug("starting tag synchronization cycle")

	// 1. Get active VM morefs from DB
	morefs, err := p.persister.ListActiveVMMorefs(ctx, p.cfg.VCenterID)
	if err != nil {
		p.logger.Error("failed to list active VMs for tag sync", "error", err)
		return fmt.Errorf("failed to list active VMs: %w", err)
	}

	if len(morefs) == 0 {
		p.mu.Lock()
		p.state = "ok"
		now := time.Now()
		p.lastSuccessful = &now
		p.mu.Unlock()
		p.updateTelemetry("ok", &now)
		_ = p.persister.UpdateTagsState(ctx, p.cfg.VCenterID, "ok", &now)
		return nil
	}

	// 2. Fetch tags for objects via REST API
	refs := make([]types.ManagedObjectReference, len(morefs))
	for i, m := range morefs {
		refs[i] = types.ManagedObjectReference{
			Type:  "VirtualMachine",
			Value: m,
		}
	}

	callCtx, cancel := context.WithTimeout(ctx, p.cfg.CallTimeout)
	fetchedTags, fetchErr := p.fetcher.FetchTagsForObjects(callCtx, refs)
	cancel()

	if fetchErr != nil {
		if p.metrics != nil {
			p.metrics.TagFetchFailures.WithLabelValues(p.cfg.VCenterID).Inc()
		}
		// Failure isolation: retain last known tags, mark degraded (S2.14 / ADR-016)
		p.mu.Lock()
		if p.lastSuccessful != nil {
			p.state = "degraded"
		} else {
			p.state = "unavailable"
		}
		currentState := p.state
		lastSync := p.lastSuccessful
		p.mu.Unlock()

		p.logger.Warn("tag service unavailable; retaining last known tags and marking degraded",
			"error", fetchErr,
			"tags_state", currentState,
		)
		p.updateTelemetry(currentState, lastSync)
		_ = p.persister.UpdateTagsState(ctx, p.cfg.VCenterID, currentState, lastSync)
		return fetchErr
	}

	// 3. Query existing tags in DB to perform diff check
	existingTags, err := p.persister.GetVMTags(ctx, p.cfg.VCenterID)
	if err != nil {
		p.logger.Error("failed to fetch existing tags from DB", "error", err)
		return fmt.Errorf("failed to fetch existing tags: %w", err)
	}

	// 4. Update ONLY VMs whose tag set actually changed
	changedVMs := 0
	for _, moref := range morefs {
		fetched := fetchedTags[moref]
		current := existingTags[moref]

		if !tagsMatch(fetched, current) {
			changedVMs++
			if updateErr := p.persister.UpdateVMTags(ctx, p.cfg.VCenterID, moref, fetched); updateErr != nil {
				p.logger.Error("failed to update tags for VM", "moref", moref, "error", updateErr)
				continue
			}
			if p.metrics != nil {
				p.metrics.TagsPersisted.WithLabelValues(p.cfg.VCenterID).Add(float64(len(fetched)))
			}
		}
	}

	now := time.Now()
	p.mu.Lock()
	p.state = "ok"
	p.lastSuccessful = &now
	p.mu.Unlock()

	p.logger.Info("tag synchronization cycle completed",
		"active_vms", len(morefs),
		"vms_updated", changedVMs,
		"tags_state", "ok",
	)
	p.updateTelemetry("ok", &now)
	_ = p.persister.UpdateTagsState(ctx, p.cfg.VCenterID, "ok", &now)

	return nil
}

// Run starts the background tag synchronization loop until ctx is canceled.
func (p *TagPoller) Run(ctx context.Context) error {
	pollCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if p.cleanupReg != nil {
		p.cleanupReg.RegisterProducer("tag-poller", cancel)
		p.cleanupReg.RegisterREST(p.fetcher.Close)
	}

	// Initial sync run
	if err := p.SyncOnce(pollCtx); err != nil && !errors.Is(err, ErrTaggingUnavailable) {
		p.logger.Warn("initial tag synchronization encountered error", "error", err)
	}

	ticker := time.NewTicker(p.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-pollCtx.Done():
			p.logger.Info("tag poller stopped: context canceled")
			return pollCtx.Err()
		case <-ticker.C:
			if err := p.SyncOnce(pollCtx); err != nil && !errors.Is(err, ErrTaggingUnavailable) {
				p.logger.Warn("tag synchronization cycle encountered error", "error", err)
			}
		}
	}
}

func (p *TagPoller) updateTelemetry(state string, lastSync *time.Time) {
	if p.metrics == nil {
		return
	}
	states := []string{"ok", "degraded", "unavailable"}
	for _, s := range states {
		val := 0.0
		if s == state {
			val = 1.0
		}
		p.metrics.TagsState.WithLabelValues(p.cfg.VCenterID, s).Set(val)
	}
	if lastSync != nil {
		p.metrics.TagsLastSyncedTimestamp.WithLabelValues(p.cfg.VCenterID).Set(float64(lastSync.Unix()))
	}
}

// tagsMatch checks if two sets of domain.TagRecord represent identical tag assignments (order-independent).
func tagsMatch(a, b []domain.TagRecord) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}

	sigA := make([]string, len(a))
	for i, t := range a {
		sigA[i] = t.CategoryName + ":" + t.TagName
	}
	sigB := make([]string, len(b))
	for i, t := range b {
		sigB[i] = t.CategoryName + ":" + t.TagName
	}

	sort.Strings(sigA)
	sort.Strings(sigB)

	for i := range sigA {
		if sigA[i] != sigB[i] {
			return false
		}
	}
	return true
}
