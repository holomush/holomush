#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Nightly Postgres backup via Kopia.
#
# Flow: pg_dump → kopia snapshot create --stdin → encrypted+deduped+compressed
#       upload to S3-compatible bucket. Retention policies (keep-daily,
#       keep-weekly, keep-monthly) applied per snapshot source.
#
# Invoked by supercronic at 03:00 UTC. Run manually via:
#   docker compose exec backup /usr/local/bin/backup.sh [--tag=pre-deploy:vX]

set -euo pipefail

: "${POSTGRES_HOST:?must be set}"
: "${POSTGRES_USER:?must be set}"
: "${POSTGRES_DB:?must be set}"
: "${PGPASSWORD:?must be set}"
: "${BACKUP_S3_BUCKET:?must be set}"
: "${KOPIA_PASSWORD:?must be set}"
: "${BACKUP_S3_ACCESS_KEY:?must be set}"
: "${BACKUP_S3_SECRET_KEY:?must be set}"

# Parse optional --tag=<key:value> argument (used for pre-deploy pins).
TAG=""
for arg in "$@"; do
  case "${arg}" in
    --tag=*) TAG="${arg#--tag=}" ;;
    *) echo "unknown arg: ${arg}" >&2; exit 2 ;;
  esac
done

export KOPIA_PASSWORD
export AWS_ACCESS_KEY_ID="${BACKUP_S3_ACCESS_KEY}"
export AWS_SECRET_ACCESS_KEY="${BACKUP_S3_SECRET_KEY}"

# Connect to the repo if not already connected. `kopia repository status`
# returns non-zero if not connected; in that case, connect (the repo is
# created once during cloud-init; see operations runbook).
if ! kopia repository status >/dev/null 2>&1; then
  echo "[backup] connecting to existing Kopia repository"
  endpoint_args=""
  if [ -n "${BACKUP_S3_ENDPOINT:-}" ]; then
    endpoint_args="--endpoint=${BACKUP_S3_ENDPOINT}"
  fi
  # shellcheck disable=SC2086
  kopia repository connect s3 \
    --bucket="${BACKUP_S3_BUCKET}" \
    ${endpoint_args} \
    --access-key="${BACKUP_S3_ACCESS_KEY}" \
    --secret-access-key="${BACKUP_S3_SECRET_KEY}"
fi

source_name="holomush-${POSTGRES_DB}"
echo "[backup] $(date -u +%FT%TZ) streaming pg_dump → kopia snapshot (source=${source_name})"

tag_args=""
pin_args=""
if [ -n "${TAG}" ]; then
  tag_args="--tags=${TAG}"
  # Pre-deploy and manual-pin snapshots are pinned so the retention policy
  # never expires them. The runbooks use manual-pin:* for pre-restore and
  # long-lived operator snapshots.
  case "${TAG}" in
    pre-deploy:* | manual-pin:*) pin_args="--pin=${TAG}" ;;
  esac
fi

# shellcheck disable=SC2086
pg_dump -h "${POSTGRES_HOST}" -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" \
  | kopia snapshot create \
      --stdin-file="${source_name}.sql" \
      ${tag_args} \
      ${pin_args} \
      -

echo "[backup] $(date -u +%FT%TZ) applying retention policy"
kopia policy set "${source_name}" \
  --keep-daily="${BACKUP_KEEP_DAILY:-7}" \
  --keep-weekly="${BACKUP_KEEP_WEEKLY:-4}" \
  --keep-monthly="${BACKUP_KEEP_MONTHLY:-6}" \
  --keep-annual=0 \
  --keep-hourly=0 \
  --keep-latest=0 \
  >/dev/null

kopia snapshot expire "${source_name}" --delete

echo "[backup] $(date -u +%FT%TZ) done"
