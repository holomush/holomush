---
phase: 02-abac-schema-vocabulary
plan: 04
subsystem: world
tags: [lifecycle, abac, invariants, character]
status: complete
requires:
  - "migration 000054_character_identity_and_lifecycle.sql (plan 02-01) — the status and last_active_at columns"
  - "internal/world/postgres.CharacterRepository — the three full-row reads"
  - "internal/grpc.CoreServer.SelectCharacter — the tree's only character-selection path"
provides:
  - "world.Status / StatusActive / StatusRetired / StatusIdle — the closed lifecycle vocabulary"
  - "world.ParseStatus — exact-literal parse, INVALID_CHARACTER_STATUS on anything else"
  - "world.Selectable — the ONE exhaustive selectability predicate with a denying default"
  - "world.NeverActive — the last_active_at never-active sentinel"
  - "world.Character.Status / world.Character.LastActiveAt"
  - "test/integration/world/world_suite_test.go — real policy.Engine, world.Service, CharacterReapingService, CharacterService and CoreServer for the world suite"
  - "INV-WORLD-5 and INV-WORLD-6 bound in the registry"
affects:
  - "internal/grpc.CoreServer.SelectCharacter now REFUSES a non-active character (new refusal on an existing surface)"
  - "internal/testsupport/integrationtest harness + test/integration/auth adapters now read characters.status"
tech-stack:
  added: []
  patterns:
    - "closed string vocabulary + exhaustive switch with a denying default (INV-WORLD-5)"
    - "zero-value lifecycle state handled at the CALL SITE (logged, fail-closed) rather than by softening the predicate (B-21)"
    - "full-entity reads carry lifecycle fields; id/name projections stay projections and say so"
key-files:
  created:
    - internal/world/lifecycle.go
    - internal/world/lifecycle_test.go
    - test/integration/world/character_lifecycle_test.go
  modified:
    - internal/world/character.go
    - internal/world/character_test.go
    - internal/world/postgres/character_repo.go
    - internal/grpc/auth_handlers.go
    - internal/grpc/auth_handlers_test.go
    - test/integration/world/world_suite_test.go
    - test/integration/auth/auth_suite_test.go
    - internal/testsupport/integrationtest/harness.go
    - docs/architecture/invariants.yaml
    - docs/architecture/invariants.md
decisions:
  - "INV-WORLD-6's summary was corrected BEFORE it was bound: the shipped 'ONLY path' wording was already falsified by auth.CharacterReapingService, and binding it unamended would have written a fabricated guarantee into the registry."
  - "INV-WORLD-5's binding condition held — the spec drives CoreServer.SelectCharacter and names world.Selectable nowhere — so it was flipped to bound rather than left pending with a coverage-gap issue."
  - "The world integration suite now builds a REAL policy.Engine over the seeded corpus (admin permit) rather than a canned-decision fake, so the authorized delete is genuinely authorized."
  - "Both test-only authCharRepoAdapter.ListByPlayer projections were widened to read status; the alternative — softening Selectable's default — was rejected because the denying default IS INV-WORLD-5."
metrics:
  duration: ~85m
  completed: 2026-08-04
actuals:
  tokens: 46000
  tasks: 3
  commits: 3
---

# Phase 2 Plan 04: Character Lifecycle Vocabulary Summary

The three-value `characters.status` vocabulary is now a closed Go type with one
exhaustive selectability predicate that `CoreServer.SelectCharacter` routes
through, and the two Phase-2 world invariants are bound to integration specs
that were each observed failing against a deliberately broken production path.

## What Shipped

**Task 1 — the vocabulary and its one predicate** (`e1ee54690`)

`internal/world/lifecycle.go` defines `Status` with `StatusActive` /
`StatusRetired` / `StatusIdle`, `ParseStatus` (exact lowercase literals — no
`strings.ToLower`, no `TrimSpace`, no `EqualFold`; anything else is
`INVALID_CHARACTER_STATUS`), `Selectable` (an exhaustive `switch` whose
`default:` arm denies), and `NeverActive int64 = 0`.

