package collector

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/domain"
	"github.com/vmware/govmomi/vim25/types"
)

type mockTagFetcher struct {
	mu        sync.Mutex
	tags      map[string][]domain.TagRecord
	err       error
	callCount int
}

func (m *mockTagFetcher) FetchTagsForObjects(ctx context.Context, refs []types.ManagedObjectReference) (map[string][]domain.TagRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
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

func (m *mockTagFetcher) Close(ctx context.Context) error {
	return nil
}

type mockTagPersister struct {
	mu           sync.Mutex
	activeMorefs []string
	tags         map[string][]domain.TagRecord
	tagsState    string
	lastSync     *time.Time
	updatesCount int
}

func (m *mockTagPersister) ListActiveVMMorefs(ctx context.Context, vcenterID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeMorefs, nil
}

func (m *mockTagPersister) GetVMTags(ctx context.Context, vcenterID string) (map[string][]domain.TagRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	copyMap := make(map[string][]domain.TagRecord)
	for k, v := range m.tags {
		copyMap[k] = append([]domain.TagRecord(nil), v...)
	}
	return copyMap, nil
}

func (m *mockTagPersister) UpdateVMTags(ctx context.Context, vcenterID string, moref string, tags []domain.TagRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updatesCount++
	m.tags[moref] = tags
	return nil
}

func (m *mockTagPersister) UpdateTagsState(ctx context.Context, vcenterID string, state string, lastSyncedAt *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tagsState = state
	m.lastSync = lastSyncedAt
	return nil
}

func TestTagReconciler_DriftDetectionAndConvergence(t *testing.T) {
	ctx := context.Background()

	persister := &mockTagPersister{
		activeMorefs: []string{"vm-1", "vm-2"},
		tags: map[string][]domain.TagRecord{
			"vm-1": {
				{TagID: "t-1", CategoryName: "env", TagName: "prod"},
				{TagID: "t-2", CategoryName: "tier", TagName: "web"},
			},
			"vm-2": {
				{TagID: "t-3", CategoryName: "owner", TagName: "finance"},
			},
		},
	}

	// vCenter has drift for vm-1: 'web' removed, 'api' added. vm-2 is unchanged.
	fetcher := &mockTagFetcher{
		tags: map[string][]domain.TagRecord{
			"vm-1": {
				{TagID: "t-1", CategoryName: "env", TagName: "prod"},
				{TagID: "t-4", CategoryName: "tier", TagName: "api"},
			},
			"vm-2": {
				{TagID: "t-3", CategoryName: "owner", TagName: "finance"},
			},
		},
	}

	cfg := DefaultTagReconcilerConfig("vc-1")
	cfg.DriftAlertThreshold = 0.50

	reconciler := NewTagReconciler(fetcher, persister, cfg, nil, nil)
	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if report.VMsChecked != 2 {
		t.Fatalf("expected 2 VMs checked, got %d", report.VMsChecked)
	}
	if report.VMsDrifted != 1 {
		t.Fatalf("expected 1 VM drifted, got %d", report.VMsDrifted)
	}
	if report.TagsAdded != 1 {
		t.Fatalf("expected 1 tag added, got %d", report.TagsAdded)
	}
	if report.TagsRemoved != 1 {
		t.Fatalf("expected 1 tag removed, got %d", report.TagsRemoved)
	}
	if report.TagsUnchanged != 2 { // 'env:prod' on vm-1 and 'owner:finance' on vm-2
		t.Fatalf("expected 2 tags unchanged, got %d", report.TagsUnchanged)
	}

	// Verify DB state converged
	vm1Tags := persister.tags["vm-1"]
	if len(vm1Tags) != 2 || vm1Tags[0].TagName != "prod" || vm1Tags[1].TagName != "api" {
		t.Fatalf("vm-1 tags failed to converge properly: %+v", vm1Tags)
	}
	if persister.updatesCount != 1 {
		t.Fatalf("expected exactly 1 DB update call, got %d", persister.updatesCount)
	}
}

func TestTagReconciler_NonDestructiveOnFetchFailure(t *testing.T) {
	ctx := context.Background()

	initialTags := map[string][]domain.TagRecord{
		"vm-1": {
			{TagID: "t-1", CategoryName: "env", TagName: "prod"},
		},
	}

	persister := &mockTagPersister{
		activeMorefs: []string{"vm-1"},
		tags:         initialTags,
	}

	fetcher := &mockTagFetcher{
		err: errors.New("connection timeout to vapi/rest"),
	}

	cfg := DefaultTagReconcilerConfig("vc-2")
	reconciler := NewTagReconciler(fetcher, persister, cfg, nil, nil)

	_, err := reconciler.Reconcile(ctx)
	if err == nil {
		t.Fatalf("expected error on fetch failure")
	}

	// CRITICAL INVARIANT: NEVER delete existing tags on fetch failure
	if len(persister.tags["vm-1"]) != 1 {
		t.Fatalf("invariant violated: tags were modified or deleted on fetch error!")
	}
	if persister.updatesCount != 0 {
		t.Fatalf("invariant violated: UpdateVMTags was called during fetch failure")
	}
}

func TestTagReconciler_DriftAlertRaised(t *testing.T) {
	ctx := context.Background()

	persister := &mockTagPersister{
		activeMorefs: []string{"vm-1"},
		tags: map[string][]domain.TagRecord{
			"vm-1": {
				{TagID: "t-1", CategoryName: "a", TagName: "1"},
			},
		},
	}

	// 100% drift: old tag removed, new tag added
	fetcher := &mockTagFetcher{
		tags: map[string][]domain.TagRecord{
			"vm-1": {
				{TagID: "t-2", CategoryName: "b", TagName: "2"},
			},
		},
	}

	cfg := DefaultTagReconcilerConfig("vc-3")
	cfg.DriftAlertThreshold = 0.20 // 20% threshold

	reconciler := NewTagReconciler(fetcher, persister, cfg, nil, nil)
	report, err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if !report.AlertRaised {
		t.Fatalf("expected AlertRaised to be true when drift exceeds threshold")
	}
	if report.DriftRatio <= 0.20 {
		t.Fatalf("expected drift ratio > 0.20, got %f", report.DriftRatio)
	}
}
