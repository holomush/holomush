<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Session Lifecycle Hooks — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add connect/disconnect lifecycle handling — emit `arrive`/`leave` events, clean up guest sessions, release guest names, and delete legacy telnet code.

**Architecture:** Engine gets `HandleConnect`/`HandleDisconnect` methods (emit events through store + broadcaster). CoreServer gets `disconnectHooks` callback list + `IsGuest`/`CharacterName` on `SessionInfo`/`AuthResult`. Guest name release happens via a disconnect hook. Legacy `ConnectionHandler`/`Server`/`AuthHandler` are deleted.

**Tech Stack:** Go 1.24, gRPC (existing), testify, Ginkgo/Gomega (integration tests)

**Spec:** `docs/superpowers/specs/2026-03-17-session-lifecycle-hooks-design.md`

**Epic:** `holomush-pju7`

---

## File Structure

### New Files

None — all changes are to existing files.

### Modified Files

| File | Change |
| --- | --- |
| `internal/core/engine.go` | Add `ArrivePayload`, `LeavePayload`, `HandleConnect`, `HandleDisconnect` |
| `internal/core/engine_test.go` | Tests for `HandleConnect`/`HandleDisconnect` |
| `internal/grpc/server.go:25-48` | Add `IsGuest` to `AuthResult`, add `CharacterName`+`IsGuest` to `SessionInfo`, add `disconnectHooks` to `CoreServer`, add `WithDisconnectHook` option |
| `internal/grpc/server.go:133-184` | `Authenticate` — store new fields, call `engine.HandleConnect` |
| `internal/grpc/server.go:358-396` | `Disconnect` — call `engine.HandleDisconnect`, `EndSession` for guests, run hooks |
| `internal/grpc/server_test.go` | Tests for disconnect hooks, arrive/leave events, EndSession for guests |
| `internal/telnet/guest_auth.go:87-91` | Set `IsGuest: true` on `AuthResult` |
| `cmd/holomush/core.go:348-354` | Wire `WithDisconnectHook` for guest name release |
| `test/integration/telnet/e2e_test.go` | Add arrive/leave event assertions, guest name reuse test |

### Files to Delete

| File | Reason |
| --- | --- |
| `internal/telnet/handler.go` | Legacy `ConnectionHandler` — replaced by `GatewayHandler` |
| `internal/telnet/handler_test.go` | Tests for deleted handler |
| `internal/telnet/server.go` | Legacy `Server` — never used by gateway |
| `internal/telnet/server_test.go` | Tests for deleted server |
| `internal/telnet/auth_handler.go` | Legacy `AuthHandler` — never used by gateway |
| `internal/telnet/auth_handler_test.go` | Tests for deleted auth handler |
| `internal/telnet/auth_handler_logging_test.go` | Logging tests for deleted auth handler |

---

## Chunk 1: Engine Lifecycle Methods

### Task 1: HandleConnect + ArrivePayload

**Files:**

- Modify: `internal/core/engine.go`
- Modify: `internal/core/engine_test.go`

#### Step 1.1: Write failing test for HandleConnect

- [ ] **Write test**

In `internal/core/engine_test.go`, add:

```go
func TestEngine_HandleConnect(t *testing.T) {
    store := &MemoryEventStore{}
    broadcaster := NewBroadcaster()
    engine := NewEngine(store, NewSessionManager(), broadcaster)

    charID := NewULID()
    locationID := NewULID()
    stream := "location:" + locationID.String()

    // Subscribe to catch broadcast
    ch := broadcaster.Subscribe(stream)
    defer broadcaster.Unsubscribe(stream, ch)

    err := engine.HandleConnect(context.Background(), charID, locationID, "Sapphire_Neon")
    require.NoError(t, err)

    // Verify event in store
    events, err := store.Replay(context.Background(), stream, ulid.ULID{}, 10)
    require.NoError(t, err)
    require.Len(t, events, 1)

    assert.Equal(t, EventTypeArrive, events[0].Type)
    assert.Equal(t, charID.String(), events[0].Actor.ID)
    assert.Equal(t, stream, events[0].Stream)

    var payload ArrivePayload
    require.NoError(t, json.Unmarshal(events[0].Payload, &payload))
    assert.Equal(t, "Sapphire_Neon", payload.CharacterName)

    // Verify broadcast
    select {
    case ev := <-ch:
        assert.Equal(t, EventTypeArrive, ev.Type)
    default:
        t.Fatal("expected broadcast event")
    }
}
```

