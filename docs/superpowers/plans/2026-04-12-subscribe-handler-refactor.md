# Subscribe Handler Refactor Implementation Plan (B7)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewrite `CoreServer.Subscribe` from ~270 lines to ~120 by delegating stream selection to `FocusCoordinator.RestoreFocus`, replacing per-stream `Subscribe()` with session-wide `SubscribeSession()` (Variant A), adding merge-sort replay (I-15), and clean-breaking the `SubscribeRequest.streams`/`replay_from_cursor` fields.

**Architecture:** The handler becomes a thin RPC boundary: session lifecycle, gRPC delivery, and cursor-lock interaction stay; stream selection moves to `RestoreFocus`, per-stream relay goroutines are replaced by a single `Subscription.Notifications()` channel, and the replay pass uses merge-sort across all streams. The `locationFollower` is reworked to use `Subscription.AddStream/RemoveStream` instead of per-stream `Subscribe`. All in-tree callers (telnet, web, tests) update atomically — no backward compat.

**Tech Stack:** Go 1.26, `internal/grpc`, `internal/grpc/focus`, `internal/core`, protobuf, testify.

**Design spec:** `docs/superpowers/specs/2026-04-11-focus-substrate-design.md` Section 5.

**Bead:** `holomush-oy6e.7`
**Depends on:** B2 (SubscribeSession), B3 (FocusMembership), B4 (Coordinator), B6 (ScenePolicy) — all merged
**Blocks:** B8 (Plugin host API)

---

## File Map

| File | Change |
| --- | --- |
| `api/proto/holomush/core/v1/core.proto` | Modify: remove `streams` and `replay_from_cursor` from `SubscribeRequest` |
| `api/proto/holomush/web/v1/web.proto` | Modify: remove `replay_from_cursor` from `StreamEventsRequest` |
| `internal/grpc/replay.go` | New: `replayRestorePlan` merge-sort and `applyCtrlUpdate` helper |
| `internal/grpc/replay_test.go` | New: tests for merge-sort replay and ctrl update |
| `internal/grpc/server.go` | Modify: rewrite `Subscribe` (~280→~120 lines), remove `subscribeStream`, `startNotificationRelay`, `streamNotification`, `legacySubscriber` |
| `internal/grpc/location_follow.go` | Modify: rework to use `core.Subscription` instead of per-stream Subscribe+relay |
| `internal/grpc/location_follow_test.go` | Modify: update for new `locationFollower` fields |
| `internal/grpc/server_test.go` | Modify: remove `Streams`/`ReplayFromCursor` from all test SubscribeRequests; update Subscribe tests for new flow |
| `internal/web/handler.go` | Modify: remove `ReplayFromCursor` from `StreamEventsRequest` construction |
| `internal/web/handler_test.go` | Modify: update mock if needed |
| `test/integration/session/session_persistence_integration_test.go` | Modify: remove `ReplayFromCursor` from test requests |
| Generated proto files | Regenerated: `task proto:gen` or equivalent |

---

## Task 1: Proto clean break — remove streams and replay\_from\_cursor

Remove the client-specified fields from the proto definitions. Regenerate Go code. Fix all callers.

**Files:**

- Modify: `api/proto/holomush/core/v1/core.proto`
- Modify: `api/proto/holomush/web/v1/web.proto`
- Regenerate: all `*.pb.go` files via `buf generate`

- [ ] **Step 1: Remove fields from core.proto**

In `api/proto/holomush/core/v1/core.proto`, change `SubscribeRequest`:

```protobuf
message SubscribeRequest {
  RequestMeta meta = 1;
  string session_id = 2;
  // Fields 3 (streams) and 4 (replay_from_cursor) removed in focus
  // substrate clean break. Server determines streams and replay policy
  // via FocusCoordinator.RestoreFocus.
  reserved 3, 4;
  reserved "streams", "replay_from_cursor";
}
```

- [ ] **Step 2: Remove replay\_from\_cursor from web.proto**

In `api/proto/holomush/web/v1/web.proto`, change `StreamEventsRequest`:

```protobuf
message StreamEventsRequest {
  // Field 2 (replay_from_cursor) removed — server always replays from cursor.
  reserved 2;
  reserved "replay_from_cursor";
}
```

- [ ] **Step 3: Regenerate proto Go code**

Run: `buf generate` (or whatever the project uses — check `Taskfile.yml` for the proto generation task)

Run: `task proto:gen` or `buf generate`

