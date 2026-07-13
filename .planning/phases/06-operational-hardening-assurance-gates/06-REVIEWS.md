---
phase: 6
reviewers: [codex, opencode, antigravity]
reviewed_at: 2026-07-13T22:27:55Z
plans_reviewed: [06-01-PLAN.md, 06-02-PLAN.md, 06-03-PLAN.md, 06-04-PLAN.md, 06-05-PLAN.md]
models: {codex: default, opencode: openrouter/x-ai/grok-4.5, antigravity: default}
---

# Cross-AI Plan Review — Phase 6

> Three external reviewers ran against the plans in this worktree. **Codex** and
> **OpenCode (grok-4.5)** read live source (grounded — high weight). **Antigravity**
> ran without the repo in its workspace ("source codebase was not present") and
> validated against the prompt's research logs only — its assertions restate the
> plan/research and carry low weight, though it independently surfaced two concerns.
> The orchestrator independently VERIFIED the highest-stakes claim (protect-main
> required checks) — see the Consensus Summary.

---

## Codex Review

# Phase 6 plan review

## Overall assessment

**Verdict: NOT READY. Overall risk: HIGH.**

The decomposition and test intent are strong, but four load-bearing assumptions are false against the current code and PostgreSQL/GitHub state:

1. The proposed DEFAULT-partition migration cannot coexist with the planned current-month partitions and prevents `DETACH ... CONCURRENTLY`.
2. The ULID timestamp change does not preserve hot/cold timestamp semantics or rolling-deploy idempotency.
3. The live `protect-main` ruleset currently requires neither `codecov/patch` nor `codecov/project`.
4. The DLQ resolver order does not mirror the server, and the planned integration test need not exercise the CLI resolver.

These should be corrected before execution.

---

## 06-01 — events_audit partition migration

### Summary

The plan correctly identifies the composite-PK/idempotency coupling, but the proposed DEFAULT-partition swap and timestamp conversion are not safe. This plan needs substantial redesign.

### Strengths

- The current write really is `ON CONFLICT (id)` with JetStream store time at [projection.go:414](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/projection.go:414), and replay shares this exact write at [replay.go:219](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/replay.go:219). Treating the schema and write-path changes atomically is correct.
- The present schema has a single-column PK at [000009_create_events_audit.up.sql:4](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/store/migrations/000009_create_events_audit.up.sql:4), while the established partition pattern includes the partition key in the PK at [000001_baseline.up.sql:285](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/store/migrations/000001_baseline.up.sql:285).
- Migration 000038 did convert both audit timestamps to BIGINT at [000038_eventbus_crypto_timestamps_to_bigint.up.sql:35](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/store/migrations/000038_eventbus_crypto_timestamps_to_bigint.up.sql:35), so avoiding new TIMESTAMPTZ columns is correct.

### Concerns

