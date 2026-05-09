#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# PostToolUse hook (Bash): after a successful `jj git push --branch <name>`,
# emit the workspace-cleanup reminder from .claude/rules/landing-the-plane.md
# so the assistant doesn't forget step 5.
#
# Fires only when:
#   - tool was Bash
#   - command was `jj git push ...` (with optional `--branch X`)
#   - exit code was 0 (success)
#   - cwd is a feature workspace (not the default), so cleanup is appropriate
#
# Soft notice (exit 0). Convenience hook; never blocks.

set -uo pipefail
trap 'exit 0' ERR

INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty' 2>/dev/null) || exit 0
EXIT_CODE=$(echo "$INPUT" | jq -r '.tool_response.exit_code // empty' 2>/dev/null) || EXIT_CODE=""

[[ -z "$COMMAND" ]] && exit 0
[[ -n "$EXIT_CODE" && "$EXIT_CODE" != "0" ]] && exit 0

# Strip quoted strings so the literal command "jj git push" inside a message
# doesn't trigger.
STRIPPED=$(printf '%s' "$COMMAND" | perl -0777 -pe "s/'[^']*'//g; s/\"[^\"]*\"//g" 2>/dev/null) || STRIPPED="$COMMAND"

# Match the actual jj git push invocation. Tolerate env-var prefixes,
# wrapper commands (env, sudo, etc.) by allowing any leading whitespace
# and any number of WORD=val tokens.
if ! echo "$STRIPPED" | command grep -qE '^[[:space:]]*([A-Za-z_][A-Za-z_0-9]*=[^[:space:]]*[[:space:]]+)*jj([[:space:]]+--no-pager)?[[:space:]]+git[[:space:]]+push'; then
  exit 0
fi

# Only nudge when in a feature workspace (not the main checkout / default).
WS_ROOT="$(jj workspace root 2>/dev/null || true)"
if [ -z "$WS_ROOT" ] || [ ! -e "$WS_ROOT/scripts/jj-main-repo.sh" ]; then
  exit 0
fi
( cd "$WS_ROOT" && . "$WS_ROOT/scripts/jj-main-repo.sh" >/dev/null 2>&1 ) || exit 0
cd "$WS_ROOT" || exit 0
. "$WS_ROOT/scripts/jj-main-repo.sh"

if [ "${IS_DEFAULT:-no}" = "yes" ]; then
  exit 0
fi

WS_NAME=$(basename "$WS_ROOT")

cat >&2 <<EOF

Push succeeded. Don't forget the workspace cleanup (per .claude/rules/landing-the-plane.md):

  cd $MAIN_REPO
  jj workspace forget $WS_NAME
  rm -rf $MAIN_REPO/../.worktrees/$WS_NAME

The 'cd $MAIN_REPO' matters — '../.worktrees/$WS_NAME' is unsafe from any nested cwd.

EOF
exit 0
