<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Sandbox Deployment Design

## Overview

HoloMUSH ships signed container images to `ghcr.io/holomush/holomush:<version>`
via GoReleaser and provides `compose.prod.yaml` as an exemplary single-host
deployment. What is missing is:

1. A **deployed long-running sandbox** at `game.holomush.dev` — a stable URL
   the project can point contributors, curious players, or demo links at.
   Data is preserved across deploys and backed up.
2. A **reusable first-boot recipe** that both the sandbox *and* third-party
   self-hosters can use to stand up a HoloMUSH server from a blank cloud VM
   in one step. Published as a cloud-init `user_data` file.

Hosting for the sandbox is **DigitalOcean** with a single droplet. Cloudflare
fronts the web path via Cloudflare Tunnel (no public 80/443 on the droplet).
Telnet traffic reaches the droplet directly because Cloudflare's free/paid
tiers do not proxy arbitrary TCP.

For self-hosters, the same recipe supports **two ingress modes** — Cloudflare
Tunnel or Caddy + Let's Encrypt — selected at provision time by environment
variables. The sandbox itself uses Tunnel; supporting both modes in the
published recipe broadens the self-hosting audience without requiring
Cloudflare for operators who do not want it.

Per-PR smoke testing is **out of scope**. The existing CI pipeline runs
`compose.e2e.yaml` / `compose.e2e.cover.yaml` suites on GitHub Actions runners
and provides adequate per-PR verification. Deploying per-PR sandboxes to DO
would add operational surface without meaningfully raising CI confidence.

Packer images and DO Marketplace listings are deferred. A cloud-init + compose
recipe works on any cloud VM, has no per-release rebuild treadmill, and
dogfoods itself via the sandbox's own first-boot path. Prebuilt images and
marketplace listings can be added later if operator demand materializes.

## Goals

- Automatic deployment of the long-running sandbox on release-tag push
- Protocol parity with production: both telnet and web exercised end-to-end
- Preserve long-running sandbox data across deploys with recoverable backups
- A single first-boot recipe (cloud-init + compose) that serves both the
  sandbox and third-party self-hosters
- Operator-chosen ingress: Cloudflare Tunnel (zero-trust) or Caddy + Let's
  Encrypt (public 443 with automatic TLS)
- Zero-cost observability acceptable (stdout logs) for sandbox scale
- Monthly cost under $30 for the sandbox

## Non-Goals

- Per-PR ephemeral sandboxes on DigitalOcean (CI covers this)
- Packer-built cloud images and DO Marketplace listings (deferred)
- Multi-region or HA deployment (single droplet is sufficient)
- Blue/green or canary deployment
- Automated rollback (manual re-run with previous tag is acceptable)
- Full observability stack (OTel collector, Grafana) — tracked separately
- Off-box Postgres via DO Managed PG — graduate later if the single droplet
  becomes insufficient
- Self-hosting support for clouds other than "generic Ubuntu 24.04 VM" — the
  cloud-init recipe is portable in principle but only DO is actively tested
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

`compose.prod.yaml` (existing) is extended in place — no separate
`compose.sandbox.yaml`. The existing services (`postgres`, `core`, `gateway`,
`caddy`, `otel-collector`) stay as-is; new services are added under compose
profiles so operators opt in.

- **Default behaviour unchanged.** `docker compose up -d` still brings up the
  Caddy + Let's Encrypt path documented today. Existing self-hosters are not
  disrupted.
- **New `tunnel` profile** runs a `cloudflared` container that terminates a
  Cloudflare Tunnel to `gateway:8080`. Operators who enable it SHOULD also
  stop the `caddy` service; the cloud-init handles this automatically.
