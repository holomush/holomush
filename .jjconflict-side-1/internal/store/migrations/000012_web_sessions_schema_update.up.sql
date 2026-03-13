-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Update web_sessions table to match domain model:
-- - Rename token_signature to token_hash
-- - Make character_id nullable (session may not have character selected yet)
-- - Add user_agent and ip_address columns
-- - Rename last_active_at to last_seen_at

-- Drop existing indexes before renaming columns
DROP INDEX IF EXISTS idx_web_sessions_token;

-- Rename columns
ALTER TABLE web_sessions RENAME COLUMN token_signature TO token_hash;
ALTER TABLE web_sessions RENAME COLUMN last_active_at TO last_seen_at;

-- Make character_id nullable and drop the foreign key constraint
ALTER TABLE web_sessions DROP CONSTRAINT IF EXISTS web_sessions_character_id_fkey;
ALTER TABLE web_sessions ALTER COLUMN character_id DROP NOT NULL;

-- Re-add foreign key constraint (allows NULL now)
ALTER TABLE web_sessions ADD CONSTRAINT web_sessions_character_id_fkey
    FOREIGN KEY (character_id) REFERENCES characters(id) ON DELETE SET NULL;

-- Add new columns
ALTER TABLE web_sessions ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE web_sessions ADD COLUMN ip_address TEXT NOT NULL DEFAULT '';

-- Recreate index with new column name
CREATE UNIQUE INDEX IF NOT EXISTS idx_web_sessions_token ON web_sessions(token_hash);
