---
phase: 05-world-model-integrity-fixes-m2-m12
plan: 15
subsystem: world-model / character-creation genesis chokepoint
tags: [character-genesis, INV-WORLD-4, compile-fence, outbox, MODEL-04, guest, bootstrap-admin]
requires: [05-07, 05-09, 05-14]
provides:
  - "auth.CharacterGenesisService — ONE atomic character-creation primitive (character insert + optional binding + genesis envelope, one re-entrant tx, fail-closed)"
  - "all three production creation paths routed through genesis: registered (gRPC CreateCharacter → CharacterService.CreateBound), guest (GuestService), bootstrap-admin (SeedAdmin → CharacterService.Create)"
  - "compile-level fence: Create removed from auth.CharacterRepository, GuestCharacterRepository, CharRepoAdapter — only the genesis service inserts a character"
  - "per-path binding semantics: registered initial_bind, guest initial_bind_guest, bootstrap-admin no binding (empty bindReason)"
affects: [05-16]
tech-stack:
  added: []
  patterns: [atomic-application-service, re-entrant-tx-enrollment, compile-level-interface-fence, ordered-non-atomic-compensation, service-declared-narrow-deps]
key-files:
  created:
    - internal/auth/character_genesis.go
    - internal/auth/character_genesis_test.go
    - internal/auth/character_genesis_integration_test.go
  modified:
    - internal/auth/character_service.go
    - internal/auth/character_service_test.go
    - internal/auth/guest_service.go
    - internal/auth/guest_service_test.go
    - internal/auth/mocks/mock_CharacterRepository.go
    - internal/auth/mocks/mock_GuestCharacterRepository.go
    - internal/bootstrap/admin.go
    - internal/bootstrap/admin_test.go
    - internal/bootstrap/setup/adapters.go
    - internal/bootstrap/setup/subsystem.go
    - internal/grpc/server.go
    - internal/grpc/auth_handlers.go
    - internal/grpc/auth_handlers_test.go
    - internal/grpc/mocks/mock_CharacterServiceProvider.go
    - cmd/holomush/sub_grpc.go
    - cmd/holomush/core.go
    - internal/testsupport/integrationtest/harness.go
    - test/integration/auth/auth_suite_test.go
  deleted:
    - internal/auth/mocks/mock_GuestBindingCreator.go
    - internal/auth/mocks/mock_GuestTransactor.go
decisions:
  - "The atomic unit is character + optional binding + genesis envelope (round-4 B4). All three enroll in the world postgres txKey; the genesis service uses local kind/schema constants (never imports internal/world/outbox — that closes an eventbus-relay import cycle back through internal/admin/auth → internal/auth, same pattern as internal/world/service.go)."
  - "Player (guest/bootstrap) and admin role are NOT atomic with the genesis tx — player_repo and role_store use their own pools and do not read the world txKey. Ordering resolves the FK hazard: guest commits the player BEFORE genesis; bootstrap assigns the admin role AFTER the genesis tx commits. Orphan-player and admin-role-missing are accepted, documented compensation gaps outside INV-WORLD-4."
  - "No shared exported transaction executor introduced (05-14's txKey stays unexported); the round-2 CoreServer WithOutboxWriter design is superseded — the OutboxWriter lives inside the genesis service constructed at the composition roots."
  - "Fail-closed by construction: the genesis constructor rejects nil deps; the gRPC handler's non-transactional fallback (auth_handlers.go:521-526) and the s.transactor field/WithTransactor option are deleted."
  - "Actor on the genesis envelope is the owning player (char.PlayerID) — the already-authorized, committed fact that caused the creation."
metrics:
  duration: ~120min
  tasks: 2
  files: 21
  completed: 2026-07-12
status: complete
---

# Phase 5 Plan 15: Atomic Character-Genesis Chokepoint Summary

Closes round-3 Codex blocker #5: three production paths created characters
directly and would commit rows WITHOUT genesis envelopes, making INV-WORLD-4
("exactly one semantic envelope per successful externally-visible command") false
for character creation. This plan builds ONE atomic `CharacterGenesisService` and
routes ALL production character creation through it, then removes `Create` from
the auth-side repo interfaces so envelope-less creation is impossible by
construction, not by convention.

