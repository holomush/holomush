-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Add rendering column. Three steps to be safe on non-empty events_audit
-- and idempotent under repeated application:
--   1. Add the column nullable (IF NOT EXISTS keeps re-runs safe).
--   2. Backfill any pre-existing rows with an empty JSON object so the
--      NOT NULL constraint can be enforced.
--   3. Promote to NOT NULL to match the production invariant: every
--      audited event has rendering metadata stamped at publish time.
ALTER TABLE events_audit
  ADD COLUMN IF NOT EXISTS rendering JSONB;

UPDATE events_audit
SET rendering = '{}'::jsonb
WHERE rendering IS NULL;

ALTER TABLE events_audit
  ALTER COLUMN rendering SET NOT NULL;
