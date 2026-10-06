-- name: InsertAuditEvent :one
INSERT INTO audit_events (
    actor,
    action,
    target_id,
    outcome,
    request_id,
    details
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: ListAuditEvents :many
SELECT * FROM audit_events
ORDER BY created_at DESC
LIMIT $1;
