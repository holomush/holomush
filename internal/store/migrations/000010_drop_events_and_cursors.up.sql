-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- F6: Drop the legacy PG event store and the per-session cursor column.
-- All event publication now flows through JetStream; audit projection to
-- events_audit; Subscribe and QueryStreamHistory read from the eventbus.
-- The sessions.event_cursors column was the per-stream replay cursor used
-- by the legacy PG-notify Subscribe path, which is now gone.

ALTER TABLE sessions DROP COLUMN IF EXISTS event_cursors;
DROP TABLE IF EXISTS events;
