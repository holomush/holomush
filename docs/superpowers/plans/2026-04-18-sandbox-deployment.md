<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Sandbox Deployment — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up a long-running HoloMUSH sandbox at `game.holomush.dev` on
a single DigitalOcean droplet, using extended versions of the existing
`compose.prod.yaml` and `scripts/cloud-init.sh` so the same artifacts serve
self-hosters. Add nightly Postgres backups to DO Spaces, Cloudflare Tunnel as
an alternate ingress, and a `deploy-sandbox` workflow job that deploys on
release-tag push.

**Architecture:** Extend `compose.prod.yaml` in place with two compose
profiles — `tunnel` (cloudflared) and `backups` (alpine + awscli + cron +
pg_dump to S3-compatible storage). Extend `scripts/cloud-init.sh` to read
`HOLOMUSH_INGRESS` and backup env vars, write them to `.env`, and auto-start
compose with the right profiles. All wired through a new GitHub Actions
`deploy-sandbox` job that runs after `verify-release`.

**Tech Stack:** Docker Compose v2, Cloudflare Tunnel (`cloudflared`), DO
Spaces (S3-compatible), awscli, cron, bash, GitHub Actions, GoReleaser
(existing pipeline; no changes).

**Spec:** `docs/superpowers/specs/2026-04-18-sandbox-deployment-design.md`

**Beads Epic:** to be created by the executing session before Task 1 (`bd
create --type=epic --title="Long-running sandbox deployment at game.holomush.dev"`).
Each task below becomes a child beads issue via `--parent`.

---

## File Structure

### New Files

| File                                        | Responsibility                                                                                  |
| ------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| `docker/postgres-backup/Dockerfile`         | Alpine base + awscli + dcron + copy of `backup.sh`                                              |
| `docker/postgres-backup/backup.sh`          | pg_dump → gzip → `aws s3 cp` → retention prune                                                  |
| `deploy/cloudflared/config.yml.tmpl`        | Tunnel ingress config (reads env-rendered tunnel ID + hostname)                                 |
| `deploy/doctl/firewall-sandbox.json`        | DO cloud firewall: SSH allowlisted, 4201/tcp public, 80/443 closed (tunnel-only)                |
| `scripts/sandbox.env.example`               | Reference `.env` for the sandbox droplet (secrets redacted)                                     |
| `site/docs/operating/sandbox-operations.md` | Runbook: SSH, logs, secret rotation, tunnel recreation, manual deploy                           |
| `site/docs/operating/sandbox-restore.md`    | Runbook: pull backup from Spaces, restore into fresh Postgres                                   |

### Modified Files

| File                                     | Change                                                                                         |
| ---------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `compose.prod.yaml`                      | Add `cloudflared` service under `tunnel` profile; add `backup` service under `backups` profile |
| `scripts/cloud-init.sh`                  | Read `HOLOMUSH_INGRESS`; render tunnel+backup env into `.env`; adjust UFW; auto-start compose  |
| `.github/workflows/release.yaml`         | Add `deploy-sandbox` job after `verify-release`                                                |
| `site/docs/operating/deployment.md`      | Add "Cloudflare Tunnel ingress" and "Automated backups" sections                               |
| `CLAUDE.md`                              | One-line link to the two new sandbox runbooks                                                  |

### Unchanged (Reference)

| File                        | Why Referenced                                                 |
| --------------------------- | -------------------------------------------------------------- |
| `.goreleaser.yaml`          | Image build/sign for `ghcr.io/holomush/holomush` — unchanged   |
| `Dockerfile`                | Runtime image layout — unchanged                               |
| `compose.yaml`              | Local dev compose — unchanged                                  |

---

## Chunk 1: Backup Image

### Task 1: Postgres-Backup Container Image

**Files:**

- Create: `docker/postgres-backup/Dockerfile`
- Create: `docker/postgres-backup/backup.sh`

#### Step 1.1: Write `backup.sh`

- [ ] Create `docker/postgres-backup/backup.sh` with the following content. This
  script is the cron job body: dumps Postgres, gzips, uploads to S3, prunes
  older than retention-days.

