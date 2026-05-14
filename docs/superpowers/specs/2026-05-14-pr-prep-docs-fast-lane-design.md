# `pr-prep` + CI Docs-Only Fast Lane

## Status

Draft, revision 3 (2026-05-14). Addresses design-reviewer r2 blocking finding (go-task `exec` does not short-circuit subsequent `- cmd:` entries) + non-blocking r2 findings.

Tracking beads:

- Epic: `holomush-yp2t` — CI pipeline streamlining + `pr-prep` optimization
- `holomush-yp2t.1` — CI workflow `paths-ignore` + same-name skip workflow
- `holomush-yp2t.2` — `pr-prep` docs-only auto-detect + `pr-prep:docs` target

Adjacent (not in scope here, referenced for context):

- `holomush-ceon` (P1) — `Cmd+K opens palette` E2E flake; root-caused, fix in progress
- `holomush-35y6` (P1) — rumdl local↔CI version alignment
- `holomush-h8xj` / `holomush-qeiu` / `holomush-rmo2` — `go.work` multi-workspace corruption

### Revision notes

- **r3 (2026-05-14)** — fixes blocking r2 defect: r2's §4.3.1 used two `- cmd:` entries (one for detection, one for the existing flock body). go-task spawns each `- cmd:` in its own subshell, so the docs-lane `exec` only replaces the FIRST subshell — go-task then ran the second `- cmd:` (full lane) anyway. r3 collapses to a single `- cmd:` block with the detection logic prepended to the existing flock body inline. Adds critical regression-guard assertion in §4.7.1 (flock lock file MUST NOT appear when docs lane is selected). Acknowledges `**/*.md` edge case in `.github/**` / `.claude/hooks/**` (§4.1). Fixes §4.4.3 "all five" wording and §7 R-3 cross-reference.
- **r2 (2026-05-14)** — replaced r1's `CI Required` `workflow_run` aggregator (unworkable; `workflow_run` does not fire for path-skipped upstream) with the GitHub-canonical **same-name skip workflow** pattern. Rollout simplified to a single PR with no branch-protection settings changes. Dropped r1's INV-2 perf claim (untestable as written) to a non-binding goal in §2. Pinned current required-check list. Added `addlicense` pre-install in `task setup`. Tightened regex compilation, INV-6 rationale, and jj-snapshot semantics per review.

## 1. Problem

Both pre-push gates run the full pipeline regardless of what changed:

| Gate                              | Full-lane cost                              | Docs-only diff cost today |
| --------------------------------- | ------------------------------------------- | ------------------------- |
| `.github/workflows/ci.yaml`       | lint + test + integration + e2e + build, ~8-10 min wall, ~5× compute | Same (full pipeline) |
| `task pr-prep` (local pre-push)   | schema regen + plugin builds + lint + unit + integration + e2e, ~3-5 min healthy, 5-15 min on retry loops | Same (full pipeline) |

**Quantified waste** (30-day audit, 2026-04-14 → 2026-05-14):

- 8 docs-only PRs ran full CI: `#3832, #3831, #3677, #3538, #3528, #3028, #2365, #236`. Estimated ~75 wasted Namespace runner-minutes.
- 84% of Claude transcripts (74 of 88 sessions) reference `task pr-prep` at least once. 184 distinct `Failed to run task "pr-prep"` events captured.
- Most docs PRs need only markdown lint + format check. Today they pay schema regen + plugin compile + Go test suites + Playwright E2E.

`.github/workflows/site.yml:6-12` already demonstrates the `paths:` pattern for the inverse case (zensical site builds only on `site/**` changes). The asymmetry — `site.yml` is path-aware but `ci.yaml` is not — is the structural root cause on the CI side.

### Current branch-protection state

Captured 2026-05-14 via `gh api repos/holomush/holomush/rules/branches/main`:

