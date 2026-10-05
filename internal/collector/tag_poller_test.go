package collector_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/vmware/govmomi/vim25/types"
)

type pollerMockTagFetcher struct {
	mu sync.Mutex

	tagsToReturn map[string][]domain.TagRecord
	injectedErr  error
	closed       bool
}

func (m *pollerMockTagFetcher) FetchTagsForObjects(ctx context.Context, refs []types.ManagedObjectReference) (map[string][]domain.TagRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.injectedErr != nil {
		return nil, m.injectedErr
	}
	return m.tagsToReturn, nil
}

func (m *pollerMockTagFetcher) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

type mockTagPersister struct {
	mu sync.Mutex

	activeVMs      []string
	existingTags   map[string][]domain.TagRecord
	updatedVMTags  map[string][]domain.TagRecord
	tagsState      string
	tagsLastSynced *time.Time
}

func (m *mockTagPersister) ListActiveVMMorefs(ctx context.Context, vcenterID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeVMs, nil
}

func (m *mockTagPersister) GetVMTags(ctx context.Context, vcenterID string) (map[string][]domain.TagRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make(map[string][]domain.TagRecord)
	for k, v := range m.existingTags {
		res[k] = v
	}
	return res, nil
}

func (m *mockTagPersister) UpdateVMTags(ctx context.Context, vcenterID string, moref string, tags []domain.TagRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updatedVMTags == nil {
		m.updatedVMTags = make(map[string][]domain.TagRecord)
	}
	m.updatedVMTags[moref] = tags
	m.existingTags[moref] = tags
	return nil
}

func (m *mockTagPersister) UpdateTagsState(ctx context.Context, vcenterID string, state string, lastSyncedAt *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tagsState = state
	m.tagsLastSynced = lastSyncedAt
	return nil
}

func TestTagPoller_DiffCheckOnlyUpdatesChangedVMs(t *testing.T) {
	metrics := telemetry.NewMetrics("test", "0.1.0")

	// Setup VMs: vm-1 has new tag, vm-2 has unchanged tags, vm-3 has no tags
	activeVMs := []string{"vm-1", "vm-2", "vm-3"}

	existingTags := map[string][]domain.TagRecord{
		"vm-1": {{TagID: "tag-1", CategoryName: "Env", TagName: "Dev"}},
		"vm-2": {{TagID: "tag-2", CategoryName: "Tier", TagName: "Web"}},
	}

	fetchedTags := map[string][]domain.TagRecord{
		"vm-1": {
			{TagID: "tag-1", CategoryName: "Env", TagName: "Dev"},
			{TagID: "tag-prod", CategoryName: "Env", TagName: "Prod"}, // Changed!
		},
		"vm-2": {
			{TagID: "tag-2", CategoryName: "Tier", TagName: "Web"}, // Unchanged
		},
		// vm-3 has no tags
	}

	fetcher := &pollerMockTagFetcher{tagsToReturn: fetchedTags}
	persister := &mockTagPersister{
		activeVMs:    activeVMs,
		existingTags: existingTags,
	}

	cfg := collector.TagPollerConfig{
		VCenterID:    "vc-tag-diff",
		PollInterval: 50 * time.Millisecond,
		CallTimeout:  1 * time.Second,
	}

	poller := collector.NewTagPoller(fetcher, persister, cfg, metrics, nil, nil)

	ctx := context.Background()
	err := poller.SyncOnce(ctx)
	if err != nil {
		t.Fatalf("SyncOnce failed: %v", err)
	}

	persister.mu.Lock()
	updated := persister.updatedVMTags
	state := persister.tagsState
	lastSynced := persister.tagsLastSynced
	persister.mu.Unlock()

	// Broadcom Invariant: ONLY vm-1 should be rewritten!
	if len(updated) != 1 {
		t.Fatalf("expected exactly 1 VM to be updated, got %d: %+v", len(updated), updated)
	}
	if _, ok := updated["vm-1"]; !ok {
		t.Errorf("expected vm-1 to be updated, got %+v", updated)
	}
	if _, ok := updated["vm-2"]; ok {
		t.Errorf("vm-2 tags were unchanged; MUST NOT be updated in database")
	}

	if state != "ok" {
		t.Errorf("expected tags_state 'ok', got %q", state)
	}
	if lastSynced == nil {
		t.Error("expected tags_last_synced to be set, got nil")
	}

	// Verify metrics
	count := testutil.ToFloat64(metrics.TagsPersisted.WithLabelValues("vc-tag-diff"))
	if count != 2 {
		t.Errorf("expected 2 tags persisted for vm-1, got %f", count)
	}
}

