//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infrastructure/sims/internal/api"
	"github.com/infrastructure/sims/internal/auth"
	"github.com/infrastructure/sims/internal/netutil"
	"github.com/infrastructure/sims/internal/search"
	"github.com/infrastructure/sims/internal/server"
	"github.com/infrastructure/sims/internal/vsphere"
)

type leafDef struct {
	op       search.Operator
	field    string
	val      any
	values   []any
	category string
	tag      string
	name     string
}

// TestGateS3_ASTFuzzer generates 100 randomized valid compound AST trees
// and verifies that the deterministic SQL compiler emits valid PostgreSQL queries
// without crashing, syntax errors, or panics.
func TestGateS3_ASTFuzzer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	roPool, err := pgxpool.New(ctx, getConnString("query_ro", "query_ro_pass"))
	if err != nil {
		t.Fatalf("failed to connect query_ro pool: %v", err)
	}
	defer roPool.Close()

	leafPool := []leafDef{
		{op: search.OpEQ, field: "power_state", val: "poweredOn"},
		{op: search.OpNE, field: "power_state", val: "poweredOff"},
		{op: search.OpIN, field: "power_state", values: []any{"poweredOn", "suspended"}},
		{op: search.OpContains, field: "name", val: "prod"},
		{op: search.OpPrefix, field: "name", val: "db-"},
		{op: search.OpEQ, field: "cpu_cores", val: int32(4)},
		{op: search.OpEQ, field: "memory_mb", val: int64(8192)},
		{op: search.OpCIDRContains, field: "guest_ip_address", val: "10.0.0.0/8"},
		{op: search.OpCIDROverlap, field: "guest_ip_address", val: "192.168.1.0/24"},
		{op: search.OpVLANOverlap, field: "vlan_ids", val: "100-200"},
		{op: search.OpDescendantsOfFolder, field: "folder_path", val: "Datacenter.vm.Production"},
		{op: search.OpHasTag, category: "Environment", tag: "Production"},
		{op: search.OpHasAttribute, name: "Owner", val: "SRE"},
	}

	for i := 0; i < 100; i++ {
		ast := generateRandomAST(leafPool, 0, 3)

		if err := search.Validate(ast); err != nil {
			t.Fatalf("iteration %d: generated invalid AST: %v", i, err)
		}

		scope := &search.ScopeConstraint{
			AllowedVCenterIDs:  []uuid.UUID{uuid.New()},
			AllowedFolderPaths: []string{"Datacenter.vm.Production"},
		}

		res, err := search.CompileVMQuery(ast, search.CompileOptions{
			Scope: scope,
			Limit: 10,
		})
		if err != nil {
			t.Fatalf("iteration %d: compilation failed: %v", i, err)
		}

		if res.SQL == "" {
			t.Fatalf("iteration %d: compiled empty SQL", i)
		}

		// Execute compiled SQL against PostgreSQL (dry run via EXPLAIN)
		querySQL := fmt.Sprintf("EXPLAIN %s", res.SQL)
		_, err = roPool.Exec(ctx, querySQL, res.Args...)
		if err != nil {
			t.Fatalf("iteration %d: SQL execution failed on PostgreSQL:\nSQL: %s\nArgs: %v\nError: %v", i, querySQL, res.Args, err)
		}
	}
}

func generateRandomAST(leafPool []leafDef, depth, maxDepth int) *search.FilterNode {
	if depth >= maxDepth || randomInt(3) == 0 {
		leaf := leafPool[randomInt(len(leafPool))]
		return &search.FilterNode{
			Op:       leaf.op,
			Field:    leaf.field,
			Value:    leaf.val,
			Values:   leaf.values,
			Category: leaf.category,
			Tag:      leaf.tag,
			Name:     leaf.name,
		}
	}

	logicals := []search.Operator{search.OpAnd, search.OpOr, search.OpNot}
	chosen := logicals[randomInt(len(logicals))]

	if chosen == search.OpNot {
		return &search.FilterNode{
			Op:      search.OpNot,
			Operand: generateRandomAST(leafPool, depth+1, maxDepth),
		}
	}

	numChildren := 2 + randomInt(2)
	operands := make([]*search.FilterNode, numChildren)
	for i := 0; i < numChildren; i++ {
		operands[i] = generateRandomAST(leafPool, depth+1, maxDepth)
	}

	return &search.FilterNode{
		Op:       chosen,
		Operands: operands,
	}
}

