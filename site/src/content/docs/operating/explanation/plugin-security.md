---
title: "Plugin security"
---

This page explains the defensive controls the plugin host enforces on
plugin code, and why each one exists.

## Lua plugin resource limits

The plugin host enforces three defensive controls on every Lua plugin
invocation to prevent CPU exhaustion, memory exhaustion, and dispatcher
starvation.

Two of these controls are governed by operator-tunable knobs.

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

The third control, the watchdog goroutine, has no operator knob: every
`CallByParam` runs in its own goroutine so a stuck host function cannot
hang the dispatcher. If the CPU deadline fires while a host function is
still running, the dispatcher waits for the goroutine to drain —
bounded by the hostfunc audit invariant that every registered host
function respects context.

## See also

- [Tune Lua plugin resource limits](/operating/how-to/tune-plugin-resource-limits/)
  — when and how to raise the two operator knobs.
- [Plugin metrics](/operating/reference/plugin-metrics/) — the Prometheus
  metrics that expose resource-limit state.