- **New `backups` profile** runs an alpine + cron + [Kopia](https://kopia.io/)
  container. Nightly, `pg_dump` is streamed into `kopia snapshot create`,
  which applies client-side encryption (AES-256 by default), content-addressable
  deduplication, zstd compression, and integrity verification before uploading
  to an S3-compatible bucket (DO Spaces or AWS S3). Off by default; enabled
  when the operator provides a `BACKUP_S3_BUCKET` + `KOPIA_PASSWORD` pair.
- **`otel-collector` stays under its existing `observability` profile** —
  not in scope here.

Selecting ingress is one environment variable (`HOLOMUSH_INGRESS=tunnel|caddy`,
default `caddy`) read by the cloud-init script, which then starts compose
with the right `--profile` combination. The sandbox pins this to `tunnel`;
self-hosters default to `caddy` or opt into `tunnel`.

### Ingress model

Sandbox uses the `tunnel` profile:

| Path   | Routing                                                                                 |
| ------ | --------------------------------------------------------------------------------------- |
| Web    | Cloudflare proxied CNAME → CF Tunnel → `cloudflared` container → `gateway:8080`         |
| Telnet | DNS-only A record `telnet.game.holomush.dev` → droplet IPv4 → `gateway:4201`            |
| SSH    | Key-only, allowlisted CIDRs (operator + GitHub Actions deploy-runner egress)            |

Self-hosters using the `caddy` profile:

| Path   | Routing                                                                                 |
| ------ | --------------------------------------------------------------------------------------- |
| Web    | Public A record → Caddy on host `:80`/`:443` → `gateway:8080`. Let's Encrypt via HTTP-01 |
| Telnet | Public A record → droplet `:4201` → `gateway:4201`                                      |
| SSH    | Operator's responsibility (cloud-init ships with key-only defaults)                     |

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

## Cloud-Init First-Boot Recipe

`scripts/cloud-init.sh` (existing) is extended in place — no new cloud-init
artifact. The existing script already handles Docker install, `holomush`
user creation, `/opt/holomush/` layout, random Postgres password generation,
UFW firewall rules, and idempotency. The changes:

- **Read `HOLOMUSH_INGRESS` from the environment** (default `caddy`). When
  `tunnel`, skip UFW rules for 80/443 and write `CLOUDFLARE_TUNNEL_TOKEN` to
  `.env`.
- **Read optional backup variables** (`BACKUP_S3_BUCKET`,
  `BACKUP_S3_ACCESS_KEY`, `BACKUP_S3_SECRET_KEY`, `BACKUP_RETENTION_DAYS`)
  and add them to `.env`. When set, the script adds `--profile backups` to
  the compose invocation.
- **Actually start compose.** Today the script prints "next steps" and
  leaves compose to the operator. With the ingress and backups env passed
  via user-data, the script can finish the deploy autonomously. Existing
  operators who paste the script without those env vars still get the
  manual-start flow because `DOMAIN` starts as `mush.example.com` which the
  script detects and defers to manual.
- **Drop the OTel collector download** — not used at sandbox scale; existing
  users who want OTel can follow the original doc path.

Variables consumed (all optional unless noted; operator supplies via DO
droplet user-data env or by pre-editing `.env` before first boot):

| Variable                        | Required when             | Purpose                                        |
| ------------------------------- | ------------------------- | ---------------------------------------------- |
| `HOLOMUSH_VERSION`              | always (defaults pinned)  | Image tag, e.g. `v0.3.0` or `latest`           |
| `HOLOMUSH_DOMAIN`               | always (for autorun)      | Base domain, e.g. `game.example.com`           |
| `HOLOMUSH_INGRESS`              | defaults `caddy`          | `tunnel` or `caddy`                            |
| `CLOUDFLARE_TUNNEL_TOKEN`       | `HOLOMUSH_INGRESS=tunnel` | Tunnel connection token                        |
| `LETSENCRYPT_EMAIL`             | `HOLOMUSH_INGRESS=caddy`  | ACME account contact (Caddy reads from `.env`) |
| `BACKUP_S3_BUCKET`              | optional                  | Enables `backups` profile when set             |
| `BACKUP_S3_ENDPOINT`            | if bucket is non-AWS      | e.g. `sfo3.digitaloceanspaces.com` (no scheme) |
| `BACKUP_S3_ACCESS_KEY` / `_SECRET_KEY` | if bucket set      | Credentials Kopia uses to access the bucket    |
| `KOPIA_PASSWORD`                | if bucket set             | Kopia repository password (encrypts backups)   |
| `BACKUP_KEEP_DAILY`             | optional                  | Kopia retention — defaults to 7                |
| `BACKUP_KEEP_WEEKLY`            | optional                  | Kopia retention — defaults to 4                |
| `BACKUP_KEEP_MONTHLY`           | optional                  | Kopia retention — defaults to 6                |

A `scripts/sandbox.env.example` file shows exactly the values the internal
`game.holomush.dev` deploy uses (with secrets redacted). The sandbox's
`deploy-sandbox` workflow reads these from GitHub Secrets and hands them to
the cloud-init on first boot; re-provisioning the sandbox is a one-command
`doctl compute droplet create --user-data-file scripts/cloud-init.sh ...`
that dogfoods the recipe self-hosters use.

## Deploy Flow

**First boot (one-time or when replacing the droplet):** the DO droplet is
created with the cloud-init `user_data` described above. Cloud-init installs
everything, renders `.env`, and brings the stack up. No SSH required for a
green-field deploy.

**Ongoing deploys — trigger:** push of a release tag (`v*`) or
`workflow_dispatch`.

A new job `deploy-sandbox` in `.github/workflows/release.yaml`, wired after
`verify-release`, SHOULD:

1. Take a pre-deploy Postgres safety snapshot via
   `docker compose exec postgres pg_dump | gzip | aws s3 cp - s3://holomush-sandbox-backups/pre-deploy/<tag>.sql.gz`
2. `docker compose pull core gateway` (plus `cloudflared` when on the tunnel
   profile, or `caddy` on the caddy profile)
3. `docker compose up -d --no-recreate postgres` (leave DB alone if healthy)
4. `docker compose run --rm core migrate`
5. `docker compose up -d`
6. Probe `docker compose exec gateway curl -sf http://localhost:8080/healthz`
7. On failure: fail the workflow. Rollback is manual — re-run with previous
   tag as input.

## Backups

| Requirement          | Description                                                                                                                |
| -------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| **MUST** encrypt     | Backups are encrypted client-side by Kopia before upload. Operators who can read the Spaces bucket cannot read the backups |
| **MUST** run nightly | `pg_dump` at 03:00 UTC via a `backup` compose service (alpine + cron + kopia)                                              |
| **MUST** push to     | A Kopia repository rooted at `s3://holomush-sandbox-backups/` (managed by Kopia; structure is internal)                    |
| **MUST** retain      | Kopia policy: 7 daily, 4 weekly, 6 monthly. Pre-deploy snapshots are pinned (never auto-expired)                           |
| **MUST** pre-deploy  | A pinned safety snapshot before each release, tagged with the release tag                                                  |
| **MUST NOT** bake    | The Kopia repository password (`KOPIA_PASSWORD`) into the image; rendered into `.env` at cloud-init from GitHub Secrets    |
| **SHOULD** document  | Restore procedure in `site/docs/operating/sandbox-restore.md`; restores remain manual                                      |

Kopia choice rationale: purpose-built for backup workloads, client-side
encryption with AES-256 or ChaCha20, content-addressable deduplication across
snapshots (useful because our event-sourced data grows monotonically, so
older snapshots share blocks with newer ones), zstd compression, and built-in
integrity verification. The alternative — `pg_dump | openssl enc | aws s3 cp`
— would give us encryption but lose retention policies, dedup, compression,
and integrity checks. Kopia is one extra tool; the other approach is three
extra responsibilities we'd hand-roll.

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

1. **`compose.prod.yaml`** (modified) — add `tunnel` and `backups` profiles.
   Existing services and default behaviour unchanged.
2. **`scripts/cloud-init.sh`** (modified) — add ingress-profile selection
   and backup wiring. Existing Caddy-default path unchanged.
3. **`scripts/sandbox.env.example`** — documented example of the sandbox's
   own user-data values (with secrets redacted).
4. **`deploy/doctl/firewall-sandbox.json`** — DO cloud firewall definition
   applied via workflow.
5. **`deploy/cloudflared/config.yml.tmpl`** — tunnel config rendered from
   env at container start (used by the `tunnel` profile). Caddy config is
   already handled inline in `compose.prod.yaml` and does not need a
   separate template.
6. **`docker/postgres-backup/`** — new alpine-based image (Dockerfile +
   `backup.sh` + Kopia policy bootstrap) for the `backup` service. Built
   inline via the compose `build:` directive on the deploy host. Contains
   `kopia`, `postgresql-client` (for `pg_dump`), and `dcron`. Graduate to
   a goreleaser-published `ghcr.io/holomush/postgres-backup` image if
   self-host adoption creates demand.
7. **GitHub Secrets to add:**
   - `DIGITALOCEAN_ACCESS_TOKEN`
   - `DIGITALOCEAN_SSH_KEY_ID`
   - `DIGITALOCEAN_SSH_PRIVATE_KEY`
   - `CLOUDFLARE_API_TOKEN` (scope: DNS Edit, Tunnel management)
   - `CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_ZONE_ID`
   - `CLOUDFLARE_TUNNEL_ID`, `CLOUDFLARE_TUNNEL_TOKEN`
   - `DO_SPACES_ACCESS_KEY`, `DO_SPACES_SECRET_KEY`
   - `KOPIA_SANDBOX_PASSWORD` (encrypts the sandbox's Kopia repository;
     rotating it requires creating a new repository and discarding older
     snapshots — document in the runbook)
   - `HOLOMUSH_SANDBOX_POSTGRES_PASSWORD`
8. **Runbook** `site/docs/operating/sandbox-operations.md` — SSH, logs,
   secret rotation, tunnel recreation.
9. **Restore runbook** `site/docs/operating/sandbox-restore.md` — pull
   backup from Spaces, restore into fresh Postgres.
10. **`site/docs/operating/deployment.md`** (modified) — existing page is
    the self-hosting guide. Add a "Cloudflare Tunnel ingress" section
    showing the `HOLOMUSH_INGRESS=tunnel` variant; add a "Automated
    backups" section showing the `BACKUP_S3_*` variant. No new
    self-hosting page.

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

1. **First-boot reproducibility (sandbox).** Destroy the sandbox droplet.
   Create a new one from scratch with the documented `doctl compute droplet
   create --user-data-file deploy/cloud-init/holomush.yaml ...` invocation
   and the sandbox's env values. Confirm the web UI and telnet both come up
   without any SSH or manual steps.
2. **Self-hosting walkthrough (caddy profile).** Follow
   `site/docs/operating/self-hosting.md` end to end on a throwaway
   non-Cloudflare domain. Confirm Caddy obtains a Let's Encrypt cert and the
   web UI serves at `https://<domain>`.
3. **Deploy on tag:** tag a release. Observe `deploy-sandbox` run. Verify
   the pre-deploy backup lands in Spaces, migrations run,
   `https://game.holomush.dev` serves the web UI via the tunnel, and
   `telnet telnet.game.holomush.dev 4201` connects and can create a guest.
4. **Backup restore dry-run:** on a second throwaway droplet, pull the
   latest backup from Spaces and restore into a fresh Postgres. Document the
   commands and output in the restore runbook.
5. **Tunnel failure:** stop the `cloudflared` container. Confirm Cloudflare
   serves a 530/1033 rather than routing to a broken origin. Restart and
   confirm recovery.
6. **Gateway failure:** break `gateway` health. Confirm `cloudflared` logs
   show the health-probe failure and the edge serves a 521/522.
7. **Firewall posture (sandbox):** from an off-allowlist IP, confirm 22/tcp
   is rejected; confirm 80/443 are closed (tunnel-only); confirm 4201/tcp
   is open.

## Critical Files

### New

| Path                                              | Purpose                                                |
| ------------------------------------------------- | ------------------------------------------------------ |
| `deploy/doctl/firewall-sandbox.json`              | DO firewall definition for the sandbox droplet         |
| `deploy/cloudflared/config.yml.tmpl`              | Tunnel config template (used by `tunnel` profile)      |
| `docker/postgres-backup/Dockerfile`               | Alpine + awscli + cron image for nightly backups       |
| `docker/postgres-backup/backup.sh`                | pg_dump + S3 upload + retention script                 |
| `scripts/sandbox.env.example`                     | Sandbox's own env values (secrets redacted)            |
| `site/docs/operating/sandbox-operations.md`       | Sandbox runbook                                        |
| `site/docs/operating/sandbox-restore.md`          | DB restore runbook                                     |

### Modified

| Path                                     | Purpose                                                        |
| ---------------------------------------- | -------------------------------------------------------------- |
| `compose.prod.yaml`                      | Add `tunnel` and `backups` profiles; existing default unchanged|
| `scripts/cloud-init.sh`                  | Add ingress-profile selection + backup env wiring              |
| `.github/workflows/release.yaml`         | Add `deploy-sandbox` job wired after `verify-release`          |
| `site/docs/operating/deployment.md`      | Tunnel + backups sections; link to sandbox runbooks            |
| `CLAUDE.md`                              | One-line link to sandbox runbooks                              |

## Out-of-Scope Follow-Ups

Track in beads if pursued:

- Per-PR ephemeral sandboxes (revisit only if CI's `compose.e2e.cover.yaml`
  proves insufficient in practice)
- Packer-built cloud images per cloud (DO/AWS/GCE/Azure) for sub-30s
  provisioning — add when release cadence stabilizes
- DO Marketplace 1-click app listing — a 1.0-graduation milestone item
- Multi-region or HA for the long-running sandbox
- Blue/green or canary deploys
- Automated rollback on deploy failure
- Observability stack (OTel collector, Grafana)
- Off-box Postgres via DO Managed PG
- Basic uptime monitoring / alerting (UptimeRobot, BetterUptime) — decide
  separately from observability
- Windows / non-Ubuntu host support for the cloud-init recipe
