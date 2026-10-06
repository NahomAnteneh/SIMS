//go:build integration

package integration_test

import (
	"context"
	"fmt"
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
	"github.com/vmware/govmomi/vim25/types"
)

func createGateTarget(ctx context.Context, pool *pgxpool.Pool, vcID pgtype.UUID) pgtype.UUID {
	var targetID pgtype.UUID
	_ = pool.QueryRow(ctx, `
		INSERT INTO collector_targets (vcenter_id, enabled, priority)
		VALUES ($1, true, 100)
		RETURNING id
	`, vcID).Scan(&targetID)
	return targetID
}

// TestGateS4_WorkerTakeoverOnExpiredLease tests that when a worker lease expires,
// a standby worker cleanly takes over the target and the fencing version increments.
func TestGateS4_WorkerTakeoverOnExpiredLease(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, getConnString("postgres", "postgres"))
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	queries := db.New(pool)

	vcID := uuid.New()
	createdVC, err := queries.CreateVCenter(ctx, db.CreateVCenterParams{
		Name:         fmt.Sprintf("gate-takeover-%s", vcID.String()[:8]),
		ApiUrl:       fmt.Sprintf("https://vc-%s.corp.local/sdk", vcID.String()[:8]),
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

	targetID := createGateTarget(ctx, pool, createdVC.ID)

	defer func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM collector_leases WHERE target_id = $1", targetID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM collector_targets WHERE id = $1", targetID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM vcenters WHERE id = $1", createdVC.ID)
	}()

	worker1 := "worker-alpha"
	worker2 := "worker-bravo"

	// 1. Worker 1 acquires lease with a short 2-second TTL
	lease1, err := queries.AcquireLease(ctx, db.AcquireLeaseParams{
		TargetID:   targetID,
		HolderID:   worker1,
		LeaseUntil: pgtype.Timestamptz{Time: time.Now().Add(2 * time.Second), Valid: true},
	})
	if err != nil {
		t.Fatalf("worker 1 initial lease acquisition failed: %v", err)
	}

	if lease1.HolderID != worker1 {
		t.Fatalf("expected holder %s, got %s", worker1, lease1.HolderID)
	}
	initialVersion := lease1.Version

	// 2. Worker 2 immediately tries to acquire — must fail because lease is active
	_, err = queries.AcquireLease(ctx, db.AcquireLeaseParams{
		TargetID:   targetID,
		HolderID:   worker2,
		LeaseUntil: pgtype.Timestamptz{Time: time.Now().Add(5 * time.Second), Valid: true},
	})
	if err == nil {
		t.Fatalf("worker 2 should not acquire an active non-expired lease")
	}

	// 3. Wait for Worker 1's lease to expire
	time.Sleep(2500 * time.Millisecond)

	// 4. Worker 2 now acquires expired lease
	lease2, err := queries.AcquireLease(ctx, db.AcquireLeaseParams{
		TargetID:   targetID,
		HolderID:   worker2,
		LeaseUntil: pgtype.Timestamptz{Time: time.Now().Add(10 * time.Second), Valid: true},
	})
	if err != nil {
		t.Fatalf("worker 2 takeover of expired lease failed: %v", err)
	}

	if lease2.HolderID != worker2 {
		t.Fatalf("expected new holder to be %s, got %s", worker2, lease2.HolderID)
	}
	if lease2.Version <= initialVersion {
		t.Fatalf("expected fencing version to increment, was %d now %d", initialVersion, lease2.Version)
	}
}

