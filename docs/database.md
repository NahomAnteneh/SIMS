# Database Architecture and Schema Specification

## 1. Overview

The SIMS platform utilizes PostgreSQL 18 with extensions (`ltree`, `pg_trgm`) as the single operational store for vSphere inventory, topology, network mapping, and event journals.

All migrations are versioned via Goose (`db/migrations/`), and type-safe query interfaces are generated with `sqlc` targeting `pgx/v5`. No ORM is used.

---

## 2. Table Inventory and Core Models

| Table | Purpose | Primary Key | Key Constraints |
| :--- | :--- | :--- | :--- |
| `vcenters` | Managed vCenter Server instances | `id UUID` | `UNIQUE(api_url)` |
| `collector_targets` | Targets enabled for background collection | `id UUID` | `UNIQUE(vcenter_id)` |
| `collector_leases` | Worker leader leases for partition prevention | `id UUID` | `UNIQUE(target_id)` |
| `collector_state` | High-watermarks, tokens, and collector status | `id UUID` | `UNIQUE(target_id)` |
| `datacenters` | vSphere Datacenters | `id UUID` | `UNIQUE(vcenter_id, moref)` |
| `inventory_folders` | Organizational folders with hierarchical `ltree` paths | `id UUID` | `UNIQUE(vcenter_id, moref)` |
| `clusters` | Compute clusters | `id UUID` | `UNIQUE(vcenter_id, moref)` |
| `hosts` | ESXi Host systems | `id UUID` | `UNIQUE(vcenter_id, moref)` |
| `datastores` | Storage datastores (VMFS, NFS, vSAN) | `id UUID` | `UNIQUE(vcenter_id, moref)` |
| `distributed_switches` | Distributed Virtual Switches (DVS) | `id UUID` | `UNIQUE(vcenter_id, moref)` |
| `portgroups` | Distributed & standard port groups | `id UUID` | `UNIQUE(vcenter_id, moref)` |
| `networks` | Virtual networks with VLAN allocations | `id UUID` | `UNIQUE(vcenter_id, moref)` |
| `virtual_machines` | Normalized virtual machine inventory records | `id UUID` | `UNIQUE(instance_uuid) WHERE deleted_at IS NULL`<br>`UNIQUE(vcenter_id, moref) WHERE deleted_at IS NULL` |
| `vm_datastores` | VM storage attachments | `(vm_id, datastore_id)` | Foreign keys cascade on delete |
| `vm_network_interfaces` | Virtual NICs, MACs, IP assignments, VLANs | `id UUID` | FK to `virtual_machines(id)` |
| `inventory_events` | Immutable vCenter event log | `id BIGSERIAL` | `UNIQUE(vcenter_id, event_key)` |

---

## 3. Query Patterns and Index Mapping

Every database index in SIMS directly services a documented query access pattern:

### 3.1 Virtual Machines
- **`uq_vms_instance_uuid_active`** (`UNIQUE (instance_uuid) WHERE deleted_at IS NULL`):
  - *Pattern:* Global VM lookup and identity enforcement across active VMs (`SELECT * FROM virtual_machines WHERE instance_uuid = $1 AND deleted_at IS NULL`). Allows soft-deleted identity reuse.
- **`uq_vms_vcenter_moref_active`** (`UNIQUE (vcenter_id, moref) WHERE deleted_at IS NULL`):
  - *Pattern:* vCenter-local MoRef reconciliation (`SELECT * FROM virtual_machines WHERE vcenter_id = $1 AND moref = $2 AND deleted_at IS NULL`).
- **`idx_vms_name_id_active`** (`(name ASC, id ASC) WHERE deleted_at IS NULL`):
  - *Pattern:* Keyset pagination across all inventory (`WHERE (name, id) > ($cursor_name, $cursor_id) AND deleted_at IS NULL ORDER BY name ASC, id ASC LIMIT $page_size`).
