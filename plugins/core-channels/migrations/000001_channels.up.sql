-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Plan 01-03 schema: channels + channel_memberships + channel_ops_events.
--
-- Channels are location-INDEPENDENT (CHAN-01): there is deliberately NO
-- location_id / FK to locations. A channel is a persistent, named comms space
-- keyed only by its case-insensitive unique name.
--
-- Schema lives in the plugin's isolated search_path (plugin_core_channels),
-- so the unqualified table names resolve correctly.
--
-- Timestamps are BIGINT epoch-nanoseconds (INV-STORE-1), matching the rest of
-- the platform's storage convention (holomush-gfo6); the Go layer bridges via
-- the pgnanos scan/insert seam.

CREATE TABLE IF NOT EXISTS channels (
    id             TEXT        PRIMARY KEY,
    name           TEXT        NOT NULL,
    type           TEXT        NOT NULL DEFAULT 'public'
                       CHECK (type IN ('public', 'private', 'admin')),
    owner_id       TEXT        NOT NULL,
    -- Soft-archive flag: `channel delete` sets archived = true and NEVER
    -- hard-deletes the row (spec §specifics; store DeleteChannel).
    archived       BOOLEAN     NOT NULL DEFAULT false,
    -- Marks channels seeded by SeedDefaultChannels at plugin Init (D-01).
    -- ListDefaultChannels selects WHERE is_default = true — the seam 01-08
    -- unions into QuerySessionStreams for guest auto-join.
    is_default     BOOLEAN     NOT NULL DEFAULT false,
    -- Per-channel retention override in days (D-07); NULL = use the plugin
    -- config default (retention_window). Admin channels MAY be unlimited.
    retention_days INTEGER,
    created_at     BIGINT      NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
);

-- Case-insensitive uniqueness (T-01-10): "Public" and "public" collide. The
-- store's GetByName / CreateChannel / SeedDefaultChannels all key on lower(name).
CREATE UNIQUE INDEX IF NOT EXISTS idx_channels_lower_name ON channels (lower(name));
CREATE INDEX IF NOT EXISTS idx_channels_owner ON channels(owner_id);
CREATE INDEX IF NOT EXISTS idx_channels_is_default ON channels(is_default) WHERE is_default;

CREATE TABLE IF NOT EXISTS channel_memberships (
    channel_id   TEXT        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    character_id TEXT        NOT NULL,
    -- Role CHECK keeps 'op' as a DORMANT value per D-05 (lightest path): op/deop
    -- delegation is deferred this phase, but including 'op' in the CHECK now
    -- means adding the role later needs no migration. The Go channelRole type
    -- treats only 'owner'/'member' as usable; 'op' is reserved, never stamped.
    role         TEXT        NOT NULL DEFAULT 'member'
                     CHECK (role IN ('owner', 'member', 'op')),
    -- Membership is keyed per-character but the effect is player-level (D-02).
    muted        BOOLEAN     NOT NULL DEFAULT false,
    banned       BOOLEAN     NOT NULL DEFAULT false,
    -- joined_at is the history-floor boundary (D-07): history reads never cross
    -- a member's most-recent joined_at.
    joined_at    BIGINT      NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
    PRIMARY KEY (channel_id, character_id)
);

CREATE INDEX IF NOT EXISTS idx_channel_memberships_character
    ON channel_memberships(character_id);
CREATE INDEX IF NOT EXISTS idx_channel_memberships_channel_role
    ON channel_memberships(channel_id, role);

-- Defense-in-depth: at most one owner row per channel. Maintained by the
-- application layer; the partial unique index is the schema-level guarantee.
CREATE UNIQUE INDEX IF NOT EXISTS idx_channel_memberships_one_owner
    ON channel_memberships(channel_id)
    WHERE role = 'owner';

-- Append-only moderation/lifecycle journal (T-01-11): every join/leave/kick/
-- ban/mute/transfer/create/archive records a row. Mirrors scene_ops_events.
CREATE TABLE IF NOT EXISTS channel_ops_events (
    id          TEXT        PRIMARY KEY,
    channel_id  TEXT        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    kind        TEXT        NOT NULL CHECK (kind ~ '^[a-z]+\.[a-z_]+$'),
    actor_id    TEXT        NOT NULL,
    target_id   TEXT,
    payload     JSONB       NOT NULL DEFAULT '{}',
    occurred_at BIGINT      NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
);

CREATE INDEX IF NOT EXISTS idx_channel_ops_events_channel
    ON channel_ops_events(channel_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_channel_ops_events_target
    ON channel_ops_events(target_id, occurred_at DESC) WHERE target_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_channel_ops_events_kind
    ON channel_ops_events(channel_id, kind, occurred_at DESC);
