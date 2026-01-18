# HoloMUSH Architecture Design

**Status:** Draft
**Date:** 2026-01-17
**Version:** 0.1.0

## Overview

HoloMUSH is a modern MUSH platform combining classic text-based multiplayer gameplay with contemporary technology. It provides simultaneous web and telnet access, a WASM-based plugin system, resumable sessions, and an offline-capable web client.

### Design Goals

- Modern Go core with event-oriented architecture
- Dual protocol support (web and telnet)
- Language-agnostic plugin system via WASM
- Tmux-style session persistence and reconnection
- Offline-capable PWA with sync
- Platform-first approach supporting RP and sandbox gameplay

## Development Principles

### Process Requirements

| Principle     | Requirement                                                                                              |
| ------------- | -------------------------------------------------------------------------------------------------------- |
| Test-Driven   | Tests MUST be written first and MUST pass before any task is complete                                    |
| Spec-Driven   | Work MUST NOT start without a spec/design/plan                                                           |
| Documentation | Implementation MUST NOT be considered complete until documentation reflects both spec AND implementation |
| Language      | All directives, plans, and designs MUST use RFC2119 keywords                                             |

### Workflow

```text
Spec/Design (RFC2119) → Tests (failing) → Implementation → Tests (passing) → Documentation → Done
```

### Documentation Structure

- `docs/specs/` - Specifications (what MUST be built)
- `docs/plans/` - Implementation plans (how to build it)
- `docs/reference/` - API/user documentation (what was built)

## Architecture Overview

```text
┌─────────────────────────────────────────────────────────────────┐
│                         Clients                                 │
├──────────────────────┬──────────────────────────────────────────┤
│   Telnet Adapter     │           Web (SvelteKit PWA)            │
│   (ANSI, GMCP)       │   Terminal │ Wiki │ Forum │ Chars │ Admin│
└──────────┬───────────┴──────────────────┬───────────────────────┘
           │                              │
           ▼                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Go Core (Event-Oriented)                     │
├─────────────────────────────────────────────────────────────────┤
│  Session Manager   │  World Engine   │  Plugin Host (Extism)   │
│  - Per-char buffer │  - Locations    │  - WASM sandbox         │
│  - Reconnect/sync  │  - Objects      │  - Capability grants    │
│  - Protocol adapt  │  - Commands     │  - Multi-language       │
└─────────┬───────────────────┬───────────────────┬───────────────┘
          │                   │                   │
          ▼                   ▼                   ▼
┌──────────────────┐ ┌─────────────────┐ ┌────────────────────────┐
│  Event Store     │ │   PostgreSQL    │ │  ABAC Policy Engine    │
│  (PG/NATS)       │ │  World + Content│ │  Attribute-based ACL   │
└──────────────────┘ └─────────────────┘ └────────────────────────┘
```

### Key Principles

- Everything flows through events - commands in, events out
- Protocol adapters translate telnet/websocket to common event format
- WASM plugins are sandboxed, granted explicit capabilities
- Session state persisted per-character for reconnection
- ABAC governs all access decisions

## Event System

### Event Model

All game activity flows through ordered, persistent event streams. This enables:

- Reconnection with full catch-up (tmux-style)
- Offline web client sync
- Plugin reactions to game events
- Audit/debug logging

### Event Structure

```go
type EventType string // Defined type, not raw string

const (
    EventTypeSay     EventType = "say"
    EventTypePose    EventType = "pose"
    EventTypeArrive  EventType = "arrive"
    EventTypeLeave   EventType = "leave"
    EventTypePage    EventType = "page"
    EventTypeSystem  EventType = "system"
    // Extensible via plugins
)

type ActorKind uint8

const (
    ActorCharacter ActorKind = iota
    ActorSystem
    ActorPlugin
)

type Actor struct {
    Kind     ActorKind
    ID       string    // CharID, plugin name, or "system"
}

type Event struct {
    ID        ulid.ULID // Coordination-free, sortable, globally unique
    Stream    string    // e.g., "location:42", "channel:ooc", "char:7"
    Type      EventType
    Timestamp time.Time
    Actor     Actor
    Payload   []byte    // JSON event data
}
```