```bash
#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Nightly Postgres backup: pg_dump → gzip → S3 upload → retention prune.
# Invoked by cron; run manually for a one-off backup via docker compose exec.

set -eu

: "${POSTGRES_HOST:?must be set}"
: "${POSTGRES_USER:?must be set}"
: "${POSTGRES_DB:?must be set}"
: "${PGPASSWORD:?must be set}"
: "${BACKUP_S3_BUCKET:?must be set}"
: "${BACKUP_S3_PREFIX:=game}"
: "${BACKUP_RETENTION_DAYS:=14}"

date_iso=$(date -u +%Y-%m-%dT%H-%M-%SZ)
s3_key="${BACKUP_S3_PREFIX}/$(date -u +%Y/%m/%d)/${date_iso}.sql.gz"

endpoint_flag=""
if [ -n "${BACKUP_S3_ENDPOINT_URL:-}" ]; then
  endpoint_flag="--endpoint-url ${BACKUP_S3_ENDPOINT_URL}"
fi

echo "[backup] $(date -u +%FT%TZ) starting pg_dump → s3://${BACKUP_S3_BUCKET}/${s3_key}"
# shellcheck disable=SC2086
pg_dump -h "${POSTGRES_HOST}" -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" \
  | gzip \
  | aws s3 cp ${endpoint_flag} - "s3://${BACKUP_S3_BUCKET}/${s3_key}" \
    --content-type application/gzip

echo "[backup] $(date -u +%FT%TZ) pruning objects older than ${BACKUP_RETENTION_DAYS} days"
cutoff_epoch=$(( $(date -u +%s) - BACKUP_RETENTION_DAYS * 86400 ))
# shellcheck disable=SC2086
aws s3 ls ${endpoint_flag} "s3://${BACKUP_S3_BUCKET}/${BACKUP_S3_PREFIX}/" --recursive \
  | while read -r line; do
      ts=$(echo "${line}" | awk '{print $1" "$2}')
      key=$(echo "${line}" | awk '{for(i=4;i<=NF;i++) printf "%s%s", $i, (i==NF?"\n":" ")}')
      obj_epoch=$(date -u -d "${ts}" +%s 2>/dev/null || date -u -j -f '%Y-%m-%d %H:%M:%S' "${ts}" +%s)
      if [ "${obj_epoch}" -lt "${cutoff_epoch}" ]; then
        echo "[backup] prune ${key}"
        # shellcheck disable=SC2086
        aws s3 rm ${endpoint_flag} "s3://${BACKUP_S3_BUCKET}/${key}"
      fi
    done
echo "[backup] $(date -u +%FT%TZ) done"
```

- [ ] Make it executable: `chmod +x docker/postgres-backup/backup.sh`.

#### Step 1.2: Write the Dockerfile

- [ ] Create `docker/postgres-backup/Dockerfile`:

```dockerfile
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Nightly Postgres backup image. Built inline by compose.prod.yaml under
# the `backups` profile. Runs crond in the foreground; cron invokes
# /usr/local/bin/backup.sh at 03:00 UTC.

FROM alpine:3.23

RUN apk add --no-cache aws-cli dcron postgresql18-client coreutils tzdata \
    && adduser -D -g '' backup

COPY backup.sh /usr/local/bin/backup.sh
RUN chmod +x /usr/local/bin/backup.sh

# 03:00 UTC nightly. Crond passes env through the shell for env-aware scripts.
RUN mkdir -p /etc/crontabs \
    && printf '%s\n' '0 3 * * * /usr/local/bin/backup.sh >> /var/log/cron.log 2>&1' \
       > /etc/crontabs/root \
    && touch /var/log/cron.log

ENTRYPOINT ["/usr/sbin/crond", "-f", "-l", "2", "-L", "/dev/stdout"]
```

#### Step 1.3: Validate the image builds

- [ ] Build the image locally from the worktree root:

```bash
docker build -t holomush-postgres-backup:test docker/postgres-backup/
```

Expected: build succeeds, image tagged.

- [ ] Smoke-run the image with env to confirm the script exits on missing vars:

```bash
docker run --rm holomush-postgres-backup:test /usr/local/bin/backup.sh
```

Expected: exits non-zero with `POSTGRES_HOST: must be set`.

- [ ] `shellcheck` the script:

```bash
shellcheck docker/postgres-backup/backup.sh
```

Expected: no warnings (or only the explicitly-disabled SC2086 noted in the
file).

#### Step 1.4: Commit

- [ ] `git add docker/postgres-backup/ && git commit -m "feat(backup): alpine+awscli+cron image for nightly Postgres backups"`

---

## Chunk 2: Compose Profiles

### Task 2: `tunnel` Ingress Profile on compose.prod.yaml

**Files:**

- Modify: `compose.prod.yaml`

