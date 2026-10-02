# Engineering Report: vSphere Tags and Custom Attributes Inventory Support

**Status:** Completed  
**Branch:** `feature/vm-tags-custom-attributes`  
**Scope:** Backend Only (`web/` untouched)  
**Date:** 2026-10-02  

---

## 1. Executive Summary

This feature extends the SIMS backend inventory pipeline to support **vSphere Tags** (categories and tag names) and **Custom Attributes** (key/value pairs) on virtual machines, following the existing database, hydration, persistence, and API patterns:

1. **Database Layer (Migration 000006):**
   - Tables: `vm_tags` and `vm_custom_attributes` with primary keys, foreign keys cascading on delete to `virtual_machines(id)`, and uniqueness constraints (`UNIQUE(vm_id, tag_id)` and `UNIQUE(vm_id, name)`).
   - Compound indexes: `idx_vm_tags_category_tag_vm` on `(category_name, tag_name, vm_id)` and `idx_vm_custom_attrs_name_value_vm` on `(name, value, vm_id)`.
   - Role privileges: `collector_rw` granted DML only (`SELECT`, `INSERT`, `UPDATE`, `DELETE`); `query_ro` granted `SELECT` only; zero DDL for both.
2. **vSphere Hydration Layer:**
   - **Custom Attributes:** Populated from the VM `customValue` property (SOAP `RetrievePropertiesEx`), resolving integer keys to attribute names using `CustomFieldsManager.field` definitions cached in the topology snapshot.
   - **vSphere Tags:** Hydrated via a dedicated REST client (`vapi/rest` and `vapi/tags.Manager`). Preloads tag and category definitions, performs bounded batch lookups (`ListAttachedTagsOnObjects` with max 500 objects), applies strict context timeouts, and logs out cleanly on exit.
   - **Graceful Degradation:** A failure or absence of the tagging REST service (e.g., against standalone `vcsim` or during a network partition) returns typed `ErrTaggingUnavailable`, logs a warning, increments `sims_tag_fetch_failures_total`, and allows inventory hydration to complete successfully with tags degraded.
3. **Persistence Layer:**
   - Employs atomic **replace-set semantics** in the same transaction as the VM upsert merge: deletes existing tags/attributes for batch VMs and inserts the hydrated set via binary `COPY FROM STDIN` into temporary staging tables.
   - Batch compaction collapses multi-update sequences so the final compacted record carries the final tag/attribute sets. Batch replay is verified idempotent.
4. **API Layer (Backend Only):**
   - Added `tags` and `custom_attributes` arrays to `GET /v1/vms/{id}` detail response.
   - The list endpoint `GET /v1/vms` remains strictly unchanged (detail-only fields omitted).
   - Updated `api/openapi.yaml` (with `VMTag` and `VMCustomAttribute` components), verified via OpenAPI 3.1 JSONSchema validator.
5. **Observability:**
   - Registered 4 Prometheus metrics on `/metrics`: `sims_tag_fetch_duration_seconds`, `sims_tag_fetch_failures_total`, `sims_tags_persisted_total`, and `sims_custom_attributes_persisted_total`.
6. **Architecture & Documentation:**
   - Updated `docs/vsphere-capability-matrix.md` with empirical vcsim findings and instructions for real vCenter validation.
   - Published [ADR-016](adr/ADR-016.md) (Proposed) detailing REST vs SOAP protocol separation, field ownership, and Sprint 2/4 polling reconciliation.
   - Updated `docs/database.md` and `docs/api.md`.

---

## 2. Verification Evidence and Test Results

### 2.1 Database & Migrations
- **Goose Migration Verification (`make test-migration`):**
  - Up -> Down -> Up verified forward-compatible and idempotent.
- **Table Role Privileges (`TestAllApplicationTablesRolePrivileges`):**
  - Verified across all 18 application tables (including `vm_tags` and `vm_custom_attributes`):
  - `collector_rw`: INSERT, UPDATE, DELETE, SELECT pass; DDL (`CREATE TABLE`) fails with `ERROR: permission denied for schema public (SQLSTATE 42501)`.
  - `query_ro`: SELECT passes; INSERT, UPDATE, DELETE fail with `ERROR: permission denied for table ... (SQLSTATE 42501)`.

