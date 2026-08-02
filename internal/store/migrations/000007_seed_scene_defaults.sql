-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- +goose Up

-- Seed the default scene focus replay tail count in holomush_system_info.
-- ON CONFLICT DO NOTHING preserves operator overrides on re-application.

INSERT INTO holomush_system_info (key, value)
VALUES ('scenes.focus.replay_tail_default', '3')
ON CONFLICT (key) DO NOTHING;

-- +goose Down

-- Remove the seeded scene default. Only deletes the row if the value is
-- still the default '3'; operator-customized values are preserved.

DELETE FROM holomush_system_info
WHERE key = 'scenes.focus.replay_tail_default'
  AND value = '3';
