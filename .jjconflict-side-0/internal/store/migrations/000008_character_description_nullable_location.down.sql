-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Revert: Make location_id required again
ALTER TABLE characters ALTER COLUMN location_id SET NOT NULL;

-- Revert: Remove description column
ALTER TABLE characters DROP COLUMN IF EXISTS description;
