#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# SessionStart hook: injects a compact search-tool-ladder cheat-sheet into the
# session's additional context — mcp__probe__* for Go symbol/AST queries, rg
# for text (never bare grep), ast-grep for structural matches — instead of
# requiring the full `dev-flow:grepping` skill to load. The dev-flow plugin's
# nudge-rg-failure PostToolUse hook points back at the full skill on demand
# after any rg failure.
#
# Output contract: emit the cheat-sheet to plain stdout (the Claude Code
# SessionStart hook concatenates stdout into the session's additional context).
# Never block session start.

set -euo pipefail

# Consume the JSON event from stdin (no field needed; honor the hook contract).
cat >/dev/null

cat <<'EOF'
## Search-tool ladder (grepping cheat-sheet)

Repo search ladder (full skill: `dev-flow:grepping` — load on demand; the
dev-flow plugin's nudge-rg-failure hook will point you at it after any rg
failure):

- **Go symbol / "where is X defined, how does Y work"** → `mcp__probe__search_code`
  first (whole AST blocks; beats grep→Read).
- **Raw text** → `rg`. NEVER bare `grep`/`egrep`/`fgrep` (PreToolUse hook nudges).
- **Structural code shapes / codemods** → `ast-grep` (`sg` alias where
  installed); NOT for pkg-qualified call patterns (misparses — use `rg`).

rg silent-failure traps (these produce WRONG results, not errors):
- `rg 'A\|B'` — `\|` matches a LITERAL pipe; alternation is bare `|`.
- `rg -rn 'pat'` — rg's `-r` is --replace and EATS `n` as replacement text;
  rg is already recursive: use `rg -n 'pat'`.

Judging command success: decide pass/fail by EXIT CODE, never by grepping
stdout/stderr for success/error strings (fixtures echo those; May 2026
pr-prep incident). Brief sub-agents on this ladder — they default to bare
grep / full-file reads without it.
EOF
