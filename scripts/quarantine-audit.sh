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
# Process substitution (not a pipe) keeps the loop in the MAIN shell so that
# rc mutations survive — a `... | while read` loop runs in a subshell and
# `exit "$rc"` would always be 0 (the audit would be a silent no-op).
while read -r issue; do
  [ -n "$issue" ] || continue
  state=$(gh issue view "$issue" -R holomush/holomush --json state --jq .state 2>/dev/null || echo "UNKNOWN")
  if [ "$state" = "CLOSED" ]; then
    echo "QUARANTINE AUDIT: issue #$issue is closed but still quarantined — un-quarantine it." >&2
    rc=1
  fi
done < <(grep -oE 'issue:[[:space:]]*[0-9]+' "$REG" | awk '{print $2}' | sort -u)
exit "$rc"
