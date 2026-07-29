---
phase: 05-world-model-integrity-fixes-m2-m12
plan: 11
subsystem: world-model / D-01 rollout completion + genesis snapshot + feed epoch + census
tags: [outbox, mutate-seam, reader-view-fence, genesis, feed-epoch, checkpoint-idempotency, census, INV-WORLD-4, MODEL-04, D-01, D-07]
requires: [05-05, 05-06, 05-07, 05-09, 05-10, 05-14, 05-15]
provides:
  - "world.Service.{DeleteCharacter,UpdateCharacterDescription} routed through the mutate() seam — one declared-kind envelope per command in the same tx (character_deleted / character_updated)"
  - "compile-time write fence COMPLETE: world.Service holds only reader views (LocationReader/ExitReader/ObjectReader/CharacterReader/SceneReader/PropertyReader); the write executor is the sole owner of the writer repos"
  - "internal/world/postgres.GenesisStore — cutover genesis snapshot (one checkpoint-idempotent envelope per existing aggregate) + persistent feed epoch read/advance (one-locked complete reset)"
  - "internal/world/outbox.GenesisStore consumer-owned interface + GenesisService orchestration (no outbox->postgres import; result types in wmodel leaf)"
  - "cmd/holomush world genesis / world epoch-reset admin CLI — the real operator entry (round-4 A3)"
  - "test/meta/world_envelope_census_test.go — bijection between the explicit in-Service write-command descriptor set (world.WriteCommands) and the declared taxonomy kinds, no allow-list (D-01)"
affects: [05-12]
tech-stack:
  added: []
  patterns: [reader-view-compile-fence, consumer-owned-store-interface, checkpoint-keyed-idempotency, one-locked-epoch-reset, explicit-closed-command-construct, go-ast-mutating-method-cross-check, shared-outbox-insert-helper]
key-files:
  created:
    - internal/world/fence_test.go
    - internal/world/wmodel/genesis.go
    - internal/world/outbox/genesis.go
    - internal/world/outbox/genesis_test.go
    - internal/world/postgres/genesis_store.go
    - internal/world/postgres/genesis_store_test.go
    - cmd/holomush/world_genesis.go
    - cmd/holomush/world_genesis_test.go
    - test/meta/world_envelope_census_test.go
  modified:
    - internal/world/service.go
    - internal/world/mutator.go
    - internal/world/payloads.go
    - internal/world/property.go
    - internal/world/service_test.go
    - internal/world/postgres/outbox_store.go
    - internal/world/postgres/cascade_delete_test.go
    - internal/property/entity_mutator_test.go
    - cmd/holomush/root.go
    - cmd/holomush/gateway_imports_test.go
decisions:
  - "DeleteCharacter emits one character_deleted tombstone (the SAME kind the 05-16 guest reaping service reuses); UpdateCharacterDescription emits one character_updated envelope and now surfaces WORLD_CONCURRENT_EDIT on a stale write (D-02) — matching the 05-10 Update/Move precedent."
  - "The reader-view compile fence is completed: world.Service's repo fields switched from the full Repository interfaces to the read-only reader views (new PropertyReader added), so a direct s.xRepo.Update()/.Delete()/.Create() is a type error. A reflection test pins the field types. The enforceable INV-WORLD-4 boundary = this reader-view fence + the AST SQL fence (05-09) + the composition allowlist (05-07) + the two sanctioned out-of-world writers (05-15 creation, 05-16 deletion)."
  - "Property writes (SetName/SetDescription -> UpdateLocation/UpdateObject, emitting since 05-10) funnel to EXACTLY ONE parent-update envelope — no duplicate property-level envelope; property Create/Update already on execerFromCtx (05-14). A property-path test asserts the single parent update."
  - "Genesis snapshot idempotency is checkpoint-keyed and SCHEMA-BACKED (round-3 MEDIUM): each aggregate's envelope inserts its world_genesis_checkpoint row (PK game_id/epoch/aggregate_type/aggregate_id) under the per-game counter FOR UPDATE lock BEFORE allocating a position; a same-epoch re-run conflicts and skips with no gap, a concurrent double-run serializes, and the identity survives outbox pruning."
  - "AdvanceEpoch is ONE locked, complete operation (round-6 Codex MEDIUM): under the counter lock it quarantines unpublished old-epoch outbox rows (marks published_at so the relay's next-unpublished scan never returns them), increments epoch, resets next_position to the origin (positions restart, not inherited), and fires the transaction-side relay wakeup. An integration test proves the leased relay never returns the quarantined row and the counter restarts at the origin."
  - "Genesis orchestration reaches postgres ONLY through the consumer-owned outbox.GenesisStore interface (round-4 A3): the impl is injected at the composition root; result types live in the wmodel leaf so postgres never references outbox and outbox never imports postgres. The 05-07 eight-edge import guard stays green."
  - "The census derives membership from an EXPLICIT closed construct (world.WriteCommands — 14 descriptors, each naming its kind), NOT name-prefix inference or the incomplete world.Mutator subset (Codex finding 10). A go/ast cross-check asserts the structural set of Service methods routing through s.mutator equals the descriptor set, and that the D-07-removed scene-participant surface is absent. character_genesis is classified out-of-Service (05-15 producer, asserted in 05-12). Completeness is honestly scoped: bijection = registry consistency; completeness = bijection + the paired fences."
