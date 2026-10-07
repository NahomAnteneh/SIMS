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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infrastructure/sims/internal/api"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/persistence"
	"github.com/infrastructure/sims/internal/telemetry"
)

// TestAPIReadOnlyRoleCannotWrite verifies that the query_ro database user
// used by the Query API cannot perform INSERT, UPDATE, or DELETE operations.
func TestAPIReadOnlyRoleCannotWrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	roPool, err := pgxpool.New(ctx, getConnString("query_ro", "query_ro_pass"))
	if err != nil {
		t.Fatalf("failed to connect with query_ro: %v", err)
	}
	defer roPool.Close()

	// 1. Verify INSERT is denied
	_, err = roPool.Exec(ctx, `
		INSERT INTO virtual_machines (vcenter_id, moref, instance_uuid, name, power_state, connection_state, cpu_cores, memory_mb)
		VALUES ('a0000000-0000-0000-0000-000000000001', 'vm-ro-test', 'ro-uuid-1', 'ro-vm', 'poweredOn', 'connected', 2, 4096)
	`)
	if err == nil {
		t.Fatalf("expected INSERT to fail for query_ro role, but it succeeded")
	}

	// 2. Verify UPDATE is denied
	_, err = roPool.Exec(ctx, `UPDATE virtual_machines SET name = 'hacked' WHERE true`)
	if err == nil {
		t.Fatalf("expected UPDATE to fail for query_ro role, but it succeeded")
	}

	// 3. Verify DELETE is denied
	_, err = roPool.Exec(ctx, `DELETE FROM virtual_machines WHERE true`)
	if err == nil {
		t.Fatalf("expected DELETE to fail for query_ro role, but it succeeded")
	}

	// 4. Verify SELECT is allowed
	var count int
	err = roPool.QueryRow(ctx, "SELECT count(*) FROM virtual_machines").Scan(&count)
	if err != nil {
		t.Fatalf("expected SELECT to succeed for query_ro role, got: %v", err)
	}
}

