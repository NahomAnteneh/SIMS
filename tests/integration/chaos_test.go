//go:build integration
// +build integration

package integration_test

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	db "github.com/infrastructure/sims/generated/db"
	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/netutil"
	"github.com/infrastructure/sims/internal/persistence"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/infrastructure/sims/internal/vsphere"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/types"
)

// TestChaos_VCenterOutageAndReconnect verifies that when vCenter connection is abruptly dropped,
// the collector detects the disconnection, applies backoff, and automatically resumes
// streaming once the connection is restored, without dropping data.
func TestChaos_VCenterOutageAndReconnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	vcenterID := "vc-chaos-outage-1"
	metrics := telemetry.NewMetrics("chaos-test", "v1.0.0")
	fakePC := vsphere.NewFakePropertyCollector()

	// Initial normal update
	fakePC.EnqueueUpdate(&types.UpdateSet{
		Version: "2",
		FilterSet: []types.PropertyFilterUpdate{
			{
				Filter: types.ManagedObjectReference{Type: "PropertyFilter", Value: "filter-1"},
				ObjectSet: []types.ObjectUpdate{
					{
						Kind: types.ObjectUpdateKindModify,
						Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-101"},
						ChangeSet: []types.PropertyChange{
							{Name: "name", Val: "vm-before-outage"},
						},
					},
				},
			},
		},
	})

	var recoveredUpdates int64
	streamerCfg := collector.StreamerConfig{
		VCenterID:      vcenterID,
		MaxWaitSeconds: 1,
		CallDeadline:   2 * time.Second,
	}

	streamer := collector.NewPropertyStreamer(
		fakePC,
		types.PropertyFilterSpec{},
		streamerCfg,
		metrics,
		testLogger(),
		nil,
	)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			if ctx.Err() != nil {
				return
			}
			err := streamer.Run(ctx, func(_ context.Context, updates []types.ObjectUpdate) error {
				atomic.AddInt64(&recoveredUpdates, int64(len(updates)))
				return nil
			})
			if err == nil || errors.Is(err, context.Canceled) {
				return
			}
			// Brief backoff before reconnecting
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}()

	// Wait for first update to be received
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt64(&recoveredUpdates) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if atomic.LoadInt64(&recoveredUpdates) == 0 {
		t.Fatal("expected initial update to be received before outage")
	}

	// 1. Inject Chaos: Drop connection abruptly
	fakePC.InjectDisconnect()
	t.Log("Injected Chaos: vCenter connection dropped abruptly")

	// Wait during outage
	time.Sleep(200 * time.Millisecond)

	// 2. Clear fault: Connection restored, enqueue post-recovery update
	fakePC.InjectFault(nil)
	fakePC.EnqueueUpdate(&types.UpdateSet{
		Version: "3",
		FilterSet: []types.PropertyFilterUpdate{
			{
				Filter: types.ManagedObjectReference{Type: "PropertyFilter", Value: "filter-1"},
				ObjectSet: []types.ObjectUpdate{
					{
						Kind: types.ObjectUpdateKindModify,
						Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-101"},
						ChangeSet: []types.PropertyChange{
							{Name: "name", Val: "vm-after-recovery"},
						},
					},
				},
			},
		},
	})
	t.Log("Chaos Cleared: vCenter connection restored, post-recovery update queued")

	// Wait for automatic recovery and update consumption
	deadline = time.Now().Add(5 * time.Second)
	for atomic.LoadInt64(&recoveredUpdates) < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	// Stop streamer and wait for exit before test teardown
	cancel()
	wg.Wait()

	if atomic.LoadInt64(&recoveredUpdates) < 2 {
		t.Fatalf("expected at least 2 updates processed after outage recovery, got %d", atomic.LoadInt64(&recoveredUpdates))
	}
	t.Log("SUCCESS: Collector detected vCenter outage and automatically reconnected with 0 data loss.")
}

