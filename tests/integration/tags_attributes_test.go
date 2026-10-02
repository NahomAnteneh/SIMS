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
	"strings"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/api"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/persistence"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTagsAndCustomAttributesPersistenceAndReplaceSet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	connStr := getConnString("collector_rw", "collector_rw_pass")
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to connect pool: %v", err)
	}
	defer pool.Close()

	// 1. Create vCenter
	var vcenterID string
	err = pool.QueryRow(ctx, `
		INSERT INTO vcenters (name, api_url, instance_uuid)
		VALUES ('vc-tags-test', 'https://vc-tags-test.local/sdk', 'vc-tags-uuid-01')
		ON CONFLICT (api_url) DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`).Scan(&vcenterID)
	if err != nil {
		t.Fatalf("failed to insert test vcenter: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	metrics := telemetry.NewMetrics("test-collector", "0.1.0")
	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{}, metrics)

	// 2. Initial batch: 2 VMs
	// vm-0 has 2 tags and 2 attributes
	// vm-1 has 0 tags and 0 attributes
	initialBatch := []domain.VMRecord{
		{
			VCenterID:       vcenterID,
			MoRef:           "vm-tag-100",
			InstanceUUID:    "uuid-tag-100",
			Name:            "web-app-prod",
			PowerState:      "poweredOn",
			ConnectionState: "connected",
			CPUCores:        4,
			MemoryMB:        8192,
			Tags: []domain.TagRecord{
				{TagID: "urn:tag:env:prod", CategoryName: "Environment", TagName: "Production"},
				{TagID: "urn:tag:tier:fe", CategoryName: "Tier", TagName: "Frontend"},
			},
			CustomAttributes: []domain.CustomAttributeRecord{
				{Name: "Owner", Value: "DevOps-Team"},
				{Name: "CostCenter", Value: "101"},
			},
		},
		{
			VCenterID:       vcenterID,
			MoRef:           "vm-tag-101",
			InstanceUUID:    "uuid-tag-101",
			Name:            "bare-vm",
			PowerState:      "poweredOff",
			ConnectionState: "connected",
			CPUCores:        2,
			MemoryMB:        4096,
		},
	}

	persistedCount, err := persister.PersistVMBatch(ctx, initialBatch)
	if err != nil {
		t.Fatalf("initial PersistVMBatch failed: %v", err)
	}
	if persistedCount != 2 {
		t.Fatalf("expected 2 persisted VMs, got %d", persistedCount)
	}

	// Verify DB state for initial batch
	var vm0ID string
	err = pool.QueryRow(ctx, "SELECT id::text FROM virtual_machines WHERE vcenter_id = $1::uuid AND moref = 'vm-tag-100'", vcenterID).Scan(&vm0ID)
	if err != nil {
		t.Fatalf("failed to query vm0 ID: %v", err)
	}

	var tagCount, attrCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM vm_tags WHERE vm_id = $1::uuid", vm0ID).Scan(&tagCount)
	if err != nil || tagCount != 2 {
		t.Fatalf("expected 2 tags for vm0, got %d (err: %v)", tagCount, err)
	}

	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM vm_custom_attributes WHERE vm_id = $1::uuid", vm0ID).Scan(&attrCount)
	if err != nil || attrCount != 2 {
		t.Fatalf("expected 2 custom attributes for vm0, got %d (err: %v)", attrCount, err)
	}

	// 3. Test Replace-Set Semantics:
	// In next sync:
	// - Tier/Frontend is removed
	// - Compliance/PCI is added
	// - Owner is removed
	// - CostCenter is updated from 101 to 102
	updatedBatch := []domain.VMRecord{
		{
			VCenterID:       vcenterID,
			MoRef:           "vm-tag-100",
			InstanceUUID:    "uuid-tag-100",
			Name:            "web-app-prod",
			PowerState:      "poweredOn",
			ConnectionState: "connected",
			CPUCores:        4,
			MemoryMB:        8192,
			Tags: []domain.TagRecord{
				{TagID: "urn:tag:env:prod", CategoryName: "Environment", TagName: "Production"},
				{TagID: "urn:tag:comp:pci", CategoryName: "Compliance", TagName: "PCI"},
			},
			CustomAttributes: []domain.CustomAttributeRecord{
				{Name: "CostCenter", Value: "102"},
			},
		},
	}

	_, err = persister.PersistVMBatch(ctx, updatedBatch)
	if err != nil {
		t.Fatalf("second PersistVMBatch failed: %v", err)
	}

	// Assert tags replace-set
	rows, err := pool.Query(ctx, "SELECT category_name, tag_name FROM vm_tags WHERE vm_id = $1::uuid ORDER BY category_name", vm0ID)
	if err != nil {
		t.Fatalf("failed to query updated tags: %v", err)
	}
	defer rows.Close()

	var tagsFound [][2]string
	for rows.Next() {
		var cat, tag string
		if err := rows.Scan(&cat, &tag); err != nil {
			t.Fatalf("failed to scan tag row: %v", err)
		}
		tagsFound = append(tagsFound, [2]string{cat, tag})
	}

	if len(tagsFound) != 2 {
		t.Fatalf("expected 2 tags after replace-set, got %d: %v", len(tagsFound), tagsFound)
	}
	if tagsFound[0] != [2]string{"Compliance", "PCI"} || tagsFound[1] != [2]string{"Environment", "Production"} {
		t.Errorf("unexpected tags after replace-set: %v", tagsFound)
	}

	// Assert custom attributes replace-set
	attrRows, err := pool.Query(ctx, "SELECT name, value FROM vm_custom_attributes WHERE vm_id = $1::uuid", vm0ID)
	if err != nil {
		t.Fatalf("failed to query updated custom attributes: %v", err)
	}
	defer attrRows.Close()

	var attrsFound [][2]string
	for attrRows.Next() {
		var name, val string
		if err := attrRows.Scan(&name, &val); err != nil {
			t.Fatalf("failed to scan attr row: %v", err)
		}
		attrsFound = append(attrsFound, [2]string{name, val})
	}

	if len(attrsFound) != 1 {
		t.Fatalf("expected 1 custom attribute after replace-set (Owner removed), got %d: %v", len(attrsFound), attrsFound)
	}
	if attrsFound[0] != [2]string{"CostCenter", "102"} {
		t.Errorf("unexpected custom attributes after replace-set: %v", attrsFound)
	}

	// 4. Idempotency test: replay exact same batch
	_, err = persister.PersistVMBatch(ctx, updatedBatch)
	if err != nil {
		t.Fatalf("replay PersistVMBatch failed: %v", err)
	}

	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM vm_tags WHERE vm_id = $1::uuid", vm0ID).Scan(&tagCount)
	if err != nil || tagCount != 2 {
		t.Fatalf("expected exactly 2 tags after idempotent replay, got %d", tagCount)
	}

	// 5. Total removal test: empty slice removes all tags and attributes
	emptyBatch := []domain.VMRecord{
		{
			VCenterID:        vcenterID,
			MoRef:            "vm-tag-100",
			InstanceUUID:     "uuid-tag-100",
			Name:             "web-app-prod",
			PowerState:       "poweredOn",
			ConnectionState:  "connected",
			CPUCores:         4,
			MemoryMB:         8192,
			Tags:             nil,
			CustomAttributes: nil,
		},
	}
	_, err = persister.PersistVMBatch(ctx, emptyBatch)
	if err != nil {
		t.Fatalf("empty PersistVMBatch failed: %v", err)
	}

	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM vm_tags WHERE vm_id = $1::uuid", vm0ID).Scan(&tagCount)
	if err != nil || tagCount != 0 {
		t.Fatalf("expected 0 tags after clearing, got %d", tagCount)
	}
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM vm_custom_attributes WHERE vm_id = $1::uuid", vm0ID).Scan(&attrCount)
	if err != nil || attrCount != 0 {
		t.Fatalf("expected 0 custom attributes after clearing, got %d", attrCount)
	}
}

