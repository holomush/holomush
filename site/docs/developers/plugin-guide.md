# Plugin Guide

HoloMUSH supports two plugin types for extending game functionality:

| Type   | Language | Use Case                        | Performance |
| ------ | -------- | ------------------------------- | ----------- |
| Lua    | Lua 5.1  | Simple scripts, rapid iteration | Fast        |
| Binary | Go       | Complex logic, external APIs    | Fastest     |

Both plugin types use the same event-driven model: plugins receive events and can
emit new events in response.

## Lua Plugins

Lua plugins are ideal for simple game logic. They run in a sandboxed Lua VM and
require no compilation.

### Project Structure

```text
plugins/my-plugin/
├── plugin.yaml    # Plugin manifest
└── main.lua       # Entry point
```

### Manifest (plugin.yaml)

```yaml
name: my-plugin
version: 1.0.0
type: lua
events:
  - say
capabilities:
  - events.emit.location
lua-plugin:
  entry: main.lua
```

| Field          | Required | Description                                    |
| -------------- | -------- | ---------------------------------------------- |
| `name`         | Yes      | Unique identifier (lowercase, a-z0-9, hyphens) |
| `version`      | Yes      | Semantic version (e.g., 1.0.0)                 |
| `type`         | Yes      | Must be `lua` for Lua plugins                  |
| `events`       | No       | Event types to receive                         |
| `capabilities` | No       | Required capabilities                          |
| `lua-plugin`   | Yes      | Lua-specific configuration                     |

### Event Handler

Lua plugins implement a single `on_event` function:

```lua
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

function on_event(event)
    -- Only respond to say events
    if event.type ~= "say" then
        return nil
    end

    -- Don't echo plugin messages (prevents loops)
    if event.actor_kind == "plugin" then
        return nil
    end

    -- Parse message from payload
    local msg = event.payload:match('"message":"([^"]*)"')
    if not msg then
        return nil
    end

    -- Return events to emit
    return {
        {
            stream = event.stream,
            type = "say",
            payload = '{"message":"Echo: ' .. msg .. '"}'
        }
    }
end
```

### Event Structure

The `event` table passed to `on_event` contains:

| Field        | Type   | Description                          |
| ------------ | ------ | ------------------------------------ |
| `id`         | string | Unique event ID (ULID)               |
| `stream`     | string | Event stream (e.g., location:room1)  |
| `type`       | string | Event type (say, pose, arrive, etc.) |
| `timestamp`  | number | Unix milliseconds                    |
| `actor_kind` | string | "character", "system", or "plugin"   |
| `actor_id`   | string | Actor identifier                     |
| `payload`    | string | JSON-encoded event data              |

### Host Functions

Lua plugins can call host functions via the `holomush` global:

```lua
-- Logging (no capability required)
holomush.log("info", "Plugin loaded")
holomush.log("debug", "Processing event")
holomush.log("warn", "Something unexpected")
holomush.log("error", "Failed to process")

-- Request ID generation (no capability required)
local id = holomush.new_request_id()

-- Key-value storage (requires kv.read or kv.write capability)
local value, err = holomush.kv_get("my-key")
local _, err = holomush.kv_set("my-key", "my-value")
local _, err = holomush.kv_delete("my-key")
```

## Binary Plugins

Binary plugins are Go programs that communicate with HoloMUSH over gRPC using
HashiCorp's go-plugin system. They offer maximum performance and access to
Go's ecosystem.

### Project Structure

```text
plugins/my-binary-plugin/
├── plugin.yaml    # Plugin manifest
├── main.go        # Plugin source
└── my-plugin      # Compiled executable
```

### Manifest (plugin.yaml)

```yaml
name: my-binary-plugin
version: 1.0.0
type: binary
events:
  - say
capabilities:
  - events.emit.location
binary-plugin:
  executable: my-plugin
```

### Implementation

Binary plugins import `github.com/holomush/holomush/pkg/plugin` and implement
the `Handler` interface:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "context"
    "encoding/json"

    "github.com/holomush/holomush/pkg/plugin"
)

type EchoPlugin struct{}

func (p *EchoPlugin) HandleEvent(ctx context.Context, event plugin.Event) ([]plugin.EmitEvent, error) {
    // Only respond to say events
    if event.Type != plugin.EventTypeSay {
        return nil, nil
    }

    // Don't echo plugin messages
    if event.ActorKind == plugin.ActorPlugin {
        return nil, nil
    }

    // Parse payload
    var payload struct {
        Message string `json:"message"`
    }
    if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
        return nil, nil
    }

    // Emit echo response
    responsePayload, _ := json.Marshal(map[string]string{
        "message": "Echo: " + payload.Message,
    })

    return []plugin.EmitEvent{
        {
            Stream:  event.Stream,
            Type:    plugin.EventTypeSay,
            Payload: string(responsePayload),
        },
    }, nil
}