// TestGateS4_MultiWorkerRaceZeroDuplicateLeases asserts that concurrent lease acquisition
// guarantees exactly one winner with strictly zero duplicate active leases.
func TestGateS4_MultiWorkerRaceZeroDuplicateLeases(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, getConnString("postgres", "postgres"))
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	queries := db.New(pool)

	vcID := uuid.New()
	createdVC, err := queries.CreateVCenter(ctx, db.CreateVCenterParams{
		Name:         fmt.Sprintf("gate-race-%s", vcID.String()[:8]),
		ApiUrl:       "https://vc-race.corp.local/sdk",
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

	targetID := createGateTarget(ctx, pool, createdVC.ID)

	defer func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM collector_leases WHERE target_id = $1", targetID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM collector_targets WHERE id = $1", targetID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM vcenters WHERE id = $1", createdVC.ID)
	}()

	numWorkers := 10
	var wg sync.WaitGroup
	var acquiredCount atomic.Int32
	var winnerHolder string
	var mu sync.Mutex

	startBarrier := make(chan struct{})

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		workerID := fmt.Sprintf("worker-candidate-%d", i)
		go func(wID string) {
			defer wg.Done()
			<-startBarrier

			lease, err := queries.AcquireLease(ctx, db.AcquireLeaseParams{
				TargetID:   targetID,
				HolderID:   wID,
				LeaseUntil: pgtype.Timestamptz{Time: time.Now().Add(15 * time.Second), Valid: true},
			})
			if err == nil {
				acquiredCount.Add(1)
				mu.Lock()
				winnerHolder = lease.HolderID
				mu.Unlock()
			}
		}(workerID)
	}

	close(startBarrier)
	wg.Wait()

	if acquiredCount.Load() != 1 {
		t.Fatalf("expected exactly 1 winner in lease race, got %d", acquiredCount.Load())
	}

	// Verify database row count for this target
	var leaseCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM collector_leases WHERE target_id = $1", targetID).Scan(&leaseCount)
	if err != nil {
		t.Fatalf("failed to count leases: %v", err)
	}
	if leaseCount != 1 {
		t.Fatalf("expected exactly 1 lease row in DB, got %d", leaseCount)
	}

	t.Logf("Race resolved: 1 winner (%s), 0 duplicate leases", winnerHolder)
}

// TestGateS4_CrossVCenterMigration_XvMotion tests identity continuity across vCenters
// in both event arrival orders.
func TestGateS4_CrossVCenterMigration_XvMotion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, getConnString("postgres", "postgres"))
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	queries := db.New(pool)
	handler := collector.NewXvMotionHandler(queries)

	vcA, err := queries.CreateVCenter(ctx, db.CreateVCenterParams{
		Name:         fmt.Sprintf("gate-xvm-a-%s", uuid.NewString()[:8]),
		ApiUrl:       "https://vca-xvm.corp.local/sdk",
		Version:      "8.0.3",
		Build:        "24000000",
		Status:       "AVAILABLE",
		Priority:     100,
		Enabled:      true,
		Capabilities: []byte("{}"),
		TlsSettings:  []byte("{}"),
	})
	if err != nil {
		t.Fatalf("failed to create vcA: %v", err)
	}

	vcB, err := queries.CreateVCenter(ctx, db.CreateVCenterParams{
		Name:         fmt.Sprintf("gate-xvm-b-%s", uuid.NewString()[:8]),
		ApiUrl:       "https://vcb-xvm.corp.local/sdk",
		Version:      "7.0.3",
		Build:        "20150588",
		Status:       "AVAILABLE",
		Priority:     100,
		Enabled:      true,
		Capabilities: []byte("{}"),
		TlsSettings:  []byte("{}"),
	})
	if err != nil {
		t.Fatalf("failed to create vcB: %v", err)
	}

	defer func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM virtual_machines WHERE vcenter_id IN ($1, $2)", vcA.ID, vcB.ID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM vcenters WHERE id IN ($1, $2)", vcA.ID, vcB.ID)
	}()

	vcAID := uuid.UUID(vcA.ID.Bytes)
	vcBID := uuid.UUID(vcB.ID.Bytes)

	// Order 1: Leave(A) -> Enter(B)
	t.Run("Order1_LeaveThenEnter", func(t *testing.T) {
		instUUID := uuid.NewString()
		morefA := "vm-gate-a-1"
		morefB := "vm-gate-b-1"

		initialVM, err := queries.UpsertVM(ctx, db.UpsertVMParams{
			VcenterID:    vcA.ID,
			Moref:        morefA,
			InstanceUuid: instUUID,
			Name:         "vm-order1",
			PowerState:   "poweredOn",
		})
		if err != nil {
			t.Fatalf("upsert initial failed: %v", err)
		}

		// Leave A
		if err := handler.HandleLeave(ctx, vcAID, morefA, instUUID); err != nil {
			t.Fatalf("HandleLeave failed: %v", err)
		}

		// Enter B
		migrated, isMigrated, err := handler.HandleEnter(ctx, vcBID, morefB, instUUID)
		if err != nil {
			t.Fatalf("HandleEnter failed: %v", err)
		}
		if !isMigrated {
			t.Fatalf("expected isMigrated to be true")
		}
		if migrated.ID != initialVM.ID {
			t.Fatalf("synthetic ID changed after migration! expected %v, got %v", initialVM.ID, migrated.ID)
		}
		if migrated.VcenterID.Bytes != vcB.ID.Bytes {
			t.Fatalf("destination vcenter_id mismatch")
		}
	})

	// Order 2: Enter(B) -> Leave(A)
	t.Run("Order2_EnterThenLeave", func(t *testing.T) {
		instUUID := uuid.NewString()
		morefA := "vm-gate-a-2"
		morefB := "vm-gate-b-2"

		initialVM, err := queries.UpsertVM(ctx, db.UpsertVMParams{
			VcenterID:    vcA.ID,
			Moref:        morefA,
			InstanceUuid: instUUID,
			Name:         "vm-order2",
			PowerState:   "poweredOn",
		})
		if err != nil {
			t.Fatalf("upsert initial failed: %v", err)
		}

		// Enter B before Leave A
		migrated, isMigrated, err := handler.HandleEnter(ctx, vcBID, morefB, instUUID)
		if err != nil {
			t.Fatalf("HandleEnter failed: %v", err)
		}
		if !isMigrated {
			t.Fatalf("expected isMigrated to be true")
		}
		if migrated.ID != initialVM.ID {
			t.Fatalf("synthetic ID changed")
		}

		// Subsequent stale Leave A arrives
		if err := handler.HandleLeave(ctx, vcAID, morefA, instUUID); err != nil {
			t.Fatalf("HandleLeave failed: %v", err)
		}

		// Verify migrated VM on B remains active
		activeOnB, err := queries.GetVMByInstanceUUIDAnyStatus(ctx, instUUID)
		if err != nil {
			t.Fatalf("lookup failed: %v", err)
		}
		if activeOnB.DeletedAt.Valid {
			t.Fatalf("stale leave mistakenly deleted the migrated active VM")
		}
		if activeOnB.VcenterID.Bytes != vcB.ID.Bytes {
			t.Fatalf("vcenter_id corrupted")
		}
	})
}

