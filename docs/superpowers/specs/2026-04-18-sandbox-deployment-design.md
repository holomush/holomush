<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Sandbox Deployment Design

## Overview

HoloMUSH ships signed container images to `ghcr.io/holomush/holomush:<version>`
via GoReleaser and provides `compose.prod.yaml` as an exemplary single-host
deployment. What is missing is a **deployed target** the project can point a
contributor, curious player, or demo link at: a long-running public sandbox
at `game.holomush.dev` with a stable URL, data preserved across deploys, and
recoverable backups.

Hosting is **DigitalOcean** with a single droplet. Cloudflare fronts the web
path for DNS, TLS, WAF, and origin protection via Cloudflare Tunnel. Telnet
traffic reaches the droplet directly because Cloudflare's free/paid tiers do
not proxy arbitrary TCP.

Per-PR smoke testing is **out of scope**. The existing CI pipeline runs
`compose.e2e.yaml` / `compose.e2e.cover.yaml` suites on GitHub Actions runners
and provides adequate per-PR verification. Deploying per-PR sandboxes to DO
would add operational surface (provisioning, teardown, DNS, tunnel lifecycle,
orphan cleanup) without meaningfully raising CI confidence.

## Goals

- Automatic deployment of the long-running sandbox on release-tag push
- Protocol parity with production: both telnet and web exercised end-to-end
- Preserve long-running sandbox data across deploys with recoverable backups
- Zero-cost observability acceptable (stdout logs) for sandbox scale
- Monthly cost under $30

## Non-Goals

- Per-PR ephemeral sandboxes on DigitalOcean (CI covers this)
- Multi-region or HA deployment (single droplet is sufficient)
- Blue/green or canary deployment
- Automated rollback (manual re-run with previous tag is acceptable)
- Full observability stack (OTel collector, Grafana) — tracked separately
- Off-box Postgres via DO Managed PG — graduate later if the single droplet
  becomes insufficient
- Replacement for local CI. `task pr-prep` remains the PR gate; this sandbox
  is a downstream deployment target, not a verification gate

## Hosting Decision

DigitalOcean was chosen over Cloudflare Containers because:

| Requirement                      | DigitalOcean | Cloudflare Containers |
| -------------------------------- | ------------ | --------------------- |
| Raw TCP ingress (telnet:4201)    | Yes          | **No** (HTTP only)    |
| Persistent volumes for Postgres  | Yes          | **No** (snapshots TBD)|
| Long-lived services (no sleep)   | Yes          | Scale-to-zero default |
| Mapping to existing compose file | Direct       | Requires Worker layer |

Cloudflare's role is limited to DNS, TLS termination at the edge, WAF, and
Cloudflare Tunnel for origin protection of the web path. Cloudflare Hyperdrive
is out of scope because the compute tier is not Cloudflare Workers.

## Architecture

### Resources

| Requirement        | Description                                                                              |
| ------------------ | ---------------------------------------------------------------------------------------- |
| **MUST** run       | 1 × `s-2vcpu-2gb-amd` droplet in `sfo3`                                                  |
| **MUST** attach    | 25 GB block-storage volume at `/opt/holomush/data` for Postgres                          |
| **MUST** persist   | Postgres data across droplet rebuilds via the block volume                               |
| **MUST** firewall  | Inbound: 4201/tcp (public), 22/tcp (allowlisted); deny 80/443 (tunnel-only egress)       |
| **MUST NOT** expose| Postgres port externally; DB is reachable only on the compose backend network            |

### Compose stack

A new `compose.sandbox.yaml` is derived from `compose.prod.yaml`. Key
deviations:

- **Caddy is removed.** `cloudflared` handles web ingress, eliminating Let's
  Encrypt complexity and avoiding public ports 80/443 on the droplet.
- **OTel collector is dropped** from the default profile. Sandbox uses
  container stdout and `docker logs`.
- **A `backup` service is added** — alpine + cron + awscli that runs
  `pg_dump` nightly and pushes to DO Spaces.

### Ingress model

| Path   | Routing                                                                                 |
| ------ | --------------------------------------------------------------------------------------- |
| Web    | Cloudflare proxied CNAME → CF Tunnel → `cloudflared` container → `gateway:8080`         |
| Telnet | DNS-only A record `telnet.game.holomush.dev` → droplet IPv4 → `gateway:4201`            |
| SSH    | Key-only, allowlisted CIDRs (operator + GitHub Actions deploy-runner egress)            |

