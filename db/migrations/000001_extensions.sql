-- +goose Up
-- Enable only required extensions for inventory hierarchy and fuzzy text search
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS ltree;

-- +goose Down
DROP EXTENSION IF EXISTS ltree;
DROP EXTENSION IF EXISTS pg_trgm;
