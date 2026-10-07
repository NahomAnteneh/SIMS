package benchmark

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func getTestDBPool(t *testing.T) *pgxpool.Pool {
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5433"
	}
	dbURL := fmt.Sprintf("postgres://postgres:postgres@%s:%s/sims?sslmode=disable", host, port)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("Skipping test; unable to connect to Postgres: %v", err)
		return nil
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Skipping test; unable to ping Postgres: %v", err)
		return nil
	}

	// Verify that migrations have been applied before attempting synthetic generation
	var exists bool
	err = pool.QueryRow(ctx, "SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'virtual_machines')").Scan(&exists)
	if err != nil || !exists {
		pool.Close()
		t.Skip("skipping db-dependent test; tables not migrated")
		return nil
	}

	return pool
}

func TestDatasetConfigDefaults(t *testing.T) {
	cfg := DefaultDatasetConfig()
	if cfg.VMCount != 10000 {
		t.Errorf("expected default VMCount 10000, got %d", cfg.VMCount)
	}
	if cfg.VCenterCount != 1 {
		t.Errorf("expected default VCenterCount 1, got %d", cfg.VCenterCount)
	}
	if cfg.BatchSize != 5000 {
		t.Errorf("expected default BatchSize 5000, got %d", cfg.BatchSize)
	}
}

func TestGenerator_SyntheticBatch(t *testing.T) {
	pool := getTestDBPool(t)
	if pool == nil {
		return
	}
	defer pool.Close()

	ctx := context.Background()
	cfg := DatasetConfig{
		VMCount:      200,
		VCenterCount: 1,
		BatchSize:    100,
		CleanFirst:   true,
	}

	gen := NewGenerator(pool, cfg)
	stats, err := gen.Generate(ctx)
	if err != nil {
		t.Fatalf("generation failed: %v", err)
	}

	if stats.VMCount != 200 {
		t.Errorf("expected 200 VMs, got %d", stats.VMCount)
	}
	if stats.NICCount < 200 {
		t.Errorf("expected at least 200 NICs, got %d", stats.NICCount)
	}
	if stats.TagCount < 200 {
		t.Errorf("expected at least 200 tags, got %d", stats.TagCount)
	}
	if stats.CustomAttrCount < 200 {
		t.Errorf("expected at least 200 custom attributes, got %d", stats.CustomAttrCount)
	}
	if len(stats.SampleVMIDs) == 0 {
		t.Errorf("expected non-empty SampleVMIDs")
	}

	// Now test the workload runner against these generated records
	workloadCfg := WorkloadConfig{
		Concurrency:  2,
		Duration:     2 * time.Second,
		TotalQueries: 20,
	}
	runner := NewWorkloadRunner(pool, workloadCfg, stats)
	res, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("workload runner failed: %v", err)
	}

	if res.TotalQueries == 0 {
		t.Errorf("expected executed queries > 0, got %d", res.TotalQueries)
	}
	if res.ErrorCount > 0 {
		t.Errorf("expected 0 errors, got %d", res.ErrorCount)
	}
	if res.P50 <= 0 {
		t.Errorf("expected positive P50 latency, got %v", res.P50)
	}
}
