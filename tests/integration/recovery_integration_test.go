//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/persistence"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/infrastructure/sims/internal/vsphere"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/view"
)

func TestReconcileStaleVMs_SoftDeletesMissingVMs(t *testing.T) {
	ctx := context.Background()
	pool := setupTestPool(t, ctx)
	defer pool.Close()

	metrics := telemetry.NewMetrics("test", "0.1.0")
	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{FlushSize: 100}, metrics)

	vcenterID := createTestVCenter(t, ctx, pool, "vc-stale-test")
	t0 := time.Now().Add(-10 * time.Minute)

	// Insert initial batch of 2 VMs at t0
	vms := []domain.VMRecord{
		{
			VCenterID:       vcenterID,
			MoRef:           "vm-keep",
			Name:            "web-keep-01",
			PowerState:      "poweredOn",
			ConnectionState: "connected",
			CPUCores:        2,
			MemoryMB:        4096,
		},
		{
			VCenterID:       vcenterID,
			MoRef:           "vm-stale",
			Name:            "web-stale-02",
			PowerState:      "poweredOff",
			ConnectionState: "connected",
			CPUCores:        4,
			MemoryMB:        8192,
		},
	}

	persisted, err := persister.PersistVMBatch(ctx, vms)
	if err != nil {
		t.Fatalf("failed to persist initial VM batch: %v", err)
	}
	if persisted != 2 {
		t.Fatalf("expected 2 persisted VMs, got: %d", persisted)
	}

	// Manually set last_seen_at for both to t0
	_, err = pool.Exec(ctx, `UPDATE virtual_machines SET last_seen_at = $1 WHERE vcenter_id = $2`, t0, vcenterID)
	if err != nil {
		t.Fatalf("failed to update last_seen_at to t0: %v", err)
	}

	// Re-hydration starts at t1
	t1 := time.Now().Add(-1 * time.Minute)

	// New sync only sees vm-keep (vm-stale was deleted from vCenter while offline)
	activeBatch := []domain.VMRecord{
		{
			VCenterID:       vcenterID,
			MoRef:           "vm-keep",
			Name:            "web-keep-01",
			PowerState:      "poweredOn",
			ConnectionState: "connected",
			CPUCores:        2,
			MemoryMB:        4096,
		},
	}
	_, err = persister.PersistVMBatch(ctx, activeBatch)
	if err != nil {
		t.Fatalf("failed to persist active VM batch: %v", err)
	}

	// Run ReconcileStaleVMs with cutoff t1
	reconciled, err := persister.ReconcileStaleVMs(ctx, vcenterID, t1)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if reconciled != 1 {
		t.Fatalf("expected 1 stale VM reconciled, got: %d", reconciled)
	}

	// Verify vm-stale is soft deleted (deleted_at IS NOT NULL)
	var staleDeletedAt *time.Time
	err = pool.QueryRow(ctx, `SELECT deleted_at FROM virtual_machines WHERE vcenter_id = $1 AND moref = 'vm-stale'`, vcenterID).Scan(&staleDeletedAt)
	if err != nil {
		t.Fatalf("failed to query vm-stale: %v", err)
	}
	if staleDeletedAt == nil {
		t.Fatal("expected vm-stale to be soft-deleted, but deleted_at is NULL")
	}

	// Verify vm-keep is active (deleted_at IS NULL)
	var keepDeletedAt *time.Time
	err = pool.QueryRow(ctx, `SELECT deleted_at FROM virtual_machines WHERE vcenter_id = $1 AND moref = 'vm-keep'`, vcenterID).Scan(&keepDeletedAt)
	if err != nil {
		t.Fatalf("failed to query vm-keep: %v", err)
	}
	if keepDeletedAt != nil {
		t.Fatalf("expected vm-keep to be active, but deleted_at is %v", keepDeletedAt)
	}

	// Verify active inventory query returns exactly 1 VM (no duplicate identities, queryable)
	var activeCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM virtual_machines WHERE vcenter_id = $1 AND deleted_at IS NULL`, vcenterID).Scan(&activeCount)
	if err != nil {
		t.Fatalf("failed to count active VMs: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly 1 active VM, got: %d", activeCount)
	}
}

func TestRecoveryManager_FullPipelineLiveVCSim(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := setupTestPool(t, ctx)
	defer pool.Close()

	// Clean up any stale vcsim VMs from prior test runs
	_, _ = pool.Exec(ctx, "DELETE FROM virtual_machines WHERE name LIKE 'DC0_%'")
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM virtual_machines WHERE name LIKE 'DC0_%'")
	}()

	// Configure vcsim with vSphere 8.0.3 per ADR-018
	model := simulator.VPX()
	model.ServiceContent.About.Version = "8.0.3"
	model.ServiceContent.About.ApiVersion = "8.0.3.0"
	model.ServiceContent.About.FullName = "VMware vCenter Server 8.0.3"
	defer model.Remove()

	if err := model.Create(); err != nil {
		t.Fatalf("failed to create vcsim: %v", err)
	}

	server := model.Service.NewServer()
	defer server.Close()

	sm, err := vsphere.NewSessionManager(vsphere.SessionConfig{
		URL:            server.URL.String(),
		Username:       "user",
		Password:       "pass",
		Insecure:       true,
		RequestTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create session manager: %v", err)
	}

	if err := sm.Connect(ctx); err != nil {
		t.Fatalf("failed to connect to vcsim: %v", err)
	}
	if err := sm.Login(ctx); err != nil {
		t.Fatalf("failed to login to vcsim: %v", err)
	}
	defer func() {
		_ = sm.Logout(context.Background())
	}()

	client := sm.Client()
	viewMgr := view.NewManager(client.Client)
	cv, err := viewMgr.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		t.Fatalf("failed to create ContainerView: %v", err)
	}
	defer func() {
		_ = cv.Destroy(context.Background())
	}()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-rec-pipeline")
	metrics := telemetry.NewMetrics("test", "0.1.0")
	fsm := collector.NewWorkerFSM(vcenterID, metrics, nil)

	// Advance FSM to DELTA_STREAMING
	_ = fsm.Transition(collector.WorkerStateConnecting)
	_ = fsm.Transition(collector.WorkerStateSessionActive)
	_ = fsm.Transition(collector.WorkerStateTopologyPreflight)
	_ = fsm.Transition(collector.WorkerStateFullInventorySync)
	_ = fsm.Transition(collector.WorkerStateDeltaStreaming)

	spec := collector.BuildVMPropertyFilterSpec(cv.Reference())
	govmomiPC := vsphere.NewGovmomiPropertyCollector(client)
	registry := collector.NewCleanupRegistry(nil, 5*time.Second)
	defer func() {
		_ = registry.Teardown(context.Background())
	}()

	cfg := collector.StreamerConfig{
		VCenterID:              vcenterID,
		MaxWaitSeconds:         1,
		MaxObjects:             100,
		MaxStreamBlockDuration: 5 * time.Second,
	}

	streamer := collector.NewPropertyStreamer(govmomiPC, spec, cfg, metrics, nil, registry)
	if err := streamer.InitFilter(ctx); err != nil {
		t.Fatalf("failed to init filter: %v", err)
	}

	hydrator := collector.NewHydrator(sm, collector.HydratorConfig{MaxObjects: 100})
	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{FlushSize: 100}, metrics)

	rm := collector.NewRecoveryManager(vcenterID, streamer, hydrator, persister, fsm, metrics, nil)

	// Execute recovery simulating an InvalidCollectorVersion fault
	err = rm.Recover(ctx, collector.ErrInvalidCollectorVersion)
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	// Verify FSM is back in DELTA_STREAMING
	if fsm.Current() != collector.WorkerStateDeltaStreaming {
		t.Fatalf("expected FSM in DELTA_STREAMING after recovery, got: %s", fsm.Current())
	}

	// Verify resync metric
	resyncCount := testutil.ToFloat64(metrics.ResyncTotal.WithLabelValues(vcenterID, "invalid_collector_version"))
	if resyncCount != 1 {
		t.Fatalf("expected resync count 1, got: %v", resyncCount)
	}

	// Verify inventory in DB is fully populated and queryable (4 VMs from vcsim)
	var vmCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM virtual_machines WHERE vcenter_id = $1 AND deleted_at IS NULL`, vcenterID).Scan(&vmCount)
	if err != nil {
		t.Fatalf("failed to query inventory: %v", err)
	}
	if vmCount < 4 {
		t.Fatalf("expected at least 4 active VMs after recovery, got: %d", vmCount)
	}

	// Verify no duplicate identities
	var dupCount int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM (
			SELECT moref FROM virtual_machines
			WHERE vcenter_id = $1 AND deleted_at IS NULL
			GROUP BY moref HAVING COUNT(*) > 1
		) dups
	`, vcenterID).Scan(&dupCount)
	if err != nil {
		t.Fatalf("failed to check for duplicates: %v", err)
	}
	if dupCount != 0 {
		t.Fatalf("expected 0 duplicate active morefs, got: %d", dupCount)
	}
}
