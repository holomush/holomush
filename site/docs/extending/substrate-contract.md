<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Substrate Contract

The substrate contract describes the rules that govern what plugins can rely on
from the substrate and what they MUST NOT touch. Every plugin in HoloMUSH is a
consumer of the substrate; this page is the orientation guide for that
relationship.

Canonical detail lives in the design spec. This page is the on-ramp — read this
first, then follow the links for the full specification:
[Substrate Contract Design Spec](../../../docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md).

---

## Substrate primitives plugin authors can rely on

These surfaces are stable for the lifetime of the spec. You do not need to
implement them; the substrate provides them turn-key.

| Primitive                           | What it does                                                                                       | Where it lives                             |
| ----------------------------------- | -------------------------------------------------------------------------------------------------- | ------------------------------------------ |
| JetStream event bus                 | Durable per-stream delivery, replay, history fallback, ULID identity, JetStream-seq ordering       | `internal/eventbus/` — `Publisher`, `Subscriber`, `HistoryReader` interfaces |
| Crypto envelope                     | Per-event-type sensitivity enforcement, KEK/DEK rotation, AuthGuard fence, INV-50 downgrade fence  | `internal/plugin/event_emitter.go`, `internal/plugin/sensitivity_fence.go` |
| Manifest emit-type validation (INV-S5) | Startup-time fail-fast when manifest declared set does not equal code-registered set            | `internal/plugin/manager.go::loadPlugin`   |
| ABAC engine                         | Policy evaluation, attribute resolution wiring, default-deny posture                               | `internal/access/` — `access.Engine` interface |
| Focus coordinator                   | Per-connection subscription state, multi-tab visibility, restore-on-reconnect                      | `internal/grpc/focus/`, `pkg/plugin/focus_client.go` |
| Plugin host RPCs (Go + Lua parity)  | `Emit`, `QueryHistory`, `JoinFocus`/`LeaveFocus`/`PresentFocus`, `PluginAuditService.AuditEvent`  | `pkg/plugin/` (Go SDK), `internal/plugin/hostfunc/` (Lua hostfuncs) |
| Per-plugin storage                  | Isolated Postgres schema + role, embedded migration runner, `search_path` scoping                  | `internal/store/`, plugin's `migrations/`  |
| Audit projection                    | Host ack-and-skip for plugin-owned subjects; dispatch to plugin's `AuditEvent` RPC                 | `internal/eventbus/audit/`                 |

