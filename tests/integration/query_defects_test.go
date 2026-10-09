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
)

// TestQueryAPI_Defect1_SearchPaginationLimitProbe verifies:
// 1. POST /v1/vms/search with limit=100 returns 100 items, hasMore=true, nextCursor present when 100+ rows match.
// 2. Fetching with nextCursor returns subsequent items (rows 101+).
// 3. GET /v1/search/vms with limit=100 behaves identically (hasMore=true, nextCursor present).
// 4. Invalid limit parameters (0, negative, non-numeric) on quick search return 400 Bad Request.
// 5. Oversized limit (>100) on quick search clamps to 100 instead of falling back to default 20.
func TestQueryAPI_Defect1_SearchPaginationLimitProbe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	roPool, err := pgxpool.New(ctx, getConnString("query_ro", "query_ro_pass"))
	if err != nil {
		t.Fatalf("failed to connect query_ro pool: %v", err)
	}
	defer roPool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New("sims-query-api", "1.0.0", "8080", logger)
	handler := api.NewHandler(roPool, logger)
	handler.RegisterRoutes(srv.Mux)

	adminIdentity := &auth.Identity{
		Subject: "admin@corp.local",
		Role:    auth.RoleAdmin,
	}

	// 1. POST /v1/vms/search with limit=100
	t.Run("POST /v1/vms/search limit=100 preserves probe and hasMore", func(t *testing.T) {
		body := map[string]any{
			"filter": map[string]any{
				"op":    "EQ",
				"field": "power_state",
				"value": "poweredOn",
			},
			"limit": 100,
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/v1/vms/search", bytes.NewReader(b))
		req = req.WithContext(auth.WithIdentity(req.Context(), adminIdentity))
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp api.SearchVMsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if len(resp.Items) != 100 {
			t.Fatalf("expected exactly 100 items on page 1, got %d", len(resp.Items))
		}
		if !resp.HasMore {
			t.Fatalf("expected hasMore=true for limit=100 when >100 rows match, got false")
		}
		if resp.NextCursor == "" {
			t.Fatalf("expected non-empty nextCursor when hasMore=true")
		}

		// Fetch page 2
		bodyPage2 := map[string]any{
			"filter": map[string]any{
				"op":    "EQ",
				"field": "power_state",
				"value": "poweredOn",
			},
			"limit":  100,
			"cursor": resp.NextCursor,
		}
		bPage2, _ := json.Marshal(bodyPage2)
		reqPage2 := httptest.NewRequest(http.MethodPost, "/v1/vms/search", bytes.NewReader(bPage2))
		reqPage2 = reqPage2.WithContext(auth.WithIdentity(reqPage2.Context(), adminIdentity))
		recPage2 := httptest.NewRecorder()
		srv.Mux.ServeHTTP(recPage2, reqPage2)

		if recPage2.Code != http.StatusOK {
			t.Fatalf("expected 200 for page 2, got %d: %s", recPage2.Code, recPage2.Body.String())
		}

		var respPage2 api.SearchVMsResponse
		if err := json.Unmarshal(recPage2.Body.Bytes(), &respPage2); err != nil {
			t.Fatalf("unmarshal error page 2: %v", err)
		}
		if len(respPage2.Items) == 0 {
			t.Fatalf("expected non-empty page 2, got 0 items")
		}
		if respPage2.Items[0].ID == resp.Items[0].ID {
			t.Fatalf("page 2 first item ID %s matches page 1 first item ID (cursor did not advance)", respPage2.Items[0].ID)
		}
	})

	// 2. GET /v1/search/vms with limit=100
	t.Run("GET /v1/search/vms limit=100 preserves probe and hasMore", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/search/vms?q=prod&limit=100", nil)
		req = req.WithContext(auth.WithIdentity(req.Context(), adminIdentity))
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp api.SearchVMsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if len(resp.Items) != 100 {
			t.Fatalf("expected 100 items on page 1, got %d", len(resp.Items))
		}
		if !resp.HasMore {
			t.Fatalf("expected hasMore=true for limit=100, got false")
		}
		if resp.NextCursor == "" {
			t.Fatalf("expected nextCursor")
		}
	})

	// 3. Quick search invalid and oversized limits
	t.Run("GET /v1/search/vms limit validation", func(t *testing.T) {
		// limit=0 -> 400
		req0 := httptest.NewRequest(http.MethodGet, "/v1/search/vms?q=prod&limit=0", nil)
		req0 = req0.WithContext(auth.WithIdentity(req0.Context(), adminIdentity))
		rec0 := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec0, req0)
		if rec0.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for limit=0, got %d", rec0.Code)
		}

		// limit=abc -> 400
		reqABC := httptest.NewRequest(http.MethodGet, "/v1/search/vms?q=prod&limit=abc", nil)
		reqABC = reqABC.WithContext(auth.WithIdentity(reqABC.Context(), adminIdentity))
		recABC := httptest.NewRecorder()
		srv.Mux.ServeHTTP(recABC, reqABC)
		if recABC.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for limit=abc, got %d", recABC.Code)
		}

		// limit=-5 -> 400
		reqNeg := httptest.NewRequest(http.MethodGet, "/v1/search/vms?q=prod&limit=-5", nil)
		reqNeg = reqNeg.WithContext(auth.WithIdentity(reqNeg.Context(), adminIdentity))
		recNeg := httptest.NewRecorder()
		srv.Mux.ServeHTTP(recNeg, reqNeg)
		if recNeg.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for limit=-5, got %d", recNeg.Code)
		}

		// limit=101 -> clamps to 100, returns 100 items (not fallback to 20)
		req101 := httptest.NewRequest(http.MethodGet, "/v1/search/vms?q=prod&limit=101", nil)
		req101 = req101.WithContext(auth.WithIdentity(req101.Context(), adminIdentity))
		rec101 := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec101, req101)
		if rec101.Code != http.StatusOK {
			t.Fatalf("expected 200 for limit=101, got %d", rec101.Code)
		}
		var resp101 api.SearchVMsResponse
		_ = json.Unmarshal(rec101.Body.Bytes(), &resp101)
		if len(resp101.Items) != 100 {
			t.Errorf("expected limit=101 to clamp to 100 items, got %d", len(resp101.Items))
		}
	})

	// 4. Structured search negative limit
	t.Run("POST /v1/vms/search negative limit", func(t *testing.T) {
		body := map[string]any{
			"filter": map[string]any{"op": "EQ", "field": "power_state", "value": "poweredOn"},
			"limit":  -1,
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/v1/vms/search", bytes.NewReader(b))
		req = req.WithContext(auth.WithIdentity(req.Context(), adminIdentity))
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for limit=-1, got %d", rec.Code)
		}
	})
}

