---
phase: 03-world-character-commands
plan: 02
subsystem: host-composition
tags: [lifecycle, subsystem, jetstream, eventbus, refactor]
status: complete

requires:
  - "audit.createConsumerWithRetry / consumerCreateBackoffs (pre-existing, package-private)"
  - "lifecycle.Subsystem Prepare/Activate/Stop contract (07-09 D-12 Wave B)"
  - "internal/world/setup/relay_subsystem.go as the skeleton model"
provides:
  - "internal/eventbus/consumer: CreateWithRetry + CreateBackoffs (neutral shared package)"
  - "internal/retirement: Subsystem / NewSubsystem / Config"
  - "internal/charactivity: Subsystem / NewSubsystem / NewSubsystemWithStorage / Config"
  - "lifecycle.SubsystemRetirementReactor (id 18) / lifecycle.SubsystemCharacterActivity (id 19)"
  - "productionSubsystemSet.RetirementReactor / .CharacterActivity"
affects:
  - "plan 03-04 (fills retirement.Subsystem Prepare/Activate; consumes consumer.CreateWithRetry)"
  - "plan 03-05 (fills charactivity.Subsystem Prepare/Activate; uses the WithStorage seam)"
  - "any future subsystem addition (the cascade is now a 20-slot shape)"

tech-stack:
  added: []
  patterns:
    - "neutral leaf package for a helper shared across sibling consumers, coding nothing so each caller owns its error code"
    - "constructor-allocates-nothing subsystem skeletons whose ID and DependsOn are final before their bodies exist"
    - "paired NewSubsystem / NewSubsystemWithStorage seam where the production default is the type's zero value"

key-files:
  created:
    - internal/eventbus/consumer/consumer.go
    - internal/eventbus/consumer/consumer_test.go
    - internal/retirement/subsystem.go
    - internal/retirement/subsystem_test.go
    - internal/charactivity/subsystem.go
    - internal/charactivity/subsystem_test.go
  modified:
    - internal/eventbus/audit/projection.go
    - internal/eventbus/audit/plugin_consumer.go
    - internal/eventbus/audit/projection_unit_test.go
    - internal/lifecycle/subsystem.go
    - internal/lifecycle/subsystemid_string.go
    - cmd/holomush/core.go
    - cmd/holomush/core_subsystems_test.go
    - cmd/holomush/core_topo_order_test.go

decisions:
  - "The §0.4-1 interlock fired on the CREATE branch: no neutral package existed, and 02.2 landed its D-55 provenance triple on internal/world/caller.go instead. CreateWithRetry carries a pointer comment naming that location, and stamps nothing."
  - "The relocated helper deliberately codes nothing — a new unit test asserts it returns the create error UNWRAPPED, so neither audit caller's oops Code can be changed by a future edit here."
  - "`task generate` does not exist in this repo; the stringer is regenerated with `go generate ./internal/lifecycle/`, matching how every other generator in the tree is invoked."
  - "The two new lifecycle IDs landed in Task 2 rather than Task 3, because the skeleton packages cannot compile without them. The cascade's RED signal is unaffected — the IDs alone are inert to cmd/holomush."
  - "RESEARCH's claim that a new subsystem is a COMPILE error in core_subsystems_test.go is only half true, and the false half is the dangerous one — corrected in the allStubs doc comment."

metrics:
  duration: "~35 min"
  completed: 2026-08-09

actuals:
  tokens: 21500
  tasks: 3
  commits: 4
---

# Phase 03 Plan 02: Shared Substrate and the Subsystem Cascade Summary

The consumer-create retry helper moves to a neutral package that codes nothing, two inert-but-real subsystem skeletons enter the boot graph with final IDs and dependency edges, and the 18→20 composition cascade lands once so plans 03-04 and 03-05 never touch the lifecycle registry again.

## What was built

### D-46 — the relocation

`internal/eventbus/consumer` is a leaf package holding `CreateWithRetry` and `CreateBackoffs`, moved verbatim from `internal/eventbus/audit/projection.go`. Both audit callers now route through it; both wrappers (`wrapConsumerCreateError` → `AUDIT_CONSUMER_CREATE_FAILED`, `wrapPluginConsumerCreateError` → `AUDIT_PLUGIN_CONSUMER_CREATE_FAILED`) stay in package `audit`, unchanged.

The load-bearing property is that the helper **codes nothing**. A new test asserts `err == permanent` by identity, not `errors.Is` — so a future edit that wraps the return would fail immediately rather than silently demoting both audit callers' codes to an inner position in the chain. That is the single failure mode T-03-05 names.

