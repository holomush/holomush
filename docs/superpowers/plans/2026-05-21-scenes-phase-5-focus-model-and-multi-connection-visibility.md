# Scenes Phase 5 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `dev-flow:subagent-driven-development` (recommended) or `dev-flow:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. **Always read the verbatim spec at [`docs/superpowers/specs/2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility-design.md`](../specs/2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility-design.md) before starting any task** — the plan is the *how*, the spec is the *what*.

**Goal:** Implement per-connection focus tracking and multi-connection visibility for scenes; close the v2 §11 plugin↔server focus-contract BLOCKER with three new PluginHostService RPCs and a substrate-internal per-Connection subscription routing extension.

**Architecture:** Per-Connection `FocusKey` field on `session.Connection`; substrate validates membership against `session.Info.FocusMemberships` (no host→plugin RPC); a single combined `SessionConnectionMutator` writes both `Connection.FocusKey` and `Info.PresentingFocus` atomically under one Store-lock acquisition; per-Connection JetStream filtering via `SessionStreamRegistry.SendToConnection`.

**Tech Stack:** Go (sessions, gRPC, focus coordinator); Postgres (per-row `FOR UPDATE` for `UpdateSessionConnection`); protobuf (3 new RPCs in `plugin/v1`); gopher-lua (3 hostfunc bindings); Ginkgo/Gomega (integration tests in `test/integration/scenes/`).

**Spec invariants pinned by this plan:** INV-P5-1 through INV-P5-14 (14 numbered invariants — see spec §10). Decision references D1-D11 throughout.