func main() {
    plugin.Serve(&plugin.ServeConfig{
        Handler: &EchoPlugin{},
    })
}
```

### Building

```bash
cd plugins/my-binary-plugin
go build -o my-plugin .
```

### SDK Types

The plugin SDK provides these core types:

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

## Event Types

Both plugin types handle the same event types:

| Type     | Description       | Payload                   |
| -------- | ----------------- | ------------------------- |
| `say`    | Character speech  | `{"message": "text"}`     |
| `pose`   | Character action  | `{"message": "text"}`     |
| `arrive` | Character arrives | `{"character_id": "..."}` |
| `leave`  | Character leaves  | `{"character_id": "..."}` |
| `system` | System message    | `{"message": "text"}`     |

## Capabilities

Plugins declare required capabilities in their manifest. The capability system
uses glob patterns:

| Pattern               | Matches                |
| --------------------- | ---------------------- |
| `events.emit.*`       | Direct children only   |
| `events.emit.**`      | All descendants        |
| `world.read.location` | Exact match only       |
| `kv.read`             | Key-value read access  |
| `kv.write`            | Key-value write access |

Example capabilities:

```yaml
capabilities:
  - events.emit.location # Emit events to locations
  - kv.read # Read from key-value store
  - kv.write # Write to key-value store
```

## Best Practices

### Avoid Echo Loops

Always check `actor_kind` to avoid responding to your own events:

=== "Lua"

    ```lua
    if event.actor_kind == "plugin" then
        return nil
    end
    ```

=== "Go"

    ```go
    if event.ActorKind == plugin.ActorPlugin {
        return nil, nil
    }
    ```

### Return Early

If an event doesn't match your criteria, return immediately:

=== "Lua"

    ```lua
    if event.type ~= "say" then
        return nil
    end
    ```

=== "Go"

    ```go
    if event.Type != plugin.EventTypeSay {
        return nil, nil
    }
    ```

### Handle Missing Data

Check for missing or invalid data in payloads:

=== "Lua"

    ```lua
    local msg = event.payload:match('"message":"([^"]*)"')
    if not msg or msg == "" then
        return nil
    end
    ```

=== "Go"

    ```go
    var payload struct {
        Message string `json:"message"`
    }
    if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
        return nil, nil // Skip invalid payloads
    }
    if payload.Message == "" {
        return nil, nil
    }
    ```

### Keep Handlers Fast

Plugin handlers have a 5-second timeout. If exceeded:

- The call fails with a timeout error
- The event is skipped for that plugin
- The plugin continues receiving future events

For slow operations, consider caching results or offloading work to external
systems.

## Stream Patterns

Events are organized into streams. Subscribe to streams using patterns:

| Pattern          | Matches                       |
| ---------------- | ----------------------------- |
| `location:*`     | All location events           |
| `location:room1` | Specific location only        |
| `global:*`       | All global events             |
| `character:*`    | All character-specific events |
| `*`              | Everything                    |

## Example: Dice Roller

A complete example showing both plugin types:

=== "Lua"

    ```lua
    -- plugins/dice/main.lua
    -- Responds to "roll XdY" commands

    function on_event(event)
        if event.type ~= "say" then
            return nil
        end

        local msg = event.payload:match('"message":"([^"]*)"')
        if not msg then
            return nil
        end

        -- Match "roll XdY" pattern
        local count, sides = msg:match("roll%s+(%d+)d(%d+)")
        if not count then
            return nil
        end

        count = tonumber(count)
        sides = tonumber(sides)

        if count < 1 or count > 100 or sides < 2 or sides > 100 then
            return nil
        end

        -- Roll dice
        local total = 0
        local results = {}
        for i = 1, count do
            local roll = math.random(1, sides)
            total = total + roll
            table.insert(results, tostring(roll))
        end

        local result_str = table.concat(results, " + ")
        local response = string.format("Rolled %dd%d: %s = %d",
            count, sides, result_str, total)

        return {
            {
                stream = event.stream,
                type = "say",
                payload = '{"message":"' .. response .. '"}'
            }
        }
    end
    ```

=== "Go"

    ```go
    // plugins/dice/main.go
    package main

    import (
        "context"
        "encoding/json"
        "fmt"
        "math/rand"
        "regexp"
        "strconv"
        "strings"

        "github.com/holomush/holomush/pkg/plugin"
    )

    var dicePattern = regexp.MustCompile(`roll\s+(\d+)d(\d+)`)

    type DicePlugin struct{}

    func (p *DicePlugin) HandleEvent(ctx context.Context, event plugin.Event) ([]plugin.EmitEvent, error) {
        if event.Type != plugin.EventTypeSay {
            return nil, nil
        }

        var payload struct {
            Message string `json:"message"`
        }
        if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
            return nil, nil
        }

        matches := dicePattern.FindStringSubmatch(payload.Message)
        if matches == nil {
            return nil, nil
        }

        count, _ := strconv.Atoi(matches[1])
        sides, _ := strconv.Atoi(matches[2])

        if count < 1 || count > 100 || sides < 2 || sides > 100 {
            return nil, nil
        }

        var total int
        var results []string
        for i := 0; i < count; i++ {
            roll := rand.Intn(sides) + 1
            total += roll
            results = append(results, strconv.Itoa(roll))
        }

        response := fmt.Sprintf("Rolled %dd%d: %s = %d",
            count, sides, strings.Join(results, " + "), total)

        responsePayload, _ := json.Marshal(map[string]string{
            "message": response,
        })

        return []plugin.EmitEvent{
            {
                Stream:  event.Stream,
                Type:    plugin.EventTypeSay,
                Payload: string(responsePayload),
            },
        }, nil
    }

    func main() {
        plugin.Serve(&plugin.ServeConfig{
            Handler: &DicePlugin{},
        })
    }
    ```

## Next Steps

- Review the [echo-bot example](https://github.com/holomush/holomush/tree/main/plugins/echo-bot)
- Learn about the [Event System](events.md)
- Explore [Host Functions](plugins/host-functions.md) for advanced capabilities
