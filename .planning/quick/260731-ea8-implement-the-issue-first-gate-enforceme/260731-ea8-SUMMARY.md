---
phase: quick-260731-ea8
plan: 01
subsystem: ci
tags: [ci, github-actions, governance, contributing, bats]
status: complete
requires: []
provides:
  - "Issue Gate check (GitHub Actions job, not yet a required ruleset context)"
  - "scripts/pr-gate-paths.sh three-valued path-exemption matcher"
  - "Taskfile.yaml vars.REPO_CONFIG_ONLY_PATHS"
affects:
  - Taskfile.yaml
  - .github/workflows/scripts-tests.yaml
  - CONTRIBUTING.md
  - .github/PULL_REQUEST_TEMPLATE.md
  - .github/PULL_REQUEST_TEMPLATE/
tech-stack:
  added: []
  patterns:
    - "git :(glob) pathspec as the glob engine — no hand-written glob compiler"
    - "three-valued exit contract (0 exempt / 10 not exempt / 1 error)"
    - "GraphQL closingIssuesReferences instead of a Closes #N regex"
    - "pull_request_target with base-only checkout and no paths filter"
key-files:
  created:
    - scripts/pr-gate-paths.sh
    - scripts/tests/pr-gate-paths.bats
    - .github/workflows/issue-gate.yaml
  modified:
    - Taskfile.yaml
    - .github/workflows/scripts-tests.yaml
    - CONTRIBUTING.md
    - .github/PULL_REQUEST_TEMPLATE.md
    - .github/PULL_REQUEST_TEMPLATE/feature.md
    - .github/PULL_REQUEST_TEMPLATE/enhancement.md
    - .github/PULL_REQUEST_TEMPLATE/chore.md
    - .github/PULL_REQUEST_TEMPLATE/fix.md
decisions:
  - "Exemption reports GREEN from the same job — no paths filter, no ci-docs-skip stand-in, no job-level if"
  - "The ruleset context is NOT added in this change; it is an ordered post-merge step tracked by GH #4895"
  - "All twelve human-closure sentences were amended, never deleted or inverted"
metrics:
  duration: ~50 min
  completed: 2026-07-31
actuals:
  tokens: 21000
  tasks: 3
  commits: 3
---

# Quick Task 260731-ea8: Issue-First Gate Enforcement Summary

