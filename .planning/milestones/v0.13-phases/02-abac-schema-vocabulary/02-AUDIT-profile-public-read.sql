-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors
--
-- 02-AUDIT-profile-public-read.sql
-- The committed, re-runnable exposure audit for the `seed:profile-public-read-*`
-- widening. Discharges ROADMAP Phase 2 success criterion 4 and PROFILE-11.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- READ-ONLY BY CONSTRUCTION
-- ─────────────────────────────────────────────────────────────────────────────
-- Every statement in this file is a SELECT. It issues no INSERT, no UPDATE, no
-- DELETE, no DDL and no row lock, and it MUST stay that way: an acceptance gate
-- on plan 02-10 counts those keywords across the non-comment lines of this file
-- and of its operator-only sibling, and fails on any match.
--
-- Remediation is NOT this file's job. Any `visibility` change the recorded
-- ledger prescribes belongs in 02-REMEDIATION.sql, behind plan 02-10 Task 4's
-- blocking approval.
--
-- MUST NOT be run as part of planning. This is an artifact the phase commits and
-- an operator runs against a real database.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- THIS FILE DELIBERATELY EMITS NO PLAYER-AUTHORED TEXT
-- ─────────────────────────────────────────────────────────────────────────────
-- Result sets 6 and 7 are per-row LEDGERS, and their output is transcribed into
-- 02-AUDIT-RESULT.md, which is committed to a public repository permanently.
-- So they emit only: row ids, property NAMES (a schema-level enumeration fixed
-- by 01-SPEC.md §8.6, not player prose), integer character lengths, and md5
-- content digests. No property value and no character description is ever an
-- output column here.
--
-- The digest is not for secrecy. It is a stable handle: it lets a verdict name a
-- specific row, and lets a later re-run prove whether that row changed, without
-- either run recording what the row said.
--
-- The text an operator actually needs in order to JUDGE a row lives in the
-- sibling file 02-AUDIT-detail-operator-only.sql, whose output is written
-- outside this repository, read there, and deleted.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- SCHEMA NOTES, re-derived from the migrations on 2026-08-05
-- ─────────────────────────────────────────────────────────────────────────────
-- * entity_properties (internal/store/migrations/000001_baseline.sql:354-377)
--   carries NO FOREIGN KEY to characters. The parent link is application-
--   enforced (see test/integration/access/seed_policies_test.go's fixture
--   ordering note). Result sets 1, 2, 3 and 6 may therefore legitimately count
--   ORPHANED rows whose parent_id names no live character. That is information,
--   not a bug; a reader who assumes an FK would misread the totals.
-- * visibility is a CHECK-constrained vocabulary:
--   public / private / restricted / system / admin. Only 'public' is widened.
-- * characters.description (000001_baseline.sql:72-80) is an INTRINSIC COLUMN
--   with NO visibility field of any kind. `visibility` exists only on
--   entity_properties. A description verdict that says "change the row's
--   visibility" names a column that does not exist.
-- * characters gained status / last_active_at / normalized_name / name_skeleton
--   in 000054_character_identity_and_lifecycle.sql. Result sets 4, 5 and 7 count
--   characters of EVERY lifecycle status on purpose: a retired character's
--   description is still published by the widening, so excluding retired rows
--   would under-report the exposure.
-- * players.is_guest is a BOOLEAN NOT NULL DEFAULT false, added by
--   000002_player_is_guest.sql:8. Confirmed against that file, not assumed.
-- * The §8.6 name lists in result sets 3 and 6 were re-derived from
--   .planning/phases/01-portal-spec/01-SPEC.md:1552-1566 in this task. They are
--   23 names: 13 scalar profile fields plus the 10 enumerated gallery slots.
--   The two `characters`-column rows (name, in-world description) and the
--   profile-reachability facet are NOT property names and are excluded.
--   A stale enumeration here silently under-reports the exact population this
--   audit exists to find, which is worse than no query at all because it looks
--   like evidence.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- HOW TO RUN
-- ─────────────────────────────────────────────────────────────────────────────
--   psql "$DATABASE_URL" --echo-queries --pset=footer=off \
--        -f .planning/phases/02-abac-schema-vocabulary/02-AUDIT-profile-public-read.sql
--
-- --echo-queries labels each of the seven result sets with the statement that
-- produced it, so the aggregate sets can be transcribed into 02-AUDIT-RESULT.md
-- without ambiguity about which is which.
--
-- Result sets 1-5 are AGGREGATES and disclose nothing; they are recorded
-- verbatim. Result sets 6 and 7 are the per-row LEDGERS; each of their rows gets
-- a verdict in 02-AUDIT-RESULT.md.


-- ═══ (1) Public character property rows, grouped by property name ═══
-- Question: which property names exist at visibility='public' on characters,
-- and how many rows and how many distinct characters does each cover?
-- Why it matters: every row counted here becomes readable by an OFF-LOCATION
-- character after the widening (D-10). This is the shape of the exposure.
SELECT
    name                                                       AS property_name,
    count(*)                                                   AS row_count,
    count(DISTINCT parent_id)                                  AS distinct_characters,
    count(*) FILTER (WHERE value IS NOT NULL AND value <> '')  AS nonempty_values
