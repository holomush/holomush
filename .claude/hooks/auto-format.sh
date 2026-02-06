#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# PostToolUse hook: auto-format files after Edit/Write
# Uses dprint for markdown/json/toml and goimports for Go files.
# Error strategy: convenience hook — fails open (errors don't block edits).
set -uo pipefail
trap 'exit 0' ERR

INPUT=$(cat)
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // empty' 2>/dev/null) || {
  echo "auto-format: failed to parse hook input" >&2
  exit 0
}

[[ -z "$FILE_PATH" ]] && exit 0
[[ -f "$FILE_PATH" ]] || exit 0

REPO_ROOT=$(git -C "$(dirname "$FILE_PATH")" rev-parse --show-toplevel 2>/dev/null) || exit 0

EXT="${FILE_PATH##*.}"
FORMATTED=true
RAN_FORMATTER=false

case "$EXT" in
  md|json|toml)
    # dprint handles markdown, json, toml
    if [[ -f "$REPO_ROOT/dprint.json" ]] || [[ -f "$REPO_ROOT/.dprint.json" ]]; then
      if command -v dprint >/dev/null 2>&1; then
        RAN_FORMATTER=true
        if ! OUTPUT=$(dprint fmt "$FILE_PATH" 2>&1); then
          FORMATTED=false
          echo "dprint error: $OUTPUT" >&2
        fi
      fi
    fi
    ;;
  go)
    # goimports handles Go import organization and formatting
    if command -v goimports >/dev/null 2>&1; then
      RAN_FORMATTER=true
      if ! OUTPUT=$(goimports -w "$FILE_PATH" 2>&1); then
        FORMATTED=false
        echo "goimports error: $OUTPUT" >&2
      fi
    fi
    ;;
  *)
    exit 0
    ;;
esac

# Only report if a formatter actually ran — don't claim formatting
# happened when no tool was available.
if [[ "$RAN_FORMATTER" == "false" ]]; then
  exit 0
fi

RELATIVE_PATH="${FILE_PATH#"$REPO_ROOT"/}"
if [[ "$FORMATTED" == "true" ]]; then
  echo "Auto-formatted: $RELATIVE_PATH"
else
  echo "Auto-format encountered errors: $RELATIVE_PATH"
fi
