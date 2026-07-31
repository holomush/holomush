---
phase: quick-260731-ea8
verified: 2026-07-31T20:15:00Z
status: human_needed
score: 4/7 must-haves verified
behavior_unverified: 3
overrides_applied: 0
behavior_unverified_items:
  - truth: "A PR whose diff leaves the exempt path sets AND lacks a linked holomush/holomush issue carrying a gate label gets the `gate-violation` label, an explanatory comment, and a RED `Issue Gate` check."
    test: "After merge to main, open a non-exempt PR with no gate-labelled linked issue and watch the Issue Gate run."
    expected: "gate-violation applied; one marker-keyed comment posted; the job concludes failure. Then add the gate label to the linked issue, re-trigger via an edit, and confirm the label is REMOVED and the check goes green."
    why_human: "Structural — GitHub sources a pull_request_target workflow from the DEFAULT BRANCH, so this workflow cannot execute on its own PR. The label/comment/upsert API calls have never run. Only the decision LOGIC (matcher + truncation guard + GraphQL predicate) was exercisable pre-merge, and it was — see RED 3."
  - truth: "A PR whose diff is confined to the exempt path sets gets a GREEN `Issue Gate` check from the same job — not a skipped job, not a path-filtered workflow."
    test: "After merge, open a docs-only PR and confirm the Issue Gate context reports a real green conclusion (not Pending, not Skipped)."
    expected: "One check run named exactly `Issue Gate`, conclusion success, attached to the PR head SHA."
    why_human: "The absence of paths/paths-ignore and of a job-level `if` is statically verified, but 'the context is actually emitted and attaches to the PR head SHA' is a GitHub Actions runtime property no static check can prove."
  - truth: "A draft PR gets a GREEN check without evaluating the gate."
    test: "After merge, open a draft PR touching non-exempt files; confirm Issue Gate is green and the log shows only the Skip drafts step. Then mark it ready_for_review and confirm the gate evaluates."
    expected: "Draft: green, gate not evaluated. ready_for_review: gate evaluates and reports the real verdict."
    why_human: "Step-level short-circuit is present and the job carries no job-level `if`, but the draft==true / draft!=true expression evaluation and the resulting job conclusion have never executed."
human_verification:
  - test: "Decide whether `.github/workflows/issue-gate.yaml` needs `issues: read` in its `permissions:` block."
    expected: "The Linked-issue verdict step's GraphQL query reads Issue nodes (closingIssuesReferences -> nodes -> labels). The workflow declares `permissions: {contents: read, pull-requests: write}`; GitHub sets every UNDECLARED scope to `none` once a permissions block is present, so `issues` is `none`. If reading issue labels needs `issues: read`, the step fails closed in the WORST shape: `gh api graphql` errors (or returns `data.repository = null`, on which the step's own jq exits 5 — measured), `set -euo pipefail` aborts the step, the job goes RED with NO label and NO comment. That is precisely the unattributable-failure shape the design's own comments say it must avoid (#4878). Adding `issues: read` is a one-line, strictly-safe change and costs nothing if it turns out to be unnecessary."
    why_human: "Cannot be resolved locally — it needs a scoped GITHUB_TOKEN in a real Actions run. Note the trap for #4895 step 2: the GraphQL step ONLY runs when `exempt == false`, so observing a green Issue Gate on a docs-only PR does NOT exercise this path and would falsely confirm. Step 2's observation MUST be made on a NON-EXEMPT PR."
  - test: "After merge, confirm the check context byte-matches before adding it to ruleset 11923801 (GH #4895 step 3)."
    expected: "The check run's name as GitHub reports it is exactly `Issue Gate`, with no workflow-name prefix, matching the precedent set by `Conventional Commit (PR title)`."
    why_human: "The job's display name is `Issue Gate` in the YAML (verified byte-exact), but whether GitHub renders the context with or without a prefix is an observation, not a static property. Adding the wrong string reproduces the permanently-Pending #4878 failure."
---

# Quick Task 260731-ea8: Issue-First Gate Enforcement — Verification Report

