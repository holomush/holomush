# Phase 1: Tracer Bullet Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a functional telnet server where users connect, chat, disconnect, reconnect, and see missed events.

**Architecture:** Event-sourced Go server with PostgreSQL storage. Telnet adapter translates line I/O to internal events. Sessions persist across disconnections with event replay on reconnect.

**Tech Stack:** Go 1.22+, PostgreSQL 16+, wazero (WASM), oklog/ulid

**Epic:** holomush-9qb

---

## Task 1: Project Dependencies

**Files:**

- Modify: `go.mod`

**Step 1: Add required dependencies**

```bash
go get github.com/oklog/ulid/v2
go get github.com/jackc/pgx/v5
go get github.com/tetratelabs/wazero
go get golang.org/x/crypto/bcrypt
```

**Step 2: Verify go.mod updated**

Run: `cat go.mod`
Expected: Shows ulid, pgx, wazero, bcrypt dependencies

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "build: add core dependencies for Phase 1"
```

---

## Task 2: Event Types and ULID Generator

**Files:**

- Create: `internal/core/event.go`
- Create: `internal/core/event_test.go`
- Create: `internal/core/ulid.go`
- Create: `internal/core/ulid_test.go`

**Step 1: Write failing test for EventType**

Create `internal/core/event_test.go`:

```go
package core

import "testing"

func TestEventType_String(t *testing.T) {
    tests := []struct {
        name     string
        input    EventType
        expected string
    }{
        {"say event", EventTypeSay, "say"},
        {"pose event", EventTypePose, "pose"},
        {"arrive event", EventTypeArrive, "arrive"},
        {"leave event", EventTypeLeave, "leave"},
        {"system event", EventTypeSystem, "system"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := string(tt.input); got != tt.expected {
                t.Errorf("got %q, want %q", got, tt.expected)
            }
        })
    }
}

