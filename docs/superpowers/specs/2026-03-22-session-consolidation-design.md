<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Session Consolidation Design

Unify the two overlapping session systems (`core.SessionManager` and
`session.Store`) into a single Postgres-backed implementation. Eliminates
split-brain state, the `hadSessionBefore` guard, and duplicate cursor tracking.

## Problem

Two independent session systems track overlapping state:

| State | `core.SessionManager` (in-memory) | `session.Store` (Postgres) |
|-------|-----------------------------------|----------------------------|
| Connections | `[]ulid.ULID` in struct | `AddConnection`/`RemoveConnection` rows |
| Event cursors | `map[string]ulid.ULID` in struct | `UpdateCursors` column |
| Last activity | `time.Time` in struct | `UpdatedAt` column |
| Session lifecycle | `Connect`/`EndSession` map ops | `Set`/`Delete`/`UpdateStatus` rows |

Every session operation must touch both systems. When they diverge, bugs
appear:

- The quit handler calls `SessionManager.EndSession` (in-memory) but not
  `sessionStore.Delete` (Postgres). The gRPC server compensates with a
  `hadSessionBefore` guard that inspects in-memory state before and after
  command dispatch.
- Connection tracking lives only in memory. Server restarts lose all
  connection state.
- Event cursors are written to both systems independently. No mechanism
  ensures they stay consistent.

## Design Decisions

### Sentinel error for quit and self-boot

The quit handler and self-boot path MUST return `command.ErrSessionEnded`
instead of calling `EndSession` or `DeleteByCharacter` directly.
`executeViaDispatcher` checks for this sentinel and performs server-side
teardown (leave event, PG delete, disconnect hooks).

