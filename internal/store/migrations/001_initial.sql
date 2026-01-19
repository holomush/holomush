-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Players (accounts)
CREATE TABLE IF NOT EXISTS players (
    id TEXT PRIMARY KEY,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Locations
CREATE TABLE IF NOT EXISTS locations (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Characters
CREATE TABLE IF NOT EXISTS characters (
    id TEXT PRIMARY KEY,
    player_id TEXT NOT NULL REFERENCES players(id),
    name TEXT NOT NULL,
    location_id TEXT NOT NULL REFERENCES locations(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Events
CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    stream TEXT NOT NULL,
    type TEXT NOT NULL,
    actor_kind SMALLINT NOT NULL,
    actor_id TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_events_stream_id ON events (stream, id);

-- Sessions
CREATE TABLE IF NOT EXISTS sessions (
    character_id TEXT PRIMARY KEY REFERENCES characters(id),
    last_event_id TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Test data
INSERT INTO players (id, username, password_hash)
VALUES ('01JTEST001PLAYER00000001', 'testuser', '$2a$10$N9qo8uLOickgx2ZMRZoMye')
ON CONFLICT (id) DO NOTHING;

INSERT INTO locations (id, name, description)
VALUES ('01JTEST001LOCATN00000001', 'The Void', 'An empty expanse of nothing. This is where it all begins.')
ON CONFLICT (id) DO NOTHING;

INSERT INTO characters (id, player_id, name, location_id)
VALUES ('01JTEST001CHRCTR00000001', '01JTEST001PLAYER00000001', 'TestChar', '01JTEST001LOCATN00000001')
ON CONFLICT (id) DO NOTHING;