- [ ] **Step 4: Fix compilation errors from removed fields**

The generated code will no longer have `GetStreams()`, `GetReplayFromCursor()`, `Streams`, or `ReplayFromCursor` fields. Every caller that references them will fail to compile. Fix each:

1. `internal/grpc/server.go` — remove `req.Streams` and `req.ReplayFromCursor` references
2. `internal/web/handler.go:185` — remove `ReplayFromCursor: req.Msg.GetReplayFromCursor()` from the `SubscribeRequest` construction
3. `internal/grpc/server_test.go` — remove `Streams: []string{...}` and `ReplayFromCursor: true/false` from all `SubscribeRequest` literals
4. `test/integration/session/session_persistence_integration_test.go` — remove `ReplayFromCursor: true` from test requests

Run: `task build`
Expected: compiles (may have test failures until Subscribe is rewritten).

- [ ] **Step 5: Commit**

```text
feat(proto): clean-break SubscribeRequest — remove streams and replay_from_cursor (B7 task 1)
```

---

## Task 2: Rework locationFollower for Subscription

The `locationFollower` currently uses the old per-stream `eventStore.Subscribe()` + `startNotificationRelay` pattern. It needs to use the session-wide `core.Subscription` instead — calling `AddStream` for new location streams and `RemoveStream` for old ones.

**Files:**

- Modify: `internal/grpc/location_follow.go`
- Modify: `internal/grpc/location_follow_test.go`

- [ ] **Step 1: Rework locationFollower struct**

Replace the old fields (`eventStore`, `notifyCh`, `errCh`, `locCancel`) with a reference to the session-wide Subscription:

```go
type locationFollower struct {
    characterID   ulid.ULID
    currentLocID  ulid.ULID
    worldQuerier  WorldQuerier
    sessionStore  session.Store
    locStreamName string
    sub           core.Subscription // session-wide subscription, shared with handler
}
```

Remove the `locCancel` field and the `defer lf.locCancel()` cleanup — the Subscription owns the PG connection lifecycle.

- [ ] **Step 2: Rework switchLocationSubscription**

Replace the old per-stream Subscribe + relay goroutine approach with:

```go
func (lf *locationFollower) switchLocationSubscription(ctx context.Context, newLocID ulid.ULID) {
    if lf.sub == nil {
        return
    }

    newStreamName := world.LocationStream(newLocID)

    // Add new stream BEFORE removing old — ensures continuous location feed.
    if err := lf.sub.AddStream(ctx, newStreamName); err != nil {
        slog.WarnContext(ctx, "location-following: add stream failed",
            "stream", newStreamName, "error", err)
        return
    }

    // Remove old location stream.
    if lf.locStreamName != "" && lf.locStreamName != newStreamName {
        _ = lf.sub.RemoveStream(ctx, lf.locStreamName)
    }

    lf.locStreamName = newStreamName
}
```

- [ ] **Step 3: Add sendSynthetic method**

Extract the synthetic location\_state send into a method so the handler can call it cleanly:

```go
// sendSynthetic sends the initial synthetic location_state for the session's
// current location. Called once at Subscribe start.
func (lf *locationFollower) sendSynthetic(
    ctx context.Context,
    stream grpc.ServerStreamingServer[corev1.SubscribeResponse],
) error {
    if lf.worldQuerier == nil || lf.currentLocID.IsZero() {
        return nil
    }
    locState, err := lf.buildLocationState(ctx, lf.currentLocID)
    if err != nil {
        return nil // best-effort — don't fail the subscribe
    }
    return stream.Send(locState)
}
```

- [ ] **Step 4: Update location\_follow\_test.go**

Update test construction to pass `sub` (mock Subscription) instead of `eventStore`/`notifyCh`/`errCh`. The `handleEvent` tests should still work with the new struct fields — they only use `worldQuerier`, `sessionStore`, `characterID`, `currentLocID`. The `switchLocationSubscription` tests need updating to use a mock `Subscription`.

- [ ] **Step 5: Run tests**

Run: `task test -- ./internal/grpc/ -run TestLocation`
Expected: PASS.

- [ ] **Step 6: Commit**

```text
refactor(grpc): rework locationFollower for session-wide Subscription (B7 task 2)
```

---

## Task 3: New replay helpers — replayRestorePlan and applyCtrlUpdate

Two new helper functions that the rewritten Subscribe handler delegates to.

**Files:**

