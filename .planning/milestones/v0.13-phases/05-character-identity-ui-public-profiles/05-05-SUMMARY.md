---
phase: 05-character-identity-ui-public-profiles
plan: 05
subsystem: access
tags: [abac, profile-visibility, integration-test, invariant-registry]
status: complete

requires:
  - "05-03: the CharacterAccessServer facade constructor shape"
  - "internal/access/policy: the seeded §8.6 tier-floor family"
  - "internal/grpc/characteraccess_projection.go: profileImagePrimaryName + profileGallerySlotNames"
provides:
  - "newReloadableCorpusEngine — a corpus control whose cache and store stay reachable after the engine is built"
  - "ROADMAP criterion 4 proven: the viewer-tier floor is evaluated at read time and never stamped"
  - "ROADMAP criterion 5 proven: the eleven media names survive the real viewer-filtered read path"
  - "INV-ACCESS-10 bound"
affects:
  - "test/integration/access (three files)"
  - "docs/architecture/invariants.yaml + invariants.md"
  - "internal/access/profilevis/profilevis_test.go (annotations only)"

tech-stack:
  added: []
  patterns:
    - "corpus mutation + Cache.Reload against an already-built server, so the policy is the only variable between two reads"
    - "direct-SQL snapshots either side of a behavioural change as the discriminating step against a stamped/backfilled implementation"
    - "scrambled-order fixture insertion to separate emitted order from insertion order"

key-files:
  created:
    - test/integration/access/character_readtime_floor_test.go
    - test/integration/access/media_schema_test.go
  modified:
    - test/integration/access/character_profile_read_test.go
    - docs/architecture/invariants.yaml
    - docs/architecture/invariants.md
    - internal/access/profilevis/profilevis_test.go

decisions:
  - "INV-ACCESS-10 flipped to bound: all three clauses of its summary have a genuine asserting site, and the clause→site table is written into the YAML because TestBoundInvariantsAreGenuinelyAsserted cannot detect a partial binding."
  - "The read-time spec asserts READ-TIME EVALUATION and makes no latency claim; ROADMAP criterion 4's 'next load' wording is filed as an owed amendment rather than encoded as a test."
  - "Five already-covered clauses are cited in file headers, never reproved (rule 7zy1161fh1)."
  - "The two deltas stay two specs so a media-schema regression and a read-time-evaluation regression cannot share one red test."

metrics:
  duration: ~55m
  completed: 2026-08-12
  tasks: 3
  commits: 3

actuals:
  tokens: 9618
  tasks: 3
  commits: 3
---

# Phase 5 Plan 05: Read-Time Floor and Media Schema Summary

Two integration specs close ROADMAP criteria 4 and 5 — a corpus mutation that changes a
logged-out visitor's answer with nothing written to any row, and eleven real media rows
through the real viewer-filtered read path — plus one honest, clause-by-clause
INV-ACCESS-10 binding and two filed issues.

## What shipped

### Task 1 — the read-time floor (`977347d35`)

`newCorpusEngine` discarded both the `*policy.Cache` and the `*profileCorpusStore`, so no
caller could reload a second time. A sibling `newReloadableCorpusEngine` now hands all three
back, and `newCorpusEngine` delegates to it — so its **two guards keep exactly one
definition**, its signature is unchanged, and its three shipped call sites in
`character_directory_test.go` are untouched.

`character_readtime_floor_test.go` builds the server **once** over that engine, seeds one
`profile.biography` row at the guest rung and one `profile.pronouns` row on a second
character at the untouched anonymous rung, then runs read A → mutate the clearing set →
`cache.Reload` → read B. The same anonymous viewer, the same server value and the same rows
produce a different field set; the second character's field set is identical under both
corpora, so the mutation moved one attribute's floor rather than every rung.

**The discriminating step** is the pair of direct-SQL snapshots either side:
`characters.version` and the character's complete `(id, name, value, visibility)` property-row
tuple set are asserted byte-identical. Without them the spec degenerates into "the anonymous
read omits field X" — true in the shipped state whether or not the floor is read-time.

### Task 2 — the media schema (`bd253ee3b`)

`media_schema_test.go` inserts twelve real `entity_properties` rows: the primary, the ten
enumerated gallery slots, and the deliberately-unenumerated `profile.image.gallery.10`. The
gallery rows are inserted in a **scrambled order** on purpose — the emitted order must come
from the projection's slot-name slice rather than from insertion order or Go's map iteration,
and an in-order fixture could not tell the two apart. Each slot carries a distinct sentinel,
so a positional mix-up is a readable diff rather than ten identical strings.

