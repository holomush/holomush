#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Verify DOCS_ONLY_PATHS is byte-identical across Taskfile.yaml,
# ci.yaml's paths-ignore (push + pull_request), and ci-docs-skip.yaml's
# paths (push + pull_request). Five extraction points across three files.
# Spec: docs/superpowers/specs/2026-05-14-pr-prep-docs-fast-lane-design.md §4.4.3

set -euo pipefail

# REPO_ROOT may be overridden by tests; default to script's parent.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${REPO_ROOT:-$(cd "$SCRIPT_DIR/.." && pwd)}"
TASKFILE="$REPO_ROOT/Taskfile.yaml"
CI="$REPO_ROOT/.github/workflows/ci.yaml"
CI_SKIP="$REPO_ROOT/.github/workflows/ci-docs-skip.yaml"

command -v yq >/dev/null 2>&1 || { echo "ERROR: yq not installed" >&2; exit 1; }

normalize() {
  # Trim trailing whitespace and blank lines; preserve order.
  awk 'NF { sub(/[[:space:]]+$/, ""); print }'
}

# Extract canonical. Hardened against the yq-null trap (returns literal
# "null" with exit 0 when key is missing).
canonical_raw="$(yq -e '.vars.DOCS_ONLY_PATHS' "$TASKFILE" 2>/dev/null)" || {
  echo "ERROR: vars.DOCS_ONLY_PATHS not found in $TASKFILE (yq -e failed)" >&2
  exit 1
}
if [ -z "$canonical_raw" ] || [ "$canonical_raw" = "null" ]; then
  echo "ERROR: vars.DOCS_ONLY_PATHS is empty or null in $TASKFILE" >&2
  exit 1
fi
canonical="$(printf '%s\n' "$canonical_raw" | normalize)"

ci_push="$(yq '.on.push.paths-ignore[]' "$CI" 2>/dev/null | normalize || true)"
ci_pr="$(yq '.on.pull_request.paths-ignore[]' "$CI" 2>/dev/null | normalize || true)"
skip_push="$(yq '.on.push.paths[]' "$CI_SKIP" 2>/dev/null | normalize || true)"
skip_pr="$(yq '.on.pull_request.paths[]' "$CI_SKIP" 2>/dev/null | normalize || true)"

mismatches=0
check() {
  local name="$1" actual="$2"
  if [ "$actual" != "$canonical" ]; then
    echo "ERROR: docs-paths drift in $name" >&2
    diff <(printf '%s' "$canonical") <(printf '%s' "$actual") >&2 || true
    mismatches=$((mismatches + 1))
  fi
}

check "ci.yaml on.push.paths-ignore" "$ci_push"
check "ci.yaml on.pull_request.paths-ignore" "$ci_pr"
check "ci-docs-skip.yaml on.push.paths" "$skip_push"
check "ci-docs-skip.yaml on.pull_request.paths" "$skip_pr"

if [ "$mismatches" -ne 0 ]; then
  exit 1
fi

echo "docs-paths in sync across Taskfile + ci.yaml + ci-docs-skip.yaml."
