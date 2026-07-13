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
**Adaptation:** retarget `access_audit_log` → `events_audit`, naming `events_audit_%04d_%02d`. Add `DetachExpiredPartitions` (`ALTER TABLE events_audit DETACH PARTITION ... CONCURRENTLY` — MUST run outside an explicit tx; `pool.Exec` autocommits), `DropDetachedPartitions` (`DROP TABLE` past grace), `PurgeExpiredAllows` → **no-op `return 0, nil`** (events_audit has no allow/deny split), `HealthCheck` → cheap `SELECT 1 FROM events_audit LIMIT 0`.

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

**Analog A — partitioned-table shape** (`000001_baseline.up.sql:287-308`):
```sql
CREATE TABLE access_audit_log (
    id               TEXT NOT NULL,
    timestamp        TIMESTAMPTZ NOT NULL DEFAULT now(),
    ...
    PRIMARY KEY (id, timestamp)          -- composite PK MUST include partition col
) PARTITION BY RANGE (timestamp);

CREATE INDEX idx_audit_log_timestamp ON access_audit_log USING BRIN (timestamp)
    WITH (pages_per_range = 128);
CREATE INDEX idx_audit_log_subject ON access_audit_log(subject, timestamp DESC);
```
**Analog B — table being converted** (`000009_create_events_audit.up.sql`, note `timestamp`/`inserted_at` are now **BIGINT epoch-ns** after `000038`, so partition bounds are int64 ns exactly like `access_audit_log`):
```sql
CREATE TABLE IF NOT EXISTS events_audit (
    id           BYTEA       PRIMARY KEY,   -- becomes PRIMARY KEY (id, timestamp)
    subject      TEXT NOT NULL, type TEXT NOT NULL, timestamp <BIGINT ns>, ...
);
CREATE INDEX events_audit_subject_id  ON events_audit (subject, id);
CREATE INDEX events_audit_subject_ts  ON events_audit (subject, timestamp);
CREATE INDEX events_audit_subject_pat ON events_audit (subject text_pattern_ops);
```
**Adaptation:** per research §OPS-02(a): `RENAME events_audit → events_audit_unpartitioned` (regclass-guarded) → `CREATE TABLE events_audit (...) PARTITION BY RANGE (timestamp)` with composite `PRIMARY KEY (id, timestamp)` and the three indexes recreated on the parent → `ATTACH PARTITION` the old table (recommended) rather than copy rows. **Migration-rule compliance:** idempotent (`IF EXISTS`/`IF NOT EXISTS`/regclass), paired down that cleanly reverts, no triggers/functions (DETACH/DROP lives in the Go worker). Column MUST be BIGINT (INV-STORE-1 / `lint:no-timestamptz` `Taskfile.yaml:709`) — do NOT reintroduce TIMESTAMPTZ.

### `internal/eventbus/audit/projection.go:414-421` (MOD — idempotency crux)

**Current block to change** (`projection.go:414-425`):
```go
_, err = pool.Exec(ctx, `
	INSERT INTO events_audit (
		id, subject, type, timestamp, actor_kind, actor_id,
		envelope, schema_ver, codec, js_seq, rendering,
		dek_ref, dek_version
	) VALUES ($1, ...)
	ON CONFLICT (id) DO NOTHING`,        // ← FAILS on composite PK
	idBytes, subject, eventType,
	pgnanos.From(meta.Timestamp),         // ← non-deterministic live vs replay
	...)
```
**Adaptation (the highest-risk edit):** change conflict target to `ON CONFLICT (id, timestamp) DO NOTHING` AND change the `timestamp` source from `pgnanos.From(meta.Timestamp)` (JetStream store time — differs between live and DLQ-replay for the same event) to a **deterministic** value derived from the event ULID (`ulid.Time(id)` embedded creation ms). `writeAuditRow` is shared by the live projection (`projection.go:330`) and DLQ replay (`replay.go:219`), so this one change covers both paths — which is exactly why replay dedup depends on the same key. Verify no cold-history reader relies on `timestamp == JetStream store time` (research A3).

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
Rewrite the fictional "MUST maintain >80% coverage | Per-package" rows to the enforced reality: **codecov/patch @ 80% is a hard merge gate via the protect-main ruleset** (NOT branch protection — `branches/main/protection` 404 is expected); codecov measures patch + project, **never per-package**. Drop the "SHOULD 90%+ per-package" line.

### Non-YAML operator step (FLAG, not in-repo)
Making `codecov/project` *block* merges requires adding it to the protect-main **ruleset** required checks (same as `codecov/patch` today). Adding the status to `.codecov.yml` only makes codecov *post* it. Call this out explicitly in the plan so it isn't left as "codecov will block."

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
