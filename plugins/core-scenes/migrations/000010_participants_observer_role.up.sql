-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Widen the participants role constraint to admit the E9.5 observer role
-- (spec 2026-06-07-web-portal-scenes-design.md D6, INV-SCENE-61).
ALTER TABLE scene_participants
    DROP CONSTRAINT IF EXISTS scene_participants_role_check;
ALTER TABLE scene_participants
    ADD CONSTRAINT scene_participants_role_check
    CHECK (role IN ('owner', 'member', 'invited', 'observer'));
