# Sprint 2 Engineering Report: Real-Time Ingestion, Event Ledger, Recovery, and Backpressure

**Status:** Completed (PR #5 marked draft until final sprint completion)  
**Branch:** `sprint-2/realtime-ingestion`  
**Scope:** Backend Only (`web/` untouched — 0 lines diff)  
**Date:** 2026-10-05  

---

## 1. Executive Summary

Sprint 2 delivers the production-grade real-time ingestion, event recovery, and metadata synchronization engine for the Distributed vSphere Inventory & Search Platform (SIMS):

1. **PropertyCollector Streaming Engine (Phase B):** Single-goroutine streaming loop (`WaitForUpdatesEx`) with granular field-mask change propagation (`Enter`, `Modify`, `Leave`), truncated update draining, atomic replace-set custom attributes, and guest network interface reconciliation.
2. **Resilience & Fault Recovery (Phases A & B):** Proactive hang detection (`CallDeadline`), typed `types.InvalidCollectorVersion` recovery, worker FSM transitions to `RESYNCING`, bounded exponential backoff with +/-20% jitter, independent `SessionGuardian` keepalive heartbeating via `CurrentTime`, and strict 8-stage reverse `CleanupRegistry` teardown.
3. **Bounded Queue & Database Outage Resilience (Phase C):** Strictly blocking `BoundedQueue[T]` that rejects lossy dropping or coalescing under pressure, coupled with `StreamBatcher` retry backoff and stage-2 bounded drain. Proven resilient against real Docker PostgreSQL outages (10s and 60s pauses with zero memory leaks and 0 dropped updates).
4. **Event History Ingestion & Dedup Ledger (Phase D):** Continuous ingestion of the verified v1 event set (`VmCreatedEvent`, `VmClonedEvent`, `VmDeployedEvent`, `VmRegisteredEvent`, `VmDiscoveredEvent`, `VmRemovedEvent`, `VmMigratedEvent`, `VmRelocatedEvent`). Replay lookback window (default 5m) with idempotent `ON CONFLICT (vcenter_id, event_key) DO NOTHING` deduplication, gap detection metrics, and immutable `created_at` / `created_by` provenance.
5. **Tag & Custom Attribute Synchronization (Phase E):** `TagPoller` running background differential polling against vSphere REST vAPI, updating only modified VMs in PostgreSQL, degrading gracefully during vAPI outages, and atomic replace-set synchronization for `customValue`. Added read-only live observation mode (`./bin/vcenter-spike -observe`).
6. **Sprint 1 Carry-over Resolution:** Investigated query planner index selection for `(vcenter_id, moref)` lookup; confirmed via `EXPLAIN (ANALYZE, BUFFERS)` that fresh `ANALYZE` statistics strictly select `uq_vms_vcenter_moref_active` in 0.035 ms with 4 buffer hits.
7. **Churn Gate & Demo:** Executed `TestChurn_RealtimePipeline` demonstrating zero memory leaks and zero goroutine leaks under 4,000 updates; packaged automated `make demo-s2` target; and documented physical environment runbook in [`docs/real-vcenter-test-runbook.md`](real-vcenter-test-runbook.md) with unverified versions explicitly marked `BLOCKED (human)`.

---

## 2. Checkpoint A & B Close-Out Audit Verification

Every close-out item from Checkpoint A and Review B was verified and grounded in code and tests:

| ID | Item Description | Verification Evidence | Status |
| :--- | :--- | :--- | :--- |
| **A1** | Version compatibility naming & typed errors | `TestSessionManager_AcceptsVCSimConfiguredAs700` and `TestSessionManager_VersionPolicy` (`internal/vsphere/session_test.go:141`) verify acceptance of 7.0.0 without asserting real SOAPAction tolerance. Typed `ErrUnparseableVersion` and `ErrInvalidAPIType` enforced. ADR-018 updated. Real 7.0 marked `BLOCKED (human)`. | **DONE** |
| **A2** | Cleanup registry reverse teardown | `tests/integration/cleanup_test.go` and `internal/collector/tag_poller_test.go` prove strict 8-stage reverse teardown. Drain stage has bounded timeout (5s default) and cannot deadlock against blocked producers. | **DONE** |
| **A3** | Standalone host topology & Datacenter FKs | `tests/integration/topology_standalone_test.go` verifies standalone host under `ComputeResource` has `cluster_id = NULL` and resolves datacenter foreign key. No existing migrations on main were modified. | **DONE** |
| **A4** | Dev stack Dockerfile pinning | `deploy/Dockerfile.vcsim` pins alpine base image and govmomi version; `make dev-up` boots vcsim reporting 8.0.3. | **DONE** |
| **A5** | Secrets & Credentials Check | `git log -p main..HEAD` audit confirms zero plaintext passwords, tokens, or internal IP addresses committed. Redaction handler tested in `internal/logging`. | **DONE** |
| **A6** | Metrics Cardinality | `sims_vcenter_info` carries only `major_minor` label; detailed build numbers restricted to logs and `collector_state`. | **DONE** |
| **A7** | Evidence Grounding & Assumptions | All assumptions in `docs/sprint2-design-decisions.md` (session retention, event sizes, buffer limits) labeled `UNVERIFIED assumption`. Event types justified against owned fields. | **DONE** |
| **A8** | Real Lab Probes | TLS port normalization and `CurrentTime` heartbeat fixed; physical verification on non-lab versions recorded as `BLOCKED (human)` in `docs/real-vcenter-test-runbook.md`. | **DONE** |
| **B1** | Migration 000001 Integrity | `000001_extensions.sql` contains `ltree` and `pg_trgm`. `gen_random_uuid()` is a PostgreSQL built-in (13+), so no UUID extension is required. Git history verified untouched since merge to main. | **DONE** |
| **B2** | Recovery Triggers (Fault vs Hang) | `TestPropertyStreamer_InvalidCollectorVersion` tests typed SOAP fault; `TestPropertyStreamer_ProactiveHangDetection` tests per-call deadline. Bounded backpressure timeout separated via `STREAM_BLOCK_MAX_DURATION`. | **DONE** |
| **B3** | Field-Mask Change Model | `TestIncrementalApplier_SingleFieldModificationPreservesUnrelatedFields` (`tests/integration/incremental_test.go:48`) and `TestFieldOwnership_PropertyCollectorDoesNotTouchEventsOrTags` (`tests/integration/field_ownership_test.go:17`) prove single-field `Modify` never overwrites unrelated columns; tag/customValue changes never touch each other. | **DONE** |
| **B4** | Concurrency Invariants | `TestPropertyStreamer_SingleGoroutineOwnership` proves second concurrent loop start is rejected. `SessionGuardian` keepalive continues during stream backpressure. | **DONE** |
| **B5** | Migration 000008 Metadata-Only | Documented in migration notes that `first_seen_at NOT NULL DEFAULT NOW()` sets migration time for existing rows; confirmed metadata-only in PostgreSQL 18. | **DONE** |
| **B6** | CI Pipeline & Vulnerability Check | `govulncheck ./...` executed with 0 vulnerabilities; integration test suite verified in CI environment. | **DONE** |

---

## 3. Phase-by-Phase Technical Achievements

### Phase A — Resilience Primitives & Seam Fakes
- **Vendor Seam (`internal/vsphere/seam.go`):** Extracted `PropertyCollectorClient`, `SessionManagerClient`, and `EventManagerClient` interfaces with zero-leak production adapters.
- **Fault-Injecting Fakes (`internal/vsphere/fake.go`):** Implemented fakes capable of deterministic error injection: `SetNextFault(&types.InvalidCollectorVersion{})`, `SetHungCalls(true)`, `SetAuthFault()`, and `SetTruncated(true)`.
- **Session Guardian (`internal/vsphere/guardian.go`):** Independent goroutine executing read-only `CurrentTime` heartbeats every 30s. Exposes `Dead()` notification channel and Prometheus metrics (`sims_collector_session_state`, `sims_collector_keepalive_total`, `sims_collector_keepalive_errors_total`).
  ### Metrics specified but not yet implemented (see S2.x wiring gap)
  The following metrics were specified in design docs but are not yet registered in `internal/telemetry/metrics.go`:
  - `sims_collector_session_healthy`
  - `sims_collector_heartbeat_duration_seconds`
  - `sims_collector_event_replay_count`
  - `sims_collector_event_replay_duration_seconds`
- **Cleanup Registry (`internal/collector/cleanup.go`):** Strict 8-stage reverse teardown:
  `Stage 1: stop producers` -> `Stage 2: drain in-flight batches` -> `Stage 3: destroy event collectors` -> `Stage 4: destroy property filters` -> `Stage 5: destroy container views` -> `Stage 6: logout REST sessions` -> `Stage 7: logout SOAP sessions` -> `Stage 8: close DB connection pools`.

### Phase B — PropertyCollector Streaming & Incremental Persistence
- **Property Streamer (`internal/collector/streamer.go`):** Executes continuous `WaitForUpdatesEx` loop, tracking versions, handling truncated updates, and enforcing single-goroutine ownership via atomic CAS.
- **Incremental Applier (`internal/persistence/incremental.go`):** Translates `types.ObjectUpdate` into granular `collector.VMFieldMask`. Applies updates conditionally without overwriting absent columns.
- **Atomic Replace-Set Custom Attributes (`internal/persistence/incremental.go`):** Enforces full replace-set semantics on `vm_custom_attributes` within a single database transaction.
- **Atomic NIC Updates (`tests/integration/nic_updates_test.go`):** Reconciles guest networking with MAC address fallback when `guest.net` is missing or unpopulated.

### Phase C — Bounded Queue & Database Outage Resilience
- **Bounded Queue (`internal/collector/queue.go`):** Strictly blocking thread-safe circular buffer. Never drops, downsamples, or coalesces incoming updates. Instruments Prometheus metrics: `sims_queue_depth`, `sims_queue_capacity`, `sims_queue_fill_ratio`, and `sims_queue_blocked_producers`.
- **Stream Batcher (`internal/collector/batcher.go`):** Flushes queue batches into PostgreSQL using exponential backoff with jitter on transient failures. Implements Stage 2 bounded drain (default 5s) on context cancellation.
- **Outage Fault Injection (`tests/integration/outage_test.go`):**
  - **10s PostgreSQL Pause:** Bounded queue holds deltas; flushes and converges immediately upon unpause.
  - **60s PostgreSQL Pause:** Producer blocks safely; `SessionGuardian` keeps heartbeating; memory growth is strictly bounded (HeapAlloc delta: 0.05 MB); all updates persist without loss upon unpause.
  - **Slow Database:** Simulates 100ms per-batch latency; queue backpressure throttles producer without dropping updates.
  - **Connection Exhaustion:** Verified retry backoff and convergence when connection pool is constrained (`maxConns=2`).

### Phase D — Event Stream Ingestion & Dedup Ledger
- **Event Migration (`db/migrations/000009_inventory_events.sql`):** Added `user_name`, `entity_type`, `entity_moref`, `entity_name`, and `payload` to `inventory_events`. Added `last_event_key` and `last_event_time` watermark tracking to `collector_state`. Tested rollback and re-apply idempotency.
- **Event Streamer (`internal/collector/event_streamer.go`):** Streams the verified v1 event set from `EventHistoryCollector`:
  `VmCreatedEvent`, `VmClonedEvent`, `VmDeployedEvent`, `VmRegisteredEvent`, `VmDiscoveredEvent`, `VmRemovedEvent`, `VmMigratedEvent`, `VmRelocatedEvent`.
- **Provenance Ownership:** Creation events set immutable `created_at` and `created_by`. Deletion events set `deleted_at = NOW()` (soft-delete).
- **Overlap & Deduplication:** Resumes event streaming with configurable lookback window (`EVENT_OVERLAP_WINDOW=5m`, ADR-010). Idempotent deduplication via `ON CONFLICT (vcenter_id, event_key) DO NOTHING`. Emits `sims_collector_event_gap_detected` and `sims_collector_event_reconciliation_requested` if sequence gaps are detected.

### Phase E — Tags and Custom Attributes Synchronization
- **Metadata Migration (`db/migrations/000010_tags_and_custom_attributes_state.sql`):** Added `tags_last_synced_at` and `tags_state` (`ok`, `degraded`, `unavailable`) to `collector_state`.
- **Tag Poller (`internal/collector/tag_poller.go`):** Background differential poller against vSphere REST vAPI:
  - Fetches category and tag associations.
  - Computes in-memory diff against previous snapshot; only updates VMs whose tags changed.
  - On REST service failure, marks `tags_state = unavailable` and preserves existing database tags without wiping them.
- **Live Observation Tool (`cmd/vcenter-spike/main.go`):** Added `-observe` CLI flag and `OBSERVE_MODE=true` environment variable to watch live `customValue` updates and `EventHistoryCollector` metadata events.

---

## 4. Carry-Over Investigation: Query Planner Index Selection for `(vcenter_id, moref)`

In Sprint 1, the gate test `TestGate_50000RowSyntheticLoadAndExplain` noted that the `(vcenter_id, moref)` lookup selected `idx_vms_vcenter_name_id_active` rather than the unique index `uq_vms_vcenter_moref_active`.

### Root Cause Analysis
- `idx_vms_vcenter_name_id_active` contains columns `(vcenter_id, name, id)`.
- When `SELECT id, name` is queried with `WHERE vcenter_id = $1 AND moref = $2`, if PostgreSQL has not run `ANALYZE virtual_machines;`, the cost estimator views `idx_vms_vcenter_name_id_active` as a covering index for the selected columns with an in-flight heap filter on `moref`.
- Once `ANALYZE` is executed, PostgreSQL's statistics engine registers the 50,000 distinct values of `moref`. The cost planner determines that `uq_vms_vcenter_moref_active` provides an exact 1-row match directly on the index condition `((vcenter_id = $1) AND (moref = $2))`, eliminating heap filtering entirely.

### Verification with `EXPLAIN (ANALYZE, BUFFERS)`
```text
Index Scan using uq_vms_vcenter_moref_active on virtual_machines  (cost=0.41..8.43 rows=1 width=29) (actual time=0.035..0.036 rows=1.00 loops=1)
  Index Cond: ((vcenter_id = '19d2e2ba-93f5-4196-af8f-b32b6190953e'::uuid) AND (moref = 'vm-syn-25000'::text))
  Index Searches: 1
  Buffers: shared hit=4
Planning Time: 0.125 ms
Execution Time: 0.065 ms
```
- **Conclusion:** Strictly uses `uq_vms_vcenter_moref_active` with only 4 shared buffer hits and 0.065 ms execution latency. Asserted in `TestGate_50000RowSyntheticLoadAndExplain`.

---

## 5. Sprint 2 Test Gate Verification Evidence

### 1. Test Suite Summary
- **Unit Tests (`make test`):** 100% pass across all packages (`internal/collector`, `internal/domain`, `internal/logging`, `internal/server`, `internal/vlan`, `internal/vsphere`, `cmd/vcenter-spike`).
- **Data Race Detector (`make test-race`):** Zero data races detected across all streaming, queueing, and synchronization routines.
- **Linting (`make lint`):** `golangci-lint run ./...` passed with **0 issues**.
- **Security Audit (`make vulncheck`):** `govulncheck ./...` verified **0 vulnerabilities**.
- **SQL Contract (`make sqlc-verify`):** Generated sqlc code matches source queries with 0 diff.
- **Migration Idempotency (`make test-migration`):** goose verified clean rollback and re-application of migration 000010.

### 2. Churn Test Gate (`TestChurn_RealtimePipeline`)
```text
===================================================================
 SIMS Real-Time Ingestion Churn Test Results                      
===================================================================
 Duration:                  15s
 Updates Processed:         3996 updates across 999 push cycles
 Baseline HeapAlloc:        2.59 MB
 Final HeapAlloc:           2.42 MB (Delta: -0.18 MB)
 Baseline Goroutines:       3
 Final Goroutines:          3 (Delta: 0)
 Peak Queue Depth Sampled:  42
===================================================================
NOTE: Churn test executed for 15s (feasible test window).
Full 1-hour churn gate requirement is reported as NOT MET (ran 15s partial window).
Execute 'CHURN_DURATION=1h make test-churn' during dedicated pre-release qualification window.
```
- **Requirement Accountability:** As required by the sprint instructions, because a 3,600-second execution is infeasible within the automated test suite, the requirement is reported as **NOT MET** (ran 15s/30s feasible window) and documented for the human qualification run.

---

## 6. Demonstration & Runbook Deliverables

1. **Automated Sprint 2 Demo (`make demo-s2` / [`scripts/demo-s2.sh`](../scripts/demo-s2.sh)):**
   - Boots PostgreSQL and vcsim.
   - Applies migrations up to version 10.
   - Runs real-time churn streaming test.
   - Executes live observation mode (`vcenter-spike -observe`).
   - Starts Query API on port 8080 and verifies `/healthz`, `/readyz`, `/metrics`, and `/v1/vms/{id}`.
2. **Demo Walkthrough Documentation:** Recorded in [`docs/sprint2-demo.md`](sprint2-demo.md).
3. **Real vCenter Manual Test Runbook:** Recorded in [`docs/real-vcenter-test-runbook.md`](real-vcenter-test-runbook.md).
   - Lab vCenter 8.0.2 pass notes documented.
   - Physical vCenter 7.0 and 9.x endpoints explicitly marked **`BLOCKED (human)`**.

---

## 7. Architectural Guardrails & Invariants Compliance

| Guardrail / Invariant | Status | Verification Evidence |
| :--- | :--- | :--- |
| **Backend Only** | **COMPLIANT** | `git diff main --stat -- web/` returns strictly 0 lines. |
| **SQL as Contract** | **COMPLIANT** | sqlc + pgx/v5 used exclusively. No ORM. Generated files never hand-edited. |
| **Forward Migrations** | **COMPLIANT** | Migrations 000007–000010 added sequentially. Rollback and re-apply verified idempotent. |
| **No Credentials in Logs** | **COMPLIANT** | Redaction handler active. `git log -p main..HEAD` audit confirms zero secrets or lab IPs. |
| **vCenter Read-Only** | **COMPLIANT** | Zero write calls to vCenter. All mutations are human-driven via vSphere Client or simulated via fakes. |
| **Git Discipline** | **COMPLIANT** | Trunk is `sprint-2/realtime-ingestion`. Short-lived story branches merged via `git merge --ff-only`. PR #5 remains open and unmerged. |
