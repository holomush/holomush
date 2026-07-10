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

# Prerequisites. (ADR ids are self-minted since the 2026-07-09 beads-tracker
# retirement; all checks are file-level and run everywhere, including CI.)
[ -d "$ADR_DIR" ] || { echo "missing $ADR_DIR" >&2; exit 2; }
[ -f "$SPEC" ] || { echo "missing $SPEC (invariant_coverage meta-test cannot run)" >&2; exit 2; }

# --- count_real_files (INV-A12) ---
note "count_real_files (INV-A12)"
real=$(find "$ADR_DIR" -maxdepth 1 -type f -name '*.md' \
  | grep -E '/[a-z][a-z0-9-]+-[a-z][a-z0-9-]+\.md$' \
  | grep -vE '/[0-9]{4}-' \
  | wc -l | tr -d ' ')
if [ "$real" -lt "17" ]; then
  check_fail "expected at least 17 <adr-id>-<slug>.md files after migration; got $real"
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
# Relational: real + (fixed legacy stubs) + README. Grows naturally as new
# ADRs are added via /capture-adrs; only the legacy stub count is pinned.
expected_total=$((real + stubs + 1))
if [ "$total" != "$expected_total" ]; then
  check_fail "expected $expected_total files total in $ADR_DIR (real=$real + stubs=$stubs + README); got $total"
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
  # Extract the actual markdown link TARGET (the part in parentheses),
  # not the display text. Defensive against future stub-format changes
  # where display text might diverge from target.
  target=$(sed -nE 's@.*\[[^]]+\]\((holomush-[a-z0-9-]+\.md)\).*@\1@p' "$stub" | head -1)
  if [ -z "$target" ]; then
    check_fail "$stub: no markdown link target found in stub body"
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
  # Tighten: adr-id format is 'holomush-XXXX'; pull the first 'holomush-XXXX' substring.
  bd_id_from_filename=$(echo "$bn" | grep -oE '^holomush-[a-z0-9]+' || true)
  decision_line=$(grep -E '^\*\*Decision:\*\*\s+holomush-' "$f" | head -1)
  if [ -z "$decision_line" ]; then
    check_fail "$f: missing **Decision:** holomush-<id> header"
    continue
  fi
  decision_id=$(echo "$decision_line" | grep -oE 'holomush-[a-z0-9]+')
  if [ "$decision_id" != "$bd_id_from_filename" ]; then
    check_fail "$f: **Decision:** $decision_id does not match filename adr-id $bd_id_from_filename"
    continue
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
# Optional: shellcheck is its own pipeline in `task lint` (covers all hook
# scripts), so we don't duplicate the check here if the binary isn't on
# PATH. The standalone `task lint:shellcheck` (or equivalent) is the
# authoritative shellcheck gate.
if command -v shellcheck >/dev/null 2>&1; then
  if ! shellcheck "$HOOK" >/dev/null 2>&1; then
    check_fail "$HOOK: shellcheck failed"
  fi
fi

# --- forbid_skill_commits (INV-A2) ---
note "forbid_skill_commits (INV-A2)"
SKILL="$REPO_ROOT/.claude/skills/capture-adrs/SKILL.md"
if [ -f "$SKILL" ]; then
  # Look for command-shaped forbidden strings (in code blocks or inline-command form).
  # The Anti-patterns section may mention them in 'DO NOT commit' prose; allow that.
  if grep -qE '(^\s*\$\s*(git commit|git add)|^\s*`(git commit|git add)`)' "$SKILL"; then
    check_fail "$SKILL: contains a commit/describe command — skill MUST NOT commit"
  fi
fi

# --- supersession_edges (INV-A13) ---
# File-level since the beads-tracker retirement: a "Superseded by X" status
# must point at an ADR file that actually exists.
note "supersession_edges (INV-A13)"
for f in "$ADR_DIR"/holomush-*-*.md; do
  [ -f "$f" ] || continue
  status=$(grep -E '^\*\*Status:\*\*\s+Superseded by\s+holomush-' "$f" | head -1 || true)
  [ -n "$status" ] || continue
  superseder=$(echo "$status" | grep -oE 'holomush-[a-z0-9]+')
  if ! ls "$ADR_DIR/$superseder"-*.md >/dev/null 2>&1; then
    check_fail "$f: Status says superseded by $superseder, but no $superseder-*.md file exists"
  fi
done

# --- invariant_coverage (meta-test) ---
note "invariant_coverage"
SURFACE_ENUM='doctor|hook-test|skill-test|migration-assert|manual'
# Extract every '| INV-A<n> | <surface> |' row from the spec.
while IFS='|' read -r _ id surface _; do
  id="$(echo "$id" | tr -d ' ')"
  surface="$(echo "$surface" | sed 's/^ *//;s/ *$//')"
  case "$id" in INV-A[0-9]*) ;; *) continue ;; esac
  if [ -z "$surface" ]; then
    check_fail "meta-test: $id has empty Surface column"
    continue
  fi
  # Validate every '+' -joined token is in the enum.
  IFS='+' read -ra toks <<< "$(echo "$surface" | tr -d ' `')"
  for tok in "${toks[@]}"; do
    case "$tok" in
      doctor|hook-test|skill-test|migration-assert|manual) ;;
      *) check_fail "meta-test: $id has Surface token '$tok' not in {$SURFACE_ENUM}" ;;
    esac
  done
  # If doctor-tagged, this script must mention '# INV-A<n>' in a comment.
  if echo "$surface" | grep -q 'doctor'; then
    if ! grep -qE "#.*$id\b" "$0"; then
      check_fail "meta-test: $id is doctor-tagged but no '# $id' comment exists in adr-doctor.sh"
    fi
  fi
done < "$SPEC"

if [ "$fail_count" -gt 0 ]; then
  echo "$fail_count check(s) failed." >&2
  exit 1
fi
echo "adr-doctor: all checks passed."
exit 0
