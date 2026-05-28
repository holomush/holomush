#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# INV-4 (open half): fail if any doc-ratchet.yaml cited bead is closed.
# Run locally / pre-bd-close; NOT in CI (bd not guaranteed there).
set -euo pipefail
reg="api/proto/doc-ratchet.yaml"
beads=$(rg -o 'holomush-[a-z0-9.]+' "$reg" | sort -u)
rc=0
for b in $beads; do
  # bd show --json returns an ARRAY; index [0] (matches scripts/quarantine-audit.sh:24).
  # `bd show` exits non-zero for an unknown bead; under `set -e`+pipefail that would
  # abort the loop, so fall back to MISSING and keep auditing the rest.
  status=$(bd show "$b" --json 2>/dev/null | jq -r '.[0].status // "MISSING"') || status="MISSING"
  if [ "$status" = "closed" ] || [ "$status" = "MISSING" ]; then
    echo "ERROR: doc-ratchet bead $b is $status (must be open)" >&2
    rc=1
  fi
done
[ "$rc" -eq 0 ] && echo "✓ all doc-ratchet beads open"
exit "$rc"
