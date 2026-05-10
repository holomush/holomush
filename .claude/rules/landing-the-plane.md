# Landing the Plane (Session Completion)

When ending a work session, work is **NOT complete until changes are pushed**. This is a jj-colocated repo — invoke the `jj:jujutsu` skill for command details.

## Mandatory checklist

1. **File beads for remaining work** — anything not finished gets a `bd create` so it survives the session
2. **Run `task pr-prep`** (mirrors CI) if code changed — MUST be green before push. Run as a single command to full completion; never approximate by running individual steps. Reason: April 2026 incident where a partial check claimed pr-prep passed and pushed broken integration tests.
3. **Update issue status** — `bd close` what's done; `bd update` what's still in flight
4. **Push:**
   - `jj git fetch`
   - Targeted rebase: `jj rebase -r <change-id> -d main@origin` — **never bare `jj rebase -d main`**. Reason: bare rebase sweeps up descendants of other agents' in-flight work.
   - Set bookmark: `jj bookmark set <branch> -r @-` (or whichever rev)
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
- NEVER use `jj op restore` to "fix" an unexpected state without explicit user direction. Reason: the op log is repo-global; rewinding from one workspace silently wipes other agents' in-flight work in every other workspace. Treat it like `git push --force` to a shared branch.

## Skipping the chain

For small fixes (typo, dependency bump, single-file bug) the bead → implementation → review → PR direct path is the right shape. The full chain (brainstorming → spec → plan → bead chain → subagent-driven-development) is for multi-task work.
