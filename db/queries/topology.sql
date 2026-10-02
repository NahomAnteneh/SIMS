-- Datacenters
-- name: UpsertDatacenter :one
INSERT INTO datacenters (vcenter_id, moref, name, parent_moref, updated_at)
VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT (vcenter_id, moref) DO UPDATE
SET name = EXCLUDED.name,
    parent_moref = EXCLUDED.parent_moref,
    updated_at = NOW()
RETURNING *;

-- name: ListDatacentersByVCenter :many
SELECT * FROM datacenters WHERE vcenter_id = $1 ORDER BY name ASC;

-- Inventory Folders
-- name: UpsertFolder :one
INSERT INTO inventory_folders (vcenter_id, moref, name, folder_type, parent_id, path, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, NOW())
ON CONFLICT (vcenter_id, moref) DO UPDATE
SET name = EXCLUDED.name,
    folder_type = EXCLUDED.folder_type,
    parent_id = EXCLUDED.parent_id,
    path = EXCLUDED.path,
    updated_at = NOW()
RETURNING *;

-- name: ListFoldersByVCenter :many
SELECT * FROM inventory_folders WHERE vcenter_id = $1 ORDER BY path ASC;

-- Clusters
-- name: UpsertCluster :one
INSERT INTO clusters (vcenter_id, datacenter_id, moref, name, updated_at)
VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT (vcenter_id, moref) DO UPDATE
SET datacenter_id = EXCLUDED.datacenter_id,
    name = EXCLUDED.name,
    updated_at = NOW()
RETURNING *;

-- name: ListClustersByVCenter :many
SELECT * FROM clusters WHERE vcenter_id = $1 ORDER BY name ASC;

-- Hosts
-- name: UpsertHost :one
INSERT INTO hosts (vcenter_id, cluster_id, datacenter_id, moref, name, connection_state, power_state, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
ON CONFLICT (vcenter_id, moref) DO UPDATE
SET cluster_id = EXCLUDED.cluster_id,
    datacenter_id = EXCLUDED.datacenter_id,
    name = EXCLUDED.name,
    connection_state = EXCLUDED.connection_state,
    power_state = EXCLUDED.power_state,
    updated_at = NOW()
RETURNING *;

-- name: ListHostsByVCenter :many
SELECT * FROM hosts WHERE vcenter_id = $1 ORDER BY name ASC;

-- Datastores
-- name: UpsertDatastore :one
INSERT INTO datastores (vcenter_id, datacenter_id, moref, name, type, capacity_bytes, free_bytes, accessible, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
ON CONFLICT (vcenter_id, moref) DO UPDATE
SET datacenter_id = EXCLUDED.datacenter_id,
    name = EXCLUDED.name,
    type = EXCLUDED.type,
    capacity_bytes = EXCLUDED.capacity_bytes,
    free_bytes = EXCLUDED.free_bytes,
    accessible = EXCLUDED.accessible,
    updated_at = NOW()
RETURNING *;

-- name: ListDatastoresByVCenter :many
SELECT * FROM datastores WHERE vcenter_id = $1 ORDER BY name ASC;

-- Distributed Switches
-- name: UpsertDistributedSwitch :one
INSERT INTO distributed_switches (vcenter_id, datacenter_id, moref, name, updated_at)
VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT (vcenter_id, moref) DO UPDATE
SET datacenter_id = EXCLUDED.datacenter_id,
    name = EXCLUDED.name,
    updated_at = NOW()
RETURNING *;

-- name: ListDistributedSwitchesByVCenter :many
SELECT * FROM distributed_switches WHERE vcenter_id = $1 ORDER BY name ASC;

-- Portgroups
-- name: UpsertPortgroup :one
INSERT INTO portgroups (vcenter_id, switch_id, moref, name, vlan_ids, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW())
ON CONFLICT (vcenter_id, moref) DO UPDATE
SET switch_id = EXCLUDED.switch_id,
    name = EXCLUDED.name,
    vlan_ids = EXCLUDED.vlan_ids,
    updated_at = NOW()
RETURNING *;

-- name: ListPortgroupsByVCenter :many
SELECT * FROM portgroups WHERE vcenter_id = $1 ORDER BY name ASC;

-- Networks
-- name: UpsertNetwork :one
INSERT INTO networks (vcenter_id, datacenter_id, portgroup_id, moref, name, vlan_ids, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, NOW())
ON CONFLICT (vcenter_id, moref) DO UPDATE
SET datacenter_id = EXCLUDED.datacenter_id,
    portgroup_id = EXCLUDED.portgroup_id,
    name = EXCLUDED.name,
    vlan_ids = EXCLUDED.vlan_ids,
    updated_at = NOW()
RETURNING *;

-- name: ListNetworksByVCenter :many
SELECT * FROM networks WHERE vcenter_id = $1 ORDER BY name ASC;
