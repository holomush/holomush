<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Beads Issue Tracking

This project uses [Beads (bd)](https://github.com/steveyegge/beads) for issue tracking.

## Core Rules

- Track ALL work in bd (never use markdown TODOs or comment-based task lists)
- Use `bd ready` to find available work
- Use `bd create` to track new issues/tasks/bugs
- Use `bd dolt push` at end of session to sync the beads database with remote
- **NEVER run `bd github sync`** — bidirectional GH-Issues bridge with `--prefer-newer` default that flips local closures back to open (regression hit 2026-05-16 and 2026-05-17, see `feedback_bd_dolt_vs_github` memory). `bd dolt` is canonical; manage GitHub Issues manually if a public-facing surface is needed.
- Run `bd prime` for complete workflow context (SSOT for operational commands)
- **VCS**: This repo is **jj-colocated** (`.jj/` present) — prefer `jj` over `git`. `bd dolt push` syncs the beads DB only; pushing code is a separate step (see [Landing the Plane](../../CLAUDE.md#landing-the-plane-session-completion) in CLAUDE.md)
- **Dolt tags (recovery checkpoints)**: tag the dolt DB at confirmed-good states. Use the `dolt` CLI directly (not `bd dolt`), `cd "$HOME/.beads/shared-server/dolt/holomush" && dolt tag -m "<msg>" <tag>`. Convention: `safe-YYYY-MM-DD` for date-stamped snapshots, `last-known-good` as rolling pointer to most-recent snapshot. Snapshots are pointers, not copies — cheap to keep. **Retention: prune `safe-*` tags older than 30 days** (manual sweep; the rolling pointer keeps the latest accessible).
- **Strategic themes**: multi-epic clusters use `theme:<slug>` labels paired with a narrative section in [`docs/roadmap.md`](../../docs/roadmap.md). When adding a `theme:*` label to a bead, MUST also add/update the roadmap section. When closing all work in a theme, MUST move its roadmap section to "Completed themes" with a date. Full directive at root CLAUDE.md "Strategic Themes".

## Quick Reference

```bash
bd prime                              # Load complete workflow context (SSOT)
bd ready                              # Show issues ready to work (no blockers)
bd list --status=open                 # List all open issues
bd create "title" -t task -p 2        # Create new issue
bd update <id> --claim                # Claim work atomically
bd close <id>                         # Mark complete
bd dep add <issue> <depends-on>       # Add dependency
bd dolt push                          # Sync with remote
```

## Command gotchas

- **`--parent` requires the full canonical ID, not a short prefix.** `bd list --parent holomush-5rh` lists the children; `bd list --parent 5rh` prints `Error: error checking parent issue: not found: issue 5rh` and returns **zero** children — even though `bd show 5rh` resolves the short ID fine (`--parent` does not run the same resolver). Worse, that error goes to **stderr while the command still exits `0`**, so a script checking only the exit code sees a silent empty result — this is what produces false "epic has no children" reads in drain/epic pre-flight. Always pass the `holomush-` prefix to `--parent`.

## Workflow

1. Check for ready work: `bd ready`
2. Claim an issue atomically: `bd update <id> --claim`
3. Do the work
4. Mark complete: `bd close <id>`
5. Sync beads DB: `bd dolt push`
6. Push code: use `jj git push --branch <branch>` (jj-colocated) or `git push` (plain git). See CLAUDE.md "Landing the Plane" for the full pre-push checklist (`task pr-prep`, targeted rebase, etc.)

## Issue Types

- `bug` - Something broken
- `feature` - New functionality
- `task` - Work item (tests, docs, refactoring)
- `epic` - Large feature with subtasks
- `chore` - Maintenance (dependencies, tooling)

## Priorities

- `0` - Critical (security, data loss, broken builds)
- `1` - High (major features, important bugs)
- `2` - Medium (default, nice-to-have)
- `3` - Low (polish, optimization)
- `4` - Backlog (future ideas)

## Context Loading

Run `bd prime` to get complete workflow documentation in AI-optimized format.
`bd prime` is the single source of truth for operational commands and session workflow.

For detailed docs: see AGENTS.md, QUICKSTART.md, or run `bd --help`
