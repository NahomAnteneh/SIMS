//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/persistence"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/infrastructure/sims/internal/vsphere"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/event"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/types"
)

// TestEvents_DeduplicationAndFieldOwnership proves:
// 1. Ingesting an event is strictly idempotent: duplicate delivery does not create duplicate rows in inventory_events.
// 2. Lifecycle creation events set VM created_at and created_by provenance.
// 3. VmRemovedEvent triggers event-driven soft-deletion (deleted_at = NOW()).
func TestEvents_DeduplicationAndFieldOwnership(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-events-dedup")
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{}, nil)

	// 1. Pre-insert an active VM
	vmMoref := "vm-event-101"
	instanceUUID := uuid.New().String()
	initialCreated := time.Now().Add(-1 * time.Hour).Truncate(time.Second)

	_, err := pool.Exec(ctx, `
		INSERT INTO virtual_machines (
			vcenter_id, moref, instance_uuid, name, power_state,
			created_at, created_by, updated_at
		) VALUES (
			$1, $2, $3, $4, 'poweredOn',
			$5, '', NOW()
		)
	`, vcenterID, vmMoref, instanceUUID, "event-vm-1", initialCreated)
	if err != nil {
		t.Fatalf("failed to insert initial VM: %v", err)
	}

	// 2. Persist VmCreatedEvent
	eventTime := time.Now().Add(-20 * time.Minute).Truncate(time.Second)
	evRecord := collector.EventRecord{
		EventKey:    5001,
		ChainID:     5001,
		EventType:   "VmCreatedEvent",
		EntityMoref: vmMoref,
		EntityName:  "event-vm-1",
		EntityType:  "VirtualMachine",
		UserName:    "CORP\\alice",
		Message:     "VirtualMachine event-vm-1 created",
		CreatedTime: eventTime,
		Payload:     []byte(`{"note":"created-via-test"}`),
	}

	err = persister.PersistEvent(ctx, vcenterID, evRecord)
	if err != nil {
		t.Fatalf("failed to persist VmCreatedEvent: %v", err)
	}

	// Verify inventory_events row exists
	var count int
	var storedUser, storedType string
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(MAX(user_name), ''), COALESCE(MAX(event_type), '')
		FROM inventory_events
		WHERE vcenter_id = $1 AND event_key = 5001
	`, vcenterID).Scan(&count, &storedUser, &storedType)
	if err != nil {
		t.Fatalf("failed to query inventory_events: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row in inventory_events, got %d", count)
	}
	if storedUser != "CORP\\alice" {
		t.Errorf("expected user CORP\\alice, got %q", storedUser)
	}
	if storedType != "VmCreatedEvent" {
		t.Errorf("expected event_type VmCreatedEvent, got %q", storedType)
	}

	// Verify VM provenance updated
	var vmCreatedBy string
	var vmCreatedAt time.Time
	err = pool.QueryRow(ctx, `
		SELECT created_by, created_at
		FROM virtual_machines
		WHERE vcenter_id = $1 AND moref = $2
	`, vcenterID, vmMoref).Scan(&vmCreatedBy, &vmCreatedAt)
	if err != nil {
		t.Fatalf("failed to query virtual_machines: %v", err)
	}
	if vmCreatedBy != "CORP\\alice" {
		t.Errorf("expected VM created_by to be 'CORP\\alice', got %q", vmCreatedBy)
	}
	if !vmCreatedAt.Equal(eventTime) {
		t.Errorf("expected VM created_at to be %v, got %v", eventTime, vmCreatedAt)
	}

	// 3. Test Deduplication: Re-deliver the exact same event
	err = persister.PersistEvent(ctx, vcenterID, evRecord)
	if err != nil {
		t.Fatalf("replaying event returned unexpected error: %v", err)
	}

	// Confirm row count in inventory_events is STILL exactly 1
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM inventory_events
		WHERE vcenter_id = $1 AND event_key = 5001
	`, vcenterID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query event count after replay: %v", err)
	}
	if count != 1 {
		t.Errorf("expected deduplication to keep event count at 1, got %d", count)
	}

	// 4. Test VmRemovedEvent soft-deletes the VM
	removedRecord := collector.EventRecord{
		EventKey:    5002,
		ChainID:     5002,
		EventType:   "VmRemovedEvent",
		EntityMoref: vmMoref,
		EntityName:  "event-vm-1",
		EntityType:  "VirtualMachine",
		UserName:    "CORP\\bob",
		Message:     "VirtualMachine event-vm-1 deleted",
		CreatedTime: time.Now(),
		Payload:     []byte(`{}`),
	}

	err = persister.PersistEvent(ctx, vcenterID, removedRecord)
	if err != nil {
		t.Fatalf("failed to persist VmRemovedEvent: %v", err)
	}

	var deletedAt *time.Time
	err = pool.QueryRow(ctx, `
		SELECT deleted_at FROM virtual_machines
		WHERE vcenter_id = $1 AND moref = $2
	`, vcenterID, vmMoref).Scan(&deletedAt)
	if err != nil {
		t.Fatalf("failed to query deleted_at: %v", err)
	}
	if deletedAt == nil {
		t.Error("expected VM deleted_at to be set after VmRemovedEvent, got nil")
	}
}

