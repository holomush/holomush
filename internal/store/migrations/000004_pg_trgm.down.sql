-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Drop trigram indexes. Extension left in place - may be used by other schemas.
-- Note: If extension removal is needed, run 'DROP EXTENSION IF EXISTS pg_trgm' manually.
DROP INDEX IF EXISTS idx_locations_name_trgm;
DROP INDEX IF EXISTS idx_objects_name_trgm;
DROP INDEX IF EXISTS idx_exits_name_trgm;