Admin-boot (booting another player) uses `DeleteByCharacter` directly.
The target's session deletion triggers `WatchSession` watchers, which
send `STREAM_CLOSED` to any active streams. The boot handler already
emits a system message to the target's stream before deletion. Emitting
a leave event for the booted target is a pre-existing gap (the current
`hadSessionBefore` guard only checks the executor's session) — tracked
as follow-up work, not addressed in this consolidation.

**Why:** Replaces the `hadSessionBefore` guard, which couples quit detection
to in-memory state inspection. A sentinel error is explicit, testable, and
independent of storage backend.

### Two interfaces: `session.Store` and `session.Access`

Command handlers use a narrow `session.Access` interface (3 methods).
The gRPC server and reaper use the full `session.Store`.

**Why:** Command handler tests mock 3 methods instead of 20+. Follows
"accept interfaces, return structs" and keeps handler tests focused.
Named `Access` (not `Querier`) because it includes the mutating
`DeleteByCharacter` method.

### Engine loses session dependency

`Engine.ReplayEvents` has no production callers (the Subscribe handler
reads cursors from `session.Store` directly). Remove it and the
`Engine.sessions` field. The Engine constructor simplifies from
`NewEngine(store, sessions)` to `NewEngine(store)`.

**Why:** Dead code removal. The Engine becomes a pure event emitter.

### No caching layer

All session reads go directly to Postgres. The `session.Store` interface
permits adding a cache later if profiling shows need, but no cache is
built preemptively.

**Why:** YAGNI. PG handles the expected load. Adding a cache creates a
third state source, reintroducing the split-brain problem this work
eliminates.

## Unified Interface

### session.Access (new)

Narrow interface for command handlers. Three methods covering what
`core.SessionService` provided:

```go
type Access interface {
    // ListActive returns all sessions with status=active.
    ListActive(ctx context.Context) ([]*Info, error)

    // FindByCharacter returns the active or detached session for a character.
    // Already exists on Store — reused, not added.
    FindByCharacter(ctx context.Context, characterID ulid.ULID) (*Info, error)

    // DeleteByCharacter finds and deletes a character's session.
    // Returns the deleted Info for caller use (disconnect hooks, leave events).
    // Returns nil, nil if no session exists.
    DeleteByCharacter(ctx context.Context, characterID ulid.ULID, reason string) (*Info, error)
}
```

`PostgresSessionStore` implements `Access` as a subset of `Store`.
`FindByCharacter` is already implemented — only `ListActive` and
`DeleteByCharacter` are new methods.

### session.Store additions

Three new methods on the existing `Store` interface:

```go
// ListActive returns all sessions with status=active.
ListActive(ctx context.Context) ([]*Info, error)

// DeleteByCharacter finds and deletes a character's session.
// Returns the deleted Info, or nil if no session exists.
DeleteByCharacter(ctx context.Context, characterID ulid.ULID, reason string) (*Info, error)

// UpdateActivity bumps the updated_at timestamp for a session.
UpdateActivity(ctx context.Context, id string) error
```

`ListActive` replaces `core.SessionManager.ListActiveSessions()`.
`DeleteByCharacter` replaces `FindByCharacter` + `Delete` two-step.
`UpdateActivity` replaces `core.SessionManager.UpdateActivity()`.

### command.ErrSessionEnded (new)

```go
// ErrSessionEnded signals that the handler ended the session gracefully.
// Server-side teardown (leave event, PG delete, hooks) happens in
// executeViaDispatcher, not the handler.
var ErrSessionEnded = errors.New("session ended")
```

The quit handler returns `oops.Code("SESSION_ENDED").Wrap(ErrSessionEnded)`.
`executeViaDispatcher` checks `errors.Is(err, command.ErrSessionEnded)`.

## Removed Components

| Component | Reason |
|-----------|--------|
| `core.SessionManager` | Replaced by `session.Store` (Postgres) |
| `core.Session` struct | Replaced by `session.Info` |
| `core.SessionService` interface | Replaced by `session.Access` |
| `core.copySession()` helper | No longer needed (PG returns fresh structs) |
| `Engine.sessions` field | Dead dependency (ReplayEvents has no callers) |
| `Engine.ReplayEvents` method | Dead code; Subscribe handler reads cursors directly |
| `CoreServer.sessions` field | Replaced by `CoreServer.sessionStore` (already exists) |
| `hadSessionBefore` guard | Replaced by `ErrSessionEnded` sentinel |

## Migration Map

Callers of in-memory `SessionManager`, mapped to their replacements:

### CoreServer (internal/grpc/server.go)

| Current call | New call |
|---|---|
| `s.sessions.Connect(charID, connID)` | Remove. Session created via `sessionStore.Set`, connection via `sessionStore.AddConnection`. Both already happen in the same code paths. |
| `s.sessions.Disconnect(charID, connID)` (executeViaSwitch quit, line 528) | Remove. PG session deleted via `sessionStore.Delete` on same line. |
| `s.sessions.Disconnect(charID, ulid.ULID{})` (Disconnect RPC guest path, line 977) | Remove. PG session deleted via `sessionStore.Delete` on line 970. |
| `s.sessions.EndSession(charID)` (Disconnect RPC guest path, line 978) | Remove. PG session deleted via `sessionStore.Delete` on line 970. |
| `s.sessions.Disconnect(charID, ulid.ULID{})` (Disconnect RPC detach path, line 1004) | Remove. PG status update via `sessionStore.UpdateStatus` on line 996. |
| `s.sessions.GetSession(charID)` (hadSessionBefore, line 414) | Remove. Replaced by `ErrSessionEnded` sentinel check. |
| `s.sessions.UpdateCursor(charID, stream, eventID)` (Subscribe replay loop, line 702) | Remove. `persistCursorAsync` already writes cursors to PG via `sessionStore.UpdateCursors`. |

### CoreServer auth handlers (internal/grpc/auth_handlers.go)

| Current call | New call |
|---|---|
| `s.sessions.Connect(charID, connID)` (3 sites) | Remove. Session + connection registration already handled by `sessionStore.Set` + `sessionStore.AddConnection`. |

### Engine (internal/core/engine.go)

| Current call | New call |
|---|---|
| `e.sessions.GetSession(charID)` in ReplayEvents | Remove entire method. No production callers. |

### Command handlers

| Handler | Current | New |
|---|---|---|
| who | `Session().ListActiveSessions()` | `Session().ListActive(ctx)` + handle error |
| boot | `Session().ListActiveSessions()` (via `findCharacterByName` helper) | `Session().ListActive(ctx)` + handle error |
| boot (self) | `Session().EndSession(charID)` | Return `ErrSessionEnded` (same as quit — self-boot is semantically identical) |
| boot (admin) | `Session().EndSession(targetID)` | `Session().DeleteByCharacter(ctx, targetID, reason)` (target teardown; leave event is pre-existing gap) |
| quit | `Session().EndSession(charID)` | Return `ErrSessionEnded` (no store call) |
| wall | `Session().ListActiveSessions()` | `Session().ListActive(ctx)` + handle error |

### command.Services (internal/command/types.go)

| Current | New |
|---|---|
| `Session core.SessionService` in ServicesConfig | `Session session.Access` |
| `session core.SessionService` in Services struct | `session session.Access` |
| `Session() core.SessionService` getter | `Session() session.Access` |

### cmd/holomush/core.go

| Current | New |
|---|---|
| `sessions := core.NewSessionManager()` | Remove. |
| `engine := core.NewEngine(realStore, sessions)` | `engine := core.NewEngine(realStore)` |
| `holoGRPC.NewCoreServer(engine, sessions, sessionStore, ...)` | `holoGRPC.NewCoreServer(engine, sessionStore, ...)` |
| `Session: sessions` in ServicesConfig | `Session: sessionStore` (Store implements Access) |

## Handler Test Changes

All handler tests currently create `core.NewSessionManager()` and call
`Connect` to populate sessions. After consolidation, tests use a shared
`mockAccess` that implements `session.Access`:

```go
type mockAccess struct {
    sessions []*session.Info
}

func (m *mockAccess) ListActive(_ context.Context) ([]*session.Info, error) {
    return m.sessions, nil
}

func (m *mockAccess) FindByCharacter(_ context.Context, charID ulid.ULID) (*session.Info, error) {
    for _, s := range m.sessions {
        if s.CharacterID == charID {
            return s, nil
        }
    }
    return nil, nil
}

func (m *mockAccess) DeleteByCharacter(_ context.Context, charID ulid.ULID, _ string) (*session.Info, error) {
    for i, s := range m.sessions {
        if s.CharacterID == charID {
            m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
            return s, nil
        }
    }
    return nil, nil
}
```

Place in `internal/command/handlers/testutil/` alongside `ServicesBuilder`.
The `ServicesBuilder.WithSession` method changes from accepting
`core.SessionService` to `session.Access`.

## executeViaDispatcher After Consolidation

```go
func (s *CoreServer) executeViaDispatcher(ctx context.Context, info *session.Info, input string) error {
    input = expandMUSHPrefix(input)
    char := core.CharacterRef{ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID}

    sessionID, parseErr := ulid.Parse(info.ID)
    if parseErr != nil {
        return oops.Code("INVALID_SESSION_ID").With("session_id", info.ID).Wrap(parseErr)
    }

    var buf bytes.Buffer
    exec, err := command.NewCommandExecution(command.CommandExecutionConfig{
        CharacterID:   info.CharacterID,
        LocationID:    info.LocationID,
        CharacterName: info.CharacterName,
        SessionID:     sessionID,
        Output:        &buf,
        Services:      s.cmdServices,
    })
    if err != nil {
        return oops.Code("EXECUTION_SETUP_FAILED").Wrap(err)
    }

    dispatchErr := s.dispatcher.Dispatch(ctx, input, exec)

    // Emit buffered output as command_response event.
    if buf.Len() > 0 {
        isError := dispatchErr != nil && !errors.Is(dispatchErr, command.ErrSessionEnded)
        if emitErr := s.emitCommandResponse(ctx, char, strings.TrimRight(buf.String(), "\n"), isError); emitErr != nil {
            return oops.Wrap(emitErr)
        }
    }

    // Quit detection: handler signals intent, server does teardown.
    if errors.Is(dispatchErr, command.ErrSessionEnded) {
        if dcErr := s.engine.HandleDisconnect(ctx, char, "quit"); dcErr != nil {
            slog.WarnContext(ctx, "leave event failed", "error", dcErr)
        }
        if delErr := s.sessionStore.Delete(ctx, info.ID, "Goodbye!"); delErr != nil {
            slog.WarnContext(ctx, "session delete failed", "error", delErr)
        }
        s.runDisconnectHooks(ctx, *info)
        return nil
    }

    if dispatchErr != nil {
        if isUserFacingError(dispatchErr) {
            if buf.Len() == 0 {
                if emitErr := s.emitCommandResponse(ctx, char, command.PlayerMessage(dispatchErr), true); emitErr != nil {
                    return oops.Wrap(emitErr)
                }
            }
            return nil
        }
        return oops.Wrap(dispatchErr)
    }

    return nil
}
```

## Behavioral Changes

- **Quit output `isError` flag:** Currently, the quit handler's "Goodbye!"
  output is emitted before the `hadSessionBefore` guard runs, so the
  `isError` flag reflects `dispatchErr != nil` at emit time (false, since
  quit returns nil). After consolidation, quit returns `ErrSessionEnded`
  and the emit uses `isError := dispatchErr != nil && !errors.Is(...)`,
  which is also false. No behavioral change in practice.

- **All `ListActiveSessions` callers gain error handling.** The current
  `core.SessionService.ListActiveSessions()` returns `[]*Session` (no
  error). The replacement `Access.ListActive(ctx)` returns
  `([]*Info, error)`. Every call site (who, boot, wall, findCharacterByName)
  MUST add `if err != nil` handling.

## Legacy `executeViaSwitch`

Removing `executeViaSwitch` entirely is out of scope (tracked separately).
However, it references `s.sessions` (the field being removed) on line 528:

```go
s.sessions.Disconnect(info.CharacterID, ulid.ULID{})
```

This call MUST be removed as part of this consolidation since the
`CoreServer.sessions` field no longer exists. The quit path in
`executeViaSwitch` already calls `s.sessionStore.Delete` (line 525),
so removing the in-memory disconnect is safe.

## Out of Scope

- Caching layer for session reads (YAGNI; add if profiling shows need).
- Full removal of the legacy `executeViaSwitch` path (tracked separately;
  only the `s.sessions` reference is removed here).
- Session reaper changes (already uses `session.Store` exclusively).
- Postgres schema changes (connection tracking tables already exist).
- Leave event for admin-boot targets (pre-existing gap; tracked as
  follow-up).
