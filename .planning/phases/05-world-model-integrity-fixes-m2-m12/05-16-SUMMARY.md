---
phase: 05-world-model-integrity-fixes-m2-m12
plan: 16
subsystem: auth guest-deletion lifecycle / world-model feed completeness (D-06)
tags: [character-reaping, INV-WORLD-4, D-06, anti-TOCTOU, tombstone, outbox, MODEL-04, guest]
requires: [05-03, 05-15, 05-09, 05-11, 05-14, 05-06]
provides:
  - "auth.CharacterReapingService — ONE atomic tombstone-emitting guest character-deletion primitive (per-character {binding delete + property cascade + guarded world Delete + character_deleted envelope} in one re-entrant world tx, then ordered player delete; version-bearing list; fail-closed)"
  - "both guest-deletion paths routed through it: the guest reaper (guest_reaper.go GuestCleaner) and failed-guest cleanup (guest_service.go cleanupGuestPlayer)"
  - "anti-TOCTOU substrate (round-6 R6-2): players.reaping_at (migration 000051) + world/postgres.PlayerReapingGuard (SELECT reaping_at ... FOR UPDATE) wired into the genesis service + PlayerRepository.MarkReaping"
  - "world/postgres.BindingRepository.DeleteByCharacter — in-tx guest-teardown hard delete of a character's bindings (RESTRICT FK, operator path untouched)"
affects: []
tech-stack:
  added: []
  patterns: [atomic-application-service, per-entity-transaction-resumable, mark-then-reject-toctou-serialization, re-entrant-tx-enrollment, service-declared-narrow-deps, ordered-non-atomic-compensation, documented-layering-exception]
key-files:
  created:
    - internal/store/migrations/000051_player_reaping.up.sql
    - internal/store/migrations/000051_player_reaping.down.sql
    - internal/world/postgres/reaping_guard.go
    - internal/world/postgres/reaping_guard_test.go
    - internal/auth/character_reaping.go
    - internal/auth/character_reaping_test.go
    - test/integration/auth/guest_reaper_tombstone_test.go
    - test/integration/auth/guest_reaper_race_test.go
  modified:
    - internal/auth/character_genesis.go
    - internal/auth/character_genesis_test.go
    - internal/auth/character_genesis_integration_test.go
    - internal/auth/postgres/player_repo.go
    - internal/auth/postgres/player_repo_test.go
    - internal/auth/guest_service.go
    - internal/auth/guest_service_test.go
    - internal/world/postgres/binding_repo.go
    - internal/store/migrate_test.go
    - internal/store/migrate_integration_test.go
    - cmd/holomush/sub_grpc.go
    - internal/bootstrap/setup/subsystem.go
    - internal/testsupport/integrationtest/harness.go
    - test/integration/auth/auth_suite_test.go
decisions:
  - "The atomic unit is exactly {binding delete + property cascade + guarded character Delete + character_deleted envelope} in ONE re-entrant world tx PER CHARACTER (05-14 txKey). The player row (auth player_repo, own pool) is NOT atomic with any tombstone tx (round-4 B4 two-pool boundary): characters are tombstoned + deleted FIRST (committed), THEN the player is deleted, so characters.player_id ON DELETE CASCADE becomes a no-op. Orphan-player is documented compensation outside INV-WORLD-4, symmetric to 05-15's orphan-guest-player gap."
  - "Per-character tx = resumable (round-6 Codex MEDIUM): a WORLD_CONCURRENT_EDIT on character N leaves the committed tombstones for 1..N-1 intact and the player un-deleted (retried next cycle, player stays marked reaping). The conflict is propagated with its oops code preserved (not masked into a tombstone-less success)."
  - "Anti-TOCTOU is closed at the CREATION side (round-6 R6-2 option (b), not option (a) single shared tx — precluded by the two-pool boundary): the reaper MARKS players.reaping_at before enumerating, and the genesis service rejects creation for a reaping player via SELECT reaping_at ... FOR UPDATE. The mark UPDATE and the genesis FOR UPDATE read contend on the same players row, so they serialize cross-connection."
  - "The reaper lists via the version-scanning worldpostgres.CharacterRepository.ListByPlayer (05-03/R6-1) so char.Version is the stored version and the guarded CAS Delete matches — never a version-blind list (Version==0 → permanent conflict)."
  - "The per-character tx mirrors world.Service.DeleteCharacter's cascade (round-6 R6-3): propertyDeleter.DeleteByParent(\"character\", id) runs BEFORE the character delete — entity_properties has no FK to characters, so a bare delete would orphan property rows."
  - "The reaping service is the SECOND sanctioned out-of-world writer under INV-WORLD-4 (deletion side); its envelope reuses the SAME character_deleted taxonomy kind DeleteCharacter emits (local literal — internal/auth MUST NOT import internal/world/outbox), never a new kind."
  - "The PlayerReapingGuard reads the AUTH players table on the WORLD tx connection — a DURABLE, INTENTIONAL layering exception required for the FOR UPDATE serialization with MarkReaping; documented on the type so a future layering pass does not relocate it."
