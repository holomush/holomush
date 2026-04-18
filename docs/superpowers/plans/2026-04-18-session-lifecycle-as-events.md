# Session Lifecycle as Events — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the flaky Telnet E2E "Player A disconnects cleanly via quit" race by making session lifecycle a first-class event on the character's own stream, eliminating the two-plane control-vs-data race that causes STREAM_CLOSED to be delivered after the client's 2-second drain timeout.

**Architecture:** Introduce `EventTypeSessionEnded` on `character:{ID}` streams, emitted by a new `engine.EndSession` method with a decoupled ctx. Subscribe handles termination inside the existing notification pipeline via an unexported `errStreamTerminated` sentinel. Delete `WatchSession`, `session.Event`, `session.Destroyed`, and the `reason` parameter on `sessionStore.Delete`. Add `disconnect` telnet command (pure glue over existing `Disconnect` RPC). Implement logout and PlayerSession eviction fanout so child game sessions emit `session_ended` before their rows cascade.

**Tech Stack:** Go 1.22+, jj/git colocated VCS, PostgreSQL (LISTEN/NOTIFY via pgx), `samber/oops` errors, `oklog/ulid/v2`, mockery v2, Ginkgo/Gomega BDD for integration tests, gotestsum for unit tests, Taskfile for build orchestration.

**Spec:** `docs/superpowers/specs/2026-04-18-session-lifecycle-as-events-design.md`
**Bead:** `holomush-9es6`
**Workspace:** `/Users/sean/Code/github.com/holomush/.worktrees/session-lifecycle-events/`

---

## File Structure Overview

### Files to create

| Path | Responsibility |
|---|---|
| `internal/core/session_ended_payload.go` | `SessionEndedPayload` struct + JSON tags (co-located with other event payloads) |
| `internal/core/engine_end_session.go` | `engine.EndSession` method + context decoupling |
| `internal/core/engine_end_session_test.go` | Unit tests for EndSession shape + ruleguard compliance |
| `internal/core/engine_end_session_ctx_test.go` | Regression test for ctx-decoupling (caller ctx cancel does not drop event) |
| `internal/grpc/stream_terminated.go` | `errStreamTerminated` sentinel (unexported package-local) |
| `internal/grpc/stream_terminated_test.go` | Unit tests for sentinel detection + live-loop short-circuit |
| `test/integration/session_lifecycle/quit_regression_integration_test.go` | Ginkgo suite: flake-proof quit regression + replay isolation + ctx cancel |
| `test/integration/session_lifecycle/fanout_integration_test.go` | Ginkgo suite: multi-surface fan-out + disconnect isolation |
| `test/integration/session_lifecycle/logout_eviction_integration_test.go` | Ginkgo suite: logout fanout + PlayerSession eviction fanout + reaper audit |
| `test/integration/session_lifecycle/ordering_integration_test.go` | Ginkgo suite: character + location cross-stream ordering with session_ended as terminal |
| `test/integration/session_lifecycle/session_lifecycle_suite_test.go` | Ginkgo suite bootstrap (boilerplate analog of `test/integration/telnet/e2e_suite_test.go`) |

### Files to modify

