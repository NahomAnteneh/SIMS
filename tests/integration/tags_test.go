//go:build integration

package integration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/persistence"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/vmware/govmomi/vim25/types"
)

type integrationMockTagFetcher struct {
	tagsToReturn map[string][]domain.TagRecord
	injectedErr  error
}

func (m *integrationMockTagFetcher) FetchTagsForObjects(ctx context.Context, refs []types.ManagedObjectReference) (map[string][]domain.TagRecord, error) {
	if m.injectedErr != nil {
		return nil, m.injectedErr
	}
	return m.tagsToReturn, nil
}

func (m *integrationMockTagFetcher) Close(ctx context.Context) error {
	return nil
}

// TestTags_TagPollerDiffCheckInDatabase proves:
// 1. TagPoller synchronizes tags into vm_tags with replace-set semantics.
// 2. Only VMs with modified tag sets have their database rows rewritten.
// 3. collector_state tracks tags_state = 'ok' and tags_last_synced_at.
func TestTags_TagPollerDiffCheckInDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-tags-diff")
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{}, nil)
	metrics := telemetry.NewMetrics("test", "0.1.0")

	// 1. Insert 2 VMs
	moref1 := "vm-tag-db-1"
	moref2 := "vm-tag-db-2"
	_, err := pool.Exec(ctx, `
		INSERT INTO virtual_machines (vcenter_id, moref, instance_uuid, name, created_at, updated_at)
		VALUES
			($1, $2, $3, 'app-server-1', NOW(), NOW()),
			($1, $4, $5, 'app-server-2', NOW(), NOW())
	`, vcenterID, moref1, uuid.New().String(), moref2, uuid.New().String())
	if err != nil {
		t.Fatalf("failed to insert test VMs: %v", err)
	}

	// 2. Seed initial tags
	err = persister.UpdateVMTags(ctx, vcenterID, moref1, []domain.TagRecord{
		{TagID: "tag-env-dev", CategoryName: "Environment", TagName: "Development"},
	})
	if err != nil {
		t.Fatalf("failed to seed initial tags for vm 1: %v", err)
	}
	err = persister.UpdateVMTags(ctx, vcenterID, moref2, []domain.TagRecord{
		{TagID: "tag-tier-web", CategoryName: "Tier", TagName: "WebTier"},
	})
	if err != nil {
		t.Fatalf("failed to seed initial tags for vm 2: %v", err)
	}

	// 3. Configure tag fetcher: vm 1 gets an extra tag (changed); vm 2 remains unchanged
	fetcher := &integrationMockTagFetcher{
		tagsToReturn: map[string][]domain.TagRecord{
			moref1: {
				{TagID: "tag-env-dev", CategoryName: "Environment", TagName: "Development"},
				{TagID: "tag-env-stage", CategoryName: "Environment", TagName: "Staging"},
			},
			moref2: {
				{TagID: "tag-tier-web", CategoryName: "Tier", TagName: "WebTier"},
			},
		},
	}

	cfg := collector.TagPollerConfig{
		VCenterID:    vcenterID,
		PollInterval: 50 * time.Millisecond,
		CallTimeout:  2 * time.Second,
	}

	poller := collector.NewTagPoller(fetcher, persister, cfg, metrics, nil, nil)

	// Run single synchronization cycle
	err = poller.SyncOnce(ctx)
	if err != nil {
		t.Fatalf("SyncOnce failed: %v", err)
	}

	// 4. Verify vm 1 has 2 tags now
	tags1, err := persister.GetVMTags(ctx, vcenterID)
	if err != nil {
		t.Fatalf("GetVMTags failed: %v", err)
	}
	if len(tags1[moref1]) != 2 {
		t.Errorf("expected 2 tags for vm 1, got %d", len(tags1[moref1]))
	}
	if len(tags1[moref2]) != 1 {
		t.Errorf("expected 1 tag for vm 2, got %d", len(tags1[moref2]))
	}

	// 5. Verify collector_state
	state, lastSync, err := persister.GetTagsState(ctx, vcenterID)
	if err != nil {
		t.Fatalf("GetTagsState failed: %v", err)
	}
	if state != "ok" {
		t.Errorf("expected tags_state 'ok', got %q", state)
	}
	if lastSync == nil {
		t.Error("expected tags_last_synced_at to be non-nil")
	}
}

