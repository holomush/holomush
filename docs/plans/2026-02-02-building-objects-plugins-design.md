# Building & Objects Plugins Design

**Date:** 2026-02-02
**Status:** Draft
**Task:** holomush-bpms6

## Overview

Implement building commands as two plugins with different implementation strategies:

- **Building Plugin (Lua)** - World topology commands (dig, link)
- **Objects Plugin (Go)** - Entity manipulation commands (create, set)

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Plugin split | building + objects | Separation of concerns: topology vs entity manipulation |
| Building implementation | Lua | Proves Lua model per epic goal, simple command→API |
| Objects implementation | Go | Core infrastructure, property system, type safety |
| Property matching | Prefix-based | `desc`, `descr`, `description` all work if unique |
| Entity references | ID or name | `#01ABC...` for ID, otherwise name lookup |

## Building Plugin (Lua)

### Commands

| Command | Syntax | Capabilities |
|---------|--------|--------------|
| `dig` | `dig <exit> to "<location>" [return <exit>]` | `build.location`, `build.exit` |
| `link` | `link <exit> to <target>` | `build.exit` |

### Examples

```text
dig north to "Town Square"
dig north to "Market" return south
link east to #01HXYZ123ABC
link east to "Garden"
```

### Files

- `plugins/building/plugin.yaml` - Manifest
- `plugins/building/main.lua` - Command handlers

## Objects Plugin (Go)

### Commands

| Command | Syntax | Capabilities |
|---------|--------|--------------|
| `create` | `create <type> "<name>"` | `object.create.<type>` |
| `set` | `set <property> of <target> to <value>` | `property.set.<property>` |

### Examples

```text
create object "Iron Sword"
create location "Secret Room"
set description of here to A cozy tavern with warm firelight.
set desc of sword to |
A gleaming blade with ancient runes.
The hilt is wrapped in worn leather.
.
```

### Property System

**Prefix Matching:**
Properties support unique prefix matching. If ambiguous, error with collisions:

```text
> set d of here to foo
Error: Ambiguous property 'd' - matches: description, dark_mode
```

**Property Registry:**

| Property | Type | Capability | Applies To |
|----------|------|------------|------------|
| `description` | text | `property.set.description` | location, object, character, exit |
| `name` | string | `property.set.name` | location, object, exit |

Future: Properties have visibility levels (all, owner, admin).

### Files

- `internal/command/handlers/objects.go` - Go plugin implementation

## Host Functions

New `holo.world.*` functions for Lua plugins:

| Function | Signature | Returns |
|----------|-----------|---------|
| `create_location` | `(name, description, type)` | `{id, name}` or `nil, error` |
| `create_exit` | `(from_id, to_id, name, opts)` | `{id, name}` or `nil, error` |
| `find_location` | `(name)` | `{id, name}` or `nil, error` |
| `set_property` | `(entity_type, entity_id, property, value)` | `true` or `nil, error` |
| `get_property` | `(entity_type, entity_id, property)` | `value` or `nil, error` |

All functions check capabilities via the adapter pattern.

**File:** `internal/plugin/hostfunc/world_write.go`

## Implementation Tasks

| # | Task | Type | Files |
|---|------|------|-------|
| 1 | World mutation host functions | Go | `internal/plugin/hostfunc/world_write.go` |
| 2 | Property registry with prefix matching | Go | `pkg/holo/property.go` |
| 3 | Building plugin manifest | YAML | `plugins/building/plugin.yaml` |
| 4 | Building plugin handlers (dig, link) | Lua | `plugins/building/main.lua` |
| 5 | Objects plugin (create, set) | Go | `internal/command/handlers/objects.go` |
| 6 | Integration tests | Go | `*_test.go` |

### Task Dependencies

```text
Task 1 (host functions) ──┬──> Task 4 (building Lua)
                          │
Task 2 (properties) ──────┴──> Task 5 (objects Go)
                          │
Task 3 (manifest) ────────┘
```

## Verification

1. `dig north to "Test Room"` creates location and exit
2. `dig north to "Test" return south` creates bidirectional exits
3. `link east to "Test Room"` links by name
4. `link east to #<id>` links by ID
5. `create object "Sword"` creates object in current location
6. `set description of here to ...` updates location description
7. `set desc of here to ...` works (prefix matching)
8. `set d of here to ...` errors if ambiguous

## Out of Scope

- `get` command (read properties) - future
- `examine` command - may use existing `look`
- Property visibility levels - future
- Custom property types - future