| Path | Reason |
|---|---|
| `internal/core/event.go` | Add `EventTypeSessionEnded` constant |
| `internal/core/engine.go` | (No change — EndSession goes in its own file; NewEngine changes are separate below) |
| `internal/core/engine.go` (again) | `NewEngine` runtime guardrail (Design Decision #8 — concrete type assertion on `*EventWriter`) |
| `internal/session/session.go` | Remove `WatchSession` method from `Store` interface; remove `EventType`, `Destroyed`, `Event`; change `Delete` and `DeleteByCharacter` signatures (drop `reason`) |
| `internal/session/memstore.go` | Remove `watchers` map + `WatchSession` + Delete's watcher-notify block; update `Delete`/`DeleteByCharacter` signatures |
| `internal/store/session_store.go` | Same — Postgres implementation of signature changes and `WatchSession` removal |
| `internal/store/session_store.go` (again) | Add `ListByPlayerSession(ctx, []ulid.ULID) ([]*Info, error)` method to Store interface + Postgres impl |
| `internal/session/memstore.go` (again) | Add `ListByPlayerSession` to MemStore |
| `internal/session/mocks/mock_Store.go` | Regenerate (auto-generated via `mockery`) |
| `internal/grpc/server.go` | Remove `WatchSession` call (~line 876); remove `case sessionCh` arm (~lines 905-913); add `session_ended` detection in `sendAndCommitEvent`; add sentinel short-circuit in live loop and `replayRestorePlan` error path; rewire quit path; rewire guest disconnect; rewire admin boot |
| `internal/grpc/server_test.go` | Update tests for changed signatures; delete WatchSession-dependent tests |
| `cmd/holomush/sub_grpc.go` | Update reaper `OnExpired` callback (add `engine.EndSession` after `HandleDisconnect`) |
| `internal/auth/auth_service.go` | Change `CreateWithCap` callers to use new return type (`[]ulid.ULID` of trimmed PS IDs); implement eviction fanout; implement Logout fanout |
| `internal/auth/player_session.go` (or wherever `CreateWithCap` is defined) | Change `CreateWithCap` signature to return `(trimmedPlayerSessionIDs []ulid.ULID, error)` |
| `internal/auth/repository.go` | Same signature change on the interface |
| `internal/auth/mocks/*` | Regenerate mocks that reference `CreateWithCap` |
| `internal/telnet/gateway_handler.go` | Add `disconnect` command to command dispatcher (pure glue over existing `Disconnect` RPC) |
| `internal/telnet/gateway_handler_test.go` | Add unit test for `disconnect` command |
| `internal/session/memstore_test.go` | Delete `TestMemStore_WatchSession_*` tests (dead code removal) |
| `internal/store/session_store_integration_test.go` | Update `Delete` caller lines 103, 328 (remove `reason` arg) |
| `test/integration/auth/player_session_test.go` | Update `Delete` caller lines 226, 267 (remove `reason` arg) |
| `test/integration/telnet/e2e_test.go` | Delete or skip the flaky "Player A disconnects cleanly via quit" test — the new regression test in `test/integration/session_lifecycle/` replaces it |

### Files to delete

- None (all removals are edits within existing files or mock regeneration)

---

## Dependency Graph

```
Phase 1: Foundation (linear)
  Task 1  -->  Task 2  -->  Task 3

Phase 2: Subscribe cutover (linear, depends on Task 2)
  Task 4  -->  Task 5  -->  Task 6

Phase 3: Interface cleanup + call-site migration (depends on Task 5)
  Task 7  -->  Task 8

Phase 4: Logout/eviction fanout (depends on Task 7)
  Task 9  -->  Task 10  -->  Task 11

Phase 5: Disconnect command (depends on Task 7)
  Task 12

Phase 6: Reaper wiring (depends on Task 2)
  Task 13

Phase 7: Final integration + PR gate (depends on all above)
  Task 14
```

Tasks 9–13 can run in parallel after their prerequisites complete. Task 14 is the final `task pr-prep` gate.

---

## Task 1: New event type and payload

**Goal:** Add `EventTypeSessionEnded` and `SessionEndedPayload` so the rest of the plan has types to reference.

**Files:**

- Modify: `internal/core/event.go` (add constant)
- Create: `internal/core/session_ended_payload.go` (payload struct)

**Dependencies:** none

- [ ] **Step 1: Add event type constant**

Edit `internal/core/event.go`. Find the `EventType` constants block (lines 39–70) and add `EventTypeSessionEnded` after `EventTypeExitUpdate`:

```go
const (
    // ... existing constants ...
    EventTypeLocationState EventType = "location_state"
    EventTypeExitUpdate    EventType = "exit_update"

    // Session lifecycle
    EventTypeSessionEnded EventType = "session_ended"
)
```

- [ ] **Step 2: Create payload struct**

Create `internal/core/session_ended_payload.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

// SessionEndedPayload is the JSON payload for session_ended events.
//
// Emitted on the character's own stream (character:{ID}) when a session
// terminates for any reason. Subscribers filter on SessionID to determine
// whether the termination is for their own session; a non-matching
// session_ended is forwarded verbatim for audit/UX value but does NOT
// terminate the Subscribe stream.
//
// See docs/superpowers/specs/2026-04-18-session-lifecycle-as-events-design.md
// for the full design rationale and load-bearing invariants.
type SessionEndedPayload struct {
    SessionID   string `json:"session_id"`    // ULID of the ended session
    CharacterID string `json:"character_id"`  // ULID of the character whose session ended
    Cause       string `json:"cause"`         // quit|logout|guest_end|kicked|reaped|evicted
    Reason      string `json:"reason"`        // human-readable; delivered to client as STREAM_CLOSED message
}

// Cause constants for SessionEndedPayload.Cause.
const (
    SessionEndedCauseQuit     = "quit"
    SessionEndedCauseLogout   = "logout"
    SessionEndedCauseGuestEnd = "guest_end"
    SessionEndedCauseKicked   = "kicked"
    SessionEndedCauseReaped   = "reaped"
    SessionEndedCauseEvicted  = "evicted"
)
```

- [ ] **Step 3: Run formatter + build**

Run: `task fmt` then `task build`
Expected: clean build, no errors.

- [ ] **Step 4: Commit**

```bash
jj commit -m "feat(core): add EventTypeSessionEnded and SessionEndedPayload (9es6.1)

Introduces the session lifecycle event type that will carry terminal
signals on character:{ID} streams. Payload includes SessionID for
replay-filter discrimination, Cause for audit categorization, and
Reason for client-visible STREAM_CLOSED message.

Part of holomush-9es6.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Engine.EndSession with decoupled ctx

**Goal:** Add the `engine.EndSession` method that emits `session_ended` on the character stream using a decoupled ctx so caller-ctx cancel does not drop the audit-critical event.

**Files:**

- Create: `internal/core/engine_end_session.go`
- Create: `internal/core/engine_end_session_test.go`
- Create: `internal/core/engine_end_session_ctx_test.go`

**Dependencies:** Task 1

- [ ] **Step 1: Write the happy-path test**

Create `internal/core/engine_end_session_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core_test

import (
    "context"
    "encoding/json"
    "testing"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/core"
)

func TestEndSessionEmitsCorrectEventShapeOnCharacterStream(t *testing.T) {
    ctx := context.Background()
    store := newInMemoryTestEventStore(t)
    engine := core.NewEngine(store)

    charID := ulid.Make()
    sessionID := ulid.Make().String()
    char := core.CharacterRef{ID: charID, Name: "Testy", LocationID: ulid.Make()}

    err := engine.EndSession(ctx, char, sessionID, core.SessionEndedCauseQuit, "Goodbye!")
    require.NoError(t, err)

    events := store.All()
    require.Len(t, events, 1)

    ev := events[0]
    assert.Equal(t, "character:"+charID.String(), ev.Stream, "stream must be character:{ID}")
    assert.Equal(t, core.EventTypeSessionEnded, ev.Type)
    assert.Equal(t, core.ActorCharacter, ev.Actor.Kind, "cause=quit uses ActorCharacter")
    assert.Equal(t, charID.String(), ev.Actor.ID)
    assert.NotZero(t, ev.ID, "event MUST have a ULID (monotonic per I-16)")

    var payload core.SessionEndedPayload
    require.NoError(t, json.Unmarshal(ev.Payload, &payload))
    assert.Equal(t, sessionID, payload.SessionID)
    assert.Equal(t, charID.String(), payload.CharacterID)
    assert.Equal(t, core.SessionEndedCauseQuit, payload.Cause)
    assert.Equal(t, "Goodbye!", payload.Reason)
}

func TestEndSessionUsesActorSystemForNonQuitCauses(t *testing.T) {
    ctx := context.Background()
    store := newInMemoryTestEventStore(t)
    engine := core.NewEngine(store)

    charID := ulid.Make()
    char := core.CharacterRef{ID: charID, Name: "Testy", LocationID: ulid.Make()}

    cases := []string{
        core.SessionEndedCauseLogout,
        core.SessionEndedCauseGuestEnd,
        core.SessionEndedCauseKicked,
        core.SessionEndedCauseReaped,
        core.SessionEndedCauseEvicted,
    }

    for _, cause := range cases {
        t.Run("cause="+cause, func(t *testing.T) {
            store.Reset()
            err := engine.EndSession(ctx, char, ulid.Make().String(), cause, "reason")
            require.NoError(t, err)
            events := store.All()
            require.Len(t, events, 1)
            assert.Equal(t, core.ActorSystem, events[0].Actor.Kind)
            assert.Equal(t, "system", events[0].Actor.ID)
        })
    }
}
```

Note: `newInMemoryTestEventStore` is a helper you add to a test-only file in the same package. If one already exists for engine_test.go, reuse it (check `engine_test.go`).

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -timeout 30s -run TestEndSession ./internal/core/`
Expected: FAIL with "engine.EndSession undefined" or similar.

- [ ] **Step 3: Implement EndSession**

Create `internal/core/engine_end_session.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
    "context"
    "encoding/json"
    "time"

    "github.com/samber/oops"
)

// sessionTerminalCommitTimeout bounds how long EndSession will block the
// caller waiting for the session_ended event to persist. It decouples the
// audit-critical append from the caller's ctx (which may have been cancelled
// by a client hangup) — see § New engine method in the design spec.
const sessionTerminalCommitTimeout = 5 * time.Second

// EndSession emits a session_ended event on the character's own stream.
//
// It does NOT delete the session — callers MUST still call
// sessionStore.Delete after EndSession returns. EndSession is responsible
// only for producing the terminal event; storage lifecycle is orthogonal.
//
// Context discipline: EndSession uses a fresh background context with a
// bounded timeout for the append, NOT the caller's ctx. Rationale: a
// client that hangs up mid-quit (ctx cancel) still needs the session_ended
// event persisted for audit and for any OTHER Subscribers on the same
// session to receive STREAM_CLOSED. Dropping the event on caller-ctx
// cancel would reintroduce the orphaning bug this refactor closes.
//
// The caller's ctx is still honored for cancellation of pre-append work
// (payload marshal + error returns) to avoid leaking work when the caller
// has gone away.
func (e *Engine) EndSession(
    ctx context.Context,
    char CharacterRef,
    sessionID string,
    cause string,
    reason string,
) error {
    // Fast-path caller-ctx check so we don't marshal + allocate after
    // the caller has clearly gone away. The append itself uses a fresh ctx.
    if err := ctx.Err(); err != nil {
        return oops.With("operation", "pre_marshal_ctx_check").Wrap(err)
    }

    payload, err := json.Marshal(SessionEndedPayload{
        SessionID:   sessionID,
        CharacterID: char.ID.String(),
        Cause:       cause,
        Reason:      reason,
    })
    if err != nil {
        return oops.With("operation", "marshal_session_ended_payload").Wrap(err)
    }

    actor := Actor{Kind: ActorSystem, ID: "system"}
    if cause == SessionEndedCauseQuit {
        actor = Actor{Kind: ActorCharacter, ID: char.ID.String()}
    }

    event := NewEvent(
        "character:"+char.ID.String(),
        EventTypeSessionEnded,
        actor,
        payload,
    )

    // Decoupled context — see function-level doc comment.
    appendCtx, cancel := context.WithTimeout(context.Background(), sessionTerminalCommitTimeout)
    defer cancel()

    if err := e.store.Append(appendCtx, event); err != nil {
        return oops.Code("SESSION_ENDED_APPEND_FAILED").
            With("session_id", sessionID).
            With("cause", cause).
            Wrap(err)
    }

    return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -timeout 30s -run TestEndSession ./internal/core/`
Expected: PASS.

- [ ] **Step 5: Write ctx-decoupling regression test**

Create `internal/core/engine_end_session_ctx_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core_test

import (
    "context"
    "testing"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/core"
)

// TestEndSessionPersistsEventEvenWhenCallerCtxCancelledAfterMarshal verifies
// the decoupled-ctx discipline: EndSession's append uses context.Background
// + bounded timeout, so a caller ctx cancel after the pre-marshal check
// does not prevent the audit-critical event from persisting.
func TestEndSessionPersistsEventEvenWhenCallerCtxCancelledAfterMarshal(t *testing.T) {
    store := newInMemoryTestEventStore(t)
    engine := core.NewEngine(store)

    ctx, cancel := context.WithCancel(context.Background())
    charID := ulid.Make()
    char := core.CharacterRef{ID: charID, Name: "Testy", LocationID: ulid.Make()}

    // Simulate a caller that cancels its ctx the moment EndSession begins
    // its append. Because append uses a decoupled ctx, the event still
    // persists.
    done := make(chan error, 1)
    go func() {
        done <- engine.EndSession(ctx, char, ulid.Make().String(), core.SessionEndedCauseQuit, "Goodbye!")
    }()
    cancel()
    err := <-done

    require.NoError(t, err, "EndSession must succeed even when caller ctx is cancelled")
    assert.Len(t, store.All(), 1, "session_ended event MUST be persisted")
}

// TestEndSessionFailsFastWhenCallerCtxAlreadyCancelled verifies that
// EndSession does not do wasted work if the caller has clearly already
// gone (ctx.Err() non-nil before marshal).
func TestEndSessionFailsFastWhenCallerCtxAlreadyCancelled(t *testing.T) {
    store := newInMemoryTestEventStore(t)
    engine := core.NewEngine(store)

    ctx, cancel := context.WithCancel(context.Background())
    cancel() // pre-cancel

    charID := ulid.Make()
    char := core.CharacterRef{ID: charID, Name: "Testy", LocationID: ulid.Make()}

    err := engine.EndSession(ctx, char, ulid.Make().String(), core.SessionEndedCauseQuit, "Goodbye!")
    assert.Error(t, err, "pre-cancelled ctx should fail fast")
    assert.Empty(t, store.All(), "no event should be appended")
}
```

- [ ] **Step 6: Run tests**

Run: `task test -- -timeout 30s -run TestEndSession ./internal/core/`
Expected: all four tests PASS.

- [ ] **Step 7: Lint + commit**

```bash
task lint
# If clean:
jj commit -m "feat(core): engine.EndSession emits session_ended on character stream (9es6.2)

Introduces the session-terminal-event emission primitive. Actor is
ActorCharacter for cause=quit and ActorSystem{id=\"system\"} for all
other causes per Design Decision #1.

Context discipline: append uses a fresh context.Background with
bounded timeout so caller-ctx cancel (e.g., client hangup mid-quit)
does not drop the audit-critical event. Pre-marshal check preserves
fast-fail for callers that have clearly gone away.

Ruleguard EventIDMustBeMonotonic satisfied via core.NewEvent which
stamps core.NewULID().

Part of holomush-9es6.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: EventWriter runtime guardrail in NewEngine

**Goal:** Enforce I1 (EventWriter serialization) at engine construction time in production. A concrete-type assertion ensures the engine's store is an `*EventWriter` rather than a raw store.

**Files:**

- Modify: `internal/core/engine.go` (update `NewEngine`)
- Create test case alongside existing `engine_test.go`

**Dependencies:** Task 2

- [ ] **Step 1: Write the failing test**

Add to `internal/core/engine_test.go` (or create if guardrail deserves its own file):

```go
func TestNewEnginePanicsWhenStoreIsNotEventWriterInProductionMode(t *testing.T) {
    rawStore := newInMemoryTestEventStore(t) // implements EventStore but not *EventWriter

    defer func() {
        if r := recover(); r == nil {
            t.Fatal("expected panic from NewEngine in production mode with non-writer store")
        }
    }()

    core.NewEngine(rawStore, core.WithProductionGuardrail())
}

func TestNewEngineAcceptsRawStoreInTestMode(t *testing.T) {
    rawStore := newInMemoryTestEventStore(t)
    // No WithProductionGuardrail — test default is permissive.
    e := core.NewEngine(rawStore)
    assert.NotNil(t, e)
}

func TestNewEngineAcceptsEventWriterInProductionMode(t *testing.T) {
    rawStore := newInMemoryTestEventStore(t)
    writer := core.NewEventWriter(rawStore)
    t.Cleanup(func() { writer.Close() })

    e := core.NewEngine(writer, core.WithProductionGuardrail())
    assert.NotNil(t, e)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -timeout 30s -run TestNewEngine ./internal/core/`
Expected: FAIL with "WithProductionGuardrail undefined" or "NewEngine accepts too many arguments".

- [ ] **Step 3: Implement the option + guard**

Edit `internal/core/engine.go`:

```go
// EngineOption configures a new Engine.
type EngineOption func(*engineConfig)

type engineConfig struct {
    productionGuardrail bool
}

// WithProductionGuardrail enables the runtime assertion that the engine's
// store is a *core.EventWriter. Enforces invariant I1 (EventWriter
// serialization) in production wiring. Test constructors typically omit
// this to allow lightweight in-memory stores for pure-logic tests.
//
// See docs/superpowers/specs/2026-04-18-session-lifecycle-as-events-design.md
// § Load-Bearing Invariants I1 and Design Decision #8.
func WithProductionGuardrail() EngineOption {
    return func(c *engineConfig) { c.productionGuardrail = true }
}

// NewEngine creates a new game engine.
//
// When WithProductionGuardrail is passed, the store parameter MUST be a
// *EventWriter (concrete-type assertion). This catches regressions where
// engine wiring bypasses the writer's serialization guarantee.
func NewEngine(store EventStore, opts ...EngineOption) *Engine {
    cfg := engineConfig{}
    for _, opt := range opts {
        opt(&cfg)
    }
    if cfg.productionGuardrail {
        if _, ok := store.(*EventWriter); !ok {
            panic("core.NewEngine: production mode requires *EventWriter store (I1 guardrail). " +
                "Got " + typeName(store) + ". See design spec Design Decision #8.")
        }
    }
    return &Engine{store: store}
}

// typeName returns a human-readable type name for the panic message.
// Falls back to "<nil>" for nil interfaces.
func typeName(v any) string {
    if v == nil {
        return "<nil>"
    }
    return fmt.Sprintf("%T", v)
}
```

Add import for `fmt`.

- [ ] **Step 4: Wire production construction**

Edit `cmd/holomush/sub_grpc.go` around line 135. Change:

```go
engine := core.NewEngine(eventStore)
```

to:

```go
engine := core.NewEngine(eventStore, core.WithProductionGuardrail())
```

- [ ] **Step 5: Run tests**

Run: `task test -- -timeout 30s -run TestNewEngine ./internal/core/ && task test -- -timeout 30s ./cmd/holomush/...`
Expected: PASS.

- [ ] **Step 6: Lint + commit**

```bash
task lint && jj commit -m "feat(core): I1 runtime guardrail on NewEngine (9es6.3)

Adds WithProductionGuardrail option that enforces the engine's store
is a *core.EventWriter via concrete-type assertion. Interface
conformance is insufficient because *EventWriter implements EventStore
by wrapping — raw stores also satisfy the interface.

Wired into cmd/holomush/sub_grpc.go so any future refactor that
bypasses the writer fails loudly at startup rather than silently
breaking the append-order = notification-order invariant that
session_ended ordering depends on.

Test constructors opt out by omitting the option.

Part of holomush-9es6.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: errStreamTerminated sentinel

**Goal:** Introduce the unexported sentinel error that Subscribe translates to graceful stream close, used by `sendAndCommitEvent` to signal "session_ended matched, stream terminating."

**Files:**

- Create: `internal/grpc/stream_terminated.go`
- Create: `internal/grpc/stream_terminated_test.go`

**Dependencies:** Task 2

- [ ] **Step 1: Write the failing test**

Create `internal/grpc/stream_terminated_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
    "errors"
    "testing"

    "github.com/samber/oops"
    "github.com/stretchr/testify/assert"
)

func TestErrStreamTerminatedIsDetectableViaErrorsIs(t *testing.T) {
    assert.True(t, errors.Is(errStreamTerminated, errStreamTerminated))
}

func TestErrStreamTerminatedSurvivesOopsWrap(t *testing.T) {
    wrapped := oops.Code("SEND_FAILED").Wrap(errStreamTerminated)
    assert.True(t, errors.Is(wrapped, errStreamTerminated),
        "oops must preserve the sentinel through wrap")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -timeout 30s -run TestErrStreamTerminated ./internal/grpc/`
Expected: FAIL with "errStreamTerminated undefined".

- [ ] **Step 3: Create the sentinel**

Create `internal/grpc/stream_terminated.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import "errors"

// errStreamTerminated signals graceful Subscribe termination after a
// matching session_ended event. Propagates from sendAndCommitEvent →
// replayAndSend → the Subscribe live loop, which translates it to
// `return nil` (clean gRPC stream close).
//
// Unexported — not a public contract. The only callers that check for
// it are internal to this package.
//
// See docs/superpowers/specs/2026-04-18-session-lifecycle-as-events-design.md
// § Sentinel propagation.
var errStreamTerminated = errors.New("stream terminated by session_ended")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -timeout 30s -run TestErrStreamTerminated ./internal/grpc/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj commit -m "feat(grpc): errStreamTerminated sentinel for graceful Subscribe close (9es6.4)

Unexported sentinel that propagates from sendAndCommitEvent through
replayAndSend to the Subscribe live loop when a matching session_ended
event terminates the stream. Survives oops.Wrap so existing error
wrapping at the call sites keeps working unchanged.

Part of holomush-9es6.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: sendAndCommitEvent handles session_ended terminal

**Goal:** Extend `sendAndCommitEvent` to detect `session_ended` events whose payload `SessionID` matches the current session, send STREAM_CLOSED, and return the sentinel. Non-matching events are forwarded intact per Design Decision #3.

**Files:**

- Modify: `internal/grpc/server.go` (sendAndCommitEvent, around line 604)
- Modify: `internal/grpc/server_test.go` (add coverage)

**Dependencies:** Task 4

- [ ] **Step 1: Write the failing tests**

Add to `internal/grpc/server_test.go`:

```go
func TestSendAndCommitEventTerminatesStreamOnMatchingSessionEnded(t *testing.T) {
    // ARRANGE
    // Construct a CoreServer with an in-memory event store, a fake grpc
    // stream that captures sends, and session info whose ID matches the
    // payload's SessionID.
    srv, fakeStream, info := newSendAndCommitEventHarness(t)

    charID := info.CharacterID
    payload, err := json.Marshal(core.SessionEndedPayload{
        SessionID:   info.ID,
        CharacterID: charID.String(),
        Cause:       core.SessionEndedCauseQuit,
        Reason:      "Goodbye!",
    })
    require.NoError(t, err)

    ev := core.NewEvent(
        "character:"+charID.String(),
        core.EventTypeSessionEnded,
        core.Actor{Kind: core.ActorCharacter, ID: charID.String()},
        payload,
    )

    // ACT
    err = srv.sendAndCommitEvent(context.Background(), info, ev.Stream, ev, fakeStream, nil)

    // ASSERT
    assert.ErrorIs(t, err, errStreamTerminated, "matching session_ended must return sentinel")

    // The fake stream should have received TWO sends: the event itself, then STREAM_CLOSED.
    require.Len(t, fakeStream.sent, 2)
    assert.NotNil(t, fakeStream.sent[0].GetEvent(), "first frame: the event itself (forwarded to client)")
    require.NotNil(t, fakeStream.sent[1].GetControl(), "second frame: STREAM_CLOSED control")
    assert.Equal(t, corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED,
        fakeStream.sent[1].GetControl().GetSignal())
    assert.Equal(t, "Goodbye!", fakeStream.sent[1].GetControl().GetMessage())
}

func TestSendAndCommitEventForwardsNonMatchingSessionEndedVerbatim(t *testing.T) {
    // A replayed session_ended from a PRIOR session (different SessionID
    // in payload) must be forwarded verbatim and must NOT terminate.
    srv, fakeStream, info := newSendAndCommitEventHarness(t)
    charID := info.CharacterID

    payload, err := json.Marshal(core.SessionEndedPayload{
        SessionID:   "01PRIORSESSIONPRIOR0000000",  // different from info.ID
        CharacterID: charID.String(),
        Cause:       core.SessionEndedCauseQuit,
        Reason:      "Goodbye!",
    })
    require.NoError(t, err)

    ev := core.NewEvent(
        "character:"+charID.String(),
        core.EventTypeSessionEnded,
        core.Actor{Kind: core.ActorCharacter, ID: charID.String()},
        payload,
    )

    err = srv.sendAndCommitEvent(context.Background(), info, ev.Stream, ev, fakeStream, nil)

    assert.NoError(t, err, "non-matching session_ended must NOT return the sentinel")

    // Exactly ONE send: the event itself. No STREAM_CLOSED.
    require.Len(t, fakeStream.sent, 1)
    assert.NotNil(t, fakeStream.sent[0].GetEvent())
}
```

Helper `newSendAndCommitEventHarness` should mirror existing test helper patterns in `server_test.go`. It returns a CoreServer, a fake grpc.ServerStream that captures every `Send` call, and a valid session.Info with a populated ID/CharacterID.

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -timeout 30s -run TestSendAndCommitEvent ./internal/grpc/`
Expected: FAIL.

- [ ] **Step 3: Extend sendAndCommitEvent**

Edit `internal/grpc/server.go` around line 604. After the existing Send + UpdateCursors block (around line 656), add the session_ended detection:

```go
func (s *CoreServer) sendAndCommitEvent(
    ctx context.Context,
    info *session.Info,
    streamName string,
    ev core.Event,
    grpcStream grpc.ServerStreamingServer[corev1.SubscribeResponse],
    lf *locationFollower,
) error {
    // ... existing locking + handleEvent + Send code unchanged ...

    // Existing UpdateCursors logic (around line 648) unchanged.

    // NEW: check for session_ended terminal after Send + UpdateCursors succeed.
    // Non-matching session_ended is forwarded verbatim (already Sent above) and
    // the cursor is already advanced; we just return nil.
    if ev.Type == core.EventTypeSessionEnded {
        var payload core.SessionEndedPayload
        if err := json.Unmarshal(ev.Payload, &payload); err == nil && payload.SessionID == info.ID {
            // Terminal match: emit STREAM_CLOSED and return sentinel.
            //nolint:errcheck // best-effort: client may already be disconnected
            _ = grpcStream.Send(streamClosedFrame(payload.Reason))
            return errStreamTerminated
        }
    }
    return nil
}
```

Note: leave the existing error handling, cursor commit logic, and lock semantics UNCHANGED. The new block is strictly additive at the tail.

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -timeout 30s -run TestSendAndCommitEvent ./internal/grpc/`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

```bash
task lint && jj commit -m "feat(grpc): sendAndCommitEvent handles session_ended terminal (9es6.5)

After Send + UpdateCursors succeed, if the event is session_ended
with a payload SessionID matching the current session, emit
STREAM_CLOSED (reason from payload) and return errStreamTerminated.

Non-matching session_ended (replay of a prior session's terminal
event) is forwarded verbatim — the event is already Sent and the
cursor already advanced; the function just returns nil. Per
Design Decision #3 this gives multi-surface clients free 'your
other session ended' UX value and guarantees cursors never stall.

Part of holomush-9es6.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Subscribe live-loop sentinel short-circuit + WatchSession removal

**Goal:** Wire the sentinel through the Subscribe live loop and `replayRestorePlan` error paths; remove the `WatchSession` call and the `sessionCh` arm.

**Files:**

- Modify: `internal/grpc/server.go` (Subscribe: ~lines 876, 905-913, 894-923)
- Modify: `internal/grpc/replay.go` (replayRestorePlan callers, if a wrapper exists) or the relevant site in `server.go` that calls replayRestorePlan
- Modify: `internal/grpc/server_test.go` (remove tests that depend on WatchSession-driven STREAM_CLOSED)

**Dependencies:** Task 5

- [ ] **Step 1: Write the failing test**

Add to `internal/grpc/server_test.go` a test that opens a Subscribe stream and injects a matching `session_ended` event via the event store; asserts the Subscribe returns nil (graceful close) and the client receives both the event and STREAM_CLOSED.

Since Subscribe is a gRPC streaming handler, use the existing test harness patterns (look for `TestCoreServer_Subscribe*` tests that set up fake streams and event stores).

```go
func TestSubscribeReturnsCleanlyWhenSessionEndedMatchesCurrentSession(t *testing.T) {
    // Harness: server with in-memory event store, real auth.ValidateSessionOwnership
    // stub, a fake grpc stream, and an active session.
    h := newSubscribeTestHarness(t)

    // Start Subscribe in a goroutine
    errCh := make(chan error, 1)
    go func() {
        errCh <- h.server.Subscribe(h.subReq, h.fakeStream)
    }()

    // Wait for the initial replay + REPLAY_COMPLETE to settle.
    h.awaitReplayComplete()

    // Append a matching session_ended event to the event store.
    payload, _ := json.Marshal(core.SessionEndedPayload{
        SessionID:   h.sessionInfo.ID,
        CharacterID: h.sessionInfo.CharacterID.String(),
        Cause:       core.SessionEndedCauseQuit,
        Reason:      "Goodbye!",
    })
    require.NoError(t, h.eventWriter.Write(ctx, core.NewEvent(
        "character:"+h.sessionInfo.CharacterID.String(),
        core.EventTypeSessionEnded,
        core.Actor{Kind: core.ActorCharacter, ID: h.sessionInfo.CharacterID.String()},
        payload,
    )))

    // Subscribe should return nil within a bounded window.
    select {
    case err := <-errCh:
        assert.NoError(t, err, "Subscribe must return nil on matching session_ended")
    case <-time.After(5 * time.Second):
        t.Fatal("Subscribe did not return within 5s after matching session_ended")
    }

    // Fake stream must have received STREAM_CLOSED as the final frame.
    frames := h.fakeStream.CapturedFrames()
    require.NotEmpty(t, frames)
    last := frames[len(frames)-1]
    require.NotNil(t, last.GetControl())
    assert.Equal(t, corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED, last.GetControl().GetSignal())
    assert.Equal(t, "Goodbye!", last.GetControl().GetMessage())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -timeout 30s -run TestSubscribeReturnsCleanly ./internal/grpc/`
Expected: FAIL or hang (if the old WatchSession path still dominates).

- [ ] **Step 3: Update the Subscribe live loop**

Edit `internal/grpc/server.go`. In the Subscribe function around lines 875–923:

**a) Remove the WatchSession call (around line 876):**

```go
// DELETE these lines:
sessionCh, watchErr := s.sessionStore.WatchSession(ctx, req.SessionId)
if watchErr != nil {
    slog.Warn("failed to watch session lifecycle",
        "session_id", req.SessionId, "error", watchErr)
}
```

**b) Update the live-loop select to remove the sessionCh arm and add sentinel short-circuit on the notification arm:**

