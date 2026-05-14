#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Test harness for nudge-adr-capture.sh hook.
# Feeds synthetic PostToolUse JSON on stdin; asserts exit code + stdout.

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOOK="$SCRIPT_DIR/nudge-adr-capture.sh"

pass=0
fail=0
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

# expect_case <case-name> <stdin-json> <expected-exit> <expected-stdout-pattern-or-empty>
expect_case() {
  local name="$1" input="$2" want_exit="$3" want_stdout_pat="$4"
  local got_stdout got_exit
  got_stdout="$(printf '%s' "$input" | "$HOOK" 2>/dev/null)" && got_exit=0 || got_exit=$?
  if [ "$got_exit" -ne "$want_exit" ]; then
    echo "FAIL $name: exit $got_exit, want $want_exit" >&2
    fail=$((fail+1))
    return
  fi
  if [ -z "$want_stdout_pat" ]; then
    if [ -n "$got_stdout" ]; then
      echo "FAIL $name: stdout non-empty: $got_stdout" >&2
      fail=$((fail+1))
      return
    fi
  else
    if ! printf '%s' "$got_stdout" | grep -qE "$want_stdout_pat"; then
      echo "FAIL $name: stdout '$got_stdout' did not match /$want_stdout_pat/" >&2
      fail=$((fail+1))
      return
    fi
  fi
  pass=$((pass+1))
}

# --- Case 1: non-spec path (internal code edit) → silent ---
expect_case "non-spec-path" \
  '{"tool_name":"Edit","tool_input":{"file_path":"/repo/internal/foo.go"}}' \
  0 ""

echo "passed=$pass failed=$fail"
[ "$fail" -eq 0 ]
