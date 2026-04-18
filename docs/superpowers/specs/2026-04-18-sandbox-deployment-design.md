<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Sandbox Deployment Design

## Overview

HoloMUSH ships signed container images to `ghcr.io/holomush/holomush:<version>`
via GoReleaser and provides `compose.prod.yaml` as an exemplary single-host
deployment. What is missing is a **deployed target** for two distinct sandbox
needs:

1. **A long-running public demo** at `game.holomush.dev` — a stable URL anyone
   can point a contributor or curious player at. Data is preserved across
   deploys and backed up.
2. **An ephemeral per-PR smoke-test sandbox** — internal audience only,
   provisioned on PR open and destroyed on PR close. Verifies each PR produces
   a deployable artifact that runs end to end (boot, migrate, authenticate,
   telnet + web).

Hosting is **DigitalOcean**. Cloudflare fronts the web path for DNS, TLS, WAF,
and origin protection via Cloudflare Tunnel. Telnet traffic reaches droplets
directly because Cloudflare's free/paid tiers do not proxy arbitrary TCP.

The design objective is a **single deployment recipe** that both environments
share, differing only in droplet size, persistence, DNS, and lifecycle. No
bespoke tooling; everything is GitHub Actions + `doctl` + `cloudflared` +
a sandbox-specific compose file derived from `compose.prod.yaml`.

## Goals

- Automatic deployment of the long-running sandbox on release-tag push
- Automatic per-PR smoke sandbox with lifecycle tied to PR state
- Protocol parity with production: both telnet and web exercised
- Fast PR-open → sandbox-ready time (target: under 90 seconds)
- Preserve long-running sandbox data across deploys with recoverable backups
- Zero-cost observability acceptable (stdout logs) for sandbox scale
- Monthly cost under $30 at expected traffic

## Non-Goals

- Multi-region or HA deployment (single droplet per environment is sufficient)
- Blue/green or canary deployment on the long-running environment
- Automated rollback (manual re-run with previous tag is acceptable)
- Full observability stack (OTel collector, Grafana) — tracked separately
- Off-box Postgres (DO Managed PG) — graduate later if single droplet is
  insufficient
- Per-contributor preview environments (`user-<handle>.smoke.holomush.dev`) —
  per-PR coverage is sufficient
- Replacement for local CI. `task pr-prep` remains the PR gate; the smoke
  sandbox is additive

## Hosting Decision

DigitalOcean was chosen over Cloudflare Containers because:

| Requirement                      | DigitalOcean | Cloudflare Containers |
| -------------------------------- | ------------ | --------------------- |
| Raw TCP ingress (telnet:4201)    | Yes          | **No** (HTTP only)    |
| Persistent volumes for Postgres  | Yes          | **No** (snapshots TBD)|
| Long-lived services (not sleep)  | Yes          | Scale-to-zero default |
| Mapping to existing compose file | Direct       | Requires Worker layer |

Cloudflare's role is limited to DNS, TLS termination at the edge, WAF, and
Cloudflare Tunnel for origin protection of the web path. Cloudflare Hyperdrive
is out of scope because the compute tier is not Cloudflare Workers.

## Architecture

### Shared contract

Both environments run `compose.sandbox.yaml` (new file, derived from
`compose.prod.yaml`) with two profiles:

- `long-running` — includes `cloudflared` + `backup` services
- `smoke` — includes `cloudflared` (ephemeral tunnel per PR); omits `backup`

Differences are expressed entirely through environment variables, compose
profiles, and DNS wiring, not through forked compose files.

**Key deviations from `compose.prod.yaml`:**

- **Caddy is removed.** Both profiles rely on `cloudflared` for web ingress,
  eliminating Let's Encrypt complexity and avoiding public ports 80/443 on the
  droplet.
- **OTel collector is dropped** from the default profile. Sandbox uses
  container stdout and `docker logs`.
- **Environment-driven image tag.** `HOLOMUSH_VERSION` defaults to the
  release tag for long-running and to `sha-<shortsha>` for smoke PRs.

