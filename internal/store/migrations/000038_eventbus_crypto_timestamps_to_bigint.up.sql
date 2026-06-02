-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Convert events_audit and crypto_keys timestamp columns from TIMESTAMPTZ
-- to BIGINT (epoch nanoseconds, UTC). See:
--   docs/superpowers/specs/2026-05-22-nanosecond-timestamps-design.md
-- INV-STORE-1, INV-STORE-4, INV-STORE-5.
--
-- Idempotent: each ALTER COLUMN ... TYPE step is wrapped in a DO block that
-- guards on information_schema.columns.data_type, so re-running this migration
-- (recovery replays, partial-apply retries) is safe. Pattern mirrors
-- 000017_events_audit_envelope_rename.up.sql.
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
  -- events_audit.inserted_at — drop default (was TIMESTAMPTZ DEFAULT now())
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'events_audit'
      AND column_name = 'inserted_at'
      AND data_type = 'timestamp with time zone'
  ) THEN
    EXECUTE 'ALTER TABLE events_audit ALTER COLUMN inserted_at DROP DEFAULT';
  END IF;

  -- events_audit.timestamp → BIGINT
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'events_audit'
      AND column_name = 'timestamp'
      AND data_type = 'timestamp with time zone'
  ) THEN
    EXECUTE 'ALTER TABLE events_audit ALTER COLUMN timestamp TYPE BIGINT USING CASE WHEN timestamp IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM timestamp) * 1000000000))::bigint END';
  END IF;

  -- events_audit.inserted_at → BIGINT
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'events_audit'
      AND column_name = 'inserted_at'
      AND data_type = 'timestamp with time zone'
  ) THEN
    EXECUTE 'ALTER TABLE events_audit ALTER COLUMN inserted_at TYPE BIGINT USING CASE WHEN inserted_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM inserted_at) * 1000000000))::bigint END';
  END IF;

  -- events_audit.inserted_at — new BIGINT default; only set if no default present
  IF NOT EXISTS (
    SELECT 1 FROM pg_attrdef d
    JOIN pg_class c ON c.oid = d.adrelid
    JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = d.adnum
    WHERE c.relname = 'events_audit' AND a.attname = 'inserted_at'
  ) THEN
    EXECUTE 'ALTER TABLE events_audit ALTER COLUMN inserted_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;

  -- crypto_keys.created_at — drop default
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'crypto_keys'
      AND column_name = 'created_at'
      AND data_type = 'timestamp with time zone'
  ) THEN
    EXECUTE 'ALTER TABLE crypto_keys ALTER COLUMN created_at DROP DEFAULT';
  END IF;

  -- crypto_keys.created_at → BIGINT
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'crypto_keys'
      AND column_name = 'created_at'
      AND data_type = 'timestamp with time zone'
  ) THEN
    EXECUTE 'ALTER TABLE crypto_keys ALTER COLUMN created_at TYPE BIGINT USING CASE WHEN created_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM created_at) * 1000000000))::bigint END';
  END IF;

  -- crypto_keys.rotated_at → BIGINT (nullable, no default)
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'crypto_keys'
      AND column_name = 'rotated_at'
      AND data_type = 'timestamp with time zone'
  ) THEN
    EXECUTE 'ALTER TABLE crypto_keys ALTER COLUMN rotated_at TYPE BIGINT USING CASE WHEN rotated_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM rotated_at) * 1000000000))::bigint END';
  END IF;

  -- crypto_keys.destroyed_at → BIGINT (nullable, no default)
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'crypto_keys'
      AND column_name = 'destroyed_at'
      AND data_type = 'timestamp with time zone'
  ) THEN
    EXECUTE 'ALTER TABLE crypto_keys ALTER COLUMN destroyed_at TYPE BIGINT USING CASE WHEN destroyed_at IS NULL THEN NULL ELSE GREATEST((-9223372036854775808)::numeric, LEAST(9223372036854775807::numeric, EXTRACT(EPOCH FROM destroyed_at) * 1000000000))::bigint END';
  END IF;

  -- crypto_keys.created_at — new BIGINT default
  IF NOT EXISTS (
    SELECT 1 FROM pg_attrdef d
    JOIN pg_class c ON c.oid = d.adrelid
    JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = d.adnum
    WHERE c.relname = 'crypto_keys' AND a.attname = 'created_at'
  ) THEN
    EXECUTE 'ALTER TABLE crypto_keys ALTER COLUMN created_at SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT';
  END IF;
END $$;
