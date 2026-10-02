package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/infrastructure/sims/internal/server"
)

func TestCoreEndpoints(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New("test-service", "0.1.0-test", "0", logger)

	// Test /healthz
	t.Run("GET /healthz returns 200 ok", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if body["status"] != "ok" {
			t.Errorf("expected status ok, got %v", body["status"])
		}
		if body["service"] != "test-service" {
			t.Errorf("expected service test-service, got %v", body["service"])
		}
	})

	// Test /readyz without failing checks
	t.Run("GET /readyz returns 200 when all checks pass", func(t *testing.T) {
		srv.AddReadinessCheck("dummy_check", func(ctx context.Context) error {
			return nil
		})

		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
	})

	// Test /readyz with failing check
	t.Run("GET /readyz returns 503 when a check fails", func(t *testing.T) {
		srv.AddReadinessCheck("failing_check", func(ctx context.Context) error {
			return errors.New("connection failed")
		})

		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d", rec.Code)
		}
	})

	// Test /metrics
	t.Run("GET /metrics returns prometheus text", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "sims_build_info") {
			t.Errorf("expected sims_build_info in metrics, got:\n%s", body)
		}
	})
}
