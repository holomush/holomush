-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse 000009_auth_player_fields.up.sql

-- Remove FK constraint first
ALTER TABLE players DROP CONSTRAINT IF EXISTS fk_players_default_character;

-- Restore NOT NULL on characters.player_id
-- WARNING: This will fail if any rostered characters exist (NULL player_id).
-- Before running this down migration, either:
--   1. Assign all rostered characters to a player, or
--   2. Delete rostered characters
-- This is intentional: reversing Epic 5 requires addressing rostered characters.
ALTER TABLE characters ALTER COLUMN player_id SET NOT NULL;

-- Remove index
DROP INDEX IF EXISTS idx_players_email;

-- Remove columns
ALTER TABLE players DROP COLUMN IF EXISTS updated_at;
ALTER TABLE players DROP COLUMN IF EXISTS preferences;
ALTER TABLE players DROP COLUMN IF EXISTS default_character_id;
ALTER TABLE players DROP COLUMN IF EXISTS locked_until;
ALTER TABLE players DROP COLUMN IF EXISTS failed_attempts;
ALTER TABLE players DROP COLUMN IF EXISTS email_verified;
ALTER TABLE players DROP COLUMN IF EXISTS email;
