-- +goose Up
CREATE TABLE vm_tags (
    id BIGSERIAL PRIMARY KEY,
    vm_id UUID NOT NULL REFERENCES virtual_machines(id) ON DELETE CASCADE,
    tag_id TEXT NOT NULL,
    category_name TEXT NOT NULL,
    tag_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_vm_tags_vm_tag UNIQUE (vm_id, tag_id)
);

-- Query Pattern: lookup all tags for a VM (used in GET /v1/vms/{id})
CREATE INDEX idx_vm_tags_vm_id ON vm_tags (vm_id);

-- Query Pattern: find VMs by category + tag name (search / filter)
CREATE INDEX idx_vm_tags_category_tag_vm ON vm_tags (category_name, tag_name, vm_id);

CREATE TABLE vm_custom_attributes (
    id BIGSERIAL PRIMARY KEY,
    vm_id UUID NOT NULL REFERENCES virtual_machines(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    value TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_vm_custom_attributes_vm_name UNIQUE (vm_id, name)
);

-- Query Pattern: lookup all custom attributes for a VM (used in GET /v1/vms/{id})
CREATE INDEX idx_vm_custom_attrs_vm_id ON vm_custom_attributes (vm_id);

-- Query Pattern: find VMs by attribute name + value (search / filter)
CREATE INDEX idx_vm_custom_attrs_name_value_vm ON vm_custom_attributes (name, value, vm_id);

-- Least privilege role grants (conditional if roles exist)
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'collector_rw') THEN
        EXECUTE 'GRANT SELECT, INSERT, UPDATE, DELETE ON vm_tags, vm_custom_attributes TO collector_rw';
        EXECUTE 'GRANT USAGE, SELECT ON SEQUENCE vm_tags_id_seq, vm_custom_attributes_id_seq TO collector_rw';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'query_ro') THEN
        EXECUTE 'GRANT SELECT ON vm_tags, vm_custom_attributes TO query_ro';
        EXECUTE 'GRANT SELECT ON SEQUENCE vm_tags_id_seq, vm_custom_attributes_id_seq TO query_ro';
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS vm_custom_attributes;
DROP TABLE IF EXISTS vm_tags;
