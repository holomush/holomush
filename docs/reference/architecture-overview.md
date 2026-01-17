# Architecture Overview

HoloMUSH is a modern MUSH platform built with an event-sourced architecture. This document provides a high-level summary of the system design.

For complete specifications, see the [full architecture document](../plans/2026-01-17-holomush-architecture-design.md).

## Design Goals

| Goal                | Description                                        |
| ------------------- | -------------------------------------------------- |
| Event-sourced       | All game actions produce immutable, ordered events |
| Dual protocol       | Simultaneous web and telnet access                 |
| Plugin system       | Language-agnostic WASM plugins                     |
| Session persistence | Tmux-style reconnection with event replay          |
| Platform first      | Built for RP and sandbox gameplay                  |

## System Architecture

```text
+-------------------------------------------------------------------+
|                         Clients                                    |
+---------------------------+---------------------------------------+
|   Telnet Adapter          |           Web (SvelteKit PWA)         |
|   (ANSI, GMCP)            |   Terminal | Wiki | Forum | Admin     |
+-----------+---------------+-------------------+-------------------+
            |                                   |
            v                                   v
+-------------------------------------------------------------------+
|                    Go Core (Event-Oriented)                        |
+-------------------------------------------------------------------+
|  Session Manager   |  World Engine   |  Plugin Host (wazero)      |
|  - Per-char buffer |  - Locations    |  - WASM sandbox            |
|  - Reconnect/sync  |  - Objects      |  - Capability grants       |
|  - Protocol adapt  |  - Commands     |  - Multi-language          |
+--------+--------------------+--------------------+-----------------+
         |                    |                    |
         v                    v                    v
+------------------+ +-----------------+ +------------------------+
|  Event Store     | |   PostgreSQL    | |  ABAC Policy Engine    |
|  (PG/NATS)       | |  World + Content| |  Attribute-based ACL   |
+------------------+ +-----------------+ +------------------------+
```

## Core Concepts

### Event Sourcing

All game activity flows through ordered, persistent event streams:

- Commands from players produce events
- Events are stored before acknowledgment
- State is derived from event replay
- Enables reconnection with full catch-up

### Event Structure

```go
type Event struct {
    ID        ulid.ULID  // Globally unique, sortable
    Stream    string     // e.g., "location:42", "char:7"
    Type      EventType  // say, pose, arrive, leave, system
    Timestamp time.Time
    Actor     Actor      // Who caused this event
    Payload   []byte     // JSON event data
}
```

### Stream Types

| Stream           | Content                               | Subscribers            |
| ---------------- | ------------------------------------- | ---------------------- |
| `location:<id>`  | Room activity (says, poses, arrivals) | Characters in location |
| `channel:<name>` | Channel messages                      | Channel members        |
| `char:<id>`      | Private messages, notifications       | That character         |
| `world`          | Global events (connects, broadcasts)  | Admins, plugins        |

### Session Model

Sessions persist across disconnections:

```go
type Session struct {
    CharacterID  ulid.ULID
    Connections  []ulid.ULID          // Zero or more active
    EventCursors map[string]ulid.ULID // Last seen per stream
}
```

- Multiple simultaneous connections allowed
- Session persists even with zero connections
- Reconnection replays missed events

## Directory Structure

```text
cmd/holomush/        # Server entry point
internal/            # Private implementation
  core/              # Event system, sessions, engine
    event.go         # Event types and structures
    store.go         # EventStore interface
    session.go       # Session management
    engine.go        # Command processing
    broadcaster.go   # Real-time event distribution
  telnet/            # Telnet protocol adapter
    server.go        # TCP server
    handler.go       # Connection handling
  store/             # Storage implementations
    postgres.go      # PostgreSQL EventStore
  wasm/              # Plugin host
    host.go          # wazero runtime
pkg/                 # Public plugin API (future)
plugins/             # WASM plugins
docs/
  specs/             # Specifications
  plans/             # Implementation plans
  reference/         # This documentation
```

## Key Interfaces

### EventStore

The primary storage abstraction:

```go
type EventStore interface {
    // Append persists an event
    Append(ctx context.Context, event Event) error

    // Replay returns events after a given ID
    Replay(ctx context.Context, stream string, afterID ulid.ULID, limit int) ([]Event, error)

    // LastEventID returns the most recent event ID
    LastEventID(ctx context.Context, stream string) (ulid.ULID, error)
}
```

Implementations:

| Implementation       | Use Case             |
| -------------------- | -------------------- |
| `MemoryEventStore`   | Development, testing |
| `PostgresEventStore` | Production           |

### SessionManager

Tracks character presence and connections:

```go
type SessionManager interface {
    Connect(charID, connID ulid.ULID) *Session
    Disconnect(charID, connID ulid.ULID)
    GetSession(charID ulid.ULID) *Session
    UpdateCursor(charID ulid.ULID, stream string, eventID ulid.ULID)
}
```

## Data Flow

### Command Processing

```text
1. Player types: "say Hello!"
2. Telnet handler parses command
3. Engine.HandleSay() creates Event
4. Event stored via EventStore.Append()
5. Broadcaster distributes to subscribers
6. All connected handlers receive and display
```

### Reconnection Flow

```text
1. Player reconnects
2. Session found (or created)
3. Engine.ReplayEvents() fetches missed events
4. Events sent to client
5. Session cursor updated
6. Real-time subscription resumed
```

## Technology Stack

| Component    | Technology          |
| ------------ | ------------------- |
| Language     | Go 1.23+            |
| Storage      | PostgreSQL 16+      |
| WASM Runtime | wazero (pure Go)    |
| Web Client   | SvelteKit (planned) |
| IDs          | ULID (oklog/ulid)   |

## Phase 1 Implementation

Phase 1 delivers a minimal vertical slice:

| Layer    | Implementation                   |
| -------- | -------------------------------- |
| Protocol | Telnet only                      |
| Session  | Single character, event replay   |
| Events   | ULID-based, PostgreSQL or memory |
| World    | Single location                  |
| Plugins  | Proof-of-concept WASM host       |

## Future Phases

| Phase | Features                                 |
| ----- | ---------------------------------------- |
| 2     | Web client, multiple locations, movement |
| 3     | Character creation, player accounts      |
| 4     | Full plugin API, capability model        |
| 5     | ABAC access control, scenes              |

## Further Reading

- [Full Architecture Design](../plans/2026-01-17-holomush-architecture-design.md) - Complete specifications
- [Phase 1 Implementation Plan](../plans/2026-01-17-phase1-implementation.md) - Task breakdown
- [Getting Started](getting-started.md) - Setup and usage guide
