---
phase: 07-event-model-bootstrap-decomposition
plan: 03
subsystem: infra
tags: [go, gateway-boundary, dependency-graph, ulid, refactor]

# Dependency graph
requires:
  - phase: 07-01
    provides: internal/grpcclient leaf extraction that first closed the gateway's internal/grpc leak
  - phase: 07-02
    provides: internal/eventvocab leaf extraction (event-type vocabulary), leaving core.NewULID/core.ParseCommand/session.DefaultLeaseRefreshInterval as the three remaining gateway-needed symbols this plan moves
provides:
  - internal/ulidgen (dependency-free ULID generator leaf; core.NewULID/core.ParseULID retained as documented forwarders)
  - internal/cmdparse (dependency-free command-grammar leaf; core.ParseCommand deleted, no forwarder — zero other consumers)
  - internal/sessionlease (dependency-free lease-refresh-interval leaf; session.DefaultLeaseRefreshInterval retained as a documented forwarder)
  - internal/telnet and internal/web now import NEITHER internal/core NOR internal/session (production or test code) — the gateway boundary invariant's remaining leaks (D-16) are closed
affects: [07-04]

# Tech tracking
tech-stack:
  added: []
  patterns: ["verbatim-move + documented-forwarder for symbols with existing external callers (core.NewULID, session.DefaultLeaseRefreshInterval); verbatim-move + delete-no-forwarder for symbols with a single external consumer (core.ParseCommand)"]

key-files:
  created:
    - internal/ulidgen/ulidgen.go
    - internal/ulidgen/ulidgen_test.go
    - internal/ulidgen/ulidgen_internal_test.go
    - internal/cmdparse/cmdparse.go
    - internal/cmdparse/cmdparse_test.go
    - internal/sessionlease/sessionlease.go
    - internal/sessionlease/sessionlease_test.go
  modified:
    - internal/core/ulid.go
    - internal/core/ulid_test.go
    - internal/session/reaper.go
    - internal/telnet/gateway_handler.go
    - internal/telnet/limits.go
    - internal/web/handler.go
    - internal/web/translate_test.go
    - CLAUDE.md

key-decisions:
  - "Kept core.NewULID/core.ParseULID and session.DefaultLeaseRefreshInterval as documented forwarders (D-03: no dependency inversion) rather than rewriting their ~68 core-side call sites; the gateway calls the leaf packages directly instead."
  - "core.ParseCommand had exactly one external consumer (internal/telnet/gateway_handler.go) so it was deleted outright with no forwarder — a forwarder for a single-caller symbol would be pure drift."
  - "Moved an existing internal/core/ulid_internal_test.go (generateULID clamp test, not enumerated in the plan's files_modified) into internal/ulidgen alongside the other generator tests — required to keep internal/core building after the move."
  - "Corrected the retained ULID doc/test rationale to drop stale 'lexicographic order MUST match arrival order' and PostgresEventStore.Replay claims (cross-AI rounds 9/12): ULID identity/dedup and monotonic-generation are documented as two separate properties; EventBus ordering is exclusively JetStream's per-stream sequence."
  - "Fixed a wrapcheck finding on the ulidgen.Parse forwarder call by wrapping via oops.Wrap instead of returning the external package's error bare."

requirements-completed: [ARCH-05]

coverage:
  - id: D1
    description: "internal/ulidgen leaf holds the single ULID entropy source; core.NewULID/core.ParseULID forward to it; both gateway packages generate IDs via ulidgen.New() directly"
    requirement: "ARCH-05"
    verification:
      - kind: unit
        ref: "internal/ulidgen/ulidgen_test.go#TestNewULIDRemainsStrictlyMonotonicUnderRapidSuccessiveCalls"
        status: pass
      - kind: unit
        ref: "internal/core/ulid_test.go#TestNewULIDForwarderYieldsValidStrictlyIncreasingULIDs"
        status: pass
    human_judgment: false
  - id: D2
    description: "internal/cmdparse leaf holds the command grammar (verbatim, byte-identical); core.ParseCommand deleted; telnet parses through cmdparse.ParseCommand"
    requirement: "ARCH-05"
    verification:
      - kind: unit
        ref: "internal/cmdparse/cmdparse_test.go#TestParseCommand"
        status: pass
    human_judgment: false
  - id: D3
    description: "internal/sessionlease leaf holds DefaultRefreshInterval (15s, single definition); session.DefaultLeaseRefreshInterval forwards; both gateways read the leaf directly"
    requirement: "ARCH-05"
    verification:
      - kind: unit
        ref: "internal/sessionlease/sessionlease_test.go#TestDefaultRefreshIntervalIsFifteenSeconds"
        status: pass
      - kind: integration
        ref: "task test:int (cmd/holomush parseSessionConfig, internal/session reaper)"
        status: pass
    human_judgment: false
  - id: D4
    description: "internal/telnet and internal/web import no internal/core, internal/session, internal/grpc, internal/world, internal/access, internal/command, internal/store, internal/plugin, internal/eventbus, or internal/auth — full transitive closure verified via go list -deps"
    requirement: "ARCH-05"
    verification:
      - kind: other
        ref: "go list -deps ./internal/telnet | rg -c 'holomush/internal/(core|session|grpc|world|access|command|store|plugin|eventbus)$' -> 0; same for ./internal/web"
        status: pass
    human_judgment: false

