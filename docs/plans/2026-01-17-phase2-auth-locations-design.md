# Phase 2 Design: Authentication & Locations

**Status:** Approved
**Date:** 2026-01-17
**Version:** 1.0.0

## Overview

Phase 2 adds real user accounts and spatial navigation to HoloMUSH, building on the Phase 1 tracer bullet.

### Scope

**In scope:**

- Player accounts with bcrypt passwords
- Admin bootstrap on first server start
- Self-registration (`register username password`)
- Character creation (auto-created, 1 per player for now)
- ABAC foundation (subject attributes, PermissionEvaluator)
- Database-backed locations and exits
- Movement commands (`go <exit>`, cardinal shortcuts)
- Emit command for raw text output
- Updated `look` to show exits and characters present

**Out of scope:**

- Web client (Phase 3+)
- Multiple characters per player (Phase 3+)
- Room creation commands (admin seeds DB directly)
- Full ABAC policies (Phase 4+)

## Success Criteria

1. Fresh server creates admin player with random password logged to console (or uses `ADMIN_PASSWORD` env var)
2. New player can `register`, gets account + character, lands in starting location
3. `connect` validates bcrypt password, rejects invalid credentials
4. Admin player has `core:permissions=["admin"]` attribute, `PermissionEvaluator` interface works
5. Player can `go <exit>` or use cardinal shortcuts, location updates in database
6. Movement emits arrive/leave events to both origin and destination location streams
7. Other players see arrive/leave events in real-time
8. `emit <text>` sends raw text to location stream
9. `look` shows location name, description, visible exits, and characters present
10. After reconnect, missed events (including movement) replay correctly

## Data Model

### Players

```sql
CREATE TABLE players (
    id          TEXT PRIMARY KEY,  -- ULID
    username    TEXT UNIQUE NOT NULL,
    password    TEXT NOT NULL,     -- bcrypt hash
    created_at  TIMESTAMPTZ DEFAULT NOW()
);
```

### Subject Attributes (ABAC Foundation)

```sql
CREATE TABLE subject_attributes (
    subject_type TEXT NOT NULL,   -- 'player', 'system', 'plugin'
    subject_id   TEXT NOT NULL,   -- ULID or identifier
    attribute    TEXT NOT NULL,   -- 'permissions', 'faction', etc.
    value        JSONB NOT NULL,  -- ["admin"] or {"level": 5}
    source       TEXT NOT NULL,   -- 'core' or plugin identifier
    granted_at   TIMESTAMPTZ DEFAULT NOW(),
    granted_by   TEXT,            -- subject_id of granter, NULL for bootstrap
    PRIMARY KEY (subject_type, subject_id, attribute, source)
);
```

The `source` column tracks attribute provenance:

- `core` for system-granted attributes
- Plugin identifier for plugin-contributed attributes

This allows plugins to extend subjects with custom attributes (faction, karma, etc.) while maintaining clear ownership.

### Characters

```sql
CREATE TABLE characters (
    id          TEXT PRIMARY KEY,  -- ULID
    player_id   TEXT NOT NULL REFERENCES players(id),
    name        TEXT UNIQUE NOT NULL,
    location_id TEXT NOT NULL REFERENCES locations(id),
    created_at  TIMESTAMPTZ DEFAULT NOW()
);
```

Player and character namespaces are separate. A player "alice" and character "Alice" are different entities. This enables future support for alts and character transfers between players.

### Locations

```sql
CREATE TABLE locations (
    id          TEXT PRIMARY KEY,  -- ULID
    name        TEXT NOT NULL,
    description TEXT NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);
```

### Exits

```sql
CREATE TABLE exits (
    id              TEXT PRIMARY KEY,  -- ULID
    location_id     TEXT NOT NULL REFERENCES locations(id),
    destination_id  TEXT NOT NULL REFERENCES locations(id),
    name            TEXT NOT NULL,
    aliases         TEXT[] DEFAULT '{}',  -- PostgreSQL array
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(location_id, name)
);
```

Exits have a name and any number of aliases. No name or alias may conflict with another exit's name or alias within the same location. Alias uniqueness enforced at application layer.

### Seed Data

Three locations with connecting exits:

- **Lobby** - Central starting location
- **Garden** - Connected to Lobby (north/south)
- **Library** - Connected to Lobby (east/west)

## Permission System

### PermissionEvaluator Interface

```go
type PermissionEvaluator interface {
    Check(ctx context.Context, subject Subject, action string, resource Resource) (bool, error)
}

type Subject struct {
    Type       string         // "player", "system", "plugin"
    ID         string         // ULID or identifier
    Attributes map[string]any // loaded from subject_attributes
}

type Resource struct {
    Type  string         // "player", "location", "character"
    ID    string
    Attrs map[string]any
}
```

Subjects are not limited to players - they can be the system, plugins, or bots. Attributes describe the subject; policies evaluate attributes.

### Phase 2 Policies

Hardcoded for Phase 2 (file-based policies in Phase 4+):

- Subjects with `permissions` containing `"admin"` can perform admin actions
- All authenticated players can move, speak, look, emit

## Commands

### Pre-login

| Command                          | Description                            |
| -------------------------------- | -------------------------------------- |
| `connect <username> <password>`  | Login with existing account            |
| `register <username> <password>` | Create account + character, then login |

