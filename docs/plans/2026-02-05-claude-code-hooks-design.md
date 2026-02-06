# Claude Code Hooks Design

**Date:** 2026-02-05
**Status:** Draft
**Scope:** Developer tooling — Claude Code session-time guardrails

## Problem

Several recurring issues arise during AI-assisted development sessions that
aren't caught until commit time (or not at all):

1. Forgetting to run `task fmt` after edits, causing noisy formatting diffs
2. Accidentally editing files on `main` instead of a feature branch
3. Using raw `go test`/`go build` instead of `task test`/`task build`, or
   shell commands (`grep`, `cat`, `find`) instead of dedicated tools
4. Forgetting to `bd sync` at end of session, losing beads state

Lefthook pre-commit hooks catch some of these at commit time, but catching
them during the session provides faster feedback and prevents wasted effort.

## Solution

Four Claude Code hooks configured in `.claude/settings.json`, implemented as
shell scripts in `.claude/hooks/`. All scripts use `jq` to parse stdin JSON.
Problems 1 and 2 each have a dedicated hook; problem 3 is handled by a
single hook covering both task runner and tool enforcement.

protect-main fails open outside repos (allows non-repo files) and fails
closed within repos (unknown branch = block). enforce-task-runner fails open
on parse errors but deterministically blocks known bad patterns. Convenience
hooks (auto-format, session-reminder) fail open — errors do not block the
user. All fail-open hooks use `trap 'exit 0' ERR` to ensure unexpected
errors produce exit 0 rather than a non-zero-non-2 exit code.

### Hook 1: Auto-format after Edit/Write

- **Event:** `PostToolUse`
- **Matcher:** `Edit|Write`
- **Script:** `.claude/hooks/auto-format.sh`
- **Behavior:** After any successful Edit or Write to a formattable file,
  runs `dprint fmt <file>` for markdown/json/toml and `goimports` for Go
  files. Reports what it formatted in verbose mode output (plain text
  stdout). Only reports if a formatter actually ran — does not claim
  formatting occurred when tools are missing.
- **Cannot block:** PostToolUse hooks run after the tool succeeds.

### Hook 2: Prevent edits on main

- **Event:** `PreToolUse`
- **Matcher:** `Edit|Write`
- **Script:** `.claude/hooks/protect-main.sh`
- **Behavior:** Before any Edit or Write, checks `git branch --show-current`
  for the file's repository. If on `main`, blocks with exit code 2 and
  message: _"Cannot edit files on main. Create a feature branch first."_
  Skips files outside a git repo (e.g., temp files in `/tmp/`, user-global
  files in `~/.claude/`). Within a repo, fails closed — if the current branch
  cannot be determined or HEAD is detached, blocks as a precaution. Walks up
  the directory tree for new deep paths where parent dirs don't exist yet.

### Hook 3: Enforce task runner and dedicated tools

- **Event:** `PreToolUse`
- **Matcher:** `Bash`
- **Script:** `.claude/hooks/enforce-task-runner.sh`
- **Behavior:** Before any Bash command, checks for blocked patterns:

| Pattern                                     | Suggestion        |
| ------------------------------------------- | ----------------- |
| `go test`                                   | Use `task test`   |
| `go build`                                  | Use `task build`  |
| `golangci-lint`                             | Use `task lint`   |
| `gofmt` / `goimports`                       | Use `task fmt`    |
| `grep` / `rg` (first in pipeline)           | Use the Grep tool |
| `cat` / `head` / `tail` (first in pipeline) | Use the Read tool |
| `find` (first in pipeline)                  | Use the Glob tool |

`sed`/`awk`/`echo` are intentionally not blocked — they are frequently
needed for shell operations where native Claude Code tools are insufficient.

Allows blocked patterns when they appear after pipes (e.g.,
`git log \| grep`). Strips shell wrapper prefixes (`env`, `sudo`,
`command`, `exec`, `nice`, `nohup`), their flags, and inline env var
assignments (including quoted values) before matching. Known limitations:
commands inside `$(...)` subshells are not inspected; quoted strings
containing `&&`/`;`/`||` are incorrectly split.

### Hook 4: Beads sync reminder

- **Event:** `Stop`
- **Matcher:** _(none — fires on every Stop)_
- **Script:** `.claude/hooks/session-reminder.sh`
- **Behavior:** Checks `bd sync --status`, `git status --porcelain`, and
  `git log '@{upstream}..HEAD'` for unpushed commits. If any show
  unsynced/uncommitted/unpushed changes, outputs a reminder in verbose mode
  output (plain text stdout). Silent when everything is clean.
- **Caveat:** Does not fire on user interrupts (Ctrl+C) — only on natural
  response completion.

## File Layout

```text
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
