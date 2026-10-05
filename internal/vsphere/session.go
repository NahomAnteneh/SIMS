package vsphere

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/infrastructure/sims/internal/logging"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

var (
	ErrTimeout               = errors.New("vsphere request timeout")
	ErrFault                 = errors.New("vsphere soap fault")
	ErrInvalidCredentials    = errors.New("invalid vsphere credentials")
	ErrNotConnected          = errors.New("vsphere client not connected")
	ErrCertificateValidation = errors.New("vsphere certificate validation failed")
	ErrSessionExpired        = errors.New("vsphere session expired")
	ErrUnsupportedVersion    = errors.New("unsupported_version")
)

// UnsupportedVersionError represents an endpoint version that is strictly below 7.0.
type UnsupportedVersionError struct {
	Version    string
	ApiVersion string
	Product    string
}

func (e *UnsupportedVersionError) Error() string {
	return fmt.Sprintf("unsupported_version: %s (vSphere 7.0 and above required)", e.Version)
}

func (e *UnsupportedVersionError) Is(target error) bool {
	return target == ErrUnsupportedVersion
}

// SessionConfig configures vSphere connection and security parameters.
type SessionConfig struct {
	URL            string
	Username       string
	Password       string
	Insecure       bool
	RootCAs        []byte
	TLSMinVersion  uint16        // default: tls.VersionTLS12
	RequestTimeout time.Duration // default: 15s
}

// SessionManager manages vSphere client lifecycle and SOAP operations under explicit timeouts.
type SessionManager struct {
	cfg SessionConfig
	mu  sync.RWMutex

	client *govmomi.Client
	user   *url.Userinfo
}

// NewSessionManager creates a new SessionManager with validated configuration defaults.
func NewSessionManager(cfg SessionConfig) (*SessionManager, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("vcenter URL is required")
	}
	if cfg.TLSMinVersion == 0 {
		cfg.TLSMinVersion = tls.VersionTLS12
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 15 * time.Second
	}

	var userInfo *url.Userinfo
	if cfg.Username != "" {
		userInfo = url.UserPassword(cfg.Username, cfg.Password)
	}

	return &SessionManager{
		cfg:  cfg,
		user: userInfo,
	}, nil
}

// Connect initializes the SOAP transport and connects to the vCenter endpoint.
func (sm *SessionManager) Connect(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	return sm.connectLocked(ctx)
}

func (sm *SessionManager) connectLocked(ctx context.Context) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, sm.cfg.RequestTimeout)
	defer cancel()

	u, err := soap.ParseURL(sm.cfg.URL)
	if err != nil {
		return fmt.Errorf("failed to parse vcenter url %s: %w", logging.RedactURL(sm.cfg.URL), err)
	}
	u.User = sm.user

	soapClient := soap.NewClient(u, sm.cfg.Insecure)
	transport := soapClient.DefaultTransport()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.MinVersion = sm.cfg.TLSMinVersion

	if len(sm.cfg.RootCAs) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(sm.cfg.RootCAs) {
			return fmt.Errorf("%w: failed to parse custom root CA PEM", ErrCertificateValidation)
		}
		transport.TLSClientConfig.RootCAs = pool
	}

	vimClient, err := vim25.NewClient(timeoutCtx, soapClient)
	if err != nil {
		return sm.classifyError(err)
	}

	about := vimClient.ServiceContent.About
	major, err := parseMajorVersion(about.Version, about.ApiVersion)
	if err != nil {
		return fmt.Errorf("failed to parse vcenter version: %w", err)
	}
	if major < 7 {
		return &UnsupportedVersionError{
			Version:    about.Version,
			ApiVersion: about.ApiVersion,
			Product:    about.FullName,
		}
	}

	sm.client = &govmomi.Client{
		Client:         vimClient,
		SessionManager: session.NewManager(vimClient),
	}

	return nil
}

