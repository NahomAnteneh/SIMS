# Sprint 2 Architectural Design Decisions

This document formally records the design decisions and architectural trade-offs for Sprint 2 (Real-Time Ingestion, Event Ledger, Recovery, and Backpressure), resolving design questions a–e.

---

## Decision a: Topology Change Synchronization

### Problem
How do topology changes (new or renamed hosts, datastores, networks, portgroups, folders, clusters) reach the database and the in-memory topology snapshot?

### Options Considered
1. **Periodic Topology Refresh Only:** Poll topology on a fixed interval and swap `TopologySnapshot`. Simple, but introduces synchronization lag where newly referenced topology nodes in VM updates fail resolution until the next sweep.
2. **Topology Objects in PropertyCollector Stream Only:** Add all topology managed object types to the streaming PropertyFilterSpec. Real-time, but adds significant complexity: folder moves require cascading recursive updates to descendant `ltree` paths, and cluster/host hierarchy changes produce wide update bursts.
3. **Hybrid Streaming + Scheduled Reconciliation (Selected):**
   - **Stream Topology in Primary Filter:** Include `HostSystem`, `Datastore`, `Network`, `DistributedVirtualPortgroup`, and `Folder` in the `PropertyFilterSpec` watching `name` and `parent`.
   - **Incremental Application:** When a topology update arrives, mutate the relational record in PostgreSQL (`ON CONFLICT (vcenter_id, moref) DO UPDATE`) and update the current in-memory `TopologySnapshot`. For folder hierarchy changes, rebuild affected folder paths.
   - **Scheduled Topology Sweep:** Run a full background topology hydration on a configurable interval (default: 15 minutes, `TOPOLOGY_REFRESH_INTERVAL=15m`) and swap the immutable snapshot via `TopologyCache.Swap()`.

### Justification
Provides immediate convergence for host, datastore, and network renames/additions without waiting for polling, while retaining the scheduled sweep as a safety net against out-of-order reference updates or complex folder re-parenting.

---

## Decision b: Field-Mask Change Model for Incremental Updates

### Problem
Sprint 1 bulk persistence (`BulkPersister`) operates on full `domain.VMRecord` entities, rewriting core columns and wiping/re-inserting all child tables (`vm_datastores`, `vm_network_interfaces`, `vm_tags`, `vm_custom_attributes`). In streaming mode, `Modify` updates contain only changed property paths (e.g. `runtime.powerState`). A naive full-record upsert would overwrite untouched fields with zero values and destroy child associations.

### Solution & Design
1. **Domain Change Model:**
   Introduce `domain.VMChange` with an explicit `FieldMask` (bitmask or typed enum set):
   - `FieldPowerState`, `FieldName`, `FieldConnectionState`, `FieldCPUCores`, `FieldMemoryMB`, `FieldFolder`, `FieldHost`, `FieldDatacenter`, `FieldCluster`, `FieldGuestOS`, `FieldGuestHostName`, `FieldGuestIPAddress`, `FieldAnnotation`, `FieldDatastores`, `FieldNetworkInterfaces`, `FieldCustomAttributes`, `FieldDeletedAt`.
2. **Update Classification:**
   - **`Enter`:** Marks all core fields and present child collections in the mask (full baseline upsert).
   - **`Modify`:** Sets only the specific fields modified in the incoming `PropertyUpdate`.
   - **`Leave`:** Sets only `FieldDeletedAt` (`deleted_at = NOW()`), preserving all child relations and column history for audit.
