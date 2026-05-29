#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# check-docs-quality.sh — SP5 docs quality structural invariants (INV-1/2/4/7).
#
# INV-1 rubric+audit: both the style-guide rubric page and the audit file exist.
# INV-2 row-parity:   audit data row count == content-page count.
# INV-4 CardGrid:     every section index.mdx contains a <CardGrid>.
# INV-7 social links: astro.config.mjs has a GitHub repo link AND a Discussions link.
#
# Advisory only (non-gating): terminology proximity grep for location/room misuse.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONTENT="$REPO_ROOT/site/src/content/docs"
CONFIG="$REPO_ROOT/site/astro.config.mjs"
AUDIT="$REPO_ROOT/docs/superpowers/sp5-docs-quality-audit.md"
STYLE_GUIDE="$CONTENT/contributing/reference/docs-style-guide.md"

fail=0
ok()   { printf '✓ %s\n' "$*"; }
err()  { printf '✗ %s\n' "$*" >&2; fail=1; }
note() { printf '%s\n' "$*"; }

# ── INV-1: rubric page + audit file both exist ─────────────────────────────
inv1_ok=1
[[ -f "$STYLE_GUIDE" ]] || { err "INV-1: style guide rubric missing: $STYLE_GUIDE"; inv1_ok=0; }
[[ -f "$AUDIT" ]]       || { err "INV-1: audit file missing: $AUDIT"; inv1_ok=0; }
((inv1_ok)) && ok "INV-1 files: style guide and audit file both present"

# ── INV-2: audit row count == content-page count ──────────────────────────
page_count=$(fd -e md -e mdx . "$CONTENT" | rg -v 'docs/index\.mdx|reference/events/' | wc -l | tr -d ' ')
row_count=$(rg -c '^\| \`' "$AUDIT" 2>/dev/null || echo 0)
if [[ "$page_count" -eq "$row_count" ]]; then
  ok "INV-2 row-parity: audit rows ($row_count) == content pages ($page_count)"
else
  err "INV-2 row-parity: MISMATCH — content pages=$page_count, audit rows=$row_count (add/remove rows to match)"
fi

# ── INV-4: each section index.mdx contains <CardGrid> ─────────────────────
sections=(guide operating extending contributing reference)
inv4_ok=1
for section in "${sections[@]}"; do
  idx="$CONTENT/$section/index.mdx"
  if [[ ! -f "$idx" ]]; then
    err "INV-4: section index missing: $idx"
    inv4_ok=0
  elif ! rg -q '<CardGrid>' "$idx"; then
    err "INV-4: <CardGrid> missing in $section/index.mdx"
    inv4_ok=0
  fi
done
((inv4_ok)) && ok "INV-4 CardGrid: all 5 section index files contain <CardGrid>"

# ── INV-7: social links in astro.config.mjs ───────────────────────────────
inv7_ok=1
if ! rg -q "icon: 'github'" "$CONFIG"; then
  err "INV-7: astro.config.mjs missing GitHub repo social link (icon: 'github')"
  inv7_ok=0
fi
if ! rg -q "holomush/holomush/discussions" "$CONFIG"; then
  err "INV-7: astro.config.mjs missing GitHub Discussions social link"
  inv7_ok=0
fi
((inv7_ok)) && ok "INV-7 social: GitHub repo and Discussions links present"

# ── ADVISORY: terminology proximity grep ──────────────────────────────────
note ""
note "── ADVISORY (non-gating) — terminology proximity grep ──────────────────"
note "   Candidate location/room juxtapositions for human triage — NOT a gate."
rg -n 'location[^.]{0,30}\broom\b' "$CONTENT" || true
rg -n '\broom\b[^.]{0,30}location' "$CONTENT" || true
note "── end advisory ─────────────────────────────────────────────────────────"
note ""

# ── Final result ──────────────────────────────────────────────────────────
if ((fail)); then
  printf '\n✗ FAIL: one or more SP5 quality invariants failed.\n'
  exit 1
fi
printf '\n✓ PASS: all SP5 quality invariants hold.\n'
exit 0
