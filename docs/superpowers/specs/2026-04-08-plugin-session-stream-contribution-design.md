<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Plugin Session Stream Contribution Design

**Issue:** holomush-oirq  
**Date:** 2026-04-08  
**Status:** Approved

---

## Problem

Core's `Subscribe` RPC hardcodes subscriptions to only two streams: the character stream and the
current location stream. Plugins that expose session-scoped streams (channels, guilds, friend feeds,
faction broadcasts) have no mechanism to contribute additional streams at session establishment.

Concrete impact: a channels plugin cannot auto-subscribe a character to their channel memberships on
login. The character misses all channel messages until they explicitly re-subscribe or reconnect.

---

## Requirements

### Functional

- **FR1**: A plugin MUST be able to contribute additional stream names to a session's subscription
  set when the session is established.
- **FR2**: Core's Subscribe handler MUST set up LISTEN subscriptions for plugin-contributed streams
  before the replay phase, preserving the existing LISTEN-before-replay ordering invariant.
- **FR3**: Contribution MUST be scoped to the session's character and player so plugins can filter
  their own membership relations.
- **FR4**: Core MUST NOT hardcode knowledge of specific plugins. The mechanism is a general
  extension point usable by any plugin.
- **FR5**: Mid-session subscription changes (UC2) MUST take effect immediately — joining or leaving
  a channel mid-session MUST start/stop message delivery without reconnecting.

### Non-functional

- **NFR1**: Plugin contribution failures MUST use degraded-subscribe semantics: log the error, skip
  the plugin's contribution, proceed with what succeeded. Session establishment MUST NOT fail due to
  a plugin error.
- **NFR2**: The mechanism MUST be testable end-to-end: a test can assert that a character receives
  messages from a plugin-contributed stream via both replay (UC1) and live-forward (UC2) paths.
- **NFR3**: Per-Subscribe call cost MUST be bounded. Only plugins that explicitly opt in are called.

---

## Design

### Approach

**UC1 (session start):** Add `QuerySessionStreams` RPC to `PluginService` in `plugin.proto`. Core
calls this on all opted-in plugins in parallel before LISTEN setup. Results are merged (deduplicated)
into the stream list before the existing LISTEN loop runs — preserving the ordering invariant.

**UC2 (mid-session changes):** Add `AddSessionStream` / `RemoveSessionStream` to
`HostFunctionsService` in `hostfunc.proto`. The Subscribe goroutine gains a `ctrlCh` that it selects
on alongside `notifyCh`. A `SessionStreamRegistry` maps active session IDs to their control
channels. Plugin calls write to the registry; the goroutine handles them inline.

This approach is uniform across plugin types (Lua and binary both use the same RPC contract), fits
the existing `HandleEvent`/`HandleCommand` pattern, and mirrors how `locationFollower` already
manages live stream switching inside the Subscribe goroutine.

---

## Proto Changes

### `api/proto/holomush/plugin/v1/plugin.proto`

Add to `PluginService`:

```protobuf
// QuerySessionStreams returns stream names to subscribe for a session.
// Called once at session establishment, before LISTEN setup.
// Only called for plugins that declare session_streams: true in their manifest.
rpc QuerySessionStreams(QuerySessionStreamsRequest) returns (QuerySessionStreamsResponse);

message QuerySessionStreamsRequest {
  string character_id = 1;
  string player_id    = 2;
  string session_id   = 3;
}

message QuerySessionStreamsResponse {
  repeated string streams = 1;
  // non-empty = plugin-reported error; host degrades (logs + skips this plugin's contribution)
  string error = 2;
}
```

### `api/proto/holomush/plugin/v1/hostfunc.proto`

Add to `HostFunctionsService`:

```protobuf
// AddSessionStream subscribes an active session to an additional stream mid-session.
// Returns SESSION_NOT_FOUND if session_id is not active (character disconnected).
rpc AddSessionStream(AddSessionStreamRequest) returns (AddSessionStreamResponse);

// RemoveSessionStream unsubscribes an active session from a stream.
// Idempotent: succeeds silently if stream is not subscribed.
rpc RemoveSessionStream(RemoveSessionStreamRequest) returns (RemoveSessionStreamResponse);

message AddSessionStreamRequest {
  string session_id = 1;
  string stream     = 2;
}
message AddSessionStreamResponse {
  bool   success = 1;
  string error   = 2;
}

message RemoveSessionStreamRequest {
  string session_id = 1;
  string stream     = 2;
}
message RemoveSessionStreamResponse {
  bool   success = 1;
  string error   = 2;
}
```