- Create: `internal/grpc/replay.go`
- Create: `internal/grpc/replay_test.go`

- [ ] **Step 1: Implement replayRestorePlan**

`replayRestorePlan` fetches events per-stream per-mode from the `RestorePlan`, merge-sorts by ULID, and delivers in strict global order (I-15).

```go
// replayRestorePlan executes the initial replay pass from the coordinator's
// RestorePlan. Events from all streams are fetched per-mode, merged by ULID,
// and delivered in strict global order through sendAndCommitEvent.
//
// Modes:
//   - FromCursor: Replay from EventCursors[stream] (reconnect resume)
//   - BoundedTail: ReplayTail(stream, tailCount, notBefore) (focus-switch catch-up)
//   - LiveOnly: Advance cursor to stream tail, no replay
func (s *CoreServer) replayRestorePlan(
    ctx context.Context,
    info *session.Info,
    plan focus.RestorePlan,
    stream grpc.ServerStreamingServer[corev1.SubscribeResponse],
    lf *locationFollower,
) error {
    // Collect all events across streams, then sort by ID for merge delivery.
    var allEvents []taggedEvent

    for _, sm := range plan.Streams {
        events, err := s.fetchForMode(ctx, info, sm)
        if err != nil {
            slog.WarnContext(ctx, "replay fetch failed", "stream", sm.Stream, "error", err)
            continue // non-fatal: skip this stream
        }
        for _, ev := range events {
            allEvents = append(allEvents, taggedEvent{stream: sm.Stream, event: ev})
        }
    }

    // Sort by event ID (ULID = time-ordered) for strict global order (I-15).
    sort.Slice(allEvents, func(i, j int) bool {
        return allEvents[i].event.ID.Compare(allEvents[j].event.ID) < 0
    })

    // Deliver in order.
    for _, te := range allEvents {
        if err := s.sendAndCommitEvent(ctx, info, te.stream, te.event, stream, lf); err != nil {
            return err
        }
    }

    return nil
}

// taggedEvent pairs an event with the stream it came from, needed because
// sendAndCommitEvent requires the stream name for cursor commits.
type taggedEvent struct {
    stream string
    event  core.Event
}

// fetchForMode fetches events for a single stream according to its replay mode.
func (s *CoreServer) fetchForMode(
    ctx context.Context,
    info *session.Info,
    sm focus.StreamWithMode,
) ([]core.Event, error) {
    switch sm.Mode {
    case focus.ReplayModeFromCursor:
        cursor := ulid.ULID{}
        if info.EventCursors != nil {
            if c, ok := info.EventCursors[sm.Stream]; ok {
                cursor = c
            }
        }
        return s.eventStore.Replay(ctx, sm.Stream, cursor, s.maxReplay())

    case focus.ReplayModeBoundedTail:
        return s.eventStore.ReplayTail(ctx, sm.Stream, sm.TailCount, sm.NotBefore)

    case focus.ReplayModeLiveOnly:
        // Advance cursor to current tail without replaying.
        lastID, err := s.eventStore.LastEventID(ctx, sm.Stream)
        if err != nil && !errors.Is(err, core.ErrStreamEmpty) {
            return nil, err
        }
        if !lastID.IsZero() {
            commitCtx, cancel := context.WithTimeout(context.Background(), cursorCommitTimeout)
            defer cancel()
            _ = s.sessionStore.UpdateCursors(commitCtx, info.ID,
                map[string]ulid.ULID{sm.Stream: lastID})
        }
        return nil, nil // no events to deliver

    default:
        return nil, nil
    }
}
```

- [ ] **Step 2: Implement applyCtrlUpdate**

`applyCtrlUpdate` handles mid-session stream additions/removals from the control channel, dispatching replay per the update's `ReplayMode`.

