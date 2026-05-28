---
title: "Restoring a Postgres Backup"
---

Restore a Kopia snapshot produced by the `backups` compose profile (or a
pre-deploy safety snapshot) into a running HoloMUSH instance.

## Find the snapshot

Snapshots are identified by Kopia snapshot IDs, not filesystem paths. List
the 10 most recent snapshots:

```bash
ssh holomush@game.holomush.dev \
  'docker compose -f /opt/holomush/compose.yaml --profile tunnel --profile backups \
     exec -T backup kopia snapshot list --all --max-results=10'
```

List only the pinned pre-deploy snapshots (one per release):

```bash
ssh holomush@game.holomush.dev \
  'docker compose -f /opt/holomush/compose.yaml --profile tunnel --profile backups \
     exec -T backup kopia snapshot list --all --tags=pre-deploy:'
```

Grab the snapshot ID from the leftmost column (e.g. `kabc123...`).

## Restore path A: into a throwaway Postgres (verification)

Use this path to verify a snapshot without touching the running sandbox.
Requires the `kopia` binary and the repository password on your machine.

```bash
# On your local machine
mkdir /tmp/restore-test && cd /tmp/restore-test

# One-time: connect your local kopia to the repo
export KOPIA_PASSWORD="<your-KOPIA_SANDBOX_PASSWORD>"
export AWS_ACCESS_KEY_ID="<your-DO_SPACES_ACCESS_KEY>"
export AWS_SECRET_ACCESS_KEY="<your-DO_SPACES_SECRET_KEY>"
kopia repository connect s3 \
  --bucket=holomush-sandbox-backups \
  --endpoint=nyc3.digitaloceanspaces.com

# Pull the chosen snapshot contents to a file
kopia snapshot restore <snapshot-id> ./backup.sql

# Spin up a throwaway Postgres and load the dump
docker run --rm -d --name pg-restore-test \
  -e POSTGRES_USER=holomush -e POSTGRES_PASSWORD=verify -e POSTGRES_DB=holomush \
  -p 5433:5432 postgres:18-alpine

sleep 3
PGPASSWORD=verify psql -h localhost -p 5433 -U holomush -d holomush < backup.sql

# Spot-check tables
PGPASSWORD=verify psql -h localhost -p 5433 -U holomush -d holomush \
  -c "SELECT count(*) FROM events"

docker rm -f pg-restore-test
```

## Restore path B: into the live sandbox (destructive)

**WARNING:** This overwrites the running sandbox's database. Take a pinned
manual backup first.

```bash
ssh holomush@game.holomush.dev
cd /opt/holomush

# 1. Fresh pinned backup of current state
docker compose --profile tunnel --profile backups \
  exec backup /usr/local/bin/backup.sh --tag=manual-pin:pre-restore

# 2. Stop services that write to the DB
docker compose stop core gateway

# 3. Restore the chosen snapshot contents to a file in the backup container
SNAPSHOT_ID=<id>
docker compose --profile tunnel --profile backups \
  exec -T backup kopia snapshot restore "${SNAPSHOT_ID}" /tmp/restore.sql

# 4. Drop and recreate the DB
docker compose exec -T postgres psql -U holomush -d postgres <<'SQL'
DROP DATABASE holomush;
CREATE DATABASE holomush OWNER holomush;
SQL

# 5. Load the dump into the fresh DB (use the backup container's network
#    connection to postgres; it can psql because the image includes
#    postgresql-client)
docker compose --profile tunnel --profile backups \
  exec -T backup sh -c \
  'PGPASSWORD="${PGPASSWORD}" psql -h postgres -U holomush -d holomush < /tmp/restore.sql'

# 6. Restart services
docker compose --profile tunnel --profile backups up -d

# 7. Verify
docker compose exec -T gateway curl -sf http://localhost:8080/healthz
```

## Rollback after a bad deploy

If a release broke the sandbox, restore from the pinned pre-deploy snapshot
taken at the start of the deploy workflow:

```bash
# Find the pre-deploy snapshot for the broken release
ssh holomush@game.holomush.dev \
  'docker compose -f /opt/holomush/compose.yaml --profile tunnel --profile backups \
     exec -T backup kopia snapshot list --tags=pre-deploy:v0.3.0'

# Follow Restore path B with the resulting snapshot ID, then redeploy the
# previous good tag via the deploy-sandbox workflow workflow_dispatch.
```
