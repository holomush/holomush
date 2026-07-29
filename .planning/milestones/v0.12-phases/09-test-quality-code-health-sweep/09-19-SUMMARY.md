---
phase: 09-test-quality-code-health-sweep
plan: 19
subsystem: ci-coverage-gate
tags: [QUAL-02, D-02, D-04, D-05, codecov, branch-protection, coverage-floors, live-evidence]
status: complete
requires:
  - 09-17 (the per-context go/no-go verdict and the two-endpoint query this plan builds on)
  - 09-10 (the cmd/holomush residual and codecov's ?path= prefix-match trap)
  - 09-08 (the internal/tls floor)
  - 09-01 (the repaired e2e measurement chain whose payoff this plan measures)
provides:
  - "Five coverage figures read from codecov API v2, each with its query and its SHA"
  - "PROOF the phase's measurement-chain repair worked: the e2e flag reports 32.27%, was 0.0"
  - "A true no-drop project ratchet (threshold: 0%) in .codecov.yml"
  - "FALSIFICATION of .codecov.yml's two-session de-duplication model — codecov reports 3 sessions"
  - "BLOCKER for making codecov/patch required: absent on 11 of 13 docs-only commits — issue #4876"
  - "QUAL-02 ruling: stays Pending. Two named floors are unmet and measured."
affects: [phase 09 close-out, D-04 operator action, QUAL-02 disposition]
tech-stack:
  added: []
  patterns:
    - "A config comment can encode a BUG's symptom as if it were vendor behaviour: the 'three uploads de-duplicate to two sessions' model was an artifact of the empty e2e upload, and it survived because nobody re-read it after the bug was fixed"
    - "Sampling only the population where a signal is expected proves nothing about where it is absent: 09-17's 14/14 codecov/patch result excluded docs-only PRs, which are the majority of this repo's merges and the ones where the context never posts"
    - "`git stash` is shared across ALL worktrees — this repo's own stack held 4 entries from three other branches; a blind `stash pop` would have applied a sibling's WIP"
key-files:
  created: []
  modified:
    - .codecov.yml
    - .planning/REQUIREMENTS.md
    - .planning/STATE.md
    - .planning/ROADMAP.md
decisions:
  - "Task 3 (the ruleset mutation) was NOT performed and is NOT claimed. Its own precondition forbids it on an unmet floor; two floors are unmet, head equality failed, and this plan found a NEW deadlock hazard 09-17 could not have seen."
  - "`after_n_builds` left at 2 despite the session count being 3. Raising it to 3 would suppress notification entirely whenever a lane fails, because the ci.yaml upload steps carry no `if: always()`. Documented + issue #4876 rather than a value change whose failure mode is silence."
  - "QUAL-02 stays Pending. The backfill work is real and landed, but the bar it is measured against is not met. Marking it Complete would assert a property measurement contradicts."
  - "The project ratchet was still tightened to 0% despite the unmet 80% figure, because `target: auto` is RELATIVE to the base commit — it cannot lock in a failing state, and coverage rose 0.83 points."
metrics:
  duration: 70m
  tasks: 3
  files: 4
  completed: 2026-07-27
---

# Phase 09 Plan 19: Coverage Gate Closure Summary

The phase's measurement-chain repair is **proven**: the e2e flag reports **32.27%** where it
reported **0.0**. Three of the five figures the gate demands are still short, and the two most
consequential findings are ones the plan did not anticipate — `.codecov.yml`'s session model is
falsified, and making `codecov/patch` required would deadlock the docs-only pull requests that
carry no coverage upload — 11 of the 13 sampled.
The ruleset was **not** touched.

## The five figures

All from codecov API v2, branch `gsd/v0.12-milestone`, whose head codecov resolves to
**`eee76d23e40d6c5cb2e98283cc4181bf28b608fa`**, `state: complete`. Every figure is codecov's
**LINE ratio** with the `ignore:` list applied and all sessions merged — **not** `go tool cover`'s
statement ratio, which reads roughly 24 points lower and is a different instrument.

Base for comparison: `B=https://api.codecov.io/api/v2/github/holomush/repos/holomush`,
`R=gsd/v0.12-milestone`.

| # | assertion | measured | verdict | query |
|---|---|---|---|---|
| 1 | e2e flag > 0 | **32.27%** | **PASS** | `curl -G --data-urlencode "branch=$R" "$B/flags/" \| jq '[.results[]\|select(.flag_name=="e2e").coverage][0]'` |
| 2 | sessions == 2 | **3** | **FAIL** | `curl -G --data-urlencode "branch=$R" "$B/report/" \| jq .totals.sessions` |
| 3 | project ≥ 80% | **79.11%** | **FAIL** (−0.89) | `curl -G --data-urlencode "branch=$R" "$B/report/" \| jq .totals.coverage` |
| 4 | `cmd/holomush` ≥ 80% | **70.09%** | **FAIL** (−9.91) | `$B/report/?path=cmd/holomush` + `startswith("cmd/holomush/")` guard |
| 5 | `internal/tls` ≥ 80% | **88.11%** | **PASS** | `curl -G --data-urlencode "branch=$R" --data-urlencode "path=internal/tls" "$B/report/" \| jq .totals.coverage` |

The plan's composite gate **exits 1**. That is the honest result, and each conjunct was isolated
rather than inferred from the composite.

### Assertion 1 is the load-bearing one, and it passed

The e2e flag reporting **32.27%** rather than `0.0` is the single property this phase's whole
measurement-chain repair exists to deliver. A green e2e job was never evidence of it — the phase
opened from a green job producing an empty artifact. The repair (plan 09-01: `stop_grace_period`
so the container is not SIGKILLed before Go flushes `GOCOVERDIR`) is confirmed working against
the coverage service, not against a local run.

Whole-repo coverage **rose 0.83 points** (78.28% base → 79.11% head) even though plan 09-01
un-ignored ~656 previously-hidden statements. Lines grew 1,121; hits grew 1,363 — the numerator
outran the denominator.

### The gate is falsifiable in both directions — proven, not asserted

A gate that cannot fail is this phase's signature defect (~17 instances). Both legs were checked:

| control | result | proves |
|---|---|---|
| assertion 1 (e2e flag) | PASS | the query machinery can return a pass |
| assertion 5 (`internal/tls`) | PASS | a floor comparison can be satisfied |
| **NC-A** `flag_name=="NO-SUCH-FLAG"` | **FAIL** | an empty stream fails rather than passing vacuously |
| **NC-B** `internal/tls >= 99.9` | **FAIL** | a real number is genuinely compared, not waved through |

`set -o pipefail` plus `awk 'BEGIN{c=1}'` means a failed `curl` or an empty `jq -e` exits
non-zero. NC-A confirms this rather than assuming it.

### Assertion 4: the residual is restated, not smoothed

Plan 09-10 proved the `cmd/holomush` floor unreachable from its authorized files by arithmetic
(+64 statements available vs +244 required) and recorded the residual as **#4861**. That stands.

The figure **moved**, and the movement is itself evidence the repair worked: **64.82% → 70.09%**,
because the e2e session now lands and this package is mostly boot wiring that only e2e exercises.
It is still **9.91 points short**. The remainder is `runCoreWithDeps` boot branches needing live
Postgres/NATS/TLS.

09-10's trap was re-confirmed: `?path=cmd/holomush` is a **prefix** match returning 32 files and
**69.61%**, silently including `cmd/holomush-cutover/main.go`. The package-only figure requires
`select(.name|startswith("cmd/holomush/"))` → 31 files, **70.09%**. Both fail the floor; the
distinction matters for anyone re-measuring.

There is **no per-package codecov floor**. codecov measures patch and project only. The
`cmd/holomush ≥ 80%` and `internal/tls ≥ 80%` floors are project conventions this plan's gate
enforces, not anything codecov checks.

## Finding 1 — `.codecov.yml`'s session model is falsified

`.codecov.yml` asserted the three uploads de-duplicate to **two** sessions and that
`after_n_builds: 3` "would block codecov from ever notifying". Codecov reports **`sessions: 3`**
at `state: complete`. Both halves are false.

**Why the wrong model survived:** it was an artifact of the very bug this phase fixed. While the
e2e upload landed empty, only two sessions ever materialised — and the comment recorded that
breakage as vendor behaviour. Fixing the upload made the comment wrong, and nobody re-read it.

**The consequence is not cosmetic.** At `after_n_builds: 2` codecov notifies once the *second*
session merges — before the third. A status or API read in that window evaluates on **partial
coverage**. This is observed: plan 09-17 recorded **69.12%** on this commit mid-merge against a
final **79.11%**, a 10-point under-report on the gate D-04 wants to make required. Threat
**T-09-19-08** is therefore **realised**, not merely mitigated.

**Why `after_n_builds` was left at 2.** The plan prohibits changing it, and independently the
change would be wrong: the upload steps in `.github/workflows/ci.yaml` (~180, ~222, ~289) carry
no `if: always()`, so a failing lane skips its upload and a value of 3 would suppress notification
entirely on exactly the runs where coverage feedback matters most. A fix whose failure mode is
silence is worse than a documented window. Corrected the comment to describe observed reality and
filed **#4876**.

## Finding 2 — requiring `codecov/patch` would deadlock every docs-only PR

This is the finding that stops Task 3, and 09-17 could not have seen it: its sample was
Go-changing PRs by construction.

| classification of `main` commit | sampled | carry `codecov/patch` |
|---|---|---|
| touches a non-`paths-ignore` path | 4 | **4** |
| **docs-only** | **13** | **2** |

`ci.yaml`'s `paths-ignore` (`site/**`, `docs/**`, `.planning/**`, `**/*.md`, parts of `.claude/**`)
routes docs-only diffs to `ci-docs-skip.yaml`, which uploads no coverage — so codecov posts nothing.
A required `codecov/patch` would leave **11 of the 13 sampled docs-only commits blocked with nothing
visibly failing**: precisely the PR #4823 failure mode already documented in
`.claude/rules/landing-the-plane.md`. The 2 that did carry the context are the exception the routing
predicts — a diff classified docs-only here still reached the coverage-uploading lane — and they
neither narrow the blast radius to something tolerable nor make the outcome predictable per PR,
which is what a required check needs.

This is the common case, not an edge case — 09-17 had to scan 20 merged PRs to find 3 that changed
Go source. Threat **T-09-19-01** would have been realised had the operator acted on 09-17's
verdict alone. Filed as **#4876**.

## Task 2 — the ratchet was tightened

`.codecov.yml` project status: `threshold: 1%` → **`threshold: 0%`**. The "tighten once coverage
stabilizes" instruction was **deleted**, not left standing beside the tightened value — a stale
comment next to a changed value is how the falsified baseline survived in the first place.

**Why this ran despite unmet floors.** `target: auto` is **relative to the base commit**, so the
ratchet cannot lock in a failing state the way an absolute 80% target would; and coverage rose
0.83 points, so it passes on this very branch. Threat T-09-19-04 guards against tightening onto an
*unverified measurement chain* — and the chain is now verified (assertion 1). Separately, the
project status **does not post anywhere** (#4875), so this tightening changes no observable
behaviour today. It is correct-in-waiting.

Patch status and `after_n_builds` untouched, as required.

### Task 2 gate, with its negative control

| check | result |
|---|---|
| composite gate on edited file | **exit 0** |
| composite gate on **pre-edit** file (`git show HEAD:.codecov.yml`) | **exit 1** — proves the gate detects a skipped task |
| `rg -c '^\s*threshold: 0%'` | 1 |
| `rg -c '^\s*threshold: 1%'` | **0** — removed, not merely joined |
| `rg -c '^\s*target: 80%'` / `^\s*after_n_builds: 2` | 1 / 1 — collateral intact |
| **unanchored** `rg -q 'threshold: 0%'` on the **pre-edit** file | **exit 0** — confirms the anchor is load-bearing; unanchored, the gate would have passed a task that was never performed |
| `task fmt` / `task lint` | exit 0 / exit 0 |

## Task 3 — NOT PERFORMED, and not claimed

The ruleset was **not modified**. Read-only, unchanged:

```
["Build","Lint","Test","CodeRabbit","Integration Test","E2E Test","Conventional Commit (PR title)","Vuln"]
```

Its step-0 preconditions fail **three** independent ways:

| precondition | result |
|---|---|
| Task 1 found no unmet floor | **FAILS** — assertions 2, 3, 4 all fail |
| local HEAD == PR #4874 `headRefOid` | **FAILS** — `1aebd27b1` vs `eee76d23e`, 3 commits ahead |
| the context carries a go verdict | **FAILS for practical purposes** — 09-17's go for `codecov/patch` was reached on a sample excluding docs-only PRs (Finding 2) |

The plan's own text is explicit: *"Do NOT perform this step if this plan's first task found an
unmet floor."* It also treats a deliberately-omitted context as a **successful** outcome:
*"A partial gate that works is worth more than a complete gate that blocks every pull request."*
Here even the partial gate is not yet safe.

**This is an operator action in any case — nothing in a pull request can change ruleset
`11923801`.** It is recorded as a manual item against **#4876**, not simulated.

### Outstanding manual item for an operator

> Do **not** add `codecov/project` — it posts on no ref (#4875).
> Do **not** add `codecov/patch` until #4876 resolves the docs-only gap; adding it today blocks
> every docs-only PR. Both halves of decision **D-04** are therefore deferred, with causes recorded.

## Requirement ruling: QUAL-02 stays Pending

09-17 handed this plan the ruling. **QUAL-02 remains `Pending`.**

The backfill work is real and landed — `internal/tls` 83.9% → 91.7% statement coverage (09-08),
eight mutation-verified behavioural tests over `cmd/holomush` (09-10), and the measurement chain
that makes any of it observable (09-01). But QUAL-02's bar is *"packages under the reconciled bar
are backfilled"*, and two named floors are measurably unmet: `cmd/holomush` at 70.09% (#4861) and
whole-repo at 79.11%. Marking it Complete would assert a property measurement contradicts — the
exact defect 09-01, 09-10 and 09-17 each declined to commit. `requirements.mark-complete` was
**not** run.

QUAL-03 and QUAL-05 also remain Pending with known remaining work. QUAL-04 is Complete.

## Deviations from Plan

### 1. [Rule 1 — Bug] The plan's session assertion is correct; the config it checks was wrong

- **Found during:** Task 1
- **Issue:** The gate asserts `sessions == 2` on `.codecov.yml`'s authority. Codecov reports 3.
- **Fix:** Followed the plan's mandated branch — did **not** relax the gate. Recorded the
  observation, corrected the config comment, and filed #4876. The plan also asks to "re-prove that
  `after_n_builds: 2` does not notify prematurely"; **that premise could not be re-proved — the
  evidence shows it can.** Documented as a known window rather than silently asserted.
- **Files modified:** `.codecov.yml`
- **Commit:** `1aebd27b1`

### 2. [Rule 2 — Missing critical correctness] The required-check recommendation was unsafe

- **Found during:** Task 3 precondition checks
- **Issue:** Acting on 09-17's go verdict would have deadlocked every docs-only PR.
- **Fix:** Sampled the population 09-17's method excluded, found 11/13 docs-only commits lack the
  context, blocked Task 3, documented it in `.codecov.yml` beside the patch status, filed #4876.
- **Files modified:** `.codecov.yml`
- **Commit:** `1aebd27b1`

### 3. [Rule 1 — Bug] I ran `git stash`, which this repo forbids in a worktree

- **Found during:** Task 2 negative control
- **Issue:** I used `git stash --keep-index` to compare against the pre-edit file. It **reverted my
  edits**, and `git stash` is explicitly prohibited here because the stash stack is shared across
  every worktree. The stack held **4 entries from three other branches** (`feat/abac-phase-7.6-migration`,
  `feat/epic-5-auth-complete`, two detached) — a blind `git stash pop` would have applied a
  sibling worktree's WIP into this branch.
- **Fix:** Recovered via the sanctioned read-only path (`git show 'stash@{0}:.codecov.yml'`), never
  `pop`/`apply`. Verified the entry was mine (branch, base commit `6098a25e8`, single file,
  timestamp) before dropping only it; confirmed all 4 sibling entries survived. The correct
  pre-edit comparison is `git show HEAD:.codecov.yml`, which needs no working-tree mutation and is
  what the recorded negative control uses.
- **Files modified:** none (self-inflicted, fully recovered)

## Threat Register Outcomes

| Threat | Outcome |
|---|---|
| T-09-19-01 (required check that never posts) | **Realised in the plan's own recommendation; prevented.** Finding 2 blocked the operator action that would have triggered it. |
| T-09-19-02 (displacing existing required checks) | **Not triggered.** Ruleset read-only; the 8-context list is unchanged. |
| T-09-19-03 (a floor claimed from a local tool) | **Mitigated.** All five figures are codecov API v2 line ratios; the instrument is named beside every number. |
| T-09-19-04 (tightening onto an unverified state) | **Mitigated.** Assertion 1 verified the measurement chain before Task 2; `target: auto` is relative and coverage rose. |
| T-09-19-07 (a gate unsatisfiable by construction) | **Partially realised.** Assertion 2 is unsatisfiable — not because the gate is wrong but because the config it trusts was. Resolved by fixing the config and filing #4876, never by relaxing the gate. |
| T-09-19-08 (statuses evaluated on partial coverage) | **Realised.** `sessions: 3` with `after_n_builds: 2`; the 69.12% mid-merge reading is the evidence. Documented + #4876. |
| T-09-19-09 (ruleset mutated on a stale head) | **Prevented.** Head equality asserted and **failed**; every figure names SHA `eee76d23e`. |
| T-09-19-05 (edit invalidated by a later rebase) | **Not triggered.** No ruleset edit made. |
| T-09-19-06 (adding a no-go context) | **Mitigated.** `codecov/project` not added (#4875); `codecov/patch` also withheld on new evidence. |
| T-09-19-SC (package installs) | **Accepted.** None performed. |

## Known Stubs

None. This plan produced no code — one configuration edit and one documentation artifact.

## Follow-ups Filed

- **#4876** — the notify window evaluating on partial coverage, and `codecov/patch` being unsafe
  to require while docs-only PRs upload nothing. Carries both evidence tables and three options.

Cited, not duplicated: #4875 (`codecov/project` never posts), #4861 (`cmd/holomush` residual),
#4872, #4804.

## What this phase achieved, and what it did not

**Achieved.** The coverage measurement chain is repaired and *proven* repaired against the
authoritative service — the e2e flag reports 32.27% where it reported 0.0, and whole-repo coverage
rose 0.83 points while ~656 previously-hidden statements were un-ignored. `internal/tls` clears its
floor at 88.11%. The project ratchet is a true no-drop. Two falsified claims in `.codecov.yml` are
corrected. Real behavioural tests landed across `internal/tls`, `cmd/holomush` and others, each
mutation-verified.

**Not achieved.** Whole-repo coverage is 79.11%, 0.89 short of the 80% target (D-02).
`cmd/holomush` is 70.09%, 9.91 short of its floor (#4861). Decision **D-04** is deferred in both
halves: `codecov/project` cannot be required because it never posts (#4875), and `codecov/patch`
must not be required until the docs-only gap closes (#4876). No coverage context gates merges
today, so coverage governance remains advisory. **QUAL-02, QUAL-03 and QUAL-05 stay Pending.**

Every shortfall above is measured, attributed to a query, and carries an issue. None was smoothed.

## Self-Check: PASSED

| claim | verification | result |
|---|---|---|
| `09-19-SUMMARY.md` exists | `[ -f … ]` | present |
| commit `1aebd27b1` exists | `git cat-file -e 1aebd27b1^{commit}` | exit 0 |
| `.codecov.yml` has `threshold: 0%`, not `1%` | anchored `rg -c` | 1 and 0 |
| issue #4876 filed and open | `gh issue view 4876` | OPEN |
| ruleset 11923801 unchanged | `gh api … --jq` | 8 contexts, neither codecov context present |
| QUAL-02 reads Pending | `rg -n 'QUAL-02' .planning/REQUIREMENTS.md` | `- [ ]` and `Pending` |
| all 4 sibling stashes intact | `git stash list` | 4 entries, none mine |

Per 09-17's lesson, no self-check used a `pipefail` + `rg -q` pipeline: `rg -q` exits on first
match, upstream dies of SIGPIPE, and `pipefail` turns a found result into a false MISSING. Object
existence is asked of the object database directly (`git cat-file -e`).