`world.Character` gained `Status` and `LastActiveAt`. `NewCharacterWithID` starts
a character active and never-active; `Validate()` rejects a status outside the
closed set.

**Full-row `SELECT` census.** The plan's lockstep obligation was verified by grep
rather than trusted from prose:

```
rg -n 'SELECT id, player_id, name, description, location_id, created_at, version' \
   internal/world/postgres/character_repo.go
```

returned **exactly three** sites — `:33` (`Get`), `:162` (`GetByLocation`),
`:183` (`ListByPlayer`) — repo-wide, with no occurrences outside that file. All
three were widened with `status, last_active_at` together, and both scan sites
(`scanCharacterRow`, `scanCharacters`) moved with them. The earlier draft's
"five" was wrong; the correction the review made is confirmed.

`GetNamesByIDs` and `ListAll` were **not** widened. Each gained a doc comment
stating that its result carries no lifecycle fields and that a selectability
decision MUST NOT be made from it. `rg -n 'LastActiveAt'
internal/world/postgres/character_repo.go` shows only scan targets — no INSERT
or UPDATE assigns it, so no `last_active_at` write seam ships (D-24 keeps it in
Phase 3).

**`SelectCharacter` sites that decide selectability: exactly one.** The handler
consulted status nowhere before this change (`ListByPlayer` at `:265`, then an
ownership loop at `:271-277` accepting any owned character). One
`world.Selectable` call was inserted immediately after the ownership check,
returning the existing `SelectCharacterResponse{Success: false, ErrorMessage: …}`
shape — a new refusal reason on an existing surface, not a new error class. No
second site exists.

**Task 2 — the invariant proofs** (`7fc0f0809`)

`test/integration/world/world_suite_test.go` now assembles the real
collaborators: a genuine `policy.Engine` (real `attribute` providers, the real
compiled seed corpus) plus `world.Service`, `auth.CharacterReapingService`,
`auth.CharacterService` and a `CoreServer`. Authorization for the delete comes
from the seeded admin permit (`permit(principal is character, action, resource)
when { "admin" in principal.character.roles }`); only the *role source* is a
fixture.

`test/integration/world/character_lifecycle_test.go` carries both specs plus the
repository projection assertions. `internal/grpc/auth_handlers_test.go`'s
`TestSelectCharacter` gained idle / retired / lifecycle-active-control subtests —
the fast pin reachable by `task test`.

**Task 3 — the registry** (`82514fdb5`) — see *Registry amendment* below.

## Verification

| Gate | Result |
|------|--------|
| `task test -- ./internal/world/... ./internal/grpc/` | exit 0 |
| `task test` (whole repo, unit) | exit 0 |
| `task test:int -- ./test/integration/world/` | exit 0 |
| `task test:int` (full suite, harness was touched) | exit 0 — 11120 tests |
| `task test -- ./test/meta/` | exit 0 — 109 tests |
| `go run ./cmd/inv-render -check` | exit 0 |
| `task lint` | exit 0 |
| `task fmt:check` | exit 0 |

Acceptance greps:

| Check | Value |
|-------|-------|
| `rg -v '^\s*//' internal/world/lifecycle.go \| rg -o 'strings\.ToLower\|strings\.TrimSpace\|EqualFold' \| wc -l` | 0 |
| `rg -o 'last_active_at' internal/world/postgres/character_repo.go \| wc -l` | 3 |
| `rg -c '// Verifies: INV-WORLD-5' <spec>` / `INV-WORLD-6` | 1 / 1 |
| `rg -o 'Skip\(' <spec> \| wc -l` | 0 |
| `rg -o 'world\.Selectable' <spec> \| wc -l` | 0 |
| `rg -o 'policytest\.AllowAllEngine\|DenyAllEngine' <spec> \| wc -l` | 0 |
| `rg -c 'SelectCharacter' <spec>` | 6 |

### Gates demonstrated RED (verification-integrity rule 4)

Both were **observed**, not assumed:

