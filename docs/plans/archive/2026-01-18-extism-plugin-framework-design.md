# Extism Plugin Framework Design

**Status:** Archived (superseded by roadmap)
**Date:** 2026-01-18
**Task:** holomush-n1o

## Overview

Replace the current wazero-based plugin system with Extism to simplify plugin authoring and expand language support. This design also establishes a plugin-first architecture where all game commands and behaviors are plugins.

### Goals

- Eliminate FFI boilerplate for plugin authors
- Support Python, JavaScript, Go, and Rust plugins
- Enable plugins to extend schema, register commands, and call external APIs
- Establish micro-core architecture with explicit plugin load ordering

### Non-Goals

- Backward compatibility with current plugin format (clean break)
- Sub-millisecond plugin performance (MUSH workloads are human-speed)

## Architecture

### What Changes

| Component         | Current                   | With Extism                  |
| ----------------- | ------------------------- | ---------------------------- |
| Host runtime      | wazero (direct)           | Extism Go SDK (wraps wazero) |
| Plugin protocol   | Custom alloc/handle_event | Extism PDK exports           |
| Memory management | Manual ptr/len            | Automatic                    |
| Plugin languages  | TinyGo only               | Python, JS, Go, Rust         |
| Host functions    | None                      | Game API, HTTP (sandboxed)   |

### What Stays the Same

- Event-oriented architecture (plugins receive/emit events)
- JSON serialization at boundaries
- Plugin subscription model
- 5-second execution timeout

### High-Level Architecture

```text
┌─────────────────────────────────────────────────────────┐
│                    Go Core                              │
├─────────────────────────────────────────────────────────┤
│  PluginHost (Extism)                                    │
│  ├─ LoadPlugin(name, wasm, manifest) → Plugin instance  │
│  ├─ DeliverEvent(plugin, event) → []EmitEvent           │
│  └─ Host Functions:                                     │
│       ├─ holomush_emit_event(json)                      │
│       ├─ holomush_query_location(id) → json             │
│       ├─ holomush_query_object(id) → json               │
│       ├─ holomush_query_character(id) → json            │
│       ├─ holomush_http_get(url) → json (sandboxed)      │
│       └─ holomush_log(level, message)                   │
└─────────────────────────────────────────────────────────┘
```

## Plugin-First Architecture

### Micro-Core

The core provides foundational services only:

| Component            | Rationale                                                     |
| -------------------- | ------------------------------------------------------------- |
| Event system         | Foundation everything depends on                              |
| Session management   | Protocol-level, needs tight integration                       |
| Authentication       | Security boundary                                             |
| Plugin host (Extism) | Cannot bootstrap itself                                       |
| Database primitives  | Plugins need storage                                          |
| Object model         | Locations, characters, objects exist (identity, not behavior) |

### Bundled Plugins

All commands and behaviors ship as replaceable plugins:

| Plugin                   | Provides                 |
| ------------------------ | ------------------------ |
| `holomush-communication` | say, pose, emit, whisper |
| `holomush-navigation`    | move, look, exits        |
| `holomush-messaging`     | page, channels           |
| `holomush-building`      | dig, link                |
| `holomush-admin`         | boot, shutdown, stats    |

## Plugin Authoring

### Current Pain

The echo plugin requires 119 lines, with ~90 lines of FFI boilerplate:

```go
// Manual memory management
var allocOffset uint32 = 1024

//export alloc
func alloc(size uint32) uint32 {
    ptr := allocOffset
    allocOffset += size
    return ptr
}

//export handle_event
func handleEvent(ptr, length uint32) uint64 {
    // Manual memory read with unsafe pointers
    eventJSON := make([]byte, length)
    for i := uint32(0); i < length; i++ {
        eventJSON[i] = *(*byte)(unsafe.Pointer(uintptr(ptr + i)))
    }
    // ... business logic ...
    return (uint64(respPtr) << 32) | uint64(len(respJSON))
}
```

### With Extism PDK

The same plugin in ~30 lines:

```python
import extism
from holomush_pdk import Event, emit_event

def handle_event():
    event = Event.from_input()

    if event.type != "say" or event.actor_kind == "plugin":
        return

    emit_event(
        stream=event.stream,
        type="say",
        payload={"message": f"Echo: {event.payload['message']}"}
    )

extism.plugin_fn(handle_event)
```

