-- Database Role Bootstrap Script
-- Creates least-privilege roles for SIMS platform:
-- 1. migration_owner: Schema author and migration executor (DDL rights)
-- 2. collector_rw: Inventory data ingestion and worker lease management (DML rights only)
-- 3. query_ro: Read-only query API access for search and views (SELECT only)
-- 4. sims: Unified database user for entire application

DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'migration_owner') THEN
        CREATE ROLE migration_owner WITH LOGIN PASSWORD 'migration_owner_pass';
    END IF;

    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'collector_rw') THEN
        CREATE ROLE collector_rw WITH LOGIN PASSWORD 'collector_rw_pass';
    END IF;

    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'query_ro') THEN
        CREATE ROLE query_ro WITH LOGIN PASSWORD 'query_ro_pass';
    END IF;

    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'sims') THEN
        CREATE ROLE sims WITH LOGIN PASSWORD 'sims_password';
    END IF;
END
$$;

-- Grant database connection
GRANT CONNECT ON DATABASE sims TO migration_owner, collector_rw, query_ro, sims;

-- Schema privileges
GRANT USAGE, CREATE ON SCHEMA public TO migration_owner, sims;
GRANT USAGE ON SCHEMA public TO collector_rw, query_ro;

-- Permissions for unified sims user on all existing tables and sequences
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO sims;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO sims;

-- Default privileges for future tables created by migration_owner:
ALTER DEFAULT PRIVILEGES FOR ROLE migration_owner IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO collector_rw, sims;

ALTER DEFAULT PRIVILEGES FOR ROLE migration_owner IN SCHEMA public
    GRANT SELECT ON TABLES TO query_ro;

-- Default privileges for future sequences created by migration_owner:
ALTER DEFAULT PRIVILEGES FOR ROLE migration_owner IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO collector_rw, sims;

ALTER DEFAULT PRIVILEGES FOR ROLE migration_owner IN SCHEMA public
    GRANT SELECT ON SEQUENCES TO query_ro;

ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO sims;

ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO sims;
