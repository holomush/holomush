---
phase: 03-world-character-commands
plan: 01
subsystem: world-domain
tags: [world, character, lifecycle, outbox, abac, cas]
status: complete

requires:
  - "world.Caller typed-caller surface (Phase 02.1)"
  - "worldMutator.mutate() write-requires-envelope seam"
  - "classifyCASZeroRow / primaryDeltaVersioned (internal/world/postgres)"
provides:
  - "world.Service.RetireCharacter"
  - "world.Service.UnretireCharacter"
  - "world.CharacterRepository.SetStatus"
  - "outbox.KindCharacterRetired / outbox.KindCharacterUnretired"
  - "world.BuildCharacterLifecyclePayload"
  - "ABAC action strings: retire, unretire"
affects:
  - "plan 03-03 (two-replica stale-write proof drives the caller-supplied expected_version)"
  - "plan 03-04 (ABAC grants for the retire/unretire actions — ADMIN-ONLY for v0.13)"
  - "the retirement reactor (consumes character_retired)"

tech-stack:
  added: []
  patterns:
    - "caller-supplied expected_version with a pinned guard order (version-required -> access -> read -> version precheck -> lifecycle guard -> CAS)"
    - "one mutator executor serving two Service commands (the census keys on Service methods, not executor methods)"
    - "exhaustive status switch with a denying default arm (INV-WORLD-5)"

key-files:
  created:
    - internal/world/service_retire_test.go
    - internal/world/postgres/character_repo_status_test.go
    - internal/world/postgres/character_repo_status_integration_test.go
  modified:
    - internal/world/outbox/taxonomy.go
    - internal/world/service.go
    - internal/world/mutator.go
    - internal/world/repository.go
    - internal/world/payloads.go
    - internal/world/postgres/character_repo.go
    - internal/world/worldtest/mock_CharacterRepository.go
    - internal/world/caller_test.go
    - internal/access/policy/attribute/character_test.go
    - test/meta/world_sql_fence_test.go

decisions:
  - "The two command bodies are deliberately parallel, not factored into a shared helper — two meta-test AST cross-checks require each command's OWN body to reference s.mutator and to call s.checkAccess."
  - "The unit-level repo SetStatus tests are integration-tagged: internal/world/postgres has no mocked-pool harness, and a pure-unit test of a SQL CAS would assert nothing real."
  - "Task 1 landed CharacterRepository.SetStatus on the interface (plus the mock and the two hand-rolled fakes) in the RED commit, because the RED tests cannot compile without it."

metrics:
  duration: "~14 min"
  completed: 2026-08-09

actuals:
  tokens: 14900
  tasks: 3
  commits: 6
---

# Phase 03 Plan 01: World Character Retire/Unretire Commands Summary

Soft-retire and unretire land as two census-registered `world.Service` commands routing through `mutate()`, each carrying a caller-supplied `expected_version` whose conflict outranks every lifecycle-state guard.

## What was built

`world.Service.RetireCharacter` and `world.Service.UnretireCharacter` write `characters.status` through a version-predicated CAS and emit exactly one `character_retired` / `character_unretired` envelope in the same transaction. Both route through the new `worldMutator.setCharacterStatus` executor into a new `CharacterRepository.SetStatus`, which also clears `players.default_character_id` in the retire transaction (D-34).

### The R1 guard chain, in order

Both commands run the same six steps, and the order is the load-bearing part:

0. `expectedVersion <= 0` → `CHARACTER_VERSION_REQUIRED`, **before any read**. The repository keeps its `expectedVersion == 0` unversioned-write affordance for repo-level callers, but INV-WORLD-7 requires an existing-row character mutation to *carry* an expected version — so the commands make that affordance unreachable rather than merely discouraged.
1. `checkAccess` on the **distinct** actions `retire` / `unretire` (D-40), with no ownership predicate in Go (D-39).
2. `Get` — its `Version` arms step 3, its `Status` arms step 4.
3. Version precheck → `WORLD_CONCURRENT_EDIT`.
4. Lifecycle guard: exhaustive switch over the closed vocabulary with a denying default arm.
5. CAS carrying the **caller's** `expectedVersion`, never the freshly-read `char.Version` — passing the re-read value would make step 3's guarantee vacuous at the write.

Steps 3-before-4 is what makes the R1 property true: a stale caller racing a writer that already completed the transition sees the **conflict**, never that writer's outcome (`CHARACTER_ALREADY_RETIRED` / `CHARACTER_NOT_RETIRED`). Reporting the racing writer's state would tell the caller their stale view was authoritative. Both directions are unit-proven, each paired with an assertion that the mutator was **not** invoked and no envelope was emitted.

### Taxonomy