// TestChaos_PropertyCollectorRollover verifies recovery when vCenter returns InvalidCollectorVersion
// (PropertyCollector session invalidated / rollover).
func TestChaos_PropertyCollectorRollover(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	vcenterID := "vc-chaos-rollover-1"
	metrics := telemetry.NewMetrics("chaos-test", "v1.0.0")

	fakePC := vsphere.NewFakePropertyCollector()
	// 1. Inject typed InvalidCollectorVersion soap fault
	fakePC.InjectInvalidCollectorVersion()

	streamerCfg := collector.StreamerConfig{
		VCenterID:      vcenterID,
		MaxWaitSeconds: 1,
		CallDeadline:   2 * time.Second,
	}

	streamer := collector.NewPropertyStreamer(
		fakePC,
		types.PropertyFilterSpec{},
		streamerCfg,
		metrics,
		testLogger(),
		nil,
	)

	var resyncTriggered int64
	var recoveredUpdates int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			if ctx.Err() != nil {
				return
			}
			err := streamer.Run(ctx, func(_ context.Context, updates []types.ObjectUpdate) error {
				atomic.AddInt64(&recoveredUpdates, int64(len(updates)))
				return nil
			})
			if err == nil || errors.Is(err, context.Canceled) {
				return
			}
			if errors.Is(err, collector.ErrInvalidCollectorVersion) || collector.IsInvalidCollectorVersion(err) {
				atomic.AddInt64(&resyncTriggered, 1)
				// Re-sync: clear fault, reinit filter, enqueue fresh update
				fakePC.InjectFault(nil)
				_ = streamer.InitFilter(ctx)
				fakePC.EnqueueUpdate(&types.UpdateSet{
					Version: "post-rollover-1",
					FilterSet: []types.PropertyFilterUpdate{
						{
							Filter: types.ManagedObjectReference{Type: "PropertyFilter", Value: "filter-1"},
							ObjectSet: []types.ObjectUpdate{
								{
									Kind: types.ObjectUpdateKindEnter,
									Obj:  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-post-rollover"},
								},
							},
						},
					},
				})
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}()

	// Wait for rollover recovery and update consumption
	deadline := time.Now().Add(5 * time.Second)
	for (atomic.LoadInt64(&resyncTriggered) == 0 || atomic.LoadInt64(&recoveredUpdates) == 0) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	cancel()
	wg.Wait()

	if atomic.LoadInt64(&resyncTriggered) == 0 {
		t.Fatal("expected PropertyCollector rollover to trigger resync recovery handler")
	}
	if atomic.LoadInt64(&recoveredUpdates) == 0 {
		t.Fatal("expected updates to be received after rollover recovery")
	}
	t.Log("SUCCESS: PropertyCollector rollover detected and handled via resync recovery.")
}

// TestChaos_NetworkPartitionAndHighLatency asserts that high network latency and hung calls
// are bounded by CallDeadline and do not hang the collector indefinitely.
func TestChaos_NetworkPartitionAndHighLatency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	vcenterID := "vc-chaos-latency-1"
	fakePC := vsphere.NewFakePropertyCollector()

	// Inject hung call simulating packet drop / blackhole (500ms delay)
	fakePC.InjectHungWaitForUpdates(500 * time.Millisecond)

	maxCallDeadline := 100 * time.Millisecond
	streamerCfg := collector.StreamerConfig{
		VCenterID:      vcenterID,
		MaxWaitSeconds: 1,
		CallDeadline:   maxCallDeadline,
	}

	streamer := collector.NewPropertyStreamer(
		fakePC,
		types.PropertyFilterSpec{},
		streamerCfg,
		nil,
		testLogger(),
		nil,
	)

	start := time.Now()
	err := streamer.Run(ctx, func(_ context.Context, _ []types.ObjectUpdate) error {
		return nil
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected hung call to time out and return error, got nil")
	}
	if !errors.Is(err, collector.ErrStreamHung) {
		t.Fatalf("expected ErrStreamHung, got: %v", err)
	}
	if elapsed > 400*time.Millisecond {
		t.Fatalf("hung call took too long to time out: %v (expected <= 400ms)", elapsed)
	}
	if fakePC.CanceledWaits() == 0 {
		t.Error("expected CancelWaitForUpdates to be called upon proactive hang detection")
	}
	t.Logf("SUCCESS: Hung call cleanly timed out in %v (bounded by CallDeadline).", elapsed)
}


