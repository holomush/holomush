-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Rename policy_id / policy_name to event_id / event_name to reflect
-- that the audit log records events from any authorization source, not
-- just ABAC policies. Add source, component, message columns.

ALTER TABLE access_audit_log RENAME COLUMN policy_id TO event_id;
ALTER TABLE access_audit_log RENAME COLUMN policy_name TO event_name;

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
