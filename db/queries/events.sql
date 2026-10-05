-- S2.8 and S2.9: Inventory events and collector watermark queries

-- name: InsertInventoryEvent :one
INSERT INTO inventory_events (
    vcenter_id, event_key, chain_id, event_type, vm_moref, vm_instance_uuid,
    user_name, entity_type, entity_moref, entity_name, payload, message,
    created_time, created_at
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11, $12,
    $13, NOW()
)
ON CONFLICT (vcenter_id, event_key) DO NOTHING
RETURNING *;

-- name: ListEventsForEntity :many
SELECT * FROM inventory_events
WHERE vcenter_id = $1 AND entity_moref = $2
ORDER BY created_time DESC, event_key DESC
LIMIT $3;

-- name: ListEventsByVCenter :many
SELECT * FROM inventory_events
WHERE vcenter_id = $1
ORDER BY created_time DESC, event_key DESC
LIMIT $2;

-- name: GetLatestEvent :one
SELECT * FROM inventory_events
WHERE vcenter_id = $1
ORDER BY created_time DESC, event_key DESC
LIMIT 1;

-- name: GetEventWatermark :one
SELECT last_event_key, last_event_time
FROM collector_state
WHERE vcenter_id = $1;

-- name: UpsertEventWatermark :exec
INSERT INTO collector_state (vcenter_id, last_event_key, last_event_time, updated_at)
VALUES ($1, $2, $3, NOW())
ON CONFLICT (vcenter_id) WHERE vcenter_id IS NOT NULL DO UPDATE
SET last_event_key = EXCLUDED.last_event_key,
    last_event_time = EXCLUDED.last_event_time,
    updated_at = NOW();

-- name: UpdateVMProvenanceFromEvent :exec
UPDATE virtual_machines
SET created_at = $3,
    created_by = $4,
    updated_at = NOW()
WHERE vcenter_id = $1 AND moref = $2 AND deleted_at IS NULL;

-- name: GetVMCreationEvent :one
SELECT user_name, created_time
FROM inventory_events
WHERE vcenter_id = $1 AND entity_moref = $2 AND event_type IN (
    'VmCreatedEvent', 'VmClonedEvent', 'VmDeployedEvent', 'VmRegisteredEvent', 'VmDiscoveredEvent'
)
ORDER BY created_time ASC
LIMIT 1;
