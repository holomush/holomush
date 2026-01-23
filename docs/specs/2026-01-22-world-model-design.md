# World Model Architecture Design

**Status:** Implemented
**Date:** 2026-01-22
**Epic:** holomush-x2t (Epic 4: World Model)
**Task:** holomush-x2t.1

## Overview

This document defines the world model architecture for HoloMUSH. The design provides
a hybrid world model supporting persistent locations, temporary scene rooms, and
flexible object containment.

### Goals

- Hybrid location model: persistent world + scene/temporary rooms
- Graph-compatible schema using recursive CTEs (Apache AGE-compatible for future)
- Configurable bidirectional exits with visibility and lock support
- Flexible object containment (room, character inventory, nested containers)
- Scene rooms that can shadow/clone persistent locations
- Event-driven world state changes with unified movement events
- ABAC integration for world permissions

### Non-Goals

- Full graph database (Apache AGE) in Phase 1 (schema designed for future adoption)
- Procedural world generation (deferred)
- Player housing system (deferred)
- Complex scripting/trigger system (basic hooks only)

## Architecture

### Storage Approach

The world model uses standard PostgreSQL tables with recursive CTEs for graph
traversal. The schema is designed to be Apache AGE-compatible for future migration
if complex graph queries become necessary.

**Rationale:** Benchmarks show recursive CTEs are ~40x faster than Apache AGE for
bounded-depth traversals (1-3 hops), which covers most MUSH operations. AGE excels
at variable-depth pattern matching, which can be added later without schema changes.

### Package Structure

```text
internal/world/
  location.go        # Location types and domain logic
  exit.go            # Exit types and bidirectional handling
  object.go          # Object types and containment
  scene.go           # Scene-specific logic
  repository.go      # Repository interfaces

internal/world/postgres/
  location_repo.go   # PostgreSQL LocationRepository
  exit_repo.go       # PostgreSQL ExitRepository
  object_repo.go     # PostgreSQL ObjectRepository
  scene_repo.go      # PostgreSQL scene queries

internal/world/worldtest/
  mocks.go           # Test helpers and mocks
```

## Data Model

### Locations

Locations store rooms with type-based lifecycle:

```sql
CREATE TABLE locations (
    id TEXT PRIMARY KEY,              -- ULID
    type TEXT NOT NULL DEFAULT 'persistent',  -- 'persistent' | 'scene' | 'instance'
    shadows_id TEXT REFERENCES locations(id), -- For scenes cloning a real room
    name TEXT NOT NULL,
    description TEXT NOT NULL,
    owner_id TEXT REFERENCES characters(id),  -- Builder who created it
    replay_policy TEXT NOT NULL DEFAULT 'last:0',  -- 'last:0' | 'last:10' | 'last:-1'
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at TIMESTAMPTZ           -- For scene cleanup
);
CREATE INDEX idx_locations_type ON locations(type);
CREATE INDEX idx_locations_shadows ON locations(shadows_id) WHERE shadows_id IS NOT NULL;
```

**Location types:**

| Type         | Description                | Default Replay   |
| ------------ | -------------------------- | ---------------- |
| `persistent` | Permanent world rooms      | `last:0` (none)  |
| `scene`      | Temporary RP scenes        | `last:-1` (full) |
| `instance`   | Instanced content (future) | `last:0` (none)  |

**Scene shadowing:** Scenes can reference a persistent location via `shadows_id` to
inherit its name and description. The scene has its own event stream and can override
properties as needed.

### Exits

Exits connect locations with configurable bidirectionality:

```sql
CREATE TABLE exits (
    id TEXT PRIMARY KEY,
    from_location_id TEXT NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
    to_location_id TEXT NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
    name TEXT NOT NULL,               -- "north", "door", "portal"
    aliases TEXT[] DEFAULT '{}',      -- ["n", "doorway"]
    bidirectional BOOLEAN NOT NULL DEFAULT TRUE,
    return_name TEXT,                 -- "south" for the auto-created return
    -- Visibility
    visibility TEXT NOT NULL DEFAULT 'all',  -- 'all' | 'owner' | 'list'
    visible_to TEXT[] DEFAULT '{}',   -- Character IDs when visibility='list'
    -- Locking
    locked BOOLEAN NOT NULL DEFAULT FALSE,
    lock_type TEXT,                   -- 'key' | 'password' | 'condition'
    lock_data JSONB,                  -- {key_object_id: "..."} or {password_hash: "..."}
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(from_location_id, name)
);
CREATE INDEX idx_exits_from ON exits(from_location_id);
CREATE INDEX idx_exits_to ON exits(to_location_id);
```

**Bidirectional handling:** When `bidirectional = true`, the application layer
creates and manages the return exit automatically. This is done in Go, not via
database triggers, to keep logic explicit and testable.

### Objects

Objects use flexible containment with mutual exclusion enforced at application layer:

