---
title: "Session Isolation"
---

:::note[Maintainer workflow]
This page documents the maintainer/agent session-isolation workflow using
native [git worktrees](https://git-scm.com/docs/git-worktree). **External
contributors don't need worktrees** — a standard `git` fork-and-branch workflow
is all that's required. See
[CONTRIBUTING.md](https://github.com/holomush/holomush/blob/main/CONTRIBUTING.md).
:::

HoloMUSH is developed primarily by concurrent AI agent sessions (Claude Code) and human contributors working in parallel. Two sessions editing files in the same working tree clobber each other's uncommitted changes (last write wins on the filesystem), so each session gets its own isolated checkout.

To prevent collisions, every Claude Code session runs in its own git worktree under `<repo-parent>/.worktrees/<name>`.

## Creating a worktree

`task workspace:new -- <name>` is the canonical entry point. It:

- Resolves the main repo from any cwd (via `git rev-parse`)
- Runs `git fetch origin` first so `origin/main` is fresh
- Creates the worktree at `<parent>/.worktrees/<name>` on a new branch `<name>` off `origin/main` — or, if a branch `<name>` already exists, attaches the worktree to that existing branch (so a reused name resumes its work rather than resetting)
- Writes `.beads/redirect` pointing at the main repo's `.beads/` so `bd` works
- Is idempotent on re-invocation (just prints the existing path)

```bash
task workspace:new -- my-feature
# → /Volumes/Code/github.com/holomush/.worktrees/my-feature
```

## For agents

Run `task workspace:new -- <name>`, then `cd` into the printed path. Sub-agents launched via the `Task` tool inherit the parent's worktree; the parent MUST NOT dispatch parallel `Task` calls that edit the same files.

## For humans

Use a `claude-iso` shell function that wraps `task workspace:new` + `cd` + `exec claude` in one command. Drop one of the snippets below into your shell rc.

### fish (`~/.config/fish/config.fish`)

```fish
# IMPORTANT: `set var (cmd | tail -n 1); or ...` does NOT propagate the
# failure of `cmd` because the pipeline's exit status is `tail`'s, not
# `cmd`'s. We therefore call `task workspace:new` twice — first to check
# the exit status, then again inside command substitution to capture the
# path. The second call is idempotent and just prints the path for an
# existing worktree.
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
claude-iso my-feature   # creates worktree, cds in, launches claude
```

## Hooks and `.claude/`

New worktrees inherit `.claude/` (tracked in git), so `SessionStart`, `UserPromptSubmit`, and other Claude Code hooks fire identically in any worktree — no hook re-wiring is needed.

The `SessionStart` hook also warns if you start a session in the shared main checkout (primary worktree), which is reserved for read-only inspection.

## Using `gh` in a worktree

A git worktree carries its own `.git` file linking back to the main repo, so
`gh` auto-detects the remote — no `-R` flag is required. You MAY still pass
`-R holomush/holomush` explicitly in scripts for robustness (e.g.
`gh pr view 123 -R holomush/holomush`).

## Cleanup after landing

After your branch lands on `main`, remove the worktree. The leading `cd` matters — `../.worktrees/<name>` is unsafe from any nested cwd:

```bash
cd <repo-root>
git worktree remove <repo-parent>/.worktrees/<name>
git branch -d <name>
```

## Troubleshooting

**`bd` fails with "no beads database found"** — the worktree's `.beads/redirect` is missing or stale. Re-run `task workspace:new -- <name>` to self-heal it.

**Stale `origin/main`** — `task workspace:new` always runs `git fetch origin` first, so creating a fresh worktree is the easiest way to get a fresh starting point.

**Two sessions in the main checkout** — exit one, run `task workspace:new -- <name>` for the other.
