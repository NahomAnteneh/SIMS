package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func generateTestRSAKey(t *testing.T) (*rsa.PrivateKey, string, string) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	nBytes := key.PublicKey.N.Bytes()
	nStr := base64.RawURLEncoding.EncodeToString(nBytes)

	eBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(eBytes, uint32(key.PublicKey.E))
	// trim leading zeros
	for len(eBytes) > 1 && eBytes[0] == 0 {
		eBytes = eBytes[1:]
	}
	eStr := base64.RawURLEncoding.EncodeToString(eBytes)

	return key, nStr, eStr
}

func signJWT(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	header := map[string]string{
		"alg": "RS256",
		"kid": kid,
		"typ": "JWT",
	}
	hBytes, _ := json.Marshal(header)
	cBytes, _ := json.Marshal(claims)

	part1 := base64.RawURLEncoding.EncodeToString(hBytes)
	part2 := base64.RawURLEncoding.EncodeToString(cBytes)
	signedContent := part1 + "." + part2

	hashed := sha256.Sum256([]byte(signedContent))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hashed[:])
	if err != nil {
		t.Fatalf("failed to sign JWT: %v", err)
	}

	part3 := base64.RawURLEncoding.EncodeToString(sig)
	return signedContent + "." + part3
}

func TestOIDC_DevBypassGuardrails(t *testing.T) {
	ctx := context.Background()

	// 1. Refuses to start if AUTH_DISABLED=true and ENV=production
	_, err := NewAuthenticator(ctx, Config{
		AuthDisabled: true,
		Env:          "production",
	})
	if err != ErrAuthDisabledInProduction {
		t.Errorf("expected ErrAuthDisabledInProduction, got %v", err)
	}

	_, err = NewAuthenticator(ctx, Config{
		AuthDisabled: true,
		Env:          "prod",
	})
	if err != ErrAuthDisabledInProduction {
		t.Errorf("expected ErrAuthDisabledInProduction, got %v", err)
	}

	// 2. Succeeds in dev mode
	authDev, err := NewAuthenticator(ctx, Config{
		AuthDisabled: true,
		Env:          "development",
	})
	if err != nil {
		t.Fatalf("expected success in development, got %v", err)
	}

	id, err := authDev.AuthenticateToken(ctx, "any-token")
	if err != nil {
		t.Fatalf("expected bypass token success, got %v", err)
	}
	if id.Role != RoleInventoryAdmin {
		t.Errorf("expected dev admin role, got %s", id.Role)
	}
}

func TestOIDC_DiscoveryAndTokenVerification(t *testing.T) {
	ctx := context.Background()
	key1, n1, e1 := generateTestRSAKey(t)
	kid1 := "key-1"

	var mockJWKS jwksDoc
	mockJWKS.Keys = []jwkKey{
		{Kty: "RSA", Kid: kid1, Alg: "RS256", N: n1, E: e1},
	}

	// Mock OIDC IdP server
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"issuer":   serverURL,
				"jwks_uri": serverURL + "/jwks.json",
			})
		case "/jwks.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mockJWKS)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	auth, err := NewAuthenticator(ctx, Config{
		IssuerURL: serverURL,
		Audience:  "sims-api",
	})
	if err != nil {
		t.Fatalf("failed to initialize authenticator: %v", err)
	}

	// 1. Valid token
	validClaims := map[string]any{
		"iss":          serverURL,
		"aud":          "sims-api",
		"sub":          "user-123",
		"email":        "alice@corp.local",
		"role":         "inventory-admin",
		"vcenter_ids":  []string{"a0000000-0000-0000-0000-000000000001"},
		"folder_paths": []string{"Datacenters.DC1.vm"},
		"exp":          time.Now().Add(time.Hour).Unix(),
	}
	token := signJWT(t, key1, kid1, validClaims)

	id, err := auth.AuthenticateToken(ctx, token)
	if err != nil {
		t.Fatalf("expected valid token authentication, got: %v", err)
	}
	if id.Subject != "user-123" || id.Role != RoleInventoryAdmin {
		t.Errorf("unexpected identity: %+v", id)
	}
	if len(id.AllowedVCenterIDs) != 1 || len(id.AllowedFolderPaths) != 1 {
		t.Errorf("unexpected scope attributes in identity: %+v", id)
	}

	// 2. Expired token
	expiredClaims := map[string]any{
		"iss": serverURL,
		"aud": "sims-api",
		"sub": "user-expired",
		"exp": time.Now().Add(-2 * time.Hour).Unix(),
	}
	expiredToken := signJWT(t, key1, kid1, expiredClaims)
	_, err = auth.AuthenticateToken(ctx, expiredToken)
	if err != ErrExpiredToken {
		t.Errorf("expected ErrExpiredToken, got %v", err)
	}

	// 3. Issuer mismatch
	wrongIssClaims := map[string]any{
		"iss": "https://evil.idp.local",
		"aud": "sims-api",
		"sub": "user-hacker",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	wrongIssToken := signJWT(t, key1, kid1, wrongIssClaims)
	_, err = auth.AuthenticateToken(ctx, wrongIssToken)
	if err != ErrInvalidIssuer {
		t.Errorf("expected ErrInvalidIssuer, got %v", err)
	}

	// 4. Audience mismatch
	wrongAudClaims := map[string]any{
		"iss": serverURL,
		"aud": "wrong-audience",
		"sub": "user-other",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	wrongAudToken := signJWT(t, key1, kid1, wrongAudClaims)
	_, err = auth.AuthenticateToken(ctx, wrongAudToken)
	if err != ErrInvalidAudience {
		t.Errorf("expected ErrInvalidAudience, got %v", err)
	}

	// 5. Tampered token signature
	tamperedToken := token[:len(token)-5] + "XXXXX"
	_, err = auth.AuthenticateToken(ctx, tamperedToken)
	if err == nil {
		t.Errorf("expected tampered token rejection, but succeeded")
	}
}

func TestOIDC_Middleware(t *testing.T) {
	ctx := context.Background()
	key1, n1, e1 := generateTestRSAKey(t)
	kid1 := "key-mid"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jwksDoc{
			Keys: []jwkKey{{Kty: "RSA", Kid: kid1, Alg: "RS256", N: n1, E: e1}},
		})
	}))
	defer server.Close()

	auth, _ := NewAuthenticator(ctx, Config{
		IssuerURL: "https://auth.sims.local",
		Audience:  "sims",
		JWKSURL:   server.URL,
	})

	handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			id, ok := FromContext(r.Context())
			if !ok || id == nil {
				t.Errorf("identity missing in context")
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fmt.Sprintf("OK: %s", id.Subject)))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK: healthz"))
	}))

	// 1. Missing Authorization header -> 401
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/v1/vms", nil)
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec1.Code)
	}

	// 2. Healthz bypasses auth -> 200
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("expected 200 on healthz, got %d", rec2.Code)
	}

	// 3. Valid bearer token -> 200
	validClaims := map[string]any{
		"iss": "https://auth.sims.local",
		"aud": "sims",
		"sub": "bob@corp.local",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	token := signJWT(t, key1, kid1, validClaims)

	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/v1/vms", nil)
	req3.Header.Set("Authorization", "Bearer "+token)
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Errorf("expected 200 with valid token, got %d: %s", rec3.Code, rec3.Body.String())
	}
}
