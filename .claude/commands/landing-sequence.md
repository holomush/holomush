---
description: Orchestrate the session-completion checklist (Landing the Plane) — pr-prep + bead status + push + workspace cleanup
---

You are landing the plane on this session's work. Walk through the
session-completion checklist from `.claude/rules/landing-the-plane.md`,
verifying each step before moving to the next. Stop on any failure and
surface it; do not paper over.

**Scope:** $ARGUMENTS (optional — if you got a branch/bookmark name, use it
for push; otherwise infer from the current jj workspace).

## Sequence

1. **Status sweep**
   - `jj st` — confirm what's in the working copy
   - `jj log -r 'main..@' --no-pager --no-graph -T 'change_id.shortest() ++ " | " ++ description.first_line() ++ "\n"'` — show the chain that will be pushed

2. **Bead hygiene**
   - `bd list --status in_progress --assignee $(git config user.email)` — anything still claimed but unfinished?
   - For each: either `bd close <id>` (if done) or `bd note <id> "<state at end of session>"` (if continuing)

3. **pr-prep gate**
   - MUST run `task pr-prep` to full completion before push, regardless of what changed. No subset, no approximation, no exceptions. Reason: `task pr-prep` bundles lint + format + schema + license + unit + `task test:int` + `task test:e2e` to mirror CI. Subset checks miss integration-only failures.
   - If pr-prep fails: STOP, surface the failure, do not push.
   - For `.claude/`-touching changes, additionally verify `task lint:docs-symmetry` passes (the docs-symmetry lint runs as part of `task lint`, which `task pr-prep` invokes — but call it out separately if a CLAUDE.md/AGENTS.md edit was the primary motivation).

4. **Targeted rebase**
   - `jj git fetch`
   - `jj rebase -r <change-id> -d main@origin` — scope to YOUR change only. NEVER bare `jj rebase -d main`. Reason: bare rebase sweeps up descendants of other agents' in-flight work in other workspaces.

5. **Push**
   - `jj bookmark set <branch> -r @-` (or whichever rev is the tip)
   - `jj git push --branch <branch>`
   - `jj st` to verify

6. **Workspace cleanup** (only if this is a feature workspace, not the default)
   - `cd <repo-root>` — the cd matters
   - `jj workspace forget <name>`
   - `rm -rf <repo-parent>/.worktrees/<name>`

7. **Handoff** (optional)
   - If there's pickup work for next session, invoke `superpowers:handoff-prompt`

## Critical rules

- Work is NOT complete until `jj git push` succeeds.
- Never claim "ready to push" — push.
- If anything blocks (bd issue, pr-prep red, push rejected): fix it, don't ignore it.
- For an undeployed codebase: skip prod-shape discipline (no migration backfills, no reserved proto fields, no deprecation windows, no fallback paths) — when no consumers exist, those tools protect nothing and add complexity.
