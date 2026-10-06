package collector

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/infrastructure/sims/generated/db"
)

type mockVCScanner struct {
	refs []VMRef
}

func (m *mockVCScanner) ListVMReferences(ctx context.Context) ([]VMRef, error) {
	return m.refs, nil
}

func TestDifferentialReconciler_SetMath(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	vcID, targetID := createTestVCenterAndTarget(t, pool)
	defer cleanupTestTarget(pool, targetID)

	queries := db.New(pool)
	pgVCID := pgtype.UUID{Bytes: vcID, Valid: true}

	// Seed 2 active VMs in DB: vm-1 and vm-2
	moref1 := "vm-101"
	moref2 := "vm-102"
	uuid1 := uuid.NewString()
	uuid2 := uuid.NewString()

	_, err := queries.UpsertVM(ctx, db.UpsertVMParams{
		VcenterID:    pgVCID,
		Moref:        moref1,
		InstanceUuid: uuid1,
		Name:         "test-vm-1",
		PowerState:   "poweredOn",
	})
	if err != nil {
		t.Fatalf("failed to insert vm-1: %v", err)
	}

	_, err = queries.UpsertVM(ctx, db.UpsertVMParams{
		VcenterID:    pgVCID,
		Moref:        moref2,
		InstanceUuid: uuid2,
		Name:         "test-vm-2",
		PowerState:   "poweredOn",
	})
	if err != nil {
		t.Fatalf("failed to insert vm-2: %v", err)
	}

	// vCenter state:
	// - vm-1 exists in both (present in both -> touch)
	// - vm-2 does NOT exist in vCenter (missing in vCenter -> tombstone)
	// - vm-3 exists ONLY in vCenter (missing in DB -> enqueue hydration)
	moref3 := "vm-103"
	scanner := &mockVCScanner{
		refs: []VMRef{
			{MoRef: moref1},
			{MoRef: moref3},
		},
	}

	var hydratedMoRefs []string
	reconciler := NewDifferentialReconciler(pool, queries, func(ctx context.Context, vcenterID uuid.UUID, missingMoRefs []string) error {
		hydratedMoRefs = append(hydratedMoRefs, missingMoRefs...)
		return nil
	}, nil)

	stats, err := reconciler.Reconcile(ctx, vcID, scanner)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	if stats.CheckedDB != 2 {
		t.Errorf("expected 2 DB VMs checked, got %d", stats.CheckedDB)
	}
	if stats.CheckedVCenter != 2 {
		t.Errorf("expected 2 vCenter VMs checked, got %d", stats.CheckedVCenter)
	}
	if stats.MissingInVCenter != 1 {
		t.Errorf("expected 1 missing in vCenter, got %d", stats.MissingInVCenter)
	}
	if stats.MissingInDB != 1 {
		t.Errorf("expected 1 missing in DB, got %d", stats.MissingInDB)
	}
	if stats.PresentInBoth != 1 {
		t.Errorf("expected 1 present in both, got %d", stats.PresentInBoth)
	}

	// Verify vm-2 is tombstoned (deleted_at IS NOT NULL)
	var deletedAt pgtype.Timestamptz
	err = pool.QueryRow(ctx, "SELECT deleted_at FROM virtual_machines WHERE vcenter_id = $1 AND moref = $2", pgVCID, moref2).Scan(&deletedAt)
	if err != nil {
		t.Fatalf("failed to query vm-2: %v", err)
	}
	if !deletedAt.Valid {
		t.Errorf("expected vm-2 to be tombstoned with non-null deleted_at")
	}

	// Verify vm-1 remains active (deleted_at IS NULL)
	err = pool.QueryRow(ctx, "SELECT deleted_at FROM virtual_machines WHERE vcenter_id = $1 AND moref = $2", pgVCID, moref1).Scan(&deletedAt)
	if err != nil {
		t.Fatalf("failed to query vm-1: %v", err)
	}
	if deletedAt.Valid {
		t.Errorf("expected vm-1 to remain active with null deleted_at")
	}

	// Verify vm-3 was passed to hydration callback
	if len(hydratedMoRefs) != 1 || hydratedMoRefs[0] != moref3 {
		t.Errorf("expected vm-3 enqueued for hydration, got %v", hydratedMoRefs)
	}
}
