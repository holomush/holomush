---
title: "Getting Started with Plugins"
---

This guide walks you through creating your first HoloMUSH plugin. By the end,
you will have a working Lua plugin that responds to in-game speech.

## Prerequisites

You need a running HoloMUSH server. If you have not set one up yet, follow the
[Installation Guide](../operating/installation.md) first.

You also need a text editor and a way to connect to the server (telnet or the
web client).

## Create the Plugin Directory

Plugins live in the `plugins/` directory under the HoloMUSH data path. Each
plugin gets its own subdirectory:

```bash
mkdir -p plugins/greeter
```

Your plugin needs two files: a manifest that tells the server what your plugin
does, and a Lua script that handles events.

## Write the Manifest

Create `plugins/greeter/plugin.yaml`:

```yaml
name: greeter
version: 1.0.0
type: lua
events:
  - say
policies:
  - name: "emit-events"
    dsl: |
      permit(principal is plugin, action in ["emit"], resource is stream) when {
        principal.plugin.name == "greeter"
      };
lua-plugin:
  entry: main.lua
```

This tells HoloMUSH:

- **name** -- A unique identifier for your plugin.
- **type** -- This is a Lua plugin (no compilation needed).
- **events** -- Subscribe to `say` events (character speech).
- **policies** -- Request permission to emit events back to streams.
- **lua-plugin.entry** -- The Lua file to load.

## Write the Event Handler

Create `plugins/greeter/main.lua`:

```lua
function on_event(event)
    -- Only respond to say events
    if event.type ~= "say" then
        return nil
    end

    -- Ignore messages from other plugins (prevents loops)
    if event.actor_kind == "plugin" then
        return nil
    end

    -- Parse the message from the JSON payload
    local msg = event.payload:match('"message":"([^"]*)"')
    if not msg then
        return nil
    end

    -- Respond to greetings
    if msg:lower():match("^hello") or msg:lower():match("^hi") then
        return {
            {
                stream = event.stream,
                type = "say",
                payload = '{"message":"Hello! Welcome to HoloMUSH."}'
            }
        }
    end

    return nil
end
```

The `on_event` function is called every time an event your plugin subscribed to
arrives. It receives an event table and can return a list of new events to emit,
or `nil` to do nothing.

## Load and Test

Restart the HoloMUSH server to pick up the new plugin. Then connect and try it
out:

```text
> connect testuser password
Welcome back, TestChar!
> say Hello everyone!
You say, "Hello everyone!"
Greeter says, "Hello! Welcome to HoloMUSH."
```

If the plugin does not respond, check the server logs for loading errors. Common
issues:

- **YAML syntax error** -- Validate your `plugin.yaml` with a YAML linter.
- **Missing policy** -- Without the `emit-events` policy, the plugin can receive
  events but cannot send any back.
- **Lua syntax error** -- The server logs the specific line and error message.

## What Just Happened?

Here is the flow:

1. You typed `say Hello everyone!`
2. The server created a `say` event on the location's stream.
3. The greeter plugin received that event (because it subscribes to `say`).
4. Your `on_event` function matched the greeting and returned a new `say` event.
5. The server emitted that event, and everyone in the location saw the response.

## Next Steps

- Read the [Plugin Guide](plugin-guide.md) for the full picture: binary plugins,
  host functions, world queries, ABAC policies, and error handling.
- See the [Event Reference](events.md) for all available event types and their
  payload schemas.
- Browse the [example plugins](https://github.com/holomush/holomush/tree/main/plugins)
  for more patterns.
