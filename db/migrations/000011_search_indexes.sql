-- +goose Up
-- S3.5: Search indexes for trigram, ltree hierarchy, IP network, multirange and keyset pagination

-- GIN trigram index on guest_host_name for substring search
CREATE INDEX IF NOT EXISTS idx_vms_guest_host_name_trgm ON virtual_machines USING gin (guest_host_name gin_trgm_ops);

-- GiST index on virtual_machines.guest_ip_address using inet_ops for CIDR containment and overlap
CREATE INDEX IF NOT EXISTS idx_vms_guest_ip_address_gist ON virtual_machines USING gist (guest_ip_address inet_ops);

-- GiST index on vm_network_interfaces.vlan_ids for VLAN multirange overlap queries
CREATE INDEX IF NOT EXISTS idx_vm_nic_vlan_ids_gist ON vm_network_interfaces USING gist (vlan_ids);

-- Keyset pagination indexes on (updated_at, id)
CREATE INDEX IF NOT EXISTS idx_vms_updated_at_id_active ON virtual_machines (updated_at ASC, id ASC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_vms_vcenter_updated_at_id_active ON virtual_machines (vcenter_id, updated_at ASC, id ASC) WHERE deleted_at IS NULL;

-- Keyset pagination indexes on (first_seen_at, id)
CREATE INDEX IF NOT EXISTS idx_vms_first_seen_at_id_active ON virtual_machines (first_seen_at ASC, id ASC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_vms_vcenter_first_seen_at_id_active ON virtual_machines (vcenter_id, first_seen_at ASC, id ASC) WHERE deleted_at IS NULL;

-- B-tree indexes for hot classification lookup
CREATE INDEX IF NOT EXISTS idx_vms_guest_host_name_btree ON virtual_machines (guest_host_name) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_vms_power_state ON virtual_machines (power_state) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_vms_power_state;
DROP INDEX IF EXISTS idx_vms_guest_host_name_btree;
DROP INDEX IF EXISTS idx_vms_vcenter_first_seen_at_id_active;
DROP INDEX IF EXISTS idx_vms_first_seen_at_id_active;
DROP INDEX IF EXISTS idx_vms_vcenter_updated_at_id_active;
DROP INDEX IF EXISTS idx_vms_updated_at_id_active;
DROP INDEX IF EXISTS idx_vm_nic_vlan_ids_gist;
DROP INDEX IF EXISTS idx_vms_guest_ip_address_gist;
DROP INDEX IF EXISTS idx_vms_guest_host_name_trgm;
