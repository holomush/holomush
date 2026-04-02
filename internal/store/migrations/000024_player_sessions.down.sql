-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

ALTER TABLE session_connections DROP COLUMN IF EXISTS player_session_id;

DROP TABLE IF EXISTS player_sessions;

-- Recreate original tables (from migrations 018 and 010).
CREATE TABLE IF NOT EXISTS player_tokens (
    token       TEXT PRIMARY KEY,
    player_id   TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_player_tokens_player ON player_tokens (player_id);

-- Recreate web_sessions with the schema as of migration 012 (post-schema-update).
-- Earlier down migrations (013, 012, 010) handle reverting indexes and columns.
CREATE TABLE IF NOT EXISTS web_sessions (
    id TEXT PRIMARY KEY,
    player_id TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    character_id TEXT REFERENCES characters(id) ON DELETE SET NULL,
    token_hash TEXT NOT NULL,
    user_agent TEXT NOT NULL DEFAULT '',
    ip_address TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_web_sessions_token ON web_sessions(token_hash);
