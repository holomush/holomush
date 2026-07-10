#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Fails if any GitHub issue referenced by test/quarantine.yaml is closed (fix
# landed but the spec was never un-quarantined). Run locally / before closing
# a quarantine-tracking issue; requires `gh` on PATH with repo access. INV-3.
set -euo pipefail

REG="test/quarantine.yaml"
[ -f "$REG" ] || { echo "no $REG; nothing to audit"; exit 0; }

if ! command -v gh >/dev/null 2>&1; then
  echo "quarantine:audit: gh not on PATH — skipping (run where gh is reachable)" >&2
  exit 0
fi

rc=0

# Every entry must carry an `issue:` field — a row without one would escape
# the audit forever. Compare entry count (id: lines) to issue: count.
ids=$(grep -cE '^[[:space:]]*-[[:space:]]*id:' "$REG" || true)
issues=$(grep -cE '^[[:space:]]*issue:[[:space:]]*[0-9]+' "$REG" || true)
if [ "$ids" != "$issues" ]; then
  echo "QUARANTINE AUDIT: $ids entries but $issues issue: fields — every row MUST cite a GitHub issue." >&2
  rc=1
fi

# Process substitution (not a pipe) keeps the loop in the MAIN shell so that
# rc mutations survive — a `... | while read` loop runs in a subshell and
# `exit "$rc"` would always be 0 (the audit would be a silent no-op).
while read -r issue; do
  [ -n "$issue" ] || continue
  if ! state=$(gh issue view "$issue" -R holomush/holomush --json state --jq .state 2>/dev/null); then
    echo "QUARANTINE AUDIT: cannot resolve issue #$issue (bad number or gh auth)." >&2
    rc=1
  elif [ "$state" = "CLOSED" ]; then
    echo "QUARANTINE AUDIT: issue #$issue is closed but still quarantined — un-quarantine it." >&2
    rc=1
  fi
done < <(grep -oE 'issue:[[:space:]]*[0-9]+' "$REG" | awk '{print $2}' | sort -u)
exit "$rc"
