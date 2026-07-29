---
phase: 06-operational-hardening-assurance-gates
plan: 02
subsystem: database
tags: [postgres, partitioning, events_audit, retention, detach-concurrently, ulid, eventbus, audit, deploy-choreography, backfill]

# Dependency graph
requires:
  - phase: 06-01
    provides: events_audit RANGE-partitioned on deterministic event_ms (composite PK, no DEFAULT partition), shared eventMsFromULID helper, writeAuditRow composite-PK dedup, legacy rows left in events_audit_unpartitioned
provides:
  - EventsAuditPartitionManager (production PartitionManager for events_audit) — detach-rename-then-drop-by-name-age with a durable provenance marker gated at stamp time
  - one-time Backfill re-homing events_audit_unpartitioned legacy rows into the partitioned table via 06-01's eventMsFromULID (idempotent, straddle-safe)
  - RetentionWorker wired into SubsystemAuditProjection with a synchronous Backfill+EnsurePartitions boot gate before the projection accepts traffic
  - RetentionWorker initial-delay mode so no destructive Detach/Drop fires on Start (red-deploy safety)
  - RetainWindow/PurgeInterval config (90d/24h) with non-positive rejection in the tested audit.Config surface
  - re-choreographed production deploy (deploy.yaml + sandbox runbook) that stops cloudflared+gateway+core before the 000052 migrate, starts only the readiness-gated new core, restores traffic last
  - bootstrap orphan check extended to scan events_audit_unpartitioned
affects: [cold-history tier, audit retention/compliance, production deploy runbook, 06 phase verification]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Detach-rename-then-drop-by-name-age: DETACH ... CONCURRENTLY (outside a tx) → rename to events_audit_<YYYY_MM>_detached_<unix> (durable grace clock in the name) → DROP when now-unix > grace"
    - "Durable partition provenance marker COMMENT ON TABLE ... IS 'holomush:events_audit_partition' — STAMPED only behind a schema-qualified pg_inherits child-ness gate (fail-closed on a same-named non-child); REQUIRED by reconcile + drop; survives DETACH and the _detached_<unix> rename"
    - "Two-pass detach recovery: FINALIZE pg_inherits.inhdetachpending children (interrupted concurrent detach) THEN reconcile crash-orphaned canonical-named former-children (marker-gated) THEN the normal detach"
    - "Synchronous boot gate (Backfill + EnsurePartitions ONLY — never a destructive RunOnce) runs BEFORE the projection accepts traffic; a gate failure returns from Start with nothing half-up"
    - "Cross-package same-name import alias: retaudit \"internal/audit\" everywhere internal/eventbus/audit references the RetentionWorker/PartitionManager/RetentionConfig/WithInitialDelay (both packages are `package audit`)"
    - "Deploy choreography (NOT expand/contract): sever the whole player-traffic path (cloudflared→gateway→core) before an incompatible-schema migrate, gate new-core start on the backfill readiness gate, restore traffic last"

key-files:
  created:
    - internal/eventbus/audit/retention_partitions.go
    - internal/eventbus/audit/retention_partitions_test.go
    - internal/eventbus/audit/subsystem_boot_gate_integration_test.go
  modified:
    - internal/eventbus/audit/subsystem.go
    - internal/eventbus/audit/subsystem_test.go
    - internal/eventbus/config.go
    - internal/audit/retention.go
    - internal/audit/retention_test.go
    - cmd/holomush/core.go
    - cmd/holomush/bootstrap_orphan.go
    - cmd/holomush/bootstrap_orphan_test.go
    - .github/workflows/deploy.yaml
    - site/src/content/docs/operating/how-to/sandbox/sandbox-operations.md

