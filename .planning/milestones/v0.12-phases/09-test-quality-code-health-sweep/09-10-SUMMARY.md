---
phase: 09-test-quality-code-health-sweep
plan: 10
subsystem: testing
tags: [coverage, codecov, cmd-holomush, eventbus, oops, config, mutation-testing, residual]

# Dependency graph
requires:
  - phase: 09-01
    provides: "the repaired E2E coverage chain, the un-ignored wiring files, and the codecov-vs-go-tool-cover instrument distinction"
  - phase: 09-04
    provides: "the config.Load-level pinning pattern this plan reuses for the six core config sections"
provides:
  - "Eight behavioural test functions over cmd/holomush/core.go, each proven falsifiable by mutation"
  - "Full coverage of rekeyAuditPublisherAdapter.PublishAudit, previously 0%"
  - "A three-lane measurement of cmd/holomush with the instrument named for every figure"
  - "A recorded, per-file coverage residual with a tracking issue (#4861)"
affects: [09-18 test naming ratchet, 09-19 coverage floor gate, QUAL-02 verification]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Fall-through negative control: clear DATABASE_URL so a dropped config-section Load reaches the database check and FAILS the test instead of passing silently"
    - "Cross-wiring assertion: assert an override landed on its own field AND not on a sibling's, so a mis-routed assignment cannot satisfy a single-field check"

key-files:
  created: []
  modified:
    - cmd/holomush/core_test.go

key-decisions:
  - "The plan's 80% floor is NOT met and was not faked. Maximum reachable from the plan's two authorized files was +64 statements against a +244 requirement — an arithmetic certainty established in Task 1 before a line of test code was written."
  - "deps_test.go was deliberately left untouched: deps.go is already 100% covered (20/20) under the union, so any test added there would have been coverage theatre, the exact defect QUAL-03 exists to remove."
  - "codecov's ?path= filter is a PREFIX match and silently includes cmd/holomush-cutover/main.go; the package-only figure requires an explicit startswith(\"cmd/holomush/\") select or the total is wrong."
  - "No duplicate issue filed: #4647 and #4804 were read first. #4647's premise was falsified by 09-01 and was corrected in-place with a comment; the residual got its own issue (#4861) because neither existing issue is its home."

patterns-established:
  - "Name the instrument on every coverage figure; never compare a number from one instrument to a threshold defined for the other"
  - "Confirm a -run gate actually ran the intended tests by matching the reported test COUNT, not just the exit code"

requirements-completed: []

coverage:
  - id: D1
    description: "The core command's rekey audit adapter honours its injected clock, returns the published event's ID, and reports each failure mode without inventing an event ID"
    requirement: QUAL-02
    verification:
      - kind: unit
        ref: "cmd/holomush/core_test.go#TestRekeyAuditPublisherStampsTheInjectedClockAndReturnsThePublishedEventID (mutation NC1 confirms falsifiable)"
        status: pass
      - kind: unit
        ref: "cmd/holomush/core_test.go#TestRekeyAuditPublisherRejectsAMalformedSubjectOrTypeWithoutPublishing (NC2)"
        status: pass
      - kind: unit
        ref: "cmd/holomush/core_test.go#TestRekeyAuditPublisherReportsAPublishFailureAndMintsNoEventID (NC3)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Each --log-* sink override lands on its own field, and an unset flag never clobbers a configured value"
    requirement: QUAL-02
    verification:
      - kind: unit
        ref: "cmd/holomush/core_test.go#TestApplyLogSinkFlagsRoutesEachSinkOverrideToItsOwnField (NC4)"
        status: pass
      - kind: unit
        ref: "cmd/holomush/core_test.go#TestApplyLogSinkFlagsLeavesAConfiguredLoggingSectionUntouchedWhenNoSinkFlagIsSet (NC5)"
        status: pass
    human_judgment: false
  - id: D3
    description: "A malformed core/game/auth/event_bus/crypto/logging section stops the run at that section instead of starting the server"
    requirement: QUAL-02
    verification:
      - kind: unit
        ref: "cmd/holomush/core_test.go#TestCoreCommandSurfacesAMalformedConfigSectionInsteadOfStartingTheServer, 6 subtests (NC6)"
        status: pass
      - kind: unit
        ref: "cmd/holomush/core_test.go#TestCoreCommandRejectsAnUnrecognisedEventBusModeBeforeStartingTheServer (NC7)"
        status: pass
    human_judgment: false
  - id: D4
    description: "cmd/holomush clears its named 80% coverage floor on codecov's merged figure"
    requirement: QUAL-02
    verification:
      - kind: other
        ref: "NOT MET — measured 64.82% (codecov line ratio, main) / 70.6% (unit u E2E statement ratio). Residual recorded per file in #4861."
        status: fail
    human_judgment: true
    rationale: "The floor is unreachable from this plan's two authorized files by arithmetic (+64 available vs +244 required). Recorded as a named residual with a tracking issue, which the plan explicitly permits as a correct outcome; the alternative was fabrication."

