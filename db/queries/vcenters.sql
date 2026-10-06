-- name: UpsertVCenter :one
INSERT INTO vcenters (name, api_url, instance_uuid, version, build, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW())
ON CONFLICT (api_url) DO UPDATE
SET name = EXCLUDED.name,
    instance_uuid = EXCLUDED.instance_uuid,
    version = EXCLUDED.version,
    build = EXCLUDED.build,
    updated_at = NOW()
RETURNING *;

-- name: GetVCenterByID :one
SELECT * FROM vcenters WHERE id = $1;

-- name: GetVCenterByURL :one
SELECT * FROM vcenters WHERE api_url = $1;

-- name: ListVCenters :many
SELECT * FROM vcenters ORDER BY name ASC;

-- name: ListActiveVCenters :many
SELECT * FROM vcenters WHERE deleted_at IS NULL ORDER BY name ASC;

-- name: GetActiveVCenterByID :one
SELECT * FROM vcenters WHERE id = $1 AND deleted_at IS NULL;

-- name: GetActiveVCenterByURL :one
SELECT * FROM vcenters WHERE api_url = $1 AND deleted_at IS NULL;

-- name: CreateVCenter :one
INSERT INTO vcenters (
    name,
    api_url,
    instance_uuid,
    version,
    build,
    product_version,
    api_version,
    capabilities,
    tls_settings,
    credential_ref,
    status,
    priority,
    enabled
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
RETURNING *;

-- name: UpdateVCenter :one
UPDATE vcenters
SET name = COALESCE($2, name),
    enabled = COALESCE($3, enabled),
    priority = COALESCE($4, priority),
    credential_ref = COALESCE($5, credential_ref),
    tls_settings = COALESCE($6, tls_settings),
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteVCenter :exec
UPDATE vcenters
SET deleted_at = NOW(),
    enabled = false,
    status = 'DECOMMISSIONED',
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL;

-- name: TombstoneVCenterVMs :exec
UPDATE virtual_machines
SET deleted_at = NOW()
WHERE vcenter_id = $1 AND deleted_at IS NULL;

-- name: InsertCollectorTarget :one
INSERT INTO collector_targets (vcenter_id, enabled)
VALUES ($1, $2)
ON CONFLICT (vcenter_id) DO UPDATE
SET enabled = EXCLUDED.enabled,
    updated_at = NOW()
RETURNING *;

-- name: DisableCollectorTarget :exec
UPDATE collector_targets
SET enabled = false,
    updated_at = NOW()
WHERE vcenter_id = $1;
