-- name: CreateUser :one
INSERT INTO users (
    username,
    password_hash,
    email,
    role,
    allowed_vcenter_ids,
    allowed_folder_paths
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING id, username, email, role, allowed_vcenter_ids, allowed_folder_paths, created_at, updated_at;

-- name: GetUserByUsername :one
SELECT id, username, password_hash, email, role, allowed_vcenter_ids, allowed_folder_paths, created_at, updated_at
FROM users
WHERE username = $1;

-- name: GetUserByID :one
SELECT id, username, email, role, allowed_vcenter_ids, allowed_folder_paths, created_at, updated_at
FROM users
WHERE id = $1;

-- name: ListUsers :many
SELECT id, username, email, role, allowed_vcenter_ids, allowed_folder_paths, created_at, updated_at
FROM users
ORDER BY username ASC;

-- name: DeleteUser :exec
DELETE FROM users
WHERE id = $1;

-- name: UpdateUserPassword :exec
UPDATE users
SET password_hash = $2, updated_at = NOW()
WHERE id = $1;
