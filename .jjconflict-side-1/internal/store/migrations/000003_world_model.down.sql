-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Drop index on characters (added by this migration)
DROP INDEX IF EXISTS idx_characters_location;

-- Drop scene_participants table and index
DROP INDEX IF EXISTS idx_scene_participants_character;
DROP TABLE IF EXISTS scene_participants;

-- Drop objects table and indexes
DROP INDEX IF EXISTS idx_objects_contained;
DROP INDEX IF EXISTS idx_objects_held_by;
DROP INDEX IF EXISTS idx_objects_location;
DROP TABLE IF EXISTS objects;

-- Drop exits table and indexes
DROP INDEX IF EXISTS idx_exits_to;
DROP INDEX IF EXISTS idx_exits_from;
DROP TABLE IF EXISTS exits;

-- Drop location indexes added by this migration
DROP INDEX IF EXISTS idx_locations_shadows;
DROP INDEX IF EXISTS idx_locations_type;

-- Remove columns added to locations table
ALTER TABLE locations DROP COLUMN IF EXISTS archived_at;
ALTER TABLE locations DROP COLUMN IF EXISTS replay_policy;
ALTER TABLE locations DROP COLUMN IF EXISTS owner_id;
ALTER TABLE locations DROP COLUMN IF EXISTS shadows_id;
ALTER TABLE locations DROP COLUMN IF EXISTS type;