```go
// applyCtrlUpdate processes a mid-session stream update from the control
// channel. Adds or removes the stream on the Subscription and, for additions,
// dispatches replay according to the update's ReplayMode.
func (s *CoreServer) applyCtrlUpdate(
    ctx context.Context,
    info *session.Info,
    sub core.Subscription,
    ctrl sessionStreamUpdate,
    stream grpc.ServerStreamingServer[corev1.SubscribeResponse],
    lf *locationFollower,
) error {
    if ctrl.add {
        if err := sub.AddStream(ctx, ctrl.stream); err != nil {
            slog.WarnContext(ctx, "mid-session stream add failed",
                "session_id", info.ID, "stream", ctrl.stream, "error", err)
            return nil // non-fatal
        }
        // Replay per mode.
        sm := focus.StreamWithMode{
            Stream:    ctrl.stream,
            Mode:      ctrl.replayMode,
            TailCount: ctrl.tailCount,
            NotBefore: ctrl.notBefore,
        }
        events, err := s.fetchForMode(ctx, info, sm)
        if err != nil {
            slog.WarnContext(ctx, "mid-session replay failed",
                "session_id", info.ID, "stream", ctrl.stream, "error", err)
            return nil
        }
        for _, ev := range events {
            if err := s.sendAndCommitEvent(ctx, info, ctrl.stream, ev, stream, lf); err != nil {
                return err
            }
        }
    } else {
        if err := sub.RemoveStream(ctx, ctrl.stream); err != nil {
            slog.WarnContext(ctx, "mid-session stream remove failed",
                "session_id", info.ID, "stream", ctrl.stream, "error", err)
        }
    }
    return nil
}
```

- [ ] **Step 3: Write tests for replayRestorePlan**

Test cases:
- `TestReplayRestorePlanMergeSortsAcrossStreams` — 2 streams with interleaved events → delivery in global ULID order
- `TestReplayRestorePlanHandlesEmptyPlan` — empty plan → no events, no error
- `TestReplayRestorePlanLiveOnlyAdvancesCursorWithoutReplay` — LiveOnly stream → cursor advanced, zero events delivered
- `TestReplayRestorePlanBoundedTailUsesReplayTail` — BoundedTail → calls ReplayTail with correct count

Test cases for `applyCtrlUpdate`:
- `TestApplyCtrlUpdateAddsStreamAndReplays` — add with FromCursor → stream added + replayed
- `TestApplyCtrlUpdateRemovesStream` — remove → stream removed from Subscription

- [ ] **Step 4: Run tests**

Run: `task test -- ./internal/grpc/ -run TestReplay`
Expected: PASS.

- [ ] **Step 5: Commit**

```text
feat(grpc): add replayRestorePlan merge-sort and applyCtrlUpdate helper (B7 task 3)
```

---

## Task 4: Rewrite Subscribe handler

The core refactor. Replace the ~280-line handler with the ~120-line target shape from spec Section 5.2.

**Files:**

- Modify: `internal/grpc/server.go`

- [ ] **Step 1: Remove dead code**

Remove from `server.go`:
- `startNotificationRelay` function (lines ~66-90)
- `streamNotification` struct
- `subscribeStream` function
- `legacySubscriber` interface

These are all replaced by `Subscription.Notifications()`.

- [ ] **Step 2: Rewrite Subscribe**

Replace the entire `Subscribe` method body with the target shape from spec Section 5.2. Key structural changes:

1. **Session lookup** stays under cursor lock (unchanged)
2. **Detach detection** — check `info.Status == session.StatusDetached`, call `ReattachCAS`
3. **RestoreFocus** — coordinator produces the full subscription plan
4. **SubscribeSession** — single dedicated PG connection, multi-LISTEN
5. **AddStream loop** — add each stream from the plan
6. **locationFollower** — constructed with `sub` reference, sends synthetic
7. **StreamRegistry** — register control channel
8. **afterLISTENHook** — preserved
9. **replayRestorePlan** — merge-sort replay pass
10. **REPLAY\_COMPLETE** frame
11. **WatchSession** — session lifecycle
12. **Live loop** — select on `sub.Notifications()`, `sub.Errors()`, `sessionCh`, `ctrlCh`, `ctx.Done()`

The handler delegates to `replayRestorePlan` (task 3) and `applyCtrlUpdate` (task 3). `sendAndCommitEvent` is unchanged.

The live loop notification handling changes: instead of calling `replayAndSend(stream, lastSentID[stream])`, it calls `replayAndSend(notif.Stream, notif.EventID-delta)`. Since `Subscription.Notifications()` delivers `StreamNotification{Stream, EventID}`, we replay from the session's cursor for that stream (which `sendAndCommitEvent` keeps up to date).

- [ ] **Step 3: Update replayAndSend callers**

In the live loop, the notification gives us `notif.Stream`. We replay from the current cursor:

```go
case notif := <-sub.Notifications():
    cursor := ulid.ULID{}
    if info.EventCursors != nil {
        if c, ok := info.EventCursors[notif.Stream]; ok {
            cursor = c
        }
    }
    if _, err := s.replayAndSend(ctx, info, notif.Stream, cursor, stream, lf); err != nil {
        return err
    }
```

