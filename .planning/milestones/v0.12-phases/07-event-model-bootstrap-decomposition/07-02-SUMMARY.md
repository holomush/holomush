---
phase: 07-event-model-bootstrap-decomposition
plan: 02
subsystem: infra
tags: [go, event-vocabulary, package-decomposition, arch-04, arch-05, gateway-boundary]

# Dependency graph
requires:
  - phase: 07-event-model-bootstrap-decomposition
    provides: "07-01: internal/grpcclient leaf package (same-wave file-overlap ordering, not a type dependency)"
provides:
  - "internal/eventvocab — the dependency-free event-type vocabulary leaf (D-05): EventType + 9 wire constants, MaxPayloadSize/ValidatePayload, and 11 JSON payload structs (LocationState*, ExitUpdatePayload, PagePayload, WhisperPayload, WhisperNoticePayload, OOCPayload, PemitPayload, CommandResponsePayload)"
  - "internal/core re-typed against eventvocab.EventType with zero forwarding alias — core.Event.Type and NewEvent's eventType param are eventvocab.EventType"
  - "internal/telnet's arrive/leave render switch and internal/command's BroadcastSystemMessage read the vocabulary through eventvocab, not core"
affects: ["07-03 (ulidgen/cmdparse/sessionlease extraction — telnet/web still import internal/core for ParseCommand/NewULID until then)", "07-04 (permanent gateway-boundary closure gate)", "07-07 (core.Event/eventbus.Event collapse — the vocabulary struct itself, not just the type names, converges there)"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Event-type vocabulary as a dependency-free leaf package importable by both internal/eventbus and the gateway (internal/web, internal/telnet), following the internal/telnet/gamenotice/ 2-file tiny-leaf shape"

key-files:
  created:
    - internal/eventvocab/eventvocab.go
    - internal/eventvocab/eventvocab_test.go
  modified:
    - internal/core/event.go
    - internal/core/engine.go
    - internal/core/engine_end_session.go
    - internal/telnet/gateway_handler.go
    - internal/command/types.go
    - internal/grpc/server.go
    - internal/grpc/location_follow.go
    - internal/plugin/event_emitter.go
    - internal/plugin/hostcap/system_broadcaster.go
    - internal/testsupport/integrationtest/harness.go
    - cmd/holomush/sub_grpc.go

key-decisions:
  - "internal/core/event_payload_size_test.go deleted rather than retargeted — its ValidatePayload/MaxPayloadSize boundary assertions became exact duplicates of eventvocab_test.go; the two not-yet-covered cases (empty/nil payload accept, error-context field assertions) were folded into eventvocab_test.go instead of left orphaned"
  - "internal/core/event_test.go's TestEventType_String and TestEventTypeLocationStateConstantsMatchExpectedValues deleted as exact wire-string duplicates of the new eventvocab_test.go; TestHostEventTypesMatchPluginSDKReExports retargeted (renamed struct field core->host) to assert eventvocab vs pluginsdk agreement instead of core vs pluginsdk"
  - "internal/telnet/gateway_handler_test.go and test/integration/channels/channels_e2e_test.go dropped their internal/core import entirely — every core. reference in each file was a moved vocabulary symbol, so after retyping to eventvocab the import became unused. internal/web/translate_test.go and internal/grpc/*_test.go files kept internal/core alongside the new eventvocab import (core.NewULID/core.Actor/core.Event usages remain until 07-03/07-07)"

patterns-established:
  - "Doc-comment de-stale during a verbatim move: eventvocab.ValidatePayload's doc comment was reworded from the retired 'before EventStore.Append' phrasing to 'before publishing or accepting an event' per the plan's one explicit exception to the verbatim-copy rule"

requirements-completed: [ARCH-05, ARCH-04]

coverage:
  - id: D1
    description: "internal/eventvocab is a true leaf package (zero holomush/internal deps) holding EventType, the 9 wire constants, MaxPayloadSize/ValidatePayload, and the 11 payload structs, moved verbatim from internal/core with pinning tests for every wire string and JSON tag"
    requirement: "ARCH-05"
    verification:
      - kind: unit
        ref: "task test -- ./internal/eventvocab/ (14 tests)"
        status: pass
      - kind: other
        ref: "go list -deps ./internal/eventvocab | rg -c 'holomush/' == 1 (itself only)"
        status: pass
    human_judgment: false
  - id: D2
    description: "internal/core no longer defines EventType/EventTypeArrive/../MaxPayloadSize/ValidatePayload/LocationState*/ExitUpdatePayload/PagePayload/WhisperPayload/WhisperNoticePayload/OOCPayload/PemitPayload/CommandResponsePayload anywhere in the tree — repo-wide grep for core.<symbol> returns 0 matches, with no forwarding type alias left behind"
    requirement: "ARCH-05"
    verification:
      - kind: other
        ref: "rg -c 'core\\.EventType|core\\.MaxPayloadSize|core\\.ValidatePayload|core\\.LocationState|core\\.ExitUpdatePayload|core\\.OOCPayload|core\\.PagePayload|core\\.PemitPayload|core\\.WhisperPayload|core\\.WhisperNoticePayload|core\\.CommandResponsePayload' --type go == 0; rg -c 'type EventType' internal/core/ == 0"
        status: pass
      - kind: unit
        ref: "task test (10249 tests, 4 pre-existing skips)"
        status: pass
      - kind: integration
        ref: "task test:int (10675 tests, 7 pre-existing quarantined skips)"
        status: pass
      - kind: other
        ref: "task build; task lint (0 issues)"
        status: pass
    human_judgment: false
  - id: D3
    description: "internal/telnet's arrive/leave render switch reads eventvocab.EventTypeArrive/EventTypeLeave; the gateway's (internal/telnet, internal/web) vocabulary reads go entirely through eventvocab; internal/command retains its D-01 exclusion from internal/eventbus after gaining the eventvocab import"
    requirement: "ARCH-04"
    verification:
      - kind: other
        ref: "rg -n 'eventvocab.EventTypeArrive' internal/telnet/gateway_handler.go (1 hit); rg -c 'core\\.EventType|core\\.LocationState|...' internal/telnet/ internal/web/ == 0; go list -deps ./internal/command | rg -c 'holomush/internal/eventbus$' == 0"
        status: pass
    human_judgment: false

duration: 33min
completed: 2026-07-17
status: complete
---

# Phase 07 Plan 02: Create internal/eventvocab and Repoint All Consumers Summary

**Event-type vocabulary (EventType + 9 wire constants, MaxPayloadSize/ValidatePayload, 11 payload structs) extracted verbatim into a new dependency-free leaf package internal/eventvocab, with internal/core and 39 consumers (9 production + 30 test) repointed; no core.EventType\*/core.\*Payload reference survives anywhere in the tree.**

## Performance

- **Duration:** ~33 min
- **Started:** 2026-07-17T22:06:25Z
- **Completed:** 2026-07-17T22:39:07Z
- **Tasks:** 2
- **Files modified:** 40 (2 created, 37 modified, 1 deleted)

## Accomplishments
- Created `internal/eventvocab`, a true leaf package (zero `holomush/internal` deps) holding `EventType`, the 9 wire constants (`arrive`, `leave`, `system`, `move`, `command_response`, `command_error`, `location_state`, `exit_update`, `session_ended`), `MaxPayloadSize`/`ValidatePayload`, and 11 JSON payload structs — copied verbatim (identifiers, wire strings, JSON tags, doc comments) from `internal/core/event.go` and `internal/core/engine.go`.
- `eventvocab_test.go` pins every wire string via a table-driven test, the 64 KiB boundary (accept at exactly `MaxPayloadSize`, reject at `+1`, plus the empty/nil and well-below-limit accept cases folded in from the deleted `event_payload_size_test.go`), and JSON round-trips for `LocationStatePayload` and `CommandResponsePayload` asserting exact tag names.
- Deleted the moved symbols from `internal/core/event.go` and `CommandResponsePayload` from `internal/core/engine.go`; `internal/core` now imports `internal/eventvocab` and re-types `core.Event.Type` and `NewEvent`'s `eventType` parameter as `eventvocab.EventType` — no forwarding type alias left behind.
- Repointed 9 production files: `internal/telnet/gateway_handler.go` (arrive/leave render switch), `internal/command/types.go` (`BroadcastSystemMessage`), `internal/grpc/server.go` + `internal/grpc/location_follow.go` (command-response emit, move-event guard, location_state synthesis), `internal/plugin/event_emitter.go` (`ValidatePayload`), `internal/plugin/hostcap/system_broadcaster.go`, `internal/testsupport/integrationtest/harness.go`, `cmd/holomush/sub_grpc.go` (history read-back translation).
- Repointed 30 test files (re-ran the plan's census command before editing — the live consumer set matched the plan's `files_modified` list exactly, no drift, no missed files).
- Reworded `ValidatePayload`'s doc comment during the move (the plan's one explicit non-verbatim exception): the stale "before `EventStore.Append`" reference (an API already retired by the JetStream cutover) now reads "before publishing or accepting an event."

