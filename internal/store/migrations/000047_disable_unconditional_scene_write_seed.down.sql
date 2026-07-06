-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Revert the paired up migration (see 000047_..._seed.up.sql for the full
-- rationale): re-enable the vestigial scene-write seed row. Guarded on the same
-- exact-DSL + source='seed' + disabled-state, so an operator customization —
-- and a fresh install where the row was never created — is a no-op.
--
-- WARNING: this restores the authorization bypass the up migration closed (a
-- non-participant could emit into any scene). It exists solely for
-- reversibility and is not a production state.

UPDATE access_policies
SET enabled    = true,
    updated_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
WHERE name = 'seed:player-scene-participant'
  AND source = 'seed'
  AND dsl_text = 'permit(principal is character, action in ["write"], resource is scene);'
  AND enabled = false;
