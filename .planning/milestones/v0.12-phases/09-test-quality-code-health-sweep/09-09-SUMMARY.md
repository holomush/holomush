---
phase: 09-test-quality-code-health-sweep
plan: 09
subsystem: testing
tags: [qual-03, weak-tests, compile-time-assertion, interface-satisfaction, re-derivation, alias-repository]

requires:
  - phase: 09-01
    provides: the phase's re-derivation of the archived ec22.15/ec22.16 site lists against HEAD
provides:
  - A package-scope compile-time interface assertion in internal/store/alias.go, proven load-bearing by mutation
  - A constructor test that asserts the constructor actually wires its argument, replacing a lone require.NotNil
  - A site-by-site re-derivation of the archived weak-test record against the current tree
  - A grounded judgment on the misleading-but-passing test handed over by plan 09-08
affects: [09-11 skip-file disposition, 09-18 ACE naming ratchet, QUAL-03 verification]

tech-stack:
  added: []
  patterns:
    - "Interface-satisfaction guarantees live in the production file as `var _ Iface = (*Type)(nil)`, where the compiler enforces them, not in a test that cannot fail once compiled"
    - "Every guard proven falsifiable by mutation, with the failure attributed to the guard itself rather than to collateral damage"

key-files:
  created: []
  modified:
    - internal/store/alias.go
    - internal/store/alias_test.go

key-decisions:
  - "The plan's premise held: 6 of the 10 sites cited by holomush-ec22.16 no longer exist as cited, and the surviving in-scope remediation set is exactly 2 functions — under the plan's cap of 4."
  - "The 8 pre-existing TestPostgresAliasRepository_* functions were NOT renamed: all 8 carry subtests and so fall under the documented TestType_Method exception in .claude/rules/testing.md. My first check of this used a 40-line window and wrongly reported 6 of 8 as subtest-free; re-checked by locating every t.Run in the file."
  - "The first constructor mutation produced a nil-pointer panic in a different test that aborted the binary before my test ran — non-evidence. Re-run isolated, where the assertion's own message fired."
  - "TestEnsureCerts_DirectoryCreationFailure (handed over by 09-08) is a confirmed real defect but sits outside this plan's derived remediation set; filed as #4860 rather than silently fixed or silently dropped."

requirements-completed: []

coverage:
  - id: D1
    description: "The interface-satisfaction guarantee is enforced by the compiler rather than by a test that cannot fail"
    requirement: "QUAL-03"
    verification:
      - kind: build
        ref: "Mutation control: renaming DeletePlayerAlias fails `task build` at internal/store/alias.go:52 naming the missing method"
        status: pass
    human_judgment: false
  - id: D2
    description: "The constructor test asserts that the constructor wires the supplied pool onto the returned repository"
    requirement: "QUAL-03"
    verification:
      - kind: unit
        ref: "internal/store/alias_test.go#TestNewPostgresAliasRepositoryStoresTheSuppliedPoolOnTheReturnedRepository"
        status: pass
      - kind: unit
        ref: "Isolated mutation control: constructor dropping `pool: pool` fails the test with 'constructor left the pool field unset'"
        status: pass
    human_judgment: false
  - id: D3
    description: "Every site named by the archived weak-test record is re-derived against the current tree with its disposition recorded"
    requirement: "QUAL-03"
    verification:
      - kind: analysis
        ref: "Re-derivation table below — 10 cited sites, each with a filesystem or repo-wide symbol-search result"
        status: pass
    human_judgment: false

status: complete
---

# Phase 09 Plan 09: QUAL-03 Weak-Test Remediation Summary

Moved the alias repository's interface guarantee from a test that could never fail into a
compile-time declaration that provably breaks the build, gave a `require.NotNil`-only constructor
test something real to assert, and re-derived the three-month-old weak-test site list — 6 of its 10
citations no longer exist as written.

## Task 1 — Re-derivation of the archived weak-test record

Recovered verbatim from `.planning/archive/beads/2026-07-09-beads-live.jsonl` (record
`holomush-ec22.16`, line 413, extracted as JSON rather than read as raw text). The record cites
**10 sites**. Every one was checked individually against the current tree.

