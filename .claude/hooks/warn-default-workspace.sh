#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# SessionStart hook: warns the assistant when the Claude Code session is
# operating in the shared `default` jj workspace. Stays silent when the
# session is in any other workspace.
#
# Output contract: emit warning text to plain stdout (the Claude Code
# SessionStart hook concatenates stdout into the session's additional
# context). Stay silent (exit 0, no output) when no warning is needed.

set -euo pipefail

# Consume the JSON event from stdin (we don't need any field; just being
# polite to the hook contract).
cat >/dev/null

# Source the shared MAIN_REPO/IS_DEFAULT helper. The hook script's cwd is
# the Claude session's launching cwd, so .jj/repo resolution from . is
# the right starting point.
ws_root="$(jj workspace root 2>/dev/null || true)"
if [ -z "$ws_root" ] || [ ! -e "$ws_root/scripts/jj-main-repo.sh" ]; then
  # Not in a jj repo, or helper missing — silently exit. The hook is
  # purely informational; never block session start.
  exit 0
fi

# shellcheck source=../../scripts/jj-main-repo.sh
( cd "$ws_root" && . "$ws_root/scripts/jj-main-repo.sh" >/dev/null 2>&1 ) || exit 0

# Re-source in current shell to populate IS_DEFAULT (the subshell above
# only validated the script doesn't error; we need the var here).
cd "$ws_root"
# shellcheck source=../../scripts/jj-main-repo.sh
. "$ws_root/scripts/jj-main-repo.sh"

if [ "${IS_DEFAULT:-no}" != "yes" ]; then
  exit 0
fi

cat <<'EOF'
**⚠️ You are in the shared `default` jj workspace — read-only inspection ONLY.**

You MUST NOT edit files here. Concurrent agent sessions share this workspace and
collide at every `jj` command boundary (jj snapshots the working copy on every
command), silently corrupting each other's uncommitted edits. Before editing ANY
file, isolate this session first:

- **Agents (or humans without the function):** run `task workspace:new -- <name>`, then `cd <printed-path>` and do all work there.
- **Humans:** run `claude-iso <name>` (the shell function in `~/.config/fish/config.fish` or `~/.bashrc` — see CLAUDE.md "Session isolation").

Read-only work (search, reads, answering questions) is fine to continue here.
EOF
