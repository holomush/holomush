# Phase 06: Operational Hardening & Assurance Gates - Pattern Map

**Mapped:** 2026-07-13
**Files analyzed:** 14 target files (5 new, 7 modified, 1 deleted, 1 doc)
**Analogs found:** 13 / 14 (osv-scanner.toml has no in-repo analog — external-tool config)

The research (`06-RESEARCH.md` §(b) per requirement) already grounds every target with `path:line`. This map verifies those analogs and extracts the load-bearing excerpts each executor copies from.

---

## File Classification

| Target file | New/Mod | Role | Data flow | Closest analog | Match |
|-------------|---------|------|-----------|----------------|-------|
| `internal/eventbus/audit/retention_partitions.go` | NEW | service (PartitionManager impl) | batch / DDL | `internal/audit/partition_creator.go` | role+flow (partial — only `EnsurePartitions` exists) |
| `internal/store/migrations/000052_events_audit_partition.{up,down}.sql` | NEW | migration | schema DDL | `000001_baseline.up.sql:285-308` (partitioned `access_audit_log`) + `000009_create_events_audit.up.sql` (table being converted) | exact (partitioned-table shape) |
| `internal/eventbus/audit/subsystem.go` | MOD | subsystem lifecycle | event-driven / lifecycle | itself (`Start`/`Stop` at `:202,270`) + `internal/audit/retention.go` (`RetentionWorker.Start/Stop`) | exact |
| `internal/eventbus/audit/projection.go:414-421` | MOD | service (write path) | CRUD (idempotent INSERT) | itself — the ON CONFLICT block | self (crux edit) |
| `internal/eventbus/config.go` (retention cfg) | MOD | config | — | `internal/audit/retention.go:16-29` (`RetentionConfig`/`DefaultRetentionConfig`) | exact |
| `cmd/holomush/cmd_audit.go` | MOD | CLI command | request-response | itself (`runAuditDLQReplay:298`, `dlqConfigForGame:337`) + `internal/store/postgres.go:70,97` (`GetSystemInfo`) | self |
| `internal/eventbus/audit/dlq_replay_integration_test.go` | REWRITE | test (integration) | event-driven | `test/integration/resilience/chaos_helpers_test.go` (`startExternalNATS`) + `internal/testsupport/natstest/nats.go` | role+flow |
| `Taskfile.yaml` `lint:vuln` | NEW | build task | CI gate | `Taskfile.yaml:709-744` (`lint:no-timestamptz`) | role (self-contained `set -euo pipefail` block) |
| `osv-scanner.toml` | NEW | config | — | (none in-repo) | none |
| `.github/workflows/ci.yaml` `vuln:` job | NEW | CI job | CI gate | `ci.yaml:36-97` (`lint:` job scaffold) | exact |
| `.codecov.yml` project status | MOD | config | — | itself `:23-32` | self |
| `codecov.yml` | DELETE | config | — | — | — |
| `.claude/rules/testing.md` + `CLAUDE.md` coverage rows | MOD | docs | — | — | doc edit |
| `go.mod:22` / `go.sum` | MOD | deps | — | — | mechanical bump |

---

## OPS-02 — events_audit retention (partition + wire RetentionWorker)

### `internal/eventbus/audit/retention_partitions.go` (NEW — PartitionManager impl)

**Analog:** `internal/audit/partition_creator.go` — the DDL/naming pattern (only `EnsurePartitions` exists today; `Detach`/`Drop`/`Purge`/`HealthCheck` are test mocks, so the executor writes genuinely new methods).