func setupAPITestData(t *testing.T, ctx context.Context) (string, []string, func()) {
	t.Helper()

	rwPool, err := pgxpool.New(ctx, getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to connect collector_rw: %v", err)
	}

	// Insert test vCenter
	var vcenterID string
	err = rwPool.QueryRow(ctx, `
		INSERT INTO vcenters (name, api_url, instance_uuid)
		VALUES ('api-test-vc', 'https://api-test-vc.local/sdk', 'api-test-vc-uuid')
		ON CONFLICT (api_url) WHERE deleted_at IS NULL DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`).Scan(&vcenterID)
	if err != nil {
		rwPool.Close()
		t.Fatalf("failed to insert test vcenter: %v", err)
	}

	// Insert datastore and network
	var dsID, netID string
	err = rwPool.QueryRow(ctx, `
		INSERT INTO datastores (vcenter_id, moref, name, type, capacity_bytes, free_bytes)
		VALUES ($1::uuid, 'ds-api-1', 'API_Datastore_1', 'VMFS', 1000000000, 500000000)
		ON CONFLICT (vcenter_id, moref) DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`, vcenterID).Scan(&dsID)
	if err != nil {
		rwPool.Close()
		t.Fatalf("failed to insert datastore: %v", err)
	}

	err = rwPool.QueryRow(ctx, `
		INSERT INTO networks (vcenter_id, moref, name, vlan_ids)
		VALUES ($1::uuid, 'net-api-1', 'API_Network_1', '{[100,100]}'::int4multirange)
		ON CONFLICT (vcenter_id, moref) DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`, vcenterID).Scan(&netID)
	if err != nil {
		rwPool.Close()
		t.Fatalf("failed to insert network: %v", err)
	}

	// Seed 25 VMs: "vm-01" to "vm-25" using bulk persister
	metrics := telemetry.NewMetrics("test-collector", "0.1.0")
	persister := persistence.NewBulkPersister(rwPool, persistence.WriterConfig{}, metrics)

	records := make([]domain.VMRecord, 25)
	for i := 1; i <= 25; i++ {
		name := fmt.Sprintf("vm-%02d", i)
		records[i-1] = domain.VMRecord{
			VCenterID:       vcenterID,
			MoRef:           fmt.Sprintf("vm-%d", i),
			InstanceUUID:    fmt.Sprintf("inst-uuid-%02d", i),
			BiosUUID:        fmt.Sprintf("bios-uuid-%02d", i),
			Name:            name,
			PowerState:      "poweredOn",
			ConnectionState: "connected",
			CPUCores:        2,
			MemoryMB:        4096,
			GuestOS:         "rhel9_64Guest",
			GuestHostName:   fmt.Sprintf("%s.local", name),
			GuestIPAddress:  fmt.Sprintf("10.0.1.%d", i),
			DatastoreIDs:    []string{dsID},
			NetworkInterfaces: []domain.NetworkInterfaceRecord{
				{
					DeviceKey:  4000,
					MacAddress: fmt.Sprintf("00:50:56:00:00:%02x", i),
					IPAddress:  fmt.Sprintf("10.0.1.%d", i),
					NetworkID:  netID,
				},
			},
		}
	}

	if _, err := persister.PersistVMBatch(ctx, records); err != nil {
		rwPool.Close()
		t.Fatalf("failed to seed test batch: %v", err)
	}

	cleanup := func() {
		defer rwPool.Close()
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanCancel()
		_, _ = rwPool.Exec(cleanCtx, "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}

	return vcenterID, []string{dsID, netID}, cleanup
}

func TestAPIListVMsKeysetPagination(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	vcenterID, _, cleanup := setupAPITestData(t, ctx)
	defer cleanup()

	roPool, err := pgxpool.New(ctx, getConnString("query_ro", "query_ro_pass"))
	if err != nil {
		t.Fatalf("failed to connect query_ro pool: %v", err)
	}
	defer roPool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := api.NewHandler(roPool, logger)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// 1. Fetch Page 1 with limit=10
	req1 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/vms?vcenter_id=%s&limit=10", vcenterID), nil)
	rec1 := httptest.NewRecorder()
	mux.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("page 1 status = %d, expected 200: %s", rec1.Code, rec1.Body.String())
	}

	var resp1 api.ListVMsResponse
	if err := json.Unmarshal(rec1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("failed to unmarshal page 1 response: %v", err)
	}

	if len(resp1.Items) != 10 {
		t.Fatalf("expected 10 items on page 1, got %d", len(resp1.Items))
	}
	if !resp1.Pagination.HasMore {
		t.Fatalf("expected has_more=true on page 1")
	}
	if resp1.Pagination.NextCursor == "" {
		t.Fatalf("expected non-empty next_cursor on page 1")
	}
	if resp1.Items[0].Name != "vm-01" || resp1.Items[9].Name != "vm-10" {
		t.Fatalf("page 1 order mismatch: first=%s, last=%s", resp1.Items[0].Name, resp1.Items[9].Name)
	}

	// 2. Fetch Page 2 using cursor
	req2 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/vms?vcenter_id=%s&limit=10&cursor=%s", vcenterID, resp1.Pagination.NextCursor), nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("page 2 status = %d, expected 200: %s", rec2.Code, rec2.Body.String())
	}

	var resp2 api.ListVMsResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("failed to unmarshal page 2 response: %v", err)
	}

	if len(resp2.Items) != 10 {
		t.Fatalf("expected 10 items on page 2, got %d", len(resp2.Items))
	}
	if !resp2.Pagination.HasMore {
		t.Fatalf("expected has_more=true on page 2")
	}
	if resp2.Items[0].Name != "vm-11" || resp2.Items[9].Name != "vm-20" {
		t.Fatalf("page 2 order mismatch: first=%s, last=%s", resp2.Items[0].Name, resp2.Items[9].Name)
	}

	// 3. Fetch Page 3 using cursor (remaining 5 items)
	req3 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/vms?vcenter_id=%s&limit=10&cursor=%s", vcenterID, resp2.Pagination.NextCursor), nil)
	rec3 := httptest.NewRecorder()
	mux.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusOK {
		t.Fatalf("page 3 status = %d, expected 200: %s", rec3.Code, rec3.Body.String())
	}

	var resp3 api.ListVMsResponse
	if err := json.Unmarshal(rec3.Body.Bytes(), &resp3); err != nil {
		t.Fatalf("failed to unmarshal page 3 response: %v", err)
	}

	if len(resp3.Items) != 5 {
		t.Fatalf("expected 5 items on page 3, got %d", len(resp3.Items))
	}
	if resp3.Pagination.HasMore {
		t.Fatalf("expected has_more=false on page 3")
	}
	if resp3.Pagination.NextCursor != "" {
		t.Fatalf("expected empty next_cursor on page 3, got %s", resp3.Pagination.NextCursor)
	}
	if resp3.Items[0].Name != "vm-21" || resp3.Items[4].Name != "vm-25" {
		t.Fatalf("page 3 order mismatch: first=%s, last=%s", resp3.Items[0].Name, resp3.Items[4].Name)
	}
}

