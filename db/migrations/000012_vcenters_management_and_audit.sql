-- +goose Up
-- S3.13: vCenter Management API metadata expansion and audit ledger

ALTER TABLE vcenters
    ADD COLUMN IF NOT EXISTS product_version TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS api_version TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS capabilities JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS tls_settings JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS credential_ref TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'PENDING_ASSIGNMENT',
    ADD COLUMN IF NOT EXISTS priority INT NOT NULL DEFAULT 100,
    ADD COLUMN IF NOT EXISTS enabled BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_vcenters_status ON vcenters (status) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS audit_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    target_id TEXT NOT NULL DEFAULT '',
    outcome TEXT NOT NULL,
    request_id TEXT NOT NULL DEFAULT '',
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_events_created_at ON audit_events (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_action ON audit_events (action);
CREATE INDEX IF NOT EXISTS idx_audit_events_actor ON audit_events (actor);

-- Least privilege role grants
GRANT SELECT, INSERT ON audit_events TO collector_rw;
GRANT SELECT ON audit_events TO query_ro;

-- +goose Down
DROP TABLE IF EXISTS audit_events;

DROP INDEX IF EXISTS idx_vcenters_status;

ALTER TABLE vcenters
    DROP COLUMN IF EXISTS deleted_at,
    DROP COLUMN IF EXISTS enabled,
    DROP COLUMN IF EXISTS priority,
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS credential_ref,
    DROP COLUMN IF EXISTS tls_settings,
    DROP COLUMN IF EXISTS capabilities,
    DROP COLUMN IF EXISTS api_version,
    DROP COLUMN IF EXISTS product_version;