- Required status checks: **`Build`, `Lint`, `Test`, `CodeRabbit`** (4 entries, integration_id 15368 for the first three, 347564 for CodeRabbit).
- **Not required:** `Integration Test`, `E2E Test` (CI workflow jobs that run but are not gating), `Buf Proto CI`, `Deploy Site`, `Scripts Tests` (separate path-filtered workflows).
- PR rules: `required_approving_review_count: 0`, `dismiss_stale_reviews_on_push: true`, `allowed_merge_methods: [squash]`.

The required-check list determines exactly which job names the same-name skip workflow (§4.2.2) MUST cover: `Build`, `Lint`, `Test`. `CodeRabbit` is the CodeRabbit GitHub App (integration_id 347564) and runs independently of these workflows — unaffected.

## 2. Goal

Make both gates **path-aware** by consulting an identical `DOCS_ONLY_PATHS` glob list. Symmetric semantics by construction: a diff classified as docs-only locally is also classified as docs-only by CI.

Success criteria:

- A docs-only PR does not invoke the `CI` workflow's lint/test/integration/e2e/build jobs.
- Required branch-protection checks (`Build`, `Lint`, `Test`) remain green on docs-only PRs (no "checks haven't completed" stall).
- Any non-docs path in the diff routes both gates to the full pipeline.
- Drift between the CI glob list and the Taskfile glob list is caught at lint time, not by symptom.
- **Non-binding performance goal:** `task pr-prep` on a docs-only diff should typically complete in well under a minute on a developer machine that has run `task setup` at least once. Not a tested invariant; surfaced because the bound is what the design is optimizing toward.

## 3. Invariants (RFC2119)

- **INV-1.** The `DOCS_ONLY_PATHS` glob list **MUST** be byte-identical between `.github/workflows/ci.yaml`'s `paths-ignore:` block, `.github/workflows/ci-docs-skip.yaml`'s `paths:` block, and `Taskfile.yaml`'s `DOCS_ONLY_PATHS` variable, enforced by `task lint:docs-paths-sync` running in CI's lint job.
- **INV-2.** `task pr-prep` on any diff containing one or more non-docs paths **MUST** run the full `pr-prep:run` lane.
- **INV-3.** `HOLOMUSH_PR_PREP_FORCE_FULL=1 task pr-prep` **MUST** run the full lane unconditionally, regardless of diff classification.
- **INV-4.** On detection failure — empty diff, missing `origin/main`, `git fetch` failure, or any non-zero exit from the diff command — `task pr-prep` **MUST** fall back to the full lane.
- **INV-5.** Required branch-protection checks (currently `Build`, `Lint`, `Test`; see §1) **MUST** report a green status on docs-only PRs via the `ci-docs-skip.yaml` workflow, with each check name byte-identical to its full-lane counterpart so GitHub treats them as the same required check.
- **INV-6.** The docs lane **MUST NOT** acquire the `pr-prep` flock. Rationale: the docs lane invokes only read-only static-file operations (`rumdl check`, `yamlfmt -lint`, `dprint check`, `addlicense -check`, `awk`-based docs-symmetry diff) on files that are not mutated by any concurrent full-lane build step. Concurrent docs+full lanes on the same workspace are safe.

## 4. Design

### 4.1 Canonical path list

```text
site/**
docs/**
**/*.md
.claude/agents/**
.claude/commands/**
.claude/rules/**
.claude/agent-memory/**
LICENSE
LICENSE_HEADER
```

**Included** (rationale):

- `site/**`, `docs/**` — documentation source trees by definition.
- `**/*.md` — any markdown anywhere in the tree, including root-level `README.md`, `CLAUDE.md`, `AGENTS.md`, plus subtree `*.md` (e.g., `web/CLAUDE.md`).
- `.claude/agents/**`, `.claude/commands/**`, `.claude/rules/**`, `.claude/agent-memory/**` — agent prose / rules / commands; affect AI behavior, not build / runtime.
- `LICENSE`, `LICENSE_HEADER` — license boilerplate.

**Excluded** (load-bearing exclusions):

- `.claude/hooks/**` — shell hooks; can break CI / git flows if broken.
- `.claude/settings*.json` — Claude harness behavior; integrity-relevant.
- `.github/**` — workflow and dependabot configuration; changes here MUST run the full lane (the lane that validates `ci.yaml` itself).

