-- +goose Up
-- S5.5: Extended PostgreSQL statistics for correlated hierarchy and state attributes

-- Multi-column correlation across vCenter hierarchy (vcenter_id, cluster_id, host_id)
CREATE STATISTICS IF NOT EXISTS stat_vms_vcenter_cluster_host (dependencies, ndistinct)
    ON vcenter_id, cluster_id, host_id FROM virtual_machines;

-- Multi-column correlation across VM state attributes (power_state, connection_state)
CREATE STATISTICS IF NOT EXISTS stat_vms_state (dependencies, ndistinct)
    ON power_state, connection_state FROM virtual_machines;

-- Analyze to compute statistics immediately
ANALYZE virtual_machines;

-- +goose Down
DROP STATISTICS IF EXISTS stat_vms_state;
DROP STATISTICS IF EXISTS stat_vms_vcenter_cluster_host;
