---
title: "Event Reference"
---

HoloMUSH is event-driven. Everything that happens in the game -- speech,
movement, object interactions -- produces an event. Plugins subscribe to events
and can emit new events in response. This page covers the event types, their
payloads, and the stream system that organizes them.

For event sensitivity (whisper, DM, private-scene content), see
[Declaring event sensitivity](/extending/how-to/event-sensitivity/).

## Communication Events

These events represent character communication within a location.

| Type      | Description                                                              | Payload                                                              |
| --------- | ------------------------------------------------------------------------ | -------------------------------------------------------------------- |
| `say`     | Character speech                                                         | `{"message": "text"}`                                                |
| `pose`    | Character action/emote                                                   | `{"message": "text"}`                                                |
| `page`    | Private message to another character                                     | `{"message": "text", "sender": "name", "recipient": "name"}`        |
| `whisper` | Whispered message in location                                            | `{"message": "text", "sender": "name", "recipient": "name"}`        |
| `arrive`  | Character enters location                                                | `{"character_name": "name", "from": "location_name"}`               |
| `leave`   | Character leaves location                                                | `{"character_name": "name", "to": "location_name"}`                 |
| `system`  | Server-generated notification (login announcements, maintenance warnings, etc.) | `{"message": "text"}`                                                |

## World Events

These events represent changes to the game world.

| Type             | Description                      | Payload Fields                                                                                    |
| ---------------- | -------------------------------- | ------------------------------------------------------------------------------------------------- |
| `move`           | Character or object moved        | `entity_type`, `entity_id`, `from_type`, `from_id`, `to_type`, `to_id`, `exit_id?`, `exit_name?` |
| `object_create`  | Object created                   | `object_id`, `object_name`, `location_id`                                                         |
| `object_destroy` | Object destroyed                 | `object_id`, `object_name`                                                                        |
| `object_use`     | Object used                      | `object_id`, `object_name`, `character_id`                                                        |
| `object_examine` | Object examined                  | `object_id`, `object_name`, `character_id`                                                        |
| `object_give`    | Object transferred between chars | `object_id`, `object_name`, `from_character_id`, `to_character_id`                                |

See the [World Model Design](https://github.com/holomush/holomush/blob/main/docs/specs/2026-01-22-world-model-design.md)
for complete payload specifications.

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
