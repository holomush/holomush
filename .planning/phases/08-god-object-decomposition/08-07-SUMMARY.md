---
phase: 08-god-object-decomposition
plan: 07
subsystem: grpc
tags: [arch-01, decomposition, query, coreserver, closeout]
status: complete
requires:
  - "08-05 (CoreServer.buildHandlers() — the ordered constructor this plan extends)"
  - "08-03 (SubscribeHandler; supplies the SessionIdentityBuilder seam this plan repoints)"
provides:
  - "internal/grpc.QueryHandler — the current-state query cluster as a constructor-injected unit"
  - "internal/grpc.QueryDeps — its 12-collaborator dependency bundle"
  - "QueryHandler.buildCharacterIdentity — the new owner of the SessionIdentityBuilder seam"
affects:
  - "internal/grpc/server.go (CoreServer is now a facade over four constructor-injected units)"
  - "ARCH-01 success criterion 1 — closed; all four CoreServer clusters extracted"
tech-stack:
  added: []
  patterns:
    - "Deps-struct constructor injection (continuing 08-03 and 08-05)"
    - "Receiver-only relocation: methods stay in their existing one-method files"
    - "buildHandlers() extended to four units with a two-owner ordering constraint"
key-files:
  created:
    - internal/grpc/query_handler.go
    - internal/grpc/query_handler_test.go
  modified:
    - internal/grpc/server.go
    - internal/grpc/query_stream_history.go
    - internal/grpc/list_focus_presence.go
    - internal/grpc/list_session_streams.go
    - internal/grpc/list_available_commands.go
    - internal/grpc/refresh_connection.go
    - internal/grpc/session_identity.go
    - internal/grpc/query_stream_history_test.go
    - internal/grpc/list_focus_presence_test.go
    - internal/grpc/list_session_streams_test.go
    - internal/grpc/list_available_commands_test.go
    - internal/grpc/refresh_connection_test.go
    - internal/grpc/session_identity_test.go
decisions:
  - "buildCharacterIdentity moves to QueryHandler, so buildHandlers() now has TWO seam owners (lifecycleHandler, queryHandler) constructed before their two consumers (commandHandler, subscribeHandler). This is the 08-03 interlock landing."
  - "Methods stayed in their existing one-method files; only the receiver and field-selector prefix changed. The whole diff for six files is 33 receiver lines — no comment, error code, slog call, or //nolint touched."
  - "server.go GREW 642 -> 657 LoC. Five of the seven methods already lived outside server.go, so the facade gained ~65 lines of delegation and construction while shedding only GetCommandHistory's 41. The phase-level figure is 1891 -> 657."
metrics:
  duration: ~75m
  tasks: 3
  files: 15
  completed: 2026-07-19
---

# Phase 8 Plan 07: Current-State Query Cluster Extraction Summary

Extracted the seven-method current-state query cluster off `*CoreServer` into a
constructor-injected `QueryHandler`, proven constructible from `package grpc_test` with no
`*CoreServer` and no harness. This closes ARCH-01: `CoreServer` is now a facade over four
extracted units, its exported method set unchanged at 23, and `server.go` sits at **657 LoC**
against a pre-phase 1891.

## What Shipped

| Task | Outcome | Commit |
| --- | --- | --- |
| RED gate (Task 1, `tdd="true"`) | 5 proof tests fail to compile — `QueryHandler`/`QueryDeps` do not exist | `44c1a5e47` |
| 1+2 — Extract the unit, delegate from the facade | 7 methods relocated, bodies verified verbatim | `2fb515438` |
| 3 — Wave-boundary integration gate + ARCH-01 closeout | `task test:int` exit 0 (10769 tests), `task lint` exit 0 | (verification only) |

**Tasks 1 and 2 landed in one commit**, following 08-03 deviation 3 and 08-05: Task 1 removes six
RPCs from `CoreServer`, which stops it satisfying `corev1.CoreServiceServer`. Splitting the commits
would leave a non-compiling intermediate and break `git bisect`.

## The call graph, not the matrix — and this time the edge was a plan premise

