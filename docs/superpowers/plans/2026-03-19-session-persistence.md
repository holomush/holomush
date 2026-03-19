<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Server-Side Session Persistence — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Postgres-backed session persistence with tmux-style detach/reattach, event replay on reconnect, command history, two-phase login with character selection, grid presence phasing, and multi-connection support. This is sub-spec 2a of Epic 8 (Web Client) — both terminal mode (2b) and chat mode (2c) depend on it.

**Architecture:** Sessions move from the in-memory `grpc.SessionInfo`/`grpc.InMemorySessionStore` to a new `internal/session` package with a `session.Store` interface backed by `PostgresSessionStore`. The `CoreServer` switches to the new interface. On disconnect, sessions detach instead of being deleted. A background reaper cleans expired sessions. Reconnecting clients get missed events replayed via the existing `EventStore.Replay` method with concurrent broadcaster draining for gap-free delivery. Login splits into Authenticate + SelectCharacter with player tokens for character switching.

**Tech Stack:** Go 1.24, pgxpool, oops, ulid, testify, Ginkgo/Gomega, testcontainers-go, protobuf

**Spec:** `docs/specs/2026-03-19-session-persistence-design.md`

**Epic:** `holomush-qve`

---

## File Structure

### New Files

| File                                                                  | Responsibility                                                                        |
| --------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| `internal/session/session.go`                                         | `Info`, `Status`, `Connection` types and `Store` interface                            |
| `internal/session/session_test.go`                                    | Unit tests for types (Status validation, Info construction)                           |
| `internal/session/memstore.go`                                        | In-memory `Store` implementation (test helper, migrated from `grpc.InMemorySessionStore`) |
| `internal/session/memstore_test.go`                                   | Unit tests for memstore CRUD, FindByCharacter, connection tracking                    |
| `internal/store/migrations/000018_sessions.up.sql`                    | Create `sessions`, `session_connections`, `player_tokens` tables                      |
| `internal/store/migrations/000018_sessions.down.sql`                  | Drop `sessions`, `session_connections`, `player_tokens` tables                        |
| `internal/store/session_store.go`                                     | `PostgresSessionStore` implementing `session.Store`                                   |
| `internal/store/session_store_test.go`                                | Unit tests with mock pool                                                             |
| `internal/store/session_store_integration_test.go`                    | Integration tests with testcontainers PostgreSQL                                      |
| `internal/session/reaper.go`                                          | `SessionReaper` background goroutine for expired session cleanup                      |
| `internal/session/reaper_test.go`                                     | Unit tests for reaper with mock store                                                 |
| `internal/auth/player_token.go`                                       | `PlayerToken` struct, `PlayerTokenRepository` interface                               |
| `internal/auth/player_token_test.go`                                  | Unit tests for PlayerToken creation and validation                                    |
| `internal/store/player_token_store.go`                                | `PostgresPlayerTokenStore` implementing `PlayerTokenRepository`                       |
| `internal/store/player_token_store_test.go`                           | Unit tests for player token CRUD                                                      |
| `test/integration/session/session_persistence_integration_test.go`    | BDD integration tests: reconnect flow, cross-protocol, TTL expiration                 |
| `test/integration/session/session_persistence_suite_test.go`          | Ginkgo bootstrap for session persistence test suite                                   |

### Modified Files

| File                                                    | Change                                                                                                              |
| ------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| `internal/grpc/server.go:38-102`                        | Delete `SessionStore` interface, `SessionInfo` struct, `InMemorySessionStore`; import `session.Store`               |
| `internal/grpc/server.go:89-102`                        | Change `CoreServer.sessionStore` field type from `SessionStore` to `session.Store`                                  |
| `internal/grpc/server.go:129-143`                       | Update constructor to accept `session.Store`; remove `NewInMemorySessionStore()` default                            |
| `internal/grpc/server.go:145-220`                       | Update `Authenticate` to use `session.Store.Set` with context + `session.Info`                                      |
| `internal/grpc/server.go:222-298`                       | Update `HandleCommand` to use `session.Store.Get` with context                                                      |
| `internal/grpc/server.go:301-382`                       | Update `Subscribe` to support `replay_from_cursor`; add replay-before-live merge logic                              |
| `internal/grpc/server.go:385-443`                       | Update `Disconnect` to detach session instead of delete; track connections                                          |
| `cmd/holomush/core.go:48-56`                            | Add session config fields to `coreConfig` (session\_ttl, session\_max\_history, etc.)                               |
| `cmd/holomush/core.go:351-371`                          | Pass `session.Store` to `NewCoreServer`; start `SessionReaper`; add reaper to shutdown                              |
| `cmd/holomush/deps.go:39-69`                            | Add `SessionStoreFactory` to `CoreDeps`                                                                             |
| `api/proto/holomush/core/v1/core.proto:67-71`           | Add `replay_from_cursor` field to `SubscribeRequest`                                                                |
| `api/proto/holomush/web/v1/web.proto`                   | Add `Authenticate`, `ListCharacters`, `SelectCharacter`, `ListSessions`, `GetCommandHistory` RPCs and message types |
| `internal/web/handler.go`                               | Add new RPC handler methods; update `StreamEvents` to pass `replay_from_cursor`                                     |
| `internal/auth/player.go:69-73`                         | Add `AutoConnectMode` to `PlayerPreferences`                                                                        |
| `internal/auth/auth_service.go`                         | Add `AuthenticatePlayer` method (returns player token + character list)                                             |
| `internal/config/config.go:26-28`                       | Add session config fields to `GameConfig` or new `SessionConfig` struct                                             |
| `internal/core/session.go`                              | Add `GridPresent` field to `Session`; update `SessionService` with grid presence methods                            |

### Unchanged (Reference)

| File                                      | Why Referenced                                                                        |
| ----------------------------------------- | ------------------------------------------------------------------------------------- |
| `internal/core/store.go`                  | `EventStore.Replay` — used for event replay on reconnect                              |
| `internal/core/broadcaster.go`            | `Broadcaster.Subscribe` — buffer size 100, concurrent drain required during replay    |
| `internal/store/postgres.go`              | Pattern for `pgxpool`-based store implementations + `poolIface`                       |
| `internal/grpc/client.go`                 | `Client` implements core RPCs — web handler reuses `GRPCClient` interface             |
| `internal/telnet/gateway_handler.go`      | Telnet integration point — will use same `session.Store` for cross-protocol sessions  |
| `cmd/holomush/deps.go:93-97`             | `EventStore` interface pattern — new `SessionStoreFactory` follows same approach      |

---

## Chunk 0: Session Package + Interface

This chunk creates the `internal/session` package with core types and the `Store` interface. The in-memory implementation moves here from `internal/grpc/server.go` and is expanded to match the new interface. This is the foundation that all subsequent chunks build on.

### Task 0a: Create Session Types and Store Interface

**Files:**

- Create: `internal/session/session.go`
- Create: `internal/session/session_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/session/session_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStatus_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		want   bool
	}{
		{"active", StatusActive, true},
		{"detached", StatusDetached, true},
		{"expired", StatusExpired, true},
		{"empty", Status(""), false},
		{"unknown", Status("unknown"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.IsValid())
		})
	}
}

func TestStatus_String(t *testing.T) {
	assert.Equal(t, "active", StatusActive.String())
	assert.Equal(t, "detached", StatusDetached.String())
	assert.Equal(t, "expired", StatusExpired.String())
}

func TestInfo_IsDetachable(t *testing.T) {
	info := &Info{Status: StatusActive}
	assert.True(t, info.IsActive())

	info.Status = StatusDetached
	assert.False(t, info.IsActive())
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/session/ -run TestStatus -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Implement session types**

Create `internal/session/session.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package session provides types and interfaces for persistent game sessions.
// Sessions track a character's ongoing presence, survive disconnects, and
// support replay of missed events on reconnect.
package session

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
)

// Status represents the lifecycle state of a session.
type Status string

const (
	StatusActive   Status = "active"
	StatusDetached Status = "detached"
	StatusExpired  Status = "expired"
)

// IsValid returns true if the status is a recognized value.
func (s Status) IsValid() bool {
	switch s {
	case StatusActive, StatusDetached, StatusExpired:
		return true
	default:
		return false
	}
}

// String returns the status as a string.
func (s Status) String() string {
	return string(s)
}

// Info contains all state for a persistent game session.
type Info struct {
	ID            string
	CharacterID   ulid.ULID
	CharacterName string
	LocationID    ulid.ULID
	IsGuest       bool
	Status        Status
	GridPresent   bool
	EventCursors  map[string]ulid.ULID
	CommandHistory []string
	TTLSeconds    int
	MaxHistory    int
	DetachedAt    *time.Time
	ExpiresAt     *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// IsActive returns true if the session is in active status.
func (i *Info) IsActive() bool {
	return i.Status == StatusActive
}

// IsExpired returns true if the session has passed its expiry time.
func (i *Info) IsExpired() bool {
	if i.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*i.ExpiresAt)
}

