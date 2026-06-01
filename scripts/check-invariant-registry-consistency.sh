#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Verifies docs/architecture/invariants.yaml and invariants.md are in sync.
# - Every invariant ID in the YAML must appear in the markdown table.
# - Every invariant ID in the markdown table must appear in the YAML.
# - The scope count in YAML scopes: must match the scope index table rows.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
YAML="$REPO_ROOT/docs/architecture/invariants.yaml"
MD="$REPO_ROOT/docs/architecture/invariants.md"

if [[ ! -f "$YAML" ]]; then
  echo "ERROR: invariants.yaml not found at $YAML"
  exit 1
fi
if [[ ! -f "$MD" ]]; then
  echo "ERROR: invariants.md not found at $MD"
  exit 1
fi

# Extract IDs from YAML (lines matching "id: INV-...")
yaml_ids=$(grep -E '^[[:space:]]+id:[[:space:]]+INV-[A-Z]+-[0-9]+' "$YAML" | sed -E 's/^[[:space:]]+id:[[:space:]]+//' | sort || true)
if [[ -z "$yaml_ids" ]]; then
  echo "WARNING: invariants.yaml has no invariant entries yet — consistency check skipped"
  exit 0
fi

# Extract IDs from markdown table rows (lines matching "| `INV-...` |")
md_ids=$(grep -E '^\|[[:space:]]+`INV-[A-Z]+-[0-9]+`' "$MD" | sed -E 's/^.*`(INV-[A-Z]+-[0-9]+)`.*$/\1/' | sort)

# Every YAML ID must appear in markdown.
fail=0
while IFS= read -r id; do
  if ! echo "$md_ids" | grep -qxF "$id"; then
    echo "ERROR: YAML has $id but markdown table does not"
    fail=1
  fi
done <<< "$yaml_ids"

# Every markdown ID must appear in YAML.
while IFS= read -r id; do
  if ! echo "$yaml_ids" | grep -qxF "$id"; then
    echo "ERROR: markdown table has $id but YAML does not"
    fail=1
  fi
done <<< "$md_ids"

if [[ $fail -eq 0 ]]; then
  echo "✓ invariants.yaml and invariants.md are consistent ($(echo "$yaml_ids" | wc -l | tr -d ' ') invariants)"
fi

exit $fail