The five retry-loop tests moved to the new package. What stays in `audit` is the wrap-shape test (`TestNewProjectionWrapSurfacesUnderlyingNATSError`) — the part the relocation must not have changed — now driving `consumer.CreateWithRetry` directly.

### Two skeletons

Both copy `worldsetup.NewOutboxRelaySubsystem`'s shape: the constructor allocates nothing, `Prepare` is idempotent behind a field guard, `Activate` behind a done-channel guard, and `Stop` resets **both** so a retry after teardown rebuilds rather than short-circuits.

| | `internal/retirement` | `internal/charactivity` |
|---|---|---|
| ID | `SubsystemRetirementReactor` (18) | `SubsystemCharacterActivity` (19) |
| DependsOn | Database, EventBus, World, Sessions, Bootstrap | Database, EventBus |
| Bodies filled by | 03-04 | 03-05 |

The `Bootstrap` edge is the one a later reader is most likely to think spurious, so it is declared **now** with the reason at the declaration: the fanout's destination is `StartLocationID()`, which panics before bootstrap's `Prepare`. Declaring it in 03-02 is the whole point — discovering it in 03-04 would mean re-running this cascade.

`charactivity` ships the `NewSubsystemWithStorage` pair. Two tests carry the D-42 hazard explicitly: one asserts `jetstream.FileStorage` **is** the zero value of `StorageType`, the other that the seam forces `MemoryStorage`. Those two together are why the seam is required rather than merely convenient — without it, "the field is unset" and "the bucket is disk-backed" are indistinguishable states, and every test bucket silently lands on disk.

### The cascade

All 13 sites from RESEARCH §3.2 worked mechanically. The IDs were appended at the const-block END and `subsystemid_string.go` regenerated, never hand-edited.

## The observed RED diff and the computed insertion positions

The pinned topological order was **computed from the live graph**, never guessed (research open question 5). Running the test after wiring produced:

```
expected: [... "outbox_relay",                       "plugins", ... "audit_projection",                      "grpc", ...]  (len 18)
actual:   [... "outbox_relay", "character_activity", "plugins", ... "audit_projection", "retirement_reactor", "grpc", ...]  (len 20)
```

| ID | Final index | Why it lands there |
|---|---|---|
| `character_activity` | 11, after `outbox_relay` | Shares `outbox_relay`'s ready tier — both are Database+EventBus leaves. `topoSort` tie-breaks by SubsystemID value: 16 < 19. |
| `retirement_reactor` | 16, after `audit_projection` | Bootstrap-gated, so it only becomes ready in `audit_projection`'s tier. Tie-break 10 < 18. It precedes `grpc` because `grpc` is not ready until `audit_projection` completes. |

Both rationales are recorded inline in the `want` literal so the next reader can re-derive them.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `task generate` does not exist**

