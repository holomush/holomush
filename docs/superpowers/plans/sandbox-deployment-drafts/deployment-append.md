<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Deployment Doc Appendix (Draft)

<!-- Two new sections to APPEND to site/docs/operating/deployment.md. -->
<!-- Copy everything below the horizontal rule verbatim. This H1 is not -->
<!-- part of the content to copy. -->

---

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
