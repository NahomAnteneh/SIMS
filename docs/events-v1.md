# v1 Event Set Specification (S2.8 / ADR-010)

This document defines the minimal, verified "v1 Event Set" captured by the `EventHistoryCollector` in SIMS.

---

## 1. Architectural Justification & Field Ownership

Per the platform Field Ownership Rules (S2.10):
- **PropertyCollector** owns current entity state: `power_state`, `hostname`, `host`, `datastores`, `NIC/IP`, `folder`, `custom_attributes`.
- **Event Layer** owns provenance fields: `created_at` and `created_by`.

The v1 event set is kept minimal, focused on events that:
1. Establish identity origin (`created_at` timestamp and `created_by` username).
2. Capture critical lifecycle transitions that can occur between PropertyCollector polling intervals (creation, deletion, clone, migration).

---

## 2. Verified Event Types

The following event types are verified against the upstream VMware vSphere Web Services API Reference and `github.com/vmware/govmomi/vim25/types`:

| Event Type | Category | Owned Fields / Semantic Action | Justification |
| :--- | :--- | :--- | :--- |
| **`VmCreatedEvent`** | Lifecycle | `created_at`, `created_by` | Emitted when a new VM is created from scratch. Sets initial creation timestamp and originating user. |
| **`VmClonedEvent`** | Lifecycle | `created_at`, `created_by` | Emitted when a VM is provisioned via clone. Captures source VM and creating user. |
| **`VmDeployedEvent`** | Lifecycle | `created_at`, `created_by` | Emitted when a VM is deployed from a template or OVF/OVA. Sets creation timestamp and user. |
| **`VmRegisteredEvent`** | Lifecycle | `created_at`, `created_by` | Emitted when an existing VM is registered into inventory from a datastore path (`.vmx`). |
| **`VmDiscoveredEvent`** | Lifecycle | `created_at` | Emitted when an ESXi host with pre-existing VMs is added to vCenter. |
| **`VmRemovedEvent`** | Lifecycle | Reconciler / Tombstone Trigger | Emitted when a VM is deleted from disk or unregistered. Provides explicit event-driven trigger for soft-delete (`deleted_at = NOW()`) if removal happens between PropertyCollector poll intervals. |
| **`VmMigratedEvent`** | Relocation | xvMotion Correlation Trigger | Emitted when a VM is migrated via vMotion across hosts, datastores, or cross-vCenter (xvMotion). Connects VM relocation across independent vCenters using `instance_uuid` (ADR-011). |
| **`VmRelocatedEvent`** | Relocation | Storage/Host Relocation Trigger | Emitted when a VM's storage or compute configuration is relocated across datastores or standalone hosts. Triggers targeted re-indexing of datastore associations. |


---

## 3. Database Schema & Persistence Contract

Events are stored in `inventory_events` as defined in `db/migrations/000004_virtual_machines.sql`:

```sql
CREATE TABLE inventory_events (
    id BIGSERIAL PRIMARY KEY,
    vcenter_id UUID NOT NULL REFERENCES vcenters(id) ON DELETE CASCADE,
    event_key INT NOT NULL,
    chain_id INT NOT NULL DEFAULT 0,
    event_type TEXT NOT NULL,
    vm_moref TEXT,
    vm_instance_uuid TEXT,
    message TEXT NOT NULL DEFAULT '',
    created_time TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_inventory_events_vcenter_key UNIQUE (vcenter_id, event_key)
);

CREATE INDEX idx_events_vcenter_time ON inventory_events (vcenter_id, created_time DESC);
```

> [!NOTE]
> Migration `000009_inventory_events.sql` extends `inventory_events` with expanded metadata columns (`user_name TEXT`, `entity_type TEXT`, `entity_moref TEXT`, `entity_name TEXT`, and `payload JSONB DEFAULT '{}'::jsonb`), adds index `idx_inventory_events_entity (vcenter_id, entity_moref, created_time DESC)`, and adds watermark tracking columns (`last_event_key BIGINT`, `last_event_time TIMESTAMPTZ`) to `collector_state`.

### Real Column Reference
- `id`: `BIGSERIAL PRIMARY KEY` internal sequence ID.
- `vcenter_id`: `UUID NOT NULL REFERENCES vcenters(id)` identifying source vCenter.
- `event_key`: `INT NOT NULL` unique event sequence number assigned by vCenter.
- `chain_id`: `INT NOT NULL DEFAULT 0` task/event correlation chain ID.
- `event_type`: `TEXT NOT NULL` concrete vSphere event type name (e.g. `VmCreatedEvent`).
- `vm_moref`: `TEXT` Managed Object Reference of the affected VM (if available).
- `vm_instance_uuid`: `TEXT` invariant BIOS/SMBIOS instance UUID.
- `message`: `TEXT NOT NULL DEFAULT ''` full formatted event message.
- `created_time`: `TIMESTAMPTZ NOT NULL` server-side timestamp reported by vCenter.
- `created_at`: `TIMESTAMPTZ NOT NULL DEFAULT NOW()` platform ingestion timestamp.
- Expanded in Migration 000009: `user_name` (`TEXT`), `entity_type` (`TEXT`), `entity_moref` (`TEXT`), `entity_name` (`TEXT`), `payload` (`JSONB`).

### Deduplication, Watermarking, and Overlap Guarantee
Per `internal/collector/event_streamer.go` and `internal/persistence/events.go`:
1. **Deduplication:** Storage is strictly idempotent via `ON CONFLICT (vcenter_id, event_key) DO NOTHING`. Replaying events across overlapping windows never duplicates records or mutates existing provenance.
2. **Watermarking:** The streamer persists high watermarks (`last_event_key`, `last_event_time`) in `collector_state` at the end of each processed batch.
3. **Overlap Lookback Window (ADR-010):** On startup, if a prior `last_event_time` exists, the collector queries with `BeginTime = last_event_time - EVENT_OVERLAP_WINDOW` (default: 5 minutes / `300s`).
4. **Retention Gap Detection:** If the oldest event returned in the first batch after reconnect is strictly newer than `last_event_time`, an event retention gap is registered (`sims_collector_event_gap_detected`). If `ReconcileOnGap` is enabled, a full inventory reconciliation is triggered (`sims_collector_event_reconciliation_requested`).
5. **Field Ownership Enforcement:** Creation events (`VmCreatedEvent`, `VmClonedEvent`, `VmDeployedEvent`, `VmRegisteredEvent`) set `virtual_machines.created_at` and `virtual_machines.created_by`. Deletion events (`VmRemovedEvent`) soft-delete the VM by setting `deleted_at = NOW()`.
