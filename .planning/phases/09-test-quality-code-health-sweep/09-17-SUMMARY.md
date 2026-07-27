---
phase: 09-test-quality-code-health-sweep
plan: 17
subsystem: ci-coverage-gate
tags: [QUAL-02, D-04, codecov, branch-protection, live-evidence]
status: complete
requires:
  - 09-21 (pushed the branch and opened PR #4874 — the only live rollup this phase has)
  - 09-01 (repaired the E2E coverage chain, so the head commit carries a real 3-session report)
provides:
  - "GO verdict for `codecov/patch` as a required check ON GO-CHANGING PULL REQUESTS ONLY, grounded in 14/14 live head-commit observations — the sample excludes docs-only pull requests by construction, so it is not evidence that the context is globally safe to require. Plan 09-19 sampled the excluded population and found it absent on 11 of 13 docs-only commits, deferring the requirement — issue #4876"
  - "NO-GO verdict for `codecov/project`, grounded in 64 observations across two endpoints and three months — issue #4875"
  - "The corrected two-endpoint verdict command 09-19 must use; the single-endpoint form in this plan's own gate reports a FALSE NEGATIVE for codecov/patch"
  - "SHA eee76d23e40d6c5cb2e98283cc4181bf28b608fa — the head every rollup here was observed against"
affects: [09-19 coverage gate closure and ruleset operator action]
tech-stack:
  added: []
  patterns:
    - "GitHub reports check results through TWO endpoints — /commits/{sha}/status and /commits/{sha}/check-runs — and a context present in one is absent from the other; querying one and reading the zero as absence is the phase's signature defect wearing a new hat"
    - "A vendor's own API is the second source that falsifies a repo-side hypothesis: codecov's base_totals/head_totals refuted 'no base to compare against' in one call"
    - "A 'never posts' claim needs a positive control of EACH kind the query can return — CodeRabbit (a status) and Lint (a check run) prove both legs live before a 0 is trusted"
key-files:
  created: []
  modified:
    - .planning/REQUIREMENTS.md
decisions:
  - "`.codecov.yml` NOT edited. Task 2 mandates the narrowest change that makes the context post; diagnosis established the cause is out-of-repo (account-plan), so every available in-repo edit would have been unverifiable motion on the file whose miswiring this plan exists to prevent"
  - "The plan's Task 2 gate was run VERBATIM and its output transcribed VERBATIM, then corrected alongside rather than edited — the prohibition forbids reaching a verdict by editing the gate, and the correction adds a second source rather than relaxing the first"
  - "QUAL-02 restored to Pending, matching 09-01's recorded decision and 09-10's independent reasoning; 09-08's mark-complete was a protocol side-effect, not a judgment"
  - "Filed a NEW issue (#4875) rather than commenting only on #4872 — #4872 tracks the project status's OUTCOME, this is the prior fact that it has no outcome; #4872 was commented and gated on it"
  - "Recorded the Team-plan cause as a HYPOTHESIS with its vendor quote, not as a finding — the org's codecov plan needs an authenticated session this executor does not have"
metrics:
  duration: 55m
  tasks: 2
  files: 1
  completed: 2026-07-27
---

# Phase 09 Plan 17: Live Verification of the Coverage Status Contexts Summary

`codecov/patch` posts on the pull-request head commit and is safe to require;
`codecov/project` has never posted anywhere in this repository and requiring it would
deadlock every pull request. Both verdicts rest on two independent GitHub endpoints, and the
central discovery is that this plan's own premise — that coverage statuses are commit
statuses — is false, which made its own gate report `codecov/patch` as absent.

## The finding that inverts the plan

The plan's Task 1 action instructs: *"Use the commit status API, not the checks list: coverage
statuses are commit statuses rather than check runs."* **That premise is false for this
repository.** On a pull-request head commit, codecov posts a **check run**; on a `main` push it
posts a **commit status**. The two live at different endpoints.

Observed on PR #4874 head `eee76d23e40d6c5cb2e98283cc4181bf28b608fa`:

```
$ gh api repos/holomush/holomush/commits/eee76d23e.../status --jq '.total_count, .statuses[].context'
1
CodeRabbit

$ gh api repos/holomush/holomush/commits/eee76d23e.../check-runs --paginate \
    --jq '.check_runs[] | select(.app.id==254) | "\(.name) = \(.conclusion)"'
codecov/patch = success
```

The consequence is direct: the planner's central finding — *"the patch context appeared on merge
commits for two of three sampled pull requests and on no head commit"* — is an **artifact of
querying one endpoint**. The patch context appears on **every** head commit examined, which is
exactly the ref a required check is evaluated against. Threat `T-09-17-05` (requiring a
merge-commit-only context would deadlock every pull request) is therefore **refuted**, not
mitigated.

## Evidence table

Every row was collected with both endpoints. `check` = `/commits/{sha}/check-runs`,
`status` = `/commits/{sha}/status`. Sample selection: **20 merged pull requests had to be
scanned to find three that changed Go source** — the first 17 by recency were documentation and
dependency bumps, confirming the planner's warning. All 13 Go-changing PRs inside a 60-PR window
were then examined rather than stopping at three.

### Merged Go-changing pull requests — HEAD commits (the ref a required check reads)

| PR | head commit | `codecov/patch` | `codecov/project` |
|---|---|---|---|
| #4832 | `0554b2c27` | **check** | — (neither) |
| #4825 | `365abb080` | **check** | — |
| #4819 | `9493b3806` | **check** | — |
| #4816 | `6dec47af2` | **check** | — |
| #4814 | `16d5bf033` | **check** | — |
| #4813 | `54ce04ced` | **check** | — |
| #4782 | `403e93b23` | **check** | — |
| #4595 | `2822c081e` | **check** | — |
| #4586 | `3f69c13a9` | **check** | — |
| #4585 | `526e58359` | **check** | — |
| #4575 | `d852da7c1` | **check** | — |
| #4574 | `b25ef85d4` | **check** | — |
| #4571 | `c9c096d0c` | **check** | — |

**13/13 head commits carry `codecov/patch` as a check run. 0/13 carry it as a commit status.
0/13 carry `codecov/project` on either endpoint.**

### The same pull requests — MERGE commits (squashed onto `main`)

| PR | merge commit | `codecov/patch` | `codecov/project` |
|---|---|---|---|
| #4832 | `497748c6d` | **status** | — |
| #4825 | `cce89c702` | — (neither) | — |
| #4819 | `f063b8045` | **status** | — |
| #4816 | `7ff05af3c` | **status** | — |
| #4813 | `02b8ce146` | **status** | — |
| #4782 | `30d55a162` | **status** | — |

**5/6 merge commits carry `codecov/patch` as a commit status. 0/6 carry it as a check run.
0/6 carry `codecov/project` on either endpoint.** The reporting mode is exactly inverted
between the two refs.

### `main` history — has `codecov/project` ever posted?

Twelve first-parent `main` commits sampled at even intervals across 2026-04-26 → 2026-07-24,
both endpoints each:

| commit | date | `codecov/patch` | `codecov/project` |
|---|---|---|---|
| `728c20684` | 2026-07-24 | — | — |
| `9b2ea903b` | 2026-07-03 | — | — |
| `d2c443747` | 2026-06-22 | status | — |
| `c4cdcfa9c` | 2026-06-14 | status | — |
| `58388fab4` | 2026-06-04 | status | — |
| `a4acb2b3b` | 2026-05-29 | status | — |
| `aeb05598a` | 2026-05-25 | — | — |
| `33293bb2d` | 2026-05-23 | status | — |
| `dd446136a` | 2026-05-19 | status | — |
| `dc2b331f8` | 2026-05-19 | status | — |
| `f9263a620` | 2026-05-14 | status | — |
| `72bbd4fa8` | 2026-04-26 | — | — |

### This phase's own pull request

**PR #4874, head `eee76d23e40d6c5cb2e98283cc4181bf28b608fa`, equal to the local `HEAD` at
observation time** (asserted by the gate, and by hand: `git rev-parse HEAD` matched
`gh pr view 4874 --json headRefOid`).

**Continuous integration HAD completed when queried.** `gh pr view --json statusCheckRollup`
returns `{"SUCCESS": 24}` with zero pending; `gh pr checks 4874` lists 19 rows, all `pass`;
the codecov check run's own `completed_at` is `2026-07-27T16:14:24Z`. This is a final rollup,
not an early poll, so an absence here means "absent", not "not yet".

| context | `/status` | `/check-runs` | conclusion |
|---|---|---|---|
| `codecov/patch` | absent | **present** | `success` |
| `codecov/project` | **absent** | **absent** | never posted |
| `CodeRabbit` | present | absent | positive control for the status leg |
| `Lint` | absent | present ×2 | positive control for the check leg |

Codecov's own check **suite** on this commit reports `latest_check_runs_count: 1` — codecov
produced exactly one check, and it was `codecov/patch`.

**Total: 64 observations (32 commits × 2 endpoints). `codecov/project` appears in zero of
them.** It is not "did not appear yet". It has never been observed posting in this repository.

## Verdict

Of the four outcomes the plan enumerates, the fourth holds for one context and the first for
the other — a split the plan's list did not anticipate:

- **`codecov/patch` — GO for the sampled population only.** Exact string an operator enters:
  `codecov/patch`. Observed present and `success` on 14/14 pull-request head commits, including
  this phase's own. Every one of those 14 changed Go source — docs-only pull requests were
  excluded by this plan's own sample-selection filter — so this verdict is silent about them and
  MUST NOT be read as "safe to require" in general. Plan 09-19 sampled exactly that excluded
  population and found the context absent on 11 of 13 docs-only commits, because `paths-ignore`
  routes those diffs to `ci-docs-skip.yaml`, which uploads no coverage. The requirement is
  therefore deferred rather than confirmed — issue **#4876**.
- **`codecov/project` — NO-GO.** Follow-up issue **#4875**. It posts on no ref, on neither
  endpoint, and has not done so at any point in the repository's observable history.

**SHA handed to plan 09-19: `eee76d23e40d6c5cb2e98283cc4181bf28b608fa`.** 09-19 must re-assert
head equality before the operator acts — the branch will have moved by then, including by this
plan's own commits.

## Why `codecov/project` does not post

Diagnosed from observed evidence before concluding, with each alternative ruled out by a
source that is not the configuration file.

**Ruled out — "a status with no base commit to compare against does not emit."** This is
09-RESEARCH assumption A4 and the plan objective's own stated explanation. Codecov's API
falsifies it:

```
$ curl -s https://api.codecov.io/api/v2/github/holomush/repos/holomush/pulls/4874/
  base_totals.coverage = 78.28    head_totals.coverage = 79.11    ci_passed = true
```

A base exists, a head exists, and `target: auto` + `threshold: 1%` evaluates 79.11 vs 78.28 →
pass. The status still does not post.

**Ruled out — "a status configured as informational reports but does not gate."**
`informational` is not set anywhere in `.codecov.yml`, and codecov's default is `false`. An
informational status also still posts.

**Ruled out — "the upload-count threshold was never reached."** `after_n_builds: 2`;
codecov reports `sessions: 3` for this commit. `codecov/patch`, subject to the identical
top-level `notify` gate, posted.

**Ruled out — the repository configuration is malformed or not the effective one.**

- `.codecov.yml` is **`Valid!`** per codecov's own validator
  (`POST https://codecov.io/validate`), which echoes both `coverage.status.project.default`
  and `coverage.status.patch.default` as parsed.
- Exactly one codecov config file is tracked: `git ls-files | rg -i codecov` → `.codecov.yml`.
- `api.codecov.io/.../repos/holomush/` reports `"yaml": null` — no repo-level UI override.
- The `project:` block has been present since PR #179 and has never been observed posting, so
  this is a longstanding condition rather than a regression introduced by this phase.

**Leading hypothesis, vendor-documented, NOT directly confirmed.** codecov's pricing FAQ
(<https://about.codecov.io/pricing/>) states verbatim:

> **The Team Plan shows Patch ONLY coverage.**
> Project Coverage – Shows the code coverage on each pull request for your entire project.

Patch-everywhere plus project-nowhere plus a valid parsed config matches this exactly. If it
holds, the cause is an **account-plan condition, not a repository configuration condition**, and
no `.codecov.yml` edit can address it — which is why this plan made none. It is recorded as a
hypothesis because the `holomush` org's plan needs an authenticated codecov session to read.
The operator confirmation step and the refutation path are both written into #4875.

## Task 2: no configuration change

Task 1's verdict was not "both contexts post", so Task 2's skip clause did not apply and the
diagnosis above was performed. It ended at the plan's own terminal branch — *"If, after
diagnosis, a context genuinely cannot be made to post on the head commit, that is the answer
and the verdict is no-go for that context. Say so and stop."*

`.codecov.yml` is **unchanged**. Both pinned literals are intact:
`rg -v '^\s*#' .codecov.yml | rg -c 'after_n_builds: 2'` → `1`, and the same filtered form for
`threshold: 1%` → `1`. The upload-count threshold was not raised and the tightening is left to
09-19.

Because no edit was made, the publish-then-observe checkpoint reduced to its stated skip form:
the observation SHA was still asserted equal to the local head, so no stale rollup was
inherited from an earlier plan.

## The gate output, verbatim — and why 09-19 must not use it

The plan's Task 2 gate was run **verbatim**. Its acceptance criteria require the verdict line be
transcribed verbatim, so:

```
sha=eee76d23e40d6c5cb2e98283cc4181bf28b608fa patch=0 project=0
```

**`patch=0` in that line is a false negative.** The gate queries only
`/commits/{sha}/status`; `codecov/patch` lives on the check-runs endpoint for a pull-request
head. Read mechanically, this line would hand 09-19 a NO-GO for a context that demonstrably
posts and passes — omitting the one coverage gate the repository can actually enforce. That is
the phase's recurring "a zero result read as absence" defect, and this instance is the
consequential one, because the plan's own note asserts the false premise as fact.

The gate was **not edited** — the prohibition forbids reaching a verdict by weakening it, and
its verdict is transcribed above unaltered. The correction below **adds a second source**
rather than relaxing the first.

**Corrected command for 09-19.** Both endpoints, whole-line matches, every call fatal:

```bash
set -o pipefail
SHA=$(gh pr view <PR> -R holomush/holomush --json headRefOid --jq .headRefOid) || exit 1
test "$(git rev-parse HEAD)" = "$SHA" || exit 1          # staleness guard, unchanged
gh api "repos/holomush/holomush/commits/$SHA/status"     --jq '.statuses[].context'  >  /tmp/ctx.txt || exit 1
gh api "repos/holomush/holomush/commits/$SHA/check-runs" --paginate --jq '.check_runs[].name' >> /tmp/ctx.txt || exit 1
printf 'sha=%s patch=%s project=%s\n' "$SHA" \
  "$(rg -cx 'codecov/patch'   /tmp/ctx.txt || echo 0)" \
  "$(rg -cx 'codecov/project' /tmp/ctx.txt || echo 0)"
```

Run against `eee76d23e` it yields `patch=1 project=0` — the true state.

**Proven falsifiable in both directions before being trusted**, because a command that only ever
returns zero proves nothing:

| probe | result | what it proves |
|---|---|---|
| `codecov/patch` | 1 | the check-runs leg is live |
| `CodeRabbit` | 1 | the status leg is live (positive control of the OTHER kind) |
| `Lint` | 2 | a check run can be counted more than once — see the caution below |
| `codecov/project` | 0 | the finding |
| `codecov/NEGATIVE-CONTROL` | 0 | a 0 is a real absence, not an always-zero query |

## Ruleset facts 09-19 needs

Read live from `gh api repos/holomush/holomush/rulesets/11923801` (`protect-main`, `active`,
scoped to `refs/heads/main`). Current required checks:

| context | `integration_id` | reported as |
|---|---|---|
| `Build` | 15368 (GitHub Actions) | check run |
| `Lint` | 15368 | check run |
| `Test` | 15368 | check run |
| `Integration Test` | *(none — any app)* | check run |
| `E2E Test` | *(none — any app)* | check run |
| `Conventional Commit (PR title)` | 15368 | check run |
| `Vuln` | 15368 | check run |
| `CodeRabbit` | 347564 | **commit status** |

Neither codecov context is present, confirming 09-RESEARCH.

**This table settles the mechanical question `codecov/patch` raises.** Seven of the eight
currently-required checks are reported as check runs, not commit statuses — the status endpoint
on the head returns `total_count: 1` (`CodeRabbit` alone). So the ruleset demonstrably matches
**both** kinds by name, and requiring `codecov/patch` — a check run — is sound. This is proved
by the repository's own working configuration, not by reasoning about GitHub's documentation.

Codecov's app id is **254** (`app_slug: codecov`); an operator pinning `integration_id` should
use it, or leave it unset as `Integration Test` and `E2E Test` do.

**Caution for 09-19 — duplicate check names.** `Lint`, `Test`, `Build`, `Integration Test` and
`E2E Test` each appear **twice** in the check-runs response, because
`.github/workflows/ci-docs-skip.yaml` defines jobs with the same names as `.github/workflows/ci.yaml`
(the sanctioned same-name skip-workflow pattern). All 24 runs are `SUCCESS` here, so nothing is
masked, but a required name matching two runs is satisfied only when both pass. `codecov/patch`
is unaffected — codecov's suite contains exactly one run.

## The number, and which instrument produced it

Every figure below is codecov's **line ratio** from codecov's API v2 — not `go tool cover`'s
statement ratio, which applies neither the `ignore:` list nor the cross-lane session merge and
reads roughly 24 points lower.

| commit | coverage | lines | hits | sessions | source |
|---|---|---|---|---|---|
| `497748c6d` (`main`, base) | **78.28%** | 57,480 | 44,997 | 2 | `/branches/main/` |
| `eee76d23e` (PR #4874 head) | **79.11%** | 58,601 | 46,360 | 3 | `/commits/{sha}/` |

**Coverage rose 0.83 points.** It did not regress. Lines grew by 1,121 — the ~656 statements
plan 09-01 un-ignored (`cmd/holomush/core.go`, `sub_grpc.go`) plus this phase's new tests — and
hits grew by 1,363, i.e. faster than the denominator. The project ratchet (`target: auto`,
`threshold: 1%`) would pass comfortably, if it posted.

**A 69.12% reading was recorded during this plan's briefing and is a mid-merge artifact.** The
commit now reports `sessions: 3`; an earlier read caught it before all three upload sessions had
merged. This is the same hazard `.codecov.yml`'s own comment and 09-RESEARCH Pitfall 6 describe
for status checks, and it applies to the API figure too: **a codecov commit reading is only
final once `state: complete` and the expected session count are both observed.** Both were
checked before the numbers above were recorded.

## Deviations from Plan

### 1. [Rule 1 — Bug] The plan's stated endpoint premise is false; its gate under-reports

- **Found during:** Task 1
- **Issue:** The plan asserts coverage statuses are commit statuses, and both gates query only
  `/commits/{sha}/status`. On pull-request head commits codecov posts a check run, so the gate
  records `patch=0` for a context that is present and passing.
- **Fix:** Gate run verbatim and its output transcribed verbatim as the acceptance criteria
  require; a corrected two-endpoint command, with positive controls of each kind and a negative
  control, is supplied above for 09-19. The gate itself was not edited.
- **Files modified:** none
- **Commit:** evidence-only

### 2. [Rule 2 — Missing critical correctness] QUAL-02 asserted Complete over a documented gap

- **Found during:** Task 2, while reconciling this plan's `requirements: [QUAL-02]`
- **Issue:** `.planning/REQUIREMENTS.md` marked QUAL-02 `Complete`. 09-01 had deliberately
  reverted an identical `requirements.mark-complete` flip and recorded that QUAL-02 stays
  Pending for the last QUAL-02 plan to close; 09-08 (`ed92e20b8`) re-flipped it as a protocol
  side-effect; 09-10 then reasoned independently that it stays Pending. Two artifacts disagreed
  with the table, and the table asserted the property.
- **Fix:** Restored to Pending (checkbox and traceability row), citing 09-01's decision, the
  ~15-point `cmd/holomush` shortfall (#4861), and this plan's `codecov/project` no-go (#4875).
  **This plan did NOT run `requirements.mark-complete`** — it does not complete QUAL-02, and
  running the protocol step would have re-created the defect it just removed. 09-19 is the last
  QUAL-02 plan and owns the ruling.
- **Files modified:** `.planning/REQUIREMENTS.md`
- **Commit:** `b39619974`

### 3. [Rule 3 — Blocking] `09-21-SUMMARY.md` does not exist

- **Found during:** Task 1 `read_first`
- **Issue:** The plan directs the executor to take the phase pull request's number and head
  commit from `.planning/phases/09-test-quality-code-health-sweep/09-21-SUMMARY.md`. That file
  is absent from the phase directory although 09-21's push and pull request did land.
- **Fix:** Resolved both from the live API instead —
  `gh pr list --head gsd/v0.12-milestone --json number,headRefOid` → #4874 / `eee76d23e…`. This
  is strictly stronger than the recorded value: it cannot be stale, and it was cross-checked
  against `git rev-parse HEAD`. No blocker remains; recorded so 09-19 does not repeat the
  lookup and so the missing artifact is visible to the phase audit.
- **Files modified:** none

## Threat Register Outcomes

| Threat | Outcome |
|---|---|
| T-09-17-01 (requiring a status that never posts) | **Mitigated for the sampled population only.** `codecov/project` is a recorded NO-GO with issue #4875; only `codecov/patch`, observed on 14/14 heads, is offered to the operator — but all 14 changed Go source, so the offer does not cover docs-only pull requests. 09-19 found the gap there and withdrew the offer (#4876). |
| T-09-17-02 (configuration read as evidence of behaviour) | **Mitigated.** Every claim cites an observed rollup or a vendor API response; the one configuration-derived claim (the YAML is valid) is sourced to codecov's validator, not to reading the file. |
| T-09-17-03 (raising the upload-count threshold) | **Not triggered.** `.codecov.yml` unchanged; both pinned literals verified intact under the comment-filtered form. |
| T-09-17-04 (this plan's own gates passing without observing a status) | **Partially realised.** Both gates queried the status API as designed and passed — but the Task 2 gate still recorded a false `patch=0`, because the endpoint the plan chose is the wrong one. The gate was hardened against every failure mode except a wrong instrument. |
| T-09-17-05 (requiring a merge-commit-only context) | **Refuted, not merely mitigated.** The merge-commit-only observation was an artifact of the single endpoint; `codecov/patch` is present on every head commit examined. |
| T-09-17-06 (verdict recorded against a stale head) | **Mitigated.** Head equality asserted by the gate and by hand at observation time; every rollup in this SUMMARY names its SHA. |
| T-09-17-SC (package installs) | **Accepted.** None performed. |

## Known Stubs

None. This plan produced no code.

## Follow-ups Filed

- **#4875** — `codecov/project` status never posts; the project half of D-04 is unachievable.
  Carries the full evidence table, the falsification of the "no base" explanation, the
  vendor-quoted Team-plan hypothesis, and the operator confirmation/refutation steps.
- **#4872** — commented and gated on #4875. Its action ("watch the codecov `project` status on
  the first few post-merge runs") is unperformable while the status never posts.

## For plan 09-19

1. Require **`codecov/patch`** only. Exact string: `codecov/patch`. Codecov app id `254`.
2. Do **not** require `codecov/project` — record it as deferred against **#4875**. A partial
   gate that works is worth more than a complete gate that deadlocks the repository.
3. Re-assert head equality with the corrected two-endpoint command before the operator acts.
   The verdict SHA here is `eee76d23e40d6c5cb2e98283cc4181bf28b608fa` and the branch has since
   moved.
4. The ruleset mutation is an **operator action** — nothing in a pull request can change ruleset
   `11923801`. It was not attempted and is not claimed.
5. QUAL-02 is `Pending` and 09-19 owns the ruling. Two gaps stand against Complete: #4861
   (`cmd/holomush` ~15 points under its floor) and #4875 (the project gate).
6. Tightening `threshold: 1%` toward `0%` is 09-19's job and was deliberately left alone here.
   Note that the project status the threshold governs does not currently post, so the tightening
   changes no observable behaviour until #4875 is resolved.

## Self-Check: PASSED

| claim | verification | result |
|---|---|---|
| `09-17-SUMMARY.md` exists | `[ -f … ]` | present, 340 lines |
| commit `b39619974` exists | `git cat-file -e b39619974^{commit}` | `b3961997453af166eeb3d4f649d99075208c7047` |
| QUAL-02 reads `Pending` in both places | `rg -n 'QUAL-02' .planning/REQUIREMENTS.md` | line 46 `- [ ]`, line 97 `Pending` |
| `.codecov.yml` unmodified | `git status --porcelain -- .codecov.yml` | empty |
| issue #4875 filed and open | `gh issue view 4875` | `OPEN` |

**One self-check falsely reported MISSING and is worth recording**, because it is the same
defect class as the plan's own gate. The first form of the commit check was
`git log --oneline --all | rg -q 'b39619974' && echo FOUND || echo MISSING` under
`set -o pipefail`. `rg -q` exits on first match, `git log` then dies of `SIGPIPE`, `pipefail`
propagates that non-zero status, and the `&&` short-circuits to `MISSING` — for a commit that
was plainly in the output. Re-run with `git cat-file -e`, which asks the object database
directly, it passes. A negative result from a pipeline says nothing until you have established
the pipeline can return a positive one.