**Task Goal:** implement the issue-first gate enforcement workflow (GH holomush/holomush#4890)
**Verified:** 2026-07-31
**Status:** human_needed
**Re-verification:** No — initial verification
**Scope verified:** `b57c5f435..e03301ae1` on branch `gate-enforcement`

## Headline

Everything that could be proven locally, was proven locally — independently, not by
reading the SUMMARY. All three demonstrated-REDs re-derived from scratch and each was
red for the right reason. Both settled numbers held exactly. All locked decisions are
honoured in the shipped YAML. All four scope boundaries held. The ruleset is untouched
and the required-check half is correctly reported as PENDING.

The three truths left unverified are unverifiable *by construction* — a
`pull_request_target` workflow cannot run on its own PR — and #4895 step 2 is the
designed observation point. One concrete, actionable runtime risk was found in the
permissions block (see Human Verification); it is not a blocker but it should be
decided before #4895 step 3.

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Non-exempt + no gate-labelled linked issue -> `gate-violation` + comment + RED check | ⚠️ PRESENT_BEHAVIOR_UNVERIFIED | Decision logic verified live (RED 3 below). Label/comment/check emission never executed — structurally impossible pre-merge. |
| 2 | Exempt diff -> GREEN check from the same job | ⚠️ PRESENT_BEHAVIOR_UNVERIFIED | No `paths`/`paths-ignore` (yq: both `false`), no job-level `if` (yq: `false`), matcher returns 0 on live #4885/#4889. Runtime emission unobserved. |
| 3 | `.github/CODEOWNERS` / root `CODEOWNERS` NOT exempt even amid `.github/**` | ✓ VERIFIED | bats cases 4/5/6 pass; case 5 is the silent-trap mixed diff (`.github/CODEOWNERS` + `.github/workflows/ci.yaml`) -> exit 10. `docs/CODEOWNERS` -> exit 0 (case 7), so the carve-out is path-exact. Separate positive query present at `pr-gate-paths.sh:147`. |
| 4 | Draft PR gets GREEN without evaluating the gate | ⚠️ PRESENT_BEHAVIOR_UNVERIFIED | Step-level `if: ...draft == true` short-circuit at `:93-97`; every later step carries `draft != true`; no job-level `if`. Expression evaluation never executed. |
| 5 | Matcher fails LOUD (exit 1) on a missing var / truncated list — never degrades to "everything exempt" | ✓ VERIFIED | bats case 14 asserts exit **exactly 1** and passes. Guard runs in the MAIN shell (`pr-gate-paths.sh:99-107`), not inside a process substitution. Truncation guard re-derived live: declared==collected on all four fixtures (174==174 on #4874). |
| 6 | No PR is ever closed by automation | ✓ VERIFIED | `rg -v '^\s*#' issue-gate.yaml \| rg 'pr close\|--state closed\|state: closed'` -> no match. Statically provable negative. Every amended prose site also states it explicitly. |
| 7 | All twelve surviving closure assertions each state the automated consequence | ✓ VERIFIED | Post-edit sweep = **12** with the same per-file distribution as pre-edit (1/5/2/1/2/1). Diff shows 12 additions, zero deletions of closure sentences. Both literals present in all six files (MISSING = 0). |

**Score:** 4/7 truths verified (3 present, behavior-unverified)

### Item 1 — The three demonstrated-REDs, independently re-derived

Not taken from SUMMARY.md. Each reproduced from the repo.

**RED 1 — bats before the script existed.** Reproduced by copying `pr-gate-paths.bats`
+ `Taskfile.yaml` into a `mktemp -d` tree *without* `scripts/pr-gate-paths.sh`, so
`HELPER` resolves to a nonexistent path:

```
RED1_EXIT=1
NOT_OK_COUNT=15
OK_COUNT=0
BW01: `run`'s command `gate ...` exited with code 127, indicating 'Command not found'.
```

15/15 `not ok`, status 127 on every case. **Red for the right reason** — the failure is
the missing script, not a harness error: the identical file passes 15/15 in the real tree.

**RED 2 — wording gate before the edits.** Reproduced by materializing the pre-edit tree
(`git archive b57c5f435 | tar -x`) and running the Task 3 acceptance gate against it:

```
PRE_EDIT_GATE_EXIT=1
PRE_EDIT_MISSING_COUNT=11
PRE_EDIT_SITES=12
```

The eleven pairs listed match the SUMMARY's list byte-for-byte, and the asymmetry is
explained rather than coincidental — `CONTRIBUTING.md` already carried `gate-violation`
at `:215` (label table), so only its `Issue Gate` literal was missing. **Red for the
right reason.**

**RED 3 — live PR #4874.** Re-derived with the real matcher and the workflow's *own*
GraphQL query and jq predicate (copied out of the YAML), against live API data:

| PR | declared | collected | matcher exit | linked issue | verdict |
|---|---|---|---|---|---|
| #4885 | 4 | 4 | 0 | — | GREEN (exempt) |
| #4889 | 13 | 13 | 0 | — | GREEN (exempt) |
| #4894 | 12 | 12 | 10 | `holomush/holomush#4892 [CLOSED]` labels incl. `confirmed-bug` | GREEN (gated) |
| #4874 | 174 | 174 | 10 | `holomush/holomush#576 [CLOSED]` labels `type::bug,pr-review,pr-review-finding,pr:139,turn:1,aspect:security,priority::high,severity:important` | **RED** |

#4874 is RED for the right reason and for **both** required reasons independently: the
diff is not path-exempt (exit 10), *and* its only linked issue carries no gate label. The
truncation guard also holds on every fixture. #4894 is the useful control — also
non-exempt, but GREEN, which proves the gate is not simply failing everything.

### Item 2 — The two settled numbers

| Metric | Pre-edit (re-derived) | Post-edit (measured) | Verdict |
|---|---|---|---|
| MISSING LITERALS | **11** | **0** | ✓ no drift |
| SITES | **12** | **12** | ✓ no drift, none deleted |

Per-file site distribution is identical pre- and post-edit — `PULL_REQUEST_TEMPLATE.md` 1,
`feature` 2, `enhancement` 2, `chore` 1, `fix` 1, `CONTRIBUTING` 5. The two metrics stayed
distinct; neither collapsed into the other.

The twelve sites, at their shifted post-edit line numbers, all surviving:

| Pre-edit | Post-edit | Sentence |
|---|---|---|
| `PULL_REQUEST_TEMPLATE.md:36` | `:36` | "is closed without review." |
| `CONTRIBUTING.md:15` | `:15` | "issue gets closed" |
| `CONTRIBUTING.md:64` | `:67` | "is closed without review if the linked issue" |
| `CONTRIBUTING.md:84` | `:88` | "is closed without review if the linked" |
| `CONTRIBUTING.md:86` | `:90` | "incomplete spec; they close it." |
| `CONTRIBUTING.md:142` | `:148` | "PRs that arrive without a properly-labeled linked issue are closed." |
| `feature.md:20,22` | `:20,:22` | both survive |
| `enhancement.md:20,22` | `:20,:22` | both survive |
| `chore.md:20` | `:20` | survives |
| `fix.md:20` | `:20` | survives |

`CONTRIBUTING.md:86` — the site both CONTEXT.md and RESEARCH.md omit — survives at `:90`
and is amended *in the same sentence*: "…they close it. **Both of those are human
decisions, and the `Issue Gate` check precedes both**…". This is the strongest evidence
that the twelve were amended deliberately rather than swept up incidentally.

Every amendment adds text; the `git diff` shows **zero** deletions of a closure sentence.
No `auto-closed` / `closed automatically` / `automatically closed` wording anywhere.

### Item 3 — Locked decisions in `.github/workflows/issue-gate.yaml`

| Locked decision | Status | Evidence |
|---|---|---|
| Never closes a PR | ✓ | No `pr close` / `--state closed` / `state: closed` on any non-comment line. FAIL branch ends `exit 1`, not a close. |
| Skips drafts | ✓ | `:93-97` step-level `if: github.event.pull_request.draft == true`; all later steps `draft != true`. Deliberately NOT a job-level `if` (yq confirms `has("if") == false`) — a skipped job never reports its context (#4878). |
| Evaluates on `ready_for_review` | ✓ | Trigger set is set-equal to `{opened, reopened, synchronize, edited, ready_for_review, converted_to_draft}` — both over- and under-listing would fail the comparison. |
| NO `paths-ignore` | ✓ | yq: `.on.pull_request_target \| has("paths")` = `false`; `has("paths-ignore")` = `false`. |
| Applies `gate-violation` | ✓ | `:284` `gh pr edit "$PR" --repo "$REPO" --add-label gate-violation`, with **no** `\|\| true` — a permission failure is loud by design. |
| Fails the job on violation | ✓ | `:334` `exit 1` terminates the FAIL branch after label + comment upsert. |
| Job name is the future ruleset context | ✓ | yq: `.jobs["issue-gate"].name` == `Issue Gate`, byte-exact. |

Supporting: `permissions` = `{contents: read, pull-requests: write}` (`checks: write`
deliberately absent). No `pull_request.head` reference anywhere — the
`pull_request_target` privilege-escalation surface is closed. `task lint:actions` exit **0**.
Three-valued exit handling present at `:153-165` (`0` / `10` / `*` -> `exit 1`), so a
matcher malfunction cannot read as an exemption.

### Item 4 — Scope boundaries

| Boundary | Status | Evidence |
|---|---|---|
| Ruleset `11923801` not mutated | ✓ | Read-only `gh api` (exit 0) returns exactly the eight documented contexts: Build, Lint, Test, CodeRabbit, Integration Test, E2E Test, Conventional Commit (PR title), Vuln. Unchanged from the 2026-07-28 baseline. |
| No `ci-docs-skip.yaml` stand-in added | ✓ | `git diff --name-status b57c5f435..HEAD -- .github/workflows/` = `A issue-gate.yaml`, `M scripts-tests.yaml`. Nothing else. |
| No "auto-closed" wording restored | ✓ | `rg -i "auto-closed\|closed automatically\|automatically closed"` over all six files -> no match. |
| `DOCS_ONLY_PATHS` / `DEPENDENCY_ONLY_PATHS` unchanged | ✓ | `git diff b57c5f435..HEAD -- Taskfile.yaml` is **+24 / -0**: a comment block plus `REPO_CONFIG_ONLY_PATHS: \|` with the single entry `.github/**`, inserted after `compose.cluster.yaml`. Not one line removed or edited in either existing var. |

Also confirmed in scope: `.github/actionlint.yaml` and `.golangci.yaml` untouched (no lint
config widened — the one `shellcheck disable=SC2016` is line-scoped with rationale at
`:188`). No `.planning/` artifact committed. Full changed set is 11 files, all declared in
the plan's `files_modified`.

### Item 5 — Required status check correctly PENDING

| Check | Status | Evidence |
|---|---|---|
| `Issue Gate` absent from ruleset `11923801` | ✓ | Live read returns the eight originals; `Issue Gate` is **not** among them. |
| GH #4895 exists | ✓ | `gh issue view 4895` -> OPEN, labels `ci` + `type: chore`, titled "chore(ci): make Issue Gate a required context on ruleset 11923801 (post-merge, ordered)". |
| #4895 carries the ordered post-merge steps | ✓ | Body has the three numbered steps (merge -> observe a real non-`Pending` conclusion -> only then add the context), the structural reason (`pull_request_target` sourced from the default branch), the copy-pasteable snapshot/edit/PUT with `integration_id: 15368`, the "no stand-in wanted" note, and closes with "the 'required check' half of #4890 is **PENDING — POST-MERGE**, not done." |
| SUMMARY does not tick the box | ✓ | Reported under "PENDING — POST-MERGE (do not tick)". |

### Required Artifacts

| Artifact | Status | Details |
|---|---|---|
| `scripts/pr-gate-paths.sh` | ✓ VERIFIED | 166 lines, mode `100755` in git, SPDX header, `set -euo pipefail`, documented 0/10/1 contract, no glob compiler (git `:(glob)` pathspec), separate CODEOWNERS query at `:147`, fail-loud var guard in the main shell at `:99-107`. |
| `scripts/tests/pr-gate-paths.bats` | ✓ VERIFIED | 15 cases, every one a real assertion — no skip stubs. `bats scripts/tests/pr-gate-paths.bats` exit **0**, 15/15. |
| `.github/workflows/issue-gate.yaml` | ✓ VERIFIED | 335 lines, actionlint clean, all structural assertions pass. |
| `Taskfile.yaml vars.REPO_CONFIG_ONLY_PATHS` | ✓ VERIFIED | Present with sibling-style comment block; read at runtime by the matcher (proven by bats case 14, which errors when a fixture Taskfile omits it). |
| Follow-up issue with ruleset command | ✓ VERIFIED | GH #4895, OPEN. |

### Key Link Verification

| From | To | Via | Status |
|---|---|---|---|
| `issue-gate.yaml` job name | future ruleset context | byte-exact `Issue Gate`, no workflow prefix | ✓ WIRED (byte-match confirmed; GitHub-side rendering is a human-verification item) |
| `pr-gate-paths.sh` | `Taskfile.yaml` vars | runtime awk extraction of all three vars | ✓ WIRED — single source of truth, no second copy. Proven live: a fixture Taskfile missing the var exits 1. |
| `issue-gate.yaml` | `pr-gate-paths.sh` 0/10/1 contract | `case "$rc" in 0 \| 10 \| *)` at `:153-165` | ✓ WIRED — all three branches distinct, `*` exits 1. |
| GraphQL `closingIssuesReferences` | `nameWithOwner` filter -> gate-label check | jq predicate at `:224-236` | ✓ WIRED — re-executed against live #4894 (GREEN) and #4874 (RED). |
| `scripts-tests.yaml` | `issue-gate.yaml` | added to `on.pull_request.paths` | ✓ WIRED |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|---|---|---|---|
| bats suite green | `bats scripts/tests/pr-gate-paths.bats` | exit 0, 15/15 ok | ✓ PASS |
| full bats suite | `task test:bats` | exit 0, 83 tests | ✓ PASS |
| actionlint | `task lint:actions` | exit 0 | ✓ PASS |
| bats RED without script | temp tree, script absent | exit 1, 15/15 not ok, 127 | ✓ PASS (RED reproduced) |
| wording gate RED pre-edit | `git archive b57c5f435` + gate | exit 1, 11 missing | ✓ PASS (RED reproduced) |
| live matcher, 4 PRs | `pr-gate-paths.sh < <files>` | 0 / 0 / 10 / 10 | ✓ PASS |
| live GraphQL predicate, 2 PRs | workflow's own query + jq | #4894 gated, #4874 not | ✓ PASS (RED reproduced) |
| ruleset contexts | `gh api .../rulesets/11923801` | 8 contexts, no `Issue Gate` | ✓ PASS |
| GH #4895 | `gh issue view 4895` | OPEN, ordered steps present | ✓ PASS |
| workflow end-to-end on GitHub | — | cannot run pre-merge | ? SKIP -> human |

All verdicts read from exit codes. No pass/fail was inferred by grepping stdout.

### Anti-Patterns Found

| File | Pattern | Severity | Notes |
|---|---|---|---|
| — | `TBD` / `FIXME` / `XXX` | — | None in any changed file. |
| — | `TODO` / `HACK` / `PLACEHOLDER` | — | None in the three new files. |
| — | skip-stub tests | — | None; all 15 bats cases carry real assertions. |
| `issue-gate.yaml:188` | `# shellcheck disable=SC2016` | ℹ️ Info | Line-scoped with rationale (single-quoted GraphQL variables must not expand). No lint config widened — correct per repo rule. |

## Human Verification Required

### 1. Decide whether `issue-gate.yaml` needs `issues: read`

The Linked-issue verdict step reads **Issue** nodes via GraphQL
(`closingIssuesReferences -> nodes -> labels`). The workflow declares
`permissions: {contents: read, pull-requests: write}`, and once a `permissions:` block is
present GitHub sets every undeclared scope to `none` — so `issues` is `none`.

If that read needs `issues: read`, the failure lands in the worst possible shape. Measured
locally: on a denied/null response the step's own jq exits **5**
(`Cannot iterate over null`); under `set -euo pipefail` the step aborts, GitHub's implicit
`success()` skips `Apply verdict`, and the job goes **RED with no label and no comment** —
exactly the unattributable-failure shape `issue-gate.yaml`'s own comments and #4878 say the
design must avoid.

**Trap for #4895 step 2:** the GraphQL step only runs when `exempt == false`. Observing a
green `Issue Gate` on a docs-only PR does **not** exercise this path and would falsely
confirm. Step 2's observation must be made on a **non-exempt** PR.

Remedy if needed: add `issues: read` to the permissions block — one line, strictly safe,
costs nothing if unnecessary.

### 2. Confirm the context string before #4895 step 3

Before adding the context to ruleset `11923801`, confirm GitHub renders the check run as
exactly `Issue Gate` with no workflow-name prefix. The YAML is byte-exact and matches the
`Conventional Commit (PR title)` precedent, but the rendered string is an observation.
Adding the wrong string reproduces the permanently-Pending #4878 failure.

### 3. Observe the runtime behaviours listed in `behavior_unverified_items`

Label apply, label removal on fix, comment upsert (PATCH vs POST), `$RUNNER_TEMP` plumbing
across steps, base-ref checkout, and the draft short-circuit in both directions. All are
present and correctly wired in the YAML; none has ever executed. #4895 step 2 is the
designed observation point.

## Gaps Summary

**No gaps.** Nothing is missing, stubbed, unwired, or contradicted by the codebase. Every
claim in SUMMARY.md that could be independently re-derived, was — and every one held,
including both settled numbers and all three demonstrated-REDs, each red for the right
reason rather than for an unrelated error.

The status is `human_needed` rather than `passed` for one structural reason: three of the
seven truths assert GitHub Actions **runtime** behaviour that a `pull_request_target`
workflow cannot exercise on its own PR. That is a property of the platform, not a defect in
the work — the plan anticipated it, the SUMMARY discloses it honestly under "Could not
verify", and #4895 exists precisely to gate the required-check step behind a real
observation.

The one substantive finding is the `issues: read` permission question. It is a specific,
testable, plausible runtime failure with a one-line remedy, and it interacts with #4895
step 2 in a way that could produce a false confirmation. It should be decided before the
context is made required.

---

_Verified: 2026-07-31_
_Verifier: Claude (gsd-verifier)_
