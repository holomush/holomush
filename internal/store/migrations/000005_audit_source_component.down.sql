-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP INDEX IF EXISTS idx_audit_log_source_component;

ALTER TABLE access_audit_log DROP COLUMN IF EXISTS message;
ALTER TABLE access_audit_log DROP COLUMN IF EXISTS component;
ALTER TABLE access_audit_log DROP COLUMN IF EXISTS source;

ALTER TABLE access_audit_log RENAME COLUMN event_name TO policy_name;
ALTER TABLE access_audit_log RENAME COLUMN event_id TO policy_id;