- [ ] **Run test to verify it fails**

```bash
task test -- -run TestEngine_HandleConnect -v ./internal/core/...
```

Expected: FAIL — `HandleConnect` undefined.

#### Step 1.2: Implement HandleConnect

- [ ] **Add ArrivePayload and HandleConnect to engine.go**

After `PosePayload` (line 23), add:

```go
// ArrivePayload is the JSON payload for arrive events.
type ArrivePayload struct {
    CharacterName string `json:"character_name"`
}
```

After `HandlePose` (after line ~100), add:

```go
// HandleConnect emits an arrive event when a character connects.
func (e *Engine) HandleConnect(ctx context.Context, charID, locationID ulid.ULID, charName string) error {
    payload, err := json.Marshal(ArrivePayload{CharacterName: charName})
    if err != nil {
        return oops.With("operation", "marshal_arrive_payload").Wrap(err)
    }

    event := Event{
        ID:        NewULID(),
        Stream:    "location:" + locationID.String(),
        Type:      EventTypeArrive,
        Timestamp: time.Now(),
        Actor:     Actor{Kind: ActorCharacter, ID: charID.String()},
        Payload:   payload,
    }

    if err := e.store.Append(ctx, event); err != nil {
        return oops.With("operation", "append_arrive_event").Wrap(err)
    }

    if e.broadcaster != nil {
        e.broadcaster.Broadcast(event)
    }

    return nil
}
```

- [ ] **Run test**

```bash
task test -- -run TestEngine_HandleConnect -v ./internal/core/...
```

Expected: PASS

- [ ] **Commit**

```bash
JJ_EDITOR=true jj --no-pager new -m "feat(core): add Engine.HandleConnect with arrive event"
```

### Task 2: HandleDisconnect + LeavePayload

**Files:**

- Modify: `internal/core/engine.go`
- Modify: `internal/core/engine_test.go`

#### Step 2.1: Write failing test

- [ ] **Write test**

```go
func TestEngine_HandleDisconnect(t *testing.T) {
    store := &MemoryEventStore{}
    broadcaster := NewBroadcaster()
    engine := NewEngine(store, NewSessionManager(), broadcaster)

    charID := NewULID()
    locationID := NewULID()
    stream := "location:" + locationID.String()

    ch := broadcaster.Subscribe(stream)
    defer broadcaster.Unsubscribe(stream, ch)

    err := engine.HandleDisconnect(context.Background(), charID, locationID, "Ruby_Xenon", "quit")
    require.NoError(t, err)

    events, err := store.Replay(context.Background(), stream, ulid.ULID{}, 10)
    require.NoError(t, err)
    require.Len(t, events, 1)

    assert.Equal(t, EventTypeLeave, events[0].Type)

    var payload LeavePayload
    require.NoError(t, json.Unmarshal(events[0].Payload, &payload))
    assert.Equal(t, "Ruby_Xenon", payload.CharacterName)
    assert.Equal(t, "quit", payload.Reason)
}
```

- [ ] **Run test to verify it fails**

Expected: FAIL — `HandleDisconnect` undefined.

#### Step 2.2: Implement HandleDisconnect

- [ ] **Add LeavePayload and HandleDisconnect to engine.go**

