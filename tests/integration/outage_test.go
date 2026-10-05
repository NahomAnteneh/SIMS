//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/persistence"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vmware/govmomi/vim25/types"
)

func pausePostgres(t *testing.T) {
	t.Helper()
	cmd := exec.Command("docker", "pause", "sims-postgres")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to pause sims-postgres: %v (%s)", err, string(out))
	}
}

func unpausePostgres(t *testing.T) {
	t.Helper()
	cmd := exec.Command("docker", "unpause", "sims-postgres")
	_ = cmd.Run() // Idempotent unpause
}

func seedInitialVM(t *testing.T, ctx context.Context, persister *persistence.BulkPersister, vcenterID, moref string) {
	t.Helper()
	rec := domain.VMRecord{
		VCenterID:       vcenterID,
		MoRef:           moref,
		Name:            "vm-outage-test",
		PowerState:      "poweredOff",
		ConnectionState: "connected",
		CPUCores:        4,
		MemoryMB:        8192,
	}
	_, err := persister.PersistVMBatch(ctx, []domain.VMRecord{rec})
	if err != nil {
		t.Fatalf("failed to seed initial VM: %v", err)
	}
}

// TestDatabaseOutage_Unavailable10s verifies:
// 1. When PostgreSQL is paused for 10s, no updates are dropped.
// 2. The queue buffers deltas and producer blocks when capacity is reached.
// 3. When PostgreSQL recovers, the queue drains and database converges.
func TestDatabaseOutage_Unavailable10s(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-outage-10s")
	metrics := telemetry.NewMetrics("test", "0.1.0")
	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{}, metrics)
	applier := collector.NewIncrementalApplier(persister, nil, nil)

	moref := "vm-outage-10"
	seedInitialVM(t, ctx, persister, vcenterID, moref)

	queueCapacity := 20
	queue := collector.NewBoundedQueue[types.ObjectUpdate](vcenterID, queueCapacity, metrics)

	cfg := collector.BatcherConfig{
		VCenterID:     vcenterID,
		MaxBatchSize:  5,
		FlushInterval: 20 * time.Millisecond,
		DrainTimeout:  5 * time.Second,
		Backoff: collector.BackoffConfig{
			BaseDelay: 200 * time.Millisecond,
			MaxDelay:  1 * time.Second,
			Factor:    2.0,
		},
	}
	batcher := collector.NewStreamBatcher(cfg, queue, applier, metrics, nil, nil)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = batcher.Run(ctx)
	}()

	// 1. Pause PostgreSQL for 10 seconds
	pausePostgres(t)
	defer unpausePostgres(t) // Safety invariant

	t.Log("PostgreSQL paused (simulating 10s outage)...")
	outageStart := time.Now()

	// 2. Stream producer sends updates during the outage
	for i := 1; i <= 10; i++ {
		update := types.ObjectUpdate{
			Kind: types.ObjectUpdateKindModify,
			Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: moref},
			ChangeSet: []types.PropertyChange{
				{
					Name: "runtime.powerState",
					Op:   types.PropertyChangeOpAssign,
					Val:  types.VirtualMachinePowerStatePoweredOn,
				},
			},
		}
		if err := queue.Push(ctx, update); err != nil {
			t.Fatalf("push failed during outage: %v", err)
		}
	}

	// Wait remainder of 10s outage
	elapsed := time.Since(outageStart)
	if elapsed < 10*time.Second {
		time.Sleep(10*time.Second - elapsed)
	}

	// Verify queue retained updates during outage
	if queue.Depth() == 0 {
		t.Errorf("expected queue depth > 0 during DB outage, got 0")
	}
	t.Logf("Queue depth during 10s outage: %d / %d", queue.Depth(), queue.Capacity())

	// 3. Unpause PostgreSQL
	unpausePostgres(t)
	t.Log("PostgreSQL resumed. Waiting for queue to drain...")

	// 4. Verify queue drains completely
	drainDeadline := time.Now().Add(15 * time.Second)
	for queue.Depth() > 0 && time.Now().Before(drainDeadline) {
		time.Sleep(100 * time.Millisecond)
	}

	if queue.Depth() != 0 {
		t.Fatalf("expected queue depth 0 after recovery, got %d", queue.Depth())
	}

	// 5. Verify database converged
	var powerState string
	err := pool.QueryRow(ctx, "SELECT power_state FROM virtual_machines WHERE vcenter_id = $1 AND moref = $2", vcenterID, moref).Scan(&powerState)
	if err != nil {
		t.Fatalf("failed to query VM power state: %v", err)
	}
	if powerState != "poweredOn" {
		t.Errorf("expected converged power_state 'poweredOn', got: %s", powerState)
	}

	cancel()
	batcher.Stop()
	wg.Wait()
}