For the full substrate inventory with `path:line` citations, see
[§1 of the substrate-contract spec](../../../docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md#1-substrate-inventory).

---

## Plugin-boundary rule INV-S1

**Plugin PRs MUST touch only `plugins/<plugin-name>/`** and may import from
approved substrate-facing packages (`pkg/plugin/*`, generated proto under
`pkg/proto/`). That is the complete permitted footprint.

If your plugin needs a substrate capability that does not yet exist, that
substrate change is **separate work**: its own bead, its own PR, its own review
gate. Bundling a substrate change inside a plugin PR is forbidden — it bypasses
the substrate review gates (`crypto-reviewer`, `abac-reviewer`, `code-reviewer`).

The dual invariant is **INV-S2**: substrate (`internal/`) MUST stay domain-free.
No `internal/` package may contain entity types, event vocabularies, or domain
logic for scenes, channels, forums, discord, or any other specific use. INV-S1
keeps plugin work out of substrate; INV-S2 keeps domain knowledge out of
substrate.

For the full boundary definition and anti-patterns, see:
[ADR holomush-z1e7 — Strict Plugin-Boundary: Plugins Must Not Modify internal/](../../../docs/adr/holomush-z1e7-strict-plugin-boundary.md).

---

## Manifest emit-type validation INV-S5

INV-S5 requires set-equality between your manifest's declared emit types and
your code's registered emit types. Mismatch fails plugin load with error code
`EVENT_TYPE_REGISTRY_MISMATCH`. This catches two failure modes that the
runtime emit gate misses:

- **Declared-but-unregistered:** a `crypto.emits` entry the code never emits
  (dead declaration or typo).
- **Registered-but-undeclared:** plugin code emitting a type the manifest never
  declared, silently as plaintext.

INV-S5 applies only to plugins with a non-empty `crypto.emits` block. Plugins
without `crypto.emits` skip the check entirely.

### Step 1 — Declare types in plugin.yaml

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

### Step 2 — Register types in code

#### Lua plugins

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

#### Binary plugins

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

For the full mechanism design including proto extension and host-side validator,
see [INV-S5 Mechanism Design Spec](../../../docs/superpowers/specs/2026-05-17-inv-s5-mechanism-design.md)
and [ADR holomush-3vsb — Startup-Time Set-Equality Validation of crypto.emits Declarations](../../../docs/adr/holomush-3vsb-manifest-emit-type-startup-validation.md).

---

## eventkit and groupkit SDKs (named, not yet built)

`pkg/plugin/eventkit/` and `pkg/plugin/groupkit/` are co-designed in the
substrate-contract spec but their code lands **only after N=2 validation**
(INV-S7): two distinct use plugins must adopt a primitive cleanly before it is
extracted to substrate SDK code.

**Today:** plugins implement event-replay, group-membership, and focus-wire
patterns inline (scenes-bespoke, channels-bespoke).

**After N=2 validation:** the primitive is extracted to the appropriate package:

| SDK          | Location                    | Primitives (planned)                          | Who can use it       |
| ------------ | --------------------------- | --------------------------------------------- | -------------------- |
| `eventkit`   | `pkg/plugin/eventkit/`      | `replay` (ABAC-filtered history fan-out), `cryptoemit` (call-site sensitivity assertion) | Any plugin emitting ABAC-gated sensitive events |
| `groupkit`   | `pkg/plugin/groupkit/`      | `membership`, `focuswire`, `groupabac`        | Uses with explicit member-of-entity state (scenes, channels) |

Forums uses `eventkit` only. Discord defaults to no SDK. **INV-S10 forbids
forums and discord from importing `groupkit`.**

Relevant ADRs:

- [holomush-p7w0 — Split Plugin SDK into eventkit and groupkit by Scope](../../../docs/adr/holomush-p7w0-split-plugin-sdk-eventkit-groupkit.md)
- [holomush-lrt3 — Require N=2 Consumer Validation Before SDK Primitive Extraction](../../../docs/adr/holomush-lrt3-n2-consumer-validation-sdk-extraction.md)

---

## References

**Specs:**

- [Substrate Contract Design Spec](../../../docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md) — canonical detail for §1 (substrate inventory), §2 (boundary invariants), §3 (SDK design), §4 (per-use validation)
- [INV-S5 Mechanism Design Spec](../../../docs/superpowers/specs/2026-05-17-inv-s5-mechanism-design.md) — runtime mechanism for binary and Lua emit-type registration and host-side validation

**ADRs (theme:social-spaces):**

- [holomush-p7w0 — Split Plugin SDK into eventkit and groupkit by Scope](../../../docs/adr/holomush-p7w0-split-plugin-sdk-eventkit-groupkit.md)
- [holomush-lrt3 — Require N=2 Consumer Validation Before SDK Primitive Extraction](../../../docs/adr/holomush-lrt3-n2-consumer-validation-sdk-extraction.md)
- [holomush-z1e7 — Strict Plugin-Boundary: Plugins Must Not Modify internal/](../../../docs/adr/holomush-z1e7-strict-plugin-boundary.md)
- [holomush-3vsb — Startup-Time Set-Equality Validation of crypto.emits Declarations](../../../docs/adr/holomush-3vsb-manifest-emit-type-startup-validation.md)
- [holomush-c8a9 — Enforce Scene Privacy at Plugin Code, Not ABAC Engine](../../../docs/adr/holomush-c8a9-scene-privacy-plugin-code-enforcement.md)
- [holomush-vie9 — Use Init-RPC Protocol Extension to Communicate Code-Registered Emit Types](../../../docs/adr/holomush-vie9-init-rpc-emit-type-communication.md)
- [holomush-7h0c — Scope Lua Load Capture Pass to crypto.emits-Declaring Plugins Only](../../../docs/adr/holomush-7h0c-lua-load-pass-optin-scope.md)