func TestAPIPaginationStabilityUnderMutation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	vcenterID, _, cleanup := setupAPITestData(t, ctx)
	defer cleanup()

	roPool, err := pgxpool.New(ctx, getConnString("query_ro", "query_ro_pass"))
	if err != nil {
		t.Fatalf("failed to connect query_ro pool: %v", err)
	}
	defer roPool.Close()

	rwPool, err := pgxpool.New(ctx, getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to connect collector_rw: %v", err)
	}
	defer rwPool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := api.NewHandler(roPool, logger)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// Fetch Page 1 (limit=10) -> items vm-01 through vm-10
	req1 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/vms?vcenter_id=%s&limit=10", vcenterID), nil)
	rec1 := httptest.NewRecorder()
	mux.ServeHTTP(rec1, req1)

	var resp1 api.ListVMsResponse
	if err := json.Unmarshal(rec1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("failed to unmarshal page 1: %v", err)
	}
	page1Cursor := resp1.Pagination.NextCursor

	// 1. Insert concurrent VM sorting before page 1 ("vm-00-new") and after page 2 ("vm-99-new")
	_, err = rwPool.Exec(ctx, `
		INSERT INTO virtual_machines (vcenter_id, moref, instance_uuid, name, power_state, connection_state, cpu_cores, memory_mb)
		VALUES ($1::uuid, 'vm-00', 'inst-uuid-00', 'vm-00-new', 'poweredOn', 'connected', 2, 4096),
		       ($1::uuid, 'vm-99', 'inst-uuid-99', 'vm-99-new', 'poweredOn', 'connected', 2, 4096)
	`, vcenterID)
	if err != nil {
		t.Fatalf("failed to insert concurrent VMs: %v", err)
	}

	// Fetch Page 2 with page 1's cursor -> MUST still return vm-11 through vm-20 without shifting!
	req2 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/vms?vcenter_id=%s&limit=10&cursor=%s", vcenterID, page1Cursor), nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)

	var resp2 api.ListVMsResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("failed to unmarshal page 2: %v", err)
	}

	if len(resp2.Items) != 10 {
		t.Fatalf("expected 10 items on page 2 after insert, got %d", len(resp2.Items))
	}
	if resp2.Items[0].Name != "vm-11" || resp2.Items[9].Name != "vm-20" {
		t.Fatalf("keyset pagination shifted due to insert! first=%s, last=%s", resp2.Items[0].Name, resp2.Items[9].Name)
	}

	// 2. Delete VM from Page 1 ("vm-05")
	_, err = rwPool.Exec(ctx, `DELETE FROM virtual_machines WHERE vcenter_id = $1::uuid AND name = 'vm-05'`, vcenterID)
	if err != nil {
		t.Fatalf("failed to delete vm-05: %v", err)
	}

	// Fetch Page 2 with page 1's cursor -> MUST still return vm-11 through vm-20!
	rec2Del := httptest.NewRecorder()
	mux.ServeHTTP(rec2Del, req2)

	var resp2Del api.ListVMsResponse
	if err := json.Unmarshal(rec2Del.Body.Bytes(), &resp2Del); err != nil {
		t.Fatalf("failed to unmarshal page 2 after delete: %v", err)
	}

	if len(resp2Del.Items) != 10 {
		t.Fatalf("expected 10 items on page 2 after delete, got %d", len(resp2Del.Items))
	}
	if resp2Del.Items[0].Name != "vm-11" || resp2Del.Items[9].Name != "vm-20" {
		t.Fatalf("keyset pagination shifted due to delete! first=%s, last=%s", resp2Del.Items[0].Name, resp2Del.Items[9].Name)
	}
}

func TestAPIMalformedCursorAndValidation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	roPool, err := pgxpool.New(ctx, getConnString("query_ro", "query_ro_pass"))
	if err != nil {
		t.Fatalf("failed to connect query_ro pool: %v", err)
	}
	defer roPool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := api.NewHandler(roPool, logger)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// 1. Invalid base64 cursor
	req := httptest.NewRequest(http.MethodGet, "/v1/vms?cursor=not-valid-base64!", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid base64 cursor, got %d", rec.Code)
	}

	var errResp api.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.Code != http.StatusBadRequest {
		t.Fatalf("expected error code 400, got %d", errResp.Code)
	}

	// 2. Empty JSON object in cursor (missing name/id)
	emptyJSONCursor := "e30=" // base64 of "{}"
	reqEmpty := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/vms?cursor=%s", emptyJSONCursor), nil)
	recEmpty := httptest.NewRecorder()
	mux.ServeHTTP(recEmpty, reqEmpty)

	if recEmpty.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty cursor payload, got %d", recEmpty.Code)
	}

	// 3. Invalid limit
	reqBadLimit := httptest.NewRequest(http.MethodGet, "/v1/vms?limit=-5", nil)
	recBadLimit := httptest.NewRecorder()
	mux.ServeHTTP(recBadLimit, reqBadLimit)

	if recBadLimit.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative limit, got %d", recBadLimit.Code)
	}
}

