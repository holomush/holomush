#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# SessionStart hook: requires the `dev-flow:grepping` skill to be loaded at the
# start of every session, alongside the `jj:jujutsu` skill (which the jj
# plugin's own SessionStart hook enforces). The grepping skill establishes this
# repo's search-tool ladder — mcp__probe__* for Go symbol/AST queries, rg for
# text (never bare grep), ast-grep for structural matches — so loading it up
# front prevents defaulting to bare grep / full-file reads.
#
# Output contract: emit the requirement to plain stdout (the Claude Code
# SessionStart hook concatenates stdout into the session's additional context).
# Never block session start.

set -euo pipefail

# Consume the JSON event from stdin (no field needed; honor the hook contract).
cat >/dev/null

cat <<'EOF'
## REQUIRED: Load grepping skill

You MUST invoke the Skill tool with skill="dev-flow:grepping" BEFORE your first
response in this session — alongside the required jj:jujutsu skill. It establishes
the repo's search-tool ladder (mcp__probe__* for Go symbol/AST queries, rg for
text — never bare grep, ast-grep for structural matches and codemods) per
.claude/rules/search-tools.md. Brief sub-agents on the same ladder; they default
to rg / full-file reads without it.
EOF
