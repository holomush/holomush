---
phase: 260731-ea8-implement-the-issue-first-gate-enforceme
reviewed: 2026-07-31T16:25:45Z
depth: quick
files_reviewed: 11
files_reviewed_list:
  - .github/workflows/issue-gate.yaml
  - .github/workflows/scripts-tests.yaml
  - scripts/pr-gate-paths.sh
  - scripts/tests/pr-gate-paths.bats
  - Taskfile.yaml
  - CONTRIBUTING.md
  - .github/PULL_REQUEST_TEMPLATE.md
  - .github/PULL_REQUEST_TEMPLATE/chore.md
  - .github/PULL_REQUEST_TEMPLATE/enhancement.md
  - .github/PULL_REQUEST_TEMPLATE/feature.md
  - .github/PULL_REQUEST_TEMPLATE/fix.md
findings:
  critical: 2
  warning: 11
  info: 0
  total: 13
status: issues_found
---

# Phase 260731-ea8: Code Review Report

**Reviewed:** 2026-07-31T16:25:45Z
**Depth:** quick
**Files Reviewed:** 11
**Status:** issues_found

## Summary

The `pull_request_target` hardening is genuinely solid — I could not find a code-execution
or shell-injection path. The base ref is checked out with `persist-credentials: false`, no
head-ref checkout exists, every `${{ }}` expansion lands in an `env:` block rather than a
`run:` body, no PR title/body/branch/author is read at all, and the GraphQL query passes
`-F`/`-f` typed parameters instead of string interpolation. The three-valued exit contract
is genuinely branched on all three values (`issue-gate.yaml:153-165`). Those were the
highest-risk surfaces and they hold up.

Two defects rise to BLOCKER. The CODEOWNERS carve-out — the one governance exception the
design goes out of its way to protect, with a dedicated second git query and a dedicated
bats case — is **bypassable via `docs/CODEOWNERS`**, which GitHub honors as a first-class
CODEOWNERS location and which the matcher and its test explicitly declare exempt. And the
truncation guard that exists specifically to stop the gate failing open does not fire when
its input is non-numeric, because `[ x -ne y ]` returns 2 and `if` swallows that.

Everything below was reproduced by running the actual script; no finding is speculative.

## Structural Findings (fallow)

No structural pre-pass was provided with this review.

## Narrative Findings (AI reviewer)

## Critical Issues

### CR-01: `docs/CODEOWNERS` defeats the CODEOWNERS carve-out entirely

**File:** `scripts/pr-gate-paths.sh:147`, pinned wrong by `scripts/tests/pr-gate-paths.bats:99-102`

**Issue:** GitHub reads a CODEOWNERS file from **three** locations: the repository root,
`.github/`, and **`docs/`**. The positive carve-out query only covers two of them:

```sh
owners="$(git -C "$scratch" ... ls-files -- ':(glob).github/CODEOWNERS' 'CODEOWNERS')"
```

`docs/CODEOWNERS` is swallowed by `DOCS_ONLY_PATHS`' `docs/**` glob and reported exempt.
Reproduced against the committed script:

```
$ printf 'docs/CODEOWNERS\n' | ./scripts/pr-gate-paths.sh
exempt: all 1 changed file(s) matched an exempt pattern
rc=0
```

Concrete failure scenario: a contributor opens a PR whose only change is adding
`docs/CODEOWNERS` assigning themselves as owner of `internal/**`. The Issue Gate reports
GREEN (path-exempt, no issue required), and GitHub then honors that file for review
routing. The stated rationale for the carve-out — "changing review ownership is a
governance decision" (`Taskfile.yaml:127-128`, `scripts/pr-gate-paths.sh:145-146`,
`CONTRIBUTING.md:182-183`) — is defeated by the one path the code was never told about.

Worse, `scripts/tests/pr-gate-paths.bats:99-102` **pins the hole as correct behavior**:

```bats
@test "docs/CODEOWNERS is exempt — the carve-out is path-exact, not a name match" {
  run gate "docs/CODEOWNERS"
  [ "$status" -eq 0 ]
}
```

so a future fix will read as a test regression.

**Fix:** add the third location to the positive query and invert the test.

