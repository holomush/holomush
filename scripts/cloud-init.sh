#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Cloud-init user-data script for DigitalOcean droplets.
# Installs Docker, creates /opt/holomush/, generates .env.
# Operator must set DOMAIN in .env before starting the stack.
#
# Usage: Paste into DigitalOcean droplet "User Data" field.

set -euo pipefail

HOLOMUSH_DIR="/opt/holomush"
HOLOMUSH_USER="holomush"
RELEASE_URL="https://raw.githubusercontent.com/holomush/holomush/main"

# --- Idempotency guard ---
if [ -f "${HOLOMUSH_DIR}/.env" ]; then
  echo "HoloMUSH already configured at ${HOLOMUSH_DIR}, skipping."
  exit 0
fi

# --- Install Docker ---
if ! command -v docker &>/dev/null; then
  echo "Installing Docker..."
  apt-get update -qq
  apt-get install -y -qq ca-certificates curl
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
if ! id "${HOLOMUSH_USER}" &>/dev/null; then
  useradd --system --create-home --shell /usr/sbin/nologin "${HOLOMUSH_USER}"
  usermod -aG docker "${HOLOMUSH_USER}"
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

# --- Generate .env ---
POSTGRES_PASSWORD=$(openssl rand -base64 24 | tr -d '/+=' | head -c 32)

cat > "${HOLOMUSH_DIR}/.env" <<EOF
# HoloMUSH Production Configuration
# Edit DOMAIN before running: docker compose up -d

# REQUIRED: Your domain name (Caddy gets HTTPS certs automatically)
DOMAIN=mush.example.com

# Database (auto-generated, change only if using external DB)
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}

# Image version (default: latest)
# HOLOMUSH_VERSION=latest

# Data paths (default: /opt/holomush/*)
# DATA_DIR=/opt/holomush/data
# CONFIG_DIR=/opt/holomush/config
# CADDY_DIR=/opt/holomush/caddy

# Optional: Observability (uncomment and configure, then start with --profile observability)
# Grafana Cloud example:
# OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp-gateway-prod-us-central-0.grafana.net/otlp
# OTEL_EXPORTER_OTLP_HEADERS=Basic <base64-encoded-user:token>
EOF

chmod 600 "${HOLOMUSH_DIR}/.env"

# --- Set ownership ---
chown -R "${HOLOMUSH_USER}:${HOLOMUSH_USER}" "${HOLOMUSH_DIR}"

# --- Configure firewall ---
if command -v ufw &>/dev/null; then
  echo "Configuring firewall..."
  ufw allow 22/tcp   # SSH
  ufw allow 80/tcp   # HTTP (Caddy ACME)
  ufw allow 443/tcp  # HTTPS
  ufw allow 4201/tcp # Telnet
  ufw --force enable
fi

echo ""
echo "============================================"
echo "  HoloMUSH installed to ${HOLOMUSH_DIR}"
echo "============================================"
echo ""
echo "Next steps:"
echo "  1. Point your domain's DNS A record to this server's IP"
echo "  2. Edit ${HOLOMUSH_DIR}/.env and set DOMAIN"
echo "  3. cd ${HOLOMUSH_DIR} && docker compose up -d"
echo "  4. Visit https://your-domain to verify"
echo ""
