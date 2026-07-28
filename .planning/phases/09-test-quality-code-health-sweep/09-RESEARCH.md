# Phase 9: Test-Quality & Code-Health Sweep - Research

**Researched:** 2026-07-25
**Domain:** Go test quality, coverage instrumentation & gating, ABAC/web security polish, DB migration
**Confidence:** HIGH (both sizing gates measured directly; every repo claim carries a `path:line` or a tool-run)

## Summary

Both load-bearing sizing gates fired, and **both falsify a premise in CONTEXT.md** — in
opposite directions. **GATE 1 (QUAL-02):** the "~54.6% baseline" is not codecov's project
coverage; it is the raw `go tool cover -func` unit-only total, which does not apply
`.codecov.yml`'s `ignore:` list and does not merge the integration/e2e upload session.
Codecov's authoritative project coverage on `main` today is **78.28%** — already **8.28
points above D-02's 70% target**. The coverage lift D-02 describes does not need to happen.
**GATE 2 (QUAL-03):** the D-07 predicate, implemented exactly as written and honouring the
`TestType_Method`-with-subtests exception, returns **1,106 hits** — **7.4× D-09's ~150
re-scope threshold**. Applying the exception removes only 466 of 1,572 underscore-form
functions, not "most" of them. Reading a random sample of 40 shows the overwhelming
majority are already ACE-compliant sentences that merely use `_` as a separator
(`TestSetupWithBridge_AllDisabledDiscards`, `TestCascadeDelete_Object_RollsBackPropertiesOnParentDeleteFail`),
and 25 are `TestINV_<SCOPE>_<N>_*` invariant-binding names whose form is load-bearing.

The net effect is that **the phase gets smaller and sharper, not larger**. QUAL-02 collapses
from "lift 15 points" to "close two named floor gaps (`cmd/holomush` −15.2pts,
`internal/tls` −3.8pts) and wire the gate that Phase 6 left unwired" — the gate wiring being
the actual durable win, since `codecov/patch` and `codecov/project` are confirmed **absent**
from ruleset `11923801`. QUAL-03's ACE half must be re-scoped under D-09's own release
valve. QUAL-04 is spec authoring on a fully-built harness (zero TODO panics remain; all
eight cited helpers verified present), and the matrix is **38 populated cells, not 48** — ten
are `n/a` in izk0's own table. QUAL-05's four in-scope items are all live with accurate
citations.

**Primary recommendation:** Re-point QUAL-02 at the `holomush-0yo6` floor set + D-04 gate
wiring (the 70% target is already met — do not manufacture work to "reach" it); take D-09's
re-scope conversation on QUAL-03's ACE half with the tightened 114-hit predicate below; plan
QUAL-04 and QUAL-05 as specified.

## User Constraints (from CONTEXT.md)

### Locked Decisions

Reproduced verbatim from `09-CONTEXT.md` `<decisions>`. Where research falsifies a premise,
the decision is still shown as locked and the conflict is flagged inline — **the planner,
not this research, decides how to resolve it.**

**QUAL-02 — Coverage backfill**
- **D-01:** Risk-ranked whole-repo audit. Rank packages by **(uncovered statements × blast
  radius)**, weighting `internal/eventbus`, `internal/access`, `internal/crypto`,
  `internal/session`, `internal/world` upward. The `holomush-0yo6` named set
  (`cmd/holomush`, `internal/tls`, `internal/xdg` @≥80%; `internal/core` @≥90%) is a **floor,
  not the definition** — MUST be cleared regardless of rank.
- **D-02:** **70%+**, up from the ~54.6% baseline. Deliberately aggressive ~15-point lift.
  → ⚠ **PREMISE FALSIFIED — see GATE 1.** Codecov reports 78.28% today.
- **D-03:** Integration-flag coverage counts. Do NOT `codecov:ignore` code to move a number.
  → ⚠ **PARTIALLY FALSIFIED** — `cmd/holomush/core.go` and `sub_grpc.go` are **already**
  ignored (`.codecov.yml:69,74`).
- **D-04:** Require `codecov/patch` + `codecov/project` in the ruleset **early** at
  `threshold: 1%`; drop to `threshold: 0%` as the **final plan of the phase**.
  Reversibility: costly (operator round-trip on ruleset `11923801`).
- **D-05:** Backfill MUST be **genuine behavioral tests** (positive + negative, real
  assertions). A test written only to move a percentage is a QUAL-03 violation.

**QUAL-03 — Weak tests + ACE naming**
- **D-06:** Scope = **union** of (a) re-derived `holomush-ec22.15`/`ec22.16` site lists and
  (b) a fresh repo-wide mechanical predicate sweep.
- **D-07:** A violation is **`TestX_Y` (underscore form) whose function declares NO
  subtests**, plus vague subtest strings. The `TestType_Method`-with-subtests exception MUST
  be honoured.
- **D-08:** **Fix all hits — no allowlist.** Reversibility: costly.
  → ⚠ **SIZING GATE FIRED — see GATE 2.** 1,106 hits vs D-09's ~150 threshold.
- **D-09:** Research **runs the predicate and reports the hit count BEFORE planning commits
  to D-08.** Above ~150 renames, triggers an explicit re-scope conversation.
- **D-10:** **ACE sweep runs LAST, as a single pass.**
- **D-11:** The four `test/integration/eventbus_e2e/` skip files — **trim the body to
  `Skip` + a live GitHub issue reference**, deleting the maintained dead code.

**QUAL-04 — Session-lifecycle test matrix**
- **D-12:** Full 12×4 matrix as `holomush-izk0` specifies. Each cell gets a passing spec or
  an explicit "covered elsewhere" pointer. Acceptance: `task test:int --
  ./test/integration/session/...` runs **≥15 specs**. Telnet column included deliberately.
- **D-13:** **Committed matrix + meta-test** modelled on `test/meta/quarantine_registry_test.go`.
- **D-14:** Fold in **both** privacy items: `holomush-dqd1`'s two named tests **and** **#4682**.
- **D-15:** Add a **timestamped emit variant** — `EmitDirectEventAt(..., at time.Time)` or an
  option argument alongside `Session.EmitDirectEvent`. No sleeps.
- **D-16:** `test/integration/auth/multi_tab_test.go` MUST be **cited**, not duplicated.

**QUAL-05 — Code-health & security-polish batch**
- **D-17:** **Four of five land; #4792 is deferred.** In: #4793, #4794, #4796, #4797.
  Deferred with rationale: **#4792** (perf change needing benchmarks + `crypto-reviewer`).
  **Leave #4792 open and comment the deferral on it.**
