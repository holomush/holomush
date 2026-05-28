---
title: "Lua Plugin Author Guide"
---

Lua plugins run inside a sandboxed gopher-lua VM. They are ideal for simple
event reactions, formatting, and game commands that do not need persistent
storage beyond the KV store.

For the full plugin system overview, see [Plugin Guide](/extending/plugin-guide/).

## Project structure

```text
plugins/my-plugin/
  plugin.yaml    # Manifest
  main.lua       # Entry point
```

## Event handler

Lua plugins implement a single `on_event` function:

```lua
function on_event(event)
    if event.type ~= "say" then return nil end
    if event.actor_kind == "plugin" then return nil end

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

## Host functions

All Lua plugins have access to host functions via the `holomush` global table
and the `holo` standard library. Additional capability modules are injected
based on the `requires` list in the manifest.

### Always available

#### holomush.* (core)

| Function                        | Signature                                            | Description                      |
| ------------------------------- | ---------------------------------------------------- | -------------------------------- |
| `holomush.log`                  | `(level, message)`                                   | Log at debug/info/warn/error     |
| `holomush.new_request_id`       | `() -> string`                                       | Generate a ULID                  |
| `holomush.kv_get`               | `(key) -> value, err`                                | Read from KV store               |
| `holomush.kv_set`               | `(key, value) -> nil, err`                           | Write to KV store                |
| `holomush.kv_delete`            | `(key) -> nil, err`                                  | Delete from KV store             |
| `holomush.query_location`       | `(id) -> table, err`                                 | Get location by ID               |
| `holomush.query_character`      | `(id) -> table, err`                                 | Get character by ID              |
| `holomush.query_location_characters` | `(id, [opts]) -> table, err`                    | List characters at a location    |
| `holomush.query_object`         | `(id) -> table, err`                                 | Get object by ID                 |
| `holomush.create_location`      | `(name, desc, type) -> table, err`                   | Create a location                |
| `holomush.create_exit`          | `(from, to, name, [opts]) -> table, err`             | Create an exit between locations |
| `holomush.create_object`        | `(name, opts) -> table, err`                         | Create an object                 |
| `holomush.find_location`        | `(name) -> table, err`                               | Find location by name            |
| `holomush.set_property`         | `(entity_type, id, property, value) -> true, err`    | Set entity property              |
| `holomush.get_property`         | `(entity_type, id, property) -> value, err`          | Get entity property              |
| `holomush.list_commands`        | `(character_id) -> table, err`                       | List available commands           |
| `holomush.get_command_help`     | `(name, character_id) -> table, err`                 | Get command help details         |

#### holo.fmt.* (text formatting)

| Function             | Signature                  | Description                            |
| -------------------- | -------------------------- | -------------------------------------- |
| `holo.fmt.bold`      | `(text) -> string`         | Bold text                              |
| `holo.fmt.italic`    | `(text) -> string`         | Italic text                            |
| `holo.fmt.dim`       | `(text) -> string`         | Dim text                               |
| `holo.fmt.underline` | `(text) -> string`         | Underlined text                        |
| `holo.fmt.color`     | `(color, text) -> string`  | Colored text                           |
| `holo.fmt.list`      | `(items) -> string`        | Bulleted list from array table         |
| `holo.fmt.pairs`     | `(table) -> string`        | Key-value pair display                 |
| `holo.fmt.table`     | `({headers, rows}) -> str` | Formatted table                        |
| `holo.fmt.separator` | `() -> string`             | Visual separator line                  |
| `holo.fmt.header`    | `(text) -> string`         | Section header                         |
| `holo.fmt.parse`     | `(text) -> string`         | Parse inline markup                    |

#### holo.emit.* (event emission)

| Function              | Signature                              | Description                    |
| --------------------- | -------------------------------------- | ------------------------------ |
| `holo.emit.location`  | `(location_id, event_type, payload)`   | Queue event to a location      |
| `holo.emit.character` | `(character_id, event_type, payload)`  | Queue event to a character     |
| `holo.emit.global`    | `(event_type, payload)`                | Queue a global event           |
| `holo.emit.flush`     | `() -> table or nil`                   | Flush and return queued events |

#### holo.session.* (session queries)

Available when `session.Access` is configured on the host (always in production):

| Function                        | Signature                        | Description                     |
| ------------------------------- | -------------------------------- | ------------------------------- |
| `holo.session.find_by_name`     | `(name) -> table or nil, err`    | Find session by character name  |
| `holo.session.set_last_whispered` | `(session_id, name)`           | Record last whisper target      |

The returned session table contains `character_id`, `character_name`, and
`location_id`.

### Capability modules (requires-based)

These modules are injected only when the corresponding proto service is listed
in the manifest's `requires` field. Each module registers a global Lua table.

#### session.* (requires `holomush.session.v1.SessionService`)

| Function                       | Signature                       | Description                       |
| ------------------------------ | ------------------------------- | --------------------------------- |
| `session.find_by_name`         | `(name) -> table or nil, err`   | Find session by character name    |
| `session.list_active`          | `() -> table, err`              | List all active sessions          |
| `session.broadcast`            | `(message) -> nil, err`         | Broadcast system message          |
| `session.set_last_whispered`   | `(session_id, name)`            | Record last whisper target        |
| `session.disconnect`           | `(session_id, reason) -> nil, err` | Forcibly disconnect a session  |

Session table fields: `id`, `character_id`, `character_name`, `location_id`,
`grid_present`, `last_whispered`.

#### alias.* (requires `holomush.alias.v1.AliasService`)

| Function                | Signature                              | Description                    |
| ----------------------- | -------------------------------------- | ------------------------------ |
| `alias.set_player`      | `(player_id, name, command)`           | Create/update player alias     |
| `alias.delete_player`   | `(player_id, name)`                    | Remove player alias            |
| `alias.list_player`     | `(player_id) -> table`                 | List player's aliases          |
| `alias.check_shadow`    | `(name) -> table`                      | Check if alias shadows command |
| `alias.set_system`      | `(name, command, created_by)`          | Create/update system alias     |
| `alias.delete_system`   | `(name)`                               | Remove system alias            |
| `alias.list_system`     | `() -> table`                          | List system aliases            |

Alias entry tables contain `alias` and `command` fields. `check_shadow` returns
`{shadows = bool, command = string}`.

#### property.* (requires `holomush.property.v1.PropertyService`)

| Function                              | Signature                                      | Description                   |
| ------------------------------------- | ---------------------------------------------- | ----------------------------- |
| `property.list_by_parent`             | `(subject_id, parent_type, parent_id) -> table` | List properties by parent    |
| `property.find_by_prefix`             | `(prefix) -> table`                            | Find properties by name prefix|
| `property.update_character_description` | `(subject_id, character_id, description)`    | Set character description     |

Property tables contain `name`, `value`, `visibility`. Extended results from
`find_by_prefix` also include `parent_type` and `parent_id`.

#### world_ext.* (requires `holomush.world.v1.WorldService`)

Extended world queries beyond the always-available `holomush.query_*` functions:

| Function                                  | Signature                             | Description                      |
| ----------------------------------------- | ------------------------------------- | -------------------------------- |
| `world_ext.get_objects_by_location`        | `(subject_id, location_id) -> table`  | All objects at a location        |
| `world_ext.get_characters_by_location`     | `(subject_id, location_id) -> table`  | All characters at a location     |

Object tables: `id`, `name`, `description`, `location_id`, `owner_id`.
Character tables: `id`, `player_id`, `name`, `description`, `location_id`.

## Error handling

Host functions return errors as a second return value (`nil, err`). Check errors
before using results:

```lua
local location, err = holomush.query_location(location_id)
if err then
    holomush.log("warn", "query failed: " .. err)
    return nil
end
-- Use location.name, location.description, etc.
```

Common error messages: `"not found"`, `"access denied"`, `"query timed out"`,
`"internal error (ref: XXXX...)"`. See [Plugin Guide](/extending/plugin-guide/#error-handling)
for details on the correlation ID pattern.

## Manifest example with capabilities

```yaml
name: my-social-plugin
version: 1.0.0
type: lua

requires:
  - holomush.session.v1.SessionService
  - holomush.alias.v1.AliasService

events:
  - say
  - arrive

policies:
  - name: "emit-events"
    dsl: |
      permit(principal is plugin, action in ["emit"], resource is stream) when {
        principal.plugin.name == "my-social-plugin"
      };

lua-plugin:
  entry: main.lua
```

With these `requires`, your `main.lua` gets access to both `session.*` and
`alias.*` global tables in addition to the always-available `holomush.*` and
`holo.*` functions.
