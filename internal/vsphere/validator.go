package vsphere

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/infrastructure/sims/internal/netutil"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/vapi/rest"
	"github.com/vmware/govmomi/vapi/tags"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

// TLSValidationConfig holds TLS verification settings for connection validation.
type TLSValidationConfig struct {
	Verify            bool    `json:"verify"`
	CAPEM             string  `json:"ca_pem,omitempty"`
	PinnedFingerprint *string `json:"pinned_fingerprint,omitempty"`
}

// ValidationParams configures the pre-registration validation pipeline.
type ValidationParams struct {
	Host           string
	Port           int
	Username       string
	Password       string
	TLS            TLSValidationConfig
	OverallTimeout time.Duration
	StepTimeout    time.Duration
	AllowPrivate   bool
	AllowLoopback  bool
	Policy         *netutil.SSRFPolicy
}

// ValidationResult represents the discovered capabilities and version of the validated vCenter.
type ValidationResult struct {
	Host           string            `json:"host"`
	Port           int               `json:"port"`
	ProductVersion string            `json:"product_version"`
	APIVersion     string            `json:"api_version"`
	Build          string            `json:"build"`
	ProductName    string            `json:"product_name"`
	InstanceUUID   string            `json:"instance_uuid"`
	TagsCapability string            `json:"tags_capability"` // "available" or "unavailable"
	Fingerprint    string            `json:"fingerprint,omitempty"`
	Capabilities   map[string]string `json:"capabilities"`
}

// ValidationError is a typed error returned by the validation pipeline with a stable code.
type ValidationError struct {
	Code        string         `json:"code"` // unreachable | tls_untrusted | auth_failed | unsupported_version | insufficient_privileges | timeout | address_not_allowed
	Message     string         `json:"message"`
	Fingerprint string         `json:"fingerprint,omitempty"`
	Details     map[string]any `json:"details,omitempty"`
}

