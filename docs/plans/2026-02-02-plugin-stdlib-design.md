# Plugin Standard Library Design

**Date:** 2026-02-02
**Status:** Draft
**Related:** [Commands & Behaviors Implementation](2026-02-02-commands-behaviors-implementation.md)

## Overview

This document defines the plugin standard library (stdlib) for HoloMUSH. The stdlib provides common functionality to both Lua and Go plugins, reducing boilerplate and ensuring consistent behavior.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Implementation | Go-core, Lua-surface | Single implementation, two interfaces. Lua calls Go via host functions. |
| Output targeting | Dual-target (semantic) | Plugins emit semantic structure; output layer renders per client type. |
| Formatting scope | Standard | Text styling, lists, tables, columns, headers. Rich formatting deferred. |
| Data handling | Typed command context | Pre-parsed context object eliminates JSON handling in plugins. |
| Namespace | Nested tables | `holo.fmt.*`, `holo.emit.*` mirrors Go package structure. |

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                   Lua Plugin                        │
│  on_command(ctx)                                    │
│    local msg = holo.fmt.table(data)                 │
│    holo.emit.location(ctx.location_id, "say", msg)  │
└─────────────────────────────────────────────────────┘
                        │ host function calls
                        ▼
┌─────────────────────────────────────────────────────┐
│              pkg/holo (Go stdlib)                   │
│  holo.Fmt.Table()   holo.Emit.Location()           │
│  holo.Color.Red()   holo.Parse.Args()              │
└─────────────────────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────┐
│              internal/plugin/hostfunc               │
│  Registers stdlib as Lua globals                    │
│  Handles Lua ↔ Go type conversion                   │
└─────────────────────────────────────────────────────┘
```

### Key Files

| Path | Purpose |
|------|---------|
| `pkg/holo/fmt.go` | Formatting primitives + %x code parsing |
| `pkg/holo/emit.go` | Event emitter |
| `pkg/holo/context.go` | CommandContext struct |
| `pkg/holo/codes.go` | MU* format code definitions |
| `internal/plugin/hostfunc/stdlib.go` | Lua bindings for stdlib |
| `internal/plugin/lua/context.go` | Typed CommandContext builder |

## Typed Command Context

Command handlers receive a pre-parsed context object instead of raw events.

### Lua Handler Signature

**Before (current):**
```lua
function on_event(event)
    if event.type ~= "command" then return nil end
    local cmd = event.payload:match('"name":"([^"]*)"')
    local args = event.payload:match('"args":"([^"]*)"')
    -- ... brittle JSON parsing ...
end
```

**After (with stdlib):**
```lua
function on_command(ctx)
    -- ctx.name           "say"
    -- ctx.args           "Hello everyone!"
    -- ctx.invoked_as     ";" (if alias used)
    -- ctx.character_name "Alice"
    -- ctx.character_id   "01ABC..."
    -- ctx.location_id    "01DEF..."
    -- ctx.player_id      "01GHI..."
end
```

### Handler Selection

The manifest declares which events the plugin handles:

```yaml
events:
  - command    # → calls on_command(ctx) if defined, else on_event(event)
  - say        # → calls on_event(event)
```

The Lua host checks for `on_command` when delivering command events; falls back to `on_event` for backwards compatibility.

### Go Handler Signature

```go
func (p *MyPlugin) HandleCommand(ctx context.Context, cmd holo.CommandContext) ([]plugin.EmitEvent, error) {
    // cmd.Name, cmd.Args, cmd.CharacterName, etc. available
}
```

## Formatting Primitives

### Function-Based Formatting

Plugins use `holo.fmt.*` functions to create semantic formatting:

```lua
-- Text styling
holo.fmt.bold("important")        -- semantic bold
holo.fmt.italic("whispered")      -- semantic italic
holo.fmt.color("red", "danger!")  -- named colors
holo.fmt.dim("(quietly)")         -- muted/gray text

-- Structure
holo.fmt.list({"sword", "shield", "potion"})

holo.fmt.pairs({HP = 100, MP = 50, Level = 5})

holo.fmt.table({
    headers = {"Name", "Location", "Idle"},
    rows = {
        {"Alice", "Town Square", "2m"},
        {"Bob", "Forest", "5m"},
    }
})

