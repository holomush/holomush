#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# PostToolUse hook: nudge Claude when a spec/plan is edited without a
# current ADR-capture marker. See
# docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md §2.

set -uo pipefail

# Read PostToolUse JSON from stdin; bail silently on parse failure.
input="$(cat)"
tool_name="$(printf '%s' "$input" | jq -r '.tool_name // empty' 2>/dev/null)"
file_path="$(printf '%s' "$input" | jq -r '.tool_input.file_path // empty' 2>/dev/null)"

# Bail unless an Edit/Write touches an absolute file_path.
case "$tool_name" in
  Edit|Write) ;;
  *) exit 0 ;;
esac
[ -n "$file_path" ] || exit 0

# Watched-path regex (single source of truth per spec §Watched-path pattern).
if ! [[ "$file_path" =~ ^(.*/)?docs/(superpowers/)?(specs|plans)/.+\.md$ ]]; then
  exit 0
fi

# Path is in scope. Inspect the file's last line for an adr-capture marker.
[ -r "$file_path" ] || exit 0

# Read the file's final line. If it matches the marker prefix, treat it
# as the marker and the rest of the file as "stripped content".
last_line="$(tail -n 1 "$file_path")"
case "$last_line" in
  '<!-- adr-capture: '*' -->')
    marker_line="$last_line"
    has_marker=1
    ;;
  *)
    marker_line=""
    has_marker=0
    ;;
esac

# Classify the marker kind: optout | sha | malformed.
# Disambiguator: optout=true + reason="..." → optout; sha256=<16hex> → sha; else malformed.
marker_kind="none"
optout_reason=""
marker_sha=""
# shellcheck disable=SC2034  # optout_reason reserved for future skill-side log emission
if [ -n "$marker_line" ]; then
  if printf '%s' "$marker_line" | grep -qE 'optout=true'; then
    if printf '%s' "$marker_line" | grep -qE 'reason="[^"]+"'; then
      marker_kind="optout"
      optout_reason="$(printf '%s' "$marker_line" | sed -nE 's/.*reason="([^"]+)".*/\1/p')"
    else
      marker_kind="malformed"
    fi
  elif printf '%s' "$marker_line" | grep -qE 'sha256=[0-9a-f]{16}([^0-9a-f]|$)'; then
    marker_kind="sha"
    marker_sha="$(printf '%s' "$marker_line" | sed -nE 's/.*sha256=([0-9a-f]{16}).*/\1/p')"
  else
    marker_kind="malformed"
  fi
fi

# Compute current SHA over stripped content (first 16 hex chars).
# CRITICAL: bash command substitution "$(...)" strips trailing newlines from
# the captured value. The skill stamps a SHA over content that ends with `\n`
# (per spec §Marker convention "Stamping rule" step 2). If we capture into a
# variable, the trailing `\n` is lost and the hook SHA disagrees with the
# skill SHA on every spec. Stream awk's output directly into sha256sum
# instead.
if [ "$has_marker" = "1" ]; then
  current_sha="$(awk -v n="$(wc -l <"$file_path")" 'NR<n' "$file_path" \
    | sha256sum | cut -c1-16)"
else
  current_sha="$(sha256sum <"$file_path" | cut -c1-16)"
fi

# Decide outcome.
case "$marker_kind" in
  optout)
    exit 0  # opt-out wins; never nudge
    ;;
  sha)
    if [ "$marker_sha" = "$current_sha" ]; then
      exit 0  # fresh marker; silent
    fi
    reason="content changed since capture"
    ;;
  malformed)
    reason="malformed marker"
    ;;
  none)
    reason="no marker"
    ;;
esac

# Nudge path. Emit PostToolUse additionalContext JSON; bare stdout does
# NOT reach Claude for PostToolUse — only additionalContext does.
jq -nc \
  --arg path "$file_path" \
  --arg reason "$reason" \
  '{
    hookSpecificOutput: {
      hookEventName: "PostToolUse",
      additionalContext: ("adr-capture: " + $path + " was modified (" + $reason + "). Run /capture-adrs " + $path + " to extract any ADR-worthy decisions, or --dry-run to preview.")
    }
  }'
exit 0