Per the standing prior-wave warning, the dependency set was derived from the call graph. Two
method-level edges the field matrix cannot see:

| Edge | Consequence |
| --- | --- |
| `SubscribeHandler` consumes `buildCharacterIdentity` via 08-03's `SessionIdentityBuilder` seam | Moving the helper to `QueryHandler` forces `queryHandler` to be constructed **before** `subscribeHandler` |
| `accessEngine` is read by `auth_handlers.go:568,584` (the auth cluster, which stays on the facade) as well as by this cluster | It cannot be "moved"; it is passed by value to `QueryDeps` and remains a `CoreServer` field |

`buildHandlers()` therefore grew from three units to four, with **two** seam owners:

```
lifecycleHandler   (owns runDisconnectHooks, recomputeSessionLiveness)
queryHandler       (owns buildCharacterIdentity)
commandHandler     (consumes RunDisconnectHooks)
subscribeHandler   (consumes BuildIdentity + RecomputeLiveness)
```

08-05's handoff instruction was followed exactly: the query unit was added inside
`buildHandlers()`, not appended to `NewCoreServer`. That was the right call — the ordering
constraint here is real and a construction-site append would have captured a nil `queryHandler`
in `SubscribeDeps.BuildIdentity`.

## T-8-02 — every session-ownership preamble survived verbatim

The high-severity threat. Two independent checks:

**1. The six in-place files were diffed line-by-line.** Their entire diff is 33 lines, and every
one is a receiver-selector rename (`s.` → `h.`) or a `func` signature. No comment, no `oops.Code`,
no `.With(...)` key, no `slog.*Context` call, and no `//nolint` directive appears anywhere in the
diff. This is stronger than a normalized comparison because the diff itself is the evidence.

**2. `GetCommandHistory` — the one body that changed files — is byte-identical.** Compared against
`git show origin/main:internal/grpc/server.go`, normalizing only the receiver with **perl**
(not `sed`: BSD `sed` silently no-ops on `\b`, the trap 08-06 recorded):

```
pre=41 lines  post=41 lines  →  diff empty
GetCommandHistory: BYTE-IDENTICAL after receiver normalization
```

All five `auth.ValidateSessionOwnership` call sites are intact, each still an ordinary free-function
statement inside its own body:

| Method | Site |
| --- | --- |
| `GetCommandHistory` | `query_handler.go:182` |
| `ListFocusPresence` | `list_focus_presence.go:45` |
| `ListSessionStreams` | `list_session_streams.go:47` |
| `ListAvailableCommands` | `list_available_commands.go:32` |
| `RefreshConnection` | `refresh_connection.go:23` |

`test/integration/auth/` is green.

## Nil-collaborator semantics — preserved, each justified from its own pre-split behavior

This cluster carries the phase's densest set of fail-closed defaults. Every one was read from its
pre-split site and migrated with its field doc comment; **no blanket rule was applied**.

| Collaborator | Pre-split semantics | Source | Preserved how |
| --- | --- | --- | --- |
| `accessEngine` nil | public stream reads **DENIED** (fail-closed) | `server.go:185-187` | comment migrated verbatim; pinned by `...DeniesPublicStreamWhenAccessEngineIsNil` |
| `commandQuerier` nil | **PERMISSION_DENIED** (fail-closed) | `server.go:193-195` | comment migrated; pinned by `...FailsClosedWhenCommandQuerierIsNil` |
| `historyReader` nil | **INTERNAL**, never an empty success | `server.go:203-205` | comment migrated; pinned by `...ReturnsInternalWhenHistoryReaderIsNil` |
| `identityRegistry` nil | ULID-string fallback | `server.go:180-183` | comment migrated |
| `characterNameResolver` nil | **INTERNAL** — misconfiguration, *not* a security boundary | `list_focus_presence.go:97-99` | guard untouched in the moved body |
| `focusCoordinator` nil | ambient-stream fallback (mirrors Subscribe) | `list_session_streams.go:88-90` | guard untouched |
| `bindings` nil / `cryptoActive` false | zero (passthrough) identity | `session_identity.go:21-23` | guard untouched |

