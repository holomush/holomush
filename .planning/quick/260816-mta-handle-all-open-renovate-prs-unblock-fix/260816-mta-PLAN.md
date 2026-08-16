---
phase: quick-260816-mta
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - Taskfile.yaml
  - CONTRIBUTING.md
  - .github/PULL_REQUEST_TEMPLATE.md
  - .github/renovate.json
  - scripts/tests/pr-gate-paths.bats
  - compose.e2e.yaml
  - web/package.json
  - web/pnpm-lock.yaml
autonomous: false
requirements: [QUICK-260816-mta]
estimate:
  tokens: 90000
  raw_tokens: 45000
  tasks: 3
  confidence: low

must_haves:
  truths:
    - "#4851 has no CHANGES_REQUESTED review and no unresolved review thread; merge is left to the user."
    - "#4848 is closed with an explanatory comment, a tracking issue exists, and Renovate will not reopen a TypeScript 7 PR."
    - "#4910's branch carries a regenerated go.sum so its Go checks can pass."
    - "#4550 is closed as superseded."
    - "`web/buf.gen.yaml` is dependency-exempt in the authoritative Taskfile var and both prose mirrors, proven by a bats case that was RED before the change."
    - "compose.e2e.yaml's playwright image and web/package.json's @playwright/test are bumped in lockstep in a single PR, so E2E on main never sees a version-mismatched pair."
    - "renovate.json groups @playwright/test with the playwright Docker image so future bumps arrive together."
    - "Our PR links an approved issue with `Closes #NNN` and passes its own Issue Gate."
  artifacts:
    - Taskfile.yaml (DEPENDENCY_ONLY_PATHS + rationale comment)
    - CONTRIBUTING.md ("Exempt by file path" mirror)
    - .github/PULL_REQUEST_TEMPLATE.md ("Exemptions" mirror)
    - scripts/tests/pr-gate-paths.bats (new exempt + non-exempt cases)
    - .github/renovate.json (playwright group rule, typescript allowedVersions)
    - compose.e2e.yaml (playwright image tag + matching digest)
  key_links:
    - "compose.e2e.yaml image tag <-> web/package.json @playwright/test version (must match on the same minor)"
    - "Taskfile.yaml DEPENDENCY_ONLY_PATHS <-> scripts/pr-gate-paths.sh (read at runtime) <-> two prose mirrors"
    - "renovate.json packageRules array ORDER: last matching rule wins for groupName"
---

<objective>
Resolve all six remaining open Renovate PRs, and land the two durable repo-config
fixes that stop #4915 and #4917 from recurring.

