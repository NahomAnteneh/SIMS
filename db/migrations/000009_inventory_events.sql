-- +goose Up
-- S2.8 / S2.9: Inventory events v1 expansion and collector event state watermarking

ALTER TABLE inventory_events
    ADD COLUMN IF NOT EXISTS user_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS entity_type TEXT NOT NULL DEFAULT 'VirtualMachine',
    ADD COLUMN IF NOT EXISTS entity_moref TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS entity_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS payload JSONB NOT NULL DEFAULT '{}'::jsonb;

-- Backfill entity_moref from vm_moref if vm_moref exists
UPDATE inventory_events
SET entity_moref = vm_moref
WHERE entity_moref = '' AND vm_moref IS NOT NULL;

-- Query Pattern: lookup events by entity MoRef within a vCenter (e.g. for VM timeline or audit)
CREATE INDEX IF NOT EXISTS idx_inventory_events_entity
ON inventory_events (vcenter_id, entity_moref, created_time DESC);

-- S2.9: Watermark tracking for EventHistoryCollector restart/replay
ALTER TABLE collector_state
    ALTER COLUMN target_id DROP NOT NULL,
    ADD COLUMN IF NOT EXISTS vcenter_id UUID REFERENCES vcenters(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS last_event_key BIGINT,
    ADD COLUMN IF NOT EXISTS last_event_time TIMESTAMPTZ;

CREATE UNIQUE INDEX IF NOT EXISTS uq_collector_state_vcenter_id
ON collector_state (vcenter_id) WHERE vcenter_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS uq_collector_state_vcenter_id;
ALTER TABLE collector_state
    DROP COLUMN IF EXISTS last_event_time,
    DROP COLUMN IF EXISTS last_event_key,
    DROP COLUMN IF EXISTS vcenter_id,
    ALTER COLUMN target_id SET NOT NULL;

DROP INDEX IF EXISTS idx_inventory_events_entity;
ALTER TABLE inventory_events
    DROP COLUMN IF EXISTS payload,
    DROP COLUMN IF EXISTS entity_name,
    DROP COLUMN IF EXISTS entity_moref,
    DROP COLUMN IF EXISTS entity_type,
    DROP COLUMN IF EXISTS user_name;
