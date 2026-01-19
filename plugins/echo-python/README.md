# Echo Plugin (Python)

A simple echo plugin implemented in Python using the Extism PDK. This plugin demonstrates how to write HoloMUSH plugins in Python that compile to WebAssembly.

## Building

Install the Extism Python PDK:

```bash
curl -Ls https://raw.githubusercontent.com/extism/python-pdk/main/install.sh | bash
```

Build the plugin:

```bash
task plugin:build:echo-python
# Or manually:
extism-py plugin.py -o echo.wasm
```

This produces `echo.wasm` which can be loaded by the HoloMUSH plugin host.

## Behavior

The echo plugin:

- Listens for `say` events from players
- Ignores events from other plugins (to prevent echo loops)
- Responds with "Echo: \<original message\>"

## Example

When a player says "Hello world", the plugin emits a response event with the message "Echo: Hello world".

## Plugin API

The plugin exports a single function:

- `handle_event` - Receives events as JSON, returns response events as JSON

### Input Format

```json
{
  "id": "01ABC123...",
  "stream": "location:room1",
  "type": "say",
  "timestamp": 1737208800000,
  "actor_kind": 0,
  "actor_id": "char1",
  "payload": "{\"message\": \"Hello world\"}"
}
```

Note: `timestamp` is Unix milliseconds (int64), not an ISO 8601 string.

### Output Format

```json
{
  "events": [
    {
      "stream": "location:room1",
      "type": "say",
      "payload": "{\"message\": \"Echo: Hello world\"}"
    }
  ]
}
```
