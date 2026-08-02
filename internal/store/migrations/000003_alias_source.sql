-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- +goose Up

-- Add source column to track alias provenance (plugin name, "sysalias", etc.).
-- Separate from created_by which is FK to players for human-created aliases.
ALTER TABLE system_aliases ADD COLUMN IF NOT EXISTS source TEXT;

COMMENT ON COLUMN system_aliases.source IS 'Origin of the alias: plugin name for manifest-seeded, sysalias for operator-created';

-- +goose Down

ALTER TABLE system_aliases DROP COLUMN IF EXISTS source;
