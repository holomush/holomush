-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Bootstrap script for databases that were migrated with the old system.
-- Run this ONCE on existing databases before using golang-migrate.
--
-- Usage:
--   psql -d holomush -f scripts/bootstrap-migrations.sql
--
-- This tells golang-migrate that migrations 1-7 have already been applied.

CREATE TABLE IF NOT EXISTS schema_migrations (
    version bigint NOT NULL PRIMARY KEY,
    dirty boolean NOT NULL
);

INSERT INTO schema_migrations (version, dirty)
VALUES (7, false)
ON CONFLICT (version) DO NOTHING;

-- Verify
SELECT version, dirty FROM schema_migrations;
