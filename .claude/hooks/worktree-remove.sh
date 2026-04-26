#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# WorktreeRemove hook: cleans up the jj workspace + on-disk directory that
# WorktreeCreate provisioned. Mirrors the manual cleanup documented in
# CLAUDE.md "Landing the Plane":
#
#     cd <repo-root>
#     jj workspace forget <name>
#     rm -rf <path>
#
# Contract (from https://code.claude.com/docs/en/hooks.md):
#   - Fire-and-forget. No decision control. Failures are logged in debug
#     mode only; the runtime proceeds regardless. We still try to be
#     idempotent and silent on the happy path.
#   - Stdin: JSON event with common fields and (per runtime) worktree_path.
#
# Safety:
#   - We refuse to touch any path not strictly under the project's
#     <repo-parent>/.worktrees/ directory. The guard is symlink-resolved
#     via `pwd -P` and rejects `..` segments lexically *before* resolution,
#     because a non-existent path with `..` would otherwise fall through
#     `cd` and bypass a pure prefix check (`/parent/.worktrees/../../etc`
#     lexically matches `/parent/.worktrees/*`).

set -euo pipefail

# jq is required — silently inventing names on parse failure would let a
# spoofed/missing payload still trigger work. Be loud and exit non-zero.
command -v jq >/dev/null || {
  echo "worktree-remove.sh: jq not found in PATH; cannot parse hook payload" >&2
  exit 0  # fire-and-forget: do not block runtime
}

INPUT="$(cat || true)"

WT_PATH="$(printf '%s' "$INPUT" | jq -r '.worktree_path // .worktreePath // .path // ""')"

if [ -z "$WT_PATH" ]; then
  echo "worktree-remove.sh: no worktree_path in payload; nothing to do" >&2
  exit 0
fi

# Lexical reject of `..` segments BEFORE any path resolution. This is the
# load-bearing safety check — `pwd -P` cannot canonicalize a non-existent
# path on macOS (no GNU `realpath -m`), so we must refuse traversal
# tokens up front. Trailing slash is also rejected to avoid an empty
# basename downstream. Match only true segment-boundary `..` so that
# legitimate filenames containing `..` (e.g. `acme..labs`) are not
# falsely refused.
case "$WT_PATH" in
  ..|*/..|*/../*|*/)
    echo "worktree-remove.sh: refusing path with '..' segments or trailing '/': $WT_PATH" >&2
    exit 0
    ;;
esac

# Require the path to actually exist on disk before we touch it. If it
# doesn't, there's nothing to clean up and we cannot symlink-resolve.
if [ ! -d "$WT_PATH" ]; then
  echo "worktree-remove.sh: path does not exist or is not a directory: $WT_PATH" >&2
  exit 0
fi

# Resolve repo root and the .worktrees parent. jj workspace root works from
# any workspace, including the one being removed (we'll cd out of it before
# calling `jj workspace forget`).
WS_ROOT="$(jj workspace root 2>/dev/null || true)"
if [ -z "$WS_ROOT" ]; then
  echo "worktree-remove.sh: not in a jj repo; skipping" >&2
  exit 0
fi
cd "$WS_ROOT"
# shellcheck source=../../scripts/jj-main-repo.sh
. "$WS_ROOT/scripts/jj-main-repo.sh"

# WORKTREES is set by jj-main-repo.sh as <repo-parent>/.worktrees. Both
# sides are now symlink-resolved (the existence check above guarantees
# the cd succeeds).
ABS_WT="$(cd "$WT_PATH" && pwd -P)"
ABS_PARENT="$(cd "$WORKTREES" && pwd -P)"

case "$ABS_WT" in
  "$ABS_PARENT"/*) : ;;
  *)
    echo "worktree-remove.sh: refusing to remove '$ABS_WT' — not under '$ABS_PARENT'" >&2
    exit 0
    ;;
esac

NAME="${ABS_WT##*/}"
[ -n "$NAME" ] || {
  echo "worktree-remove.sh: empty workspace name from path '$ABS_WT'" >&2
  exit 0
}

# Run from the main repo so `jj workspace forget` is unambiguous and we
# don't try to forget the workspace whose working copy is our cwd.
cd "$MAIN_REPO"

# Order: rm-rf first, then jj workspace forget. The dominant failure mode
# of two-step cleanup is "step 1 succeeds, step 2 fails". With this order
# the recoverable orphan is a dangling jj workspace ref (which `jj
# workspace forget` cleanly handles on next attempt). The opposite order
# leaves an on-disk orphan dir under .worktrees/ that `task workspace:new`
# would silently re-adopt via its idempotent `[ -d "$TARGET" ]` branch.
rm -rf -- "$ABS_WT"
jj workspace forget "$NAME" || \
  echo "worktree-remove.sh: jj workspace forget '$NAME' failed (already forgotten?)" >&2
