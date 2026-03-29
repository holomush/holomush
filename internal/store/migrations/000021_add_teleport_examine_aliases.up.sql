-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Add system aliases for teleport and examine commands
-- created_by is NULL for system-seeded aliases (FK references players table)
INSERT INTO system_aliases (alias, command)
VALUES ('tel', 'teleport'),
       ('ex', 'examine')
ON CONFLICT (alias) DO NOTHING;
