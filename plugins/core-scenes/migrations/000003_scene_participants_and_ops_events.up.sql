-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Phase 3 schema: scene_participants (membership snapshot) and
-- scene_ops_events (append-only ops journal).
--
-- See docs/superpowers/specs/2026-04-07-scenes-phase-3-membership-design.md
-- for the full design rationale, especially decisions P3.D3 (separate audit
-- table) and P3.D4 (ops vs content separation).

CREATE TABLE IF NOT EXISTS scene_participants (
    scene_id     TEXT        NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    character_id TEXT        NOT NULL,
    role         TEXT        NOT NULL CHECK (role IN ('owner', 'member', 'invited')),
    joined_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (scene_id, character_id)
);

CREATE INDEX IF NOT EXISTS idx_participants_scene_role
    ON scene_participants(scene_id, role);
CREATE INDEX IF NOT EXISTS idx_participants_character
    ON scene_participants(character_id);

CREATE TABLE IF NOT EXISTS scene_ops_events (
    id          TEXT        PRIMARY KEY,
    scene_id    TEXT        NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    kind        TEXT        NOT NULL CHECK (kind ~ '^[a-z]+\.[a-z_]+$'),
    actor_id    TEXT        NOT NULL,
    target_id   TEXT,
    payload     JSONB       NOT NULL DEFAULT '{}',
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_scene_ops_events_scene
    ON scene_ops_events(scene_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_scene_ops_events_target
    ON scene_ops_events(target_id, occurred_at DESC) WHERE target_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_scene_ops_events_kind
    ON scene_ops_events(scene_id, kind, occurred_at DESC);
