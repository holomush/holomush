# Epic 4: World Model Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement the world model with locations, exits, objects, and scene support.

**Architecture:** Hybrid location model (persistent + scenes) with per-entity repositories, recursive CTE graph traversal, and event-driven state changes. Objects use flexible containment (room/character/container).

**Tech Stack:** Go 1.23+, PostgreSQL, pgx/v5, testify, oops errors

---

## Phase 1: Database Schema

### Task 1.1: Create World Model Migration

**Files:**

- Create: `internal/store/migrations/003_world_model.sql`
- Modify: `internal/store/postgres.go:24` (add migration embed)

**Step 1: Write the migration SQL**

Create `internal/store/migrations/003_world_model.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Extend locations table with world model fields
ALTER TABLE locations ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT 'persistent';
ALTER TABLE locations ADD COLUMN IF NOT EXISTS shadows_id TEXT REFERENCES locations(id);
ALTER TABLE locations ADD COLUMN IF NOT EXISTS owner_id TEXT REFERENCES characters(id);
ALTER TABLE locations ADD COLUMN IF NOT EXISTS replay_policy TEXT NOT NULL DEFAULT 'last:0';
ALTER TABLE locations ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_locations_type ON locations(type);
CREATE INDEX IF NOT EXISTS idx_locations_shadows ON locations(shadows_id) WHERE shadows_id IS NOT NULL;

-- Exits table
CREATE TABLE IF NOT EXISTS exits (
    id TEXT PRIMARY KEY,
    from_location_id TEXT NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
    to_location_id TEXT NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    aliases TEXT[] DEFAULT '{}',
    bidirectional BOOLEAN NOT NULL DEFAULT TRUE,
    return_name TEXT,
    visibility TEXT NOT NULL DEFAULT 'all',
    visible_to TEXT[] DEFAULT '{}',
    locked BOOLEAN NOT NULL DEFAULT FALSE,
    lock_type TEXT,
    lock_data JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(from_location_id, name)
);
CREATE INDEX IF NOT EXISTS idx_exits_from ON exits(from_location_id);
CREATE INDEX IF NOT EXISTS idx_exits_to ON exits(to_location_id);

-- Objects table
CREATE TABLE IF NOT EXISTS objects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL,
    location_id TEXT REFERENCES locations(id) ON DELETE SET NULL,
    held_by_character_id TEXT REFERENCES characters(id) ON DELETE SET NULL,
    contained_in_object_id TEXT REFERENCES objects(id) ON DELETE SET NULL,
    is_container BOOLEAN NOT NULL DEFAULT FALSE,
    owner_id TEXT REFERENCES characters(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_not_self_contained CHECK (contained_in_object_id IS NULL OR contained_in_object_id != id)
);
CREATE INDEX IF NOT EXISTS idx_objects_location ON objects(location_id) WHERE location_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_objects_held_by ON objects(held_by_character_id) WHERE held_by_character_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_objects_contained ON objects(contained_in_object_id) WHERE contained_in_object_id IS NOT NULL;

-- Scene participants
CREATE TABLE IF NOT EXISTS scene_participants (
    scene_id TEXT NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
    character_id TEXT NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'member',
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (scene_id, character_id)
);
CREATE INDEX IF NOT EXISTS idx_scene_participants_character ON scene_participants(character_id);
```

**Step 2: Add migration embed to postgres.go**

In `internal/store/postgres.go`, after line 24 (`var migration002SQL string`), add:

```go
//go:embed migrations/003_world_model.sql
var migration003SQL string
```

**Step 3: Update Migrate function**

In `internal/store/postgres.go`, update the `Migrate` function to include the new migration:

```go
func (s *PostgresEventStore) Migrate(ctx context.Context) error {
    migrations := []string{migration001SQL, migration002SQL, migration003SQL}
    // ... rest unchanged
}
```

**Step 4: Verify migration applies cleanly**

Run: `task test:integration` (or manually test with a fresh database)

Expected: Migration applies without errors

**Step 5: Commit**

```bash
git add internal/store/migrations/003_world_model.sql internal/store/postgres.go
git commit -m "feat(world): add database schema for world model"
```

---

## Phase 2: Domain Types

### Task 2.1: Location Domain Types

**Files:**

- Create: `internal/world/location.go`
- Test: `internal/world/location_test.go`

**Step 1: Write the failing test**

