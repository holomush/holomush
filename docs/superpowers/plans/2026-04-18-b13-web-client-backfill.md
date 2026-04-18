# B13 — Web Client Reload Backfill Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After page reload, the web terminal's scrollback is repopulated with prior events (dimmed), then transitions to live events — reusing existing dimming/separator infrastructure and layering a web-portable helper that future panels can reuse.

**Architecture:** New `CoreService.ListSessionStreams` RPC (wrapping `focusCoordinator.RestoreFocus`), proxied by `WebService.WebListSessionStreams`. A new web-portable `backfillStreams` helper fans out `WebQueryStreamHistory` per stream, merges by `(timestamp, event_id)`, and returns events. The terminal page runs Subscribe + backfill in parallel, buffering Subscribe events until backfill completes, then draining them with an `event_id`-based dedup set to close the detach+reattach+reload overlap path.

**Tech Stack:** Go 1.23, ConnectRPC / gRPC, SvelteKit 5 + Svelte 5 runes, protobuf-es, vitest, Playwright, testcontainers-postgres.

**Spec:** `docs/superpowers/specs/2026-04-17-b13-web-client-backfill-design.md`

**Bead:** `holomush-oy6e.13`

---

## File Structure

### Server (Go)

| File | Purpose |
|---|---|
| `api/proto/holomush/core/v1/core.proto` | **Modify** — add `ListSessionStreams` RPC, request/response messages |
| `api/proto/holomush/web/v1/web.proto` | **Modify** — add `event_id` to `GameEvent`, add `WebListSessionStreams` RPC |
| `pkg/proto/holomush/core/v1/*.pb.go` | **Regen** (from `task proto`) |
| `pkg/proto/holomush/web/v1/*.pb.go` | **Regen** (from `task proto`) |
| `internal/grpc/list_session_streams.go` | **Create** — handler for `CoreService.ListSessionStreams` |
| `internal/grpc/list_session_streams_test.go` | **Create** — unit tests for handler |
| `internal/web/handler.go` | **Modify** — add `ListSessionStreams` to `CoreClient` interface; add `WebListSessionStreams` handler method |
| `internal/web/handler_test.go` | **Modify** — add `WebListSessionStreams` test |
| `internal/web/translate.go` | **Modify** — populate `event_id` in `translateEvent` (state + non-state paths) |
| `internal/web/translate_test.go` | **Modify** — assert `event_id` propagation |
| `internal/web/mocks/CoreClient_mock.go` | **Regen** (from `task mocks:generate`) |
| `test/integration/list_session_streams/list_session_streams_integration_test.go` | **Create** — integration test |

### Client (TypeScript)

| File | Purpose |
|---|---|
| `web/src/lib/connect/holomush/**` | **Regen** (from `task web:generate`) |
| `web/src/lib/connect/errors.ts` | **Create** — `isUnimplementedError` helper |
| `web/src/lib/backfill/streamBackfill.ts` | **Create** — backfill helper |
| `web/src/lib/backfill/streamBackfill.test.ts` | **Create** — vitest unit tests |
| `web/src/routes/(authed)/terminal/+page.svelte` | **Modify** — replace `startStreaming` with `hydrateAndStream`, remove dead `replayFromCursor`, add dedup |
| `web/e2e/terminal.spec.ts` | **Modify** — un-skip reload test, add separator assertion, add new detach+accumulated test |

---

## Task 0: Claim the bead

- [ ] **Step 0.1: Verify the bead is claimed**

Run (from the main repo dir):

```bash
bd show holomush-oy6e.13
```

Expected: `Status: in_progress` (already set at session start). If not, run `bd update holomush-oy6e.13 --status in_progress`.

---

## Task 1: Add `event_id` to `webv1.GameEvent` and propagate in `translateEvent`

**Files:**

- Modify: `api/proto/holomush/web/v1/web.proto`
- Modify: `internal/web/translate.go`
- Modify: `internal/web/translate_test.go`
- Regen: `pkg/proto/holomush/web/v1/web.pb.go`, `web/src/lib/connect/holomush/web/v1/web_pb.ts`

- [ ] **Step 1.1: Edit `web.proto` — add `event_id` field to `GameEvent`**

Locate `message GameEvent` (around line 93 of the proto). Add field 9:

```protobuf
message GameEvent {
  string type = 1;
  string category = 2;
  string format = 3;
  EventChannel display_target = 4;
  int64 timestamp = 5;
  string actor = 6;
  string text = 7;
  google.protobuf.Struct metadata = 8;
  string event_id = 9; // ULID; populated from corev1.Event.id
}
```

- [ ] **Step 1.2: Regenerate Go proto**

Run:

```bash
GOWORK=off task proto
```

Expected: `pkg/proto/holomush/web/v1/web.pb.go` diff shows new `EventId` field on `GameEvent` struct.

- [ ] **Step 1.3: Regenerate TS proto**

Run:

```bash
GOWORK=off task web:generate
```

Expected: `web/src/lib/connect/holomush/web/v1/web_pb.ts` now includes `eventId: string` on the `GameEvent` type.

- [ ] **Step 1.4: Write failing test — `event_id` propagates through `translateEvent`**

Append to `internal/web/translate_test.go`:

```go
func TestTranslateEvent_PopulatesEventIdForCommunicationEvents(t *testing.T) {
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Id:        "01HYXYZ000000000000000001A",
		Type:      "say",
		Timestamp: timestamppb.New(timestamppb.Now().AsTime()),
		Payload:   mustMarshal(t, map[string]string{"character_name": "Alice", "message": "Hello!"}),
	}

	got := h.translateEvent(ev)
	require.NotNil(t, got)
	assert.Equal(t, "01HYXYZ000000000000000001A", got.GetEventId())
}

func TestTranslateEvent_PopulatesEventIdForStateEvents(t *testing.T) {
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Id:        "01HYXYZ000000000000000002B",
		Type:      "location_state",
		Timestamp: timestamppb.New(timestamppb.Now().AsTime()),
		Payload:   mustMarshal(t, map[string]any{"name": "Cafe", "description": "a place"}),
	}

	got := h.translateEvent(ev)
	require.NotNil(t, got)
	assert.Equal(t, "01HYXYZ000000000000000002B", got.GetEventId())
}
```

- [ ] **Step 1.5: Run tests to verify they fail**

Run:

```bash
GOWORK=off task test -- -run 'TestTranslateEvent_PopulatesEventId' ./internal/web/
```

Expected: FAIL — `got.GetEventId()` returns `""` (empty).

- [ ] **Step 1.6: Update `translate.go` to populate `event_id`**

In `internal/web/translate.go`, the non-state path returns a `&webv1.GameEvent{...}` at lines 132-141. Add `EventId: ev.GetId(),`:

```go
return &webv1.GameEvent{
    Type:          eventType,
    Category:      category,
    Format:        format,
    DisplayTarget: displayTarget,
    Timestamp:     ts,
    Actor:         actor,
    Text:          text,
    Metadata:      metadata,
    EventId:       ev.GetId(),
}
```

In the same file, `translateStateEvent` (lines 165-191) also returns a `&webv1.GameEvent{...}`. Add the same field:

```go
return &webv1.GameEvent{
    Type:          eventType,
    Category:      category,
    Format:        format,
    DisplayTarget: displayTarget,
    Timestamp:     ts,
    Metadata:      s,
    EventId:       ev.GetId(),
}
```

- [ ] **Step 1.7: Run tests to verify they pass**

Run:

```bash
GOWORK=off task test -- -run 'TestTranslateEvent_PopulatesEventId' ./internal/web/
```

Expected: PASS (2/2).

- [ ] **Step 1.8: Run the full translate test suite for regression**

Run:

```bash
GOWORK=off task test -- ./internal/web/
```

Expected: all pass.

- [ ] **Step 1.9: Commit**

From the worktree `/Users/sean/Code/github.com/holomush/.worktrees/b13-web-backfill/`:

```bash
jj --no-pager commit -m "feat(proto): add event_id to webv1.GameEvent for backfill dedup

Populated by translateEvent from corev1.EventFrame.id. Shared between
Subscribe and QueryStreamHistory paths so both sources carry the same
stable identifier for client-side dedup."
```

