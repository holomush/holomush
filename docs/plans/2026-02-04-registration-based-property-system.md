# Registration-Based Property System Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace hardcoded switch statements in hostfunc/world_write.go with a registration-based property system using PropertyDefinition interface.

**Architecture:** Create PropertyDefinition interface with Validate(), Get(), Set() methods. Properties register themselves at init time via a PropertyRegistry. Host functions look up properties by name and delegate to registered handlers, eliminating the need for hardcoded switches.

**Tech Stack:** Go, Lua (gopher-lua), existing holo.PropertyRegistry from pkg/holo/property.go

---

## Context: Current State

The problematic code in `internal/plugin/hostfunc/world_write.go:274-338` has hardcoded switch statements:

```go
// getEntityProperty - switch on entity type, then switch on property
func getEntityProperty(ctx context.Context, adapter *WorldQuerierAdapter, opts *propertyOpts) (string, error) {
    switch opts.entityType {
    case "location":
        loc, err := adapter.GetLocation(ctx, opts.entityID)
        // ...
        switch opts.property {
        case "name":
            return loc.Name, nil
        case "description":
            return loc.Description, nil
        }
    case "object":
        obj, err := adapter.GetObject(ctx, opts.entityID)
        // ...
        switch opts.property {
        case "name":
            return obj.Name, nil
        case "description":
            return obj.Description, nil
        }
    }
    return "", nil
}
```

This violates the open/closed principle - adding new properties requires editing multiple switch statements.

---

## Task 1: Define PropertyDefinition Interface

**Files:**

- Create: `internal/plugin/hostfunc/property_definition.go`
- Test: `internal/plugin/hostfunc/property_definition_test.go`

**Step 1: Write the failing test**

Create `internal/plugin/hostfunc/property_definition_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
    "context"
    "testing"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/world"
)

// Mock adapter for testing
type mockPropertyAdapter struct {
    location *world.Location
    object   *world.Object
    character *world.Character
    exit     *world.Exit
}

func (m *mockPropertyAdapter) GetLocation(ctx context.Context, id ulid.ULID) (*world.Location, error) {
    return m.location, nil
}

func (m *mockPropertyAdapter) GetObject(ctx context.Context, id ulid.ULID) (*world.Object, error) {
    return m.object, nil
}

func (m *mockPropertyAdapter) GetCharacter(ctx context.Context, id ulid.ULID) (*world.Character, error) {
    return m.character, nil
}

func (m *mockPropertyAdapter) GetExit(ctx context.Context, id ulid.ULID) (*world.Exit, error) {
    return m.exit, nil
}

func TestPropertyDefinitionInterfaceExists(t *testing.T) {
    // Test that PropertyDefinition interface is defined with required methods
    var _ PropertyDefinition = (*mockPropertyDef)(nil)
}

type mockPropertyDef struct {
    name       string
    entityTypes []string
}

func (m *mockPropertyDef) Name() string {
    return m.name
}

func (m *mockPropertyDef) AppliesTo(entityType string) bool {
    for _, et := range m.entityTypes {
        if et == entityType {
            return true
        }
    }
    return false
}

func (m *mockPropertyDef) Validate(value string) error {
    return nil
}

func (m *mockPropertyDef) Get(ctx context.Context, adapter WorldQuerier, entityType string, entityID ulid.ULID) (string, error) {
    return "test-value", nil
}

func (m *mockPropertyDef) Set(ctx context.Context, adapter WorldQuerier, mutator WorldMutator, subjectID string, entityType string, entityID ulid.ULID, value string) error {
    return nil
}

func TestPropertyDefinition_Name(t *testing.T) {
    def := &mockPropertyDef{name: "test-property"}
    assert.Equal(t, "test-property", def.Name())
}

func TestPropertyDefinition_AppliesTo(t *testing.T) {
    def := &mockPropertyDef{
        name:        "test",
        entityTypes: []string{"location", "object"},
    }

    assert.True(t, def.AppliesTo("location"))
    assert.True(t, def.AppliesTo("object"))
    assert.False(t, def.AppliesTo("character"))
    assert.False(t, def.AppliesTo("exit"))
}
```

**Step 2: Run test to verify it fails**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test -v ./internal/plugin/hostfunc/... -run TestPropertyDefinition
```

Expected: FAIL with "PropertyDefinition not defined" or similar compilation error.

**Step 3: Implement PropertyDefinition interface**

Create `internal/plugin/hostfunc/property_definition.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
    "context"

    "github.com/oklog/ulid/v2"
)

// PropertyDefinition defines a property that can be get/set on entities.
// Implementations register themselves at init time to handle specific properties.
type PropertyDefinition interface {
    // Name returns the property name (e.g., "name", "description")
    Name() string

    // AppliesTo returns true if this property applies to the given entity type
    AppliesTo(entityType string) bool

    // Validate checks if the value is valid for this property
    Validate(value string) error

    // Get retrieves the property value from an entity
    Get(ctx context.Context, adapter WorldQuerier, entityType string, entityID ulid.ULID) (string, error)

    // Set updates the property value on an entity
    Set(ctx context.Context, adapter WorldQuerier, mutator WorldMutator, subjectID string, entityType string, entityID ulid.ULID, value string) error
}
```

**Step 4: Run test to verify it passes**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test -v ./internal/plugin/hostfunc/... -run TestPropertyDefinition
```

