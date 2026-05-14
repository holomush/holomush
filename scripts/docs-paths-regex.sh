#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Compile DOCS_ONLY_PATHS globs (read from Taskfile.yaml) into one anchored
# extended-regex string for grep -vE. Spec:
# docs/superpowers/specs/2026-05-14-pr-prep-docs-fast-lane-design.md §4.4.2

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${REPO_ROOT:-$(cd "$SCRIPT_DIR/.." && pwd)}"
TASKFILE="$REPO_ROOT/Taskfile.yaml"

[ -f "$TASKFILE" ] || { echo "ERROR: $TASKFILE not found" >&2; exit 1; }
command -v yq >/dev/null 2>&1 || { echo "ERROR: yq not installed" >&2; exit 1; }

# yq -e exits non-zero if the path is null/missing — but defensive coders
# don't trust a single signal. Also reject the literal string "null"
# (which yq emits with exit 0 when the key resolves to YAML null).
GLOBS="$(yq -e '.vars.DOCS_ONLY_PATHS' "$TASKFILE" 2>/dev/null)" || {
  echo "ERROR: vars.DOCS_ONLY_PATHS not found in $TASKFILE (yq -e failed)" >&2
  exit 1
}
if [ -z "$GLOBS" ] || [ "$GLOBS" = "null" ]; then
  echo "ERROR: vars.DOCS_ONLY_PATHS is empty or null in $TASKFILE" >&2
  exit 1
fi

# Compile each glob into a regex alternative.
ALTS=""
while IFS= read -r glob; do
  [ -n "$glob" ] || continue
  case "$glob" in
    *'**'*'**'*)
      echo "ERROR: glob '$glob' has multiple '**'; not supported" >&2
      exit 1
      ;;
    '**/*.md')
      alt='.*\.md'
      ;;
    *'/**')
      # foo/** -> foo/.*
      prefix="${glob%/**}"
      # escape literal dots
      prefix_re="${prefix//./\\.}"
      alt="${prefix_re}/.*"
      ;;
    *'**'*)
      echo "ERROR: glob '$glob' has unsupported '**' position" >&2
      exit 1
      ;;
    *)
      # Literal path (e.g., LICENSE, LICENSE_HEADER). Escape dots.
      alt="${glob//./\\.}"
      ;;
  esac
  if [ -z "$ALTS" ]; then
    ALTS="$alt"
  else
    ALTS="$ALTS|$alt"
  fi
done <<< "$GLOBS"

printf '^(%s)$\n' "$ALTS"
