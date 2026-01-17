-- System info table for storing key-value configuration like game_id
CREATE TABLE IF NOT EXISTS holomush_system_info (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    updated_at  TIMESTAMPTZ DEFAULT NOW()
);
