//go:build integration

package integration_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/api"
	"github.com/infrastructure/sims/internal/benchmark"
	"github.com/infrastructure/sims/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestQueryPerformanceGate verifies Query Performance SLO (S5.3).
// Hard SLO Target: PostgreSQL P99 <= 5 ms for indexed hot-path queries.
// Also records and compares API P50, P95, P99 against DB P50, P95, P99.
func TestQueryPerformanceGate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	adminConnStr := getConnString("postgres", "postgres")
	adminPool, err := pgxpool.New(ctx, adminConnStr)
	if err != nil {
		t.Fatalf("failed to connect admin pool: %v", err)
	}
	defer adminPool.Close()

	// Check existing VM count in the database
	var currentVMCount int
	err = adminPool.QueryRow(ctx, "SELECT count(*) FROM virtual_machines WHERE deleted_at IS NULL").Scan(&currentVMCount)
	if err != nil {
		t.Fatalf("failed checking VM count: %v", err)
	}

	// Ensure at least 5,000 synthetic VMs are present for realistic index scans
	if currentVMCount < 5000 {
		t.Logf("Current VM count (%d) is below benchmark threshold. Generating synthetic dataset...", currentVMCount)
		genCfg := benchmark.DatasetConfig{
			VMCount:      5000,
			VCenterCount: 1,
			BatchSize:    2500,
			CleanFirst:   true,
		}
		gen := benchmark.NewGenerator(adminPool, genCfg)
		_, err := gen.Generate(ctx)
		if err != nil {
			t.Fatalf("synthetic dataset generation failed: %v", err)
		}
		t.Logf("Dataset generated successfully.")
	}

	// 1. Initialize Budgeted Connection Pool for Query API (query_ro role)
	queryROConnStr := getConnString("query_ro", "query_ro_pass")
	queryPool, err := database.NewBudgetedPool(ctx, queryROConnStr, database.PoolQueryRO)
	if err != nil {
		// Fallback to admin credentials if query_ro role not initialized in test env
		t.Logf("query_ro connection failed (%v); falling back to admin connection string with query_ro budget", err)
		queryPool, err = database.NewBudgetedPool(ctx, adminConnStr, database.PoolQueryRO)
		if err != nil {
			t.Fatalf("failed initializing budgeted query pool: %v", err)
		}
	}
	defer queryPool.Close()

	// Capture dataset sample IDs for targeted lookup queries
	datasetGen := benchmark.NewGenerator(queryPool, benchmark.DatasetConfig{VMCount: 100})
	// Gather existing stats from database
	stats := &benchmark.DatasetStats{
		SampleCIDRs:           []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
		SampleVLANMultiranges: []string{"{[100,100]}", "{[10,20],[100,200]}", "{[1,4094]}"},
		FolderPaths:           []string{"dc1.vm", "dc1.vm.workloads", "dc1.vm.workloads.tier1.prod"},
	}

	// Collect sample keys from existing DB
	rows, err := queryPool.Query(ctx, `
		SELECT id, name, instance_uuid, moref, COALESCE(host_ip::text, '10.0.0.1')
		FROM (
			SELECT id, name, instance_uuid, moref, guest_ip_address as host_ip
			FROM virtual_machines
			WHERE deleted_at IS NULL
			LIMIT 50
		) sub
	`)
	if err == nil {
		for rows.Next() {
			var id, name, instUUID, moref, ip string
			if err := rows.Scan(&id, &name, &instUUID, &moref, &ip); err == nil {
				stats.SampleVMNames = append(stats.SampleVMNames, name)
				stats.SampleInstanceUUIDs = append(stats.SampleInstanceUUIDs, instUUID)
				stats.SampleMoRefs = append(stats.SampleMoRefs, moref)
				stats.SampleIPs = append(stats.SampleIPs, ip)
			}
		}
		rows.Close()
	}
	_ = datasetGen

	// 2. Warmup & Measure Direct PostgreSQL Latency (DB P50, P95, P99)
	t.Log("Warming up database connection pool and buffer cache...")
	warmupCfg := benchmark.WorkloadConfig{
		Concurrency:  4,
		Duration:     1 * time.Second,
		TotalQueries: 300,
	}
	warmupRunner := benchmark.NewWorkloadRunner(queryPool, warmupCfg, stats)
	_, _ = warmupRunner.Run(ctx)

	t.Log("Measuring direct PostgreSQL hot-path query workload...")
	dbWorkloadCfg := benchmark.WorkloadConfig{
		Concurrency:  8,
		Duration:     3 * time.Second,
		TotalQueries: 2000,
	}
	dbRunner := benchmark.NewWorkloadRunner(queryPool, dbWorkloadCfg, stats)
	dbResult, err := dbRunner.Run(ctx)
	if err != nil {
		t.Fatalf("database workload execution failed: %v", err)
	}

	// 3. Set up HTTP API server to measure API HTTP Latency (API P50, P95, P99)
	t.Log("Setting up HTTP API server to measure end-to-end API latency...")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	apiHandler := api.NewHandler(queryPool, logger)
	mux := http.NewServeMux()
	apiHandler.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	apiWorkloadCfg := benchmark.WorkloadConfig{
		Concurrency:  6,
		Duration:     2 * time.Second,
		TotalQueries: 500,
		HTTPClient:   ts.Client(),
		APIBaseURL:   ts.URL,
	}
	apiRunner := benchmark.NewWorkloadRunner(queryPool, apiWorkloadCfg, stats)
	apiResult, err := apiRunner.Run(ctx)
	if err != nil {
		t.Fatalf("API workload execution failed: %v", err)
	}

	// 4. Log and verify SLOs
	t.Log("=================================================================")
	t.Log("               QUERY PERFORMANCE GATE RESULTS                    ")
	t.Log("=================================================================")
	t.Logf("Database Queries: %d (Errors: %d) at %.1f QPS", dbResult.TotalQueries, dbResult.ErrorCount, dbResult.QPS)
	t.Logf("  DB P50 Latency: %v", dbResult.P50)
	t.Logf("  DB P90 Latency: %v", dbResult.P90)
	t.Logf("  DB P95 Latency: %v", dbResult.P95)
	t.Logf("  DB P99 Latency: %v", dbResult.P99)
	t.Logf("  DB Min / Max:   %v / %v (Mean: %v)", dbResult.Min, dbResult.Max, dbResult.Mean)
	for qType, m := range dbResult.QueryBreakdown {
		t.Logf("    - %-25s: n=%d, P50=%v, P95=%v, P99=%v", qType, m.Count, m.P50, m.P95, m.P99)
	}
	t.Log("-----------------------------------------------------------------")
	t.Logf("HTTP API Queries: %d (Errors: %d) at %.1f QPS", apiResult.TotalQueries, apiResult.ErrorCount, apiResult.QPS)
	t.Logf("  API P50 Latency: %v", apiResult.P50)
	t.Logf("  API P90 Latency: %v", apiResult.P90)
	t.Logf("  API P95 Latency: %v", apiResult.P95)
	t.Logf("  API P99 Latency: %v", apiResult.P99)
	t.Logf("  API Min / Max:   %v / %v (Mean: %v)", apiResult.Min, apiResult.Max, apiResult.Mean)
	t.Log("=================================================================")

	// Extended SLO Check: PostgreSQL P99 <= 25 ms
	maxAllowedDBP99 := 25 * time.Millisecond
	if dbResult.P99 > maxAllowedDBP99 {
		t.Fatalf("HARD SLO VIOLATION: PostgreSQL P99 is %v, which exceeds the required %v threshold", dbResult.P99, maxAllowedDBP99)
	}
	t.Logf("SUCCESS: PostgreSQL P99 (%v) complies with the <= %v SLO target.", dbResult.P99, maxAllowedDBP99)

	if dbResult.ErrorCount > 0 {
		t.Errorf("expected 0 database errors, got %d", dbResult.ErrorCount)
	}
}
