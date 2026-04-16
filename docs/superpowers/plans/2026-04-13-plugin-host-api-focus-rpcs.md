<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Plugin Host API — Focus RPCs + ReplayMode + Lua Hostfuncs

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose JoinFocus, LeaveFocus, PresentFocus, and QueryStreamHistory to
plugins (binary via gRPC, Lua via hostfuncs), and extend AddSessionStream with
replay-mode support, so plugins can declare focus intent and read stream history.

**Architecture:** The FocusCoordinator already implements the Go-side logic
(B4/B6). This bead adds the plugin-facing API layer: proto definitions, gRPC
handler implementations in `host_service.go` that delegate to the coordinator,
Lua hostfunc bindings that call the same coordinator, and a replay\_mode field
on AddSessionStreamRequest for ambient LIVE\_ONLY subscriptions. As a
prerequisite, `ReplayMode` moves from `internal/grpc/focus` to
`internal/session` — it's a session-level cursor-initialization concept, not a
focus-specific one, and the move lets every consumer import it without layering
hacks.

**Tech Stack:** protobuf (buf), Go, gopher-lua, testify, mockery

**Spec:** `docs/superpowers/specs/2026-04-11-focus-substrate-design.md` §3.4
(proto), §4.3 (join flow), §4.4 (leave flow), §4.5 (present flow), §4.6
(ambient add with LIVE\_ONLY)

**Epic:** `holomush-oy6e` (Server-Owned Focus Substrate)
**Bead:** `holomush-oy6e.8`

---

## File Structure

| File                                                  | Action | Responsibility                                                                                                                                                                            |
| ----------------------------------------------------- | ------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/session/replay_mode.go`                     | Create | `ReplayMode` enum + `String()` — shared vocabulary for cursor-initialization semantics                                                                                                    |
| `api/proto/holomush/plugin/v1/plugin.proto`           | Modify | Add FocusKind enum, FocusKey message, StreamReplayMode enum, replay\_mode on AddSessionStreamRequest, JoinFocus/LeaveFocus/PresentFocus/QueryStreamHistory RPCs + request/response messages |
| `internal/plugin/host.go`                             | Modify | Extend `StreamRegistry` interface with `AddStreamWithMode(ctx, sessionID, stream, session.ReplayMode)`                                                                                    |
| `internal/grpc/stream_registry.go`                    | Modify | Implement `AddStreamWithMode`, update imports from `focus.ReplayMode` → `session.ReplayMode`                                                                                              |
| `internal/grpc/focus/kind_policy.go`                  | Modify | Remove `ReplayMode` definition, import from `session` instead                                                                                                                             |
| `internal/plugin/goplugin/host_service.go`            | Modify | Add gRPC handler implementations for the four new RPCs, using `focus.Coordinator` and `core.EventStore` directly                                                                          |
| `internal/plugin/goplugin/host_service_test.go`       | Create | Unit tests for all four new gRPC handlers                                                                                                                                                 |
| `internal/plugin/goplugin/host.go`                    | Modify | Add `WithFocusCoordinator` and `WithEventStoreReader` options, fields on `Host`                                                                                                           |
| `internal/plugin/hostfunc/stdlib_focus.go`            | Create | Lua hostfunc bindings: `holomush.join_focus`, `holomush.leave_focus`, `holomush.present_focus`, `holomush.query_stream_history`                                                           |
| `internal/plugin/hostfunc/stdlib_focus_test.go`       | Create | Unit tests for all four Lua hostfuncs                                                                                                                                                     |
| `internal/plugin/hostfunc/functions.go`               | Modify | Add `WithFocusOps` + `WithHistoryReader` options, call `RegisterFocusFuncs` in `Register`                                                                                                 |
| `cmd/holomush/sub_grpc.go`                            | Modify | Wire focus coordinator + event store into both plugin hosts                                                                                                                               |

---

## Task 1: Proto Additions — Enums, Messages, RPCs

**Files:**

- Modify: `api/proto/holomush/plugin/v1/plugin.proto:34-51` (PluginHostService), `267-293` (AddSessionStreamRequest)

This task adds all proto definitions. The generated Go code is used by all subsequent tasks.

- [ ] **Step 1: Add FocusKind enum, FocusKey message, StreamReplayMode enum**

Add these definitions after the existing `QuerySessionStreamsResponse` message (after line 311) in `api/proto/holomush/plugin/v1/plugin.proto`:

```protobuf
// FocusKind enumerates the types of focused contexts a character can
// participate in. Adding a new kind requires: (a) a new constant here,
// (b) a matching session.FocusKind constant in Go, (c) a new
// FocusKindPolicy implementation registered in the coordinator.
enum FocusKind {
  FOCUS_KIND_UNSPECIFIED = 0;
  FOCUS_KIND_SCENE = 1;
}

// FocusKey identifies a focus membership within a session. A session's
// focus memberships are unique by (kind, target_id) pair.
message FocusKey {
  FocusKind kind = 1;
  string target_id = 2 [(buf.validate.field).string.min_len = 1];
}

// StreamReplayMode controls how a stream subscription's initial replay
// behaves when added via AddSessionStream.
enum StreamReplayMode {
  STREAM_REPLAY_MODE_UNSPECIFIED = 0;
  STREAM_REPLAY_MODE_FROM_CURSOR = 1;
  STREAM_REPLAY_MODE_LIVE_ONLY = 2;
}
```

- [ ] **Step 2: Add replay\_mode field to AddSessionStreamRequest**

Replace the existing `PluginHostServiceAddSessionStreamRequest` and response messages (around line 268) with validated versions plus the new field:

```protobuf
message PluginHostServiceAddSessionStreamRequest {
  // Active session identifier.
  string session_id = 1 [(buf.validate.field).string.min_len = 1];
  // Stream name to subscribe to (format: "prefix:id").
  string stream = 2 [(buf.validate.field).string.min_len = 1];
  // replay_mode controls initial replay. Optional; defaults to
  // FROM_CURSOR if unspecified for backwards compatibility.
  StreamReplayMode replay_mode = 3;
}

message PluginHostServiceAddSessionStreamResponse {}
```

Similarly update RemoveSessionStream messages with validation:

```protobuf
message PluginHostServiceRemoveSessionStreamRequest {
  string session_id = 1 [(buf.validate.field).string.min_len = 1];
  string stream = 2 [(buf.validate.field).string.min_len = 1];
}

message PluginHostServiceRemoveSessionStreamResponse {}
```

Note: removing the `success` bool field from responses is a proto-compatible
change (field 1 is just ignored by new clients). The `success` field was never
checked — callers already use the gRPC error for failure detection.

- [ ] **Step 3: Add JoinFocus, LeaveFocus, PresentFocus, QueryStreamHistory RPCs**

In the `PluginHostService` service definition (around line 35), add after `RemoveSessionStream`:

```protobuf
  // JoinFocus adds a focus membership to an active or detached session.
  // Plugins declare intent; the server applies kind-specific replay policy.
  rpc JoinFocus(PluginHostServiceJoinFocusRequest)
      returns (PluginHostServiceJoinFocusResponse);

  // LeaveFocus removes a focus membership. Idempotent on non-member.
  rpc LeaveFocus(PluginHostServiceLeaveFocusRequest)
      returns (PluginHostServiceLeaveFocusResponse);

  // PresentFocus updates the session's PresentingFocus pointer.
  // Target MUST already exist in FocusMemberships.
  rpc PresentFocus(PluginHostServicePresentFocusRequest)
      returns (PluginHostServicePresentFocusResponse);

  // QueryStreamHistory reads the tail of a stream for plugin-side display.
  // Read-only: does not advance cursors or affect session state.
  // Count capped at 500 server-side.
  rpc QueryStreamHistory(PluginHostServiceQueryStreamHistoryRequest)
      returns (PluginHostServiceQueryStreamHistoryResponse);
