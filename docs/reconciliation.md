# SIMS Differential Reconciliation Engine (v1.0.0)

## 1. Purpose & Scope

While PropertyCollector streaming (`WaitForUpdatesEx`) provides real-time change events, network disruptions, vCenter reboots, or dropped events can lead to drift between vCenter inventory and the SIMS database.

The **Differential Reconciliation Engine (S4.6)** periodically performs background sweeps against vCenter inventory to detect:
1. **Stale / Missing VMs:** Virtual machines removed in vCenter while the collector was disconnected.
2. **Cross-vCenter vMotion (xvMotion):** Virtual machines migrated across vCenter boundaries.
3. **Property Drift:** Any discrepancy in power state, IP allocation, network attachment, or hardware configuration.

---

## 2. Invariants & Guardrails

### 2.1 Immutable Provenance Invariant (AGENTS.md §5)
- VM creation provenance (`created_at`, `created_by`) is strictly owned by the vCenter Event history ledger (`EventHistoryCollector`).
- Reconciliation sweeps must **NEVER** overwrite or modify `created_at` or `created_by`.

### 2.2 Isolated Connection Pool Budget
- Reconciliation sweeps run against an isolated connection pool quota (`reconciliation_pool`).
- This prevents heavy periodic sweeps from starving real-time collector writes (`collector_rw`) or user queries (`query_ro`).

### 2.3 Cross-vCenter vMotion (xvMotion) Identity Safety
- Virtual machines are tracked by their immutable BIOS UUID (`instance_uuid`).
- When a VM undergoes xvMotion from vCenter A to vCenter B:
  - The destination vCenter B registers the new VM with its new MoRef and the identical `instance_uuid`.
  - The reconciler marks the previous VM record in vCenter A as soft-deleted (`deleted_at = NOW()`), ensuring 0 duplicate active identities exist in the system at any time.

---

## 3. Reconciliation Algorithm & Phases

```
+-------------------------------------------------------+
| 1. Snapshot Sweep                                     |
|    - Retrieve all active MoRefs & InstanceUUIDs       |
|    - Record sync start timestamp T_sync               |
+---------------------------+---------------------------+
                            |
                            v
+-------------------------------------------------------+
| 2. Differential Compare                               |
|    - Compare live snapshot against DB virtual_machines|
|    - Identify newly discovered and missing entities   |
+---------------------------+---------------------------+
                            |
                            v
+-------------------------------------------------------+
| 3. Set-Based Reconcile & Soft Deletion                |
|    - Update drifted properties                        |
|    - Soft-delete stale VMs (last_seen_at < T_sync)    |
|    - Reconcile tags and custom attributes             |
+-------------------------------------------------------+
```

### Stale VM Tombstoning Query
```sql
UPDATE virtual_machines
SET deleted_at = NOW(),
    updated_at = NOW()
WHERE vcenter_id = $1
  AND deleted_at IS NULL
  AND last_seen_at < $2;
```