The existing services (`postgres`, `core`, `gateway`, `caddy`,
`otel-collector`) stay. Add one new service under a `tunnel` profile and note
that `caddy` should be in an implicit-default profile so operators can
opt-out for tunnel ingress. Docker Compose's default behaviour is that
profile-less services always start; to make `caddy` mutually exclusive with
`tunnel`, gate `caddy` behind its own `caddy` profile (default). The
cloud-init decides which profile to pass.

#### Step 2.1: Gate `caddy` under a profile

- [ ] In `compose.prod.yaml`, add `profiles: ["caddy"]` to the `caddy`
  service block. Run `task fmt` afterwards to keep formatting stable.

- [ ] Verify the caddy service still parses:

```bash
docker compose -f compose.prod.yaml --profile caddy config | grep -E "^  caddy:"
```

Expected: line appears.

- [ ] Verify default invocation no longer brings up caddy:

```bash
docker compose -f compose.prod.yaml config --services
```

Expected: `postgres`, `core`, `gateway` listed; `caddy` NOT listed.

#### Step 2.2: Add the `cloudflared` service

- [ ] Append to `compose.prod.yaml`:

```yaml
  cloudflared:
    image: cloudflare/cloudflared:latest
    restart: unless-stopped
    profiles: [tunnel]
    command:
      - tunnel
      - --no-autoupdate
      - run
      - --token
      - ${CLOUDFLARE_TUNNEL_TOKEN:?Set CLOUDFLARE_TUNNEL_TOKEN in .env}
    networks:
      - frontend
    depends_on:
      gateway:
        condition: service_started
```

Notes for the implementer:

- `cloudflared` uses token-based tunnels. This means the operator creates
  the tunnel once via the Cloudflare dashboard or API, copies the token
  into `.env`, and cloudflared auto-registers ingress rules based on the
  dashboard config for that tunnel ID. No local config.yml needed at this
  tier — the template in `deploy/cloudflared/config.yml.tmpl` (Task 3) is
  only used if the operator wants config-file based tunnels instead of
  tokens. Document both in the runbook.
- `depends_on: gateway:service_started` is sufficient; cloudflared retries
  internally until the origin is healthy.

#### Step 2.3: Validate

- [ ] `docker compose -f compose.prod.yaml --profile tunnel config` — ensure
  the rendered config includes `cloudflared` and does NOT include `caddy`.

- [ ] Lint yaml: `task lint` (yamlfmt + actionlint + rumdl). Expected: pass.

#### Step 2.4: Commit

- [ ] `git add compose.prod.yaml && git commit -m "feat(sandbox): cloudflared tunnel ingress profile"`

---

### Task 3: `backups` Profile on compose.prod.yaml

**Files:**

- Modify: `compose.prod.yaml`

#### Step 3.1: Add the `backup` service

- [ ] Append to `compose.prod.yaml` below `cloudflared`:

```yaml
  backup:
    build: ./docker/postgres-backup
    restart: unless-stopped
    profiles: [backups]
    environment:
      POSTGRES_HOST: postgres
      POSTGRES_USER: holomush
      POSTGRES_DB: holomush
      PGPASSWORD: ${POSTGRES_PASSWORD}
      BACKUP_S3_BUCKET: ${BACKUP_S3_BUCKET:?Set BACKUP_S3_BUCKET in .env}
      BACKUP_S3_ENDPOINT_URL: ${BACKUP_S3_ENDPOINT_URL:-}
      BACKUP_S3_PREFIX: ${BACKUP_S3_PREFIX:-game}
      BACKUP_RETENTION_DAYS: ${BACKUP_RETENTION_DAYS:-14}
      AWS_ACCESS_KEY_ID: ${BACKUP_S3_ACCESS_KEY:?Set BACKUP_S3_ACCESS_KEY in .env}
      AWS_SECRET_ACCESS_KEY: ${BACKUP_S3_SECRET_KEY:?Set BACKUP_S3_SECRET_KEY in .env}
      AWS_DEFAULT_REGION: ${BACKUP_S3_REGION:-us-east-1}
    networks:
      - backend
    depends_on:
      postgres:
        condition: service_healthy
```

#### Step 3.2: Validate

- [ ] With a populated `.env`, render the backups-profile config:

```bash
cat > /tmp/sandbox-env <<'EOF'
POSTGRES_PASSWORD=dummy
HOLOMUSH_VERSION=v0.0.0
DOMAIN=example.com
BACKUP_S3_BUCKET=holomush-test
BACKUP_S3_ACCESS_KEY=dummy
BACKUP_S3_SECRET_KEY=dummy
EOF
docker compose -f compose.prod.yaml --profile backups --env-file /tmp/sandbox-env config | grep -E "^  backup:"
```

