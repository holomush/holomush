#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Stop hook: remind about unsynced beads and uncommitted changes
# Fires when the agent finishes responding (Stop event). Only outputs
# when there's actually unsynced work to avoid noise.
# Does not fire on user interrupts (Ctrl+C) — only when Claude finishes responding.
# Error strategy: convenience hook — fails open (errors don't block).
set -uo pipefail
trap 'exit 0' ERR

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
  CHANGED_COUNT=$(echo "$GIT_STATUS" | grep -c .)
  REMINDERS+=("$CHANGED_COUNT uncommitted file(s) in working tree")
fi

# bd sync --status shows "Pending changes: none" when fully synced.
# Trigger reminder if pending changes or conflicts exist.
if command -v bd >/dev/null 2>&1; then
  BEADS_STATUS=$(bd sync --status 2>/dev/null) || {
    REMINDERS+=("beads sync check failed — run 'bd sync --status' manually")
    BEADS_STATUS=""
  }
  if [[ -n "$BEADS_STATUS" ]] && ! echo "$BEADS_STATUS" | command grep -q "Pending changes: none"; then
    REMINDERS+=("beads issues need syncing (run 'bd sync')")
  fi
fi

# Check for unpushed commits (may fail if no upstream tracking)
UNPUSHED=$(git -C "$REPO_ROOT" log --oneline '@{upstream}..HEAD' 2>/dev/null) || true
if [[ -n "$UNPUSHED" ]]; then
  COMMIT_COUNT=$(echo "$UNPUSHED" | grep -c .)
  REMINDERS+=("$COMMIT_COUNT unpushed commit(s)")
fi

if [[ ${#REMINDERS[@]} -gt 0 ]]; then
  echo "Session reminder:"
  for r in "${REMINDERS[@]}"; do
    echo "  - $r"
  done
fi

exit 0