**Error model:** `QuerySessionStreamsResponse.error` is a plugin-reported error string, consistent
with the existing `EmitEventResponse.error` pattern — used for logging only, not gRPC status.
`AddSessionStream`/`RemoveSessionStream` use gRPC status codes for host-side errors (session not
found, invalid stream name) since those are infrastructure errors.

---

## Manifest Changes

### `internal/plugin/manifest.go`

```go
type Manifest struct {
    // ... existing fields ...

    // SessionStreams indicates the plugin implements QuerySessionStreams and wants
    // to contribute streams to session subscriptions.
    // Only valid for lua and binary plugin types.
    SessionStreams bool `yaml:"session_streams,omitempty" json:"session_streams,omitempty"`
}
```

Validation: `session_streams: true` is rejected for `core` and `setting` plugin types with a clear
error message. Core plugins are wired in-process and have no plugin-side code to implement the
callback; setting plugins are content-only.

---

## Host Interface & Manager

### `internal/plugin/host.go`

```go
type SessionStreamsRequest struct {
    CharacterID string
    PlayerID    string
    SessionID   string
}

type Host interface {
    Load(ctx context.Context, manifest *Manifest, dir string) error
    Unload(ctx context.Context, name string) error
    DeliverEvent(ctx context.Context, name string, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error)
    DeliverCommand(ctx context.Context, name string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error)
    QuerySessionStreams(ctx context.Context, name string, req SessionStreamsRequest) ([]string, error) // NEW
    Plugins() []string
    Close(ctx context.Context) error
}
```

### `internal/plugin/manager.go`

`Manager.QuerySessionStreams` fans out to all opted-in plugins in parallel, collects results with
degraded-subscribe semantics, deduplicates, and returns the merged stream list:

```go
func (m *Manager) QuerySessionStreams(ctx context.Context, req SessionStreamsRequest) []string
```

- Collects opted-in plugins under read lock; releases lock before fan-out
- Each plugin called in its own goroutine with the provided context (caller sets timeout)
- Results sent on a buffered channel; all goroutines awaited
- Per-plugin errors: log warning with plugin name + session context, skip contribution
- Invalid stream names (see validation below) dropped after collection
- Deduplication via `map[string]bool` before returning

`CoreServer` receives a `SessionStreamContributor` interface (not `*Manager` directly) to keep
wiring testable:

```go
type SessionStreamContributor interface {
    QuerySessionStreams(ctx context.Context, req plugins.SessionStreamsRequest) []string
}
```

---

## Subscribe Handler Changes

### `internal/grpc/stream_registry.go` (new)

```go
type sessionStreamUpdate struct {
    stream string
    add    bool // true = subscribe, false = unsubscribe
}

// SessionStreamRegistry maps active session IDs to their Subscribe control channels.
// Multiple subscribers per session are supported — each Subscribe call registers
// its own channel, and updates are broadcast to all active subscribers.
type SessionStreamRegistry struct {
    mu       sync.Mutex
    channels map[string]map[chan<- sessionStreamUpdate]struct{}
}

func (r *SessionStreamRegistry) Register(sessionID string, ch chan<- sessionStreamUpdate)
func (r *SessionStreamRegistry) Deregister(sessionID string, ch chan<- sessionStreamUpdate)
func (r *SessionStreamRegistry) Send(sessionID string, update sessionStreamUpdate) error
```

`Send` is non-blocking (select with default). If the control channel buffer (capacity 16) is full,
`Send` returns `CONTROL_CHANNEL_FULL` — the plugin's hostfunc call returns an error and the plugin
can retry. This prevents backpressure from a slow Subscribe goroutine blocking plugin execution.

### `internal/grpc/server.go` — Subscribe

Changes in order within the handler:

1. **After session info resolved, before LISTEN setup:** `CoreServer.Subscribe` wraps `ctx` with a
   2s deadline before calling `s.streamContributor.QuerySessionStreams(contributionCtx, ...)`.
   The Manager uses the provided context as-is — timeout responsibility belongs to the caller.
   Merge returned streams into `staticStreams` (dedup already done by manager).