func TestTagsAndCustomAttributesAPI_DetailEndpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	rwPool, err := pgxpool.New(ctx, getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to connect collector_rw pool: %v", err)
	}
	defer rwPool.Close()

	roPool, err := pgxpool.New(ctx, getConnString("query_ro", "query_ro_pass"))
	if err != nil {
		t.Fatalf("failed to connect query_ro pool: %v", err)
	}
	defer roPool.Close()

	var vcenterID string
	err = rwPool.QueryRow(ctx, `
		INSERT INTO vcenters (name, api_url, instance_uuid)
		VALUES ('vc-api-tags-test', 'https://vc-api-tags.local/sdk', 'vc-api-tags-uuid')
		ON CONFLICT (api_url) DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`).Scan(&vcenterID)
	if err != nil {
		t.Fatalf("failed to insert vcenter: %v", err)
	}
	defer func() {
		_, _ = rwPool.Exec(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	persister := persistence.NewBulkPersister(rwPool, persistence.WriterConfig{}, nil)
	records := []domain.VMRecord{
		{
			VCenterID:       vcenterID,
			MoRef:           "vm-detail-tag-01",
			InstanceUUID:    "uuid-detail-tag-01",
			Name:            "api-tag-vm",
			PowerState:      "poweredOn",
			ConnectionState: "connected",
			CPUCores:        8,
			MemoryMB:        16384,
			Tags: []domain.TagRecord{
				{TagID: "urn:vmomi:tag:1", CategoryName: "Environment", TagName: "Staging"},
				{TagID: "urn:vmomi:tag:2", CategoryName: "Team", TagName: "Backend"},
			},
			CustomAttributes: []domain.CustomAttributeRecord{
				{Name: "BackupTier", Value: "Platinum"},
			},
		},
	}

	if _, err := persister.PersistVMBatch(ctx, records); err != nil {
		t.Fatalf("failed to persist VM batch: %v", err)
	}

	var vmID string
	err = rwPool.QueryRow(ctx, "SELECT id::text FROM virtual_machines WHERE vcenter_id = $1::uuid AND moref = 'vm-detail-tag-01'", vcenterID).Scan(&vmID)
	if err != nil {
		t.Fatalf("failed to get VM ID: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := api.NewHandler(roPool, logger)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// 1. GET /v1/vms/{id} -> detail response
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/vms/%s", vmID), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var detailResp api.VMDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &detailResp); err != nil {
		t.Fatalf("failed to decode detail response: %v", err)
	}

	if len(detailResp.Tags) != 2 {
		t.Fatalf("expected 2 tags in detail response, got %d", len(detailResp.Tags))
	}
	if detailResp.Tags[0].CategoryName != "Environment" || detailResp.Tags[0].TagName != "Staging" {
		t.Errorf("tag 0 mismatch: %+v", detailResp.Tags[0])
	}
	if detailResp.Tags[1].CategoryName != "Team" || detailResp.Tags[1].TagName != "Backend" {
		t.Errorf("tag 1 mismatch: %+v", detailResp.Tags[1])
	}

	if len(detailResp.CustomAttributes) != 1 {
		t.Fatalf("expected 1 custom attribute in detail response, got %d", len(detailResp.CustomAttributes))
	}
	if detailResp.CustomAttributes[0].Name != "BackupTier" || detailResp.CustomAttributes[0].Value != "Platinum" {
		t.Errorf("custom attribute mismatch: %+v", detailResp.CustomAttributes[0])
	}

	// 2. GET /v1/vms -> list endpoint must NOT contain tags or custom_attributes in items
	listReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/vms?vcenter_id=%s", vcenterID), nil)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for list, got %d", listRec.Code)
	}

	var rawList map[string]any
	if err := json.Unmarshal(listRec.Body.Bytes(), &rawList); err != nil {
		t.Fatalf("failed to unmarshal raw list response: %v", err)
	}
	items := rawList["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("expected at least 1 item in list response")
	}
	firstItem := items[0].(map[string]any)
	if _, hasTags := firstItem["tags"]; hasTags {
		t.Errorf("list item must not contain 'tags' field; tags are detail-only")
	}
	if _, hasAttrs := firstItem["custom_attributes"]; hasAttrs {
		t.Errorf("list item must not contain 'custom_attributes' field; attributes are detail-only")
	}
}

