---
title: "Plugin API reference"
---

Reference for the plugin API surface: the event structure, host functions,
world-query functions, Go SDK types, common ABAC policy patterns, and host
function error types. For a build-along walkthrough, see the
[Plugin Guide](/extending/tutorials/plugin-guide/); for error-handling
procedure, see [Handle plugin errors](/extending/how-to/handle-plugin-errors/).

## Event structure

The `event` table passed to a Lua `on_event` handler (and the fields of the Go
`plugin.Event` struct) contains:

| Field        | Type   | Description                          |
| ------------ | ------ | ------------------------------------ |
| `id`         | string | Unique event ID (ULID)               |
| `stream`     | string | Event stream (e.g., `location:<id>`) |
| `type`       | string | Event type (say, pose, arrive, etc.) |
| `timestamp`  | number | Unix milliseconds                    |
| `actor_kind` | string | "character", "system", or "plugin"   |
| `actor_id`   | string | Actor identifier                     |
| `payload`    | string | JSON-encoded event data              |

## Host functions

Lua plugins call host functions via the `holomush` global:

```lua
-- Logging (no capability required)
holomush.log("info", "Plugin loaded")
holomush.log("debug", "Processing event")
holomush.log("warn", "Something unexpected")
holomush.log("error", "Failed to process")

-- Request ID generation (no capability required)
local id = holomush.new_request_id()

-- Key-value storage (enforced via ABAC)
local value, err = holomush.kv_get("my-key")
holomush.kv_set("my-key", "my-value")
holomush.kv_delete("my-key")
```

## World-query functions

World queries are enforced via ABAC policies declared in `plugin.yaml`. Each
returns the result on success, or `nil, error_message` on failure.

```lua
-- Query location information
local location, err = holomush.query_location(location_id)
-- Returns: table with id, name, description, type

-- Query character information
local char, err = holomush.query_character(character_id)
-- Returns: table with id, name, player_id, location_id

-- Query characters in a location
local chars, err = holomush.query_location_characters(location_id)
-- Returns: array of character tables

-- Query object information
local obj, err = holomush.query_object(object_id)
-- Returns: table with id, name, description, is_container, owner_id, containment_type
```

Example manifest with world-query policies:

```yaml
name: world-aware-plugin
version: 1.0.0
type: lua
events:
  - say
policies:
  - name: "read-world"
    dsl: |
      permit(principal is plugin, action in ["read"], resource is world_object) when {
        principal.plugin.name == "world-aware-plugin" &&
        (resource like "location:*" || resource like "character:*")
      };
lua-plugin:
  entry: main.lua
```

## SDK types

The Go plugin SDK provides these core types:

```go
// Event received by plugins
type Event struct {
    ID        string
    Stream    string
    Type      EventType
    Timestamp int64     // Unix milliseconds
    ActorKind ActorKind
    ActorID   string
    Payload   string    // JSON string
}

// Event to emit in response
type EmitEvent struct {
    Stream  string
    Type    EventType
    Payload string // JSON string
}

// Event types
const (
    EventTypeSay    EventType = "say"
    EventTypePose   EventType = "pose"
    EventTypeArrive EventType = "arrive"
    EventTypeLeave  EventType = "leave"
    EventTypeSystem EventType = "system"
)

// Actor kinds
const (
    ActorCharacter ActorKind = iota
    ActorSystem
    ActorPlugin
)
```

## Common policy patterns

Plugins declare access in their manifest using Cedar-style DSL (default-deny).
For task-focused recipes, see
[Plugin Access Control](/extending/how-to/access-control/). At a glance:

| Access Needed          | Action      | Resource Pattern |
| ---------------------- | ----------- | ---------------- |
| Emit events to streams | `"emit"`    | `"stream:*"`     |
| Read locations         | `"read"`    | `"location:*"`   |
| Read characters        | `"read"`    | `"character:*"`  |
| Read objects           | `"read"`    | `"object:*"`     |
| Key-value read         | `"read"`    | `"kv:*"`         |
| Key-value write        | `"write"`   | `"kv:*"`         |
| Key-value delete       | `"delete"`  | `"kv:*"`         |
| Execute commands       | `"execute"` | `"command:*"`    |

## Host function error types

Host functions return errors as a second return value. For the handling pattern
and correlation-ID workflow, see
[Handle plugin errors](/extending/how-to/handle-plugin-errors/).

| Error Message                     | Cause                             | Recovery                          |
| --------------------------------- | --------------------------------- | --------------------------------- |
| `"location not found"`            | Location ID doesn't exist         | Check ID validity, handle missing |
| `"character not found"`           | Character ID doesn't exist        | Check ID validity, handle missing |
| `"object not found"`              | Object ID doesn't exist           | Check ID validity, handle missing |
| `"access denied"`                 | Plugin lacks required ABAC policy | Add policy to manifest            |
| `"query timed out"`               | Query exceeded 5-second timeout   | Simplify query or retry later     |
| `"internal error (ref: XXXX...)"` | Server error with correlation ID  | Log and surface to user           |
