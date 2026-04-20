-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Down migration: recreate the events table and event_cursors column.
-- NOTE: Production rollback is via reverting the squash-merge commit, not
-- running this migration. This file exists by convention only.

CREATE TABLE IF NOT EXISTS events (
    id         TEXT PRIMARY KEY,
    stream     TEXT NOT NULL,
    type       TEXT NOT NULL,
    actor_kind SMALLINT NOT NULL,
    actor_id   TEXT NOT NULL,
    payload    JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_events_stream_id ON events (stream, id);

ALTER TABLE sessions ADD COLUMN IF NOT EXISTS event_cursors JSONB NOT NULL DEFAULT '{}';
