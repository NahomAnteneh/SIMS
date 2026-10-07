package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrAuthDisabledInProduction = errors.New("cannot disable authentication in production environment")
	ErrMissingBearerToken       = errors.New("missing or malformed authorization header")
	ErrInvalidToken             = errors.New("invalid jwt token")
	ErrExpiredToken             = errors.New("token has expired")
	ErrInvalidIssuer            = errors.New("token issuer mismatch")
	ErrInvalidAudience          = errors.New("token audience mismatch")
	ErrKeyNotFound              = errors.New("signing key not found in jwks")
)

// Config configures the OIDC authenticator.
type Config struct {
	IssuerURL    string
	Audience     string
	JWKSURL      string
	AuthDisabled bool
	Env          string
}

// Authenticator validates Bearer JWT tokens against upstream OIDC provider or local credentials.
type Authenticator struct {
	cfg        Config
	httpClient *http.Client

	mu          sync.RWMutex
	keys        map[string]*rsa.PublicKey
	lastRefresh time.Time

	tokenSigner *TokenSigner
	userStore   UserStore
}

// SetTokenSigner configures local JWT token validation and issuance.
func (a *Authenticator) SetTokenSigner(s *TokenSigner) {
	a.tokenSigner = s
}

// TokenSigner returns the configured TokenSigner.
func (a *Authenticator) TokenSigner() *TokenSigner {
	return a.tokenSigner
}

// SetUserStore configures credential storage for local username and password verification.
func (a *Authenticator) SetUserStore(s UserStore) {
	a.userStore = s
}

// UserStore returns the configured UserStore.
func (a *Authenticator) UserStore() UserStore {
	return a.userStore
}

// NewAuthenticator creates and initializes an Authenticator.
func NewAuthenticator(ctx context.Context, cfg Config) (*Authenticator, error) {
	// Guard invariant: AUTH_DISABLED=true refuses to start if ENV=production
	if cfg.AuthDisabled {
		env := strings.ToLower(strings.TrimSpace(cfg.Env))
		if env == "production" || env == "prod" {
			return nil, ErrAuthDisabledInProduction
		}
		return &Authenticator{cfg: cfg}, nil
	}

	a := &Authenticator{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		keys:       make(map[string]*rsa.PublicKey),
	}

	// Discover JWKS URL if omitted
	if a.cfg.JWKSURL == "" && a.cfg.IssuerURL != "" {
		jwksURL, err := a.discoverJWKS(ctx)
		if err != nil {
			return nil, fmt.Errorf("oidc discovery failed: %w", err)
		}
		a.cfg.JWKSURL = jwksURL
	}

	// Fetch initial keys
	if a.cfg.JWKSURL != "" {
		if err := a.refreshJWKS(ctx); err != nil {
			return nil, fmt.Errorf("initial jwks fetch failed: %w", err)
		}
	}

	return a, nil
}

func (a *Authenticator) discoverJWKS(ctx context.Context) (string, error) {
	wellKnown := strings.TrimSuffix(a.cfg.IssuerURL, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnown, nil)
	if err != nil {
		return "", err
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d from %s", resp.StatusCode, wellKnown)
	}

	var doc struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", err
	}
	if doc.JWKSURI == "" {
		return "", errors.New("jwks_uri missing in openid-configuration")
	}
	return doc.JWKSURI, nil
}

type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwksDoc struct {
	Keys []jwkKey `json:"keys"`
}

func (a *Authenticator) refreshJWKS(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.JWKSURL, nil)
	if err != nil {
		return err
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d from jwks uri %s", resp.StatusCode, a.cfg.JWKSURL)
	}

	var doc jwksDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return err
	}

	newKeys := make(map[string]*rsa.PublicKey)
	for _, k := range doc.Keys {
		if k.Kty == "RSA" && k.N != "" && k.E != "" {
			pubKey, err := parseRSAPublicKey(k.N, k.E)
			if err == nil {
				newKeys[k.Kid] = pubKey
			}
		}
	}

	a.mu.Lock()
	a.keys = newKeys
	a.lastRefresh = time.Now()
	a.mu.Unlock()

	return nil
}

func parseRSAPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, err
	}

	n := new(big.Int).SetBytes(nBytes)
	var e int
	if len(eBytes) < 4 {
		padded := make([]byte, 4)
		copy(padded[4-len(eBytes):], eBytes)
		e = int(binary.BigEndian.Uint32(padded))
	} else {
		e = int(binary.BigEndian.Uint32(eBytes))
	}

	return &rsa.PublicKey{
		N: n,
		E: e,
	}, nil
}

