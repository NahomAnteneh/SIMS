# Sprint 5 Discrepancy & Verification Register

**Branch:** `sprint-5/release-hardening`
**Created:** 2026-10-06
**Method:** Code and verified tests are the source of truth (AGENTS.md §4). Every phase is verified against live PostgreSQL 18, vcsim, and API contracts.

---

## Sprint 5 Phase Tracking

| Phase | Story / Topic | Key Invariants & Guardrails | Verification Target | Status |
|:---|:---|:---|:---|:---: |
| **Phase A** | S5.1, S5.2 Performance Dataset Generator & Query Workload Model (`cmd/dataset-gen`, `internal/benchmark`) | High-throughput synthetic data generator (100k, 500k, 1M VMs; 5M–10M rows) via binary COPY; realistic distributions (ltree, multirange, CIDR, trgm); mixed concurrent query workload | `go test -v ./internal/benchmark/...` | [ ] Pending |
| **Phase B** | S5.3, S5.4, S5.5, S5.6 Performance Gates & Postgres / Pool Tuning (`tests/integration/perf_query_test.go`, `perf_ingest_test.go`, `internal/database/pool.go`, `docs/postgres-tuning.md`) | Query SLO: DB P99 ≤ 5 ms; Ingestion SLO: Change → DB P95 ≤ 250 ms, Batch write P95 ≤ 15 ms, P99 ≤ 50 ms; Isolated connection pool budgets (`query_ro`, `collector_rw`, `reconciliation`, `admin`) | `make test-perf-query && make test-perf-ingest` | [ ] Pending |
| **Phase C** | S5.7 OpenTelemetry Instrumentation & Observability (`internal/telemetry/tracer.go`, collector, API, reconciler) | W3C tracecontext propagation; HTTP, AST compiler, DB, and collector pipeline instrumentation; **CRITICAL:** 0 secrets/passwords/tokens in span attributes or logs | Unit & Integration Tracing Tests | [ ] Pending |
| **Phase D** | S5.8 Security Hardening & Secret-Leak Auditing (`tests/integration/security_test.go`) | Secret leak scan across logs, spans, API payloads, audit events; SSRF defenses; JWT rotation & RBAC; DB least privilege; SQL injection & cursor tampering fuzzing; `make vulncheck` | `make vulncheck && go test -v -tags=integration ./tests/integration -run TestSecurityAudit` | [ ] Pending |
| **Phase E** | S5.9, S5.10 Chaos Resilience Suite & 72-Hour Soak Harness (`tests/integration/chaos_test.go`, `tests/soak`, `scripts/soak-test.sh`) | vCenter drop/restart, network partition/latency, DB loss/slowdown, worker crash/takeover, event burst, tag REST outage, mixed fleet; Soak runner for heap RSS & goroutine leaks | `make test-chaos && go test -v -tags=integration ./tests/soak` | [ ] Pending |
| **Phase F** | S5.11–S5.15 Packaging, Release Candidate (v1.0.0), Final Gate & Demo (`deploy/`, `docs/`, `tests/integration/gate_s5_test.go`, `scripts/demo-s5.sh`) | Non-root minimal Dockerfiles; complete operational documentation; frozen OpenAPI v1.0.0; Sprint 5 Gate & end-to-end Demo | `make test-gate-s5 && make demo-s5` | [ ] Pending |

---

## Discrepancies & Resolutions Log

| ID | Topic | Discrepancy Description | Resolution & Evidence | Status |
|:---|:---|:---|:---|:---:|
| - | - | - | - | - |
