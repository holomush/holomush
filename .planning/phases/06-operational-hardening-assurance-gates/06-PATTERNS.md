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
**Adaptation:** retarget `access_audit_log` → `events_audit`, naming `events_audit_%04d_%02d`, partition on `event_ms` (int64-ns). `EnsurePartitions` covers the retention window **backward** (derived from the operator RetainWindow) AND `months` forward, and (round-4 F9) stamps a **durable provenance marker** `COMMENT ON TABLE <partition> IS 'holomush:events_audit_partition'` on each created partition. Add `DetachExpiredPartitions` (`ALTER TABLE events_audit DETACH PARTITION ... CONCURRENTLY` — MUST run outside an explicit tx; `pool.Exec` autocommits — THEN rename child → `events_audit_<YYYY_MM>_detached_<unix>` to stamp the grace clock; the COMMENT survives DETACH + rename), `DropDetachedPartitions` (discover by the `events_audit_%_detached_%` name pattern **schema-qualified AND gated on the `holomush:events_audit_partition` marker** via `obj_description(c.oid,'pg_class')`, parse the epoch, `DROP TABLE` past grace — round-4 F9: never drop a marker-less same-named table), `PurgeExpiredAllows` → **no-op `return 0, nil`** (events_audit has no allow/deny split), `HealthCheck` → cheap `SELECT 1 FROM events_audit LIMIT 0`.

**Crash/interrupt-atomicity (round-2 finding 7 + round-3 finding 2 + round-4 F9):** `DETACH … CONCURRENTLY` runs in TWO internal transactions outside an explicit tx, and the rename is a separate statement. Each detach cycle MUST run TWO recovery passes BEFORE any new detach, both schema-qualified: (1) **FINALIZE** — find any child marked `pg_inherits.inhdetachpending = true` (an INTERRUPTED concurrent detach PG left mid-way — still a child), run `ALTER TABLE events_audit DETACH PARTITION <child> FINALIZE`, then rename to the `_detached_<now>` form; (2) **RECONCILE** — find any canonical-named `events_audit_YYYY_MM` that is NOT a current child (absent from `pg_inherits`), lacks the `_detached_<unix>` suffix, AND carries the durable `holomush:events_audit_partition` provenance marker (a completed detach whose rename crashed) and rename it to `_detached_<now>`. **The marker (round-4 F9) is the load-bearing provenance guard** — schema+name+shape alone cannot prove a table absent from `pg_inherits` was formerly an events_audit child, so the marker (not shape) is what makes reconcile/drop safe against a coincidentally-named non-child table.

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
rows) into a temp restored table — BOTH copies use `ON CONFLICT (id) DO NOTHING` so a resumed
partial down is idempotent (round-3 MEDIUM) — DROP the partitioned parent+children FIRST (frees
the original index/PK names), then rename temp → `events_audit` and create the original PK/indexes.

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
**Adaptation:** single window `RetainWindow` (default 90d, D-02) mapped onto `RetentionConfig.RetainDenials`; `RetainAllows` unused (no-op purge). Wire into `Config.Defaults()` at `subsystem.go:100`. **Validate (round-3 finding 3):** Defaults() fills only ZERO values, so a NEGATIVE survives — a validation step MUST REJECT `RetainWindow <= 0` (would make `now.Add(-RetainDenials)` future-facing → detach every partition, retention.go:83) and `PurgeInterval <= 0` (panics `time.NewTicker`, retention.go:130) in the tested audit.Config surface (unit-tested in `internal/eventbus/audit/subsystem_test.go`, round-4 LOW), surfaced as a Start error. **Initial-delay (round-4 MEDIUM):** `RetentionWorker.run` (retention.go:133-136) RunOnces IMMEDIATELY before the ticker, so even with the Backfill+Ensure-only boot gate the periodic worker still fires a destructive Detach/Drop the instant it is started. Add an initial-delay functional option to `RetentionWorker` (e.g. `WithInitialDelay`/`WithSkipFirstRun`) that skips the immediate RunOnce; wire the audit worker with it so the first destructive cycle only fires after the first ticker tick / process readiness (the default no-option behavior is preserved for the ABAC access_audit worker). Unit-test in `internal/audit/retention_test.go`.

### `.github/workflows/deploy.yaml` + `sandbox-operations.md` — deploy choreography (NEW, round-4 HIGH)

**Why:** the production deploy runs `core migrate` BEFORE recreating services (`deploy.yaml` migrate at `:107`, `up -d` at `:108`; manual runbook `sandbox-operations.md:235-239`), so after 000052 the still-running OLD core's `INSERT ... ON CONFLICT (id)` with no `event_ms` (`projection.go:414`) fails on BOTH the NOT NULL event_ms and the dropped id-alone unique index. **Operator decision: deploy CHOREOGRAPHY, NOT expand/contract — 000052 stays single-step.**