| # | Cited site | Function | Disposition at HEAD | Evidence |
|---|---|---|---|---|
| 1 | `internal/session/memstore_test.go:36` | `TestMemStore_Get_NotFound` | **FILE GONE** | `ls` → `No such file or directory` |
| 2 | `internal/session/memstore_test.go:233` | `TestMemStore_ConcurrentAccess` | **FILE GONE** | same |
| 3 | `internal/session/memstore_test.go:397` | `TestFocusMutatorHasMutateField` | **FILE GONE** | same |
| 4 | `internal/access/resolver_test.go:15` | `TestLocationResolverSatisfiesInterface` | **FUNCTION GONE** (file remains, 1416 B) | repo-wide search for `SatisfiesInterface` → 0 matches; `func TestLocationResolver` → 0 matches |
| 5 | `internal/store/alias_test.go:579` | `TestAliasRepositoryInterface` | **SURVIVES at cited line** → remediated | read in full |
| 6 | `internal/store/alias_test.go:588` | `TestNewPostgresAliasRepository` | **SURVIVES at cited line** → remediated | read in full |
| 7 | `test/integration/eventbus_e2e/audit_drift_detector_test.go:36` | Ginkgo `Skip` | **SURVIVES at cited line** — out of scope | skip present at `:36` |
| 8 | `test/integration/eventbus_e2e/js_storage_corruption_test.go:38` | Ginkgo `Skip` | **SURVIVES at cited line** — out of scope | skip present at `:38` |
| 9 | `test/integration/eventbus_e2e/multi_protocol_fanout_test.go:36` | Ginkgo `Skip` | **SURVIVES at cited line** — out of scope | skip present at `:36` |
| 10 | `test/integration/eventbus_e2e/backfill_rebuild_test.go:28` | Ginkgo `Skip` | **SURVIVES at cited line** — out of scope | skip present at `:28` |

**Survivor tally: 6 of 10 cited sites still exist. 4 are dead citations.**

### The two load-bearing claims, independently confirmed

The plan required both be verified directly rather than taken on trust from `09-RESEARCH.md`,
because both decide whether this plan is small.

**(a) `internal/session/memstore_test.go` is gone.** `ls` returns `No such file or directory`
(exit 1). A repo-wide symbol search for all three cited function names returns **0 matches**
(rg exit 1). A filesystem sweep for any `memstore*` file finds only three documents — including
`docs/adr/holomush-bozv-drop-session-memstore-test-against-postgres.md`, the ADR that records the
removal. Confirmed, with a documented cause.

**(b) The resolver interface-canary is gone.** `internal/access/resolver_test.go` **does still
exist** (1416 bytes) — only the function is gone. Repo-wide searches for `SatisfiesInterface` and
`func TestLocationResolver` both return **0 matches**. Confirmed.

Note the distinction the table preserves: site 4 is *function gone, file present*, which is a
different disposition from sites 1–3 (*file gone*). A future phase applying this list blind would
open a file that exists and fail to find the function.

### The arch review's cleared list was not re-flagged

The zero-assertion sweep in `docs/reviews/arch-review/2026-07-11/findings/d9a-testing-ci.md`
cleared candidates concentrated in `internal/plugin/hostcap`, `internal/eventbus`,
`internal/access/policy/attribute`, `gorules/analyzers`, and `plugins/core-communication`. My
remediation set is confined to `internal/store` — **no intersection**. The cleared list was not
re-derived or second-guessed.

### Remediation set

**2 functions**, both in `internal/store/alias_test.go` — within the plan's cap of 4. No stop
condition triggered.

## Task 2 — What changed

**`internal/store/alias.go`** gains a package-scope declaration, matching the convention already
used three times in this package (`role_resolver.go:16`, `character_settings_repo.go:107`,
`player_session_store.go:30`):

```go
var _ AliasRepository = (*PostgresAliasRepository)(nil)
```

with a comment naming the finding and stating that it replaces a runtime canary, so the deletion is
legible in a future `git blame`.

**`internal/store/alias_test.go`**:

- `TestAliasRepositoryInterface` **deleted**. It held `var _ AliasRepository = NewPostgresAliasRepository(mock)`
  inside a function body — a compile-time check wearing a test's clothes. It could not fail at
  runtime. A comment at the deletion site points to the new home.
- `TestNewPostgresAliasRepository` → `TestNewPostgresAliasRepositoryStoresTheSuppliedPoolOnTheReturnedRepository`.
  Its sole `require.NotNil(t, repo)` is replaced by assertions that the constructor genuinely wired
  its argument: the repository is non-nil, its pool field is set, and it is the *same* pool that was
  passed in.

