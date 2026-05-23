-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Revert world-domain timestamp columns from BIGINT (epoch nanoseconds) back
-- to TIMESTAMPTZ. PRECISION LOSS: sub-microsecond nanoseconds are discarded
-- by to_timestamp(col::double precision / 1e9).

-- player_character_bindings

ALTER TABLE player_character_bindings
    ALTER COLUMN ended_at
        TYPE TIMESTAMPTZ USING to_timestamp(ended_at::double precision / 1e9);

ALTER TABLE player_character_bindings
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE player_character_bindings
    ALTER COLUMN created_at
        TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9);
ALTER TABLE player_character_bindings
    ALTER COLUMN created_at
        SET DEFAULT now();

-- scene_participants

ALTER TABLE scene_participants
    ALTER COLUMN joined_at DROP DEFAULT;
ALTER TABLE scene_participants
    ALTER COLUMN joined_at
        TYPE TIMESTAMPTZ USING to_timestamp(joined_at::double precision / 1e9);
ALTER TABLE scene_participants
    ALTER COLUMN joined_at
        SET DEFAULT NOW();

-- entity_properties

ALTER TABLE entity_properties
    ALTER COLUMN updated_at DROP DEFAULT;
ALTER TABLE entity_properties
    ALTER COLUMN updated_at
        TYPE TIMESTAMPTZ USING to_timestamp(updated_at::double precision / 1e9);
ALTER TABLE entity_properties
    ALTER COLUMN updated_at
        SET DEFAULT now();

ALTER TABLE entity_properties
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE entity_properties
    ALTER COLUMN created_at
        TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9);
ALTER TABLE entity_properties
    ALTER COLUMN created_at
        SET DEFAULT now();

-- objects

ALTER TABLE objects
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE objects
    ALTER COLUMN created_at
        TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9);
ALTER TABLE objects
    ALTER COLUMN created_at
        SET DEFAULT NOW();

-- exits

ALTER TABLE exits
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE exits
    ALTER COLUMN created_at
        TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9);
ALTER TABLE exits
    ALTER COLUMN created_at
        SET DEFAULT NOW();

-- locations

ALTER TABLE locations
    ALTER COLUMN archived_at
        TYPE TIMESTAMPTZ USING to_timestamp(archived_at::double precision / 1e9);

ALTER TABLE locations
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE locations
    ALTER COLUMN created_at
        TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9);
ALTER TABLE locations
    ALTER COLUMN created_at
        SET DEFAULT NOW();

-- characters

ALTER TABLE characters
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE characters
    ALTER COLUMN created_at
        TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9);
ALTER TABLE characters
    ALTER COLUMN created_at
        SET DEFAULT NOW();
