-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Restore original FK without CASCADE.
ALTER TABLE characters DROP CONSTRAINT IF EXISTS characters_player_id_fkey;
ALTER TABLE characters ADD CONSTRAINT characters_player_id_fkey
    FOREIGN KEY (player_id) REFERENCES players(id);

DROP INDEX IF EXISTS idx_players_is_guest;
ALTER TABLE players DROP COLUMN IF EXISTS is_guest;
