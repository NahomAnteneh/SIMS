package collector

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/infrastructure/sims/generated/db"
	"github.com/infrastructure/sims/internal/domain"
)

type dummyPersister struct{}

func (dp *dummyPersister) PersistTopology(ctx context.Context, snap *TopologySnapshot) error {
	return nil
}

func (dp *dummyPersister) PersistVMBatch(ctx context.Context, batch []domain.VMRecord) (int, error) {
	return len(batch), nil
}

func TestFleetManager_LifecycleAndDiscovery(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, targetID := createTestVCenterAndTarget(t, pool)
	defer cleanupTestTarget(pool, targetID)

	queries := db.New(pool)
	workerID := "fleet-test-worker-1"

	leaseCoord := NewLeaseCoordinator(pool, queries, LeaseConfig{
		WorkerID:          workerID,
		LeaseTTL:          5 * time.Second,
		HeartbeatInterval: 1 * time.Second,
	}, nil)

	targetFSM := NewTargetFSM(queries, nil)
	limiter := NewConcurrencyRegistry(2, 5, 2)

	fleet := NewFleetManager(
		pool,
		queries,
		leaseCoord,
		targetFSM,
		nil, // no listener needed for direct eval test
		limiter,
		&dummyPersister{},
		FleetConfig{
			WorkerID:           workerID,
			TargetEvalInterval: 100 * time.Millisecond,
		},
		nil,
		nil,
	)

	// Verify available targets before start
	available, err := leaseCoord.ListAvailableTargets(ctx)
	if err != nil {
		t.Fatalf("failed to list available targets: %v", err)
	}
	found := false
	for _, a := range available {
		parsed, _ := uuid.FromBytes(a.TargetID.Bytes[:])
		if parsed == targetID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("target %s should be in available targets", targetID)
	}

	// Start fleet
	if err := fleet.Start(ctx); err != nil {
		t.Fatalf("failed to start fleet manager: %v", err)
	}

	// Wait for fleet to acquire lease and spawn target
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fleet.ActiveTargetCount() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if fleet.ActiveTargetCount() != 1 {
		t.Fatalf("expected 1 active target, got %d", fleet.ActiveTargetCount())
	}

	// Verify lease is held by this worker in DB
	pgTID := pgtype.UUID{Bytes: targetID, Valid: true}
	leaseRow, err := queries.GetActiveLeaseByTargetID(ctx, pgTID)
	if err != nil {
		t.Fatalf("expected active lease in DB: %v", err)
	}
	if leaseRow.HolderID != workerID {
		t.Fatalf("expected holder %s, got %s", workerID, leaseRow.HolderID)
	}

	// Stop fleet
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer stopCancel()
	if err := fleet.Stop(stopCtx); err != nil {
		t.Fatalf("failed to stop fleet: %v", err)
	}

	if fleet.ActiveTargetCount() != 0 {
		t.Fatalf("expected 0 active targets after stop, got %d", fleet.ActiveTargetCount())
	}
}
