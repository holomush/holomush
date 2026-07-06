-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- holomush-8m01u: disable the vestigial unconditional scene-write seed policy
-- in existing deployments.
--
-- seed:player-scene-participant was seeded as an UNCONDITIONAL
-- permit(character, write, scene). Because the ABAC engine OR-combines matching
-- permits (forbid overrides, no most-specific-wins), that broad permit subsumed
-- and nullified the core-scenes plugin's participant-conditioned
-- write-scene-as-participant policy, so any character — including a
-- non-participant — could emit IC/OOC into any scene.
--
-- The seed is removed from the Go seed corpus (internal/access/policy/seed.go)
-- so fresh bootstraps never create it. Bootstrap only creates/upgrades seeds; it
-- never prunes rows absent from the shipped set, so existing deployments retain
-- the stale row. This migration disables it, leaving scene-write authorization
-- solely to the plugin's write-scene-as-participant policy.
--
-- Guarded on the exact vestigial DSL + source='seed' so an operator who
-- customized the policy (via `policy edit`) is left untouched — the
-- compare-and-swap spirit of the seed-migration contract
-- (docs/specs/abac/07-migration-seeds.md). The `enabled = true` guard makes
-- re-application a clean no-op. updated_at is written as epoch-nanoseconds
-- BIGINT to match internal/access/policy/store/postgres.go (INV-STORE-1: post-
-- gfo6 migrations use BIGINT epoch-ns, not wall-clock column types).

UPDATE access_policies
SET enabled    = false,
    updated_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
WHERE name = 'seed:player-scene-participant'
  AND source = 'seed'
  AND dsl_text = 'permit(principal is character, action in ["write"], resource is scene);'
  AND enabled = true;
