-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP INDEX IF EXISTS idx_sessions_player_session_id;

ALTER TABLE sessions
  DROP COLUMN IF EXISTS player_session_id;
