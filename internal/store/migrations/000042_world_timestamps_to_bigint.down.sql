-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Revert world-domain timestamp columns from BIGINT (epoch nanoseconds) back
-- to TIMESTAMPTZ. PRECISION LOSS: sub-microsecond nanoseconds are discarded
-- by to_timestamp(col::double precision / 1e9).
--
-- Idempotent: guarded on data_type = 'bigint' so re-running is safe.

DO $$
BEGIN
  -- player_character_bindings.ended_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_character_bindings'
               AND column_name = 'ended_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE player_character_bindings ALTER COLUMN ended_at TYPE TIMESTAMPTZ USING to_timestamp(ended_at::double precision / 1e9)';
  END IF;

  -- player_character_bindings.created_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'player_character_bindings'
               AND column_name = 'created_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE player_character_bindings ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE player_character_bindings ALTER COLUMN created_at TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE player_character_bindings ALTER COLUMN created_at SET DEFAULT now()';
  END IF;

  -- scene_participants.joined_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'scene_participants'
               AND column_name = 'joined_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE scene_participants ALTER COLUMN joined_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE scene_participants ALTER COLUMN joined_at TYPE TIMESTAMPTZ USING to_timestamp(joined_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE scene_participants ALTER COLUMN joined_at SET DEFAULT NOW()';
  END IF;

  -- entity_properties.updated_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'entity_properties'
               AND column_name = 'updated_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE entity_properties ALTER COLUMN updated_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE entity_properties ALTER COLUMN updated_at TYPE TIMESTAMPTZ USING to_timestamp(updated_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE entity_properties ALTER COLUMN updated_at SET DEFAULT now()';
  END IF;

  -- entity_properties.created_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'entity_properties'
               AND column_name = 'created_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE entity_properties ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE entity_properties ALTER COLUMN created_at TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE entity_properties ALTER COLUMN created_at SET DEFAULT now()';
  END IF;

  -- objects.created_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'objects'
               AND column_name = 'created_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE objects ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE objects ALTER COLUMN created_at TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE objects ALTER COLUMN created_at SET DEFAULT NOW()';
  END IF;

  -- exits.created_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'exits'
               AND column_name = 'created_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE exits ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE exits ALTER COLUMN created_at TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE exits ALTER COLUMN created_at SET DEFAULT NOW()';
  END IF;

  -- locations.archived_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'locations'
               AND column_name = 'archived_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE locations ALTER COLUMN archived_at TYPE TIMESTAMPTZ USING to_timestamp(archived_at::double precision / 1e9)';
  END IF;

  -- locations.created_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'locations'
               AND column_name = 'created_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE locations ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE locations ALTER COLUMN created_at TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE locations ALTER COLUMN created_at SET DEFAULT NOW()';
  END IF;

  -- characters.created_at
  IF EXISTS (SELECT 1 FROM information_schema.columns
             WHERE table_schema = 'public' AND table_name = 'characters'
               AND column_name = 'created_at' AND data_type = 'bigint') THEN
    EXECUTE 'ALTER TABLE characters ALTER COLUMN created_at DROP DEFAULT';
    EXECUTE 'ALTER TABLE characters ALTER COLUMN created_at TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9)';
    EXECUTE 'ALTER TABLE characters ALTER COLUMN created_at SET DEFAULT NOW()';
  END IF;
END $$;