### In-game

| Command         | Description                                                                            |
| --------------- | -------------------------------------------------------------------------------------- |
| `look`          | Show location name, description, exits, characters present                             |
| `say <message>` | Speak - outputs: `Alice says, "message"`                                               |
| `pose <action>` | Emote - outputs: `Alice action`                                                        |
| `emit <text>`   | Raw output - outputs: `text` (no character prefix)                                     |
| `go <exit>`     | Move through named exit                                                                |
| `<direction>`   | Shortcut: `north`, `n`, `south`, `s`, `east`, `e`, `west`, `w`, `up`, `u`, `down`, `d` |
| `quit`          | Disconnect                                                                             |

## Event Types

### New Events

```go
const (
    // Existing (Phase 1)
    EventTypeSay    EventType = "say"
    EventTypePose   EventType = "pose"
    EventTypeSystem EventType = "system"

    // New (Phase 2)
    EventTypeEmit   EventType = "emit"
    EventTypeArrive EventType = "arrive"
    EventTypeLeave  EventType = "leave"
)
```

### Payloads

```go
type EmitPayload struct {
    Text string `json:"text"`
}

type ArrivePayload struct {
    CharacterID   string `json:"character_id"`
    CharacterName string `json:"character_name"`
    FromLocation  string `json:"from_location,omitempty"`  // empty if login
    ExitUsed      string `json:"exit_used,omitempty"`
}

type LeavePayload struct {
    CharacterID   string `json:"character_id"`
    CharacterName string `json:"character_name"`
    ToLocation    string `json:"to_location,omitempty"`    // empty if logout
    ExitUsed      string `json:"exit_used,omitempty"`
}
```

### Rendering

| Event  | Output to others                                                |
| ------ | --------------------------------------------------------------- |
| say    | `Alice says, "Hello!"`                                          |
| pose   | `Alice waves hello.`                                            |
| emit   | `The ground shakes.` (raw text)                                 |
| arrive | `Alice arrives from the garden.` or `Alice has connected.`      |
| leave  | `Alice leaves toward the library.` or `Alice has disconnected.` |

## Flows

### Bootstrap Flow

```text
1. Server starts
2. Check: any players exist in database?
3. If no:
   a. Generate random 16-char password (or use ADMIN_PASSWORD env var)
   b. Create player "admin" with bcrypt-hashed password
   c. Create character "admin" in starting location
   d. Grant subject_attribute: player/admin/permissions = ["admin"] (source: core)
   e. Log password to console: "Admin password: <password>"
4. Continue normal startup
```

### Registration Flow

```text
1. Player types: "register alice secretpass"
2. Validate username:
   - Alphanumeric, 3-20 characters
   - Not already taken
3. Hash password with bcrypt (cost 12)
4. Create player record
5. Create character "alice" in starting location
6. Auto-login (same as connect flow)
7. Show welcome message + location description
```

### Movement Flow

```text
1. Player types: "go garden" or "north"
2. Handler finds exit by name or alias in current location
3. If not found: "You don't see an exit called 'garden'."
4. Engine.HandleMove(ctx, charID, exitID)
5. Create "leave" event in old location stream
6. Update character.location_id in database
7. Create "arrive" event in new location stream
8. Broadcaster delivers events to subscribers in both locations
9. Player sees new location description (auto-look)
```

## Testing Strategy

### Unit Tests

| Component             | Tests                                                              |
| --------------------- | ------------------------------------------------------------------ |
| `PermissionEvaluator` | Check with/without attributes, unknown subject, policy enforcement |
| `PlayerStore`         | Create, authenticate (bcrypt), duplicate username rejection        |
| `CharacterStore`      | Create, lookup by player, update location                          |
| `LocationStore`       | Get by ID, list exits, find exit by name/alias                     |
| `Engine.HandleMove`   | Valid move, invalid exit, leave/arrive events emitted              |
| `Engine.HandleEmit`   | Event stored and broadcast                                         |
| Password utils        | Hash, verify, random generation                                    |

### Integration Tests

| Scenario               | Verification                                                   |
| ---------------------- | -------------------------------------------------------------- |
| Bootstrap              | First start creates admin, second start skips                  |
| Registration           | New player + character created, can login                      |
| Login                  | Valid creds succeed, invalid fail                              |
| Movement               | Character location updates, events broadcast to both locations |
| Real-time arrive/leave | Other players see movement events                              |
| Reconnect after move   | Replays events from new location                               |

### Coverage Target

> 80% on new code (matching Phase 1 standard)

## Migration Notes

### Database Migration

New migration file: `002_phase2_auth_locations.sql`

- Creates `players`, `subject_attributes`, `characters`, `locations`, `exits` tables
- Seeds initial locations and exits
- No data migration needed (Phase 1 used hardcoded auth)

### Breaking Changes

- `connect testuser password` no longer works (hardcoded auth removed)
- Players must `register` or use bootstrap admin account

## Future Considerations

Deferred to later phases:

- **Phase 3:** Web client, multiple characters per player
- **Phase 4:** Full ABAC policies (file-based), builder commands for locations
- **Phase 5:** Scenes, advanced permissions
