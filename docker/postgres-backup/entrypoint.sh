#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Container entrypoint. If invoked with no args (or the default CMD "cron"),
# dump relevant env vars to /etc/backup.env so supercronic's minimal shell
# can source them, then exec supercronic. Any other argv — e.g.,
# `docker compose run --rm backup kopia repository connect s3 ...` — is
# executed directly, bypassing the cron path.

set -eu

env \
  | grep -E '^(POSTGRES_|KOPIA_PASSWORD|BACKUP_|AWS_)' \
  | sed 's/^/export /' \
  > /etc/backup.env

if [ "$#" -eq 0 ] || [ "$1" = "cron" ]; then
  exec /usr/bin/supercronic /etc/backup.crontab
fi

exec "$@"
