package persistence

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/generated/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	if testing.Short() {
		t.Skip("skipping db test in short mode")
	}
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5433"
	}
	connStr := fmt.Sprintf("postgres://postgres:postgres@%s:%s/sims?sslmode=disable", host, port)

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

func TestTombstonePurger_BoundedBatchPurging(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	queries := db.New(pool)

	// Create test vcenter
	vcUUID := uuid.New()
	createdVC, err := queries.CreateVCenter(ctx, db.CreateVCenterParams{
		Name:         fmt.Sprintf("vc-tombstone-%s", vcUUID.String()[:8]),
		ApiUrl:       fmt.Sprintf("https://vc-%s.local/sdk", vcUUID.String()[:8]),
		Version:      "8.0.3",
		Build:        "24000000",
		Status:       "AVAILABLE",
		Priority:     100,
		Enabled:      true,
		Capabilities: []byte("{}"),
		TlsSettings:  []byte("{}"),
	})
	if err != nil {
		t.Fatalf("failed to create vcenter: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM collector_targets WHERE vcenter_id = $1", createdVC.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM virtual_machines WHERE vcenter_id = $1", createdVC.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM vcenters WHERE id = $1", createdVC.ID)
	}()

	// Insert 10 VMs:
	// - 6 VMs soft-deleted 40 days ago (older than 30-day retention)
	// - 2 VMs soft-deleted 5 days ago (within 30-day retention)
	// - 2 VMs active (deleted_at IS NULL)
	now := time.Now()
	cutoff40DaysAgo := now.Add(-40 * 24 * time.Hour)
	cutoff5DaysAgo := now.Add(-5 * 24 * time.Hour)

	for i := 0; i < 10; i++ {
		moref := fmt.Sprintf("vm-purge-%d", i)
		instUUID := uuid.NewString()

		vm, err := queries.UpsertVM(ctx, db.UpsertVMParams{
			VcenterID:    createdVC.ID,
			Moref:        moref,
			InstanceUuid: instUUID,
			Name:         fmt.Sprintf("vm-%d", i),
			PowerState:   "poweredOff",
		})
		if err != nil {
			t.Fatalf("failed to insert test vm %d: %v", i, err)
		}

		if i < 6 {
			// Older than 30 days
			_, err = pool.Exec(ctx, "UPDATE virtual_machines SET deleted_at = $1 WHERE id = $2", cutoff40DaysAgo, vm.ID)
			if err != nil {
				t.Fatalf("failed to set deleted_at: %v", err)
			}
		} else if i < 8 {
			// Recent (5 days ago)
			_, err = pool.Exec(ctx, "UPDATE virtual_machines SET deleted_at = $1 WHERE id = $2", cutoff5DaysAgo, vm.ID)
			if err != nil {
				t.Fatalf("failed to set deleted_at: %v", err)
			}
		}
		// i >= 8 remain active (deleted_at IS NULL)
	}

	purger := NewTombstonePurger(pool, queries, nil)

	// Execute bounded purging: batch size 2, retention 30 days
	opts := PurgeOptions{
		BatchSize:           2,
		MaxBatches:          10,
		RetentionPeriod:     30 * 24 * time.Hour,
		SleepBetweenBatches: 5 * time.Millisecond,
	}

	report, err := purger.PurgeTombstones(ctx, opts)
	if err != nil {
		t.Fatalf("PurgeTombstones failed: %v", err)
	}

	if report.TotalRowsPurged != 6 {
		t.Fatalf("expected exactly 6 rows purged, got %d", report.TotalRowsPurged)
	}
	if report.BatchesRun < 3 {
		t.Fatalf("expected at least 3 batches for batch size 2, got %d", report.BatchesRun)
	}
	if report.LockDuration <= 0 {
		t.Fatalf("expected non-zero lock duration recorded")
	}

	// Verify remaining count in DB for this vCenter
	var remainingCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM virtual_machines WHERE vcenter_id = $1", createdVC.ID).Scan(&remainingCount)
	if err != nil {
		t.Fatalf("failed to count remaining VMs: %v", err)
	}

	if remainingCount != 4 {
		t.Fatalf("expected 4 remaining VMs (2 recent tombstones + 2 active), got %d", remainingCount)
	}
}