metrics:
  duration: ~150min
  tasks: 3
  files: 19
  completed: 2026-07-13
status: complete
---

# Phase 5 Plan 11: D-01 Rollout Completion + Genesis Snapshot + Feed Epoch + Census Summary

Completes the D-01 mechanical emission rollout inside `world.Service`, closes the
compile-time write fence, adds cutover genesis snapshot emission with durable
checkpoint-keyed idempotency and a persistent one-locked feed epoch/reset (reached
through a consumer-owned `GenesisStore` interface + a real operator CLI), and lands
the coverage census on an explicit closed command construct — so it passes on
introduction (data-first, enforcement-last).

## What was built

**Task 1 — character + property write commands through the outbox + the reader-view fence (TDD).**
`DeleteCharacter` and `UpdateCharacterDescription` now route through the executor's
per-operation methods (`worldMutator.deleteCharacter` / `updateCharacter`) →
`mutate(intent, closure)`, each emitting exactly one taxonomy-declared envelope in
the same transaction (`character_deleted` tombstone / `character_updated`).
`UpdateCharacterDescription` surfaces `WORLD_CONCURRENT_EDIT` on a stale write (D-02).
The compile-time write fence is completed: `world.Service`'s repo fields switch from
the full `*Repository` interfaces to the read-only reader views (new `PropertyReader`
added), so the write-capable repos live ONLY on the write executor — a direct
`s.xRepo.Update()/.Delete()/.Create()` is now a type error. `fence_test.go` pins the
field types by reflection and asserts the reader interfaces carry no write method. A
property-path test confirms `SetName`/`SetDescription` funnel to exactly one
parent-update envelope (no duplicate). There is no scene-participant command to
migrate (removed in 05-14, D-07).

**Task 2 — genesis snapshot + feed epoch/reset behind a consumer-owned interface + admin CLI (TDD).**
`internal/world/postgres/genesis_store.go` emits one genesis envelope per existing
location/exit/character/object at the current epoch, each atomic with its
`world_genesis_checkpoint` row. The checkpoint insert runs under the per-game counter
`FOR UPDATE` lock BEFORE a position is allocated, so a same-epoch re-run conflicts on
the checkpoint PK and skips with no gap (race-safe; the identity survives outbox
pruning). `AdvanceEpoch` is ONE locked operation: it quarantines unpublished
old-epoch outbox rows (marks `published_at` so the relay never publishes a stale-epoch
position), increments the epoch, resets `next_position` to the origin, and fires the
transaction-side relay wakeup. The consumer-owned `GenesisStore` interface +
`GenesisService` orchestration live in `internal/world/outbox` (result types in the
`wmodel` leaf), so `outbox` never imports `postgres` (round-4 A3; the eight-edge
import guard stays green). `holomush world genesis` / `world epoch-reset`
(`cmd/holomush/world_genesis.go`) is the real operator entry, off crypto/abac
surfaces. The shared `insertOutboxRow` helper is used by both `WriteIntent` and the
genesis writer.

**Task 3 — the census meta-test.**
`world.WriteCommands()` is the EXPLICIT closed in-Service write-command descriptor set
(14 commands, each naming its taxonomy kind). `test/meta/world_envelope_census_test.go`
asserts a bijection between it and the declared taxonomy kinds with NO allow-list
(D-01); a go/ast cross-check asserts the structural set of `world.Service` methods
routing through `s.mutator` equals the descriptor set, and that the D-07-removed
scene-participant surface is absent. `character_genesis` is classified out-of-Service
(its producer is the 05-15 service, asserted in 05-12). Passes on introduction.

## Verification

- `task lint` — exit 0.
- `task build:all` — exit 0.
- `task test` — 10196 tests, 4 skipped (pre-existing), exit 0.
- `task test:int` — 10589 tests, 7 skipped (pre-existing quarantines + the opt-in
  resilience harness), exit 0. Includes the genesis snapshot/idempotency/concurrent/
  epoch-reset/quarantine integration tests and the `WriteIntent` refactor path.
