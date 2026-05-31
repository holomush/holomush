-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Connection lease: last_seen_at is refreshed by the gateway while the client
-- socket is open (holomush-rsoe6). A connection whose last_seen_at is older
-- than the lease TTL is reaped, decoupling liveness from the stored status enum.
ALTER TABLE session_connections
  ADD COLUMN IF NOT EXISTS last_seen_at BIGINT NOT NULL DEFAULT 0;

-- Backfill existing rows: treat their connect time as their last-seen time.
UPDATE session_connections SET last_seen_at = connected_at WHERE last_seen_at = 0;

CREATE INDEX IF NOT EXISTS idx_session_connections_last_seen
  ON session_connections (last_seen_at);
