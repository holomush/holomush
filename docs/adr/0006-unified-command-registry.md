# ADR 0006: Unified Command Registry

**Date:** 2026-02-02
**Status:** Accepted
**Deciders:** HoloMUSH Contributors

## Context

HoloMUSH supports two types of command implementations:

- **Go commands**: Core engine commands (e.g., `look`, `move`, `quit`) implemented directly in Go
  for performance and access to internal services
- **Lua commands**: Plugin-provided commands (e.g., `say`, `pose`, `dig`) implemented in Lua for
  customization and extensibility

Both types need registration to enable:

- Command dispatch (routing input to the correct handler)
- Capability checking (ABAC permission verification before execution)
- Help system integration (listing available commands, showing usage)
- Conflict detection (warning when plugins shadow existing commands)

The question is how to organize command registration: should Go and Lua commands use separate
registries, or share a unified registry?

## Options Considered

### Option A: Separate Registries

Maintain distinct registries for Go and Lua commands:

```go
type GoRegistry struct {
    commands map[string]GoCommandEntry
}

type LuaRegistry struct {
    commands map[string]LuaCommandEntry
}
```

Dispatch would check Go registry first, then Lua registry.

**Pros:**

- Type-safe: Go and Lua entries have distinct types
- Clear separation of concerns
- No mixing of implementation details

**Cons:**

- Inconsistent error handling between registries
- Help system must query two sources
- Conflict detection requires cross-registry coordination
- Plugin override of core commands is awkward (which registry wins?)
- Duplicate code for registration, lookup, iteration

### Option B: Unified Registry

Single registry stores all commands regardless of implementation:

```go
type CommandRegistry struct {
    commands map[string]CommandEntry
}

type CommandEntry struct {
    Name         string
    Handler      CommandHandler  // abstraction over Go/Lua
    Capabilities []string
    Help         string
    Source       string  // "core" or plugin name
}
```

**Pros:**

- Single source of truth for command metadata
- Uniform error handling and capability checking
- Help system has one query target
- Clean conflict detection: last registration wins, log warning
- Same API for Go and Lua command registration
- Simpler iteration for introspection

**Cons:**

- Handler abstraction needed to support both Go and Lua
- Less type safety (handler is generic interface)

### Option C: Registry of Registries

Meta-registry that delegates to typed sub-registries:

```go
type MetaRegistry struct {
    go   *GoRegistry
    lua  *LuaRegistry
}
```

**Pros:**

- Type safety for each registry
- Can query individually or together

**Cons:**

- Complexity without clear benefit
- Ordering and conflict resolution still needed
- Help system must aggregate results

## Decision

We adopt **Option B: Unified Registry**.

A single `CommandRegistry` stores all commands (Go and Lua) with a common `CommandEntry` type.
The `Handler` field uses a function type that abstracts over implementation:

```go
type CommandHandler func(ctx context.Context, exec *CommandExecution) error
```

For Go commands, handlers are direct function references. For Lua commands, handlers are dispatcher
functions that route to the plugin system.

### Conflict Resolution

Commands load in deterministic order:

1. Core Go commands register at server startup
2. Plugins load in alphabetical order by plugin name
3. Within a plugin, commands register in manifest declaration order

If a command name is already registered:

- The new registration overwrites the existing one (last-loaded wins)
- A warning is logged: "Command 'X' from plugin 'Y' overrides existing command from 'Z'"

This allows plugins to intentionally override core commands (with admin awareness) while
preventing silent conflicts.

### Registration API

```go
// Go command registration (at startup)
entry, err := NewCommandEntry("look", handlers.Look, []string{"world.look"}, "core")
if err != nil {
    return err
}
registry.Register(*entry)

// Lua command registration (at plugin load)
entry, err := NewCommandEntry("say", LuaDispatcher("communication", "say", host), []string{"comms.say"}, "communication")
if err != nil {
    return err
}
registry.Register(*entry)
```

## Consequences

### Positive

- **Single source of truth**: One registry to query for all commands
- **Uniform introspection**: Help system iterates one collection
- **Consistent capability checking**: Same ABAC flow for Go and Lua commands
- **Clean conflict detection**: Single registration point detects overwrites
- **Plugin flexibility**: Plugins can override core commands if needed
- **Simpler testing**: Mock one registry interface, not multiple

### Negative

- **Handler abstraction**: Lua commands need a dispatcher wrapper
- **Less compile-time safety**: Handler type is generic function
- **Source tracking needed**: Must store where each command came from

### Neutral

- **Memory usage**: Negligible difference (command corpus is small)
- **Lookup performance**: O(1) map lookup in both approaches

## References

- [Commands & Behaviors Design](../specs/2026-02-02-commands-behaviors-design.md) - Parent spec
- [Plugin System Design](../specs/2026-01-18-plugin-system-design.md) - Lua plugin architecture
- [ADR 0005: Command State Management](0005-command-state-management.md) - Related command ADR
- Implementation: `internal/command/registry.go` (planned)
