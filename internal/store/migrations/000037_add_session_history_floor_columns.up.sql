-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

ALTER TABLE sessions
    ADD COLUMN IF NOT EXISTS location_arrived_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS guest_character_created_at TIMESTAMPTZ NOT NULL DEFAULT 'epoch';