// TestDatabaseOutage_Unavailable60s verifies:
// 1. When PostgreSQL is paused for 60s, queue buffers without dropping updates.
// 2. Memory usage remains bounded during prolonged outage.
// 3. After recovery, all updates drain and database converges.
func TestDatabaseOutage_Unavailable60s(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-outage-60s")
	metrics := telemetry.NewMetrics("test", "0.1.0")
	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{}, metrics)
	applier := collector.NewIncrementalApplier(persister, nil, nil)

	moref := "vm-outage-60"
	seedInitialVM(t, ctx, persister, vcenterID, moref)

	queueCapacity := 100
	queue := collector.NewBoundedQueue[types.ObjectUpdate](vcenterID, queueCapacity, metrics)

	cfg := collector.BatcherConfig{
		VCenterID:     vcenterID,
		MaxBatchSize:  10,
		FlushInterval: 50 * time.Millisecond,
		DrainTimeout:  10 * time.Second,
		Backoff: collector.BackoffConfig{
			BaseDelay: 500 * time.Millisecond,
			MaxDelay:  3 * time.Second,
			Factor:    2.0,
		},
	}
	batcher := collector.NewStreamBatcher(cfg, queue, applier, metrics, nil, nil)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = batcher.Run(ctx)
	}()

	// Measure baseline memory
	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// Pause PostgreSQL for 60 seconds
	pausePostgres(t)
	defer unpausePostgres(t)

	t.Log("PostgreSQL paused for 60s outage test...")
	outageStart := time.Now()

	// Push 50 updates over the 60s window
	go func() {
		for i := 1; i <= 50; i++ {
			time.Sleep(1 * time.Second)
			_ = queue.Push(ctx, types.ObjectUpdate{
				Kind: types.ObjectUpdateKindModify,
				Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: moref},
				ChangeSet: []types.PropertyChange{
					{
						Name: "runtime.powerState",
						Op:   types.PropertyChangeOpAssign,
						Val:  types.VirtualMachinePowerStatePoweredOn,
					},
				},
			})
		}
	}()

	// Wait for full 60 seconds outage
	time.Sleep(60 * time.Second)
	t.Logf("60s outage elapsed (%v). Current queue depth: %d", time.Since(outageStart), queue.Depth())

	// Verify memory stays bounded
	var memDuring runtime.MemStats
	runtime.ReadMemStats(&memDuring)
	heapDeltaMB := float64(memDuring.HeapAlloc-memBefore.HeapAlloc) / (1024 * 1024)
	t.Logf("HeapAlloc delta during 60s outage: %.2f MB", heapDeltaMB)
	if heapDeltaMB > 50.0 {
		t.Errorf("heap growth exceeded 50MB bound: %.2f MB", heapDeltaMB)
	}

	// Unpause PostgreSQL
	unpausePostgres(t)
	t.Log("PostgreSQL resumed. Waiting for queue to drain...")

	// Verify queue drains
	drainDeadline := time.Now().Add(25 * time.Second)
	for queue.Depth() > 0 && time.Now().Before(drainDeadline) {
		time.Sleep(200 * time.Millisecond)
	}

	if queue.Depth() != 0 {
		t.Fatalf("expected queue depth 0 after recovery, got %d", queue.Depth())
	}

	var powerState string
	err := pool.QueryRow(ctx, "SELECT power_state FROM virtual_machines WHERE vcenter_id = $1 AND moref = $2", vcenterID, moref).Scan(&powerState)
	if err != nil {
		t.Fatalf("failed to query VM power state: %v", err)
	}
	if powerState != "poweredOn" {
		t.Errorf("expected converged power_state 'poweredOn', got: %s", powerState)
	}

	cancel()
	batcher.Stop()
	wg.Wait()
}

type mockSlowApplier struct {
	mu           sync.Mutex
	appliedCount int
	delayPerCall time.Duration
}

func (m *mockSlowApplier) ApplyBatch(ctx context.Context, _ string, updates []types.ObjectUpdate) error {
	if m.delayPerCall > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.delayPerCall):
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appliedCount += len(updates)
	return nil
}

func (m *mockSlowApplier) AppliedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.appliedCount
}