Expected: PASS

**Step 5: Commit**

```bash
cd /Volumes/Code/github.com/holomush/holomush
git add internal/plugin/hostfunc/property_definition.go internal/plugin/hostfunc/property_definition_test.go
git commit -m "feat(property): define PropertyDefinition interface

Add PropertyDefinition interface with Name(), AppliesTo(), Validate(),
Get(), and Set() methods. This is the foundation for the registration-based
property system that will replace hardcoded switch statements."
```

---

## Task 2: Create PropertyRegistry for Definitions

**Files:**

- Create: `internal/plugin/hostfunc/property_registry.go`
- Test: `internal/plugin/hostfunc/property_registry_test.go`

**Step 1: Write the failing test**

Create `internal/plugin/hostfunc/property_registry_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestPropertyRegistry_New(t *testing.T) {
    r := NewPropertyDefinitionRegistry()
    require.NotNil(t, r)
    assert.NotNil(t, r.definitions)
}

func TestPropertyRegistry_RegisterAndGet(t *testing.T) {
    r := NewPropertyDefinitionRegistry()

    def := &mockPropertyDef{
        name:        "test-prop",
        entityTypes: []string{"location"},
    }

    // Register should succeed
    err := r.Register(def)
    require.NoError(t, err)

    // Get should return the definition
    got, ok := r.Get("test-prop")
    assert.True(t, ok)
    assert.Equal(t, def, got)
}

func TestPropertyRegistry_RegisterDuplicate(t *testing.T) {
    r := NewPropertyDefinitionRegistry()

    def1 := &mockPropertyDef{name: "test", entityTypes: []string{"location"}}
    def2 := &mockPropertyDef{name: "test", entityTypes: []string{"object"}}

    err := r.Register(def1)
    require.NoError(t, err)

    err = r.Register(def2)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "already registered")
}

func TestPropertyRegistry_Get_NotFound(t *testing.T) {
    r := NewPropertyDefinitionRegistry()

    got, ok := r.Get("nonexistent")
    assert.False(t, ok)
    assert.Nil(t, got)
}

func TestPropertyRegistry_ValidFor(t *testing.T) {
    r := NewPropertyDefinitionRegistry()

    // Register a property for locations only
    r.Register(&mockPropertyDef{
        name:        "location-only",
        entityTypes: []string{"location"},
    })

    // Register a property for multiple types
    r.Register(&mockPropertyDef{
        name:        "universal",
        entityTypes: []string{"location", "object", "character"},
    })

    tests := []struct {
        name       string
        entityType string
        property   string
        want       bool
    }{
        {"location-only on location", "location", "location-only", true},
        {"location-only on object", "object", "location-only", false},
        {"universal on location", "location", "universal", true},
        {"universal on object", "object", "universal", true},
        {"nonexistent property", "location", "missing", false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := r.ValidFor(tt.entityType, tt.property)
            assert.Equal(t, tt.want, got)
        })
    }
}

func TestPropertyRegistry_GetAll(t *testing.T) {
    r := NewPropertyDefinitionRegistry()

    def1 := &mockPropertyDef{name: "prop1", entityTypes: []string{"location"}}
    def2 := &mockPropertyDef{name: "prop2", entityTypes: []string{"object"}}

    r.Register(def1)
    r.Register(def2)

    all := r.GetAll()
    assert.Len(t, all, 2)
    assert.Contains(t, all, def1)
    assert.Contains(t, all, def2)
}
```

**Step 2: Run test to verify it fails**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test -v ./internal/plugin/hostfunc/... -run TestPropertyRegistry
```

Expected: FAIL with "NewPropertyDefinitionRegistry not defined" or similar.

**Step 3: Implement PropertyRegistry**

Create `internal/plugin/hostfunc/property_registry.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
    "fmt"
    "sync"
)

// PropertyDefinitionRegistry manages property definitions with thread-safe access.
type PropertyDefinitionRegistry struct {
    mu          sync.RWMutex
    definitions map[string]PropertyDefinition
}

// NewPropertyDefinitionRegistry creates a new empty registry.
func NewPropertyDefinitionRegistry() *PropertyDefinitionRegistry {
    return &PropertyDefinitionRegistry{
        definitions: make(map[string]PropertyDefinition),
    }
}

// Register adds a property definition to the registry.
// Returns an error if a property with the same name is already registered.
func (r *PropertyDefinitionRegistry) Register(def PropertyDefinition) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    name := def.Name()
    if _, exists := r.definitions[name]; exists {
        return fmt.Errorf("property %q already registered", name)
    }

    r.definitions[name] = def
    return nil
}

// Get retrieves a property definition by name.
// Returns (nil, false) if not found.
func (r *PropertyDefinitionRegistry) Get(name string) (PropertyDefinition, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    def, ok := r.definitions[name]
    return def, ok
}

// ValidFor checks if a property is valid for a given entity type.
// Returns false if the property doesn't exist or doesn't apply to the entity type.
func (r *PropertyDefinitionRegistry) ValidFor(entityType, propertyName string) bool {
    r.mu.RLock()
    defer r.mu.RUnlock()

    def, ok := r.definitions[propertyName]
    if !ok {
        return false
    }

    return def.AppliesTo(entityType)
}

