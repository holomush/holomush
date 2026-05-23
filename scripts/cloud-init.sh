#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Cloud-init user-data script for DigitalOcean droplets.
# Installs Docker, creates /opt/holomush/, generates .env.
# Supports caddy (default) and tunnel ingress modes, plus optional
# nightly backups via Kopia.
#
# Usage: Paste into DigitalOcean droplet "User Data" field.

set -euo pipefail

HOLOMUSH_DIR="/opt/holomush"
HOLOMUSH_USER="holomush"
# Source ref to fetch compose files and config from. Any git ref works:
#   - branch  (e.g. "main", "develop") — sandbox tracking-mode
#   - tag     (e.g. "v0.1.0") — pinned production deployment
#   - commit  (e.g. "abc1234") — exact reproduce
# Default is "main" so a fresh sandbox boots from the latest committed code
# rather than depending on a released tag that may not exist yet.
# Backward-compat: legacy callers that only set HOLOMUSH_VERSION map to a
# matching v-prefixed ref. `latest` is special-cased — it's a docker image
# tag, not a git ref — so it resolves to `main`. Idempotent on a leading "v"
# (so HOLOMUSH_VERSION=v0.1.0 doesn't double-prefix).
if [ -z "${HOLOMUSH_REF:-}" ]; then
  case "${HOLOMUSH_VERSION:-}" in
    ""|latest) HOLOMUSH_REF="main" ;;
    *)         HOLOMUSH_REF="v${HOLOMUSH_VERSION#v}" ;;
  esac
fi
# HOLOMUSH_VERSION is the docker image tag (separate concept from the source
# ref above). If unset (e.g. standalone runs that only specify HOLOMUSH_REF),
# derive it from the ref: a vX.Y.Z tag drops the leading "v" to match the
# image tag goreleaser publishes; anything else (branch/sha) falls back to
# the floating `latest` tag.
if [ -z "${HOLOMUSH_VERSION:-}" ]; then
  case "${HOLOMUSH_REF}" in
    v[0-9]*) HOLOMUSH_VERSION="${HOLOMUSH_REF#v}" ;;
    *)       HOLOMUSH_VERSION="latest" ;;
  esac
fi
RELEASE_URL="https://raw.githubusercontent.com/holomush/holomush/${HOLOMUSH_REF}"

# Ingress mode: "caddy" (default, public 80/443 with Let's Encrypt) or
# "tunnel" (cloudflared, no public HTTP ports).
HOLOMUSH_INGRESS="${HOLOMUSH_INGRESS:-caddy}"

# Optional: automated nightly backups to S3-compatible storage via Kopia.
# When BACKUP_S3_BUCKET and KOPIA_PASSWORD are both set, the `backups`
# profile is enabled.
BACKUP_S3_BUCKET="${BACKUP_S3_BUCKET:-}"
BACKUP_S3_ENDPOINT="${BACKUP_S3_ENDPOINT:-}"
BACKUP_S3_ACCESS_KEY="${BACKUP_S3_ACCESS_KEY:-}"
BACKUP_S3_SECRET_KEY="${BACKUP_S3_SECRET_KEY:-}"
KOPIA_PASSWORD="${KOPIA_PASSWORD:-}"
BACKUP_KEEP_DAILY="${BACKUP_KEEP_DAILY:-7}"
BACKUP_KEEP_WEEKLY="${BACKUP_KEEP_WEEKLY:-4}"
BACKUP_KEEP_MONTHLY="${BACKUP_KEEP_MONTHLY:-6}"

# Tunnel-mode only.
CLOUDFLARE_TUNNEL_TOKEN="${CLOUDFLARE_TUNNEL_TOKEN:-}"

# Caddy-mode only.
LETSENCRYPT_EMAIL="${LETSENCRYPT_EMAIL:-}"

# Sentry error/trace reporting (backend: core + gateway). Empty DSN disables
# Sentry entirely. Defaults: environment "prod", release tracks the image tag,
# 10% trace sampling.
SENTRY_DSN="${SENTRY_DSN:-}"
SENTRY_ENVIRONMENT="${SENTRY_ENVIRONMENT:-prod}"
SENTRY_RELEASE="${SENTRY_RELEASE:-${HOLOMUSH_VERSION}}"
SENTRY_TRACES_SAMPLE_RATE="${SENTRY_TRACES_SAMPLE_RATE:-0.1}"

# When set, the script auto-starts compose after .env is written.
HOLOMUSH_DOMAIN="${HOLOMUSH_DOMAIN:-}"

# Validate ingress value
case "${HOLOMUSH_INGRESS}" in
  caddy|tunnel) ;;
  *)
    echo "ERROR: HOLOMUSH_INGRESS must be 'caddy' or 'tunnel', got '${HOLOMUSH_INGRESS}'" >&2
    exit 1
    ;;