func TestActorKind_String(t *testing.T) {
    tests := []struct {
        name     string
        input    ActorKind
        expected string
    }{
        {"character", ActorCharacter, "character"},
        {"system", ActorSystem, "system"},
        {"plugin", ActorPlugin, "plugin"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := tt.input.String(); got != tt.expected {
                t.Errorf("got %q, want %q", got, tt.expected)
            }
        })
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/core/... -v`
Expected: FAIL - package not found or types not defined

**Step 3: Write Event types implementation**

Create `internal/core/event.go`:

```go
// Package core contains the core game engine types and logic.
package core

import (
    "time"

    "github.com/oklog/ulid/v2"
)

// EventType identifies the kind of event.
type EventType string

const (
    EventTypeSay    EventType = "say"
    EventTypePose   EventType = "pose"
    EventTypeArrive EventType = "arrive"
    EventTypeLeave  EventType = "leave"
    EventTypeSystem EventType = "system"
)

// ActorKind identifies what type of entity caused an event.
type ActorKind uint8

const (
    ActorCharacter ActorKind = iota
    ActorSystem
    ActorPlugin
)

func (a ActorKind) String() string {
    switch a {
    case ActorCharacter:
        return "character"
    case ActorSystem:
        return "system"
    case ActorPlugin:
        return "plugin"
    default:
        return "unknown"
    }
}

// Actor represents who or what caused an event.
type Actor struct {
    Kind ActorKind
    ID   string // Character ID, plugin name, or "system"
}

// Event represents something that happened in the game world.
type Event struct {
    ID        ulid.ULID
    Stream    string    // e.g., "location:01ABC", "char:01XYZ"
    Type      EventType
    Timestamp time.Time
    Actor     Actor
    Payload   []byte // JSON
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/core/... -v`
Expected: PASS

**Step 5: Write failing test for ULID generator**

Create `internal/core/ulid_test.go`:

```go
package core

import "testing"

func TestNewULID(t *testing.T) {
    id1 := NewULID()
    id2 := NewULID()

    if id1.String() == "" {
        t.Error("ULID should not be empty")
    }

    if id1.String() == id2.String() {
        t.Error("Two ULIDs should be different")
    }

    // ULIDs should be lexicographically sortable by time
    if id1.String() > id2.String() {
        t.Error("Later ULID should sort after earlier ULID")
    }
}

func TestParseULID(t *testing.T) {
    original := NewULID()
    parsed, err := ParseULID(original.String())
    if err != nil {
        t.Fatalf("ParseULID failed: %v", err)
    }
    if parsed != original {
        t.Errorf("Parsed ULID %v != original %v", parsed, original)
    }
}

func TestParseULID_Invalid(t *testing.T) {
    _, err := ParseULID("invalid")
    if err == nil {
        t.Error("ParseULID should fail on invalid input")
    }
}
```

**Step 6: Run test to verify it fails**

Run: `go test ./internal/core/... -v`
Expected: FAIL - NewULID, ParseULID not defined

**Step 7: Write ULID implementation**

Create `internal/core/ulid.go`:

```go
package core

import (
    "crypto/rand"
    "sync"
    "time"

    "github.com/oklog/ulid/v2"
)

var (
    entropy     = ulid.Monotonic(rand.Reader, 0)
    entropyLock sync.Mutex
)

// NewULID generates a new ULID.
func NewULID() ulid.ULID {
    entropyLock.Lock()
    defer entropyLock.Unlock()
    return ulid.MustNew(ulid.Timestamp(time.Now()), entropy)
}

// ParseULID parses a ULID string.
func ParseULID(s string) (ulid.ULID, error) {
    return ulid.Parse(s)
}
```

**Step 8: Run test to verify it passes**

Run: `go test ./internal/core/... -v`
Expected: PASS

**Step 9: Commit**

```bash
git add internal/core/
git commit -m "feat(core): add Event types and ULID generator"
```

---

## Task 3: EventStore Interface

**Files:**

- Create: `internal/core/store.go`
- Create: `internal/core/store_test.go`

**Step 1: Write the interface and memory implementation test**

Create `internal/core/store_test.go`:

```go
package core

import (
    "context"
    "testing"
    "time"

    "github.com/oklog/ulid/v2"
)

func TestMemoryEventStore_Append(t *testing.T) {
    store := NewMemoryEventStore()
    ctx := context.Background()

    event := Event{
        ID:        NewULID(),
        Stream:    "location:test",
        Type:      EventTypeSay,
        Timestamp: time.Now(),
        Actor:     Actor{Kind: ActorCharacter, ID: "char1"},
        Payload:   []byte(`{"message":"hello"}`),
    }

    err := store.Append(ctx, event)
    if err != nil {
        t.Fatalf("Append failed: %v", err)
    }
}

func TestMemoryEventStore_Replay(t *testing.T) {
    store := NewMemoryEventStore()
    ctx := context.Background()

    // Append 5 events
    var ids []ulid.ULID
    for i := 0; i < 5; i++ {
        event := Event{
            ID:        NewULID(),
            Stream:    "location:test",
            Type:      EventTypeSay,
            Timestamp: time.Now(),
            Actor:     Actor{Kind: ActorCharacter, ID: "char1"},
            Payload:   []byte(`{}`),
        }
        ids = append(ids, event.ID)
        if err := store.Append(ctx, event); err != nil {
            t.Fatalf("Append failed: %v", err)
        }
        time.Sleep(time.Millisecond) // Ensure different timestamps
    }

    // Replay from beginning, limit 3
    events, err := store.Replay(ctx, "location:test", ulid.ULID{}, 3)
    if err != nil {
        t.Fatalf("Replay failed: %v", err)
    }
    if len(events) != 3 {
        t.Errorf("Expected 3 events, got %d", len(events))
    }

    // Replay after third event
    events, err = store.Replay(ctx, "location:test", ids[2], 10)
    if err != nil {
        t.Fatalf("Replay failed: %v", err)
    }
    if len(events) != 2 {
        t.Errorf("Expected 2 events after id[2], got %d", len(events))
    }
}

func TestMemoryEventStore_LastEventID(t *testing.T) {
    store := NewMemoryEventStore()
    ctx := context.Background()

    // Empty stream
    _, err := store.LastEventID(ctx, "empty")
    if err == nil {
        t.Error("Expected error for empty stream")
    }

    // Add event
    event := Event{
        ID:        NewULID(),
        Stream:    "location:test",
        Type:      EventTypeSay,
        Timestamp: time.Now(),
        Actor:     Actor{Kind: ActorSystem, ID: "system"},
        Payload:   []byte(`{}`),
    }
    store.Append(ctx, event)

    lastID, err := store.LastEventID(ctx, "location:test")
    if err != nil {
        t.Fatalf("LastEventID failed: %v", err)
    }
    if lastID != event.ID {
        t.Errorf("Expected %v, got %v", event.ID, lastID)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/core/... -v`
Expected: FAIL - EventStore, NewMemoryEventStore not defined

**Step 3: Write EventStore interface and MemoryEventStore**

Create `internal/core/store.go`:

```go
package core

import (
    "context"
    "errors"
    "sync"

    "github.com/oklog/ulid/v2"
)

// ErrStreamEmpty is returned when a stream has no events.
var ErrStreamEmpty = errors.New("stream is empty")

// EventStore persists and retrieves events.
type EventStore interface {
    // Append persists an event to a stream.
    Append(ctx context.Context, event Event) error

    // Replay returns up to limit events from a stream, starting after afterID.
    // If afterID is zero ULID, starts from beginning.
    Replay(ctx context.Context, stream string, afterID ulid.ULID, limit int) ([]Event, error)

    // LastEventID returns the most recent event ID for a stream.
    LastEventID(ctx context.Context, stream string) (ulid.ULID, error)
}

// MemoryEventStore is an in-memory EventStore for testing.
type MemoryEventStore struct {
    mu      sync.RWMutex
    streams map[string][]Event
}

// NewMemoryEventStore creates a new in-memory event store.
func NewMemoryEventStore() *MemoryEventStore {
    return &MemoryEventStore{
        streams: make(map[string][]Event),
    }
}

func (s *MemoryEventStore) Append(ctx context.Context, event Event) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.streams[event.Stream] = append(s.streams[event.Stream], event)
    return nil
}

func (s *MemoryEventStore) Replay(ctx context.Context, stream string, afterID ulid.ULID, limit int) ([]Event, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    events := s.streams[stream]
    if len(events) == 0 {
        return nil, nil
    }

    // Find start index
    startIdx := 0
    if afterID.Compare(ulid.ULID{}) != 0 {
        for i, e := range events {
            if e.ID == afterID {
                startIdx = i + 1
                break
            }
        }
    }

    // Slice with limit
    endIdx := startIdx + limit
    if endIdx > len(events) {
        endIdx = len(events)
    }

    result := make([]Event, endIdx-startIdx)
    copy(result, events[startIdx:endIdx])
    return result, nil
}

func (s *MemoryEventStore) LastEventID(ctx context.Context, stream string) (ulid.ULID, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    events := s.streams[stream]
    if len(events) == 0 {
        return ulid.ULID{}, ErrStreamEmpty
    }
    return events[len(events)-1].ID, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/core/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/core/
git commit -m "feat(core): add EventStore interface with memory implementation"
```

---

## Task 4: PostgreSQL EventStore

**Files:**

- Create: `internal/store/postgres.go`
- Create: `internal/store/postgres_test.go`
- Create: `internal/store/migrations/001_initial.sql`

**Step 1: Create migration file**

Create `internal/store/migrations/001_initial.sql`:

```sql
-- Players (accounts)
CREATE TABLE IF NOT EXISTS players (
    id TEXT PRIMARY KEY,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Locations
CREATE TABLE IF NOT EXISTS locations (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Characters
CREATE TABLE IF NOT EXISTS characters (
    id TEXT PRIMARY KEY,
    player_id TEXT NOT NULL REFERENCES players(id),
    name TEXT NOT NULL,
    location_id TEXT NOT NULL REFERENCES locations(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Events
CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    stream TEXT NOT NULL,
    type TEXT NOT NULL,
    actor_kind SMALLINT NOT NULL,
    actor_id TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_events_stream_id ON events (stream, id);

-- Sessions
CREATE TABLE IF NOT EXISTS sessions (
    character_id TEXT PRIMARY KEY REFERENCES characters(id),
    last_event_id TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Test data
INSERT INTO players (id, username, password_hash)
VALUES ('01JTEST001PLAYER00000001', 'testuser', '$2a$10$N9qo8uLOickgx2ZMRZoMye')
ON CONFLICT (id) DO NOTHING;

INSERT INTO locations (id, name, description)
VALUES ('01JTEST001LOCATN00000001', 'The Void', 'An empty expanse of nothing. This is where it all begins.')
ON CONFLICT (id) DO NOTHING;

INSERT INTO characters (id, player_id, name, location_id)
VALUES ('01JTEST001CHRCTR00000001', '01JTEST001PLAYER00000001', 'TestChar', '01JTEST001LOCATN00000001')
ON CONFLICT (id) DO NOTHING;
```

**Step 2: Write PostgresEventStore test**

Create `internal/store/postgres_test.go`:

```go
//go:build integration

package store

import (
    "context"
    "os"
    "testing"
    "time"

    "github.com/holomush/holomush/internal/core"
)

func TestPostgresEventStore_Integration(t *testing.T) {
    dsn := os.Getenv("DATABASE_URL")
    if dsn == "" {
        t.Skip("DATABASE_URL not set, skipping integration test")
    }

    ctx := context.Background()
    store, err := NewPostgresEventStore(ctx, dsn)
    if err != nil {
        t.Fatalf("Failed to create store: %v", err)
    }
    defer store.Close()

    // Run migrations
    if err := store.Migrate(ctx); err != nil {
        t.Fatalf("Migration failed: %v", err)
    }

    stream := "test:" + core.NewULID().String()

    // Test Append
    event := core.Event{
        ID:        core.NewULID(),
        Stream:    stream,
        Type:      core.EventTypeSay,
        Timestamp: time.Now(),
        Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char1"},
        Payload:   []byte(`{"message":"hello"}`),
    }

    if err := store.Append(ctx, event); err != nil {
        t.Fatalf("Append failed: %v", err)
    }

    // Test Replay
    events, err := store.Replay(ctx, stream, core.ulid.ULID{}, 10)
    if err != nil {
        t.Fatalf("Replay failed: %v", err)
    }
    if len(events) != 1 {
        t.Errorf("Expected 1 event, got %d", len(events))
    }

    // Test LastEventID
    lastID, err := store.LastEventID(ctx, stream)
    if err != nil {
        t.Fatalf("LastEventID failed: %v", err)
    }
    if lastID != event.ID {
        t.Errorf("Expected %v, got %v", event.ID, lastID)
    }
}
```

**Step 3: Write PostgresEventStore implementation**

Create `internal/store/postgres.go`:

```go
// Package store provides storage implementations.
package store

import (
    "context"
    _ "embed"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/oklog/ulid/v2"

    "github.com/holomush/holomush/internal/core"
)

//go:embed migrations/001_initial.sql
var migrationSQL string

// PostgresEventStore implements EventStore using PostgreSQL.
type PostgresEventStore struct {
    pool *pgxpool.Pool
}

// NewPostgresEventStore creates a new PostgreSQL event store.
func NewPostgresEventStore(ctx context.Context, dsn string) (*PostgresEventStore, error) {
    pool, err := pgxpool.New(ctx, dsn)
    if err != nil {
        return nil, err
    }
    return &PostgresEventStore{pool: pool}, nil
}

// Close closes the database connection pool.
func (s *PostgresEventStore) Close() {
    s.pool.Close()
}

// Migrate runs database migrations.
func (s *PostgresEventStore) Migrate(ctx context.Context) error {
    _, err := s.pool.Exec(ctx, migrationSQL)
    return err
}

// Append persists an event.
func (s *PostgresEventStore) Append(ctx context.Context, event core.Event) error {
    _, err := s.pool.Exec(ctx,
        `INSERT INTO events (id, stream, type, actor_kind, actor_id, payload, created_at)
         VALUES ($1, $2, $3, $4, $5, $6, $7)`,
        event.ID.String(),
        event.Stream,
        string(event.Type),
        event.Actor.Kind,
        event.Actor.ID,
        event.Payload,
        event.Timestamp,
    )
    return err
}

// Replay returns events from a stream after the given ID.
func (s *PostgresEventStore) Replay(ctx context.Context, stream string, afterID ulid.ULID, limit int) ([]core.Event, error) {
    var rows pgx.Rows
    var err error

    if afterID.Compare(ulid.ULID{}) == 0 {
        rows, err = s.pool.Query(ctx,
            `SELECT id, stream, type, actor_kind, actor_id, payload, created_at
             FROM events WHERE stream = $1 ORDER BY id LIMIT $2`,
            stream, limit)
    } else {
        rows, err = s.pool.Query(ctx,
            `SELECT id, stream, type, actor_kind, actor_id, payload, created_at
             FROM events WHERE stream = $1 AND id > $2 ORDER BY id LIMIT $3`,
            stream, afterID.String(), limit)
    }
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var events []core.Event
    for rows.Next() {
        var e core.Event
        var idStr string
        var typeStr string
        if err := rows.Scan(&idStr, &e.Stream, &typeStr, &e.Actor.Kind, &e.Actor.ID, &e.Payload, &e.Timestamp); err != nil {
            return nil, err
        }
        e.ID, _ = ulid.Parse(idStr)
        e.Type = core.EventType(typeStr)
        events = append(events, e)
    }
    return events, rows.Err()
}

// LastEventID returns the most recent event ID for a stream.
func (s *PostgresEventStore) LastEventID(ctx context.Context, stream string) (ulid.ULID, error) {
    var idStr string
    err := s.pool.QueryRow(ctx,
        `SELECT id FROM events WHERE stream = $1 ORDER BY id DESC LIMIT 1`,
        stream).Scan(&idStr)
    if err == pgx.ErrNoRows {
        return ulid.ULID{}, core.ErrStreamEmpty
    }
    if err != nil {
        return ulid.ULID{}, err
    }
    return ulid.Parse(idStr)
}
```

**Step 4: Run unit tests (memory store)**

Run: `go test ./internal/core/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat(store): add PostgreSQL EventStore implementation"
```

---

## Task 5: Session Manager

**Files:**

- Create: `internal/core/session.go`
- Create: `internal/core/session_test.go`

**Step 1: Write failing session test**

Create `internal/core/session_test.go`:

```go
package core

import (
    "testing"

    "github.com/oklog/ulid/v2"
)

func TestSessionManager_Connect(t *testing.T) {
    sm := NewSessionManager()

    charID := NewULID()
    connID := NewULID()

    session := sm.Connect(charID, connID)
    if session == nil {
        t.Fatal("Expected session, got nil")
    }
    if session.CharacterID != charID {
        t.Errorf("CharacterID mismatch")
    }
    if len(session.Connections) != 1 {
        t.Errorf("Expected 1 connection, got %d", len(session.Connections))
    }
}

func TestSessionManager_Reconnect(t *testing.T) {
    sm := NewSessionManager()

    charID := NewULID()
    conn1 := NewULID()
    conn2 := NewULID()

    // First connection
    session1 := sm.Connect(charID, conn1)

    // Update cursor
    eventID := NewULID()
    sm.UpdateCursor(charID, "location:test", eventID)

    // Disconnect
    sm.Disconnect(charID, conn1)

    // Reconnect with new connection
    session2 := sm.Connect(charID, conn2)

    // Should be same session with preserved cursor
    if session2.CharacterID != session1.CharacterID {
        t.Error("Should be same session")
    }
    if session2.EventCursors["location:test"] != eventID {
        t.Error("Cursor should be preserved")
    }
}

func TestSessionManager_MultipleConnections(t *testing.T) {
    sm := NewSessionManager()

    charID := NewULID()
    conn1 := NewULID()
    conn2 := NewULID()

    sm.Connect(charID, conn1)
    session := sm.Connect(charID, conn2)

    if len(session.Connections) != 2 {
        t.Errorf("Expected 2 connections, got %d", len(session.Connections))
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/core/... -v`
Expected: FAIL - SessionManager not defined

**Step 3: Write SessionManager implementation**

Create `internal/core/session.go`:

```go
package core

import (
    "sync"

    "github.com/oklog/ulid/v2"
)

// Session represents a character's ongoing presence in the game.
type Session struct {
    CharacterID  ulid.ULID
    Connections  []ulid.ULID          // Active connection IDs
    EventCursors map[string]ulid.ULID // Last seen event per stream
}

// SessionManager manages character sessions.
type SessionManager struct {
    mu       sync.RWMutex
    sessions map[ulid.ULID]*Session // keyed by CharacterID
}

// NewSessionManager creates a new session manager.
func NewSessionManager() *SessionManager {
    return &SessionManager{
        sessions: make(map[ulid.ULID]*Session),
    }
}

// Connect attaches a connection to a character's session.
// Creates the session if it doesn't exist.
func (sm *SessionManager) Connect(charID, connID ulid.ULID) *Session {
    sm.mu.Lock()
    defer sm.mu.Unlock()

    session, exists := sm.sessions[charID]
    if !exists {
        session = &Session{
            CharacterID:  charID,
            Connections:  make([]ulid.ULID, 0, 1),
            EventCursors: make(map[string]ulid.ULID),
        }
        sm.sessions[charID] = session
    }

    session.Connections = append(session.Connections, connID)
    return session
}

// Disconnect removes a connection from a character's session.
// The session persists even with zero connections.
func (sm *SessionManager) Disconnect(charID, connID ulid.ULID) {
    sm.mu.Lock()
    defer sm.mu.Unlock()

    session, exists := sm.sessions[charID]
    if !exists {
        return
    }

    // Remove connection
    for i, id := range session.Connections {
        if id == connID {
            session.Connections = append(session.Connections[:i], session.Connections[i+1:]...)
            break
        }
    }
}

// UpdateCursor updates the last seen event for a stream.
func (sm *SessionManager) UpdateCursor(charID ulid.ULID, stream string, eventID ulid.ULID) {
    sm.mu.Lock()
    defer sm.mu.Unlock()

    session, exists := sm.sessions[charID]
    if !exists {
        return
    }
    session.EventCursors[stream] = eventID
}

// GetSession returns a character's session, or nil if none exists.
func (sm *SessionManager) GetSession(charID ulid.ULID) *Session {
    sm.mu.RLock()
    defer sm.mu.RUnlock()
    return sm.sessions[charID]
}

// GetConnections returns all connection IDs for a character.
func (sm *SessionManager) GetConnections(charID ulid.ULID) []ulid.ULID {
    sm.mu.RLock()
    defer sm.mu.RUnlock()

    session, exists := sm.sessions[charID]
    if !exists {
        return nil
    }
    result := make([]ulid.ULID, len(session.Connections))
    copy(result, session.Connections)
    return result
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/core/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/core/
git commit -m "feat(core): add SessionManager for connection tracking"
```

---

## Task 6: Telnet Server Foundation

**Files:**

- Create: `internal/telnet/server.go`
- Create: `internal/telnet/server_test.go`

**Step 1: Write failing test for telnet server**

Create `internal/telnet/server_test.go`:

```go
package telnet

import (
    "bufio"
    "context"
    "net"
    "testing"
    "time"
)

func TestServer_AcceptsConnections(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Start server on random port
    srv := NewServer(":0")
    go srv.Run(ctx)

    // Wait for server to start
    time.Sleep(50 * time.Millisecond)

    addr := srv.Addr()
    if addr == "" {
        t.Fatal("Server has no address")
    }

    // Connect
    conn, err := net.Dial("tcp", addr)
    if err != nil {
        t.Fatalf("Failed to connect: %v", err)
    }
    defer conn.Close()

    // Should receive welcome message
    conn.SetReadDeadline(time.Now().Add(time.Second))
    reader := bufio.NewReader(conn)
    line, err := reader.ReadString('\n')
    if err != nil {
        t.Fatalf("Failed to read welcome: %v", err)
    }
    if line == "" {
        t.Error("Expected welcome message")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/telnet/... -v`
Expected: FAIL - package/types not defined

**Step 3: Write basic telnet server**

Create `internal/telnet/server.go`:

```go
// Package telnet provides the telnet protocol adapter.
package telnet

import (
    "bufio"
    "context"
    "fmt"
    "log/slog"
    "net"
    "sync"
)

// Server is a telnet server.
type Server struct {
    addr     string
    listener net.Listener
    mu       sync.RWMutex
}

// NewServer creates a new telnet server.
func NewServer(addr string) *Server {
    return &Server{addr: addr}
}

// Addr returns the server's listen address.
func (s *Server) Addr() string {
    s.mu.RLock()
    defer s.mu.RUnlock()
    if s.listener == nil {
        return ""
    }
    return s.listener.Addr().String()
}

// Run starts the server and blocks until context is cancelled.
func (s *Server) Run(ctx context.Context) error {
    listener, err := net.Listen("tcp", s.addr)
    if err != nil {
        return fmt.Errorf("failed to listen: %w", err)
    }

    s.mu.Lock()
    s.listener = listener
    s.mu.Unlock()

    slog.Info("Telnet server started", "addr", listener.Addr())

    go func() {
        <-ctx.Done()
        listener.Close()
    }()

    for {
        conn, err := listener.Accept()
        if err != nil {
            select {
            case <-ctx.Done():
                return nil
            default:
                slog.Error("Accept failed", "error", err)
                continue
            }
        }
        go s.handleConnection(ctx, conn)
    }
}

func (s *Server) handleConnection(ctx context.Context, conn net.Conn) {
    defer conn.Close()

    slog.Info("New connection", "remote", conn.RemoteAddr())

    // Send welcome
    fmt.Fprintln(conn, "Welcome to HoloMUSH!")
    fmt.Fprintln(conn, "Use: connect <username> <password>")

    reader := bufio.NewReader(conn)
    for {
        select {
        case <-ctx.Done():
            return
        default:
        }

        line, err := reader.ReadString('\n')
        if err != nil {
            slog.Info("Connection closed", "remote", conn.RemoteAddr())
            return
        }

        // Echo for now (will be replaced with command handling)
        fmt.Fprintf(conn, "You said: %s", line)
    }
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/telnet/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/telnet/
git commit -m "feat(telnet): add basic telnet server"
```

---

## Task 7: Command Parser and Handler

**Files:**

- Create: `internal/core/command.go`
- Create: `internal/core/command_test.go`

**Step 1: Write failing test for command parsing**

Create `internal/core/command_test.go`:

```go
package core

import "testing"

func TestParseCommand(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        wantCmd string
        wantArg string
    }{
        {"connect", "connect user pass", "connect", "user pass"},
        {"say", "say hello world", "say", "hello world"},
        {"look", "look", "look", ""},
        {"pose", "pose waves", "pose", "waves"},
        {"quit", "quit", "quit", ""},
        {"empty", "", "", ""},
        {"whitespace", "  say  hello  ", "say", "hello"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            cmd, arg := ParseCommand(tt.input)
            if cmd != tt.wantCmd {
                t.Errorf("cmd = %q, want %q", cmd, tt.wantCmd)
            }
            if arg != tt.wantArg {
                t.Errorf("arg = %q, want %q", arg, tt.wantArg)
            }
        })
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/core/... -v -run TestParseCommand`
Expected: FAIL - ParseCommand not defined

**Step 3: Write command parser**

Create `internal/core/command.go`:

```go
package core

import "strings"

// ParseCommand splits input into command and arguments.
func ParseCommand(input string) (cmd, arg string) {
    input = strings.TrimSpace(input)
    if input == "" {
        return "", ""
    }

    parts := strings.SplitN(input, " ", 2)
    cmd = strings.ToLower(parts[0])
    if len(parts) > 1 {
        arg = strings.TrimSpace(parts[1])
    }
    return cmd, arg
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/core/... -v -run TestParseCommand`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/core/
git commit -m "feat(core): add command parser"
```

---

## Task 8: Game Engine Core

**Files:**

- Create: `internal/core/engine.go`
- Create: `internal/core/engine_test.go`

**Step 1: Write failing engine test**

Create `internal/core/engine_test.go`:

```go
package core

import (
    "context"
    "testing"
)

func TestEngine_HandleSay(t *testing.T) {
    store := NewMemoryEventStore()
    sessions := NewSessionManager()
    engine := NewEngine(store, sessions)

    ctx := context.Background()
    charID := NewULID()
    locationID := NewULID()

    // Emit say event
    err := engine.HandleSay(ctx, charID, locationID, "Hello, world!")
    if err != nil {
        t.Fatalf("HandleSay failed: %v", err)
    }

    // Verify event was stored
    stream := "location:" + locationID.String()
    events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
    if err != nil {
        t.Fatalf("Replay failed: %v", err)
    }
    if len(events) != 1 {
        t.Errorf("Expected 1 event, got %d", len(events))
    }
    if events[0].Type != EventTypeSay {
        t.Errorf("Expected say event, got %v", events[0].Type)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/core/... -v -run TestEngine`
Expected: FAIL - Engine, NewEngine not defined

**Step 3: Write Engine implementation**

Add to `internal/core/engine.go`:

```go
package core

import (
    "context"
    "encoding/json"
    "time"

    "github.com/oklog/ulid/v2"
)

// SayPayload is the JSON payload for say events.
type SayPayload struct {
    Message string `json:"message"`
}

// PosePayload is the JSON payload for pose events.
type PosePayload struct {
    Action string `json:"action"`
}

// Engine is the core game engine.
type Engine struct {
    store    EventStore
    sessions *SessionManager
}

// NewEngine creates a new game engine.
func NewEngine(store EventStore, sessions *SessionManager) *Engine {
    return &Engine{
        store:    store,
        sessions: sessions,
    }
}

// HandleSay processes a say command.
func (e *Engine) HandleSay(ctx context.Context, charID, locationID ulid.ULID, message string) error {
    payload, _ := json.Marshal(SayPayload{Message: message})

    event := Event{
        ID:        NewULID(),
        Stream:    "location:" + locationID.String(),
        Type:      EventTypeSay,
        Timestamp: time.Now(),
        Actor:     Actor{Kind: ActorCharacter, ID: charID.String()},
        Payload:   payload,
    }

    return e.store.Append(ctx, event)
}

// HandlePose processes a pose command.
func (e *Engine) HandlePose(ctx context.Context, charID, locationID ulid.ULID, action string) error {
    payload, _ := json.Marshal(PosePayload{Action: action})

    event := Event{
        ID:        NewULID(),
        Stream:    "location:" + locationID.String(),
        Type:      EventTypePose,
        Timestamp: time.Now(),
        Actor:     Actor{Kind: ActorCharacter, ID: charID.String()},
        Payload:   payload,
    }

    return e.store.Append(ctx, event)
}

// ReplayEvents returns missed events for a character.
func (e *Engine) ReplayEvents(ctx context.Context, charID ulid.ULID, stream string, limit int) ([]Event, error) {
    session := e.sessions.GetSession(charID)
    var afterID ulid.ULID
    if session != nil {
        afterID = session.EventCursors[stream]
    }
    return e.store.Replay(ctx, stream, afterID, limit)
}
```

**Step 4: Fix import and run test**

Run: `go test ./internal/core/... -v -run TestEngine`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/core/
git commit -m "feat(core): add game Engine with say/pose handling"
```

---

## Task 9: Wire Telnet to Engine

**Files:**

- Modify: `internal/telnet/server.go`
- Create: `internal/telnet/handler.go`

**Step 1: Create connection handler**

Create `internal/telnet/handler.go`:

```go
package telnet

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"
    "net"
    "strings"

    "github.com/oklog/ulid/v2"

    "github.com/holomush/holomush/internal/core"
)

// ConnectionHandler handles a single telnet connection.
type ConnectionHandler struct {
    conn       net.Conn
    reader     *bufio.Reader
    engine     *core.Engine
    sessions   *core.SessionManager
    connID     ulid.ULID
    charID     ulid.ULID
    locationID ulid.ULID
    charName   string
    authed     bool
}

// NewConnectionHandler creates a new handler.
func NewConnectionHandler(conn net.Conn, engine *core.Engine, sessions *core.SessionManager) *ConnectionHandler {
    return &ConnectionHandler{
        conn:     conn,
        reader:   bufio.NewReader(conn),
        engine:   engine,
        sessions: sessions,
        connID:   core.NewULID(),
    }
}

// Handle processes the connection until closed.
func (h *ConnectionHandler) Handle(ctx context.Context) {
    defer h.conn.Close()

    h.send("Welcome to HoloMUSH!")
    h.send("Use: connect <username> <password>")

    for {
        select {
        case <-ctx.Done():
            return
        default:
        }

        line, err := h.reader.ReadString('\n')
        if err != nil {
            if h.authed {
                h.sessions.Disconnect(h.charID, h.connID)
            }
            return
        }

        h.processLine(ctx, strings.TrimSpace(line))
    }
}

func (h *ConnectionHandler) processLine(ctx context.Context, line string) {
    cmd, arg := core.ParseCommand(line)

    switch cmd {
    case "connect":
        h.handleConnect(ctx, arg)
    case "look":
        h.handleLook(ctx)
    case "say":
        h.handleSay(ctx, arg)
    case "pose":
        h.handlePose(ctx, arg)
    case "quit":
        h.handleQuit()
    default:
        if cmd != "" {
            h.send("Unknown command: " + cmd)
        }
    }
}

func (h *ConnectionHandler) handleConnect(ctx context.Context, arg string) {
    if h.authed {
        h.send("Already connected.")
        return
    }

    parts := strings.SplitN(arg, " ", 2)
    if len(parts) != 2 {
        h.send("Usage: connect <username> <password>")
        return
    }

    // Hardcoded test user for Phase 1
    username, password := parts[0], parts[1]
    if username != "testuser" || password != "password" {
        h.send("Invalid username or password.")
        return
    }

    // Hardcoded test character
    h.charID, _ = ulid.Parse("01JTEST001CHRCTR00000001")
    h.locationID, _ = ulid.Parse("01JTEST001LOCATN00000001")
    h.charName = "TestChar"
    h.authed = true

    h.sessions.Connect(h.charID, h.connID)
    h.send(fmt.Sprintf("Welcome back, %s!", h.charName))

    // Replay missed events
    stream := "location:" + h.locationID.String()
    events, _ := h.engine.ReplayEvents(ctx, h.charID, stream, 50)
    if len(events) > 0 {
        h.send(fmt.Sprintf("--- %d missed events ---", len(events)))
        for _, e := range events {
            h.sendEvent(e)
        }
        h.send("--- end of replay ---")
    }
}

func (h *ConnectionHandler) handleLook(ctx context.Context) {
    if !h.authed {
        h.send("You must connect first.")
        return
    }

    // Hardcoded for Phase 1
    h.send("The Void")
    h.send("An empty expanse of nothing. This is where it all begins.")
}

func (h *ConnectionHandler) handleSay(ctx context.Context, message string) {
    if !h.authed {
        h.send("You must connect first.")
        return
    }
    if message == "" {
        h.send("Say what?")
        return
    }

    h.engine.HandleSay(ctx, h.charID, h.locationID, message)
}

func (h *ConnectionHandler) handlePose(ctx context.Context, action string) {
    if !h.authed {
        h.send("You must connect first.")
        return
    }
    if action == "" {
        h.send("Pose what?")
        return
    }

    h.engine.HandlePose(ctx, h.charID, h.locationID, action)
}

func (h *ConnectionHandler) handleQuit() {
    h.send("Goodbye! Your session has been saved.")
    if h.authed {
        h.sessions.Disconnect(h.charID, h.connID)
    }
    h.conn.Close()
}

func (h *ConnectionHandler) send(msg string) {
    fmt.Fprintln(h.conn, msg)
}

func (h *ConnectionHandler) sendEvent(e core.Event) {
    switch e.Type {
    case core.EventTypeSay:
        var p core.SayPayload
        json.Unmarshal(e.Payload, &p)
        h.send(fmt.Sprintf("[%s] %s says, \"%s\"", e.Actor.ID[:8], h.charName, p.Message))
    case core.EventTypePose:
        var p core.PosePayload
        json.Unmarshal(e.Payload, &p)
        h.send(fmt.Sprintf("[%s] %s %s", e.Actor.ID[:8], h.charName, p.Action))
    }
}
```

**Step 2: Update Server to use handler**

Modify `internal/telnet/server.go` to add engine injection:

```go
// Package telnet provides the telnet protocol adapter.
package telnet

import (
    "context"
    "fmt"
    "log/slog"
    "net"
    "sync"

    "github.com/holomush/holomush/internal/core"
)

// Server is a telnet server.
type Server struct {
    addr     string
    listener net.Listener
    engine   *core.Engine
    sessions *core.SessionManager
    mu       sync.RWMutex
}

// NewServer creates a new telnet server.
func NewServer(addr string, engine *core.Engine, sessions *core.SessionManager) *Server {
    return &Server{
        addr:     addr,
        engine:   engine,
        sessions: sessions,
    }
}

// Addr returns the server's listen address.
func (s *Server) Addr() string {
    s.mu.RLock()
    defer s.mu.RUnlock()
    if s.listener == nil {
        return ""
    }
    return s.listener.Addr().String()
}

// Run starts the server and blocks until context is cancelled.
func (s *Server) Run(ctx context.Context) error {
    listener, err := net.Listen("tcp", s.addr)
    if err != nil {
        return fmt.Errorf("failed to listen: %w", err)
    }

    s.mu.Lock()
    s.listener = listener
    s.mu.Unlock()

    slog.Info("Telnet server started", "addr", listener.Addr())

    go func() {
        <-ctx.Done()
        listener.Close()
    }()

    for {
        conn, err := listener.Accept()
        if err != nil {
            select {
            case <-ctx.Done():
                return nil
            default:
                slog.Error("Accept failed", "error", err)
                continue
            }
        }
        handler := NewConnectionHandler(conn, s.engine, s.sessions)
        go handler.Handle(ctx)
    }
}
```

**Step 3: Update server test**

Update `internal/telnet/server_test.go`:

```go
package telnet

import (
    "bufio"
    "context"
    "net"
    "testing"
    "time"

    "github.com/holomush/holomush/internal/core"
)

func TestServer_AcceptsConnections(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    store := core.NewMemoryEventStore()
    sessions := core.NewSessionManager()
    engine := core.NewEngine(store, sessions)

    srv := NewServer(":0", engine, sessions)
    go srv.Run(ctx)

    time.Sleep(50 * time.Millisecond)

    addr := srv.Addr()
    if addr == "" {
        t.Fatal("Server has no address")
    }

    conn, err := net.Dial("tcp", addr)
    if err != nil {
        t.Fatalf("Failed to connect: %v", err)
    }
    defer conn.Close()

    conn.SetReadDeadline(time.Now().Add(time.Second))
    reader := bufio.NewReader(conn)
    line, err := reader.ReadString('\n')
    if err != nil {
        t.Fatalf("Failed to read welcome: %v", err)
    }
    if line == "" {
        t.Error("Expected welcome message")
    }
}
```

**Step 4: Run tests**

Run: `go test ./internal/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/
git commit -m "feat(telnet): wire server to game engine with command handling"
```

---

## Task 10: Main Entry Point

**Files:**

- Modify: `cmd/holomush/main.go`

**Step 1: Update main.go**

```go
// Package main is the entry point for the HoloMUSH server.
package main

import (
    "context"
    "log/slog"
    "os"
    "os/signal"
    "syscall"

    "github.com/holomush/holomush/internal/core"
    "github.com/holomush/holomush/internal/telnet"
)

var (
    version = "dev"
    commit  = "unknown"
    date    = "unknown"
)

func main() {
    slog.Info("HoloMUSH starting",
        "version", version,
        "commit", commit,
        "date", date,
    )

    // Setup
    store := core.NewMemoryEventStore()
    sessions := core.NewSessionManager()
    engine := core.NewEngine(store, sessions)

    // Telnet server
    addr := os.Getenv("TELNET_ADDR")
    if addr == "" {
        addr = ":4201"
    }
    srv := telnet.NewServer(addr, engine, sessions)

    // Graceful shutdown
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    go func() {
        sigCh := make(chan os.Signal, 1)
        signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
        <-sigCh
        slog.Info("Shutting down...")
        cancel()
    }()

    // Run
    if err := srv.Run(ctx); err != nil {
        slog.Error("Server error", "error", err)
        os.Exit(1)
    }

    slog.Info("Server stopped")
}
```

**Step 2: Build and test manually**

Run: `go build ./cmd/holomush && ./holomush &`

In another terminal:

```bash
telnet localhost 4201
# Should see welcome message
# Type: connect testuser password
# Type: say hello
# Type: quit
```

**Step 3: Commit**

```bash
git add cmd/holomush/
git commit -m "feat: wire up main entry point with telnet server"
```

---

## Task 11: Event Broadcasting (Real-time)

**Files:**

- Create: `internal/core/broadcaster.go`
- Create: `internal/core/broadcaster_test.go`
- Modify: `internal/telnet/handler.go`

**Step 1: Write failing broadcaster test**

Create `internal/core/broadcaster_test.go`:

```go
package core

import (
    "testing"
    "time"
)

func TestBroadcaster_Subscribe(t *testing.T) {
    bc := NewBroadcaster()

    ch := bc.Subscribe("location:test")
    if ch == nil {
        t.Fatal("Expected channel")
    }

    // Broadcast event
    event := Event{ID: NewULID(), Stream: "location:test", Type: EventTypeSay}
    bc.Broadcast(event)

    select {
    case received := <-ch:
        if received.ID != event.ID {
            t.Errorf("Event ID mismatch")
        }
    case <-time.After(100 * time.Millisecond):
        t.Error("Timeout waiting for event")
    }
}

func TestBroadcaster_Unsubscribe(t *testing.T) {
    bc := NewBroadcaster()

    ch := bc.Subscribe("location:test")
    bc.Unsubscribe("location:test", ch)

    // Channel should be closed
    select {
    case _, ok := <-ch:
        if ok {
            t.Error("Channel should be closed")
        }
    case <-time.After(100 * time.Millisecond):
        t.Error("Channel should be closed immediately")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/core/... -v -run TestBroadcaster`
Expected: FAIL

**Step 3: Write Broadcaster implementation**

Create `internal/core/broadcaster.go`:

```go
package core

import "sync"

// Broadcaster distributes events to subscribers.
type Broadcaster struct {
    mu   sync.RWMutex
    subs map[string][]chan Event
}

// NewBroadcaster creates a new broadcaster.
func NewBroadcaster() *Broadcaster {
    return &Broadcaster{
        subs: make(map[string][]chan Event),
    }
}

// Subscribe creates a channel for receiving events on a stream.
func (b *Broadcaster) Subscribe(stream string) chan Event {
    b.mu.Lock()
    defer b.mu.Unlock()

    ch := make(chan Event, 100)
    b.subs[stream] = append(b.subs[stream], ch)
    return ch
}

// Unsubscribe removes a channel from a stream.
func (b *Broadcaster) Unsubscribe(stream string, ch chan Event) {
    b.mu.Lock()
    defer b.mu.Unlock()

    subs := b.subs[stream]
    for i, sub := range subs {
        if sub == ch {
            b.subs[stream] = append(subs[:i], subs[i+1:]...)
            close(ch)
            return
        }
    }
}

// Broadcast sends an event to all subscribers of its stream.
func (b *Broadcaster) Broadcast(event Event) {
    b.mu.RLock()
    defer b.mu.RUnlock()

    for _, ch := range b.subs[event.Stream] {
        select {
        case ch <- event:
        default:
            // Drop if buffer full
        }
    }
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/core/... -v -run TestBroadcaster`
Expected: PASS

**Step 5: Wire broadcaster into engine and handler**

This requires updating Engine to broadcast after storing, and Handler to subscribe and display events. (Details in implementation)

**Step 6: Commit**

```bash
git add internal/core/
git commit -m "feat(core): add Broadcaster for real-time event distribution"
```

---

## Task 12: WASM Plugin Proof of Concept

**Files:**

- Create: `internal/wasm/host.go`
- Create: `internal/wasm/host_test.go`
- Create: `plugins/hello/main.go` (compile to WASM)

**Step 1: Write failing test for WASM host**

Create `internal/wasm/host_test.go`:

```go
package wasm

import (
    "context"
    "testing"
)

func TestPluginHost_LoadPlugin(t *testing.T) {
    host := NewPluginHost()
    ctx := context.Background()

    // Test with embedded hello plugin
    err := host.LoadPlugin(ctx, "hello", helloWASM)
    if err != nil {
        t.Fatalf("LoadPlugin failed: %v", err)
    }

    // Call the plugin
    result, err := host.CallPlugin(ctx, "hello", "greet", []byte(`{"name":"World"}`))
    if err != nil {
        t.Fatalf("CallPlugin failed: %v", err)
    }
    if string(result) == "" {
        t.Error("Expected result from plugin")
    }
}
```

**Step 2: Create minimal WASM host**

Create `internal/wasm/host.go`:

```go
// Package wasm provides the WASM plugin host using wazero.
package wasm

import (
    "context"
    "fmt"

    "github.com/tetratelabs/wazero"
    "github.com/tetratelabs/wazero/api"
)

// PluginHost manages WASM plugins.
type PluginHost struct {
    runtime wazero.Runtime
    modules map[string]api.Module
}

// NewPluginHost creates a new plugin host.
func NewPluginHost() *PluginHost {
    return &PluginHost{
        modules: make(map[string]api.Module),
    }
}

// Initialize sets up the wazero runtime.
func (h *PluginHost) Initialize(ctx context.Context) error {
    h.runtime = wazero.NewRuntime(ctx)
    return nil
}

// Close shuts down the runtime.
func (h *PluginHost) Close(ctx context.Context) error {
    if h.runtime != nil {
        return h.runtime.Close(ctx)
    }
    return nil
}

// LoadPlugin loads a WASM module.
func (h *PluginHost) LoadPlugin(ctx context.Context, name string, wasm []byte) error {
    if h.runtime == nil {
        if err := h.Initialize(ctx); err != nil {
            return err
        }
    }

    mod, err := h.runtime.Instantiate(ctx, wasm)
    if err != nil {
        return fmt.Errorf("failed to instantiate %s: %w", name, err)
    }

    h.modules[name] = mod
    return nil
}

// CallPlugin calls a function in a loaded plugin.
func (h *PluginHost) CallPlugin(ctx context.Context, plugin, function string, input []byte) ([]byte, error) {
    mod, ok := h.modules[plugin]
    if !ok {
        return nil, fmt.Errorf("plugin %s not loaded", plugin)
    }

    fn := mod.ExportedFunction(function)
    if fn == nil {
        return nil, fmt.Errorf("function %s not found in %s", function, plugin)
    }

    // Simplified: real implementation needs memory management
    results, err := fn.Call(ctx)
    if err != nil {
        return nil, err
    }

    return []byte(fmt.Sprintf("%v", results)), nil
}
```

**Step 3: Create hello plugin (Go → WASM)**

Create `plugins/hello/main.go`:

```go
//go:build wasm

package main

//export greet
func greet() int32 {
    return 42 // Simplified proof-of-concept
}

func main() {}
```

Build: `GOOS=wasip1 GOARCH=wasm go build -o plugins/hello/hello.wasm ./plugins/hello/`

**Step 4: Test and commit**

```bash
git add internal/wasm/ plugins/hello/
git commit -m "feat(wasm): add plugin host proof-of-concept with wazero"
```

---

## Task 13: Final Integration and Cleanup

**Files:**

- Update: `cmd/holomush/main.go`
- Add: Integration test

**Step 1: Final main.go with all components**

Update to include broadcaster and prepare for PostgreSQL (flag-based).

**Step 2: Run full test suite**

```bash
task test
task lint
```

**Step 3: Final commit**

```bash
git add -A
git commit -m "feat: complete Phase 1 tracer bullet implementation"
```

---

## Summary

| Task | Component            | Tests           |
| ---- | -------------------- | --------------- |
| 1    | Dependencies         | -               |
| 2    | Event types, ULID    | ✓               |
| 3    | EventStore interface | ✓               |
| 4    | PostgreSQL store     | ✓ (integration) |
| 5    | SessionManager       | ✓               |
| 6    | Telnet server        | ✓               |
| 7    | Command parser       | ✓               |
| 8    | Game engine          | ✓               |
| 9    | Telnet + engine      | ✓               |
| 10   | Main entry point     | Manual          |
| 11   | Broadcaster          | ✓               |
| 12   | WASM plugin          | ✓               |
| 13   | Integration          | ✓               |

**Success Criteria Mapping:**

- ✓ Connect via telnet, authenticate → Tasks 6, 7, 9
- ✓ See events from others → Task 11
- ✓ Reconnect with replay → Tasks 3, 5, 9
- ✓ WASM plugin works → Task 12
- ✓ Events persisted → Tasks 3, 4