// GetAll returns all registered property definitions.
// Returns a defensive copy to prevent external modification.
func (r *PropertyDefinitionRegistry) GetAll() []PropertyDefinition {
    r.mu.RLock()
    defer r.mu.RUnlock()

    result := make([]PropertyDefinition, 0, len(r.definitions))
    for _, def := range r.definitions {
        result = append(result, def)
    }
    return result
}

// MustRegister registers a property definition, panicking on error.
// Use only during package initialization.
func (r *PropertyDefinitionRegistry) MustRegister(def PropertyDefinition) {
    if err := r.Register(def); err != nil {
        panic(err)
    }
}
```

**Step 4: Run test to verify it passes**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test -v ./internal/plugin/hostfunc/... -run TestPropertyRegistry
```

Expected: PASS

**Step 5: Commit**

```bash
cd /Volumes/Code/github.com/holomush/holomush
git add internal/plugin/hostfunc/property_registry.go internal/plugin/hostfunc/property_registry_test.go
git commit -m "feat(property): create PropertyDefinitionRegistry

Add thread-safe registry for PropertyDefinition instances with Register,
Get, ValidFor, and GetAll methods. Supports MustRegister for init-time
registration with panic on duplicate."
```

---

## Task 3: Implement Name Property Definition

**Files:**

- Create: `internal/plugin/hostfunc/properties.go`
- Test: `internal/plugin/hostfunc/properties_test.go`

**Step 1: Write the failing test**

Create `internal/plugin/hostfunc/properties_test.go` with name property tests:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
    "context"
    "testing"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/world"
)

// nameProperty tests

func TestNameProperty_Name(t *testing.T) {
    p := &nameProperty{}
    assert.Equal(t, "name", p.Name())
}

func TestNameProperty_AppliesTo(t *testing.T) {
    p := &nameProperty{}

    // Should apply to location, object, exit
    assert.True(t, p.AppliesTo("location"))
    assert.True(t, p.AppliesTo("object"))
    assert.True(t, p.AppliesTo("exit"))

    // Should NOT apply to character (per existing behavior in DefaultRegistry)
    assert.False(t, p.AppliesTo("character"))

    // Should not apply to unknown types
    assert.False(t, p.AppliesTo("unknown"))
}

func TestNameProperty_Validate(t *testing.T) {
    p := &nameProperty{}

    // Valid names
    assert.NoError(t, p.Validate("Test Room"))
    assert.NoError(t, p.Validate("Sword"))
    assert.NoError(t, p.Validate("A"))

    // Empty name should be invalid
    assert.Error(t, p.Validate(""))

    // Whitespace-only should be invalid
    assert.Error(t, p.Validate("   "))
    assert.Error(t, p.Validate("\t\n"))
}

func TestNameProperty_Get_Location(t *testing.T) {
    locID := ulid.Make()
    loc := &world.Location{
        ID:   locID,
        Name: "Test Room",
    }

    adapter := &mockPropertyAdapter{location: loc}
    p := &nameProperty{}

    value, err := p.Get(context.Background(), adapter, "location", locID)
    require.NoError(t, err)
    assert.Equal(t, "Test Room", value)
}

func TestNameProperty_Get_Object(t *testing.T) {
    objID := ulid.Make()
    locID := ulid.Make()
    obj, err := world.NewObjectWithID(objID, "Magic Sword", world.InLocation(locID))
    require.NoError(t, err)

    adapter := &mockPropertyAdapter{object: obj}
    p := &nameProperty{}

    value, err := p.Get(context.Background(), adapter, "object", objID)
    require.NoError(t, err)
    assert.Equal(t, "Magic Sword", value)
}

func TestNameProperty_Get_UnsupportedEntity(t *testing.T) {
    p := &nameProperty{}
    adapter := &mockPropertyAdapter{}
    id := ulid.Make()

    _, err := p.Get(context.Background(), adapter, "character", id)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "unsupported entity type")
}
```

**Step 2: Run test to verify it fails**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test -v ./internal/plugin/hostfunc/... -run TestNameProperty
```

Expected: FAIL with "nameProperty not defined" or similar.

**Step 3: Implement name property**

Create `internal/plugin/hostfunc/properties.go` with name property:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
    "context"
    "errors"
    "fmt"
    "strings"

    "github.com/oklog/ulid/v2"
)

// nameProperty implements the "name" property for locations, objects, and exits.
type nameProperty struct{}

// Name returns the property name.
func (p *nameProperty) Name() string {
    return "name"
}

// AppliesTo returns true for entity types that support the name property.
func (p *nameProperty) AppliesTo(entityType string) bool {
    switch entityType {
    case "location", "object", "exit":
        return true
    default:
        return false
    }
}

// Validate checks that the name is not empty or whitespace-only.
func (p *nameProperty) Validate(value string) error {
    if strings.TrimSpace(value) == "" {
        return errors.New("name cannot be empty")
    }
    return nil
}