**Edge case acknowledgment.** The `**/*.md` include matches markdown files anywhere in the tree, including inside otherwise-excluded directories — e.g., `.github/PULL_REQUEST_TEMPLATE.md`, `.github/CODEOWNERS.md`, `.claude/hooks/README.md`. These cases are intentionally classified as docs-only: edits to a workflow's documentation prose do not need the full pipeline. The directory-exclude prose above governs non-`.md` files inside those directories (e.g., `.github/workflows/ci.yaml`, `.claude/hooks/pre-commit.sh`).

### 4.2 CI side — `paths-ignore` + same-name skip workflow

#### 4.2.1 `paths-ignore` on the `CI` workflow

Edit `.github/workflows/ci.yaml:3-8`:

```yaml
on:
  push:
    branches: [main]
    paths-ignore:
      - site/**
      - docs/**
      - "**/*.md"
      - .claude/agents/**
      - .claude/commands/**
      - .claude/rules/**
      - .claude/agent-memory/**
      - LICENSE
      - LICENSE_HEADER
  pull_request:
    branches: [main]
    paths-ignore:
      - site/**
      - docs/**
      - "**/*.md"
      - .claude/agents/**
      - .claude/commands/**
      - .claude/rules/**
      - .claude/agent-memory/**
      - LICENSE
      - LICENSE_HEADER
  workflow_dispatch:
```

#### 4.2.2 Same-name skip workflow

