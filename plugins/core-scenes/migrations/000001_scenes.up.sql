-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Phase 1 schema: scenes table only.
-- Subsequent migrations add scene_participants (Phase 3),
-- scene_logs (Phase 6), and scene_templates (Phase 7).

CREATE TABLE IF NOT EXISTS scenes (
    id               TEXT        PRIMARY KEY,
    title            TEXT        NOT NULL,
    description      TEXT        NOT NULL DEFAULT '',
    location_id      TEXT,
    owner_id         TEXT        NOT NULL,
    state            TEXT        NOT NULL DEFAULT 'active',
    pose_order       TEXT        NOT NULL DEFAULT 'free',
    visibility       TEXT        NOT NULL DEFAULT 'open',
    idle_timeout_secs INTEGER,
    template_id      TEXT,
    content_warnings TEXT[]      NOT NULL DEFAULT '{}',
    tags             TEXT[]      NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at         TIMESTAMPTZ,
    archived_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_scenes_state ON scenes(state);
CREATE INDEX IF NOT EXISTS idx_scenes_owner ON scenes(owner_id);
CREATE INDEX IF NOT EXISTS idx_scenes_location ON scenes(location_id) WHERE location_id IS NOT NULL;
