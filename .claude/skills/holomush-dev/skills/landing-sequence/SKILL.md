---
name: landing-sequence
description: Orchestrate the session-completion checklist (Landing the Plane) — pr-prep + bead status + push + workspace cleanup
disable-model-invocation: true
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
   - MUST run the fast `task pr-prep` (schema + license + lint + fmt + unit + build) green before push. Integration + E2E are required CI checks (`Integration Test`, `E2E Test`) — they gate the PR in CI, not locally. If you touched `test/integration/**`, `web/e2e/**`, or integration-tagged packages, run targeted `task test:int -- ./<domain>` or `task pr-prep:full` first (recommended, not mandatory).
   - If pr-prep fails: STOP, surface the failure, do not push.
   - For `.claude/`-touching changes, additionally verify `task lint:docs-symmetry` passes (the docs-symmetry lint runs as part of `task lint`, which `task pr-prep` invokes — but call it out separately if a CLAUDE.md/AGENTS.md edit was the primary motivation).

4. **Pre-push rebase** — defer to the `jj:jujutsu` skill's "Pre-Push Rebase" section. The chain-safe recipe handles single-commit PRs and chains identically:

   ```bash
   jj git fetch
   jj rebase -s "$(jj --no-pager log -r 'roots(trunk()..@)' --no-graph -T 'change_id.short(12)')" -o main@origin --skip-emptied
   ```

   The `guard-jj-rebase-chain` PreToolUse hook (shipped with the `jj` plugin) BLOCKS the truncation-prone `jj rebase -r @ -o <trunk>` shape — the failure mode that lost 8 of 9 commits on PR #4049 (`holomush-lfri`). If you genuinely need to extract `@` alone (confirmed single-commit PR), append `# jj-exempt` to escalate-to-ASK.

5. **Push**
   - `jj bookmark set <branch> -r @-` (or whichever rev is the tip)
   - Sanity-check the chain length one more time before push: `jj log -r 'main@origin..@' --no-pager --no-graph -T 'change_id ++ "\n"' | wc -l`. If it dropped unexpectedly between sessions (e.g., 9 → 1), STOP — investigate before pushing.
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
