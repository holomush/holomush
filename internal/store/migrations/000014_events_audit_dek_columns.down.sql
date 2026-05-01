-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP INDEX IF EXISTS events_audit_dek_ref;

ALTER TABLE events_audit
    DROP COLUMN IF EXISTS dek_version,
    DROP COLUMN IF EXISTS dek_ref;
