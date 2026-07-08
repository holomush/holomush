---
name: spawn-workspace
description: Create a new isolated git worktree for parallel agent work — wraps `task workspace:new`
disable-model-invocation: true
---

Create an isolated git worktree for this work. Per CLAUDE.md "Session
isolation", any non-trivial work or sub-agent dispatch in this repo SHOULD
happen in a feature worktree, not the shared main checkout — parallel sessions
sharing one working tree clobber each other's uncommitted edits (last write
wins on the filesystem).

**Name:** $ARGUMENTS

If no name was given, ask the user (or pick a slug from the bead/branch
context). Names must match `[A-Za-z0-9._-]+` (no slashes, no `..`).

## Procedure

1. Run:
   ```bash
   task workspace:new -- $ARGUMENTS
   ```
   This:
   - Validates the name
   - `git fetch origin` first (so the new worktree is current)
   - `git worktree add -b $ARGUMENTS ../.worktrees/$ARGUMENTS origin/main`
   - Writes `.beads/redirect` so `bd` works. Each git worktree gets a fresh
     checkout with an empty `.beads/`; the redirect points back to the main
     repo's Dolt DB.
   - Prints the absolute worktree path

2. `cd` into the printed path.

3. Verify the move:
   ```bash
   git status --short && git branch --show-current
   ```
   You should be on branch `$ARGUMENTS` with a clean working tree.

4. Continue work from inside the new worktree.

## When NOT to use

- Quick read-only inspections (no file edits planned) — the warning hook for
  the main checkout is just precautionary; reads are safe.
- Tiny one-line fixes that won't conflict with parallel sessions.

## Cleanup

When the work is done and pushed, run the cleanup steps from
`.claude/rules/landing-the-plane.md` step 5 — `git worktree remove` +
`git branch -d` (the post-push hook will remind you).