// Get retrieves the name from an entity.
func (p *nameProperty) Get(ctx context.Context, adapter WorldQuerier, entityType string, entityID ulid.ULID) (string, error) {
    switch entityType {
    case "location":
        loc, err := adapter.GetLocation(ctx, entityID)
        if err != nil {
            return "", err
        }
        return loc.Name, nil
    case "object":
        obj, err := adapter.GetObject(ctx, entityID)
        if err != nil {
            return "", err
        }
        return obj.Name, nil
    case "exit":
        exit, err := adapter.GetExit(ctx, entityID)
        if err != nil {
            return "", err
        }
        return exit.Name, nil
    default:
        return "", fmt.Errorf("unsupported entity type for name property: %s", entityType)
    }
}

// Set updates the name on an entity.
func (p *nameProperty) Set(ctx context.Context, adapter WorldQuerier, mutator WorldMutator, subjectID string, entityType string, entityID ulid.ULID, value string) error {
    // Validate first
    if err := p.Validate(value); err != nil {
        return err
    }

    switch entityType {
    case "location":
        loc, err := adapter.GetLocation(ctx, entityID)
        if err != nil {
            return err
        }
        loc.Name = value
        return mutator.UpdateLocation(ctx, subjectID, loc)
    case "object":
        obj, err := adapter.GetObject(ctx, entityID)
        if err != nil {
            return err
        }
        obj.Name = value
        return mutator.UpdateObject(ctx, subjectID, obj)
    case "exit":
        exit, err := adapter.GetExit(ctx, entityID)
        if err != nil {
            return err
        }
        exit.Name = value
        return mutator.UpdateExit(ctx, subjectID, exit)
    default:
        return fmt.Errorf("unsupported entity type for name property: %s", entityType)
    }
}
```

**Step 4: Run test to verify it passes**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test -v ./internal/plugin/hostfunc/... -run TestNameProperty
```

Expected: PASS

**Step 5: Commit**

```bash
cd /Volumes/Code/github.com/holomush/holomush
git add internal/plugin/hostfunc/properties.go internal/plugin/hostfunc/properties_test.go
git commit -m "feat(property): implement name property definition

Add nameProperty implementing PropertyDefinition interface for
locations, objects, and exits. Supports Get, Set, and Validate
with proper error handling for each entity type."
```

---

## Task 4: Implement Description Property Definition

**Files:**

- Modify: `internal/plugin/hostfunc/properties.go` (add description property)
- Modify: `internal/plugin/hostfunc/properties_test.go` (add tests)

**Step 1: Write the failing test**

Add to `internal/plugin/hostfunc/properties_test.go`:

```go
// descriptionProperty tests

func TestDescriptionProperty_Name(t *testing.T) {
    p := &descriptionProperty{}
    assert.Equal(t, "description", p.Name())
}

func TestDescriptionProperty_AppliesTo(t *testing.T) {
    p := &descriptionProperty{}

    // Should apply to location, object, character, exit (per DefaultRegistry)
    assert.True(t, p.AppliesTo("location"))
    assert.True(t, p.AppliesTo("object"))
    assert.True(t, p.AppliesTo("character"))
    assert.True(t, p.AppliesTo("exit"))

    // Should not apply to unknown types
    assert.False(t, p.AppliesTo("unknown"))
}

func TestDescriptionProperty_Validate(t *testing.T) {
    p := &descriptionProperty{}

    // Description can be empty
    assert.NoError(t, p.Validate(""))

    // Description can have content
    assert.NoError(t, p.Validate("A test description"))
    assert.NoError(t, p.Validate("Multi\nline\ndescription"))
}

func TestDescriptionProperty_Get_Location(t *testing.T) {
    locID := ulid.Make()
    loc := &world.Location{
        ID:          locID,
        Name:        "Test Room",
        Description: "A cozy room with a fireplace",
    }

    adapter := &mockPropertyAdapter{location: loc}
    p := &descriptionProperty{}

    value, err := p.Get(context.Background(), adapter, "location", locID)
    require.NoError(t, err)
    assert.Equal(t, "A cozy room with a fireplace", value)
}

func TestDescriptionProperty_Get_Object(t *testing.T) {
    objID := ulid.Make()
    locID := ulid.Make()
    obj, err := world.NewObjectWithID(objID, "Magic Sword", world.InLocation(locID))
    require.NoError(t, err)
    obj.Description = "A gleaming blade"

    adapter := &mockPropertyAdapter{object: obj}
    p := &descriptionProperty{}

    value, err := p.Get(context.Background(), adapter, "object", objID)
    require.NoError(t, err)
    assert.Equal(t, "A gleaming blade", value)
}

func TestDescriptionProperty_Get_Character(t *testing.T) {
    charID := ulid.Make()
    locID := ulid.Make()
    char := world.NewCharacter(charID, "TestCharacter", locID)
    char.Description = "A brave adventurer"

    adapter := &mockPropertyAdapter{character: char}
    p := &descriptionProperty{}

    value, err := p.Get(context.Background(), adapter, "character", charID)
    require.NoError(t, err)
    assert.Equal(t, "A brave adventurer", value)
}

func TestDescriptionProperty_Get_Exit(t *testing.T) {
    exitID := ulid.Make()
    fromID := ulid.Make()
    toID := ulid.Make()
    exit := world.NewExit(exitID, "north", fromID, toID)
    exit.Description = "A winding path north"

    adapter := &mockPropertyAdapter{exit: exit}
    p := &descriptionProperty{}

    value, err := p.Get(context.Background(), adapter, "exit", exitID)
    require.NoError(t, err)
    assert.Equal(t, "A winding path north", value)
}

func TestDescriptionProperty_Get_EmptyDescription(t *testing.T) {
    locID := ulid.Make()
    loc := &world.Location{
        ID:          locID,
        Name:        "Test Room",
        Description: "", // Empty is valid
    }

    adapter := &mockPropertyAdapter{location: loc}
    p := &descriptionProperty{}

    value, err := p.Get(context.Background(), adapter, "location", locID)
    require.NoError(t, err)
    assert.Equal(t, "", value)
}
```

