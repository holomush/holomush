---
phase: 07-event-model-bootstrap-decomposition
plan: 07
subsystem: infra
tags: [go, eventbus, plugin, grpc, event-model-collapse, arch-04]

# Dependency graph
requires:
  - phase: 07-04
    provides: "cmd/holomush/gateway_closure_test.go transitive-closure gate; ARCH-05 gateway-boundary invariant asserting before this plan's internal/core and cmd/holomush edits"
  - phase: 07-06
    provides: "internal/sysbroadcast.Broadcaster and command.SystemBroadcaster; internal/command and internal/plugin/hostcap hold no remaining core.EventAppender reference"
provides:
  - "Single Event representation: eventbus.Event. core.Event, core.NewEvent, and core.EventAppender are deleted from the tree."
  - "plugins.HistoryReader.ReplayTail and hostfunc.HistoryReader.ReplayTail both return []eventbus.Event (carrying Seq), unblocking 07-08's cursor fix"
  - "CoreServer.publisher (eventbus.Publisher) + WithEventPublisher(pub, gameID) replacing CoreServer.eventStore (core.EventAppender) + WithEventStore; emitCommandResponse publishes via eventbus.NewEvent through the wrapped RenderingPublisher"
  - "Exactly one core.Actor -> eventbus.Actor bridge survives (plugins.coreActorToEventbusActor in event_emitter.go); CoreServer's system actor is a direct typed literal, not routed through it"
  - "Two zero-aware actorIDString(ulid.ULID) string helpers (hostfunc, hostcap) preserving the deleted busEventToCoreEvent's zero->\"\" actor-id mapping for plugins"
  - "CLAUDE.md and .claude/rules/event-conventions.md mandate eventbus.NewEvent() by name; no always-loaded rule references a deleted symbol"