metrics:
  duration: ~150min
  tasks: 3
  files: 22
  completed: 2026-07-13
status: complete
---

# Phase 5 Plan 16: Atomic Guest Character-Reaping (D-06) Summary

Closes the round-5 D-06 hole: the guest reaper and failed-guest cleanup deleted
characters through an FK cascade with NO tombstone, so a reaped guest's character
entered the feed via genesis (05-15) at creation but its deletion was invisible —
INV-WORLD-4 / delta-parity / feed-completeness were FALSE for a live production
path. This plan builds ONE atomic `CharacterReapingService` (the deletion-side
counterpart to 05-15's `CharacterGenesisService`) and routes ALL guest character
deletion through a tombstone-emitting world deletion path, so guest expiration
cannot produce genesis-without-tombstone feed history — even under concurrency.

## What was built

**Task 1 — anti-TOCTOU substrate (round-6 R6-2).**
Migration `000051_player_reaping` adds a nullable `players.reaping_at BIGINT`
(epoch-ns) durable reaping flag. `internal/world/postgres.PlayerReapingGuard`
(`EnsureNotReaping`) runs `SELECT reaping_at FROM players WHERE id=$1 FOR UPDATE`
on the ambient (genesis) tx connection (`querierFromCtx`), returning
`PLAYER_REAPING` when set and holding the players row lock until the genesis tx
commits. It carries a documented **durable layering exception** (it reads the
auth `players` table on the world tx connection — required for the FOR UPDATE
serialization with `MarkReaping`; a READ, outside the SQL fence). The
`CharacterGenesisService` gains a fail-closed `PlayerReapingGuard` dep and calls
`EnsureNotReaping` FIRST in its creation tx — a character can never be inserted
for a reaping player. `PlayerRepository.MarkReaping` sets `reaping_at` for a
guest (`is_guest=true`), taking the players row lock so it serializes with an
in-flight genesis FOR UPDATE read. All six genesis-constructor callers were
updated in the same commit (the compile fence).

**Task 2 — the atomic CharacterReapingService.**
`internal/auth/character_reaping.go`: service-declared narrow deps (a
version-scanning lister, a guarded character deleter, a property deleter, a
binding deleter, the re-entrant transactor, `world.OutboxWriter`, a guest player
deleter, a reaping marker). `DeleteGuestPlayer` (satisfies `auth.GuestCleaner`):
(1) `MarkReaping` FIRST; (2) list via the version-scanning `ListByPlayer`
(R6-1); (3) for EACH character its OWN re-entrant world tx {binding delete +
`DeleteByParent("character", id)` (R6-3 parity) + guarded `Delete(id, version)`
→ tombstone delta + `WriteIntent` (character_deleted kind, `core.NewULID()`,
system actor)}; (4) delete the player AFTER all tombstones. A
`WORLD_CONCURRENT_EDIT` propagates with its code preserved (retriable; earlier
tombstones survive, player stays marked). Fail-closed constructor (8 deps).

**Task 3 — reroute both paths + composition wiring + the D-06 gates.**
`cmd/holomush/sub_grpc.go` constructs the reaping service and injects it as BOTH
the guest reaper's `GuestCleaner` and the guest service's cleaner.
`guest_service.go` `cleanupGuestPlayer` routes through the reaping service.
`test/integration/auth/guest_reaper_tombstone_test.go` (`// Verifies:
INV-WORLD-4`) proves create-then-reap leaves BOTH the `character_genesis` and
`character_deleted` envelopes on the feed (tombstone after genesis in feed
order), the character + `entity_properties` + player rows gone, no un-tombstoned
FK cascade. `test/integration/auth/guest_reaper_race_test.go` proves both
interleavings: (a) genesis after `MarkReaping` is rejected `PLAYER_REAPING`; (b)
a genesis in flight holding the players FOR UPDATE lock blocks `MarkReaping`
until it commits, then the new character is enumerated + tombstoned.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] RESTRICT binding FK blocked the character-first tombstone delete**
- **Found during:** Task 3 `task test:int` (`TestGuestReaperTombstonesEveryCharacter` and siblings failed 23503).
- **Issue:** the reaping service deletes the character individually (to emit the
  tombstone via the guarded world `Delete`), but `player_character_bindings.
  character_id` is a RESTRICT FK — migration 000040 deliberately keeps it
  non-cascading so the OPERATOR character-delete path soft-ends bindings via
  `End()` for crypto forensic retention. Deleting the character row while a
  guest's `initial_bind_guest` binding referenced it violated the FK. The
  pre-05-16 reaper never hit this because it deleted the PLAYER first (the
  `player_id` ON DELETE CASCADE removed the binding before the character).
