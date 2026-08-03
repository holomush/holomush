-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- +goose Up

ALTER TABLE sessions
    ADD COLUMN IF NOT EXISTS location_arrived_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS guest_character_created_at TIMESTAMPTZ NOT NULL DEFAULT 'epoch';

-- +goose Down

ALTER TABLE sessions
    DROP COLUMN IF EXISTS location_arrived_at,
    DROP COLUMN IF EXISTS guest_character_created_at;
