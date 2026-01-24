-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Enable pg_trgm extension for fuzzy text matching
-- Allows typo-tolerant matching for exit names, object names, etc.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Add GIN index for trigram-based fuzzy matching on exit names
-- Note: Aliases are searched via unnest() at query time; pg_trgm doesn't support text[] arrays directly
CREATE INDEX IF NOT EXISTS idx_exits_name_trgm ON exits USING gin (name gin_trgm_ops);

-- Add GIN index for fuzzy object name matching
CREATE INDEX IF NOT EXISTS idx_objects_name_trgm ON objects USING gin (name gin_trgm_ops);

-- Add GIN index for fuzzy location name matching
CREATE INDEX IF NOT EXISTS idx_locations_name_trgm ON locations USING gin (name gin_trgm_ops);