## What was built

**Task 1 — the atomic CharacterGenesisService.**
`internal/auth/character_genesis.go`: a service with service-declared narrow deps
(`CharacterWriter`, `GenesisTransactor`, `GenesisBindingCreator`,
`world.OutboxWriter`) mirroring the guest-service idiom. Its single
`Create(ctx, char, bindReason)` runs inside one re-entrant `InTransaction`: (1)
`delta := writer.Create(txCtx, char)`; (2) if `bindReason != ""`, create the
binding; (3) `OutboxWriter.WriteIntent(txCtx, intent, delta)` — the writer
allocates epoch/feed_position and finalizes (round-3 blocker #1). The constructor
fails closed on any nil dep. The genesis intent uses the taxonomy-declared
`character_genesis` kind and `core.NewULID()` identity. Because
`internal/auth` MUST NOT import `internal/world/outbox` (an eventbus-relay import
cycle), the kind/schema strings are local literals that mirror the taxonomy —
the same pattern `internal/world/service.go` uses.

Tests: unit (fail-closed constructor, writer→binding→envelope call order,
empty-bindReason no-binding mode, per-step rollback, nil-char rejection) +
integration against a real pool (one-tx atomicity of character+binding+envelope,
no-binding-still-envelope, ambient-tx enrollment with outer-rollback-removes-all-
three, failed-insert rollback).

**Task 2 — reroute all three paths + the compile fence.**
- `CharacterService` gains a `CharacterGenesis` dep and a `CreateBound(reason)`
  entry point. `Create`/`CreateWithMaxCharacters` route through genesis with
  bindReason `""` (bootstrap-admin no-binding, signature unchanged). `Create` is
  removed from `auth.CharacterRepository` (reads-only).
- gRPC `CreateCharacter` calls `characterService.CreateBound(ctx, pid, name,
  "initial_bind")`; `createCharacterAtomic` + its non-transactional fallback and
  the `s.transactor` field / `WithTransactor` option are deleted. `BindingRepo`
  narrows to `Current` (Subscribe/QueryStreamHistory keep it); `bindings` field
  stays for that lookup.
- `GuestService` commits the guest PLAYER first (own pool), then routes character
  + `initial_bind_guest` binding + envelope through genesis atomically; `Create`
  removed from `GuestCharacterRepository`; transactor/bindings deps dropped.
- `bootstrap.SeedAdmin` runs player → genesis (char + envelope, no binding) →
  admin role, in that order (round-4 B4). Its `Transactor` dep,
  `bootstrapTransactor`, and the `WorldTx` config plumbing are removed. A test
  asserts the role-assign is post-character-create.
- Composition roots (`cmd/holomush/sub_grpc.go`, `bootstrap/setup/subsystem.go`)
  build the genesis service from the concrete `worldpostgres.CharacterRepository`
  + transactor + binding repo + `OutboxStore`. No CoreServer `outboxWriter` field
  (round-2 design superseded).
- Auth mocks regenerated (`mockery`); stale `GuestTransactor`/`GuestBindingCreator`
  mocks deleted. The integrationtest harness + auth integration suite rewired;
  test-support direct char seeding uses the concrete world repo (outside the
  production fence by design, test-code only).

## Round-4 B4 compensation gaps (documented, accepted)

- **Orphan guest player.** If genesis fails after the guest player commit, the
  player exists without a character. `CreateGuest` best-effort deletes it
  (`cleanupGuestPlayer`); reconciled by re-run / guest cleanup. Outside
  INV-WORLD-4 (which binds character↔genesis-envelope, sound here).
- **Admin-role-missing.** If the admin role-assign fails after the character
  commits, the admin character exists without the role; reconciled by a SeedAdmin
  re-run. player/character/role are NOT one transaction (the round-3 claim was
  false) — only character + envelope are atomic.

## Verification

- `task build:all` — exit 0 (composition-root + interface change compiles; no import cycle).
- `task lint` — exit 0.
- `task test` — 10182 tests, 4 skipped (pre-existing), exit 0.
- `task test:int -- -run 'CreateCharacter|CreateGuest|SeedAdmin|Genesis' ./internal/auth/... ./internal/bootstrap/... ./test/integration/auth/` — 21 green.
- Harness end-to-end guest genesis path exercised: `task test:int ./test/integration/auth/` + `./test/integration/presence/` (ConnectGuest → real GuestService → genesis) green.
- `rg -n 'Fall back|non-transactional' internal/grpc/auth_handlers.go` → nothing.
- `rg -n 'Create\(ctx context.Context, char \*world.Character\)' internal/auth/ internal/bootstrap/setup/ --glob '!*_test.go'` → matches ONLY the genesis `CharacterWriter` interface declaration.
- `rg -n 'idgen.New' internal/auth/character_genesis.go` → nothing.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] internal/auth cannot import internal/world/outbox**
- **Found during:** Task 1 `task test` (setup failed — import cycle).
- **Issue:** importing `internal/world/outbox` for the `KindCharacterGenesis` /
  `AppSchemaVersion` constants pulls in the eventbus relay → `internal/admin/auth`
  → `internal/auth`, an import cycle.