# Metrics
duration: ~85min
completed: 2026-07-26
status: complete
---

# Phase 09 Plan 10: cmd/holomush Coverage Floor Summary

**Measured the package honestly across three lanes, proved before writing any test that the 80%
floor was unreachable from the plan's authorized files, backfilled the 22 statements that were
genuinely testable anyway — and recorded the 763-statement residual in #4861 rather than faking
the number.**

## Performance

- **Duration:** ~85 min
- **Tasks:** 2 of 2
- **Files modified:** 1 (`cmd/holomush/core_test.go`, +306 lines, 0 deletions)

## Task 1 — The three lanes, with the instrument named for each

The plan's own caution was the load-bearing one: this repo reports coverage through two
instruments that are not interchangeable. Every figure below says which one produced it.

| # | Figure | Instrument | Value | Command |
|---|---|---|---|---|
| 1 | `cmd/holomush`, **unit lane** | `go tool cover` STATEMENT ratio | **55.2%** (1433/2597) | `task test -- -coverprofile=/tmp/p9-cmd-before.out ./cmd/holomush/` then `go tool cover -func` |
| 2 | `cmd/holomush`, **E2E lane** | `go tool cover` STATEMENT ratio | **37.3%** (969/2597) | aggregation over `coverage-e2e.out` |
| 3 | `cmd/holomush`, **codecov merged** | **codecov LINE ratio**, ignore list applied, `sessions: 2` | **64.82%** (1817/2803 lines, 29 files) | codecov API v2 `report/?path=cmd/holomush` |

Figure 3 is the authoritative one — it is the only one that applies `.codecov.yml`'s ignore list
and merges every uploaded session.

### E2E lane, per file — including the two newly-un-ignored wiring files

The plan's acceptance criterion required both un-ignored files to show a non-zero E2E percentage
by name, or the un-ignore is not yet justified. Both do:

| File | E2E lane alone |
|---|---|
| **`core.go`** | **70.1%** (260/371) |
| **`sub_grpc.go`** | **66.0%** (188/285) |
| `gateway.go` | 66.4% (146/220) |
| `cryptowiring.go` | 77.1% (64/83) |
| `deps.go` | 100% (20/20) |
| package total | 37.3% (969/2597) |

My aggregation reproduces plan 09-01's recorded figures **exactly** (70.1% / 66.0% / 37.3%),
which validates the arithmetic against an independently-produced measurement.

### Two measurement traps hit and corrected during Task 1

1. **codecov's `?path=` is a PREFIX match.** `?path=cmd/holomush` returns **30** files and 64.25%
   — it silently includes `cmd/holomush-cutover/main.go`, a different package. The package-only
   figure requires `select(.name | startswith("cmd/holomush/"))`, giving **29** files and
   **64.82%**. Recorded in #4861 so the next reader does not repeat it.
2. **A too-loose `rg`/awk needle.** My first residual query used `/deps\.go:/`, which also matched
   `cmd_admin_totp_deps.go` and reported 24 uncovered statements in `deps.go`. Anchored to
   `/holomush\/deps\.go:/` the true answer is **0**. Same defect class as 09-07's prose-counting
   needle and 09-09's undersized `rg` window — caught because the number contradicted the per-file
   table computed moments earlier.

### The backfill target — union of the lanes, not unit-only

