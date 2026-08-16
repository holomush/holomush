---
phase: 03-world-character-commands
plan: 03
subsystem: world-domain-verification
tags: [world, character, retire, resilience, outbox, atomicity, invariants]
status: complete

requires:
  - "world.Service.RetireCharacter with a caller-supplied expectedVersion (plan 03-01)"
  - "the two-replica resilience harness (test/integration/resilience, OPS-05)"
  - "the world integration suite's real-ABAC lifecycle env (test/integration/world)"
provides:
  - "a two-replica stale-expected_version rejection proof for RetireCharacter"
  - "a three-direction row+envelope atomicity proof for the retire path"
  - "a name-reservation proof driven through the production retire command"
  - "GitHub issue #4952 (INV-WORLD-6 rename-half registry defect)"
  - "GitHub issue #4953 (pre-existing resilience-suite breakage)"
affects:
  - "plan 03-04 (the retirement reactor consumes the character_retired envelope this proves is emitted exactly once)"

tech-stack:
  added: []
  patterns:
    - "caller-supplied expected_version makes a two-replica CAS conflict expressible under a FULLY SEQUENTIAL drive — no interleave hook, no production-predicate neutralization"
    - "guard-order proved as an assertion PAIR: the expected code is surfaced AND the racing writer's outcome code is not"
    - "atomicity asserted against committed DB rows in three directions, mirroring the INV-WORLD-1 reference proof case-for-case"

key-files:
  created:
    - test/integration/resilience/retire_concurrency_test.go
    - test/integration/world/character_retire_atomicity_test.go
    - .planning/phases/03-world-character-commands/deferred-items.md
  modified:
    - test/integration/world/character_lifecycle_test.go

decisions:
  - "Neither new spec carries a `// Verifies:` annotation. INV-WORLD-1 is already bound to the reference proof and INV-WORLD-6 to the paired lifecycle spec; a second claimant on a bound entry buys nothing and dilutes provenance, and the registry rule forbids binding a test to a claim it does not prove."
  - "The plan's full-suite acceptance command cannot exit 0: the resilience suite carries 4 pre-existing failures, established by a baseline run with the new file absent. The new Describe is verified green in isolation instead, and the breakage is filed as #4953 rather than fixed here (SCOPE BOUNDARY)."
  - "The atomicity spec's third case drives the transactor + repo directly rather than the Service. A caller-version CAS failure inside the transaction is unreachable through the public command by construction — 03-01's precheck fires first — so the direction that matters (state surviving a failed envelope) needs the repo seam."
  - "The resilience Describe boots WITHOUT in-tree plugins: it drives world.Service directly and dispatches no command, so it takes the lighter boot that boot_smoke / m2_dualwrite / outbox_faultinjection already use."

metrics:
  duration: "~35 min"
  completed: 2026-08-09

actuals:
  tokens: 10400
  tasks: 2
  commits: 2
---

# Phase 03 Plan 03: IDENT-10 Verification for the Retire Path Summary

The two-replica harness now proves a stale caller-held `expected_version` is rejected on `RetireCharacter` with the conflict and never with the racing writer's outcome, and the retire path's state change and its one envelope are proven to commit or roll back together in all three directions.

## What was built

### Task 1 — two-replica stale-version rejection (`test/integration/resilience/retire_concurrency_test.go`)

Retargets `m12_lastwritewins_test.go`'s spec-1 mechanism from `UpdateLocation` to `RetireCharacter`. M12 carries its expected version *inside* the `*world.Location` struct, so "two writers holding the same read version" is expressed as two `Get` results. 03-01 gave `RetireCharacter` a **caller-supplied** `expectedVersion`, so the same precondition is expressed directly: read the row once, hand the same integer to both replicas, drive them **sequentially**. A commits (version N → N+1); B calls with N and is rejected.

The spec asserts a **pair**, and the pair is the deliverable:

| Assertion | What it proves |
|---|---|
| `WORLD_CONCURRENT_EDIT` surfaced (`MatchError(ErrConcurrentEdit)`, `oops` code, and `errutil.AssertErrorCode`) | the guard closes on the new command |
| the code is **NOT** `CHARACTER_ALREADY_RETIRED` | the guard **order** holds — 03-01 pins the version precheck before the lifecycle-state guard, so a stale caller learns its view is stale, never the racing writer's outcome |