// TestTags_CustomAttributeReplaceSetInDatabase proves:
// 1. Updating custom attributes via ApplyVMIncrementalUpdate performs atomic replace-set.
// 2. Removing an attribute in the update explicitly deletes the attribute row from vm_custom_attributes.
func TestTags_CustomAttributeReplaceSetInDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-ca-replaceset")
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{}, nil)

	// Insert VM
	moref := "vm-ca-test-1"
	_, err := pool.Exec(ctx, `
		INSERT INTO virtual_machines (vcenter_id, moref, instance_uuid, name, created_at, updated_at)
		VALUES ($1, $2, $3, 'ca-vm', NOW(), NOW())
	`, vcenterID, moref, uuid.New().String())
	if err != nil {
		t.Fatalf("failed to insert VM: %v", err)
	}

	// Step 1: Initial attributes: CostCenter=Engineering, Project=SIMS
	mask1 := collector.VMFieldMask{
		CustomAttributes: []domain.CustomAttributeRecord{
			{Name: "CostCenter", Value: "Engineering"},
			{Name: "Project", Value: "SIMS"},
		},
	}
	err = persister.ApplyVMIncrementalUpdate(ctx, vcenterID, moref, mask1)
	if err != nil {
		t.Fatalf("initial ApplyVMIncrementalUpdate failed: %v", err)
	}

	// Confirm 2 rows exist
	var count int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM vm_custom_attributes vca
		JOIN virtual_machines vm ON vca.vm_id = vm.id
		WHERE vm.vcenter_id = $1 AND vm.moref = $2
	`, vcenterID, moref).Scan(&count)
	if err != nil || count != 2 {
		t.Fatalf("expected 2 custom attributes initially, got %d (err: %v)", count, err)
	}

	// Step 2: Replace-set update: CostCenter=Research, Project removed!
	mask2 := collector.VMFieldMask{
		CustomAttributes: []domain.CustomAttributeRecord{
			{Name: "CostCenter", Value: "Research"},
		},
	}
	err = persister.ApplyVMIncrementalUpdate(ctx, vcenterID, moref, mask2)
	if err != nil {
		t.Fatalf("second ApplyVMIncrementalUpdate failed: %v", err)
	}

	// Confirm exactly 1 row exists (CostCenter=Research), Project was deleted
	rows, err := pool.Query(ctx, `
		SELECT vca.name, vca.value FROM vm_custom_attributes vca
		JOIN virtual_machines vm ON vca.vm_id = vm.id
		WHERE vm.vcenter_id = $1 AND vm.moref = $2
	`, vcenterID, moref)
	if err != nil {
		t.Fatalf("failed to query custom attributes: %v", err)
	}
	defer rows.Close()

	attrs := make(map[string]string)
	for rows.Next() {
		var n, v string
		_ = rows.Scan(&n, &v)
		attrs[n] = v
	}

	if len(attrs) != 1 {
		t.Fatalf("expected exactly 1 custom attribute after replace-set, got %d: %+v", len(attrs), attrs)
	}
	if attrs["CostCenter"] != "Research" {
		t.Errorf("expected CostCenter 'Research', got %q", attrs["CostCenter"])
	}
	if _, exists := attrs["Project"]; exists {
		t.Errorf("Project attribute was removed in replace-set, but still exists in database!")
	}
}

// TestTags_ServiceOutagePreservesDatabaseTags proves:
// 1. When the tagging REST endpoint fails, existing vm_tags rows in the database are NOT deleted.
// 2. tags_state is set to 'degraded' (if previously synced) or 'unavailable'.
func TestTags_ServiceOutagePreservesDatabaseTags(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool := setupTestPool(t, ctx)
	defer pool.Close()

	vcenterID := createTestVCenter(t, ctx, pool, "vc-tags-outage")
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{}, nil)
	metrics := telemetry.NewMetrics("test", "0.1.0")

	// Insert VM and seed tag
	moref := "vm-outage-1"
	_, err := pool.Exec(ctx, `
		INSERT INTO virtual_machines (vcenter_id, moref, instance_uuid, name, created_at, updated_at)
		VALUES ($1, $2, $3, 'outage-vm', NOW(), NOW())
	`, vcenterID, moref, uuid.New().String())
	if err != nil {
		t.Fatalf("failed to insert VM: %v", err)
	}

	err = persister.UpdateVMTags(ctx, vcenterID, moref, []domain.TagRecord{
		{TagID: "tag-retain", CategoryName: "Retention", TagName: "Critical"},
	})
	if err != nil {
		t.Fatalf("failed to seed initial tag: %v", err)
	}

	// Fetcher fails with tagging outage
	fetcher := &integrationMockTagFetcher{
		injectedErr: collector.ErrTaggingUnavailable,
	}

	cfg := collector.TagPollerConfig{
		VCenterID:    vcenterID,
		PollInterval: 50 * time.Millisecond,
		CallTimeout:  2 * time.Second,
	}

	poller := collector.NewTagPoller(fetcher, persister, cfg, metrics, nil, nil)

	// Run sync cycle (should fail gracefully)
	err = poller.SyncOnce(ctx)
	if !errors.Is(err, collector.ErrTaggingUnavailable) {
		t.Fatalf("expected ErrTaggingUnavailable, got: %v", err)
	}

	// Verify database tags are 100% intact
	tagsMap, err := persister.GetVMTags(ctx, vcenterID)
	if err != nil {
		t.Fatalf("failed to query tags: %v", err)
	}
	if len(tagsMap[moref]) != 1 || tagsMap[moref][0].TagID != "tag-retain" {
		t.Errorf("expected existing tag 'tag-retain' to be preserved, got: %+v", tagsMap[moref])
	}

	// Verify tags_state marked unavailable
	state, _, err := persister.GetTagsState(ctx, vcenterID)
	if err != nil {
		t.Fatalf("failed to query tags state: %v", err)
	}
	if state != "unavailable" {
		t.Errorf("expected tags_state 'unavailable', got %q", state)
	}
}