type gateMockScanner struct {
	refs []collector.VMRef
}

func (s *gateMockScanner) ListVMReferences(ctx context.Context) ([]collector.VMRef, error) {
	return s.refs, nil
}

// TestGateS4_DifferentialReconciliation_Convergence tests set math differential reconciliation.
func TestGateS4_DifferentialReconciliation_Convergence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, getConnString("postgres", "postgres"))
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	queries := db.New(pool)

	vcID := uuid.New()
	createdVC, err := queries.CreateVCenter(ctx, db.CreateVCenterParams{
		Name:         fmt.Sprintf("gate-diff-%s", vcID.String()[:8]),
		ApiUrl:       "https://vc-diff.corp.local/sdk",
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

	targetID := createGateTarget(ctx, pool, createdVC.ID)

	defer func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM virtual_machines WHERE vcenter_id = $1", createdVC.ID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM collector_targets WHERE id = $1", targetID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM vcenters WHERE id = $1", createdVC.ID)
	}()

	// Seed 3 VMs in DB: vm-1, vm-2, vm-3
	vm1, _ := queries.UpsertVM(ctx, db.UpsertVMParams{
		VcenterID:    createdVC.ID,
		Moref:        "vm-gate-1",
		InstanceUuid: uuid.NewString(),
		Name:         "vm-1",
		PowerState:   "poweredOn",
	})
	queries.UpsertVM(ctx, db.UpsertVMParams{
		VcenterID:    createdVC.ID,
		Moref:        "vm-gate-2",
		InstanceUuid: uuid.NewString(),
		Name:         "vm-2",
		PowerState:   "poweredOn",
	})
	queries.UpsertVM(ctx, db.UpsertVMParams{
		VcenterID:    createdVC.ID,
		Moref:        "vm-gate-3",
		InstanceUuid: uuid.NewString(),
		Name:         "vm-3",
		PowerState:   "poweredOn",
	})

	// vCenter scanner reports: vm-2, vm-3, and a new vm-4 (vm-1 was destroyed in vCenter)
	scanner := &gateMockScanner{
		refs: []collector.VMRef{
			{MoRef: "vm-gate-2", InstanceUUID: "uuid-2"},
			{MoRef: "vm-gate-3", InstanceUUID: "uuid-3"},
			{MoRef: "vm-gate-4", InstanceUUID: "uuid-4"},
		},
	}

	var enqueuedMorefs []string
	var enqueueMu sync.Mutex
	mockHydrateEnqueue := func(ctx context.Context, vcenterID uuid.UUID, missingMoRefs []string) error {
		enqueueMu.Lock()
		enqueuedMorefs = append(enqueuedMorefs, missingMoRefs...)
		enqueueMu.Unlock()
		return nil
	}

	reconciler := collector.NewDifferentialReconciler(pool, queries, mockHydrateEnqueue, nil)
	report, err := reconciler.Reconcile(ctx, uuid.UUID(createdVC.ID.Bytes), scanner)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// Assertions:
	// Missing in vCenter: vm-1 -> Tombstoned
	if report.MissingInVCenter != 1 {
		t.Fatalf("expected 1 missing in vcenter (tombstoned), got %d", report.MissingInVCenter)
	}
	// Missing in DB: vm-4 -> Enqueued for hydration
	if report.MissingInDB != 1 {
		t.Fatalf("expected 1 missing in DB (enqueued for hydration), got %d", report.MissingInDB)
	}
	// Present in both: vm-2 and vm-3 -> Touched
	if report.PresentInBoth != 2 {
		t.Fatalf("expected 2 present in both (touched), got %d", report.PresentInBoth)
	}

	// Verify in DB that vm-1 is tombstoned
	var tombstonedAt pgtype.Timestamptz
	err = pool.QueryRow(ctx, "SELECT deleted_at FROM virtual_machines WHERE id = $1", vm1.ID).Scan(&tombstonedAt)
	if err != nil {
		t.Fatalf("failed to query vm-1: %v", err)
	}
	if !tombstonedAt.Valid {
		t.Fatalf("expected vm-1 to have deleted_at set after reconciliation")
	}

	// Verify vm-4 was enqueued
	enqueueMu.Lock()
	defer enqueueMu.Unlock()
	if len(enqueuedMorefs) != 1 || enqueuedMorefs[0] != "vm-gate-4" {
		t.Fatalf("expected vm-gate-4 enqueued for hydration, got: %v", enqueuedMorefs)
	}
}

