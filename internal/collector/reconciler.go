package collector

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/infrastructure/sims/generated/db"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// VMRef represents a lightweight virtual machine identifier from vCenter (S4.6)
type VMRef struct {
	MoRef        string
	InstanceUUID string
}

// VCReferenceScanner defines interface to scan lightweight VM identifiers from vCenter
type VCReferenceScanner interface {
	ListVMReferences(ctx context.Context) ([]VMRef, error)
}

// GovmomiReferenceScanner scans VM references using ContainerView
type GovmomiReferenceScanner struct {
	client *vim25.Client
}

func NewGovmomiReferenceScanner(client *vim25.Client) *GovmomiReferenceScanner {
	return &GovmomiReferenceScanner{client: client}
}

func (s *GovmomiReferenceScanner) ListVMReferences(ctx context.Context) ([]VMRef, error) {
	if s.client == nil {
		return nil, fmt.Errorf("vim25 client is nil")
	}
	mgr := view.NewManager(s.client)
	v, err := mgr.CreateContainerView(ctx, s.client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create container view: %w", err)
	}
	defer func() { _ = v.Destroy(ctx) }()

	refs, err := v.Find(ctx, []string{"VirtualMachine"}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to find virtual machines: %w", err)
	}

	result := make([]VMRef, len(refs))
	for i, ref := range refs {
		result[i] = VMRef{
			MoRef: ref.Value,
		}
	}
	return result, nil
}

// ReconciliationStats records differential set math metrics (S4.6)
type ReconciliationStats struct {
	CheckedDB        int           `json:"checked_db"`
	CheckedVCenter   int           `json:"checked_vcenter"`
	MissingInVCenter int           `json:"missing_in_vcenter"`
	MissingInDB      int           `json:"missing_in_db"`
	PresentInBoth    int           `json:"present_in_both"`
	Duration         time.Duration `json:"duration"`
}

// HydrationCallback is called when VMs missing in DB are detected ($S_{vc} \setminus S_{db}$)
type HydrationCallback func(ctx context.Context, vcenterID uuid.UUID, missingMoRefs []string) error

// DifferentialReconciler implements the differential set reconciliation engine (S4.6)
type DifferentialReconciler struct {
	pool      *pgxpool.Pool
	queries   *db.Queries
	onHydrate HydrationCallback
	logger    *slog.Logger
}

func NewDifferentialReconciler(
	pool *pgxpool.Pool,
	queries *db.Queries,
	onHydrate HydrationCallback,
	logger *slog.Logger,
) *DifferentialReconciler {
	if logger == nil {
		logger = slog.Default()
	}
	return &DifferentialReconciler{
		pool:      pool,
		queries:   queries,
		onHydrate: onHydrate,
		logger:    logger.With(slog.String("component", "differential-reconciler")),
	}
}

// Reconcile performs set math between DB and vCenter identifier sets (S4.6)
//
// Set math:
//   - S_db \ S_vc: Missing in vCenter -> Tombstone (deleted_at = NOW())
//   - S_vc \ S_db: Missing in DB -> Enqueue for immediate hydration
//   - S_db ∩ S_vc: Present in both -> Touch updated_at
func (r *DifferentialReconciler) Reconcile(
	ctx context.Context,
	vcenterID uuid.UUID,
	scanner VCReferenceScanner,
) (*ReconciliationStats, error) {
	ctx, span := telemetry.StartSpan(ctx, "reconciliation.differential_sweep",
		trace.WithAttributes(
			attribute.String("vcenter.id", vcenterID.String()),
		),
	)
	defer span.End()

	start := time.Now()
	pgVCID := pgtype.UUID{Bytes: vcenterID, Valid: true}

	// 1. Fetch S_db: Active VMs in DB
	dbVMs, err := r.queries.ListActiveVMIdentifiers(ctx, pgVCID)
	if err != nil {
		telemetry.RecordSafeError(span, err)
		return nil, fmt.Errorf("failed to fetch active VMs from DB: %w", err)
	}

	dbByMoRef := make(map[string]pgtype.UUID, len(dbVMs))
	for _, vm := range dbVMs {
		dbByMoRef[vm.Moref] = vm.ID
	}

	// 2. Fetch S_vc: Active VMs in vCenter
	vcRefs, err := scanner.ListVMReferences(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to scan VM references from vCenter: %w", err)
	}

	vcByMoRef := make(map[string]bool, len(vcRefs))
	for _, ref := range vcRefs {
		vcByMoRef[ref.MoRef] = true
	}

	// 3. Set Math:
	// A) Missing in vCenter: S_db \ S_vc
	var tombstoneIDs []pgtype.UUID
	var presentIDs []pgtype.UUID

	for moref, id := range dbByMoRef {
		if !vcByMoRef[moref] {
			tombstoneIDs = append(tombstoneIDs, id)
		} else {
			presentIDs = append(presentIDs, id)
		}
	}

	// B) Missing in DB: S_vc \ S_db
	var missingInDBMoRefs []string
	for _, ref := range vcRefs {
		if _, exists := dbByMoRef[ref.MoRef]; !exists {
			missingInDBMoRefs = append(missingInDBMoRefs, ref.MoRef)
		}
	}

	// 4. Execute Actions:
	// A) Tombstone missing VMs
	if len(tombstoneIDs) > 0 {
		if err := r.queries.TombstoneVMsByIDs(ctx, tombstoneIDs); err != nil {
			r.logger.Error("failed to tombstone missing VMs", slog.String("error", err.Error()))
			return nil, fmt.Errorf("tombstone failed: %w", err)
		}
	}

	// B) Touch present VMs
	if len(presentIDs) > 0 {
		if err := r.queries.TouchVMsLastSeen(ctx, presentIDs); err != nil {
			r.logger.Warn("failed to touch last seen for present VMs", slog.String("error", err.Error()))
		}
	}

	// C) Enqueue missing VMs for hydration
	if len(missingInDBMoRefs) > 0 && r.onHydrate != nil {
		if err := r.onHydrate(ctx, vcenterID, missingInDBMoRefs); err != nil {
			r.logger.Warn("failed to enqueue hydration for missing VMs", slog.String("error", err.Error()))
		}
	}

	stats := &ReconciliationStats{
		CheckedDB:        len(dbVMs),
		CheckedVCenter:   len(vcRefs),
		MissingInVCenter: len(tombstoneIDs),
		MissingInDB:      len(missingInDBMoRefs),
		PresentInBoth:    len(presentIDs),
		Duration:         time.Since(start),
	}

	r.logger.Info("differential reconciliation complete",
		slog.String("vcenter_id", vcenterID.String()),
		slog.Int("checked_db", stats.CheckedDB),
		slog.Int("checked_vc", stats.CheckedVCenter),
		slog.Int("missing_in_vc_tombstoned", stats.MissingInVCenter),
		slog.Int("missing_in_db_enqueued", stats.MissingInDB),
		slog.Int("present_in_both", stats.PresentInBoth),
		slog.Duration("duration", stats.Duration),
	)

	span.SetAttributes(
		attribute.Int("reconcile.checked_db", stats.CheckedDB),
		attribute.Int("reconcile.checked_vcenter", stats.CheckedVCenter),
		attribute.Int("reconcile.missing_in_vcenter", stats.MissingInVCenter),
		attribute.Int("reconcile.missing_in_db", stats.MissingInDB),
		attribute.Int("reconcile.present_in_both", stats.PresentInBoth),
	)

	return stats, nil
}
