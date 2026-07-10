-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse 000011_scene_notify_prefs.up.sql in reverse order: drop the indexes
-- first, then the table.
DROP INDEX IF EXISTS scene_notify_prefs_scene;
DROP INDEX IF EXISTS scene_notify_prefs_global;
DROP TABLE IF EXISTS scene_notify_prefs;
