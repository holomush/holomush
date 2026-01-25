-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Drop trigram indexes (don't drop extension - may be used by other schemas)
DROP INDEX IF EXISTS idx_locations_name_trgm;
DROP INDEX IF EXISTS idx_objects_name_trgm;
DROP INDEX IF EXISTS idx_exits_name_trgm;