### Topology

```text
                    ┌───────────────────────────────────────────────┐
                    │              Cloudflare DNS                   │
                    │                                               │
                    │  game.holomush.dev          ← proxied         │
                    │  telnet.game.holomush.dev   ← DNS-only        │
                    └────────────────────┬──────────────────────────┘
                                         │
                ┌────────────────────────┴─────────────────────────┐
                │  long-running sandbox Droplet (s-2vcpu-2gb-amd)  │
                │                                                  │
                │  cloudflared ──── CF Tunnel ──── gateway:8080    │
                │  gateway   ◀──── :4201 public ──── telnet        │
                │  core                                            │
                │  postgres  ── pg_dump → DO Spaces (nightly, 14d) │
                │  backup (cron)                                   │
                └──────────────────────────────────────────────────┘
```

## Deploy Flow

**Trigger:** push of a release tag (`v*`) or `workflow_dispatch`.

A new job `deploy-sandbox` in `.github/workflows/release.yaml`, wired after
`verify-release`, SHOULD:

1. Take a pre-deploy Postgres safety snapshot via
   `docker compose exec postgres pg_dump | gzip | aws s3 cp - s3://holomush-sandbox-backups/pre-deploy/<tag>.sql.gz`
2. `docker compose pull core gateway cloudflared`
3. `docker compose up -d --no-recreate postgres` (leave DB alone if healthy)
4. `docker compose run --rm core migrate`
5. `docker compose up -d core gateway cloudflared backup`
6. Probe `docker compose exec gateway curl -sf http://localhost:8080/healthz`
7. On failure: fail the workflow. Rollback is manual — re-run with previous
   tag as input.

## Backups

| Requirement          | Description                                                                                |
| -------------------- | ------------------------------------------------------------------------------------------ |
| **MUST** run nightly | `pg_dump` at 03:00 UTC via a `backup` compose service (alpine + cron + awscli)             |
| **MUST** push to     | `s3://holomush-sandbox-backups/game/YYYY/MM/DD.sql.gz`                                     |
| **MUST** retain      | 14 days via a DO Spaces lifecycle rule                                                     |
| **MUST** pre-deploy  | A safety snapshot before each release, keyed by tag, under `pre-deploy/<tag>.sql.gz`       |
| **SHOULD** document  | Restore procedure in `site/docs/operating/sandbox-restore.md`; restores remain manual      |

## DNS (one-time Cloudflare setup)

- `game.holomush.dev` → Tunnel CNAME `<tunnel-id>.cfargotunnel.com`, proxied
- `telnet.game.holomush.dev` → droplet IPv4 A-record, DNS-only
- `www.game.holomush.dev` (optional) → CNAME to `game.holomush.dev`

The tunnel is created once via `cloudflared tunnel create holomush-sandbox`.
Its credentials JSON is stored in GitHub Secrets as
`CLOUDFLARE_TUNNEL_CREDENTIALS` and rendered to the droplet during deploy.

## Prerequisites

The implementation MUST deliver these new capabilities before the environment
is usable:

1. **`compose.sandbox.yaml`** — new compose file with `cloudflared` +
   `backup` services and no Caddy.
2. **`deploy/doctl/firewall-sandbox.json`** — DO cloud firewall definition
   applied via workflow.
3. **`deploy/cloudflared/config.yml.tmpl`** — tunnel config rendered at
   deploy time.
4. **GitHub Secrets to add:**
   - `DIGITALOCEAN_ACCESS_TOKEN`
   - `DIGITALOCEAN_SSH_KEY_ID`
   - `DIGITALOCEAN_SSH_PRIVATE_KEY`
   - `CLOUDFLARE_API_TOKEN` (scope: DNS Edit, Tunnel management)
   - `CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_ZONE_ID`
   - `CLOUDFLARE_TUNNEL_ID`, `CLOUDFLARE_TUNNEL_CREDENTIALS`
   - `DO_SPACES_ACCESS_KEY`, `DO_SPACES_SECRET_KEY`
   - `HOLOMUSH_SANDBOX_POSTGRES_PASSWORD`
5. **Runbook** `site/docs/operating/sandbox-operations.md` — SSH, logs,
   secret rotation, tunnel recreation.