```sh
# scripts/pr-gate-paths.sh:147 — GitHub honors CODEOWNERS at root, .github/, AND docs/.
owners="$(git -C "$scratch" -c core.quotePath=false ls-files -- \
  ':(glob).github/CODEOWNERS' ':(glob)docs/CODEOWNERS' 'CODEOWNERS')"
```

```bats
# scripts/tests/pr-gate-paths.bats — replace the exempt assertion
@test "docs/CODEOWNERS is never exempt — GitHub honors docs/ as a CODEOWNERS location" {
  run gate "docs/CODEOWNERS"
  [ "$status" -eq 10 ]
  [[ "$output" == *"CODEOWNERS"* ]]
}

@test "docs/CODEOWNERS hidden among exempt docs files is still not exempt" {
  run gate "docs/CODEOWNERS" "docs/guide.md"
  [ "$status" -eq 10 ]
}
```

The prose in `CONTRIBUTING.md:181-183` and `Taskfile.yaml:127-128` says "inside that tree"
/ "a root-level CODEOWNERS" and must be widened to name all three locations too.

---

### CR-02: The truncation guard cannot fire on a non-numeric `declared` — the gate goes green with an unvalidated file list

**File:** `.github/workflows/issue-gate.yaml:126-133`

**Issue:** The guard's own comment states it will "fail LOUD on any mismatch" because "a
silently under-reported list would make a large PR look exempt — failing in the permissive
direction, which is the one failure direction this gate must never have." It does not do
that. `[ "$declared" -ne "$collected" ]` with a non-integer `declared` exits **2** with a
diagnostic on stderr, and because the test is the condition of an `if`, `set -e` is
suspended and status 2 is simply treated as false — the error branch is skipped and the
step continues to `echo "list=$out"` and **succeeds**:

```
$ bash -c 'set -euo pipefail; declared="null"; collected="3";
  if [ "$declared" -ne "$collected" ]; then echo TRIPPED; else echo "NOT TRIPPED"; fi'
bash: line 1: [: null: integer expected
NOT TRIPPED
$ echo $?
0
```

Reachability: `gh api "repos/$REPO/pulls/$PR" --jq '.changed_files'` yields the string
`null` whenever the field is absent from an HTTP-200 body (schema change, a proxied/cached
response, an unexpected object shape). The guard is then a no-op, and if `$out` is also
short or empty the matcher returns `exempt: empty changed-file list` (exit 0,
`scripts/pr-gate-paths.sh:80-83`) and the check reports **GREEN**. That is precisely the
"malfunction reads as an exemption" defect the three-valued exit code exists to prevent —
reintroduced one step upstream of it.

**Fix:** validate both operands as integers before comparing, and fail on anything else.

```sh
declared="$(gh api "repos/$REPO/pulls/$PR" --jq '.changed_files')"
collected="$(wc -l < "$out" | tr -d ' ')"
echo "changed files: declared=$declared collected=$collected"
case "$declared" in
  ''|*[!0-9]*)
    echo "ERROR: PR reported a non-numeric changed_files ('$declared')." >&2
    echo "Refusing to evaluate the gate against an unverifiable diff." >&2
    exit 1
    ;;
esac
if [ "$declared" -ne "$collected" ]; then
  echo "ERROR: changed-file list is incomplete (declared $declared, collected $collected)." >&2
  exit 1
fi
```

A bats or workflow-level regression test for this branch does not exist and should.

---

## Warnings

### WR-01: The awk block-scalar extractor silently truncates at a blank line

**File:** `scripts/pr-gate-paths.sh:47-55`

**Issue:** `inblock { exit }` is reached by any line that is neither a 4-space comment nor
4-space content — including a **blank line**, which YAML permits inside a `|` block scalar.
Every glob after the blank line is silently dropped. Reproduced with a fixture Taskfile
containing one blank line inside `DOCS_ONLY_PATHS`:

```
$ printf 'docs/x.md\n' | REPO_ROOT=/tmp/awkfix ./scripts/pr-gate-paths.sh
not exempt: 1 file(s) outside the exempt path sets
docs/x.md
rc=10
```

`docs/**` and `**/*.md` were both dropped and a plainly documentation-only diff was flagged
as a violation. The direction is fail-closed (false reds, not false greens), which is why
this is a WARNING and not a BLOCKER — but it is silent, the review priorities explicitly
asked whether the extractor "can mis-parse a legitimate future Taskfile edit," and adding a
blank line to group globs is an entirely ordinary edit. `task lint:docs-paths-sync` will not
catch it because that gate compares var text against workflow text, not against what awk
extracts.