// Connection represents a single client attached to a session.
type Connection struct {
	ID         ulid.ULID
	SessionID  string
	ClientType string   // "terminal", "comms_hub", "telnet"
	Streams    []string // event streams this connection subscribes to
	ConnectedAt time.Time
}

// Store manages persistent session state. Implementations MUST be
// safe for concurrent use.
type Store interface {
	// Get retrieves a session by ID.
	Get(ctx context.Context, id string) (*Info, error)

	// Set creates or updates a session.
	Set(ctx context.Context, id string, info *Info) error

	// Delete removes a session.
	Delete(ctx context.Context, id string) error

	// FindByCharacter returns the active or detached session for a character.
	FindByCharacter(ctx context.Context, characterID ulid.ULID) (*Info, error)

	// ListByPlayer returns all non-expired sessions for a player's characters.
	ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*Info, error)

	// ListExpired returns all sessions past their expiry time.
	ListExpired(ctx context.Context) ([]*Info, error)

	// UpdateStatus transitions a session's status.
	UpdateStatus(ctx context.Context, id string, status Status,
		detachedAt *time.Time, expiresAt *time.Time) error

	// ReattachCAS atomically transitions a detached session to active.
	// Returns true if the row was updated, false if another client won the race.
	ReattachCAS(ctx context.Context, id string) (bool, error)

	// UpdateCursors updates the event cursors for a session.
	UpdateCursors(ctx context.Context, id string, cursors map[string]ulid.ULID) error

	// AppendCommand adds a command to the session's history, enforcing the cap.
	AppendCommand(ctx context.Context, id string, command string, maxHistory int) error

	// GetCommandHistory returns the session's command history.
	GetCommandHistory(ctx context.Context, id string) ([]string, error)

	// AddConnection registers a new connection to a session.
	AddConnection(ctx context.Context, conn *Connection) error

	// RemoveConnection removes a connection from a session.
	RemoveConnection(ctx context.Context, connectionID ulid.ULID) error

	// CountConnections returns the number of active connections for a session.
	CountConnections(ctx context.Context, sessionID string) (int, error)

	// CountConnectionsByType returns the number of active connections of a
	// specific client type for a session.
	CountConnectionsByType(ctx context.Context, sessionID string, clientType string) (int, error)
}
```

- [ ] **Step 4: Run to verify tests pass**

Run: `go test ./internal/session/ -run TestStatus -v && go test ./internal/session/ -run TestInfo -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
jj commit -m "feat(session): introduce session package with Info, Status, Connection types and Store interface

New internal/session package provides the foundation for persistent
game sessions. Types model session lifecycle (active/detached/expired),
multi-connection tracking, and the Store interface for Postgres-backed
persistence with cache-aside readiness."
```

### Task 0b: Create In-Memory Session Store

**Files:**

- Create: `internal/session/memstore.go`
- Create: `internal/session/memstore_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/session/memstore_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemStore_GetSet(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	info := &Info{
		ID:            "session-1",
		CharacterID:   ulid.Make(),
		CharacterName: "TestChar",
		Status:        StatusActive,
		EventCursors:  map[string]ulid.ULID{},
	}

	require.NoError(t, store.Set(ctx, "session-1", info))

	got, err := store.Get(ctx, "session-1")
	require.NoError(t, err)
	assert.Equal(t, "TestChar", got.CharacterName)
}

func TestMemStore_Get_NotFound(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent")
	assert.Error(t, err)
}

func TestMemStore_Delete(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	info := &Info{ID: "session-1", Status: StatusActive}
	require.NoError(t, store.Set(ctx, "session-1", info))
	require.NoError(t, store.Delete(ctx, "session-1"))

	_, err := store.Get(ctx, "session-1")
	assert.Error(t, err)
}

func TestMemStore_FindByCharacter(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()
	charID := ulid.Make()

	info := &Info{
		ID:          "session-1",
		CharacterID: charID,
		Status:      StatusDetached,
	}
	require.NoError(t, store.Set(ctx, "session-1", info))

	got, err := store.FindByCharacter(ctx, charID)
	require.NoError(t, err)
	assert.Equal(t, "session-1", got.ID)
}

func TestMemStore_FindByCharacter_SkipsExpired(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()
	charID := ulid.Make()

	info := &Info{
		ID:          "session-1",
		CharacterID: charID,
		Status:      StatusExpired,
	}
	require.NoError(t, store.Set(ctx, "session-1", info))

	_, err := store.FindByCharacter(ctx, charID)
	assert.Error(t, err)
}

