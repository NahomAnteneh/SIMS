-- name: UpsertCollectorState :one
INSERT INTO collector_state (target_id, status, last_sync_at, sync_token, error_message, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW())
ON CONFLICT (target_id) DO UPDATE
SET status = EXCLUDED.status,
    last_sync_at = COALESCE(EXCLUDED.last_sync_at, collector_state.last_sync_at),
    sync_token = COALESCE(EXCLUDED.sync_token, collector_state.sync_token),
    error_message = EXCLUDED.error_message,
    updated_at = NOW()
RETURNING *;

-- name: UpdateCollectorStateStatus :one
UPDATE collector_state
SET status = $2,
    error_message = COALESCE($3, error_message),
    updated_at = NOW()
WHERE target_id = $1
RETURNING *;

-- name: GetCollectorStateByTargetID :one
SELECT * FROM collector_state WHERE target_id = $1;

-- name: GetCollectorTargetByID :one
SELECT * FROM collector_targets WHERE id = $1;

-- name: GetCollectorTargetByVCenterID :one
SELECT * FROM collector_targets WHERE vcenter_id = $1;

-- name: ListActiveCollectorTargets :many
SELECT * FROM collector_targets WHERE enabled = true ORDER BY priority DESC;

-- name: GetVCenterCollectorDetails :one
SELECT 
    v.*,
    t.id AS target_id,
    t.enabled AS target_enabled,
    t.priority AS target_priority,
    l.holder_id,
    l.lease_until,
    l.version AS lease_version,
    s.status AS collector_status,
    s.last_sync_at,
    s.sync_token,
    s.error_message
FROM vcenters v
LEFT JOIN collector_targets t ON t.vcenter_id = v.id
LEFT JOIN collector_leases l ON l.target_id = t.id AND l.lease_until > NOW()
LEFT JOIN collector_state s ON s.target_id = t.id
WHERE v.id = $1 AND v.deleted_at IS NULL;

-- name: ListActiveVCentersWithCollectorDetails :many
SELECT 
    v.*,
    t.id AS target_id,
    t.enabled AS target_enabled,
    t.priority AS target_priority,
    l.holder_id,
    l.lease_until,
    l.version AS lease_version,
    s.status AS collector_status,
    s.last_sync_at,
    s.sync_token,
    s.error_message
FROM vcenters v
LEFT JOIN collector_targets t ON t.vcenter_id = v.id
LEFT JOIN collector_leases l ON l.target_id = t.id AND l.lease_until > NOW()
LEFT JOIN collector_state s ON s.target_id = t.id
WHERE v.deleted_at IS NULL
ORDER BY v.name ASC;