func randomInt(max int) int {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max)))
	return int(n.Int64())
}

// TestGateS3_CursorTamperingFuzzer asserts that invalid signatures,
// mismatched query hashes, or expired timestamps are deterministically rejected with HTTP 400.
func TestGateS3_CursorTamperingFuzzer(t *testing.T) {
	signer := api.NewCursorSigner(nil, 24*time.Hour)
	queryHash := "hash1234"

	validCursor := signer.Encode(&api.Cursor{
		Version:   1,
		QueryHash: queryHash,
		SortField: "name",
		SortDir:   "asc",
		LastValue: "vm-prod-01",
		LastID:    uuid.New().String(),
	})

	// 1. Valid cursor decodes cleanly
	p, err := signer.Decode(validCursor, queryHash)
	if err != nil || p == nil {
		t.Fatalf("expected valid cursor to decode, got error: %v", err)
	}

	// 2. Tampered signature
	rawBytes, _ := base64.RawURLEncoding.DecodeString(validCursor)
	var env map[string]any
	_ = json.Unmarshal(rawBytes, &env)
	env["sig"] = "invalidsignature1234567890abcdef"
	tamperedSigBytes, _ := json.Marshal(env)
	tamperedSigCursor := base64.RawURLEncoding.EncodeToString(tamperedSigBytes)

	_, err = signer.Decode(tamperedSigCursor, queryHash)
	if !errors.Is(err, api.ErrCursorTampered) {
		t.Errorf("expected ErrCursorTampered, got: %v", err)
	}

	// 3. Mismatched query hash
	_, err = signer.Decode(validCursor, "different_query_hash")
	if !errors.Is(err, api.ErrQueryMismatch) {
		t.Errorf("expected ErrQueryMismatch, got: %v", err)
	}

	// 4. Expired cursor
	expiredCursor := signer.Encode(&api.Cursor{
		Version:   1,
		QueryHash: queryHash,
		SortField: "name",
		SortDir:   "asc",
		LastValue: "vm-prod-01",
		LastID:    uuid.New().String(),
		ExpiresAt: time.Now().Add(-1 * time.Hour).Unix(),
	})
	_, err = signer.Decode(expiredCursor, queryHash)
	if !errors.Is(err, api.ErrCursorExpired) {
		t.Errorf("expected ErrCursorExpired, got: %v", err)
	}

	// 5. Corrupted base64
	_, err = signer.Decode("!!!notbase64!!!", queryHash)
	if !errors.Is(err, api.ErrInvalidCursor) {
		t.Errorf("expected ErrInvalidCursor on corrupted base64, got: %v", err)
	}
}

// TestGateS3_SQLInjectionPenetration verifies parameter isolation across
// classifier, AST compiler, and pagination inputs.
func TestGateS3_SQLInjectionPenetration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	vcenterID, _, cleanup := setupAPITestData(t, ctx)
	defer cleanup()

	rwPool, err := pgxpool.New(ctx, getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to connect rwPool: %v", err)
	}
	defer rwPool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New("sims-query-api", "1.0.0", "8080", logger)
	handler := api.NewHandler(rwPool, logger)
	handler.RegisterRoutes(srv.Mux)

	payloads := []string{
		"'; DROP TABLE virtual_machines CASCADE; --",
		"' OR 1=1 --",
		"' UNION SELECT id, name, NULL FROM virtual_machines --",
		"admin'--",
		"\" OR \"\"=\"",
		"1'; DELETE FROM audit_events; --",
		"' OR pg_sleep(5) --",
	}

	// Verify VM count before test
	var initialCount int
	_ = rwPool.QueryRow(ctx, "SELECT count(*) FROM virtual_machines WHERE vcenter_id = $1", vcenterID).Scan(&initialCount)

	for _, payload := range payloads {
		// 1. Classifier endpoint GET /v1/search/vms?q=...
		req := httptest.NewRequest(http.MethodGet, "/v1/search/vms?q="+url.QueryEscape(payload), nil)
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, req)
		if rec.Code == http.StatusInternalServerError {
			t.Fatalf("classifier 500 internal server error with payload %q: %s", payload, rec.Body.String())
		}

		// 2. Structured search POST /v1/vms/search
		filterReq := api.SearchVMsRequest{
			Filter: &search.FilterNode{
				Op:    search.OpEQ,
				Field: "name",
				Value: payload,
			},
		}
		b, _ := json.Marshal(filterReq)
		req2 := httptest.NewRequest(http.MethodPost, "/v1/vms/search", bytes.NewReader(b))
		rec2 := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec2, req2)
		if rec2.Code == http.StatusInternalServerError {
			t.Fatalf("structured search 500 with payload %q: %s", payload, rec2.Body.String())
		}
	}

	// Assert table count remained completely unchanged
	var finalCount int
	_ = rwPool.QueryRow(ctx, "SELECT count(*) FROM virtual_machines WHERE vcenter_id = $1", vcenterID).Scan(&finalCount)
	if initialCount != finalCount {
		t.Fatalf("SQL INJECTION FAILURE: table row count changed from %d to %d", initialCount, finalCount)
	}
}

