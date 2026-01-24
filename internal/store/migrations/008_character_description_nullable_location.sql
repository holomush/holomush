-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Add description column to characters table
ALTER TABLE characters ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';

-- Make location_id nullable (characters may not be in the world yet)
ALTER TABLE characters ALTER COLUMN location_id DROP NOT NULL;
