-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Delete test data first (reverse order of insertion)
DELETE FROM characters WHERE id = '01KDVDNA002MB1E60S38DHR78Y';
DELETE FROM locations WHERE id = '01KDVDNA001C60T3GF208H44RM';
DELETE FROM players WHERE id = '01KDVDNA00041061050R3GG28A';

-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS sessions;
DROP INDEX IF EXISTS idx_events_stream_id;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS characters;
DROP TABLE IF EXISTS locations;
DROP TABLE IF EXISTS players;
