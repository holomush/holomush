#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# PostToolUse hook: auto-format files after Edit/Write
# Uses dprint for markdown/json/toml and goimports for Go files.
# Error strategy: convenience hook — fails open (errors don't block edits).
set -euo pipefail

INPUT=$(cat)
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // empty')

[[ -z "$FILE_PATH" ]] && exit 0
[[ -f "$FILE_PATH" ]] || exit 0

REPO_ROOT=$(git -C "$(dirname "$FILE_PATH")" rev-parse --show-toplevel 2>/dev/null) || exit 0

EXT="${FILE_PATH##*.}"
case "$EXT" in
  go|md|json|toml|yaml|yml)
    ;;
  *)
    exit 0
    ;;
esac

FORMATTED=true

# dprint handles markdown, json, toml (no Go or YAML plugins configured)
if [[ -f "$REPO_ROOT/dprint.json" ]] || [[ -f "$REPO_ROOT/.dprint.json" ]]; then
  if command -v dprint >/dev/null 2>&1; then
    if ! dprint fmt "$FILE_PATH" 2>&1; then
      FORMATTED=false
    fi
  fi
fi

# goimports handles Go import organization and formatting
if [[ "$EXT" == "go" ]]; then
  if command -v goimports >/dev/null 2>&1; then
    if ! goimports -w "$FILE_PATH" 2>&1; then
      FORMATTED=false
    fi
  fi
fi

RELATIVE_PATH="${FILE_PATH#"$REPO_ROOT"/}"
if [[ "$FORMATTED" == "true" ]]; then
  echo "Auto-formatted: $RELATIVE_PATH"
else
  echo "Auto-format encountered errors: $RELATIVE_PATH"
fi
