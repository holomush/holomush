# Phase 6: Operational Hardening & Assurance Gates - Research

**Researched:** 2026-07-13
**Domain:** Postgres time-partitioning + retention workers, Go supply-chain vuln scanning (govulncheck/osv-scanner), NATS DLQ replay, Codecov policy governance
**Confidence:** HIGH (all code claims read this session; CVE/advisory claims CITED from repo finding + GitHub advisories; two clearly-flagged open items at MEDIUM/LOW)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01 (OPS-02 mechanism):** Partition `events_audit` + wire the RetentionWorker (the scale-correct path — O(1) DETACH/DROP, NOT time-based `DELETE`). Deliberately the larger option.
- **D-02 (OPS-02 config):** Retention window MUST be configurable (new audit/event-bus retention config section). Default **90 days** (mirrors ABAC `RetentionConfig.RetainDenials`). Operator-adjustable; not locked.
- **D-03 (OPS-03 bump):** Bump `nats-server/v2` v2.14.2 → ≥ v2.14.3 (`go.mod:22`). Also check `nats.go` (v1.52.0) and `prometheus-nats-exporter` (v0.20.1) against the same advisories.
- **D-04 (OPS-03 gate):** govulncheck, blocking, with an allowlist. New `task lint:vuln` + a `ci.yaml` job. Callgraph-aware. Accepted/unreachable CVEs via a documented allowlist.
- **D-05 (OPS-04 game_id):** Auto-resolve the server's persisted `game_id` from the DB (`holomush_system_info`, CLI pool already open) as default, plus an explicit `--game-id` override flag.
- **D-06 (OPS-04 test):** Replacement test MUST exercise **divergent** server/CLI `game_id` (the F3 mismatch path) using a **real NATS container via `internal/testsupport/natstest`**, not embedded `eventbustest`. Replace, don't augment.
- **D-07 (QUAL-01 direction):** Correct the doc to the real bar (codecov/patch @ 80% is a hard ruleset merge gate; project is a separate status) + add a **project-coverage ratchet** (no-drop threshold). Do NOT frame as "add a patch gate" — it already exists.
- **D-08 (QUAL-01 cleanup):** Resolve the two conflicting codecov files (`.codecov.yml` full vs `codecov.yml` ignore-only) → one authoritative config.

### Claude's Discretion
- Retention window default (90d) and patch-coverage target number.
- govulncheck allowlist file format.
- Partition key column for `events_audit` (`timestamp` vs `inserted_at`).

