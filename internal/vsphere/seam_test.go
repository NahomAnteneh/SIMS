package vsphere_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/vsphere"
	"github.com/vmware/govmomi/fault"
	"github.com/vmware/govmomi/vim25/types"
)

func TestSeamInterfaceCompatibility(t *testing.T) {
	// Compile-time interface assertion
	var _ vsphere.Session = (*vsphere.SessionManager)(nil)
	var _ vsphere.Session = (*vsphere.FakeSession)(nil)
	var _ vsphere.PropertyCollector = (*vsphere.GovmomiPropertyCollector)(nil)
	var _ vsphere.PropertyCollector = (*vsphere.FakePropertyCollector)(nil)
	var _ vsphere.EventCollector = (*vsphere.GovmomiEventCollector)(nil)
	var _ vsphere.EventCollector = (*vsphere.FakeEventCollector)(nil)
	var _ vsphere.EventManager = (*vsphere.GovmomiEventManager)(nil)
	var _ vsphere.EventManager = (*vsphere.FakeEventManager)(nil)
}

func TestFaultInjection_InvalidCollectorVersion(t *testing.T) {
	fakePC := vsphere.NewFakePropertyCollector()
	fakePC.InjectInvalidCollectorVersion()

	_, err := fakePC.WaitForUpdatesEx(context.Background(), "1", nil)
	if err == nil {
		t.Fatalf("expected error from injected fault, got nil")
	}

	if !fault.Is(err, &types.InvalidCollectorVersion{}) {
		t.Fatalf("expected fault.Is(err, &types.InvalidCollectorVersion{}) to be true, got error: %v", err)
	}
}

func TestFaultInjection_AuthFailures(t *testing.T) {
	t.Run("NotAuthenticated", func(t *testing.T) {
		fakePC := vsphere.NewFakePropertyCollector()
		fakePC.InjectAuthFailure()

		_, err := fakePC.WaitForUpdatesEx(context.Background(), "1", nil)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !fault.Is(err, &types.NotAuthenticated{}) {
			t.Errorf("expected fault.Is(err, &types.NotAuthenticated{}) to be true, got %v", err)
		}
	})

	t.Run("InvalidLogin", func(t *testing.T) {
		err := vsphere.NewInvalidLoginFault()
		if !fault.Is(err, &types.InvalidLogin{}) {
			t.Errorf("expected fault.Is(err, &types.InvalidLogin{}) to be true, got %v", err)
		}
	})
}

func TestFaultInjection_HungCalls(t *testing.T) {
	fakePC := vsphere.NewFakePropertyCollector()
	fakePC.InjectHungWaitForUpdates(500 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := fakePC.WaitForUpdatesEx(ctx, "1", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
	}
}

func TestFaultInjection_TruncatedUpdates(t *testing.T) {
	fakePC := vsphere.NewFakePropertyCollector()
	fakePC.InjectTruncatedUpdates(true)

	res, err := fakePC.WaitForUpdatesEx(context.Background(), "1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Truncated == nil || !*res.Truncated {
		t.Fatalf("expected Truncated=true, got: %v", res.Truncated)
	}
}

func TestFaultInjection_Disconnects(t *testing.T) {
	fakePC := vsphere.NewFakePropertyCollector()
	fakePC.InjectDisconnect()

	_, err := fakePC.WaitForUpdatesEx(context.Background(), "1", nil)
	if !errors.Is(err, vsphere.ErrFakeDisconnect) {
		t.Fatalf("expected ErrFakeDisconnect, got: %v", err)
	}
}

func TestFakeEventCollector_PagingAndReset(t *testing.T) {
	fakeEC := vsphere.NewFakeEventCollector()
	fakeEC.EnqueueEvents(
		&types.VmCreatedEvent{Event: types.Event{Key: 100, CreatedTime: time.Now()}},
		&types.VmPoweredOnEvent{Event: types.Event{Key: 101, CreatedTime: time.Now()}},
		&types.VmPoweredOffEvent{Event: types.Event{Key: 102, CreatedTime: time.Now()}},
	)

	ctx := context.Background()
	events, err := fakeEC.ReadNextEvents(ctx, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].GetEvent().Key != 100 || events[1].GetEvent().Key != 101 {
		t.Errorf("unexpected event keys: %d, %d", events[0].GetEvent().Key, events[1].GetEvent().Key)
	}

	remaining, err := fakeEC.ReadNextEvents(ctx, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining event, got %d", len(remaining))
	}

	if err := fakeEC.Reset(ctx); err != nil {
		t.Fatalf("Reset failed: %v", err)
	}
	if err := fakeEC.Destroy(ctx); err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}
}
