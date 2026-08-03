---
title: "Restoring a Postgres Backup"
---

Restore a Kopia snapshot produced by the `backups` compose profile (or a
pre-deploy safety snapshot) into a running HoloMUSH instance.

## Reaching the droplet

`game.holomush.dev` is Cloudflare-proxied, so port 22 is unreachable through it.
Every `ssh` command below uses the droplet's public IPv4, the same way the deploy
workflow does. Resolve it once per shell:

```bash
# Assign and export SEPARATELY: `export X=$(cmd)` always returns 0, so it
# discards cmd's exit status and leaves DROPLET_IP empty on failure — every
# later command then silently targets `holomush@`.
DROPLET_IP=$(doctl compute droplet get holomush-sandbox-game \
  --format PublicIPv4 --no-header)
export DROPLET_IP="${DROPLET_IP:?doctl returned no address — check auth (doctl account get) and the droplet name}"
```

:::danger[The Kopia snapshot does NOT contain the KEK keyfile]
`backup.sh` streams `pg_dump` into Kopia — the **database only**. The certs
volume holding `master.key.enc` is never captured. Once any `crypto_keys` row
exists, restoring a snapshot onto a droplet without the original keyfile leaves
the service unable to start: the core mints a fresh KEK, then refuses with
`KEK_PROVIDER_CANNOT_UNWRAP_EXISTING_DEKS` because it cannot unwrap the restored
DEKs. Back up `${CONFIG_DIR:-/opt/holomush/config}/certs/master.key.enc` and its
passphrase **alongside** every database snapshot, and restore both together.
:::

## Find the snapshot

Snapshots are identified by Kopia snapshot IDs, not filesystem paths. List
the 10 most recent snapshots:

```bash
ssh "holomush@${DROPLET_IP}" \
  'docker compose -f /opt/holomush/compose.yaml --profile tunnel --profile backups \
     exec -T backup kopia snapshot list --all --max-results=10'
```

List only the pinned pre-deploy snapshots (one per release):

```bash
ssh "holomush@${DROPLET_IP}" \
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

### Pre-deploy rehearsal: the goose migration cutover

The first boot of a binary carrying the goose migration engine **adopts** an
existing database: it seeds `goose_db_version` from the old `schema_migrations`
ledger and renames that table to `schema_migrations_pre_goose`. The write is
unattended, one-way, and performs **no schema verification** — it trusts the
recorded version. This rehearsal against restored real data is the only place
drift between the recorded version and the actual schema would surface, so it is
not optional. The integration tests prove the mechanism on synthetic data seeded
at v53; only real sandbox data reveals actual drift.

**1. Take a fresh `pre-deploy:` snapshot immediately before the cutover deploy.**

```bash
ssh "holomush@${DROPLET_IP}" \
  'docker compose -f /opt/holomush/compose.yaml --profile tunnel --profile backups \
     exec -T backup /usr/local/bin/backup.sh --tag=pre-deploy:goose-cutover' </dev/null
```

Do **not** rehearse against the most recent existing snapshot. Everything that
happened between that snapshot and the deploy is exactly what the rehearsal would
not see, and it is the window in which drift accumulates.

**2. Restore it into a throwaway Postgres** using Restore path A above, then run
the new binary's migration against that container:

```bash
# Capture the pre-adopt ledger version FIRST. The adopt renames
# schema_migrations, so this value cannot be recovered afterward — and it is
# the only thing that makes check (a) below meaningful.
RECORDED=$(PGPASSWORD=verify psql -h localhost -p 5433 -U holomush \
  -d holomush -tAc 'SELECT version FROM schema_migrations')
echo "pre-adopt recorded version: ${RECORDED:?empty — is this really a pre-goose database?}"

DATABASE_URL="postgres://holomush:verify@localhost:5433/holomush?sslmode=disable" \
  ./holomush migrate up
```

Count the embedded migrations at or below that version — this is the expected
`migrations` value in (a), and it is what the adopt actually seeds:

```bash
ls internal/store/migrations/*.sql \
  | sed -E 's#.*/([0-9]{6})_.*#\1#' | sort -u \
  | awk -v r="$RECORDED" '$1+0 <= r+0' | wc -l
