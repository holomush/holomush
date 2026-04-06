-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

CREATE TABLE IF NOT EXISTS channels (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL DEFAULT 'public',
    description TEXT NOT NULL DEFAULT '',
    owner_id    TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at TIMESTAMPTZ,
    CONSTRAINT channels_type_check CHECK (type IN ('public', 'private', 'admin')),
    CONSTRAINT channels_name_format CHECK (name ~ '^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$')
);

CREATE UNIQUE INDEX IF NOT EXISTS channels_name_unique ON channels (lower(name));

CREATE TABLE IF NOT EXISTS channel_memberships (
    channel_id  TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    player_id   TEXT NOT NULL,
    role        TEXT NOT NULL DEFAULT 'member',
    joined_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    muted_until TIMESTAMPTZ,
    banned      BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY (channel_id, player_id),
    CONSTRAINT membership_role_check CHECK (role IN ('owner', 'op', 'member'))
);

CREATE TABLE IF NOT EXISTS channel_gags (
    channel_id   TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    character_id TEXT NOT NULL,
    gagged       BOOLEAN NOT NULL DEFAULT true,
    PRIMARY KEY (channel_id, character_id)
);

CREATE TABLE IF NOT EXISTS channel_messages (
    id          TEXT PRIMARY KEY,
    channel_id  TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    author_id   TEXT NOT NULL,
    author_name TEXT NOT NULL,
    message     TEXT NOT NULL,
    event_type  TEXT NOT NULL DEFAULT 'channel_say',
    source      TEXT NOT NULL DEFAULT 'game',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_channel_messages_history
    ON channel_messages (channel_id, created_at DESC);