- **Fix:** local `kindCharacterGenesis`/`genesisSchemaVersion` literals mirroring
  the taxonomy (the same pattern `internal/world/service.go` uses for its kind
  constants); documented on the constants.
- **Files:** internal/auth/character_genesis.go
- **Commit:** 2cede43a4

**2. [Rule 3 - Blocking] Removed WorldTx bootstrap plumbing (was only for the deleted deps.Transactor)**
- **Found during:** Task 2 wiring of the bootstrap composition root.
- **Issue:** the round-4 B4 sequential ordering removes `deps.Transactor` from
  SeedAdmin; `bootstrapTransactor` + `WorldTransactorProvider` + the `WorldTx`
  config field (set at `cmd/holomush/core.go:441`) then had no consumer. The
  genesis service builds its own `worldpostgres.NewTransactor(pool)`.
- **Fix:** removed `WorldTransactorProvider`, `bootstrapTransactor`, the `WorldTx`
  config field, and the `core.go` wiring line.
- **Files:** internal/bootstrap/setup/subsystem.go, cmd/holomush/core.go
- **Commit:** 51487a507

### Scope / tracking notes

- **Requirement MODEL-04 not marked complete here.** MODEL-04's envelope-emission
  rollout spans 05-10/05-11 as well; final marking is deferred to phase completion
  / the verifier. This plan closes the character-creation slice of it.
- **census (05-11).** The taxonomy already declares `character_genesis` (05-09);
  the coverage census meta-test lands in 05-11. This plan is the emitting site
  the census will bind — no census here by design.

## TDD Gate Compliance

Per the phase cadence (one green commit per task, every intermediate commit
compiling and passing), each task is a single atomic commit with the RED/GREEN
cycle followed internally: Task 1's failing tests were authored and observed
(the import-cycle RED was captured explicitly) before GREEN; Task 2's rerouted
tests were updated to the new shape before the interface removal compiled. No
intermediate commit ships a broken build. Commit types: `feat` (both tasks).

## Known Stubs

None. All three production creation paths route through the genesis service; the
only direct char-repo seeding that remains is the integrationtest harness
(test-support, outside the production fence by design).

## Threat Flags

None beyond the plan's `<threat_model>` (T-05-45/46/47/53 mitigated, T-05-48
accepted). No new network endpoint, auth path, or trust-boundary schema change:
the genesis service publishes an already-authorized, already-committed fact
through the existing OutboxWriter and imports no `internal/access`/crypto surface.

## Self-Check: PASSED

- FOUND: internal/auth/character_genesis.go, character_genesis_test.go, character_genesis_integration_test.go
- FOUND: Create removed from auth.CharacterRepository / GuestCharacterRepository / CharRepoAdapter (greps above)
- FOUND commits: 2cede43a4 (Task 1), 51487a507 (Task 2)
- GREEN: task build:all (0), task lint (0), task test (10182, exit 0), targeted task test:int (21) + harness guest-genesis path