**Step 2: Run test to verify it fails**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test -v ./internal/plugin/hostfunc/... -run TestDescriptionProperty
```

Expected: FAIL with "descriptionProperty not defined" or similar.

**Step 3: Implement description property**

Add to `internal/plugin/hostfunc/properties.go`:

```go
// descriptionProperty implements the "description" property for locations, objects, characters, and exits.
type descriptionProperty struct{}

// Name returns the property name.
func (p *descriptionProperty) Name() string {
    return "description"
}

// AppliesTo returns true for entity types that support the description property.
func (p *descriptionProperty) AppliesTo(entityType string) bool {
    switch entityType {
    case "location", "object", "character", "exit":
        return true
    default:
        return false
    }
}

// Validate allows any value (including empty) for descriptions.
func (p *descriptionProperty) Validate(value string) error {
    // Description can be empty or any string
    return nil
}

// Get retrieves the description from an entity.
func (p *descriptionProperty) Get(ctx context.Context, adapter WorldQuerier, entityType string, entityID ulid.ULID) (string, error) {
    switch entityType {
    case "location":
        loc, err := adapter.GetLocation(ctx, entityID)
        if err != nil {
            return "", err
        }
        return loc.Description, nil
    case "object":
        obj, err := adapter.GetObject(ctx, entityID)
        if err != nil {
            return "", err
        }
        return obj.Description, nil
    case "character":
        char, err := adapter.GetCharacter(ctx, entityID)
        if err != nil {
            return "", err
        }
        return char.Description, nil
    case "exit":
        exit, err := adapter.GetExit(ctx, entityID)
        if err != nil {
            return "", err
        }
        return exit.Description, nil
    default:
        return "", fmt.Errorf("unsupported entity type for description property: %s", entityType)
    }
}

// Set updates the description on an entity.
func (p *descriptionProperty) Set(ctx context.Context, adapter WorldQuerier, mutator WorldMutator, subjectID string, entityType string, entityID ulid.ULID, value string) error {
    // No validation needed for description
    switch entityType {
    case "location":
        loc, err := adapter.GetLocation(ctx, entityID)
        if err != nil {
            return err
        }
        loc.Description = value
        return mutator.UpdateLocation(ctx, subjectID, loc)
    case "object":
        obj, err := adapter.GetObject(ctx, entityID)
        if err != nil {
            return err
        }
        obj.Description = value
        return mutator.UpdateObject(ctx, subjectID, obj)
    case "character":
        char, err := adapter.GetCharacter(ctx, entityID)
        if err != nil {
            return err
        }
        char.Description = value
        return mutator.UpdateCharacter(ctx, subjectID, char)
    case "exit":
        exit, err := adapter.GetExit(ctx, entityID)
        if err != nil {
            return err
        }
        exit.Description = value
        return mutator.UpdateExit(ctx, subjectID, exit)
    default:
        return fmt.Errorf("unsupported entity type for description property: %s", entityType)
    }
}
```

**Step 4: Run test to verify it passes**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test -v ./internal/plugin/hostfunc/... -run TestDescriptionProperty
```

Expected: PASS

**Step 5: Commit**

```bash
cd /Volumes/Code/github.com/holomush/holomush
git add internal/plugin/hostfunc/properties.go internal/plugin/hostfunc/properties_test.go
git commit -m "feat(property): implement description property definition

Add descriptionProperty implementing PropertyDefinition interface
for locations, objects, characters, and exits. Description accepts
any value including empty strings."
```

---

## Task 5: Create Default Property Registry

**Files:**

- Modify: `internal/plugin/hostfunc/properties.go` (add init and DefaultPropertyRegistry)
- Modify: `internal/plugin/hostfunc/properties_test.go` (add registry tests)

**Step 1: Write the failing test**

Add to `internal/plugin/hostfunc/properties_test.go`:

```go
// Default registry tests

func TestDefaultPropertyRegistry_Exists(t *testing.T) {
    // The registry should be accessible
    require.NotNil(t, DefaultPropertyRegistry)
}

func TestDefaultPropertyRegistry_ContainsName(t *testing.T) {
    def, ok := DefaultPropertyRegistry.Get("name")
    assert.True(t, ok, "name property should be registered")
    assert.NotNil(t, def)
    assert.Equal(t, "name", def.Name())
}

func TestDefaultPropertyRegistry_ContainsDescription(t *testing.T) {
    def, ok := DefaultPropertyRegistry.Get("description")
    assert.True(t, ok, "description property should be registered")
    assert.NotNil(t, def)
    assert.Equal(t, "description", def.Name())
}

func TestDefaultPropertyRegistry_ValidFor(t *testing.T) {
    // name property applies to location, object, exit
    assert.True(t, DefaultPropertyRegistry.ValidFor("location", "name"))
    assert.True(t, DefaultPropertyRegistry.ValidFor("object", "name"))
    assert.True(t, DefaultPropertyRegistry.ValidFor("exit", "name"))
    assert.False(t, DefaultPropertyRegistry.ValidFor("character", "name"))

    // description property applies to all four entity types
    assert.True(t, DefaultPropertyRegistry.ValidFor("location", "description"))
    assert.True(t, DefaultPropertyRegistry.ValidFor("object", "description"))
    assert.True(t, DefaultPropertyRegistry.ValidFor("character", "description"))
    assert.True(t, DefaultPropertyRegistry.ValidFor("exit", "description"))
}

func TestDefaultPropertyRegistry_InvalidProperties(t *testing.T) {
    // Unknown properties
    assert.False(t, DefaultPropertyRegistry.ValidFor("location", "unknown"))
    assert.False(t, DefaultPropertyRegistry.ValidFor("object", "invalid"))

    // Invalid entity types
    assert.False(t, DefaultPropertyRegistry.ValidFor("unknown", "name"))
    assert.False(t, DefaultPropertyRegistry.ValidFor("", "description"))
}
```

**Step 2: Run test to verify it fails**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test -v ./internal/plugin/hostfunc/... -run TestDefaultPropertyRegistry
```

Expected: FAIL with "DefaultPropertyRegistry not defined" or similar.

**Step 3: Implement default registry**

Add to `internal/plugin/hostfunc/properties.go` at the end:

```go
// DefaultPropertyRegistry is the global registry of property definitions.
// It is initialized with the standard properties (name, description).
var DefaultPropertyRegistry *PropertyDefinitionRegistry

func init() {
    DefaultPropertyRegistry = NewPropertyDefinitionRegistry()
    DefaultPropertyRegistry.MustRegister(&nameProperty{})
    DefaultPropertyRegistry.MustRegister(&descriptionProperty{})
}
```

**Step 4: Run test to verify it passes**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test -v ./internal/plugin/hostfunc/... -run TestDefaultPropertyRegistry
```

Expected: PASS

**Step 5: Commit**

```bash
cd /Volumes/Code/github.com/holomush/holomush
git add internal/plugin/hostfunc/properties.go internal/plugin/hostfunc/properties_test.go
git commit -m "feat(property): create default property definition registry

Initialize DefaultPropertyRegistry with name and description
property definitions at package init time."
```

---

## Task 6: Refactor getEntityProperty to Use Registration System

**Files:**

- Modify: `internal/plugin/hostfunc/world_write.go:274-301` (getEntityProperty)

**Step 1: Understand current getEntityProperty**

Current code (lines 274-301):

```go
// getEntityProperty retrieves a property value from an entity.
func getEntityProperty(ctx context.Context, adapter *WorldQuerierAdapter, opts *propertyOpts) (string, error) {
    switch opts.entityType {
    case "location":
        loc, err := adapter.GetLocation(ctx, opts.entityID)
        if err != nil {
            return "", err
        }
        switch opts.property {
        case "name":
            return loc.Name, nil
        case "description":
            return loc.Description, nil
        }
    case "object":
        obj, err := adapter.GetObject(ctx, opts.entityID)
        if err != nil {
            return "", err
        }
        switch opts.property {
        case "name":
            return obj.Name, nil
        case "description":
            return obj.Description, nil
        }
    }
    return "", nil
}
```

**Step 2: Refactor to use registry**

Replace the entire `getEntityProperty` function in `internal/plugin/hostfunc/world_write.go`:

```go
// getEntityProperty retrieves a property value from an entity using the property registry.
func getEntityProperty(ctx context.Context, adapter *WorldQuerierAdapter, opts *propertyOpts) (string, error) {
    // Look up the property definition in the registry
    def, ok := DefaultPropertyRegistry.Get(opts.property)
    if !ok {
        return "", fmt.Errorf("unknown property: %s", opts.property)
    }

    // Delegate to the property definition's Get method
    return def.Get(ctx, adapter, opts.entityType, opts.entityID)
}
```

**Step 3: Verify all existing tests still pass**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test -v ./internal/plugin/hostfunc/... -run "TestGetProperty|TestNameProperty|TestDescriptionProperty"
```

Expected: PASS

**Step 4: Run the full test suite for the package**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test ./internal/plugin/hostfunc/...
```

Expected: PASS

**Step 5: Commit**

```bash
cd /Volumes/Code/github.com/holomush/holomush
git add internal/plugin/hostfunc/world_write.go
git commit -m "refactor(property): use registry for getEntityProperty

Replace hardcoded switch statements with registry lookup.
getEntityProperty now delegates to registered PropertyDefinition's
Get() method, eliminating the need for entity-specific switches."
```

---

## Task 7: Refactor setEntityProperty to Use Registration System

**Files:**

- Modify: `internal/plugin/hostfunc/world_write.go:303-338` (setEntityProperty)

**Step 1: Understand current setEntityProperty**

Current code (lines 303-338):