## Task Commits

1. **Task 1: Create the internal/eventvocab leaf** - `50617ccfb` (feat)
2. **Task 2: Delete the moved symbols from internal/core and repoint all 34 consumers** - `18598d143` (feat)

## Files Created/Modified
- `internal/eventvocab/eventvocab.go` - New leaf package: `EventType` + 9 constants, `MaxPayloadSize`/`ValidatePayload`, 11 payload structs (verbatim move)
- `internal/eventvocab/eventvocab_test.go` - Table-driven wire-string pins, boundary tests (including cases folded in from the deleted core test), JSON tag round-trips
- `internal/core/event.go` - Deleted the moved symbols; `Event.Type` and `NewEvent` re-typed to `eventvocab.EventType`; imports `internal/eventvocab`
- `internal/core/engine.go`, `internal/core/engine_end_session.go` - `CommandResponsePayload` removed; `EventTypeArrive`/`EventTypeLeave`/`EventTypeSessionEnded` references re-qualified to `eventvocab.*`
- `internal/core/event_test.go`, `internal/core/event_constructor_test.go`, `internal/core/engine_test.go`, `internal/core/engine_end_session_test.go` - Retargeted to `eventvocab.*`; two now-duplicate wire-string tests deleted from `event_test.go`, one retargeted
- `internal/core/event_payload_size_test.go` - **Deleted** (see Deviations)
- `internal/telnet/gateway_handler.go`, `internal/telnet/gateway_handler_test.go` - Render switch + test fixtures repointed; test file's now-unused `internal/core` import dropped
- `internal/command/types.go`, `internal/command/types_test.go`, `internal/command/handlers/shutdown_test.go` - `BroadcastSystemMessage`'s `EventTypeSystem` re-qualified
- `internal/grpc/server.go`, `internal/grpc/location_follow.go` + their test files (`dispatcher_test.go`, `auth_handlers_test.go`, `location_follow_test.go`, `pipeline_rendering_test.go`, `subscribe_loop_test.go`) - command-response, move-guard, and location_state vocabulary repointed
- `internal/plugin/event_emitter.go`, `internal/plugin/hostcap/system_broadcaster.go` + their test files (`event_emitter_test.go`, `event_emitter_round3_test.go`, `manager_routing_test.go`, `subscriber_test.go`, `hostcap/system_broadcaster_test.go`, `hostfunc/stdlib_focus_test.go`, `goplugin/host_test.go`) - `ValidatePayload`/`EventTypeSystem`/`EventTypeArrive` repointed
- `internal/testsupport/integrationtest/harness.go`, `cmd/holomush/sub_grpc.go`, `cmd/holomush/sub_grpc_adapters_test.go` - history read-back `busEventToCoreEvent`/adapter translations repointed
- `internal/auth/auth_service_test.go`, `internal/web/translate_test.go` - `EventTypeSessionEnded`/payload-struct fixtures repointed
- `test/integration/channels/channels_e2e_test.go`, `test/integration/pluginparity/session_admin_broadcast_test.go`, `test/integration/scenes/{focus_routed_input,observer_emit_denial,scene_info_read_access}_test.go` - integration-tagged fixtures repointed (verified via `task test:int`)

