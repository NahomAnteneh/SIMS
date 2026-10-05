-- +goose Up
-- S2.10: Field ownership - first_seen_at owned by persistence layer
ALTER TABLE virtual_machines
    ADD COLUMN IF NOT EXISTS first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Backfill first_seen_at from created_at for existing rows
UPDATE virtual_machines
SET first_seen_at = created_at
WHERE first_seen_at IS NULL;

-- +goose Down
ALTER TABLE virtual_machines
    DROP COLUMN IF EXISTS first_seen_at;
