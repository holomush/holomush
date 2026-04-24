#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Ensure the DO Spaces bucket exists.
#
# Self-contained: mints a short-lived `fullaccess` Spaces key, uses it via the
# S3-compatible endpoint to HEAD-or-CREATE the bucket, then deletes the key.
# The key is deleted even on failure (trap EXIT).
#
# Why the temp-key dance: DO's /v2/spaces/keys rejects grants whose bucket
# does not exist ("403 invalid grant"). Buckets can only be created via the
# S3-compatible API, which needs a key. So we use a throwaway key here and
# let scripts/bootstrap-sandbox/spaces-key.sh mint the permanent, bucket-
# scoped `readwrite` key afterwards.
#
# Required env:
#   BUCKET_NAME  Bucket to ensure
#   REGION       DO region slug (e.g. sfo3)
#   DRY_RUN      "true" to echo and exit
#
# Outputs ($GITHUB_OUTPUT if set):
#   spaces_endpoint  Regional S3 endpoint (e.g. sfo3.digitaloceanspaces.com)
#
# Prerequisites: doctl (authenticated), aws CLI, python3.

set -euo pipefail

: "${BUCKET_NAME:?BUCKET_NAME must be set}"
: "${REGION:?REGION must be set}"
: "${DRY_RUN:?DRY_RUN must be set (\"true\" or \"false\")}"

ENDPOINT="https://${REGION}.digitaloceanspaces.com"

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

if [ "${DRY_RUN}" = "true" ]; then
  echo "::notice::[dry-run] Would ensure Spaces bucket ${BUCKET_NAME} exists in ${REGION}"
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    echo "spaces_endpoint=${REGION}.digitaloceanspaces.com" >> "${GITHUB_OUTPUT}"
  fi
  exit 0
fi

TEMP_KEY_NAME="holomush-sandbox-bootstrap-$$"
TEMP_ACCESS_KEY=""

cleanup_temp_key() {
  if [ -n "${TEMP_ACCESS_KEY}" ]; then
    echo "Deleting temporary bootstrap key ${TEMP_KEY_NAME}"
    doctl spaces keys delete "${TEMP_ACCESS_KEY}" --force \
      || echo "::warning::Failed to delete temporary Spaces key ${TEMP_KEY_NAME} (${TEMP_ACCESS_KEY}) — delete it manually"
  fi
}
trap cleanup_temp_key EXIT

TEMP_CREATED=$(doctl spaces keys create "${TEMP_KEY_NAME}" \
  --grants "bucket=;permission=fullaccess" \
  --output json)
TEMP_ACCESS_KEY=$(echo "${TEMP_CREATED}" | python3 -c \
  "import sys,json; d=json.load(sys.stdin); o=d[0] if isinstance(d,list) else d; print(o.get('access_key',''))")
TEMP_SECRET_KEY=$(echo "${TEMP_CREATED}" | python3 -c \
  "import sys,json; d=json.load(sys.stdin); o=d[0] if isinstance(d,list) else d; print(o.get('secret_key',''))")

if [ -z "${TEMP_ACCESS_KEY}" ] || [ -z "${TEMP_SECRET_KEY}" ]; then
  REDACTED=$(echo "${TEMP_CREATED}" | redact_secret_key)
  echo "::error::Failed to create temporary Spaces key ${TEMP_KEY_NAME}"
  echo "Response body (redacted): ${REDACTED}"
  exit 1
fi
echo "::add-mask::${TEMP_SECRET_KEY}"

aws configure set aws_access_key_id "${TEMP_ACCESS_KEY}" --profile dospaces-boot
aws configure set aws_secret_access_key "${TEMP_SECRET_KEY}" --profile dospaces-boot

if aws --profile dospaces-boot --endpoint-url "${ENDPOINT}" \
     s3api head-bucket --bucket "${BUCKET_NAME}" 2>/dev/null; then
  echo "::notice::Reusing existing Spaces bucket: ${BUCKET_NAME}"
else
  aws --profile dospaces-boot --endpoint-url "${ENDPOINT}" \
    s3api create-bucket --bucket "${BUCKET_NAME}"
  echo "::notice::Created Spaces bucket: ${BUCKET_NAME}"
fi

if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "spaces_endpoint=${REGION}.digitaloceanspaces.com" >> "${GITHUB_OUTPUT}"
fi
