# Plugin System Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement a two-tier plugin system with Lua for simple scripts and go-plugin for complex extensions.

**Architecture:** Lua scripts run in-process via gopher-lua with sandboxed state (fresh per event). Heavy plugins run as gRPC subprocesses via HashiCorp go-plugin. Both share a unified capability model and host function API.

**Tech Stack:** gopher-lua, go-plugin (gRPC), YAML manifests, JSON Schema validation

**Design Spec:** [docs/specs/2026-01-18-plugin-system-design.md](../specs/2026-01-18-plugin-system-design.md)

---

## Phase Overview

| Phase | Bead ID          | Deliverable                   | Files Created                                                              |
| ----- | ---------------- | ----------------------------- | -------------------------------------------------------------------------- |
| 2.1   | `holomush-1hq.3` | Lua runtime integration       | `internal/plugin/lua/host.go`, `state.go`                                  |
| 2.2   | `holomush-1hq.4` | Plugin discovery & lifecycle  | `internal/plugin/manager.go`, `manifest.go`                                |
| 2.3   | `holomush-1hq.5` | Event subscription & delivery | `internal/plugin/subscriber.go`                                            |
| 2.4   | `holomush-1hq.6` | Host functions                | `internal/plugin/hostfunc/functions.go`                                    |
| 2.5   | `holomush-1hq.7` | Capability model              | `internal/plugin/capability/enforcer.go`                                   |
| 2.6   | `holomush-1hq.8` | go-plugin integration         | `internal/plugin/goplugin/host.go`, `api/proto/holomush/plugin/v1/*.proto` |
| 2.7   | `holomush-1hq.9` | Echo bot in Lua               | `plugins/echo-bot/plugin.yaml`, `main.lua`                                 |

---

## Task 1: Capability Enforcer (Phase 2.5 - Foundation)

**Rationale:** Build capability enforcement first because it's a dependency for host functions and has no dependencies itself.

**Files:**

- Create: `internal/plugin/capability/enforcer.go`
- Create: `internal/plugin/capability/enforcer_test.go`

### Step 1: Write failing tests for capability matching

```go
// internal/plugin/capability/enforcer_test.go
package capability_test

import (
    "testing"

    "github.com/holomush/holomush/internal/plugin/capability"
)

func TestCapabilityEnforcer_Check(t *testing.T) {
    tests := []struct {
        name       string
        grants     []string
        capability string
        want       bool
    }{
        {
            name:       "exact match",
            grants:     []string{"world.read.location"},
            capability: "world.read.location",
            want:       true,
        },
        {
            name:       "wildcard suffix matches child",
            grants:     []string{"world.read.*"},
            capability: "world.read.location",
            want:       true,
        },
        {
            name:       "wildcard suffix matches nested",
            grants:     []string{"world.*"},
            capability: "world.read.location",
            want:       true,
        },
        {
            name:       "no match returns false",
            grants:     []string{"world.read.character"},
            capability: "world.read.location",
            want:       false,
        },
        {
            name:       "empty grants returns false",
            grants:     []string{},
            capability: "world.read.location",
            want:       false,
        },
        {
            name:       "partial match not allowed",
            grants:     []string{"world.read"},
            capability: "world.read.location",
            want:       false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            e := capability.NewEnforcer()
            e.SetGrants("test-plugin", tt.grants)

            got := e.Check("test-plugin", tt.capability)
            if got != tt.want {
                t.Errorf("Check() = %v, want %v", got, tt.want)
            }
        })
    }
}

func TestCapabilityEnforcer_Check_UnknownPlugin(t *testing.T) {
    e := capability.NewEnforcer()
    if e.Check("unknown", "any.capability") {
        t.Error("Check() should return false for unknown plugin")
    }
}
```

### Step 2: Run test to verify it fails

```bash
task test -- -run TestCapabilityEnforcer ./internal/plugin/capability/...
```

