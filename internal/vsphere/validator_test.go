package vsphere_test

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/netutil"
	"github.com/infrastructure/sims/internal/vsphere"
	"github.com/vmware/govmomi/simulator"
)

func TestValidateConnection_ValidVCSim(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	model := simulator.VPX()
	vsphere.ConfigureTestVPX(model)
	defer model.Remove()

	if err := model.Create(); err != nil {
		t.Fatalf("failed to create simulator model: %v", err)
	}

	model.Service.TLS = new(tls.Config)
	s := model.Service.NewServer()
	defer s.Close()

	host, portStr, err := net.SplitHostPort(s.URL.Host)
	if err != nil {
		t.Fatalf("failed to split host port: %v", err)
	}
	port, _ := strconv.Atoi(portStr)

	res, err := vsphere.ValidateConnection(ctx, vsphere.ValidationParams{
		Host:     host,
		Port:     port,
		Username: s.URL.User.Username(),
		Password: "password",
		TLS:      vsphere.TLSValidationConfig{Verify: false},
		Policy: &netutil.SSRFPolicy{
			AllowLoopback:      true,
			AllowPrivateRanges: true,
		},
	})
	if err != nil {
		t.Fatalf("expected validation success, got error: %v", err)
	}

	if res.ProductVersion != "8.0.3" {
		t.Errorf("expected product version 8.0.3, got %s", res.ProductVersion)
	}
	if res.APIVersion != "8.0.3.0" {
		t.Errorf("expected API version 8.0.3.0, got %s", res.APIVersion)
	}
	if res.Fingerprint == "" {
		t.Errorf("expected non-empty certificate fingerprint")
	}
}

func TestValidateConnection_AuthFailed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	model := simulator.VPX()
	vsphere.ConfigureTestVPX(model)
	defer model.Remove()

	if err := model.Create(); err != nil {
		t.Fatalf("failed to create simulator model: %v", err)
	}

	model.Service.TLS = new(tls.Config)
	s := model.Service.NewServer()
	defer s.Close()

	model.Service.Listen.User = url.UserPassword("user", "password")

	host, portStr, _ := net.SplitHostPort(s.URL.Host)
	port, _ := strconv.Atoi(portStr)

	_, err := vsphere.ValidateConnection(ctx, vsphere.ValidationParams{
		Host:     host,
		Port:     port,
		Username: "bad_user",
		Password: "bad_pass",
		TLS:      vsphere.TLSValidationConfig{Verify: false},
		Policy: &netutil.SSRFPolicy{
			AllowLoopback:      true,
			AllowPrivateRanges: true,
		},
	})
	if err == nil {
		t.Fatalf("expected auth_failed error, got nil")
	}

	var valErr *vsphere.ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *vsphere.ValidationError, got %T: %v", err, err)
	}
	if valErr.Code != "auth_failed" {
		t.Errorf("expected code auth_failed, got %s", valErr.Code)
	}
}