- **`idx_vms_vcenter_name_id_active`** (`(vcenter_id, name ASC, id ASC) WHERE deleted_at IS NULL`):
  - *Pattern:* Keyset pagination scoped to a specific vCenter (`WHERE vcenter_id = $1 AND (name, id) > ($cursor_name, $cursor_id) AND deleted_at IS NULL ORDER BY name ASC, id ASC LIMIT $page_size`).
- **`idx_vms_name_trgm`** (`USING gin (name gin_trgm_ops)`):
  - *Pattern:* Substring and prefix/suffix fuzzy filtering (`WHERE name ILIKE '%' || $query || '%'`).
- **`idx_vms_folder_path_gist`** (`USING gist (folder_path)`):
  - *Pattern:* Hierarchical folder tree filtering (`WHERE folder_path <@ 'Datacenter.vm.Engineering'::ltree`).
- **`idx_vms_host_id`**, **`idx_vms_cluster_id`**, **`idx_vms_datacenter_id`**:
  - *Pattern:* Fast foreign-key seeks when filtering VMs by compute cluster, host, or datacenter.

### 3.2 Network Interfaces and VLANs
- **`idx_vm_nic_mac`** (`mac_address` B-tree):
  - *Pattern:* Fast exact MAC lookup (`WHERE mac_address = $mac`).
- **`idx_vm_nic_ip_gist`** (`USING gist (ip_address inet_ops)`):
  - *Pattern:* Subnet containment and IP range searches (`WHERE ip_address && '10.0.0.0/16'::inet` or `ip_address = $ip`).
- **`idx_vm_nic_network_id`** (`network_id` B-tree):
  - *Pattern:* Listing all interfaces attached to a specific virtual network.
- **`idx_vm_nic_vm_id`** (`vm_id` B-tree):
  - *Pattern:* Retrieving all NICs for a given VM (`WHERE vm_id = $vm_id`).
- **`idx_portgroups_vlan_ids_gist`** and **`idx_networks_vlan_ids_gist`** (`USING gist (vlan_ids)`):
  - *Pattern:* VLAN containment (`vlan_ids @> 100`) and range overlap (`vlan_ids && '{[100, 110]}'::int4multirange`).

### 3.3 Topology and Folders
- **`idx_inventory_folders_path_gist`** (`USING gist (path)`):
  - *Pattern:* Subtree queries (`WHERE path <@ $parent_path`).
- **`idx_vm_datastores_datastore_id`** (`datastore_id` B-tree):
  - *Pattern:* Finding all VMs stored on a datastore.

---

## 4. Least-Privilege Role Model

Security is enforced at the database connection layer:
1. **`migration_owner`**:
   - Holds schema ownership and executes DDL migrations.
2. **`collector_rw`**:
   - Ingestion worker credentials.
   - Granted DML only (`SELECT`, `INSERT`, `UPDATE`, `DELETE`) on all application tables.
   - FORBIDDEN from executing DDL (`CREATE`, `ALTER`, `DROP`).
3. **`query_ro`**:
   - Query API service credentials.
   - Granted `SELECT` only on application tables.
   - FORBIDDEN from executing `INSERT`, `UPDATE`, `DELETE`, or DDL.

Default privileges are configured in schema `public` so all newly migrated tables inherit these role assignments automatically.

---

## 5. Connection Pool and Resource Sizing (`pgxpool`)

For the collector and API services:
- **Query API:**
  - `MaxConns`: 20 connections per API instance.
  - `MinConns`: 5 idle connections.
  - `MaxConnLifetime`: 30 minutes.
  - `MaxConnIdleTime`: 5 minutes.
  - Sizing Rationale: Query API serves fast, indexed read queries (<10ms). 20 connections comfortably handle >1,000 req/sec per node without saturating database connection slots.
- **Collector Worker:**
  - `MaxConns`: 10 connections per collector instance.
  - `MinConns`: 2 idle connections.
  - Sizing Rationale: Collector performs batch operations and bulk COPY streams. Fewer connections prevent connection starvation while maximizing dedicated worker bandwidth.