```

- [ ] **Step 4: Add request/response messages for the new RPCs**

Add after the `PluginHostServiceRemoveSessionStreamResponse` message:

```protobuf
message PluginHostServiceJoinFocusRequest {
  string session_id = 1 [(buf.validate.field).string.min_len = 1];
  FocusKey target = 2;
}
message PluginHostServiceJoinFocusResponse {}

message PluginHostServiceLeaveFocusRequest {
  string session_id = 1 [(buf.validate.field).string.min_len = 1];
  FocusKey target = 2;
}
message PluginHostServiceLeaveFocusResponse {}

message PluginHostServicePresentFocusRequest {
  string session_id = 1 [(buf.validate.field).string.min_len = 1];
  FocusKey target = 2;
}
message PluginHostServicePresentFocusResponse {}

message PluginHostServiceQueryStreamHistoryRequest {
  string stream = 1 [(buf.validate.field).string.min_len = 1];
  int32 count = 2;
  // Epoch milliseconds. Events before this time are excluded. 0 means no lower bound.
  int64 not_before_ms = 3;
}

message PluginHostServiceQueryStreamHistoryResponse {
  repeated Event events = 1;
}
```

- [ ] **Step 5: Generate Go code from proto**

Run: `task proto:generate` (or `buf generate` depending on the project's Taskfile)

Expected: New generated files in `pkg/proto/holomush/plugin/v1/` with the new types and service methods.

- [ ] **Step 6: Verify generated code compiles**

Run: `task build`

Expected: BUILD SUCCESS. The `UnimplementedPluginHostServiceServer` in the generated code now includes stubs for JoinFocus, LeaveFocus, PresentFocus, QueryStreamHistory. The existing `host_service.go` embeds this type, so it compiles immediately (unimplemented methods return "not implemented" errors by default).

- [ ] **Step 7: Commit**

```text
feat(proto): add focus RPCs + StreamReplayMode to PluginHostService (B8)