## Decisions Made
- `internal/core/event_payload_size_test.go` deleted rather than retargeted: retargeting would have produced an exact duplicate of `eventvocab_test.go`'s boundary tests under a different package. Its two not-yet-covered cases (empty/nil payload, well-below-limit accept) and the `errutil.AssertErrorContext` assertions were folded into `eventvocab_test.go` first, so no coverage was lost.
- `internal/core/event_test.go`'s `TestEventType_String` and `TestEventTypeLocationStateConstantsMatchExpectedValues` deleted (exact wire-string duplicates); `TestHostEventTypesMatchPluginSDKReExports` retargeted in place (struct field renamed `core`→`host`) since it tests eventvocab-vs-pluginsdk agreement, a genuinely distinct assertion from `eventvocab_test.go`'s own pins.
- Two test files (`internal/telnet/gateway_handler_test.go`, `test/integration/channels/channels_e2e_test.go`) had their `internal/core` import dropped entirely once every reference in the file resolved to a moved vocabulary symbol — confirmed via `rg -c '\bcore\.'` returning zero before removing the import line.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `internal/core/event_test.go`'s duplicate wire-string tests would have failed to compile after the move**
- **Found during:** Task 2, editing `internal/core/event_test.go` (package `core`, not `core_test`)
- **Issue:** `TestEventType_String` and `TestEventTypeLocationStateConstantsMatchExpectedValues` referenced the bare (unqualified) `EventType`/`EventTypeArrive`/etc. identifiers that no longer exist in package `core`. Retargeting them with an `eventvocab.` prefix would produce byte-identical assertions to the new `eventvocab_test.go`.
- **Fix:** Deleted both tests (their coverage is fully subsumed by `eventvocab_test.go`); retargeted the remaining `TestHostEventTypesMatchPluginSDKReExports` to `eventvocab.*`.
- **Files modified:** `internal/core/event_test.go`
- **Verification:** `task test -- ./internal/core/` passes; `eventvocab_test.go` still asserts every wire string independently.
- **Committed in:** `18598d143` (Task 2 commit)

