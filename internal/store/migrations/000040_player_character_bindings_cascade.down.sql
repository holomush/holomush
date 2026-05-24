-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Revert the player_id FK to the original no-cascade (RESTRICT) form created
-- by 000015. (character_id was never altered by the up migration.)

ALTER TABLE player_character_bindings
    DROP CONSTRAINT IF EXISTS player_character_bindings_player_id_fkey;
ALTER TABLE player_character_bindings
    ADD CONSTRAINT player_character_bindings_player_id_fkey
    FOREIGN KEY (player_id) REFERENCES players(id);
