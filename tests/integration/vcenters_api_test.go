//go:build integration

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
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infrastructure/sims/internal/api"
	"github.com/infrastructure/sims/internal/auth"
	"github.com/infrastructure/sims/internal/server"
	"github.com/infrastructure/sims/internal/vsphere"
)

func TestVCentersAPI_FullLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rwPool, err := pgxpool.New(ctx, getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to connect collector_rw pool: %v", err)
	}
	defer rwPool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New("sims-query-api", "0.2.0", "8080", logger)
	handler := api.NewHandler(rwPool, logger)
	handler.RegisterRoutes(srv.Mux)

	adminIdentity := &auth.Identity{
		Subject: "admin@corp.local",
		Role:    auth.RoleInventoryAdmin,
	}

	viewerIdentity := &auth.Identity{
		Subject: "viewer@corp.local",
		Role:    auth.RoleViewer,
	}

	// 1. POST /v1/vcenters/test-connection against SSRF (loopback denied)
	t.Run("POST /v1/vcenters/test-connection rejects loopback SSRF", func(t *testing.T) {
		reqBody := api.TestConnectionRequest{
			Host:     "127.0.0.1",
			Port:     443,
			Username: "user",
			Password: "password",
			TLS:      vsphere.TLSValidationConfig{Verify: false},
		}
		b, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/v1/vcenters/test-connection", bytes.NewReader(b))
		req = req.WithContext(auth.WithIdentity(req.Context(), adminIdentity))
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d: %s", rec.Code, rec.Body.String())
		}
		var prob map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &prob)
		if prob["error_code"] != "address_not_allowed" {
			t.Errorf("expected error_code address_not_allowed, got %v", prob["error_code"])
		}
	})

	// 2. Non-admin forbidden from registering vCenter
	t.Run("POST /v1/vcenters forbidden for non-admin", func(t *testing.T) {
		reqBody := api.CreateVCenterRequest{
			Name:     "vc-forbidden",
			Host:     "vc-forbidden.example.com",
			Username: "user",
			Password: "password",
		}
		b, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/v1/vcenters", bytes.NewReader(b))
		req = req.WithContext(auth.WithIdentity(req.Context(), viewerIdentity))
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// 3. Register a vCenter directly into DB for testing management API
	registeredVCID := uuid.New()
	targetHost := fmt.Sprintf("vc-%s.corp.local", registeredVCID.String()[:8])
	apiURL := fmt.Sprintf("https://%s:443/sdk", targetHost)

	_, err = rwPool.Exec(ctx, `
		INSERT INTO vcenters (id, name, api_url, product_version, api_version, build, status, priority, enabled)
		VALUES ($1, $2, $3, '8.0.3', '8.0.3.0', '24022515', 'PENDING_ASSIGNMENT', 100, true)
	`, registeredVCID, "vc-prod-01", apiURL)
	if err != nil {
		t.Fatalf("failed to insert test vcenter: %v", err)
	}

	_, err = rwPool.Exec(ctx, `
		INSERT INTO collector_targets (vcenter_id, enabled)
		VALUES ($1, true)
	`, registeredVCID)
	if err != nil {
		t.Fatalf("failed to insert test collector_target: %v", err)
	}

	// Insert test VM belonging to this vCenter
	vmID := uuid.New()
	_, err = rwPool.Exec(ctx, `
		INSERT INTO virtual_machines (id, vcenter_id, moref, instance_uuid, name, power_state, connection_state, cpu_cores, memory_mb, guest_os, created_by)
		VALUES ($1, $2, 'vm-999', $3, 'test-vm-decom', 'poweredOn', 'connected', 4, 8192, 'rhel9_64Guest', 'test')
	`, vmID, registeredVCID, uuid.NewString())
	if err != nil {
		t.Fatalf("failed to insert test VM: %v", err)
	}

	// 4. Duplicate registration returns 409 Conflict
	t.Run("POST /v1/vcenters returns 409 Conflict on existing host", func(t *testing.T) {
		reqBody := api.CreateVCenterRequest{
			Name:     "vc-prod-duplicate",
			Host:     targetHost,
			Port:     443,
			Username: "user",
			Password: "password",
		}
		b, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/v1/vcenters", bytes.NewReader(b))
		req = req.WithContext(auth.WithIdentity(req.Context(), adminIdentity))
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict, got %d: %s", rec.Code, rec.Body.String())
		}
		var prob map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &prob)
		if prob["error_code"] != "conflict" {
			t.Errorf("expected error_code conflict, got %v", prob["error_code"])
		}
	})

	// 5. GET /v1/vcenters with role and scope enforcement
	t.Run("GET /v1/vcenters scope filtering", func(t *testing.T) {
		// Viewer without this vCenter in scope sees 0 items
		viewerOutOfScope := &auth.Identity{
			Subject:          "viewer-out@corp.local",
			Role:             auth.RoleViewer,
			AllowedVCenterIDs: []uuid.UUID{uuid.New()}, // Different vCenter
		}
		req := httptest.NewRequest(http.MethodGet, "/v1/vcenters", nil)
		req = req.WithContext(auth.WithIdentity(req.Context(), viewerOutOfScope))
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var listResp api.VCenterListResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &listResp)
		for _, item := range listResp.Items {
			if item.ID == registeredVCID.String() {
				t.Fatalf("out-of-scope viewer should not see vCenter %s", registeredVCID)
			}
		}

		// Viewer with this vCenter in scope sees it
		viewerinScope := &auth.Identity{
			Subject:          "viewer-in@corp.local",
			Role:             auth.RoleViewer,
			AllowedVCenterIDs: []uuid.UUID{registeredVCID},
		}
		req2 := httptest.NewRequest(http.MethodGet, "/v1/vcenters", nil)
		req2 = req2.WithContext(auth.WithIdentity(req2.Context(), viewerinScope))
		rec2 := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec2, req2)

		if rec2.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec2.Code)
		}
		var listResp2 api.VCenterListResponse
		_ = json.Unmarshal(rec2.Body.Bytes(), &listResp2)
		found := false
		for _, item := range listResp2.Items {
			if item.ID == registeredVCID.String() {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("in-scope viewer should see vCenter %s", registeredVCID)
		}
	})

	// 6. GET /v1/vcenters/{id}
	t.Run("GET /v1/vcenters/{id}", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/vcenters/"+registeredVCID.String(), nil)
		req = req.WithContext(auth.WithIdentity(req.Context(), adminIdentity))
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var vcResp api.VCenterResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &vcResp)
		if vcResp.ID != registeredVCID.String() {
			t.Errorf("expected ID %s, got %s", registeredVCID, vcResp.ID)
		}
		if vcResp.Status != "PENDING_ASSIGNMENT" {
			t.Errorf("expected status PENDING_ASSIGNMENT, got %s", vcResp.Status)
		}
	})

	// 7. PATCH /v1/vcenters/{id}
	t.Run("PATCH /v1/vcenters/{id}", func(t *testing.T) {
		newName := "vc-renamed-01"
		newPriority := 50
		reqBody := api.UpdateVCenterRequest{
			Name:     &newName,
			Priority: &newPriority,
		}
		b, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPatch, "/v1/vcenters/"+registeredVCID.String(), bytes.NewReader(b))
		req = req.WithContext(auth.WithIdentity(req.Context(), adminIdentity))
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var vcResp api.VCenterResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &vcResp)
		if vcResp.Name != newName {
			t.Errorf("expected updated name %s, got %s", newName, vcResp.Name)
		}
		if vcResp.Priority != newPriority {
			t.Errorf("expected updated priority %d, got %d", newPriority, vcResp.Priority)
		}
	})

	// 8. POST /v1/vcenters/{id}/resync
	t.Run("POST /v1/vcenters/{id}/resync", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/vcenters/"+registeredVCID.String()+"/resync", nil)
		req = req.WithContext(auth.WithIdentity(req.Context(), adminIdentity))
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// 9. DELETE /v1/vcenters/{id} (Decommission and tombstone inventory)
	t.Run("DELETE /v1/vcenters/{id} decommissions and tombstones inventory", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/v1/vcenters/"+registeredVCID.String(), nil)
		req = req.WithContext(auth.WithIdentity(req.Context(), adminIdentity))
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify vCenter disappeared from GET /v1/vcenters
		req2 := httptest.NewRequest(http.MethodGet, "/v1/vcenters/"+registeredVCID.String(), nil)
		req2 = req2.WithContext(auth.WithIdentity(req2.Context(), adminIdentity))
		rec2 := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusNotFound {
			t.Fatalf("decommissioned vCenter should return 404, got %d", rec2.Code)
		}

		// Verify VM was tombstoned
		var vmDeletedAt *time.Time
		err := rwPool.QueryRow(ctx, "SELECT deleted_at FROM virtual_machines WHERE id = $1", vmID).Scan(&vmDeletedAt)
		if err != nil {
			t.Fatalf("failed to query VM: %v", err)
		}
		if vmDeletedAt == nil {
			t.Fatalf("expected VM to have non-nil deleted_at timestamp (tombstoned)")
		}

		// Idempotent DELETE returns 204
		rec3 := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec3, req)
		if rec3.Code != http.StatusNoContent {
			t.Fatalf("repeated DELETE should return 204 No Content, got %d", rec3.Code)
		}
	})

	// 10. Audit Ledger Verification: no credentials stored, all actions recorded
	t.Run("Audit Ledger records administrative actions with zero secrets", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/audit-events", nil)
		req = req.WithContext(auth.WithIdentity(req.Context(), adminIdentity))
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for audit logs, got %d: %s", rec.Code, rec.Body.String())
		}

		var auditResp api.AuditEventListResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &auditResp)

		if len(auditResp.Items) == 0 {
			t.Fatalf("expected audit events to be recorded, got 0")
		}

		// Check for presence of actions and assert zero passwords/tokens in payload
		actionsSeen := make(map[string]bool)
		for _, ev := range auditResp.Items {
			actionsSeen[ev.Action] = true

			// Ensure zero secrets in details
			detailsBytes, _ := json.Marshal(ev.Details)
			detailsStr := string(detailsBytes)
			if bytes.Contains(detailsBytes, []byte("password")) || bytes.Contains(detailsBytes, []byte("secret")) || bytes.Contains(detailsBytes, []byte("SuperSecret")) {
				t.Fatalf("AUDIT LEAK DETECTED: audit event contains secret: %s", detailsStr)
			}
		}

		if !actionsSeen["vcenter.test_connection"] {
			t.Errorf("expected audit event vcenter.test_connection")
		}
		if !actionsSeen["vcenter.update"] {
			t.Errorf("expected audit event vcenter.update")
		}
		if !actionsSeen["vcenter.delete"] {
			t.Errorf("expected audit event vcenter.delete")
		}
	})
}
