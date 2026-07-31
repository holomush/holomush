# Quick Task 260731-ea8: issue-first gate enforcement (#4890) — Research

**Researched:** 2026-07-31
**Domain:** GitHub Actions workflow + glob matcher + required status check
**Confidence:** HIGH on Q1/Q2/Q4/Q5; HIGH on Q3 (empirically measured); MEDIUM on two Q6 sub-points (flagged inline)

All claims below carry `path:line` or a verified command + exit code. Two claims I
could **not** verify are marked `[UNVERIFIED]` explicitly rather than asserted.

---

## User Constraints (from CONTEXT.md)

Locked and not revisited here: label + failing check run (never close); skip drafts,
evaluate on `ready_for_review`; the check becomes a required context on ruleset
`11923801`; exemption means the check reports **GREEN from the same job** (no
`paths-ignore` on the enforcement workflow); Taskfile vars are the single source of
truth with a new `REPO_CONFIG_ONLY_PATHS`; `CODEOWNERS` never exempt; `Taskfile.yaml`
and `scripts/**` not exempt except listed lockfile shapes; `pr-template-exempt` waives
nothing.

---

## Q1 — Is a `ci-docs-skip.yaml` stand-in needed? **NO. Adding one would be actively harmful.**

**Verified.** The stand-in mechanism exists *only* because `ci.yaml` is path-filtered:

- `.github/workflows/ci.yaml:6-16` and `:19-28` — `on.push.paths-ignore` and
  `on.pull_request.paths-ignore` both list the ten `DOCS_ONLY_PATHS` globs.
- `.github/workflows/ci-docs-skip.yaml:6-10` states the reason verbatim:
  > "Same-name companion workflow for docs-only PRs. When ci.yaml is path-skipped,
  > branch-protection required checks (Build, Lint, Test, Integration Test, E2E
  > Test, Vuln) would otherwise stay 'Pending' forever. Job names here are
  > byte-identical to ci.yaml's required-check job names so GitHub treats them as
  > the same check."