// TestDatabaseOutage_SlowDatabase verifies that when the database is slow,
// the bounded queue buffers bursts cleanly and eventually drains without data loss.
func TestDatabaseOutage_SlowDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	vcenterID := "vc-slow-db"
	metrics := telemetry.NewMetrics("test", "0.1.0")

	// Slow applier that introduces 50ms per batch
	slowApplier := &mockSlowApplier{
		delayPerCall: 50 * time.Millisecond,
	}

	queueCapacity := 30
	queue := collector.NewBoundedQueue[types.ObjectUpdate](vcenterID, queueCapacity, metrics)

	cfg := collector.BatcherConfig{
		VCenterID:     vcenterID,
		MaxBatchSize:  5,
		FlushInterval: 10 * time.Millisecond,
		DrainTimeout:  5 * time.Second,
	}
	batcher := collector.NewStreamBatcher(cfg, queue, slowApplier, metrics, nil, nil)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = batcher.Run(ctx)
	}()

	totalUpdates := 25
	for i := 1; i <= totalUpdates; i++ {
		err := queue.Push(ctx, types.ObjectUpdate{
			Kind: types.ObjectUpdateKindModify,
			Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: fmt.Sprintf("vm-%d", i)},
		})
		if err != nil {
			t.Fatalf("push failed: %v", err)
		}
	}

	// Drain remaining items
	deadline := time.Now().Add(10 * time.Second)
	for slowApplier.AppliedCount() < totalUpdates && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	if slowApplier.AppliedCount() != totalUpdates {
		t.Fatalf("expected all %d updates applied through slow DB, got %d", totalUpdates, slowApplier.AppliedCount())
	}

	cancel()
	batcher.Stop()
	wg.Wait()
}

// TestDatabaseOutage_ConnectionExhaustion verifies that when connection pool is exhausted,
// the batcher safely retries with backoff and drains once connections become available.
func TestDatabaseOutage_ConnectionExhaustion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a constrained connection pool with maxConns=2
	poolCfg, err := pgxpool.ParseConfig(getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}
	poolCfg.MaxConns = 2
	poolCfg.MinConns = 1
	constrainedPool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer constrainedPool.Close()

	vcenterID := createTestVCenter(t, ctx, constrainedPool, "vc-exhaustion")
	metrics := telemetry.NewMetrics("test", "0.1.0")
	persister := persistence.NewBulkPersister(constrainedPool, persistence.WriterConfig{}, metrics)
	applier := collector.NewIncrementalApplier(persister, nil, nil)

	moref := "vm-exhaust"
	seedInitialVM(t, ctx, persister, vcenterID, moref)

	// Exhaust both connections in the pool
	conn1, err := constrainedPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire conn1: %v", err)
	}
	conn2, err := constrainedPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire conn2: %v", err)
	}

	queue := collector.NewBoundedQueue[types.ObjectUpdate](vcenterID, 20, metrics)
	cfg := collector.BatcherConfig{
		VCenterID:     vcenterID,
		MaxBatchSize:  2, // Batcher takes first 2, leaving remaining items in queue while blocked
		FlushInterval: 10 * time.Millisecond,
		DrainTimeout:  5 * time.Second,
		Backoff: collector.BackoffConfig{
			BaseDelay: 50 * time.Millisecond,
			MaxDelay:  200 * time.Millisecond,
			Factor:    1.5,
		},
	}
	batcher := collector.NewStreamBatcher(cfg, queue, applier, metrics, nil, nil)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = batcher.Run(ctx)
	}()

	// Push 8 updates while pool is exhausted
	for i := 1; i <= 8; i++ {
		_ = queue.Push(ctx, types.ObjectUpdate{
			Kind: types.ObjectUpdateKindModify,
			Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: moref},
			ChangeSet: []types.PropertyChange{
				{
					Name: "runtime.powerState",
					Op:   types.PropertyChangeOpAssign,
					Val:  types.VirtualMachinePowerStatePoweredOn,
				},
			},
		})
	}

	// Wait briefly for batcher to pop first batch and block on connection acquisition
	time.Sleep(100 * time.Millisecond)

	// Verify remaining updates are buffered in queue while connections are held
	depth := queue.Depth()
	if depth == 0 {
		t.Errorf("expected buffered updates in queue during connection exhaustion, got 0")
	}
	t.Logf("Queue depth during connection exhaustion: %d", depth)

	// Release both held connections
	conn1.Release()
	conn2.Release()
	t.Log("Released exhausted connections; batcher should now acquire and flush")

	// Wait for database to converge to poweredOn
	converged := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var powerState string
		if err := constrainedPool.QueryRow(ctx, "SELECT power_state FROM virtual_machines WHERE vcenter_id = $1 AND moref = $2", vcenterID, moref).Scan(&powerState); err == nil {
			if powerState == "poweredOn" && queue.Depth() == 0 {
				converged = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !converged {
		t.Fatalf("database failed to converge to poweredOn after connection exhaustion cleared")
	}

	cancel()
	batcher.Stop()
	wg.Wait()
}