6. **Restore runbook** `site/docs/operating/sandbox-restore.md` — pull
   backup from Spaces, restore into fresh Postgres.

## Security Considerations

| Requirement          | Description                                                                                    |
| -------------------- | ---------------------------------------------------------------------------------------------- |
| **MUST NOT** expose  | Web ports 80/443 on the droplet. Only `cloudflared` reaches gateway:8080                       |
| **MUST** rely on     | Existing gateway auth + DoS hardening for public telnet exposure (connection cap, deadlines)   |
| **MUST NOT** expose  | Postgres externally; it binds to the compose backend network only                              |
| **MUST** harden SSH  | Key-only, root disabled, dedicated `holomush` user                                             |
| **SHOULD** allowlist | SSH inbound to known operator IPs + GH Actions deploy-runner egress                            |
| **MUST** rotate      | The `HOLOMUSH_SANDBOX_POSTGRES_PASSWORD` secret on a documented cadence (quarterly minimum)    |
| **MUST NOT** skip    | `task pr-prep` as the PR gate; this sandbox is a deployment target, not a CI gate              |

## Cost Estimate

| Line item                                       | Monthly      |
| ----------------------------------------------- | ------------ |
| Long-running droplet (`s-2vcpu-2gb-amd`)        | $18.00       |
| Block storage (25 GB)                           | $2.50        |
| DO Spaces (backups, <1 GB typical)              | $5.00 (base) |
| Cloudflare Tunnel / DNS (free tier)             | $0.00        |
| **Total**                                       | **~$25.50**  |

First-order costs only. Egress overages and Spaces API requests are
negligible at this scale.

## Verification

Each component ships with an explicit verification step the implementer MUST
run and show output for:

1. **Manual one-time setup is reproducible from the runbook.** Tear down and
   rebuild the droplet from scratch following only the runbook; confirm the
   web UI and telnet both come back.
2. **Deploy on tag:** tag a release. Observe `deploy-sandbox` run. Verify
   the pre-deploy backup lands in Spaces, migrations run,
   `https://game.holomush.dev` serves the web UI via the tunnel, and
   `telnet telnet.game.holomush.dev 4201` connects and can create a guest.
3. **Backup restore dry-run:** on a second throwaway droplet, pull the
   latest backup from Spaces and restore into a fresh Postgres. Document the
   commands and output in the restore runbook.
4. **Tunnel failure:** stop the `cloudflared` container. Confirm Cloudflare
   serves a 530/1033 rather than routing to a broken origin. Restart and
   confirm recovery.
5. **Gateway failure:** break `gateway` health. Confirm `cloudflared` logs
   show the health-probe failure and the edge serves a 521/522.
6. **Firewall posture:** from an off-allowlist IP, confirm 22/tcp is
   rejected; confirm 80/443 are closed (tunnel-only); confirm 4201/tcp is
   open.

## Critical Files

### New

| Path                                        | Purpose                                                  |
| ------------------------------------------- | -------------------------------------------------------- |
| `compose.sandbox.yaml`                      | Sandbox compose stack with `cloudflared` + `backup`      |
| `deploy/doctl/firewall-sandbox.json`        | DO firewall definition                                   |
| `deploy/cloudflared/config.yml.tmpl`        | Tunnel config template                                   |
| `site/docs/operating/sandbox-operations.md` | Runbook                                                  |
| `site/docs/operating/sandbox-restore.md`    | DB restore runbook                                       |

### Modified

| Path                                     | Purpose                                                        |
| ---------------------------------------- | -------------------------------------------------------------- |
| `.github/workflows/release.yaml`         | Add `deploy-sandbox` job wired after `verify-release`          |
| `site/docs/operating/deployment.md`      | Add sandbox architecture section + link to runbooks            |
| `CLAUDE.md`                              | One-line link to sandbox runbooks                              |

## Out-of-Scope Follow-Ups

Track in beads if pursued:

- Per-PR ephemeral sandboxes (revisit only if CI's `compose.e2e.cover.yaml`
  proves insufficient in practice)
- Multi-region or HA for the long-running sandbox
- Blue/green or canary deploys
- Automated rollback on deploy failure
- Observability stack (OTel collector, Grafana)
- Off-box Postgres via DO Managed PG
- Basic uptime monitoring / alerting (UptimeRobot, BetterUptime) — decide
  separately from observability