// Gate mock structures for TagReconciler testing in integration test package
type gateMockTagFetcher struct {
	tags map[string][]domain.TagRecord
	err  error
}

func (m *gateMockTagFetcher) FetchTagsForObjects(ctx context.Context, refs []types.ManagedObjectReference) (map[string][]domain.TagRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	res := make(map[string][]domain.TagRecord)
	for _, ref := range refs {
		if t, ok := m.tags[ref.Value]; ok {
			res[ref.Value] = t
		} else {
			res[ref.Value] = []domain.TagRecord{}
		}
	}
	return res, nil
}

func (m *gateMockTagFetcher) Close(ctx context.Context) error {
	return nil
}

type gateMockTagPersister struct {
	mu           sync.Mutex
	activeMorefs []string
	tags         map[string][]domain.TagRecord
}

func (m *gateMockTagPersister) ListActiveVMMorefs(ctx context.Context, vcenterID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeMorefs, nil
}

func (m *gateMockTagPersister) GetVMTags(ctx context.Context, vcenterID string) (map[string][]domain.TagRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	copyMap := make(map[string][]domain.TagRecord)
	for k, v := range m.tags {
		copyMap[k] = append([]domain.TagRecord(nil), v...)
	}
	return copyMap, nil
}

func (m *gateMockTagPersister) UpdateVMTags(ctx context.Context, vcenterID string, moref string, tags []domain.TagRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tags[moref] = tags
	return nil
}

func (m *gateMockTagPersister) UpdateTagsState(ctx context.Context, vcenterID string, state string, lastSyncedAt *time.Time) error {
	return nil
}

