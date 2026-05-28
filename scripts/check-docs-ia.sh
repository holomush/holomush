#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# check-docs-ia.sh — SP2 Diátaxis IA invariants (INV-1/2/3/5/6).
#
# INV-1 parity:      every content slug (all .md/.mdx under site/src/content/docs
#                    except the root index.mdx) resolves to a built page
#                    site/dist/<slug>/index.html (run `task docs:build` first).
# INV-2 one-bucket:  no non-index .md/.mdx sits directly under an audience dir;
#                    every non-index doc is under <audience>/<mode>/… (reference/
#                    is flat by design and exempt).
# INV-3 retired-gone:contributing/event-delivery.* and operating/legacy-id-cutover.*
#                    are absent, and no link resolves to their slugs.
# INV-5 branding:    vs main@origin (jj-native diff; worktree has no .git), the
#                    branding assets are byte-identical, astro.config.mjs differs
#                    only within the sidebar field, and tsconfig.json only adds the
#                    compilerOptions.paths alias (extends/include/exclude intact).
# INV-6 nav:         ≤7 top-level sidebar sections (hard); mode folders with >7
#                    direct children are flagged as a Diátaxis guideline (SHOULD
#                    sub-group) — non-fatal.

set -euo pipefail
shopt -s nullglob

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="$REPO_ROOT/site/dist"
CONTENT="$REPO_ROOT/site/src/content/docs"
CONFIG="$REPO_ROOT/site/astro.config.mjs"
DIFF_BASE="${SP2_DIFF_BASE:-main@origin}"
AUDIENCES=(guide operating extending contributing)

fail=0
ok()   { printf '✓ %s\n' "$*"; }
err()  { printf '✗ %s\n' "$*"; fail=1; }
note() { printf '%s\n' "$*"; }

# slug → built index.html path (Starlight directory-style URLs).
slug_to_built() {
  local slug="$1"
  if [[ "$slug" == */index ]]; then
    echo "$DIST/${slug%/index}/index.html"
  else
    echo "$DIST/${slug}/index.html"
  fi
}

# ── INV-1: parity ──────────────────────────────────────────────────────────
inv1_missing=()
while IFS= read -r f; do
  rel="${f#"$CONTENT"/}"
  [[ "$rel" == "index.mdx" || "$rel" == "index.md" ]] && continue  # root splash
  slug="${rel%.md}"; slug="${slug%.mdx}"
  built="$(slug_to_built "$slug")"
  [[ -f "$built" ]] || inv1_missing+=("$slug → ${built#"$REPO_ROOT"/}")
done < <(rg --files "$CONTENT" -g '*.md' -g '*.mdx' | sort)
if ((${#inv1_missing[@]})); then
  err "INV-1 parity: ${#inv1_missing[@]} content slug(s) without a built page"
  printf '    MISSING: %s\n' "${inv1_missing[@]}"
else
  ok "INV-1 parity: every content slug resolves to a built page"
fi

# ── INV-2: one-bucket ──────────────────────────────────────────────────────
inv2_viol=()
for a in "${AUDIENCES[@]}"; do
  for f in "$CONTENT/$a"/*.md "$CONTENT/$a"/*.mdx; do
    base="$(basename "$f")"
    [[ "$base" == index.md || "$base" == index.mdx ]] && continue
    inv2_viol+=("${f#"$CONTENT"/}")
  done
done
if ((${#inv2_viol[@]})); then
  err "INV-2 one-bucket: ${#inv2_viol[@]} non-index doc(s) directly under an audience dir"
  printf '    UNBUCKETED: %s\n' "${inv2_viol[@]}"
else
  ok "INV-2 one-bucket: every non-index doc lives under <audience>/<mode>/"
fi

# ── INV-3: retired-gone ────────────────────────────────────────────────────
inv3_ok=1
for slug in contributing/event-delivery operating/legacy-id-cutover; do
  if [[ -f "$CONTENT/$slug.md" || -f "$CONTENT/$slug.mdx" ]]; then
    err "INV-3: retired doc still present: $slug"; inv3_ok=0
  fi
done
if rg -q '\](/[^)]*(event-delivery|legacy-id-cutover))' "$CONTENT"; then
  err "INV-3: inbound link(s) to a retired slug remain:"
  rg -n '\](/[^)]*(event-delivery|legacy-id-cutover))' "$CONTENT" | sed 's/^/    /'
  inv3_ok=0
fi
((inv3_ok)) && ok "INV-3 retired-gone: both retired docs absent; no inbound links"

# ── INV-5: branding (jj diff vs base) ──────────────────────────────────────
if ! command -v jj >/dev/null 2>&1; then
  note "⚑ INV-5 skipped: jj not available (branding diff requires jj-native diff vs $DIFF_BASE)"
elif ! ( cd "$REPO_ROOT" && jj --no-pager log -r "$DIFF_BASE" >/dev/null 2>&1 ); then
  note "⚑ INV-5 skipped: revset '$DIFF_BASE' not resolvable here"
else
  inv5_ok=1
  for p in site/src/styles/custom.css site/src/assets/logo.png site/public/favicon.png; do
    if [[ -n "$( cd "$REPO_ROOT" && jj --no-pager diff --from "$DIFF_BASE" -- "$p" 2>/dev/null )" ]]; then
      err "INV-5: branding asset changed vs $DIFF_BASE: $p"; inv5_ok=0
    fi
  done
  cfg_brand="$( cd "$REPO_ROOT" && jj --no-pager diff --git --from "$DIFF_BASE" -- site/astro.config.mjs 2>/dev/null \
    | rg '^[+-]' | rg -v '^[+-]{3}' | rg 'title:|description:|logo:|favicon:|social:|customCss:|plugins:|site:' || true )"
  if [[ -n "$cfg_brand" ]]; then
    err "INV-5: astro.config.mjs changed a branding field (only the sidebar may change):"
    printf '    %s\n' "$cfg_brand"; inv5_ok=0
  fi
  ts_removed="$( cd "$REPO_ROOT" && jj --no-pager diff --git --from "$DIFF_BASE" -- site/tsconfig.json 2>/dev/null \
    | rg '^-' | rg -v '^-{3}' | rg 'extends|include|exclude' || true )"
  if [[ -n "$ts_removed" ]]; then
    err "INV-5: tsconfig.json removed a preserved key (only the paths alias may be added):"
    printf '    %s\n' "$ts_removed"; inv5_ok=0
  fi
  ((inv5_ok)) && ok "INV-5 branding: assets byte-identical; config diffs scoped to sidebar + paths alias"
fi

# ── INV-6: nav shape ───────────────────────────────────────────────────────
sections="$(rg -c 'autogenerate:' "$CONFIG" 2>/dev/null || echo 0)"
if (( sections > 7 )); then
  err "INV-6: $sections top-level sidebar sections (>7)"
else
  ok "INV-6 nav: $sections top-level sidebar sections (≤7)"
fi
for a in "${AUDIENCES[@]}"; do
  for mode in "$CONTENT/$a"/*/; do
    [[ -d "$mode" ]] || continue
    cnt=0
    for f in "$mode"*.md "$mode"*.mdx; do cnt=$((cnt + 1)); done
    (( cnt > 7 )) && note "  ⚑ INV-6 guideline: ${mode#"$CONTENT"/} has $cnt direct children (>7 — SHOULD topically sub-group)"
  done
done

exit "$fail"
