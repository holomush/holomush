---
phase: 09-test-quality-code-health-sweep
plan: 11
subsystem: test-quality
tags: [integration-tests, eventbus, dead-code, skip-hygiene, issue-tracking]
status: complete
requires:
  - "issue numbers for the four eventbus_e2e skip files (from plan 09-02)"
provides:
  - "four trimmed eventbus_e2e specs citing open GitHub issues"
  - "delete-vs-retain verdict for #4855/#4856 (retain, with the condition under which deletion becomes correct)"
affects: []
tech-stack:
  added: []
  patterns:
    - "Ginkgo JSON report (-ginkgo.json-report) as a spec-count instrument instead of parsing gotestsum text"
key-files:
  created: []
  modified:
    - test/integration/eventbus_e2e/audit_drift_detector_test.go
    - test/integration/eventbus_e2e/backfill_rebuild_test.go
    - test/integration/eventbus_e2e/js_storage_corruption_test.go
    - test/integration/eventbus_e2e/multi_protocol_fanout_test.go
decisions:
  - "Retained all four specs rather than deleting the twice-declined #4855/#4856 files — the trim itself dissolves the maintenance-burden argument that made deletion attractive, and both issue bodies assign that call to a maintainer closing the issue"
  - "Scoped the plan's negative assertion to the four owned files — as written against the whole directory it could never pass, because four out-of-scope files carry historical holomush-xxxx provenance references"
  - "Anchored the gate's skip needle to \\bSkip\\( after a negative control proved the unanchored form matched the substring inside NotSkip("
metrics:
  duration: ~40min
  tasks: 1
  files: 4
  completed: 2026-07-26
---

# Phase 09 Plan 11: Trim Unimplemented Event-Bus Specs Summary

Reduced four unimplemented `eventbus_e2e` integration specs from 282 lines to 127 — deleting
~200 lines of compiled-but-unreachable setup below their `Skip(...)` calls and repointing four
dead beads tokens at live GitHub issues.

## What was done

Each file carried 40–50 lines of setup positioned *after* an unconditional Ginkgo `Skip()`. That
code never executed but still compiled, so every refactor of the harness or the event types had
to keep it building. Each skip also cited a retired beads token that resolves to nothing.

| File (`test/integration/eventbus_e2e/`) | Before | After | Old token | New citation |
|---|---:|---:|---|---|
| `audit_drift_detector_test.go` | 70 | 31 | `holomush-ecbg` | **#4853** |
| `backfill_rebuild_test.go` | 60 | 28 | `holomush-l4kx` | **#4854** |
| `js_storage_corruption_test.go` | 75 | 34 | `holomush-6nds` | **#4855** |
| `multi_protocol_fanout_test.go` | 77 | 34 | `holomush-nko7` | **#4856** |
| **Total** | **282** | **127** | | |

Each file keeps its SPDX header, build tag, package clause, the comment block recording what an
implementation would have to prove, and the `Describe`/`It`/`Skip` triple. All four container and
spec descriptions are **unchanged in wording** — verified byte-identically, not by eye (below).
Deletion orphaned five imports (`gomega`, `context`, `time`, `internal/eventbus`,
`internal/eventbus/audit`); only the ginkgo dot-import remains. Every shared helper the deleted
setup used (`freshBus`, `freshPool`, `fixedJS`, `fixedPool`, `mintEvent`, `itoa`,
`freshSessionID`, `suiteT`) was confirmed still referenced by other specs, so nothing became
dead at the suite level.

All four issues were re-confirmed `OPEN` by live query immediately before the numbers were
written, and again before commit.

## The delete-vs-retain decision: retain all four

Plan 09-02 escalated this deliberately. #4855 and #4856 cover work declined **twice** — their
predecessors #2387/#2386 were closed `NOT_PLANNED` on 2026-05-17, and the beads migration then
classified them "Archive only — not migrated" rather than consolidating them. Both issue bodies
state that deleting the test file outright is a legitimate resolution.

**Verdict: retain, and here is the reasoning rather than a default.**

1. **The trim dissolves the case for deletion.** The argument for deleting is that a skipped spec
   is dead weight maintained through refactors. That was true at 75–77 lines of compiling setup
   touching `audit.NewSubsystem`, `bus.JS.Stream`, `OpenSession`, and `pool.Exec`. It is not true
   at ~30 lines whose only import is ginkgo. Post-trim the retention cost is approximately zero,
   and the strongest deletion argument was retired by this plan's own work.
2. **Both issue bodies assign the call elsewhere.** #4856: *"that call belongs to a maintainer,
   not to the mechanical sweep that filed this"*, and *"The sweep deliberately does not decide the
   delete-vs-re-site question."* #4855 says the same. Deleting here would be this sweep taking a
   decision its own tracking record reserves for a maintainer.
