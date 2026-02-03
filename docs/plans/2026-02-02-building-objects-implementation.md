# Building & Objects Plugins Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement building commands (dig, link) as Lua plugin and objects commands (create, set) as Go handlers.

**Architecture:** Two plugins - Lua for topology (dig, link), Go for entity manipulation (create, set). Both use world service via new host functions. Property system supports prefix matching.

**Tech Stack:** Go 1.21+, gopher-lua, world.Service, ULID

**Design:** [docs/plans/2026-02-02-building-objects-plugins-design.md](2026-02-02-building-objects-plugins-design.md)

---

## Task 1: World Write Host Functions

Extend `internal/plugin/hostfunc` with world mutation functions for Lua plugins.

**Files:**

- Create: `internal/plugin/hostfunc/world_write.go`
- Create: `internal/plugin/hostfunc/world_write_test.go`
- Modify: `internal/plugin/hostfunc/functions.go:140-148` (add registrations)

**Step 1: Write failing tests for create_location**

```go
// world_write_test.go
func TestCreateLocationFn_Success(t *testing.T) {
    mockWorld := mocks.NewMockWorldService(t)
    mockEnforcer := mocks.NewMockCapabilityChecker(t)
    mockEnforcer.EXPECT().Check("test-plugin", "world.write.location").Return(true)
    mockWorld.EXPECT().CreateLocation(mock.Anything, "system:plugin:test-plugin", mock.MatchedBy(func(loc *world.Location) bool {
        return loc.Name == "Test Room" && loc.Type == world.LocationTypePersistent
    })).Return(nil)

    f := New(nil, mockEnforcer, WithWorldService(mockWorld))
    L := lua.NewState()
    defer L.Close()
    f.Register(L, "test-plugin")

    err := L.DoString(`result, err = holomush.create_location("Test Room", "A test room", "persistent")`)
    require.NoError(t, err)

    result := L.GetGlobal("result")
    require.Equal(t, lua.LTTable, result.Type())
    tbl := result.(*lua.LTable)
    assert.NotEmpty(t, tbl.RawGetString("id").String())
    assert.Equal(t, "Test Room", tbl.RawGetString("name").String())
}

func TestCreateLocationFn_CapabilityDenied(t *testing.T) {
    mockEnforcer := mocks.NewMockCapabilityChecker(t)
    mockEnforcer.EXPECT().Check("test-plugin", "world.write.location").Return(false)

    f := New(nil, mockEnforcer)
    L := lua.NewState()
    defer L.Close()
    f.Register(L, "test-plugin")

    err := L.DoString(`result, err = holomush.create_location("Test", "", "persistent")`)
    require.Error(t, err)
    assert.Contains(t, err.Error(), "capability denied")
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v ./internal/plugin/hostfunc/... -run TestCreateLocationFn`
Expected: FAIL - function not defined

**Step 3: Implement create_location host function**

```go
// world_write.go
package hostfunc

import (
    "context"
    "errors"
    "log/slog"

    "github.com/oklog/ulid/v2"
    lua "github.com/yuin/gopher-lua"

    "github.com/holomush/holomush/internal/world"
)

// WorldMutator extends WorldService with write operations.
type WorldMutator interface {
    WorldService
    CreateLocation(ctx context.Context, subjectID string, loc *world.Location) error
    CreateExit(ctx context.Context, subjectID string, exit *world.Exit) error
    CreateObject(ctx context.Context, subjectID string, obj *world.Object) error
    UpdateLocation(ctx context.Context, subjectID string, loc *world.Location) error
    UpdateObject(ctx context.Context, subjectID string, obj *world.Object) error
    FindLocationByName(ctx context.Context, subjectID string, name string) (*world.Location, error)
}

// createLocationFn returns a Lua function that creates a new location.
// Lua signature: create_location(name, description, type) -> {id, name} or nil, error
func (f *Functions) createLocationFn(pluginName string) lua.LGFunction {
    return func(L *lua.LState) int {
        if f.worldService == nil {
            L.Push(lua.LNil)
            L.Push(lua.LString("world service not configured"))
            return 2
        }

        mutator, ok := f.worldService.(WorldMutator)
        if !ok {
            L.Push(lua.LNil)
            L.Push(lua.LString("world service does not support mutations"))
            return 2
        }

        name := L.CheckString(1)
        description := L.CheckString(2)
        locTypeStr := L.CheckString(3)

        locType := world.LocationType(locTypeStr)
        if err := locType.Validate(); err != nil {
            L.Push(lua.LNil)
            L.Push(lua.LString("invalid location type: " + locTypeStr))
            return 2
        }

        loc := &world.Location{
            ID:          ulid.Make(),
            Name:        name,
            Description: description,
            Type:        locType,
        }

        ctx, cancel := context.WithTimeout(context.Background(), defaultPluginQueryTimeout)
        defer cancel()

        subjectID := "system:plugin:" + pluginName
        if err := mutator.CreateLocation(ctx, subjectID, loc); err != nil {
            L.Push(lua.LNil)
            L.Push(lua.LString(sanitizeErrorForPlugin(pluginName, "location", name, err)))
            return 2
        }

        result := L.NewTable()
        L.SetField(result, "id", lua.LString(loc.ID.String()))
        L.SetField(result, "name", lua.LString(loc.Name))
        L.Push(result)
        L.Push(lua.LNil)
        return 2
    }
}
```

