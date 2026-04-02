-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Replace player_tokens + web_sessions with unified player_sessions.

CREATE TABLE player_sessions (
    id            TEXT PRIMARY KEY,
    player_id     TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    token_hash    TEXT NOT NULL,
    user_agent    TEXT NOT NULL DEFAULT '',
    ip_address    TEXT NOT NULL DEFAULT '',
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_player_sessions_token_hash ON player_sessions (token_hash);
CREATE INDEX idx_player_sessions_player ON player_sessions (player_id);
CREATE INDEX idx_player_sessions_expires ON player_sessions (expires_at);

-- Track which player session owns each connection for clean logout.
ALTER TABLE session_connections
    ADD COLUMN player_session_id TEXT REFERENCES player_sessions(id) ON DELETE SET NULL;

-- Drop replaced tables.
DROP TABLE IF EXISTS player_tokens;
DROP TABLE IF EXISTS web_sessions;