Create `internal/world/location_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
    "testing"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"

    "github.com/holomush/holomush/internal/world"
)

func TestLocationType_String(t *testing.T) {
    tests := []struct {
        name     string
        locType  world.LocationType
        expected string
    }{
        {"persistent", world.LocationTypePersistent, "persistent"},
        {"scene", world.LocationTypeScene, "scene"},
        {"instance", world.LocationTypeInstance, "instance"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.expected, tt.locType.String())
        })
    }
}

func TestParseReplayPolicy(t *testing.T) {
    tests := []struct {
        name     string
        policy   string
        expected int
    }{
        {"none", "last:0", 0},
        {"ten", "last:10", 10},
        {"fifty", "last:50", 50},
        {"unlimited", "last:-1", -1},
        {"invalid prefix", "recent:10", 0},
        {"empty", "", 0},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.expected, world.ParseReplayPolicy(tt.policy))
        })
    }
}

func TestDefaultReplayPolicy(t *testing.T) {
    tests := []struct {
        name     string
        locType  world.LocationType
        expected string
    }{
        {"persistent", world.LocationTypePersistent, "last:0"},
        {"scene", world.LocationTypeScene, "last:-1"},
        {"instance", world.LocationTypeInstance, "last:0"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.expected, world.DefaultReplayPolicy(tt.locType))
        })
    }
}

func TestLocation_EffectiveDescription(t *testing.T) {
    parentID := ulid.Make()
    parent := &world.Location{
        ID:          parentID,
        Type:        world.LocationTypePersistent,
        Name:        "The Tavern",
        Description: "A cozy tavern with a roaring fire.",
    }

    t.Run("no shadow returns own description", func(t *testing.T) {
        loc := &world.Location{
            ID:          ulid.Make(),
            Type:        world.LocationTypePersistent,
            Name:        "Town Square",
            Description: "The center of town.",
        }
        desc, err := loc.EffectiveDescription(nil)
        assert.NoError(t, err)
        assert.Equal(t, "The center of town.", desc)
    })

    t.Run("scene with shadow returns parent description", func(t *testing.T) {
        loc := &world.Location{
            ID:          ulid.Make(),
            Type:        world.LocationTypeScene,
            ShadowsID:   &parentID,
            Name:        "",
            Description: "",
        }
        desc, err := loc.EffectiveDescription(parent)
        assert.NoError(t, err)
        assert.Equal(t, "A cozy tavern with a roaring fire.", desc)
    })

    t.Run("scene with override returns own description", func(t *testing.T) {
        loc := &world.Location{
            ID:          ulid.Make(),
            Type:        world.LocationTypeScene,
            ShadowsID:   &parentID,
            Name:        "Private Room",
            Description: "A private back room in the tavern.",
        }
        desc, err := loc.EffectiveDescription(parent)
        assert.NoError(t, err)
        assert.Equal(t, "A private back room in the tavern.", desc)
    })
}
```

**Step 2: Run test to verify it fails**

Run: `task test -- -run TestLocation ./internal/world/...`

Expected: FAIL - package does not exist

**Step 3: Write minimal implementation**

Create `internal/world/location.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package world contains the world model domain types and logic.
package world

import (
    "strconv"
    "strings"
    "time"

    "github.com/oklog/ulid/v2"
)

// LocationType identifies the kind of location.
type LocationType string

// Location types.
const (
    LocationTypePersistent LocationType = "persistent"
    LocationTypeScene      LocationType = "scene"
    LocationTypeInstance   LocationType = "instance"
)

// String returns the string representation of the location type.
func (t LocationType) String() string {
    return string(t)
}

// Location represents a room in the game world.
type Location struct {
    ID           ulid.ULID
    Type         LocationType
    ShadowsID    *ulid.ULID // For scenes cloning a persistent location
    Name         string
    Description  string
    OwnerID      *ulid.ULID
    ReplayPolicy string
    CreatedAt    time.Time
    ArchivedAt   *time.Time
}

// EffectiveDescription returns the description to show, falling back to shadow if empty.
// If this location shadows another and has an empty description, returns the parent's description.
// The parent parameter should be the shadowed location, or nil if not shadowing.
func (l *Location) EffectiveDescription(parent *Location) (string, error) {
    if l.Description != "" {
        return l.Description, nil
    }
    if l.ShadowsID != nil && parent != nil {
        return parent.Description, nil
    }
    return l.Description, nil
}

// EffectiveName returns the name to show, falling back to shadow if empty.
func (l *Location) EffectiveName(parent *Location) string {
    if l.Name != "" {
        return l.Name
    }
    if l.ShadowsID != nil && parent != nil {
        return parent.Name
    }
    return l.Name
}

// ParseReplayPolicy extracts the count from "last:N" format.
// Returns -1 for unlimited, 0 for none/invalid, positive N for limited.
func ParseReplayPolicy(policy string) int {
    if !strings.HasPrefix(policy, "last:") {
        return 0
    }
    n, err := strconv.Atoi(strings.TrimPrefix(policy, "last:"))
    if err != nil {
        return 0
    }
    return n
}

// DefaultReplayPolicy returns the default replay policy for a location type.
func DefaultReplayPolicy(locType LocationType) string {
    switch locType {
    case LocationTypeScene:
        return "last:-1"
    default:
        return "last:0"
    }
}
```

**Step 4: Run test to verify it passes**

Run: `task test -- -run TestLocation ./internal/world/...`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/world/location.go internal/world/location_test.go
git commit -m "feat(world): add Location domain type"
```

---

### Task 2.2: Exit Domain Types

**Files:**

- Create: `internal/world/exit.go`
- Test: `internal/world/exit_test.go`

**Step 1: Write the failing test**

Create `internal/world/exit_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
    "testing"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"

    "github.com/holomush/holomush/internal/world"
)