Block keys are identical across the two profiles (`core.go`: 227 blocks in each, symmetric
difference **0**), so the union is well defined.

| Union figure (statement instrument) | Value |
|---|---|
| package, unit ∪ E2E | **69.8%** (1812/2597) |
| `core.go` | 82.7% (307/371) — **64** statements uncovered |
| `deps.go` | **100%** (20/20) — **0** statements uncovered |

This union is a **lower bound** on codecov's merged figure: it omits the integration lane, which
codecov merges and which contributes heavily here (`cryptowiring.go` reads 9.6% unit / 77.1% E2E
locally but **78.14%** on codecov). It is reported as a targeting aid only. Per the plan's
prohibition, **no hand-merged number is presented as the authoritative figure**, and the text
profiles were not merged into a synthetic profile.

### Classification of `core.go`'s 64 uncovered statements

| Region | Stmts | Classification | Reason |
|---|---|---|---|
| `rekeyAuditPublisherAdapter.PublishAudit` (1245–1262) | **11** | **genuine** | Entire function uncovered. Pure adapter with both dependencies injected (`eventbus.Publisher`, a `Now()` clock); production wires it at `cryptowiring.go:213` |
| `NewCoreCmd` RunE config-section error returns (120–154) | **7** | **genuine** | Six `config.Load` failures + `eventBusConfig.Validate()`. A dropped section Load is invisible today |
| `applyLogSinkFlags` (203–214) | **3** | **genuine** | `--log-stderr-level`, `--log-otel-level`, `--log-sentry`. The existing test covers the other three branches only |
| `pluginAuditClientAdapter.AuditEvent` (1065–1067) | **1** | **genuine** | The `AUDIT_PLUGIN_RPC_FAILED` wrap |
| `runCoreWithDeps` internals (244–955) | **42** | **unreachable without a boot** | A 740-line function annotated `// codecov:ignore — tested by integration and E2E tests`; its error branches need live Postgres, NATS, TLS material and loaded plugins. The E2E lane already covers 260/371 of `core.go` |

**Genuine and economically testable: 22 statements.** Ceiling therefore 1812+22 = **1834/2597 =
70.6%** — computed in Task 1, before any test was written.

### The floor was proven unreachable before Task 2 began

Reaching 80% of statements needs **+244** covered statements. The plan's authorized files could
supply at most **+64** (`core.go`'s full remainder, unreachable branches included; `deps.go` had
nothing). The shortfall is arithmetic, not an estimate. Task 2 therefore proceeded on the plan's
explicit instruction: cover the genuine behaviours, then record an honest residual.

## Task 2 — Eight test functions, all in `core_test.go`

Exact names, recorded so plan 09-18's naming ratchet does not rename them. None contains an
underscore, so no name's final underscore-delimited segment is a single CamelCase token.

1. `TestRekeyAuditPublisherStampsTheInjectedClockAndReturnsThePublishedEventID`
2. `TestRekeyAuditPublisherRejectsAMalformedSubjectOrTypeWithoutPublishing` *(2 subtests)*
3. `TestRekeyAuditPublisherReportsAPublishFailureAndMintsNoEventID`
4. `TestPluginAuditClientAdapterWrapsAFailedRPCAndPassesASuccessThroughUnchanged` *(2 subtests)*
5. `TestApplyLogSinkFlagsRoutesEachSinkOverrideToItsOwnField`
6. `TestApplyLogSinkFlagsLeavesAConfiguredLoggingSectionUntouchedWhenNoSinkFlagIsSet`
7. `TestCoreCommandSurfacesAMalformedConfigSectionInsteadOfStartingTheServer` *(6 subtests)*
8. `TestCoreCommandRejectsAnUnrecognisedEventBusModeBeforeStartingTheServer`

**18 test cases total** — and the `-run` invocation reported exactly `DONE 18 tests`, which is how
the run was confirmed non-vacuous. A `-run` gate exits 0 when nothing matches; the count is what
makes it falsifiable.

Two design choices worth naming, both aimed at the phase's defect class:

- **The config-section tests clear `DATABASE_URL`.** It is the next thing `RunE` reaches. If a
  section's `Load` were removed, the run would fall through to the database check and the
  `NotContains(err, "DATABASE_URL")` assertion would **fail** — rather than the test passing
  because "an error was returned". NC6 confirms this fires.
- **The log-sink test asserts routing, not just value.** An override landing on a sibling sink's
  field satisfies a single-field assertion. NC4 (cross-wiring `--log-otel-level` onto
  `Stderr.Level`) confirms the cross-wiring guards catch it.

Assertions were made on `oops` codes via `errutil.AssertErrorCode` / `AssertErrorContext` and on
causes via `assert.ErrorIs` — never on message substrings. Per 09-08's finding about `oops`
merging context innermost-first, the keys asserted (`section`, `mode`) are set at the only oops
wrap in their chain, so nothing shadows them.

### Falsifiability — mutation results

Every added test was proven to fail against a deliberately broken production branch. Each
mutation's anchor was first verified to match **exactly once** in `core.go`; `core.go` was
restored with `git checkout --` after each run and confirmed clean.

| # | Mutation to `cmd/holomush/core.go` | Test that must fail | Result |
|---|---|---|---|
| NC1 | `ev.Timestamp = a.clock.Now()` deleted | `…StampsTheInjectedClock…` | fails ✓ |
| NC2 | invalid-subject guard neutered | `…RejectsAMalformedSubjectOrType…` | fails ✓ |
| NC3 | publish error swallowed | `…ReportsAPublishFailure…` | fails ✓ |
| NC4 | `--log-otel-level` written to `Stderr.Level` | `…RoutesEachSinkOverrideToItsOwnField` | fails ✓ |
| NC5 | `Changed("log-sentry")` guard dropped | `…LeavesAConfiguredLoggingSectionUntouched…` | fails ✓ |
| NC6 | `game` section `config.Load` removed | `…SurfacesAMalformedConfigSection…` | fails ✓ |
| NC7 | `eventBusConfig.Validate()` call removed | `…RejectsAnUnrecognisedEventBusMode…` | fails ✓ |
| NC8 | `AUDIT_PLUGIN_RPC_FAILED` wrap removed | `…WrapsAFailedRPC…` | fails ✓ |

Each failure was additionally checked to be **attributed to the intended test by name**, not
merely a non-zero exit — 09-09's lesson that a non-zero exit proves something failed, not that
your assertion fired.

### Movement

| Figure | Before | After |
|---|---|---|
| `core.go`, unit ∪ E2E (statement) | 82.7% (307/371) | **88.7%** (329/371) |
| package, unit ∪ E2E (statement) | 69.8% (1812/2597) | **70.6%** (1834/2597) |
| package, unit lane alone (statement) | 55.2% | **56.1%** |
| `cmd/holomush` test count | 548 | **566** |

+22 statements — exactly the 22 classified genuine in Task 1, no more and no less.

### The gate actually held to

The plan deliberately specifies no 80% threshold on Task 2's verify, and explains why: the local
figure is unit-lane only. That is correct, but it leaves the task with no gate that can fail. I
added a falsifiable **strict-increase gate on a single instrument**, matching 09-08's remedy:

```
before=55.2 after=56.1 strictly_greater=YES   # exit 0
```

Negative control: feeding the inverted values exits **1**. This gate fails if the added tests
cover nothing. Per-test falsifiability is established separately by the mutation table above.

## Residual — recorded, not papered over

**763 of 2597 statements uncovered (70.6% covered). Reaching 80% needs +244.** The gap is
concentrated in files no plan in phase 09 owns:

| Uncovered | File | Covered | % |
|---|---|---|---|
| 138 | `cmd_audit.go` | 35/173 | 20.2% |
| 84 | `migrate.go` | 140/224 | 62.5% |
| 76 | `cmd_admin_read_stream.go` | 94/170 | 55.3% |
| 62 | `sub_grpc.go` | 223/285 | 78.2% |
| 60 | `cmd_crypto_rekey.go` | 155/215 | 72.1% |
| 48 | `crypto_rekey_wiring.go` | 44/92 | 47.8% |
| 42 | `core.go` | 329/371 | 88.7% |
| 41 | `cmd_admin_totp.go` | 84/125 | 67.2% |
| 29 | `gateway.go` | 191/220 | 86.8% |
| 25 each | `outbox_admin.go`, `kek_provision.go`, `admin_client.go` | — | 32–52% |
| 24 each | `world_genesis.go`, `cmd_admin_totp_deps.go` | — | 29–44% |
| ≤15 each | `cmd_plugin_events.go`, `cryptowiring.go`, `status.go`, `cmd_admin_approve.go`, `cmd_admin_totp_reset.go`, `readstream_wiring.go`, `phase7_fence_wiring.go`, `cmd_plugin_validate.go`, `cert_poll.go` | — | ≥72% |