- **D-18 (#4794):** **Invert the default.** Secure cookies + HSTS + CSP ON unless explicitly
  disabled for local dev. **Requires a release note.** Reversibility: costly.
- **D-19 (#4796):** **Plain `CREATE INDEX IF NOT EXISTS`** + paired `DROP INDEX IF EXISTS`.
  No `CONCURRENTLY` precedent exists. Reversibility: costly (migration number consumed).
- **D-20:** De-slop / humanization — **out of scope, explicitly deferred with rationale.**
- **D-21:** Fold the cookie/TLS-coupling item only (it *is* #4794). **Defer** argon2
  dummy-hash entropy, `http.Server` write timeout, `addlicense` pin as separate small issues.

**Phase shape**
- **D-22:** **One PR on the milestone branch** `gsd/v0.12-milestone`. Worktree already created.

### Claude's Discretion

- Exact risk weighting in the D-01 coverage ranking.
- The precise D-09 re-scope threshold (~150 is a starting number, not a requirement).
- Whether the D-15 timestamped emit is a new method or a variadic option.
- Wave/plan decomposition within D-04 (gate-early) → backfill → D-10 (ACE-sweep-last).
- Whether the D-13 matrix meta-test lives in `test/meta/` or beside the suite.

### Deferred Ideas (OUT OF SCOPE)

- **#4792** DEK read-cache. **De-slop / humanization** (`holomush-89o9`).
- **`holomush-ec22.9` residue** — argon2 dummy-hash entropy, `http.Server` write timeout,
  `addlicense` pin.
- **`holomush-ec22.13`** — ~16 `time.Sleep` sites. **`holomush-ec22.14`** — ~20 string-match
  error assertions. **`holomush-ec22.22`** — archive stale plans.
- **Implementing** the four `eventbus_e2e` TODO tests (D-11 only trims scaffolding).
- **Phase-8 merge follow-ups** — #4830, #4831, #4850, #4829, #4828.
- **`loader.go` (1142 LoC)** split.

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| QUAL-02 | Packages under the reconciled bar are backfilled with genuine behavioral tests | GATE 1: project target already met (78.28%); scope reduces to the `holomush-0yo6` floor gaps + D-04 gate wiring. Risk-ranked table supplied. |
| QUAL-03 | Skeleton/weak tests remediated; ACE naming violations corrected | GATE 2: 1,106 hits at D-07's literal predicate; tightened predicate yields 114. Weak-test half is near-empty (arch-review sweep + ec22.16 re-derivation). |
| QUAL-04 | Session-lifecycle test matrix (connect/reconnect/multi-char/idle-timeout) | izk0 matrix recovered verbatim (38 populated cells); harness fully built (8/8 helpers verified, zero TODO panics); D-15's exact gap identified. |
| QUAL-05 | Code-health & security-polish batch (arch-review Medium cluster) | All 4 in-scope issues OPEN; all cited `path:line` sites verified accurate; migration number 000053 confirmed free. |

---

# GATE 1 — QUAL-02 Coverage Sizing

## VERDICT: The 70% target is already exceeded. D-02's premise is falsified.

### How the number was obtained

Three independent measurements, all run in this session in the worktree at
`gsd/v0.12-milestone` (HEAD `42d0f66a9`):

| # | Method | Result | What it measures |
|---|--------|--------|------------------|
| 1 | `task test:cover` → `go tool cover -func=coverage.out \| tail -1` | **54.3%** | Raw unit-only, **no** `.codecov.yml` ignores applied |
| 2 | Merged `coverage.out` + `coverage-int.out`, `.codecov.yml` `ignore:` applied | **82.25%** (31290/38042 stmts) | Local reconstruction of what codecov sees |
| 3 | `api.codecov.io/api/v2/github/holomush/repos/holomush/branches/main/` | **78.28%** (44997/57480 lines, 607 files, `sessions: 2`) | **Authoritative — this is what the gate reads** |

Both runs exited 0: unit `DONE 10366 tests, 4 skipped in 78.5s`; integration
`DONE 10786 tests, 7 skipped in 141.2s` (Docker present, testcontainers ran).

**Measurement 1 is where `~54.6%` comes from.** [VERIFIED: `go tool cover` run this session]

### The falsification, precisely

`.codecov.yml:29` states *"The baseline (~54.6%) is NOT retroactively blocked"* in the
`project` status block — attributing a raw-`go-tool-cover` figure to codecov's project
status. `git log -S'54.6' -- .codecov.yml` shows the line was introduced by
`f063b8045` (*"feat(ops): … & coverage ratchet (Phase 6) (#4819)"*). CONTEXT.md D-02
inherits it. [VERIFIED: `git log -S`]

The gap between 54.3% and 78.28% is fully explained by two mechanisms, both measured:

1. **`.codecov.yml:46-90` `ignore:` excludes 20,415 statements** that `go tool cover`
   counts. Breakdown measured this session:

   | Statements | Pattern (`.codecov.yml` line) |
   |-----------:|-------------------------------|
   | 13,768 | `**/*.pb.go` (`:50`) |
   | 2,465 | `**/mocks/**` (`:58`) |
   | 2,034 | `**/mock_*.go` (`:59`) |
   | 990 | `pkg/proto/**` (`:51`) |
   | 371 | `cmd/holomush/core.go` (`:69`) |
   | 285 | `cmd/holomush/sub_grpc.go` (`:74`) |
   | 181 | `internal/cluster/clustertest/**` (`:89`) |
   | 166 | `plugins/*/main.go` (`:64`) |
   | 103 | `internal/eventbus/eventbustest/**` (`:90`) |
   | 52 | metrics stubs (`:78,83,84`) |
   | **20,415** | **total** |

   Generated code alone (`.pb.go` + `pkg/proto`) is 14,758 statements — 72% of the ignored
   mass, and essentially 0% covered. Excluding it moves unit-only from 54.3% → **68.15%**.

2. **The integration/e2e upload session.** `.codecov.yml:20` `after_n_builds: 2` merges the
   unflagged unit session with the merged flagged integration+e2e session. Adding
   `coverage-int.out` moves 68.15% → **82.25%** locally. Codecov's 78.28% is lower than my
   82.25% because codecov counts *lines* with a separate `partials` bucket (3,140 partials
   counted against the ratio) where Go counts *statement blocks*; and codecov's third
   session is e2e, which I did not run. The two figures agree on the conclusion.
   [VERIFIED: codecov API v2, `sessions: 2`, `totals.coverage: 78.28`]

### Statement count needed to reach 70%

**Negative — the target is already met.** From the merged local profile
(31,290 covered / 38,042 in-scope statements):

| Target | Statements needed |
|--------|------------------:|
| 60% | **−8,465** (already exceeded) |
| 65% | **−6,563** (already exceeded) |
| 70% | **−4,661** (already exceeded) |

On codecov's own numbers, 70% of 57,480 lines is 40,236 hits; the repo has 44,997.
**Surplus: 4,761 lines / 8.28 percentage points.**

For completeness, had the 54.6% premise been true, the lift would have been ~+5,800
statements — roughly 250–400 new test functions. That work is not required.

### `holomush-0yo6` floor set — the real, tractable QUAL-02 scope

Measured against codecov's authoritative per-package data on `main`:

| Package | Floor | Today | Verdict | Uncovered lines |
|---------|------:|------:|---------|----------------:|
| `cmd/holomush` | ≥80% | **64.8%** | **GAP −15.2 pts** | 986 |
| `internal/tls` | ≥80% | **76.2%** | **GAP −3.8 pts** | 72 |
| `internal/xdg` | ≥80% | **97.9%** | **PASS** | 1 |
| `internal/core` | ≥90% | **91.6%** | **PASS** | 13 |

[VERIFIED: codecov API v2 report endpoint, `branch=main`]

Two of four already pass. `internal/tls` needs ~12 lines covered to clear 80%.
`cmd/holomush` is the only substantial item — and note it is measured **with**
`core.go` (371 stmts) and `sub_grpc.go` (285 stmts) already excluded per `.codecov.yml:69,74`.

> ⚠ **D-03 conflict.** D-03 says *"Do NOT mark such code ignored to make a number move"* and
> names `runCore()` (`cmd/holomush/core.go:166`) as the example. That code **is already
> ignored** — `.codecov.yml:69` (`cmd/holomush/core.go`) and `:74` (`sub_grpc.go`), both
> landed before this phase with inline rationale. `holomush-0yo6`'s own text anticipated
> this: *"`runCore()` may be intentionally `codecov:ignore`'d as integration-test-covered
> …; confirm with owner whether the ignore satisfies the original acceptance criterion."*
> **This is an open question for the planner** (see Open Questions #1), not something
> research can settle. D-03's *forward-looking* instruction (don't add new ignores) is
> unaffected and should be honoured.

### D-01 risk-ranked audit (uncovered lines × blast radius)

Weights applied (Claude's discretion per CONTEXT.md): 3.0 for the five CONTEXT-named
high-blast-radius trees (`internal/eventbus`, `internal/access`, `internal/crypto`,
`internal/session`, `internal/world`, incl. subpackages) plus `internal/eventbus/crypto/*`
and `internal/admin/readstream` (crypto read path); 2.0 for `cmd/holomush` (floor-set
obligation); 1.5 for other `internal/`+`pkg/`; 1.0 for `plugins/` and `test/`.
Rows with <25 uncovered lines omitted.

| Risk | Uncov | Wt | Cov% | Lines | Package |
|-----:|------:|---:|-----:|------:|---------|
| 1972 | 986 | 2.0 | 64.8% | 2803 | `cmd/holomush` ← floor-set |
| 1893 | 631 | 3.0 | 71.0% | 2177 | `internal/eventbus/crypto/dek` |
| 1737 | 579 | 3.0 | 73.8% | 2209 | `internal/world/postgres` |
| 1324 | 1324 | 1.0 | 76.6% | 5658 | `plugins/core-scenes` |
| 1132 | 755 | 1.5 | 76.1% | 3165 | `internal/grpc` |
| 744 | 248 | 3.0 | 80.1% | 1246 | `internal/eventbus/audit` |
| 702 | 468 | 1.5 | 84.9% | 3098 | `internal/plugin` |
| 696 | 232 | 3.0 | 80.0% | 1158 | `internal/eventbus` |
| 657 | 219 | 3.0 | 83.1% | 1298 | `internal/eventbus/history` |
| 648 | 432 | 1.5 | **38.2%** | 699 | `internal/plugin/luabridge` ← lowest coverage of any large pkg |
| 543 | 181 | 3.0 | 78.9% | 857 | `internal/admin/readstream` |
| 540 | 360 | 1.5 | 76.8% | 1551 | `internal/store` |
| 525 | 175 | 3.0 | 88.0% | 1453 | `internal/world` |
| 513 | 171 | 3.0 | 63.3% | 466 | `internal/world/outbox` |
| 506 | 506 | 1.0 | 68.7% | 1615 | `plugins/core-channels` |
| 417 | 278 | 1.5 | 83.9% | 1728 | `internal/web` |
| 380 | 253 | 1.5 | 83.2% | 1507 | `internal/testsupport/integrationtest` |
| 369 | 123 | 3.0 | 88.4% | 1059 | `internal/access/policy` |
| 336 | 112 | 3.0 | 89.6% | 1078 | `internal/access/policy/attribute` |
| 333 | 222 | 1.5 | **24.5%** | 294 | `internal/grpcclient` |

`internal/session` is **63.1%** (55 uncovered lines) — small in absolute terms but a
CONTEXT-weighted package, and QUAL-04's matrix will exercise much of it as a side effect.
`internal/access` itself is 98.4% (2 uncovered lines). [VERIFIED: codecov API v2]

### D-04 gate state — the actual durable win

```
gh api repos/holomush/holomush/rulesets/11923801
→ {"name":"protect-main","enforcement":"active",
   "checks":["Build","Lint","Test","CodeRabbit","Integration Test",
             "E2E Test","Conventional Commit (PR title)","Vuln"]}
```

**`codecov/patch` and `codecov/project` are confirmed absent.** [VERIFIED: `gh api`]
The Phase 6 gap D-04 targets is real and still open. Note `Vuln` **is** present — Phase 6's
"once the operator adds it" caveat in `.claude/rules/testing.md:34` is now stale.

On live PRs, `codecov/patch` posts and passes (#4832, #4849 both `SUCCESS`), and
`codecov/project` does not appear at all in the status rollup — consistent with codecov
only emitting `project` when there is a base to compare against.

### D-04 ordering hazard — what it actually implies

CONTEXT.md warns that requiring the statuses early means every PR merged during the phase
must clear them. The concrete mechanics:

- **`project` at `threshold: 1%` is genuinely permissive at 78.28%.** A 1-point drop is
  ~575 lines of net-newly-uncovered code. Behaviour-preserving refactors do not approach
  that. **Deleting** covered code *raises* project coverage (removes hits and lines in the
  same ratio only if the deleted code was at exactly the project average — deleting
  *well-covered* code lowers it). The realistic failure mode is deleting a large,
  well-covered block; nothing in Phase 9's scope does that.
- **`patch` at `target: 80%, threshold: 5%` is the sharper edge** — it demands 75%+ on
  *changed lines*. The `#4794` inversion and `#4797` log/metric add are small diffs where a
  single uncovered `slog.*Context` error branch can sink the ratio. Carried forward from
  Phase 6: multi-line `slog.*Context` calls count as many lines each.
- **`notify.after_n_builds: 2` + `wait_for_ci: true` (`.codecov.yml:20-21`)** mean the
  status does not post until both sessions land. A plan whose PR-level verification greps
  for a codecov status too early will see nothing, not a failure. Judge by the final
  rollup, not an early poll.
- **Sequencing recommendation:** land D-04's ruleset change **after** the #4794/#4797 code
  plans (which carry the thinnest patch margins) and **before** the ACE sweep (D-10, which
  is rename-only and touches `_test.go` files — excluded from coverage by
  `.codecov.yml:57`, so it produces an *empty* patch and cannot fail either status). This
  inverts D-04's literal "early" but honours its intent (gate landed within the phase, tightened
  last) without stranding the phase's own plans. **Flagging as a planner decision, not a
  research override.**

### GATE 1 re-scope options for the planner

D-02 as written has nothing to do. Three honest options:

1. **Re-point (recommended).** Restate QUAL-02 as: *close the `holomush-0yo6` floor gaps
   (`cmd/holomush` → ≥80%, `internal/tls` → ≥80%) and wire D-04's gate*, and record that the
   70% project target was already met at 78.28% with the measurement that proves it.
   Correct `.codecov.yml:29`'s stale `~54.6%` comment in the same change — leaving it invites
   the next phase to repeat this error.
2. **Raise the target to a real ratchet.** Set the *project* expectation at the measured
   78.28% and let D-04's `threshold: 0%` final plan make it a true no-drop floor. This is
   arguably what D-02's *intent* (an aggressive, durable bar) actually wants.
3. **Keep 70% as written.** It passes today with no work. Cheapest, but records a success
   criterion that was never a constraint.

**Do not manufacture tests to "reach" 70%** — that is precisely the coverage theatre D-05
forbids.

---

# GATE 2 — QUAL-03 ACE Predicate Sizing

## VERDICT: 1,106 hits — 7.4× D-09's ~150 threshold. Re-scope conversation is required.

### How the number was obtained

D-07's predicate implemented as a throwaway `go/ast` walker (`/tmp/p9ace/main.go`, **not
committed** — productionizing it is a planning decision, see below). It parses every
`*_test.go` under the repo (excluding `.git`, `node_modules`, `web`, `site`, `.planning`),
identifies top-level `func TestX(t *testing.T)` declarations, and classifies:

- **subtest-declaring** = the body contains any `X.Run(<first-arg>)` call (literal *or*
  dynamic, e.g. `t.Run(tt.name, …)`) — this is the sanctioned-exception detector.
- **vague subtest** = a `t.Run` string literal *or* a table-driven `name:`/`desc:` field
  value matching the vague vocabulary.

### The counts

```
total Test funcs:                                    6294
underscore-form total:                               1572
  with subtests (SANCTIONED EXCEPTION):               466
  without subtests (D-07 VIOLATION):                 1106
table-driven case strings scanned:                   2770
vague-subtest hits (broad vocabulary):                 13
TOTAL HITS:                                          1119
```

**Against D-09's ~150 threshold: 1,106 top-level renames is 7.4× over.**

### CONTEXT.md's "~1653 false positives" claim — close, and the conclusion drawn from it is wrong

The measured underscore-form total is **1,572** (vs the cited ~1,653 — 5% drift, consistent
with three months of churn). But the *inference* CONTEXT.md draws is the problem: it implies
honouring the exception is what makes the predicate tractable. It is not. The exception
removes **466 of 1,572 (29.6%)**. The remaining **1,106** is still an order of magnitude
past the re-scope threshold. [VERIFIED: AST walk this session]

### Why the count is so high — the predicate over-fires

A random sample of 40 violations (seed 7):

```
internal/logging/handler_test.go:151          TestSetupWithBridge_AllDisabledDiscards
internal/world/payloads_test.go:1028          TestBuildObjectMovePayload_CarriesFromAndToContainment
plugins/core-scenes/commands_emit_test.go:420 TestHandleEmit_UnfocusedTwoMembershipsPreservesAmbiguityError
internal/access/grants_test.go:99             TestHasPlayerGrant_RejectsEmptyPlayerID
internal/world/postgres/cascade_delete_test.go:192
                       TestCascadeDelete_Object_RollsBackPropertiesOnParentDeleteFail
internal/admin/readstream/handler_test.go:372 TestINV_CRYPTO_54_AuditPublishFailRefuses
internal/grpc/location_follow_test.go:383     TestBuildLocationStateRendering_UnregisteredVerbFailsClosed
internal/eventbus/crypto/dek/negative_path_internal_test.go:52
                       TestHexEncodeBytes_EncodesLowercaseHex
```

Every one of these **already satisfies ACE** — Action, Condition, and Expectation are all
present. They use `_` as a separator between the unit-under-test and the behaviour clause.
`.claude/rules/testing.md:59` grades underscores as **`SHOULD NOT`** ("*SHOULD NOT use
underscores*"), not `MUST NOT`; the `MUST` at `:57` is *"Every test name communicates action,
condition, and expectation"* — which these satisfy. **D-07's predicate tests the SHOULD, not
the MUST.**

This is structurally the same defect the arch-review's zero-assertion sweep avoided
(`docs/reviews/arch-review/2026-07-11/findings/d9a-testing-ci.md:57`): a mechanically clean
predicate whose hits are overwhelmingly legitimate.

### Calibration against the known-clean sweep

The arch-review sweep list is about *assertions*, not *names*, so it cannot false-positive
under a naming predicate — but I checked explicitly:

| File from the arch-review clean list | Flagged by D-07 predicate? |
|--------------------------------------|----------------------------|
| `internal/plugin/emit_intent_parity_test.go` | clean |
| `internal/eventbus/scopecheck_test.go` | clean |
| `internal/plugin/hostcap/session_test.go` | clean |
| `internal/plugin/hostcap/world_test.go` | clean |
| `internal/auth/auth_service_test.go` | clean |

**All five clean.** [VERIFIED: cross-check against `/tmp/p9ace/hits.json`]
The naming predicate and the assertion predicate are orthogonal; the calibration concern in
CONTEXT.md's planner flag applies to the **weak-test half (ec22.16)**, not the ACE half —
and the arch-review already ran that sweep to a zero-real-hit conclusion.

### A tightened predicate — the re-scope release valve

Decomposing the 1,106 by the descriptiveness of the trailing segment (tokenising the final
`_`-delimited segment into CamelCase words):

| Bucket | Count | Character |
|--------|------:|-----------|
| `TestINV_<SCOPE>_<N>_*` invariant-binding names | **25** | **MUST NOT rename** — see below |
| tail is 3+ CamelCase tokens (full expectation clause) | 658 | Already ACE-compliant |
| tail is 2 tokens | 309 | Mostly compliant |
| tail is a single token (bare method/noun — no expectation) | **114** | **Genuine ACE violations** |

The 114 single-token-tail names are the real defect population:

```
TestStatus_String            TestPropertyRepository_Delete    TestClient_Subscribe
TestCommandRequest_Fields    TestGatewayCommand_Properties    TestParseContextFlag_Empty
TestProperty_Fields          TestAdminBootstrapper_Priority   TestStatus_Help
TestSentryLogsTarget_Invalid TestFormatEvent_System           TestMigrateCommand_Help
```

These are topics, not sentences — no expectation clause. `TestStatus_String`
(`internal/session/session_test.go:32`) is on **both** ec22.15's hand-curated list and this
bucket, which is a good sign the tightened predicate tracks the original intent.

**114 is within D-09's ~150 threshold.** [VERIFIED: AST classification this session]

> ⚠ **`TestINV_*` must be exempted explicitly.** 25 functions use the
> `TestINV_<SCOPE>_<N>_<Behaviour>` form (e.g.
> `internal/admin/readstream/deadline_writer_test.go:77`
> `TestINV_CRYPTO_64_SendWithDeadlineTrips`; `internal/logging/import_guard_test.go:17`
> `TestINV_L1_LoggingHasNoSentryImport`). The underscores encode the registry id, which is
> the whole point — `.claude/rules/invariants.md` binds these via `// Verifies: INV-<SCOPE>-N`
> annotations (**100 test files** carry such an annotation). Renaming them destroys the
> id↔test readability that `TestBoundInvariantsAreGenuinelyAsserted`
> (`test/meta/invariant_registry_test.go`) and human reviewers depend on. Any productionized
> predicate MUST carve these out. [VERIFIED: `rg 'func TestINV'`, `rg -c 'Verifies: INV-'`]

### Vague subtest strings — CONTEXT.md's "only one" is falsified

CONTEXT.md's planner flag: *"Only **one** vague subtest string exists repo-wide today."*
Measured: **13 hits** under a broad vocabulary, **9** under D-07's own literal enumeration
(`"success"`, `"error case"`, `"happy path"`, `"test N"`):

| File:line | Test | String |
|-----------|------|--------|
| `cmd/holomush/automigrate_test.go:318` | `TestRunAutoMigration` | `"success"` |
| `cmd/holomush/cmd_admin_totp_run_test.go:84` | `TestRunBootstrapEnroll` | `"happy path"` |
| `cmd/holomush/cmd_admin_totp_run_test.go:181` | `TestRunEnroll` | `"happy path"` |
| `internal/store/player_session_store_test.go:66,137,278,333,449,768` | 6× `TestPostgresPlayerSessionStore_*` | `"happy path"` ×6 |
| *(broad vocabulary only, judgement calls)* | | |
| `internal/command/handlers/shutdown_test.go:112` | `TestShutdownHandler_InvalidDelay` | `"negative"` |
| `internal/plugin/hostfunc/world_write_format_test.go:19` | `TestFormatAllowedEntityTypes` | `"empty"` |
| `internal/plugin/manifest_test.go:333` | `TestParseManifest_ValidNames` | `"simple"` |
| `internal/world/character_test.go:490` | `TestValidateCharacterName` | `"empty"` |

CONTEXT.md's own earlier planner flag contradicts its "only one" claim by naming *"six
`happy path` subtests in `internal/store/player_session_store_test.go`"* — the six are
real (line numbers drifted +2 from ec22.15's `64,135,276,331,447,766`). The subtest half is
**9–13 fixes, not 1** — still small, and CONTEXT.md's conclusion that *"the top-level naming
half carries the work"* holds. [VERIFIED: AST walk]

### ec22.15 / ec22.16 re-derivation against HEAD

Recovered verbatim from `.planning/archive/beads/2026-07-09-beads-live.jsonl`.

**`holomush-ec22.15` (ACE, "~15 violations"):**

| Cited site | Status at HEAD |
|------------|----------------|
| `internal/session/memstore_test.go:18,36,44,110,138,173,338,397` (8 sites) | **FILE GONE** — `ls` → `No such file or directory` |
| `internal/store/alias_test.go:579` `TestAliasRepositoryInterface` | **SURVIVES** at `:579` |
| `internal/store/alias_test.go:588` `TestNewPostgresAliasRepository` | **SURVIVES** at `:588` |
| `internal/world/object_test.go:17,53,102,125` `TestContainment_{Constructors,Validate,Type,ID}` | **ALL 4 SURVIVE** at exactly those lines |
| `internal/session/session_test.go:32` `TestStatus_String` | **SURVIVES** at `:32` |
| `internal/store/player_session_store_test.go` 6× `"happy path"` | **SURVIVE**, lines drifted `64,135,276,331,447,766` → `66,137,278,333,449,768` |
| `internal/plugin/manifest_test.go:326` `{name: "simple"}` | **SURVIVES**, drifted to `:333` |

**`holomush-ec22.16` (weak tests):**

| Cited site | Status at HEAD |
|------------|----------------|
| `internal/session/memstore_test.go:36,233,397` (3 sites) | **FILE GONE** |
| `internal/store/alias_test.go:579,588` | **SURVIVE** (same two as ec22.15) |
| `internal/access/resolver_test.go:15` `TestLocationResolverSatisfiesInterface` | **GONE** — file exists, function does not (`rg -ln 'SatisfiesInterface'` → no hits repo-wide) |
| 4× `test/integration/eventbus_e2e/*_test.go` skip files | **ALL 4 SURVIVE** |

**Survivor tally: 13 of 24 cited sites.** CONTEXT.md's "cited 8 times / no longer exists"
claim for `memstore_test.go` is **confirmed** (8 in ec22.15 + 3 in ec22.16 = 11 dead
citations). The `resolver_test.go` compile-canary is a **fourth** drifted-away site
CONTEXT.md did not flag. [VERIFIED: `ls`, `rg`]

The surviving weak-test population is **4 functions** (`TestAliasRepositoryInterface`,
`TestNewPostgresAliasRepository`, plus the 2 that are also ACE hits) — the arch-review's
zero-real-hit sweep and this re-derivation agree the weak-test half of QUAL-03 is nearly
empty.

### D-11 — the four skip files

| File | Lines | Skip at | Retired bead |
|------|------:|--------:|--------------|
| `test/integration/eventbus_e2e/audit_drift_detector_test.go` | 70 | `:36` | `holomush-ecbg` |
| `test/integration/eventbus_e2e/js_storage_corruption_test.go` | 75 | `:38` | `holomush-6nds` |
| `test/integration/eventbus_e2e/multi_protocol_fanout_test.go` | 77 | `:36` | `holomush-nko7` |
| `test/integration/eventbus_e2e/backfill_rebuild_test.go` | 60 | `:28` | `holomush-l4kx` |

**282 lines total**, of which ~200 are unreachable-but-compiled setup.

> ⚠ **Two corrections to CONTEXT.md / ec22.16.** (a) The call is Ginkgo **`Skip(...)`**, not
> **`t.Skip(...)`** — it is the first statement inside an `It(...)` closure, not the first
> line of a `func TestX(t *testing.T)`. A planner instructing an executor to "find the
> `t.Skip` first line" will find nothing. (b) It is not literally the file's first line;
> `backfill_rebuild_test.go:26-28` shows `Describe` → `It` → `Skip`.
> [VERIFIED: `rg -n 'Skip\(' test/integration/eventbus_e2e/`, `sed -n '25,55p'`]

**Issue mapping is NOT satisfiable from existing issues.** `gh issue list --search` for each
bead id returns: `holomush-nko7` → no results; `holomush-6nds` → no results; `holomush-l4kx`
→ only `#2856` (CLOSED, unrelated: *"Wire EVENTS_AUDIT_DLQ for audit projection MaxDeliver
exhaustion"* — a substring false match); `holomush-ecbg` → same false match.
**All four need new GitHub issues filed** before D-11's trim, or the references dangle
exactly as CONTEXT.md's planner flag warns. This is a prerequisite task, not a side effect.
[VERIFIED: `gh issue list --search` ×4]

### ACE predicate productionization — where it should live

| Option | Mechanics | Assessment |
|--------|-----------|------------|
| **`gorules/analyzers/<name>/` module plugin** | One `init() { register.Plugin("<name>", newPlugin) }` per enableable linter id. There are exactly **11** today, 1:1 with `.golangci.yaml`'s custom list — an ACE analyzer would be the **12th**, requiring: a new `gorules/analyzers/acetestnaming/` package + `plugin.go` + `analyzer.go` + `analysistest` fixtures, an entry in `.golangci.yaml` `linters.settings.custom` **and** `linters.enable`, and a `./bin/custom-gcl` rebuild. **Critical:** golangci-lint's default config excludes `_test.go` from several linters via `linters.exclusions.rules` — an analyzer whose *entire domain* is `_test.go` needs its exclusions verified, not assumed. | Real-time feedback in `task lint`; highest integration cost; the v2 one-plugin-one-id constraint is already correctly respected in this repo (arch-review d9a confirms 11:11), so the pattern is proven. |
| **`test/meta/` walker** (recommended) | A plain `func TestACENamingRegistry(t *testing.T)` doing `filepath.WalkDir` + `go/parser`, exactly the shape of `test/meta/quarantine_registry_test.go:35,76` (`TestQuarantineRegistryBijection` + `filepath.WalkDir`). Runs under `task test`; no `custom-gcl` rebuild; no golangci-lint exclusion interactions; sits beside the two existing ratchets (`quarantine_registry_test.go`, `invariant_registry_test.go`). | Lower cost, same ratchet strength, established in-repo precedent. Loses editor-time feedback. **Recommended.** |

[VERIFIED: `rg -n 'register.Plugin\(' gorules/` → 11 calls;
`docs/reviews/arch-review/2026-07-11/findings/d9a-testing-ci.md:57` confirms the 11:11 match]

### GATE 2 re-scope options for the planner

1. **Tighten the predicate to the single-token-tail form + keep D-08's "fix all"
   (recommended).** ~**114 renames**, within D-09's threshold, targeting the names that
   genuinely fail the `MUST` at `.claude/rules/testing.md:57`. Exempt `TestINV_*`. Preserves
   D-08's no-allowlist intent because the *tightened* predicate's hit set is fully
   remediated — the end state is clean against the gate that ships.
2. **Keep D-07 literal, drop D-08.** 1,106 renames is a multi-thousand-line mechanical diff
   across ~50 packages that would conflict with every concurrent branch (D-08's own
   reversibility note flags this), and would rename ~658 names that are already correct.
   Not recommended.
3. **Keep D-07 literal, seed a shrink-only allowlist.** Explicitly rejected by D-08 and by
   CONTEXT.md's `<specifics>`. Listed for completeness.

Option 1 uses D-09 exactly as designed: *"the sizing gate is the release valve; use it
rather than quietly reintroducing an allowlist."*

---

## QUAL-04 — Session-Lifecycle Matrix

### The `holomush-izk0` matrix, verbatim from the archive

Recovered from `.planning/archive/beads/2026-07-09-beads-live.jsonl`
(`holomush-izk0`, status `open`):

| Transition | Web guest | Web regular char | Telnet | Multi-session |
|---|---|---|---|---|
| Fresh `SelectCharacter` → Active | ✓ | ✓ | ✓ | n/a |
| Reattach within TTL (SelectCharacter path) | ✓ | ✓ | ✓ | ✓ |
| Reattach within TTL (Subscribe.ReattachCAS) | ✓ | ✓ | ✓ | ✓ |
| Detach (drop all connections) → status=Detached + TTL | ✓ | ✓ | ✓ | n/a |
| Reaper sweep at TTL expiry → row deleted | ✓ | ✓ | ✓ | n/a |
| Post-TTL re-login → fresh session (new LocationArrivedAt) | ✓ | ✓ | ✓ | n/a |
| `quit` command → row deleted | ✓ | ✓ | ✓ | n/a |
| Explicit logout → row deleted | ✓ | ✓ | ✓ | ✓ |
| Admin boot → row deleted | n/a | ✓ | ✓ | ✓ |
| Character move while attached → LocationArrivedAt advances | n/a | ✓ | ✓ | ✓ |
| Tmux-style telnet reattach (same playerSession, new connection) | n/a | n/a | ✓ | n/a |
| WiFi blip (transport-level drop + reconnect, no logout) | ✓ | ✓ | ✓ | ✓ |

> ⚠ **Correction to the research brief and D-12's framing: the matrix is 38 populated cells,
> not 48.** 12 rows × 4 columns = 48 *positions*, but **10 are `n/a` in izk0's own table**
> (multi-session on rows 1/4/5/6/7 and 11; web-guest/web-char on rows 9/10; web columns on
> row 11). Planning 48 cells would invent 10 specs izk0 explicitly says do not apply.
> Column totals: web-guest 9, web-regular-char 10, telnet 12, multi-session 7.

izk0's other acceptance criteria, verbatim:
- `task test:int -- ./test/integration/session/...` runs **≥15 specs**.
- Each row has ≥1 passing spec **or** an explicit `covered elsewhere` pointer.
- Spec IDs from the iwzt privacy spec (e.g. `I-PRIV-3`) asserted **by-ID**.
- The two `holomush-dqd1` privacy tests **live here**.

### izk0's stated blocker is gone — CONTEXT.md confirmed

izk0's `## Dependencies` says: *"harness fill-ins for `MoveTo`, `WaitForEvent`,
`QueryStreamHistory` are TODO panics today (per iwzt.6 worker report). Filling them in is
part of this task's scope OR a precursor bead."* All verified implemented:

| Helper | Location | Status |
|--------|----------|--------|
| `(*Session).WaitForEvent` | `internal/testsupport/integrationtest/session.go:171` | ✓ |
| `(*Session).MoveTo` | `session.go:303` | ✓ |
| `(*Session).DetachTransport` | `session.go:366` | ✓ |
| `(*Session).ReattachTransport` | `session.go:419` | ✓ |
| `(*Session).QueryStreamHistory` | `session.go:705` | ✓ |
| `(*Session).QueryStreamHistoryBounded` | `session.go:722` | ✓ |
| `(*Session).EmitDirectEvent` | `session.go:770` | ✓ |
| `(*Server).ExpireSession` | `harness.go:995` | ✓ |

`rg -n 'panic\("TODO|TODO\(' internal/testsupport/integrationtest/` → **zero hits.**
**QUAL-04 is spec authoring, not harness construction.** [VERIFIED: `rg` this session]

### D-15 — the exact remaining gap

```go
// internal/testsupport/integrationtest/session.go:770
func (s *Session) EmitDirectEvent(ctx context.Context, stream, evType string, payload []byte) error
```

No timestamp parameter — confirmed. This *is* option (B) that `holomush-hdnx` recommended
building (*"Add a direct-emit helper to `Server` or `Session` … `(…, at time.Time)` — bypasses
the command layer, writes directly to the event store / publisher, returns the event ULID
for assertion. … (B) is cleaner because it gives test-controlled timestamps. (A) emits at
`time.Now()` and would require sleeps for ordering."*). D-15 closes the single remaining
delta: the `at time.Time` parameter. hdnx also wants the **event ULID returned** for
assertion — the current signature returns only `error`. A variadic-option form
(`EmitDirectEvent(ctx, stream, evType, payload, WithEmitAt(t))`) preserves all existing
call sites; a sibling `EmitDirectEventAt` returning `(string, error)` satisfies hdnx's
return-the-ULID ask without touching them. **Claude's-discretion call per CONTEXT.md.**

### #4682 (`holomush-hdnx`) — the ABAC-override question CONTEXT.md flagged

CONTEXT.md asks whether #4682's floor-arm needs `WithRealABAC()` or an `allowAllPolicyEngine`
override. **Answer: neither — it needs the harness default.**
`internal/testsupport/integrationtest/harness.go:314` sets
`cfg := &startConfig{accessEngine: &allowAllPolicyEngine{}}` — **allow-all is the default**;
`WithRealABAC()` (`harness.go:249`) is the *opt-in* to real policy.
`test/integration/privacy/privacy_test.go:47` documents this: *"The harness uses
allowAllPolicyEngine which grants…"*. So the floor-arm spec gets its gate-bypass for free by
**not** passing `WithRealABAC()`. hdnx's *"via allowAllPolicyEngine override"* language
predates the consolidation. [VERIFIED: `rg -n` on harness.go]

### Existing suite inventory

| File | `It(` blocks | Lines |
|------|-------------:|------:|
| `test/integration/session/session_persistence_suite_test.go` | **0** | 108 |
| `test/integration/session/session_lease_test.go` | **3** | 97 |
| `test/integration/session/session_list_active_by_location_test.go` | **4** | 146 |
| `test/integration/telnet/telnet_suite_test.go` | **0** | 21 |

> ⚠ **Minor correction:** CONTEXT.md says `session_lease_test.go` has 4 blocks and
> `session_list_active_by_location_test.go` has 5. Measured `It(` counts are **3** and **4**.
> The discrepancy is `Describe`-vs-`It` counting. The ≥15-spec acceptance bar therefore
> starts from **7 existing `It` blocks**, not 9 — the new suite must contribute ≥8 to clear
> it, and the matrix implies far more. The empty-suite and telnet-column claims are both
> **confirmed**: `session_persistence_suite_test.go` is 108 lines of bootstrap with zero
> blocks, and `test/integration/telnet/` is a 21-line suite bootstrap only.
> [VERIFIED: `rg -c 'It\('`, `wc -l`]

### D-16 — what `multi_tab_test.go` actually covers

`test/integration/auth/multi_tab_test.go` — 7 `Describe` / 8 `It`:

| Line | Describe | Cites which matrix cell |
|-----:|----------|-------------------------|
| 31/56 | two-tab guest | Reattach-within-TTL × multi-session (guest) |
| 81/106 | same character in two tabs | Reattach-within-TTL × multi-session |
| 217/242 | browser cookie + concurrent telnet auth | Reattach × telnet + multi-session |
| 282/320 | two characters of one player | multi-session, distinct sessions |
| 386/411 | logout in tab 1, action in tab 2 | Explicit logout × multi-session |
| 508/531 | Subscribe post-logout | Explicit logout × multi-session (Subscribe path) |
| 626/650,661 | Pre-deploy `WebCheckSession` contract | (not a matrix cell) |

The multi-session column is **7 populated cells**; `multi_tab_test.go` plausibly covers 4–5
of them by citation. **D-16's anti-redundancy instruction is well-founded and cheap to
honour.** [VERIFIED: `rg -n 'It\(|Describe\('`]

### `holomush-dqd1` — the two named tests, verbatim

- `TestPrivacy_ReattachWithinTTLPreservesFloor` — A connects at T0 in L
  (`LocationArrivedAt=T0`); third party emits at T1; A's transport drops at T2
  (`status=Detached`); third party emits at T3; A's `Subscribe.ReattachCAS` at T4 (within
  TTL); A's history query for `location:L` returns events in [T0,T4] **including T1 and T3**.
- `TestPrivacy_TTLExpiryEndsSessionFreshFloor` — A connects at T0; drops at T1; no reattach
  for TTL+1; reaper deletes at T2; A logs in again at T3 → fresh `SelectCharacter` with
  `LocationArrivedAt=T3`; events in [T0,T3) **NOT visible**.

Both names are `TestPrivacy_*` underscore-form — **they are D-07 predicate hits on arrival.**
D-10's "ACE sweep runs LAST" ordering is what prevents this collision, but the planner should
note that dqd1 (and the iwzt spec §8 meta-test that *finds them by name*) pins these names.
Either exempt them or accept the spec-§8 meta-test breaks. Under the recommended tightened
predicate they are **not** hits (multi-token descriptive tails), which resolves it cleanly.

---

## QUAL-05 — Code-Health & Security Polish

All four in-scope issues verified **OPEN** via `gh issue view -R holomush/holomush`:

| # | Title | Labels |
|---|-------|--------|
| 4793 | security: attribute providers emit empty-string sentinel — latent fail-open (ADR ti1b) | bug, review-finding, abac |
| 4794 | security: secure-cookie/HSTS/CSP gated on a default-false flag — insecure behind a proxy | bug, review-finding, security |
| 4796 | data: sessions.location_id is unindexed — presence / who's-here query | bug, review-finding, observability |
| 4797 | reliability: plugin-decrypt audit emitter silently drops records (no log/metric) | bug, review-finding |
| 4682 | iwzt: I-PRIV-6 floor-preservation arm | priority::medium, migrated-from-beads |
| 4792 | perf: DEK caches bypassed on the encrypted read path | enhancement, review-finding — **deferred, leave open** |

### #4793 — ABAC empty-string sentinels: **all 7 sites confirmed exactly**

```
internal/access/policy/attribute/location.go:72    attrs["owner_id"] = ""
internal/access/policy/attribute/location.go:80    attrs["shadows_id"] = ""
internal/access/policy/attribute/object.go:117     attrs["owner_id"] = ""
internal/access/policy/attribute/object.go:125     attrs["held_by_character_id"] = ""
internal/access/policy/attribute/object.go:133     attrs["contained_in_object_id"] = ""
internal/access/policy/attribute/property.go:93    attrs["value"] = ""
internal/access/policy/attribute/property.go:102   attrs["owner"] = ""
```

CONTEXT.md's site list is **100% accurate** — every line number matches.
`character.go:139` is the already-fixed reference and carries the explanatory comment
(*"un-locatable. Emitting \"\" would satisfy `\"\" == \"\"` and create a…"*).
`stream.go:40-48` is the second reference form. `.claude/rules/abac-providers.md` auto-loads
on this path and mandates: omit the key entirely; **always** emit the `has_X` witness on
every code path. **`abac-reviewer` MUST run before push.** [VERIFIED: `rg -n '= ""'`]

### #4794 — secure-cookie inversion: plumbing mapped

| Site | Content |
|------|---------|
| `cmd/holomush/gateway.go:40` | `SecureCookies bool \`koanf:"secure_cookies"\`` |
| `cmd/holomush/gateway.go:120` | `cmd.Flags().BoolVar(&cfg.SecureCookies, "secure-cookies", false, "…default false for local plain-HTTP dev")` ← **the default to invert** |
| `cmd/holomush/gateway.go:314` | `Secure: cfg.SecureCookies` |
| `internal/web/cookie.go:45` | `func sessionCookie(value string, maxAge int, secure bool) *http.Cookie` |
| `internal/web/cookie.go:52,55-56` | `Secure: true` then `if !secure { c.Secure = false }` ← **downgrade**, already correct |
| `internal/web/security_headers.go:74,80-82` | `SecurityHeadersMiddleware(secure bool, …)`; `if secure { HSTS; CSP }` |

CONTEXT.md's characterisation is **exactly right**: cookie *construction* already defaults to
`Secure: true` and downgrades; the defect is the **flag's default and plumbing**. The
inversion is a one-line default flip at `gateway.go:120` plus a flag-name/semantics decision
(`--secure-cookies` default-true, or a new `--insecure-cookies`/`--dev-mode` opt-out).
**Existing tests pin the current default** — `cmd/holomush/gateway_test.go:543-545` asserts
*"secure-cookies defaults false so local plain-HTTP dev keeps working"* — so the inversion
**must update that test**, which is a good D-18 acceptance anchor. **Release note required.**
[VERIFIED: `rg -n`]

### #4796 — `sessions.location_id` index: confirmed unindexed, 000053 free

All indexes on `sessions` across all 52 migrations:

```
000001_baseline.up.sql:221  idx_sessions_active_character  ON sessions (character_id) WHERE status IN ('active','detached')
000001_baseline.up.sql:223  idx_sessions_status            ON sessions (status) WHERE status = 'detached'
000008_session_player_fk.up.sql:12  idx_sessions_player_session_id ON sessions(player_session_id)
```

**No index on `location_id`** — confirmed. (`idx_characters_location` at `:76` and
`idx_objects_location` at `:152` are on *other* tables — an easy misread.)
Last migration on disk is `000052_events_audit_partition.{up,down}.sql`; **next free is
`000053`** — CONTEXT.md correct. D-19's cited precedent verified:
`000008_session_player_fk.up.sql:12` `CREATE INDEX IF NOT EXISTS` + `000008_…down.sql:4`
`DROP INDEX IF EXISTS`. `.claude/rules/database-migrations.md` applies (idempotent, paired,
no triggers/functions, 6-digit zero-padded). [VERIFIED: `rg -n`, `ls`]

### #4797 — silent audit-emitter drop

`internal/eventbus/history/plugin_downgrade_fence.go:423` → `if f.emitter == nil {` —
**exact line confirmed**. The fix adds a log/metric on this drop path.
`.claude/rules/logging.md` applies: `*Context` variants are **mandatory** where a `ctx` is in
scope (`sloglint` `context: scope` enforces mechanically), `static-msg`, lowercase message,
snake_case keys. **`crypto-reviewer` MUST run before push** —
`internal/eventbus/history/` is in its trigger list per `CLAUDE.md`.
[VERIFIED: `rg -n 'emitter == nil'`]

### D-21 — the three deferred `ec22.9` items

argon2 dummy-hash entropy, `http.Server` write timeout, `addlicense` pin. Research did not
locate live issues for these; CONTEXT.md says *"file them if no live issue exists."*
Planner should include an issue-filing task (same shape as D-11's four).

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Coverage measurement & gating | CI / repo settings | — | `.codecov.yml` (in-repo) + ruleset `11923801` (operator action). Neither is application code. |
| ACE naming ratchet | Test-meta (`test/meta/`) or lint plugin (`gorules/`) | — | A mechanical guard over `_test.go`; owns no runtime behaviour. |
| Session-lifecycle matrix | Integration test tier (`test/integration/session/`) | Harness (`internal/testsupport/integrationtest/`) | Specs live in the suite; only D-15's timestamped emit touches the harness. |
| Timestamped event emit (D-15) | Test harness | — | `internal/testsupport/integrationtest` is a testsupport package; production code MUST NOT import it (depguard). |
| ABAC attribute emission (#4793) | Policy attribute providers (`internal/access/policy/attribute/`) | ABAC evaluator | Providers own the bag's shape; the evaluator's fail-safe semantics depend on omission. |
| Cookie/HSTS/CSP defaults (#4794) | Gateway CLI flag (`cmd/holomush`) | Web middleware (`internal/web`) | The *default* is a CLI/config concern; `internal/web` already implements correctly and only consumes the bool. |
| `sessions.location_id` index (#4796) | Database / migrations | — | Pure schema; `internal/store/migrations/`. |
| Audit-emitter drop observability (#4797) | EventBus history fence | Telemetry/logging | The fence owns the drop decision; logging is the cross-cutting concern it emits into. |

## Project Constraints (from CLAUDE.md)

Directives the planner MUST verify compliance against:

| Constraint | Source | Bearing on Phase 9 |
|------------|--------|--------------------|
| **`main` is protected; no direct commits; squash-merge via PR** | CLAUDE.md §Protected Branch Policy | D-22's single PR on `gsd/v0.12-milestone`. |
| **Tests MUST be written before implementation** (TDD) | §Test-Driven Development | Applies to #4794/#4797 code changes; QUAL-02/04 are test-only. |
| **MUST use `task` for build/test/lint/fmt** — never raw `go test`/`golangci-lint` | §Commands | All verification commands in the plan. |
| **MUST run `task pr-prep` before push** (fast lane); read **exit code**, not stdout strings | §Commands, `.claude/rules/search-tools.md` | Final gate. go-task collapses failures to **201**. |
| **`task fmt` mutates files — commit those edits** | §Commands | The ACE rename sweep touches many files; run `task fmt` after. |
| **`task test` does NOT compile `//go:build integration`** — MUST run `task test:int` on refactors | §Testing, `.claude/rules/testing.md:41` | D-15's harness signature change is a shared-type change → `task test:int` mandatory. |
| **`abac-reviewer` MUST run** for `internal/access/**` | §Pre-Push Review Gates | #4793. |
| **`crypto-reviewer` MUST run** for `internal/eventbus/history/**` | §Pre-Push Review Gates | #4797. |
| **`/gsd-code-review` over the phase's changed files** | §Code review | All plans. |
| **NEVER use `[ci skip]`** on a branch with an open PR | `.claude/rules/landing-the-plane.md` | D-22 single-PR shape makes this live. |
| **SPDX Apache-2.0 header** on new `.go`/`.sh`/`.sql`-adjacent files | §License Headers | New test files, new migration, any new analyzer. |
| **Production code MUST NOT import `eventbustest`/`coretest`/`natstest`/`quarantinetest`** (depguard) | §Testing | D-15's harness work. |
| **`// Verifies: INV-<SCOPE>-N` MUST NOT be fabricated** | `.claude/rules/invariants.md` | If D-13's matrix meta-test rises to a registry invariant. |
| **Terminology: `location` never `room`** | `.claude/rules/terminology.md` | New test names and the matrix table. |
| **Migrations: idempotent, paired up/down, no triggers/functions** | `.claude/rules/database-migrations.md` | #4796. |
| **Logging: `*Context` variants mandatory where `ctx` in scope** | `.claude/rules/logging.md` | #4797. |
| **Sub-agents MUST be briefed on the search ladder** (probe → rg → ast-grep; never bare grep) | `.claude/rules/subagent-briefing.md` | Every executor dispatch. |
| **Worktree isolation** — edits only in `.worktrees/v0.12-foundation-hardening` | §Session isolation | Already satisfied (D-22). |

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Coverage percentage math | A bespoke coverage parser as a gate | `.codecov.yml` + codecov statuses | codecov already applies `ignore:` and merges sessions; a local `go tool cover` number is off by ~24 points (proved above). |
| Registry↔artifact bijection (D-13) | A new walker from scratch | Copy `test/meta/quarantine_registry_test.go:35,76` (`TestQuarantineRegistryBijection` + `filepath.WalkDir`) | Proven in-repo, arch-review-audited as "a genuine structural guard, not decorative". |
| Deterministic event ordering in specs | `time.Sleep` between emits | D-15's timestamped emit | `holomush-ec22.13` already flags ~16 sleep sites; adding a 17th is a documented anti-pattern. |
| "Compile canary" interface-satisfaction tests | `func TestXSatisfiesInterface(_ *testing.T) {}` | `var _ Interface = Type{}` at package scope | ec22.16's own recommendation; zero-assertion by construction, and the compiler is the better prover. |
| Error-code assertions | `strings.Contains(err.Error(), "CODE")` | `errutil.AssertErrorCode` / `assert.ErrorIs` | `.claude/rules/testing.md`; `oops.Code("X").Is(err)` matches ANY oops error (documented trap). |
| Multiple analyzers in one lint plugin | One `register.Plugin` returning N analyzers | One `register.Plugin` per enableable id | golangci-lint v2 constraint; repo already respects it 11:11. |
| ABAC "absent" attribute signalling | `attrs["x"] = ""` | Omit the key; emit `has_x` witness | `.claude/rules/abac-providers.md`; `"" == ""` is a fail-open match. This *is* #4793. |
| Concurrent-index migration | `CREATE INDEX CONCURRENTLY` | `CREATE INDEX IF NOT EXISTS` | No precedent in 52 migrations; `CONCURRENTLY` cannot run in a transaction and would need new runner support (D-19). |

**Key insight:** every one of these has an in-repo precedent that an executor will *not* find
unless the plan cites it by `path:line`. The dominant failure mode in this phase is
reinventing a guard that already exists three directories away.

## Common Pitfalls

### Pitfall 1: Treating `go tool cover` output as the gate's number
**What goes wrong:** A plan sets a coverage target from `task test:cover`'s tail line and
either panics (54.3% vs a 70% target) or over-builds.
**Why it happens:** `go tool cover -func` applies **no** `.codecov.yml` `ignore:` and merges
**no** integration session. The delta is ~24 points here.
**How to avoid:** Read codecov's own number
(`api.codecov.io/api/v2/github/holomush/repos/holomush/branches/main/`) or reconstruct with
the ignore list applied.
**Warning signs:** Any coverage figure near 54% being called "project coverage".

### Pitfall 2: `rg -r` eats the next flag
**What goes wrong:** `rg -rn 'pat' .` silently treats `n` as a **replacement string**
(`-r` is `--replace`), mangling output into partial matches. I hit this live this session:
`rg -rn 'Skip\("TODO' .` returned `n(holomush-nko7): telnet + web…` — the leading
`t.Skip("TODO` was *replaced* with `n`.
**How to avoid:** `rg -n`. rg is already recursive. Also: `rg 'A\|B'` matches a **literal
pipe** — alternation is bare `|`.
**Warning signs:** Match text that starts mid-token.

### Pitfall 3: Searching for `t.Skip` in Ginkgo files
**What goes wrong:** D-11's four files are found by `t.Skip` in ec22.16's description but
use Ginkgo's bare `Skip(...)` inside an `It(...)` closure. A grep for `t.Skip` returns zero.
**How to avoid:** `rg -n 'Skip\(' test/integration/eventbus_e2e/`.
**Warning signs:** A task that says "trim the four files" finding none.

### Pitfall 4: Renaming `TestINV_*` in the ACE sweep
**What goes wrong:** 25 invariant-binding tests encode the registry id in the underscores.
Renaming breaks the id↔test readability the registry and its meta-tests depend on.
**How to avoid:** Carve out `^TestINV_` in any predicate. Also carve out `TestPrivacy_*`
if dqd1's spec-§8 name-matching meta-test is in play.
**Warning signs:** A rename diff touching `test/meta/` fixtures or `invariants.yaml`.

### Pitfall 5: Patch-coverage failure on a tiny security diff
**What goes wrong:** #4794/#4797 are ~10-line diffs. `codecov/patch` wants 75%+ (target 80,
threshold 5). One uncovered multi-line `slog.ErrorContext(...)` branch can fail it.
**How to avoid:** Write the error-branch test alongside the fix, not after. Carried forward
from Phase 6.
**Warning signs:** A plan that adds a log line with no accompanying assertion.

### Pitfall 6: Reading a codecov status before both uploads land
**What goes wrong:** `.codecov.yml:20-21` (`after_n_builds: 2`, `wait_for_ci: true`) mean
the status is absent — not failing — until unit and integration/e2e both upload.
**How to avoid:** Judge by the final rollup after CI completes.
**Warning signs:** "codecov/project is missing, the gate is broken."

### Pitfall 7: Planning 48 matrix cells
**What goes wrong:** 10 of the 48 positions are `n/a` in izk0's own table; planning them
invents specs for transitions that do not exist (e.g. tmux-style telnet reattach on the web
columns).
**How to avoid:** Use the verbatim table above; 38 populated cells.

### Pitfall 8: `task fmt` drift after a large rename sweep
**What goes wrong:** The ACE sweep touches many files; `task fmt` reflows and adds SPDX
headers, and uncommitted `fmt` output is the documented top cause of red CI on an
otherwise-green PR.
**How to avoid:** `task fmt` then commit its edits, before `task pr-prep`.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `testify` (unit); **Ginkgo/Gomega** (integration, `//go:build integration`); Playwright (E2E) |
| Config file | `Taskfile.yaml:150` (`test:cover`), `:165` (`test:int`); `.golangci.yaml`; `.codecov.yml` |
| Quick run command | `task test -- ./<package>/` |
| Full suite command | `task test:cover` then `task test:int`; final gate `task pr-prep` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| QUAL-02 | `cmd/holomush` ≥80% | coverage | `task test:cover` + codecov `project` status on the PR | ✅ (task exists) |
| QUAL-02 | `internal/tls` ≥80% | coverage | `task test:cover` | ✅ |
| QUAL-02 | `codecov/patch` + `codecov/project` are **required** checks | ratchet (operator) | `gh api repos/holomush/holomush/rulesets/11923801 --jq '[.rules[]\|select(.type=="required_status_checks")\|.parameters.required_status_checks[].context]'` must contain both | ✅ (verified runnable) |
| QUAL-02 | `threshold: 0%` landed as final plan | config | `rg -n 'threshold: 0%' .codecov.yml` | ✅ |
| QUAL-03 | ACE predicate returns zero hits | ratchet | `task test -- -run TestACENamingRegistry ./test/meta/` (or `task lint`) | ❌ Wave 0 |
| QUAL-03 | Four skip files trimmed; each cites an **open** issue | ratchet | extend the D-13/quarantine-style walker, or `task quarantine:audit`-shaped check | ❌ Wave 0 |
| QUAL-04 | Suite runs ≥15 specs | integration | `task test:int -- ./test/integration/session/...` | ✅ (suite bootstrap exists; specs ❌) |
| QUAL-04 | Every matrix cell maps to a real spec or a cited pointer | ratchet (bijection) | `task test -- -run TestSessionMatrixRegistry ./test/meta/` | ❌ Wave 0 |
| QUAL-04 | `TestPrivacy_ReattachWithinTTLPreservesFloor` passes | integration | `task test:int -- ./test/integration/session/...` | ❌ Wave 0 |
| QUAL-04 | `TestPrivacy_TTLExpiryEndsSessionFreshFloor` passes | integration | same | ❌ Wave 0 |
| QUAL-04 | #4682 I-PRIV-6 floor arm passes | integration | `task test:int -- ./test/integration/privacy/...` | ❌ Wave 0 |
| QUAL-04 | D-15 timestamped emit exists | unit + integration | `rg -n 'func \(s \*Session\) EmitDirectEventAt\|WithEmitAt' internal/testsupport/integrationtest/` + `task test:int` | ❌ Wave 0 |
| QUAL-05 | #4793 — zero `attrs[...] = ""` in `attribute/` | unit + ratchet | `task test -- ./internal/access/policy/attribute/` and a test asserting the key is **absent** (per `.claude/rules/abac-providers.md`) | ⚠ partial (package has tests; absence assertions ❌) |
| QUAL-05 | #4794 — secure defaults ON | unit | `task test -- ./cmd/holomush/ ./internal/web/` — must **update** `cmd/holomush/gateway_test.go:543-545` which pins `false` | ✅ (test exists, must invert) |
| QUAL-05 | #4796 — index exists and migration reverses | integration | `task test:int` (runs migrations on a fresh DB); plus up/down round-trip | ✅ |
| QUAL-05 | #4797 — drop path logs/metrics | unit | `task test -- ./internal/eventbus/history/` with a nil-emitter case asserting the log/metric | ⚠ partial |
| QUAL-05 | #4792 deferral recorded | manual-only | `gh issue view 4792 -R holomush/holomush --comments` shows the deferral rationale | n/a |

### Sampling Rate

- **Per task commit:** `task test -- ./<changed-package>/` (seconds) + `task lint`.
- **Per wave merge:** `task test:cover` (~80s measured) and, for any wave touching shared
  types or the harness, `task test:int` (~141s measured, needs Docker).
- **Phase gate:** `task pr-prep` green inline in the parent, then `/gsd-verify-work`.
  Read the **exit code** — go-task collapses failures to 201.

### Wave 0 Gaps

- [ ] `test/meta/ace_naming_registry_test.go` — the productionized ACE predicate (covers QUAL-03). **Prerequisite for D-08/D-10.**
- [ ] `test/meta/session_matrix_registry_test.go` — D-13's matrix↔spec bijection (covers QUAL-04). Model: `test/meta/quarantine_registry_test.go:35,76`.
- [ ] `.planning`-external committed matrix table (D-13) — the artifact the meta-test reads.
- [ ] D-15 timestamped emit in `internal/testsupport/integrationtest/session.go` — **blocks** the two dqd1 specs and #4682.
- [ ] Four GitHub issues for `holomush-{ecbg,6nds,nko7,l4kx}` — **blocks** D-11's trim (no existing issues; verified).
- [ ] Three GitHub issues for D-21's deferred ec22.9 residue.
- [ ] Absence-assertion test helper for #4793 (assert key **not present**, not `== ""`).
- No framework install needed — Ginkgo, testify, gotestsum all present and green.

## Security Domain

`security_enforcement` is not disabled; this section applies. Two of QUAL-05's four items are
`label: security`/`abac` findings from an adversarial architecture review.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no (indirect) | QUAL-04 exercises session lifecycle but adds no auth logic |
| V3 Session Management | **yes** | `Secure` + `SameSite=Strict` cookie attributes (#4794, `internal/web/cookie.go:45-56`); session TTL/reaper behaviour asserted by the QUAL-04 matrix |
| V4 Access Control | **yes** | ABAC default-deny; attribute **omission** (not sentinel) so missing attrs evaluate `false` — ADR `holomush-iv43` + `holomush-ti1b` (#4793) |
| V5 Input Validation | no | No new external input surface in this phase |
| V6 Cryptography | **yes (read-only)** | #4797 touches the plugin-decrypt audit fence; **do not** change crypto behaviour — add observability only. `crypto-reviewer` gate |
| V8 Data Protection | **yes** | HSTS + CSP defaults (#4794, `internal/web/security_headers.go:74-82`) |
| V14 Configuration | **yes** | Fail-safe-by-default flag inversion (D-18) — the core of #4794 |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Session cookie sent over plaintext behind a TLS-terminating proxy | Information Disclosure | `Secure` + `SameSite=Strict` **on by default**; opt out only for local dev (D-18) |
| Session fixation / cookie theft absent HSTS | Spoofing / Info Disclosure | `Strict-Transport-Security: max-age=31536000; includeSubDomains` (`security_headers.go:25`) on by default |
| XSS via missing CSP | Tampering | CSP header half on by default (`security_headers.go:82`) |
| ABAC fail-open via empty-string attribute matching (`"" == ""`) | **Elevation of Privilege** | Omit the key; emit `has_X` witness on **every** code path (#4793) — missing attrs evaluate `false` per ADR `holomush-iv43` |
| Audit record silently dropped → unattributable decrypt | Repudiation | Log + metric on the `f.emitter == nil` path (#4797, `plugin_downgrade_fence.go:423`); **fail-closed is out of scope** — observability only |
| Unindexed presence query → resource exhaustion under load | Denial of Service | `idx_sessions_location` (#4796) |
| Coverage gate posted-but-not-required → regressions merge silently | (assurance) | Add `codecov/patch`+`codecov/project` to ruleset `11923801` (D-04) |

> **Both domain gates fire this phase.** `abac-reviewer` for #4793
> (`internal/access/**`); `crypto-reviewer` for #4797
> (`internal/eventbus/history/`). Per `CLAUDE.md` both are **MUST**-run before push and are
> read-only (`permissionMode: plan`).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | codecov's 78.28% includes an e2e session I did not run locally; the 82.25% vs 78.28% delta is line-vs-statement + partials accounting | GATE 1 | Low — both figures are far above 70%; the verdict is insensitive to the reconciliation |
| A2 | The risk weights (3.0/2.0/1.5/1.0) in the D-01 table are my construction | GATE 1 | Low — CONTEXT.md assigns weighting to Claude's discretion; raw uncovered counts are given so the planner can re-weight |
| A3 | The "single-token tail" heuristic (114 hits) is a reasonable proxy for "no expectation clause" | GATE 2 | Medium — a hand-read of the 114 would refine it; the *count magnitude* (~100, not ~1000) is robust |
| A4 | `codecov/project` is absent from PR rollups because there is no base to compare, not because it is misconfigured | GATE 1 / D-04 | Low — `.codecov.yml:25-37` defines it; ruleset absence is independently verified |
| A5 | Deleting well-covered code is the realistic `project`-status failure mode, not refactors | D-04 hazard | Low — no Phase 9 plan deletes large covered blocks |
| A6 | The `_test.go` exclusion (`.codecov.yml:57`) makes the ACE sweep produce an empty codecov patch | D-04 sequencing | Low — verified in the ignore list; worst case the sweep's patch status is `n/a` rather than passing |

## Open Questions

1. **Does `.codecov.yml`'s existing ignore of `cmd/holomush/core.go` + `sub_grpc.go` satisfy
   `holomush-0yo6`'s `cmd/holomush (incl. runCore()) ≥80%` acceptance criterion?**
   - What we know: both are ignored (`.codecov.yml:69,74`) with inline rationale, landed
     pre-phase. `holomush-0yo6` itself says *"confirm with owner whether the ignore satisfies
     the original acceptance criterion."* Measured `cmd/holomush` **with** them excluded is
     64.8%.
   - What's unclear: whether D-03's "do not ignore to move a number" is meant to *reverse*
     these pre-existing ignores (which would make the gap much larger) or only to forbid
     *new* ones.
   - Recommendation: read D-03 as forward-looking (no new ignores) and accept the existing
     two, since both predate the phase and carry documented rationale. **Confirm with the
     user before planning** — it is the difference between a ~986-line and a ~1,642-line gap.

2. **Which GATE 1 re-scope option does the user want?** Re-point (recommended), raise to a
   real ratchet at 78.28%, or keep 70% as a no-op. This is a scope decision research cannot
   make.

3. **Which GATE 2 re-scope option?** Tightened predicate (~114, recommended), literal D-07
   with D-08 dropped (~1,106), or literal + allowlist (rejected by D-08). D-09 explicitly
   routes this to an "explicit re-scope conversation."

4. **`--secure-cookies` flag shape after inversion (D-18).** Flip the default to `true` and
   keep the name (operators passing `--secure-cookies` are unaffected; those relying on the
   default change behaviour), or introduce `--insecure-cookies`/`--dev-mode` and deprecate.
   The release note's content depends on this.
   - Recommendation: flip the default and keep the name; add an explicit
     `--secure-cookies=false` path for dev. Smallest operator-facing surface.

5. **Does D-13's matrix meta-test rise to a registry invariant?**
   `.claude/rules/invariants.md` says register a guarantee when violating it is *a regression
   in a guarantee* rather than *a missing feature*. The quarantine bijection (its model) is
   **not** registered. Recommendation: follow the precedent — plain meta-test, no registry
   entry — unless the planner wants an `INV-SESSION-N`.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | all | ✓ | go1.26.5 darwin/arm64 | — |
| Docker | `task test:int` (testcontainers: Postgres) | ✓ | daemon responding | — |
| `gotestsum` | `task test`/`test:cover`/`test:int` | ✓ | on PATH | `go tool` fallback in Taskfile |
| `gh` CLI (authenticated) | issue verification, ruleset read, D-11 issue filing | ✓ | authenticated against `holomush/holomush` | — |
| codecov API (public, unauthenticated) | GATE 1 authoritative number | ✓ | api.codecov.io v2 | local profile reconstruction (done; agrees) |
| `task` | every verification command | ✓ | go-task | — |
| `./bin/custom-gcl` | `task lint:go` if the ACE predicate becomes a lint plugin | not probed | — | `test/meta/` walker (recommended anyway) |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** none material.

## Sources

### Primary (HIGH confidence)
- Live tool runs in this session: `task test:cover` (exit 0, 10366 tests), `task test:int`
  (exit 0, 10786 tests), `go tool cover -func`, `gh api rulesets/11923801`,
  `gh issue view` ×6, `gh issue list --search` ×4, custom `go/ast` predicate over 6,294 test
  functions, custom coverprofile parser applying `.codecov.yml` ignores.
- codecov API v2 — `/branches/main/` and `/report/?branch=main` (607 files, per-package
  totals).
- `.planning/archive/beads/2026-07-09-beads-live.jsonl` — `holomush-izk0`, `holomush-0yo6`,
  `holomush-dqd1`, `holomush-hdnx`, `holomush-ec22.15`, `holomush-ec22.16` full descriptions.
- Repo files cited inline by `path:line` throughout.

### Secondary (MEDIUM confidence)
- `docs/reviews/arch-review/2026-07-11/findings/d9a-testing-ci.md:57` — the zero-assertion
  sweep and the 11:11 `register.Plugin` confirmation.
- `.planning/phases/09-test-quality-code-health-sweep/09-CONTEXT.md` — decisions; premises
  independently re-verified, four falsified.

### Tertiary (LOW confidence)
- None. No claim in this document rests on WebSearch or unverified training knowledge.
  No external packages are recommended, so the Package Legitimacy Audit is not applicable
  (this phase installs nothing).

## Metadata

**Confidence breakdown:**
- GATE 1 coverage sizing: **HIGH** — three independent measurements, two agreeing on the
  verdict, plus the authoritative gate-side API.
- GATE 2 ACE sizing: **HIGH** for the counts (deterministic AST walk); **MEDIUM** for the
  114-hit tightened figure (heuristic classification, stated as A3).
- QUAL-04 harness/matrix: **HIGH** — every helper verified at its cited line; matrix
  recovered verbatim.
- QUAL-05 sites: **HIGH** — all issue states and all `path:line` citations verified live.
- D-11 issue mapping: **HIGH** — four `gh issue list --search` runs, all negative.

**Research date:** 2026-07-25
**Valid until:** 2026-08-24 (30 days). Coverage figures move with every merge to `main`; the
GATE 1 verdict has ~8 points of headroom and is unlikely to invert, but re-read the codecov
API number if planning slips past the milestone.