func TestExit_MatchesName(t *testing.T) {
    exit := &world.Exit{
        ID:       ulid.Make(),
        Name:     "north",
        Aliases:  []string{"n", "forward"},
    }

    tests := []struct {
        name     string
        input    string
        expected bool
    }{
        {"exact name", "north", true},
        {"alias n", "n", true},
        {"alias forward", "forward", true},
        {"case insensitive name", "North", true},
        {"case insensitive alias", "N", true},
        {"no match", "south", false},
        {"partial match", "nor", false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.expected, exit.MatchesName(tt.input))
        })
    }
}

func TestVisibility_String(t *testing.T) {
    tests := []struct {
        name       string
        visibility world.Visibility
        expected   string
    }{
        {"all", world.VisibilityAll, "all"},
        {"owner", world.VisibilityOwner, "owner"},
        {"list", world.VisibilityList, "list"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.expected, tt.visibility.String())
        })
    }
}

func TestExit_IsVisibleTo(t *testing.T) {
    ownerID := ulid.Make()
    allowedID := ulid.Make()
    otherID := ulid.Make()

    t.Run("visibility all", func(t *testing.T) {
        exit := &world.Exit{Visibility: world.VisibilityAll}
        assert.True(t, exit.IsVisibleTo(otherID, nil))
    })

    t.Run("visibility owner - is owner", func(t *testing.T) {
        exit := &world.Exit{Visibility: world.VisibilityOwner}
        // Note: owner check requires location owner, passed separately
        assert.True(t, exit.IsVisibleTo(ownerID, &ownerID))
    })

    t.Run("visibility owner - not owner", func(t *testing.T) {
        exit := &world.Exit{Visibility: world.VisibilityOwner}
        assert.False(t, exit.IsVisibleTo(otherID, &ownerID))
    })

    t.Run("visibility list - in list", func(t *testing.T) {
        exit := &world.Exit{
            Visibility: world.VisibilityList,
            VisibleTo:  []ulid.ULID{allowedID},
        }
        assert.True(t, exit.IsVisibleTo(allowedID, nil))
    })

    t.Run("visibility list - not in list", func(t *testing.T) {
        exit := &world.Exit{
            Visibility: world.VisibilityList,
            VisibleTo:  []ulid.ULID{allowedID},
        }
        assert.False(t, exit.IsVisibleTo(otherID, nil))
    })
}
```

**Step 2: Run test to verify it fails**

Run: `task test -- -run TestExit ./internal/world/...`

Expected: FAIL - Exit type not defined

**Step 3: Write minimal implementation**

Create `internal/world/exit.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
    "strings"
    "time"

    "github.com/oklog/ulid/v2"
)

// Visibility controls who can see an exit.
type Visibility string

// Visibility options.
const (
    VisibilityAll   Visibility = "all"
    VisibilityOwner Visibility = "owner"
    VisibilityList  Visibility = "list"
)

// String returns the string representation of the visibility.
func (v Visibility) String() string {
    return string(v)
}

// LockType identifies how an exit is locked.
type LockType string

// Lock types.
const (
    LockTypeKey       LockType = "key"
    LockTypePassword  LockType = "password"
    LockTypeCondition LockType = "condition"
)

// Exit represents a connection between two locations.
type Exit struct {
    ID             ulid.ULID
    FromLocationID ulid.ULID
    ToLocationID   ulid.ULID
    Name           string
    Aliases        []string
    Bidirectional  bool
    ReturnName     string
    Visibility     Visibility
    VisibleTo      []ulid.ULID // Character IDs when Visibility=list
    Locked         bool
    LockType       LockType
    LockData       map[string]any
    CreatedAt      time.Time
}

// MatchesName returns true if the given input matches the exit name or any alias.
// Matching is case-insensitive.
func (e *Exit) MatchesName(input string) bool {
    input = strings.ToLower(input)
    if strings.ToLower(e.Name) == input {
        return true
    }
    for _, alias := range e.Aliases {
        if strings.ToLower(alias) == input {
            return true
        }
    }
    return false
}

// IsVisibleTo returns true if the given character can see this exit.
// locationOwnerID is the owner of the location this exit is in (for VisibilityOwner).
func (e *Exit) IsVisibleTo(charID ulid.ULID, locationOwnerID *ulid.ULID) bool {
    switch e.Visibility {
    case VisibilityAll:
        return true
    case VisibilityOwner:
        return locationOwnerID != nil && *locationOwnerID == charID
    case VisibilityList:
        for _, allowed := range e.VisibleTo {
            if allowed == charID {
                return true
            }
        }
        return false
    default:
        return true
    }
}

