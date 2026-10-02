package collector_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/domain"
)

func TestCompactBatch_SingleUpdate(t *testing.T) {
	records := []domain.VMRecord{
		{
			InstanceUUID: "uuid-1",
			Name:         "vm-single",
			PowerState:   "poweredOn",
		},
	}

	compacted := collector.CompactBatch(records)
	if len(compacted) != 1 {
		t.Fatalf("expected 1 record, got %d", len(compacted))
	}
	if compacted[0].Name != "vm-single" {
		t.Errorf("expected name vm-single, got %s", compacted[0].Name)
	}
}

func TestCompactBatch_TwoUpdates(t *testing.T) {
	records := []domain.VMRecord{
		{
			InstanceUUID: "uuid-1",
			Name:         "vm-initial",
			PowerState:   "poweredOff",
			CPUCores:     2,
		},
		{
			InstanceUUID: "uuid-1",
			Name:         "vm-renamed",
			PowerState:   "poweredOn",
		},
	}

	compacted := collector.CompactBatch(records)
	if len(compacted) != 1 {
		t.Fatalf("expected 1 record, got %d", len(compacted))
	}
	res := compacted[0]
	if res.Name != "vm-renamed" {
		t.Errorf("expected name vm-renamed, got %s", res.Name)
	}
	if res.PowerState != "poweredOn" {
		t.Errorf("expected powerState poweredOn, got %s", res.PowerState)
	}
	if res.CPUCores != 2 {
		t.Errorf("expected CPUCores preserved as 2, got %d", res.CPUCores)
	}
}

func TestCompactBatch_HundredUpdates(t *testing.T) {
	const count = 100
	records := make([]domain.VMRecord, count)
	for i := 0; i < count; i++ {
		records[i] = domain.VMRecord{
			InstanceUUID: "uuid-stress-1",
			Name:         fmt.Sprintf("vm-v%d", i),
			MemoryMB:     int64(1024 + i*10),
		}
	}

	compacted := collector.CompactBatch(records)
	if len(compacted) != 1 {
		t.Fatalf("expected 100 updates to collapse into 1, got %d", len(compacted))
	}
	if compacted[0].Name != "vm-v99" {
		t.Errorf("expected final name vm-v99, got %s", compacted[0].Name)
	}
	if compacted[0].MemoryMB != int64(1024+99*10) {
		t.Errorf("expected final memory %d, got %d", 1024+99*10, compacted[0].MemoryMB)
	}
}

func TestCompactBatch_MixedPropertyUpdates(t *testing.T) {
	records := []domain.VMRecord{
		{
			InstanceUUID:   "uuid-mixed",
			Name:           "vm-mixed",
			PowerState:     "poweredOff",
			GuestIPAddress: "10.0.0.1",
		},
		{
			InstanceUUID: "uuid-mixed",
			PowerState:   "poweredOn",
			CPUCores:     8,
		},
		{
			InstanceUUID:   "uuid-mixed",
			GuestIPAddress: "10.0.0.2",
			Annotation:     "Production server",
		},
	}

	compacted := collector.CompactBatch(records)
	if len(compacted) != 1 {
		t.Fatalf("expected 1 record, got %d", len(compacted))
	}
	res := compacted[0]
	if res.Name != "vm-mixed" {
		t.Errorf("expected preserved name vm-mixed, got %s", res.Name)
	}
	if res.PowerState != "poweredOn" {
		t.Errorf("expected updated powerState poweredOn, got %s", res.PowerState)
	}
	if res.CPUCores != 8 {
		t.Errorf("expected updated CPUCores 8, got %d", res.CPUCores)
	}
	if res.GuestIPAddress != "10.0.0.2" {
		t.Errorf("expected latest IP 10.0.0.2, got %s", res.GuestIPAddress)
	}
	if res.Annotation != "Production server" {
		t.Errorf("expected updated Annotation, got %s", res.Annotation)
	}
}

func TestCompactBatch_DeleteThenUpdate(t *testing.T) {
	now := time.Now()
	records := []domain.VMRecord{
		{
			InstanceUUID: "uuid-recreate",
			Name:         "vm-deleted",
			DeletedAt:    &now,
		},
		{
			InstanceUUID: "uuid-recreate",
			Name:         "vm-recreated",
			PowerState:   "poweredOn",
			DeletedAt:    nil,
		},
	}

	compacted := collector.CompactBatch(records)
	if len(compacted) != 1 {
		t.Fatalf("expected 1 record, got %d", len(compacted))
	}
	if compacted[0].DeletedAt != nil {
		t.Errorf("expected DeletedAt to be cleared, got %v", compacted[0].DeletedAt)
	}
	if compacted[0].Name != "vm-recreated" {
		t.Errorf("expected name vm-recreated, got %s", compacted[0].Name)
	}
}

func TestCompactBatch_UpdateThenDelete(t *testing.T) {
	now := time.Now()
	records := []domain.VMRecord{
		{
			InstanceUUID: "uuid-retire",
			Name:         "vm-active",
			PowerState:   "poweredOn",
		},
		{
			InstanceUUID: "uuid-retire",
			DeletedAt:    &now,
		},
	}

	compacted := collector.CompactBatch(records)
	if len(compacted) != 1 {
		t.Fatalf("expected 1 record, got %d", len(compacted))
	}
	if compacted[0].DeletedAt == nil {
		t.Errorf("expected DeletedAt to be set")
	}
	if compacted[0].Name != "vm-active" {
		t.Errorf("expected name vm-active preserved, got %s", compacted[0].Name)
	}
}
