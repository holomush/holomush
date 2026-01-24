# Epic 4 Phase 2: World Model Completion

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Complete Epic 4 by implementing character-location binding, world query host functions, plugin hooks for world events, and world seeding CLI.

**Architecture:** Extend existing world package with character repository, add world query functions to Lua host functions, wire event emission for movement/object operations, and create CLI command for initial world seeding.

**Tech Stack:** Go 1.23+, PostgreSQL, pgx/v5, gopher-lua, testify, oops errors

---

## Phase Overview

| Phase | Description | Status |
|-------|-------------|--------|
| 4.1-4.3 | Locations, Exits, Objects | ✅ Complete (PR #35) |
| 4.4 | Character-location binding | This plan |
| 4.5 | Plugin hooks for world events | This plan |
| 4.6 | World seeding | This plan |
| Host functions | QueryRoom, QueryCharacter, QueryRoomCharacters | This plan |

---

## Task 1: Character Repository Interface

**Files:**

- Modify: `internal/world/repository.go`
- Create: `internal/world/character.go`
- Create: `internal/world/character_test.go`

**Step 1: Write the failing test for Character type**

Create `internal/world/character_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
)

func TestCharacter_Validate(t *testing.T) {
	locID := ulid.Make()

	t.Run("valid character", func(t *testing.T) {
		char := &world.Character{
			Name:       "TestChar",
			LocationID: &locID,
		}
		require.NoError(t, char.Validate())
	})

	t.Run("empty name fails", func(t *testing.T) {
		char := &world.Character{
			Name:       "",
			LocationID: &locID,
		}
		err := char.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name")
	})

	t.Run("nil location allowed", func(t *testing.T) {
		char := &world.Character{
			Name:       "TestChar",
			LocationID: nil,
		}
		require.NoError(t, char.Validate())
	})
}
```

**Step 2: Run test to verify it fails**

Run: `task test -- -v ./internal/world/... -run TestCharacter`
Expected: FAIL with "undefined: world.Character"

**Step 3: Write Character type**

Create `internal/world/character.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"time"

	"github.com/oklog/ulid/v2"
)

// Character represents a player character in the world.
type Character struct {
	ID         ulid.ULID
	PlayerID   ulid.ULID
	Name       string
	LocationID *ulid.ULID // Current location (nil if not in world)
	CreatedAt  time.Time
}

// Validate checks that the character has required fields.
func (c *Character) Validate() error {
	if err := ValidateName(c.Name); err != nil {
		return err
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `task test -- -v ./internal/world/... -run TestCharacter`
Expected: PASS

**Step 5: Add CharacterRepository interface**

Add to `internal/world/repository.go`:

```go
// CharacterRepository defines operations for character persistence.
type CharacterRepository interface {
	// Get retrieves a character by ID.
	Get(ctx context.Context, id ulid.ULID) (*Character, error)

	// GetByLocation retrieves all characters at a location.
	GetByLocation(ctx context.Context, locationID ulid.ULID) ([]*Character, error)

	// UpdateLocation moves a character to a new location.
	UpdateLocation(ctx context.Context, characterID ulid.ULID, locationID *ulid.ULID) error
}
```

**Step 6: Commit**

```bash
git add internal/world/character.go internal/world/character_test.go internal/world/repository.go
git commit -m "feat(world): add Character type and CharacterRepository interface"
```

---

## Task 2: PostgreSQL Character Repository

**Files:**

- Create: `internal/world/postgres/character_repo.go`
- Create: `internal/world/postgres/character_repo_test.go`

**Step 1: Write the failing test**

Create `internal/world/postgres/character_repo_test.go`:

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

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/postgres"
)

func TestCharacterRepository_Get(t *testing.T) {
	pool := getTestPool(t)
	repo := postgres.NewCharacterRepository(pool)
	ctx := context.Background()

	t.Run("returns ErrNotFound for non-existent character", func(t *testing.T) {
		_, err := repo.Get(ctx, ulid.Make())
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
	})
}

func TestCharacterRepository_GetByLocation(t *testing.T) {
	pool := getTestPool(t)
	repo := postgres.NewCharacterRepository(pool)
	ctx := context.Background()

	t.Run("returns empty slice for location with no characters", func(t *testing.T) {
		chars, err := repo.GetByLocation(ctx, ulid.Make())
		require.NoError(t, err)
		assert.Empty(t, chars)
	})
}
```

**Step 2: Run test to verify it fails**

Run: `task test -- -v ./internal/world/postgres/... -run TestCharacterRepository`
Expected: FAIL with "undefined: postgres.NewCharacterRepository"

**Step 3: Write minimal implementation**

Create `internal/world/postgres/character_repo.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

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

// CharacterRepository implements world.CharacterRepository using PostgreSQL.
type CharacterRepository struct {
	pool *pgxpool.Pool
}

// NewCharacterRepository creates a new PostgreSQL character repository.
func NewCharacterRepository(pool *pgxpool.Pool) *CharacterRepository {
	return &CharacterRepository{pool: pool}
}

// Get retrieves a character by ID.
func (r *CharacterRepository) Get(ctx context.Context, id ulid.ULID) (*world.Character, error) {
	const query = `
		SELECT id, player_id, name, location_id, created_at
		FROM characters
		WHERE id = $1`

	var char world.Character
	var locationIDStr *string

	err := r.pool.QueryRow(ctx, query, id.String()).Scan(
		&char.ID,
		&char.PlayerID,
		&char.Name,
		&locationIDStr,
		&char.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("CHARACTER_NOT_FOUND").With("id", id.String()).Wrap(world.ErrNotFound)
	}
	if err != nil {
		return nil, oops.With("operation", "get character").Wrap(err)
	}

	if locationIDStr != nil {
		locID, err := ulid.Parse(*locationIDStr)
		if err != nil {
			return nil, oops.With("operation", "parse location_id").Wrap(err)
		}
		char.LocationID = &locID
	}

	return &char, nil
}

// GetByLocation retrieves all characters at a location.
func (r *CharacterRepository) GetByLocation(ctx context.Context, locationID ulid.ULID) ([]*world.Character, error) {
	const query = `
		SELECT id, player_id, name, location_id, created_at
		FROM characters
		WHERE location_id = $1
		ORDER BY name`

	rows, err := r.pool.Query(ctx, query, locationID.String())
	if err != nil {
		return nil, oops.With("operation", "get characters by location").Wrap(err)
	}
	defer rows.Close()

	var chars []*world.Character
	for rows.Next() {
		var char world.Character
		var locationIDStr *string

		if err := rows.Scan(&char.ID, &char.PlayerID, &char.Name, &locationIDStr, &char.CreatedAt); err != nil {
			return nil, oops.With("operation", "scan character").Wrap(err)
		}

		if locationIDStr != nil {
			locID, err := ulid.Parse(*locationIDStr)
			if err != nil {
				return nil, oops.With("operation", "parse location_id").Wrap(err)
			}
			char.LocationID = &locID
		}

		chars = append(chars, &char)
	}

	if err := rows.Err(); err != nil {
		return nil, oops.With("operation", "iterate characters").Wrap(err)
	}

	return chars, nil
}

// UpdateLocation moves a character to a new location.
func (r *CharacterRepository) UpdateLocation(ctx context.Context, characterID ulid.ULID, locationID *ulid.ULID) error {
	var locIDStr *string
	if locationID != nil {
		s := locationID.String()
		locIDStr = &s
	}

	const query = `UPDATE characters SET location_id = $2 WHERE id = $1`
	result, err := r.pool.Exec(ctx, query, characterID.String(), locIDStr)
	if err != nil {
		return oops.With("operation", "update character location").Wrap(err)
	}

	if result.RowsAffected() == 0 {
		return oops.Code("CHARACTER_NOT_FOUND").With("id", characterID.String()).Wrap(world.ErrNotFound)
	}

	return nil
}

// Compile-time check that CharacterRepository implements world.CharacterRepository.
var _ world.CharacterRepository = (*CharacterRepository)(nil)
```

**Step 4: Run test to verify it passes**

Run: `task test -- -v ./internal/world/postgres/... -run TestCharacterRepository`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/world/postgres/character_repo.go internal/world/postgres/character_repo_test.go
git commit -m "feat(world): add PostgreSQL CharacterRepository"
```

---

## Task 3: World Query Host Functions

**Files:**

- Modify: `internal/plugin/hostfunc/functions.go`
- Create: `internal/plugin/hostfunc/world.go`
- Create: `internal/plugin/hostfunc/world_test.go`

**Step 1: Define WorldQuerier interface**

Create `internal/plugin/hostfunc/world.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"

	"github.com/oklog/ulid/v2"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/world"
)

