-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Password reset tokens for Epic 5
-- Stores hashed tokens with expiry for secure password recovery

CREATE TABLE IF NOT EXISTS password_resets (
    id TEXT PRIMARY KEY,
    player_id TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,  -- NULL means unused, set when token is consumed
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_password_resets_player ON password_resets(player_id);
CREATE INDEX IF NOT EXISTS idx_password_resets_expires ON password_resets(expires_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_password_resets_token_hash ON password_resets(token_hash);