key-decisions:
  - "Provenance is provable at STAMP time (round-5 H2), not only at drop time: EnsurePartitions applies the durable marker ONLY after a schema-qualified pg_inherits child-ness check and FAILS CLOSED on a pre-existing same-named non-child that CREATE ... IF NOT EXISTS silently skipped"
  - "Boot gate runs Backfill + EnsurePartitions ONLY — never RunOnce — so a first boot on a red deploy never prunes; pruning is left to the periodic worker"
  - "RetentionWorker gains an initial-delay option so the audit worker's FIRST Detach/Drop only fires after the first PurgeInterval tick; the default (no option) preserves immediate-run for the ABAC access_audit worker"
  - "Deploy re-choreographed (operator chose OPTION A, deploy choreography, NOT expand/contract): stop cloudflared+gateway+core before migrate, start only the readiness-gated new core, restore traffic last — the brief audit-write outage is a deliberate bounded single-node risk with the readiness/backfill gate as the compensating control"
  - "06-01 + 06-02 co-ship as one indivisible PR: 000052 renames all history off events_audit and only 06-02's Backfill re-homes it, so a 06-01-only merge is a cold-history blackout"

patterns-established:
  - "Pattern 1: detach-rename-then-drop-by-name-age with a stamp-time-gated durable provenance marker"
  - "Pattern 2: synchronous non-destructive boot gate before a subsystem accepts traffic"
  - "Pattern 3: sever-traffic-path deploy choreography for an incompatible-schema single-step migration"

requirements-completed: [OPS-02]

coverage:
  - id: D1
    description: "EventsAuditPartitionManager prunes old partitions via detach-rename-then-drop-by-name-age (recent retained, within-grace detached not dropped) and covers the retention window backward + forward so an in-window historical DLQ replay lands in a real prunable partition"
    requirement: OPS-02
    verification:
      - kind: integration
        ref: "internal/eventbus/audit/retention_partitions_test.go (seed-old/recent → detach→rename→drop-past-grace; recent retained; within-grace not dropped; EnsurePartitions covers within-retention past event_ms)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Crash-atomicity + FINALIZE recovery: a canonical-named child detached-but-not-renamed is reconciled (marker-gated) on the next cycle; a child left pg_inherits.inhdetachpending is FINALIZEd — neither is permanently stranded"
    requirement: OPS-02
    verification:
      - kind: integration
        ref: "internal/eventbus/audit/retention_partitions_test.go (crash-orphan reconcile + inhdetachpending FINALIZE recovery)"
        status: pass
    human_judgment: false
  - id: D3
    description: "STAMP-TIME provenance gate: a full RunOnce over a manually-created markerless same-named NON-child leaves it with NO marker, NEVER renamed to _detached_<unix>, NEVER dropped (round-5 H2)"
    requirement: OPS-02
    verification:
      - kind: integration
        ref: "internal/eventbus/audit/retention_partitions_test.go (full-RunOnce non-child: no marker / no rename / no drop)"
        status: pass
    human_judgment: false
  - id: D4
    description: "One-time Backfill re-homes events_audit_unpartitioned legacy rows into the partitioned table with no history loss and straddle idempotency: legacy row + post-backfill replay of the same event → exactly ONE row (event_ms parity via 06-01's shared eventMsFromULID)"
    requirement: OPS-02
    verification:
      - kind: integration
        ref: "internal/eventbus/audit/retention_partitions_test.go (migration-straddle: legacy row + replay → 1 row; legacy queryable via events_audit; idempotent second call)"
        status: pass
    human_judgment: false
  - id: D5
    description: "RetentionWorker wired into SubsystemAuditProjection: synchronous Backfill+EnsurePartitions boot gate runs BEFORE the projection accepts traffic; a gate failure returns from Start; worker started with the initial-delay option; Stop drains it"
    requirement: OPS-02
    verification:
      - kind: integration
        ref: "internal/eventbus/audit/subsystem_boot_gate_integration_test.go (boot gate precedes projection; Start error on gate failure; clean Stop)"
        status: pass
    human_judgment: false
  - id: D6
    description: "Config validation rejects non-positive RetainWindow/PurgeInterval in the tested audit.Config surface; RetentionWorker initial-delay option skips the immediate destructive RunOnce (no Detach/Drop before the first tick)"
    requirement: OPS-02
    verification:
      - kind: unit
        ref: "internal/eventbus/audit/subsystem_test.go (Config.Validate rejects RetainWindow<=0 and PurgeInterval<=0)"
        status: pass
      - kind: unit
        ref: "internal/audit/retention_test.go (initial-delay: zero Detach/Drop immediately after Start, >=1 after a tick)"
        status: pass
    human_judgment: false
  - id: D7
    description: "Bootstrap orphan check also scans events_audit_unpartitioned when present, so residual legacy rows are visible to the pre-Start defense-in-depth gate after 000052's rename"
    requirement: OPS-02
    verification:
      - kind: unit
        ref: "cmd/holomush/bootstrap_orphan_test.go (scans events_audit_unpartitioned when present)"
        status: pass
    human_judgment: false
  - id: D8
    description: "Production deploy re-choreographed: deploy.yaml + sandbox-operations.md pre-migrate row-count budget probe → stop cloudflared+gateway+core → migrate (-T </dev/null) → readiness-gated new-core start (up -d --wait --no-deps core) → restore gateway+cloudflared last"
    requirement: OPS-02
    verification:
      - kind: manual_procedural
        ref: ".github/workflows/deploy.yaml + site/src/content/docs/operating/how-to/sandbox/sandbox-operations.md — deploy ordering (severs traffic path before migrate, gates new-core on readiness, restores traffic last)"
        status: pass
    human_judgment: true
    rationale: "Deploy-sequence correctness against a live production single-node sandbox is an operator-judgment surface (traffic severing, health-budget probe thresholds, escape hatches); static ordering is asserted but real safety is judged by the operator on the next production deploy"
  - id: D9
    description: "06-01 + 06-02 co-ship confirmed as one indivisible PR/ship unit (blocking pre-PR checkpoint, Task 6) — 000052 cannot reach production without the Backfill that restores cold history"
    requirement: OPS-02
    verification:
      - kind: manual_procedural
        ref: "git diff --name-only origin/main...HEAD co-presence (06-01 migration+projection, 06-02 Backfill+subsystem, deploy choreography) on the single branch gsd/phase-06-operational-hardening-assurance-gates — CO-SHIP CONFIRMED by orchestrator"
        status: pass
    human_judgment: true
    rationale: "Co-ship is a human/orchestrator ship-gate judgment (single-PR guarantee), confirmed here; must be re-verified is not split into two PRs at ship time"