2. **Per-stream cancel contexts:** each stream (original and plugin-contributed) gets
   `context.WithCancel(ctx)`. Cancel functions stored in `streamCancels map[string]context.CancelFunc`.
   Required for `RemoveSessionStream` to cancel individual stream relay goroutines. Parent ctx
   cancellation (client disconnect) still cascades to all children automatically.

3. **Register control channel before live-forward loop:**

   ```go
   ctrlCh := make(chan sessionStreamUpdate, 16)
   s.streamRegistry.Register(info.ID, ctrlCh)
   defer s.streamRegistry.Deregister(info.ID)
   ```

4. **Select loop gains `ctrlCh` arm:**
   - **Add:** if stream not in `streamCancels`, call `subscribeStreamWithCancel`, then
     `replayAndSend` using `info.EventCursors[stream]` as the cursor (zero if no prior cursor
     exists). This means returning channel members replay only new messages since they last left;
     first-time subscribers replay from the beginning. Mirrors the UC1 cursor logic exactly.
     If stream already subscribed: no-op (idempotent).
   - **Remove:** if stream in `streamCancels`, call cancel, delete from map. Location stream
     (`world.StreamPrefixLocation`) is rejected — owned by `locationFollower`, not plugin-managed.

5. **Test seam:** expose `afterLISTENHook func()` on `CoreServer` (configurable via
   `WithAfterLISTENHook`) that fires between LISTEN setup and replay. Allows integration tests to
   inject an event into the race window and assert it is received exactly once. Mirrors the
   `cursorCommitHook` pattern from PR #201.

---

## Lua Host Changes

### `on_session_subscribe` callback

Lua plugins that declare `session_streams: true` implement an optional global function:

```lua
function on_session_subscribe(character_id, player_id, session_id)
    -- return a table of stream name strings
    return {"channel:abc123", "channel:def456"}
end
```

If `on_session_subscribe` is not defined in a plugin that declares `session_streams: true`, the Lua
host returns an empty list (not an error).

### Stdlib additions (`hostfunc/stdlib_session.go`)

```text
holo.add_session_stream(session_id, stream) → success bool, err string
holo.remove_session_stream(session_id, stream) → success bool, err string
```

Wired through the existing Lua hostfunc adapter pattern (same as `holo.emit_event`).

---

## Binary Plugin Wiring

`GopluginHost.QuerySessionStreams` is a thin RPC wrapper over the new `PluginService` RPC, following
the identical pattern as `HandleEvent` and `HandleCommand`.

`AddSessionStream` / `RemoveSessionStream` on `HostFunctionsService` (binary plugin side) look up
the `SessionStreamRegistry`, write the control update, and return success/error — identical in
structure to the existing `EmitEvent` host function handler.

---

## Stream Name Validation

Applied in `Manager.QuerySessionStreams` before merging, and in `AddSessionStream` hostfunc handler:

- Non-empty
- No whitespace characters
- Contains at least one `:` (all HoloMUSH stream names follow `prefix:id`)
- Maximum 256 characters

Invalid names are dropped with a warning log. ABAC handles stream-level access control separately;
validation here is format-only.

---

## Failure Policy

| Scenario | Behaviour |
|----------|-----------|
| Plugin `QuerySessionStreams` returns error | Log warning (plugin name + session), skip contribution, continue |
| Plugin `QuerySessionStreams` times out (2s) | Same as error |
| All plugins fail | Log error, proceed with core-only streams |
| `AddSessionStream`: session not found | Return `SESSION_NOT_FOUND` gRPC error to plugin |
| `AddSessionStream`: stream already subscribed | No-op, return success |
| `AddSessionStream`: control channel full | Return `CONTROL_CHANNEL_FULL` error; plugin may retry |
| `RemoveSessionStream`: stream not subscribed | No-op, return success (idempotent) |
| `RemoveSessionStream`: location stream | Rejected with `INVALID_STREAM` error |

---

## Testing

### Boundary Tests (unit)

