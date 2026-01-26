-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Auth player fields for Epic 5
-- Adds email, rate limiting, preferences, and rostering support

-- Email and verification
ALTER TABLE players ADD COLUMN IF NOT EXISTS email TEXT;
ALTER TABLE players ADD COLUMN IF NOT EXISTS email_verified BOOLEAN NOT NULL DEFAULT FALSE;

-- Rate limiting fields
ALTER TABLE players ADD COLUMN IF NOT EXISTS failed_attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE players ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ;

-- Default character preference (FK added after characters nullable player_id)
ALTER TABLE players ADD COLUMN IF NOT EXISTS default_character_id TEXT;

-- Extensible preferences
ALTER TABLE players ADD COLUMN IF NOT EXISTS preferences JSONB NOT NULL DEFAULT '{}';

-- Timestamps (updated_at managed at application layer, not via database trigger)
ALTER TABLE players ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Unique index on email (partial - only when email is set)
CREATE UNIQUE INDEX IF NOT EXISTS idx_players_email ON players(email) WHERE email IS NOT NULL;

-- Allow rostered characters (player_id nullable for future holomush-gloh epic)
ALTER TABLE characters ALTER COLUMN player_id DROP NOT NULL;

-- Now add the FK constraint for default_character_id
ALTER TABLE players ADD CONSTRAINT fk_players_default_character
    FOREIGN KEY (default_character_id) REFERENCES characters(id) ON DELETE SET NULL;