esac

# Tunnel mode requires a token for autorun
if [ "${HOLOMUSH_INGRESS}" = "tunnel" ] && [ -n "${HOLOMUSH_DOMAIN}" ] \
   && [ -z "${CLOUDFLARE_TUNNEL_TOKEN}" ]; then
  echo "ERROR: HOLOMUSH_INGRESS=tunnel with autorun requires CLOUDFLARE_TUNNEL_TOKEN" >&2
  exit 1
fi

# --- Idempotency guard ---
if [ -f "${HOLOMUSH_DIR}/.env" ]; then
  echo "HoloMUSH already configured at ${HOLOMUSH_DIR}, skipping."
  exit 0
fi

# --- Install Docker ---
if ! command -v docker &>/dev/null; then
  echo "Installing Docker..."
  apt-get update -qq
  # git is used by the deploy-sandbox workflow to sync release assets on-host.
  apt-get install -y -qq ca-certificates curl git
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
  chmod a+r /etc/apt/keyrings/docker.asc
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] \
    https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" \
    > /etc/apt/sources.list.d/docker.list
  apt-get update -qq
  apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-compose-plugin
fi

# --- Create system user ---
# Login-capable so the deploy-sandbox GitHub Actions job can SSH in as this
# user (instead of as root) to run docker compose. DO's --ssh-keys flag
# populates root's authorized_keys, so we clone those keys to the holomush
# user so the same DigitalOcean SSH key works for both accounts.
# Create the host user with a FIXED uid/gid 1000 to match the holomush user
# baked into the container image (ghcr.io/holomush/holomush runs as uid 1000).
# A --system user lands in the 100-999 range, so bind-mounted dirs under
# /opt/holomush (e.g. config/certs) would be owned by a uid the containerized
# core/gateway can't write to → "permission denied" generating mTLS certs.
if ! id "${HOLOMUSH_USER}" &>/dev/null; then
  groupadd --gid 1000 "${HOLOMUSH_USER}"
  useradd --uid 1000 --gid 1000 --create-home --shell /bin/bash "${HOLOMUSH_USER}"
  usermod -aG docker "${HOLOMUSH_USER}"
fi

HOLOMUSH_HOME=$(getent passwd "${HOLOMUSH_USER}" | cut -d: -f6)
install -d -m 0700 -o "${HOLOMUSH_USER}" -g "${HOLOMUSH_USER}" "${HOLOMUSH_HOME}/.ssh"
if [ -s /root/.ssh/authorized_keys ] && [ ! -s "${HOLOMUSH_HOME}/.ssh/authorized_keys" ]; then
  install -m 0600 -o "${HOLOMUSH_USER}" -g "${HOLOMUSH_USER}" \
    /root/.ssh/authorized_keys "${HOLOMUSH_HOME}/.ssh/authorized_keys"
fi

# --- Create directory structure ---
echo "Creating ${HOLOMUSH_DIR}..."
mkdir -p "${HOLOMUSH_DIR}/data/postgres"
mkdir -p "${HOLOMUSH_DIR}/config/certs"
mkdir -p "${HOLOMUSH_DIR}/caddy/data"
mkdir -p "${HOLOMUSH_DIR}/caddy/config"

# --- Download compose and config files ---
echo "Downloading compose files..."
curl -fsSL "${RELEASE_URL}/compose.prod.yaml" -o "${HOLOMUSH_DIR}/compose.yaml"
curl -fsSL "${RELEASE_URL}/docker/otel-collector/config.prod.yaml" \
  -o "${HOLOMUSH_DIR}/config/otel-collector.yaml"

# Backup-service build context. The `backup` service in compose.prod.yaml
# uses `build: ./docker/postgres-backup`, so these files must exist before
# `docker compose --profile backups build backup` runs at the end of this
# script. Without them, enabling the `backups` profile breaks bootstrap.
echo "Downloading postgres-backup build context..."
mkdir -p "${HOLOMUSH_DIR}/docker/postgres-backup"
for f in Dockerfile backup.sh entrypoint.sh; do
  curl -fsSL "${RELEASE_URL}/docker/postgres-backup/${f}" \
    -o "${HOLOMUSH_DIR}/docker/postgres-backup/${f}"
done
chmod +x "${HOLOMUSH_DIR}/docker/postgres-backup/backup.sh" \
         "${HOLOMUSH_DIR}/docker/postgres-backup/entrypoint.sh"

# --- Generate .env ---
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-$(openssl rand -base64 24 | tr -d '/+=' | head -c 32)}"

cat > "${HOLOMUSH_DIR}/.env" <<EOF
# HoloMUSH Production Configuration
# Edit DOMAIN before first compose up if not set by cloud-init.