# Metrics
duration: ~20min (Tasks 1-5) + checkpoint resolution
completed: 2026-07-15
status: complete
---

# Phase 06 Plan 02: events_audit Retention + Deploy Choreography Summary

**Production events_audit PartitionManager (detach-rename-then-drop-by-name-age with a stamp-time-gated durable provenance marker), a straddle-safe one-time Backfill of legacy rows, a synchronous boot gate wiring the previously-unwired RetentionWorker into SubsystemAuditProjection, and a re-choreographed 000052 deploy that severs the player-traffic path before migrating.**

## Performance

- **Duration:** ~20 min (Tasks 1-5) plus the Task 6 co-ship checkpoint resolution
- **Started:** 2026-07-15T12:21:04Z (first task commit)
- **Completed:** 2026-07-15 (Task 6 confirmed by orchestrator)
- **Tasks:** 6 (5 implementation + 1 blocking co-ship checkpoint)
- **Files modified:** 14 (3 created, 11 modified)

## Accomplishments
- **EventsAuditPartitionManager** (`retention_partitions.go`) — the first production `internal/audit.PartitionManager` impl for `events_audit`: EnsurePartitions covers the retention window backward + forward and stamps the durable `holomush:events_audit_partition` marker ONLY after a schema-qualified pg_inherits child-ness check (fails closed on a same-named non-child); DetachExpiredPartitions FINALIZEs `inhdetachpending` children, reconciles marker-gated crash-orphans, then detaches CONCURRENTLY + renames to `events_audit_<YYYY_MM>_detached_<unix>`; DropDetachedPartitions drops by parsed name-age AND the provenance marker.
- **One-time Backfill** re-homes `events_audit_unpartitioned` legacy rows into the partitioned table using 06-01's shared `eventMsFromULID`, so a legacy row and a later DLQ replay of the same event dedup to exactly one row.
- **RetentionWorker wired** into `SubsystemAuditProjection` behind a synchronous Backfill+EnsurePartitions boot gate that runs BEFORE the projection accepts traffic; the worker starts with a new initial-delay option so no destructive Detach/Drop fires on Start (red-deploy safety).
- **Config** gained `RetainWindow`/`PurgeInterval` (90d/24h) with non-positive rejection in the tested `audit.Config` surface; bootstrap orphan check extended to scan `events_audit_unpartitioned`.
- **Deploy re-choreographed** (`deploy.yaml` + `sandbox-operations.md`): pre-migrate row-count budget probe → stop cloudflared+gateway+core → migrate (`-T </dev/null`) → readiness-gated new-core start → restore traffic last.

