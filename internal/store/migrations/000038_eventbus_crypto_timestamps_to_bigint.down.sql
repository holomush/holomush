-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- DOWN MIGRATION IS PRECISION-LOSSY. Recovers TIMESTAMPTZ semantics but
-- truncates ns → µs. No backfill of pre-down-migration data is provided.
--
-- Idempotent: each ALTER COLUMN ... TYPE step is wrapped in a DO block that
-- guards on information_schema.columns.data_type, so re-running this rollback
-- (recovery replays, partial-apply retries) is safe.

DO $$
BEGIN
  -- events_audit.inserted_at — drop BIGINT default
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'events_audit'
      AND column_name = 'inserted_at'
      AND data_type = 'bigint'
  ) THEN
    EXECUTE 'ALTER TABLE events_audit ALTER COLUMN inserted_at DROP DEFAULT';
  END IF;

  -- events_audit.timestamp → TIMESTAMPTZ
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'events_audit'
      AND column_name = 'timestamp'
      AND data_type = 'bigint'
  ) THEN
    EXECUTE 'ALTER TABLE events_audit ALTER COLUMN timestamp TYPE TIMESTAMPTZ USING to_timestamp(timestamp::double precision / 1e9)';
  END IF;

  -- events_audit.inserted_at → TIMESTAMPTZ
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'events_audit'
      AND column_name = 'inserted_at'
      AND data_type = 'bigint'
  ) THEN
    EXECUTE 'ALTER TABLE events_audit ALTER COLUMN inserted_at TYPE TIMESTAMPTZ USING to_timestamp(inserted_at::double precision / 1e9)';
  END IF;

  -- events_audit.inserted_at — restore TIMESTAMPTZ default
  IF NOT EXISTS (
    SELECT 1 FROM pg_attrdef d
    JOIN pg_class c ON c.oid = d.adrelid
    JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = d.adnum
    WHERE c.relname = 'events_audit' AND a.attname = 'inserted_at'
  ) THEN
    EXECUTE 'ALTER TABLE events_audit ALTER COLUMN inserted_at SET DEFAULT now()';
  END IF;

  -- crypto_keys.created_at — drop BIGINT default
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'crypto_keys'
      AND column_name = 'created_at'
      AND data_type = 'bigint'
  ) THEN
    EXECUTE 'ALTER TABLE crypto_keys ALTER COLUMN created_at DROP DEFAULT';
  END IF;

  -- crypto_keys.created_at → TIMESTAMPTZ
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'crypto_keys'
      AND column_name = 'created_at'
      AND data_type = 'bigint'
  ) THEN
    EXECUTE 'ALTER TABLE crypto_keys ALTER COLUMN created_at TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9)';
  END IF;

  -- crypto_keys.rotated_at → TIMESTAMPTZ
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'crypto_keys'
      AND column_name = 'rotated_at'
      AND data_type = 'bigint'
  ) THEN
    EXECUTE 'ALTER TABLE crypto_keys ALTER COLUMN rotated_at TYPE TIMESTAMPTZ USING to_timestamp(rotated_at::double precision / 1e9)';
  END IF;

  -- crypto_keys.destroyed_at → TIMESTAMPTZ
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'crypto_keys'
      AND column_name = 'destroyed_at'
      AND data_type = 'bigint'
  ) THEN
    EXECUTE 'ALTER TABLE crypto_keys ALTER COLUMN destroyed_at TYPE TIMESTAMPTZ USING to_timestamp(destroyed_at::double precision / 1e9)';
  END IF;

  -- crypto_keys.created_at — restore TIMESTAMPTZ default
  IF NOT EXISTS (
    SELECT 1 FROM pg_attrdef d
    JOIN pg_class c ON c.oid = d.adrelid
    JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = d.adnum
    WHERE c.relname = 'crypto_keys' AND a.attname = 'created_at'
  ) THEN
    EXECUTE 'ALTER TABLE crypto_keys ALTER COLUMN created_at SET DEFAULT now()';
  END IF;
END $$;