**Predecessor:** Phase 4 (`5rh.13`) merged 2026-05-21 (PR #4153 + #4156). All substrate prerequisites (scene IC/OOC dot-style emission, FocusMemberships, scope_floor.go scene branch, iwzt.15 filter-at-delivery) are in place.

**Follow-up bead filed (out of Phase 5 scope):** `holomush-3d9o` — in-band signal on reconnect-fallback.

---

## File Structure

Phase 5 touches three architectural layers. Files are grouped by layer with their per-file responsibility.

### Substrate layer

| File | Responsibility | Phase 5 change |
|---|---|---|
| `internal/store/migrations/000038_connection_focus_key.up.sql` (NEW) + `.down.sql` (NEW) | Adds `focus_key JSONB NULL` column to `session_connections`. Mirrors the `focus_memberships JSONB` precedent at `000006_session_focus.up.sql:9-13`. | Whole file. |
| `internal/session/session.go` | Connection struct + Store interface + mutator sentinel patterns. | Add `Connection.FocusKey *FocusKey` field; add `sessionConnectionMutatorSentinel` type + `SessionConnectionMutator` struct + `NewSessionConnectionMutator` constructor; extend `Store` interface with `UpdateSessionConnection` + `ListConnectionsBySession`. |
| `internal/session/memstore.go` | In-memory Store impl backing tests + dev workspaces. | Add `UpdateSessionConnection` + `ListConnectionsBySession` methods using existing `m.mu` `sync.RWMutex`. |
| `internal/store/session_store.go` | Postgres-backed Store impl. | Add `UpdateSessionConnection` (single tx; `sessions FOR UPDATE` THEN `session_connections FOR UPDATE` per D11) + `ListConnectionsBySession`. |
| `internal/grpc/stream_registry.go` | Session subscriber registration + broadcast. | Add `RegisterConnection(sessionID, connID, ch)` + `SendToConnection(sessionID, connID, update)` alongside existing session-wide `Send`. |
| `internal/grpc/focus/coordinator.go` | I-6 server-authoritative mutation. | Extend Coordinator interface with `SetConnectionFocus`, `AutoFocusOnJoin`, `IsAnyConnFocused`, `RestoreConnectionFocus`. |
| `internal/grpc/focus/subscription_router.go` (NEW) | Translates focus changes into per-Connection stream deltas. | Whole file. |
| `internal/grpc/server.go` (`CoreServer.Subscribe` handler) | Subscribe gRPC streaming endpoint. | Switch registration from `Register(sessionID, ch)` to `RegisterConnection(sessionID, connectionID, ch)`. |

### Plugin-facing API layer

| File | Responsibility | Phase 5 change |
|---|---|---|
| `api/proto/holomush/plugin/v1/plugin.proto` | PluginHostService RPC definitions. | Add 3 new RPCs (`SetConnectionFocus`, `AutoFocusOnJoin`, `IsAnyConnFocused`); add `FocusFailure` message + `FocusFailureReason` enum. |
| `internal/plugin/hostfunc/stdlib_focus.go` | Lua hostfunc registry for focus operations. | Add 3 Lua bindings paralleling the 3 new RPCs (INV-S3 parity per D6). |

### Plugin consumer

| File | Responsibility | Phase 5 change |
|---|---|---|
| `plugins/core-scenes/commands.go` | `scene <verb>` subcommand handlers. | Replace `:840` placeholder; implement `scene focus #<id>`, `scene grid`, `scene list` subcommands; extend `handleJoin` to call `AutoFocusOnJoin` after `JoinScene → JoinFocus`. |

### Tests

Integration tests land in `test/integration/scenes/` (Ginkgo suite established in Phase 4 T25/T26). Unit tests land alongside their implementation files. The INV-P5-8 meta-test goes in `internal/test/invariants/` (Phase 4 T28 placement).

---

## Conventions for all tasks

- **VCS**: this repo is `jj`-colocated. Each task ends with `jj describe -m "<message>"` then `jj new` for the next task's working copy. Never use `git commit` directly.
- **Test runner**: `task test -- ./<package>/...` for unit; `task test:int -- -run <TestName>` for integration. Never invoke `go test` directly (project rule).
- **License headers**: `task license:add` runs on commit hooks; new `.go` / `.sql` files need the SPDX-Apache-2.0 header. New tests under build tag `//go:build integration` for integration suite.
- **Error wrapping**: use `oops.Code("PHASE5_*").Wrap(err)` for new error codes; existing project pattern.
- **`oops.Code()` deepest-code rule**: outer wraps shadow inner codes; tests that assert via `errutil.AssertErrorCode` see the deepest. Be deliberate about which code you want to surface.

---

## Phase A — Substrate data model + Store (T1-T8)

### Task 1: Migration 000038 — `session_connections.focus_key` column

**Files:**

- Create: `internal/store/migrations/000038_connection_focus_key.up.sql`
- Create: `internal/store/migrations/000038_connection_focus_key.down.sql`
- Test: `internal/store/session_store_integration_test.go` (extend; verify column exists post-migration)

- [ ] **Step 1: Write the failing test**

Append to `internal/store/session_store_integration_test.go`:

```go
func TestSessionConnectionsHasFocusKeyColumn(t *testing.T) {
    t.Parallel()
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    pool := setupTestPool(t)
    defer pool.Close()

    var dataType string
    err := pool.QueryRow(ctx, `
        SELECT data_type FROM information_schema.columns
        WHERE table_name = 'session_connections' AND column_name = 'focus_key'
    `).Scan(&dataType)
    require.NoError(t, err)
    require.Equal(t, "jsonb", dataType,
        "INV-P5-2 substrate column: session_connections.focus_key MUST be JSONB nullable")
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
task test -- -run TestSessionConnectionsHasFocusKeyColumn ./internal/store/
```

Expected: FAIL — column does not exist.

- [ ] **Step 3: Write the migration**

Create `internal/store/migrations/000038_connection_focus_key.up.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Phase 5 (holomush-5rh.14): per-Connection focus pointer.
-- NULL = grid focus (default for new connections); JSONB-encoded
-- FocusKey when explicitly focused on a scene/channel/etc.
-- JSONB shape mirrors the focus_memberships precedent at
-- 000006_session_focus.up.sql:9-13.
ALTER TABLE session_connections
    ADD COLUMN IF NOT EXISTS focus_key JSONB NULL;
```

Create `internal/store/migrations/000038_connection_focus_key.down.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

ALTER TABLE session_connections DROP COLUMN IF EXISTS focus_key;
```

- [ ] **Step 4: Run test to verify it passes**

```bash
task test -- -run TestSessionConnectionsHasFocusKeyColumn ./internal/store/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(session-schema): add session_connections.focus_key column (5rh.14 T1)

Migration 000038 adds JSONB nullable focus_key column to
session_connections per Phase 5 spec §4.1. NULL = grid focus
(default); JSONB-encoded FocusKey when focused. Mirrors the
focus_memberships precedent at 000006_session_focus.up.sql:9-13.

Bead: holomush-5rh.14.1"
jj --no-pager new
```

---

### Task 2: `Connection.FocusKey` field

**Files:**

- Modify: `internal/session/session.go:194-201`
- Test: `internal/session/connection_test.go` (NEW)

- [ ] **Step 1: Write the failing test**

Create `internal/session/connection_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session

import (
    "testing"
    "time"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
)

func TestConnection_FocusKeyNilByDefault(t *testing.T) {
    t.Parallel()
    c := Connection{
        ID:          ulid.Make(),
        SessionID:   "sess-1",
        ClientType:  "terminal",
        ConnectedAt: time.Now(),
    }
    assert.Nil(t, c.FocusKey, "INV-P5-2: new Connection MUST default to nil FocusKey (= grid focus)")
}

func TestConnection_FocusKeyAcceptsSceneKey(t *testing.T) {
    t.Parallel()
    sceneID := ulid.Make()
    fk := &FocusKey{Kind: FocusKindScene, TargetID: sceneID}
    c := Connection{
        ID:         ulid.Make(),
        SessionID:  "sess-1",
        ClientType: "terminal",
        FocusKey:   fk,
    }
    assert.NotNil(t, c.FocusKey)
    assert.Equal(t, FocusKindScene, c.FocusKey.Kind)
    assert.Equal(t, sceneID, c.FocusKey.TargetID)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
task test -- -run TestConnection_FocusKey ./internal/session/
```

Expected: FAIL — `c.FocusKey` undefined.

- [ ] **Step 3: Add the field**

Modify `internal/session/session.go` at the `Connection` struct (current location `:194-201`):

```go
// Connection represents a single client attached to a session.
type Connection struct {
    ID          ulid.ULID
    SessionID   string
    ClientType  string   // "terminal", "comms_hub", "telnet"
    Streams     []string // event streams this connection subscribes to
    // FocusKey is the per-connection focus pointer (Phase 5, INV-P5-2).
    // nil = grid focus (default for new connections); non-nil = focused
    // on the named context. Mutated only via the Coordinator-invoked
    // SessionConnectionMutator (I-6 server-authoritative; INV-P5-7).
    FocusKey    *FocusKey
    ConnectedAt time.Time
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
task test -- -run TestConnection_FocusKey ./internal/session/
```

Expected: PASS (both subtests).

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(session): add Connection.FocusKey field for per-conn focus (5rh.14 T2)

Phase 5 INV-P5-2: each Connection carries an optional FocusKey
pointer; nil = grid focus (default). Mutation routed through the
new SessionConnectionMutator (T3) under I-6.

Bead: holomush-5rh.14.2"
jj --no-pager new
```

---

### Task 3: `SessionConnectionMutator` sentinel type

**Files:**

- Modify: `internal/session/session.go` (append after existing `FocusMutator` block, currently `:91-132`)
- Test: `internal/session/session_connection_mutator_test.go` (NEW)

- [ ] **Step 1: Write the failing test**

Create `internal/session/session_connection_mutator_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session

import (
    "errors"
    "testing"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// TestSessionConnectionMutator_OnlyConstructibleViaConstructor pins
// INV-P5-7: the sentinel pattern blocks direct struct construction
// from outside this package. The constructor is the sole legitimate
// path. This is a runtime check; a parallel compile-fail doc test
// in internal/grpc/focus enforces the actual at-rest invariant.
func TestSessionConnectionMutator_OnlyConstructibleViaConstructor(t *testing.T) {
    t.Parallel()
    fn := func(info Info, conn Connection) (Info, Connection, error) {
        return info, conn, nil
    }
    m := NewSessionConnectionMutator(fn)
    require.NotNil(t, m.Mutate, "constructor MUST populate the callback")
}

func TestSessionConnectionMutator_AppliesBothFields(t *testing.T) {
    t.Parallel()
    sceneID := ulid.Make()
    target := &FocusKey{Kind: FocusKindScene, TargetID: sceneID}

    m := NewSessionConnectionMutator(func(info Info, conn Connection) (Info, Connection, error) {
        conn.FocusKey = target
        info.PresentingFocus = target
        return info, conn, nil
    })

    info := Info{}
    conn := Connection{}
    nextInfo, nextConn, err := m.Mutate(info, conn)
    require.NoError(t, err)
    assert.Equal(t, target, nextInfo.PresentingFocus)
    assert.Equal(t, target, nextConn.FocusKey)
}

func TestSessionConnectionMutator_ErrorPropagates(t *testing.T) {
    t.Parallel()
    sentinelErr := errors.New("FOCUS_WITHOUT_MEMBERSHIP")
    m := NewSessionConnectionMutator(func(info Info, conn Connection) (Info, Connection, error) {
        return info, conn, sentinelErr
    })
    _, _, err := m.Mutate(Info{}, Connection{})
    require.ErrorIs(t, err, sentinelErr)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
task test -- -run TestSessionConnectionMutator ./internal/session/
```

Expected: FAIL — `SessionConnectionMutator` / `NewSessionConnectionMutator` undefined.

- [ ] **Step 3: Add the type + constructor**

Modify `internal/session/session.go` — append after the existing `NewFocusMutator` constructor (currently `:132`):

```go
// sessionConnectionMutatorSentinel is an unexported type that prevents
// construction of SessionConnectionMutator from outside the
// internal/grpc/focus package — same compile-time enforcement as
// focusMutatorSentinel at :91-93. Phase 5 (INV-P5-7) requires that
// per-Connection focus mutations route through the Coordinator alone.
type sessionConnectionMutatorSentinel struct{}

// SessionConnectionMutator atomically mutates BOTH Info (PresentingFocus
// + future session-scoped fields) AND a single Connection (FocusKey)
// under one Store-lock acquisition. Phase 5 introduced this in place
// of the separate FocusMutator + ConnectionMutator two-call pattern
// the round-2 reviewer flagged: that pattern admitted a torn-state
// observer window between the two locked sections. This single mutator
// closes that window (INV-P5-7).
//
// FocusMutator (above) retains its existing role for FocusMemberships /
// PresentingFocus-only mutations (LeaveFocus, JoinFocus). The two
// mutator types co-exist and have non-overlapping use cases.
type SessionConnectionMutator struct {
    _      sessionConnectionMutatorSentinel
    Mutate func(info Info, conn Connection) (nextInfo Info, nextConn Connection, err error)
}

// NewSessionConnectionMutator parallels NewFocusMutator. Callable from
// any package; the sentinel field's unexported type blocks direct struct
// literal construction outside session, so the constructor is the only
// reachable path. The "only grpc/focus is the legitimate caller" rule
// is enforced via a lint rule + compile-fail documentation test (see
// internal/grpc/focus/session_connection_mutator_doctest.go).
func NewSessionConnectionMutator(
    fn func(info Info, conn Connection) (nextInfo Info, nextConn Connection, err error),
) SessionConnectionMutator {
    return SessionConnectionMutator{Mutate: fn}
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
task test -- -run TestSessionConnectionMutator ./internal/session/
```

Expected: PASS (all 3 subtests).

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(session): SessionConnectionMutator sentinel for atomic dual-field write (5rh.14 T3)

Phase 5 D7 + INV-P5-7: introduces SessionConnectionMutator, a single
mutator type whose callback receives (Info, Connection) and returns
both updated. Replaces the two-mutator two-call pattern that admitted
a torn-state observer window. Sentinel-protected like FocusMutator at
session.go:91-93. Constructor is the only public construction path.

Bead: holomush-5rh.14.3"
jj --no-pager new
```

---

### Task 4: Store interface extension — `UpdateSessionConnection` + `ListConnectionsBySession`

**Files:**

- Modify: `internal/session/session.go::Store` interface (currently `:233+`)
- Test: deferred to T5-T8 (impl-specific tests)

- [ ] **Step 1: Add interface methods**

Modify `internal/session/session.go` — append to the `Store` interface (find the `type Store interface {` block at `:233+` and add these methods):

```go
    // GetConnection looks up a single Connection by ID. The lookup is
    // O(1) (single map index in MemStore; primary-key SELECT in
    // Postgres). Returns CONNECTION_NOT_FOUND if absent.
    GetConnection(ctx context.Context, connectionID ulid.ULID) (*Connection, error)

    // UpdateSessionConnection atomically runs the mutator callback
    // against the named (session, connection) pair under one Store-lock
    // acquisition. Both Info AND Connection writes commit together.
    // MemStore impl: acquires the store-wide m.mu sync.RWMutex
    // (memstore.go:18) for the callback duration. Postgres impl: single
    // transaction, FOR UPDATE on sessions row FIRST then session_connections
    // row (D11 canonical lock order).
    // Returns CONNECTION_NOT_FOUND if the connection isn't registered.
    UpdateSessionConnection(
        ctx context.Context,
        sessionID string,
        connectionID ulid.ULID,
        m SessionConnectionMutator,
    ) error

    // ListConnectionsBySession returns a snapshot of all active
    // Connections for a session. Used by AutoFocusOnJoin's fan-out
    // enumeration. Returns an empty slice (nil error) if the session
    // has no connections; returns SESSION_NOT_FOUND if the session
    // itself doesn't exist.
    ListConnectionsBySession(ctx context.Context, sessionID string) ([]*Connection, error)
```

**Build-broken-tree window** (IMP-6): adding methods to `Store` breaks compilation across every package that imports it (focus coordinator, plugin host, gRPC server, web) until T8 lands the last impl. Intermediate commits T4-T7 will fail `task lint` and `task test` outside this section. Recovery is automatic when T8 completes.

- [ ] **Step 2: Verify compile (impls follow in T5-T8)**

```bash
task lint -- ./internal/session/
```

Expected: FAIL — MemStore and PostgresSessionStore don't yet implement the new methods. This is intentional; T5-T8 fix it. The compile failure proves the interface extension is in place.

- [ ] **Step 3: Commit (interface-only change; build-broken intentionally)**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(session): Store iface — UpdateSessionConnection + ListConnectionsBySession (5rh.14 T4)

Adds two methods to the Store interface for Phase 5. Impls follow
in T5 (MemStore UpdateSessionConnection), T6 (Postgres
UpdateSessionConnection + D11 lock order), T7 (MemStore
ListConnectionsBySession), T8 (Postgres ListConnectionsBySession).
Build is broken between this task and T8 — recover by completing
the chain.

Bead: holomush-5rh.14.4"
jj --no-pager new
```

---

### Task 5: MemStore `GetConnection` + `UpdateSessionConnection` impls + atomicity test

**Files:**

- Modify: `internal/session/memstore.go` (add `GetConnection` + `UpdateSessionConnection`)
- Test: `internal/session/memstore_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Append to `internal/session/memstore_test.go`:

```go
func TestUpdateSessionConnection_HappyPath(t *testing.T) {
    t.Parallel()
    s := NewMemStore()
    ctx := context.Background()

    sessionID := "sess-uc-happy"
    require.NoError(t, s.Set(ctx, sessionID, &Info{ID: sessionID, Status: StatusActive}))

    connID := ulid.Make()
    require.NoError(t, s.AddConnection(ctx, &Connection{
        ID: connID, SessionID: sessionID, ClientType: "terminal",
    }))

    sceneID := ulid.Make()
    target := &FocusKey{Kind: FocusKindScene, TargetID: sceneID}

    m := NewSessionConnectionMutator(func(info Info, conn Connection) (Info, Connection, error) {
        conn.FocusKey = target
        info.PresentingFocus = target
        return info, conn, nil
    })

    require.NoError(t, s.UpdateSessionConnection(ctx, sessionID, connID, m))

    // Verify Connection.FocusKey written.
    conn, err := s.GetConnection(ctx, connID)
    require.NoError(t, err)
    require.NotNil(t, conn.FocusKey)
    assert.Equal(t, target.TargetID, conn.FocusKey.TargetID)

    // Verify Info.PresentingFocus written.
    info, err := s.Get(ctx, sessionID)
    require.NoError(t, err)
    require.NotNil(t, info.PresentingFocus)
    assert.Equal(t, target.TargetID, info.PresentingFocus.TargetID)
}

func TestUpdateSessionConnection_ConnectionNotFound(t *testing.T) {
    t.Parallel()
    s := NewMemStore()
    ctx := context.Background()

    require.NoError(t, s.Set(ctx, "sess-uc-404", &Info{ID: "sess-uc-404", Status: StatusActive}))

    m := NewSessionConnectionMutator(func(info Info, conn Connection) (Info, Connection, error) {
        return info, conn, nil
    })
    err := s.UpdateSessionConnection(ctx, "sess-uc-404", ulid.Make(), m)
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "CONNECTION_NOT_FOUND")
}

func TestUpdateSessionConnection_MutatorErrorPropagates(t *testing.T) {
    t.Parallel()
    s := NewMemStore()
    ctx := context.Background()

    sessionID := "sess-uc-err"
    require.NoError(t, s.Set(ctx, sessionID, &Info{ID: sessionID, Status: StatusActive}))
    connID := ulid.Make()
    require.NoError(t, s.AddConnection(ctx, &Connection{ID: connID, SessionID: sessionID, ClientType: "telnet"}))

    sentinel := oops.Code("FOCUS_WITHOUT_MEMBERSHIP").Errorf("test")
    m := NewSessionConnectionMutator(func(info Info, conn Connection) (Info, Connection, error) {
        return info, conn, sentinel
    })
    err := s.UpdateSessionConnection(context.Background(), sessionID, connID, m)
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "FOCUS_WITHOUT_MEMBERSHIP")

    // Verify NO write happened despite mutator returning (info, conn, err).
    info, err2 := s.Get(ctx, sessionID)
    require.NoError(t, err2)
    assert.Nil(t, info.PresentingFocus, "mutator error MUST abort the write")
}

func TestUpdateSessionConnection_AtomicCommit(t *testing.T) {
    t.Parallel()
    // INV-P5-7: an external observer between the mutator call and
    // its commit cannot see one field updated while the other lags.
    // MemStore implements this by holding m.mu.Lock() for the whole
    // mutator callback, so any concurrent Get() blocks until commit.
    s := NewMemStore()
    ctx := context.Background()

    sessionID := "sess-uc-atomic"
    require.NoError(t, s.Set(ctx, sessionID, &Info{ID: sessionID, Status: StatusActive}))
    connID := ulid.Make()
    require.NoError(t, s.AddConnection(ctx, &Connection{ID: connID, SessionID: sessionID, ClientType: "terminal"}))

    target := &FocusKey{Kind: FocusKindScene, TargetID: ulid.Make()}

    // Launch the mutation, blocking the mutator mid-flight via a channel.
    blockCh := make(chan struct{})
    doneCh := make(chan error)
    go func() {
        m := NewSessionConnectionMutator(func(info Info, conn Connection) (Info, Connection, error) {
            <-blockCh // hold the lock
            conn.FocusKey = target
            info.PresentingFocus = target
            return info, conn, nil
        })
        doneCh <- s.UpdateSessionConnection(ctx, sessionID, connID, m)
    }()

    // While the mutator is blocked, a concurrent Get MUST block until
    // the lock is released. We give the mutator a 50ms head start.
    time.Sleep(50 * time.Millisecond)

    // External read attempted in a goroutine; should observe POST-commit
    // state once we unblock.
    readDone := make(chan struct{})
    var info *Info
    var conn *Connection
    go func() {
        var err error
        info, err = s.Get(ctx, sessionID)
        require.NoError(t, err)
        conn, err = s.GetConnection(ctx, connID)
        require.NoError(t, err)
        close(readDone)
    }()

    // Verify the reader is blocked while mutator holds the lock.
    select {
    case <-readDone:
        t.Fatal("INV-P5-7 violated: external read returned before mutator committed (torn state observable)")
    case <-time.After(20 * time.Millisecond):
        // good — reader is blocked
    }

    // Release the mutator; commit happens; reader unblocks.
    close(blockCh)
    require.NoError(t, <-doneCh)
    <-readDone

    // Both fields MUST be observed post-commit (no torn state).
    require.NotNil(t, info.PresentingFocus, "PresentingFocus visible post-commit")
    require.NotNil(t, conn.FocusKey, "FocusKey visible post-commit")
    assert.Equal(t, target.TargetID, info.PresentingFocus.TargetID)
    assert.Equal(t, target.TargetID, conn.FocusKey.TargetID)
}
```

(Imports may need `"time"`, `"sync"`, `"github.com/samber/oops"`, `"github.com/holomush/holomush/pkg/errutil"` — add as needed.)

- [ ] **Step 2: Run tests to verify they fail**

```bash
task test -- -run TestUpdateSessionConnection ./internal/session/
```

Expected: FAIL — `UpdateSessionConnection` method does not exist on `MemStore`.

- [ ] **Step 3: Implement the methods**

Append to `internal/session/memstore.go` (after `UpdateFocusMemberships` at `:418-459`):

```go
// GetConnection looks up a single Connection by ID. O(1) via the
// store's existing connections map.
func (m *MemStore) GetConnection(_ context.Context, connectionID ulid.ULID) (*Connection, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    conn, ok := m.connections[connectionID]
    if !ok {
        return nil, oops.Code("CONNECTION_NOT_FOUND").
            With("connection_id", connectionID.String()).
            Errorf("connection not found")
    }
    cpy := *conn // defensive copy — callers can't mutate the store
    return &cpy, nil
}

// UpdateSessionConnection runs the mutator under the store-wide m.mu
// lock. Both Info and Connection writes commit atomically. INV-P5-7:
// external observers cannot see one field updated while the other lags.
func (m *MemStore) UpdateSessionConnection(
    _ context.Context,
    sessionID string,
    connectionID ulid.ULID,
    mut SessionConnectionMutator,
) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    info, ok := m.sessions[sessionID]
    if !ok {
        return oops.Code("SESSION_NOT_FOUND").
            With("session_id", sessionID).
            Errorf("session not found")
    }

    conn, ok := m.connections[connectionID]
    if !ok || conn.SessionID != sessionID {
        return oops.Code("CONNECTION_NOT_FOUND").
            With("session_id", sessionID).
            With("connection_id", connectionID.String()).
            Errorf("connection not found in session")
    }

    nextInfo, nextConn, err := mut.Mutate(*info, *conn)
    if err != nil {
        return err //nolint:wrapcheck // mutator error codes pass through
    }

    // Narrow assignment for parity with the Postgres impl (T6) which
    // only UPDATEs focus_key + presenting_focus. Mutator changes to any
    // other Info/Connection field are silently dropped on both backends.
    // CONTRACT: Phase 5's mutator only writes these two fields.
    info.PresentingFocus = nextInfo.PresentingFocus
    conn.FocusKey = nextConn.FocusKey
    return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
task test -- -run TestUpdateSessionConnection ./internal/session/
```

Expected: PASS (all 4 subtests).

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(memstore): UpdateSessionConnection — atomic dual-field write (5rh.14 T5)

INV-P5-7: single mutator call writes both Info and Connection atomically
under the store-wide m.mu sync.RWMutex. Observer-visibility test pins
that no external Get() can see one field updated while the other lags.
Postgres parallel impl in T6.

Bead: holomush-5rh.14.5"
jj --no-pager new
```

---

### Task 6: Postgres `UpdateSessionConnection` impl + D11 lock-order test

**Files:**

- Modify: `internal/store/session_store.go` (add new method after `UpdateFocusMemberships` at `:668-755`)
- Test: `internal/store/session_store_integration_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Append to `internal/store/session_store_integration_test.go`:

```go
func TestPostgresUpdateSessionConnection_HappyPath(t *testing.T) {
    t.Parallel()
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    pool := setupTestPool(t)
    defer pool.Close()
    s := NewPostgresSessionStore(pool)

    sessionID := "sess-pg-uc-happy"
    require.NoError(t, s.Set(ctx, sessionID, &session.Info{
        ID: sessionID, Status: session.StatusActive,
    }))

    connID := ulid.Make()
    require.NoError(t, s.AddConnection(ctx, &session.Connection{
        ID: connID, SessionID: sessionID, ClientType: "terminal",
    }))

    target := &session.FocusKey{Kind: session.FocusKindScene, TargetID: ulid.Make()}
    m := session.NewSessionConnectionMutator(func(info session.Info, conn session.Connection) (session.Info, session.Connection, error) {
        conn.FocusKey = target
        info.PresentingFocus = target
        return info, conn, nil
    })
    require.NoError(t, s.UpdateSessionConnection(ctx, sessionID, connID, m))

    // Verify Postgres round-trip.
    conn, err := s.GetConnection(ctx, connID)
    require.NoError(t, err)
    require.NotNil(t, conn.FocusKey)
    assert.Equal(t, target.TargetID, conn.FocusKey.TargetID)

    info, err := s.Get(ctx, sessionID)
    require.NoError(t, err)
    require.NotNil(t, info.PresentingFocus)
    assert.Equal(t, target.TargetID, info.PresentingFocus.TargetID)
}

func TestPostgresUpdateSessionConnection_LockAcquisitionOrder_NoDeadlock(t *testing.T) {
    t.Parallel()
    // INV-P5-14 (D11): two concurrent UpdateSessionConnection calls on
    // the same session for different connections MUST NOT deadlock.
    // The canonical lock order is sessions row FIRST, then
    // session_connections row.
    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()

    pool := setupTestPool(t)
    defer pool.Close()
    s := NewPostgresSessionStore(pool)

    sessionID := "sess-pg-deadlock"
    require.NoError(t, s.Set(ctx, sessionID, &session.Info{
        ID: sessionID, Status: session.StatusActive,
    }))

    connA := ulid.Make()
    connB := ulid.Make()
    require.NoError(t, s.AddConnection(ctx, &session.Connection{
        ID: connA, SessionID: sessionID, ClientType: "terminal",
    }))
    require.NoError(t, s.AddConnection(ctx, &session.Connection{
        ID: connB, SessionID: sessionID, ClientType: "telnet",
    }))

    // Spawn N concurrent UpdateSessionConnection pairs racing on
    // (connA, connB). With canonical order both takes lock on sessions
    // first then on their respective session_connections row; no
    // deadlock possible. Without canonical order this would hang.
    const iters = 50
    targetA := &session.FocusKey{Kind: session.FocusKindScene, TargetID: ulid.Make()}
    targetB := &session.FocusKey{Kind: session.FocusKindScene, TargetID: ulid.Make()}

    errs := make(chan error, iters*2)
    for i := 0; i < iters; i++ {
        go func() {
            m := session.NewSessionConnectionMutator(func(info session.Info, conn session.Connection) (session.Info, session.Connection, error) {
                conn.FocusKey = targetA
                return info, conn, nil
            })
            errs <- s.UpdateSessionConnection(ctx, sessionID, connA, m)
        }()
        go func() {
            m := session.NewSessionConnectionMutator(func(info session.Info, conn session.Connection) (session.Info, session.Connection, error) {
                conn.FocusKey = targetB
                return info, conn, nil
            })
            errs <- s.UpdateSessionConnection(ctx, sessionID, connB, m)
        }()
    }

    for i := 0; i < iters*2; i++ {
        require.NoError(t, <-errs, "INV-P5-14: lock-order discipline MUST prevent deadlock under concurrency")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
task test:int -- -run TestPostgresUpdateSessionConnection ./internal/store/
```

Expected: FAIL — method does not exist on `PostgresSessionStore`.

- [ ] **Step 3: Implement the methods**

Append to `internal/store/session_store.go` (after `UpdateFocusMemberships` at `:664-755`). The pattern mirrors `UpdateFocusMemberships` (read narrow columns FOR UPDATE; unmarshal JSONB; call mutator; marshal and write back — DO NOT use `row_to_json`).

```go
// GetConnection reads one session_connections row by PK. O(1) PK lookup.
// ULID columns are TEXT in the schema (migrations/000001_baseline.up.sql:227);
// mirror parseSessionRow pattern at session_store.go:51-83 — scan TEXT into
// string then ulid.Parse. player_session_id omitted from SELECT —
// session.Connection has no PlayerSessionID field (CRIT-B from plan-review
// r2; the column persists via the existing AddConnection insert path
// session_store.go:466-483 unchanged).
func (s *PostgresSessionStore) GetConnection(ctx context.Context, connectionID ulid.ULID) (*session.Connection, error) {
    var (
        idStr        string
        sessionID    string
        clientType   string
        streams      []string
        focusKeyJSON []byte
        connectedAt  time.Time
    )
    err := s.pool.QueryRow(ctx, `
        SELECT id, session_id, client_type, streams, focus_key, connected_at
        FROM session_connections WHERE id = $1
    `, connectionID.String()).Scan(
        &idStr, &sessionID, &clientType, &streams, &focusKeyJSON, &connectedAt,
    )
    if errors.Is(err, pgx.ErrNoRows) {
        return nil, oops.Code("CONNECTION_NOT_FOUND").
            With("connection_id", connectionID.String()).
            Errorf("connection not found")
    }
    if err != nil {
        return nil, oops.With("operation", "get connection").Wrap(err)
    }
    id, err := ulid.Parse(idStr)
    if err != nil {
        return nil, oops.With("operation", "parse connection id").Wrap(err)
    }
    var fk *session.FocusKey
    if len(focusKeyJSON) > 0 {
        var k session.FocusKey
        if uerr := json.Unmarshal(focusKeyJSON, &k); uerr != nil {
            return nil, oops.With("operation", "unmarshal focus_key").Wrap(uerr)
        }
        fk = &k
    }
    return &session.Connection{
        ID:          id,
        SessionID:   sessionID,
        ClientType:  clientType,
        Streams:     streams,
        FocusKey:    fk,
        ConnectedAt: connectedAt,
    }, nil
}

// UpdateSessionConnection runs the mutator under a single transaction.
// Lock-acquisition order is canonical per D11: sessions row FOR UPDATE
// FIRST, then session_connections row FOR UPDATE. Two concurrent calls
// on the same session for different connections cannot deadlock
// (INV-P5-14).
func (s *PostgresSessionStore) UpdateSessionConnection(
    ctx context.Context,
    sessionID string,
    connectionID ulid.ULID,
    mut session.SessionConnectionMutator,
) error {
    tx, err := s.pool.Begin(ctx)
    if err != nil {
        return oops.With("operation", "begin tx for update session connection").Wrap(err)
    }
    defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is no-op

    // D11 canonical lock order: sessions row FIRST. Mirror
    // UpdateFocusMemberships' narrow-column SELECT pattern at :676-691.
    // The mutator only needs FocusMemberships + PresentingFocus to make
    // decisions; we also read character_id + location_id for context.
    // ULIDs are TEXT in the schema (parseSessionRow pattern :51-83) —
    // scan into strings, then ulid.Parse.
    var statusStr string
    var focusMembershipsJSON, presentingFocusJSON []byte
    var characterIDStr, locationIDStr string
    err = tx.QueryRow(ctx, `
        SELECT status, focus_memberships, presenting_focus, character_id, location_id
        FROM sessions WHERE id = $1 FOR UPDATE
    `, sessionID).Scan(&statusStr, &focusMembershipsJSON, &presentingFocusJSON, &characterIDStr, &locationIDStr)
    if errors.Is(err, pgx.ErrNoRows) {
        return oops.Code("SESSION_NOT_FOUND").
            With("session_id", sessionID).
            Errorf("session not found")
    }
    if err != nil {
        return oops.With("operation", "read session for connection mutation").Wrap(err)
    }
    if statusStr == string(session.StatusExpired) {
        return oops.Code("SESSION_EXPIRED").
            With("session_id", sessionID).
            Errorf("cannot mutate connection on expired session")
    }

    // Unmarshal session fields the mutator may inspect.
    var fms []session.FocusMembership
    if len(focusMembershipsJSON) > 0 {
        if uerr := json.Unmarshal(focusMembershipsJSON, &fms); uerr != nil {
            return oops.With("operation", "unmarshal focus_memberships").Wrap(uerr)
        }
    }
    var pf *session.FocusKey
    if len(presentingFocusJSON) > 0 {
        var k session.FocusKey
        if uerr := json.Unmarshal(presentingFocusJSON, &k); uerr != nil {
            return oops.With("operation", "unmarshal presenting_focus").Wrap(uerr)
        }
        pf = &k
    }
    characterID, perr := ulid.Parse(characterIDStr)
    if perr != nil {
        return oops.With("operation", "parse session character_id").Wrap(perr)
    }
    locationID, perr := ulid.Parse(locationIDStr)
    if perr != nil {
        return oops.With("operation", "parse session location_id").Wrap(perr)
    }
    info := session.Info{
        ID:               sessionID,
        Status:           session.Status(statusStr),
        CharacterID:      characterID,
        LocationID:       locationID,
        FocusMemberships: fms,
        PresentingFocus:  pf,
    }

    // Then session_connections row FOR UPDATE. ULIDs scanned as TEXT
    // then ulid.Parse (CRIT-A fix). player_session_id omitted —
    // session.Connection has no PlayerSessionID field (CRIT-B); the
    // column still persists via AddConnection's insert path
    // (session_store.go:466-483) unchanged.
    var (
        cIDStr        string
        cSessionID    string
        cClientType   string
        cStreams      []string
        cFocusKeyJSON []byte
        cConnectedAt  time.Time
    )
    err = tx.QueryRow(ctx, `
        SELECT id, session_id, client_type, streams, focus_key, connected_at
        FROM session_connections WHERE id = $1 AND session_id = $2 FOR UPDATE
    `, connectionID.String(), sessionID).Scan(
        &cIDStr, &cSessionID, &cClientType, &cStreams, &cFocusKeyJSON, &cConnectedAt,
    )
    if errors.Is(err, pgx.ErrNoRows) {
        return oops.Code("CONNECTION_NOT_FOUND").
            With("session_id", sessionID).
            With("connection_id", connectionID.String()).
            Errorf("connection not found in session")
    }
    if err != nil {
        return oops.With("operation", "read connection for mutation").Wrap(err)
    }
    cID, perr := ulid.Parse(cIDStr)
    if perr != nil {
        return oops.With("operation", "parse connection id").Wrap(perr)
    }
    var cFK *session.FocusKey
    if len(cFocusKeyJSON) > 0 {
        var k session.FocusKey
        if uerr := json.Unmarshal(cFocusKeyJSON, &k); uerr != nil {
            return oops.With("operation", "unmarshal connection focus_key").Wrap(uerr)
        }
        cFK = &k
    }
    conn := session.Connection{
        ID: cID, SessionID: cSessionID, ClientType: cClientType,
        Streams: cStreams, FocusKey: cFK, ConnectedAt: cConnectedAt,
    }

    // Call the mutator with coherent snapshots of both Info and Connection.
    // CONTRACT: the mutator MUST NOT modify Connection.Streams.
    // Streams is owned by SessionStreamRegistry (via subscription_router
    // SendToConnection calls), not by this Store path. The UPDATE below
    // writes only focus_key + presenting_focus; any Streams change in
    // the mutator callback is silently dropped. This is by design —
    // Phase 5's mutator only writes the two focus fields.
    nextInfo, nextConn, merr := mut.Mutate(info, conn)
    if merr != nil {
        return merr //nolint:wrapcheck // mutator error codes pass through
    }

    // Marshal and write back. Per D9/D10 the mutator may or may not
    // change PresentingFocus; write whatever it returned.
    var nextPresentingJSON []byte
    if nextInfo.PresentingFocus != nil {
        nextPresentingJSON, err = json.Marshal(nextInfo.PresentingFocus)
        if err != nil {
            return oops.With("operation", "marshal next presenting_focus").Wrap(err)
        }
    }
    if _, err := tx.Exec(ctx, `
        UPDATE sessions SET presenting_focus = $1::jsonb, updated_at = now() WHERE id = $2
    `, nextPresentingJSON, sessionID); err != nil {
        return oops.With("operation", "write presenting_focus").Wrap(err)
    }

    var nextFocusKeyJSON []byte
    if nextConn.FocusKey != nil {
        nextFocusKeyJSON, err = json.Marshal(nextConn.FocusKey)
        if err != nil {
            return oops.With("operation", "marshal next focus_key").Wrap(err)
        }
    }
    if _, err := tx.Exec(ctx, `
        UPDATE session_connections SET focus_key = $1::jsonb WHERE id = $2
    `, nextFocusKeyJSON, connectionID); err != nil {
        return oops.With("operation", "write connection focus_key").Wrap(err)
    }

    if cerr := tx.Commit(ctx); cerr != nil {
        return oops.With("operation", "commit session connection update").Wrap(cerr)
    }
    return nil
}
```

**Why narrow-column SELECT, not `row_to_json`:** the existing scan path at `session_store.go:111-149` parses 22 individually-named columns; ULIDs are TEXT and parsed via `ulid.Parse`. `row_to_json` would produce a denormalized blob that mismatches `session.Info` field names and silently drops fields. Mirror `UpdateFocusMemberships`'s narrow read pattern instead.

- [ ] **Step 4: Run tests to verify they pass**

```bash
task test:int -- -run TestPostgresUpdateSessionConnection ./internal/store/
```

Expected: PASS — happy path commits; deadlock test completes within 15s (no hangs).

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(postgres-store): UpdateSessionConnection + D11 lock-order (5rh.14 T6)

INV-P5-14 (D11): single transaction; sessions row FOR UPDATE FIRST,
then session_connections row FOR UPDATE. 50-iteration concurrency
test confirms no deadlock between racing UpdateSessionConnection
calls on different conns of the same session.

Bead: holomush-5rh.14.6"
jj --no-pager new
```

---

### Task 7: MemStore `ListConnectionsBySession`

**Files:**

- Modify: `internal/session/memstore.go`
- Test: `internal/session/memstore_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Append to `internal/session/memstore_test.go`:

```go
func TestListConnectionsBySession_Empty(t *testing.T) {
    t.Parallel()
    s := NewMemStore()
    ctx := context.Background()
    require.NoError(t, s.Set(ctx, "sess-list-empty", &Info{ID: "sess-list-empty", Status: StatusActive}))

    conns, err := s.ListConnectionsBySession(ctx, "sess-list-empty")
    require.NoError(t, err)
    assert.Empty(t, conns)
}

func TestListConnectionsBySession_Multi(t *testing.T) {
    t.Parallel()
    s := NewMemStore()
    ctx := context.Background()
    sessionID := "sess-list-multi"
    require.NoError(t, s.Set(ctx, sessionID, &Info{ID: sessionID, Status: StatusActive}))

    for _, ct := range []string{"terminal", "telnet", "comms_hub"} {
        require.NoError(t, s.AddConnection(ctx, &Connection{ID: ulid.Make(), SessionID: sessionID, ClientType: ct}))
    }

    conns, err := s.ListConnectionsBySession(ctx, sessionID)
    require.NoError(t, err)
    assert.Len(t, conns, 3)

    seen := map[string]bool{}
    for _, c := range conns {
        seen[c.ClientType] = true
    }
    assert.True(t, seen["terminal"])
    assert.True(t, seen["telnet"])
    assert.True(t, seen["comms_hub"])
}

func TestListConnectionsBySession_SessionNotFound(t *testing.T) {
    t.Parallel()
    s := NewMemStore()
    _, err := s.ListConnectionsBySession(context.Background(), "nope")
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "SESSION_NOT_FOUND")
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
task test -- -run TestListConnectionsBySession ./internal/session/
```

Expected: FAIL — method does not exist.

- [ ] **Step 3: Implement the method**

Append to `internal/session/memstore.go`:

```go
// ListConnectionsBySession returns a snapshot of all active Connections
// for a session. Used by AutoFocusOnJoin's fan-out enumeration.
func (m *MemStore) ListConnectionsBySession(_ context.Context, sessionID string) ([]*Connection, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()

    if _, ok := m.sessions[sessionID]; !ok {
        return nil, oops.Code("SESSION_NOT_FOUND").
            With("session_id", sessionID).
            Errorf("session not found")
    }

    out := make([]*Connection, 0)
    for _, conn := range m.connections {
        if conn.SessionID == sessionID {
            // Return a defensive copy so callers can't mutate the
            // store's underlying Connection through the returned ptr.
            c := *conn
            out = append(out, &c)
        }
    }
    return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
task test -- -run TestListConnectionsBySession ./internal/session/
```

Expected: PASS (3 subtests).

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(memstore): ListConnectionsBySession (5rh.14 T7)

Enumerator used by AutoFocusOnJoin's fan-out. Returns defensive
copies so callers can't mutate stored Connection through the
returned slice.

Bead: holomush-5rh.14.7"
jj --no-pager new
```

---

### Task 8: Postgres `ListConnectionsBySession`

**Files:**

- Modify: `internal/store/session_store.go`
- Test: `internal/store/session_store_integration_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Append:

```go
func TestPostgresListConnectionsBySession(t *testing.T) {
    t.Parallel()
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    pool := setupTestPool(t)
    defer pool.Close()
    s := NewPostgresSessionStore(pool)

    sessionID := "sess-pg-list"
    require.NoError(t, s.Set(ctx, sessionID, &session.Info{ID: sessionID, Status: session.StatusActive}))

    for _, ct := range []string{"terminal", "comms_hub"} {
        require.NoError(t, s.AddConnection(ctx, &session.Connection{
            ID: ulid.Make(), SessionID: sessionID, ClientType: ct,
        }))
    }

    conns, err := s.ListConnectionsBySession(ctx, sessionID)
    require.NoError(t, err)
    assert.Len(t, conns, 2)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
task test:int -- -run TestPostgresListConnectionsBySession ./internal/store/
```

Expected: FAIL — method missing.

- [ ] **Step 3: Implement the method**

Append to `internal/store/session_store.go`:

```go
// ListConnectionsBySession returns a snapshot of all active Connections
// for a session. No lock — callers must tolerate the racy snapshot
// (each per-conn UpdateSessionConnection re-validates atomically).
func (s *PostgresSessionStore) ListConnectionsBySession(ctx context.Context, sessionID string) ([]*session.Connection, error) {
    // Verify session exists first.
    var exists bool
    if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sessions WHERE id = $1)`, sessionID).Scan(&exists); err != nil {
        return nil, oops.Code("SESSION_GET_FAILED").Wrap(err)
    }
    if !exists {
        return nil, oops.Code("SESSION_NOT_FOUND").
            With("session_id", sessionID).
            Errorf("session not found")
    }

    // ULIDs scanned as TEXT then ulid.Parse (CRIT-A); player_session_id
    // omitted (CRIT-B — Connection has no PlayerSessionID field).
    rows, err := s.pool.Query(ctx, `
        SELECT id, session_id, client_type, streams, focus_key, connected_at
        FROM session_connections
        WHERE session_id = $1
    `, sessionID)
    if err != nil {
        return nil, oops.Code("CONNECTION_LIST_FAILED").Wrap(err)
    }
    defer rows.Close()

    out := make([]*session.Connection, 0)
    for rows.Next() {
        var (
            idStr string; sid string; ct string; streams []string
            fkJSON []byte; connectedAt time.Time
        )
        if err := rows.Scan(&idStr, &sid, &ct, &streams, &fkJSON, &connectedAt); err != nil {
            return nil, oops.Code("CONNECTION_SCAN_FAILED").Wrap(err)
        }
        id, perr := ulid.Parse(idStr)
        if perr != nil {
            return nil, oops.With("operation", "parse connection id").Wrap(perr)
        }
        var fk *session.FocusKey
        if len(fkJSON) > 0 {
            var k session.FocusKey
            if uerr := json.Unmarshal(fkJSON, &k); uerr != nil {
                return nil, oops.With("operation", "unmarshal connection focus_key").Wrap(uerr)
            }
            fk = &k
        }
        conn := session.Connection{
            ID: id, SessionID: sid, ClientType: ct,
            Streams: streams, FocusKey: fk, ConnectedAt: connectedAt,
        }
        out = append(out, &conn)
    }
    if err := rows.Err(); err != nil {
        return nil, oops.Code("CONNECTION_ITER_FAILED").Wrap(err)
    }
    return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
task test:int -- -run TestPostgresListConnectionsBySession ./internal/store/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(postgres-store): ListConnectionsBySession (5rh.14 T8)

Postgres equivalent of MemStore.ListConnectionsBySession. No lock —
callers tolerate racy snapshot since per-conn UpdateSessionConnection
re-validates atomically.

Bead: holomush-5rh.14.8"
jj --no-pager new
```

---

## Phase B — Substrate subscription routing (T9-T11)

### Task 9: `SessionStreamRegistry` per-Connection methods

**Files:**

- Modify: `internal/grpc/stream_registry.go`
- Test: `internal/grpc/stream_registry_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Append:

```go
func TestSendToConnection_TargetsOneConnectionOnly(t *testing.T) {
    t.Parallel()
    // INV-P5-10: SendToConnection delivers update to EXACTLY the named
    // connection's channel; other connections in the same session do
    // NOT receive the update via this path.
    r := NewSessionStreamRegistry()
    sessionID := "sess-stc"
    connA := ulid.Make()
    connB := ulid.Make()
    chA := make(chan sessionStreamUpdate, 1)
    chB := make(chan sessionStreamUpdate, 1)

    r.RegisterConnection(sessionID, connA, chA)
    r.RegisterConnection(sessionID, connB, chB)

    err := r.SendToConnection(sessionID, connA, sessionStreamUpdate{stream: "events.main.scene.X.ic", add: true})
    require.NoError(t, err)

    select {
    case upd := <-chA:
        assert.Equal(t, "events.main.scene.X.ic", upd.stream)
        assert.True(t, upd.add)
    case <-time.After(100 * time.Millisecond):
        t.Fatal("expected delivery to connA's channel")
    }

    select {
    case upd := <-chB:
        t.Fatalf("INV-P5-10 violated: connB received SendToConnection meant for connA: %+v", upd)
    case <-time.After(50 * time.Millisecond):
        // good — connB did NOT receive
    }
}

func TestSendToConnection_ReturnsConnectionNotRegistered(t *testing.T) {
    t.Parallel()
    r := NewSessionStreamRegistry()
    err := r.SendToConnection("sess-x", ulid.Make(), sessionStreamUpdate{stream: "s", add: true})
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "CONNECTION_NOT_REGISTERED")
}

func TestSend_StillBroadcastsForSessionWideCallers(t *testing.T) {
    t.Parallel()
    // Regression: existing Send (session-wide broadcast) MUST be
    // unchanged by Phase 5 additions.
    r := NewSessionStreamRegistry()
    sessionID := "sess-broadcast"
    ch1 := make(chan sessionStreamUpdate, 1)
    ch2 := make(chan sessionStreamUpdate, 1)
    r.Register(sessionID, ch1)
    r.Register(sessionID, ch2)

    require.NoError(t, r.Send(sessionID, sessionStreamUpdate{stream: "ambient", add: true}))

    for _, ch := range []chan sessionStreamUpdate{ch1, ch2} {
        select {
        case upd := <-ch:
            assert.Equal(t, "ambient", upd.stream)
        case <-time.After(100 * time.Millisecond):
            t.Fatal("Send broadcast regression: subscriber missed the update")
        }
    }
}
```

- [ ] **Step 2: Run tests to verify failure**

```bash
task test -- -run 'TestSendToConnection|TestSend_StillBroadcasts' ./internal/grpc/
```

Expected: FAIL — `RegisterConnection` / `SendToConnection` methods do not exist.

- [ ] **Step 3: Implement the methods**

Modify `internal/grpc/stream_registry.go`. Add new field + methods:

```go
type SessionStreamRegistry struct {
    mu          sync.Mutex
    channels    map[string]map[chan<- sessionStreamUpdate]struct{}
    // connections maps (sessionID → connectionID → channel) for the
    // Phase 5 per-Connection routing path (D5). Co-exists with channels;
    // session-wide Send still broadcasts via channels.
    connections map[string]map[ulid.ULID]chan<- sessionStreamUpdate
}

func NewSessionStreamRegistry() *SessionStreamRegistry {
    return &SessionStreamRegistry{
        channels:    make(map[string]map[chan<- sessionStreamUpdate]struct{}),
        connections: make(map[string]map[ulid.ULID]chan<- sessionStreamUpdate),
    }
}

// RegisterConnection associates a (sessionID, connectionID) pair with
// its control channel. Used by CoreServer.Subscribe at stream setup
// time when the request carries an explicit ConnectionId.
func (r *SessionStreamRegistry) RegisterConnection(
    sessionID string, connectionID ulid.ULID, ch chan<- sessionStreamUpdate,
) {
    r.mu.Lock()
    defer r.mu.Unlock()
    conns, ok := r.connections[sessionID]
    if !ok {
        conns = make(map[ulid.ULID]chan<- sessionStreamUpdate)
        r.connections[sessionID] = conns
    }
    conns[connectionID] = ch
}

// SendToConnection delivers update to EXACTLY the named connection's
// channel. INV-P5-10.
// Returns CONNECTION_NOT_REGISTERED if the conn isn't registered;
// CONTROL_CHANNEL_FULL if the buffer is exhausted.
func (r *SessionStreamRegistry) SendToConnection(
    sessionID string, connectionID ulid.ULID, update sessionStreamUpdate,
) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    conns, ok := r.connections[sessionID]
    if !ok {
        return oops.Code("CONNECTION_NOT_REGISTERED").
            With("session_id", sessionID).
            With("connection_id", connectionID.String()).
            Errorf("no connection registered for session")
    }
    ch, ok := conns[connectionID]
    if !ok {
        return oops.Code("CONNECTION_NOT_REGISTERED").
            With("session_id", sessionID).
            With("connection_id", connectionID.String()).
            Errorf("connection not registered for session")
    }
    select {
    case ch <- update:
        return nil
    default:
        return oops.Code("CONTROL_CHANNEL_FULL").
            With("session_id", sessionID).
            With("connection_id", connectionID.String()).
            Errorf("control channel full")
    }
}
```

Also add `DeregisterConnection` to mirror the new `RegisterConnection` path. The existing `Deregister(sessionID, ch)` (`stream_registry.go:62`) stays unchanged for legacy callers; the new method targets `(sessionID, connectionID)`:

```go
// DeregisterConnection removes the (sessionID, connectionID) entry
// from the per-Connection routing map. Used by CoreServer.Subscribe
// (T11) at stream teardown — paired with RegisterConnection above.
func (r *SessionStreamRegistry) DeregisterConnection(sessionID string, connectionID ulid.ULID) {
    r.mu.Lock()
    defer r.mu.Unlock()
    conns, ok := r.connections[sessionID]
    if !ok {
        return
    }
    delete(conns, connectionID)
    if len(conns) == 0 {
        delete(r.connections, sessionID)
    }
}
```

**Error-code disambiguation (CRIT-5 from plan-review r1):** the existing `Send(sessionID, update)` returns `SESSION_NOT_FOUND` when the session has zero subscribers (`stream_registry.go:84`). The new `SendToConnection(sessionID, connID, update)` returns `CONNECTION_NOT_REGISTERED` when the (session, conn) pair isn't in the new connections map. These are semantically distinct conditions and callers always know which method they invoked — `subscription_router` only calls `SendToConnection`; legacy callers stay on `Send`. The two codes do NOT collide at the call site; both surfaces are documented in the godoc on each method.

- [ ] **Step 4: Run tests to verify they pass**

```bash
task test -- -run 'TestSendToConnection|TestSend_StillBroadcasts' ./internal/grpc/
```

Expected: PASS (3 subtests).

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(stream-registry): per-Connection routing (5rh.14 T9)

D5 + INV-P5-10: RegisterConnection + SendToConnection target a single
named connection. Existing session-wide Send is unchanged. Error
contract: CONNECTION_NOT_REGISTERED, CONTROL_CHANNEL_FULL.

Bead: holomush-5rh.14.9"
jj --no-pager new
```

---

### Task 10: `subscription_router.go` — focus-managed stream deltas

**Files:**

- Create: `internal/grpc/focus/subscription_router.go`
- Test: `internal/grpc/focus/subscription_router_test.go` (NEW)

- [ ] **Step 1: Write the failing test**

Create `internal/grpc/focus/subscription_router_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
    "testing"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"

    "github.com/holomush/holomush/internal/session"
)

func TestComputeFocusManagedStreams_GridFocus_LocationOnly(t *testing.T) {
    t.Parallel()
    locID := ulid.Make()
    streams := computeFocusManagedStreams(nil, locID, "main")
    assert.Equal(t, []string{"location:" + locID.String()}, streams)
}

func TestComputeFocusManagedStreams_SceneFocus_ICAndOOC(t *testing.T) {
    t.Parallel()
    sceneID := ulid.Make()
    fk := &session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}
    locID := ulid.Make()
    streams := computeFocusManagedStreams(fk, locID, "main")
    assert.ElementsMatch(t, []string{
        "events.main.scene." + sceneID.String() + ".ic",
        "events.main.scene." + sceneID.String() + ".ooc",
    }, streams)
}

func TestComputeFocusManagedStreams_Deterministic(t *testing.T) {
    t.Parallel()
    // INV-P5-3: same inputs → same streams (membership comparison).
    sceneID := ulid.Make()
    fk := &session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}
    locID := ulid.Make()
    for i := 0; i < 5; i++ {
        a := computeFocusManagedStreams(fk, locID, "main")
        b := computeFocusManagedStreams(fk, locID, "main")
        assert.Equal(t, a, b)
    }
}

