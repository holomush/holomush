-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Optimize GetByPlayer queries which filter by player_id and order by created_at DESC.
-- Replace the single-column index with a composite index for better query performance.

DROP INDEX IF EXISTS idx_web_sessions_player;
CREATE INDEX idx_web_sessions_player_created ON web_sessions(player_id, created_at DESC);
