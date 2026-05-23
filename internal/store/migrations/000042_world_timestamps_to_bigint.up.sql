-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Convert world-domain timestamp columns from TIMESTAMPTZ to BIGINT
-- (epoch nanoseconds, UTC). INV-TS-1.

-- characters: created_at has DEFAULT NOW() and must have its default dropped
-- before the TYPE change (no implicit TIMESTAMPTZ→BIGINT cast in PostgreSQL).

ALTER TABLE characters
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE characters
    ALTER COLUMN created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM created_at) * 1e9)::BIGINT;
ALTER TABLE characters
    ALTER COLUMN created_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

-- locations: created_at has DEFAULT NOW() and must have its default dropped first.
-- archived_at is nullable with no DEFAULT; no drop/restore needed.

ALTER TABLE locations
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE locations
    ALTER COLUMN created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM created_at) * 1e9)::BIGINT;
ALTER TABLE locations
    ALTER COLUMN created_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE locations
    ALTER COLUMN archived_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM archived_at) * 1e9)::BIGINT;

-- exits: created_at has DEFAULT NOW() and must have its default dropped first.

ALTER TABLE exits
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE exits
    ALTER COLUMN created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM created_at) * 1e9)::BIGINT;
ALTER TABLE exits
    ALTER COLUMN created_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

-- objects: created_at has DEFAULT NOW() and must have its default dropped first.

ALTER TABLE objects
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE objects
    ALTER COLUMN created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM created_at) * 1e9)::BIGINT;
ALTER TABLE objects
    ALTER COLUMN created_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

-- entity_properties: created_at and updated_at both have DEFAULT now() and
-- must have their defaults dropped before the TYPE change.

ALTER TABLE entity_properties
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE entity_properties
    ALTER COLUMN created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM created_at) * 1e9)::BIGINT;
ALTER TABLE entity_properties
    ALTER COLUMN created_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE entity_properties
    ALTER COLUMN updated_at DROP DEFAULT;
ALTER TABLE entity_properties
    ALTER COLUMN updated_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM updated_at) * 1e9)::BIGINT;
ALTER TABLE entity_properties
    ALTER COLUMN updated_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

-- scene_participants: joined_at has DEFAULT NOW() and must have its default
-- dropped first. This is the host-side table; plugin-side was migrated in Phase 1.

ALTER TABLE scene_participants
    ALTER COLUMN joined_at DROP DEFAULT;
ALTER TABLE scene_participants
    ALTER COLUMN joined_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM joined_at) * 1e9)::BIGINT;
ALTER TABLE scene_participants
    ALTER COLUMN joined_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

-- player_character_bindings: created_at has DEFAULT now() and must have its
-- default dropped first. ended_at is nullable with no DEFAULT; no drop/restore needed.

ALTER TABLE player_character_bindings
    ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE player_character_bindings
    ALTER COLUMN created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM created_at) * 1e9)::BIGINT;
ALTER TABLE player_character_bindings
    ALTER COLUMN created_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE player_character_bindings
    ALTER COLUMN ended_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM ended_at) * 1e9)::BIGINT;
