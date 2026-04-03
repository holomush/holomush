-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- ═══════════════════════════════════════════════════════════════════════════════
-- HoloMUSH 0.1 Baseline Schema
-- ═══════════════════════════════════════════════════════════════════════════════
-- Single baseline migration replacing 24 incremental migrations.
-- No upgrade path from pre-0.1 databases.
-- ═══════════════════════════════════════════════════════════════════════════════


-- ═══ Extensions ═══

-- Fuzzy text matching for exit names, object names, etc.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Query performance monitoring (requires shared_preload_libraries).
DO $$
BEGIN
    CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
EXCEPTION
    WHEN undefined_file THEN
        RAISE NOTICE 'pg_stat_statements not available (shared_preload_libraries not configured)';
    WHEN OTHERS THEN
        RAISE NOTICE 'Could not enable pg_stat_statements: %', SQLERRM;
END
$$;


-- ═══ Core Infrastructure ═══

-- Key-value configuration store (e.g., game_id).
CREATE TABLE holomush_system_info (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Tracks one-time bootstrap state (active setting, schema version, etc.).
CREATE TABLE bootstrap_metadata (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
COMMENT ON TABLE bootstrap_metadata IS 'Tracks one-time bootstrap state (active setting, schema version, etc.)';


-- ═══ Players & Characters ═══

-- Player accounts.
CREATE TABLE players (
    id                   TEXT PRIMARY KEY,
    username             TEXT UNIQUE NOT NULL,
    password_hash        TEXT NOT NULL,
    email                TEXT,
    email_verified       BOOLEAN NOT NULL DEFAULT FALSE,
    failed_attempts      INTEGER NOT NULL DEFAULT 0,
    locked_until         TIMESTAMPTZ,
    default_character_id TEXT,
    preferences          JSONB NOT NULL DEFAULT '{}',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_players_email ON players(email) WHERE email IS NOT NULL;

-- In-game entities controlled by players.
CREATE TABLE characters (
    id          TEXT PRIMARY KEY,
    player_id   TEXT REFERENCES players(id),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    location_id TEXT,  -- nullable: character may not be in the world yet
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_characters_location ON characters(location_id);

-- FK for default_character_id (deferred to avoid circular dependency).
ALTER TABLE players ADD CONSTRAINT fk_players_default_character
    FOREIGN KEY (default_character_id) REFERENCES characters(id) ON DELETE SET NULL;

-- Character-to-role mapping for RBAC.
CREATE TABLE character_roles (
    character_id TEXT NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    role         TEXT NOT NULL,
    PRIMARY KEY (character_id, role)
);


-- ═══ World Model ═══

-- Locations (rooms/areas) that contain objects and characters.
CREATE TABLE locations (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    description   TEXT NOT NULL,
    type          TEXT NOT NULL DEFAULT 'persistent',
    shadows_id    TEXT REFERENCES locations(id),
    owner_id      TEXT REFERENCES characters(id),
    replay_policy TEXT NOT NULL DEFAULT 'last:0',
    archived_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_locations_type ON locations(type);
CREATE INDEX idx_locations_shadows ON locations(shadows_id) WHERE shadows_id IS NOT NULL;
CREATE INDEX idx_locations_name_trgm ON locations USING gin (name gin_trgm_ops);

-- Add FK from characters.location_id to locations now that locations exist.
ALTER TABLE characters ADD CONSTRAINT fk_characters_location
    FOREIGN KEY (location_id) REFERENCES locations(id);

-- Connections between locations (with optional locks).
CREATE TABLE exits (
    id               TEXT PRIMARY KEY,
    from_location_id TEXT NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
    to_location_id   TEXT NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    aliases          TEXT[] DEFAULT '{}',
    bidirectional    BOOLEAN NOT NULL DEFAULT TRUE,
    return_name      TEXT,
    visibility       TEXT NOT NULL DEFAULT 'all',
    visible_to       TEXT[] DEFAULT '{}',
    locked           BOOLEAN NOT NULL DEFAULT FALSE,
    lock_type        TEXT,
    lock_data        JSONB,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(from_location_id, name),
    CONSTRAINT chk_not_self_referential CHECK (from_location_id != to_location_id)
);
CREATE INDEX idx_exits_from ON exits(from_location_id);
CREATE INDEX idx_exits_to ON exits(to_location_id);
CREATE INDEX idx_exits_name_trgm ON exits USING gin (name gin_trgm_ops);

-- Physical objects in the world.
CREATE TABLE objects (
    id                    TEXT PRIMARY KEY,
    name                  TEXT NOT NULL,
    description           TEXT NOT NULL,
    location_id           TEXT REFERENCES locations(id) ON DELETE SET NULL,
    held_by_character_id  TEXT REFERENCES characters(id) ON DELETE SET NULL,
    contained_in_object_id TEXT REFERENCES objects(id) ON DELETE SET NULL,
    is_container          BOOLEAN NOT NULL DEFAULT FALSE,
    owner_id              TEXT REFERENCES characters(id),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_not_self_contained CHECK (contained_in_object_id IS NULL OR contained_in_object_id != id),
    CONSTRAINT chk_exactly_one_containment CHECK (
        (CASE WHEN location_id IS NOT NULL THEN 1 ELSE 0 END +
         CASE WHEN held_by_character_id IS NOT NULL THEN 1 ELSE 0 END +
         CASE WHEN contained_in_object_id IS NOT NULL THEN 1 ELSE 0 END) = 1
    )
);
CREATE INDEX idx_objects_location ON objects(location_id) WHERE location_id IS NOT NULL;
CREATE INDEX idx_objects_held_by ON objects(held_by_character_id) WHERE held_by_character_id IS NOT NULL;
CREATE INDEX idx_objects_contained ON objects(contained_in_object_id) WHERE contained_in_object_id IS NOT NULL;
CREATE INDEX idx_objects_name_trgm ON objects USING gin (name gin_trgm_ops);

-- RP scene participants.
CREATE TABLE scene_participants (
    scene_id     TEXT NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
    character_id TEXT NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    role         TEXT NOT NULL DEFAULT 'member',
    joined_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (scene_id, character_id)
);
CREATE INDEX idx_scene_participants_character ON scene_participants(character_id);


-- ═══ Authentication & Sessions ═══

-- Unified player sessions (replaces legacy web_sessions and player_tokens).
CREATE TABLE player_sessions (
    id         TEXT PRIMARY KEY,
    player_id  TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    user_agent TEXT NOT NULL DEFAULT '',
    ip_address TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_player_sessions_token_hash ON player_sessions (token_hash);
CREATE INDEX idx_player_sessions_player ON player_sessions (player_id);
CREATE INDEX idx_player_sessions_expires ON player_sessions (expires_at);

-- Hashed tokens for secure password recovery.
CREATE TABLE password_resets (
    id         TEXT PRIMARY KEY,
    player_id  TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_password_resets_player ON password_resets(player_id);
CREATE INDEX idx_password_resets_expires ON password_resets(expires_at);
CREATE UNIQUE INDEX idx_password_resets_token_hash ON password_resets(token_hash);


-- ═══ Game Sessions ═══

-- Persistent game sessions that survive disconnects.
CREATE TABLE sessions (
    id             TEXT PRIMARY KEY,
    character_id   TEXT NOT NULL,
    character_name TEXT NOT NULL,
    location_id    TEXT NOT NULL,
    is_guest       BOOLEAN NOT NULL DEFAULT false,
    status         TEXT NOT NULL DEFAULT 'active',
    grid_present   BOOLEAN NOT NULL DEFAULT false,
    event_cursors  JSONB NOT NULL DEFAULT '{}',
    command_history TEXT[] NOT NULL DEFAULT '{}',
    ttl_seconds    INTEGER NOT NULL DEFAULT 1800,
    max_history    INTEGER NOT NULL DEFAULT 500,
    last_paged     TEXT NOT NULL DEFAULT '',
    last_whispered TEXT NOT NULL DEFAULT '',
    detached_at    TIMESTAMPTZ,
    expires_at     TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_sessions_active_character
    ON sessions (character_id) WHERE status IN ('active', 'detached');
CREATE INDEX idx_sessions_status ON sessions (status) WHERE status = 'detached';

-- Individual client connections to a session.
CREATE TABLE session_connections (
    id                TEXT PRIMARY KEY,
    session_id        TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    client_type       TEXT NOT NULL,
    streams           TEXT[] NOT NULL,
    player_session_id TEXT REFERENCES player_sessions(id) ON DELETE SET NULL,
    connected_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_session_connections_session ON session_connections (session_id);


-- ═══ Events ═══

-- Immutable event log for event sourcing.
CREATE TABLE events (
    id         TEXT PRIMARY KEY,
    stream     TEXT NOT NULL,
    type       TEXT NOT NULL,
    actor_kind SMALLINT NOT NULL,
    actor_id   TEXT NOT NULL,
    payload    JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_events_stream_id ON events (stream, id);


-- ═══ Access Control ═══

-- ABAC policy definitions.
CREATE TABLE access_policies (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    description  TEXT,
    effect       TEXT NOT NULL CHECK (effect IN ('permit', 'forbid')),
    source       TEXT NOT NULL DEFAULT 'admin'
                 CHECK (source IN ('seed', 'lock', 'admin', 'plugin')),
    dsl_text     TEXT NOT NULL,
    compiled_ast JSONB NOT NULL,
    enabled      BOOLEAN NOT NULL DEFAULT true,
    seed_version INTEGER DEFAULT NULL,
    created_by   TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    version      INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_policies_enabled ON access_policies(enabled) WHERE enabled = true;

-- Versioned history of policy changes.
CREATE TABLE access_policy_versions (
    id          TEXT PRIMARY KEY,
    policy_id   TEXT NOT NULL REFERENCES access_policies(id) ON DELETE CASCADE,
    version     INTEGER NOT NULL,
    dsl_text    TEXT NOT NULL,
    changed_by  TEXT NOT NULL,
    changed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    change_note TEXT,
    UNIQUE(policy_id, version)
);

-- Audit log for access decisions (partitioned by timestamp).
-- Partitions MUST be created at bootstrap before any writes.
CREATE TABLE access_audit_log (
    id               TEXT NOT NULL,
    timestamp        TIMESTAMPTZ NOT NULL DEFAULT now(),
    subject          TEXT NOT NULL,
    original_subject TEXT,
    action           TEXT NOT NULL,
    resource         TEXT NOT NULL,
    effect           TEXT NOT NULL CHECK (effect IN ('allow', 'deny', 'default_deny', 'system_bypass')),
    policy_id        TEXT,
    policy_name      TEXT,
    attributes       JSONB,
    error_message    TEXT,
    provider_errors  JSONB,
    duration_us      INTEGER,
    PRIMARY KEY (id, timestamp)
) PARTITION BY RANGE (timestamp);

CREATE INDEX idx_audit_log_timestamp ON access_audit_log USING BRIN (timestamp)
    WITH (pages_per_range = 128);
CREATE INDEX idx_audit_log_subject ON access_audit_log(subject, timestamp DESC);
CREATE INDEX idx_audit_log_denied ON access_audit_log(effect, timestamp DESC)
    WHERE effect IN ('deny', 'default_deny');


-- ═══ Content & Configuration ═══

-- General-purpose content store for managed game content.
CREATE TABLE content_items (
    key           TEXT PRIMARY KEY,
    content_type  TEXT NOT NULL DEFAULT 'text/markdown',
    body          BYTEA NOT NULL,
    metadata      JSONB NOT NULL DEFAULT '{}',
    search_vector TSVECTOR,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_content_items_prefix ON content_items (key text_pattern_ops);
CREATE INDEX idx_content_items_search ON content_items USING GIN (search_vector);
COMMENT ON TABLE content_items IS 'General-purpose content store for managed game content';

-- System-wide command aliases (admin-managed).
CREATE TABLE system_aliases (
    alias      TEXT PRIMARY KEY,
    command    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by TEXT REFERENCES players(id)
);
COMMENT ON TABLE system_aliases IS 'System-wide command aliases managed by administrators';

-- Per-player command aliases.
CREATE TABLE player_aliases (
    player_id  TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    alias      TEXT NOT NULL,
    command    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (player_id, alias)
);
CREATE INDEX idx_player_aliases_player_id ON player_aliases(player_id);
COMMENT ON TABLE player_aliases IS 'Per-player command aliases';


-- ═══ Entity Properties ═══

-- Extensible property system for any entity type.
CREATE TABLE entity_properties (
    id            TEXT PRIMARY KEY,
    parent_type   TEXT NOT NULL,
    parent_id     TEXT NOT NULL,
    name          TEXT NOT NULL,
    value         TEXT,
    owner         TEXT,
    visibility    TEXT NOT NULL DEFAULT 'public'
                  CHECK (visibility IN ('public', 'private', 'restricted', 'system', 'admin')),
    flags         JSONB DEFAULT '[]',
    visible_to    JSONB DEFAULT NULL,
    excluded_from JSONB DEFAULT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT entity_properties_parent_name_unique UNIQUE(parent_type, parent_id, name),
    CONSTRAINT visibility_restricted_requires_lists
        CHECK (visibility != 'restricted'
            OR (visible_to IS NOT NULL AND excluded_from IS NOT NULL)),
    CONSTRAINT visibility_non_restricted_nulls_lists
        CHECK (visibility = 'restricted'
            OR (visible_to IS NULL AND excluded_from IS NULL))
);
CREATE INDEX idx_entity_properties_parent ON entity_properties(parent_type, parent_id);
CREATE INDEX idx_properties_owner ON entity_properties(owner) WHERE owner IS NOT NULL;


-- ═══ Seed Data ═══

-- System aliases
INSERT INTO system_aliases (alias, command)
VALUES ('tel', 'teleport'),
       ('ex', 'examine')
ON CONFLICT (alias) DO NOTHING;

-- Bootstrap: test player, location, and character
INSERT INTO players (id, username, password_hash)
VALUES ('01KDVDNA00041061050R3GG28A', 'testuser', '$2a$10$N9qo8uLOickgx2ZMRZoMye')
ON CONFLICT (id) DO NOTHING;

INSERT INTO locations (id, name, description)
VALUES ('01KDVDNA001C60T3GF208H44RM', 'The Void', 'An empty expanse of nothing. This is where it all begins.')
ON CONFLICT (id) DO NOTHING;

INSERT INTO characters (id, player_id, name, location_id)
VALUES ('01KDVDNA002MB1E60S38DHR78Y', '01KDVDNA00041061050R3GG28A', 'TestChar', '01KDVDNA001C60T3GF208H44RM')
ON CONFLICT (id) DO NOTHING;
