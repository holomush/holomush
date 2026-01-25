-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Extend locations table with world model fields
ALTER TABLE locations ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT 'persistent';
ALTER TABLE locations ADD COLUMN IF NOT EXISTS shadows_id TEXT REFERENCES locations(id);
ALTER TABLE locations ADD COLUMN IF NOT EXISTS owner_id TEXT REFERENCES characters(id);
ALTER TABLE locations ADD COLUMN IF NOT EXISTS replay_policy TEXT NOT NULL DEFAULT 'last:0';
ALTER TABLE locations ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_locations_type ON locations(type);
CREATE INDEX IF NOT EXISTS idx_locations_shadows ON locations(shadows_id) WHERE shadows_id IS NOT NULL;

-- Exits table
CREATE TABLE IF NOT EXISTS exits (
    id TEXT PRIMARY KEY,
    from_location_id TEXT NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
    to_location_id TEXT NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    aliases TEXT[] DEFAULT '{}',
    bidirectional BOOLEAN NOT NULL DEFAULT TRUE,
    return_name TEXT,
    visibility TEXT NOT NULL DEFAULT 'all',
    visible_to TEXT[] DEFAULT '{}',
    locked BOOLEAN NOT NULL DEFAULT FALSE,
    lock_type TEXT,
    lock_data JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(from_location_id, name)
);
CREATE INDEX IF NOT EXISTS idx_exits_from ON exits(from_location_id);
CREATE INDEX IF NOT EXISTS idx_exits_to ON exits(to_location_id);

-- Objects table
CREATE TABLE IF NOT EXISTS objects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL,
    location_id TEXT REFERENCES locations(id) ON DELETE SET NULL,
    held_by_character_id TEXT REFERENCES characters(id) ON DELETE SET NULL,
    contained_in_object_id TEXT REFERENCES objects(id) ON DELETE SET NULL,
    is_container BOOLEAN NOT NULL DEFAULT FALSE,
    owner_id TEXT REFERENCES characters(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_not_self_contained CHECK (contained_in_object_id IS NULL OR contained_in_object_id != id)
);
CREATE INDEX IF NOT EXISTS idx_objects_location ON objects(location_id) WHERE location_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_objects_held_by ON objects(held_by_character_id) WHERE held_by_character_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_objects_contained ON objects(contained_in_object_id) WHERE contained_in_object_id IS NOT NULL;

-- Scene participants
CREATE TABLE IF NOT EXISTS scene_participants (
    scene_id TEXT NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
    character_id TEXT NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'member',
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (scene_id, character_id)
);
CREATE INDEX IF NOT EXISTS idx_scene_participants_character ON scene_participants(character_id);

-- Add index for character location lookups (column already exists from migration 001)
CREATE INDEX IF NOT EXISTS idx_characters_location ON characters(location_id);
