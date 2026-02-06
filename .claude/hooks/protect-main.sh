#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# PreToolUse hook: prevent edits on main branch
# Blocks Edit/Write tool calls when the target file is in a repo on main.
set -euo pipefail

INPUT=$(cat)
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // empty')

# No file path means nothing to guard
[[ -z "$FILE_PATH" ]] && exit 0

# Get the directory — if the file doesn't exist yet (Write), use parent dir
if [[ -d "$(dirname "$FILE_PATH")" ]]; then
  DIR="$(dirname "$FILE_PATH")"
else
  exit 0
fi

# Determine if we're in a git repo
REPO_ROOT=$(git -C "$DIR" rev-parse --show-toplevel 2>/dev/null) || exit 0

# Get current branch
BRANCH=$(git -C "$REPO_ROOT" branch --show-current 2>/dev/null) || exit 0

if [[ "$BRANCH" == "main" ]]; then
  echo "Cannot edit files on the main branch." >&2
  echo "Create a feature branch or worktree first:" >&2
  echo "  git worktree add ../.worktrees/<name> -b feat/<name>" >&2
  exit 2
fi

exit 0