// WorldQuerier provides read access to world data.
type WorldQuerier interface {
	// GetLocation retrieves a location by ID.
	GetLocation(ctx context.Context, id ulid.ULID) (*world.Location, error)

	// GetCharacter retrieves a character by ID.
	GetCharacter(ctx context.Context, id ulid.ULID) (*world.Character, error)

	// GetCharactersByLocation retrieves all characters at a location.
	GetCharactersByLocation(ctx context.Context, locationID ulid.ULID) ([]*world.Character, error)
}

// queryRoomFn returns a Lua function that queries room information.
func (f *Functions) queryRoomFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.world == nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("world querier not configured"))
			return 2
		}

		roomID := L.CheckString(1)
		id, err := ulid.Parse(roomID)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid room ID: " + err.Error()))
			return 2
		}

		ctx, cancel := context.WithTimeout(context.Background(), kvTimeout)
		defer cancel()

		loc, err := f.world.GetLocation(ctx, id)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}

		// Return room info as a table
		room := L.NewTable()
		L.SetField(room, "id", lua.LString(loc.ID.String()))
		L.SetField(room, "name", lua.LString(loc.Name))
		L.SetField(room, "description", lua.LString(loc.Description))
		L.SetField(room, "type", lua.LString(string(loc.Type)))

		L.Push(room)
		L.Push(lua.LNil)
		return 2
	}
}