Purpose: unblock a security update (#4851, CVE-2026-59727), stop weekly churn from
structurally-unmergeable PRs, and remove two classes of self-inflicted CI failure.
Output: one PR from the `renovate-triage` worktree carrying the durable fixes, plus
`gh` operations against the six Renovate PRs.

**Working directory for ALL file edits:**
`/Volumes/Code/github.com/holomush/.worktrees/renovate-triage` (branch `renovate-triage`).
`/Volumes/Code/github.com/holomush/holomush` is READ-ONLY.

Every `gh` invocation passes `-R holomush/holomush`.
Never add `[ci skip]` / `[skip ci]` to any commit (repo rule `sqzzr02e2j`).
</objective>

<context>
@.planning/STATE.md
@CLAUDE.md
@.claude/rules/landing-the-plane.md
@CONTRIBUTING.md
@Taskfile.yaml
@scripts/pr-gate-paths.sh
@scripts/tests/pr-gate-paths.bats
@.github/renovate.json
@compose.e2e.yaml
</context>

<already_diagnosed_do_not_reinvestigate>
The six PRs and their root causes are settled. Do NOT re-diagnose.

| PR | Update | Verified root cause | Decision |
|---|---|---|---|
| #4851 | astro v7 (SECURITY, CVE-2026-59727 XSS) | Stale CodeRabbit CHANGES_REQUESTED asking to regenerate `site/bun.lock` — the lockfile IS in the diff with astro 7 entries and all 11 checks are SUCCESS | Unblock, DO NOT MERGE |
| #4848 | typescript v7 | Svelte toolchain hard-errors: TS7 support needs TS7 **and** TS6 installed plus `--tsgo`. Ecosystem-readiness, not a repo bug | Close + tracking issue + Renovate pin |
| #4910 | Google Go (grpc 1.82.1 -> 1.83.0) | Diff is `go.mod` only; `renovate/artifacts` failed so `go.sum` was never regenerated; every Go package reports `[setup failed]` | Fix: regenerate go.sum, push |
| #4550 | all non-major (opened 2026-06-29) | Same stale `go.sum`; 7 weeks old; overlaps #4910's go.mod changes | Close as superseded |
| #4915 | @bufbuild/protoc-gen-es v2.14.0 | `web/buf.gen.yaml` is not in `DEPENDENCY_ONLY_PATHS`, so `pr-gate-paths.sh` exits 10 and Issue Gate demands a linked issue | Per-PR + durable config fix |
| #4917 | npm (minor) | `@playwright/test` 1.61.1 -> 1.62.1 but `compose.e2e.yaml` pins the `v1.61.0-noble` image; browsers live at a version-keyed path so the runner cannot find them | Per-PR + durable config fix |
</already_diagnosed_do_not_reinvestigate>

<traps>
1. **`compose.e2e.yaml` MUST NOT be added to the dependency exemption.** `CONTRIBUTING.md:172-173`
   states it is deliberately absent because it configures the required `E2E Test` check —
   a gate definition, not a dependency. Never push a `compose.e2e.yaml` edit onto a Renovate
   branch: that trades a red `E2E Test` for a red `Issue Gate`.
2. **The Issue Gate applies to OUR PR.** It touches `Taskfile.yaml`, `scripts/**`,
   `compose.e2e.yaml`, `web/**` — none exempt. Create the intake issue FIRST and put
   `Closes #NNN` in the PR body.
3. **`DEPENDENCY_ONLY_PATHS` has two prose mirrors.** `Taskfile.yaml` is authoritative;
   `CONTRIBUTING.md` "Exempt by file path" and `.github/PULL_REQUEST_TEMPLATE.md`
   "Exemptions" mirror it. All three move together. There is no byte-identity sync gate for
   this var (unlike `DOCS_ONLY_PATHS`), so drift is silent.
4. **Playwright image and runner MUST match on the same minor.** Browser binaries live at a
   version-keyed path inside the image. Bumping `compose.e2e.yaml` alone breaks E2E on main
   just as surely as bumping `@playwright/test` alone did on #4917. Both move in OUR PR.
5. **Digest is real, never invented.** `compose.e2e.yaml` pins tag AND `@sha256:`. Resolve
   the digest from the registry.
6. **`renovate/google-go` is a bot-owned branch.** Renovate may force-push over our commit.
   Say so in the summary and recommend prompt merge.
7. **`packageRules` order matters.** Last matching rule wins for `groupName`. The playwright
   rule goes at the END of the array so it overrides "Batch all non-major npm/pnpm bumps".
</traps>

<tasks>

<task type="tracer">
  <name>Task 1: Pre-flight probes and intake issues (no repo file edits)</name>
  <files>none — read-only probes plus `gh issue create`</files>
  <precondition>Docker is running (needed to resolve the playwright image digest) and `gh auth status` succeeds.</precondition>
  <action>
This task resolves every unknown the later tasks depend on, and creates the two issues
whose numbers are needed as inputs. Work in the `renovate-triage` worktree.

**1a. Read #4917's actual target playwright version.** Do not assume 1.62.1 — read it:
`gh pr diff 4917 -R holomush/holomush -- web/package.json`. Record the exact
`@playwright/test` version #4917 lands on. Call it `PW_VERSION`.

**1b. Resolve the playwright image tag and digest (trap 5).** Target tag is
`v${PW_VERSION}-noble`. Resolve with
`docker buildx imagetools inspect mcr.microsoft.com/playwright:v${PW_VERSION}-noble --format '{{.Manifest.Digest}}'`.
Fallback: `docker manifest inspect mcr.microsoft.com/playwright:v${PW_VERSION}-noble`.
Decide by EXIT CODE, never by grepping output. If that exact tag does not exist, pick the
highest `v${MINOR}.*-noble` tag published for the same minor and record which one and why.
Record `PW_IMAGE_TAG` and `PW_IMAGE_DIGEST`. Do NOT invent a digest under any circumstances.

**1c. Probe whether the buf codegen bump changes committed output (trap: generated web
stubs are committed and no `git diff --exit-code` gate covers the web side).**
In the worktree: temporarily edit `web/buf.gen.yaml` line `- remote: buf.build/bufbuild/es:v2.12.1`
to `:v2.14.0`, run `task --force web:generate`, then `git diff --stat`. Record the result.
Restore immediately and completely:
`git checkout -- web/buf.gen.yaml web/src/lib/connect` and confirm `git status --porcelain`
is clean before continuing.

This probe is a DECISION GATE for Task 2:
  - **No diff in `web/src/lib/connect/**`** -> proceed with adding `web/buf.gen.yaml` to
    `DEPENDENCY_ONLY_PATHS` in Task 2.
  - **Any diff** -> do NOT add the path in Task 2. Instead state plainly in the summary that
    the v2.14.0 bump regenerates committed stubs, that #4915 needs those regenerated files
    committed by a human (which makes it non-exempt regardless), and skip the Taskfile /
    CONTRIBUTING / PR-template / bats edits for that path. Everything else in Task 2 still
    proceeds. Surface this to the user before continuing.

**1d. Create the intake issue for OUR PR (trap 2).** `gh issue create -R holomush/holomush`
as a chore. Title along the lines of "chore: unbreak two recurring Renovate CI failures
(buf.gen pin exemption, playwright image/runner lockstep)". Body states the current state
(both failures reproduce weekly), the proposed work (the exact file list from this plan's
frontmatter), and what done means (a Renovate buf-codegen no-op bump passes Issue Gate; a
playwright bump arrives as one PR carrying both halves). Record the number as `CHORE_ISSUE`.
Note that this issue needs the `approved-chore` label from a maintainer — flag that to the
user rather than assuming it.

**1e. Create the TypeScript 7 tracking issue (decision 2).** `gh issue create -R holomush/holomush`
titled "Revisit TypeScript 7 once the Svelte toolchain supports it standalone". Body records
the verbatim blocker: TypeScript 7 support currently requires both TypeScript 7 and
TypeScript 6 installed in the project, and requires the `--tsgo` or
`--tsgo-experimental-api` flag. State that `.github/renovate.json` pins typescript to `<7`
and that this issue is the trigger to remove that pin. Link #4848. Record as `TS7_ISSUE`.

Report `PW_VERSION`, `PW_IMAGE_TAG`, `PW_IMAGE_DIGEST`, the 1c verdict, `CHORE_ISSUE`, and
`TS7_ISSUE` before starting Task 2.
  </action>
  <verify>
    <automated>cd /Volumes/Code/github.com/holomush/.worktrees/renovate-triage &amp;&amp; git status --porcelain | grep -q . &amp;&amp; echo "DIRTY - probe not restored" &amp;&amp; exit 1 || echo "clean"</automated>
  </verify>
  <done>PW_VERSION, PW_IMAGE_TAG and a registry-resolved PW_IMAGE_DIGEST are recorded; the 1c codegen verdict is recorded; CHORE_ISSUE and TS7_ISSUE exist; the worktree is clean.</done>
</task>

<task type="auto">
  <name>Task 2: Land the durable fixes in the worktree and open our PR</name>
  <files>Taskfile.yaml, CONTRIBUTING.md, .github/PULL_REQUEST_TEMPLATE.md, scripts/tests/pr-gate-paths.bats, .github/renovate.json, compose.e2e.yaml, web/package.json, web/pnpm-lock.yaml</files>
  <precondition>Task 1 reported its verdicts; `git status --porcelain` in the worktree is clean and the branch is `renovate-triage`.</precondition>
  <action>
All edits in `/Volumes/Code/github.com/holomush/.worktrees/renovate-triage`. Confirm with
`git worktree list` and `git branch --show-current` before touching a file.

**2a. RED proof for the new bats coverage (trap 3; repo rule `wdn1abkmd6`).** Do this BEFORE
editing `Taskfile.yaml`. Most cases in `scripts/tests/pr-gate-paths.bats` run against the
REAL Taskfile via the `gate()` helper, so a new case is a genuine RED/GREEN probe. Add two
cases next to the existing "dependency-only diff is exempt" case:

  - A case feeding `web/buf.gen.yaml`, `web/package.json`, `web/pnpm-lock.yaml` and asserting
    `[ "$status" -eq 0 ]`. This is the RED case.
  - A companion case feeding those three PLUS a `web/src/lib/connect/**/*_pb.ts` path and
    asserting `[ "$status" -eq 10 ]`. State in its comment that this one passes both before
    and after the change — it pins the exemption against over-widening and is NOT the RED
    proof.

Run `bats scripts/tests/pr-gate-paths.bats` and CAPTURE the output showing the first new case
FAILING with status 10. (A targeted probe is warranted here; `task test:bats` remains the
authoritative run in 2f.) If it does not fail, the case is not coverage — stop and report.

**2b. Taskfile.yaml — the authoritative var.** Add `web/buf.gen.yaml` to the
`DEPENDENCY_ONLY_PATHS` block scalar. Match the existing 4-space block indentation exactly;
the extractor in `scripts/pr-gate-paths.sh` terminates on any non-blank line that is not
4-space indented. Extend the surrounding comment with the rationale, in the voice of the
comments already there:

  - `web/buf.gen.yaml` carries only a pinned remote codegen plugin version, held in lockstep
    with the `@bufbuild/protoc-gen-es` devDependency by the existing "buf codegen"
    packageRule. It is a dependency manifest in the same sense as `package.json`.
  - The exemption is self-limiting: when a pin bump DOES change generated output, a human
    runs `task web:generate` and the regenerated `web/**/*_pb.ts` files enter the diff. Those
    match no exempt glob, so the PR becomes non-exempt automatically. Adding the pin file
    therefore exempts only the no-op case.
  - Contrast `compose.e2e.yaml`, which stays out because it defines the gate.

Re-run `bats scripts/tests/pr-gate-paths.bats` — the RED case must now be GREEN.

**2c. Both prose mirrors (trap 3).** Add `web/buf.gen.yaml` to the **Manifests** list in
`CONTRIBUTING.md` "### Exempt by file path" and in `.github/PULL_REQUEST_TEMPLATE.md`
"## Exemptions". Add one sentence to each carrying the self-limiting rationale. Leave the
existing sentence about regenerated code not being exempt intact — it now does more work,
not less.

**2d. Playwright lockstep (trap 4, trap 5).** In the SAME commit series:
  - `compose.e2e.yaml`: set the playwright service image to
    `mcr.microsoft.com/playwright:${PW_IMAGE_TAG}@${PW_IMAGE_DIGEST}` using the values Task 1
    resolved from the registry.
  - `web/package.json`: set `"@playwright/test"` to `PW_VERSION` (it is an exact pin today, no
    caret — keep it exact).
  - `web/pnpm-lock.yaml`: regenerate via `task web:install` (runs `pnpm install` in `web/`).
    Commit the resulting lockfile change.
  - Grep `compose.e2e.cover.yaml` for a playwright image pin; if one exists, bump it the same
    way. (At time of planning it had none — verify rather than assume.)

**2e. `.github/renovate.json`.**
  - Append a NEW packageRule at the END of the `packageRules` array (trap 7 — last match wins
    for `groupName`) grouping `@playwright/test` with the `mcr.microsoft.com/playwright`
    Docker image, e.g. `groupName: "playwright"`, matching both the npm package name and the
    docker image name. Set `automerge: false` and say why in the `description`: the grouped
    PR necessarily contains `compose.e2e.yaml`, which is deliberately non-exempt from the
    Issue Gate, so the group always needs a linked chore issue. That friction is the accepted
    price of the two halves never landing apart. Reference `CHORE_ISSUE`.
  - Add a rule pinning typescript below 7: match package name `typescript` with
    `allowedVersions: "<7"`, and a `description` naming `TS7_ISSUE` as the trigger to remove
    the pin and quoting the toolchain blocker. Use `allowedVersions` rather than disabling
    the package so TS 6 patch/minor updates keep flowing.
  - Validate the JSON parses (`node -e 'JSON.parse(require("fs").readFileSync(".github/renovate.json","utf8"))'`).

**2f. Gate, commit, push, PR.** `task fmt` first (it mutates files — SPDX headers, reflowed
tables — and uncommitted fmt output is a common cause of red CI). Then `task pr-prep`
(fast lane; LONG-RUNNING, several minutes — run it as a single command to completion, never
approximate it by running individual steps). Judge it by EXIT CODE and the
`▸ pr-prep result:` file, never by grepping stdout.

Because this PR changes the E2E harness definition itself, the fast lane does not prove the
playwright change. `task pr-prep:full` (Docker, LONG-RUNNING) gives local proof; if it is
skipped, the required CI `E2E Test` check is the authoritative gate and MUST be green before
merge — say which path was taken in the summary.

Commit atomically with conventional-commit messages, ending with the AI authorship byline.
Suggested split: (1) the gate-path exemption + bats + mirrors, (2) the playwright lockstep,
(3) the renovate.json rules. Then `git fetch origin && git rebase origin/main`, re-run
`task pr-prep`, `git push -u origin renovate-triage`.

Open the PR with the chore template and `Closes #CHORE_ISSUE` in the body (trap 2 — without
it our own Issue Gate goes red). Verify the Issue Gate check is green on our PR.
  </action>
  <verify>
    <automated>cd /Volumes/Code/github.com/holomush/.worktrees/renovate-triage &amp;&amp; bats scripts/tests/pr-gate-paths.bats &amp;&amp; printf 'web/buf.gen.yaml\nweb/package.json\nweb/pnpm-lock.yaml\n' | ./scripts/pr-gate-paths.sh; test $? -eq 0</automated>
    <automated>cd /Volumes/Code/github.com/holomush/.worktrees/renovate-triage &amp;&amp; node -e 'JSON.parse(require("fs").readFileSync(".github/renovate.json","utf8"))'</automated>
  </verify>
  <done>
The new bats case was captured RED before the Taskfile edit and is GREEN after; all three
copies of the dependency path list agree; `compose.e2e.yaml` and `web/package.json` name the
same playwright minor with a registry-resolved digest; `renovate.json` parses and carries the
playwright group (last in the array) and the typescript `<7` pin; `task pr-prep` exited 0;
the branch is pushed and the PR links `Closes #CHORE_ISSUE` with a green Issue Gate.
  </done>
</task>

<task type="auto">
  <name>Task 3: Renovate PR operations (gh only — touches no file in our worktree)</name>
  <files>none — `gh` operations against holomush/holomush, plus a throwaway worktree for #4910</files>
  <precondition>`gh auth status` succeeds with write access sufficient to dismiss a review and resolve a review thread.</precondition>
  <action>
Every command passes `-R holomush/holomush`. This task is deliberately separate from Task 2:
nothing here edits a file in the `renovate-triage` worktree.

**Part A — independent of our PR. Do these now.**

**3a. #4851 astro v7 — unblock, DO NOT MERGE.**
  1. Confirm the evidence before asserting it: `gh pr diff 4851 -R holomush/holomush --name-only`
     shows `site/bun.lock`, and `gh pr checks 4851 -R holomush/holomush` shows all checks
     SUCCESS. Quote the astro-7 entries from the lockfile diff in the reply.
  2. Find the unresolved thread:
     `gh api graphql -f query='query{repository(owner:"holomush",name:"holomush"){pullRequest(number:4851){reviewThreads(first:50){nodes{id isResolved isOutdated path line comments(first:1){nodes{author{login} body}}}}}}}'`
  3. Reply on that thread with the evidence (lockfile present, entries updated, `build` green),
     then resolve it:
     `gh api graphql -f query='mutation{resolveReviewThread(input:{threadId:"THREAD_ID"}){thread{isResolved}}}'`
  4. Dismiss the stale review. Get the id:
     `gh api repos/holomush/holomush/pulls/4851/reviews --jq '.[] | select(.state=="CHANGES_REQUESTED") | {id, user: .user.login, submitted_at}'`
     then `gh api -X PUT repos/holomush/holomush/pulls/4851/reviews/REVIEW_ID/dismissals -f message="..." -f event=DISMISS`
     with a message citing the lockfile evidence and the review's staleness date.
  5. Confirm `gh pr view 4851 -R holomush/holomush --json reviewDecision` no longer reports
     CHANGES_REQUESTED. **Leave the merge button to the user** — do not merge, do not enable
     auto-merge.

**3b. #4848 typescript v7 — close.** Comment explaining the close: quote the toolchain
blocker verbatim (TS7 support currently requires both TypeScript 7 and TypeScript 6 installed
and the `--tsgo` / `--tsgo-experimental-api` flag), link `TS7_ISSUE` as the revisit trigger,
and note that `.github/renovate.json` now pins typescript `<7` (landing via our PR) so this
will not churn weekly. Then `gh pr close 4848 -R holomush/holomush`.

**3c. #4910 Google Go — regenerate `go.sum` and push (trap 6).** Do NOT do this in the
`renovate-triage` worktree. Create a throwaway one:
```
git fetch origin renovate/google-go
git worktree add /Volumes/Code/github.com/holomush/.worktrees/renovate-google-go renovate/google-go
```
`cd` there, confirm `git branch --show-current` is `renovate/google-go`, then run `task deps`
(`go mod download` + `go mod tidy`) to regenerate `go.sum`. Confirm `go.sum` actually changed
(`git status --porcelain`); if it did not, stop and report — the diagnosis assumed it would.
Commit only `go.mod`/`go.sum` with a conventional-commit message and the AI authorship byline,
no `[ci skip]`, and push to `renovate/google-go`. Watch `gh pr checks 4910 -R holomush/holomush`
until the Go checks resolve.

Then clean up: `cd` to the repo root and
`git worktree remove /Volumes/Code/github.com/holomush/.worktrees/renovate-google-go`.

Record in the summary that `renovate/google-go` is a bot-owned branch — Renovate may
force-push over this commit on its next run, so #4910 should be merged promptly.

**3d. #4550 all-non-major — close as superseded.** Comment: opened 2026-06-29, carries the
same never-regenerated `go.sum`, and its `go.mod` changes overlap #4910's. Renovate will
recreate a fresh grouped PR against current main on its next run. Then
`gh pr close 4550 -R holomush/holomush`.

**Part B — blocked on our PR reaching main. Comment now, act after merge.**

**3e. #4915 buf codegen.** Post a comment stating the diagnosis (`web/buf.gen.yaml` was
outside `DEPENDENCY_ONLY_PATHS`, so `pr-gate-paths.sh` exits 10), that the fix is in
`CHORE_ISSUE` / our PR, and that this PR needs a rebase once that merges. If Task 1c found
that v2.14.0 regenerates committed stubs, say so here too and state that a human must run
`task web:generate` and commit the regenerated `web/**/*_pb.ts` on this PR — which makes it
non-exempt by design, exactly as `CONTRIBUTING.md` describes. Do NOT close it.