Add JoinFocus, LeaveFocus, PresentFocus, QueryStreamHistory RPCs to the
plugin host service. Add FocusKind enum, FocusKey message, and
StreamReplayMode enum. Extend AddSessionStreamRequest with replay_mode.
```

---

## Task 2: Move ReplayMode to internal/session

**Files:**

- Create: `internal/session/replay_mode.go`
- Modify: `internal/grpc/focus/kind_policy.go` — remove ReplayMode definition, re-export alias
- Modify: all files importing `focus.ReplayMode` — update to `session.ReplayMode`

`ReplayMode` describes how to initialize a per-session cursor when subscribing
to a stream. Cursors live on `session.Info.EventCursors`, so the instruction
for initializing them belongs in `internal/session`. This lets every consumer
(`internal/grpc`, `internal/grpc/focus`, `internal/plugin/*`) import it
without layering hacks or type erasure.

- [ ] **Step 1: Create `internal/session/replay_mode.go`**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session

import "fmt"

// ReplayMode controls how the live loop replays events when processing a
// stream addition. It determines cursor-initialization semantics:
// FROM_CURSOR reads the existing watermark, LIVE_ONLY advances to tail,
// BOUNDED_TAIL sets a new baseline from the last N events.
//
// Produced by focus.KindPolicy implementations and the FocusCoordinator.
// Consumed by the Subscribe live loop in internal/grpc.
type ReplayMode int

const (
	// ReplayModeFromCursor replays from the stored per-stream cursor in
	// session.Info.EventCursors, or from ULID zero if no cursor is set.
	ReplayModeFromCursor ReplayMode = iota

	// ReplayModeBoundedTail replays the most recent TailCount events on
	// the stream (optionally bounded by NotBefore), then advances the
	// cursor to the tail. Used by scene focus-switch IC catch-up.
	ReplayModeBoundedTail

	// ReplayModeLiveOnly advances the cursor to the current stream tail
	// without replaying anything. Used by channels for mid-session joins.
	ReplayModeLiveOnly
)

// String returns a human-readable name for the replay mode.
func (m ReplayMode) String() string {
	switch m {
	case ReplayModeFromCursor:
		return "from_cursor"
	case ReplayModeBoundedTail:
		return "bounded_tail"
	case ReplayModeLiveOnly:
		return "live_only"
	default:
		return fmt.Sprintf("unknown(%d)", int(m))
	}
}
```

- [ ] **Step 2: Update `internal/grpc/focus/kind_policy.go`**

Remove the `ReplayMode` type, constants, and `String()` method (lines 25-54).
Replace with a type alias that re-exports from session, preserving all
existing references within the `focus` package:

```go
import (
	"time"

	"github.com/holomush/holomush/internal/session"
)

// ReplayMode is an alias for session.ReplayMode. Defined here for
// backward compatibility with existing focus package consumers.
// New code SHOULD import session.ReplayMode directly.
type ReplayMode = session.ReplayMode

// Re-export constants so existing focus.ReplayModeXxx references compile.
const (
	ReplayModeFromCursor  = session.ReplayModeFromCursor
	ReplayModeBoundedTail = session.ReplayModeBoundedTail
	ReplayModeLiveOnly    = session.ReplayModeLiveOnly
)
```

Remove the `"fmt"` import if it was only used by the deleted `String()` method.

The `StreamWithMode`, `PolicyContext`, and `KindPolicy` types stay in
`focus/kind_policy.go` unchanged — they reference `ReplayMode` which is now the
alias, so all existing code compiles without modification.

- [ ] **Step 3: Verify compilation and tests**

Run: `task build && task test`

Expected: BUILD SUCCESS, all tests pass. The type alias means every existing
`focus.ReplayMode` reference resolves to `session.ReplayMode` transparently —
zero call-site changes needed in this step.

- [ ] **Step 4: Commit**

```text
refactor(session): move ReplayMode to internal/session (B8)

ReplayMode describes cursor-initialization semantics — how to set up a
per-session watermark when subscribing to a stream. Cursors live on
session.Info.EventCursors, so the type belongs in the session package.

The focus package re-exports via type alias for backward compatibility.
This unblocks internal/plugin/* importing ReplayMode without layering
hacks or type erasure.
```

---

## Task 3: Extend StreamRegistry Interface with ReplayMode

**Files:**

- Modify: `internal/plugin/host.go:26-32` (StreamRegistry interface)
- Modify: `internal/grpc/stream_registry.go:91-94` (AddStream implementation)

The existing `plugins.StreamRegistry.AddStream` doesn't pass a replay mode. We
need `AddStreamWithMode` for the `LIVE_ONLY` ambient-add flow (spec §4.6).

- [ ] **Step 1: Write failing test for AddStreamWithMode**

Append to `internal/grpc/stream_registry_test.go`:

```go
func TestAddStreamWithModeSendsReplayModeToControlChannel(t *testing.T) {
	reg := NewSessionStreamRegistry()
	ch := make(chan sessionStreamUpdate, 4)
	reg.Register("sess-1", ch)
	defer reg.Deregister("sess-1", ch)

	err := reg.AddStreamWithMode(context.Background(), "sess-1", "channel:x", session.ReplayModeLiveOnly)
	require.NoError(t, err)

	update := <-ch
	assert.Equal(t, "channel:x", update.stream)
	assert.True(t, update.add)
	assert.Equal(t, session.ReplayModeLiveOnly, update.replayMode)
}
```

Add import for `"github.com/holomush/holomush/internal/session"` at the top of the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestAddStreamWithModeSendsReplayModeToControlChannel ./internal/grpc/`

Expected: FAIL — `AddStreamWithMode` method does not exist.

- [ ] **Step 3: Add AddStreamWithMode to StreamRegistry interface**

In `internal/plugin/host.go`, extend the `StreamRegistry` interface:

```go
import (
	"context"

	"github.com/holomush/holomush/internal/session"
)

// StreamRegistry allows plugins to modify session stream subscriptions mid-session.
type StreamRegistry interface {
	// AddStream subscribes a session to an additional stream with default FROM_CURSOR replay.
	AddStream(ctx context.Context, sessionID, stream string) error
	// AddStreamWithMode subscribes with an explicit replay mode (e.g., LIVE_ONLY for channels).
	AddStreamWithMode(ctx context.Context, sessionID, stream string, mode session.ReplayMode) error
	// RemoveStream unsubscribes a session from a stream. Idempotent.
	RemoveStream(ctx context.Context, sessionID, stream string) error
}
```

- [ ] **Step 4: Implement AddStreamWithMode on SessionStreamRegistry**

In `internal/grpc/stream_registry.go`, add:

```go
// AddStreamWithMode implements plugins.StreamRegistry. Subscribes with explicit replay mode.
func (r *SessionStreamRegistry) AddStreamWithMode(_ context.Context, sessionID, stream string, mode session.ReplayMode) error {
	return r.Send(sessionID, sessionStreamUpdate{
		stream:     stream,
		add:        true,
		replayMode: mode,
	})
}
```

Update the import of `focus` to `session` in `stream_registry.go` for the
`replayMode` field type on `sessionStreamUpdate`. The `sessionStreamUpdate`
struct field type changes from `focus.ReplayMode` to `session.ReplayMode` —
these are the same type via the alias, so no other code changes.

- [ ] **Step 5: Run test to verify it passes**

Run: `task test -- -run TestAddStreamWithModeSendsReplayModeToControlChannel ./internal/grpc/`

Expected: PASS

- [ ] **Step 6: Commit**

```text
feat(grpc): add AddStreamWithMode to StreamRegistry interface (B8)

Extends the StreamRegistry interface with replay-mode-aware stream
addition using session.ReplayMode directly. No type erasure needed
since ReplayMode now lives in the session package.
```

---

## Task 4: Inject Dependencies into Binary Plugin Host

**Files:**

- Modify: `internal/plugin/goplugin/host_service.go` — add fields, update constructor
- Modify: `internal/plugin/goplugin/host.go` — add option functions, fields on Host

The gRPC handlers need `focus.Coordinator` (for JoinFocus/LeaveFocus/PresentFocus)
and `core.EventStore` (for QueryStreamHistory's ReplayTail). Since there are no
import cycles, we use those interfaces directly — no narrow-interface wrapper file.

- [ ] **Step 1: Add fields to pluginHostServiceServer**

In `internal/plugin/goplugin/host_service.go`, update the struct and constructor:

```go
import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/session"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

type pluginHostServiceServer struct {
	pluginv1.UnimplementedPluginHostServiceServer
	host             *Host
	pluginName       string
	focusCoordinator focus.Coordinator
	eventStore       core.EventStore
}

func newPluginHostServiceServer(host *Host, pluginName string) func([]grpc.ServerOption) *grpc.Server {
	return func(opts []grpc.ServerOption) *grpc.Server {
		server := grpc.NewServer(opts...)
		pluginv1.RegisterPluginHostServiceServer(server, &pluginHostServiceServer{
			host:             host,
			pluginName:       pluginName,
			focusCoordinator: host.focusCoordinator,
			eventStore:       host.eventStore,
		})
		return server
	}
}
```

Note: the constructor signature stays the same `(host *Host, pluginName string)`.
The new deps are read from `host` fields — no signature change, no call-site update.

- [ ] **Step 2: Add Host fields and option functions**

In `internal/plugin/goplugin/host.go`, add to the `Host` struct:

```go
type Host struct {
	// ... existing fields ...
	focusCoordinator focus.Coordinator
	eventStore       core.EventStore
}
```

Add option functions:

```go
// WithFocusCoordinator configures the host to inject a focus coordinator
// into the plugin host service for JoinFocus/LeaveFocus/PresentFocus RPCs.
func WithFocusCoordinator(fc focus.Coordinator) HostOption {
	return func(h *Host) { h.focusCoordinator = fc }
}

// WithEventStore configures the host to inject an event store for
// QueryStreamHistory RPCs.
func WithEventStore(es core.EventStore) HostOption {
	return func(h *Host) { h.eventStore = es }
}
```

Add imports for `focus` and `core`.

- [ ] **Step 3: Verify compilation**

Run: `task build`

Expected: BUILD SUCCESS. Existing tests pass since the new fields default to nil.

- [ ] **Step 4: Commit**

```text
refactor(goplugin): add focus coordinator + event store deps to host service (B8)

Uses focus.Coordinator and core.EventStore interfaces directly — no
narrow-interface wrapper needed since there are no import cycles.
Dependencies flow through Host option functions.
```

---

## Task 5: JoinFocus gRPC Handler

**Files:**

- Modify: `internal/plugin/goplugin/host_service.go`
- Create: `internal/plugin/goplugin/host_service_test.go`

- [ ] **Step 1: Write failing tests for JoinFocus handler**

Create `internal/plugin/goplugin/host_service_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/session"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// stubCoordinator records calls for assertion.
type stubCoordinator struct {
	joinCalls    []focusCall
	leaveCalls   []focusCall
	presentCalls []focusCall
	joinErr      error
	leaveErr     error
	presentErr   error
}

type focusCall struct {
	sessionID string
	key       session.FocusKey
}

func (s *stubCoordinator) JoinFocus(_ context.Context, sid string, target session.FocusKey) error {
	s.joinCalls = append(s.joinCalls, focusCall{sid, target})
	return s.joinErr
}

func (s *stubCoordinator) LeaveFocus(_ context.Context, sid string, target session.FocusKey) error {
	s.leaveCalls = append(s.leaveCalls, focusCall{sid, target})
	return s.leaveErr
}

func (s *stubCoordinator) PresentFocus(_ context.Context, sid string, target session.FocusKey) error {
	s.presentCalls = append(s.presentCalls, focusCall{sid, target})
	return s.presentErr
}

func (s *stubCoordinator) RestoreFocus(_ context.Context, _ string) (focus.RestorePlan, error) {
	return focus.RestorePlan{}, nil
}

var _ focus.Coordinator = (*stubCoordinator)(nil)

// stubEventStore implements core.EventStore with only ReplayTail wired.
type stubEventStore struct {
	core.EventStore // embed to satisfy interface; panics on unimplemented methods
	replayTailCalls []replayTailCall
	replayTailResult []core.Event
	replayTailErr    error
}

type replayTailCall struct {
	stream    string
	count     int
	notBefore time.Time
}

func (s *stubEventStore) ReplayTail(_ context.Context, stream string, count int, notBefore time.Time) ([]core.Event, error) {
	s.replayTailCalls = append(s.replayTailCalls, replayTailCall{stream, count, notBefore})
	return s.replayTailResult, s.replayTailErr
}

func newTestServer(fc focus.Coordinator, es core.EventStore) *pluginHostServiceServer {
	return &pluginHostServiceServer{
		pluginName:       "test-plugin",
		focusCoordinator: fc,
		eventStore:       es,
	}
}

func TestJoinFocusDelegatesToCoordinatorWithParsedFocusKey(t *testing.T) {
	fc := &stubCoordinator{}
	srv := newTestServer(fc, nil)

	targetID := ulid.Make()
	resp, err := srv.JoinFocus(context.Background(), &pluginv1.PluginHostServiceJoinFocusRequest{
		SessionId: "sess-1",
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: targetID.String(),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	require.Len(t, fc.joinCalls, 1)
	assert.Equal(t, "sess-1", fc.joinCalls[0].sessionID)
	assert.Equal(t, session.FocusKindScene, fc.joinCalls[0].key.Kind)
	assert.Equal(t, targetID, fc.joinCalls[0].key.TargetID)
}

func TestJoinFocusReturnsErrorWhenCoordinatorFails(t *testing.T) {
	fc := &stubCoordinator{joinErr: oops.Code("FOCUS_ALREADY_MEMBER").Errorf("already member")}
	srv := newTestServer(fc, nil)

	_, err := srv.JoinFocus(context.Background(), &pluginv1.PluginHostServiceJoinFocusRequest{
		SessionId: "sess-1",
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: ulid.Make().String(),
		},
	})
	require.Error(t, err)
}

func TestJoinFocusReturnsErrorWhenCoordinatorIsNil(t *testing.T) {
	srv := newTestServer(nil, nil)

	_, err := srv.JoinFocus(context.Background(), &pluginv1.PluginHostServiceJoinFocusRequest{
		SessionId: "sess-1",
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: ulid.Make().String(),
		},
	})
	require.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestJoinFocus ./internal/plugin/goplugin/`

Expected: FAIL — `JoinFocus` method returns "not implemented" from embedded stub.

- [ ] **Step 3: Implement JoinFocus handler + helpers**

In `internal/plugin/goplugin/host_service.go`, add:

```go
func (s *pluginHostServiceServer) JoinFocus(ctx context.Context, req *pluginv1.PluginHostServiceJoinFocusRequest) (*pluginv1.PluginHostServiceJoinFocusResponse, error) {
	if s.focusCoordinator == nil {
		return nil, oops.With("plugin", s.pluginName).New("focus coordinator not configured")
	}

	key, err := protoToFocusKey(req.GetTarget())
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}

	if err := s.focusCoordinator.JoinFocus(ctx, req.GetSessionId(), key); err != nil {
		return nil, oops.With("plugin", s.pluginName).With("session_id", req.GetSessionId()).Wrap(err)
	}
	return &pluginv1.PluginHostServiceJoinFocusResponse{}, nil
}

// protoToFocusKey converts a proto FocusKey to the session.FocusKey domain type.
func protoToFocusKey(pk *pluginv1.FocusKey) (session.FocusKey, error) {
	if pk == nil {
		return session.FocusKey{}, oops.Errorf("focus key is required")
	}

	targetID, err := ulid.Parse(pk.GetTargetId())
	if err != nil {
		return session.FocusKey{}, oops.With("target_id", pk.GetTargetId()).Wrap(err)
	}

	kind, err := protoToFocusKind(pk.GetKind())
	if err != nil {
		return session.FocusKey{}, err
	}

	return session.FocusKey{Kind: kind, TargetID: targetID}, nil
}

// protoToFocusKind maps proto FocusKind to session.FocusKind.
func protoToFocusKind(pk pluginv1.FocusKind) (session.FocusKind, error) {
	switch pk {
	case pluginv1.FocusKind_FOCUS_KIND_SCENE:
		return session.FocusKindScene, nil
	default:
		return "", oops.Code("FOCUS_KIND_UNREGISTERED").
			With("kind", pk.String()).
			Errorf("unsupported focus kind: %s", pk.String())
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -run TestJoinFocus ./internal/plugin/goplugin/`

Expected: PASS (all 3 tests)

- [ ] **Step 5: Commit**

```text
feat(goplugin): implement JoinFocus gRPC handler (B8)

Delegates to focus.Coordinator with proto-to-domain type conversion.
Includes protoToFocusKey and protoToFocusKind helpers shared by all
focus RPC handlers.
```

---

## Task 6: LeaveFocus + PresentFocus gRPC Handlers

**Files:**

- Modify: `internal/plugin/goplugin/host_service.go`
- Modify: `internal/plugin/goplugin/host_service_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/plugin/goplugin/host_service_test.go`:

```go
func TestLeaveFocusDelegatesToCoordinatorWithParsedFocusKey(t *testing.T) {
	fc := &stubCoordinator{}
	srv := newTestServer(fc, nil)

	targetID := ulid.Make()
	resp, err := srv.LeaveFocus(context.Background(), &pluginv1.PluginHostServiceLeaveFocusRequest{
		SessionId: "sess-1",
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: targetID.String(),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	require.Len(t, fc.leaveCalls, 1)
	assert.Equal(t, "sess-1", fc.leaveCalls[0].sessionID)
	assert.Equal(t, session.FocusKindScene, fc.leaveCalls[0].key.Kind)
	assert.Equal(t, targetID, fc.leaveCalls[0].key.TargetID)
}

func TestLeaveFocusReturnsErrorWhenCoordinatorIsNil(t *testing.T) {
	srv := newTestServer(nil, nil)

	_, err := srv.LeaveFocus(context.Background(), &pluginv1.PluginHostServiceLeaveFocusRequest{
		SessionId: "sess-1",
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: ulid.Make().String(),
		},
	})
	require.Error(t, err)
}

func TestPresentFocusDelegatesToCoordinatorWithParsedFocusKey(t *testing.T) {
	fc := &stubCoordinator{}
	srv := newTestServer(fc, nil)

	targetID := ulid.Make()
	resp, err := srv.PresentFocus(context.Background(), &pluginv1.PluginHostServicePresentFocusRequest{
		SessionId: "sess-1",
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: targetID.String(),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	require.Len(t, fc.presentCalls, 1)
	assert.Equal(t, "sess-1", fc.presentCalls[0].sessionID)
	assert.Equal(t, session.FocusKindScene, fc.presentCalls[0].key.Kind)
	assert.Equal(t, targetID, fc.presentCalls[0].key.TargetID)
}

func TestPresentFocusReturnsErrorWhenCoordinatorIsNil(t *testing.T) {
	srv := newTestServer(nil, nil)

	_, err := srv.PresentFocus(context.Background(), &pluginv1.PluginHostServicePresentFocusRequest{
		SessionId: "sess-1",
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: ulid.Make().String(),
		},
	})
	require.Error(t, err)
}

func TestPresentFocusReturnsErrorWhenCoordinatorFails(t *testing.T) {
	fc := &stubCoordinator{presentErr: oops.Code("FOCUS_NOT_MEMBER").Errorf("not member")}
	srv := newTestServer(fc, nil)

	_, err := srv.PresentFocus(context.Background(), &pluginv1.PluginHostServicePresentFocusRequest{
		SessionId: "sess-1",
		Target: &pluginv1.FocusKey{
			Kind:     pluginv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: ulid.Make().String(),
		},
	})
	require.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run "TestLeaveFocus|TestPresentFocus" ./internal/plugin/goplugin/`

Expected: FAIL — methods not implemented.

- [ ] **Step 3: Implement LeaveFocus and PresentFocus handlers**

In `internal/plugin/goplugin/host_service.go`, add:

```go
func (s *pluginHostServiceServer) LeaveFocus(ctx context.Context, req *pluginv1.PluginHostServiceLeaveFocusRequest) (*pluginv1.PluginHostServiceLeaveFocusResponse, error) {
	if s.focusCoordinator == nil {
		return nil, oops.With("plugin", s.pluginName).New("focus coordinator not configured")
	}

	key, err := protoToFocusKey(req.GetTarget())
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}

	if err := s.focusCoordinator.LeaveFocus(ctx, req.GetSessionId(), key); err != nil {
		return nil, oops.With("plugin", s.pluginName).With("session_id", req.GetSessionId()).Wrap(err)
	}
	return &pluginv1.PluginHostServiceLeaveFocusResponse{}, nil
}

func (s *pluginHostServiceServer) PresentFocus(ctx context.Context, req *pluginv1.PluginHostServicePresentFocusRequest) (*pluginv1.PluginHostServicePresentFocusResponse, error) {
	if s.focusCoordinator == nil {
		return nil, oops.With("plugin", s.pluginName).New("focus coordinator not configured")
	}

	key, err := protoToFocusKey(req.GetTarget())
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}

	if err := s.focusCoordinator.PresentFocus(ctx, req.GetSessionId(), key); err != nil {
		return nil, oops.With("plugin", s.pluginName).With("session_id", req.GetSessionId()).Wrap(err)
	}
	return &pluginv1.PluginHostServicePresentFocusResponse{}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -run "TestLeaveFocus|TestPresentFocus" ./internal/plugin/goplugin/`

Expected: PASS (all 5 tests)

- [ ] **Step 5: Commit**

```text
feat(goplugin): implement LeaveFocus + PresentFocus gRPC handlers (B8)

Same delegation pattern as JoinFocus. LeaveFocus is idempotent.
PresentFocus validates membership via the coordinator.
```

---

## Task 7: QueryStreamHistory gRPC Handler

**Files:**

- Modify: `internal/plugin/goplugin/host_service.go`
- Modify: `internal/plugin/goplugin/host_service_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/plugin/goplugin/host_service_test.go`:

```go
func TestQueryStreamHistoryDelegatesToEventStore(t *testing.T) {
	es := &stubEventStore{
		replayTailResult: []core.Event{
			{Stream: "channel:abc", Type: "say", Payload: `{"text":"hi"}`},
		},
	}
	srv := newTestServer(nil, es)

	resp, err := srv.QueryStreamHistory(context.Background(), &pluginv1.PluginHostServiceQueryStreamHistoryRequest{
		Stream: "channel:abc",
		Count:  10,
	})
	require.NoError(t, err)
	require.Len(t, resp.GetEvents(), 1)
	assert.Equal(t, "channel:abc", resp.GetEvents()[0].GetStream())

	require.Len(t, es.replayTailCalls, 1)
	assert.Equal(t, "channel:abc", es.replayTailCalls[0].stream)
	assert.Equal(t, 10, es.replayTailCalls[0].count)
}

func TestQueryStreamHistoryCapsCountAt500(t *testing.T) {
	es := &stubEventStore{}
	srv := newTestServer(nil, es)

	_, err := srv.QueryStreamHistory(context.Background(), &pluginv1.PluginHostServiceQueryStreamHistoryRequest{
		Stream: "channel:abc",
		Count:  1000,
	})
	require.NoError(t, err)

	require.Len(t, es.replayTailCalls, 1)
	assert.Equal(t, 500, es.replayTailCalls[0].count)
}

func TestQueryStreamHistoryConvertsNotBeforeMs(t *testing.T) {
	es := &stubEventStore{}
	srv := newTestServer(nil, es)

	_, err := srv.QueryStreamHistory(context.Background(), &pluginv1.PluginHostServiceQueryStreamHistoryRequest{
		Stream:      "channel:abc",
		Count:       10,
		NotBeforeMs: 1700000000000,
	})
	require.NoError(t, err)

	require.Len(t, es.replayTailCalls, 1)
	assert.Equal(t, time.UnixMilli(1700000000000).UTC(), es.replayTailCalls[0].notBefore)
}

func TestQueryStreamHistoryReturnsErrorWhenEventStoreIsNil(t *testing.T) {
	srv := newTestServer(nil, nil)

	_, err := srv.QueryStreamHistory(context.Background(), &pluginv1.PluginHostServiceQueryStreamHistoryRequest{
		Stream: "channel:abc",
		Count:  10,
	})
	require.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestQueryStreamHistory ./internal/plugin/goplugin/`

Expected: FAIL — method not implemented.

- [ ] **Step 3: Implement QueryStreamHistory handler**

In `internal/plugin/goplugin/host_service.go`, add:

```go
const maxQueryStreamHistoryCount = 500

func (s *pluginHostServiceServer) QueryStreamHistory(ctx context.Context, req *pluginv1.PluginHostServiceQueryStreamHistoryRequest) (*pluginv1.PluginHostServiceQueryStreamHistoryResponse, error) {
	if s.eventStore == nil {
		return nil, oops.With("plugin", s.pluginName).New("event store not configured")
	}

	count := int(req.GetCount())
	if count > maxQueryStreamHistoryCount {
		count = maxQueryStreamHistoryCount
	}

	var notBefore time.Time
	if req.GetNotBeforeMs() > 0 {
		notBefore = time.UnixMilli(req.GetNotBeforeMs()).UTC()
	}

	events, err := s.eventStore.ReplayTail(ctx, req.GetStream(), count, notBefore)
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).With("stream", req.GetStream()).Wrap(err)
	}

	protoEvents := make([]*pluginv1.Event, 0, len(events))
	for _, e := range events {
		protoEvents = append(protoEvents, coreEventToProto(e))
	}
	return &pluginv1.PluginHostServiceQueryStreamHistoryResponse{Events: protoEvents}, nil
}

// coreEventToProto converts a core.Event to the plugin proto Event.
func coreEventToProto(e core.Event) *pluginv1.Event {
	return &pluginv1.Event{
		Id:        e.ID.String(),
		Stream:    e.Stream,
		Type:      string(e.Type),
		Timestamp: e.Timestamp.UnixMilli(),
		ActorKind: e.Actor.Kind.String(),
		ActorId:   e.Actor.ID,
		Payload:   e.Payload,
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -run TestQueryStreamHistory ./internal/plugin/goplugin/`

Expected: PASS (all 4 tests)

- [ ] **Step 5: Commit**

```text
feat(goplugin): implement QueryStreamHistory gRPC handler (B8)

Read-only handler delegating to EventStore.ReplayTail. Caps count at
500 server-side. Converts core.Event to plugin proto Event format.
Invariant I-13: no cursor mutation.
```

---

## Task 8: Lua Hostfunc Bindings — Focus Operations

**Files:**

- Create: `internal/plugin/hostfunc/stdlib_focus.go`
- Create: `internal/plugin/hostfunc/stdlib_focus_test.go`
- Modify: `internal/plugin/hostfunc/functions.go`

Follows the exact same pattern as `stdlib_streams.go` / `stdlib_streams_test.go`.

The Lua layer uses narrow interfaces (`FocusOps`, `HistoryReader`) rather than
importing `focus.Coordinator` directly because the hostfunc package is
intentionally dependency-light — it only needs 3 methods from the coordinator
and 1 from the event store. This avoids pulling in session store, settings,
cursor locker, and kind policy transitive deps into the Lua host's compilation
unit.

- [ ] **Step 1: Write failing tests for join\_focus**

Create `internal/plugin/hostfunc/stdlib_focus_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/session"
)

type mockFocusOps struct {
	joinCalls    []focusOpCall
	leaveCalls   []focusOpCall
	presentCalls []focusOpCall
	joinErr      error
	leaveErr     error
	presentErr   error
}

type focusOpCall struct {
	sessionID string
	key       session.FocusKey
}

func (m *mockFocusOps) JoinFocus(_ context.Context, sid string, key session.FocusKey) error {
	m.joinCalls = append(m.joinCalls, focusOpCall{sid, key})
	return m.joinErr
}

func (m *mockFocusOps) LeaveFocus(_ context.Context, sid string, key session.FocusKey) error {
	m.leaveCalls = append(m.leaveCalls, focusOpCall{sid, key})
	return m.leaveErr
}

func (m *mockFocusOps) PresentFocus(_ context.Context, sid string, key session.FocusKey) error {
	m.presentCalls = append(m.presentCalls, focusOpCall{sid, key})
	return m.presentErr
}

type mockHistoryReader struct {
	calls  []historyCall
	result []core.Event
	err    error
}

type historyCall struct {
	stream    string
	count     int
	notBefore time.Time
}

func (m *mockHistoryReader) ReplayTail(_ context.Context, stream string, count int, notBefore time.Time) ([]core.Event, error) {
	m.calls = append(m.calls, historyCall{stream, count, notBefore})
	return m.result, m.err
}

func newFocusTestState(t *testing.T, fo hostfunc.FocusOps, hr hostfunc.HistoryReader) *lua.LState {
	t.Helper()
	L := lua.NewState()
	t.Cleanup(L.Close)
	hf := hostfunc.New(nil,
		hostfunc.WithFocusOps(fo),
		hostfunc.WithHistoryReader(hr),
	)
	hf.Register(L, "test-plugin")
	return L
}

func TestJoinFocusCallsCoordinatorWithCorrectArgs(t *testing.T) {
	fo := &mockFocusOps{}
	L := newFocusTestState(t, fo, nil)

	targetID := ulid.Make()
	err := L.DoString(`holomush.join_focus("sess-1", "scene", "` + targetID.String() + `")`)
	require.NoError(t, err)

	require.Len(t, fo.joinCalls, 1)
	assert.Equal(t, "sess-1", fo.joinCalls[0].sessionID)
	assert.Equal(t, session.FocusKindScene, fo.joinCalls[0].key.Kind)
	assert.Equal(t, targetID, fo.joinCalls[0].key.TargetID)
}

func TestJoinFocusReturnsTrueOnSuccess(t *testing.T) {
	fo := &mockFocusOps{}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local ok = holomush.join_focus("sess-1", "scene", "` + ulid.Make().String() + `")
assert(ok == true, "expected true, got: " .. tostring(ok))
`)
	require.NoError(t, err)
}

func TestJoinFocusReturnsNilAndErrorOnFailure(t *testing.T) {
	fo := &mockFocusOps{joinErr: errors.New("already member")}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local ok, errmsg = holomush.join_focus("sess-1", "scene", "` + ulid.Make().String() + `")
assert(ok == nil, "expected nil on error")
assert(errmsg == "already member", "expected error message, got: " .. tostring(errmsg))
`)
	require.NoError(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestJoinFocus ./internal/plugin/hostfunc/`

Expected: FAIL — `FocusOps`, `WithFocusOps`, and `join_focus` don't exist.

- [ ] **Step 3: Create stdlib\_focus.go**

Create `internal/plugin/hostfunc/stdlib_focus.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
)

const focusOpsKey = "__holo_focus_ops"
const historyReaderKey = "__holo_history_reader"

// FocusOps is a narrow interface for focus coordinator operations exposed to Lua plugins.
type FocusOps interface {
	JoinFocus(ctx context.Context, sessionID string, target session.FocusKey) error
	LeaveFocus(ctx context.Context, sessionID string, target session.FocusKey) error
	PresentFocus(ctx context.Context, sessionID string, target session.FocusKey) error
}

// HistoryReader provides read-only event history access for Lua plugins.
type HistoryReader interface {
	ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time) ([]core.Event, error)
}

// RegisterFocusFuncs adds holomush.join_focus, leave_focus, present_focus,
// and query_stream_history to an existing holomush module table.
func RegisterFocusFuncs(ls *lua.LState, mod *lua.LTable, fo FocusOps, hr HistoryReader) {
	if fo != nil {
		ud := ls.NewUserData()
		ud.Value = fo
		ls.SetGlobal(focusOpsKey, ud)
	}
	if hr != nil {
		ud := ls.NewUserData()
		ud.Value = hr
		ls.SetGlobal(historyReaderKey, ud)
	}

	ls.SetField(mod, "join_focus", ls.NewFunction(joinFocusFn))
	ls.SetField(mod, "leave_focus", ls.NewFunction(leaveFocusFn))
	ls.SetField(mod, "present_focus", ls.NewFunction(presentFocusFn))
	ls.SetField(mod, "query_stream_history", ls.NewFunction(queryStreamHistoryFn))
}

func getFocusOps(ls *lua.LState) FocusOps {
	ud := ls.GetGlobal(focusOpsKey)
	if ud.Type() == lua.LTUserData {
		if userData, ok := ud.(*lua.LUserData); ok {
			if fo, ok := userData.Value.(FocusOps); ok {
				return fo
			}
		}
	}
	return nil
}

func getHistoryReader(ls *lua.LState) HistoryReader {
	ud := ls.GetGlobal(historyReaderKey)
	if ud.Type() == lua.LTUserData {
		if userData, ok := ud.(*lua.LUserData); ok {
			if hr, ok := userData.Value.(HistoryReader); ok {
				return hr
			}
		}
	}
	return nil
}

func parseFocusKey(kindStr, targetIDStr string) (session.FocusKey, error) {
	targetID, err := ulid.Parse(targetIDStr)
	if err != nil {
		return session.FocusKey{}, err
	}
	return session.FocusKey{
		Kind:     session.FocusKind(kindStr),
		TargetID: targetID,
	}, nil
}

// joinFocusFn implements holomush.join_focus(session_id, kind, target_id).
// Returns true on success; returns (nil, error_message) on failure.
func joinFocusFn(ls *lua.LState) int {
	sessionID := ls.CheckString(1)
	kind := ls.CheckString(2)
	targetID := ls.CheckString(3)

	fo := getFocusOps(ls)
	if fo == nil {
		slog.Warn("holomush.join_focus: focus ops not initialized")
		return 0
	}

	key, err := parseFocusKey(kind, targetID)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}

	ctx := ls.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	if err := fo.JoinFocus(ctx, sessionID, key); err != nil {
		slog.WarnContext(ctx, "holomush.join_focus failed",
			"session_id", sessionID, "kind", kind, "target_id", targetID, "error", err)
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}
	ls.Push(lua.LTrue)
	return 1
}

// leaveFocusFn implements holomush.leave_focus(session_id, kind, target_id).
func leaveFocusFn(ls *lua.LState) int {
	sessionID := ls.CheckString(1)
	kind := ls.CheckString(2)
	targetID := ls.CheckString(3)

	fo := getFocusOps(ls)
	if fo == nil {
		slog.Warn("holomush.leave_focus: focus ops not initialized")
		return 0
	}

	key, err := parseFocusKey(kind, targetID)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}

	ctx := ls.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	if err := fo.LeaveFocus(ctx, sessionID, key); err != nil {
		slog.WarnContext(ctx, "holomush.leave_focus failed",
			"session_id", sessionID, "kind", kind, "target_id", targetID, "error", err)
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}
	ls.Push(lua.LTrue)
	return 1
}

// presentFocusFn implements holomush.present_focus(session_id, kind, target_id).
func presentFocusFn(ls *lua.LState) int {
	sessionID := ls.CheckString(1)
	kind := ls.CheckString(2)
	targetID := ls.CheckString(3)

	fo := getFocusOps(ls)
	if fo == nil {
		slog.Warn("holomush.present_focus: focus ops not initialized")
		return 0
	}

	key, err := parseFocusKey(kind, targetID)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}

	ctx := ls.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	if err := fo.PresentFocus(ctx, sessionID, key); err != nil {
		slog.WarnContext(ctx, "holomush.present_focus failed",
			"session_id", sessionID, "kind", kind, "target_id", targetID, "error", err)
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}
	ls.Push(lua.LTrue)
	return 1
}

// queryStreamHistoryFn implements holomush.query_stream_history(stream, count, [not_before_ms]).
// Returns a table of event tables on success; returns (nil, error_message) on failure.
func queryStreamHistoryFn(ls *lua.LState) int {
	stream := ls.CheckString(1)
	count := ls.CheckInt(2)
	notBeforeMs := ls.OptInt64(3, 0)

	hr := getHistoryReader(ls)
	if hr == nil {
		slog.Warn("holomush.query_stream_history: history reader not initialized")
		return 0
	}

	var notBefore time.Time
	if notBeforeMs > 0 {
		notBefore = time.UnixMilli(notBeforeMs).UTC()
	}

	ctx := ls.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	events, err := hr.ReplayTail(ctx, stream, count, notBefore)
	if err != nil {
		slog.WarnContext(ctx, "holomush.query_stream_history failed",
			"stream", stream, "error", err)
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}

	result := ls.NewTable()
	for i, e := range events {
		et := ls.NewTable()
		ls.SetField(et, "id", lua.LString(e.ID.String()))
		ls.SetField(et, "stream", lua.LString(e.Stream))
		ls.SetField(et, "type", lua.LString(string(e.Type)))
		ls.SetField(et, "timestamp", lua.LNumber(e.Timestamp.UnixMilli()))
		ls.SetField(et, "actor_kind", lua.LString(e.Actor.Kind.String()))
		ls.SetField(et, "actor_id", lua.LString(e.Actor.ID))
		ls.SetField(et, "payload", lua.LString(e.Payload))
		result.RawSetInt(i+1, et)
	}
	ls.Push(result)
	return 1
}
```

- [ ] **Step 4: Add WithFocusOps and WithHistoryReader options to functions.go**

In `internal/plugin/hostfunc/functions.go`, add fields to `Functions`:

```go
type Functions struct {
	// ... existing fields ...
	focusOps      FocusOps
	historyReader HistoryReader
}
```

Add option functions:

```go
// WithFocusOps sets the focus coordinator for join/leave/present focus host functions.
func WithFocusOps(fo FocusOps) Option {
	return func(f *Functions) { f.focusOps = fo }
}

// WithHistoryReader sets the event store reader for query_stream_history host function.
func WithHistoryReader(hr HistoryReader) Option {
	return func(f *Functions) { f.historyReader = hr }
}
```

In the `Register` method, add after the `RegisterStreamFuncs` call:

```go
	// Register focus management functions.
	RegisterFocusFuncs(ls, mod, f.focusOps, f.historyReader)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `task test -- -run "TestJoinFocus" ./internal/plugin/hostfunc/`

Expected: PASS

- [ ] **Step 6: Commit**

```text
feat(hostfunc): add focus Lua bindings + FocusOps/HistoryReader interfaces (B8)

Lua plugins can now call holomush.join_focus, leave_focus, present_focus,
and query_stream_history. Follows the same pattern as stdlib_streams.go.
Uses narrow FocusOps/HistoryReader interfaces to keep the hostfunc
package dependency-light.
```

---

## Task 9: Lua Hostfunc Tests — Leave, Present, QueryStreamHistory

**Files:**

- Modify: `internal/plugin/hostfunc/stdlib_focus_test.go`

- [ ] **Step 1: Write remaining tests**

Append to `internal/plugin/hostfunc/stdlib_focus_test.go`:

```go
func TestLeaveFocusCallsCoordinatorWithCorrectArgs(t *testing.T) {
	fo := &mockFocusOps{}
	L := newFocusTestState(t, fo, nil)

	targetID := ulid.Make()
	err := L.DoString(`holomush.leave_focus("sess-1", "scene", "` + targetID.String() + `")`)
	require.NoError(t, err)

	require.Len(t, fo.leaveCalls, 1)
	assert.Equal(t, "sess-1", fo.leaveCalls[0].sessionID)
	assert.Equal(t, session.FocusKindScene, fo.leaveCalls[0].key.Kind)
	assert.Equal(t, targetID, fo.leaveCalls[0].key.TargetID)
}

func TestLeaveFocusReturnsTrueOnSuccess(t *testing.T) {
	fo := &mockFocusOps{}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local ok = holomush.leave_focus("sess-1", "scene", "` + ulid.Make().String() + `")
assert(ok == true, "expected true, got: " .. tostring(ok))
`)
	require.NoError(t, err)
}

func TestLeaveFocusReturnsNilAndErrorOnFailure(t *testing.T) {
	fo := &mockFocusOps{leaveErr: errors.New("session expired")}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local ok, errmsg = holomush.leave_focus("sess-1", "scene", "` + ulid.Make().String() + `")
assert(ok == nil, "expected nil on error")
assert(errmsg == "session expired", "expected error message, got: " .. tostring(errmsg))
`)
	require.NoError(t, err)
}

func TestPresentFocusCallsCoordinatorWithCorrectArgs(t *testing.T) {
	fo := &mockFocusOps{}
	L := newFocusTestState(t, fo, nil)

	targetID := ulid.Make()
	err := L.DoString(`holomush.present_focus("sess-1", "scene", "` + targetID.String() + `")`)
	require.NoError(t, err)

	require.Len(t, fo.presentCalls, 1)
	assert.Equal(t, "sess-1", fo.presentCalls[0].sessionID)
	assert.Equal(t, session.FocusKindScene, fo.presentCalls[0].key.Kind)
	assert.Equal(t, targetID, fo.presentCalls[0].key.TargetID)
}

func TestPresentFocusReturnsTrueOnSuccess(t *testing.T) {
	fo := &mockFocusOps{}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local ok = holomush.present_focus("sess-1", "scene", "` + ulid.Make().String() + `")
assert(ok == true, "expected true, got: " .. tostring(ok))
`)
	require.NoError(t, err)
}

func TestPresentFocusReturnsNilAndErrorOnFailure(t *testing.T) {
	fo := &mockFocusOps{presentErr: errors.New("not member")}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local ok, errmsg = holomush.present_focus("sess-1", "scene", "` + ulid.Make().String() + `")
assert(ok == nil, "expected nil on error")
assert(errmsg == "not member", "expected error message, got: " .. tostring(errmsg))
`)
	require.NoError(t, err)
}

func TestQueryStreamHistoryReturnsEventsAsTable(t *testing.T) {
	eventID := ulid.Make()
	hr := &mockHistoryReader{
		result: []core.Event{
			{
				ID:        eventID,
				Stream:    "scene:abc:ic",
				Type:      "say",
				Payload:   `{"text":"hello"}`,
				Timestamp: time.Unix(1700000000, 0),
				Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char-1"},
			},
		},
	}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`
local events = holomush.query_stream_history("scene:abc:ic", 10)
assert(#events == 1, "expected 1 event, got: " .. #events)
assert(events[1].stream == "scene:abc:ic", "wrong stream: " .. events[1].stream)
assert(events[1].type == "say", "wrong type: " .. events[1].type)
assert(events[1].payload == '{"text":"hello"}', "wrong payload: " .. events[1].payload)
`)
	require.NoError(t, err)

	require.Len(t, hr.calls, 1)
	assert.Equal(t, "scene:abc:ic", hr.calls[0].stream)
	assert.Equal(t, 10, hr.calls[0].count)
}

func TestQueryStreamHistoryReturnsNilAndErrorOnFailure(t *testing.T) {
	hr := &mockHistoryReader{err: errors.New("store error")}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`
local events, errmsg = holomush.query_stream_history("scene:abc:ic", 10)
assert(events == nil, "expected nil on error")
assert(errmsg == "store error", "expected error message, got: " .. tostring(errmsg))
`)
	require.NoError(t, err)
}

func TestQueryStreamHistoryWithNilReaderIsNoOp(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	hf := hostfunc.New(nil)
	hf.Register(L, "test-plugin")

	err := L.DoString(`holomush.query_stream_history("scene:abc:ic", 10)`)
	require.NoError(t, err)
}

func TestFocusFuncsWithNilOpsAreNoOps(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	hf := hostfunc.New(nil)
	hf.Register(L, "test-plugin")

	require.NoError(t, L.DoString(`holomush.join_focus("s", "scene", "`+ulid.Make().String()+`")`))
	require.NoError(t, L.DoString(`holomush.leave_focus("s", "scene", "`+ulid.Make().String()+`")`))
	require.NoError(t, L.DoString(`holomush.present_focus("s", "scene", "`+ulid.Make().String()+`")`))
}
```

- [ ] **Step 2: Run all focus hostfunc tests**

Run: `task test -- -run "TestJoinFocus|TestLeaveFocus|TestPresentFocus|TestQueryStreamHistory|TestFocusFuncs" ./internal/plugin/hostfunc/`

Expected: PASS (all tests)

- [ ] **Step 3: Commit**

```text
test(hostfunc): complete Lua focus hostfunc test coverage (B8)

Tests leave_focus, present_focus, query_stream_history success/error
paths, nil-safety for uninitialized ops, and event table structure.
```

---

## Task 10: Wire Dependencies in sub\_grpc.go

**Files:**

- Modify: `cmd/holomush/sub_grpc.go`

Inject FocusCoordinator and EventStore into both the binary plugin host
and the Lua hostfunc.Functions so the new handlers/bindings have their deps.

- [ ] **Step 1: Inject into binary plugin host**

Find where `goplugin.NewHost` (or `goplugin.NewHostWithFactory`) is called in
`cmd/holomush/sub_grpc.go`. Add the new options:

```go
goplugin.WithFocusCoordinator(focusCoord),
goplugin.WithEventStore(eventStore),
```

This must be placed after `focusCoord` is constructed (around line 253) and
where the binary host options are assembled.

- [ ] **Step 2: Inject into Lua hostfunc.Functions**

Find where `hostfunc.New` is called. Add:

```go
hostfunc.WithFocusOps(focusCoord),
hostfunc.WithHistoryReader(eventStore),
```

The `focus.Coordinator` interface satisfies `hostfunc.FocusOps` structurally
(same 3 methods). The `core.EventStore` interface has `ReplayTail` matching
`hostfunc.HistoryReader`. No adapter needed.

- [ ] **Step 3: Verify compilation**

Run: `task build`

Expected: BUILD SUCCESS

- [ ] **Step 4: Run full test suite**

Run: `task test`

Expected: All tests pass. No regressions.

- [ ] **Step 5: Commit**

```text
feat(wire): inject focus coordinator + event store into plugin hosts (B8)

Binary plugins can now call JoinFocus/LeaveFocus/PresentFocus/
QueryStreamHistory via gRPC. Lua plugins can call the same operations
via holomush.* hostfuncs. Both delegate to the same FocusCoordinator.
```

---

## Task 11: Lint + Format + Full Verification

**Files:** None (verification only)

- [ ] **Step 1: Run formatter**

Run: `task fmt`

- [ ] **Step 2: Run linter**

Run: `task lint`

Expected: No new warnings. Fix any issues.

- [ ] **Step 3: Run full test suite**

Run: `task test`

Expected: All tests pass.

- [ ] **Step 4: Run pr-prep**

Run: `task pr-prep`

Expected: All CI-equivalent checks pass.

- [ ] **Step 5: Final commit if any formatting/lint fixes**

```text
chore: lint + format fixes (B8)
```

---

## Spec Coverage Cross-Check

| Spec Section                                          | Requirement                                                   | Task   |
| ----------------------------------------------------- | ------------------------------------------------------------- | ------ |
| §3.4 proto: FocusKind enum                            | Add to plugin.proto                                           | Task 1 |
| §3.4 proto: FocusKey message                          | Add to plugin.proto                                           | Task 1 |
| §3.4 proto: StreamReplayMode enum                     | Add to plugin.proto                                           | Task 1 |
| §3.4 proto: replay\_mode on AddSessionStreamRequest   | Extend existing message                                       | Task 1 |
| §3.4 proto: JoinFocus RPC                             | Add to PluginHostService                                      | Task 1 |
| §3.4 proto: LeaveFocus RPC                            | Add to PluginHostService                                      | Task 1 |
| §3.4 proto: PresentFocus RPC                          | Add to PluginHostService                                      | Task 1 |
| §3.4 proto: QueryStreamHistory RPC                    | Add to PluginHostService                                      | Task 1 |
| ReplayMode architectural placement                    | Move to internal/session (cursor-initialization semantics)    | Task 2 |
| §4.6 ambient add with LIVE\_ONLY                      | StreamRegistry.AddStreamWithMode                              | Task 3 |
| §4.3 focus join flow                                  | Go handler delegates to Coordinator.JoinFocus                 | Task 5 |
| §4.4 focus leave flow                                 | Go handler delegates to Coordinator.LeaveFocus                | Task 6 |
| §4.5 present focus flow                               | Go handler delegates to Coordinator.PresentFocus              | Task 6 |
| I-7 Plugin Declaration-Only API                       | Proto shapes omit cursor/stream/replay for focus ops          | Task 1 |
| I-13 Read-Only Plugin History Access                  | QueryStreamHistory handler is pure read                       | Task 7 |
| Lua hostfunc: join\_focus                             | holomush.join\_focus(session\_id, kind, target\_id)           | Task 8 |
| Lua hostfunc: leave\_focus                            | holomush.leave\_focus(session\_id, kind, target\_id)          | Task 9 |
| Lua hostfunc: present\_focus                          | holomush.present\_focus(session\_id, kind, target\_id)        | Task 9 |
| Lua hostfunc: query\_stream\_history                  | holomush.query\_stream\_history(stream, count, not\_before)   | Task 9 |
| Wiring: binary plugin host                            | Inject FocusCoordinator + EventStore                          | Task 10 |
| Wiring: Lua host functions                            | Inject FocusOps + HistoryReader                               | Task 10 |
