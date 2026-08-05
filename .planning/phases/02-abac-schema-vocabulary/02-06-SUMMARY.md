---
phase: 02-abac-schema-vocabulary
plan: 06
subsystem: character-identity
tags: [admission-token, writer-boundary, advisory-lock, uts39, abac, composition-root]
status: complete

requires:
  - charname.Gate + internal/charname/syntax leaf (02-01)
  - migration 000054 identity columns (02-01)
  - blocklist.Subsystem.Matcher() + the BlockList config transport (02-05)
provides:
  - charname.Admitted + (*charname.Gate).Admit — the single-constructor admission token
  - world.CharacterRepository.Rename — the second and last characters.name write
  - worldpostgres.guardSkeleton — D-30 part 2 serialization
  - worldpostgres.SkeletonLookup — the production charname.SkeletonLookup
  - worldpostgres.BackfillCharacterIdentity + its collision-set types (02-12 calls it)
  - setup.NewCharacterNameGate — the ONE gate construction helper all three roots call
  - test/meta/character_name_admission_test.go — the five-rule admission census
  - ExistsByNormalizedName with the transitional predicate at all five sites
affects:
  - internal/world (CharacterRepository.Create/Update/Rename; NormalizeCharacterName removed)
  - internal/auth (CharacterService + GuestService both gated; genesis takes the token)
  - internal/bootstrap/setup, cmd/holomush (all three composition roots wire the gate)
  - internal/testsupport/integrationtest + four integration suites (corpus repair)

tech-stack:
  added: []
  patterns:
    - opaque single-constructor value type as a compile-time capability token
    - transaction-scoped Postgres advisory lock keyed on an FNV-1a hash computed in Go
    - go/parser census keyed on function facts (params, results, literals, calls)
    - transitional SQL predicate keeping every intermediate commit deployable

key-files:
  created:
    - internal/charname/admission.go
    - internal/charname/admission_test.go
    - test/meta/character_name_admission_test.go
    - internal/world/postgres/skeleton_guard.go
    - internal/world/postgres/skeleton_guard_test.go
    - internal/world/postgres/identity_backfill.go
    - internal/world/postgres/identity_backfill_test.go
    - internal/world/postgres/admission_fixture_test.go
    - test/integration/auth/character_create_gate_test.go
  modified:
    - internal/world/repository.go
    - internal/world/validation.go
    - internal/world/character_test.go
    - internal/world/postgres/character_repo.go
    - internal/world/postgres/character_repo_test.go
    - internal/world/postgres/negative_path_test.go
    - internal/world/postgres/postgres_test.go
    - internal/world/worldtest/mock_CharacterRepository.go
    - internal/access/policy/attribute/character_test.go
    - internal/auth/character_genesis.go
    - internal/auth/character_service.go
    - internal/auth/guest_service.go
    - internal/auth/mocks/mock_CharacterRepository.go
    - internal/auth/mocks/mock_GuestCharacterRepository.go
    - internal/bootstrap/setup/adapters.go
    - internal/bootstrap/setup/subsystem.go
    - cmd/holomush/sub_grpc.go
    - internal/testsupport/integrationtest/harness.go
    - test/integration/auth/auth_suite_test.go
    - test/integration/world/world_suite_test.go
    - test/integration/world/character_lifecycle_test.go