func TestStreamDeltas_AddsAndRemoves(t *testing.T) {
    t.Parallel()
    old := []string{"a", "b", "c"}
    new := []string{"b", "c", "d"}
    adds, removes := streamDeltas(old, new)
    assert.ElementsMatch(t, []string{"d"}, adds)
    assert.ElementsMatch(t, []string{"a"}, removes)
}
```

- [ ] **Step 2: Run tests to verify failure**

```bash
task test -- -run 'TestComputeFocusManagedStreams|TestStreamDeltas' ./internal/grpc/focus/
```

Expected: FAIL — functions undefined.

- [ ] **Step 3: Create `subscription_router.go`**

Create `internal/grpc/focus/subscription_router.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
    "github.com/oklog/ulid/v2"

    "github.com/holomush/holomush/internal/session"
)

// computeFocusManagedStreams returns the focus-managed subset of a
// Connection's stream subscriptions (INV-P5-3 deterministic function
// of FocusKey + character_location + gameID). Always-on streams
// (notifications:<character_id>) are written once at connection
// creation and not touched by this router.
//
// Grid focus (FocusKey == nil) → location:<character_location_id>
// (colon-style retained; dot-style migration tracked by holomush-rops).
//
// Scene focus → events.<gameID>.scene.<sceneID>.{ic,ooc} (dot-style
// from Phase 4 T11).
func computeFocusManagedStreams(fk *session.FocusKey, characterLocationID ulid.ULID, gameID string) []string {
    if fk == nil {
        return []string{"location:" + characterLocationID.String()}
    }
    if fk.Kind == session.FocusKindScene {
        sceneID := fk.TargetID.String()
        return []string{
            "events." + gameID + ".scene." + sceneID + ".ic",
            "events." + gameID + ".scene." + sceneID + ".ooc",
        }
    }
    // Future kinds (channel, etc.) fall through to grid until plumbed.
    return []string{"location:" + characterLocationID.String()}
}

