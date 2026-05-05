-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

CREATE TABLE IF NOT EXISTS plugins (
    id              BYTEA       PRIMARY KEY,
    name            TEXT        NOT NULL,
    display_name    TEXT        NOT NULL,
    version         TEXT        NOT NULL,
    manifest_hash   BYTEA       NOT NULL,
    content_hash    BYTEA,
    first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    gc_at           TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS plugins_name_active
    ON plugins(name)
    WHERE gc_at IS NULL;

-- Eliminate legacy plugin-actor events whose envelope blobs carry
-- Actor.legacy_id (string). Post-w9ml the proto field is gone, so old
-- envelopes cannot round-trip cleanly. Irreversible at the data layer.
TRUNCATE events_audit;
