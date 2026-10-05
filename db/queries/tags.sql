-- S2.14: Tag synchronization queries

-- name: GetTagsState :one
SELECT tags_last_synced_at, tags_state
FROM collector_state
WHERE vcenter_id = $1;

-- name: UpsertTagsState :exec
INSERT INTO collector_state (vcenter_id, tags_state, tags_last_synced_at, updated_at)
VALUES ($1, $2, $3, NOW())
ON CONFLICT (vcenter_id) WHERE vcenter_id IS NOT NULL DO UPDATE
SET tags_state = EXCLUDED.tags_state,
    tags_last_synced_at = EXCLUDED.tags_last_synced_at,
    updated_at = NOW();

-- name: ListAllVMTags :many
SELECT vm.moref, vt.tag_id, vt.category_name, vt.tag_name
FROM vm_tags vt
JOIN virtual_machines vm ON vt.vm_id = vm.id
WHERE vm.vcenter_id = $1 AND vm.deleted_at IS NULL;

-- name: ListActiveVMMorefs :many
SELECT moref FROM virtual_machines
WHERE vcenter_id = $1 AND deleted_at IS NULL
ORDER BY moref ASC;
