-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

CREATE TABLE IF NOT EXISTS player_totp (
    player_id        TEXT PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
    wrapped_secret   BYTEA NOT NULL,
    wrap_key_id      TEXT NOT NULL,
    enrolled_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_verified_at TIMESTAMPTZ,
    last_used_step   BIGINT,
    failed_attempts  INTEGER NOT NULL DEFAULT 0,
    locked_until     TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS player_totp_recovery_codes (
    id           TEXT PRIMARY KEY,
    player_id    TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    code_hash    TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    consumed_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_pt_recovery_player_active
    ON player_totp_recovery_codes (player_id) WHERE consumed_at IS NULL;

CREATE TABLE IF NOT EXISTS crypto_bootstrap_state (
    key                     TEXT PRIMARY KEY,
    consumed_at             TIMESTAMPTZ NOT NULL,
    consumed_by_player_id   TEXT NOT NULL REFERENCES players(id)
);
