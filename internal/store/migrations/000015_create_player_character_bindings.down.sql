-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP INDEX IF EXISTS idx_pcb_player_active;
DROP INDEX IF EXISTS idx_pcb_active_per_character;
DROP TABLE IF EXISTS player_character_bindings;
-- Note: pgcrypto extension is NOT dropped; other migrations may depend on it later.