Plus: A's retire survived, and B left **no second version bump**. All read-backs go straight to the shared pgxpool (RESEARCH Pitfall 6). No quarantine-marker idiom appears anywhere in the file — gating stays in the suite entry point, so `test/meta/quarantine_registry_test.go`'s bijection is untouched (`task test -- ./test/meta/` exit 0).

### Task 2 — atomicity, name reservation, and the filed defect

**`test/integration/world/character_retire_atomicity_test.go`** mirrors the INV-WORLD-1 ATOMIC-FEED proof (`internal/world/postgres/outbox_store_test.go:134`) case-for-case, asserting against committed DB rows every time:

| Case | Assertion |
|---|---|
| commit | retired status + version bumped once + **exactly 1** `character_retired` outbox row for the aggregate |
| rejected write (stale `expected_version`) | status unchanged, version unchanged, **0** outbox rows |
| envelope failure after the state write | the `SetStatus` write **rolls back** — status active, version untouched, 0 outbox rows |

The third case drives `worldpg.NewTransactor` + `env.Characters.SetStatus` + a duplicate-`event_id` `WriteIntent` inside one transaction, poisoning the **real** envelope shape (kind, aggregate type, and the actual lifecycle payload). It is the only direction the public command cannot reach: 03-01's precheck rejects a stale caller before the transaction opens, so an in-transaction CAS failure is unreachable sequentially through `Service`.

**`test/integration/world/character_lifecycle_test.go`** gains one `It` alongside the existing INV-WORLD-6 spec: the name stays reserved when the retire runs through the **production** `RetireCharacter` command, not just through the direct-SQL status write the existing spec uses. That gap mattered — when the existing spec was written, direct SQL was the only way to reach the retired state; Phase 3 shipped a production writer that could have released the name.

`git diff --numstat` on that file: **34 insertions, 0 deletions**. The `// Verifies: INV-WORLD-6` block is byte-identical.

## Filed issues