Expected: `backup:` line appears.

- [ ] Confirm the default invocation still does NOT include `backup`:

```bash
docker compose -f compose.prod.yaml --env-file /tmp/sandbox-env config --services
```

Expected: no `backup`.

#### Step 3.3: Commit

- [ ] `git add compose.prod.yaml && git commit -m "feat(sandbox): nightly Postgres backup profile (backups)"`

---

## Chunk 3: Cloudflared + Firewall Artifacts

### Task 4: Cloudflared Config Template

**Files:**

- Create: `deploy/cloudflared/config.yml.tmpl`

This template is an alternative to token-based tunnels for operators who
prefer declarative config. Not used by default (sandbox uses tokens).
Referenced by the runbook.

#### Step 4.1: Write the template

- [ ] Create `deploy/cloudflared/config.yml.tmpl`:

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Cloudflared config-file template. Rendered by an operator who prefers
# config-based tunnels over tokens. Substitute {{ TUNNEL_ID }},
# {{ DOMAIN }}, and {{ CREDENTIALS_PATH }} with real values, then mount
# this file at /etc/cloudflared/config.yml in the cloudflared container.

tunnel: "{{ TUNNEL_ID }}"
credentials-file: "{{ CREDENTIALS_PATH }}"

ingress:
  - hostname: "{{ DOMAIN }}"
    service: http://gateway:8080
  - service: http_status:404
```

#### Step 4.2: Validate YAML

- [ ] `yamlfmt -lint deploy/cloudflared/config.yml.tmpl`. Expected: no output
  (lint pass).

#### Step 4.3: Commit

- [ ] `git add deploy/cloudflared/config.yml.tmpl && git commit -m "feat(sandbox): optional cloudflared config-file template"`

---

### Task 5: DO Cloud Firewall Definition

**Files:**

- Create: `deploy/doctl/firewall-sandbox.json`

Firewall is applied via `doctl compute firewall create --inbound-rules @...`
during one-time bootstrap and is re-applied on droplet rebuild.

#### Step 5.1: Write the JSON

- [ ] Create `deploy/doctl/firewall-sandbox.json`:

```json
{
  "name": "holomush-sandbox",
  "inbound_rules": [
    {
      "protocol": "tcp",
      "ports": "22",
      "sources": { "addresses": ["0.0.0.0/0"] },
      "comment": "SSH - narrow in runbook before production use"
    },
    {
      "protocol": "tcp",
      "ports": "4201",
      "sources": { "addresses": ["0.0.0.0/0"] },
      "comment": "Public telnet"
    }
  ],
  "outbound_rules": [
    {
      "protocol": "tcp",
      "ports": "0",
      "destinations": { "addresses": ["0.0.0.0/0", "::/0"] }
    },
    {
      "protocol": "udp",
      "ports": "0",
      "destinations": { "addresses": ["0.0.0.0/0", "::/0"] }
    },
    {
      "protocol": "icmp",
      "destinations": { "addresses": ["0.0.0.0/0", "::/0"] }
    }
  ],
  "tags": ["holomush-sandbox"]
}
```

Notes:

- Ports 80/443 are deliberately absent — `cloudflared` makes outbound
  connections to the Cloudflare edge; no inbound HTTP is needed.
- The `22/tcp` rule is 0.0.0.0/0 in this file for first-boot convenience.
  The runbook instructs the operator to narrow it to their static IP +
  GitHub Actions runner egress range as a post-bootstrap step.

#### Step 5.2: Validate JSON

- [ ] `jq . deploy/doctl/firewall-sandbox.json > /dev/null` — expected:
  silent success.

#### Step 5.3: Commit

- [ ] `git add deploy/doctl/firewall-sandbox.json && git commit -m "feat(sandbox): DO cloud firewall definition for sandbox droplet"`

---

## Chunk 4: Cloud-Init Extension

### Task 6: Extend `scripts/cloud-init.sh`

**Files:**

- Modify: `scripts/cloud-init.sh`

The existing 184-line script handles Docker install, `holomush` user
creation, `/opt/holomush/` directory layout, random Postgres password
generation, UFW firewall, and idempotency. Add: (a) `HOLOMUSH_INGRESS`
selection, (b) backup env vars, (c) profile-aware compose startup, (d) UFW
rule conditional on ingress mode.

#### Step 6.1: Add env-var intake at the top

- [ ] In `scripts/cloud-init.sh`, after the `HOLOMUSH_VERSION=` line (~line
  16), add the following variables (all optional, defaults match existing
  behaviour):

```bash
# Ingress mode: "caddy" (default, public 80/443 with Let's Encrypt) or
# "tunnel" (cloudflared, no public HTTP ports).
HOLOMUSH_INGRESS="${HOLOMUSH_INGRESS:-caddy}"

