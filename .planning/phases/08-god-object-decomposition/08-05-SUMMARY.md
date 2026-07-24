---
phase: 08-god-object-decomposition
plan: 05
subsystem: grpc
tags: [arch-01, decomposition, command, lifecycle, coreserver]
status: complete
requires:
  - "08-03 (SubscribeHandler; supplies the SessionLivenessRecomputer seam this plan repoints)"
provides:
  - "internal/grpc.CommandHandler — the command-execution cluster as a constructor-injected unit"
  - "internal/grpc.LifecycleHandler — the session-lifecycle cluster as a constructor-injected unit"
  - "internal/grpc.DisconnectHookRunner — the named function type carrying the command->lifecycle edge"
  - "CoreServer.buildHandlers() — the single ordered constructor for all three extracted units"
affects:
  - "internal/grpc/server.go (1154 -> 642 LoC; HandleCommand and Disconnect are 1-line delegators)"
  - "internal/grpc/auth_handlers.go (Logout's hook call repointed at lifecycleHandler)"
tech-stack:
  added: []
  patterns:
    - "Deps-struct constructor injection (continuing 08-03)"
    - "Named function types as narrow cross-cluster seams, in place of a parent backpointer"
    - "Single ordered buildHandlers() so unit construction order cannot be gotten wrong"
key-files:
  created:
    - internal/grpc/command_handler.go
    - internal/grpc/command_handler_test.go
    - internal/grpc/lifecycle_handler.go
    - internal/grpc/lifecycle_handler_test.go
  modified:
    - internal/grpc/server.go
    - internal/grpc/auth_handlers.go
    - internal/grpc/export_test.go
    - internal/grpc/dispatcher_test.go
    - internal/grpc/server_helpers_test.go
    - internal/grpc/server_liveness_test.go
    - internal/grpc/subscribe_server_test.go
decisions:
  - "runDisconnectHooks has THREE consumers, not one: executeViaDispatcher (command), Disconnect (lifecycle), and CoreServer.Logout (auth cluster, staying on the facade). LifecycleHandler is its single owner; the command cluster takes it as a DisconnectHookRunner function value and Logout calls it through s.lifecycleHandler."
  - "The Deps-snapshot pattern silently breaks post-construction field mutation. This was not a hypothetical — it produced a real red test. Resolved with CoreServer.buildHandlers(), which fixtures re-call after poking a collaborator field."
  - "LifecycleHandler.runDisconnectHooks carries a nil-receiver guard. That restores exact pre-split behavior for &CoreServer{} fixtures (which previously ranged over a nil slice); it is not new defensive code. recomputeSessionLiveness deliberately has NO such guard — a nil guard there would silently skip a real state transition."
metrics:
  duration: ~70m
  tasks: 3
  files: 11
  completed: 2026-07-19
---

# Phase 8 Plan 05: Command and Lifecycle Cluster Extraction Summary

Extracted the 4-method command-execution cluster and the 3-method session-lifecycle cluster off
`*CoreServer` into `CommandHandler` and `LifecycleHandler`, both proven constructible from
`package grpc_test` with no `*CoreServer` and no harness. `server.go` fell 1154 → **642** LoC.
`CoreServer.HandleCommand` and `CoreServer.Disconnect` are one-line delegators with byte-identical
signatures, and the exported method set is unchanged at 23.

## What Shipped

| Task | Outcome | Commit |
| --- | --- | --- |
| RED gate (Tasks 1+2, `tdd="true"`) | Both test files fail to compile — handlers do not exist | `359163ffa` |
| 1+2 — Extract both units, delegate from the facade | 7 methods relocated, bodies verified byte-identical | `c57fab30a` |
| 3 — Wave-boundary integration gate | `task test:int` exit 0 (10751 tests), `task lint` exit 0 | (verification only) |

Supporting commit: `202fb3a84` preserves an 08-04 CONTEXT.md amendment found uncommitted in the
working tree (see Deviation 5).

**Tasks 1 and 2 landed in one commit**, following 08-03's deviation 3: Task 1 removes
`HandleCommand`/`Disconnect` from `CoreServer`, which stops it satisfying
`corev1.CoreServiceServer`. Splitting the commits would leave a non-compiling intermediate and
break `git bisect`.

## CRITICAL: the field matrix missed a three-way method-level edge

Per the prior-wave warning, the dependency sets were derived from the **call graph**, not
08-RESEARCH.md's method→field matrix. The matrix records field *access*, so it attributes
`disconnectHooks` to `runDisconnectHooks` alone and sees a clean two-cluster split. The call graph
says otherwise:

| Caller | Cluster | Calls |
| --- | --- | --- |
| `executeViaDispatcher` (4 sites: quit teardown ×2, admin-boot ×2) | **command** | `runDisconnectHooks` |
| `Disconnect` (2 sites) | **lifecycle** | `runDisconnectHooks` |
| `CoreServer.Logout` (`auth_handlers.go:703`) | **auth — stays on the facade** | `runDisconnectHooks` |
| `SubscribeHandler.Subscribe` (08-03's seam) | **subscribe** | `recomputeSessionLiveness` |

Three separate clusters plus an already-extracted sibling depend on lifecycle methods. The plan's
premise that these two clusters are cleanly separable modulo `presence.Emitter` is **incomplete**.

**Resolution** — `LifecycleHandler` is the single owner; nobody duplicates and nobody holds a
backpointer:

- `CommandHandler` takes `DisconnectHookRunner func(ctx, session.Info)`, a named function type in
  the style of 08-03's `SessionLivenessRecomputer`. `newCommandHandler` passes the method value
  `s.lifecycleHandler.runDisconnectHooks`. The handler sees a plain `func` and never learns
  `CoreServer` or `LifecycleHandler` exists — the proof test supplies neither.
- `CoreServer.Logout` calls `s.lifecycleHandler.runDisconnectHooks` directly (one-token change).
- 08-03's `SubscribeDeps.RecomputeLiveness` now binds `s.lifecycleHandler.recomputeSessionLiveness`
  instead of the deleted `CoreServer` method. **This is the interlock with 08-03** and it forces
  the construction order below.

Also invisible to the matrix: the plan's Behavior 2 asserts "`executeCommand` touches zero
CoreServer fields, so it is directly assertable." That is true of `executeCommand`'s own body — it
is a one-line forward — but it transitively reaches `dispatcher`, `cmdServices`, `presence`,
`sessionStore`, `publisher`, and the hook runner. The test was written against the real
(transitive) dependency set rather than the matrix's.

## T-8-02 — the session-ownership preamble survived verbatim on BOTH methods

The high-severity threat. Both preambles were verified **byte-identical** by a mechanical
line-for-line diff of every moved body against `git show origin/main:internal/grpc/server.go`,
normalizing only the receiver (`\bs\.` → `h.`):

```
HandleCommand              IDENTICAL
executeCommand             IDENTICAL
executeViaDispatcher       IDENTICAL
emitCommandResponse        DIFFERS  (one line — see below)
Disconnect                 IDENTICAL
recomputeSessionLiveness   IDENTICAL
runDisconnectHooks         IDENTICAL
```

`auth.ValidateSessionOwnership(ctx, h.playerSessionRepo, h.sessionStore, token, sessionID)` remains
an ordinary free-function statement inside each body, with its `slog.DebugContext` line and its
collapse to the enumeration-safe `"session not found"` response intact. No session-resolver type
was extracted. `test/integration/auth/`, `.../command/`, and `.../session/` are green.

**The single `emitCommandResponse` difference is pre-existing, not this plan's:**

```
-  sub, err := h.toSubject(h.currentGameID(), world.CharacterStream(char.ID))
+  sub, err := qualifyStreamSubject(h.currentGameID(), world.CharacterStream(char.ID))
```

That is 08-03's deviation 2 (`toSubject`'s body became the free function `qualifyStreamSubject`
because this cluster was a second caller). It was already on `HEAD` before this plan. Against
`HEAD~1` the body is byte-identical.

## Nil-collaborator semantics — preserved, and deliberately NOT homogenised

| Collaborator | Semantics | Preserved how |
| --- | --- | --- |
| `publisher` nil | **silent no-op** — return nil, do not dereference | guard moved verbatim; pinned by `...IsSilentNoOpWhenPublisherIsNil` |
| `presence` nil | **panics** (unguarded deref, as before) | no guard added; documented on `CommandDeps.Presence` / `LifecycleDeps.Presence` |
| `runDisconnectHooks` runner nil | **no-op** | `fireDisconnectHooks` wrapper, matching an empty hook list |
| `dispatcher` / `cmdServices` nil | **panic at construction** | `NewCoreServer`'s guard left exactly where it was (T-8-19) |

I explicitly did **not** give `presence` a nil guard to match `publisher`'s. Those two directions
differ in the original and flattening them would be a behavior change with a green build.

Likewise `LifecycleHandler.recomputeSessionLiveness` has **no** nil-receiver guard even though
`runDisconnectHooks` does — a nil guard there would silently skip a real liveness state transition
rather than restore prior behavior. See Deviation 2.

## Contract invariants — all verified