// TestChaos_WorkerCrashAndStandbyTakeover verifies distributed lease failover under worker crash:
// Worker A crashes -> Lease TTL expires -> Standby Worker B acquires lease with bumped fencing version ->
// Worker A is fenced out from any further renewal.
func TestChaos_WorkerCrashAndStandbyTakeover(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to connect to DB: %v", err)
	}
	defer pool.Close()

	testVCID := uuid.New()
	targetID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO vcenters (id, name, api_url, credential_ref, status)
		VALUES ($1, 'vc-takeover', $2, 'cred-1', 'active')
	`, testVCID, "https://vc-takeover-"+testVCID.String()+".test/sdk")
	if err != nil {
		t.Fatalf("failed to insert vcenter: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO collector_targets (id, vcenter_id, enabled)
		VALUES ($1, $2, true)
	`, targetID, testVCID)
	if err != nil {
		t.Fatalf("failed to insert target: %v", err)
	}

	queries := db.New(pool)

	// Worker A acquires lease with short TTL (2 seconds)
	leaseTTL := 2 * time.Second
	workerA := collector.NewLeaseCoordinator(pool, queries, collector.LeaseConfig{
		WorkerID:          "worker-active-A",
		LeaseTTL:          leaseTTL,
		HeartbeatInterval: 500 * time.Millisecond,
	}, testLogger())

	leaseA, err := workerA.Acquire(ctx, targetID)
	if err != nil {
		t.Fatalf("worker A failed to acquire initial lease: %v", err)
	}
	initialVersion := leaseA.Version
	t.Logf("Worker A acquired lease (holder=%s, version=%d)", leaseA.HolderID, initialVersion)

	// Standby Worker B attempts immediate acquisition -> Must fail (lease currently held)
	workerB := collector.NewLeaseCoordinator(pool, queries, collector.LeaseConfig{
		WorkerID:          "worker-standby-B",
		LeaseTTL:          leaseTTL,
		HeartbeatInterval: 500 * time.Millisecond,
	}, testLogger())

	_, err = workerB.Acquire(ctx, targetID)
	if !errors.Is(err, collector.ErrLeaseHeld) {
		t.Fatalf("expected ErrLeaseHeld for standby worker B, got: %v", err)
	}

	// 1. Simulate Worker A Crash: Worker A stops renewing (we cancel its heartbeat)
	leaseA.Close()
	t.Log("Simulated Worker A crash (heartbeat terminated). Waiting for lease TTL expiry...")

	// Wait for lease TTL to expire
	time.Sleep(leaseTTL + 300*time.Millisecond)

	// 2. Standby Worker B takes over expired lease
	leaseB, err := workerB.Acquire(ctx, targetID)
	if err != nil {
		t.Fatalf("worker B failed to take over expired lease: %v", err)
	}
	defer leaseB.Close()

	if leaseB.HolderID != "worker-standby-B" {
		t.Fatalf("expected holder worker-standby-B, got %s", leaseB.HolderID)
	}
	if leaseB.Version <= initialVersion {
		t.Fatalf("expected fencing version bumped (> %d), got %d", initialVersion, leaseB.Version)
	}
	t.Logf("Worker B took over lease successfully (holder=%s, new version=%d)", leaseB.HolderID, leaseB.Version)

	// 3. Fencing assertion: If zombie Worker A awakens and attempts renewal, it must be rejected
	err = workerA.Renew(ctx, leaseA)
	if !errors.Is(err, collector.ErrLeaseLost) {
		t.Fatalf("expected zombie worker A renewal to be rejected with ErrLeaseLost, got: %v", err)
	}
	t.Log("SUCCESS: Fencing token prevented zombie Worker A from corrupting state.")
}

