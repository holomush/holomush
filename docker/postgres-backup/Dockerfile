# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Nightly Postgres backup image. Built inline by compose.prod.yaml under
# the `backups` profile. Runs supercronic in the foreground as a non-root
# user; supercronic invokes /usr/local/bin/backup.sh at 03:00 UTC.
#
# Tooling: kopia (encrypt + dedupe + compress + ship to S3), pg_dump
# (Postgres logical dump), supercronic (container-native scheduler,
# runs without root).

FROM alpine:3.23

# kopia is distributed as a single static binary from their GitHub releases;
# pull it in the build step. Pin the version here and bump deliberately.
ARG KOPIA_VERSION=0.18.0

RUN apk add --no-cache ca-certificates curl supercronic postgresql18-client coreutils tzdata \
    && adduser -D -g '' backup \
    && arch="$(uname -m)"; \
       case "${arch}" in \
         x86_64)  kopia_arch=x64 ;; \
         aarch64) kopia_arch=arm64 ;; \
         *) echo "unsupported arch ${arch}" >&2; exit 1 ;; \
       esac; \
    curl -fsSL "https://github.com/kopia/kopia/releases/download/v${KOPIA_VERSION}/kopia-${KOPIA_VERSION}-linux-${kopia_arch}.tar.gz" \
      | tar -xz -C /tmp \
    && install -m 0755 "/tmp/kopia-${KOPIA_VERSION}-linux-${kopia_arch}/kopia" /usr/local/bin/kopia \
    && rm -rf /tmp/kopia-*

COPY backup.sh /usr/local/bin/backup.sh
RUN chmod +x /usr/local/bin/backup.sh

# supercronic reads a plain crontab file and runs as the invoking user.
# Env vars pass through directly (no stripped environment like dcron/crond).
# The crontab sources /etc/backup.env for vars written at container start.
RUN printf '%s\n' '0 3 * * * . /etc/backup.env && /usr/local/bin/backup.sh' \
       > /etc/backup.crontab \
    && chown backup:backup /etc/backup.crontab \
    && touch /etc/backup.env \
    && chown backup:backup /etc/backup.env

USER backup

# At container start, dump env to /etc/backup.env so the crontab's shell
# can source it; then hand off to supercronic.
ENTRYPOINT ["/bin/sh", "-c", "\
  env | grep -E '^(POSTGRES_|KOPIA_PASSWORD|BACKUP_|AWS_)' \
      | sed 's/^/export /' > /etc/backup.env; \
  exec /usr/bin/supercronic /etc/backup.crontab \
"]
