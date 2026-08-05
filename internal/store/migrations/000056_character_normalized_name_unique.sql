-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- +goose Up

-- 000056 — constrain the character-name uniqueness key (D-21 step C).
--
-- This is the THIRD and last of three migrations in one release. 000054 (A)
-- added the nullable identity columns; 000055 (B) backfilled them and halted on
-- any pre-existing duplicate; this step makes the database — not an application
-- check — the thing that decides whether a name is free (01-SPEC.md §6.1.3).
--
-- ## Statement ORDER is load-bearing, not stylistic
--
-- SET NOT NULL comes FIRST. PostgreSQL treats NULLs as distinct for uniqueness,
-- so a unique index over an unbackfilled nullable column is created
-- successfully and enforces NOTHING — a green deploy with IDENT-09's guarantee
-- silently absent, producing no error at any point. Putting SET NOT NULL first
-- makes 000055's execution a HARD precondition of this migration: had it not
-- run, every normalized_name would still be NULL and this ALTER fails with
-- "column contains null values" before the index is ever considered.
--
-- ## Uniqueness is over normalized_name ONLY
--
-- The name_skeleton index that 000054 created stays NON-UNIQUE (D-21, D-30 part
-- 1). A UNIQUE constraint there would block a legitimate post-Unicode-upgrade
-- recompute that collapses two live rows. The confusable guarantee is enforced
-- instead by serialization — the transaction-scoped advisory lock in
-- worldpostgres.guardSkeleton (D-30 part 2) — plus 000055's skeleton-collision
-- scan for the pre-existing corpus (D-30 part 3).
--
-- ## Deployment precondition
--
-- 000054, 000055 and 000056 are ONE RELEASE. Two things follow, and an operator
-- staring at an incident needs to find them here rather than debug them:
--
--   1. Character writers MUST be quiesced across the 000055 -> 000056 boundary.
--      goose's advisory lock serializes MIGRATORS, not application writers, so
--      nothing in the framework stops an old binary inserting a NULL
--      normalized_name after the backfill commits but before SET NOT NULL, or a
--      new binary inserting a duplicate before the index exists. Three
--      acceptable ways to satisfy it: drain all character writers before
--      000055; run the pair while only one replica is up; or rely on the
--      single-replica topology (today's compose.prod.yaml declares one `core`
--      service with no `replicas:` key, and migrations complete before
--      orch.StartAll, so the migrating process never serves during its own
--      migration and the window is empty by construction). The window is NOT
--      empty for compose.cluster.yaml or any multi-replica posture.
--
--      If the precondition is violated the failure is LOUD, not silent: the
--      ALTER below fails with "column contains null values", or the index
--      creation fails with a duplicate-key error. The exposure is a repeatedly
--      failing rollout, not corruption. This is deliberately NOT closed by
--      merging the backfill and the constraint into one transaction: that would
--      hold an ACCESS EXCLUSIVE lock for the backfill's full duration, trading a
--      bounded, loud, retryable failure for an unbounded outage.
--
--   2. A deployment that applies 000054 and STOPS before 000055 backfills the
--      column refuses EVERY character create and rename with
--      NAME_SKELETON_UNVERIFIABLE until the chain completes. That is
--      charname.Gate failing closed against a partially populated skeleton
--      column (D-30) — the correct direction, and closed by construction when
--      the three ship together — but it presents as "character creation is
--      broken", which is not obviously a migration symptom.

ALTER TABLE characters ALTER COLUMN normalized_name SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS characters_normalized_name_key
    ON characters (normalized_name);

-- +goose Down

-- Reverse in reverse order: drop the index, then relax the constraint.
DROP INDEX IF EXISTS characters_normalized_name_key;

ALTER TABLE characters ALTER COLUMN normalized_name DROP NOT NULL;