```go
// setEntityProperty sets a property value on an entity.
func setEntityProperty(ctx context.Context, adapter *WorldQuerierAdapter,
    mutator WorldMutator, subjectID string, opts *propertyOpts,
    value string) error {
    switch opts.entityType {
    case "location":
        loc, err := adapter.GetLocation(ctx, opts.entityID)
        if err != nil {
            return err
        }
        switch opts.property {
        case "name":
            loc.Name = value
        case "description":
            loc.Description = value
        }
        if err := mutator.UpdateLocation(ctx, subjectID, loc); err != nil {
            return oops.Wrapf(err, "update location %s", opts.entityID)
        }
        return nil
    case "object":
        obj, err := adapter.GetObject(ctx, opts.entityID)
        if err != nil {
            return err
        }
        switch opts.property {
        case "name":
            obj.Name = value
        case "description":
            obj.Description = value
        }
        if err := mutator.UpdateObject(ctx, subjectID, obj); err != nil {
            return oops.Wrapf(err, "update object %s", opts.entityID)
        }
        return nil
    }
    return nil
}
```

**Step 2: Refactor to use registry**

Replace the entire `setEntityProperty` function in `internal/plugin/hostfunc/world_write.go`:

```go
// setEntityProperty sets a property value on an entity using the property registry.
func setEntityProperty(ctx context.Context, adapter *WorldQuerierAdapter, mutator WorldMutator, subjectID string, opts *propertyOpts, value string) error {
    // Look up the property definition in the registry
    def, ok := DefaultPropertyRegistry.Get(opts.property)
    if !ok {
        return fmt.Errorf("unknown property: %s", opts.property)
    }

    // Validate the value first
    if err := def.Validate(value); err != nil {
        return err
    }

    // Delegate to the property definition's Set method
    return def.Set(ctx, adapter, mutator, subjectID, opts.entityType, opts.entityID, value)
}
```

**Step 3: Verify all existing tests still pass**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test -v ./internal/plugin/hostfunc/... -run "TestSetProperty|TestNameProperty|TestDescriptionProperty"
```

Expected: PASS

**Step 4: Run the full test suite for the package**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test ./internal/plugin/hostfunc/...
```

Expected: PASS

**Step 5: Commit**

```bash
cd /Volumes/Code/github.com/holomush/holomush
git add internal/plugin/hostfunc/world_write.go
git commit -m "refactor(property): use registry for setEntityProperty

Replace hardcoded switch statements with registry lookup.
setEntityProperty now delegates to registered PropertyDefinition's
Validate() and Set() methods, eliminating entity-specific switches."
```

---

## Task 8: Update Property Validation in validatePropertyArgs

**Files:**

- Modify: `internal/plugin/hostfunc/world_write.go:261-264` (property validation)

**Step 1: Review current validation**

Current validation (lines 260-264):

```go
// Validate property using PropertyRegistry
if !propertyRegistry.ValidFor(entityType, property) {
    pushError(L, "invalid property: "+property+" for "+entityType)
    return nil, false
}
```

This uses `propertyRegistry` from `pkg/holo` (metadata registry). We should update it to use our new `DefaultPropertyRegistry` from `internal/plugin/hostfunc` (definition registry).

**Step 2: Update validation to use DefaultPropertyRegistry**

Change the validation in `validatePropertyArgs` (line 261):

```go
// Validate property using DefaultPropertyRegistry
if !DefaultPropertyRegistry.ValidFor(entityType, property) {
    pushError(L, "invalid property: "+property+" for "+entityType)
    return nil, false
}
```

**Step 3: Consider removing the old propertyRegistry variable**

Check if `propertyRegistry` (from pkg/holo) is used anywhere else in world_write.go:

```bash
cd /Volumes/Code/github.com/holomush/holomush
grep -n "propertyRegistry" internal/plugin/hostfunc/world_write.go
```

If it's only used in validatePropertyArgs, we can remove the variable declaration at line 21:

```go
// Remove this:
// propertyRegistry is used to validate properties for entity types.
// Uses the default registry from pkg/holo which defines standard properties.
var propertyRegistry = holo.DefaultRegistry()
```

And remove the import of `github.com/holomush/holomush/pkg/holo` if it's no longer needed.

**Step 4: Run tests to verify everything works**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test ./internal/plugin/hostfunc/...
```

Expected: PASS

**Step 5: Commit**

```bash
cd /Volumes/Code/github.com/holomush/holomush
git add internal/plugin/hostfunc/world_write.go
git commit -m "refactor(property): use DefaultPropertyRegistry for validation

Update validatePropertyArgs to use the new DefaultPropertyRegistry
instead of the metadata-only propertyRegistry from pkg/holo. This
ensures validation is consistent with the definition-based system."
```

---

## Task 9: Remove Old propertyRegistry Variable (if unused)

**Files:**

- Modify: `internal/plugin/hostfunc/world_write.go:19-21` (remove variable)

**Step 1: Check if propertyRegistry is used elsewhere**

```bash
cd /Volumes/Code/github.com/holomush/holomush
grep -n "propertyRegistry" internal/plugin/hostfunc/world_write.go
```

**Step 2: Remove if unused**

If the only reference is in validatePropertyArgs (which we just updated), remove lines 19-21:

```go
// Remove these lines:
// propertyRegistry is used to validate properties for entity types.
// Uses the default registry from pkg/holo which defines standard properties.
var propertyRegistry = holo.DefaultRegistry()
```

**Step 3: Check if holo import is still needed**

```bash
cd /Volumes/Code/github.com/holomush/holomush
grep -n '"github.com/holomush/holomush/pkg/holo"' internal/plugin/hostfunc/world_write.go
```

If the only use was for propertyRegistry, remove the import:

```go
// In the import block, remove:
// "github.com/holomush/holomush/pkg/holo"
```

**Step 4: Verify tests still pass**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test ./internal/plugin/hostfunc/...
```

