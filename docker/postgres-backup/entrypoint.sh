#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Container entrypoint. Starts as root so it can fix ownership of the kopia
# cache directory (a root-owned Docker named volume mounted at
# /home/backup/.cache/kopia), then drops to the unprivileged `backup` user
# via su-exec. If invoked with no args (or the default CMD "cron"), dump
# relevant env vars to /etc/backup.env so supercronic's minimal shell can
# source them, then exec supercronic. Any other argv — e.g.,
# `docker compose run --rm backup kopia repository connect s3 ...` — is
# executed directly (still dropped to `backup`), bypassing the cron path.

set -eu

# Docker named volumes are created root-owned; the non-root `backup` user
# cannot write into the mounted cache dir without this. Idempotent.
mkdir -p /home/backup/.cache/kopia
chown -R backup:backup /home/backup/.cache

env \
  | grep -E '^(POSTGRES_|PGPASSWORD|KOPIA_PASSWORD|BACKUP_|AWS_)' \
  | sed 's/^/export /' \
  > /etc/backup.env

if [ "$#" -eq 0 ] || [ "$1" = "cron" ]; then
  exec su-exec backup /usr/bin/supercronic /etc/backup.crontab
fi

exec su-exec backup "$@"
