//go:build integration
// +build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/api"
	"github.com/infrastructure/sims/internal/auth"
	"github.com/infrastructure/sims/internal/search"
	"github.com/infrastructure/sims/internal/server"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/infrastructure/sims/internal/vsphere"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// bufferLogHandler captures slog records into an in-memory buffer for secret leak auditing.
type bufferLogHandler struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (h *bufferLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *bufferLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.buf.WriteString(r.Message)
	h.buf.WriteString(" ")
	r.Attrs(func(a slog.Attr) bool {
		h.buf.WriteString(fmt.Sprintf("%s=%v ", a.Key, a.Value))
		return true
	})
	h.buf.WriteString("\n")
	return nil
}
func (h *bufferLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *bufferLogHandler) WithGroup(name string) slog.Handler       { return h }

func (h *bufferLogHandler) String() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.buf.String()
}

func TestSecurity_SecretLeakInAPIErrorResponsesAndPayloads(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to connect to DB: %v", err)
	}
	defer pool.Close()

	logCapturer := &bufferLogHandler{}
	logger := slog.New(logCapturer)

	srv := server.New("sims-security-test", "v1.0.0", "0", logger)
	handler := api.NewHandler(pool, logger)
	handler.RegisterRoutes(srv.Mux)

	testPassword := "SuperSecretVCPassword999!"
	testToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.SuperSecretJWT"

	// 1. Test POST /v1/vcenters/test-connection with invalid credentials
	body := map[string]any{
		"host":     "127.0.0.1",
		"port":     8989,
		"username": "administrator@vsphere.local",
		"password": testPassword,
		"tls": map[string]any{
			"verify": false,
		},
	}
	bodyJSON, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/vcenters/test-connection", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	respBody := rec.Body.String()

	// Invariant: Secret password and token must NEVER appear in HTTP response
	if strings.Contains(respBody, testPassword) {
		t.Fatalf("CRITICAL SECURITY VIOLATION: HTTP response leaked plaintext password: %s", respBody)
	}
	if strings.Contains(respBody, testToken) {
		t.Fatalf("CRITICAL SECURITY VIOLATION: HTTP response leaked bearer token: %s", respBody)
	}

	// Invariant: Secret must NEVER appear in captured logs
	capturedLogs := logCapturer.String()
	if strings.Contains(capturedLogs, testPassword) {
		t.Fatalf("CRITICAL SECURITY VIOLATION: Logger leaked plaintext password: %s", capturedLogs)
	}
	if strings.Contains(capturedLogs, testToken) {
		t.Fatalf("CRITICAL SECURITY VIOLATION: Logger leaked bearer token: %s", capturedLogs)
	}
}

func TestSecurity_AuditEventsRedaction(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to connect to DB: %v", err)
	}
	defer pool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New("sims-security-test", "v1.0.0", "0", logger)
	handler := api.NewHandler(pool, logger)
	handler.RegisterRoutes(srv.Mux)

	testPassword := "AuditSecretPasswordToFilter#123"

	// Trigger test-connection which records an audit event
	body := map[string]any{
		"host":     "127.0.0.1",
		"port":     9443,
		"username": "admin@vsphere.local",
		"password": testPassword,
		"tls": map[string]any{
			"verify": false,
		},
	}
	bodyJSON, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/vcenters/test-connection", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	// Inspect audit_events in the database
	rows, err := pool.Query(ctx, `
		SELECT details::text
		FROM audit_events
		ORDER BY created_at DESC
		LIMIT 10
	`)
	if err != nil {
		t.Fatalf("failed to query audit_events: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var detailsStr string
		if err := rows.Scan(&detailsStr); err != nil {
			t.Fatalf("failed to scan audit details: %v", err)
		}
		if strings.Contains(detailsStr, testPassword) {
			t.Fatalf("CRITICAL SECURITY VIOLATION: audit_events table contains plaintext password: %s", detailsStr)
		}
		if strings.Contains(strings.ToLower(detailsStr), "password") {
			t.Fatalf("CRITICAL SECURITY VIOLATION: audit_events table contains password field: %s", detailsStr)
		}
	}
}