3. **Staging & Conditional Merge:**
   - Extend the staging table `stage_vms` with `field_mask BIGINT NOT NULL`.
   - The set-based merge query conditionally updates columns:
     ```sql
     UPDATE virtual_machines vm
     SET
         power_state = CASE WHEN (s.field_mask & 1) != 0 THEN s.power_state ELSE vm.power_state END,
         name = CASE WHEN (s.field_mask & 2) != 0 THEN s.name ELSE vm.name END,
         ...
         updated_at = NOW()
     FROM stage_vms s
     WHERE vm.vcenter_id = s.vcenter_id AND vm.moref = s.moref AND vm.deleted_at IS NULL;
     ```
   - Child tables (`vm_datastores`, `vm_network_interfaces`, `vm_custom_attributes`) are deleted and re-inserted **only** for VMs where the corresponding child mask bit is asserted.
   - `vm_tags` is **never** touched by PropertyCollector streaming (strictly owned by the tag synchronizer).

### Justification
Guarantees that a `power_state` or `customValue` update cannot touch NICs, tags, or core attributes, satisfying the non-negotiable isolation invariants of S2.2 and S2.10.

---

## Decision c: Minimal v1 Event Set (S2.8)

### Problem
Define the verified v1 event set for the `EventHistoryCollector` ledger, justified strictly by the fields the event layer owns (`created_at`, `created_by`).

### Selected Event Set
Verified against upstream VMware vSphere Web Services API Reference:
1. `VmCreatedEvent` (VM provisioned from scratch)
2. `VmClonedEvent` (VM cloned from another VM)
3. `VmDeployedEvent` (VM deployed from template/OVF)
4. `VmRegisteredEvent` (Existing VM registered from datastore path)
5. `VmDiscoveredEvent` (VM discovered upon host registration)
6. `VmRemovedEvent` (VM deleted from disk or unregistered)
7. `VmMigratedEvent` (VM migrated across hosts/datastores or xvMotion)
8. `VmRelocatedEvent` (VM compute/storage relocated)

Detailed schema and contracts documented in [`docs/events-v1.md`](events-v1.md).

---

## Decision d: Event Overlap Window (S2.9)

### Problem
Determine the default lookback overlap window for event retrieval after collector restart, preventing event loss across vCenter cluster synchronization delays.

### Decision
- **Default Overlap Window:** **5 minutes** (`300 seconds`), configurable via `EVENT_OVERLAP_WINDOW=5m`.
- **Deduplication:** Events retrieved within `[last_event_time - 5m, now]` are inserted using `ON CONFLICT (vcenter_id, event_key) DO NOTHING`.

### Justification
1. Compensates for clock skew, NTP drift, and transaction commit latency across clustered vCenter appliances.
2. In typical enterprise environments, 5 minutes yields 50–500 events—small enough to fetch and deduplicate in under 50 ms.
3. Configurable via environment to accommodate specialized topologies.

---

## Decision e: Bounded Queue Backpressure & vSphere Stream Trade-offs (S2.12 / S2.13)

### Problem
What happens to the vSphere stream when the bounded queue fills up due to a database outage or slow consumer?

### Trade-off Analysis & Invariant
1. **Producer Blocking Invariant:**
   When `BoundedQueue` reaches capacity, the producer thread blocks on a Go channel write. As mandated by S2.12, the collector **never** drops oldest, drops newest, or silently overwrites events.
2. **Effect on vSphere SOAP Connection:**
   While the producer is blocked, the single worker goroutine stops issuing `WaitForUpdatesEx` calls.
   - vCenter Server holds incoming inventory deltas in its internal per-session change buffer.
   - For short database pauses (10s–60s): the buffer easily holds all changes. Once the DB recovers and the queue drains, the worker resumes `WaitForUpdatesEx`, consuming the deltas seamlessly.
   - For extended database outages (> 15–30 minutes): vCenter Server may evict the idle session or truncate the version history.
   - When the worker finally calls `WaitForUpdatesEx` with the stale version token, vCenter returns SOAP fault `types.InvalidCollectorVersion`.
3. **Resilience & Recovery:**
   This does **not** cause data loss. The worker catches `InvalidCollectorVersion`, transitions to `RESYNCING`, destroys the stale filter and ContainerView, runs a full baseline rehydration, reconciles the inventory, and establishes a fresh stream (S2.4).
