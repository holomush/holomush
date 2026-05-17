-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

ALTER TABLE sessions
    DROP COLUMN IF EXISTS location_arrived_at,
    DROP COLUMN IF EXISTS guest_character_created_at;
