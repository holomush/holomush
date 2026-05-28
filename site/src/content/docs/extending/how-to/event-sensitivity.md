---
title: "Declaring event sensitivity"
---

Some events shouldn't be readable by every plugin on the server. A whisper
carries content only the sender and recipient should see. A page between two
characters is a private conversation. A storyteller's `pemit` is directed at
one character only. HoloMUSH uses sensitivity contracts to model this: each
event type is declared as always-sensitive, sometimes-sensitive, or never-sensitive,
and plugins that want plaintext of sensitive events have to say so explicitly.

Phase 1 records these declarations in the manifest and validates them at load
time. Runtime enforcement (payload encryption, delivery filtering) comes in
Phase 3.

## What sensitivity means

Each event type gets one of three contracts:

| Contract  | Meaning                                                          | Examples                                    |
| --------- | ---------------------------------------------------------------- | ------------------------------------------- |
| `always`  | Every event of this type is sensitive, no exceptions            | `page`, `whisper`, `pemit`                  |
| `may`     | Sensitivity is decided per-event at emit time                   | A custom scene event that can be private    |
| `never`   | This event type is never sensitive                              | `say`, `pose`, `arrive`                     |

`always` is the right choice when there's no scenario where the content should
be broadcast. `may` is for event types where the emit site (a command handler or
plugin) holds the context to decide. `never` covers events that are by definition
public — location speech, movement, world changes.

Traditional MUSHes enforced this at the application layer, buried in individual
command handlers. HoloMUSH moves it into the manifest so it's machine-readable
and can be enforced uniformly.

## Declaring emitted event types

The `crypto.emits` block in `plugin.yaml` lists every event type your plugin
emits, along with its sensitivity contract:

```yaml
crypto:
  emits:
    - event_type: whisper
      sensitivity: always
      description: "In-character private message between two characters in the same location."
    - event_type: whisper_notice
      sensitivity: never
      description: "Public notice that a whisper occurred (no content), visible in the location."
    - event_type: scene_ic
      sensitivity: may
      description: "In-character scene post; private when the scene is closed."
```

The `description` field is optional but gets picked up by `task docs:gen-events`
to populate the auto-generated event reference. It's worth filling in.

If your plugin emits events but omits the `crypto` block entirely, the runtime
treats all of them as `never`. For plugins that only handle public game events
that's fine. For anything involving private communication, you need the block.

## Consuming sensitive events from another plugin

If your plugin subscribes to a stream that carries sensitive events from another
plugin, use `crypto.consumes` to declare what you're subscribing to and, per
event type, whether you need plaintext:

```yaml
crypto:
  consumes:
    - subjects:
        - "events.*.character.>"
      requests_decryption:
        - "core-communication:page"
        - "core-communication:whisper"
```

The `subjects` list uses the same NATS subject patterns as your event
subscriptions. The `requests_decryption` list names the event types whose
payload you actually need to read, using the qualified `<plugin>:<event_type>`
format. Types you don't list stay encrypted when delivery gating is enforced.

`requests_decryption` is intentionally explicit. You can't opt into decryption
of an event type without naming it, and the loader validates that every type
you name is declared `always` or `may` by the plugin that owns it — you can't
request decryption of a `never`-sensitivity type.

## What handlers receive for sensitive events

In Phase 3, when a plugin receives a sensitive event and has not declared
`requests_decryption` for that event type, the payload will be stripped. The
event still arrives — `id`, `stream`, `type`, `timestamp`, `actor_kind`,
`actor_id` are all present — but `payload` is empty.

A handler that might receive either form should check before parsing:

```go
func (h *MyHandler) HandleEvent(ctx context.Context, event *pluginsdk.Event) error {
    if event.Payload == "" {
        // Sensitive event, payload not available to this plugin.
        // You can still react to the event's metadata.
        return nil
    }
    // Parse and process payload.
    var p MyPayload
    if err := json.Unmarshal([]byte(event.Payload), &p); err != nil {
        return err
    }
    // ...
    return nil
}
```

This is a Phase 3 behavior. In Phase 1, payloads are delivered as-is regardless
of sensitivity declarations.

## Runtime gates (Phase 3)

Phase 1 records sensitivity declarations and validates them at plugin load time.
Phase 3 adds:

- Payload encryption at emit time for `always` and (when flagged) `may` events
- AuthGuard enforcement: plugins without `requests_decryption` receive stripped payloads
- Audit of decryption requests

No code changes to your manifest are needed for Phase 3. If your declarations
are correct now, they'll carry forward.

## See also

- [Plugin Guide](/extending/plugin-guide/) — manifest structure, event handlers, ABAC policies
- [Event Reference](/reference/events/) — event types, payload schemas, stream patterns
- [Auto-generated event reference](/reference/events/) — per-plugin sensitivity tables
