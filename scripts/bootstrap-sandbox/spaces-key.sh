#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Resolve or create the permanent DO Spaces access key for the sandbox,
# scoped `readwrite` to BUCKET_NAME.
#
# Precondition: BUCKET_NAME must already exist. DO's /v2/spaces/keys rejects
# grants that reference a non-existent bucket with "403 invalid grant". Run
# scripts/bootstrap-sandbox/spaces-bucket.sh first.
#
# Idempotent: no-op when (a) both GH Secrets DO_SPACES_ACCESS_KEY and
# DO_SPACES_SECRET_KEY are populated AND (b) the existing DO-side
# `holomush-sandbox` key's grant matches BUCKET_NAME with `readwrite`.
# Otherwise rotate (delete any stale key by name, create fresh) and write
# the new access/secret to GH Secrets.
#
# Required env:
#   BUCKET_NAME  Bucket to scope the grant to (must already exist)
#   GH_TOKEN     PAT with repo and secrets:write scopes
#   DRY_RUN      "true" to echo and exit
#
# Optional env (local testing):
#   SKIP_GH_SECRET_WRITE=1   Skip `gh secret set` calls
#
# Outputs ($GITHUB_OUTPUT if set):
#   access_key / secret_key
#
# Prerequisites: doctl (authenticated), gh (authenticated), python3.

set -euo pipefail

: "${BUCKET_NAME:?BUCKET_NAME must be set}"
: "${GH_TOKEN:?GH_TOKEN must be set}"
: "${DRY_RUN:?DRY_RUN must be set (\"true\" or \"false\")}"

KEY_NAME="holomush-sandbox"

if [ "${DRY_RUN}" = "true" ]; then
  echo "DRY RUN: would resolve or create Spaces access key '${KEY_NAME}'"
  exit 0
fi

# Strip `secret_key` from a DO Spaces JSON response before logging. Used on
# error paths where the unparsed body would otherwise leak a credential.
redact_secret_key() {
  python3 -c '
import sys, json
try:
    d = json.load(sys.stdin)
    items = d if isinstance(d, list) else [d]
    for it in items:
        if isinstance(it, dict) and "secret_key" in it:
            it["secret_key"] = "***REDACTED***"
    print(json.dumps(d))
except Exception:
    print("<unparseable response>")
'
}

# Fail closed on `gh secret list` errors — a network/auth failure must not be
# silently interpreted as "secrets missing" and push us into rotation.
# The `|| true` guards ONLY grep's 0-match exit (1), not the gh call itself.
SECRET_LIST=$(gh secret list --app actions)
HAS_ACCESS_KEY=$(echo "${SECRET_LIST}" | grep -c '^DO_SPACES_ACCESS_KEY' || true)
HAS_SECRET_KEY=$(echo "${SECRET_LIST}" | grep -c '^DO_SPACES_SECRET_KEY' || true)

# Look up the server-side key and determine whether its grant still matches
# the requested BUCKET_NAME. The operator can rerun the workflow with a
# different bucket_name input; in that case existing DO_SPACES_* secrets
# would be stale (scoped to the old bucket) and must be rotated.
EXISTING_JSON=$(doctl spaces keys list --output json)
GRANT_STATUS=$(BUCKET_NAME="${BUCKET_NAME}" KEY_NAME="${KEY_NAME}" \
  python3 -c '
import os, sys, json
d = json.load(sys.stdin)
keys = [k for k in d if k.get("name") == os.environ["KEY_NAME"]]
if not keys:
    print("absent")
else:
    grants = keys[0].get("grants", []) or []
    ok = any(
        g.get("bucket") == os.environ["BUCKET_NAME"]
        and g.get("permission") == "readwrite"
        for g in grants
    )
    print("match" if ok else "mismatch")
' <<<"${EXISTING_JSON}")

if [ "${HAS_ACCESS_KEY}" -gt "0" ] \
   && [ "${HAS_SECRET_KEY}" -gt "0" ] \
   && [ "${GRANT_STATUS}" = "match" ]; then
  echo "::notice::DO_SPACES_ACCESS_KEY/SECRET_KEY already set and scoped to ${BUCKET_NAME} — skipping"
  exit 0
fi

# Rotate: delete any pre-existing key by name (doctl's secret_key is never
# returned on list, so a pre-existing key is useless to us — we must mint a
# fresh one even when the grant already matches).
EXISTING_ACCESS_KEY=$(KEY_NAME="${KEY_NAME}" \
  python3 -c '
import os, sys, json
d = json.load(sys.stdin)
keys = [k for k in d if k.get("name") == os.environ["KEY_NAME"]]
print(keys[0].get("access_key", "") if keys else "")
' <<<"${EXISTING_JSON}")

if [ -n "${EXISTING_ACCESS_KEY}" ]; then
  case "${GRANT_STATUS}" in
    match)    echo "Existing key has correct grant but GH Secrets are missing/stale — rotating" ;;
    mismatch) echo "Existing key is scoped to a different bucket — rotating" ;;
  esac
  doctl spaces keys delete "${EXISTING_ACCESS_KEY}" --force
fi

CREATED=$(doctl spaces keys create "${KEY_NAME}" \
  --grants "bucket=${BUCKET_NAME};permission=readwrite" \
  --output json)
ACCESS_KEY=$(echo "${CREATED}" | python3 -c \
  "import sys,json; d=json.load(sys.stdin); o=d[0] if isinstance(d,list) else d; print(o.get('access_key',''))")
SECRET_KEY=$(echo "${CREATED}" | python3 -c \
  "import sys,json; d=json.load(sys.stdin); o=d[0] if isinstance(d,list) else d; print(o.get('secret_key',''))")

if [ -z "${ACCESS_KEY}" ] || [ -z "${SECRET_KEY}" ]; then
  REDACTED=$(echo "${CREATED}" | redact_secret_key)
  echo "::error::doctl spaces keys create returned empty access/secret key"
  echo "Response body (redacted): ${REDACTED}"
  exit 1
fi

echo "::add-mask::${SECRET_KEY}"

if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "access_key=${ACCESS_KEY}" >> "${GITHUB_OUTPUT}"
  echo "secret_key=${SECRET_KEY}" >> "${GITHUB_OUTPUT}"
fi

if [ -z "${SKIP_GH_SECRET_WRITE:-}" ]; then
  echo "${ACCESS_KEY}" | gh secret set DO_SPACES_ACCESS_KEY
  echo "${SECRET_KEY}" | gh secret set DO_SPACES_SECRET_KEY
  echo "::notice::Created Spaces access key '${KEY_NAME}' and stored in GH Secrets"
else
  echo "SKIP_GH_SECRET_WRITE set — not writing DO_SPACES_ACCESS_KEY/SECRET_KEY to GH Secrets"
fi