func TestMemStore_ReattachCAS(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	info := &Info{ID: "session-1", Status: StatusDetached}
	require.NoError(t, store.Set(ctx, "session-1", info))

	ok, err := store.ReattachCAS(ctx, "session-1")
	require.NoError(t, err)
	assert.True(t, ok)

	got, err := store.Get(ctx, "session-1")
	require.NoError(t, err)
	assert.Equal(t, StatusActive, got.Status)

	// Second CAS fails — already active
	ok, err = store.ReattachCAS(ctx, "session-1")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestMemStore_ConnectionTracking(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	info := &Info{ID: "session-1", Status: StatusActive}
	require.NoError(t, store.Set(ctx, "session-1", info))

	connID := ulid.Make()
	conn := &Connection{
		ID:         connID,
		SessionID:  "session-1",
		ClientType: "terminal",
		Streams:    []string{"location:abc"},
		ConnectedAt: time.Now(),
	}
	require.NoError(t, store.AddConnection(ctx, conn))

	count, err := store.CountConnections(ctx, "session-1")
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	require.NoError(t, store.RemoveConnection(ctx, connID))

	count, err = store.CountConnections(ctx, "session-1")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestMemStore_AppendCommand(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	info := &Info{ID: "session-1", Status: StatusActive, CommandHistory: []string{}}
	require.NoError(t, store.Set(ctx, "session-1", info))

	require.NoError(t, store.AppendCommand(ctx, "session-1", "say hello", 3))
	require.NoError(t, store.AppendCommand(ctx, "session-1", "pose waves", 3))
	require.NoError(t, store.AppendCommand(ctx, "session-1", "look", 3))
	require.NoError(t, store.AppendCommand(ctx, "session-1", "say bye", 3))

	history, err := store.GetCommandHistory(ctx, "session-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"pose waves", "look", "say bye"}, history)
}

func TestMemStore_ListExpired(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	past := time.Now().Add(-1 * time.Hour)
	info := &Info{
		ID:        "session-1",
		Status:    StatusDetached,
		ExpiresAt: &past,
	}
	require.NoError(t, store.Set(ctx, "session-1", info))

	expired, err := store.ListExpired(ctx)
	require.NoError(t, err)
	assert.Len(t, expired, 1)
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/session/ -run TestMemStore -v`
Expected: FAIL — `NewMemStore` not defined

- [ ] **Step 3: Implement MemStore**

Create `internal/session/memstore.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session

import (
	"context"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// MemStore is an in-memory implementation of Store for testing.
type MemStore struct {
	mu          sync.RWMutex
	sessions    map[string]*Info
	connections map[ulid.ULID]*Connection // keyed by connection ID
}

// NewMemStore creates a new in-memory session store.
func NewMemStore() *MemStore {
	return &MemStore{
		sessions:    make(map[string]*Info),
		connections: make(map[ulid.ULID]*Connection),
	}
}

// compile-time check that MemStore implements Store.
var _ Store = (*MemStore)(nil)

func (m *MemStore) Get(_ context.Context, id string) (*Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info, ok := m.sessions[id]
	if !ok {
		return nil, oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	return copyInfo(info), nil
}

func (m *MemStore) Set(_ context.Context, id string, info *Info) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sessions[id] = copyInfo(info)
	return nil
}

func (m *MemStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.sessions, id)
	// Also remove associated connections
	for connID, conn := range m.connections {
		if conn.SessionID == id {
			delete(m.connections, connID)
		}
	}
	return nil
}

func (m *MemStore) FindByCharacter(_ context.Context, characterID ulid.ULID) (*Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, info := range m.sessions {
		if info.CharacterID == characterID &&
			(info.Status == StatusActive || info.Status == StatusDetached) {
			return copyInfo(info), nil
		}
	}
	return nil, oops.Code("SESSION_NOT_FOUND").
		With("character_id", characterID.String()).
		Errorf("no active or detached session for character")
}

func (m *MemStore) ListByPlayer(_ context.Context, _ ulid.ULID) ([]*Info, error) {
	// MemStore does not track player-to-character relationships.
	// This is a stub that returns all non-expired sessions.
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Info
	for _, info := range m.sessions {
		if info.Status != StatusExpired {
			result = append(result, copyInfo(info))
		}
	}
	return result, nil
}

func (m *MemStore) ListExpired(_ context.Context) ([]*Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	var result []*Info
	for _, info := range m.sessions {
		if info.Status == StatusDetached && info.ExpiresAt != nil && now.After(*info.ExpiresAt) {
			result = append(result, copyInfo(info))
		}
	}
	return result, nil
}

func (m *MemStore) UpdateStatus(_ context.Context, id string, status Status,
	detachedAt *time.Time, expiresAt *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[id]
	if !ok {
		return oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	info.Status = status
	info.DetachedAt = detachedAt
	info.ExpiresAt = expiresAt
	info.UpdatedAt = time.Now()
	return nil
}

func (m *MemStore) ReattachCAS(_ context.Context, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[id]
	if !ok {
		return false, oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	if info.Status != StatusDetached {
		return false, nil
	}
	info.Status = StatusActive
	info.DetachedAt = nil
	info.ExpiresAt = nil
	info.UpdatedAt = time.Now()
	return true, nil
}

func (m *MemStore) UpdateCursors(_ context.Context, id string, cursors map[string]ulid.ULID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[id]
	if !ok {
		return oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	if info.EventCursors == nil {
		info.EventCursors = make(map[string]ulid.ULID)
	}
	for k, v := range cursors {
		info.EventCursors[k] = v
	}
	return nil
}

func (m *MemStore) AppendCommand(_ context.Context, id string, command string, maxHistory int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[id]
	if !ok {
		return oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	info.CommandHistory = append(info.CommandHistory, command)
	if len(info.CommandHistory) > maxHistory {
		info.CommandHistory = info.CommandHistory[len(info.CommandHistory)-maxHistory:]
	}
	return nil
}

func (m *MemStore) GetCommandHistory(_ context.Context, id string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info, ok := m.sessions[id]
	if !ok {
		return nil, oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	result := make([]string, len(info.CommandHistory))
	copy(result, info.CommandHistory)
	return result, nil
}

func (m *MemStore) AddConnection(_ context.Context, conn *Connection) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.connections[conn.ID] = conn
	return nil
}

func (m *MemStore) RemoveConnection(_ context.Context, connectionID ulid.ULID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.connections, connectionID)
	return nil
}

func (m *MemStore) CountConnections(_ context.Context, sessionID string) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, conn := range m.connections {
		if conn.SessionID == sessionID {
			count++
		}
	}
	return count, nil
}

func (m *MemStore) CountConnectionsByType(_ context.Context, sessionID string, clientType string) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, conn := range m.connections {
		if conn.SessionID == sessionID && conn.ClientType == clientType {
			count++
		}
	}
	return count, nil
}

// copyInfo returns a defensive copy of an Info to prevent external modification.
func copyInfo(info *Info) *Info {
	cursors := make(map[string]ulid.ULID, len(info.EventCursors))
	for k, v := range info.EventCursors {
		cursors[k] = v
	}
	history := make([]string, len(info.CommandHistory))
	copy(history, info.CommandHistory)

	cp := *info
	cp.EventCursors = cursors
	cp.CommandHistory = history
	return &cp
}
```

- [ ] **Step 4: Run to verify tests pass**

Run: `go test ./internal/session/ -v`
Expected: PASS

- [ ] **Step 5: Run linter**

Run: `task lint`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
jj commit -m "feat(session): add MemStore in-memory implementation of session.Store

Thread-safe in-memory session store for use in tests. Implements
all Store interface methods including FindByCharacter, ReattachCAS,
connection tracking, and command history with cap enforcement."
```

---

## Chunk 1: Database Migration + Postgres Store

### Task 1a: Create Database Migration

**Files:**

- Create: `internal/store/migrations/000018_sessions.up.sql`
- Create: `internal/store/migrations/000018_sessions.down.sql`

- [ ] **Step 1: Create up migration**

Create `internal/store/migrations/000018_sessions.up.sql`:

```sql
-- sessions: persistent game sessions that survive disconnects
CREATE TABLE sessions (
    id              TEXT PRIMARY KEY,
    character_id    TEXT NOT NULL,
    character_name  TEXT NOT NULL,
    location_id     TEXT NOT NULL,
    is_guest        BOOLEAN NOT NULL DEFAULT false,
    status          TEXT NOT NULL DEFAULT 'active',
    grid_present    BOOLEAN NOT NULL DEFAULT false,
    event_cursors   JSONB NOT NULL DEFAULT '{}',
    command_history TEXT[] NOT NULL DEFAULT '{}',
    ttl_seconds     INTEGER NOT NULL DEFAULT 1800,
    max_history     INTEGER NOT NULL DEFAULT 500,
    detached_at     TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One active/detached session per character at most
CREATE UNIQUE INDEX idx_sessions_active_character
    ON sessions (character_id) WHERE status IN ('active', 'detached');

-- Fast lookup for reaper: find detached sessions by expiry
CREATE INDEX idx_sessions_status ON sessions (status) WHERE status = 'detached';

-- session_connections: tracks individual client connections to a session
CREATE TABLE session_connections (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    client_type  TEXT NOT NULL,
    streams      TEXT[] NOT NULL,
    connected_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_session_connections_session ON session_connections (session_id);

-- player_tokens: opaque tokens for two-phase login
CREATE TABLE player_tokens (
    token       TEXT PRIMARY KEY,
    player_id   TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_player_tokens_player ON player_tokens (player_id);
```

- [ ] **Step 2: Create down migration**

Create `internal/store/migrations/000018_sessions.down.sql`:

```sql
DROP TABLE IF EXISTS session_connections;
DROP TABLE IF EXISTS player_tokens;
DROP TABLE IF EXISTS sessions;
```

- [ ] **Step 3: Verify migration compiles**

Run: `task test -- -run TestMigrationsEmbed ./internal/store/`
Expected: PASS — migration files are embedded correctly

- [ ] **Step 4: Commit**

```bash
jj commit -m "feat(store): add migration 000018 for sessions, connections, and player tokens

Creates three tables: sessions (persistent game sessions with status,
cursors, command history), session_connections (multi-connection tracking
per session), and player_tokens (opaque tokens for two-phase login).
Partial unique index enforces one active/detached session per character."
```

### Task 1b: Implement PostgresSessionStore

**Files:**

- Create: `internal/store/session_store.go`
- Create: `internal/store/session_store_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/store/session_store_test.go` with unit tests using a mock pool. Follow the pattern in `internal/store/postgres_test.go`. Cover:

- `Get` — happy path, not found
- `Set` — insert and upsert
- `Delete` — happy path
- `FindByCharacter` — happy path, not found, skips expired
- `UpdateStatus` — transitions status fields
- `ReattachCAS` — returns true on success, false on race
- `UpdateCursors` — partial update via `jsonb_set`
- `AppendCommand` — cap enforcement
- `GetCommandHistory` — returns history array
- `AddConnection` / `RemoveConnection` / `CountConnections` — CRUD
- `CountConnectionsByType` — filters by client type
- `ListExpired` — returns sessions past expiry

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/store/ -run TestPostgresSessionStore -v`
Expected: FAIL — `PostgresSessionStore` not defined

- [ ] **Step 3: Implement PostgresSessionStore**

Create `internal/store/session_store.go`. Key implementation notes:

- Uses `*pgxpool.Pool` (same pool as `PostgresEventStore`)
- `ReattachCAS`: `UPDATE sessions SET status = 'active', detached_at = NULL, expires_at = NULL, updated_at = now() WHERE id = $1 AND status = 'detached'` — check `RowsAffected() == 1`
- `UpdateCursors`: Use `UPDATE sessions SET event_cursors = event_cursors || $1::jsonb, updated_at = now() WHERE id = $2`
- `AppendCommand`: `UPDATE sessions SET command_history = command_history[greatest(1, array_length(command_history,1) - $2 + 2) : array_length(command_history,1)] || ARRAY[$1], updated_at = now() WHERE id = $3`
- `ListExpired`: `SELECT * FROM sessions WHERE status = 'detached' AND expires_at < now()`
- All methods accept `context.Context` for cancellation
- All errors wrapped with `oops.Code(...)` and relevant context

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/session"
)

// PostgresSessionStore implements session.Store using PostgreSQL.
type PostgresSessionStore struct {
	pool *pgxpool.Pool
}

// NewPostgresSessionStore creates a new Postgres-backed session store.
func NewPostgresSessionStore(pool *pgxpool.Pool) *PostgresSessionStore {
	return &PostgresSessionStore{pool: pool}
}

// compile-time check
var _ session.Store = (*PostgresSessionStore)(nil)

// ... implement all Store methods per the interface ...
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run TestPostgresSessionStore -v`
Expected: PASS

- [ ] **Step 5: Run linter**

Run: `task lint`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
jj commit -m "feat(store): implement PostgresSessionStore for persistent sessions

Postgres-backed session.Store with CRUD, FindByCharacter, ReattachCAS
(optimistic concurrency), cursor updates via jsonb merge, command
history with array-based cap enforcement, and connection tracking.
Uses same pgxpool.Pool as PostgresEventStore."
```

### Task 1c: Integration Tests with Testcontainers

**Files:**

- Create: `internal/store/session_store_integration_test.go`

- [ ] **Step 1: Write integration tests**

Create `internal/store/session_store_integration_test.go` with `//go:build integration` tag. Follow the pattern in `internal/store/postgres_integration_test.go`. Tests should:

- Start a testcontainers PostgreSQL instance
- Run migrations (including 000018)
- Exercise full CRUD lifecycle
- Test `ReattachCAS` concurrency (two goroutines race)
- Test `ListExpired` with time-based filtering
- Test `AppendCommand` cap enforcement with real Postgres array slicing

- [ ] **Step 2: Run integration tests**

Run: `go test -race -v -tags=integration ./internal/store/ -run TestPostgresSessionStore`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
jj commit -m "test(store): add integration tests for PostgresSessionStore

Tests against real PostgreSQL via testcontainers. Covers full CRUD
lifecycle, ReattachCAS concurrency, ListExpired time filtering,
and command history cap enforcement with Postgres array operations."
```

---

## Chunk 2: Core Integration — Replace grpc.SessionInfo

This chunk migrates the `CoreServer` from the informal `grpc.SessionStore` / `grpc.SessionInfo` types to the new `session.Store` / `session.Info` types. All existing call sites update. Existing tests continue to pass using `session.MemStore`.

### Task 2a: Update CoreServer to Use session.Store

**Files:**

- Modify: `internal/grpc/server.go:38-102` (delete old types)
- Modify: `internal/grpc/server.go:89-102` (change field type)
- Modify: `internal/grpc/server.go:129-143` (update constructor)

- [ ] **Step 1: Delete old types from server.go**

Remove `SessionStore` interface (lines 39-43), `SessionInfo` struct (lines 46-52), `InMemorySessionStore` struct and all its methods (lines 54-87) from `internal/grpc/server.go`.

- [ ] **Step 2: Update CoreServer struct**

Change the `sessionStore` field type and update the `disconnectHooks` signature:

```go
import "github.com/holomush/holomush/internal/session"

type CoreServer struct {
	corev1.UnimplementedCoreServer

	engine          *core.Engine
	sessions        *core.SessionManager
	broadcaster     *core.Broadcaster
	authenticator   Authenticator
	sessionStore    session.Store
	disconnectHooks []func(session.Info)

	newSessionID func() ulid.ULID
}
```

- [ ] **Step 3: Update constructor and options**

Update `NewCoreServer` to require a `session.Store` parameter (no longer creates `InMemorySessionStore` internally). Update `WithSessionStore` option. Update `WithDisconnectHook` signature.

```go
func NewCoreServer(engine *core.Engine, sessions *core.SessionManager,
	broadcaster *core.Broadcaster, sessionStore session.Store,
	opts ...CoreServerOption) *CoreServer {
	s := &CoreServer{
		engine:       engine,
		sessions:     sessions,
		broadcaster:  broadcaster,
		sessionStore: sessionStore,
		newSessionID: core.NewULID,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
```

- [ ] **Step 4: Update all callers of NewCoreServer**

In `cmd/holomush/core.go` (line ~363), pass a `session.Store`:

```go
sessionStore := session.NewMemStore() // temporary — Chunk 3 switches to Postgres
coreServer := holoGRPC.NewCoreServer(engine, sessions, broadcaster, sessionStore,
	holoGRPC.WithAuthenticator(guestAuth),
	// ...
)
```

Update all test files that create `CoreServer` to pass `session.NewMemStore()`.

- [ ] **Step 5: Run all tests**

Run: `task test`
Expected: PASS — verify no regressions across all packages

- [ ] **Step 6: Run linter**

Run: `task lint`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
jj commit -m "refactor(grpc): replace grpc.SessionInfo with session.Store interface

CoreServer now accepts session.Store as a constructor parameter
instead of using the internal InMemorySessionStore. Deletes
SessionStore interface, SessionInfo struct, and InMemorySessionStore
from internal/grpc/server.go. All call sites updated to use
session.Info and session.MemStore."
```

### Task 2b: Update Authenticate Call Site

**Files:**

- Modify: `internal/grpc/server.go:145-220`

- [ ] **Step 1: Update Authenticate to use session.Store.Set with context**

The current code calls `s.sessionStore.Set(sessionID.String(), &SessionInfo{...})` (no context, no error return). Update to:

```go
sessionInfo := &session.Info{
	ID:            sessionID.String(),
	CharacterID:   result.CharacterID,
	CharacterName: result.CharacterName,
	LocationID:    result.LocationID,
	IsGuest:       result.IsGuest,
	Status:        session.StatusActive,
	GridPresent:   true,
	EventCursors:  map[string]ulid.ULID{},
	TTLSeconds:    1800, // default, resolved from config in Chunk 3
	MaxHistory:    500,
	CreatedAt:     time.Now(),
	UpdatedAt:     time.Now(),
}

if err := s.sessionStore.Set(ctx, sessionID.String(), sessionInfo); err != nil {
	slog.ErrorContext(ctx, "failed to store session",
		"request_id", requestID,
		"session_id", sessionID.String(),
		"error", err,
	)
	return &corev1.AuthResponse{
		Meta:    responseMeta(requestID),
		Success: false,
		Error:   "session creation failed",
	}, nil
}

// Register connection
connInfo := &session.Connection{
	ID:          connID,
	SessionID:   sessionID.String(),
	ClientType:  "terminal", // default; web handler will pass appropriate type
	Streams:     []string{"location:" + result.LocationID.String()},
	ConnectedAt: time.Now(),
}
if err := s.sessionStore.AddConnection(ctx, connInfo); err != nil {
	slog.WarnContext(ctx, "failed to register connection",
		"request_id", requestID,
		"error", err,
	)
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/grpc/ -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
jj commit -m "refactor(grpc): update Authenticate to use session.Store with context

Authenticate now creates session.Info with full field set and
registers the connection via AddConnection. Error handling added
for store operations."
```

### Task 2c: Update HandleCommand and Subscribe Call Sites

**Files:**

- Modify: `internal/grpc/server.go:222-382`

- [ ] **Step 1: Update HandleCommand**

Change `s.sessionStore.Get(req.SessionId)` (returns `(*SessionInfo, bool)`) to `s.sessionStore.Get(ctx, req.SessionId)` (returns `(*session.Info, error)`):

```go
info, err := s.sessionStore.Get(ctx, req.SessionId)
if err != nil {
	return &corev1.CommandResponse{
		Meta:    responseMeta(requestID),
		Success: false,
		Error:   "session not found",
	}, nil
}
```

Update `executeCommand` signature to accept `*session.Info` instead of `*SessionInfo`.

- [ ] **Step 2: Update Subscribe**

Same pattern for the `Get` call. The `UpdateCursor` call currently goes through `s.sessions.UpdateCursor(info.CharacterID, ...)` — this stays for now (the in-memory `SessionManager` cursor tracking is separate from the persistent store). In Chunk 4, cursor updates will also write to the session store.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/grpc/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
jj commit -m "refactor(grpc): update HandleCommand and Subscribe to use session.Store

Get calls now use context and return error instead of bool.
executeCommand accepts *session.Info. Subscribe session lookup
updated to match new interface."
```

### Task 2d: Update Disconnect Call Site

**Files:**

- Modify: `internal/grpc/server.go:385-443`

- [ ] **Step 1: Update Disconnect**

Change `s.sessionStore.Get(req.SessionId)` and `s.sessionStore.Delete(req.SessionId)` to use context. Update disconnect hooks to receive `session.Info`. For now, keep the immediate-delete behavior — Chunk 3 changes this to detach.

```go
info, err := s.sessionStore.Get(ctx, req.SessionId)
if err != nil {
	// Session already gone, return success
	return &corev1.DisconnectResponse{
		Meta:    responseMeta(requestID),
		Success: true,
	}, nil
}

// ... emit leave event ...

// Delete session (Chunk 3 will change this to detach)
if err := s.sessionStore.Delete(ctx, req.SessionId); err != nil {
	slog.WarnContext(ctx, "failed to delete session",
		"request_id", requestID,
		"session_id", req.SessionId,
		"error", err,
	)
}
```

- [ ] **Step 2: Update disconnect hook callers in core.go**

Update the disconnect hook in `cmd/holomush/core.go` to match the new `session.Info` type:

```go
holoGRPC.WithDisconnectHook(func(info session.Info) {
	if info.IsGuest {
		guestAuth.ReleaseGuest(info.CharacterName)
	}
}),
```

- [ ] **Step 3: Run all tests**

Run: `task test`
Expected: PASS — full regression check

- [ ] **Step 4: Run linter**

Run: `task lint`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
jj commit -m "refactor(grpc): update Disconnect to use session.Store with context

Disconnect now uses session.Store.Get/Delete with context and
error handling. Disconnect hooks receive session.Info. Keeps
immediate-delete behavior for now; detach logic added in Chunk 3."
```

---

## Chunk 3: Session Lifecycle — Detach, TTL, Reaper

### Task 3a: Detach-on-Disconnect

**Files:**

- Modify: `internal/grpc/server.go:385-443` (Disconnect method)

- [ ] **Step 1: Update Disconnect to detach instead of delete**

Replace the `Delete` call with connection removal + conditional detach:

```go
// Remove this connection
if err := s.sessionStore.RemoveConnection(ctx, info.ConnectionID); err != nil {
	slog.WarnContext(ctx, "failed to remove connection",
		"request_id", requestID,
		"error", err,
	)
}

// Check remaining connections
count, err := s.sessionStore.CountConnections(ctx, req.SessionId)
if err != nil {
	slog.WarnContext(ctx, "failed to count connections",
		"request_id", requestID,
		"error", err,
	)
}

if count == 0 {
	// Last connection closed — detach session
	now := time.Now()
	expiresAt := now.Add(time.Duration(info.TTLSeconds) * time.Second)
	if err := s.sessionStore.UpdateStatus(ctx, req.SessionId,
		session.StatusDetached, &now, &expiresAt); err != nil {
		slog.WarnContext(ctx, "failed to detach session",
			"request_id", requestID,
			"error", err,
		)
	}
	// Do NOT emit leave event — player may reconnect
	// Reaper handles leave events when TTL expires
}
```

Note: The `info` struct needs a `ConnectionID` to know which connection to remove. This requires storing the connection ID in the session flow. The connection ID is generated in `Authenticate` and must be tracked. Add a `connectionID` field to the context or pass it through the `SubscribeRequest`.

Alternative: use the gRPC stream context to look up the connection. For now, store the connection ID in the `SubscribeRequest.Meta.RequestId` or add a dedicated field. The simplest approach: add a `connection_id` field to `DisconnectRequest` in the core proto. If the client doesn't send it, fall back to removing all connections for the session (single-connection case).

- [ ] **Step 2: Handle explicit quit**

When the command is `quit`, the session MUST be terminated immediately (not detached):

```go
// In executeCommand, "quit" case:
case "quit":
	// Explicit quit — terminate session immediately
	// Emit leave event
	char := core.CharacterRef{ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID}
	if err := s.engine.HandleDisconnect(ctx, char, "quit"); err != nil {
		slog.WarnContext(ctx, "leave event failed", "error", err)
	}
	// Delete session
	if err := s.sessionStore.Delete(ctx, info.ID); err != nil {
		slog.WarnContext(ctx, "session delete failed", "error", err)
	}
	// Run disconnect hooks
	for _, hook := range s.disconnectHooks {
		hook(*info)
	}
	return "Goodbye!", nil
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/grpc/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
jj commit -m "feat(session): detach session on disconnect instead of immediate delete

Last connection closing transitions session to detached status
with TTL-based expiry. Explicit quit command still terminates
immediately. Leave events deferred to reaper on TTL expiration."
```

### Task 3b: Session Reaper

**Files:**

- Create: `internal/session/reaper.go`
- Create: `internal/session/reaper_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/session/reaper_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReaper_ReapsExpiredSessions(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	past := time.Now().Add(-1 * time.Hour)
	info := &Info{
		ID:          "expired-session",
		CharacterID: ulid.Make(),
		CharacterName: "Ghost",
		Status:      StatusDetached,
		ExpiresAt:   &past,
		IsGuest:     false,
	}
	require.NoError(t, store.Set(ctx, "expired-session", info))

	var reaped []Info
	reaper := NewReaper(store, ReaperConfig{
		Interval:  100 * time.Millisecond,
		OnExpired: func(info *Info) { reaped = append(reaped, *info) },
	})

	reaperCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	reaper.Run(reaperCtx)

	assert.Len(t, reaped, 1)
	assert.Equal(t, "Ghost", reaped[0].CharacterName)

	// Session should be deleted
	_, err := store.Get(ctx, "expired-session")
	assert.Error(t, err)
}

func TestReaper_SkipsActiveAndFutureSessions(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	future := time.Now().Add(1 * time.Hour)
	require.NoError(t, store.Set(ctx, "active", &Info{
		ID:     "active",
		Status: StatusActive,
	}))
	require.NoError(t, store.Set(ctx, "future", &Info{
		ID:        "future",
		Status:    StatusDetached,
		ExpiresAt: &future,
	}))

	var reaped []Info
	reaper := NewReaper(store, ReaperConfig{
		Interval:  100 * time.Millisecond,
		OnExpired: func(info *Info) { reaped = append(reaped, *info) },
	})

	reaperCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	reaper.Run(reaperCtx)

	assert.Empty(t, reaped)
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/session/ -run TestReaper -v`
Expected: FAIL — `NewReaper` not defined

- [ ] **Step 3: Implement SessionReaper**

Create `internal/session/reaper.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session

import (
	"context"
	"log/slog"
	"time"
)

// ReaperConfig configures the session reaper.
type ReaperConfig struct {
	Interval  time.Duration     // how often to check for expired sessions
	OnExpired func(info *Info)  // callback for each expired session (emit leave events, etc.)
}

// Reaper periodically checks for and cleans up expired detached sessions.
type Reaper struct {
	store  Store
	config ReaperConfig
}

// NewReaper creates a new session reaper.
func NewReaper(store Store, config ReaperConfig) *Reaper {
	if config.Interval <= 0 {
		config.Interval = 30 * time.Second
	}
	return &Reaper{
		store:  store,
		config: config,
	}
}

// Run starts the reaper loop. Blocks until context is cancelled.
func (r *Reaper) Run(ctx context.Context) {
	ticker := time.NewTicker(r.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reapExpired(ctx)
		}
	}
}

func (r *Reaper) reapExpired(ctx context.Context) {
	expired, err := r.store.ListExpired(ctx)
	if err != nil {
		slog.WarnContext(ctx, "reaper: failed to list expired sessions", "error", err)
		return
	}

	for _, info := range expired {
		// Notify callback (emit leave events, release guest characters)
		if r.config.OnExpired != nil {
			r.config.OnExpired(info)
		}

		// Mark as expired and delete
		if err := r.store.UpdateStatus(ctx, info.ID, StatusExpired, nil, nil); err != nil {
			slog.WarnContext(ctx, "reaper: failed to expire session",
				"session_id", info.ID,
				"error", err,
			)
			continue
		}

		if err := r.store.Delete(ctx, info.ID); err != nil {
			slog.WarnContext(ctx, "reaper: failed to delete expired session",
				"session_id", info.ID,
				"error", err,
			)
		}

		slog.InfoContext(ctx, "reaper: expired session cleaned up",
			"session_id", info.ID,
			"character_name", info.CharacterName,
			"is_guest", info.IsGuest,
		)
	}
}
```

- [ ] **Step 4: Run to verify tests pass**

Run: `go test ./internal/session/ -run TestReaper -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
jj commit -m "feat(session): add SessionReaper for expired session cleanup

Background goroutine periodically scans for expired detached sessions.
Calls OnExpired callback for leave event emission and guest cleanup,
then deletes the session. Configurable interval (default 30s)."
```

### Task 3c: Wire Reaper into Core Process

**Files:**

- Modify: `cmd/holomush/core.go:351-371`

- [ ] **Step 1: Start reaper goroutine in runCoreWithDeps**

After creating the `coreServer`, start the reaper:

```go
// Start session reaper
reaper := session.NewReaper(sessionStore, session.ReaperConfig{
	Interval: 30 * time.Second, // TODO: read from config in Chunk 8
	OnExpired: func(info *session.Info) {
		// Emit leave event
		char := core.CharacterRef{
			ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID,
		}
		if err := engine.HandleDisconnect(ctx, char, "session expired"); err != nil {
			slog.Warn("reaper: leave event failed",
				"session_id", info.ID,
				"error", err,
			)
		}
		// Release guest character
		if info.IsGuest {
			guestAuth.ReleaseGuest(info.CharacterName)
		}
	},
})
go reaper.Run(ctx)
```

- [ ] **Step 2: Run tests**

Run: `task test`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
jj commit -m "feat(core): wire SessionReaper into core process startup

Reaper runs as a background goroutine, emitting leave events and
releasing guest characters when detached sessions expire. Stops
on context cancellation during graceful shutdown."
```

### Task 3d: TTL Resolution from Config

**Files:**

- Modify: `cmd/holomush/core.go:48-56` (add config fields)
- Modify: `internal/grpc/server.go:145-220` (use config values)

- [ ] **Step 1: Add session config fields to coreConfig**

```go
type coreConfig struct {
	// ... existing fields ...
	SessionTTL             string `koanf:"session_ttl"`
	SessionMaxHistory      int    `koanf:"session_max_history"`
	SessionMaxReplay       int    `koanf:"session_max_replay"`
	SessionReaperInterval  string `koanf:"session_reaper_interval"`
}
```

Add defaults and CLI flags:

```go
cmd.Flags().StringVar(&cfg.SessionTTL, "session-ttl", "30m", "default session TTL")
cmd.Flags().IntVar(&cfg.SessionMaxHistory, "session-max-history", 500, "default max command history per session")
cmd.Flags().IntVar(&cfg.SessionMaxReplay, "session-max-replay", 1000, "max events replayed on reconnect per stream")
cmd.Flags().StringVar(&cfg.SessionReaperInterval, "session-reaper-interval", "30s", "session reaper check interval")
```

- [ ] **Step 2: Parse durations and pass to CoreServer**

```go
ttl, err := time.ParseDuration(cfg.SessionTTL)
if err != nil {
	return oops.Code("CONFIG_INVALID").With("field", "session_ttl").Wrap(err)
}
reaperInterval, err := time.ParseDuration(cfg.SessionReaperInterval)
if err != nil {
	return oops.Code("CONFIG_INVALID").With("field", "session_reaper_interval").Wrap(err)
}
```

Pass to `CoreServer` via a new option:

```go
holoGRPC.WithSessionDefaults(holoGRPC.SessionDefaults{
	TTL:        ttl,
	MaxHistory: cfg.SessionMaxHistory,
	MaxReplay:  cfg.SessionMaxReplay,
}),
```

- [ ] **Step 3: Run tests**

Run: `task test`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
jj commit -m "feat(config): add session TTL, max history, and reaper interval config

New core config fields: session_ttl (default 30m),
session_max_history (default 500), session_max_replay (default 1000),
session_reaper_interval (default 30s). Parsed as durations and
passed to CoreServer and SessionReaper."
```

---

## Chunk 4: Event Replay on Reconnect

### Task 4a: Add replay_from_cursor to Core Proto

**Files:**

- Modify: `api/proto/holomush/core/v1/core.proto:67-71`

- [ ] **Step 1: Add replay_from_cursor field to SubscribeRequest**

```protobuf
message SubscribeRequest {
  RequestMeta meta = 1;
  string session_id = 2;
  repeated string streams = 3;
  bool replay_from_cursor = 4;
}
```

- [ ] **Step 2: Regenerate Go code**

Run: `buf generate`
Expected: Generated code in `pkg/proto/` updated

- [ ] **Step 3: Run tests to verify no regression**

Run: `task test`
Expected: PASS — existing code ignores the new field (default `false`)

- [ ] **Step 4: Commit**

```bash
jj commit -m "feat(proto): add replay_from_cursor field to SubscribeRequest

New boolean field on SubscribeRequest enables clients to request
missed event replay on reconnect. Default false preserves existing
live-only behavior."
```

### Task 4b: Implement Replay-Before-Live Merge in Subscribe

**Files:**

- Modify: `internal/grpc/server.go:301-382`
- Reference: `internal/core/broadcaster.go` (buffer size 100)
- Reference: `internal/core/store.go` (EventStore.Replay)

- [ ] **Step 1: Write failing test for replay**

Add test in `internal/grpc/server_test.go`:

```go
func TestSubscribe_ReplayFromCursor(t *testing.T) {
	// Set up: session store with session that has event cursors
	// Event store with 5 historical events after cursor
	// Broadcaster that will deliver 2 live events during replay
	// Expect: 5 replayed events (replayed=true in future), then
	//         2 live events (one may overlap with replay — deduped)
}
```

- [ ] **Step 2: Implement replay logic in Subscribe**

When `req.ReplayFromCursor` is true:

```go
func (s *CoreServer) Subscribe(req *corev1.SubscribeRequest, stream grpc.ServerStreamingServer[corev1.Event]) error {
	ctx := stream.Context()
	// ... existing session lookup ...

	info, err := s.sessionStore.Get(ctx, req.SessionId)
	if err != nil {
		return oops.Code("SESSION_NOT_FOUND").With("session_id", req.SessionId).Wrap(err)
	}

	// Subscribe to broadcaster channels FIRST (before replay)
	channels := make([]chan core.Event, 0, len(streams))
	for _, streamName := range streams {
		ch := s.broadcaster.Subscribe(streamName)
		channels = append(channels, ch)
		defer s.broadcaster.Unsubscribe(streamName, ch)
	}
	merged := mergeChannels(ctx, channels)

	if req.ReplayFromCursor && len(info.EventCursors) > 0 {
		// Start concurrent drain goroutine
		var bufMu sync.Mutex
		var liveBuf []core.Event
		drainDone := make(chan struct{})

		go func() {
			defer close(drainDone)
			for {
				select {
				case <-ctx.Done():
					return
				case ev, ok := <-merged:
					if !ok {
						return
					}
					bufMu.Lock()
					liveBuf = append(liveBuf, ev)
					bufMu.Unlock()
				}
			}
		}()

		// Replay from each stream
		replayedIDs := make(map[ulid.ULID]struct{})
		for _, streamName := range streams {
			cursor, hasCursor := info.EventCursors[streamName]
			if !hasCursor {
				continue
			}
			events, err := s.eventStore.Replay(ctx, streamName, cursor, s.sessionDefaults.MaxReplay)
			if err != nil {
				slog.WarnContext(ctx, "replay failed", "stream", streamName, "error", err)
				continue
			}
			for _, ev := range events {
				replayedIDs[ev.ID] = struct{}{}
				// Send with replayed indicator (future proto field)
				protoEvent := eventToProto(ev)
				if err := stream.Send(protoEvent); err != nil {
					return oops.Code("SEND_FAILED").Wrap(err)
				}
				s.sessions.UpdateCursor(info.CharacterID, ev.Stream, ev.ID)
			}
		}

		// Stop drainer, collect buffer
		// Note: we don't cancel the drainer — it stops when merged closes
		// Instead, we switch to direct reads from merged below

		// Drain buffered live events, deduplicating
		bufMu.Lock()
		buf := liveBuf
		liveBuf = nil
		bufMu.Unlock()

		for _, ev := range buf {
			if _, replayed := replayedIDs[ev.ID]; replayed {
				continue // dedup
			}
			protoEvent := eventToProto(ev)
			if err := stream.Send(protoEvent); err != nil {
				return oops.Code("SEND_FAILED").Wrap(err)
			}
			s.sessions.UpdateCursor(info.CharacterID, ev.Stream, ev.ID)
		}

		// TODO: Send replay_complete marker via StreamEventsResponse
	}

	// Switch to normal live forwarding
	for {
		select {
		case <-ctx.Done():
			return oops.Code("SUBSCRIPTION_CANCELLED").Wrap(ctx.Err())
		case event, ok := <-merged:
			if !ok {
				return nil
			}
			s.sessions.UpdateCursor(info.CharacterID, event.Stream, event.ID)
			if err := stream.Send(eventToProto(event)); err != nil {
				return oops.Code("SEND_FAILED").Wrap(err)
			}
		}
	}
}
```

Note: This is a simplified sketch. The actual implementation must handle the drainer goroutine lifecycle carefully — stop draining once we switch to direct reads from `merged`. One approach: use a separate channel to signal the drainer to stop, then drain remaining buffer.

- [ ] **Step 3: Extract eventToProto helper**

Extract the proto event construction into a helper function (already partially exists):

```go
func eventToProto(ev core.Event) *corev1.Event {
	return &corev1.Event{
		Id:        ev.ID.String(),
		Stream:    ev.Stream,
		Type:      string(ev.Type),
		Timestamp: timestamppb.New(ev.Timestamp),
		ActorType: ev.Actor.Kind.String(),
		ActorId:   ev.Actor.ID,
		Payload:   ev.Payload,
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/grpc/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
jj commit -m "feat(grpc): implement replay-before-live merge in Subscribe

When replay_from_cursor is true, Subscribe replays missed events
from EventStore while concurrently draining broadcaster to prevent
buffer overflow. After replay, buffered live events are sent with
deduplication by event ID. Guarantees zero missed events."
```

### Task 4c: Add Replay Fields to Web Proto

**Files:**

- Modify: `api/proto/holomush/web/v1/web.proto`

- [ ] **Step 1: Add replayed and replay_complete fields**

```protobuf
message StreamEventsResponse {
  GameEvent event = 1;
  bool replayed = 2;
  bool replay_complete = 3;
}
```

- [ ] **Step 2: Regenerate code**

Run: `buf generate`

- [ ] **Step 3: Update web handler to pass replay fields through**

In `internal/web/handler.go`, update `StreamEvents` to pass `replay_from_cursor` to the core `SubscribeRequest` and forward the `replayed` / `replay_complete` fields from the core response.

- [ ] **Step 4: Run tests**

Run: `task test`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
jj commit -m "feat(proto): add replayed and replay_complete fields to StreamEventsResponse

Web clients can distinguish replayed events from live events
and render a visual separator when replay_complete is received."
```

### Task 4d: Persist Cursors to Session Store

**Files:**

- Modify: `internal/grpc/server.go` (Subscribe cursor updates)

- [ ] **Step 1: Write cursor updates to both SessionManager and session.Store**

After each event is sent in Subscribe, update both the in-memory cursor (for the session's lifetime) and persist to the store (for crash recovery):

```go
// Update in-memory cursor (fast, used during this connection)
s.sessions.UpdateCursor(info.CharacterID, event.Stream, event.ID)

// Persist cursor to store (durable, used on reconnect)
// Best-effort — don't block the stream for a DB write
go func(sid string, stream string, eventID ulid.ULID) {
	if err := s.sessionStore.UpdateCursors(context.Background(), sid,
		map[string]ulid.ULID{stream: eventID}); err != nil {
		slog.Warn("cursor persist failed", "session_id", sid, "error", err)
	}
}(req.SessionId, event.Stream, event.ID)
```

Note: For performance, consider batching cursor updates (e.g., every N events or every M seconds) rather than writing on every event. This optimization can be deferred.

- [ ] **Step 2: Run tests**

Run: `task test`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
jj commit -m "feat(session): persist event cursors to session store on event delivery

Cursor updates write to both in-memory SessionManager (fast path)
and persistent session.Store (durable, used for replay on reconnect).
Persistence is best-effort and non-blocking."
```

---

## Chunk 5: Two-Phase Login + Character Selection

### Task 5a: Player Token Types and Repository

**Files:**

- Create: `internal/auth/player_token.go`
- Create: `internal/auth/player_token_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/auth/player_token_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPlayerToken(t *testing.T) {
	playerID := ulid.Make()
	token, err := NewPlayerToken(playerID, 24*time.Hour)
	require.NoError(t, err)
	assert.NotEmpty(t, token.Token)
	assert.Equal(t, playerID, token.PlayerID)
	assert.False(t, token.IsExpired())
}

func TestPlayerToken_IsExpired(t *testing.T) {
	playerID := ulid.Make()
	token, err := NewPlayerToken(playerID, -1*time.Hour)
	require.NoError(t, err)
	assert.True(t, token.IsExpired())
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/auth/ -run TestNewPlayerToken -v`
Expected: FAIL — `NewPlayerToken` not defined

- [ ] **Step 3: Implement PlayerToken**

Create `internal/auth/player_token.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
)

// PlayerToken is an opaque token for two-phase login.
// Players authenticate once to get a token, then use it for
// character selection without re-entering credentials.
type PlayerToken struct {
	Token     string
	PlayerID  ulid.ULID
	CreatedAt time.Time
	ExpiresAt time.Time
}

// NewPlayerToken creates a player token with a ULID as the token value.
func NewPlayerToken(playerID ulid.ULID, ttl time.Duration) (*PlayerToken, error) {
	now := time.Now()
	return &PlayerToken{
		Token:     ulid.Make().String(),
		PlayerID:  playerID,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}, nil
}

// IsExpired returns true if the token has passed its expiry time.
func (t *PlayerToken) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

// PlayerTokenRepository manages player token persistence.
type PlayerTokenRepository interface {
	Create(ctx context.Context, token *PlayerToken) error
	GetByToken(ctx context.Context, token string) (*PlayerToken, error)
	DeleteByToken(ctx context.Context, token string) error
	DeleteByPlayer(ctx context.Context, playerID ulid.ULID) error
	DeleteExpired(ctx context.Context) (int64, error)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/auth/ -run TestNewPlayerToken -v && go test ./internal/auth/ -run TestPlayerToken -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
jj commit -m "feat(auth): add PlayerToken type and repository interface

Opaque ULID-based player tokens for two-phase login. Players
authenticate once, receive a token, then use it for character
selection without re-entering credentials. Tokens have TTL-based
expiry."
```

### Task 5b: PostgresPlayerTokenStore

**Files:**

- Create: `internal/store/player_token_store.go`
- Create: `internal/store/player_token_store_test.go`

- [ ] **Step 1: Write the failing tests**

Cover: Create, GetByToken (happy + not found + expired), DeleteByToken, DeleteByPlayer, DeleteExpired.

- [ ] **Step 2: Implement PostgresPlayerTokenStore**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/holomush/holomush/internal/auth"
)

// PostgresPlayerTokenStore implements auth.PlayerTokenRepository.
type PostgresPlayerTokenStore struct {
	pool *pgxpool.Pool
}

func NewPostgresPlayerTokenStore(pool *pgxpool.Pool) *PostgresPlayerTokenStore {
	return &PostgresPlayerTokenStore{pool: pool}
}

var _ auth.PlayerTokenRepository = (*PostgresPlayerTokenStore)(nil)

// ... implement all methods ...
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/store/ -run TestPostgresPlayerToken -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
jj commit -m "feat(store): implement PostgresPlayerTokenStore for two-phase login

Postgres-backed player token repository. CRUD operations with
TTL-based expiry enforcement. Shares pgxpool with other stores."
```

### Task 5c: Add Proto RPCs for Two-Phase Login

**Files:**

- Modify: `api/proto/holomush/web/v1/web.proto`
- Modify: `api/proto/holomush/core/v1/core.proto`

- [ ] **Step 1: Add new RPCs and messages to web proto**

Add `Authenticate`, `ListCharacters`, `SelectCharacter`, `ListSessions`, `GetCommandHistory` RPCs and all associated message types as specified in the design doc. Keep existing `Login` RPC for guest convenience.

- [ ] **Step 2: Add corresponding RPCs to core proto if needed**

The core proto may need new RPCs or the web handler can compose them from existing RPCs + direct store access. Evaluate which approach is cleaner. If the web handler uses `session.Store` directly (via dependency injection), the core proto may not need changes beyond `replay_from_cursor`.

- [ ] **Step 3: Regenerate code**

Run: `buf generate`

- [ ] **Step 4: Commit**

```bash
jj commit -m "feat(proto): add two-phase login RPCs to web proto

New RPCs: Authenticate (player auth + token), ListCharacters,
SelectCharacter (create/reattach session), ListSessions,
GetCommandHistory. Existing Login RPC kept for guest access."
```

### Task 5d: Implement Web Handler Methods

**Files:**

- Modify: `internal/web/handler.go`
- Modify: `internal/auth/auth_service.go`
- Modify: `internal/auth/player.go:69-73`

- [ ] **Step 1: Add AutoConnectMode to PlayerPreferences**

In `internal/auth/player.go`:

```go
type PlayerPreferences struct {
	AutoLogin       bool   `json:"auto_login,omitempty"`
	MaxCharacters   int    `json:"max_characters,omitempty"`
	Theme           string `json:"theme,omitempty"`
	AutoConnectMode string `json:"auto_connect_mode,omitempty"` // "last_connected", "default", "ask"
}
```

- [ ] **Step 2: Add AuthenticatePlayer to auth.Service**

Add a method that validates credentials and returns a player token + character list instead of creating a game session:

```go
func (s *Service) AuthenticatePlayer(ctx context.Context, username, password string) (*PlayerToken, []*CharacterSummary, error) {
	// ... validate credentials (reuse existing Login logic) ...
	// ... create PlayerToken ...
	// ... query characters for this player ...
	// ... check for existing sessions per character ...
	return token, characters, nil
}
```

- [ ] **Step 3: Implement web handler methods**

In `internal/web/handler.go`, add implementations for `Authenticate`, `ListCharacters`, `SelectCharacter`, `ListSessions`, `GetCommandHistory`. Each delegates to appropriate backend services.

- [ ] **Step 4: Run tests**

Run: `task test`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
jj commit -m "feat(web): implement two-phase login handler methods

Authenticate validates credentials and returns player token.
ListCharacters returns player's characters with session status.
SelectCharacter creates or reattaches a game session.
ListSessions and GetCommandHistory provide session management."
```

---

## Chunk 6: Command History

### Task 6a: Record Commands on HandleCommand

**Files:**

- Modify: `internal/grpc/server.go` (HandleCommand method)

- [ ] **Step 1: Append command to session history on every HandleCommand**

After successful session lookup in `HandleCommand`, before executing the command:

```go
// Record command in session history (best-effort)
if err := s.sessionStore.AppendCommand(ctx, req.SessionId, req.Command, info.MaxHistory); err != nil {
	slog.WarnContext(ctx, "command history append failed",
		"session_id", req.SessionId,
		"error", err,
	)
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/grpc/ -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
jj commit -m "feat(session): record commands in session history on HandleCommand

Every command sent through HandleCommand is appended to the session's
command history in the store. Cap enforcement per session's max_history
setting. Cross-protocol: telnet commands recorded in same session."
```

### Task 6b: Implement GetCommandHistory RPC

**Files:**

- Modify: `internal/grpc/server.go`
- Modify: `internal/web/handler.go`

- [ ] **Step 1: Add GetCommandHistory to CoreServer**

If the GetCommandHistory RPC is on the core proto:

```go
func (s *CoreServer) GetCommandHistory(ctx context.Context, req *corev1.GetCommandHistoryRequest) (*corev1.GetCommandHistoryResponse, error) {
	history, err := s.sessionStore.GetCommandHistory(ctx, req.SessionId)
	if err != nil {
		return nil, oops.Code("HISTORY_FETCH_FAILED").Wrap(err)
	}
	return &corev1.GetCommandHistoryResponse{Commands: history}, nil
}
```

Or if it's only on the web proto, implement in the web handler using the session store directly.

- [ ] **Step 2: Run tests**

Run: `task test`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
jj commit -m "feat(session): implement GetCommandHistory RPC

Returns the session's command history. Used by web client for
command recall and by telnet for history navigation."
```

---

## Chunk 7: Grid Presence + Multi-Connection

### Task 7a: Connection Type Tracking

**Files:**

- Modify: `internal/grpc/server.go` (Authenticate, Disconnect)

- [ ] **Step 1: Accept client_type in connection registration**

The `Authenticate` RPC currently hardcodes `ClientType: "terminal"`. Add a mechanism for callers to specify the client type. Options:

1. Add a `client_type` field to `AuthRequest` in the core proto
2. Pass client type via gRPC metadata
3. Determine from the calling gateway (telnet gateway = "telnet", web gateway = "terminal" or "comms_hub")

The cleanest approach: add `client_type` to the proto. Telnet gateway sends `"telnet"`, web handler sends the type from the client request.

- [ ] **Step 2: Update connection registration to use client_type**

In Authenticate:

```go
connInfo := &session.Connection{
	ID:          connID,
	SessionID:   sessionID.String(),
	ClientType:  req.ClientType, // from proto field
	Streams:     streams,
	ConnectedAt: time.Now(),
}
```

- [ ] **Step 3: Run tests**

Run: `task test`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
jj commit -m "feat(session): track client type per connection

Connections now store their client type (terminal, comms_hub, telnet)
for grid presence phasing decisions."
```

### Task 7b: Grid Presence Phasing

**Files:**

- Modify: `internal/grpc/server.go` (Authenticate, Disconnect)
- Modify: `internal/core/session.go` (add GridPresent field)

- [ ] **Step 1: Add grid presence transitions**

On authenticate / add-connection:

```go
// Phase in: if this is the first terminal/telnet connection
if clientType == "terminal" || clientType == "telnet" {
	terminalCount, _ := s.sessionStore.CountConnectionsByType(ctx, sessionID, "terminal")
	telnetCount, _ := s.sessionStore.CountConnectionsByType(ctx, sessionID, "telnet")
	if terminalCount+telnetCount == 1 { // this is the first one (just added)
		// Phase in: emit arrive event
		char := core.CharacterRef{ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID}
		if err := s.engine.HandleConnect(ctx, char); err != nil {
			slog.WarnContext(ctx, "phase-in arrive event failed", "error", err)
		}
		// Update grid_present
		info.GridPresent = true
		// persist to store
	}
}
```

On disconnect / remove-connection:

```go
// Phase out: if last terminal/telnet connection closed
if clientType == "terminal" || clientType == "telnet" {
	terminalCount, _ := s.sessionStore.CountConnectionsByType(ctx, sessionID, "terminal")
	telnetCount, _ := s.sessionStore.CountConnectionsByType(ctx, sessionID, "telnet")
	if terminalCount+telnetCount == 0 {
		// Check if comms hub connections remain
		totalCount, _ := s.sessionStore.CountConnections(ctx, sessionID)
		if totalCount > 0 {
			// Phase out: emit leave event, session stays active
			char := core.CharacterRef{ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID}
			if err := s.engine.HandleDisconnect(ctx, char, "phased out"); err != nil {
				slog.WarnContext(ctx, "phase-out leave event failed", "error", err)
			}
			info.GridPresent = false
		}
		// else: no connections at all — handled by detach logic
	}
}
```

- [ ] **Step 2: Write tests for phase transitions**

Test cases:

1. Terminal connects → phase in (arrive event emitted)
2. Comms hub connects (no terminal) → no phase in
3. Terminal + comms hub connected, terminal disconnects → phase out (leave event)
4. Terminal + terminal connected, one disconnects → no phase out (still one terminal)
5. Only comms hub remains, all disconnect → detach (no leave event — reaper handles it)

- [ ] **Step 3: Run tests**

Run: `task test`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
jj commit -m "feat(session): implement grid presence phasing on connection changes

Characters phase in (arrive event) when first terminal/telnet
connection opens. Phase out (leave event) when last terminal/telnet
closes but comms hub remains. Session stays active during phase out.
Full detach only when zero connections remain."
```

### Task 7c: Web Handler Connection Registration

**Files:**

- Modify: `internal/web/handler.go` (StreamEvents, Disconnect)

- [ ] **Step 1: Register connection on StreamEvents**

When the web client starts streaming, register a connection with the appropriate type:

```go
func (h *Handler) StreamEvents(ctx context.Context, req *connect.Request[webv1.StreamEventsRequest],
	stream *connect.ServerStream[webv1.StreamEventsResponse]) error {
	sessionID := req.Msg.GetSessionId()
	clientType := req.Msg.GetClientType() // "terminal" or "comms_hub"
	if clientType == "" {
		clientType = "terminal" // default
	}

	// ... register connection ...
	// ... start subscribe with replay_from_cursor ...
}
```

- [ ] **Step 2: Remove connection on stream close**

When the stream ends (client disconnects, context cancelled), remove the connection:

```go
defer func() {
	// Remove connection on stream close
	if removeErr := h.sessionStore.RemoveConnection(ctx, connID); removeErr != nil {
		slog.WarnContext(ctx, "web: failed to remove connection on stream close", "error", removeErr)
	}
}()
```

- [ ] **Step 3: Run tests**

Run: `task test`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
jj commit -m "feat(web): register/remove connections on StreamEvents lifecycle

Web handler registers a connection when StreamEvents starts and
removes it when the stream closes. Connection type (terminal or
comms_hub) determined from client request."
```

---

## Chunk 8: Config + Documentation + Final Verification

### Task 8a: Config YAML Documentation

**Files:**

- Modify: `internal/config/config.go` (if needed)

- [ ] **Step 1: Verify config fields work end-to-end**

Create a test config YAML with session fields and verify they load correctly:

```yaml
core:
  session_ttl: "30m"
  session_max_history: 500
  session_max_replay: 1000
  session_reaper_interval: "30s"
```

Run: `task test`

- [ ] **Step 2: Commit**

```bash
jj commit -m "test(config): verify session config fields load correctly from YAML"
```

### Task 8b: Integration Tests

**Files:**

- Create: `test/integration/session/session_persistence_suite_test.go`
- Create: `test/integration/session/session_persistence_integration_test.go`

- [ ] **Step 1: Create Ginkgo bootstrap**

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSessionPersistence(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Session Persistence Suite")
}
```

- [ ] **Step 2: Write BDD integration tests**

Cover the scenarios from the spec:

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Session Persistence", func() {
	Describe("Reconnect flow", func() {
		It("replays missed events with replayed=true then switches to live", func() {
			// Connect as guest → send commands → disconnect
			// → reconnect → verify replayed events → verify live events
		})

		It("sends replay_complete marker after replay finishes", func() {
			// Connect → disconnect → send events while disconnected
			// → reconnect → verify replay_complete received
		})
	})

	Describe("Command history", func() {
		It("persists commands across disconnect/reconnect", func() {
			// Connect → send commands → disconnect → reconnect
			// → GetCommandHistory returns previous commands
		})

		It("enforces per-session cap", func() {
			// Send more commands than max_history → oldest truncated
		})
	})

	Describe("TTL expiration", func() {
		It("emits leave event when detached session expires", func() {
			// Connect → disconnect → wait for TTL → verify leave event
		})

		It("creates new session on reconnect after expiration", func() {
			// Connect → disconnect → expire → reconnect
			// → new session (not reattach)
		})
	})

	Describe("Explicit quit", func() {
		It("terminates session immediately without detach", func() {
			// Connect → quit → verify leave event → verify session deleted
		})
	})

	Describe("Concurrent reattach", func() {
		It("only one client wins the race", func() {
			// Detach session → two concurrent SelectCharacter
			// → one succeeds, other gets error
		})
	})

	Describe("Empty cursors on reconnect", func() {
		It("sends replay_complete immediately without replay", func() {
			// Connect → disconnect immediately → reconnect
			// → replay_complete sent, no replayed events
		})
	})
})
```

- [ ] **Step 3: Run integration tests**

Run: `go test -race -v -tags=integration ./test/integration/session/`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
jj commit -m "test(session): add BDD integration tests for session persistence

Ginkgo/Gomega specs covering reconnect flow with replay, command
history persistence, TTL expiration, explicit quit, concurrent
reattach race, and empty cursor reconnect."
```

### Task 8c: Full Test Suite + Linting

- [ ] **Step 1: Run full test suite**

Run: `task test`
Expected: PASS — all unit tests pass

- [ ] **Step 2: Run linter**

Run: `task lint`
Expected: PASS — no lint errors

- [ ] **Step 3: Run build**

Run: `task build`
Expected: PASS — binary compiles

- [ ] **Step 4: Run integration tests**

Run: `go test -race -v -tags=integration ./test/integration/...`
Expected: PASS

- [ ] **Step 5: Commit any final fixes**

```bash
jj commit -m "chore(session): final lint and test fixes for session persistence"
```

### Task 8d: Close Beads

- [ ] **Step 1: Close the session persistence task**

```bash
bd close <task-id> --reason "Session persistence implemented: Postgres-backed sessions, detach/reattach, event replay, command history, two-phase login, grid presence phasing, multi-connection support"
```

---

## Post-Implementation Checklist

- [ ] All unit tests pass (`task test`)
- [ ] All integration tests pass (`go test -race -v -tags=integration ./test/integration/...`)
- [ ] Linter passes (`task lint`)
- [ ] Build succeeds (`task build`)
- [ ] Migration 000018 applies cleanly to fresh database
- [ ] Migration 000018 rolls back cleanly
- [ ] `grpc.SessionInfo` and `grpc.InMemorySessionStore` are fully deleted
- [ ] All `SessionStore` references point to `session.Store`
- [ ] No TODO comments left without corresponding beads issues
- [ ] SPDX headers on all new files
- [ ] Conventional commits throughout
