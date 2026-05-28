---
title: "Plugin Access Control"
---

Every host function your plugin calls is checked against the policies in your
`plugin.yaml`. No policy, no access. This guide is a set of recipes: each
section grants one capability. Add the policies your plugin needs and skip the
rest.

For the full DSL reference, operator guide, and capability tables, see the
[Access Control Reference](/reference/access-control/).

## React to events without any policy

A plugin that only reacts to events and returns new events needs no policies.
The event delivery system handles subscriptions from the `events` list in your
manifest; returning events from `on_event` goes through that system, not a host
function:

```yaml
name: echo-bot
version: 1.0.0
type: lua
events:
  - say
lua-plugin:
  entry: main.lua
```

```lua
function on_event(event)
    if event.type ~= "say" or event.actor_kind == "plugin" then
        return nil
    end
    local msg = event.payload:match('"message":"([^"]*)"')
    if not msg then return nil end
    return {
        {
            stream = event.stream,
            type = "say",
            payload = '{"message":"Echo: ' .. msg .. '"}'
        }
    }
end
```

To emit events outside of a direct `on_event` response, you need an emit policy
(next recipe).

## Grant proactive event emission

To emit events proactively (not just as a return value from `on_event`), add an
emit policy:

```yaml
name: announcer
version: 1.0.0
type: lua
events:
  - system
policies:
  - name: "emit-events"
    dsl: |
      permit(principal is plugin, action in ["emit"], resource is stream) when {
        principal.plugin.name == "announcer"
      };
lua-plugin:
  entry: main.lua
```

The `principal.plugin.name` condition scopes the permit to just this plugin —
other plugins can't piggyback on your policy.

## Grant world reads

To read game-world state — who's in a location, character names, objects — add
a read policy scoped to the resource patterns you need:

```yaml
name: greeter
version: 1.0.0
type: lua
events:
  - arrive
policies:
  - name: "read-characters"
    dsl: |
      permit(principal is plugin, action in ["read"], resource is world_object) when {
        principal.plugin.name == "greeter" &&
        resource like "character:*"
      };
  - name: "emit-events"
    dsl: |
      permit(principal is plugin, action in ["emit"], resource is stream) when {
        principal.plugin.name == "greeter"
      };
lua-plugin:
  entry: main.lua
```

The `resource like "character:*"` pattern grants character reads but not
locations or objects — grant exactly what you need. To also read locations,
widen the pattern:

```yaml
  - name: "read-world"
    dsl: |
      permit(principal is plugin, action in ["read"], resource is world_object) when {
        principal.plugin.name == "greeter" &&
        (resource like "character:*" || resource like "location:*")
      };
```

## Grant key-value storage

To persist data between events (scores, settings, cooldowns), grant access to
the key-value store:

```yaml
name: dice-tracker
version: 1.0.0
type: lua
events:
  - say
policies:
  - name: "emit-events"
    dsl: |
      permit(principal is plugin, action in ["emit"], resource is stream) when {
        principal.plugin.name == "dice-tracker"
      };
  - name: "kv-storage"
    dsl: |
      permit(principal is plugin, action in ["read", "write"], resource is kv) when {
        principal.plugin.name == "dice-tracker"
      };
lua-plugin:
  entry: main.lua
```

This grants read and write but not delete. To grant all three, add `"delete"`
to the action list:

```yaml
      permit(principal is plugin, action in ["read", "write", "delete"], resource is kv) when {
```

## Combine policies for a full plugin

A full-featured plugin declares one policy per capability. A combat system that
emits events, reads world state, stores data, and registers commands declares
all four:

```yaml
name: combat-system
version: 1.0.0
type: binary
events:
  - say
  - arrive
  - leave
policies:
  - name: "emit-combat-events"
    dsl: |
      permit(principal is plugin, action in ["emit"], resource is stream) when {
        principal.plugin.name == "combat-system"
      };

  - name: "read-world-state"
    dsl: |
      permit(principal is plugin, action in ["read"], resource is world_object) when {
        principal.plugin.name == "combat-system" &&
        (resource like "character:*" || resource like "location:*" || resource like "object:*")
      };

  - name: "combat-storage"
    dsl: |
      permit(principal is plugin, action in ["read", "write", "delete"], resource is kv) when {
        principal.plugin.name == "combat-system"
      };

  - name: "register-commands"
    dsl: |
      permit(principal is plugin, action in ["execute"], resource is command) when {
        principal.plugin.name == "combat-system"
      };
binary-plugin:
  executable: combat-system
```

An operator reviewing this manifest sees exactly what the plugin needs — emit to
any stream, read characters/locations/objects, read/write/delete storage, and
register its own commands — and decides whether to trust it.

## Handle access denials

If your plugin tries something without a matching policy, the host function
returns `"access denied"`. Branch on it:

```lua
local location, err = holomush.query_location(location_id)
if err then
    if err:match("access denied") then
        -- Missing policy — check your plugin.yaml
        holomush.log("warn", "No policy for location query")
        return nil
    end
    -- Some other error
    holomush.log("error", "Query failed: " .. err)
    return nil
end
```

The denial is logged server-side too, so operators can see what happened.

## Debugging tips

1. **Check server logs.** Denied actions are logged with your plugin name,
   the action, and the resource.
2. **Match your plugin name exactly.** The `principal.plugin.name` in your
   policy must match the `name` in your manifest. Case matters.
3. **Check your resource patterns.** `"location:*"` won't match
   `"character:123"`. Make sure your `like` patterns cover what you're
   accessing.
4. **Start broad, narrow later.** While developing, you can use broad
   patterns like `resource like "*"`, then tighten them before release.

## Further reading

- [Access Control Reference](/reference/access-control/) — Full DSL spec, operator guide, capability tables
- [Plugin Guide](/extending/tutorials/plugin-guide/) — Complete plugin development guide
- [Getting Started](/extending/tutorials/getting-started/) — Your first plugin