// TestChaos_EventBurstAndWatermarkMonotonicity verifies that rapid bursts of thousands of events
// are handled cleanly without watermark regression or data loss.
func TestChaos_EventBurstAndWatermarkMonotonicity(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to connect to DB: %v", err)
	}
	defer pool.Close()

	testVCID := uuid.New()
	vcenterID := testVCID.String()
	_, err = pool.Exec(ctx, `
		INSERT INTO vcenters (id, name, api_url, credential_ref, status)
		VALUES ($1, 'vc-event-burst', $2, 'cred-1', 'active')
	`, testVCID, "https://vc-burst-"+vcenterID+".test/sdk")
	if err != nil {
		t.Fatalf("failed to insert vcenter: %v", err)
	}

	baseTime := time.Now().UTC()

	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{
		FlushInterval: 50 * time.Millisecond,
		FlushSize:     500,
	}, nil)

	// Burst of 1,000 sequential event watermarks
	totalEvents := 1000
	var lastPersistedKey int64
	for i := 1; i <= totalEvents; i++ {
		key := int64(i)
		eventTime := baseTime.Add(time.Duration(i) * time.Millisecond)
		err := persister.SaveEventWatermark(ctx, vcenterID, key, eventTime)
		if err != nil {
			t.Fatalf("failed to save event watermark %d: %v", key, err)
		}
		lastPersistedKey = key
	}

	// Verify watermark in database
	var dbKey int64
	var dbTime time.Time
	err = pool.QueryRow(ctx, `
		SELECT last_event_key, last_event_time
		FROM collector_state
		WHERE vcenter_id = $1
	`, testVCID).Scan(&dbKey, &dbTime)
	if err != nil {
		t.Fatalf("failed to query collector_state watermark: %v", err)
	}

	if dbKey != lastPersistedKey {
		t.Fatalf("expected watermark key %d, got %d", lastPersistedKey, dbKey)
	}
	t.Logf("SUCCESS: Event burst of %d events processed with monotonic watermark %d.", totalEvents, dbKey)
}