// TestGateS4_TagAttributeConvergence_NonDestructive tests tag reconciliation convergence
// and guarantees the non-destructive rule on API failures.
func TestGateS4_TagAttributeConvergence_NonDestructive(t *testing.T) {
	ctx := context.Background()

	persister := &gateMockTagPersister{
		activeMorefs: []string{"vm-10"},
		tags: map[string][]domain.TagRecord{
			"vm-10": {
				{TagID: "t-1", CategoryName: "env", TagName: "staging"},
			},
		},
	}

	fetcher := &gateMockTagFetcher{
		err: fmt.Errorf("REST API 503 Service Unavailable"),
	}

	reconciler := collector.NewTagReconciler(fetcher, persister, collector.DefaultTagReconcilerConfig("vc-tag-gate"), nil, nil)

	// 1. Fetch failure must fail without modifying existing tags
	_, err := reconciler.Reconcile(ctx)
	if err == nil {
		t.Fatalf("expected error on REST outage")
	}

	if len(persister.tags["vm-10"]) != 1 {
		t.Fatalf("non-destructive rule violated: tags deleted or modified on fetch failure")
	}

	// 2. REST service recovers with drift: 'staging' replaced with 'production'
	fetcher.err = nil
	fetcher.tags = map[string][]domain.TagRecord{
		"vm-10": {
			{TagID: "t-2", CategoryName: "env", TagName: "production"},
		},
	}

	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("reconcile on recovered service failed: %v", err)
	}

	if report.TagsAdded != 1 || report.TagsRemoved != 1 {
		t.Fatalf("expected 1 added and 1 removed, got added=%d removed=%d", report.TagsAdded, report.TagsRemoved)
	}

	convergedTags := persister.tags["vm-10"]
	if len(convergedTags) != 1 || convergedTags[0].TagName != "production" {
		t.Fatalf("tags failed to converge: %+v", convergedTags)
	}
}

// TestGateS4_TombstoneBatchPurger tests bounded batch deletion of tombstones.
func TestGateS4_TombstoneBatchPurger(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, getConnString("postgres", "postgres"))
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	queries := db.New(pool)
	purger := persistence.NewTombstonePurger(pool, queries, nil)

	vcID := uuid.New()
	createdVC, err := queries.CreateVCenter(ctx, db.CreateVCenterParams{
		Name:         fmt.Sprintf("gate-purge-%s", vcID.String()[:8]),
		ApiUrl:       "https://vc-purge.corp.local/sdk",
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
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM virtual_machines WHERE vcenter_id = $1", createdVC.ID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM vcenters WHERE id = $1", createdVC.ID)
	}()

	now := time.Now()
	expiredCutoff := now.Add(-45 * 24 * time.Hour) // 45 days old (> 30-day retention)
	recentCutoff := now.Add(-10 * 24 * time.Hour)  // 10 days old (< 30-day retention)

	// Insert 5 expired tombstones, 2 recent tombstones, 2 active VMs
	for i := 0; i < 9; i++ {
		vm, err := queries.UpsertVM(ctx, db.UpsertVMParams{
			VcenterID:    createdVC.ID,
			Moref:        fmt.Sprintf("vm-purge-%d", i),
			InstanceUuid: uuid.NewString(),
			Name:         fmt.Sprintf("vm-purge-%d", i),
			PowerState:   "poweredOff",
		})
		if err != nil {
			t.Fatalf("failed to insert test vm: %v", err)
		}

		if i < 5 {
			_, _ = pool.Exec(ctx, "UPDATE virtual_machines SET deleted_at = $1 WHERE id = $2", expiredCutoff, vm.ID)
		} else if i < 7 {
			_, _ = pool.Exec(ctx, "UPDATE virtual_machines SET deleted_at = $1 WHERE id = $2", recentCutoff, vm.ID)
		}
	}

	// Purge with batch size 2
	opts := persistence.PurgeOptions{
		BatchSize:           2,
		MaxBatches:          10,
		RetentionPeriod:     30 * 24 * time.Hour,
		SleepBetweenBatches: 2 * time.Millisecond,
	}

	report, err := purger.PurgeTombstones(ctx, opts)
	if err != nil {
		t.Fatalf("PurgeTombstones failed: %v", err)
	}

	if report.TotalRowsPurged != 5 {
		t.Fatalf("expected exactly 5 expired rows purged, got %d", report.TotalRowsPurged)
	}
	if report.BatchesRun < 3 {
		t.Fatalf("expected at least 3 batches, got %d", report.BatchesRun)
	}
	if report.LockDuration <= 0 {
		t.Fatalf("expected lock latency recorded")
	}

	var remaining int
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM virtual_machines WHERE vcenter_id = $1", createdVC.ID).Scan(&remaining)
	if remaining != 4 { // 2 recent + 2 active
		t.Fatalf("expected 4 remaining VMs in DB, got %d", remaining)
	}
}
