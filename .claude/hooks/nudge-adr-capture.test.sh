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

# --- Case 2: docs/specs flat → reaches marker logic (currently still silent stub) ---
expect_case "docs-specs-flat" \
  '{"tool_name":"Edit","tool_input":{"file_path":"/repo/docs/specs/foo.md"}}' \
  0 ""

# --- Case 3: docs/plans flat ---
expect_case "docs-plans-flat" \
  '{"tool_name":"Edit","tool_input":{"file_path":"/repo/docs/plans/bar.md"}}' \
  0 ""

# --- Case 4: docs/superpowers/specs nested ---
expect_case "docs-superpowers-specs-nested" \
  '{"tool_name":"Edit","tool_input":{"file_path":"/repo/docs/superpowers/specs/2026/baz.md"}}' \
  0 ""

# --- Case 5: docs/superpowers/plans flat ---
expect_case "docs-superpowers-plans-flat" \
  '{"tool_name":"Edit","tool_input":{"file_path":"/repo/docs/superpowers/plans/qux.md"}}' \
  0 ""

# --- Case 6: docs/adr/ — out of scope ---
expect_case "docs-adr-not-watched" \
  '{"tool_name":"Edit","tool_input":{"file_path":"/repo/docs/adr/0001-foo.md"}}' \
  0 ""

# --- Case 7: workspace path (.worktrees/foo/docs/specs/...) — should match ---
expect_case "worktree-path-matches" \
  '{"tool_name":"Edit","tool_input":{"file_path":"/repo/.worktrees/foo/docs/specs/bar.md"}}' \
  0 ""

# --- Case 8: non-Edit/Write tool → bail ---
expect_case "non-edit-tool-bail" \
  '{"tool_name":"Read","tool_input":{"file_path":"/repo/docs/specs/foo.md"}}' \
  0 ""

# Helper: write content + marker to a file under tmpdir; return path.
make_spec() {
  local name="$1" content="$2"
  local p="$tmpdir/docs/specs/$name.md"
  mkdir -p "$(dirname "$p")"
  printf '%s' "$content" > "$p"
  printf '%s' "$p"
}

# Compute SHA the way the hook will (after stripping any trailing marker line).
spec_sha() {
  local file="$1"
  # Drop trailing marker line if last line matches; sha256sum the rest.
  awk 'BEGIN{prev=""} {if(NR>1)print prev; prev=$0} END{
    if(prev ~ /^<!-- adr-capture: .*-->$/) {}
    else { if(NR>0) print prev }
  }' "$file" | sha256sum | cut -c1-16
}

# --- Case 9: spec with no marker → nudge (currently still stub; xfail until hook impl) ---
no_marker_path="$(make_spec "no-marker" 'spec body without marker\n')"
expect_case "no-marker-nudges" \
  "$(printf '{"tool_name":"Edit","tool_input":{"file_path":"%s"}}' "$no_marker_path")" \
  0 'hookSpecificOutput.*PostToolUse.*no marker'

# --- Case 10: spec with fresh (matching) marker → silent ---
fresh_body="fresh content\n"
fresh_path="$(make_spec "fresh-marker" "$fresh_body")"
fresh_sha="$(spec_sha "$fresh_path")"
printf '\n<!-- adr-capture: sha256=%s; session=test; ts=2026-05-14T00:00:00Z; adrs= -->\n' "$fresh_sha" >> "$fresh_path"
expect_case "fresh-marker-silent" \
  "$(printf '{"tool_name":"Edit","tool_input":{"file_path":"%s"}}' "$fresh_path")" \
  0 ""

# --- Case 11: spec with stale (mismatched) marker → nudge ---
stale_path="$(make_spec "stale-marker" "original content")"
printf '\n<!-- adr-capture: sha256=deadbeefdeadbeef; session=test; ts=2026-05-14T00:00:00Z; adrs= -->\n' >> "$stale_path"
expect_case "stale-marker-nudges" \
  "$(printf '{"tool_name":"Edit","tool_input":{"file_path":"%s"}}' "$stale_path")" \
  0 'hookSpecificOutput.*PostToolUse.*content changed'

# --- Case 12: opt-out marker → silent (no nudge) ---
optout_path="$(make_spec "optout" "doc body")"
printf '\n<!-- adr-capture: optout=true; reason="external doc" -->\n' >> "$optout_path"
expect_case "optout-silent" \
  "$(printf '{"tool_name":"Edit","tool_input":{"file_path":"%s"}}' "$optout_path")" \
  0 ""

# --- Case 13: malformed marker (prefix but no sha256= and no optout=) → nudge ---
mal_path="$(make_spec "malformed" "doc body")"
printf '\n<!-- adr-capture: foo=bar -->\n' >> "$mal_path"
expect_case "malformed-nudges" \
  "$(printf '{"tool_name":"Edit","tool_input":{"file_path":"%s"}}' "$mal_path")" \
  0 'hookSpecificOutput.*PostToolUse'

echo "passed=$pass failed=$fail"
[ "$fail" -eq 0 ]
