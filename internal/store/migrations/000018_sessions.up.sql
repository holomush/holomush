-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Drop the legacy sessions table from migration 001 (simple character_id/last_event_id tracker).
-- The new schema below replaces it with a full-featured persistent session model.
DROP TABLE IF EXISTS sessions;

-- sessions: persistent game sessions that survive disconnects
CREATE TABLE sessions (
    id              TEXT PRIMARY KEY,
    character_id    TEXT NOT NULL,
    character_name  TEXT NOT NULL,
    location_id     TEXT NOT NULL,
    is_guest        BOOLEAN NOT NULL DEFAULT false,
    status          TEXT NOT NULL DEFAULT 'active',
    grid_present    BOOLEAN NOT NULL DEFAULT false,
    event_cursors   JSONB NOT NULL DEFAULT '{}',
    command_history TEXT[] NOT NULL DEFAULT '{}',
    ttl_seconds     INTEGER NOT NULL DEFAULT 1800,
    max_history     INTEGER NOT NULL DEFAULT 500,
    detached_at     TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One active/detached session per character at most
CREATE UNIQUE INDEX idx_sessions_active_character
    ON sessions (character_id) WHERE status IN ('active', 'detached');

-- Fast lookup for reaper: find detached sessions by expiry
CREATE INDEX idx_sessions_status ON sessions (status) WHERE status = 'detached';

-- session_connections: tracks individual client connections to a session
CREATE TABLE session_connections (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    client_type  TEXT NOT NULL,
    streams      TEXT[] NOT NULL,
    connected_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_session_connections_session ON session_connections (session_id);

-- player_tokens: opaque tokens for two-phase login
CREATE TABLE player_tokens (
    token       TEXT PRIMARY KEY,
    player_id   TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_player_tokens_player ON player_tokens (player_id);
