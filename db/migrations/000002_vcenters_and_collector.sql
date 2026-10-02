-- +goose Up
-- S1.1: vCenters and collector worker lease/state schema

CREATE TABLE vcenters (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    api_url TEXT NOT NULL UNIQUE,
    instance_uuid TEXT,
    version TEXT NOT NULL DEFAULT '',
    build TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Query Pattern: lookup vCenter by instance_uuid or api_url during discovery
CREATE INDEX idx_vcenters_instance_uuid ON vcenters (instance_uuid);

CREATE TABLE collector_targets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vcenter_id UUID NOT NULL REFERENCES vcenters(id) ON DELETE CASCADE UNIQUE,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE collector_leases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_id UUID NOT NULL REFERENCES collector_targets(id) ON DELETE CASCADE UNIQUE,
    holder_id TEXT NOT NULL,
    lease_until TIMESTAMPTZ NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Query Pattern: scan for expired leases or lease ownership check
CREATE INDEX idx_collector_leases_until ON collector_leases (lease_until);

CREATE TABLE collector_state (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_id UUID NOT NULL REFERENCES collector_targets(id) ON DELETE CASCADE UNIQUE,
    status TEXT NOT NULL DEFAULT 'INIT',
    last_sync_at TIMESTAMPTZ,
    sync_token TEXT,
    error_message TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Least privilege role grants
GRANT SELECT, INSERT, UPDATE, DELETE ON vcenters, collector_targets, collector_leases, collector_state TO collector_rw;
GRANT SELECT ON vcenters, collector_targets, collector_leases, collector_state TO query_ro;

-- +goose Down
DROP TABLE IF EXISTS collector_state;
DROP TABLE IF EXISTS collector_leases;
DROP TABLE IF EXISTS collector_targets;
DROP TABLE IF EXISTS vcenters;