| Check | Result |
| --- | --- |
| Exported `CoreServer` method set pre vs post | **identical** (23 methods, diffed against `origin/main`) |
| `git diff --stat origin/main...HEAD -- api/proto/ pkg/proto/ internal/grpc/mocks/` | **empty** |
| `git diff --stat origin/main...HEAD -- test/integration/` | **empty — zero assertion churn (D-15)** |
| `cmd/holomush/sub_grpc.go` / `integrationtest/harness.go` | untouched by this plan (the 4/2 delta vs `origin/main` is 08-02's `78a3e6e9a`) |
| `CoreServer.HandleCommand` body | 1 line |
| `CoreServer.Disconnect` body | 1 line |
| `rg -n 'CoreServer'` in all 4 new files, comments excluded | **NONE** — no backpointer |
| `wc -l internal/grpc/server.go` | **642** (from 1154; feeds Wave C ceiling calibration) |

`presence.Emitter` is passed as the **same value** into both `NewCommandHandler` and
`NewLifecycleHandler` — no clone, no wrapper, no re-derivation. It is an emitter, not shared
mutable state, so there is nothing to coordinate (the plan's canonical shared-collaborator case).

## D-02 / SC1: the bar was met

Both proof tests are `package grpc_test` and construct their unit from mocks and literals only:

- `TestNewCommandHandlerIsConstructibleWithOnlyItsOwnCollaborators`
- `TestNewLifecycleHandlerIsConstructibleWithOnlyItsOwnCollaborators`

Neither file constructs a `*CoreServer`, imports `integrationtest`, or carries
`//go:build integration`. The plan's acceptance grep
(`rg -c 'CoreServer|integrationtest|go:build integration'` returns 0) reports matches in both
files — every one is **prose in a doc comment** describing the seam ("no `*CoreServer`, no
integrationtest harness"). Same grep artifact 08-03 and 08-04 recorded. The structural property
holds under the comment-excluding form, which returns NONE.

Nine tests total: 6 command-side (constructibility, exact-count publish, error-type selection,
nil-publisher no-op, the full `executeCommand → executeViaDispatcher → emitCommandResponse` chain,
malformed-connection-id rejection) and 4 lifecycle-side.

**T-8-17** is pinned by `require.Len(t, pub.events, 1)` on a counting publisher — a double-publish
or a dropped publish fails on the count, not on presence. **T-8-18** is pinned by
`assert.Equal(t, []string{"first","second","third"}, order)` — sequence, not membership — plus a
panicking-hook test proving recovery does not skip successors.

## Deviations from Plan

### 1. [Rule 3 — Blocking] `runDisconnectHooks` has three consumers, and `Logout` is one

Fully described above. The plan scoped the shared-collaborator problem to `presence.Emitter` and
did not anticipate a shared *method*. `auth_handlers.go` needed a one-token repoint.

### 2. [Rule 1 — Bug] The Deps-snapshot pattern broke a real test, twice

Two distinct red failures, both genuine defects introduced by snapshotting collaborators at
construction:

**(a) Nil `*LifecycleHandler` on fixtures that bypass `NewCoreServer`.**
`TestLogoutEmitsSessionEndedForEachChildGameSession` builds a bare `&CoreServer{...}` literal, so
`lifecycleHandler` was nil and `Logout` faulted where it previously no-opped over a nil
`disconnectHooks` slice. Fixed with a **nil-receiver guard on `runDisconnectHooks` only**, which
restores the exact pre-split observable behavior. I did **not** add the same guard to
`recomputeSessionLiveness`: pre-split, `&CoreServer{sessionStore: store}.recomputeSessionLiveness`
did real work, so a nil guard would silently skip a state transition — that would be papering over,
not preserving.

**(b) Stale snapshot after a post-construction field poke.**
`TestDisconnectGatesGuestCleanupOnRemovalSignal` swaps `server.sessionStore` for a wrapper *after*
construction to isolate the `holomush-cizj` duplicate-disconnect gate. Pre-split, `Disconnect` read
`s.sessionStore` live and the swap took effect; post-split the handler held the original store, and
the test went red (`expected 1, actual 0` session_ended events).

This is the most important finding for 08-06/08-07/08-08: **the Deps-struct pattern silently
decouples the facade's fields from the extracted units.** Resolution is `CoreServer.buildHandlers()`
— one ordered constructor for all three units — called by `NewCoreServer` and re-called by any
fixture that mutates a collaborator field. The 10 fixtures in `subscribe_server_test.go` that used
08-03's `s.subscribeHandler = s.newSubscribeHandler()` now call `s.buildHandlers()`, which also
removes their exposure to the nil-lifecycle fault in (a).

**No assertion was edited** in any of these — only constructors and one store-swap line. All are
`package grpc` files outside `test/integration/`, so D-15 does not govern them; `test/integration/`
remains byte-empty in the diff.

### 3. Method counts are 6 and 3, not the plan's 4 and 3

`rg -c 'func \(h \*CommandHandler\)'` reports **6**: the four planned methods plus `currentGameID`
(needed by `emitCommandResponse`, mirroring `SubscribeHandler.currentGameID`) and
`fireDisconnectHooks` (the nil-guarding wrapper for the injected runner). `LifecycleHandler` reports
exactly **3** as planned. Same class of seam-wrapper overcount 08-03 recorded (10 vs 8).

### 4. `isUserFacingError` moved with the cluster

The plan lists four methods; `isUserFacingError` is a package-level free function whose only caller
is `executeViaDispatcher` (verified: 1 call site). Leaving it in `server.go` would have stranded a
command-cluster helper in the facade. Moved to `command_handler.go` unchanged.

### 5. An uncommitted 08-04 CONTEXT.md amendment was found and preserved

`08-CONTEXT.md` carried an uncommitted 15-line block — 08-04's correction that the D-06 lock-split
safety argument is scoped to `loadPlugin`, not global. It was left out of 08-04's closing docs
commit. Committed separately as `202fb3a84` so 08-06 and 08-08 do not lose it. Not a code change
and not part of this plan's diff.

## Verification

| Gate | Exit |
| --- | --- |
| `task build` | 0 |
| `task test -- ./internal/grpc/...` | 0 (615 tests) |
| `task test -- ./internal/grpc/... ./cmd/holomush/... ./internal/testsupport/...` | 0 (1171 tests, 1 skipped) |
| `task lint` | 0 |
| `task test:int` | 0 (**10751 tests**, 7 skipped/quarantined) |
| `task fmt` | no residual diff |
| `task license:check` (via `task fmt`) | 0 invalid across 3784 files |

Named downstream suites, all green:

```
✓  test/integration/command    ✓  test/integration/session (3.724s)
✓  test/integration/presence (7.055s)   ✓  test/integration/auth
✓  test/integration/wholesystem (5.861s)   ✓  test/integration/scenes (1m38.035s)
```

`test/integration/phase1_5_test.go`'s three `NewCoreServer` constructions on the command-execution
path pass unmodified.

`git diff --stat origin/main...HEAD -- test/integration/` → **empty**.

**Finding 4 (invariant provenance) did not fire this time.** The moved bodies carry no
`INV-<SCOPE>-N` token — `INV-SCENE-62` sits on the `sceneMute` struct-field comment, which belongs
to the subscribe cluster and did not move — and `invariants.yaml` holds no ref to
`internal/grpc/server.go`. Checked before the move; `TestProvenanceGuard` confirms.

## Known Stubs

None. Both handlers are fully wired; every nil collaborator is an intentional, documented,
test-pinned configuration.

## Threat Flags

None. No new network endpoint, auth path, file-access pattern, or schema change — this plan
relocated existing code and its existing gates.

## Notes for Wave C

- `server.go` is **642 LoC** post-plan (1891 → 1154 → 642). The query cluster (08-07) is the last
  extraction before the ratchet ceiling can be calibrated.
- `command_handler.go` is 417 LoC and `lifecycle_handler.go` 386 — both are natural ceiling entries.
- **`CoreServer.buildHandlers()` is the extension point for 08-07.** Add the query unit there
  rather than appending another line to `NewCoreServer`; the ordering constraint is real.
- `export_test.go` grew three shims (`ExportEmitCommandResponse`, `ExportExecuteCommand`,
  `ExportRunDisconnectHooks`), all with `ctx` first per 08-03's revive fix.
- `replayCompleteFrame`, `streamClosedFrame`, `subscribeSessionNotFound`, `filterSetToSlice`, and
  now `actorIDString` are used only by `subscribe_handler.go` in production but still live in
  `server.go`. Relocating them remains deferred (`deferred-items.md`).

## Self-Check: PASSED

Created files exist:
- `internal/grpc/command_handler.go` — FOUND
- `internal/grpc/command_handler_test.go` — FOUND
- `internal/grpc/lifecycle_handler.go` — FOUND
- `internal/grpc/lifecycle_handler_test.go` — FOUND

Commits exist on `gsd/phase-08-god-object-decomposition`:
- `359163ffa` test(08-05): add failing tests for the command and lifecycle seams — FOUND
- `202fb3a84` docs(08-cont): record 08-04's scoped D-06 lock-split correction — FOUND
- `c57fab30a` refactor(08-05): extract CommandHandler and LifecycleHandler from CoreServer — FOUND
