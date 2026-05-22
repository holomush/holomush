-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- DOWN MIGRATION IS PRECISION-LOSSY. Recovers TIMESTAMPTZ semantics but
-- truncates ns → µs. No backfill of pre-down-migration data is provided.

-- Drop BIGINT defaults before type conversion back to TIMESTAMPTZ;
-- PostgreSQL cannot auto-cast BIGINT defaults when changing column type
-- to TIMESTAMPTZ.
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
        TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9),
    ALTER COLUMN ended_at
        TYPE TIMESTAMPTZ USING to_timestamp(ended_at::double precision / 1e9),
    ALTER COLUMN archived_at
        TYPE TIMESTAMPTZ USING to_timestamp(archived_at::double precision / 1e9);

ALTER TABLE scenes
    ALTER COLUMN created_at SET DEFAULT now();

ALTER TABLE scene_participants
    ALTER COLUMN joined_at
        TYPE TIMESTAMPTZ USING to_timestamp(joined_at::double precision / 1e9),
    ALTER COLUMN last_pose_at
        TYPE TIMESTAMPTZ USING to_timestamp(last_pose_at::double precision / 1e9);

ALTER TABLE scene_participants
    ALTER COLUMN joined_at SET DEFAULT now();

ALTER TABLE scene_ops_events
    ALTER COLUMN occurred_at
        TYPE TIMESTAMPTZ USING to_timestamp(occurred_at::double precision / 1e9);

ALTER TABLE scene_ops_events
    ALTER COLUMN occurred_at SET DEFAULT now();

ALTER TABLE scene_log
    ALTER COLUMN timestamp
        TYPE TIMESTAMPTZ USING to_timestamp(timestamp::double precision / 1e9),
    ALTER COLUMN inserted_at
        TYPE TIMESTAMPTZ USING to_timestamp(inserted_at::double precision / 1e9);

ALTER TABLE scene_log
    ALTER COLUMN inserted_at SET DEFAULT now();
