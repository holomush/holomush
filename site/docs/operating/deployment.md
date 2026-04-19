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
6. Paste the [cloud-init script](https://raw.githubusercontent.com/holomush/holomush/main/scripts/cloud-init.sh) into the User Data field
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

| Symptom                    | Check                                                        |
| -------------------------- | ------------------------------------------------------------ |
| HTTPS not working          | DNS propagated? `dig your-domain` should show droplet IP     |
| "502 Bad Gateway" from Caddy | Gateway healthy? `docker compose logs gateway`             |
| Telnet connection refused  | UFW allows 4201? `ufw status`                                |
| Game world empty           | Core bootstrap logs: `docker compose logs core \| grep bootstrap` |
| Caddy cert errors          | Port 80 open? ACME needs it for HTTP-01 challenge            |

For detailed monitoring and health check information, see
[Operations](operations.md).

## Generic Linux Host

Any Linux host with Docker 24+ works. Skip the DigitalOcean-specific steps
and instead:

1. Install Docker: [docs.docker.com/engine/install](https://docs.docker.com/engine/install/)
2. Download `compose.prod.yaml` to `/opt/holomush/compose.yaml`
3. Download `docker/otel-collector/config.prod.yaml` to `/opt/holomush/config/otel-collector.yaml`
4. Create `/opt/holomush/.env` (see the generated `.env` in the cloud-init script for the template)
5. Create the data directories: `mkdir -p /opt/holomush/{data/postgres,config/certs,caddy/{data,config}}`
6. Follow steps 3–5 from the Quick Start above

## Alternative: Cloudflare Tunnel ingress

If you want your server reachable only through Cloudflare (no public HTTP
ports open on the droplet), use the `tunnel` ingress profile instead of
Caddy. Cloudflare terminates TLS at its edge and your droplet makes an
outbound connection to Cloudflare — nothing connects inbound to 80 or 443.

1. In the Cloudflare dashboard, go to **Zero Trust → Networks → Tunnels**
   and create a new tunnel named `holomush`.
2. Copy the tunnel token (it starts with `eyJh...`).
3. In the tunnel's **Public Hostnames** tab, add a route:
   `mush.example.com → http://gateway:8080`.
4. Set your cloud-init user-data to include these env vars:

    ```bash
    HOLOMUSH_INGRESS=tunnel
    HOLOMUSH_DOMAIN=mush.example.com
    CLOUDFLARE_TUNNEL_TOKEN=eyJh...your-token...
    ```

5. Create the droplet with the cloud-init script. Once first boot
   completes, `https://mush.example.com` is live.

Telnet (port 4201) still reaches your droplet directly — Cloudflare does
not proxy arbitrary TCP. Add a "DNS only" A record for
`telnet.mush.example.com` pointing at your droplet's IP.

## Automated nightly backups (encrypted)

To back up Postgres nightly to S3-compatible storage (DigitalOcean Spaces,
AWS S3, Cloudflare R2) with client-side encryption, add these vars to your
cloud-init:

```bash
BACKUP_S3_BUCKET=my-holomush-backups
BACKUP_S3_ENDPOINT=sfo3.digitaloceanspaces.com   # omit for AWS S3
BACKUP_S3_ACCESS_KEY=...
BACKUP_S3_SECRET_KEY=...
KOPIA_PASSWORD=...           # long random string — encrypts every snapshot
BACKUP_KEEP_DAILY=7          # optional
BACKUP_KEEP_WEEKLY=4         # optional
BACKUP_KEEP_MONTHLY=6        # optional
```

When set, the cloud-init enables the `backups` compose profile, which runs
a cron container that dumps Postgres at 03:00 UTC and streams it through
[Kopia](https://kopia.io/). Kopia encrypts the stream client-side (your
cloud provider cannot read the backups), deduplicates against previous
snapshots, compresses with zstd, and uploads. Retention is policy-based —
expired snapshots are pruned automatically.

**Keep `KOPIA_PASSWORD` somewhere recoverable.** If you lose it, every
snapshot in the repository becomes unrecoverable. Kopia has no recovery
backdoor.

Run a one-off backup via:

```bash
cd /opt/holomush
docker compose --profile caddy --profile backups exec backup /usr/local/bin/backup.sh
```

(Replace `--profile caddy` with `--profile tunnel` if you use tunnel
ingress.)

Restore from a backup: see [Restoring a backup](sandbox-restore.md).