**EnsurePartitions DDL + naming to copy verbatim, retargeting the table** (`partition_creator.go:29-62`):
```go
func (c *PostgresPartitionCreator) EnsurePartitions(ctx context.Context, months int) error {
	now := time.Now().UTC()
	for i := 0; i < months; i++ {
		t := now.AddDate(0, i, 0)
		name, start, end := partitionRange(t)
		// bounds are int64 ns; range_end exclusive (BIGINT epoch-ns column)
		query := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF access_audit_log FOR VALUES FROM (%d) TO (%d)`,
			name, start.UnixNano(), end.UnixNano())
		if _, err := c.pool.Exec(ctx, query); err != nil { /* oops.With(...).Errorf */ }
	}
	return nil
}
// partitionRange: name = fmt.Sprintf("access_audit_log_%04d_%02d", t.Year(), t.Month())
//   start = first-of-month UTC; end = start.AddDate(0,1,0)
```
**Adaptation:** retarget `access_audit_log` → `events_audit`, naming `events_audit_%04d_%02d`, partition on `event_ms` (int64-ns). `EnsurePartitions` covers the retention window **backward** (derived from the operator RetainWindow) AND `months` forward. Add `DetachExpiredPartitions` (`ALTER TABLE events_audit DETACH PARTITION ... CONCURRENTLY` — MUST run outside an explicit tx; `pool.Exec` autocommits — THEN rename child → `events_audit_<YYYY_MM>_detached_<unix>` to stamp the grace clock), `DropDetachedPartitions` (discover by the `events_audit_%_detached_%` name pattern, parse the epoch, `DROP TABLE` past grace), `PurgeExpiredAllows` → **no-op `return 0, nil`** (events_audit has no allow/deny split), `HealthCheck` → cheap `SELECT 1 FROM events_audit LIMIT 0`.

**Crash-atomicity (round-2 review finding 7):** `DETACH … CONCURRENTLY` runs outside a tx and the rename is a separate statement — a crash between them leaves a canonical-named `events_audit_YYYY_MM` that is no longer a child and lacks the `_detached_<unix>` suffix (stranded). Each detach cycle MUST first **reconcile**: find any `events_audit_YYYY_MM` that is NOT a current child of `events_audit` (absent from `pg_inherits`) and rename it to the `_detached_<now>` form before proceeding.

**Legacy `Backfill(ctx)` (round-2 review finding 4; the migration leaves rows in `events_audit_unpartitioned`):** re-home each legacy row into the partitioned `events_audit`, computing `event_ms` via the SAME shared helper as `writeAuditRow` (`ulid.Time(parsedID.Time()).UnixNano()`), `EnsurePartitions` over the legacy range first, `ON CONFLICT (id, event_ms) DO NOTHING`, chunked; then rename/drop `events_audit_unpartitioned`. Idempotent (no-op when the old table is gone).

**Interface it must satisfy** (`internal/audit/retention.go:31-38`):
```go
type PartitionManager interface {
	EnsurePartitions(ctx context.Context, months int) error
	PurgeExpiredAllows(ctx context.Context, olderThan time.Time) (int64, error)
	DetachExpiredPartitions(ctx context.Context, olderThan time.Time) ([]string, error)
	DropDetachedPartitions(ctx context.Context, gracePeriod time.Duration) ([]string, error)
	HealthCheck(ctx context.Context) error
}
```

### `000052_events_audit_partition.{up,down}.sql` (NEW migration)

> **REVISED after round-2 cross-AI review** (06-REVIEWS.md). The design below
> SUPERSEDES the earlier `PARTITION BY RANGE (timestamp)` + `ATTACH`-of-old-table
> shape. Load-bearing changes: (a) a **separate deterministic `event_ms BIGINT
> NOT NULL`** partition key — NOT the `timestamp` column, which is left untouched;
> (b) **NO DEFAULT partition**; (c) legacy rows are **left in
> `events_audit_unpartitioned`** and re-homed by a **Go `Backfill`** (06-02),
> never ATTACHed; (d) the legacy PK/indexes are **renamed to `_legacy`** before
> the new parent is created (name-collision fix); (e) a **data-preserving down**.

**Analog A — partitioned-table shape** (`000001_baseline.up.sql:287-308`): mirror
the SHAPE (composite PK includes the partition column, BRIN + btree indexes), but
partition on the NEW `event_ms` column, not `timestamp`:
```sql
CREATE TABLE access_audit_log (
    id               TEXT NOT NULL,
    ...
    PRIMARY KEY (id, timestamp)          -- composite PK MUST include partition col
) PARTITION BY RANGE (timestamp);
```
**Analog B — table being converted** (`000009_create_events_audit.up.sql`, but the
CURRENT column set is post-000038: `timestamp`/`inserted_at` are **BIGINT epoch-ns**,
`payload`→`envelope` (000017), `rendering` (000012), `dek_ref`/`dek_version`
(000014) — reproduce the POST-000038 column set, NOT 000009 verbatim). Existing
relation names 000009→000014 created: PK `events_audit_pkey`, indexes
`events_audit_subject_id`, `events_audit_subject_ts`, `events_audit_subject_pat`,
`events_audit_subject_js_seq` (000011), `events_audit_dek_ref` (000014).
**Adaptation (revised):**
1. Regclass-guarded `RENAME events_audit → events_audit_unpartitioned` (only when
   `events_audit` exists AND relkind != 'p' AND `events_audit_unpartitioned` absent).
2. **Rename the legacy PK + every `events_audit_*` index to `_legacy`** on the
   renamed table (`ALTER TABLE events_audit_unpartitioned RENAME CONSTRAINT
   events_audit_pkey TO events_audit_pkey_legacy`; `ALTER INDEX events_audit_subject_id
   RENAME TO events_audit_subject_id_legacy`; …) inside a DO-block guarded on
   `to_regclass`/catalog existence (the 000017/000038 idempotency idiom) — otherwise
   the new parent's PK/index names collide and `CREATE INDEX IF NOT EXISTS` can
   silently reuse the legacy index, leaving the new parent unindexed.
3. `CREATE TABLE events_audit ( <post-000038 columns> , event_ms BIGINT NOT NULL )
   PARTITION BY RANGE (event_ms)` with composite `PRIMARY KEY (id, event_ms)`;
   recreate the original-named indexes on the parent + a BRIN index on `event_ms`.
4. Create the CURRENT + next-2 monthly `event_ms` partitions inline (naming
   `events_audit_%04d_%02d`, int64-ns FROM/TO). **NO DEFAULT partition** (a DEFAULT
   forbids `DETACH … CONCURRENTLY` in 06-02 and never prunes). Do NOT ATTACH/copy
   the legacy rows — 06-02's `Backfill` re-homes them.
**Migration-rule compliance:** idempotent (`IF EXISTS`/`IF NOT EXISTS`/regclass/DO-guard),
paired data-preserving down, no persisted triggers/functions (anonymous DO-blocks are
fine; DETACH/DROP lives in the Go worker), no in-migration backfill. Every column
BIGINT (INV-STORE-1 / `lint:no-timestamptz` `Taskfile.yaml:709`) — do NOT reintroduce
TIMESTAMPTZ. **Down:** copy partitioned rows (+ surviving `events_audit_unpartitioned`
rows) into a temp restored table, DROP the partitioned parent+children FIRST (frees the
original index/PK names), then rename temp → `events_audit` and create the original PK/indexes.

### `internal/eventbus/audit/projection.go` writeAuditRow (MOD — idempotency crux)

**Current block to change** (`projection.go:414-435`, the INSERT; `idBytes, err :=
decodeULIDString(msgID)` at :409 — `decodeULIDString` returns `([]byte, error)`, NOT a
parsed ULID):
```go
_, err = pool.Exec(ctx, `
	INSERT INTO events_audit (
		id, subject, type, timestamp, actor_kind, actor_id,
		envelope, schema_ver, codec, js_seq, rendering,
		dek_ref, dek_version
	) VALUES ($1, ...)
	ON CONFLICT (id) DO NOTHING`,        // ← FAILS on composite PK
	idBytes, subject, eventType,
	pgnanos.From(meta.Timestamp),         // ← LEAVE THIS UNCHANGED (store-time)
	...)
