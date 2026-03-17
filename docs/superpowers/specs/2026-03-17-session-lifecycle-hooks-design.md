<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Session Lifecycle Hooks — Design Spec

## Goal

Add connect/disconnect lifecycle handling so the system properly cleans up
session state and emits arrival/departure events. Fixes the guest name pool
exhaustion bug (400-connection DoS) and wires the existing but unused
`arrive`/`leave` event types.

## Scope

Two deliverables plus legacy cleanup:

1. **Engine-level lifecycle events** — `HandleConnect` and `HandleDisconnect`
   methods that emit `arrive`/`leave` events through the event store and
   broadcaster
2. **CoreServer disconnect hooks** — callback list for infrastructure cleanup
   (guest name release, ephemeral session teardown)
3. **Legacy telnet handler removal** — delete the old `ConnectionHandler`,
   `Server`, and `AuthHandler` code replaced by `GatewayHandler`

## Non-Goals

- Reconnect grace period or session TTL reaper
- Plugin-side `arrive`/`leave` event handler implementations (infrastructure
  is wired; plugins can subscribe naturally through existing mechanisms)
- Rate limiting on the telnet listener
- Registered player authentication

## Architecture

```text
Connect flow:
  Gateway → CoreServer.Authenticate
    → authenticator.Authenticate (returns AuthResult with IsGuest flag)
    → sessions.Connect(charID, connID)
    → engine.HandleConnect(ctx, charID, locationID, charName)
      → appends "arrive" event to store
      → broadcasts to "location:<locationID>" stream
    → sessionStore.Set(sessionID, SessionInfo)
    → return AuthResponse

Disconnect flow:
  Gateway → CoreServer.Disconnect
    → sessionStore.Get(sessionID) → SessionInfo
    → engine.HandleDisconnect(ctx, charID, locationID, charName, reason)
      → appends "leave" event to store
      → broadcasts to "location:<locationID>" stream
    → sessions.Disconnect(charID, connID)
    → if IsGuest: sessions.EndSession(charID)
    → for each disconnectHook: hook(info)
    → sessionStore.Delete(sessionID)
```

The leave event fires before session teardown so it can reference a
still-valid session. Disconnect hooks fire after teardown since they are
for cleanup.

## Changes to Existing Types

### AuthResult

Add `IsGuest` flag so the CoreServer knows whether to call `EndSession`
on disconnect. The authenticator decides — any auth type can declare its
sessions as ephemeral.

```go
type AuthResult struct {
    CharacterID   ulid.ULID
    CharacterName string
    LocationID    ulid.ULID
    IsGuest       bool
}
```

### SessionInfo

Add `CharacterName` (needed by disconnect hooks, e.g., `ReleaseGuest`)
and `IsGuest` (carried from `AuthResult`).

```go
type SessionInfo struct {
    CharacterID   ulid.ULID
    LocationID    ulid.ULID
    ConnectionID  ulid.ULID
    CharacterName string
    IsGuest       bool
}
```

### CoreServer

Add a disconnect hook list and a new option function:

```go
type CoreServer struct {
    // ... existing fields
    disconnectHooks []func(SessionInfo)
}

func WithDisconnectHook(hook func(SessionInfo)) CoreServerOption {
    return func(s *CoreServer) {
        s.disconnectHooks = append(s.disconnectHooks, hook)
    }
}
```

### GuestAuthenticator

Set `IsGuest: true` on the returned `AuthResult`. No other changes — the
authenticator already has `ReleaseGuest`.

## Engine Lifecycle Methods

### HandleConnect

```go
func (e *Engine) HandleConnect(ctx context.Context, charID, locationID ulid.ULID, charName string) error
```

Builds an `arrive` event with `ArrivePayload`, appends to the event store
on stream `"location:<locationID>"`, and broadcasts.

### HandleDisconnect

```go
func (e *Engine) HandleDisconnect(ctx context.Context, charID, locationID ulid.ULID, charName, reason string) error
```

Builds a `leave` event with `LeavePayload`, appends to the event store on
stream `"location:<locationID>"`, and broadcasts.

### Event Payloads

```go
type ArrivePayload struct {
    CharacterName string `json:"character_name"`
}

type LeavePayload struct {
    CharacterName string `json:"character_name"`
    Reason        string `json:"reason"`
}
```

Reason values: `"quit"`, `"disconnect"`, `"timeout"`.

For this slice, `CoreServer.Disconnect` always passes `"quit"` as the
reason. Differentiating between voluntary quit and involuntary disconnect
requires a field on `DisconnectRequest` — deferred to follow-up work.

Both use the existing `EventTypeArrive` and `EventTypeLeave` constants
which are already defined in `internal/core/event.go` but never emitted.

## CoreServer.Disconnect Sequence

