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

# `bd dolt status` is intentionally NOT checked here: uncommitted Dolt
# working-set state is a session-close concern (handled by the landing
# checklist), not a per-turn one, and its server round-trip dominated this
# Stop hook's latency. Only the in-progress-claim check below remains.
if command -v bd >/dev/null 2>&1; then
  # Stranded in-progress claims: beads still open and in_progress for the
  # current actor. Catches "claimed but never closed" patterns when a
  # session ends mid-task without a handoff. Best-effort — silent on errors.
  ACTOR=$(git -C "$REPO_ROOT" config user.email 2>/dev/null) || ACTOR=""
  if [[ -n "$ACTOR" ]]; then
    IN_PROG=$(bd list --status in_progress --assignee "$ACTOR" --json 2>/dev/null | jq -r 'length' 2>/dev/null) || IN_PROG=0
    if [[ "$IN_PROG" -gt 0 ]] 2>/dev/null; then
      REMINDERS+=("$IN_PROG bead(s) in_progress under your assignee — close, hand off, or note current state")
    fi
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
