<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

---
paths:
  - "internal/store/migrations/**"
---

# Database Migration Rules

Migrations live in `internal/store/migrations/` and are embedded into the binary at compile time.
The engine is [goose](https://github.com/pressly/goose) (`github.com/pressly/goose/v3`), and the
bookkeeping table is `goose_db_version`.

`internal/store/migrations/` is also a **Go package** (`package migrations`), so Go migrations can
live alongside the SQL ones. `internal/store/migrations_register.go` carries the blank import that
makes their `init()` functions run.

## Naming and file format

- **One file per version**: `NNNNNN_name.sql` — there is no `.up.sql`/`.down.sql` pair. Both
  directions live in the same file, separated by annotations.
- Sequential numeric prefix, padded to 6 digits, zero-prefixed: `000054_add_foo.sql`
- Snake_case description after the prefix
- Every file MUST carry `-- +goose Up`, and SHOULD carry `-- +goose Down` after it

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- +goose Up
CREATE TABLE IF NOT EXISTS thing (
    id         TEXT PRIMARY KEY,
    created_at BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
);

-- +goose Down
DROP TABLE IF EXISTS thing;
```

## Annotations

goose treats **any** comment line beginning with `--` that contains the token `+goose` as an
annotation, and an unrecognised one is a hard parse error (`unknown annotation`). Two consequences:

- **MUST NOT** write prose mentioning that token into a migration comment. A line such as
  `-- read the goose annotation docs` is fine; the same sentence spelling the token out is a
  migration that will not parse.
- The recognised set is exactly: `Up`, `Down`, `StatementBegin`, `StatementEnd`, `NO TRANSACTION`,
  `ENVSUB ON`, `ENVSUB OFF`.

### Dollar-quoted bodies MUST be wrapped

Any `$$`-delimited body (`DO $$ … END $$;`) **MUST** be bracketed by `-- +goose StatementBegin` and
`-- +goose StatementEnd`. goose splits a migration into statements on semicolons, line by line, and
without the wrapper it tears the body apart at the first `;` inside it.

The failure mode is **loud and atomic**, and that is exactly why the wrapper is easy to think
unnecessary:

| Mode | What an unwrapped `$$` body does |
| ---- | -------------------------------- |
| goose's default (transactional) | Postgres rejects the torn fragment (`unterminated dollar-quoted string`) and the **whole migration rolls back** — nothing is applied, nothing is recorded |
| `-- +goose NO TRANSACTION` | earlier statements in the same migration **stay applied** while the torn body fails — a partially-applied migration |

A future reader who sees only the loud rollback must not conclude the wrapper is decorative: the
silent, partial variant is one annotation away. `000052_events_audit_partition.sql` is the worked
example in-tree.

**Enforced by** `TestEveryDollarQuotedMigrationBodyIsWrappedInStatementBeginEnd`
(`internal/store/migrations_format_test.go`), which runs in the untagged `task test` lane.

### `ENVSUB` is declined on purpose

goose can interpolate environment variables into migration SQL between `-- +goose ENVSUB ON` and
`-- +goose ENVSUB OFF`. HoloMUSH **MUST NOT** use it. A migration is a permanent, replayed artifact;
letting the deployment environment change the SQL it executes makes the applied schema a function of
whoever ran it, and turns the environment into a config-injection surface into DDL. This is a
decision, not an oversight — anything a migration needs at runtime belongs in Go.

Interpolation is enabled **per migration** by that in-file annotation; goose exposes no
provider-level switch to decline it, and `NewMigrator` (`internal/store/migrate.go`) passes none. So
this prohibition has no backstop other than the scan below.

**Enforced by** the same `TestEveryDollarQuotedMigrationBodyIsWrappedInStatementBeginEnd`
(`internal/store/migrations_format_test.go`), which rejects either annotation anywhere in an embedded
migration — inside a statement block as well as outside, because goose reads its annotations the same
way — in the untagged `task test` lane.

### Transactions

goose wraps each migration in a transaction by default, so a migration body **MUST NOT** contain its
own `BEGIN;` / `COMMIT;`. For the rare statement Postgres forbids inside a transaction (e.g.
`CREATE INDEX CONCURRENTLY`), put `-- +goose NO TRANSACTION` at the top of the file — and accept
that the migration is then no longer atomic.

## Content rules

- **Idempotent** — use `IF NOT EXISTS` / `IF EXISTS` so reruns are safe
- **No triggers, no functions, no stored procedures** — all logic lives in Go (PostgreSQL is just
  storage). Anonymous `DO` blocks are not persisted objects and are permitted.
- Columns added MUST be nullable or have a default; never `NOT NULL` without backfill
- **No long-running data backfills inside the migration** — issue a separate one-shot job if you
  need backfill. Migrations should stay cheap to run repeatedly.
- Timestamp-class columns MUST be `BIGINT` epoch-nanoseconds, never `TIMESTAMPTZ`/`TIMESTAMP`
  (INV-STORE-1, enforced by `task lint:no-timestamptz`)

## Down migrations

- MUST cleanly revert the up. Drop in reverse order. `DROP TABLE IF EXISTS` / `DROP COLUMN IF EXISTS`.
- If the up creates an index, the down drops it.
- If the up alters a constraint, the down restores the original (or recreates it) — do not leave the
  schema in a different state than before the up.

## Go migrations

A Go migration is `NNNNNN_name.go` in the same directory, sharing the version sequence with the SQL
ones; goose interleaves both by version number.

| Requirement | Rule |
| ----------- | ---- |
| **MUST** register in `init()` | The file's own `init()` calls `goose.AddMigrationContext` (or `AddMigrationNoTxContext`). `//go:embed migrations/*.sql` never embeds `.go`, so an unregistered Go migration vanishes from the chain **silently** — goose's own unregistered-Go guard cannot fire. |
| **MUST** return an error from an irreversible down | If the up's effect cannot be reverted, the down returns an error naming why. A silent no-op down lets an operator watch a rollback succeed and believe state reverted when it did not. |
| **SHOULD** stay transactional | `AddMigrationContext` runs inside goose's transaction. |
| **MUST** justify a non-transactional migration | `AddMigrationNoTxContext` requires a `// goose-no-tx: <reason>` comment naming the statement that forbids a transaction. |

Both the registration requirement and the `goose-no-tx` marker are enforced by
`TestGoMigrationRegistrationHoldsAcrossTheMigrationsCorpus`, and the blank import that runs those
`init()`s is pinned by `TestExactlyOneBlankImportWiresTheMigrationsPackageIntoStore` (both in
`internal/store/migrations_register_test.go`). See INV-STORE-11.

## Verification

Before opening a PR with a new migration:

1. Run `task test` — the format and registration guards run in the untagged lane
2. Run `task test:int` — integration tests run against a fresh DB and exercise migrations
3. Roll the migration up and down locally on a scratch DB to confirm reversibility
4. Check `task lint:access-migration` if your migration touches access-control tables (CI runs this
   gate)

## See also

- `site/src/content/docs/contributing/how-to/database-migrations.md` for the full guide and worked
  examples