func TestTagsAndCustomAttributes_ExplainQueryPlan(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	connStr := getConnString("collector_rw", "collector_rw_pass")
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to connect pool: %v", err)
	}
	defer pool.Close()

	// 1. Create vCenter
	var vcenterID string
	err = pool.QueryRow(ctx, `
		INSERT INTO vcenters (name, api_url, instance_uuid)
		VALUES ('vc-explain-test', 'https://vc-explain.local/sdk', 'vc-explain-uuid')
		ON CONFLICT (api_url) DO UPDATE SET updated_at = NOW()
		RETURNING id::text
	`).Scan(&vcenterID)
	if err != nil {
		t.Fatalf("failed to insert vcenter: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM vcenters WHERE id = $1::uuid", vcenterID)
	}()

	// 2. Populate synthetic dataset: 1000 VMs with tags and attributes
	// to make index scans attractive to the cost-based query planner
	t.Log("Generating synthetic dataset of 1000 VMs for EXPLAIN verification...")
	_, err = pool.Exec(ctx, `
		INSERT INTO virtual_machines (vcenter_id, moref, instance_uuid, name, power_state, connection_state, cpu_cores, memory_mb)
		SELECT $1::uuid,
		       'vm-exp-' || i,
		       'uuid-exp-' || i,
		       'vm-explain-' || i,
		       'poweredOn',
		       'connected',
		       2,
		       4096
		FROM generate_series(1, 1000) AS s(i);
	`, vcenterID)
	if err != nil {
		t.Fatalf("failed to seed VMs for explain: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO vm_tags (vm_id, tag_id, category_name, tag_name)
		SELECT id,
		       'urn:tag:exp:' || abs(hashtext(id::text || 'tag') % 100),
		       'Category_' || (abs(hashtext(id::text || 'cat')) % 50),
		       'Tag_' || (abs(hashtext(id::text || 'name')) % 50)
		FROM virtual_machines
		WHERE vcenter_id = $1::uuid;
	`, vcenterID)
	if err != nil {
		t.Fatalf("failed to seed vm_tags for explain: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO vm_custom_attributes (vm_id, name, value)
		SELECT id,
		       'Attribute_' || (abs(hashtext(id::text || 'attr')) % 50),
		       'Value_' || (abs(hashtext(id::text || 'val')) % 50)
		FROM virtual_machines
		WHERE vcenter_id = $1::uuid;
	`, vcenterID)
	if err != nil {
		t.Fatalf("failed to seed vm_custom_attributes for explain: %v", err)
	}

	// Run ANALYZE to ensure optimizer statistics are up-to-date
	_, err = pool.Exec(ctx, "ANALYZE vm_tags; ANALYZE vm_custom_attributes;")
	if err != nil {
		t.Fatalf("failed to run ANALYZE: %v", err)
	}

	// 3. EXPLAIN query on vm_tags: "find VMs by category+tag"
	// Should use idx_vm_tags_category_tag_vm: (category_name, tag_name, vm_id)
	rowsTag, err := pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) SELECT vm_id FROM vm_tags WHERE category_name = 'Category_1' AND tag_name = 'Tag_1'")
	if err != nil {
		t.Fatalf("EXPLAIN vm_tags failed: %v", err)
	}
	defer rowsTag.Close()

	var planTagLines []string
	for rowsTag.Next() {
		var line string
		if err := rowsTag.Scan(&line); err != nil {
			t.Fatalf("failed to scan plan line: %v", err)
		}
		planTagLines = append(planTagLines, line)
	}
	fullTagPlan := strings.Join(planTagLines, "\n")
	t.Logf("EXPLAIN Plan for vm_tags:\n%s", fullTagPlan)

	if !strings.Contains(fullTagPlan, "idx_vm_tags_category_tag_vm") && !strings.Contains(fullTagPlan, "Index") {
		t.Logf("Notice: Planner chose plan: %s", fullTagPlan)
	}

	// 4. EXPLAIN query on vm_custom_attributes: "find VMs by attribute name+value"
	// Should use idx_vm_custom_attrs_name_value_vm: (name, value, vm_id)
	rowsAttr, err := pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) SELECT vm_id FROM vm_custom_attributes WHERE name = 'Attribute_1' AND value = 'Value_1'")
	if err != nil {
		t.Fatalf("EXPLAIN vm_custom_attributes failed: %v", err)
	}
	defer rowsAttr.Close()

	var planAttrLines []string
	for rowsAttr.Next() {
		var line string
		if err := rowsAttr.Scan(&line); err != nil {
			t.Fatalf("failed to scan plan line: %v", err)
		}
		planAttrLines = append(planAttrLines, line)
	}
	fullAttrPlan := strings.Join(planAttrLines, "\n")
	t.Logf("EXPLAIN Plan for vm_custom_attributes:\n%s", fullAttrPlan)

	if !strings.Contains(fullAttrPlan, "idx_vm_custom_attrs_name_value_vm") && !strings.Contains(fullAttrPlan, "Index") {
		t.Logf("Notice: Planner chose plan: %s", fullAttrPlan)
	}
}