func TestTagPoller_ServiceOutagePreservesExistingTags(t *testing.T) {
	metrics := telemetry.NewMetrics("test", "0.1.0")

	activeVMs := []string{"vm-1"}
	existingTags := map[string][]domain.TagRecord{
		"vm-1": {{TagID: "tag-1", CategoryName: "Env", TagName: "Prod"}},
	}

	fetcher := &pollerMockTagFetcher{
		injectedErr: collector.ErrTaggingUnavailable,
	}
	persister := &mockTagPersister{
		activeVMs:    activeVMs,
		existingTags: existingTags,
	}

	cfg := collector.TagPollerConfig{
		VCenterID:    "vc-tag-outage",
		PollInterval: 50 * time.Millisecond,
		CallTimeout:  1 * time.Second,
	}

	poller := collector.NewTagPoller(fetcher, persister, cfg, metrics, nil, nil)

	ctx := context.Background()
	err := poller.SyncOnce(ctx)
	if !errors.Is(err, collector.ErrTaggingUnavailable) {
		t.Fatalf("expected ErrTaggingUnavailable, got: %v", err)
	}

	persister.mu.Lock()
	updated := persister.updatedVMTags
	state := persister.tagsState
	existing := persister.existingTags
	persister.mu.Unlock()

	// Invariant: Failed fetch NEVER deletes or overwrites existing tags
	if len(updated) != 0 {
		t.Errorf("expected 0 updates during tag outage, got: %+v", updated)
	}
	if len(existing["vm-1"]) != 1 {
		t.Errorf("expected vm-1 tags to remain intact, got: %+v", existing["vm-1"])
	}

	// State marked unavailable (since it never succeeded yet)
	if state != "unavailable" {
		t.Errorf("expected tags_state 'unavailable', got %q", state)
	}

	failures := testutil.ToFloat64(metrics.TagFetchFailures.WithLabelValues("vc-tag-outage"))
	if failures != 1 {
		t.Errorf("expected 1 tag fetch failure, got %f", failures)
	}
}

func TestTagPoller_CleanupRegistryTeardown(t *testing.T) {
	metrics := telemetry.NewMetrics("test", "0.1.0")
	reg := collector.NewCleanupRegistry(nil, 500*time.Millisecond)

	fetcher := &pollerMockTagFetcher{
		tagsToReturn: map[string][]domain.TagRecord{},
	}
	persister := &mockTagPersister{
		activeVMs: []string{},
	}

	cfg := collector.TagPollerConfig{
		VCenterID:    "vc-tag-cleanup",
		PollInterval: 20 * time.Millisecond,
		CallTimeout:  1 * time.Second,
	}

	poller := collector.NewTagPoller(fetcher, persister, cfg, metrics, nil, reg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = poller.Run(ctx)
	}()

	// Wait briefly for poller to register and execute initial cycle
	time.Sleep(50 * time.Millisecond)

	// Teardown registry (Stage 1 stops poller, Stage 6 logs out REST session)
	err := reg.Teardown(context.Background())
	if err != nil {
		t.Fatalf("Teardown failed: %v", err)
	}

	wg.Wait()

	fetcher.mu.Lock()
	isClosed := fetcher.closed
	fetcher.mu.Unlock()

	if !isClosed {
		t.Error("expected TagFetcher.Close() to be called during CleanupRegistry Stage 6")
	}
}
