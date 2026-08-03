---
title: "Database Migrations"
---

HoloMUSH uses [goose](https://github.com/pressly/goose) with embedded SQL files
for schema management. Migrations live in `internal/store/migrations/` and are
compiled into the binary via `embed.FS`. goose records applied versions in the
`goose_db_version` table.

`internal/store/migrations/` is also a Go package, so a migration may be written
in Go (`NNNNNN_name.go`) when SQL alone cannot express it; goose interleaves Go
and SQL migrations by version number. See
[Go migrations](#go-migrations) below.

## Baseline

The schema starts from a single baseline migration (`000001_baseline`) that
creates every table needed for the 0.1 release. There is no upgrade path from
pre-0.1 databases. If you need to evolve the schema, add a new migration file
after the baseline.

## Rules

These rules use RFC 2119 keywords (MUST, SHOULD, MAY).

| Rule | Description |
| ---- | ----------- |
| MUST use sequential numbering | `000002_`, `000003_`, etc. — exactly 6 digits |
| MUST use one file per version | A single `NNNNNN_name.sql` carrying both directions |
| MUST provide up and down | `-- +goose Up` and `-- +goose Down` sections in that file |
| MUST wrap dollar-quoted bodies | Any `$$` body sits inside `-- +goose StatementBegin` / `-- +goose StatementEnd` |
| MUST be idempotent | Use `IF NOT EXISTS`, `IF EXISTS`, `ON CONFLICT DO NOTHING` |
| MUST NOT use triggers or functions | All logic lives in Go; PostgreSQL is storage only |
| MUST NOT open its own transaction | goose already wraps each migration; see [Transactions](#transactions) |
| MUST NOT modify the baseline | Add new migrations instead of editing `000001_baseline` |
| SHOULD keep migrations small | One logical change per migration |
| SHOULD add comments | Explain why, not what |

## Creating a New Migration

1. Pick the next sequence number (check the highest existing file — the corpus
   currently ends at `000053`).

2. Create **one** file, by hand:

```text
internal/store/migrations/000054_add_foo.sql
```

There is no generator task. goose's own `create` command numbers sequential
migrations with five digits, which does not match this project's six-digit
convention (pinned by `migrate_embed_test.go`), so a hand-written file from the
template below is the supported path.

3. Write both directions in that file, starting from the SPDX header:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- +goose Up
CREATE TABLE IF NOT EXISTS foo (
    id         TEXT PRIMARY KEY,
    created_at BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
);

CREATE INDEX IF NOT EXISTS idx_foo_created_at ON foo (created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_foo_created_at;
DROP TABLE IF EXISTS foo;
```

The down section MUST fully undo the up section so round-trip tests pass.

4. Run the test suite:

```bash
task test && task lint && task test:int
```

### Dollar-quoted bodies

goose splits a migration into statements on semicolons, line by line. A
`DO $$ … END $$;` body contains semicolons, so without an explicit wrapper goose
tears it apart and Postgres rejects the fragment with
`unterminated dollar-quoted string`. Bracket every such body:

```sql
-- +goose StatementBegin
DO $$
BEGIN
  IF to_regclass('public.foo') IS NOT NULL THEN
    ALTER TABLE public.foo ADD COLUMN IF NOT EXISTS label TEXT;
  END IF;
END $$;
-- +goose StatementEnd
```

`000052_events_audit_partition.sql` is the worked example in-tree: 9 of the 44
migrations carry wrapped blocks, 24 blocks in total, and `000052` alone carries
9 of them.

Under goose's default transactional mode the failure is loud and atomic — the
whole migration rolls back and nothing is recorded. Under
`-- +goose NO TRANSACTION` the same mistake applies the migration's earlier
statements and leaves the rest unapplied. `task test` runs
`TestEveryDollarQuotedMigrationBodyIsWrappedInStatementBeginEnd`, which names any
unwrapped body by file and line.

### Transactions

goose wraps each migration in a transaction, so a migration MUST NOT contain its
own `BEGIN;` / `COMMIT;`. When a statement cannot run inside a transaction (for
example `CREATE INDEX CONCURRENTLY`), put `-- +goose NO TRANSACTION` at the top
of the file and accept that the migration is no longer atomic.

### `ENVSUB` is not used

goose can substitute environment variables into migration SQL between
`-- +goose ENVSUB ON` and `-- +goose ENVSUB OFF`. HoloMUSH does not use it: a
migration is a permanent, replayed artifact, and letting the environment rewrite
its SQL makes the applied schema depend on who ran it.

### Do not write the annotation token into prose

goose treats **any** `--` comment containing `+goose` as an annotation and hard-
errors on an unrecognised one. A comment inside a migration that mentions the
annotation prefix in passing is a migration that will not parse.

## Go migrations

A Go migration is `NNNNNN_name.go` in `internal/store/migrations/`, sharing the
version sequence with the SQL files. Requirements:

- Its own `init()` MUST call `goose.AddMigrationContext`. `//go:embed` never
  embeds `.go`, so an unregistered Go migration disappears from the chain
  silently.
- A down function whose effect cannot be reverted MUST return an error naming
  why — a silent no-op down shows an operator a successful rollback that did not
  happen.
- `AddMigrationNoTxContext` requires a `// goose-no-tx: <reason>` comment naming
  the statement that forbids a transaction.

`internal/store/migrations_register_test.go` enforces all three, plus the blank
import in `internal/store/migrations_register.go` that makes those `init()`
functions run.

## CLI Commands

The `migrate` subcommand manages schema versions:

```bash
holomush migrate            # Apply all pending migrations
holomush migrate up         # Same as above
holomush migrate down       # Roll back one migration
holomush migrate down --all # Roll back all migrations
holomush migrate status     # Show applied and pending migrations
holomush migrate version    # Print version number only
```

Add `--dry-run` to `up` or `down` to preview without applying.

There is no version-forcing command. goose applies a migration's body and writes
its `goose_db_version` row in the same transaction, so a migration either applies
and is recorded or does neither — the half-applied state that a force command
would exist to repair cannot arise.

## Testing

Unit tests in `migrate_embed_test.go` verify that every migration follows the
`NNNNNN_name.sql` naming convention, carries both a `-- +goose Up` and a
`-- +goose Down` annotation, and that no `.up.sql`/`.down.sql` split files
remain. `migrations_format_test.go` checks dollar-quote wrapping across the whole
corpus. The integration test in `migrate_integration_test.go` performs a
full round trip: up, verify tables, down, verify empty, up again. Any new
migration that breaks the round trip will fail CI.

## Schema Partitions

The `access_audit_log` table is partitioned by timestamp range. The migration
creates the parent table definition but does not create partitions. The server
creates partitions at bootstrap time. If you add a partitioned table, follow the
same pattern: define the table in the migration, create partitions in Go.

## Timestamp columns: BIGINT epoch nanoseconds

Per `holomush-gfo6` (INV-TS-1), all new migrations MUST use `BIGINT` for
persistent time values, storing nanoseconds since the UNIX epoch in UTC.
`TIMESTAMPTZ` and `TIMESTAMP WITH TIME ZONE` (and bare `TIMESTAMP`) are
prohibited in new schemas.

**Schema pattern:**

```sql
CREATE TABLE thing (
    id          TEXT PRIMARY KEY,
    created_at  BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
    updated_at  BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
);
```

For columns with a pre-existing `DEFAULT` that need to be migrated from
`TIMESTAMPTZ` to `BIGINT`, emit `ALTER COLUMN ... DROP DEFAULT` BEFORE
`ALTER COLUMN ... TYPE` (PostgreSQL has no implicit cast from `TIMESTAMPTZ`
to `BIGINT`), then `SET DEFAULT` with the new BIGINT expression.

**Application code pattern:**

Route every write and scan through the `pgnanos.Time` seam (ADR
`holomush-rbw6`). The seam keeps caller-visible types as `time.Time` while
satisfying pgx's binary protocol on the `BIGINT` column.

```go
import "github.com/holomush/holomush/internal/pgnanos"

// Insert
_, err := pool.Exec(ctx, `INSERT INTO thing (id, created_at) VALUES ($1, $2)`,
    id, pgnanos.From(t))

// Scan
var createdAt pgnanos.Time
err := row.Scan(&id, &createdAt)
t := createdAt.Time()
```

When two writers touch the same column with different time sources, harmonize
them to a single clock domain — typically SQL-side
`(EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT` — to avoid drift between Go's
clock and PostgreSQL's clock that can invert chronological ordering.

**Why BIGINT instead of `TIMESTAMPTZ`:** preserves full nanosecond precision
end-to-end so the audit AAD canonical encoding (INV-TS-5) reconstructs
byte-equal without a microsecond truncate discipline, eliminates the
~140 `.Truncate(time.Microsecond)` sites the prior pattern required, and
gives deterministic ordering at nanosecond resolution. The rejected
alternative (`timestamp9` PG extension) is discussed in
`docs/superpowers/specs/2026-05-22-nanosecond-timestamps-design.md`.

**Enforcement:** `task lint:no-timestamptz` rejects new `TIMESTAMPTZ`/
`TIMESTAMP` columns in post-cutoff migrations. `task lint:no-microsecond-truncate`
rejects new `.Truncate(time.Microsecond)` calls. `task lint:no-unixnano-in-repos`
rejects raw `UnixNano()` / `time.Unix(0, ...)` in repo packages.
Escape hatch on any of the three: `-- pgnanos-exempt: <reason>` (SQL) or
`// pgnanos-exempt: <reason>` (Go) on the same line.
