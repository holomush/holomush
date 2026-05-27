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

## Naming

- Sequential numeric prefix: `000001_baseline.up.sql`, `000001_baseline.down.sql`
- Pad to 6 digits, zero-prefixed
- Snake_case description after the prefix
- Always paired: every `.up.sql` MUST have a matching `.down.sql`

## Content rules

- **Idempotent** — use `IF NOT EXISTS` / `IF EXISTS` so reruns are safe
- **No triggers, no functions, no stored procedures** — all logic lives in Go (PostgreSQL is just storage)
- Columns added MUST be nullable or have a default; never `NOT NULL` without backfill
- **No long-running data backfills inside the migration** — issue a separate one-shot job if you need backfill. Migrations should stay cheap to run repeatedly.
- Use `BEGIN; … COMMIT;` only when the operations are transactional in PG (e.g., `CREATE INDEX CONCURRENTLY` cannot live in a transaction)

## Down migrations

- MUST cleanly revert the up. Drop in reverse order. `DROP TABLE IF EXISTS` / `DROP COLUMN IF EXISTS`.
- If the up creates an index, the down drops it.
- If the up alters a constraint, the down restores the original (or recreates it) — do not leave the schema in a different state than before the up.

## Verification

Before opening a PR with a new migration:

1. Run `task test:int` — integration tests run against a fresh DB and exercise migrations
2. Roll the migration up and down locally on a scratch DB to confirm reversibility
3. Check `task lint:access-migration` if your migration touches access-control tables (CI runs this gate)

## See also

- `site/docs/contributing/database-migrations.md` for the full guide and worked examples
