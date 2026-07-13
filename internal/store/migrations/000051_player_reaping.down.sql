-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Revert the D-06 anti-TOCTOU reaping-state flag (05-16). DROP COLUMN IF EXISTS
-- keeps the down idempotent and restores the players table to its pre-000051
-- shape exactly.
ALTER TABLE players DROP COLUMN IF EXISTS reaping_at;
