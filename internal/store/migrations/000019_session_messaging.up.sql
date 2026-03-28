-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

ALTER TABLE sessions
  ADD COLUMN last_paged TEXT NOT NULL DEFAULT '',
  ADD COLUMN last_whispered TEXT NOT NULL DEFAULT '';