**2. [Rule 1 - Bug] `internal/core/event_payload_size_test.go` would have been a complete duplicate suite**
- **Found during:** Task 2, auditing `internal/core` test files referencing the moved `ValidatePayload`/`MaxPayloadSize`
- **Issue:** Every test in this file (`package core_test`) called `core.ValidatePayload`/`core.MaxPayloadSize`, both now undefined. A mechanical `core.`→`eventvocab.` retarget would produce an entire file duplicating `eventvocab_test.go`'s boundary assertions verbatim.
- **Fix:** Deleted the file; ported its two not-yet-covered cases (accept well-below-limit, accept nil/empty payload) plus its `errutil.AssertErrorContext` context-field assertions into `eventvocab_test.go`'s reject-over-limit test, so no assertion was lost.
- **Files modified:** `internal/core/event_payload_size_test.go` (deleted), `internal/eventvocab/eventvocab_test.go` (extended)
- **Verification:** `task test -- ./internal/eventvocab/` — all boundary/empty/error-context cases pass; `git diff --diff-filter=D` confirms the deletion is the only one in this plan and is intentional.
- **Committed in:** `18598d143` (Task 2 commit)

**3. [Rule 3 - Blocking] Two test files' `internal/core` import became unused after retargeting**
- **Found during:** Task 2, post-edit `rg -c '\bcore\.'` audits of `internal/telnet/gateway_handler_test.go` and `test/integration/channels/channels_e2e_test.go`
- **Issue:** Every `core.` reference in each file was a moved vocabulary symbol (`core.CommandResponsePayload`, `core.EventTypeCommandResponse`/`CommandError`/`CommandResponse`); after retargeting to `eventvocab.*` the `internal/core` import had zero remaining uses, which is a compile error (unused import).
- **Fix:** Removed the now-unused `internal/core` import from both files.
- **Files modified:** `internal/telnet/gateway_handler_test.go`, `test/integration/channels/channels_e2e_test.go`
- **Verification:** `task build:all`, `task test`, `task test:int` all pass with these files compiling clean.
- **Committed in:** `18598d143` (Task 2 commit)