# Optional: automated nightly backups to S3-compatible storage.
# When BACKUP_S3_BUCKET is set, the `backups` profile is enabled.
BACKUP_S3_BUCKET="${BACKUP_S3_BUCKET:-}"
BACKUP_S3_ENDPOINT_URL="${BACKUP_S3_ENDPOINT_URL:-}"
BACKUP_S3_ACCESS_KEY="${BACKUP_S3_ACCESS_KEY:-}"
BACKUP_S3_SECRET_KEY="${BACKUP_S3_SECRET_KEY:-}"
BACKUP_S3_PREFIX="${BACKUP_S3_PREFIX:-game}"
BACKUP_RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-14}"

# Tunnel-mode only.
CLOUDFLARE_TUNNEL_TOKEN="${CLOUDFLARE_TUNNEL_TOKEN:-}"

# Caddy-mode only.
LETSENCRYPT_EMAIL="${LETSENCRYPT_EMAIL:-}"

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
```

#### Step 6.2: Extend `.env` generation

- [ ] Replace the `cat > "${HOLOMUSH_DIR}/.env"` block (~lines 62–84) with:

```bash
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

if [ -n "${BACKUP_S3_BUCKET}" ]; then
  cat >> "${HOLOMUSH_DIR}/.env" <<EOF

# Automated nightly backups.
BACKUP_S3_BUCKET=${BACKUP_S3_BUCKET}
BACKUP_S3_ENDPOINT_URL=${BACKUP_S3_ENDPOINT_URL}
BACKUP_S3_ACCESS_KEY=${BACKUP_S3_ACCESS_KEY}
BACKUP_S3_SECRET_KEY=${BACKUP_S3_SECRET_KEY}
BACKUP_S3_PREFIX=${BACKUP_S3_PREFIX}
BACKUP_RETENTION_DAYS=${BACKUP_RETENTION_DAYS}
EOF
fi

# Data paths (commented defaults for reference)
cat >> "${HOLOMUSH_DIR}/.env" <<'EOF'

# DATA_DIR=/opt/holomush/data
# CONFIG_DIR=/opt/holomush/config
# CADDY_DIR=/opt/holomush/caddy
EOF
```

#### Step 6.3: Make UFW ingress-mode aware

- [ ] Replace the `# --- Configure firewall ---` block (~lines 91–99) with:

```bash
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
```

#### Step 6.4: Add profile-aware autorun block

- [ ] Replace the trailing `echo "Next steps:"` block (~lines 101–111) with:

```bash
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
    if [ -n "${BACKUP_S3_BUCKET}" ]; then
      profiles="${profiles} --profile backups"
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
```

#### Step 6.5: Validate with shellcheck

- [ ] Run: `shellcheck scripts/cloud-init.sh`. Expected: no warnings.

#### Step 6.6: Smoke-test in a container

- [ ] Run the script in a minimal Ubuntu container to verify the non-install
  paths don't error, stubbing root-only operations:

```bash
docker run --rm -v "$(pwd)/scripts/cloud-init.sh:/init.sh:ro" \
  -e HOLOMUSH_INGRESS=tunnel \
  -e HOLOMUSH_DOMAIN="" \
  ubuntu:24.04 \
  bash -c 'cd /tmp && bash /init.sh 2>&1 | head -20 || true'
```

Expected: the early env-var block prints no validation errors; the script
exits when it tries to apt-get (OK — we're verifying our additions don't
break syntax or early-exit conditions).

#### Step 6.7: Commit

- [ ] `git add scripts/cloud-init.sh && git commit -m "feat(sandbox): cloud-init supports tunnel ingress + backup autoconfig"`

---

## Chunk 5: Sandbox Configuration Artifact

### Task 7: Sandbox env example

**Files:**

- Create: `scripts/sandbox.env.example`

This file documents the exact env vars our `deploy-sandbox` workflow hands
to cloud-init via DO droplet user-data. It does NOT contain real secrets —
operators derive it from GitHub Secrets at deploy time.

