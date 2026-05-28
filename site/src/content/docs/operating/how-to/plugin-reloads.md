---
title: "Plugin reloads — event history behaviour"
description: "How HoloMUSH handles plugin reloads at runtime and what operators should expect in the event log."
---

This page explains what happens to event history when you reload a plugin — specifically how rendering metadata and verb labels behave across a reload boundary. It's for operators who are diagnosing unexpected scrollback differences after a plugin update.

For the resource-limit controls that govern plugin execution, see [Plugin security](/operating/explanation/plugin-security/) and [Tune Lua plugin resource limits](/operating/how-to/tune-plugin-resource-limits/).

## Historical fidelity and version drift

Each event is stamped with the rendering metadata in effect at emit time. After a plugin reload with a changed verb definition, events already in `events_audit` keep their original rendering — they were emitted before the reload. Only new events carry the updated metadata.

### `source_plugin_version` and drift visibility

The `source_plugin_version` field in every event's rendering records the plugin version that declared the verb at emit time. A version change between two events of the same type signals that a reload occurred between them.

To inspect version distribution in `events_audit`:

```sql
SELECT
    rendering->>'source_plugin'         AS plugin,
    rendering->>'source_plugin_version' AS version,
    COUNT(*)                             AS event_count
FROM events_audit
WHERE rendering->>'source_plugin' = 'myplugin'
GROUP BY plugin, version
ORDER BY version;
```

### Label discontinuity

If a reload changes a verb's `label` (for example, "says" → "exclaims"), the scrollback shows different label text for events before and after the reload. This is intentional — scrollback is an accurate record of what was emitted. Version verb label changes carefully in long-running games to avoid confusing players.

## See also

- [Plugin security](/operating/explanation/plugin-security/) — the resource controls the host enforces on plugin invocations
- [Tune Lua plugin resource limits](/operating/how-to/tune-plugin-resource-limits/) — adjusting timeout and registry caps
- [Plugin metrics](/operating/reference/plugin-metrics/) — Prometheus metrics for plugin resource-limit state
- [Operations](/operating/how-to/operations/) — general operator monitoring and maintenance