func (e *ValidationError) Error() string {
	if e.Fingerprint != "" {
		return fmt.Sprintf("%s: %s (fingerprint: %s)", e.Code, e.Message, e.Fingerprint)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// FormatFingerprint formats a SHA-256 digest as a colon-separated uppercase hex string.
func FormatFingerprint(b []byte) string {
	h := sha256.Sum256(b)
	parts := make([]string, len(h))
	for i, v := range h {
		parts[i] = fmt.Sprintf("%02X", v)
	}
	return strings.Join(parts, ":")
}

// NormalizeFingerprint strips separators and converts to uppercase for comparison.
func NormalizeFingerprint(fp string) string {
	fp = strings.ReplaceAll(fp, ":", "")
	fp = strings.ReplaceAll(fp, " ", "")
	fp = strings.ReplaceAll(fp, "-", "")
	return strings.ToUpper(fp)
}

// ValidateConnection executes the complete 7-step pre-registration validation pipeline.
func ValidateConnection(ctx context.Context, params ValidationParams) (*ValidationResult, error) {
	if params.Port == 0 {
		params.Port = 443
	}
	if params.OverallTimeout <= 0 {
		params.OverallTimeout = 30 * time.Second
	}
	if params.StepTimeout <= 0 {
		params.StepTimeout = 10 * time.Second
	}

	overallCtx, overallCancel := context.WithTimeout(ctx, params.OverallTimeout)
	defer overallCancel()

	// 1. SSRF Check & DNS Resolution
	policy := params.Policy
	if policy == nil {
		policy = netutil.DefaultPolicy()
	}
	if params.AllowPrivate {
		policy.AllowPrivateRanges = true
	}
	if params.AllowLoopback {
		policy.AllowLoopback = true
	}

	resolvedIP, err := policy.ResolveAndValidate(overallCtx, params.Host, params.Port)
	if err != nil {
		if errors.Is(err, netutil.ErrAddressNotAllowed) || errors.Is(err, netutil.ErrDisallowedPort) || errors.Is(err, netutil.ErrNonHTTPSScheme) {
			return nil, &ValidationError{
				Code:    "address_not_allowed",
				Message: err.Error(),
			}
		}
		if errors.Is(err, context.DeadlineExceeded) || overallCtx.Err() != nil {
			return nil, &ValidationError{Code: "timeout", Message: "DNS resolution timed out"}
		}
		return nil, &ValidationError{
			Code:    "unreachable",
			Message: fmt.Sprintf("failed to resolve host %q: %v", params.Host, err),
		}
	}

	// 2. TCP & TLS Handshake
	stepCtx, stepCancel := context.WithTimeout(overallCtx, params.StepTimeout)
	defer stepCancel()

	dialer := &net.Dialer{Timeout: params.StepTimeout}
	rawConn, err := dialer.DialContext(stepCtx, "tcp", net.JoinHostPort(resolvedIP.String(), strconv.Itoa(params.Port)))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || stepCtx.Err() != nil {
			return nil, &ValidationError{Code: "timeout", Message: "TCP dial timed out"}
		}
		return nil, &ValidationError{Code: "unreachable", Message: fmt.Sprintf("TCP dial to %s:%d failed: %v", params.Host, params.Port, err)}
	}

	var mu sync.Mutex
	var presentedFingerprint string
	var handshakeErr error

	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: params.Host,
		// #nosec G402 -- Custom certificate verification and fingerprint pinning performed in VerifyConnection
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			mu.Lock()
			defer mu.Unlock()

			if len(cs.PeerCertificates) == 0 {
				return errors.New("no peer certificates presented")
			}
			leaf := cs.PeerCertificates[0]
			presentedFingerprint = FormatFingerprint(leaf.Raw)

			// Check pinned fingerprint if provided
			if params.TLS.PinnedFingerprint != nil && *params.TLS.PinnedFingerprint != "" {
				if NormalizeFingerprint(*params.TLS.PinnedFingerprint) == NormalizeFingerprint(presentedFingerprint) {
					return nil // Trusted via pinned fingerprint
				}
				return fmt.Errorf("fingerprint mismatch: expected %s, got %s", *params.TLS.PinnedFingerprint, presentedFingerprint)
			}

			// If verification disabled, accept
			if !params.TLS.Verify {
				return nil
			}

			// Verify certificate chain
			var roots *x509.CertPool
			if params.TLS.CAPEM != "" {
				roots = x509.NewCertPool()
				if !roots.AppendCertsFromPEM([]byte(params.TLS.CAPEM)) {
					return errors.New("failed to parse custom CA PEM")
				}
			}

			intermediates := x509.NewCertPool()
			for _, cert := range cs.PeerCertificates[1:] {
				intermediates.AddCert(cert)
			}

			verifyOpts := x509.VerifyOptions{
				DNSName:       params.Host,
				Roots:         roots,
				Intermediates: intermediates,
			}

			if _, err := leaf.Verify(verifyOpts); err != nil {
				return err
			}
			return nil
		},
	}

	tlsConn := tls.Client(rawConn, tlsCfg)
	handshakeErr = tlsConn.HandshakeContext(stepCtx)
	_ = tlsConn.Close()

	if handshakeErr != nil {
		if errors.Is(handshakeErr, context.DeadlineExceeded) || stepCtx.Err() != nil {
			return nil, &ValidationError{Code: "timeout", Message: "TLS handshake timed out"}
		}
		mu.Lock()
		fp := presentedFingerprint
		mu.Unlock()

		if fp != "" {
			return nil, &ValidationError{
				Code:        "tls_untrusted",
				Message:     fmt.Sprintf("TLS certificate untrusted: %v", handshakeErr),
				Fingerprint: fp,
			}
		}
		return nil, &ValidationError{
			Code:    "unreachable",
			Message: fmt.Sprintf("TLS handshake failed: %v", handshakeErr),
		}
	}

	// 3. SOAP Authentication with credentials
	stepCtx2, stepCancel2 := context.WithTimeout(overallCtx, params.StepTimeout)
	defer stepCancel2()

	endpointURL, err := url.Parse(fmt.Sprintf("https://%s:%d/sdk", params.Host, params.Port))
	if err != nil {
		return nil, &ValidationError{Code: "unreachable", Message: fmt.Sprintf("invalid vCenter URL: %v", err)}
	}
	endpointURL.User = url.UserPassword(params.Username, params.Password)

	soapClient := soap.NewClient(endpointURL, true)
	transport := soapClient.DefaultTransport()
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		targetAddr := net.JoinHostPort(resolvedIP.String(), strconv.Itoa(params.Port))
		var d net.Dialer
		return d.DialContext(ctx, network, targetAddr)
	}
	transport.TLSClientConfig = tlsCfg

	vimClient, err := vim25.NewClient(stepCtx2, soapClient)
	if err != nil {
		return nil, classifyValidationError(err, presentedFingerprint)
	}

	govClient := &govmomi.Client{
		Client:         vimClient,
		SessionManager: session.NewManager(vimClient),
	}

	err = govClient.Login(stepCtx2, endpointURL.User)
	if err != nil {
		return nil, classifyValidationError(err, presentedFingerprint)
	}

	// Ensure session logout at completion
	defer func() {
		logoutCtx, logoutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer logoutCancel()
		_ = govClient.Logout(logoutCtx)
	}()

	// 4. ServiceContent.About Version Detection (enforce >= 7.0 via ADR-018)
	about := vimClient.ServiceContent.About
	if about.ApiType != "" && about.ApiType != "VirtualCenter" {
		return nil, &ValidationError{
			Code:    "unsupported_version",
			Message: fmt.Sprintf("expected VirtualCenter endpoint, got %q (direct ESXi connections unsupported)", about.ApiType),
		}
	}

	major, err := parseMajorVersion(about.Version, about.ApiVersion)
	if err != nil || major < 7 {
		return nil, &ValidationError{
			Code:    "unsupported_version",
			Message: fmt.Sprintf("vSphere version %q is unsupported (vSphere 7.0 and above required)", about.Version),
		}
	}

	// 5. Read-only inventory probe sanity check
	stepCtx3, stepCancel3 := context.WithTimeout(overallCtx, params.StepTimeout)
	defer stepCancel3()

	var rootFolder mo.Folder
	pc := property.DefaultCollector(vimClient)
	err = pc.RetrieveOne(stepCtx3, vimClient.ServiceContent.RootFolder, []string{"name", "childEntity"}, &rootFolder)
	if err != nil {
		if soap.IsSoapFault(err) {
			fault := soap.ToSoapFault(err)
			if fault != nil && (fault.String == "NoPermission" || strings.Contains(fault.String, "Permission")) {
				return nil, &ValidationError{
					Code:    "insufficient_privileges",
					Message: "account lacks read privileges on root inventory folder",
				}
			}
		}
		if errors.Is(err, context.DeadlineExceeded) || stepCtx3.Err() != nil {
			return nil, &ValidationError{Code: "timeout", Message: "inventory probe timed out"}
		}
		return nil, &ValidationError{
			Code:    "insufficient_privileges",
			Message: fmt.Sprintf("read-only inventory probe failed: %v", err),
		}
	}

	// 6. REST vAPI Tagging Probe
	tagsCapability := "unavailable"
	restClient := rest.NewClient(vimClient)
	restCtx, restCancel := context.WithTimeout(overallCtx, 5*time.Second)
	defer restCancel()

	if err := restClient.Login(restCtx, endpointURL.User); err == nil {
		tagMgr := tags.NewManager(restClient)
		if _, err := tagMgr.GetCategories(restCtx); err == nil {
			tagsCapability = "available"
		}
		_ = restClient.Logout(restCtx)
	}

	// 7. Success
	return &ValidationResult{
		Host:           params.Host,
		Port:           params.Port,
		ProductVersion: about.Version,
		APIVersion:     about.ApiVersion,
		Build:          about.Build,
		ProductName:    about.FullName,
		InstanceUUID:   about.InstanceUuid,
		TagsCapability: tagsCapability,
		Fingerprint:    presentedFingerprint,
		Capabilities: map[string]string{
			"tags": tagsCapability,
		},
	}, nil
}