**4. [Verification-detail, non-blocking] Task 1's `rg -c 'EventType [A-Za-z]* = "'` acceptance-criterion regex never matches gofumpt-formatted output**
- **Found during:** Task 1 acceptance-criteria verification
- **Issue:** The plan's literal criterion `rg -c 'EventType [A-Za-z]* = "' internal/eventvocab/eventvocab.go` expects `9` but returns `0` against both the new file and the original (pre-move) `internal/core/event.go` — the regex requires two spaces around `=` (one after the literal `EventType` token, one before `= "`), but gofumpt's actual alignment produces exactly one space around `=` on every const line (`EventTypeArrive EventType = "arrive"`). The criterion cannot pass as written against real gofumpt output, on either side of the move.
- **Resolution:** Verified the underlying intent directly: `rg -n '^\tEventType[A-Za-z]+ EventType = "' internal/eventvocab/eventvocab.go` plus a manual listing confirms all 9 constants are present with correct wire strings (also independently proven by `eventvocab_test.go`'s `TestEventTypeArriveIsTheArriveWireString` table, which asserts all 9). No code change; this is purely a verification-command imprecision in the plan text, in the same family as 07-01's documented unanchored-regex deviation.
- **Files modified:** None

---

**Total deviations:** 3 auto-fixed (2 Rule 1 bugs, 1 Rule 3 blocking), 1 non-blocking verification-command note.
**Impact on plan:** All three auto-fixes were necessary for the literal "repoint every consumer" instruction to compile and avoid dead duplicate test coverage. No scope creep: fixes are confined to test-file wiring and coverage consolidation, no production behavior changed beyond the planned type relocation. The verification-detail note requires no code change and does not affect the plan's actual (substantively verified) exit criteria.

## Issues Encountered
None beyond the deviations documented above.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `internal/eventvocab` is now the single home for event-type vocabulary, available to both `internal/eventbus`'s consumers and the gateway.
- 07-03 (ulidgen/cmdparse/sessionlease extraction) can now proceed: `internal/telnet`/`internal/web` still import `internal/core` for `ParseCommand`/`NewULID` only — the vocabulary dependency this plan targeted is fully closed, so 07-03's `go list -deps` closure criteria have one less confound.
- 07-07 (the `core.Event`/`eventbus.Event` collapse) inherits a smaller `internal/core` surface: `Event`/`NewEvent`/`EventAppender` remain the only event-shaped symbols left in `internal/core`, already re-typed against the leaf vocabulary.
- No blockers for downstream phase-07 plans.

---
*Phase: 07-event-model-bootstrap-decomposition*
*Completed: 2026-07-17*

## Self-Check: PASSED

- FOUND: internal/eventvocab/eventvocab.go
- FOUND: internal/eventvocab/eventvocab_test.go
- FOUND: internal/core/event.go
- FOUND: .planning/phases/07-event-model-bootstrap-decomposition/07-02-SUMMARY.md
- FOUND: 50617ccfb (git log --oneline --all)
- FOUND: 18598d143 (git log --oneline --all)
