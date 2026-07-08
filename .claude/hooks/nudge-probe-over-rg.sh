#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# PreToolUse hook (Bash, soft-warn): nudge probe MCP over rg for code-symbol
# searches. Per CLAUDE.md tool precedence, mcp__probe__search_code returns
# whole AST blocks for symbol queries — much better than rg's line snippets
# for "where is X defined" / "how does Y work" questions.
#
# Heuristic: trigger only when rg is being used as a Go-code search tool
# (--type=go, -tgo, or matching `func|type|struct|interface` patterns).
# Trying to be conservative — false positives on text grep ("rg some-string")
# create more friction than they save.
#
# Strategy: warn but do NOT block (exit 0 always). The user / agent can
# proceed; the nudge just teaches the better tool.

set -uo pipefail
trap 'exit 0' ERR

INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty' 2>/dev/null) || exit 0
[[ -z "$COMMAND" ]] && exit 0

# Strip quoted strings so rg inside `git commit -m '...'` doesn't false-trigger.
STRIPPED=$(printf '%s' "$COMMAND" | perl -0777 -pe "s/'[^']*'//g; s/\"[^\"]*\"//g" 2>/dev/null) || STRIPPED="$COMMAND"

# Inspect each command segment separately. The previous one-shot
# `awk -F'[;&|]' '{print $1}'` only kept the first segment, so
# `cd internal && rg -tgo Foo` never fired. Split on && / ; / || and
# check the first command word of each segment (stopping before any pipe,
# since pipes filter output rather than searching code).
SEGMENTS=$(printf '%s' "$STRIPPED" | awk '{gsub(/ *&& */, "\n"); gsub(/ *; */, "\n"); gsub(/ *\|\| */, "\n"); print}')

rg_found=0
while IFS= read -r segment; do
  [[ -z "$segment" ]] && continue
  before_pipe="${segment%%|*}"
  word=$(printf '%s' "$before_pipe" | sed -E 's/^[[:space:]]+//; s/^([A-Za-z_][A-Za-z_0-9]*=[^[:space:]]+[[:space:]]+)+//' | awk '{print $1}')
  if [ "$word" = "rg" ]; then
    rg_found=1
    break
  fi
done <<< "$SEGMENTS"

[[ $rg_found -eq 0 ]] && exit 0

# Trigger only on Go-code-shaped rg invocations.
if echo "$STRIPPED" | command grep -qE -- '(--type[= ]go|-t[= ]?go|-(tgo)\b)'; then
  echo "Nudge: for Go symbol/AST searches, prefer 'mcp__probe__search_code' or 'mcp__probe__extract_code' — they return the enclosing function/type as one block instead of grep snippets. (rg is fine for raw text searches.)" >&2
fi

exit 0