duration: 25min
completed: 2026-07-17
status: complete
---

# Phase 07 Plan 03: Gateway Leaf Extraction (ulidgen, cmdparse, sessionlease) Summary

**Extracted internal/ulidgen, internal/cmdparse, internal/sessionlease as dependency-free leaves so internal/telnet and internal/web no longer import internal/core or internal/session — D-16's three remaining gateway leaks are closed.**

## Performance

- **Duration:** ~25 min
- **Completed:** 2026-07-17
- **Tasks:** 3
- **Files modified:** 15 (7 created, 8 modified; 2 deleted with no replacement)

## Accomplishments

- `internal/ulidgen` now owns the single monotonic ULID entropy source (moved verbatim, including the holomush-nri6e clamp rationale); `core.NewULID`/`core.ParseULID` are documented forwarders so the ~68 existing call sites and the `internal/eventbus → internal/core` edge are unchanged; both gateways call `ulidgen.New()` directly.
- `internal/cmdparse` now owns the command grammar (byte-identical `ParseCommand`); `internal/core/command.go` and its test were deleted outright since the grammar had exactly one external consumer (`internal/telnet/gateway_handler.go`) — a forwarder would have been drift.
- `internal/sessionlease` now owns `DefaultRefreshInterval` (15s, one definition); `session.DefaultLeaseRefreshInterval` is a documented forwarder so `cmd/holomush`'s `parseSessionConfig` and `internal/session`'s own callers are untouched; both gateways read the leaf directly.
- `internal/telnet` and `internal/web`'s transitive dependency closures are now verified clear of `internal/core`, `internal/session`, `internal/grpc`, `internal/world`, `internal/access`, `internal/command`, `internal/store`, `internal/plugin`, `internal/eventbus`, and `internal/auth` — this is the ARCH-05 leaf-work convergence point; 07-04 can now add these to a `forbidden` import list wholesale with no escape hatch.

## Task Commits

1. **Task 1: internal/ulidgen — move the generator, keep core.NewULID as the forwarder** - `e06bb4001` (feat)
2. **Task 2: internal/cmdparse — move the command grammar** - `297b2e052` (feat)
3. **Task 3: internal/sessionlease — move the lease refresh interval** - `9b0bc16e9` (feat)

_No separate plan-metadata commit was requested for this plan; STATE.md/ROADMAP.md updates and this SUMMARY land in the standard post-execution commit._

## Files Created/Modified