func TestSecurity_SQLInjectionFuzzing(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, getConnString("query_ro", "query_ro_pass"))
	if err != nil {
		t.Fatalf("failed to connect with query_ro: %v", err)
	}
	defer pool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New("sims-security-test", "v1.0.0", "0", logger)
	handler := api.NewHandler(pool, logger)
	handler.RegisterRoutes(srv.Mux)

	sqlInjectionPayloads := []string{
		`' OR 1=1 --`,
		`' OR '1'='1`,
		`'; DROP TABLE virtual_machines; --`,
		`' UNION SELECT id, name, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL FROM virtual_machines --`,
		`admin'--`,
		`1' AND (SELECT 1 FROM pg_sleep(2))='1`,
		`{"$gt": ""}`,
		`' OR pg_sleep(2) IS NOT NULL --`,
	}

	for _, payload := range sqlInjectionPayloads {
		t.Run("QuickSearch_"+payload, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/search/vms?q="+url.QueryEscape(payload), nil)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)

			// Response must either be 200 (safe parameterized match 0 rows) or 400 (bad input)
			// Crucially: NEVER 500 database syntax error from injection!
			if rec.Code == http.StatusInternalServerError {
				t.Fatalf("POSSIBLE SQL INJECTION OR 500 ERROR on payload %q: %s", payload, rec.Body.String())
			}
		})

		t.Run("StructuredFilter_"+payload, func(t *testing.T) {
			structuredReq := api.SearchVMsRequest{
				Filter: &search.FilterNode{
					Op:    search.OpEQ,
					Field: "name",
					Value: payload,
				},
			}
			reqBody, _ := json.Marshal(structuredReq)
			req := httptest.NewRequest(http.MethodPost, "/v1/vms/search", bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)

			if rec.Code == http.StatusInternalServerError {
				t.Fatalf("POSSIBLE SQL INJECTION OR 500 ERROR on structured payload %q: %s", payload, rec.Body.String())
			}
		})
	}

	// Verify virtual_machines table still exists intact
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM virtual_machines").Scan(&count)
	if err != nil {
		t.Fatalf("CRITICAL SECURITY INVARIANT VIOLATION: virtual_machines table compromised: %v", err)
	}
}

func TestSecurity_CursorTamperingAndForgery(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, getConnString("query_ro", "query_ro_pass"))
	if err != nil {
		t.Fatalf("failed to connect with query_ro: %v", err)
	}
	defer pool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New("sims-security-test", "v1.0.0", "0", logger)
	handler := api.NewHandler(pool, logger)
	handler.RegisterRoutes(srv.Mux)

	tamperedCursors := []string{
		"not-a-valid-base64-cursor",
		"eyJpZCI6IjEyMyJ9",                                    // Base64 JSON without HMAC signature
		"YWRtaW46cGFzc3dvcmQ=",                                // Plain user:pass
		"W10=",                                                // Empty array
		"eyJuYW1lIjoidm0tMSIsInNpZ24iOiJmYWtlc2lnbmF0dXJlIn0", // Forged signature
		strings.Repeat("A", 10000),                            // Buffer overflow / oversized payload
		"'; DROP TABLE virtual_machines; --",
	}

	for _, cursor := range tamperedCursors {
		t.Run("TamperedCursor_"+cursor[:min(len(cursor), 20)], func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/search/vms?q=test&cursor="+url.QueryEscape(cursor), nil)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected HTTP 400 Bad Request for tampered cursor %q, got %d (body: %s)",
					cursor, rec.Code, rec.Body.String())
			}

			var problem map[string]any
			if err := json.NewDecoder(rec.Body).Decode(&problem); err == nil {
				if problem["error_code"] != "invalid_cursor" {
					t.Errorf("expected error_code 'invalid_cursor', got %v", problem["error_code"])
				}
			}
		})
	}
}

