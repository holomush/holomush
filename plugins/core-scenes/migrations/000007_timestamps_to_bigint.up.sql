-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Convert core-scenes timestamp columns from TIMESTAMPTZ to BIGINT (epoch
-- nanoseconds, UTC). Plugin-side companion to host migration. INV-STORE-1,
-- INV-STORE-5 (plugin AAD path).

-- Drop TIMESTAMPTZ defaults before type conversion; PostgreSQL cannot
-- auto-cast TIMESTAMPTZ defaults when changing column type to BIGINT.
ALTER TABLE scenes
    ALTER COLUMN created_at DROP DEFAULT;

ALTER TABLE scene_participants
    ALTER COLUMN joined_at DROP DEFAULT;

ALTER TABLE scene_ops_events
    ALTER COLUMN occurred_at DROP DEFAULT;

ALTER TABLE scene_log
    ALTER COLUMN inserted_at DROP DEFAULT;

ALTER TABLE scenes
    ALTER COLUMN created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM created_at) * 1e9)::BIGINT,
    ALTER COLUMN ended_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM ended_at) * 1e9)::BIGINT,
    ALTER COLUMN archived_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM archived_at) * 1e9)::BIGINT;

ALTER TABLE scenes
    ALTER COLUMN created_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE scene_participants
    ALTER COLUMN joined_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM joined_at) * 1e9)::BIGINT;

ALTER TABLE scene_participants
    ALTER COLUMN joined_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE scene_ops_events
    ALTER COLUMN occurred_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM occurred_at) * 1e9)::BIGINT;

ALTER TABLE scene_ops_events
    ALTER COLUMN occurred_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE scene_log
    ALTER COLUMN timestamp
        TYPE BIGINT USING (EXTRACT(EPOCH FROM timestamp) * 1e9)::BIGINT,
    ALTER COLUMN inserted_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM inserted_at) * 1e9)::BIGINT;

ALTER TABLE scene_log
    ALTER COLUMN inserted_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE scene_participants
    ALTER COLUMN last_pose_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM last_pose_at) * 1e9)::BIGINT;
