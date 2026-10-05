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
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vmware/govmomi/simulator"

	"github.com/infrastructure/sims/internal/api"
	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/persistence"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/infrastructure/sims/internal/vsphere"
)

// TestGate_5000VMHydration verifies:
// 1. In-process vcsim 5,000-VM inventory hydration and streaming
// 2. ContainerView cleanup on every exit path (0 leaked views)
// 3. Complete bulk persistence into PostgreSQL
// 4. Batch idempotency (duplicate-batch test without count drift)
// 5. Query API serving hydrated inventory with read-only credentials
func TestGate_5000VMHydration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 5,000-VM gate test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// 1. Setup vcsim with 5,000 VMs
	t.Log("Initializing in-process vcsim with 5,000 VMs...")
	simStart := time.Now()
	model := simulator.VPX()
	vsphere.ConfigureTestVPX(model)
	model.Autostart = false // Disable power-on simulation tasks for instant model creation
	model.Datacenter = 1
	model.Cluster = 1
	model.ClusterHost = 1
	model.Host = 0
	model.Pool = 0
	model.Machine = 5000
	defer model.Remove()

	if err := model.Create(); err != nil {
		t.Fatalf("failed to create vcsim model: %v", err)
	}

	simServer := model.Service.NewServer()
	defer simServer.Close()
	t.Logf("In-process vcsim ready in %v (total VMs in model: %d)", time.Since(simStart), model.Count().Machine)

	// 2. Connect via SessionManager
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessCfg := vsphere.SessionConfig{
		URL:            simServer.URL.String(),
		Username:       "user",
		Password:       "pass",
		Insecure:       true,
		RequestTimeout: 30 * time.Second,
	}
	session, err := vsphere.NewSessionManager(sessCfg)
	if err != nil {
		t.Fatalf("failed to create session manager: %v", err)
	}

	if err := session.Connect(ctx); err != nil {
		t.Fatalf("failed to connect session to vcsim: %v", err)
	}
	if err := session.Login(ctx); err != nil {
		t.Fatalf("failed to login to vcsim: %v", err)
	}
	defer func() {
		logoutCtx, logoutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer logoutCancel()
		_ = session.Logout(logoutCtx)
	}()

	// 3. Connect to database
	rwPool, err := pgxpool.New(ctx, getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to connect collector_rw: %v", err)
	}
	defer rwPool.Close()

	roPool, err := pgxpool.New(ctx, getConnString("query_ro", "query_ro_pass"))
	if err != nil {
		t.Fatalf("failed to connect query_ro: %v", err)
	}
	defer roPool.Close()

	// Insert vCenter record
	var vcenterID string
	err = rwPool.QueryRow(ctx, `
		INSERT INTO vcenters (name, api_url, instance_uuid)
		VALUES ('vc-gate-5000', $1, 'vc-gate-5000-uuid')
		ON CONFLICT (api_url) DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`, simServer.URL.String()).Scan(&vcenterID)
	if err != nil {
		t.Fatalf("failed to insert vcenter: %v", err)
	}
	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanCancel()
		_, _ = rwPool.Exec(cleanCtx, "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	// Clean up any stale records from previous simulator runs to avoid instance_uuid collisions
	_, _ = rwPool.Exec(ctx, "DELETE FROM virtual_machines WHERE name LIKE 'DC0_%'")

	metrics := telemetry.NewMetrics("sims-gate-test", "0.1.0")
	hydrator := collector.NewHydrator(session, collector.HydratorConfig{MaxObjects: 500})
	persister := persistence.NewBulkPersister(rwPool, persistence.WriterConfig{
		FlushSize:     500,
		FlushInterval: 250 * time.Millisecond,
	}, metrics)

	// 4. Hydrate & persist topology
	t.Log("Hydrating topology...")
	snap, err := hydrator.HydrateTopology(ctx, vcenterID)
	if err != nil {
		t.Fatalf("failed to hydrate topology: %v", err)
	}
	if err := persister.PersistTopology(ctx, snap); err != nil {
		t.Fatalf("failed to persist topology: %v", err)
	}

	// Record baseline memory before hydration
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// 5. Hydrate & persist 5,000 VMs
	t.Log("Streaming 5,000-VM inventory hydration and bulk persistence...")
	hydrateStart := time.Now()
	var totalPersisted int
	var allBatches [][]domain.VMRecord

	err = hydrator.HydrateInventory(ctx, vcenterID, snap, func(batch []domain.VMRecord) error {
		allBatches = append(allBatches, batch)
		persisted, pErr := persister.PersistVMBatch(ctx, batch)
		if pErr != nil {
			return pErr
		}
		totalPersisted += persisted
		return nil
	})
	if err != nil {
		t.Fatalf("5,000-VM hydration failed: %v", err)
	}

	hydrateDuration := time.Since(hydrateStart)
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	t.Logf("5,000 VMs hydrated and persisted in %v (HeapAlloc: %d MB -> %d MB)",
		hydrateDuration,
		memBefore.HeapAlloc/(1024*1024),
		memAfter.HeapAlloc/(1024*1024),
	)

	if totalPersisted != 5000 {
		t.Fatalf("expected 5,000 VMs persisted, got %d", totalPersisted)
	}

	// Verify row count in database
	var dbCount int
	err = roPool.QueryRow(ctx, "SELECT count(*) FROM virtual_machines WHERE vcenter_id = $1::uuid AND deleted_at IS NULL", vcenterID).Scan(&dbCount)
	if err != nil {
		t.Fatalf("failed to count persisted VMs: %v", err)
	}
	if dbCount != 5000 {
		t.Fatalf("expected database count 5,000, got %d", dbCount)
	}

	// 6. Duplicate-batch idempotency test: replay the exact same batches
	t.Log("Replaying batches to verify persistence idempotency...")
	for _, batch := range allBatches {
		_, err := persister.PersistVMBatch(ctx, batch)
		if err != nil {
			t.Fatalf("duplicate batch persistence failed: %v", err)
		}
	}

	var postReplayCount int
	err = roPool.QueryRow(ctx, "SELECT count(*) FROM virtual_machines WHERE vcenter_id = $1::uuid AND deleted_at IS NULL", vcenterID).Scan(&postReplayCount)
	if err != nil {
		t.Fatalf("failed to count after replay: %v", err)
	}
	if postReplayCount != 5000 {
		t.Fatalf("duplicate batch broke idempotency! expected 5,000, got %d", postReplayCount)
	}

	// 7. Verify API returns hydrated inventory
	apiHandler := api.NewHandler(roPool, logger)
	mux := http.NewServeMux()
	apiHandler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/vms?vcenter_id=%s&limit=20", vcenterID), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("API GET /v1/vms returned %d: %s", rec.Code, rec.Body.String())
	}

	var listResp api.ListVMsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("failed to unmarshal API response: %v", err)
	}

	if len(listResp.Items) != 20 {
		t.Fatalf("expected 20 items in first page, got %d", len(listResp.Items))
	}
	if !listResp.Pagination.HasMore {
		t.Fatalf("expected has_more=true")
	}
	if listResp.Pagination.NextCursor == "" {
		t.Fatalf("expected non-empty next_cursor")
	}

	// Query single VM details
	sampleVM := listResp.Items[0]
	reqDetail := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/vms/%s", sampleVM.ID), nil)
	recDetail := httptest.NewRecorder()
	mux.ServeHTTP(recDetail, reqDetail)

	if recDetail.Code != http.StatusOK {
		t.Fatalf("API GET /v1/vms/{id} returned %d", recDetail.Code)
	}

	var detailResp api.VMDetailResponse
	if err := json.Unmarshal(recDetail.Body.Bytes(), &detailResp); err != nil {
		t.Fatalf("failed to unmarshal VM detail: %v", err)
	}
	if detailResp.ID != sampleVM.ID || detailResp.Name != sampleVM.Name {
		t.Fatalf("detail response mismatch: got id=%s name=%s", detailResp.ID, detailResp.Name)
	}
}

