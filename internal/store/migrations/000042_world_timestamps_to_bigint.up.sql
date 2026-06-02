-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Convert world-domain timestamp columns from TIMESTAMPTZ to BIGINT
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
  -- characters.created_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'characters'
               AND column_name = 'created_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE characters ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE characters ALTER COLUMN created_at TYPE BIGINT USING CASE WHEN created_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM created_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE characters ALTER COLUMN created_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- locations.created_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'locations'
               AND column_name = 'created_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE locations ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE locations ALTER COLUMN created_at TYPE BIGINT USING CASE WHEN created_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM created_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE locations ALTER COLUMN created_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- locations.archived_at → BIGINT (nullable, no DEFAULT)
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'locations'
               AND column_name = 'archived_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE locations ALTER COLUMN archived_at TYPE BIGINT USING CASE WHEN archived_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM archived_at) * 1000000000))::bigint END';
  END IF;

  -- exits.created_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'exits'
               AND column_name = 'created_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE exits ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE exits ALTER COLUMN created_at TYPE BIGINT USING CASE WHEN created_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM created_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE exits ALTER COLUMN created_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- objects.created_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'objects'
               AND column_name = 'created_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE objects ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE objects ALTER COLUMN created_at TYPE BIGINT USING CASE WHEN created_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM created_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE objects ALTER COLUMN created_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- entity_properties.created_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'entity_properties'
               AND column_name = 'created_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE entity_properties ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE entity_properties ALTER COLUMN created_at TYPE BIGINT USING CASE WHEN created_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM created_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE entity_properties ALTER COLUMN created_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- entity_properties.updated_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'entity_properties'
               AND column_name = 'updated_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE entity_properties ALTER COLUMN updated_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE entity_properties ALTER COLUMN updated_at TYPE BIGINT USING CASE WHEN updated_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM updated_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE entity_properties ALTER COLUMN updated_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- scene_participants.joined_at: DROP DEFAULT, TYPE, SET DEFAULT (host-side
  -- table; plugin-side was migrated in Phase 1).
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'scene_participants'
               AND column_name = 'joined_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE scene_participants ALTER COLUMN joined_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE scene_participants ALTER COLUMN joined_at TYPE BIGINT USING CASE WHEN joined_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM joined_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE scene_participants ALTER COLUMN joined_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- player_character_bindings.created_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_character_bindings'
               AND column_name = 'created_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE player_character_bindings ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE player_character_bindings ALTER COLUMN created_at TYPE BIGINT USING CASE WHEN created_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM created_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE player_character_bindings ALTER COLUMN created_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- player_character_bindings.ended_at → BIGINT (nullable, no DEFAULT)
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_character_bindings'
               AND column_name = 'ended_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE player_character_bindings ALTER COLUMN ended_at TYPE BIGINT USING CASE WHEN ended_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM ended_at) * 1000000000))::bigint END';
  END IF;
END $$;