### One correction to the bead's own description

`holomush-ec22.16` groups `TestAliasRepositoryInterface` with the resolver canary as *"takes
`_ *testing.T`, zero assertions"*. That is accurate for the resolver but **not** for the alias
version, which took a real `t` and did carry a `require.NoError` on mock-pool creation. It was never
literally zero-assertion — its defect was that the assertion it carried was about the fixture, not
about the guarantee in its name. Recorded because it is the same lesson the phase keeps relearning:
the predicate a finding is written against is not always the defect that is actually present.

## Falsifiability — mutation controls

| # | Guard | Mutation | Result |
|---|---|---|---|
| NC1 | `var _ AliasRepository = (*PostgresAliasRepository)(nil)` | rename `DeletePlayerAlias` on the concrete type | `task build` exits **201**, error cites **`internal/store/alias.go:52:25`** — the assertion line itself — `missing method DeletePlayerAlias` |
| NC2 | constructor test | constructor returns `&PostgresAliasRepository{}` | test exits **201**, fails with its own message `constructor left the pool field unset` |
| NC3 | constructor test (positive control) | none | exits **0**, `DONE 1 tests` |

NC1 matters beyond exit status: the failure is attributed to the assertion, not to some unrelated
caller of the renamed method. Restoration was verified by `shasum -a 256 -c` (OK) and a clean
`task build` (exit 0).

**NC2 required a second attempt, and the first attempt was non-evidence.** Mutating the constructor
and running the whole package produced exit 201 — but the log showed the failure was a nil-pointer
*panic* inside `TestPostgresAliasRepository_GetSystemAliases`, which aborted the test binary before
my test ever ran. My test appeared nowhere in the output. Re-run with `-run` isolating the single
test, the assertion's own message fired, and `DONE 1 tests` confirmed the pattern actually matched
something — the vacuous-`-run` trap this phase has hit before.

A first draft used `assert.Same` alone, which under mutation failed with testify's opaque
`Both arguments must be pointers` rather than a diagnosis. A `require.NotNil` on the pool field was
added ahead of it so the nil case reports plainly.

## Verification

| Check | Result |
|---|---|
| `task test -- ./internal/store/` | exit **0** — 180 tests |
| `task test` (repo-wide) | exit **0** — **10393** tests, 4 skipped |
| `task lint` | exit **0** |
| `task fmt` | exit **0** — mutated only the two files already in this change; committed |
| `task build` | exit **0** |
| `rg -c 'func TestAliasRepositoryInterface' internal/store/alias_test.go` | exit 1, no matches — canary gone |
| `rg -c '^var _ ' internal/store/alias.go` | 1 match at `:52` |

The repo-wide count moving 10394 → **10393** is the expected arithmetic: one test function deleted,
one renamed. It is also a small independent check that the deletion actually landed.

## Scope discipline — what each change gives up, and what replaces it

The plan's prohibition is that a guarantee, not a function, is what must survive.

| Removed | Guarantee it carried | What carries it now |
|---|---|---|
| `TestAliasRepositoryInterface` | `*PostgresAliasRepository` satisfies `AliasRepository` | `alias.go:52` — the compiler, proven by NC1. **Strictly stronger**: the old test only ran under `task test`; the declaration is checked by every build, including consumers who never run this package's tests. |
| `TestNewPostgresAliasRepository`'s `require.NotNil` body | constructor returns non-nil | Retained verbatim, plus two assertions it did not make. **No guarantee lost.** |

No coverage regression. Neither change reduces what is guarded.

## Honest scope: what this plan does NOT cover

The briefing asked for this to be stated plainly rather than reported as complete coverage of
QUAL-03.

**This plan's predicate finds only the defect shape the archived record described** — tests whose
assertions do not bear on the guarantee in their name, at ten specific cited locations. It is a
re-derivation of a fixed list, not a fresh mechanical sweep. It therefore says nothing about:

- **Misleading-but-passing tests** — tests with real assertions that exercise the wrong branch.
  Invisible to any zero-assertion predicate, and the shape actually found in this phase (below).
- **The ACE naming half of QUAL-03**, delivered by 09-18's ratchet.
- **The four `eventbus_e2e` skip files** (sites 7–10), which are 09-11's delete-vs-retain call.