Note the deliberate asymmetry inside a single method: in `ListFocusPresence` a nil `accessEngine`
returns `PERMISSION_DENIED` while a nil `characterNameResolver` returns `INTERNAL`. Those are
different fail directions two lines apart, and both were left exactly as found — the second is a
wiring bug, not an authorization decision, and collapsing them would either hide a
misconfiguration or invent a denial.

Behaviors 2, 3, and 4 assert the **top-level** oops code via `oops.AsOops(err).Code()` per
`.claude/rules/grpc-errors.md`, not a message substring — so a differently-worded permissive
return cannot pass.

## D-02 / SC1: the bar was met

`internal/grpc/query_handler_test.go` is `package grpc_test` and constructs the unit from
`sessionmocks.NewMockStore`, `authmocks.NewMockPlayerSessionRepository`, one hand-rolled
`stubHistoryReader`, and literals. No `*CoreServer` is constructed, no `integrationtest` import,
no `//go:build integration`.

The plan's acceptance grep (`rg -c 'CoreServer|integrationtest|go:build integration'` returns 0)
reports matches — **every one is prose in a doc comment** describing the seam. This is the same
grep artifact 08-03, 08-04, and 08-05 recorded; the criterion is a proxy and the proxy is wrong
here. Deleting explanatory prose to satisfy it would make the code worse. The structural property
holds under the comment-excluding form:

```
rg -n 'CoreServer' internal/grpc/query_handler.go internal/grpc/query_handler_test.go \
  | rg -v '^\S+:[0-9]+:\s*//'
→ NONE
```

`stubHistoryReader.QueryHistory` calls `t.Fatal` rather than returning data: if the authorization
gate under test ever lets a request through, the test fails at the fetch instead of silently
passing on an empty result.

## Contract invariants — all verified

| Check | Result |
| --- | --- |
| Exported `CoreServer` method set pre vs post | **identical** (23 methods, diffed against `origin/main`) |
| `git diff --stat origin/main...HEAD -- api/proto/ pkg/proto/ internal/grpc/mocks/` | **empty** |
| `mockery` re-run, then `git diff --stat -- internal/grpc/mocks/` | **empty** — research assumption A3 confirmed, not assumed |
| `git diff -- cmd/holomush/sub_grpc.go internal/testsupport/integrationtest/harness.go` | **empty — both untouched (D-04)** |
| `git diff --stat origin/main...HEAD -- test/integration/` | **empty — zero assertion churn (D-15)** |
| `git diff --stat origin/main...HEAD -- internal/access/` | **empty — `abac-reviewer` is NOT required (D-19)** |
| Each of the six delegating RPC methods | **3 lines** (well under the 10-line criterion) |
| `rg -n 'CoreServer' internal/grpc/query_handler.go`, comments excluded | **NONE** — no backpointer |
| `wc -l internal/grpc/server.go` | **657** |

`focusCoordinator`, `streamContributor`, and `identityRegistry` are passed as the **same** field
reads into both `QueryDeps` and `SubscribeDeps` (`server.go:417-421` and `:477-482`) — no clone, no
wrapper, no re-derivation. Each is a read-only interface, never shared mutable state.

## Every surviving `CoreServer` field, justified

Zero fields were deletable — the same result 08-03's Task 2 reached, and correct rather than a
miss. `CoreServerOption` is `func(*CoreServer)` and D-04 pins that signature, so every
option-injected collaborator needs a struct field to land in between the option loop and
`buildHandlers()`. `task lint` (which runs `unused`) is green, so no field is dead.

