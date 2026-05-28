---
title: "Register plugin emit types"
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

This guide shows how to declare and register your plugin's event emit types so
they satisfy manifest emit-type validation (INV-S5). For why the invariant
exists and what it catches, see
[Substrate contract invariants](/extending/explanation/substrate-invariants/#manifest-emit-type-validation-inv-s5).

This procedure applies only to plugins with a non-empty `crypto.emits` block.
Plugins without `crypto.emits` skip emit-type validation entirely.

## Step 1 — Declare types in plugin.yaml

```yaml
crypto:
  emits:
    - event_type: scene_ic
      sensitivity: always
      description: "In-character scene pose. Always encrypted."
    - event_type: scene_ooc
      sensitivity: may
      description: "Out-of-character aside. Caller decides per-emit."
```

Fields: `event_type` (string), `sensitivity` (`always`/`may`/`never`),
`description` (human-readable rationale).

## Step 2 — Register types in code

### Lua plugins

Call `holomush.register_emit_type(<type>)` **at top level** of `main.lua` for
every event type the plugin may emit:

```lua
holomush.register_emit_type("scene_ic")
holomush.register_emit_type("scene_ooc")
```

The substrate's Load pass captures these calls and validates them against the
manifest before marking the plugin ready.

**Top-level idempotency note:** top-level code in `main.lua` runs once at Load
AND once per event/command delivery. Limit top-level code to:

- `local function` and `local <const>` declarations
- `holomush.register_emit_type(...)` calls

Non-idempotent hostfunc calls (`kv_set`, `create_location`, etc.) at top level
are forbidden — they fire repeatedly on every delivery. Put those calls inside
`on_event` or `on_command` handlers.

### Binary plugins

Implement the `pluginsdk.EmitTypeRegistrar` interface. The SDK adapter
auto-populates `InitResponse.registered_emit_types` from your registry:

```go
type scenePlugin struct {
    registry *pluginsdk.EmitRegistry
    // ...
}

func newScenePlugin() *scenePlugin {
    r := pluginsdk.NewEmitRegistry()
    r.RegisterEmitType("scene_ic")
    r.RegisterEmitType("scene_ooc")
    return &scenePlugin{registry: r}
}

// EmitRegistry implements pluginsdk.EmitTypeRegistrar.
func (p *scenePlugin) EmitRegistry() *pluginsdk.EmitRegistry {
    return p.registry
}
```

Register types during construction (in `main()` before `pluginsdk.ServeWithServices`,
or inside `Init`) so the registry is populated when the host reads it.

## Further detail

For the full mechanism design including proto extension and host-side validator,
see [INV-S5 Mechanism Design Spec](https://github.com/holomush/holomush/blob/main/docs/superpowers/specs/2026-05-17-inv-s5-mechanism-design.md)
and [ADR holomush-3vsb — Startup-Time Set-Equality Validation of crypto.emits Declarations](https://github.com/holomush/holomush/blob/main/docs/adr/holomush-3vsb-manifest-emit-type-startup-validation.md).
