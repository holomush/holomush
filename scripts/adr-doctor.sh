#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Durable health check for docs/adr/. Wired into `task lint` via the
# lint:adr target. Exits 0 if clean; 1 on any check failure; 2 on
# missing prerequisites.
#
# See docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md
# §"`adr-doctor.sh` health check".

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ADR_DIR="$REPO_ROOT/docs/adr"
SPEC="$REPO_ROOT/docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md"

explain=0
if [ "${1:-}" = "--explain" ]; then
  explain=1
fi

fail_count=0

note() {
  [ "$explain" = "1" ] && echo "→ $*" >&2
}

check_fail() {
  echo "FAIL: $*" >&2
  fail_count=$((fail_count + 1))
}

# Prerequisites.
command -v bd  >/dev/null || { echo "missing prerequisite: bd"  >&2; exit 2; }
command -v jq  >/dev/null || { echo "missing prerequisite: jq"  >&2; exit 2; }
[ -d "$ADR_DIR" ] || { echo "missing $ADR_DIR" >&2; exit 2; }

# --- count_real_files (INV-A12) ---
note "count_real_files (INV-A12)"
real=$(find "$ADR_DIR" -maxdepth 1 -type f -name '*.md' \
  | grep -E '/[a-z][a-z0-9-]+-[a-z][a-z0-9-]+\.md$' \
  | grep -vE '/[0-9]{4}-' \
  | wc -l | tr -d ' ')
if [ "$real" != "17" ]; then
  check_fail "expected 17 <bd-id>-<slug>.md files; got $real"
fi

# --- count_stubs (INV-A12) ---
note "count_stubs (INV-A12)"
stubs=$(find "$ADR_DIR" -maxdepth 1 -type f -name '*.md' \
  | grep -E '/[0-9]{4}-' \
  | wc -l | tr -d ' ')
if [ "$stubs" != "17" ]; then
  check_fail "expected 17 [0-9]{4}-*.md stubs; got $stubs"
fi

# --- count_readme (INV-A12) ---
note "count_readme (INV-A12)"
if [ ! -f "$ADR_DIR/README.md" ]; then
  check_fail "missing $ADR_DIR/README.md"
fi
total=$(find "$ADR_DIR" -maxdepth 1 -type f -name '*.md' | wc -l | tr -d ' ')
if [ "$total" != "35" ]; then
  check_fail "expected 35 files total in $ADR_DIR; got $total"
fi

# --- no_legacy_subdir (INV-A12) ---
note "no_legacy_subdir (INV-A12)"
if [ -d "$ADR_DIR/legacy" ]; then
  check_fail "$ADR_DIR/legacy must not exist (stubs MUST stay flat)"
fi

# --- stub_links_real (INV-A12) ---
note "stub_links_real (INV-A12)"
for stub in "$ADR_DIR"/[0-9][0-9][0-9][0-9]-*.md; do
  [ -f "$stub" ] || continue
  target=$(grep -oE '\[`[a-z0-9-]+\.md`\]' "$stub" | head -1 | tr -d '[]`' )
  if [ -z "$target" ]; then
    check_fail "$stub: no link found in stub body"
    continue
  fi
  if [ ! -f "$ADR_DIR/$target" ]; then
    check_fail "$stub: links to missing $target"
  fi
done