Two new kinds (`character_retired`, `character_unretired`) sharing a new `characterLifecyclePayload` (`character_id`, `status`). `characterUpdatePayload` is not reusable — it declares a `description` field a lifecycle change never carries, and the registry rule is new-values-only and erasure-safe. `AppSchemaVersion` bumped 1 → 2 once for the whole plan.

## The 02.1 caller shape actually used

The plan wrote every signature against `[ASSUMED — 02.1]` constructor names. Reconciled against the landed code, **the assumptions held exactly**:

| Assumed | Landed | Delta |
|---|---|---|
| `world.HumanCaller(subjectID)` | `func HumanCaller(subjectID string) Caller` | none |
| `world.SystemCaller()` | `func SystemCaller() Caller` (no params) | none |
| `world.JobCaller(name, prov)` | `func JobCaller(name string, prov Provenance) Caller` | none |
| `checkAccess(ctx, caller, action, resource, prefix)` | identical | none |
| `(ctx, caller Caller, characterID ulid.ULID, expectedVersion int)` | identical | none |

Two landed details the plan did not anticipate, both absorbed without redesign:

- The envelope actor is `caller.subject` (package-private field access), matching how `DeleteCharacter` and `UpdateCharacterDescription` already spell it.
- 02.1 shipped a **third** census, `TestWorldServiceCallerParamCensus`, which requires every command's own body to call `s.checkAccess`. It fired during the RED phase and is the second reason the two command bodies are not deduplicated.

## Census RED observations

Every gate was demonstrated RED before it was made green (PORTAL-10 rule 4).

| Task | Gate | RED message observed |
|---|---|---|
| 1 | Bijection (assertion A) | `command "RetireCharacter" maps to kind "character_retired", which MUST be declared in the taxonomy` |
| 1 | AST cross-check (assertion B) | `census descriptor "RetireCharacter" is not a world.Service method routing through the executor (stale descriptor)` |
| 1 | Caller-param census (02.1) | `world.Service.RetireCharacter is a command … but its body calls no s.checkAccess` |
| 2 | Bijection (assertion A) | `command "UnretireCharacter" maps to kind "character_unretired", which MUST be declared in the taxonomy` |
| 2 | AST cross-check (assertion B) | `census descriptor "UnretireCharacter" is not a world.Service method routing through the executor (stale descriptor)` |
| 3 | D-34 clear | `Should be empty, but was 01KZM2ZX0WYEPF3HB0X8959P55 — retire clears the default pointer` |

All RED phases failed on **assertions**, not on compilation — the stubs were declared and returned typed not-implemented errors precisely so a compile-only red could not masquerade as a gate.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] The shared-helper factoring of the two command bodies would have failed two meta-tests**

- **Found during:** Task 1 (GREEN)
- **Issue:** The first implementation factored the guard chain into a `setCharacterLifecycle(…, lifecycleTransition)` helper that both commands delegated to. That hides both AST signals: the envelope census requires each registered command to be a `*Service` method whose **own body** references the `s.mutator` selector, and 02.1's caller-param census requires each command's own body to call `s.checkAccess`. A delegating one-liner satisfies neither.
- **Fix:** Inlined both bodies. The duplication is intentional and mechanically enforced; a comment on `RetireCharacter` records why, so the next reader does not "clean it up" and turn two green censuses red.
- **Files modified:** `internal/world/service.go`
- **Commit:** 76c9ada9b

**2. [Rule 3 - Blocking] `CharacterRepository.SetStatus` had to land on the interface in the RED commit**

- **Found during:** Task 1 (RED)
- **Issue:** The plan assigned the interface method to the GREEN half, but the RED unit tests drive `mockRepo.EXPECT().SetStatus(…)` — without the interface method, the mock has no such method and the test file does not compile. A compile-only red is a degenerate red.
- **Fix:** The RED commit adds the interface method with a not-implemented stub on the postgres repo, regenerates the mockery mock, and widens the two hand-rolled fakes (`recordingCharacterRepo` in `internal/world/caller_test.go`, `mockCharacterRepository` in `internal/access/policy/attribute/character_test.go` — neither named by the plan, both found by the interface-widening compile check). GREEN replaces the stub body only.
- **Files modified:** `internal/world/repository.go`, `internal/world/postgres/character_repo.go`, `internal/world/worldtest/mock_CharacterRepository.go`, `internal/world/caller_test.go`, `internal/access/policy/attribute/character_test.go`
- **Commit:** 8cce4ed98

**3. [Rule 3 - Blocking] `character_repo_status_test.go` is integration-tagged, not a pure unit test**

