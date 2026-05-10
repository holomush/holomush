---
paths:
  - "plugins/*/plugin.yaml"
  - "plugins/*/manifest.yaml"
---

# Plugin Manifest Conventions

Each plugin in `plugins/<n>/` has a `plugin.yaml` that the loader consumes.

## Required top-level fields

| Field | Purpose |
|-------|---------|
| `name` | Plugin name; matches the directory |
| `version` | SemVer (e.g., `1.0.0`) |
| `type` | `lua` \| `binary` \| `setting` |
| `resource_types` | List of ABAC resource types this plugin owns (e.g., `[scene]`) |
| `requires` | Proto service names this plugin consumes (e.g., `holomush.world.v1.WorldService`) |
| `provides` | Proto service names this plugin implements |

## Conditionally required

| Field | Purpose |
|-------|---------|
| `storage` | Required when the plugin persists to its own DB (`postgres`); omit otherwise |

## Optional but consequential

- `emits: [<event-domain>...]` — event domains the plugin publishes (e.g., `[scene]` ⇒ `events.<game_id>.scene.>`)
- `actor_kinds_claimable: [<kind>...]` — which actor kinds this plugin may stamp on emitted events (`plugin`, `character`, etc.). Enforced by `event_emitter.go::Emit` for both Lua and binary runtimes — see `.claude/rules/plugin-runtime-symmetry.md`.
- `audit:` — declares plugin-owned audit subjects, schema, and table. The host audit projector ack-and-skips these; deliveries forward to the plugin's `PluginAuditService.AuditEvent` RPC.
- `crypto.emits: []` — declare event types whose payloads MUST be encrypted. Enforced by the crypto-reviewer agent.
- `commands: [...]` — telnet/web commands the plugin registers, each with required `capabilities: [{action, resource, scope}]` per the command-capability spec.
- `policies:` — ABAC policy files this plugin ships.

## DAG validation

The loader resolves dependencies as a DAG: every name in `requires` must appear in some other plugin's `provides`. Cycles error at startup. `holomush.plugin.v1.AttributeResolverService` is auto-registered by the host (do NOT declare in `provides` — causes `SERVICE_ALREADY_REGISTERED`).

## When you change a manifest

- If you add a `crypto.emits` entry → `crypto-reviewer` agent must run
- If you add an `actor_kinds_claimable` entry → both Lua and binary emits must respect it (the gate is at the common path; see plugin-runtime-symmetry rule)
- If you add a new `provides` → check no other plugin already provides the same service name
- The schema is checked by `task lint`; failures point at `schemas/plugin.schema.json`
