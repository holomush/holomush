-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Remove the seeded scene default. Only deletes the row if the value is
-- still the default '3'; operator-customized values are preserved.

DELETE FROM holomush_system_info
WHERE key = 'scenes.focus.replay_tail_default'
  AND value = '3';
