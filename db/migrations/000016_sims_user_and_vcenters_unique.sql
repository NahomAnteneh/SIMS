-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'sims') THEN
        CREATE ROLE sims WITH LOGIN PASSWORD 'sims_password';
    END IF;
END $$;
-- +goose StatementEnd

GRANT CONNECT ON DATABASE sims TO sims;
GRANT USAGE, CREATE ON SCHEMA public TO sims;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO sims;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO sims;

ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO sims;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO sims;

-- Update users default role and convert existing roles to admin and user
ALTER TABLE users ALTER COLUMN role SET DEFAULT 'user';
UPDATE users SET role = 'admin' WHERE role = 'inventory-admin';
UPDATE users SET role = 'user' WHERE role = 'viewer';

ALTER TABLE vcenters DROP CONSTRAINT IF EXISTS vcenters_api_url_key;
CREATE UNIQUE INDEX IF NOT EXISTS uq_vcenters_api_url_active ON vcenters (api_url) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS uq_vcenters_api_url_active;
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'vcenters_api_url_key'
    ) THEN
        ALTER TABLE vcenters ADD CONSTRAINT vcenters_api_url_key UNIQUE (api_url);
    END IF;
END $$;
-- +goose StatementEnd

UPDATE users SET role = 'inventory-admin' WHERE role = 'admin';
UPDATE users SET role = 'viewer' WHERE role = 'user';
ALTER TABLE users ALTER COLUMN role SET DEFAULT 'viewer';
