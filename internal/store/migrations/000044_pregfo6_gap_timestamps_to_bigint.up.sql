-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Convert pre-gfo6 gap timestamp columns from TIMESTAMPTZ to BIGINT
-- (epoch nanoseconds, UTC). INV-STORE-1. These tables were missed in the
-- Phases 1–4 sweep and surfaced by the INV-STORE-1 meta-test (holomush-gfo6.22).
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
  -- ═══ bootstrap_metadata ═══
  -- initialized_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'bootstrap_metadata'
               AND column_name = 'initialized_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE bootstrap_metadata ALTER COLUMN initialized_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE bootstrap_metadata ALTER COLUMN initialized_at TYPE BIGINT USING CASE WHEN initialized_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM initialized_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE bootstrap_metadata ALTER COLUMN initialized_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- ═══ crypto_rekey_checkpoints ═══
  -- started_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'crypto_rekey_checkpoints'
               AND column_name = 'started_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE crypto_rekey_checkpoints ALTER COLUMN started_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE crypto_rekey_checkpoints ALTER COLUMN started_at TYPE BIGINT USING CASE WHEN started_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM started_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE crypto_rekey_checkpoints ALTER COLUMN started_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- last_heartbeat_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'crypto_rekey_checkpoints'
               AND column_name = 'last_heartbeat_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE crypto_rekey_checkpoints ALTER COLUMN last_heartbeat_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE crypto_rekey_checkpoints ALTER COLUMN last_heartbeat_at TYPE BIGINT USING CASE WHEN last_heartbeat_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM last_heartbeat_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE crypto_rekey_checkpoints ALTER COLUMN last_heartbeat_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- completed_at → BIGINT (nullable, no DEFAULT)
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'crypto_rekey_checkpoints'
               AND column_name = 'completed_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE crypto_rekey_checkpoints ALTER COLUMN completed_at TYPE BIGINT USING CASE WHEN completed_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM completed_at) * 1000000000))::bigint END';
  END IF;

  -- aborted_at → BIGINT (nullable, no DEFAULT)
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'crypto_rekey_checkpoints'
               AND column_name = 'aborted_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE crypto_rekey_checkpoints ALTER COLUMN aborted_at TYPE BIGINT USING CASE WHEN aborted_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM aborted_at) * 1000000000))::bigint END';
  END IF;

  -- ═══ holomush_system_info ═══
  -- created_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'holomush_system_info'
               AND column_name = 'created_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE holomush_system_info ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE holomush_system_info ALTER COLUMN created_at TYPE BIGINT USING CASE WHEN created_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM created_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE holomush_system_info ALTER COLUMN created_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- updated_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'holomush_system_info'
               AND column_name = 'updated_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE holomush_system_info ALTER COLUMN updated_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE holomush_system_info ALTER COLUMN updated_at TYPE BIGINT USING CASE WHEN updated_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM updated_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE holomush_system_info ALTER COLUMN updated_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- ═══ setting_bootstrap_state ═══
  -- updated_at: DROP DEFAULT, TYPE, SET DEFAULT
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'setting_bootstrap_state'
               AND column_name = 'updated_at' AND data_type = 'timestamp with time zone') THEN
    EXECUTE 'ALTER TABLE setting_bootstrap_state ALTER COLUMN updated_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE setting_bootstrap_state ALTER COLUMN updated_at TYPE BIGINT USING CASE WHEN updated_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM updated_at) * 1000000000))::bigint END';
    EXECUTE 'ALTER TABLE setting_bootstrap_state ALTER COLUMN updated_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;
END $$;
