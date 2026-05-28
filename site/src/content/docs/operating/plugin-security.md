# Plugin security

This page documents the defensive controls the plugin host enforces on
plugin code.

## Lua plugin resource limits

The plugin host enforces three defensive controls on every Lua plugin
invocation to prevent CPU exhaustion, memory exhaustion, and dispatcher
starvation.

Two operator-tunable knobs govern these controls:

The flag `--plugin-lua-timeout` (default `1s`) sets the per-invocation
CPU deadline. Every dispatcher entry point (event, command,
session-subscribe, command fallback) derives a `context.WithTimeout` from
the caller's context and passes it to `L.SetContext`; gopher-lua checks
this context at every VM instruction. A tight `while true do end` loop
is caught within the deadline plus a small instruction-boundary delay.

The flag `--plugin-lua-registry-max` (default `65536`) bounds the Lua
value registry per state. A plugin that allocates more values than the
cap hits a panic which `CallByParam(Protect: true)` converts to an
error, so the dispatcher returns a controlled failure rather than an
unbounded heap growth.

A third control, the watchdog goroutine, has no operator knob: every
`CallByParam` runs in its own goroutine so a stuck host function cannot
hang the dispatcher. If the CPU deadline fires while a host function is
still running, the dispatcher waits for the goroutine to drain —
bounded by the hostfunc audit invariant that every registered host
function respects context.

### Tuning

Raise `--plugin-lua-timeout` if a legitimate plugin does synchronous
work against the world service that can exceed one second end-to-end.
Monitor `holomush_plugin_lua_timeouts_total{plugin,handler}` — any
sustained non-zero rate means either the plugin has pathological loops
or the timeout is too tight.

Raise `--plugin-lua-registry-max` if a legitimate plugin holds many
active Lua values simultaneously (for instance, caching or bulk
operations). Monitor
`holomush_plugin_lua_registry_full_total{plugin,handler}` — a non-zero
rate points at either a memory-bomb plugin or a cap set too low for a
legitimate workload.

### Metrics

Three Prometheus metrics expose resource-limit state for operators:

- `holomush_plugin_lua_invocations_total{plugin,handler,outcome}` — the
  denominator for outcome-rate dashboards. `outcome` takes values
  `success`, `timeout`, `registry_full`, `error`.
- `holomush_plugin_lua_timeouts_total{plugin,handler}` — CPU-cap
  violations, attributable by plugin and handler.
- `holomush_plugin_lua_registry_full_total{plugin,handler}` — memory-cap
  violations.
