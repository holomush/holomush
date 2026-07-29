---
phase: 06-operational-hardening-assurance-gates
reviewed: 2026-07-15T00:00:00Z
depth: standard
files_reviewed: 12
files_reviewed_list:
  - internal/eventbus/audit/retention_partitions.go
  - internal/eventbus/audit/subsystem.go
  - internal/eventbus/audit/projection.go
  - internal/eventbus/audit/event_ms_test.go
  - internal/audit/retention.go
  - cmd/holomush/cmd_audit.go
  - cmd/holomush/bootstrap_orphan.go
  - cmd/nats-floor-guard/main.go
  - internal/store/migrations/000052_events_audit_partition.up.sql
  - internal/store/migrations/000052_events_audit_partition.down.sql
  - .github/workflows/deploy.yaml
  - .codecov.yml
findings:
  critical: 0
  warning: 3
  info: 2
  total: 5
resolution:
  fixed: 3            # WR-01, WR-02, IN-02
  accepted: 2         # WR-03 (tracked #4818), IN-01
  outstanding: 0
  tracking_issues:
    - "https://github.com/holomush/holomush/issues/4818"  # WR-03
status: resolved
---

# Phase 6: Code Review Report

**Reviewed:** 2026-07-15T00:00:00Z
**Depth:** standard
**Files Reviewed:** 12
**Status:** resolved (3 fixed, 2 accepted-documented, 0 outstanding)

## Resolution Summary

