-- name: GetVMByID :one
SELECT * FROM virtual_machines
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetVMByMoref :one
SELECT * FROM virtual_machines
WHERE vcenter_id = $1 AND moref = $2 AND deleted_at IS NULL;

-- name: GetVMByInstanceUUID :one
SELECT * FROM virtual_machines
WHERE instance_uuid = $1 AND deleted_at IS NULL;

-- S1.13 Keyset pagination queries
-- Keyset pagination without vcenter filter
-- name: ListVMsFirstPage :many
SELECT * FROM virtual_machines
WHERE deleted_at IS NULL
  AND (sqlc.arg(name_filter)::text = '' OR name ILIKE '%' || sqlc.arg(name_filter) || '%')
ORDER BY name ASC, id ASC
LIMIT sqlc.arg(page_limit);

-- name: ListVMsNextPage :many
SELECT * FROM virtual_machines
WHERE deleted_at IS NULL
  AND (sqlc.arg(name_filter)::text = '' OR name ILIKE '%' || sqlc.arg(name_filter) || '%')
  AND (name, id) > (sqlc.arg(cursor_name)::text, sqlc.arg(cursor_id)::uuid)
ORDER BY name ASC, id ASC
LIMIT sqlc.arg(page_limit);

-- Keyset pagination with vcenter filter
-- name: ListVMsByVCenterFirstPage :many
SELECT * FROM virtual_machines
WHERE deleted_at IS NULL
  AND vcenter_id = sqlc.arg(vcenter_id)
  AND (sqlc.arg(name_filter)::text = '' OR name ILIKE '%' || sqlc.arg(name_filter) || '%')
ORDER BY name ASC, id ASC
LIMIT sqlc.arg(page_limit);

-- name: ListVMsByVCenterNextPage :many
SELECT * FROM virtual_machines
WHERE deleted_at IS NULL
  AND vcenter_id = sqlc.arg(vcenter_id)
  AND (sqlc.arg(name_filter)::text = '' OR name ILIKE '%' || sqlc.arg(name_filter) || '%')
  AND (name, id) > (sqlc.arg(cursor_name)::text, sqlc.arg(cursor_id)::uuid)
ORDER BY name ASC, id ASC
LIMIT sqlc.arg(page_limit);

-- name: CountActiveVMs :one
SELECT COUNT(*) FROM virtual_machines WHERE deleted_at IS NULL;

-- name: CountActiveVMsByVCenter :one
SELECT COUNT(*) FROM virtual_machines WHERE vcenter_id = $1 AND deleted_at IS NULL;

-- name: UpsertVM :one
INSERT INTO virtual_machines (
    vcenter_id, moref, instance_uuid, bios_uuid, name, power_state,
    connection_state, cpu_cores, memory_mb, folder_id, folder_path,
    host_id, cluster_id, datacenter_id, guest_os, guest_host_name,
    guest_ip_address, annotation, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11,
    $12, $13, $14, $15, $16,
    $17, $18, NOW()
)
ON CONFLICT (vcenter_id, moref) WHERE deleted_at IS NULL DO UPDATE
SET instance_uuid = EXCLUDED.instance_uuid,
    bios_uuid = EXCLUDED.bios_uuid,
    name = EXCLUDED.name,
    power_state = EXCLUDED.power_state,
    connection_state = EXCLUDED.connection_state,
    cpu_cores = EXCLUDED.cpu_cores,
    memory_mb = EXCLUDED.memory_mb,
    folder_id = EXCLUDED.folder_id,
    folder_path = EXCLUDED.folder_path,
    host_id = EXCLUDED.host_id,
    cluster_id = EXCLUDED.cluster_id,
    datacenter_id = EXCLUDED.datacenter_id,
    guest_os = EXCLUDED.guest_os,
    guest_host_name = EXCLUDED.guest_host_name,
    guest_ip_address = EXCLUDED.guest_ip_address,
    annotation = EXCLUDED.annotation,
    updated_at = NOW()
RETURNING *;

-- name: SoftDeleteVM :exec
UPDATE virtual_machines
SET deleted_at = NOW(), updated_at = NOW()
WHERE vcenter_id = $1 AND moref = $2 AND deleted_at IS NULL;

-- name: AttachVMDatastore :exec
INSERT INTO vm_datastores (vm_id, datastore_id)
VALUES ($1, $2)
ON CONFLICT (vm_id, datastore_id) DO NOTHING;

-- name: ListDatastoresForVM :many
SELECT d.* FROM datastores d
JOIN vm_datastores vd ON d.id = vd.datastore_id
WHERE vd.vm_id = $1
ORDER BY d.name ASC;

-- name: DeleteVMDatastores :exec
DELETE FROM vm_datastores WHERE vm_id = $1;

-- name: InsertVMNetworkInterface :one
INSERT INTO vm_network_interfaces (
    vm_id, device_key, mac_address, ip_address, network_id, portgroup_id, vlan_ids, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, NOW()
)
RETURNING *;

-- name: ListNetworkInterfacesForVM :many
SELECT * FROM vm_network_interfaces
WHERE vm_id = $1
ORDER BY device_key ASC, id ASC;

-- name: DeleteNetworkInterfacesForVM :exec
DELETE FROM vm_network_interfaces WHERE vm_id = $1;

-- name: ListTagsForVM :many
SELECT * FROM vm_tags
WHERE vm_id = $1
ORDER BY category_name ASC, tag_name ASC;

-- name: DeleteTagsForVM :exec
DELETE FROM vm_tags WHERE vm_id = $1;

-- name: ListCustomAttributesForVM :many
SELECT * FROM vm_custom_attributes
WHERE vm_id = $1
ORDER BY name ASC;

-- name: DeleteCustomAttributesForVM :exec
DELETE FROM vm_custom_attributes WHERE vm_id = $1;

-- S4.6: Differential reconciliation queries
-- name: ListActiveVMIdentifiers :many
SELECT id, moref, instance_uuid
FROM virtual_machines
WHERE vcenter_id = $1 AND deleted_at IS NULL;

-- name: TombstoneVMsByIDs :exec
UPDATE virtual_machines
SET deleted_at = NOW(), updated_at = NOW()
WHERE id = ANY($1::uuid[]) AND deleted_at IS NULL;

-- name: TouchVMsLastSeen :exec
UPDATE virtual_machines
SET updated_at = NOW()
WHERE id = ANY($1::uuid[]) AND deleted_at IS NULL;