```

**3. Verify three things by hand.** All three, not a spot-check:

```bash
PGPASSWORD=verify psql -h localhost -p 5433 -U holomush -d holomush <<'SQL'
-- (a) the ledger: expect the count derived above, plus goose's version-0 row
SELECT count(*) FILTER (WHERE version_id > 0) AS migrations,
       count(*)                               AS total_rows,
       min(version_id)                        AS lowest
  FROM goose_db_version;

-- (b) the rename: the archive exists and the old name is gone
SELECT to_regclass('public.schema_migrations_pre_goose') AS archived,
       to_regclass('public.schema_migrations')           AS legacy;

-- (c) the application schema: spot the tables the release expects
SELECT table_name FROM information_schema.tables
 WHERE table_schema = 'public' ORDER BY table_name;
SQL
```

Expected: (a) `migrations` equals the number of embedded migrations at or below
the version the old ledger recorded, `total_rows` is that plus one for goose's
version-0 bootstrap row, and `lowest = 0`; (b) `archived` non-null and `legacy`
**null**; (c) the table list matches the schema the release was built against —
no table missing and none left over from a migration the ledger claims was
applied.

:::caution[Derive (a) from the database, do not copy a number]
The adopt seeds only the versions **at or below** what `schema_migrations`
recorded (`derivedSeedVersions`, `internal/store/migrate_adopt.go:355`) — not the
whole corpus. A database several releases behind therefore yields *fewer* rows
than the corpus contains, and that is correct, not a failure. Read
`SELECT version FROM schema_migrations` before the adopt and count the embedded
migrations at or below it; compare against that. A box fully caught up to the
current corpus gives 44 and 45, but treating those as fixed constants will
report a healthy adopt as broken on any box that is behind.
:::

If any of the three disagrees, do not deploy. A mismatch in (a) or (b) means the
adopt did not complete; a mismatch in (c) is the schema drift the adopt gate
cannot see.

## Restore path B: into the live sandbox (destructive)

**WARNING:** This overwrites the running sandbox's database. Take a pinned
manual backup first.

```bash
# Opens an interactive session ON the droplet, already in /opt/holomush.
# Everything below runs at the remote prompt.
ssh -t "holomush@${DROPLET_IP}" 'cd /opt/holomush && exec bash -l'

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
ssh "holomush@${DROPLET_IP}" \
  'docker compose -f /opt/holomush/compose.yaml --profile tunnel --profile backups \
     exec -T backup kopia snapshot list --tags=pre-deploy:v0.3.0'

# Follow Restore path B with the resulting snapshot ID, then redeploy the
# previous good tag via the deploy-sandbox workflow workflow_dispatch.
```

### Rollback after a goose cutover deploy

This procedure is valid for exactly one deploy window and then expires: it works
only while the cutover release ships alone (no new migrations in it) and no later
migration has been deployed. Once a release carrying a migration after `000053`
lands on the sandbox, this path is wrong and the only correct rollback is a full
snapshot restore via Restore path B. Deleting this section is tracked in
[holomush/holomush#4907](https://github.com/holomush/holomush/issues/4907).

Why it works at all: the cutover ships **alone** as a release constraint, so the
application schema is provably unchanged across the deploy. Only the bookkeeping
moved, and only the bookkeeping has to move back.

```bash
# Opens an interactive session ON the droplet, already in /opt/holomush.
# Everything below runs at the remote prompt.
ssh -t "holomush@${DROPLET_IP}" 'cd /opt/holomush && exec bash -l'

# 1. Stop the services that would re-run the adopt on boot
docker compose stop core gateway

# 2. Put the bookkeeping back the way the previous binary expects it
docker compose exec -T postgres psql -U holomush -d holomush <<'SQL'
BEGIN;
ALTER TABLE schema_migrations_pre_goose RENAME TO schema_migrations;
DROP TABLE goose_db_version;
COMMIT;
SQL

# 3. Redeploy the previous good tag via the deploy-sandbox workflow
#    workflow_dispatch, then verify
docker compose exec -T gateway curl -sf http://localhost:8080/healthz
```

Step 2 is a single transaction on purpose: a half-reverted ledger — the old table
back under its old name while `goose_db_version` still exists — is a database
neither binary reads correctly.

If `schema_migrations_pre_goose` is **absent**, the adopt never ran and there is
nothing to revert; redeploy the previous tag directly. If both tables are absent,
this is not a post-cutover database and this section does not apply.
