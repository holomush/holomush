-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- +goose Up

-- 000053 — index sessions.location_id (issue #4796).
--
-- The presence / who-is-here query (PostgresSessionStore.ListActiveByLocation,
-- internal/store/session_store.go) filters sessions with
--   WHERE location_id = $1 AND status = 'active' AND grid_present = true
-- and drives every location view in the game loop. Across migrations 000001
-- through 000052 the sessions table carried three indexes — a partial unique
-- index on character_id, a partial index on status, and one on
-- player_session_id — and none of them covers this access path, so the query
-- was a sequential scan that degrades as the session table grows.
--
-- Plain index build. PostgreSQL's non-blocking index-build form cannot run
-- inside a transaction, and the migration runner wraps each migration in one;
-- none of the 52 preceding migrations uses that form, so adopting it here
-- would mean adding non-transactional runner support — a new capability
-- rather than a new migration.

CREATE INDEX IF NOT EXISTS idx_sessions_location_id
  ON sessions(location_id);

-- +goose Down

-- Reverse 000053: drop the presence-query index on sessions.location_id.

DROP INDEX IF EXISTS idx_sessions_location_id;
