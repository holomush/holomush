-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Scene notification preferences (Phase 2 SCENEFWD-02, decisions D-04/D-05).
--
-- One table carries two row shapes, disambiguated by scene_id:
--   * scene_id IS NULL     -> the per-character GLOBAL notify preference.
--                             muted=true means notifications are globally OFF.
--   * scene_id IS NOT NULL -> a per-scene MUTE flag for that character.
--                             muted=true means that scene is muted.
--
-- The `mode` column is the D-05 digest seam: it ships defaulting to 'realtime'
-- so digest delivery can land later with no schema migration or prefs rewrite.
--
-- Timestamps are BIGINT epoch-nanoseconds to match the rest of the
-- plugin_core_scenes schema (migration 000007).
CREATE TABLE IF NOT EXISTS scene_notify_prefs (
    character_id TEXT NOT NULL,
    scene_id     TEXT,
    muted        BOOLEAN NOT NULL DEFAULT false,
    mode         TEXT NOT NULL DEFAULT 'realtime',
    created_at   BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
    updated_at   BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
);

-- The per-character global row is unique per character. A partial unique index
-- keyed on character_id WHERE scene_id IS NULL enforces one global row per
-- character (a plain UNIQUE would treat NULL scene_id values as distinct).
CREATE UNIQUE INDEX IF NOT EXISTS scene_notify_prefs_global
    ON scene_notify_prefs (character_id)
    WHERE scene_id IS NULL;

-- Per-scene mute rows are unique per (character_id, scene_id). Partial so the
-- global rows above are governed only by scene_notify_prefs_global.
CREATE UNIQUE INDEX IF NOT EXISTS scene_notify_prefs_scene
    ON scene_notify_prefs (character_id, scene_id)
    WHERE scene_id IS NOT NULL;
