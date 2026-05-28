---
title: "`actor_kinds_claimable`"
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

The `actor_kinds_claimable` field in your plugin's `plugin.yaml` declares which
actor kinds your plugin is allowed to vouch for on emitted events. It is the
operator-controlled trust boundary for plugin-emitted event identity: if your
plugin tries to emit an event whose actor kind is not in this list, the host
rejects the emit with a loud error.

## Schema

```yaml
# Default if absent: [plugin]
actor_kinds_claimable:
  - plugin
  - character
```

Allowed values:

| Value       | Meaning                                                                        |
| ----------- | ------------------------------------------------------------------------------ |
| `plugin`    | Plugin can vouch for plugin-actor cascades and its own identity. **Required.** |
| `character` | Plugin can also vouch for character-actor cascades (verb handlers, etc.).      |

`system` is rejected at manifest load — the host's system identity is never
claimable by plugins.

## Validation rules

| Rule                              | Error code on violation                |
| --------------------------------- | -------------------------------------- |
| MUST contain `plugin`             | `MANIFEST_ACTOR_KINDS_MISSING_PLUGIN`  |
| MUST NOT contain `system`         | `MANIFEST_ACTOR_KIND_SYSTEM_FORBIDDEN` |
| MUST only contain known kinds     | `MANIFEST_ACTOR_KIND_UNKNOWN`          |
| MUST be a list                    | `MANIFEST_ACTOR_KINDS_MALFORMED`       |
| Duplicates: silently deduplicated | n/a                                    |

## When does my plugin need `character`?

Add `character` to `actor_kinds_claimable` if EITHER of:

1. Your plugin declares `commands:` and emits events from those handlers
   (verb-style plugins like `say`, `pose`, `emote`).
2. Your plugin declares `events:` (subscribes to events that may carry
   character actors) AND emits events from `on_event` handlers (cascade
   responders like an echo bot).

If your plugin only emits with its own identity (`ActorPlugin:<your-name>`)
and never preserves cascade actors, leave the field at its default `[plugin]`.

## Loud failures

Plugins that emit during a character-driven dispatch without declaring
`character` will receive `EMIT_ACTOR_KIND_NOT_CLAIMABLE` errors at the host's
emit boundary. The error is loud by design — silent fallback would mask
plugin misconfiguration and corrupt the audit trail.

## Cascade preservation

When your plugin handles an event triggered by a character (e.g., a `say`
command), the host stamps `ActorCharacter:<dispatching-character>` on the
emit context. Your plugin's emits will be attributed to that character on
the published event — provided your manifest declares `character`.

This preserves audit-trail integrity: events document who *caused* the
chain, not just who emitted at each link.

## Two-token pattern (binary plugins)

Binary plugins undergo token authentication on the gRPC `EmitEvent`
boundary. There are two token types, each with a distinct issuance
trigger:

| Token            | Issuance                                                                                              |
| ---------------- | ----------------------------------------------------------------------------------------------------- |
| Dispatch token   | Issued automatically by the host on `DeliverEvent` / `DeliverCommand`; preserves the dispatch actor.  |
| Self-token       | Issued on demand via `RequestEmitToken` for out-of-dispatch emits as `ActorPlugin:<your-name>`.       |

The plugin's claimed actor metadata (`x-holomush-actor-kind` /
`-actor-id` headers) is no longer trusted as identity on its own — every
emit MUST present a host-issued token. This closes the forgery surface
where a binary plugin could otherwise substitute arbitrary actor IDs.

The two-token pattern is invisible to plugin authors using the standard
SDK: the SDK auto-ferries the dispatch token across the round-trip and
holds the self-token for plugin-identity emits. You will only encounter
tokens directly if you bypass the SDK or implement your own gRPC client.

### Token-related errors

| Error code              | Meaning                                                          |
| ----------------------- | ---------------------------------------------------------------- |
| `EMIT_TOKEN_MISSING`    | No token presented on `EmitEvent` — SDK bug or hand-rolled call. |
| `EMIT_TOKEN_REJECTED`   | Token expired, revoked, or not issued to this plugin.            |

## Common errors

| Error code                              | When you'll see it                                                          |
| --------------------------------------- | --------------------------------------------------------------------------- |
| `MANIFEST_ACTOR_KINDS_MISSING_PLUGIN`   | Manifest load: `actor_kinds_claimable` doesn't include `plugin`.            |
| `MANIFEST_ACTOR_KIND_SYSTEM_FORBIDDEN`  | Manifest load: `actor_kinds_claimable` includes `system` (always rejected). |
| `MANIFEST_ACTOR_KIND_UNKNOWN`           | Manifest load: an entry isn't a recognized actor kind.                      |
| `EMIT_ACTOR_KIND_NOT_CLAIMABLE`         | Emit time: claimed actor kind is not in your `actor_kinds_claimable` list.  |
| `EMIT_TOKEN_MISSING`                    | Binary plugin emit without a host-issued token.                             |
| `EMIT_TOKEN_REJECTED`                   | Binary plugin emit with an invalid, expired, or foreign token.              |

## Examples

### Verb plugin (commands → emits)

```yaml
name: core-say
version: 1.0.0
type: binary
commands:
  - say
actor_kinds_claimable:
  - plugin
  - character
```

### Cascade responder (subscribes → emits)

```yaml
name: echo-bot
version: 1.0.0
type: lua
events:
  - say
actor_kinds_claimable:
  - plugin
  - character
lua-plugin:
  entry: main.lua
```

### Plugin-identity-only emitter (default)

```yaml
name: heartbeat
version: 1.0.0
type: binary
# actor_kinds_claimable omitted — defaults to [plugin]
```

## Migration

If you maintain an out-of-tree plugin upgrading from a pre-`ec22.1` HoloMUSH
version: add `actor_kinds_claimable: [plugin, character]` to your manifest if
your plugin emits during character-driven dispatches. The first emit after
upgrade will loud-fail with `EMIT_ACTOR_KIND_NOT_CLAIMABLE` if the field is
missing.

## See also

- [Binary Plugin Author Guide](/extending/binary-plugins/) — full SDK reference.
- [Lua Plugin Author Guide](/extending/lua-plugins/) — Lua emit conventions.
- [Event Reference](/extending/events/) — event shape and actor metadata.
- [Audit Events](/extending/audit-events/) — how plugin emits flow into the audit trail.
