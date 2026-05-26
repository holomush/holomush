# Landing the Plane (Session Completion)

When ending a work session, work is **NOT complete until changes are pushed**. This is a jj-colocated repo — invoke the `jj:jujutsu` skill for command details.

## Mandatory checklist

1. **File beads for remaining work** — anything not finished gets a `bd create` so it survives the session
2. **Run `task pr-prep`** (the **fast lane**: schema/license/lint/fmt/unit/build/bats; no Docker, no flock) if code changed — MUST be green before push. Run the chosen lane as a single command to full completion; never approximate by running individual steps. Reason: April 2026 incident where a partial check claimed pr-prep passed and pushed broken integration tests. The integration and E2E gate runs in CI as required checks (`Integration Test` + `E2E Test`); run `task pr-prep:full` locally when your diff touches int/E2E surface.
3. **Update issue status** — `bd close` what's done; `bd update` what's still in flight
4. **Push:**
   - `jj git fetch`
   - **Pre-push rebase**: follow the `jj:jujutsu` skill's "Pre-Push Rebase" section. The chain-safe `jj rebase -s "$(jj log -r 'roots(trunk()..@)' --no-graph -T 'change_id.short(12)')" -o main@origin --skip-emptied` shape works for single-commit PRs *and* chains. The `guard-jj-rebase-chain` PreToolUse hook blocks the truncation-prone `jj rebase -r @ -o <trunk>` shape (PR #4049 lost 8 of 9 commits to it). Bypass via `# jj-exempt` only when extracting `@` alone is intentional.
   - Set bookmark: `jj bookmark set <branch> -r @-` (or whichever rev is the tip)
   - `jj git push --branch <branch>`
   - Verify with `jj st`
5. **Clean up workspace:**
   ```bash
   cd <repo-root>
   jj workspace forget <name>
   rm -rf <repo-parent>/.worktrees/<name>
   ```
   The `cd <repo-root>` matters — `../.worktrees/<name>` is unsafe from any nested cwd.
6. **Hand off context** — use the `superpowers:handoff-prompt` skill if there's pickup work for the next session

## Critical rules

- Work is NOT complete until `jj git push` succeeds
- NEVER stop before pushing — that leaves work stranded locally
- NEVER say "ready to push when you are" — YOU must push
- If push fails, resolve and retry until it succeeds
- `jj op restore` / `jj op abandon` rewind the global op log and silently corrupt sibling workspaces — both are gated by the `jj:jujutsu` plugin's `guard-jj-mutating` PreToolUse hook (bypass: `# jj-op-approved`). Recovery ladder (`jj undo`, `jj op revert`, etc.) is documented in the skill.

## Skipping the chain

For small fixes (typo, dependency bump, single-file bug) the bead → implementation → review → PR direct path is the right shape. The full chain (brainstorming → spec → plan → bead chain → subagent-driven-development) is for multi-task work.
