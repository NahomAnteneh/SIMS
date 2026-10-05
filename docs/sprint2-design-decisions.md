# Sprint 2 Architectural Design Decisions

This document formally records the design decisions and architectural trade-offs for Sprint 2 (Real-Time Ingestion, Event Ledger, Recovery, and Backpressure), resolving design questions a–e with empirical constraints and explicit assumption boundaries.

---

## Decision a: Topology Change Synchronization & Standalone Hosts

### Problem
How do topology changes (new or renamed hosts, datastores, networks, portgroups, folders, clusters) reach the database and the in-memory topology snapshot? How are standalone hosts handled, and how do topology parent updates avoid full VM row rewrites?

### Architectural Decisions
1. **Hybrid Streaming + Scheduled Reconciliation:**
   - **Stream Topology in Primary Filter:** Include `HostSystem`, `ComputeResource`, `ClusterComputeResource`, `Datastore`, `Network`, `DistributedVirtualPortgroup`, and `Folder` in the `PropertyFilterSpec` watching `name` and `parent`.
   - **Standalone Hosts (`ComputeResource`):** In vSphere, standalone hosts belong to a `ComputeResource` (e.g. `cr-...` or `domain-s...`), whereas clustered hosts belong to `ClusterComputeResource` (`domain-c...`). Both managed object types are monitored. For standalone hosts, `hosts.cluster_id` is set to `NULL` (nullable FK), and the parent chain is traversed through `ComputeResource` to resolve the datacenter.
   - **Scheduled Sweep Safety Net:** Run a full background topology hydration on a configurable interval (default: 15 minutes, `TOPOLOGY_REFRESH_INTERVAL=15m`) and swap the immutable snapshot via `TopologyCache.Swap()`.

2. **Re-Parenting Without VM Rewrites:**
   Due to the normalized relational schema:
   - `virtual_machines` stores foreign keys: `host_id`, `cluster_id`, `folder_id`, `datacenter_id`.
   - When a folder is renamed or moved in the tree, only `inventory_folders` rows are updated. The folder path is updated via PostgreSQL's native `ltree` prefix substitution (`UPDATE inventory_folders SET path = $new_prefix || subpath(path, $old_prefix_len) WHERE path <@ $old_prefix`). Because `virtual_machines.folder_id` references `inventory_folders.id`, VM records do not need individual column updates.
   - When a standalone host joins a cluster, only the host record is updated (`UPDATE hosts SET cluster_id = $cluster_id WHERE id = $host_id`). VMs running on that host remain attached to `host_id`.

---

## Decision b: Field-Mask Change Model for Incremental Updates

### Problem
Sprint 1 bulk persistence (`BulkPersister`) operates on full `domain.VMRecord` entities, rewriting core columns and wiping/re-inserting all child tables (`vm_datastores`, `vm_network_interfaces`, `vm_tags`, `vm_custom_attributes`). In streaming mode, `Modify` updates contain only changed property paths (e.g. `runtime.powerState`). A naive full-record upsert would overwrite untouched fields with zero values and destroy child associations.

### Solution & Design
1. **Domain Change Model:**
   Introduce `domain.VMChange` with an explicit `FieldMask` bitmask:
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

---

## Decision c: Verified v1 Event Set (S2.8)

### Problem
Define the verified v1 event set for the `EventHistoryCollector` ledger, justified strictly by the fields the event layer owns (`created_at`, `created_by`) or explicit behavioral triggers.

### Event Type Justifications

