-- +goose Up
-- S1.1, S1.2, S1.4, S1.5: Virtual machines, network interfaces, datastore attachments, and event ledger

CREATE TABLE virtual_machines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vcenter_id UUID NOT NULL REFERENCES vcenters(id) ON DELETE CASCADE,
    moref TEXT NOT NULL,
    instance_uuid TEXT NOT NULL,
    bios_uuid TEXT,
    name TEXT NOT NULL,
    power_state TEXT NOT NULL DEFAULT 'poweredOff',
    connection_state TEXT NOT NULL DEFAULT 'connected',
    cpu_cores INT NOT NULL DEFAULT 0,
    memory_mb BIGINT NOT NULL DEFAULT 0,
    folder_id UUID REFERENCES inventory_folders(id) ON DELETE SET NULL,
    folder_path ltree,
    host_id UUID REFERENCES hosts(id) ON DELETE SET NULL,
    cluster_id UUID REFERENCES clusters(id) ON DELETE SET NULL,
    datacenter_id UUID REFERENCES datacenters(id) ON DELETE SET NULL,
    guest_os TEXT NOT NULL DEFAULT '',
    guest_host_name TEXT NOT NULL DEFAULT '',
    guest_ip_address inet,
    annotation TEXT NOT NULL DEFAULT '',
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- S1.2 Identity Constraints: UNIQUE(instance_uuid) WHERE active; UNIQUE(vcenter_id, moref) WHERE active
-- Query Pattern: ensure active VM instance uniqueness and allow soft-deleted identity reuse
CREATE UNIQUE INDEX uq_vms_instance_uuid_active ON virtual_machines (instance_uuid) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_vms_vcenter_moref_active ON virtual_machines (vcenter_id, moref) WHERE deleted_at IS NULL;

-- S1.13 Keyset pagination indexes (name, id)
-- Query Pattern: Keyset pagination cursor seek: WHERE (name, id) > ($name, $id) AND deleted_at IS NULL ORDER BY name ASC, id ASC LIMIT $limit
CREATE INDEX idx_vms_name_id_active ON virtual_machines (name ASC, id ASC) WHERE deleted_at IS NULL;
-- Query Pattern: vCenter-scoped keyset pagination
CREATE INDEX idx_vms_vcenter_name_id_active ON virtual_machines (vcenter_id, name ASC, id ASC) WHERE deleted_at IS NULL;

-- Trigram fuzzy and substring search index (ADR-014)
-- Query Pattern: Substring match: WHERE name ILIKE '%' || $q || '%'
CREATE INDEX idx_vms_name_trgm ON virtual_machines USING gin (name gin_trgm_ops);

-- Hierarchical folder query index (ADR-012, ADR-014)
-- Query Pattern: Subtree scoping: WHERE folder_path <@ 'Datacenter.vm.Folder'
CREATE INDEX idx_vms_folder_path_gist ON virtual_machines USING gist (folder_path);

-- Foreign key lookup indexes
CREATE INDEX idx_vms_host_id ON virtual_machines (host_id);
CREATE INDEX idx_vms_cluster_id ON virtual_machines (cluster_id);
CREATE INDEX idx_vms_datacenter_id ON virtual_machines (datacenter_id);

CREATE TABLE vm_datastores (
    vm_id UUID NOT NULL REFERENCES virtual_machines(id) ON DELETE CASCADE,
    datastore_id UUID NOT NULL REFERENCES datastores(id) ON DELETE CASCADE,
    PRIMARY KEY (vm_id, datastore_id)
);

-- Query Pattern: lookup all VMs attached to a given datastore
CREATE INDEX idx_vm_datastores_datastore_id ON vm_datastores (datastore_id);

CREATE TABLE vm_network_interfaces (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vm_id UUID NOT NULL REFERENCES virtual_machines(id) ON DELETE CASCADE,
    device_key INT NOT NULL,
    mac_address TEXT NOT NULL,
    ip_address inet,
    network_id UUID REFERENCES networks(id) ON DELETE SET NULL,
    portgroup_id UUID REFERENCES portgroups(id) ON DELETE SET NULL,
    vlan_ids int4multirange,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- S1.4 Network indexes
-- Query Pattern: lookup VM NICs by MAC address
CREATE INDEX idx_vm_nic_mac ON vm_network_interfaces (mac_address);
-- Query Pattern: subnet containment or exact IP search using GiST
CREATE INDEX idx_vm_nic_ip_gist ON vm_network_interfaces USING gist (ip_address inet_ops);
-- Query Pattern: lookup all NICs/VMs connected to a network
CREATE INDEX idx_vm_nic_network_id ON vm_network_interfaces (network_id);
-- Query Pattern: lookup all NICs for a VM
CREATE INDEX idx_vm_nic_vm_id ON vm_network_interfaces (vm_id);

CREATE TABLE inventory_events (
    id BIGSERIAL PRIMARY KEY,
    vcenter_id UUID NOT NULL REFERENCES vcenters(id) ON DELETE CASCADE,
    event_key INT NOT NULL,
    chain_id INT NOT NULL DEFAULT 0,
    event_type TEXT NOT NULL,
    vm_moref TEXT,
    vm_instance_uuid TEXT,
    message TEXT NOT NULL DEFAULT '',
    created_time TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_inventory_events_vcenter_key UNIQUE (vcenter_id, event_key)
);

-- Query Pattern: event history collector replay and timeline queries
CREATE INDEX idx_events_vcenter_time ON inventory_events (vcenter_id, created_time DESC);

-- Least privilege role grants (conditional if roles exist)
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'collector_rw') THEN
        EXECUTE 'GRANT SELECT, INSERT, UPDATE, DELETE ON virtual_machines, vm_datastores, vm_network_interfaces, inventory_events TO collector_rw';
        EXECUTE 'GRANT USAGE, SELECT ON SEQUENCE inventory_events_id_seq TO collector_rw';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'query_ro') THEN
        EXECUTE 'GRANT SELECT ON virtual_machines, vm_datastores, vm_network_interfaces, inventory_events TO query_ro';
        EXECUTE 'GRANT SELECT ON SEQUENCE inventory_events_id_seq TO query_ro';
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS inventory_events;
DROP TABLE IF EXISTS vm_network_interfaces;
DROP TABLE IF EXISTS vm_datastores;
DROP TABLE IF EXISTS virtual_machines;
