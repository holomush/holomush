<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Operator Deployment Guide Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Provide a production Docker Compose stack, cloud-init script, and deployment guide so an operator can deploy HoloMUSH on a DigitalOcean droplet in under 10 minutes.

**Architecture:** Production compose (core + gateway + postgres + Caddy + optional OTEL collector) with all persistent data on host bind mounts under `/opt/holomush/`. Cloud-init automates first-boot setup. Deployment docs in `site/docs/operating/`.

**Tech Stack:** Docker Compose, Caddy 2, OpenTelemetry Collector, cloud-init, POSIX shell

**Spec:** `docs/superpowers/specs/2026-04-01-operator-deployment-guide.md`

---

## File Map

| Action | File | Responsibility |
| ------ | ---- | -------------- |
| Create | `compose.prod.yaml` | Production Docker Compose (5 services) |
| Create | `docker/otel-collector/config.prod.yaml` | OTEL collector config for SaaS backends |
| Create | `scripts/cloud-init.sh` | DigitalOcean first-boot automation |
| Create | `site/docs/operating/deployment.md` | Step-by-step deployment guide |
| Modify | `site/docs/operating/index.md` | Add link to deployment guide |

---

### Task 1: Production Docker Compose

**Files:**

- Create: `compose.prod.yaml`

- [ ] **Step 1: Create the production compose file**

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Production deployment stack.
# Copy to /opt/holomush/compose.yaml and configure .env before starting.
# See: https://holomush.dev/operating/deployment/

