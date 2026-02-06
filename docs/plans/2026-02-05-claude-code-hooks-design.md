# Claude Code Hooks Design

**Date:** 2026-02-05
**Status:** Draft
**Scope:** Developer tooling — Claude Code session-time guardrails

## Problem

Several recurring issues arise during AI-assisted development sessions that
aren't caught until commit time (or not at all):

1. Forgetting to run `task fmt` after edits, causing noisy formatting diffs
2. Accidentally editing files on `main` instead of a feature branch
3. Using raw `go test`/`go build` instead of `task test`/`task build`
4. Using shell commands (`grep`, `cat`, `find`) instead of dedicated tools
5. Forgetting to `bd sync` at end of session, losing beads state

Lefthook pre-commit hooks catch some of these at commit time, but catching
them during the session provides faster feedback and prevents wasted effort.

## Solution

Five Claude Code hooks configured in `.claude/settings.json`, implemented as
shell scripts in `.claude/hooks/`. All scripts use `jq` to parse stdin JSON.

### Hook 1: Auto-format after Edit/Write

- **Event:** `PostToolUse`
- **Matcher:** `Edit|Write`
- **Script:** `.claude/hooks/auto-format.sh`
- **Behavior:** After any successful Edit or Write to a formattable file
  (`.go`, `.md`, `.yaml`, `.yml`, `.json`, `.toml`), runs `dprint fmt <file>`.
  For `.go` files, also runs `goimports` if available. Reports what it
  formatted as `additionalContext`.
- **Cannot block:** PostToolUse hooks run after the tool succeeds.

### Hook 2: Prevent edits on main

- **Event:** `PreToolUse`
- **Matcher:** `Edit|Write`
- **Script:** `.claude/hooks/protect-main.sh`
- **Behavior:** Before any Edit or Write, checks `git branch --show-current`
  for the file's repository. If on `main`, blocks with exit code 2 and
  message: *"Cannot edit files on main. Create a feature branch first."*
  Skips files outside a git repo (e.g., `~/.claude/`).

### Hook 3+5: Enforce task runner and dedicated tools

- **Event:** `PreToolUse`
- **Matcher:** `Bash`
- **Script:** `.claude/hooks/enforce-task-runner.sh`
- **Behavior:** Before any Bash command, checks for blocked patterns:

| Pattern | Suggestion |
|---------|------------|
| `go test` | Use `task test` |
| `go build` | Use `task build` |
| `golangci-lint` | Use `task lint` |
| `gofmt` / `goimports` | Use `task fmt` |
| `grep ` / `rg ` (standalone) | Use the Grep tool |
| `cat ` / `head ` / `tail ` (standalone) | Use the Read tool |
| `find ` (standalone) | Use the Glob tool |

  Allows these patterns when they appear inside pipes or as subcommands
  (e.g., `git log \| grep`, `git grep`, `cat <<EOF`).

### Hook 6: Beads sync reminder

- **Event:** `Stop`
- **Matcher:** *(none — fires on every Stop)*
- **Script:** `.claude/hooks/session-reminder.sh`
- **Behavior:** Checks `bd sync --status` and `git status --porcelain`. If
  either shows unsynced/uncommitted changes, outputs a reminder as
  `additionalContext`. Silent when everything is clean.

## File Layout

```
.claude/
  hooks/
    auto-format.sh
    protect-main.sh
    enforce-task-runner.sh
    session-reminder.sh
  settings.json          # Hook configuration added here
```

## Testing

Each hook SHOULD be testable by piping sample JSON to stdin and checking
exit codes and stdout/stderr output. Manual testing in a Claude Code session
validates end-to-end behavior.
