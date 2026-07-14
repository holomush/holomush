---
phase: 06-operational-hardening-assurance-gates
plan: 01
subsystem: database
tags: [postgres, partitioning, events_audit, ulid, eventbus, audit, crypto-gate, migration]

# Dependency graph
requires:
  - phase: 05
    provides: events_audit BIGINT epoch-ns schema (000038), envelope-proto AAD path, writeAuditRow shared audit-write body
provides:
  - events_audit converted to a RANGE-partitioned table keyed on a NEW deterministic BIGINT event_ms (composite PK (id, event_ms), NO DEFAULT partition)
  - shared eventMsFromULID(id) = ulid.Time(id.Time()).UnixNano() helper (reused by 06-02 Backfill, same package)
  - writeAuditRow dedups under the composite PK via ULID-derived event_ms; timestamp column source UNCHANGED (store-time)
  - data-preserving, idempotently-resumable down migration (000052)
  - scoped test/seed sweep so the full integration suite stays green under the partitioned table
  - crypto-reviewer READY verdict for the projection.go + events_audit migration surface
affects: [06-02 (backfill/retention/deploy-choreography reuses eventMsFromULID and co-deploys with 000052), cold-history tier, audit integrity]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Deterministic partition key derived from immutable event ULID (event_ms) — identical on live + DLQ-replay paths, so composite-PK ON CONFLICT dedups across producers"
    - "Partition-swap migration: regclass-guarded rename, legacy PK/index rename-to-_legacy BEFORE new parent creation, no DEFAULT partition, data-preserving down"
    - "Scoped (not blanket) test/seed sweep: RULE 0 exempt pre-000052 pinned-version inserts; RULE 1 real-ULID → eventMsFromULID; RULE 2 non-ULID gen_random_bytes → store-time"

key-files:
  created:
    - internal/store/migrations/000052_events_audit_partition.up.sql
    - internal/store/migrations/000052_events_audit_partition.down.sql
    - internal/store/events_audit_partition_migration_integration_test.go
    - internal/eventbus/audit/projection_idempotency_integration_test.go
    - internal/eventbus/audit/event_ms_test.go
  modified:
    - internal/eventbus/audit/projection.go
    - internal/testsupport/holomushtest/server.go
    - (13 additional test/seed files — see Files Created/Modified)

key-decisions:
  - "event_ms is a SEPARATE deterministic BIGINT partition key (ulid.Time(id.Time()).UnixNano()), NOT the timestamp column — timestamp keeps its JetStream store-time meaning so cold-history tier boundary is unchanged (A3 confirmation)"
  - "NO DEFAULT partition (a DEFAULT forbids DETACH ... CONCURRENTLY in 06-02 and never prunes); out-of-window inserts fail loud"
  - "Single-step composite-PK migration kept (operator chose deploy CHOREOGRAPHY, OPTION A, NOT expand/contract); the safe stop-old→migrate→start-new→gate-on-readiness sequence is delivered by 06-02 Task 5"
  - "Down migration copies partitioned rows back BEFORE dropping the parent; BOTH parent→temp and legacy→temp copies use ON CONFLICT (id) DO NOTHING for idempotent resume"

patterns-established:
  - "Pattern 1: ULID-derived deterministic partition key for cross-producer dedup (eventMsFromULID)"
  - "Pattern 2: legacy-relation rename-to-_legacy before recreating a partitioned parent so the parent owns freshly-created PK/indexes (ownership asserted by indrelid, not name existence)"

requirements-completed: [OPS-02]

coverage:
  - id: D1
    description: "events_audit is RANGE-partitioned on deterministic event_ms with composite PK (id, event_ms), no DEFAULT partition, legacy PK/indexes renamed to _legacy so the new parent owns freshly-created indexes; timestamp column type unchanged BIGINT; up→down data-preserving"
    requirement: OPS-02
    verification:
      - kind: integration
        ref: "internal/store/events_audit_partition_migration_integration_test.go (ownership/no-DEFAULT/unchanged-BIGINT-timestamp/up→down data-preservation)"
        status: pass
    human_judgment: false
  - id: D2
    description: "writeAuditRow dedups under the composite PK via ULID-derived event_ms (same event via live + DLQ-replay → exactly 1 row; timestamp = first-write store-time); eventMsFromULID helper deterministic"
    requirement: OPS-02
    verification:
      - kind: integration
        ref: "internal/eventbus/audit/projection_idempotency_integration_test.go (dedup under composite PK, no false dedup, conflict-target match)"
        status: pass
      - kind: unit
        ref: "internal/eventbus/audit/event_ms_test.go (eventMsFromULID deterministic ULID→UnixNano)"
        status: pass
    human_judgment: false
  - id: D3
    description: "Every HEAD-schema direct events_audit INSERT site supplies event_ms + covering partition; pre-000052 pinned-version inserts exempt; full task test:int stays green"
    requirement: OPS-02
    verification:
      - kind: integration
        ref: "task test:int (full suite) — 10631 pass"
        status: pass
    human_judgment: false
  - id: D4
    description: "crypto-reviewer READY verdict for the projection.go + events_audit migration surface (event_ms/timestamp are not AAD inputs; envelope-proto AAD path unchanged)"
    requirement: OPS-02
    verification:
      - kind: manual_procedural
        ref: "/holomush-dev:review-crypto over projection.go + 000052_events_audit_partition.{up,down}.sql — VERDICT: READY"
        status: pass
    human_judgment: true
    rationale: "Mandatory blocking pre-push crypto-reviewer gate (CLAUDE.md §Pre-Push Review Gates); a human/adversarial-agent verdict is required and is never auto-approvable"