- Census green on introduction (`-run Census ./test/meta/`, 3 tests).
- Import-graph guard green: `internal/world/outbox/genesis.go` imports only
  `context`, `log/slog`, `oops`, and `wmodel` — no `internal/world/postgres`.
- `holomush world genesis` / `world epoch-reset` present and registered under root
  (CLI structure test).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `cmd/holomush` gateway-imports allowlist**
- **Found during:** Task 2 `task test` (`TestGatewayImportsAreOnlyProtocolTranslation`).
- **Issue:** the new `world_genesis.go` CLI imports `internal/world/{outbox,postgres}`
  (by design — the operator entry constructs the store and drives the service), which
  the gateway-imports guard forbids for gateway files.
- **Fix:** added `world_genesis.go` / `world_genesis_test.go` to the same host-shell
  operator-tool allowlist that already exempts `outbox_admin.go` (05-07), with a
  documenting comment (No admin UDS, no crypto/abac).
- **Files:** cmd/holomush/gateway_imports_test.go.
- **Commit:** 7d8e26312.

### Design decisions within Claude's discretion

- **"Records reset metadata" via structured logging.** `AdvanceEpoch` records the
  reset metadata (previous/new epoch, quarantined count, origin position) through a
  `slog.WarnContext` line in `GenesisService.ResetEpoch` and the returned
  `wmodel.EpochResetResult`, not a new schema column — migration 000050 already
  landed in 05-05 and this plan adds no migration. Quarantine uses the existing
  `outbox.published_at` column (setting it marks a row so the relay's
  `NextUnpublished` scan skips it). If durable reset metadata is later required, it is
  a follow-up migration.
- **Genesis cutover kinds.** A cutover snapshot emits each aggregate's CREATE kind
  (`location_created` / `exit_created` / `object_created`) and `character_genesis` for
  characters — all taxonomy-declared. Genesis kind strings are local literals in
  `genesis_store.go` (mirroring the taxonomy) because `internal/world/postgres` MUST
  NOT import `internal/world/outbox` — the same local-mirror pattern `service.go` and
  the 05-15 genesis service use.

### Scope / tracking notes

- **Requirement `MODEL-04` NOT marked complete here.** MODEL-04 spans 05-10/05-11 (+
  the out-of-Service producer census in 05-12); final marking is deferred to phase
  completion / the verifier (mirrors 05-09/05-10/05-15).
- **Plan counter.** The sequential orchestrator owns wave ordering; the coarse
  Current-Plan counter may lead the actual 05-11 completion — noted for
  reconciliation (same caveat as 05-09/05-10).

## TDD Gate Compliance

Per the phase cadence, each task is a single atomic green commit with the RED/GREEN
cycle followed internally: for both `tdd="true"` tasks (1, 2) the failing tests were
authored and observed before implementation (Task 1: the pre-executor character
commands failed with "write executor not configured" until routed; Task 2: the
genesis/epoch/checkpoint tests failed until the store landed). Task 3 (the census) is
a `test`-type task and passes on introduction because 05-10 + Tasks 1-2 + 05-15
completed the coverage it asserts. No intermediate commit ships a broken build.
Commit types: `feat` (Tasks 1-2), `test` (Task 3).

## Known Stubs

None. All remaining `world.Service` write commands now route through the outbox; the
reader-view fence is complete; genesis emission + epoch reset have a real operator
entry point.

## Threat Flags

None beyond the plan's `<threat_model>` (T-05-32/33/44/50/51 mitigated; T-05-34/59
closed by 05-15/05-16 as named in the fence text). No new network endpoint, auth
path, or trust-boundary schema change: genesis/epoch publish already-committed facts
through the existing writer boundary and the CLI imports no `internal/access`/crypto
surface.

## Self-Check: PASSED

- FOUND: internal/world/postgres/genesis_store.go, internal/world/outbox/genesis.go,
  cmd/holomush/world_genesis.go, test/meta/world_envelope_census_test.go,
  internal/world/fence_test.go, internal/world/wmodel/genesis.go
- FOUND commits: 7f4a027c6 (Task 1), 7d8e26312 (Task 2), ea745c613 (Task 3)
- GREEN: task lint (0), task build:all (0), task test (10196, exit 0), task test:int
  (10589, exit 0), census on introduction, import-graph guard (outbox->postgres absent)
- VERIFIED: no scene-participant command in the census; `holomush world genesis` /
  `world epoch-reset` registered under root
