<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Landing the Plane (Session Completion)

When ending a work session, work is **NOT complete until changes are pushed**. VCS is **native git** — commit on a feature branch in your worktree and push.

## Mandatory checklist

1. **File GitHub issues for remaining work** — anything not finished gets a `gh issue create -R holomush/holomush` so it survives the session
2. **Run `task pr-prep`** (the **fast lane**: schema/license/lint/fmt/unit/build/bats; no Docker, no flock) if code changed — MUST be green before push. Run the chosen lane as a single command to full completion; never approximate by running individual steps. Reason: April 2026 incident where a partial check claimed pr-prep passed and pushed broken integration tests. The integration and E2E gate runs in CI as required checks (`Integration Test` + `E2E Test`); run `task pr-prep:full` locally when your diff touches int/E2E surface.
3. **Update issue status** — `gh issue close` what's done; `gh issue comment` what's still in flight
4. **Push:**
   - `git add -A && git commit` your work (conventional-commit message; end with the AI authorship byline)
   - **Pre-push rebase**: `git fetch origin && git rebase origin/main`, resolve any conflicts, then re-run `task pr-prep`
   - `git push -u origin <branch>`
   - Verify with `git status` (working tree clean) — the branch-vs-`main` gap was already established by the rebase step above
5. **Clean up worktree (post-merge):**
   ```bash
   cd <repo-root>
   git worktree remove <repo-parent>/.worktrees/<name>   # add --force if it holds throwaway artifacts
   git branch -d <branch>                                  # delete the merged branch
   ```
   The `cd <repo-root>` matters — `../.worktrees/<name>` is unsafe from any nested cwd.
6. **Hand off context** — use the `superpowers:handoff-prompt` skill if there's pickup work for the next session

## Critical rules

- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing — that leaves work stranded locally
- NEVER say "ready to push when you are" — YOU must push
- If push fails, resolve and retry until it succeeds
- NEVER force-push (`git push --force`) a shared branch; use `--force-with-lease` only on your own feature branch after a rebase. `git reflog` recovers lost commits after a bad reset/rebase — check it before assuming work is gone.

## Skipping the chain

For small fixes (typo, dependency bump, single-file bug) the issue → implementation → review → PR direct path is the right shape (`/gsd-quick` or `/gsd-fast`). The full GSD loop (roadmap → `/gsd-discuss-phase` → `/gsd-plan-phase` → `/gsd-execute-phase` → `/gsd-ship`) is for multi-task work.
