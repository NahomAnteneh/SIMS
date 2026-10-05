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
	vsphere.ConfigureTestVPX(model)
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
	vsphere.ConfigureTestVPX(model)
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

func TestSessionManager_VersionPolicy(t *testing.T) {
	tests := []struct {
		name       string
		version    string
		apiVersion string
		shouldFail bool
	}{
		{
			name:       "rejects default vcsim 6.5.0",
			version:    "6.5.0",
			apiVersion: "6.5",
			shouldFail: true,
		},
		{
			name:       "rejects legacy vCenter 6.0.0",
			version:    "6.0.0",
			apiVersion: "6.0",
			shouldFail: true,
		},
		{
			name:       "rejects legacy vCenter 6.7.0",
			version:    "6.7.0",
			apiVersion: "6.7",
			shouldFail: true,
		},
		{
			name:       "accepts vCenter 7.0.0",
			version:    "7.0.0",
			apiVersion: "7.0.0.0",
			shouldFail: false,
		},
		{
			name:       "accepts vCenter 7.0.3",
			version:    "7.0.3",
			apiVersion: "7.0.3.0",
			shouldFail: false,
		},
		{
			name:       "accepts vCenter 8.0.2",
			version:    "8.0.2",
			apiVersion: "8.0.2.0",
			shouldFail: false,
		},
		{
			name:       "accepts vCenter 8.0.3",
			version:    "8.0.3",
			apiVersion: "8.0.3.0",
			shouldFail: false,
		},
		{
			name:       "accepts vCenter 9.1.0",
			version:    "9.1.0",
			apiVersion: "9.1.0.0",
			shouldFail: false,
		},
		{
			name:       "accepts future vCenter 10.0.0 without ceiling",
			version:    "10.0.0",
			apiVersion: "10.0.0.0",
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			model := simulator.VPX()
			model.ServiceContent.About.Version = tt.version
			model.ServiceContent.About.ApiVersion = tt.apiVersion
			model.ServiceContent.About.FullName = "VMware vCenter Server " + tt.version
			defer model.Remove()
			if err := model.Create(); err != nil {
				t.Fatalf("failed to create simulator: %v", err)
			}

			server := model.Service.NewServer()
			defer server.Close()

			cfg := vsphere.SessionConfig{
				URL:            server.URL.String(),
				Username:       "user",
				Password:       "pass",
				Insecure:       true,
				RequestTimeout: 5 * time.Second,
			}

			sm, err := vsphere.NewSessionManager(cfg)
			if err != nil {
				t.Fatalf("failed to create session manager: %v", err)
			}

			err = sm.Connect(ctx)
			if tt.shouldFail {
				if err == nil {
					t.Fatalf("expected Connect to fail for version %s, but succeeded", tt.version)
				}
				if !errors.Is(err, vsphere.ErrUnsupportedVersion) {
					t.Errorf("expected ErrUnsupportedVersion for version %s, got: %v", tt.version, err)
				}
				var uvErr *vsphere.UnsupportedVersionError
				if !errors.As(err, &uvErr) {
					t.Errorf("expected *vsphere.UnsupportedVersionError, got %T: %v", err, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected Connect to succeed for version %s, got: %v", tt.version, err)
				}
				if sm.About() == nil {
					t.Errorf("expected non-nil About metadata for version %s", tt.version)
				} else if sm.About().Version != tt.version {
					t.Errorf("expected About.Version %s, got %s", tt.version, sm.About().Version)
				}
			}
		})
	}
}

// TestSessionManager_AcceptsVCSimConfiguredAs700 verifies that the session layer parses and accepts
// a 7.0.0 AboutInfo and establishes a session against vcsim configured to report 7.0.0.
//
// NOTE: vcsim accepts any SOAPAction header string, so this test does NOT prove that a real
// vCenter 7.0 accepts "urn:vim25/9.1.0.0". Empirical verification against a real vCenter 7.0
// remains BLOCKED (human) until access to a real 7.0 lab environment is provided.
func TestSessionManager_AcceptsVCSimConfiguredAs700(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Configure simulator to report vCenter 7.0.0 and API version 7.0.0.0
	model := simulator.VPX()
	model.ServiceContent.About.Version = "7.0.0"
	model.ServiceContent.About.ApiVersion = "7.0.0.0"
	model.ServiceContent.About.FullName = "VMware vCenter Server 7.0.0 build-16189094"
	defer model.Remove()
	if err := model.Create(); err != nil {
		t.Fatalf("failed to create simulator: %v", err)
	}

	server := model.Service.NewServer()
	defer server.Close()

	cfg := vsphere.SessionConfig{
		URL:            server.URL.String(),
		Username:       "user",
		Password:       "pass",
		Insecure:       true,
		RequestTimeout: 5 * time.Second,
	}

	sm, err := vsphere.NewSessionManager(cfg)
	if err != nil {
		t.Fatalf("failed to create session manager: %v", err)
	}

	// 1. Connect verifies version 7.0.0 is parsed and accepted
	if err := sm.Connect(ctx); err != nil {
		t.Fatalf("Connect failed against vCenter 7.0.0: %v", err)
	}

	// 2. Login verifies session authentication against simulator
	if err := sm.Login(ctx); err != nil {
		t.Fatalf("Login failed against vCenter 7.0.0: %v", err)
	}

	// 3. CurrentTime verifies round-trip SOAP call succeeds without fault
	serverTime, err := sm.CurrentTime(ctx)
	if err != nil {
		t.Fatalf("CurrentTime failed: %v", err)
	}
	if serverTime.IsZero() {
		t.Errorf("expected non-zero server time")
	}

	// 4. Verify client version in underlying SOAP client
	c := sm.Client()
	if c == nil {
		t.Fatalf("expected non-nil client")
	}
	if c.Client.Version != "9.1.0.0" {
		t.Errorf("expected client Version 9.1.0.0, got %s", c.Client.Version)
	}
}

func TestSessionManager_RejectsUnparseableVersion(t *testing.T) {
	unparseableVersions := []struct {
		name       string
		version    string
		apiVersion string
	}{
		{name: "both empty", version: "", apiVersion: ""},
		{name: "non-numeric version", version: "unknown", apiVersion: ""},
		{name: "alpha string", version: "preview", apiVersion: "beta"},
	}

	for _, tt := range unparseableVersions {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			model := simulator.VPX()
			model.ServiceContent.About.Version = tt.version
			model.ServiceContent.About.ApiVersion = tt.apiVersion
			defer model.Remove()
			if err := model.Create(); err != nil {
				t.Fatalf("failed to create simulator: %v", err)
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

			err = sm.Connect(ctx)
			if err == nil {
				t.Fatal("expected Connect to fail on unparseable version, got nil")
			}
			if !errors.Is(err, vsphere.ErrUnparseableVersion) {
				t.Errorf("expected ErrUnparseableVersion, got: %v", err)
			}
		})
	}
}

func TestSessionManager_RejectsNonVirtualCenterApiType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Direct ESXi HostAgent endpoint
	model := simulator.VPX()
	model.ServiceContent.About.Version = "8.0.3"
	model.ServiceContent.About.ApiVersion = "8.0.3.0"
	model.ServiceContent.About.ApiType = "HostAgent"
	defer model.Remove()
	if err := model.Create(); err != nil {
		t.Fatalf("failed to create simulator: %v", err)
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

	err = sm.Connect(ctx)
	if err == nil {
		t.Fatal("expected Connect to reject HostAgent endpoint, got nil")
	}
	if !errors.Is(err, vsphere.ErrInvalidApiType) {
		t.Errorf("expected ErrInvalidApiType, got: %v", err)
	}
}