1. **INV-WORLD-5.** The `world.Selectable` gate was temporarily removed from
   `auth_handlers.go` and the suite re-run: `Ran 92 of 92 Specs … FAIL!` with the
   failure attributed to *INV-WORLD-5: lifecycle-exhaustive character selection*.
   The gate was restored (`git diff` against HEAD clean) and the suite went green.
2. **INV-WORLD-6.** `world.Service.DeleteCharacter`'s
   `mutator.deleteCharacter` call was temporarily short-circuited and the suite
   re-run: `Ran 92 of 92 Specs … FAIL!` attributed to *INV-WORLD-6: retirement
   preserves the name reservation*. Restored, green.

   **Honest qualification.** INV-WORLD-6 asserts a guarantee the tree already
   held — this plan changed no code it pins, so its RED had to be induced by
   breaking production rather than observed against a pre-fix state. The probe
   above is what establishes non-vacuity. The spec is also self-checking by
   construction: an always-failing `reclaim` breaks the post-delete assertions
   and an always-succeeding one breaks the retire assertion.

## Registry amendment (INV-WORLD-6), before/after

Recorded verbatim so plan `02-11`'s Amendment E can apply the matching
`01-SPEC.md` §13 correction from it rather than re-deriving it.

**Before:**

> RETIRE-PRESERVES-NAME: retiring a character leaves its row and its name
> reservation intact; the irreversible character delete
> (world.Service.DeleteCharacter) is the ONLY path by which a character name
> becomes claimable again. Retire MUST NOT release the name — a freed name
> claimed by a new character inherits the identity of every display name already
> denormalized into immutable event payloads and published scene archives, which
> no later write can reach.

**After:**

> RETIRE-PRESERVES-NAME: retiring a character leaves its row and its name
> reservation intact; a character name becomes claimable again only through a
> sanctioned tombstone-emitting hard delete, and there are exactly TWO such paths
> — world.Service.DeleteCharacter (the in-world authorized delete) and
> auth.CharacterReapingService (guest reaping, the second sanctioned out-of-world
> writer under INV-WORLD-4). Both are asserted; an enumeration proven on one path
> while claiming two is a fabricated guarantee no meta-test can detect. Retire
> MUST NOT release the name — a freed name claimed by a new character inherits
> the identity of every display name already denormalized into immutable event
> payloads and published scene archives, which no later write can reach.

The rationale sentence is preserved verbatim. The id and scope are unchanged and
no `legacy:` key was added — the enumeration was corrected, not renumbered.

**Binding state after this plan:** INV-WORLD-5 `bound`, INV-WORLD-6 `bound`, both
with `asserted_by: ["test/integration/world/character_lifecycle_test.go"]`.
INV-WORLD-7 remains `pending` with no `asserted_by` (Phase 4). The five
`INV-ACCESS` / `INV-PRIVACY` entries were untouched.

INV-WORLD-5's flip was conditional; the condition was checked mechanically before
writing `binding: bound` — `rg -n 'SelectCharacter' <spec>` matches, and
`rg -n 'world\.Selectable' <spec>` does not. No coverage-gap issue was needed.

## Deviations from Plan

### Auto-fixed issues

**1. [Rule 3 — Blocking] Two test-only `authCharRepoAdapter.ListByPlayer`
projections did not read `characters.status`**

- **Found during:** Task 2 (predicted by B-21, but the specific sites were not
  named in the plan).
- **Issue:** `internal/testsupport/integrationtest/harness.go:1572` and
  `test/integration/auth/auth_suite_test.go:321` each hand-roll a `ListByPlayer`
  that selects `id, player_id, name, description, location_id, created_at` only.
  Both feed `CoreServer.SelectCharacter`. With the new lifecycle gate, every
  character they returned carried a blank `Status` and hit the call-site
  fail-closed branch — breaking `test/integration/auth` and every harness-backed
  suite that selects a character.
- **Fix:** widened both `SELECT`s to carry `status` and populated `Character.Status`
  through `world.ParseStatus`, with a comment naming why. The predicate's denying
  default was NOT softened — that default is the invariant.
- **Files:** `internal/testsupport/integrationtest/harness.go`,
  `test/integration/auth/auth_suite_test.go` (neither in `files_modified`).
