<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# B9: QueryStreamHistory RPC Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a client-facing `QueryStreamHistory` unary RPC to CoreService with two-layer authorization (membership gate for private streams + ABAC for public), ULID cursor pagination, and web gateway proxy.

**Architecture:** Extends `ReplayTail` with a `beforeID` parameter for cursor-based pagination. The handler sits in `internal/grpc/query_stream_history.go` and delegates to `EventStore.ReplayTail`. Authorization uses invariant I-17 (private streams require membership, enforced in code before ABAC) plus `AccessPolicyEngine.Evaluate` for public streams. The web gateway proxies via a thin `WebQueryStreamHistory` handler.

**Tech Stack:** Go, protobuf/buf, ConnectRPC, PostgreSQL, testify, Ginkgo/Gomega, Playwright

**Spec:** `docs/superpowers/specs/2026-04-15-query-stream-history-rpc-design.md`

---

## File Map

| File | Action | Responsibility |
| --- | --- | --- |
| `api/proto/holomush/core/v1/core.proto` | Modify | Add RPC + request/response messages |
| `api/proto/holomush/web/v1/web.proto` | Modify | Add web RPC + request/response messages |
| `internal/core/store.go` | Modify | Add `beforeID` param to `ReplayTail` |
| `internal/core/store_memory.go` | Modify | Implement `beforeID` filter |
| `internal/core/store_memory_test.go` | Modify | Tests for `beforeID` |
| `internal/store/postgres.go` | Modify | SQL for `beforeID` filter |
| `internal/store/postgres_test.go` | Modify | Unit tests for `beforeID` |
| `internal/store/postgres_integration_test.go` | Modify | Integration tests for `beforeID` |
| `internal/core/mocks/mock_EventStore.go` | Regenerate | mockery |
| `internal/core/event_writer.go` | Modify | Update `ReplayTail` delegation |
| `internal/core/event_writer_test.go` | Modify | Update test calls |
| `internal/grpc/replay.go` | Modify | Update `ReplayTail` call |
| `internal/plugin/goplugin/host_service.go` | Modify | Update `ReplayTail` call |
| `internal/plugin/goplugin/host_service_test.go` | Modify | Update test calls |
| `internal/plugin/hostfunc/stdlib_focus.go` | Modify | Update `HistoryReader` interface + call |
| `internal/plugin/hostfunc/stdlib_focus_test.go` | Modify | Update mock + test calls |
| `internal/grpc/stream_access.go` | Create | `isPrivateStream`, `sessionHasMembership`, `streamToFocusKey` helpers |
| `internal/grpc/stream_access_test.go` | Create | Tests for stream access helpers |
| `internal/grpc/query_stream_history.go` | Create | Handler implementation |
| `internal/grpc/query_stream_history_test.go` | Create | Unit + invariant + boundary tests |
| `internal/access/policy/seed.go` | Modify | Add `seed:player-location-stream-read` |
| `internal/access/policy/seed_test.go` | Modify | Update count + policy assertions |
| `internal/access/policy/seed_smoke_test.go` | Modify | Stream-read smoke tests |
| `internal/web/handler.go` | Modify | Add `CoreClient` method + proxy handler |
| `internal/web/handler_test.go` | Modify | Proxy tests |
| `test/integration/stream_history/` | Create | Integration test suite |
| `web/e2e/terminal.spec.ts` | Modify | E2E test |

---

## Task 1: Extend ReplayTail Interface with beforeID

**Files:**

- Modify: `internal/core/store.go:29-34`
- Modify: `internal/core/store_memory.go:101-140`
- Modify: `internal/core/store_memory_test.go`

- [ ] **Step 1: Write failing tests for beforeID in MemoryEventStore**

Add to `internal/core/store_memory_test.go` after the existing `ReplayTail` tests (line ~333):

```go
func TestReplayTailWithBeforeIDExcludesNewerEvents(t *testing.T) {
	store := NewMemoryEventStore()
	ctx := context.Background()
	stream := "location:before-id-test"

	// Append 5 events.
	var ids []ulid.ULID
	for i := 0; i < 5; i++ {
		e := NewEvent(stream, EventType("test"), core.CharacterRef{})
		require.NoError(t, store.Append(ctx, e))
		ids = append(ids, e.ID)
	}

	// Request events before the 4th event (index 3) — should get first 3.
	events, err := store.ReplayTail(ctx, stream, 10, time.Time{}, ids[3])
	require.NoError(t, err)
	assert.Len(t, events, 3)
	assert.Equal(t, ids[0], events[0].ID)
	assert.Equal(t, ids[2], events[2].ID)
}

func TestReplayTailWithZeroBeforeIDReturnsAll(t *testing.T) {
	store := NewMemoryEventStore()
	ctx := context.Background()
	stream := "location:zero-before"

	for i := 0; i < 3; i++ {
		e := NewEvent(stream, EventType("test"), core.CharacterRef{})
		require.NoError(t, store.Append(ctx, e))
	}

	events, err := store.ReplayTail(ctx, stream, 10, time.Time{}, ulid.ULID{})
	require.NoError(t, err)
	assert.Len(t, events, 3)
}

func TestReplayTailWithBeforeIDAndNotBefore(t *testing.T) {
	store := NewMemoryEventStore()
	ctx := context.Background()
	stream := "location:both-filters"

	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var ids []ulid.ULID
	for i := 0; i < 5; i++ {
		e := NewEvent(stream, EventType("test"), core.CharacterRef{})
		e.Timestamp = baseTime.Add(time.Duration(i) * time.Minute)
		require.NoError(t, store.Append(ctx, e))
		ids = append(ids, e.ID)
	}

	// notBefore = 2 min in (excludes first 2), beforeID = ids[4] (excludes last 1).
	// Should get events at index 2, 3.
	events, err := store.ReplayTail(ctx, stream, 10, baseTime.Add(2*time.Minute), ids[4])
	require.NoError(t, err)
	assert.Len(t, events, 2)
	assert.Equal(t, ids[2], events[0].ID)
	assert.Equal(t, ids[3], events[1].ID)
}

func TestReplayTailBeforeIDAtBoundaryExcludesExact(t *testing.T) {
	store := NewMemoryEventStore()
	ctx := context.Background()
	stream := "location:boundary"

	var ids []ulid.ULID
	for i := 0; i < 3; i++ {
		e := NewEvent(stream, EventType("test"), core.CharacterRef{})
		require.NoError(t, store.Append(ctx, e))
		ids = append(ids, e.ID)
	}

	// beforeID == ids[1] — should exclude ids[1] and ids[2], return only ids[0].
	events, err := store.ReplayTail(ctx, stream, 10, time.Time{}, ids[1])
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, ids[0], events[0].ID)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestReplayTailWith ./internal/core/`
Expected: FAIL — `ReplayTail` signature mismatch (too many arguments).

