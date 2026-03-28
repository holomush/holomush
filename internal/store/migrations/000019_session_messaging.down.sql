-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

ALTER TABLE sessions
  DROP COLUMN IF EXISTS last_paged,
  DROP COLUMN IF EXISTS last_whispered;