// TestEvents_WatermarkPersistence proves collector_state stores and updates event watermark.
func TestEvents_WatermarkPersistence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-events-watermark")
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{}, nil)

	// Initial check: watermark is empty
	key, ts, err := persister.GetEventWatermark(ctx, vcenterID)
	if err != nil {
		t.Fatalf("initial GetEventWatermark failed: %v", err)
	}
	if key != 0 || ts != nil {
		t.Errorf("expected (0, nil), got (%d, %v)", key, ts)
	}

	// Save watermark 1
	t1 := time.Now().Add(-5 * time.Minute).Truncate(time.Second)
	err = persister.SaveEventWatermark(ctx, vcenterID, 8801, t1)
	if err != nil {
		t.Fatalf("failed to save watermark: %v", err)
	}

	key, ts, err = persister.GetEventWatermark(ctx, vcenterID)
	if err != nil {
		t.Fatalf("GetEventWatermark failed: %v", err)
	}
	if key != 8801 {
		t.Errorf("expected key 8801, got %d", key)
	}
	if ts == nil || !ts.Equal(t1) {
		t.Errorf("expected time %v, got %v", t1, ts)
	}

	// Update watermark 2
	t2 := time.Now().Truncate(time.Second)
	err = persister.SaveEventWatermark(ctx, vcenterID, 8850, t2)
	if err != nil {
		t.Fatalf("failed to update watermark: %v", err)
	}

	key, ts, err = persister.GetEventWatermark(ctx, vcenterID)
	if err != nil {
		t.Fatalf("GetEventWatermark after update failed: %v", err)
	}
	if key != 8850 {
		t.Errorf("expected key 8850, got %d", key)
	}
	if ts == nil || !ts.Equal(t2) {
		t.Errorf("expected time %v, got %v", t2, ts)
	}
}

