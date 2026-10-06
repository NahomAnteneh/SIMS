package netutil

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestSSRFPolicy_Validation(t *testing.T) {
	policy := DefaultPolicy()

	deniedIPs := []string{
		"127.0.0.1",
		"127.0.0.2",
		"::1",
		"169.254.169.254", // AWS/GCP metadata
		"169.254.1.1",     // Link-local
		"fe80::1",         // Link-local v6
		"fd00:ec2::254",   // AWS IMDSv6
		"0.0.0.0",
		"224.0.0.1", // Multicast
	}

	for _, ipStr := range deniedIPs {
		addr := netip.MustParseAddr(ipStr)
		if err := policy.ValidateIP(addr); err == nil {
			t.Errorf("expected IP %s to be denied by SSRF policy, but was permitted", ipStr)
		}
	}

	// Permitted enterprise IPs under default policy
	allowedIPs := []string{
		"10.20.10.15",
		"192.168.1.100",
		"172.16.0.5",
		"8.8.8.8",
	}

	for _, ipStr := range allowedIPs {
		addr := netip.MustParseAddr(ipStr)
		if err := policy.ValidateIP(addr); err != nil {
			t.Errorf("expected IP %s to be permitted, but got: %v", ipStr, err)
		}
	}
}

func TestSSRFPolicy_PortValidation(t *testing.T) {
	policy := DefaultPolicy()
	ctx := context.Background()

	// Allowed port (443)
	_, err := policy.ResolveAndValidate(ctx, "8.8.8.8", 443)
	if err != nil {
		t.Errorf("expected port 443 to be allowed, got: %v", err)
	}

	// Disallowed port (22, 80, 25)
	_, err = policy.ResolveAndValidate(ctx, "8.8.8.8", 22)
	if !errors.Is(err, ErrDisallowedPort) {
		t.Errorf("expected ErrDisallowedPort for port 22, got: %v", err)
	}
}

func TestSSRFPolicy_RedirectsForbidden(t *testing.T) {
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Target reached"))
	}))
	defer redirectTarget.Close()

	initialServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer initialServer.Close()

	policy := DefaultPolicy()
	client := policy.NewSafeHTTPClient(netip.MustParseAddr("127.0.0.1"), "localhost", nil, 2*time.Second)

	req, _ := http.NewRequest(http.MethodGet, initialServer.URL, nil)
	_, err := client.Do(req)
	if err == nil {
		t.Fatalf("expected redirect to be forbidden, but request succeeded")
	}
	if !errors.Is(err, ErrRedirectForbidden) && !strings.Contains(err.Error(), "redirects are forbidden") {
		t.Errorf("expected redirect forbidden error, got: %v", err)
	}
}
