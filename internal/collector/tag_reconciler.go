package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/vmware/govmomi/vim25/types"
)

var (
	// ErrTagFetchFailed indicates that tag retrieval failed or returned incomplete data.
	ErrTagFetchFailed = errors.New("tag fetch failed")
)

// TagReconcilerConfig defines parameters for scheduled and post-recovery tag reconciliation.
type TagReconcilerConfig struct {
	VCenterID           string
	ScheduleInterval    time.Duration // Frequency for baseline full sweep (e.g. 6h or 24h)
	BatchSize           int           // Bounded batch size per REST call (default 250)
	CallTimeout         time.Duration // Timeout per batch fetch (default 30s)
	DriftAlertThreshold float64       // Drift ratio threshold (e.g. 0.20 for 20%) that triggers an alert
}

// DefaultTagReconcilerConfig returns production-safe default configurations.
func DefaultTagReconcilerConfig(vcenterID string) TagReconcilerConfig {
	return TagReconcilerConfig{
		VCenterID:           vcenterID,
		ScheduleInterval:    6 * time.Hour,
		BatchSize:           250,
		CallTimeout:         30 * time.Second,
		DriftAlertThreshold: 0.20,
	}
}

// TagReconciliationReport contains detailed audit metrics from a reconciliation sweep.
type TagReconciliationReport struct {
	VCenterID     string        `json:"vcenter_id"`
	VMsChecked    int           `json:"vms_checked"`
	VMsDrifted    int           `json:"vms_drifted"`
	TagsAdded     int           `json:"tags_added"`
	TagsRemoved   int           `json:"tags_removed"`
	TagsUnchanged int           `json:"tags_unchanged"`
	DriftRatio    float64       `json:"drift_ratio"`
	AlertRaised   bool          `json:"alert_raised"`
	Duration      time.Duration `json:"duration_ms"`
	RestCalls     int           `json:"rest_calls"`
}

// TagReconciler coordinates scheduled and on-demand full tag reconciliation sweeps
// to detect and resolve tag drift between vCenter and SIMS.
//
// Invariants (S4.12):
// 1. Non-destructive rule: A failed tag fetch NEVER deletes tags.
// 2. Only a successful, complete fetch may remove a stale tag row.
// 3. Drift above the threshold triggers an operational alert.
type TagReconciler struct {
	fetcher   TagFetcher
	persister TagPersister
	cfg       TagReconcilerConfig
	metrics   *telemetry.Metrics
	logger    *slog.Logger

	mu          sync.Mutex
	triggerChan chan struct{}
}