```go
// LeavePayload is the JSON payload for leave events.
type LeavePayload struct {
    CharacterName string `json:"character_name"`
    Reason        string `json:"reason"`
}

// HandleDisconnect emits a leave event when a character disconnects.
func (e *Engine) HandleDisconnect(ctx context.Context, charID, locationID ulid.ULID, charName, reason string) error {
    payload, err := json.Marshal(LeavePayload{CharacterName: charName, Reason: reason})
    if err != nil {
        return oops.With("operation", "marshal_leave_payload").Wrap(err)
    }

    event := Event{
        ID:        NewULID(),
        Stream:    "location:" + locationID.String(),
        Type:      EventTypeLeave,
        Timestamp: time.Now(),
        Actor:     Actor{Kind: ActorCharacter, ID: charID.String()},
        Payload:   payload,
    }

    if err := e.store.Append(ctx, event); err != nil {
        return oops.With("operation", "append_leave_event").Wrap(err)
    }

    if e.broadcaster != nil {
        e.broadcaster.Broadcast(event)
    }

    return nil
}
```

- [ ] **Run test**

Expected: PASS

- [ ] **Commit**

```bash
JJ_EDITOR=true jj --no-pager new -m "feat(core): add Engine.HandleDisconnect with leave event"
```

---

## Chunk 2: CoreServer Type Changes + Disconnect Hooks

### Task 3: Update AuthResult, SessionInfo, and Add DisconnectHook

**Files:**

- Modify: `internal/grpc/server.go:25-97`
- Modify: `internal/grpc/server_test.go`

#### Step 3.1: Write failing test for disconnect hook

- [ ] **Write test**

In `internal/grpc/server_test.go`, add:

```go
func TestCoreServer_DisconnectHook(t *testing.T) {
    charID := core.NewULID()
    locationID := core.NewULID()
    connID := core.NewULID()
    sessionID := core.NewULID()

    store := &MemoryEventStore{}
    sessions := core.NewSessionManager()
    broadcaster := core.NewBroadcaster()
    engine := core.NewEngine(store, sessions, broadcaster)

    var hookCalled bool
    var hookInfo SessionInfo

    server := &CoreServer{
        engine:       engine,
        sessions:     sessions,
        broadcaster:  broadcaster,
        sessionStore: NewInMemorySessionStore(),
        newSessionID: func() ulid.ULID { return sessionID },
        disconnectHooks: []func(SessionInfo){
            func(info SessionInfo) {
                hookCalled = true
                hookInfo = info
            },
        },
    }

    // Create a mock authenticator that returns IsGuest=true
    auth := &mockAuthenticator{
        result: &AuthResult{
            CharacterID:   charID,
            CharacterName: "Opal_Carbon",
            LocationID:    locationID,
            IsGuest:       true,
        },
    }
    server.authenticator = auth

    ctx := context.Background()

    // Authenticate to create the session
    authResp, err := server.Authenticate(ctx, &corev1.AuthRequest{
        Username: "guest",
        Meta:     &corev1.RequestMeta{RequestId: "test"},
    })
    require.NoError(t, err)
    require.True(t, authResp.Success)

    // Disconnect
    discResp, err := server.Disconnect(ctx, &corev1.DisconnectRequest{
        SessionId: authResp.SessionId,
        Meta:      &corev1.RequestMeta{RequestId: "test"},
    })
    require.NoError(t, err)
    require.True(t, discResp.Success)

    // Verify hook was called with correct info
    assert.True(t, hookCalled)
    assert.Equal(t, charID, hookInfo.CharacterID)
    assert.Equal(t, "Opal_Carbon", hookInfo.CharacterName)
    assert.True(t, hookInfo.IsGuest)
}
```

- [ ] **Run test to verify it fails**

Expected: FAIL — `SessionInfo` has no `CharacterName` field.

#### Step 3.2: Update types and add hook support

- [ ] **Update AuthResult** (server.go line 25):

```go
type AuthResult struct {
    CharacterID   ulid.ULID
    CharacterName string
    LocationID    ulid.ULID
    IsGuest       bool
}
```

- [ ] **Update SessionInfo** (server.go line 44):

```go
type SessionInfo struct {
    CharacterID   ulid.ULID
    LocationID    ulid.ULID
    ConnectionID  ulid.ULID
    CharacterName string
    IsGuest       bool
}
```

- [ ] **Add disconnectHooks to CoreServer** (server.go line 86):

Add field: `disconnectHooks []func(SessionInfo)`

- [ ] **Add WithDisconnectHook option** (after `WithSessionStore`):

