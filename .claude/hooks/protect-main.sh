#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# PreToolUse hook: prevent edits on main branch
# Blocks Edit/Write tool calls when the target file is in a repo on main.
# Error strategy: security hook — fails closed (unknown state = block).
# Known limitation: detached HEAD at main's tip is not detected.
set -euo pipefail

INPUT=$(cat)
if ! FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // empty' 2>/dev/null); then
  echo "protect-main: failed to parse hook input" >&2
  exit 2
fi

[[ -z "$FILE_PATH" ]] && exit 0

# Get the directory — if the file doesn't exist yet (Write), use parent dir
if [[ -d "$(dirname "$FILE_PATH")" ]]; then
  DIR="$(dirname "$FILE_PATH")"
else
  exit 0
fi

# Files outside any git repo (tmp files, ~/.claude/ configs) are allowed.
REPO_ROOT=$(git -C "$DIR" rev-parse --show-toplevel 2>/dev/null) || exit 0

# Within a repo, fail closed if branch cannot be determined.
BRANCH=$(git -C "$REPO_ROOT" branch --show-current 2>/dev/null) || {
  echo "protect-main: cannot determine current branch in $REPO_ROOT" >&2
  echo "Blocking edit as a precaution." >&2
  exit 2
}

if [[ "$BRANCH" == "main" ]]; then
  echo "Cannot edit files on the main branch." >&2
  echo "Create a feature branch or worktree first:" >&2
  echo "  git worktree add ../.worktrees/<name> -b feat/<name>" >&2
  exit 2
fi

exit 0
