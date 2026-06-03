-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Phase 4 pose-order metadata + idle-nudge threshold per holomush-5rh.13
-- spec §9. Maintained transactionally by the audit consumer on each
-- scene_pose insertion (see holomush-5rh.13.7 / .8 for the update path).
-- INV-SCENE-8 pins rebuild equivalence with documented recovery SQL.

-- Per-scene monotonic pose counter. Incremented on each scene_pose audit
-- row insert via SceneAuditStore.InsertScenePose.
ALTER TABLE scenes
    ADD COLUMN IF NOT EXISTS total_pose_count INTEGER NOT NULL DEFAULT 0;

-- Per-scene idle-nudge threshold. NULL = idle nudges off (default).
-- Background trigger implementation deferred to follow-up bead
-- holomush-fux3; Phase 4 ships the column + wire-shape only.
ALTER TABLE scenes
    ADD COLUMN IF NOT EXISTS idle_nudge_threshold INTERVAL NULL;

-- Per-participant pose metadata. NULL = participant has never posed.
ALTER TABLE scene_participants
    ADD COLUMN IF NOT EXISTS last_pose_at  TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS last_pose_seq INTEGER     NULL;
