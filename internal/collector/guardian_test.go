package collector_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/infrastructure/sims/internal/vsphere"
)

func TestSessionGuardian_ProbeSuccess(t *testing.T) {
	metrics := telemetry.NewMetrics("sims-test", "test")
	fakeSess := vsphere.NewFakeSession()

	cfg := collector.SessionGuardianConfig{
		VCenterID:              "vc-test-1",
		Interval:               100 * time.Millisecond,
		Timeout:                500 * time.Millisecond,
		MaxConsecutiveFailures: 2,
	}

	guardian := collector.NewSessionGuardian(cfg, fakeSess, metrics, nil)

	if guardian.State() != collector.SessionStateConnected {
		t.Fatalf("expected initial state connected, got %v", guardian.State())
	}

	ctx := context.Background()
	if err := guardian.Probe(ctx); err != nil {
		t.Fatalf("unexpected probe error: %v", err)
	}

	if guardian.ConsecutiveFailures() != 0 {
		t.Errorf("expected 0 failures, got %d", guardian.ConsecutiveFailures())
	}
	if guardian.State() != collector.SessionStateConnected {
		t.Errorf("expected state connected, got %v", guardian.State())
	}
}

func TestSessionGuardian_ConsecutiveFailuresAndDeadNotification(t *testing.T) {
	metrics := telemetry.NewMetrics("sims-test", "test")
	fakeSess := vsphere.NewFakeSession()

	cfg := collector.SessionGuardianConfig{
		VCenterID:              "vc-test-2",
		Interval:               50 * time.Millisecond,
		Timeout:                100 * time.Millisecond,
		MaxConsecutiveFailures: 2,
	}

	guardian := collector.NewSessionGuardian(cfg, fakeSess, metrics, nil)

	var callbackCalled atomic.Bool
	guardian.SetOnSessionDead(func() {
		callbackCalled.Store(true)
	})

	// Inject error into fake session
	fakeSess.InjectCurrentTimeError(errors.New("connection reset by peer"))

	ctx := context.Background()

	// 1st failure -> degraded
	err1 := guardian.Probe(ctx)
	if err1 == nil {
		t.Fatal("expected error on 1st probe, got nil")
	}
	if guardian.ConsecutiveFailures() != 1 {
		t.Errorf("expected 1 failure, got %d", guardian.ConsecutiveFailures())
	}
	if guardian.State() != collector.SessionStateDegraded {
		t.Errorf("expected state degraded, got %v", guardian.State())
	}
	if callbackCalled.Load() {
		t.Error("callback should not be called on degraded state")
	}

	// 2nd failure -> disconnected (max consecutive failures reached)
	err2 := guardian.Probe(ctx)
	if err2 == nil {
		t.Fatal("expected error on 2nd probe, got nil")
	}
	if guardian.ConsecutiveFailures() != 2 {
		t.Errorf("expected 2 failures, got %d", guardian.ConsecutiveFailures())
	}
	if guardian.State() != collector.SessionStateDisconnected {
		t.Errorf("expected state disconnected, got %v", guardian.State())
	}

	// Verify Dead() channel signaled
	select {
	case <-guardian.Dead():
		// Success
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for Dead() channel signal")
	}

	// Wait briefly for goroutine callback
	deadline := time.Now().Add(200 * time.Millisecond)
	for !callbackCalled.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !callbackCalled.Load() {
		t.Error("expected onSessionDead callback to be called")
	}
}

func TestSessionGuardian_RecoveryOnSuccess(t *testing.T) {
	metrics := telemetry.NewMetrics("sims-test", "test")
	fakeSess := vsphere.NewFakeSession()

	cfg := collector.SessionGuardianConfig{
		VCenterID:              "vc-test-3",
		Interval:               50 * time.Millisecond,
		Timeout:                100 * time.Millisecond,
		MaxConsecutiveFailures: 2,
	}

	guardian := collector.NewSessionGuardian(cfg, fakeSess, metrics, nil)
	ctx := context.Background()

	fakeSess.InjectCurrentTimeError(errors.New("transient error"))
	_ = guardian.Probe(ctx)
	if guardian.State() != collector.SessionStateDegraded {
		t.Fatalf("expected state degraded, got %v", guardian.State())
	}

	// Clear error -> probe recovers
	fakeSess.InjectCurrentTimeError(nil)
	if err := guardian.Probe(ctx); err != nil {
		t.Fatalf("unexpected error on recovery probe: %v", err)
	}

	if guardian.ConsecutiveFailures() != 0 {
		t.Errorf("expected 0 failures after recovery, got %d", guardian.ConsecutiveFailures())
	}
	if guardian.State() != collector.SessionStateConnected {
		t.Errorf("expected state connected after recovery, got %v", guardian.State())
	}
}