FROM entity_properties
WHERE parent_type = 'character'
  AND visibility = 'public'
GROUP BY name
ORDER BY row_count DESC, property_name;


-- ═══ (2) The same set, totalled ═══
-- Question: how many public character property rows are there in total?
-- Why it matters: this single number is what ROADMAP success criterion 4
-- records. 02-AUDIT-RESULT.md states it explicitly.
SELECT
    count(*)                  AS total_public_character_rows,
    count(DISTINCT parent_id) AS characters_with_public_rows,
    count(DISTINCT name)      AS distinct_property_names
FROM entity_properties
WHERE parent_type = 'character'
  AND visibility = 'public';


-- ═══ (3) The names OUTSIDE 01-SPEC.md §8.6's enumeration ═══
-- Question: which public character property names have no §8.6 tier floor?
-- Why it matters: D-10 widens the GRID path to all public character rows, while
-- term A of §8.5.1's conjunction still denies any name with no §8.6 floor. So
-- this is the "grid-widened but web-denied" population — readable in-game from
-- anywhere after the widening, still invisible on the web. It is the set D-11's
-- audit is really looking for, because a row here was relying on colocation and
-- on colocation alone.
SELECT
    name                      AS unenumerated_property_name,
    count(*)                  AS row_count,
    count(DISTINCT parent_id) AS distinct_characters
FROM entity_properties
WHERE parent_type = 'character'
  AND visibility = 'public'
  AND name NOT IN (
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
      )
GROUP BY name
ORDER BY row_count DESC, unenumerated_property_name;


-- ═══ (4) Character in-world descriptions ═══
-- Question: how many characters carry a non-empty in-world description, and how
-- long is the longest?
-- Why it matters: §8.6 seeds the in-world description at the `anonymous` floor
-- (D-13), so every non-empty description counted here becomes readable by a
-- logged-out visitor on the public web. §8.11 records that divergence from
-- strict grid-parity as deliberate; these counts are what make that acceptance
-- informed rather than notional.
SELECT
    count(*)                                     AS total_characters,
    count(*) FILTER (WHERE description <> '')    AS nonempty_descriptions,
    max(length(description))                     AS longest_description_chars
FROM characters;


-- ═══ (5) Descriptions on guest-provisioned characters ═══
-- Question: how much of set 4 belongs to characters created by the guest path?
-- Why it matters: the guest path provisions characters automatically and at
-- volume, so a large guest share means the exposure is mostly machine-created
-- rows rather than authored prose — a materially different acceptance decision.
-- LEFT JOIN, not JOIN: characters.player_id is nullable, and an ownerless
-- character must still be counted in the denominator.
SELECT
    count(*)                                                              AS total_characters,
    count(*) FILTER (WHERE pl.is_guest)                                   AS guest_characters,
    count(*) FILTER (WHERE pl.is_guest AND ch.description <> '')          AS guest_nonempty_descriptions
FROM characters ch
LEFT JOIN players pl ON pl.id = ch.player_id;


-- ═══ (6) PROPERTY LEDGER — every public character property row ═══
-- Question: exactly WHICH rows does the widening publish?
-- Why it matters: criterion 4 asks which rows, not how many, and a count is not
-- a which. Each row below gets its own verdict line in 02-AUDIT-RESULT.md.
--
-- This set is deliberately WHOLE-POPULATION: there is no §8.6-name exclusion in
-- its WHERE clause. An earlier revision scoped the ledger to the UNenumerated
-- names (set 3) and left the enumerated ones as aggregate counts — which is
-- backwards. Term A of §8.5.1's conjunction matches only §8.6-enumerated names,
-- so the ENUMERATED rows are precisely the ones the widening publishes to the
-- web, and they were the only population the audit never enumerated.
--
-- The in_spec_86 flag keeps the two verdicts distinguishable: true means "this
-- row ships public on the web", false means "grid-widened, web-denied".
--
-- Emits no value text. length() and md5() only.
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
    coalesce(length(value), 0)    AS value_chars,
    md5(coalesce(value, ''))      AS value_digest
FROM entity_properties
WHERE parent_type = 'character'
  AND visibility = 'public'
ORDER BY id;


-- ═══ (7) DESCRIPTION LEDGER — every character with a non-empty description ═══
-- Question: exactly WHICH descriptions become anonymously readable?
-- Why it matters: each row below gets a verdict in 02-AUDIT-RESULT.md drawn from
-- the THREE-option description vocabulary — exposure accepted / redact or clear
-- the text / raise the §8.6 description floor. NOT the property vocabulary:
-- characters has no visibility column, so "change the row's visibility" names
-- something that does not exist.
--
-- Emits no description text, no character name, and no owning player id. The
-- character id is the handle; anything more would identify the author of the
-- prose this ledger exists to protect.
SELECT
    id                     AS character_id,
    length(description)    AS description_chars,
    md5(description)       AS description_digest
FROM characters
WHERE description <> ''
ORDER BY id;
