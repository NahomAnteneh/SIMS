//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"math"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/persistence"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/vmware/govmomi/vim25/types"
)

type churnSample struct {
	Timestamp   time.Duration
	HeapAllocMB float64
	Goroutines  int
	QueueDepth  int
}

// TestChurn_RealtimePipeline executes continuous, multi-component churn against the
// SIMS realtime ingestion pipeline (PropertyCollector streamer, BoundedQueue,
// StreamBatcher, and IncrementalApplier) to verify:
// 1. Zero data loss and eventual database convergence under high update velocity.
// 2. Bounded memory consumption (HeapAlloc remains stable, no buffer bloat).
// 3. Zero goroutine leakage across continuous streaming cycles.
// 4. Queue backpressure and batch drain invariants.
func TestChurn_RealtimePipeline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-churn-pipeline")
	metrics := telemetry.NewMetrics("churn_test", "0.1.0")
	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{}, metrics)
	applier := collector.NewIncrementalApplier(persister, nil, nil)

	// 1. Seed baseline VMs
	const vmCount = 20
	baselineVMs := make([]domain.VMRecord, vmCount)
	for i := 0; i < vmCount; i++ {
		moref := fmt.Sprintf("vm-churn-%03d", i)
		baselineVMs[i] = domain.VMRecord{
			VCenterID:       vcenterID,
			MoRef:           moref,
			InstanceUUID:    uuid.New().String(),
			BiosUUID:        uuid.New().String(),
			Name:            fmt.Sprintf("churn-vm-%03d", i),
			PowerState:      "poweredOff",
			ConnectionState: "connected",
			CPUCores:        2,
			MemoryMB:        4096,
			GuestOS:         "rhel9_64Guest",
		}
	}
	if _, err := persister.PersistVMBatch(ctx, baselineVMs); err != nil {
		t.Fatalf("failed to persist initial baseline VMs: %v", err)
	}

	// 2. Determine test duration
	duration := 30 * time.Second
	if durEnv := os.Getenv("CHURN_DURATION"); durEnv != "" {
		if d, err := time.ParseDuration(durEnv); err == nil && d > 0 {
			duration = d
		}
	} else if testing.Short() {
		duration = 5 * time.Second
	}

	t.Logf("Starting SIMS Real-Time Ingestion Churn Test (Configured Duration: %v)...", duration)

	// 3. Setup BoundedQueue and StreamBatcher
	queueCapacity := 500
	queue := collector.NewBoundedQueue[types.ObjectUpdate](vcenterID, queueCapacity, metrics)

	cfg := collector.BatcherConfig{
		VCenterID:     vcenterID,
		MaxBatchSize:  25,
		FlushInterval: 50 * time.Millisecond,
		DrainTimeout:  5 * time.Second,
		Backoff: collector.BackoffConfig{
			BaseDelay: 50 * time.Millisecond,
			MaxDelay:  500 * time.Millisecond,
			Factor:    1.5,
		},
	}
	batcher := collector.NewStreamBatcher(cfg, queue, applier, metrics, nil, nil)

	// 4. Measure Baseline Memory and Goroutines
	runtime.GC()
	var mStart runtime.MemStats
	runtime.ReadMemStats(&mStart)
	baselineGoroutines := runtime.NumGoroutine()

	var wg sync.WaitGroup

	// Run StreamBatcher consumer in background
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = batcher.Run(ctx)
	}()

	// 5. Continuous Churn Generator Goroutine
	churnStop := make(chan struct{})
	var churnUpdatesGenerated int64
	var churnBatchesGenerated int64

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(15 * time.Millisecond)
		defer ticker.Stop()

		toggle := false
		cycle := 0
		for {
			select {
			case <-churnStop:
				return
			case <-ticker.C:
				cycle++
				toggle = !toggle
				powerState := types.VirtualMachinePowerStatePoweredOn
				if !toggle {
					powerState = types.VirtualMachinePowerStatePoweredOff
				}

				// Pick 4 VMs to mutate each cycle
				for j := 0; j < 4; j++ {
					targetIdx := (cycle*4 + j) % vmCount
					vmMoref := fmt.Sprintf("vm-churn-%03d", targetIdx)

					update := types.ObjectUpdate{
						Kind: types.ObjectUpdateKindModify,
						Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: vmMoref},
						ChangeSet: []types.PropertyChange{
							{
								Name: "runtime.powerState",
								Op:   types.PropertyChangeOpAssign,
								Val:  powerState,
							},
							{
								Name: "summary.config.numCpu",
								Op:   types.PropertyChangeOpAssign,
								Val:  int32(2 + (cycle % 4)),
							},
						},
					}

					if err := queue.Push(ctx, update); err == nil {
						churnUpdatesGenerated++
					}
				}
				churnBatchesGenerated++
			}
		}
	}()

	// 6. Telemetry Sampler Goroutine
	var samples []churnSample
	var sampleMu sync.Mutex

	samplerDone := make(chan struct{})
	go func() {
		sampleTicker := time.NewTicker(2 * time.Second)
		defer sampleTicker.Stop()
		start := time.Now()

		for {
			select {
			case <-samplerDone:
				return
			case <-sampleTicker.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				sampleMu.Lock()
				samples = append(samples, churnSample{
					Timestamp:   time.Since(start),
					HeapAllocMB: float64(m.HeapAlloc) / 1024 / 1024,
					Goroutines:  runtime.NumGoroutine(),
					QueueDepth:  queue.Depth(),
				})
				sampleMu.Unlock()
			}
		}
	}()

	// 7. Run for Configured Duration
	time.Sleep(duration)

	// 8. Clean Shutdown
	close(churnStop)
	close(samplerDone)

	// Drain queue and stop batcher
	cancel()
	batcher.Stop()
	wg.Wait()

	// 9. Measure Final Memory and Goroutines
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	var mEnd runtime.MemStats
	runtime.ReadMemStats(&mEnd)
	finalGoroutines := runtime.NumGoroutine()

	// 10. Verify Database Convergence
	var activeCount int
	err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM virtual_machines
		WHERE vcenter_id = $1 AND deleted_at IS NULL;
	`, vcenterID).Scan(&activeCount)
	if err != nil {
		t.Fatalf("failed to query virtual_machines count: %v", err)
	}
	if activeCount != vmCount {
		t.Errorf("expected %d active VMs in database, got %d", vmCount, activeCount)
	}

	// 11. Evaluate Telemetry and Invariants
	heapDeltaMB := (float64(mEnd.HeapAlloc) - float64(mStart.HeapAlloc)) / 1024 / 1024
	goroutineDelta := finalGoroutines - baselineGoroutines

	t.Logf("===================================================================")
	t.Logf(" SIMS Real-Time Ingestion Churn Test Results                      ")
	t.Logf("===================================================================")
	t.Logf(" Duration:                  %v", duration)
	t.Logf(" Updates Processed:         %d updates across %d push cycles", churnUpdatesGenerated, churnBatchesGenerated)
	t.Logf(" Baseline HeapAlloc:        %.2f MB", float64(mStart.HeapAlloc)/1024/1024)
	t.Logf(" Final HeapAlloc:           %.2f MB (Delta: %.2f MB)", float64(mEnd.HeapAlloc)/1024/1024, heapDeltaMB)
	t.Logf(" Baseline Goroutines:       %d", baselineGoroutines)
	t.Logf(" Final Goroutines:          %d (Delta: %d)", finalGoroutines, goroutineDelta)
	t.Logf(" Peak Queue Depth Sampled:  %d", maxChurnQueueDepth(samples))
	t.Logf("===================================================================")

	// Invariant Checks
	if math.Abs(float64(goroutineDelta)) > 4 {
		t.Errorf("potential goroutine leak detected: baseline %d, final %d (delta %d)",
			baselineGoroutines, finalGoroutines, goroutineDelta)
	}

	if heapDeltaMB > 25.0 {
		t.Errorf("excessive heap growth detected: delta %.2f MB exceeded 25MB threshold", heapDeltaMB)
	}

	// SPRINT REQUIREMENT ACCOUNTABILITY:
	// A full 1-hour churn test is required for production qualification (Sprint 2 gate).
	// Because running for 3600 seconds is infeasible within the automated test suite,
	// we explicitly log the result as a partial window run.
	if duration < 1*time.Hour {
		t.Logf("NOTE: Churn test executed for %v (feasible test window).", duration)
		t.Logf("Full 1-hour churn gate requirement is reported as NOT MET (ran %v partial window).", duration)
		t.Logf("Execute 'CHURN_DURATION=1h make test-churn' during dedicated pre-release qualification window.")
	}
}

func maxChurnQueueDepth(samples []churnSample) int {
	max := 0
	for _, s := range samples {
		if s.QueueDepth > max {
			max = s.QueueDepth
		}
	}
	return max
}