### Language Support

| Language       | Status       | Notes                             |
| -------------- | ------------ | --------------------------------- |
| Python         | Official PDK | Priority target                   |
| JavaScript     | Official PDK | Priority target                   |
| Go             | Official PDK | Via TinyGo                        |
| Rust           | Official PDK | For performance-sensitive plugins |
| AssemblyScript | Official PDK | TypeScript-like                   |

### HoloMUSH PDK

We provide thin wrappers (~50-100 lines per language) that:

- Define `Event`, `EmitEvent` types
- Wrap host function calls
- Provide test harness utilities

## Plugin Manifest

```yaml
name: combat-system
version: 1.0.0
priority: 100 # Lower = loads first, higher priority in conflicts

# Capabilities required
capabilities:
  - emit:location:*
  - read:characters
  - read:locations
  - db:migrate
  - http:get:api.example.com

# Dependencies
depends:
  - holomush-communication >= 1.0.0
  - holomush-navigation >= 1.0.0

# Conflicts (cannot coexist)
conflicts:
  - alternate-combat-system

# Load order hints
before:
  - economy-system
after:
  - holomush-navigation

# Schema contributions
schema:
  attributes:
    character:
      - name: health
        type: integer
        default: 100
      - name: combat_stance
        type: string
        default: "neutral"
    location:
      - name: danger_level
        type: integer
        default: 0
  tables:
    - name: combat_logs
      columns:
        - { name: id, type: ulid, primary: true }
        - { name: attacker_id, type: ulid, ref: characters }
        - { name: defender_id, type: ulid, ref: characters }
        - { name: damage, type: integer }
        - { name: timestamp, type: timestamptz }

# Custom commands
commands:
  - name: attack
    pattern: "attack <target>"
    description: "Attack a character in the same room"
    handler: handle_attack
  - name: stance
    pattern: "stance <position>"
    description: "Change combat stance"
    handler: handle_stance
```

## Host Functions

### Game API

| Function                   | Signature                 | Capability                  |
| -------------------------- | ------------------------- | --------------------------- |
| `holomush_emit_event`      | `(json) → void`           | `emit:*` or `emit:<stream>` |
| `holomush_query_location`  | `(location_id) → json`    | `read:locations`            |
| `holomush_query_object`    | `(object_id) → json`      | `read:objects`              |
| `holomush_query_character` | `(char_id) → json`        | `read:characters`           |
| `holomush_log`             | `(level, message) → void` | Always allowed              |

### External Access (Sandboxed)

| Function             | Signature             | Capability           |
| -------------------- | --------------------- | -------------------- |
| `holomush_http_get`  | `(url) → json`        | `http:get:<domain>`  |
| `holomush_http_post` | `(url, body) → json`  | `http:post:<domain>` |
| `holomush_kv_get`    | `(key) → bytes`       | `kv:read`            |
| `holomush_kv_set`    | `(key, value) → void` | `kv:write`           |

### Capability Enforcement

- Host validates capabilities at plugin load time
- Unauthorized calls return error at runtime
- `db:migrate` capability required for schema contributions

## Load Order & Conflict Resolution

### Load Order Resolution

1. Build dependency graph from `depends`, `before`, `after`
2. Topological sort (error if cycle detected)
3. Within same level, sort by `priority` (lower loads first)

### Conflict Handling

| Scenario                          | Resolution                                       |
| --------------------------------- | ------------------------------------------------ |
| Two plugins register same command | Higher `priority` wins, log warning              |
| Two plugins handle same event     | Both receive, ordered by priority                |
| Plugin claims exclusive event     | `exclusive: true` blocks lower-priority handlers |

### Event Handling Order

```text
Event arrives → Plugin A (priority 50) → Plugin B (priority 100) → Plugin C (priority 150)
                    ↓                         ↓                         ↓
              can set "handled=true"    skipped if handled         skipped if handled
```

## Observability

Every host→plugin call MUST be wrapped in an OpenTelemetry span.

**Span attributes:**