### Stream Types

| Stream           | Content                                               | Subscribers            |
| ---------------- | ----------------------------------------------------- | ---------------------- |
| `location:<id>`  | Location activity (poses, says, arrivals, departures) | Characters in location |
| `channel:<name>` | Channel messages                                      | Channel members        |
| `char:<id>`      | Private messages, system notifications                | That character         |
| `world`          | Global events (connects, disconnects, broadcasts)     | Admins, plugins        |

### Delivery Guarantees

- Events MUST be persisted before acknowledgment
- Events MUST be delivered in order within a stream
- Clients MUST receive missed events on reconnect (bounded by configurable history)
- Plugins MAY subscribe to any stream they have capability for

### Event Store Interface

```go
type EventStore interface {
    // Append persists an event to a stream
    Append(ctx context.Context, event Event) error

    // Subscribe returns events from a stream starting after the given ID
    // If afterID is zero ULID, starts from beginning
    Subscribe(ctx context.Context, stream string, afterID ulid.ULID) (<-chan Event, error)

    // Replay returns up to limit events from a stream, starting after afterID
    Replay(ctx context.Context, stream string, afterID ulid.ULID, limit int) ([]Event, error)

    // LastEventID returns the most recent event ID for a stream
    LastEventID(ctx context.Context, stream string) (ulid.ULID, error)
}
```

#### Implementations

| Implementation       | Phase   | Use Case               |
| -------------------- | ------- | ---------------------- |
| `PostgresEventStore` | 1       | MVP, single-node       |
| `NATSEventStore`     | Future  | Scalable, multi-node   |
| `MemoryEventStore`   | Testing | Unit/integration tests |

## Session & Reconnection

### Session Model

The session IS the character's ongoing presence. Connections are views into it.

```go
type CharacterSession struct {
    CharacterID   ulid.ULID
    Subscriptions []string              // Streams this character sees
    EventCursor   map[string]ulid.ULID  // Last event per stream
    Connections   []ConnectionID        // Zero or more active connections
}

type Connection struct {
    ID          ulid.ULID
    SessionID   ulid.ULID       // -> CharacterSession
    PlayerID    ulid.ULID       // Who authenticated
    Protocol    ProtocolType
    ConnectedAt time.Time
}
```

### Connection Flow

1. Player authenticates, selects character
2. Server finds (or creates) CharacterSession for that character
3. New Connection attached to that session
4. Server sends last N events (configurable, e.g., viewport-sized buffer)
5. Multiple connections to same session allowed - all see same stream

### Requirements

- Server MUST retain all events for a character's session for a configurable duration (MAY be infinite)
- Server MUST allow multiple simultaneous connections to same session
- Server MUST broadcast new events to all connections on a session
- On connect, server MUST send last N events (regardless of "seen" status)

## Plugin System (WASM)

### Runtime

- Host: Extism SDK (wraps wazero, pure Go, no CGO)
- Plugins: Any language with Extism PDK (Rust, Go, Python, JavaScript, etc.)
- Interface: Extism plugin calling convention + custom host functions for game API

### Plugin Manifest

```yaml
name: combat-system
version: 1.0.0
language: rust
entry: combat.wasm
capabilities:
  required:
    - world.read
    - world.write.objects
    - events.subscribe.location
  optional:
    - net.http # For external dice API
```

### Capability Model

| Capability                  | Description                                                |
| --------------------------- | ---------------------------------------------------------- |
| `world.read`                | Read game state (locations, objects, characters)           |
| `world.write.<scope>`       | Modify game state (scoped: objects, locations, characters) |
| `events.subscribe.<stream>` | Subscribe to event streams                                 |
| `events.emit.<stream>`      | Emit events to streams                                     |
| `net.http`                  | Outbound HTTP requests                                     |
| `fs.data`                   | Read/write plugin's own data directory                     |
| `char.notify`               | Send messages to characters                                |

### Security Profiles

