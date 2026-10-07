//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infrastructure/sims/internal/api"
	"github.com/infrastructure/sims/internal/server"
)

// TestOpenAPIContractValidation loads api/openapi.yaml, validates the spec is compliant
// with OpenAPI 3.1, and asserts that real responses from the API match the defined schemas.
func TestOpenAPIContractValidation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Load and validate OpenAPI 3.1 specification
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatalf("failed to load api/openapi.yaml: %v", err)
	}

	if err := doc.Validate(ctx); err != nil {
		t.Fatalf("api/openapi.yaml failed OpenAPI 3.1 validation: %v", err)
	}

	// 2. Setup test data and handler
	vcenterID, _, cleanup := setupAPITestData(t, ctx)
	defer cleanup()

	roPool, err := pgxpool.New(ctx, getConnString("query_ro", "query_ro_pass"))
	if err != nil {
		t.Fatalf("failed to connect query_ro pool: %v", err)
	}
	defer roPool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New("sims-query-api", "1.0.0", "8080", logger)
	handler := api.NewHandler(roPool, logger)
	handler.RegisterRoutes(srv.Mux)

	listSchema := doc.Components.Schemas["ListVMsResponse"].Value
	detailSchema := doc.Components.Schemas["VMDetailResponse"].Value
	errorSchema := doc.Components.Schemas["ErrorResponse"].Value
	healthSchema := doc.Components.Schemas["HealthResponse"].Value

	// 3. Test GET /healthz response conforms to HealthResponse schema
	t.Run("GET /healthz conforms to schema", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var body any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if err := healthSchema.VisitJSON(body, openapi3.EnableJSONSchema2020()); err != nil {
			t.Fatalf("GET /healthz response violates HealthResponse schema: %v", err)
		}
	})

	// 4. Test GET /v1/vms response conforms to ListVMsResponse schema
	var targetVMID string
	t.Run("GET /v1/vms conforms to schema", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/vms?vcenter_id=%s&limit=5", vcenterID), nil)
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if err := listSchema.VisitJSON(body, openapi3.EnableJSONSchema2020()); err != nil {
			t.Fatalf("GET /v1/vms response violates ListVMsResponse schema: %v", err)
		}

		var typedResp api.ListVMsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &typedResp); err == nil && len(typedResp.Items) > 0 {
			targetVMID = typedResp.Items[0].ID
		}
	})

	if targetVMID == "" {
		t.Fatalf("expected at least one VM returned in list query")
	}

	// 5. Test GET /v1/vms/{id} conforms to VMDetailResponse schema
	t.Run("GET /v1/vms/{id} conforms to schema", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/vms/%s", targetVMID), nil)
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if err := detailSchema.VisitJSON(body, openapi3.EnableJSONSchema2020()); err != nil {
			t.Fatalf("GET /v1/vms/{id} response violates VMDetailResponse schema: %v", err)
		}
	})

	// 6. Test GET /v1/vms (400 Bad Request) conforms to ErrorResponse schema
	t.Run("GET /v1/vms 400 conforms to ErrorResponse schema", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/vms?limit=-10", nil)
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}

		var body any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if err := errorSchema.VisitJSON(body, openapi3.EnableJSONSchema2020()); err != nil {
			t.Fatalf("GET /v1/vms 400 response violates ErrorResponse schema: %v", err)
		}
	})

	// 7. Test GET /v1/vms/{id} (404 Not Found) conforms to ErrorResponse schema
	t.Run("GET /v1/vms/{id} 404 conforms to ErrorResponse schema", func(t *testing.T) {
		randomUUID := uuid.New().String()
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/vms/%s", randomUUID), nil)
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}

		var body any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if err := errorSchema.VisitJSON(body, openapi3.EnableJSONSchema2020()); err != nil {
			t.Fatalf("GET /v1/vms/{id} 404 response violates ErrorResponse schema: %v", err)
		}
	})
}
