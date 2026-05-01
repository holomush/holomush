#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# PreToolUse hook: keep `bd` invocations from a jj workspace from failing
# with the unhelpful "no beads database found" error.
#
# Each jj workspace under `<repo-parent>/.worktrees/<name>/` materialises an
# empty `.beads/` (only the tracked config + .gitignore land there; the Dolt
# database lives in the main repo's `.beads/dolt/`). When `bd` is invoked
# from such a workspace, its lookup terminates on the empty `.beads/` and
# fails. This hook intercepts the failure mode at the Bash boundary so the
# assistant gets an actionable message instead of having to debug bd's
# resolution logic.
#
# `task workspace:new` (and bd's own `.beads/redirect` mechanism) is the
# proper fix: when a workspace is created we write `.beads/redirect`
# pointing at the main repo's `.beads/`. This hook detects that the fix is
# in place and stays silent. It only fires for legacy workspaces created
# before the redirect-writing change landed.
#
# Error strategy: same as enforce-task-runner.sh — fail open on parse
# errors (bd command proceeds and bd's own error surfaces if it still
# can't find the DB), block reliably when we know there's a problem.

set -uo pipefail

# --- Parse phase: fail open on malformed input ---
trap 'exit 0' ERR

INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty' 2>/dev/null) || {
  echo "enforce-bd-beads-dir: failed to parse input — enforcement disabled for this command" >&2
  exit 0
}

[[ -z "$COMMAND" ]] && exit 0

# --- Enforcement phase ---
trap - ERR

strip_leading_ws() {
  local s="$1"
  echo "${s#"${s%%[![:space:]]*}"}"
}

# Returns 0 if the segment's env-var prefix sets BEADS_DIR, else 1.
# Matches BEADS_DIR=anything (quoted or not) before the first non-assignment word.
has_beads_dir_env() {
  local s
  s=$(strip_leading_ws "$1")
  while [[ "$s" =~ ^([A-Za-z_][A-Za-z_0-9]*)=(\"[^\"]*\"|\'[^\']*\'|[^[:space:]]*)[[:space:]] ]]; do
    if [[ "${BASH_REMATCH[1]}" == "BEADS_DIR" ]]; then
      return 0
    fi
    s="${s#"${BASH_REMATCH[0]}"}"
    s=$(strip_leading_ws "$s")
  done
  return 1
}

# Same env-stripping shape as enforce-task-runner.sh::first_cmd_word, but
# duplicated rather than sourced — the existing helper inlines its
# state-mutation logic, and copying keeps each hook independently
# testable and replaceable.
strip_env_vars() {
  local s="$1"
  while [[ "$s" =~ ^[A-Za-z_][A-Za-z_0-9]*=(\"[^\"]*\"|\'[^\']*\'|[^[:space:]]*)[[:space:]] ]]; do
    s="${s#"${BASH_REMATCH[0]}"}"
    s=$(strip_leading_ws "$s")
  done
  echo "$s"
}

first_cmd_word() {
  local segment="$1"
  segment=$(strip_leading_ws "$segment")
  segment=$(strip_env_vars "$segment")
  local word="${segment%% *}"
  while [[ "$word" =~ ^(env|command|exec|sudo|nice|nohup)$ ]]; do
    segment="${segment#"$word"}"
    segment=$(strip_leading_ws "$segment")
    while [[ "${segment%% *}" == -* ]]; do
      segment="${segment#"${segment%% *}"}"
      segment=$(strip_leading_ws "$segment")
    done
    segment=$(strip_env_vars "$segment")
    word="${segment%% *}"
  done
  echo "$word"
}

# Resolve workspace context. If we can't determine the main repo, fail
# open — bd's own error message will surface for the user.
WS_ROOT="$(jj workspace root 2>/dev/null || true)"
if [ -z "$WS_ROOT" ] || [ ! -e "$WS_ROOT/scripts/jj-main-repo.sh" ]; then
  exit 0
fi

# shellcheck source=../../scripts/jj-main-repo.sh
( cd "$WS_ROOT" && . "$WS_ROOT/scripts/jj-main-repo.sh" >/dev/null 2>&1 ) || exit 0
cd "$WS_ROOT" || exit 0
# shellcheck source=../../scripts/jj-main-repo.sh
. "$WS_ROOT/scripts/jj-main-repo.sh"

# In the main repo? bd's lookup will find the real .beads/ in cwd. Allow.
if [ "${IS_DEFAULT:-no}" = "yes" ]; then
  exit 0
fi

# Proper fix already in place for this workspace? Allow.
# - .beads/redirect: bd's own per-worktree override (preferred form)
# - .beads/dolt/: the Dolt directory got materialised here somehow
if [ -f "$WS_ROOT/.beads/redirect" ] || [ -d "$WS_ROOT/.beads/dolt" ]; then
  exit 0
fi

# Same segment-splitting pattern as enforce-task-runner.sh.
SEGMENTS=$(echo "$COMMAND" | awk '{gsub(/ *&& */, "\n"); gsub(/ *; */, "\n"); gsub(/ *\|\| */, "\n"); print}')

while IFS= read -r segment; do
  [[ -z "$segment" ]] && continue
  before_pipe="${segment%%|*}"

  # If the user explicitly set BEADS_DIR=... in this segment, trust them.
  if has_beads_dir_env "$before_pipe"; then
    continue
  fi

  word=$(first_cmd_word "$before_pipe")
  if [ "$word" = "bd" ]; then
    cat >&2 <<EOF
\`bd\` invoked from a jj workspace ($WS_ROOT)
without BEADS_DIR set. The workspace's .beads/ is empty; bd's lookup will
fail with "no beads database found".

Pick one:

  • Prepend BEADS_DIR to this single command:
        BEADS_DIR='$MAIN_REPO/.beads' $segment

  • Permanent fix for this workspace (one-time, then bd works bare):
        printf '%s\n' '$MAIN_REPO/.beads' > '$WS_ROOT/.beads/redirect'

The permanent fix uses bd's own per-worktree redirect mechanism;
\`task workspace:new\` writes it automatically for new workspaces, but
this workspace predates that change (holomush-k98d).
EOF
    exit 2
  fi
done <<< "$SEGMENTS"

exit 0