decisions:
  - Rename is envelope-atomic through the IN-PACKAGE OutboxStore and takes the intent as a parameter; it MUST NOT be routed through worldMutator.mutate() (double emission)
  - Census rule D is a census of METHODS, so the guardSkeleton helper (which takes a token but writes nothing) is not a member
  - NAME_EMPTY_NORMAL_FORM is mapped to CHARACTER_INVALID_NAME alongside NAME_INVALID_SYNTAX — both were CHARACTER_INVALID_NAME before the gate existed
  - mapGateError REPLACES the code rather than wrapping it, because oops resolves the DEEPEST chain code (#4902)
  - The auth CharacterWriter signature propagation moved from Task 3 into Task 2 so the tree builds
  - Integration fixtures mint REAL tokens through a real Gate with a stub SkeletonLookup; no test escape hatch was added

metrics:
  duration: ~155min
  completed: 2026-08-04

actuals:
  tokens: 59000
  tasks: 3
  commits: 3
---

# Phase 02 Plan 06: The Gated Character-Name Writer Boundary Summary

`characters.name` is now writable by exactly two methods, both of which take a
`charname.Admitted` whose only constructor is `(*charname.Gate).Admit` — so a
write site that skipped the name gate is not expressible in Go rather than
merely caught by a test — and all three production composition roots construct
that gate.

## What was built

**The token.** `charname.Admitted` is an opaque value whose populated form is
unexported state, so the only value another package can build is the zero value,
and `IsZero()` detects it. `Admit` forwards `...CheckOption` verbatim, so
`ExcludingCharacter` can mint a token for `01-SPEC.md:702-706`'s settled
case-variant rename (B-18) — without which that rename could not be expressed at
all, since `Admit` is the type's sole constructor.

**The writer boundary.** `Create` binds all four identity columns from the
TOKEN and no longer reads `char.Name` (it refreshes it instead), so a caller that
mutated the struct after admission cannot slip an unadmitted string past the
gate. `Update` no longer writes `name` at all, closing the pre-existing ungated
write at `character_repo.go:79`. `Rename` is new: version-predicated, writing all
four columns in one statement.

**Serialization (D-30 part 2).** `guardSkeleton` takes a transaction-scoped
advisory lock keyed on an FNV-1a hash computed **in Go** (not `hashtext`, so every
replica computes the same key) and re-checks the skeleton inside the write
transaction, applying the same self-exclusion and fail-closed-on-NULL rules the
gate applies. Twenty repeated concurrent-create iterations against real Postgres
produce exactly one success and one `NAME_CONFUSABLE` each time.

**The backfill (D-30 part 3).** `BackfillCharacterIdentity` reports BOTH
normalized-name and **skeleton** collision sets. The skeleton scan is the half a
`normalized_name`-only scan can never reach: NFKC deliberately does not collapse
cross-script confusables, so a pre-existing confusable pair has *different*
normalized names by construction, and `000056`'s unique index would pass straight
over it. It takes a consumer-side `database/sql` executor interface, so no
`internal/store` import cycle is created.

**Production composition.** One helper, `setup.NewCharacterNameGate`, is called
from all three roots (`internal/bootstrap/setup/subsystem.go:224`,
`cmd/holomush/sub_grpc.go:452` and the same value at `:492` for `GuestService`).
It fails closed on a nil block-list subsystem and reads the matcher through
02-05's transport.

## Composition-root census

`rg -n 'NewCharacterService|NewGuestService|NewCharacterGenesisService' --type go`
over non-test files returns **13 lines**: 3 declarations, 5 production
construction sites (2 genesis + 2 character + 1 guest), 3 doc-comment lines, and
2 harness sites. The three roots the plan names are all covered.

## Gates demonstrated RED (PORTAL-10 rule 4)

The admission census was demonstrated RED against **the tree as it actually
stood** — not a hypothetical pre-fix state constructed for the demonstration.

```
task test -- -run 'TestEveryCharacterNameWrite|TestAdmittedHasExactlyOne|TestTheSetOfNameAdmitting|TestAdmissionCensus' ./test/meta/
```

**Exit 201.** Four of the five rules fired, on the real pre-fix writer boundary:

| Rule | Observed failure |
|---|---|
| A (gated) | `character_repo.go: (*CharacterRepository).Create`, `(*CharacterRepository).Update` — name writes with no `charname.Admitted` parameter |
| B (identity-coherent) | both omit `normalized_name, name_skeleton, name_skeleton_unicode_version` |
| D (set equality) | expected name-admitting methods absent from the tree: `[(*CharacterRepository).Create (*CharacterRepository).Rename]` |
| E (serialized) | both `Create` and `Update` — no `guardSkeleton` call |

Rule C was GREEN and non-vacuous from the moment `admission.go` landed (exactly
one function under `internal/charname` returns `Admitted`, and it is
`(*Gate).Admit`). All five synthetic positive controls passed in the same run
(12 tests, 4 failures). Every rule is GREEN after Task 2 and stayed green through
Task 3.

## Verification

| Check | Result |
|---|---|
| `task test` (full untagged suite) | **exit 0** |
| `task test:int` (full) | **exit 0** — 11,231 tests, 7 skipped, 152.9s |
| `task lint` | **exit 0** |
| `task fmt:check` | **exit 0** |
| `task build` | **exit 0** |
| `task mocks:generate` then re-run | no drift; all three regenerated mocks committed |

Docker was available, so the integration half ran in full. `task build` was the
command used for the untagged compile enumeration; the three integration-tagged
`Create` callers the plan names (`guest_reaper_race_test.go`,
`guest_reaper_tombstone_test.go`, `auth_suite_test.go`) are invisible to it and
were reached by `task test:int`, which is what caught them.

### Mechanical acceptance criteria

| Criterion | Result |
|---|---|
| `UPDATE characters SET name` in `character_repo.go` (comments stripped) | **0** |
| `ExistsByName\b` files in the tree | **0** |
| `ExistsByNormalizedName` declaration sites | **5**, each with `excluding *ulid.ULID` and the "REMOVE with migration 000056" note |
| `NormalizeCharacterName` occurrences | **0** |
| `hashtext` in `skeleton_guard.go` | **0** |
| `charname.Gate` / `.Check(` in `identity_backfill.go` | **0** |
| `"winner", got.Name` in `character_repo_test.go` | **0** |
| `charname.Gate{` literals in either composition root | **0** (both call the helper) |
| `blocklist.NewCache`/`Compile` in either root | **0** |
| `strings.ReplaceAll` inside `CreateGuest`'s body | **0** |
| `ValidateCharacterName` in live auth admission code | **0** |
| `go list -deps ./internal/world/postgres \| rg 'holomush/internal/store$'` | **0** |
| `go list -deps ./internal/charname \| rg 'holomush/internal/world$'` | **0** (D-28) |

## Deviations from Plan

### Deliberate departures from the plan text

**A. `Rename` takes the envelope intent as a parameter and writes it through the
in-package `OutboxStore`.**

The plan asks `Rename` to "write the outbox envelope inside its own transaction"
(B-12 / INV-WORLD-4). Taken literally that is not constructible: a
`wmodel.EnvelopeIntent` carries a `GameID` and an `Actor`, and
`CharacterRepository` knows neither and has no channel to learn them. Inventing
values for them inside the repository would be worse than the gap.

What landed satisfies the *property* B-12 names — a caller that discards the
returned delta still produces a feed entry — with the smallest new surface:
`Rename(ctx, id, name, expectedVersion, intent)` writes the row and the envelope
in ONE transaction through `worldpostgres.OutboxStore`, which lives in the SAME
package, so this costs no new import edge and no wiring change at any of the
three sites that build a `CharacterRepository`. An integration spec calls
`Rename` directly, discards the delta, and observes the outbox row.

The consequence is recorded in `Rename`'s doc comment as a rule for Phase 3:
**`Rename` MUST NOT be routed through `worldMutator.mutate()`**, which writes an
envelope of its own from the returned delta and would emit two per rename.

**B. The `auth.CharacterWriter` signature propagation moved from Task 3 into
Task 2, so the tree builds.**

The plan's split leaves the tree uncompilable between Tasks 2 and 3, and Task 2's
own `<verify>` (`task test:int -- ./internal/world/postgres/`) is unreachable in
isolation because that test package transitively imports
`internal/bootstrap/setup`. Threading the token through `CharacterWriter` and
`CharacterGenesisService.Create` in Task 2 shrinks the non-building window to the
`auth.CharacterGenesis` interface alone. Task 1's commit is a deliberate TDD RED
(the census), and Task 2's leaves `internal/auth` uncompiled; both are closed by
Task 3, and `task build`/`task lint`/`task test:int` are green at HEAD.

**C. Census rule D counts METHODS, not every token-taking function.**

`guardSkeleton` takes a `charname.Admitted` and is not a name-admitting method —
it READS the skeleton to serialize the write and writes nothing. The plan's own
wording is "the set of `internal/world/postgres` **methods** taking a
`charname.Admitted`", so the derived side is restricted to declarations with a
receiver. Rules A and E still cover free functions: they key on functions that
write `characters.name`, receiver or not. The narrowing is recorded at the
`IsMethod` field.

**D. `NAME_EMPTY_NORMAL_FORM` is mapped to `CHARACTER_INVALID_NAME` as well as
`NAME_INVALID_SYNTAX`.**

The plan names only the syntactic remap. But an empty submission produced
`CHARACTER_INVALID_NAME` before the gate existed (the old "cannot be empty"
syntactic rule), and `Gate.Check` now refuses it one step earlier with
`NAME_EMPTY_NORMAL_FORM`. Leaving it unmapped would change a caller-visible code
for a refusal this path already had a code for. Both remaps **replace** the code
rather than wrapping it: `errutil.AssertErrorCode` and `oops.AsOops(...).Code()`
both resolve the DEEPEST chain code (issue #4902), so a wrap would have left
callers seeing the inner code and the contract would have changed silently.

**E. Integration fixtures mint real tokens with a stub `SkeletonLookup`.**

There is no test escape hatch for `Admitted` by design, so every fixture runs a
real `charname.Gate`. What the fixtures stub is the gate's *corpus read*, which
is a pre-check; the assertion that matters (serialization, fail-closed,
self-exclusion) is made by `guardSkeleton` against the REAL corpus in
`skeleton_guard_test.go`, which stubs nothing.

**F. Fixture character names were rewritten to be gate-admissible and
corpus-unique.**

`syntax.ValidateName` admits Unicode letters and single spaces only, so ~40
fixture names carrying hyphens or ULID suffixes (`"guard-create"`,
`"AAA-" + ulid.Make().String()`) were never legal character names and are now
refused. They were replaced with `charFixtureName(prefix)` — the prefix plus
eight random lowercase letters from `crypto/rand`. Uniqueness now matters in a way
it did not before: a second character whose SKELETON matches a live row is
refused, and these tests share one database.

### Auto-fixed issues

**1. [Rule 1 — Bug] The confusable fixture pair was a cross-script splice**

- **Found during:** Task 3, first `task test:int` run
- **Issue:** `confusablePair` built `latinPrefix + " сосоа"`, which mixes Latin
  and Cyrillic. `Gate.Check` refuses that with `NAME_MIXED_SCRIPT` (§6.1.2
  Mechanism A) BEFORE it ever reaches the skeleton step, so no token could be
  minted and the whole D-30-part-2 proof never ran.
- **Fix:** both forms are now drawn from a homoglyph alphabet (`aceopxy`) and the
  twin is transliterated **wholesale** to Cyrillic, so each side is single-script
  and the pair is still skeleton-equal with differing normalized names. The
  fixture asserts both of those properties before the spec body runs.
- **Files:** `internal/world/postgres/skeleton_guard_test.go`
- **Commit:** `66f1e96ef`

**2. [Rule 3 — Blocking] Every harness-backed guest login exhausted its retries**

- **Found during:** Task 3, first `task test:int` run (9 failures across
  `integrationtest`, `presence`, `privacy`)
- **Issue:** `GUEST_NAME_EXHAUSTED` after 10 attempts. Root cause is carry-forward
  fact A: `000001_baseline.sql:397` seeds a bootstrap character with NO
  `name_skeleton`, so a freshly migrated database is D-30-unverifiable and the
  gate correctly refused **every** generated candidate. The gate was right; the
  harness had no corpus repair.
- **Fix:** `integrationtest.Start` now repairs the corpus before returning, and
  the world suite and the genesis suite do the same at their own setup. Stands in
  for plan 02-12's `000055` Go migration, the same stand-in 02-01 and 02-05
  introduced.
- **Files:** `internal/testsupport/integrationtest/harness.go`,
  `test/integration/world/world_suite_test.go`,
  `internal/auth/character_genesis_integration_test.go`
- **Commit:** `66f1e96ef`

**3. [Rule 3 — Blocking] A direct-SQL lifecycle fixture broke the corpus mid-suite**

- **Found during:** Task 3, second `task test:int` run
- **Issue:** `insertCharacterWithStatus` (the INV-WORLD-6 name-reservation spec's
  fixture) inserts by direct SQL with no identity columns. That made the corpus
  unverifiable *after* setup, so the reclaim the spec expects to SUCCEED was
  refused `NAME_SKELETON_UNVERIFIABLE`.
- **Fix:** the fixture writes the three derived columns alongside the name,
  exactly as the gated writer does, with a comment saying why.
- **Files:** `test/integration/world/character_lifecycle_test.go`
- **Commit:** `66f1e96ef`

**4. [Rule 1 — Bug] Two name-literal assertions broke on unique fixture names**

`GetByLocation` asserted `[]string{"Alice","Bob"}` and `GetNamesByIDs` asserted
`"alice"`/`"bob"`. Both now assert the seeded `char.Name` values (sorted, for the
ordering claim), so the assertions still test ordering and mapping rather than
literals. **Commit:** `efdbe7813`.

**5. [Rule 1 — Bug] Four lint findings on first-pass code**

Three `errcheck` on `rows.Close()` in error paths (line-scoped `//nolint:errcheck`
with the reason: the scan already failed and the close error would mask it) and
one `gocritic unnamedResult`/`paramTypeCombine` on `SkeletonExists`. Never
widened `.golangci.yaml`. **Commit:** `66f1e96ef`.

## Broken windows

**Window #10 CLOSED.** 02-05's ledger entry — "declares the block-list transport
but constructs no `charname.Gate`; until 02-06 consumes `Matcher()` at the three
composition roots, no production create path evaluates the block list" — is
marked `fixed`. All three roots now call `setup.NewCharacterNameGate`, which reads
`cfg.BlockList.Matcher()`, and
`TestProductionCreatePathRejectsABlockListedName` drives the production
`CharacterService.Create` and observes `NAME_BLOCKED`.

No new windows opened.

## Requirements

Frontmatter names `IDENT-06`, `IDENT-07` and `IDENT-09`.

**Read `IDENT-06` and `IDENT-09` as still genuinely open**, for the reason the
executor briefing warns about (fact D): `requirements.mark-complete` has no
partial-credit model and several plans share these ids. `IDENT-06` (confusable
rejection) and `IDENT-09` (uniqueness) are **not** fully discharged here — the
`normalized_name` UNIQUE index, the `000055` backfill migration and the
concurrent-claim proof are plan `02-12`'s, and this plan's transitional predicate
exists precisely because they have not landed. `IDENT-07` (block list) IS
discharged here: 02-05 built the checking and this plan wired it into both
production create paths.

`.planning/REQUIREMENTS.md` was NOT hand-edited.

## Known Stubs

None in production code. Three deliberate, documented seams remain, each with a
named owner:

| Seam | Owner |
|---|---|
| The `normalized_name IS NULL` transitional branch at all five existence-check sites, and the test asserting a NULL-normalized row IS reported as existing | plan `02-12`, in the same atomic unit as `000055` + `000056` |
| `BackfillCharacterIdentity` has no production caller yet | plan `02-12`'s `000055` Go migration |
| The `backfillCharacterSkeletons` fixture stand-ins in four test trees | plan `02-12`'s `000055`, which makes them unnecessary |

## Threat Flags

None. Every security-relevant surface this plan introduces is already enumerated
in its threat register as T-02-30/31/73–79, T-02-93/94/95. The one addition
outside it — `worldpostgres.SkeletonLookup` — is a read-only parameterised query
over a column this plan already writes, introducing no new write path and no new
trust boundary.

## Invariants

This plan pins no registry invariant and writes no `// Verifies:` annotation, as
its verification-integrity section requires. It works WITHIN `INV-WORLD-4`'s
writer-boundary fence rather than amending it:
`test/meta/world_sql_fence_test.go` is **unmodified** and
`TestNoRawWorldSQLOutsideWriterBoundary` is green.
`docs/architecture/invariants.yaml` is unmodified. No ad-hoc `INV-NAME-*` family
was minted.

## Post-merge fix: the create path reported "request failed" for a duplicate name

CI on PR #4941 caught a **user-visible regression this plan introduced**.
`web/e2e/negative-journeys.spec.ts:253` expected `/already taken/i` on
`p.text-destructive` and got `"request failed"` (103 passed, 1 failed). E2E is a
required check; `task pr-prep`'s fast lane does not run it, which is why every
local gate in this plan was green.

### The defect

An EXACT duplicate has an identical skeleton, so `charname.Gate.Check` step 5
(`internal/charname/gate.go:177`) intercepted it and returned `NAME_CONFUSABLE`.
The friendly uniqueness pre-check that exists for precisely this case sat AFTER
`Gate.Admit` and was therefore **unreachable for it** —
`oops.Code("CHARACTER_NAME_TAKEN")` was dead code on the exact-duplicate path.
`internal/grpc/auth_errors.go` then had no `case` for `NAME_CONFUSABLE` and fell
through to `msgGenericRequestFailed`.

The irony worth recording: the pre-check's own comment called it *"a UX
affordance"*, and the gate ordering had silently made it dead for the one case
it exists to serve.

### The false premise that shipped

`mapGateError`'s comment justified passing the gate's codes through untouched
with:

> Everything else the gate can say — `NAME_CONFUSABLE`, `NAME_BLOCKED`,
> `NAME_SKELETON_UNVERIFIABLE`, `NAME_MIXED_SCRIPT` — is a NEW refusal with no
> legacy code to preserve.

**That premise was false for the exact-duplicate case.** "Already taken" is the
oldest refusal on this path and certainly did have a code. The comment is
corrected in place rather than deleted, so the next reader sees what was wrong
and why the obvious fix is not the right one.

### What was NOT done, deliberately

`NAME_CONFUSABLE` is **not** mapped onto `CHARACTER_NAME_TAKEN`. A name
confusable with a *different* player's character would then claim to be "already
taken" — asserting something untrue and disclosing more about the corpus than
§6.1.2 intends. D-30's `guardSkeleton` advisory-lock guard, the post-gate
existence check and the 23505 handler are all untouched: those are the
guarantee, and this is only the affordance in front of it.

### RED-then-GREEN proof

The durable guard is Go-level; the Playwright spec is only the UX confirmation.

| Step | Command | Exit |
|---|---|---|
| RED | `task test:int -- ./test/integration/auth/` | **201** — both duplicate subtests returned `NAME_CONFUSABLE` where `CHARACTER_NAME_TAKEN` was expected; the confusable control already passed |
| GREEN | same | **0** |

The spec asserts three cases on one fixture: an exact duplicate and a case
variant are `CHARACTER_NAME_TAKEN`, and a whole-script Cyrillic homoglyph is
still `NAME_CONFUSABLE`. Without that third case the fix could have been "always
report taken" and looked correct.

### Second, independent gap: four codes rendered as "request failed"

`NAME_CONFUSABLE`, `NAME_BLOCKED`, `NAME_MIXED_SCRIPT` and
`NAME_SKELETON_UNVERIFIABLE` had **no case** in `sanitizeAuthError`, so every
gate refusal reached the client generically. Each now has a sanitized constant
and case:

- confusable names NO colliding character; blocked names NO pattern or index —
  §6.1.2 and the block-list design both forbid these becoming enumeration
  oracles
- `NAME_SKELETON_UNVERIFIABLE` reads as **transient** ("try again shortly"),
  because the D-30 fail-closed state means the corpus could not answer yet, not
  that the name was rejected

`auth_errors_test.go` gains a row per code plus a distinctness guard — four
codes collapsed onto one message would satisfy every table row while reproducing
the generic-message defect one layer in. Proven falsifiable: collapsing
`NAME_BLOCKED` onto the confusable message fails it (exit **201**) naming both
codes.

### Consequence recorded

Moving the pre-check ahead of the gate costs one existence lookup on the
invalid-name path (`"123"` normalizes fine; it is the *syntactic* rule inside the
gate that rejects it). Two unit subtests were updated: the invalid-name case
gains that expectation, and the empty-name case deliberately gains **none** —
`charname.Normalize` fails before any lookup, and mockery's strict-call
assertion is what proves it.

### Gates

| Check | Result |
|---|---|
| `task test` | **exit 0** — 11,022 tests |
| `task test:int` | **exit 0** — 11,484 tests, 7 skipped |
| `task lint` | **exit 0** |
| `task fmt:check` | **exit 0** |
| `task build` | **exit 0** |

Playwright was not run locally (it needs the full compose stack); CI confirms
it. The chain was verified by reading: `CreateBound` → the pre-check →
`CHARACTER_NAME_TAKEN` → `sanitizeAuthError` → `msgCharacterNameTaken`
("character name is already taken") → `CreateCharacterResponse.ErrorMessage` →
`p.text-destructive`, which satisfies `/already taken/i`.

Commits: `47a620958` (ordering), `306ff5e8d` (sanitized messages).

## Self-Check: PASSED

All nine created artifacts exist on disk. All three commits (`706e7e53f`,
`efdbe7813`, `66f1e96ef`) resolve in `git log`. Working tree clean before this
document.
