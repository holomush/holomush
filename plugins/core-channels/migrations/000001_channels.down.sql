-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Drop in reverse dependency order: ops_events and memberships both FK to
-- channels, so they must go first.
DROP TABLE IF EXISTS channel_ops_events;
DROP TABLE IF EXISTS channel_memberships;
DROP TABLE IF EXISTS channels;
