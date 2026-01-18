# Echo Plugin (Python)

A simple echo plugin implemented in Python using the Extism PDK. This plugin demonstrates how to write HoloMUSH plugins in Python that compile to WebAssembly.

## Building

Install the Extism Python CLI tool:

```bash
pip install extism-cli
```

Build the plugin:

```bash
make build
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
  "stream": "room:123",
  "type": "say",
  "actor_kind": 1,
  "payload": "{\"message\": \"Hello world\"}"
}
```

### Output Format

```json
{
  "events": [
    {
      "stream": "room:123",
      "type": "say",
      "payload": "{\"message\": \"Echo: Hello world\"}"
    }
  ]
}
```