// queryCharacterFn returns a Lua function that queries character information.
func (f *Functions) queryCharacterFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.world == nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("world querier not configured"))
			return 2
		}

		charID := L.CheckString(1)
		id, err := ulid.Parse(charID)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid character ID: " + err.Error()))
			return 2
		}

		ctx, cancel := context.WithTimeout(context.Background(), kvTimeout)
		defer cancel()

		char, err := f.world.GetCharacter(ctx, id)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}

		// Return character info as a table
		character := L.NewTable()
		L.SetField(character, "id", lua.LString(char.ID.String()))
		L.SetField(character, "name", lua.LString(char.Name))
		if char.LocationID != nil {
			L.SetField(character, "location_id", lua.LString(char.LocationID.String()))
		}

		L.Push(character)
		L.Push(lua.LNil)
		return 2
	}
}

// queryRoomCharactersFn returns a Lua function that queries characters in a room.
func (f *Functions) queryRoomCharactersFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.world == nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("world querier not configured"))
			return 2
		}

		roomID := L.CheckString(1)
		id, err := ulid.Parse(roomID)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid room ID: " + err.Error()))
			return 2
		}

		ctx, cancel := context.WithTimeout(context.Background(), kvTimeout)
		defer cancel()

		chars, err := f.world.GetCharactersByLocation(ctx, id)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}

		// Return array of character info
		characters := L.NewTable()
		for i, char := range chars {
			c := L.NewTable()
			L.SetField(c, "id", lua.LString(char.ID.String()))
			L.SetField(c, "name", lua.LString(char.Name))
			characters.RawSetInt(i+1, c)
		}

		L.Push(characters)
		L.Push(lua.LNil)
		return 2
	}
}
```

**Step 2: Update Functions struct to include WorldQuerier**

In `internal/plugin/hostfunc/functions.go`, update:

```go
// Functions provides host functions to Lua plugins.
type Functions struct {
	kvStore  KVStore
	enforcer CapabilityChecker
	world    WorldQuerier // Add this field
}

