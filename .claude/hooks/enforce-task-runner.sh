#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# PreToolUse hook: enforce task runner and dedicated tools
# Blocks direct go/lint commands and shell search utilities, suggesting
# the proper task commands or native Claude Code tools.
# Error strategy: enforcement hook — fails open on jq/parse errors
# (command proceeds unchecked), but reliably blocks known bad patterns
# when parsing succeeds.
set -uo pipefail

# --- Parse phase: fail open on malformed input ---
trap 'exit 0' ERR

INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty' 2>/dev/null) || {
  echo "enforce-task-runner: failed to parse input — enforcement disabled for this command" >&2
  exit 0
}

[[ -z "$COMMAND" ]] && exit 0

# --- Enforcement phase: errors here are bugs, not parse failures ---
trap - ERR

strip_leading_ws() {
  local s="$1"
  echo "${s#"${s%%[![:space:]]*}"}"
}

# Strip leading KEY=value assignments (unquoted, double-quoted, single-quoted).
strip_env_vars() {
  local s="$1"
  while [[ "$s" =~ ^[A-Za-z_][A-Za-z_0-9]*=(\"[^\"]*\"|\'[^\']*\'|[^[:space:]]*)[[:space:]] ]]; do
    s="${s#"${BASH_REMATCH[0]}"}"
    s=$(strip_leading_ws "$s")
  done
  echo "$s"
}

# Strips leading whitespace, all KEY=value env var assignments (including
# quoted values like FOO="bar baz"), shell wrapper prefixes (env, sudo,
# command, exec, nice, nohup) and their flags, then returns the first
# real command word.
#
# Known limitations:
# - KEY=value at end of string without trailing whitespace is not stripped.
first_cmd_word() {
  local segment="$1"
  segment=$(strip_leading_ws "$segment")
  segment=$(strip_env_vars "$segment")
  local word="${segment%% *}"
  # Skip shell wrapper commands, their flags, and subsequent KEY=value pairs
  while [[ "$word" =~ ^(env|command|exec|sudo|nice|nohup)$ ]]; do
    segment="${segment#"$word"}"
    segment=$(strip_leading_ws "$segment")
    while [[ "${segment%% *}" == -* ]]; do
      segment="${segment#"${segment%% *}"}"
      segment=$(strip_leading_ws "$segment")
    done
    segment=$(strip_env_vars "$segment")
    word="${segment%% *}"
  done
  echo "$word"
}

# Split command on chain operators (&& ; ||) and check each segment's
# first word before any pipe. This catches:
#   "go test ./..."           -> blocked (go)
#   "cd foo && go test"       -> blocked (go in second segment)
#   "env go test ./..."       -> blocked (env prefix stripped)
#   "env -i go test ./..."    -> blocked (env flag stripped)
#   "FOO=a BAR=b go test"    -> blocked (multiple env vars stripped)
#   "git log | grep fix"      -> allowed (grep is after pipe)
#   "task test"               -> allowed (task)
#
# Known limitations:
# - Commands inside $(...) or backticks are not inspected.
# - Quoted strings containing && ; || are incorrectly split.
# - || fallback clauses are checked as independent commands, which may
#   produce false positives (e.g., "cmd || cat /dev/null").

# Split on && ; || using awk for portability (BSD sed does not support \n
# in replacement strings). Note: || is consumed by the awk split, so the
# pipe split below never misidentifies || as two separate pipe characters.
SEGMENTS=$(echo "$COMMAND" | awk '{gsub(/ *&& */, "\n"); gsub(/ *; */, "\n"); gsub(/ *\|\| */, "\n"); print}')

while IFS= read -r segment; do
  [[ -z "$segment" ]] && continue

  # Get the part before any pipe (first command in a pipeline)
  before_pipe="${segment%%|*}"
  word=$(first_cmd_word "$before_pipe")

  # Also get the second word for "go test" / "go build" distinction
  rest="${before_pipe#*"$word"}"
  rest=$(strip_leading_ws "$rest")
  second_word="${rest%% *}"

  case "$word" in
    go)
      case "$second_word" in
        test)
          if echo "$rest" | command grep -qE '(^|\s)-tags[= ]'; then
            echo "Use 'task test:integration' instead of 'go test -tags=...'" >&2
          else
            echo "Use 'task test' instead of 'go test'" >&2
          fi
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
    gofmt|goimports)
      echo "Use 'task fmt' instead of '$word'" >&2
      exit 2
      ;;
    grep|egrep|fgrep)
      echo "Use the Grep tool instead of $word" >&2
      exit 2
      ;;
    cat)
      # Allow heredocs (with or without space/flags): cat <<EOF, cat<<EOF, cat -s <<EOF
      # Allow /dev paths: cat /dev/null
      if ! echo "$before_pipe" | command grep -qE "(<<|/dev/)"; then
        echo "Use the Read tool instead of cat" >&2
        exit 2
      fi
      ;;
    head|tail)
      echo "Use the Read tool with offset/limit instead of $word" >&2
      exit 2
      ;;
    find)
      echo "Use the Glob tool instead of find" >&2
      exit 2
      ;;
  esac
done <<< "$SEGMENTS"

exit 0
