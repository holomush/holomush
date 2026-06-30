---
name: spawn-workspace
description: Create a new isolated jj workspace for parallel agent work — wraps `task workspace:new`
disable-model-invocation: true
---

Create an isolated jj workspace for this work. Per CLAUDE.md "Session
isolation", any non-trivial work or sub-agent dispatch in this repo SHOULD
happen in a feature workspace, not the default — jj snapshots the working
copy on every command, and parallel sessions sharing a workspace will
collide on uncommitted edits.

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
   - `jj git fetch` first (so the new workspace is current)
   - `jj workspace add ../.worktrees/$ARGUMENTS --name $ARGUMENTS -r main@origin`
   - Writes `.beads/redirect` so `bd` works. Each jj workspace materialises an empty `.beads/`; the redirect points back to the main repo's Dolt DB.
   - Prints the absolute workspace path

2. `cd` into the printed path.

3. Verify the move:
   ```bash
   jj st
   ```
   The working copy `(@)` should show your new workspace's empty change.

4. Continue work from inside the new workspace.

## When NOT to use

- Quick read-only inspections (no file edits planned) — the warning hook for
  the default workspace is just precautionary; reads are safe.
- Tiny one-line fixes that won't conflict with parallel sessions.

## Cleanup

When the work is done and pushed, run the cleanup steps from
`.claude/rules/landing-the-plane.md` step 5 — `jj workspace forget` + `rm -rf`
(the post-push hook will remind you).
