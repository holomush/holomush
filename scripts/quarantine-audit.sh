#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Fails if any bead referenced by test/quarantine.yaml is closed (fix landed
# but the spec was never un-quarantined). Run locally / before `bd close`;
# requires `bd` on PATH with a reachable beads DB. INV-3.
set -euo pipefail

REG="test/quarantine.yaml"
[ -f "$REG" ] || { echo "no $REG; nothing to audit"; exit 0; }

if ! command -v bd >/dev/null 2>&1; then
  echo "quarantine:audit: bd not on PATH — skipping (run where bd is reachable)" >&2
  exit 0
fi

rc=0
# Process substitution (not a pipe) keeps the loop in the MAIN shell so that
# rc mutations survive — a `... | while read` loop runs in a subshell and
# `exit "$rc"` would always be 0 (the audit would be a silent no-op).
while read -r bead; do
  [ -n "$bead" ] || continue
  status=$(bd show "$bead" --json 2>/dev/null | jq -r '.[0].status // "unknown"')
  if [ "$status" = "closed" ]; then
    echo "QUARANTINE AUDIT: $bead is closed but still quarantined — un-quarantine it." >&2
    rc=1
  fi
done < <(grep -oE 'bead:[[:space:]]*holomush-[a-z0-9.]+' "$REG" | awk '{print $2}' | sort -u)
exit "$rc"