### Deferred Ideas (OUT OF SCOPE)
- None. (Gateway OOM/F2 shipped as PR #4813; god-object decomposition + code-health sweep are Phases 7–9.)
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| OPS-02 | Bound `events_audit` growth (partition + wire RetentionWorker) — F4 #4786 | §OPS-02: partition-swap migration + PartitionManager impl + wiring seam + idempotency landmine |
| OPS-03 | Remediate nats-server CVE (≥v2.14.3) + add vuln-scan CI gate — F8 #4790 | §OPS-03: exact CVEs, govulncheck DB-gap finding, govulncheck+osv-scanner reconciliation |
| OPS-04 | Fix DLQ replay `game_id` bridge + non-tautological test — F3 #4787 | §OPS-04: resolve seam, `--game-id` flag, natstest divergent-game_id test shape |
| QUAL-01 | Reconcile coverage policy doc-vs-enforced + project ratchet — F7 #4804 | §QUAL-01: doc-edit targets, codecov project status, dual-file cleanup, ruleset action |
</phase_requirements>

## Summary

All four requirements are grounded in the 2026-07-11 arch-review (`docs/reviews/arch-review/2026-07-11/`). The mechanisms are decided (CONTEXT D-01..D-08); this research answers the open MECHANISM questions and surfaces three non-obvious landmines the discuss-phase flags did not reach:

1. **OPS-02 idempotency crux.** Partitioning `events_audit` by time forces the primary key to include the partition column (`PRIMARY KEY (id, <keycol>)` — exactly as `access_audit_log` does). But the audit write path dedups with `ON CONFLICT (id) DO NOTHING` (`internal/eventbus/audit/projection.go:421`), and **Postgres forbids a UNIQUE constraint on `id` alone on a table partitioned by another column**. Worse, the `timestamp` column is currently written from `pgnanos.From(meta.Timestamp)` — the JetStream *store* time, which **differs between the live path and DLQ replay** for the same event — so a naive `ON CONFLICT (id, timestamp)` would let replay insert a duplicate. The fix requires deriving the partition key deterministically per event (the event ULID already embeds its creation ms). This is the single highest-risk decision in the phase.
2. **OPS-03 govulncheck blind spot.** `github.com/nats-io/nats-server/v2` is **not in the Go vulnerability database** (verified: local `govulncheck ./...` this session reports only GO-2026-5932 in x/crypto, unreachable; `vuln.go.dev/index/modules.json` has no nats-server entry). The two nats-server CVEs (GHSA-q59r-vq66-pxc2, GHSA-p957-7v2w-g93g) exist only in the GitHub Advisory DB. **govulncheck therefore cannot make success-criterion #2 true** ("a build pinning a known-vulnerable nats-server fails the gate"). osv-scanner (OSV DB, which ingests GHSA) can. The gate needs both tools, or criterion #2 must be validated against osv-scanner.
3. **OPS-04 is the smallest, cleanest fix** — resolve `game_id` from `holomush_system_info` in the CLI (the read path is `PostgresEventStore.GetSystemInfo(ctx, "game_id")`), add a `--game-id` override, and rewrite the test to seed with a ULID-style game and replay with divergent/empty CLI game.

**Primary recommendation:** Sequence the work OPS-04 → OPS-03 → QUAL-01 → OPS-02 (ascending risk). Land OPS-02 last with the idempotency decision locked before any migration is written.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| events_audit partitioning + pruning | Database / Storage | Core server (lifecycle worker) | DDL owns partitions; a Go worker drives DETACH/DROP (logic-in-Go rule) |
| RetentionWorker lifecycle | Core server subsystem | — | Runs in-process alongside the audit projection subsystem |
| Vuln scanning | CI / Build | Taskfile (`task lint:vuln`) | Supply-chain gate; not a runtime concern |
| DLQ replay + game_id resolve | CLI (`cmd/holomush`) | Database (system_info read) | Operator tool; reads persisted game_id, writes events_audit |
| Coverage policy | CI / repo-settings (ruleset) | in-repo `.codecov.yml` | YAML defines the status; the ruleset makes it *required* |

---

## OPS-02 — Bound `events_audit` growth (partition + wire RetentionWorker)

### (a) Recommended mechanism

**Three coordinated pieces, in this order:**

**1. Partition-swap migration (new `000052_*`, DDL-only, idempotent, paired up/down).**
Postgres cannot partition a table in place. The compliant sequence (no long-running backfill in-migration, per `.claude/rules/database-migrations.md`):
- `ALTER TABLE events_audit RENAME TO events_audit_unpartitioned;` (guarded by an `IF EXISTS`/regclass check so reruns are safe).
- `CREATE TABLE IF NOT EXISTS events_audit (... same columns ...) PARTITION BY RANGE (<keycol>);` with the **composite primary key** (see landmine) and the three existing indexes recreated on the parent.
- Create the current + next N month partitions inline (cheap DDL) — or leave partition creation entirely to the worker's `EnsurePartitions` on first boot (preferred: keeps the migration purely structural). Mirror the naming/bounds convention from `internal/audit/partition_creator.go:57-62` (`events_audit_YYYY_MM`, `FOR VALUES FROM (start.UnixNano()) TO (end.UnixNano())` — both `timestamp` and `inserted_at` are **BIGINT epoch-ns** after migration `000038`, so bounds are int64 ns exactly like `access_audit_log`).
- **Existing-row copy is NOT in the migration.** Two viable disposals of the ~existing rows in `events_audit_unpartitioned`:
  - **(Recommended) `ATTACH PARTITION` the old table as a bounded partition** covering its data range (query `min/max(<keycol>)` first), so no row copy happens at all — the old data is instantly part of the partitioned parent and ages out via normal DETACH/DROP. This is O(1) and needs only a validating scan. If the range is unknown/mixed, attach it as the **DEFAULT partition** (accepts any out-of-range row) — but a DEFAULT partition cannot be range-dropped, so old data won't prune (acceptable for a one-time legacy remnant on a young table).
  - A separate one-shot Go batch-copy job (`INSERT ... SELECT` in chunks) then `DROP TABLE events_audit_unpartitioned`. Heavier; only needed if ATTACH's constraint requirements can't be met.
  - **Note:** `events_audit` today is a young, hobbyist-scale table (arch-review REPORT `:176`); the row count is likely small enough that ATTACH-as-partition is trivially correct.

**2. New `events_audit` `PartitionManager` implementation** (satisfying `internal/audit/retention.go:31-38`). **The full interface has NO production implementation today** — `PostgresPartitionCreator` (`internal/audit/partition_creator.go:17-53`) implements only `EnsurePartitions`; `PurgeExpiredAllows`/`DetachExpiredPartitions`/`DropDetachedPartitions`/`HealthCheck` exist **only as test mocks** (`internal/audit/retention_test.go`). So the planner writes a genuinely new impl (recommend `internal/eventbus/audit/retention_partitions.go` or `internal/audit/events_audit_partitions.go`) with:
  - `EnsurePartitions(ctx, months)` — `CREATE TABLE IF NOT EXISTS events_audit_YYYY_MM PARTITION OF events_audit FOR VALUES FROM (ns) TO (ns)` (copy `partition_creator.go` verbatim, retarget table name).
  - `DetachExpiredPartitions(ctx, olderThan)` — `ALTER TABLE events_audit DETACH PARTITION ... CONCURRENTLY` for partitions fully older than the window (query `pg_partitions`/`pg_inherits` for child ranges).
  - `DropDetachedPartitions(ctx, gracePeriod)` — `DROP TABLE` on detached children past grace.
  - `PurgeExpiredAllows(ctx, olderThan)` — **no-op returning `(0, nil)`** (events_audit has no allow/deny split; retention is a single window).
  - `HealthCheck(ctx)` — cheap `SELECT 1 FROM events_audit LIMIT 0` or partition-existence probe.

**3. Wire the RetentionWorker into the production lifecycle** — it currently has **zero non-test callers** (verified: `NewRetentionWorker`/`RetentionWorker` appear only in `internal/audit/retention.go` + its test). Recommended seam: **start it inside the existing audit projection subsystem** (`internal/eventbus/audit/subsystem.go`, `SubsystemAuditProjection` = `lifecycle/subsystem.go:27`) — that subsystem already owns the `events_audit` write path and holds the DB pool, so co-locating the pruner is coherent and avoids the `productionSubsystems` named-param cascade (adding a new `SubsystemID` forces a signature change in `cmd/holomush/core.go` + ~5 test updates + a stringer regen — see design-review memory "productionSubsystems signature is named-param, not variadic"). The subsystem's `Start` constructs `audit.NewRetentionWorker(cfg, mgr)` and calls `.Start(ctx)`; its `Stop` calls `.Stop()`.

**Config knob (D-02):** add a retention section to the event_bus/audit config (single window `RetainWindow time.Duration`, default 90d, plus `PurgeInterval` default 24h). Map it onto `RetentionConfig` by setting `RetainDenials = window` (the field `RunOnce` passes to `DetachExpiredPartitions`, `retention.go:83`) and leaving `RetainAllows` unused (no-op purge).

### Rejected alternatives
- **Time-based `DELETE` + vacuum** — rejected by D-01 (O(n) + vacuum load).
- **Partition by `inserted_at`** — rejected: `inserted_at` is `DEFAULT now()` and **not written by `writeAuditRow`** (not in the INSERT column list, `projection.go:416-420`), so on replay it takes a *new* now() value → any `ON CONFLICT (id, inserted_at)` dedup breaks (different key each write) → duplicate rows. Deterministic-key requirement rules it out.
- **New dedicated `SubsystemEventsAuditRetention`** — viable but higher blast radius (iota append + stringer regen + productionSubsystems cascade). Offer as the alternative if the team wants strict separation from projection.

### (b) Files/symbols to touch
- `internal/store/migrations/000052_events_audit_partition.up.sql` + `.down.sql` (NEW; down = reverse: detach children, rename partitioned→temp, rename `events_audit_unpartitioned`→`events_audit`, drop partitioned — must cleanly revert per migration rule).
- `internal/eventbus/audit/retention_partitions.go` (NEW — the `events_audit` PartitionManager).
- `internal/eventbus/audit/projection.go:414-421` — **change the `INSERT`/`ON CONFLICT` target** (see landmine).
- `internal/eventbus/audit/subsystem.go` — construct + Start/Stop the RetentionWorker; add retention config fields to `audit.Config` (`subsystem.go:100 Defaults()`).
- `internal/audit/retention.go` — reuse as-is (no change) OR add a single-window convenience; `RunOnce` (`:63-101`) already does Ensure→Detach→Drop.
- Config plumbing: `internal/eventbus/config.go` (retention fields) + `cmd/holomush/core.go` audit subsystem construction (`core.go:~567`, `audit.NewSubsystem(...)`).

### (c) Landmines / constraints
- **[LANDMINE — highest risk] Composite PK vs `ON CONFLICT (id)`.** A partitioned table's UNIQUE/PRIMARY KEY MUST include every partition column. Current PK is `id BYTEA PRIMARY KEY` (`000009:5`); after partitioning it becomes `PRIMARY KEY (id, <keycol>)`. Then `ON CONFLICT (id) DO NOTHING` (`projection.go:421`) fails at runtime — "no unique or exclusion constraint matching the ON CONFLICT specification." The conflict target must become `(id, <keycol>)`. **Both the migration PK and `writeAuditRow`'s conflict clause must change in the same PR** or every audit write breaks on deploy. `writeAuditRow` is shared by the live projection (`projection.go:330`) AND DLQ replay (`replay.go:219`), so one change covers both — but it also means replay's dedup depends on the same key.
- **[LANDMINE] `timestamp` is non-deterministic across live vs replay.** `projection.go:425` writes `timestamp = pgnanos.From(meta.Timestamp)` — the JetStream message store time. On the live path that is the EVENTS-stream store time; on replay it is the DLQ-stream capture time (later). So the same event yields *different* `timestamp` values on the two paths. With `ON CONFLICT (id, timestamp)`, replay would insert a duplicate. **Recommended resolution:** partition by `timestamp`, but change `writeAuditRow` to derive `timestamp` deterministically from the event ULID (`ulid.Time(id)` — the ULID's embedded creation ms, identical live+replay) instead of `meta.Timestamp`. Logic stays in Go (migration-rule compliant); it makes the `timestamp` column mean "event time" (arguably more correct for an audit timestamp) and is crypto-neutral (per `replay.go:204-208`, the events_audit `subject`/column values are NOT AAD inputs — cold-read AAD reconstructs from the marshaled envelope proto; the same reasoning covers `timestamp`). Verify no cold-history reader relies on `timestamp` == JetStream store time before flipping.
  - Alternative if changing the column source is deemed too invasive: add a **STORED generated partition column** extracting ms from the `id` BYTEA and partition by it, keeping `(id, gen_ts)` unique. More SQL complexity; confirm the target Postgres version allows partitioning by a generated column.
- **[LANDMINE] "no partition of relation found for row".** If partitioning by an event-supplied time and no partition covers an incoming value, the INSERT fails. Deriving the key from the ULID (near-real-time, monotonic) plus a DEFAULT partition safety-valve eliminates this. `EnsurePartitions(3)` (already called by `RunOnce`, `retention.go:68`) keeps a forward window.
- **Migration idempotency:** every step guarded (`IF EXISTS`/`IF NOT EXISTS`/regclass check), paired down that cleanly reverts (migration-rule MUST). No triggers/functions — the DETACH/DROP logic lives in the Go worker, not SQL.
- **`DETACH PARTITION CONCURRENTLY` cannot run inside a transaction block** — the worker must run it outside an explicit tx (pool.Exec autocommits; fine). Note it also cannot run while another DETACH CONCURRENTLY is in flight.
- **BRIN vs btree index choice:** `access_audit_log` uses a BRIN index on its partition-key timestamp (`000001_baseline.up.sql:304-305`). `events_audit` currently has btree `events_audit_subject_ts` (`000009:19`). Recreating indexes on the partitioned parent is required; decide whether to add a BRIN on `<keycol>` for cheap range pruning.

### (d) Open questions for the planner
- Partition key: `timestamp` (event-time, needs the ULID-derived-write change) vs a ULID-generated column. **Recommend `timestamp` sourced from `ulid.Time(id)`.**
- Existing-row disposal: ATTACH-as-partition (recommended) vs one-shot copy job.
- Worker home: inside `SubsystemAuditProjection` (recommended) vs a new `SubsystemID`.
- Whether to keep `PurgeExpiredAllows` in the interface path (no-op) or trim the interface for events_audit.

---

## OPS-03 — nats-server CVE remediation + vuln-scan CI gate

### (a) Recommended mechanism

**Two independent deliverables — the bump and the gate are separate controls.**

**1. Dependency bump (D-03).** `go get github.com/nats-io/nats-server/v2@v2.14.3 && go mod tidy` (`go.mod:22`). Trivial non-breaking patch (arch-review d9c HIGH-1). `nats.go v1.52.0` (`go.mod:23`) and `prometheus-nats-exporter v0.20.1` (`go.mod:24`) are **NOT implicated** by these two advisories — both CVEs are server-only (Connz pagination / MQTT-over-WebSocket in the embedded server). d9c "Strengths" confirms `nats.go@v1.52.0` is at latest.

**2. Vuln-scan gate (D-04) — reconcile govulncheck with the CVE reality.** See the blind-spot landmine: govulncheck alone cannot enforce the nats-server bump. Recommended **two-layer gate**, both in a new `task lint:vuln` and a dedicated CI job:
  - **`govulncheck ./...`** — callgraph-aware, low-noise, Go-native. Catches reachable Go-vulndb vulns going forward. Currently exits 0 (only GO-2026-5932 x/crypto openpgp, unreachable). Native allowlist: none — see below.
  - **`osv-scanner` with `osv-scanner.toml`** — OSV DB **includes GHSA/GitHub advisories**, so it catches GHSA-q59r-vq66-pxc2 on nats-server v2.14.2 and **passes once bumped to v2.14.3** → this is exactly the behavioral test for success-criterion #2. osv-scanner provides the **native allowlist** govulncheck lacks (`[[IgnoredVulns]]` with `id`, optional `ignoreUntil`, `reason`), cleanly satisfying D-04's "documented allowlist" requirement.

Wire order in `task lint:vuln`: run govulncheck first (fast reachability), then osv-scanner (broad DB). Fail closed on any unlisted finding. Slot as its own `ci.yaml` job (jobs today: `lint`, `test`, `integration`, `e2e`, `build` — `.github/workflows/ci.yaml:36,99,148,191,258`) rather than folding into the `lint:` umbrella (`Taskfile.yaml:102-120`), because govulncheck/osv-scanner download vuln DBs and would slow every local `task lint`. Keep `task lint:vuln` invokable locally but out of the umbrella `cmds`.

**Tool install:** mirror the repo's checksum-pinned pattern (`.github/actions/install-formatters`, `install-task`). Pin govulncheck via the tool-module mechanism (`go.tool.mod` / `go install golang.org/x/vuln/cmd/govulncheck@<pinned>`); pin osv-scanner to a released version. **Version-pin both** so DB/tool drift doesn't silently change gate behavior.

### The exact CVEs (CITED — `docs/reviews/arch-review/2026-07-11/findings/d9c-deps.md:20-33`, cross-checked against GitHub advisories this session)
- **GHSA-q59r-vq66-pxc2** — "Remote crash via integer overflow in Connz pagination." Affected `<= 2.14.2, <= 2.12.11`; fixed **2.14.3**; published 2026-06-29. **Reachable in HoloMUSH iff `event_bus.monitor_port` is enabled** (the `/connz` endpoint) — off by default (`internal/eventbus/config.go` `MonitorPort` `0=disabled`) but a documented operator opt-in (`site/.../operating/how-to/operations.md:163-167`).
- **GHSA-p957-7v2w-g93g / CVE-2026-58208** — "MQTT-over-WebSocket Path Can Crash WebSocket-Only JetStream Servers." Affected `<= 2.14.2, <= 2.12.11`; fixed **2.14.3**; published 2026-06-29. **NOT applicable** to HoloMUSH's shape (no MQTT/WebSocket configured).
- **Neither has a `GO-####-####` entry** in vuln.go.dev → govulncheck is blind to both (verified this session + d9c:23,28).

### Rejected alternatives
- **govulncheck-only gate** — rejected: cannot make criterion #2 true (nats-server absent from Go vulndb). Would enforce nothing for the nats bump.
- **osv-scanner-only** — viable (has GHSA coverage + native allowlist + experimental Go call analysis) but D-04 explicitly chose govulncheck for callgraph precision; keep govulncheck as the reachability layer and add osv-scanner for DB breadth.
- **govulncheck wrapper script filtering GO-IDs** (the CONTEXT-flagged option) — workable for govulncheck's missing allowlist, but redundant once osv-scanner (which has a native allowlist) is in the gate. Only needed if the team refuses osv-scanner.
- **Scheduled/nightly-only scan** (d9c MEDIUM-2 suggestion) — rejected vs D-04's "blocking" PR gate; a nightly scan doesn't block a merge.

### (b) Files/symbols to touch
- `go.mod:22` (nats-server bump) + `go.sum` (`go mod tidy`).
- `Taskfile.yaml` — new `lint:vuln` task (pattern: `internal/audit`/`lint:no-timestamptz` style `set -euo pipefail` block, `Taskfile.yaml:600-746`).
- `osv-scanner.toml` (NEW, repo root) — `[[IgnoredVulns]]` allowlist.
- `.github/workflows/ci.yaml` — new `vuln:` job (copy the `lint:` job scaffold at `:36-95`; add govulncheck/osv-scanner install steps).
- `.github/actions/install-*` — optionally a new pinned-install action for the scanners.
- go tool pinning: `go.tool.mod` (the repo already builds `task` from a pinned tool module).

### (c) Landmines / constraints
- **[LANDMINE] The govulncheck DB gap defeats criterion #2 unless osv-scanner is in the gate.** Do not ship a govulncheck-only gate and claim "known-vulnerable nats-server fails" — verified false this session.
- `osv-scanner` is not in the repo finding as an approved tool; run the **Package Legitimacy Gate** on it (it is a Google-maintained OSS project, `github.com/google/osv-scanner`).
- CI must not let the scan's DB-fetch flakiness turn into a non-deterministic gate — pin tool + consider a cached/offline DB or a retry.
- `bufbuild/buf-setup-action@v1` (floating tag, d9c MEDIUM-1) is an adjacent supply-chain hygiene item but **out of this phase's requirement set** — note, don't scope-creep.

### (d) Open questions for the planner
- govulncheck + osv-scanner combo (recommended) vs osv-scanner-only vs govulncheck+wrapper.
- Allowlist seed: GO-2026-5932 (x/crypto openpgp, unreachable) is currently the only govulncheck finding — decide whether to allowlist it (it already exits 0 since unreachable, so likely no entry needed) vs let osv-scanner flag it with a documented `reason`.
- PR-gated vs PR-gated + nightly (belt-and-braces).

---

## OPS-04 — DLQ replay `game_id` bridge + non-tautological test

### (a) Recommended mechanism
**Resolve `game_id` in `runAuditDLQReplay` before building the DLQ config, with a `--game-id` override (D-05).** Today the CLI reads `event_bus.game_id` via `loadEventBusConfig` (`cmd_audit.go:143-149`); if the operator config leaves it empty, `dlqConfigForGame("")` (`cmd_audit.go:337-343`) returns an empty-Subject `DLQConfig`, and `ReplayDLQ` → `cfg.Defaults()` (`replay.go:~8`, `dlq.go:69-79`) fills `Subject = "internal.main.audit.dlq"` (`dlq.go:26,37`). If the server's persisted `game_id` is a ULID (the normal case), its DLQ subject is `internal.<ULID>.audit.dlq`, so `originalSubject`'s prefix check (`replay.go:249-260`) never matches → **every dead letter counts as Failed** with the "prefix does not carry" warning (`replay.go:198-217`). That is the F3 bug.

**Fix:** in `runAuditDLQReplay` (`cmd_audit.go:298-331`), after opening the pool (`openAuditPool`, already at `:319`), resolve the effective game_id:
1. If `--game-id` flag is set → use it (override/escape hatch).
2. Else read the server's persisted value from `holomush_system_info` via the existing read path: `store.NewPostgresEventStore(ctx, url).GetSystemInfo(ctx, "game_id")` (the exact key/query `InitGameID` uses — `internal/store/postgres.go:97-99`, `GetSystemInfo(ctx, "game_id")`, returns `ErrSystemInfoNotFound` if absent). A lighter direct query `SELECT value FROM holomush_system_info WHERE key='game_id'` on the already-open `pool` is also fine and avoids constructing a full event store.
3. Fall back to `cfg.GameID` (config) only if the DB has none.
Then call `dlqConfigForGame(resolvedGameID)`.

Add the flag in `newAuditDLQReplayCmd` (`cmd_audit.go:107-119`): `cmd.Flags().String("game-id", "", "Override the game_id whose DLQ subject to replay (default: resolve from the server DB)")`.

This mirrors the server's own resolution order (`core.go`: `gameID := cfg.GameID; if gameID == "" { gameID = dbSub.GameID() }` — `core.go:~300-304`) so the CLI and server agree on the subject prefix.

### Rejected alternatives
- **Require the operator to pass `--game-id` always** — rejected by CONTEXT specifics ("zero operator burden; no operator should need to know a game_id ULID for the happy path").
- **Default the CLI subject to `internal.main.audit.dlq`** (status quo) — the bug itself.

### (b) Files/symbols to touch
- `cmd/holomush/cmd_audit.go` — `newAuditDLQReplayCmd:107-119` (add `--game-id`), `runAuditDLQReplay:298-331` (resolve + pass), `dlqConfigForGame:337-343` (unchanged; receives resolved id).
- Read path: `internal/store/postgres.go:97` (`InitGameID`/`GetSystemInfo(ctx,"game_id")`) — reuse, do not reimplement key logic.
- Test: **replace** `internal/eventbus/audit/dlq_replay_integration_test.go:97` `TestReplayDLQRestoresDeadLetterToAuditTable` (and its `replayDLQSubject = "internal.main.audit.dlq"` constant at `:27`, used on both seed `:38` and replay `:107`).

### (c) The replacement test (D-06)
- **Must exercise divergent game_id.** Seed the DLQ under a server-style game prefix (e.g. `internal.<some-ULID>.audit.dlq.<orig-subject>`), then run replay with (i) an *empty/`main`* CLI game to prove the current bug regresses to all-Failed as a guard, and (ii) the resolved/overridden correct game to prove recovery (Replayed=1, correct `events_audit.subject`). This is the F3 path the tautological test never hits (same `"main"` on both sides — CONTEXT flag).
- **Must use a real NATS container via `internal/testsupport/natstest`** (per `.claude/rules/testing.md` — external-NATS DLQ-against-a-real-broker), NOT embedded `eventbustest` (which the current test uses at `:102`). API: `natstest.StartNATS(ctx)` → `*NATSEnv`, `env.Conn(t) *nats.Conn`, `env.Terminate(ctx)` (`internal/testsupport/natstest/nats.go:47,60,77`). Reference usage: `test/integration/resilience/chaos_helpers_test.go` (`startExternalNATS`), `.../restart_reconnect_test.go`.
- If the game_id resolution is added at the CLI layer, the most faithful test drives `runAuditDLQReplay` (or a resolve helper) end-to-end. If that's awkward from `cmd/holomush`, an integration test in `internal/eventbus/audit` can seed a ULID-prefixed DLQ and call `ReplayDLQ` with a `dlqConfigForGame(resolvedID)` built from a system_info-seeded Postgres — proving the bridge. Prefer exercising the actual resolution seam, not a hand-built config, to avoid a new tautology.

### (d) Landmines / constraints
- **`natstest` requires Docker** and the `//go:build integration` tag; it runs under `task test:int`, not `task test`. Production code MUST NOT import `natstest` (depguard).
- Don't reintroduce a tautology: the divergent-game assertion is the whole point. Assert BOTH the failure guard (wrong game → Failed, no row) and the success (right game → Replayed, correct subject).
- `ReplayDLQ` applies `cfg.Defaults()` internally — a test passing an empty Subject silently becomes `internal.main.audit.dlq`; be explicit about the prefix under test.
- The `originalSubject` fail-loud path (`replay.go:198-217`) counts mismatches as `Failed` and retains the message (idempotent, safe to re-run) — the test can assert `res.Failed` for the wrong-game case.

---

## QUAL-01 — Coverage policy reconciliation + project ratchet

### (a) Recommended mechanism (D-07 + D-08)

**1. Correct the doc MUST to reality.** The docs claim a fictional per-package bar:
- `.claude/rules/testing.md` — "MUST maintain >80% coverage | Per-package coverage must exceed 80%" (the coverage table; also "SHOULD target 90%+ for core packages").
- Root `CLAUDE.md` — "MUST maintain >80% coverage | Per-package | verify with `task test:cover`".
Rewrite both to the enforced reality: **codecov/patch @ 80% is a hard merge gate via the protect-main *ruleset*** (NOT classic branch protection — `gh api .../branches/main/protection` 404 is expected and does not mean "not blocking"; durable memory `7qhyhb3hsb`/`v5k0e4zs3s`). Codecov measures **patch** (changed lines) and **project** (whole-repo), **never per-package** — so "per-package >80%" is doubly wrong (wrong granularity + not what's enforced). State that new code is held to 80% patch; legacy code is not retroactively blocked.

**2. Add a project-coverage ratchet.** `.codecov.yml` already declares a `project` status (`target: 80%, threshold: 2%`) and `patch` (`target: 80%, threshold: 5%`). The **project floor today is 80% target with a 2% drop tolerance**, but the baseline is ~54.6% (REQUIREMENTS `:45`), so an 80% project *target* fails every PR that touches uncovered code — that's a hard floor, not a ratchet. For a **no-drop ratchet** that nudges the ~54.6% baseline upward without blocking legacy code, change the project status to:
```yaml
coverage:
  status:
    project:
      default:
        target: auto        # = the base commit's coverage (the ratchet)
        threshold: 0%        # strict no-drop; or a small tolerance (e.g. 0.5%)
```
`target: auto` means "must not fall below the parent commit's project%," which is exactly the rising-floor ratchet D-07 wants (gives Phase 9 a floor that only ratchets up). Keep `patch.default.target: 80%` unchanged (that's the already-enforced gate).
- **Threshold choice (Claude's discretion):** `threshold: 0%` (strict) is the truest ratchet but flakes on coverage-measurement jitter between unit/integration merges; a small tolerance (`0.5%`–`1%`) absorbs jitter at the cost of allowing micro-drops. **Recommend `threshold: 1%`** to survive the two-upload merge jitter (see landmine) while still blocking real regressions. Revisit toward 0% once coverage stabilizes.

**3. Resolve the dual codecov files (D-08).** Two files exist at root:
- `.codecov.yml` — full config (project/patch 80%, rich `ignore` list, `codecov.notify.after_n_builds: 2`, comment layout).
- `codecov.yml` — 375B, **ignore-only** (`**/*.pb.go`, `pkg/proto/**`) — a strict subset of `.codecov.yml`'s ignore list.
**Recommend: delete `codecov.yml`, keep `.codecov.yml`.** The dotfile carries everything; the plain file adds nothing. Codecov's precedence between the two co-located files is **not officially documented** (verified against docs.codecov.com this session — see open question), so removing the duplicate eliminates the ambiguity entirely rather than relying on precedence. Optionally validate the surviving file via `https://api.codecov.io/validate`.

**4. Make the project status *required* (the ruleset action).** Adding the `project` status to `.codecov.yml` makes codecov *post* the status, but does NOT make it *block* merges — that requires adding `codecov/project` to the **protect-main ruleset's required checks** (a repo-settings/operator action, exactly like `codecov/patch` today). **Flag this explicitly in the plan as a non-YAML step** so it isn't left as "codecov will block" when the ruleset hasn't been updated (CONTEXT QUAL-01 flag).

### Rejected alternatives
- **"Add a patch gate"** — rejected by D-07: it already exists (ruleset-enforced). Reframing as new work would be wrong.
- **Enforce 80% project target now** — rejected: baseline 54.6% would block nearly every PR; the ratchet (auto/no-drop) is the chosen rising-floor.
- **Keep both codecov files** — rejected (D-08); undocumented precedence is a latent footgun.

### (b) Files/symbols to touch
- `.codecov.yml` — change `coverage.status.project.default` to `{ target: auto, threshold: 1% }`; keep patch as-is.
- `codecov.yml` — **delete**.
- `.claude/rules/testing.md` — rewrite the coverage MUST rows (and the "SHOULD 90% per-package" line).
- Root `CLAUDE.md` — rewrite the ">80% per-package" MUST.
- (repo settings, not in-repo) protect-main **ruleset** — add `codecov/project` as a required check.

### (c) Landmines / constraints
- **[LANDMINE] `branches/main/protection` 404 ≠ "not blocking."** The gate is a ruleset. Do not "discover" that patch isn't enforced and try to re-add it.
- **Two-upload merge jitter:** coverage = unit (`task test:cover`, `.github/workflows/ci.yaml:137`) + integration (`flag=integration`, `:188`) + e2e (`flag=e2e`, `:255`); codecov merges to two sessions and posts after `after_n_builds: 2` (`.codecov.yml`). A pending/absent status just means both uploads haven't landed — not a failure. A strict `threshold: 0%` project ratchet can flake on this jitter → the recommended `1%` tolerance.
- **Multi-line `slog.*Context` error branches count as many uncovered lines** (CONTEXT flag) — budget error-branch tests or patch% tanks even with the happy path covered. Relevant to OPS-02/OPS-04 new code in THIS phase (they must clear their own patch gate).
- `.codecov.yml` `ignore` already excludes `cmd/holomush/core.go` and generated code — new OPS-02 config plumbing in `core.go` is coverage-exempt, but the new PartitionManager and CLI game_id resolver are NOT exempt and must clear patch 80%.

### (d) Open questions for the planner
- Ratchet threshold: strict `0%` vs `1%` (recommended) vs `0.5%`.
- Codecov file precedence is undocumented — confirm via `api.codecov.io/validate` before/after deleting `codecov.yml` (LOW-confidence area; the delete-the-duplicate action is safe regardless).
- Whether the ruleset update is in-scope for this phase (repo-settings action) or handed to a maintainer with explicit instructions.

---

## Validation Architecture

Nyquist validation is enabled (`.planning/config.json` not overriding). Each success criterion maps to an observable check:

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing` + testify (unit); Ginkgo/Gomega (integration, `//go:build integration`) |
| Quick run | `task test -- ./internal/eventbus/audit/... ./internal/audit/... ./cmd/holomush/...` |
| Integration | `task test:int` (Docker: Postgres + real NATS via `natstest`) |
| CI gates | `task lint:vuln` (new), codecov patch/project statuses (ruleset) |

### Requirement → Validation Map
| Req | Success criterion | Validation type | Observable / command |
|-----|-------------------|-----------------|----------------------|
| OPS-02 | rows past window pruned; table bounded | integration (Postgres) | Seed rows across old/new month partitions; run `RetentionWorker.RunOnce`; assert old partitions DETACHed+DROPped, recent retained; assert `writeAuditRow` idempotency survives (live + replay of same event → 1 row) under composite PK |
| OPS-02 | migration reversible | unit/integration | Roll `000052` up then down on a scratch DB; assert `events_audit` returns to un-partitioned shape; `task test:int` runs migrations |
| OPS-03 | pinned-vulnerable nats-server FAILS gate | **CI-gate behavioral** | With `nats-server@v2.14.2` in go.mod, `task lint:vuln` (osv-scanner leg) exits non-zero citing GHSA-q59r-vq66-pxc2; after bump to v2.14.3, exits 0. This is the criterion #2 test — **only osv-scanner can produce it** (govulncheck DB gap) |
| OPS-03 | gate blocks merges | CI-gate observable | New `vuln:` job appears as a check on PRs; add to ruleset for blocking |
| OPS-04 | DLQ replay recovers external-NATS deployment | integration (real NATS) | `natstest`-backed test: seed DLQ under `internal.<ULID>.audit.dlq.*`; wrong game → `res.Failed>0`, 0 rows; resolved/overridden game → `res.Replayed==1`, correct `events_audit.subject` |
| OPS-04 | test is non-tautological | test-review | Assert divergent server/CLI game_id (not `"main"` on both sides); assert both failure-guard and recovery |
| QUAL-01 | doc and enforced bar agree | doc-review / grep | `.claude/rules/testing.md` + `CLAUDE.md` no longer say "per-package >80%"; describe ruleset patch gate + project ratchet |
| QUAL-01 | project ratchet blocks coverage-lowering PR | **CI-gate behavioral** | A PR that drops project% below parent (beyond threshold) shows `codecov/project` FAILURE + `mergeStateStatus=BLOCKED` once added to ruleset |

### Wave 0 gaps
- [ ] `internal/eventbus/audit/retention_partitions_test.go` — PartitionManager Ensure/Detach/Drop (integration; Postgres).
- [ ] Rewrite `internal/eventbus/audit/dlq_replay_integration_test.go` — divergent-game via `natstest`.
- [ ] `osv-scanner.toml` + `task lint:vuln` bats/behavioral check (pinned-vulnerable → fail).
- [ ] Framework installs: govulncheck (present locally; pin in CI), osv-scanner (new; pin + checksum).

---

## Package Legitimacy Audit

New external tooling introduced by this phase (not runtime deps):

| Package | Registry | Age | Source Repo | Verdict | Disposition |
|---------|----------|-----|-------------|---------|-------------|
| `golang.org/x/vuln/cmd/govulncheck` | Go (golang.org/x) | mature | github.com/golang/vuln | OK | Approved — official Go team tool; already installed locally (`/Users/sean/go/bin/govulncheck`) |
| `github.com/google/osv-scanner` | Go (github/google) | mature | github.com/google/osv-scanner | OK (verify) | Recommended — Google-maintained; planner MUST run `gsd-tools query package-legitimacy check` + pin a released version before adding to CI |
| `github.com/nats-io/nats-server/v2` @ v2.14.3 | Go | existing dep bump | github.com/nats-io/nats-server | OK | Approved — patch bump of an existing pinned dep (Renovate already queued it, d9c:30) |

No SLOP/SUS verdicts. `osv-scanner` is the only genuinely new tool — gate its install behind the legitimacy check per protocol. `[ASSUMED]` note: osv-scanner's exact latest version/checksum is not pinned here — the planner must resolve and pin it.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Codecov uses `.codecov.yml` over `codecov.yml` when both exist | QUAL-01 | LOW — mitigated by deleting the duplicate (D-08), which removes the dependency on precedence entirely |
| A2 | `events_audit` row count is small enough for ATTACH-as-partition to be trivial | OPS-02 | MEDIUM — if the table is large, the one-shot copy path is needed instead; planner should `SELECT count(*)`/check size on a representative DB |
| A3 | Changing `writeAuditRow`'s `timestamp` source from `meta.Timestamp` to `ulid.Time(id)` is crypto-neutral and no cold-reader depends on store-time semantics | OPS-02 | MEDIUM — verify against cold-history readers before flipping; `replay.go:204-208` establishes columns aren't AAD inputs, but confirm no query orders/filters on `timestamp==JetStream-store-time` |
| A4 | osv-scanner's OSV DB contains GHSA-q59r-vq66-pxc2 for nats-server (making criterion #2 achievable) | OPS-03 | MEDIUM — GHSA advisories are ingested into OSV, but confirm the specific ID resolves in osv-scanner before relying on it as the gate's proof; if not, fall back to a go.mod min-version lint for nats-server |
| A5 | Starting the RetentionWorker inside `SubsystemAuditProjection` avoids the productionSubsystems cascade | OPS-02 | LOW — if the audit subsystem can't host a goroutine cleanly, fall back to a dedicated SubsystemID (higher blast radius, still viable) |

## Open Questions

1. **OPS-02 partition key + dedup** — `timestamp` sourced from `ulid.Time(id)` (recommended) vs a ULID-generated column. Blocks the migration; lock before writing `000052`.
2. **OPS-03 gate composition** — govulncheck+osv-scanner (recommended) vs osv-scanner-only vs govulncheck+wrapper. Determines whether criterion #2 is provable.
3. **QUAL-01 ratchet threshold** — 0% vs 1% (recommended) vs 0.5%; and whether the ruleset update is in-phase.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| govulncheck | OPS-03 gate | ✓ | latest (local `~/go/bin`) | pin in CI |
| osv-scanner | OPS-03 gate | ✗ | — | install (Go) or osv-scanner GH action, checksum-pinned |
| Docker (Postgres + NATS testcontainers) | OPS-02/OPS-04 integration tests | ✓ (CI: testcontainers-cloud) | — | — |
| `natstest` harness | OPS-04 test | ✓ (in-repo) | — | — |
| Go 1.26.x | all | ✓ | 1.26.4 (go.mod), 1.26.5 local | — |

**Missing with fallback:** osv-scanner (install step required).

## Sources

### Primary (HIGH confidence — read this session)
- `internal/audit/retention.go:23-148` (RetentionConfig, PartitionManager iface, RetentionWorker — zero prod callers)
- `internal/audit/partition_creator.go:17-62` (PostgresPartitionCreator — EnsurePartitions only; the DDL pattern)
- `internal/access/policy/bootstrap.go:17-57` + `internal/store/migrations/000001_baseline.up.sql:287-308` (partitioned access_audit_log model)
- `internal/store/migrations/000009_create_events_audit.up.sql`, `000017`, `000038` (events_audit shape; timestamp+inserted_at → BIGINT ns)
- `internal/eventbus/audit/projection.go:353-421` (writeAuditRow, ON CONFLICT (id)), `replay.go:180-260` (replayOne/originalSubject mismatch), `dlq.go:16-79` (DLQ defaults), `subsystem.go:85-125` (Config.Defaults)
- `cmd/holomush/cmd_audit.go:107-343` (replay CLI, dlqConfigForGame), `cmd/holomush/core.go:~300-304,567` (server game_id resolve + audit subsystem)
- `internal/store/subsystem.go:67,105-111` + `internal/store/postgres.go:97` (GameID/InitGameID/GetSystemInfo("game_id"))
- `internal/lifecycle/subsystem.go:14-40` (SubsystemID enum), `internal/bootstrap/setup/subsystem.go:123-149` (subsystem wiring pattern)
- `.codecov.yml` + `codecov.yml` (dual-file conflict), `.github/workflows/ci.yaml:36-258` (jobs), `Taskfile.yaml:102-746` (lint tasks)
- Local `govulncheck ./...` + `-format json` run: only GO-2026-5932 (x/crypto openpgp, unreachable); no nats-server finding

### Secondary (CITED)
- `docs/reviews/arch-review/2026-07-11/findings/d9c-deps.md:20-51,100` (HIGH-1 CVEs, MEDIUM-2 gate gap, govulncheck-clean strength)
- GitHub advisories GHSA-q59r-vq66-pxc2, GHSA-p957-7v2w-g93g / CVE-2026-58208 (fixed 2.14.3, published 2026-06-29)
- `vuln.go.dev/index/modules.json` (no nats-server entry — govulncheck blind spot confirmed)
- OSV-Scanner docs (`[[IgnoredVulns]]` allowlist), Go vuln-management docs (govulncheck has no native suppression — golang/go#59507)
- docs.codecov.com/docs/codecov-yaml (file locations; precedence between `.codecov.yml`/`codecov.yml` undocumented → A1)

## Metadata
**Confidence breakdown:**
- OPS-02 mechanism: HIGH (code-grounded); idempotency landmine: HIGH (verified ON CONFLICT + meta.Timestamp); ATTACH-vs-copy: MEDIUM (row-count assumption A2)
- OPS-03 CVEs + govulncheck gap: HIGH (verified locally + finding); osv-scanner catches it: MEDIUM (A4)
- OPS-04: HIGH (full read path grounded)
- QUAL-01: HIGH on doc-vs-reality + dual-file; MEDIUM on codecov precedence (A1) + ratchet threshold (discretion)

**Research date:** 2026-07-13
**Valid until:** ~2026-08-13 (CVE/advisory state is time-sensitive; re-verify nats-server advisories and osv-scanner DB before the OPS-03 gate lands)
