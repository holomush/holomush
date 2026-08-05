-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- +goose Up

-- 000054 — character identity and lifecycle primitives (D-21 step A).
--
-- This is the FIRST of three migrations in one release. B (a Go migration)
-- backfills normalized_name / name_skeleton and detects pre-existing
-- duplicates; C then does SET NOT NULL on normalized_name BEFORE creating the
-- unique index over it. That order is load-bearing: PostgreSQL treats NULLs as
-- distinct for uniqueness, so a UNIQUE index over an unbackfilled nullable
-- column succeeds and enforces NOTHING — a green deploy with the guarantee
-- silently absent. SET NOT NULL first makes B's execution a precondition of C.

-- Lifecycle status, as a closed CHECK vocabulary (the house pattern; see
-- 000001_baseline.sql). NOT NULL with a default, so no row is ever in an
-- unknown lifecycle state.
ALTER TABLE characters
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'retired', 'idle'));

-- Last-active time as BIGINT epoch NANOSECONDS, never a time-zone-carrying
-- SQL type (INV-STORE-1). 0 is the Unix epoch and is the never-active sentinel: no
-- nulls, consistent with the no-unknown-state discipline above. Any "last
-- active" rendering MUST special-case 0 rather than displaying 1970. Useful
-- side effect: 0 is the minimum, so a most-recent-first sort places
-- never-active rows last without a NULLS LAST clause.
--
-- The WRITE seam for this column is a later phase's; it lands here as a schema
-- primitive so the phases that read it have something to read.
ALTER TABLE characters
    ADD COLUMN IF NOT EXISTS last_active_at BIGINT NOT NULL DEFAULT 0;

-- The stored uniqueness key: the §6.1.1 normalized form (NFKC, Cf stripped,
-- whitespace canonicalized, Unicode full case folded).
--
-- DELIBERATELY NULLABLE here. It becomes NOT NULL in migration C, after the
-- backfill has populated every row. Declaring NOT NULL now would fail on any
-- database that already holds characters.
ALTER TABLE characters
    ADD COLUMN IF NOT EXISTS normalized_name TEXT;

-- The UTS #39 confusable skeleton of the normalized key, and the Unicode
-- version it was computed from.
--
-- The version is per-ROW, not per-database (D-23): after a confusables-table
-- upgrade, a stale subset is queryable and a partial or interrupted recompute
-- is visible rather than silent.
ALTER TABLE characters
    ADD COLUMN IF NOT EXISTS name_skeleton TEXT;

ALTER TABLE characters
    ADD COLUMN IF NOT EXISTS name_skeleton_unicode_version TEXT;

-- NON-UNIQUE, and it stays that way (D-30 part 1).
--
-- The confusables table shifts between Unicode releases. A UNIQUE constraint
-- whose meaning shifts under a data-version bump retroactively invalidates
-- rows, and it would BLOCK a legitimate post-upgrade recompute that collapses
-- two live rows onto one skeleton — trading a recoverable operational state
-- for an unrecoverable one.
--
-- The confusable guarantee is enforced instead by SERIALIZATION: the gate's
-- skeleton read and the subsequent insert run inside one transaction holding
-- an advisory lock keyed on the skeleton (D-30 part 2), plus a skeleton-
-- collision scan in the duplicate-detection migration (D-30 part 3) for the
-- pre-existing corpus. Do not "strengthen" this index.
CREATE INDEX IF NOT EXISTS idx_characters_name_skeleton
    ON characters(name_skeleton);

-- Seed the character-name block list (D-14). The value is a JSON array string
-- so it round-trips through the settings string-slice accessor.
-- ON CONFLICT DO NOTHING preserves an operator override on re-application.
INSERT INTO holomush_system_info (key, value)
VALUES ('core.character.name.blocklist', '[]')
ON CONFLICT (key) DO NOTHING;

-- +goose Down

-- Reverse 000054 in reverse order.

-- Only deletes the seeded row if the value is still the default '[]';
-- an operator-customized block list is preserved.
DELETE FROM holomush_system_info
WHERE key = 'core.character.name.blocklist'
  AND value = '[]';

DROP INDEX IF EXISTS idx_characters_name_skeleton;

ALTER TABLE characters DROP COLUMN IF EXISTS name_skeleton_unicode_version;
ALTER TABLE characters DROP COLUMN IF EXISTS name_skeleton;
ALTER TABLE characters DROP COLUMN IF EXISTS normalized_name;
ALTER TABLE characters DROP COLUMN IF EXISTS last_active_at;
ALTER TABLE characters DROP COLUMN IF EXISTS status;
