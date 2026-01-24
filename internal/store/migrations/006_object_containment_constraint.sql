-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Enforce that exactly one containment field is set for objects.
-- Objects must be in exactly one place: a location, held by a character, or inside a container.
ALTER TABLE objects ADD CONSTRAINT chk_exactly_one_containment
    CHECK (
        (CASE WHEN location_id IS NOT NULL THEN 1 ELSE 0 END +
         CASE WHEN held_by_character_id IS NOT NULL THEN 1 ELSE 0 END +
         CASE WHEN contained_in_object_id IS NOT NULL THEN 1 ELSE 0 END) = 1
    );
