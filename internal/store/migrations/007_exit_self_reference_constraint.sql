-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Prevent exits from referencing the same location as both source and destination.
-- Defense-in-depth: domain validation in Go also checks this, but database constraint
-- ensures data integrity even if called directly or validation is bypassed.
ALTER TABLE exits ADD CONSTRAINT chk_not_self_referential
    CHECK (from_location_id != to_location_id);
