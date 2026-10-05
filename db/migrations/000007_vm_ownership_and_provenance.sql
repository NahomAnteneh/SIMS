-- +goose Up
-- S2.10: Field ownership and event provenance
ALTER TABLE virtual_machines
    ADD COLUMN IF NOT EXISTS created_by TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Index for last_seen_at for reconciler sweep queries
CREATE INDEX IF NOT EXISTS idx_vms_last_seen_at ON virtual_machines (vcenter_id, last_seen_at);

-- +goose Down
DROP INDEX IF EXISTS idx_vms_last_seen_at;
ALTER TABLE virtual_machines
    DROP COLUMN IF EXISTS last_seen_at,
    DROP COLUMN IF EXISTS created_by;
