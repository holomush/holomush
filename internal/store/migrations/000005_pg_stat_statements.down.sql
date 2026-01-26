-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- No-op: pg_stat_statements extension should not be dropped as it may be
-- used by other schemas or for server-wide monitoring. The extension is
-- optional and its presence/absence has no effect on application behavior.
SELECT 1;
