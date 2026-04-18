# Session Lifecycle as Events — Design Spec

**Status:** Draft
**Bead:** `holomush-9es6`
**Author:** Sean Brandt + Claude Opus 4.7 (1M context)
**Date:** 2026-04-18
**Discovered from:** PR #227 (`fix/events-immutable-metatest`) CI failure surfaced a pre-existing flake in `test/integration/telnet/e2e_test.go:548` — "Player A disconnects cleanly via quit"

---

## Problem Statement

The Telnet E2E test "Player A disconnects cleanly via quit" flakes in CI. Under investigation, this is not a test-timing flake; it is a race condition in the server's Subscribe loop where two independent signals — the last data-plane event and the session-lifecycle control signal — have non-deterministic ordering. On slow runners the ordering inverts and the client sees the character-selection prompt instead of the promised `"Goodbye!"` terminal message.

The race is not a bug in any single component. It is a consequence of treating game state and session lifecycle as two separate planes with separate delivery mechanisms. The project's event-sourcing invariant (*"All game actions produce events. Events are immutable and ordered. State is derived from event replay."*) applies to game state but not to session lifecycle. This spec brings session lifecycle into the event-sourced plane and eliminates the race by design.

This work MUST ship before any further feature work that depends on reliable session termination, including PR #227 re-merge and the `holomush-brlb` IP rate limiting work.

---

## Background

### The three-axis lifecycle model (already in place)

The codebase already distinguishes three orthogonal lifecycle concerns. This spec does not change that; it only fixes how *session* transitions are signaled.

