---
title: "Plugin Runtime Configuration"
---

Plugins can declare typed configuration keys in their manifest. At startup the
host merges server-operator overrides on top of your defaults and delivers the
resolved map to your plugin — before your code touches any game state.

Both binary and Lua plugins receive the **identical merged map** through their
respective runtime delivery paths. This is the plugin-runtime-symmetry
guarantee: there are no trust differences between runtimes at the configuration
layer.

## Declaring config in the manifest

Add a `config:` block to your `plugin.yaml`. Each key maps to a descriptor:

```yaml
config:
  my_key:
    type: string
    default: "hello"
    description: "A greeting prefix."
  retry_count:
    type: int
    default: "3"
    required: true
    description: "How many times to retry on transient failure."
```

### Field reference

| Field         | Required | Description                                                                                              |
| ------------- | -------- | -------------------------------------------------------------------------------------------------------- |
| `type`        | Yes      | One of `string`, `int`, `bool`, `duration`                                                               |
| `default`     | No       | String representation of the default value, parsed according to `type` (e.g., `"30s"` for a duration)  |
| `required`    | No       | If `true`, the key must be supplied by either a default or a server override. Defaults to `false`.       |
| `description` | No       | Human-readable explanation shown in plugin info output.                                                  |

### Types

| Type       | Format                                    | Example               |
| ---------- | ----------------------------------------- | --------------------- |
| `string`   | Any UTF-8 text                            | `"my-value"`          |
| `int`      | Decimal integer                           | `"42"`                |
| `bool`     | `"true"` or `"false"`                     | `"true"`              |
| `duration` | Go duration string (`h`, `m`, `s`, `ms`) | `"30s"`, `"168h"`     |

The host treats configuration as opaque: it validates that `default` values are
parseable to their declared type, but it does not interpret what any key means.
Semantics are entirely up to your plugin.

## Validation at load time

The host validates your schema when the plugin loads. An unknown type or a
`default` value that cannot be parsed to its declared type produces error code
`PLUGIN_CONFIG_SCHEMA_INVALID` and the plugin fails to start.

## Merge and precedence

The effective configuration is computed once, before your plugin's `Init` is
called:

```text
manifest defaults < server operator overrides
```

Operator overrides win per key. A key absent from both sources is omitted
entirely — it is not delivered as an empty value.

Three error codes cover merge failures:

| Error code                      | Cause                                                          |
| ------------------------------- | -------------------------------------------------------------- |
| `PLUGIN_CONFIG_UNKNOWN_KEY`     | Operator supplied a key that is not in your manifest schema.   |
| `PLUGIN_CONFIG_TYPE_INVALID`    | The effective value (after merge) cannot be parsed to its type.|
| `PLUGIN_CONFIG_MISSING_REQUIRED`| A `required: true` key has no default and no operator override.|

Any of these stops the plugin from loading. Operators see a descriptive error
in the server log.

## Binary plugins (Go)

The host passes the merged config map inside `ServiceConfig.plugin_config` at
`Init`. Use `pluginsdk.DecodeConfig[T]` (package
`github.com/holomush/holomush/pkg/plugin`) to decode it into a typed struct:

```go
type myConfig struct {
    VoteWindow time.Duration `mapstructure:"vote_window"`
    MaxTries   int           `mapstructure:"max_tries"`
}
```

```go
func (p *myPlugin) Init(ctx context.Context, config *pluginv1.ServiceConfig) error {
    cfg, err := pluginsdk.DecodeConfig[myConfig](config)
    if err != nil {
        return fmt.Errorf("decode config: %w", err)
    }
    p.voteWindow = cfg.VoteWindow
    p.maxTries = cfg.MaxTries
    return nil
}
```

`DecodeConfig` uses `mapstructure` struct tags. Durations decode via a duration
hook; integers and booleans decode via weak typing. Defaults and required-key
enforcement happen host-side during the merge, so by the time `DecodeConfig` is
called the map already contains only valid, parseable values.

## Lua plugins

The host injects a `holomush.config` table with typed accessor functions. Each
function returns the value for the given key, or `nil` if the key is absent
from the merged map.

| Function                             | Returns                            |
| ------------------------------------ | ---------------------------------- |
| `holomush.config.string(key)`        | string or nil                      |
| `holomush.config.int(key)`           | number or nil                      |
| `holomush.config.bool(key)`          | boolean or nil                     |
| `holomush.config.duration(key)`      | number of seconds (float) or nil   |
| `holomush.config.require_int(key)`   | number or raises a Lua error       |
| `holomush.config.require_duration(key)` | number of seconds or raises   |
| `holomush.config.require_bool(key)`  | boolean or raises a Lua error      |
| `holomush.config.require_string(key)` | string or raises a Lua error      |

A present-but-unparseable value raises immediately (fail-loud). A `require_*`
accessor raises if the key is absent. Every type has both a nil-returning
variant and a `require_*` variant; use the plain accessor with an `or` fallback
for optional keys, and the `require_*` variant when the key must be present.

```lua
-- Read optional config with a fallback
local prefix = holomush.config.string("greeting_prefix") or "Hello"

-- Read a duration (returned as seconds)
local window_secs = holomush.config.require_duration("vote_window")
holomush.log("info", "Vote window: " .. tostring(window_secs) .. "s")
```

## Worked example: core-scenes

`plugins/core-scenes/plugin.yaml` ships three duration keys that control the
publish-vote scheduler:

```yaml
config:
  vote_window:
    type: duration
    default: 168h
    description: "How long a publish-vote attempt collects votes before timeout."
  cooloff_window:
    type: duration
    default: 30m
    description: "Delay between unanimous-yes resolution and PUBLISHED."
  scheduler_interval:
    type: duration
    default: 30s
    description: "How often the publish scheduler sweeps for expired attempts."
```

On the Go side, `Init` decodes these into a struct:

```go
type scenesConfig struct {
    VoteWindow        time.Duration `mapstructure:"vote_window"`
    CooloffWindow     time.Duration `mapstructure:"cooloff_window"`
    SchedulerInterval time.Duration `mapstructure:"scheduler_interval"`
}

cfg, err := pluginsdk.DecodeConfig[scenesConfig](config)
```

A server-supplied override wins over the manifest default, per key. Suppose the
scheduler is overridden to `5m` (the manifest default is `30s`): the host
validates `"5m"` parses as a duration and merges it over the default, so the
effective value is `5m`. core-scenes is a binary plugin, so it receives that
value in the decoded struct as `SchedulerInterval == 5 * time.Minute`. A Lua
plugin reading the same key would get `holomush.config.duration("scheduler_interval") == 300`
(seconds) — same merged value, runtime-appropriate shape.

Overrides reach a plugin through the subsystem's per-plugin override map
(`PluginConfigOverrides`). Today that map is populated programmatically — for
example, by the integration-test harness to tune vote and cool-off windows
deterministically. An operator-facing server-config surface for overrides is not
yet wired; until then, the manifest defaults are what production plugins see.

## See also

- [Plugin Guide](plugin-guide.md) — overview of the plugin system and manifest fields
- [Binary Plugin Author Guide](binary-plugins.md) — `Init`, `ServiceConfig`, and the full SDK
- [Lua Plugin Author Guide](lua-plugins.md) — host functions and the Lua event model
