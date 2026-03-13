-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse 000011_password_resets.up.sql

DROP INDEX IF EXISTS idx_password_resets_expires;
DROP INDEX IF EXISTS idx_password_resets_player;
DROP TABLE IF EXISTS password_resets;