- **HIGH — the proposed DEFAULT attachment is not viable.** The plan creates current and future partitions first, then attaches the old table as DEFAULT at [06-01-PLAN.md:91](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:91) and [06-01-PLAN.md:96](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:96). Any legacy rows falling in those current-month ranges violate the DEFAULT partition constraint, so attachment will fail. PostgreSQL explicitly requires the default partition not to contain rows belonging to existing range partitions and validates this by scanning it. [PostgreSQL ALTER TABLE documentation](https://www.postgresql.org/docs/current/sql-altertable.html)

- **HIGH — DEFAULT also makes 06-02’s retention DDL invalid.** PostgreSQL does not permit `DETACH PARTITION CONCURRENTLY` while the parent has a DEFAULT partition. The plan deliberately retains that DEFAULT indefinitely at [06-01-PLAN.md:98](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:98).

- **HIGH — rolling idempotency is unproven and likely broken.** Pre-migration rows have `timestamp = meta.Timestamp` per [projection.go:346](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/projection.go:346). A post-migration replay would use the proposed ULID timestamp. The same ID could therefore exist once in the legacy/default partition and again in a monthly partition because `(id, timestamp)` differs and PostgreSQL cannot enforce global uniqueness on `id` alone. The planned test at [06-01-PLAN.md:133](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:133) tests only two post-change writes, not pre-up/live → post-up/replay.

- **HIGH — ULID time is not the canonical event timestamp.** Event ID and `Event.Timestamp` are produced by separate clock calls at [types.go:215](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/types.go:215). Hot history uses the envelope timestamp at [hot_jetstream.go:545](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/history/hot_jetstream.go:545), while cold history filters the database timestamp at [cold_postgres.go:159](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/history/cold_postgres.go:159). Substituting ULID milliseconds can change tier-boundary and `NotBefore`/`NotAfter` behavior.

- **HIGH — the down migration loses post-up rows.** The prescribed down path detaches only the old DEFAULT and drops the partitioned parent and monthly children at [06-01-PLAN.md:103](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:103). Every audit row written after the up migration would be deleted.

- **MEDIUM — required crypto review is missing.** Both `events_audit` migrations and `projection.go` trigger the mandatory crypto gate at [CLAUDE.md:117](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/CLAUDE.md:117).

### Suggestions

- Replace the permanent DEFAULT strategy with an explicit, resumable migration/backfill sequence that leaves no DEFAULT before retention begins.
- Derive the deterministic key from the preserved envelope timestamp, or formally make ULID time canonical across both hot and cold tiers. Add a hot/cold boundary parity test.
- Test legacy pre-up row → post-up replay idempotency and legacy + post-up row survival through down migration.
- Add the required crypto-reviewer gate.

### Risk assessment

**HIGH.** As written, the up migration can fail on real data, retention cannot detach partitions concurrently, rollback loses audit data, and replay can create cross-partition duplicates.

---

## 06-02 — retention manager and lifecycle

### Summary

Production wiring is necessary and correctly scoped to the audit subsystem, but the partition lifecycle lacks a workable DEFAULT policy, historical coverage, and detached-partition age tracking.

### Strengths

- The worker is genuinely unwired: `NewRetentionWorker` has only test callers; its implementation starts with an immediate cycle at [retention.go:127](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/audit/retention.go:127).
- `RunOnce` already has the desired Ensure → Purge → Detach → Drop ordering at [retention.go:63](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/audit/retention.go:63).
- Co-locating it with `SubsystemAuditProjection` is reasonable because that subsystem already resolves the pool and owns the projection lifecycle at [subsystem.go:202](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/subsystem.go:202).

### Concerns

- **HIGH — `DETACH ... CONCURRENTLY` cannot run with 06-01’s DEFAULT partition.** The conflicting requirements appear directly at [06-02-PLAN.md:91](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:91) and [06-02-PLAN.md:94](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:94). PostgreSQL documents this prohibition. [PostgreSQL ALTER TABLE documentation](https://www.postgresql.org/docs/current/sql-altertable.html)

- **HIGH — current+future partitions do not cover replayed historical events.** `EnsurePartitions` is specified to create only current and future months at [06-02-PLAN.md:76](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:76). DLQ replay can restore older events. Under the proposed ULID partition key they fall into the never-pruned DEFAULT, so repeated historical replay can keep growing it.

- **MEDIUM — grace tracking is unspecified.** The plan requires a just-detached partition not to be dropped at [06-02-PLAN.md:78](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:78), but detaching removes the table’s partition relationship and bound metadata. No side table, rename convention, or comment records when it was detached.

- **MEDIUM — the proposed Start rollback cannot observe retention failure.** `RetentionWorker.Start` always returns nil and starts `RunOnce` asynchronously at [retention.go:103](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/audit/retention.go:103). Therefore the rollback promised at [06-02-PLAN.md:166](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:166) cannot catch initial DDL failure.

### Suggestions

- Require partitions covering the retention horizon and DLQ horizon, not just future months.
- Define durable detach metadata or derive drop eligibility deterministically from validated partition names/ranges.
- Run and validate the first retention cycle synchronously before reporting subsystem readiness, or explicitly define asynchronous degradation/health behavior.
- Add a two-worker concurrency test if two replicas will run retention simultaneously.

### Risk assessment

**HIGH.** Its central DETACH operation is incompatible with 06-01, and historical replay still has an unbounded sink.

---

## 06-03 — vulnerability gate

### Summary

The two-scanner approach and pre-bump negative test are good, but the allowlist and CLI details need correction.

### Strengths

- The repository is currently pinned to `nats-server/v2 v2.14.2` at [go.mod:22](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/go.mod:22).
- Requiring demonstrated failure before the bump and success after it is much stronger than merely installing a scanner.
- The exact new GHSA was not found in the current `golang/vulndb` repository search, so retaining an OSV/GHSA-backed scanner for this advisory is justified. Govulncheck only reports vulnerabilities present in its configured Go vulnerability database. [govulncheck documentation](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck)

### Concerns

- **HIGH — the allowlist does not apply to the govulncheck leg.** The OSV `[[IgnoredVulns]]` file at [06-03-PLAN.md:129](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:129) can suppress OSV findings, but govulncheck explicitly has no finding-suppression support. An accepted reachable GO vulnerability would still fail before OSV runs. [govulncheck limitations](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck)

- **MEDIUM — the OSV command is version-ambiguous.** The plan specifies `osv-scanner --config=... <target>` at [06-03-PLAN.md:125](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:125). Current OSV-Scanner v2 uses `osv-scanner scan source ...` or `scan -L ...`. The command must be fixed after choosing the pinned major version. [OSV-Scanner usage](https://google.github.io/osv-scanner/usage/)

- **LOW — “nats-server is absent from the Go DB” is too broad.** The Go database contains other `nats-server` vulnerabilities; only this new advisory appears absent. Narrow the language to avoid a stale architectural claim.

### Suggestions

- Define one policy layer that can suppress accepted findings from both scanners, with expiry and rationale.
- Pin the scanner major version first, then write and test its exact CLI.
- Preserve the mandatory pre-bump behavioral test; it will detect an OSV ingestion gap.

### Risk assessment

**MEDIUM.** The known NATS CVE path is likely caught, but the promised general allowlist is currently fail-closed without an exception mechanism.

---

## 06-04 — coverage policy

### Summary

The YAML cleanup and `target: auto` direction are sound, but the plan’s core claim about current enforcement is contradicted by the live GitHub ruleset.

### Strengths

- The plain [codecov.yml:7](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/codecov.yml:7) is a strict subset of the full [.codecov.yml:23](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.codecov.yml:23); deleting it is safe and removes precedence ambiguity.
- `target: auto` compares project coverage with the base commit, so it is the correct ratchet mechanism rather than an immediate 80% project floor. [Codecov status documentation](https://docs.codecov.com/do/docs/commit-status)

### Concerns

- **HIGH — `codecov/patch` is not currently required.** Live inspection of `protect-main` ruleset `11923801` returned required checks `Build`, `Lint`, `Test`, `CodeRabbit`, `Integration Test`, and `E2E Test`; neither Codecov status is present. The plan nevertheless instructs docs to claim patch is already a hard gate at [06-04-PLAN.md:112](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-04-PLAN.md:112). Locally, coverage uploads are explicitly nonfatal at [ci.yaml:141](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.github/workflows/ci.yaml:141). As written, the doc rewrite would introduce a new false MUST.

- **MEDIUM — `threshold: 1%` is not “no-drop.”** The plan repeatedly labels it no-drop at [06-04-PLAN.md:25](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-04-PLAN.md:25), but Codecov defines threshold as permitted leniency below the target. A 1% threshold permits repeated declines. [Codecov common configurations](https://docs.codecov.com/docs/common-recipe-list)

### Suggestions

- Make the operator task add **both** `codecov/patch` and `codecov/project` to `protect-main`, then verify both through the ruleset API.
- Use `threshold: 0%` for a true no-drop ratchet, or call 1% what it is: a 1-percentage-point regression allowance.
- Do not merge the documentation rewrite until live required-check evidence is captured.

### Risk assessment

**HIGH.** QUAL-01 would otherwise declare two gates enforced when neither is presently required.

---

## 06-05 — DLQ game_id bridge

### Summary

The current test is demonstrably tautological, and switching to `natstest` is correct. However, the proposed resolver does not mirror server precedence and the integration test is allowed to bypass the resolver.

### Strengths

- The current test uses `"internal.main.audit.dlq"` for both seed and replay at [dlq_replay_integration_test.go:25](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/dlq_replay_integration_test.go:25), and uses embedded `eventbustest` at [dlq_replay_integration_test.go:102](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/dlq_replay_integration_test.go:102). Replacing it is necessary.
- The mismatch path already fails closed without writing at [replay.go:198](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/replay.go:198).
- `GetSystemInfo` correctly queries `holomush_system_info` and distinguishes missing rows at [postgres.go:69](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/store/postgres.go:69).

### Concerns

- **HIGH — the proposed precedence does not mirror the server.** The plan says override → DB → config at [06-05-PLAN.md:37](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-05-PLAN.md:37). The server uses configured root `game_id` first and DB only when it is empty at [core.go:300](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/core.go:300). The CLI currently loads only the `event_bus` section at [cmd_audit.go:141](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/cmd_audit.go:141). With an explicit root game ID different from the persisted DB value, automatic replay still targets the wrong subject.

- **HIGH — the integration test need not test the CLI bridge.** The plan places it in `internal/eventbus/audit` and merely says “prefer” the real resolver at [06-05-PLAN.md:134](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-05-PLAN.md:134). That package cannot exercise unexported `cmd/holomush` resolver wiring. A hand-built correct `DLQConfig` proves only that `ReplayDLQ` works when already given the right prefix—the behavior already known.

### Suggestions

- Define the effective order as `--game-id` → configured root `game_id` → persisted DB ID, matching server startup exactly.
- Put the real-broker integration test in `cmd/holomush`, or move resolution into an importable package and drive it end-to-end.
- Make “actual resolver seam exercised” an acceptance criterion, not a preference.

### Risk assessment

**HIGH.** The proposed implementation can preserve the production mismatch for configured game IDs, while the integration test still passes.

---

## Required plan changes before execution

1. Redesign 06-01/06-02 without a permanent DEFAULT partition and with a data-preserving rollback.
2. Use a canonical timestamp consistent across hot history, cold history, live writes, and replay; test the rolling migration case.
3. Specify detached-partition age tracking and historical partition coverage.
4. Fix 06-03’s cross-scanner exception policy and pin the matching OSV CLI syntax.
5. Update 06-04 to require both Codecov checks; stop calling a 1% allowance “no-drop.”
6. Make 06-05 reproduce the server’s actual game-ID precedence and test the CLI resolver end-to-end.


---

## OpenCode Review (grok-4.5)

I'll verify each plan's claims against the live code paths and migrations, then produce a structured plan review.
Reading the key tied files the plans claim to change and depend on.
Digging into the critical code paths for landmines the plans claim to handle.
# Phase 6 Plan Review — Operational Hardening & Assurance Gates

Code-backed review of plans 06-01…06-05 against the tree at this worktree. Evidence cites live paths.

---

## Overall

**Assessment:** Phasing is strong (atomic PK/idempotency first, CI gates parallel, prune + DLQ test after write-path shape). Research-landmines (composite PK vs `ON CONFLICT (id)`, govulncheck blind spot, tautological DLQ test, dual codecov + ruleset gap) are real and correctly prioritized. Plans largely achieve OPS-02..04 + QUAL-01 if two **HIGH** execution gaps are fixed before execute: migration column/ATTACH shape in 06-01, and DropDetachedPartitions bookkeeping in 06-02.

**Risk: MEDIUM–HIGH** (coated to HIGH only if 06-01 ships as written without the ATTACH/PK prep steps).

---

## 06-01 — OPS-02 atomic core (partition-swap + writeAuditRow)

### Summary
Correctly treats migration PK + `ON CONFLICT` + deterministic timestamp as one deploy unit; research lands matches code. Executable DPLs under-specify final column list and ATTACH preconditions — those will fail or recreate wrong schema if followed literally.

### Strengths
- Idempotency landmine is real: `writeAuditRow` uses `ON CONFLICT (id) DO NOTHING` with `pgnanos.From(meta.Timestamp)` at `internal/eventbus/audit/projection.go:414-425`, while the function doc admits live vs DLQ timestamps differ (`projection.go:346-352`). Partitioning like `access_audit_log` (`000001_baseline.up.sql:287-302`) forces `PRIMARY KEY (id, timestamp)`.
- Shared path coverage is right: live at `projection.go:330` and replay at `replay.go:219` both call `writeAuditRow`.
- ULID is already parsed at `projection.go:409-412` / `decodeULIDString` (`:509-517`) — deriving `timestamp` from that is a small, natural flip.
- Split from worker wiring (06-02) keeps the atomic-risk surface small.
- Wave-1 initial partitions + DEFAULT safety valve targets the “no partition for row” failure mode.

### Concerns
- **HIGH — “column set from 000009 verbatim” is wrong and dangerous.**  
  `000009_create_events_audit.up.sql:4-16` is historical only: `payload` (renamed → `envelope` in `000017`), TIMESTAMPTZ timestamps (→ BIGINT via `000038_*`), plus `rendering` (`000012`), `dek_ref`/`dek_version` (`000014`), index `events_audit_subject_js_seq` (`000011`). Reproducing 000009 literally reintroduces TIMESTAMPTZ (fails `lint:no-timestamptz`), drops columns, and breaks `cold_postgres.go:140` SELECTs.  
  **Must read live post-migration shape** (info_schema / successive migrations), not 000009 alone.

- **HIGH — ATTACH as DEFAULT without PK reshape will fail.**  
  Unpartitioned table has `PRIMARY KEY (id)` only. Parent after swap is `PRIMARY KEY (id, timestamp)`. Postgres requires child unique indexes that include the partition key before ATTACH. Plan step (4) skips:
  ```sql
  ALTER TABLE events_audit_unpartitioned DROP CONSTRAINT …;
  ALTER TABLE events_audit_unpartitioned ADD PRIMARY KEY (id, timestamp);
  -- then ATTACH
  ```
  Also keep indexes/columns aligned with the parent.

- **MEDIUM — DEFAULT partition vs “no longer grows unbounded”.**  
  DEFAULT never range-DETACH/DROPs (`DetachExpiredPartitions` must skip it — 06-02 partly notes this). (a) All pre-migration rows stay forever; (b) if `EnsurePartitions` lags, *new* rows can pile into DEFAULT and never prune. On a young table (A2) this may be fine, but success criterion #1 is only fully true for *range* children after 06-02. Prefer bounded `ATTACH … FOR VALUES FROM (min) TO (max+ε)` when min/max are finite, DEFAULT only as last resort — or document permanent-remnant clearly in SUMMARY/ops docs.

- **MEDIUM — A3 “no cold reader depends on timestamp” is too strong as stated.**  
  `cold_postgres.go:159-182` filters on `timestamp` (`NotBefore`/`NotAfter`, hot/cold edge). Order is `js_seq` (`:187-189`). Event.Timestamp is recovered from the envelope (`:215-217`), so crypto AAD is fine. Semantics *do* change for time-window queries (store time → ULID ms). Treat as intentional event-time, not “no consumer care”; assert Δ is well under typical window skew and that edges remain coherent.

- **LOW — comment debt.** `projection.go:333-352` and `subsystem.go:10` still document `ON CONFLICT (id)` / store-time metadata; must be rewritten or docs lie.

- **LOW — deploy-site month baking.** Inline “current+next2” partitions in static SQL are correct only for apply-month. Acceptable with DEFAULT + 06-02 EnsurePartitions, but 06-01-only deploys spanning months rely on DEFAULT (unprunable).

### Suggestions
1. Task 1 action: *“create parent with the **current** live columns/indexes (post-000051), BIGINT ns keys only.”*
2. Explicit pre-ATTACH: rebuild composite PK on `events_audit_unpartitioned`.
3. A3 acceptance: document cold-filter time semantics change; add a short note in cold_postgres/docs if sinks assume store-time.
4. Prefer min/max range ATTACH when count/min/max is cheap; DEFAULT as documented fallback.

### Risk
**HIGH** until ATTACH/PK and column-list are fixed in the plan text; then **MEDIUM**.

---

## 06-02 — OPS-02 worker (PartitionManager + wiring)

### Summary
Matches reality: `RetentionWorker` / `NewRetentionWorker` exist only under `internal/audit/retention.go` (+ tests); production `PartitionManager` is incomplete (`PostgresPartitionCreator.EnsurePartitions` only — `partition_creator.go:29-53`). Homing the worker inside `SubsystemAuditProjection` is good blast-radius control.

### Strengths
- Full interface gaps correctly identified (`retention.go:31-38` vs mocks in `retention_test.go`).
- Wiring seam at `subsystem.go:202-260` (pool resolved, all-or-nothing Start) avoids new `SubsystemID` / `productionSubsystems` cascade.
- Config mapping `RetainWindow → RetainDenials` for Detach (`RunOnce` at `retention.go:83`) fits no-op `PurgeExpiredAllows`.
- DETACH CONCURRENTLY outside tx flagged correctly.

### Concerns
- **HIGH — `DropDetachedPartitions` is unspecified.** No production detach/drop code exists today. After `DETACH`, tables are ordinary relations; grace-period drop needs **state** (rename suffix + catalog timeline, side table, `pg_class.relname` convention, etc.). Plan only says `DROP TABLE IF EXISTS` on “detached children past grace” with no discovery mechanism. Executor will invent or ship a no-op drop half → criterion #1 incomplete.
- **MEDIUM — DEFAULT interaction.** Must never DETACH/DROP `events_audit_unpartitioned` / DEFAULT child. Tests should assert that explicitly (plan acceptance mentions “untouched”; lock the rule in impl).
- **MEDIUM — worker Start almost never fails.** `RetentionWorker.Start` always returns nil (`retention.go:104-108`). “Rollback on worker start failure” is mostly dead; risk is failure *after* spawn inside `RunOnce` (already non-fatal log). Prefer Start after projection, log retention failures, still stop worker in Stop.
- **MEDIUM — config surface.** `internal/eventbus/config.go` has DLQ MaxAge but no audit-retain section; plan adds fields to `audit.Config` + event_bus. Avoid shadowing DLQ MaxAge (30d, `config.go:29-32,98-104`) with stream retention — use a distinct name (`events_audit.retain_window` or `audit.retain_window`).
- **LOW — ABAC worker still unwired.** In-scope exclusion is fine; note leftover debt so “RetentionWorker is wired” does not imply ABAC pruning.

### Suggestions
1. Design detach tracking **before** Task 1 code: e.g. rename on detach to `events_audit_YYYY_MM_detached_<unix>` and drop where mtime/name age > grace; document in plan.
2. Integration test: old range gone, recent kept, DEFAULT kept, EnsurePartitions covers now+forward.
3. Wire Status/metrics for last success/fail cycle (optional, helps ops).

### Risk
**MEDIUM** (implementation gap on drop half).

---

## 06-03 — OPS-03 nats CVE + vuln gate

### Summary
Bump + dual-tool gate is well motivated. `go.mod:22` is still `nats-server/v2 v2.14.2`. Arch-review evidence (`docs/reviews/arch-review/2026-07-11/findings/d9c-deps.md:20-32`) shows govulncheck-blind GHSA-q59r-vq66-pxc2; criterion #2 needs OSV/GHSA coverage.

### Strengths
- Correctly rejects govulncheck-only for success criterion #2.
- Pre-bump fail → bump → post-bump pass is the right behavioral order.
- Keep out of `lint` umbrella (Taskfile pattern at `lint:no-timestamptz` ~`:709`).
- Human legitimacy checkpoint + SHA pin for osv-scanner is appropriate (package-legitimacy).
- Does not drive unrelated bumps of `nats.go` / exporter (`go.mod:23-24`).
- Ruleset required for *blocking* is explicit (`user_setup`, autonomous:false).

### Concerns
- **MEDIUM — A4 residual:** plan STOPS if osv-scanner does not cite GHSA-q59r-vq66-pxc2 — keep that gate hard (fallback: `go list -m` min-version assert on `nats-server/v2`).
- **MEDIUM — pass/fail by exit code only is right**, but CI flaky DB fetch can fail-closed (burn)</; pin versions, document retry. Prefer offline/cache if available.
- **LOW — “allowlist via osv only”:** govulncheck still lacks native suppress; OK if unreachable GO-2026-5932 stays clean. Don’t invent a wrapper that can fail-open.
- **LOW — ruleset todos stack up** (vuln + codecov/project in 06-04). Collective operator checklist.

### Suggestions
1. Capture exact pre-bump osv-scanner JSON line in SUMMARY as criterion-#2 evidence.
2. Optionally add a thin `task lint:nats-min-version` belt if OSV lags GHSA again.
3. Single checklist issue for all protect-main additions (vuln, codecov/project).

### Risk
**LOW–MEDIUM** (depends on A4 confirmation at execute).

---

## 06-04 — QUAL-01 coverage reconciliation

### Summary
Matches tree: `.codecov.yml:25-32` project `{80%, threshold 2%}` + patch `{80%, 5%}`; `codecov.yml` is ignore-only subset; docs still claim per-package >80% (`CLAUDE.md:187`, `.claude/rules/testing.md:25-27`). Reframe (patch already ruleset-gated; add project ratchet) is correct.

### Strengths
- `target: auto` + `threshold: 1%` is a true no-drop ratchet; 80% project target against ~54.6% baseline would brick PRs.
- Dual-file delete removes undocumented precedence ambiguity.
- Explicit “YAML posts; ruleset blocks” avoids false assurance (T-06-4-01).
- after_n_builds:2 noted (`:14-20`) — good context for pending ≠ fail.

### Concerns
- **MEDIUM — Task 3 deferral path.** If ruleset step is deferred, SUMMARY must say “posting, not blocking” or QUAL-01 success criterion is overstated.
- **LOW — AGENTS.md symlink / docs symmetry.** Plan edits `CLAUDE.md`; symlink policy (`task lint:docs-symmetry`) should stay clean — accept as done if AGENTS.md remains symlink.
- **LOW — threshold 1%** can still allow creeping erosion; plan already allows tighten later — OK.

### Suggestions
1. Optional validate via codecov validate API after delete.
2. Same operator issue for ruleset as 06-03.
3. Grep acceptance for leftover “per-package” rows is good; also check site docs if any.

### Risk
**LOW** for in-repo work; enforcement incomplete until ruleset update.

---

## 06-05 — OPS-04 DLQ game_id + non-tautological test

### Summary
Bug and fix target are accurate. CLI uses `dlqConfigForGame(cfg.GameID)` with no DB resolve (`cmd_audit.go:325-342`); empty Subject → `Defaults()` → `internal.main.audit.dlq` (`dlq.go:24-37,69-75`). Server DLQ subject uses resolved game ULID (`core.go:300-302,571-577`). Current test seeds and replays `internal.main.audit.dlq` via embedded `eventbustest` (`dlq_replay_integration_test.go:27,102-107`) — tautological w.r.t. F3.

### Strengths
- Resolution order CLI: `--game-id` → `GetSystemInfo(…,"game_id")` (`postgres.go:70-81,97-98`) → config is the right fix for empty-config deployments.
- Replace (not augment) + divergent + both failure and recovery assertions match D-06.
- `natstest.StartNATS` / `Conn` / `Terminate` exist (`internal/testsupport/natstest/nats.go:47,60,77`).
- `originalSubject` fail-loud path already counts Failed without writing (`replay.go:198-217`) — failure-guard is free.
- depends_on 06-01 is correct: replay shares composite-PK `writeAuditRow`.

### Concerns
- **MEDIUM — “drive resolveGameID end-to-end” vs package split.** Resolver lives in `cmd/holomush`; replacement test is `internal/eventbus/audit` with integration tag. Without exporting a tiny shared helper / testing IRR command entry, executor will hand-build `DLQConfig{Subject: …}` and only unit-test resolve → resolution integrable bug can land untested. Prefer: (1) unit tests for resolve (plan Task 1), (2) integration that still leaves open at least `dlqConfigForGame(resolvedFromSystemInfo)` after a direct DB seed of `holomush_system_info`, not a magic string constants both sides.
- **MEDIUM — incident with event_bus.Defaults.** Core applies `eventBusConfig.Defaults()` (`core.go:140`) which fills empty GameID with `"main"` (`config.go:153-154`), but process gameID is `cfg.GameID` (core flag) then DB — not event_bus. CLI does not Defaults() game_id. Document that digest: do not call `eventbus.Config.Defaults()` on the CLI path or it can force `"main"` and reintroduce mismatch when DB holds a ULID.
- **LOW — cross-tenant.** Wrong `--game-id` fails loud (good). No multi-tenant product claim; still correct.

### Suggestions
1. Seed system_info in integration DB; resolve path → subject; wrong override vs right override.
2. Keep unit tests table-driven for four precedence cases including all-empty (legacy main default).
3. Comment on `replay.go` / CLI: recovery requires matching capture game_id.

### Risk
**LOW–MEDIUM**.

---

## Phase success criteria map

| Criterion | Plans | Will it hold? |
|---|---|---|
| #1 events_audit bounded by RetentionWorker | 06-01 + 06-02 | **Partial as written:** needs Drop bookkeeping + DEFAULT policy clarity; residual unpruned DEFAULT |
| #2 vuln nats pin fails gate; ≥v2.14.3 | 06-03 | **Yes if** osv-scanner sees GHSA; govulncheck alone insufficient (verified by review evidence) |
| #3 DLQ external game_id + non-tautological test | 06-05 (+06-01 write path) | **Yes** if test stays divergent + natstest |
| #4 coverage policy == enforcement | 06-04 | **Docs + ratchet YAML: yes; blocking project: only after ruleset** |

---

## Cross-cutting suggestions (do before execute)

1. **Patch 06-01 Task 1** with: current column/index inventory; composite-PK prep on unpartitioned; ATTACH sequence; optional min/max attach.
2. **Patch 06-02 Task 1** with detach inventory/grace discovery algorithm (concrete SQL).
3. **One operator checklist** for protect-main: `vuln`, `codecov/project` (and confirm `codecov/patch` already present).
4. Keep wave order: Wave 1 (01/03/04) → Wave 2 (02/05). Do **not** ship 06-01 without the writeAuditRow half.
5. After 06-01: refresh comments at `projection.go:333-352`, `subsystem.go:8-10`, and any tests naming `ON CONFLICT (id)` (`dlq_replay_integration_test.go:126` rewritten in 06-05).

---

## Risk Assessment (phase)

| Area | Risk |
|---|---|
| OPS-02 correctness / data plane | **HIGH → MEDIUM** after 06-01 ATTACH/columns fix + 06-02 drop design |
| OPS-03 supply-chain gate | **LOW–MEDIUM** (A4) |
| OPS-04 CLI + test fidelity | **LOW–MEDIUM** |
| QUAL-01 doc/ratchet | **LOW** (ruleset deferred = incomplete blocking) |
| **Overall** | **MEDIUM**, temporarily **HIGH** until 06-01 attach/schema instructions are corrected |

---

### Must-fix before autonomous execute (blocking)

1. 06-01: stop “000009 verbatim”; use full post-migration schema (BIGINT + envelope + rendering + dek columns + all indexes).
2. 06-01: document ALTER PK on unpartitioned **before** ATTACH DEFAULT/range.
3. 06-02: specify how detached partitions are tracked and aged for drop.

Everything else is solid and grounded in the current code.


---

## Antigravity Review

No tool calls needed right now. Pausing to receive the background search results.
No tool calls needed right now. Waiting for projection.go search results.
# Phase 6: Operational Hardening & Assurance Gates — Plan Review

> [!NOTE]
> **Environment Context**: The source codebase was not present in the active workspace path (`/Users/sean/.gemini/antigravity-cli/scratch`), so code-level assertions have been validated against the provided research logs, migration rules, and canonical architectural findings. Code-level verifications are highlighted and flagged with explicit open questions where appropriate.

---

## Executive Summary

Phase 6 provides a comprehensive, well-researched, and operationally rigorous execution strategy for hardening HoloMUSH's v0.11 foundation. The plan set (06-01 through 06-05) addresses all four target requirements (**OPS-02**, **OPS-03**, **OPS-04**, **QUAL-01**) with impressive technical depth. Crucially, the plans discover and mitigate several critical landmines that naive implementations would have missed—specifically, Postgres composite primary key constraints breaking `writeAuditRow` deduplication in `projection.go`, `govulncheck`'s blind spot regarding NATS-server CVEs, and the tautological nature of the existing DLQ replay test. The wave breakdown and dependency sequencing (Wave 1: core substrate & gates; Wave 2: worker & divergent tests) are sound and maintain high structural safety.

---

## Overall Phase Assessment

- **Plan Quality & Completeness**: **EXCELLENT**. High attention to detail, explicit STRIDE threat modeling, and clear acceptance criteria for each task.
- **Dependency Sequencing**: Correct. Wave 1 establishes the atomic partition substrate (06-01), supply-chain gates (06-03), and coverage governance (06-04). Wave 2 builds the retention worker (06-02) and DLQ test suite (06-05) atop the solid Wave 1 foundation.
- **Overall Risk Rating**: **MEDIUM** (primarily driven by database migration details around table partitioning and rollback behavior in Plan 06-01).

---

## Detailed Plan Reviews

### Plan 06-01: `events_audit` Partition-Swap & Idempotency Crux (OPS-02 Core)

#### Summary
Plan 06-01 handles the single highest-risk change in the phase: converting `events_audit` into a RANGE-partitioned table while atomically updating `writeAuditRow` in `projection.go` to maintain conflict handling and idempotent deduplication across live projection and DLQ replay.

#### Strengths
- **Crux Identification**: Identifies that Postgres prohibits `PRIMARY KEY (id)` on range-partitioned tables unless the partition key is included (`PRIMARY KEY (id, timestamp)`), which would immediately crash `ON CONFLICT (id) DO NOTHING` on deployment.
- **Atomic Execution**: Bundles the SQL migration, conflict target change (`ON CONFLICT (id, timestamp)`), and deterministic timestamp derivation (`ulid.Time(id)`) into a single atomic plan/PR.
- **Zero-Copy Migration Strategy**: Uses an O(1) `ALTER TABLE ... RENAME` and attaches `events_audit_unpartitioned` as the `DEFAULT` partition, strictly adhering to `.claude/rules/database-migrations.md` by avoiding long-running data backfills during migration execution.

#### Concerns
- 🚨 **HIGH**: **Unprunable `DEFAULT` Partition Data**. Attaching `events_audit_unpartitioned` as the `DEFAULT` partition solves the zero-copy migration requirement, but Postgres `DEFAULT` partitions cannot be range-dropped or detached via bound checks. Legacy rows residing in `DEFAULT` will remain unpruned indefinitely by `RetentionWorker`.
- ⚠️ **MEDIUM**: **Rollback Data Loss Risk**. The down migration `000052_events_audit_partition.down.sql` drops the partitioned parent table (and all monthly child partitions). If rolled back after running in production, any new audit records written to the monthly range partitions will be irreversibly lost when the parent table is dropped.
- ⚠️ **MEDIUM**: **ULID Timestamp Drift across Month Boundaries**. Sourcing `timestamp` from `ulid.Time(id)` instead of `meta.Timestamp` aligns live and replay paths, but if events are generated near the end of a month and persisted shortly after midnight of the next month, partition creation boundaries must remain buffered.

---

### Plan 06-02: `events_audit` Retention Worker & PartitionManager (OPS-02 Worker)

#### Summary
Plan 06-02 implements `EventsAuditPartitionManager`, wires the dormant `RetentionWorker` into `SubsystemAuditProjection`, and exposes a configurable retention window (default 90 days).

#### Strengths
- **Surgical Subsystem Integration**: Co-locating worker lifecycle management inside `SubsystemAuditProjection` avoids modifying `productionSubsystems`, preventing signature changes across test suites and stringer re-generation.
- **DDL Safety**: Explicitly executes `ALTER TABLE ... DETACH PARTITION CONCURRENTLY` outside explicit transaction blocks to comply with Postgres DDL execution requirements.
- **Full Interface Satisfaction**: Provides concrete implementations for `EnsurePartitions`, `DetachExpiredPartitions`, `DropDetachedPartitions`, `PurgeExpiredAllows`, and `HealthCheck`.

#### Concerns
- ⚠️ **MEDIUM**: **Safeguarding the `DEFAULT` Partition**. Because `DetachExpiredPartitions` explicitly ignores the `DEFAULT` partition (to prevent detaching un-bounded legacy data), any row inserted with a missing or out-of-bounds timestamp will land in `DEFAULT` and silently evade retention pruning.
- ℹ️ **LOW**: **Grace Period Defaults**. Ensure `DropDetachedPartitions` grace period configuration gives sufficient operational margin before executing irreversible `DROP TABLE` statements.

---

### Plan 06-03: `nats-server` CVE Bump & Vuln-Scan CI Gate (OPS-03)

#### Summary
Plan 06-03 remediates the `nats-server` CVE by upgrading to `≥ v2.14.3` and establishes a two-tiered vulnerability scanning CI gate utilizing `govulncheck` and `osv-scanner`.

#### Strengths
- **Critical Vulnerability Scanner Gap Identified**: Discovers that `nats-server` CVEs (GHSA-q59r-vq66-pxc2 and GHSA-p957-7v2w-g93g) are absent from `vuln.go.dev`, proving that `govulncheck` alone would fail to detect `v2.14.2`.
- **Dual-Tool Strategy**: Pairs `govulncheck` (for Go callgraph reachability) with `osv-scanner` (for broad OSV/GHSA database coverage and native allowlisting via `osv-scanner.toml`).
- **Supply-Chain Guardrails**: Incorporates a explicit human-in-the-loop legitimacy verification checkpoint (Task 1) to inspect and pin tool release SHA-256 hashes prior to CI installation.

#### Concerns
- ⚠️ **MEDIUM**: **Ruleset Requirement Enforcement**. The plan introduces a `vuln:` workflow job in `.github/workflows/ci.yaml`, but the job will not block pull requests until an administrator manually adds `vuln` to the `protect-main` ruleset required status checks.
- ℹ️ **LOW**: **CI Tool Caching & Network Dependencies**. `osv-scanner` and `govulncheck` download vulnerability databases at runtime; network issues could occasionally cause transient CI job failures unless retry mechanisms or cached database definitions are configured.

---

### Plan 06-04: Coverage Policy Reconciliation & Project Ratchet (QUAL-01)

#### Summary
Plan 06-04 eliminates conflicting Codecov settings files, establishes a non-regressive project coverage ratchet (`target: auto, threshold: 1%`), and updates documentation (`CLAUDE.md` and `.claude/rules/testing.md`) to reflect actual CI enforcement rulesets.

#### Strengths
- **Duplicate Configuration Elimination**: Deletes redundant `codecov.yml` (375 B) in favor of authoritative `.codecov.yml`, removing precedence ambiguity.
- **Documentation Accuracy**: Corrects inaccurate doc assertions mandating "per-package >80%" coverage, aligning written rules with the enforced Codecov patch ruleset (`codecov/patch @ 80%`).
- **Pragmatic Coverage Ratchet**: `target: auto` prevents baseline coverage degradation without prematurely blocking pull requests on legacy, under-tested packages.

#### Concerns
- ℹ️ **LOW**: **Cumulative Coverage Drift under 1% Threshold**. A `threshold: 1%` tolerance allows minor project coverage drops per pull request. While designed to handle dual-upload timing jitter (`after_n_builds: 2`), cumulative unmitigated drops could gradually lower overall coverage over time.

---

### Plan 06-05: Audit-DLQ Replay `game_id` Resolution & Divergent Test (OPS-04)

#### Summary
Plan 06-05 resolves the audit DLQ replay subject mismatch by auto-resolving the server's persisted `game_id` from `holomush_system_info` (with `--game-id` override capability) and replaces the existing tautological test with a containerized `natstest` integration suite.

#### Strengths
- **Root Cause Resolution**: Solves issue F3 (#4787), ensuring DLQ replay subjects accurately target `internal.<ULID>.audit.dlq` rather than defaulting to `internal.main.audit.dlq`.
- **Non-Tautological Test Suite**: Replaces the flawed unit test (which used identical `"main"` subjects for both publisher and consumer) with a full `natstest` integration test that explicitly validates rejection on mismatched game IDs and success on matching game IDs.
- **Zero-Config Convenience**: Maintains seamless CLI ergonomics for operators by auto-fetching `game_id` directly from the database connection pool.

#### Concerns
- ℹ️ **LOW**: **Empty System Info Fallback**. Ensure that if `holomush_system_info` lacks a `game_id` record, the resolver degrades gracefully to the configuration default without throwing unhandled internal errors.

---

## Specific Suggestions & Improvements

1. **OPS-02 Partition Rollback Handling (Plan 06-01)**:
   Modify `000052_events_audit_partition.down.sql` to issue an `INSERT INTO events_audit_unpartitioned SELECT * FROM events_audit` before dropping the partitioned parent, ensuring data written while the partition migration was active is preserved during a rollback.
2. **`DEFAULT` Partition Cleanup Strategy (Plans 06-01 & 06-02)**:
   Add an explicit operational note or background maintenance task to inspect `events_audit_unpartitioned` (the `DEFAULT` partition). Once all legacy rows fall past the 90-day retention window, operators should execute `ALTER TABLE events_audit DETACH PARTITION events_audit_unpartitioned; DROP TABLE events_audit_unpartitioned;` to ensure complete O(1) table space reclamation.
3. **Automated Ruleset Verification Checklist**:
   Include a verification checklist in the phase wrap-up documentation highlighting the required GitHub repository administrative updates:
   - Add `vuln` check to `protect-main` ruleset.
   - Add `codecov/project` check to `protect-main` ruleset.

---

## Open Verification Questions

Because local source files were not directly indexed in the current scratch space, the following verification points should be confirmed against the codebase prior to execution:

1. **`writeAuditRow` Signature Alignment**: Confirm that `projection.go` does not have auxiliary callers outside `projection.go` and `replay.go` expecting `meta.Timestamp` to be returned or passed onward.
2. **System Info Key Schema**: Confirm that `holomush_system_info` stores the key literal as `"game_id"` in `internal/store/postgres.go`.

---

## Final Recommendation

**APPROVE WITH MINOR SUGGESTIONS**. The Phase 6 implementation plans demonstrate exemplary software architecture principles, proactive failure analysis, and thorough governance alignment. Proceed with execution according to the defined two-wave plan sequence.

---

## Consensus Summary

**Overall: NOT READY as planned. The two source-grounded reviewers (Codex: NOT READY / HIGH; OpenCode-grok: MEDIUM–HIGH, HIGH until 06-01 is fixed) converge that 06-01/06-02's partition strategy needs real rework and 06-04's premise is factually wrong. Antigravity's "APPROVE / EXCELLENT" is discounted — it did not read the source.**

### Agreed Strengths (2+ reviewers, grounded)
- The composite-PK ↔ `ON CONFLICT (id)` idempotency crux is correctly identified, and bundling the migration PK + conflict-target + timestamp change into one atomic PR (06-01) is the right call. `writeAuditRow` really is shared by live projection (`projection.go`) and DLQ replay (`replay.go:219`).
- Rejecting a govulncheck-only gate is correct — the nats-server advisory is absent from the Go vuln DB, so osv-scanner is genuinely required for success-criterion #2 (06-03).
- The current DLQ test is genuinely tautological (`"main"` on both sides, embedded `eventbustest`); replacing it with a divergent-game `natstest` test is the right intent (06-05).
- Deleting the duplicate `codecov.yml` and using `target: auto` as the ratchet mechanism is sound (06-04).
- Wave sequencing (W1 substrate/gates, W2 worker/tests) and homing the worker in `SubsystemAuditProjection` (avoiding the productionSubsystems cascade) are good.

### Agreed Concerns (highest priority first)

1. **[BLOCKER] The DEFAULT-partition migration strategy is not viable (all 3 reviewers).**
   - Codex (grounded): attaching the old table as DEFAULT *after* creating current/future partitions **fails** — legacy rows in the current-month ranges violate the DEFAULT constraint (Postgres scans & rejects). AND Postgres **forbids `DETACH PARTITION CONCURRENTLY` while a DEFAULT partition exists** → 06-02's core retention DDL is invalid against 06-01's permanent-DEFAULT design. **06-01 and 06-02 are mutually incompatible as written.**
   - OpenCode (grounded): ATTACH also requires the old table's PK to be reshaped to `(id, timestamp)` *before* ATTACH (its unique index must include the partition key) — the plan skips this.
   - All three: DEFAULT rows never range-DETACH/DROP → legacy + any out-of-range rows are permanently unprunable (partial failure of criterion #1).
   - **Fix:** redesign without a permanent DEFAULT — a resumable/bounded ATTACH (min/max range) or an explicit backfill sequence that leaves no DEFAULT before retention begins.

2. **[BLOCKER] 06-01 timestamp semantics + rolling-migration idempotency (Codex + OpenCode).**
   - `ulid.Time(id)` is NOT the canonical event time: Event ID and `Event.Timestamp` come from separate clock calls (`types.go:215`); hot history uses the envelope timestamp (`hot_jetstream.go:545`), cold history filters the DB `timestamp` (`cold_postgres.go:159`). Swapping to ULID ms changes tier-boundary / `NotBefore`/`NotAfter` behavior.
   - Rolling idempotency is untested and likely broken: a pre-migration row (`timestamp=meta.Timestamp`) and a post-migration replay of the same event (`timestamp=ulid.Time`) land in *different* partitions with different composite keys — Postgres cannot dedup on `id` alone → cross-partition duplicate. The plan's test only exercises two post-change writes.
   - **Fix:** derive the deterministic key from the preserved envelope timestamp, OR make ULID time canonical across all tiers; add a hot/cold boundary parity test AND a pre-up-live → post-up-replay idempotency test.

3. **[BLOCKER] 06-01 down-migration deletes post-up rows (Codex + Antigravity).** The down drops the partitioned parent + monthly children; every audit row written after the up is lost. **Fix:** data-preserving down (e.g. `INSERT … SELECT` back into the un-partitioned table before drop).

4. **[HIGH] 06-02 detach→drop age-tracking is unspecified (all 3 + internal plan-checker).** After DETACH the child is an ordinary table with no bound metadata; `DropDetachedPartitions` has no discovery mechanism for "past grace." Also `EnsurePartitions` covers only current+future months, so replayed historical events fall into the unprunable DEFAULT. **Fix:** durable detach metadata (rename-suffix + timestamp / side table) or deterministic drop-eligibility from validated names; cover the retention + DLQ horizon.

5. **[HIGH] 06-03 allowlist covers only osv-scanner (Codex).** govulncheck has no finding-suppression, so an accepted *reachable* GO vuln fails-closed before OSV runs — D-04's "documented allowlist" is only half-implemented. The OSV CLI syntax is also version-ambiguous (`--config … <target>` vs OSV-Scanner v2 `scan source …`). **Fix:** one exception policy across both scanners (with expiry/rationale); pin the OSV major version, then write & test its exact CLI. Keep the mandatory pre-bump behavioral test.

6. **[HIGH] 06-05 resolver precedence is inverted vs the server (Codex) + the test can't exercise the CLI bridge (Codex + OpenCode).** The plan does `--game-id → DB → config`, but the server does configured-root `game_id` first, DB only when empty (`core.go:300`). With an explicit root game_id ≠ the persisted DB value, auto-replay still targets the wrong subject. The replacement test in `internal/eventbus/audit` cannot reach the unexported `cmd/holomush` resolver — a hand-built `DLQConfig` only re-proves `ReplayDLQ`. **Fix:** mirror the server order exactly; move resolution into an importable package (or put the test in `cmd/holomush`) and make "actual resolver seam exercised" an acceptance criterion.

7. **[HIGH — orchestrator-VERIFIED] 06-04's premise is false: codecov/patch is NOT a required check.** Codex live-inspected `protect-main` ruleset `11923801`; the orchestrator independently confirmed via `gh api` — required checks are **Build, Lint, Test, CodeRabbit, Integration Test, E2E Test** only; neither `codecov/patch` nor `codecov/project` is present, and classic branch protection is off (404). `ci.yaml` marks the coverage upload nonfatal. **So codecov/patch does NOT currently block merges** — this contradicts durable memory `7qhyhb3hsb`/`v5k0e4zs3s` and CONTEXT D-07. The planned doc rewrite would write a NEW false MUST ("patch @ 80% is a hard merge gate"). **Fix:** QUAL-01 must EITHER add `codecov/patch` (and `codecov/project`) to the ruleset — making the gate real — OR rewrite the docs to the true state (patch *posts* a status but does not currently *block*). Success-criterion #4's "the gate blocks merges" half is currently UNMET.

8. **[MEDIUM] "1% threshold" is not "no-drop" (Codex + OpenCode).** A codecov `threshold` is permitted leniency below target; 1% permits repeated 1-point declines. **Fix:** use `threshold: 0%` for a true no-drop ratchet, or rename it a "1-point regression allowance" and stop calling it no-drop.

9. **[MEDIUM] 06-01 crypto-reviewer gate is required (Codex; orchestrator flagged).** Changes to `internal/eventbus/audit/projection.go` and `events_audit` migrations trip the mandatory `crypto-reviewer` surface (CLAUDE.md). Run `/holomush-dev:review-crypto` on 06-01 before push. (Research A3 argued the change is crypto-neutral because audit columns aren't AAD inputs — the gate confirms it.)

### Divergent Views
- **Overall verdict split:** Codex NOT READY / HIGH · OpenCode MEDIUM–HIGH (fixable; HIGH only until 06-01's ATTACH/column issues are corrected) · Antigravity APPROVE / MEDIUM. The divergence tracks source access: the two reviewers that read the code both demand 06-01/06-02 rework; Antigravity (no repo access) rubber-stamped. **Weight the grounded verdicts.**
- **06-01 parent-schema angle:** OpenCode frames it as "don't recreate columns from `000009` verbatim — the table has drifted (envelope/BIGINT/rendering/dek columns + extra indexes)"; Codex frames the same reality as "avoid reintroducing TIMESTAMPTZ (000038)." Consistent: build the parent from the *current live shape*, not `000009`.
- **Antigravity's net-new (ungrounded but plausible) contributions:** the down-migration data-loss concern (corroborated by Codex) and a ULID month-boundary partition-buffering note — worth carrying into the replan as things to verify.

### Recommendation
**Do not execute as-is.** Run `/gsd-plan-phase 6 --reviews` to incorporate. Rework concentrates in **06-01 + 06-02** (partition strategy — the DEFAULT approach must go; timestamp canonicality; data-preserving down; detach-tracking) and **06-04** (correct the false codecov-gate premise). **06-05** needs the resolver order flipped to mirror the server and a test that exercises the real CLI seam; **06-03** needs a cross-scanner exception policy + pinned OSV CLI. Re-run this review (esp. Codex) after the replan.
