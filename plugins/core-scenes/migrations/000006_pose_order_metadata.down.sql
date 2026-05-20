-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse the Phase 4 pose-order metadata + idle-nudge columns.

ALTER TABLE scene_participants
    DROP COLUMN IF EXISTS last_pose_seq,
    DROP COLUMN IF EXISTS last_pose_at;

ALTER TABLE scenes
    DROP COLUMN IF EXISTS idle_nudge_threshold;

ALTER TABLE scenes
    DROP COLUMN IF EXISTS total_pose_count;
