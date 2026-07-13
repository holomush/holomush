---
phase: 05-world-model-integrity-fixes-m2-m12
plan: 09
subsystem: world-model / taxonomy + writer-boundary enforcement
tags: [taxonomy, envelope-kinds, App-Schema-Version, character-settings, writer-boundary, sql-fence, MODEL-03, MODEL-04, ARCH-04]
requires: [05-05, 05-06, 05-08, 05-14]
provides:
  - "internal/world/outbox/taxonomy.go — versioned world-change kind registry (per-type payload schema + App-Schema-Version; ARCH-04/Phase-7 input)"
  - "world.Service.UpdateCharacterPreferences — guarded/versioned/enveloped character-preferences write (folded-in character_settings escape)"
  - "world/postgres.CharacterRepository.UpdatePreferences — version-guarded preferences CAS writer"
  - "settings.CharacterPreferencesUpdater / store narrow write port — settings write routes through the world boundary"
  - "test/meta/world_sql_fence_test.go — AST/token raw-world-SQL fence (schema-scoped to core/world; scene_participants + plugins/ excluded; migration channel closed)"
affects: [05-10, 05-11, 05-12, 05-15, 05-16]
tech-stack:
  added: []
  patterns: [versioned-taxonomy-registry, narrow-write-port, ast-string-literal-fence, migration-dml-fence, folded-escape-into-boundary]
key-files:
  created:
    - internal/world/outbox/taxonomy.go
    - internal/world/outbox/taxonomy_test.go
    - test/meta/world_sql_fence_test.go
  modified:
    - internal/world/postgres/character_repo.go
    - internal/world/postgres/character_repo_test.go
    - internal/world/service.go
    - internal/world/service_test.go
    - internal/world/mutator.go
    - internal/world/repository.go
    - internal/store/character_settings_repo.go
    - internal/settings/character_store.go
    - internal/settings/character_store_integration_test.go
    - cmd/holomush/sub_grpc.go
    - internal/world/worldtest/mock_CharacterRepository.go
    - internal/access/policy/attribute/character_test.go
    - test/meta/world_import_graph_test.go
decisions:
  - "The taxonomy is the rollout CONTRACT + ARCH-04 input; declared kinds are the vocabulary 05-10/05-11 wire commands to. The coverage census is NOT added here (lands 05-11 after rollout) so no intermediate commit ships a failing census — the deliberate data-first, enforcement-last ordering."
  - "character_settings' raw UPDATE characters is FOLDED into internal/world/postgres (round-4 C5 / D-05): version-guarded CAS + one character_preferences_update envelope in-tx; settings routes through a narrow CharacterPreferencesUpdater port (internal/store does NOT import internal/world)."
  - "world.Service.UpdateCharacterPreferences runs NO checkAccess — it is the settings persistence primitive (authorization is at the settings command layer; the prior raw path had no ABAC). Gating it here would be an out-of-scope behavior change (D-05)."
  - "The RMW conflict surfaces the typed WORLD_CONCURRENT_EDIT to the settings caller unchanged (D-02 — no auto-retry)."
  - "The SQL fence is a REAL go/ast string-literal scanner, NOT a depguard rule (depguard matches imported packages, not SQL literals; Codex finding 6). scene_participants + the plugins/ tree are EXCLUDED (D-05, #4815). It also scans migrations (baseline registered exception + world-sql-fence:allow marker)."
metrics:
  duration: ~24min
  tasks: 3
  files: 16
  completed: 2026-07-13
status: complete
---

# Phase 5 Plan 09: Taxonomy Registry + Character-Settings Fold-in + Raw-World-SQL Fence Summary

