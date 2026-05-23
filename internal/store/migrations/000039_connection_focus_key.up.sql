-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Phase 5 (holomush-5rh.14): per-Connection focus pointer.
-- NULL = grid focus (default for new connections); JSONB-encoded
-- FocusKey when explicitly focused on a scene/channel/etc.
-- JSONB shape mirrors the focus_memberships precedent at
-- 000006_session_focus.up.sql:9-13.
ALTER TABLE session_connections
    ADD COLUMN IF NOT EXISTS focus_key JSONB NULL;