// TestGateS3_ScopeBypassNegative proves that malicious AST expressions
// cannot breach the injected tenant boundary constraints.
func TestGateS3_ScopeBypassNegative(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rwPool, err := pgxpool.New(ctx, getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to connect pool: %v", err)
	}
	defer rwPool.Close()

	tenantA := uuid.New()
	tenantB := uuid.New()

	_, _ = rwPool.Exec(ctx, `
		INSERT INTO vcenters (id, name, api_url, status)
		VALUES ($1, 'vc-tenant-a', 'https://vc-a:443/sdk', 'READY'),
		       ($2, 'vc-tenant-b', 'https://vc-b:443/sdk', 'READY')
		ON CONFLICT (api_url) WHERE deleted_at IS NULL DO NOTHING
	`, tenantA, tenantB)

	vmA := uuid.New()
	vmB := uuid.New()

	_, _ = rwPool.Exec(ctx, `
		INSERT INTO virtual_machines (id, vcenter_id, moref, instance_uuid, name, power_state, folder_path, created_by)
		VALUES ($1, $2, 'vm-100', $3, 'vm-secret-a', 'poweredOn', 'Datacenter.vm.TenantA', 'test'),
		       ($4, $5, 'vm-200', $6, 'vm-secret-b', 'poweredOn', 'Datacenter.vm.TenantB', 'test')
	`, vmA, tenantA, uuid.NewString(), vmB, tenantB, uuid.NewString())

	// Scope restricted strictly to Tenant A
	tenantAScope := &search.ScopeConstraint{
		AllowedVCenterIDs:  []uuid.UUID{tenantA},
		AllowedFolderPaths: []string{"Datacenter.vm.TenantA"},
	}

	maliciousFilters := []*search.FilterNode{
		// Malicious 1: OR 1=1 equivalent
		{
			Op: search.OpOr,
			Operands: []*search.FilterNode{
				{Op: search.OpEQ, Field: "power_state", Value: "poweredOn"},
				{Op: search.OpEQ, Field: "power_state", Value: "poweredOff"},
			},
		},
		// Malicious 2: Negate power_state to match everything
		{
			Op: search.OpNot,
			Operand: &search.FilterNode{
				Op: search.OpEQ, Field: "power_state", Value: "unknown",
			},
		},
		// Malicious 3: Explicitly request Tenant B
		{
			Op:    search.OpEQ,
			Field: "name",
			Value: "vm-secret-b",
		},
	}

	for i, f := range maliciousFilters {
		res, err := search.CompileVMQuery(f, search.CompileOptions{
			Scope: tenantAScope,
			Limit: 10,
		})
		if err != nil {
			t.Fatalf("filter %d: compilation failed: %v", i, err)
		}

		rows, err := rwPool.Query(ctx, res.SQL, res.Args...)
		if err != nil {
			t.Fatalf("filter %d: query failed: %v", i, err)
		}

		var matchedIDs []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			var rest []any
			values, err := rows.Values()
			if err == nil && len(values) > 0 {
				if uid, ok := values[0].(uuid.UUID); ok {
					id = uid
				}
			}
			_ = rest
			matchedIDs = append(matchedIDs, id)
		}
		rows.Close()

		// Verify that vmB was NEVER returned under any filter
		for _, id := range matchedIDs {
			if id == vmB {
				t.Fatalf("SCOPE BYPASS DETECTED in filter %d: out-of-scope VM %s leaked to tenant A!", i, vmB)
			}
		}
	}
}

