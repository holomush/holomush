-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse the Phase 3d rename. Column reverts to its original name.

ALTER TABLE events_audit RENAME COLUMN envelope TO payload;