```go
// 11. Live loop — single select on Subscription notifications + control.
for {
    select {
    case <-ctx.Done():
        if ctx.Err() == context.Canceled {
            return nil
        }
        return oops.Code("SUBSCRIPTION_CANCELLED").With("session_id", req.SessionId).Wrap(ctx.Err())

    case subLoopErr := <-sub.Errors():
        return oops.Code("SUBSCRIPTION_ERROR").With("session_id", req.SessionId).Wrap(subLoopErr)

    case notif := <-sub.Notifications():
        cursor := ulid.ULID{}
        if c, ok := info.EventCursors[notif.Stream]; ok {
            cursor = c
        }
        last, sendErr := s.replayAndSend(ctx, info, notif.Stream, cursor, stream, lf)
        if errors.Is(sendErr, errStreamTerminated) {
            return nil
        }
        if sendErr != nil {
            return oops.Code("SEND_FAILED").With("session_id", req.SessionId).Wrap(sendErr)
        }
        info.EventCursors[notif.Stream] = last

    // NOTE: sessionCh arm DELETED — session lifecycle is now event-sourced via
    // session_ended on the character stream, handled inside sendAndCommitEvent.

    case ctrl, ok := <-ctrlCh:
        if !ok {
            return nil
        }
        if ctrlErr := s.applyCtrlUpdate(ctx, info, sub, ctrl, stream, lf); ctrlErr != nil {
            return oops.Code("SEND_FAILED").With("session_id", info.ID).Wrap(ctrlErr)
        }
    }
}
```

