-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Convert auth-domain timestamp columns from TIMESTAMPTZ to BIGINT
-- (epoch nanoseconds, UTC). INV-TS-1.

-- players: locked_until is nullable with no DEFAULT; drop/restore not needed.
-- created_at and updated_at have DEFAULT NOW() and must have their default dropped
-- before the TYPE change (no implicit TIMESTAMPTZ→BIGINT cast in PostgreSQL).

ALTER TABLE players
    ALTER COLUMN locked_until
        TYPE BIGINT USING (EXTRACT(EPOCH FROM locked_until) * 1e9)::BIGINT;

ALTER TABLE players
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE players
    ALTER COLUMN created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM created_at) * 1e9)::BIGINT;
ALTER TABLE players
    ALTER COLUMN created_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE players
    ALTER COLUMN updated_at DROP DEFAULT;
ALTER TABLE players
    ALTER COLUMN updated_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM updated_at) * 1e9)::BIGINT;
ALTER TABLE players
    ALTER COLUMN updated_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

-- password_resets: expires_at and used_at are nullable with no DEFAULT.
-- created_at has DEFAULT NOW() and must have its default dropped first.

ALTER TABLE password_resets
    ALTER COLUMN expires_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM expires_at) * 1e9)::BIGINT,
    ALTER COLUMN used_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM used_at) * 1e9)::BIGINT;

ALTER TABLE password_resets
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE password_resets
    ALTER COLUMN created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM created_at) * 1e9)::BIGINT;
ALTER TABLE password_resets
    ALTER COLUMN created_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

-- sessions: expires_at and detached_at are nullable with no DEFAULT.
-- created_at and updated_at have DEFAULT now() and must have defaults dropped first.

ALTER TABLE sessions
    ALTER COLUMN expires_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM expires_at) * 1e9)::BIGINT,
    ALTER COLUMN detached_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM detached_at) * 1e9)::BIGINT;

ALTER TABLE sessions
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE sessions
    ALTER COLUMN created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM created_at) * 1e9)::BIGINT;
ALTER TABLE sessions
    ALTER COLUMN created_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE sessions
    ALTER COLUMN updated_at DROP DEFAULT;
ALTER TABLE sessions
    ALTER COLUMN updated_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM updated_at) * 1e9)::BIGINT;
ALTER TABLE sessions
    ALTER COLUMN updated_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

-- session_connections: connected_at has DEFAULT now() and must have default dropped first.

ALTER TABLE session_connections
    ALTER COLUMN connected_at DROP DEFAULT;
ALTER TABLE session_connections
    ALTER COLUMN connected_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM connected_at) * 1e9)::BIGINT;
ALTER TABLE session_connections
    ALTER COLUMN connected_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

-- player_sessions: expires_at is NOT NULL with no DEFAULT.
-- created_at and updated_at have DEFAULT now() and must have defaults dropped first.
-- Note: player_sessions has no detached_at column; plan SQL is corrected here.

ALTER TABLE player_sessions
    ALTER COLUMN expires_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM expires_at) * 1e9)::BIGINT;

ALTER TABLE player_sessions
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE player_sessions
    ALTER COLUMN created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM created_at) * 1e9)::BIGINT;
ALTER TABLE player_sessions
    ALTER COLUMN created_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE player_sessions
    ALTER COLUMN updated_at DROP DEFAULT;
ALTER TABLE player_sessions
    ALTER COLUMN updated_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM updated_at) * 1e9)::BIGINT;
ALTER TABLE player_sessions
    ALTER COLUMN updated_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

-- Session history floor columns (added in migration 000037). Migrated here
-- alongside the rest of the sessions table so the table converts atomically
-- and session_store.go::Set() does not have to handle a mixed-type interim state.
-- location_arrived_at has DEFAULT NOW() from migration 000037.
-- guest_character_created_at has DEFAULT 'epoch' from migration 000037.

ALTER TABLE sessions
    ALTER COLUMN location_arrived_at DROP DEFAULT;
ALTER TABLE sessions
    ALTER COLUMN location_arrived_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM location_arrived_at) * 1e9)::BIGINT;
ALTER TABLE sessions
    ALTER COLUMN location_arrived_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE sessions
    ALTER COLUMN guest_character_created_at DROP DEFAULT;
ALTER TABLE sessions
    ALTER COLUMN guest_character_created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM guest_character_created_at) * 1e9)::BIGINT;
ALTER TABLE sessions
    ALTER COLUMN guest_character_created_at
        SET DEFAULT 0;
