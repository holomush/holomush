-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- DROP TABLE cascades to dependent indexes; drop the child (FK) table first.
DROP TABLE IF EXISTS published_scene_votes;
DROP TABLE IF EXISTS published_scenes;
