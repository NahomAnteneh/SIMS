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

Events are stored in `inventory_events`:

```sql
CREATE TABLE IF NOT EXISTS inventory_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vcenter_id VARCHAR(64) NOT NULL REFERENCES vcenters(id) ON DELETE CASCADE,
    event_key BIGINT NOT NULL,
    chain_id BIGINT,
    event_type VARCHAR(128) NOT NULL,
    created_time TIMESTAMPTZ NOT NULL,
    user_name VARCHAR(256),
    entity_type VARCHAR(64) NOT NULL,
    entity_moref VARCHAR(64) NOT NULL,
    entity_name VARCHAR(256),
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_inventory_events_vcenter_key UNIQUE (vcenter_id, event_key)
);

CREATE INDEX IF NOT EXISTS idx_inventory_events_entity
ON inventory_events (vcenter_id, entity_moref, created_time DESC);

CREATE INDEX IF NOT EXISTS idx_inventory_events_time
ON inventory_events (vcenter_id, created_time DESC);
```

### Deduplication Guarantee
- Storage is strictly idempotent via `ON CONFLICT (vcenter_id, event_key) DO NOTHING`.
- Replaying events from an overlap window never results in duplicate rows or skewed counts.
