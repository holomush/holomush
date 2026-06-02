-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Convert auth-domain timestamp columns from TIMESTAMPTZ to BIGINT
-- (epoch nanoseconds, UTC). INV-STORE-1.
--
-- Idempotent: each ALTER COLUMN ... TYPE step is wrapped in a DO block that
-- guards on information_schema.columns.data_type, so re-running this migration
-- (recovery replays, partial-apply retries) is safe. Pattern mirrors
-- 000038_eventbus_crypto_timestamps_to_bigint.up.sql.
--
-- Overflow-safe (INV-STORE-9): each TYPE USING clause converts in numeric and
-- clamps with GREATEST/LEAST to the int64-ns range, so pre-existing values
-- beyond ~[1678, 2262] or ±infinity saturate to the int64 bounds instead of
-- raising "bigint out of range" (SQLSTATE 22003). NULL is guarded explicitly
-- (LEAST/GREATEST ignore NULL inputs). SET DEFAULT keeps now()*1e9 — now()
-- cannot overflow. Backfills the gap that wedged the sandbox deploy
-- (holomush-0b3ec).

DO $$
BEGIN
  -- players.locked_until → BIGINT (nullable, no DEFAULT)
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'players'
               AND column_name = 'locked_until' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE players ALTER COLUMN locked_until TYPE BIGINT USING CASE WHEN locked_until IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM locked_until) * 1000000000))::bigint END';
  END IF;

  -- players.created_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'players'
               AND column_name = 'created_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE players ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE players ALTER COLUMN created_at TYPE BIGINT USING CASE WHEN created_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM created_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE players ALTER COLUMN created_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- players.updated_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'players'
               AND column_name = 'updated_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE players ALTER COLUMN updated_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE players ALTER COLUMN updated_at TYPE BIGINT USING CASE WHEN updated_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM updated_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE players ALTER COLUMN updated_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- password_resets.expires_at → BIGINT (nullable, no DEFAULT)
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'password_resets'
               AND column_name = 'expires_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE password_resets ALTER COLUMN expires_at TYPE BIGINT USING CASE WHEN expires_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM expires_at) * 1000000000))::bigint END';
  END IF;

  -- password_resets.used_at → BIGINT (nullable, no DEFAULT)
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'password_resets'
               AND column_name = 'used_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE password_resets ALTER COLUMN used_at TYPE BIGINT USING CASE WHEN used_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM used_at) * 1000000000))::bigint END';
  END IF;

  -- password_resets.created_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'password_resets'
               AND column_name = 'created_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE password_resets ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE password_resets ALTER COLUMN created_at TYPE BIGINT USING CASE WHEN created_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM created_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE password_resets ALTER COLUMN created_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- sessions.expires_at → BIGINT (nullable, no DEFAULT)
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'sessions'
               AND column_name = 'expires_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN expires_at TYPE BIGINT USING CASE WHEN expires_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM expires_at) * 1000000000))::bigint END';
  END IF;

  -- sessions.detached_at → BIGINT (nullable, no DEFAULT)
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'sessions'
               AND column_name = 'detached_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN detached_at TYPE BIGINT USING CASE WHEN detached_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM detached_at) * 1000000000))::bigint END';
  END IF;

  -- sessions.created_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'sessions'
               AND column_name = 'created_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN created_at TYPE BIGINT USING CASE WHEN created_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM created_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN created_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- sessions.updated_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'sessions'
               AND column_name = 'updated_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN updated_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN updated_at TYPE BIGINT USING CASE WHEN updated_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM updated_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN updated_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- session_connections.connected_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'session_connections'
               AND column_name = 'connected_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE session_connections ALTER COLUMN connected_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE session_connections ALTER COLUMN connected_at TYPE BIGINT USING CASE WHEN connected_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM connected_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE session_connections ALTER COLUMN connected_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- player_sessions.expires_at → BIGINT (NOT NULL, no DEFAULT)
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_sessions'
               AND column_name = 'expires_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE player_sessions ALTER COLUMN expires_at TYPE BIGINT USING CASE WHEN expires_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM expires_at) * 1000000000))::bigint END';
  END IF;

  -- player_sessions.created_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_sessions'
               AND column_name = 'created_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE player_sessions ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE player_sessions ALTER COLUMN created_at TYPE BIGINT USING CASE WHEN created_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM created_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE player_sessions ALTER COLUMN created_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- player_sessions.updated_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_sessions'
               AND column_name = 'updated_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE player_sessions ALTER COLUMN updated_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE player_sessions ALTER COLUMN updated_at TYPE BIGINT USING CASE WHEN updated_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM updated_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE player_sessions ALTER COLUMN updated_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- Session history floor columns (added in migration 000037; pre-existed at
  -- gfo6 time and migrated atomically here so session_store.go::Set() does not
  -- face mixed types).
  --
  -- sessions.location_arrived_at: DEFAULT NOW() → BIGINT default
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'sessions'
               AND column_name = 'location_arrived_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN location_arrived_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN location_arrived_at TYPE BIGINT USING CASE WHEN location_arrived_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM location_arrived_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN location_arrived_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- sessions.guest_character_created_at: DEFAULT 'epoch' → BIGINT 0 (epoch in ns)
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'sessions'
               AND column_name = 'guest_character_created_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN guest_character_created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN guest_character_created_at TYPE BIGINT USING CASE WHEN guest_character_created_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM guest_character_created_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE sessions ALTER COLUMN guest_character_created_at SET DEFAULT 0';
  END IF;
END $$;