holo.fmt.separator()
holo.fmt.header("Inventory")
```

### Inline Format Codes

Text supports MU*-compatible `%x` codes for inline styling:

```lua
local msg = "This is %xhbold%xn and %xrred%xn text."
```

#### Style Codes

| Code | Effect | Code | Effect |
|------|--------|------|--------|
| `%xn` | Reset/normal | `%xh` | Bold |
| `%xu` | Underline | `%xi` | Italic |
| `%xd` | Dim | | |

#### Color Codes

| Code | Color | Code | Bright |
|------|-------|------|--------|
| `%xr` | Red | `%xR` | Bright red |
| `%xg` | Green | `%xG` | Bright green |
| `%xb` | Blue | `%xB` | Bright blue |
| `%xc` | Cyan | `%xC` | Bright cyan |
| `%xm` | Magenta | `%xM` | Bright magenta |
| `%xy` | Yellow | `%xY` | Bright yellow |
| `%xw` | White | `%xW` | Bright white |
| `%xx` | Black | `%x##` | 256-color |

#### Whitespace Codes

| Code | Effect |
|------|--------|
| `%r` | Newline |
| `%b` | Space |
| `%t` | Tab (4 spaces) |

### Rendering

The `holo.fmt.*` functions return an intermediate representation. The output layer renders based on client type:

- **Telnet** → ANSI escape codes, fixed-width columns
- **Web** → HTML/semantic markup (future)

## Event Emission

Plugins emit events via `holo.emit.*` functions with accumulate + flush pattern.

### Lua API

```lua
-- Emit to location stream
holo.emit.location(ctx.location_id, "say", {
    message = msg,
    speaker = ctx.character_name
})

-- Emit to character stream (private message)
holo.emit.character(target_id, "tell", {
    message = msg,
    sender = ctx.character_name
})

-- Emit to global stream
holo.emit.global("announcement", {
    message = "Server restarting in 5 minutes"
})

-- Return accumulated emits
return holo.emit.flush()
```

### Go API

```go
emitter := holo.NewEmitter()
emitter.Location(cmd.LocationID, plugin.EventTypeSay, holo.Payload{
    "message": msg,
    "speaker": cmd.CharacterName,
})
return emitter.Flush(), nil
```

### Benefits Over Current Approach

**Before (boilerplate):**
```lua
return {
    {
        stream = "location:" .. location_id,
        type = "say",
        payload = '{"message":"' .. escape_json(msg) .. '","speaker":"' .. escape_json(name) .. '"}'
    }
}
```

**After (stdlib):**
```lua
holo.emit.location(ctx.location_id, "say", {message = msg, speaker = ctx.character_name})
return holo.emit.flush()
```

The stdlib handles:
- Stream name construction
- JSON encoding with proper escaping
- Event accumulation

## Go Package Structure

```
pkg/holo/
├── fmt.go        // Fmt.Bold(), Fmt.Table(), Fmt.Parse()
├── emit.go       // Emitter, Location(), Character(), Global()
├── context.go    // CommandContext struct
└── codes.go      // %x code parsing and definitions
```

Go plugins import `pkg/holo` directly. Lua plugins access the same functionality via host function bindings in `internal/plugin/hostfunc/stdlib.go`.

## Implementation Phases

| Phase | Scope | Dependencies |
|-------|-------|--------------|
| 1 | `pkg/holo/fmt.go` — formatting primitives + %x codes | None |
| 2 | `pkg/holo/emit.go` — event emitter | None |
| 3 | `internal/plugin/hostfunc/stdlib.go` — Lua bindings | Phases 1-2 |
| 4 | `internal/plugin/lua/context.go` — typed CommandContext | Phase 3 |
| 5 | Migrate communication plugin, update tests | Phase 4 |

## Out of Scope (Backlog)

- **Rich formatting**: Images, clickable commands, collapsible sections
- **holo.json.\***: General JSON encode/decode for custom payloads (add if needed)

## Migration Example

The communication plugin reduces from ~183 lines to ~30:

```lua
function on_command(ctx)
    if ctx.name == "say" then
        if ctx.args == "" then return end
        local sep = " "
        if ctx.invoked_as == ";" then sep = "" end
        local msg = ctx.character_name .. ' says, "' .. ctx.args .. '"'
        holo.emit.location(ctx.location_id, "say", {
            message = msg,
            speaker = ctx.character_name
        })
    elseif ctx.name == "pose" then
        if ctx.args == "" then return end
        local sep = " "
        if ctx.invoked_as == ";" then sep = "" end
        local msg = ctx.character_name .. sep .. ctx.args
        holo.emit.location(ctx.location_id, "pose", {
            message = msg,
            actor = ctx.character_name
        })
    elseif ctx.name == "emit" then
        if ctx.args == "" then return end
        holo.emit.location(ctx.location_id, "emit", {message = ctx.args})
    end
    return holo.emit.flush()
end
```