3. **A skipped spec does not read as coverage.** Ginkgo reports these as `4 Skipped` on every
   integration run — an explicit, visible, grep-able in-repo record of an uncovered behaviour.
   The behaviours are real and user-visible: telnet and web seeing the same pose (#4856), and
   ULID stability across a JetStream rebuild (#4855).
4. **Deleting would strand the issues.** #4855/#4856 are OPEN and describe a file by path.
   Removing the file leaves two open issues pointing at nothing — reintroducing, in the tracker,
   exactly the dangling-reference problem this plan set out to fix in the code.

**The condition under which deletion becomes correct:** if a maintainer declines this work a
third time, the file should be deleted *in the same change that closes the issue*, so the code
and the tracker move together. That condition is now recorded in the two files themselves, so a
future reader meets it where the decision would be made.

## Deviations from Plan

### 1. [Rule 3 — Gate could never pass] Negative assertion scoped to the four owned files

**Found during:** Task 1, before editing.

**Issue:** the plan's gate and acceptance criterion both assert
`! rg -q 'holomush-[a-z0-9]{4}\b' test/integration/eventbus_e2e/` over the **whole directory**.
Four out-of-scope files in that directory carry `holomush-xxxx` tokens:

| File | Line | Token |
|---|---|---|
| `audit_only_channel_test.go` | 21 | `holomush-jxo8.6.26` |
| `cursor_concurrent_test.go` | 23 | `holomush-suos` |
| `cross_tier_query_test.go` | 633 | `holomush-gfo6.30` |
| `cursor_concurrent_suite_test.go` | 21 | `holomush-cz4s` |

So the gate would have failed after a *perfect* trim. These four are historical **provenance**
notes (which bead filed the test, why it was renamed), not live citations a reader is meant to
follow, and rewriting them is outside this plan's `files_modified`.

**Fix:** the assertion is scoped to the four files this plan owns, iterated per-file. This is
strictly more falsifiable than the original, not less — a stale token surviving in any one of the
four fails it, which the directory-wide form could not distinguish from unrelated noise.

### 2. [Rule 1 — My own gate had the phase-9 defect] Skip needle was unanchored

**Found during:** verification, by negative control D.

The gate's positive control used `rg -q 'Skip\('`. Control D renamed `Skip(` to `NotSkip(` and
**the gate still passed** — `Skip(` is a substring of `NotSkip(`. A needle matching more than
intended is the exact defect class this phase exists to eliminate, and it appeared in my own
verification. Anchored to `\bSkip\(`; all controls re-run from scratch afterwards.

## Verification

Every result below is by **exit code**, never by matching output text.

**Instrument choice.** `task test:int` runs `gotestsum --format pkgname`, which collapses the
suite to one line and prints no spec counts — text-parsing it could not have detected a spec
silently vanishing. Ginkgo's `-ginkgo.json-report` passes through `CLI_ARGS` and yields structured
per-spec state, so the count claim rests on a machine-readable instrument.

| Check | Result |
|---|---|
| `task test:int -- ./test/integration/eventbus_e2e/...` (baseline, pre-change) | 0 — **24 passed, 4 skipped** |
| same, post-trim | 0 — **24 passed, 4 skipped** |
| `diff` of baseline vs post-trim skipped-spec descriptions | empty — **byte-identical** |
| `task lint` | 0 (run twice: after the trim and after the prose tightening) |
| `task fmt` | 0, no mutations beyond the four files |
| `task quarantine:audit` | 0 |
| all four files < 35 lines | 31 / 28 / 34 / 34 |
| #4853–#4856 `gh issue view --json state` | all **OPEN** (re-checked pre-commit) |

The `diff` of skipped-spec descriptions is what substantiates "descriptions unchanged in
wording" — it compares the strings Ginkgo actually registered, so a reworded `Describe` or `It`
would surface as a diff even though the suite would still pass.

**Negative controls on the trim gate.** The gate was mutated eight ways to confirm it can fail:

| Scenario | Exit |
|---|---|
| A. untouched copy (positive control) | **0** |
| B. one stale beads token restored | 1 |
| C. one spec file deleted | 1 |
| D. `Skip(` renamed to `NotSkip(` | 1 *(0 before the anchoring fix — see Deviation 2)* |
| D2. the `Skip` line deleted outright | 1 |
| E. issue reference stripped from a file | 1 |
| F. emptied directory | 1 |
| G. unreadable / nonexistent directory | 1 |

F and G matter specifically: the two positive controls (a skip must be present, an issue number
must be present) mean the negative assertion cannot pass vacuously against a directory that is
empty or cannot be read.

## Threat Flags

None. No network endpoint, auth path, file-access pattern, or schema changed — the diff deletes
test-only code and rewrites comments.

## Notes for later plans

- **`task test:int` accepts `--` package args**, contradicting
  `.claude/rules/references/plan-review-learnings.md`. Scoped runs work and cut the loop to ~62s
  versus a full integration pass. It also forwards extra flags, which is how the Ginkgo JSON
  report was obtained.
- **`-ginkgo.json-report=<abs-path>` is the way to assert spec counts** under this repo's
  `--format pkgname` runner. Recommended for any later plan claiming a skipped/passing count.
- The four out-of-scope `holomush-xxxx` provenance references in `eventbus_e2e/` (Deviation 1)
  were left untouched. They are not dangling citations in the same sense — they explain why a
  test exists rather than pointing at work to do — but if a later plan wants directory-wide
  token cleanliness, those four are the remainder.

## Self-Check: PASSED

- All four modified files exist and are 31 / 28 / 34 / 34 lines.
- Commit `64f80e3b3` found in `git log`; `git diff --diff-filter=D HEAD~1 HEAD` empty — no file
  deletions, confirming all four specs survive.
- #4853, #4854, #4855, #4856 each appear exactly once, in exactly one file, all distinct, all
  matching plan 09-02's mapping, all `OPEN`.
- No `// Verifies:` annotation exists in any of the four files, so no invariant registry entry
  was falsely bound or orphaned by the trim.
- No quarantine marker touched: these are plain Ginkgo `Skip`s for unimplemented features, not
  `quarantinetest.Skip` flake quarantines, so `test/quarantine.yaml` is unchanged and
  `task quarantine:audit` stays clean.

## Known Stubs

None introduced. The four skipped specs are pre-existing, deliberately-retained coverage gaps,
each now citing an open tracking issue — documented above rather than hidden.