# --- file_has_decision_header (INV-A4, INV-A5) ---
note "file_has_decision_header (INV-A4, INV-A5)"
for f in "$ADR_DIR"/*-*.md; do
  [ -f "$f" ] || continue
  case "$(basename "$f")" in
    [0-9][0-9][0-9][0-9]-*) continue ;;  # stub
    README.md) continue ;;
  esac
  bn=$(basename "$f")
  bd_id_from_filename="${bn%-*.md}"  # naive: take everything before the last '-<slug>.md'
  # Tighten: bd-id format is 'holomush-XXXX'; pull the first 'holomush-XXXX' substring.
  bd_id_from_filename=$(echo "$bn" | grep -oE '^holomush-[a-z0-9]+' || true)
  decision_line=$(grep -E '^\*\*Decision:\*\*\s+holomush-' "$f" | head -1)
  if [ -z "$decision_line" ]; then
    check_fail "$f: missing **Decision:** holomush-<id> header"
    continue
  fi
  decision_id=$(echo "$decision_line" | grep -oE 'holomush-[a-z0-9]+')
  if [ "$decision_id" != "$bd_id_from_filename" ]; then
    check_fail "$f: **Decision:** $decision_id does not match filename bd-id $bd_id_from_filename"
    continue
  fi
  if ! bd show "$decision_id" >/dev/null 2>&1; then
    check_fail "$f: bd show $decision_id failed (record missing)"
  fi
done

# --- file_has_validator_sections (INV-A4) ---
note "file_has_validator_sections (INV-A4)"
for f in "$ADR_DIR"/holomush-*-*.md; do
  [ -f "$f" ] || continue
  for hdr in '## Decision' '## Rationale' '## Alternatives Considered'; do
    if ! grep -qF "$hdr" "$f"; then
      check_fail "$f: missing $hdr header"
    fi
  done
done

# --- agent_frontmatter (INV-A14, INV-A15) ---
note "agent_frontmatter (INV-A14, INV-A15)"
AGENT="$REPO_ROOT/.claude/agents/adr-extractor.md"
if [ ! -f "$AGENT" ]; then
  check_fail "agent file missing: $AGENT"
else
  # Extract YAML between the first pair of '---' lines.
  fm=$(awk '/^---$/{c++; next} c==1' "$AGENT")
  if ! printf '%s\n' "$fm" | grep -qE '^model:\s+sonnet\s*$'; then
    check_fail "$AGENT: model must be sonnet"
  fi
  if printf '%s\n' "$fm" | grep -qE '^\s+-\s+(Write|Edit|NotebookEdit)\s*$'; then
    check_fail "$AGENT: tools list MUST NOT include Write/Edit/NotebookEdit"
  fi
fi

# --- hook_executable ---
note "hook_executable"
HOOK="$REPO_ROOT/.claude/hooks/nudge-adr-capture.sh"
if [ ! -x "$HOOK" ]; then
  check_fail "$HOOK: not executable"
fi
if ! shellcheck "$HOOK" >/dev/null 2>&1; then
  check_fail "$HOOK: shellcheck failed"
fi

# --- forbid_skill_commits (INV-A2) ---
note "forbid_skill_commits (INV-A2)"
SKILL="$REPO_ROOT/.claude/skills/capture-adrs/SKILL.md"
if [ -f "$SKILL" ]; then
  # Look for command-shaped forbidden strings (in code blocks or inline-command form).
  # The Anti-patterns section may mention them in 'DO NOT commit' prose; allow that.
  if grep -qE '(^\s*\$\s*(jj commit|jj describe|git commit|git add)|^\s*`(jj commit|jj describe|git commit|git add)`)' "$SKILL"; then
    check_fail "$SKILL: contains a commit/describe command — skill MUST NOT commit"
  fi
fi

# --- supersession_edges (INV-A13) ---
note "supersession_edges (INV-A13)"
for f in "$ADR_DIR"/holomush-*-*.md; do
  [ -f "$f" ] || continue
  status=$(grep -E '^\*\*Status:\*\*\s+Superseded by\s+holomush-' "$f" | head -1 || true)
  [ -n "$status" ] || continue
  this_id=$(grep -oE '^\*\*Decision:\*\*\s+holomush-[a-z0-9]+' "$f" | grep -oE 'holomush-[a-z0-9]+')
  superseder=$(echo "$status" | grep -oE 'holomush-[a-z0-9]+')
  # Confirm bd dep list shows the edge.
  if ! bd dep list "$superseder" 2>/dev/null | grep -q "supersedes.*$this_id"; then
    check_fail "$f: Status says superseded by $superseder, but bd dep edge missing"
  fi
done

if [ "$fail_count" -gt 0 ]; then
  echo "$fail_count check(s) failed." >&2
  exit 1
fi
echo "adr-doctor: all checks passed."
exit 0
