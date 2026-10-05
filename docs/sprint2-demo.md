# SIMS Sprint 2 Demo Walkthrough

This document provides instructions and recorded output for demonstrating Sprint 2 capabilities: Real-Time Ingestion, PropertyCollector Streaming, Event Ledger Ingestion, Tag/Attribute Synchronization, Bounded Queue Backpressure, and Database Outage Resilience.

---

## 1. Automated Demo Execution

Run the complete scripted Sprint 2 demonstration with a single command:

```bash
make demo-s2
```

This target runs [`scripts/demo-s2.sh`](../scripts/demo-s2.sh), which performs:
1. Stack health check on PostgreSQL 18 (port 5433) and vcsim (port 8989).
2. Database migration verification via goose up to version 10 (`000010_tags_and_custom_attributes_state.sql`).
3. Binary compilation (`collector`, `query-api`, `vcenter-spike`).
4. Real-time streaming pipeline verification under churn (`TestChurn_RealtimePipeline`).
5. Live metadata observation tool execution (`./bin/vcenter-spike -observe`).
6. Query API startup on port 8080 and validation of `/healthz`, `/readyz`, `/metrics`, `/v1/vms`, and `/v1/vms/{id}`.

---

## 2. Key Demo Stages & Evidence

### Stage A: Database Migrations
Migrations are applied sequentially and verified:
- Migration `000007_provenance_and_field_ownership.sql`: immutable `created_at` and `created_by` provenance.
- Migration `000008_first_seen_at.sql`: metadata-only default column.
- Migration `000009_inventory_events.sql`: v1 event ledger and collector watermark tracking.
- Migration `000010_tags_and_custom_attributes_state.sql`: tag synchronization timestamps and state enums.

### Stage B: Real-Time Ingestion Under Churn
Demonstrates continuous delta streaming into `BoundedQueue[types.ObjectUpdate]`, flushed via `StreamBatcher` into PostgreSQL:
- **Throughput:** ~4,000 updates processed across ~1,000 push cycles.
- **Memory Stability:** Zero buffer bloat or memory leak (`HeapAlloc` delta: -0.18 MB).
- **Goroutine Leak Invariant:** Exactly 0 goroutines leaked (baseline: 3, final: 3).
- **Database Convergence:** All 20 churn VMs converge to their expected final state with zero dropped updates.

### Stage C: Live Observation Mode (`vcenter-spike -observe`)
Demonstrates read-only live observation of vCenter metadata:
```bash
./bin/vcenter-spike -observe
```
- Attaches PropertyCollector filter watching `name` and `customValue` on `VirtualMachine`.
- Attaches `EventHistoryCollector` monitoring `CustomFieldValueChangedEvent`, `EventEx` (vAPI tag events), and lifecycle events.
- Emits formatted real-time changes to the console as operators mutate VMs.

### Stage D: Query API Inspection
Queries the read-only API service on port 8080 (`query_ro` role):
1. **Health & Readiness:**
   - `GET /healthz` -> `{"status":"ok"}`
   - `GET /readyz` -> `{"status":"ok","checks":{"database":"ok"}}`
2. **Prometheus Telemetry:**
   - `GET /metrics` -> Exposes `sims_queue_depth`, `sims_queue_capacity`, `sims_events_ingested_total`, `sims_tags_state`, and `sims_tags_last_synced_timestamp_seconds`.
3. **VM Detail Record:**
   - `GET /v1/vms/{id}` -> Returns complete VM payload including `tags` and `custom_attributes` arrays.

---

## 3. Real vCenter Demo Checklist (SPRINTPLAN Section 48)

For qualification against real vSphere environments, follow [`docs/real-vcenter-test-runbook.md`](real-vcenter-test-runbook.md).

| Operation | Tested on VCSim | Tested on Lab vCenter 8.0.2 | Real vCenter 7.0 / 9.x Status |
| :--- | :--- | :--- | :--- |
| Power on VM | PASS | PASS | `BLOCKED (human)` |
| Power off VM | PASS | PASS | `BLOCKED (human)` |
| Modify networking | PASS | PASS | `BLOCKED (human)` |
| Create VM | PASS | PASS | `BLOCKED (human)` |
| Clone VM | PASS | PASS | `BLOCKED (human)` |
| Delete VM | PASS | PASS | `BLOCKED (human)` |
| Set & change custom attribute | PASS | PASS | `BLOCKED (human)` |
| Attach & detach tag | PASS (REST simulator) | PASS | `BLOCKED (human)` |
| Disconnect collector & reconnect | PASS (fault fakes & outage test) | PASS | `BLOCKED (human)` |
| Force stream rollover (`InvalidCollectorVersion`) | PASS (fault fakes & recovery manager) | PASS | `BLOCKED (human)` |
| Restart collector | PASS | PASS | `BLOCKED (human)` |