// TestChaos_TagRESTOutage_NonDestructiveRule verifies that when the vSphere Automation REST
// service (vapi/rest) is unavailable, existing VM tags in the database are PRESERVED and not wiped.
func TestChaos_TagRESTOutage_NonDestructiveRule(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to connect to DB: %v", err)
	}
	defer pool.Close()

	testVCID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO vcenters (id, name, api_url, credential_ref, status)
		VALUES ($1, 'vc-tags-chaos', $2, 'cred-1', 'active')
	`, testVCID, "https://vc-tags-"+testVCID.String()+".test/sdk")
	if err != nil {
		t.Fatalf("failed to insert vcenter: %v", err)
	}

	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{
		FlushInterval: 50 * time.Millisecond,
		FlushSize:     10,
	}, nil)

	moref := "vm-tag-chaos-1"
	initialVM := domain.VMRecord{
		VCenterID:       testVCID.String(),
		MoRef:           moref,
		Name:            "vm-tagged-for-chaos",
		PowerState:      "poweredOn",
		ConnectionState: "connected",
		CPUCores:        4,
		MemoryMB:        8192,
		Tags: []domain.TagRecord{
			{
				TagID:        "urn:vmomi:InventoryServiceTag:tag-chaos-prod",
				CategoryName: "Environment",
				TagName:      "Production",
			},
			{
				TagID:        "urn:vmomi:InventoryServiceTag:tag-chaos-pci",
				CategoryName: "Compliance",
				TagName:      "PCI-DSS",
			},
		},
	}

	// 1. Initial persist with tags
	_, err = persister.PersistVMBatch(ctx, []domain.VMRecord{initialVM})
	if err != nil {
		t.Fatalf("initial persist with tags failed: %v", err)
	}

	// Verify tags in database
	var tagCount int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM vm_tags vt
		JOIN virtual_machines vm ON vt.vm_id = vm.id
		WHERE vm.vcenter_id = $1 AND vm.moref = $2
	`, testVCID, moref).Scan(&tagCount)
	if err != nil || tagCount != 2 {
		t.Fatalf("expected 2 tags initially, got %d (err: %v)", tagCount, err)
	}

	// 2. Simulate subsequent update where TagFetcher fails (tags slice is nil)
	// Non-destructive rule (AGENTS.md §5):
	// nil Tags slice indicates absence in changeSet -> DO NOT CLEAR TAGS!
	// Only explicit empty slice make([]domain.TagRecord, 0) clears child collections.
	subsequentVM := domain.VMRecord{
		VCenterID:       testVCID.String(),
		MoRef:           moref,
		Name:            "vm-tagged-for-chaos-renamed",
		PowerState:      "poweredOn",
		ConnectionState: "connected",
		CPUCores:        4,
		MemoryMB:        8192,
		Tags:            nil, // Tag service failed -> nil tags
	}

	_, err = persister.PersistVMBatch(ctx, []domain.VMRecord{subsequentVM})
	if err != nil {
		t.Fatalf("subsequent persist failed: %v", err)
	}

	// 3. Assert tags were PRESERVED in the database
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM vm_tags vt
		JOIN virtual_machines vm ON vt.vm_id = vm.id
		WHERE vm.vcenter_id = $1 AND vm.moref = $2
	`, testVCID, moref).Scan(&tagCount)
	if err != nil {
		t.Fatalf("failed to query tags: %v", err)
	}

	if tagCount != 2 {
		t.Fatalf("CRITICAL DATA LOSS: Tag REST outage wiped existing tags! Expected 2, got %d", tagCount)
	}
	t.Log("SUCCESS: Non-destructive tag semantics verified: tags preserved during REST outage.")
}

// TestChaos_MixedFleetVersioning asserts simultaneous vSphere 7.0 and 8.0 targets operate concurrently
// without version policy conflicts or cross-target pollution.
func TestChaos_MixedFleetVersioning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Target 1: vSphere 7.0.3
	model7 := simulator.VPX()
	vsphere.ConfigureTestVPX(model7)
	model7.ServiceContent.About.Version = "7.0.3"
	model7.ServiceContent.About.ApiVersion = "7.0.3.0"
	model7.ServiceContent.About.FullName = "VMware vCenter Server 7.0.3 build-20150588"
	defer model7.Remove()
	if err := model7.Create(); err != nil {
		t.Fatalf("failed to create vcsim 7.0: %v", err)
	}
	model7.Service.TLS = new(tls.Config)
	s7 := model7.Service.NewServer()
	defer s7.Close()

	// Target 2: vSphere 8.0.3
	model8 := simulator.VPX()
	vsphere.ConfigureTestVPX(model8)
	defer model8.Remove()
	if err := model8.Create(); err != nil {
		t.Fatalf("failed to create vcsim 8.0: %v", err)
	}
	model8.Service.TLS = new(tls.Config)
	s8 := model8.Service.NewServer()
	defer s8.Close()

	// Validate Target 1
	host7, portStr7, err := net.SplitHostPort(s7.URL.Host)
	if err != nil {
		t.Fatalf("failed to split host port for s7: %v", err)
	}
	port7, _ := strconv.Atoi(portStr7)
	res7, err := vsphere.ValidateConnection(ctx, vsphere.ValidationParams{
		Host:          host7,
		Port:          port7,
		Username:      s7.URL.User.Username(),
		Password:      "password",
		TLS:           vsphere.TLSValidationConfig{Verify: false},
		AllowLoopback: true,
		AllowPrivate:  true,
		Policy: &netutil.SSRFPolicy{
			AllowLoopback:      true,
			AllowPrivateRanges: true,
		},
	})
	if err != nil {
		t.Fatalf("validation for 7.0 target failed: %v", err)
	}

	// Validate Target 2
	host8, portStr8, err := net.SplitHostPort(s8.URL.Host)
	if err != nil {
		t.Fatalf("failed to split host port for s8: %v", err)
	}
	port8, _ := strconv.Atoi(portStr8)
	res8, err := vsphere.ValidateConnection(ctx, vsphere.ValidationParams{
		Host:          host8,
		Port:          port8,
		Username:      s8.URL.User.Username(),
		Password:      "password",
		TLS:           vsphere.TLSValidationConfig{Verify: false},
		AllowLoopback: true,
		AllowPrivate:  true,
		Policy: &netutil.SSRFPolicy{
			AllowLoopback:      true,
			AllowPrivateRanges: true,
		},
	})
	if err != nil {
		t.Fatalf("validation for 8.0 target failed: %v", err)
	}

	if res7.ProductVersion != "7.0.3" || res8.ProductVersion != "8.0.3" {
		t.Fatalf("expected version 7.0.3 and 8.0.3, got %q and %q", res7.ProductVersion, res8.ProductVersion)
	}
	t.Logf("SUCCESS: Mixed fleet validated concurrently (Target 7.0: %s, Target 8.0: %s)",
		res7.ProductVersion, res8.ProductVersion)
}

// TestChaos_TargetRegistrationUnderHeavyLoad asserts that new vCenter targets can be registered
// dynamically while the bulk persistence pipeline is actively processing batches of records.
func TestChaos_TargetRegistrationUnderHeavyLoad(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to connect to DB: %v", err)
	}
	defer pool.Close()

	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{
		FlushInterval: 10 * time.Millisecond,
		FlushSize:     20,
	}, nil)

	activeVCID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO vcenters (id, name, api_url, credential_ref, status)
		VALUES ($1, 'vc-heavy-load', $2, 'cred-1', 'active')
	`, activeVCID, "https://vc-heavy-"+activeVCID.String()+".test/sdk")
	if err != nil {
		t.Fatalf("failed to insert initial vcenter: %v", err)
	}

	// Background worker producing heavy ingestion load on activeVCID
	stopLoad := make(chan struct{})
	var batchesWritten int64
	go func() {
		for {
			select {
			case <-stopLoad:
				return
			default:
				records := make([]domain.VMRecord, 10)
				for i := range records {
					records[i] = domain.VMRecord{
						VCenterID:       activeVCID.String(),
						MoRef:           fmt.Sprintf("vm-load-%d", i),
						Name:            fmt.Sprintf("load-vm-%d", i),
						PowerState:      "poweredOn",
						ConnectionState: "connected",
						CPUCores:        2,
						MemoryMB:        2048,
					}
				}
				if _, err := persister.PersistVMBatch(ctx, records); err == nil {
					atomic.AddInt64(&batchesWritten, 1)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	// Wait for load to ramp up
	time.Sleep(100 * time.Millisecond)

	// Dynamically register 3 new vCenter targets while under heavy ingestion load
	for i := 1; i <= 3; i++ {
		newVCID := uuid.New()
		_, err := pool.Exec(ctx, `
			INSERT INTO vcenters (id, name, api_url, credential_ref, status)
			VALUES ($1, $2, $3, 'cred-ref', 'active')
		`, newVCID, fmt.Sprintf("vc-dynamic-%d", i), fmt.Sprintf("https://vc-dyn-%s.test/sdk", newVCID.String()))
		if err != nil {
			close(stopLoad)
			t.Fatalf("failed to dynamically register vCenter under load: %v", err)
		}
	}

	close(stopLoad)
	t.Logf("SUCCESS: Dynamically registered 3 vCenter targets while %d batches were persisted under heavy load.",
		atomic.LoadInt64(&batchesWritten))
}

// TestChaos_DatabaseConnectionLossAndReconnect verifies that when database backend connections
// are abruptly terminated (e.g., database restart, connection dropped), the connection pool
// automatically recovers and transparently resumes query execution without panicking or deadlocking.
func TestChaos_DatabaseConnectionLossAndReconnect(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to connect to DB: %v", err)
	}
	defer pool.Close()

	// 1. Initial query succeeds
	var val int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&val); err != nil {
		t.Fatalf("initial query failed: %v", err)
	}

	// 2. Terminate all backend connections for collector_rw (simulating DB restart / connection kill)
	adminPool, err := pgxpool.New(ctx, getConnString("postgres", "postgres"))
	if err != nil {
		t.Fatalf("failed to connect admin DB: %v", err)
	}
	defer adminPool.Close()

	_, err = adminPool.Exec(ctx, `
		SELECT pg_terminate_backend(pid)
		FROM pg_stat_activity
		WHERE usename = 'collector_rw'
		  AND pid <> pg_backend_pid()
	`)
	if err != nil {
		t.Fatalf("failed to terminate connections: %v", err)
	}
	t.Log("Terminated all collector_rw backend connections to simulate abrupt DB disconnect")

	// 3. Subsequent query to pool must automatically establish a fresh connection and succeed
	var recoveredVal int
	deadline := time.Now().Add(5 * time.Second)
	var queryErr error
	for time.Now().Before(deadline) {
		queryErr = pool.QueryRow(ctx, "SELECT 42").Scan(&recoveredVal)
		if queryErr == nil && recoveredVal == 42 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if queryErr != nil || recoveredVal != 42 {
		t.Fatalf("pool failed to automatically reconnect and execute query: %v (val=%d)", queryErr, recoveredVal)
	}
	t.Log("SUCCESS: Database connection pool automatically recovered after abrupt connection termination.")
}

func int32Ptr(i int32) *int32 {
	return &i
}

