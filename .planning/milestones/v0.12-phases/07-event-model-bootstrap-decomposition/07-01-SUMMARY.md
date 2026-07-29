---
phase: 07-event-model-bootstrap-decomposition
plan: 01
subsystem: infra
tags: [go, grpc, gateway-boundary, arch-05, package-decomposition]

# Dependency graph
requires: []
provides:
  - "internal/grpcclient — a true-leaf gRPC client package (Client, ClientConfig, NewClient, TranslateSubscribeErr, 45 RPC wrapper methods)"
  - "internal/telnet transitive closure reduced from 47 to 10 holomush/internal/ packages, with internal/grpc, internal/world, internal/access, internal/command, internal/store all absent"
affects: [07-03 (closure gate enforcement), gateway-boundary rule]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "gRPC client extracted as a dependency-free leaf package so protocol-translation gateways (telnet/web) can hold a client without pulling in the CoreServer monolith's domain closure"

key-files:
  created:
    - internal/grpcclient/client.go
    - internal/grpcclient/client_test.go
  modified:
    - cmd/holomush/gateway.go
    - cmd/holomush/deps.go
    - cmd/holomush/deps_test.go
    - internal/telnet/gateway_handler.go
    - internal/telnet/gateway_handler_test.go
    - internal/web/handler_test.go
    - internal/web/scene_handlers_test.go
    - internal/grpc/server_helpers_test.go
    - test/integration/auth/multi_tab_test.go
    - test/integration/phase1_5_test.go

key-decisions:
  - "Local testPlayerSessionToken const added to internal/grpcclient/client_test.go (deliberately not shared with internal/grpc's package-private constant of the same name)"
  - "TestSubscribeSessionNotFoundStampsUnauthenticatedWireCode relocated from client_test.go to internal/grpc/server_helpers_test.go (it exercises the package-private subscribeSessionNotFound server function, which cannot move to grpcclient)"

patterns-established:
  - "Verbatim package moves that carry test files must audit for package-private helper dependencies the moved file used implicitly (shared test constants, sibling package-private functions) — a byte-for-byte move can still fail to compile"

requirements-completed: [ARCH-05]

coverage:
  - id: D1
    description: "internal/grpcclient is a true leaf package (proto + grpc-go + oops only) holding the full gRPC client surface moved verbatim from internal/grpc/client.go"
    requirement: "ARCH-05"
    verification:
      - kind: unit
        ref: "task test -- ./internal/grpcclient/ (30 tests)"
        status: pass
      - kind: other
        ref: "go list -deps ./internal/grpcclient | rg -c 'holomush/internal/' == 1 (itself only)"
        status: pass
    human_judgment: false
  - id: D2
    description: "internal/telnet's transitive internal closure no longer contains internal/grpc, internal/world, internal/access, internal/command, or internal/store; closure shrank from 47 to 10 packages"
    requirement: "ARCH-05"
    verification:
      - kind: other
        ref: "go list -deps ./internal/telnet | rg -c 'holomush/internal/(grpc|world|access|command|store)$' == 0 (anchored — see Deviations for why the plan's unanchored grep needed correction)"
        status: pass
    human_judgment: false
  - id: D3
    description: "Every caller of the moved Client/ClientConfig/NewClient/TranslateSubscribeErr symbols rewired to internal/grpcclient; internal/grpc retains its server-side symbols and package doc"
    requirement: "ARCH-05"
    verification:
      - kind: unit
        ref: "task test -- ./internal/telnet/ ./internal/web/ ./internal/grpc/ ./cmd/holomush/ (1567 tests)"
        status: pass
      - kind: integration
        ref: "task test:int (10672 tests, 7 pre-existing quarantined skips)"
        status: pass
      - kind: other
        ref: "task build; task lint"
        status: pass
    human_judgment: false

duration: 20min
completed: 2026-07-17
status: complete
---

# Phase 07 Plan 01: Extract gRPC client into internal/grpcclient Summary

