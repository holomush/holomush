#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# release-notes-collect.sh <tag> — print a structured context block for the
# /release-notes workflow. Deterministic data-gathering only; the in-session
# model turns this block into narrative prose. No prose is written here.
set -euo pipefail

TAG="${1:?usage: release-notes-collect.sh <vX.Y.Z>}"

# Previous tag = the tag immediately before <tag> in version order.
PREV="$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | grep -A1 -xF "$TAG" | tail -n1)" || true
if [ -z "$PREV" ] || [ "$PREV" = "$TAG" ]; then
  echo "::error:: could not resolve a previous tag before $TAG" >&2
  exit 1
fi

echo "# Release context for $TAG"
echo
echo "Range: ${PREV}..${TAG}"
echo

# GoReleaser-equivalent exclude filters (.goreleaser.yaml:111-119 changelog
# block). Mirror it EXACTLY: ^docs: is anchored and does NOT match scoped
# docs(scope): commits — same as GoReleaser. Do not "improve" this to catch
# scoped prefixes, or the filtered section diverges from the mechanical list
# it cross-checks. The Merge patterns are unanchored exactly as GoReleaser
# leaves them (the collector also passes --no-merges, so they rarely matter).
EXCLUDE='^docs:|^test:|^chore:|Merge pull request|Merge branch'

mapfile -t SUBJECTS < <(git log --no-merges --pretty='%s' "${PREV}..${TAG}")

echo "## Filtered commits (mechanical set)"
echo
for s in "${SUBJECTS[@]}"; do
  printf '%s\n' "$s" | grep -Eqv "$EXCLUDE" && echo "- $s"
done
echo

echo "## Referenced beads"
echo
printf '%s\n' "${SUBJECTS[@]}" \
  | grep -Ev "$EXCLUDE" \
  | grep -oE 'holomush-[a-z0-9]+(\.[0-9]+)*' \
  | sort -u \
  | while read -r id; do
      # bd show is best-effort: degrade to the bare id if bd is unavailable
      # or fails (e.g. no .beads directory in the working dir). The || true
      # prevents set -e from exiting when bd/jq returns non-zero; the
      # ${line:-$id} fallback then ensures the bare id is always emitted.
      if command -v bd >/dev/null 2>&1; then
        line="$(bd show "$id" --json 2>/dev/null \
          | { command -v jq >/dev/null 2>&1 && jq -r '"\(.id) [\(.type)] \(.title) labels=\(.labels // [] | join(","))"' || cat; })" || true
        echo "- ${line:-$id}"
      else
        echo "- $id"
      fi
    # Outer guard: when the range has zero bead refs, `grep -oE` exits 1 and
    # `pipefail` would abort the script here; `|| true` keeps it going.
    done || true
echo

echo "## Coverage gaps (no bead ref)"
echo
for s in "${SUBJECTS[@]}"; do
  printf '%s\n' "$s" | grep -Eq "$EXCLUDE" && continue
  printf '%s\n' "$s" | grep -Eq 'holomush-[a-z0-9]+' || echo "- $s"
done
echo

echo "## Roadmap theme sections"
echo
echo "Consult docs/roadmap.md for theme:* sections; the model maps referenced"
echo "beads' theme labels to the relevant narrative headings."