**Fix:** skip blank lines instead of terminating on them, and terminate only on dedent.

```awk
$0 ~ "^  " key ": \\|$" { inblock=1; next }
inblock && /^[[:space:]]*$/ { next }     # blank lines are legal inside a block scalar
inblock && /^    #/         { next }
inblock && /^    [^ ]/      { sub(/^    /, ""); sub(/[[:space:]]+$/, ""); print; next }
inblock                     { exit }
```

Add a bats case using a fixture Taskfile with an interior blank line asserting exit 0.

### WR-02: Attacker-controlled filenames break out of the fenced code block in the bot comment

**File:** `.github/workflows/issue-gate.yaml:301-307`

**Issue:** The verdict file — which contains PR-authored path strings verbatim — is emitted
between two ` ``` ` fences. A file whose name is literally three backticks closes the fence
early, and everything after it renders as live markdown/HTML in a maintainer-facing comment
posted by `github-actions[bot]`:

```
$ printf 'internal/a.go\n```\n' | ./scripts/pr-gate-paths.sh
not exempt: 2 file(s) outside the exempt path sets
```
internal/a.go
```

Git sorts the backtick entry first, so it lands on the first content line inside the fence.
A fork PR author needs no permissions at all to do this: add a file, get a bot-authored
comment that renders arbitrary attacker markdown (fake "gate satisfied" text, images,
links). Not code execution, but it is content spoofing on a trust-carrying surface.

**Fix:** use a fence longer than any possible filename fragment, or strip fence characters:

```sh
echo '~~~~'
head -n 21 "$RUNNER_TEMP/path-verdict.txt" | tr -d '`'
...
echo '~~~~'
```

### WR-03: The comment upsert can be hijacked — the marker search does not filter by author

**File:** `.github/workflows/issue-gate.yaml:324-325`

**Issue:**

```sh
existing="$(gh api "repos/$REPO/issues/$PR/comments" --paginate \
  --jq '[.[] | select(.body | contains("<!-- holomush:issue-gate -->")) | .id] | .[0] // empty')"
```

Comments come back in ascending creation order and `.[0]` takes the first. A PR author can
post the marker in their own comment before the workflow first runs; the gate then PATCHes
that comment instead of posting its own. The `GITHUB_TOKEN` has repo write scope and can
edit another user's comment, so this succeeds silently. Result: the explanation is buried
inside a user-authored comment (or, on a subsequent run, an author-edited one), while the
gate believes it upserted correctly. The check still goes red, so this is not a merge
bypass — it is a reliability/spoofing problem.

**Fix:** constrain the search to the bot's own comments.

```sh
existing="$(gh api "repos/$REPO/issues/$PR/comments" --paginate \
  --jq '[.[] | select(.user.login == "github-actions[bot]")
             | select(.body | startswith("<!-- holomush:issue-gate -->")) | .id] | .[0] // empty')"
```

### WR-04: No `concurrency` group — racing runs can leave label and check disagreeing

**File:** `.github/workflows/issue-gate.yaml:60-72`

**Issue:** The workflow fires on `synchronize` and `edited`, both of which trivially arrive
in bursts (push, then immediately edit the body to add `Closes #N`). Two runs then race on
`gh pr edit --add-label` / `--remove-label` and on the comment upsert. The
read-then-write upsert at :324-331 has no atomicity, so two concurrent runs both see
`existing=""` and both POST — producing the duplicate-comment wall the upsert exists to
prevent. Worse ordering: a stale FAIL run finishing after a fresh PASS run re-applies
`gate-violation` to a PR whose check is green. The repo's only precedent
(`deploy.yaml:32`) shows the pattern is already understood here.

**Fix:**

```yaml
concurrency:
  group: issue-gate-${{ github.event.pull_request.number }}
  cancel-in-progress: true
```

### WR-05: `dirname "$p"` without `--` errors on any root-level path beginning with `-`

**File:** `scripts/pr-gate-paths.sh:129`

**Issue:** `mkdir -p "$scratch/$(dirname "$p")"` passes the raw path as the first argument.
A legal repository file named `-e` (or `-n`, `--help`, …) is consumed as an option:

