CREATE TABLE IF NOT EXISTS scenes (
    id               TEXT        PRIMARY KEY,
    title            TEXT        NOT NULL,
    description      TEXT        NOT NULL DEFAULT '',
    location_id      TEXT,
    owner_id         TEXT        NOT NULL,
    state            TEXT        NOT NULL DEFAULT 'active',
    pose_order       TEXT        NOT NULL DEFAULT 'free',
    visibility       TEXT        NOT NULL DEFAULT 'open',
    idle_timeout_secs INTEGER,
    template_id      TEXT,
    content_warnings TEXT[]      NOT NULL DEFAULT '{}',
    tags             TEXT[]      NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at         TIMESTAMPTZ,
    archived_at      TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS scene_participants (
    scene_id           TEXT        NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    character_id       TEXT        NOT NULL,
    role               TEXT        NOT NULL DEFAULT 'member',
    origin_location_id TEXT,
    joined_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    publish_vote       BOOLEAN,
    PRIMARY KEY (scene_id, character_id)
);

CREATE TABLE IF NOT EXISTS scene_templates (
    id               TEXT        PRIMARY KEY,
    owner_id         TEXT        NOT NULL,
    title            TEXT        NOT NULL,
    description      TEXT        NOT NULL DEFAULT '',
    location_id      TEXT,
    pose_order       TEXT        NOT NULL DEFAULT 'free',
    content_warnings TEXT[]      NOT NULL DEFAULT '{}',
    tags             TEXT[]      NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS scene_logs (
    id           TEXT        PRIMARY KEY,
    scene_id     TEXT        NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    title        TEXT        NOT NULL,
    content      TEXT        NOT NULL,
    participants TEXT[]      NOT NULL DEFAULT '{}',
    published_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_scenes_state         ON scenes(state);
CREATE INDEX IF NOT EXISTS idx_scenes_location      ON scenes(location_id) WHERE location_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_scene_participants_character ON scene_participants(character_id);
CREATE INDEX IF NOT EXISTS idx_scene_logs_scene     ON scene_logs(scene_id);