```go
// WithDisconnectHook registers a callback invoked on session disconnect.
func WithDisconnectHook(hook func(SessionInfo)) CoreServerOption {
    return func(s *CoreServer) {
        s.disconnectHooks = append(s.disconnectHooks, hook)
    }
}
```

- [ ] **Update Authenticate** to store new fields (server.go ~line 175):

Change the `sessionStore.Set` call to include `CharacterName` and `IsGuest`:

```go
s.sessionStore.Set(sessionID.String(), &SessionInfo{
    CharacterID:   result.CharacterID,
    LocationID:    result.LocationID,
    ConnectionID:  connID,
    CharacterName: result.CharacterName,
    IsGuest:       result.IsGuest,
})
```

- [ ] **Run test**

Expected: FAIL — `disconnectHooks` not invoked in `Disconnect` yet. The test will get past compilation but `hookCalled` will be false.

#### Step 3.3: Update Disconnect to call hooks and EndSession

- [ ] **Rewrite Disconnect** (server.go lines 358-396):

```go
func (s *CoreServer) Disconnect(ctx context.Context, req *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error) {
    requestID := ""
    if req.Meta != nil {
        requestID = req.Meta.RequestId
    }

    slog.DebugContext(ctx, "disconnect request",
        "request_id", requestID,
        "session_id", req.SessionId,
    )

    info, ok := s.sessionStore.Get(req.SessionId)
    if !ok {
        return &corev1.DisconnectResponse{
            Meta:    responseMeta(requestID),
            Success: true,
        }, nil
    }

    // Emit leave event while session is still active
    if err := s.engine.HandleDisconnect(ctx, info.CharacterID, info.LocationID, info.CharacterName, "quit"); err != nil {
        slog.WarnContext(ctx, "leave event failed",
            "request_id", requestID,
            "session_id", req.SessionId,
            "error", err,
        )
    }

    // Remove connection from session manager
    s.sessions.Disconnect(info.CharacterID, info.ConnectionID)

    // End ephemeral sessions immediately
    if info.IsGuest {
        if err := s.sessions.EndSession(info.CharacterID); err != nil {
            slog.WarnContext(ctx, "end session failed",
                "request_id", requestID,
                "character_id", info.CharacterID.String(),
                "error", err,
            )
        }
    }

    // Run disconnect hooks with panic recovery
    for _, hook := range s.disconnectHooks {
        func() {
            defer func() {
                if r := recover(); r != nil {
                    slog.ErrorContext(ctx, "disconnect hook panicked",
                        "request_id", requestID,
                        "panic", r,
                    )
                }
            }()
            hook(*info)
        }()
    }

    // Remove gRPC session mapping
    s.sessionStore.Delete(req.SessionId)

    slog.InfoContext(ctx, "session disconnected",
        "request_id", requestID,
        "session_id", req.SessionId,
        "character_id", info.CharacterID.String(),
        "is_guest", info.IsGuest,
    )

    return &corev1.DisconnectResponse{
        Meta:    responseMeta(requestID),
        Success: true,
    }, nil
}
```

- [ ] **Regenerate mocks**

```bash
mockery
task build
```

Expected: Build succeeds with regenerated mocks.

- [ ] **Run test**

Expected: PASS

- [ ] **Write non-guest EndSession test**