### Ingress model

| Path   | Long-running                                            | Smoke                                                          |
| ------ | ------------------------------------------------------- | -------------------------------------------------------------- |
| Web    | CF Tunnel → `gateway:8080` over compose network         | Ephemeral CF Tunnel (per PR) → `gateway:8080`, gated by Access |
| Telnet | Public A record `telnet.game.holomush.dev` → droplet IP | Droplet public IP; firewall allows only GH Action runner CIDRs |
| SSH    | Key-only, allowlisted CIDRs                             | Key-only, allowlisted CIDRs                                    |

**Rationale for using `cloudflared` in smoke too:** a smoke droplet with a
public HTTP port would allow bypassing Cloudflare Access by connecting
directly to the droplet IP. Running an ephemeral tunnel per PR closes that
bypass without requiring IP allowlisting of the entire CF edge range.

### Topology

```text
                    ┌──────────────────────────────────────────────┐
                    │              Cloudflare DNS                  │
                    │                                              │
                    │  game.holomush.dev        ← proxied          │
                    │  telnet.game.holomush.dev ← DNS-only         │
                    │  pr-<N>.smoke.holomush.dev ← CF Access       │
                    └────────────────┬─────────────────────────────┘
                                     │
              ┌──────────────────────┴──────────────────────┐
              │                                             │
    ┌─────────▼──────────┐                   ┌──────────────▼───────────┐
    │  long-running      │                   │  ephemeral smoke-test    │
    │  Droplet           │                   │  Droplet (per PR, from   │
    │  (s-2vcpu-2gb-amd) │                   │   golden snapshot)       │
    │                    │                   │                          │
    │  cloudflared ──────┤── Tunnel ─────── gateway (HTTP 8080)         │
    │  gateway  ◀──── :4201 public ────── telnet clients                │
    │  core                                                             │
    │  postgres ── pg_dump → DO Spaces (nightly, 14d retention)         │
    │  backup (cron)                                                    │
    └────────────────────┘                   └──────────────────────────┘
```

## Long-Running Sandbox

### Resources

| Requirement        | Description                                                                              |
| ------------------ | ---------------------------------------------------------------------------------------- |
| **MUST** run       | 1 × `s-2vcpu-2gb-amd` droplet in `sfo3`                                                  |
| **MUST** attach    | 25 GB block-storage volume at `/opt/holomush/data` for Postgres                          |
| **MUST** persist   | Postgres data across droplet rebuilds via the block volume                               |
| **MUST** firewall  | Inbound: 4201/tcp (public), 22/tcp (allowlisted); deny 80/443 (tunnel-only egress)       |
| **MUST NOT** expose| Postgres port externally; DB is reachable only on the compose backend network            |

### Deploy flow

Trigger: push of a release tag (`v*`) or `workflow_dispatch`.

A new job `deploy-sandbox` in `.github/workflows/release.yaml` (wired after
`verify-release`) SHOULD:

1. Take a pre-deploy Postgres safety snapshot:
   `docker compose exec postgres pg_dump | gzip | aws s3 cp - s3://holomush-sandbox-backups/pre-deploy/<tag>.sql.gz`
2. `docker compose pull core gateway cloudflared`
3. `docker compose up -d --no-recreate postgres` (leave DB alone if healthy)
4. `docker compose run --rm core migrate`
5. `docker compose up -d core gateway cloudflared backup`
6. Probe `docker compose exec gateway curl -sf http://localhost:8080/healthz`
7. On failure: fail workflow. Rollback is manual: re-run with previous tag.

### Backups

| Requirement          | Description                                                                                |
| -------------------- | ------------------------------------------------------------------------------------------ |
| **MUST** run nightly | `pg_dump` at 03:00 UTC via a `backup` compose service (alpine + cron + awscli)             |
| **MUST** push to     | `s3://holomush-sandbox-backups/game/YYYY/MM/DD.sql.gz`                                     |
| **MUST** retain      | 14 days via a Spaces lifecycle rule                                                        |
| **MUST** pre-deploy  | A safety snapshot before each release, keyed by tag, under `pre-deploy/<tag>.sql.gz`       |
| **SHOULD** document  | Restore procedure in `site/docs/operating/sandbox-restore.md`; restores remain manual      |