func classifyValidationError(err error, fingerprint string) *ValidationError {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return &ValidationError{Code: "timeout", Message: "request timed out"}
	}

	if soap.IsSoapFault(err) {
		fault := soap.ToSoapFault(err)
		if fault != nil {
			vimFault := fault.VimFault()
			if _, ok := vimFault.(*types.InvalidLogin); ok || fault.String == "InvalidLogin" {
				return &ValidationError{Code: "auth_failed", Message: "invalid vCenter credentials"}
			}
			if _, ok := vimFault.(*types.NoPermission); ok || fault.String == "NoPermission" {
				return &ValidationError{Code: "insufficient_privileges", Message: "insufficient vSphere privileges"}
			}
		}
		return &ValidationError{Code: "auth_failed", Message: fmt.Sprintf("SOAP authentication failed: %v", err)}
	}

	var certErr x509.CertificateInvalidError
	var unknownAuth x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	if errors.As(err, &certErr) || errors.As(err, &unknownAuth) || errors.As(err, &hostnameErr) {
		return &ValidationError{
			Code:        "tls_untrusted",
			Message:     fmt.Sprintf("TLS certificate validation failed: %v", err),
			Fingerprint: fingerprint,
		}
	}

	return &ValidationError{Code: "unreachable", Message: fmt.Sprintf("endpoint unreachable: %v", err)}
}