| Profile     | Grants                                | Use Case                 |
| ----------- | ------------------------------------- | ------------------------ |
| `untrusted` | world.read, events.subscribe.location | User-submitted plugins   |
| `trusted`   | All read + scoped write + char.notify | Vetted community plugins |
| `admin`     | All capabilities                      | Core game systems        |

### Override Example

```yaml
plugins:
  combat-system:
    profile: trusted
    grant:
      - net.http # Allow external dice roller
    revoke:
      - world.write.characters # But not char modification
```

## Access Control (ABAC)

Attribute-Based Access Control applies across the system - plugins, players, characters, objects.

### ABAC Components

| Component   | Description        | Example Attributes                                           |
| ----------- | ------------------ | ------------------------------------------------------------ |
| Subject     | Who is acting      | player.role, character.faction, plugin.name, character.level |
| Resource    | What is accessed   | location.zone, object.owner, channel.type, scene.status      |
| Action      | What they're doing | read, write, emit, enter, modify, delete                     |
| Environment | Context            | time, character.scene, connection.protocol                   |

### Policy Structure

```yaml
policies:
  - name: faction-headquarters-access
    effect: allow
    subjects:
      character.faction: "{{resource.location.faction}}"
    resources:
      type: location
      location.restricted: true
    actions: [enter, look]

  - name: builder-modify-objects
    effect: allow
    subjects:
      player.roles: [builder, admin]
    resources:
      type: object
    actions: [create, modify, delete]

  - name: plugin-world-read
    effect: allow
    subjects:
      type: plugin
      plugin.capabilities: [world.read]
    resources:
      type: [location, object, character]
    actions: [read]
```

### Evaluation

1. Collect subject, resource, action, environment attributes
2. Find matching policies
3. If any `deny` matches → deny
4. If any `allow` matches → allow
5. Default → deny

### Requirements

- All access checks MUST go through ABAC evaluator
- Policies MUST be stored in database, editable at runtime
- Policy changes MUST be auditable
- Plugins MUST only receive attributes they have capability to see
- ABAC evaluator MUST be fast (cache compiled policies)

### Future Consideration