- **Commit:** `7fc0f0809`.

**2. [Rule 1 — Bug] `internal/world/character_test.go` fixtures asserted a row
shape production no longer produces**

- **Found during:** Task 1.
- **Issue:** 15 hand-built `world.Character{…}` literals left `Status` blank and
  expected `Validate()` to pass; the strict validation the plan requires rejects
  the empty string.
- **Fix:** set `Status: world.StatusActive` on each fixture. `Character.Validate()`
  is called from exactly one production site (`NewCharacterWithID`), so no
  production caller was affected.
- **Commit:** `e1ee54690`.

**3. [Rule 3 — Blocking] The new integration `BeforeEach` tripped
`locations_owner_id_fkey`**

- **Found during:** Task 1.
- **Issue:** `locations.owner_id` references `characters`, so a scene left owned
  by an earlier spec's character blocks the `DELETE FROM characters` the
  documented ordering requires.
- **Fix:** `UPDATE locations SET owner_id = NULL` precedes the deletes; the
  documented `entity_properties`-before-`characters` ordering (RESEARCH P-11) is
  otherwise followed exactly, with its reason carried in the comment.
- **Commit:** `e1ee54690`.

### Scope notes (no action taken)

- **`worldtest` mock returns.** The plan asks that "the `worldtest` mock returns
  set `StatusActive` explicitly". `internal/world/worldtest/` contains only
  generated mockery mocks with no canned returns — there is nothing to set. The
  equivalent obligation landed on the `internal/grpc` `SelectCharacter` fixtures,
  which now set `StatusActive` explicitly.
- **`TestSelectCharacterRefusesANonActiveCharacter` was folded into
  `TestSelectCharacter`.** Task 1's B-21 pin and Task 2's criterion both wanted
  handler-level idle/retired/active subtests. Rather than ship two overlapping
  test functions, the idle/retired/active-control cases live as table entries in
  `TestSelectCharacter` (Task 2's criterion, literally) and the empty-status
  fail-closed-plus-log case stays as its own function (Task 1's criterion).
- **Character WRITE boundary untouched.** Nothing here changes how characters are
  written; `charname.Admitted` on `CharacterRepository.Create` remains plan
  `02-06`'s.

### Requirement checkbox observation

`IDENT-09` is this plan's frontmatter requirement, and `requirements.mark-complete
IDENT-09` was run per that frontmatter — `.planning/REQUIREMENTS.md:124` is now
`[x]`. **That flip is premature and is flagged here rather than worked around.**
IDENT-09's text is about the **unique index on a stored normalized character
name**, which plan `02-12` lands, not this one; the traceability row at `:356`
still reads `Pending`, so the two halves of the file now disagree. This plan
*advances* IDENT-09 — INV-WORLD-6 asserts the reservation semantics over exactly
the column that index will enforce — but does not satisfy it. The verb has no
partial-credit model and several Phase-2 plans share requirement IDs, so the flip
is a known artifact of that; `.planning/REQUIREMENTS.md` was NOT hand-edited to
compensate.

## Known Stubs

None. No hardcoded empty values, placeholder text, or unwired data sources were
introduced. `rg -o 'Skip\(' test/integration/world/character_lifecycle_test.go`
returns 0, so neither annotated spec is a Skip-only placeholder.

## Threat Flags

None. The two mitigations this plan owed (`T-02-17` exhaustive-read fail-open,
`T-02-18` name released on retire) are the two bound invariants; `T-02-89`
(fabricated INV-WORLD-6 binding) is closed by the summary amendment landing
before the binding and by the guest-reaping half being asserted. No new network
endpoint, auth path, file access pattern, or trust-boundary schema change was
introduced — the only new refusal is a domain predicate on an existing RPC.

## Self-Check: PASSED

Files verified present:

- `internal/world/lifecycle.go` — FOUND
- `internal/world/lifecycle_test.go` — FOUND
- `test/integration/world/character_lifecycle_test.go` — FOUND

Commits verified in `git log`:

- `e1ee54690` — FOUND
- `7fc0f0809` — FOUND
- `82514fdb5` — FOUND
