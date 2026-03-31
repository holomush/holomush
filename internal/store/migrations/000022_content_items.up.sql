-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

CREATE TABLE IF NOT EXISTS content_items (
    key            TEXT PRIMARY KEY,
    content_type   TEXT NOT NULL DEFAULT 'text/markdown',
    body           BYTEA NOT NULL,
    metadata       JSONB NOT NULL DEFAULT '{}',
    search_vector  TSVECTOR,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_content_items_prefix
    ON content_items (key text_pattern_ops);

CREATE INDEX IF NOT EXISTS idx_content_items_search
    ON content_items USING GIN (search_vector);

COMMENT ON TABLE content_items IS 'General-purpose content store for managed game content';