DOMAIN=${HOLOMUSH_DOMAIN:-mush.example.com}
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
HOLOMUSH_VERSION=${HOLOMUSH_VERSION}

# Ingress mode selector for compose profiles.
HOLOMUSH_INGRESS=${HOLOMUSH_INGRESS}
EOF

if [ "${HOLOMUSH_INGRESS}" = "tunnel" ]; then
  cat >> "${HOLOMUSH_DIR}/.env" <<EOF

# Cloudflare Tunnel token — tunnel ingress.
CLOUDFLARE_TUNNEL_TOKEN=${CLOUDFLARE_TUNNEL_TOKEN}
EOF
fi

if [ "${HOLOMUSH_INGRESS}" = "caddy" ] && [ -n "${LETSENCRYPT_EMAIL}" ]; then
  cat >> "${HOLOMUSH_DIR}/.env" <<EOF

# Caddy ACME email (optional; Caddy will still get certs without it).
LETSENCRYPT_EMAIL=${LETSENCRYPT_EMAIL}
EOF
fi

if [ -n "${BACKUP_S3_BUCKET}" ] \
  && [ -n "${KOPIA_PASSWORD}" ] \
  && [ -n "${BACKUP_S3_ACCESS_KEY}" ] \
  && [ -n "${BACKUP_S3_SECRET_KEY}" ]; then
  cat >> "${HOLOMUSH_DIR}/.env" <<EOF

# Automated nightly backups via Kopia (encrypted, deduped, compressed).
BACKUP_S3_BUCKET=${BACKUP_S3_BUCKET}
BACKUP_S3_ENDPOINT=${BACKUP_S3_ENDPOINT}
BACKUP_S3_ACCESS_KEY=${BACKUP_S3_ACCESS_KEY}
BACKUP_S3_SECRET_KEY=${BACKUP_S3_SECRET_KEY}
KOPIA_PASSWORD=${KOPIA_PASSWORD}
BACKUP_KEEP_DAILY=${BACKUP_KEEP_DAILY}
BACKUP_KEEP_WEEKLY=${BACKUP_KEEP_WEEKLY}
BACKUP_KEEP_MONTHLY=${BACKUP_KEEP_MONTHLY}
EOF
elif [ -n "${BACKUP_S3_BUCKET}" ] \
  || [ -n "${KOPIA_PASSWORD}" ] \
  || [ -n "${BACKUP_S3_ACCESS_KEY}" ] \
  || [ -n "${BACKUP_S3_SECRET_KEY}" ]; then
  echo "WARNING: backups require BACKUP_S3_BUCKET, KOPIA_PASSWORD, BACKUP_S3_ACCESS_KEY, and BACKUP_S3_SECRET_KEY — backups disabled" >&2
fi

if [ -n "${SENTRY_DSN}" ]; then
  cat >> "${HOLOMUSH_DIR}/.env" <<EOF

# Sentry error/trace reporting (core + gateway). Empty DSN disables it.
SENTRY_DSN=${SENTRY_DSN}
SENTRY_ENVIRONMENT=${SENTRY_ENVIRONMENT}
SENTRY_RELEASE=${SENTRY_RELEASE}
SENTRY_TRACES_SAMPLE_RATE=${SENTRY_TRACES_SAMPLE_RATE}
EOF
fi

# Data paths (commented defaults for reference)
cat >> "${HOLOMUSH_DIR}/.env" <<'EOF'

# DATA_DIR=/opt/holomush/data
# CONFIG_DIR=/opt/holomush/config
# CADDY_DIR=/opt/holomush/caddy
EOF

chmod 600 "${HOLOMUSH_DIR}/.env"

# --- Set ownership ---
chown -R "${HOLOMUSH_USER}:${HOLOMUSH_USER}" "${HOLOMUSH_DIR}"

# The postgres container runs as uid/gid 70 (postgres:18-alpine), not the
# holomush host user. Its data dir is bind-mounted at /var/lib/postgresql, so
# it must be owned by 70 — otherwise (on volume reuse) the chown -R above would
# hand postgres' own data to uid 1000 and postgres would fail to read it.
chown -R 70:70 "${HOLOMUSH_DIR}/data/postgres"

# --- Configure firewall ---
if command -v ufw &>/dev/null; then
  echo "Configuring firewall..."
  ufw allow 22/tcp    # SSH
  ufw allow 4201/tcp  # Telnet
  if [ "${HOLOMUSH_INGRESS}" = "caddy" ]; then
    ufw allow 80/tcp  # HTTP (Caddy ACME HTTP-01)
    ufw allow 443/tcp # HTTPS
  fi
  # Tunnel mode: 80/443 stay closed — cloudflared uses outbound connections only.
  ufw --force enable
fi

