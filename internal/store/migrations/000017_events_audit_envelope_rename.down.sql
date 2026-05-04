-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse the Phase 3d rename. Column reverts to its original name.
-- Idempotent symmetric to the up migration: only rename when 'envelope'
-- is present and 'payload' is absent.

DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'events_audit'
      AND column_name = 'envelope'
  ) AND NOT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'events_audit'
      AND column_name = 'payload'
  ) THEN
    ALTER TABLE events_audit RENAME COLUMN envelope TO payload;
  END IF;
END $$;
