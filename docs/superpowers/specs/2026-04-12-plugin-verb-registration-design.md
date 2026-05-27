<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Plugin Verb Registration Design

**Issue:** holomush-6l7l
**Date:** 2026-04-12
**Status:** Draft
**Author:** Sean Brandt, Claude Opus 4.6

## Problem

`VerbRegistry` in `internal/core/registry.go` is a runtime registry with
`Register()`/`Lookup()` methods. `builtins.go` seeds it with core entries at
startup. Plugins have no way to register their own verb entries.

When a plugin emits an event with an unknown type (e.g., `channel_say`), the
registry falls back to default rendering (system/narrative/TERMINAL). Plugin
events like channel messages render as generic text instead of
domain-appropriate formats.

The only way to add verb entries for plugin event types is to edit
`builtins.go` — a boundary violation.

## Approach

Manifest-declared verbs following the established pattern for commands,
policies, resource_types, and actions. Plugins declare a `verbs:` section in
their `manifest.yaml`. The loader calls `VerbRegistry.Register()` for each
entry at load time.

### Why manifest-declared

- Consistent with every other manifest-declared resource in the system
- Inspectable without loading the plugin — tooling, `plugin info`, docs
- Validation happens at load time — bad entries caught early
- VerbRegistry already has `Register()` with duplicate detection and format
  validation

### Alternatives considered

**Runtime host function** — Plugin calls `RegisterVerb` during initialization.
Rejected: breaks manifest-as-contract pattern, requires new host function for
Lua AND new gRPC service for binary, creates ordering questions.

**Hybrid (manifest + runtime)** — Rejected: YAGNI. No known use case for
runtime registration today. Can be added later without disturbing
manifest-declared verbs.

## Design

### Manifest Schema

Add optional `verbs` section to `manifest.yaml`:

```yaml
verbs:
  - type: channel_say
    category: communication
    format: speech
    label: "says"
    display_target: terminal
  - type: channel_pose
    category: communication
    format: action
    display_target: terminal
  - type: channel_join
    category: state
    format: notification
    display_target: both
  - type: channel_leave
    category: state
    format: notification
    display_target: both
```

#### Fields

| Field | Required | Description |
| --- | --- | --- |
| `type` | MUST | Event type string (e.g., `channel_say`) |
| `category` | MUST | One of: `communication`, `movement`, `state`, `system`, `command` |
| `format` | MUST | One of: `speech`, `action`, `narrative`, `notification`, `error`, `snapshot`, `delta` |
| `label` | Conditional | MUST be present when `format` is `speech` (e.g., "says", "whispers") |
| `display_target` | MUST | One of: `terminal`, `state`, `both` |

#### display_target

`display_target` is the event's routing intent — it controls which client
display surface receives the event. It decouples routing from category
semantics, allowing flexibility (e.g., a `state` event that also appears in
the terminal, or a `communication` event that updates a sidebar panel).

| Value | Meaning |
| --- | --- |
| `terminal` | Main scrolling text output only |
| `state` | Sidebar/status panel only |
| `both` | Both terminal and sidebar |

The server uses `display_target` for telnet filtering (skip state-only events)
and stamps it onto the `GameEvent` proto for web clients. The web client's
`eventRouter.ts` uses it as the primary routing signal.

#### Metadata keys

Metadata keys (e.g., `no_space: bool` for `pose`) are omitted from the
manifest schema. They are currently purely descriptive — nothing validates them
at runtime. Adding them later is a backwards-compatible change.

### VerbRegistration Changes

Add `Source` field to track ownership:

```go
type VerbRegistration struct {
    Type          string
    Category      string
    Format        string
    Label         string
    DisplayTarget webv1.EventChannel
    MetadataKeys  []MetadataKey
    Source        string // "builtin" or plugin name
}
```

Builtins set `Source: "builtin"`. Plugin-registered verbs set `Source` to the
plugin's manifest name.

### VerbRegistry Changes

Add removal methods to support unload:

```go
// Unregister removes a single verb by event type. Returns true if found.
func (r *VerbRegistry) Unregister(eventType string) bool

// UnregisterBySource removes all verbs registered by a given source.
// Returns the count of removed entries.
func (r *VerbRegistry) UnregisterBySource(source string) int
```

