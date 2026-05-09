#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# INV-B-BEAD: ensure jxo8.5's bead description matches sub-epic B's
# narrowed scope before this PR is pushed.

set -euo pipefail

BEAD_ID="holomush-jxo8.5"
DESCRIPTION_FINGERPRINT="capability + storage + facade + amendments"
STALE_FRAGMENT="OperatorAuthProvider check sequence (creds → TOTP → RoleAdmin → crypto.operator)"

if ! command -v bd >/dev/null 2>&1; then
    echo "bd CLI not found; skipping bead-description check (best-effort)"
    exit 0
fi
if ! command -v jq >/dev/null 2>&1; then
    echo "ERROR: jq required for bead-description check" >&2
    exit 1
fi

# bd show returns a JSON array; the bead is at index 0.
DESC=$(bd show "$BEAD_ID" --json 2>/dev/null | jq -r '.[0].description // ""')
if [ -z "$DESC" ]; then
    echo "ERROR: could not read description for $BEAD_ID" >&2
    exit 1
fi

if ! grep -qF "$DESCRIPTION_FINGERPRINT" <<<"$DESC"; then
    echo "ERROR: $BEAD_ID description must contain fingerprint:" >&2
    echo "  '$DESCRIPTION_FINGERPRINT'" >&2
    echo "Update via: bd update $BEAD_ID --body-file -  (see plan Task 14)" >&2
    exit 1
fi

if grep -qF "$STALE_FRAGMENT" <<<"$DESC"; then
    echo "ERROR: $BEAD_ID description still contains stale text:" >&2
    echo "  '$STALE_FRAGMENT'" >&2
    echo "Sub-epic B does not own this; that's sub-epic D's scope." >&2
    exit 1
fi

# Sanity: jxo8.5 already blocks jxo8.6 by epic-creation time. Verify the
# edge survives, in case someone removes it.
if ! bd show "$BEAD_ID" --json 2>/dev/null | \
        jq -e '.[0].dependents[]? | select(.id == "holomush-jxo8.6" and .dependency_type == "blocks")' \
        >/dev/null; then
    echo "ERROR: $BEAD_ID must block holomush-jxo8.6 (sub-epic D consumes B's facade)" >&2
    exit 1
fi

echo "OK: $BEAD_ID description and blocks-edge match sub-epic B's narrowed scope"
