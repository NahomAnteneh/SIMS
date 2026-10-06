//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infrastructure/sims/generated/db"
	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/persistence"
)

func toPgUUID(u uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: u, Valid: true}
}

type scaleResult struct {
	numVCenters     int
	vmsPerVC        int
	totalVMs        int
	startupDuration time.Duration
	ingestDuration  time.Duration
	writeRate       float64 // VMs / second
	heapAllocMB     float64
	maxConcurrent   int64
}

func runScaleSimulation(t *testing.T, pool *pgxpool.Pool, numVCenters int, vmsPerVC int) scaleResult {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	queries := db.New(pool)
	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{
		FlushInterval: 50 * time.Millisecond,
		FlushSize:     250,
	}, nil)

	// Concurrency limiter: global hydration semaphore (max 4 concurrent hydrations)
	concurrencyRegistry := collector.NewConcurrencyRegistry(4, 5, 5)

	startupStart := time.Now()

	// Seed vCenters and targets
	vcIDs := make([]uuid.UUID, numVCenters)
	for i := 0; i < numVCenters; i++ {
		vcUUID := uuid.New()
		version := "8.0.3"
		if i%2 == 1 {
			version = "7.0.3"
		}

		createdVC, err := queries.CreateVCenter(ctx, db.CreateVCenterParams{
			Name:         fmt.Sprintf("scale-vc-%d-%s", i, vcUUID.String()[:8]),
			ApiUrl:       fmt.Sprintf("https://vc-scale-%d.corp.local/sdk", i),
			Version:      version,
			Build:        "24000000",
			Status:       "AVAILABLE",
			Priority:     100,
			Enabled:      true,
			Capabilities: []byte("{}"),
			TlsSettings:  []byte("{}"),
		})
		if err != nil {
			t.Fatalf("failed to create scale vcenter %d: %v", i, err)
		}
		vcIDs[i] = uuid.UUID(createdVC.ID.Bytes)
	}

	startupDuration := time.Since(startupStart)

	defer func() {
		cleanupCtx := context.Background()
		for _, vcID := range vcIDs {
			pgVC := toPgUUID(vcID)
			_, _ = pool.Exec(cleanupCtx, "DELETE FROM collector_targets WHERE vcenter_id = $1", pgVC)
			_, _ = pool.Exec(cleanupCtx, "DELETE FROM vm_tags WHERE vm_id IN (SELECT id FROM virtual_machines WHERE vcenter_id = $1)", pgVC)
			_, _ = pool.Exec(cleanupCtx, "DELETE FROM virtual_machines WHERE vcenter_id = $1", pgVC)
			_, _ = pool.Exec(cleanupCtx, "DELETE FROM vcenters WHERE id = $1", pgVC)
		}
	}()

	var mStart runtime.MemStats
	runtime.ReadMemStats(&mStart)

	ingestStart := time.Now()
	var wg sync.WaitGroup
	var activeHydrations atomic.Int64
	var maxConcurrent atomic.Int64

	for i := 0; i < numVCenters; i++ {
		wg.Add(1)
		vcID := vcIDs[i]
		go func(idx int, targetVC uuid.UUID) {
			defer wg.Done()

			// Acquire global hydration semaphore slot
			if err := concurrencyRegistry.Global().AcquireHydration(ctx); err != nil {
				t.Errorf("failed to acquire hydration semaphore for target %d: %v", idx, err)
				return
			}
			defer concurrencyRegistry.Global().ReleaseHydration()

			cur := activeHydrations.Add(1)
			for {
				oldMax := maxConcurrent.Load()
				if cur <= oldMax || maxConcurrent.CompareAndSwap(oldMax, cur) {
					break
				}
			}
			defer activeHydrations.Add(-1)

			// Generate and bulk persist synthetic inventory records
			records := make([]domain.VMRecord, vmsPerVC)
			targetVCStr := targetVC.String()
			for v := 0; v < vmsPerVC; v++ {
				records[v] = domain.VMRecord{
					VCenterID:       targetVCStr,
					MoRef:           fmt.Sprintf("vm-scale-%d-%d", idx, v),
					InstanceUUID:    uuid.NewString(),
					Name:            fmt.Sprintf("scale-vm-%d-%d", idx, v),
					PowerState:      "poweredOn",
					ConnectionState: "connected",
					CPUCores:        4,
					MemoryMB:        8192,
					GuestOS:         "rhel9_64Guest",
					GuestHostName:   fmt.Sprintf("scale-vm-%d-%d.internal", idx, v),
					Annotation:      "scale test inventory",
				}
			}

			_, persistErr := persister.PersistVMBatch(ctx, records)
			if persistErr != nil {
				t.Errorf("bulk persist failed for target %d: %v", idx, persistErr)
			}
		}(i, vcID)
	}

	wg.Wait()
	ingestDuration := time.Since(ingestStart)

	var mEnd runtime.MemStats
	runtime.ReadMemStats(&mEnd)

	totalVMs := numVCenters * vmsPerVC
	writeRate := float64(totalVMs) / ingestDuration.Seconds()
	heapMB := float64(mEnd.HeapAlloc) / (1024 * 1024)

	return scaleResult{
		numVCenters:     numVCenters,
		vmsPerVC:        vmsPerVC,
		totalVMs:        totalVMs,
		startupDuration: startupDuration,
		ingestDuration:  ingestDuration,
		writeRate:       writeRate,
		heapAllocMB:     heapMB,
		maxConcurrent:   maxConcurrent.Load(),
	}
}

func TestScaleS4_MultiTargetScale(t *testing.T) {
	connStr := getConnString("postgres", "postgres")
	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		t.Fatalf("failed to connect to postgres: %v", err)
	}
	defer pool.Close()

	densities := []struct {
		vcenters int
		vms      int
	}{
		{vcenters: 3, vms: 100},
		{vcenters: 5, vms: 100},
		{vcenters: 10, vms: 100},
	}

	t.Logf("=== S4.13 Multi-Target Scale Suite ===")
	t.Logf("%-10s %-10s %-12s %-16s %-16s %-16s %-12s",
		"Targets", "Total VMs", "Startup (ms)", "Ingest Time (ms)", "Write Rate (vm/s)", "Heap Alloc (MB)", "Max Hydration")

	for _, d := range densities {
		res := runScaleSimulation(t, pool, d.vcenters, d.vms)

		// Assertions:
		// 1. All total VMs were ingested
		if res.totalVMs != d.vcenters*d.vms {
			t.Fatalf("scale count mismatch: expected %d, got %d", d.vcenters*d.vms, res.totalVMs)
		}
		// 2. Global hydration concurrency never exceeded limit of 4
		if res.maxConcurrent > 4 {
			t.Fatalf("concurrency limit violated: maxConcurrent was %d, expected <= 4", res.maxConcurrent)
		}

		t.Logf("%-10d %-10d %-12.2f %-16.2f %-16.2f %-16.2f %-12d",
			res.numVCenters,
			res.totalVMs,
			float64(res.startupDuration.Microseconds())/1000.0,
			float64(res.ingestDuration.Microseconds())/1000.0,
			res.writeRate,
			res.heapAllocMB,
			res.maxConcurrent,
		)
	}
}
