# Sprint 3 Discrepancy & Verification Register

**Branch:** `sprint-3/search-and-api`  
**Created:** 2026-10-06  
**Method:** Code and verified tests are the source of truth (AGENTS.md §4). Every phase is verified against live PostgreSQL 18, vcsim, and API contracts.

---

## Sprint 3 Phase Tracking

| Phase | Story / Topic | Key Invariants & Guardrails | Verification Target | Status |
|:---|:---|:---|:---|:---:|
| **Phase A** | S3.1 Query Contract (`api/openapi.yaml`) | RFC 7807 (`application/problem+json`), OpenAPI 3.1 validation, all S3 endpoints declared | `TestOpenAPIContractValidation` | [x] Verified (`1b0cb7b`) |
| **Phase A** | S3.2 Search Classifier (`internal/search/classifier.go`) | Fast input classification (UUID, MoRef, IPv4, IPv6, MAC, exact, hostname, trigram) | `TestSearchClassifier` | [x] Verified (`1b0cb7b`) |
| **Phase A** | S3.3 Filter AST (`internal/search/ast.go`) | Typed recursive AST (Logical, Comp, Network, Hierarchy, Metadata), 400 on unknown fields | `TestFilterASTValidation` | [x] Verified (`1b0cb7b`) |
| **Phase B** | S3.4 SQL Compiler (`internal/search/compiler.go`) | Positional parameterized SQL (`$1...`), zero string interpolation, canonical node ordering | `TestSQLCompiler` | [x] Verified (`4c35c66`) |
| **Phase B** | S3.5 Search Indexes (`000011_search_indexes.sql`) | Trigram GIN, ltree GiST, multirange GiST, inet GiST/SP-GiST, keyset pagination indexes | `make test-migration` | [x] Verified (`4c35c66`) |
| **Phase B** | S3.6 Query Plan Regression (`tests/integration/query_plan_test.go`) | `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` index scans on hot paths, 0 seq scans | `TestQueryPlanRegression` | [x] Verified (`4c35c66`) |
| **Phase C** | S3.7 Hardened Keyset Cursor (`internal/api/cursor.go`) | HMAC-SHA256 signature, query hash, multiple sort tuples, tamper detection | `TestCursorTampering` | [x] Verified (`c981ce7`) |
| **Phase C** | S3.8 OIDC Authentication (`internal/auth/oidc.go`) | Discovery, JWKS caching & rotation, JWT claims validation, dev bypass forbidden in prod | `TestOIDCAuthentication` | [x] Verified (`c981ce7`) |
| **Phase C** | S3.9 Scope Injection (`internal/auth/scope.go`) | Mandatory tenant scope injection (`vcenter_id IN`, `folder_path <@`), negative bypass tests | `TestScopeBypassPenetration` | [x] Verified (`c981ce7`) |
| **Phase D** | S3.10 Search Performance Path (`internal/api/search.go`) | Direct pgx row scanning, streaming response, 0 reflection overhead | `TestSearchAPIStreaming` | [x] Verified (`b3e5395`) |
| **Phase D** | S3.11 Facets & Metadata (`internal/api/facets.go`, `meta.go`) | Scope-aware facets, `/v1/meta/vm-fields`, `/v1/tags`, `/v1/custom-attributes` | `TestFacetsAndMetadata` | [x] Verified (`b3e5395`) |
| **Phase D** | S3.12 Topology Read Endpoints (`internal/api/topology.go`) | `/v1/{datacenters,clusters,hosts,datastores,networks,folders}`, `/v1/me` | `TestTopologyEndpoints` | [x] Verified (`b3e5395`) |
| **Phase E** | S3.13 vCenters Schema & Audit (`000012_vcenters_management_and_audit.sql`) | Expand vcenters metadata, `audit_events` ledger, least privilege | `make test-migration` | [x] Verified (`326be76`) |
| **Phase E** | S3.13 SSRF Guard (`internal/netutil/ssrf.go`) | Go `net/netip`, deny loopback/link-local/metadata, single DNS resolve, no redirects | `TestSSRFGuard` | [x] Verified (`326be76`) |
| **Phase E** | S3.13 vCenter Validation Pipeline (`internal/vsphere/validator.go`) | TCP/TLS handshake, version >= 7.0 check, tagging probe, safe teardown | `TestVCenterValidator` | [x] Verified (`326be76`) |
| **Phase E** | S3.13 vCenter Management API (`internal/api/vcenters.go`) | Idempotent POST, test-connection, PATCH, DELETE tombstoning, `inventory-admin` role | `TestVCenterManagementAPI` | [x] Verified (`326be76`) |
| **Phase F** | S3.14 Test Gates (`tests/integration/gate_s3_test.go`) | AST fuzzer, cursor fuzzer, SQL injection suite, credential leak scanner | `make test-gate-s3` | [x] Verified (`4d4d87b`) |
| **Phase F** | S3.14 Sprint 3 Demo (`scripts/demo-s3.sh`) | End-to-end automated demonstration of search, facets, auth, vCenter registration | `make demo-s3` | [x] Verified (`4d4d87b`) |

---

## Discrepancies & Resolutions Log

| ID | Topic | Discrepancy Description | Resolution & Evidence | Status |
|:---|:---|:---|:---|:---:|
| D1 | Cursor TTL Guard | Setting `ttl <= 0` in `NewCursorSigner` fell back to `DefaultCursorTTL` (24h) to protect against expired-by-default production tokens. | Tests validating expired cursor handling must explicitly manipulate `ExpiresAt` before encoding rather than relying on 0 TTL constructor arguments. Verified in `TestGateS3_CursorTamperingFuzzer`. | Resolved |
| D2 | TLS Fingerprint Verification (Gosec G402) | Static analysis flags `InsecureSkipVerify: true` even when using custom `VerifyConnection` callback for certificate fingerprint pinning. | Annotated with `// #nosec G402` and documented security rationale: TLS certificate chain is cryptographically verified against pinned SHA-256 fingerprint or custom CA cert pool before establishing connection. | Resolved |
| D3 | Port Collision in Demo Script | Background Docker service `sims-query-api` occupies port 8080, causing bind collision for local test server instances. | Configured `demo-s3.sh` to run the demonstration instance on `PORT=8082` with dynamic readiness probe on `/readyz`. | Resolved |
| D4 | Foreign Key Dependency in Demo Fixtures | Inserting virtual machine fixtures into PostgreSQL requires prerequisite rows in `datacenters`, `clusters`, and `inventory_folders`. | Added prerequisite rows to `scripts/demo-s3.sh` database seeding block with proper cascading cleanups. Verified in `make demo-s3`. | Resolved |