| Field | Justification |
| --- | --- |
| `sessionStore` | feeds all 4 Deps; also read by `auth_handlers.go` |
| `playerSessionRepo` | feeds all 4 Deps; also read by `auth_handlers.go` |
| `presence` | feeds `CommandDeps` + `LifecycleDeps` |
| `disconnectHooks` | feeds `LifecycleDeps` |
| `dispatcher`, `cmdServices` | feed `CommandDeps`; also the `NewCoreServer` nil panic guard |
| `publisher` | feeds `CommandDeps` |
| `subscriber`, `verbRegistry`, `streamRegistry`, `sceneMute`, `worldQuerier` | feed `SubscribeDeps` |
| `focusCoordinator`, `streamContributor`, `identityRegistry` | feed **both** `SubscribeDeps` and `QueryDeps` |
| `accessEngine` | feeds `QueryDeps`; also read by `auth_handlers.go:568,584` |
| `historyReader`, `characterNameResolver`, `commandQuerier`, `bindings`, `cryptoActive` | feed `QueryDeps` |
| `gameID` | read by `CoreServer.currentGameID`, which is the `GameID` provider for 3 Deps |
| `newSessionID`, `authService`, `resetService`, `characterService`, `playerRepo`, `charRepo`, `guestService`, `sessionDefaults` | the auth cluster, which stays on the facade (`auth_handlers.go`) |
| `subscribeHandler`, `commandHandler`, `lifecycleHandler`, `queryHandler` | the four extracted units |

## ARCH-01 closeout record (for Wave C)

**Four extracted units, four `package grpc_test` proof tests:**

| Unit | Proof test (all `package grpc_test`, no harness, no build tag) |
| --- | --- |
| `SubscribeHandler` | `internal/grpc/subscribe_handler_test.go` |
| `CommandHandler` | `internal/grpc/command_handler_test.go` |
| `LifecycleHandler` | `internal/grpc/lifecycle_handler_test.go` |
| `QueryHandler` | `internal/grpc/query_handler_test.go` |

**`server.go` LoC:** pre-phase **1891** → post-phase **657**.

