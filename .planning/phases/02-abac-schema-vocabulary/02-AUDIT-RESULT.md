# Phase 02 — `seed:profile-public-read-property` exposure audit result

The recorded evidence for ROADMAP Phase 2 success criterion 4 and PROFILE-11, and
the merge gate on plan `02-07`'s widening.

**Run date:** 2026-08-05 (UTC)

**Database identifier:** the HoloMUSH **sandbox** corpus, taken from kopia snapshot
**`7e48a9b592c2e0d302a5da3cf0171835`** (snapshot start `2026-08-05T03:00:00Z`,
source path `/sandbox/holomush-holomush`, 255,767 bytes), restored into a
**throwaway local PostgreSQL 18.4 container** and queried there.

No connection string and no credential appears in this file. A kopia snapshot id
is a content address, not a secret.

**Target discipline.** The queries ran against the restored throwaway copy, never
against the live sandbox at `game.holomush.dev`. The container was destroyed after
the run.

**Schema path taken: the restore was audited AS-IS, with no migrations applied.**
The restored corpus is at goose schema level **53**; this branch adds `000054`,
`000055` and `000056`. Applying them was unnecessary because the audit reads only
columns that predate them — `entity_properties.{id, parent_type, parent_id, name,
value, visibility}` (all `000001`), `characters.{id, description, player_id}` (all
`000001`), and `players.is_guest` (`000002`). Verified against the restored DDL
before running, not assumed. Auditing the corpus in the shape it actually exists is
also the more faithful reading of criterion 4.

Both committed queries ran to completion with **zero** SQL errors:

- `02-AUDIT-profile-public-read.sql` — the seven-part sanitized ledger (this file)
- `02-AUDIT-detail-operator-only.sql` — the detail report, written to a path outside
  the repository, read, and deleted (see the closing section)

---

## The result in one line

**The audited corpus contains no rows that the widening exposes.** `entity_properties`
is empty in its entirety — not merely empty of public character rows — and all three
characters carry an empty in-world description. This is a genuine zero-row result from
a database that was actually reached, which criterion 4 accepts as evidence.

---

## Aggregate result sets (verbatim)

### (1) Public character property rows, grouped by property name

```text
 property_name | row_count | distinct_characters | nonempty_values
---------------+-----------+---------------------+-----------------
(0 rows)
```

### (2) The same set, totalled

```text
 total_public_character_rows | characters_with_public_rows | distinct_property_names
-----------------------------+-----------------------------+-------------------------
                           0 |                           0 |                       0
(1 row)
```

**Result set 2 total: `0`.** This is the number success criterion 4 records: the
widening publishes **zero** existing property rows in the audited corpus.

### (3) Public character property names outside 01-SPEC.md §8.6's enumeration

```text
 unenumerated_property_name | row_count | distinct_characters
----------------------------+-----------+---------------------
(0 rows)
```

The "grid-widened but web-denied" population — the set D-11's audit is really looking
for — is empty.

### (4) Character in-world descriptions

```text
 total_characters | nonempty_descriptions | longest_description_chars
------------------+-----------------------+---------------------------
                3 |                     0 |                         0
(1 row)
```

Three characters exist; **none** carries a non-empty description. The `anonymous`
floor §8.6 seeds for the in-world description (D-13) therefore publishes nothing here.

### (5) Descriptions on guest-provisioned characters

```text
 total_characters | guest_characters | guest_nonempty_descriptions
------------------+------------------+-----------------------------
                3 |                2 |                           0
(1 row)
```

Two of the three characters belong to guest-provisioned players, consistent with a
demo environment. None has a description.

---

## Property ledger (result set 6)

```text
 property_row_id | character_id | property_name | in_spec_86 | value_chars | value_digest
-----------------+--------------+---------------+------------+-------------+--------------
(0 rows)
```

