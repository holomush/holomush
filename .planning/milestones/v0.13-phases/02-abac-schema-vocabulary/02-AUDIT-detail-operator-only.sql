-- OPERATOR-ONLY OUTPUT — player-authored property values and in-world descriptions.
-- Its OUTPUT MUST NOT be committed, pasted into any .planning/ artifact or SUMMARY,
-- or attached to an issue: write it OUTSIDE this repository, read it there, delete it.
--
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors
--
-- 02-AUDIT-detail-operator-only.sql
-- The adjudication companion to 02-AUDIT-profile-public-read.sql, for plan 02-10
-- of phase 02-abac-schema-vocabulary.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- THE FILE IS COMMITTED. THE OUTPUT NEVER IS.
-- ─────────────────────────────────────────────────────────────────────────────
-- This file is a QUERY, so it lives in version control like any other artifact.
-- What it RETURNS is the exact prose the audit exists to protect, and that is
-- why the phase's committed evidence is the sanitized ledger in
-- 02-AUDIT-RESULT.md — row ids, lengths and md5 digests — and never this.
--
-- The two are designed to line up. Both order by the same id, and the ledger's
-- md5 digest is computed over the same bytes this query prints, so an operator
-- reading a row here can match it to the ledger line they are about to write a
-- verdict against, and a later re-run can prove whether that row changed.
--
-- Without this query the operator adjudicates rows they have never seen: a
-- verdict of "no change needed" on a `profile.biography` row is worthless if
-- nobody read the biography. The ledger proves every row was ACCOUNTED FOR; this
-- query is how a human decides what the verdict should be.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- READ-ONLY BY CONSTRUCTION
-- ─────────────────────────────────────────────────────────────────────────────
-- Both statements are SELECTs. This file issues no INSERT, no UPDATE, no DELETE,
-- no DDL and no row lock. Remediation belongs in 02-REMEDIATION.sql, behind plan
-- 02-10 Task 4's blocking approval.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- HOW TO RUN
-- ─────────────────────────────────────────────────────────────────────────────
-- Send the output outside the repository. A worked invocation:
--
--   DETAIL_OUT="${TMPDIR:-/tmp}/holomush-02-10-audit-detail.$(date +%Y%m%dT%H%M%SZ).txt"
--   psql "$DATABASE_URL" --pset=footer=off \
--        -f .planning/phases/02-abac-schema-vocabulary/02-AUDIT-detail-operator-only.sql \
--        > "$DETAIL_OUT"
--   chmod 600 "$DETAIL_OUT"
--   # ... read it, record the verdicts in 02-AUDIT-RESULT.md, then:
--   rm -f "$DETAIL_OUT"
--
-- $TMPDIR and /tmp are both outside the repository working tree, so the output
-- cannot be swept into a commit by a wildcard add. Do NOT redirect it to a path
-- under .planning/ "just while reading".


-- ═══ (A) PROPERTY DETAIL — the same population as ledger set 6 ═══
-- Every public character property row, WHATEVER its name, with its value.
-- Same WHERE clause and same ORDER BY as result set 6 of the ledger query, so
-- the two line up row for row on property_row_id.
--
-- in_spec_86 = true  → §8.6 gives this name a tier floor, so term A permits it
--                      and the widening publishes it ON THE WEB. Read these
--                      first: they are the rows that go public.
-- in_spec_86 = false → no §8.6 floor, so term A denies it on the web. It is
--                      still newly readable IN-GAME from any location, which is
--                      the de-facto-privacy question D-11 asks about.
SELECT
    id                            AS property_row_id,
    parent_id                     AS character_id,
    name                          AS property_name,
    (name IN (
        'profile.pronouns', 'profile.rumors', 'profile.currently',
        'profile.rp_preferences', 'profile.timezone', 'profile.concept',
        'profile.species', 'profile.age', 'profile.faction',
        'profile.appearance', 'profile.personality', 'profile.biography',
        'profile.image.primary',
        'profile.image.gallery.00', 'profile.image.gallery.01',
        'profile.image.gallery.02', 'profile.image.gallery.03',
        'profile.image.gallery.04', 'profile.image.gallery.05',
        'profile.image.gallery.06', 'profile.image.gallery.07',
        'profile.image.gallery.08', 'profile.image.gallery.09'
      ))                          AS in_spec_86,
    value                         AS property_value
FROM entity_properties
WHERE parent_type = 'character'
  AND visibility = 'public'
ORDER BY id;


-- ═══ (B) DESCRIPTION DETAIL — the same population as ledger set 7 ═══
-- Every character carrying a non-empty in-world description, with its text.
-- §8.6 seeds the description at the `anonymous` floor (D-13), so each of these
-- becomes readable by a logged-out visitor.
--
-- The verdict vocabulary for these rows has THREE options and none of them is
-- "change the row's visibility": characters has no visibility column. The
-- options are exposure accepted / redact or clear the text / raise the §8.6
-- description floor to `player`.
SELECT
    id             AS character_id,
    description    AS character_description
FROM characters
WHERE description <> ''
ORDER BY id;