These methods are needed for:

- Load-failure unwind (clean up partial verb registration)
- Future plugin unload orchestration

Full plugin unload/reload orchestration is out of scope for this issue. The
registry API is designed to support it when needed.

### Manifest Parsing

Add `VerbSpec` struct to manifest types:

```go
type VerbSpec struct {
    Type          string `yaml:"type"`
    Category      string `yaml:"category"`
    Format        string `yaml:"format"`
    Label         string `yaml:"label,omitempty"`
    DisplayTarget string `yaml:"display_target"`
}
```

`display_target` is parsed as a string in YAML and converted to
`webv1.EventChannel` via an explicit case-insensitive mapping during
validation:

```go
var displayTargetValues = map[string]webv1.EventChannel{
    "terminal": webv1.EventChannel_EVENT_CHANNEL_TERMINAL,
    "state":    webv1.EventChannel_EVENT_CHANNEL_STATE,
    "both":     webv1.EventChannel_EVENT_CHANNEL_BOTH,
}
```

#### Validation Rules

Performed at manifest parse time (`ParseManifest`):

| Rule | Error |
| --- | --- |
| `type` MUST NOT be empty | `INVALID_MANIFEST` |
| `category` MUST be a known value | `INVALID_MANIFEST` |
| `format` MUST be a known value | `INVALID_MANIFEST` |
| `label` MUST be present when `format` is `speech` | `INVALID_MANIFEST` |
| `display_target` MUST be a known value | `INVALID_MANIFEST` |
| Duplicate `type` within same manifest | `INVALID_MANIFEST` |
| `setting` type plugins MUST NOT declare verbs | `INVALID_MANIFEST` |

### Loader Wiring

In `Manager.loadPlugin()`, after policy installation and before alias seeding:

1. Iterate `manifest.Verbs`
2. Convert each `VerbSpec` to `core.VerbRegistration` (with `Source` set to
   plugin name, `DisplayTarget` converted from string to enum)
3. Call `VerbRegistry.Register()` for each
4. If registration fails → call `VerbRegistry.UnregisterBySource(pluginName)`
   to clean up any verbs already registered in this batch, then return error
   (plugin fails to load)

Registration failure is fatal — same as a bad policy or invalid capability.

### Namespace Enforcement

Convention-based (B). Plugin verb types SHOULD be prefixed with the plugin's
domain (e.g., `channel_*` for `core-channels`). This is documented but not
enforced at load time. `Register()` duplicate detection serves as the safety
net — if two plugins try to register the same event type, the second fails.

### plugin info Integration

Add verbs to the `plugin info <name>` admin command output:

```text
Verbs: channel_say (communication/speech), channel_pose (communication/action),
       channel_join (state/notification), channel_leave (state/notification)
```

Format: `type (category/format)` — compact, scannable.

## Invariants

1. **No duplicate event types.** `Register()` rejects duplicates across
   builtins and all loaded plugins. A plugin MUST NOT register a verb type
   that conflicts with a builtin or another plugin.

2. **Speech requires label.** Any verb with `format: speech` MUST declare a
   `label`. Enforced at both manifest parse time and `Register()` time.

3. **Source tracking.** Every `VerbRegistration` MUST have a non-empty
   `Source` field. Builtins use `"builtin"`, plugins use their manifest name.

4. **Load-failure cleanup.** If verb registration fails partway through a
   plugin's verb list, all verbs from that plugin MUST be removed before the
   error propagates. No orphaned registrations.

5. **Fallback rendering.** Unknown event types (not registered in
   VerbRegistry) MUST continue to fall back to system/narrative/TERMINAL.
   This design does not change fallback behavior.

6. **Setting plugins excluded.** Plugins with `type: setting` MUST NOT
   declare verbs. Enforced at parse time.

## Consumer Impact

### Telnet handler (`internal/telnet/gateway_handler.go`)

No changes needed. The handler already:

- Looks up VerbRegistration via `Lookup()`
- Filters by `DisplayTarget` (skips state-only)
- Switches on `Category` with fallback to `formatFallback()`

Plugin verbs with known categories work through existing formatting. Unknown
categories hit the fallback path safely.