// AuthenticateToken verifies the JWT string and returns caller Identity.
func (a *Authenticator) AuthenticateToken(ctx context.Context, tokenStr string) (*Identity, error) {
	// Dev bypass mode
	if a.cfg.AuthDisabled {
		return &Identity{
			Subject: "dev-admin@sims.local",
			Email:   "dev-admin@sims.local",
			Role:    RoleAdmin,
		}, nil
	}

	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid header base64", ErrInvalidToken)
	}

	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("%w: invalid header json", ErrInvalidToken)
	}

	if header.Alg != "RS256" {
		return nil, fmt.Errorf("%w: unsupported alg %s", ErrInvalidToken, header.Alg)
	}

	// Lookup key
	a.mu.RLock()
	pubKey, ok := a.keys[header.Kid]
	lastRef := a.lastRefresh
	a.mu.RUnlock()

	if !ok {
		// Key rotation: try refreshing if cooldown (5s) has passed
		if time.Since(lastRef) > 5*time.Second && a.cfg.JWKSURL != "" {
			_ = a.refreshJWKS(ctx)
			a.mu.RLock()
			pubKey, ok = a.keys[header.Kid]
			a.mu.RUnlock()
		}
	}

	if !ok || pubKey == nil {
		return nil, ErrKeyNotFound
	}

	// Verify signature
	signedContent := parts[0] + "." + parts[1]
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid signature base64", ErrInvalidToken)
	}

	hashed := sha256.Sum256([]byte(signedContent))
	if err := rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hashed[:], sigBytes); err != nil {
		return nil, fmt.Errorf("%w: signature verification failed", ErrInvalidToken)
	}

	// Parse payload
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid payload base64", ErrInvalidToken)
	}

	var claims struct {
		Iss         string   `json:"iss"`
		Aud         any      `json:"aud"`
		Exp         int64    `json:"exp"`
		Sub         string   `json:"sub"`
		Email       string   `json:"email"`
		Role        string   `json:"role"`
		Roles       []string `json:"roles"`
		Groups      []string `json:"groups"`
		VCenterIDs  []string `json:"vcenter_ids"`
		FolderPaths []string `json:"folder_paths"`
	}
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, fmt.Errorf("%w: invalid payload json", ErrInvalidToken)
	}

	// Validate issuer
	if a.cfg.IssuerURL != "" && claims.Iss != a.cfg.IssuerURL {
		return nil, ErrInvalidIssuer
	}

	// Validate audience
	if a.cfg.Audience != "" {
		audMatch := false
		switch v := claims.Aud.(type) {
		case string:
			audMatch = (v == a.cfg.Audience)
		case []any:
			for _, item := range v {
				if strItem, ok := item.(string); ok && strItem == a.cfg.Audience {
					audMatch = true
					break
				}
			}
		}
		if !audMatch {
			return nil, ErrInvalidAudience
		}
	}

	// Validate expiry (required; 1 min leeway)
	if claims.Exp == 0 || time.Now().Unix() > claims.Exp+60 {
		return nil, ErrExpiredToken
	}

	// Determine Role: default to RoleUser, grant RoleAdmin for admin or inventory-admin
	role := RoleUser
	if strings.EqualFold(claims.Role, "admin") || strings.EqualFold(claims.Role, "inventory-admin") {
		role = RoleAdmin
	}
	for _, r := range claims.Roles {
		if strings.EqualFold(r, "admin") || strings.EqualFold(r, "inventory-admin") {
			role = RoleAdmin
		}
	}
	for _, g := range claims.Groups {
		if strings.EqualFold(g, "admin") || strings.EqualFold(g, "inventory-admin") {
			role = RoleAdmin
		}
	}

	// Parse vcenter UUIDs
	var vcenterUUIDs []uuid.UUID
	for _, idStr := range claims.VCenterIDs {
		if u, err := uuid.Parse(idStr); err == nil {
			vcenterUUIDs = append(vcenterUUIDs, u)
		}
	}

	sub := claims.Sub
	if sub == "" {
		sub = claims.Email
	}

	return &Identity{
		Subject:            sub,
		Email:              claims.Email,
		Role:               role,
		AllowedVCenterIDs:  vcenterUUIDs,
		AllowedFolderPaths: claims.FolderPaths,
	}, nil
}

// Middleware returns an HTTP middleware verifying authentication.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Public health, metrics & login bypass
		path := r.URL.Path
		if path == "/healthz" || path == "/readyz" || path == "/metrics" || path == "/v1/auth/login" {
			next.ServeHTTP(w, r)
			return
		}

		if a.cfg.AuthDisabled {
			id, _ := a.AuthenticateToken(r.Context(), "")
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"about:blank","title":"Unauthorized","status":401,"detail":"missing authorization header","code":"unauthorized"}`))
			return
		}

		// Support Basic Auth
		if strings.HasPrefix(strings.ToLower(authHeader), "basic ") {
			if a.userStore == nil {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"type":"about:blank","title":"Unauthorized","status":401,"detail":"basic auth is not enabled","code":"unauthorized"}`))
				return
			}
			rawB64 := strings.TrimSpace(authHeader[6:])
			decoded, err := base64.StdEncoding.DecodeString(rawB64)
			if err != nil {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"type":"about:blank","title":"Unauthorized","status":401,"detail":"malformed basic auth header","code":"unauthorized"}`))
				return
			}
			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) != 2 {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"type":"about:blank","title":"Unauthorized","status":401,"detail":"malformed basic credentials","code":"unauthorized"}`))
				return
			}
			id, err := a.userStore.AuthenticateUser(r.Context(), parts[0], parts[1])
			if err != nil {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"type":"about:blank","title":"Unauthorized","status":401,"detail":"invalid username or password","code":"unauthorized"}`))
				return
			}
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
			return
		}

		// Support Bearer Auth
		if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			tokenStr := strings.TrimSpace(authHeader[7:])
			if a.tokenSigner != nil {
				if id, err := a.tokenSigner.VerifyToken(tokenStr); err == nil {
					next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
					return
				}
			}

			if a.cfg.IssuerURL != "" || a.cfg.JWKSURL != "" {
				id, err := a.AuthenticateToken(r.Context(), tokenStr)
				if err == nil {
					next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
					return
				}
			}

			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"about:blank","title":"Unauthorized","status":401,"detail":"invalid or expired bearer token","code":"unauthorized"}`))
			return
		}

		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Unauthorized","status":401,"detail":"unsupported authorization scheme (use Bearer or Basic)","code":"unauthorized"}`))
	})
}
