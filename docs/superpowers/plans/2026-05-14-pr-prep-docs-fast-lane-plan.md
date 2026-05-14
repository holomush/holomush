# `pr-prep` + CI Docs-Only Fast Lane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make both pre-push gates (`task pr-prep` and the `CI` GitHub Actions workflow) path-aware so docs-only PRs skip the full pipeline and complete in seconds rather than minutes.

**Architecture:** A single canonical `DOCS_ONLY_PATHS` glob list lives in `Taskfile.yaml`'s `vars:` block. It is mirrored inline into `.github/workflows/ci.yaml` (as `paths-ignore`) and a new `.github/workflows/ci-docs-skip.yaml` (as `paths`, same-name skip workflow per GitHub's canonical pattern). `task lint:docs-paths-sync` enforces byte-equivalence across all three. `task pr-prep` prepends a docs-only detection step inside its existing single `- cmd:` block; on a docs-only diff it `exec`s into a new lightweight `pr-prep:docs` task and skips the flock body.

**Tech Stack:** go-task, bats-core (shell tests), GitHub Actions, yq (YAML parsing), grep extended regex, jj (colocated repo).

**Spec:** `docs/superpowers/specs/2026-05-14-pr-prep-docs-fast-lane-design.md` (r3, READY)

**Tracking:**

- Epic: `holomush-yp2t`
- `holomush-yp2t.1` — CI `paths-ignore` + same-name skip workflow
- `holomush-yp2t.2` — `pr-prep` docs detection + `pr-prep:docs` target

Both beads close on this PR's merge.

## Plan revision notes

- **r2 (2026-05-14)** — addresses plan-reviewer r1 blocking findings: (1) `yq` returns literal `"null"` on missing keys with exit 0, so the `[ -n "$GLOBS" ]` guard fails open — switched to `yq -e` + explicit null-literal check, with bats coverage. (2) r1's Task 8 bats test invoked production `task pr-prep` against the real repo, causing recursion through `pr-prep:run → test:bats → same bats file` plus real lint side-effects. r2 rewrites Task 8 to extend the existing `scripts/tests/Taskfile.test.yaml` fixture pattern (mirrors `scripts/tests/pr-prep-lock.bats`), with separate markers for docs vs full lane and a git PATH-shim. (3) Reordered Tasks 3–5 so CI YAML files exist BEFORE the `lint:docs-paths-sync` wiring, eliminating the intermediate-commit `task lint` breakage. (4) Task 1's file-range edit reanchored on the existing `LICENSE_DIRS:` line to preserve the blank-line separator. (5) Existing `pr-prep-lock.bats` tests are made robust to the new detection prologue by setting `HOLOMUSH_PR_PREP_FORCE_FULL=1` in the shared test helper.

## Bead chain structure

Implementation is tracked under the two existing beads — `yp2t.1` and `yp2t.2` — with `yp2t.1` already gated on `yp2t.2` via `bd dep add` (verified via `bd show holomush-yp2t.1` earlier this session; the edge is in place). This plan does not require new task beads; the two existing beads describe the outcomes, and this plan's task list is the implementation breakdown executed under a single PR. The PR's merge closes both beads.

| Plan Task | Bead         | Notes |
|-----------|--------------|-------|
| 1–2, 5–10 | `yp2t.2`     | Taskfile changes, detection, regex helper, sync lint, docs, smoke |
| 3, 4      | `yp2t.1`     | `ci-docs-skip.yaml` + `ci.yaml` `paths-ignore` |
| All       | `yp2t` epic  | Parent rollup |

No supersessions. No follow-up beads created by this plan.

## Reviewer non-blocker decisions (pinned)

Plan-time decisions deferred by the spec (§6 + design-reviewer non-blocker list). Pinned here:

1. **`addlicense` install command in `setup`** — use `go install github.com/google/addlicense@latest` (matches existing `setup` style at `Taskfile.yaml:614-621`; no other tools in the project are pinned via version tag).
2. **Rollout shape** — single PR (spec §6.2 recommendation). All file edits land atomically.
3. **`ci-docs-skip.yaml` `Lint` job upgrade** — KEEP as `echo` no-op. Local `pr-prep:docs` includes `lint:docs-paths-sync` (Task 7); a docs-only PR that edits the glob list will fail `pr-prep:docs` on drift locally, so the user can't push it.
4. **§4.7.1 detection-test assertion** — use the **fixture-marker** form (lane-specific marker files via `STUB_MARKER`), NOT lock-file existence or production-stdout grep. Eliminates cross-session false positives AND recursion.
5. **`.github/PULL_REQUEST_TEMPLATE.md` test-table row** — included in Task 8's bats table.
6. **`timeout 5 git fetch ... || true` AND `timeout 5 git diff ...`** — both bounded per reviewer NB3 + NB5.

## File Structure

**Create:**

| Path | Responsibility |
|------|----------------|
| `.github/workflows/ci-docs-skip.yaml` | Same-name skip workflow with no-op `Lint`/`Test`/`Build` jobs that fire on docs-only diffs (inverse paths filter). Keeps branch-protection required checks green when `ci.yaml` is path-skipped. |
| `scripts/docs-paths-regex.sh` | Compiles `DOCS_ONLY_PATHS` (read from `Taskfile.yaml`) into a single extended-regex string for `grep -vE`. |
| `scripts/lint-docs-paths-sync.sh` | Verifies the glob list is byte-identical across the canonical Taskfile var + four mirror locations (ci.yaml push/pr × ci-docs-skip.yaml push/pr). |
| `scripts/tests/docs-paths-regex.bats` | Bats tests for the glob→regex helper, including the yq-null-handling failure path. |
| `scripts/tests/lint-docs-paths-sync.bats` | Bats tests for the sync lint (fixture-based synthetic drift/match cases + null handling). |
| `scripts/tests/pr-prep-docs-detection.bats` | Bats tests for `pr-prep`'s docs-only detection logic. Extends the existing fixture pattern from `scripts/tests/Taskfile.test.yaml`. Includes the regression-guard assertion that the flock body is NOT entered on docs-only inputs. |

**Modify:**

| Path | Change |
|------|--------|
| `Taskfile.yaml` | Add `DOCS_ONLY_PATHS` to `vars:`; new `pr-prep:docs` task; new `lint:docs-paths-sync` task wired into the `lint` umbrella; prepend detection logic inside existing `pr-prep` `cmd:` block (same subshell as flock body); pre-install `addlicense` in `setup`. |
| `scripts/tests/Taskfile.test.yaml` | Extend the fixture `pr-prep` task with the detection prologue + add `pr-prep:docs` stub. Both stubs use distinct `STUB_MARKER_*` env vars so bats tests can detect which lane ran. |
| `scripts/tests/pr-prep-lock-helpers.bash` (existing) | Add `export HOLOMUSH_PR_PREP_FORCE_FULL=1` to `init_test_env` so existing lock tests are robust against the new detection prologue. |
| `.github/workflows/ci.yaml` | Add `paths-ignore:` to both `push` and `pull_request` triggers. |
| `CLAUDE.md` | Update pr-prep policy section (`:243-249`) to describe the auto-selected docs lane + escape hatch + jj-snapshot caveat. |
| `AGENTS.md` | Mirror CLAUDE.md change. |
| `site/docs/contributing/pr-prep.md` | New section on the docs lane, same-name skip workflow pattern, escape hatch, and jj-snapshot caveat. |

---

## Task 1: Add `DOCS_ONLY_PATHS` canonical variable to Taskfile

**Files:**

- Modify: `Taskfile.yaml` (extend `vars:` block — anchored on `LICENSE_DIRS:` line, NOT line range, to avoid consuming the blank-line separator)

- [ ] **Step 1: Edit `Taskfile.yaml` — append `DOCS_ONLY_PATHS` after `LICENSE_DIRS`**

Use the Edit tool with this `old_string` / `new_string`:

