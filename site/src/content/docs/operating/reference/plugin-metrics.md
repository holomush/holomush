---
title: "Plugin metrics"
---

Prometheus metrics that expose Lua plugin resource-limit state for
operators. For how to act on these metrics, see
[Tune Lua plugin resource limits](/operating/how-to/tune-plugin-resource-limits/);
for what the limits defend against, see
[Plugin security](/operating/explanation/plugin-security/).

## Lua resource-limit metrics

| Metric | Labels | Meaning |
| ------ | ------ | ------- |
| `holomush_plugin_lua_invocations_total` | `plugin`, `handler`, `outcome` | The denominator for outcome-rate dashboards. `outcome` takes values `success`, `timeout`, `registry_full`, `error`. |
| `holomush_plugin_lua_timeouts_total` | `plugin`, `handler` | CPU-cap violations, attributable by plugin and handler. |
| `holomush_plugin_lua_registry_full_total` | `plugin`, `handler` | Memory-cap (value-registry) violations. |
