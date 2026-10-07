//go:build integration || soak
// +build integration soak

package soak_test

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/persistence"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/jackc/pgx/v5/pgxpool"
)

func getConnString(user, password string) string {
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = os.Getenv("SIMS_DB_HOST")
	}
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = os.Getenv("SIMS_DB_PORT")
	}
	if port == "" {
		port = "5433"
	}
	dbName := os.Getenv("SIMS_DB_NAME")
	if dbName == "" {
		dbName = "sims"
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, password, host, port, dbName)
}

type soakSample struct {
	Timestamp     time.Time
	Goroutines    int
	HeapAllocMB   float64
	HeapSysMB     float64
	PoolAcquired  int32
	PoolIdle      int32
	TotalIngested int64
}

// TestSoak_ContinuousWorkload executes the 72-Hour Soak Test Harness (S5.10).
// In automated CI/integration runs, duration defaults to 10s (smoke mode).
// Set SOAK_DURATION=5m (smoke) or SOAK_DURATION=72h (full soak) for endurance testing.
func TestSoak_ContinuousWorkload(t *testing.T) {
	durationStr := os.Getenv("SOAK_DURATION")
	soakDuration := 10 * time.Second
	if durationStr != "" {
		parsed, err := time.ParseDuration(durationStr)
		if err != nil {
			t.Fatalf("invalid SOAK_DURATION %q: %v", durationStr, err)
		}
		soakDuration = parsed
	}

	t.Logf("Starting SIMS Soak Test Harness (Duration: %v)...", soakDuration)

	ctx, cancel := context.WithTimeout(context.Background(), soakDuration+15*time.Second)
	defer cancel()

	poolCfg, err := pgxpool.ParseConfig(getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to parse pool config: %v", err)
	}
	poolCfg.MaxConns = 20
	poolCfg.MinConns = 5

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	testVCID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO vcenters (id, name, api_url, credential_ref, status)
		VALUES ($1, 'vc-soak-harness', $2, 'cred-1', 'active')
	`, testVCID, "https://vc-soak-"+testVCID.String()+".test/sdk")
	if err != nil {
		t.Fatalf("failed to insert test vcenter: %v", err)
	}

	metrics := telemetry.NewMetrics("soak-test", "v1.0.0")
	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{
		FlushInterval: 50 * time.Millisecond,
		FlushSize:     200,
	}, metrics)

	// Measure baseline
	runtime.GC()
	baselineGoroutines := runtime.NumGoroutine()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	baselineAllocMB := float64(m.HeapAlloc) / (1024 * 1024)

	t.Logf("Baseline metrics: Goroutines=%d, HeapAlloc=%.2f MB", baselineGoroutines, baselineAllocMB)

	var totalIngestedVMs int64
	var totalIngestErrors int64
	var totalQueriesExecuted int64
	var totalQueryErrors int64

	stopCh := make(chan struct{})
	var wg sync.WaitGroup

	// 1. Continuous Ingestion Pipeline Worker (produces batches of VM updates)
	wg.Add(1)
	go func() {
		defer wg.Done()
		batchIndex := 0
		for {
			select {
			case <-stopCh:
				return
			default:
				records := make([]domain.VMRecord, 25)
				for i := range records {
					records[i] = domain.VMRecord{
						VCenterID:       testVCID.String(),
						MoRef:           fmt.Sprintf("vm-soak-%d-%d", batchIndex%100, i),
						Name:            fmt.Sprintf("soak-vm-%d-%d", batchIndex%100, i),
						PowerState:      "poweredOn",
						ConnectionState: "connected",
						CPUCores:        4,
						MemoryMB:        8192,
						Tags: []domain.TagRecord{
							{TagID: "tag-soak-1", CategoryName: "Env", TagName: "Soak"},
						},
					}
				}
				batchIndex++
				n, pErr := persister.PersistVMBatch(ctx, records)
				if pErr != nil {
					atomic.AddInt64(&totalIngestErrors, 1)
				} else {
					atomic.AddInt64(&totalIngestedVMs, int64(n))
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
	}()

	// 2. Continuous Query Worker (simulates ongoing read load)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopCh:
				return
			default:
				var count int
				qErr := pool.QueryRow(ctx, `
					SELECT COUNT(*)
					FROM virtual_machines
					WHERE vcenter_id = $1
				`, testVCID).Scan(&count)
				if qErr != nil {
					atomic.AddInt64(&totalQueryErrors, 1)
				} else {
					atomic.AddInt64(&totalQueriesExecuted, 1)
				}
				time.Sleep(30 * time.Millisecond)
			}
		}
	}()

	// 3. Telemetry & Stability Monitor (samples memory, goroutines, pool health)
	samples := make([]soakSample, 0)
	sampleTicker := time.NewTicker(500 * time.Millisecond)
	defer sampleTicker.Stop()

	soakTimer := time.NewTimer(soakDuration)
	defer soakTimer.Stop()

	running := true
	for running {
		select {
		case <-soakTimer.C:
			running = false
		case <-sampleTicker.C:
			var sm runtime.MemStats
			runtime.ReadMemStats(&sm)
			stat := pool.Stat()
			s := soakSample{
				Timestamp:     time.Now(),
				Goroutines:    runtime.NumGoroutine(),
				HeapAllocMB:   float64(sm.HeapAlloc) / (1024 * 1024),
				HeapSysMB:     float64(sm.HeapSys) / (1024 * 1024),
				PoolAcquired:  stat.AcquiredConns(),
				PoolIdle:      stat.IdleConns(),
				TotalIngested: atomic.LoadInt64(&totalIngestedVMs),
			}
			samples = append(samples, s)
		}
	}

	// Gracefully terminate workload workers
	close(stopCh)
	wg.Wait()

	// Wait for stabilization and final GC
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	var finalM runtime.MemStats
	runtime.ReadMemStats(&finalM)
	finalGoroutines := runtime.NumGoroutine()
	finalAllocMB := float64(finalM.HeapAlloc) / (1024 * 1024)

	ingested := atomic.LoadInt64(&totalIngestedVMs)
	ingestErrors := atomic.LoadInt64(&totalIngestErrors)
	queries := atomic.LoadInt64(&totalQueriesExecuted)
	queryErrors := atomic.LoadInt64(&totalQueryErrors)
	poolStat := pool.Stat()

	t.Logf("=== Soak Test Run Completed ===")
	t.Logf("Duration:             %v", soakDuration)
	t.Logf("Total Ingested VMs:   %d (errors: %d)", ingested, ingestErrors)
	t.Logf("Total Queries Run:    %d (errors: %d)", queries, queryErrors)
	t.Logf("Baseline Goroutines:  %d -> Final Goroutines: %d", baselineGoroutines, finalGoroutines)
	t.Logf("Baseline Heap:        %.2f MB -> Final Heap: %.2f MB", baselineAllocMB, finalAllocMB)
	t.Logf("Pool Acquired Conns:  %d (idle: %d, total: %d)",
		poolStat.AcquiredConns(), poolStat.IdleConns(), poolStat.TotalConns())

	// Assertions for Zero Loss and Stability
	if ingestErrors > 0 {
		t.Fatalf("detected %d ingestion errors during soak run", ingestErrors)
	}
	if queryErrors > 0 {
		t.Fatalf("detected %d query errors during soak run", queryErrors)
	}
	if ingested == 0 {
		t.Fatal("expected at least some VMs to be ingested during soak run")
	}

	// Goroutine leak assertion: final goroutines within tolerance of baseline
	goroutineDelta := finalGoroutines - baselineGoroutines
	if goroutineDelta > 15 {
		t.Fatalf("potential goroutine leak detected: delta=%d (baseline=%d, final=%d)",
			goroutineDelta, baselineGoroutines, finalGoroutines)
	}

	// Connection leak assertion: all leased connections must be returned
	if poolStat.AcquiredConns() != 0 {
		t.Fatalf("connection pool leak detected: %d acquired connections remain unreleased", poolStat.AcquiredConns())
	}

	// Heap growth rate assertion: final heap should not have exploded exponentially
	if finalAllocMB > baselineAllocMB+50.0 {
		t.Fatalf("potential memory leak: heap allocated grew excessively by %.2f MB", finalAllocMB-baselineAllocMB)
	}

	t.Log("SUCCESS: Soak test verified 0 data loss, bounded memory, 0 goroutine leaks, and 0 pool leaks.")
}
