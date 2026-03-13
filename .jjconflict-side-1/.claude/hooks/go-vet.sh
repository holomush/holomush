#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# PostToolUse hook: run go vet on edited Go files
# Catches semantic issues (bad printf verbs, unreachable code, struct tags)
# that goimports/gofmt don't detect.
# Error strategy: convenience hook — fails open (errors don't block edits).
set -uo pipefail
trap 'exit 0' ERR

INPUT=$(cat)
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // empty' 2>/dev/null) || {
  echo "go-vet: failed to parse hook input" >&2
  exit 0
}

[[ -z "$FILE_PATH" ]] && exit 0
[[ -f "$FILE_PATH" ]] || exit 0

# Only vet Go source files (skip test helpers, generated code checked separately)
EXT="${FILE_PATH##*.}"
[[ "$EXT" != "go" ]] && exit 0

REPO_ROOT=$(git -C "$(dirname "$FILE_PATH")" rev-parse --show-toplevel 2>/dev/null) || exit 0

# Derive the package path from the file's directory
PKG_DIR=$(dirname "$FILE_PATH")
RELATIVE_PKG="./${PKG_DIR#"$REPO_ROOT"/}"

# Run go vet on the package (not the single file — vet needs full package context)
if ! OUTPUT=$(cd "$REPO_ROOT" && go vet "$RELATIVE_PKG/..." 2>&1); then
  RELATIVE_PATH="${FILE_PATH#"$REPO_ROOT"/}"
  echo "go vet found issues in $RELATIVE_PATH:"
  echo "$OUTPUT"
else
  # Silent on success — only report problems
  :
fi

exit 0