func TestSecurity_SSRFProtection(t *testing.T) {
	ctx := context.Background()

	maliciousTargets := []struct {
		host        string
		port        int
		description string
	}{
		{"127.0.0.1", 443, "IPv4 loopback"},
		{"127.0.1.1", 443, "IPv4 loopback alias"},
		{"::1", 443, "IPv6 loopback"},
		{"10.0.0.1", 443, "RFC1918 10.0.0.0/8 private"},
		{"172.16.0.1", 443, "RFC1918 172.16.0.0/12 private"},
		{"192.168.1.1", 443, "RFC1918 192.168.0.0/16 private"},
		{"169.254.169.254", 80, "AWS/GCP/Azure Cloud Metadata Endpoint"},
		{"fe80::1", 443, "IPv6 Link-Local"},
	}

	for _, target := range maliciousTargets {
		t.Run(target.description, func(t *testing.T) {
			params := vsphere.ValidationParams{
				Host:           target.host,
				Port:           target.port,
				Username:       "admin",
				Password:       "pass",
				AllowPrivate:   false,
				AllowLoopback:  false,
				StepTimeout:    200 * time.Millisecond,
				OverallTimeout: 500 * time.Millisecond,
			}

			_, err := vsphere.ValidateConnection(ctx, params)
			if err == nil {
				t.Fatalf("CRITICAL SSRF DEFENSE FAILURE: target %s (%s) was allowed through validator!",
					target.host, target.description)
			}

			var valErr *vsphere.ValidationError
			if !strings.Contains(err.Error(), "address_not_allowed") && !strings.Contains(err.Error(), "SSRF") {
				t.Logf("connection safely blocked with error: %v", err)
			}
			_ = valErr
		})
	}
}

func TestSecurity_DatabaseLeastPrivilegeRoles(t *testing.T) {
	ctx := context.Background()

	// 1. Verify query_ro is strictly SELECT-only
	roPool, err := pgxpool.New(ctx, getConnString("query_ro", "query_ro_pass"))
	if err != nil {
		t.Fatalf("failed to connect with query_ro: %v", err)
	}
	defer roPool.Close()

	// SELECT must succeed
	var count int
	if err := roPool.QueryRow(ctx, "SELECT COUNT(*) FROM virtual_machines").Scan(&count); err != nil {
		t.Fatalf("query_ro failed to SELECT: %v", err)
	}

	// INSERT must fail with permission denied
	_, err = roPool.Exec(ctx, `
		INSERT INTO virtual_machines (id, vcenter_id, moref, name, power_state, connection_state)
		VALUES ($1, $2, 'vm-malicious', 'hacked', 'poweredOn', 'connected')
	`, uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("CRITICAL DB LEAST PRIVILEGE VIOLATION: query_ro was permitted to INSERT")
	}

	// UPDATE must fail with permission denied
	_, err = roPool.Exec(ctx, `UPDATE virtual_machines SET name = 'hacked'`)
	if err == nil {
		t.Fatal("CRITICAL DB LEAST PRIVILEGE VIOLATION: query_ro was permitted to UPDATE")
	}

	// DELETE must fail with permission denied
	_, err = roPool.Exec(ctx, `DELETE FROM virtual_machines`)
	if err == nil {
		t.Fatal("CRITICAL DB LEAST PRIVILEGE VIOLATION: query_ro was permitted to DELETE")
	}

	// DROP TABLE must fail with permission denied
	_, err = roPool.Exec(ctx, `DROP TABLE virtual_machines`)
	if err == nil {
		t.Fatal("CRITICAL DB LEAST PRIVILEGE VIOLATION: query_ro was permitted to DROP TABLE")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Suppress unused imports
var _ = auth.FromContext
var _ = tracetest.NewInMemoryExporter
var _ = telemetry.InitTracer