```go
func TestCoreServer_Disconnect_NonGuest_NoEndSession(t *testing.T) {
    charID := core.NewULID()
    locationID := core.NewULID()
    sessionID := core.NewULID()

    store := &MemoryEventStore{}
    sessions := core.NewSessionManager()
    engine := core.NewEngine(store, sessions, core.NewBroadcaster())

    auth := &mockAuthenticator{
        result: &AuthResult{
            CharacterID:   charID,
            CharacterName: "TestPlayer",
            LocationID:    locationID,
            IsGuest:       false, // NOT a guest
        },
    }

    server := &CoreServer{
        engine:        engine,
        sessions:      sessions,
        broadcaster:   core.NewBroadcaster(),
        authenticator: auth,
        sessionStore:  NewInMemorySessionStore(),
        newSessionID:  func() ulid.ULID { return sessionID },
    }

    ctx := context.Background()
    authResp, err := server.Authenticate(ctx, &corev1.AuthRequest{
        Username: "player1",
        Meta:     &corev1.RequestMeta{RequestId: "test"},
    })
    require.NoError(t, err)
    require.True(t, authResp.Success)

    // Disconnect
    _, err = server.Disconnect(ctx, &corev1.DisconnectRequest{
        SessionId: authResp.SessionId,
        Meta:      &corev1.RequestMeta{RequestId: "test"},
    })
    require.NoError(t, err)

    // Session should still exist (not ended for non-guests)
    session := sessions.GetSession(charID)
    assert.NotNil(t, session, "non-guest session should persist after disconnect")
}
```

- [ ] **Run test**

Expected: PASS

- [ ] **Commit**

```bash
JJ_EDITOR=true jj --no-pager new -m "feat(grpc): add disconnect hooks, IsGuest, CharacterName to SessionInfo"
```

### Task 4: Add arrive event to Authenticate

**Files:**

- Modify: `internal/grpc/server.go:133-184`
- Modify: `internal/grpc/server_test.go`

#### Step 4.1: Write failing test

- [ ] **Write test**

```go
func TestCoreServer_Authenticate_EmitsArriveEvent(t *testing.T) {
    charID := core.NewULID()
    locationID := core.NewULID()

    store := &MemoryEventStore{}
    sessions := core.NewSessionManager()
    broadcaster := core.NewBroadcaster()
    engine := core.NewEngine(store, sessions, broadcaster)

    auth := &mockAuthenticator{
        result: &AuthResult{
            CharacterID:   charID,
            CharacterName: "Jade_Helium",
            LocationID:    locationID,
        },
    }

    server := &CoreServer{
        engine:       engine,
        sessions:     sessions,
        broadcaster:  broadcaster,
        authenticator: auth,
        sessionStore: NewInMemorySessionStore(),
        newSessionID: core.NewULID,
    }

    ctx := context.Background()
    resp, err := server.Authenticate(ctx, &corev1.AuthRequest{
        Username: "guest",
        Meta:     &corev1.RequestMeta{RequestId: "test"},
    })
    require.NoError(t, err)
    require.True(t, resp.Success)

    // Verify arrive event in store
    stream := "location:" + locationID.String()
    events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
    require.NoError(t, err)
    require.Len(t, events, 1)
    assert.Equal(t, core.EventTypeArrive, events[0].Type)

    var payload core.ArrivePayload
    require.NoError(t, json.Unmarshal(events[0].Payload, &payload))
    assert.Equal(t, "Jade_Helium", payload.CharacterName)
}
```

- [ ] **Run test to verify it fails**