```
**Adaptation (the highest-risk edit) — REVISED:**
- Add `event_ms BIGINT` to the INSERT column list + placeholder.
- Compute `event_ms` **deterministically from the event ULID** via a shared package
  helper (both `writeAuditRow` and 06-02's `Backfill` are in `internal/eventbus/audit`,
  so they share it): `parsedID, err := ulid.Parse(msgID)`; `event_ms :=
  ulid.Time(parsedID.Time()).UnixNano()`. NOTE the API: `oklog/ulid/v2@v2.1.1` defines
  `func (id ULID) Time() uint64` (embedded ms) and `func Time(ms uint64) time.Time` —
  so it MUST be `ulid.Time(parsedID.Time())`, NOT `ulid.Time(parsedID)` (which does not
  compile). Equivalent form: `int64(parsedID.Time()) * int64(time.Millisecond)`.
- Change the conflict target `ON CONFLICT (id) DO NOTHING` →
  `ON CONFLICT (id, event_ms) DO NOTHING` (matches the composite PK).
- **LEAVE `timestamp` = `pgnanos.From(meta.Timestamp)`** (JetStream store-time). This is
  the whole point of the separate `event_ms` key: cold_postgres.go filters on `timestamp`
  (`:159-183`), so keeping its store-time meaning preserves the hot/cold tier boundary
  with no parity test. `writeAuditRow` is shared by live projection (`projection.go:329`)
  and DLQ replay (`replay.go:219`), so `event_ms` (identical per event) dedups the same
  event across both paths even when store-times differ.

### `internal/eventbus/audit/subsystem.go` (MOD — wire the worker) + `internal/eventbus/config.go` (retention cfg)

**Analog — worker lifecycle** (`internal/audit/retention.go:63-117`): `RunOnce` already does Ensure→Purge→Detach→Drop; `Start(ctx)` spawns `go w.run(ctx)`; `Stop()` cancels + `wg.Wait()`. **Home the worker inside `SubsystemAuditProjection`** to avoid the `productionSubsystems` named-param cascade (research A5). The audit `Subsystem.Start` (`subsystem.go:202-261`) already resolves the pool via `s.poolProv.Pool()` and holds `s.cancel` — construct `audit.NewRetentionWorker(cfg, mgr)` there and `.Start(workerCtx)`; mirror the existing rollback/drain discipline; `.Stop()` in `Subsystem.Stop` (`:270-293`).

**Config analog** (`retention.go:16-29`) — add a retention section to `audit.Config` (`subsystem.go:65-118`, extend `Defaults()`):
```go
type RetentionConfig struct {
	RetainDenials time.Duration; RetainAllows time.Duration; PurgeInterval time.Duration
}
func DefaultRetentionConfig() RetentionConfig {
	return RetentionConfig{RetainDenials: 90*24*time.Hour, RetainAllows: 7*24*time.Hour, PurgeInterval: 24*time.Hour}
}
```
**Adaptation:** single window `RetainWindow` (default 90d, D-02) mapped onto `RetentionConfig.RetainDenials`; `RetainAllows` unused (no-op purge). Wire into `Config.Defaults()` at `subsystem.go:100`.

---

## OPS-03 — nats-server CVE + vuln-scan gate

### `Taskfile.yaml` `lint:vuln` (NEW task)

**Analog — self-contained lint task** (`Taskfile.yaml:709-744`, `lint:no-timestamptz`):
```yaml
lint:no-timestamptz:
  desc: "Reject new TIMESTAMPTZ ... columns (INV-STORE-1)"
  cmds:
    - |
      set -euo pipefail
      ...
      if [[ "$violations" -gt 0 ]]; then
        echo "FAIL: ..."; exit 1
      fi