**Step 4: Register the function in functions.go**

Add after line 145 in `functions.go`:

```go
// World mutations (capability required)
ls.SetField(mod, "create_location", ls.NewFunction(f.wrap(pluginName, "world.write.location", f.createLocationFn(pluginName))))
```

**Step 5: Run tests to verify they pass**

Run: `go test -v ./internal/plugin/hostfunc/... -run TestCreateLocationFn`
Expected: PASS

**Step 6: Write tests for create_exit**

```go
func TestCreateExitFn_Success(t *testing.T) {
    mockWorld := mocks.NewMockWorldService(t)
    mockEnforcer := mocks.NewMockCapabilityChecker(t)
    mockEnforcer.EXPECT().Check("test-plugin", "world.write.exit").Return(true)

    fromID := ulid.Make()
    toID := ulid.Make()
    mockWorld.EXPECT().CreateExit(mock.Anything, "system:plugin:test-plugin", mock.MatchedBy(func(exit *world.Exit) bool {
        return exit.Name == "north" && exit.FromLocationID == fromID && exit.ToLocationID == toID
    })).Return(nil)

    f := New(nil, mockEnforcer, WithWorldService(mockWorld))
    L := lua.NewState()
    defer L.Close()
    f.Register(L, "test-plugin")

    code := fmt.Sprintf(`result, err = holomush.create_exit("%s", "%s", "north", {})`, fromID, toID)
    err := L.DoString(code)
    require.NoError(t, err)

    result := L.GetGlobal("result")
    require.Equal(t, lua.LTTable, result.Type())
}
```

**Step 7: Implement create_exit**

```go
// createExitFn returns a Lua function that creates a new exit.
// Lua signature: create_exit(from_id, to_id, name, opts) -> {id, name} or nil, error
// opts: { bidirectional = true, return_name = "south" }
func (f *Functions) createExitFn(pluginName string) lua.LGFunction {
    return func(L *lua.LState) int {
        if f.worldService == nil {
            L.Push(lua.LNil)
            L.Push(lua.LString("world service not configured"))
            return 2
        }

        mutator, ok := f.worldService.(WorldMutator)
        if !ok {
            L.Push(lua.LNil)
            L.Push(lua.LString("world service does not support mutations"))
            return 2
        }

        fromIDStr := L.CheckString(1)
        toIDStr := L.CheckString(2)
        name := L.CheckString(3)

        fromID, err := ulid.Parse(fromIDStr)
        if err != nil {
            L.Push(lua.LNil)
            L.Push(lua.LString("invalid from_id: " + err.Error()))
            return 2
        }

        toID, err := ulid.Parse(toIDStr)
        if err != nil {
            L.Push(lua.LNil)
            L.Push(lua.LString("invalid to_id: " + err.Error()))
            return 2
        }

        exit := &world.Exit{
            ID:             ulid.Make(),
            FromLocationID: fromID,
            ToLocationID:   toID,
            Name:           name,
            Visibility:     world.VisibilityAll,
        }

        // Parse optional options table
        if L.GetTop() >= 4 && L.Get(4).Type() == lua.LTTable {
            opts := L.ToTable(4)
            if bidir := opts.RawGetString("bidirectional"); bidir.Type() == lua.LTBool {
                exit.Bidirectional = bool(bidir.(lua.LBool))
            }
            if retName := opts.RawGetString("return_name"); retName.Type() == lua.LTString {
                exit.ReturnName = string(retName.(lua.LString))
            }
        }

        ctx, cancel := context.WithTimeout(context.Background(), defaultPluginQueryTimeout)
        defer cancel()

        subjectID := "system:plugin:" + pluginName
        if err := mutator.CreateExit(ctx, subjectID, exit); err != nil {
            L.Push(lua.LNil)
            L.Push(lua.LString(sanitizeErrorForPlugin(pluginName, "exit", name, err)))
            return 2
        }

        result := L.NewTable()
        L.SetField(result, "id", lua.LString(exit.ID.String()))
        L.SetField(result, "name", lua.LString(exit.Name))
        L.Push(result)
        L.Push(lua.LNil)
        return 2
    }
}
```

