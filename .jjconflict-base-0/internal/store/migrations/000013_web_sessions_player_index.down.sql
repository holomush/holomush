-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Revert to original single-column index
DROP INDEX IF EXISTS idx_web_sessions_player_created;
CREATE INDEX idx_web_sessions_player ON web_sessions(player_id);