- `internal/ulidgen/ulidgen.go` - the monotonic ULID generator (moved verbatim from `internal/core/ulid.go`)
- `internal/ulidgen/ulidgen_test.go` - the holomush-nri6e monotonicity test suite, moved with corrected (non-ordering) rationale
- `internal/ulidgen/ulidgen_internal_test.go` - the `generateULID` clamp-regression test, moved from `internal/core/ulid_internal_test.go` (not in the plan's `files_modified`; required to keep `internal/core` building — see Deviations)
- `internal/core/ulid.go` - rewritten to two documented forwarders: `NewULID` and `ParseULID`
- `internal/core/ulid_test.go` - rewritten to a single forwarder smoke test (`TestNewULIDForwarderYieldsValidStrictlyIncreasingULIDs`)
- `internal/cmdparse/cmdparse.go` - the command grammar (moved verbatim from `internal/core/command.go`); `internal/core/command.go` deleted
- `internal/cmdparse/cmdparse_test.go` - the existing test cases plus a new lowercase-verb/inner-spacing pin case; `internal/core/command_test.go` deleted
- `internal/sessionlease/sessionlease.go` - `DefaultRefreshInterval = 15 * time.Second` with its I-LIVE-4 doc comment
- `internal/sessionlease/sessionlease_test.go` - value-pin test
- `internal/session/reaper.go` - `DefaultLeaseRefreshInterval` becomes a documented forwarder to the leaf
- `internal/telnet/gateway_handler.go` - repointed to `ulidgen.New()` and `cmdparse.ParseCommand`; `internal/core` import dropped
- `internal/telnet/limits.go` - repointed to `sessionlease.DefaultRefreshInterval`; `internal/session` import dropped
- `internal/web/handler.go` - repointed to `ulidgen.New()` and `sessionlease.DefaultRefreshInterval`; `internal/core`/`internal/session` imports dropped
- `internal/web/translate_test.go` - test-fixture ULID generation switched from `core.NewULID()` to `ulidgen.New()`, clearing `internal/core` out of `internal/web`'s test code too
- `CLAUDE.md` - one note added to § ULID Generation naming the new leaf and the gateway's direct-call convention

## Decisions Made

- Retained `core.NewULID`/`core.ParseULID` and `session.DefaultLeaseRefreshInterval` as forwarders (D-03 stands: no dependency inversion), rather than repointing every core-side call site — the gateway is the only caller that needed to move.
- Deleted `core.ParseCommand` with no forwarder since it had exactly one external consumer.
- Corrected stale ULID-as-ordering prose in both the retained doc comment and the moved test's rationale comment (cross-AI rounds 9 and 12): dedup needs a stable/nonzero/unique ID, not lex order; monotonic generation is a separately-justified generator property; EventBus ordering is exclusively JetStream's per-stream sequence.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Moved an un-enumerated `generateULID` test file**
- **Found during:** Task 1 (internal/ulidgen extraction)
- **Issue:** `internal/core/ulid_internal_test.go` (a package-internal test of `generateULID`'s clamp behavior, `TestGenerateULIDClampsRegressingClock`) was not listed in the plan's `read_first` or `files_modified`, but calling the unqualified `generateULID` symbol, it broke `internal/core`'s build once `generateULID` moved to `internal/ulidgen`.
- **Fix:** Moved the file verbatim to `internal/ulidgen/ulidgen_internal_test.go` (package `ulidgen`, same unqualified calls) — it belongs with the other `generateULID` tests per the plan's own stated principle ("the `generateULID` tests are in-package and must stay in-package").
- **Files modified:** `internal/core/ulid_internal_test.go` (deleted), `internal/ulidgen/ulidgen_internal_test.go` (created)
- **Verification:** `task test -- ./internal/ulidgen/ ./internal/core/` passes; `TestGenerateULIDClampsRegressingClock` runs in its new home.
- **Committed in:** `e06bb4001` (Task 1 commit)

**2. [Rule 1 - Bug/lint] Fixed a wrapcheck violation on the ParseULID forwarder**
- **Found during:** Task 3 (`task lint` run, part of the end-of-plan gate)
- **Issue:** `core.ParseULID`'s one-line `return ulidgen.Parse(s)` returned the external package's error bare, which `wrapcheck` flags once `ulidgen` became a separate package from `core`.
- **Fix:** Wrapped the error via `oops.Wrap(err)` (an allowlisted wrapcheck signature) instead of returning it directly.
- **Files modified:** `internal/core/ulid.go`
- **Verification:** `task lint` exits 0; `task test -- ./internal/core/` still passes.
- **Committed in:** `9b0bc16e9` (Task 3 commit, since that's where `task lint` is gated in the plan)

---

**Total deviations:** 2 auto-fixed (1 blocking, 1 lint/bug)
**Impact on plan:** Both were necessary for the plan's own stated invariants (in-package test placement; lint-clean commits) and neither changed the plan's architectural shape. No scope creep.

## Issues Encountered

None beyond the two deviations above.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- 07-04 can now add `internal/core`, `internal/session`, `internal/grpc`, `internal/world`, `internal/access`, `internal/command`, `internal/store`, `internal/plugin`, `internal/eventbus`, and `internal/auth` to its `forbidden` import list wholesale and bind INV-EVENTBUS-1 — verified via `go list -deps` that neither gateway's transitive closure contains any of them, so 07-04 has no remaining code to change, only enforcement to add.
- `internal/naming` was confirmed (already, no change needed) to remain a true dependency-free leaf; `naming.Theme` stays in `internal/telnet/guest_auth.go` untouched, per the plan's drift_correction.
- Full verification suite green: `task build`, `task test` (whole repo, 10252 tests), `task test:int` (10678 tests, 7 known quarantines skipped), `task lint`.

---

*Phase: 07-event-model-bootstrap-decomposition*
*Completed: 2026-07-17*

## Self-Check: PASSED

All 7 created files and the SUMMARY.md itself verified present on disk; all 3 task commits (`e06bb4001`, `297b2e052`, `9b0bc16e9`) verified present in `git log --oneline --all`.