## Task Commits

Each task was committed atomically:

1. **Task 1: events_audit PartitionManager (detach-rename-drop + stamp-time-gated provenance marker)** — `0dc39e46f` (feat)
2. **Task 2: one-time Backfill re-homes legacy events_audit_unpartitioned rows** — `4cd1744b8` (feat)
3. **Task 3: config knob + RetentionWorker initial-delay + subsystem wiring with synchronous boot gate** — `26fc3a542` (feat)
4. **Task 4: bootstrap orphan check also scans events_audit_unpartitioned** — `4bb6f76dc` (feat)
5. **Task 5: re-choreograph deploy for the 000052 migration window** — `18fd95bc6` (feat)
6. **Task 6: blocking co-ship checkpoint** — no code commit; CO-SHIP CONFIRMED (evidence below)

_Tasks 1-3 were TDD; commits collapse the RED/GREEN into the feat commit shown._

## Files Created/Modified
- `internal/eventbus/audit/retention_partitions.go` (created) — EventsAuditPartitionManager + Backfill
- `internal/eventbus/audit/retention_partitions_test.go` (created) — detach/drop/reconcile/FINALIZE/stamp-gate/straddle integration tests
- `internal/eventbus/audit/subsystem_boot_gate_integration_test.go` (created) — boot-gate-before-projection ordering test
- `internal/eventbus/audit/subsystem.go` (modified) — reordered Start (manager → Backfill → EnsurePartitions gate → projection → worker); Stop drains worker
- `internal/eventbus/audit/subsystem_test.go` (modified) — Config.Validate non-positive rejection unit test
- `internal/eventbus/config.go` (modified) — `audit.retain_window` operator knob (distinct from DLQ MaxAge)
- `internal/audit/retention.go` (modified) — RetentionWorker initial-delay option
- `internal/audit/retention_test.go` (modified) — initial-delay unit test
- `cmd/holomush/core.go` (modified) — threads RetainWindow; `retaudit` alias
- `cmd/holomush/bootstrap_orphan.go` (modified) — scans events_audit_unpartitioned when present
- `cmd/holomush/bootstrap_orphan_test.go` (modified) — orphan-scan test
- `.github/workflows/deploy.yaml` (modified) — re-choreographed CI deploy
- `site/src/content/docs/operating/how-to/sandbox/sandbox-operations.md` (modified) — re-choreographed manual runbook

## Decisions Made
None beyond the plan — the round-2 through round-5 review incorporations (stamp-time provenance gate, initial-delay worker, deploy choreography, non-positive config rejection, cross-package `retaudit` alias) were all executed as specified in PLAN.md.

## Deviations from Plan
None - plan executed exactly as written.

## Verification Evidence (confirmed green before this SUMMARY)
- `task test:int -- ./internal/eventbus/audit/...` — **169 pass** (PartitionManager prune/retain/reconcile/FINALIZE/stamp-gate; boot gate before projection; straddle idempotency; clean Stop).
- `TestBootstrap` `./cmd/holomush/` — **4 pass** (orphan check scans events_audit_unpartitioned).
- `task test ./internal/audit/` — **pass** (RetentionWorker initial-delay: no destructive cycle on Start).
- `task lint` + `task build` — **green**.

## Task 6 — Co-Ship Confirmation (CONFIRMED)

**Verdict: CO-SHIP CONFIRMED** by the orchestrator. The co-deploy invariant holds: all co-ship artifacts live on the SINGLE branch `gsd/phase-06-operational-hardening-assurance-gates`, so 06-01 and 06-02 are one indivisible PR/ship unit — 06-01 cannot merge separately, and 000052 cannot reach production without 06-02's Backfill that restores cold history.

