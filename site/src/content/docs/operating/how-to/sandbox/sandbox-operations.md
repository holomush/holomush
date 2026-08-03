---
title: "Sandbox Operations — game.holomush.dev"
---

Day-to-day operations for the HoloMUSH project's own sandbox at `game.holomush.dev`,
maintained by the core team. Self-hosters should refer to
[Deploying HoloMUSH](/operating/how-to/deploy/deployment/) instead.

## One-time bootstrap

:::danger[Fresh bootstrap is currently unsupported — see #4928]
The bootstrap path does **not** provision `HOLOMUSH_KEK_PASSPHRASE`. Neither
`scripts/cloud-init.sh` nor `.github/workflows/bootstrap-sandbox.yaml` writes it
into the generated `.env`, and `compose.prod.yaml` requires it — so a freshly
bootstrapped droplet fails `docker compose config`, and the core would refuse to
start even if it got that far (`a KEK is required to start`).

Do not treat the flow below as deployable until
[#4928](https://github.com/holomush/holomush/issues/4928) lands. Its fix must
generate the passphrase **once** and persist it to a secret, the way
`KOPIA_SANDBOX_PASSWORD` is handled — **not** regenerate it per run the way
`POSTGRES_PASSWORD` is. Regenerating a KEK strands every previously wrapped DEK.

The rest of this section is accurate for the resources it provisions; only the
KEK secret is missing.
:::

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

Every `ssh` command in this section targets the droplet's public IPv4.
`game.holomush.dev` is Cloudflare-proxied, so port 22 is unreachable through it.
Export the address once per shell before running anything below:

```bash
export DROPLET_IP=$(doctl compute droplet get holomush-sandbox-game \
  --format PublicIPv4 --no-header)
```

### Deploy a new version

Deploying is a **separate human gate** from cutting a release (INV-4): cutting a
release does NOT deploy it. Dispatch the `Deploy Sandbox` workflow
(`.github/workflows/deploy.yaml`) manually with the release tag.

Confirm all four prerequisites first — the workflow does not check them for you,
and several fail in ways that resemble success:

| Prerequisite | Why it matters |
| --- | --- |
| A **published** (non-draft) GitHub Release exists for the tag | The workflow contract requires the release, not just the tag. A draft satisfies `gh release view` but is not published |
| The GHCR image exists for that version | The deploy pulls `ghcr.io/holomush/holomush:${HOLOMUSH_VERSION}`; a missing image fails mid-deploy, after traffic has already been stopped |
| Repository variable `SANDBOX_DEPLOY_ENABLED` is `true` | The deploy job is guarded by `if: vars.SANDBOX_DEPLOY_ENABLED == 'true'`. When it is not `true` the dispatch still succeeds and a run still appears — the job is simply **skipped** |
| The droplet is not several releases behind | See the caution below |

```bash
TAG=v0.12.0
DROPLET_IP=$(doctl compute droplet get holomush-sandbox-game \
  --format PublicIPv4 --no-header)

# 1. Release exists AND is published, not a draft
gh release view "$TAG" -R holomush/holomush --json isDraft \
  --jq 'if .isDraft then error("release is a DRAFT — publish it first") else "release published" end'

# 2. GHCR image exists for that version (tag without the leading v).
#    --paginate is load-bearing: the endpoint pages, and without it only the
#    most recent page is searched, so an older-but-valid tag reports missing.
gh api --paginate /orgs/holomush/packages/container/holomush/versions \
  --jq '.[].metadata.container.tags[]?' | grep -qx "${TAG#v}" \
  && echo "image ${TAG#v} present" || echo "IMAGE MISSING — do not deploy"

# 3. Deploy is enabled. Assert the VALUE — the variable existing tells you
#    nothing, and `false` is exactly the case that silently skips the job.
test "$(gh variable list -R holomush/holomush --json name,value \
  --jq '.[] | select(.name=="SANDBOX_DEPLOY_ENABLED") | .value')" = "true" \
  && echo "deploy enabled" || echo "DEPLOY DISABLED — the job would skip"

# 4. Droplet is not behind
ssh "holomush@${DROPLET_IP}" 'grep ^HOLOMUSH_VERSION= /opt/holomush/.env'
```

Then dispatch, capturing the run so the check below cannot land on someone
else's:

```bash
gh workflow run deploy.yaml -R holomush/holomush -f tag="$TAG"

RUN_ID=$(gh run list -R holomush/holomush --workflow=deploy.yaml \
  --event=workflow_dispatch --limit 1 --json databaseId --jq '.[0].databaseId')

gh run watch "$RUN_ID" -R holomush/holomush --exit-status
```

The deploy takes its own `pre-deploy:<tag>` snapshot as its first step, so the
rollback point is created automatically.

:::caution[Assert the JOB conclusion, not the run's]
A guarded job that skips does not fail, so checking the run tells you nothing
about whether anything deployed. Require the **job** to be `success` — that is
correct regardless of how the run itself reports:

```bash
gh run view "$RUN_ID" -R holomush/holomush --json jobs \
  --jq '.jobs[] | select(.name=="Deploy Sandbox") | .conclusion'
# MUST print: success
# "skipped" means SANDBOX_DEPLOY_ENABLED was not "true" — nothing was deployed
```

Do **not** substitute `gh run list --limit 1` here: a concurrent dispatch can
put a different run at the top.
:::

:::caution[Check the deployed version against the migration corpus first]
The "ships alone" constraint below is phrased release-to-release, so it does
NOT catch a sandbox that is several releases stale: a box left on an old tag
can be many migrations behind, and deploying the cutover to it would run the
irreversible adopt AND the pending migrations in one unattended boot. Compare
`HOLOMUSH_VERSION` in `/opt/holomush/.env` and `schema_migrations.version`
against the corpus, and stage an intermediate deploy first if there is a gap.
:::

> **Release constraint — the goose migration cutover ships alone.** The release
> that first carries the goose migration engine MUST NOT also carry new
> migrations; the next release carries those. The cutover's first boot rewrites
> the bookkeeping one-way and unattended, and shipping it alone is what keeps the
> application schema provably unchanged across that deploy — which is the only
> reason the surgical rollback in
> [Restoring a Postgres Backup](/operating/how-to/sandbox/sandbox-restore/) exists. Ship them together
> and any rollback becomes a full snapshot restore with data loss. Rehearse the
> cutover first using the pre-deploy rehearsal in that same document.

To deploy manually, mirror what the `deploy-sandbox` job does — sync the
tag's compose file, `docker/` tree, and `deploy/` tree onto the droplet
BEFORE pulling images and restarting. Without the sync, compose/profile
changes or backup-image updates in the release never reach the host:

```bash
ssh "holomush@${DROPLET_IP}"
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
  psql -U holomush -d holomush -tAc "SELECT reltuples::bigint FROM pg_class WHERE oid = 'public.events_audit'::regclass" </dev/null | tr -d '[:space:]')
echo "pre-migrate events_audit row estimate: ${ROWS:-unknown} (pg_class.reltuples; core health budget ~90s)"

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
ssh "holomush@${DROPLET_IP}"
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
ssh "holomush@${DROPLET_IP}"
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