func TestSessionGuardian_UpdateSession(t *testing.T) {
	metrics := telemetry.NewMetrics("sims-test", "test")
	fakeSess := vsphere.NewFakeSession()
	fakeSess.InjectCurrentTimeError(errors.New("hard failure"))

	cfg := collector.SessionGuardianConfig{
		VCenterID:              "vc-test-4",
		Interval:               50 * time.Millisecond,
		Timeout:                100 * time.Millisecond,
		MaxConsecutiveFailures: 1,
	}

	guardian := collector.NewSessionGuardian(cfg, fakeSess, metrics, nil)
	_ = guardian.Probe(context.Background())

	if guardian.State() != collector.SessionStateDisconnected {
		t.Fatalf("expected disconnected, got %v", guardian.State())
	}

	// New healthy session
	newSess := vsphere.NewFakeSession()
	guardian.UpdateSession(newSess)

	if guardian.State() != collector.SessionStateConnected {
		t.Errorf("expected state reset to connected, got %v", guardian.State())
	}
	if guardian.ConsecutiveFailures() != 0 {
		t.Errorf("expected failure counter reset to 0, got %d", guardian.ConsecutiveFailures())
	}
}

func TestSessionGuardian_BackgroundLoopAndStop(t *testing.T) {
	metrics := telemetry.NewMetrics("sims-test", "test")
	fakeSess := vsphere.NewFakeSession()

	cfg := collector.SessionGuardianConfig{
		VCenterID:              "vc-test-5",
		Interval:               20 * time.Millisecond,
		Timeout:                100 * time.Millisecond,
		MaxConsecutiveFailures: 2,
	}

	guardian := collector.NewSessionGuardian(cfg, fakeSess, metrics, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := guardian.Start(ctx); err != nil {
		t.Fatalf("failed to start guardian: %v", err)
	}

	// Starting twice should error
	if err := guardian.Start(ctx); err == nil {
		t.Error("expected error when starting already running guardian")
	}

	// Allow multiple ticker loops to fire
	time.Sleep(100 * time.Millisecond)

	guardian.Stop()

	// Stopping again should be safe and idempotent
	guardian.Stop()
}

func TestSessionGuardian_ProbeTimeout(t *testing.T) {
	metrics := telemetry.NewMetrics("sims-test", "test")
	fakeSess := vsphere.NewFakeSession()

	// Simulate call taking longer than probe timeout
	fakeSess.InjectCurrentTimeDelay(100 * time.Millisecond)

	cfg := collector.SessionGuardianConfig{
		VCenterID:              "vc-test-6",
		Interval:               50 * time.Millisecond,
		Timeout:                20 * time.Millisecond,
		MaxConsecutiveFailures: 2,
	}

	guardian := collector.NewSessionGuardian(cfg, fakeSess, metrics, nil)

	err := guardian.Probe(context.Background())
	if err == nil {
		t.Fatal("expected probe to fail on timeout, got nil")
	}

	if guardian.ConsecutiveFailures() != 1 {
		t.Errorf("expected 1 failure on timeout, got %d", guardian.ConsecutiveFailures())
	}
}

func TestSessionGuardian_HeartbeatsWhileStreamPaused(t *testing.T) {
	metrics := telemetry.NewMetrics("sims-test", "test")
	fakeSess := vsphere.NewFakeSession()

	cfg := collector.SessionGuardianConfig{
		VCenterID:              "vc-paused-test",
		Interval:               20 * time.Millisecond,
		Timeout:                100 * time.Millisecond,
		MaxConsecutiveFailures: 3,
	}

	guardian := collector.NewSessionGuardian(cfg, fakeSess, metrics, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := guardian.Start(ctx); err != nil {
		t.Fatalf("failed to start guardian: %v", err)
	}
	defer guardian.Stop()

	// Simulate stream paused for 100ms (no stream activity / stream backpressure)
	time.Sleep(100 * time.Millisecond)

	// Verify guardian kept heartbeating independently
	if fakeSess.CurrentTimeCalls() < 3 {
		t.Errorf("expected at least 3 keepalive probes during stream pause, got %d", fakeSess.CurrentTimeCalls())
	}
	if guardian.State() != collector.SessionStateConnected {
		t.Errorf("expected session to remain connected during pause, got %v", guardian.State())
	}
	if guardian.ConsecutiveFailures() != 0 {
		t.Errorf("expected 0 consecutive failures, got %d", guardian.ConsecutiveFailures())
	}
}
