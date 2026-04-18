# Session Lifecycle as Events — Design Spec

**Status:** Draft (revised after architect review 2026-04-18)
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

## Load-Bearing Invariants

Option D's correctness rests on three project invariants. Implementation MUST NOT erode them.

### I1 — EventWriter serializes all appends

All event appends in production go through `EventWriter` (`internal/core/event_writer.go`), wired in `cmd/holomush/sub_grpc.go`. EventWriter runs a single-goroutine serialization path, stamps a monotonic ULID at write-time (I-14 / I-16), and performs `INSERT + pg_notify` as a unit. This is what makes append-order equal notification-order across all appenders.

**Implication for this work:** `engine.EndSession` MUST append through the same path as `engine.HandleDisconnect` — i.e., via the store the engine was constructed with, which in production is the EventWriter-wrapped `PostgresEventStore`. Any construction of `engine.Engine` with a raw store in production would silently break ordering and re-introduce a different race.

**Guardrail:** add either a type-level constraint (change `NewEngine`'s parameter type to `*EventWriter`) OR a startup assertion via concrete type check `_, ok := store.(*core.EventWriter)`. An interface-conformance check is insufficient because `*EventWriter` implements `EventStore` by wrapping — raw stores also satisfy the interface. The plan MUST pick one (see Design Decision #9).

### I2 — SubscribeSession preserves append-order per connection

`SubscribeSession` (`internal/grpc/server.go:819`) is "Variant A — strict cross-stream ordering via single PG connection." A single PostgreSQL session consuming LISTEN/NOTIFY receives notifications in commit order (PG documented guarantee). Combined with I1's serialized commits, notifications arrive in the exact order `EventWriter` wrote them.

**Implication:** once `EndSession` returns successfully, its `session_ended` event is guaranteed to be the LAST notification delivered to the owning Subscribe before any later event on any subscribed stream. No drain loop, no timeout, no fallback needed — ordering is provided by the infrastructure.

**Guardrail:** the existing Variant A integration test at `internal/store/postgres_integration_test.go:575` proves cross-stream ordering. The plan MUST add an analogous test covering character-stream + location-stream interleaving with `session_ended` as the terminal event.

### I3 — `FindByCharacter` reattach: one character, one session

`sessionStore.FindByCharacter(charID)` returns a single active-or-detached session per character (`internal/session/memstore.go:94-107`). `SelectCharacter` reattaches rather than forking. This invariant is load-bearing for:
- Grid presence (one `arrive`/`leave`/position per character)
- Peers' view of a character (one set of events, no dedupe needed)
- The session cap (PR #225: "11th login evicts oldest" — counts one per character)
- Plugin per-session state (one scope per character)

**Implication:** `session_ended` on the character's stream identifies *that* session by ULID in the payload. Any past `session_ended` event in replay history belongs to a prior session lifecycle and MUST NOT self-terminate a new Subscribe for the same character. This is the replay-filter design decision in § Design Decisions #3.

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
    SessionID   string `json:"session_id"`     // ULID of the ended session
    CharacterID string `json:"character_id"`   // ULID of the character
    Cause       string `json:"cause"`          // quit|logout|guest_end|kicked|reaped|evicted
    Reason      string `json:"reason"`         // human-readable; delivered to client as STREAM_CLOSED message
}
```

**Actor convention** (per Design Decision #1 below):
- `cause="quit"` → `Actor{Kind: ActorCharacter, ID: char.ID}` (character-initiated)
- All other causes (`logout`, `guest_end`, `kicked`, `reaped`, `evicted`) → `Actor{Kind: ActorSystem, ID: "system"}` (system-initiated). The character reference lives in the payload's `CharacterID`.

### New engine method

```go
// EndSession emits a session_ended event on the character's own stream.
// It does NOT delete the session — callers MUST still call
// sessionStore.Delete after EndSession returns. EndSession is responsible
// only for producing the terminal event; storage lifecycle is orthogonal.
func (e *Engine) EndSession(
    ctx context.Context,
    char CharacterRef,
    sessionID string,
    cause string,
    reason string,
) error
```

Implementation mirrors `HandleDisconnect`: marshal payload, construct event via `core.NewEvent` (stamps a monotonic ULID through `core.NewULID`, satisfying `EventIDMustBeMonotonic`), append to `character:{ID}` stream via the EventWriter-wrapped store (I1).

### Subscribe loop changes

Remove the `WatchSession` / `sessionCh` path from `internal/grpc/server.go` Subscribe:

- Delete `sessionCh, watchErr := s.sessionStore.WatchSession(ctx, req.SessionId)` (line 876).
- Delete the `case ev, ok := <-sessionCh:` arm (lines 905-913).

Handle `session_ended` inside the notification-processing path. `sendAndCommitEvent` always forwards the event and advances the cursor; after those side effects, if the event is `session_ended` with matching `SessionID`, it emits STREAM_CLOSED and returns a sentinel:

```go
// Inside sendAndCommitEvent, after Send + UpdateCursors succeed:
if ev.Type == core.EventTypeSessionEnded {
    var payload core.SessionEndedPayload
    if err := json.Unmarshal(ev.Payload, &payload); err == nil && payload.SessionID == info.ID {
        _ = grpcStream.Send(streamClosedFrame(payload.Reason))
        return errStreamTerminated
    }
}
return nil
```

### Sentinel propagation

Introduce `errStreamTerminated` in `internal/grpc` (unexported — not a public contract):

```go
// errStreamTerminated signals graceful Subscribe termination after a
// matching session_ended event. The live loop translates this to
// `return nil` (clean gRPC close).
var errStreamTerminated = errors.New("stream terminated by session_ended")
```

`replayAndSend` (`internal/grpc/server.go:585`) propagates the sentinel unchanged. The live loop's error arm at `:899-903` currently wraps any `sendErr` with `oops.Code("SEND_FAILED")`; this MUST be updated to short-circuit on the sentinel:

```go
case notif := <-sub.Notifications():
    // ...
    last, sendErr := s.replayAndSend(ctx, info, notif.Stream, cursor, stream, lf)
    if errors.Is(sendErr, errStreamTerminated) {
        return nil
    }
    if sendErr != nil {
        return oops.Code("SEND_FAILED").With("session_id", req.SessionId).Wrap(sendErr)
    }
```

Same treatment in `replayRestorePlan` callers.

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
    if delErr := s.sessionStore.Delete(ctx, info.ID); delErr != nil { ... }  // storage only
    s.runDisconnectHooks(ctx, *info)
    return nil
}
```

Ordering: `HandleDisconnect` (leave on location) → `EndSession` (session_ended on character) → `sessionStore.Delete`. Under I1 + I2, the event-store appends are serialized and the Subscribe receives notifications in append order; the `session_ended` is the last frame any subscribed surface sees before STREAM_CLOSED.

### Other session-terminating paths

Apply the same pattern everywhere `sessionStore.Delete(ctx, id, reason)` is currently called:

| Site | Current behavior | Target cause / reason |
|---|---|---|
| `internal/grpc/server.go:411` (quit handler) | `HandleDisconnect`(leave) + `Delete(id,"Goodbye!")` → `WatchSession` fires | `cause="quit"`, `reason="Goodbye!"` |
| `internal/grpc/server.go:1041` (Disconnect RPC — all-connections-gone, guest session) | `HandleDisconnect`(leave) + `Delete(id,"Guest session ended")` | `cause="guest_end"`, `reason="Session ended."` |
| `internal/session/reaper.go:82` (detached-session reaper) | `HandleDisconnect`(leave) + `Delete(id,"Session expired...")` — the reaper calls Delete, so `WatchSession` fires. **But reaper operates on `StatusDetached` sessions, which by definition have no live Subscribe: the signal has no listener.** Option D makes this observable via the persisted event. | `cause="reaped"`, `reason="Session expired due to inactivity."` |
| Admin boot path (`internal/grpc/server.go:457`, `HandleDisconnect(ctx, booted, "booted")`) | Emits leave event only. Session deletion happens via the boot RPC handler — verify exact flow during implementation. | `cause="kicked"`, `reason="You have been disconnected by an administrator."` |
| **PlayerSession eviction** (11th login evicts oldest, `internal/auth/auth_service.go:89-149`) | Today: evicts `player_sessions` rows via `CreateWithCap`'s trimming logic. FK cascade (`migrations/000008_session_player_fk.up.sql`) removes child game `sessions` rows. **No Go code iterates child game sessions — no leave, no session_ended, no hooks.** Option D does NOT fix this automatically. | Requires a new task (see § Logout & eviction fanout) — `cause="evicted"`, `reason="Session evicted — you logged in elsewhere."` |

### Logout & eviction fanout (new)

`Logout` (RPC `internal/grpc/auth_handlers.go:441`, service `internal/auth/auth_service.go:151`) today deletes the `player_sessions` row and relies on FK cascade to reap child game sessions. Any live Subscribe on those game sessions currently orphans until ctx cancels. Same story for PlayerSession eviction during login cap enforcement.

Both paths MUST, before invoking the PlayerSession-level deletion:

1. Enumerate active game sessions associated with the target PlayerSession(s). This is a new query — `sessionStore.ListByPlayerSession(ctx, playerSessionID []ulid.ULID) ([]*Info, error)` — or we piggyback on the existing `sessionStore.ListByPlayer` filtered by `PlayerSessionID`. Implementation chooses the less invasive option.
2. For each active game session, emit:
   - `engine.HandleDisconnect(ctx, char, <cause>)` — leave event on location stream, visible to peers
   - `engine.EndSession(ctx, char, sessionID, cause, reason)` — terminal event on character stream
   - `sessionStore.Delete(ctx, sessionID)` — remove row (FK will cascade but we want the Go-side signals first)
   - `runDisconnectHooks(ctx, *info)` — plugin cleanup
3. Only then invoke the PlayerSession-level deletion (`authService.Logout` / `CreateWithCap`).

**Ordering invariant:** per-game-session signals MUST complete before `player_sessions` row removal, otherwise the auth token is invalidated mid-flight and a concurrent Subscribe validation (`ValidateSessionOwnership`) could flap.

### `sessionStore.Delete` signature change

Remove the `reason string` parameter — it's no longer used for signaling. Signature becomes:

```go
Delete(ctx context.Context, sessionID string) error
```

This forces every caller to move the reason into an `EndSession` call, making the architectural invariant (session_ended MUST be emitted before Delete) explicit at the API boundary. The interface change invalidates the generated `MockStore` — mocks MUST be regenerated as a task in the plan.

### Dead code removal

After cutover, the following become unused and MUST be deleted:

- `internal/session/session.go`: `WatchSession` method on `Store` interface; `Event` type; `EventType`; `Destroyed` constant
- `internal/session/memstore.go`: `WatchSession`, `watchers` map, Delete's watcher-notification block
- `internal/store/session_store.go`: same (Postgres implementation)
- `internal/session/mocks/mock_Store.go`: regenerate after interface change
- Tests: `TestMemStore_WatchSession_*`, etc. — delete, do not port

---

## Command Semantics (Path 1)

The architectural change unlocks clean per-command semantics. Document the intended command palette explicitly so future contributors and players share a model.

| Intent | Command | Primitive | Event(s) emitted | Effect |
|---|---|---|---|---|
| Close this surface, keep playing elsewhere | `disconnect` (new telnet command; web UI tab close signal) | `Disconnect(connectionID)` RPC | None from session lifecycle (wire close is in-band). If the closing connection was the last grid-present connection, existing `phase_out` emits a `leave` event. | That one wire dies; session continues; other surfaces unaffected; grid-present flips if this was the last grid connection |
| End this play session | `quit` | `HandleDisconnect` + `EndSession` + `Delete` | `leave` on location; `session_ended` on character | All surfaces drop; player remains authenticated, returns to character selection |
| Sign out of account | `logout` | Iterate child game sessions → per-session fanout (see § Logout & eviction fanout) → `authService.Logout` | For each active child game session: `leave` on location + `session_ended` on character | All surfaces of all this player's active game sessions drop; player must re-authenticate |

### The `disconnect` telnet command

Add a new command word `disconnect` (or alias `/disconnect` in future rich UIs) to the telnet command dispatcher.

- Telnet handler: when dispatched, the command maps to `client.Disconnect(req)` with the current `connectionID`. The gRPC Disconnect RPC already handles the "last grid connection" → `phase_out` + leave-event path — no new server-side logic required for the primitive.
- Client UX: server responds with a brief confirmation (`"Disconnected. Other surfaces remain active."` or similar) before closing the telnet wire.
- Web UI: tab close / page unload fires a `Disconnect(connID)` via the web gateway. This is an existing affordance already in place for graceful TCP close; the `disconnect` command word is the explicit counterpart for telnet users who cannot close a tab.

Zero new server primitives — `disconnect` is pure glue over the existing Disconnect RPC. The telnet handler already retains `h.playerSessionToken` (set at two-phase login, `internal/telnet/gateway_handler.go:77`) and reuses it for existing core RPCs; the new `disconnect` command path passes the same token. The plan has this as one telnet-side task + associated unit tests.

### Multi-surface quit friction

When a player issues `quit` and their session has more than one active connection, the server MUST NOT immediately tear down. Instead:

1. **First `quit` with `> 1` connections:** emit a `command_response` event on the character stream with body: *`"You have N other surfaces connected. Type QUIT again within 30 seconds to end the session for all of them, or type DISCONNECT to close only this surface."`* Set a session-scoped flag `PendingQuitConfirmationUntil` to `now + 30s`.
2. **Second `quit` within the TTL:** proceed with the full quit path (HandleDisconnect + EndSession + Delete).
3. **Any other command or TTL expiry:** clear the flag. A subsequent `quit` re-enters step 1.

**Storage:** `session.Info.PendingQuitConfirmationUntil *time.Time` is in-memory only (no migration). On process restart the flag clears — benign (user re-issues `quit`, sees prompt again). This is Design Decision #7.

**Store mutation discipline:** `Store.Get` returns a defensive copy (`copyInfo` in `memstore.go:481`), so a `Get → mutate → Set` pattern would race with concurrent `Set` calls. The flag MUST be mutated in-place under the store's mutex. Add a new store method `SetPendingQuitConfirmation(ctx context.Context, sessionID string, until *time.Time) error` analogous to the existing `UpdateLastPaged` / `UpdateGridPresent` in-place mutators. Callers MUST use this method, not `Get → Set`.

**Cross-surface visibility:** the confirmation prompt is emitted as a `command_response` event on the character stream, so all surfaces subscribed to the character see the message. The user can confirm on any surface. If two surfaces both race the second `quit` within the TTL, both proceed with teardown — the emitted `session_ended` events are idempotent from a Subscriber's perspective (first match terminates the stream; any subsequent matching events are moot because the stream has already returned). No CAS is required.

**Interaction with `disconnect`:** the prompt explicitly tells the user to type `disconnect` if they only want to close one surface. `disconnect` does NOT clear `PendingQuitConfirmationUntil` (intentional — a user who types `quit` → `disconnect` is saying "not that, just this" and should be able to retry `quit` later without re-entering the prompt immediately; the TTL expiry handles this naturally).

**TTL expiry does NOT emit events or invoke `runDisconnectHooks`.** It is pure state clear.

---

## Testing Strategy

### Unit tests

- `engine.EndSession` appends a correctly-shaped event to `character:{ID}` with Actor per Design Decision #1.
- `engine.EndSession` uses `core.NewULID` (not `idgen.New`) — verify the ruleguard `EventIDMustBeMonotonic` passes on the new call site.
- `sendAndCommitEvent` with matching `session_ended`: Sends event + updates cursor + Sends STREAM_CLOSED + returns `errStreamTerminated`.
- `sendAndCommitEvent` with *non-matching* `session_ended`: Sends event + updates cursor + returns `nil` (does NOT terminate). **Negative assertion: the event body is forwarded to the client intact**, per Design Decision #3.
- Subscribe live loop error arm: `errors.Is(err, errStreamTerminated)` short-circuits to `return nil` before the `oops.Code("SEND_FAILED")` wrap.

### Integration tests (Ginkgo/Gomega BDD per project convention, `//go:build integration`)

- **Flake-proof quit regression:** replace the existing flaky "Player A disconnects cleanly via quit" test. Assertions: (a) `"Goodbye!"` received within 5s; (b) *no* `"Your characters:"` line appears in the stream (negative assertion on the specific symptom); (c) exactly one `session_ended` event on `character:{ID}` with matching `SessionID`, `cause="quit"`, reason `"Goodbye!"` in the event store after the test.
- **Multi-surface fan-out:** open two Subscribe streams for the same `sessionID` from different `connectionID`s (existing `AddConnection` permits this; no new infra needed). Issue `quit` from one wire (after the multi-surface confirmation is resolved). Assert BOTH streams receive STREAM_CLOSED with matching reason and both return cleanly.
- **Replay isolation:** guest A connects, quits, new guest on same character logic is guest-specific so use a registered player: player connects, quits, reconnects + resumes. Assert the new Subscribe does NOT self-terminate despite the prior `session_ended` being in replay history.
- **Character + location ordering:** append `say` on location, `leave` on location, `session_ended` on character, verify the merge-sort replay delivers them in append order (I2 regression guard, analogous to `internal/store/postgres_integration_test.go:575`).
- **Multi-surface quit confirmation:** two connections open; first `quit` emits confirmation prompt as `command_response` (no `session_ended` persisted yet); second `quit` within TTL triggers full flow. Variant: second `quit` after TTL → new prompt.
- **Disconnect command isolation:** two connections open; `disconnect` from one closes that wire only; other wire continues to receive events; no `session_ended` in event store.
- **Logout fanout:** player with two active game sessions (two characters); `Logout` emits `session_ended` on BOTH characters' streams; all live Subscribes drop; `player_sessions` row gone.
- **PlayerSession eviction fanout:** login at cap → oldest PlayerSession's child game session emits `session_ended` before eviction completes.
- **Reaper on detached session:** detached session gets reaped; `session_ended` with `cause="reaped"` is persisted (audit trail) even though no Subscribe was listening. Verifies I2's guarantee holds even without a consumer.
- **Ctx cancel during terminate:** client ctx canceled mid-quit; `session_ended` is still persisted in the store; no `panic` or hang on the server.

### EventWriter coverage guard

Depending on which guardrail the plan picks for I1:
- If type-level (`NewEngine(*EventWriter)`): compilation error if violated — nothing else needed.
- If startup assertion: a unit test that `NewEngine(rawStore)` panics, and an integration test that the production wiring passes.

### PR gate

`task pr-prep` MUST pass. The flaky test's fix is the proof of correctness; there MUST NOT be a retry shim, timeout extension, or client-side fallback introduced as part of this work.

---

## Rejected Alternatives

### A. Synchronous drain on Destroyed in Subscribe (fallback-worthy but not chosen)

When `sessionCh` fires, drain pending `sub.Notifications()` non-blockingly before sending STREAM_CLOSED. Under I1 + I2, EventWriter's serialization plus PG LISTEN single-connection delivery means append-order equals notification-order — so a drain *could* close the race, not merely narrow it, by consuming all pending notifications up to a known `LastEventID` before emitting STREAM_CLOSED.

Option D wins on:
- **Code reduction** — deletes a whole control plane rather than adding drain logic.
- **Audit trail** — `session_ended` persists in the event store; Option A leaves no durable record.
- **Architectural alignment** — session state becomes event-sourced like everything else.
- **Surface area** — Option D deletes WatchSession + session.Event + session.Destroyed; Option A keeps them and adds draining complexity.

**Kept as the documented fallback** if Option D encounters an unexpected blocker during implementation (e.g., a hidden consumer of WatchSession, a Postgres edge case we didn't anticipate). Option A is correct, just less good.

### B. Terminal marker in the event stream, separate from `session_ended`

Append a synthetic "end-of-stream" marker that Subscribe treats as terminal. Halfway measure: invents a fake-event concept solely for stream termination, doesn't address the broader gap that session state is not event-sourced. Adds complexity without full payoff.

### C. Priority select in Subscribe

Go does not have priority select. Nested selects with non-blocking reads can approximate it but yield the *wrong* ordering (processing Destroyed *before* draining pending data events). Incorrect.

### Path 2: Per-surface sessions

Each surface gets its own session. `quit` on telnet ends only that telnet's session; web-ui keeps running. Breaks I3 and cascading invariants:

- `FindByCharacter` returns the one session for a character — grid presence model depends on this
- Peers see exactly one `arrive`, one set of events per character; per-surface sessions require dedupe everywhere
- Session cap (PR #225) becomes ambiguous
- Plugin per-session state needs per-surface replication

The "logged in on two devices" mental model is accommodated via the new `disconnect` command and the multi-surface `quit` confirmation, without fragmenting the session model.

### Client-side fallback "Goodbye!"

Send `Goodbye!` from the telnet handler itself when `drainUntilClosed` times out, even if no STREAM_CLOSED was received. Hides a server-side contract violation behind gateway UX glue. Rejected.

---

## Design Decisions

The following were open questions during brainstorming; each is now decided and MUST be implemented as stated.

1. **Actor convention.**
   - `cause="quit"` → `Actor{Kind: ActorCharacter, ID: char.ID.String()}` (character-initiated).
   - All other causes (`logout`, `guest_end`, `kicked`, `reaped`, `evicted`) → `Actor{Kind: ActorSystem, ID: "system"}` (system-initiated). Character correlation is in the payload's `CharacterID`.

2. **Notification ordering.** Relies on I1 (EventWriter serialization) + I2 (single-connection LISTEN order). Plan MUST add the cross-stream ordering integration test described in § Testing Strategy.

3. **Replay cursor semantics.** `sendAndCommitEvent` ALWAYS forwards the event to the client and advances the cursor, regardless of whether the `session_ended` payload matches this session's ID. Only AFTER those side effects, if matching, does it emit STREAM_CLOSED and return the sentinel. Non-matching `session_ended` is forwarded verbatim — web clients can optionally surface a toast ("your other session ended"); telnet clients can ignore it. This gives multi-surface UX value and guarantees cursors never stall.

4. **`runDisconnectHooks`.** Unchanged — runs after `sessionStore.Delete` as today. Hook observers (`guestAuth.ReleaseGuest`, test instrumentation, future plugin cleanup) do not depend on `WatchSession`.

5. **Reaper characterization.** The reaper calls `sessionStore.Delete` and fires `WatchSession`, but operates on `StatusDetached` sessions that by definition have no live Subscribe. The signal has no listener today; Option D makes reaper terminations observable via the persisted `session_ended` event for audit purposes.

6. **Backwards compatibility.** `session.Event`, `WatchSession`, and `session.Destroyed` are not exported from `pkg/`. Interface-method deletion is safe. All mocks MUST be regenerated.

7. **Multi-surface quit confirmation flag storage.** In-memory only (`session.Info.PendingQuitConfirmationUntil *time.Time`). No migration. Process restart clears the flag; user re-issues `quit` and re-sees the prompt. Benign.

8. **Cutover style.** Single-commit cutover (no transition period). `WatchSession` has exactly one real consumer (`server.go:876` Subscribe); everything else is mocks and tests. The plan deletes the interface method, regenerates mocks, and removes dead tests in one coherent change.

9. **EventWriter guardrail.** Either type-level (`NewEngine` takes `*EventWriter`) or runtime (startup check asserting the engine's store is EventWriter-wrapped). Plan picks one and states why. Preference: runtime check, because the engine's current signature takes the `EventStore` interface and many test constructors deliberately use a non-writer store — a type-level change ripples into test code that doesn't need the ordering guarantee.

   **Implementation note:** `*core.EventWriter` itself implements `core.EventStore` (by wrapping), so an interface-conformance check has no discriminating power. The runtime guard MUST do a concrete-type assertion: `if _, ok := store.(*core.EventWriter); !ok { /* panic or error */ }`. Production wiring at `cmd/holomush/sub_grpc.go` already constructs the EventWriter explicitly; the guard catches any regression where a future refactor bypasses it.

10. **Plugin per-session state cleanup.** Plugin cleanup continues via `DisconnectHook`, not via `session_ended` event observers. Plugins that want to observe session termination for their own side effects MAY subscribe to the character stream and filter on `session_ended`, but this is optional; the authoritative cleanup path is hooks.

---

## Residual Open Questions

Only genuinely plan-level questions remain. Implementation MUST resolve each before declaring the relevant task complete.

1. **Admin boot path exact shape.** `server.go:457` calls `HandleDisconnect` with reason `"booted"`; the full boot RPC flow (where is the session deletion?) needs one more read during plan authoring. Expected to be analogous to quit.
2. **`ListByPlayerSession` vs. filtered `ListByPlayer`.** For the logout/eviction fanout, which API is cleanest? Current store has `ListByPlayer` (character ID filter); we need enumeration by `player_session_id`. Plan evaluates cost of adding a new method vs. filtering in memory.
3. **Confirmation prompt wording finalization.** The literal string in § Multi-surface quit friction is a proposal; i18n and voice might want tuning. Not blocking implementation but blocking merge.

---

## Out of Scope for This Spec

- Rich-web-ui "Connected surfaces" management UI (shows device types with "Close this device" buttons). Separate bead.
- Per-surface focus state (currently all surfaces share session focus; changing that is a different architectural discussion).
- Postgres session store TTL reaper refactor (separate work).
- Post-Option-D observability follow-up: any `drainUntilClosed` timeout on the telnet gateway post-cutover indicates a different bug; add a WARN log in a tiny separate bead.
- Session creation as an event (`session_started` on character stream). Symmetric with `session_ended` but not required to fix the flake; possible follow-up for audit completeness.

---

## References

- Bead: `holomush-9es6`
- Flaky test: `test/integration/telnet/e2e_test.go:548` "Player A disconnects cleanly via quit"
- Current Subscribe loop: `internal/grpc/server.go:883` (select with racing channels)
- EventWriter: `internal/core/event_writer.go`
- Variant A ordering test (template for I2 regression): `internal/store/postgres_integration_test.go:575`
- Event-sourcing invariant: `CLAUDE.md` "Patterns" / "Event Sourcing" section
- Three-axis lifecycle: `internal/grpc/server.go:939` (Disconnect RPC comment "phases out" semantics)
- Focus system (orthogonal): `api/proto/holomush/plugin/v1/plugin.proto:320` (FocusKind)
- PR #225 session identity: `holomush-abbg` and follow-ups
- PR #229 telnet DoS hardening: `holomush-abbg` (merged)
- PR #231 Lua resource limits: `holomush-u9p5` (merged)
