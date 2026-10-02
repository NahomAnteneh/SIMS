package vsphere_test

import (
	"context"
	"crypto/tls"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/vsphere"
	"github.com/vmware/govmomi/simulator"
)

func TestSessionManagerLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. In-process simulator
	model := simulator.VPX()
	defer model.Remove()
	if err := model.Create(); err != nil {
		t.Fatalf("failed to create vcsim model: %v", err)
	}

	server := model.Service.NewServer()
	defer server.Close()

	cfg := vsphere.SessionConfig{
		URL:            server.URL.String(),
		Username:       "user",
		Password:       "pass",
		Insecure:       true,
		TLSMinVersion:  tls.VersionTLS12,
		RequestTimeout: 5 * time.Second,
	}

	sm, err := vsphere.NewSessionManager(cfg)
	if err != nil {
		t.Fatalf("failed to create session manager: %v", err)
	}

	// 2. Connect
	if err := sm.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	if sm.Client() == nil {
		t.Fatalf("expected non-nil client after Connect")
	}

	// 3. Login
	if err := sm.Login(ctx); err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// 4. CurrentTime
	serverTime, err := sm.CurrentTime(ctx)
	if err != nil {
		t.Fatalf("CurrentTime failed: %v", err)
	}
	if serverTime.IsZero() {
		t.Errorf("expected non-zero server time")
	}

	// 5. Reconnect
	if err := sm.Reconnect(ctx); err != nil {
		t.Fatalf("Reconnect failed: %v", err)
	}

	// 6. Logout
	if err := sm.Logout(ctx); err != nil {
		t.Fatalf("Logout failed: %v", err)
	}
	if sm.Client() != nil {
		t.Errorf("expected nil client after Logout")
	}
}

func TestSessionManagerInvalidCredentials(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	model := simulator.VPX()
	defer model.Remove()
	if err := model.Create(); err != nil {
		t.Fatalf("failed to create vcsim model: %v", err)
	}

	server := model.Service.NewServer()
	defer server.Close()

	// Enforce credential validation in simulator
	model.Service.Listen.User = url.UserPassword("user", "pass")

	cfg := vsphere.SessionConfig{
		URL:            server.URL.String(),
		Username:       "baduser",
		Password:       "badpass",
		Insecure:       true,
		RequestTimeout: 5 * time.Second,
	}

	sm, err := vsphere.NewSessionManager(cfg)
	if err != nil {
		t.Fatalf("failed to create session manager: %v", err)
	}

	err = sm.Login(ctx)
	if err == nil {
		t.Fatalf("expected error on invalid credentials, got nil")
	}
	if !errors.Is(err, vsphere.ErrInvalidCredentials) && !errors.Is(err, vsphere.ErrFault) {
		t.Errorf("expected ErrInvalidCredentials or ErrFault, got: %v", err)
	}
}

func TestSessionManagerTimeout(t *testing.T) {
	// Context already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := vsphere.SessionConfig{
		URL:            "https://192.0.2.1:8989/sdk", // Non-routable TEST-NET-1 IP
		Insecure:       true,
		RequestTimeout: 10 * time.Millisecond,
	}

	sm, err := vsphere.NewSessionManager(cfg)
	if err != nil {
		t.Fatalf("failed to create session manager: %v", err)
	}

	err = sm.Connect(ctx)
	if err == nil {
		t.Fatalf("expected timeout/cancellation error, got nil")
	}
}