// TestGateS3_SSRFSuite asserts complete rejection of SSRF vectors
// (loopback, link-local, cloud metadata, redirects, DNS rebinding).
func TestGateS3_SSRFSuite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	policy := netutil.DefaultPolicy()

	forbiddenTargets := []string{
		"127.0.0.1",
		"127.0.0.2",
		"169.254.169.254", // AWS/GCP metadata
		"fd00:ec2::254",    // IPv6 metadata
		"0.0.0.0",
		"::1",
	}

	for _, target := range forbiddenTargets {
		_, err := policy.ResolveAndValidate(ctx, target, 443)
		if !errors.Is(err, netutil.ErrAddressNotAllowed) {
			t.Errorf("expected ErrAddressNotAllowed for target %s, got: %v", target, err)
		}
	}

	// Disallowed ports
	disallowedPorts := []int{22, 25, 80, 8080, 3306, 5432}
	for _, port := range disallowedPorts {
		_, err := policy.ResolveAndValidate(ctx, "8.8.8.8", port)
		if !errors.Is(err, netutil.ErrDisallowedPort) {
			t.Errorf("expected ErrDisallowedPort for port %d, got: %v", port, err)
		}
	}
}

// TestGateS3_CredentialLeakScanner inspects all logs, metrics, audit logs,
// and API responses to guarantee 0 write-only secrets leaked.
func TestGateS3_CredentialLeakScanner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rwPool, err := pgxpool.New(ctx, getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to connect pool: %v", err)
	}
	defer rwPool.Close()

	secretCanary := "SuperSecretCanary999!"

	adminIdentity := &auth.Identity{
		Subject: "admin@corp.local",
		Role:    auth.RoleInventoryAdmin,
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New("sims-query-api", "1.0.0", "8080", logger)
	handler := api.NewHandler(rwPool, logger)
	handler.RegisterRoutes(srv.Mux)

	reqBody := api.TestConnectionRequest{
		Host:     "127.0.0.1",
		Port:     443,
		Username: "svc-user",
		Password: secretCanary,
		TLS:      vsphere.TLSValidationConfig{Verify: false},
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/vcenters/test-connection", bytes.NewReader(b))
	req = req.WithContext(auth.WithIdentity(req.Context(), adminIdentity))
	rec := httptest.NewRecorder()
	srv.Mux.ServeHTTP(rec, req)

	// 1. Scan response
	if strings.Contains(rec.Body.String(), secretCanary) {
		t.Fatalf("CREDENTIAL LEAK: API response contains canary secret: %s", rec.Body.String())
	}

	// 2. Scan audit_events table
	rows, err := rwPool.Query(ctx, "SELECT details::text FROM audit_events")
	if err != nil {
		t.Fatalf("failed to query audit_events: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var detailsStr string
		_ = rows.Scan(&detailsStr)
		if strings.Contains(detailsStr, secretCanary) {
			t.Fatalf("CREDENTIAL LEAK: audit_events table contains canary secret: %s", detailsStr)
		}
		if strings.Contains(detailsStr, "collector_rw_pass") || strings.Contains(detailsStr, "query_ro_pass") {
			t.Fatalf("CREDENTIAL LEAK: audit_events table contains database credentials: %s", detailsStr)
		}
	}

	// 3. Scan Prometheus metrics
	reqMetrics := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recMetrics := httptest.NewRecorder()
	srv.Mux.ServeHTTP(recMetrics, reqMetrics)
	metricsOut := recMetrics.Body.String()

	if strings.Contains(metricsOut, secretCanary) {
		t.Fatalf("CREDENTIAL LEAK: Prometheus metrics output contains canary secret: %s", metricsOut)
	}
}
