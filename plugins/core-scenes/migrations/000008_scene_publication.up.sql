-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Phase 6 (holomush-5rh.15): publication artifact distinct from the
-- audit scene_log table. See spec section 3.3.
-- Timestamps are BIGINT epoch-nanoseconds (INV-STORE-1).

CREATE TABLE IF NOT EXISTS published_scenes (
    id                     TEXT        PRIMARY KEY,
    scene_id               TEXT        NOT NULL,
    attempt_number         INTEGER     NOT NULL,
    status                 TEXT        NOT NULL CHECK (status IN
                              ('COLLECTING','COOLOFF','PUBLISHED','ATTEMPT_FAILED')),
    initiated_by           TEXT        NOT NULL,
    initiated_at           BIGINT      NOT NULL,
    cooloff_started_at     BIGINT,
    resolved_at            BIGINT,
    vote_window            INTERVAL    NOT NULL,
    cooloff_window         INTERVAL    NOT NULL,
    max_attempts_snapshot  INTEGER     NOT NULL,
    content_entries        JSONB,
    title_snapshot         TEXT,
    participants_snapshot  JSONB,
    published_at           BIGINT,
    failure_reason         TEXT        CHECK (failure_reason IS NULL OR failure_reason IN
                              ('ANY_NO','TIMEOUT','WITHDRAWN',
                               'SNAPSHOT_DECRYPT_FAILED','SNAPSHOT_RENDER_FAILED',
                               'COOLOFF_INVARIANT_BROKEN')),
    -- failure_reason is present iff the attempt failed (state-model integrity).
    CHECK ((status = 'ATTEMPT_FAILED') = (failure_reason IS NOT NULL))
);

CREATE UNIQUE INDEX IF NOT EXISTS published_scenes_one_active_per_scene
    ON published_scenes(scene_id) WHERE status IN ('COLLECTING','COOLOFF');
CREATE UNIQUE INDEX IF NOT EXISTS published_scenes_one_published_per_scene
    ON published_scenes(scene_id) WHERE status = 'PUBLISHED';
CREATE UNIQUE INDEX IF NOT EXISTS published_scenes_attempt_unique
    ON published_scenes(scene_id, attempt_number);
CREATE INDEX IF NOT EXISTS published_scenes_scene_status
    ON published_scenes(scene_id, status);

CREATE TABLE IF NOT EXISTS published_scene_votes (
    published_scene_id  TEXT        NOT NULL REFERENCES published_scenes(id) ON DELETE CASCADE,
    character_id        TEXT        NOT NULL,
    vote                BOOLEAN,
    voted_at            BIGINT,
    last_changed_at     BIGINT,
    PRIMARY KEY (published_scene_id, character_id)
);

CREATE INDEX IF NOT EXISTS published_scene_votes_pending
    ON published_scene_votes(published_scene_id) WHERE vote IS NULL;