A `pull_request_target` workflow named `Issue Gate` now labels, comments on, and fails PRs
that are neither path-exempt nor linked to a gate-labelled `holomush/holomush` issue —
closing the gap where the `gate-violation` label existed and nothing applied it (GH #4890).

## Commits

| Commit | Task | Message |
| --- | --- | --- |
| `a3695519c` | 1 | `feat(ci): add REPO_CONFIG_ONLY_PATHS and the pr-gate path matcher` |
| `10156dde6` | 2 | `feat(ci): enforce the issue-first gate with a required Issue Gate check` |
| `e03301ae1` | 3 | `docs(contributing): state the Issue Gate consequence alongside the human one` |

No `.planning/` artifact was committed, per instruction. `STATE.md` and `ROADMAP.md` were
deliberately left untouched.

## The three demonstrated-RED observations

Every gate below was watched failing against a known-violating input before it was
trusted. Exit codes are the actual numbers observed, not a paraphrase.

### RED 1 — bats suite before `scripts/pr-gate-paths.sh` existed

`bats scripts/tests/pr-gate-paths.bats` → **exit 1**, 15 of 15 cases `not ok`, every case
reporting bats warning `BW01: ... exited with code 127, indicating 'Command not found'`.

```
1..15
not ok 1 docs-only diff is exempt
not ok 2 dependency-only diff is exempt, including root go.mod and go.tool*.mod
not ok 3 repo-config diff under .github is exempt
not ok 4 .github/CODEOWNERS alone is never exempt
not ok 5 .github/CODEOWNERS hidden among exempt .github files is still not exempt
not ok 6 root CODEOWNERS alone is never exempt
not ok 7 docs/CODEOWNERS is exempt — the carve-out is path-exact, not a name match
not ok 8 Taskfile.yaml is never exempt
not ok 9 scripts/foo.sh is never exempt
not ok 10 a listed lockfile shape under scripts/ is exempt
not ok 11 an unlisted lockfile under scripts/ is not exempt
not ok 12 paths containing spaces do not break parsing
not ok 13 empty changed-file list is a deliberate exempt verdict
not ok 14 a Taskfile missing REPO_CONFIG_ONLY_PATHS exits 1, not 0 and not 10
not ok 15 a mixed diff is not exempt and names the offending file
BATS_EXIT=1
```

**A second RED, unplanned and load-bearing.** On the first run *with* the script present,
14 of 15 passed and **case 14 failed** — the fail-loud guard. First implementation put the
missing-var check inside a `require_var` helper consumed as `< <(require_var "$VAR")`;
`exit 1` there terminates only the process-substitution subshell, so the script continued
with an empty pattern list and returned a verdict instead of erroring. Observed exit was
**10**, expected **1**. Fixed by moving the emptiness check into the main shell before the
pathspec array is built. That is precisely the silent degrade the case exists to catch, and
it was live in the first draft.

Final: `bats scripts/tests/pr-gate-paths.bats` → **exit 0**, 15/15 `ok`.

### RED 2 — Task 3 wording gate before the edits

Acceptance gate run pre-edit → **exit 1**, listing exactly **ELEVEN** missing
(file, literal) pairs. Not twelve — `CONTRIBUTING.md` already carried `gate-violation` at
`:215` in its label table, so only its `Issue Gate` literal was missing.

```
MISSING:
.github/PULL_REQUEST_TEMPLATE.md:gate-violation
.github/PULL_REQUEST_TEMPLATE.md:Issue-Gate
.github/PULL_REQUEST_TEMPLATE/feature.md:gate-violation
.github/PULL_REQUEST_TEMPLATE/feature.md:Issue-Gate
.github/PULL_REQUEST_TEMPLATE/enhancement.md:gate-violation
.github/PULL_REQUEST_TEMPLATE/enhancement.md:Issue-Gate
.github/PULL_REQUEST_TEMPLATE/chore.md:gate-violation
.github/PULL_REQUEST_TEMPLATE/chore.md:Issue-Gate
.github/PULL_REQUEST_TEMPLATE/fix.md:gate-violation
.github/PULL_REQUEST_TEMPLATE/fix.md:Issue-Gate
CONTRIBUTING.md:Issue-Gate
PRE_EDIT_GATE_EXIT=1
```

Post-edit the same gate → **exit 0**, `SITES=12`, MISSING LITERALS 0.

The two metrics stayed distinct and both landed on their settled values:
**MISSING LITERALS PRE-EDIT = 11**, **SITES TO AMEND = 12**. The 12-site sweep was
independently re-measured pre-edit and returned 12
(`PULL_REQUEST_TEMPLATE.md` 1, `feature` 2, `enhancement` 2, `chore` 1, `fix` 1,
`CONTRIBUTING` 5 — including `:86` `they close it` and `:142` `are closed`).

### RED 3 — live PR #4874 through the gate's full decision logic

The workflow cannot execute on its own PR (GitHub sources a `pull_request_target` workflow
from the default branch), so the decision logic was mirrored in a throwaway local script
— same matcher invocation, same truncation guard, same GraphQL query and jq predicate —
and run against all four live fixtures. Actual exits:

| PR | files | path verdict | linked issue | outcome | dry-run exit |
| --- | --- | --- | --- | --- | --- |
| #4885 | 4 | EXEMPT | — | GREEN | **0** |
| #4889 | 13 | EXEMPT | — | GREEN | **0** |
| #4894 | 12 | NOT EXEMPT | `holomush/holomush#4892 [CLOSED]` labels incl. `confirmed-bug` | GREEN | **0** |
| #4874 | 174 | NOT EXEMPT | `holomush/holomush#576 [CLOSED]` labels `type::bug, pr-review, pr-review-finding, pr:139, turn:1, aspect:security, priority::high, severity:important` — **no gate label** | **RED** (apply `gate-violation` + comment) | **1** |

**#4874 came out RED.** The gate fires. Truncation guard: declared `changed_files` matched
the paginated `/files` count on every fixture (174 = 174 on #4874).

## Verification actually run

| Check | Exit |
| --- | --- |
| `bats scripts/tests/pr-gate-paths.bats` (15 cases) | 0 |
| Task 1 automated verify — 5 synthetic + 4 live-PR verdicts | 0 |
| `task lint:actions` (actionlint over all workflows) | 0 |
| Task 2 structural verify (yq job name, no path filter, trigger set, no job-level `if`, never closes, never touches PR head, unswallowed label call, scripts-tests wiring) | 0 |
| Task 3 acceptance gate post-edit | 0 |
| `task test:bats` (full suite, 83 tests) | 0 |
| `task lint` (full umbrella) | 0 |
| `task fmt` | 0, working tree clean afterwards |
| `task pr-prep` (fast lane) | 0; result file `status=pass lane=fast exit=0` |

Exit codes were read directly; no verdict was inferred by grepping stdout.

## What is done vs pending

### Done

- `Issue Gate` job exists, `name: Issue Gate` byte-exact, no `paths`/`paths-ignore`, no
  job-level `if`, draft short-circuit at the step level so a draft still gets a green
  conclusion.
- Violating PR gets `gate-violation`, a marker-keyed upserted comment, and `exit 1`.
  A passing PR has a stale label removed. **No PR is ever closed by automation** —
  asserted by the workflow verify (`pr close|--state closed` must not appear).
- `.github/CODEOWNERS` and root `CODEOWNERS` are non-exempt, pinned by the mixed-diff bats
  case whose input the exclusion query alone returns EMPTY for.
- No glob compiler was written; git's own `:(glob)` engine matches.
- All twelve human-closure sentences survive and each now states the automated
  consequence. No auto-close wording exists anywhere.

### PENDING — POST-MERGE (do not tick)

**The required-status-check half is NOT done.** `Issue Gate` is not a context on ruleset
`11923801` and must not be added until after this merges. Reason is structural, not
procedural: GitHub sources a `pull_request_target` workflow file from the repository's
default branch, so the workflow cannot run on its own PR; requiring the context now leaves
it permanently `Pending` and blocks every open PR with nothing failing to point at (#4878).

Tracked by **GH #4895**, which carries the ordered steps (merge → observe a real non-Pending
conclusion on the next unrelated PR → only then add the context) and the copy-pasteable
snapshot-edit-PUT command. The ruleset was read (to confirm the eight existing contexts and
`integration_id=15368`) and **not mutated**.

## Deviations from plan

1. **`require_var` helper removed** (Task 1). The plan specified an `extract_var` function
   plus a per-call emptiness check; the natural helper shape put `exit 1` inside a process
   substitution, where it cannot terminate the script. Restructured so the check runs in the
   main shell. Rule 1 (bug), caught by the plan's own bats case.
2. **Path-traversal guard added** to `pr-gate-paths.sh` (Task 1). PR-author-influenced
   filenames are materialized as filesystem paths; an absolute path or a `..` component now
   exits 1 rather than writing outside the scratch dir. GitHub cannot produce either, so
   this fires only when something is already wrong. Rule 2 (missing critical functionality
   at a trust boundary).
3. **`# shellcheck disable=SC2016`** added to the GraphQL step (Task 2). actionlint runs
   shellcheck over `run:` bodies and flagged the single-quoted `$owner`/`$name`/`$number`
   GraphQL variables as non-expanding — which is the whole point of that quoting. Line-scoped
   directive with rationale; no lint config was widened.
4. **`gh api --jq` used instead of `grep`/`rg` for label and comment lookups** in the
   workflow. `rg` is not present on a `ubuntu-latest` runner and bare `grep` is banned by
   repo rule, so the checks are done in jq.
5. **Comment file list capped at 21 lines**, with an overflow note pointing at the check log.
   #4874 has 174 changed files; an uncapped list would post an unreadable comment.
6. **Workflow header cross-references #4895.** Added after the issue was filed so the
   permanently-Pending trap is documented at the point of temptation.

## Could not verify

- **The workflow has never executed on GitHub.** It cannot, until it is on `main`. Every
  claim about its behaviour rests on (a) the decision logic dry-run above, which exercised
  the real matcher, the real truncation guard, the real GraphQL query and the real jq
  predicate against live PR data, and (b) actionlint + structural assertions on the YAML.
  Not exercised end-to-end: `actions/checkout` on the base ref, `$RUNNER_TEMP` plumbing,
  `gh pr edit --add-label`/`--remove-label`, the comment upsert PATCH/POST, and the draft
  short-circuit. Step 2 of #4895 exists precisely to observe those before anything depends
  on them.
- **The `CODEOWNERS` carve-out has no live fixture.** No `CODEOWNERS` file exists at either
  location in the repo today, so the carve-out is covered by synthetic bats cases only
  (including the silent-trap mixed diff). That is the strongest coverage available.
- **`repository.nameWithOwner` cross-repo filter has no live fixture.** No PR in the repo
  links a foreign-repo issue. The filter is asserted by reading the jq predicate, not by
  observing a rejection.

## Known stubs

None.

## Threat flags

None beyond the plan's register. T-ea8-01/02/03/05/06 mitigations are all present and
asserted; T-ea8-08 (ruleset added before merge) is mitigated by not doing it and by #4895.

## Self-Check: PASSED

All three created files exist on disk; `scripts/pr-gate-paths.sh` is executable; all three
commit hashes resolve in `git log`; `vars.REPO_CONFIG_ONLY_PATHS` is present in
`Taskfile.yaml`; GH issue #4895 exists and is OPEN.

---

## Post-execution: verification and code review (appended by the orchestrator)

Two read-only gates ran after execution. Both found real defects; all were fixed
before push. Commits `f31d8ab30`, `e6464335b`, `ddb99158f`.

### Verification — `human_needed`, 4/7 must-haves

Three must-haves assert GitHub Actions **runtime** behaviour that a
`pull_request_target` workflow cannot exercise on its own PR, because GitHub
sources the workflow file from the default branch. Platform property, not a
defect. #4895 step 2 is the gate for them.

Its substantive finding: `permissions:` omitted `issues: read` while the
linked-issue step reads Issue nodes. Dangerous rather than obviously broken —
this repo is public, so the same read succeeds unauthenticated, and a refusal
would not have surfaced as a permissions error. Measured failure shape: jq exits
5 on the null response, `set -euo pipefail` aborts the step, the implicit
`success()` skips the verdict step, and the job goes RED with no label and no
comment. Fail-closed, but unattributable — the one outcome this workflow's own
design notes say it must never produce.

### Code review — 2 BLOCKER, 11 WARNING, every one reproduced

**CR-01 (fixed, `e6464335b`) — `docs/CODEOWNERS` defeated the carve-out.**
GitHub honors CODEOWNERS at THREE locations: root, `.github/`, and `docs/`. The
carve-out covered two; `docs/**` swallowed the third. A PR adding
`docs/CODEOWNERS` reassigning `internal/**` passed GREEN.

Worth recording *how it survived*: the issue comment that set the contract named
two locations, CONTEXT.md locked those two, research proposed the broader
`**/CODEOWNERS`, the planner correctly narrowed it to the lock, the plan-checker
blessed the narrowing as a correct read of the lock, a bats case pinned the hole
as intended behaviour, and the orchestrator manually verified
`docs/CODEOWNERS → 0` and called it correct. Six checkpoints, all validating
against the specification, none against GitHub. **A correct implementation of a
wrong specification passes every test derived from that specification.**

**CR-02 (fixed, `ddb99158f`) — the truncation guard could not fire.**
`[ "$declared" -ne "$collected" ]` exits 2 on a non-integer; as an `if`
*condition* `set -e` is suspended, so 2 read as false. Fail-open inside the guard
written to prevent fail-open.

**Warnings fixed:** blank line inside a `|` block scalar silently truncating the
pattern list (WR-01); bare `..` reaching mkdir and refused only by the
filesystem (WR-05); `dirname` without `--` leaking `illegal option -- e` into a
public comment (WR-05); filenames escaping the comment's backtick fence to render
markdown under the `github-actions[bot]` identity (WR-02).

### Warnings NOT fixed — deliberate, recorded rather than silently dropped

| ID | Finding | Why not fixed |
| --- | --- | --- |
| WR-09 | `pr-gate-paths.bats` "root CODEOWNERS alone is never exempt" passes with the `owners` query entirely stubbed out — a vacuous assertion. | Root `CODEOWNERS` matches no exempt pattern, so query 1 already returns it as non-exempt; the carve-out entry for it is defence-in-depth against a future exempt pattern that would match it. The case cannot be made load-bearing without first introducing the hole it guards. The `.github/CODEOWNERS` and new `docs/CODEOWNERS` mixed-diff cases ARE real pins. |
| WR-10 | `.github/**` exempts the gate's own workflow, so a PR weakening `issue-gate.yaml` is exempt from the gate. | Pre-existing prose policy set by #4889 and deliberately reaffirmed there; changing it is a policy decision for the maintainer, not an implementation choice. Load-bearing as of this commit in a way it was not before — the exemption now covers the enforcement mechanism itself. Worth a follow-up issue. |

### What remains unobserved

The workflow has never executed on GitHub and cannot until it merges. The dry run
exercised the real matcher, truncation guard, GraphQL query and jq predicate
against live API data, but `actions/checkout`, `gh pr edit --add/--remove-label`,
the comment upsert, and the draft short-circuit are unobserved. The CODEOWNERS
carve-out and the cross-repo `nameWithOwner` filter have no live fixtures — they
rest on bats cases and code reading respectively.

**#4895 step 2 must be observed on a NON-EXEMPT PR.** The GraphQL step only runs
when `exempt == false`, so a green on a docs-only PR would confirm nothing about
the enforcing half. Recorded as a comment on #4895.