// ReverseExit creates the return exit for a bidirectional exit.
// Returns nil if not bidirectional or no return name is set.
func (e *Exit) ReverseExit() *Exit {
    if !e.Bidirectional || e.ReturnName == "" {
        return nil
    }
    return &Exit{
        FromLocationID: e.ToLocationID,
        ToLocationID:   e.FromLocationID,
        Name:           e.ReturnName,
        Bidirectional:  true,
        ReturnName:     e.Name,
        Visibility:     e.Visibility,
        VisibleTo:      e.VisibleTo,
        Locked:         e.Locked,
        LockType:       e.LockType,
        LockData:       e.LockData,
    }
}
```

**Step 4: Run test to verify it passes**

Run: `task test -- -run TestExit ./internal/world/...`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/world/exit.go internal/world/exit_test.go
git commit -m "feat(world): add Exit domain type"
```

---

### Task 2.3: Object Domain Types

**Files:**

- Create: `internal/world/object.go`
- Test: `internal/world/object_test.go`

**Step 1: Write the failing test**

Create `internal/world/object_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
    "testing"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"

    "github.com/holomush/holomush/internal/world"
)

func TestContainment_Validate(t *testing.T) {
    locID := ulid.Make()
    charID := ulid.Make()
    objID := ulid.Make()

    tests := []struct {
        name        string
        containment world.Containment
        expectErr   bool
    }{
        {
            name:        "in location",
            containment: world.Containment{LocationID: &locID},
            expectErr:   false,
        },
        {
            name:        "held by character",
            containment: world.Containment{CharacterID: &charID},
            expectErr:   false,
        },
        {
            name:        "in container",
            containment: world.Containment{ObjectID: &objID},
            expectErr:   false,
        },
        {
            name:        "nowhere - invalid",
            containment: world.Containment{},
            expectErr:   true,
        },
        {
            name:        "multiple places - invalid",
            containment: world.Containment{LocationID: &locID, CharacterID: &charID},
            expectErr:   true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := tt.containment.Validate()
            if tt.expectErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}

func TestContainment_Type(t *testing.T) {
    locID := ulid.Make()
    charID := ulid.Make()
    objID := ulid.Make()

    tests := []struct {
        name        string
        containment world.Containment
        expected    string
    }{
        {"location", world.Containment{LocationID: &locID}, "location"},
        {"character", world.Containment{CharacterID: &charID}, "character"},
        {"object", world.Containment{ObjectID: &objID}, "object"},
        {"empty", world.Containment{}, ""},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.expected, tt.containment.Type())
        })
    }
}

func TestObject_Containment(t *testing.T) {
    locID := ulid.Make()
    obj := &world.Object{
        ID:         ulid.Make(),
        Name:       "Sword",
        LocationID: &locID,
    }

    containment := obj.Containment()
    assert.NotNil(t, containment.LocationID)
    assert.Equal(t, locID, *containment.LocationID)
    assert.Nil(t, containment.CharacterID)
    assert.Nil(t, containment.ObjectID)
}
```

**Step 2: Run test to verify it fails**

Run: `task test -- -run TestContainment ./internal/world/...`

Expected: FAIL - Containment type not defined

**Step 3: Write minimal implementation**

Create `internal/world/object.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
    "errors"
    "time"

    "github.com/oklog/ulid/v2"
)

// ErrInvalidContainment is returned when containment validation fails.
var ErrInvalidContainment = errors.New("object must be in exactly one place")

// Containment represents where an object is located.
// Exactly one field must be set.
type Containment struct {
    LocationID  *ulid.ULID
    CharacterID *ulid.ULID
    ObjectID    *ulid.ULID // Container object
}

// Validate ensures exactly one containment field is set.
func (c *Containment) Validate() error {
    count := 0
    if c.LocationID != nil {
        count++
    }
    if c.CharacterID != nil {
        count++
    }
    if c.ObjectID != nil {
        count++
    }
    if count != 1 {
        return ErrInvalidContainment
    }
    return nil
}

// Type returns the containment type: "location", "character", "object", or "".
func (c *Containment) Type() string {
    if c.LocationID != nil {
        return "location"
    }
    if c.CharacterID != nil {
        return "character"
    }
    if c.ObjectID != nil {
        return "object"
    }
    return ""
}

// ID returns the ID of the container (location, character, or object).
func (c *Containment) ID() *ulid.ULID {
    if c.LocationID != nil {
        return c.LocationID
    }
    if c.CharacterID != nil {
        return c.CharacterID
    }
    return c.ObjectID
}

// Object represents an item in the game world.
type Object struct {
    ID                  ulid.ULID
    Name                string
    Description         string
    LocationID          *ulid.ULID
    HeldByCharacterID   *ulid.ULID
    ContainedInObjectID *ulid.ULID
    IsContainer         bool
    OwnerID             *ulid.ULID
    CreatedAt           time.Time
}

// Containment returns the current containment of this object.
func (o *Object) Containment() Containment {
    return Containment{
        LocationID:  o.LocationID,
        CharacterID: o.HeldByCharacterID,
        ObjectID:    o.ContainedInObjectID,
    }
}

// SetContainment updates the object's location.
// Clears all previous containment and sets the new one.
func (o *Object) SetContainment(c Containment) {
    o.LocationID = c.LocationID
    o.HeldByCharacterID = c.CharacterID
    o.ContainedInObjectID = c.ObjectID
}
```

