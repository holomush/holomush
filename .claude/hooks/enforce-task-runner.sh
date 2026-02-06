#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# PreToolUse hook: enforce task runner and dedicated tools
# Blocks direct go/lint commands and shell search utilities, suggesting
# the proper task commands or native Claude Code tools.
# Error strategy: enforcement hook — fails open on jq/parse errors
# but deterministically blocks known bad commands.
set -euo pipefail

INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty' 2>/dev/null) || true

[[ -z "$COMMAND" ]] && exit 0

# Strips leading whitespace, all KEY=value env var assignments, shell
# wrapper prefixes (env, sudo, command, exec, nice, nohup) and their
# flags, then returns the first real command word.
first_cmd_word() {
  local segment="$1"
  segment=$(echo "$segment" | sed 's/^[[:space:]]*//')
  # Strip all leading KEY=value assignments (FOO=bar BAZ=qux cmd)
  while echo "$segment" | grep -qE '^[A-Za-z_][A-Za-z_0-9]*=[^[:space:]]* '; do
    segment=$(echo "$segment" | sed 's/^[A-Za-z_][A-Za-z_0-9]*=[^[:space:]]* *//')
  done
  local word="${segment%% *}"
  # Skip shell wrapper commands, their flags, and subsequent KEY=value pairs
  while [[ "$word" == "env" || "$word" == "command" || "$word" == "exec" || "$word" == "sudo" || "$word" == "nice" || "$word" == "nohup" ]]; do
    segment="${segment#"$word"}"
    segment="${segment#"${segment%%[![:space:]]*}"}"
    # Skip flags (words starting with -)
    while [[ "${segment%% *}" == -* ]]; do
      segment="${segment#"${segment%% *}"}"
      segment="${segment#"${segment%%[![:space:]]*}"}"
    done
    # Strip all KEY=value assignments after wrapper prefix
    while echo "$segment" | grep -qE '^[A-Za-z_][A-Za-z_0-9]*=[^[:space:]]* '; do
      segment=$(echo "$segment" | sed 's/^[A-Za-z_][A-Za-z_0-9]*=[^[:space:]]* *//')
    done
    word="${segment%% *}"
  done
  echo "$word"
}

# Split command on chain operators (&& ; ||) and check each segment's
# first word before any pipe. This catches:
#   "go test ./..."           → blocked (go)
#   "cd foo && go test"       → blocked (go in second segment)
#   "env go test ./..."       → blocked (env prefix stripped)
#   "env -i go test ./..."    → blocked (env flag stripped)
#   "FOO=a BAR=b go test"    → blocked (multiple env vars stripped)
#   "git log | grep fix"      → allowed (grep is after pipe)
#   "task test"               → allowed (task)
#
# Known limitations:
# - Commands inside $(...) or backticks are not inspected.
# - Quoted strings containing && ; || are incorrectly split.

# Split on && ; || (preserving pipe chains as single segments)
SEGMENTS=$(echo "$COMMAND" | sed 's/ *&& */\n/g; s/ *; */\n/g; s/ *|| */\n/g')

while IFS= read -r segment; do
  [[ -z "$segment" ]] && continue

  # Get the part before any pipe (first command in a pipeline)
  before_pipe="${segment%%|*}"
  word=$(first_cmd_word "$before_pipe")

  # Also get the second word for "go test" / "go build" distinction
  rest="${before_pipe#*"$word"}"
  rest="${rest#"${rest%%[![:space:]]*}"}"
  second_word="${rest%% *}"

  case "$word" in
    go)
      case "$second_word" in
        test)
          echo "Use 'task test' instead of 'go test'" >&2
          exit 2
          ;;
        build)
          echo "Use 'task build' instead of 'go build'" >&2
          exit 2
          ;;
      esac
      ;;
    golangci-lint)
      echo "Use 'task lint' instead of 'golangci-lint'" >&2
      exit 2
      ;;
    gofmt)
      echo "Use 'task fmt' instead of 'gofmt'" >&2
      exit 2
      ;;
    goimports)
      echo "Use 'task fmt' instead of 'goimports'" >&2
      exit 2
      ;;
    grep|egrep|fgrep)
      echo "Use the Grep tool instead of $word" >&2
      exit 2
      ;;
    rg)
      echo "Use the Grep tool instead of rg" >&2
      exit 2
      ;;
    cat)
      # Allow heredocs: cat <<EOF, cat <<'EOF'
      # Allow /dev/null: cat /dev/null
      if ! echo "$before_pipe" | grep -qE "cat[[:space:]]+(<<|/dev/)"; then
        echo "Use the Read tool instead of cat" >&2
        exit 2
      fi
      ;;
    head)
      echo "Use the Read tool with offset/limit instead of head" >&2
      exit 2
      ;;
    tail)
      echo "Use the Read tool with offset/limit instead of tail" >&2
      exit 2
      ;;
    find)
      echo "Use the Glob tool instead of find" >&2
      exit 2
      ;;
  esac
done <<< "$SEGMENTS"

exit 0