// TestQueryAPI_Defect2_TopologyPagination verifies:
// 1. GET /v1/hosts/{id}/vms?limit=N returns N items for N in 1..100.
// 2. Fetching subsequent page with nextCursor returns distinct items.
// 3. Walking the sub-collection to exhaustion yields the exact SQL count.
// 4. Passing an invalid/tampered cursor returns 400 Bad Request.
// 5. Passing invalid limit values returns 400 Bad Request.
func TestQueryAPI_Defect2_TopologyPagination(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	roPool, err := pgxpool.New(ctx, getConnString("query_ro", "query_ro_pass"))
	if err != nil {
		t.Fatalf("failed to connect query_ro pool: %v", err)
	}
	defer roPool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New("sims-query-api", "1.0.0", "8080", logger)
	handler := api.NewHandler(roPool, logger)
	handler.RegisterRoutes(srv.Mux)

	adminIdentity := &auth.Identity{
		Subject: "admin@corp.local",
		Role:    auth.RoleAdmin,
	}

	// Find a host with at least 25 VMs
	var hostID string
	var expectedCount int
	err = roPool.QueryRow(ctx, `
		SELECT host_id::text, count(*)
		FROM virtual_machines
		WHERE host_id IS NOT NULL AND deleted_at IS NULL
		GROUP BY host_id
		HAVING count(*) >= 25
		ORDER BY count(*) DESC
		LIMIT 1;
	`).Scan(&hostID, &expectedCount)
	if err != nil {
		t.Fatalf("failed finding suitable host: %v", err)
	}

	t.Run("honors limit and cursor", func(t *testing.T) {
		// 1. limit=5 returns 5 rows
		req1 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/hosts/%s/vms?limit=5", hostID), nil)
		req1 = req1.WithContext(auth.WithIdentity(req1.Context(), adminIdentity))
		rec1 := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec1, req1)

		if rec1.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec1.Code, rec1.Body.String())
		}
		var resp1 api.SearchVMsResponse
		if err := json.Unmarshal(rec1.Body.Bytes(), &resp1); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if len(resp1.Items) != 5 {
			t.Fatalf("expected 5 items for limit=5, got %d", len(resp1.Items))
		}
		if !resp1.HasMore || resp1.NextCursor == "" {
			t.Fatalf("expected hasMore=true and nextCursor for limit=5")
		}

		// 2. Fetching with returned cursor returns next page with different items
		req2 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/hosts/%s/vms?limit=5&cursor=%s", hostID, resp1.NextCursor), nil)
		req2 = req2.WithContext(auth.WithIdentity(req2.Context(), adminIdentity))
		rec2 := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec2, req2)

		if rec2.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec2.Code, rec2.Body.String())
		}
		var resp2 api.SearchVMsResponse
		if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
			t.Fatalf("unmarshal error page 2: %v", err)
		}
		if len(resp2.Items) != 5 {
			t.Fatalf("expected 5 items on page 2, got %d", len(resp2.Items))
		}
		if resp2.Items[0].ID == resp1.Items[0].ID {
			t.Fatalf("page 2 first item matches page 1 first item (pagination did not advance)")
		}

		// 3. Walk to exhaustion and assert total count equals SQL count
		totalCollected := 0
		seenIDs := make(map[string]bool)
		cursor := ""
		for {
			url := fmt.Sprintf("/v1/hosts/%s/vms?limit=10", hostID)
			if cursor != "" {
				url += "&cursor=" + cursor
			}
			reqPage := httptest.NewRequest(http.MethodGet, url, nil)
			reqPage = reqPage.WithContext(auth.WithIdentity(reqPage.Context(), adminIdentity))
			recPage := httptest.NewRecorder()
			srv.Mux.ServeHTTP(recPage, reqPage)

			if recPage.Code != http.StatusOK {
				t.Fatalf("fetch page failed: %d: %s", recPage.Code, recPage.Body.String())
			}
			var pageResp api.SearchVMsResponse
			_ = json.Unmarshal(recPage.Body.Bytes(), &pageResp)

			for _, it := range pageResp.Items {
				if seenIDs[it.ID] {
					t.Fatalf("duplicate VM %s encountered while paginating", it.ID)
				}
				seenIDs[it.ID] = true
				totalCollected++
			}

			if !pageResp.HasMore {
				break
			}
			cursor = pageResp.NextCursor
		}

		if totalCollected != expectedCount {
			t.Fatalf("walked total %d items, expected %d from SQL count", totalCollected, expectedCount)
		}
	})

	t.Run("returns 400 on bad cursor", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/hosts/%s/vms?cursor=bad-cursor-token", hostID), nil)
		req = req.WithContext(auth.WithIdentity(req.Context(), adminIdentity))
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for bad cursor, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("returns 400 on invalid limit", func(t *testing.T) {
		for _, invalidLimit := range []string{"0", "-1", "abc"} {
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/hosts/%s/vms?limit=%s", hostID, invalidLimit), nil)
			req = req.WithContext(auth.WithIdentity(req.Context(), adminIdentity))
			rec := httptest.NewRecorder()
			srv.Mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for limit=%s, got %d: %s", invalidLimit, rec.Code, rec.Body.String())
			}
		}
	})
}

