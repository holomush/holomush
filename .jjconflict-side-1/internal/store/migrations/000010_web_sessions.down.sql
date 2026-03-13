-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse 000010_web_sessions.up.sql

DROP INDEX IF EXISTS idx_web_sessions_expires;
DROP INDEX IF EXISTS idx_web_sessions_token;
DROP INDEX IF EXISTS idx_web_sessions_player;
DROP TABLE IF EXISTS web_sessions;
