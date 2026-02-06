#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Stop hook: remind about unsynced beads and uncommitted changes
# Fires when the agent finishes responding (Stop event). Only outputs
# when there's actually unsynced work to avoid noise.
# Error strategy: convenience hook — fails open (errors don't block).
set -euo pipefail

INPUT=$(cat)
CWD=$(echo "$INPUT" | jq -r '.cwd // empty' 2>/dev/null) || true

WORKDIR="${CWD:-.}"

REPO_ROOT=$(git -C "$WORKDIR" rev-parse --show-toplevel 2>/dev/null) || exit 0

REMINDERS=()

# Check for uncommitted changes
GIT_STATUS=$(git -C "$REPO_ROOT" status --porcelain 2>/dev/null) || {
  REMINDERS+=("git status failed — check repo health")
  GIT_STATUS=""
}
if [[ -n "$GIT_STATUS" ]]; then
  CHANGED_COUNT=$(echo "$GIT_STATUS" | wc -l | tr -d ' ')
  REMINDERS+=("$CHANGED_COUNT uncommitted file(s) in working tree")
fi

# bd sync --status outputs "ahead", "dirty", or "unsync" when out of sync
if command -v bd >/dev/null 2>&1; then
  BEADS_STATUS=$(bd sync --status 2>/dev/null) || true
  if echo "$BEADS_STATUS" | grep -qi "ahead\|dirty\|unsync"; then
    REMINDERS+=("beads issues need syncing (run 'bd sync')")
  fi
fi

# Check for unpushed commits (may fail if no upstream tracking)
UNPUSHED=$(git -C "$REPO_ROOT" log --oneline '@{upstream}..HEAD' 2>/dev/null) || true
if [[ -n "$UNPUSHED" ]]; then
  COMMIT_COUNT=$(echo "$UNPUSHED" | wc -l | tr -d ' ')
  REMINDERS+=("$COMMIT_COUNT unpushed commit(s)")
fi

if [[ ${#REMINDERS[@]} -gt 0 ]]; then
  echo "Session reminder:"
  for r in "${REMINDERS[@]}"; do
    echo "  - $r"
  done
fi

exit 0
