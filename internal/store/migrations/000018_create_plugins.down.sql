-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP TABLE IF EXISTS plugins;
-- Note: events_audit TRUNCATE in up is irreversible. Down rolls back schema
-- only; truncated rows are not restored.
