#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# PostToolUse hook: auto-format files after Edit/Write
# Runs dprint fmt on formattable files to keep formatting consistent.
set -euo pipefail

INPUT=$(cat)
TOOL_NAME=$(echo "$INPUT" | jq -r '.tool_name // empty')
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // empty')

# Nothing to do without a file path
[[ -z "$FILE_PATH" ]] && exit 0

# Only format files that exist (Write might target a new path that failed)
[[ -f "$FILE_PATH" ]] || exit 0

# Determine the repo root for this file
REPO_ROOT=$(git -C "$(dirname "$FILE_PATH")" rev-parse --show-toplevel 2>/dev/null) || exit 0

# Check file extension
EXT="${FILE_PATH##*.}"
case "$EXT" in
  go|md|json|toml|yaml|yml)
    ;;
  *)
    exit 0
    ;;
esac

# Run dprint if config exists in the repo
if [[ -f "$REPO_ROOT/dprint.json" ]] || [[ -f "$REPO_ROOT/.dprint.json" ]]; then
  if command -v dprint >/dev/null 2>&1; then
    dprint fmt "$FILE_PATH" 2>/dev/null || true
  fi
fi

# For Go files, also run goimports for import organization
if [[ "$EXT" == "go" ]]; then
  if command -v goimports >/dev/null 2>&1; then
    goimports -w "$FILE_PATH" 2>/dev/null || true
  fi
fi

# Report what we did
RELATIVE_PATH="${FILE_PATH#"$REPO_ROOT"/}"
echo "Auto-formatted: $RELATIVE_PATH"
