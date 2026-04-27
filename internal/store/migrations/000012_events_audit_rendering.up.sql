-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

ALTER TABLE events_audit
  ADD COLUMN rendering JSONB NOT NULL;