When `ci.yaml` is path-ignored, GitHub leaves its required checks in a `Pending` state forever — branch protection blocks merge ([GitHub docs: handling skipped but required checks](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/required-status-checks#handling-skipped-but-required-checks)). The canonical mitigation is a **same-name skip workflow**: a separate workflow with the inverse path filter, defining jobs whose `name:` strings are byte-identical to the gated checks. GitHub treats two jobs with the same name as the same required check; whichever one runs reports the status.

New file `.github/workflows/ci-docs-skip.yaml`:

```yaml
name: CI

# Runs on docs-only PRs (inverse of ci.yaml's paths-ignore).
# Provides no-op jobs whose names match ci.yaml's required-check job names,
# so branch-protection required checks (Build, Lint, Test) report green
# without invoking the full pipeline.
#
# DOCS_ONLY_PATHS — must remain byte-identical with ci.yaml:paths-ignore
# and Taskfile.yaml:DOCS_ONLY_PATHS. Enforced by task lint:docs-paths-sync.

on:
  push:
    branches: [main]
    paths:
      - site/**
      - docs/**
      - "**/*.md"
      - .claude/agents/**
      - .claude/commands/**
      - .claude/rules/**
      - .claude/agent-memory/**
      - LICENSE
      - LICENSE_HEADER
  pull_request:
    branches: [main]
    paths:
      - site/**
      - docs/**
      - "**/*.md"
      - .claude/agents/**
      - .claude/commands/**
      - .claude/rules/**
      - .claude/agent-memory/**
      - LICENSE
      - LICENSE_HEADER

permissions:
  contents: read

jobs:
  Lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
      - run: echo "docs-only PR — Lint skipped (no code changes)"
  Test:
    name: Test
    runs-on: ubuntu-latest
    steps:
      - run: echo "docs-only PR — Test skipped (no code changes)"
  Build:
    name: Build
    runs-on: ubuntu-latest
    steps:
      - run: echo "docs-only PR — Build skipped (no code changes)"
```

Notes:

- The `name:` workflow header is `CI` (intentionally identical to `ci.yaml`'s `name: CI`). GitHub branch-protection check identity is per `(workflow_name, job_name)` — matching both is required.
- `runs-on: ubuntu-latest` (NOT `namespace-profile-linux-amd64-4x8`) because the jobs do nothing; the GitHub-hosted public runner is faster to start for a single `echo`.
- `Integration Test` and `E2E Test` are NOT in the required-check list (§1) and therefore NOT in this file — skipping them on docs PRs is correct.
- No branch-protection settings changes required; the existing required-check list (`Build`, `Lint`, `Test`, `CodeRabbit`) keeps working.

### 4.3 `pr-prep` side — detection + `pr-prep:docs` target

#### 4.3.1 Detection step

**Critical structural constraint:** go-task spawns each `- cmd:` block in its own subshell. `exec` inside one subshell replaces only that subshell — it does NOT prevent go-task from running the NEXT `- cmd:` entry. Similarly, `exit 0` in cmd1 still allows cmd2 to run; only non-zero exits abort the task. This means the detection step CANNOT be a separate `- cmd:` entry before the existing flock body — the docs lane would `exec` correctly, then go-task would still run the full lane immediately afterward.

Resolution: the detection logic and the existing flock body MUST live in the **same** `- cmd:` block, so that `exec task pr-prep:docs` replaces the same subshell that would otherwise have reached the flock body. Edit `Taskfile.yaml:534-563` to PREPEND the detection logic inside the existing single `cmd:` block:

```yaml
pr-prep:
  desc: ...   # unchanged
  preconditions:
    - sh: command -v flock >/dev/null 2>&1
      msg: "flock(1) is required..."   # unchanged
  cmds:
    - cmd: |
        set -euo pipefail

        # NEW — docs-only detection. Lives in the SAME subshell as the flock
        # body below, so `exec` correctly short-circuits the full lane.
        if [ "${HOLOMUSH_PR_PREP_FORCE_FULL:-}" != "1" ]; then
          git fetch -q origin main 2>/dev/null || true
          CHANGED=$(git diff --name-only origin/main...HEAD 2>/dev/null || true)
          if [ -n "$CHANGED" ]; then
            DOCS_REGEX=$(bash scripts/docs-paths-regex.sh)
            if ! printf '%s\n' "$CHANGED" | grep -vE "$DOCS_REGEX" >/dev/null; then
              echo "▸ docs-only diff detected; running pr-prep:docs"
              exec task pr-prep:docs
            fi
          fi
        fi

        # EXISTING — flock-wrapped full lane, inlined verbatim from
        # Taskfile.yaml:535-562 (current commit). No structural changes.
        LOCK_DIR="${TMPDIR:-/tmp}/holomush-pr-prep"
        mkdir -p "$LOCK_DIR"
        LOCK="$LOCK_DIR/lock"
        INFO="$LOCK_DIR/info"
        rc=0
        flock -n -E 75 "$LOCK" sh -c '
          export HOLOMUSH_PR_PREP_BYPASS_LOCK=1
          printf "pid=%s\nworkspace=%s\nstarted_at=%s\n" \
            "$$" "$2" "$(date -u +%FT%TZ)" > "$1"
          exec task pr-prep:run
        ' _ "$INFO" "$PWD" || rc=$?
        # ...rest of existing body (collision message + non-zero handling)
        # unchanged at Taskfile.yaml:547-562.
```

**Scope acknowledgment** (per design-reviewer R5.1): the `git fetch -q origin main` runs on every `task pr-prep` invocation, including full-lane runs that would have skipped fetch in r1. On a warm clone this is sub-second; on a cold / slow network it can take longer, with the `|| true` swallowing errors. This is a deliberate behavior change to the full lane — not an optimization, but a precondition for correct docs-only detection. Acknowledged as in-spec, not out-of-scope.

Justification for `git` (not `jj`) in detection:

- The repo is jj-colocated. After any `jj` command, git HEAD is auto-synced to reflect `@`'s parent / current snapshot.
- `git` is universally available; no jj-native detection needed.
- `origin/main...HEAD` (three-dot) returns the merge-base diff.

**jj snapshot caveat:** detection uses git's view, which reflects jj's last auto-snapshot. Uncommitted edits made between jj commands (i.e., the user edits a file, then runs `task pr-prep` without any intervening `jj` invocation) may be invisible to detection — those edits exist on disk but are not yet in `@`. Workaround: run any read-only `jj` command (e.g., `jj st`) before `task pr-prep` to force a snapshot. Documented in `site/docs/contributing/pr-prep.md`. Risk is low because most workflows interleave `jj describe` / `jj commit` before push.

Detection-failure handling:

- `git fetch` failure → swallowed; fall through with cached `origin/main`.
- `git diff` non-zero exit → `CHANGED` is empty → outer `if [ -n "$CHANGED" ]` is false → fall through to full lane (INV-4).
- Empty diff (rebased, no new commits) → same fall-through.
- Missing `origin/main` (fresh clone, no fetch yet) → `git diff` errors, empty `CHANGED`, full lane.

#### 4.3.2 `pr-prep:docs` target

Inserted in `Taskfile.yaml` after `pr-prep:run`:

```yaml
pr-prep:docs:
  desc: Docs-only fast lane (auto-selected by pr-prep on docs-only diffs)
  cmds:
    - echo "▸ Running docs-only fast lane"
    - task: lint:markdown
    - task: lint:yaml
    - task: lint:docs-symmetry
    - task: fmt:check
    - task: license:check
    - task: lint:docs-paths-sync
    - echo "✓ Docs lane passed."
```

`lint:docs-paths-sync` is included so meta-edits to the glob list (a docs-only PR that touches `Taskfile.yaml`'s `DOCS_ONLY_PATHS` is path-ignored by CI's lint job, but still classified as docs-only locally) are still validated. This is the only edit pattern where the docs lane validates itself.

No flock (INV-6).

### 4.4 Sync enforcement — `lint:docs-paths-sync` + regex helper

#### 4.4.1 Storage of `DOCS_ONLY_PATHS`

In `Taskfile.yaml`, as a top-level `vars:` heredoc:

```yaml
vars:
  DOCS_ONLY_PATHS: |
    site/**
    docs/**
    **/*.md
    .claude/agents/**
    .claude/commands/**
    .claude/rules/**
    .claude/agent-memory/**
    LICENSE
    LICENSE_HEADER
```

One canonical location. The detection helper and the sync lint both read from this one place; `ci.yaml` + `ci-docs-skip.yaml` mirror it inline.

#### 4.4.2 `scripts/docs-paths-regex.sh`

Compiles the glob list to an extended regex used by `grep -vE`. Output is a single regex string that matches a path iff it matches any glob.

Worked example. Given the §4.1 glob list, the helper emits (one possible canonical form):

```text
^(site/.*|docs/.*|.*\.md|\.claude/agents/.*|\.claude/commands/.*|\.claude/rules/.*|\.claude/agent-memory/.*|LICENSE|LICENSE_HEADER)$
```

Compilation rules (the helper implements these mechanically):

- `**` at end of segment + `/...` → `.*` (matches any depth + any file).
- `**` mid-pattern (rare; not used in §4.1) → unsupported; helper exits non-zero.
- `**/*.md` → `.*\.md` (any path ending in `.md`, including root and nested).
- Literal `.` → `\.`.
- Literal directory names → unchanged.
- All alternatives joined with `|`, wrapped in `^(...)$`.

Tested via `scripts/tests/docs-paths-regex.bats` (table: input glob → expected regex → sample paths that match / don't match).

#### 4.4.3 `task lint:docs-paths-sync`

```yaml
lint:docs-paths-sync:
  desc: Verify DOCS_ONLY_PATHS list is identical across ci.yaml, ci-docs-skip.yaml, Taskfile.yaml.
  cmds:
    - bash scripts/lint-docs-paths-sync.sh
```

`scripts/lint-docs-paths-sync.sh`:

1. Extract the canonical list from `Taskfile.yaml`'s `vars.DOCS_ONLY_PATHS` via `yq`.
2. Extract `.on.pull_request.paths-ignore` and `.on.push.paths-ignore` from `.github/workflows/ci.yaml`.
3. Extract `.on.pull_request.paths` and `.on.push.paths` from `.github/workflows/ci-docs-skip.yaml`.
4. Compare the four mirror locations (ci.yaml push/pr `paths-ignore` × ci-docs-skip.yaml push/pr `paths`) against the canonical Taskfile var — five extraction points across three files. Fail with a clear diff message if any differ.

Wired into `task lint` (called from CI's `Lint` job at `ci.yaml:88` AND from `pr-prep:run`). Also wired directly into `pr-prep:docs` (§4.3.2).

### 4.5 `addlicense` pre-install in `task setup`

`license:check` invokes `addlicense -check`, and `license:run` (Taskfile.yaml:519) auto-runs `go install github.com/google/addlicense@latest` when the binary is missing. On a fresh checkout this triggers a Go compile, which contradicts the docs-lane's "no Go compilation" intent and slows the first `task pr-prep` on a new machine.

Fix: append to `task setup` (Taskfile.yaml:614-621):

```yaml
- go install github.com/google/addlicense@latest
```

Side-effect: `setup` becomes slightly slower (one-time). All subsequent `license:check` invocations are immediate.

### 4.6 Documentation updates

- `CLAUDE.md:243-249` and `AGENTS.md:243-249` — replace the strict "MUST run `task pr-prep` before any push" prose with: "`task pr-prep` is mandatory; it auto-selects the docs lane (typically under a minute) when the diff is documentation-only. Use `HOLOMUSH_PR_PREP_FORCE_FULL=1 task pr-prep` to force the full lane. Note: docs detection relies on jj's snapshot of `@`; run `jj st` first if you've made edits since the last `jj` command."
- `site/docs/contributing/pr-prep.md` — new section on the docs lane, escape hatch, the same-name skip workflow pattern, and the jj-snapshot caveat.

### 4.7 Tests

#### 4.7.1 `scripts/tests/pr-prep-docs-detection.bats`

Table-driven over `git diff --name-only` outputs, asserting lane selection. Concrete paths (NOT glob patterns):

| `git diff` lines                                | Expected lane |
| ----------------------------------------------- | ------------- |
| `site/docs/index.md`                            | docs          |
| `README.md`                                     | docs          |
| `web/CLAUDE.md`                                 | docs          |
| `docs/superpowers/specs/foo.md`                 | docs          |
| `.claude/agents/code-reviewer.md`               | docs          |
| `internal/foo/bar.go`                           | full          |
| `site/docs/index.md`<br>`internal/foo.go`       | full (mixed)  |
| `.claude/hooks/pre-commit.sh`                   | full          |
| `.claude/settings.json`                         | full          |
| `.github/workflows/ci.yaml`                     | full          |
| (empty)                                         | full          |
| (`git diff` exits non-zero)                     | full          |
| (env `HOLOMUSH_PR_PREP_FORCE_FULL=1`, any diff) | full (forced) |

Implemented as a bats suite that injects a `git diff` mock via PATH shim and verifies the `exec task pr-prep:docs` vs flock-cmd decision via stdout banner.

**Critical assertion** (regression guard against the r2 structural defect): on every docs-only input, the test MUST verify that the **flock body is NOT entered** — e.g., assert the lock file `/tmp/holomush-pr-prep/lock` is not created during the run, OR assert the "flock-acquired" / `pr-prep:run` stdout marker never appears. The docs banner alone is insufficient evidence; both lanes can print a banner if the cmd-block structure is wrong.

#### 4.7.2 `scripts/tests/lint-docs-paths-sync.bats`

- Synthetic drift between Taskfile and ci.yaml → fail with non-zero exit.
- Synthetic drift between ci.yaml and ci-docs-skip.yaml → fail.
- Synthetic match across all three → pass.
- Synthetic `push.paths-ignore` vs `pull_request.paths-ignore` drift inside `ci.yaml` → fail.

#### 4.7.3 `scripts/tests/docs-paths-regex.bats`

Glob → regex compilation correctness. Table: input glob → expected regex → sample matching / non-matching paths. Covers each glob shape used in §4.1.

#### 4.7.4 Manual integration test (one-time during rollout)

After the implementation PR merges:

1. Open a follow-up PR touching only one `*.md` file. Confirm:
   - `ci.yaml` does NOT run.
   - `ci-docs-skip.yaml` DOES run.
   - Branch protection shows `Build / Lint / Test` green.
   - Mergeable.
2. Open a follow-up PR touching one `*.go` file. Confirm:
   - `ci.yaml` DOES run (the full pipeline).
   - `ci-docs-skip.yaml` does NOT run.
   - Branch protection shows `Build / Lint / Test` reporting from `ci.yaml`.

Documented in `site/docs/contributing/pr-prep.md` for re-validation after future glob-list edits.

## 5. Out of scope

- Optimization of the full-lane `pr-prep:run` (parallelism, source-gating, step reordering) — tracked separately in epic siblings `holomush-yp2t.5` (plugin gating) and `holomush-yp2t.6` (hygiene).
- Reworking existing flock semantics or the `pr-prep:run` escape hatch — `holomush-yp2t.4`.
- Skipping CI Lint for docs-only when the docs lane already ran locally — CI is the authoritative gate.
- jj-native detection — git suffices in colocated.
- Cross-host serialization of `pr-prep`.
- Migrating `pr-prep` to a Go-native gate runner.
- Adding `Integration Test` / `E2E Test` to required checks (separate decision; today they are advisory).
- Path-aware behavior for `Buf Proto CI`, `Scripts Tests`, `Deploy Site` workflows. They already have `paths:` allow-list filters (§1 inventory), are NOT in the required-check list, and therefore don't need same-name skip workflows.

## 6. Open implementation choices

These do not affect the design's correctness and are deferred to the implementation plan.

### 6.1 `addlicense` install command in `setup`

§4.5 uses `go install ...@latest`. Alternative: pin a version (e.g., `@v1.2.0`) for build determinism. Marginal; planning's call.

### 6.2 Rollout in one PR vs split

The design supports either:

- **Single PR.** `ci.yaml` paths-ignore + `ci-docs-skip.yaml` + Taskfile changes + lint check + docs all in one PR. Simpler, atomic, exercises the design on its own merge.
- **Two PRs.** First PR: `ci-docs-skip.yaml` only (introduces same-name jobs; runs alongside existing CI on every PR until docs-only detection lands). Second PR: `ci.yaml` paths-ignore + Taskfile changes. Allows independent verification of each half.

Recommendation: single PR. Risk is bounded; both halves are cheap to revert if something breaks. The first PR's own merge tests the docs lane (it touches Taskfile, ci.yaml, *.md — a mixed diff → full lane, validating that path).

## 7. Risks

- **R-1 Same-name-job collision.** GitHub's check-identity rule treats `(workflow_name, job_name)` as the key. Both `ci.yaml` and `ci-docs-skip.yaml` use `name: CI` at workflow level, and the same `name: Lint`/`Test`/`Build` at job level. Since the two workflows have inverse path filters, only one fires per SHA — no collision in practice. Verified by GitHub docs reference in §4.2.2.
- **R-2 Stale `origin/main`** if the user's `git fetch` lags. Mitigation: detection performs a quiet fetch inside the body; on failure, falls back to full lane (INV-4). Worst case is a docs PR runs the full lane — degraded performance, not incorrectness.
- **R-3 Glob drift between detection and `paths-ignore`** despite `lint:docs-paths-sync`. The sync check runs in CI's lint job — but on a docs-only PR that *edits* the glob list, CI's lint job is itself path-skipped. Mitigation: `pr-prep:docs` (§4.3.2) runs `lint:docs-paths-sync` locally. Optional hardening: upgrade `ci-docs-skip.yaml`'s `Lint` no-op echo to actually run `task lint:docs-paths-sync` (cheap; same `name: Lint` still satisfies branch protection). Pin this decision in the plan; default for r2 is local-only via `pr-prep:docs`.
- **R-4 Glob compilation correctness.** `**/*.md` MUST compile to a regex matching `README.md` (root) AND `web/CLAUDE.md` (subtree). The helper `scripts/docs-paths-regex.sh` (§4.4.2) is the load-bearing piece; correctness is tested via §4.7.3.
- **R-5 jj-without-snapshot edits.** Documented caveat (§4.3.1) — edits made between jj commands are invisible to git diff. Risk is low; docs-only PRs typically go through `jj describe -m "..."` before pre-push.