| Issue | Title | Disposition |
|---|---|---|
| [#4952](https://github.com/holomush/holomush/issues/4952) | INV-WORLD-6 registry summary overclaims: rename half is false in production | **filed, not fixed** (rename territory, backlog 999.20) — the plan's deliverable |
| [#4953](https://github.com/holomush/holomush/issues/4953) | Resilience suite is fully red on the nightly lane: `natstest.Conn` dials a scoped NATS env without credentials | **filed, not fixed** (out of scope) — see Deviations |

#4952 carries RESEARCH §6.3's citations verbatim-grounded and re-verified against current code: the registry entry at `docs/architecture/invariants.yaml:5151-5163`; `CharacterRepository.Rename`'s four-column UPDATE at `internal/world/postgres/character_repo.go:228-235`; the shipped operator CLI call at `cmd/holomush/cmd_character_name.go:405`; INV-WORLD-4's cross-entry contradiction (it enumerates that same CLI as the third sanctioned writer); and the confirmation that the binding test never exercises rename. It states explicitly that the **retire half stands**.

## Deviations from Plan

### 1. [Rule 3 — Blocking, downgraded to deferred] The plan's full-suite acceptance command cannot exit 0

- **Found during:** Task 1, running the plan's own acceptance command.
- **Issue:** `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience ./test/integration/resilience/` exits non-zero with **4 failures**. Three are panics from `natstest.(*NATSEnv).Conn` dialing a **scoped** NATS container with no credentials (`nats: Authorization Violation`); the fourth reaches the broker through the same helper.
- **Established as pre-existing**, not caused by this plan, by a baseline run with the new file moved out of the package:

  | Run | Ran | Passed | Failed |
  |---|---|---|---|
  | baseline (new file absent) | 17 of 22 | 13 | 4 |
  | with the new Describe | 18 of 23 | 14 | **the same 4** |

  The new Describe adds exactly one spec and it passes.
- **Fix:** none applied — SCOPE BOUNDARY (the defect is in `internal/testsupport/natstest`, not in anything this plan touches). Logged to `deferred-items.md` and filed as **#4953** per CLAUDE.md's "file discovered work" directive.
- **Substitute verification:** `task test:int -- -run TestWorldModelResilience ./test/integration/resilience/ -ginkgo.focus=IDENT-10` → **exit 0**. (Note the flag must follow the package path; `go test` mis-parses a trailing-position-sensitive `-ginkgo.*` flag placed before it and pulls in the repo-root package.)

### 2. [Rule 2 — Missing critical verification] Proved the new specs actually execute

- **Found during:** Task 2.
- **Issue:** `gotestsum --format pkgname` suppresses a passing test's output, so a green `task test:int -- ./test/integration/world/` cannot by itself distinguish "the new specs passed" from "the new specs never ran". A silently-unregistered spec is a false green.
- **Fix:** temporarily inverted the `exactly 1 envelope` assertion to `2` and confirmed the suite went **red on that exact assertion** (`RED_EXIT=201`, `[FAILED] XX_TEMP_RED_XX EXACTLY ONE character_retired envelope …`), then reverted and re-ran green. The revert is verified: no `XX_TEMP_RED_XX` remains in the tree.

### 3. [Rule 2 — Missing critical coverage] The name-reservation spec drives the production command

- **Issue:** the plan asked to "extend with a spec proving a retired character's name is refused by the creation path". The existing INV-WORLD-6 spec already asserts that — but against a status written by **direct SQL**, because that was the only way to reach `retired` when it was written.
- **Fix:** the new spec retires through `world.Service.RetireCharacter` (authorized by the real engine against the real seeded corpus) and then attempts the reclaim, so the assertion covers the writer Phase 3 actually shipped. Paired with a control (the name is reserved while active) so a green result cannot mean creation is simply broken.

## Threat mitigations applied

| Threat | Disposition | Where |
|---|---|---|
| T-03-08 (Repudiation, IDENT-10 guarantee) | mitigated — this plan IS the mitigation | the two-replica rejection proof and the three-direction atomicity proof turn the guarantee from asserted to demonstrated |
| T-03-09 (Info disclosure, test read-backs via shared pgxpool) | accepted | test-only substrate inside the integration harness; no production path |

No new trust boundary: this plan writes tests and files issues; no production surface changed (`git diff` touches only `test/` and `.planning/`).

## Known Stubs

None. Both new files are complete specs with real assertions against real rows; no `TODO`/`FIXME`/skip/placeholder was introduced. The one temporary assertion inversion used to prove execution (Deviation 2) was reverted and verified absent.

## Verification

| Command | Result |
|---|---|
| `task test:int -- -run TestWorldModelResilience ./test/integration/resilience/ -ginkgo.focus=IDENT-10` (with `HOLOMUSH_RUN_QUARANTINED=1`) | exit 0, 2.74s |
| `task test:int -- -run TestWorldModelResilience ./test/integration/resilience/` (env var unset) | exit 0 via skip |
| `task test:int -- ./test/integration/world/` | exit 0, 2.6s |
| `task test -- ./test/meta/` | exit 0 (quarantine bijection unaffected) |
| `task lint` | exit 0 before each commit |
| `task fmt` | exit 0; output committed |
| full resilience suite, `HOLOMUSH_RUN_QUARANTINED=1` | exit 201 — **4 pre-existing failures**, see Deviation 1 and #4953 |

## Success Criteria

| Criterion | Status |
|---|---|
| ROADMAP success criterion 3: stale `expected_version` rejected with the typed signal on the new command, in the real two-replica harness | met |
| IDENT-10's in-transaction outbox guarantee proven in both directions for retire | met (three directions) |
| Name reservation of retired characters proven at the creation path | met (now through the production command) |
| INV-WORLD-6 defect tracked in GitHub Issues with grounded citations | met (#4952) |

## Requirement status

IDENT-10 is left **unchecked** in REQUIREMENTS.md, consistent with 03-01's disposition: it is claimed by several plans in this phase, and the last plan to close it will flip it honestly. This plan discharges IDENT-10's *verification* obligation for the retire path only.

## Self-Check: PASSED

All three created files exist on disk; both commit hashes (`2fbd9235d`, `8d865830f`) resolve in `git log`; both filed issues (#4952, #4953) resolve via `gh issue list`.
