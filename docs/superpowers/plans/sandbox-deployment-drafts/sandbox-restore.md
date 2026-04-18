<!-- Draft content for site/docs/operating/sandbox-restore.md. -->
<!-- Copied verbatim during Task 11; this draft is not itself shipped. -->

# Restoring a Postgres Backup

Restore a backup produced by the `backups` compose profile (or a pre-deploy
safety snapshot) into a running HoloMUSH instance.

## Find the backup

Nightly backups live at
`s3://<bucket>/<prefix>/YYYY/MM/DD/<timestamp>.sql.gz`.
Pre-deploy safety snapshots live at `s3://<bucket>/pre-deploy/<tag>.sql.gz`.

On the droplet, list the 10 most recent nightly backups:

```bash
docker compose exec backup \
  aws --endpoint-url "${BACKUP_S3_ENDPOINT_URL}" \
      s3 ls "s3://${BACKUP_S3_BUCKET}/${BACKUP_S3_PREFIX}/" --recursive \
  | sort | tail -10
```

## Restore path A: into a throwaway Postgres (verification)

Use this path to verify a backup without touching the running sandbox.

```bash
# On your local machine
mkdir /tmp/restore-test && cd /tmp/restore-test

aws --endpoint-url https://sfo3.digitaloceanspaces.com \
    s3 cp "s3://holomush-sandbox-backups/<key>.sql.gz" ./backup.sql.gz

docker run --rm -d --name pg-restore-test \
  -e POSTGRES_USER=holomush -e POSTGRES_PASSWORD=verify -e POSTGRES_DB=holomush \
  -p 5433:5432 postgres:18-alpine

sleep 3
gunzip -c backup.sql.gz | PGPASSWORD=verify \
  psql -h localhost -p 5433 -U holomush -d holomush

# Spot-check tables
PGPASSWORD=verify psql -h localhost -p 5433 -U holomush -d holomush \
  -c "SELECT count(*) FROM events"

docker rm -f pg-restore-test
```

## Restore path B: into the live sandbox (destructive)

**WARNING:** This overwrites the running sandbox's database. Take a fresh
manual backup first.

```bash
ssh holomush@game.holomush.dev
cd /opt/holomush

# 1. Fresh backup of current state
docker compose exec backup /usr/local/bin/backup.sh

# 2. Stop services that write to the DB
docker compose stop core gateway

# 3. Pull the chosen backup
docker compose exec backup \
  aws --endpoint-url "${BACKUP_S3_ENDPOINT_URL}" \
      s3 cp "s3://${BACKUP_S3_BUCKET}/<key>.sql.gz" /tmp/restore.sql.gz

# 4. Drop and recreate the DB
docker compose exec -T postgres psql -U holomush -d postgres <<'SQL'
DROP DATABASE holomush;
CREATE DATABASE holomush OWNER holomush;
SQL

# 5. Restore
docker compose exec -T backup sh -c \
  'gunzip -c /tmp/restore.sql.gz | psql -h postgres -U holomush -d holomush'

# 6. Restart services
docker compose --profile tunnel --profile backups up -d

# 7. Verify
docker compose exec -T gateway curl -sf http://localhost:8080/healthz
```

## Rollback after a bad deploy

If a release broke the sandbox, restore from the pre-deploy snapshot taken
at the start of the deploy workflow:

```bash
# The snapshot key is pre-deploy/<tag>.sql.gz
KEY=pre-deploy/v0.3.0.sql.gz

# Follow Restore path B with this KEY, then redeploy the previous good
# tag via the deploy-sandbox workflow workflow_dispatch.
```