**Step 4: Run test to verify it passes**

Run: `task test -- -run TestContainment ./internal/world/... && task test -- -run TestObject ./internal/world/...`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/world/object.go internal/world/object_test.go
git commit -m "feat(world): add Object domain type with containment"
```

---

### Task 2.4: Repository Interfaces

**Files:**

- Create: `internal/world/repository.go`

**Step 1: Write repository interfaces**

Create `internal/world/repository.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
    "context"

    "github.com/oklog/ulid/v2"
)

// LocationRepository manages location persistence.
type LocationRepository interface {
    // Get retrieves a location by ID.
    Get(ctx context.Context, id ulid.ULID) (*Location, error)

    // Create persists a new location.
    Create(ctx context.Context, loc *Location) error

    // Update modifies an existing location.
    Update(ctx context.Context, loc *Location) error

    // Delete removes a location by ID.
    Delete(ctx context.Context, id ulid.ULID) error

    // ListByType returns all locations of the given type.
    ListByType(ctx context.Context, locType LocationType) ([]*Location, error)

    // GetShadowedBy returns scenes that shadow the given location.
    GetShadowedBy(ctx context.Context, id ulid.ULID) ([]*Location, error)
}

// ExitRepository manages exit persistence.
type ExitRepository interface {
    // Get retrieves an exit by ID.
    Get(ctx context.Context, id ulid.ULID) (*Exit, error)

    // Create persists a new exit.
    // If bidirectional, also creates the return exit.
    Create(ctx context.Context, exit *Exit) error

    // Update modifies an existing exit.
    Update(ctx context.Context, exit *Exit) error

    // Delete removes an exit by ID.
    // If bidirectional, also removes the return exit.
    Delete(ctx context.Context, id ulid.ULID) error

    // ListFromLocation returns all exits from a location.
    ListFromLocation(ctx context.Context, locationID ulid.ULID) ([]*Exit, error)

    // FindByName finds an exit by name or alias from a location.
    FindByName(ctx context.Context, locationID ulid.ULID, name string) (*Exit, error)
}

// ObjectRepository manages object persistence.
type ObjectRepository interface {
    // Get retrieves an object by ID.
    Get(ctx context.Context, id ulid.ULID) (*Object, error)

    // Create persists a new object.
    Create(ctx context.Context, obj *Object) error

    // Update modifies an existing object.
    Update(ctx context.Context, obj *Object) error

    // Delete removes an object by ID.
    Delete(ctx context.Context, id ulid.ULID) error

    // ListAtLocation returns all objects at a location.
    ListAtLocation(ctx context.Context, locationID ulid.ULID) ([]*Object, error)

    // ListHeldBy returns all objects held by a character.
    ListHeldBy(ctx context.Context, characterID ulid.ULID) ([]*Object, error)

    // ListContainedIn returns all objects inside a container object.
    ListContainedIn(ctx context.Context, objectID ulid.ULID) ([]*Object, error)

    // Move changes an object's containment.
    // Validates containment and enforces business rules.
    Move(ctx context.Context, objectID ulid.ULID, to Containment) error
}

// SceneRepository manages scene-specific operations.
type SceneRepository interface {
    // AddParticipant adds a character to a scene.
    AddParticipant(ctx context.Context, sceneID, characterID ulid.ULID, role string) error

    // RemoveParticipant removes a character from a scene.
    RemoveParticipant(ctx context.Context, sceneID, characterID ulid.ULID) error

    // ListParticipants returns all participants in a scene.
    ListParticipants(ctx context.Context, sceneID ulid.ULID) ([]SceneParticipant, error)

    // GetScenesFor returns all scenes a character is participating in.
    GetScenesFor(ctx context.Context, characterID ulid.ULID) ([]*Location, error)
}

// SceneParticipant represents a character's membership in a scene.
type SceneParticipant struct {
    CharacterID ulid.ULID
    Role        string // "owner", "member", "invited"
}
```

**Step 2: Run lint to verify**

Run: `task lint`

Expected: No errors

**Step 3: Commit**

```bash
git add internal/world/repository.go
git commit -m "feat(world): add repository interfaces"
```

---

## Phase 3: PostgreSQL Repositories

### Task 3.1: Location Repository

**Files:**

- Create: `internal/world/postgres/location_repo.go`
- Test: `internal/world/postgres/location_repo_test.go`

**Step 1: Write the failing test**

Create `internal/world/postgres/location_repo_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres_test

import (
    "context"
    "testing"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/core"
    "github.com/holomush/holomush/internal/world"
    "github.com/holomush/holomush/internal/world/postgres"
)