func TestValidateConnection_TLSUntrustedAndPinning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	model := simulator.VPX()
	vsphere.ConfigureTestVPX(model)
	defer model.Remove()

	if err := model.Create(); err != nil {
		t.Fatalf("failed to create simulator model: %v", err)
	}

	model.Service.TLS = new(tls.Config)
	s := model.Service.NewServer()
	defer s.Close()

	host, portStr, _ := net.SplitHostPort(s.URL.Host)
	port, _ := strconv.Atoi(portStr)

	// 1. With Verify: true against self-signed vcsim cert -> should fail with tls_untrusted and return fingerprint
	_, err := vsphere.ValidateConnection(ctx, vsphere.ValidationParams{
		Host:     host,
		Port:     port,
		Username: s.URL.User.Username(),
		Password: "password",
		TLS:      vsphere.TLSValidationConfig{Verify: true},
		Policy: &netutil.SSRFPolicy{
			AllowLoopback:      true,
			AllowPrivateRanges: true,
		},
	})
	if err == nil {
		t.Fatalf("expected tls_untrusted error, got nil")
	}

	var valErr *vsphere.ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *vsphere.ValidationError, got %T: %v", err, err)
	}
	if valErr.Code != "tls_untrusted" {
		t.Errorf("expected code tls_untrusted, got %s", valErr.Code)
	}
	if valErr.Fingerprint == "" {
		t.Fatalf("expected non-empty fingerprint for untrusted cert")
	}

	// 2. Now pin the presented fingerprint -> should succeed!
	pinnedFP := valErr.Fingerprint
	res, err := vsphere.ValidateConnection(ctx, vsphere.ValidationParams{
		Host:     host,
		Port:     port,
		Username: s.URL.User.Username(),
		Password: "password",
		TLS: vsphere.TLSValidationConfig{
			Verify:            true,
			PinnedFingerprint: &pinnedFP,
		},
		Policy: &netutil.SSRFPolicy{
			AllowLoopback:      true,
			AllowPrivateRanges: true,
		},
	})
	if err != nil {
		t.Fatalf("expected success with pinned fingerprint, got: %v", err)
	}
	if res.Fingerprint != pinnedFP {
		t.Errorf("expected fingerprint %s, got %s", pinnedFP, res.Fingerprint)
	}

	// 3. Pin a wrong fingerprint -> should fail with tls_untrusted
	wrongFP := "AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99"
	_, err = vsphere.ValidateConnection(ctx, vsphere.ValidationParams{
		Host:     host,
		Port:     port,
		Username: s.URL.User.Username(),
		Password: "password",
		TLS: vsphere.TLSValidationConfig{
			Verify:            true,
			PinnedFingerprint: &wrongFP,
		},
		Policy: &netutil.SSRFPolicy{
			AllowLoopback:      true,
			AllowPrivateRanges: true,
		},
	})
	if err == nil {
		t.Fatalf("expected mismatch error, got nil")
	}
	if !errors.As(err, &valErr) || valErr.Code != "tls_untrusted" {
		t.Errorf("expected code tls_untrusted on mismatch, got: %v", err)
	}
}

func TestValidateConnection_UnsupportedVersion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	model := simulator.VPX()
	// Deliberately set to legacy vSphere 6.5
	model.ServiceContent.About.Version = "6.5.0"
	model.ServiceContent.About.ApiVersion = "6.5"
	model.ServiceContent.About.FullName = "VMware vCenter Server 6.5.0"
	defer model.Remove()

	if err := model.Create(); err != nil {
		t.Fatalf("failed to create simulator model: %v", err)
	}

	model.Service.TLS = new(tls.Config)
	s := model.Service.NewServer()
	defer s.Close()

	host, portStr, _ := net.SplitHostPort(s.URL.Host)
	port, _ := strconv.Atoi(portStr)

	_, err := vsphere.ValidateConnection(ctx, vsphere.ValidationParams{
		Host:     host,
		Port:     port,
		Username: s.URL.User.Username(),
		Password: "password",
		TLS:      vsphere.TLSValidationConfig{Verify: false},
		Policy: &netutil.SSRFPolicy{
			AllowLoopback:      true,
			AllowPrivateRanges: true,
		},
	})
	if err == nil {
		t.Fatalf("expected unsupported_version error, got nil")
	}

	var valErr *vsphere.ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *vsphere.ValidationError, got %T: %v", err, err)
	}
	if valErr.Code != "unsupported_version" {
		t.Errorf("expected code unsupported_version, got %s", valErr.Code)
	}
}

func TestValidateConnection_SSRFProtection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Default policy denies loopback
	_, err := vsphere.ValidateConnection(ctx, vsphere.ValidationParams{
		Host:          "127.0.0.1",
		Port:          443,
		Username:      "user",
		Password:      "pass",
		AllowLoopback: false, // Default
	})
	if err == nil {
		t.Fatalf("expected address_not_allowed error for loopback, got nil")
	}

	var valErr *vsphere.ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *vsphere.ValidationError, got %T: %v", err, err)
	}
	if valErr.Code != "address_not_allowed" {
		t.Errorf("expected code address_not_allowed, got %s", valErr.Code)
	}
}

func TestValidateConnection_UnreachableHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect to non-existent private address on port 443 with short timeout
	_, err := vsphere.ValidateConnection(ctx, vsphere.ValidationParams{
		Host:           "192.0.2.1", // TEST-NET-1 (non-routable)
		Port:           443,
		Username:       "user",
		Password:       "pass",
		StepTimeout:    500 * time.Millisecond,
		OverallTimeout: 1 * time.Second,
		AllowPrivate:   true,
	})
	if err == nil {
		t.Fatalf("expected unreachable or timeout error, got nil")
	}

	var valErr *vsphere.ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *vsphere.ValidationError, got %T: %v", err, err)
	}
	if valErr.Code != "unreachable" && valErr.Code != "timeout" {
		t.Errorf("expected code unreachable or timeout, got %s", valErr.Code)
	}
}