| Finding | Disposition | Reference |
| ------- | ----------- | --------- |
| WR-01   | Fixed       | commit `bb9d68fc7` |
| WR-02   | Fixed       | commit `695e40a7d` |
| WR-03   | Accepted (documented) — tracking issue filed | [#4818](https://github.com/holomush/holomush/issues/4818) |
| IN-01   | Accepted (documented) | — |
| IN-02   | Fixed       | commit `606d0bcd3` |

## Summary

Reviewed the non-crypto surface of phase 06 (OPS-02 partition manager +
retention worker, OPS-03 nats-floor-guard/lint:vuln, OPS-04 DLQ replay
resolver, QUAL-01 codecov ratchet, deploy choreography). The crypto surface
(projection.go, migration 000052, rekey tests) was already gated READY by the
crypto-reviewer and was only spot-checked here for the `event_ms` derivation
consistency that Backfill depends on — confirmed identical (`eventMsFromULID`
reused verbatim by both `writeAuditRow` and `Backfill`).

Overall the partition-manager DDL is well-hardened: every dynamic identifier
flows through `pgx.Identifier{...}.Sanitize()`, the provenance-marker literal
through `quoteLiteral`, and no operator/user input reaches any DDL string — no
SQL-injection surface. The nats-floor-guard semver logic is correct for the
`v`-prefixed module versions go.mod produces. Coverage docs
(CLAUDE.md / testing.md) accurately describe codecov's non-required, non-per-
package statuses and match `.codecov.yml` — no false coverage-gate claims.

The findings below are consistency/operability gaps, not correctness breakers.
The lead item is that the leaked-search_path hardening applied to the partition
DDL was not carried into the `Backfill` INSERT / legacy-table RENAME in the same
file.

## Warnings

### WR-01: Backfill INSERT + legacy RENAME + HealthCheck bypass the leaked-search_path hardening applied to the partition DDL

**File:** `internal/eventbus/audit/retention_partitions.go:463`, `:483`, `:377`
**Issue:** The round-5 hardening public-qualified every partition DDL
identifier (`ensureMonthPartition`, `detach*`, `drop*`, `rename*` all use
`pgx.Identifier{schemaName, name}.Sanitize()`), and a dedicated regression test
(`retention_partitions_test.go:152`
`TestEnsurePartitionsLandsChildrenInPublicUnderLeakedSearchPath`) exists
specifically because a non-public session `search_path` can be leaked onto a
pooled connection. But three statements in the *same file* still use bare,
unqualified relation names and therefore remain search_path-relative:
- `Backfill` line 463: `INSERT INTO events_audit (...)` — under a leaked
  `search_path TO other_ns, public` this would target `other_ns.events_audit`
  if one existed.
- `Backfill` line 483: `ALTER TABLE events_audit_unpartitioned RENAME TO
  events_audit_legacy_migrated` — same exposure, on the boot-gate rename that
  makes re-runs a no-op.
- `HealthCheck` line 377: `SELECT 1 FROM events_audit LIMIT 0` (read probe;
  lower risk, but same inconsistency).

This is the boot-gate path (`subsystem.go:294`), so it runs on every start. The
exposure is defense-in-depth (production `search_path` is normally `public`),
but leaving the same file half-qualified after an explicit hardening pass is a
latent gap the tested threat model already takes seriously.

**Fix:** Qualify the parent/legacy names the same way the DDL does, e.g.
```go
// line 463
`INSERT INTO public.events_audit ( ... ) VALUES (...) ON CONFLICT (id, event_ms) DO NOTHING`
// line 483
fmt.Sprintf("ALTER TABLE %s RENAME TO %s",
    pgx.Identifier{schemaName, "events_audit_unpartitioned"}.Sanitize(),
    pgx.Identifier{"events_audit_legacy_migrated"}.Sanitize())
// line 377
`SELECT 1 FROM public.events_audit LIMIT 0`
```
(`readLegacyChunk` at :493 and the `to_regclass('public.events_audit_unpartitioned')`
probe at :421 are already public-anchored; only the write/rename/health paths lag.)

**Resolution (FIXED — commit `bb9d68fc7`):** Public-qualified all three
statements consistently with the rest of the file: `Backfill` INSERT →
`INSERT INTO public.events_audit`; the legacy `RENAME` uses a
schema-qualified source with a BARE target (`pgx.Identifier{schemaName,
"events_audit_unpartitioned"}.Sanitize()` → `pgx.Identifier{"events_audit_
legacy_migrated"}.Sanitize()`, mirroring `renameDetached`); `HealthCheck` →
`SELECT 1 FROM public.events_audit LIMIT 0`. Added a sibling regression test
(`TestBackfillAndHealthCheckTargetPublicUnderLeakedSearchPath`) that plants a
decoy `leaked_ns.events_audit` and proves the re-homed row lands in
`public.events_audit` (RED→GREEN) and HealthCheck operates on public under a
leaked non-public search_path.

### WR-02: A single malformed legacy row permanently blocks server boot with no operator bypass

**File:** `internal/eventbus/audit/retention_partitions.go:441-445`
**Issue:** `Backfill` aborts the whole boot gate if any legacy row's id is not
exactly 16 bytes (`return oops.Code("AUDIT_BACKFILL_BAD_ID")...`). Because the
legacy table is renamed to `events_audit_legacy_migrated` only *after all rows
are copied* (line 483), an error on row N leaves `events_audit_unpartitioned`
in place, so the next boot re-runs Backfill and hits the same row — a permanent,
un-bypassable boot failure (`subsystem.go:294` returns
`AUDIT_BACKFILL_BOOT_GATE_FAILED` from `Start`). Fail-closed is defensible for
an audit trail, and the schema stores 16-byte BYTEA ids so this "can't happen"
from a clean history — but a restore-from-old-backup (the exact scenario
`bootstrap_orphan.go` guards) or a partial migration could introduce one, and
there is no skip/quarantine escape hatch for the operator.
**Fix:** Either (a) log-and-skip the malformed row (count them, surface a
non-fatal warning, still rename the table so subsequent boots proceed), or
(b) keep the hard fail but add an operator escape hatch (env-gated
"skip-malformed-audit-rows" or a `holomush audit backfill --force-skip-bad`
subcommand) so a single bad row is recoverable without hand-editing Postgres.

**Resolution (FIXED — commit `695e40a7d`):** Took option (a). `Backfill` now
SKIPs any malformed-id row (WARN-logs it via context-carrying slog with a
`skipped_malformed` count) and continues re-homing the valid rows, then
completes the legacy-table rename so subsequent boots are a no-op. A malformed
id can't be a valid event, so dropping it from the backfill is acceptable — it
must not brick boot. The keyset cursor now advances for every row (valid or
skipped) so a malformed row at a chunk boundary cannot loop pagination. Test
`TestBackfillSkipsMalformedLegacyRowsWithoutBricking` (RED→GREEN) seeds one
malformed + one valid legacy row and asserts Backfill completes, the valid row
is re-homed, the bad row is dropped, and the legacy table is renamed.

### WR-03: 000052 down migration neither restores nor removes runtime-detached partitions

**File:** `internal/store/migrations/000052_events_audit_partition.down.sql:37-81`
**Issue:** The down copies rows back only from the partitioned parent
(`FROM events_audit`, Step 2, which sees attached children only) and the surviving
legacy table (Step 3), then `DROP TABLE events_audit CASCADE` (Step 4). Any
runtime-detached partition — `events_audit_<YYYY_MM>_detached_<unix>` produced by
`detachOlderThan`/`renameDetached` and awaiting its grace-period drop — is *not*
a child of `events_audit`, so (a) its rows are not copied into the restored
table and (b) it is not dropped by the CASCADE. The migration rule requires a
down to leave the schema in the same state as before the up; these tables
linger. Impact is low: the orphaned rows are past the 90d retention window
(that is why they were detached), and `DropDetachedPartitions` matches by name
pattern + provenance marker regardless of parent, so a subsequent up + retention
cycle self-heals the litter. Still, a pure down leaves stray tables and silently
omits their rows from the restored history.
**Fix:** Add a Step to the down that, before/after the parent drop, discovers
`events_audit_%_detached_%` tables carrying the provenance marker and either
copies their rows into `events_audit_restore_tmp` (data-preserving) and/or drops
them, so the revert is clean.

**Resolution (ACCEPTED — documented; tracking issue
[#4818](https://github.com/holomush/holomush/issues/4818)):** The
`events_audit_*_detached_*` tables are retention-worker RUNTIME residue created
AFTER the migration ran, holding PAST-RETENTION data already slated for
deletion — outside migration 000052's own up-scope (its up/down are symmetric
for what the up creates). On rollback they linger harmlessly and self-heal on
re-up (`DropDetachedPartitions` matches by name pattern + provenance marker
regardless of parent). The migration cleanly reverts its own up; runtime
detached-partition cleanup is a separate retention-lifecycle concern. Filed as
enhancement #4818 (priority::low) for a possible future down/cleanup step. No
code change in this pass (touching 000052 would re-trigger the crypto gate).

## Info

### IN-01: Backfill discards the legacy `inserted_at`, re-stamping it to backfill time

**File:** `internal/eventbus/audit/retention_partitions.go:461-469`, `:493-499`
**Issue:** `readLegacyChunk` does not SELECT `inserted_at`, and the Backfill
INSERT omits the column, so backfilled rows take the parent's
`DEFAULT (EXTRACT(EPOCH FROM now())*1e9)` — i.e. backfill time, not the original
projector-write time. The 000052 down migration, by contrast, *does* carry
`inserted_at` through both copy steps (down.sql:30,46,63), so up and down are
asymmetric. Semantics are arguably defensible (the row genuinely enters the
partitioned table now, and cold-history filtering uses `timestamp`, not
`inserted_at`), but the silent asymmetry could surprise anyone querying
`inserted_at`.
**Fix:** If original insertion time matters, add `inserted_at` to the
`readLegacyChunk` SELECT and the Backfill INSERT column list; otherwise add a
one-line comment noting the deliberate re-stamp so a future reader doesn't read
it as a bug.

**Resolution (ACCEPTED — documented):** Affects only past-retention backfilled
rows where the exact original `inserted_at` is non-load-bearing (cold-history
filtering uses `timestamp`, not `inserted_at`). The row genuinely enters the
partitioned table at backfill time, so the re-stamp is defensible. No code
change.

### IN-02: Pre-migrate deploy probe does a full `count(*)` on a potentially large audit table

**File:** `.github/workflows/deploy.yaml:123-124`
**Issue:** The backfill-budget warning runs
`SELECT count(*) FROM events_audit` immediately before `core migrate`. This is
the exact full-aggregate-scan pattern `bootstrap_orphan.go:30-33` deliberately
avoids (it uses an EXISTS probe and only counts on the failure path) because the
scan "can noticeably delay startup" on large audit tables — here it runs during
the deploy window on the very table sized to matter. Purely diagnostic, so the
cost is bounded, but it is the anti-pattern the codebase already recognized.
**Fix:** Use an estimate instead of an exact count, e.g.
`SELECT reltuples::bigint FROM pg_class WHERE oid = 'public.events_audit'::regclass`
for the >500k threshold check — cheap and adequate for a "warn if big" gate.

**Resolution (FIXED — commit `606d0bcd3`):** Replaced the `count(*)` aggregate
scan with the suggested `pg_class.reltuples` estimate. Non-fatal warning
heuristic and surrounding deploy choreography preserved exactly.

---

_Reviewed: 2026-07-15T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