```
$ printf -- '-e\n' | ./scripts/pr-gate-paths.sh
dirname: illegal option -- e
usage: dirname string [...]
not exempt: 1 file(s) outside the exempt path sets
-e
rc=10
```

Here the verdict happens to be right, but the diagnostic is not contained: the workflow
merges stderr into the verdict file (`issue-gate.yaml:147`, `2>&1`) and then pastes the
first 21 lines of that file into the public PR comment (:302). A contributor sees
`dirname: illegal option` in the bot's explanation. And a nested variant (`-e/foo.go`)
takes a different branch: `mkdir` silently creates `$scratch/`, the subsequent
`: > "$scratch/-e/foo.go"` fails on the missing directory, and `set -e` aborts with exit 1
— a hard ERROR verdict for an ordinary filename.

**Fix:** `mkdir -p "$scratch/$(dirname -- "$p")"`.

### WR-06: The suspicious-path guard misses a bare `..` component

**File:** `scripts/pr-gate-paths.sh:67-72`

**Issue:** The `case` arms are `/*`, `../*`, `*/../*`, `*/..`. A line consisting of exactly
`..` matches none of them (there is no slash to anchor on) and reaches the materializer:

```
$ printf '..\n' | ./scripts/pr-gate-paths.sh
./scripts/pr-gate-paths.sh: line 130: /var/folders/.../tmp.Mr58YcLUgk/..: Is a directory
rc=1
```

It exits 1 only because `$scratch/..` happens to be a directory and `: >` refuses to
truncate it — the guard did not catch it, the filesystem did. The stated intent at :63-66
("fail loud rather than write outside the scratch directory") is not met by the code, and
the guard is one arm short of meeting it.

**Fix:** add the bare form.

```sh
case "$line" in
  /* | .. | ../* | */../* | */..)
```

Add a bats case: `run gate ".."` → `[ "$status" -eq 1 ]` and output contains
`refusing to materialize`.

### WR-07: `converted_to_draft` turns the check green but leaves the `gate-violation` label

**File:** `.github/workflows/issue-gate.yaml:253-277`

**Issue:** Every real step is gated on `github.event.pull_request.draft != true`, including
`Apply verdict`. Converting a violating PR to draft therefore produces a green check while
the `gate-violation` label and the failure comment both remain. The workflow header at
:25-28 explicitly frames `converted_to_draft` as "gets its check turned green again rather
than left red" — the label half of the verdict is not reverted with it, so a PR can sit
labeled `gate-violation` with a passing Issue Gate. That combination will be read as a bug
by anyone triaging the label.

**Fix:** run the stale-label cleanup unconditionally on drafts — either extend the `Skip
drafts` step to remove the label, or add a `Clear draft verdict` step with
`if: github.event.pull_request.draft == true` containing the same guarded
`--remove-label` block from :270-275.

### WR-08: GraphQL page limits produce unattributable false reds

**File:** `.github/workflows/issue-gate.yaml:198` and `:203`

**Issue:** `closingIssuesReferences(first: 20)` and `labels(first: 50)` are hard caps with
no `pageInfo.hasNextPage` check. A PR that closes 25 issues where only the 25th carries the
gate label is flagged as a violation, and the comment tells the author to "link an issue
… with a closing keyword" — advice that will not fix anything. The `Collect changed files`
step models the right behavior (compare against a declared total, fail loud on mismatch);
this step does not. Direction is fail-closed, so it is not a bypass, but it is an
unattributable failure of exactly the shape the header at :34-35 says the design is trying
to avoid.

**Fix:** request `pageInfo { hasNextPage }` on both connections and `exit 1` with an
explicit "too many linked issues / labels to evaluate reliably" message when either is true.

### WR-09: The `'CODEOWNERS'` positive pathspec is entirely untested

**File:** `scripts/tests/pr-gate-paths.bats:94-97`, guarding `scripts/pr-gate-paths.sh:147`

**Issue:** The review brief asked whether any assertion can pass vacuously. This one does.

```bats
@test "root CODEOWNERS alone is never exempt" {
  run gate "CODEOWNERS"
  [ "$status" -eq 10 ]
}
```

