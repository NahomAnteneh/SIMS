# Sprint 3 Discrepancy & Verification Register

**Branch:** `sprint-3/search-and-api`  
**Created:** 2026-10-06  
**Method:** Code and verified tests are the source of truth (AGENTS.md §4). Every phase is verified against live PostgreSQL 18, vcsim, and API contracts.

---

## Sprint 3 Phase Tracking

| Phase | Story / Topic | Key Invariants & Guardrails | Verification Target | Status |
|:---|:---|:---|:---|:---:|
| **Phase A** | S3.1 Query Contract (`api/openapi.yaml`) | RFC 7807 (`application/problem+json`), OpenAPI 3.1 validation, all S3 endpoints declared | `TestOpenAPIContractValidation` | [ ] Pending |
| **Phase A** | S3.2 Search Classifier (`internal/search/classifier.go`) | Fast input classification (UUID, MoRef, IPv4, IPv6, MAC, exact, hostname, trigram) | `TestSearchClassifier` | [ ] Pending |
| **Phase A** | S3.3 Filter AST (`internal/search/ast.go`) | Typed recursive AST (Logical, Comp, Network, Hierarchy, Metadata), 400 on unknown fields | `TestFilterASTValidation` | [ ] Pending |
| **Phase B** | S3.4 SQL Compiler (`internal/search/compiler.go`) | Positional parameterized SQL (`$1...`), zero string interpolation, canonical node ordering | `TestSQLCompiler` | [ ] Pending |
| **Phase B** | S3.5 Search Indexes (`000011_search_indexes.sql`) | Trigram GIN, ltree GiST, multirange GiST, inet GiST/SP-GiST, keyset pagination indexes | `make test-migration` | [ ] Pending |
| **Phase B** | S3.6 Query Plan Regression (`tests/integration/query_plan_test.go`) | `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` index scans on hot paths, 0 seq scans | `TestQueryPlanRegression` | [ ] Pending |
| **Phase C** | S3.7 Hardened Keyset Cursor (`internal/api/cursor.go`) | HMAC-SHA256 signature, query hash, multiple sort tuples, tamper detection | `TestCursorTampering` | [ ] Pending |
| **Phase C** | S3.8 OIDC Authentication (`internal/auth/oidc.go`) | Discovery, JWKS caching & rotation, JWT claims validation, dev bypass forbidden in prod | `TestOIDCAuthentication` | [ ] Pending |
| **Phase C** | S3.9 Scope Injection (`internal/auth/scope.go`) | Mandatory tenant scope injection (`vcenter_id IN`, `folder_path <@`), negative bypass tests | `TestScopeBypassPenetration` | [ ] Pending |
| **Phase D** | S3.10 Search Performance Path (`internal/api/search.go`) | Direct pgx row scanning, streaming response, 0 reflection overhead | `TestSearchAPIStreaming` | [ ] Pending |
| **Phase D** | S3.11 Facets & Metadata (`internal/api/facets.go`, `meta.go`) | Scope-aware facets, `/v1/meta/vm-fields`, `/v1/tags`, `/v1/custom-attributes` | `TestFacetsAndMetadata` | [ ] Pending |
| **Phase D** | S3.12 Topology Read Endpoints (`internal/api/topology.go`) | `/v1/{datacenters,clusters,hosts,datastores,networks,folders}`, `/v1/me` | `TestTopologyEndpoints` | [ ] Pending |
| **Phase E** | S3.13 vCenters Schema & Audit (`000012_vcenters_management_and_audit.sql`) | Expand vcenters metadata, `audit_events` ledger, least privilege | `make test-migration` | [ ] Pending |
| **Phase E** | S3.13 SSRF Guard (`internal/netutil/ssrf.go`) | Go `net/netip`, deny loopback/link-local/metadata, single DNS resolve, no redirects | `TestSSRFGuard` | [ ] Pending |
| **Phase E** | S3.13 vCenter Validation Pipeline (`internal/vsphere/validator.go`) | TCP/TLS handshake, version >= 7.0 check, tagging probe, safe teardown | `TestVCenterValidator` | [ ] Pending |
| **Phase E** | S3.13 vCenter Management API (`internal/api/vcenters.go`) | Idempotent POST, test-connection, PATCH, DELETE tombstoning, `inventory-admin` role | `TestVCenterManagementAPI` | [ ] Pending |
| **Phase F** | S3.14 Test Gates (`tests/integration/...`) | AST fuzzer, cursor fuzzer, SQL injection suite, credential leak scanner | `make test-gate-s3` | [ ] Pending |
| **Phase F** | S3.14 Sprint 3 Demo (`scripts/demo-s3.sh`) | End-to-end automated demonstration of search, facets, auth, vCenter registration | `make demo-s3` | [ ] Pending |

---

## Discrepancies & Resolutions Log

| ID | Topic | Discrepancy Description | Resolution & Evidence | Status |
|:---|:---|:---|:---|:---:|
| D1 | Open | To be populated during implementation | | Open |