```sql
CREATE TABLE objects (
    id TEXT PRIMARY KEY,              -- ULID
    name TEXT NOT NULL,
    description TEXT NOT NULL,
    -- Containment (exactly one must be set)
    location_id TEXT REFERENCES locations(id) ON DELETE SET NULL,
    held_by_character_id TEXT REFERENCES characters(id) ON DELETE SET NULL,
    contained_in_object_id TEXT REFERENCES objects(id) ON DELETE SET NULL,
    -- Properties
    is_container BOOLEAN NOT NULL DEFAULT FALSE,
    owner_id TEXT REFERENCES characters(id),  -- Who created/owns it
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_objects_location ON objects(location_id) WHERE location_id IS NOT NULL;
CREATE INDEX idx_objects_held_by ON objects(held_by_character_id) WHERE held_by_character_id IS NOT NULL;
CREATE INDEX idx_objects_contained ON objects(contained_in_object_id) WHERE contained_in_object_id IS NOT NULL;

ALTER TABLE objects ADD CONSTRAINT chk_not_self_contained
    CHECK (contained_in_object_id IS NULL OR contained_in_object_id != id);
```

**Containment rules** (enforced in Go):

- Object MUST be in exactly one place (room, character inventory, or container)
- Only `is_container = true` objects MAY hold other objects
- Max nesting depth is configurable (default 3)

### Scene Participants

Scene membership for access control:

```sql
CREATE TABLE scene_participants (
    scene_id TEXT NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
    character_id TEXT NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'member',  -- 'owner' | 'member' | 'invited'
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (scene_id, character_id)
);
CREATE INDEX idx_scene_participants_character ON scene_participants(character_id);
```

### Character Location

Extending the existing characters table:

```sql
ALTER TABLE characters ADD COLUMN IF NOT EXISTS location_id TEXT REFERENCES locations(id);
CREATE INDEX idx_characters_location ON characters(location_id);
```

## Event Types

### Movement Event (Unified)

A single `move` event type handles all entity movement:

```go
EventTypeMove EventType = "move"
```

**Payload:**

```go
type MovePayload struct {
    EntityType string `json:"entity_type"`  // "character" | "object"
    EntityID   string `json:"entity_id"`
    FromType   string `json:"from_type"`    // "location" | "character" | "object"
    FromID     string `json:"from_id"`
    ToType     string `json:"to_type"`
    ToID       string `json:"to_id"`
    ExitID     string `json:"exit_id,omitempty"`     // Only for character via exit
    ExitName   string `json:"exit_name,omitempty"`
}
```

### Object Events

```go
EventTypeObjectCreate  EventType = "object_create"
EventTypeObjectDestroy EventType = "object_destroy"
EventTypeObjectUse     EventType = "object_use"
EventTypeObjectExamine EventType = "object_examine"
EventTypeObjectGive    EventType = "object_give"
```

**Object give payload:**

```go
type ObjectGivePayload struct {
    ObjectID        string `json:"object_id"`
    ObjectName      string `json:"object_name"`
    FromCharacterID string `json:"from_character_id"`
    ToCharacterID   string `json:"to_character_id"`
}
```

### Event Routing

| Event Type         | Emitted To                                   | Who Receives                   |
| ------------------ | -------------------------------------------- | ------------------------------ |
| `say`, `pose`      | `location:<current>`                         | All characters at location     |
| `move` (departure) | `location:<from>`                            | Characters in origin room      |
| `move` (arrival)   | `location:<to>`                              | Characters in destination room |
| `object_give`      | `location:<where>` + `character:<recipient>` | Room + direct to recipient     |
| `object_use`       | `location:<where>`                           | Characters at location         |

## Repository Interfaces

```go
// LocationRepository manages location persistence.
type LocationRepository interface {
    Get(ctx context.Context, id ulid.ULID) (*Location, error)
    Create(ctx context.Context, loc *Location) error
    Update(ctx context.Context, loc *Location) error
    Delete(ctx context.Context, id ulid.ULID) error
    ListByType(ctx context.Context, locType LocationType) ([]*Location, error)
    GetShadowedBy(ctx context.Context, id ulid.ULID) ([]*Location, error)
}

// ExitRepository manages exits between locations.
type ExitRepository interface {
    Get(ctx context.Context, id ulid.ULID) (*Exit, error)
    Create(ctx context.Context, exit *Exit) error
    Update(ctx context.Context, exit *Exit) error
    Delete(ctx context.Context, id ulid.ULID) error
    ListFromLocation(ctx context.Context, locationID ulid.ULID) ([]*Exit, error)
    FindByName(ctx context.Context, locationID ulid.ULID, name string) (*Exit, error)
}

// ObjectRepository manages objects and containment.
type ObjectRepository interface {
    Get(ctx context.Context, id ulid.ULID) (*Object, error)
    Create(ctx context.Context, obj *Object) error
    Update(ctx context.Context, obj *Object) error
    Delete(ctx context.Context, id ulid.ULID) error
    ListAtLocation(ctx context.Context, locationID ulid.ULID) ([]*Object, error)
    ListHeldBy(ctx context.Context, characterID ulid.ULID) ([]*Object, error)
    ListContainedIn(ctx context.Context, objectID ulid.ULID) ([]*Object, error)
    Move(ctx context.Context, objectID ulid.ULID, to Containment) error
}

// Containment represents where an object is.
type Containment struct {
    LocationID  *ulid.ULID
    CharacterID *ulid.ULID
    ObjectID    *ulid.ULID  // Container
}
```

