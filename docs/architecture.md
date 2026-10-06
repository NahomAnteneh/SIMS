# SIMS Architecture Guide (v1.0.0)

## 1. System Overview

**SIMS (Scalable Inventory & Management Service)** is a high-throughput, distributed vSphere inventory, real-time ingestion, and search platform designed to track and query fleets ranging from small clusters to 1,000,000+ virtual machines across dozens of vCenter Server instances.

```
+--------------------+        +--------------------+
|  vCenter Server A  |        |  vCenter Server B  |
|  (vSphere 7.0/8.0) |        |  (vSphere 7.0/8.0) |
+---------+----------+        +---------+----------+
          | SOAP / REST                 | SOAP / REST
          v                             v
+--------------------------------------------------+
|             Collector Fleet Workers              |
|   - Distributed PostgreSQL Lease Coordinator     |
|   - Single-Goroutine PropertyCollector Streamer  |
|   - EventHistoryCollector Streamer               |
|   - Differential Reconciliation Engine           |
|   - Bulk Ingestion Engine (Binary COPY + Merge)  |
+-------------------------+------------------------+
                          |
                          v
+--------------------------------------------------+
|            PostgreSQL 18 Database                |
|   - Core VM & Topology Ledger                    |
|   - Keyset Pagination & Trigram Search (GIN)     |
|   - Multirange & CIDR Spatial Indexing           |
|   - Isolated Connection Pool Budgets             |
+-------------------------+------------------------+
                          |
                          v
+--------------------------------------------------+
|                 Query API Service                |
|   - OpenAPI 3.1 Strict REST Endpoints            |
|   - AST Query Compiler & Sanitizer               |
|   - OTel Distributed Tracing (W3C Propagated)    |
|   - Read-Only Least Privilege (`query_ro`)       |
+--------------------------------------------------+
```

---

## 2. Ingestion Pipeline & Collector Architecture

### 2.1 Single Goroutine PropertyCollector Streaming
Per VMware vSphere architectural invariants:
- Exactly one background goroutine owns the PropertyCollector `WaitForUpdatesEx` stream per target vCenter.
- Concurrent calls to `WaitForUpdatesEx` against the same session cause race conditions and corrupted filters.
- All streaming operations enforce proactive timeout bounds via `CallDeadline` (default: 90s) to abort hung calls during network partitions without freezing the worker.

### 2.2 Bulk Ingestion & Upsert Merge
To meet high-throughput ingestion targets:
1. **Compaction:** Incoming updates are merged and de-duplicated in memory.
2. **Binary COPY:** Clean staging records are written directly into PostgreSQL via binary `COPY FROM`.
3. **Set-Based Merge:** Staging data is atomically merged into core tables (`virtual_machines`, `clusters`, `datastores`, `hosts`, etc.) via set-based `INSERT ... ON CONFLICT DO UPDATE`.
4. **Field Masking & Non-Destructive Semantics (AGENTS.md §5):**
   - In update payloads, `nil` slices indicate absence from the changeSet and preserve existing child records in the database.
   - Explicit empty slices (`make([]T, 0)`) denote deliberate clearing of child collections.
   - Primary identity timestamps (`created_at`, `created_by`) are strictly owned by the Event ledger and never modified by PropertyCollector updates or reconciliations.

---

## 3. High Availability & Distributed Lease Coordination

- **PostgreSQL Leases:** Target assignments across distributed collector workers are managed via transactionally fenced leases in `collector_targets`.
- **Fencing Tokens:** Every lease renewal increments a monotonic fencing token (`version`). Zombie workers that wake up after lease expiration are strictly fenced out from database commits.
- **Standby Takeover:** If a primary worker crashes or fails to heartbeat within `LeaseTTL` (default: 30s), standby workers automatically acquire the lease and re-establish the PropertyCollector filter.
- **Graceful Termination:** On `SIGTERM`, workers cancel heartbeats, flush pending batches, gracefully release all active leases via `ReleaseAll`, and logout from vCenter.

---

## 4. Database Architecture & Pool Isolation

SIMS enforces strict PostgreSQL least-privilege budgeting across 4 isolated connection pools:
1. **Query API Pool (`query_ro`):** Strictly read-only `SELECT` access to inventory tables. Cannot execute DML or modify collector state.
2. **Collector Ingestion Pool (`collector_rw`):** DML access to inventory core and staging tables, collector state, and lease records.
3. **Reconciliation Pool:** Dedicated, rate-limited connection quota ensuring background sweeps never starve real-time ingestion.
4. **Administration & Migration Pool:** Schema migrations (`goose`) and diagnostic maintenance.

---

## 5. Security & Secret Protection Invariants

- **Write-Only Credentials:** Passwords, API tokens, and vCenter session tokens are strictly write-only.
- **Zero-Leak Logging & Tracing:** All OpenTelemetry span attributes, slog fields, HTTP error responses, and audit trails strip credential and token keys (`password`, `token`, `session_id`, `vmware_session_id`, `secret`).
- **SSRF Defenses:** Pre-registration validation rejects disallowed ports, loopback addresses, and unauthorized hosts via `netutil.SSRFPolicy`.