**3f. #4917 npm minor.** Post a comment stating that `compose.e2e.yaml` pinned a
version-mismatched playwright image, that our PR bumps the image and `@playwright/test` in
lockstep, and that this PR is currently `DIRTY` from the jsdom merge and needs a Renovate
rebase after ours lands. Do NOT push a `compose.e2e.yaml` edit onto this branch (trap 1). Do
NOT close it.

**Post-merge handoff (record in the summary, do not block on it):** once our PR is squash-merged,
comment `@renovatebot rebase` (or tick the rebase checkbox) on #4917 and #4915, then re-check
`gh pr checks` for both. #4917 should lose its playwright conflict and its E2E failure;
#4915's Issue Gate should flip to SUCCESS if and only if its diff stays inside the exempt
shapes.
  </action>
  <verify>
    <automated>gh pr list -R holomush/holomush --state open --label renovate-bot --json number,title,mergeStateStatus,reviewDecision --limit 20</automated>
    <automated>gh pr view 4851 -R holomush/holomush --json reviewDecision,state --jq '.reviewDecision + " " + .state'</automated>
    <automated>gh pr view 4848 -R holomush/holomush --json state --jq .state; gh pr view 4550 -R holomush/holomush --json state --jq .state</automated>
  </verify>
  <done>
#4851 reports no CHANGES_REQUESTED with its thread resolved and is still OPEN and unmerged;
#4848 and #4550 are CLOSED with explanatory comments; #4910's branch carries a regenerated
`go.sum` and its Go checks have been re-run; #4915 and #4917 carry diagnosis comments and
remain open; the throwaway `renovate-google-go` worktree is removed.
  </done>
