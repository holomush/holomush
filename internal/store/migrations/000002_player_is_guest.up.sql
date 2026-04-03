-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Add is_guest flag to players table for ephemeral guest accounts.

ALTER TABLE players ADD COLUMN IF NOT EXISTS is_guest BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS idx_players_is_guest ON players (is_guest) WHERE is_guest = true;

-- Add ON DELETE CASCADE to characters.player_id so deleting a guest player
-- cascades to their characters. The original FK had no cascade action.
ALTER TABLE characters DROP CONSTRAINT IF EXISTS characters_player_id_fkey;
ALTER TABLE characters ADD CONSTRAINT characters_player_id_fkey
    FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE;