// TestEvents_VCSimStreamerIntegration tests EventStreamer end-to-end against real vcsim:
// 1. Connects to vcsim configured as vSphere 8.0.3
// 2. Posts an event to vcsim EventManager
// 3. EventStreamer consumes event and writes it to PostgreSQL
// 4. Verifies watermark is updated in collector_state
// 5. Restarts EventStreamer with overlap window and verifies deduplication (no duplicate rows)
// 6. Verifies CleanupRegistry destroys the server-side EventHistoryCollector
func TestEvents_VCSimStreamerIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-events-vcsim")
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{}, nil)
	metrics := telemetry.NewMetrics("test", "0.1.0")

	// 1. Initialize vcsim
	model := simulator.VPX()
	vsphere.ConfigureTestVPX(model)
	defer model.Remove()

	if err := model.Create(); err != nil {
		t.Fatalf("failed to create vcsim model: %v", err)
	}

	server := model.Service.NewServer()
	defer server.Close()

	client, err := govmomi.NewClient(ctx, server.URL, true)
	if err != nil {
		t.Fatalf("failed to connect to vcsim: %v", err)
	}

	eventMgr := vsphere.NewGovmomiEventManager(client)
	govmomiEM := event.NewManager(client.Client)

	// Post an event to vcsim before or during streaming
	postEvent := &types.VmCreatedEvent{
		VmEvent: types.VmEvent{
			Event: types.Event{
				UserName:             "vcsim\\admin",
				FullFormattedMessage: "vcsim test VM created",
				Host: &types.HostEventArgument{
					EntityEventArgument: types.EntityEventArgument{Name: "DC0_H0"},
					Host:                types.ManagedObjectReference{Type: "HostSystem", Value: "host-0"},
				},
				Datacenter: &types.DatacenterEventArgument{
					EntityEventArgument: types.EntityEventArgument{Name: "DC0"},
					Datacenter:          types.ManagedObjectReference{Type: "Datacenter", Value: "dc-0"},
				},
				Vm: &types.VmEventArgument{
					EntityEventArgument: types.EntityEventArgument{Name: "vcsim-vm-1"},
					Vm:                  types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-vcsim-1"},
				},
			},
		},
	}
	err = govmomiEM.PostEvent(ctx, postEvent)
	if err != nil {
		t.Fatalf("failed to post event to vcsim: %v", err)
	}

	// 2. Start EventStreamer
	reg := collector.NewCleanupRegistry(nil, 5*time.Second)
	cfg := collector.EventStreamerConfig{
		VCenterID:     vcenterID,
		PageSize:      10,
		PollInterval:  20 * time.Millisecond,
		OverlapWindow: 5 * time.Minute,
		CallDeadline:  2 * time.Second,
	}

	streamer := collector.NewEventStreamer(eventMgr, persister, cfg, metrics, nil, reg)

	streamCtx, cancelStream := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = streamer.Run(streamCtx)
	}()

	// Wait for event to be persisted and watermark to be saved
	var watermarkKey int64
	var watermarkTime *time.Time
	for i := 0; i < 50; i++ {
		watermarkKey, watermarkTime, err = persister.GetEventWatermark(ctx, vcenterID)
		if err == nil && watermarkKey > 0 && watermarkTime != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if watermarkKey == 0 || watermarkTime == nil {
		t.Fatalf("expected valid watermark after streaming, got key=%d, time=%v (err: %v)", watermarkKey, watermarkTime, err)
	}

	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM inventory_events WHERE vcenter_id = $1", vcenterID).Scan(&count)
	if err != nil || count < 1 {
		t.Fatalf("expected at least 1 event to be ingested into PostgreSQL, got %d (err: %v)", count, err)
	}

	// Stop the first streamer
	cancelStream()
	wg.Wait()

	// 3. Restart EventStreamer to test replay & deduplication
	streamCtx2, cancelStream2 := context.WithCancel(ctx)
	reg2 := collector.NewCleanupRegistry(nil, 5*time.Second)
	streamer2 := collector.NewEventStreamer(eventMgr, persister, cfg, metrics, nil, reg2)

	var wg2 sync.WaitGroup
	wg2.Add(1)
	go func() {
		defer wg2.Done()
		_ = streamer2.Run(streamCtx2)
	}()

	// Give streamer time to query overlap window and process
	time.Sleep(100 * time.Millisecond)

	cancelStream2()
	wg2.Wait()

	// Verify no duplicate rows were inserted
	var countAfterReplay int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM inventory_events WHERE vcenter_id = $1
	`, vcenterID).Scan(&countAfterReplay)
	if err != nil {
		t.Fatalf("failed to query count after replay: %v", err)
	}
	if countAfterReplay != count {
		t.Errorf("expected count to remain %d after replay, got %d", count, countAfterReplay)
	}

	// 4. Test CleanupRegistry destroys collector on vcsim
	// Create another collector to test explicit registry teardown
	collectorInstance, err := eventMgr.CreateCollectorForEvents(ctx, types.EventFilterSpec{})
	if err != nil {
		t.Fatalf("failed to create EventCollector for teardown test: %v", err)
	}
	collectorRef := collectorInstance.Reference()

	teardownReg := collector.NewCleanupRegistry(nil, 5*time.Second)
	teardownReg.RegisterEventCollector(collectorInstance)

	err = teardownReg.Teardown(ctx)
	if err != nil {
		t.Fatalf("failed to teardown registry: %v", err)
	}

	// Confirm ManagedObjectNotFound on server
	_, probeErr := collectorInstance.LatestPage(ctx)
	if probeErr == nil {
		t.Errorf("expected EventHistoryCollector %s to be destroyed, but it still exists", collectorRef.Value)
	} else if !isManagedObjectNotFound(probeErr) {
		t.Errorf("expected ManagedObjectNotFound, got: %v", probeErr)
	}
}

// Suppress unused imports
var _ = json.Marshal
var _ = fmt.Sprintf
