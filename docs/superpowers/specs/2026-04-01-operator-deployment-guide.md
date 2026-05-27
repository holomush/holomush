<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Operator Deployment Guide — Design Spec

## Overview

Provide a production-ready Docker Compose deployment for HoloMUSH, targeting
DigitalOcean droplets as the documented platform. An operator with a domain name
and a DO account can go from zero to a running MUSH in under 10 minutes.

## Non-Goals

- Kubernetes / Helm charts
- Bare-metal / systemd deployment
- Multi-node / HA configuration
- Automated backup to object storage (tracked by holomush-n6z3)
- Disaster recovery runbook (tracked by holomush-6doi)

## Deliverables

### 1. `compose.prod.yaml` — Production Docker Compose

A single compose file with five services:

| Service | Image | Ports (host) | Purpose |
| --------------- | ------------------------------------ | ------------ | ---------------------------------------- |
| postgres | `postgres:18-alpine` | none | Database (internal only) |
| core | `ghcr.io/holomush/holomush:latest` | none | Game engine, gRPC on internal network |
| gateway | `ghcr.io/holomush/holomush:latest` | 4201 (TCP) | Web client + telnet |
| caddy | `caddy:2-alpine` | 80, 443 | Reverse proxy, automatic HTTPS via ACME |
| otel-collector | `otel/opentelemetry-collector-contrib` | none | Telemetry export (profile: observability) |

**Architecture:**

```text
Internet
  │
  ├─ HTTPS (:443/:80) ──▶ Caddy ──▶ Gateway (:8080 internal)
  │
  └─ Telnet (:4201) ────────────▶ Gateway (:4201 direct)
                                      │
                                      ▼ gRPC :9000
                                    Core
                                      │
                                      ▼ :5432
                                   Postgres

  [Optional: otel-collector ──▶ Grafana Cloud / any OTLP backend]
```

**Key design decisions:**

- Caddy handles TLS termination with automatic Let's Encrypt certificates.
  The operator sets `DOMAIN` in `.env`; Caddy does the rest.
- Telnet port 4201 is exposed directly on the host — MU\* clients do not
  support TLS, so no proxy wrapping.
- Postgres binds only to the Docker network. No host port exposure.
- Core gRPC (9000), control plane (9001/9002), and metrics (9100/9101)
  ports are internal-only.
- The OTEL Collector is gated behind the `observability` compose profile.
  It starts only when `docker compose --profile observability up -d` is used.

**Persistent data — all bind mounts to host filesystem:**

```text
/opt/holomush/
├── data/
│   └── postgres/           # PostgreSQL data directory
├── config/
│   ├── certs/              # Auto-generated mTLS certificates
│   └── otel-collector.yaml # OTEL collector config (optional)
├── caddy/                  # Caddy state (ACME certs, config DB)
├── .env                    # All operator configuration
└── compose.yaml            # Production compose file
```

Bind mounts instead of Docker named volumes so the entire `/opt/holomush/`
directory can live on a separate block storage volume. Backup is
`tar czf` or a volume snapshot.

**`.env` file — all configuration in one place:**

```bash
# Required
DOMAIN=mush.example.com
POSTGRES_PASSWORD=<generated-by-cloud-init>
DATABASE_URL=postgres://holomush:${POSTGRES_PASSWORD}@postgres:5432/holomush?sslmode=disable

# Optional: override data/config paths (default: /opt/holomush/)
# DATA_DIR=/opt/holomush/data
# CONFIG_DIR=/opt/holomush/config
# CADDY_DIR=/opt/holomush/caddy

# Optional: observability (Grafana Cloud or any OTLP endpoint)
# OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp-gateway-prod-us-central-0.grafana.net/otlp
# OTEL_EXPORTER_OTLP_HEADERS=Authorization=Basic <base64>
```

**Caddy configuration** is embedded in the compose file via a `command:` or
inline Caddyfile that reverse-proxies `{$DOMAIN}` to `gateway:8080`.

**OTEL Collector configuration** uses environment variable substitution for
the exporter endpoint and headers. Operators can point it at Grafana Cloud,
Axiom, SigNoz, or any OTLP-compatible backend by changing `.env` values.

**Healthchecks:**

