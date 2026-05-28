#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# check-docs-parity.sh — verify nav parity (INV-1) and migration completeness (INV-5)
#
# Assumes `bunx astro build` has already run and site/dist exists.
# Exits nonzero listing any missing built pages.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="$REPO_ROOT/site/dist"
CONTENT_DOCS="$REPO_ROOT/site/src/content/docs"
NAV_FIXTURE="$REPO_ROOT/scripts/tests/fixtures/zensical-nav.txt"

fail=0
nav_missing=()
src_missing=()

# ---------------------------------------------------------------------------
# slug_to_built_path <slug>
#   Maps a content slug to its expected built index.html path under site/dist.
#
# Rules (Starlight directory-style URLs):
#   - slug "index"         → dist/index.html
#   - slug "guide/index"   → dist/guide/index.html  (section root)
#   - slug "guide/foo"     → dist/guide/foo/index.html
# ---------------------------------------------------------------------------
slug_to_built_path() {
  local slug="$1"
  # Strip a trailing /index component (section root)
  if [[ "$slug" == "index" ]]; then
    echo "$DIST/index.html"
  elif [[ "$slug" == */index ]]; then
    local dir="${slug%/index}"
    echo "$DIST/${dir}/index.html"
  else
    echo "$DIST/${slug}/index.html"
  fi
}

# ---------------------------------------------------------------------------
# INV-1: nav parity
#   Every slug in zensical-nav.txt must have a built page.
# ---------------------------------------------------------------------------
nav_count=0
while IFS= read -r slug; do
  [[ -z "$slug" ]] && continue
  nav_count=$((nav_count + 1))
  built="$(slug_to_built_path "$slug")"
  if [[ ! -f "$built" ]]; then
    nav_missing+=("$slug → $built")
    fail=1
  fi
done < "$NAV_FIXTURE"

# ---------------------------------------------------------------------------
# INV-5: migration completeness
#   Every source .md / .mdx page must have a corresponding built page.
# ---------------------------------------------------------------------------
src_count=0
while IFS= read -r src_file; do
  [[ -z "$src_file" ]] && continue
  src_count=$((src_count + 1))

  # Derive slug: strip content-docs prefix and extension
  rel="${src_file#"$CONTENT_DOCS/"}"
  slug="${rel%.md}"
  slug="${slug%.mdx}"

  built="$(slug_to_built_path "$slug")"
  if [[ ! -f "$built" ]]; then
    src_missing+=("$src_file → $built")
    fail=1
  fi
done < <(
  rg --files "$CONTENT_DOCS" | rg '\.(md|mdx)$' | sort
)

# ---------------------------------------------------------------------------
# Report
# ---------------------------------------------------------------------------
nav_ok=$((nav_count - ${#nav_missing[@]}))
src_ok=$((src_count - ${#src_missing[@]}))

if [[ ${#nav_missing[@]} -gt 0 ]]; then
  echo "✗ nav parity: ${nav_ok}/${nav_count} (missing ${#nav_missing[@]})"
  for m in "${nav_missing[@]}"; do
    echo "  MISSING: $m"
  done
else
  echo "✓ nav parity: ${nav_ok}/${nav_count}"
fi

if [[ ${#src_missing[@]} -gt 0 ]]; then
  echo "✗ page migration: ${src_ok}/${src_count} (missing ${#src_missing[@]})"
  for m in "${src_missing[@]}"; do
    echo "  MISSING: $m"
  done
else
  echo "✓ page migration: ${src_ok}/${src_count}"
fi

exit $fail