A guest-rung viewer receives the primary and all ten slots, positionally, with every sentinel
present in the **marshaled bytes**. The same character read anonymously yields neither, with
every sentinel absent. And the `gallery.10` row — legal, `public`, spelled exactly as its
siblings — is absent from the guest-rung response, which is §8.6's totality rule denying an
unenumerated name rather than defaulting it to the rung its ten siblings sit at.

### Task 3 — the binding and the two issues (`fb8a72e92`)

See the clause→site table below. Both issues are filed rather than hand-edited into
`.planning/`: `ROADMAP.md`, `REQUIREMENTS.md` and `STATE.md` are tool-owned, and no
`gsd-tools` verb rewrites an existing phase's success criteria.

## INV-ACCESS-10: the clause→site table

The summary carries three clauses. `TestBoundInvariantsAreGenuinelyAsserted` cannot detect a
**partial** binding, so the mapping was written out — into the YAML as well as here — before
any annotation was added.

| Clause | Asserting site | Genuine? |
| --- | --- | --- |
| (a) evaluated at **READ TIME** by the default-deny engine, against the attribute **name** and the viewer's **tier** | `test/integration/access/character_readtime_floor_test.go` (R1) — the corpus is the only variable between the two reads; the mutated policy names one attribute, so the decision is keyed on the name, and the only thing that changed is the clearing set, so it is keyed on the tier. Structure inside the clause: `internal/access/profilevis/profilevis_test.go:113` (`TestAttributeVisibleIssuesExactlyTwoEvaluationsSeparatedByTheActionToken`) asserts two evaluations against the same row, separated by the action token. | yes |
| (b) **never stamped** onto `entity_properties.visibility` per row | Same R1 — `characters.version` and the complete property-row tuple set are read by direct SQL either side and asserted identical, so the answer moved with nothing written and no backfill exists. | yes |
| (c) an infrastructure failure resolves **DENY**, never permit and never a silently sparse profile | `internal/access/profilevis/profilevis_test.go:344` (`TestVisibleAttributesAbortsTheWholeCallWhenAnyEvaluationFails`) — requires the error, asserts `ErrEvaluationFailed`, and asserts the returned set is **nil** rather than partially populated. | yes |

All three have a genuine site, so `binding: bound` with `asserted_by` listing the two files.
`docs/architecture/invariants.md` was regenerated with `go run ./cmd/inv-render`; nothing
inside its generated regions was hand-edited, and `inv-render -check` exits 0.

## RED observations (PORTAL-10 rule 4)

| Spec | Hypothesis driven RED | Result |
| --- | --- | --- |
| Task 1 | The pre-widening harness (no `newReloadableCorpusEngine`) | `build failed` — `character_readtime_floor_test.go:241:27: undefined: newReloadableCorpusEngine` |
| Task 1 | A **stamped floor** — temporarily asserting the two field sets are EQUAL | `[FAILED] Expected … to equal …` at the differ assertion. 1 Passed, 1 Failed. |
| Task 2 | An **unenumerated name defaults to the guest rung** — temporarily asserting the `gallery.10` sentinel IS in the bytes | `[FAILED] … to be true` |
| Task 2 | The **gallery order is insertion / map order** — temporarily reversing the expected sentinel order | `[FAILED] Expected …` on the positional comparison |

Each temporary edit was reverted and the suite re-run green before committing.

## Existing coverage CITED, never reproved (rule `7zy1161fh1`)

Both new files carry these in their headers rather than rebuilding them:

| Clause | Cited proof |
| --- | --- |
| `UNIQUE(parent_type,parent_id,name)` rejects a duplicate primary | `TestPropertyRepository_ParentNameUniqueness` (`internal/world/postgres/property_repo_test.go:430`) |
| Clearing a floor is set membership, never ordinal | `TestNoTierFloorPolicyUsesAnOrdinalTierComparison` (`seed_profile_visibility_test.go:260`) |
| A synthetic 4th rung clears neither shipped floor | `TestASyntheticFourthRungClearsNeitherShippedTierFloor` (`tierfloor_test.go:173`) |
| Exactly two evaluations, separated by the action token | `TestAttributeVisibleIssuesExactlyTwoEvaluationsSeparatedByTheActionToken` (`profilevis_test.go:113`) |
| The policy enumerates exactly eleven media names, not twelve | `TestTheElevenMediaNamesAreEnumeratedAndTheTwelfthIsNot` (`seed_profile_visibility_test.go:393`) |

No second gate was added over any of them. **Exactly two** new specs shipped.

## The latency claim, stated honestly

ROADMAP criterion 4 says a viewer-tier change is what a logged-out visitor sees "on the
**next load**". That overclaims and was **not** encoded as an assertion.
`internal/access/policy/cache.go` holds an immutable compiled snapshot refreshed by a poller
whose interval defaults to **10 seconds** when unset (`internal/access/policy/poller.go:95-96`),
so propagation is bounded by that interval, not by the next request. The spec drives
`Cache.Reload` explicitly, names itself after read-time evaluation, and contains no occurrence
of "next load" or "immediately" (`rg -in 'next load|immediately'` → 0 matches). The wording
amendment is owed and is item 1 of issue **#4963**.

