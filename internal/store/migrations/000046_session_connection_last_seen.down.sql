-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP INDEX IF EXISTS idx_session_connections_last_seen;
ALTER TABLE session_connections DROP COLUMN IF EXISTS last_seen_at;