`replayAndSend` continues to work unchanged — it replays from cursor, sends, commits cursor per event.

- [ ] **Step 4: Verify compilation**

Run: `task build`
Expected: compiles (tests may still fail).

- [ ] **Step 5: Commit**

```text
feat(grpc): rewrite Subscribe handler with RestoreFocus + SubscribeSession (B7 task 4)
```

---

## Task 5: Update RestoreFocus to include ambient streams

Currently `RestoreFocus` only returns focus-membership streams. For the new Subscribe handler to work, it also needs to return ambient streams (character, location, plugin-contributed). The coordinator must query `streamContributor.QuerySessionStreams` and include those in the plan.

**Files:**

- Modify: `internal/grpc/focus/coordinator.go` (add `StreamContributor` dependency)
- Modify: `internal/grpc/focus/restore.go` (include ambient streams)
- Modify: `internal/grpc/focus/coordinator_test.go` / `restore_test.go` (update tests)

- [ ] **Step 1: Add StreamContributor interface to coordinator**

```go
// StreamContributor provides plugin-contributed session streams.
// Decouples the coordinator from the concrete plugin manager.
type StreamContributor interface {
    QuerySessionStreams(ctx context.Context, characterID, playerID, sessionID string) []string
}

// WithStreamContributor sets the plugin stream contributor.
func WithStreamContributor(sc StreamContributor) CoordinatorOption {
    return func(c *defaultCoordinator) { c.streamContributor = sc }
}
```

Add `streamContributor StreamContributor` field to `defaultCoordinator`.

- [ ] **Step 2: Update RestoreFocus to include ambient streams**

After iterating focus memberships, add ambient streams:

```go
// Ambient streams: character, location, plugin-contributed.
charStream := world.CharacterStream(info.CharacterID)
plan.Streams = append(plan.Streams, StreamWithMode{
    Stream: charStream,
    Mode:   ambientMode,
})

if !info.LocationID.IsZero() {
    locStream := world.LocationStream(info.LocationID)
    plan.Streams = append(plan.Streams, StreamWithMode{
        Stream: locStream,
        Mode:   ambientMode,
    })
}

// Plugin-contributed streams.
if c.streamContributor != nil {
    playerID := ""
    if !info.PlayerID.IsZero() {
        playerID = info.PlayerID.String()
    }
    pluginStreams := c.streamContributor.QuerySessionStreams(
        ctx, info.CharacterID.String(), playerID, info.ID)
    seen := make(map[string]bool)
    for _, s := range plan.Streams {
        seen[s.Stream] = true
    }
    for _, ps := range pluginStreams {
        if !seen[ps] {
            plan.Streams = append(plan.Streams, StreamWithMode{
                Stream: ps,
                Mode:   ambientMode,
            })
            seen[ps] = true
        }
    }
}
```

Where `ambientMode` is `ReplayModeFromCursor` for reconnect (has cursors) or `ReplayModeLiveOnly` for initial attach (no cursors to replay from):

```go
// Ambient replay mode: cursor-faithful for reconnect, live-only for initial attach.
ambientMode := ReplayModeFromCursor
isInitialAttach := len(info.FocusMemberships) == 0 && allCursorsZero(info.EventCursors)
if isInitialAttach {
    ambientMode = ReplayModeLiveOnly
}
```

- [ ] **Step 3: Import world package**

The `focus` package now needs `world.CharacterStream` and `world.LocationStream`. Add:

```go
import "github.com/holomush/holomush/internal/world"
```

Check for import cycles — `world` should not import `focus`.

- [ ] **Step 4: Update restore tests**

Add tests for:
- `TestRestoreFocusIncludesAmbientStreams` — active session with location → plan includes character + location streams
- `TestRestoreFocusIncludesPluginContributedStreams` — stub contributor returns streams → plan includes them
- `TestRestoreFocusUsesLiveOnlyForInitialAttach` — no memberships + zero cursors → ambient mode is LiveOnly
- `TestRestoreFocusUsesFromCursorForReconnect` — detached session with cursors → ambient mode is FromCursor

- [ ] **Step 5: Wire StreamContributor in sub\_grpc.go**

In `cmd/holomush/sub_grpc.go`, add `WithStreamContributor(pluginManager)` to the coordinator options. The `pluginManager` already implements `QuerySessionStreams`.