# Metrics
duration: continuation
completed: 2026-07-14
status: complete
---

# Phase 6 Plan 01: events_audit Partition-Swap on a Deterministic event_ms Key Summary

**events_audit converted to a RANGE-partitioned table keyed on a ULID-derived deterministic BIGINT event_ms (composite PK, no DEFAULT partition), with writeAuditRow dedup-correct under the composite PK, a data-preserving down migration, and a crypto-reviewer READY verdict.**

## Performance

- **Duration:** continuation session (Tasks 1–3 executed prior; Task 4 gate resolved this session)
- **Completed:** 2026-07-14
- **Tasks:** 4 (Tasks 1–3 implementation, Task 4 crypto gate)
- **Files modified/created:** 22 across the three task commits

## Accomplishments
- Migration 000052 partition-swaps `events_audit` onto a NEW deterministic BIGINT `event_ms` (`ulid.Time(id.Time()).UnixNano()`) with composite `PRIMARY KEY (id, event_ms)`, NO DEFAULT partition, legacy PK + all `events_audit_*` indexes renamed to `_legacy` before the new parent is created; current + next-2 monthly partitions cover live writes. Down migration is data-preserving and idempotently resumable.
- `writeAuditRow` gains the shared `eventMsFromULID` helper, adds the `event_ms` column, and changes the conflict target to `ON CONFLICT (id, event_ms) DO NOTHING`; the `timestamp` column source is UNCHANGED (`pgnanos.From(meta.Timestamp)`, store-time). Proven by the live+replay-of-same-event integration test (exactly 1 row; persisted timestamp = first-write store-time).
- Scoped test/seed sweep across 14 test/testsupport files: HEAD-schema direct INSERTs supply `event_ms` (real-ULID via `eventMsFromULID`; non-ULID `gen_random_bytes` seeds from store-time) + covering partition; pre-000052 pinned-version inserts left exempt. Full `task test:int` stays green.
- Crypto-reviewer pre-push gate (Task 4) returned **READY** — the change is crypto-neutral.

## Task Commits

Each task was committed atomically:

1. **Task 1: Migration 000052 — partition-swap events_audit on deterministic event_ms key** - `e1a0c46ad` (feat)
2. **Task 2: writeAuditRow idempotency crux — event_ms column + composite conflict target** - `49d8f514c` (feat; TDD — helper unit test + idempotency integration test)
3. **Task 3: test/seed blast-radius sweep — event_ms + covering partitions** - `c29fb6281` (test)
4. **Task 4: crypto-reviewer pre-push gate** - no code file; verdict recorded in this SUMMARY

**Plan metadata:** committed with this SUMMARY (`docs(06-01): complete events_audit partition-swap plan`)

_Note: Task 2 (tdd) carries the helper unit test (`event_ms_test.go`) + the idempotency integration test alongside the projection.go implementation in a single feat commit._

## Files Created/Modified
- `internal/store/migrations/000052_events_audit_partition.up.sql` (NEW) - partition-swap DDL, deterministic event_ms key, no DEFAULT, legacy rename-to-_legacy
- `internal/store/migrations/000052_events_audit_partition.down.sql` (NEW) - data-preserving, idempotently-resumable reversal
- `internal/store/events_audit_partition_migration_integration_test.go` (NEW) - ownership / no-DEFAULT / unchanged-BIGINT-timestamp / up→down data-preservation
- `internal/eventbus/audit/projection.go` (MOD) - `eventMsFromULID` helper, event_ms column, `ON CONFLICT (id, event_ms)`, doc comment rewrite; timestamp unchanged
- `internal/eventbus/audit/projection_idempotency_integration_test.go` (NEW) - live+replay-of-same-event → 1 row under composite PK
- `internal/eventbus/audit/event_ms_test.go` (NEW) - eventMsFromULID deterministic unit test
- `internal/testsupport/holomushtest/server.go` (MOD) - shared seed helpers ensure covering partition + supply event_ms
- Sweep test/seed files (MOD): `cmd/holomush/admin_read_stream_e2e_test.go`, `cmd/holomush/bootstrap_orphan_test.go` (arbitrary 16-byte id → real `ulid.Make()`), `internal/admin/policy/verifier_integration_test.go`, `internal/admin/readstream/cold_reader_integration_test.go`, `internal/eventbus/audit/chain/repo_postgres_integration_test.go`, `internal/eventbus/crypto/dek/rekey_orchestrator_test.go`, `internal/eventbus/crypto/dek/rekey_phase3_integration_test.go`, `internal/eventbus/history/cold_postgres_integration_test.go`, `internal/store/events_audit_test.go`, `internal/store/migrate_integration_test.go`, `internal/store/migrate_test.go`, `test/integration/crypto/harness_impl_test.go`, `test/integration/crypto/rekey_inv39_test.go`, `test/integration/crypto/rekey_sweep_ttl_test.go`, `test/integration/eventbus_e2e/cross_tier_query_test.go`