#### Step 7.1: Create the example

- [ ] Create `scripts/sandbox.env.example`:

```bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Example env values for the long-running sandbox at game.holomush.dev.
# Values marked REDACTED are supplied from GitHub Secrets at deploy time.
# This file is committed as reference; the actual .env lives only on the
# sandbox droplet and is regenerated from secrets on first boot.

HOLOMUSH_VERSION=v0.1.0
HOLOMUSH_DOMAIN=game.holomush.dev
HOLOMUSH_INGRESS=tunnel

# Cloudflare Tunnel ingress
CLOUDFLARE_TUNNEL_TOKEN=REDACTED

# Postgres
POSTGRES_PASSWORD=REDACTED

# Automated backups to DO Spaces
BACKUP_S3_BUCKET=holomush-sandbox-backups
BACKUP_S3_ENDPOINT_URL=https://sfo3.digitaloceanspaces.com
BACKUP_S3_PREFIX=game
BACKUP_S3_ACCESS_KEY=REDACTED
BACKUP_S3_SECRET_KEY=REDACTED
BACKUP_S3_REGION=us-east-1
BACKUP_RETENTION_DAYS=14
```

#### Step 7.2: Commit

- [ ] `git add scripts/sandbox.env.example && git commit -m "docs(sandbox): reference env for game.holomush.dev"`

---

## Chunk 6: Deploy Workflow

### Task 8: `deploy-sandbox` Workflow Job

**Files:**

- Modify: `.github/workflows/release.yaml`

The new job runs after `verify-release` (the existing final release job),
SSHes into the sandbox droplet, takes a pre-deploy Postgres snapshot, pulls
the new image, runs migrations, and brings the stack up.

#### Step 8.1: Add the job

- [ ] In `.github/workflows/release.yaml`, after the `verify-release` job
  block, append:

```yaml
  deploy-sandbox:
    name: Deploy Sandbox
    runs-on: namespace-profile-linux-amd64-4x8
    needs: [goreleaser, verify-release]
    if: startsWith(github.ref, 'refs/tags/')
    concurrency:
      group: deploy-sandbox
      cancel-in-progress: false
    steps:
      - name: Install doctl
        uses: digitalocean/action-doctl@v2
        with:
          token: ${{ secrets.DIGITALOCEAN_ACCESS_TOKEN }}

      - name: Configure SSH
        run: |
          mkdir -p ~/.ssh
          echo "${{ secrets.DIGITALOCEAN_SSH_PRIVATE_KEY }}" > ~/.ssh/id_ed25519
          chmod 600 ~/.ssh/id_ed25519
          ssh-keyscan -H "$(doctl compute droplet get holomush-sandbox-game --format PublicIPv4 --no-header)" >> ~/.ssh/known_hosts

      - name: Resolve droplet IP
        id: droplet
        run: |
          IP=$(doctl compute droplet get holomush-sandbox-game --format PublicIPv4 --no-header)
          echo "ip=${IP}" >> "$GITHUB_OUTPUT"

      - name: Pre-deploy Postgres safety snapshot
        run: |
          VERSION="${GITHUB_REF_NAME}"
          ssh -o StrictHostKeyChecking=yes holomush@${{ steps.droplet.outputs.ip }} \
            "cd /opt/holomush \
             && docker compose exec -T postgres pg_dump -U holomush holomush \
                | gzip \
                | aws --endpoint-url https://sfo3.digitaloceanspaces.com \
                      s3 cp - s3://holomush-sandbox-backups/pre-deploy/${VERSION}.sql.gz"
        env:
          AWS_ACCESS_KEY_ID: ${{ secrets.DO_SPACES_ACCESS_KEY }}
          AWS_SECRET_ACCESS_KEY: ${{ secrets.DO_SPACES_SECRET_KEY }}

      - name: Pull + migrate + restart
        run: |
          VERSION="${GITHUB_REF_NAME}"
          ssh holomush@${{ steps.droplet.outputs.ip }} bash -s <<EOF
            set -euo pipefail
            cd /opt/holomush
            sed -i "s/^HOLOMUSH_VERSION=.*/HOLOMUSH_VERSION=${VERSION}/" .env
            docker compose --profile tunnel --profile backups pull core gateway cloudflared
            docker compose --profile tunnel --profile backups up -d --no-recreate postgres
            docker compose --profile tunnel --profile backups run --rm core migrate
            docker compose --profile tunnel --profile backups up -d
          EOF

      - name: Health probe
        run: |
          ssh holomush@${{ steps.droplet.outputs.ip }} \
            "docker compose -f /opt/holomush/compose.yaml exec -T gateway \
               curl -sf http://localhost:8080/healthz"
```