// TestQueryAPI_Defect3_TenantScopeConsistency verifies the Acceptance Table:
// 1. admin, no restrictions -> everything
// 2. 1 vCenter only -> only that vCenter's VMs (4 in the dev dataset for vcsim)
// 3. folder only -> exactly the folder-subtree count (e.g. dc1.vm.workloads)
// 4. vCenter + folder -> intersection (0 for vcsim x dc1.vm.workloads), never unfiltered
// 5. non-admin with both lists empty -> 0 rows, has_more:false
// Asserts identical behavior across GET /v1/vms, POST /v1/vms/search, and GET /v1/vms/facets.
func TestQueryAPI_Defect3_TenantScopeConsistency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	roPool, err := pgxpool.New(ctx, getConnString("query_ro", "query_ro_pass"))
	if err != nil {
		t.Fatalf("failed to connect query_ro pool: %v", err)
	}
	defer roPool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New("sims-query-api", "1.0.0", "8080", logger)
	handler := api.NewHandler(roPool, logger)
	handler.RegisterRoutes(srv.Mux)

	// Fetch known vCenter IDs and counts
	var totalVMs int
	err = roPool.QueryRow(ctx, "SELECT count(*) FROM virtual_machines WHERE deleted_at IS NULL").Scan(&totalVMs)
	if err != nil {
		t.Fatalf("failed querying total VMs: %v", err)
	}

	var vcsimID uuid.UUID
	var vcsimCount int
	err = roPool.QueryRow(ctx, `
		SELECT vc.id, count(vm.id)
		FROM vcenters vc
		LEFT JOIN virtual_machines vm ON vm.vcenter_id = vc.id AND vm.deleted_at IS NULL
		WHERE vc.name = 'vcsim' AND vc.deleted_at IS NULL
		GROUP BY vc.id
	`).Scan(&vcsimID, &vcsimCount)
	if err != nil {
		t.Fatalf("failed querying vcsim: %v", err)
	}

	testFolder := "dc1.vm.workloads"
	var folderCount int
	err = roPool.QueryRow(ctx, `
		SELECT count(*)
		FROM virtual_machines
		WHERE folder_path <@ $1::ltree AND deleted_at IS NULL
	`, testFolder).Scan(&folderCount)
	if err != nil {
		t.Fatalf("failed querying folder count: %v", err)
	}

	var intersectionCount int
	err = roPool.QueryRow(ctx, `
		SELECT count(*)
		FROM virtual_machines
		WHERE vcenter_id = $1 AND folder_path <@ $2::ltree AND deleted_at IS NULL
	`, vcsimID, testFolder).Scan(&intersectionCount)
	if err != nil {
		t.Fatalf("failed querying intersection count: %v", err)
	}

	type testCase struct {
		name          string
		identity      *auth.Identity
		expectedTotal int
	}

	testCases := []testCase{
		{
			name: "1. admin no restrictions -> everything",
			identity: &auth.Identity{
				Subject: "admin@corp.local",
				Role:    auth.RoleAdmin,
			},
			expectedTotal: totalVMs,
		},
		{
			name: "2. 1 vCenter only -> only that vCenter's VMs",
			identity: &auth.Identity{
				Subject:           "vc-user@corp.local",
				Role:              auth.RoleUser,
				AllowedVCenterIDs: []uuid.UUID{vcsimID},
			},
			expectedTotal: vcsimCount,
		},
		{
			name: "3. folder only -> exactly folder-subtree count",
			identity: &auth.Identity{
				Subject:            "folder-user@corp.local",
				Role:               auth.RoleUser,
				AllowedFolderPaths: []string{testFolder},
			},
			expectedTotal: folderCount,
		},
		{
			name: "4. vCenter + folder -> intersection",
			identity: &auth.Identity{
				Subject:            "combo-user@corp.local",
				Role:               auth.RoleUser,
				AllowedVCenterIDs:  []uuid.UUID{vcsimID},
				AllowedFolderPaths: []string{testFolder},
			},
			expectedTotal: intersectionCount,
		},
		{
			name: "5. non-admin with both lists empty -> 0 rows fail-closed",
			identity: &auth.Identity{
				Subject: "empty-user@corp.local",
				Role:    auth.RoleUser,
			},
			expectedTotal: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// A. Test GET /v1/vms
			getListTotal := func() int {
				total := 0
				cursor := ""
				for {
					url := "/v1/vms?limit=100"
					if cursor != "" {
						url += "&cursor=" + cursor
					}
					req := httptest.NewRequest(http.MethodGet, url, nil)
					req = req.WithContext(auth.WithIdentity(req.Context(), tc.identity))
					rec := httptest.NewRecorder()
					srv.Mux.ServeHTTP(rec, req)

					if rec.Code != http.StatusOK {
						t.Fatalf("GET /v1/vms failed: %d: %s", rec.Code, rec.Body.String())
					}
					var resp api.ListVMsResponse
					_ = json.Unmarshal(rec.Body.Bytes(), &resp)
					total += len(resp.Items)
					if !resp.Pagination.HasMore {
						break
					}
					cursor = resp.Pagination.NextCursor
				}
				return total
			}
			listTotal := getListTotal()
			if listTotal != tc.expectedTotal {
				t.Errorf("GET /v1/vms count mismatch: expected %d, got %d", tc.expectedTotal, listTotal)
			}

			// B. Test POST /v1/vms/search
			getSearchTotal := func() int {
				total := 0
				cursor := ""
				for {
					body := map[string]any{
						"filter": map[string]any{
							"op":       "OR",
							"operands": []map[string]any{{"op": "EQ", "field": "power_state", "value": "poweredOn"}, {"op": "NE", "field": "power_state", "value": "poweredOn"}},
						},
						"limit": 100,
					}
					if cursor != "" {
						body["cursor"] = cursor
					}
					b, _ := json.Marshal(body)
					req := httptest.NewRequest(http.MethodPost, "/v1/vms/search", bytes.NewReader(b))
					req = req.WithContext(auth.WithIdentity(req.Context(), tc.identity))
					rec := httptest.NewRecorder()
					srv.Mux.ServeHTTP(rec, req)

					if rec.Code != http.StatusOK {
						t.Fatalf("POST /v1/vms/search failed: %d: %s", rec.Code, rec.Body.String())
					}
					var resp api.SearchVMsResponse
					_ = json.Unmarshal(rec.Body.Bytes(), &resp)
					total += len(resp.Items)
					if !resp.HasMore {
						break
					}
					cursor = resp.NextCursor
				}
				return total
			}
			searchTotal := getSearchTotal()
			if searchTotal != tc.expectedTotal {
				t.Errorf("POST /v1/vms/search count mismatch: expected %d, got %d", tc.expectedTotal, searchTotal)
			}

			// C. Test GET /v1/vms/facets
			reqFacets := httptest.NewRequest(http.MethodGet, "/v1/vms/facets?fields=power_state&limit=100", nil)
			reqFacets = reqFacets.WithContext(auth.WithIdentity(reqFacets.Context(), tc.identity))
			recFacets := httptest.NewRecorder()
			srv.Mux.ServeHTTP(recFacets, reqFacets)
			if recFacets.Code != http.StatusOK {
				t.Fatalf("GET /v1/vms/facets failed: %d: %s", recFacets.Code, recFacets.Body.String())
			}
			var facetsResp api.FacetsResponse
			_ = json.Unmarshal(recFacets.Body.Bytes(), &facetsResp)
			facetTotal := int64(0)
			for _, b := range facetsResp.Facets["power_state"] {
				facetTotal += b.Count
			}
			if int(facetTotal) != tc.expectedTotal {
				t.Errorf("GET /v1/vms/facets count mismatch: expected %d, got %d", tc.expectedTotal, facetTotal)
			}
		})
	}
}

