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

For the descriptor fields, value types, error codes, and Lua accessor list, see
the [Plugin configuration reference](/extending/reference/plugin-config/).

## Declare config in the manifest

Add a `config:` block to your `plugin.yaml`. Each key maps to a descriptor (see
[Config descriptor fields](/extending/reference/plugin-config/#config-descriptor-fields)
and [Config value types](/extending/reference/plugin-config/#config-value-types)):

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

The host treats configuration as opaque: it validates that `default` values are
parseable to their declared type, but it does not interpret what any key means.
Semantics are entirely up to your plugin.

## Validation at load time

The host validates your schema when the plugin loads. An unknown type or a
`default` value that cannot be parsed to its declared type fails the load with
`PLUGIN_CONFIG_SCHEMA_INVALID` and the plugin does not start (see
[Config error codes](/extending/reference/plugin-config/#config-error-codes)).

## Merge and precedence

The effective configuration is computed once, before your plugin's `Init` is
called:

```text
manifest defaults < server operator overrides
```

Operator overrides win per key. A key absent from both sources is omitted
entirely — it is not delivered as an empty value. Merge failures (unknown key,
unparseable effective value, missing required key) each stop the plugin from
loading; see [Config error codes](/extending/reference/plugin-config/#config-error-codes).

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

The host injects a `holomush.config` table with typed accessor functions (see
[Lua config accessors](/extending/reference/plugin-config/#lua-config-accessors)
for the full list). Use the plain accessor with an `or` fallback for optional
keys, and the `require_*` variant when the key must be present:

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

- [Plugin configuration reference](/extending/reference/plugin-config/) — descriptor fields, types, error codes, Lua accessors
- [Plugin Guide](/extending/tutorials/plugin-guide/) — overview of the plugin system and manifest fields
- [Binary Plugin Author Guide](/extending/tutorials/binary-plugins/) — `Init`, `ServiceConfig`, and the full SDK
- [Lua Plugin Author Guide](/extending/tutorials/lua-plugins/) — host functions and the Lua event model
