# Database Management

HoloMUSH uses PostgreSQL as its primary data store. This guide covers database
setup, migrations, and maintenance.

## Prerequisites

- PostgreSQL 14 or later
- A database user with CREATE privileges
- The `pg_trgm` extension (for fuzzy search)

## Database Migrations

HoloMUSH uses [golang-migrate](https://github.com/golang-migrate/migrate) for
schema management. Migrations are embedded in the binary and run automatically
on startup, or can be managed manually.

### Running Migrations

```bash
# Apply all pending migrations
task migrate

# Check current migration status
task migrate:status

# Rollback one migration
task migrate:down

# Force version (for dirty state recovery)
task migrate:force -- 7
```

### Automatic Migrations

By default, HoloMUSH runs migrations automatically on startup. This can be
controlled with environment variables:

```bash
# Disable automatic migrations
HOLOMUSH_DB_AUTO_MIGRATE=false

# Run migrations manually
task migrate
```

### Creating New Migrations

For development, create new migration files:

```bash
# Install migrate CLI (one-time)
go install -tags 'pgx5' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Create migration files
task migrate:create -- descriptive_name
# Creates: NNNNNN_descriptive_name.up.sql and NNNNNN_descriptive_name.down.sql
```

**Migration file conventions:**

- Up migrations apply changes (`.up.sql`)
- Down migrations revert changes (`.down.sql`)
- Files are numbered sequentially
- Each migration should be atomic and reversible

### Bootstrapping Existing Databases

If migrating from an older HoloMUSH version with an existing schema:

```bash
psql -d holomush -f scripts/bootstrap-migrations.sql
```

This marks all migrations as applied without running them.

## Connection Configuration

Configure the database connection via environment variables:

| Variable          | Description                    | Default  |
| ----------------- | ------------------------------ | -------- |
| `DATABASE_URL`    | Full PostgreSQL connection URL | Required |
| `DB_MAX_CONNS`    | Maximum open connections       | 25       |
| `DB_MIN_CONNS`    | Minimum idle connections       | 5        |
| `DB_MAX_LIFETIME` | Connection max lifetime        | 1h       |

**Example connection URL:**

```bash
DATABASE_URL="postgres://holomush:secret@localhost:5432/holomush?sslmode=require"
```

## Backup and Recovery

### Creating Backups

```bash
# Full database dump
pg_dump -Fc holomush > holomush_$(date +%Y%m%d_%H%M%S).dump

# Schema only
pg_dump -Fc --schema-only holomush > holomush_schema.dump

# Data only
pg_dump -Fc --data-only holomush > holomush_data.dump
```

### Restoring from Backup

```bash
# Restore full backup
pg_restore -d holomush holomush_backup.dump

# Restore to new database
createdb holomush_restored
pg_restore -d holomush_restored holomush_backup.dump
```

## Troubleshooting

### Dirty Migration State

If a migration fails partway through, the database may be in a "dirty" state:

```bash
# Check current state
task migrate:status

# If dirty, fix manually then force version
task migrate:force -- 6  # Force to version 6
```

### Connection Issues

Check PostgreSQL is running and accessible:

```bash
psql $DATABASE_URL -c "SELECT 1"
```

Verify the `pg_trgm` extension is available:

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;
```