- **Fix:** added `BindingRepository.DeleteByCharacter` — an in-tx hard delete of a
  character's bindings, documented as **guest-teardown-only** (the reaped guest's
  entire player is removed regardless, matching the prior player_id-cascade
  behavior). The reaping per-character tx runs it BEFORE the property cascade and
  the character delete. The operator `DeleteCharacter` path (soft-end via `End()`
  for forensic retention) is UNTOUCHED — the RESTRICT FK and its forensic
  invariant remain intact.
- **Files modified:** internal/world/postgres/binding_repo.go, internal/auth/character_reaping.go (+ the constructor dep threaded through all callers).
- **Commit:** 587697cd0

### Scope / tracking notes

- **Requirement MODEL-04 NOT marked complete here.** MODEL-04's envelope-emission
  rollout spans 05-09/05-10/05-11/05-15 and the phase-completion verifier; this
  plan closes the guest-deletion slice (D-06). Final marking is deferred to phase
  completion, mirroring 05-11/05-15.
- **INV-WORLD-4 binding.** The D-06 regression test carries the
  `// Verifies: INV-WORLD-4` annotation; wiring the registry `asserted_by` entry
  is 05-12's responsibility (per the plan). `task lint` (invariants check) is
  green with the annotation present.
- **Anti-TOCTOU option choice.** Closed at the creation side (mark-then-reject,
  round-6 R6-2 option (b)) rather than a single shared tx (option (a)) —
  precluded by the round-4 B4 two-pool boundary (player_repo's own pool cannot
  enroll in the world txKey).

## Verification

- `task build:all` — exit 0.
- `task lint` — exit 0 (incl. no-timestamptz migration gate, invariants render check).
- `task test` — 10213 tests, 4 skipped (pre-existing), exit 0.
- `task test:int` — 10617 tests, 7 skipped (pre-existing quarantines + opt-in resilience harness), exit 0. Includes the D-06 tombstone regression + the deterministic anti-TOCTOU race + migration 000051 up/down.
- `rg -n 'idgen.New' internal/auth/character_reaping.go` → nothing (tombstone events use `core.NewULID()`).
- `rg -n 'FOR UPDATE' internal/world/postgres/reaping_guard.go` → the reaping-reject locking read present.
- `rg -n '// Verifies: INV-WORLD-4' test/integration/auth/guest_reaper_tombstone_test.go` → present.
- The reaping envelope reuses the `character_deleted` taxonomy kind (no new kind).

## TDD Gate Compliance

Per the phase cadence, each task is a single atomic green commit with the
RED/GREEN cycle followed internally: Task 1's genesis reaping-reject + guard +
MarkReaping tests were authored/observed against the new substrate; Task 2's
reaping-service unit tests (fail-closed, mark-before-enumerate, version-bearing
list, cascade parity, per-character resumability) preceded GREEN; Task 3's D-06
regression + race integration tests drove the reroute. No intermediate commit
ships a broken build. Commit types: `feat` (all three tasks).

## Known Stubs

None. Both guest-deletion paths route through the tombstone-emitting reaping
service; the anti-TOCTOU substrate + guard are wired; migration 000051 applies +
reverts cleanly.

## Threat Flags

None beyond the plan's `<threat_model>` (T-05-54/55/56/61/62/63 mitigated,
T-05-57 accepted). No new network endpoint, auth path, or trust-boundary schema
change: the reaping service publishes already-committed deletion facts through
the existing OutboxWriter and imports no `internal/access`/crypto surface. The
one schema change (`players.reaping_at`, a nullable flag) and
`BindingRepository.DeleteByCharacter` (guest-teardown-only) do not alter a trust
boundary; the operator forensic-retention path is untouched.

## Self-Check: PASSED

- FOUND: internal/auth/character_reaping.go, internal/world/postgres/reaping_guard.go, migrations 000051 up/down, test/integration/auth/guest_reaper_{tombstone,race}_test.go
- FOUND commits: 776b87e64 (Task 1), 9eadc3aad (Task 2), 587697cd0 (Task 3)
- GREEN: task build:all (0), task lint (0), task test (10213, exit 0), task test:int (10617, exit 0)
- VERIFIED: reaping routes through the tombstone path; reaping_guard FOR UPDATE present; migration 000051 clean up/down; all NewCharacterGenesisService callers updated