- postgres: `pg_isready -U holomush`
- core: `wget -q --spider http://127.0.0.1:9100/healthz/readiness`
- gateway: `wget -q --spider http://127.0.0.1:9101/healthz/readiness`
- Service dependencies ensure startup order: postgres → core → gateway → caddy.

### 2. `scripts/cloud-init.sh` — DigitalOcean First-Boot Script

A cloud-init user-data script that prepares a fresh Ubuntu 24.04 droplet:

1. Install Docker Engine from the official apt repository.
2. Create `holomush` system user, add to `docker` group.
3. Create `/opt/holomush/{data/postgres,config/certs,caddy}` owned by
   `holomush`.
4. Download `compose.prod.yaml` from the GitHub release as
   `/opt/holomush/compose.yaml` (renamed for simplicity at the deploy path).
   Download `otel-collector.yaml` to `/opt/holomush/config/`.
5. Generate `.env` with a random `POSTGRES_PASSWORD` via `openssl rand -base64 24`.
6. Configure UFW firewall: allow 22 (SSH), 80, 443, 4201.
7. Log instructions: "Edit `/opt/holomush/.env` to set DOMAIN, then
   `cd /opt/holomush && docker compose up -d`".

The script MUST NOT start the stack automatically. The operator must set
`DOMAIN` first so Caddy does not request a certificate for a placeholder domain.

The script MUST be idempotent — running it twice does not overwrite an
existing `.env` or data directory.

### 3. `site/docs/operating/deployment.md` — Deployment Guide

Step-by-step guide targeting operators with basic Linux and Docker familiarity.

**Structure:**

```text
# Deploying HoloMUSH

## Prerequisites
- Domain name with DNS control
- DigitalOcean account (or any Linux host with Docker)

## Quick Start (DigitalOcean)
1. Create a droplet
   - Ubuntu 24.04, $6/mo (1 vCPU, 1GB RAM)
   - Paste the cloud-init script into User Data
2. Point DNS
   - A record: your-domain → droplet IP
3. Configure
   - SSH in, edit /opt/holomush/.env, set DOMAIN
4. Start
   - cd /opt/holomush && docker compose up -d
5. Verify
   - Visit https://your-domain
   - telnet your-domain 4201
   - holomush status (from the container)

## Optional: Block Storage
- Attach a DO volume
- Mount at /opt/holomush/ (or set DATA_DIR in .env)
- Recommended for production — easy to snapshot and resize

## Optional: Monitoring with Grafana Cloud
- Sign up at grafana.com (free tier: 10K metrics, 50GB logs, 50GB traces)
- Create a service account token
- Add OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_EXPORTER_OTLP_HEADERS to .env
- docker compose --profile observability up -d
- Note: any OTLP-compatible backend works (Axiom, SigNoz, etc.)

## Upgrading
- docker compose pull && docker compose up -d
- Migrations run automatically on core startup

## Backups
- Entire deployment lives in /opt/holomush/
- tar czf or snapshot the block volume
- See: Backup Guide (coming soon)

## Troubleshooting
- docker compose logs core
- docker compose logs gateway
- Check healthz endpoints
- See: operations.md for detailed monitoring

## Generic Linux (non-DigitalOcean)
- Any Linux host with Docker works
- Install Docker, create /opt/holomush/, download compose file
- Follow the same Configure → Start → Verify steps
```

Links to existing docs (`configuration.md`, `database.md`, `operations.md`,
`authentication.md`) for deeper topics. Does not duplicate their content.

## Testing

The compose file MUST be validated by:

- `docker compose -f compose.prod.yaml config` — syntax check
- Manual test: spin up a DO droplet with the cloud-init script, verify the
  full flow (DNS, HTTPS, telnet, guest login, say command)
- The cloud-init script MUST be tested with `shellcheck`.

## Security Considerations

- `POSTGRES_PASSWORD` is generated randomly, never a default value.
- Postgres binds only to the Docker network.
- Core control plane (9001) and gateway control plane (9002) are never
  exposed to the host.
- Metrics endpoints (9100/9101) are internal-only.
- UFW configured to allow only 22, 80, 443, 4201.
- The `.env` file SHOULD have `chmod 600` permissions.
- Caddy handles HTTPS automatically — no manual certificate management.

## Follow-On Work

- `holomush-q426` — Backup and restore guide
- `holomush-6doi` — Disaster recovery: recreate from backup
- `holomush-n6z3` — Automated periodic backup to S3/object storage