`old_string`:

```yaml
  LICENSE_DIRS: api cmd internal pkg plugins scripts
```

`new_string`:

```yaml
  LICENSE_DIRS: api cmd internal pkg plugins scripts
  # Canonical docs-only path globs. MUST stay byte-identical with
  # .github/workflows/ci.yaml's paths-ignore: blocks and
  # .github/workflows/ci-docs-skip.yaml's paths: blocks.
  # Enforced by `task lint:docs-paths-sync`. Spec:
  # docs/superpowers/specs/2026-05-14-pr-prep-docs-fast-lane-design.md §4.1.
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

- [ ] **Step 2: Verify the var parses correctly**

Run: `task --list 2>&1 | head -3`

Expected: command runs without error. Output starts with `task: Available tasks for this project:`.

- [ ] **Step 3: Verify yq can read the var**

Run: `yq '.vars.DOCS_ONLY_PATHS' Taskfile.yaml`

Expected: nine lines from `site/**` through `LICENSE_HEADER`. No "null" output.

- [ ] **Step 4: Commit**

```bash
jj commit -m "feat(taskfile): add DOCS_ONLY_PATHS canonical var (yp2t.2)

Defines the canonical docs-only glob list. Mirrors land in
ci.yaml and ci-docs-skip.yaml in later tasks; lint:docs-paths-sync
will enforce byte-equivalence."
```

---

## Task 2: Build `scripts/docs-paths-regex.sh` + bats tests

**Files:**

- Create: `scripts/docs-paths-regex.sh`
- Create: `scripts/tests/docs-paths-regex.bats`

- [ ] **Step 1: Write the failing bats test (including yq-null failure-path coverage)**

Create `scripts/tests/docs-paths-regex.bats`:

```bash
#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# Test the glob->regex compilation for DOCS_ONLY_PATHS.
# The helper reads DOCS_ONLY_PATHS from Taskfile.yaml via yq and emits
# one anchored extended regex.

setup() {
  REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  HELPER="$REPO_ROOT/scripts/docs-paths-regex.sh"
}

# Happy path: run against the real Taskfile.yaml in the repo root.
run_helper() {
  REGEX="$(bash "$HELPER")"
}

@test "regex matches site/ paths" {
  run_helper
  echo "site/docs/index.md" | grep -E "$REGEX"
}

@test "regex matches docs/ paths" {
  run_helper
  echo "docs/superpowers/specs/foo.md" | grep -E "$REGEX"
}

@test "regex matches root README.md" {
  run_helper
  echo "README.md" | grep -E "$REGEX"
}

@test "regex matches nested *.md (web/CLAUDE.md)" {
  run_helper
  echo "web/CLAUDE.md" | grep -E "$REGEX"
}

@test "regex matches .claude/agents/*.md" {
  run_helper
  echo ".claude/agents/code-reviewer.md" | grep -E "$REGEX"
}

@test "regex matches .github/PULL_REQUEST_TEMPLATE.md (intentional per spec §4.1)" {
  run_helper
  echo ".github/PULL_REQUEST_TEMPLATE.md" | grep -E "$REGEX"
}

@test "regex does NOT match internal/foo.go" {
  run_helper
  run bash -c "echo internal/foo.go | grep -E '$REGEX'"
  [ "$status" -ne 0 ]
}

@test "regex does NOT match .claude/hooks/pre-commit.sh" {
  run_helper
  run bash -c "echo .claude/hooks/pre-commit.sh | grep -E '$REGEX'"
  [ "$status" -ne 0 ]
}

@test "regex does NOT match .claude/settings.json" {
  run_helper
  run bash -c "echo .claude/settings.json | grep -E '$REGEX'"
  [ "$status" -ne 0 ]
}

@test "regex does NOT match .github/workflows/ci.yaml" {
  run_helper
  run bash -c "echo .github/workflows/ci.yaml | grep -E '$REGEX'"
  [ "$status" -ne 0 ]
}

@test "regex matches literal LICENSE (no extension)" {
  run_helper
  echo "LICENSE" | grep -E "$REGEX"
}

@test "regex does NOT match LICENSE.txt (literal LICENSE only)" {
  run_helper
  run bash -c "echo LICENSE.txt | grep -E '$REGEX'"
  [ "$status" -ne 0 ]
}

@test "helper fails (does NOT emit ^(null)$) when DOCS_ONLY_PATHS is missing" {
  # Fixture: a Taskfile.yaml without DOCS_ONLY_PATHS. Override REPO_ROOT
  # via env (helper uses ${REPO_ROOT:-...}).
  FIX="$(mktemp -d)"
  cat > "$FIX/Taskfile.yaml" <<'YAML'
version: "3"
vars:
  BINARY_NAME: holomush
YAML
  run env REPO_ROOT="$FIX" bash "$HELPER"
  [ "$status" -ne 0 ]
  [[ "$output" == *"DOCS_ONLY_PATHS"* ]]
  # CRITICAL: must NOT silently emit the broken regex.
  [[ "$output" != "^(null)$" ]]
  rm -rf "$FIX"
}

@test "helper fails when DOCS_ONLY_PATHS is literally null" {
  FIX="$(mktemp -d)"
  cat > "$FIX/Taskfile.yaml" <<'YAML'
version: "3"
vars:
  DOCS_ONLY_PATHS:
YAML
  run env REPO_ROOT="$FIX" bash "$HELPER"
  [ "$status" -ne 0 ]
  [[ "$output" != "^(null)$" ]]
  rm -rf "$FIX"
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bats scripts/tests/docs-paths-regex.bats`

Expected: 14 of 14 fail (script doesn't exist). Error message for each: `bash: /Volumes/.../scripts/docs-paths-regex.sh: No such file or directory`.

- [ ] **Step 3: Create the helper script with hardened null handling**

Create `scripts/docs-paths-regex.sh`:

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Compile DOCS_ONLY_PATHS globs (read from Taskfile.yaml) into one anchored
# extended-regex string for grep -vE. Spec:
# docs/superpowers/specs/2026-05-14-pr-prep-docs-fast-lane-design.md §4.4.2

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${REPO_ROOT:-$(cd "$SCRIPT_DIR/.." && pwd)}"
TASKFILE="$REPO_ROOT/Taskfile.yaml"

[ -f "$TASKFILE" ] || { echo "ERROR: $TASKFILE not found" >&2; exit 1; }
command -v yq >/dev/null 2>&1 || { echo "ERROR: yq not installed" >&2; exit 1; }

# yq -e exits non-zero if the path is null/missing — but defensive coders
# don't trust a single signal. Also reject the literal string "null"
# (which yq emits with exit 0 when the key resolves to YAML null).
GLOBS="$(yq -e '.vars.DOCS_ONLY_PATHS' "$TASKFILE" 2>/dev/null)" || {
  echo "ERROR: vars.DOCS_ONLY_PATHS not found in $TASKFILE (yq -e failed)" >&2
  exit 1
}
if [ -z "$GLOBS" ] || [ "$GLOBS" = "null" ]; then
  echo "ERROR: vars.DOCS_ONLY_PATHS is empty or null in $TASKFILE" >&2
  exit 1
fi

# Compile each glob into a regex alternative.
ALTS=""
while IFS= read -r glob; do
  [ -n "$glob" ] || continue
  case "$glob" in
    *'**'*'**'*)
      echo "ERROR: glob '$glob' has multiple '**'; not supported" >&2
      exit 1
      ;;
    '**/*.md')
      alt='.*\.md'
      ;;
    *'/**')
      # foo/** -> foo/.*
      prefix="${glob%/**}"
      # escape literal dots
      prefix_re="${prefix//./\\.}"
      alt="${prefix_re}/.*"
      ;;
    *'**'*)
      echo "ERROR: glob '$glob' has unsupported '**' position" >&2
      exit 1
      ;;
    *)
      # Literal path (e.g., LICENSE, LICENSE_HEADER). Escape dots.
      alt="${glob//./\\.}"
      ;;
  esac
  if [ -z "$ALTS" ]; then
    ALTS="$alt"
  else
    ALTS="$ALTS|$alt"
  fi