**Step 8: Write tests for find_location and set_property**

Similar pattern for:

- `find_location(name)` - searches by name
- `set_property(entity_type, entity_id, property, value)` - updates property
- `get_property(entity_type, entity_id, property)` - reads property

**Step 9: Add all registrations to functions.go**

```go
// World mutations (capability required)
ls.SetField(mod, "create_location", ls.NewFunction(f.wrap(pluginName, "world.write.location", f.createLocationFn(pluginName))))
ls.SetField(mod, "create_exit", ls.NewFunction(f.wrap(pluginName, "world.write.exit", f.createExitFn(pluginName))))
ls.SetField(mod, "create_object", ls.NewFunction(f.wrap(pluginName, "world.write.object", f.createObjectFn(pluginName))))
ls.SetField(mod, "find_location", ls.NewFunction(f.wrap(pluginName, "world.read.location", f.findLocationFn(pluginName))))
ls.SetField(mod, "set_property", ls.NewFunction(f.wrap(pluginName, "property.set", f.setPropertyFn(pluginName))))
ls.SetField(mod, "get_property", ls.NewFunction(f.wrap(pluginName, "property.get", f.getPropertyFn(pluginName))))
```

**Step 10: Run all tests**

Run: `go test -v ./internal/plugin/hostfunc/...`
Expected: PASS

**Step 11: Commit**

```bash
git add internal/plugin/hostfunc/world_write.go internal/plugin/hostfunc/world_write_test.go internal/plugin/hostfunc/functions.go
git commit -m "feat(hostfunc): add world mutation host functions for Lua plugins

Add create_location, create_exit, create_object, find_location,
set_property, and get_property host functions. All require
appropriate capabilities.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 2: Property Registry with Prefix Matching

Implement property registry for the `set` command with prefix-based property name matching.

**Files:**

- Create: `pkg/holo/property.go`
- Create: `pkg/holo/property_test.go`

**Step 1: Write failing tests for property registry**

```go
// property_test.go
package holo

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestPropertyRegistry_Resolve_ExactMatch(t *testing.T) {
    r := NewPropertyRegistry()
    r.Register(Property{Name: "description", Type: "text", Capability: "property.set.description"})

    prop, err := r.Resolve("description")
    require.NoError(t, err)
    assert.Equal(t, "description", prop.Name)
}

func TestPropertyRegistry_Resolve_PrefixMatch(t *testing.T) {
    r := NewPropertyRegistry()
    r.Register(Property{Name: "description", Type: "text", Capability: "property.set.description"})

    tests := []struct {
        prefix   string
        expected string
    }{
        {"desc", "description"},
        {"descr", "description"},
        {"descrip", "description"},
    }

    for _, tt := range tests {
        t.Run(tt.prefix, func(t *testing.T) {
            prop, err := r.Resolve(tt.prefix)
            require.NoError(t, err)
            assert.Equal(t, tt.expected, prop.Name)
        })
    }
}

func TestPropertyRegistry_Resolve_Ambiguous(t *testing.T) {
    r := NewPropertyRegistry()
    r.Register(Property{Name: "description", Type: "text"})
    r.Register(Property{Name: "dark_mode", Type: "bool"})

    _, err := r.Resolve("d")
    require.Error(t, err)

    var ambigErr *AmbiguousPropertyError
    require.ErrorAs(t, err, &ambigErr)
    assert.ElementsMatch(t, []string{"dark_mode", "description"}, ambigErr.Matches)
}