```go
func (s *CoreServer) Disconnect(ctx, req) {
    info, ok := s.sessionStore.Get(req.SessionId)
    if !ok {
        return success // already gone
    }

    // 1. Emit leave event (session still active)
    if err := s.engine.HandleDisconnect(ctx, ...); err != nil {
        slog.Warn("leave event failed", "error", err) // log and continue
    }

    // 2. Remove connection from session manager
    s.sessions.Disconnect(info.CharacterID, info.ConnectionID)

    // 3. End ephemeral sessions immediately
    if info.IsGuest {
        if err := s.sessions.EndSession(info.CharacterID); err != nil {
            slog.Warn("end session failed", "error", err) // log and continue
        }
    }

    // 4. Run infrastructure cleanup hooks (recover panics)
    for _, hook := range s.disconnectHooks {
        func() {
            defer func() {
                if r := recover(); r != nil {
                    slog.Error("disconnect hook panicked", "panic", r)
                }
            }()
            hook(*info)
        }()
    }

    // 5. Remove gRPC session mapping
    s.sessionStore.Delete(req.SessionId)
}
```

Note: `mockery` MUST be re-run after changing `SessionInfo` to regenerate
the `MockSessionStore`.

## CoreServer.Authenticate Sequence

After existing authentication and session creation, add the connect event:

```go
func (s *CoreServer) Authenticate(ctx, req) {
    result := s.authenticator.Authenticate(ctx, username, password)

    sessionID := s.newSessionID()
    connID := core.NewULID()
    s.sessions.Connect(result.CharacterID, connID)

    s.sessionStore.Set(sessionID.String(), &SessionInfo{
        CharacterID:   result.CharacterID,
        LocationID:    result.LocationID,
        ConnectionID:  connID,
        CharacterName: result.CharacterName,
        IsGuest:       result.IsGuest,
    })

    // Emit arrive event
    s.engine.HandleConnect(ctx, result.CharacterID,
        result.LocationID, result.CharacterName)

    return AuthResponse{...}
}
```

## Wiring

In `cmd/holomush/core.go`:

```go
guestAuth := telnet.NewGuestAuthenticator(...)

coreServer := holoGRPC.NewCoreServer(engine, sessions, broadcaster,
    holoGRPC.WithAuthenticator(guestAuth),
    holoGRPC.WithDisconnectHook(func(info holoGRPC.SessionInfo) {
        if info.IsGuest {
            guestAuth.ReleaseGuest(info.CharacterName)
        }
    }),
)
```

## Legacy Cleanup

Delete the old telnet handler code that was replaced by `GatewayHandler`.
No backward compatibility needed — these types have zero external
references.

### Files to Delete

| File | What It Contains |
| --- | --- |
| `internal/telnet/handler.go` | `ConnectionHandler` — hardcoded test auth, direct engine calls |
| `internal/telnet/handler_test.go` | `ConnectionHandler` tests |
| `internal/telnet/server.go` | `Server` struct — required direct core components |
| `internal/telnet/server_test.go` | `Server` tests |
| `internal/telnet/auth_handler.go` | `AuthHandler` — designed for gateway but never used |
| `internal/telnet/auth_handler_test.go` | `AuthHandler` tests |
| `internal/telnet/auth_handler_logging_test.go` | `AuthHandler` logging tests |

This also removes the hardcoded `testCharID`/`testLocationID` ULIDs from
`handler.go` that were never used by the gateway path.

## Testing

### Unit Tests

- `internal/core/engine_test.go`: `HandleConnect` appends `arrive` event
  to store and broadcasts. `HandleDisconnect` appends `leave` event.
- `internal/grpc/server_test.go`: `Authenticate` emits `arrive` event.
  `Disconnect` emits `leave` event, calls hooks with correct `SessionInfo`,
  calls `EndSession` for guests but not for non-guests.
- `internal/grpc/server_test.go`: `WithDisconnectHook` registers and
  invokes hooks in order.

### Integration Tests

- `test/integration/telnet/e2e_test.go`: After guest disconnect and
  reconnect, a new unique name is generated (proves `ReleaseGuest` was
  called via hook). Arrive/leave events appear in the location stream
  replay.

## Success Criteria

- [ ] `ReleaseGuest` is called on guest disconnect via hook
- [ ] Guest sessions are ended via `EndSession` on disconnect
- [ ] `arrive` events are emitted on successful authentication
- [ ] `leave` events are emitted on disconnect
- [ ] Events appear in location stream and can be replayed
- [ ] Disconnect hooks fire with correct `SessionInfo`
- [ ] Legacy telnet handler code is deleted
- [ ] All tests pass
- [ ] No zombie sessions accumulate for guest connections

## Risks

| Risk | Mitigation |
| --- | --- |
| `HandleConnect` failure after session created | Log error and continue — session is valid, event is best-effort |
| `HandleDisconnect` failure blocks disconnect | Log error and continue — cleanup MUST proceed |
| Hook panic crashes server | Recover in hook loop, log, continue with remaining hooks |
| Existing tests depend on deleted handler code | Verified: no external references to deleted types |
| `arrive`/`leave` events increase event store volume | Minimal — one per connect/disconnect, same as say/pose |