Expected: PASS

**Step 5: Commit**

```bash
cd /Volumes/Code/github.com/holomush/holomush
git add internal/plugin/hostfunc/world_write.go
git commit -m "cleanup(property): remove unused propertyRegistry variable

Remove the metadata-only propertyRegistry variable and its import
now that we use DefaultPropertyRegistry for all validation."
```

---

## Task 10: Verify All Existing Tests Pass

**Files:**

- Test: `internal/plugin/hostfunc/world_write_test.go`

**Step 1: Run all tests in the package**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test -v ./internal/plugin/hostfunc/...
```

Expected: All tests PASS

**Step 2: Run specific property-related tests**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test -v ./internal/plugin/hostfunc/... -run "Property|property"
```

Expected: All property tests PASS

**Step 3: Run integration-style tests for get/set property**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test -v ./internal/plugin/hostfunc/... -run "TestGetProperty|TestSetProperty"
```

Expected: All tests PASS

**Step 4: Commit (if all pass)**

```bash
cd /Volumes/Code/github.com/holomush/holomush
git commit -m "test(property): verify all tests pass with new registry system

All existing tests pass with the registration-based property system.
The hardcoded switch statements have been successfully replaced with
delegation to registered PropertyDefinition instances."
```

---

## Task 11: Self-Review and Final Verification

**Step 1: Review changes for code quality**

```bash
cd /Volumes/Code/github.com/holomush/holomush
git diff --stat HEAD~10
```

Check that:

- All new files have proper SPDX license headers
- All new files have copyright comments
- Code follows Go conventions
- No dead code or commented-out code
- Proper error handling throughout

**Step 2: Check test coverage**

```bash
cd /Volumes/Code/github.com/holomush/holomush
go test -cover ./internal/plugin/hostfunc/...
```

Expected: Coverage for new files should be reasonable (>70%)

**Step 3: Run linting if available**

```bash
cd /Volumes/Code/github.com/holomush/holomush
make lint 2>/dev/null || golangci-lint run ./internal/plugin/hostfunc/... 2>/dev/null || echo "No linter configured"
```

**Step 4: Final commit summary**

```bash
cd /Volumes/Code/github.com/holomush/holomush
git log --oneline HEAD~10..
```

Verify commit history is clean and logical.

---

## Summary of Changes

### Files Created

1. `internal/plugin/hostfunc/property_definition.go` - PropertyDefinition interface
2. `internal/plugin/hostfunc/property_definition_test.go` - Interface tests
3. `internal/plugin/hostfunc/property_registry.go` - PropertyDefinitionRegistry
4. `internal/plugin/hostfunc/property_registry_test.go` - Registry tests
5. `internal/plugin/hostfunc/properties.go` - Property implementations (name, description)
6. `internal/plugin/hostfunc/properties_test.go` - Property tests

### Files Modified

1. `internal/plugin/hostfunc/world_write.go`:
   - Refactored `getEntityProperty` to use registry (replaced hardcoded switch)
   - Refactored `setEntityProperty` to use registry (replaced hardcoded switch)
   - Updated validation to use DefaultPropertyRegistry
   - Removed old propertyRegistry variable and import

### Architecture Change

**Before:**

```text
get_property("location", id, "name")
  -> getEntityProperty
    -> switch entityType
      -> switch property
        -> return value
```

**After:**

```text
get_property("location", id, "name")
  -> getEntityProperty
    -> DefaultPropertyRegistry.Get("name")
      -> nameProperty.Get(adapter, "location", id)
        -> return value
```

### Benefits

1. **Open/Closed Principle:** Add new properties without modifying existing code
2. **Single Responsibility:** Each property defines its own behavior
3. **Testability:** Individual properties can be tested in isolation
4. **Extensibility:** New entity types and properties are easy to add

---

## Appendix: Adding a New Property (Example)

To add a "visible" property for locations and objects:

```go
// In a new file or properties.go

type visibleProperty struct{}

func (p *visibleProperty) Name() string { return "visible" }
func (p *visibleProperty) AppliesTo(entityType string) bool {
    return entityType == "location" || entityType == "object"
}
func (p *visibleProperty) Validate(value string) error {
    if value != "true" && value != "false" {
        return errors.New("visible must be 'true' or 'false'")
    }
    return nil
}
func (p *visibleProperty) Get(ctx context.Context, adapter WorldQuerier, entityType string, entityID ulid.ULID) (string, error) {
    // Implementation...
}
func (p *visibleProperty) Set(ctx context.Context, adapter WorldQuerier, mutator WorldMutator, subjectID string, entityType string, entityID ulid.ULID, value string) error {
    // Implementation...
}

// In init()
func init() {
    DefaultPropertyRegistry.MustRegister(&visibleProperty{})
}
```

No changes needed to `world_write.go`!