// TestQueryAPI_Defect4_AuditEventsLimit verifies:
// 1. GET /v1/audit-events?limit=1 returns exactly 1 item.
// 2. GET /v1/audit-events with invalid limit (0, negative, non-numeric) returns 400 Bad Request.
// 3. Non-admin caller receives 403 Forbidden.
func TestQueryAPI_Defect4_AuditEventsLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rwPool, err := pgxpool.New(ctx, getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to connect collector_rw pool: %v", err)
	}
	defer rwPool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New("sims-query-api", "1.0.0", "8080", logger)
	handler := api.NewHandler(rwPool, logger)
	handler.RegisterRoutes(srv.Mux)

	adminIdentity := &auth.Identity{
		Subject: "admin@corp.local",
		Role:    auth.RoleAdmin,
	}
	userIdentity := &auth.Identity{
		Subject: "user@corp.local",
		Role:    auth.RoleUser,
	}

	// Insert test audit events if none exist
	for i := 0; i < 3; i++ {
		_, err = rwPool.Exec(ctx, `
			INSERT INTO audit_events (actor, action, target_id, outcome, request_id, details)
			VALUES ('test-runner', 'test.action', '00000000-0000-0000-0000-000000000001', 'success', 'req-1', '{}')
		`)
		if err != nil {
			t.Fatalf("failed inserting test audit event: %v", err)
		}
	}

	t.Run("GET /v1/audit-events?limit=1 returns 1 row", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/audit-events?limit=1", nil)
		req = req.WithContext(auth.WithIdentity(req.Context(), adminIdentity))
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp api.AuditEventListResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if len(resp.Items) != 1 {
			t.Fatalf("expected exactly 1 item, got %d", len(resp.Items))
		}
	})

	t.Run("GET /v1/audit-events with invalid limits returns 400", func(t *testing.T) {
		for _, badLimit := range []string{"0", "-1", "abc"} {
			req := httptest.NewRequest(http.MethodGet, "/v1/audit-events?limit="+badLimit, nil)
			req = req.WithContext(auth.WithIdentity(req.Context(), adminIdentity))
			rec := httptest.NewRecorder()
			srv.Mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for limit=%s, got %d: %s", badLimit, rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("GET /v1/audit-events non-admin gets 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/audit-events?limit=1", nil)
		req = req.WithContext(auth.WithIdentity(req.Context(), userIdentity))
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 for non-admin, got %d", rec.Code)
		}
	})
}