| Event Type | Owned Field / Behavioral Action | Upstream Justification |
| :--- | :--- | :--- |
| **`VmCreatedEvent`** | `created_at`, `created_by` | VM provisioned from scratch. Populates initial creation timestamp and user. |
| **`VmClonedEvent`** | `created_at`, `created_by` | VM provisioned via cloning. Populates creation timestamp and user. |
| **`VmDeployedEvent`** | `created_at`, `created_by` | VM deployed from template or OVF/OVA. Populates creation timestamp and user. |
| **`VmRegisteredEvent`** | `created_at`, `created_by` | Pre-existing VM registered from datastore path. Populates creation timestamp and user. |
| **`VmDiscoveredEvent`** | `created_at` | VM discovered on an ESXi host added to vCenter. Sets initial creation timestamp. |
| **`VmRemovedEvent`** | Reconciler / Tombstone Trigger | VM deleted or unregistered. Provides explicit event-driven trigger for soft-delete (`deleted_at = NOW()`) if the VM was removed between PropertyCollector poll intervals. |
| **`VmMigratedEvent`** | xvMotion Correlation Trigger | Cross-vCenter vMotion (xvMotion) destination trigger. Connects VM relocation across independent vCenters using `instance_uuid` (ADR-011). |
| **`VmRelocatedEvent`** | Storage/Host Relocation Trigger | VM relocated across datastores or standalone hosts. Triggers targeted re-indexing of datastore associations. |

Verified against upstream `github.com/vmware/govmomi/vim25/types`. Detailed contract documented in [`docs/events-v1.md`](events-v1.md).

---

## Decision d: Event Overlap Window (S2.9)

### Problem
Determine the default lookback overlap window for event retrieval after collector restart, preventing event loss across vCenter cluster synchronization delays.

### Decision & Assumptions
- **Default Overlap Window:** **5 minutes** (`300 seconds`), configurable via `EVENT_OVERLAP_WINDOW=5m`.
- **Deduplication:** Events retrieved within `[last_event_time - 5m, now]` are inserted using `ON CONFLICT (vcenter_id, event_key) DO NOTHING`.
- **Assumption & Measurement Plan:**
  - *Assumption:* In typical production vCenters, a 5-minute replay window yields < 500 duplicate events, requiring < 50 ms to ingest and deduplicate.
  - *Measurement Plan:* The collector instruments `sims_collector_event_replay_count` and `sims_collector_event_replay_duration_seconds`. Real-world volume and latency will be empirically measured during Sprint 2 test gates and 1-hour churn testing on both vcsim and lab vCenter.

---

## Decision e: Bounded Queue Backpressure & vSphere Stream Trade-offs (S2.12 / S2.13)

### Problem
What happens to the vSphere stream when the bounded queue fills up due to a database outage or slow consumer?

### Invariants & Failure Mode Decoupling
1. **Producer Blocking Invariant:**
   When `BoundedQueue` reaches capacity, the producer thread blocks on a Go channel write. As mandated by S2.12, the collector **never** drops oldest, drops newest, or silently overwrites events.
2. **Decoupled Heartbeat:**
   While the streaming loop is blocked, the **`SessionGuardian` continues heartbeating via `CurrentTime` on its own independent goroutine**. This ensures the vCenter session remains active and is not prematurely declared dead.
3. **Proactive Stream Unblock (`STREAM_BLOCK_MAX_DURATION`):**
   - *Unverified Assumption:* It is widely assumed that vCenter Server buffers delta changes in memory for 15–30 minutes before raising `InvalidCollectorVersion`. However, Broadcom does not publish guaranteed retention SLAs for inactive PropertyCollector sessions.
   - *Correctness Guard:* Correctness must NOT depend on unverified vCenter buffer limits.
   - *Design:* Introduce a configurable `STREAM_BLOCK_MAX_DURATION` (default: **5 minutes / 300 seconds**). If the bounded queue remains full and blocks the producer continuously for longer than `STREAM_BLOCK_MAX_DURATION`, the worker proactively cancels the stalled `WaitForUpdatesEx` call, transitions to `RESYNCING`, and executes a full reconciliation once database backpressure clears.
4. **InvalidCollectorVersion Handling:**
   If vCenter terminates the collector version token earlier than expected, vCenter returns a typed SOAP fault (`ServerFaultCode: InvalidCollectorVersion`). The worker catches this fault, transitions to `RESYNCING`, destroys the stale filter and ContainerView, runs a full baseline rehydration, and establishes a fresh stream (S2.4).
