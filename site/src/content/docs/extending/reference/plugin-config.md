---
title: "Plugin configuration reference"
---

Reference for the `config:` block in `plugin.yaml`: descriptor fields, value
types, and the error codes that configuration problems produce. For how to
declare, merge, and read configuration in your plugin, see
[Plugin Runtime Configuration](/extending/how-to/plugin-config/).

## Config descriptor fields

Each key under `config:` maps to a descriptor with these fields:

| Field         | Required | Description                                                                                            |
| ------------- | -------- | ------------------------------------------------------------------------------------------------------ |
| `type`        | Yes      | One of `string`, `int`, `bool`, `duration`                                                             |
| `default`     | No       | String representation of the default value, parsed according to `type` (e.g., `"30s"` for a duration) |
| `required`    | No       | If `true`, the key must be supplied by either a default or a server override. Defaults to `false`.     |
| `description` | No       | Human-readable explanation shown in plugin info output.                                                |

## Config value types

| Type       | Format                                   | Example           |
| ---------- | ---------------------------------------- | ----------------- |
| `string`   | Any UTF-8 text                           | `"my-value"`      |
| `int`      | Decimal integer                          | `"42"`            |
| `bool`     | `"true"` or `"false"`                    | `"true"`          |
| `duration` | Go duration string (`h`, `m`, `s`, `ms`) | `"30s"`, `"168h"` |

The host treats configuration as opaque: it validates that `default` values are
parseable to their declared type, but it does not interpret what any key means.
Semantics are entirely up to your plugin.

## Config error codes

A configuration problem stops the plugin from loading and is logged
server-side with a descriptive message.

| Error code                       | Cause                                                                                |
| -------------------------------- | ------------------------------------------------------------------------------------ |
| `PLUGIN_CONFIG_SCHEMA_INVALID`   | An unknown `type`, or a `default` value that cannot be parsed to its declared type.  |
| `PLUGIN_CONFIG_UNKNOWN_KEY`      | Operator supplied a key that is not in the manifest schema.                          |
| `PLUGIN_CONFIG_TYPE_INVALID`     | The effective value (after merge) cannot be parsed to its type.                      |
| `PLUGIN_CONFIG_MISSING_REQUIRED` | A `required: true` key has no default and no operator override.                      |

## Lua config accessors

The host injects a `holomush.config` table with typed accessor functions. Each
plain accessor returns the value or `nil` if the key is absent; each `require_*`
variant raises a Lua error if the key is absent.

| Function                                | Returns                          |
| --------------------------------------- | -------------------------------- |
| `holomush.config.string(key)`           | string or nil                    |
| `holomush.config.int(key)`              | number or nil                    |
| `holomush.config.bool(key)`             | boolean or nil                   |
| `holomush.config.duration(key)`         | number of seconds (float) or nil |
| `holomush.config.require_int(key)`      | number or raises a Lua error     |
| `holomush.config.require_duration(key)` | number of seconds or raises      |
| `holomush.config.require_bool(key)`     | boolean or raises a Lua error    |
| `holomush.config.require_string(key)`   | string or raises a Lua error     |

A present-but-unparseable value raises immediately (fail-loud).
