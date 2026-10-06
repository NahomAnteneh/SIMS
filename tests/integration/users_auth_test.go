//go:build integration
// +build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	db "github.com/infrastructure/sims/generated/db"
	"github.com/infrastructure/sims/internal/api"
	"github.com/infrastructure/sims/internal/auth"
	"github.com/infrastructure/sims/internal/logging"
	"github.com/jackc/pgx/v5/pgxpool"
)

func setupTestAuthAPI(t *testing.T) (http.Handler, *db.Queries) {
	ctx := context.Background()
	connStr := getConnString("collector_rw", "collector_rw_pass")
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	queries := db.New(pool)
	logger := logging.DefaultLogger("error")

	// Ensure migration 15 is applied and admin user exists
	_, _ = pool.Exec(ctx, `
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('admin', '$2a$10$yumf2.g1cGQdiOQKODxySOy7B95gd4Z72Q6azRY1BvN9ZIT8j26aK', 'admin@sims.local', 'inventory-admin')
		ON CONFLICT (username) DO NOTHING;
	`)

	userStore := auth.NewDBUserStore(queries)
	tokenSigner := auth.NewTokenSigner([]byte("test-auth-jwt-secret-key-32b-secure!"), 1*time.Hour)

	authCfg := auth.Config{
		AuthDisabled: false,
		Env:          "testing",
	}
	authenticator, err := auth.NewAuthenticator(ctx, authCfg)
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}
	authenticator.SetTokenSigner(tokenSigner)
	authenticator.SetUserStore(userStore)

	mux := http.NewServeMux()
	handler := api.NewHandler(pool, logger)
	handler.SetAuth(tokenSigner, userStore)
	handler.RegisterRoutes(mux)

	wrapped := authenticator.Middleware(mux)
	return wrapped, queries
}

func TestLocalAuth_Login_SuccessAndFailure(t *testing.T) {
	handler, _ := setupTestAuthAPI(t)

	// 1. Success case: admin / admin123
	loginBody, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "admin123",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(loginBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Token     string `json:"token"`
		TokenType string `json:"token_type"`
		User      struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("expected non-empty token")
	}
	if resp.User.Username != "admin" {
		t.Errorf("expected username 'admin', got %s", resp.User.Username)
	}
	if resp.User.Role != "inventory-admin" {
		t.Errorf("expected role 'inventory-admin', got %s", resp.User.Role)
	}

	// 2. Failure case: wrong password
	badBody, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "wrongpassword",
	})
	req = httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(badBody))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
	}

	// 3. Failure case: nonexistent user
	noUserBody, _ := json.Marshal(map[string]string{
		"username": "nosuchuser",
		"password": "password",
	})
	req = httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(noUserBody))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
	}
}

func TestLocalAuth_BearerAndBasic_ProtectedEndpoints(t *testing.T) {
	handler, _ := setupTestAuthAPI(t)

	// 1. Unauthenticated request to /v1/me should fail
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated /v1/me, got %d", rec.Code)
	}

	// 2. Authenticate and obtain Bearer token
	loginBody, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "admin123",
	})
	req = httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(loginBody))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var loginResp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &loginResp)

	// 3. Request /v1/me using Bearer token
	req = httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+loginResp.Token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with Bearer token, got %d: %s", rec.Code, rec.Body.String())
	}
	var meResp struct {
		Subject string `json:"subject"`
		Role    string `json:"role"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &meResp)
	if meResp.Subject != "admin" || meResp.Role != "inventory-admin" {
		t.Errorf("unexpected /v1/me response: %+v", meResp)
	}

	// 4. Request /v1/me using HTTP Basic Auth
	basicAuth := base64.StdEncoding.EncodeToString([]byte("admin:admin123"))
	req = httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Basic "+basicAuth)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with Basic Auth, got %d: %s", rec.Code, rec.Body.String())
	}

	// 5. Basic Auth with invalid password
	badBasicAuth := base64.StdEncoding.EncodeToString([]byte("admin:badpass"))
	req = httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Basic "+badBasicAuth)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with bad Basic Auth, got %d", rec.Code)
	}
}

func TestUserManagement_LifecycleAndRBAC(t *testing.T) {
	handler, _ := setupTestAuthAPI(t)

	// Login as admin
	adminBasic := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:admin123"))

	// 1. Create a new user 'operator_sec' with role 'viewer'
	createUserBody, _ := json.Marshal(map[string]any{
		"username": "operator_sec",
		"password": "initial_password_123",
		"email":    "operator@corp.local",
		"role":     "viewer",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/users", bytes.NewReader(createUserBody))
	req.Header.Set("Authorization", adminBasic)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var createdUser struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &createdUser)
	if createdUser.Username != "operator_sec" || createdUser.Role != "viewer" {
		t.Errorf("unexpected created user: %+v", createdUser)
	}

	// Ensure no password hash is exposed
	if bytes.Contains(rec.Body.Bytes(), []byte("password_hash")) || bytes.Contains(rec.Body.Bytes(), []byte("initial_password_123")) {
		t.Fatal("SECURITY LEAK: user creation returned password details!")
	}

	// 2. Login as the newly created user
	opLoginBody, _ := json.Marshal(map[string]string{
		"username": "operator_sec",
		"password": "initial_password_123",
	})
	req = httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(opLoginBody))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for operator login, got %d", rec.Code)
	}
	var opLoginResp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &opLoginResp)

	// 3. Verify RBAC enforcement: 'viewer' role CANNOT create users
	forbiddenCreateBody, _ := json.Marshal(map[string]string{
		"username": "rogue_user",
		"password": "password123",
	})
	req = httptest.NewRequest(http.MethodPost, "/v1/users", bytes.NewReader(forbiddenCreateBody))
	req.Header.Set("Authorization", "Bearer "+opLoginResp.Token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for viewer user creation, got %d", rec.Code)
	}

	// 4. Update operator password as admin
	newPass := "updated_password_456"
	updateBody, _ := json.Marshal(map[string]string{
		"password": newPass,
	})
	req = httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/v1/users/%s", createdUser.ID), bytes.NewReader(updateBody))
	req.Header.Set("Authorization", adminBasic)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for user update, got %d: %s", rec.Code, rec.Body.String())
	}

	// 5. Verify old password fails and new password works
	oldLogin, _ := json.Marshal(map[string]string{"username": "operator_sec", "password": "initial_password_123"})
	req = httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(oldLogin))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for old password, got %d", rec.Code)
	}

	newLogin, _ := json.Marshal(map[string]string{"username": "operator_sec", "password": newPass})
	req = httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(newLogin))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for new password login, got %d", rec.Code)
	}

	// 6. Delete user
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/v1/users/%s", createdUser.ID), nil)
	req.Header.Set("Authorization", adminBasic)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content for user deletion, got %d", rec.Code)
	}

	// Verify user is gone
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/users/%s", createdUser.ID), nil)
	req.Header.Set("Authorization", adminBasic)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found after deletion, got %d", rec.Code)
	}
}
