-- +goose Up
-- S4.1, S4.2, S4.9: Collector target priority/limits, lease optimization, and tombstone purge index

ALTER TABLE collector_targets
    ADD COLUMN IF NOT EXISTS priority INT NOT NULL DEFAULT 100,
    ADD COLUMN IF NOT EXISTS limits JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS concurrency_policy JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE INDEX IF NOT EXISTS idx_collector_targets_enabled ON collector_targets (enabled);

CREATE INDEX IF NOT EXISTS idx_collector_leases_target_holder ON collector_leases (target_id, holder_id);
CREATE INDEX IF NOT EXISTS idx_collector_leases_holder ON collector_leases (holder_id);

CREATE INDEX IF NOT EXISTS idx_collector_state_status ON collector_state (status);

-- Partial index for bounded tombstone purging (S4.9)
CREATE INDEX IF NOT EXISTS idx_vms_deleted_at_purge ON virtual_machines (deleted_at) WHERE deleted_at IS NOT NULL;

-- Least privilege role grants
GRANT SELECT, INSERT, UPDATE, DELETE ON collector_targets, collector_leases, collector_state TO collector_rw;
GRANT SELECT ON collector_targets, collector_leases, collector_state TO query_ro;

-- +goose Down
DROP INDEX IF EXISTS idx_vms_deleted_at_purge;
DROP INDEX IF EXISTS idx_collector_state_status;
DROP INDEX IF EXISTS idx_collector_leases_holder;
DROP INDEX IF EXISTS idx_collector_leases_target_holder;
DROP INDEX IF EXISTS idx_collector_targets_enabled;

ALTER TABLE collector_targets
    DROP COLUMN IF EXISTS concurrency_policy,
    DROP COLUMN IF EXISTS limits,
    DROP COLUMN IF EXISTS priority;