| Test | Covers |
|------|--------|
| `TestManagerQuerySessionStreamsEmptyResult` | Plugin returns empty list — no streams merged |
| `TestManagerQuerySessionStreamsNoOptedInPlugins` | Zero opted-in plugins — returns nil immediately |
| `TestManagerQuerySessionStreamsDuplicateAcrossPlugins` | Two plugins return same stream — deduplicated |
| `TestManagerQuerySessionStreamsAllPluginsFail` | All error — empty result, all logged |
| `TestManagerQuerySessionStreamsTimeout` | Plugin hangs past 2s deadline (deadline set by CoreServer, not Manager) — context cancels, Subscribe unblocked |
| `TestSessionStreamRegistryControlChannelFull` | Buffer full — `Send` returns error, does not block |
| `TestSubscribeAddStreamAlreadySubscribed` | Duplicate add — idempotent, no duplicate LISTEN |
| `TestSubscribeRemoveStreamNotSubscribed` | Remove unknown stream — silent success |
| `TestSubscribeRemoveLocationStream` | Plugin removes location stream — rejected |
| `TestStreamNameValidationRejectsEmpty` | Empty name dropped |
| `TestStreamNameValidationRejectsNoColon` | `"channelabcdef"` dropped |
| `TestStreamNameValidationRejectsWhitespace` | `"channel: abc"` dropped |
| `TestStreamNameValidationRejectsTooLong` | 257-char name dropped |
| `TestManifestSessionStreamsRejectedForCoreType` | `session_streams: true` on core plugin — validation error |
| `TestManifestSessionStreamsRejectedForSettingType` | Same for setting plugin |

### Invariant Tests (unit)

| Test | Invariant |
|------|-----------|
| `TestSubscribeLISTENBeforeReplayWithPluginStreams` | Plugin streams have LISTEN before replay — verified by mock call ordering |
| `TestSubscribePluginStreamsIncludedInReplay` | Every plugin-contributed stream passed to `replayAndSend` |
| `TestAddSessionStreamLISTENBeforeReplay` | Mid-session add: LISTEN before `replayAndSend` |
| `TestSubscribeRegistryCleanedUpOnExit` | `Deregister` called on every Subscribe exit path |
| `TestSubscribeStreamCancelsCleanedUpOnExit` | All per-stream cancels called on exit — no goroutine leaks |
| `TestManagerQuerySessionStreamsNeverCallsOptedOutPlugin` | Opted-out plugin's `QuerySessionStreams` never called |

### End-to-End Tests (integration, Ginkgo/Gomega)

```go
Describe("Plugin session stream contribution", func() {

    Describe("UC1: session-start auto-subscribe", func() {
        It("receives messages posted before login via replay")
        It("subscribes to multiple channels simultaneously")
        It("does not subscribe to channels the character is not a member of")
        It("proceeds normally when channels plugin fails to respond")
    })

    Describe("UC2: mid-session subscription changes", func() {
        It("receives messages immediately after joining a channel mid-session")
        It("receives tail replay of recent messages on mid-session join")
        It("stops receiving messages immediately after leaving a channel")
        It("handles rapid join/leave without message duplication")
    })

    Describe("LISTEN-before-replay invariant", func() {
        It("never loses a message posted in the race window between LISTEN setup and replay", func() {
            // Uses subscribePhaseHook test seam (mirrors cursorCommitHook from PR #201)
            // to inject a message during setup; asserts received exactly once
        })
    })
})
```

---

## Non-Goals

- Stream-level access control (handled by existing ABAC + relation gates)
- Dynamic stream creation or stream lifecycle management
- Client-side changes (web gateway passes empty streams; Subscribe adds everything server-side)
- Caching of `QuerySessionStreams` results (plugins own their membership data; caching is their
  responsibility if needed)

---

## Affected Files

| File | Change |
|------|--------|
| `api/proto/holomush/plugin/v1/plugin.proto` | Add `QuerySessionStreams` RPC + messages |
| `api/proto/holomush/plugin/v1/hostfunc.proto` | Add `AddSessionStream` / `RemoveSessionStream` |
| `internal/plugin/host.go` | Add `QuerySessionStreams` to `Host` interface |
| `internal/plugin/manifest.go` | Add `SessionStreams` field + validation |
| `internal/plugin/manager.go` | Add `QuerySessionStreams` fan-out method |
| `internal/plugin/lua/` | Wire `on_session_subscribe` callback + stdlib hostfuncs |
| `internal/plugin/goplugin/` | Wire binary plugin `QuerySessionStreams` RPC |
| `internal/plugin/hostfunc/functions.go` | Handle `AddSessionStream` / `RemoveSessionStream` |
| `internal/grpc/server.go` | Collect plugin streams; ctrlCh select arm; per-stream cancels; test hook |
| `internal/grpc/stream_registry.go` | New: `SessionStreamRegistry` |

---

## Related

- holomush-oirq — this issue
- holomush-0sc.12 — Channel plugin rework (blocked by this)
