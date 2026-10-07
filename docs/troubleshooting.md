# SIMS Production Troubleshooting & Anomaly Runbook (v1.0.0)

This runbook provides actionable procedures for triaging and resolving operational anomalies in SIMS.

---

## 1. PropertyCollector History Rollover (`InvalidCollectorVersion`)

### Symptom
- Log entry: `PropertyCollector returned InvalidCollectorVersion; triggering re-sync`
- Metric: `sims_collector_resync_total{reason="invalid_collector_version"}` increments.

### Root Cause
vCenter Server maintains a finite circular change buffer in memory. If a network partition, collector pause, or high change rate causes the collector to lag behind the buffer head, vCenter invalidates the PropertyCollector filter version.

### System Behavior & Resolution
1. **Automated Recovery:** The collector automatically transitions from `DELTA_STREAMING` to `RESYNCING`. It destroys the invalidated PropertyFilter, triggers a differential rehydration sweep, reconciles any stale entries, recreates a fresh PropertyFilter, and transitions back to `DELTA_STREAMING`.
2. **Action Required:** None if the alert resolves within 1–2 minutes. If recurring frequently:
   - Check network latency between collector workers and vCenter.
   - Reduce batch size or increase `MaxConcurrentHydrations`.

---

## 2. Network Partitions & Hung SOAP Calls (`stream_hung`)

### Symptom
- Log entry: `PropertyCollector WaitForUpdatesEx hung beyond call deadline; triggering recovery`
- Metric: `sims_collector_resync_total{reason="stream_hung"}` increments.

### Root Cause
Silent TCP connection drop or packet blackhole caused the SOAP request to hang indefinitely without FIN/RST packet.

### System Behavior & Resolution
1. **Automated Recovery:** The proactive timeout guard (`CallDeadline`, default 90s) fires, executes `CancelWaitForUpdates`, terminates the hung socket, and re-establishes the TCP/TLS connection with exponential backoff.
2. **Action Required:** If persistent across targets, verify upstream firewall state-table timeout settings (must be $\ge$ 120s for long-polling HTTP connections).

---

## 3. Worker Crash & Distributed Lease Takeover

### Symptom
- Primary collector worker terminated unexpectedly (e.g. OOM, node reboot).
- Log entry on standby worker: `lease acquired (holder=worker-standby-..., version=...)`.

### System Behavior & Resolution
1. **Automated Recovery:**
   - The primary worker's heartbeat ceases.
   - When `LeaseTTL` (30s) expires, PostgreSQL lease query allows standby workers to atomically acquire the target.
   - The standby worker bumps the fencing token (`version`) and boots the collection pipeline.
   - If the old worker recovers, its subsequent renewals fail with `ErrLeaseLost` and it gracefully steps down.
2. **Action Required:** Verify healthy standby replica count ($N \ge 2$).

---

## 4. Tag REST API Outage (`vapi/rest`)

### Symptom
- Log entry: `tag synchronization failed; preserving existing tags`
- Metric: `sims_collector_tag_sync_errors_total` increments.

### Architectural Invariant (AGENTS.md §5)
- **Non-Destructive Preservation:** An outage or error in the vSphere Automation REST service (`vapi/rest`) must **NEVER** wipe existing tags.
- Tag slices are set to `nil` in the persistence pipeline. The database merge engine preserves existing `vm_tags` records. Only explicit empty slices (`make([]TagRecord, 0)`) clear tags.

---

## 5. Ingestion Latency Spikes & Backpressure

### Symptom
- Metric: `sims_collector_batch_persist_duration_seconds{quantile="0.95"}` exceeds 75ms.
- Metric: `sims_collector_queue_depth` grows continuously.

### Root Cause & Diagnostics
1. **PostgreSQL Write Contention:**
   - Run `SELECT * FROM pg_stat_activity WHERE state != 'idle'` to identify lock conflicts or long-running transactions.
   - Check disk I/O latency and IOPS on the PostgreSQL volume.
2. **Connection Pool Starvation:**
   - Check `sims_collector_pool_conns{state="acquired"}` vs `max_conns`.
   - Ensure the isolated pool budget for `reconciliation` is not starving `collector_rw`.