A root `CODEOWNERS` matches no glob in any of the three vars, so query 1 already returns
it. Deleting the `'CODEOWNERS'` term from :147 leaves this test green — verified by
running a patched copy with the whole `owners` query stubbed to empty:

```
$ printf 'CODEOWNERS\n' | REPO_ROOT="$PWD" /tmp/noowners.sh
not exempt: 1 file(s) outside the exempt path sets
CODEOWNERS
rc=10                       # test still passes with the carve-out removed
$ printf '.github/CODEOWNERS\n' | REPO_ROOT="$PWD" /tmp/noowners.sh
exempt: all 1 changed file(s) matched an exempt pattern
rc=0                        # the .github/ case IS a genuine pin
```

The `.github/CODEOWNERS` cases at :79-92 are real pins; the root case is decoration.

**Fix:** either assert the message that only the positive query emits, or drop the claim:

```bats
@test "root CODEOWNERS alone is never exempt" {
  run gate "CODEOWNERS"
  [ "$status" -eq 10 ]
  [[ "$output" == *"CODEOWNERS is never exempt"* ]]
}
```

### WR-10: `.github/**` exempts the gate's own enforcement workflow

**File:** `Taskfile.yaml:136-137`, consumed by `scripts/pr-gate-paths.sh:100`

**Issue:** `REPO_CONFIG_ONLY_PATHS: .github/**` makes every workflow file path-exempt,
including `.github/workflows/issue-gate.yaml` itself and every required-check workflow
(`ci.yaml`, `commit-lint.yaml`). A PR that deletes or neuters the Issue Gate reports GREEN
from the very gate it removes, with no linked issue required. That is a strictly stronger
governance decision than the CODEOWNERS case which *does* get a carve-out
(`Taskfile.yaml:127-128`).

This mechanizes pre-existing documented policy (`CONTRIBUTING.md:180-183` already listed
"workflows" as exempt) rather than inventing it, which is why it is a WARNING and not a
BLOCKER — but the policy was prose until this commit and is now load-bearing, so this is
the moment to decide it deliberately.

**Fix:** either extend the positive-query carve-out at `scripts/pr-gate-paths.sh:147` to
cover `.github/workflows/**` alongside CODEOWNERS, or record an explicit decision in
`Taskfile.yaml:114-135` that self-exemption of the enforcement surface is accepted and why.

### WR-11: `permissions: pull-requests: write` may be insufficient for the issue-comment endpoints, and cannot be validated before merge

**File:** `.github/workflows/issue-gate.yaml:52-58`, used at `:270`, `:324`, `:327`, `:330`

**Issue:** Three calls target the `issues` REST surface —
`GET repos/{r}/issues/{n}/labels`, `POST repos/{r}/issues/{n}/comments`,
`PATCH repos/{r}/issues/comments/{id}` — while only `pull-requests: write` is granted.
GitHub documents these endpoints under the **Issues** permission; the PR-scoped mapping
generally works but is not contractual, and `PATCH /issues/comments/{id}` is the least
certain of the three because its path is not PR-scoped at all. The workflow deliberately
carries no `|| true` (:281-283), so a 403 fails the step — fail-closed, correct direction —
but the observable result is a red `Issue Gate` with **no label and no comment**, which the
header at :283 itself names as "the same unattributable-failure shape as #4878".

This cannot be smoke-tested before merge: `pull_request_target` sources the workflow from
the default branch (:12-15), so the first live execution is on `main` against a real
contributor's PR.

**Fix:** add `issues: write` to the `permissions:` block. It is not a meaningful escalation
(the job already has PR write) and it removes the ambiguity on all three calls. Then, per
`#4895`, exercise the workflow once against a scratch PR before adding the context to
ruleset 11923801.

---

## Info

No Info-severity findings. The documentation changes (`CONTRIBUTING.md`,
`.github/PULL_REQUEST_TEMPLATE.md`, the four typed templates) are accurate against the
implemented behavior — including the non-obvious claim that the check "never closes the
PR", which the workflow honors — and the `scripts-tests.yaml` trigger addition correctly
re-runs the bats suite when `issue-gate.yaml` changes. Note that WR-10's `.github/**`
exemption means that trigger addition is itself in a path-exempt tree.

---

_Reviewed: 2026-07-31T16:25:45Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: quick_