done <<< "$GLOBS"

printf '^(%s)$\n' "$ALTS"
```

- [ ] **Step 4: Make executable**

Run: `chmod +x scripts/docs-paths-regex.sh`

- [ ] **Step 5: Run tests to verify they pass**

Run: `bats scripts/tests/docs-paths-regex.bats`

Expected: 14 of 14 pass.

- [ ] **Step 6: Manually inspect the emitted regex**

Run: `bash scripts/docs-paths-regex.sh`

Expected (single line):

```text
^(site/.*|docs/.*|.*\.md|\.claude/agents/.*|\.claude/commands/.*|\.claude/rules/.*|\.claude/agent-memory/.*|LICENSE|LICENSE_HEADER)$
```

- [ ] **Step 7: Commit**

```bash
jj commit -m "feat(scripts): docs-paths-regex.sh glob compiler + bats tests (yp2t.2)

Compiles DOCS_ONLY_PATHS globs (read from Taskfile.yaml via yq) into
one anchored extended-regex string. Supports the three glob shapes used
in the canonical list: 'foo/**', '**/*.md', and literal paths.

Hardened against the yq-null trap: yq emits literal 'null' with exit 0
when a key resolves to YAML null, which would silently produce ^(null)$
and break detection. Helper checks both yq -e exit code AND the literal
'null' string.

Bats coverage: 14 cases including .github/PULL_REQUEST_TEMPLATE.md
(spec §4.1 edge case) and two missing-DOCS_ONLY_PATHS failure paths."
```

---

## Task 3: Create `.github/workflows/ci-docs-skip.yaml`

(Ordered before the sync lint so that when Task 5's sync lint runs against the real files, all three are present.)

**Files:**

- Create: `.github/workflows/ci-docs-skip.yaml`

- [ ] **Step 1: Create the file**

Create `.github/workflows/ci-docs-skip.yaml`:

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

name: CI

# Same-name skip workflow for docs-only PRs. When ci.yaml is path-skipped,
# branch-protection required checks (Build, Lint, Test) would otherwise stay
# in 'Pending' state forever. This workflow provides no-op jobs whose names
# are byte-identical to ci.yaml's required-check job names, so GitHub treats
# them as the same required check and reports green.
#
# DOCS_ONLY_PATHS — must remain byte-identical with ci.yaml's paths-ignore
# blocks and Taskfile.yaml's DOCS_ONLY_PATHS var. Enforced by
# `task lint:docs-paths-sync`.

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

- [ ] **Step 2: Validate workflow syntax**

Run: `actionlint .github/workflows/ci-docs-skip.yaml`

Expected: zero output, exit 0.

- [ ] **Step 3: Commit**

```bash
jj commit -m "feat(ci): same-name skip workflow for docs-only PRs (yp2t.1)

Adds .github/workflows/ci-docs-skip.yaml with workflow name 'CI' and
no-op jobs Lint/Test/Build whose names match the required-check entries
in branch protection. GitHub's check-identity rule treats
(workflow_name, job_name) as the same required check across files, so
this workflow reports green for docs-only PRs while ci.yaml is path-
skipped (next task).

Spec §4.2.2. Required checks captured 2026-05-14:
Build/Lint/Test/CodeRabbit. CodeRabbit is the bot app; unaffected."
```

---

## Task 4: Add `paths-ignore` to `.github/workflows/ci.yaml`

**Files:**

- Modify: `.github/workflows/ci.yaml:3-8`

- [ ] **Step 1: Replace the `on:` block via Edit tool**

`old_string`:

```yaml
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
  workflow_dispatch:
```

`new_string`:

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

- [ ] **Step 2: Validate workflow syntax**

Run: `actionlint .github/workflows/ci.yaml`

Expected: zero output, exit 0.

- [ ] **Step 3: Commit**

```bash
jj commit -m "feat(ci): add paths-ignore to ci.yaml for docs-only PRs (yp2t.1)

Skip full CI (lint/test/integration/e2e/build) when only docs paths
change. Same-name skip workflow ci-docs-skip.yaml (previous commit)
keeps branch-protection required checks green.

Spec §4.2.1."
```

---

## Task 5: Build `scripts/lint-docs-paths-sync.sh` + bats tests + Taskfile entry

(Ordered after Tasks 3-4 so the runtime invocation against real files passes immediately on commit — no intermediate `task lint` breakage.)

**Files:**

- Create: `scripts/lint-docs-paths-sync.sh`
- Create: `scripts/tests/lint-docs-paths-sync.bats`
- Modify: `Taskfile.yaml` (add `lint:docs-paths-sync` task; wire into `lint` umbrella at `:62-73`)

- [ ] **Step 1: Write the failing bats test (fixture-based)**

Create `scripts/tests/lint-docs-paths-sync.bats`:

```bash
#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# Verify lint-docs-paths-sync.sh detects drift across the canonical
# Taskfile var and the four mirror locations in ci.yaml + ci-docs-skip.yaml.
# Tests use fixtures, not the real repo files, so they're independent of
# other tasks' ordering.

setup() {
  REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  HELPER="$REPO_ROOT/scripts/lint-docs-paths-sync.sh"
  TMPDIR_FIX="$(mktemp -d)"
  mkdir -p "$TMPDIR_FIX/.github/workflows"
}

teardown() {
  rm -rf "$TMPDIR_FIX"
}

write_taskfile() {
  cat > "$TMPDIR_FIX/Taskfile.yaml" <<'YAML'
version: "3"
vars:
  DOCS_ONLY_PATHS: |
    site/**
    docs/**
    **/*.md
YAML
}

write_taskfile_missing_var() {
  cat > "$TMPDIR_FIX/Taskfile.yaml" <<'YAML'
version: "3"
vars:
  BINARY_NAME: holomush
YAML
}

write_ci_yaml() {
  local push_paths="$1" pr_paths="$2"
  cat > "$TMPDIR_FIX/.github/workflows/ci.yaml" <<YAML
name: CI
on:
  push:
    branches: [main]
    paths-ignore:
$push_paths
  pull_request:
    branches: [main]
    paths-ignore:
$pr_paths
YAML
}

write_ci_skip_yaml() {
  local push_paths="$1" pr_paths="$2"
  cat > "$TMPDIR_FIX/.github/workflows/ci-docs-skip.yaml" <<YAML
name: CI
on:
  push:
    branches: [main]
    paths:
$push_paths
  pull_request:
    branches: [main]
    paths:
$pr_paths
YAML
}

@test "matching across all five extraction points passes" {
  write_taskfile
  PATHS="      - site/**
      - docs/**
      - \"**/*.md\""
  write_ci_yaml "$PATHS" "$PATHS"
  write_ci_skip_yaml "$PATHS" "$PATHS"
  run env REPO_ROOT="$TMPDIR_FIX" bash "$HELPER"
  [ "$status" -eq 0 ]
}

@test "drift between Taskfile and ci.yaml fails" {
  write_taskfile
  DRIFTED="      - site/**
      - docs/**"
  PATHS="      - site/**
      - docs/**
      - \"**/*.md\""
  write_ci_yaml "$DRIFTED" "$DRIFTED"
  write_ci_skip_yaml "$PATHS" "$PATHS"
  run env REPO_ROOT="$TMPDIR_FIX" bash "$HELPER"
  [ "$status" -ne 0 ]
  [[ "$output" == *"ci.yaml"* ]]
}