Check the actual method signature on `pluginManager` — the coordinator's `StreamContributor` interface needs to match. The plugin manager's method is:

```go
QuerySessionStreams(ctx context.Context, req plugins.SessionStreamsRequest) []string
```

This takes a struct, not three strings. Define a narrowing adapter or adjust the interface to match. The simplest: make the coordinator's interface take `(ctx, characterID, playerID, sessionID string)` and have the wiring site wrap it.

- [ ] **Step 6: Run tests**

Run: `task test -- ./internal/grpc/focus/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```text
feat(focus): include ambient + plugin-contributed streams in RestoreFocus (B7 task 5)
```

---

## Task 6: Update all Subscribe tests

The existing Subscribe tests in `server_test.go` use `Streams:` and `ReplayFromCursor:` fields that no longer exist. They also construct the old per-stream notification pattern. Update them for the new flow.

**Files:**

- Modify: `internal/grpc/server_test.go`
- Modify: `test/integration/session/session_persistence_integration_test.go`

- [ ] **Step 1: Fix all SubscribeRequest construction**

Search and replace — every `SubscribeRequest` that passes `Streams:` or `ReplayFromCursor:` gets those fields removed:

```go
// Before:
&corev1.SubscribeRequest{
    SessionId:        "sess-1",
    Streams:          []string{"location:abc"},
    ReplayFromCursor: true,
}

// After:
&corev1.SubscribeRequest{
    SessionId: "sess-1",
}
```

- [ ] **Step 2: Update test helpers**

Tests that construct `CoreServer` instances manually may need:
- A `FocusCoordinator` (use a simple one with `NullPolicy`)
- An `EventStore` that implements `SubscribeSession()` (the `MemoryEventStore` already does)

Update `newTestSessionStore` and similar helpers if needed.

- [ ] **Step 3: Remove or rewrite obsolete tests**

Some tests are now obsolete:
- `TestCoreServer_MalformedRequest_InvalidSubscribeStreams` — streams field gone
- `TestCoreServer_Subscribe_NoReplayWhenNotRequested` — replay is always done by server
- Tests that assert specific stream selection behavior — server decides now

Rewrite or remove as appropriate. The new behavior to test:
- Subscribe with valid session → events delivered
- Subscribe with invalid session → error
- Subscribe with detached session → reattach + replay
- REPLAY\_COMPLETE frame emitted
- STREAM\_CLOSED on session destroy
- Mid-session stream updates via control channel

- [ ] **Step 4: Run full test suite**

Run: `task test -- ./internal/grpc/`
Expected: PASS.

- [ ] **Step 5: Commit**

```text
test(grpc): update Subscribe tests for focus substrate clean break (B7 task 6)
```

---

## Task 7: Update web and telnet adapters

Minor changes — both already mostly work since the removed fields were optional.

**Files:**

- Modify: `internal/web/handler.go`
- Modify: `internal/web/handler_test.go`
- Verify: `internal/telnet/gateway_handler.go` (should need no changes)

- [ ] **Step 1: Update web handler**

In `internal/web/handler.go`, the `StreamEvents` method constructs a `SubscribeRequest` with `ReplayFromCursor: req.Msg.GetReplayFromCursor()`. Remove that field since it no longer exists:

```go
sub, err := h.client.Subscribe(ctx, &corev1.SubscribeRequest{
    SessionId: sessionID,
})
```

- [ ] **Step 2: Update web.proto StreamEventsRequest**

Already done in task 1 (reserved field 2). Verify the web handler doesn't reference `GetReplayFromCursor()` anymore.

- [ ] **Step 3: Verify telnet gateway**

The telnet gateway already sends only `SessionId:` — no changes needed. Verify by reading `internal/telnet/gateway_handler.go`.

- [ ] **Step 4: Run adapter tests**

Run: `task test -- ./internal/web/... ./internal/telnet/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```text
feat(web): update StreamEvents for SubscribeRequest clean break (B7 task 7)
```

---

## Task 8: Integration tests and quality gate

- [ ] **Step 1: Run lint**

Run: `task lint`
Expected: Clean.

- [ ] **Step 2: Run full unit test suite**

Run: `task test`
Expected: PASS.

- [ ] **Step 3: Run integration tests**

Run: `task test:int`
Expected: PASS.

- [ ] **Step 4: Fix any issues**

Commit fixes:

```text
fix(grpc): address lint/test issues from B7 quality gate
```