func TestAPIGetVMByID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	vcenterID, attachments, cleanup := setupAPITestData(t, ctx)
	defer cleanup()

	roPool, err := pgxpool.New(ctx, getConnString("query_ro", "query_ro_pass"))
	if err != nil {
		t.Fatalf("failed to connect query_ro pool: %v", err)
	}
	defer roPool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := api.NewHandler(roPool, logger)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// List to obtain a real seeded VM ID
	reqList := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/vms?vcenter_id=%s&limit=1", vcenterID), nil)
	recList := httptest.NewRecorder()
	mux.ServeHTTP(recList, reqList)

	var listResp api.ListVMsResponse
	if err := json.Unmarshal(recList.Body.Bytes(), &listResp); err != nil || len(listResp.Items) == 0 {
		t.Fatalf("failed to list seeded VM: %v", err)
	}
	targetVM := listResp.Items[0]

	// 1. GET by valid ID
	reqGet := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/vms/%s", targetVM.ID), nil)
	recGet := httptest.NewRecorder()
	mux.ServeHTTP(recGet, reqGet)

	if recGet.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid VM ID, got %d: %s", recGet.Code, recGet.Body.String())
	}

	var detailResp api.VMDetailResponse
	if err := json.Unmarshal(recGet.Body.Bytes(), &detailResp); err != nil {
		t.Fatalf("failed to decode VM detail response: %v", err)
	}

	if detailResp.ID != targetVM.ID {
		t.Fatalf("expected VM ID %s, got %s", targetVM.ID, detailResp.ID)
	}
	if len(detailResp.Datastores) != 1 || detailResp.Datastores[0].ID != attachments[0] {
		t.Fatalf("expected datastore %s, got %v", attachments[0], detailResp.Datastores)
	}
	if len(detailResp.NetworkInterfaces) != 1 || detailResp.NetworkInterfaces[0].NetworkID != attachments[1] {
		t.Fatalf("expected network %s, got %v", attachments[1], detailResp.NetworkInterfaces)
	}

	// 2. GET by nonexistent ID
	nonexistentUUID := uuid.New().String()
	req404 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/vms/%s", nonexistentUUID), nil)
	rec404 := httptest.NewRecorder()
	mux.ServeHTTP(rec404, req404)

	if rec404.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent VM, got %d", rec404.Code)
	}

	// 3. GET by malformed ID (not UUID)
	req400 := httptest.NewRequest(http.MethodGet, "/v1/vms/not-a-valid-uuid", nil)
	rec400 := httptest.NewRecorder()
	mux.ServeHTTP(rec400, req400)

	if rec400.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed VM UUID, got %d", rec400.Code)
	}
}

func TestAPICORSMiddleware(t *testing.T) {
	corsMw := api.NewCORSMiddleware("http://localhost:3000,https://app.corp.local")
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	handler := corsMw.Handler(dummyHandler)

	// 1. Allowed origin GET
	reqAllowed := httptest.NewRequest(http.MethodGet, "/v1/vms", nil)
	reqAllowed.Header.Set("Origin", "http://localhost:3000")
	recAllowed := httptest.NewRecorder()
	handler.ServeHTTP(recAllowed, reqAllowed)

	if recAllowed.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatalf("expected CORS allow origin header, got %s", recAllowed.Header().Get("Access-Control-Allow-Origin"))
	}

	// 2. Allowed origin OPTIONS preflight
	reqOptions := httptest.NewRequest(http.MethodOptions, "/v1/vms", nil)
	reqOptions.Header.Set("Origin", "http://localhost:3000")
	recOptions := httptest.NewRecorder()
	handler.ServeHTTP(recOptions, reqOptions)

	if recOptions.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for preflight, got %d", recOptions.Code)
	}
	if recOptions.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Fatalf("expected Access-Control-Allow-Methods on preflight")
	}

	// 3. Disallowed origin
	reqDisallowed := httptest.NewRequest(http.MethodGet, "/v1/vms", nil)
	reqDisallowed.Header.Set("Origin", "https://untrusted-site.com")
	recDisallowed := httptest.NewRecorder()
	handler.ServeHTTP(recDisallowed, reqDisallowed)

	if recDisallowed.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("expected no CORS header for disallowed origin, got %s", recDisallowed.Header().Get("Access-Control-Allow-Origin"))
	}
}
