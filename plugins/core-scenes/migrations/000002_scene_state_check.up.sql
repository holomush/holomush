-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Defense-in-depth: enforce the scene state machine at the database level.
-- The Go-side state machine (plugins/core-scenes/lifecycle.go IsValidTransition,
-- CanEnd, CanPause, CanResume, CanUpdate) and the race-safe UPDATE WHERE
-- clauses in plugins/core-scenes/store.go are the primary enforcement
-- mechanisms. This CHECK constraint is the last line of defense against
-- corruption from bugs, manual SQL during incident response, or future
-- migrations that bypass the application layer.
--
-- The four valid states are defined in plugins/core-scenes/types.go as
-- SceneStateActive, SceneStatePaused, SceneStateEnded, SceneStateArchived.
-- If a new state is added, this constraint must be updated AND a new
-- migration written; the constraint serves as a forcing function.

ALTER TABLE scenes
  ADD CONSTRAINT scenes_state_check
  CHECK (state IN ('active', 'paused', 'ended', 'archived'));
