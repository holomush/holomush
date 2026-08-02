-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- +goose Up

-- holomush-sjtlz: disable the vestigial unconditional scene-read seed policy
-- in existing deployments (read twin of migration 000047 / holomush-8m01u).
--
-- seed:player-scene-read was seeded as an UNCONDITIONAL
-- permit(character, read, scene). Because the ABAC engine OR-combines matching
-- permits (forbid overrides, no most-specific-wins), that broad permit subsumed
-- and nullified the core-scenes plugin's membership-conditioned read policies,
-- so any character — participant or not — could `scene info` any scene and
-- read its metadata (owner, state, visibility, participant roster, invitees).
--
-- The seed is removed from the Go seed corpus (internal/access/policy/seed.go)
-- so fresh bootstraps never create it. Bootstrap only creates/upgrades seeds; it
-- never prunes rows absent from the shipped set, so existing deployments retain
-- the stale row. This migration disables it, leaving scene-read authorization
-- solely to the plugin's read-scene-as-participant / read-scene-as-invitee /
-- read-open-scene policies.
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
WHERE name = 'seed:player-scene-read'
  AND source = 'seed'
  AND dsl_text = 'permit(principal is character, action in ["read"], resource is scene);'
  AND enabled = true;

-- +goose Down

-- Revert the paired up migration (see 000048_..._seed.up.sql for the full
-- rationale): re-enable the vestigial scene-read seed row. Guarded on the same
-- exact-DSL + source='seed' + disabled-state, so an operator customization —
-- and a fresh install where the row was never created — is a no-op.
--
-- WARNING: this restores the authorization bypass the up migration closed (any
-- character could read any scene's metadata via `scene info`). It exists
-- solely for reversibility and is not a production state.

UPDATE access_policies
SET enabled    = true,
    updated_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
WHERE name = 'seed:player-scene-read'
  AND source = 'seed'
  AND dsl_text = 'permit(principal is character, action in ["read"], resource is scene);'
  AND enabled = false;
