# Sprint 4 Discrepancy & Verification Register

**Branch:** `sprint-4/leases-and-reconciliation`
**Created:** 2026-10-06
**Method:** Code and verified tests are the source of truth (AGENTS.md §4). Every phase is verified against live PostgreSQL 18, vcsim, and API contracts.

---

## Sprint 4 Phase Tracking

| Phase | Story / Topic | Key Invariants & Guardrails | Verification Target | Status |
|:---|:---|:---|:---|:---: |
| **Phase A** | S4.1, S4.2, S4.3 Schema & Queries (`000013_leases_and_reconciliation.sql`, `leases.sql`, `collector.sql`) | Atomic lease claim/renewal/release with fencing version; collector state tracking; type-safe sqlc | `make test-migration && make sqlc-verify` | [x] Verified |
| **Phase A** | S4.2 Worker Lease Coordinator (`internal/collector/lease.go`) | Transactional row-locking; randomized heartbeat jitter; standby takeover; graceful release | `TestLeaseCoordinator` | [x] Verified |
| **Phase A** | S4.3 Target Assignment FSM (`internal/collector/target_fsm.go`, `internal/api/vcenters.go`) | FSM states (`AVAILABLE`, `ASSIGNED`, `RUNNING`, `DEGRADED`, `REASSIGNING`); `GET /v1/vcenters/{id}` exposes owner, status, last_sync | `TestTargetAssignmentFSM && TestVCentersAPI` | [x] Verified |
| **Phase B** | S4.4 LISTEN/NOTIFY Integration (`internal/collector/listener.go`) | Dedicated `pgx.Conn` on `sims_vcenter_events`; API broadcasts on create/update/resync; 60s fallback periodic sweep | `TestPostgresListener` | [x] Verified |
| **Phase B** | S4.5 Fleet Manager & Limiter (`internal/collector/fleet.go`, `limiter.go`) | Dynamic target spawn/teardown; global hydration semaphore; per-vCenter API token bucket/semaphore | `TestFleetManager && TestConcurrencyLimiter` | [x] Verified |
| **Phase C** | S4.6 Differential Reconciliation (`internal/collector/reconciler.go`) | Lightweight identifier set math ($S_{db} \setminus S_{vc}$, $S_{vc} \setminus S_{db}$, $S_{db} \cap S_{vc}$); tombstoning; hydration enqueue; touch last seen | `TestDifferentialReconciler` | [x] Verified |
| **Phase C** | S4.7 Reconciliation Triggers (`internal/collector/reconciler_triggers.go`) | 24-hour maintenance sweep; post-recovery automatic trigger; manual `POST /v1/vcenters/{id}/resync` | `TestTriggerManager` | [x] Verified |
| **Phase D** | S4.8 xvMotion Cross-vCenter Migration (`internal/collector/xvmotion.go`) | Correlate by `instance_uuid`; handle `Leave -> Enter` and `Enter -> Leave` without unique constraint clash | `TestXvMotion` | [x] Verified |
| **Phase D** | S4.9 Tombstone Batch Purger (`internal/persistence/tombstone.go`) | Partial index on `deleted_at`; bounded batch deletes (`LIMIT`); rows purged and latency telemetry | `TestTombstonePurger` | [x] Verified |
| **Phase E** | S4.10, S4.11 PostgreSQL HA & Runbooks (`docs/ha-runbook.md`, `docs/backup-restore-runbook.md`) | CloudNativePG / Patroni architecture; failover drill; WAL archival & base backup runbook; RTO/RPO measurement | Document Review & Drill Verification | [x] Verified |
| **Phase E** | S4.12 Tag & Attribute Convergence (`internal/collector/tag_reconciler.go`) | Non-destructive tag reconciliation; drift resolution; never delete tags on fetch failure; alert on high drift | `TestTagReconciler` | [x] Verified |
| **Phase F** | S4.13 Multi-Target Scale Suite (`tests/integration/scale_s4_test.go`) | 3, 5, 10 vCenter simulations; measure hydration concurrency, queue depth, DB writes, memory | `TestScaleS4` | Pending |
| **Phase F** | S4.14 Sprint 4 Test Gate & Demo (`tests/integration/gate_s4_test.go`, `scripts/demo-s4.sh`) | Comprehensive test gates (`make test-gate-s4`) and automated end-to-end demo (`make demo-s4`) | `make test-gate-s4 && make demo-s4` | Pending |

---

## Discrepancies & Resolutions Log

| ID | Topic | Discrepancy Description | Resolution & Evidence | Status |
|:---|:---|:---|:---|:---:|
| - | - | - | - | - |
