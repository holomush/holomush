-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Phase 6: per-scene configurable max publish attempts. Default 3;
-- admin can bump via ExtendScenePublishVoteAttempts RPC. See spec §3.4.

ALTER TABLE scenes
    ADD COLUMN IF NOT EXISTS max_publish_attempts INTEGER NOT NULL DEFAULT 3;