affects: ["07-08 (cursor Seq fix: ReplayTail now carries Seq end-to-end; the hostcap/servers.go 'core.Event without Seq' comment and encodeHostEventCursor's Seq:0 are 07-08's to rewrite)"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Test-only fake eventbus.Publisher (internal/grpc's testEventStore) replacing a retired core.EventAppender-backed in-memory store: records events per game-relative stream name (stripping the 'events.main.' qualification prefix, a no-op TrimPrefix when the caller already published a relative subject), with a Replay(ctx, stream, afterID, limit) test-inspection method mirroring the old fixture's shape"
    - "Reusing an existing CoreServerOption's target field from a new option (WithEventPublisher sets the pre-existing s.gameID field, the same GameIDProvider WithGameID sets) instead of adding a same-named duplicate field — avoids a compile collision and keeps D-02's 'one game-id source' property"

key-files:
  created: []
  modified:
    - internal/core/event.go
    - internal/grpc/server.go
    - internal/grpc/test_helpers_test.go
    - internal/grpc/dispatcher_test.go
    - internal/grpc/pipeline_rendering_test.go
    - internal/grpc/auth_handlers_test.go
    - internal/grpc/server_helpers_test.go
    - internal/grpc/location_follow.go
    - internal/plugin/host.go
    - internal/plugin/hostfunc/stdlib_focus.go
    - internal/plugin/hostfunc/streamauth.go
    - internal/plugin/hostcap/servers.go
    - internal/plugin/goplugin/host_service_test.go
    - internal/testsupport/integrationtest/harness.go
    - cmd/holomush/sub_grpc.go
    - CLAUDE.md
    - .claude/rules/event-conventions.md

key-decisions:
  - "Combined Task 1 (ReplayTail retype) and Task 2 (core.Event deletion) into one commit — they were executed and verified together as a single build/test/test:int/lint-green unit before either was committed; splitting them post-hoc into separate commits risked reconstructing an inconsistent intermediate state. Documented as a process deviation, not a scope change."
  - "WithEventPublisher(pub, gameID) sets CoreServer's existing s.gameID field (type GameIDProvider = func() string) rather than adding a second field of the same name — the plan's literal 'two fields: publisher and gameID' instruction would have collided with the pre-existing WithGameID-backed field; reusing it is the correct application of D-02 (one game-id source for the whole host)"
  - "internal/plugin/event_emitter.go was NOT edited — the plan's crypto-reviewer trigger assumed exporting the survivor actor bridge; round-6 re-scope settled on CoreServer constructing its own typed eventbus.Actor{Kind: ActorKindSystem, ID: core.SystemActorULID} literal instead, so the plugin-private bridge and its file are untouched. crypto-reviewer is not triggered by this plan."
  - "Left internal/plugin/hostcap/servers.go's 'plugins.HistoryReader.ReplayTail interface returns core.Event without Seq' comment untouched per Task 1's explicit instruction (07-08 rewrites it alongside the cursor Seq fix) — this is the one sanctioned exception to the repo-wide 'zero core.Event references' grep criterion"
  - "Filed gh issue #4820 for the out-of-scope PROJECT.md / ARCHITECTURE.md event-sourcing framing drift (MODEL-02) rather than fixing it — Task 3 explicitly scoped rule amendments to code-symbol references, not the higher-level world-state-model narrative"

patterns-established:
  - "When a WithX(...) option needs to set a field another pre-existing option already targets, reuse that field rather than introducing a compile-colliding duplicate — check for an existing same-typed field before adding one"

requirements-completed: [ARCH-04]

coverage:
  - id: D1
    description: "core.Event, core.NewEvent, and core.EventAppender no longer exist anywhere in the tree; eventbus.Event is the single Event representation"
    requirement: "ARCH-04"
    verification:
      - kind: other
        ref: "rg -c 'core\\.Event\\b|core\\.NewEvent|core\\.EventAppender' --type go . -> 1 hit (internal/plugin/hostcap/servers.go, a sanctioned Task-1-instructed exception for 07-08)"
        status: pass
      - kind: unit
        ref: "task test (10246 tests)"
        status: pass
    human_judgment: false
  - id: D2
    description: "plugins.HistoryReader.ReplayTail and hostfunc.HistoryReader.ReplayTail both return []eventbus.Event across all 4 production implementations and 4 test fakes; no plugin-facing seq field was added to the proto"
    requirement: "ARCH-04"
    verification:
      - kind: unit
        ref: "internal/plugin/hostfunc, internal/plugin/hostcap, internal/plugin/goplugin, cmd/holomush (all packages touched by the retype)"
        status: pass
      - kind: other
        ref: "rg -c 'seq' api/proto/holomush/plugin/host/v1/stream.proto -i -> 0"
        status: pass
    human_judgment: false
  - id: D3
    description: "Exactly one core.Actor -> eventbus.Actor bridge survives (plugins.coreActorToEventbusActor), private to the plugin emit path with its non-ULID rejection intact; CoreServer's system actor is a direct typed literal"
    requirement: "ARCH-04"
    verification:
      - kind: other
        ref: "rg -c 'func coreActorToEventbusActor' internal/plugin/event_emitter.go -> 1; rg -c 'coreActorToEventbusActor' internal/grpc/ -> 0; rg -c 'eventbus.ActorKindSystem' internal/grpc/server.go -> 1"
        status: pass
      - kind: integration
        ref: "test/integration/crypto/ (non-ULID rejection), test/integration/pluginparity/ (Lua/binary emit parity) via task test:int"
        status: pass
    human_judgment: false
  - id: D4
    description: "CoreServer publishes command_response/command_error events through the wrapped (RenderingPublisher) publisher via WithEventPublisher/eventbus.NewEvent, qualified by exact literal; a nil publisher stays a silent no-op"
    requirement: "ARCH-04"
    verification:
      - kind: unit
        ref: "internal/grpc TestEmitCommandResponseNilPublisherIsSilentNoOp, TestEmitCommandResponsePublishesOnExactQualifiedSubject"
        status: pass
      - kind: other
        ref: "rg -c 'WithEventStore' --type go . -> 0; rg -c 'eventbus\\.Event\\{' internal/grpc/server.go -> 0 (canonical constructor only)"
        status: pass
    human_judgment: false
  - id: D5
    description: "CLAUDE.md and .claude/rules/event-conventions.md mandate eventbus.NewEvent() by name and no longer reference core.NewEvent/core.EventAppender/core.Engine/MemoryEventStore; identity-vs-ordering (Nats-Msg-Id) preserved"
    requirement: "ARCH-04"
    verification:
      - kind: other
        ref: "rg -c 'core\\.NewEvent|core\\.EventAppender|core\\.Engine|MemoryEventStore' CLAUDE.md .claude/rules/ -> 0; rg -c 'eventbus.NewEvent' CLAUDE.md .claude/rules/event-conventions.md -> 2; task lint"
        status: pass
    human_judgment: false

duration: 46min
completed: 2026-07-17
status: complete
---

# Phase 07 Plan 07: Event Model Collapse Summary

**Deleted `core.Event`/`core.NewEvent`/`core.EventAppender`, retyped the plugin history read path to `[]eventbus.Event`, collapsed three duplicate actor bridges to one, and amended the repo's always-loaded rules to stop mandating the deleted symbols — `eventbus.Event` is now the single Event representation (ARCH-04).**

## Performance

- **Duration:** 46 min
- **Started:** 2026-07-17T20:36Z (approx, following 07-06 completion)
- **Completed:** 2026-07-17T21:22Z
- **Tasks:** 3 completed
- **Files modified:** 35 (28 in the combined Task 1+2 commit, 7 in the Task 3 docs commit); 3 files deleted (`internal/core/store.go`, `internal/core/event_constructor_test.go`, `internal/core/coretest/store_memory.go`)

## Accomplishments

- Retyped both `HistoryReader.ReplayTail` interfaces (`plugins` and `hostfunc` packages, coupled by structural typing via the Lua adapter) from `[]core.Event` to `[]eventbus.Event` across all 4 production implementations and 4 test fakes, deleting `busEventToCoreEvent` (the hop that discarded `Seq`) so `Seq` now survives to the cursor encoder for 07-08.
- Deleted `core.Event`, `core.NewEvent`, `core.EventAppender`, and `coretest.MemoryEventStore` outright; `eventbus.Event` is the tree's only Event type.
- Collapsed the three hand-copied `core.Actor -> eventbus.Actor` bridges (`coreToBusActor` in `cmd/holomush`, `harnessCoreToBusActor` in the integration test harness, and the busEventAppenderAdapter duplicate) down to the single pre-existing bridge in `internal/plugin/event_emitter.go`, which carries the non-ULID rejection `test/integration/crypto/` depends on.
- Replaced `CoreServer.eventStore`/`WithEventStore` with `CoreServer.publisher`/`WithEventPublisher`, wiring `emitCommandResponse` to qualify via the existing `toSubject` helper and publish through `eventbus.NewEvent` over the wrapped `RenderingPublisher` (so `command_response`/`command_error` events keep landing in `events_audit`).
- Added two tiny zero-aware `actorIDString(ulid.ULID) string` helpers (in `hostfunc` and `hostcap`) that preserve the deleted `busEventToCoreEvent`'s zero-ULID → `""` mapping for the plugin-facing Lua table and proto converter, each with a zero/nonzero test.
- Amended `CLAUDE.md` § ULID Generation and `.claude/rules/event-conventions.md` § Event construction to mandate `eventbus.NewEvent()` by name, and repointed five stale doc comments across `internal/idgen`, `internal/eventbus`, `plugins/core-scenes`, `internal/testsupport/natstest`, and `.claude/rules/event-interfaces.md`.

## Task Commits

1. **Tasks 1+2 (combined): retype ReplayTail to `[]eventbus.Event`; delete `core.Event`/`NewEvent`/`EventAppender`; collapse actor bridges; rewire `CoreServer` publication** - `06a783358` (feat)
2. **Task 3: amend repo rules referencing the deleted symbols** - `b52438827` (docs)

_Note: Tasks 1 and 2 were combined into a single commit — see Deviations below._

## Files Created/Modified

- `internal/core/event.go` — `Event` struct + `NewEvent` deleted; `Actor`/`ActorKind`/sentinel ULIDs/`SystemBroadcastSubject` survive unchanged
- `internal/core/store.go`, `internal/core/coretest/store_memory.go`, `internal/core/event_constructor_test.go` — deleted (EventAppender, MemoryEventStore, and the constructor's own test)
- `internal/grpc/server.go` — `eventStore core.EventAppender` field → `publisher eventbus.Publisher`; `WithEventStore` → `WithEventPublisher`; `emitCommandResponse` rewritten to qualify + publish via `eventbus.NewEvent`
- `internal/grpc/test_helpers_test.go` — new `testEventStore` fake `eventbus.Publisher` (replacing `coretest.MemoryEventStore` + the retired `eventbusToCoreAppender` reverse-adapter) backing `newTestPresenceEmitter`/`newHandleCommandServer`; `mockEventStore` retyped to `eventbus.Publisher`
- `internal/grpc/dispatcher_test.go`, `pipeline_rendering_test.go`, `auth_handlers_test.go`, `server_helpers_test.go`, `location_follow.go`/`location_follow_test.go` — retyped to `eventbus.Event`/`eventbus.Actor`/`eventbus.Subject`/`eventbus.Type`; two new tests added (`TestEmitCommandResponseNilPublisherIsSilentNoOp`, `TestEmitCommandResponsePublishesOnExactQualifiedSubject`)
- `internal/plugin/host.go`, `internal/plugin/hostfunc/stdlib_focus.go`, `internal/plugin/hostfunc/streamauth.go`, `internal/plugin/hostcap/servers.go` (+ their test files) — `HistoryReader.ReplayTail` retyped; `coreEventToProto` renamed `eventbusEventToProto`; `actorIDString` helpers added
- `internal/testsupport/integrationtest/harness.go`, `cmd/holomush/sub_grpc.go` (+ their test files) — production wiring migrated to `WithEventPublisher`; dead adapters deleted
- `CLAUDE.md`, `.claude/rules/event-conventions.md`, `.claude/rules/event-interfaces.md`, `internal/idgen/id.go`, `internal/eventbus/types.go`, `plugins/core-scenes/idle_scheduler.go`, `internal/testsupport/natstest/nats.go` — rule/doc-comment amendments

## Decisions Made

- **Combined Task 1+2 commit** (see Task Commits note): the two tasks' edits were interdependent in practice — Task 2's core-package deletions could not be independently build-verified without Task 1's `ReplayTail` retype already in place across `cmd/holomush` and the integration harness — so they were executed and verified together before either was committed.
- **Reused the existing `CoreServer.gameID` field** for `WithEventPublisher` rather than adding a second `gameID` field as the plan's literal wording suggested (see key-decisions in frontmatter) — the pre-existing field is already wired to the same `s.cfg.EventBus.GameID` source in production, so this is a correctness fix (a literal second field would not compile) applying D-02's own "one game-id source" principle.
- **`internal/plugin/event_emitter.go` left untouched** — no export of the survivor bridge was needed once CoreServer's system actor was built as a direct typed literal, so the crypto-reviewer gate the plan anticipated does not fire.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Fixed additional core.Event/EventAppender/actor-bridge call sites not listed in Task 1/2's `<files>` tags**
- **Found during:** Tasks 1 and 2
- **Issue:** Retyping `ReplayTail` and deleting `core.Event`/`EventAppender` broke compilation in several files not enumerated in the tasks' own `<files>` lists (though present in the plan's frontmatter `files_modified`): `internal/grpc/location_follow_test.go`, `internal/plugin/hostcap/servers_test.go`, `internal/presence/emitter.go`/`emitter_test.go` (stale comments only), `test/integration/pluginparity/session_admin_broadcast_test.go` (stale comment only).
- **Fix:** Updated each to the new types/wiring or repointed the stale comment.
- **Files modified:** listed above.
- **Verification:** `task build`, `task test`, `task test:int`, `task lint` all green.
- **Committed in:** `06a783358`

**2. [Rule 1 - Bug] Fixed 3 test assertions expecting the un-qualified subject on presence-emitted session_ended events**
- **Found during:** Task 2 (post-edit `task test` run)
- **Issue:** `TestQuitPathAppendsSessionEndedOnCharacterStream`, `TestGuestDisconnectEmitsSessionEndedOnCharacterStream`, and `TestAdminBootEmitsSessionEndedWithKickedCause` asserted `sessionEnded.Subject == "character.<id>"`, but `presence.Emitter` (established in 07-05) always publishes a fully-qualified subject (`events.main.character.<id>`); the retired `eventbusToCoreAppender` test shim used to silently re-strip that qualification back to the relative form before storing, masking the real wire shape.
- **Fix:** Updated the three assertions to expect the qualified subject, matching actual production behavior.
- **Files modified:** `internal/grpc/dispatcher_test.go`
- **Verification:** `task test` — all 3 tests pass; full suite green (10246 tests).
- **Committed in:** `06a783358`

**3. [Rule 2 - Missing Critical] Added tests for the nil-publisher no-op and exact-qualified-subject contracts**
- **Found during:** Task 2 (self-review against the plan's own acceptance criteria)
- **Issue:** The plan's acceptance criteria explicitly required a test proving a nil publisher is a silent no-op and a test asserting the published subject by exact literal (not a recomputed `eventbus.Qualify` call); neither existed.
- **Fix:** Added `TestEmitCommandResponseNilPublisherIsSilentNoOp` and `TestEmitCommandResponsePublishesOnExactQualifiedSubject`.
- **Files modified:** `internal/grpc/server_helpers_test.go`
- **Verification:** Both tests pass.
- **Committed in:** `06a783358`

**4. [Rule 1 - Bug] Fixed 2 gocritic rangeValCopy findings from the larger eventbus.Event struct**
- **Found during:** Task 1 (`task lint:go`)
- **Issue:** `for _, e := range events` loops in `internal/plugin/hostcap/servers.go` and `internal/plugin/hostfunc/stdlib_focus.go` copy 168 bytes/iteration now that the loop variable is `eventbus.Event` (larger than the former `core.Event`), tripping gocritic.
- **Fix:** Converted both to index-based iteration.
- **Files modified:** `internal/plugin/hostcap/servers.go`, `internal/plugin/hostfunc/stdlib_focus.go`
- **Verification:** `task lint:go` — 0 issues.
- **Committed in:** `06a783358`

---

**Total deviations:** 4 auto-fixed (2 Rule 1 bug, 1 Rule 2 missing-critical, 1 Rule 3 blocking)
**Impact on plan:** All auto-fixes were necessary for correctness (compile, accurate test assertions, lint) or completeness (missing acceptance-criteria test coverage). No scope creep beyond what the plan's own frontmatter `files_modified` list already anticipated ("blast radius under-declared" section).

## Issues Encountered

- **Transient Postgres testcontainer flake** during the first `task test:int` run for Task 1 (23 Ginkgo specs in `plugins/core-scenes` panicked mid-suite when the container entered PostgreSQL recovery mode). Confirmed unrelated to this plan's changes (that package touches none of the files this plan modifies) and transient — a clean re-run passed all 10667 tests. No code change made in response, per the documented "diagnose before quarantining" reflex.
- **Executor process deviation:** Tasks 1 and 2 were executed and fully verified together before either was committed (see Decisions Made). This is a deviation from strict one-commit-per-task, not from the plan's technical content — both tasks' acceptance criteria were independently re-verified via the greps listed in the `coverage` block above before committing.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `eventbus.Event` is now the tree's single Event representation; 07-08 (the cursor Seq fix) can proceed directly against `ReplayTail`'s `[]eventbus.Event` return without any further type-barrier work.
- `internal/plugin/hostcap/servers.go`'s `encodeHostEventCursor` and its "core.Event without Seq" comment are the one deliberately-left loose end for 07-08 to rewrite.
- GitHub issue #4820 tracks the still-open PROJECT.md/ARCHITECTURE.md event-sourcing framing correction (MODEL-02) — unrelated to 07-08, no blocker.

---
*Phase: 07-event-model-bootstrap-decomposition*
*Completed: 2026-07-17*

## Self-Check: PASSED

All claimed created/modified files verified present; all claimed deletions verified absent; both commit hashes (`06a783358`, `b52438827`) verified in `git log`.
