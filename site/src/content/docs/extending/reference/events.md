---
title: "Event Reference"
---

HoloMUSH is event-driven. Everything that happens in the game -- speech,
movement, object interactions -- produces an event. Plugins subscribe to events
and can emit new events in response. This page points you to the event-type
catalogue and payload specs, then covers the stream system that organizes them.

For event sensitivity (whisper, DM, private-scene content), see
[Declaring event sensitivity](/extending/how-to/event-sensitivity/).

## Event types

The authoritative, per-plugin catalogue of event types (with sensitivity) is
auto-generated from plugin manifests — see the
[Event type reference](/reference/events/). For complete payload schemas, see the
[World Model Design](https://github.com/holomush/holomush/blob/main/docs/specs/2026-01-22-world-model-design.md).

## Stream Patterns

Events are organized into streams. Each stream represents a scope -- a location,
a channel, or a specific character. Plugins subscribe to streams using patterns:

| Pattern          | Matches                         |
| ---------------- | ------------------------------- |
| `location:<id>`  | Events in a specific location   |
| `location:*`     | All location events             |
| `channel:<name>` | A specific channel              |
| `character:<id>` | Private messages, notifications |
| `global:*`       | All global events               |
| `*`              | Everything                      |

Most plugins subscribe to `location:*` or specific event types within locations.
The `character:<id>` stream carries private messages (pages, whispers) and
per-character notifications.