services:
  postgres:
    image: postgres:18-alpine
    restart: unless-stopped
    environment:
      POSTGRES_USER: holomush
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?Set POSTGRES_PASSWORD in .env}
      POSTGRES_DB: holomush
    volumes:
      - ${DATA_DIR:-/opt/holomush/data}/postgres:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U holomush"]
      interval: 5s
      timeout: 5s
      retries: 10

  core:
    image: ghcr.io/holomush/holomush:${HOLOMUSH_VERSION:-latest}
    restart: unless-stopped
    command:
      - core
      - --grpc-addr=0.0.0.0:9000
      - --metrics-addr=0.0.0.0:9100
      - --log-format=json
    environment:
      DATABASE_URL: postgres://holomush:${POSTGRES_PASSWORD}@postgres:5432/holomush?sslmode=disable
      OTEL_EXPORTER_OTLP_ENDPOINT: ${OTEL_EXPORTER_OTLP_ENDPOINT:-http://localhost:4317}
    volumes:
      - ${CONFIG_DIR:-/opt/holomush/config}/certs:/home/holomush/.config/holomush/certs
    depends_on:
      postgres:
        condition: service_healthy
    healthcheck:
      test: ["CMD-SHELL", "wget -q --spider http://127.0.0.1:9100/healthz/readiness || exit 1"]
      interval: 5s
      timeout: 5s
      retries: 15
      start_period: 15s

  gateway:
    image: ghcr.io/holomush/holomush:${HOLOMUSH_VERSION:-latest}
    restart: unless-stopped
    command:
      - gateway
      - --core-addr=core:9000
      - --metrics-addr=0.0.0.0:9101
      - --log-format=json
    environment:
      OTEL_EXPORTER_OTLP_ENDPOINT: ${OTEL_EXPORTER_OTLP_ENDPOINT:-http://localhost:4317}
    volumes:
      - ${CONFIG_DIR:-/opt/holomush/config}/certs:/home/holomush/.config/holomush/certs:ro
    ports:
      - "4201:4201"
    depends_on:
      core:
        condition: service_healthy

  caddy:
    image: caddy:2-alpine
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    environment:
      DOMAIN: ${DOMAIN:?Set DOMAIN in .env}
    command: caddy reverse-proxy --from ${DOMAIN} --to gateway:8080
    volumes:
      - ${CADDY_DIR:-/opt/holomush/caddy}/data:/data
      - ${CADDY_DIR:-/opt/holomush/caddy}/config:/config
    depends_on:
      - gateway

  otel-collector:
    image: otel/opentelemetry-collector-contrib:latest
    restart: unless-stopped
    profiles: [observability]
    environment:
      OTEL_EXPORTER_OTLP_ENDPOINT: ${OTEL_EXPORTER_OTLP_ENDPOINT}
      OTEL_EXPORTER_OTLP_HEADERS: ${OTEL_EXPORTER_OTLP_HEADERS}
    volumes:
      - ${CONFIG_DIR:-/opt/holomush/config}/otel-collector.yaml:/etc/otelcol-contrib/config.yaml:ro
```

- [ ] **Step 2: Validate compose syntax**

Run: `docker compose -f compose.prod.yaml config > /dev/null`

This will fail because required env vars aren't set. Instead, validate with dummy values:

Run: `DOMAIN=test.example.com POSTGRES_PASSWORD=test docker compose -f compose.prod.yaml config > /dev/null && echo "Valid"`

Expected: `Valid`

- [ ] **Step 3: Commit**

```bash
jj commit -m "feat(deploy): add production Docker Compose

Services: postgres, core, gateway, caddy (HTTPS), otel-collector (optional).
All persistent data on host bind mounts under /opt/holomush/.
Configuration via .env file. Caddy handles automatic HTTPS via ACME."
```

---

### Task 2: Production OTEL Collector Config

**Files:**

- Create: `docker/otel-collector/config.prod.yaml`

- [ ] **Step 1: Create the production collector config**

This config uses environment variable substitution to export to any OTLP backend.

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Production OTEL Collector config — exports to any OTLP-compatible backend.
# Set OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_EXPORTER_OTLP_HEADERS in .env.
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

exporters:
  otlphttp:
    endpoint: ${env:OTEL_EXPORTER_OTLP_ENDPOINT}
    headers:
      authorization: ${env:OTEL_EXPORTER_OTLP_HEADERS}

processors:
  batch:
    send_batch_size: 1024
    timeout: 5s

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlphttp]
    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlphttp]
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlphttp]
```

- [ ] **Step 2: Commit**

```bash
jj commit -m "feat(deploy): add production OTEL collector config

Exports traces, metrics, and logs to any OTLP-compatible backend
(Grafana Cloud, Axiom, SigNoz, etc.) via environment variables."
```

---

### Task 3: Cloud-Init Script

**Files:**

- Create: `scripts/cloud-init.sh`

- [ ] **Step 1: Create the cloud-init script**

```bash
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
```

- [ ] **Step 2: Make executable and lint**

Run: `chmod +x scripts/cloud-init.sh && shellcheck scripts/cloud-init.sh`

Expected: No errors (or only informational notes about `source`).

- [ ] **Step 3: Commit**

```bash
jj commit -m "feat(deploy): add cloud-init script for DigitalOcean droplets

Installs Docker, creates /opt/holomush/ directory structure, downloads
compose file, generates .env with random postgres password, configures
UFW firewall. Idempotent — skips if .env already exists."
```

---

### Task 4: Deployment Guide Documentation

**Files:**

- Create: `site/docs/operating/deployment.md`
- Modify: `site/docs/operating/index.md`

- [ ] **Step 1: Create the deployment guide**

````markdown
# Deploying HoloMUSH

Deploy a production HoloMUSH instance on a DigitalOcean droplet with automatic
HTTPS, telnet support, and optional monitoring. The entire deployment runs in
Docker and takes about 10 minutes.

## Prerequisites

- A **domain name** you control (for HTTPS certificates)
- A **DigitalOcean account** (or any Linux host with Docker 24+)

## Quick Start

### 1. Create a Droplet

In the DigitalOcean control panel:

1. Click **Create Droplet**
2. Choose **Ubuntu 24.04 LTS**
3. Select the **$6/mo** plan (1 vCPU, 1 GB RAM) — sufficient for most games
4. Choose a datacenter region close to your players
5. Under **Advanced Options**, check **Add Initialization Scripts**
6. Paste the
   [cloud-init script](https://raw.githubusercontent.com/holomush/holomush/main/scripts/cloud-init.sh)
   into the User Data field
7. Click **Create Droplet**

The script installs Docker, creates the deployment directory at
`/opt/holomush/`, and generates a configuration file with a random database
password.

### 2. Point Your DNS

Create an **A record** pointing your domain to the droplet's IP address:

```text
mush.example.com  →  A  →  203.0.113.42
```

Wait for DNS to propagate (usually 1–5 minutes).

### 3. Configure

SSH into your droplet and edit the configuration:

```bash
ssh root@your-droplet-ip
nano /opt/holomush/.env
```

Set `DOMAIN` to your actual domain:

```bash
DOMAIN=mush.example.com
```

Save and exit.

### 4. Start the Stack

```bash
cd /opt/holomush
docker compose up -d
```

On first boot, HoloMUSH runs database migrations and seeds the default
world ("The Crossroads"). This takes about 30 seconds.

### 5. Verify

- **Web client:** Visit `https://your-domain` — you should see the HoloMUSH
  landing page. Click "Try as Guest" to enter the game.
- **Telnet:** Connect with any MU\* client: `telnet your-domain 4201`
- **Logs:** `docker compose logs -f core` to watch game events.

## Block Storage (Recommended)

For easier backups and the ability to resize storage independently:

1. In DigitalOcean, create a **Volume** (10 GB is plenty to start)
2. Attach it to your droplet
3. Mount it:

```bash
# Mount the volume (DigitalOcean provides exact commands)
mount -o defaults,nofail,discard /dev/disk/by-id/scsi-0DO_Volume_xxx /mnt/holomush

# Stop the stack
cd /opt/holomush && docker compose down

# Move data to the volume
mv /opt/holomush/data /mnt/holomush/data

# Update .env
echo 'DATA_DIR=/mnt/holomush/data' >> /opt/holomush/.env

# Restart
docker compose up -d
```

Now you can snapshot the volume for backups.

## Monitoring (Optional)

HoloMUSH exports OpenTelemetry data (traces, metrics, logs) to any
OTLP-compatible backend. [Grafana Cloud](https://grafana.com/products/cloud/)
offers a generous free tier (10K metrics series, 50GB logs, 50GB traces).

### Set Up Grafana Cloud

1. Sign up at [grafana.com](https://grafana.com/) (free)
2. Go to **Connections → Add new connection → OpenTelemetry (OTLP)**
3. Copy the OTLP endpoint URL and generate an API token
4. Add to `/opt/holomush/.env`:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp-gateway-prod-us-central-0.grafana.net/otlp
OTEL_EXPORTER_OTLP_HEADERS=Basic <your-base64-token>
```

5. Start with the observability profile:

```bash
docker compose --profile observability up -d
```

Any OTLP-compatible backend works — Axiom, SigNoz, Better Stack, and others
all accept the same format. Change the endpoint and headers in `.env`.

## Upgrading

Pull the latest images and restart:

```bash
cd /opt/holomush
docker compose pull
docker compose up -d
```

Database migrations run automatically on startup. Check logs after upgrading:

```bash
docker compose logs core | head -50
```

## Backups

The entire deployment lives in `/opt/holomush/`. Back it up with:

```bash
# Stop postgres to ensure consistency
cd /opt/holomush && docker compose stop postgres

# Archive
tar czf /tmp/holomush-backup-$(date +%Y%m%d).tar.gz /opt/holomush/

# Restart
docker compose start postgres
```

Or snapshot the DigitalOcean volume if using block storage.

## Troubleshooting

| Symptom | Check |
| ---------------------------------- | ------------------------------------------------------------ |
| HTTPS not working | DNS propagated? `dig your-domain` should show droplet IP |
| "502 Bad Gateway" from Caddy | Gateway healthy? `docker compose logs gateway` |
| Telnet connection refused | UFW allows 4201? `ufw status` |
| Game world empty after first boot | Core bootstrap logs: `docker compose logs core \| grep bootstrap` |
| Caddy cert errors | Port 80 open? ACME needs it for HTTP-01 challenge |

For detailed monitoring and health check information, see
[Operations](operations.md).

## Generic Linux Host

Any Linux host with Docker 24+ works. Skip the DigitalOcean-specific steps
and instead:

1. Install Docker: [docs.docker.com/engine/install](https://docs.docker.com/engine/install/)
2. Download `compose.prod.yaml` to `/opt/holomush/compose.yaml`
3. Download `docker/otel-collector/config.prod.yaml` to
   `/opt/holomush/config/otel-collector.yaml`
4. Create `/opt/holomush/.env` (see the generated `.env` in the cloud-init
   script for the template)
5. Create the data directories: `mkdir -p /opt/holomush/{data/postgres,config/certs,caddy/{data,config}}`
6. Follow steps 3–5 from the Quick Start above
````

- [ ] **Step 2: Add deployment link to the operating index page**

In `site/docs/operating/index.md`, add a link to the deployment guide in the Documentation section. Insert after the existing "Installation" link:

```markdown
- [Deployment](deployment.md) -- Production deployment with Docker Compose
```

- [ ] **Step 3: Build the docs site to verify**

Run: `task docs:build`

Expected: Build succeeds, no broken links.

- [ ] **Step 4: Commit**

```bash
jj commit -m "docs(operating): add production deployment guide

Step-by-step guide for deploying on DigitalOcean droplets with Docker
Compose, Caddy for automatic HTTPS, and optional Grafana Cloud monitoring.
Covers block storage, upgrades, backups, and troubleshooting."
```

---

### Task 5: Final Verification

- [ ] **Step 1: Validate compose file with all profiles**

Run: `DOMAIN=test.example.com POSTGRES_PASSWORD=test OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317 OTEL_EXPORTER_OTLP_HEADERS=none docker compose -f compose.prod.yaml --profile observability config > /dev/null && echo "Valid"`

Expected: `Valid`

- [ ] **Step 2: Lint the cloud-init script**

Run: `shellcheck scripts/cloud-init.sh`

Expected: No errors.

- [ ] **Step 3: Lint markdown**

Run: `task fmt && task lint:markdown`

Expected: No errors in the new files.

- [ ] **Step 4: Run existing tests to confirm no regressions**

Run: `task test && task lint`

Expected: All pass (no Go code changed, but verify nothing broke).