- [ ] **Step 4: Update replayRestorePlan caller site**

Find `replayRestorePlan` in server.go (search for the call, around line 866). Add the same sentinel short-circuit:

```go
// 8. Merge-sort replay (I-15).
if replayErr := s.replayRestorePlan(ctx, info, plan, stream, lf); replayErr != nil {
    if errors.Is(replayErr, errStreamTerminated) {
        return nil
    }
    return replayErr
}
```

- [ ] **Step 5: Run tests**

Run: `task test -- -timeout 60s ./internal/grpc/...`
Expected: new tests PASS. Some EXISTING tests that asserted WatchSession-triggered STREAM_CLOSED will now FAIL — mark those for deletion/update in Step 6.

- [ ] **Step 6: Clean up tests that directly exercised WatchSession → STREAM_CLOSED**

Search `internal/grpc/server_test.go` for `WatchSession`, `streamClosedFrame("Goodbye!")` assertions, `session.Destroyed`, etc. Those tests rely on behavior the new architecture replaces. Either:

- Delete the test if it's covered by the new `TestSubscribeReturnsCleanly...` test
- Update it to drive the new event-sourced path (emit session_ended via eventWriter, assert same observable)

Err on the side of replacement rather than deletion — keep test coverage, but move from "WatchSession fires → STREAM_CLOSED" to "session_ended appended → STREAM_CLOSED".

- [ ] **Step 7: Run full package tests**

Run: `task test -- -timeout 90s ./internal/grpc/...`
Expected: all PASS.

- [ ] **Step 8: Lint + commit**

```bash
task lint && jj commit -m "refactor(grpc): Subscribe cutover — session_ended in live loop (9es6.6)

Removes WatchSession wire-up and the sessionCh arm from Subscribe.
Session termination is now detected inside sendAndCommitEvent when
a session_ended event's payload SessionID matches info.ID; the
errStreamTerminated sentinel propagates through replayAndSend and
replayRestorePlan to the live loop's notification arm, which
short-circuits to 'return nil' (clean gRPC close).

Also updates replayRestorePlan caller to treat the sentinel as
graceful termination rather than an error to wrap.

Tests previously asserting WatchSession-triggered STREAM_CLOSED
either deleted (covered by new TestSubscribeReturnsCleanly...) or
rewritten to drive the event-sourced path.

Part of holomush-9es6.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Delete signature change + WatchSession removal + mock regen

**Goal:** Remove the `reason` parameter from `Store.Delete` and `Store.DeleteByCharacter`; delete `WatchSession`, `session.Event`, `session.EventType`, `session.Destroyed`; regenerate mocks; migrate all call sites.

**Files:**

- Modify: `internal/session/session.go` (Store interface + type deletions)
- Modify: `internal/session/memstore.go` (signature changes + watchers map removal)
- Modify: `internal/store/session_store.go` (same for Postgres)
- Modify: `internal/session/memstore_test.go` (delete TestMemStore_WatchSession_*)
- Modify: `internal/store/session_store_integration_test.go` (update Delete calls lines 103, 328)
- Modify: `test/integration/auth/player_session_test.go` (update Delete calls lines 226, 267)
- Modify: `internal/grpc/server.go` (update `Delete` call sites — quit handler :411, guest disconnect :1041, anywhere else)
- Modify: `internal/session/reaper.go` (line 82 — Delete call)
- Modify: `internal/session/mocks/mock_Store.go` (regenerate via mockery)
- Delete: nothing at file level — everything is in-file removals

**Dependencies:** Task 6

- [ ] **Step 1: Change the Store interface signature**

Edit `internal/session/session.go`:

**a) Remove the `EventType`, `Destroyed`, `Event` types** (lines 180–192):

```go
// DELETE these:
//
// type EventType int
// const ( Destroyed EventType = iota )
// type Event struct { Type EventType; Message string }
```

**b) Remove `WatchSession` from the Store interface** (lines 235–238):

```go
// DELETE:
// WatchSession(ctx context.Context, sessionID string) (<-chan Event, error)
```

**c) Change `Delete` signature** (line 233):

```go
// BEFORE:
// Delete(ctx context.Context, id string, reason string) error

// AFTER:
Delete(ctx context.Context, id string) error
```

**d) Change `DeleteByCharacter` signature** (lines 290–292):

```go
// BEFORE:
// DeleteByCharacter(ctx context.Context, characterID ulid.ULID, reason string) (*Info, error)

// AFTER:
DeleteByCharacter(ctx context.Context, characterID ulid.ULID) (*Info, error)
```

**e) Same change in the `Access` interface** (line 209):

```go
// BEFORE:
// DeleteByCharacter(ctx context.Context, characterID ulid.ULID, reason string) (*Info, error)