</task>

</tasks>

<verification>
- `git worktree list` and `git branch --show-current` were checked before any file edit, and
  no file under `/Volumes/Code/github.com/holomush/holomush` was modified.
- The new bats case was observed FAILING (status 10) against the pre-change Taskfile, and
  PASSING after — captured output, not asserted from memory.
- `Taskfile.yaml`, `CONTRIBUTING.md` and `.github/PULL_REQUEST_TEMPLATE.md` list the same
  dependency paths.
- `compose.e2e.yaml`'s image tag and `web/package.json`'s `@playwright/test` name the same
  playwright minor; the digest came from the registry, not from this plan.
- `task pr-prep` exited 0 (read the exit code and the `▸ pr-prep result:` file, not stdout
  strings); the required CI `E2E Test` check is green on our PR before merge.
- No commit on any branch with an open PR carries `[ci skip]` / `[skip ci]`.
- All six PRs have a recorded disposition: #4851 unblocked-not-merged, #4848 closed,
  #4910 fixed, #4550 closed, #4915 and #4917 diagnosed and pending our merge.
</verification>

<success_criteria>
- Four PRs reach a terminal or green state without user intervention (#4848, #4550 closed;
  #4910 green; #4851 unblocked and left for the user to merge).
- Two PRs (#4915, #4917) are unblocked structurally by a merged config fix rather than by a
  one-off patch, so neither failure mode recurs on the next Renovate run.
- Our PR passes its own Issue Gate, which is itself the proof that the gate still works.
</success_criteria>

<output>
Create `.planning/quick/260816-mta-handle-all-open-renovate-prs-unblock-fix/260816-mta-SUMMARY.md` when done.

The summary MUST state explicitly:
- The Task 1c verdict (did the buf codegen bump change committed stubs?) and what followed.
- The resolved `PW_IMAGE_TAG` / `PW_IMAGE_DIGEST` and how they were resolved.
- That `renovate/google-go` is bot-owned and Renovate may force-push over our `go.sum`
  commit — #4910 should be merged promptly.
- Whether `task pr-prep:full` was run locally or CI's `E2E Test` is carrying the proof.
- The post-merge handoff steps for #4917 and #4915.
- That #4851 was deliberately left unmerged.
</output>
