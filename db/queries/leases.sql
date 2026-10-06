-- name: AcquireLease :one
INSERT INTO collector_leases (target_id, holder_id, lease_until, version, updated_at)
VALUES ($1, $2, $3, 1, NOW())
ON CONFLICT (target_id) DO UPDATE
SET holder_id = EXCLUDED.holder_id,
    lease_until = EXCLUDED.lease_until,
    version = collector_leases.version + 1,
    updated_at = NOW()
WHERE collector_leases.lease_until < NOW() OR collector_leases.holder_id = EXCLUDED.holder_id
RETURNING *;

-- name: RenewLease :one
UPDATE collector_leases
SET lease_until = $3,
    updated_at = NOW()
WHERE target_id = $1 AND holder_id = $2 AND version = $4
RETURNING *;

-- name: ReleaseLease :exec
UPDATE collector_leases
SET lease_until = NOW() - INTERVAL '1 second',
    updated_at = NOW()
WHERE target_id = $1 AND holder_id = $2;

-- name: DeleteLease :exec
DELETE FROM collector_leases
WHERE target_id = $1 AND holder_id = $2;

-- name: GetActiveLeaseByTargetID :one
SELECT * FROM collector_leases
WHERE target_id = $1 AND lease_until > NOW();

-- name: GetLeaseByTargetID :one
SELECT * FROM collector_leases
WHERE target_id = $1;

-- name: ListLeasesByHolder :many
SELECT * FROM collector_leases
WHERE holder_id = $1 AND lease_until > NOW();

-- name: ListExpiredLeases :many
SELECT l.*, t.vcenter_id
FROM collector_leases l
JOIN collector_targets t ON l.target_id = t.id
WHERE l.lease_until < NOW() AND t.enabled = true;

-- name: ListAvailableTargets :many
SELECT t.id AS target_id, t.vcenter_id, t.enabled, t.priority,
       l.id AS lease_id, l.holder_id, l.lease_until, l.version AS lease_version
FROM collector_targets t
LEFT JOIN collector_leases l ON t.id = l.target_id
WHERE t.enabled = true
  AND (l.id IS NULL OR l.lease_until < NOW())
ORDER BY t.priority DESC, t.created_at ASC;
