---
title: "Session Isolation"
---

HoloMUSH is developed primarily by concurrent AI agent sessions (Claude Code) and human contributors working in parallel. Because `jj` snapshots the working copy on every command, two sessions sharing the same `jj` workspace will collide on uncommitted edits.

To prevent this, every Claude Code session runs in its own `jj` workspace under `<repo-parent>/.worktrees/<name>`.

## Creating a workspace

`task workspace:new -- <name>` is the canonical entry point. It:

- Resolves the main repo's `.jj/repo` path from any cwd
- Runs `jj git fetch` first so `main@origin` is fresh
- Creates the workspace at `<parent>/.worktrees/<name>` rooted at `main@origin`
- Writes `.beads/redirect` pointing at the main repo's `.beads/` so `bd` works
- Is idempotent on re-invocation (just prints the existing path)

```bash
task workspace:new -- my-feature
# → /Volumes/Code/github.com/holomush/.worktrees/my-feature
```

## For agents

Run `task workspace:new -- <name>`, then `cd` into the printed path. Sub-agents launched via the `Task` tool inherit the parent's workspace; the parent MUST NOT dispatch parallel `Task` calls that edit the same files.

## For humans

Use a `claude-iso` shell function that wraps `task workspace:new` + `cd` + `exec claude` in one command. Drop one of the snippets below into your shell rc.

### fish (`~/.config/fish/config.fish`)

```fish
# IMPORTANT: `set var (cmd | tail -n 1); or ...` does NOT propagate the
# failure of `cmd` because the pipeline's exit status is `tail`'s, not
# `cmd`'s. We therefore call `task workspace:new` twice — first to check
# the exit status, then again inside command substitution to capture the
# path. The second call is idempotent and just prints the path for an
# existing workspace.
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

### bash / zsh (`~/.bashrc` or `~/.zshrc`)

```bash
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

Usage:

```bash
claude-iso my-feature   # creates workspace, cds in, launches claude
```

## Hooks and `.claude/`

New workspaces inherit `.claude/` (tracked in git), so `SessionStart`, `UserPromptSubmit`, and other Claude Code hooks fire identically in any workspace — no hook re-wiring is needed.

The `SessionStart` hook also warns if you start a session in the shared `default` workspace, which is reserved for read-only inspection.

## Cleanup after landing

After your branch lands on `main`, remove the workspace. The leading `cd` matters — `../.worktrees/<name>` is unsafe from any nested cwd:

```bash
cd <repo-root>
jj workspace forget <name>
rm -rf <repo-parent>/.worktrees/<name>
```

## Troubleshooting

**`bd` fails with "no beads database found"** — the workspace's `.beads/redirect` is missing or stale. Re-run `task workspace:new -- <name>` to self-heal it.

**Stale `main@origin`** — `task workspace:new` always runs `jj git fetch` first, so creating a fresh workspace is the easiest way to get a fresh starting point.

**Two sessions in `default`** — exit one, run `task workspace:new -- <name>` for the other.