### DNS (one-time Cloudflare setup)

- `game.holomush.dev` → Tunnel CNAME `<tunnel-id>.cfargotunnel.com`, proxied
- `telnet.game.holomush.dev` → droplet IPv4 A-record, DNS-only
- `www.game.holomush.dev` (optional) → CNAME to `game.holomush.dev`

The tunnel is created once via `cloudflared tunnel create holomush-sandbox`.
Its credentials JSON is stored in GitHub Secrets as
`CLOUDFLARE_TUNNEL_CREDENTIALS` and rendered to the droplet during deploy.

## Ephemeral Smoke-Test Sandbox

### Resources

| Requirement       | Description                                                                     |
| ----------------- | ------------------------------------------------------------------------------- |
| **MUST** use      | 1 × `s-1vcpu-1gb` droplet per PR (~$0.009/hour, prorated)                       |
| **MUST** boot     | From the `holomush-sandbox-base:current` golden snapshot                        |
| **MUST** tag      | Droplets with `holomush-smoke`, `pr-<N>`, `sha-<shortsha>`                      |
| **MUST** isolate  | Each PR gets its own droplet, tunnel, and DNS record — no shared state          |
| **MUST NOT** run  | A `backup` service (smoke data is throwaway)                                    |

### Golden snapshot

A scheduled workflow builds `holomush-sandbox-base` via Packer and aliases the
newest build as `:current`. The snapshot contains:

- Ubuntu 24.04 LTS
- Docker Engine + compose plugin
- Pre-pulled `ghcr.io/holomush/holomush:latest` and `postgres:18-alpine`
- `/opt/holomush/compose.sandbox.yaml` and `.env.smoke.template` placed
- `cloudflared` binary
- `awscli` (for debug/parity; smoke does not write backups)
- SSH hardened: key-only, no root, dedicated `holomush` user owns workdir

### Snapshot refresh

| Requirement          | Description                                                               |
| -------------------- | ------------------------------------------------------------------------- |
| **MUST** run weekly  | `schedule: cron "0 6 * * 1"` (Monday 06:00 UTC)                           |
| **MUST** allow       | `workflow_dispatch` for manual rebuild                                    |
| **MUST** alias       | Newest build as `holomush-sandbox-base:current`                           |
| **MUST** retain      | 21 days; older snapshots pruned by the workflow                           |

### PR lifecycle

Trigger: `pull_request` with types `opened`, `synchronize`, `reopened`,
`closed`.

**On `opened` / `synchronize` / `reopened`:**

1. `doctl compute droplet create holomush-smoke-pr-<N>` from
   `holomush-sandbox-base:current`
2. Wait for droplet active + SSH reachable (~30–60s)
3. SSH: pull PR image `ghcr.io/holomush/holomush:sha-<shortsha>` (requires new
   PR image workflow — see Prerequisites)
4. Create an ephemeral Cloudflare Tunnel `pr-<N>`; render its credentials to
   the droplet
5. `docker compose -f compose.sandbox.yaml --profile smoke up -d` with
   `HOLOMUSH_VERSION=sha-<shortsha>`, `SANDBOX_MODE=smoke`
6. Wait for gateway `/healthz` green via `docker compose exec`
7. Create CF DNS record `pr-<N>.smoke.holomush.dev` → tunnel; apply CF Access
   policy requiring the repo's GitHub org membership
8. Run smoke checks (see Smoke Coverage below)
9. Post PR comment with URL, telnet host:port, and pass/fail summary

**On `closed`:**

1. Delete CF DNS record and Access policy
2. Delete ephemeral Tunnel
3. `doctl compute droplet delete holomush-smoke-pr-<N>`

### Smoke coverage

The smoke suite MUST verify, at minimum:

- Deployment boots without error (compose reaches healthy state)
- Migrations run cleanly on an empty database
- Gateway `/healthz` returns 200 through the Cloudflare Tunnel
- A guest can register via the web UI and receive a session cookie
- A telnet client on the GH Action runner can connect to `<droplet-ip>:4201`,
  complete the welcome handshake, and create a guest

**Implementation note:** the existing `compose.e2e.cover.yaml` runs tests
locally within docker-compose. Pointing it at a remote target requires
verifying whether integration tests accept a target URL via environment
variable. If not, a simpler smoke script using `curl` + `nc` + a short
scripted telnet interaction is acceptable for v1 and SHOULD be tracked as a
follow-up to graduate to full integration coverage.

### Housekeeping

A nightly `.github/workflows/smoke-housekeeping.yaml` job MUST list all
droplets tagged `holomush-smoke` and delete any whose PR is no longer open.
This guards against missed `closed` webhooks stranding droplets.

## Prerequisites

The implementation MUST deliver these new capabilities before either
environment is usable:

1. **Per-PR image builds.** `release.yaml` only publishes on tag. A new
   workflow `.github/workflows/pr-image.yaml` MUST build and push
   `ghcr.io/holomush/holomush:sha-<shortsha>` on every PR push, with a
   14-day GHCR retention policy. Image reuses the existing `Dockerfile`.
2. **`compose.sandbox.yaml`** — new compose file with `long-running` and
   `smoke` profiles as described above.
3. **`deploy/packer/sandbox-base.pkr.hcl`** — Packer template for the golden
   snapshot.
4. **`deploy/doctl/firewall-long-running.json`** and
   **`deploy/doctl/firewall-smoke.json`** — DO cloud firewall definitions
   applied via workflow.
5. **`deploy/cloudflared/config.yml.tmpl`** — tunnel config rendered at
   deploy time.
6. **GitHub Secrets to add:**
   - `DIGITALOCEAN_ACCESS_TOKEN`
   - `DIGITALOCEAN_SSH_KEY_ID`
   - `DIGITALOCEAN_SSH_PRIVATE_KEY`
   - `CLOUDFLARE_API_TOKEN` (scope: DNS Edit, Tunnel create/delete, Access
     app create/delete)
   - `CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_ZONE_ID`
   - `CLOUDFLARE_TUNNEL_ID_GAME`, `CLOUDFLARE_TUNNEL_CREDENTIALS`
   - `DO_SPACES_ACCESS_KEY`, `DO_SPACES_SECRET_KEY`
   - `HOLOMUSH_SANDBOX_POSTGRES_PASSWORD` (per-env, rotatable)
7. **Runbook** `site/docs/operating/sandbox-operations.md` — SSH, logs,
   secret rotation, snapshot rebuild.
8. **Restore runbook** `site/docs/operating/sandbox-restore.md` — pull backup
   from Spaces, restore into fresh Postgres.

## Security Considerations

| Requirement          | Description                                                                                    |
| -------------------- | ---------------------------------------------------------------------------------------------- |
| **MUST NOT** expose  | Web ports 80/443 on droplets. Only `cloudflared` reaches gateway:8080                          |
| **MUST** rely on     | Existing gateway auth + DoS hardening for public telnet exposure (connection cap, deadlines)   |
| **MUST** gate        | Smoke URLs behind Cloudflare Access (repo-org membership policy)                               |
| **MUST** isolate     | Per-environment Postgres passwords, Spaces buckets, and tunnels                                |
| **MUST NOT** bake    | Secrets into the golden snapshot; render `.env` at first boot from GitHub Secrets              |
| **MUST** harden SSH  | Key-only, root disabled, dedicated `holomush` user (baked into snapshot)                       |
| **SHOULD** allowlist | SSH inbound to known deploy-runner egress + operator IPs                                       |
| **MUST NOT** skip    | `task pr-prep` as the PR gate; smoke sandbox is additive verification                          |

## Cost Estimate