## Decisions Made
- **event_ms is a separate deterministic partition key, NOT the timestamp column.** Keeping `timestamp` at store-time preserves the cold-history tier boundary and window-query semantics unchanged — resolved the review's timestamp-semantics blocker by design, not by a parity test.
- **No DEFAULT partition** — a DEFAULT forbids `DETACH ... CONCURRENTLY` (06-02) and never prunes; out-of-window inserts fail loud (correct).
- **Single-step composite-PK migration kept (deploy CHOREOGRAPHY, OPTION A)** — the safe stop-old→migrate→start-new→gate-on-readiness sequence is delivered by 06-02 Task 5; this plan does not redesign 000052 as expand/contract.

## A3 Confirmation (timestamp source unchanged → cold readers unaffected)
The `timestamp` column source is UNCHANGED — still `pgnanos.From(meta.Timestamp)` (JetStream store-time). `event_ms` is a NEW, separate BIGINT partition key derived purely from the immutable event ULID; it is never read by the cold SELECT (`cold_postgres.go:140`). Cold-tier readers that filter on `timestamp` (NotBefore/NotAfter/edge) are byte-for-byte unaffected by the event_ms addition — no hot/cold tier-boundary drift.

## Crypto-Reviewer Gate (Task 4) — VERDICT: READY

`/holomush-dev:review-crypto` reviewed the 06-01 crypto-surface diff (`internal/eventbus/audit/projection.go` + migration `000052_events_audit_partition.{up,down}.sql`) and returned **READY** with no blocking findings. Confirmations recorded:

1. **AAD-binding completeness (INV-CRYPTO-107):** `aad.Build` reads the envelope proto's Timestamp (`aad.go:106-114`), never the `events_audit.timestamp` scalar column and never `event_ms`; cold reads rebuild AAD from `proto.Unmarshal(row.Envelope)` (`cold_postgres.go:407-408` / `dispatcher.go:178-179`). New/changed scalar columns cannot perturb the AAD path.
2. **Idempotency / no DEK-rebind (CLUSTER-04, D-11):** `event_ms = eventMsFromULID(id)` is a pure function of the immutable ULID, so `(id, event_ms)` is 1:1 with `id`; composite-PK dedup remains globally enforced; `ON CONFLICT DO NOTHING` drops the second write wholesale with no crypto-column rebind. Proven by `projection_idempotency_integration_test.go:65-90`.
3. **Migration safety / DEK columns (INV-CRYPTO-25):** 000052 reproduces `envelope`/`codec`/`dek_ref`/`dek_version` and the `dek_ref` partial index verbatim, only adds `event_ms` into the composite PK, introduces no FK on `dek_ref`, leaves the encrypted-payload wire path untouched, and the down migration is data-preserving and cleanly reversible.
4. **Test sweep preserves crypto guards (INV-CRYPTO-25):** the `dek_ref`/`dek_version`-mismatch scanner test is intact; all test `event_ms` derivations match production; no rekey/DEK test bypasses a guard.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None - the crypto gate was the only blocking checkpoint and returned READY.

## User Setup Required
None - no external service configuration required.

## Verification Evidence (green)
- Unit `task test` (audit + store): 321 pass
- `task test:int` (full suite): 10631 pass
- `task lint`, `task lint:no-timestamptz`: pass
- `task build`: compiles

## Next Phase Readiness
- `events_audit` substrate is bounded-ready: partitioned on a deterministic key with a dedup-correct write path, no DEFAULT partition, data-preserving down.
- **Co-deploy constraint:** 06-01 renames all history off `events_audit`; it MUST ship in the SAME PR/ship unit as 06-02 (Backfill re-homes legacy rows + restores cold-history visibility; deploy choreography in 06-02 Task 5). Do NOT push 06-01 alone.
- `eventMsFromULID` is available (same package) for 06-02's Backfill.

## Self-Check: PASSED

---
*Phase: 06-operational-hardening-assurance-gates*
*Completed: 2026-07-14*