### Web translator (`internal/web/translate.go`)

No changes needed. The translator already:

- Looks up VerbRegistration via `Lookup()`
- Passes category, format, label, display_target through to `GameEvent` proto
- Falls back to system/narrative/TERMINAL for unknown types

Plugin verbs flow through to web clients with correct metadata.

### Web client (`web/src/lib/stores/eventRouter.ts`)

No changes needed. The client already:

- Routes by `displayTarget` as primary signal
- Sub-routes sidebar by `category`

Plugin events with correct `displayTarget` will route correctly.

## Out of Scope

| Item | Reason |
| --- | --- |
| Metadata keys in manifest | YAGNI. Additive later. |
| Emit-time validation | Separate concern. Unregistered types get fallback rendering today. |
| `emits`/verbs cross-validation | Orthogonal axes (stream namespaces vs event type rendering). |
| Full plugin unload/reload orchestration | Own epic. VerbRegistry API designed to support it. |
| Runtime verb registration (host function/gRPC) | No known use case. Additive later if needed. |

## Test Plan

### Unit Tests — VerbRegistry (`internal/core`)

| Test | Type |
| --- | --- |
| Register valid verb with all fields | Positive |
| Register speech verb with label | Positive |
| Reject empty type | Negative |
| Reject empty category | Negative |
| Reject empty format | Negative |
| Reject speech format without label | Negative |
| Reject duplicate type | Negative |
| Lookup returns registered verb | Positive |
| Lookup returns false for unknown type | Negative |
| Unregister removes entry, Lookup returns false | Positive |
| Unregister nonexistent type returns false | Boundary |
| UnregisterBySource removes all verbs from source | Positive |
| UnregisterBySource with unknown source returns 0 | Boundary |
| All() includes verbs from multiple sources | Positive |
| Source field preserved through Register/Lookup | Invariant |

### Unit Tests — Manifest Parsing (`internal/plugin`)

| Test | Type |
| --- | --- |
| Parse manifest with valid verbs section | Positive |
| Parse manifest with no verbs section | Positive |
| Reject unknown display_target value | Negative |
| Reject unknown category value | Negative |
| Reject unknown format value | Negative |
| Reject speech verb without label | Negative |
| Reject duplicate type within same manifest | Negative |
| Reject verbs on setting-type plugin | Negative |
| display_target string→enum conversion (terminal, state, both) | Boundary |
| display_target case insensitivity (TERMINAL, Terminal, terminal) | Boundary |

### Unit Tests — Loader Wiring (`internal/plugin`)

| Test | Type |
| --- | --- |
| loadPlugin registers verbs in VerbRegistry | Positive |
| loadPlugin sets Source to plugin name | Invariant |
| Invalid verb → loadPlugin returns error | Negative |
| Duplicate verb (cross-plugin) → second plugin fails | Negative |
| Duplicate verb (plugin vs builtin) → plugin fails | Boundary |
| Load failure after partial verb registration → verbs cleaned up | Invariant |
| Plugin with verbs appears in plugin info output | Positive |

### Integration Tests (`test/integration`)

| Test | Type |
| --- | --- |
| Full plugin load cycle: plugin with verbs → verbs in registry | Positive |
| Event with registered verb → web translator produces correct GameEvent fields | Positive |
| Event with registered verb → telnet formats according to registration | Positive |
| Plugin verb with display_target: state → telnet skips, web routes to sidebar | Boundary |
| Plugin verb with unknown category → telnet/web use fallback formatting | Boundary |

### E2E Tests

| Test | Type |
| --- | --- |
| Load plugin with verbs → `plugin info` shows verbs | Positive |
| Emit event with plugin-registered verb → correct rendering reaches client | Positive |

## References

- `internal/core/registry.go` — VerbRegistry implementation
- `internal/core/builtins.go` — builtin verb registrations
- `internal/plugin/manifest.go` — manifest parsing
- `internal/plugin/manager.go` — plugin loader
- `internal/web/translate.go` — web event translation
- `internal/telnet/gateway_handler.go` — telnet event formatting
- `web/src/lib/stores/eventRouter.ts` — web client event routing
- holomush-275o (closed) — extensible actions, pattern precedent
