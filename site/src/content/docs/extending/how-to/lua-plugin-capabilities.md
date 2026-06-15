---
title: "Lua Plugin Capabilities: Migration and Brokered Surface"
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

As of HoloMUSH `holomush-eykuh.4` (ADR holomush-05f3v), the ten legacy Lua
host functions that were injected unconditionally into every plugin's `holomush`
module are retired. They are replaced by **host-brokered capability globals**:
proto-backed gRPC stubs injected as Lua globals only when a plugin declares the
corresponding capability in `plugin.yaml` and the host resolver grants it.

This page covers the migration mapping, the access pattern, response-shape
changes, the nil-guard idiom, and the stability contract for the new surface.

## Legacy → brokered surface mapping

The table below maps every retired legacy function to its brokered replacement.

| Legacy call | Brokered replacement | Notes |
| --- | --- | --- |
| `holomush.query_location(id)` | `_G["world.query"].QueryLocation({location_id = id})` | Returns `(resp, err)`; `resp` is the full location proto table |
| `holomush.query_character(id)` | `_G["world.query"].QueryCharacter({character_id = id})` | |
| `holomush.query_location_characters(id)` | `_G["world.query"].QueryLocationCharacters({location_id = id})` | Returns `{characters = [...]}` (array nested under `characters`) |
| `holomush.find_location(name)` | `_G["world.query"].FindLocation({name = name})` | |
| `holomush.query_object(id)` | `_G["world.query"].QueryObject({object_id = id})` | |
| `holomush.create_location(name, ...)` | `_G["world.mutation"].CreateLocation({name = name, description = "", type = "persistent"})` | Returns location proto with `.id` |
| `holomush.create_exit(from, to, name)` | `_G["world.mutation"].CreateExit({from_id = from, to_id = to, name = name})` | Pass `bidirectional = true, return_name = ...` for two-way exits |
| `holomush.create_object(...)` | `_G["world.mutation"].CreateObject({...})` | |
| `holomush.set_property(eid, k, v)` | `_G["property"].SetProperty({entity_id = eid, key = k, value = v})` | |
| `holomush.get_property(eid, k)` | `_G["property"].GetProperty({entity_id = eid, key = k})` | |
| `holomush.kv_get(k)` | `_G["kv"].Get({key = k})` | |
| `holomush.kv_set(k, v)` | `_G["kv"].Set({key = k, value = v})` | |
| `holomush.kv_delete(k)` | `_G["kv"].Delete({key = k})` | |
| `holomush.session.find_by_name(n)` | `_G["session"].FindByName({name = n})` | Response shape changed — see [Response-shape changes](#response-shape-changes) |
| `holomush.session.list_active()` | `_G["session"].ListActive({})` | Response shape changed — see [Response-shape changes](#response-shape-changes) |
| `holomush.session.set_last_whispered(sid, n)` | `_G["session"].SetLastWhispered({session_id = sid, name = n})` | |
| `holomush.session.broadcast(msg)` | `_G["session.admin"].Broadcast({message = msg})` | Requires the `session.admin` capability, not `session` |
| `holomush.session.disconnect(sid)` | `_G["session.admin"].Disconnect({session_id = sid})` | |
| `holomush.evaluate(action, resource)` | `_G["eval"].Evaluate({action = action, resource = resource})` | Returns `(resp, err)` — see [Evaluate response shape](#evaluate-response-shape) |
| `holomush.get_setting(k)` | `_G["settings"].GetSetting({key = k})` | |
| `holomush.set_setting(k, v)` | `_G["settings"].SetSetting({key = k, value = v})` | |

## The `_G["dotted.name"]` access pattern

Brokered capability globals are registered under their **full capability token
name** — the same string that appears in `plugin.yaml` `requires`. A dotted
name like `"world.query"` or `"session.admin"` is the **entire key**: there is
no `world` table with a `query` field.

Always retrieve these at the **top level of `main.lua`** using `_G[...]`:

```lua
-- Correct: retrieve at top level so each event/command delivery uses the
-- same pre-wired table.
local world_query  = _G["world.query"]
local world_mut    = _G["world.mutation"]
local session_caps = _G["session"]
local session_admin = _G["session.admin"]
local kv_caps      = _G["kv"]
local prop_caps    = _G["property"]
local eval_caps    = _G["eval"]
local settings_caps = _G["settings"]
local focus_caps   = _G["focus"]
```

Single-word capabilities (`session`, `eval`, `kv`, `property`, `settings`,
`focus`, `emit`) are also retrieved the same way — `_G["session"]` is not the
same as `session` (which would be a nil global if nothing sets it).

Each method on the table takes a **single proto-request table** (snake\_case
field names matching the proto definition) and returns two values:
`(response_table, err)`. On success `err` is `nil`; on failure `response_table`
is `nil` and `err` is a non-empty string.

```lua
local resp, err = world_query.QueryLocation({location_id = "01J9..."})
if err then
    holomush.log("error", "query failed: " .. err)
    return failure_response("Unable to look up location.")
end
local loc_name = resp.name
```

## Response-shape changes

Several responses now nest their entity under a named field rather than
returning the entity directly.

### `FindByName` — entity nested under `.session`

The legacy `session.find_by_name(name)` returned the session entity directly
(or `nil`). The brokered `session.FindByName({name = n})` always returns a
response table; the entity is nested under `.session` and is absent (nil) when
no active session matches the name:

```lua
local resp, err = session_caps.FindByName({name = target_name})
if err then
    return failure_response("Unable to reach target right now.")
end
-- resp.session is nil when the character is not connected.
local sess = resp and resp.session
if not sess then
    return error_response('No one named "' .. target_name .. '" is connected.')
end
-- Use sess.character_id, sess.character_name, sess.location_id, etc.
```

### `ListActive` — sessions nested under `.sessions`

The brokered `session.ListActive({})` returns `{sessions = [...]}`. The
sessions array is nested under the `sessions` key:

```lua
local list_resp, list_err = session_caps.ListActive({})
if list_err then
    holomush.log("warn", "list sessions failed: " .. list_err)
else
    local sessions = list_resp and list_resp.sessions
    local count = sessions and #sessions or 0
end
```

### `QueryLocationCharacters` — characters nested under `.characters`

```lua
local resp, err = world_query.QueryLocationCharacters({location_id = loc_id})
if not err then
    local chars = resp and resp.characters or {}
end
```

### Evaluate response shape

`eval.Evaluate` returns a response table, not a `(bool, string)` pair:

```lua
local resp, err = eval_caps.Evaluate({action = "write", resource = "scene:" .. id})
if err then
    return failure_response("Permission check failed.")
end
if not resp or not resp.allowed then
    return error_response(resp and resp.reason or "you are not permitted to do that")
end
-- authorized — proceed
```

## Capability injection and the nil-guard idiom

A capability global is injected **only** when both conditions hold:

1. The plugin's `plugin.yaml` declares the capability in `requires`:

   ```yaml
   requires:
     - capability: session
     - capability: session.admin
   ```

2. The host resolver grants the capability to this plugin (based on declared
   `requires`, security policy, and runtime availability).

When a capability is not declared or not granted, its global is **absent** (the
`_G[...]` lookup returns `nil`). Plugins MUST nil-guard before using any
capability:

```lua
local session_caps = _G["session"]

local function handle_page(ctx)
    if not session_caps then
        return error_response("This command requires session access which is not available.")
    end
    -- safe to call session_caps.FindByName(...)
end
```

Nil-guarding at the top-level variable assignment is not sufficient on its
own — if the global is nil, field access `session_caps.FindByName` will raise
a Lua error. Guard at usage.

## Stability note

The host-brokered capability surface (the ten capability domains: `kv`,
`world.query`, `world.mutation`, `property`, `session`, `session.admin`,
`focus`, `eval`, `settings`, `emit`) is the **sole supported capability
contract for Lua plugins going forward**. The legacy `holomush.*` functions are
removed and will not return.

This surface is backed by stable proto service definitions in
`holomush.plugin.host.v1`. Proto field and response-shape changes are
**breaking**: a field renamed or removed in the proto will break any Lua plugin
that references it by the old name. When the host ships a breaking proto
change, the change log will document the affected fields and the migration
path.

Pin your plugin against a known host version if you require stability
guarantees across host upgrades.
