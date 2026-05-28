# Verb Registration

HoloMUSH uses a **verb registry** to associate event types with rendering
metadata. As a plugin author, you declare verbs in your plugin manifest
— the host wires everything else automatically.

## Declaring verbs in your manifest

In `manifest.yaml`, add a `verbs:` block:

```yaml
verbs:
  - type: "myplugin:greet"
    category: "communication"
    format: "speech"
    label: "greets"
    display_target: "terminal"
```

### Field reference

| Field | Required | Values | Description |
|---|---|---|---|
| `type` | Yes | `"plugin-name:verb"` | Unique identifier. Convention: `plugin-name:verb`. |
| `category` | Yes | `communication`, `movement`, `state`, `command`, `system` | Semantic category. |
| `format` | Yes | `speech`, `action`, `narrative`, `notification`, `error`, `snapshot`, `delta` | Wire shape. |
| `label` | When `format == speech` | String | Verb phrase: "says", "whispers". |
| `display_target` | Yes | `terminal`, `state`, `both` | Which UI surface receives the event. |

### `display_target` values

| Value | Meaning |
|---|---|
| `terminal` | Scrollback / telnet only |
| `state` | State sidebar only |
| `both` | Both surfaces |

## How verb metadata flows

```text
manifest.yaml verbs:
    ↓ plugin loader reads at LoadAll
VerbRegistry.RegisterWithSource(reg, manifest.Version)
    ↓ RenderingPublisher wraps every Publisher call
event.Rendering stamped at emit time
    ↓ JetStream + gRPC wire
Gateway reads EventFrame.Rendering — no domain knowledge needed
```

Plugin authors do not need to do anything at emit time. Emitting an event
with the registered type is sufficient — `RenderingPublisher` stamps the
rendering metadata automatically.

## Unregistered types

Emitting an event whose type is not in the registry returns
`EMIT_UNKNOWN_VERB`. Always declare every event type your plugin emits.

## Version tracking

The plugin version from `manifest.yaml` is recorded as
`source_plugin_version` in the rendering metadata. After a plugin reload,
new events carry the new version. Old events in `events_audit` keep their
original version, enabling drift detection.

## See also

- Verb registration spec: `docs/superpowers/specs/2026-04-26-gateway-verb-registry-sourcing.md`
- Event sensitivity (`crypto.emits`): `site/docs/extending/event-sensitivity.md`
- Event emit pipeline: `site/docs/contributing/event-emit-pipeline.md`