Expected: `@` advances to a new empty change; the commit appears in `jj log`.

---

## Task 2: Add `ListSessionStreams` RPC to `core.proto`

**Files:**

- Modify: `api/proto/holomush/core/v1/core.proto`
- Regen: `pkg/proto/holomush/core/v1/core.pb.go`, `pkg/proto/holomush/core/v1/core_grpc.pb.go`
- Modify: `internal/web/handler.go` — add to `CoreClient` interface

- [ ] **Step 2.1: Edit `core.proto` — add RPC, request, response**

Locate `rpc QueryStreamHistory(...)` in the service block (around line 87). Add after it:

```protobuf
  // ListSessionStreams returns the set of streams the session is currently
  // subscribed to, derived from focusCoordinator.RestoreFocus. Used by
  // web clients to enumerate streams for backfill on reload. Pure read.
  rpc ListSessionStreams(ListSessionStreamsRequest) returns (ListSessionStreamsResponse);
```

At the end of the file (where other request/response messages live — use `grep -n '^message' api/proto/holomush/core/v1/core.proto` to find a good spot near QueryStreamHistory's messages), add:

```protobuf
message ListSessionStreamsRequest {
  string session_id = 1;
  RequestMeta meta = 2;
}

message ListSessionStreamsResponse {
  repeated string streams = 1;
}
```

- [ ] **Step 2.2: Regenerate Go**

Run:

```bash
GOWORK=off task proto
```

Expected: `core_grpc.pb.go` now exposes `ListSessionStreams` on `CoreServiceClient` and `CoreServiceServer`.

- [ ] **Step 2.3: Add `ListSessionStreams` to `CoreClient` interface**

In `internal/web/handler.go`, the `CoreClient` interface (lines 33-56) lists all methods the web gateway calls. Add:

```go
	ListSessionStreams(ctx context.Context, req *corev1.ListSessionStreamsRequest) (*corev1.ListSessionStreamsResponse, error)
```

Place it adjacent to `QueryStreamHistory` for visual grouping.

- [ ] **Step 2.4: Regenerate CoreClient mock**

Run:

```bash
GOWORK=off task mocks:generate
```

Expected: `internal/web/mocks/CoreClient_mock.go` diff shows new `ListSessionStreams` method on the mock.

- [ ] **Step 2.5: Verify build**

Run:

```bash
GOWORK=off task lint
```

Expected: no errors. (If there are "CoreClient does not implement ..." errors at wiring sites, leave them — Task 3 fills them.)

- [ ] **Step 2.6: Commit**

```bash
jj --no-pager commit -m "feat(proto): add CoreService.ListSessionStreams RPC

Thin enumeration of streams the session is subscribed to. Will be wired
to focusCoordinator.RestoreFocus in the next commit."
```

---

## Task 3: Implement `CoreService.ListSessionStreams` handler (TDD)

**Files:**

- Create: `internal/grpc/list_session_streams.go`
- Create: `internal/grpc/list_session_streams_test.go`
- Modify: `internal/grpc/server.go` — register the handler (if the server struct needs explicit wiring; often inherited from embedded generated server)

- [ ] **Step 3.1: Write failing tests**

Create `internal/grpc/list_session_streams_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

func TestListSessionStreams_RequiresSessionID(t *testing.T) {
	s := &CoreServer{}
	_, err := s.ListSessionStreams(context.Background(), &corev1.ListSessionStreamsRequest{})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "INVALID_ARGUMENT", o.Code())
}

func TestListSessionStreams_ReturnsSessionNotFoundOnMiss(t *testing.T) {
	s := &CoreServer{
		sessionStore: &fakeSessionStore{getErr: oops.Code("SESSION_NOT_FOUND").Errorf("nope")},
	}
	_, err := s.ListSessionStreams(context.Background(), &corev1.ListSessionStreamsRequest{
		SessionId: "missing",
	})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SESSION_NOT_FOUND", o.Code())
}

func TestListSessionStreams_ReturnsSessionExpiredForExpiredSession(t *testing.T) {
	expired := &session.Info{
		ID:        "sess-expired",
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	s := &CoreServer{
		sessionStore: &fakeSessionStore{getInfo: expired},
	}
	_, err := s.ListSessionStreams(context.Background(), &corev1.ListSessionStreamsRequest{
		SessionId: "sess-expired",
	})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SESSION_EXPIRED", o.Code())
}

func TestListSessionStreams_ReturnsRestoreFocusStreams(t *testing.T) {
	charID := ulid.MustParse("01HYXYZCHAR00000000000000CH")
	locID := ulid.MustParse("01HYXYZLOC0000000000000000L0")
	info := &session.Info{
		ID:          "sess-1",
		CharacterID: charID,
		LocationID:  locID,
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	fakeCoord := &fakeFocusCoordinator{
		plan: focus.RestorePlan{
			Streams: []focus.StreamWithMode{
				{Stream: world.CharacterStream(charID), Mode: focus.ReplayModeFromCursor},
				{Stream: world.LocationStream(locID), Mode: focus.ReplayModeFromCursor},
				{Stream: "scene:01HYXSCENE00000000000000SC:ic", Mode: focus.ReplayModeFromCursor},
			},
		},
	}
	s := &CoreServer{
		sessionStore:     &fakeSessionStore{getInfo: info},
		focusCoordinator: fakeCoord,
	}

	resp, err := s.ListSessionStreams(context.Background(), &corev1.ListSessionStreamsRequest{
		SessionId: "sess-1",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{
		world.CharacterStream(charID),
		world.LocationStream(locID),
		"scene:01HYXSCENE00000000000000SC:ic",
	}, resp.GetStreams())
}

func TestListSessionStreams_FallsBackWhenCoordinatorNil(t *testing.T) {
	charID := ulid.MustParse("01HYXYZCHAR00000000000000CH")
	locID := ulid.MustParse("01HYXYZLOC0000000000000000L0")
	info := &session.Info{
		ID:          "sess-2",
		CharacterID: charID,
		LocationID:  locID,
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	s := &CoreServer{
		sessionStore:     &fakeSessionStore{getInfo: info},
		focusCoordinator: nil, // explicitly nil
	}

	resp, err := s.ListSessionStreams(context.Background(), &corev1.ListSessionStreamsRequest{
		SessionId: "sess-2",
	})
	require.NoError(t, err)
	assert.Contains(t, resp.GetStreams(), world.CharacterStream(charID))
	assert.Contains(t, resp.GetStreams(), world.LocationStream(locID))
}
```

Check the existing `server_test.go` for `fakeSessionStore` and `fakeFocusCoordinator` — they may already exist. If not, add minimal stubs at the bottom of the new test file:

```go
// Minimal fakes — if server_test.go already has these, delete these and import the existing ones.
type fakeSessionStore struct {
	getInfo *session.Info
	getErr  error
}

func (f *fakeSessionStore) Get(_ context.Context, _ string) (*session.Info, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.getInfo, nil
}

// Implement the other session.Store methods as panics (unused in these tests).
// Copy the full list from internal/session/store.go.

type fakeFocusCoordinator struct {
	plan focus.RestorePlan
	err  error
}

func (f *fakeFocusCoordinator) RestoreFocus(_ context.Context, _ string) (focus.RestorePlan, error) {
	return f.plan, f.err
}

// Implement other focus.Coordinator methods as panics (unused in these tests).
```

**Note:** If `server_test.go` already has these fakes, **delete** the stub definitions at the bottom of this new file and rely on the package-wide fakes. Run `grep -n 'type fakeSessionStore' internal/grpc/*.go` to check.

- [ ] **Step 3.2: Run tests — verify all fail**

Run:

```bash
GOWORK=off task test -- -run 'TestListSessionStreams' ./internal/grpc/
```

Expected: compile errors (`ListSessionStreams` method not defined on `CoreServer`).

- [ ] **Step 3.3: Implement the handler**

Create `internal/grpc/list_session_streams.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/plugins"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// ListSessionStreams returns the stream names the session is subscribed to,
// derived from focusCoordinator.RestoreFocus. Pure read — does not mutate
// session state. Auth follows the QueryStreamHistory pattern: session
// existence + expiry only; no player_session_token required.
func (s *CoreServer) ListSessionStreams(ctx context.Context, req *corev1.ListSessionStreamsRequest) (*corev1.ListSessionStreamsResponse, error) {
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}
	slog.DebugContext(ctx, "list session streams",
		"request_id", requestID,
		"session_id", req.SessionId,
	)

	if req.SessionId == "" {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("session_id is required")
	}

	info, err := s.sessionStore.Get(ctx, req.SessionId)
	if err != nil {
		if oopsErr, ok := oops.AsOops(err); ok && oopsErr.Code() == "SESSION_NOT_FOUND" {
			return nil, oops.Code("SESSION_NOT_FOUND").
				With("session_id", req.SessionId).
				Errorf("session not found")
		}
		return nil, oops.Code("INTERNAL").Wrap(err)
	}
	if info.IsExpired() {
		return nil, oops.Code("SESSION_EXPIRED").
			With("session_id", req.SessionId).
			Errorf("session expired")
	}

	var plan focus.RestorePlan
	if s.focusCoordinator != nil {
		var planErr error
		plan, planErr = s.focusCoordinator.RestoreFocus(ctx, req.SessionId)
		if planErr != nil {
			slog.WarnContext(ctx, "RestoreFocus failed, falling back to empty plan",
				"session_id", req.SessionId, "error", planErr)
		}
	}

	// Fallback: replicate Subscribe's focusCoordinator-nil ambient-stream
	// assembly (server.go:787-816) so this RPC never returns a different
	// stream set than Subscribe under any server configuration.
	if len(plan.Streams) == 0 {
		if !info.CharacterID.IsZero() {
			plan.Streams = append(plan.Streams,
				focus.StreamWithMode{Stream: world.CharacterStream(info.CharacterID), Mode: focus.ReplayModeFromCursor},
			)
		}
		if !info.LocationID.IsZero() {
			plan.Streams = append(plan.Streams,
				focus.StreamWithMode{Stream: world.LocationStream(info.LocationID), Mode: focus.ReplayModeFromCursor},
			)
		}
		if s.streamContributor != nil {
			playerID := ""
			if !info.PlayerID.IsZero() {
				playerID = info.PlayerID.String()
			}
			pluginStreams := s.streamContributor.QuerySessionStreams(ctx, plugins.SessionStreamsRequest{
				CharacterID: info.CharacterID.String(),
				PlayerID:    playerID,
				SessionID:   info.ID,
			})
			for _, ps := range pluginStreams {
				plan.Streams = append(plan.Streams,
					focus.StreamWithMode{Stream: ps, Mode: focus.ReplayModeFromCursor},
				)
			}
		}
	}

	out := make([]string, 0, len(plan.Streams))
	for _, sm := range plan.Streams {
		out = append(out, sm.Stream)
	}
	return &corev1.ListSessionStreamsResponse{Streams: out}, nil
}
```

**Note:** The exact fakes used in tests (`fakeSessionStore`, `fakeFocusCoordinator`) may already be defined in `internal/grpc/server_test.go`. If compilation fails due to missing or duplicate `fakeSessionStore`, investigate and reconcile (use the canonical version that already exists; if both exist, delete the new file's duplicate).

- [ ] **Step 3.4: Run tests — verify all pass**

Run:

```bash
GOWORK=off task test -- -run 'TestListSessionStreams' ./internal/grpc/
```

Expected: PASS (5/5).

- [ ] **Step 3.5: Run full grpc package tests for regression**

Run:

```bash
GOWORK=off task test -- ./internal/grpc/
```

Expected: all pass.

- [ ] **Step 3.6: Commit**

```bash
jj --no-pager commit -m "feat(grpc): ListSessionStreams handler

Wraps focusCoordinator.RestoreFocus, with fallback to manual ambient
stream assembly when the coordinator is nil (matches Subscribe's
behavior at server.go:787-816). Auth follows B9's QueryStreamHistory
pattern — session existence + expiry only."
```

---

## Task 4: Core integration test — `ListSessionStreams` over real gRPC

**Files:**

- Create: `test/integration/list_session_streams/list_session_streams_integration_test.go`

- [ ] **Step 4.1: Study the template**

Run:

```bash
GOWORK=off task test -- -count=1 -tags=integration -run TestQueryStreamHistory ./test/integration/stream_history/
```

Expected: PASS (seed data pattern works).

Read `test/integration/stream_history/` to understand testcontainer + seed-data conventions.

- [ ] **Step 4.2: Write integration test**

Create `test/integration/list_session_streams/list_session_streams_integration_test.go`:

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package listsessionstreams_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Import the integration harness used by test/integration/stream_history.
	// Use the exact package/path names from the canonical example; copy the
	// helper imports verbatim into this file.
	integrationhelpers "github.com/holomush/holomush/test/integration/helpers"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

func TestListSessionStreams_ReturnsCharacterAndLocationStreams(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	h := integrationhelpers.NewHarness(t)
	defer h.Close()

	// Create a session with a character and a location.
	sess := h.SeedSessionWithCharacterAndLocation(t, ctx)

	resp, err := h.CoreClient.ListSessionStreams(ctx, &corev1.ListSessionStreamsRequest{
		SessionId: sess.ID,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Contains(t, resp.GetStreams(), "character:"+sess.CharacterID.String())
	assert.Contains(t, resp.GetStreams(), "location:"+sess.LocationID.String())
}

func TestListSessionStreams_RejectsMissingSessionID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	h := integrationhelpers.NewHarness(t)
	defer h.Close()

	_, err := h.CoreClient.ListSessionStreams(ctx, &corev1.ListSessionStreamsRequest{})
	require.Error(t, err)
	// gRPC status error; string-match is acceptable here since we only
	// want to confirm the handler rejected it (precise code check is
	// done in the unit tests).
	assert.Contains(t, err.Error(), "session_id is required")
}
```

**If** `test/integration/helpers` does NOT have a `SeedSessionWithCharacterAndLocation` helper, use the seeding pattern from `test/integration/stream_history/`: a combination of `SeedCharacter`, `SeedLocation`, `CreateSession`. Adapt field/method names to what actually exists.

- [ ] **Step 4.3: Run the integration test**

Run:

```bash
GOWORK=off task test:int -- -run TestListSessionStreams ./test/integration/list_session_streams/
```

Expected: PASS. If the helper signature doesn't match, read `test/integration/helpers/*.go` and adjust the test to the real API.

- [ ] **Step 4.4: Commit**

```bash
jj --no-pager commit -m "test(integration): ListSessionStreams returns expected streams

Verifies the happy path (character + location) and that invalid inputs
are rejected. Scene + plugin-contributed streams are covered by the
existing focusCoordinator tests; this suite verifies the RPC wiring."
```

---

## Task 5: Add `WebListSessionStreams` RPC to `web.proto` and regenerate

**Files:**

- Modify: `api/proto/holomush/web/v1/web.proto`
- Regen: `pkg/proto/holomush/web/v1/*.pb.go`, `web/src/lib/connect/holomush/web/v1/*.ts`

- [ ] **Step 5.1: Edit `web.proto`**

Locate `rpc WebQueryStreamHistory(...)` in the `WebService` block (around line 66). Add after it:

```protobuf
  // WebListSessionStreams returns the stream names the session is subscribed
  // to, proxied from CoreService.ListSessionStreams. Used by the web client
  // to enumerate streams for reload-backfill.
  rpc WebListSessionStreams(WebListSessionStreamsRequest) returns (WebListSessionStreamsResponse);
```

Add request/response messages near the other `WebQueryStreamHistory` messages (around line 255):

```protobuf
message WebListSessionStreamsRequest {
  string session_id = 1;
}

message WebListSessionStreamsResponse {
  repeated string streams = 1;
}
```

- [ ] **Step 5.2: Regenerate Go + TS**

Run:

```bash
GOWORK=off task proto && GOWORK=off task web:generate
```

Expected: new types appear in both `pkg/proto/holomush/web/v1/` and `web/src/lib/connect/holomush/web/v1/`.

- [ ] **Step 5.3: Verify build**

Run:

```bash
GOWORK=off task lint
```

Expected: one compile error at `internal/web/handler.go` — `*Handler` no longer satisfies `webv1connect.WebServiceHandler` because `WebListSessionStreams` is unimplemented. This is expected; Task 6 fixes it.

- [ ] **Step 5.4: Commit**

```bash
jj --no-pager commit -m "feat(proto): add WebService.WebListSessionStreams RPC

Thin web-gateway wrapper for CoreService.ListSessionStreams. Handler
implementation follows in the next commit."
```

---

## Task 6: Implement `WebListSessionStreams` handler (TDD)

**Files:**

- Modify: `internal/web/handler.go`
- Modify: `internal/web/handler_test.go`

- [ ] **Step 6.1: Write failing test**

Append to `internal/web/handler_test.go`:

```go
func TestWebListSessionStreams_ProxiesToCore(t *testing.T) {
	mockClient := mocks.NewMockCoreClient(t)
	mockClient.EXPECT().
		ListSessionStreams(mock.Anything, &corev1.ListSessionStreamsRequest{SessionId: "s1"}).
		Return(&corev1.ListSessionStreamsResponse{Streams: []string{"character:c1", "location:l1"}}, nil)

	h := NewHandler(mockClient)

	resp, err := h.WebListSessionStreams(context.Background(),
		connect.NewRequest(&webv1.WebListSessionStreamsRequest{SessionId: "s1"}))
	require.NoError(t, err)
	assert.Equal(t, []string{"character:c1", "location:l1"}, resp.Msg.GetStreams())
}

func TestWebListSessionStreams_PassesErrorsThrough(t *testing.T) {
	mockClient := mocks.NewMockCoreClient(t)
	mockClient.EXPECT().
		ListSessionStreams(mock.Anything, mock.Anything).
		Return(nil, oops.Code("SESSION_EXPIRED").Errorf("expired"))

	h := NewHandler(mockClient)

	_, err := h.WebListSessionStreams(context.Background(),
		connect.NewRequest(&webv1.WebListSessionStreamsRequest{SessionId: "s1"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SESSION_EXPIRED")
}
```

**Imports to add if missing** (top of the file):

```go
import (
    // ... existing ...
    "github.com/samber/oops"
    "github.com/stretchr/testify/mock"
)
```

- [ ] **Step 6.2: Run test to verify it fails**

Run:

```bash
GOWORK=off task test -- -run TestWebListSessionStreams ./internal/web/
```

Expected: compile error (`WebListSessionStreams` method not defined).

- [ ] **Step 6.3: Implement the handler**

Add to `internal/web/handler.go` (near `WebQueryStreamHistory` around line 396):

```go
// WebListSessionStreams proxies stream enumeration requests to CoreService.
// Authorization is enforced by the core service.
func (h *Handler) WebListSessionStreams(ctx context.Context, req *connect.Request[webv1.WebListSessionStreamsRequest]) (*connect.Response[webv1.WebListSessionStreamsResponse], error) {
	slog.DebugContext(ctx, "web: WebListSessionStreams",
		"session_id", req.Msg.GetSessionId(),
	)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.client.ListSessionStreams(rpcCtx, &corev1.ListSessionStreamsRequest{
		SessionId: req.Msg.GetSessionId(),
	})
	if err != nil {
		slog.ErrorContext(ctx, "web: list session streams RPC failed",
			"session_id", req.Msg.GetSessionId(), "error", err)
		return nil, err //nolint:wrapcheck // gRPC status errors pass through so clients can distinguish SESSION_EXPIRED / SESSION_NOT_FOUND / INVALID_ARGUMENT.
	}

	return connect.NewResponse(&webv1.WebListSessionStreamsResponse{
		Streams: resp.GetStreams(),
	}), nil
}
```

- [ ] **Step 6.4: Run tests to verify pass**

Run:

```bash
GOWORK=off task test -- -run TestWebListSessionStreams ./internal/web/
```

Expected: PASS (2/2).

- [ ] **Step 6.5: Full lint + unit tests for regression**

Run:

```bash
GOWORK=off task lint && GOWORK=off task test
```

Expected: all pass. The type-assertion `var _ webv1connect.WebServiceHandler = (*Handler)(nil)` at `handler.go:78` now satisfies cleanly.

- [ ] **Step 6.6: Commit**

```bash
jj --no-pager commit -m "feat(web): WebListSessionStreams handler

Thin proxy to CoreService.ListSessionStreams. Matches the
WebQueryStreamHistory pattern: rpcCtx with timeout, pass gRPC errors
through unwrapped so clients can distinguish status codes."
```

---

## Task 7: Install and configure vitest (prerequisite for client unit tests)

**Rationale:** The web project currently has no unit test runner (`package.json` shows only `dev`, `build`, `preview`, `check`). The CLAUDE.md mentions vitest aspirationally. Tasks 8-11 need it. Install once here.

**Files:**

- Modify: `web/package.json`
- Modify: `web/vite.config.ts`
- Create: (no test files yet — Task 8 adds the first)

- [ ] **Step 7.1: Install vitest and jsdom**

Run:

```bash
cd web && pnpm add -D vitest @vitest/ui jsdom
```

- [ ] **Step 7.2: Add test script to `package.json`**

In `web/package.json`, add to the `"scripts"` object:

```json
    "test:unit": "vitest run",
    "test:unit:watch": "vitest"
```

- [ ] **Step 7.3: Configure vitest in `vite.config.ts`**

Read the current `web/vite.config.ts`. It uses a `defineConfig` export from `vite`. vitest reads the same file — add a `test` block inside `defineConfig`. If the config doesn't already import from `vitest/config`, change the import:

```typescript
// Before:
// import { defineConfig } from 'vite';
// After:
import { defineConfig } from 'vitest/config';
```

Then add the `test` block to the config object (adjacent to plugins/build sections — exact placement depends on what the file currently looks like):

```typescript
export default defineConfig({
  // ... existing config (plugins, etc.) ...
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts'],
    globals: false,
  },
});
```

- [ ] **Step 7.4: Write a sanity test to verify the setup**

Create `web/src/lib/test-sanity.test.ts` (temporary — delete in the same commit):

```typescript
import { describe, it, expect } from 'vitest';

describe('vitest sanity', () => {
  it('runs', () => {
    expect(1 + 1).toBe(2);
  });
});
```

- [ ] **Step 7.5: Run the test**

Run:

```bash
cd web && pnpm test:unit
```

Expected: PASS (1/1). If vitest can't find the file or errors on the config, adjust `vite.config.ts` per the vitest docs.

- [ ] **Step 7.6: Delete the sanity test**

Remove `web/src/lib/test-sanity.test.ts`.

- [ ] **Step 7.7: Commit**

```bash
jj --no-pager commit -m "chore(web): install and configure vitest

Prerequisite for client-side unit tests of the backfill helper and
isUnimplementedError utility. Tests live in src/**/*.test.ts and run
via 'pnpm test:unit' (wrapping 'vitest run')."
```

---

## Task 8: `isUnimplementedError` utility (TDD)

**Files:**

- Create: `web/src/lib/connect/errors.ts`
- Create: `web/src/lib/connect/errors.test.ts`

- [ ] **Step 8.1: Write failing test**

Create `web/src/lib/connect/errors.test.ts`:

```typescript
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect } from 'vitest';
import { ConnectError, Code } from '@connectrpc/connect';
import { isUnimplementedError } from './errors';

describe('isUnimplementedError', () => {
  it('returns true for ConnectError with Code.Unimplemented', () => {
    const err = new ConnectError('not implemented', Code.Unimplemented);
    expect(isUnimplementedError(err)).toBe(true);
  });

  it('returns false for ConnectError with a different code', () => {
    const err = new ConnectError('not found', Code.NotFound);
    expect(isUnimplementedError(err)).toBe(false);
  });

  it('returns false for non-ConnectError values', () => {
    expect(isUnimplementedError(new Error('boom'))).toBe(false);
    expect(isUnimplementedError('boom')).toBe(false);
    expect(isUnimplementedError(null)).toBe(false);
    expect(isUnimplementedError(undefined)).toBe(false);
  });
});
```

- [ ] **Step 8.2: Run test to verify it fails**

Run (from `web/`):

```bash
cd web && pnpm test:unit errors.test
```

Expected: FAIL — module `./errors` not found.

- [ ] **Step 8.3: Implement `errors.ts`**

Create `web/src/lib/connect/errors.ts`:

```typescript
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { ConnectError, Code } from '@connectrpc/connect';

/**
 * Returns true when the given value is a ConnectRPC error with code
 * Unimplemented. Used by the web client to tolerate staged rollouts where
 * the server may not yet implement a new RPC.
 */
export function isUnimplementedError(e: unknown): boolean {
  return e instanceof ConnectError && e.code === Code.Unimplemented;
}
```

- [ ] **Step 8.4: Run test to verify pass**

Run:

```bash
cd web && pnpm test:unit errors.test
```

Expected: PASS (3/3).

- [ ] **Step 8.5: Commit**

```bash
jj --no-pager commit -m "feat(web): isUnimplementedError utility for staged rollouts

Returns true for ConnectError with Code.Unimplemented. Used by the
backfill integration to tolerate servers that predate a new RPC."
```

---

## Task 9: `backfillStreams` helper — empty-streams shortcut (TDD)

**Files:**

- Create: `web/src/lib/backfill/streamBackfill.ts`
- Create: `web/src/lib/backfill/streamBackfill.test.ts`

- [ ] **Step 9.1: Write failing test**

Create `web/src/lib/backfill/streamBackfill.test.ts`:

```typescript
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, vi } from 'vitest';
import { backfillStreams } from './streamBackfill';

function makeClient() {
  return {
    webQueryStreamHistory: vi.fn(),
  };
}

describe('backfillStreams', () => {
  it('returns empty result for empty stream list without making RPC calls', async () => {
    const client = makeClient();
    const result = await backfillStreams(client as never, 'sess-1', []);
    expect(result.events).toEqual([]);
    expect(result.failedStreams).toEqual([]);
    expect(client.webQueryStreamHistory).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 9.2: Run test — fails (module not found)**

Run:

```bash
cd web && pnpm test:unit streamBackfill.test
```

Expected: FAIL.

- [ ] **Step 9.3: Minimal implementation**

Create `web/src/lib/backfill/streamBackfill.ts`:

```typescript
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import type { Client } from '@connectrpc/connect';
import { ConnectError, Code } from '@connectrpc/connect';
import type { WebService } from '$lib/connect/holomush/web/v1/web_pb';
import type { GameEvent } from '$lib/connect/holomush/web/v1/web_pb';

export interface BackfillResult {
  events: GameEvent[];
  failedStreams: string[];
}

export interface BackfillOpts {
  count?: number;
  signal?: AbortSignal;
}

const DEFAULT_COUNT = 150;

export async function backfillStreams(
  client: Client<typeof WebService>,
  sessionId: string,
  streams: string[],
  opts: BackfillOpts = {},
): Promise<BackfillResult> {
  if (streams.length === 0) {
    return { events: [], failedStreams: [] };
  }
  // Full implementation added in subsequent tasks.
  throw new Error('not implemented');
}
```

**Note:** The actual type for `client` depends on the generated code. If `WebService` isn't the right symbol name, use `WebServiceClient` or the type returned by `createClient(WebService, transport)`. Check `web/src/routes/(authed)/terminal/+page.svelte:24` for the invocation pattern.

- [ ] **Step 9.4: Run test — passes**

Run:

```bash
cd web && pnpm test:unit streamBackfill.test
```

Expected: PASS (1/1).

- [ ] **Step 9.5: Commit**

```bash
jj --no-pager commit -m "feat(web): backfillStreams helper — empty-streams shortcut"
```

---

## Task 10: `backfillStreams` — success path + merge order (TDD)

**Files:**

- Modify: `web/src/lib/backfill/streamBackfill.ts`
- Modify: `web/src/lib/backfill/streamBackfill.test.ts`

- [ ] **Step 10.1: Add failing test for single-stream success**

Append to `streamBackfill.test.ts`:

```typescript
it('fetches a single stream and returns its events', async () => {
  const client = makeClient();
  client.webQueryStreamHistory.mockResolvedValueOnce({
    events: [
      { eventId: 'e-a', timestamp: 100n, type: 'say' },
      { eventId: 'e-b', timestamp: 200n, type: 'say' },
    ],
    hasMore: false,
  });

  const result = await backfillStreams(client as never, 'sess-1', ['location:l1']);
  expect(result.events.map((e) => e.eventId)).toEqual(['e-a', 'e-b']);
  expect(result.failedStreams).toEqual([]);
  expect(client.webQueryStreamHistory).toHaveBeenCalledWith(
    { sessionId: 'sess-1', stream: 'location:l1', count: 150, beforeId: '', notBeforeMs: 0n },
    expect.anything(),
  );
});

it('merges events from multiple streams in ascending (timestamp, eventId) order', async () => {
  const client = makeClient();
  // Two streams: character has event-at-t100, location has events at t50 and t200.
  client.webQueryStreamHistory.mockImplementation((req: { stream: string }) => {
    if (req.stream === 'character:c1') {
      return Promise.resolve({
        events: [{ eventId: 'e-mid', timestamp: 100n, type: 'say' }],
        hasMore: false,
      });
    }
    return Promise.resolve({
      events: [
        { eventId: 'e-first', timestamp: 50n, type: 'say' },
        { eventId: 'e-last', timestamp: 200n, type: 'say' },
      ],
      hasMore: false,
    });
  });

  const result = await backfillStreams(client as never, 'sess-1', ['character:c1', 'location:l1']);
  expect(result.events.map((e) => e.eventId)).toEqual(['e-first', 'e-mid', 'e-last']);
});

it('uses eventId as tiebreaker for same-timestamp events', async () => {
  const client = makeClient();
  client.webQueryStreamHistory.mockImplementation((req: { stream: string }) => {
    if (req.stream === 'character:c1') {
      return Promise.resolve({
        events: [{ eventId: 'e-beta', timestamp: 100n, type: 'say' }],
        hasMore: false,
      });
    }
    return Promise.resolve({
      events: [{ eventId: 'e-alpha', timestamp: 100n, type: 'say' }],
      hasMore: false,
    });
  });

  const result = await backfillStreams(client as never, 'sess-1', ['character:c1', 'location:l1']);
  expect(result.events.map((e) => e.eventId)).toEqual(['e-alpha', 'e-beta']);
});
```

- [ ] **Step 10.2: Run tests — verify they fail**

Run:

```bash
cd web && pnpm test:unit streamBackfill.test
```

Expected: FAIL — "not implemented" error from the helper.

- [ ] **Step 10.3: Implement success path**

Update `streamBackfill.ts` to fan out + merge. Replace the function body:

```typescript
export async function backfillStreams(
  client: Client<typeof WebService>,
  sessionId: string,
  streams: string[],
  opts: BackfillOpts = {},
): Promise<BackfillResult> {
  if (streams.length === 0) {
    return { events: [], failedStreams: [] };
  }
  const count = opts.count ?? DEFAULT_COUNT;

  const results = await Promise.all(
    streams.map((stream) => fetchOneStream(client, sessionId, stream, count, opts.signal)),
  );

  const events: GameEvent[] = [];
  const failedStreams: string[] = [];
  for (let i = 0; i < results.length; i++) {
    const r = results[i];
    if (r.ok) {
      events.push(...r.events);
    } else {
      failedStreams.push(streams[i]);
    }
  }

  events.sort((a, b) => {
    const at = typeof a.timestamp === 'bigint' ? a.timestamp : BigInt(a.timestamp ?? 0);
    const bt = typeof b.timestamp === 'bigint' ? b.timestamp : BigInt(b.timestamp ?? 0);
    if (at < bt) return -1;
    if (at > bt) return 1;
    const aid = a.eventId ?? '';
    const bid = b.eventId ?? '';
    if (aid < bid) return -1;
    if (aid > bid) return 1;
    return 0;
  });

  return { events, failedStreams };
}

type FetchResult =
  | { ok: true; events: GameEvent[] }
  | { ok: false; error: unknown };

async function fetchOneStream(
  client: Client<typeof WebService>,
  sessionId: string,
  stream: string,
  count: number,
  signal?: AbortSignal,
): Promise<FetchResult> {
  try {
    const resp = await client.webQueryStreamHistory(
      {
        sessionId,
        stream,
        count,
        beforeId: '',
        notBeforeMs: 0n,
      },
      { signal },
    );
    return { ok: true, events: resp.events };
  } catch (e) {
    return { ok: false, error: e };
  }
}
```

- [ ] **Step 10.4: Run tests — verify pass**

Run:

```bash
cd web && pnpm test:unit streamBackfill.test
```

Expected: PASS (all four tests: empty + single + merge + tiebreaker).

- [ ] **Step 10.5: Commit**

```bash
jj --no-pager commit -m "feat(web): backfillStreams success path with (timestamp, eventId) merge"
```

---

## Task 11: `backfillStreams` — retry once on transient errors (TDD)

**Files:**

- Modify: `web/src/lib/backfill/streamBackfill.ts`
- Modify: `web/src/lib/backfill/streamBackfill.test.ts`

- [ ] **Step 11.1: Add failing tests**

Append to `streamBackfill.test.ts`:

```typescript
import { ConnectError, Code } from '@connectrpc/connect';

// ... existing tests ...

it('retries once on transient error, then succeeds', async () => {
  const client = makeClient();
  const transient = new ConnectError('network', Code.Unavailable);
  client.webQueryStreamHistory
    .mockRejectedValueOnce(transient)
    .mockResolvedValueOnce({
      events: [{ eventId: 'e-x', timestamp: 10n, type: 'say' }],
      hasMore: false,
    });

  const result = await backfillStreams(client as never, 'sess-1', ['location:l1'], {
    count: 150,
  });
  expect(result.events.map((e) => e.eventId)).toEqual(['e-x']);
  expect(result.failedStreams).toEqual([]);
  expect(client.webQueryStreamHistory).toHaveBeenCalledTimes(2);
});

it('records failed stream when transient error persists after retry', async () => {
  const client = makeClient();
  const transient = new ConnectError('network', Code.Unavailable);
  client.webQueryStreamHistory
    .mockRejectedValueOnce(transient)
    .mockRejectedValueOnce(transient);

  const result = await backfillStreams(client as never, 'sess-1', ['location:l1']);
  expect(result.events).toEqual([]);
  expect(result.failedStreams).toEqual(['location:l1']);
  expect(client.webQueryStreamHistory).toHaveBeenCalledTimes(2);
});

it('does NOT retry on permanent errors (PermissionDenied, NotFound, InvalidArgument)', async () => {
  const permanents = [
    new ConnectError('denied', Code.PermissionDenied),
    new ConnectError('missing', Code.NotFound),
    new ConnectError('bad', Code.InvalidArgument),
  ];
  for (const err of permanents) {
    const client = makeClient();
    client.webQueryStreamHistory.mockRejectedValueOnce(err);
    const result = await backfillStreams(client as never, 'sess-1', ['location:l1']);
    expect(result.failedStreams).toEqual(['location:l1']);
    expect(client.webQueryStreamHistory).toHaveBeenCalledTimes(1);
  }
});
```

- [ ] **Step 11.2: Run tests — verify failures**

Run:

```bash
cd web && pnpm test:unit streamBackfill.test
```

Expected: 3 new tests FAIL (retry not implemented; permanent failures not categorized).

- [ ] **Step 11.3: Implement retry logic**

In `streamBackfill.ts`, replace `fetchOneStream` with:

```typescript
const RETRY_DELAY_MS = 500;

function isRetryable(e: unknown): boolean {
  if (!(e instanceof ConnectError)) return true; // non-Connect errors: treat as transient
  switch (e.code) {
    case Code.Unavailable:
    case Code.DeadlineExceeded:
    case Code.Internal:
    case Code.Unknown:
      return true;
    default:
      return false;
  }
}

async function fetchOneStream(
  client: Client<typeof WebService>,
  sessionId: string,
  stream: string,
  count: number,
  signal?: AbortSignal,
): Promise<FetchResult> {
  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      const resp = await client.webQueryStreamHistory(
        {
          sessionId,
          stream,
          count,
          beforeId: '',
          notBeforeMs: 0n,
        },
        { signal },
      );
      return { ok: true, events: resp.events };
    } catch (e) {
      if (signal?.aborted) {
        return { ok: false, error: e };
      }
      if (attempt === 1 || !isRetryable(e)) {
        return { ok: false, error: e };
      }
      await sleep(RETRY_DELAY_MS, signal);
    }
  }
  return { ok: false, error: new Error('unreachable') };
}

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const t = setTimeout(resolve, ms);
    signal?.addEventListener('abort', () => {
      clearTimeout(t);
      reject(signal.reason);
    });
  });
}
```

- [ ] **Step 11.4: Run tests — verify pass**

Run:

```bash
cd web && pnpm test:unit streamBackfill.test
```

Expected: all tests PASS.

- [ ] **Step 11.5: Commit**

```bash
jj --no-pager commit -m "feat(web): backfillStreams retries transient errors once

Retryable: Unavailable, DeadlineExceeded, Internal, Unknown, non-Connect
errors. Permanent (PermissionDenied, NotFound, InvalidArgument, etc.)
fail fast. Retry delay 500ms, honors AbortSignal."
```

---

## Task 12: `backfillStreams` — honors `AbortSignal` (TDD)

**Files:**

- Modify: `web/src/lib/backfill/streamBackfill.test.ts`

- [ ] **Step 12.1: Add failing test**

Append:

```typescript
it('aborts in-flight calls when AbortSignal is triggered', async () => {
  const client = makeClient();
  // Simulate a call that checks signal and rejects if aborted.
  client.webQueryStreamHistory.mockImplementation(
    (_req: unknown, opts?: { signal?: AbortSignal }) =>
      new Promise((_resolve, reject) => {
        opts?.signal?.addEventListener('abort', () => {
          reject(new DOMException('aborted', 'AbortError'));
        });
      }),
  );

  const controller = new AbortController();
  const promise = backfillStreams(client as never, 'sess-1', ['location:l1'], {
    signal: controller.signal,
  });
  // Fire abort immediately.
  controller.abort();
  const result = await promise;
  expect(result.events).toEqual([]);
  expect(result.failedStreams).toEqual(['location:l1']);
});
```

- [ ] **Step 12.2: Run test — expected to pass already**

Run:

```bash
cd web && pnpm test:unit streamBackfill.test
```

Expected: PASS (the retry loop already short-circuits on `signal.aborted`). If it fails, verify the abort-handling path in `fetchOneStream` propagates correctly.

- [ ] **Step 12.3: Commit**

```bash
jj --no-pager commit -m "test(web): backfillStreams honors AbortSignal mid-flight"
```

---

## Task 13: Terminal page integration — `hydrateAndStream`

**Files:**

- Modify: `web/src/routes/(authed)/terminal/+page.svelte`

**Why this is a single commit:** the replacement of `startStreaming` with `hydrateAndStream` is one coherent change — splitting it produces intermediate broken states (e.g., replayFromCursor removed but no backfill yet, or backfill added but Subscribe not buffered).

- [ ] **Step 13.1: Read the current page carefully**

Run:

```bash
GOWORK=off task test  # baseline: all client tests pass
```

Also re-read `web/src/routes/(authed)/terminal/+page.svelte` lines 126-193 to understand the current `startStreaming` flow before editing.

- [ ] **Step 13.2: Edit `+page.svelte`**

Modify the imports at the top to add:

```typescript
import { backfillStreams } from '$lib/backfill/streamBackfill';
import { isUnimplementedError } from '$lib/connect/errors';
```

Replace the `startStreaming` function (currently lines 126-193) with `hydrateAndStream`:

```typescript
async function hydrateAndStream() {
  abortController?.abort();
  abortController = new AbortController();
  clearLines();
  replayActive.set(true);
  const generation = ++streamGeneration;
  createStreamReadyGate(generation);
  let inReplay = true;

  if (streamSpan) {
    streamSpan.addEvent('reconnect');
    streamSpan.end();
  }
  streamSpan = tracer.startSpan('stream.lifecycle');

  const liveBuffer: GameEvent[] = [];
  const seenEventIds = new Set<string>();
  let backfillDone = false;

  // --- Subscribe: runs in parallel with backfill; events are buffered
  // until backfill completes, then drained. Subscribe replay-phase events
  // represent events the user MISSED while detached — they are live, not
  // replayed, so they render as replayed=false after draining.
  //
  // streamReadyGate resolves on REPLAY_COMPLETE (not backfill). This is
  // deliberate: commands sent during backfill get a response via Subscribe
  // which is buffered and drains as live after the dimmed scrollback.
  const subscribePromise = (async () => {
    try {
      // NOTE: replayFromCursor is removed — the field is reserved in the
      // proto (web.proto:88-90) and Subscribe is now server-driven replay
      // + live. See spec §6.2.
      for await (const response of client.streamEvents(
        { sessionId },
        { signal: abortController.signal },
      )) {
        if (response.frame.case === 'control') {
          const ctrl = response.frame.value;
          if (ctrl.signal === ControlSignal.REPLAY_COMPLETE) {
            inReplay = false;
            resolveStreamReady(generation);
          } else if (ctrl.signal === ControlSignal.STREAM_CLOSED) {
            if (ctrl.message) {
              appendLine(
                { type: 'system', characterName: '', text: ctrl.message, channel: 0 },
                false,
              );
            }
            clearCharacterSession();
            connected = false;
            sessionId = '';
            rejectStreamReady(generation, new Error(ctrl.message || 'Stream closed'));
            streamSpan?.end();
            streamSpan = null;
            goto('/characters');
            return;
          }
        } else if (response.frame.case === 'event') {
          const ev = response.frame.value;
          if (pendingCommandSpan && ev.type === 'command_response') {
            pendingCommandSpan.end();
            pendingCommandSpan = null;
          }
          if (!backfillDone) {
            liveBuffer.push(ev);
          } else {
            if (ev.eventId && seenEventIds.has(ev.eventId)) continue;
            if (ev.eventId) seenEventIds.add(ev.eventId);
            routeEvent(ev, false);
          }
        }
      }
    } catch (e) {
      if (e instanceof Error && e.name !== 'AbortError') {
        connected = false;
        error = 'Connection lost. Click "Reconnect" or refresh the page.';
        rejectStreamReady(generation, e);
      } else {
        rejectStreamReady(generation, e);
      }
    } finally {
      if (streamReadyGate?.generation === generation) {
        rejectStreamReady(generation, new Error('Event stream ended before replay completed'));
      }
      streamSpan?.end();
      streamSpan = null;
    }
  })();

  // --- Backfill: enumerate streams, fan-out QueryStreamHistory, render
  // results as replayed=true. On any failure, log and continue — the
  // user's scrollback will be under-populated but the terminal works.
  try {
    let streams: string[] = [];
    try {
      const resp = await client.webListSessionStreams({ sessionId });
      streams = resp.streams;
    } catch (e) {
      if (isUnimplementedError(e)) {
        console.info('[backfill] WebListSessionStreams not available; skipping backfill');
      } else {
        console.warn('[backfill] stream enumeration failed', e);
      }
    }
    const { events, failedStreams } = await backfillStreams(client, sessionId, streams, {
      signal: abortController.signal,
    });
    if (failedStreams.length > 0) {
      console.warn('[backfill] streams failed', { failedStreams });
    }
    for (const ev of events) {
      if (ev.eventId) seenEventIds.add(ev.eventId);
      routeEvent(ev, true);
    }
  } finally {
    backfillDone = true;
    replayActive.set(false);
    // Drain Subscribe events that arrived during backfill, deduping against
    // backfill.
    for (const ev of liveBuffer) {
      if (ev.eventId && seenEventIds.has(ev.eventId)) continue;
      if (ev.eventId) seenEventIds.add(ev.eventId);
      routeEvent(ev, false);
    }
    liveBuffer.length = 0;
  }

  await subscribePromise;
}
```

Also:

- Change the call site in `onMount` from `startStreaming()` to `hydrateAndStream()`.
- Change the call site in `reconnect()` from `startStreaming()` to `hydrateAndStream()`.
- Add `import type { GameEvent } from '$lib/connect/holomush/web/v1/web_pb';` to the imports block.

**Critical: confirm `replayFromCursor` is no longer passed anywhere.** Grep the file for `replayFromCursor` — there should be zero hits.

- [ ] **Step 13.3: Run `pnpm check` (svelte-check + TypeScript)**

Run:

```bash
cd web && pnpm check
```

Expected: no type errors. If `client.webListSessionStreams` is complained about, re-run `task web:generate` and verify.

- [ ] **Step 13.4: Run client unit tests**

Run:

```bash
cd web && pnpm test:unit
```

Expected: all pass (backfill helper + errors util).

- [ ] **Step 13.5: Smoke test in dev**

Run in one terminal:

```bash
GOWORK=off task dev
```

In another terminal (optional manual check):

- Open `http://localhost:5173`, log in as guest, say "smoketest-a", "smoketest-b".
- Reload the page.
- **Expected:** scrollback shows "smoketest-a" and "smoketest-b" dimmed, with "--- LIVE ---" separator, then subsequent commands render un-dimmed.
- Kill the dev server after smoke test.

- [ ] **Step 13.6: Commit**

```bash
jj --no-pager commit -m "feat(web): terminal page calls WebQueryStreamHistory on mount for backfill

Replaces startStreaming with hydrateAndStream. Runs Subscribe + backfill
in parallel; Subscribe events buffered until backfill completes, then
drained with event_id-based dedup.

Drops the dead replayFromCursor field (reserved in proto since B7).
streamReadyGate still resolves on REPLAY_COMPLETE so commands can send
during backfill — responses render un-dimmed after the dimmed scrollback."
```

---

## Task 14: Un-skip existing E2E test + add separator assertion

**Files:**

- Modify: `web/e2e/terminal.spec.ts`

- [ ] **Step 14.1: Un-skip the test**

In `web/e2e/terminal.spec.ts`, locate line 289:

```typescript
test.skip('page reload replays prior events from multiple guests', async ({ browser }) => {
```

Change `test.skip` to `test`.

Also remove the TODO comment block immediately above (lines 286-288):

```typescript
  // TODO(holomush-oy6e.13): Un-skip when QueryStreamHistory RPC (B9) lands
  // and the web client calls it on mount for reload backfill.
  // See also: holomush-oy6e.9 notes.
```

- [ ] **Step 14.2: Add `--- LIVE ---` separator assertion**

Inside the same test, after the three "dimmed count" assertions (around line 361 — search for `dimmedCount`), add:

```typescript
  // Assert the --- LIVE --- separator appears between replayed and live events.
  await expect(
    page1.locator('.separator', { hasText: '--- LIVE ---' }),
  ).toBeVisible({ timeout: 5000 });
```

Place this BEFORE the `reloadedInput.fill(...)` step, so the separator is verified before the live event is added (the separator already renders once replayed events are present and the live event appears).

Actually — looking at the flow, the separator only renders when a live event appears AFTER replayed events. So this assertion goes AFTER the `say delta-${token}` is emitted and the `deltaDimmed` count is checked. Place it immediately after the `expect(deltaDimmed).toBe(0);` line:

```typescript
  expect(deltaDimmed).toBe(0);

  // Separator between replayed and live events.
  await expect(
    page1.locator('.separator', { hasText: '--- LIVE ---' }),
  ).toBeVisible({ timeout: 5000 });
```

- [ ] **Step 14.3: Run the test**

Run (from repo root):

```bash
GOWORK=off task test:e2e -- --grep "page reload replays prior events from multiple guests"
```

Expected: PASS. If the separator assertion fails, open the DOM to verify the `.separator` class matches what `TerminalView.svelte:50-51` actually renders and adjust the locator.

- [ ] **Step 14.4: Commit**

```bash
jj --no-pager commit -m "test(e2e): un-skip reload-replay test, add separator assertion

B13 satisfies the bead's acceptance criterion. Adds a --- LIVE ---
separator visibility check to catch boundary regressions that the
dimmed-count assertions miss."
```

---

## Task 15: New E2E test — detach + accumulated events: no duplicates

**Files:**

- Modify: `web/e2e/terminal.spec.ts`

- [ ] **Step 15.1: Add the test**

Append (after the un-skipped test, before the "command history persists across reconnect" test):

```typescript
test('detach + accumulated events + reload produces no duplicate scrollback entries', async ({
  browser,
}) => {
  // Two guests in the same location.
  const ctx1 = await browser.newContext();
  const ctx2 = await browser.newContext();
  const page1 = await ctx1.newPage();
  const page2 = await ctx2.newPage();

  await connectAsGuest(page1);
  await connectAsGuest(page2);

  // Guest 1 says one event to ensure its session is well-seeded.
  const token = Date.now();
  const input1 = page1.locator('textarea');
  await input1.fill(`say seed-${token}`);
  await input1.press('Enter');
  await expect(
    page1.locator('[data-testid="event"]').filter({ hasText: `seed-${token}` }),
  ).toBeVisible({ timeout: 10000 });

  // "Detach" page1 by closing it. Guest 2 emits three events while page1 is gone.
  const sessionId = await getClientSessionId(page1);
  expect(sessionId).toBeTruthy();
  await page1.close();

  const input2 = page2.locator('textarea');
  for (const label of ['detached-a', 'detached-b', 'detached-c']) {
    await input2.fill(`say ${label}-${token}`);
    await input2.press('Enter');
    await expect(
      page2.locator('[data-testid="event"]').filter({ hasText: `${label}-${token}` }),
    ).toBeVisible({ timeout: 10000 });
  }

  // Reopen page1 (same session via sessionStorage).
  const page1Reopened = await ctx1.newPage();
  await page1Reopened.goto('/terminal');
  await expect(page1Reopened.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

  // Each detached-* event must appear EXACTLY ONCE, even though Subscribe's
  // cursor-based replay AND QueryStreamHistory both deliver them.
  for (const label of ['detached-a', 'detached-b', 'detached-c']) {
    const count = await page1Reopened
      .locator('[data-testid="event"]')
      .filter({ hasText: `${label}-${token}` })
      .count();
    expect(count, `expected exactly one rendering of ${label}-${token}`).toBe(1);
  }

  await ctx1.close();
  await ctx2.close();
});
```

- [ ] **Step 15.2: Run the test**

Run:

```bash
GOWORK=off task test:e2e -- --grep "detach \\+ accumulated"
```

Expected: PASS.

- [ ] **Step 15.3: Commit**

```bash
jj --no-pager commit -m "test(e2e): detach + accumulated events produces no duplicates

Exercises spec §7.1 scenario 3 (the systematic dup path that motivated
adding event_id). Subscribe's cursor-based replay AND QueryStreamHistory
both deliver the events committed while detached; the client's eventId
set deduplicates them."
```

---

## Task 16: Full PR prep + push

- [ ] **Step 16.1: Run full pre-flight**

From the worktree:

```bash
GOWORK=off task pr-prep
```

Expected: ALL checks green (lint, format, schema, license, unit, integration, E2E). If any fail, fix before proceeding.

**MUST** be full green — no approximations (per project memory `feedback_pr_prep_must_run`).

- [ ] **Step 16.2: Review commit graph**

Run:

```bash
jj --no-pager log -r 'main..@'
```

Expected: clean linear series of commits (one per task).

- [ ] **Step 16.3: Push and open PR**

```bash
jj bookmark set b13 -r @
jj git push -b b13
gh pr create --title "feat(web): B13 reload backfill via QueryStreamHistory" --body "$(cat <<'EOF'
## Summary

- New \`CoreService.ListSessionStreams\` RPC wraps \`focusCoordinator.RestoreFocus\` for client stream enumeration.
- New \`WebService.WebListSessionStreams\` RPC proxies to core.
- Adds \`event_id\` to \`webv1.GameEvent\` for deterministic dedup between QueryStreamHistory backfill and Subscribe output.
- New web helper \`backfillStreams\` fans out per stream, retries once, merges by \`(timestamp, event_id)\`.
- Terminal page runs Subscribe + backfill in parallel with live-event buffering and dedup.
- Un-skips the canonical reload-replay E2E test + adds a detach/accumulated no-duplicates test.

Closes bead holomush-oy6e.13.

Design: \`docs/superpowers/specs/2026-04-17-b13-web-client-backfill-design.md\`
Plan: \`docs/superpowers/plans/2026-04-18-b13-web-client-backfill.md\`

## Test plan

- [x] \`task pr-prep\` green (lint + format + schema + license + unit + integration + E2E)
- [x] Un-skipped E2E test passes locally
- [x] New detach + accumulated E2E test passes locally
- [x] Smoke test in dev server: reload shows dimmed scrollback
EOF
)"
```

- [ ] **Step 16.4: Close the bead**

From main repo dir:

```bash
bd close holomush-oy6e.13 --reason "Merged via PR (link below)"
```

- [ ] **Step 16.5: File follow-up beads**

```bash
# Each uses --deps discovered-from:holomush-oy6e.13

bd create "Backfill scroll-up pagination (older history via before_id)" \
  --description="When user scrolls to top of terminal scrollback, fetch older events via WebQueryStreamHistory with before_id=oldestEventId. Extends B13's single-page fetch to full tmux-like scrollback." \
  --type=feature --priority=3 --deps discovered-from:holomush-oy6e.13

bd create "localStorage scrollback persistence" \
  --description="Persist dimmed scrollback to localStorage keyed on sessionId. On reload, render from localStorage immediately then merge with QueryStreamHistory via event_id dedup. Prerequisite (event_id) landed in B13." \
  --type=feature --priority=3 --deps discovered-from:holomush-oy6e.13

bd create "OTel tracing for backfill" \
  --description="Add backfill.* span with attributes: stream count, event count, failed streams, duration. Measures B13's SHOULD <1s goal." \
  --type=task --priority=3 --deps discovered-from:holomush-oy6e.13

bd create "A11y: ARIA-live announcement for backfill loading" \
  --description="Announce 'Restoring scrollback…' via aria-live=polite while replayActive is true. B13 reused the existing --- REPLAY --- visual indicator without a screen-reader equivalent." \
  --type=task --priority=3 --deps discovered-from:holomush-oy6e.13
```

- [ ] **Step 16.6: Update epic notes**

```bash
bd update holomush-oy6e --notes "2026-04-18: B13 merged — web-terminal backfill via ListSessionStreams + WebQueryStreamHistory + event_id dedup. Next: channel UI work per holomush-oy6e.11."
```

---

## Open follow-ups (informational — beads filed in 15.5)

These are captured in the spec's §10 and will be filed as beads only at task completion:

- Scroll-up pagination for older history
- localStorage scrollback persistence (needs event_id — landed in this PR)
- OTel tracing for backfill
- A11y ARIA-live announcement
- Concurrency cap on per-stream fan-out (deferred until telemetry)
- Per-stream `count` tuning (deferred)
- B9 auth model review (independent concern)

---

## Notes for implementers

- **Worktree:** `/Users/sean/Code/github.com/holomush/.worktrees/b13-web-backfill/`. All commands run from there unless otherwise noted.
- **`GOWORK=off` prefix:** required for Go commands due to the `task gowork` duplicate-module bug tracked as `holomush-h8xj`. Web (pnpm) commands are unaffected.
- **Beads commands:** run from `/Volumes/Code/github.com/holomush/holomush/` (main repo) — the worktree's `.beads/` dir has permission issues.
- **jj workflow:** `jj commit -m "..."` creates a commit from the working copy and starts a new empty change. NOT `git commit`. To amend the current description: `jj describe -m "..."`.
- **Proto fields never reuse numbers.** `event_id = 9` is a new field; don't use any reserved number.
