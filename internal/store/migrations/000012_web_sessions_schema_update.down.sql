-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse 000012_web_sessions_schema_update.up.sql

-- Drop index before renaming columns
DROP INDEX IF EXISTS idx_web_sessions_token;

-- Remove added columns
ALTER TABLE web_sessions DROP COLUMN IF EXISTS ip_address;
ALTER TABLE web_sessions DROP COLUMN IF EXISTS user_agent;

-- Reverse character_id constraint changes
ALTER TABLE web_sessions DROP CONSTRAINT IF EXISTS web_sessions_character_id_fkey;
ALTER TABLE web_sessions ALTER COLUMN character_id SET NOT NULL;
ALTER TABLE web_sessions ADD CONSTRAINT web_sessions_character_id_fkey
    FOREIGN KEY (character_id) REFERENCES characters(id) ON DELETE CASCADE;

-- Reverse column renames
ALTER TABLE web_sessions RENAME COLUMN last_seen_at TO last_active_at;
ALTER TABLE web_sessions RENAME COLUMN token_hash TO token_signature;

-- Recreate index with original column name
CREATE UNIQUE INDEX IF NOT EXISTS idx_web_sessions_token ON web_sessions(token_signature);