**Co-presence evidence** (`git diff --name-only origin/main...HEAD`):
- **06-01 (migration + projection):** `internal/store/migrations/000052_events_audit_partition.up.sql` + `.down.sql`, `internal/eventbus/audit/projection.go`
- **06-02 (Backfill + boot gate):** `internal/eventbus/audit/retention_partitions.go`, `internal/eventbus/audit/subsystem.go`
- **06-02 (deploy choreography):** `.github/workflows/deploy.yaml` (stop cloudflared→gateway→core BEFORE migrate with `-T </dev/null`; readiness-gated new-core start `up -d --wait --no-deps core`; traffic restored last; round-5 pre-migrate backfill-budget probe) + `site/src/content/docs/operating/how-to/sandbox/sandbox-operations.md`

**Deploy-ordering evidence:** the re-choreographed sequence severs the entire player-traffic path (cloudflared → gateway → core) before the 000052 migrate, starts only the readiness-gated new core (gated on this plan's synchronous Backfill+EnsurePartitions boot gate), and restores gateway + cloudflared LAST — so the old binary's `INSERT ... ON CONFLICT (id)` never runs against the post-000052 schema, and no player request can reach the new core before the readiness gate completes (round-5 H1).

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## SHIP CONSTRAINT (carried forward — MUST hold at ship time)
- **06-01 + 06-02 MUST remain ONE PR.** 000052 renames all history off `events_audit`; only 06-02's Backfill re-homes it. A 06-01-only merge is a cold-history blackout. Do NOT split the phase into per-plan PRs.
- **The production migrate MUST use the Task 5 deploy choreography.** The 000052 migration is incompatible with a live old core writer (`event_ms NOT NULL` + dropped id-alone unique index). Use the stop-traffic-path → migrate → readiness-gated new-core → restore-traffic sequence in `deploy.yaml` / `sandbox-operations.md`. Run the pre-migrate row-count budget probe; if above threshold, run an ahead-of-deploy backfill or temporarily raise the core `start_period` before proceeding.

## Next Phase Readiness
- OPS-02 retention path is complete: `events_audit` no longer grows unbounded (the wired RetentionWorker prunes on its cycle).
- The phase is ready for verification/ship. The two ship constraints above are the only gates the shipper must honor.

## Self-Check: PASSED
- Created files verified on disk: retention_partitions.go, retention_partitions_test.go, subsystem_boot_gate_integration_test.go, deploy.yaml, 06-02-SUMMARY.md
- Task commits verified in git log: 0dc39e46f, 4cd1744b8, 26fc3a542, 4bb6f76dc, 18fd95bc6

## Post-completion fixes
- **PartitionManager DDL schema-qualified to public.** The phase-integration gate (full `task test:int`) surfaced a search_path-leak × bare-DDL interaction: the eventbus_e2e harness ran a non-LOCAL `SET search_path TO plugin_core_scenes, public` on the shared pgxpool, which leaked onto the pooled connection (pgx does not reset session state on release) and corrupted the audit subsystem's boot-time partition DDL. `EnsurePartitions`' `CREATE ... PARTITION OF` used bare, search-path-relative identifiers, so a within-retention backward month (not pre-created by migration 000052) landed its child partition in the leaked schema instead of `public`; the public-pinned child-ness gate then failed closed with `AUDIT_PARTITION_NAME_OCCUPIED`, breaking 3 specs (plugin_audit_isolation, plugin_audit_round_trip, plugin_crash_resilience). Fixed by public-qualifying every DDL write in `retention_partitions.go` (CREATE + COMMENT + DETACH FINALIZE + DETACH CONCURRENTLY + RENAME source + DROP) so the write side matches the public-pinned read/discovery side unconditionally, and by removing the redundant non-LOCAL harness `SET` at the source (all harness statements were already schema-qualified). The child-ness marker logic is unchanged (a non-public child genuinely is not a public child — the marker was behaving correctly). Production was never affected: plugin provisioning uses a separate connstring with `search_path` as a URL query param, so a non-public search_path never leaks onto the audit pool. Added an integration regression test proving `EnsurePartitions` lands children in `public` even under a leaked non-public search_path. Commits: 58f5bc7d9 (fix + regression test), e62629e64 (harness leak removal).

---
*Phase: 06-operational-hardening-assurance-gates*
*Completed: 2026-07-15*