// AFTER:
DeleteByCharacter(ctx context.Context, characterID ulid.ULID) (*Info, error)
```

- [ ] **Step 2: Update MemStore implementation**

Edit `internal/session/memstore.go`:

- Remove the `watchers map[string][]chan Event` field
- Remove the `watchers: make(map[string][]chan Event)` initialization
- In `Delete`, remove the `reason string` parameter and the watcher-notify block (roughly lines 60–81). Keep the session + connections removal logic.
- Remove the entire `WatchSession` method (lines 83–91)
- In `DeleteByCharacter`, remove `reason string` parameter and pass-through to the new `Delete` signature

- [ ] **Step 3: Update Postgres implementation**

Edit `internal/store/session_store.go`:

- Remove the watchers map and `WatchSession` method (line 338+)
- In `Delete`, remove `reason` parameter and the watcher-notify block (around line 316). Keep the SQL DELETE.
- Same for `DeleteByCharacter`

- [ ] **Step 4: Regenerate mocks**

Run:

```bash
mockery
```

Expected: `internal/session/mocks/mock_Store.go` and any auth mocks touched by the signature change are regenerated.

- [ ] **Step 5: Update production call sites**

Grep for `sessionStore.Delete(` and fix each — all call sites lose their `reason` arg:

- `internal/grpc/server.go:411` (quit handler) — will change in Task 8 to add EndSession call. For now, just drop the reason arg.
- `internal/grpc/server.go:1041` (guest disconnect) — will change in Task 8. Drop the reason arg.
- `internal/session/reaper.go:82` — drop the reason arg. Reaper gets its EndSession wiring in Task 13.

Grep for `sessionStore.DeleteByCharacter(` and fix each similarly.

- [ ] **Step 6: Update test call sites**

Fix the Delete call-site lines flagged by the reviewer:

- `test/integration/auth/player_session_test.go:226`: remove the reason arg
- `test/integration/auth/player_session_test.go:267`: remove the reason arg
- `internal/store/session_store_integration_test.go:103`: remove the reason arg
- `internal/store/session_store_integration_test.go:328`: remove the reason arg

Delete dead WatchSession tests in `internal/session/memstore_test.go`:

- Find `TestMemStore_WatchSession_*` tests and delete them. Do NOT port — the signal they tested no longer exists.

- [ ] **Step 7: Run full build + tests**

```bash
task lint
task test -- -timeout 60s ./internal/...
task test -- -timeout 60s ./cmd/holomush/...
```

Expected: clean build, all unit tests pass.

- [ ] **Step 8: Commit**

```bash
jj commit -m "refactor(session): drop WatchSession + reason param on Delete (9es6.7)

Removes the session-lifecycle control plane now obsolete under the
event-sourced session_ended approach:

* Store.Delete signature drops the reason parameter
* Store.DeleteByCharacter signature drops the reason parameter
* Access.DeleteByCharacter same
* WatchSession method removed from Store interface
* session.EventType, session.Destroyed, session.Event types removed
* MemStore's watchers map + watcher-notify block removed
* PostgresSessionStore same

Regenerates internal/session/mocks/mock_Store.go via mockery.

All call sites updated. Tests that asserted on WatchSession behavior
deleted (their semantics are replaced by the new event-sourced flow
tested in internal/grpc and the integration suites).

Part of holomush-9es6.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: Quit + guest-disconnect + admin-boot handlers emit session_ended

**Goal:** Rewire the three session-ending paths in `internal/grpc/server.go` so they call `engine.EndSession` between `HandleDisconnect` and `sessionStore.Delete`.

**Files:**

- Modify: `internal/grpc/server.go` (quit handler ~:407-416, guest disconnect ~:1029-1049, admin boot ~:440-465)

**Dependencies:** Task 7

- [ ] **Step 1: Write the failing test**

Add to `internal/grpc/server_test.go` a unit test that exercises the quit path with an engine whose store captures appends, and asserts a session_ended event is appended on the character stream with the right payload/cause.

```go
func TestQuitPathAppendsSessionEndedOnCharacterStream(t *testing.T) {
    // Harness: CoreServer with capturing event store, populated session info,
    // and a dispatcher that returns command.ErrSessionEnded for "quit".
    h := newQuitHandlerHarness(t)

    _, err := h.server.HandleCommand(ctx, &corev1.HandleCommandRequest{
        SessionId:          h.sessionInfo.ID,
        Command:            "quit",
        PlayerSessionToken: h.playerSessionToken,
    })
    require.NoError(t, err)

    // The capturing store should have received 2 events: leave (on location)
    // and session_ended (on character).
    events := h.eventStore.Captured()
    require.Len(t, events, 2)

    var leaveEv, sessionEndedEv core.Event
    for _, e := range events {
        switch e.Type {
        case core.EventTypeLeave:
            leaveEv = e
        case core.EventTypeSessionEnded:
            sessionEndedEv = e
        }
    }

    // Leave on location stream.
    assert.Equal(t, "location:"+h.sessionInfo.LocationID.String(), leaveEv.Stream)

    // session_ended on character stream.
    assert.Equal(t, "character:"+h.sessionInfo.CharacterID.String(), sessionEndedEv.Stream)

    var payload core.SessionEndedPayload
    require.NoError(t, json.Unmarshal(sessionEndedEv.Payload, &payload))
    assert.Equal(t, h.sessionInfo.ID, payload.SessionID)
    assert.Equal(t, core.SessionEndedCauseQuit, payload.Cause)
    assert.Equal(t, "Goodbye!", payload.Reason)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -timeout 30s -run TestQuitPath ./internal/grpc/`
Expected: FAIL (only `leave` appended, no `session_ended`).

- [ ] **Step 3: Update quit handler**

Edit `internal/grpc/server.go` around lines 407–416:

```go
// Quit/self-boot detection: handler signals intent, server does teardown.
if errors.Is(dispatchErr, command.ErrSessionEnded) {
    if dcErr := s.engine.HandleDisconnect(ctx, char, "quit"); dcErr != nil {
        slog.WarnContext(ctx, "leave event failed", "error", dcErr)
    }
    if endErr := s.engine.EndSession(ctx, char, info.ID,
        core.SessionEndedCauseQuit, "Goodbye!"); endErr != nil {
        slog.WarnContext(ctx, "session_ended event failed",
            "session_id", info.ID, "error", endErr)
    }
    if delErr := s.sessionStore.Delete(ctx, info.ID); delErr != nil {
        slog.WarnContext(ctx, "session delete failed", "error", delErr)
    }
    s.runDisconnectHooks(ctx, *info)
    return nil
}
```

- [ ] **Step 4: Update guest-disconnect path**

Find the guest disconnect block around `internal/grpc/server.go:1029-1049`:

```go
if info.IsGuest {
    char := core.CharacterRef{ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID}
    if err := s.engine.HandleDisconnect(ctx, char, "quit"); err != nil {
        slog.WarnContext(ctx, "leave event failed", ...)
    }
    if endErr := s.engine.EndSession(ctx, char, info.ID,
        core.SessionEndedCauseGuestEnd, "Session ended."); endErr != nil {
        slog.WarnContext(ctx, "session_ended event failed",
            "session_id", info.ID, "error", endErr)
    }
    if err := s.sessionStore.Delete(ctx, info.ID); err != nil {
        slog.WarnContext(ctx, "failed to delete guest session", ...)
    }
    s.runDisconnectHooks(ctx, *info)
}
```

- [ ] **Step 5: Update admin-boot path**

Find the boot iteration around `internal/grpc/server.go:440-465`. The existing code calls `HandleDisconnect` but never deletes the session from the dispatch-time boot path — the actual session deletion happens elsewhere in the boot RPC flow (grep for `sessionStore.Delete(ctx, .*boot` and the admin boot RPC handler to locate it).

**Residual Open Question #1 resolution:** locate the exact Delete site for the admin-boot RPC. Likely in a file like `internal/grpc/admin_boot.go` or similar; if it doesn't exist as a separate file, the Delete is probably in the boot RPC handler itself. Once found, apply the pattern:

```go
if dcErr := s.engine.HandleDisconnect(ctx, char, "booted"); dcErr != nil { ... }
if endErr := s.engine.EndSession(ctx, char, info.ID,
    core.SessionEndedCauseKicked,
    "You have been disconnected by an administrator."); endErr != nil { ... }
if delErr := s.sessionStore.Delete(ctx, info.ID); delErr != nil { ... }
s.runDisconnectHooks(ctx, *info)
```

If the boot RPC code does NOT currently call `Delete`, add it per the spec's target flow.

- [ ] **Step 6: Run tests**

```bash
task test -- -timeout 60s ./internal/grpc/...
```

Expected: new test PASS; existing quit-related tests PASS (they were updated in Task 7 to align with the new signatures).

- [ ] **Step 7: Lint + commit**

```bash
task lint && jj commit -m "feat(grpc): quit + guest + boot paths emit session_ended (9es6.8)

Rewires the three server-side session-terminating paths:

  1. Quit handler (server.go:~411) — Quit cause, 'Goodbye!' reason
  2. Guest disconnect (server.go:~1041) — GuestEnd cause, 'Session ended.'
  3. Admin boot (server.go:~457) — Kicked cause, admin-disconnect reason

Each path now: HandleDisconnect (leave on location) →
engine.EndSession (session_ended on character) →
sessionStore.Delete → runDisconnectHooks.

Ordering guaranteed by I1 (EventWriter serialization) + I2
(single-connection LISTEN order): by the time the handler returns,
the Subscribe stream has received the session_ended notification,
emitted STREAM_CLOSED, and cleanly closed.

Part of holomush-9es6.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 9: ListByPlayerSession store method

**Goal:** Add the new enumeration method to `session.Store` so Logout and eviction fanout can find the child game sessions to emit `session_ended` for.

**Files:**

- Modify: `internal/session/session.go` (Store interface)
- Modify: `internal/session/memstore.go` (implementation)
- Modify: `internal/store/session_store.go` (Postgres implementation)
- Modify: `internal/session/memstore_test.go` (tests)
- Modify: `internal/store/session_store_integration_test.go` (Postgres integration test)
- Modify: `internal/session/mocks/mock_Store.go` (regenerated)

**Dependencies:** Task 7

- [ ] **Step 1: Write the failing test (MemStore)**

Add to `internal/session/memstore_test.go`:

```go
func TestMemStoreListByPlayerSessionReturnsOnlyMatchingSessions(t *testing.T) {
    ctx := context.Background()
    store := session.NewMemStore()

    ps1 := ulid.Make()
    ps2 := ulid.Make()
    ps3 := ulid.Make()

    require.NoError(t, store.Set(ctx, "s1", &session.Info{
        ID: "s1", CharacterID: ulid.Make(), PlayerSessionID: ps1, Status: session.StatusActive,
    }))
    require.NoError(t, store.Set(ctx, "s2", &session.Info{
        ID: "s2", CharacterID: ulid.Make(), PlayerSessionID: ps2, Status: session.StatusActive,
    }))
    require.NoError(t, store.Set(ctx, "s3", &session.Info{
        ID: "s3", CharacterID: ulid.Make(), PlayerSessionID: ps1, Status: session.StatusActive,
    }))
    require.NoError(t, store.Set(ctx, "s4", &session.Info{
        ID: "s4", CharacterID: ulid.Make(), PlayerSessionID: ps3, Status: session.StatusActive,
    }))

    got, err := store.ListByPlayerSession(ctx, []ulid.ULID{ps1, ps2})
    require.NoError(t, err)

    gotIDs := make(map[string]bool)
    for _, info := range got {
        gotIDs[info.ID] = true
    }
    assert.True(t, gotIDs["s1"])
    assert.True(t, gotIDs["s2"])
    assert.True(t, gotIDs["s3"])
    assert.False(t, gotIDs["s4"], "s4's PlayerSession not in query — must be excluded")
    assert.Len(t, got, 3)
}

func TestMemStoreListByPlayerSessionReturnsEmptyForNoMatches(t *testing.T) {
    ctx := context.Background()
    store := session.NewMemStore()
    got, err := store.ListByPlayerSession(ctx, []ulid.ULID{ulid.Make()})
    require.NoError(t, err)
    assert.Empty(t, got)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -timeout 30s -run TestMemStoreListByPlayerSession ./internal/session/`
Expected: FAIL with "ListByPlayerSession undefined".

- [ ] **Step 3: Add to Store interface + implement in MemStore**

Edit `internal/session/session.go`, add to the `Store` interface:

```go
// ListByPlayerSession returns all active/detached sessions whose
// PlayerSessionID matches any of the given IDs. Used by logout and
// PlayerSession eviction fanout to identify child game sessions that
// need their session_ended event emitted before the PlayerSession is
// deleted (and the FK cascade removes them).
ListByPlayerSession(ctx context.Context, playerSessionIDs []ulid.ULID) ([]*Info, error)
```

Edit `internal/session/memstore.go`:

```go
// ListByPlayerSession returns all non-expired sessions whose
// PlayerSessionID matches any of the given IDs.
func (m *MemStore) ListByPlayerSession(_ context.Context, playerSessionIDs []ulid.ULID) ([]*Info, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()

    want := make(map[ulid.ULID]struct{}, len(playerSessionIDs))
    for _, id := range playerSessionIDs {
        want[id] = struct{}{}
    }

    var result []*Info
    for _, info := range m.sessions {
        if info.Status == StatusExpired {
            continue
        }
        if _, ok := want[info.PlayerSessionID]; ok {
            result = append(result, copyInfo(info))
        }
    }
    return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -timeout 30s -run TestMemStoreListByPlayerSession ./internal/session/`
Expected: PASS.

- [ ] **Step 5: Implement in Postgres store**

Edit `internal/store/session_store.go`. Add the method:

```go
// ListByPlayerSession returns all non-expired sessions whose
// PlayerSessionID is in the given set.
func (s *PostgresSessionStore) ListByPlayerSession(
    ctx context.Context,
    playerSessionIDs []ulid.ULID,
) ([]*session.Info, error) {
    if len(playerSessionIDs) == 0 {
        return nil, nil
    }

    // Convert ULIDs to []interface{} for pgx parameter expansion.
    args := make([]any, len(playerSessionIDs))
    for i, id := range playerSessionIDs {
        args[i] = id.String()
    }

    // Build a $1,$2,... parameter list.
    placeholders := make([]string, len(args))
    for i := range args {
        placeholders[i] = fmt.Sprintf("$%d", i+1)
    }

    query := fmt.Sprintf(`
        SELECT %s
        FROM sessions
        WHERE player_session_id IN (%s)
          AND status != 'expired'
    `, sessionSelectColumns, strings.Join(placeholders, ","))
    // sessionSelectColumns is the existing SELECT column list — reuse the
    // same constant/string the other List* methods use.

    rows, err := s.pool.Query(ctx, query, args...)
    if err != nil {
        return nil, oops.Code("LIST_BY_PLAYER_SESSION_FAILED").Wrap(err)
    }
    defer rows.Close()

    var result []*session.Info
    for rows.Next() {
        info, scanErr := scanSessionRow(rows)
        if scanErr != nil {
            return nil, oops.Code("LIST_BY_PLAYER_SESSION_SCAN_FAILED").Wrap(scanErr)
        }
        result = append(result, info)
    }
    if err := rows.Err(); err != nil {
        return nil, oops.Code("LIST_BY_PLAYER_SESSION_ROWS_ERR").Wrap(err)
    }
    return result, nil
}
```

Note: if `sessionSelectColumns` or `scanSessionRow` don't exist under those exact names, use the helpers the other `ListBy*` methods in the same file use. Inspect the existing code and follow the established pattern.

- [ ] **Step 6: Integration test for Postgres**

Add to `internal/store/session_store_integration_test.go`:

```go
//go:build integration

func TestPostgresSessionStoreListByPlayerSessionFiltersCorrectly(t *testing.T) {
    // Follow the existing integration test setup pattern in this file
    // (testcontainers, apply migrations, construct store).
    ctx := context.Background()
    store := setupPostgresSessionStore(t)

    ps1 := ulid.Make()
    ps2 := ulid.Make()

    // Insert a PlayerSession row for each (the FK requires it).
    insertPlayerSession(t, ps1)
    insertPlayerSession(t, ps2)

    // Insert 3 game sessions: 2 under ps1, 1 under ps2.
    require.NoError(t, store.Set(ctx, "s1", &session.Info{
        ID: "s1", CharacterID: ulid.Make(), PlayerSessionID: ps1,
        Status: session.StatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now(),
    }))
    require.NoError(t, store.Set(ctx, "s2", &session.Info{
        ID: "s2", CharacterID: ulid.Make(), PlayerSessionID: ps1,
        Status: session.StatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now(),
    }))
    require.NoError(t, store.Set(ctx, "s3", &session.Info{
        ID: "s3", CharacterID: ulid.Make(), PlayerSessionID: ps2,
        Status: session.StatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now(),
    }))

    got, err := store.ListByPlayerSession(ctx, []ulid.ULID{ps1})
    require.NoError(t, err)
    assert.Len(t, got, 2)
}
```

- [ ] **Step 7: Regenerate mocks**

Run: `mockery`

- [ ] **Step 8: Build, test, commit**

```bash
task lint
task test -- -timeout 60s ./internal/session/ ./internal/store/
task test:int -- -ginkgo.focus="ListByPlayerSession" ./internal/store/
# If clean:
jj commit -m "feat(session): ListByPlayerSession store method (9es6.9)

Adds Store.ListByPlayerSession for enumerating active/detached game
sessions by their parent PlayerSession IDs. Used by logout and
PlayerSession eviction fanout to identify child game sessions that
need session_ended emission before their rows cascade-delete.

Per Design Decision #13: a targeted query matches the existing
ListBy* scoping patterns, scales via an indexed WHERE IN (...)
rather than scanning all player sessions, and keeps cap-enforcement
code readable.

Part of holomush-9es6.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 10: Logout fanout

**Goal:** Before invoking `authService.Logout` (which deletes the PlayerSession and cascades to game sessions), enumerate child game sessions and emit `session_ended` for each.

**Files:**

- Modify: `internal/grpc/auth_handlers.go` (Logout RPC handler around line 441)
- Modify: `internal/grpc/server_test.go` (logout handler test)

**Dependencies:** Task 9

- [ ] **Step 1: Write the failing test**

Add to `internal/grpc/server_test.go` (or a dedicated `auth_handlers_test.go` if one exists):

```go
func TestLogoutEmitsSessionEndedForEachChildGameSession(t *testing.T) {
    // Harness with 2 active game sessions under the same PlayerSession.
    h := newLogoutFanoutHarness(t, 2 /* child sessions */)

    _, err := h.server.Logout(ctx, &corev1.LogoutRequest{
        PlayerSessionToken: h.token,
    })
    require.NoError(t, err)

    // Assert 2 session_ended events, one per child session, both cause=logout.
    appended := h.eventStore.AppendedByType(core.EventTypeSessionEnded)
    assert.Len(t, appended, 2)

    seen := make(map[string]bool)
    for _, e := range appended {
        var p core.SessionEndedPayload
        require.NoError(t, json.Unmarshal(e.Payload, &p))
        assert.Equal(t, core.SessionEndedCauseLogout, p.Cause)
        seen[p.SessionID] = true
    }
    for _, sid := range h.childSessionIDs {
        assert.True(t, seen[sid], "session %s not in logout fanout", sid)
    }

    // And: the PlayerSession row is gone (authService.Logout was called).
    _, findErr := h.playerSessionRepo.GetByTokenHash(ctx, auth.HashSessionToken(h.token))
    assert.ErrorIs(t, findErr, auth.ErrNotFound)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -timeout 30s -run TestLogoutEmitsSessionEnded ./internal/grpc/`
Expected: FAIL (no session_ended events).

- [ ] **Step 3: Implement fanout in Logout RPC**

Edit `internal/grpc/auth_handlers.go`. The current Logout RPC resolves the PlayerSession, hashes the token, and calls `authService.Logout`. Before that final call, add the fanout:

```go
func (s *CoreServer) Logout(ctx context.Context, req *corev1.LogoutRequest) (*corev1.LogoutResponse, error) {
    tokenHash := auth.HashSessionToken(req.PlayerSessionToken)
    slog.DebugContext(ctx, "grpc: Logout", "token_hash_prefix", tokenHash[:16])

    if s.authService == nil {
        return nil, oops.Code("NOT_CONFIGURED").Errorf("auth service not configured")
    }

    // Resolve the PlayerSession so we can fan out to its children.
    ps, err := s.playerSessionRepo.GetByTokenHash(ctx, tokenHash)
    if err != nil {
        // If the PS isn't found we still let authService.Logout handle the
        // error path — it already produces SESSION_NOT_FOUND.
        if _, logoutErr := s.authService.Logout(ctx, tokenHash); logoutErr != nil {
            return nil, oops.Code("LOGOUT_FAILED").Wrap(logoutErr)
        }
        return &corev1.LogoutResponse{}, nil
    }

    // Fanout: emit session_ended on each child game session BEFORE deleting
    // the PlayerSession. Ordering guarantees the Subscribe stream receives
    // STREAM_CLOSED with reason rather than orphaning on ctx cancel.
    childSessions, listErr := s.sessionStore.ListByPlayerSession(ctx, []ulid.ULID{ps.ID})
    if listErr != nil {
        slog.WarnContext(ctx, "logout: list child sessions failed — proceeding without fanout",
            "player_session_id", ps.ID.String(), "error", listErr)
    }
    for _, info := range childSessions {
        char := core.CharacterRef{
            ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID,
        }
        if dcErr := s.engine.HandleDisconnect(ctx, char, "logout"); dcErr != nil {
            slog.WarnContext(ctx, "logout: leave event failed",
                "session_id", info.ID, "error", dcErr)
        }
        if endErr := s.engine.EndSession(ctx, char, info.ID,
            core.SessionEndedCauseLogout, "Session ended by logout."); endErr != nil {
            slog.WarnContext(ctx, "logout: session_ended event failed",
                "session_id", info.ID, "error", endErr)
        }
        if delErr := s.sessionStore.Delete(ctx, info.ID); delErr != nil {
            slog.WarnContext(ctx, "logout: game session delete failed",
                "session_id", info.ID, "error", delErr)
        }
        s.runDisconnectHooks(ctx, *info)
    }

    // Now delete the PlayerSession itself.
    if _, logoutErr := s.authService.Logout(ctx, tokenHash); logoutErr != nil {
        return nil, oops.Code("LOGOUT_FAILED").
            With("token_hash_prefix", tokenHash[:16]).
            Wrap(logoutErr)
    }

    return &corev1.LogoutResponse{}, nil
}
```

Note: the `PlayerSession` needs an `ID` field that matches `session.Info.PlayerSessionID`'s type (`ulid.ULID`). If the field exists with a different name, adjust.

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -timeout 30s -run TestLogoutEmitsSessionEnded ./internal/grpc/`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

```bash
task lint && jj commit -m "feat(grpc): Logout fanout — emit session_ended per child session (9es6.10)

Before invoking authService.Logout (which deletes the PlayerSession
row and cascades to game sessions), enumerate child game sessions via
sessionStore.ListByPlayerSession and emit HandleDisconnect +
EndSession(cause=logout) + Delete + hooks for each.

Closes the 'orphaned Subscribe on logout' gap the spec's § Logout &
eviction fanout documents. Ordering invariant: per-session signals
complete before PlayerSession deletion so ValidateSessionOwnership
(which runs at Subscribe open time) doesn't flap mid-fanout.

Part of holomush-9es6.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 11: PlayerSession eviction fanout

**Goal:** When `AuthenticatePlayer`'s `CreateWithCap` evicts oldest PlayerSessions to maintain the cap, emit `session_ended` on each evicted PlayerSession's child game sessions before the cascade-delete removes them.

**Note on complexity:** `CreateWithCap` currently returns only a count of trimmed rows. To know WHICH PlayerSessions got trimmed, the signature must change to return the trimmed IDs. The enumeration of child game sessions must happen BEFORE the TX runs — otherwise the cascade deletes them before we can emit events.

**Files:**

- Modify: `internal/auth/player_session.go` (or wherever `CreateWithCap` is defined) — change signature
- Modify: `internal/auth/repository.go` (repository interface) — change signature
- Modify: `internal/auth/auth_service.go` — implement eviction fanout
- Modify: `internal/auth/auth_service_test.go` — update mock expectations
- Modify: `internal/auth/mocks/*` — regenerated

**Dependencies:** Task 9

- [ ] **Step 1: Write the failing test**

Add to `internal/auth/auth_service_test.go`:

```go
func TestAuthenticatePlayerEmitsSessionEndedForEvictedSessionsChildren(t *testing.T) {
    ctx := context.Background()
    const capN = 2

    svc, playerRepo, sessionRepo, gameStore, engine, hasher := newAuthServiceWithGameFanout(t, capN)
    player := testPlayerWithCredentials(t, playerRepo, hasher, "alice")

    // Pre-populate 2 existing PlayerSessions at cap, and 2 child game sessions
    // (one under each PlayerSession).
    oldestPS := seedPlayerSessionWithGameSession(t, sessionRepo, gameStore, player.ID, "gsOldest")
    _ = seedPlayerSessionWithGameSession(t, sessionRepo, gameStore, player.ID, "gsRecent")

    // A 3rd login triggers eviction of the oldest PS + its child game session.
    sessionRepo.On("CreateWithCap", ctx, mock.AnythingOfType("*auth.PlayerSession"), capN).
        Return([]ulid.ULID{oldestPS.ID}, nil).Once()

    tok, _, err := svc.AuthenticatePlayer(ctx, "alice", "password", "ua", "ip")
    require.NoError(t, err)
    assert.NotEmpty(t, tok)

    // The evicted PlayerSession's child game session should have a
    // session_ended event with cause=evicted.
    appended := engine.AppendedByType(core.EventTypeSessionEnded)
    require.Len(t, appended, 1)
    var p core.SessionEndedPayload
    require.NoError(t, json.Unmarshal(appended[0].Payload, &p))
    assert.Equal(t, "gsOldest", p.SessionID)
    assert.Equal(t, core.SessionEndedCauseEvicted, p.Cause)
}
```

Helper `newAuthServiceWithGameFanout` wires the auth service with an `engine` (capturing event store) and a game `sessionStore` with a seeded FK-consistent fixture. Follow existing test helper patterns.

- [ ] **Step 2: Run test to verify it fails**

Expected: FAIL (no session_ended in the capturing store; possibly compile failure because `CreateWithCap` doesn't yet return `[]ulid.ULID`).

- [ ] **Step 3: Change CreateWithCap signature**

Find `CreateWithCap` in `internal/auth/player_session.go` (or the repository impl — grep `func.*CreateWithCap`). Change the signature:

```go
// BEFORE:
// CreateWithCap(ctx context.Context, session *PlayerSession, cap int) (trimmed int, err error)

// AFTER:
// CreateWithCap atomically inserts session and evicts the oldest rows
// to keep the player's active session count at cap. Returns the ULIDs
// of the PlayerSessions that were trimmed (empty slice if none).
CreateWithCap(ctx context.Context, session *PlayerSession, cap int) (trimmedPlayerSessionIDs []ulid.ULID, err error)
```

Update the Postgres implementation: the existing SQL likely does `DELETE ... WHERE id IN (SELECT oldest)`. Change to `DELETE ... RETURNING id` and scan the returned IDs into a slice.

- [ ] **Step 4: Inject engine + sessionStore into auth.Service**

The auth `Service` needs `engine.EndSession` and `sessionStore.ListByPlayerSession` to do the fanout. Add them as dependencies:

```go
// In internal/auth/auth_service.go:

type Service struct {
    // ... existing fields ...
    engine       *core.Engine
    gameSessions session.Store
}

// New constructor option:
func WithGameSessionFanout(engine *core.Engine, gameSessions session.Store) ServiceOption {
    return func(s *Service) {
        s.engine = engine
        s.gameSessions = gameSessions
    }
}
```

Wire in `cmd/holomush/sub_grpc.go` wherever the auth Service is constructed.

- [ ] **Step 5: Implement eviction fanout in AuthenticatePlayer**

Modify `internal/auth/auth_service.go`'s `AuthenticatePlayer` around lines 133–146:

```go
trimmedIDs, err := s.playerSessions.CreateWithCap(ctx, session, s.maxSessionsPerPlayer)
if err != nil {
    return "", nil, oops.Code("AUTH_LOGIN_FAILED").
        With("operation", "persist player session with cap").
        Wrap(err)
}
if len(trimmedIDs) > 0 {
    s.logger.InfoContext(ctx, "session cap trimmed oldest sessions",
        "event", "session_cap_trimmed",
        "player_id", player.ID.String(),
        "trimmed_count", len(trimmedIDs),
        "cap", s.maxSessionsPerPlayer,
    )

    // Eviction fanout: the child game sessions have ALREADY been removed
    // by FK cascade inside CreateWithCap's TX. We emit session_ended for
    // audit + Subscriber STREAM_CLOSED by enumerating what we know from
    // memory (child sessions we seeded before CreateWithCap ran). Since
    // cascade already fired, ListByPlayerSession would return empty here.
    //
    // Strategy: capture the snapshot BEFORE CreateWithCap, not after.
    // See refactor below.
}
```

Since cascade fires inside the TX, the correct pattern is:

```go
// NEW: Before CreateWithCap, snapshot child game sessions for the PlayerSessions
// that MAY be trimmed (all active PSs for this player). We emit session_ended
// only for the actually-trimmed subset returned by CreateWithCap.

// Option 1 (simpler, slight over-fetch): enumerate all active PS for this
// player and their child game sessions, then filter after CreateWithCap.
if s.engine != nil && s.gameSessions != nil && s.maxSessionsPerPlayer > 0 {
    activePSs, listErr := s.playerSessions.ListActiveByPlayer(ctx, player.ID) // assumes this exists; adapt
    if listErr == nil && len(activePSs) >= s.maxSessionsPerPlayer {
        psIDs := make([]ulid.ULID, len(activePSs))
        for i, ps := range activePSs {
            psIDs[i] = ps.ID
        }
        candidateChildren, _ := s.gameSessions.ListByPlayerSession(ctx, psIDs)

        // Now run CreateWithCap which returns trimmed PS IDs.
        trimmedIDs, err := s.playerSessions.CreateWithCap(ctx, session, s.maxSessionsPerPlayer)
        if err != nil { ... }

        // Build a set of trimmed PS IDs for filter.
        trimmed := make(map[ulid.ULID]struct{}, len(trimmedIDs))
        for _, id := range trimmedIDs {
            trimmed[id] = struct{}{}
        }

        // Emit session_ended for children of trimmed PSs.
        for _, child := range candidateChildren {
            if _, isTrimmed := trimmed[child.PlayerSessionID]; !isTrimmed {
                continue
            }
            char := core.CharacterRef{
                ID: child.CharacterID, Name: child.CharacterName, LocationID: child.LocationID,
            }
            if endErr := s.engine.EndSession(ctx, char, child.ID,
                core.SessionEndedCauseEvicted,
                "Session evicted — you logged in elsewhere."); endErr != nil {
                s.logger.WarnContext(ctx, "eviction: session_ended failed",
                    "session_id", child.ID, "error", endErr)
            }
        }
        return rawToken, player, nil
    }
}

// Fallback / non-cap path — just CreateWithCap without fanout.
trimmedIDs, err := s.playerSessions.CreateWithCap(ctx, session, s.maxSessionsPerPlayer)
if err != nil { ... }
```

**NOTE:** this refactor is significant and the exact API of `ListActiveByPlayer` on the `PlayerSessionRepository` may not exist — inspect the interface and add if needed (follow pattern of existing `ListByPlayer` variants).

**Race note (acknowledged in spec Design Decision #10):** there's a TOCTOU window between the candidate snapshot and `CreateWithCap`'s atomic TX. A concurrent login could evict different sessions. In that case, some candidate children we enumerated won't actually be evicted — the `trimmed` filter correctly skips them. Conversely, sessions evicted that weren't in our snapshot go un-signalled this cycle. This is a best-effort audit trail; the FK cascade still cleans up state correctly. The alternative (pre-cascade SELECT in the TX itself) requires SQL changes out of scope for this task.

- [ ] **Step 6: Regenerate mocks, run tests**

```bash
mockery
task test -- -timeout 60s ./internal/auth/...
```

Expected: the new test and existing tests PASS (update existing mock expectations for `CreateWithCap` to return `[]ulid.ULID, error` instead of `int, error`).

- [ ] **Step 7: Wire auth.Service with fanout dependencies**

Edit `cmd/holomush/sub_grpc.go`. When constructing the auth.Service, pass the engine + game session store:

```go
authService = auth.NewService(
    // ... existing deps ...
    auth.WithGameSessionFanout(engine, sessionStore),
)
```

- [ ] **Step 8: Lint + commit**

```bash
task lint && jj commit -m "feat(auth): PlayerSession eviction fanout — emit session_ended (9es6.11)

Changes CreateWithCap signature to return the trimmed PlayerSession
IDs (was: trimmed count). In AuthenticatePlayer, snapshot candidate
child game sessions before CreateWithCap, then emit session_ended
(cause=evicted) for children of actually-trimmed PlayerSessions.

Race acknowledged (Design Decision #10): TOCTOU window between
snapshot and atomic TX means some candidates may not be evicted
(filter skips them) and some evicted sessions may not be in our
snapshot (unsignalled this cycle, FK cascade still cleans up
state). Best-effort audit trail.

Part of holomush-9es6.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 12: disconnect telnet command

**Goal:** Add a `disconnect` command word to the telnet dispatcher that maps to the existing `Disconnect(connID)` RPC. Zero new server primitives — pure glue.

**Files:**

- Modify: `internal/telnet/gateway_handler.go` (dispatcher at `processLine`, around lines 245–262)
- Modify: `internal/telnet/gateway_handler_test.go` (unit test)

**Dependencies:** Task 7

- [ ] **Step 1: Write the failing test**

Add to `internal/telnet/gateway_handler_test.go`:

```go
func TestGatewayHandlerDisconnectCommandInvokesDisconnectRPC(t *testing.T) {
    // Harness: stub CoreClient that captures Disconnect calls; telnet handler
    // in authed+selectMode=false state with a known connectionID.
    h := newAuthedGatewayHandler(t)

    h.sendFromClient("disconnect\n")

    // Disconnect RPC should be called with this session's connectionID.
    require.Eventually(t, func() bool {
        return len(h.client.DisconnectCalls()) == 1
    }, time.Second, 10*time.Millisecond)

    call := h.client.DisconnectCalls()[0]
    assert.Equal(t, h.sessionID, call.SessionId)
    assert.Equal(t, h.connectionID, call.ConnectionId)
    assert.Equal(t, h.playerSessionToken, call.PlayerSessionToken)

    // Server wrote a brief confirmation to the wire.
    line := h.readWireLine()
    assert.Contains(t, line, "Disconnected")

    // Handler exited cleanly (wire closed, no hang).
    require.Eventually(t, func() bool {
        return h.isHandlerExited()
    }, 2*time.Second, 10*time.Millisecond)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -timeout 30s -run TestGatewayHandlerDisconnectCommand ./internal/telnet/`
Expected: FAIL.

- [ ] **Step 3: Add the command to processLine dispatcher**

Edit `internal/telnet/gateway_handler.go` around lines 245–262. In the `switch cmd { ... }` block:

```go
switch cmd {
case "connect":
    return h.handleConnect(ctx, arg)
case "say":
    h.handleSay(ctx, arg)
case "pose":
    h.handlePose(ctx, arg)
case "quit":
    h.handleQuit(ctx)
case "logout":
    h.handleLogout(ctx)
case "disconnect":  // NEW
    h.handleDisconnect(ctx)
default:
    if cmd != "" {
        h.handleGenericCommand(ctx, cmd, arg)
    }
}
```

Then add the handler method (alongside `handleQuit` / `handleLogout`):

```go
// handleDisconnect closes this wire without ending the session. Other
// surfaces subscribed to the same session remain active. Pure glue over
// the existing Disconnect RPC — no new server primitives.
//
// See docs/superpowers/specs/2026-04-18-session-lifecycle-as-events-design.md
// § The `disconnect` telnet command.
func (h *GatewayHandler) handleDisconnect(ctx context.Context) {
    if !h.authed || h.sessionID == "" || h.connectionID == "" {
        h.send("You are not currently connected to a character.")
        return
    }

    rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
    defer cancel()

    _, err := h.client.Disconnect(rpcCtx, &corev1.DisconnectRequest{
        SessionId:          h.sessionID,
        ConnectionId:       h.connectionID,
        PlayerSessionToken: h.playerSessionToken,
    })
    if err != nil {
        slog.WarnContext(ctx, "gateway: disconnect RPC failed",
            "session_id", h.sessionID, "error", err)
    }

    h.send("Disconnected. Other surfaces remain active.")

    // Exit the handler: we're done with this wire. The session (and other
    // connections) stay alive server-side.
    h.quitting = true
    h.loggingOut = true  // skip the "return to character picker" branch
}
```

Setting both `quitting` and `loggingOut` uses the existing post-quit plumbing to cleanly exit the handler without returning to the character picker — see the main loop check around `gateway_handler.go:174`.

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -timeout 30s -run TestGatewayHandlerDisconnectCommand ./internal/telnet/`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

```bash
task lint && jj commit -m "feat(telnet): add 'disconnect' command (9es6.12)

Adds the 'disconnect' command word to the telnet dispatcher. Maps
to the existing Disconnect(connID) RPC — zero new server primitives.

Closes this wire only; the session and other surfaces (web-terminal,
rich-web-ui) remain active. Uses the existing quitting+loggingOut
flags to exit the handler without returning to the character picker.

Per Design Decision's 'three-axis' model: disconnect is the per-surface
operation; quit is per-session; logout is per-player.

Part of holomush-9es6.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 13: Reaper OnExpired callback emits session_ended

**Goal:** Update the reaper's `OnExpired` callback to emit `session_ended` (cause=reaped) in addition to `HandleDisconnect`.

**Files:**

- Modify: `cmd/holomush/sub_grpc.go` (reaper config around lines 285-301)
- Modify: `cmd/holomush/core_test.go` or a new test file — add coverage if the existing harness allows

**Dependencies:** Task 2

- [ ] **Step 1: Update the reaper callback**

Edit `cmd/holomush/sub_grpc.go` lines 285-301:

```go
s.sessionReaper = session.NewReaper(sessionStore, session.ReaperConfig{
    Interval: s.cfg.ReaperInterval,
    OnExpired: func(info *session.Info) {
        char := core.CharacterRef{
            ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID,
        }
        if dcErr := engine.HandleDisconnect(reaperCtx, char, "session expired"); dcErr != nil {
            slog.Warn("reaper: leave event failed",
                "session_id", info.ID, "error", dcErr)
        }
        // NEW: emit session_ended for audit trail (detached sessions have no
        // live Subscribe so no client receives STREAM_CLOSED, but the event
        // persists for Query Stream History).
        if endErr := engine.EndSession(reaperCtx, char, info.ID,
            core.SessionEndedCauseReaped,
            "Session expired due to inactivity."); endErr != nil {
            slog.Warn("reaper: session_ended event failed",
                "session_id", info.ID, "error", endErr)
        }
        if info.IsGuest {
            guestAuth.ReleaseGuest(info.CharacterName)
        }
    },
})
```

Note: `reaperCtx` is a long-lived `context.Background()` derivative (see `sub_grpc.go:282`), so the ctx-decoupling requirement is already satisfied at this site.

- [ ] **Step 2: Test**

Run: `task test -- -timeout 60s ./cmd/holomush/...`
Expected: existing tests PASS; integration coverage for the reaper path will come via Task 14's integration suite.

- [ ] **Step 3: Lint + commit**

```bash
task lint && jj commit -m "feat(reaper): emit session_ended on expired session (9es6.13)

Updates cmd/holomush/sub_grpc.go's ReaperConfig.OnExpired callback
to call engine.EndSession(cause=reaped) after HandleDisconnect.

Detached sessions by definition have no live Subscribe, so the event
goes unheard in real time — but it persists in the event store for
audit via Query Stream History, closing the 'silently orphaned
reaped sessions' gap documented in the spec.

reaperCtx is background-scoped so ctx-decoupling discipline is
already satisfied.

Part of holomush-9es6.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 14: Integration test suite + ABAC verification + PR gate

**Goal:** Build the full integration test suite for session lifecycle events. Verify ABAC on `session_ended` reads. Run `task pr-prep`.

**Files:**

- Create: `test/integration/session_lifecycle/session_lifecycle_suite_test.go`
- Create: `test/integration/session_lifecycle/quit_regression_integration_test.go`
- Create: `test/integration/session_lifecycle/fanout_integration_test.go`
- Create: `test/integration/session_lifecycle/logout_eviction_integration_test.go`
- Create: `test/integration/session_lifecycle/ordering_integration_test.go`
- Modify: `test/integration/telnet/e2e_test.go` — delete or rename the obsolete flaky test at line 548
- Modify: `docs/superpowers/plans/2026-04-18-session-lifecycle-as-events.md` — append ABAC verification paragraph

**Dependencies:** Tasks 1–13

- [ ] **Step 1: Ginkgo suite bootstrap**

Create `test/integration/session_lifecycle/session_lifecycle_suite_test.go`. Use `test/integration/telnet/e2e_suite_test.go` as the template. It will need:

- testcontainers Postgres setup
- engine + event writer construction (with `WithProductionGuardrail`)
- session store (Postgres-backed)
- a test gRPC server + client harness (reuse existing patterns where possible)

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package session_lifecycle_test

import (
    "testing"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
)

func TestSessionLifecycleIntegration(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "Session Lifecycle as Events E2E Suite")
}
```

- [ ] **Step 2: Flake-proof quit regression test**

Create `test/integration/session_lifecycle/quit_regression_integration_test.go`:

```go
//go:build integration

package session_lifecycle_test

import (
    "strings"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    "github.com/holomush/holomush/internal/core"
)

var _ = Describe("Quit Regression (holomush-9es6)", func() {
    var env *testEnv

    BeforeEach(func() {
        env = setupTestEnv()
    })
    AfterEach(func() {
        env.teardown()
    })

    It("delivers Goodbye! reliably on quit without falling through to character list", func() {
        client := env.newTelnetClient()
        charName := connectAsGuest(client)

        client.SendLine("quit")

        // Assertion A: Goodbye! arrives within 5s.
        var allLines []string
        gotGoodbye := false
        deadline := time.Now().Add(5 * time.Second)
        for time.Now().Before(deadline) && !gotGoodbye {
            line, ok := client.ReadLineOrTimeout(500 * time.Millisecond)
            if !ok {
                continue
            }
            allLines = append(allLines, line)
            if strings.Contains(line, "Goodbye!") {
                gotGoodbye = true
            }
        }
        Expect(gotGoodbye).To(BeTrue(),
            "Goodbye! MUST be received on quit; got lines: %v", allLines)

        // Assertion B: negative — no "Your characters:" prompt leaks through.
        for _, line := range allLines {
            Expect(line).NotTo(ContainSubstring("Your characters:"),
                "character-selection prompt MUST NOT appear on the quit wire "+
                "(specific symptom of the pre-9es6 race)")
        }

        // Assertion C: exactly one session_ended event persisted.
        events := env.queryCharacterStream(charName)
        var sessionEndedEvents []core.Event
        for _, e := range events {
            if e.Type == core.EventTypeSessionEnded {
                sessionEndedEvents = append(sessionEndedEvents, e)
            }
        }
        Expect(sessionEndedEvents).To(HaveLen(1))
        // Further payload checks: cause=quit, reason=Goodbye!, matching SessionID
    })

    It("replay of prior session_ended does NOT terminate a new Subscribe", func() {
        // Registered player connects, quits, reconnects.
        player := env.newRegisteredPlayer("bob")

        client1 := env.newTelnetClient()
        client1.ConnectAs(player)
        client1.SelectCharacter(player.Character)

        client1.SendLine("quit")
        client1.ExpectLine("Goodbye!")
        client1.ExpectClosed()

        // Reconnect — new session for the same character.
        client2 := env.newTelnetClient()
        client2.ConnectAs(player)
        client2.SelectCharacter(player.Character)

        // The new Subscribe replays the character stream history, which
        // INCLUDES the prior session's session_ended. The filter on payload
        // SessionID != info.ID MUST prevent self-termination.
        //
        // Test via a positive behavior: send a command after reconnect and
        // observe that the stream is still open.
        client2.SendLine("say hello again")
        client2.ExpectLine(ContainSubstring(`says, "hello again"`))
    })

    It("persists session_ended even when client ctx cancels mid-quit", func() {
        // Connect, quit, immediately close the wire — verify the event store
        // still has the session_ended event.
        client := env.newTelnetClient()
        charName := connectAsGuest(client)

        client.SendLine("quit")
        client.Close() // drop TCP mid-flight

        // Wait for the server-side flush.
        Eventually(func() int {
            events := env.queryCharacterStream(charName)
            count := 0
            for _, e := range events {
                if e.Type == core.EventTypeSessionEnded {
                    count++
                }
            }
            return count
        }, 5*time.Second, 100*time.Millisecond).Should(Equal(1),
            "session_ended MUST persist even when client ctx cancels mid-quit")
    })
})
```

- [ ] **Step 3: Fanout + disconnect isolation tests**

Create `test/integration/session_lifecycle/fanout_integration_test.go` with:

- "two Subscribes on same sessionID both receive STREAM_CLOSED on quit"
- "disconnect from one connection does NOT emit session_ended; other connection continues"

- [ ] **Step 4: Logout + eviction + reaper tests**

Create `test/integration/session_lifecycle/logout_eviction_integration_test.go` with:

- "logout emits session_ended for each child game session"
- "11th login evicts oldest PS → that PS's child game session emits session_ended (cause=evicted)"
- "detached session reaped → session_ended persists with cause=reaped (no live Subscribe)"

- [ ] **Step 5: Cross-stream ordering test**

Create `test/integration/session_lifecycle/ordering_integration_test.go` with a test analogous to `internal/store/postgres_integration_test.go:575`:

- Append say → leave → session_ended across character + location streams
- Open a fresh Subscribe
- Assert the merge-sort replay delivers in append order, with session_ended as the terminal event

- [ ] **Step 6: Delete obsolete flaky test**

Edit `test/integration/telnet/e2e_test.go` around line 548. Delete the `"Player A disconnects cleanly via quit"` test — its replacement is in the new `session_lifecycle` suite.

- [ ] **Step 7: Verify ABAC on session_ended reads**

Open `internal/grpc/server.go`'s `QueryStreamHistory` RPC implementation (search for `QueryStreamHistory`). Read its authorization logic. Verify that `character:{ID}` stream reads require the caller to own the character (existing ABAC). Confirm that `session_ended` events are naturally scoped to the owner.

Append a paragraph to the plan's end:

```markdown
### ABAC verification (Design Decision #11)

Confirmed during Task 14: `QueryStreamHistory` enforces owner-only
reads on character streams via [the authorization check in <file>:<line>].
`session_ended` events inherit this scope without any new policy
addition.
```

- [ ] **Step 8: Run the full PR gate**

```bash
task pr-prep
```

Expected: green. If any test fails, return to the relevant task and fix — do NOT add retries, relaxed timeouts, or client-side fallbacks.

- [ ] **Step 9: Final commit**

```bash
task lint && jj commit -m "test(session-lifecycle): integration suite + ABAC verification (9es6.14)

Adds the Ginkgo/Gomega integration suite that exercises every
session-terminating path end-to-end:

  * Quit regression: Goodbye! delivered reliably, no 'Your characters:'
    leak, session_ended persisted
  * Replay isolation: prior session_ended does not self-terminate new
    Subscribe
  * Ctx cancel during quit: event still persists
  * Multi-surface fanout: two Subscribes both receive STREAM_CLOSED
  * Disconnect isolation: single-wire close does not emit session_ended
  * Logout fanout: all child game sessions get session_ended
  * Eviction fanout: oldest PS's child game session gets session_ended
  * Reaper audit: detached-session reap persists session_ended
  * Cross-stream ordering: I2 regression guard

Obsoletes and deletes the flaky telnet quit test at
test/integration/telnet/e2e_test.go:548 — its coverage is replaced
by the flake-proof regression test.

ABAC verification: Query Stream History inherits character-stream
owner-only scope; no policy change needed for session_ended reads.

Closes holomush-9es6.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Self-review checklist

Spec coverage:

- ✅ New event type + payload — Task 1
- ✅ engine.EndSession with decoupled ctx — Task 2
- ✅ EventWriter guardrail (Design Decision #8) — Task 3
- ✅ errStreamTerminated sentinel — Task 4
- ✅ sendAndCommitEvent match-and-terminate (Design Decision #3) — Task 5
- ✅ Subscribe live-loop sentinel + WatchSession removal — Task 6
- ✅ Delete signature change + dead code removal + mock regen — Task 7
- ✅ Quit + guest + admin-boot rewiring — Task 8
- ✅ ListByPlayerSession store method (Design Decision #13) — Task 9
- ✅ Logout fanout — Task 10
- ✅ PlayerSession eviction fanout with CreateWithCap signature change — Task 11
- ✅ disconnect telnet command — Task 12
- ✅ Reaper wiring — Task 13
- ✅ Full integration suite + ABAC verification + PR gate — Task 14

Load-bearing invariants:

- ✅ I1 (EventWriter serialization) — enforced at Task 3 via runtime guardrail
- ✅ I2 (cross-stream ordering) — regression test at Task 14
- ✅ I3 (one-character-one-session) — preserved throughout; replay isolation test at Task 14

Design decisions (all 13): each has a Task that implements it, or is a convention noted in the relevant task.

Residual open questions:

- ✅ #1 (admin boot insertion point) — resolved in Task 8 step 5 (code read required during execution)
- ✅ #2 (ABAC verification) — resolved in Task 14 step 7

Out-of-scope items: none leaked into tasks. Multi-surface quit confirmation and gateway-side stale session_ended filtering are explicitly deferred per the spec.

Placeholders: none. Every step has concrete code, exact file paths, and expected command output.

Type consistency:

- `SessionEndedPayload` fields (`SessionID`, `CharacterID`, `Cause`, `Reason`) consistent across Tasks 1, 2, 5, 8, 10, 11, 13, 14.
- Cause constants (`SessionEndedCauseQuit` etc.) used consistently.
- `engine.EndSession(ctx, char, sessionID, cause, reason)` signature consistent across Tasks 2, 8, 10, 11, 13.
- `CreateWithCap` returns `[]ulid.ULID` after Task 11 — test call sites updated in same task.
- `errStreamTerminated` sentinel name consistent across Tasks 4, 5, 6.