### 2.2 Normalization & Custom Fields Unit Tests
- **`TestNormalizeVMCustomAttributes` (`internal/collector/normalizer_test.go`):**
  - Multiple attributes with key-to-name resolution: PASS.
  - Unset/empty attribute values omitted: PASS.
  - Unknown keys not in `CustomFieldsByKey` safely ignored without panic: PASS.
  - VM with nil custom values returns empty slice: PASS.

### 2.3 Tag Fetcher & Degradation Tests
- **`TestRestTagFetcher_LiveVAPISimulator` (`internal/collector/tag_fetcher_test.go`):**
  - Tested against live govmomi `vapi/simulator` mounted on VPX model.
  - Created categories (`Environment`, `CostCenter`) and tags (`Production`, `Finance-90210`).
  - Attached tags to simulator VMs, authenticated REST session, preloaded definitions, and verified bounded batch retrieval across multiple VMs: PASS.
- **`TestRestTagFetcher_DegradationOnFailure`:**
  - Verified unreachable endpoint returns wrapped `ErrTaggingUnavailable`: PASS.
  - Verified uninitialized fetcher safely returns `ErrTaggingUnavailable` without panicking: PASS.
- **`TestHydrateInventory_TagFetcherDegradationAndEnrichment` (`internal/collector/hydrator_test.go`):**
  - Tag fetcher failure allows inventory hydration to succeed cleanly with zero leaked views: PASS.
  - Tag fetcher successfully enriches VM records with resolved tags: PASS.
- **`TestHydrateInventoryAndTopology_CustomFields`:**
  - Added custom field to `CustomFieldsManager` and set value on VM.
  - Topology snapshot successfully loaded custom field definitions into `CustomFieldsByKey`, and inventory hydration normalized VM custom attributes: PASS.

### 2.4 Persistence Integration Tests
- **`TestTagsAndCustomAttributesPersistenceAndReplaceSet` (`tests/integration/tags_attributes_test.go`):**
  - Initial batch write persists tags and attributes: PASS.
  - Replace-set semantics: removed tags/attributes deleted, updated attributes modified in place: PASS.
  - Replay idempotency: re-running identical batch produces zero duplicate rows or errors: PASS.
  - Total removal: empty tag/attribute slice cleans up all rows for target VM: PASS.

### 2.5 Query Plan Index Verification (PostgreSQL `EXPLAIN`)
- **`TestTagsAndCustomAttributes_ExplainQueryPlan`:**
  - Executed on a synthetic dataset of 1,000 VMs with tags and custom attributes after `ANALYZE`:
  - **Category + Tag Query:**
    ```text
    Index Only Scan using idx_vm_tags_category_tag_vm on vm_tags
      Index Cond: ((category_name = 'Category_1'::text) AND (tag_name = 'Tag_1'::text))
      Execution Time: 0.059 ms
    ```
  - **Custom Attribute Name + Value Query:**
    ```text
    Index Only Scan using idx_vm_custom_attrs_name_value_vm on vm_custom_attributes
      Index Cond: ((name = 'Attribute_1'::text) AND (value = 'Value_1'::text))
      Execution Time: 0.050 ms
    ```

### 2.6 API & OpenAPI Contract Validation
- **`TestTagsAndCustomAttributesAPI_DetailEndpoint`:**
  - `GET /v1/vms/{id}` returns populated `tags` and `custom_attributes`: PASS.
  - `GET /v1/vms` (list endpoint) confirmed to omit `tags` and `custom_attributes`: PASS.
- **`TestOpenAPIContractValidation` (`tests/integration/openapi_contract_test.go`):**
  - Validates `api/openapi.yaml` against OpenAPI 3.1 specification.
  - Validates that real API JSON responses conform to `VMDetailResponse` and `ListVMsResponse` schemas: PASS.

### 2.7 Observability
- **`TestCoreEndpoints/GET_/metrics_returns_prometheus_text` (`internal/server/server_test.go`):**
  - Confirmed `sims_tag_fetch_duration_seconds`, `sims_tag_fetch_failures_total`, `sims_tags_persisted_total`, and `sims_custom_attributes_persisted_total` are exported and formatted correctly on `/metrics`: PASS.

### 2.8 Frontend Non-Interference Check
- Executed `git diff main --stat -- web/`:
  - Output is strictly **0 files changed, 0 insertions, 0 deletions**.