| Metric | Value |
| --- | --- |
| Ledger rows recorded | 0 |
| Result set 6 row count | 0 |
| Rows with `in_spec_86 = true` (the population the widening publishes to the web) | 0 |
| Rows with `in_spec_86 = false` (grid-widened, web-denied) | 0 |
| Verdict `no change needed` | 0 |
| Verdict `remediate: set this row's visibility to private` | 0 |

The ledger's row count **equals** result set 6's row count (0 = 0), so no row the
widening publishes went un-adjudicated. There are no rows to adjudicate.

**No verdict in this file changes the policy.** D-11 fixes the remedy in advance:
for a property row that was relying on colocation as de-facto privacy, the fix is that
row's `visibility` column. The policy itself is not the remedy and is not touched.

## Description ledger (result set 7)

```text
 character_id | description_chars | description_digest
--------------+-------------------+--------------------
(0 rows)
```

| Metric | Value |
| --- | --- |
| Ledger rows recorded | 0 |
| Characters with a non-empty description | 0 |
| Verdict `exposure accepted` | 0 |
| Verdict `remediate: redact or clear the description text` | 0 |
| Verdict `the configured description floor was raised to player in §8.6` | 0 |

No description verdict mentions a row `visibility`, because `characters.description`
is an intrinsic column (`internal/store/migrations/000001_baseline.sql:72-80`) and
carries no visibility field; `visibility` exists only on `entity_properties`.

---

## How far this evidence reaches, and how far it does not

Stated plainly so the zero is not over-read:

- **It discharges criterion 4 as written.** The gate is "the widening ships only after
  an audit of existing public `parent_type='character'` rows and existing character
  descriptions." That audit has now run against real restored data and found nothing
  exposed. Nobody is merging the widening without having looked.
- **The audited corpus is small.** Three characters, two of them guest-provisioned,
  and an entirely empty `entity_properties` table. The sandbox is a demo environment,
  not a populated game. This is evidence that **the widening exposes nothing in the
  sandbox corpus** — it is not, and cannot be, evidence about a future populated
  production corpus.
- **That is exactly why the query is committed and re-runnable** (D-12). Re-run
  `02-AUDIT-profile-public-read.sql` against any environment that later holds real
  player data, before the widening reaches it. The empty result here costs nothing to
  re-establish and the ledger's content digests make a later run able to prove whether
  any row changed.
- **The behavioural half of criterion 4 is separate.** That the widening actually
  widens — an off-location read that the shipped colocation policy denied now
  succeeding, against a control corpus where it still fails — is proven by
  `test/integration/access/profile_public_read_test.go`, not by this file.

---

## Remediation

**Task 4's checkpoint is deliberately unanswered.** It approves irreversible writes to
live player data and the maintainer has reserved that decision; this run does not
select an option, performs no write, and writes no `02-REMEDIATION.sql`.

What the evidence above hands to that decision: the ledger carries **zero** `remediate`
verdicts, because it carries zero rows. There are no row ids to enumerate and no prior
values to capture.

This section is **not** the `## Remediation verdict` section the plan's completion gate
requires. That section records an approver and a date and is written by Task 4b, after
Task 4 is answered.

---

## No production row was changed

Both queries are read-only by construction: every statement in both files is a
`SELECT`, mechanically gated. They ran against a throwaway restored copy, and that
copy has been destroyed. Nothing in this run wrote to the sandbox or to any live
database.

## No player-authored text appears in this file

This file contains only: integers, a kopia snapshot id, schema and column names,
property names drawn from 01-SPEC.md §8.6's fixed enumeration, and table headers
emitted by `psql`. It contains no `entity_properties` value, no `characters.description`
text, no character name and no player name — and in this run there was none to contain,
since both detail result sets returned zero rows.

The operator-only detail report was written to
`$TMPDIR/holomush-02-10-audit-detail.20260805T191624Z.txt` — a path outside the
repository working tree, `chmod 600` — read there, and **deleted**. It was never
committed, never pasted into a `.planning/` artifact, never quoted in a SUMMARY, and
never attached to an issue. `git status --short` is clean of it.
