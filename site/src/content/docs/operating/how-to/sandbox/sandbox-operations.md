---
title: "Sandbox Operations — game.holomush.dev"
---

Day-to-day operations for the HoloMUSH project's own sandbox at `game.holomush.dev`,
maintained by the core team. Self-hosters should refer to
[Deploying HoloMUSH](/operating/how-to/deploy/deployment/) instead.

## One-time bootstrap

The bootstrap workflow automates all cloud resource provisioning from zero
in a single run. It is idempotent — safe to re-run if it fails partway through.

### Seed secrets (set once, before running the workflow)

Add these seven secrets to the repository via **Settings → Secrets and
variables → Actions → New repository secret**:

| Secret                       | How to obtain                                              |
| ---------------------------- | ---------------------------------------------------------- |
| `DIGITALOCEAN_ACCESS_TOKEN`  | DO dashboard → API → Personal Access Tokens               |
| `DIGITALOCEAN_SSH_KEY_ID`    | Fingerprint or numeric ID of an existing key in DO         |
| `DIGITALOCEAN_SSH_PRIVATE_KEY` | Matching private key for the above                       |
| `CLOUDFLARE_API_TOKEN`       | CF dashboard → My Profile → API Tokens (Zone:DNS:Edit + Account:Cloudflare Tunnel:Edit) |
| `CLOUDFLARE_ACCOUNT_ID`      | CF dashboard → right sidebar → Account ID                  |
| `CLOUDFLARE_ZONE_ID`         | CF dashboard → domain → right sidebar → Zone ID            |
| `SECRETS_ADMIN_PAT`          | GitHub → Developer Settings → Fine-grained PAT with Secrets:Write on this repo |

### Run the workflow

1. Go to **Actions → Bootstrap Sandbox → Run workflow**.
2. Accept the defaults (or adjust region, sizes, bucket name, etc.).
3. Leave **dry\_run** unchecked for a real provisioning run.
4. Click **Run workflow** and monitor the run — it takes ~5–10 minutes.

The workflow creates and writes back to GitHub Secrets:

- `KOPIA_SANDBOX_PASSWORD` — Kopia repository encryption key (generated once; back it up)
- `CLOUDFLARE_TUNNEL_ID` and `CLOUDFLARE_TUNNEL_TOKEN`
- `DO_SPACES_ACCESS_KEY` and `DO_SPACES_SECRET_KEY`

### After the workflow completes

1. Confirm the healthz check passed in the workflow summary.
2. Back up `KOPIA_SANDBOX_PASSWORD` to a secure location (1Password, sealed
   secret, etc.). If it is lost, existing snapshots become unrecoverable —
   Kopia encrypts client-side with no recovery path.
