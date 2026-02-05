-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Aliases for Epic 6: Commands & Behaviors

-- System-wide aliases (admin-managed)
CREATE TABLE IF NOT EXISTS system_aliases (
    alias       TEXT PRIMARY KEY,
    command     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  TEXT REFERENCES players(id)
);

-- Player-specific aliases
CREATE TABLE IF NOT EXISTS player_aliases (
    player_id   TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    alias       TEXT NOT NULL,
    command     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (player_id, alias)
);

-- Index for efficient player alias lookups
CREATE INDEX IF NOT EXISTS idx_player_aliases_player_id ON player_aliases(player_id);

COMMENT ON TABLE system_aliases IS 'System-wide command aliases managed by administrators';
COMMENT ON TABLE player_aliases IS 'Per-player command aliases';