// NewTagReconciler constructs a new TagReconciler.
func NewTagReconciler(
	fetcher TagFetcher,
	persister TagPersister,
	cfg TagReconcilerConfig,
	metrics *telemetry.Metrics,
	logger *slog.Logger,
) *TagReconciler {
	if cfg.ScheduleInterval <= 0 {
		cfg.ScheduleInterval = 6 * time.Hour
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 250
	}
	if cfg.CallTimeout <= 0 {
		cfg.CallTimeout = 30 * time.Second
	}
	if cfg.DriftAlertThreshold <= 0 {
		cfg.DriftAlertThreshold = 0.20
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &TagReconciler{
		fetcher:     fetcher,
		persister:   persister,
		cfg:         cfg,
		metrics:     metrics,
		logger:      logger.With("component", "tag_reconciler", "vcenter_id", cfg.VCenterID),
		triggerChan: make(chan struct{}, 1),
	}
}

// Trigger notifies the reconciler to execute an immediate reconciliation sweep
// (e.g. after tag REST service recovers or on manual trigger).
func (r *TagReconciler) Trigger() {
	select {
	case r.triggerChan <- struct{}{}:
	default:
	}
}

// Reconcile executes a full tag reconciliation sweep across all active VMs for this vCenter.
func (r *TagReconciler) Reconcile(ctx context.Context) (*TagReconciliationReport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	start := time.Now()
	report := &TagReconciliationReport{
		VCenterID: r.cfg.VCenterID,
	}

	// 1. Fetch active VM morefs from DB
	morefs, err := r.persister.ListActiveVMMorefs(ctx, r.cfg.VCenterID)
	if err != nil {
		report.Duration = time.Since(start)
		return report, fmt.Errorf("failed to list active VMs: %w", err)
	}

	report.VMsChecked = len(morefs)
	if len(morefs) == 0 {
		report.Duration = time.Since(start)
		now := time.Now()
		_ = r.persister.UpdateTagsState(ctx, r.cfg.VCenterID, "ok", &now)
		return report, nil
	}

	// 2. Fetch tags in bounded batches respecting the REST budget
	allFetchedTags := make(map[string][]domain.TagRecord)

	for i := 0; i < len(morefs); i += r.cfg.BatchSize {
		end := i + r.cfg.BatchSize
		if end > len(morefs) {
			end = len(morefs)
		}
		chunk := morefs[i:end]

		refs := make([]types.ManagedObjectReference, len(chunk))
		for j, m := range chunk {
			refs[j] = types.ManagedObjectReference{
				Type:  "VirtualMachine",
				Value: m,
			}
		}

		batchCtx, cancel := context.WithTimeout(ctx, r.cfg.CallTimeout)
		batchTags, batchErr := r.fetcher.FetchTagsForObjects(batchCtx, refs)
		cancel()
		report.RestCalls++

		if batchErr != nil {
			report.Duration = time.Since(start)
			r.logger.Warn("tag reconciliation fetch failed; preserving all existing tags without modification",
				slog.String("error", batchErr.Error()),
				slog.Int("processed_before_error", i),
			)
			if r.metrics != nil {
				r.metrics.TagFetchFailures.WithLabelValues(r.cfg.VCenterID).Inc()
			}
			// INVARIANT: Never delete tags on fetch failure.
			return report, fmt.Errorf("%w: %v", ErrTagFetchFailed, batchErr)
		}

		for k, v := range batchTags {
			allFetchedTags[k] = v
		}
	}

	// 3. Query existing tags from DB
	existingTags, err := r.persister.GetVMTags(ctx, r.cfg.VCenterID)
	if err != nil {
		report.Duration = time.Since(start)
		return report, fmt.Errorf("failed to get existing tags: %w", err)
	}

	// 4. Compute drift and apply convergence
	for _, moref := range morefs {
		fetched := allFetchedTags[moref]
		existing := existingTags[moref]

		added, removed, unchanged := calculateTagDiff(fetched, existing)
		report.TagsAdded += added
		report.TagsRemoved += removed
		report.TagsUnchanged += unchanged

		if added > 0 || removed > 0 {
			report.VMsDrifted++
			if updateErr := r.persister.UpdateVMTags(ctx, r.cfg.VCenterID, moref, fetched); updateErr != nil {
				r.logger.Error("failed to converge tags for VM",
					slog.String("moref", moref),
					slog.String("error", updateErr.Error()),
				)
				continue
			}
			if r.metrics != nil {
				r.metrics.TagsPersisted.WithLabelValues(r.cfg.VCenterID).Add(float64(len(fetched)))
			}
		}
	}

	totalTagsProcessed := report.TagsAdded + report.TagsRemoved + report.TagsUnchanged
	if totalTagsProcessed > 0 {
		report.DriftRatio = float64(report.TagsAdded+report.TagsRemoved) / float64(totalTagsProcessed)
	}

	if report.DriftRatio > r.cfg.DriftAlertThreshold {
		report.AlertRaised = true
		r.logger.Error("tag drift exceeded alert threshold",
			slog.Float64("drift_ratio", report.DriftRatio),
			slog.Float64("threshold", r.cfg.DriftAlertThreshold),
			slog.Int("vms_drifted", report.VMsDrifted),
			slog.Int("tags_added", report.TagsAdded),
			slog.Int("tags_removed", report.TagsRemoved),
		)
	}

	report.Duration = time.Since(start)
	now := time.Now()
	_ = r.persister.UpdateTagsState(ctx, r.cfg.VCenterID, "ok", &now)

	r.logger.Info("tag reconciliation completed",
		slog.Int("vms_checked", report.VMsChecked),
		slog.Int("vms_drifted", report.VMsDrifted),
		slog.Int("tags_added", report.TagsAdded),
		slog.Int("tags_removed", report.TagsRemoved),
		slog.Float64("drift_ratio", report.DriftRatio),
		slog.Bool("alert_raised", report.AlertRaised),
		slog.Duration("duration", report.Duration),
		slog.Int("rest_calls", report.RestCalls),
	)

	return report, nil
}

// Run starts the scheduled background reconciliation loop.
func (r *TagReconciler) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.cfg.ScheduleInterval)
	defer ticker.Stop()

	// Initial reconciliation on boot
	if _, err := r.Reconcile(ctx); err != nil && !errors.Is(err, ErrTaggingUnavailable) {
		r.logger.Warn("initial tag reconciliation encountered error", slog.String("error", err.Error()))
	}

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("tag reconciler shutting down")
			return ctx.Err()
		case <-r.triggerChan:
			r.logger.Info("tag reconciliation triggered out-of-band")
			if _, err := r.Reconcile(ctx); err != nil && !errors.Is(err, ErrTaggingUnavailable) {
				r.logger.Warn("triggered tag reconciliation encountered error", slog.String("error", err.Error()))
			}
		case <-ticker.C:
			r.logger.Info("scheduled tag reconciliation sweep started")
			if _, err := r.Reconcile(ctx); err != nil && !errors.Is(err, ErrTaggingUnavailable) {
				r.logger.Warn("scheduled tag reconciliation encountered error", slog.String("error", err.Error()))
			}
		}
	}
}

// calculateTagDiff compares fetched vs existing tags using (category_name, tag_name) as identity.
func calculateTagDiff(fetched, existing []domain.TagRecord) (added, removed, unchanged int) {
	existingKeys := make(map[string]struct{}, len(existing))
	for _, t := range existing {
		existingKeys[t.CategoryName+":"+t.TagName] = struct{}{}
	}

	fetchedKeys := make(map[string]struct{}, len(fetched))
	for _, t := range fetched {
		key := t.CategoryName + ":" + t.TagName
		fetchedKeys[key] = struct{}{}
		if _, ok := existingKeys[key]; ok {
			unchanged++
		} else {
			added++
		}
	}

	for _, t := range existing {
		key := t.CategoryName + ":" + t.TagName
		if _, ok := fetchedKeys[key]; !ok {
			removed++
		}
	}

	return added, removed, unchanged
}
