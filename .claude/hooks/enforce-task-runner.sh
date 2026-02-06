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
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty')

[[ -z "$COMMAND" ]] && exit 0

# Strips leading whitespace, env var assignments (FOO=bar), and shell
# wrapper prefixes (env, sudo, command, exec, nice, nohup), then returns
# the first real command word.
first_cmd_word() {
  local segment="$1"
  # Strip leading whitespace and a single simple env var assignment
  # (FOO=bar cmd, no spaces in value)
  segment=$(echo "$segment" | sed 's/^[[:space:]]*//' | sed 's/^[A-Za-z_][A-Za-z_0-9]*=[^[:space:]]* *//')
  local word="${segment%% *}"
  # Skip shell wrapper commands and re-extract
  while [[ "$word" == "env" || "$word" == "command" || "$word" == "exec" || "$word" == "sudo" || "$word" == "nice" || "$word" == "nohup" ]]; do
    segment="${segment#"$word"}"
    segment="${segment#"${segment%%[![:space:]]*}"}"
    # Also strip any KEY=value after env
    segment=$(echo "$segment" | sed 's/^[A-Za-z_][A-Za-z_0-9]*=[^[:space:]]* *//')
    word="${segment%% *}"
  done
  echo "$word"
}

# Split command on chain operators (&& ; ||) and check each segment's
# first word before any pipe. This catches:
#   "go test ./..."           → blocked (go)
#   "cd foo && go test"       → blocked (go in second segment)
#   "env go test ./..."       → blocked (env prefix stripped)
#   "git log | grep fix"      → allowed (grep is after pipe)
#   "task test"               → allowed (task)
#
# Known limitation: commands inside $(...) or backticks are not inspected.

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
