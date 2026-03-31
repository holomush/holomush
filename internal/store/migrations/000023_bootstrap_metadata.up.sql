-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

CREATE TABLE IF NOT EXISTS bootstrap_metadata (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE bootstrap_metadata IS 'Tracks one-time bootstrap state (active setting, schema version, etc.)';
