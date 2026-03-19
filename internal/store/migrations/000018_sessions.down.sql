DROP TABLE IF EXISTS session_connections;
DROP TABLE IF EXISTS player_tokens;
DROP TABLE IF EXISTS sessions;

-- Restore the legacy sessions table from migration 001
CREATE TABLE IF NOT EXISTS sessions (
    character_id TEXT PRIMARY KEY REFERENCES characters(id),
    last_event_id TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
