-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Phase 3b grounding doc Decision 7 / master spec §4.3a.
-- Bindings are long-lived player↔character tenures (weeks/months,
-- spanning many sessions). binding_id is the load-bearing identifier
-- in §7.2 Branch 1 AuthGuard decisions and crypto_keys.participants.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS player_character_bindings (
    id            TEXT PRIMARY KEY,             -- ULID-format string
    player_id     TEXT NOT NULL REFERENCES players(id),
    character_id  TEXT NOT NULL REFERENCES characters(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at      TIMESTAMPTZ,
    ended_reason  TEXT
);

-- Exactly one active binding per character.
CREATE UNIQUE INDEX IF NOT EXISTS idx_pcb_active_per_character
    ON player_character_bindings (character_id) WHERE ended_at IS NULL;

-- Player-side index for "what's this player's active binding for character X" lookups.
CREATE INDEX IF NOT EXISTS idx_pcb_player_active
    ON player_character_bindings (player_id) WHERE ended_at IS NULL;

-- Back-population: every existing character with non-NULL player_id
-- gets a binding row. Orphan characters (player_id IS NULL, permitted
-- by the baseline schema's nullable FK) are excluded; they have no
-- active binding and Subscribe will return BINDING_MISSING for them
-- under Phase 3d's flag flip. Phase 4 wizard-transfer or character
-- deletion will resolve those edge cases.
INSERT INTO player_character_bindings (id, player_id, character_id, created_at, ended_reason)
SELECT
    -- Synthetic id for back-populated rows: 64-char hex from sha256 of
    -- (player_id || character_id). New rows use 26-char ULIDs from
    -- idgen.New() in Go. The shape difference is documented and only
    -- visible to operators reading the table directly.
    encode(digest(player_id || id, 'sha256'), 'hex')::TEXT,
    player_id,
    id,
    NOW(),
    'back_populated_at_migration_000015'
FROM characters
WHERE player_id IS NOT NULL
ON CONFLICT (character_id) WHERE ended_at IS NULL DO NOTHING;