`core.go`'s remaining 42 are the `runCoreWithDeps` boot branches classified unreachable in Task 1.

**Tracking issue: [#4861](https://github.com/holomush/holomush/issues/4861)** — carries the full
per-file table, all three lane figures with their instruments named, the `?path=` prefix-match
trap, and a suggested next step (the top four files are cobra subcommands with injectable deps,
so unit-testable without a boot — 358 of the 763 statements).

## Decisions Made

1. **`deps_test.go` was not touched.** `deps.go` is at **100%** (20/20) under the union. The plan
   lists it as a file to modify, but adding a test to a fully-covered file would be exactly the
   assertion-free coverage theatre the plan's own prohibitions forbid. Recorded as a deviation.
2. **The floor was not chased.** Proven unreachable in Task 1 with arithmetic, before any code was
   written, so Task 2 wrote only what asserts real behaviour.
3. **No duplicate issue.** #4647 and #4804 were read in full before filing. Neither is the
   residual's home, so #4861 was filed and both were cross-linked.
4. **#4647's premise was corrected in place.** It claims `cmd/holomush/{core,sub_grpc}.go` sit at
   "0–0.6% patch coverage" because "instrumentation isn't observing it". 09-01 resolved the
   observability half; the files now measure 88.7% / 78.2% under the union. A grounded comment
   with the measurements was added and a re-scope suggested. It was **not** closed —
   `sub_grpc.go` still has 62 uncovered statements, so the issue is not fully resolved.

## Deviations from Plan

**1. [Rule 3 — Blocking] Task 2's verify had no gate that could fail.**

- **Found during:** Task 2 setup.
- **Issue:** The plan deliberately omits an 80% threshold (correctly — the local figure is
  unit-lane only), but the resulting verify is `task test … && go tool cover -func … && task test`,
  which passes whether or not the added tests cover anything.
- **Fix:** Ran the plan's verify as written for the record (exit 0) and added a falsifiable
  strict-increase gate on a single instrument (55.2 → 56.1, negative control exits 1), plus the
  eight per-test mutation controls as the real acceptance evidence.
- **Files modified:** none (verification method only).

**2. [Rule 3 — Blocking] `deps_test.go`, one of the plan's two authorized files, had nothing
honest to add.**

- **Found during:** Task 1 classification.
- **Issue:** `deps.go` measures 100% (20/20) under the union — zero uncovered statements. Writing
  tests there to satisfy the plan's file list would have produced assertion-free or tautological
  tests, which the plan's prohibitions and QUAL-03 both forbid.
- **Fix:** Left `deps_test.go` untouched and recorded why. All eight added tests target `core.go`
  behaviours via `core_test.go`, the plan's other authorized file.
- **Files modified:** none.

**3. [Rule 1 — Bug] My own residual query used a too-loose pattern.**

- **Found during:** Task 1 residual computation.
- **Issue:** `/deps\.go:/` also matches `cmd_admin_totp_deps.go`, reporting 24 uncovered
  statements in `deps.go` where the true count is 0 — contradicting the per-file table computed
  moments earlier.
- **Fix:** Anchored to `/holomush\/deps\.go:/` and re-ran. Caught before it influenced any
  decision.
- **Files modified:** none.

---

**Total deviations:** 3 auto-fixed (2 × Rule 3, 1 × Rule 1). None expands scope; two of the three
prevent this plan from repeating the phase's own named defect class.

## Verification

| Check | Result |
|---|---|
| Task 1 verify gate (as written in the plan) | **exit 0**; negative control against a nonexistent profile **exit 1** |
| `task test -- -run <8 new tests> ./cmd/holomush/` | exit 0 — **18 tests**, matching the expected count exactly |
| Mutation controls NC1–NC8 | **8/8** non-zero AND attributed to the intended test by name |
| Strict-increase gate | exit 0 (55.2 → 56.1); inverted inputs exit 1 |
| `task lint` | **exit 0** |
| `task test` (repo-wide) | **exit 0** — 10,411 tests, 4 skipped (was 10,393; +18) |
| `rg -c 'cmd/holomush' .codecov.yml` | **0** — no ignore entry restored |
| `t.Skip(` count in `cmd/holomush/` | **0** before, **0** after — no skip added |
| `git diff --diff-filter=D` on the task commit | empty — no test file deleted |
| `git diff --stat` | 1 file, **+306 / −0** |
| `core.go` restored after every mutation | `git status --short cmd/holomush/core.go` clean each time |

Every result above is read from an **exit code** or a numeric artifact measurement. The one place
a count is used (`DONE 18 tests`) is precisely because the exit code alone cannot distinguish "all
passed" from "nothing matched the `-run` pattern".

## Task Commits

1. **Task 1: Re-measure the package across all three lanes** — no repo files; analysis recorded
   above, as the plan specifies.
2. **Task 2: Backfill the classified untested behaviours** — `640df6204` (test)

## Known Stubs

None. No stub, placeholder, skipped test, or unrun `<verify>` was introduced. Every verification
listed above was executed and its result recorded.

The **coverage residual is not a stub** — it is a measured shortfall in production code this plan
was not authorized to test, recorded per file with a tracking issue (#4861). It is logged to the
broken-windows ledger below as an unmet truth.

## Threat Flags

None. No new network endpoint, auth path, file-access pattern, or schema change at a trust
boundary; the change is test-only.

On the register:

- **T-09-10-01** (repudiation — a floor reached by assertion-free tests or a restored ignore
  entry): mitigated. Every added test asserts behaviour and was proven falsifiable by mutation;
  `rg -c 'cmd/holomush' .codecov.yml` returns 0; the unreached floor is recorded as a named
  residual with tracking issue #4861 rather than fabricated.
- **T-09-10-02** (tampering — test deletion or skipping): mitigated. Zero deletions in the commit;
  skip count 0 → 0.
- **T-09-10-03** (repudiation — measurement method): mitigated. The three lanes are reported
  separately with their instruments named; no hand-merged number is presented as authoritative;
  the local union is explicitly labelled a lower bound and a targeting aid.
- **T-09-10-SC** (supply chain): did not arise. No package installed, no dependency manifest
  touched.

## Next Phase Readiness

- **09-18** (naming ratchet): the eight added names are listed above; none contains an underscore,
  and no added subtest string is a vague placeholder. The six pre-existing vague subtest strings
  in sibling files were deliberately **not** touched, per the plan's instruction to avoid
  colliding with that sweep.
- **09-19** (coverage floor gate): this package will **not** clear 80% on the merged figure. The
  gate should expect that and read #4861. The gate is also the only place the merged number can be
  checked — it is produced by the CI upload pipeline, not reproducible locally, and 09-01
  established that a green CI job does not prove the upload landed.
- **QUAL-02** remains **Pending**, consistent with 09-01's bookkeeping note: six plans in this
  phase carry it, and this plan delivers a backfill plus an honest residual, not the requirement's
  full text. `frontmatter.requirements-completed` is left empty here because the floor this plan
  names was not met.

---
*Phase: 09-test-quality-code-health-sweep*
*Completed: 2026-07-26*

## Self-Check: PASSED

- `cmd/holomush/core_test.go` — FOUND (modified, committed `640df6204`)
- `.planning/phases/09-test-quality-code-health-sweep/09-10-SUMMARY.md` — FOUND
- Commit `640df6204` — FOUND in `git log --oneline --all`
- Issue #4861 — FOUND (created)
- Comment on #4647 — FOUND (`issuecomment-5085719182`)
- `cmd/holomush/deps_test.go` — intentionally unmodified; confirmed absent from `git diff --stat`
