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

if [ "$fail_count" -gt 0 ]; then
  echo "$fail_count check(s) failed." >&2
  exit 1
fi
echo "adr-doctor: all checks passed."
exit 0