// New creates host functions with dependencies.
func New(kv KVStore, enforcer CapabilityChecker, opts ...Option) *Functions {
	if enforcer == nil {
		panic("hostfunc.New: enforcer cannot be nil")
	}
	f := &Functions{
		kvStore:  kv,
		enforcer: enforcer,
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// Option configures Functions.
type Option func(*Functions)

// WithWorldQuerier sets the world querier for world query functions.
func WithWorldQuerier(w WorldQuerier) Option {
	return func(f *Functions) {
		f.world = w
	}
}
```

**Step 3: Register world query functions**

In `Register` function, add:

```go
// World queries (capability required)
ls.SetField(mod, "query_room", ls.NewFunction(f.wrap(pluginName, "world.read.location", f.queryRoomFn(pluginName))))
ls.SetField(mod, "query_character", ls.NewFunction(f.wrap(pluginName, "world.read.character", f.queryCharacterFn(pluginName))))
ls.SetField(mod, "query_room_characters", ls.NewFunction(f.wrap(pluginName, "world.read.character", f.queryRoomCharactersFn(pluginName))))
```

**Step 4: Write tests**

Create `internal/plugin/hostfunc/world_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/world"
)

type mockWorldQuerier struct {
	location   *world.Location
	character  *world.Character
	characters []*world.Character
	err        error
}

func (m *mockWorldQuerier) GetLocation(ctx context.Context, id ulid.ULID) (*world.Location, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.location, nil
}

func (m *mockWorldQuerier) GetCharacter(ctx context.Context, id ulid.ULID) (*world.Character, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.character, nil
}

func (m *mockWorldQuerier) GetCharactersByLocation(ctx context.Context, locationID ulid.ULID) ([]*world.Character, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.characters, nil
}

type mockEnforcerAllow struct{}

func (m *mockEnforcerAllow) Check(plugin, capability string) bool {
	return true
}

func TestQueryRoom(t *testing.T) {
	locID := ulid.Make()
	loc := &world.Location{
		ID:          locID,
		Name:        "Test Room",
		Description: "A test room",
		Type:        world.LocationTypePersistent,
	}

	querier := &mockWorldQuerier{location: loc}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldQuerier(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`
		room, err = holomush.query_room("` + locID.String() + `")
	`)
	require.NoError(t, err)

	room := L.GetGlobal("room")
	require.Equal(t, lua.LTTable, room.Type())

	tbl := room.(*lua.LTable)
	assert.Equal(t, loc.Name, tbl.RawGetString("name").String())
	assert.Equal(t, loc.Description, tbl.RawGetString("description").String())
}
```

**Step 5: Run tests**

Run: `task test -- -v ./internal/plugin/hostfunc/... -run TestQueryRoom`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/plugin/hostfunc/functions.go internal/plugin/hostfunc/world.go internal/plugin/hostfunc/world_test.go
git commit -m "feat(plugin): add world query host functions for Lua plugins"
```

---

## Task 4: Generate CharacterRepository Mock

**Files:**

- Modify: `.mockery.yaml`
- Generate: `internal/world/worldtest/mock_CharacterRepository.go`

**Step 1: Add CharacterRepository to mockery config**

In `.mockery.yaml`, add under the world package section:

```yaml
      CharacterRepository:
        config:
          dir: "{{.InterfaceDir}}/worldtest"
```

**Step 2: Generate mock**

Run: `mockery`

**Step 3: Commit**

```bash
git add .mockery.yaml internal/world/worldtest/mock_CharacterRepository.go
git commit -m "chore(world): generate CharacterRepository mock"
```

---

## Task 5: World Seeding CLI Command

**Files:**

- Create: `cmd/holomush/seed.go`

**Step 1: Write the seed command**

Create `cmd/holomush/seed.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/postgres"
)

var seedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Seed the world with initial data",
	Long: `Creates initial world data including a starting location.
This command is idempotent - it will not create duplicates if run multiple times.`,
	RunE: runSeed,
}

func init() {
	rootCmd.AddCommand(seedCmd)
}

func runSeed(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Connect to database
	pool, err := store.NewPool(ctx, cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer pool.Close()

	// Run migrations
	if err := store.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Create repositories
	locationRepo := postgres.NewLocationRepository(pool)

	// Seed starting location
	startingLoc := &world.Location{
		ID:          ulid.Make(),
		Name:        "The Nexus",
		Description: "A swirling vortex of energy marks the center of the multiverse. Paths branch off in every direction, leading to countless worlds and possibilities. This is where all journeys begin.",
		Type:        world.LocationTypePersistent,
	}

	if err := locationRepo.Create(ctx, startingLoc); err != nil {
		// Check if already exists (idempotent)
		slog.Info("Starting location may already exist", "error", err)
	} else {
		slog.Info("Created starting location", "id", startingLoc.ID, "name", startingLoc.Name)
	}

	fmt.Println("World seeding complete!")
	return nil
}
```

**Step 2: Test manually**

Run: `go build -o holomush ./cmd/holomush && ./holomush seed --help`
Expected: Shows seed command help

**Step 3: Commit**

```bash
git add cmd/holomush/seed.go
git commit -m "feat(cli): add world seed command"
```

---

## Task 6: Plugin Event Emission on World Operations

**Files:**

- Modify: `internal/world/service.go`
- Create: `internal/world/events.go`
- Modify: `internal/world/service_test.go`

**Step 1: Define EventEmitter interface**

Create `internal/world/events.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"encoding/json"

	"github.com/oklog/ulid/v2"
)

// EventEmitter publishes world events.
type EventEmitter interface {
	// Emit publishes an event to the given stream.
	Emit(ctx context.Context, stream string, eventType string, payload []byte) error
}

// EmitMoveEvent emits a move event for character or object movement.
func EmitMoveEvent(ctx context.Context, emitter EventEmitter, payload MovePayload) error {
	if emitter == nil {
		return nil // No emitter configured, skip
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// Emit to destination location stream
	stream := "location:" + payload.ToID
	return emitter.Emit(ctx, stream, "move", data)
}

// EmitObjectCreateEvent emits an object creation event.
func EmitObjectCreateEvent(ctx context.Context, emitter EventEmitter, obj *Object) error {
	if emitter == nil {
		return nil
	}

	payload := map[string]string{
		"object_id":   obj.ID.String(),
		"object_name": obj.Name,
	}
	if obj.Containment.LocationID != nil {
		payload["location_id"] = obj.Containment.LocationID.String()
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	stream := "location:*" // Broadcast to all locations
	if obj.Containment.LocationID != nil {
		stream = "location:" + obj.Containment.LocationID.String()
	}
	return emitter.Emit(ctx, stream, "object_create", data)
}
```

**Step 2: Update Service to accept EventEmitter**

In `internal/world/service.go`, update `ServiceConfig`:

```go
// ServiceConfig holds dependencies for the world service.
type ServiceConfig struct {
	LocationRepo  LocationRepository
	ExitRepo      ExitRepository
	ObjectRepo    ObjectRepository
	SceneRepo     SceneRepository
	CharacterRepo CharacterRepository // Add this
	AccessControl AccessControl
	EventEmitter  EventEmitter // Add this
}
```

**Step 3: Emit events in MoveObject**

Update `MoveObject` to emit events:

```go
func (s *Service) MoveObject(ctx context.Context, subjectID string, id ulid.ULID, to Containment) error {
	// ... existing validation and move logic ...

	// After successful move, emit event
	payload := MovePayload{
		EntityType: EntityTypeObject,
		EntityID:   id.String(),
		FromType:   from.Type(),
		FromID:     from.ID().String(),
		ToType:     to.Type(),
		ToID:       to.ID().String(),
	}

	if err := EmitMoveEvent(ctx, s.events, payload); err != nil {
		slog.Warn("failed to emit move event", "error", err)
		// Don't fail the operation for event emission failure
	}

	return nil
}
```

**Step 4: Write test**

Add to `internal/world/service_test.go`:

```go
func TestService_MoveObject_EmitsEvent(t *testing.T) {
	// Test that MoveObject emits a move event
	// Use mock EventEmitter to verify
}
```

**Step 5: Commit**

```bash
git add internal/world/events.go internal/world/service.go internal/world/service_test.go
git commit -m "feat(world): emit plugin events for world operations"
```

---

## Task 7: Update Epic 4 Acceptance Criteria

**Files:**

- Update: `.beads/` (via bd command)

**Step 1: Verify all acceptance criteria**

Run tests:
- `task test -- ./internal/world/...`
- `task test -- ./internal/plugin/hostfunc/...`

**Step 2: Close Epic 4**

```bash
bd close holomush-x2t --reason="All phases complete: locations, exits, objects, characters, host functions, seeding, events"
```

---

## Post-Implementation Checklist

- [ ] All tests pass: `task test`
- [ ] Linting passes: `task lint`
- [ ] CharacterRepository interface defined and implemented
- [ ] PostgreSQL CharacterRepository works
- [ ] World query host functions (query_room, query_character, query_room_characters) work
- [ ] Seed command creates initial world
- [ ] Move events emitted for object movement
- [ ] Epic 4 acceptance criteria met
- [ ] Documentation updated in site/docs/developers/

---

## Integration Test Checklist

Run integration tests to verify end-to-end:

```bash
task test -- -tags=integration ./test/integration/world/...
```

Verify:
- [ ] Character location updates persist
- [ ] World queries return correct data
- [ ] Move events reach plugin subscribers
