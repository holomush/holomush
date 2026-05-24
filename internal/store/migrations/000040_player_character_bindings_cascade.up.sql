-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- player_character_bindings (000015) created its player_id FK WITHOUT ON DELETE
-- CASCADE, unlike every other player-referencing FK (e.g. characters.player_id
-- in 000002, player_totp in 000019). This blocked PlayerRepository.
-- DeleteGuestPlayer (called by GuestReaper): deleting a guest player that had a
-- binding failed with
--   "violates foreign key constraint player_character_bindings_player_id_fkey"
-- (SQLSTATE 23503), so guests with characters were never reaped and the reaper
-- logged a WARN every interval. Re-create the player_id FK with ON DELETE
-- CASCADE so deleting a player removes its bindings (and characters.player_id's
-- existing cascade removes the character).
--
-- The character_id FK is intentionally LEFT as RESTRICT (no cascade): the
-- crypto forensic-retention design (docs/superpowers/specs/
-- 2026-04-25-event-payload-crypto-design.md §"Character deletion") requires
-- character deletion to SOFT-end the binding (ended_at/ended_reason) and keep
-- historical bindings queryable for forensic decryption — they MUST NOT be
-- cascade-deleted. The guest-reaper path does not need it: deleting the player
-- cascade-deletes the binding via player_id before the character is removed.

ALTER TABLE player_character_bindings
    DROP CONSTRAINT IF EXISTS player_character_bindings_player_id_fkey;
ALTER TABLE player_character_bindings
    ADD CONSTRAINT player_character_bindings_player_id_fkey
    FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE;
