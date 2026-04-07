-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

ALTER TABLE sessions ADD COLUMN IF NOT EXISTS player_id TEXT NOT NULL DEFAULT '';
