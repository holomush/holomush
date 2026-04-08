-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP INDEX IF EXISTS idx_audit_log_source_component;

ALTER TABLE access_audit_log DROP COLUMN IF EXISTS message;
ALTER TABLE access_audit_log DROP COLUMN IF EXISTS component;
ALTER TABLE access_audit_log DROP COLUMN IF EXISTS source;

-- Idempotent reverse renames: only rename if the source column still exists
-- and the target column has not yet been recreated.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'access_audit_log' AND column_name = 'event_name'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'access_audit_log' AND column_name = 'policy_name'
    ) THEN
        ALTER TABLE access_audit_log RENAME COLUMN event_name TO policy_name;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'access_audit_log' AND column_name = 'event_id'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'access_audit_log' AND column_name = 'policy_id'
    ) THEN
        ALTER TABLE access_audit_log RENAME COLUMN event_id TO policy_id;
    END IF;
END $$;
