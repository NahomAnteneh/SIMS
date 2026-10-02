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