Expected: FAIL — no arrive event in store (Authenticate doesn't emit one yet).

#### Step 4.2: Add HandleConnect call to Authenticate

- [ ] **Add after the `sessionStore.Set` call** (around line 179):

```go
// Emit arrive event (best-effort — session is valid even if event fails)
if err := s.engine.HandleConnect(ctx, result.CharacterID, result.LocationID, result.CharacterName); err != nil {
    slog.WarnContext(ctx, "arrive event failed",
        "request_id", requestID,
        "character_id", result.CharacterID.String(),
        "error", err,
    )
}
```

- [ ] **Run test**

Expected: PASS

- [ ] **Run all grpc tests**

```bash
task test -- -v ./internal/grpc/...
```

Expected: All PASS (existing tests unaffected — arrive event is additive).

- [ ] **Commit**

```bash
JJ_EDITOR=true jj --no-pager new -m "feat(grpc): emit arrive event on successful authentication"
```

---

## Chunk 3: Guest Auth + Wiring

### Task 5: Set IsGuest on AuthResult

**Files:**

- Modify: `internal/telnet/guest_auth.go:87-91`
- Modify: `internal/telnet/guest_auth_test.go`

#### Step 5.1: Write failing test

- [ ] **Add to guest_auth_test.go:**

```go
func TestGuestAuthenticator_ReturnsIsGuest(t *testing.T) {
    startLocation := ulid.Make()
    auth := NewGuestAuthenticator(NewGemstoneElementTheme(), startLocation)

    result, err := auth.Authenticate(context.Background(), "guest", "")
    require.NoError(t, err)
    assert.True(t, result.IsGuest)
}
```

- [ ] **Run test to verify it fails**

Expected: FAIL — `IsGuest` is `false`.

#### Step 5.2: Set IsGuest in Authenticate

- [ ] **Modify the return in guest_auth.go Authenticate** (around line 87):

Change:

```go
    return &grpcserver.AuthResult{
        CharacterID:   ulid.Make(),
        CharacterName: name,
        LocationID:    a.startLocation,
    }, nil
```

To:

```go
    return &grpcserver.AuthResult{
        CharacterID:   ulid.Make(),
        CharacterName: name,
        LocationID:    a.startLocation,
        IsGuest:       true,
    }, nil
```

- [ ] **Run test**

Expected: PASS

- [ ] **Commit**

```bash
JJ_EDITOR=true jj --no-pager new -m "feat(telnet): set IsGuest=true on guest AuthResult"
```

### Task 6: Wire DisconnectHook in core.go

**Files:**

- Modify: `cmd/holomush/core.go:348-354`

#### Step 6.1: Add WithDisconnectHook

- [ ] **Update the NewCoreServer call** to include the hook:

```go
coreServer := holoGRPC.NewCoreServer(engine, sessions, broadcaster,
    holoGRPC.WithAuthenticator(guestAuth),
    holoGRPC.WithDisconnectHook(func(info holoGRPC.SessionInfo) {
        if info.IsGuest {
            guestAuth.ReleaseGuest(info.CharacterName)
        }
    }),
)
```

- [ ] **Verify build**

```bash
task build
```

Expected: Success

- [ ] **Commit**

```bash
JJ_EDITOR=true jj --no-pager new -m "feat(core): wire guest name release disconnect hook"
```

---

## Chunk 4: Legacy Cleanup

### Task 7: Delete Legacy Telnet Code

**Files to delete:**

- `internal/telnet/handler.go`
- `internal/telnet/handler_test.go`
- `internal/telnet/server.go`
- `internal/telnet/server_test.go`
- `internal/telnet/auth_handler.go`
- `internal/telnet/auth_handler_test.go`
- `internal/telnet/auth_handler_logging_test.go`

#### Step 7.1: Delete all 7 files

- [ ] **Delete files**

```bash
rm internal/telnet/handler.go \
   internal/telnet/handler_test.go \
   internal/telnet/server.go \
   internal/telnet/server_test.go \
   internal/telnet/auth_handler.go \
   internal/telnet/auth_handler_test.go \
   internal/telnet/auth_handler_logging_test.go
```

- [ ] **Verify build**

```bash
task build
```

Expected: Success (no external references to deleted types).

- [ ] **Run all telnet package tests**

```bash
task test -- -v ./internal/telnet/...
```

Expected: Only `gateway_handler_test.go` and `guest_auth_test.go` tests run. All PASS.

- [ ] **Commit**

```bash
JJ_EDITOR=true jj --no-pager new -m "chore(telnet): delete legacy ConnectionHandler, Server, and AuthHandler"
```

---

## Chunk 5: Integration Tests + Quality Gates

### Task 8: Update E2E Tests

**Files:**

- Modify: `test/integration/telnet/e2e_test.go`

#### Step 8.1: Add arrive/leave event replay assertion

- [ ] **Add a test scenario** to the existing E2E Describe block:

```go
Describe("Lifecycle Events", func() {
    It("emits arrive event on guest connect", func() {
        client, err := newTestTelnetClient(telnetAddr)
        Expect(err).NotTo(HaveOccurred())
        defer client.Close()

        connectAsGuest(client)

        // Give event time to persist
        time.Sleep(200 * time.Millisecond)

        // Replay location stream — should contain arrive event
        events, err := eventStore.Replay(testCtx,
            "location:"+startLocation.String(), ulid.ULID{}, 100)
        Expect(err).NotTo(HaveOccurred())

        found := false
        for _, e := range events {
            if string(e.Type) == "arrive" {
                found = true
                break
            }
        }
        Expect(found).To(BeTrue(), "expected arrive event in store")
    })

    It("emits leave event on guest disconnect", func() {
        client, err := newTestTelnetClient(telnetAddr)
        Expect(err).NotTo(HaveOccurred())

        connectAsGuest(client)
        client.SendLine("quit")
        _ = client.ReadLine() // Goodbye
        client.Close()

        time.Sleep(200 * time.Millisecond)

        events, err := eventStore.Replay(testCtx,
            "location:"+startLocation.String(), ulid.ULID{}, 100)
        Expect(err).NotTo(HaveOccurred())

        found := false
        for _, e := range events {
            if string(e.Type) == "leave" {
                found = true
                break
            }
        }
        Expect(found).To(BeTrue(), "expected leave event in store")
    })

    It("releases guest name after disconnect", func() {
        // Connect 5 guests, record names, disconnect all
        names := make(map[string]bool)
        for range 5 {
            c, err := newTestTelnetClient(telnetAddr)
            Expect(err).NotTo(HaveOccurred())
            name := connectAsGuest(c)
            names[name] = true
            c.SendLine("quit")
            _ = c.ReadLine()
            c.Close()
        }
        // Wait for disconnect hooks to fire
        time.Sleep(500 * time.Millisecond)

        // Connect 5 more — at least one name should be reused
        // (proves ReleaseGuest was called)
        for range 5 {
            c, err := newTestTelnetClient(telnetAddr)
            Expect(err).NotTo(HaveOccurred())
            name := connectAsGuest(c)
            if names[name] {
                // Name was reused — test passes
                c.Close()
                return
            }
            c.SendLine("quit")
            _ = c.ReadLine()
            c.Close()
        }
        // With 400 names and 5+5 connections, reuse is not guaranteed
        // by random chance but IS guaranteed if ReleaseGuest works.
        // The pool is shuffled, so after release the name goes back
        // to the end of the pool. With only 5 names released and 5
        // new connections, we should see reuse.
        Fail("expected at least one guest name to be reused after disconnect")
    })
})
```

- [ ] **Run integration tests**

```bash
task test:integration
```

Expected: All PASS including new lifecycle scenarios.

- [ ] **Commit**

```bash
JJ_EDITOR=true jj --no-pager new -m "test(e2e): add lifecycle event and guest name reuse assertions"
```

### Task 9: Quality Gates

- [ ] **Run linter**

```bash
task lint
```

Fix any issues.

- [ ] **Run all unit tests**

```bash
task test
```

All PASS.

- [ ] **Run integration tests**

```bash
task test:integration
```

All PASS.

- [ ] **Check coverage**

```bash
task test:coverage
```

Verify `internal/core` and `internal/grpc` packages are >80%.

- [ ] **Commit any fixes**

```bash
JJ_EDITOR=true jj --no-pager new -m "chore: fix lint issues from session lifecycle hooks"
```

### Task 10: Code Review + PR

- [ ] **Run code review**

Invoke `pr-review-toolkit:review-pr` for comprehensive review.

- [ ] **Address all findings**

- [ ] **Close epic**

```bash
bd close holomush-pju7 --reason "Session lifecycle hooks complete"
```

- [ ] **Create PR**

Use `finishing-a-development-branch` skill.

---

## Post-Implementation Checklist

- [ ] All unit tests pass (`task test`)
- [ ] All integration tests pass (`task test:integration`)
- [ ] Linter clean (`task lint:go`)
- [ ] Formatter clean (`task fmt`)
- [ ] License headers on all modified files
- [ ] Legacy telnet files deleted (7 files)
- [ ] `arrive` events emitted on connect
- [ ] `leave` events emitted on disconnect
- [ ] Guest names released via disconnect hook
- [ ] Guest sessions ended via `EndSession`
- [ ] No zombie sessions for guest connections
- [ ] Code review passed
- [ ] PR created
