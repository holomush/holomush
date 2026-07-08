#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# WorktreeCreate hook: maps Claude Code's `isolation: "worktree"` request to
# a fresh git worktree under <repo-parent>/.worktrees/<name> via the existing
# `task workspace:new -- <name>` primitive (which is idempotent and fetches
# origin/main first).
#
# Contract (from https://code.claude.com/docs/en/hooks.md):
#   - Stdin: JSON event with common fields (session_id, cwd, hook_event_name).
#     Runtime-provided event-specific fields (e.g. worktree_path, base_ref)
#     are not part of the public spec and may vary; we read them tolerantly.
#   - Stdout (success): absolute path of the worktree, plain text.
#   - Exit non-zero: aborts subagent creation.
#
# Strategy:
#   1. Read stdin JSON.
#   2. Derive a workspace NAME:
#        - If runtime provided worktree_path/.path/.name, use its basename.
#        - Sanitize to match the [A-Za-z0-9._-]+ regex `task workspace:new`
#          enforces; fall back to "agent-<short-session-id>" if empty.
#   3. Run `task workspace:new -- <NAME>`; its stdout is the absolute path.
#   4. Print the absolute path on stdout.
#
# All diagnostic output goes to stderr so it doesn't pollute the path
# stdout the runtime parses.

set -euo pipefail

# jq is required. With jq missing, parse failures would silently invent
# `agent-<datetime>` and ignore the runtime-supplied worktree_path,
# producing a workspace at a path the runtime didn't ask for. Fail loud:
# WorktreeCreate is blocking, so non-zero exit aborts the subagent
# dispatch — preferable to a name mismatch.
command -v jq >/dev/null || {
  echo "worktree-create.sh: jq not found in PATH; required to parse hook payload" >&2
  exit 1
}

# Read stdin JSON (may be empty if invoked manually for testing).
INPUT="$(cat || true)"

# Tolerant extraction. jq's // operator falls through nulls in order.
RAW_NAME="$(
  printf '%s' "$INPUT" | jq -r '
    (.worktree_path // .worktreePath // .path // .name // "") as $v
    | $v | sub(".*/"; "")
  '
)"

SHORT_SID="$(
  printf '%s' "$INPUT" | jq -r '.session_id // ""' \
    | tr -d -c 'A-Za-z0-9' \
    | cut -c1-8
)"

# Sanitize: strip anything outside [A-Za-z0-9._-]; replace runs with '-'.
SAFE_NAME="$(printf '%s' "$RAW_NAME" | LC_ALL=C tr -c 'A-Za-z0-9._-' '-' | sed -E 's/^-+|-+$//g; s/-+/-/g')"

# Reject `.`, `..`, anything containing `..`, or empty.
case "$SAFE_NAME" in
  ''|'.'|'..'|*..*) SAFE_NAME='' ;;
esac

if [ -z "$SAFE_NAME" ]; then
  if [ -n "$SHORT_SID" ]; then
    SAFE_NAME="agent-${SHORT_SID}"
  else
    SAFE_NAME="agent-$(date +%Y%m%d%H%M%S)"
  fi
fi

# Resolve a working directory we can run task from. The hook's cwd may be
# any worktree (or the main repo); git rev-parse --show-toplevel works in either.
WS_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [ -z "$WS_ROOT" ]; then
  echo "ERROR: worktree-create.sh: not in a git repository (git rev-parse --show-toplevel failed)" >&2
  exit 1
fi
cd "$WS_ROOT"

# `task workspace:new` is idempotent: if .worktrees/<NAME> already exists it
# prints the path and exits 0. `task` already routes its diagnostic echoes
# to fd 2 by default, so capturing stdout via $(...) gives us path-only on
# our stdout. The post-check is defense-in-depth against future changes
# to the `workspace:new` task definition.
NEW_PATH="$(task workspace:new -- "$SAFE_NAME" | tail -n 1)"

if [ -z "$NEW_PATH" ] || [ ! -d "$NEW_PATH" ]; then
  echo "ERROR: worktree-create.sh: task workspace:new produced no usable path (got: '${NEW_PATH}')" >&2
  exit 1
fi

printf '%s\n' "$NEW_PATH"