3. Narrow the SSH firewall rule (`22/tcp`) from `0.0.0.0/0` to your static IP
   plus the GitHub Actions egress range (see <https://api.github.com/meta>).

### Save the `.env` shape

Commit a redacted `.env` example to `scripts/sandbox.env.example` if the
real shape has drifted from the committed version.

---

## Manual bootstrap (air-gapped or debugging)

Use these steps if the workflow is unavailable or you need to troubleshoot
individual resources.

### 1. Create the Cloudflare Tunnel

In the Cloudflare dashboard:

1. **Zero Trust → Networks → Tunnels → Create a tunnel**
2. Name: `holomush-sandbox`
3. Copy the token (starts with `eyJh...`) into GitHub Secrets as
   `CLOUDFLARE_TUNNEL_TOKEN`.
4. Add a **Public Hostname** route:
   `game.holomush.dev → http://gateway:8080`.
5. Save.

### 2. Create the Spaces bucket and Kopia password

1. DigitalOcean → **Spaces → Create a Space** in `nyc3`, name
   `holomush-sandbox-backups`.
2. Generate an access key pair: **API → Spaces Keys → Generate New Key**.
3. Save both values into GitHub Secrets as `DO_SPACES_ACCESS_KEY` and
   `DO_SPACES_SECRET_KEY`.
4. Generate a long Kopia repository password and save it as
   `KOPIA_SANDBOX_PASSWORD`:

    ```bash
    openssl rand -base64 48 | tr -d '=/+' | head -c 64
    ```

   **Store this password somewhere recoverable** (1Password, a sealed
   secret, etc.). If it is lost, every snapshot in the repository becomes
   unrecoverable — Kopia encrypts client-side with no recovery.
5. No Spaces lifecycle rule is needed. Kopia manages retention
   internally: pinned `pre-deploy:*` snapshots live forever, others are
   pruned by policy.

### 3. Create the droplet

The commands below assemble a cloud-init script by prepending your secrets
as exported shell variables ahead of `scripts/cloud-init.sh`, then passing
the result as user-data to the new droplet. Run these from your local machine:

```bash
# Render user-data from scripts/sandbox.env.example + real secrets,
# then merge with scripts/cloud-init.sh as user-data.
export HOLOMUSH_REF=v0.1.0     # source ref for cloud-init (tag/branch/sha; defaults to "main")
export HOLOMUSH_VERSION=0.1.0  # docker image tag pulled from ghcr.io (without "v")
export HOLOMUSH_DOMAIN=game.holomush.dev
export HOLOMUSH_INGRESS=tunnel
export CLOUDFLARE_TUNNEL_TOKEN="..."
export POSTGRES_PASSWORD="$(openssl rand -base64 24 | tr -d '/+=' | head -c 32)"
export BACKUP_S3_BUCKET=holomush-sandbox-backups
export BACKUP_S3_ENDPOINT=nyc3.digitaloceanspaces.com
export BACKUP_S3_ACCESS_KEY="..."
export BACKUP_S3_SECRET_KEY="..."
export KOPIA_PASSWORD="..."   # from KOPIA_SANDBOX_PASSWORD secret
export BACKUP_KEEP_DAILY=7
export BACKUP_KEEP_WEEKLY=4
export BACKUP_KEEP_MONTHLY=6

# Prepend env exports to the cloud-init body
(
  printf '#!/bin/bash\n'
  env | grep -E '^(HOLOMUSH_|CLOUDFLARE_TUNNEL_TOKEN|POSTGRES_PASSWORD|BACKUP_|KOPIA_)' \
      | sed 's/^/export /'
  sed -n '10,$p' scripts/cloud-init.sh
) > /tmp/holomush-cloud-init.sh
```

### 4. Create the block volume BEFORE the droplet

Cloud-init's `mounts:` directive only runs during first boot, so the
volume must exist and be attached BEFORE the droplet boots. Create it
first, then pass its ID via `--volumes` at droplet-create time:

```bash
VOLUME_ID=$(doctl compute volume create holomush-sandbox-data \
  --region nyc3 --size 25GiB --fs-type ext4 --format ID --no-header)
```

### 5. Create the droplet with the volume attached at boot

```bash
doctl compute droplet create holomush-sandbox-game \
  --image ubuntu-24-04-x64 \
  --size s-2vcpu-2gb-amd \
  --region nyc3 \
  --ssh-keys "$(doctl compute ssh-key list --format ID --no-header | head -1)" \
  --tag-names holomush-sandbox \
  --volumes "${VOLUME_ID}" \
  --user-data-file /tmp/holomush-cloud-init.sh
```

Wait ~2 minutes for cloud-init to finish. Postgres's first init will
land on the attached volume because cloud-init's `mounts:` stanza runs
before Docker Compose.

### 6. Apply the firewall

`doctl compute firewall create --inbound-rules-file` expects a DSL string,
not JSON. The committed `deploy/doctl/firewall-sandbox.json` is
REST-API-shaped (matches what the bootstrap workflow posts). Use curl:

```bash
# Substitute your SSH-allowlist CIDRs before posting.
SSH_CIDRS='["203.0.113.5/32"]'   # e.g. your static IP; comma-separate if more
SSH_CIDRS_JSON=$(printf '%s' "${SSH_CIDRS}")

FW_JSON=$(jq \
  --argjson ssh_sources "${SSH_CIDRS_JSON}" '
    .inbound_rules[] |= (
      if .protocol == "tcp" and .ports == "22"
      then .sources.addresses = $ssh_sources
      else .
      end
    )
  ' deploy/doctl/firewall-sandbox.json)

FW_ID=$(curl -fsS -X POST \
  -H "Authorization: Bearer ${DIGITALOCEAN_ACCESS_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "${FW_JSON}" \
  "https://api.digitalocean.com/v2/firewalls" | jq -r '.firewall.id')

curl -fsS -X POST \
  -H "Authorization: Bearer ${DIGITALOCEAN_ACCESS_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"droplet_ids\":[${DROPLET_ID}]}" \
  "https://api.digitalocean.com/v2/firewalls/${FW_ID}/droplets"
```

The committed JSON ships a locked-down `127.0.0.1/32` placeholder for SSH.
Always substitute your real operator CIDR allowlist before posting.

### 7. Wire DNS

- `game.holomush.dev` — already routed via the tunnel (Step 1).
- `telnet.game.holomush.dev` — A record → droplet public IP, **DNS only**
  (grey cloud).

### 8. Save the `.env` shape

Commit a redacted `.env` example to `scripts/sandbox.env.example` if the
real shape has drifted from the committed version.

## Ongoing operations

### Deploy a new version

Pushing a `v*` tag to `main` triggers the `deploy-sandbox` workflow. No
manual steps needed.

To deploy manually, mirror what the `deploy-sandbox` job does — sync the
tag's compose file, `docker/` tree, and `deploy/` tree onto the droplet
BEFORE pulling images and restarting. Without the sync, compose/profile
changes or backup-image updates in the release never reach the host:

```bash
ssh holomush@game.holomush.dev
VERSION=v0.2.0
sudo apt-get install -y git  # if not already present

# Sync release assets onto the host
rm -rf /tmp/holomush-release
git clone --depth 1 --branch "${VERSION}" \
  https://github.com/holomush/holomush.git /tmp/holomush-release
cp /tmp/holomush-release/compose.prod.yaml /opt/holomush/compose.yaml
rm -rf /opt/holomush/docker /opt/holomush/deploy
cp -r /tmp/holomush-release/docker /opt/holomush/docker
cp -r /tmp/holomush-release/deploy /opt/holomush/deploy
rm -rf /tmp/holomush-release

# Update version pin + run the deploy sequence
cd /opt/holomush
sed -i "s/^HOLOMUSH_VERSION=.*/HOLOMUSH_VERSION=${VERSION}/" .env
docker compose --profile tunnel --profile backups pull core gateway cloudflared
docker compose --profile tunnel --profile backups build backup
docker compose --profile tunnel --profile backups up -d --no-recreate postgres

# Pre-migrate backfill-budget probe: the new core's synchronous audit Backfill
# runs before readiness, so a large events_audit can exceed the core health
# budget (~90s: start_period 15s + retries 15 × interval 5s). Above ~500k rows,
# run an ahead-of-deploy backfill or temporarily raise the core start_period.
ROWS=$(docker compose --profile tunnel --profile backups exec -T postgres \
  psql -U holomush -d holomush -tAc "SELECT count(*) FROM events_audit" </dev/null | tr -d '[:space:]')
echo "pre-migrate events_audit rows: ${ROWS} (core health budget ~90s)"

# Sever the whole player-traffic path AND the old core before migrate, so the
# old core's now-incompatible audit INSERT never runs against the 000052 schema.
docker compose --profile tunnel --profile backups stop cloudflared gateway core

# `-T` + `</dev/null` guard stdin (this block may be pasted into an ssh heredoc).
docker compose --profile tunnel --profile backups run --rm -T core migrate </dev/null

# Start ONLY the new core, gated on its readiness (its synchronous audit backfill
# boot gate). If the gate fails, `up -d --wait` exits non-zero and aborts here —
# player traffic is never restored onto a bad core.
docker compose --profile tunnel --profile backups up -d --wait --no-deps core

# Restore player traffic (gateway + cloudflared) only after core is ready.
docker compose --profile tunnel --profile backups up -d
```

Migration 000052 (audit-log partitioning, shipped in the same release as the
retention worker) makes the old core's `events_audit` INSERT incompatible with
the new schema (`event_ms NOT NULL` plus a dropped `id`-alone unique index). The
sequence above therefore **stops cloudflared + gateway + core before
`core migrate`** — severing the entire player-traffic path so no request reaches
a half-migrated core — runs the migration with no old core writing, starts
**only** the new core gated on its readiness, and restores player traffic
**last**. The brief audit-write outage during the migrate/readiness window is a
deliberate, bounded single-node risk; the readiness gate aborts the deploy
before traffic is restored if the new core's audit backfill fails. The
pre-migrate row-count probe warns when a large `events_audit` history could push
the synchronous backfill past the ~90s core health budget.

### View logs

On the droplet, cloud-init (and the release deploy workflow) copies the
repository's `compose.prod.yaml` to `/opt/holomush/compose.yaml`, so the
`-f /opt/holomush/compose.yaml` path below is correct despite the source
file being named `compose.prod.yaml`.

```bash
ssh holomush@game.holomush.dev
docker compose -f /opt/holomush/compose.yaml logs -f core gateway cloudflared
```

### Rotate Postgres password

1. Generate a new password: `openssl rand -base64 24 | tr -d '/+=' | head -c 32`
2. On the droplet, `docker compose exec postgres psql -U holomush -d
   holomush -c "ALTER USER holomush WITH PASSWORD '...';"`
3. Update `.env` and `docker compose up -d core gateway backup` to pick
   up the new value.

### Recreate the tunnel

If the tunnel token is compromised:

1. Revoke the old tunnel in the Cloudflare dashboard.
2. Create a new tunnel with the same name; copy the new token.
3. Update GitHub Secret `CLOUDFLARE_TUNNEL_TOKEN`.
4. SSH to the droplet, update `.env`, `docker compose up -d cloudflared`.

### Take a manual backup

```bash
ssh holomush@game.holomush.dev
docker compose -f /opt/holomush/compose.yaml --profile tunnel --profile backups \
  exec backup /usr/local/bin/backup.sh
```

To take a pinned snapshot that retention policy will not expire:

```bash
docker compose -f /opt/holomush/compose.yaml --profile tunnel --profile backups \
  exec backup /usr/local/bin/backup.sh --tag=manual-pin:$(date -u +%F)
```

### Rotate the Kopia repository password

**Warning:** rotating the repository password means every snapshot currently
in the repository becomes unreadable. There is no "re-encrypt" operation.

1. Take a final backup under the old password and download it locally.
2. Create a new bucket (or prefix) for the new repository.
3. Update `KOPIA_SANDBOX_PASSWORD` in GitHub Secrets.
4. SSH to the droplet, update `.env` with the new password (and bucket /
   prefix if changed), then **explicitly initialize the new repository**
   from the `backup` container — `backup.sh` only connects to an existing
   repo, it does not create one, so without this step cron backups silently
   fail:

    ```bash
    cd /opt/holomush
    docker compose --profile tunnel --profile backups run --rm backup \
      kopia repository create s3 \
        --bucket="${BACKUP_S3_BUCKET}" \
        --endpoint="${BACKUP_S3_ENDPOINT}" \
        --access-key="${BACKUP_S3_ACCESS_KEY}" \
        --secret-access-key="${BACKUP_S3_SECRET_KEY}"
    ```

   Old snapshots remain encrypted with the old password — keep a copy if
   they matter.

### Restore a backup

See [sandbox-restore.md](/operating/how-to/sandbox/sandbox-restore/).

### Rebuild the droplet from scratch

If the droplet is compromised or misconfigured beyond repair:

1. Detach the block volume from the old droplet:

    ```bash
    doctl compute volume-action detach "${VOLUME_ID}" "${OLD_DROPLET_ID}"
    ```

2. Destroy the old droplet:

    ```bash
    doctl compute droplet delete holomush-sandbox-game
    ```

3. Create the new droplet **with the existing volume attached at boot** so
   cloud-init's `mounts:` stanza mounts `/opt/holomush/data` before
   Postgres initializes — reattaching after create would put Postgres
   back on ephemeral disk:

    ```bash
    doctl compute droplet create holomush-sandbox-game \
      --image ubuntu-24-04-x64 \
      --size s-2vcpu-2gb-amd \
      --region nyc3 \
      --ssh-keys "$(doctl compute ssh-key list --format ID --no-header | head -1)" \
      --tag-names holomush-sandbox \
      --volumes "${VOLUME_ID}" \
      --user-data-file /tmp/holomush-cloud-init.sh
    ```

4. Re-apply the firewall to the new droplet (see **Manual bootstrap Step 6**
   for the `doctl compute firewall add-droplets` call).
5. Verify the stack is up: `ssh holomush@<new-ip> docker compose -f /opt/holomush/compose.yaml ps`.
