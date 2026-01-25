-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS sessions;
DROP INDEX IF EXISTS idx_events_stream_id;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS characters;
DROP TABLE IF EXISTS locations;
DROP TABLE IF EXISTS players;