| Line item                                       | Monthly      |
| ----------------------------------------------- | ------------ |
| Long-running droplet (`s-2vcpu-2gb-amd`)        | $18.00       |
| Long-running block storage (25 GB)              | $2.50        |
| DO Spaces (backups, <1 GB typical)              | $5.00 (base) |
| Snapshot storage (~2 × 5 GB)                    | $0.60        |
| Smoke droplets (20 PRs × 4h × $0.009/hr)        | $0.72        |
| Cloudflare Tunnel / DNS / Access (free ≤ 50)    | $0.00        |
| **Total**                                       | **~$27**     |

First-order costs only. Egress overages, orphaned smoke droplets, and Spaces
API requests are negligible at this scale.

## Verification

Each component ships with an explicit verification step:

1. **Snapshot build end-to-end:** dispatch `build-sandbox-snapshot.yaml`,
   observe Packer output, verify `doctl snapshot list` shows both the dated
   tag and the `:current` alias.
2. **Smoke PR lifecycle:** open a throwaway PR against a test branch. Verify:
   droplet exists, DNS resolves, `pr-<N>.smoke.holomush.dev` returns the web
   UI behind CF Access, telnet connects from the GH Action runner, smoke
   script reports green, PR comment posted. Close the PR and confirm
   droplet + DNS + tunnel are gone within two minutes.
3. **Long-running deploy:** tag a release; observe `deploy-sandbox` job.
   Verify pre-deploy backup appears in Spaces, migrations run,
   `https://game.holomush.dev` serves the web UI, and
   `telnet telnet.game.holomush.dev 4201` connects and can create a guest.
4. **Backup restore dry-run:** on a second droplet, pull the latest backup
   from Spaces and restore into a fresh Postgres. Document the commands and
   output in the restore runbook.
5. **Housekeeping:** force a smoke droplet to orphan state by skipping the
   `closed` branch and confirm the nightly job deletes it.
6. **Failure injection:** break `gateway` health on long-running. Confirm the
   tunnel serves a 521/522 rather than routing to a broken origin, and that
   `cloudflared` logs show the health-probe failure.

## Critical Files

### New

| Path                                              | Purpose                                       |
| ------------------------------------------------- | --------------------------------------------- |
| `compose.sandbox.yaml`                            | Shared stack with profiles                    |
| `deploy/packer/sandbox-base.pkr.hcl`              | Golden snapshot builder                       |
| `deploy/doctl/firewall-long-running.json`         | DO firewall definition                        |
| `deploy/doctl/firewall-smoke.json`                | DO firewall definition                        |
| `deploy/cloudflared/config.yml.tmpl`              | Tunnel config template                        |
| `.github/workflows/pr-image.yaml`                 | Publish `sha-<shortsha>` images               |
| `.github/workflows/smoke-sandbox.yaml`            | Per-PR lifecycle                              |
| `.github/workflows/build-sandbox-snapshot.yaml`   | Snapshot build (cron + manual)                |
| `.github/workflows/smoke-housekeeping.yaml`       | Orphan cleanup (nightly)                      |
| `site/docs/operating/sandbox-operations.md`       | Runbook                                       |
| `site/docs/operating/sandbox-restore.md`          | DB restore runbook                            |

### Modified

| Path                                     | Purpose                                                        |
| ---------------------------------------- | -------------------------------------------------------------- |
| `.github/workflows/release.yaml`         | Add `deploy-sandbox` job wired after `verify-release`          |
| `site/docs/operating/deployment.md`      | Add sandbox architecture section + link to runbooks            |
| `CLAUDE.md`                              | One-line link to sandbox runbooks                              |

## Out-of-Scope Follow-Ups

Track in beads if pursued:

- Multi-region or HA for the long-running sandbox
- Blue/green or canary deploys
- Automated rollback on deploy failure
- Observability stack (OTel collector, Grafana) across sandbox + production
- Off-box Postgres via DO Managed PG
- Per-contributor preview environments
- Graduating the smoke script to full `compose.e2e.cover.yaml` integration
  coverage against a remote target