Notes:

- `concurrency.group: deploy-sandbox` serializes deploys so two tags
  landing close together don't race.
- The droplet name `holomush-sandbox-game` is fixed; the operator creates
  it once per Task 10.
- Existing Caddy users do not get auto-deployed — this workflow is sandbox
  specific (the `startsWith(github.ref, 'refs/tags/')` guard).

#### Step 8.2: Validate with actionlint

- [ ] `task lint` (runs actionlint on workflows). Expected: pass.

#### Step 8.3: Commit

- [ ] `git add .github/workflows/release.yaml && git commit -m "ci(sandbox): deploy-sandbox job on release-tag push"`

---

## Chunk 7: Documentation

### Task 9: Extend `deployment.md` with tunnel + backups

**Files:**

- Modify: `site/docs/operating/deployment.md`

#### Step 9.1: Append the tunnel + backups sections

- [ ] Open `docs/superpowers/plans/sandbox-deployment-drafts/deployment-append.md`
  — this is the exact content to append. It contains two new top-level
  sections: `## Alternative: Cloudflare Tunnel ingress` and `## Automated
  nightly backups`.

- [ ] Append the draft's content — everything starting at
  `## Alternative: Cloudflare Tunnel ingress` (after the horizontal rule in
  the draft) — to the end of `site/docs/operating/deployment.md`, preserving
  formatting. Do not copy the draft's H1 or the comment block.

#### Step 9.2: Validate markdown

- [ ] `task lint:markdown` — expected pass.

#### Step 9.3: Commit

- [ ] `git add site/docs/operating/deployment.md && git commit -m "docs(operating): tunnel ingress + automated backups"`

---

### Task 10: Sandbox Operations Runbook

**Files:**

- Create: `site/docs/operating/sandbox-operations.md`

This runbook is written for the HoloMUSH team managing `game.holomush.dev`,
not for self-hosters. It documents the one-time bootstrap and ongoing ops.

#### Step 10.1: Copy the runbook from draft

- [ ] Open `docs/superpowers/plans/sandbox-deployment-drafts/sandbox-operations.md`.
  This is the exact content the runbook ships with.

- [ ] Copy the draft's body (everything below the `<!-- ... -->` comment
  header) into a new file at `site/docs/operating/sandbox-operations.md`,
  preserving formatting.

#### Step 10.2: Validate markdown

- [ ] `task lint:markdown` — expected pass.

#### Step 10.3: Commit

- [ ] `git add site/docs/operating/sandbox-operations.md && git commit -m "docs(operating): sandbox operations runbook"`

---

### Task 11: Restore Runbook

**Files:**

- Create: `site/docs/operating/sandbox-restore.md`

#### Step 11.1: Copy the runbook from draft

- [ ] Open `docs/superpowers/plans/sandbox-deployment-drafts/sandbox-restore.md`.

- [ ] Copy the draft's body (everything below the `<!-- ... -->` comment
  header) into a new file at `site/docs/operating/sandbox-restore.md`.

#### Step 11.2: Validate markdown

- [ ] `task lint:markdown` — expected pass.

#### Step 11.3: Commit

- [ ] `git add site/docs/operating/sandbox-restore.md && git commit -m "docs(operating): Postgres restore runbook"`

### Task 12: Root CLAUDE.md link

**Files:**

- Modify: `CLAUDE.md`

#### Step 12.1: Add link to the Documentation Structure table or nearby

- [ ] In `CLAUDE.md`, find the "Documentation Structure" section. Add one
  line after the existing `site/docs/` row:

```markdown
For sandbox operations at `game.holomush.dev`, see
[site/docs/operating/sandbox-operations.md](site/docs/operating/sandbox-operations.md)
and [site/docs/operating/sandbox-restore.md](site/docs/operating/sandbox-restore.md).
```

Position it after the existing documentation-directories table, before any
next heading.

### Step 12.2: Validate

- [ ] `task lint:markdown` — expected pass.

#### Step 12.3: Commit

- [ ] `git add CLAUDE.md && git commit -m "docs: link sandbox runbooks from CLAUDE.md"`

---

## Chunk 8: End-to-End Verification

### Task 13: One-time Bootstrap (operator runs)