func TestLocationRepository_CRUD(t *testing.T) {
    // Skip if no test database
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    ctx := context.Background()
    repo := postgres.NewLocationRepository(testPool)

    t.Run("create and get", func(t *testing.T) {
        loc := &world.Location{
            ID:           core.NewULID(),
            Type:         world.LocationTypePersistent,
            Name:         "Test Room",
            Description:  "A test room for testing.",
            ReplayPolicy: "last:0",
        }

        err := repo.Create(ctx, loc)
        require.NoError(t, err)

        got, err := repo.Get(ctx, loc.ID)
        require.NoError(t, err)
        assert.Equal(t, loc.Name, got.Name)
        assert.Equal(t, loc.Description, got.Description)
        assert.Equal(t, loc.Type, got.Type)

        // Cleanup
        _ = repo.Delete(ctx, loc.ID)
    })

    t.Run("update", func(t *testing.T) {
        loc := &world.Location{
            ID:           core.NewULID(),
            Type:         world.LocationTypePersistent,
            Name:         "Original Name",
            Description:  "Original description.",
            ReplayPolicy: "last:0",
        }

        err := repo.Create(ctx, loc)
        require.NoError(t, err)

        loc.Name = "Updated Name"
        loc.Description = "Updated description."
        err = repo.Update(ctx, loc)
        require.NoError(t, err)

        got, err := repo.Get(ctx, loc.ID)
        require.NoError(t, err)
        assert.Equal(t, "Updated Name", got.Name)
        assert.Equal(t, "Updated description.", got.Description)

        // Cleanup
        _ = repo.Delete(ctx, loc.ID)
    })

    t.Run("list by type", func(t *testing.T) {
        scene := &world.Location{
            ID:           core.NewULID(),
            Type:         world.LocationTypeScene,
            Name:         "Test Scene",
            Description:  "A scene.",
            ReplayPolicy: "last:-1",
        }

        err := repo.Create(ctx, scene)
        require.NoError(t, err)

        scenes, err := repo.ListByType(ctx, world.LocationTypeScene)
        require.NoError(t, err)
        assert.NotEmpty(t, scenes)

        found := false
        for _, s := range scenes {
            if s.ID == scene.ID {
                found = true
                break
            }
        }
        assert.True(t, found, "created scene should be in list")

        // Cleanup
        _ = repo.Delete(ctx, scene.ID)
    })

    t.Run("get not found", func(t *testing.T) {
        _, err := repo.Get(ctx, ulid.Make())
        assert.Error(t, err)
    })
}
```

**Step 2: Run test to verify it fails**

Run: `task test -- -run TestLocationRepository ./internal/world/postgres/...`

Expected: FAIL - package does not exist

**Step 3: Write minimal implementation**

Create `internal/world/postgres/location_repo.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package postgres provides PostgreSQL implementations of world repositories.
package postgres

import (
    "context"
    "errors"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/oklog/ulid/v2"
    "github.com/samber/oops"

    "github.com/holomush/holomush/internal/world"
)

// ErrNotFound is returned when an entity is not found.
var ErrNotFound = errors.New("not found")

// LocationRepository implements world.LocationRepository using PostgreSQL.
type LocationRepository struct {
    pool *pgxpool.Pool
}

// NewLocationRepository creates a new LocationRepository.
func NewLocationRepository(pool *pgxpool.Pool) *LocationRepository {
    return &LocationRepository{pool: pool}
}

// Get retrieves a location by ID.
func (r *LocationRepository) Get(ctx context.Context, id ulid.ULID) (*world.Location, error) {
    var loc world.Location
    var idStr string
    var shadowsIDStr *string
    var ownerIDStr *string

    err := r.pool.QueryRow(ctx, `
        SELECT id, type, shadows_id, name, description, owner_id, replay_policy, created_at, archived_at
        FROM locations WHERE id = $1
    `, id.String()).Scan(
        &idStr, &loc.Type, &shadowsIDStr, &loc.Name, &loc.Description,
        &ownerIDStr, &loc.ReplayPolicy, &loc.CreatedAt, &loc.ArchivedAt,
    )
    if errors.Is(err, pgx.ErrNoRows) {
        return nil, oops.With("id", id.String()).Wrap(ErrNotFound)
    }
    if err != nil {
        return nil, oops.With("operation", "get location").With("id", id.String()).Wrap(err)
    }

    loc.ID, _ = ulid.Parse(idStr)
    if shadowsIDStr != nil {
        sid, _ := ulid.Parse(*shadowsIDStr)
        loc.ShadowsID = &sid
    }
    if ownerIDStr != nil {
        oid, _ := ulid.Parse(*ownerIDStr)
        loc.OwnerID = &oid
    }

    return &loc, nil
}

// Create persists a new location.
func (r *LocationRepository) Create(ctx context.Context, loc *world.Location) error {
    var shadowsID, ownerID *string
    if loc.ShadowsID != nil {
        s := loc.ShadowsID.String()
        shadowsID = &s
    }
    if loc.OwnerID != nil {
        o := loc.OwnerID.String()
        ownerID = &o
    }

    _, err := r.pool.Exec(ctx, `
        INSERT INTO locations (id, type, shadows_id, name, description, owner_id, replay_policy, created_at, archived_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
    `, loc.ID.String(), loc.Type, shadowsID, loc.Name, loc.Description,
        ownerID, loc.ReplayPolicy, loc.CreatedAt, loc.ArchivedAt)
    if err != nil {
        return oops.With("operation", "create location").With("id", loc.ID.String()).Wrap(err)
    }
    return nil
}

