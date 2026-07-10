---
name: landing-sequence
description: Orchestrate the session-completion checklist (Landing the Plane) — pr-prep + GitHub issue status + push + worktree cleanup
disable-model-invocation: true
---

You are landing the plane on this session's work. Walk through the
session-completion checklist from `.claude/rules/landing-the-plane.md`,
verifying each step before moving to the next. Stop on any failure and
surface it; do not paper over.

**Scope:** $ARGUMENTS (optional — if you got a branch name, use it for push;
otherwise infer from the current git branch).

## Sequence

1. **Status sweep**
   - `git status --short` — confirm what's in the working copy
   - `git log --oneline origin/main..HEAD` — show the commits that will be pushed

2. **GitHub issue hygiene**
   - `gh issue list -R holomush/holomush --assignee @me --state open` — anything still claimed but unfinished?
   - For each: either `gh issue close <number> -R holomush/holomush` (if done) or `gh issue comment <number> -R holomush/holomush --body "<state at end of session>"` (if continuing)

3. **pr-prep gate**
   - MUST run the fast `task pr-prep` (schema + license + lint + fmt + unit + build) green before push. Integration + E2E are required CI checks (`Integration Test`, `E2E Test`) — they gate the PR in CI, not locally. If you touched `test/integration/**`, `web/e2e/**`, or integration-tagged packages, run targeted `task test:int -- ./<domain>` or `task pr-prep:full` first (recommended, not mandatory).
   - If pr-prep fails: STOP, surface the failure, do not push.
   - For `.claude/`-touching changes, additionally verify `task lint:docs-symmetry` passes (the docs-symmetry lint runs as part of `task lint`, which `task pr-prep` invokes — but call it out separately if a CLAUDE.md/AGENTS.md edit was the primary motivation).

4. **Pre-push rebase**

   ```bash
   git fetch origin
   git rebase origin/main
   ```

   Resolve any conflicts, then re-run `task pr-prep`. Never force-push a shared
   branch; use `--force-with-lease` only on your own feature branch after a
   rebase. `git reflog` recovers commits after a bad rebase/reset — check it
   before assuming work is gone.

5. **Push**
   - `git push -u origin <branch>`
   - `git status` to verify (working tree clean) — the ahead-of-`main` gap was established by the rebase step above; `git status` compares against the branch's own upstream, not `main`

6. **Worktree cleanup** (post-merge only — skip if the PR hasn't landed yet; the post-push hook reminds you)
   - `cd <repo-root>` — the cd matters; `../.worktrees/<name>` is unsafe from any nested cwd
   - `git worktree remove <repo-parent>/.worktrees/<name>`
   - `git branch -d <branch>`

7. **Handoff** (optional)
   - If there's pickup work for next session, invoke `superpowers:handoff-prompt`

## Critical rules

- Work is NOT complete until `git push` succeeds.
- Never claim "ready to push" — push.
- If anything blocks (an unresolved GitHub issue, pr-prep red, push rejected): fix it, don't ignore it.
- For an undeployed codebase: skip prod-shape discipline (no migration backfills, no reserved proto fields, no deprecation windows, no fallback paths) — when no consumers exist, those tools protect nothing and add complexity.