| Axis | What it models | Primary signals today | Event-sourced? |
|---|---|---|---|
| **Session** | A character's "play instance" — at most one active session per character (`FindByCharacter` reattach invariant) | `sessionStore.Set` / `sessionStore.Delete` + `WatchSession` channel | **No** (the bug) |
| **Connection** | One wire from one surface — `client_type ∈ {terminal, comms_hub, telnet}` | `sessionStore.AddConnection` / `sessionStore.RemoveConnection`; `Disconnect(connID)` RPC | No, but no race (each wire's close is in-band via gRPC stream close) |
| **Grid presence** | Whether a character is visible on the spatial grid (triggers `arrive`/`leave`/`phase_out` visible to peers) | `engine.HandleDisconnect` emits `leave` event on location stream | **Yes** (correct — this is a world state change visible to peers) |

**Focus** (currently `FOCUS_KIND_SCENE` only) is a *subscription preference* layered on top of a session's Subscribe call. It is not a lifecycle concern and is out of scope for this spec.

### Current quit-teardown flow

When a character issues `quit`:

1. `dispatcher.Dispatch("quit")` returns `command.ErrSessionEnded`.
2. `engine.HandleDisconnect(ctx, char, "quit")` appends a `leave` event to the **location** stream (`internal/core/engine.go:126`).
3. `sessionStore.Delete(ctx, sessionID, "Goodbye!")` removes the session row and pushes `Event{Type: Destroyed, Message: "Goodbye!"}` onto every registered `WatchSession` channel, then closes each watcher channel (`internal/session/memstore.go:60`; same in `internal/store/session_store.go:316`).
4. `HandleCommand` returns success.

On a separate goroutine, the session's Subscribe handler (`internal/grpc/server.go:883`) is running a select loop:

```go
for {
    select {
    case <-ctx.Done():               // client disconnect
    case subLoopErr := <-sub.Errors(): ...
    case notif := <-sub.Notifications():
        // replayAndSend — data plane
    case ev, ok := <-sessionCh:
        if ev.Type == session.Destroyed {
            _ = stream.Send(streamClosedFrame(ev.Message))   // "Goodbye!"
            return nil
        }
    case ctrl, ok := <-ctrlCh: ...
    }
}
```

Go's `select` is intentionally non-deterministic. After a quit:

- **`sub.Notifications()`** fires for the newly-appended `leave` event.
- **`sessionCh`** fires for the `Destroyed` signal.

Both are ready simultaneously. If `Notifications` wins, `replayAndSend` runs → under load this can take tens or hundreds of milliseconds per event, and there may be several events to drain. On the client, `drainUntilClosed` (telnet gateway, `internal/telnet/gateway_handler.go:708`) has a 2-second budget. When it expires, the client silently falls through to `showCharacterList()`, the user sees `"Your characters:"` instead of `"Goodbye!"`, and the test fails.

### Why this is an architectural defect, not a test-timing issue

The tests are correctly detecting a contract violation: the server promises `"Goodbye!"` on quit, but does not reliably deliver it under realistic load. Papering over the test (retries, longer timeouts, client-side fallbacks) would be covering up a real defect.

The defect is structural: there is no invariant that forces the `Destroyed` signal to arrive *after* the last data-plane event. Any fix that preserves the two-plane architecture can only narrow the race window, not close it. The only fix that closes the window is unifying the two planes.

---

## Design

### Core principle

**Session lifecycle is state; state changes are events.** Every other state change in the system produces an event (`arrive`, `leave`, `say`, `pose`, `object_create`, ...). Session creation and termination will do the same.

### New event type

Add `EventTypeSessionEnded` to `internal/core/event.go`:

```go
EventTypeSessionEnded EventType = "session_ended"
```

Emitted on the character's own stream: `character:{ID}`.

Payload (JSON):

```go
type SessionEndedPayload struct {
    SessionID string `json:"session_id"`     // ULID of the ended session
    Cause     string `json:"cause"`          // "quit" | "logout" | "guest_end" | "kicked" | "reaped" | "evicted"
    Reason    string `json:"reason"`         // human-readable, delivered to client as STREAM_CLOSED message
}
```

`Actor` is `{Kind: ActorSystem}` with the character ID in `Actor.ID` — session termination can be system-initiated (reap, evict) as well as character-initiated (quit). The payload `SessionID` is authoritative for correlation.

### New engine method

```go
// EndSession emits a session_ended event on the character's own stream.
// It does NOT delete the session — callers MUST still call
// sessionStore.Delete (or detach) after EndSession returns. EndSession is
// responsible only for producing the terminal event; storage lifecycle is
// orthogonal.
func (e *Engine) EndSession(
    ctx context.Context,
    char CharacterRef,
    sessionID string,
    cause string,
    reason string,
) error
```

Implementation mirrors `HandleDisconnect`: marshal payload, construct event with `NewULID()` (satisfies `EventIDMustBeMonotonic` ruleguard), append to `character:{ID}` stream.

### Subscribe loop changes

Remove the `WatchSession` / `sessionCh` path from `internal/grpc/server.go` Subscribe:

- Delete `sessionCh, watchErr := s.sessionStore.WatchSession(ctx, req.SessionId)` (line 876).
- Delete the `case ev, ok := <-sessionCh:` arm (lines 905-913).

Handle `session_ended` inside the notification-processing path. `sendAndCommitEvent` becomes aware that `EventTypeSessionEnded` with matching `SessionID` is terminal:

```go
// Inside sendAndCommitEvent, after grpcStream.Send succeeds:
if ev.Type == core.EventTypeSessionEnded {
    var payload SessionEndedPayload
    if err := json.Unmarshal(ev.Payload, &payload); err == nil {
        if payload.SessionID == info.ID {
            // Terminal: send STREAM_CLOSED carrying the reason, then return
            // a sentinel error that Subscribe recognizes as graceful termination.
            _ = grpcStream.Send(streamClosedFrame(payload.Reason))
            return errStreamTerminated  // new sentinel
        }
    }
}
```

The caller in `replayAndSend` (`internal/grpc/server.go:585`) propagates the sentinel; Subscribe's top-level loop recognizes it and returns `nil` (graceful close).

### Quit flow (Path 1, session-level)

Rewrite the quit path in `internal/grpc/server.go:407-416`:

```go
// Before:
if errors.Is(dispatchErr, command.ErrSessionEnded) {
    if dcErr := s.engine.HandleDisconnect(ctx, char, "quit"); dcErr != nil { ... }
    if delErr := s.sessionStore.Delete(ctx, info.ID, "Goodbye!"); delErr != nil { ... }
    s.runDisconnectHooks(ctx, *info)
    return nil
}

// After:
if errors.Is(dispatchErr, command.ErrSessionEnded) {
    if dcErr := s.engine.HandleDisconnect(ctx, char, "quit"); dcErr != nil { ... }
    if endErr := s.engine.EndSession(ctx, char, info.ID, "quit", "Goodbye!"); endErr != nil { ... }
    if delErr := s.sessionStore.Delete(ctx, info.ID); delErr != nil { ... }  // no reason — storage only
    s.runDisconnectHooks(ctx, *info)
    return nil
}
```

Ordering: `HandleDisconnect` (leave on location) → `EndSession` (session_ended on character) → `sessionStore.Delete`. The event-store append in `EndSession` happens-before the Subscribe notification for that event, by `SubscribeSession`'s ordering guarantees.

### Other session-terminating paths

Apply the same pattern everywhere `sessionStore.Delete(ctx, id, reason)` is currently called:

| Site | Current reason | New cause/reason |
|---|---|---|
| `internal/grpc/server.go:411` (quit handler) | `"Goodbye!"` | cause=`"quit"`, reason=`"Goodbye!"` |
| `internal/grpc/server.go:1041` (guest disconnect) | `"Guest session ended"` | cause=`"guest_end"`, reason=`"Session ended."` |
| `cmd/holomush/sub_grpc.go:287-301` (session reaper `OnExpired`) | Emits leave event via `HandleDisconnect`; session row removed by the reaper's own lifecycle logic. **Does not currently emit any session-termination signal to Subscribers — a latent defect Option D also fixes.** | cause=`"reaped"`, reason=`"Session expired due to inactivity."` |
| Admin boot path (`internal/grpc/server.go:457`) | `"booted"` via `HandleDisconnect` only | cause=`"kicked"`, reason=`"You have been disconnected by an administrator."` |
| **PlayerSession eviction** (11th login evicts oldest, `internal/auth/auth_service.go:71-141`) | Evicts the *auth* session, not directly the game session. Cascade to game sessions is via subsequent token-invalidation rather than explicit termination signal. Open question (#5 below) — implementation MUST decide whether a PlayerSession evict also emits `session_ended` on each game session whose token was invalidated. | cause=`"evicted"`, reason=`"Session evicted — you logged in elsewhere."` |

### `sessionStore.Delete` signature change

Remove the `reason string` parameter — it's no longer used for signaling. Signature becomes:

```go
Delete(ctx context.Context, sessionID string) error
```

This forces every caller to move the reason to the `EndSession` call, making the architectural invariant (session_ended MUST be emitted before Delete) explicit at the API boundary.

### Dead code removal

After cutover, the following become unused and MUST be deleted:

- `internal/session/session.go`: `WatchSession` method on `Store` interface; `Event` type; `EventType`; `Destroyed` constant
- `internal/session/memstore.go`: `WatchSession`, `watchers` map, and Delete's watcher-notification block
- `internal/store/session_store.go`: same (Postgres implementation)
- `internal/session/mocks/mock_Store.go`: regenerate mocks after interface change
- Tests: `TestMemStore_WatchSession_*`, `TestMemStore_WatchSession_ChannelClosedOnDelete`, etc.

### Regression test

A new integration test in `test/integration/telnet/e2e_test.go` (or a new file) that:

1. Connects a guest, captures their character name.
2. Sends `quit`.
3. Asserts `"Goodbye!"` is received within 5 seconds.
4. Queries the event store and asserts exactly one `session_ended` event exists on `character:{ID}` with matching `SessionID` and `cause="quit"`.

The existing `"Player A disconnects cleanly via quit"` test will be promoted from flaky to reliable; this new test adds the audit-trail assertion as a guardrail against regressions in the event-emission path.

---

## Command Semantics (Path 1)

The architectural change unlocks clean per-command semantics. Document the intended command palette explicitly so future contributors (and players) share a model.

| Intent | Command | Primitive | Event(s) emitted | Effect |
|---|---|---|---|---|
| Close this surface, keep playing elsewhere | `disconnect` (new telnet command; web UI tab close) | `Disconnect(connectionID)` RPC | none from session lifecycle (event-stream close is in-band); optional `phase_out` leave if grid count hits zero | That one wire dies; session continues; grid-present flips if this was the last grid connection |
| End this play session | `quit` | `engine.EndSession` + `sessionStore.Delete` | `leave` on location; `session_ended` on character | All surfaces drop; player remains authenticated, returns to character selection |
| Sign out of account | `logout` | Destroys `PlayerSession` → cascades to all character sessions | For each character session: `leave` + `session_ended` | All surfaces of all this player's characters drop; player must re-authenticate |

### Multi-surface quit friction

When a player issues `quit` and their session has more than one connection, the server MUST NOT immediately tear down. Instead:

1. First `quit` with `> 1` connections: emit a `command_response` event on the character stream with body: *`"You have N other surfaces connected. Typing QUIT again will end the session for all of them. Type DISCONNECT to close only this surface."`* Set a session-scoped flag `pendingQuitConfirmation` with a TTL of e.g. 30 seconds.
2. Second `quit` within the TTL: proceed with the full quit path.
3. Any other command, or TTL expiry: clear the flag. A subsequent `quit` re-enters step 1.

The flag lives on `session.Info` (new field `PendingQuitConfirmationUntil *time.Time`) so it's observable across surfaces. The confirmation prompt is delivered as an event, so all surfaces see it — the player can confirm on any surface. This keeps the UX coherent across surfaces while preserving the session-level semantic.

The `disconnect` command (new) is the per-surface escape hatch.

### Deferred work

- The new `disconnect` telnet command and corresponding web UI affordance.
- Rich-web-ui session-management UX (e.g. "Connected surfaces" panel showing device types, with a "Close this device" button per row).
- Confirmation prompt wording and i18n.

These are UX surface-area items, separately beaded and not blocking this architecture change. This spec establishes the primitive (`session_ended` event + `Disconnect(connID)` RPC) that those features build on.

---

## Testing Strategy

### Unit tests
- `engine.EndSession` appends a correctly-shaped event to the character stream.
- `sendAndCommitEvent` recognizes `session_ended` with matching `SessionID` as terminal, sends STREAM_CLOSED, returns `errStreamTerminated`.
- `sendAndCommitEvent` ignores `session_ended` with *non-matching* `SessionID` (replay scenario).

### Integration tests (Ginkgo/Gomega BDD per project convention)
- Flake-proof version of "Player A disconnects cleanly via quit": asserts `Goodbye!` + `session_ended` event persisted.
- Multi-surface fan-out: open telnet + a second connection to the same session (via Subscribe with the same sessionID? — verify feasibility during implementation); assert both receive STREAM_CLOSED on quit.
- Replay isolation: quit, reconnect same character, assert new Subscribe does NOT self-terminate from the prior `session_ended`.
- Multi-surface quit confirmation: first `quit` produces confirmation prompt (no session_ended yet); second `quit` proceeds.

### Ruleguard
Ensure `EventIDMustBeMonotonic` passes — `EndSession` uses `core.NewULID()` (not `idgen.New()`).

### PR gate
`task pr-prep` MUST pass. The flaky test's fix is the proof of correctness; there MUST NOT be a retry shim, timeout extension, or client-side fallback introduced as part of this work.

---

## Rejected Alternatives

### A. Synchronous drain on Destroyed in Subscribe (band-aid)
When `sessionCh` fires, drain pending `sub.Notifications()` non-blockingly before sending STREAM_CLOSED. Narrows the race window but does not close it — events appended just before Delete may not have been notified yet. Preserves the two-plane architecture and its fundamental defect.

### B. Terminal marker in the event stream, separate from `session_ended`
Append a synthetic "end-of-stream" marker that Subscribe treats as terminal. Halfway measure: invents a new fake-event concept just for stream termination, does not address the broader gap that session state is not event-sourced. Adds complexity without full payoff.

### C. Priority select in Subscribe
Go does not have priority select. Nested selects with non-blocking reads can approximate it but yield the *wrong* ordering (processing Destroyed *before* draining pending data events). Incorrect.

### Path 2: Per-surface sessions (rejected during brainstorming)
Each surface gets its own session. `quit` on telnet ends only that telnet's session; web-ui keeps running. Breaks load-bearing invariants:
- `FindByCharacter` returns the one session for a character — the grid presence model depends on this.
- Peers see exactly one `arrive`, one set of events per character. Per-surface sessions would fan out to N sessions per character, requiring dedupe everywhere.
- Session cap (PR #225: "11th login evicts oldest") is ambiguous — what is "a session" now?
- Plugin system's per-session state (e.g., focus memberships) would need per-surface replication.

The "logged in on two devices" mental model is accommodated via the surface-level `disconnect` command and the multi-surface `quit` confirmation prompt, without fragmenting the session model.

### Client-side fallback "Goodbye!"
Send `Goodbye!` from the telnet handler itself when `drainUntilClosed` times out, even if no STREAM_CLOSED was received. Hides a server-side contract violation behind gateway UX glue. Rejected: the server-side contract is what needs fixing, not the gateway's display logic.

---

## Open Questions and Assumptions to Verify During Implementation

Each is a concrete question that the implementation plan MUST answer before proceeding past the relevant task:

1. **Actor convention for system-initiated events.** Is `Actor{Kind: ActorSystem, ID: characterID}` the right shape, or does the codebase have a stronger convention? Check existing system events.
2. **Postgres event store notification latency.** `SubscribeSession` is the "strict cross-stream ordering via single PG connection" variant (`internal/grpc/server.go:819`). Verify that an `Append` completes before its corresponding `Notification` fires on the same connection. If yes, ordering is guaranteed within a single Subscribe. If no, we have a different race to address.
3. **Does `runDisconnectHooks` need to move?** Currently runs after `sessionStore.Delete`; needs to continue running once per session termination. Confirm hook observers do not depend on `WatchSession`.
4. **Replay cursor semantics.** A replayed `session_ended` with non-matching `SessionID` must still advance the cursor to avoid re-delivery. Verify `sendAndCommitEvent` updates the cursor in all paths, including the new "ignore and move on" path.
5. **`bd` — eviction path.** Verify where "11th login evicts oldest" lives and apply the same `EndSession` treatment. This closes PR #225's eviction path observability gap.
6. **Backwards compatibility.** `session.Event` and `WatchSession` are not public API, but mocks for them are used in tests. Deleting the interface method is a breaking change for any external consumer; inspect `pkg/` to confirm no public export.
7. **Multi-surface quit-confirmation flag storage.** `session.Info.PendingQuitConfirmationUntil` adds a new field. Does Postgres session store need a migration, or is this purely in-memory runtime state? Design implication: if in-memory only, restart clears the flag (benign — user re-issues `quit`). Recommend in-memory only.

---

## Out of Scope for This Spec

- The `disconnect` telnet command and its web-UI counterpart (own bead; can land after or alongside).
- Rich-web-ui "Connected surfaces" management UI.
- Per-surface focus state (currently all surfaces share session focus; changing that is a different architectural discussion).
- Postgres session store TTL reaper refactor (separate work).
- The pre-existing flake UX (a flakey `drainUntilClosed` *should* log WARN; propose a tiny observability-only follow-up after Option D lands, because post-Option-D the drain should never time out and any such timeout is a signal of a different bug).

---

## References

- Bead: `holomush-9es6`
- Flaky test: `test/integration/telnet/e2e_test.go:548` "Player A disconnects cleanly via quit"
- Current Subscribe loop: `internal/grpc/server.go:883` (select with racing channels)
- Event-sourcing invariant: `CLAUDE.md` "Event Sourcing" section
- Three-axis lifecycle: `internal/grpc/server.go:939` (Disconnect RPC comment "phases out" semantics)
- Focus system (orthogonal): `api/proto/holomush/plugin/v1/plugin.proto:320` (FocusKind)
- PR #225 session identity work: `holomush-abbg` and follow-ups
