-- +goose Up
-- S2: Storage allocation and utilization metrics on virtual machines
ALTER TABLE virtual_machines
    ADD COLUMN IF NOT EXISTS committed_storage_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS provisioned_storage_bytes BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE virtual_machines
    DROP COLUMN IF EXISTS provisioned_storage_bytes,
    DROP COLUMN IF EXISTS committed_storage_bytes;