Trajectory: 1891 → 1154 (08-03) → 642 (08-05) → **657** (08-07). The final step is a **+15
increase**, and that is expected rather than a regression: five of this cluster's seven methods
already lived in their own files, so the facade shed only `GetCommandHistory`'s 41 lines while
gaining six 3-line delegations plus a 26-line `newQueryHandler`. The plan's objective called this
out ("this plan gives them their own *dependencies*, which is the part the earlier file-splitting
never did").

**Unit file sizes** (Wave C ceiling candidates): `subscribe_handler.go` 973, `command_handler.go`
417, `lifecycle_handler.go` 386, `query_handler.go` 213.

**Method-set equality:** confirmed identical at 23 methods, diffed against `origin/main`.

**`git diff --stat origin/main...HEAD -- test/integration/`:** empty — no assertion churn.

**`internal/access/` touched:** **no** (`git diff --stat origin/main...HEAD -- internal/access/`
is empty). `abac-reviewer` is therefore **not** required at PR time under D-19.

## Deviations from Plan

### 1. `QueryHandler` carries 8 methods, not 7

`rg -c 'func \(h \*QueryHandler\)'` reports **8**: the seven planned methods plus `currentGameID`,
needed by `QueryStreamHistory`'s `eventbus.Qualify` call and mirroring
`SubscribeHandler.currentGameID` / `CommandHandler.currentGameID`. Same seam-wrapper overcount
08-03 (10 vs 8) and 08-05 (6 vs 4) recorded. The plan's acceptance criterion asserting exactly 7 is
counting the cluster, not the receiver.

### 2. [Rule 3 — Blocking] 36 in-package fixtures needed `buildHandlers()`

`query_stream_history_test.go`, `list_focus_presence_test.go`, `list_session_streams_test.go`,
`list_available_commands_test.go`, and `refresh_connection_test.go` build bare `&CoreServer{...}`
literals and then call the exported RPC. Post-split those reach a nil `queryHandler` and fault —
exactly the failure mode 08-05 documented. 35 fixtures gained one line, `s.buildHandlers()`, and the
`newQueryStreamHistoryServer` helper was changed from `return &CoreServer{...}` to assign → build →
return.

`session_identity_test.go` is different: it calls the *unexported* `buildCharacterIdentity`
directly, so its four fixtures now construct `NewQueryHandler(QueryDeps{Bindings: …,
CryptoActive: …})`. Its `// Verifies: INV-CRYPTO-118` annotation and every assertion are untouched.

**No assertion was edited anywhere** — only constructors. All six files are `package grpc` outside
`test/integration/`, so D-15 does not govern them, and `test/integration/` remains byte-empty in
the diff.

### 3. [Rule 1 — Bug] Two defects in my own RED fixtures

Both were real test bugs caught by the RED→GREEN cycle, not production issues:

- `01HYXPLAYER00000000000001` is 25 characters and contains `L`, which Crockford base32 excludes.
  `ulid.MustParse` panicked. Replaced with `01HYXPLYR00000000000000001` (26 chars, valid alphabet).
- A `//nolint:gosec` on the test token was flagged unused by `nolintlint` — `gosec` never fired on
  it. Removed the directive and explained the constant in prose instead. Per the project rule,
  widening `.golangci.yaml` was never on the table; the correct fix was deleting a directive that
  suppressed nothing.

### 4. `log/slog` dropped from `server.go`'s imports

`GetCommandHistory` was the last `slog` caller in `server.go`. The import was removed with it; the
`slog.DebugContext` call itself travelled verbatim into `query_handler.go`.

### 5. Finding 5 (invariant provenance) did not fire — by construction

Five prior plans hit `TestProvenanceGuard` on `task test:int`. This plan did not, because the
methods stayed in their existing files: `INV-PRIVACY-1/5`, `INV-EVENTBUS-18`, `INV-SCENE-1`,
`INV-PRESENCE-4/9`, and `INV-COMMAND-3` never changed file. `INV-SCENE-62` stays on `server.go`'s
`sceneMute` field comment, and `invariants.yaml` holds no ref to `internal/grpc/server.go`. This
was checked **before** the move rather than discovered by the gate; `test/meta` is green.

## Verification

| Gate | Exit |
| --- | --- |
| `task build` | 0 |
| `task test -- ./internal/grpc/...` | 0 (624 tests) |
| `task test -- ./internal/grpc/... ./cmd/holomush/... ./internal/testsupport/... ./internal/web/...` | 0 (1544 tests, 1 skipped) |
| `task lint` | 0 |
| `task test:int` | 0 (**10769 tests**, 7 skipped/quarantined) |
| `task fmt` | no residual diff |

Named downstream suites, all green:

```
✓  test/integration/stream_history (463ms)
✓  test/integration/list_session_streams (441ms)
✓  test/integration/streams (20.237s)
✓  test/integration/scenes (1m41.726s)
✓  test/integration/access (5.078s)
```

`test/integration/access/` is the detector for T-8-01 (the `accessEngine` fail-closed default) and
is green against the migrated gates.

## Known Stubs

None. `QueryHandler` is fully wired; every nil collaborator is an intentional, documented,
test-pinned configuration.

## Threat Flags

None. No new network endpoint, auth path, file-access pattern, or schema change — this plan
relocated existing code and its existing gates. No `api/proto/**` change; `internal/access/`
untouched.

## Notes for Wave C

- **ARCH-01 is complete.** Four units, four external-package proof tests, method set fixed at 23.
- `server.go` is **657 LoC**; the ceiling should be calibrated from this number, not from 08-05's
  642 (see the +15 explanation above).
- `query_handler.go` (213 LoC) is the smallest of the four unit files — mostly `QueryDeps`
  documentation, since six of its seven method bodies live in their original per-RPC files.
- The deferred relocations logged by 08-03/08-05 still stand: `replayCompleteFrame`,
  `streamClosedFrame`, `subscribeSessionNotFound`, `filterSetToSlice`, and `actorIDString` are used
  only by `subscribe_handler.go` in production but still live in `server.go`. `responseMeta` is now
  shared across `server.go`, `query_handler.go`, and the auth cluster, so it correctly stays put.
- `export_test.go` gained no new shims: all seven query methods are exported or reachable through
  exported RPCs, so the proof test needed none.

## Self-Check: PASSED

Created files exist:
- `internal/grpc/query_handler.go` — FOUND
- `internal/grpc/query_handler_test.go` — FOUND

Commits exist on `gsd/phase-08-god-object-decomposition`:
- `44c1a5e47` test(08-07): add failing tests for the QueryHandler seam — FOUND
- `2fb515438` refactor(08-07): extract QueryHandler from CoreServer — FOUND
