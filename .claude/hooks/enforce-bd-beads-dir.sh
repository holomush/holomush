#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# PreToolUse hook: keep `bd` invocations from a fresh git worktree from
# failing with the unhelpful "no beads database found" error.
#
# Each git worktree under `<repo-parent>/.worktrees/<name>/` gets a fresh
# checkout with an empty `.beads/` (only the tracked config + .gitignore land
# there; the Dolt database lives in the main repo's `.beads/dolt/`). When `bd`
# is invoked from such a worktree, its lookup terminates on the empty `.beads/`
# and fails. This hook intercepts the failure mode at the Bash boundary so the
# assistant gets an actionable message instead of having to debug bd's
# resolution logic.
#
# `task workspace:new` (and bd's own `.beads/redirect` mechanism) is the
# proper fix: when a worktree is created we write `.beads/redirect`
# pointing at the main repo's `.beads/`. This hook detects that the fix is
# in place and stays silent. It only fires for hand-made worktrees created
# without the redirect (e.g. a bare `git worktree add`).
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

# Whitespace-aware first-token extraction. ${seg%% *} only splits on
# space, so `time\tbd ready` would slip through as the single word
# "time\tbd". `read` honours IFS (default: space + tab + newline).
first_token() {
  local _w _rest
  read -r _w _rest <<< "$1"
  echo "$_w"
}

# Returns 0 if the WHOLE segment is a standalone BEADS_DIR assignment
# (with optional `export`). Used to track `export BEADS_DIR=…` followed
# by `bd …` in a later segment.
is_standalone_beads_dir_assignment() {
  local s
  s=$(strip_leading_ws "$1")
  if [[ "$s" =~ ^export[[:space:]]+ ]]; then
    s="${s#"${BASH_REMATCH[0]}"}"
    s=$(strip_leading_ws "$s")
  fi
  if [[ "$s" =~ ^BEADS_DIR=(\"[^\"]*\"|\'[^\']*\'|[^[:space:]]*)[[:space:]]*$ ]]; then
    return 0
  fi
  return 1
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
  local word
  word=$(first_token "$segment")
  while [[ "$word" =~ ^(env|command|exec|sudo|nice|nohup|time|builtin)$ ]]; do
    segment="${segment#"$word"}"
    segment=$(strip_leading_ws "$segment")
    local peek
    peek=$(first_token "$segment")
    while [[ "$peek" == -* ]]; do
      segment="${segment#"$peek"}"
      segment=$(strip_leading_ws "$segment")
      peek=$(first_token "$segment")
    done
    segment=$(strip_env_vars "$segment")
    word=$(first_token "$segment")
  done
  echo "$word"
}

# Resolve worktree context. If we can't determine the main repo, fail
# open — bd's own error message will surface for the user.
WS_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [ -z "$WS_ROOT" ] || [ ! -e "$WS_ROOT/scripts/git-main-repo.sh" ]; then
  exit 0
fi

# shellcheck source=../../scripts/git-main-repo.sh
( cd "$WS_ROOT" && . "$WS_ROOT/scripts/git-main-repo.sh" >/dev/null 2>&1 ) || exit 0
cd "$WS_ROOT" || exit 0
# shellcheck source=../../scripts/git-main-repo.sh
. "$WS_ROOT/scripts/git-main-repo.sh"

# In the main repo? bd's lookup will find the real .beads/ in cwd. Allow.
if [ "${IS_DEFAULT:-no}" = "yes" ]; then
  exit 0
fi

# Proper fix already in place for this worktree? Allow.
# - .beads/redirect: bd's own per-worktree override (preferred form)
# - .beads/dolt/: the Dolt directory got materialised here somehow
if [ -f "$WS_ROOT/.beads/redirect" ] || [ -d "$WS_ROOT/.beads/dolt" ]; then
  exit 0
fi

# Strip single- and double-quoted string contents (across newlines) before
# segment-splitting so commands like `git commit -m 'msg containing bd'`
# don't false-trigger. See enforce-gh-repo.sh for the same pattern + caveats.
STRIPPED=$(printf '%s' "$COMMAND" | perl -0777 -pe "s/'[^']*'//g; s/\"[^\"]*\"//g" 2>/dev/null) || STRIPPED="$COMMAND"
SEGMENTS=$(printf '%s' "$STRIPPED" | awk '{gsub(/ *&& */, "\n"); gsub(/ *; */, "\n"); gsub(/ *\|\| */, "\n"); print}')

# Track whether an earlier segment exported BEADS_DIR so chained
# commands like `export BEADS_DIR=...; bd ready` aren't false-positive
# blocked.
beads_dir_seen=0

while IFS= read -r segment; do
  [[ -z "$segment" ]] && continue

  # Standalone BEADS_DIR assignment in a prior segment opts out of the gate.
  if is_standalone_beads_dir_assignment "$segment"; then
    beads_dir_seen=1
    continue
  fi

  # Inspect every component of the pipeline, not just the leftmost.
  # `git log | bd ...` would otherwise silently bypass the gate.
  PIPE_PARTS=$(printf '%s\n' "$segment" | awk '{gsub(/ *\| */, "\n"); print}')
  triggered_part=""
  while IFS= read -r part; do
    [[ -z "$part" ]] && continue
    if has_beads_dir_env "$part" || [[ $beads_dir_seen -eq 1 ]]; then
      continue
    fi
    word=$(first_cmd_word "$part")
    if [ "$word" = "bd" ]; then
      triggered_part="$part"
      break
    fi
  done <<< "$PIPE_PARTS"

  if [ -n "$triggered_part" ]; then
    # Note: $triggered_part is derived from STRIPPED (quote-stripped form), so
    # we don't interpolate it into the suggestion — quoted args (e.g.,
    # `bd note 'foo bar'`) would be lost. The user has the original command
    # in their shell history; we just tell them how to make it work.
    cat >&2 <<EOF
\`bd\` invoked from a git worktree ($WS_ROOT) without BEADS_DIR set.
The worktree's .beads/ is empty; bd's lookup will fail with "no beads
database found".

Pick one:

  • Prepend BEADS_DIR to your bd command, e.g.:
        BEADS_DIR='$MAIN_REPO/.beads' bd ready

  • Permanent fix for this worktree (one-time, then bd works bare):
        printf '%s\n' '$MAIN_REPO/.beads' > '$WS_ROOT/.beads/redirect'

The permanent fix uses bd's own per-worktree redirect mechanism;
\`task workspace:new\` writes it automatically for new worktrees, but
this one was created without it (holomush-k98d).
EOF
    exit 2
  fi
done <<< "$SEGMENTS"

exit 0
