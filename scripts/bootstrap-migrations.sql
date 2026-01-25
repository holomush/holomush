-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Bootstrap script for databases that were migrated with the old system.
-- Run this ONCE on existing databases before using golang-migrate.
--
-- Usage:
--   psql -d holomush -f scripts/bootstrap-migrations.sql
--
-- This tells golang-migrate that migrations 1-7 have already been applied.

-- ============================================================================
-- WARNING: This script assumes ALL 7 migrations were already applied!
-- ============================================================================
--
-- This script is ONLY for databases that were FULLY migrated with the old
-- (pre-golang-migrate) system. It marks version 7 as applied.
--
-- DANGER: If your database has only partial migrations (e.g., versions 1-5),
-- running this script will cause golang-migrate to SKIP migrations 6-7,
-- potentially leaving your schema in an inconsistent state.
--
-- Before running, verify your database has:
--   - events table with stream, id, type, payload, metadata, occurred_at columns
--   - players table with id, email, created_at columns
--   - characters table with id, player_id, name, location_id columns
--   - locations table with id, name, description columns
--   - sessions table
--   - system_info table
--   - pg_trgm extension enabled
--
-- If any of these are missing, DO NOT run this script. Instead, run:
--   task migrate
-- to apply migrations from scratch.
-- ============================================================================

-- Verify expected tables exist before proceeding
DO $$
BEGIN
    -- Check for tables created by migrations 1-7
    IF NOT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'events') THEN
        RAISE EXCEPTION 'Table "events" not found. Database may not be fully migrated. Aborting.';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'players') THEN
        RAISE EXCEPTION 'Table "players" not found. Database may not be fully migrated. Aborting.';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'characters') THEN
        RAISE EXCEPTION 'Table "characters" not found. Database may not be fully migrated. Aborting.';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'locations') THEN
        RAISE EXCEPTION 'Table "locations" not found. Database may not be fully migrated. Aborting.';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'system_info') THEN
        RAISE EXCEPTION 'Table "system_info" not found. Database may not be fully migrated. Aborting.';
    END IF;
    RAISE NOTICE 'All expected tables found. Proceeding with bootstrap.';
END $$;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version bigint NOT NULL PRIMARY KEY,
    dirty boolean NOT NULL
);

INSERT INTO schema_migrations (version, dirty)
VALUES (7, false)
ON CONFLICT (version) DO NOTHING;

-- Verify
SELECT version, dirty FROM schema_migrations;
