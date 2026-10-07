# SIMS On-Call Engineering Guide (v1.0.0)

## 1. Service Level Objectives (SLOs) & Alert Thresholds

| Objective | Target SLO | Warning (Sev-2) | Paging Breach (Sev-1) |
|:---|:---|:---|:---|
| **Query Hot-Path Latency (DB)** | P99 $\le$ 5 ms | P99 > 8 ms for 5m | P99 > 15 ms for 5m |
| **API End-to-End Latency** | P99 $\le$ 20 ms | P99 > 30 ms for 5m | P99 > 50 ms for 5m |
| **Ingestion Pipeline Delay** | P95 $\le$ 250 ms | P95 > 500 ms for 5m | P95 > 2,000 ms for 5m |
| **Batch Write Latency** | P95 $\le$ 75 ms | P95 > 100 ms for 5m | P95 > 250 ms for 5m |
| **Data Integrity** | 0 duplicate identities | N/A | Any duplicate active VM |
| **Lease Availability** | 100% active targets | Target unassigned > 60s | Target unassigned > 3m |

---

## 2. Paging Alert Matrix & Immediate Triage

### 2.1 `SIMSQueryLatencySLOBreach` (Sev-1)
- **Condition:** DB Query P99 exceeds 15ms or API P99 exceeds 50ms.
- **Triage Steps:**
  1. Check PostgreSQL CPU and Disk I/O utilization in Grafana dashboard.
  2. Inspect running queries for lock contention:
     ```sql
     SELECT pid, now() - query_start AS duration, query, state
     FROM pg_stat_activity
     WHERE state != 'idle' ORDER BY duration DESC;
     ```
  3. Terminate runaway blocking queries:
     ```sql
     SELECT pg_cancel_backend(<pid>);
     ```

### 2.2 `SIMSIngestionLagSLOBreach` (Sev-1)
- **Condition:** Change-to-durable database write latency exceeds 2 seconds.
- **Triage Steps:**
  1. Check `sims_collector_queue_depth` per vCenter target.
  2. Check PostgreSQL write I/O wait times and WAL write throughput.
  3. Check network latency between collector and vCenter (`sims_collector_wait_for_updates_duration_seconds`).
  4. Ensure collector connection pool has free connections (`sims_collector_pool_conns{state="idle"}`).

### 2.3 `SIMSTargetUnassigned` (Sev-2)
- **Condition:** Target vCenter has no active lease holder for > 60 seconds.
- **Triage Steps:**
  1. Check collector pod status (`kubectl get pods -l app=sims-collector`).
  2. If all workers are busy or down, check pod restarts and OOM kills.
  3. Query `collector_targets`:
     ```sql
     SELECT target_id, vcenter_id, assigned_worker_id, state, lease_until
     FROM collector_targets WHERE state != 'assigned';
     ```
  4. Scale collector deployment if necessary (`kubectl scale deployment sims-collector --replicas=4`).

---

## 3. Emergency Procedures

### 3.1 Force Lease Release
If a dead worker retains a database lock or lease record due to abnormal termination:
```sql
UPDATE collector_targets
SET assigned_worker_id = NULL,
    state = 'available',
    lease_until = NOW() - INTERVAL '1 second'
WHERE assigned_worker_id = '<stuck-worker-id>';
```

### 3.2 Safe Rollback Procedure
If a deployment triggers critical regressions:
1. Roll back deployment:
   ```bash
   kubectl rollout undo deployment/sims-query-api
   kubectl rollout undo deployment/sims-collector
   ```
2. Migrations are designed to be forward-compatible and support expand/contract patterns. Only roll back schema migrations if explicitly verified with `make test-migration`.
