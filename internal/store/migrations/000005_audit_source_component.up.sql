-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Rename policy_id / policy_name to event_id / event_name to reflect
-- that the audit log records events from any authorization source, not
-- just ABAC policies. Add source, component, message columns.

-- Idempotent column renames: only rename if the source column still exists
-- and the target column has not yet been created. This makes the migration
-- safe to re-run after partial application.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'access_audit_log' AND column_name = 'policy_id'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'access_audit_log' AND column_name = 'event_id'
    ) THEN
        ALTER TABLE access_audit_log RENAME COLUMN policy_id TO event_id;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'access_audit_log' AND column_name = 'policy_name'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'access_audit_log' AND column_name = 'event_name'
    ) THEN
        ALTER TABLE access_audit_log RENAME COLUMN policy_name TO event_name;
    END IF;
END $$;

ALTER TABLE access_audit_log ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'engine';
ALTER TABLE access_audit_log ADD COLUMN IF NOT EXISTS component TEXT NOT NULL DEFAULT 'abac';
ALTER TABLE access_audit_log ADD COLUMN IF NOT EXISTS message TEXT NOT NULL DEFAULT '';

-- Drop the default now that historical rows have been backfilled. New
-- rows must provide explicit values.
ALTER TABLE access_audit_log ALTER COLUMN source DROP DEFAULT;
ALTER TABLE access_audit_log ALTER COLUMN component DROP DEFAULT;
ALTER TABLE access_audit_log ALTER COLUMN message DROP DEFAULT;

-- Index for operator queries that filter by source and component.
CREATE INDEX IF NOT EXISTS idx_audit_log_source_component
    ON access_audit_log(source, component, timestamp DESC);
