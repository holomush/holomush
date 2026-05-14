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

# Path is in scope. Subsequent steps will check the marker and emit
# additionalContext JSON when stale or missing. Until then: stub.
exit 0
