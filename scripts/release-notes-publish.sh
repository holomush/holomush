#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# release-notes-publish.sh --tag <vX.Y.Z> --narrative-file <path>
#
# Publishes the narrative ABOVE the existing GoReleaser release body. Because
# `gh release edit --notes-file` REPLACES the body, this script fetches the
# current body first and combines (narrative + separator + existing). It MUST
# NOT publish a narrative-only body — that would drop the mechanical commit
# list and violate jfb9x INV-7.
set -euo pipefail

TAG=""; NARR=""
while [ $# -gt 0 ]; do
  case "$1" in
    --tag) TAG="${2:?}"; shift 2 ;;
    --narrative-file) NARR="${2:?}"; shift 2 ;;
    *) echo "::error:: unknown arg: $1" >&2; exit 2 ;;
  esac
done
[ -n "$TAG" ] || { echo "::error:: --tag is required" >&2; exit 2; }
[ -n "$NARR" ] || { echo "::error:: --narrative-file is required" >&2; exit 2; }
if [ ! -s "$NARR" ]; then
  echo "::error:: narrative file is empty; refusing to publish (would drop the GoReleaser list)" >&2
  exit 1
fi

EXISTING="$(gh release view "$TAG" --json body -q .body 2>/dev/null || true)"
if [ -z "$EXISTING" ]; then
  # Fail closed: an empty body means GoReleaser hasn't run (or the wrong tag was
  # given). Publishing narrative-only would silently drop the mechanical list and
  # violate INV-7. Surface the anomaly instead of papering over it.
  echo "::error:: existing release body for $TAG is empty — refusing to publish narrative-only (GoReleaser notes missing; INV-7). Run GoReleaser first or check the tag." >&2
  exit 1
fi

COMBINED="$(mktemp)"
trap 'rm -f "$COMBINED"' EXIT
cat "$NARR" > "$COMBINED"
printf '\n\n---\n\n' >> "$COMBINED"
printf '%s\n' "$EXISTING" >> "$COMBINED"

# --notes-file (never --notes inline): the combined file is the whole body.
gh release edit "$TAG" --notes-file "$COMBINED"
echo "Published combined release notes for $TAG" >&2
