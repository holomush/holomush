---
title: "Plugin Access Control"
---

Every host function your plugin calls is checked against the policies in your
`plugin.yaml`. No policy, no access. This page walks through writing policies
from the simplest case to more complex scenarios.

For the full DSL reference, operators guide, and capability tables, see the
[Access Control Reference](../reference/access-control.md).

## The Simplest Plugin: No Policies Needed

A plugin that only reacts to events and returns new events doesn't need any
policies at all. The event delivery system handles subscriptions based on
the `events` list in your manifest:

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

This works because returning events from `on_event` goes through the event
delivery system, not a host function. No policy needed.

**But wait** — this plugin is emitting events by returning them. What if you
want to emit events outside of a direct response? Then you need a policy.

## Adding Event Emission

If your plugin needs to emit events proactively (not just as a return value
from `on_event`), add an emit policy:

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

The policy says: "The plugin named `announcer` is allowed to emit events to
any stream." The `principal.plugin.name` condition scopes it to just this
plugin — other plugins can't piggyback on your policy.

## Reading the World

A plugin that needs to know about the game world — checking who's in a
location, looking up character names, examining objects — needs read policies.

Here's a greeter that welcomes players by name when they arrive:

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

Notice the `resource like "character:*"` pattern — this plugin can read
character data but not locations or objects. You grant exactly what you need.

If the greeter also needed to check what location someone arrived in:

```yaml
  - name: "read-world"
    dsl: |
      permit(principal is plugin, action in ["read"], resource is world_object) when {
        principal.plugin.name == "greeter" &&
        (resource like "character:*" || resource like "location:*")
      };
```

## Using Key-Value Storage

Plugins that need to persist data between events (scores, settings, cooldowns)
use the key-value store. Access requires its own policy:

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

This grants read and write but not delete. If you need all three:

```yaml
      permit(principal is plugin, action in ["read", "write", "delete"], resource is kv) when {
```

## A Complex Plugin: Combat System

A full-featured plugin might need multiple policies. Here's what a combat
system might declare:

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

This plugin can:

- Emit events to any stream (combat results, damage, effects)
- Read characters, locations, and objects (check stats, range, inventory)
- Store and retrieve data (HP, buffs, cooldowns)
- Register its own commands (attack, defend, flee)

An operator reviewing this manifest can see exactly what the combat system
needs — and decide whether to trust it.

## When Access Is Denied

If your plugin tries something without a matching policy, the host function
returns `"access denied"`:

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

## Debugging Tips

1. **Check server logs.** Denied actions are logged with your plugin name,
   the action, and the resource.
2. **Match your plugin name exactly.** The `principal.plugin.name` in your
   policy must match the `name` in your manifest. Case matters.
3. **Check your resource patterns.** `"location:*"` won't match
   `"character:123"`. Make sure your `like` patterns cover what you're
   accessing.
4. **Start broad, narrow later.** While developing, you can use broad
   patterns like `resource like "*"`, then tighten them before release.

## Further Reading

- [Access Control Reference](../reference/access-control.md) — Full DSL spec, operator guide, capability tables
- [Plugin Guide](plugin-guide.md) — Complete plugin development guide
- [Getting Started](getting-started.md) — Your first plugin