// Update modifies an existing location.
func (r *LocationRepository) Update(ctx context.Context, loc *world.Location) error {
    var shadowsID, ownerID *string
    if loc.ShadowsID != nil {
        s := loc.ShadowsID.String()
        shadowsID = &s
    }
    if loc.OwnerID != nil {
        o := loc.OwnerID.String()
        ownerID = &o
    }

    result, err := r.pool.Exec(ctx, `
        UPDATE locations SET type = $2, shadows_id = $3, name = $4, description = $5,
        owner_id = $6, replay_policy = $7, archived_at = $8
        WHERE id = $1
    `, loc.ID.String(), loc.Type, shadowsID, loc.Name, loc.Description,
        ownerID, loc.ReplayPolicy, loc.ArchivedAt)
    if err != nil {
        return oops.With("operation", "update location").With("id", loc.ID.String()).Wrap(err)
    }
    if result.RowsAffected() == 0 {
        return oops.With("id", loc.ID.String()).Wrap(ErrNotFound)
    }
    return nil
}

// Delete removes a location by ID.
func (r *LocationRepository) Delete(ctx context.Context, id ulid.ULID) error {
    result, err := r.pool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, id.String())
    if err != nil {
        return oops.With("operation", "delete location").With("id", id.String()).Wrap(err)
    }
    if result.RowsAffected() == 0 {
        return oops.With("id", id.String()).Wrap(ErrNotFound)
    }
    return nil
}

// ListByType returns all locations of the given type.
func (r *LocationRepository) ListByType(ctx context.Context, locType world.LocationType) ([]*world.Location, error) {
    rows, err := r.pool.Query(ctx, `
        SELECT id, type, shadows_id, name, description, owner_id, replay_policy, created_at, archived_at
        FROM locations WHERE type = $1 ORDER BY created_at DESC
    `, string(locType))
    if err != nil {
        return nil, oops.With("operation", "list locations by type").With("type", string(locType)).Wrap(err)
    }
    defer rows.Close()

    return scanLocations(rows)
}

// GetShadowedBy returns scenes that shadow the given location.
func (r *LocationRepository) GetShadowedBy(ctx context.Context, id ulid.ULID) ([]*world.Location, error) {
    rows, err := r.pool.Query(ctx, `
        SELECT id, type, shadows_id, name, description, owner_id, replay_policy, created_at, archived_at
        FROM locations WHERE shadows_id = $1 ORDER BY created_at DESC
    `, id.String())
    if err != nil {
        return nil, oops.With("operation", "get shadowed by").With("id", id.String()).Wrap(err)
    }
    defer rows.Close()

    return scanLocations(rows)
}

func scanLocations(rows pgx.Rows) ([]*world.Location, error) {
    var locations []*world.Location
    for rows.Next() {
        var loc world.Location
        var idStr string
        var shadowsIDStr, ownerIDStr *string

        if err := rows.Scan(
            &idStr, &loc.Type, &shadowsIDStr, &loc.Name, &loc.Description,
            &ownerIDStr, &loc.ReplayPolicy, &loc.CreatedAt, &loc.ArchivedAt,
        ); err != nil {
            return nil, oops.With("operation", "scan location").Wrap(err)
        }

        loc.ID, _ = ulid.Parse(idStr)
        if shadowsIDStr != nil {
            sid, _ := ulid.Parse(*shadowsIDStr)
            loc.ShadowsID = &sid
        }
        if ownerIDStr != nil {
            oid, _ := ulid.Parse(*ownerIDStr)
            loc.OwnerID = &oid
        }

        locations = append(locations, &loc)
    }

    if err := rows.Err(); err != nil {
        return nil, oops.With("operation", "iterate locations").Wrap(err)
    }

    return locations, nil
}
```

**Step 4: Create test setup file**

Create `internal/world/postgres/postgres_test.go` for test setup:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres_test

import (
    "context"
    "os"
    "testing"

    "github.com/jackc/pgx/v5/pgxpool"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
    dsn := os.Getenv("TEST_DATABASE_URL")
    if dsn == "" {
        dsn = "postgres://localhost:5432/holomush_test?sslmode=disable"
    }

    ctx := context.Background()
    pool, err := pgxpool.New(ctx, dsn)
    if err != nil {
        // Skip tests if no database available
        os.Exit(0)
    }
    testPool = pool

    code := m.Run()
    pool.Close()
    os.Exit(code)
}
```

**Step 5: Run test to verify it passes**

Run: `task test -- -run TestLocationRepository ./internal/world/postgres/...`

Expected: PASS (or skip if no test database)

**Step 6: Commit**

```bash
git add internal/world/postgres/
git commit -m "feat(world): add PostgreSQL LocationRepository"
```

---

### Task 3.2: Exit Repository

**Files:**

- Create: `internal/world/postgres/exit_repo.go`
- Test: `internal/world/postgres/exit_repo_test.go`

