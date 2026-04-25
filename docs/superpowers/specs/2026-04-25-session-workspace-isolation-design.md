# Session Workspace Isolation + go.work Removal

**Date:** 2026-04-25
**Status:** Design — READY (3 rounds of design-review passed 2026-04-25)
**Tracking bead:** _to be filed_
**Resolves:** `holomush-rmo2` (as a side effect of removing `go.work`)
**Related:** `holomush-krsq` (#264 — reviewer report persistence), `holomush-rsu` (#266 — reviewer trigger hooks)

## Problem

HoloMUSH is developed almost entirely by AI agents (Claude Code and others) running against a jj-colocated git repository. In practice, multiple Claude Code sessions and `Task`-tool sub-agents frequently operate concurrently against the same repo. Because jj snapshots the working copy on every command, two concurrent sessions sharing the **same workspace** (typically the `default` workspace at the repo root) collide deterministically:

1. Session A is editing files for change `pwq`. The edits exist on disk but have not been committed.
2. Session B runs `jj edit yu` (a different change) for unrelated work.
3. jj snapshots disk into `pwq` (capturing whatever A had so far) and switches `@` to `yu`.
4. Session A's next disk edit is now snapshotted into `yu`'s diff — the wrong commit.

This happened during PR #266 development on 2026-04-25: a parallel cloud-init session in the `default` workspace caused 3 of 5 in-flight `.claude/*` edits to be split across two unrelated commits. Recovery required creating a new workspace, restoring the original commit, and re-applying lost edits.

The structural barrier to "just always create a workspace" is **`go.work`**:

- `task gowork` discovers every `.worktrees/*/go.mod` and writes a `use <path>` line per worktree
- Every worktree is a separate copy of the same Go module (`github.com/holomush/holomush`)
- Go workspace mode rejects multiple `use` entries pointing to the same module path: `module appears multiple times in workspace`
- Result: the more workspaces you create, the more broken `go.work` becomes (`holomush-rmo2`)

`go.work`'s sole stated purpose in this repo (per CLAUDE.md) is "so gopls covers all active workspaces without 'not in workspace' LSP diagnostics. Works from any workspace." That is a feature for human editors. The maintainer does not use a human editor against this repo.

## Goals

1. **MUST eliminate the cross-session collision class** demonstrated by the PR #266 incident: two Claude Code sessions doing edits in the same jj working copy.
2. **MUST remove `go.work` and `task gowork` entirely**, resolving `holomush-rmo2` as a side effect and removing the structural barrier to creating workspaces freely.
3. **MUST make per-session workspace isolation cheap** — a single command starts a new isolated Claude session.
4. **MUST surface a soft warning** when a Claude session is operating in the shared `default` workspace, so even users who don't opt in to isolation are informed before they collide with someone.
5. **MUST document the new operating model** in CLAUDE.md so contributors (human or AI) understand the isolation discipline.
6. **SHOULD update the existing "Landing the Plane" workflow** to incorporate workspace cleanup as part of the post-merge sequence.

## Non-goals

1. **Hard enforcement at the OS level** (e.g., a `claude` launcher that refuses to start in `default`). The maintainer prefers soft nudges over hard blocks for this class of problem.
2. **Sub-agent (`Task` tool) auto-isolation.** Sub-agents inherit the parent's working copy. Per the brainstorming round, this is lower-blast-radius than parallel sessions and the parent can supervise. A future change MAY add per-`Task`-call workspace creation; this spec does not.
3. **Replacing LSP entirely.** Whether the maintainer disables the Claude Code `LSP` MCP tool globally is out of scope. This spec only removes `go.work` from the repo and stops shipping LSP-supporting infrastructure here.
4. **Cross-worktree `gopls` coverage.** Anyone wanting that can write their own per-worktree `go.work` locally; it just isn't shipped or maintained.
5. **Rewriting existing in-progress workspaces.** Whatever workspaces exist at the time of merge stay as they are. The new policy applies to sessions started after merge.

## Design

The work is decomposed into four phases. Each is independently mergeable.

### Phase 1 — Tear down `go.work`

| Action | Detail |
|---|---|
| Delete `go.work` from the repo root | Tracked file; remove via VCS |
| Delete the `gowork` task | Removed from `Taskfile.yaml` (or wherever it lives) |
| Add `/go.work` and `/go.work.sum` to `.gitignore` | So locally-generated files don't accidentally get committed (`go.work.sum` is created by `go work sync` if anyone regenerates locally) |
| Update CLAUDE.md "jj Workspace Commands" section | Drop the "MUST run `task gowork` after every workspace add/forget" rule. The replacement workflow is just `jj workspace add`/`jj workspace forget` — no follow-up step |
| Close `holomush-rmo2` as fixed-by-removal | Reference this PR's commit |

**Definition of done:** `go build ./...`, `task lint`, `task test`, `task pr-prep` all pass with no `go.work` present and no `GOWORK=off` workaround. CI green on the PR.

### Phase 2 — Make isolation cheap

Add two `task` targets. Both MUST resolve `MAIN_REPO` via the `.jj/repo`-pointer technique already used by the soon-to-be-deleted `gowork` task (Taskfile.yaml:521-530) so they work correctly when invoked from inside any worktree, not just from the main checkout:

Use the **literal verbatim** technique from the soon-to-be-deleted `gowork` task (`Taskfile.yaml:525-530`). The pointer path inside `.jj/repo` is relative to `.jj/`, not to the workspace root, so resolution requires `cd`-ing into `.jj/` first:

```bash
# In a jj workspace, .jj/repo is a FILE containing a path relative to
# .jj/ that points to the shared repo. In the main checkout itself,
# .jj/repo is a DIRECTORY. Resolve MAIN_REPO accordingly.
if [ -f ".jj/repo" ]; then
  POINTER=$(cat ".jj/repo")
  MAIN_REPO=$(cd ".jj/${POINTER}/../.." && pwd -P)
else
  MAIN_REPO=$(pwd -P)
fi
WORKTREES="$(dirname "$MAIN_REPO")/.worktrees"
```

**Implementation note:** since the Phase 3 SessionStart hook also needs to know whether it's in the default workspace (separate but related concept), the implementation plan SHOULD extract this MAIN_REPO discovery into a shared shell helper (e.g., `scripts/jj-main-repo.sh` or a sourced shell snippet) callable from both the Phase 2 task and the Phase 3 hook. Two divergent code paths for the same concept is a drift risk.

| Target | Behavior |
|---|---|
| `task workspace:new -- <name>` | Resolve `MAIN_REPO`/`WORKTREES` as above. If `$WORKTREES/<name>` already exists, print its absolute path and exit 0 (idempotence DoD requirement — pre-check via `[ -d "$WORKTREES/<name>" ]`). Otherwise: `jj git fetch && jj workspace add "$WORKTREES/<name>" -r main@origin`, then print the absolute path of the new workspace on the last line of output (so callers — agents and humans alike — can capture it via `tail -n 1`). Both `jj git fetch` and `jj workspace add` resolve shared repo storage via `.jj/repo` and work from any cwd; no `cd` is needed. |

`task` cannot mutate the calling shell's `pwd` (subshell isolation), so a "spawn Claude in the new workspace" task target would be a half-measure for humans (their shell would land back at the original `pwd` after Claude exits). Instead, document a one-line shell snippet for human callers in `CLAUDE.md` (Phase 4):

```fish
# fish: add to ~/.config/fish/config.fish (or define inline)
function claude-iso
    set name $argv[1]
    task workspace:new -- $name; or return $status
    cd (task workspace:new -- $name | tail -n 1)
    exec claude
end
```

```bash
# bash/zsh: add to ~/.bashrc or ~/.zshrc
claude-iso() {
  local name="$1"
  task workspace:new -- "$name" || return $?
  cd "$(task workspace:new -- "$name" | tail -n 1)"
  exec claude
}
```

(Both snippets call `task workspace:new` twice for clarity — the second call is idempotent and just prints the path. A more efficient version stashes the path on the first call. Implementation plan can pick.)

Agents dispatching new Claude sessions invoke `task workspace:new` directly and then run `claude` in the printed path; agents do not need a `cd`-mutating shell function because they always operate in their own subshell context.

**Definition of done:**

- `task workspace:new` documented in CLAUDE.md "Commands" section, with the MAIN_REPO-resolution caveat noted (works from any worktree)
- `task workspace:new -- foo` invoked from the repo root creates `<repo-parent>/.worktrees/foo/` and prints that absolute path on the last line of stdout — verifiable by `[ "$(task workspace:new -- foo | tail -n 1)" = "<repo-parent>/.worktrees/foo" ]`
- Same invocation from inside any existing worktree (e.g. `cd ../.worktrees/095g && task workspace:new -- foo`) creates the workspace at `<repo-parent>/.worktrees/foo/`, NOT at `<repo-parent>/.worktrees/095g/.worktrees/foo/`
- Re-running `task workspace:new -- foo` when `foo` already exists prints the existing workspace path and exits 0 (idempotent; no re-create, no error)
- The `claude-iso` shell-function snippet (fish + bash variants) is included verbatim in the new CLAUDE.md "Session isolation" section. It is documentation, not a shipped artifact — humans copy it into their own rc file
- `task claude:isolated` is explicitly NOT shipped (rejected during design as a half-measure: `task` cannot mutate the calling shell's `pwd`)

### Phase 3 — SessionStart soft hook

A new `SessionStart` hook script `.claude/hooks/warn-default-workspace.sh` that:

1. Reads the JSON event on stdin (per Claude Code hook contract; the script does not need any field from the event but should consume stdin to be polite)
2. Determines whether the current jj workspace is the `default` workspace by checking `.jj/repo`: in the main checkout (the `default` workspace), `.jj/repo` is a directory; in every other workspace, it is a file (a relative pointer back to the main repo's `.jj`). Same technique used by the soon-to-be-deleted `gowork` task and by the Phase 2 `task workspace:new` MAIN_REPO discovery:

   ```bash
   ws_root="$(jj workspace root)"
   if [ -d "$ws_root/.jj/repo" ]; then
     ws=default
   else
     ws="$(basename "$ws_root")"
   fi
   ```

3. If `$ws` is `default`, emit a warning to **plain stdout** (the simpler of the two SessionStart contracts; matches the existing `bd prime` SessionStart hook which also uses plain stdout). The Claude Code SessionStart hook concatenates plain stdout into the session's additional context. The warning text:

   > **You are in the shared `default` jj workspace.** If you intend to edit files, another Claude Code session in the same workspace can collide with your edits at any `jj` command boundary (jj snapshots the working copy on every command). To isolate this session, exit and:
   >
   > - **Humans:** run `claude-iso <name>` (the shell function in `~/.config/fish/config.fish` or `~/.bashrc` — see CLAUDE.md "Session isolation" for the snippet)
   > - **Agents (or humans without the function):** run `task workspace:new -- <name>`, then `cd <printed-path> && claude`
   >
   > To ignore this warning, continue as normal.

4. If `$ws` is not `default`, exit 0 with no output (silent)

Wired in `.claude/settings.json` under `hooks.SessionStart` with empty matcher, alongside the existing `bd prime`.

**Definition of done:**

- Starting Claude with cwd = repo root (the `default` workspace) emits the warning text at session start (verifiable by checking the SessionStart additional context shown to the model, OR by running the hook script directly with `echo '{}' | .claude/hooks/warn-default-workspace.sh` from the repo root and observing non-empty stdout)
- Starting Claude with cwd = any other worktree (e.g. `cd ../.worktrees/foo && claude`) produces no warning output — verifiable by `echo '{}' | .claude/hooks/warn-default-workspace.sh` from that dir producing zero bytes on stdout and exit 0
- Hook smoke-tested with both cases via direct `echo '{}' | ./warn-default-workspace.sh` invocation
- Hook is shellcheck-clean (no warnings)

### Phase 4 — CLAUDE.md update

Add a new `###` sub-section under `## Commands`, named **Session isolation**, between the existing `### jj Workspace Commands` and `### Beads Commands` sub-sections (CLAUDE.md lines 552 / 570):

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

Update the "Landing the Plane" workflow (CLAUDE.md lines 815-859) to expand step 5 — currently `5. Clean up — Clear stashes, prune remote branches, jj workspace forget unused workspaces` — to spell out the workspace-forget-and-rm sequence in a fenced block. Do NOT introduce a "5.5" step or renumber 6/7; just expand step 5 with the following markdown (note: the outer 4-backtick fence keeps the nested 3-backtick `bash` block intact):

````markdown
5. **Clean up** — clear stashes, prune remote branches, and (if this work was done in a dedicated workspace per the "Session isolation" discipline) forget and remove the workspace:

   ```bash
   cd <repo-root>                           # exit the workspace before forgetting it
   jj workspace forget <name>
   rm -rf <repo-parent>/.worktrees/<name>
   ```
````

**Definition of done:**

- New "Session isolation" section present in CLAUDE.md
- "Landing the Plane" updated with cleanup step
- "jj Workspace Commands" section no longer references `task gowork`

## Acceptance criteria (full design)

1. After all four phases merge, `go.work` does not exist in the repo and is gitignored
2. After all four phases merge, `task gowork` does not exist
3. After all four phases merge, `task workspace:new -- <name>` creates a fresh workspace at `<repo-parent>/.worktrees/<name>` (printing the path), invokable from either the repo root or any existing worktree, idempotent on re-invocation
4. After all four phases merge, opening Claude in `default` produces a one-shot warning at SessionStart; opening Claude in any other workspace is silent
5. After all four phases merge, CLAUDE.md documents the per-session-isolation discipline including post-merge cleanup
6. After all four phases merge, `holomush-rmo2` is closed
7. Two Claude sessions started in distinct workspaces (created via `task workspace:new -- a` and `task workspace:new -- b`, then each session `cd`s in and `exec`s `claude`) editing independent files do not collide on jj snapshots — verifiable by inspecting `jj log -r 'mutable() & ~empty()' --no-graph -T 'change_id.short() ++ " " ++ working_copies ++ " " ++ description.first_line() ++ "\n"'` and confirming the two workspace tips show in distinct change IDs (one per workspace, no cross-pollution). `mutable()` scopes the revset to non-pushed work, avoiding noise from history.

## Decisions (resolved during brainstorming)

| Question | Decision | Rationale |
|---|---|---|
| Drop `go.work` or keep multi-worktree mode? | Drop entirely | Sole purpose was multi-worktree gopls coverage for editor users; maintainer doesn't use an editor here |
| Drop LSP integration? | Out of scope for this PR; no longer ship LSP-supporting infrastructure (just `go.work` removal) | LSP MCP tool is deferred-load anyway; agents use Grep/Read by default |
| `task gowork`: no-op or delete? | Delete entirely | No callers remain after `go.work` is removed |
| Hard or soft enforcement of session isolation? | Soft (SessionStart hook warning) | Maintainer preference; matches PR #266 trigger-hook approach |
| Auto-isolate sub-agents (`Task` tool)? | No — explicit non-goal | Lower blast radius; parent can supervise; future MAY revisit |
| Workspace naming convention? | Caller-supplied via `task workspace:new -- <name>` | Naming discipline lives in CLAUDE.md / human judgment, not in the tool |
| Ship a `task claude:isolated` target? | No — only `task workspace:new` | `task` cannot mutate the calling shell's `pwd`, so a "spawn Claude in workspace" task target is a half-measure for humans. Ship a `claude-iso` shell-function snippet in CLAUDE.md docs instead; agents use `task workspace:new` directly |
| Cleanup timing? | Manual, documented in "Landing the Plane" | Auto-cleanup risks losing in-flight follow-up work |

## Open questions for review

None — all design questions resolved during brainstorming. Ready for spec review.

## Risks

| Risk | Mitigation |
|---|---|
| Removing `go.work` breaks a contributor's local IDE setup | Document in PR body; offer per-worktree `use .` as a one-line workaround for anyone affected |
| The SessionStart warning becomes noise users tune out | Phrase it as a one-shot warning; do not repeat per prompt; only fire in `default` |
| Post-merge cleanup is forgotten, `.worktrees/` accumulates | "Landing the Plane" change is the mitigation; if it grows unwieldy, a future task can add a periodic cleanup audit |
| `claude-iso` shell-function snippet doesn't work in fish (maintainer's shell) due to syntax differences | Both fish and bash variants are included verbatim in the CLAUDE.md "Session isolation" section. Smoke-test both on the maintainer's machine before merge |

## Implementation order

Phases are independent but ordered for least surprise:

1. Phase 1 (`go.work` teardown) first — unblocks all subsequent workspace creation without env contortions
2. Phase 2 (`task` targets) second — makes the new policy actionable
3. Phase 3 (SessionStart hook) third — wires up the soft enforcement
4. Phase 4 (CLAUDE.md) last — documents the now-implemented model

Each phase is a separate commit on the same PR branch (or separate PRs if Phase 1 needs to land first to unblock CI for the rest).
