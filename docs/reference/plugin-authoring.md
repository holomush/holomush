# Plugin Authoring Guide

This guide covers writing HoloMUSH plugins using the Extism Plugin Development Kit (PDK).

## Overview

HoloMUSH uses [Extism](https://extism.org/) for its plugin system, which allows plugins to be written in any language with an Extism PDK:

- Python
- Rust
- Go
- JavaScript/TypeScript
- AssemblyScript
- C/C++
- Zig
- Haskell

Plugins compile to WebAssembly (WASM) and run in a sandboxed environment.

## Quick Start (Python)

### Prerequisites

- Python 3.11+
- [uv](https://github.com/astral-sh/uv) (recommended) or pip

### Project Setup

Create a new plugin directory:

```bash
mkdir my-plugin
cd my-plugin
```

Create `pyproject.toml`:

```toml
[project]
name = "my-plugin"
version = "0.1.0"
requires-python = ">=3.11"
dependencies = ["extism-pdk"]
```

Create `plugin.py`:

```python
"""My custom plugin."""

import json
import extism


@extism.plugin_fn
def handle_event():
    """Handle incoming events."""
    # Read input event
    event_json = extism.input_str()
    event = json.loads(event_json)

    # Process the event...
    event_type = event.get("type")
    payload = json.loads(event.get("payload", "{}"))

    # Optionally emit response events
    response = {
        "events": [{
            "stream": event.get("stream"),
            "type": "say",
            "payload": json.dumps({"message": "Hello from plugin!"})
        }]
    }

    extism.output_str(json.dumps(response))
```

### Building

Install the Extism Python CLI:

```bash
pip install extism-cli
# or with uv:
uv pip install extism-cli
```

Build the plugin:

```bash
extism-py plugin.py -o my-plugin.wasm
```

## Event Structure

### Input Event

Plugins receive events as JSON with the following structure:

| Field        | Type   | Description                                 |
| ------------ | ------ | ------------------------------------------- |
| `id`         | string | Unique event ID (ULID)                      |
| `stream`     | string | Event stream (e.g., "location:room1")       |
| `type`       | string | Event type (e.g., "say", "pose", "arrive")  |
| `timestamp`  | int64  | Unix milliseconds timestamp                 |
| `actor_kind` | uint8  | Actor type: 0=character, 1=system, 2=plugin |
| `actor_id`   | string | Actor identifier                            |
| `payload`    | string | JSON-encoded event payload                  |

Example input:

```json
{
  "id": "01JGXYZ123ABC456DEF789GHI",
  "stream": "location:room1",
  "type": "say",
  "timestamp": 1737195000000,
  "actor_kind": 0,
  "actor_id": "player123",
  "payload": "{\"message\": \"Hello, world!\"}"
}
```

### Output Response

Plugins return a JSON object with emitted events:

```json
{
  "events": [
    {
      "stream": "location:room1",
      "type": "say",
      "payload": "{\"message\": \"Response message\"}"
    }
  ]
}
```

### Empty Output

All of these are valid ways to indicate "no action taken":

1. **Return without calling `extism.output_str()`** - The function simply returns
   without producing output
2. **Output an empty JSON object** - `extism.output_str("{}")`
3. **Output an empty events array** - `extism.output_str('{"events": []}')`

Example of returning early without output:

```python
@extism.plugin_fn
def handle_event():
    event = json.loads(extism.input_str())

    if event.get("type") != "say":
        return  # No output call - valid, means "no action"

    # ... handle say events
```

## Event Types

| Type     | Description               | Payload                   |
| -------- | ------------------------- | ------------------------- |
| `say`    | Character speech          | `{"message": "text"}`     |
| `pose`   | Character action/emote    | `{"message": "text"}`     |
| `arrive` | Character arrives at room | `{"character_id": "..."}` |
| `leave`  | Character leaves room     | `{"character_id": "..."}` |
| `system` | System-generated message  | `{"message": "text"}`     |

## Stream Patterns

Plugins subscribe to events using stream patterns:

| Pattern          | Matches                       |
| ---------------- | ----------------------------- |
| `location:*`     | All location events           |
| `location:room1` | Events in specific location   |
| `global:*`       | All global events             |
| `character:*`    | All character-specific events |

### Pattern Matching Rules

The `*` wildcard matches any sequence of characters (including empty strings):

| Pattern          | Stream           | Matches? | Why                                  |
| ---------------- | ---------------- | -------- | ------------------------------------ |
| `*`              | `location:room1` | Yes      | Single `*` matches everything        |
| `*`              | `global:chat`    | Yes      | Single `*` matches everything        |
| `*`              | (empty)          | Yes      | Single `*` matches empty strings too |
| `location:*`     | `location:room1` | Yes      | `*` matches `room1`                  |
| `location:*`     | `location:`      | Yes      | `*` matches empty string after `:`   |
| `location:room1` | `location:room1` | Yes      | Exact match                          |
| `location:room1` | `location:room2` | No       | No wildcard, requires exact match    |
| (empty)          | (empty)          | Yes      | Empty pattern matches empty stream   |
| (empty)          | `location:room1` | No       | Empty pattern only matches empty     |

### Edge Cases

**Single `*` pattern**: Matches all streams, including empty streams. Use this when a
plugin needs to receive every event in the system.

**Empty pattern**: Only matches events with an empty stream name. This is rarely
useful in practice since most events have stream names.

**Patterns without `*`**: Require an exact match. The pattern `location:room1` only
matches the stream `location:room1`, not `location:room123` or `location:room1:sub`.

## Best Practices

### Avoid Echo Loops

Always check `actor_kind` to avoid responding to your own events:

```python
if event.get("actor_kind") == 2:  # ActorPlugin
    return  # Ignore events from plugins
```

### Handle Missing Fields

Use `.get()` with defaults for optional fields:

```python
message = payload.get("message", "")
if not message:
    return
```

### Keep Plugins Fast

Plugins have a 5-second timeout for each event delivery. When a plugin exceeds
this timeout:

- The plugin call returns an error to the event bus
- The event is **not** retried - it is permanently skipped for that plugin
- The plugin remains loaded and will receive future events normally

Keep processing fast and avoid blocking operations. If you need to perform
slow operations (network calls, heavy computation), consider:

- Caching results where possible
- Breaking work into smaller chunks
- Offloading work to external systems via the API

### Return Early

If the event doesn't match your criteria, return early without output:

```python
if event.get("type") != "say":
    return  # Nothing to do
```

### Concurrency and Thread Safety

**Important:** WebAssembly plugins are NOT thread-safe for concurrent calls. This is
a fundamental WASM limitation that applies to all plugins regardless of source
language (Python, Rust, Go, etc.).

**Why:** WASM linear memory is shared between the host application and plugin. When
multiple callers access the same plugin instance simultaneously, memory corruption
occurs.

**What this means for plugin authors:**

- Your plugin will only receive one event at a time (HoloMUSH serializes delivery)
- You don't need to add locks or synchronization in your plugin code
- Each `handle_event` call completes before the next one starts
- Global state in your plugin is safe to use without thread protection

**What HoloMUSH does:** The event subscriber delivers events to each plugin
sequentially—while your plugin processes one event, it won't receive another.
This happens transparently; you just write normal single-threaded code.

## Testing Locally

### Unit Testing

Test your plugin logic with standard Python tests before compiling to WASM.

### Integration Testing

Run the HoloMUSH integration tests with your plugin:

```bash
# Copy your plugin to testdata
cp my-plugin.wasm internal/wasm/testdata/

# Run integration tests
go test -v -tags=integration ./internal/wasm/...
```

## Example: Echo Plugin

A complete working example is in `plugins/echo-python/`:

```python
"""Echo plugin - responds to say events with echoed message."""

import json
import extism


@extism.plugin_fn
def handle_event():
    """Handle incoming events and emit echo responses."""
    event_json = extism.input_str()
    event = json.loads(event_json)

    # Only respond to "say" events from characters
    if event.get("type") != "say":
        return

    if event.get("actor_kind") == 2:  # Ignore plugin events
        return

    payload = json.loads(event.get("payload", "{}"))
    message = payload.get("message", "")

    if not message:
        return

    response = {
        "events": [{
            "stream": event.get("stream"),
            "type": "say",
            "payload": json.dumps({"message": f"Echo: {message}"})
        }]
    }

    extism.output_str(json.dumps(response))
```

## Resources

- [Extism Documentation](https://extism.org/docs)
- [Extism Python PDK](https://github.com/extism/python-pdk)
- [HoloMUSH Architecture](../plans/2026-01-17-holomush-architecture-design.md)
