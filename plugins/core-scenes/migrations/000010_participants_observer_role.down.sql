-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DELETE FROM scene_participants WHERE role = 'observer';
ALTER TABLE scene_participants
    DROP CONSTRAINT IF EXISTS scene_participants_role_check;
ALTER TABLE scene_participants
    ADD CONSTRAINT scene_participants_role_check
    CHECK (role IN ('owner', 'member', 'invited'));