## ABAC Policies

### Resource Patterns

```yaml
# Locations
location:<id>              # Specific room
location:*                 # All rooms
location:type:scene        # All scenes

# Exits
exit:<id>                  # Specific exit
exit:from:<location_id>    # All exits from a location

# Objects
object:<id>                # Specific object
object:at:<location_id>    # All objects in a room
object:held:<char_id>      # All objects held by character
```

### Permission Groups

```yaml
player-powers:
  # Location access
  - read:location:$here
  - read:exit:from:$here
  - read:object:at:$here
  - read:object:held:$self
  - write:object:held:$self
  - emit:stream:location:$here

  # Commands
  - execute:command:go
  - execute:command:get
  - execute:command:drop
  - execute:command:give

builder-powers:
  - write:location:*
  - write:exit:*
  - delete:exit:*
  - write:object:*
  - delete:object:*
  - execute:command:@dig
  - execute:command:@link
  - execute:command:@create

scene-member-powers:
  - read:location:$scene_member
  - read:character:at:$scene_member
  - emit:stream:location:$scene_member

scene-owner-powers:
  - write:location:$owned
  - delete:location:$owned
  - write:exit:from:$owned
```

### Dynamic Tokens

| Token           | Resolves To                           |
| --------------- | ------------------------------------- |
| `$self`         | Subject's character ID                |
| `$here`         | Subject's current location ID         |
| `$owned`        | Locations/objects owned by subject    |
| `$scene_member` | Scenes where subject is a participant |

## Location Streaming

### Stream Naming

```text
location:<id>           # Events in a persistent room
location:scene:<id>     # Events in a scene room (isolated)
character:<id>          # Private events to a character
system                  # Global system events
```

### Replay Policy

Unified `last:N` format:

| Policy    | Meaning                  |
| --------- | ------------------------ |
| `last:0`  | No replay                |
| `last:10` | Last 10 events           |
| `last:50` | Last 50 events           |
| `last:-1` | Full history (unlimited) |

**Defaults by location type:**

- Persistent rooms: `last:0` (no replay unless admin)
- Scene rooms: `last:-1` (full history for catching up on RP)

```go
func ParseReplayPolicy(policy string) int {
    if !strings.HasPrefix(policy, "last:") {
        return 0
    }
    n, _ := strconv.Atoi(strings.TrimPrefix(policy, "last:"))
    return n
}

func DefaultReplayPolicy(locType LocationType) string {
    switch locType {
    case LocationTypeScene:
        return "last:-1"
    default:
        return "last:0"
    }
}
```

### Scene Isolation

Scenes have completely separate event streams from their shadowed location. A scene
shadowing "The Tavern" (`location:01TAVERN`) has stream `location:scene:01ABC`.
Characters in the scene don't see main tavern events and vice versa.

## Plugin Integration

### Event Subscriptions

Plugins declare subscriptions in their manifest:

```yaml
subscriptions:
  - stream: "location:*"
    events: ["move", "say", "pose"]
  - stream: "object:*"
    events: ["object_create", "object_use"]
```

### Pre-Action Hooks

| Hook                 | Trigger                       | Can Block | Use Case                   |
| -------------------- | ----------------------------- | --------- | -------------------------- |
| `before_move`        | Character about to move       | Yes       | Locked doors, traps        |
| `before_object_get`  | Character about to pick up    | Yes       | Weight limit, cursed items |
| `before_object_use`  | Character about to use object | Yes       | Cooldowns, requirements    |
| `before_exit_create` | Builder creating exit         | Yes       | Zone restrictions          |

### Post-Action Hooks

| Hook                 | Trigger         | Use Case                          |
| -------------------- | --------------- | --------------------------------- |
| `after_move`         | Character moved | Room enter scripts, NPC reactions |
| `after_object_drop`  | Object dropped  | Trigger puzzles, alerts           |
| `after_scene_create` | Scene created   | Auto-invite, logging              |

### Host Functions

```go
// World queries
GetLocation(id string) (*Location, error)
GetExitsFrom(locationID string) ([]*Exit, error)
GetCharactersAt(locationID string) ([]*Character, error)
GetObjectsAt(locationID string) ([]*Object, error)

// World mutations (permission-checked)
MoveCharacter(charID, exitID string) error
MoveObject(objectID string, to Containment) error
CreateObject(name, description string, at Containment) (*Object, error)
```

## Acceptance Criteria

- [x] Document location, exit, and object schemas
- [x] Define repository interfaces for each entity type
- [x] Specify event types for world state changes
- [x] Design ABAC policies for world objects
- [x] Document location streaming strategy
- [x] Define plugin hook points for world events

## References

- [Phase 1 Tracer Bullet](2026-01-17-phase1-tracer-bullet.md)
- [Access Control Design](2026-01-21-access-control-design.md)
- [Plugin System Design](2026-01-18-plugin-system-design.md)
- [Apache AGE](https://age.apache.org/) - Future graph extension option