Interface-first slice-3 foundation: the versioned taxonomy schema registry (the
declared-envelope-kind contract the mechanical rollout fills and the designated
ARCH-04 input), the fold-in of the one genuine world-fence escape
(`character_settings`' raw `UPDATE characters`) into the guarded/versioned/envelope
world path, and the raw-world-SQL fence as a REAL AST/token meta-test —
schema-scoped to the core/world surface per D-05 (scene_participants + `plugins/`
excluded), honestly green because the escape was folded in first.

## What was built

**Task 1 — versioned taxonomy schema registry (`internal/world/outbox/taxonomy.go`).**
A `kind -> KindSchema` registry mapping each world-change envelope kind to its
per-type new-values-only payload schema (`[]PayloadField`), a per-type
`SchemaVersion`, its aggregate, and a tombstone flag; plus a package-level
`AppSchemaVersion` (the ARCH-04/Phase-7 revision stamp). Declared kinds: location
create/update/delete, exit create/update/delete, object create/update/delete/move,
`character_genesis` (Open Question 3 — its site is the 05-15 genesis service across
all three creation paths), `character_updated`, a single `character_deleted`
tombstone (REUSED by DeleteCharacter 05-11 and the guest reaper 05-16/D-06),
`character_moved`, and `character_preferences_update` (the Task-2 fold-in).
`Lookup` REJECTS an undeclared kind (`WORLD_TAXONOMY_UNKNOWN_KIND`) rather than
silently accepting it. Examine is absent (a read; Open Question 1) and NO
scene-participant kind is declared (D-07 — the vestigial write surface is removed
in 05-14, resolving the D-01↔D-05 contradiction by removal). 16 unit tests.

**Task 2 — fold `character_settings`' `UPDATE characters` into the world boundary
(round-4 C5 / D-05).** The one genuine world-fence escape — a raw, unversioned,
envelope-less write on its own pool — is moved into the sanctioned boundary:
- `world/postgres.CharacterRepository.UpdatePreferences(ctx, id, prefs, expectedVersion)`
  — a version-predicated CAS writer (mirrors the 05-03 Update/UpdateLocation CAS:
  `UPDATE ... SET preferences=$2, version=version+1 WHERE id=$1 [AND version=$3]`
  → locked follow-up read → `WORLD_CONCURRENT_EDIT` / `CHARACTER_NOT_FOUND`),
  returning a `wmodel.MutationDelta`.
- `world.Service.UpdateCharacterPreferences(ctx, id, prefs []byte)` routes through
  the `mutate()` seam with the `character_preferences_update` kind → exactly one
  envelope in the SAME transaction (INV-WORLD-4 for characters). It is an RMW:
  reads the version internally and CASes, so a concurrent conflicting write
  surfaces the typed `WORLD_CONCURRENT_EDIT` (MODEL-03; D-02 — no auto-retry). It
  runs no `checkAccess` (settings persistence primitive; the prior path had none).
  New `worldMutator.updateCharacterPreferences` closure-builder (the write-command
  descriptor the 05-11 census will include).
- `store.CharacterSettingsRepository` no longer issues a raw `UPDATE characters`:
  the READ stays a direct pool read; the WRITE delegates to the narrow
  `store.CharacterPreferencesUpdater` port (satisfied by `*world.Service`), so
  `internal/store` does NOT import `internal/world`. Constructor now takes the
  writer; `cmd/holomush/sub_grpc.go` injects `worldService`.
- `internal/settings/character_store.go`'s `CharacterRepository` doc records the
  new routing contract (the key_link to `world.Service.UpdateCharacterPreferences`).
- Tests: repo version-guard integration (`TestCharacterRepository_UpdatePreferencesVersionGuard`),
  service unit (envelope emitted once / conflict surfaced / not-found / missing
  executor), and the round-6 grok concurrent-RMW settings integration test
  (`TestRepoCharacterSettingsConcurrentWritesSurfaceConflict`) — two concurrent
  `SetPreferences` race the read-then-CAS; exactly one wins and the other surfaces
  `WORLD_CONCURRENT_EDIT` with no silent lost update; the settings caller receives
  the typed error. Regenerated `MockCharacterRepository` for the new method.

**Task 3 — raw-world-SQL fence as an AST/token meta-test (`test/meta/world_sql_fence_test.go`).**
A REAL source-scanning fence, NOT a depguard rule (Codex finding 6): it parses
production Go with `go/ast` and inspects STRING LITERALS ONLY (not comments, not a
naive grep) for `INSERT INTO` / `UPDATE` / `DELETE FROM` against the core/world
tables (`locations, exits, characters, objects, entity_properties, outbox,
world_feed_counter`), failing on any outside the allowlisted
`internal/world/postgres`. Per D-05 it EXCLUDES `scene_participants` and does NOT
scan the `plugins/` tree (a fixture proves a plugin `scene_participants` write is
not flagged), the top-level `test/` tree, or test-support trees. It ALSO scans
`internal/store/migrations/*.sql` (round-6 Codex MEDIUM): world-table data DML
fails UNLESS the file is the registered baseline seed (`000001_baseline`) or the
line carries a `-- world-sql-fence:allow` marker; schema DDL is permitted. The
reaping-guard auth-table `FOR UPDATE` read (05-16) is documented as a durable,
out-of-scope layering exception (round-9). Nine fence tests + the honest
tree-green assertion. NO census here (05-11).

## Verification

- `task lint` — exit 0 (added a line-scoped `//nolint:gosec` G122 on the fence's
  trusted-tree `os.ReadFile`; renamed a pre-existing shadowing local `skipDirs` →
  `importGraphSkipDirs` surfaced by the meta-package cache invalidation).
- `task test` — 10164 tests, 4 skipped (pre-existing), exit 0.
- `task test:int` — targeted world/postgres + world + settings + store (81 tests)
  green; the fold-in suite (`Preference|CharacterSettings`) green incl. the
  concurrent-RMW test run alone; full `./...` integration compile-check green.
- `task build` / `task build:all` — exit 0.
- Fence honestly green: `TestNoRawWorldSQLOutsideWriterBoundary` passes against the
  current tree only because Task 2 folded the escape in first.
- `grep -n 'UPDATE characters' internal/store/character_settings_repo.go` → nothing.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Interface-change ripple: hand-rolled attribute mock**
- **Found during:** Task 2 (integration compile-check after adding
  `UpdatePreferences` to `world.CharacterRepository`).
- **Issue:** `internal/access/policy/attribute/character_test.go`'s hand-rolled
  `mockCharacterRepository` no longer satisfied the interface.
- **Fix:** Added a `not implemented` `UpdatePreferences` stub (matches the sibling
  `UpdateLocation` stub).
- **Commit:** e4adabe37

**2. [Rule 1 - Blocking-gate] Pre-existing govet shadow surfaced by the new fence file**
- **Found during:** `task lint` (final gate).
- **Issue:** `test/meta/world_import_graph_test.go:92`'s local `skipDirs` shadows
  the package-level `skipDirs` (present since 05-07); adding the fence file
  invalidated the meta-package lint cache and surfaced it, blocking the gate.
- **Fix:** Renamed the local to `importGraphSkipDirs` (zero behavior change). Also
  added the required `//nolint:gosec` on the fence's own `os.ReadFile`.
- **Commit:** 0c6e40d77

**3. [Rule 3 - Blocking] Regenerated MockCharacterRepository**
- **Issue:** The new `UpdatePreferences` interface method left the generated mock
  stale.
- **Fix:** `task mocks:generate` (clean regen; only `mock_CharacterRepository.go`
  changed). Committed with Task 2 (cb5f848c0).

### Scope / tracking notes

- **Requirements NOT marked complete.** The plan frontmatter lists
  `[MODEL-03, MODEL-04]`, but these are phase-spanning: MODEL-03's version guard
  landed in 05-01..05-04 (and is EXTENDED here to the preferences write), and
  MODEL-04's full emission rollout completes in 05-10/05-11. Marking them complete
  here would be inaccurate; final marking is deferred to phase completion / the
  verifier. This is a deliberate omission, not a miss.
- **Plan counter.** `state.advance-plan` advanced the counter to 11; the sequential
  orchestrator owns wave ordering, so the coarse counter may lead the actual
  05-09 completion — noted for reconciliation.

## TDD Gate Compliance

Per the phase's established cadence (05-05/05-06 committed one green commit per
task, keeping every intermediate commit compiling and passing — the repo's
strong intermediate-commit-green requirement), each task here is a single atomic
commit, but the RED/GREEN cycle was followed internally: for every `tdd="true"`
task the failing test was authored and observed RED before the implementation was
written (Task 1's compile-fail RED was captured explicitly), then GREEN before
commit. No intermediate commit ships a broken build. Commit types: `feat` (Tasks
1–2), `test` (Task 3 meta-test + ripple), `style` (lint compliance).

## Known Stubs

None that block the plan goal. The taxonomy is interface-first: the mechanical
rollout (05-10/05-11) wires each remaining command to a declared kind, and the
coverage-asserting census meta-test lands in 05-11 (after rollout) — this is the
plan's explicit forward boundary, not incomplete work. Payload field lists in the
registry declare the shape the rollout constructs against; exact bytes are built
at each command site.

## Threat Flags

None. No new network endpoint, auth path, or trust-boundary schema change beyond
the plan's `<threat_model>` (T-05-27/28/52 addressed): the fence fires on a
negative fixture (proving it actually enforces), is schema-scoped to core/world
(a fixture proves a plugin `scene_participants` write is not flagged), and is
honestly green because the character_settings escape was folded in first.

## Self-Check: PASSED

- FOUND: internal/world/outbox/taxonomy.go, internal/world/outbox/taxonomy_test.go
- FOUND: test/meta/world_sql_fence_test.go
- FOUND: internal/world/postgres/character_repo.go (UpdatePreferences), internal/world/service.go (UpdateCharacterPreferences)
- FOUND commits: d82dd3203 (Task 1), cb5f848c0 (Task 2), d4c56c327 (Task 3), e4adabe37 (ripple), 0c6e40d77 (lint)
- VERIFIED: `grep -n 'UPDATE characters' internal/store/character_settings_repo.go` returns nothing
- GREEN: task lint (0), task test (10164, exit 0), targeted task test:int (81) + full integration compile-check, task build (0)
