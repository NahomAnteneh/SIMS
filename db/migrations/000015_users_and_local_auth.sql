-- +goose Up
-- Users and Local Authentication

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    email TEXT,
    role TEXT NOT NULL DEFAULT 'viewer',
    allowed_vcenter_ids UUID[],
    allowed_folder_paths TEXT[],
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_username ON users (username);

-- Bootstrap default admin user if not present (username: admin, password: admin123)
INSERT INTO users (username, password_hash, email, role)
VALUES ('admin', '$2a$10$yumf2.g1cGQdiOQKODxySOy7B95gd4Z72Q6azRY1BvN9ZIT8j26aK', 'admin@sims.local', 'inventory-admin')
ON CONFLICT (username) DO NOTHING;

-- Grant role permissions
GRANT SELECT, INSERT, UPDATE, DELETE ON users TO collector_rw;
GRANT SELECT ON users TO query_ro;

-- +goose Down
DROP TABLE IF EXISTS users;
