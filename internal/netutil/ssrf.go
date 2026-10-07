package netutil

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

var (
	ErrAddressNotAllowed = errors.New("address not allowed by SSRF policy")
	ErrNonHTTPSScheme    = errors.New("only HTTPS scheme is permitted")
	ErrDisallowedPort    = errors.New("port not allowed by security policy")
	ErrRedirectForbidden = errors.New("redirects are forbidden")
)

var (
	loopbackV4  = netip.MustParsePrefix("127.0.0.0/8")
	loopbackV6  = netip.MustParsePrefix("::1/128")
	linkLocalV4 = netip.MustParsePrefix("169.254.0.0/16")
	linkLocalV6 = netip.MustParsePrefix("fe80::/10")
	cloudMetaV4 = netip.MustParsePrefix("169.254.169.254/32")
	cloudMetaV6 = netip.MustParsePrefix("fd00:ec2::254/128")
	unspecV4    = netip.MustParsePrefix("0.0.0.0/8")
	unspecV6    = netip.MustParsePrefix("::/128")
	multicastV4 = netip.MustParsePrefix("224.0.0.0/4")
	multicastV6 = netip.MustParsePrefix("ff00::/8")

	privateV4_10  = netip.MustParsePrefix("10.0.0.0/8")
	privateV4_172 = netip.MustParsePrefix("172.16.0.0/12")
	privateV4_192 = netip.MustParsePrefix("192.168.0.0/16")
	cgnatV4       = netip.MustParsePrefix("100.64.0.0/10")
)

// SSRFPolicy encapsulates destination IP validation rules.
type SSRFPolicy struct {
	AllowedPorts       map[int]bool
	AllowPrivateRanges bool
	AllowLoopback      bool
	AllowedHosts       []string
}

// DefaultPolicy provides a safe SSRF policy allowing standard vCenter HTTPS ports.
func DefaultPolicy() *SSRFPolicy {
	return &SSRFPolicy{
		AllowedPorts: map[int]bool{
			443:  true,
			8443: true,
			8989: true, // vcsim local port
			9443: true,
		},
		AllowPrivateRanges: true, // Typical enterprise vCenter deployments are on RFC 1918 networks
		AllowLoopback:      false,
		AllowedHosts:       nil,
	}
}

// ValidateIP checks if an IP address is permitted under this policy.
func (p *SSRFPolicy) ValidateIP(addr netip.Addr) error {
	// Normalize IPv4-mapped IPv6 addresses (e.g. ::ffff:127.0.0.1 -> 127.0.0.1)
	addr = addr.Unmap()

	// 1. Never allow loopback unless explicitly allowed (e.g. testing)
	if !p.AllowLoopback && (loopbackV4.Contains(addr) || loopbackV6.Contains(addr)) {
		return fmt.Errorf("%w: loopback address %s denied", ErrAddressNotAllowed, addr)
	}

	// 2. Never allow link-local
	if linkLocalV4.Contains(addr) || linkLocalV6.Contains(addr) {
		return fmt.Errorf("%w: link-local address %s denied", ErrAddressNotAllowed, addr)
	}

	// 3. Never allow cloud metadata addresses
	if cloudMetaV4.Contains(addr) || cloudMetaV6.Contains(addr) {
		return fmt.Errorf("%w: cloud metadata address %s denied", ErrAddressNotAllowed, addr)
	}

	// 4. Never allow unspecified or multicast
	if unspecV4.Contains(addr) || unspecV6.Contains(addr) || multicastV4.Contains(addr) || multicastV6.Contains(addr) {
		return fmt.Errorf("%w: invalid network address %s denied", ErrAddressNotAllowed, addr)
	}

	// 5. If private ranges not allowed
	if !p.AllowPrivateRanges {
		if privateV4_10.Contains(addr) || privateV4_172.Contains(addr) || privateV4_192.Contains(addr) || cgnatV4.Contains(addr) {
			return fmt.Errorf("%w: private network address %s denied", ErrAddressNotAllowed, addr)
		}
	}

	return nil
}

// ResolveAndValidate resolves a host string once via DNS and verifies all resolved IP addresses.
func (p *SSRFPolicy) ResolveAndValidate(ctx context.Context, host string, port int) (netip.Addr, error) {
	if p.AllowedPorts != nil && !p.AllowedPorts[port] {
		return netip.Addr{}, fmt.Errorf("%w: port %d is not allowed", ErrDisallowedPort, port)
	}

	cleanHost := strings.Trim(host, "[]")

	// If host is already an IP address
	if addr, err := netip.ParseAddr(cleanHost); err == nil {
		if err := p.ValidateIP(addr); err != nil {
			return netip.Addr{}, err
		}
		return addr, nil
	}

	// Resolve hostname once
	resolver := net.DefaultResolver
	addrs, err := resolver.LookupNetIP(ctx, "ip", cleanHost)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("failed to resolve host %q: %w", host, err)
	}
	if len(addrs) == 0 {
		return netip.Addr{}, fmt.Errorf("no IP addresses resolved for host %q", host)
	}

	// Verify all returned IP addresses satisfy the SSRF policy
	for _, addr := range addrs {
		if err := p.ValidateIP(addr); err != nil {
			return netip.Addr{}, err
		}
	}

	return addrs[0], nil
}

// NewSafeTransport creates an http.Transport configured to dial directly to the pre-resolved IP,
// preventing DNS rebinding, while presenting the intended SNI host header.
func (p *SSRFPolicy) NewSafeTransport(resolvedIP netip.Addr, host string, tlsCfg *tls.Config) *http.Transport {
	if tlsCfg == nil {
		tlsCfg = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}
	if tlsCfg.ServerName == "" {
		tlsCfg.ServerName = host
	}

	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Extract port from requested addr
			_, portStr, err := net.SplitHostPort(addr)
			if err != nil {
				portStr = "443"
			}
			targetAddr := net.JoinHostPort(resolvedIP.String(), portStr)
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, targetAddr)
		},
		TLSClientConfig:       tlsCfg,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// NewSafeHTTPClient returns an http.Client with safe transport and disabled redirects.
func (p *SSRFPolicy) NewSafeHTTPClient(resolvedIP netip.Addr, host string, tlsCfg *tls.Config, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: p.NewSafeTransport(resolvedIP, host, tlsCfg),
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return ErrRedirectForbidden
		},
	}
}

// SanitizeError strips sensitive credentials or tokens from error text.
func SanitizeError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	// Redact basic auth or URL credentials
	if strings.Contains(msg, "@") {
		parts := strings.Split(msg, "@")
		if len(parts) > 1 {
			msg = "[REDACTED]@" + parts[len(parts)-1]
		}
	}
	return msg
}
