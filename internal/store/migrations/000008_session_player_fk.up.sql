-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Link each game session to the PlayerSession that spawned it.
-- ON DELETE CASCADE means deleting a PlayerSession (logout, cap eviction,
-- revoke, password reset) automatically removes game sessions it created.

ALTER TABLE sessions
  ADD COLUMN IF NOT EXISTS player_session_id TEXT
    REFERENCES player_sessions(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_sessions_player_session_id
  ON sessions(player_session_id);
