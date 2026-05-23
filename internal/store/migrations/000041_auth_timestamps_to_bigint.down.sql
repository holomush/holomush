-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Revert auth-domain timestamp columns from BIGINT back to TIMESTAMPTZ.
-- WARNING: nanosecond precision is lost on revert (PostgreSQL TIMESTAMPTZ
-- stores microseconds only). This down migration is provided for rollback
-- purposes only; do not rely on it for production data recovery.

-- Revert session history floor columns first (reverse of up order).

ALTER TABLE sessions
    ALTER COLUMN guest_character_created_at DROP DEFAULT;
ALTER TABLE sessions
    ALTER COLUMN guest_character_created_at
        TYPE TIMESTAMPTZ USING to_timestamp(guest_character_created_at::double precision / 1e9);
ALTER TABLE sessions
    ALTER COLUMN guest_character_created_at
        SET DEFAULT 'epoch'::timestamptz;

ALTER TABLE sessions
    ALTER COLUMN location_arrived_at DROP DEFAULT;
ALTER TABLE sessions
    ALTER COLUMN location_arrived_at
        TYPE TIMESTAMPTZ USING to_timestamp(location_arrived_at::double precision / 1e9);
ALTER TABLE sessions
    ALTER COLUMN location_arrived_at
        SET DEFAULT NOW();

-- Revert player_sessions.

ALTER TABLE player_sessions
    ALTER COLUMN updated_at DROP DEFAULT;
ALTER TABLE player_sessions
    ALTER COLUMN updated_at
        TYPE TIMESTAMPTZ USING to_timestamp(updated_at::double precision / 1e9);
ALTER TABLE player_sessions
    ALTER COLUMN updated_at
        SET DEFAULT now();

ALTER TABLE player_sessions
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE player_sessions
    ALTER COLUMN created_at
        TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9);
ALTER TABLE player_sessions
    ALTER COLUMN created_at
        SET DEFAULT now();

ALTER TABLE player_sessions
    ALTER COLUMN expires_at
        TYPE TIMESTAMPTZ USING to_timestamp(expires_at::double precision / 1e9);

-- Revert session_connections.

ALTER TABLE session_connections
    ALTER COLUMN connected_at DROP DEFAULT;
ALTER TABLE session_connections
    ALTER COLUMN connected_at
        TYPE TIMESTAMPTZ USING to_timestamp(connected_at::double precision / 1e9);
ALTER TABLE session_connections
    ALTER COLUMN connected_at
        SET DEFAULT now();

-- Revert sessions.

ALTER TABLE sessions
    ALTER COLUMN updated_at DROP DEFAULT;
ALTER TABLE sessions
    ALTER COLUMN updated_at
        TYPE TIMESTAMPTZ USING to_timestamp(updated_at::double precision / 1e9);
ALTER TABLE sessions
    ALTER COLUMN updated_at
        SET DEFAULT now();

ALTER TABLE sessions
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE sessions
    ALTER COLUMN created_at
        TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9);
ALTER TABLE sessions
    ALTER COLUMN created_at
        SET DEFAULT now();

ALTER TABLE sessions
    ALTER COLUMN expires_at
        TYPE TIMESTAMPTZ USING to_timestamp(expires_at::double precision / 1e9),
    ALTER COLUMN detached_at
        TYPE TIMESTAMPTZ USING to_timestamp(detached_at::double precision / 1e9);

-- Revert password_resets.

ALTER TABLE password_resets
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE password_resets
    ALTER COLUMN created_at
        TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9);
ALTER TABLE password_resets
    ALTER COLUMN created_at
        SET DEFAULT NOW();

ALTER TABLE password_resets
    ALTER COLUMN expires_at
        TYPE TIMESTAMPTZ USING to_timestamp(expires_at::double precision / 1e9),
    ALTER COLUMN used_at
        TYPE TIMESTAMPTZ USING to_timestamp(used_at::double precision / 1e9);

-- Revert players.

ALTER TABLE players
    ALTER COLUMN updated_at DROP DEFAULT;
ALTER TABLE players
    ALTER COLUMN updated_at
        TYPE TIMESTAMPTZ USING to_timestamp(updated_at::double precision / 1e9);
ALTER TABLE players
    ALTER COLUMN updated_at
        SET DEFAULT NOW();

ALTER TABLE players
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE players
    ALTER COLUMN created_at
        TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9);
ALTER TABLE players
    ALTER COLUMN created_at
        SET DEFAULT NOW();

ALTER TABLE players
    ALTER COLUMN locked_until
        TYPE TIMESTAMPTZ USING to_timestamp(locked_until::double precision / 1e9);
