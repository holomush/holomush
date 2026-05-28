---
title: "Emitting Audit Events from Plugins"
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->


Plugins can emit audit events during command handler execution. These events flow through the same `audit.Logger` the ABAC engine uses, gaining WAL durability, mode routing, and operator-visible metrics — without any extra ops configuration.

Use this when your handler makes an authorization-relevant decision that the ABAC policy engine didn't catch. Examples: membership / ban / mute checks in a channels plugin, ownership checks in a building plugin, rate-limit denials in a communication plugin.

## When to Emit an Audit Event

| Scenario | Emit an event? |
| ----------------------------------------------------------------------- | --------------------------------------------------------------------------------- |
| Your handler denies a command because of handler-side state             | **Yes** — `audit.deny`                                                            |
| Your handler allows a command that involved a non-trivial state check   | **Yes, optionally** — `audit.allow` if the decision is operator-interesting       |
| ABAC policy engine denied the command before your handler ran           | **No** — the engine already emitted an event                                      |
| Your handler returned an internal error (DB down, invalid state)        | **No** — that's a system failure, not an authorization decision                   |

## Binary Plugins (Go)

Call `pluginsdk.Audit(ctx)` from your `HandleCommand` method. The returned recorder accumulates hints on the request context; the SDK serializes them into the response when your handler returns.

```go
import (
    "context"

    pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

func (h *handler) HandleCommand(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
    isMember, err := h.store.IsMember(channelID, req.PlayerID)
    if err != nil {
        return pluginsdk.Failuref("channel store unavailable"), nil
    }
    if !isMember {
        pluginsdk.Audit(ctx).Deny(
            "not_member",
            "player not in channel members",
            pluginsdk.AuditAttrs{"channel.type": "public"},
        )
        return pluginsdk.Errorf("You must join #%s before speaking there.", channelName), nil
    }

    // ... happy path ...
}
```

### Recorder Methods

| Method                                | Use                       |
| ------------------------------------- | ------------------------- |
| `Audit(ctx).Deny(id, message, attrs)` | Record a deny decision    |
| `Audit(ctx).Allow(id, message, attrs)` | Record an allow decision  |

### Field Conventions

| Field     | What to put here |
| --------- | ---------------- |
| `id`      | Plugin-internal rule slug. Short, kebab-or-snake-case, stable. Example: `"not_member"`, `"banned"`, `"archived"` |
| `message` | Per-firing description. Should mirror what the user sees in the error, or add operator-only context. Example: `"player X not in channel Y members"` |
| `attrs`   | Free-form context. Keys should be namespaced (`"channel.type"` not `"type"`) to avoid collision with host-merged keys |

## Lua Plugins

Call `audit.deny(...)` or `audit.allow(...)` in your Lua handler. The `audit` global is available in any plugin that declares `holomush.plugin.v1.AuditService` in its manifest `requires:` list.

```yaml
# plugin.yaml
requires:
  - holomush.plugin.v1.AuditService
```

```lua
-- main.lua
function handle_command(cmd)
    if not is_member(cmd.channel_id, cmd.player_id) then
        audit.deny(
            "not_member",
            "player not in channel members",
            {["channel.type"] = "public"}
        )
        return {
            status = "error",
            output = "You must join #" .. cmd.channel_name .. " before speaking there."
        }
    end
    -- ... happy path ...
end
```

### Lua Signature

```lua
audit.deny(id: string, message: string, attrs: table?)
audit.allow(id: string, message: string, attrs: table?)
```

The third argument is optional; omit it if no extra context is needed.

## What the Host Stamps

You do not set — and cannot set — the following fields. The dispatcher stamps them on every hint for anti-spoofing:

| Field       | Source |
| ----------- | ------ |
| `Subject`   | Dispatcher's dispatch context (the character running your command) |
| `Action`    | Command name (dispatcher-known) + your optional qualifier |
| `Source`    | Always `plugin` for hints you emit |
| `Component` | Your plugin's name (from the authenticated plugin identity) |
| `Timestamp` | Host clock at flush time |

## Failure Mode

Audit write failures never fail your command. The dispatcher logs the failure, bumps a Prometheus counter (`abac_audit_plugin_failures_total`), and returns your command response to the user unchanged. Your handler does not need to check for audit errors.

## Related Documentation

- [Access Control](/extending/access-control/) — how policies and audit events fit together
- [Binary Plugins](/extending/binary-plugins/) — Go plugin authoring guide
- [Lua Plugins](/extending/lua-plugins/) — Lua plugin authoring guide
- [AttributeResolverService](/extending/abac-attribute-resolver/) — for plugins that also want to expose custom resource attributes
