-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- +goose Up

ALTER TABLE sessions ADD COLUMN IF NOT EXISTS player_id TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE sessions DROP COLUMN IF EXISTS player_id;
