<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Session Workspace Isolation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tear down `go.work` + `task gowork`, ship `task workspace:new`, add a `SessionStart` hook that warns when starting in the shared `default` jj workspace, and document the new per-session-isolation discipline in CLAUDE.md.

**Architecture:** Soft enforcement — no harness coupling beyond what already exists. A new `scripts/jj-main-repo.sh` shell helper centralises the `.jj/repo` dir-vs-file detection used by both the new task target and the new hook. Phases ordered to land Phase 1 first so subsequent phases don't have to fight `go.work` duplicates.

**Tech Stack:** bash 3.2 (macOS default), fish 3.x (maintainer's interactive shell), `task` (go-task) for Taskfile targets, jj 0.40+, Claude Code hook contract (plain stdout for SessionStart).

**Spec:** [`docs/superpowers/specs/2026-04-25-session-workspace-isolation-design.md`](../specs/2026-04-25-session-workspace-isolation-design.md)

**Tracking bead:** _filed in Task 0_

---

## File map

| File | Op | Purpose |
|---|---|---|
| `go.work` | Delete | Repo no longer ships a workspace file |
| `.gitignore` | Modify | Add `/go.work` and `/go.work.sum` so locally-generated copies stay local |
| `Taskfile.yaml` | Modify | Delete `gowork` target (lines 515-551); add `workspace:new` target |
| `CLAUDE.md` | Modify | Drop `task gowork` references in "jj Workspace Commands" (lines 552-568); insert new "Session isolation" subsection between "jj Workspace Commands" and "Beads Commands"; expand "Landing the Plane" step 5 (line 849) |
| `scripts/jj-main-repo.sh` | Create | Sourceable shell helper — sets `IS_DEFAULT`, `MAIN_REPO`, `WORKTREES` |
| `.claude/hooks/warn-default-workspace.sh` | Create | SessionStart hook; sources `scripts/jj-main-repo.sh`; emits warning when `IS_DEFAULT=yes` |
| `.claude/settings.json` | Modify | Wire the new hook into `hooks.SessionStart` alongside existing `bd prime` |

Phases 1-4 each become an independent commit on the same PR branch (per spec implementation-order section, "Each phase is a separate commit on the same PR branch").

---

## Task 0: File tracking bead

**Files:** _none_ (beads command only)

- [ ] **Step 1: Create the bead**

Run from any workspace:

```bash
bd create --title "Session workspace isolation + go.work removal" \
  --description "$(cat <<'EOF'
Implement docs/superpowers/specs/2026-04-25-session-workspace-isolation-design.md.

Four phases:
1. Delete go.work + task gowork; resolves holomush-rmo2 as side effect
2. Add task workspace:new with .jj/repo MAIN_REPO discovery
3. Add .claude/hooks/warn-default-workspace.sh SessionStart hook
4. Document in CLAUDE.md (new "Session isolation" subsection + Landing the Plane step 5 expansion)

Plan: docs/superpowers/plans/2026-04-25-session-workspace-isolation.md
EOF
)" \
  --type task --priority 2
```

Expected output: `✓ Created issue: holomush-XXXX — Session workspace isolation + go.work removal`

- [ ] **Step 2: Record bead ID for later steps**

Note the `holomush-XXXX` ID emitted in step 1. Subsequent commits and the PR body MUST reference this bead.

- [ ] **Step 3: Add dependency on `holomush-rmo2`**

Per `bd dep --help`: `bd dep add <blocked-id> <blocker-id>` makes the first depend on the second. `holomush-rmo2` doesn't strictly block this work (the spec is finished and the implementation is queued), but the relationship is "this PR resolves holomush-rmo2" — best modelled as: this work is the blocker, and holomush-rmo2 is the blocked-by-implementation:

```bash
bd dep add holomush-rmo2 <new-bead-id>
```

Expected: dependency recorded with no error. Verify:

```bash
bd dep list holomush-rmo2
```

Expected output includes `<new-bead-id>` as something `holomush-rmo2` depends on. If `bd dep` returns an error (e.g., circular detection), skip silently and just reference both beads in the commit message instead — the dep relationship is bookkeeping, not load-bearing.

---

## Task 1: Create `scripts/jj-main-repo.sh` shared helper

**Files:**

- Create: `scripts/jj-main-repo.sh`
- Test: smoke-test inline (no formal test file; this is a 12-line shell script)

**Why this task exists:** the spec (Phase 2 implementation note) recommends extracting `MAIN_REPO` discovery into a shared helper to avoid drift between Phase 2 (the task) and Phase 3 (the hook). Both consumers need the `.jj/repo` dir-vs-file distinction.

- [ ] **Step 1: Create the helper script**

Create `scripts/jj-main-repo.sh` with this exact content:

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Sourceable helper. Sets the following variables based on cwd:
#   IS_DEFAULT — "yes" if cwd is the main repo (default jj workspace), else "no"
#   MAIN_REPO  — absolute path to the main repo root
#   WORKTREES  — absolute path to the .worktrees parent dir
#
# Usage (from a Taskfile cmd or a hook script):
#   . "$(git rev-parse --show-toplevel 2>/dev/null || pwd)/scripts/jj-main-repo.sh"
#
# In a jj workspace, .jj/repo is a FILE containing a path (relative to .jj/)
# back to the main checkout's .jj. In the main checkout itself, .jj/repo is
# a DIRECTORY. Verbatim of the technique used by the soon-to-be-deleted
# `gowork` task (Taskfile.yaml:525-530 prior to its removal).

if [ -f ".jj/repo" ]; then
  IS_DEFAULT=no
  POINTER=$(cat ".jj/repo")
  MAIN_REPO=$(cd ".jj/${POINTER}/../.." && pwd -P)
elif [ -d ".jj/repo" ]; then
  IS_DEFAULT=yes
  MAIN_REPO=$(pwd -P)
else
  echo "ERROR: $(pwd) is not a jj repo or workspace (no .jj/repo)" >&2
  return 1 2>/dev/null || exit 1
fi
WORKTREES="$(dirname "$MAIN_REPO")/.worktrees"
```

- [ ] **Step 2: Make it executable (so it can also be invoked standalone for testing)**

Run:

```bash
chmod +x scripts/jj-main-repo.sh
```

- [ ] **Step 3: Smoke-test from the main checkout**

Run from the main repo root (`/Volumes/Code/github.com/holomush/holomush` on the maintainer's machine):

```bash
( . scripts/jj-main-repo.sh && echo "IS_DEFAULT=$IS_DEFAULT MAIN_REPO=$MAIN_REPO WORKTREES=$WORKTREES" )
```

Expected output (paths machine-specific):

```text
IS_DEFAULT=yes MAIN_REPO=/Volumes/Code/github.com/holomush/holomush WORKTREES=/Volumes/Code/github.com/holomush/.worktrees
```

- [ ] **Step 4: Smoke-test from a worktree**

Run from any existing worktree (e.g., `/Volumes/Code/github.com/holomush/.worktrees/pr-b-isolation-spec/`):

```bash
( cd /Volumes/Code/github.com/holomush/.worktrees/pr-b-isolation-spec && . scripts/jj-main-repo.sh && echo "IS_DEFAULT=$IS_DEFAULT MAIN_REPO=$MAIN_REPO WORKTREES=$WORKTREES" )
```

Expected output:

```text
IS_DEFAULT=no MAIN_REPO=/Volumes/Code/github.com/holomush/holomush WORKTREES=/Volumes/Code/github.com/holomush/.worktrees
```

`MAIN_REPO` MUST resolve to the main checkout regardless of which workspace the helper is sourced from. If it resolves to the worktree itself, the helper is broken — go back and re-check the `cd ".jj/${POINTER}/../.."` line.

- [ ] **Step 5: shellcheck the script**

Run:

```bash
shellcheck scripts/jj-main-repo.sh
```

Expected: no output (clean). If shellcheck flags anything, fix it before proceeding.

- [ ] **Step 6: Commit (do not push yet)**

This helper is consumed by Phase 2 and Phase 3 — commit it as part of the Phase 1 commit so it lands first. **Defer the commit to Task 6** (combined Phase 1 commit). Do not commit independently.

---

## Task 2: Phase 1.1 — Delete `go.work` and update `.gitignore`

**Files:**

- Delete: `go.work`
- Modify: `.gitignore` (add `/go.work` and `/go.work.sum`)

- [ ] **Step 1: Verify `go.work` exists at repo root**

Run from repo root:

```bash
ls -la go.work
```

Expected: file shown with last-modified timestamp matching whoever last ran `task gowork`.

- [ ] **Step 2: Read current `.gitignore` to find a sensible insertion point**

Run:

```bash
head -20 .gitignore
```

Note the order of existing entries. The new entries should go near other generated/tooling-output entries (typically near the top, but follow the existing grouping).

- [ ] **Step 3: Append `go.work` patterns to `.gitignore`**

Edit `.gitignore`. Add (preserve any existing trailing newline):

```text
# Go workspace mode is not used by this repo (single-module). If a
# contributor regenerates go.work locally for IDE coverage, keep it local.
/go.work
/go.work.sum
```

Choose insertion point: alongside other Go-tooling outputs if such a section exists; otherwise add at the end of the file.

- [ ] **Step 4: Delete `go.work`**

Run:

```bash
rm go.work
```

- [ ] **Step 5: Verify `go.work` is gone and `.gitignore` is updated**

Run:

```bash
ls go.work 2>&1 | head -1
grep -n 'go\.work' .gitignore
```

Expected:

```text
ls: go.work: No such file or directory
<line>:/go.work
<line>:/go.work.sum
```

- [ ] **Step 6: Commit**

Defer to Task 6 (combined Phase 1 commit).

---

## Task 3: Phase 1.2 — Delete `gowork` task from `Taskfile.yaml`

**Files:**

- Modify: `Taskfile.yaml` (delete lines 515-551, the entire `gowork:` target)

- [ ] **Step 1: Verify the exact range of the `gowork` task**

Run:

```bash
sed -n '515,551p' Taskfile.yaml | head -5
sed -n '551,555p' Taskfile.yaml
```

Expected: line 515 starts `gowork:`, line 551 ends with the `echo` summary, line 552 is blank, line 553 is the next task's section comment (`# ──────────────────────────────────────────`).

If your local `Taskfile.yaml` has shifted line numbers (a parallel session may have edited it), find the actual range using:

```bash
grep -n '^  gowork:' Taskfile.yaml
grep -n '^  # Database migrations' Taskfile.yaml
```

The `gowork` block is everything from `gowork:` through the line BEFORE the next top-level `# ──────────` comment block. Adjust the sed range accordingly.

- [ ] **Step 2: Delete the `gowork` task**

Use Edit to remove the exact block. The `old_string` should be the full `gowork:` block including its trailing blank line; `new_string` should be empty (so the section comment follows the previous task with one blank line).

Verify your Edit by previewing the diff before applying.

- [ ] **Step 3: Verify deletion**

Run:

```bash
grep -n 'gowork' Taskfile.yaml
```

Expected: no output (or only output is in unrelated lines such as comments — verify each).

- [ ] **Step 4: Verify Taskfile YAML is still valid**

Run:

```bash
task --list 2>&1 | head -20
```

Expected: list of tasks (no YAML parse error). `gowork` MUST NOT appear in the list.

- [ ] **Step 5: Commit**

Defer to Task 6.

---

## Task 4: Phase 1.3 — Update CLAUDE.md "jj Workspace Commands"

**Files:**

- Modify: `CLAUDE.md` (lines 552-568 — drop `task gowork` references)

- [ ] **Step 1: Read the current section**

Already mapped above. The exact lines to replace are 557-568 (the code block + the requirement table + the explanatory paragraph).

- [ ] **Step 2: Edit the section**

Replace the existing "jj Workspace Commands" section body (everything from line 554 after the heading through line 568) with this new content (note the outer 4-backtick fence to keep the nested 3-backtick `bash` block intact):

````markdown
Workspaces live in a `.worktrees/` directory that is a sibling of the main repo root
(e.g., `<parent>/.worktrees/<name>`). The exact path is machine-specific.

```bash
jj workspace add <parent>/.worktrees/<name> --name <name> -r main@origin
jj workspace forget <name>  # then: rm -rf <parent>/.worktrees/<name>
```

For the typical case of "start a new isolated Claude session," prefer the
`task workspace:new -- <name>` wrapper (see "Session isolation" below) which
handles `.jj/repo`-based path resolution from any cwd, runs `jj git fetch`
first so `main@origin` is fresh, and is idempotent on re-invocation.
````

```text

(Note: the original `task gowork` line, the MUST-run-task-gowork requirement table, and the gopls-coverage paragraph are all removed. The `--name <name>` arg is preserved from the original since it's a useful jj convention.)

- [ ] **Step 3: Verify the section reads cleanly**

Run:

```bash
sed -n '550,575p' CLAUDE.md
```

Expected: the heading, the new paragraph, the new code block, and the new "Session isolation" forward-reference. No mention of `gowork` or `go.work`. Followed by the existing `### Beads Commands` heading.

- [ ] **Step 4: Commit**

Defer to Task 6.

---

## Task 5: Phase 1.4 — Verify Phase 1 (no `GOWORK=off` workaround needed)

**Files:** _none_ (verification only)

- [ ] **Step 1: Confirm `go.work` is gone, gowork task is gone, CLAUDE.md is updated**

Run:

```bash
ls go.work 2>&1 | head -1
grep -c 'gowork' Taskfile.yaml CLAUDE.md
```

Expected:

```text
ls: go.work: No such file or directory
Taskfile.yaml:0
CLAUDE.md:0
```

- [ ] **Step 2: Run `task pr-prep` WITHOUT `GOWORK=off` (LONG-RUNNING — ~5-15 min)**

This is the critical Phase 1 acceptance check. The whole point of Phase 1 is that `task pr-prep` should now pass without the `GOWORK=off` env hack.

**Run in background** (orchestrators executing this plan: use `run_in_background=true` and poll via TaskOutput; do NOT block synchronously for 15 min):

```bash
task pr-prep 2>&1
```

When complete, read the output file's last 5 lines. Expected last line:

```text
✓ All PR checks passed.
```

**Important caveat**: piping the run through `tail -5` masks the real exit code (tail always exits 0). Read the full output file or check the actual task exit by examining the last line for "✓ All PR checks passed." vs "task: Failed". If the run fails with `module appears multiple times in workspace` or any other go-workspace error, Phase 1 is incomplete — `go.work` is still being generated by something. Investigate (likely a leftover `task gowork` invocation by a hook or CI step).

- [ ] **Step 3: Commit**

Defer to Task 6.

---

## Task 6: Phase 1.5 — Combined Phase 1 commit

**Files:** _none_ (commit only)

- [ ] **Step 1: Verify all Phase 1 changes are present**

Run:

```bash
jj --no-pager st
```

Expected:

```text
Working copy changes:
M .gitignore
M CLAUDE.md
M Taskfile.yaml
A scripts/jj-main-repo.sh
D go.work
```

(The spec file in `docs/superpowers/specs/` and the plan file in `docs/superpowers/plans/` may also be present — they predate Phase 1 work. They MUST stay in this commit since this is the spec-and-plan-implementation PR; verify they're the right versions before continuing.)

- [ ] **Step 2: Describe the commit**

Run:

```bash
JJ_EDITOR=true jj --no-pager describe -m "phase 1: tear down go.work and task gowork

- Delete go.work (single-module repo doesn't need workspace mode)
- Delete task gowork from Taskfile.yaml
- Add /go.work and /go.work.sum to .gitignore
- Drop 'MUST run task gowork' from CLAUDE.md jj Workspace Commands
- Add scripts/jj-main-repo.sh shared helper for Phase 2/3 reuse

Resolves holomush-rmo2 as a side effect: with no multi-worktree go.work,
Go workspace mode no longer rejects multiple worktrees of the same module.

Refs: <bead-id-from-task-0>, holomush-rmo2

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Replace `<bead-id-from-task-0>` with the actual ID from Task 0 step 1.

- [ ] **Step 3: Verify commit**

Run:

```bash
jj --no-pager log -r @ --no-graph
jj --no-pager diff --stat
```

Expected: commit description matches; diff stat shows the 5 files (modify/add/delete).

- [ ] **Step 4: Start the Phase 2 commit (CRITICAL — boundary required)**

Run:

```bash
jj --no-pager new -m "phase 2 (in progress)"
```

This creates a new empty commit on top of Phase 1. Without this, Task 7's edits would land in the Phase 1 commit, and Task 9's `jj describe` would clobber Phase 1's description. Mirror this pattern at the end of Tasks 9 and 13. **Task 16 is the final phase and intentionally OMITS the trailing `jj new`** so Task 18 can set the bookmark on `@` (which IS Phase 4, not an empty trailing commit).

Verify:

```bash
jj --no-pager log -r 'main@origin..@' --no-graph -T 'change_id.short() ++ " " ++ description.first_line() ++ "\n"'
```

Expected: two entries — the new "phase 2 (in progress)" at @, and "phase 1: tear down go.work and task gowork" at @-.

---

## Task 7: Phase 2.1 — Add `task workspace:new` to `Taskfile.yaml`

**Files:**

- Modify: `Taskfile.yaml` (add new `workspace:new` task after the `mod` task / where `gowork` used to live)

- [ ] **Step 1: Locate the insertion point**

Run:

```bash
grep -n '^  mod:' Taskfile.yaml
grep -n '^  # ──' Taskfile.yaml | head -10
```

Insert the new task right where `gowork` used to live — between the `mod:` task and the `# Database migrations` (or whatever the next section comment is).

- [ ] **Step 2: Add the `workspace:new` task**

Edit `Taskfile.yaml`. Insert this block at the location identified in Step 1:

```yaml
  workspace:new:
    desc: |
      Create a fresh jj workspace at <repo-parent>/.worktrees/<name>, fetching
      main@origin first so it's current. Idempotent: if the workspace already
      exists, prints its absolute path and exits 0. Works from any cwd inside
      the repo or any worktree.

      Usage: task workspace:new -- <name>
    cmds:
      - |
        set -euo pipefail
        WS_ROOT="$(jj workspace root)"   # fail-fast if not in a jj repo
        # shellcheck source=scripts/jj-main-repo.sh
        . "$WS_ROOT/scripts/jj-main-repo.sh"

        NAME="${CLI_ARGS:-}"
        [ -n "$NAME" ] || { echo "ERROR: usage: task workspace:new -- <name>" >&2; exit 1; }

        TARGET="$WORKTREES/$NAME"

        # Idempotent pre-check
        if [ -d "$TARGET" ]; then
          echo "$TARGET"
          exit 0
        fi

        # Fresh workspace path
        jj git fetch >&2
        jj workspace add "$TARGET" --name "$NAME" -r main@origin >&2
        echo "$TARGET"
```

(Notes on the structure:

- `set -euo pipefail` — fail loudly on errors, undefined vars, pipe failures.
- `WS_ROOT="$(jj workspace root)"` + the single `.` source line uses `set -euo pipefail` to fail-fast if cwd is not in a jj repo. The helper is sourced from the workspace root path (not cwd-relative), so it works regardless of where `task workspace:new` was invoked.
- `CLI_ARGS` is the Taskfile convention for `--`-separated args (per CLAUDE.md "Test commands accept arguments after `--`").
- All progress output (`jj git fetch`, `jj workspace add`) goes to stderr so `tail -n 1` of stdout reliably gets the path.
- The final `echo "$TARGET"` is the contracted "absolute path on last line of stdout" (Phase 2 DoD).)

- [ ] **Step 3: Verify YAML and task discovery**

Run:

```bash
task --list 2>&1 | grep workspace
```

Expected: `* workspace:new:` appears with the description text.

- [ ] **Step 4: Commit**

Defer to Task 9.

---

## Task 8: Phase 2.2 — Test `task workspace:new` from repo root and from worktree

**Files:** _none_ (verification only)

- [ ] **Step 1: Test from the main checkout**

Run from `/Volumes/Code/github.com/holomush/holomush`:

```bash
PATH_OUT=$(task workspace:new -- ws-test-root | tail -n 1)
echo "Got: $PATH_OUT"
test "$PATH_OUT" = "/Volumes/Code/github.com/holomush/.worktrees/ws-test-root" \
  && echo "PASS" || echo "FAIL"
ls -la /Volumes/Code/github.com/holomush/.worktrees/ws-test-root/.jj/repo
```

Expected:

```text
Got: /Volumes/Code/github.com/holomush/.worktrees/ws-test-root
PASS
<path to file pointer> (regular file, ~26 bytes)
```

- [ ] **Step 2: Test idempotence (re-run with same name)**

Run again immediately:

```bash
PATH_OUT=$(task workspace:new -- ws-test-root | tail -n 1)
test "$PATH_OUT" = "/Volumes/Code/github.com/holomush/.worktrees/ws-test-root" \
  && echo "PASS (idempotent)" || echo "FAIL"
```

Expected: prints `PASS (idempotent)`. The `jj git fetch` and `jj workspace add` lines should NOT have run (no stderr output beyond what task itself produces).

- [ ] **Step 3: Test from inside a worktree**

Run:

```bash
cd /Volumes/Code/github.com/holomush/.worktrees/ws-test-root
PATH_OUT=$(task workspace:new -- ws-test-from-worktree | tail -n 1)
test "$PATH_OUT" = "/Volumes/Code/github.com/holomush/.worktrees/ws-test-from-worktree" \
  && echo "PASS" || echo "FAIL: got $PATH_OUT"
```

Expected: `PASS`. Critically, the path MUST NOT be `/Volumes/Code/github.com/holomush/.worktrees/ws-test-root/.worktrees/ws-test-from-worktree` (which would be the failure mode if MAIN_REPO discovery is wrong).

- [ ] **Step 4: Clean up the test workspaces**

Run from repo root:

```bash
cd /Volumes/Code/github.com/holomush/holomush
jj --no-pager workspace forget ws-test-root
jj --no-pager workspace forget ws-test-from-worktree
rm -rf /Volumes/Code/github.com/holomush/.worktrees/ws-test-root
rm -rf /Volumes/Code/github.com/holomush/.worktrees/ws-test-from-worktree
```

- [ ] **Step 5: Verify cleanup**

Run:

```bash
jj --no-pager workspace list | grep -E 'ws-test-' || echo "PASS (no leftovers)"
```

Expected: `PASS (no leftovers)`.

- [ ] **Step 6: Commit**

Defer to Task 9.

---

## Task 9: Phase 2 commit

**Files:** _none_ (commit only)

- [ ] **Step 1: Verify the diff is just Taskfile.yaml**

Run:

```bash
jj --no-pager diff --stat
```

Expected: only `Taskfile.yaml` should show as modified (the `scripts/jj-main-repo.sh` was already added in Phase 1).

- [ ] **Step 2: Describe the Phase 2 commit**

The Phase 2 changes are already in `@` (Tasks 7-8 edited Taskfile.yaml; Task 6 step 4 created the boundary `jj new`). Use `jj describe` to set the message — do NOT use `jj new` here, which would create yet another empty commit and strand Phase 2's changes in the "phase 2 (in progress)" placeholder.

Run:

```bash
JJ_EDITOR=true jj --no-pager describe -m "phase 2: add task workspace:new

Adds the agent-friendly wrapper for creating an isolated jj workspace
in one command. Resolves MAIN_REPO via the .jj/repo dir-vs-file
technique (sourced from scripts/jj-main-repo.sh, added in Phase 1)
so it works correctly from any cwd inside the repo or any worktree.

Idempotent: re-running with an existing name prints the existing
workspace path and exits 0.

Per spec, no task claude:isolated target — see Phase 4 for the
human-shell-function alternative documented in CLAUDE.md.

Refs: <bead-id-from-task-0>

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 3: Start the Phase 3 commit boundary**

Run:

```bash
jj --no-pager new -m "phase 3 (in progress)"
```

- [ ] **Step 4: Verify**

Run:

```bash
jj --no-pager log -r 'main@origin..@' --no-graph -T 'change_id.short() ++ " " ++ description.first_line() ++ "\n"'
```

Expected: three entries (the new "phase 3 (in progress)" at @, then phase 2, then phase 1):

```text
<id>  phase 3 (in progress)
<id>  phase 2: add task workspace:new
<id>  phase 1: tear down go.work and task gowork
```

---

## Task 10: Phase 3.1 — Create `warn-default-workspace.sh` SessionStart hook

**Files:**

- Create: `.claude/hooks/warn-default-workspace.sh`

- [ ] **Step 1: Create the hook script**

Create `.claude/hooks/warn-default-workspace.sh` with this exact content:

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# SessionStart hook: warns the assistant when the Claude Code session is
# operating in the shared `default` jj workspace. Stays silent when the
# session is in any other workspace.
#
# Output contract: emit warning text to plain stdout (the Claude Code
# SessionStart hook concatenates stdout into the session's additional
# context). Stay silent (exit 0, no output) when no warning is needed.

set -euo pipefail

# Consume the JSON event from stdin (we don't need any field; just being
# polite to the hook contract).
cat >/dev/null

# Source the shared MAIN_REPO/IS_DEFAULT helper. The hook script's cwd is
# the Claude session's launching cwd, so .jj/repo resolution from . is
# the right starting point.
ws_root="$(jj workspace root 2>/dev/null || true)"
if [ -z "$ws_root" ] || [ ! -e "$ws_root/scripts/jj-main-repo.sh" ]; then
  # Not in a jj repo, or helper missing — silently exit. The hook is
  # purely informational; never block session start.
  exit 0
fi

# shellcheck source=../../scripts/jj-main-repo.sh
( cd "$ws_root" && . "$ws_root/scripts/jj-main-repo.sh" >/dev/null 2>&1 ) || exit 0

# Re-source in current shell to populate IS_DEFAULT (the subshell above
# only validated the script doesn't error; we need the var here).
cd "$ws_root"
# shellcheck source=../../scripts/jj-main-repo.sh
. "$ws_root/scripts/jj-main-repo.sh"

if [ "${IS_DEFAULT:-no}" != "yes" ]; then
  exit 0
fi

cat <<'EOF'
**You are in the shared `default` jj workspace.** If you intend to edit files, another Claude Code session in the same workspace can collide with your edits at any `jj` command boundary (jj snapshots the working copy on every command). To isolate this session, exit and:

- **Humans:** run `claude-iso <name>` (the shell function in `~/.config/fish/config.fish` or `~/.bashrc` — see CLAUDE.md "Session isolation" for the snippet)
- **Agents (or humans without the function):** run `task workspace:new -- <name>`, then `cd <printed-path> && claude`

To ignore this warning, continue as normal.
EOF
```

(Notes:

- `cat >/dev/null` consumes stdin — Claude Code may send JSON; we don't need any field.
- The two-stage source is defensive: first a subshell validation (so a broken helper doesn't kill the hook), then the real source in the current shell to set `IS_DEFAULT`.
- `cd "$ws_root"` matters: `jj-main-repo.sh` uses `[ -f ".jj/repo" ]` etc. with cwd-relative paths.
- `cat <<'EOF'` with single-quoted heredoc preserves backticks and `$` literally — the warning prose contains both.)

- [ ] **Step 2: Make it executable**

Run:

```bash
chmod +x .claude/hooks/warn-default-workspace.sh
```

- [ ] **Step 3: Verify file is in place and executable**

Run:

```bash
ls -la .claude/hooks/warn-default-workspace.sh
```

Expected: file is present, mode `-rwxr-xr-x` (or equivalent showing execute bit).

- [ ] **Step 4: Commit**

Defer to Task 13.

---

## Task 11: Phase 3.2 — Smoke-test the hook from default + non-default + shellcheck

**Files:** _none_ (verification only)

- [ ] **Step 1: Test from the main checkout (default workspace)**

Run from `/Volumes/Code/github.com/holomush/holomush`:

```bash
echo '{}' | ./.claude/hooks/warn-default-workspace.sh
```

Expected: prints the warning text starting with "**You are in the shared `default` jj workspace.**" and ending with "To ignore this warning, continue as normal."

- [ ] **Step 2: Test from a worktree (non-default)**

Run from `/Volumes/Code/github.com/holomush/.worktrees/pr-b-isolation-spec/`:

```bash
echo '{}' | ./.claude/hooks/warn-default-workspace.sh
echo "---exit was $?---"
```

Expected:

```text
---exit was 0---
```

(No warning text. Exit 0. Hook stayed silent.)

- [ ] **Step 3: Test that empty JSON is handled (Claude Code may send non-empty JSON)**

Run:

```bash
echo '{"hookEventName":"SessionStart","sessionId":"abc"}' | ./.claude/hooks/warn-default-workspace.sh
```

Expected: same behavior as step 1 or 2 (depending on cwd) — the hook should not care about the JSON contents.

- [ ] **Step 4: Test that hook is robust to non-jj cwd**

Run:

```bash
( cd /tmp && echo '{}' | /Volumes/Code/github.com/holomush/holomush/.claude/hooks/warn-default-workspace.sh )
echo "exit=$?"
```

Expected: `exit=0` and no stdout output. The hook exits silently when not in a jj repo (per the `[ -z "$ws_root" ]` guard).

- [ ] **Step 5: shellcheck the hook**

Run:

```bash
shellcheck .claude/hooks/warn-default-workspace.sh
```

Expected: no output (clean). If there are warnings:

- For `SC1091` (sourced file not found): add `# shellcheck source=path` directive (already in the script)
- For other warnings: fix or add a justified `# shellcheck disable=SCxxxx` comment

- [ ] **Step 6: Commit**

Defer to Task 13.

---

## Task 12: Phase 3.3 — Wire the hook into `.claude/settings.json`

**Files:**

- Modify: `.claude/settings.json` (add the new hook to `hooks.SessionStart`)

- [ ] **Step 1: Read the current SessionStart entry**

Run:

```bash
jq '.hooks.SessionStart' .claude/settings.json
```

Expected: an array with one entry — the existing `bd prime` hook.

- [ ] **Step 2: Add the new hook to the SessionStart array**

Edit `.claude/settings.json`. Find the `SessionStart` array and replace it with:

```json
"SessionStart": [
  {
    "hooks": [
      {
        "command": "bd prime",
        "type": "command"
      }
    ],
    "matcher": ""
  },
  {
    "hooks": [
      {
        "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/warn-default-workspace.sh",
        "type": "command"
      }
    ],
    "matcher": ""
  }
]
```

(Note: keeping the new hook in a separate `hooks[]` group preserves independent matcher semantics in case future matchers diverge. Same pattern as the existing `UserPromptSubmit` hook from PR #266.)

- [ ] **Step 3: Validate JSON**

Run:

```bash
jq -e . .claude/settings.json > /dev/null && echo "JSON valid"
```

Expected: `JSON valid`.

- [ ] **Step 4: Verify SessionStart now has both hooks**

Run:

```bash
jq '.hooks.SessionStart | length' .claude/settings.json
```

Expected: `2`.

- [ ] **Step 5: Commit**

Defer to Task 13.

---

## Task 13: Phase 3 commit

**Files:** _none_ (commit only)

- [ ] **Step 1: Verify the diff is the hook + settings**

Run:

```bash
jj --no-pager diff --stat
```

Expected:

```text
.claude/hooks/warn-default-workspace.sh | <N> +++++++
.claude/settings.json                   | 11 +++++++
```

- [ ] **Step 2: Describe the commit**

Run:

```bash
JJ_EDITOR=true jj --no-pager describe -m "phase 3: SessionStart hook warns when starting in default workspace

Adds .claude/hooks/warn-default-workspace.sh — a soft-enforcement hook
that emits a warning to plain stdout (concatenated into the session's
additional context per Claude Code SessionStart hook contract) when
the session is launched in the shared default jj workspace.

Detection uses the .jj/repo dir-vs-file technique (via the shared
scripts/jj-main-repo.sh helper added in Phase 1). Silent in any
non-default workspace.

Wired in .claude/settings.json alongside the existing bd prime
SessionStart hook.

Refs: <bead-id-from-task-0>

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 3: Start the Phase 4 commit**

Run:

```bash
jj --no-pager new -m "phase 4 (in progress)"
```

---

## Task 14: Phase 4.1 — Add CLAUDE.md "Session isolation" subsection

**Files:**

- Modify: `CLAUDE.md` (insert new `### Session isolation` between `### jj Workspace Commands` (line 552) and `### Beads Commands` (line 570))

- [ ] **Step 1: Re-locate the insertion point (line numbers may have shifted from Phase 1.3 edits)**

Run:

```bash
grep -n '^### jj Workspace Commands\|^### Beads Commands' CLAUDE.md
```

Expected: two lines reported. Insert between them.

- [ ] **Step 2: Insert the new subsection**

Edit `CLAUDE.md`. Insert this new subsection AFTER the `### jj Workspace Commands` block (after its closing paragraph) and BEFORE the `### Beads Commands` heading. Prepend a blank line for separation:

````markdown

### Session isolation

This repo is developed primarily by concurrent AI agent sessions. Because jj
snapshots the working copy on every command, two sessions sharing the same
jj workspace will collide on uncommitted edits. To prevent this:

| Requirement | Description |
|---|---|
| **MUST** isolate per session | Start each Claude session in its own jj workspace. Humans: `claude-iso <name>` (shell function below). Agents: `task workspace:new -- <name>`, then `cd <printed-path> && claude` |
| **SHOULD NOT** edit files in `default` | The `default` workspace is reserved for read-only inspection and one-off throwaway work. A `SessionStart` hook warns when a session begins there |
| **MUST** clean up post-merge | After your branch lands, `jj workspace forget <name> && rm -rf ../.worktrees/<name>` from any workspace. (See "Landing the Plane.") |

**`claude-iso` shell function** — copy into your shell's rc file:

```fish
# fish: ~/.config/fish/config.fish
#
# IMPORTANT: `set var (cmd | tail -n 1); or ...` does NOT propagate the
# failure of `cmd` because the pipeline's exit status is `tail`'s, not
# `cmd`'s. We therefore call `task workspace:new` twice — first to check
# the exit status, then again inside command substitution to capture the
# path. The second call is idempotent (Phase 2 DoD requirement) and just
# prints the path for an existing workspace.
function claude-iso
    set name $argv[1]
    task workspace:new -- $name >/dev/null
    or return $status
    set ws (task workspace:new -- $name | tail -n 1)
    cd $ws
    or return $status
    exec claude
end
```

```bash
# bash/zsh: ~/.bashrc or ~/.zshrc
#
# Same caveat as fish: $(cmd | tail -n 1) carries tail's exit status, not
# cmd's. Two-call pattern; second call is idempotent.
claude-iso() {
  local name="$1"
  task workspace:new -- "$name" >/dev/null || return $?
  local ws
  ws="$(task workspace:new -- "$name" | tail -n 1)"
  cd "$ws" || return $?
  exec claude
}
```

New worktrees inherit `.claude/` (tracked in git), so `SessionStart`,
`UserPromptSubmit`, and other Claude Code hooks fire identically in any
worktree — no hook re-wiring is needed when creating a workspace.

Sub-agents launched via the `Task` tool inherit the parent's workspace. The
parent is responsible for not dispatching parallel `Task` calls that would
edit the same files. (Future work MAY add per-`Task` workspace creation.)

````

- [ ] **Step 3: Verify the section reads cleanly**

Run:

```bash
grep -n '^### jj Workspace\|^### Session isolation\|^### Beads Commands' CLAUDE.md
```

Expected: three lines in the right order.

- [ ] **Step 4: Verify markdown renders correctly (no broken fences)**

Run:

```bash
sed -n '/^### Session isolation/,/^### Beads Commands/p' CLAUDE.md | head -100
```

Visually inspect: code fences (```fish, ```bash, ``` closing) all match. The outer code fence used for the snippet was four backticks (`` ```` ``) so the inner three-backtick blocks are nested correctly.

- [ ] **Step 5: Commit**

Defer to Task 16.

---

## Task 15: Phase 4.2 — Expand "Landing the Plane" step 5 in place

**Files:**

- Modify: `CLAUDE.md` (line 849 — the existing `5. Clean up - ...` line)

- [ ] **Step 1: Locate the line**

Run:

```bash
grep -n '^5\. \*\*Clean up\*\*\|^5\. Clean up' CLAUDE.md
```

Expected: one line (current text: `5. **Clean up** - Clear stashes, prune remote branches, jj workspace forget unused workspaces`).

- [ ] **Step 2: Replace step 5 with the expanded version**

Edit `CLAUDE.md`. Replace the single-line step 5 with this expanded version (preserving `5.` and the **Clean up** label):

````markdown
5. **Clean up** — clear stashes, prune remote branches, and (if this work was done in a dedicated workspace per the "Session isolation" discipline) forget and remove the workspace:

   ```bash
   cd <repo-root>                           # exit the workspace before forgetting it
   jj workspace forget <name>
   rm -rf <repo-parent>/.worktrees/<name>
   ```
````

(Note the indentation: the fenced code block is indented by 3 spaces to remain inside the numbered list item.)

- [ ] **Step 3: Verify the surrounding numbered list is still correct**

Run:

```bash
sed -n '/^## Landing the Plane/,/^## /p' CLAUDE.md | grep -E '^[0-9]+\.' | head -10
```

Expected: numbered items 1, 2, 3, 4, 5, 6, 7 in order. Steps 6 and 7 unchanged.

- [ ] **Step 4: Commit**

Defer to Task 16.

---

## Task 16: Phase 4 commit

**Files:** _none_ (commit only)

- [ ] **Step 1: Verify the diff is just CLAUDE.md**

Run:

```bash
jj --no-pager diff --stat
```

Expected: only `CLAUDE.md` modified.

- [ ] **Step 2: Describe the Phase 4 commit**

Run:

```bash
JJ_EDITOR=true jj --no-pager describe -m "phase 4: document session-isolation discipline in CLAUDE.md

Add new ### Session isolation subsection under ## Commands, with:
- A 3-row requirements table (MUST isolate per session, SHOULD NOT edit
  in default, MUST clean up post-merge)
- The claude-iso shell-function snippets (fish + bash variants), with
  the two-call idiom and inline comment explaining why
- Notes that .claude/ is inherited by new worktrees, and that Task-tool
  sub-agents inherit the parent's workspace

Expand 'Landing the Plane' step 5 in place to spell out the workspace
forget-and-rm sequence (no renumbering of subsequent steps).

Refs: <bead-id-from-task-0>

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 3: Verify the four-commit chain**

Phase 4 is the last implementation phase — do NOT create a trailing empty `jj new` (Task 18 sets the bookmark on `@`, which is Phase 4 itself). Verify:

```bash
jj --no-pager log -r 'main@origin..@' --no-graph -T 'change_id.short() ++ " " ++ description.first_line() ++ "\n"'
```

Expected: four entries, in order from @ down — Phase 4 at @, then Phase 3, Phase 2, Phase 1.

---

## Task 17: Pre-PR verification — full acceptance pass

**Files:** _none_ (verification only)

This task verifies all 7 spec acceptance criteria.

- [ ] **Step 1: Acceptance #1 — `go.work` does not exist; gitignored**

Run:

```bash
ls go.work 2>&1 | head -1
grep -E '^/go\.work' .gitignore
```

Expected:

```text
ls: go.work: No such file or directory
/go.work
/go.work.sum
```

- [ ] **Step 2: Acceptance #2 — `task gowork` does not exist**

Run:

```bash
task --list 2>&1 | grep -i gowork && echo "FAIL" || echo "PASS"
grep -n 'gowork' Taskfile.yaml CLAUDE.md
```

Expected: `PASS` (no `gowork` task), and grep returns no matches.

- [ ] **Step 3: Acceptance #3 — `task workspace:new` works from repo root and worktree, idempotent**

Re-run Task 8 verification steps (creates and cleans up `ws-test-root` and `ws-test-from-worktree` workspaces). All three sub-checks must PASS.

- [ ] **Step 4: Acceptance #4 — SessionStart warning fires only in default**

Re-run Task 11 steps 1-2:

```bash
( cd /Volumes/Code/github.com/holomush/holomush && echo '{}' | ./.claude/hooks/warn-default-workspace.sh | head -1 )
( cd /Volumes/Code/github.com/holomush/.worktrees/pr-b-isolation-spec && echo '{}' | ./.claude/hooks/warn-default-workspace.sh | head -1 )
```

Expected: first command prints the warning's first line; second command prints nothing.

- [ ] **Step 5: Acceptance #5 — CLAUDE.md documents the discipline**

Run:

```bash
grep -n '^### Session isolation\|claude-iso' CLAUDE.md | head -5
```

Expected: section heading found; `claude-iso` referenced multiple times (once in the table, once each in the fish + bash snippets).

- [ ] **Step 6: Acceptance #6 — `holomush-rmo2` to be closed (after merge)**

Verification of this criterion happens AFTER PR merge. Run from any workspace:

```bash
bd show holomush-rmo2
```

Note the current status. After merge (Task 19), this becomes:

```bash
bd close holomush-rmo2 --reason "Resolved by removal of go.work + task gowork in PR #<N>"
```

For now, just confirm `holomush-rmo2` exists and is currently open:

```bash
bd show holomush-rmo2 | grep -i status
```

Expected: `Status: open`.

- [ ] **Step 7: Acceptance #7 — concurrent workspace edits don't collide (automated, single-shell)**

Run this as one shell pipeline from the repo root. File writes inside each workspace dir don't trigger snapshots — only `jj` invocations do — so we explicitly run `jj st` in each workspace to force snapshots, then inspect the result:

```bash
set -euo pipefail
WS_A=$(task workspace:new -- collide-test-a | tail -n 1)
WS_B=$(task workspace:new -- collide-test-b | tail -n 1)

# Distinct edits in each workspace
( cd "$WS_A" && echo "test-a" > collide-test-a-marker.txt )
( cd "$WS_B" && echo "test-b" > collide-test-b-marker.txt )

# Force jj to snapshot each workspace's working copy
( cd "$WS_A" && jj --no-pager st >/dev/null )
( cd "$WS_B" && jj --no-pager st >/dev/null )

# Inspect — should show two distinct change IDs, one per workspace
echo "--- mutable, non-empty changes (one per workspace) ---"
jj --no-pager log -r 'mutable() & ~empty()' --no-graph \
  -T 'change_id.short() ++ " " ++ working_copies ++ " " ++ description.first_line() ++ "\n"'

# Verify the change IDs are actually different
A_CHANGE=$( cd "$WS_A" && jj --no-pager log -r @ --no-graph -T 'change_id.short()' )
B_CHANGE=$( cd "$WS_B" && jj --no-pager log -r @ --no-graph -T 'change_id.short()' )
if [ "$A_CHANGE" != "$B_CHANGE" ]; then
  echo "PASS: distinct change IDs ($A_CHANGE vs $B_CHANGE)"
else
  echo "FAIL: same change ID ($A_CHANGE) — workspaces collided"
  exit 1
fi

# Cleanup
jj --no-pager workspace forget collide-test-a
jj --no-pager workspace forget collide-test-b
rm -rf "$WS_A" "$WS_B"
echo "--- cleaned up ---"
```

Expected output: the `jj log` shows two entries with `collide-test-a@` and `collide-test-b@` in the `working_copies` column with distinct change IDs, the script prints `PASS: distinct change IDs (...)`, and cleanup runs without error.

If `FAIL: same change ID` appears (script exits 1), the workspace isolation is broken — STOP and investigate before approving the PR.

- [ ] **Step 8: Run `task pr-prep` without `GOWORK=off` (LONG-RUNNING — ~5-15 min)**

This is the final pre-push gate per CLAUDE.md "MUST run task pr-prep". Run in background and read the output file's last line on completion (avoid the `tail -3` exit-code-masking trap noted in Task 5):

```bash
task pr-prep 2>&1
```

Expected last line: `✓ All PR checks passed.`

If anything else (e.g., `task: Failed to run task "pr-prep"`), STOP and address.

---

## Task 18: Code review + push + PR

**Files:** _none_ (review and PR)

- [ ] **Step 1: Adversarial code review**

Per CLAUDE.md "Pre-Push Review Gates" and the just-merged PR #266 trigger hooks, the `code-reviewer` MUST run before `jj git push` / `gh pr create`. Invoke:

```text
/review-code
```

(Or equivalent: dispatch `Agent` with `subagent_type: code-reviewer`. The slash command from PR #266 invokes the agent appropriately.)

Expected: review runs against the branch diff, returns a verdict. If NOT READY, address blocking findings inline; re-run review until READY.

- [ ] **Step 2: Set the bookmark on the Phase 4 commit**

After Task 16, `@` IS the Phase 4 commit (Task 16 step 3 explicitly does NOT create a trailing `jj new`). The bookmark targets `@`:

```bash
jj --no-pager bookmark create chore/session-workspace-isolation -r @
```

Verify:

```bash
jj --no-pager log -r 'main@origin..chore/session-workspace-isolation' --no-graph -T 'change_id.short() ++ " " ++ description.first_line() ++ "\n"'
```

Expected: four commits — phase 1, phase 2, phase 3, phase 4. If only THREE appear, the bookmark is on the wrong commit (Phase 4 was omitted) — STOP and investigate before pushing.

- [ ] **Step 3: Push**

Run:

```bash
jj --no-pager git push --bookmark chore/session-workspace-isolation
```

Expected: push succeeds. Note the SHA reported.

- [ ] **Step 4: Open the PR**

From the main checkout (`gh` requires git context):

```bash
cd /Volumes/Code/github.com/holomush/holomush
GIT_SSL_NO_VERIFY=1 gh pr create \
  --head chore/session-workspace-isolation \
  --base main \
  --title "chore: session-workspace isolation + drop go.work" \
  --body "$(cat <<'EOF'
## Summary

Closes the cross-session collision class that bit PR #266 (parallel sessions in the shared `default` jj workspace stomping each other), and removes the `go.work` infrastructure that was the structural barrier to "always create a workspace per session."

Spec: `docs/superpowers/specs/2026-04-25-session-workspace-isolation-design.md` (3 rounds of design-review passed: NOT READY → NOT READY → READY).

Plan: `docs/superpowers/plans/2026-04-25-session-workspace-isolation.md`.

Tracking bead: `<bead-id-from-task-0>`. Resolves `holomush-rmo2` as a side effect of Phase 1.

## Phases (4 commits)

1. **Phase 1: tear down `go.work` + `task gowork`** — single-module repo doesn't need workspace mode; `holomush-rmo2`'s underlying cause goes with it. Adds `scripts/jj-main-repo.sh` helper for Phase 2/3 reuse.
2. **Phase 2: `task workspace:new -- <name>`** — agent-friendly wrapper that handles `.jj/repo`-based MAIN_REPO discovery from any cwd, fetches `main@origin`, idempotent.
3. **Phase 3: SessionStart hook** — `.claude/hooks/warn-default-workspace.sh` warns to plain stdout when starting in the `default` workspace; silent in any other.
4. **Phase 4: CLAUDE.md docs** — new `### Session isolation` subsection (with fish + bash `claude-iso` snippets), `Landing the Plane` step 5 expanded in place.

## Test plan

- [x] Phase 1: `task pr-prep` passes WITHOUT `GOWORK=off`
- [x] Phase 2: `task workspace:new -- foo` works from repo root AND from inside any worktree; idempotent on re-invocation; absolute path on last line of stdout
- [x] Phase 3: hook emits warning at default workspace; silent in any other; shellcheck-clean
- [x] Phase 4: section renders correctly (nested code fences validated); "Landing the Plane" numbering preserved
- [x] Acceptance #7: two concurrent workspaces editing different files leave distinct change IDs (manual smoke per spec)

## Post-merge follow-up

- `bd close holomush-rmo2 --reason "Resolved by go.work removal in #<this-PR>"`
- Existing in-progress workspaces from before this PR keep working — no migration needed (per spec non-goal #5)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Note: replace `<bead-id-from-task-0>` with the actual bead ID. Capture the PR URL from the output.

---

## Task 19: Post-merge cleanup

**Files:** _none_ (post-merge VCS housekeeping)

This task runs AFTER the PR merges to main. The maintainer will trigger it.

- [ ] **Step 1: Fetch the merged state**

Run:

```bash
jj --no-pager git fetch
```

Expected output mentions `chore/session-workspace-isolation@origin [deleted]` and `main@origin [updated]`.

- [ ] **Step 2: Close `holomush-rmo2`**

Run:

```bash
bd close holomush-rmo2 --reason "Resolved by go.work removal in PR #<N>"
```

Replace `<N>` with the merged PR number.

- [ ] **Step 3: Close the tracking bead**

Run:

```bash
bd close <bead-id-from-task-0> --reason "Implemented in PR #<N>"
```

- [ ] **Step 4: Forget the workspace this PR was built in**

Run from the main checkout:

```bash
cd /Volumes/Code/github.com/holomush/holomush
jj --no-pager workspace forget pr-b-isolation-spec
rm -rf /Volumes/Code/github.com/holomush/.worktrees/pr-b-isolation-spec
```

Verify cleanup:

```bash
jj --no-pager workspace list | grep pr-b-isolation-spec || echo "PASS (cleaned up)"
ls /Volumes/Code/github.com/holomush/.worktrees/pr-b-isolation-spec 2>&1 | head -1
```

Expected:

```text
PASS (cleaned up)
ls: /Volumes/Code/github.com/holomush/.worktrees/pr-b-isolation-spec: No such file or directory
```

- [ ] **Step 5: bd dolt push**

Run:

```bash
bd dolt push
```

Expected: beads DB synced to remote with the closed beads.

---

## Self-review checklist

After this plan was written, the author ran the following checks:

**Spec coverage (all 7 acceptance criteria):**

- #1 (go.work gone, gitignored) → Tasks 2 + Task 17 step 1
- #2 (task gowork gone) → Task 3 + Task 17 step 2
- #3 (task workspace:new works from anywhere, idempotent) → Tasks 7-8 + Task 17 step 3
- #4 (hook fires only in default) → Tasks 10-12 + Task 17 step 4
- #5 (CLAUDE.md documents discipline) → Tasks 14-15 + Task 17 step 5
- #6 (holomush-rmo2 closed) → Task 19 step 2
- #7 (concurrent workspaces don't collide) → Task 17 step 7

**Placeholder scan:** none of "TBD", "TODO", "implement later", "fill in details", "add appropriate error handling", "Write tests for the above", "Similar to Task N" appear in this plan. All steps have either complete code or complete commands.

**Type/name consistency:**

- Helper script: `scripts/jj-main-repo.sh` (consistent across Tasks 1, 7, 10)
- Helper exports: `IS_DEFAULT`, `MAIN_REPO`, `WORKTREES` (consistent)
- Hook script: `.claude/hooks/warn-default-workspace.sh` (consistent across Tasks 10-12, 17)
- Task target: `task workspace:new -- <name>` (consistent across Tasks 7, 8, 14, 17, plan body, PR description)
- Bookmark: `chore/session-workspace-isolation` (Task 18)
- Reference path used in claude-iso snippets: `task workspace:new` (matches Task 7's actual definition)

**Spec requirements not yet covered:** none.

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-04-25-session-workspace-isolation.md`.** Two execution options:

**1. Subagent-Driven (recommended)** — dispatch a fresh subagent per task, review between tasks, fast iteration. Each subagent gets ONE task and a constrained context. Good fit for this plan because phases are independent and reviewable in isolation.

**2. Inline Execution** — execute tasks in this session via `superpowers:executing-plans`, batch with checkpoints for review. Good fit if you want to see each step's output in this conversation.

**Which approach?**
