-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

ALTER TABLE sessions DROP COLUMN IF EXISTS presenting_focus;
ALTER TABLE sessions DROP COLUMN IF EXISTS focus_memberships;
