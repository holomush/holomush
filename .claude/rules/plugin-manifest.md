<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

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
- `focus_redirects: [{focus_kind, verbs, target_command}]` — top-level verbs redirected to a target command when a connection has the given focus kind (e.g. `focus_kind: scene`, `verbs: [pose, say, ooc, emit]`, `target_command: scene` redirects a scene-focused connection's ambient verbs to the plugin's own command). Consumed generically by the host dispatcher (`command.WithFocusRedirects`) — core owns no verb/focus vocabulary. Validated at parse (known `focus_kind`, non-empty `verbs`, non-empty `target_command`) and at load (`target_command` resolves to a registered command; no duplicate `(focus_kind, verb)` pair across plugins).
- `actor_kinds_claimable: [<kind>...]` — which actor kinds this plugin may stamp on emitted events (`plugin`, `character`, etc.). Enforced by `event_emitter.go::Emit` for both Lua and binary runtimes — see `.claude/rules/plugin-runtime-symmetry.md`.
- `audit:` — declares plugin-owned audit subjects, schema, and table. The host audit projector ack-and-skips these; deliveries forward to the plugin's `PluginAuditService.AuditEvent` RPC.
- `crypto.emits: []` — declare event types whose payloads MUST be encrypted. Enforced by the crypto-reviewer agent.
- `commands: [...]` — telnet/web commands the plugin registers, each with required `capabilities: [{action, resource, scope}]` per the command-capability spec.
- `policies:` — ABAC policy files this plugin ships.

## Least-privilege parameters on `capability:` requires entries

Two optional fields narrow the host's enforcement of a capability requirement.
They are valid **only** on `capability:` entries — placing either on a `service:`
entry is a hard manifest load error (INV-PLUGIN-53):

| Field | Values | Purpose |
|-------|--------|---------|
| `access:` | `read` \| `write` | Declares the maximum operation class the plugin needs. The interceptor enforces it: a plugin declaring `access: read` cannot issue write-class host calls. |
| `scope:` | e.g., `own-location` | Declares the instance-level fence. The interceptor extracts the method's scoped resource and evaluates it against the scope policy; calls whose resource cannot be resolved from the request fail closed (`SCOPE_NO_RESOURCE`, INV-PLUGIN-52). |

**Example — a builder plugin that writes only within its own location:**

```yaml
requires:
  - capability: world.mutation
    access: write
    scope: own-location
```

`access:` and `scope:` are enforced by the host interceptor against the
host-owned `WorldMutationService`. They have no meaning on a `service:` entry
because the host does not own provider-plugin servers and cannot honor the
promise at the enforcement layer.

## DAG validation

The loader resolves dependencies as a DAG: every name in `requires` must appear in some other plugin's `provides`. Cycles error at startup. `holomush.plugin.v1.AttributeResolverService` is auto-registered by the host (do NOT declare in `provides` — causes `SERVICE_ALREADY_REGISTERED`).

## When you change a manifest

- If you add a `crypto.emits` entry → `crypto-reviewer` agent must run
- If you add an `actor_kinds_claimable` entry → both Lua and binary emits must respect it (the gate is at the common path; see plugin-runtime-symmetry rule)
- If you add a new `provides` → check no other plugin already provides the same service name
- The schema is checked by `task lint`; failures point at `schemas/plugin.schema.json`
