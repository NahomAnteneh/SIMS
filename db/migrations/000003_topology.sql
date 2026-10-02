-- +goose Up
-- S1.1 & S1.3: Topology entities with stable internal IDs and normalized hierarchy

CREATE TABLE datacenters (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vcenter_id UUID NOT NULL REFERENCES vcenters(id) ON DELETE CASCADE,
    moref TEXT NOT NULL,
    name TEXT NOT NULL,
    parent_moref TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_datacenters_vcenter_moref UNIQUE (vcenter_id, moref)
);

CREATE TABLE inventory_folders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vcenter_id UUID NOT NULL REFERENCES vcenters(id) ON DELETE CASCADE,
    moref TEXT NOT NULL,
    name TEXT NOT NULL,
    folder_type TEXT NOT NULL DEFAULT 'vm',
    parent_id UUID REFERENCES inventory_folders(id) ON DELETE SET NULL,
    path ltree NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_inventory_folders_vcenter_moref UNIQUE (vcenter_id, moref)
);

-- Query Pattern: folder subtree hierarchy queries (folder_path <@ 'Datacenter.vm.Folder')
CREATE INDEX idx_inventory_folders_path_gist ON inventory_folders USING gist (path);

CREATE TABLE clusters (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vcenter_id UUID NOT NULL REFERENCES vcenters(id) ON DELETE CASCADE,
    datacenter_id UUID REFERENCES datacenters(id) ON DELETE SET NULL,
    moref TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_clusters_vcenter_moref UNIQUE (vcenter_id, moref)
);

CREATE TABLE hosts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vcenter_id UUID NOT NULL REFERENCES vcenters(id) ON DELETE CASCADE,
    cluster_id UUID REFERENCES clusters(id) ON DELETE SET NULL,
    datacenter_id UUID REFERENCES datacenters(id) ON DELETE SET NULL,
    moref TEXT NOT NULL,
    name TEXT NOT NULL,
    connection_state TEXT NOT NULL DEFAULT 'connected',
    power_state TEXT NOT NULL DEFAULT 'poweredOn',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_hosts_vcenter_moref UNIQUE (vcenter_id, moref)
);

-- Query Pattern: lookup hosts by cluster
CREATE INDEX idx_hosts_cluster_id ON hosts (cluster_id);

CREATE TABLE datastores (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vcenter_id UUID NOT NULL REFERENCES vcenters(id) ON DELETE CASCADE,
    datacenter_id UUID REFERENCES datacenters(id) ON DELETE SET NULL,
    moref TEXT NOT NULL,
    name TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT '',
    capacity_bytes BIGINT NOT NULL DEFAULT 0,
    free_bytes BIGINT NOT NULL DEFAULT 0,
    accessible BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_datastores_vcenter_moref UNIQUE (vcenter_id, moref)
);

CREATE TABLE distributed_switches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vcenter_id UUID NOT NULL REFERENCES vcenters(id) ON DELETE CASCADE,
    datacenter_id UUID REFERENCES datacenters(id) ON DELETE SET NULL,
    moref TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_distributed_switches_vcenter_moref UNIQUE (vcenter_id, moref)
);

CREATE TABLE portgroups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vcenter_id UUID NOT NULL REFERENCES vcenters(id) ON DELETE CASCADE,
    switch_id UUID REFERENCES distributed_switches(id) ON DELETE SET NULL,
    moref TEXT NOT NULL,
    name TEXT NOT NULL,
    vlan_ids int4multirange,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_portgroups_vcenter_moref UNIQUE (vcenter_id, moref)
);

-- Query Pattern: VLAN range overlap or containment search on portgroups
CREATE INDEX idx_portgroups_vlan_ids_gist ON portgroups USING gist (vlan_ids);

CREATE TABLE networks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vcenter_id UUID NOT NULL REFERENCES vcenters(id) ON DELETE CASCADE,
    datacenter_id UUID REFERENCES datacenters(id) ON DELETE SET NULL,
    portgroup_id UUID REFERENCES portgroups(id) ON DELETE SET NULL,
    moref TEXT NOT NULL,
    name TEXT NOT NULL,
    vlan_ids int4multirange,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_networks_vcenter_moref UNIQUE (vcenter_id, moref)
);

-- Query Pattern: VLAN range overlap or containment search on networks
CREATE INDEX idx_networks_vlan_ids_gist ON networks USING gist (vlan_ids);

-- Least privilege role grants
GRANT SELECT, INSERT, UPDATE, DELETE ON
    datacenters, inventory_folders, clusters, hosts, datastores,
    distributed_switches, portgroups, networks
TO collector_rw;

GRANT SELECT ON
    datacenters, inventory_folders, clusters, hosts, datastores,
    distributed_switches, portgroups, networks
TO query_ro;

-- +goose Down
DROP TABLE IF EXISTS networks;
DROP TABLE IF EXISTS portgroups;
DROP TABLE IF EXISTS distributed_switches;
DROP TABLE IF EXISTS datastores;
DROP TABLE IF EXISTS hosts;
DROP TABLE IF EXISTS clusters;
DROP TABLE IF EXISTS inventory_folders;
DROP TABLE IF EXISTS datacenters;