**gRPC client (Client/ClientConfig/NewClient/TranslateSubscribeErr + 45 RPC wrappers) moved verbatim from internal/grpc into a new leaf package internal/grpcclient, shrinking internal/telnet's transitive closure from 47 to 10 holomush/internal/ packages.**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-07-17T21:46:36Z
- **Completed:** 2026-07-17T22:04:45Z
- **Tasks:** 2 (squashed into one commit per plan's cross-AI-review-mandated commit discipline)
- **Files modified:** 12 (2 created, 8 modified, 2 deleted-via-rename)

## Accomplishments
- Created `internal/grpcclient`, a true leaf package (only stdlib + `samber/oops` + `google.golang.org/grpc` + generated proto packages) holding the entire gRPC client surface: `Client`, `ClientConfig`, `NewClient`, `TranslateSubscribeErr`, `translateCheckPlayerSessionErr`, and all 45 RPC wrapper methods, moved verbatim (no method body, error code, or comment text changed).
- Rewired every real consumer of the moved symbols: `cmd/holomush/gateway.go` and `deps.go` (the `GRPCClientFactory`), `internal/telnet/gateway_handler.go` (`TranslateSubscribeErr` at the Subscribe recv-error path), and the compile-time `CoreClient`/`SceneAccessClient` interface assertions in `internal/web/handler_test.go` and `internal/web/scene_handlers_test.go`.
- `internal/grpc/server_helpers_test.go` now imports `internal/grpcclient` and qualifies its two `TranslateSubscribeErr` round-trip assertions (`grpcclient.TranslateSubscribeErr`) — `internal/grpc` importing `internal/grpcclient` creates no cycle since grpcclient has zero internal deps.
- `test/integration/phase1_5_test.go` keeps a dual import (`grpcpkg` for `NewCoreServer`/`WithEventStore`, new `grpcclient` for `NewClient`/`ClientConfig`) at all three client construction sites (lines 298, 375, 530).
- `internal/telnet`'s transitive internal closure dropped from 47 packages to 10, and contains none of `internal/grpc`, `internal/world`, `internal/access`, `internal/command`, or `internal/store` — closing the gap the plan's `<drift_correction>` identified: `cmd/holomush/gateway.go` was not in `coreOnlyFiles` and imported `internal/grpc` purely for client construction.

## Task Commits

Per the plan's explicit commit-discipline instruction (cross-AI round 10, rev 13 — BLOCKER), Tasks 1 and 2 were executed as separate work units for context control but squashed into **one** commit, since the mid-unit state (client extracted but callers unrewired) does not compile as a whole.

1. **Task 1+2 (squashed): Move internal/grpc/client.go to internal/grpcclient and rewire every caller** - `1e13270d5` (feat)

_No separate plan-metadata commit was issued mid-plan; this SUMMARY's final commit (below) captures execution results per the standard executor flow._

## Files Created/Modified
- `internal/grpcclient/client.go` - New leaf package: `Client`, `ClientConfig`, `NewClient`, `TranslateSubscribeErr`, `translateCheckPlayerSessionErr`, all 45 RPC wrapper methods (verbatim move from `internal/grpc/client.go`, package doc rewritten)
- `internal/grpcclient/client_test.go` - Moved test suite (30 tests); added a local `testPlayerSessionToken` const; removed `TestSubscribeSessionNotFoundStampsUnauthenticatedWireCode` (relocated — see Deviations)
- `internal/grpc/client.go`, `internal/grpc/client_test.go` - Deleted (content lives at the new path; git recorded these as renames)
- `internal/grpc/server_helpers_test.go` - Added `internal/grpcclient` import; qualified two `TranslateSubscribeErr` calls; added the relocated `TestSubscribeSessionNotFoundStampsUnauthenticatedWireCode`
- `cmd/holomush/gateway.go` - `holoGRPC` alias repointed from `internal/grpc` to `internal/grpcclient`
- `cmd/holomush/deps.go` - Same repoint (`GRPCClientFactory` field type)
- `cmd/holomush/deps_test.go` - Same repoint (8 `GRPCClientFactory` closures)
- `internal/telnet/gateway_handler.go` - Import repointed to bare `internal/grpcclient` (dropped the now-redundant `grpcclient` alias, since it already matched the new package name)
- `internal/telnet/gateway_handler_test.go` - `holoGRPC` alias repointed
- `internal/web/handler_test.go` - `holoGRPC` alias repointed (`var _ CoreClient = (*holoGRPC.Client)(nil)`)
- `internal/web/scene_handlers_test.go` - `holoGRPC` alias repointed (`var _ SceneAccessClient = (*holoGRPC.Client)(nil)`)
- `test/integration/auth/multi_tab_test.go` - `holoGRPC` alias repointed (`TranslateSubscribeErr` only)
- `test/integration/phase1_5_test.go` - Added `grpcclient` import alongside retained `grpcpkg` (`internal/grpc`) import; 3 client construction call sites repointed

## Decisions Made
- Kept the existing per-file aliases (`holoGRPC`, `grpcpkg`) wherever a file already had one, to keep diffs minimal, per the plan's explicit instruction — except `internal/telnet/gateway_handler.go`, where the plan directed dropping the redundant `grpcclient` alias since it now matches the package name exactly.
- `test/integration/phase1_5_test.go` stays dual-import (`grpcpkg` for server symbols, `grpcclient` for client symbols) rather than a blanket rename, matching the round-5/rev-8 census-gap fix already encoded in the plan.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `internal/grpc/client_test.go` referenced a package-private constant unreachable from the new package**
- **Found during:** Task 1 (verbatim move), surfaced by `task test -- ./internal/grpcclient/`
- **Issue:** The plan's Task 1 action specified a byte-for-byte move of `client_test.go` with "only the package line changes." In reality, six test functions in that file pass `testPlayerSessionToken` (a package-private `const` defined in `internal/grpc/test_helpers_test.go`, shared by ~15 other `internal/grpc` test files) into `HandleCommandRequest.PlayerSessionToken`. That constant is unreachable from `package grpcclient` — a literal verbatim move fails to compile.
- **Fix:** Added a local, file-scoped `testPlayerSessionToken` const to `internal/grpcclient/client_test.go` (same string value) with a doc comment explaining it is deliberately not shared with `internal/grpc`'s constant of the same name, since none of the client-wrapper tests' mock servers actually validate the token — it only needs to be a stable, non-empty string.
- **Files modified:** `internal/grpcclient/client_test.go`
- **Verification:** `task test -- ./internal/grpcclient/` — 30/30 tests pass
- **Committed in:** `1e13270d5` (squashed Task 1+2 commit)

**2. [Rule 3 - Blocking] `internal/grpc/client_test.go`'s `TestSubscribeSessionNotFoundStampsUnauthenticatedWireCode` calls a package-private server function unreachable from the new package**
- **Found during:** Task 1 (verbatim move), surfaced by reading the full file before moving it (not caught by the plan's read_first list, which didn't flag this dependency)
- **Issue:** This test calls `subscribeSessionNotFound("test-session")` directly — an unexported function defined in `internal/grpc/server.go` that stamps the wire-level `codes.Unauthenticated` status. This is a server-side pin, not a client-wrapper test, and cannot move to `internal/grpcclient` (the function is package-private to `internal/grpc`).
- **Fix:** Relocated the test to `internal/grpc/server_helpers_test.go` (a file already touched in this plan's Task 2 for the `TranslateSubscribeErr` qualification), updating its `TranslateSubscribeErr` call to `grpcclient.TranslateSubscribeErr`. Documented the relocation in the test's doc comment for future readers.
- **Files modified:** `internal/grpcclient/client_test.go` (test removed), `internal/grpc/server_helpers_test.go` (test added)
- **Verification:** `task test -- ./internal/grpc/` — all tests including the relocated one pass
- **Committed in:** `1e13270d5` (squashed Task 1+2 commit)

**3. [Verification-detail, non-blocking] Plan's `go list -deps` closure-check regex is unanchored and self-matches the new package name**
- **Found during:** Task 2 acceptance-criteria verification
- **Issue:** The plan's own acceptance criterion `go list -deps ./internal/telnet | rg -c 'holomush/internal/(grpc|world|access|command|store)'` returns `1`, not the expected `0` — but the single match is `internal/grpcclient` itself (a substring match on the unanchored `grpc` alternative), which is the *intended, correct* new dependency this very plan introduces. The unanchored regex cannot distinguish `internal/grpc` (forbidden) from `internal/grpcclient` (this plan's entire deliverable).
- **Resolution:** Re-ran with an end-anchor (`'holomush/internal/(grpc|world|access|command|store)$'`), which returns `0` — confirming the true intent (telnet's closure contains none of the five forbidden domain packages) is satisfied. Same anchoring applies to the phase-level `<verification>` block's identical grep. No code change; this is purely a verification-command imprecision in the plan text, noted here so a future reader isn't confused by the raw (unanchored) command returning a nonzero count.
- **Files modified:** None
- **Verification:** `go list -deps ./internal/telnet | rg -c 'holomush/internal/(grpc|world|access|command|store)$'` → `0`; full unanchored closure listing shows the only `grpc`-prefixed entry is `internal/grpcclient`

---

**Total deviations:** 2 auto-fixed blocking issues (Rule 3), 1 non-blocking verification-command note.
**Impact on plan:** Both blocking fixes were necessary for the literal "verbatim move" instruction to compile at all — the plan's Task 1 read_first section didn't surface either package-private dependency in the moved test file. No scope creep: fixes are confined to test-file wiring, no production behavior changed, and both are exactly the kind of compile-time breakage Rule 3 exists to auto-resolve. The verification-detail note requires no code change.

## Issues Encountered
None beyond the deviations documented above.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `internal/grpcclient` is now available as the canonical leaf gRPC client for any future gateway-surface work in this phase.
- 07-03 (closure gate enforcement, per plan frontmatter) can now add `internal/grpc` to its forbidden-import list for `internal/telnet` without the `gateway.go` false-negative the plan's `<drift_correction>` identified — that gap is closed.
- No blockers for downstream phase-07 plans.

---
*Phase: 07-event-model-bootstrap-decomposition*
*Completed: 2026-07-17*

## Self-Check: PASSED

- FOUND: internal/grpcclient/client.go
- FOUND: internal/grpcclient/client_test.go
- FOUND: .planning/phases/07-event-model-bootstrap-decomposition/07-01-SUMMARY.md
- FOUND: 1e13270d5 (git log --oneline --all)
