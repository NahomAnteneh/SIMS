-- +goose Up
-- S2.14: Tag synchronization state and staleness tracking

ALTER TABLE collector_state
    ADD COLUMN IF NOT EXISTS tags_last_synced_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS tags_state TEXT NOT NULL DEFAULT 'unavailable';

-- +goose Down
ALTER TABLE collector_state
    DROP COLUMN IF EXISTS tags_state,
    DROP COLUMN IF EXISTS tags_last_synced_at;