**Analog — the existing deploy heredoc** (`deploy.yaml:93-108`): `pull` → `build backup` → `up -d --no-recreate postgres` → `run --rm -T core migrate </dev/null` → `up -d`. **Re-choreograph to:**
```
docker compose ... pull core gateway cloudflared
docker compose ... build backup
docker compose ... up -d --no-recreate postgres
docker compose ... stop core                       # round-4: stop OLD core before migrate (ends the incompatible-write window)
docker compose ... run --rm -T core migrate </dev/null   # migrate with no old core writing (preserve the </dev/null + -T stdin guard, holomush-aocap)
docker compose ... up -d --wait --no-deps core     # start NEW core; --wait blocks on its healthcheck = the synchronous audit Backfill+Ensure boot gate (06-02 Task 3)
docker compose ... up -d                            # restore player traffic (gateway/cloudflared) only after core is ready
```
The core healthcheck (`compose.prod.yaml:85-90`, `wget --spider http://127.0.0.1:9100/healthz/readiness`) is what `--wait` gates on; a failed backfill/readiness gate makes `up -d --wait` exit non-zero and `set -euo pipefail` aborts the deploy BEFORE traffic is restored (the compensating control). The brief audit-write outage is an accepted, bounded single-node risk. Apply the IDENTICAL reorder to the manual runbook. Verify statically (line-order: stop < migrate < wait-gated start).

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
**Adaptation:** `set -euo pipefail`; run `govulncheck ./...` (reachability, Go-vulndb) then OSV-Scanner **v2**: `osv-scanner scan source -L go.mod --config=osv-scanner.toml` (the v2 `scan source` subcommand form — NOT the v1 `osv-scanner --config=... ./...` shape; the OSV DB includes GHSA → catches nats-server GHSA-q59r-vq66-pxc2). Fail closed on any unlisted finding, decided by EXIT CODE (never by grepping scanner stdout). **Keep OUT of the `lint:` umbrella `cmds`** (`:116` area) — the scanners download vuln DBs and would slow every local `task lint`; invokable standalone. **Local install first (round-4 MEDIUM):** the env has govulncheck but NOT osv-scanner, so before the Task-2 pre-bump proof, install the Task-1-pinned checksum-verified `osv-scanner` v2 binary (mirror the `ci.yaml:66-75` buf `curl → sha256sum -c - → install` pattern, or `go install .../v2/cmd/osv-scanner@v2.Y.Z`) into a temp/local tool dir on PATH — otherwise the pre-bump proof is deterministically blocked. CI install (Task 3) stays separately checksum-pinned.

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
**Adaptation:** new `vuln:` job (sibling of `lint`/`test`/`integration`/`e2e`) with an EXPLICIT stable `name: Vuln`. Pin govulncheck via `go install golang.org/x/vuln/cmd/govulncheck@<ver>` / tool module; pin osv-scanner **v2** with the same SHA256-verify pattern (or a checksum-pinned composite under `.github/actions/install-*`). Job runs `task lint:vuln`. Add to the protect-main ruleset for blocking (operator step — flag it). **The required-check context is the RENDERED job name `Vuln`, NOT the workflow key `vuln`** — GitHub rulesets match the rendered name (ci.yaml's `lint:` key renders `name: Lint`, which is what the live ruleset requires). Verify the added check in a real PR's `statusCheckRollup`, not only ruleset JSON.

### `osv-scanner.toml` (NEW — no in-repo analog)
Allowlist via `[[IgnoredVulns]]` (`id`, `ignoreUntil` EXPIRY, `reason`) — the documented-allowlist mechanism D-04 requires (govulncheck has none). **Seed it EMPTY as a syntactically-valid `IgnoredVulns = []` line, NOT a bare `[[IgnoredVulns]]` header** (an empty array-of-tables header creates an entry with no `id`, which is invalid). Add a `reason`+`ignoreUntil`-documented entry only if a reachable-but-accepted OSV-scoped CVE surfaces. Validate by running osv-scanner against the config (a malformed config errors there).

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
**Adaptation (D-05) — MIRRORS THE SERVER (core.go:300-303), NOT the rejected DB-before-config order:** add `cmd.Flags().String("game-id", "", "...")` in `newAuditDLQReplayCmd:107-119`; in `runAuditDLQReplay` after `openAuditPool` (`:319`) resolve in the SERVER's order: (1) non-empty `--game-id` override → use it; (2) else `core.game_id` — the `coreConfig.GameID` field loaded via `config.Load(configFile, cmd, &coreCfg, "core")` exactly as core.go:125 does (NOT `event_bus.game_id`, NOT the YAML root, and WITHOUT `event_bus.Config.Defaults()`, which forces "main"); (3) else the persisted `GetSystemInfo(ctx, "game_id")` DB value. Pass the resolved id to `dlqConfigForGame`. **Do NOT put DB before `core.game_id`** (the round-2-rejected order) — that diverges from the server whenever an explicit `core.game_id` differs from the DB value.

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
        target: 80%      # KEEP — POSTS an 80%-on-changed-lines status; NOT a required ruleset check today (accepted-deferred, see 06-04)
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

### Non-YAML operator step (FLAG, not in-repo) — one consolidated ruleset edit
Making a check *block* merges requires adding it to the protect-main **ruleset** required
checks (adding a status to `.codecov.yml` / the CI job only makes it *post/run*, not block).
**The required-check context is the RENDERED job name** (`Lint`, not `lint`; the OPS-03 job
must be added as the rendered `Vuln`, not the key `vuln`). Split disposition (round-3 F6
resolution):
- **MANDATORY** — the OPS-03 rendered `Vuln` check (D-04). A **mandatory blocking
  human-action checkpoint**, NOT an open follow-up: **06-03 Task 4** independently blocks on it.
- **OPTIONAL / ACCEPTED-DEFERRED** — `codecov/patch` + `codecov/project` (the QUAL-01 ratchet).
  Deferring them is a legitimate, documented outcome; QUAL-01 criterion #4 is met by the doc
  correction + the `.codecov.yml` ratchet either way. Do NOT call codecov "mandatory".
The consolidated checklist is enumerated in **06-04 Task 3**. Verify with
`gh api …/rulesets/11923801` AND a real PR's `statusCheckRollup`.

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
