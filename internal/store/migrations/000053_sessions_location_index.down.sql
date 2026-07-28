-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse 000053: drop the presence-query index on sessions.location_id.

DROP INDEX IF EXISTS idx_sessions_location_id;