```
**Adaptation:** `set -euo pipefail`; run `govulncheck ./...` (reachability, Go-vulndb) then `osv-scanner --config=osv-scanner.toml ./...` (OSV DB includes GHSA → catches nats-server GHSA-q59r-vq66-pxc2). Fail closed on any unlisted finding. **Keep OUT of the `lint:` umbrella `cmds`** (`:116` area) — the scanners download vuln DBs and would slow every local `task lint`; invokable standalone.

### `.github/workflows/ci.yaml` `vuln:` job (NEW)

**Analog — job scaffold** (`ci.yaml:36-97`, `lint:` job) — copy checkout + setup-go + cache + install-task, then a checksum-pinned install step modeled on the inline buf install (`ci.yaml:66-75`):
```yaml
- name: Install buf
  run: |
    BUF_VERSION="1.67.0"; BUF_SHA256="590b...e2a"
    curl -LsSfo /tmp/buf "$BUF_URL"
    echo "${BUF_SHA256}  /tmp/buf" | sha256sum -c -
    sudo install -m 0755 /tmp/buf /usr/local/bin/buf
```
**Adaptation:** new `vuln:` job (sibling of `lint`/`test`/`integration`/`e2e`). Pin govulncheck via `go install golang.org/x/vuln/cmd/govulncheck@<ver>` / tool module; pin osv-scanner with the same SHA256-verify pattern (or a checksum-pinned composite under `.github/actions/install-*`). Job runs `task lint:vuln`. Add to the protect-main ruleset for blocking (operator step — flag it).

### `osv-scanner.toml` (NEW — no in-repo analog)
Allowlist via `[[IgnoredVulns]]` (`id`, optional `ignoreUntil`, `reason`) — this is the documented-allowlist mechanism D-04 requires (govulncheck has none). Seed empty or with a `reason`-documented entry only if a reachable-but-accepted CVE surfaces.

### `go.mod:22` — mechanical bump `nats-server/v2 v2.14.2 → v2.14.3`, then `go mod tidy`. `nats.go`/`prometheus-nats-exporter` NOT implicated (server-only CVEs).

---

## OPS-04 — DLQ replay game_id bridge + non-tautological test

### `cmd/holomush/cmd_audit.go` (MOD)

**Site to fix** (`cmd_audit.go:325`, inside `runAuditDLQReplay`):
```go
res, err := audit.ReplayDLQ(cmd.Context(), js, pool, dlqConfigForGame(cfg.GameID), opts)
```
`dlqConfigForGame("")` (`:337-343`) yields an empty-Subject config → `Defaults()` → `internal.main.audit.dlq`, mismatching the server's `internal.<ULID>.audit.dlq` → every dead letter counts Failed (the F3 bug).

**Resolve analog — the server's own read path** (`internal/store/postgres.go:70` `GetSystemInfo`, `:97-99` `InitGameID`):
```go
func (s *PostgresEventStore) InitGameID(ctx context.Context) (string, error) {
	gameID, err := s.GetSystemInfo(ctx, "game_id")   // key="game_id"; ErrSystemInfoNotFound if absent
	...
}
```
**Adaptation (D-05):** add `cmd.Flags().String("game-id", "", "...")` in `newAuditDLQReplayCmd:107-119`; in `runAuditDLQReplay` after `openAuditPool` (`:319`) resolve: (1) `--game-id` override → use it; (2) else `GetSystemInfo(ctx, "game_id")` (or a direct `SELECT value FROM holomush_system_info WHERE key='game_id'` on the open pool); (3) else `cfg.GameID`. Pass resolved id to `dlqConfigForGame`. Mirrors `core.go:~300-304` server resolution order.

### `internal/eventbus/audit/dlq_replay_integration_test.go` (REWRITE)

**Current tautology** (`:27,38,107`): `replayDLQSubject = "internal.main.audit.dlq"` used as **both** seed prefix and replay prefix — same `"main"` on both sides, so the F3 mismatch is never exercised; and it uses embedded `eventbustest` (`:102`), which D-06 forbids for external-NATS DLQ behavior.

**Analog — real single-node NATS** (`internal/testsupport/natstest/nats.go`): `StartNATS(ctx) (*NATSEnv, error)` (`:77`), `env.Conn(t) *nats.Conn` (`:60`), `env.Terminate(ctx)` (`:47`). Reference usage: `test/integration/resilience/chaos_helpers_test.go` (`startExternalNATS`). Keep the existing `provisionDLQStream` seed helper shape (`dlq_replay_integration_test.go:32-45`) but drive it from `natstest`, not `eventbustest`.

**Adaptation (D-06):** `//go:build integration`; seed the DLQ under a server-style ULID prefix `internal.<ULID>.audit.dlq.<orig-subject>`; assert BOTH (i) wrong/empty CLI game → `res.Failed>0`, 0 rows (regression guard) and (ii) resolved/overridden game → `res.Replayed==1`, correct `events_audit.subject`. `natstest` requires Docker + `task test:int`; production code MUST NOT import it (depguard).