- **Found during:** Task 2
- **Issue:** The plan (and the const block's own comment) prescribe `task generate` to regenerate the stringer. `task generate` is not a task in this repo — `task --list-all` offers only `generate:confusables`, `generate:ebnf`, `generate:luabridge`, `generate:schema`, none of which covers `internal/lifecycle`. The invocation exits 200 ("Task does not exist").
- **Fix:** Ran `GOWORK=off go generate ./internal/lifecycle/`, which is exactly how every wrapped generator in `Taskfile.yaml` invokes its own (`go generate ./internal/plugin/`, `go generate ./internal/charname/`, …). `stringer` was already on PATH. The regenerated file was committed; `task fmt` restored the SPDX header stringer strips.
- **Files modified:** `internal/lifecycle/subsystemid_string.go`
- **Commit:** 7110b0099

**2. [Rule 3 - Blocking] The two lifecycle IDs had to land in Task 2, not Task 3**

- **Found during:** Task 2
- **Issue:** The plan orders the skeletons (Task 2) before the cascade (Task 3), but each skeleton's `ID()` returns a constant Task 3 was to introduce. Task 2 cannot compile without them.
- **Fix:** Pulled the plan's Task-3 step (1) — append the IDs, regenerate the stringer — forward into Task 2. This does not weaken Task 3's RED gate: the IDs alone are inert to `cmd/holomush`, whose counts read `productionSubsystems()`, not the const block. Task 3's RED was still observed live.
- **Files modified:** `internal/lifecycle/subsystem.go`, `internal/lifecycle/subsystemid_string.go`
- **Commit:** 7110b0099

**3. [Rule 1 - Bug] `gocritic` importShadow on both audit wrap functions**

- **Found during:** Task 1
- **Issue:** `wrapConsumerCreateError(err error, stream, consumer string)` and `wrapPluginConsumerCreateError(err error, pluginName, consumer string)` each take a parameter named `consumer`, which shadows the newly imported package of the same name. `task lint` failed with two `importShadow` findings.
- **Fix:** Renamed both parameters to `consumerName`. The `oops` field **key** stays `"consumer"` — that string is the observability contract (log-search and dashboard panels filter on it across both call sites) and was not touched.
- **Files modified:** `internal/eventbus/audit/projection.go`, `internal/eventbus/audit/plugin_consumer.go`
- **Commit:** 4d8c1afe2

**4. [Rule 2 - Missing critical coverage] The no-wrap contract had no test**

- **Found during:** Task 1
- **Issue:** D-46's entire safety argument is "the move MUST NOT change either caller's error code," but nothing asserted the mechanism that makes it true. The existing tests use `require.ErrorIs`, which passes just as happily if the helper wraps the error — the exact regression the decision exists to prevent.
- **Fix:** `TestCreateWithRetryReturnsTheUnderlyingErrorUnwrapped` asserts identity (`require.Equal(t, permanent, err)`), not chain membership.
- **Files modified:** `internal/eventbus/consumer/consumer_test.go`
- **Commit:** de46169af

### Corrected repo fact

RESEARCH §3.2 quotes `core_subsystems_test.go:60-62`: *"Adding a subsystem without widening all three occurrences below is a COMPILE error in this package, not a failing length assertion."* Only half of that is true, and **the false half is the dangerous one**.

Widening `productionSubsystemSet` with two new fields while leaving `[18]stubSubsystem` alone compiles cleanly. The fields are simply nil, and the observed RED was a `SIGSEGV` nil dereference inside `TestProductionSubsystemsIncludesCryptoChainVerifier` — a panic mid-suite, not a named assertion failure. The array size only forces a compile error for edits to the array itself.

The doc comment now states the real failure mode. Left uncorrected, the next person to add a subsystem would trust a guarantee that does not hold and read a segfault as an unrelated flake.

## Threat mitigations applied

| Threat | Disposition | Where |
|---|---|---|
| T-03-05 (Tampering, D-46 relocation) | mitigated | verbatim move; both codes pinned by acceptance greps AND by the new identity assertion that the helper wraps nothing; the backoff schedule is single-sourced so it cannot diverge |
| T-03-06 (DoS, boot graph) | mitigated | both dep edge sets declared now and read LIVE by two independent real-constructor graphs; the acyclicity property test and the pinned 20-element topological order both green |
| T-03-07 (Repudiation, inert subsystems) | accepted as planned | `Prepare`/`Activate` are documented no-ops; neither carries domain traffic nor binds any surface, so the Prepare/Activate contract carve-out applies |

## Known Stubs

None in the completed-work sense. The two subsystems' `Prepare`/`Activate` bodies are **intentionally inert**, which is this plan's stated deliverable ("the skeleton is architecture, not a prototype"): the lifecycle contract, `ID()` and `DependsOn()` are final, and 03-04 / 03-05 fill behavior only. Every body carries a doc comment naming its filling plan. A scan of the three new packages for `not implemented` / `TODO` / `FIXME` / `STUB` returns nothing — the RED-phase not-implemented stub was replaced in the GREEN commit.

## Verification

- `task test` — 11300 tests, 4 skipped (quarantined), exit 0
- `task test:int` — 11761 tests, 7 skipped (quarantined/nightly), exit 0
- `task test -- ./internal/eventbus/...` — 875 tests, exit 0
- `task test -- -run 'TestProductionSubsystems' ./cmd/holomush/` — 8 tests, exit 0
- `task lint` — exit 0 before every commit; `task fmt` output committed
- All 12 plan acceptance criteria checked mechanically

One acceptance criterion needs a note: `rg -c 'SubsystemRetirementReactor|SubsystemCharacterActivity' internal/lifecycle/subsystem.go` returns **4**, not the expected 2, because each constant carries a doc comment naming itself. The substantive check — exactly two const declarations, both after `SubsystemCharacterNameBlockList` — holds (lines 49, 56, 60).

## Success Criteria

| Criterion | Status |
|---|---|
| `consumer.CreateWithRetry` shared by both audit callers with unchanged error codes | met |
| 20 subsystems; count/distinctness/real-graph/topo-order tests green; stringer regenerated | met |
| Both skeletons final in ID, deps, and lifecycle shape | met |

## Requirement status

IDENT-04 is left **unchecked** in REQUIREMENTS.md. This plan lands the reactor's registration, not its behavior — nothing consumes `character_retired` until 03-04. Checking it here would repeat the overclaim 03-01 caught and reverted.

## Self-Check: PASSED

All six created files exist on disk; all four commit hashes resolve in `git log`.