- `plugin.name`
- `plugin.function`
- `event.type`
- `event.stream`

This enables tracing plugin execution time, errors, and call patterns.

## Implementation

### Files to Modify/Create

| File                            | Action  | Description                             |
| ------------------------------- | ------- | --------------------------------------- |
| `internal/wasm/host.go`         | Rewrite | Replace wazero with Extism SDK          |
| `internal/wasm/hostfuncs.go`    | New     | Host function implementations           |
| `internal/wasm/capabilities.go` | New     | Capability validation                   |
| `internal/wasm/manifest.go`     | New     | Plugin manifest parsing                 |
| `internal/wasm/loadorder.go`    | New     | Dependency resolution, topological sort |
| `pkg/plugin/event.go`           | Keep    | Event types unchanged                   |
| `plugins/echo/`                 | Rewrite | Port to Extism PDK (Python)             |

### New PluginHost Interface

```go
type PluginHost struct {
    plugins map[string]*extism.Plugin
    caps    *CapabilityChecker
    tracer  trace.Tracer
}

func (h *PluginHost) LoadPlugin(ctx context.Context, name string, wasm []byte, manifest Manifest) error
func (h *PluginHost) DeliverEvent(ctx context.Context, plugin string, event core.Event) ([]plugin.EmitEvent, error)
func (h *PluginHost) Close(ctx context.Context) error
```

### Dependencies

```go
require (
    github.com/extism/go-sdk v1.x
    go.opentelemetry.io/otel v1.x
)
```

Note: Extism Go SDK uses wazero internally.

## Testing Strategy

### Host-Side Testing

| Test Type           | Coverage                                            |
| ------------------- | --------------------------------------------------- |
| Unit tests          | Capability validation, manifest parsing, load order |
| Integration tests   | Load plugin → deliver event → verify response       |
| Host function tests | Mock game state, verify plugin queries              |

### Plugin Test Harness

```python
from holomush_pdk.testing import PluginTestHarness

def test_echo_responds_to_say():
    harness = PluginTestHarness("echo.wasm")

    result = harness.deliver_event({
        "type": "say",
        "stream": "location:1",
        "actor_kind": "character",
        "payload": {"message": "hello"}
    })

    assert len(result.emitted_events) == 1
    assert result.emitted_events[0]["payload"]["message"] == "Echo: hello"
```

**Harness features:**

- Mock host functions
- Capture emitted events
- Verify capability enforcement
- OTel span assertions

## Implementation Phases

### Phase 1: Core Extism Integration

- Replace wazero with Extism Go SDK
- Implement basic `DeliverEvent` with OTel tracing
- Port echo plugin to Python (proof of concept)
- Event-only (no host functions yet)

### Phase 2: Host Functions

- Add game query functions
- Add `emit_event` host function
- Implement capability model
- Manifest parsing (capabilities only)

### Phase 3: External Access

- HTTP functions with domain allowlist
- KV storage for plugin state
- Rate limiting

### Phase 4: Schema & Commands

- Schema contributions (attributes, tables)
- Migration system
- Command registration
- Full manifest support
- Load order resolution

### Phase 5: Bundled Plugins

- Extract core commands to plugins
- `holomush-communication`
- `holomush-navigation`
- `holomush-messaging`
- `holomush-building`
- `holomush-admin`

## Community Health (Extism)

| Metric          | Value              |
| --------------- | ------------------ |
| GitHub Stars    | 5,381              |
| Contributors    | 30                 |
| Latest Release  | v1.13.0 (Nov 2025) |
| License         | BSD-3-Clause       |
| Backing Company | Dylibso            |
| Notable Adopter | Helm v4            |

## Decision Record

| Decision                 | Rationale                                       |
| ------------------------ | ----------------------------------------------- |
| Extism over raw wazero   | Eliminates FFI boilerplate, multi-language PDKs |
| Micro-core architecture  | Maximum flexibility, dogfoods plugin API        |
| YAML manifest            | Human-readable, supports complex schema         |
| Priority-based conflicts | Simple mental model, predictable behavior       |
| OTel tracing             | Observability without plugin author effort      |
| Clean break (no compat)  | Only one plugin exists, not worth complexity    |