- Evaluate [OpenFGA](https://openfga.dev/) for ABAC implementation
- OpenFGA provides Zanzibar-style authorization with relationship-based access control (ReBAC)
- May simplify policy management, querying, and auditing

## Web Client & Portal

### Tech Stack

- Framework: SvelteKit
- Deployment: PWA (Service Worker, installable)
- Transport: WebSocket for game events, REST/fetch for portal content
- Offline: IndexedDB for command queue + event cache

### Portal Components

| Component  | Description                                           |
| ---------- | ----------------------------------------------------- |
| Terminal   | Game interface - input, scrollback, ANSI rendering    |
| Wiki       | Game documentation, lore, help files                  |
| Forum      | Player discussions, scene requests, announcements     |
| Characters | Public profiles, character sheets (game-configurable) |
| Scenes     | Scene logs, ongoing scene listings                    |
| Admin      | Game configuration, player management, logs           |

### Offline Behavior

| State     | Behavior                                               |
| --------- | ------------------------------------------------------ |
| Online    | WebSocket connected, real-time events                  |
| Offline   | Commands queued in IndexedDB, cached content available |
| Reconnect | Queued commands sent, server sends catch-up events     |

### PWA Requirements

- App shell MUST be cached for instant load
- Service Worker MUST intercept fetch for offline fallback
- IndexedDB MUST persist command queue across browser restart
- Client MUST track connection state and display to user
- Client SHOULD indicate queued command count when offline

### Terminal Requirements

- MUST render ANSI color/formatting
- MUST support scrollback buffer (configurable size)
- MUST handle MXP or GMCP if server sends it (progressive enhancement)
- SHOULD support keyboard shortcuts (command history, etc.)

## Transport Security

### TLS Requirements

| Protocol  | TLS Requirement                            |
| --------- | ------------------------------------------ |
| WebSocket | MUST use TLS 1.3+                          |
| REST/HTTP | MUST use TLS 1.3+                          |
| Telnet    | MAY use cleartext; SHOULD offer TLS option |

### Message Integrity

HMAC signatures apply to web/mobile clients (telnet is line-based, cannot support client-side crypto).

```go
type SignedMessage struct {
    Payload   []byte    // The actual message/event
    Timestamp int64     // Unix timestamp (replay protection)
    Nonce     [16]byte  // Unique per message
    Signature [32]byte  // HMAC-SHA256(key, payload || timestamp || nonce)
}
```

### Message Integrity Scope

| Path                           | Integrity                                      |
| ------------------------------ | ---------------------------------------------- |
| Web Client ↔ Server            | HMAC-signed messages over TLS                  |
| Telnet Client ↔ Telnet Adapter | Line-based text, no signing (legacy protocol)  |
| Telnet Adapter ↔ Core          | Internal - adapter is trusted server component |

### Requirements

- Web/mobile clients MUST sign all messages
- Server MUST sign all messages to web/mobile clients
- Telnet adapter MUST translate line-based I/O to internal event format
- Telnet connections are inherently less secure - this is acceptable for legacy support

## Data Model (PostgreSQL)

### Core Entities

```text
┌─────────────┐       ┌─────────────┐       ┌─────────────┐
│   Player    │──1:N──│  Character  │──N:1──│  Location   │
│  (account)  │       │             │       │             │
└─────────────┘       └─────────────┘       └─────────────┘
                             │
                             N
                             │
                      ┌──────┴──────┐
                      │   Object    │
                      │ (inventory) │
                      └─────────────┘
```

### Tables

| Table                   | Purpose                                  |
| ----------------------- | ---------------------------------------- |
| `players`               | Accounts, auth credentials, OAuth links  |
| `characters`            | In-game characters, owned by players     |
| `locations`             | Rooms/places in the game world           |
| `exits`                 | Connections between locations            |
| `objects`               | Items, props, anything carriable         |
| `channels`              | Communication channels and membership    |
| `wiki_pages`            | Wiki/documentation content               |
| `forum_posts`           | Forum threads and replies                |
| `scenes`                | Scene metadata                           |
| `scene_participants`    | Character participation in scenes        |
| `sessions`              | Character session state                  |
| `plugins`               | Installed plugins and configuration      |
| `plugin_schema_objects` | Tracks plugin-created schema for cleanup |
| `events`                | Event store (Phase 1)                    |
| `policies`              | ABAC policies                            |

### Extensibility

- Tables SHOULD use JSONB columns for game-specific attributes (character sheets, custom fields)
- Plugins MAY define their own tables via migrations
- Schema changes MUST be versioned and reversible

### Requirements

- All entities MUST have ULID primary keys
- All entities MUST have `created_at`, `updated_at` timestamps
- Soft deletes SHOULD be used where audit trail matters
- Foreign keys MUST be enforced

### Plugin Schema Tracking

```sql
CREATE TABLE plugin_schema_objects (
    id          ULID PRIMARY KEY,
    plugin_name VARCHAR(255) NOT NULL,
    object_type VARCHAR(50) NOT NULL,  -- 'table', 'column', 'index', 'function'
    object_name VARCHAR(255) NOT NULL,
    migration   VARCHAR(255) NOT NULL,  -- Which migration created this
    created_at  TIMESTAMPTZ NOT NULL
);
```

- All plugin-created schema objects MUST be registered
- Plugin removal MUST drop all registered schema objects
- Plugin upgrades MUST track schema changes per version

### Events Table (Phase 1)

```sql
CREATE TABLE events (
    id          ULID PRIMARY KEY,
    stream      VARCHAR(255) NOT NULL,
    type        VARCHAR(100) NOT NULL,
    actor_kind  SMALLINT NOT NULL,
    actor_id    VARCHAR(255) NOT NULL,
    payload     JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_events_stream_id ON events (stream, id);
```

## Scenes

Scenes are optional narrative instances that phase characters out of real-time.

### Location Visibility Model

```text
Location: "The Tavern"
├── Real-time layer (no scene)
│   └── Characters here see each other, new arrivals land here
│
├── Scene #42: "Secret Meeting"
│   └── Characters here are isolated, invisible to real-time
│
└── Scene #87: "Bar Fight Flashback"
    └── Different isolated instance, same physical location
```

### Visibility Rules

| Viewer                  | Sees                            |
| ----------------------- | ------------------------------- |
| Character in real-time  | Other real-time characters only |
| Character in Scene X    | Other Scene X participants only |
| New arrival to location | Real-time layer only            |

### Requirements

- Scenes MUST be optional - normal play happens in real-time layer
- Characters in a scene MUST NOT be visible to real-time layer
- Characters in a scene MUST NOT see real-time layer
- Different scenes in same location MUST be isolated from each other
- Character MAY exit scene to return to real-time layer
- Events MUST route to correct layer (real-time or scene)

### Schema

```sql
CREATE TABLE scenes (
    id          ULID PRIMARY KEY,
    title       VARCHAR(255),
    status      VARCHAR(50),  -- 'active', 'paused', 'closed'
    created_at  TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ
);

CREATE TABLE scene_participants (
    scene_id      ULID REFERENCES scenes(id),
    character_id  ULID REFERENCES characters(id),
    joined_at     TIMESTAMPTZ,
    status        VARCHAR(50),  -- 'active', 'invited', 'left'
    PRIMARY KEY (scene_id, character_id)
);
```

## Identity Model

### Player/Character Separation

- Player (account) = real person, handles auth, preferences
- Character(s) = in-game entities the player controls
- One player → many characters

### Authentication

- Built-in accounts (username/password)
- Optional OAuth/OIDC linking (Discord, Google, etc.)
- Staff can see alt relationships

## Phase 1: MVP Scope (Tracer Bullet)

Thin vertical slice through the system, telnet first.

### Scope

| Layer    | MVP Implementation                                        |
| -------- | --------------------------------------------------------- |
| Protocol | Telnet adapter (cleartext, line-based)                    |
| Session  | Single character, reconnection with event replay          |
| Events   | ULID-based, persisted to PostgreSQL                       |
| World    | Single location, basic commands                           |
| Storage  | PostgreSQL for everything                                 |
| Plugins  | One "hello world" WASM plugin (proves wazero integration) |

### MVP Commands

| Command                     | Purpose                       |
| --------------------------- | ----------------------------- |
| `connect <name> <password>` | Authenticate, resume session  |
| `look`                      | Describe current location     |
| `say <message>`             | Emit say event to location    |
| `pose <action>`             | Emit pose event to location   |
| `quit`                      | Disconnect (session persists) |

### Success Criteria

1. Connect via telnet, authenticate
2. See events from other connected characters in same location
3. Disconnect and reconnect - see missed events replayed
4. WASM plugin receives events, can emit responses
5. All events persisted and queryable

### Not in Phase 1

- Web client, PWA, offline
- Multiple locations, exits, movement
- Character creation, player accounts (hardcoded test data)
- Full capability/ABAC model (simplified access)
- TLS, HMAC signing
- Wiki, forum, character sheets
- Scenes

## Future Considerations

The following items are noted for future design work:

### End-to-End Encryption

- E2E encryption for private communications (pages/DMs)
- E2E encryption for scene content
- Key exchange mechanism TBD
- Consider Signal Protocol or similar

### Discord Bot Integration

- Plugin-based Discord bot for game interaction
- Bridge channels to in-game channels
- Character status/notifications to Discord
- OAuth linking for Discord accounts

### Native Mobile Apps

- iOS app (Swift/SwiftUI)
- Android app (Kotlin/Compose) - lower priority
- Consider shared core via Kotlin Multiplatform or similar
- PWA may suffice for initial mobile needs

### World Building Tools

- Visual grid/map editor
- Export/import via Mermaid or PlantUML diagrams
- Location relationship visualization
- Batch creation from diagram definitions

---

## Appendix: RFC2119 Keywords

Per [RFC2119](https://www.ietf.org/rfc/rfc2119.txt):

| Keyword        | Meaning                                    |
| -------------- | ------------------------------------------ |
| **MUST**       | Absolute requirement                       |
| **MUST NOT**   | Absolute prohibition                       |
| **SHOULD**     | Recommended, may ignore with justification |
| **SHOULD NOT** | Not recommended, may do with justification |
| **MAY**        | Optional                                   |