- `.github/workflows/ci-docs-skip.yaml:12-18` records the #4878 failure mode:
  > "Adding a required check to the ruleset without adding its stand-in here
  > silently blocks every docs-only PR — GitHub reports BLOCKED with nothing
  > failing to point at (issue #4878 …)."

**Reasoning:** the stand-in is a *workaround for a path filter*. The locked design
removes the path filter, so the precondition for a stand-in does not exist. The
enforcement job runs on every PR and always reports a conclusion — green when exempt.

**Why a stand-in would be harmful here:** GitHub merges same-named checks across
workflows (`ci-docs-skip.yaml:8-10`). A second job with the same display name on a PR
that also runs the real job creates two check runs under one required context; the
ruleset requires *both* to pass. On a code PR the enforcement job runs AND a
hypothetical stand-in would also run → duplicate contexts, non-deterministic gate.
State the "not needed" conclusion explicitly in the PR body and in the issue's
"Done when" checkbox, per CONTEXT.md decision 1.

---

## Q2 — Can ruleset `11923801` be mutated from this session? **YES (token has admin), but treat as operator-gated.**

Read succeeded, exit 0:

```
gh api repos/holomush/holomush/rulesets/11923801 --jq '{name,target,enforcement,contexts:[...]}'
→ {"contexts":["Build","Lint","Test","CodeRabbit","Integration Test",
    "E2E Test","Conventional Commit (PR title)","Vuln"],
   "enforcement":"active","name":"protect-main","target":"branch"}
EXIT=0
```

Eight contexts — matches CONTEXT.md's "eight required contexts as of 2026-07-28".

Write capability, determined **without mutating**:

- `gh auth status` (exit 0) → token scopes `'admin:org', 'codespace', 'gist', 'repo',
  'workflow', 'write:discussion', 'write:packages'`. `repo` is the scope the repo-ruleset
  API requires.
- `gh api repos/holomush/holomush --jq '.permissions'` (exit 0) →
  `{"admin":true,"maintain":true,"pull":true,"push":true,"triage":true}`.

Both preconditions for `PUT /repos/{owner}/{repo}/rulesets/{id}` are satisfied, so the
write is technically available.

**Recommendation regardless:** do **not** perform the write inside this change. Q4
establishes that a `pull_request_target` workflow only runs from the **default branch**
(GitHub changelog, effective 2025-12-08: *"The workflow file and checkout commit will
always be taken from the repository's default branch"*). The enforcement workflow
therefore **cannot run on its own PR**, and cannot run on any PR until it is merged to
`main`. Adding the context to the ruleset before the workflow exists on `main`
reproduces #4878 exactly: every open PR blocks on a check that never arrives.

**Correct ordering — this is the load-bearing sequencing constraint of the whole task:**

1. Merge the workflow to `main` (context absent from ruleset; nothing blocks).
2. Confirm the check appears and is green/red correctly on at least one subsequent PR.
3. *Then* add the context to the ruleset.

So the deliverable is workflow + a copy-pasteable operator instruction, and the
"required check" state is reported as **PENDING OPERATOR** — matching CONTEXT.md's
fallback, but for a sequencing reason rather than a permissions reason. Record that
distinction; it is not the reason prior sessions recorded.

Copy-pasteable step 3 (append, do not replace — the API replaces the whole rules array):

```bash
gh api repos/holomush/holomush/rulesets/11923801 > /tmp/rs.json   # snapshot first
# add {"context":"<Job Display Name>","integration_id":15368} to the
# required_status_checks array, then:
gh api -X PUT repos/holomush/holomush/rulesets/11923801 --input /tmp/rs-edited.json
```

---

## Q3 — Minimal correct glob→matcher approach: **(c) git `:(glob)` pathspec. No compiler at all.**

### The three candidate engines, measured

Empirical results (each command exit 0):

| Pattern | Input | git `:(glob)` | gobwas/glob | doublestar v4 |
|---|---|---|---|---|
| `**/go.mod` | `go.mod` (root) | ✅ match | ❌ **no match** | ✅ match |
| `**/go.mod` | `a/b/go.mod` | ✅ | ✅ | ✅ |
| `**/Dockerfile` | `Dockerfile` (root) | ✅ | ❌ **no match** | ✅ |
| `**/*.md` | `README.md` (root) | ✅ (n/a in fixture) | ❌ **no match** | ✅ |
| `go.tool*.mod` | `go.tool.mod`, `go.tool-lint.mod` | ✅ both | ✅ both | ✅ |
| `.github/**` | `.github/CODEOWNERS`, `.github/workflows/x.yaml` | ✅ both | ✅ | ✅ |

git measurement (git 2.55.0, scratch repo, `git ls-files -- ':(glob)…'`):

```
':(glob)**/go.mod'     → a/b/go.mod, go.mod
':(glob)go.tool*.mod'  → go.tool-lint.mod, go.tool.mod
':(glob)**/Dockerfile' → Dockerfile, a/Dockerfile
':(glob).github/**'    → .github/CODEOWNERS, .github/workflows.yaml, .github/workflows/x.yaml
```

**gobwas/glob is disqualified.** It is a *direct* dependency at `go.mod:13`
(`github.com/gobwas/glob v0.2.3`, used by `internal/access/policy/compiler.go` and
`internal/access/policy/dsl/evaluator.go`), so it is the obvious reach — but it does
**not** match `**/x` against a root-level `x`. `Taskfile.yaml:95-97` states the hard
requirement:

> "The same hedge is NOT repeated for `**/go.mod`, `**/go.sum`, `**/package.json` or
> `**/pyproject.toml`: both dialects this repo uses match those at root (verified),
> and #4890 must preserve that."

gobwas would silently under-match root `go.mod`/`go.sum`/`package.json`/`pyproject.toml`
— i.e. a genuine Renovate dependency-only PR gets flagged. Fail-closed, not a security
hole, but wrong and exactly the "silently wrong matcher" class this task exists to avoid.

**doublestar is disqualified on availability**, not semantics. It is correct
(all six probes matched) but appears only as an **indirect** golangci-lint dependency:
`go.tool-lint.mod:54` `github.com/bmatcuk/doublestar/v4 v4.9.1 // indirect`. It is not
in `go.mod`; using it means adding a new direct dependency to the main module.

### Recommendation: option (c), git pathspec — no glob compiler is written at all

Do **not** generalize `scripts/docs-paths-regex.sh` (option a) and do not write a
sibling compiler (option b). Both re-implement a glob engine; the repo already ships
one with the exact dialect the Taskfile comment demands.

Shape:

```bash
# NONEXEMPT = changed files minus every exempt glob
NONEXEMPT=$(git diff --name-only "$BASE_SHA...$HEAD_SHA" -- \
  ':(exclude,glob)site/**' ':(exclude,glob)**/*.md' … )
# CODEOWNERS carve-out: a separate positive query
OWNERS=$(git diff --name-only "$BASE_SHA...$HEAD_SHA" -- \
  ':(glob)**/CODEOWNERS' ':(glob)CODEOWNERS')
exempt = [ -z "$NONEXEMPT" ] && [ -z "$OWNERS" ]
```

The exclude pathspecs are generated by a trivial `sed`/`awk` wrapper over the Taskfile
var — string prefixing `:(exclude,glob)`, **not** glob compilation. That is the
difference that makes this safe.

Verified exclude behaviour (exit 0 each):

```
git ls-files -- ':(glob).github/**' ':(exclude).github/CODEOWNERS'        → .github/workflows.yaml
git ls-files -- ':(glob).github/**' ':!(glob)**/CODEOWNERS'               → .github/workflows.yaml
git ls-files -- ':(glob).github/**' ':(exclude,glob).github/CODEOWNERS'   → .github/workflows.yaml
```

**Known limitation, and why the two-query shape above is required:** git pathspec
applies all `:(exclude)` terms last and ANDs them. There is no "exclude `.github/**`
then re-include `CODEOWNERS`" ordering. The CODEOWNERS carve-out is therefore a
**separate positive query**, not an exclusion-of-an-exclusion. Do not attempt to
express it inline; it cannot be done and the failure is silent.

**Getting the diff without executing PR code.** Under `pull_request_target` the runner
checks out the base ref. Fetch the PR head **objects only** and diff:

```bash
git fetch --no-tags --depth=… origin "refs/pull/${PR}/head"
git diff --name-only "$BASE_SHA...$HEAD_SHA" -- …
```

Fetching objects is data transfer; nothing from the PR is executed or checked out. This
is the safe boundary (see Q4). The alternative — `gh api /repos/.../pulls/N/files
--paginate --jq '.[].filename'` — avoids the fetch entirely but yields a bare string
list that git pathspec cannot match against, which would drag the glob compiler back in.
Prefer the fetch.

**Go-program precedent exists but is not needed here.** `cmd/lint-plugin-manifests/main.go`
(wired at `Taskfile.yaml:782-785`, `go run ./cmd/lint-plugin-manifests`, called from the
lint umbrella at `Taskfile.yaml:185`) is the precedent CLAUDE.md's plan-review learnings
point to for "yq is not installed in CI". It is the right fallback **if** the reviewer
rejects the git-pathspec approach — but it would require adding doublestar as a direct
dep, since gobwas (already present) has the wrong `**` semantics.

**Note on `yq` availability:** the repo's plan-review learnings say "yq is NOT installed
in HoloMUSH CI". That is now only *partly* true — `scripts-tests.yaml:59-70` builds
mikefarah yq from `go.tool.mod` specifically because `docs-paths-regex.sh` and
`lint-docs-paths-sync.sh` need it, but it does so **only in the `bats` job**. A new
workflow gets no yq unless it repeats that install step. Prefer parsing the Taskfile var
with `sed`/`awk` in the enforcement workflow, or repeat the pinned build step.

---

## Q4 — Safe `pull_request_target` shape

### Trigger

```yaml
on:
  pull_request_target:
    types: [opened, reopened, synchronize, edited, ready_for_review, converted_to_draft]
    # NO paths / paths-ignore — see Q1
```

- `edited` **is required.** `edited` fires on title/body change, which is the only way a
  `Closes #N` link is added or removed after open. Without it, a PR opened with no link
  and fixed by a body edit keeps a stale red check forever.
- `ready_for_review` is required by the locked draft decision.
- `converted_to_draft` is recommended so a PR flipped back to draft gets its check turned
  green again rather than left red.
- `synchronize` is required because the exemption verdict is diff-derived — a new push can
  add a non-exempt file to a previously-exempt PR.

### Permissions (least privilege)

```yaml
permissions:
  contents: read          # fetch base + PR head objects
  pull-requests: write    # label + comment
```

`checks: write` is **not** needed if the job's own conclusion is the check (see below).
`buf.yml:22-24` is the existing `pull-requests: write` precedent in this repo:

```yaml
permissions:
  contents: read
  pull-requests: write
```

Every other workflow in `.github/workflows/` uses `permissions: contents: read` only
(verified: `ci.yaml:32-33`, `ci-docs-skip.yaml:58-59`, `scripts-tests.yaml:17-18`).

### Hard security rule

`pull_request_target` runs the **base-branch** workflow with a write token.
**Never** `actions/checkout` the PR head ref, never run `task`/`npm`/`make`/any script
from the PR head, never `source` a PR-authored file. Fetching PR head *objects* for
`git diff --name-only` is fine; checking them out into the working tree is not.
Note `ci-docs-skip.yaml:69-74` already sets `persist-credentials: false` on checkout as
a zizmor-artipacked hardening — mirror that.

### Emitting the check: use the job's own conclusion, **not** `gh api /check-runs`

Two facts, both verified:

1. **The required-check context is the job's `name:` value**, not `<workflow>/<job>`.
   `ci.yaml` job ids vs display names:
   `lint`→`Lint` (`:36-37`), `vuln`→`Vuln` (`:99,:103`), `test`→`Test` (`:137-138`),
   `integration`→`Integration Test` (`:186-187`), `E2E Test` (`:230`),
   `build`→`Build` (`:296-297`). The ruleset's six ci.yaml-sourced contexts are exactly
   `["Build","Lint","Test","Integration Test","E2E Test","Vuln"]` — a byte match against
   the display names, with no workflow-name prefix.

2. **A `pull_request_target` job's check run attaches to the PR head SHA**, despite
   `GITHUB_SHA` being the base. GitHub docs state for `pull_request_target`:
   *GITHUB_SHA = "Last commit on default branch"*, GITHUB_REF = "Default branch"
   (vs `pull_request`: "Last merge commit on the GITHUB_REF branch").
   That is the **env var**, not the check-run anchor. Measured on a live public repo
   (exit 0):

   ```
   gh api repos/vercel/next.js/actions/runs?event=pull_request_target&per_page=1
     → {"name":"Pull Request Auto-Labeler","head_branch":"yav/error-styles",
        "head_sha":"86b4e638101f0c923207dd3e67ce50413e004f95"}
   gh api repos/vercel/next.js/commits/86b4e638…/check-runs
     → {"name":"label","app":"github-actions","conclusion":"success",
        "head_sha":"86b4e638101f0c923207dd3e67ce50413e004f95"}
   gh api repos/vercel/next.js/commits/canary --jq .sha
     → 6f7ed2ecb936ee58ce8654da317ea1e2b06cb598   # different — NOT the default-branch tip
   ```

   The check run sits on the PR head branch's SHA, so branch-protection evaluation sees it.

Conclusion: give the job a stable `name:` (e.g. `name: Issue Gate`), let it `exit 1` on
violation and `exit 0` on pass/exempt/draft. That single string is the ruleset context.
An explicit `POST /repos/{o}/{r}/check-runs` would additionally need `checks: write` and
would create a *second* context — avoid it.

### The workflow cannot run on its own PR

GitHub changelog 2025-11-07, effective **2025-12-08**:
> "The workflow file and checkout commit will always be taken from the repository's
> default branch, regardless of the pull request's base branch."

Consequence: the enforcement workflow's own PR will show no such check. Do not treat
that absence as a bug, and do not add the ruleset context before merge (Q2).

---

## Q5 — Repo precedents to imitate

| Need | Precedent | Location |
|---|---|---|
| `pull-requests: write` workflow | Buf Proto CI | `.github/workflows/buf.yml:22-24` |
| Job display name = ruleset context | ci.yaml jobs | `ci.yaml:36-37, 99/103, 137-138, 186-187, 230, 296-297` |
| bats tests for a shell script | 12 `.bats` files, fixture-driven | `scripts/tests/` — `lint-docs-paths-sync.bats:10-30` builds a `mktemp -d` fixture repo rather than touching real files; copy that shape |
| bats runner | `task test:bats` | `Taskfile.yaml:406-414`; CI job `bats` in `.github/workflows/scripts-tests.yaml:41-…` |
| `task lint:*` sync gate | `lint:docs-paths-sync` → `scripts/lint-docs-paths-sync.sh` | `Taskfile.yaml:787-790`; script compares the Taskfile var against 4 mirror extraction points and diffs on drift (`lint-docs-paths-sync.sh:41-63`) |
| actionlint on new workflows | `{{.GO_TOOL_LINT}} actionlint -config-file .github/actionlint.yaml .github/workflows/*` | `Taskfile.yaml:747`; config at `.github/actionlint.yaml` (declares three `namespace-profile-*` self-hosted labels only) |

**`scripts-tests.yaml` needs a new path trigger.** Its `on.pull_request.paths`
(`scripts-tests.yaml:8-15`) lists `scripts/**`, `Taskfile.yaml`,
`.github/workflows/scripts-tests.yaml`, `.github/actions/**`,
`.claude/skills/holomush-dev/scripts/**`. A new bats file under `scripts/tests/` is
covered by `scripts/**`, but the new **workflow** file is not — add it if the bats suite
should re-run when the workflow changes.

**`REPO_CONFIG_ONLY_PATHS` and the sync gate.** `lint-docs-paths-sync.sh` exists because
`DOCS_ONLY_PATHS` is mirrored into two workflow files (`Taskfile.yaml:25-29` names the
contract). `REPO_CONFIG_ONLY_PATHS` will have exactly **one** machine consumer (the new
workflow, which reads the Taskfile var directly) and **prose** mirrors in
`CONTRIBUTING.md:172-175` and `.github/PULL_REQUEST_TEMPLATE.md`. If the workflow reads
the var at runtime there is no second copy to drift, so a byte-identity sync gate is
unnecessary — but the *prose* mirror can still drift. Note this rather than reflexively
cloning the sync gate; `Taskfile.yaml:41-48` already sets the precedent of documenting
"this var has NO machine consumer yet and is NOT covered by any sync gate."

---

## Q6 — Pitfalls specific to this task

### 6a. Do NOT regex-parse `Closes #N`. Use GraphQL `closingIssuesReferences`.

Verified working (exit 0):

```
gh api graphql -f query='{repository(owner:"holomush",name:"holomush"){
  pullRequest(number:4889){ number isDraft
    closingIssuesReferences(first:10){ nodes{
      number state repository{nameWithOwner} labels(first:20){nodes{name}} }}}}}'
→ {"number":4889,"isDraft":false,"closingIssuesReferences":{"nodes":[
    {"number":4888,"state":"CLOSED","repository":{"nameWithOwner":"holomush/holomush"},
     "labels":{"nodes":[{"name":"meta"},{"name":"type: chore"},{"name":"approved-chore"}]}}]}}
```

One query yields **link + issue state + repo + labels** — everything the gate needs.
GitHub computes the link itself, so it honors GitHub's own keyword list and its own
markdown handling. This sidesteps the entire regex-in-code-block problem by construction.

Regex risk this avoids, concretely: `feature.md:113` contains
`` - [ ] Issue linked above with `Closes #NNN`, and it carries `approved-feature` `` —
inline code. A contributor who writes a real number in a similar inline-code example
would trip a naive `rg 'Closes #[0-9]+'`. There are **no fenced code blocks** in any of
the five template files (verified: `rg -n '^```' .github/PULL_REQUEST_TEMPLATE.md
.github/PULL_REQUEST_TEMPLATE/` → no output, exit 1), but inline code and HTML comments
are present throughout.

`[UNVERIFIED]` — I did not empirically confirm that GitHub excludes fenced/inline code
and `<!-- -->` comments when computing `closingIssuesReferences`. It is authoritative by
definition (it is the same resolution that drives auto-close on merge), so a mismatch
between the gate and GitHub's own behavior is impossible even if the underlying parser
is permissive. That is the argument for using it, independent of parser details.

### 6b. Keyword list — moot if using GraphQL

GitHub's full list is `close/closes/closed/fix/fixes/fixed/resolve/resolves/resolved`.
The templates instruct only two of them: `Closes #` in feature/enhancement/chore
(`feature.md:18`, `enhancement.md:18`, `chore.md:18`) and `Fixes #` in fix
(`fix.md:18`). Under GraphQL the gate accepts whatever GitHub accepts, so no list needs
maintaining — a real advantage over regex.

### 6c. Cross-repo refs

Templates produce bare `Closes #` / `Fixes #` only — no `owner/repo#N` and no URLs
(verified by the `rg -n -i 'closes|fixes|resolves'` sweep over all five template files;
only the eight hits listed above). But a contributor **can** write one, and
`closingIssuesReferences` returns cross-repo nodes. The query already selects
`repository{nameWithOwner}` — **filter to `holomush/holomush`** and treat a cross-repo-only
link as no link. Otherwise a PR could satisfy the gate by pointing at an approved issue in
a repo the maintainers don't control.

### 6d. Linked issue in an unexpected state

`closingIssuesReferences` returns `state` (`OPEN`/`CLOSED`) — PR #4889's link resolved to
issue #4888 in state `CLOSED` (that is normal: the issue closed when the PR merged).
Decide and document:
- **CLOSED** — at PR-open time a closed issue is a smell, but blocking on it breaks
  legitimate follow-up PRs against an issue closed early. Recommend: check the **label**,
  not the state, and let the human catch state problems.
- **Transferred / deleted** — a transferred issue's node resolves in the new repo, which
  6c's same-repo filter would then reject. Rare; acceptable to flag.
- The gate's real predicate is **"link exists in this repo AND carries the matching
  `approved-*` / `confirmed-bug` label"**, not issue state.

Labels verified present on the repo (`gh label list`, exit 0): `approved-feature`,
`approved-enhancement`, `approved-chore`, `confirmed-bug`, `gate-violation`,
`needs-triage`, `needs-review`, `needs-verify`.

### 6e. Pre-policy backlog — the transitional note already exists

`gh api 'search/issues?q=repo:holomush/holomush+is:issue+is:open'` → **241** open issues
(exit 0). CONTRIBUTING.md:18 warns about "the 200+ open issues … filed by the" original
author. The documented transitional note is `CONTRIBUTING.md:201-203`:

> "**Transitional note:** issues filed before this policy was adopted predate the gate
> labels and are being triaged retroactively. A missing gate label on an older issue is a
> backlog artifact, not an approval."

So the transitional note **is** documented and the policy is explicit: missing label is
**not** approval. The gate should flag such PRs (that is the intended behavior), and the
violation comment should link to `CONTRIBUTING.md#exempt-by-file-path` and name the
retroactive-triage path so contributors know the remedy is "ask a maintainer to label the
issue", not "argue with the bot".

### 6f. Template enforcement wording is currently WRONG and must be corrected in this change

CONTEXT.md asserts the "auto-closed" phrasings "were corrected". They were **not** — five
"closed" assertions survive on `main` and would now be actively false once the workflow
lands (it labels and blocks; it never closes):

- `.github/PULL_REQUEST_TEMPLATE.md:36` — "without a linked, approved issue is closed without review."
- `.github/PULL_REQUEST_TEMPLATE/feature.md:20` and `:22` — "is closed without review" / "gets closed"
- `.github/PULL_REQUEST_TEMPLATE/enhancement.md:20` and `:22` — same two
- `.github/PULL_REQUEST_TEMPLATE/chore.md:20` — "is closed without review"
- `.github/PULL_REQUEST_TEMPLATE/fix.md:20` — "is closed without review"
- `CONTRIBUTING.md:15`, `:64`, `:84` — "issue gets closed" / "is closed without review"

Per the locked decision the truthful phrasing is **"flagged and blocked from merge"**.
This is in scope: shipping the workflow without fixing these leaves the repo asserting an
enforcement that still cannot fire — the exact defect class this task exists to close.

### 6g. Two more traps

- **Two-query CODEOWNERS carve-out.** Restated because it is silent: git pathspec cannot
  re-include after exclude. If someone folds CODEOWNERS into the exclusion list the gate
  self-exempts governance changes and nothing fails.
- **Empty-diff edge case.** A PR with zero changed files (or a pure merge-commit
  refresh) yields an empty `NONEXEMPT`, which the naive check reads as "exempt → green".
  Guard: if the full changed-file list is empty, treat as exempt-green deliberately and
  say so, rather than letting it fall out of the logic by accident.

---

## Recommended shape (one paragraph)

Single workflow `.github/workflows/issue-gate.yaml`, `on: pull_request_target` with the
six types above and **no path filter**; `permissions: {contents: read, pull-requests: write}`;
one job with a stable `name:` that becomes the ruleset context. Steps: (1) short-circuit
green if `github.event.pull_request.draft`; (2) checkout base with
`persist-credentials: false`; (3) `git fetch origin refs/pull/N/head` (objects only, never
checkout); (4) read `DOCS_ONLY_PATHS` / `DEPENDENCY_ONLY_PATHS` / new
`REPO_CONFIG_ONLY_PATHS` from `Taskfile.yaml` and prefix each line with
`:(exclude,glob)`; (5) `git diff --name-only base...head` with those excludes → if empty
**and** the separate `:(glob)**/CODEOWNERS` query is empty → exempt, exit 0; (6) otherwise
GraphQL `closingIssuesReferences`, filter to `holomush/holomush`, require the matching
`approved-*` / `confirmed-bug` label; (7) on pass remove `gate-violation` if present and
exit 0; on fail add `gate-violation`, upsert a single explanatory comment, exit 1. Tests:
a bats file under `scripts/tests/` exercising the pathspec matcher against a `mktemp -d`
fixture repo, mirroring `lint-docs-paths-sync.bats:10-30`. Ruleset context added by the
operator **after** merge to `main`.

---

## Sources

**Primary (HIGH — read this session in the `gate-enforcement` worktree)**
`Taskfile.yaml:20-114, 185, 406-414, 747, 782-790` · `.github/workflows/ci.yaml:1-60, 99-297` ·
`.github/workflows/ci-docs-skip.yaml` (full) · `.github/workflows/buf.yml` (full) ·
`.github/workflows/scripts-tests.yaml` (full) · `.github/actionlint.yaml` (full) ·
`scripts/docs-paths-regex.sh` (full) · `scripts/lint-docs-paths-sync.sh:1-63` ·
`scripts/tests/lint-docs-paths-sync.bats:1-30` · `CONTRIBUTING.md:148-203` ·
`.github/PULL_REQUEST_TEMPLATE.md` + `PULL_REQUEST_TEMPLATE/{feature,fix,chore,enhancement}.md` ·
`cmd/lint-plugin-manifests/main.go` (existence)

**Primary (HIGH — commands run this session, all exit 0)**
`gh api repos/holomush/holomush/rulesets/11923801` · `gh auth status` ·
`gh api repos/holomush/holomush --jq .permissions` · `gh label list` ·
`gh api search/issues` · `gh api graphql closingIssuesReferences` ·
`gh api repos/vercel/next.js/{actions/runs,commits/*/check-runs,commits/canary}` ·
git 2.55.0 pathspec probes in a scratch repo · Go probes of `gobwas/glob v0.2.3` (in-module)
and `bmatcuk/doublestar/v4 v4.9.1` (scratch module)

**Secondary (MEDIUM — official GitHub docs/changelog, fetched this session)**
docs.github.com "Events that trigger workflows" (GITHUB_SHA/GITHUB_REF per event) ·
github.blog changelog 2025-11-07 "Actions pull_request_target and environment branch
protections changes" (effective 2025-12-08)