---

## QUAL-01 — coverage reconciliation

### `.codecov.yml` (MOD) + `codecov.yml` (DELETE)

**Block to change** (`.codecov.yml:23-32`):
```yaml
coverage:
  status:
    project:
      default:
        target: 80%      # → target: auto  (ratchet = parent commit's coverage)
        threshold: 2%    # → threshold: 1%  (absorb two-upload merge jitter)
    patch:
      default:
        target: 80%      # KEEP — already the ruleset-enforced hard gate
        threshold: 5%
```
**Adaptation:** `project.default → { target: auto, threshold: 1% }` (D-07 rising-floor ratchet; baseline ~54.6% not retroactively blocked). Keep `patch` unchanged. **Delete `codecov.yml`** (375 B, ignore-only subset of `.codecov.yml`'s ignore list — D-08); `.codecov.yml` `notify.after_n_builds: 2` (`:20`) is why a pending status ≠ failure.

### `.claude/rules/testing.md` + `CLAUDE.md` (doc MOD)
> **CORRECTED after round-2 review** (the earlier "codecov/patch @ 80% is a hard
> merge gate" claim is FALSE against live state — VERIFIED via `gh api
> repos/holomush/holomush/rulesets/11923801`: required checks are `[Build, Lint,
> Test, CodeRabbit, Integration Test, E2E Test]`; NEITHER `codecov/patch` NOR
> `codecov/project` is required.)

Rewrite the fictional "MUST maintain >80% coverage | Per-package" rows to the TRUE
enforced reality: `codecov/patch` and `codecov/project` **POST statuses but are NOT
currently required checks** — they block only when added to the protect-main ruleset
(`branches/main/protection` 404 is expected — enforcement is via ruleset, not classic
branch protection); codecov measures patch + project, **never per-package**. Drop the
"SHOULD 90%+ per-package" line.

### Non-YAML operator step (FLAG, not in-repo) — MANDATORY blocking checkpoint
Making `codecov/patch`, `codecov/project`, AND the OPS-03 `vuln` check *block* merges
requires adding them to the protect-main **ruleset** required checks. Adding a status to
`.codecov.yml` / the CI job only makes it *post/run*, not block. This is a **mandatory
human-action checkpoint** (round-2 review finding 6), NOT an open follow-up — the
consolidated protect-main assurance-gate checklist (`vuln` + `codecov/patch` +
`codecov/project`) is enumerated in **06-04 Task 3**; **06-03 Task 4** independently
blocks on `vuln` (D-04 mandates it). Verify with `gh api …/rulesets/11923801`.

---

## Shared Patterns

### Error handling
**Source:** `internal/audit/partition_creator.go:44-50`, `cmd_audit.go` throughout.
Structured `oops` at boundaries: `oops.With(k, v).Errorf("...: %w", err)` / `oops.Code("AUDIT_...").Wrap(err)`. New CLI/worker code follows the same code-prefixed style.

### Structured logging
**Source:** `internal/audit/retention.go:69,79` — `w.logger.ErrorContext(ctx, ...)` / `InfoContext`. All new worker/CLI code with a `ctx` in scope MUST use `*Context` slog variants (`.claude/rules/logging.md`).

### SPDX headers
Every new `.go` / `.sql` / task file: `// SPDX-License-Identifier: Apache-2.0` + copyright line (applied by `task fmt`). Skip generated files.

## No Analog Found

| File | Role | Reason |
|------|------|--------|
| `osv-scanner.toml` | config | External-tool config; no in-repo precedent. Format is the tool's `[[IgnoredVulns]]` schema. |

## Metadata

**Analog search scope:** `internal/audit/`, `internal/eventbus/audit/`, `internal/access/policy/`, `internal/store/migrations/`, `internal/store/`, `cmd/holomush/`, `internal/testsupport/natstest/`, `.github/workflows/`, `Taskfile.yaml`, `.codecov.yml`/`codecov.yml`.
**Files scanned:** ~15 (all path:line claims in RESEARCH verified this session).
**Pattern extraction date:** 2026-07-13