// Login authenticates an active session with vCenter using configured credentials.
func (sm *SessionManager) Login(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.client == nil {
		if err := sm.connectLocked(ctx); err != nil {
			return err
		}
	}

	if sm.user == nil {
		return nil
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, sm.cfg.RequestTimeout)
	defer cancel()

	err := sm.client.Login(timeoutCtx, sm.user)
	if err != nil {
		return sm.classifyError(err)
	}
	return nil
}

// CurrentTime retrieves the server time from vCenter.
func (sm *SessionManager) CurrentTime(ctx context.Context) (time.Time, error) {
	sm.mu.RLock()
	client := sm.client
	sm.mu.RUnlock()

	if client == nil {
		return time.Time{}, ErrNotConnected
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, sm.cfg.RequestTimeout)
	defer cancel()

	t, err := methods.GetCurrentTime(timeoutCtx, client)
	if err != nil {
		return time.Time{}, sm.classifyError(err)
	}
	if t == nil {
		return time.Time{}, fmt.Errorf("vcenter returned nil current time")
	}
	return *t, nil
}

// Logout terminates the authenticated vCenter session cleanly.
func (sm *SessionManager) Logout(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.client == nil {
		return nil
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, sm.cfg.RequestTimeout)
	defer cancel()

	err := sm.client.Logout(timeoutCtx)
	sm.client = nil
	if err != nil {
		return sm.classifyError(err)
	}
	return nil
}

// Reconnect tears down any existing session and creates a fresh authenticated session.
func (sm *SessionManager) Reconnect(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.client != nil {
		_ = sm.client.Logout(ctx)
		sm.client = nil
	}

	if err := sm.connectLocked(ctx); err != nil {
		return err
	}

	if sm.user != nil {
		timeoutCtx, cancel := context.WithTimeout(ctx, sm.cfg.RequestTimeout)
		defer cancel()
		if err := sm.client.Login(timeoutCtx, sm.user); err != nil {
			return sm.classifyError(err)
		}
	}
	return nil
}

// Client returns the underlying govmomi client, or nil if not connected.
func (sm *SessionManager) Client() *govmomi.Client {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.client
}

// UserInfo returns the user credentials used for session login.
func (sm *SessionManager) UserInfo() *url.Userinfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.user
}

// classifyError maps raw errors into typed platform errors.
func (sm *SessionManager) classifyError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	}

	// Check certificate errors
	var certErr x509.CertificateInvalidError
	var unknownAuth x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	if errors.As(err, &certErr) || errors.As(err, &unknownAuth) || errors.As(err, &hostnameErr) {
		return fmt.Errorf("%w: %v", ErrCertificateValidation, err)
	}

	// Check SOAP faults
	if soap.IsSoapFault(err) {
		fault := soap.ToSoapFault(err)
		if fault != nil {
			vimFault := fault.VimFault()
			if _, ok := vimFault.(*types.InvalidLogin); ok || fault.String == "InvalidLogin" {
				return fmt.Errorf("%w: %v", ErrInvalidCredentials, err)
			}
			if _, ok := vimFault.(*types.NotAuthenticated); ok {
				return fmt.Errorf("%w: %v", ErrSessionExpired, err)
			}
		}
		return fmt.Errorf("%w: %v", ErrFault, err)
	}

	return err
}

// About returns the vCenter AboutInfo metadata retrieved at connection time,
// or nil if the client is not connected.
func (sm *SessionManager) About() *types.AboutInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if sm.client == nil {
		return nil
	}
	return &sm.client.ServiceContent.About
}

// parseMajorVersion extracts the leading integer major version from a version or apiVersion string.
func parseMajorVersion(version, apiVersion string) (int, error) {
	v := strings.TrimSpace(version)
	if v == "" {
		v = strings.TrimSpace(apiVersion)
	}
	if v == "" {
		return 0, fmt.Errorf("version and apiVersion are both empty")
	}
	parts := strings.Split(v, ".")
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("invalid major version %q in %q: %w", parts[0], v, err)
	}
	return major, nil
}