## Filed issues

| Issue | Contents |
| --- | --- |
| [#4963](https://github.com/holomush/holomush/issues/4963) | The **four** owed amendments, each quoting current text and replacement: (1) criterion 4's "next load" latency; (2) criterion 4 / PROFILE-12's retirement half moving to Phase 6; (3) `01-SPEC.md` §9.3's missing `SetDefaultCharacter` row; (4) §9.6's missing `CHARACTER_LIMIT_REACHED`, `CHARACTER_NO_STARTING_LOCATION` and `CHARACTER_NOT_PLAYABLE` rows (the first two are 05-03's SPEC amendment 4). |
| [#4964](https://github.com/holomush/holomush/issues/4964) | The **ungated web test runner**: `web/package.json` declares `test:unit` and `check`, but `rg -n 'vitest\|svelte-check\|web:test' Taskfile.yaml` and `rg -n 'vitest\|svelte-check\|test:unit' .github/workflows/` both return **zero** hits. Names both UI-SPEC backstops (the media renderer and the byte counter) as assertions for which this ungated runner is the *sole* gate, proposes a `web:test` task wired into `pr-prep`, and records why it was out of scope here. |

Kept as two issues on purpose: one is a register of SPEC/ROADMAP text owed to the ROADMAP's
owner, the other is a tooling change owed to CI, and folding them would break the
four-amendment tally.

## Deviations from Plan

### Auto-fixed issues

**1. [Rule 1 — Bug] The media spec's fixture created no guest player or session**

- **Found during:** Task 2, first run.
- **Issue:** the `BeforeEach` seeded the location, owner and character but never created the
  guest player or its `player_sessions` row, so `guestToken` resolved to nothing and the
  "guest-rung" read silently collapsed into a second **anonymous** read. `M1` failed on the
  primary image; had the assertion been weaker, the paired control would have compared one
  rung against itself and passed while proving nothing.
- **Fix:** create the guest player (`IsGuest: true`) and a live session via
  `auth.NewPlayerSession` / `auth.HashSessionToken`, mirroring
  `character_profile_read_test.go`'s fixture, with a comment naming the failure mode.
- **Files modified:** `test/integration/access/media_schema_test.go`
- **Commit:** `bd253ee3b`

**2. [Plan-shape] The two read-time behaviours were merged into one `It`**

The plan's acceptance criteria require `rg -c 'Reload\(' character_readtime_floor_test.go` to
return exactly `1`. The behaviour list also asks for an untouched-second-character control.
Written as two `It` blocks each doing its own reload, the grep would return `2`. The control
is folded into `R1` — both characters are read before the mutation and both after — which
satisfies the criterion literally and tightens the pairing (one corpus change, two readers,
one of which must not move). No behaviour was dropped.

## Known state, untouched by design

- `requirements mark-complete` reports `table_unmatched` and silently skips the traceability
  table, as it did for 05-01..05-04. The traceability table was **not** hand-patched.
- Eight Playwright E2E specs remain broken by 05-03's removal of the roster inline create
  form; the replacement ships in 05-06/05-08. Not fixed and not quarantined here.

## Known Stubs

None. Neither new spec carries a stub, a skipped test or an unrun `<verify>`.

## Verification

| Gate | Result |
| --- | --- |
| `task build` | exit 0 |
| `task lint` | exit 0 (includes `lint:invariants` → `inv-render -check`) |
| `task test` | exit 0 — 11636 tests, 4 skipped (pre-existing quarantine) |
| `task test:int -- ./test/integration/access/...` | exit 0 — 109 specs |
| `go run ./cmd/inv-render -check` | exit 0 (generated file matches the YAML) |
| `task test -- ./test/meta/` | exit 0 — 187 tests, including the binding-presence, provenance and genuinely-asserted guards |
| `git diff --stat .planning/` | empty — no `ROADMAP.md`, `REQUIREMENTS.md` or `STATE.md` edit |

Acceptance greps: `Reload(` → 1; `time.Sleep|Eventually` → 0; `SELECT` → 2; `next load|immediately`
→ 0; both `newCorpusEngine` guard messages still present; three unmodified `newCorpusEngine`
call sites; unique gallery names in the media spec → 11; the ten enumerated names set-equal to
the projection's; `profile.image.primary` literal → 1 line; `proto.Marshal` present.

## Self-Check: PASSED

Both created spec files and this SUMMARY exist on disk; all three task commits
(`977347d35`, `bd253ee3b`, `fb8a72e92`) resolve in `git log`.