- [ ] **Step 3: Update the EventStore interface**

In `internal/core/store.go`, change the `ReplayTail` signature at line 34:

```go
ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time, beforeID ulid.ULID) ([]Event, error)
```

Update the doc comment to mention `beforeID`:

```go
// ReplayTail returns up to count most recent events on stream,
// ordered ascending by event ID. If notBefore is non-zero, events
// with timestamps before it are excluded. If beforeID is non-zero,
// events with id >= beforeID are excluded. Count is capped server-side
// at 500. Used by FocusKindPolicy implementations for bounded-tail
// reads and by QueryStreamHistory for client scrollback.
//
// The default page size of 150 is applied in the handler, not here.
// This method treats count literally.
ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time, beforeID ulid.ULID) ([]Event, error)
```

- [ ] **Step 4: Update MemoryEventStore implementation**

Replace the `ReplayTail` method in `internal/core/store_memory.go` (lines 101-140):

```go
func (s *MemoryEventStore) ReplayTail(_ context.Context, stream string, count int, notBefore time.Time, beforeID ulid.ULID) ([]Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if count > maxReplayTailCount {
		count = maxReplayTailCount
	}
	if count <= 0 {
		return nil, nil
	}

	events := s.streams[stream]
	if len(events) == 0 {
		return nil, nil
	}

	// Filter from the end: beforeID excludes events with id >= beforeID,
	// notBefore excludes events with timestamp before notBefore.
	zeroID := ulid.ULID{}
	hasBeforeID := beforeID != zeroID
	hasNotBefore := !notBefore.IsZero()

	var eligible []Event
	for i := len(events) - 1; i >= 0 && len(eligible) < count; i-- {
		e := events[i]
		if hasBeforeID && e.ID.Compare(beforeID) >= 0 {
			continue
		}
		if hasNotBefore && e.Timestamp.Before(notBefore) {
			continue
		}
		eligible = append(eligible, e)
	}

	// Reverse to ascending order.
	for i, j := 0, len(eligible)-1; i < j; i, j = i+1, j-1 {
		eligible[i], eligible[j] = eligible[j], eligible[i]
	}
	return eligible, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `task test -- -run TestReplayTail ./internal/core/`
Expected: FAIL — compilation errors in other files that call `ReplayTail` with old signature.

- [ ] **Step 6: Fix all existing callers (add zero beforeID)**

Update each caller to pass `ulid.ULID{}` as the new last argument:

`internal/core/event_writer.go:138`:

```go
events, err := w.store.ReplayTail(ctx, stream, count, notBefore, ulid.ULID{})
```

`internal/grpc/replay.go:93`:

```go
events, err := s.eventStore.ReplayTail(ctx, sm.Stream, sm.TailCount, sm.NotBefore, ulid.ULID{})
```

`internal/plugin/goplugin/host_service.go:192`:

```go
events, err := es.ReplayTail(ctx, req.GetStream(), count, notBefore, ulid.ULID{})
```

`internal/plugin/hostfunc/stdlib_focus.go` — update the `HistoryReader` interface (line 32-34):

```go
type HistoryReader interface {
	ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time, beforeID ulid.ULID) ([]core.Event, error)
}
```

And the call site (line 238):

```go
events, err := hr.ReplayTail(ctx, stream, count, notBefore, ulid.ULID{})
```

- [ ] **Step 7: Fix test files**

Update all test files that call `ReplayTail` to pass the extra `ulid.ULID{}` argument. Files:

- `internal/core/event_writer_test.go` (lines 269, 280)
- `internal/store/postgres_test.go` (lines 762, 783)
- `internal/plugin/goplugin/host_service_test.go` (all `QueryStreamHistory` tests)
- `internal/plugin/hostfunc/stdlib_focus_test.go` (mock `HistoryReader` implementation)

For `stdlib_focus_test.go`, update the mock struct's `ReplayTail` method signature:

```go
func (m *mockHistoryReader) ReplayTail(_ context.Context, stream string, count int, notBefore time.Time, _ ulid.ULID) ([]core.Event, error) {
```

- [ ] **Step 8: Regenerate mocks**

Run: `mockery`

This regenerates `internal/core/mocks/mock_EventStore.go` with the updated `ReplayTail` signature.

- [ ] **Step 9: Run full test suite**

Run: `task test`
Expected: PASS — all existing tests pass with the new signature.

- [ ] **Step 10: Commit**

```text
jj --no-pager commit -m "feat(core): add beforeID parameter to ReplayTail for cursor pagination

Extends EventStore.ReplayTail with a beforeID ulid.ULID parameter.
When non-zero, events with id >= beforeID are excluded. All existing
callers pass zero (no behavior change). Enables ULID-based cursor
pagination for QueryStreamHistory (B9).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Update PostgresEventStore.ReplayTail

**Files:**

- Modify: `internal/store/postgres.go:245-300`
- Modify: `internal/store/postgres_test.go`
- Modify: `internal/store/postgres_integration_test.go`

- [ ] **Step 1: Write failing unit tests**

Add to `internal/store/postgres_test.go` after the existing `ReplayTail` tests:

```go
func TestReplayTailWithBeforeIDExcludesNewerEvents(t *testing.T) {
	pool := setupTestPool(t)
	store := NewPostgresEventStore(pool)
	ctx := context.Background()

	stream := "location:pg-before-id"
	var ids []ulid.ULID
	for i := 0; i < 5; i++ {
		e := core.NewEvent(stream, core.EventType("test"), core.CharacterRef{})
		require.NoError(t, store.Append(ctx, e))
		ids = append(ids, e.ID)
	}

	events, err := store.ReplayTail(ctx, stream, 10, time.Time{}, ids[3])
	require.NoError(t, err)
	assert.Len(t, events, 3)
	assert.Equal(t, ids[2], events[2].ID)
}

func TestReplayTailWithZeroBeforeIDReturnsAllPG(t *testing.T) {
	pool := setupTestPool(t)
	store := NewPostgresEventStore(pool)
	ctx := context.Background()

	stream := "location:pg-zero-before"
	for i := 0; i < 3; i++ {
		e := core.NewEvent(stream, core.EventType("test"), core.CharacterRef{})
		require.NoError(t, store.Append(ctx, e))
	}

	events, err := store.ReplayTail(ctx, stream, 10, time.Time{}, ulid.ULID{})
	require.NoError(t, err)
	assert.Len(t, events, 3)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestReplayTailWith ./internal/store/`
Expected: FAIL — signature mismatch.

- [ ] **Step 3: Update PostgresEventStore.ReplayTail**

Replace the implementation in `internal/store/postgres.go` (lines 245-300):

```go
func (s *PostgresEventStore) ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time, beforeID ulid.ULID) ([]core.Event, error) {
	if count > maxReplayTailCount {
		count = maxReplayTailCount
	}
	if count <= 0 {
		return nil, nil
	}

	zeroID := ulid.ULID{}
	hasNotBefore := !notBefore.IsZero()
	hasBeforeID := beforeID != zeroID

	var rows pgx.Rows
	var err error

	switch {
	case !hasNotBefore && !hasBeforeID:
		rows, err = s.pool.Query(ctx,
			`SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			 FROM (
			     SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			     FROM events WHERE stream = $1
			     ORDER BY id DESC LIMIT $2
			 ) sub ORDER BY id ASC`,
			stream, count)
	case hasNotBefore && !hasBeforeID:
		rows, err = s.pool.Query(ctx,
			`SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			 FROM (
			     SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			     FROM events WHERE stream = $1 AND created_at >= $2
			     ORDER BY id DESC LIMIT $3
			 ) sub ORDER BY id ASC`,
			stream, notBefore, count)
	case !hasNotBefore && hasBeforeID:
		rows, err = s.pool.Query(ctx,
			`SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			 FROM (
			     SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			     FROM events WHERE stream = $1 AND id < $2
			     ORDER BY id DESC LIMIT $3
			 ) sub ORDER BY id ASC`,
			stream, beforeID.String(), count)
	case hasNotBefore && hasBeforeID:
		rows, err = s.pool.Query(ctx,
			`SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			 FROM (
			     SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			     FROM events WHERE stream = $1 AND created_at >= $2 AND id < $3
			     ORDER BY id DESC LIMIT $4
			 ) sub ORDER BY id ASC`,
			stream, notBefore, beforeID.String(), count)
	}
	if err != nil {
		return nil, oops.With("operation", "replay tail").With("stream", stream).Wrap(err)
	}
	defer rows.Close()
```

The row scanning code below (lines 280+) remains unchanged.

- [ ] **Step 4: Fix existing postgres_test.go callers**

Update the two existing `ReplayTail` test calls (lines 762, 783) to pass `ulid.ULID{}`:

```go
events, err := store.ReplayTail(context.Background(), "location:test", 5, time.Time{}, ulid.ULID{})
```

```go
events, err := store.ReplayTail(context.Background(), "location:test", 5, notBefore, ulid.ULID{})
```

- [ ] **Step 5: Run unit tests**

Run: `task test -- -run TestReplayTail ./internal/store/`
Expected: PASS.

- [ ] **Step 6: Add integration tests**

Add to `internal/store/postgres_integration_test.go` inside the existing `Describe("ReplayTail")` block (after line ~340):

```go
It("returns only events before beforeID", func() {
	stream := "location:int-before-id-" + ulid.Make().String()
	var ids []ulid.ULID
	for i := 0; i < 5; i++ {
		e := core.NewEvent(stream, core.EventType("test"), core.CharacterRef{})
		Expect(eventStore.Append(ctx, e)).To(Succeed())
		ids = append(ids, e.ID)
	}

	events, err := eventStore.ReplayTail(ctx, stream, 10, time.Time{}, ids[3])
	Expect(err).NotTo(HaveOccurred())
	Expect(events).To(HaveLen(3))
	Expect(events[2].ID).To(Equal(ids[2]))
})

It("ignores zero beforeID", func() {
	stream := "location:int-zero-before-" + ulid.Make().String()
	for i := 0; i < 3; i++ {
		e := core.NewEvent(stream, core.EventType("test"), core.CharacterRef{})
		Expect(eventStore.Append(ctx, e)).To(Succeed())
	}

	events, err := eventStore.ReplayTail(ctx, stream, 10, time.Time{}, ulid.ULID{})
	Expect(err).NotTo(HaveOccurred())
	Expect(events).To(HaveLen(3))
})
```

Also update the existing three integration test calls (lines 301, 331, 340) to pass `ulid.ULID{}`.

- [ ] **Step 7: Run integration tests**

Run: `task test:int -- -run ReplayTail ./internal/store/`
Expected: PASS.

- [ ] **Step 8: Commit**

```text
jj --no-pager commit -m "feat(store): implement beforeID filter in PostgresEventStore.ReplayTail

Adds SQL id < $beforeID clause for ULID-based cursor pagination.
Four query variants cover all combinations of notBefore and beforeID.
Integration tests verify correct DB-level filtering.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Proto Definitions

**Files:**

- Modify: `api/proto/holomush/core/v1/core.proto:66-67`
- Modify: `api/proto/holomush/web/v1/web.proto:61`

- [ ] **Step 1: Add QueryStreamHistory to CoreService proto**

In `api/proto/holomush/core/v1/core.proto`, add after the `CheckPlayerSession` RPC (line 67):

```protobuf
  // QueryStreamHistory reads paginated event history from a stream.
  // Two-layer authorization: membership gate (I-17) for private streams,
  // ABAC policy evaluation for public streams.
  // Pure read — does not mutate session cursors (invariant I-13).
  rpc QueryStreamHistory(QueryStreamHistoryRequest) returns (QueryStreamHistoryResponse);
```

Add the messages after the existing messages (before closing of file):

```protobuf
message QueryStreamHistoryRequest {
  RequestMeta meta = 1;
  string session_id = 2;
  string stream = 3;
  int32 count = 4;          // page size; 0 = default (150), max 500, negative rejected
  int64 not_before_ms = 5;  // epoch ms time floor; 0 = no lower bound
  string before_id = 6;     // ULID pagination cursor; events older than this; empty = latest
}

message QueryStreamHistoryResponse {
  ResponseMeta meta = 1;
  repeated EventFrame events = 2;
  bool has_more = 3;
}
```

**Note:** Uses `EventFrame` (the existing event message at line 92), not a new type.

- [ ] **Step 2: Add WebQueryStreamHistory to WebService proto**

In `api/proto/holomush/web/v1/web.proto`, add after `WebListContent` (line 61):

```protobuf
  // WebQueryStreamHistory reads paginated event history for the web client.
  rpc WebQueryStreamHistory(WebQueryStreamHistoryRequest) returns (WebQueryStreamHistoryResponse);
```

Add messages:

```protobuf
message WebQueryStreamHistoryRequest {
  string session_id = 1;
  string stream = 2;
  int32 count = 3;
  int64 not_before_ms = 4;
  string before_id = 5;
}

message WebQueryStreamHistoryResponse {
  repeated GameEvent events = 1;
  bool has_more = 2;
}
```

- [ ] **Step 3: Generate Go code**

Run: `task proto`
Expected: BUILD SUCCESS. Generated files update in `pkg/proto/`.

- [ ] **Step 4: Verify compilation**

Run: `task build`
Expected: FAIL — `UnimplementedCoreServiceServer` now includes a `QueryStreamHistory` stub that CoreServer doesn't implement yet. This is expected and will be fixed in Task 5.

- [ ] **Step 5: Commit**

```text
jj --no-pager commit -m "feat(proto): add QueryStreamHistory RPC to CoreService and WebService

Adds QueryStreamHistoryRequest/Response with ULID cursor pagination
(before_id), time floor (not_before_ms), and has_more pagination signal.
Web variant uses GameEvent for client rendering.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Stream Access Helpers and Seed Policy

**Files:**

- Create: `internal/grpc/stream_access.go`
- Create: `internal/grpc/stream_access_test.go`
- Modify: `internal/access/policy/seed.go`
- Modify: `internal/access/policy/seed_test.go`
- Modify: `internal/access/policy/seed_smoke_test.go`

- [ ] **Step 1: Write failing tests for stream access helpers**

Create `internal/grpc/stream_access_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/session"
)

func TestIsPrivateStreamReturnsTrueForSceneStreams(t *testing.T) {
	assert.True(t, isPrivateStream("scene:01ABC:ic"))
	assert.True(t, isPrivateStream("scene:01ABC:ooc"))
}

func TestIsPrivateStreamReturnsTrueForCharacterStreams(t *testing.T) {
	assert.True(t, isPrivateStream("character:01ABC"))
}

func TestIsPrivateStreamReturnsFalseForLocationStreams(t *testing.T) {
	assert.False(t, isPrivateStream("location:01ABC"))
}

func TestIsPrivateStreamReturnsFalseForUnknownStreams(t *testing.T) {
	assert.False(t, isPrivateStream("global"))
	assert.False(t, isPrivateStream(""))
}

func TestSessionHasMembershipPermitsOwnCharacterStream(t *testing.T) {
	charID := ulid.Make()
	info := &session.Info{CharacterID: charID}
	assert.True(t, sessionHasMembership(info, "character:"+charID.String()))
}

func TestSessionHasMembershipDeniesOtherCharacterStream(t *testing.T) {
	info := &session.Info{CharacterID: ulid.Make()}
	assert.False(t, sessionHasMembership(info, "character:"+ulid.Make().String()))
}

func TestSessionHasMembershipPermitsSceneStreamWithFocusMembership(t *testing.T) {
	targetID := ulid.Make()
	info := &session.Info{
		FocusMemberships: []session.FocusMembership{
			{Kind: session.FocusKindScene, TargetID: targetID},
		},
	}
	assert.True(t, sessionHasMembership(info, "scene:"+targetID.String()+":ic"))
	assert.True(t, sessionHasMembership(info, "scene:"+targetID.String()+":ooc"))
}

func TestSessionHasMembershipDeniesSceneStreamWithoutMembership(t *testing.T) {
	info := &session.Info{
		FocusMemberships: []session.FocusMembership{
			{Kind: session.FocusKindScene, TargetID: ulid.Make()},
		},
	}
	assert.False(t, sessionHasMembership(info, "scene:"+ulid.Make().String()+":ic"))
}

func TestSessionHasMembershipDeniesSceneStreamWithEmptyMemberships(t *testing.T) {
	info := &session.Info{}
	assert.False(t, sessionHasMembership(info, "scene:"+ulid.Make().String()+":ic"))
}

func TestStreamToFocusKeyParsesSceneIC(t *testing.T) {
	id := ulid.Make()
	fk, err := streamToFocusKey("scene:" + id.String() + ":ic")
	assert.NoError(t, err)
	assert.Equal(t, session.FocusKindScene, fk.Kind)
	assert.Equal(t, id, fk.TargetID)
}

func TestStreamToFocusKeyParsesSceneOOC(t *testing.T) {
	id := ulid.Make()
	fk, err := streamToFocusKey("scene:" + id.String() + ":ooc")
	assert.NoError(t, err)
	assert.Equal(t, session.FocusKindScene, fk.Kind)
	assert.Equal(t, id, fk.TargetID)
}

func TestStreamToFocusKeyRejectsNonSceneStream(t *testing.T) {
	_, err := streamToFocusKey("location:01ABC")
	assert.Error(t, err)
}

func TestStreamToFocusKeyRejectsMalformedULID(t *testing.T) {
	_, err := streamToFocusKey("scene:not-a-ulid:ic")
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestIsPrivateStream ./internal/grpc/`
Expected: FAIL — functions not defined.

- [ ] **Step 3: Implement stream access helpers**

Create `internal/grpc/stream_access.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/session"
)

// isPrivateStream returns true if the stream requires membership to read (I-17).
// Private streams: character:<id>, scene:<id>:ic, scene:<id>:ooc.
// Public streams: location:<id>, and anything else.
func isPrivateStream(stream string) bool {
	return strings.HasPrefix(stream, "character:") || strings.HasPrefix(stream, "scene:")
}

// sessionHasMembership checks if the session has membership for a private stream.
// For character streams: the session's character ID must match.
// For scene streams: the session must have a FocusMembership whose derived
// streams include the requested stream.
func sessionHasMembership(info *session.Info, stream string) bool {
	if strings.HasPrefix(stream, "character:") {
		charID := strings.TrimPrefix(stream, "character:")
		return info.CharacterID.String() == charID
	}

	if strings.HasPrefix(stream, "scene:") {
		fk, err := streamToFocusKey(stream)
		if err != nil {
			return false
		}
		for _, fm := range info.FocusMemberships {
			if fm.Kind == fk.Kind && fm.TargetID == fk.TargetID {
				return true
			}
		}
	}

	return false
}

// streamToFocusKey parses a scene stream name into a FocusKey.
// Expects format "scene:<ulid>:ic" or "scene:<ulid>:ooc".
func streamToFocusKey(stream string) (*session.FocusKey, error) {
	if !strings.HasPrefix(stream, "scene:") {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("not a scene stream: %s", stream)
	}

	// "scene:<ulid>:ic" → parts = ["scene", "<ulid>", "ic"]
	parts := strings.SplitN(stream, ":", 3)
	if len(parts) < 3 {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("malformed scene stream: %s", stream)
	}

	targetID, err := ulid.Parse(parts[1])
	if err != nil {
		return nil, oops.Code("INVALID_ARGUMENT").With("stream", stream).Wrap(err)
	}

	return &session.FocusKey{
		Kind:     session.FocusKindScene,
		TargetID: targetID,
	}, nil
}
```

- [ ] **Step 4: Run stream access tests**

Run: `task test -- -run "TestIsPrivateStream|TestSessionHasMembership|TestStreamToFocusKey" ./internal/grpc/`
Expected: PASS.

- [ ] **Step 5: Add seed policy**

In `internal/access/policy/seed.go`, add a new entry to the `SeedPolicies()` slice:

```go
{
	Name:        "seed:player-location-stream-read",
	Description: "Characters can read history of their current location stream",
	DSLText:     `permit(principal is character, action in ["read"], resource is stream) when { resource.stream.name like "location:*" && resource.stream.location == principal.character.location };`,
	SeedVersion: 1,
},
```

- [ ] **Step 6: Update seed_test.go count**

In `internal/access/policy/seed_test.go`, update `TestSeedPoliciesCount`:

```go
assert.Len(t, seeds, 26, "expected 26 seed policies (25 permit, 1 forbid)")
```

And add to `TestSeedPoliciesExpectedNames` the new name `"seed:player-location-stream-read"`.

Update `TestSeedPoliciesEffectDistribution` to expect 25 permit, 1 forbid.

- [ ] **Step 7: Add seed smoke test for stream read**

In `internal/access/policy/seed_smoke_test.go`, add:

```go
func TestSeedSmokePlayerCanReadCoLocatedLocationStream(t *testing.T) {
	engine := createSeedEngine(t)
	req, err := types.NewAccessRequest("character:01ABC", "read", "stream:location:01XYZ")
	require.NoError(t, err)
	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, decision.Allowed(), "co-located character should read location stream")
}

func TestSeedSmokePlayerCannotReadNonCoLocatedLocationStream(t *testing.T) {
	engine := createSeedEngine(t)
	req, err := types.NewAccessRequest("character:01ABC", "read", "stream:location:01OTHER")
	require.NoError(t, err)
	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, decision.Allowed(), "non-co-located character should not read location stream")
}
```

Note: these smoke tests depend on the seed engine's attribute providers resolving `character:01ABC` with `location=01XYZ`. Check the existing smoke test setup to match the character/location IDs used.

- [ ] **Step 8: Run seed tests**

Run: `task test -- -run TestSeedPolicies ./internal/access/policy/`
Expected: PASS.

- [ ] **Step 9: Commit**

```text
jj --no-pager commit -m "feat(access): stream access helpers + seed:player-location-stream-read policy

Adds isPrivateStream, sessionHasMembership, streamToFocusKey helpers for
two-layer authorization (I-17). Adds seed policy for co-located location
stream reads, mirroring seed:player-stream-emit.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: QueryStreamHistory Handler

**Files:**

- Create: `internal/grpc/query_stream_history.go`
- Create: `internal/grpc/query_stream_history_test.go`
- Modify: `internal/grpc/server.go` (add ABAC engine field + option)

- [ ] **Step 1: Add ABAC engine field to CoreServer**

In `internal/grpc/server.go`, add to the `CoreServer` struct (after `focusCoordinator` at line ~131):

```go
// accessEngine evaluates ABAC policies for stream read authorization.
// Nil if ABAC is not configured (all public stream reads denied).
accessEngine types.AccessPolicyEngine
```

Add the import for `types`:

```go
accessTypes "github.com/holomush/holomush/internal/access/policy/types"
```

Add the option function (after `WithFocusCoordinator`):

```go
// WithAccessEngine sets the ABAC policy engine for stream access authorization.
func WithAccessEngine(engine accessTypes.AccessPolicyEngine) CoreServerOption {
	return func(s *CoreServer) { s.accessEngine = engine }
}
```

- [ ] **Step 2: Write the handler tests**

Create `internal/grpc/query_stream_history_test.go`. This file is large — it contains unit tests, invariant tests (I-17), and boundary tests. The full content is provided in each sub-step below.

Start with the basic happy path and validation tests:

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

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/core"
	coreMocks "github.com/holomush/holomush/internal/core/mocks"
	"github.com/holomush/holomush/internal/session"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// newTestQueryServer creates a CoreServer with mocked deps for QueryStreamHistory tests.
func newTestQueryServer(t *testing.T, opts ...CoreServerOption) *CoreServer {
	t.Helper()
	engine, err := core.NewEngine(core.EngineConfig{})
	require.NoError(t, err)
	return NewCoreServer(engine, nil, nil, nil, opts...)
}

func TestQueryStreamHistoryRejectsEmptySessionID(t *testing.T) {
	srv := newTestQueryServer(t)
	resp, err := srv.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "",
		Stream:    "location:01ABC",
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	// Check for error via oops code or response structure per project patterns.
}
```

(Continue with all tests from spec §8.1-8.3. Each test follows the pattern above — create server with appropriate mocked deps, call `QueryStreamHistory`, assert response.)

Due to plan length constraints, the full test file content is specified in the spec's test tables. The implementer MUST write all tests listed in §8.1 (unit), §8.2 (invariant), and §8.3 (boundary) before implementing the handler.

- [ ] **Step 3: Run tests to verify they fail**

Run: `task test -- -run TestQueryStreamHistory ./internal/grpc/`
Expected: FAIL — `QueryStreamHistory` method not defined.

- [ ] **Step 4: Implement the handler**

Create `internal/grpc/query_stream_history.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/types/known/timestamppb"

	accessTypes "github.com/holomush/holomush/internal/access/policy/types"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

const (
	defaultHistoryPageSize = 150
	maxHistoryPageSize     = 500
)

func (s *CoreServer) QueryStreamHistory(ctx context.Context, req *corev1.QueryStreamHistoryRequest) (*corev1.QueryStreamHistoryResponse, error) {
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}

	slog.DebugContext(ctx, "query stream history",
		"request_id", requestID,
		"session_id", req.SessionId,
		"stream", req.Stream,
	)

	// Step 0: Guard — eventStore must be configured.
	if s.eventStore == nil {
		return nil, oops.Code("INTERNAL").Errorf("event store not configured")
	}

	// Step 1: Validate session.
	if req.SessionId == "" {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("session_id is required")
	}
	info, err := s.sessionStore.Get(ctx, req.SessionId)
	if err != nil {
		if oopsErr, ok := oops.AsOops(err); ok && oopsErr.Code() == "SESSION_NOT_FOUND" {
			return nil, oops.Code("SESSION_NOT_FOUND").Errorf("session not found")
		}
		return nil, oops.Code("INTERNAL").Wrap(err)
	}
	if info.Status == "expired" || info.IsExpired() {
		return nil, oops.Code("SESSION_EXPIRED").Errorf("session expired")
	}

	// Step 2: Validate stream.
	if req.Stream == "" {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("stream is required")
	}

	// Step 3: Normalize count.
	count := int(req.Count)
	if count < 0 {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("count must be non-negative")
	}
	if count == 0 {
		count = defaultHistoryPageSize
	}
	if count > maxHistoryPageSize {
		count = maxHistoryPageSize
	}

	// Step 4: Parse before_id.
	var beforeID ulid.ULID
	if req.BeforeId != "" {
		parsed, parseErr := ulid.Parse(req.BeforeId)
		if parseErr != nil {
			return nil, oops.Code("INVALID_ARGUMENT").Errorf("before_id must be a valid ULID")
		}
		beforeID = parsed
	}

	// Step 5: Authorization — two-layer model.
	if isPrivateStream(req.Stream) {
		// Layer 1: Membership gate (I-17). No policy override.
		if !sessionHasMembership(info, req.Stream) {
			return nil, oops.Code("STREAM_ACCESS_DENIED").
				With("session_id", req.SessionId).
				With("stream", req.Stream).
				Errorf("not authorized to read stream")
		}
	} else {
		// Layer 2: ABAC policy for public streams.
		if s.accessEngine == nil {
			return nil, oops.Code("STREAM_ACCESS_DENIED").Errorf("access engine not configured")
		}
		accessReq, reqErr := accessTypes.NewAccessRequest(
			"character:"+info.CharacterID.String(),
			"read",
			"stream:"+req.Stream,
		)
		if reqErr != nil {
			return nil, oops.Code("INTERNAL").Wrap(reqErr)
		}
		decision, evalErr := s.accessEngine.Evaluate(ctx, accessReq)
		if evalErr != nil {
			return nil, oops.Code("INTERNAL").Wrap(evalErr)
		}
		if !decision.Allowed() {
			return nil, oops.Code("STREAM_ACCESS_DENIED").
				With("session_id", req.SessionId).
				With("stream", req.Stream).
				Errorf("not authorized to read stream")
		}
	}

	// Step 6: Parse not_before.
	var notBefore time.Time
	if req.NotBeforeMs > 0 {
		notBefore = time.UnixMilli(req.NotBeforeMs).UTC()
	}

	// Step 7: Fetch count+1 for has_more.
	events, fetchErr := s.eventStore.ReplayTail(ctx, req.Stream, count+1, notBefore, beforeID)
	if fetchErr != nil {
		return nil, oops.Code("INTERNAL").
			With("stream", req.Stream).
			Wrap(fetchErr)
	}

	// Step 8: Build response.
	hasMore := len(events) > count
	if hasMore {
		events = events[:count]
	}

	protoEvents := make([]*corev1.EventFrame, 0, len(events))
	for _, e := range events {
		protoEvents = append(protoEvents, coreEventToEventFrame(e))
	}

	return &corev1.QueryStreamHistoryResponse{
		Meta:    responseMeta(requestID),
		Events:  protoEvents,
		HasMore: hasMore,
	}, nil
}

// coreEventToEventFrame converts a core.Event to a proto EventFrame.
func coreEventToEventFrame(e core.Event) *corev1.EventFrame {
	return &corev1.EventFrame{
		Id:        e.ID.String(),
		Stream:    e.Stream,
		Type:      string(e.Type),
		Timestamp: timestamppb.New(e.Timestamp),
		ActorType: string(e.Actor.Kind),
		ActorId:   e.Actor.ID,
		Payload:   e.Payload,
	}
}
```

**Note:** The `coreEventToEventFrame` function may already exist (check `server.go` for a similar conversion in the `Subscribe` handler). If so, reuse it. If the existing converter has a different name, use that name instead.

- [ ] **Step 5: Run tests**

Run: `task test -- -run TestQueryStreamHistory ./internal/grpc/`
Expected: PASS for all unit, invariant, and boundary tests.

- [ ] **Step 6: Run full test suite**

Run: `task test`
Expected: PASS — no regressions.

- [ ] **Step 7: Commit**

```text
jj --no-pager commit -m "feat(grpc): QueryStreamHistory handler with two-layer auth (I-17 + ABAC)

Implements CoreService.QueryStreamHistory with:
- Membership gate for private streams (I-17: scene, character)
- ABAC Evaluate() for public streams (location)
- ULID cursor pagination via before_id
- Default 150 / max 500 page size
- has_more via count+1 fetch
- No cursor mutation (I-13)

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Web Gateway Proxy

**Files:**

- Modify: `internal/web/handler.go`
- Modify: `internal/web/handler_test.go`

- [ ] **Step 1: Add QueryStreamHistory to CoreClient interface**

In `internal/web/handler.go`, add to the `CoreClient` interface (after `CreateGuest` at line 51):

```go
QueryStreamHistory(ctx context.Context, req *corev1.QueryStreamHistoryRequest) (*corev1.QueryStreamHistoryResponse, error)
```

- [ ] **Step 2: Write failing proxy test**

In `internal/web/handler_test.go`, add:

```go
func TestWebQueryStreamHistoryProxiesToCoreService(t *testing.T) {
	// Setup mock CoreClient that returns 2 events with has_more=true.
	// Call handler.WebQueryStreamHistory with a WebQueryStreamHistoryRequest.
	// Assert response contains 2 GameEvents and has_more=true.
}

func TestWebQueryStreamHistoryPropagatesError(t *testing.T) {
	// Setup mock CoreClient that returns an error.
	// Assert error is propagated.
}
```

The implementer should follow the pattern of existing proxy tests in this file (e.g., `TestSendCommand`).

- [ ] **Step 3: Run tests to verify they fail**

Run: `task test -- -run TestWebQueryStreamHistory ./internal/web/`
Expected: FAIL — method not defined.

- [ ] **Step 4: Implement the proxy handler**

In `internal/web/handler.go`, add:

```go
func (h *Handler) WebQueryStreamHistory(ctx context.Context, req *connect.Request[webv1.WebQueryStreamHistoryRequest]) (*connect.Response[webv1.WebQueryStreamHistoryResponse], error) {
	slog.DebugContext(ctx, "web: WebQueryStreamHistory",
		"session_id", req.Msg.GetSessionId(),
		"stream", req.Msg.GetStream(),
	)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.client.QueryStreamHistory(rpcCtx, &corev1.QueryStreamHistoryRequest{
		SessionId:   req.Msg.GetSessionId(),
		Stream:      req.Msg.GetStream(),
		Count:       req.Msg.GetCount(),
		NotBeforeMs: req.Msg.GetNotBeforeMs(),
		BeforeId:    req.Msg.GetBeforeId(),
	})
	if err != nil {
		slog.Error("web: query stream history RPC failed",
			"session_id", req.Msg.GetSessionId(), "error", err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	gameEvents := make([]*webv1.GameEvent, 0, len(resp.GetEvents()))
	for _, ef := range resp.GetEvents() {
		gameEvents = append(gameEvents, eventFrameToGameEvent(ef))
	}

	return connect.NewResponse(&webv1.WebQueryStreamHistoryResponse{
		Events:  gameEvents,
		HasMore: resp.GetHasMore(),
	}), nil
}
```

The `eventFrameToGameEvent` function should already exist in `handler.go` (used by `StreamEvents`). If not, create a minimal conversion:

```go
func eventFrameToGameEvent(ef *corev1.EventFrame) *webv1.GameEvent {
	return &webv1.GameEvent{
		Type:      ef.GetType(),
		Timestamp: ef.GetTimestamp().AsTime().UnixMilli(),
		Actor:     ef.GetActorId(),
		Text:      string(ef.GetPayload()),
	}
}
```

Check the existing `StreamEvents` handler for the actual conversion function name and use that.

- [ ] **Step 5: Run tests**

Run: `task test -- -run TestWebQueryStreamHistory ./internal/web/`
Expected: PASS.

- [ ] **Step 6: Commit**

```text
jj --no-pager commit -m "feat(web): WebQueryStreamHistory gateway proxy

Thin proxy from WebService to CoreService.QueryStreamHistory.
Maps EventFrame responses to GameEvent for web client rendering.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Integration Tests

**Files:**

- Create: `test/integration/stream_history/stream_history_suite_test.go`
- Create: `test/integration/stream_history/query_stream_history_test.go`

- [ ] **Step 1: Create test suite**

Create `test/integration/stream_history/stream_history_suite_test.go`:

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package stream_history_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestStreamHistory(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "QueryStreamHistory Integration Suite")
}
```

- [ ] **Step 2: Write integration tests**

Create `test/integration/stream_history/query_stream_history_test.go` with Ginkgo specs that:

1. Set up a real PostgresEventStore via testcontainers
2. Create a real session in the session store
3. Append events to a stream
4. Call `QueryStreamHistory` on a real CoreServer
5. Verify events returned, pagination, and I-17 enforcement

Follow the patterns in `test/integration/phase1_5_test.go` for test setup.

The implementer MUST write all tests listed in spec §8.9:

- `QueryStreamHistoryReturnsEventsForSubscribedStream`
- `QueryStreamHistoryDeniesNonMemberSceneStream`
- `QueryStreamHistoryPaginatesCorrectly`
- `QueryStreamHistoryAdminReadsPublicStream`

- [ ] **Step 3: Run integration tests**

Run: `task test:int -- ./test/integration/stream_history/`
Expected: PASS.

- [ ] **Step 4: Commit**

```text
jj --no-pager commit -m "test(integration): QueryStreamHistory RPC with real PostgreSQL

Integration tests for full handler flow: session creation, event append,
ABAC evaluation, pagination, and I-17 membership enforcement against
real database via testcontainers.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: Wire Up in Gateway and E2E Test

**Files:**

- Modify: `cmd/holomush/deps.go` or `cmd/holomush/gateway.go` (wire ABAC engine to CoreServer)
- Modify: `web/e2e/terminal.spec.ts`

- [ ] **Step 1: Wire ABAC engine into CoreServer**

Find where `NewCoreServer` is called in `cmd/holomush/` and add `WithAccessEngine(abacEngine)` to the options. Check `cmd/holomush/deps.go` for the ABAC engine setup.

- [ ] **Step 2: Add E2E test**

In `web/e2e/terminal.spec.ts`, add:

```typescript
test('QueryStreamHistory returns events via web gateway', async ({ page }) => {
  // 1. Create guest, connect, send a command to generate events
  // 2. Call WebQueryStreamHistory via the ConnectRPC client
  // 3. Assert events are returned
});
```

Follow the patterns of existing E2E tests in the file.

- [ ] **Step 3: Run E2E tests**

Run: `task test:e2e`
Expected: PASS.

- [ ] **Step 4: Run full pr-prep**

Run: `task pr-prep`
Expected: PASS — all CI mirrors green.

- [ ] **Step 5: Commit**

```text
jj --no-pager commit -m "feat(gateway): wire QueryStreamHistory + E2E test

Connects ABAC engine to CoreServer for stream read authorization.
E2E test verifies WebQueryStreamHistory accessible through web gateway.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Post-Implementation Checklist

- [ ] `task pr-prep` passes (lint, format, schema, license, unit, integration, E2E)
- [ ] All tests from spec §8.1-8.10 are written and passing
- [ ] I-17 invariant tests explicitly named and passing
- [ ] `bd close holomush-oy6e.9` with reason
- [ ] PR created via `gh pr create`