This follows the same pattern as Task 3.1. Implementation includes:

- CRUD operations for exits
- Bidirectional exit handling (create/delete pairs)
- `FindByName` with alias matching
- Integration tests

**Commit message:** `feat(world): add PostgreSQL ExitRepository`

---

### Task 3.3: Object Repository

**Files:**

- Create: `internal/world/postgres/object_repo.go`
- Test: `internal/world/postgres/object_repo_test.go`

This follows the same pattern as Task 3.1. Implementation includes:

- CRUD operations for objects
- Containment queries (ListAtLocation, ListHeldBy, ListContainedIn)
- Move operation with validation
- Integration tests

**Commit message:** `feat(world): add PostgreSQL ObjectRepository`

---

### Task 3.4: Scene Repository

**Files:**

- Create: `internal/world/postgres/scene_repo.go`
- Test: `internal/world/postgres/scene_repo_test.go`

This follows the same pattern as Task 3.1. Implementation includes:

- Participant management
- Scene listing for characters
- Integration tests

**Commit message:** `feat(world): add PostgreSQL SceneRepository`

---

## Phase 4: Event Types

### Task 4.1: Add World Event Types

**Files:**

- Modify: `internal/core/event.go`
- Test: `internal/core/event_test.go`

**Step 1: Add new event types**

Add to `internal/core/event.go`:

```go
// World event types
const (
    EventTypeMove          EventType = "move"
    EventTypeObjectCreate  EventType = "object_create"
    EventTypeObjectDestroy EventType = "object_destroy"
    EventTypeObjectUse     EventType = "object_use"
    EventTypeObjectExamine EventType = "object_examine"
    EventTypeObjectGive    EventType = "object_give"
)
```

**Step 2: Add tests for new event types**

**Step 3: Commit**

```bash
git add internal/core/event.go internal/core/event_test.go
git commit -m "feat(core): add world model event types"
```

---

### Task 4.2: Add Event Payloads

**Files:**

- Create: `internal/world/payloads.go`
- Test: `internal/world/payloads_test.go`

**Step 1: Define payload types**

```go
// MovePayload for "move" events.
type MovePayload struct {
    EntityType string `json:"entity_type"`  // "character" | "object"
    EntityID   string `json:"entity_id"`
    FromType   string `json:"from_type"`    // "location" | "character" | "object"
    FromID     string `json:"from_id"`
    ToType     string `json:"to_type"`
    ToID       string `json:"to_id"`
    ExitID     string `json:"exit_id,omitempty"`
    ExitName   string `json:"exit_name,omitempty"`
}

// ObjectGivePayload for "object_give" events.
type ObjectGivePayload struct {
    ObjectID        string `json:"object_id"`
    ObjectName      string `json:"object_name"`
    FromCharacterID string `json:"from_character_id"`
    ToCharacterID   string `json:"to_character_id"`
}
```

**Step 2: Add JSON marshal/unmarshal tests**

**Step 3: Commit**

```bash
git add internal/world/payloads.go internal/world/payloads_test.go
git commit -m "feat(world): add event payload types"
```

---

## Phase 5: Test Helpers and Mocks

### Task 5.1: Generate Mocks

**Files:**

- Create: `internal/world/worldtest/mocks.go`

**Step 1: Add mockery configuration**

Add to `.mockery.yaml`:

```yaml
packages:
  github.com/holomush/holomush/internal/world:
    interfaces:
      LocationRepository:
        config:
          outpkg: worldtest
          dir: internal/world/worldtest
      ExitRepository:
        config:
          outpkg: worldtest
          dir: internal/world/worldtest
      ObjectRepository:
        config:
          outpkg: worldtest
          dir: internal/world/worldtest
      SceneRepository:
        config:
          outpkg: worldtest
          dir: internal/world/worldtest
```

**Step 2: Generate mocks**

Run: `mockery`

**Step 3: Commit**

```bash
git add .mockery.yaml internal/world/worldtest/
git commit -m "feat(world): add test mocks for repositories"
```

---

## Summary

| Phase | Tasks   | Description                                       |
| ----- | ------- | ------------------------------------------------- |
| 1     | 1.1     | Database migration for world model                |
| 2     | 2.1-2.4 | Domain types (Location, Exit, Object, Interfaces) |
| 3     | 3.1-3.4 | PostgreSQL repository implementations             |
| 4     | 4.1-4.2 | Event types and payloads                          |
| 5     | 5.1     | Test helpers and mocks                            |

## Test Strategy

- **Unit tests:** All domain types, validation logic, helper functions
- **Integration tests:** Repository implementations against real PostgreSQL
- **Table-driven tests:** Cover edge cases systematically
- **Mocks:** Generated for all repository interfaces
- **Coverage target:** >80% per package

## Migration Notes

- Migration 003 extends existing `locations` table (ALTER TABLE)
- New tables: `exits`, `objects`, `scene_participants`
- Test data in migration 001 remains compatible
- Run `task test:integration` to verify migration
