-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Revert 000049_world_version_guard.up.sql: drop the optimistic-concurrency
-- version column from the four world tables. Dropped in reverse table order
-- (objects, characters, exits, locations) with DROP COLUMN IF EXISTS so the
-- revert is idempotent and cleanly restores the pre-migration schema.

ALTER TABLE objects DROP COLUMN IF EXISTS version;
ALTER TABLE characters DROP COLUMN IF EXISTS version;
ALTER TABLE exits DROP COLUMN IF EXISTS version;
ALTER TABLE locations DROP COLUMN IF EXISTS version;