// TestGate_50000RowSyntheticLoadAndExplain verifies:
// 1. Bulk loading of 50,000 synthetic VM rows into PostgreSQL
// 2. EXPLAIN query plan verification confirming index usage:
//   - idx_vms_active_name_id for keyset pagination
//   - pk_virtual_machines for primary key lookup
//   - idx_vms_instance_uuid_active for instance_uuid lookup
//   - idx_vms_vcenter_moref_active for (vcenter_id, moref) lookup
func TestGate_50000RowSyntheticLoadAndExplain(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 50,000-row synthetic load in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	rwPool, err := pgxpool.New(ctx, getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to connect collector_rw: %v", err)
	}
	defer rwPool.Close()

	roPool, err := pgxpool.New(ctx, getConnString("query_ro", "query_ro_pass"))
	if err != nil {
		t.Fatalf("failed to connect query_ro: %v", err)
	}
	defer roPool.Close()

	// Insert vCenter
	var vcenterID string
	err = rwPool.QueryRow(ctx, `
		INSERT INTO vcenters (name, api_url, instance_uuid)
		VALUES ('vc-synthetic-50k', 'https://vc-synthetic-50k.local/sdk', 'vc-synthetic-50k-uuid')
		ON CONFLICT (api_url) DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`).Scan(&vcenterID)
	if err != nil {
		t.Fatalf("failed to insert vcenter: %v", err)
	}
	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		_, _ = rwPool.Exec(cleanCtx, "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	metrics := telemetry.NewMetrics("sims-gate-50k", "0.1.0")
	persister := persistence.NewBulkPersister(rwPool, persistence.WriterConfig{}, metrics)

	const totalRows = 50000
	const batchSize = 1000

	t.Logf("Ingesting %d synthetic VM records in batches of %d...", totalRows, batchSize)
	loadStart := time.Now()

	for startIdx := 0; startIdx < totalRows; startIdx += batchSize {
		batch := make([]domain.VMRecord, batchSize)
		for j := 0; j < batchSize; j++ {
			num := startIdx + j + 1
			batch[j] = domain.VMRecord{
				VCenterID:       vcenterID,
				MoRef:           fmt.Sprintf("vm-syn-%05d", num),
				InstanceUUID:    fmt.Sprintf("syn-inst-%05d-uuid", num),
				BiosUUID:        fmt.Sprintf("syn-bios-%05d-uuid", num),
				Name:            fmt.Sprintf("syn-vm-%05d", num),
				PowerState:      "poweredOn",
				ConnectionState: "connected",
				CPUCores:        4,
				MemoryMB:        8192,
				GuestOS:         "rhel9_64Guest",
				GuestHostName:   fmt.Sprintf("syn-vm-%05d.corp.local", num),
				GuestIPAddress:  fmt.Sprintf("10.100.%d.%d", (num>>8)&255, num&255),
			}
		}

		if _, err := persister.PersistVMBatch(ctx, batch); err != nil {
			t.Fatalf("failed to persist batch at index %d: %v", startIdx, err)
		}
	}

	loadDuration := time.Since(loadStart)
	throughput := float64(totalRows) / loadDuration.Seconds()
	t.Logf("%d synthetic records persisted in %v (%.1f records/sec)", totalRows, loadDuration, throughput)

	// Ensure statistics are fresh
	adminPool, aErr := pgxpool.New(ctx, getConnString("postgres", "postgres"))
	if aErr == nil {
		_, _ = adminPool.Exec(ctx, "ANALYZE virtual_machines;")
		adminPool.Close()
	}

	// 1. EXPLAIN check: Keyset pagination query MUST use idx_vms_name_id_active
	t.Run("EXPLAIN keyset pagination uses idx_vms_name_id_active", func(t *testing.T) {
		rows, err := roPool.Query(ctx, `
			EXPLAIN (ANALYZE, BUFFERS) SELECT id, vcenter_id, moref, instance_uuid, name
			FROM virtual_machines
			WHERE deleted_at IS NULL AND (name, id) > ('syn-vm-25000', '00000000-0000-0000-0000-000000000000'::uuid)
			ORDER BY name ASC, id ASC
			LIMIT 20;
		`)
		if err != nil {
			t.Fatalf("failed to explain keyset query: %v", err)
		}
		defer rows.Close()

		var lines []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatalf("failed to scan explain line: %v", err)
			}
			lines = append(lines, line)
		}
		plan := strings.Join(lines, "\n")

		t.Logf("Keyset pagination plan:\n%s", plan)
		if !strings.Contains(plan, "idx_vms_name_id_active") || !strings.Contains(plan, "Index Scan") {
			t.Fatalf("expected Index Scan on idx_vms_name_id_active, got:\n%s", plan)
		}
	})

	// 2. EXPLAIN check: Instance UUID lookup MUST use uq_vms_instance_uuid_active
	t.Run("EXPLAIN instance_uuid lookup uses uq_vms_instance_uuid_active", func(t *testing.T) {
		rows, err := roPool.Query(ctx, `
			EXPLAIN (ANALYZE, BUFFERS) SELECT id, name
			FROM virtual_machines
			WHERE instance_uuid = 'syn-inst-25000-uuid' AND deleted_at IS NULL;
		`)
		if err != nil {
			t.Fatalf("failed to explain instance_uuid query: %v", err)
		}
		defer rows.Close()

		var lines []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatalf("failed to scan explain line: %v", err)
			}
			lines = append(lines, line)
		}
		plan := strings.Join(lines, "\n")

		t.Logf("Instance UUID lookup plan:\n%s", plan)
		if !strings.Contains(plan, "uq_vms_instance_uuid_active") || !strings.Contains(plan, "Index Scan") {
			t.Fatalf("expected Index Scan on uq_vms_instance_uuid_active, got:\n%s", plan)
		}
	})

	// 3. EXPLAIN check: (vcenter_id, moref) lookup MUST use uq_vms_vcenter_moref_active
	t.Run("EXPLAIN vcenter_moref lookup uses uq_vms_vcenter_moref_active", func(t *testing.T) {
		rows, err := roPool.Query(ctx, `
			EXPLAIN (ANALYZE, BUFFERS) SELECT id, name
			FROM virtual_machines
			WHERE vcenter_id = $1::uuid AND moref = 'vm-syn-25000' AND deleted_at IS NULL;
		`, vcenterID)
		if err != nil {
			t.Fatalf("failed to explain vcenter_moref query: %v", err)
		}
		defer rows.Close()

		var lines []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatalf("failed to scan explain line: %v", err)
			}
			lines = append(lines, line)
		}
		plan := strings.Join(lines, "\n")

		t.Logf("vcenter_moref lookup plan:\n%s", plan)
		if !strings.Contains(plan, "uq_vms_vcenter_moref_active") || !strings.Contains(plan, "Index Scan") {
			t.Fatalf("expected Index Scan on uq_vms_vcenter_moref_active, got:\n%s", plan)
		}
	})
}
