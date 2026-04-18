<!-- Draft content for site/docs/operating/sandbox-operations.md. -->
<!-- Copied verbatim during Task 10; this draft is not itself shipped. -->

# Sandbox Operations — game.holomush.dev

Day-to-day operations for the long-running sandbox at `game.holomush.dev`.
Self-hosters should refer to [Deploying HoloMUSH](deployment.md) instead.

## One-time bootstrap

Run these steps once per fresh droplet. They are not automated in the
`deploy-sandbox` workflow because they involve creating durable cloud
resources (DNS, Spaces bucket, tunnel) that survive droplet replacement.

### 1. Create the Cloudflare Tunnel

In the Cloudflare dashboard:

1. **Zero Trust → Networks → Tunnels → Create a tunnel**
2. Name: `holomush-sandbox`
3. Copy the token (starts with `eyJh...`) into GitHub Secrets as
   `CLOUDFLARE_TUNNEL_TOKEN`.
4. Add a **Public Hostname** route:
   `game.holomush.dev → http://gateway:8080`.
5. Save.

### 2. Create the Spaces bucket

1. DigitalOcean → **Spaces → Create a Space** in `sfo3`, name
   `holomush-sandbox-backups`.
2. Generate an access key pair: **API → Spaces Keys → Generate New Key**.
3. Save both values into GitHub Secrets as `DO_SPACES_ACCESS_KEY` and
   `DO_SPACES_SECRET_KEY`.
4. Add a lifecycle rule: objects under prefix `game/` expire after 14
   days; objects under prefix `pre-deploy/` never expire (we want every
   release's safety snapshot to persist until explicitly pruned).

### 3. Create the droplet

From your local machine:

```bash
# Render user-data from scripts/sandbox.env.example + real secrets,
# then merge with scripts/cloud-init.sh as user-data.
export HOLOMUSH_VERSION=v0.1.0
export HOLOMUSH_DOMAIN=game.holomush.dev
export HOLOMUSH_INGRESS=tunnel
export CLOUDFLARE_TUNNEL_TOKEN="..."
export POSTGRES_PASSWORD="$(openssl rand -base64 24 | tr -d '/+=' | head -c 32)"
export BACKUP_S3_BUCKET=holomush-sandbox-backups
export BACKUP_S3_ENDPOINT_URL=https://sfo3.digitaloceanspaces.com
export BACKUP_S3_ACCESS_KEY="..."
export BACKUP_S3_SECRET_KEY="..."
export BACKUP_S3_REGION=us-east-1

# Prepend env exports to the cloud-init body
(
  printf '#!/bin/bash\n'
  env | grep -E '^(HOLOMUSH_|CLOUDFLARE_TUNNEL_TOKEN|POSTGRES_PASSWORD|BACKUP_)' \
      | sed 's/^/export /'
  sed -n '10,$p' scripts/cloud-init.sh
) > /tmp/holomush-cloud-init.sh

doctl compute droplet create holomush-sandbox-game \
  --image ubuntu-24-04-x64 \
  --size s-2vcpu-2gb-amd \
  --region sfo3 \
  --ssh-keys "$(doctl compute ssh-key list --format ID --no-header | head -1)" \
  --tag-names holomush-sandbox \
  --user-data-file /tmp/holomush-cloud-init.sh
```

Wait ~2 minutes for cloud-init to finish.

### 4. Apply the firewall

```bash
doctl compute firewall create \
  --name holomush-sandbox \
  --inbound-rules-file deploy/doctl/firewall-sandbox.json
doctl compute firewall add-droplets <firewall-id> --droplet-ids <droplet-id>
```

After confirming SSH works, narrow the rule to your static IP + GitHub
Actions egress range (see <https://api.github.com/meta> for current
ranges).

### 5. Attach block storage for Postgres data

```bash
doctl compute volume create holomush-sandbox-data --region sfo3 --size 25GiB
doctl compute volume-action attach <volume-id> <droplet-id>
```

SSH in and mount it at `/opt/holomush/data` (document mount point in
`/etc/fstab`). Re-run the cloud-init after mounting to reinitialize the
Postgres data dir on the volume.

### 6. Wire DNS

- `game.holomush.dev` — already routed via the tunnel (Step 1).
- `telnet.game.holomush.dev` — A record → droplet public IP, **DNS only**
  (grey cloud).

### 7. Save the `.env` shape

Commit a redacted `.env` example to `scripts/sandbox.env.example` if the
real shape has drifted from the committed version.

## Ongoing operations

### Deploy a new version

Pushing a `v*` tag to `main` triggers the `deploy-sandbox` workflow. No
manual steps needed.

To deploy manually:

```bash
ssh holomush@game.holomush.dev
cd /opt/holomush
sed -i 's/^HOLOMUSH_VERSION=.*/HOLOMUSH_VERSION=v0.2.0/' .env
docker compose --profile tunnel --profile backups pull core gateway
docker compose --profile tunnel --profile backups run --rm core migrate
docker compose --profile tunnel --profile backups up -d
```

### View logs

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
4. Update GitHub Secret `HOLOMUSH_SANDBOX_POSTGRES_PASSWORD`.

### Recreate the tunnel

If the tunnel token is compromised:

1. Revoke the old tunnel in the Cloudflare dashboard.
2. Create a new tunnel with the same name; copy the new token.
3. Update GitHub Secret `CLOUDFLARE_TUNNEL_TOKEN`.
4. SSH to the droplet, update `.env`, `docker compose up -d cloudflared`.

### Take a manual backup

```bash
ssh holomush@game.holomush.dev
docker compose -f /opt/holomush/compose.yaml exec backup /usr/local/bin/backup.sh
```

### Restore a backup

See [sandbox-restore.md](sandbox-restore.md).

### Rebuild the droplet from scratch

If the droplet is compromised or misconfigured beyond repair:

1. Detach the block volume (`doctl compute volume-action detach`).
2. Destroy the droplet (`doctl compute droplet delete holomush-sandbox-game`).
3. Follow the **One-time bootstrap** Step 3 again to create a new droplet.
4. Attach the block volume back to the new droplet and remount at
   `/opt/holomush/data`.
5. Re-apply the firewall (Step 4).
6. Run `docker compose --profile tunnel --profile backups up -d`.
