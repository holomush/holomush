#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# PreToolUse hook: prevent edits on main branch
# Blocks Edit/Write tool calls when the target file is in a repo on main.
# Error strategy: security hook — fails open outside repos (allows non-repo
# files), fails closed within repos (unknown branch = block).
set -uo pipefail
trap 'echo "protect-main: unexpected error" >&2; exit 2' ERR

INPUT=$(cat)
if ! FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // empty' 2>/dev/null); then
  echo "protect-main: failed to parse hook input" >&2
  exit 2
fi

[[ -z "$FILE_PATH" ]] && exit 0

# Walk up to find the nearest existing directory for the target path.
# Handles Write to new deep paths where parent dirs don't exist yet.
DIR="$(dirname "$FILE_PATH")"
while [[ ! -d "$DIR" && "$DIR" != "/" ]]; do
  DIR="$(dirname "$DIR")"
done
if [[ ! -d "$DIR" ]]; then
  echo "protect-main: cannot determine directory for $FILE_PATH" >&2
  exit 2
fi

# Files outside any git repo (tmp files, ~/.claude/ configs) are allowed.
REPO_ROOT=$(git -C "$DIR" rev-parse --show-toplevel 2>/dev/null) || exit 0

# Within a repo, fail closed if branch cannot be determined.
BRANCH=$(git -C "$REPO_ROOT" branch --show-current 2>/dev/null) || {
  echo "protect-main: cannot determine current branch in $REPO_ROOT" >&2
  echo "Blocking edit as a precaution." >&2
  exit 2
}

# Detached HEAD returns empty string — fail closed since we can't confirm
# the user is NOT on main's tip.
if [[ -z "$BRANCH" ]]; then
  echo "protect-main: detached HEAD in $REPO_ROOT — cannot verify branch" >&2
  echo "Blocking edit as a precaution. Checkout a named branch first." >&2
  exit 2
fi

if [[ "$BRANCH" == "main" ]]; then
  echo "Cannot edit files on the main branch." >&2
  echo "Create a feature branch or worktree first:" >&2
  echo "  git worktree add ../.worktrees/<name> -b feat/<name>" >&2
  exit 2
fi

exit 0