- **Found during:** Task 1 (RED)
- **Issue:** The plan offered "mocked pool or integration" for the repo-level SetStatus behaviors. `internal/world/postgres` has **no** mocked-pool harness — every one of its 23 repo test files is `//go:build integration` against a testcontainer `testPool`. Building a pgx mock solely for this method would assert against a fake instead of against Postgres, which is precisely what the CAS split (stale-version vs absent-row, resolved by a locked follow-up read) needs a real database to prove.
- **Fix:** The file keeps the plan's name and carries `//go:build integration`, joining the existing harness. `character_repo_status_integration_test.go` holds Task 3's D-34 behaviors as planned.
- **Files modified:** `internal/world/postgres/character_repo_status_test.go`
- **Commit:** 8cce4ed98

**4. [Rule 2 - Missing critical coverage] Added D-40 action-split denial tests**

- **Found during:** Tasks 1 and 2
- **Issue:** D-40 splits `retire` from `unretire` (and from `write`) specifically so a policy can grant one without the other, but no planned test asserted the split actually holds — a command that checked `write` would have passed every other test in the plan.
- **Fix:** Two subtests: a caller granted only `write` is denied `RetireCharacter`; a caller granted only `retire` is denied `UnretireCharacter`. Both assert `CHARACTER_ACCESS_DENIED` and that `Get` was never called.
- **Files modified:** `internal/world/service_retire_test.go`
- **Commits:** 8cce4ed98, 09f980e4f

**5. [Rule 2 - Missing critical coverage] Added an INV-WORLD-6 name-preservation assertion**

- **Found during:** Task 1
- **Issue:** "Retire MUST NOT release the character's name reservation" is a plan prohibition with no planned assertion — it held only by inspection of the UPDATE statement.
- **Fix:** An integration subtest snapshots `name`, `normalized_name` and `name_skeleton` before and after a retire and asserts byte equality.
- **Files modified:** `internal/world/postgres/character_repo_status_test.go`
- **Commit:** 8cce4ed98

### Process note

Task 2's RED and GREEN edits were made before the RED commit was cut. Rather than collapse them into one commit and lose the gate, the RED tree state was reconstructed from `HEAD` plus the RED-phase artifacts (census row, kind const, stub), committed as `test(03-01)`, and the GREEN implementation restored and committed separately. The RED observation quoted in the table above was taken from the live run, not reconstructed. Git history therefore shows `test(03-01)` before `feat(03-01)` for all three tasks.

## Threat mitigations applied

| Threat | Disposition | Where |
|---|---|---|
| T-03-01 (EoP) | mitigated in-plan | `checkAccess` on distinct `retire`/`unretire` actions, no Go ownership predicate; grants land in 03-04 (ABAC is default-deny, so the commands are unreachable until then) |
| T-03-02 (Tampering, SQL) | mitigated | `status` is a typed `world.Status` const bound as `$2`; ids are stringified ULIDs; `expected_version` is numeric SQL equality (`AND version = $3`) on the INTEGER column |
| T-03-03 (Tampering, players write) | mitigated | idempotent single-statement clear in the same tx as the CAS; widening documented at the method and in the fence-test doc block |
| T-03-04 (Repudiation) | mitigated | exactly-one-envelope via `mutate()`; census bijection enforces the closed set |

## Known Stubs

None. Every stub introduced during a RED phase was replaced in the corresponding GREEN commit; a scan of the changed source for `not implemented` / `STUB` / `TODO` / `FIXME` returns nothing.

## Verification

- `task test -- ./internal/world/... ./test/meta/` — 1149 tests, exit 0
- `task test:int -- ./internal/world/...` — 1296 tests, exit 0
- `task lint` — exit 0 before every commit; `task fmt` output committed
- All 15 plan acceptance criteria checked mechanically (census counts, `AppSchemaVersion = 2`, single const, SG-5 correction absent, D-34 documented at both sites, `test(03-01)` before `feat(03-01)` ×3)

## Success Criteria

| Criterion | Status |
|---|---|
| Both commands are `*Service` methods routing through `s.mutator` (census green both directions) | met |
| Two kinds + two census rows + `AppSchemaVersion` 2 in the same plan | met |
| Caller-supplied expected_version with the R1 guard order unit-proven; stale CAS → `WORLD_CONCURRENT_EDIT` | met (two-replica proof is plan 03-03) |
| D-34 same-tx clear proven under the integration harness | met (4 behaviors, including the CAS-failure atomicity proof) |
| No name column touched by either command; `DeleteCharacter`'s path has zero diffs | met (`service.go` diff is insertions-only) |

## Requirement status

IDENT-04 and IDENT-10 are deliberately left **unchecked** in REQUIREMENTS.md. Both are claimed by five other plans in this phase (03-02 … 03-06), and IDENT-04's "the character leaves active play" is only true once the retirement reactor lands in 03-04. `requirements mark-complete` flipped both checkboxes off this plan's frontmatter alone; that flip was reverted as an overclaim. The last plan in the phase to close them will flip them honestly.

## Self-Check: PASSED

All created files exist on disk; all six commit hashes resolve in `git log`.