func TestPropertyRegistry_Resolve_NotFound(t *testing.T) {
    r := NewPropertyRegistry()
    r.Register(Property{Name: "description", Type: "text"})

    _, err := r.Resolve("xyz")
    require.Error(t, err)
    assert.ErrorIs(t, err, ErrPropertyNotFound)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v ./pkg/holo/... -run TestPropertyRegistry`
Expected: FAIL - types not defined

**Step 3: Implement property registry**

```go
// property.go
package holo

import (
    "errors"
    "fmt"
    "sort"
    "strings"
)

// ErrPropertyNotFound indicates no property matched the given name/prefix.
var ErrPropertyNotFound = errors.New("property not found")

// AmbiguousPropertyError indicates multiple properties match a prefix.
type AmbiguousPropertyError struct {
    Prefix  string
    Matches []string
}

func (e *AmbiguousPropertyError) Error() string {
    sort.Strings(e.Matches)
    return fmt.Sprintf("ambiguous property '%s' - matches: %s", e.Prefix, strings.Join(e.Matches, ", "))
}

// Property defines a settable property on game entities.
type Property struct {
    Name       string   // Full property name (e.g., "description")
    Type       string   // Property type: "string", "text", "number", "bool"
    Capability string   // Required capability to set (e.g., "property.set.description")
    AppliesTo  []string // Entity types this property applies to
}

// PropertyRegistry manages known properties with prefix resolution.
type PropertyRegistry struct {
    properties map[string]Property
}

// NewPropertyRegistry creates an empty property registry.
func NewPropertyRegistry() *PropertyRegistry {
    return &PropertyRegistry{
        properties: make(map[string]Property),
    }
}

// Register adds a property to the registry.
func (r *PropertyRegistry) Register(p Property) {
    r.properties[p.Name] = p
}

// Resolve finds a property by exact name or unique prefix.
// Returns AmbiguousPropertyError if multiple properties match.
// Returns ErrPropertyNotFound if no properties match.
func (r *PropertyRegistry) Resolve(nameOrPrefix string) (Property, error) {
    // Exact match first
    if p, ok := r.properties[nameOrPrefix]; ok {
        return p, nil
    }

    // Prefix matching
    var matches []string
    for name := range r.properties {
        if strings.HasPrefix(name, nameOrPrefix) {
            matches = append(matches, name)
        }
    }

    switch len(matches) {
    case 0:
        return Property{}, ErrPropertyNotFound
    case 1:
        return r.properties[matches[0]], nil
    default:
        return Property{}, &AmbiguousPropertyError{Prefix: nameOrPrefix, Matches: matches}
    }
}

// DefaultRegistry returns a registry with standard properties.
func DefaultRegistry() *PropertyRegistry {
    r := NewPropertyRegistry()
    r.Register(Property{
        Name:       "description",
        Type:       "text",
        Capability: "property.set.description",
        AppliesTo:  []string{"location", "object", "character", "exit"},
    })
    r.Register(Property{
        Name:       "name",
        Type:       "string",
        Capability: "property.set.name",
        AppliesTo:  []string{"location", "object", "exit"},
    })
    return r
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v ./pkg/holo/... -run TestPropertyRegistry`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/holo/property.go pkg/holo/property_test.go
git commit -m "feat(holo): add property registry with prefix matching

Properties can be resolved by exact name or unique prefix.
Ambiguous prefixes return error with all matching property names.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 3: Building Plugin Manifest

Create the Lua plugin manifest for building commands.

**Files:**

- Create: `plugins/building/plugin.yaml`

**Step 1: Create manifest**

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

name: building
version: 1.0.0
type: lua
events:
  - command
capabilities:
  - build.location
  - build.exit
  - world.write.location
  - world.write.exit
  - world.read.location
commands:
  - name: dig
    capabilities:
      - build.location
      - build.exit
    help: "Create a new location with connecting exit"
    usage: "dig <exit> to \"<location>\" [return <exit>]"
    helpText: |
      ## Dig

      Creates a new location and an exit connecting your current location to it.

      ### Syntax

      `dig <exit-name> to "<location-name>" [return <return-exit-name>]`

      ### Examples

      - `dig north to "Town Square"` - Creates "Town Square" with one-way exit "north"
      - `dig north to "Market" return south` - Creates bidirectional exits

      ### Requirements

      Requires `build.location` and `build.exit` capabilities.

  - name: link
    capabilities:
      - build.exit
    help: "Link current location to another with an exit"
    usage: "link <exit> to <target>"
    helpText: |
      ## Link

      Creates an exit from your current location to an existing location.

      ### Syntax

      `link <exit-name> to <target>`

      Target can be:
      - A location ID prefixed with `#` (e.g., `#01HXYZ...`)
      - A location name (must be unique)

      ### Examples

      - `link east to "Garden"` - Link by name
      - `link east to #01HXYZ123ABC` - Link by ID

      ### Requirements

      Requires `build.exit` capability.

lua-plugin:
  entry: main.lua
```

**Step 2: Verify YAML is valid**

Run: `python3 -c "import yaml; yaml.safe_load(open('plugins/building/plugin.yaml'))"`
Expected: No errors

**Step 3: Commit**

```bash
git add plugins/building/plugin.yaml
git commit -m "feat(building): add building plugin manifest

Declares dig and link commands with capabilities and help text.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 4: Building Plugin Lua Handlers

Implement dig and link command handlers in Lua.

**Files:**

- Create: `plugins/building/main.lua`

**Step 1: Implement on_command dispatcher**

```lua
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Building plugin: handles dig, link commands for world topology
-- Uses holo stdlib for event emission and holomush.* for world mutations.

function on_command(ctx)
    if ctx.name == "dig" then
        return handle_dig(ctx)
    elseif ctx.name == "link" then
        return handle_link(ctx)
    end
    return nil
end
```

**Step 2: Implement dig handler**

```lua
-- Parse: dig <exit> to "<location>" [return <exit>]
function parse_dig_args(args)
    -- Pattern: exit_name to "location_name" [return return_exit]
    local exit_name, location_name, return_exit = args:match('^(%S+)%s+to%s+"([^"]+)"')
    if not exit_name or not location_name then
        return nil, "Usage: dig <exit> to \"<location>\" [return <exit>]"
    end

    -- Check for optional return clause
    local remaining = args:match('^%S+%s+to%s+"[^"]+"(.*)$')
    if remaining then
        return_exit = remaining:match('%s+return%s+(%S+)')
    end

    return {
        exit_name = exit_name,
        location_name = location_name,
        return_exit = return_exit
    }, nil
end

function handle_dig(ctx)
    if ctx.args == "" then
        holo.emit.character(ctx.character_id, "error", {
            message = "Usage: dig <exit> to \"<location>\" [return <exit>]"
        })
        return holo.emit.flush()
    end

    local parsed, err = parse_dig_args(ctx.args)
    if not parsed then
        holo.emit.character(ctx.character_id, "error", { message = err })
        return holo.emit.flush()
    end

    -- Create the new location
    local loc, loc_err = holomush.create_location(
        parsed.location_name,
        "", -- empty description initially
        "persistent"
    )
    if not loc then
        holo.emit.character(ctx.character_id, "error", {
            message = "Failed to create location: " .. (loc_err or "unknown error")
        })
        return holo.emit.flush()
    end

    -- Create exit from current location to new location
    local exit_opts = {}
    if parsed.return_exit then
        exit_opts.bidirectional = true
        exit_opts.return_name = parsed.return_exit
    end

    local exit, exit_err = holomush.create_exit(
        ctx.location_id,
        loc.id,
        parsed.exit_name,
        exit_opts
    )
    if not exit then
        holo.emit.character(ctx.character_id, "error", {
            message = "Location created but exit failed: " .. (exit_err or "unknown error")
        })
        return holo.emit.flush()
    end

    -- Success message
    local msg = string.format('Created "%s" with exit "%s"', parsed.location_name, parsed.exit_name)
    if parsed.return_exit then
        msg = msg .. string.format(' and return exit "%s"', parsed.return_exit)
    end
    msg = msg .. "."

    holo.emit.character(ctx.character_id, "system", { message = msg })
    return holo.emit.flush()
end
```

**Step 3: Implement link handler**

```lua
-- Parse: link <exit> to <target>
function parse_link_args(args)
    local exit_name, target = args:match('^(%S+)%s+to%s+(.+)$')
    if not exit_name or not target then
        return nil, "Usage: link <exit> to <target>"
    end

    target = target:match('^%s*(.-)%s*$') -- trim whitespace

    return {
        exit_name = exit_name,
        target = target
    }, nil
end

function resolve_location(target)
    -- If starts with #, treat as ID
    if target:sub(1, 1) == "#" then
        local id = target:sub(2)
        local loc, err = holomush.query_room(id)
        if not loc then
            return nil, "Location not found: " .. id
        end
        return loc, nil
    end

    -- Otherwise, search by name
    local loc, err = holomush.find_location(target)
    if not loc then
        return nil, "Location not found: " .. target
    end
    return loc, nil
end

function handle_link(ctx)
    if ctx.args == "" then
        holo.emit.character(ctx.character_id, "error", {
            message = "Usage: link <exit> to <target>"
        })
        return holo.emit.flush()
    end

    local parsed, err = parse_link_args(ctx.args)
    if not parsed then
        holo.emit.character(ctx.character_id, "error", { message = err })
        return holo.emit.flush()
    end

    -- Resolve target location
    local target_loc, resolve_err = resolve_location(parsed.target)
    if not target_loc then
        holo.emit.character(ctx.character_id, "error", { message = resolve_err })
        return holo.emit.flush()
    end

    -- Create exit
    local exit, exit_err = holomush.create_exit(
        ctx.location_id,
        target_loc.id,
        parsed.exit_name,
        {}
    )
    if not exit then
        holo.emit.character(ctx.character_id, "error", {
            message = "Failed to create exit: " .. (exit_err or "unknown error")
        })
        return holo.emit.flush()
    end

    local msg = string.format('Linked "%s" to "%s"', parsed.exit_name, target_loc.name)
    holo.emit.character(ctx.character_id, "system", { message = msg })
    return holo.emit.flush()
end
```

**Step 4: Run lint check**

Run: `luacheck plugins/building/main.lua`
Expected: No errors

**Step 5: Commit**

```bash
git add plugins/building/main.lua
git commit -m "feat(building): implement dig and link command handlers

- dig <exit> to \"<location>\" [return <exit>]
- link <exit> to <target> (ID or name)

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 5: Objects Plugin (Go Handlers)

Implement create and set commands as Go handlers.

**Files:**

- Create: `internal/command/handlers/objects.go`
- Create: `internal/command/handlers/objects_test.go`
- Modify: `internal/command/registry.go` (register handlers)

**Step 1: Write failing tests**

```go
// objects_test.go
package handlers

import (
    "bytes"
    "context"
    "testing"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/command"
    "github.com/holomush/holomush/internal/world"
    "github.com/holomush/holomush/internal/world/mocks"
)

func TestCreateHandler_Object(t *testing.T) {
    mockWorld := mocks.NewMockWorldService(t)
    mockWorld.EXPECT().CreateObject(mock.Anything, mock.Anything, mock.MatchedBy(func(obj *world.Object) bool {
        return obj.Name == "Iron Sword"
    })).Return(nil)

    var buf bytes.Buffer
    exec := &command.CommandExecution{
        CharacterID: ulid.Make(),
        LocationID:  ulid.Make(),
        Args:        `object "Iron Sword"`,
        Output:      &buf,
        Services:    &command.Services{World: mockWorld},
    }

    err := CreateHandler(context.Background(), exec)
    require.NoError(t, err)
    assert.Contains(t, buf.String(), "Created object")
}

func TestSetHandler_Description(t *testing.T) {
    locID := ulid.Make()
    mockWorld := mocks.NewMockWorldService(t)
    mockWorld.EXPECT().GetLocation(mock.Anything, mock.Anything, locID).Return(&world.Location{
        ID:   locID,
        Name: "Test Room",
    }, nil)
    mockWorld.EXPECT().UpdateLocation(mock.Anything, mock.Anything, mock.MatchedBy(func(loc *world.Location) bool {
        return loc.Description == "A cozy room."
    })).Return(nil)

    var buf bytes.Buffer
    exec := &command.CommandExecution{
        CharacterID: ulid.Make(),
        LocationID:  locID,
        Args:        "description of here to A cozy room.",
        Output:      &buf,
        Services:    &command.Services{World: mockWorld},
    }

    err := SetHandler(context.Background(), exec)
    require.NoError(t, err)
    assert.Contains(t, buf.String(), "Set description")
}

func TestSetHandler_PrefixMatch(t *testing.T) {
    locID := ulid.Make()
    mockWorld := mocks.NewMockWorldService(t)
    mockWorld.EXPECT().GetLocation(mock.Anything, mock.Anything, locID).Return(&world.Location{
        ID:   locID,
        Name: "Test Room",
    }, nil)
    mockWorld.EXPECT().UpdateLocation(mock.Anything, mock.Anything, mock.Anything).Return(nil)

    var buf bytes.Buffer
    exec := &command.CommandExecution{
        CharacterID: ulid.Make(),
        LocationID:  locID,
        Args:        "desc of here to Short description.",
        Output:      &buf,
        Services:    &command.Services{World: mockWorld},
    }

    err := SetHandler(context.Background(), exec)
    require.NoError(t, err)
    // Should resolve "desc" to "description"
    assert.Contains(t, buf.String(), "Set description")
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v ./internal/command/handlers/... -run TestCreate`
Expected: FAIL - handlers not defined

**Step 3: Implement create handler**

```go
// objects.go
package handlers

import (
    "context"
    "fmt"
    "regexp"
    "strings"

    "github.com/oklog/ulid/v2"

    "github.com/holomush/holomush/internal/command"
    "github.com/holomush/holomush/internal/world"
    "github.com/holomush/holomush/pkg/holo"
)

// createPattern matches: create <type> "<name>"
var createPattern = regexp.MustCompile(`^(\w+)\s+"([^"]+)"$`)

// CreateHandler handles the create command.
// Syntax: create <type> "<name>"
// Types: object, location, exit
func CreateHandler(ctx context.Context, exec *command.CommandExecution) error {
    args := strings.TrimSpace(exec.Args)
    if args == "" {
        fmt.Fprintln(exec.Output, "Usage: create <type> \"<name>\"")
        return nil
    }

    matches := createPattern.FindStringSubmatch(args)
    if matches == nil {
        fmt.Fprintln(exec.Output, "Usage: create <type> \"<name>\"")
        return nil
    }

    entityType := strings.ToLower(matches[1])
    name := matches[2]
    subjectID := "char:" + exec.CharacterID.String()

    switch entityType {
    case "object":
        return createObject(ctx, exec, subjectID, name)
    case "location":
        return createLocation(ctx, exec, subjectID, name)
    default:
        fmt.Fprintf(exec.Output, "Unknown type: %s. Use: object, location\n", entityType)
        return nil
    }
}

func createObject(ctx context.Context, exec *command.CommandExecution, subjectID, name string) error {
    obj := &world.Object{
        ID:   ulid.Make(),
        Name: name,
    }
    obj.SetLocation(exec.LocationID)

    if err := exec.Services.World.CreateObject(ctx, subjectID, obj); err != nil {
        fmt.Fprintf(exec.Output, "Failed to create object: %v\n", err)
        return nil
    }

    fmt.Fprintf(exec.Output, "Created object \"%s\" (#%s)\n", name, obj.ID)
    return nil
}

func createLocation(ctx context.Context, exec *command.CommandExecution, subjectID, name string) error {
    loc := &world.Location{
        ID:   ulid.Make(),
        Name: name,
        Type: world.LocationTypePersistent,
    }

    if err := exec.Services.World.CreateLocation(ctx, subjectID, loc); err != nil {
        fmt.Fprintf(exec.Output, "Failed to create location: %v\n", err)
        return nil
    }

    fmt.Fprintf(exec.Output, "Created location \"%s\" (#%s)\n", name, loc.ID)
    return nil
}
```

**Step 4: Implement set handler**

```go
// setPattern matches: set <property> of <target> to <value>
var setPattern = regexp.MustCompile(`^(\w+)\s+of\s+(\S+)\s+to\s+(.+)$`)

// SetHandler handles the set command.
// Syntax: set <property> of <target> to <value>
// Properties support prefix matching (desc -> description).
func SetHandler(ctx context.Context, exec *command.CommandExecution) error {
    args := strings.TrimSpace(exec.Args)
    if args == "" {
        fmt.Fprintln(exec.Output, "Usage: set <property> of <target> to <value>")
        return nil
    }

    matches := setPattern.FindStringSubmatch(args)
    if matches == nil {
        fmt.Fprintln(exec.Output, "Usage: set <property> of <target> to <value>")
        return nil
    }

    propertyPrefix := matches[1]
    target := matches[2]
    value := matches[3]

    // Resolve property with prefix matching
    registry := holo.DefaultRegistry()
    prop, err := registry.Resolve(propertyPrefix)
    if err != nil {
        fmt.Fprintf(exec.Output, "Error: %v\n", err)
        return nil
    }

    // Resolve target
    entityType, entityID, err := resolveTarget(ctx, exec, target)
    if err != nil {
        fmt.Fprintf(exec.Output, "Error: %v\n", err)
        return nil
    }

    // Apply the property change
    if err := applyProperty(ctx, exec, entityType, entityID, prop.Name, value); err != nil {
        fmt.Fprintf(exec.Output, "Error: %v\n", err)
        return nil
    }

    fmt.Fprintf(exec.Output, "Set %s of %s.\n", prop.Name, target)
    return nil
}

func resolveTarget(ctx context.Context, exec *command.CommandExecution, target string) (string, ulid.ULID, error) {
    // "here" -> current location
    if target == "here" {
        return "location", exec.LocationID, nil
    }
    // "me" -> current character
    if target == "me" {
        return "character", exec.CharacterID, nil
    }
    // #<id> -> direct ID reference
    if strings.HasPrefix(target, "#") {
        id, err := ulid.Parse(target[1:])
        if err != nil {
            return "", ulid.ULID{}, fmt.Errorf("invalid ID: %s", target)
        }
        // TODO: determine entity type from ID lookup
        return "object", id, nil
    }
    // Otherwise, search for object by name in current location
    // TODO: implement object search
    return "", ulid.ULID{}, fmt.Errorf("target not found: %s", target)
}

func applyProperty(ctx context.Context, exec *command.CommandExecution, entityType string, entityID ulid.ULID, propName, value string) error {
    subjectID := "char:" + exec.CharacterID.String()

    switch entityType {
    case "location":
        loc, err := exec.Services.World.GetLocation(ctx, subjectID, entityID)
        if err != nil {
            return fmt.Errorf("location not found: %w", err)
        }
        switch propName {
        case "description":
            loc.Description = value
        case "name":
            loc.Name = value
        default:
            return fmt.Errorf("property %s not applicable to location", propName)
        }
        return exec.Services.World.UpdateLocation(ctx, subjectID, loc)

    case "object":
        obj, err := exec.Services.World.GetObject(ctx, subjectID, entityID)
        if err != nil {
            return fmt.Errorf("object not found: %w", err)
        }
        switch propName {
        case "description":
            obj.Description = value
        case "name":
            obj.Name = value
        default:
            return fmt.Errorf("property %s not applicable to object", propName)
        }
        return exec.Services.World.UpdateObject(ctx, subjectID, obj)

    default:
        return fmt.Errorf("cannot set properties on %s", entityType)
    }
}
```

**Step 5: Run tests**

Run: `go test -v ./internal/command/handlers/... -run 'TestCreate|TestSet'`
Expected: PASS

**Step 6: Register handlers in registry**

Add to the command registration (modify appropriate file):

```go
registry.Register(&command.CommandEntry{
    Name:         "create",
    Handler:      handlers.CreateHandler,
    Capabilities: []string{"object.create"},
    Help:         "Create a new entity",
    Usage:        "create <type> \"<name>\"",
    Source:       "core",
})

registry.Register(&command.CommandEntry{
    Name:         "set",
    Handler:      handlers.SetHandler,
    Capabilities: []string{"property.set"},
    Help:         "Set a property on an entity",
    Usage:        "set <property> of <target> to <value>",
    Source:       "core",
})
```

**Step 7: Commit**

```bash
git add internal/command/handlers/objects.go internal/command/handlers/objects_test.go
git commit -m "feat(command): add create and set command handlers

- create <type> \"<name>\" - creates objects, locations
- set <property> of <target> to <value> - with prefix matching

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Task 6: Integration Tests

Write integration tests verifying the full command flow.

**Files:**

- Create: `test/integration/building/building_test.go`

**Step 1: Write integration tests**

```go
//go:build integration

package building_test

import (
    "testing"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
)

func TestBuilding(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "Building Integration Suite")
}

var _ = Describe("Building Commands", func() {
    Describe("dig command", func() {
        It("creates a location and exit", func() {
            // Setup test world, execute dig, verify location and exit created
            Skip("Requires full integration harness")
        })
    })

    Describe("link command", func() {
        It("links to existing location by name", func() {
            Skip("Requires full integration harness")
        })
    })
})

var _ = Describe("Objects Commands", func() {
    Describe("create command", func() {
        It("creates an object in current location", func() {
            Skip("Requires full integration harness")
        })
    })

    Describe("set command", func() {
        It("sets description with prefix matching", func() {
            Skip("Requires full integration harness")
        })
    })
})
```

**Step 2: Run integration tests**

Run: `go test -v -tags=integration ./test/integration/building/...`
Expected: PASS (skipped tests)

**Step 3: Commit**

```bash
git add test/integration/building/
git commit -m "test(integration): add building and objects command tests

Placeholder integration tests for dig, link, create, set commands.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Verification Checklist

Run after all tasks complete:

```bash
# Unit tests
task test

# Lint
task lint

# Build
task build

# Verify new files exist
ls -la plugins/building/
ls -la internal/plugin/hostfunc/world_write.go
ls -la pkg/holo/property.go
ls -la internal/command/handlers/objects.go
```

Expected: All pass, all files present.
