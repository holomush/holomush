-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Add focus membership tracking to sessions. FocusMemberships is a JSONB
-- array of {kind, target_id, joined_at} objects. PresentingFocus is a JSONB
-- object {kind, target_id} or NULL. Both columns support the focus substrate
-- (spec: 2026-04-11-focus-substrate-design.md, invariants I-1, I-2, I-6).

ALTER TABLE sessions
    ADD COLUMN IF NOT EXISTS focus_memberships JSONB NOT NULL DEFAULT '[]';

ALTER TABLE sessions
    ADD COLUMN IF NOT EXISTS presenting_focus JSONB DEFAULT NULL;