// streamDeltas computes the (adds, removes) sets between two stream
// lists. Used by subscription_router to derive AddSessionStream /
// RemoveSessionStream calls on focus change.
func streamDeltas(old, new []string) (adds, removes []string) {
    oldSet := make(map[string]struct{}, len(old))
    for _, s := range old {
        oldSet[s] = struct{}{}
    }
    newSet := make(map[string]struct{}, len(new))
    for _, s := range new {
        newSet[s] = struct{}{}
    }
    for s := range newSet {
        if _, ok := oldSet[s]; !ok {
            adds = append(adds, s)
        }
    }
    for s := range oldSet {
        if _, ok := newSet[s]; !ok {
            removes = append(removes, s)
        }
    }
    return adds, removes
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
task test -- -run 'TestComputeFocusManagedStreams|TestStreamDeltas' ./internal/grpc/focus/
```

Expected: PASS (4 subtests).

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(focus): subscription_router for per-Conn deltas (5rh.14 T10)

INV-P5-3: deterministic function of (FocusKey, location, gameID).
streamDeltas helper computes (adds, removes) for the post-mutation
SendToConnection fan-out. Always-on streams not touched.

Bead: holomush-5rh.14.10"
jj --no-pager new
```

---

### Task 11: Subscribe handler switches to `RegisterConnection`

**Files:**

- Modify: `internal/grpc/server.go` (`CoreServer.Subscribe`)
- Test: `internal/grpc/subscribe_server_test.go` (extend or rely on existing coverage)

- [ ] **Step 1: Locate the existing Register call**

```bash
rg -n 'registry\.Register\b' internal/grpc/server.go
```

Identify the `Register(sessionID, ch)` call site in `CoreServer.Subscribe`.

- [ ] **Step 2: Write the failing test**

Append to `internal/grpc/subscribe_server_test.go`:

```go
func TestSubscribe_RegistersByConnectionID(t *testing.T) {
    t.Parallel()
    // After Phase 5, Subscribe must use RegisterConnection so the
    // resulting filter set is per-Connection, not session-wide.
    // Verifiable: send two SubscribeRequests with distinct ConnectionIds
    // on the same session, then inspect the SessionStreamRegistry's
    // connections map and confirm both ConnectionIds are registered.

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    // Existing test helpers in subscribe_server_test.go construct a
    // *CoreServer with a backing SessionStreamRegistry. We reuse them.
    srv, registry := setupSubscribeTestServer(t)

    sessionID := "sess-rbcid"
    connA := ulid.Make()
    connB := ulid.Make()

    // Fire two Subscribe RPCs in goroutines (Subscribe is a streaming
    // RPC; we let it register, then cancel ctx to deregister cleanly).
    subA, cancelA := context.WithCancel(ctx)
    subB, cancelB := context.WithCancel(ctx)
    defer cancelA()
    defer cancelB()

    go func() {
        _ = srv.Subscribe(&pluginv1.SubscribeRequest{
            SessionId:    sessionID,
            ConnectionId: connA.String(),
            ClientType:   "terminal",
        }, &fakeSubscribeStream{ctx: subA})
    }()
    go func() {
        _ = srv.Subscribe(&pluginv1.SubscribeRequest{
            SessionId:    sessionID,
            ConnectionId: connB.String(),
            ClientType:   "comms_hub",
        }, &fakeSubscribeStream{ctx: subB})
    }()

    // Wait until both registrations have landed (synchronization via
    // the registry's internal snapshot — see helper).
    waitForRegistrations(t, registry, sessionID, 2)

    // INV-P5-11 sibling property: the registry tracks both connections
    // as distinct (sessionID, connectionID) keys, not as siblings in
    // the session-wide channels map.
    assert.True(t, registry.HasConnection(sessionID, connA),
        "Subscribe MUST register connA via RegisterConnection")
    assert.True(t, registry.HasConnection(sessionID, connB),
        "Subscribe MUST register connB via RegisterConnection")
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
task test -- -run TestSubscribe_RegistersByConnectionID ./internal/grpc/
```

Expected: FAIL — current Subscribe still uses session-keyed Register.

- [ ] **Step 4: Update the Subscribe handler**

In `internal/grpc/server.go`, locate the `r.streamRegistry.Register(sessionID, ch)` call. Replace with:

```go
connID, err := ulid.Parse(req.GetConnectionId())
if err != nil {
    return oops.Code("SUBSCRIBE_INVALID_CONNECTION").
        With("connection_id", req.GetConnectionId()).
        Wrap(err)
}
r.streamRegistry.RegisterConnection(sessionID, connID, ch)
```

(Update the corresponding `Deregister` to `DeregisterConnection`.)

- [ ] **Step 5: Run test to verify it passes**

```bash
task test -- -run TestSubscribe_RegistersByConnectionID ./internal/grpc/
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(grpc-subscribe): switch to RegisterConnection (5rh.14 T11)

CoreServer.Subscribe now registers per-Connection via the new
SessionStreamRegistry.RegisterConnection path. SubscribeRequest.ConnectionId
is already populated by all callers (subscribe_server_test.go:213-247).

Bead: holomush-5rh.14.11"
jj --no-pager new
```

---

## Phase C — Proto + Lua hostfunc (T12-T13)

### Task 12: Proto definitions for 3 RPCs

**Files:**

- Modify: `api/proto/holomush/plugin/v1/plugin.proto`
- Test: `task generate` regenerates Go stubs; verify compile

- [ ] **Step 1: Add the proto messages and RPCs**

Append to `api/proto/holomush/plugin/v1/plugin.proto` in the `PluginHostService` service block (after the existing RPCs):

```proto
  // SetConnectionFocus — Phase 5 explicit focus mutation for one
  // Connection. Substrate validates membership against FocusMemberships
  // (D4); writes Connection.FocusKey + (D9-gated) Info.PresentingFocus
  // atomically under one Store-lock acquisition (D7).
  rpc SetConnectionFocus(PluginHostServiceSetConnectionFocusRequest) returns (PluginHostServiceSetConnectionFocusResponse);

  // AutoFocusOnJoin — Phase 5 fan-out: focuses all terminal/telnet
  // connections of the character on the given scene. Skips conns
  // already explicitly focused elsewhere (D8). Caller must have
  // completed JoinFocus before invocation.
  rpc AutoFocusOnJoin(PluginHostServiceAutoFocusOnJoinRequest) returns (PluginHostServiceAutoFocusOnJoinResponse);

  // IsAnyConnFocused — Phase 5 notification-emission helper: true iff
  // any of the character's connections has FocusKey == {scene, scene_id}.
  rpc IsAnyConnFocused(PluginHostServiceIsAnyConnFocusedRequest) returns (PluginHostServiceIsAnyConnFocusedResponse);
}

message FocusKey {
  string kind = 1;       // "scene" in Phase 5
  bytes target_id = 2;   // ULID
}

message PluginHostServiceSetConnectionFocusRequest {
  bytes connection_id = 1;  // ULID
  optional FocusKey focus_key = 2;
  // is_scene_grid signals that this call originated from a `scene grid`
  // command — substrate skips the D9 PresentingFocus write per D10.
  bool is_scene_grid = 3;
}

message PluginHostServiceSetConnectionFocusResponse {
  optional FocusKey focus_key = 1;
}

message PluginHostServiceAutoFocusOnJoinRequest {
  bytes character_id = 1;
  bytes scene_id = 2;
}

message PluginHostServiceAutoFocusOnJoinResponse {
  repeated bytes focused_connection_ids = 1;
  uint32 total_connection_count = 2;
  repeated bytes skipped_connection_ids = 3;
  repeated FocusFailure failed_connection_ids = 4;
}

message FocusFailure {
  bytes connection_id = 1;
  FocusFailureReason reason = 2;
}

enum FocusFailureReason {
  FOCUS_FAILURE_REASON_UNSPECIFIED = 0;
  FOCUS_FAILURE_REASON_MEMBERSHIP_ABSENT = 1;
  FOCUS_FAILURE_REASON_CONNECTION_NOT_FOUND = 2;
}

message PluginHostServiceIsAnyConnFocusedRequest {
  bytes character_id = 1;
  bytes scene_id = 2;
}

message PluginHostServiceIsAnyConnFocusedResponse {
  bool focused = 1;
}
```

(Place the `}` carefully — the existing service block currently ends with `rpc RequestEmitToken(...);` plus a closing brace. Insert the 3 new RPCs BEFORE the closing brace.)

- [ ] **Step 2: Regenerate stubs**

```bash
task generate
```

Expected: regenerates `pkg/proto/holomush/plugin/v1/plugin.pb.go` + grpc.pb.go without errors.

- [ ] **Step 3: Verify compile**

```bash
task lint -- ./pkg/proto/...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(proto): PluginHostService — 3 Phase 5 RPCs (5rh.14 T12)

D1 + D3 + D6 + D8: SetConnectionFocus, AutoFocusOnJoin, IsAnyConnFocused
with bytes ULID fields. FocusFailure message + FocusFailureReason enum
in plugin/v1 alongside the AutoFocus response per round-4 polish.

Bead: holomush-5rh.14.12"
jj --no-pager new
```

---

### Task 13: Lua hostfunc bindings (INV-P5-6, INV-P5-9)

**Files:**

- Modify: `internal/plugin/hostfunc/stdlib_focus.go`
- Test: `internal/plugin/hostfunc/stdlib_focus_test.go` (extend)

- [ ] **Step 1: Write the failing test**

Append to `internal/plugin/hostfunc/stdlib_focus_test.go`:

```go
func TestFocusHostfunc_PhaseFive_LuaParity(t *testing.T) {
    t.Parallel()
    // INV-P5-6: the 3 new RPCs ship Go SDK + Lua hostfunc together.
    // Test that each is registered in the holomush module table.
    ls := lua.NewState()
    defer ls.Close()
    mod := ls.NewTable()
    RegisterFocusFuncs(ls, mod, /* mocks */ nil, nil)

    for _, name := range []string{"set_connection_focus", "auto_focus_on_join", "is_any_conn_focused"} {
        fn := ls.GetField(mod, name)
        require.NotEqual(t, lua.LNil, fn, "hostfunc %q MUST be registered for INV-P5-6 parity", name)
    }
}

func TestFocusHostfunc_ULIDRoundTrip(t *testing.T) {
    t.Parallel()
    // INV-P5-9: Lua hostfunc accepts 26-char base32 string ULIDs; proto
    // wire takes bytes; the boundary converts. Round-trip a known ULID
    // through set_connection_focus's parser.
    id := ulid.Make()
    s := id.String()
    parsed, err := ulid.Parse(s)
    require.NoError(t, err)
    assert.Equal(t, id, parsed, "ULID round-trip MUST be exact")
}
```

- [ ] **Step 2: Run tests to verify failure**

```bash
task test -- -run 'TestFocusHostfunc_PhaseFive|TestFocusHostfunc_ULIDRoundTrip' ./internal/plugin/hostfunc/
```

Expected: FAIL — hostfuncs not registered.

- [ ] **Step 3: Add the bindings**

Modify `internal/plugin/hostfunc/stdlib_focus.go`:

Extend the `FocusOps` interface (`:27-32`) with the 3 new methods:

```go
type FocusOps interface {
    JoinFocus(ctx context.Context, sessionID string, target session.FocusKey) error
    LeaveFocus(ctx context.Context, sessionID string, target session.FocusKey) error
    LeaveFocusByTarget(ctx context.Context, target session.FocusKey) (session.LeaveByTargetResult, error)
    PresentFocus(ctx context.Context, sessionID string, target session.FocusKey) error
    // Phase 5 additions (INV-P5-6, D6):
    SetConnectionFocus(ctx context.Context, connectionID ulid.ULID, focusKey *session.FocusKey, isSceneGrid bool) error
    AutoFocusOnJoin(ctx context.Context, characterID, sceneID ulid.ULID) (focused, skipped []ulid.ULID, failed []focusFailure, totalConnCount uint32, err error)
    IsAnyConnFocused(ctx context.Context, characterID, sceneID ulid.ULID) (bool, error)
}

// focusFailure mirrors the proto FocusFailure shape for Lua serialization.
type focusFailure struct {
    ConnectionID ulid.ULID
    Reason       string // "membership_absent" | "connection_not_found"
}
```

Extend `RegisterFocusFuncs` (`:41-58`):

```go
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
    ls.SetField(mod, "leave_focus_by_target", ls.NewFunction(leaveFocusByTargetFn))
    ls.SetField(mod, "present_focus", ls.NewFunction(presentFocusFn))
    ls.SetField(mod, "query_stream_history", ls.NewFunction(queryStreamHistoryFn))
    // Phase 5 additions:
    ls.SetField(mod, "set_connection_focus", ls.NewFunction(setConnectionFocusFn))
    ls.SetField(mod, "auto_focus_on_join", ls.NewFunction(autoFocusOnJoinFn))
    ls.SetField(mod, "is_any_conn_focused", ls.NewFunction(isAnyConnFocusedFn))
}
```

Add 3 new `*Fn` functions at the end of the file. Each:

1. Pulls Lua args (strings).
2. Parses ULIDs via `ulid.Parse`; pushes `nil, err_string` on parse failure (INV-P5-9).
3. Invokes the matching FocusOps method.
4. Pushes the result back to Lua.

Concrete shapes:

```go
func setConnectionFocusFn(ls *lua.LState) int {
    fo := getFocusOps(ls)
    if fo == nil {
        ls.Push(lua.LNil)
        ls.Push(lua.LString("focus_ops not registered"))
        return 2
    }
    connIDStr := ls.CheckString(1)
    connID, err := ulid.Parse(connIDStr)
    if err != nil {
        ls.Push(lua.LNil)
        ls.Push(lua.LString("INVALID_ULID: " + err.Error()))
        return 2
    }
    var fk *session.FocusKey
    if ls.Get(2) != lua.LNil {
        // Lua table {kind="scene", target_id="..."}
        tbl := ls.CheckTable(2)
        kind := tbl.RawGetString("kind").String()
        targetIDStr := tbl.RawGetString("target_id").String()
        targetID, err := ulid.Parse(targetIDStr)
        if err != nil {
            ls.Push(lua.LNil)
            ls.Push(lua.LString("INVALID_ULID: " + err.Error()))
            return 2
        }
        fk = &session.FocusKey{Kind: session.FocusKind(kind), TargetID: targetID}
    }
    isSceneGrid := ls.OptBool(3, false)
    if err := fo.SetConnectionFocus(ls.Context(), connID, fk, isSceneGrid); err != nil {
        ls.Push(lua.LNil)
        ls.Push(lua.LString(err.Error()))
        return 2
    }
    ls.Push(lua.LTrue)
    return 1
}

func autoFocusOnJoinFn(ls *lua.LState) int {
    fo := getFocusOps(ls)
    if fo == nil {
        ls.Push(lua.LNil)
        ls.Push(lua.LString("focus_ops not registered"))
        return 2
    }
    charIDStr := ls.CheckString(1)
    sceneIDStr := ls.CheckString(2)
    charID, err := ulid.Parse(charIDStr)
    if err != nil {
        ls.Push(lua.LNil)
        ls.Push(lua.LString("INVALID_ULID: " + err.Error()))
        return 2
    }
    sceneID, err := ulid.Parse(sceneIDStr)
    if err != nil {
        ls.Push(lua.LNil)
        ls.Push(lua.LString("INVALID_ULID: " + err.Error()))
        return 2
    }
    focused, skipped, failed, total, err := fo.AutoFocusOnJoin(ls.Context(), charID, sceneID)
    if err != nil {
        ls.Push(lua.LNil)
        ls.Push(lua.LString(err.Error()))
        return 2
    }
    // Return a Lua table mirroring the proto response shape so plugin
    // authors can render the 4 branches in §7.4.
    resp := ls.NewTable()
    focusedTbl := ls.NewTable()
    for i, id := range focused {
        focusedTbl.RawSetInt(i+1, lua.LString(id.String()))
    }
    resp.RawSetString("focused_connection_ids", focusedTbl)

    skippedTbl := ls.NewTable()
    for i, id := range skipped {
        skippedTbl.RawSetInt(i+1, lua.LString(id.String()))
    }
    resp.RawSetString("skipped_connection_ids", skippedTbl)

    failedTbl := ls.NewTable()
    for i, f := range failed {
        entry := ls.NewTable()
        entry.RawSetString("connection_id", lua.LString(f.ConnectionID.String()))
        entry.RawSetString("reason", lua.LString(f.Reason))
        failedTbl.RawSetInt(i+1, entry)
    }
    resp.RawSetString("failed_connection_ids", failedTbl)

    resp.RawSetString("total_connection_count", lua.LNumber(total))

    ls.Push(resp)
    return 1
}

func isAnyConnFocusedFn(ls *lua.LState) int {
    fo := getFocusOps(ls)
    if fo == nil {
        ls.Push(lua.LNil)
        ls.Push(lua.LString("focus_ops not registered"))
        return 2
    }
    charIDStr := ls.CheckString(1)
    sceneIDStr := ls.CheckString(2)
    charID, err := ulid.Parse(charIDStr)
    if err != nil {
        ls.Push(lua.LNil)
        ls.Push(lua.LString("INVALID_ULID: " + err.Error()))
        return 2
    }
    sceneID, err := ulid.Parse(sceneIDStr)
    if err != nil {
        ls.Push(lua.LNil)
        ls.Push(lua.LString("INVALID_ULID: " + err.Error()))
        return 2
    }
    focused, err := fo.IsAnyConnFocused(ls.Context(), charID, sceneID)
    if err != nil {
        ls.Push(lua.LNil)
        ls.Push(lua.LString(err.Error()))
        return 2
    }
    ls.Push(lua.LBool(focused))
    return 1
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
task test -- -run 'TestFocusHostfunc_PhaseFive|TestFocusHostfunc_ULIDRoundTrip' ./internal/plugin/hostfunc/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(hostfunc): 3 Phase 5 Lua bindings + ULID parity (5rh.14 T13)

INV-P5-6 + INV-P5-9 + D6: Lua bindings for set_connection_focus,
auto_focus_on_join, is_any_conn_focused. Accept 26-char ULID strings;
parse to bytes at the gRPC boundary. Lua returns (nil, err_string) on
INVALID_ULID per the existing convention.

Bead: holomush-5rh.14.13"
jj --no-pager new
```

---

## Phase D — Coordinator + RPC handlers (T14-T18)

### Task 14: `Coordinator.SetConnectionFocus` (INV-P5-1, INV-P5-13)

**Files:**

- Create: `internal/grpc/focus/set_connection_focus.go`
- Test: `internal/grpc/focus/set_connection_focus_test.go` (NEW)

- [ ] **Step 1: Write the failing tests**

Create `internal/grpc/focus/set_connection_focus_test.go` with at least:

- `TestSetConnectionFocus_HappyPath` — scene focus on member, terminal client; assert `Connection.FocusKey` + `Info.PresentingFocus` both written.
- `TestSetConnectionFocus_RequiresMembership` (INV-P5-1) — non-member → `FOCUS_WITHOUT_MEMBERSHIP`.
- `TestSetConnectionFocus_CommsHubDoesNotWritePresentingFocus` (D9) — same call from `comms_hub` client → only `Connection.FocusKey` writes, `Info.PresentingFocus` stays nil.
- `TestSceneGrid_DoesNotClearPresentingFocus` (INV-P5-13) — set explicit scene focus first; then call SetConnectionFocus with nil focus_key and `isSceneGrid=true`; assert `Info.PresentingFocus` unchanged.
- `TestSetConnectionFocus_ConnectionNotFound` — bad connID → `CONNECTION_NOT_FOUND`.

Use the existing focus test harness (`coordinator_test.go` patterns).

- [ ] **Step 2: Run tests to verify failure**

```bash
task test -- -run TestSetConnectionFocus ./internal/grpc/focus/
```

Expected: FAIL — method missing.

- [ ] **Step 3: Implement the method**

Create `internal/grpc/focus/set_connection_focus.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
    "context"

    "github.com/oklog/ulid/v2"
    "github.com/samber/oops"

    "github.com/holomush/holomush/internal/session"
)

// SetConnectionFocus is the Coordinator-side implementation of the
// Phase 5 SetConnectionFocus RPC. Delegates to a single
// Store.UpdateSessionConnection call so Connection.FocusKey +
// (D9-gated) Info.PresentingFocus commit atomically (D7, INV-P5-7).
// Returns oldFocusKey so the RPC handler (T18) can compute stream
// deltas via subscription_router (T10 helpers).
func (c *defaultCoordinator) SetConnectionFocus(
    ctx context.Context,
    connectionID ulid.ULID,
    focusKey *session.FocusKey,
    isSceneGrid bool,
) (oldFocusKey *session.FocusKey, err error) {
    // Look up connection to learn its session_id (we need it for the
    // Store call; the RPC carries only connection_id).
    conn, err := c.sessionStore.GetConnection(ctx, connectionID)
    if err != nil {
        return nil, err //nolint:wrapcheck // pass through existing codes
    }
    sessionID := conn.SessionID
    isTerminal := conn.ClientType == "terminal" || conn.ClientType == "telnet"

    m := session.NewSessionConnectionMutator(func(info session.Info, conn session.Connection) (session.Info, session.Connection, error) {
        // Membership validation (INV-P5-1) — only when focusing on a non-nil
        // scene-kind target.
        if focusKey != nil && focusKey.Kind == session.FocusKindScene {
            if !hasMembership(info.FocusMemberships, focusKey.Kind, focusKey.TargetID) {
                return info, conn, oops.Code("FOCUS_WITHOUT_MEMBERSHIP").
                    With("character_id", info.CharacterID.String()).
                    With("scene_id", focusKey.TargetID.String()).
                    Errorf("focus target not in session FocusMemberships")
            }
        }

        // Capture old focus for the post-commit subscription delta.
        if conn.FocusKey != nil {
            cpy := *conn.FocusKey
            oldFocusKey = &cpy
        }

        // Write Connection.FocusKey unconditionally.
        conn.FocusKey = focusKey

        // D9: only terminal/telnet explicit focus changes update PresentingFocus.
        // D10: scene grid is NEVER allowed to clear PresentingFocus.
        if isTerminal && !isSceneGrid {
            info.PresentingFocus = focusKey
        }
        return info, conn, nil
    })

    if err := c.sessionStore.UpdateSessionConnection(ctx, sessionID, connectionID, m); err != nil {
        return nil, err //nolint:wrapcheck
    }
    return oldFocusKey, nil
}

func hasMembership(memberships []session.FocusMembership, kind session.FocusKind, target ulid.ULID) bool {
    for _, fm := range memberships {
        if fm.Kind == kind && fm.TargetID == target {
            return true
        }
    }
    return false
}
```

After the mutator commits, the caller will invoke the `subscription_router` to compute stream deltas (T10 helpers) and call `SendToConnection` (T9). That wiring lands in T18 (RPC handler dispatch).

- [ ] **Step 4: Run tests to verify they pass**

```bash
task test -- -run TestSetConnectionFocus ./internal/grpc/focus/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(focus): Coordinator.SetConnectionFocus (5rh.14 T14)

INV-P5-1 (FocusMemberships validation D4), D7 single-mutator path,
D9 (terminal/telnet PresentingFocus write), INV-P5-13 (scene grid
preserves PresentingFocus). Stream deltas + SendToConnection wired
in T18.

Bead: holomush-5rh.14.14"
jj --no-pager new
```

---

### Task 15: `Coordinator.AutoFocusOnJoin` (INV-P5-4, INV-P5-11)

**Files:**

- Create: `internal/grpc/focus/auto_focus_on_join.go`
- Test: `internal/grpc/focus/auto_focus_on_join_test.go` (NEW)

- [ ] **Step 1: Write the failing tests**

Cover:

- `TestAutoFocus_HappyPath_TerminalOnly` — session with 1 terminal + 1 comms_hub conn; verify only terminal lands in `focused_connection_ids`.
- `TestAutoFocus_FiltersByClientType` (INV-P5-4) — session with telnet + comms_hub; assert comms_hub absent from focused.
- `TestAutoFocus_SkipsAlreadyExplicitlyFocusedConn` (INV-P5-11) — terminal already focused on scene A; AutoFocus to scene B → that conn lands in `skipped_connection_ids`, not focused.
- `TestAutoFocus_FailsForMembershipAbsent` — pre-call without JoinFocus → all conns fail with `FOCUS_FAILURE_REASON_MEMBERSHIP_ABSENT`.
- `TestAutoFocus_TotalConnectionCount` — session with 3 conns (2 terminal + 1 comms_hub); response total_connection_count == 3.

- [ ] **Step 2-4: Implement (Coordinator.AutoFocusOnJoin)**

Create `internal/grpc/focus/auto_focus_on_join.go`. Algorithm per spec §6.2:

1. `ListConnectionsBySession`.
2. Filter `ClientType ∈ {terminal, telnet}`.
3. For each filtered conn, call `Store.UpdateSessionConnection` with a mutator that:
   - Skip-rule: if `conn.FocusKey != nil && *conn.FocusKey != target` → return unchanged + record `skipped`.
   - Membership check: if `info.FocusMemberships` lacks the target → return `FOCUS_WITHOUT_MEMBERSHIP` + record `failed`.
   - Apply: set FocusKey + (terminal) PresentingFocus → record `focused`.
4. Return response.

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(focus): Coordinator.AutoFocusOnJoin (5rh.14 T15)

INV-P5-4 (terminal-only), INV-P5-11 (D8 skip-already-focused), §6.2
fan-out with focused / skipped / failed / total_connection_count
response. Mutator runs under one Store-lock per conn; concurrent
LeaveFocus produces FOCUS_FAILURE_REASON_MEMBERSHIP_ABSENT.

Bead: holomush-5rh.14.15"
jj --no-pager new
```

---

### Task 16: `Coordinator.IsAnyConnFocused`

**Files:**

- Create: `internal/grpc/focus/is_any_conn_focused.go`
- Test: `internal/grpc/focus/is_any_conn_focused_test.go` (NEW)

Cover:

- `TestIsAnyConnFocused_TrueWhenOneMatches`
- `TestIsAnyConnFocused_FalseWhenNoneMatch`
- `TestIsAnyConnFocused_FalseWhenSessionMissing`

Implementation: enumerate connections via `ListConnectionsBySession`; iterate FocusKey; return true on first match.

```go
func (c *defaultCoordinator) IsAnyConnFocused(
    ctx context.Context,
    characterID, sceneID ulid.ULID,
) (bool, error) {
    info, err := c.sessionStore.FindByCharacter(ctx, characterID)
    if err != nil {
        // "Character has no active session" surfaces as SESSION_NOT_FOUND
        // from both MemStore (memstore.go:82-84) and Postgres
        // (session_store.go:347-349). Translate to (false, nil) so the
        // plugin's notification-emission decision short-circuits cleanly
        // (spec §6.3: "if false → emit a notification").
        var oe oops.OopsError
        if errors.As(err, &oe) && oe.Code() == "SESSION_NOT_FOUND" {
            return false, nil
        }
        return false, err //nolint:wrapcheck
    }
    conns, err := c.sessionStore.ListConnectionsBySession(ctx, info.ID)
    if err != nil {
        return false, err //nolint:wrapcheck
    }
    for _, conn := range conns {
        if conn.FocusKey != nil && conn.FocusKey.Kind == session.FocusKindScene && conn.FocusKey.TargetID == sceneID {
            return true, nil
        }
    }
    return false, nil
}
```

Commit with `Bead: holomush-5rh.14.16`.

---

### Task 17: `Coordinator.RestoreConnectionFocus` (INV-P5-5, INV-P5-12)

**Files:**

- Create: `internal/grpc/focus/restore_connection_focus.go`
- Test: `internal/grpc/focus/restore_connection_focus_test.go` (NEW)

Cover:

- `TestRestoreConnectionFocus_RestoresFromPresentingFocus` — set `Info.PresentingFocus = &{scene, X}`; member; assert new Connection.FocusKey = X.
- `TestRestoreConnectionFocus_NoOpWhenPresentingFocusNil` — PresentingFocus nil; assert FocusKey stays nil.
- `TestReconnect_FallsBackToGridWhenMembershipRevoked` (INV-P5-5) — PresentingFocus set; FocusMembership removed; assert FocusKey stays nil + structured warning log.
- `TestReconnect_VsConcurrentLeave_Serializes` (INV-P5-12) — race the restoration against a LeaveFocus; both outcomes (leave-first / restoration-first) are valid; no corruption.

Implementation pattern matches §8: single `UpdateSessionConnection` mutator that reads `info.PresentingFocus` + `info.FocusMemberships` from one locked snapshot, decides via the three branches in spec §8 step 2.

**Imports** (new file `restore_connection_focus.go`):

```go
import (
    "context"
    "log/slog"

    "github.com/oklog/ulid/v2"

    "github.com/holomush/holomush/internal/session"
)
```

```go
func (c *defaultCoordinator) RestoreConnectionFocus(
    ctx context.Context, sessionID string, connectionID ulid.ULID,
) error {
    m := session.NewSessionConnectionMutator(func(info session.Info, conn session.Connection) (session.Info, session.Connection, error) {
        if info.PresentingFocus == nil {
            return info, conn, nil // grid default
        }
        pf := info.PresentingFocus
        if !hasMembership(info.FocusMemberships, pf.Kind, pf.TargetID) {
            // membership revoked while disconnected; log warning, fall back to grid
            slog.WarnContext(ctx, "scene.focus.restore_fallback_to_grid",
                "session_id", sessionID,
                "character_id", info.CharacterID.String(),
                "prior_presenting_focus", pf,
            )
            return info, conn, nil
        }
        cpy := *pf
        conn.FocusKey = &cpy
        return info, conn, nil
    })
    return c.sessionStore.UpdateSessionConnection(ctx, sessionID, connectionID, m)
}
```

Commit with `Bead: holomush-5rh.14.17`.

---

### Task 18: PluginHostService server-side RPC dispatch + subscription wiring

**Files:**

- Modify: `internal/grpc/server.go` (or wherever PluginHostService methods land — likely `internal/plugin/host_service.go` — find via `rg -l 'func.*JoinFocus.*PluginHostService'`)
- Test: same file's `_test.go`

- [ ] **Step 1: Locate the existing PluginHostService methods**

```bash
rg -l 'JoinFocus|PresentFocus' internal/ | grep -E '(host_service|grpc/server)'
```

- [ ] **Step 2-4: Add server-side handlers**

For each of `SetConnectionFocus`, `AutoFocusOnJoin`, `IsAnyConnFocused`:

1. Decode proto bytes ULID → `ulid.ULID`.
2. Delegate to the corresponding `Coordinator` method (T14-T16).
3. For `SetConnectionFocus`: AFTER the Coordinator returns successfully, also:
   - Read pre-mutation FocusKey (captured before the Coordinator call) — but actually the mutator already captured `oldFocusKey`; surface via a return value or have the Coordinator return both old + new.
   - Compute `computeFocusManagedStreams(oldFK, locID, gameID)` and `computeFocusManagedStreams(newFK, locID, gameID)`.
   - `streamDeltas` → drive `SendToConnection(sessionID, connID, {stream, add})` per delta.
4. Encode response.

Implementation detail: extend `Coordinator.SetConnectionFocus` to return `(oldFK, newFK *session.FocusKey, err error)` so the handler can run the subscription_router post-commit. Alternative: have the Coordinator method itself drive the SendToConnection call (couples it to stream_registry; cleaner if avoidable).

Pick the cleaner option:

```go
func (c *defaultCoordinator) SetConnectionFocus(
    ctx context.Context,
    connectionID ulid.ULID,
    focusKey *session.FocusKey,
    isSceneGrid bool,
) (oldFocusKey *session.FocusKey, err error) {
    // ... mutator as in T14, but captures conn.FocusKey BEFORE mutation
    // and returns it via the outer (oldFocusKey, err) return.
}
```

The RPC handler then drives subscription_router calls using `(oldFocusKey, focusKey)`.

Commit with `Bead: holomush-5rh.14.18`.

---

### Task 18b: ConnectionID plumbing through CommandRequest (precursor to T19)

**Files:**

- Modify: `pkg/plugin/command.go` (add `ConnectionID` field to `CommandRequest`)
- Modify: `internal/command/types.go` (add `connectionID` private field + `ConnectionID()` accessor on `CommandExecution`; add to `CommandExecutionConfig`)
- Modify: `internal/command/dispatcher.go:296-305` (pass `exec.ConnectionID().String()` into `pluginsdk.CommandRequest`)
- Trace upstream callers of `NewCommandExecution` / `CommandExecutionConfig` — every dispatcher / gateway / test fixture that constructs an execution needs the ConnectionID source
- Test: `internal/command/dispatcher_test.go` (extend)

**Why this is a precursor:** verified via `rg -n 'ConnectionID' pkg/plugin/ internal/command/` — `pluginsdk.CommandRequest` lacks a `ConnectionID` field (`pkg/plugin/command.go:26-35`) and `CommandExecution` has no `ConnectionID()` accessor (`internal/command/types.go:389-442`). T19's `req.ConnectionID` would not compile without this precursor.

- [ ] **Step 1: Write the failing test**

Append to `internal/command/dispatcher_test.go`:

```go
func TestDispatcher_PassesConnectionIDToPluginCommand(t *testing.T) {
    t.Parallel()
    // Verifies that CommandExecution.ConnectionID() flows into
    // pluginsdk.CommandRequest.ConnectionID at dispatch time. Phase 5
    // plugin commands (scene focus / scene grid) require this.

    connID := ulid.Make()
    // ... construct CommandExecution with connectionID via the
    // existing CommandExecutionConfig pattern (search for existing
    // setupExecForDispatchTest helpers; mirror them) ...

    var capturedCmd pluginsdk.CommandRequest
    deliverer := mockPluginDeliverer{
        deliver: func(ctx context.Context, name string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
            capturedCmd = cmd
            return &pluginsdk.CommandResponse{}, nil
        },
    }
    d := NewDispatcher(/* deps including deliverer */)

    require.NoError(t, d.Dispatch(/* exec etc. */))
    assert.Equal(t, connID.String(), capturedCmd.ConnectionID,
        "INV-P5 precursor: dispatcher MUST propagate ConnectionID to plugin CommandRequest")
}
```

- [ ] **Step 2: Run test to verify failure**

```bash
task test -- -run TestDispatcher_PassesConnectionIDToPluginCommand ./internal/command/
```

Expected: FAIL — `CommandRequest.ConnectionID` field doesn't exist yet (compile error first; once added, value will be empty).

- [ ] **Step 3: Add the field + accessor + wire-up**

Modify `pkg/plugin/command.go`:

```go
type CommandRequest struct {
    Command       string
    Args          string
    CharacterID   string
    CharacterName string
    LocationID    string
    SessionID     string
    PlayerID      string
    InvokedAs     string
    // ConnectionID is the ULID of the originating Connection. Added
    // for Phase 5 (holomush-5rh.14): per-Connection focus commands
    // (scene focus / scene grid) need to know which connection issued
    // the command. Always populated for plugin-routed commands;
    // empty string only for internal/test fixtures predating this
    // field.
    ConnectionID  string
}
```

Modify `internal/command/types.go::CommandExecution` (currently at `:389-442`):

```go
type CommandExecution struct {
    // existing fields...
    connectionID ulid.ULID // NEW: Phase 5
}

func (e *CommandExecution) ConnectionID() ulid.ULID { return e.connectionID }
```

Extend `CommandExecutionConfig` (`:353-388`) with a `ConnectionID ulid.ULID` field. Update `NewCommandExecution` (or equivalent constructor) to wire it through.

Update `internal/command/dispatcher.go:296-305`:

```go
cmd := pluginsdk.CommandRequest{
    Command:       entry.Name,
    Args:          exec.Args,
    CharacterID:   exec.CharacterID().String(),
    CharacterName: exec.CharacterName(),
    LocationID:    exec.LocationID().String(),
    SessionID:     exec.SessionID().String(),
    PlayerID:      exec.PlayerID().String(),
    InvokedAs:     invokedAs,
    ConnectionID:  exec.ConnectionID().String(), // NEW: Phase 5
}
```

Trace `CommandExecutionConfig` callers (gateway adapters, test fixtures) and pass through ConnectionID. Likely call sites: `internal/telnet/gateway_handler.go`, `internal/web/handler.go`, plus various test fixtures. For each, source ConnectionID from the originating Connection (telnet's per-conn handle; web's request-bound connection).

- [ ] **Step 4: Run tests to verify pass**

```bash
task test -- -run TestDispatcher_PassesConnectionIDToPluginCommand ./internal/command/
task test -- ./internal/command/ ./internal/telnet/ ./internal/web/
```

Expected: all pass (test fixtures may need ConnectionID values, even if synthetic).

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin-sdk): plumb ConnectionID through CommandRequest (5rh.14 T18b)

Adds Connection.ID propagation to pluginsdk.CommandRequest via
CommandExecution.ConnectionID() + dispatcher wiring. Phase 5
precursor: scene focus / scene grid commands need to know which
connection issued them.

Bead: holomush-5rh.14.18b"
jj --no-pager new
```

---

## Phase E — Plugin commands (T19-T22)

### Task 19: `scene focus #<id>` subcommand

**Files:**

- Modify: `plugins/core-scenes/commands.go` (replace `:840` placeholder + add handler)
- Test: `plugins/core-scenes/commands_focus_test.go` (NEW)

- [ ] **Step 1: Write the failing tests**

Create `plugins/core-scenes/commands_focus_test.go` covering:

- `TestHandleSceneFocus_HappyPath` — member calls `scene focus #42`; assert SetConnectionFocus called with `{scene, 42}` + `isSceneGrid=false`; render returns "Focused on scene #42.".
- `TestHandleSceneFocus_NotAMember` — non-member; assert plugin pre-check returns SCENE_FOCUS_NOT_A_MEMBER user error; no RPC fired.
- `TestHandleSceneFocus_InvalidSceneID` — bad ULID; assert SCENE_NOT_FOUND.

- [ ] **Step 2-4: Implement**

Locate the `:840` placeholder block. Replace with subcommand dispatch:

```go
switch sub {
case "focus":
    return p.handleSceneFocus(ctx, req, args)
case "grid":
    return p.handleSceneGrid(ctx, req, args)
case "list":
    return p.handleSceneList(ctx, req, args)
// existing pose/say/emit/ooc cases from Phase 4 continue
}
```

Add `handleSceneFocus`:

```go
func (p *scenePlugin) handleSceneFocus(ctx context.Context, req pluginsdk.CommandRequest, args []string) (*pluginsdk.CommandResponse, error) {
    if len(args) == 0 {
        return pluginsdk.Errorf(pluginsdk.UserError, "USAGE_SCENE_FOCUS", "scene focus #<id>"), nil
    }
    sceneIDStr := strings.TrimPrefix(args[0], "#")
    sceneID, err := ulid.Parse(sceneIDStr)
    if err != nil {
        return pluginsdk.Errorf(pluginsdk.UserError, "INVALID_SCENE_ID", "invalid scene id %q", args[0]), nil
    }

    // Plugin pre-check: IsParticipant (defense-in-depth; spec §7.1)
    ok, err := p.service.IsParticipant(ctx, sceneID.String(), req.CharacterID)
    if err != nil {
        return nil, oops.Code("SCENE_FOCUS_PARTICIPANT_CHECK_FAILED").Wrap(err)
    }
    if !ok {
        return pluginsdk.Errorf(pluginsdk.UserError, "SCENE_FOCUS_NOT_A_MEMBER", "you are not a participant of scene #%s", sceneID), nil
    }

    // Call substrate.
    fk := &session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}
    if err := p.pluginHost.SetConnectionFocus(ctx, req.ConnectionID, fk, false); err != nil {
        return nil, oops.Code("SCENE_FOCUS_SET_FAILED").Wrap(err)
    }

    return pluginsdk.Render("Focused on scene #" + sceneID.String() + "."), nil
}
```

(The plugin needs a typed handle to `pluginHost`'s SetConnectionFocus. Either via the existing focus_client wrapper or a new method added there.)

Commit with `Bead: holomush-5rh.14.19`.

---

### Task 20: `scene grid` subcommand

**Files:**

- Modify: `plugins/core-scenes/commands.go`
- Test: `plugins/core-scenes/commands_focus_test.go` (extend)

Implementation:

```go
func (p *scenePlugin) handleSceneGrid(ctx context.Context, req pluginsdk.CommandRequest, _ []string) (*pluginsdk.CommandResponse, error) {
    // D10: isSceneGrid=true so substrate skips PresentingFocus write.
    if err := p.pluginHost.SetConnectionFocus(ctx, req.ConnectionID, nil /* focus_key */, true /* isSceneGrid */); err != nil {
        return nil, oops.Code("SCENE_GRID_SET_FAILED").Wrap(err)
    }
    return pluginsdk.Render("Focused on the grid."), nil
}
```

Test pin: `TestHandleSceneGrid_PreservesPresentingFocus` — runs `scene focus #A` then `scene grid` and asserts `Info.PresentingFocus` still references scene A (INV-P5-13).

Commit with `Bead: holomush-5rh.14.20`.

---

### Task 21: `scene list` subcommand

**Files:**

- Modify: `plugins/core-scenes/commands.go`
- Test: `plugins/core-scenes/commands_focus_test.go` (extend)

Implementation: read `session.Info.FocusMemberships` (via existing plugin session-access path — see Phase 4 `resolveSingleSceneMembership`); for each scene-kind membership, call `IsAnyConnFocused`; render `[focused]` / `[background]`.

```go
func (p *scenePlugin) handleSceneList(ctx context.Context, req pluginsdk.CommandRequest, _ []string) (*pluginsdk.CommandResponse, error) {
    memberships, err := p.pluginHost.ListSceneMemberships(ctx, req.CharacterID)
    if err != nil {
        return nil, oops.Code("SCENE_LIST_MEMBERSHIPS_FAILED").Wrap(err)
    }
    if len(memberships) == 0 {
        return pluginsdk.Render("You are not in any scenes."), nil
    }
    var b strings.Builder
    fmt.Fprintf(&b, "Your scenes (%d):\n", len(memberships))
    for _, m := range memberships {
        focused, err := p.pluginHost.IsAnyConnFocused(ctx, req.CharacterID, m.SceneID)
        if err != nil {
            return nil, oops.Code("SCENE_LIST_FOCUS_CHECK_FAILED").Wrap(err)
        }
        marker := "[background]"
        if focused {
            marker = "[focused]"
        }
        fmt.Fprintf(&b, "  #%s %s %s\n", m.SceneID, m.Title, marker)
    }
    return pluginsdk.Render(b.String()), nil
}
```

Test pin: `TestHandleSceneList_RendersFocusedAndBackground`.

Commit with `Bead: holomush-5rh.14.21`.

---

### Task 22: `scene join` → `AutoFocusOnJoin` wiring + render

**Files:**

- Modify: `plugins/core-scenes/commands.go::handleJoin` (existing — spans `:350-410`; `JoinScene → JoinFocus` chain is at `:382-405` within that)
- Test: `plugins/core-scenes/commands_join_test.go` (extend or add new file)

- [ ] **Step 1: Locate existing `handleJoin`**

```bash
rg -n 'func.*handleJoin' plugins/core-scenes/commands.go
```

- [ ] **Step 2: Write the failing test**

Test cases:

- `TestHandleJoin_AutoFocusesTerminal` — terminal session; assert AutoFocusOnJoin called; render contains "and focused your terminal connection(s)".
- `TestHandleJoin_CommsHubOnly_PromptsScenefocus` — comms_hub session only; render contains "Use 'scene focus #X' to enter".
- `TestHandleJoin_AllExplicitlyElsewhere_PromptsSwitch` — terminal already on a different scene; render mentions current focus preserved.

- [ ] **Step 3-4: Wire the call**

After the existing `service.JoinScene` and `focusClient.JoinFocus` calls succeed, add:

```go
resp, err := p.pluginHost.AutoFocusOnJoin(ctx, charID, sceneID)
if err != nil {
    return nil, oops.Code("SCENE_JOIN_AUTOFOCUS_FAILED").Wrap(err)
}
var msg string
switch {
case len(resp.FocusedConnectionIds) > 0:
    msg = fmt.Sprintf("Joined scene #%s and focused your terminal connection(s) on it.", sceneID)
case resp.TotalConnectionCount == 0:
    msg = fmt.Sprintf("Joined scene #%s.", sceneID)
case len(resp.SkippedConnectionIds) > 0:
    msg = fmt.Sprintf("Joined scene #%s. Your terminal stays on its current focus; use 'scene focus #%s' to switch.", sceneID, sceneID)
default:
    msg = fmt.Sprintf("Joined scene #%s. Use 'scene focus #%s' to enter.", sceneID, sceneID)
}
return pluginsdk.Render(msg), nil
```

Commit with `Bead: holomush-5rh.14.22`.

---

## Phase F — Integration tests (T23-T26)

### Task 23: `focus_without_membership_blocked_test.go` (INV-P5-1 e2e)

**Files:**

- Create: `test/integration/scenes/focus_without_membership_blocked_test.go`

Pattern: re-use the suite scaffolding from `test/integration/scenes/suite_test.go` (Phase 4 T25). Construct alice without scene membership; call SetConnectionFocus with a scene target; assert PermissionDenied + `FOCUS_WITHOUT_MEMBERSHIP`. Then JoinFocus + retry → succeeds.

Commit with `Bead: holomush-5rh.14.23`.

---

### Task 24: `auto_focus_on_join_terminal_only_test.go` (INV-P5-4 + INV-P5-11)

**Files:**

- Create: `test/integration/scenes/auto_focus_on_join_terminal_only_test.go`

Cover three sub-scenarios in one Ginkgo Describe:

1. Session with telnet + comms_hub conns → AutoFocus → only telnet in `focused_connection_ids` (INV-P5-4).
2. Session with two terminal conns, one already on scene A → AutoFocus to scene B → second terminal in `focused`, first in `skipped` (INV-P5-11).
3. Failure-reason coverage: pre-call AutoFocus before JoinFocus → conns in `failed_connection_ids` with reason `MEMBERSHIP_ABSENT`.

Commit with `Bead: holomush-5rh.14.24`.

---

### Task 25: `reconnect_focus_restoration_test.go` (INV-P5-5, INV-P5-12, INV-P5-13)

**Files:**

- Create: `test/integration/scenes/reconnect_focus_restoration_test.go`

Cover:

- `TestReconnect_FallsBackToGridWhenMembershipRevoked` — set PresentingFocus, remove FocusMembership, reconnect a new conn → FocusKey nil + warning log captured.
- `TestReconnect_VsConcurrentLeave_Serializes` — race the restoration vs LeaveFocus; assert both possible outcomes (leave-first → grid; restoration-first → scene_leave_ic observed on the wire).
- `TestReconnect_AfterSceneGrid_RestoresPriorScene` (INV-P5-13) — `scene focus #A` then `scene grid` then disconnect+reconnect → new conn restores to scene A (PresentingFocus preserved).

Commit with `Bead: holomush-5rh.14.25`.

---

### Task 26: `multi_connection_visibility_test.go` (INV-P5-10 wire-level)

**Files:**

- Create: `test/integration/scenes/multi_connection_visibility_test.go`

Pattern (extends T25 `non_participant_ic_isolation_test.go`): alice has 2 connections — telnet on grid + web on scene #42. Substrate emits a scene_pose event to scene #42 IC. Assert: web receives via its scene IC subscription; telnet does NOT (its filter set doesn't include scene #42 IC). This pins INV-P5-10 end-to-end on the wire.

Commit with `Bead: holomush-5rh.14.26`.

---

## Phase G — Coverage meta-test (T27)

### Task 27: INV-P5-8 self-pinning meta-test

**Files:**

- Create: `internal/test/invariants/inv_p5_coverage_meta_test.go`
- Pattern: Phase 4 T28 (`internal/test/invariants/inv_p4_coverage_meta_test.go`) — corpus-walk via `go/parser`; static cases table mapping each INV-P5-N to a test path.

- [ ] **Step 1: Read Phase 4's pattern**

```bash
rg -n 'INV_P4_Coverage' internal/test/invariants/
```

- [ ] **Step 2: Build the cases table**

Each INV-P5-N (1-14, excluding 8) maps to the test cited in spec §10. Example:

```go
var inv_p5_cases = []invCase{
    {"INV-P5-1", "internal/grpc/focus/set_connection_focus_test.go", "TestSetConnectionFocus_RequiresMembership"},
    {"INV-P5-2", "internal/session/connection_test.go", "TestConnection_FocusKeyNilByDefault"},
    {"INV-P5-3", "internal/grpc/focus/subscription_router_test.go", "TestComputeFocusManagedStreams_Deterministic"},
    {"INV-P5-4", "internal/grpc/focus/auto_focus_on_join_test.go", "TestAutoFocus_FiltersByClientType"},
    {"INV-P5-5", "internal/session/reconnect_focus_restoration_test.go", "TestReconnect_FallsBackToGridWhenMembershipRevoked"},
    {"INV-P5-6", "internal/plugin/hostfunc/stdlib_focus_test.go", "TestFocusHostfunc_PhaseFive_LuaParity"},
    {"INV-P5-7", "internal/grpc/focus/coordinator_test.go", "TestSetConnectionFocus_RoutesViaCoordinator"},
    // INV-P5-8 — meta-test references itself; excluded per no-circular convention.
    {"INV-P5-9", "internal/plugin/hostfunc/stdlib_focus_test.go", "TestFocusHostfunc_ULIDRoundTrip"},
    {"INV-P5-10", "internal/grpc/stream_registry_test.go", "TestSendToConnection_TargetsOneConnectionOnly"},
    {"INV-P5-11", "internal/grpc/focus/auto_focus_on_join_test.go", "TestAutoFocus_SkipsAlreadyExplicitlyFocusedConn"},
    {"INV-P5-12", "internal/session/reconnect_focus_restoration_test.go", "TestReconnect_VsConcurrentLeave_Serializes"},
    {"INV-P5-13", "internal/grpc/focus/set_connection_focus_test.go", "TestSceneGrid_DoesNotClearPresentingFocus"},
    {"INV-P5-14", "internal/store/session_store_integration_test.go", "TestPostgresUpdateSessionConnection_LockAcquisitionOrder_NoDeadlock"},
}
```

Test body walks the file corpus via `go/parser`, confirms each cited file exists, parses it, and asserts the cited test function name is declared. Phase 4's `inv_p4_coverage_meta_test.go` provides the exact pattern.

Commit with `Bead: holomush-5rh.14.27`.

---

## Phase H — Documentation (T28)

### Task 28: Documentation updates

**Files:**

- Modify: `docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md` (§11 — strike through the "Plugin → server focus model integration" row with a "Closed: Phase 5 (holomush-5rh.14)" note).
- Modify: `site/docs/` — any pages that describe scene commands; add `scene focus`, `scene grid`, `scene list`.

Specific edits:

- v2 §11 BLOCKER row: append `Closed by holomush-5rh.14 (Phase 5 design + impl, merged 2026-05-2X).`
- site/docs/guide/scenes.md (or equivalent player-facing doc) — add the three new commands.
- site/docs/extending/plugin-api.md — add the 3 new PluginHostService RPCs to the inventory.

Run `task lint:docs` to verify rumdl.

Commit with `Bead: holomush-5rh.14.28`.

---

## Phase I — pr-prep + push + PR (T29)

### Task 29: pr-prep + push + open PR

**Files:** none (orchestration only)

- [ ] **Step 1: Run reviewer gates** (per CLAUDE.md pre-push reviewer-gate reminders)

Dispatch in parallel:

- `code-reviewer` on branch diff
- `abac-reviewer` (Phase 5 touches I-17 substrate gate via subscription routing — though no NEW ABAC decisions, just confirm)
- `crypto-reviewer` (Phase 5 does NOT touch the crypto surface — verify no accidental emit-side changes; this should be a quick clean pass)

Address any blocking findings inline; file P3 cleanup bead for non-blocking nits (5rh.14 follow-up).

- [ ] **Step 2: Run `task pr-prep` to FULL completion**

```bash
task pr-prep
```

Expected: `✓ All PR checks passed.` Never approximate; full lane (lint + format + schema + license + unit + integration + E2E).

- [ ] **Step 3: Set bookmark + push**

```bash
jj --no-pager git fetch
jj --no-pager bookmark set 5rh-14-phase5-impl -r @-
jj --no-pager git push --allow-new --branch 5rh-14-phase5-impl
```

- [ ] **Step 4: Open PR**

```bash
GH_REPO='holomush/holomush' gh pr create --head 5rh-14-phase5-impl --base main \
  --title "Scenes Phase 5: Focus model + multi-connection visibility (5rh.14)" \
  --body "<see Phase 4 PR #4153 template; fill in INV-P5-* count, decision summary, follow-ups holomush-3d9o, etc.>"
```

- [ ] **Step 5: Close beads**

```bash
bd close holomush-5rh.14.29 --reason="PR #<N> opened: <url>. All 14 INV-P5-* pinned + meta-test."
# Verify all 5rh.14.* children closed.
bd list --parent holomush-5rh.14 --status open --json | jq 'length == 0'
# Close the epic.
bd close holomush-5rh.14 --reason="Phase 5 merged in PR #<N>; closes v2 §11 plugin↔server focus contract BLOCKER."
bd dolt push
```

Commit (for this task itself — the orchestration step) with `Bead: holomush-5rh.14.29`.

---

## Post-implementation checklist

- [ ] All `### Task N:` checkboxes complete.
- [ ] `task pr-prep` green (full lane, no approximation).
- [ ] All 14 INV-P5-* invariants have passing tests pinned in §10 of the spec.
- [ ] The INV-P5-8 meta-test enforces coverage discipline at build time.
- [ ] `holomush-3d9o` (reconnect-fallback in-band signal) remains open as the explicit Phase 5 out-of-scope follow-up.
- [ ] v2 §11 BLOCKER row marked closed.
- [ ] PR opened against main; bead `holomush-5rh.14` epic + all children closed.
- [ ] Workspace `5rh-14-phase5-brainstorm` (or successor `5rh-14-phase5-impl`) cleaned up post-merge per CLAUDE.md "Landing the Plane".

<!-- adr-capture: sha256=ddca1d7409bb2ff3; ts=2026-05-22T01:38:33Z; adrs=holomush-kuf8,holomush-x0ph,holomush-nki4,holomush-8new -->
