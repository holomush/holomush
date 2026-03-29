-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Remove system aliases for teleport and examine commands
DELETE FROM system_aliases WHERE alias IN ('tel', 'ex') AND created_by IS NULL;