@test "drift between ci.yaml and ci-docs-skip.yaml fails" {
  write_taskfile
  PATHS="      - site/**
      - docs/**
      - \"**/*.md\""
  DRIFTED="      - site/**
      - docs/**"
  write_ci_yaml "$PATHS" "$PATHS"
  write_ci_skip_yaml "$DRIFTED" "$DRIFTED"
  run env REPO_ROOT="$TMPDIR_FIX" bash "$HELPER"
  [ "$status" -ne 0 ]
  [[ "$output" == *"ci-docs-skip.yaml"* ]]
}

@test "drift between push.paths-ignore and pull_request.paths-ignore in ci.yaml fails" {
  write_taskfile
  PUSH="      - site/**
      - docs/**
      - \"**/*.md\""
  PR_DRIFTED="      - site/**
      - docs/**"
  write_ci_yaml "$PUSH" "$PR_DRIFTED"
  write_ci_skip_yaml "$PUSH" "$PUSH"
  run env REPO_ROOT="$TMPDIR_FIX" bash "$HELPER"
  [ "$status" -ne 0 ]
}

@test "missing DOCS_ONLY_PATHS in Taskfile fails (no silent null)" {
  write_taskfile_missing_var
  PATHS="      - site/**"
  write_ci_yaml "$PATHS" "$PATHS"
  write_ci_skip_yaml "$PATHS" "$PATHS"
  run env REPO_ROOT="$TMPDIR_FIX" bash "$HELPER"
  [ "$status" -ne 0 ]
  [[ "$output" == *"DOCS_ONLY_PATHS"* ]]
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bats scripts/tests/lint-docs-paths-sync.bats`

Expected: 5 of 5 fail (script doesn't exist).

- [ ] **Step 3: Create the sync lint script with hardened null handling**

Create `scripts/lint-docs-paths-sync.sh`:

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Verify DOCS_ONLY_PATHS is byte-identical across Taskfile.yaml,
# ci.yaml's paths-ignore (push + pull_request), and ci-docs-skip.yaml's
# paths (push + pull_request). Five extraction points across three files.
# Spec: docs/superpowers/specs/2026-05-14-pr-prep-docs-fast-lane-design.md §4.4.3

set -euo pipefail

# REPO_ROOT may be overridden by tests; default to script's parent.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${REPO_ROOT:-$(cd "$SCRIPT_DIR/.." && pwd)}"
TASKFILE="$REPO_ROOT/Taskfile.yaml"
CI="$REPO_ROOT/.github/workflows/ci.yaml"
CI_SKIP="$REPO_ROOT/.github/workflows/ci-docs-skip.yaml"

command -v yq >/dev/null 2>&1 || { echo "ERROR: yq not installed" >&2; exit 1; }

normalize() {
  # Trim trailing whitespace and blank lines; preserve order.
  awk 'NF { sub(/[[:space:]]+$/, ""); print }'
}

# Extract canonical. Hardened against the yq-null trap (returns literal
# "null" with exit 0 when key is missing).
canonical_raw="$(yq -e '.vars.DOCS_ONLY_PATHS' "$TASKFILE" 2>/dev/null)" || {
  echo "ERROR: vars.DOCS_ONLY_PATHS not found in $TASKFILE (yq -e failed)" >&2
  exit 1
}
if [ -z "$canonical_raw" ] || [ "$canonical_raw" = "null" ]; then
  echo "ERROR: vars.DOCS_ONLY_PATHS is empty or null in $TASKFILE" >&2
  exit 1
fi
canonical="$(printf '%s\n' "$canonical_raw" | normalize)"

ci_push="$(yq '.on.push.paths-ignore[]' "$CI" 2>/dev/null | normalize || true)"
ci_pr="$(yq '.on.pull_request.paths-ignore[]' "$CI" 2>/dev/null | normalize || true)"
skip_push="$(yq '.on.push.paths[]' "$CI_SKIP" 2>/dev/null | normalize || true)"
skip_pr="$(yq '.on.pull_request.paths[]' "$CI_SKIP" 2>/dev/null | normalize || true)"

mismatches=0
check() {
  local name="$1" actual="$2"
  if [ "$actual" != "$canonical" ]; then
    echo "ERROR: docs-paths drift in $name" >&2
    diff <(printf '%s' "$canonical") <(printf '%s' "$actual") >&2 || true
    mismatches=$((mismatches + 1))
  fi
}

check "ci.yaml on.push.paths-ignore" "$ci_push"
check "ci.yaml on.pull_request.paths-ignore" "$ci_pr"
check "ci-docs-skip.yaml on.push.paths" "$skip_push"
check "ci-docs-skip.yaml on.pull_request.paths" "$skip_pr"

if [ "$mismatches" -ne 0 ]; then
  exit 1
fi

echo "docs-paths in sync across Taskfile + ci.yaml + ci-docs-skip.yaml."
```

- [ ] **Step 4: Make executable**

Run: `chmod +x scripts/lint-docs-paths-sync.sh`

- [ ] **Step 5: Run bats tests to verify they pass**

Run: `bats scripts/tests/lint-docs-paths-sync.bats`

Expected: 5 of 5 pass.

- [ ] **Step 6: Add `lint:docs-paths-sync` task to `Taskfile.yaml`**

Use Edit tool. `old_string`:

```yaml
  lint:docs-symmetry:
    desc: Verify AGENTS.md and CLAUDE.md plugin-runtime-symmetry subsection is byte-identical.
```

`new_string`:

```yaml
  lint:docs-paths-sync:
    desc: Verify DOCS_ONLY_PATHS is byte-identical across Taskfile.yaml, ci.yaml, ci-docs-skip.yaml.
    cmds:
      - bash scripts/lint-docs-paths-sync.sh

  lint:docs-symmetry:
    desc: Verify AGENTS.md and CLAUDE.md plugin-runtime-symmetry subsection is byte-identical.
```

- [ ] **Step 7: Wire into the `lint` umbrella at `Taskfile.yaml:62-73`**

Use Edit tool. `old_string`:

```yaml
      - task: lint:docs-symmetry
```

`new_string`:

```yaml
      - task: lint:docs-symmetry
      - task: lint:docs-paths-sync
```

- [ ] **Step 8: Verify the sync lint passes against the real (post-Tasks-3-4) files**

Run: `task lint:docs-paths-sync`

Expected: `docs-paths in sync across Taskfile + ci.yaml + ci-docs-skip.yaml.` and exit 0.

- [ ] **Step 9: Verify the full `task lint` still passes**

Run: `task lint`

Expected: all sub-lints (including the new `lint:docs-paths-sync`) pass.

- [ ] **Step 10: Commit**

```bash
jj commit -m "feat(scripts): lint-docs-paths-sync.sh + Taskfile entry (yp2t.2)

Enforces byte-equivalence of DOCS_ONLY_PATHS across Taskfile.yaml,
.github/workflows/ci.yaml, and .github/workflows/ci-docs-skip.yaml.
Wired into the lint umbrella.

Hardened against the yq-null trap. Bats coverage: 5 cases including
the missing-DOCS_ONLY_PATHS failure path."
```

---

## Task 6: Pre-install `addlicense` in `task setup`

**Files:**

- Modify: `Taskfile.yaml:612-621` (the `setup:` task)

- [ ] **Step 1: Append `addlicense` install to `setup` via Edit tool**

`old_string`:

```yaml
      - go install github.com/pseudomuto/protoc-gen-doc/cmd/protoc-gen-doc@latest
      - lefthook install
```

`new_string`:

```yaml
      - go install github.com/pseudomuto/protoc-gen-doc/cmd/protoc-gen-doc@latest
      - go install github.com/google/addlicense@latest
      - lefthook install
```

- [ ] **Step 2: Verify Taskfile parses**

Run: `task --list 2>&1 | head -3`

Expected: command runs without error.

- [ ] **Step 3: Commit**

```bash
jj commit -m "feat(setup): pre-install addlicense in task setup (yp2t.2)

license:run auto-installs addlicense via 'go install' when missing
(Taskfile.yaml:519). Pre-installing in setup means the docs lane has
no Go-compile dependency on a fresh checkout.

Spec §4.5."
```

---

## Task 7: Add `pr-prep:docs` target

**Files:**

- Modify: `Taskfile.yaml` (insert after `pr-prep:run` at `:606`)

**Ordering note:** This task adds the docs lane target. Task 8 will wire `pr-prep` to invoke it. The smoke in Step 3 below tests the docs lane in isolation — `lint:docs-symmetry` requires `AGENTS.md` ↔ `CLAUDE.md` byte-identity in the symmetry block, which is true at this commit (Task 9 has not yet run; both files match `main`). Do NOT reorder Task 7 after Task 9.

- [ ] **Step 1: Insert the new task via Edit tool**

`old_string`:

```yaml
      - echo "✓ All PR checks passed."

  # ──────────────────────────────────────────
  # Setup (run once)
```

`new_string`:

```yaml
      - echo "✓ All PR checks passed."

  pr-prep:docs:
    desc: Docs-only fast lane (auto-selected by pr-prep on docs-only diffs).
    cmds:
      - echo "▸ Running docs-only fast lane"
      - task: lint:markdown
      - task: lint:yaml
      - task: lint:docs-symmetry
      - task: fmt:check
      - task: license:check
      - task: lint:docs-paths-sync
      - echo "✓ Docs lane passed."

  # ──────────────────────────────────────────
  # Setup (run once)
```

No flock acquisition (spec INV-6).

- [ ] **Step 2: Verify the task is callable**

Run: `task --list 2>&1 | grep "pr-prep:docs"`

Expected: one line showing `pr-prep:docs` and its description.

- [ ] **Step 3: Smoke-run on the current branch**

Run: `task pr-prep:docs`

Expected: all sub-tasks pass. If any fails, fix the underlying file (e.g., markdown formatting, license headers on new scripts) before continuing.

- [ ] **Step 4: Commit**

```bash
jj commit -m "feat(taskfile): add pr-prep:docs fast lane (yp2t.2)

New task wiring lint:markdown + lint:yaml + lint:docs-symmetry +
fmt:check + license:check + lint:docs-paths-sync. No flock (INV-6).
Auto-selected by pr-prep detection in the next task.

Spec §4.3.2."
```

---

## Task 8: Wire docs-only detection into `pr-prep` (single cmd block) + fixture-based bats tests

**Files:**

- Modify: `Taskfile.yaml:534-563` (the `pr-prep` task's single `cmd:` block)
- Modify: `scripts/tests/Taskfile.test.yaml` (extend fixture `pr-prep` with detection prologue; add `pr-prep:docs` stub)
- Modify: `scripts/tests/pr-prep-lock-helpers.bash` (export `HOLOMUSH_PR_PREP_FORCE_FULL=1` in `init_test_env` so existing lock tests skip detection)
- Create: `scripts/tests/pr-prep-docs-detection.bats`

This is the load-bearing change. Per spec §4.3.1, the detection logic MUST live in the same `- cmd:` block as the flock body — separate `- cmd:` entries do NOT short-circuit due to go-task subshell isolation. The TDD assertion uses lane-specific marker files written by the fixture's stubs.

- [ ] **Step 1: Write the failing fixture-based bats test FIRST (TDD red phase)**

Create `scripts/tests/pr-prep-docs-detection.bats` with this content:

```bash
#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

load 'pr-prep-lock-helpers'

# Tests pr-prep's docs-only detection prologue using the existing fixture
# pattern. Each test injects a `git diff` output via PATH shim, runs the
# fixture's `pr-prep`, and asserts which lane stub ran by checking marker
# file existence.
#
# REGRESSION GUARD: every docs-only case asserts the full-lane marker
# was NOT written (file does not exist). Catches the r2 structural defect.

setup_file() {
  if [ ! -f "scripts/tests/Taskfile.test.yaml" ]; then
    echo "ERROR: bats must be invoked from the repo root (try 'task test:bats')." >&2
    exit 1
  fi
}

setup() {
  bats_load_library bats-support
  bats_load_library bats-assert
  init_test_env
  # Docs-detection tests OVERRIDE the helper's force-full setting (added
  # in Step 2). Unset it so the detection prologue actually runs.
  unset HOLOMUSH_PR_PREP_FORCE_FULL

  # Lane markers — must be different paths so we can detect which ran.
  export STUB_MARKER="${BATS_TEST_TMPDIR}/full-marker"
  export STUB_DOCS_MARKER="${BATS_TEST_TMPDIR}/docs-marker"

  # PATH shim for git. Intercepts `git diff --name-only` and `git fetch`
  # by reading injected env vars; other git calls bypass to real git.
  GIT_SHIM_DIR="${BATS_TEST_TMPDIR}/bin"
  mkdir -p "$GIT_SHIM_DIR"
  cat > "$GIT_SHIM_DIR/git" <<'SH'
#!/usr/bin/env bash
if [ "$1" = "diff" ] && [ "$2" = "--name-only" ]; then
  printf '%s\n' "${BATS_GIT_DIFF_OUT:-}"
  exit "${BATS_GIT_DIFF_RC:-0}"
fi
if [ "$1" = "fetch" ]; then
  exit 0
fi
REAL_GIT="$(PATH="${ORIG_PATH}" command -v git)"
exec "$REAL_GIT" "$@"
SH
  chmod +x "$GIT_SHIM_DIR/git"
  export ORIG_PATH="$PATH"
  export PATH="$GIT_SHIM_DIR:$PATH"
}

teardown() {
  unset BATS_GIT_DIFF_OUT BATS_GIT_DIFF_RC ORIG_PATH \
        STUB_MARKER STUB_DOCS_MARKER HOLOMUSH_PR_PREP_FORCE_FULL
}

run_fixture_pr_prep() {
  run task -t "$(fixture_taskfile)" pr-prep
}

assert_docs_lane() {
  [ "$status" -eq 0 ]
  [ -f "$STUB_DOCS_MARKER" ]
  # REGRESSION GUARD: full-lane marker must NOT exist.
  [ ! -f "$STUB_MARKER" ]
}

assert_full_lane() {
  [ "$status" -eq 0 ]
  [ -f "$STUB_MARKER" ]
  # Inverse guard: docs marker must NOT exist.
  [ ! -f "$STUB_DOCS_MARKER" ]
}

@test "docs/single_md: site/docs/index.md -> docs lane only" {
  export BATS_GIT_DIFF_OUT="site/docs/index.md"
  run_fixture_pr_prep
  assert_docs_lane
}

@test "docs/root_readme: README.md -> docs lane only" {
  export BATS_GIT_DIFF_OUT="README.md"
  run_fixture_pr_prep
  assert_docs_lane
}

@test "docs/nested_claude: web/CLAUDE.md -> docs lane only" {
  export BATS_GIT_DIFF_OUT="web/CLAUDE.md"
  run_fixture_pr_prep
  assert_docs_lane
}

@test "docs/agents_md: .claude/agents/code-reviewer.md -> docs lane only" {
  export BATS_GIT_DIFF_OUT=".claude/agents/code-reviewer.md"
  run_fixture_pr_prep
  assert_docs_lane
}

@test "docs/pull_request_template: .github/PULL_REQUEST_TEMPLATE.md -> docs lane only (spec §4.1 edge case)" {
  export BATS_GIT_DIFF_OUT=".github/PULL_REQUEST_TEMPLATE.md"
  run_fixture_pr_prep
  assert_docs_lane
}

@test "full/go_source: internal/foo/bar.go -> full lane" {
  export BATS_GIT_DIFF_OUT="internal/foo/bar.go"
  run_fixture_pr_prep
  assert_full_lane
}

@test "full/mixed: site/docs/index.md + internal/foo.go -> full lane" {
  export BATS_GIT_DIFF_OUT=$'site/docs/index.md\ninternal/foo.go'
  run_fixture_pr_prep
  assert_full_lane
}

@test "full/claude_hooks: .claude/hooks/pre-commit.sh -> full lane" {
  export BATS_GIT_DIFF_OUT=".claude/hooks/pre-commit.sh"
  run_fixture_pr_prep
  assert_full_lane
}

@test "full/claude_settings: .claude/settings.json -> full lane" {
  export BATS_GIT_DIFF_OUT=".claude/settings.json"
  run_fixture_pr_prep
  assert_full_lane
}

@test "full/ci_yaml: .github/workflows/ci.yaml -> full lane" {
  export BATS_GIT_DIFF_OUT=".github/workflows/ci.yaml"
  run_fixture_pr_prep
  assert_full_lane
}

@test "full/empty_diff: empty diff -> full lane" {
  export BATS_GIT_DIFF_OUT=""
  run_fixture_pr_prep
  assert_full_lane
}

@test "full/git_diff_error: git diff exits non-zero -> full lane" {
  export BATS_GIT_DIFF_RC=1
  export BATS_GIT_DIFF_OUT=""
  run_fixture_pr_prep
  assert_full_lane
}

@test "force_full: HOLOMUSH_PR_PREP_FORCE_FULL=1 forces full lane on docs-only diff" {
  export HOLOMUSH_PR_PREP_FORCE_FULL=1
  export BATS_GIT_DIFF_OUT="site/docs/index.md"
  run_fixture_pr_prep
  assert_full_lane
}
```

- [ ] **Step 2: Update `scripts/tests/pr-prep-lock-helpers.bash` to force full lane in existing tests**

The existing lock tests don't set `BATS_GIT_DIFF_OUT`, so without a guard they'd hit real `git diff` and could spuriously enter the docs lane once Step 3 lands. Force-full ensures the prologue falls through.

Use Edit tool. `old_string`:

```bash
init_test_env() {
  export LOCK_DIR_OVERRIDE="${BATS_TEST_TMPDIR}/holomush-pr-prep-bats"
  export INFO_FILE="$LOCK_DIR_OVERRIDE/info"
  export LOCK_FILE="$LOCK_DIR_OVERRIDE/lock"
  rm -rf "$LOCK_DIR_OVERRIDE"
}
```

`new_string`:

```bash
init_test_env() {
  export LOCK_DIR_OVERRIDE="${BATS_TEST_TMPDIR}/holomush-pr-prep-bats"
  export INFO_FILE="$LOCK_DIR_OVERRIDE/info"
  export LOCK_FILE="$LOCK_DIR_OVERRIDE/lock"
  rm -rf "$LOCK_DIR_OVERRIDE"
  # Existing lock tests do not exercise docs-only detection. Force the full
  # lane so the detection prologue (added to the fixture in the next step
  # of this task) falls through. The new pr-prep-docs-detection.bats
  # explicitly `unset`s this in its own setup to re-enable detection.
  export HOLOMUSH_PR_PREP_FORCE_FULL=1
}
```

- [ ] **Step 3: Run bats — confirm deterministic red phase**

Run: `bats scripts/tests/pr-prep-docs-detection.bats`

Expected: **5 docs-lane tests FAIL, 8 full-lane + force-full tests PASS** (5 fail / 8 pass / 13 total).

Why deterministic: the fixture's `pr-prep` does NOT yet have the detection prologue (Step 4 adds it). Every invocation falls through to the existing flock body, runs `pr-prep:stub`, writes `$STUB_MARKER`. Docs-lane tests fail at `[ -f "$STUB_DOCS_MARKER" ]` because no docs stub ran. Full-lane tests pass because their expected marker IS written.

If you see all 13 PASS or any unrelated failure pattern, do NOT proceed — verify Step 4 hasn't already been applied to the fixture.

- [ ] **Step 4: Extend `scripts/tests/Taskfile.test.yaml` — add detection prologue + `pr-prep:docs` stub (green phase)**

Use Edit tool. `old_string` (the fixture's `pr-prep` task body, currently `Taskfile.test.yaml:18-60`):

```yaml
  pr-prep:
    desc: Locked entry point under test
    preconditions:
      - sh: command -v flock >/dev/null 2>&1
        msg: "flock(1) is required. Install with 'brew install flock' (macOS) or 'apt install util-linux' (Linux)."
    cmds:
      - cmd: |
          set -euo pipefail
          LOCK_DIR="{{.LOCK_DIR}}"
          mkdir -p "$LOCK_DIR"
          LOCK="$LOCK_DIR/lock"
          INFO="$LOCK_DIR/info"
          rc=0
```

`new_string`:

```yaml
  pr-prep:
    desc: Locked entry point under test
    preconditions:
      - sh: command -v flock >/dev/null 2>&1
        msg: "flock(1) is required. Install with 'brew install flock' (macOS) or 'apt install util-linux' (Linux)."
    cmds:
      - cmd: |
          set -euo pipefail

          # ─────────────────────────────────────────────────────────────
          # Docs-only detection (mirrors production pr-prep at
          # Taskfile.yaml:534-563). Same subshell as flock body below.
          # ─────────────────────────────────────────────────────────────
          if [ "${HOLOMUSH_PR_PREP_FORCE_FULL:-}" != "1" ]; then
            timeout 5 git fetch -q origin main 2>/dev/null || true
            CHANGED=$(timeout 5 git diff --name-only origin/main...HEAD 2>/dev/null || true)
            if [ -n "$CHANGED" ]; then
              DOCS_REGEX=$(bash scripts/docs-paths-regex.sh)
              if ! printf '%s\n' "$CHANGED" | grep -vE "$DOCS_REGEX" >/dev/null; then
                echo "▸ docs-only diff detected; running pr-prep:docs"
                exec task -t "{{.TASKFILE}}" pr-prep:docs
              fi
            fi
          fi

          LOCK_DIR="{{.LOCK_DIR}}"
          mkdir -p "$LOCK_DIR"
          LOCK="$LOCK_DIR/lock"
          INFO="$LOCK_DIR/info"
          rc=0
```

Then append a `pr-prep:docs` stub task at the end of the fixture file. Use Edit tool. `old_string`:

```yaml
  pr-prep:stub:
    cmds:
      - cmd: |-
          MARKER="${STUB_MARKER:-/tmp/pr-prep-stub-marker.$$}"
          : > "$MARKER"
          if [ -n "${STUB_SLEEP:-}" ]; then
            sleep "$STUB_SLEEP"
          fi
          if [ -n "${STUB_EXIT:-}" ]; then
            exit "$STUB_EXIT"
          fi
```

`new_string`:

```yaml
  pr-prep:stub:
    cmds:
      - cmd: |-
          MARKER="${STUB_MARKER:-/tmp/pr-prep-stub-marker.$$}"
          : > "$MARKER"
          if [ -n "${STUB_SLEEP:-}" ]; then
            sleep "$STUB_SLEEP"
          fi
          if [ -n "${STUB_EXIT:-}" ]; then
            exit "$STUB_EXIT"
          fi

  pr-prep:docs:
    cmds:
      - cmd: |-
          DOCS_MARKER="${STUB_DOCS_MARKER:-/tmp/pr-prep-docs-marker.$$}"
          : > "$DOCS_MARKER"
```

- [ ] **Step 5: Re-run bats — confirm green phase**

Run: `bats scripts/tests/pr-prep-docs-detection.bats`

Expected: 13 of 13 pass. The fixture now has the detection prologue + the docs stub; docs cases correctly `exec` into `pr-prep:docs` (writing `$STUB_DOCS_MARKER`), full cases correctly fall through to the flock body (writing `$STUB_MARKER`).

If any test fails, do NOT proceed. Inspect the fixture pr-prep body — the detection prologue MUST be INSIDE the same `- cmd:` block as the flock body (not a separate `- cmd:` entry). The reviewer r1 finding #1 explicitly catches the separate-cmd-block defect.

- [ ] **Step 6: Edit production `Taskfile.yaml:534-563` — prepend detection inline to the existing `- cmd:` block**

Use Edit tool. `old_string` (the production pr-prep cmd block):

```yaml
      - cmd: |
          set -euo pipefail
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
```

`new_string`:

```yaml
      - cmd: |
          set -euo pipefail

          # ─────────────────────────────────────────────────────────────
          # Docs-only detection. MUST live in this same `- cmd:` block as
          # the flock body below — go-task spawns each `- cmd:` in its own
          # subshell, so `exec` only short-circuits within the same shell.
          # Spec §4.3.1.
          # ─────────────────────────────────────────────────────────────
          if [ "${HOLOMUSH_PR_PREP_FORCE_FULL:-}" != "1" ]; then
            timeout 5 git fetch -q origin main 2>/dev/null || true
            CHANGED=$(timeout 5 git diff --name-only origin/main...HEAD 2>/dev/null || true)
            if [ -n "$CHANGED" ]; then
              DOCS_REGEX=$(bash scripts/docs-paths-regex.sh)
              if ! printf '%s\n' "$CHANGED" | grep -vE "$DOCS_REGEX" >/dev/null; then
                echo "▸ docs-only diff detected; running pr-prep:docs"
                exec task pr-prep:docs
              fi
            fi
          fi

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
```

Update the `pr-prep` desc to mention the auto-detection. Edit tool. `old_string`:

```yaml
  pr-prep:
    desc: Run all CI checks locally before pushing (mirrors ALL CI jobs) [serialized via flock]
```

`new_string`:

```yaml
  pr-prep:
    desc: Run all CI checks locally before pushing (mirrors ALL CI jobs) [serialized via flock; auto-selects docs lane on docs-only diffs]
```

- [ ] **Step 7: Run the full bats suite — verify production change + regression coverage**

Run: `task test:bats`

Expected: all bats tests pass — `pr-prep-lock.bats` (unchanged behavior thanks to Step 2's `init_test_env` force-full update), `docs-paths-regex.bats`, `lint-docs-paths-sync.bats`, and `pr-prep-docs-detection.bats`.

If `pr-prep-lock.bats` fails, verify Step 2's helper edit exports `HOLOMUSH_PR_PREP_FORCE_FULL=1`.

- [ ] **Step 8: Smoke — run `task pr-prep` on the current branch**

Run: `task pr-prep`

Current branch's diff vs origin/main includes Taskfile.yaml, scripts/, .github/workflows/, docs/, *.md — mixed. Expected: detection runs, classifies as mixed (non-docs paths present), falls through to full lane. The full pr-prep runs ~3-5 min.

You may abort with Ctrl-C after observing the "docs-only diff detected" banner does NOT appear and the flock acquisition proceeds. Full pr-prep completion is verified in Task 10.

- [ ] **Step 9: Commit**

```bash
jj commit -m "feat(taskfile): pr-prep auto-detects docs-only diffs (yp2t.2)

Prepends detection logic inside pr-prep's existing single \`- cmd:\`
block so \`exec task pr-prep:docs\` correctly short-circuits within
the same subshell. Spec §4.3.1 explicitly documents the go-task
subshell trap that necessitates the single-block structure.

Detection: \`timeout 5 git fetch\` + \`timeout 5 git diff
--name-only origin/main...HEAD\` classified against DOCS_REGEX from
scripts/docs-paths-regex.sh. Escape hatch:
HOLOMUSH_PR_PREP_FORCE_FULL=1.

Tests: scripts/tests/pr-prep-docs-detection.bats, 13 cases against
the extended scripts/tests/Taskfile.test.yaml fixture. Lane markers
(STUB_MARKER for full lane, STUB_DOCS_MARKER for docs lane) provide
deterministic regression-guard that the flock body is NOT entered
on docs-only inputs.

Existing pr-prep-lock.bats tests force HOLOMUSH_PR_PREP_FORCE_FULL=1
via init_test_env (helper update) so their behavior is unaffected by
the new prologue."
```

---

## Task 9: Documentation updates

**Files:**

- Modify: `CLAUDE.md:243-249` (the pr-prep section)
- Modify: `AGENTS.md:243-249` (same section)
- Modify: `site/docs/contributing/pr-prep.md` (append docs-lane + same-name skip + jj-snapshot caveat sections)

- [ ] **Step 1: Update `CLAUDE.md:243-249` via Edit tool**

`old_string`:

```markdown
**MUST** run `task pr-prep` before creating a PR or pushing to a PR branch.
This mirrors all CI jobs (lint, format, schema, license, unit, integration,
E2E) and MUST pass with zero failures. Do NOT push to a PR branch without
a green `task pr-prep`. Docker is always available — never skip E2E tests.
The gate is serialized per user — only one `task pr-prep` runs at a time on
a given machine. See [pr-prep](site/docs/contributing/pr-prep.md) for
collision behavior.
```

`new_string`:

```markdown
**MUST** run `task pr-prep` before creating a PR or pushing to a PR branch.
It auto-detects docs-only diffs (per `Taskfile.yaml` `vars.DOCS_ONLY_PATHS`)
and runs the `pr-prep:docs` fast lane in that case; for any non-docs path,
it runs the full pipeline mirroring all CI jobs (lint, format, schema,
license, unit, integration, E2E) and MUST pass with zero failures. Use
`HOLOMUSH_PR_PREP_FORCE_FULL=1 task pr-prep` to force the full lane.
Docker is always available — never skip E2E tests on the full lane. The
full lane is serialized per user — only one runs at a time on a given
machine. Note: docs detection relies on jj's snapshot of `@`; run `jj st`
first if you've made edits since the last `jj` command. See
[pr-prep](site/docs/contributing/pr-prep.md) for collision behavior and
the docs lane.
```

- [ ] **Step 2: Update `AGENTS.md:243-249` with the identical replacement**

Use Edit tool with the same `old_string` / `new_string` as Step 1.

- [ ] **Step 3: Run docs-symmetry lint to confirm**

Run: `task lint:docs-symmetry`

Expected: `AGENTS.md and CLAUDE.md plugin-runtime-symmetry subsection is byte-identical.`

- [ ] **Step 4: Append a new section to `site/docs/contributing/pr-prep.md`**

Read the file first via the Read tool to confirm its current end. Then use the Edit tool to append the new section. `old_string` = last existing line of the file. `new_string` = last existing line + the new section.

New section content (append at end-of-file):

````markdown

## Docs-only fast lane

`task pr-prep` auto-detects when a diff touches only documentation paths
(per the canonical `DOCS_ONLY_PATHS` list in `Taskfile.yaml`'s `vars:` block)
and delegates to `task pr-prep:docs`, a lightweight lane that runs:

- `lint:markdown` (rumdl)
- `lint:yaml` (yamlfmt)
- `lint:docs-symmetry` (AGENTS.md ↔ CLAUDE.md byte-equivalence)
- `fmt:check` (dprint + rumdl)
- `license:check` (addlicense)
- `lint:docs-paths-sync` (verifies the canonical glob list is in sync
  across `Taskfile.yaml` + `.github/workflows/ci.yaml` + `.github/workflows/ci-docs-skip.yaml`)

The docs lane has no Docker dependency, runs no Go compile, and does not
acquire the `pr-prep` flock — concurrent docs lanes are safe.

### When is a diff "docs-only"?

A diff is docs-only when every changed path matches one of the canonical
globs:

- `site/**`
- `docs/**`
- `**/*.md` (including `README.md`, `web/CLAUDE.md`, `.github/PULL_REQUEST_TEMPLATE.md`)
- `.claude/agents/**`
- `.claude/commands/**`
- `.claude/rules/**`
- `.claude/agent-memory/**`
- `LICENSE`, `LICENSE_HEADER`

Non-docs paths (any `.go`, `.proto`, `.yaml` outside the included dirs,
`.claude/hooks/**`, `.claude/settings*.json`, `.github/workflows/**`, etc.)
route the entire diff to the full lane.

### Forcing the full lane

```bash
HOLOMUSH_PR_PREP_FORCE_FULL=1 task pr-prep
```

Use this when a markdown change references not-yet-merged code via
`mkdocstrings`-style includes, or when you want full validation regardless
of diff classification.

### jj snapshot caveat

Detection uses `git diff --name-only origin/main...HEAD`, which reflects
jj's last auto-snapshot of `@`. If you've edited files since the last jj
command, the changes are on disk but not yet in `@`. Run `jj st` (or any
read-only jj command) before `task pr-prep` to force a snapshot.

### Same-name skip workflow

On the CI side, `.github/workflows/ci.yaml` has `paths-ignore` for the
same glob list. A separate workflow `.github/workflows/ci-docs-skip.yaml`
runs on the inverse path filter with workflow name `CI` and jobs named
`Lint`, `Test`, `Build` (no-op `echo`s). GitHub's check-identity rule
treats `(workflow_name, job_name)` as the same required check across
files, so branch-protection required checks stay green on docs-only PRs
without invoking the full pipeline.

If you ever edit `DOCS_ONLY_PATHS`, edit all three locations
(Taskfile.yaml + ci.yaml + ci-docs-skip.yaml) and run
`task lint:docs-paths-sync` to verify byte-equivalence.
````

- [ ] **Step 5: Run markdown lint to confirm docs are well-formed**

Run: `task lint:markdown`

Expected: zero output, exit 0.

- [ ] **Step 6: Commit**

```bash
jj commit -m "docs: pr-prep docs-only fast lane policy + how-to (yp2t.2)

Updates CLAUDE.md + AGENTS.md to describe the auto-selected docs lane,
the FORCE_FULL escape hatch, and the jj-snapshot caveat. Adds a new
section to site/docs/contributing/pr-prep.md covering the docs-lane
composition, the docs-only path globs, force-full instructions, and
the same-name skip workflow CI pattern."
```

---

## Task 10: Final smoke + bead status

**Files:** (no edits — verification only)

- [ ] **Step 1: Run the full `task lint`**

Run: `task lint`

Expected: all sub-lints pass, including the new `lint:docs-paths-sync`.

- [ ] **Step 2: Run the full bats suite**

Run: `task test:bats`

Expected: all bats tests pass — the new `docs-paths-regex.bats`, `lint-docs-paths-sync.bats`, `pr-prep-docs-detection.bats` plus existing `pr-prep-lock.bats`.

- [ ] **Step 3: Force-full smoke**

Run: `HOLOMUSH_PR_PREP_FORCE_FULL=1 task pr-prep`

Expected: runs the full pr-prep lane (no docs-only banner). May take 3-15 min. This validates the escape hatch end-to-end.

If pr-prep fails, the failure must be addressed before push (per CLAUDE.md "MUST run task pr-prep before any push"). Most likely failure modes:

- Markdown formatting violations in newly-added documentation (run `task fmt:markdown`).
- License headers missing from new shell scripts (run `task license:add`).
- The `lint:docs-paths-sync` failing — re-run `bash scripts/lint-docs-paths-sync.sh` to see the diff; reconcile the glob lists across the three files.

- [ ] **Step 4: Update bead status**

```bash
bd note holomush-yp2t.1 "Implementation complete in PR (single atomic landing per spec §6.2). ci.yaml paths-ignore + ci-docs-skip.yaml live; lint:docs-paths-sync passing."
bd note holomush-yp2t.2 "Implementation complete in PR. pr-prep auto-detects docs-only diffs, exec's to pr-prep:docs in the same subshell as the flock body (spec §4.3.1). 13-case fixture-based bats coverage with regression-guard assertions."
bd note holomush-yp2t "Two children ready to close on PR merge."
```

- [ ] **Step 5: Final commit (if any tweaks surfaced during smoke)**

If Step 3 surfaced any fixes, commit them:

```bash
jj commit -m "fix(docs/scripts): smoke-test cleanups (yp2t.2)

Address pr-prep-full-lane findings on the implementation branch:
[list specific fixes here]."
```

Otherwise: no commit needed.

- [ ] **Step 6: Ready for PR**

Branch is implementation-complete. PR creation follows the project's standard flow (CLAUDE.md "Pull Request Guide"). On merge, both `yp2t.1` and `yp2t.2` close. Verify on the merged PR's GH Actions tab that:

1. `ci-docs-skip.yaml` did NOT run (this PR is a mixed diff — Taskfile + scripts + docs).
2. `ci.yaml` DID run all jobs.
3. Subsequent docs-only PRs invoke only `ci-docs-skip.yaml`.

---

## Self-Review Notes

(Generated during plan-writing per `superpowers:writing-plans` checklist.)

**1. Spec coverage:**

| Spec section | Implemented by |
|---|---|
| §4.1 canonical path list | Task 1 (DOCS_ONLY_PATHS var) + Task 3 (ci-docs-skip mirror) + Task 4 (ci.yaml mirror) |
| §4.2.1 paths-ignore on ci.yaml | Task 4 |
| §4.2.2 same-name skip workflow | Task 3 |
| §4.3.1 detection step (single cmd block) | Task 8 Step 5 |
| §4.3.2 pr-prep:docs target | Task 7 |
| §4.4.1 DOCS_ONLY_PATHS storage | Task 1 |
| §4.4.2 docs-paths-regex.sh | Task 2 (with yq-null hardening) |
| §4.4.3 lint:docs-paths-sync | Task 5 (with yq-null hardening) |
| §4.5 addlicense pre-install | Task 6 |
| §4.6 documentation updates | Task 9 |
| §4.7.1 pr-prep-docs-detection.bats (incl. regression guard) | Task 8 |
| §4.7.2 lint-docs-paths-sync.bats | Task 5 |
| §4.7.3 docs-paths-regex.bats | Task 2 |
| §4.7.4 manual integration test (post-merge) | Task 10 Step 6 |
| INV-1 byte-equivalence enforcement | Task 5 |
| INV-2 mixed-diff → full lane | Task 8 (bats case `full/mixed`) |
| INV-3 FORCE_FULL=1 | Task 8 (bats case `force_full` + Task 10 Step 3 smoke) |
| INV-4 detection-failure fallback | Task 8 (bats cases `full/empty_diff` + `full/git_diff_error`) |
| INV-5 byte-identical job names | Task 3 (Lint/Test/Build match ci.yaml's job names per spec §1 capture) |
| INV-6 no flock on docs lane | Task 7 (pr-prep:docs has no flock cmd) |

No spec section is uncovered.

**2. Placeholder scan:** No TBDs, no "implement later", no "similar to Task N". Every code/config block is complete. Step 4 of Task 8 explicitly enumerates which tests are expected to pass at that intermediate state.

**3. Type/identifier consistency:**

- `DOCS_ONLY_PATHS` (var) used consistently in Tasks 1, 2, 5.
- `DOCS_REGEX` (shell var) used consistently inside Task 8's `cmd:` block (production AND fixture).
- `HOLOMUSH_PR_PREP_FORCE_FULL` env var spelled identically in Tasks 8, 9, 10 + the helper update.
- Job names `Lint`/`Test`/`Build` byte-identical between Task 3's ci-docs-skip.yaml and existing ci.yaml (`ci.yaml:14-16,93,148,191,244`).
- `STUB_MARKER` (full lane) and `STUB_DOCS_MARKER` (docs lane) consistently named between fixture (Task 8 Step 1) and bats setup (Task 8 Step 3).
- Path `scripts/docs-paths-regex.sh` referenced consistently in Tasks 2, 8.
- Path `scripts/lint-docs-paths-sync.sh` referenced consistently in Task 5.