Expected: Compilation error (package doesn't exist)

### Step 3: Write minimal implementation

```go
// internal/plugin/capability/enforcer.go
package capability

import (
    "strings"
    "sync"
)

// Enforcer checks plugin capabilities at runtime.
type Enforcer struct {
    grants map[string][]string // plugin name -> granted capabilities
    mu     sync.RWMutex
}

// NewEnforcer creates a capability enforcer.
func NewEnforcer() *Enforcer {
    return &Enforcer{
        grants: make(map[string][]string),
    }
}

// SetGrants configures capabilities for a plugin.
func (e *Enforcer) SetGrants(plugin string, capabilities []string) {
    e.mu.Lock()
    defer e.mu.Unlock()
    e.grants[plugin] = capabilities
}

// Check returns true if the plugin has the requested capability.
func (e *Enforcer) Check(plugin, capability string) bool {
    e.mu.RLock()
    defer e.mu.RUnlock()

    grants, ok := e.grants[plugin]
    if !ok {
        return false
    }

    for _, grant := range grants {
        if matchCapability(grant, capability) {
            return true
        }
    }
    return false
}

// matchCapability handles wildcard matching.
// "world.read.*" matches "world.read.location" and "world.read.character.name".
func matchCapability(grant, requested string) bool {
    if grant == requested {
        return true
    }
    if strings.HasSuffix(grant, ".*") {
        prefix := strings.TrimSuffix(grant, "*")
        return strings.HasPrefix(requested, prefix)
    }
    return false
}
```

### Step 4: Run test to verify it passes

```bash
task test -- -run TestCapabilityEnforcer ./internal/plugin/capability/...
```

Expected: PASS

### Step 5: Commit

```bash
git add internal/plugin/capability/
git commit -m "feat(plugin): add capability enforcer with wildcard matching

Implements the foundation of the capability model per the plugin system
design spec. Supports exact matches and wildcard suffix patterns.

Part of Epic 2: Plugin System (holomush-1hq)"
```

---

## Task 2: Plugin Manifest Parsing (Phase 2.2)

**Files:**

- Create: `internal/plugin/manifest.go`
- Create: `internal/plugin/manifest_test.go`
- Create: `schemas/plugin.schema.json`

### Step 1: Write failing tests for manifest parsing

```go
// internal/plugin/manifest_test.go
package plugin_test

import (
    "testing"

    "github.com/holomush/holomush/internal/plugin"
)

func TestParseManifest_LuaPlugin(t *testing.T) {
    yaml := `
name: echo-bot
version: 1.0.0
type: lua
events:
  - say
  - pose
capabilities:
  - events.emit.location
lua-plugin:
  entry: main.lua
`
    m, err := plugin.ParseManifest([]byte(yaml))
    if err != nil {
        t.Fatalf("ParseManifest() error = %v", err)
    }

    if m.Name != "echo-bot" {
        t.Errorf("Name = %q, want %q", m.Name, "echo-bot")
    }
    if m.Version != "1.0.0" {
        t.Errorf("Version = %q, want %q", m.Version, "1.0.0")
    }
    if m.Type != plugin.TypeLua {
        t.Errorf("Type = %v, want %v", m.Type, plugin.TypeLua)
    }
    if len(m.Events) != 2 {
        t.Errorf("len(Events) = %d, want 2", len(m.Events))
    }
    if m.LuaPlugin == nil || m.LuaPlugin.Entry != "main.lua" {
        t.Errorf("LuaPlugin.Entry not set correctly")
    }
}

func TestParseManifest_BinaryPlugin(t *testing.T) {
    yaml := `
name: combat-system
version: 2.1.0
type: binary
events:
  - combat_start
capabilities:
  - events.*
  - world.*
binary-plugin:
  executable: combat-${os}-${arch}
`
    m, err := plugin.ParseManifest([]byte(yaml))
    if err != nil {
        t.Fatalf("ParseManifest() error = %v", err)
    }

    if m.Type != plugin.TypeBinary {
        t.Errorf("Type = %v, want %v", m.Type, plugin.TypeBinary)
    }
    if m.BinaryPlugin == nil || m.BinaryPlugin.Executable != "combat-${os}-${arch}" {
        t.Errorf("BinaryPlugin.Executable not set correctly")
    }
}

func TestParseManifest_InvalidName(t *testing.T) {
    yaml := `
name: Invalid_Name
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`
    _, err := plugin.ParseManifest([]byte(yaml))
    if err == nil {
        t.Error("expected error for invalid name")
    }
}

func TestParseManifest_MissingLuaPlugin(t *testing.T) {
    yaml := `
name: test
version: 1.0.0
type: lua
`
    _, err := plugin.ParseManifest([]byte(yaml))
    if err == nil {
        t.Error("expected error when lua-plugin missing for type: lua")
    }
}
```

### Step 2: Run test to verify it fails

```bash
task test -- -run TestParseManifest ./internal/plugin/...
```

Expected: Compilation error

### Step 3: Write implementation

```go
// internal/plugin/manifest.go
package plugin

import (
    "fmt"
    "regexp"

    "gopkg.in/yaml.v3"
)

// PluginType identifies the plugin runtime.
type PluginType string

const (
    TypeLua    PluginType = "lua"
    TypeBinary PluginType = "binary"
)

// Manifest represents a plugin.yaml file.
type Manifest struct {
    Name         string       `yaml:"name"`
    Version      string       `yaml:"version"`
    Type         PluginType   `yaml:"type"`
    Events       []string     `yaml:"events,omitempty"`
    Capabilities []string     `yaml:"capabilities,omitempty"`
    LuaPlugin    *LuaConfig   `yaml:"lua-plugin,omitempty"`
    BinaryPlugin *BinaryConfig `yaml:"binary-plugin,omitempty"`
}

// LuaConfig holds Lua-specific configuration.
type LuaConfig struct {
    Entry string `yaml:"entry"`
}

// BinaryConfig holds binary plugin configuration.
type BinaryConfig struct {
    Executable string `yaml:"executable"`
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// ParseManifest parses and validates a plugin.yaml file.
func ParseManifest(data []byte) (*Manifest, error) {
    var m Manifest
    if err := yaml.Unmarshal(data, &m); err != nil {
        return nil, fmt.Errorf("invalid YAML: %w", err)
    }

    if err := m.Validate(); err != nil {
        return nil, err
    }

    return &m, nil
}

// Validate checks manifest constraints.
func (m *Manifest) Validate() error {
    if !namePattern.MatchString(m.Name) {
        return fmt.Errorf("name %q must match pattern ^[a-z][a-z0-9-]*$", m.Name)
    }

    if m.Version == "" {
        return fmt.Errorf("version is required")
    }

    switch m.Type {
    case TypeLua:
        if m.LuaPlugin == nil {
            return fmt.Errorf("lua-plugin is required when type is lua")
        }
        if m.LuaPlugin.Entry == "" {
            return fmt.Errorf("lua-plugin.entry is required")
        }
    case TypeBinary:
        if m.BinaryPlugin == nil {
            return fmt.Errorf("binary-plugin is required when type is binary")
        }
        if m.BinaryPlugin.Executable == "" {
            return fmt.Errorf("binary-plugin.executable is required")
        }
    default:
        return fmt.Errorf("type must be 'lua' or 'binary', got %q", m.Type)
    }

    return nil
}
```

### Step 4: Run test to verify it passes

```bash
task test -- -run TestParseManifest ./internal/plugin/...
```

Expected: PASS

### Step 5: Commit

```bash
git add internal/plugin/manifest.go internal/plugin/manifest_test.go
git commit -m "feat(plugin): add manifest parsing with validation

Parses plugin.yaml files with validation for name patterns, required
fields, and type-specific configuration (lua-plugin vs binary-plugin).

Part of Epic 2: Plugin System (holomush-1hq)"
```

---

## Task 3: Lua StateFactory (Phase 2.1)

**Files:**

- Create: `internal/plugin/lua/state.go`
- Create: `internal/plugin/lua/state_test.go`

### Step 1: Write failing tests for state factory

```go
// internal/plugin/lua/state_test.go
package lua_test

import (
    "context"
    "testing"

    pluginlua "github.com/holomush/holomush/internal/plugin/lua"
)

func TestStateFactory_NewState_LoadsSafeLibraries(t *testing.T) {
    factory := pluginlua.NewStateFactory()
    L, err := factory.NewState(context.Background())
    if err != nil {
        t.Fatalf("NewState() error = %v", err)
    }
    defer L.Close()

    // Should have base, table, string, math
    safeLibs := []string{"table", "string", "math"}
    for _, lib := range safeLibs {
        if L.GetGlobal(lib).Type().String() == "nil" {
            t.Errorf("library %q not loaded", lib)
        }
    }
}

func TestStateFactory_NewState_BlocksUnsafeLibraries(t *testing.T) {
    factory := pluginlua.NewStateFactory()
    L, err := factory.NewState(context.Background())
    if err != nil {
        t.Fatalf("NewState() error = %v", err)
    }
    defer L.Close()

    // Should NOT have os, io, debug, package
    unsafeLibs := []string{"os", "io", "debug", "package"}
    for _, lib := range unsafeLibs {
        if L.GetGlobal(lib).Type().String() != "nil" {
            t.Errorf("unsafe library %q should not be loaded", lib)
        }
    }
}

func TestStateFactory_NewState_CanExecuteLua(t *testing.T) {
    factory := pluginlua.NewStateFactory()
    L, err := factory.NewState(context.Background())
    if err != nil {
        t.Fatalf("NewState() error = %v", err)
    }
    defer L.Close()

    err = L.DoString(`result = 1 + 1`)
    if err != nil {
        t.Fatalf("DoString() error = %v", err)
    }

    result := L.GetGlobal("result")
    if result.String() != "2" {
        t.Errorf("result = %v, want 2", result)
    }
}
```

### Step 2: Run test to verify it fails

```bash
task test -- -run TestStateFactory ./internal/plugin/lua/...
```

Expected: Compilation error

### Step 3: Write implementation

```go
// internal/plugin/lua/state.go
package lua

import (
    "context"
    "fmt"

    lua "github.com/yuin/gopher-lua"
)

// StateFactory creates sandboxed Lua states.
type StateFactory struct{}

// NewStateFactory creates a new state factory.
func NewStateFactory() *StateFactory {
    return &StateFactory{}
}

// NewState creates a fresh Lua state with only safe libraries loaded.
func (f *StateFactory) NewState(_ context.Context) (*lua.LState, error) {
    L := lua.NewState(lua.Options{
        SkipOpenLibs: true, // Don't load any libraries by default
    })

    // Load only safe libraries
    safeLibs := []struct {
        name string
        fn   lua.LGFunction
    }{
        {lua.BaseLibName, lua.OpenBase},
        {lua.TabLibName, lua.OpenTable},
        {lua.StringLibName, lua.OpenString},
        {lua.MathLibName, lua.OpenMath},
    }

    for _, lib := range safeLibs {
        if err := L.CallByParam(lua.P{
            Fn:      L.NewFunction(lib.fn),
            NRet:    0,
            Protect: true,
        }, lua.LString(lib.name)); err != nil {
            L.Close()
            return nil, fmt.Errorf("failed to open library %s: %w", lib.name, err)
        }
    }

    return L, nil
}
```

### Step 4: Run test to verify it passes

```bash
task test -- -run TestStateFactory ./internal/plugin/lua/...
```

Expected: PASS

### Step 5: Add gopher-lua dependency

```bash
go get github.com/yuin/gopher-lua
```

### Step 6: Commit

```bash
git add internal/plugin/lua/ go.mod go.sum
git commit -m "feat(plugin): add sandboxed Lua state factory

Creates fresh Lua states with only safe libraries (base, table, string,
math). Blocks os, io, debug, and package libraries for security.

Part of Epic 2: Plugin System (holomush-1hq)"
```

---

## Task 4: LuaHost Implementation (Phase 2.1)

**Files:**

- Create: `internal/plugin/lua/host.go`
- Create: `internal/plugin/lua/host_test.go`
- Create: `internal/plugin/host.go` (interface)

### Step 1: Define PluginHost interface

```go
// internal/plugin/host.go
package plugin

import (
    "context"

    pluginpkg "github.com/holomush/holomush/pkg/plugin"
)

// Host manages a specific plugin runtime type.
type Host interface {
    // Load initializes a plugin from its manifest.
    Load(ctx context.Context, manifest *Manifest, dir string) error

    // Unload tears down a plugin.
    Unload(ctx context.Context, name string) error

    // DeliverEvent sends an event to a plugin and returns response events.
    DeliverEvent(ctx context.Context, name string, event pluginpkg.Event) ([]pluginpkg.EmitEvent, error)

    // Plugins returns names of all loaded plugins.
    Plugins() []string

    // Close shuts down the host and all plugins.
    Close(ctx context.Context) error
}
```

### Step 2: Write failing tests for LuaHost

```go
// internal/plugin/lua/host_test.go
package lua_test

import (
    "context"
    "os"
    "path/filepath"
    "testing"

    "github.com/holomush/holomush/internal/plugin"
    pluginlua "github.com/holomush/holomush/internal/plugin/lua"
    pluginpkg "github.com/holomush/holomush/pkg/plugin"
)

func TestLuaHost_Load(t *testing.T) {
    dir := t.TempDir()

    // Create test plugin
    mainLua := `
function on_event(event)
    return nil
end
`
    if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(mainLua), 0644); err != nil {
        t.Fatal(err)
    }

    host := pluginlua.NewHost()
    defer host.Close(context.Background())

    manifest := &plugin.Manifest{
        Name:    "test-plugin",
        Version: "1.0.0",
        Type:    plugin.TypeLua,
        LuaPlugin: &plugin.LuaConfig{
            Entry: "main.lua",
        },
    }

    err := host.Load(context.Background(), manifest, dir)
    if err != nil {
        t.Fatalf("Load() error = %v", err)
    }

    plugins := host.Plugins()
    if len(plugins) != 1 || plugins[0] != "test-plugin" {
        t.Errorf("Plugins() = %v, want [test-plugin]", plugins)
    }
}

func TestLuaHost_DeliverEvent_ReturnsEmitEvents(t *testing.T) {
    dir := t.TempDir()

    mainLua := `
function on_event(event)
    if event.type == "say" then
        return {
            {
                stream = event.stream,
                type = "say",
                payload = '{"message":"Echo: ' .. event.payload .. '"}'
            }
        }
    end
    return nil
end
`
    if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(mainLua), 0644); err != nil {
        t.Fatal(err)
    }

    host := pluginlua.NewHost()
    defer host.Close(context.Background())

    manifest := &plugin.Manifest{
        Name:      "echo",
        Version:   "1.0.0",
        Type:      plugin.TypeLua,
        LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
    }

    if err := host.Load(context.Background(), manifest, dir); err != nil {
        t.Fatal(err)
    }

    event := pluginpkg.Event{
        ID:        "01ABC",
        Stream:    "location:123",
        Type:      "say",
        Timestamp: 1705591234000,
        ActorKind: pluginpkg.ActorCharacter,
        ActorID:   "char_1",
        Payload:   "Hello",
    }

    emits, err := host.DeliverEvent(context.Background(), "echo", event)
    if err != nil {
        t.Fatalf("DeliverEvent() error = %v", err)
    }

    if len(emits) != 1 {
        t.Fatalf("len(emits) = %d, want 1", len(emits))
    }

    if emits[0].Stream != "location:123" {
        t.Errorf("emit.Stream = %q, want %q", emits[0].Stream, "location:123")
    }
}

func TestLuaHost_DeliverEvent_NoHandler(t *testing.T) {
    dir := t.TempDir()

    // Plugin without on_event function
    mainLua := `x = 1`
    if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(mainLua), 0644); err != nil {
        t.Fatal(err)
    }

    host := pluginlua.NewHost()
    defer host.Close(context.Background())

    manifest := &plugin.Manifest{
        Name:      "no-handler",
        Version:   "1.0.0",
        Type:      plugin.TypeLua,
        LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
    }

    if err := host.Load(context.Background(), manifest, dir); err != nil {
        t.Fatal(err)
    }

    event := pluginpkg.Event{ID: "01ABC", Type: "say"}
    emits, err := host.DeliverEvent(context.Background(), "no-handler", event)
    if err != nil {
        t.Fatalf("DeliverEvent() error = %v", err)
    }

    if len(emits) != 0 {
        t.Errorf("expected no emits for plugin without handler")
    }
}
```

### Step 3: Run test to verify it fails

```bash
task test -- -run TestLuaHost ./internal/plugin/lua/...
```

Expected: Compilation error

### Step 4: Write LuaHost implementation

```go
// internal/plugin/lua/host.go
package lua

import (
    "context"
    "fmt"
    "os"
    "path/filepath"
    "sync"

    "github.com/holomush/holomush/internal/plugin"
    pluginpkg "github.com/holomush/holomush/pkg/plugin"
    lua "github.com/yuin/gopher-lua"
)

// luaPlugin holds compiled Lua code for a plugin.
type luaPlugin struct {
    manifest *plugin.Manifest
    code     string // Lua source (compiled at load time in future)
}

// Host manages Lua plugins.
type Host struct {
    factory *StateFactory
    plugins map[string]*luaPlugin
    mu      sync.RWMutex
    closed  bool
}

// NewHost creates a new Lua plugin host.
func NewHost() *Host {
    return &Host{
        factory: NewStateFactory(),
        plugins: make(map[string]*luaPlugin),
    }
}

// Load reads and validates a Lua plugin.
func (h *Host) Load(ctx context.Context, manifest *plugin.Manifest, dir string) error {
    h.mu.Lock()
    defer h.mu.Unlock()

    if h.closed {
        return fmt.Errorf("host is closed")
    }

    entryPath := filepath.Join(dir, manifest.LuaPlugin.Entry)
    code, err := os.ReadFile(entryPath)
    if err != nil {
        return fmt.Errorf("failed to read %s: %w", entryPath, err)
    }

    // Validate syntax by compiling in a throwaway state
    L, err := h.factory.NewState(ctx)
    if err != nil {
        return fmt.Errorf("failed to create validation state: %w", err)
    }
    defer L.Close()

    if err := L.DoString(string(code)); err != nil {
        return fmt.Errorf("syntax error in %s: %w", manifest.LuaPlugin.Entry, err)
    }

    h.plugins[manifest.Name] = &luaPlugin{
        manifest: manifest,
        code:     string(code),
    }

    return nil
}

// Unload removes a plugin.
func (h *Host) Unload(_ context.Context, name string) error {
    h.mu.Lock()
    defer h.mu.Unlock()

    if _, ok := h.plugins[name]; !ok {
        return fmt.Errorf("plugin %s not loaded", name)
    }
    delete(h.plugins, name)
    return nil
}

// DeliverEvent executes the plugin's on_event function.
func (h *Host) DeliverEvent(ctx context.Context, name string, event pluginpkg.Event) ([]pluginpkg.EmitEvent, error) {
    h.mu.RLock()
    p, ok := h.plugins[name]
    if !ok {
        h.mu.RUnlock()
        return nil, fmt.Errorf("plugin %s not loaded", name)
    }
    code := p.code
    h.mu.RUnlock()

    // Create fresh state for this event
    L, err := h.factory.NewState(ctx)
    if err != nil {
        return nil, fmt.Errorf("failed to create state: %w", err)
    }
    defer L.Close()

    // Load plugin code
    if err := L.DoString(code); err != nil {
        return nil, fmt.Errorf("failed to load plugin code: %w", err)
    }

    // Check if on_event exists
    onEvent := L.GetGlobal("on_event")
    if onEvent.Type() == lua.LTNil {
        return nil, nil // No handler
    }

    // Build event table
    eventTable := h.buildEventTable(L, event)

    // Call on_event(event)
    if err := L.CallByParam(lua.P{
        Fn:      onEvent,
        NRet:    1,
        Protect: true,
    }, eventTable); err != nil {
        return nil, fmt.Errorf("on_event failed: %w", err)
    }

    // Get return value
    ret := L.Get(-1)
    L.Pop(1)

    return h.parseEmitEvents(ret)
}

// Plugins returns names of loaded plugins.
func (h *Host) Plugins() []string {
    h.mu.RLock()
    defer h.mu.RUnlock()

    names := make([]string, 0, len(h.plugins))
    for name := range h.plugins {
        names = append(names, name)
    }
    return names
}

// Close shuts down the host.
func (h *Host) Close(_ context.Context) error {
    h.mu.Lock()
    defer h.mu.Unlock()
    h.closed = true
    h.plugins = nil
    return nil
}

func (h *Host) buildEventTable(L *lua.LState, event pluginpkg.Event) *lua.LTable {
    t := L.NewTable()
    L.SetField(t, "id", lua.LString(event.ID))
    L.SetField(t, "stream", lua.LString(event.Stream))
    L.SetField(t, "type", lua.LString(string(event.Type)))
    L.SetField(t, "timestamp", lua.LNumber(event.Timestamp))
    L.SetField(t, "actor_kind", lua.LString(actorKindToString(event.ActorKind)))
    L.SetField(t, "actor_id", lua.LString(event.ActorID))
    L.SetField(t, "payload", lua.LString(event.Payload))
    return t
}

func actorKindToString(kind pluginpkg.ActorKind) string {
    switch kind {
    case pluginpkg.ActorCharacter:
        return "character"
    case pluginpkg.ActorSystem:
        return "system"
    case pluginpkg.ActorPlugin:
        return "plugin"
    default:
        return "unknown"
    }
}

func (h *Host) parseEmitEvents(ret lua.LValue) ([]pluginpkg.EmitEvent, error) {
    if ret.Type() == lua.LTNil {
        return nil, nil
    }

    table, ok := ret.(*lua.LTable)
    if !ok {
        return nil, nil // Non-table return is ignored
    }

    var emits []pluginpkg.EmitEvent
    table.ForEach(func(_, v lua.LValue) {
        if eventTable, ok := v.(*lua.LTable); ok {
            emit := pluginpkg.EmitEvent{
                Stream:  eventTable.RawGetString("stream").String(),
                Type:    pluginpkg.EventType(eventTable.RawGetString("type").String()),
                Payload: eventTable.RawGetString("payload").String(),
            }
            emits = append(emits, emit)
        }
    })

    return emits, nil
}
```

### Step 5: Run tests

```bash
task test -- -run TestLuaHost ./internal/plugin/lua/...
```

Expected: PASS

### Step 6: Commit

```bash
git add internal/plugin/host.go internal/plugin/lua/host.go internal/plugin/lua/host_test.go
git commit -m "feat(plugin): implement LuaHost with event delivery

LuaHost loads Lua plugins, creates fresh sandboxed states per event,
and executes on_event handlers. Plugins can return emit events.

Part of Epic 2: Plugin System (holomush-1hq)"
```

---

## Task 5: Host Functions (Phase 2.4)

**Files:**

- Create: `internal/plugin/hostfunc/functions.go`
- Create: `internal/plugin/hostfunc/functions_test.go`

### Step 1: Write failing tests

```go
// internal/plugin/hostfunc/functions_test.go
package hostfunc_test

import (
    "context"
    "testing"

    "github.com/holomush/holomush/internal/plugin/capability"
    "github.com/holomush/holomush/internal/plugin/hostfunc"
    lua "github.com/yuin/gopher-lua"
)

func TestHostFunctions_Log(t *testing.T) {
    L := lua.NewState()
    defer L.Close()

    hf := hostfunc.New(nil, nil, capability.NewEnforcer())
    hf.Register(L, "test-plugin")

    err := L.DoString(`holomush.log("info", "test message")`)
    if err != nil {
        t.Errorf("log() failed: %v", err)
    }
}

func TestHostFunctions_NewRequestID(t *testing.T) {
    L := lua.NewState()
    defer L.Close()

    hf := hostfunc.New(nil, nil, capability.NewEnforcer())
    hf.Register(L, "test-plugin")

    err := L.DoString(`id = holomush.new_request_id()`)
    if err != nil {
        t.Fatalf("new_request_id() failed: %v", err)
    }

    id := L.GetGlobal("id").String()
    if len(id) != 26 { // ULID length
        t.Errorf("id length = %d, want 26", len(id))
    }
}

func TestHostFunctions_KV_RequiresCapability(t *testing.T) {
    L := lua.NewState()
    defer L.Close()

    enforcer := capability.NewEnforcer()
    // No capabilities granted

    hf := hostfunc.New(nil, nil, enforcer)
    hf.Register(L, "test-plugin")

    err := L.DoString(`holomush.kv_get("key")`)
    if err == nil {
        t.Error("expected capability error")
    }
}

func TestHostFunctions_KV_WithCapability(t *testing.T) {
    L := lua.NewState()
    defer L.Close()

    enforcer := capability.NewEnforcer()
    enforcer.SetGrants("test-plugin", []string{"kv.read", "kv.write"})

    kvStore := &mockKVStore{data: make(map[string][]byte)}
    hf := hostfunc.New(kvStore, nil, enforcer)
    hf.Register(L, "test-plugin")

    // Set and get
    err := L.DoString(`holomush.kv_set("mykey", "myvalue")`)
    if err != nil {
        t.Fatalf("kv_set failed: %v", err)
    }

    err = L.DoString(`result = holomush.kv_get("mykey")`)
    if err != nil {
        t.Fatalf("kv_get failed: %v", err)
    }

    result := L.GetGlobal("result").String()
    if result != "myvalue" {
        t.Errorf("result = %q, want %q", result, "myvalue")
    }
}

type mockKVStore struct {
    data map[string][]byte
}

func (m *mockKVStore) Get(_ context.Context, namespace, key string) ([]byte, error) {
    return m.data[namespace+":"+key], nil
}

func (m *mockKVStore) Set(_ context.Context, namespace, key string, value []byte) error {
    m.data[namespace+":"+key] = value
    return nil
}

func (m *mockKVStore) Delete(_ context.Context, namespace, key string) error {
    delete(m.data, namespace+":"+key)
    return nil
}
```

### Step 2: Run test to verify it fails

```bash
task test -- -run TestHostFunctions ./internal/plugin/hostfunc/...
```

Expected: Compilation error

### Step 3: Write implementation

```go
// internal/plugin/hostfunc/functions.go
package hostfunc

import (
    "context"
    "log/slog"

    "github.com/holomush/holomush/internal/plugin/capability"
    "github.com/oklog/ulid/v2"
    lua "github.com/yuin/gopher-lua"
)

// KVStore provides namespaced key-value storage.
type KVStore interface {
    Get(ctx context.Context, namespace, key string) ([]byte, error)
    Set(ctx context.Context, namespace, key string, value []byte) error
    Delete(ctx context.Context, namespace, key string) error
}

// WorldReader provides read-only access to world data.
type WorldReader interface {
    // Future: GetLocation, GetCharacter, GetObject
}

// Functions provides host functions to Lua plugins.
type Functions struct {
    kvStore  KVStore
    world    WorldReader
    enforcer *capability.Enforcer
}

// New creates host functions with dependencies.
func New(kv KVStore, world WorldReader, enforcer *capability.Enforcer) *Functions {
    return &Functions{
        kvStore:  kv,
        world:    world,
        enforcer: enforcer,
    }
}

// Register adds host functions to a Lua state.
func (f *Functions) Register(L *lua.LState, pluginName string) {
    mod := L.NewTable()

    // Logging (no capability required)
    L.SetField(mod, "log", f.logFn(pluginName))

    // Request ID (no capability required)
    L.SetField(mod, "new_request_id", f.newRequestIDFn())

    // KV operations (capability required)
    L.SetField(mod, "kv_get", f.wrap(pluginName, "kv.read", f.kvGetFn(pluginName)))
    L.SetField(mod, "kv_set", f.wrap(pluginName, "kv.write", f.kvSetFn(pluginName)))
    L.SetField(mod, "kv_delete", f.wrap(pluginName, "kv.write", f.kvDeleteFn(pluginName)))

    L.SetGlobal("holomush", mod)
}

func (f *Functions) wrap(plugin, cap string, fn lua.LGFunction) lua.LGFunction {
    return func(L *lua.LState) int {
        if !f.enforcer.Check(plugin, cap) {
            L.RaiseError("capability denied: %s requires %s", plugin, cap)
            return 0
        }
        return fn(L)
    }
}

func (f *Functions) logFn(pluginName string) lua.LGFunction {
    return func(L *lua.LState) int {
        level := L.CheckString(1)
        message := L.CheckString(2)

        logger := slog.Default().With("plugin", pluginName)
        switch level {
        case "debug":
            logger.Debug(message)
        case "info":
            logger.Info(message)
        case "warn":
            logger.Warn(message)
        case "error":
            logger.Error(message)
        default:
            logger.Info(message)
        }
        return 0
    }
}

func (f *Functions) newRequestIDFn() lua.LGFunction {
    return func(L *lua.LState) int {
        id := ulid.Make()
        L.Push(lua.LString(id.String()))
        return 1
    }
}

func (f *Functions) kvGetFn(pluginName string) lua.LGFunction {
    return func(L *lua.LState) int {
        key := L.CheckString(1)

        if f.kvStore == nil {
            L.Push(lua.LNil)
            L.Push(lua.LString("kv store not available"))
            return 2
        }

        value, err := f.kvStore.Get(context.Background(), pluginName, key)
        if err != nil {
            L.Push(lua.LNil)
            L.Push(lua.LString(err.Error()))
            return 2
        }

        if value == nil {
            L.Push(lua.LNil)
            return 1
        }

        L.Push(lua.LString(string(value)))
        return 1
    }
}

func (f *Functions) kvSetFn(pluginName string) lua.LGFunction {
    return func(L *lua.LState) int {
        key := L.CheckString(1)
        value := L.CheckString(2)

        if f.kvStore == nil {
            L.Push(lua.LString("kv store not available"))
            return 1
        }

        if err := f.kvStore.Set(context.Background(), pluginName, key, []byte(value)); err != nil {
            L.Push(lua.LString(err.Error()))
            return 1
        }

        return 0
    }
}

func (f *Functions) kvDeleteFn(pluginName string) lua.LGFunction {
    return func(L *lua.LState) int {
        key := L.CheckString(1)

        if f.kvStore == nil {
            L.Push(lua.LString("kv store not available"))
            return 1
        }

        if err := f.kvStore.Delete(context.Background(), pluginName, key); err != nil {
            L.Push(lua.LString(err.Error()))
            return 1
        }

        return 0
    }
}
```

### Step 4: Run tests

```bash
task test -- -run TestHostFunctions ./internal/plugin/hostfunc/...
```

Expected: PASS

### Step 5: Commit

```bash
git add internal/plugin/hostfunc/
git commit -m "feat(plugin): add host functions with capability enforcement

Implements holomush.log, holomush.new_request_id, and kv_* functions.
All KV operations require kv.read or kv.write capabilities.

Part of Epic 2: Plugin System (holomush-1hq)"
```

---

## Task 6: Plugin Manager (Phase 2.2)

**Files:**

- Create: `internal/plugin/manager.go`
- Create: `internal/plugin/manager_test.go`

### Step 1: Write failing tests

```go
// internal/plugin/manager_test.go
package plugin_test

import (
    "context"
    "os"
    "path/filepath"
    "testing"

    "github.com/holomush/holomush/internal/plugin"
)

func TestManager_Discover(t *testing.T) {
    dir := t.TempDir()

    // Create plugin directories
    echoDir := filepath.Join(dir, "plugins", "echo-bot")
    if err := os.MkdirAll(echoDir, 0755); err != nil {
        t.Fatal(err)
    }

    manifest := `
name: echo-bot
version: 1.0.0
type: lua
events:
  - say
lua-plugin:
  entry: main.lua
`
    if err := os.WriteFile(filepath.Join(echoDir, "plugin.yaml"), []byte(manifest), 0644); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(echoDir, "main.lua"), []byte("function on_event(e) end"), 0644); err != nil {
        t.Fatal(err)
    }

    mgr := plugin.NewManager(filepath.Join(dir, "plugins"))
    manifests, err := mgr.Discover(context.Background())
    if err != nil {
        t.Fatalf("Discover() error = %v", err)
    }

    if len(manifests) != 1 {
        t.Fatalf("len(manifests) = %d, want 1", len(manifests))
    }

    if manifests[0].Name != "echo-bot" {
        t.Errorf("Name = %q, want %q", manifests[0].Name, "echo-bot")
    }
}

func TestManager_Discover_SkipsInvalidPlugins(t *testing.T) {
    dir := t.TempDir()
    pluginsDir := filepath.Join(dir, "plugins")

    // Create valid plugin
    validDir := filepath.Join(pluginsDir, "valid")
    if err := os.MkdirAll(validDir, 0755); err != nil {
        t.Fatal(err)
    }
    validManifest := `name: valid
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua`
    if err := os.WriteFile(filepath.Join(validDir, "plugin.yaml"), []byte(validManifest), 0644); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(validDir, "main.lua"), []byte(""), 0644); err != nil {
        t.Fatal(err)
    }

    // Create invalid plugin (bad YAML)
    invalidDir := filepath.Join(pluginsDir, "invalid")
    if err := os.MkdirAll(invalidDir, 0755); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(invalidDir, "plugin.yaml"), []byte("invalid: ["), 0644); err != nil {
        t.Fatal(err)
    }

    mgr := plugin.NewManager(pluginsDir)
    manifests, err := mgr.Discover(context.Background())

    // Should succeed but only return valid plugin
    if err != nil {
        t.Fatalf("Discover() error = %v", err)
    }

    if len(manifests) != 1 {
        t.Errorf("len(manifests) = %d, want 1 (valid only)", len(manifests))
    }
}
```

### Step 2: Run test to verify it fails

```bash
task test -- -run TestManager ./internal/plugin/...
```

Expected: Compilation error

### Step 3: Write implementation

```go
// internal/plugin/manager.go
package plugin

import (
    "context"
    "log/slog"
    "os"
    "path/filepath"
)

// Manager discovers and manages plugin lifecycle.
type Manager struct {
    pluginsDir string
}

// NewManager creates a plugin manager.
func NewManager(pluginsDir string) *Manager {
    return &Manager{pluginsDir: pluginsDir}
}

// DiscoveredPlugin contains a manifest and its directory.
type DiscoveredPlugin struct {
    Manifest *Manifest
    Dir      string
}

// Discover finds all valid plugins in the plugins directory.
// Invalid plugins are logged and skipped.
func (m *Manager) Discover(_ context.Context) ([]*DiscoveredPlugin, error) {
    entries, err := os.ReadDir(m.pluginsDir)
    if err != nil {
        if os.IsNotExist(err) {
            return nil, nil // No plugins directory
        }
        return nil, err
    }

    var plugins []*DiscoveredPlugin
    for _, entry := range entries {
        if !entry.IsDir() {
            continue
        }

        pluginDir := filepath.Join(m.pluginsDir, entry.Name())
        manifestPath := filepath.Join(pluginDir, "plugin.yaml")

        data, err := os.ReadFile(manifestPath)
        if err != nil {
            slog.Warn("skipping plugin without manifest",
                "dir", entry.Name(),
                "error", err)
            continue
        }

        manifest, err := ParseManifest(data)
        if err != nil {
            slog.Warn("skipping plugin with invalid manifest",
                "dir", entry.Name(),
                "error", err)
            continue
        }

        plugins = append(plugins, &DiscoveredPlugin{
            Manifest: manifest,
            Dir:      pluginDir,
        })
    }

    return plugins, nil
}
```

### Step 4: Run tests

```bash
task test -- -run TestManager ./internal/plugin/...
```

Expected: PASS

### Step 5: Commit

```bash
git add internal/plugin/manager.go internal/plugin/manager_test.go
git commit -m "feat(plugin): add plugin manager with discovery

Discovers plugins in plugins/*/ directories, parses manifests,
and skips invalid plugins with warnings.

Part of Epic 2: Plugin System (holomush-1hq)"
```

---

## Task 7: Plugin Subscriber (Phase 2.3)

**Files:**

- Create: `internal/plugin/subscriber.go`
- Create: `internal/plugin/subscriber_test.go`

### Step 1: Write failing tests

```go
// internal/plugin/subscriber_test.go
package plugin_test

import (
    "context"
    "sync"
    "testing"
    "time"

    "github.com/holomush/holomush/internal/plugin"
    pluginpkg "github.com/holomush/holomush/pkg/plugin"
)

type mockHost struct {
    delivered []pluginpkg.Event
    response  []pluginpkg.EmitEvent
    mu        sync.Mutex
}

func (m *mockHost) Load(context.Context, *plugin.Manifest, string) error { return nil }
func (m *mockHost) Unload(context.Context, string) error                 { return nil }
func (m *mockHost) Plugins() []string                                    { return []string{"test"} }
func (m *mockHost) Close(context.Context) error                          { return nil }

func (m *mockHost) DeliverEvent(_ context.Context, _ string, event pluginpkg.Event) ([]pluginpkg.EmitEvent, error) {
    m.mu.Lock()
    m.delivered = append(m.delivered, event)
    resp := m.response
    m.mu.Unlock()
    return resp, nil
}

type mockEmitter struct {
    emitted []pluginpkg.EmitEvent
    mu      sync.Mutex
}

func (m *mockEmitter) EmitPluginEvent(_ context.Context, _ string, event pluginpkg.EmitEvent) error {
    m.mu.Lock()
    m.emitted = append(m.emitted, event)
    m.mu.Unlock()
    return nil
}

func TestSubscriber_DeliversEvents(t *testing.T) {
    host := &mockHost{}
    emitter := &mockEmitter{}

    sub := plugin.NewSubscriber(host, emitter)
    sub.Subscribe("test-plugin", "location:123", []string{"say"})

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    events := make(chan pluginpkg.Event, 1)
    sub.Start(ctx, events)

    events <- pluginpkg.Event{
        ID:     "01ABC",
        Stream: "location:123",
        Type:   "say",
    }

    // Wait for delivery
    time.Sleep(50 * time.Millisecond)

    host.mu.Lock()
    if len(host.delivered) != 1 {
        t.Errorf("delivered = %d, want 1", len(host.delivered))
    }
    host.mu.Unlock()
}

func TestSubscriber_FiltersEventTypes(t *testing.T) {
    host := &mockHost{}
    emitter := &mockEmitter{}

    sub := plugin.NewSubscriber(host, emitter)
    sub.Subscribe("test-plugin", "location:123", []string{"say"}) // Only say events

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    events := make(chan pluginpkg.Event, 2)
    sub.Start(ctx, events)

    events <- pluginpkg.Event{ID: "1", Stream: "location:123", Type: "say"}
    events <- pluginpkg.Event{ID: "2", Stream: "location:123", Type: "pose"} // Should be filtered

    time.Sleep(50 * time.Millisecond)

    host.mu.Lock()
    if len(host.delivered) != 1 {
        t.Errorf("delivered = %d, want 1 (pose should be filtered)", len(host.delivered))
    }
    host.mu.Unlock()
}
```

### Step 2: Run test to verify it fails

```bash
task test -- -run TestSubscriber ./internal/plugin/...
```

Expected: Compilation error

### Step 3: Write implementation

```go
// internal/plugin/subscriber.go
package plugin

import (
    "context"
    "log/slog"
    "sync"
    "time"

    pluginpkg "github.com/holomush/holomush/pkg/plugin"
)

// EventEmitter publishes events from plugins.
type EventEmitter interface {
    EmitPluginEvent(ctx context.Context, pluginName string, event pluginpkg.EmitEvent) error
}

// subscription tracks which events a plugin wants.
type subscription struct {
    pluginName string
    stream     string
    eventTypes map[string]bool // empty = all events
}

// Subscriber dispatches events to plugins.
type Subscriber struct {
    host          Host
    emitter       EventEmitter
    subscriptions []subscription
    mu            sync.RWMutex
    wg            sync.WaitGroup
}

// NewSubscriber creates an event subscriber.
func NewSubscriber(host Host, emitter EventEmitter) *Subscriber {
    return &Subscriber{
        host:    host,
        emitter: emitter,
    }
}

// Subscribe registers a plugin to receive events.
func (s *Subscriber) Subscribe(pluginName, stream string, eventTypes []string) {
    s.mu.Lock()
    defer s.mu.Unlock()

    typeSet := make(map[string]bool)
    for _, t := range eventTypes {
        typeSet[t] = true
    }

    s.subscriptions = append(s.subscriptions, subscription{
        pluginName: pluginName,
        stream:     stream,
        eventTypes: typeSet,
    })
}

// Start begins processing events from the channel.
func (s *Subscriber) Start(ctx context.Context, events <-chan pluginpkg.Event) {
    s.wg.Add(1)
    go func() {
        defer s.wg.Done()
        for {
            select {
            case <-ctx.Done():
                return
            case event, ok := <-events:
                if !ok {
                    return
                }
                s.dispatch(ctx, event)
            }
        }
    }()
}

// Stop waits for the subscriber to finish.
func (s *Subscriber) Stop() {
    s.wg.Wait()
}

func (s *Subscriber) dispatch(ctx context.Context, event pluginpkg.Event) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    for _, sub := range s.subscriptions {
        if sub.stream != event.Stream {
            continue
        }
        if len(sub.eventTypes) > 0 && !sub.eventTypes[string(event.Type)] {
            continue
        }

        s.deliverAsync(ctx, sub.pluginName, event)
    }
}

func (s *Subscriber) deliverAsync(ctx context.Context, pluginName string, event pluginpkg.Event) {
    // Use timeout for plugin execution
    ctx, cancel := context.WithTimeout(ctx, 5*time.Second)

    go func() {
        defer cancel()

        emits, err := s.host.DeliverEvent(ctx, pluginName, event)
        if err != nil {
            slog.Error("failed to deliver event to plugin",
                "plugin", pluginName,
                "event_id", event.ID,
                "error", err)
            return
        }

        // Emit response events
        for _, emit := range emits {
            if err := s.emitter.EmitPluginEvent(ctx, pluginName, emit); err != nil {
                slog.Error("failed to emit plugin event",
                    "plugin", pluginName,
                    "stream", emit.Stream,
                    "error", err)
            }
        }
    }()
}
```

### Step 4: Run tests

```bash
task test -- -run TestSubscriber ./internal/plugin/...
```

Expected: PASS

### Step 5: Commit

```bash
git add internal/plugin/subscriber.go internal/plugin/subscriber_test.go
git commit -m "feat(plugin): add event subscriber with filtering

Dispatches events to subscribed plugins, filters by stream and event
type, and emits response events asynchronously with 5s timeout.

Part of Epic 2: Plugin System (holomush-1hq)"
```

---

## Task 8: Integration with Host Functions (Phase 2.4)

**Files:**

- Modify: `internal/plugin/lua/host.go`
- Modify: `internal/plugin/lua/host_test.go`

### Step 1: Write integration test

```go
// Add to internal/plugin/lua/host_test.go

func TestLuaHost_WithHostFunctions(t *testing.T) {
    dir := t.TempDir()

    mainLua := `
function on_event(event)
    local id = holomush.new_request_id()
    holomush.log("info", "Got event: " .. event.type)
    return {{
        stream = event.stream,
        type = "say",
        payload = '{"request_id":"' .. id .. '"}'
    }}
end
`
    if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(mainLua), 0644); err != nil {
        t.Fatal(err)
    }

    enforcer := capability.NewEnforcer()
    enforcer.SetGrants("test", []string{"kv.*"})

    hostFuncs := hostfunc.New(nil, nil, enforcer)
    host := pluginlua.NewHostWithFunctions(hostFuncs)
    defer host.Close(context.Background())

    manifest := &plugin.Manifest{
        Name:      "test",
        Version:   "1.0.0",
        Type:      plugin.TypeLua,
        LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
    }

    if err := host.Load(context.Background(), manifest, dir); err != nil {
        t.Fatal(err)
    }

    event := pluginpkg.Event{
        ID:     "01ABC",
        Stream: "location:123",
        Type:   "say",
    }

    emits, err := host.DeliverEvent(context.Background(), "test", event)
    if err != nil {
        t.Fatalf("DeliverEvent() error = %v", err)
    }

    if len(emits) != 1 {
        t.Errorf("len(emits) = %d, want 1", len(emits))
    }
}
```

### Step 2: Update LuaHost to accept host functions

```go
// Modify internal/plugin/lua/host.go

import (
    "github.com/holomush/holomush/internal/plugin/hostfunc"
)

type Host struct {
    factory   *StateFactory
    hostFuncs *hostfunc.Functions
    plugins   map[string]*luaPlugin
    mu        sync.RWMutex
    closed    bool
}

func NewHost() *Host {
    return &Host{
        factory: NewStateFactory(),
        plugins: make(map[string]*luaPlugin),
    }
}

func NewHostWithFunctions(hf *hostfunc.Functions) *Host {
    return &Host{
        factory:   NewStateFactory(),
        hostFuncs: hf,
        plugins:   make(map[string]*luaPlugin),
    }
}

// In DeliverEvent, after creating state:
if h.hostFuncs != nil {
    h.hostFuncs.Register(L, name)
}
```

### Step 3: Run tests

```bash
task test -- -run TestLuaHost ./internal/plugin/lua/...
```

Expected: PASS

### Step 4: Commit

```bash
git add internal/plugin/lua/host.go internal/plugin/lua/host_test.go
git commit -m "feat(plugin): integrate host functions with LuaHost

LuaHost now accepts optional HostFunctions that are registered in each
state, providing plugins access to holomush.* functions.

Part of Epic 2: Plugin System (holomush-1hq)"
```

---

## Task 9: Echo Bot Plugin (Phase 2.7)

**Files:**

- Create: `plugins/echo-bot/plugin.yaml`
- Create: `plugins/echo-bot/main.lua`

### Step 1: Create plugin manifest

```yaml
# plugins/echo-bot/plugin.yaml
name: echo-bot
version: 1.0.0
type: lua
events:
  - say
capabilities:
  - events.emit.location
lua-plugin:
  entry: main.lua
```

### Step 2: Create plugin implementation

```lua
-- plugins/echo-bot/main.lua

-- Echo bot: repeats messages back to the room
-- Demonstrates basic event handling and response

function on_event(event)
    -- Only respond to say events
    if event.type ~= "say" then
        return nil
    end

    -- Don't echo plugin messages (prevents loops)
    if event.actor_kind == "plugin" then
        return nil
    end

    -- Parse payload to get message
    -- payload is JSON string like {"message":"hello"}
    local msg = event.payload:match('"message":"([^"]*)"')
    if not msg then
        return nil
    end

    -- Return echo event
    return {
        {
            stream = event.stream,
            type = "say",
            payload = '{"message":"Echo: ' .. msg .. '"}'
        }
    }
end
```

### Step 3: Write integration test

```go
// internal/plugin/integration_test.go
//go:build integration

package plugin_test

import (
    "context"
    "path/filepath"
    "runtime"
    "testing"

    "github.com/holomush/holomush/internal/plugin"
    "github.com/holomush/holomush/internal/plugin/capability"
    "github.com/holomush/holomush/internal/plugin/hostfunc"
    pluginlua "github.com/holomush/holomush/internal/plugin/lua"
    pluginpkg "github.com/holomush/holomush/pkg/plugin"
)

func TestEchoBot_Integration(t *testing.T) {
    // Find project root
    _, filename, _, _ := runtime.Caller(0)
    projectRoot := filepath.Join(filepath.Dir(filename), "..", "..")
    pluginsDir := filepath.Join(projectRoot, "plugins")

    // Discover plugins
    mgr := plugin.NewManager(pluginsDir)
    discovered, err := mgr.Discover(context.Background())
    if err != nil {
        t.Fatalf("Discover() error = %v", err)
    }

    // Find echo-bot
    var echoManifest *plugin.DiscoveredPlugin
    for _, p := range discovered {
        if p.Manifest.Name == "echo-bot" {
            echoManifest = p
            break
        }
    }
    if echoManifest == nil {
        t.Skip("echo-bot plugin not found")
    }

    // Setup host
    enforcer := capability.NewEnforcer()
    enforcer.SetGrants("echo-bot", echoManifest.Manifest.Capabilities)

    hostFuncs := hostfunc.New(nil, nil, enforcer)
    host := pluginlua.NewHostWithFunctions(hostFuncs)
    defer host.Close(context.Background())

    // Load plugin
    if err := host.Load(context.Background(), echoManifest.Manifest, echoManifest.Dir); err != nil {
        t.Fatalf("Load() error = %v", err)
    }

    // Send event
    event := pluginpkg.Event{
        ID:        "01ABC",
        Stream:    "location:123",
        Type:      "say",
        Timestamp: 1705591234000,
        ActorKind: pluginpkg.ActorCharacter,
        ActorID:   "char_1",
        Payload:   `{"message":"Hello world"}`,
    }

    emits, err := host.DeliverEvent(context.Background(), "echo-bot", event)
    if err != nil {
        t.Fatalf("DeliverEvent() error = %v", err)
    }

    if len(emits) != 1 {
        t.Fatalf("len(emits) = %d, want 1", len(emits))
    }

    expected := `{"message":"Echo: Hello world"}`
    if emits[0].Payload != expected {
        t.Errorf("Payload = %q, want %q", emits[0].Payload, expected)
    }
}
```

### Step 4: Run integration test

```bash
task test -- -tags=integration -run TestEchoBot ./internal/plugin/...
```

Expected: PASS

### Step 5: Commit

```bash
git add plugins/echo-bot/ internal/plugin/integration_test.go
git commit -m "feat(plugin): add echo-bot sample plugin

Demonstrates Lua plugin development with event handling.
Echoes messages back to the room, avoiding infinite loops.

Part of Epic 2: Plugin System (holomush-1hq)"
```

---

## Task 10: go-plugin Proto Definitions (Phase 2.6)

**Files:**

- Create: `api/proto/holomush/plugin/v1/plugin.proto`
- Create: `api/proto/holomush/plugin/v1/hostfunc.proto`

### Step 1: Create plugin service proto

```protobuf
// api/proto/holomush/plugin/v1/plugin.proto
syntax = "proto3";

package holomush.plugin.v1;

option go_package = "github.com/holomush/holomush/internal/proto/holomush/plugin/v1;pluginv1";

service Plugin {
    rpc HandleEvent(HandleEventRequest) returns (HandleEventResponse);
}

message Event {
    string id = 1;
    string stream = 2;
    string type = 3;
    int64 timestamp = 4;
    string actor_kind = 5;  // "character", "system", "plugin"
    string actor_id = 6;
    bytes payload = 7;  // JSON-encoded
}

message EmitEvent {
    string stream = 1;
    string type = 2;
    bytes payload = 3;  // JSON-encoded
}

message HandleEventRequest {
    Event event = 1;
}

message HandleEventResponse {
    repeated EmitEvent events = 1;
}
```

### Step 2: Create host functions proto

```protobuf
// api/proto/holomush/plugin/v1/hostfunc.proto
syntax = "proto3";

package holomush.plugin.v1;

option go_package = "github.com/holomush/holomush/internal/proto/holomush/plugin/v1;pluginv1";

service HostFunctions {
    rpc EmitEvent(EmitEventRequest) returns (EmitEventResponse);
    rpc QueryLocation(QueryLocationRequest) returns (QueryLocationResponse);
    rpc QueryCharacter(QueryCharacterRequest) returns (QueryCharacterResponse);
    rpc QueryObject(QueryObjectRequest) returns (QueryObjectResponse);
    rpc KVGet(KVGetRequest) returns (KVGetResponse);
    rpc KVSet(KVSetRequest) returns (KVSetResponse);
    rpc KVDelete(KVDeleteRequest) returns (KVDeleteResponse);
    rpc Log(LogRequest) returns (LogResponse);
    rpc NewRequestID(NewRequestIDRequest) returns (NewRequestIDResponse);
}

message EmitEventRequest {
    string stream = 1;
    string type = 2;
    bytes payload = 3;
}

message EmitEventResponse {}

message QueryLocationRequest {
    string id = 1;
}

message QueryLocationResponse {
    bytes location = 1;  // JSON-encoded Location
}

message QueryCharacterRequest {
    string id = 1;
}

message QueryCharacterResponse {
    bytes character = 1;  // JSON-encoded Character
}

message QueryObjectRequest {
    string id = 1;
}

message QueryObjectResponse {
    bytes object = 1;  // JSON-encoded Object
}

message KVGetRequest {
    string key = 1;
}

message KVGetResponse {
    bytes value = 1;
    bool found = 2;
}

message KVSetRequest {
    string key = 1;
    bytes value = 2;
}

message KVSetResponse {}

message KVDeleteRequest {
    string key = 1;
}

message KVDeleteResponse {}

message LogRequest {
    string level = 1;
    string message = 2;
}

message LogResponse {}

message NewRequestIDRequest {}

message NewRequestIDResponse {
    string id = 1;
}
```

### Step 3: Generate Go code

```bash
buf generate api/proto
```

### Step 4: Commit

```bash
git add api/proto/holomush/plugin/v1/
git commit -m "feat(plugin): add gRPC proto definitions for go-plugin

Defines Plugin service for event handling and HostFunctions service
for plugins to call back into the host.

Part of Epic 2: Plugin System (holomush-1hq)"
```

---

## Task 11: GoPluginHost Implementation (Phase 2.6)

**Files:**

- Create: `internal/plugin/goplugin/host.go`
- Create: `internal/plugin/goplugin/host_test.go`
- Create: `pkg/pluginsdk/sdk.go`

### Step 1: Create plugin SDK

```go
// pkg/pluginsdk/sdk.go
package pluginsdk

import (
    "context"

    "github.com/hashicorp/go-plugin"
    "google.golang.org/grpc"
)

// Event matches the proto Event message.
type Event struct {
    ID        string
    Stream    string
    Type      string
    Timestamp int64
    ActorKind string // "character", "system", "plugin"
    ActorID   string
    Payload   []byte
}

// EmitEvent matches the proto EmitEvent message.
type EmitEvent struct {
    Stream  string
    Type    string
    Payload []byte
}

// Plugin is the interface that plugin implementations must satisfy.
type Plugin interface {
    HandleEvent(ctx context.Context, event Event) ([]EmitEvent, error)
}

// Host provides callbacks from plugin to host.
type Host interface {
    EmitEvent(ctx context.Context, stream, eventType string, payload []byte) error
    QueryLocation(ctx context.Context, id string) ([]byte, error)
    QueryCharacter(ctx context.Context, id string) ([]byte, error)
    QueryObject(ctx context.Context, id string) ([]byte, error)
    KVGet(ctx context.Context, key string) ([]byte, bool, error)
    KVSet(ctx context.Context, key string, value []byte) error
    KVDelete(ctx context.Context, key string) error
    NewRequestID(ctx context.Context) (string, error)
    Log(ctx context.Context, level, message string) error
}

// Handshake is used to verify plugin compatibility.
var Handshake = plugin.HandshakeConfig{
    ProtocolVersion:  1,
    MagicCookieKey:   "HOLOMUSH_PLUGIN",
    MagicCookieValue: "plugin",
}

// PluginMap is the map of plugins we can serve/consume.
var PluginMap = map[string]plugin.Plugin{
    "plugin": &GRPCPlugin{},
}

// GRPCPlugin implements plugin.GRPCPlugin interface.
type GRPCPlugin struct {
    plugin.Plugin
    Impl Plugin
}

func (p *GRPCPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
    // Server-side implementation (in plugin process)
    // Implementation details in Phase 2.6 task
    return nil
}

func (p *GRPCPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
    // Client-side implementation (in host process)
    // Implementation details in Phase 2.6 task
    return nil, nil
}
```

### Step 2: Write GoPluginHost skeleton

```go
// internal/plugin/goplugin/host.go
package goplugin

import (
    "context"
    "fmt"
    "os/exec"
    "path/filepath"
    "runtime"
    "strings"
    "sync"

    "github.com/hashicorp/go-plugin"
    internalplugin "github.com/holomush/holomush/internal/plugin"
    "github.com/holomush/holomush/internal/plugin/capability"
    pluginpkg "github.com/holomush/holomush/pkg/plugin"
    "github.com/holomush/holomush/pkg/pluginsdk"
)

// Host manages go-plugin binary plugins.
type Host struct {
    clients  map[string]*plugin.Client
    plugins  map[string]pluginsdk.Plugin
    enforcer *capability.Enforcer
    mu       sync.RWMutex
    closed   bool
}

// NewHost creates a go-plugin host.
func NewHost(enforcer *capability.Enforcer) *Host {
    return &Host{
        clients:  make(map[string]*plugin.Client),
        plugins:  make(map[string]pluginsdk.Plugin),
        enforcer: enforcer,
    }
}

// Load starts a plugin subprocess.
func (h *Host) Load(ctx context.Context, manifest *internalplugin.Manifest, dir string) error {
    h.mu.Lock()
    defer h.mu.Unlock()

    if h.closed {
        return fmt.Errorf("host is closed")
    }

    binary := expandBinaryPath(manifest.BinaryPlugin.Executable, dir)

    client := plugin.NewClient(&plugin.ClientConfig{
        HandshakeConfig:  pluginsdk.Handshake,
        Plugins:          pluginsdk.PluginMap,
        Cmd:              exec.Command(binary),
        AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
    })

    rpcClient, err := client.Client()
    if err != nil {
        client.Kill()
        return fmt.Errorf("connect to plugin: %w", err)
    }

    raw, err := rpcClient.Dispense("plugin")
    if err != nil {
        client.Kill()
        return fmt.Errorf("dispense plugin: %w", err)
    }

    h.clients[manifest.Name] = client
    h.plugins[manifest.Name] = raw.(pluginsdk.Plugin)

    return nil
}

// Unload stops a plugin subprocess.
func (h *Host) Unload(_ context.Context, name string) error {
    h.mu.Lock()
    defer h.mu.Unlock()

    client, ok := h.clients[name]
    if !ok {
        return fmt.Errorf("plugin %s not loaded", name)
    }

    client.Kill()
    delete(h.clients, name)
    delete(h.plugins, name)
    return nil
}

// DeliverEvent sends an event to the plugin via gRPC.
func (h *Host) DeliverEvent(ctx context.Context, name string, event pluginpkg.Event) ([]pluginpkg.EmitEvent, error) {
    h.mu.RLock()
    p, ok := h.plugins[name]
    if !ok {
        h.mu.RUnlock()
        return nil, fmt.Errorf("plugin %s not loaded", name)
    }
    h.mu.RUnlock()

    sdkEvent := pluginsdk.Event{
        ID:        event.ID,
        Stream:    event.Stream,
        Type:      string(event.Type),
        Timestamp: event.Timestamp,
        ActorKind: actorKindToString(event.ActorKind),
        ActorID:   event.ActorID,
        Payload:   []byte(event.Payload),
    }

    emits, err := p.HandleEvent(ctx, sdkEvent)
    if err != nil {
        return nil, err
    }

    result := make([]pluginpkg.EmitEvent, len(emits))
    for i, e := range emits {
        result[i] = pluginpkg.EmitEvent{
            Stream:  e.Stream,
            Type:    pluginpkg.EventType(e.Type),
            Payload: string(e.Payload),
        }
    }
    return result, nil
}

// Plugins returns loaded plugin names.
func (h *Host) Plugins() []string {
    h.mu.RLock()
    defer h.mu.RUnlock()

    names := make([]string, 0, len(h.plugins))
    for name := range h.plugins {
        names = append(names, name)
    }
    return names
}

// Close terminates all plugin subprocesses.
func (h *Host) Close(_ context.Context) error {
    h.mu.Lock()
    defer h.mu.Unlock()

    h.closed = true
    for _, client := range h.clients {
        client.Kill()
    }
    h.clients = nil
    h.plugins = nil
    return nil
}

func expandBinaryPath(template, dir string) string {
    result := strings.ReplaceAll(template, "${os}", runtime.GOOS)
    result = strings.ReplaceAll(result, "${arch}", runtime.GOARCH)
    return filepath.Join(dir, result)
}

// actorKindToString converts ActorKind to string for proto/SDK.
// Note: This is duplicated from lua/host.go; consider shared package in implementation.
func actorKindToString(kind pluginpkg.ActorKind) string {
    switch kind {
    case pluginpkg.ActorCharacter:
        return "character"
    case pluginpkg.ActorSystem:
        return "system"
    case pluginpkg.ActorPlugin:
        return "plugin"
    default:
        return "unknown"
    }
}
```

### Step 3: Commit

```bash
git add internal/plugin/goplugin/ pkg/pluginsdk/
git commit -m "feat(plugin): add go-plugin host skeleton

Implements GoPluginHost that manages plugin subprocesses via HashiCorp
go-plugin with gRPC communication. Full bidirectional communication
will be added when HostFunctions service is implemented.

Part of Epic 2: Plugin System (holomush-1hq)"
```

---

## Task 12: Delete WASM Spike Code

**Files:**

- Delete: `internal/wasm/` (entire directory)

### Step 1: Ensure no imports remain

```bash
rg "internal/wasm" --type go
```

Expected: No matches (or only in migration notes)

### Step 2: Delete the directory

```bash
rm -rf internal/wasm/
```

### Step 3: Commit

```bash
git add -u internal/wasm/
git commit -m "chore: remove WASM/Extism spike code

The plugin system now uses Lua (gopher-lua) and go-plugin instead
of WASM. The spike served its purpose and learnings have been applied.

Part of Epic 2: Plugin System (holomush-1hq)"
```

---

## Post-Implementation Checklist

After completing all tasks:

- [ ] Run full test suite: `task test`
- [ ] Run linter: `task lint`
- [ ] Verify coverage: `task test:coverage`
- [ ] Update CLAUDE.md if needed
- [ ] Close beads for completed phases
- [ ] Create PR for review

## Acceptance Criteria

- [ ] Lua plugins can be loaded from `plugins/*/`
- [ ] Plugins receive events matching their subscriptions
- [ ] Plugins can emit response events
- [ ] Host functions work with capability enforcement
- [ ] go-plugin skeleton compiles (full implementation deferred)
- [ ] Echo bot demonstrates end-to-end flow
- [ ] WASM spike code removed
- [ ] All tests pass with >80% coverage
