package collector

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/infrastructure/sims/generated/db"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	if testing.Short() {
		t.Skip("skipping db-dependent test in short mode")
	}
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5433"
	}
	connStr := fmt.Sprintf("postgres://sims:sims_password@%s:%s/sims?sslmode=disable", host, port)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("cannot connect to postgres: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("postgres ping failed: %v", err)
	}
	return pool
}

func createTestVCenterAndTarget(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, uuid.UUID) {
	return createTestVCenterAndTargetWithURL(t, pool, "")
}

func createTestVCenterAndTargetWithURL(t *testing.T, pool *pgxpool.Pool, customURL string) (uuid.UUID, uuid.UUID) {
	ctx := context.Background()
	queries := db.New(pool)

	vcID := uuid.New()
	targetID := uuid.New()

	apiURL := customURL
	if apiURL == "" {
		apiURL = fmt.Sprintf("https://vc-%s.local/sdk", vcID.String()[:8])
	}

	createdVC, err := queries.CreateVCenter(ctx, db.CreateVCenterParams{
		Name:         fmt.Sprintf("vcenter-test-%s", vcID.String()[:8]),
		ApiUrl:       apiURL,
		Version:      "8.0.3",
		Build:        "24000000",
		Status:       "AVAILABLE",
		Priority:     100,
		Enabled:      true,
		Capabilities: []byte("{}"),
		TlsSettings:  []byte("{}"),
	})
	if err != nil {
		t.Fatalf("failed to create test vcenter: %v", err)
	}

	// Insert collector target
	var insertedTargetID pgtype.UUID
	err = pool.QueryRow(ctx, `
		INSERT INTO collector_targets (id, vcenter_id, enabled, priority)
		VALUES ($1, $2, true, 100)
		RETURNING id
	`, pgtype.UUID{Bytes: targetID, Valid: true}, createdVC.ID).Scan(&insertedTargetID)
	if err != nil {
		t.Fatalf("failed to create test target: %v", err)
	}

	parsedVCID, _ := uuid.FromBytes(createdVC.ID.Bytes[:])
	parsedTargetID, _ := uuid.FromBytes(insertedTargetID.Bytes[:])
	return parsedVCID, parsedTargetID
}

func cleanupTestTarget(pool *pgxpool.Pool, targetID uuid.UUID) {
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `
		DELETE FROM vcenters WHERE id IN (SELECT vcenter_id FROM collector_targets WHERE id = $1)
	`, pgtype.UUID{Bytes: targetID, Valid: true})
}

func TestLeaseCoordinator_AcquireAndRelease(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	_, targetID := createTestVCenterAndTarget(t, pool)
	defer cleanupTestTarget(pool, targetID)

	ctx := context.Background()
	queries := db.New(pool)
	coord := NewLeaseCoordinator(pool, queries, LeaseConfig{
		WorkerID:          "worker-1",
		LeaseTTL:          5 * time.Second,
		HeartbeatInterval: 1 * time.Second,
	}, nil)

	// 1. Acquire lease
	al, err := coord.Acquire(ctx, targetID)
	if err != nil {
		t.Fatalf("expected acquire success, got %v", err)
	}
	if al.HolderID != "worker-1" {
		t.Fatalf("expected holder worker-1, got %s", al.HolderID)
	}
	if al.Version != 1 {
		t.Fatalf("expected version 1, got %d", al.Version)
	}

	// 2. Active lease check
	active, ok := coord.GetActiveLease(targetID)
	if !ok || active == nil {
		t.Fatalf("expected GetActiveLease to return lease")
	}

	// 3. Graceful release
	if err := coord.Release(ctx, targetID); err != nil {
		t.Fatalf("failed to release lease: %v", err)
	}

	if _, ok := coord.GetActiveLease(targetID); ok {
		t.Fatalf("lease should not be active after release")
	}
}

func TestLeaseCoordinator_ContentionAndTakeover(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	_, targetID := createTestVCenterAndTarget(t, pool)
	defer cleanupTestTarget(pool, targetID)

	ctx := context.Background()
	queries := db.New(pool)

	coord1 := NewLeaseCoordinator(pool, queries, LeaseConfig{
		WorkerID:          "worker-1",
		LeaseTTL:          2 * time.Second,
		HeartbeatInterval: 10 * time.Second, // Long heartbeat so it expires for takeover test
	}, nil)

	coord2 := NewLeaseCoordinator(pool, queries, LeaseConfig{
		WorkerID:          "worker-2",
		LeaseTTL:          2 * time.Second,
		HeartbeatInterval: 500 * time.Millisecond,
	}, nil)

	// Worker 1 acquires
	al1, err := coord1.Acquire(ctx, targetID)
	if err != nil {
		t.Fatalf("worker-1 failed to acquire: %v", err)
	}
	if al1.Version != 1 {
		t.Fatalf("expected version 1, got %d", al1.Version)
	}

	// Worker 2 attempts to acquire while active -> must fail with ErrLeaseHeld
	_, err = coord2.Acquire(ctx, targetID)
	if err != ErrLeaseHeld {
		t.Fatalf("expected ErrLeaseHeld for worker-2, got %v", err)
	}

	// Release by worker 1
	if err := coord1.Release(ctx, targetID); err != nil {
		t.Fatalf("worker-1 release failed: %v", err)
	}

	// Worker 2 should now acquire immediately
	al2, err := coord2.Acquire(ctx, targetID)
	if err != nil {
		t.Fatalf("worker-2 failed to acquire released lease: %v", err)
	}
	if al2.HolderID != "worker-2" {
		t.Fatalf("expected holder worker-2, got %s", al2.HolderID)
	}
	if al2.Version != 2 {
		t.Fatalf("expected version increment to 2, got %d", al2.Version)
	}

	_ = coord2.Release(ctx, targetID)
}

func TestLeaseCoordinator_ConcurrencyRace(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	_, targetID := createTestVCenterAndTarget(t, pool)
	defer cleanupTestTarget(pool, targetID)

	ctx := context.Background()
	queries := db.New(pool)

	numWorkers := 10
	var wg sync.WaitGroup
	winners := make([]string, 0)
	var winMu sync.Mutex
	var winnerCoord *LeaseCoordinator

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		workerID := fmt.Sprintf("race-worker-%d", i)
		coord := NewLeaseCoordinator(pool, queries, LeaseConfig{
			WorkerID:          workerID,
			LeaseTTL:          5 * time.Second,
			HeartbeatInterval: 1 * time.Second,
		}, nil)

		go func(c *LeaseCoordinator, wid string) {
			defer wg.Done()
			al, err := c.Acquire(ctx, targetID)
			if err == nil && al != nil {
				winMu.Lock()
				winners = append(winners, wid)
				winnerCoord = c
				winMu.Unlock()
			}
		}(coord, workerID)
	}

	wg.Wait()

	if len(winners) != 1 {
		t.Fatalf("expected strictly 1 race winner, got %d (%v)", len(winners), winners)
	}

	if winnerCoord != nil {
		_ = winnerCoord.Release(ctx, targetID)
	}
}
