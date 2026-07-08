#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# PostToolUse hook (Bash): after a successful `git push` of a feature branch,
# emit the worktree-cleanup reminder from .claude/rules/landing-the-plane.md
# so the assistant doesn't forget step 5.
#
# Fires only when:
#   - tool was Bash
#   - command was `git push ...` (e.g. `git push -u origin <branch>`)
#   - exit code was 0 (success)
#   - cwd is a feature worktree (not the main checkout), so cleanup is appropriate
#
# Soft notice (exit 0). Convenience hook; never blocks.

set -uo pipefail
trap 'exit 0' ERR

INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty' 2>/dev/null) || exit 0
EXIT_CODE=$(echo "$INPUT" | jq -r '.tool_response.exit_code // empty' 2>/dev/null) || EXIT_CODE=""

[[ -z "$COMMAND" ]] && exit 0
[[ -n "$EXIT_CODE" && "$EXIT_CODE" != "0" ]] && exit 0

# Strip quoted strings so the literal command "git push" inside a message
# doesn't trigger.
STRIPPED=$(printf '%s' "$COMMAND" | perl -0777 -pe "s/'[^']*'//g; s/\"[^\"]*\"//g" 2>/dev/null) || STRIPPED="$COMMAND"

# Match the actual git push invocation. Tolerate env-var prefixes and leading
# global git options (`-C <path>`, `--no-pager`) so `git -C /repo push` and
# `FOO=bar git push` both trigger. Anchored on `push` as a word so `git
# push-mirror`-style typos or `git log ... push` prose don't false-match.
if ! echo "$STRIPPED" | command grep -qE '^[[:space:]]*([A-Za-z_][A-Za-z_0-9]*=[^[:space:]]*[[:space:]]+)*git[[:space:]]+(-C[[:space:]]+[^[:space:]]+[[:space:]]+)?(--no-pager[[:space:]]+)?push([[:space:]]|$)'; then
  exit 0
fi

# Only nudge when in a feature worktree (not the main checkout).
WS_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [ -z "$WS_ROOT" ] || [ ! -e "$WS_ROOT/scripts/git-main-repo.sh" ]; then
  exit 0
fi
( cd "$WS_ROOT" && . "$WS_ROOT/scripts/git-main-repo.sh" >/dev/null 2>&1 ) || exit 0
cd "$WS_ROOT" || exit 0
. "$WS_ROOT/scripts/git-main-repo.sh"

if [ "${IS_DEFAULT:-no}" = "yes" ]; then
  exit 0
fi

WS_NAME=$(basename "$WS_ROOT")

cat >&2 <<EOF

Push succeeded. Once the PR lands, don't forget the worktree cleanup (per .claude/rules/landing-the-plane.md):

  cd $MAIN_REPO
  git worktree remove $WORKTREES/$WS_NAME
  git branch -d $WS_NAME

The 'cd $MAIN_REPO' matters — '../.worktrees/$WS_NAME' is unsafe from any nested cwd.

EOF
exit 0