The arch review's clean sweep plus this re-derivation agree the *assertion-less* population is
essentially empty. That is a statement about one predicate, not a clean bill of health for the
test suite.

## Judgment on the test handed over by plan 09-08

09-08 left `TestEnsureCerts_DirectoryCreationFailure` (`internal/tls/subsystem_test.go:332`)
explicitly for this plan to judge. **I confirmed its diagnosis from the production source rather
than inheriting it.**

`fileExists` (`internal/tls/subsystem.go:178-181`) is `err == nil || !os.IsNotExist(err)`. The
test's `badDir` is `<regular-file>/nested/certs`, so `os.Stat` returns `ENOTDIR`;
`os.IsNotExist(ENOTDIR)` is false, so `fileExists` returns **true**. `EnsureCerts` therefore takes
the load-existing branch and returns `TLS_LOAD_FAILED`, never reaching `xdg.EnsureDir` — the
`CERTS_DIR_CREATE_FAILED` branch its name refers to. It passes only because it matches the
substring `"directory"`, which `ENOTDIR`'s text satisfies. **Diagnosis confirmed.**

I also found a defect 09-08 did not report: the assertion is
`assert.True(t, assert.Condition(t, func() bool { return assert.Contains(...) || assert.Contains(...) }))`.
The inner `assert.Contains` calls register failures on `t` directly, so the left operand failing
marks the test failed even when the `||` would succeed.

**Verdict: a real defect, of a class this plan's predicate cannot see — and outside its derived
remediation set.** Task 2 forbids hunting beyond the Task 1 sites, and the real
`CERTS_DIR_CREATE_FAILED` branch is now genuinely covered by 09-08's
`TestEnsureCertsReportsWhichGenerationStageFailed`, so this is a naming and rigor defect, not a
coverage hole. Filed as **#4860** with the full derivation rather than silently fixed or silently
dropped.

## Discovered, not fixed (out of scope)

**The 8 `TestPostgresAliasRepository_*` functions were deliberately not renamed.** Their final
underscore-delimited segment is a single token, which looks like a naming-ratchet hit — but all 8
carry subtests, placing them under the documented `TestType_Method` exception in
`.claude/rules/testing.md`. They are compliant. Flagged here so 09-18's ratchet does not re-derive
them as violations.

*This nearly went the other way.* My first check used `rg -A40` per function and reported 6 of the 8
as having **zero** subtests — which would have made them genuine violations. The window was simply
too small to reach the table-driven `t.Run(tt.name, ...)` at the bottom of each function. Re-checked
by locating every `t.Run` in the file by line number: 10 of them, covering all 8 functions. A
too-small search window is the same failure mode as a too-loose predicate, and it produced a
confident wrong answer.

## Deviations from Plan

**1. [Rule 2 — Missing verification rigor] The first constructor mutation was accepted-looking but was non-evidence.**
- **Found during:** Task 2 falsifiability check.
- **Issue:** Whole-package run under mutation exited non-zero, which superficially satisfies "the test fails" — but the failure was a nil-pointer panic in a different test that aborted the binary before mine ran.
- **Fix:** Re-ran isolated with `-run`, confirmed the assertion's own message fired, and asserted `DONE 1 tests` so the `-run` could not pass vacuously.
- **Commit:** verification only; no production change.

**2. [Rule 1 — Diagnostic quality] `assert.Same` alone reported `Both arguments must be pointers` on the nil case.**
- **Fix:** Added `require.NotNil(t, repo.pool, "constructor left the pool field unset")` ahead of it. Three assertions total.
- **Files modified:** `internal/store/alias_test.go`
- **Commit:** `d3f220a91`

**3. [Rule 3 — Convention] The first commit omitted the repo's mandated AI-authorship byline.**
- **Fix:** Amended the unpushed tip commit to add `Co-Authored-By:`, matching all recent phase-09 commits. Confirmed unpushed (`git branch -r --contains HEAD` empty) before amending.
- **Commit:** `cdb257059` → `d3f220a91`

## Self-Check: PASSED

- `internal/store/alias.go` — FOUND (modified, `var _` at `:52`)
- `internal/store/alias_test.go` — FOUND (modified, canary absent, renamed test present)
- `.planning/phases/09-test-quality-code-health-sweep/09-09-SUMMARY.md` — FOUND
- Commit `d3f220a91` — FOUND
- Issue #4860 — FILED
