<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->

# Phase 5: World-Model Integrity Fixes (M2 / M12) - Pattern Map

**Mapped:** 2026-07-12
**Files analyzed:** ~24 create/modify targets (grouped by slice)
**Analogs found:** 9 strong in-repo analogs / 9 pattern families

This map is mechanism-locked (RESEARCH.md already enumerated every site with
`path:line`). Its job is to hand the planner the **concrete code to copy from**
for each new/modified file — not to re-derive the design.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/store/migrations/000049_world_version_guard.{up,down}.sql` | migration | CRUD/schema | `000001_baseline.up.sql:255-283` (access_policies version + versions table) | exact |
| `internal/store/migrations/00005X_world_outbox.{up,down}.sql` | migration | schema | `000001_baseline.up.sql` table + index idioms | role-match |
| `internal/world/location.go` (+exit/character/object structs) — add `Version int` | model | — | `location.go:48-58` (struct shape) | exact |
| `internal/world/postgres/location_repo.go` (Update/Delete → CAS) | repository | CRUD | `location_repo.go:66-97` (Update no-guard / Delete tx-aware) | exact (rewrite in place) |
| `internal/world/postgres/{exit,character,object}_repo.go` | repository | CRUD | same as location_repo | exact |
| `internal/world/mutator.go` / new mutation wrapper `mutate(ctx,entity,expectedVersion,envelope)` | service (chokepoint) | request-response | `mutator.go:18-57` (interface + compile-time assert) | role-match |
| `internal/world/outbox/relay.go` (new pkg) | backend worker | pub-sub / streaming | `internal/eventbus/audit/projection.go:113-276` (DLQ/poison posture — but HALT not continue) | role-match |
| `internal/world/outbox/*` counter + envelope write | repository | CRUD (locked counter) | `transactor.go:27-42` + `helpers.go:42` (tx enrollment) | exact |
| `internal/world/setup/subsystem.go` (relay subsystem wiring) | config/provider | — | existing `WorldSubsystem` registration (`subsystem.go:51-56`) | role-match |
| `internal/world/events.go` | DELETE (285 lines) | — | n/a — remove outright | n/a |
| `test/meta/world_envelope_census_test.go` (new) | test (meta) | — | `test/meta/depguard_config_test.go:18-57` (config-assertion meta-test) | role-match |
| `.golangci.yaml` depguard fence (raw world SQL) | config | — | `.golangci.yaml:136-154` (depguard deny block) | exact |
| `test/integration/resilience/m2_dualwrite_test.go` (rewrite) | test (integration) | — | existing resilience harness + `chaos_helpers_test.go` seams | role-match |
| `test/integration/resilience/m12_lastwritewins_test.go` (flip) | test (integration) | — | same harness | role-match |
| `internal/world/outbox/*_seams_test.go` (non-quarantined) | test | — | `internal/testsupport/integrationtest/resilience_seams_test.go:27-46` | exact |
| `docs/architecture/invariants.yaml` (+4 INV-WORLD-*) | config (registry) | — | existing yaml entries; ratchet per `.claude/rules/invariants.md` | role-match |
| MODEL-02 doc sites (CLAUDE.md, README.md, coding-standards.md, architecture.md) | docs | — | n/a — text downgrade | n/a |

## Pattern Assignments

### Version guard — migration (`000049_world_version_guard.up.sql`)

**Analog:** `internal/store/migrations/000001_baseline.up.sql:255-270`

The in-schema optimistic-concurrency precedent — `version INTEGER NOT NULL
DEFAULT 1` (line 269). `DEFAULT 1` backfills existing rows atomically, so no data
migration is needed. Mirror onto the four world tables only (locations, exits,
characters, objects — NOT `entity_properties`):

```sql
version      INTEGER NOT NULL DEFAULT 1
```

Migration rules (`.claude/rules/database-migrations.md`): paired `.up`/`.down`,
idempotent (`ADD COLUMN IF NOT EXISTS`), nullable-or-default (satisfied by
`DEFAULT 1`), down drops the column (`DROP COLUMN IF EXISTS version`). Next free
number confirmed **000049** (last on disk is `000048_disable_unconditional_scene_read_seed`).

---

### Version guard — struct field (`internal/world/location.go`)

**Analog:** `internal/world/location.go:48-58`

Add `Version int` to each of the four structs. Current shape:

```go
type Location struct {
	ID           ulid.ULID
	Type         LocationType
	...
	CreatedAt    time.Time
	ArchivedAt   *time.Time
	// ADD: Version int
}
```

`NewLocationWithID` (`:68-76`) constructs without a version — new entities get
`version=1` from the DB default; the struct field carries the read version back
in for the guarded write (do not hand-set on create).

---

### Version guard — CAS write + zero-row classifier (`internal/world/postgres/location_repo.go`)

**Analog (the site to rewrite):** `internal/world/postgres/location_repo.go:66-97`

Today `Update` (`:72`) uses **`r.pool.Exec`** with **no version predicate** and
collapses `RowsAffected()==0 → LOCATION_NOT_FOUND` (`:81-83`). `Delete` (`:89`)
already uses the tx-aware `execerFromCtx`. Two coordinated changes:

1. Switch every write method (`Update`, `Create`, `Move`) from `r.pool.Exec` to
   `execerFromCtx(ctx, r.pool).Exec` (Pitfall 1 — silent atomicity loss otherwise).
2. Add `AND version = $N`, `SET ... version = version + 1`, and replace the
   `RowsAffected()==0 → NOT_FOUND` collapse with a **locked follow-up read in the
   same tx** classifying conflict vs concurrent-delete vs not-found.

Current (no-guard) block to replace:

```go
result, err := r.pool.Exec(ctx, `
	UPDATE locations SET type = $2, ..., archived_at = $8
	WHERE id = $1
`, loc.ID.String(), ...)
if result.RowsAffected() == 0 {
	return oops.Code("LOCATION_NOT_FOUND").With("id", loc.ID.String()).Wrap(world.ErrNotFound)
}
```

Target: `... version = version + 1 WHERE id=$1 AND version=$N`; on zero rows,
`SELECT version FROM locations WHERE id=$1 FOR UPDATE` — exists+differs →
`oops.Code("WORLD_CONCURRENT_EDIT")`, absent → `LOCATION_NOT_FOUND`. Error style
matches the existing `oops.Code(...).With("id", ...).Wrap(...)` convention.

---

### Same-transaction outbox seam (`internal/world/outbox` + repo enrollment)

**Analogs:** `internal/world/postgres/transactor.go:27-42` and `helpers.go:41-47`

`Transactor.InTransaction` (`transactor.go:27`) begins a tx, stashes it in
context via `txKey{}`, commits on nil / rolls back on error. `execerFromCtx`
(`helpers.go:42`) pulls it back out:

```go
func execerFromCtx(ctx context.Context, pool execer) execer {
	if tx := txFromContext(ctx); tx != nil {
		return tx
	}
	return pool
}
```

The mutation wrapper wraps `state-change write + feed-position alloc + outbox row
insert` in a single `InTransaction`; every participating repo call MUST route
through `execerFromCtx` so all three enroll in the one tx. This is the exact
cascade-delete seam already proven by `DeleteLocation` (`service.go:270-287`).
Locked per-game counter: `SELECT next_position FROM world_feed_counter WHERE
game_id=$1 FOR UPDATE` inside the same tx (NOT `BIGSERIAL`).

---

### Compile-time write-requires-envelope seam (`internal/world/mutator.go`)

**Analog:** `internal/world/mutator.go:18-57`

The `Mutator` interface plus its compile-time assertion is the natural chokepoint
to reshape so an envelope-less write is a type error:

```go
// Compile-time check that Service implements Mutator.
var _ Mutator = (*Service)(nil)
```

Reshape the write methods to funnel through `mutate(ctx, entity, expectedVersion,
envelope)`; the `envelope` param is non-optional. Co-locate the wrapper in
`internal/world` (package `world`) so raw-repo writes are unreachable from
`world.Service` without an envelope.

---

### Relay poison posture (`internal/world/outbox/relay.go`)

**Analog:** `internal/eventbus/audit/projection.go:113-138, 254-276`

The audit projection is the reviewed DLQ/poison precedent: bounded DLQ
provisioned once at construction (`:131-136`), final-attempt capture + `Term`
past `MaxDeliver` (`:254-276`). **Key divergence to reconcile explicitly in the
plan:** the audit projection **DLQ-and-continues** (skips poison, keeps ordering
irrelevant); the relay must **HALT-and-alert** — an ordered gap-free feed
(`INV-WORLD-FEED-ORDER`) MUST NOT advance past an unpublishable position
(Pitfall 4). Reuse the construction/metric shape; invert the continue to a halt.
Publish with `Nats-Msg-Id` = envelope ULID (`core.NewEvent()` / `core.NewULID()`,
never `idgen.New()`), `LISTEN/NOTIFY` wakeup + periodic sweep, mark-published
after PubAck.

Alerting (slice 2, not deferred): mirror the counter style at
`internal/world/service.go:149` (`observability.RecordEngineFailure`).

---

### Lint fence (raw world-table SQL) — `.golangci.yaml` + meta-test

**Analog:** `.golangci.yaml:136-154` (depguard deny block) + `test/meta/depguard_config_test.go:18-57`

Two fences work together (same model as holomush-1eps2):

`.golangci.yaml` depguard `deny` block shape to extend:

```yaml
depguard:
  rules:
    no-test-only-constructs-in-production:
      files:
        - "!$test"
      deny:
        - pkg: github.com/holomush/holomush/internal/eventbus/eventbustest
          desc: "... production code MUST NOT import it (holomush-1eps2)"
```

The census/fence meta-test asserts the config claim can't silently drift —
`depguard_config_test.go:18-31` reads `.golangci.yaml` and `require.Contains`
each deny pkg. Copy this idiom for (a) the raw-world-SQL fence assertion and (b)
the write-command → declared-envelope-kind census (`INV-WORLD-WRITER-BOUNDARY`).

---

### Resilience seams — non-quarantined coverage (`internal/world/outbox/*_seams_test.go`)

**Analog:** `internal/testsupport/integrationtest/resilience_seams_test.go:16-46`

Codecov gotcha (RESEARCH §Validation): quarantine-gated resilience suites read as
**uncovered**. Every new outbox/relay seam needs a matching **non-quarantined**
seam test that self-skips off the gating lane but exercises the option/wiring
directly — copy the `TestStartOptionsApplyToConfig` pattern (`:27-46`) which
drives the `WithExternalNATS` / `WithSharedDatabase` closures with no containers.
Fault-injection extensions go in `test/integration/resilience/` via existing
seams: `WithExternalNATS` (`options.go:34`), `WithSharedDatabase` (`:52`),
`startReplica`/`pauseBroker`/`unpauseBroker` (`chaos_helpers_test.go:172,235,250`).

---

### Invariant registration (`docs/architecture/invariants.yaml`)

**Analog:** existing yaml entries; ratchet in `.claude/rules/invariants.md`.

`WORLD` is a **new boundary** (not among migrated scopes). Add four entries
`INV-WORLD-{ATOMIC-FEED,DELTA-PARITY,FEED-ORDER,WRITER-BOUNDARY}`, `boundary:
WORLD`, `binding: pending` (no `asserted_by` while pending), then bind each with a
genuinely-asserting `// Verifies: INV-WORLD-<NAME>` test, flip to `bound`, run
`go run ./cmd/inv-render`. Orphan-check caveat: the scan walks only
`docs/superpowers/specs/` — if the Phase 5 spec lives under `.planning/`, register
by hand (Pitfall 5: delta-parity must prove the manifest *matches* the delta,
presence is insufficient).

## Shared Patterns

### Tx enrollment (applies to every write repo method)
**Source:** `internal/world/postgres/helpers.go:42` (`execerFromCtx`)
**Apply to:** all `Create`/`Update`/`Move` methods in the four `*_repo.go` files.
The single most under-appreciated change — only `Delete` uses it today.

### Error codes (applies to all guarded writes + relay)
**Source:** `oops.Code("LOCATION_NOT_FOUND").With("id", ...).Wrap(world.ErrNotFound)` (`location_repo.go:82`)
**Apply to:** new `WORLD_CONCURRENT_EDIT` (typed, `world.Service` boundary, D-02);
relay poison/halt codes. Assert via `errutil.AssertErrorCode` in tests.

### Event identity (applies to envelope construction + relay)
**Source:** `core.NewEvent()` / `core.NewULID()` (`.claude/rules/event-conventions.md`)
**Apply to:** every envelope `event_id` = `Nats-Msg-Id`. Never `idgen.New()` for events.

### Subsystem lifecycle (applies to relay wiring)
**Source:** `WorldSubsystem` registration (`internal/world/setup/subsystem.go:51-56`)
**Apply to:** relay registers as a `lifecycle.Subsystem` with `SubsystemID` iota +
`DependsOn`; production `world.Service` is built here today with **no
`EventEmitter`** (`:66-75`) — replace that dead leg with relay wiring.

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `internal/world/outbox/relay.go` (position-ordered leased publisher) | worker | streaming | No existing single-leased position-ordered relay; nearest is the audit projection (different: DLQ-continue vs halt). Compose from projection construction shape + halt divergence. |
| `world_feed_counter` locked per-game counter | repository | CRUD | No existing `SELECT ... FOR UPDATE` monotonic counter in-repo; build per one-pager §3 (do NOT reuse `BIGSERIAL`). |
| versioned taxonomy schema registry (~15 types) | config/registry | — | Greenfield (ARCH-04 input). No existing registry to mirror; census meta-test is the enforcement analog. |
| MODEL-02 doc downgrades | docs | — | Pure text edits at grep-verified sites; no code analog. |

## Metadata

**Analog search scope:** `internal/world/**`, `internal/world/postgres/**`,
`internal/store/migrations/`, `internal/eventbus/audit/`, `test/meta/`,
`internal/testsupport/integrationtest/`, `.golangci.yaml`.
**Files read for excerpts:** location_repo.go, helpers.go, transactor.go,
mutator.go, location.go, audit/projection.go, depguard_config_test.go,
resilience_seams_test.go, 000001_baseline.up.sql, .golangci.yaml.
**Pattern extraction date:** 2026-07-12