# --- Start compose if we have a real domain ---
case "${HOLOMUSH_DOMAIN}" in
  ""|"mush.example.com")
    echo ""
    echo "============================================"
    echo "  HoloMUSH installed to ${HOLOMUSH_DIR}"
    echo "============================================"
    echo ""
    echo "Next steps:"
    echo "  1. Point your domain's DNS A record to this server's IP"
    echo "  2. Edit ${HOLOMUSH_DIR}/.env and set DOMAIN"
    if [ "${HOLOMUSH_INGRESS}" = "tunnel" ]; then
      echo "  3. Set CLOUDFLARE_TUNNEL_TOKEN in ${HOLOMUSH_DIR}/.env"
      echo "  4. cd ${HOLOMUSH_DIR} && docker compose --profile tunnel up -d"
    else
      echo "  3. cd ${HOLOMUSH_DIR} && docker compose --profile caddy up -d"
    fi
    ;;
  *)
    echo "Starting compose (ingress=${HOLOMUSH_INGRESS})..."
    profiles="--profile ${HOLOMUSH_INGRESS}"
    if [ -n "${BACKUP_S3_BUCKET}" ] \
      && [ -n "${KOPIA_PASSWORD}" ] \
      && [ -n "${BACKUP_S3_ACCESS_KEY}" ] \
      && [ -n "${BACKUP_S3_SECRET_KEY}" ]; then
      profiles="${profiles} --profile backups"
    fi

    # If backups are enabled, ensure the Kopia repository exists before
    # cron ever fires. Try connect first; on failure, initialize a new
    # repository. This keeps the first-boot path idempotent — re-running
    # cloud-init on an existing droplet connects to the existing repo
    # rather than wiping it.
    if [ -n "${BACKUP_S3_BUCKET}" ] \
      && [ -n "${KOPIA_PASSWORD}" ] \
      && [ -n "${BACKUP_S3_ACCESS_KEY}" ] \
      && [ -n "${BACKUP_S3_SECRET_KEY}" ]; then
      echo "Ensuring Kopia repository exists..."

      endpoint_args=""
      if [ -n "${BACKUP_S3_ENDPOINT}" ]; then
        endpoint_args="--endpoint=${BACKUP_S3_ENDPOINT}"
      fi

      # Pass secrets to the child shell via environment, never interpolated
      # into the command string. sudo -u with explicit env= args prevents
      # shell metacharacters in secrets from breaking or hijacking the command.
      # shellcheck disable=SC2016
      # Backups are an OPTIONAL profile — a repo-init failure here MUST NOT
      # abort the core stack bringup. Wrap the whole init so a non-zero exit
      # only logs a warning; the script (set -e) continues to `compose up`.
      if ! sudo -u "${HOLOMUSH_USER}" \
        env \
          HOLOMUSH_DIR="${HOLOMUSH_DIR}" \
          PROFILES="${profiles}" \
          BACKUP_S3_BUCKET="${BACKUP_S3_BUCKET}" \
          BACKUP_S3_ACCESS_KEY="${BACKUP_S3_ACCESS_KEY}" \
          BACKUP_S3_SECRET_KEY="${BACKUP_S3_SECRET_KEY}" \
          ENDPOINT_ARGS="${endpoint_args}" \
          KOPIA_PASSWORD="${KOPIA_PASSWORD}" \
        /bin/sh -c '
          set -eu
          cd "$HOLOMUSH_DIR"
          docker compose $PROFILES build backup
          docker compose $PROFILES run --rm backup \
            kopia repository connect s3 \
              --bucket="$BACKUP_S3_BUCKET" \
              $ENDPOINT_ARGS \
              --access-key="$BACKUP_S3_ACCESS_KEY" \
              --secret-access-key="$BACKUP_S3_SECRET_KEY" \
            >/dev/null 2>&1 \
          || docker compose $PROFILES run --rm backup \
            kopia repository create s3 \
              --bucket="$BACKUP_S3_BUCKET" \
              $ENDPOINT_ARGS \
              --access-key="$BACKUP_S3_ACCESS_KEY" \
              --secret-access-key="$BACKUP_S3_SECRET_KEY"
        '; then
        echo "WARNING: Kopia repository init failed — nightly backups will be" \
             "unavailable until fixed. Continuing with the core stack bringup." >&2
      fi
    fi

    su - "${HOLOMUSH_USER}" -s /bin/sh -c "
      cd ${HOLOMUSH_DIR} && docker compose ${profiles} up -d
    "
    echo ""
    echo "============================================"
    echo "  HoloMUSH running (ingress=${HOLOMUSH_INGRESS})"
    echo "============================================"
    echo "  Compose profiles: ${profiles}"
    echo "  Domain: ${HOLOMUSH_DOMAIN}"
    ;;
esac
