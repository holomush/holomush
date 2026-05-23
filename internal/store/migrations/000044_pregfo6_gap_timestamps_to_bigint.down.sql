-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Revert pre-gfo6 gap timestamp columns from BIGINT (epoch nanoseconds)
-- back to TIMESTAMPTZ. PRECISION LOSS: sub-microsecond nanoseconds are
-- discarded by to_timestamp(col::double precision / 1e9).
--
-- Idempotent: guarded on data_type = 'bigint' so re-running is safe.

DO $$
BEGIN
  -- ═══ setting_bootstrap_state ═══
  -- updated_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'setting_bootstrap_state'
               AND column_name = 'updated_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE setting_bootstrap_state ALTER COLUMN updated_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE setting_bootstrap_state ALTER COLUMN updated_at TYPE TIMESTAMPTZ USING to_timestamp(updated_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE setting_bootstrap_state ALTER COLUMN updated_at SET DEFAULT now()';
  END IF;

  -- ═══ holomush_system_info ═══
  -- updated_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'holomush_system_info'
               AND column_name = 'updated_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE holomush_system_info ALTER COLUMN updated_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE holomush_system_info ALTER COLUMN updated_at TYPE TIMESTAMPTZ USING to_timestamp(updated_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE holomush_system_info ALTER COLUMN updated_at SET DEFAULT NOW()';
  END IF;

  -- created_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'holomush_system_info'
               AND column_name = 'created_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE holomush_system_info ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE holomush_system_info ALTER COLUMN created_at TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE holomush_system_info ALTER COLUMN created_at SET DEFAULT NOW()';
  END IF;

  -- ═══ crypto_rekey_checkpoints ═══
  -- aborted_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'crypto_rekey_checkpoints'
               AND column_name = 'aborted_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE crypto_rekey_checkpoints ALTER COLUMN aborted_at TYPE TIMESTAMPTZ USING to_timestamp(aborted_at::double precision / 1e9)';
  END IF;

  -- completed_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'crypto_rekey_checkpoints'
               AND column_name = 'completed_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE crypto_rekey_checkpoints ALTER COLUMN completed_at TYPE TIMESTAMPTZ USING to_timestamp(completed_at::double precision / 1e9)';
  END IF;

  -- last_heartbeat_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'crypto_rekey_checkpoints'
               AND column_name = 'last_heartbeat_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE crypto_rekey_checkpoints ALTER COLUMN last_heartbeat_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE crypto_rekey_checkpoints ALTER COLUMN last_heartbeat_at TYPE TIMESTAMPTZ USING to_timestamp(last_heartbeat_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE crypto_rekey_checkpoints ALTER COLUMN last_heartbeat_at SET DEFAULT now()';
  END IF;

  -- started_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'crypto_rekey_checkpoints'
               AND column_name = 'started_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE crypto_rekey_checkpoints ALTER COLUMN started_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE crypto_rekey_checkpoints ALTER COLUMN started_at TYPE TIMESTAMPTZ USING to_timestamp(started_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE crypto_rekey_checkpoints ALTER COLUMN started_at SET DEFAULT now()';
  END IF;

  -- ═══ bootstrap_metadata ═══
  -- initialized_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'bootstrap_metadata'
               AND column_name = 'initialized_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE bootstrap_metadata ALTER COLUMN initialized_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE bootstrap_metadata ALTER COLUMN initialized_at TYPE TIMESTAMPTZ USING to_timestamp(initialized_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE bootstrap_metadata ALTER COLUMN initialized_at SET DEFAULT now()';
  END IF;
END $$;
