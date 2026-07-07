#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Test harness for enforce-task-runner.sh offload rules (holomush-drf7b §3.3).
# Feeds synthetic PreToolUse JSON on stdin; asserts exit code, stdout
# (deny JSON), and stderr (nudges). Pattern: nudge-adr-capture.test.sh.

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOOK="$SCRIPT_DIR/enforce-task-runner.sh"

pass=0
fail=0

# mkinput <command> [agent_id]
mkinput() {
  if [ -n "${2:-}" ]; then
    jq -cn --arg cmd "$1" --arg aid "$2" '{tool_input:{command:$cmd}, agent_id:$aid}'
  else
    jq -cn --arg cmd "$1" '{tool_input:{command:$cmd}}'
  fi
}

# expect_case <name> <mode> <stdin-json> <want_exit> <stdout-pat-or-empty> <stderr-pat-or-empty>
expect_case() {
  local name="$1" mode="$2" input="$3" want_exit="$4" want_out="$5" want_err="$6"
  local got_out got_err got_exit errf
  errf="$(mktemp)"
  got_out="$(printf '%s' "$input" | OFFLOAD_ENFORCE="$mode" "$HOOK" 2>"$errf")" && got_exit=0 || got_exit=$?
  got_err="$(cat "$errf")"; rm -f "$errf"
  if [ "$got_exit" -ne "$want_exit" ]; then
    echo "FAIL $name: exit $got_exit, want $want_exit" >&2; fail=$((fail+1)); return
  fi
  if [ -z "$want_out" ]; then
    [ -n "$got_out" ] && { echo "FAIL $name: stdout non-empty: $got_out" >&2; fail=$((fail+1)); return; }
  else
    printf '%s' "$got_out" | grep -qE "$want_out" || { echo "FAIL $name: stdout '$got_out' !~ /$want_out/" >&2; fail=$((fail+1)); return; }
  fi
  if [ -z "$want_err" ]; then
    [ -n "$got_err" ] && { echo "FAIL $name: stderr non-empty: $got_err" >&2; fail=$((fail+1)); return; }
  else
    printf '%s' "$got_err" | grep -qE "$want_err" || { echo "FAIL $name: stderr '$got_err' !~ /$want_err/" >&2; fail=$((fail+1)); return; }
  fi
  pass=$((pass+1))
}

DENY_PAT='"permissionDecision": *"deny"'

# --- deny mode: each matched task name in the MAIN session is denied ---
for name in test test:int test:cover lint build; do
  expect_case "deny-$name" deny "$(mkinput "task $name")" 0 "$DENY_PAT" ""
done
expect_case "deny-test-with-args" deny "$(mkinput 'task test -- ./internal/command/')" 0 "$DENY_PAT" ""
expect_case "deny-chained" deny "$(mkinput 'cd foo && task test')" 0 "$DENY_PAT" ""
expect_case "deny-names-local-check" deny "$(mkinput 'task lint')" 0 'local-check' ""

# --- NOT matched: excluded names run untouched ---
for name in test:verbose test:e2e lint:go lint:proto docs:build fmt; do
  expect_case "allow-$name" deny "$(mkinput "task $name")" 0 "" ""
done

# --- subagent (agent_id present): all offload rules skipped ---
expect_case "subagent-skip" deny "$(mkinput 'task test' 'agent-abc123')" 0 "" ""

# --- escape hatch ---
expect_case "exempt" deny "$(mkinput 'task test # offload-exempt')" 0 "" ""

# --- pr-prep family: never denied, soft stderr nudge only ---
for name in pr-prep pr-prep:full pr-prep:docs; do
  expect_case "prprep-$name" deny "$(mkinput "task $name")" 0 "" 'local-pr-prep'
done

# --- nudge mode: matched names nudge on stderr, no deny JSON ---
expect_case "nudge-mode" nudge "$(mkinput 'task test')" 0 "" 'local-check'

# --- pre-existing rules unaffected: raw go test still exit-2 blocked ---
expect_case "go-test-still-blocked" deny "$(mkinput 'go test ./...')" 2 "" "task test"

# --- documented-limitation pins (Known limitations block in the hook) ---
# Control-flow body exemption: task inside do…done is depth>0, not matched.
expect_case "loopbody-exempt-documented" deny "$(mkinput 'for i in 1; do task test; done')" 0 "" ""
# Exempt token is a raw-command substring check: a quoted occurrence anywhere
# exempts all task segments in the command.
expect_case "exempt-substring-documented" deny "$(mkinput 'jj describe -m "see # offload-exempt docs" && task test')" 0 "" ""

echo "pass=$pass fail=$fail"
[ "$fail" -eq 0 ]
