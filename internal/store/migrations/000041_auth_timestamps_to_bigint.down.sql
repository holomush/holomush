-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Revert auth-domain timestamp columns from BIGINT back to TIMESTAMPTZ.
-- WARNING: nanosecond precision is lost on revert (PostgreSQL TIMESTAMPTZ
-- stores microseconds only). This down migration is provided for rollback
-- purposes only; do not rely on it for production data recovery.
--
-- Idempotent: guarded on data_type = 'bigint' so re-running is safe.

DO $$
BEGIN
  -- Revert session history floor columns first (reverse of up order).
  -- sessions.guest_character_created_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'sessions'
               AND column_name = 'guest_character_created_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN guest_character_created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN guest_character_created_at TYPE TIMESTAMPTZ USING to_timestamp(guest_character_created_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN guest_character_created_at SET DEFAULT ''epoch''::timestamptz';
  END IF;

  -- sessions.location_arrived_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'sessions'
               AND column_name = 'location_arrived_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN location_arrived_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN location_arrived_at TYPE TIMESTAMPTZ USING to_timestamp(location_arrived_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN location_arrived_at SET DEFAULT NOW()';
  END IF;

  -- player_sessions.updated_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_sessions'
               AND column_name = 'updated_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE player_sessions ALTER COLUMN updated_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE player_sessions ALTER COLUMN updated_at TYPE TIMESTAMPTZ USING to_timestamp(updated_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE player_sessions ALTER COLUMN updated_at SET DEFAULT now()';
  END IF;

  -- player_sessions.created_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_sessions'
               AND column_name = 'created_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE player_sessions ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE player_sessions ALTER COLUMN created_at TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE player_sessions ALTER COLUMN created_at SET DEFAULT now()';
  END IF;

  -- player_sessions.expires_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_sessions'
               AND column_name = 'expires_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE player_sessions ALTER COLUMN expires_at TYPE TIMESTAMPTZ USING to_timestamp(expires_at::double precision / 1e9)';
  END IF;

  -- session_connections.connected_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'session_connections'
               AND column_name = 'connected_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE session_connections ALTER COLUMN connected_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE session_connections ALTER COLUMN connected_at TYPE TIMESTAMPTZ USING to_timestamp(connected_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE session_connections ALTER COLUMN connected_at SET DEFAULT now()';
  END IF;

  -- sessions.updated_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'sessions'
               AND column_name = 'updated_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN updated_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN updated_at TYPE TIMESTAMPTZ USING to_timestamp(updated_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN updated_at SET DEFAULT now()';
  END IF;

  -- sessions.created_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'sessions'
               AND column_name = 'created_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN created_at TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN created_at SET DEFAULT now()';
  END IF;

  -- sessions.expires_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'sessions'
               AND column_name = 'expires_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN expires_at TYPE TIMESTAMPTZ USING to_timestamp(expires_at::double precision / 1e9)';
  END IF;

  -- sessions.detached_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'sessions'
               AND column_name = 'detached_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN detached_at TYPE TIMESTAMPTZ USING to_timestamp(detached_at::double precision / 1e9)';
  END IF;

  -- password_resets.created_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'password_resets'
               AND column_name = 'created_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE password_resets ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE password_resets ALTER COLUMN created_at TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE password_resets ALTER COLUMN created_at SET DEFAULT NOW()';
  END IF;

  -- password_resets.expires_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'password_resets'
               AND column_name = 'expires_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE password_resets ALTER COLUMN expires_at TYPE TIMESTAMPTZ USING to_timestamp(expires_at::double precision / 1e9)';
  END IF;

  -- password_resets.used_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'password_resets'
               AND column_name = 'used_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE password_resets ALTER COLUMN used_at TYPE TIMESTAMPTZ USING to_timestamp(used_at::double precision / 1e9)';
  END IF;

  -- players.updated_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'players'
               AND column_name = 'updated_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE players ALTER COLUMN updated_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE players ALTER COLUMN updated_at TYPE TIMESTAMPTZ USING to_timestamp(updated_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE players ALTER COLUMN updated_at SET DEFAULT NOW()';
  END IF;

  -- players.created_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'players'
               AND column_name = 'created_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE players ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE players ALTER COLUMN created_at TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE players ALTER COLUMN created_at SET DEFAULT NOW()';
  END IF;

  -- players.locked_until
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'players'
               AND column_name = 'locked_until' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE players ALTER COLUMN locked_until TYPE TIMESTAMPTZ USING to_timestamp(locked_until::double precision / 1e9)';
  END IF;
END $$;