**Not a code change — this task is performed manually by the operator before
the first release-tag deploy can succeed.**

Follow every step in `site/docs/operating/sandbox-operations.md` ("One-time
bootstrap"). Do not skip steps; each creates durable state the deploy
workflow depends on.

- [ ] **Step 13.1:** Create the Cloudflare Tunnel. Copy the token into
  GitHub Secret `CLOUDFLARE_TUNNEL_TOKEN`.
- [ ] **Step 13.2:** Create the Spaces bucket + keys. Copy into secrets
  `DO_SPACES_ACCESS_KEY` and `DO_SPACES_SECRET_KEY`.
- [ ] **Step 13.3:** Create the droplet via `doctl compute droplet create`
  with the extended cloud-init. Record droplet name
  `holomush-sandbox-game`.
- [ ] **Step 13.4:** Apply the firewall from
  `deploy/doctl/firewall-sandbox.json`.
- [ ] **Step 13.5:** Attach a 25 GiB block volume and mount at
  `/opt/holomush/data`.
- [ ] **Step 13.6:** Wire DNS: `game.holomush.dev` via tunnel;
  `telnet.game.holomush.dev` A record grey-cloud.
- [ ] **Step 13.7:** Save the following additional GitHub Secrets:
  - `DIGITALOCEAN_ACCESS_TOKEN`
  - `DIGITALOCEAN_SSH_PRIVATE_KEY` (matching a key authorized on the droplet)
  - `HOLOMUSH_SANDBOX_POSTGRES_PASSWORD` (the value the cloud-init wrote
    to `.env` — SSH in and copy).

Verification:

- [ ] **Step 13.8:** `ssh holomush@<droplet-ip>` succeeds.
- [ ] **Step 13.9:** `https://game.holomush.dev/healthz` returns 200.
- [ ] **Step 13.10:** `telnet telnet.game.holomush.dev 4201` connects and
  receives the welcome banner.
- [ ] **Step 13.11:** `ssh holomush@<droplet-ip> docker compose exec backup
  /usr/local/bin/backup.sh` succeeds and a new object appears in Spaces.

---

### Task 14: First Automated Deploy Verification

**Files:** none (this is a runtime verification).

#### Step 14.1: Trigger a deploy

- [ ] After Task 13 is complete, cut a test release tag (or use
  `workflow_dispatch` on `release.yaml`). Observe the `deploy-sandbox`
  job in the Actions tab.

#### Step 14.2: Verify pre-deploy backup

- [ ] Confirm `s3://holomush-sandbox-backups/pre-deploy/<tag>.sql.gz` exists
  with non-zero size:

```bash
aws --endpoint-url https://sfo3.digitaloceanspaces.com \
    s3 ls s3://holomush-sandbox-backups/pre-deploy/
```

#### Step 14.3: Verify running version

- [ ] `ssh holomush@game.holomush.dev "cd /opt/holomush && docker compose ps --format json" | jq -r '.[].Image'`.
  Expected: images tagged with the new version.

- [ ] Confirm `https://game.holomush.dev/healthz` returns 200.
- [ ] Confirm telnet still connects.

#### Step 14.4: Verify nightly backup runs

- [ ] After 03:05 UTC the next day, confirm a new object under
  `s3://holomush-sandbox-backups/game/YYYY/MM/DD/` was created.

- [ ] After 15 days, confirm retention pruning removed the oldest objects.

#### Step 14.5: Failure-injection checks (once only)

- [ ] Stop the `cloudflared` container (`docker compose stop cloudflared`).
  Verify `https://game.holomush.dev` returns a Cloudflare 530 within 60
  seconds. Restart (`docker compose up -d cloudflared`) and verify
  recovery.

- [ ] Break gateway health (`docker compose stop gateway`). Verify
  `cloudflared` logs show health-probe failure and the edge returns
  521/522. Restart and verify recovery.

- [ ] From an off-allowlist IP, confirm SSH (22) is rejected after the
  firewall is narrowed; confirm 80/443 remain closed; confirm 4201
  stays open.

---

## Post-Implementation

- [ ] Run `task pr-prep` and confirm green.
- [ ] Close the beads epic and all child tasks.
- [ ] Open a PR; link this plan and the spec in the PR description.
- [ ] Update the beads memory: "Sandbox at game.holomush.dev is live;
  deploys on release-tag push; backups nightly to DO Spaces; runbooks
  at site/docs/operating/sandbox-{operations,restore}.md."
