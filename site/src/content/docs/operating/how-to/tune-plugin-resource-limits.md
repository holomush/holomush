---
title: "Tune Lua plugin resource limits"
---

This guide covers when and how to adjust the two operator-tunable knobs
that bound Lua plugin resource use. For why these controls exist, see
[Plugin security](/operating/explanation/plugin-security/).

## Raise the CPU deadline

Raise `--plugin-lua-timeout` (default `1s`) if a legitimate plugin does
synchronous work against the world service that can exceed one second
end-to-end.

Monitor `holomush_plugin_lua_timeouts_total{plugin,handler}` — any
sustained non-zero rate means either the plugin has pathological loops
or the timeout is too tight.

## Raise the value-registry cap

Raise `--plugin-lua-registry-max` (default `65536`) if a legitimate
plugin holds many active Lua values simultaneously (for instance,
caching or bulk operations).

Monitor `holomush_plugin_lua_registry_full_total{plugin,handler}` — a
non-zero rate points at either a memory-bomb plugin or a cap set too low
for a legitimate workload.

## See also

- [Plugin metrics](/operating/reference/plugin-metrics/) — full reference
  for the metrics named above.
- [Plugin security](/operating/explanation/plugin-security/) — what the
  knobs protect against.
