---
title: "Plugin Reloads"
---

This page documents how HoloMUSH handles plugin reloads at runtime.

## Historical fidelity and version drift

Each event is stamped with the rendering metadata in effect at emit time.
After a plugin reload with a changed verb definition, events already in
`events_audit` keep their original rendering — they were emitted before the
reload. Only new events carry the updated metadata.

### `source_plugin_version` and drift visibility

The `source_plugin_version` field in every event's rendering records the
plugin version that declared the verb at emit time. A version change between
two events of the same type signals that a reload occurred between them.

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

If a reload changes a verb's `label` (e.g., "says" → "exclaims"), the
scrollback shows different label text for events before and after the reload.
This is intentional — scrollback is an accurate record of what was emitted.
To avoid confusion in long-running games, version verb label changes carefully.
