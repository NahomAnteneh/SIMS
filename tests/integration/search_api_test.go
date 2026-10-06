//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infrastructure/sims/internal/api"
	"github.com/infrastructure/sims/internal/auth"
	"github.com/infrastructure/sims/internal/server"
)

func TestSearchAPI_ClassifierAndStructuredPaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	vcenterID, vmIDs, cleanup := setupAPITestData(t, ctx)
	defer cleanup()

	roPool, err := pgxpool.New(ctx, getConnString("query_ro", "query_ro_pass"))
	if err != nil {
		t.Fatalf("failed to connect query_ro pool: %v", err)
	}
	defer roPool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New("sims-query-api", "0.2.0", "8080", logger)
	handler := api.NewHandler(roPool, logger)
	handler.RegisterRoutes(srv.Mux)

	t.Run("GET /v1/search/vms by MoRef", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/search/vms?q=vm-1", nil)
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp api.SearchVMsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}
		if len(resp.Items) == 0 {
			t.Fatalf("expected at least 1 match for vm-1, got 0")
		}
		if resp.Items[0].MoRef != "vm-1" {
			t.Errorf("expected MoRef vm-1, got %s", resp.Items[0].MoRef)
		}
	})

	t.Run("GET /v1/search/vms by Substring", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/search/vms?q=vm-", nil)
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp api.SearchVMsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}
		if len(resp.Items) == 0 {
			t.Fatalf("expected matches for 'vm-', got 0")
		}
	})

	t.Run("POST /v1/vms/search structured filter", func(t *testing.T) {
		body := map[string]any{
			"filter": map[string]any{
				"op": "AND",
				"operands": []map[string]any{
					{"op": "EQ", "field": "power_state", "value": "poweredOn"},
					{"op": "CONTAINS", "field": "name", "value": "vm-"},
				},
			},
			"limit": 5,
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/v1/vms/search", bytes.NewReader(b))
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp api.SearchVMsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}
		for _, item := range resp.Items {
			if item.PowerState != "poweredOn" {
				t.Errorf("expected powerState poweredOn, got %s", item.PowerState)
			}
		}
	})

	t.Run("POST /v1/vms/search invalid filter returns 400", func(t *testing.T) {
		body := map[string]any{
			"filter": map[string]any{
				"op":    "EQ",
				"field": "invalid_column_name",
				"value": "hacked",
			},
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/v1/vms/search", bytes.NewReader(b))
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for unknown field, got %d", rec.Code)
		}
	})

	t.Run("GET /v1/vms/facets", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/vms/facets?fields=power_state,cluster,datacenter", nil)
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp api.FacetsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}
		if _, ok := resp.Facets["power_state"]; !ok {
			t.Fatalf("expected power_state facet in response")
		}
	})

	t.Run("GET /v1/meta/vm-fields", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/meta/vm-fields", nil)
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp api.VMFieldsMetadataResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}
		if len(resp.Fields) == 0 {
			t.Fatalf("expected non-empty fields list")
		}
	})

	t.Run("GET /v1/tags and GET /v1/custom-attributes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/tags", nil)
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /v1/tags failed: %d", rec.Code)
		}

		reqAttr := httptest.NewRequest(http.MethodGet, "/v1/custom-attributes", nil)
		recAttr := httptest.NewRecorder()
		srv.Mux.ServeHTTP(recAttr, reqAttr)
		if recAttr.Code != http.StatusOK {
			t.Fatalf("GET /v1/custom-attributes failed: %d", recAttr.Code)
		}
	})

	t.Run("GET /v1/me with authenticated identity", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		testID := &auth.Identity{
			Subject:            "test-user@corp.local",
			Role:               auth.RoleViewer,
			AllowedVCenterIDs:  []uuid.UUID{uuid.MustParse(vcenterID)},
			AllowedFolderPaths: []string{"Datacenters.DC1"},
		}
		req = req.WithContext(auth.WithIdentity(req.Context(), testID))
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET /v1/me failed: %d: %s", rec.Code, rec.Body.String())
		}

		var me api.MeResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
			t.Fatalf("failed to unmarshal me response: %v", err)
		}
		if me.Subject != "test-user@corp.local" || me.Role != "viewer" {
			t.Errorf("unexpected me profile: %+v", me)
		}
	})

	t.Run("Topology read and list endpoints", func(t *testing.T) {
		// List datacenters
		req := httptest.NewRequest(http.MethodGet, "/v1/datacenters", nil)
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /v1/datacenters failed: %d", rec.Code)
		}

		// List folders
		reqFolder := httptest.NewRequest(http.MethodGet, "/v1/folders", nil)
		recFolder := httptest.NewRecorder()
		srv.Mux.ServeHTTP(recFolder, reqFolder)
		if recFolder.Code != http.StatusOK {
			t.Fatalf("GET /v1/folders failed: %d", recFolder.Code)
		}

		// List networks
		reqNet := httptest.NewRequest(http.MethodGet, "/v1/networks", nil)
		recNet := httptest.NewRecorder()
		srv.Mux.ServeHTTP(recNet, reqNet)
		if recNet.Code != http.StatusOK {
			t.Fatalf("GET /v1/networks failed: %d", recNet.Code)
		}
	})

	_ = vmIDs
}
