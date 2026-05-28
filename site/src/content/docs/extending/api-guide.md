---
title: "API Guide"
---

HoloMUSH uses a gRPC API for communication between the Gateway (protocol
servers) and the Core (game engine). If you are building a custom client,
writing integration tests, or just want to understand how the pieces fit
together, this guide walks through the protocol.

## The Big Picture

The Core service is the single source of truth for game state. Gateways --
telnet, web, or anything you build -- are thin protocol translators that
forward requests to Core over gRPC with mTLS.

```text
Client  -->  Gateway  --[gRPC/mTLS]-->  Core  -->  EventStore / WorldService
```

A gateway never accesses the database directly. It authenticates players, relays
commands, subscribes to event streams, and translates between its native protocol
(telnet escape codes, WebSocket frames, etc.) and gRPC.

## The Core RPCs

The Core service exposes a small set of RPCs for the runtime data path plus
two-phase login. The runtime path is what most clients spend their time on:

```protobuf
service CoreService {
  rpc HandleCommand(HandleCommandRequest) returns (HandleCommandResponse);
  rpc Subscribe(SubscribeRequest) returns (stream SubscribeResponse);
  rpc Disconnect(DisconnectRequest) returns (DisconnectResponse);
  // ... plus two-phase login: AuthenticatePlayer, SelectCharacter,
  // CreateGuest, CreatePlayer, etc.
}
```

### Two-Phase Login

Login is split into two RPCs so that registered players can choose between
multiple characters:

1. `AuthenticatePlayer(username, password)` validates credentials and returns a
   `player_session_token` plus the list of characters owned by the player.
2. `SelectCharacter(player_session_token, character_id)` creates or reattaches a
   game session for the chosen character and returns a `session_id`.

For ephemeral guest play, `CreateGuest()` short-circuits step 1 by issuing a
guest player + character + token in a single call; the client then calls
`SelectCharacter` exactly the same way.

### HandleCommand

Sends a game command for execution. Pass the `session_id` and the raw command
string (e.g., `"say Hello everyone!"`).

The response indicates whether the command succeeded. Command output is not
returned in the response itself -- it arrives as events on the Subscribe stream.
This keeps the command path simple and the event stream the single channel for
all game output.

**Supported commands:** `say`, `pose`, `look`, `move`, `quit`, `who`,
`describe`, `page`, `whisper`, `help`, `alias`.

### Subscribe

Opens a server-streaming connection that delivers events in real time. This is
how a client receives everything that happens: speech, movement, system messages,
and command responses.

The request includes the `session_id` and optionally a list of `streams` to
subscribe to. If you omit streams, the server subscribes to defaults based on
the character's current location.

### Disconnect

Ends a session and cleans up server-side resources. After disconnecting, the
`session_id` is no longer valid.

## Connection Lifecycle

A typical client session looks like this:

```text
1. Connect to Core (gRPC + mTLS)
2. AuthenticatePlayer (or CreateGuest)  -->  receive player_session_token
3. SelectCharacter                       -->  receive session_id
4. Subscribe                             -->  start receiving events
5. HandleCommand                         -->  send commands as the player types them
6. Disconnect                            -->  clean up when done
```

Steps 4 and 5 happen concurrently. The Subscribe stream runs for the lifetime of
the session, while HandleCommand calls happen on demand.

## The Subscribe Response and oneof Frames

`SubscribeResponse` uses a protobuf `oneof` to deliver two kinds of frames:

```protobuf
message SubscribeResponse {
  oneof frame {
    EventFrame event = 1;
    ControlFrame control = 2;
  }
}
```

**EventFrame** carries a game event -- speech, movement, system messages. It
includes the event ID, stream, type, timestamp, actor information, and a
JSON-encoded payload.

**ControlFrame** carries out-of-band signals from the server:

| Signal                         | Meaning                                             |
| ------------------------------ | --------------------------------------------------- |
| `CONTROL_SIGNAL_REPLAY_COMPLETE` | Historical replay is done; live events follow     |
| `CONTROL_SIGNAL_STREAM_CLOSED`   | A stream has been closed (location destroyed, etc.) |

When you first subscribe, the server may replay recent events so the client has
context. The `REPLAY_COMPLETE` signal tells you the replay is finished and
everything after it is live.

## Cursor-Based Reconnection

If a client disconnects and reconnects (network blip, Core restart), it can
resume where it left off. The `SubscribeRequest` has a `replay_from_cursor`
field. When set to `true`, the server replays events from the client's last
known position rather than from the beginning.

Track the `id` field of each `EventFrame` you receive. On reconnection, the
server uses the session's stored cursor to avoid re-sending events the client
already has.

## Error Handling

Errors come in two flavors:

**gRPC status codes** for transport and session problems:

| Code                | When                            |
| ------------------- | ------------------------------- |
| `OK`                | Request succeeded               |
| `UNAUTHENTICATED`   | Invalid or expired session      |
| `INTERNAL`          | Server error                    |
| `UNAVAILABLE`       | Core is temporarily unreachable |
| `DEADLINE_EXCEEDED` | Request timed out               |

**Application-level errors** in the response's `error` field with
`success=false`. For example, `HandleCommandResponse` may return
`error="Unknown command"` while the gRPC call itself succeeds.

The distinction matters: a gRPC `UNAUTHENTICATED` means the session is gone and
you need to re-authenticate. An application-level error means the session is
fine but the specific request had a problem.

## Request Metadata

Every request includes a `RequestMeta` with a `request_id` (ULID) and
`timestamp`. The server echoes the `request_id` back in `ResponseMeta`. Use this
for log correlation -- when something goes wrong, the request ID lets you trace
the full path through Gateway and Core logs.

## Security

All connections require mutual TLS (mTLS). The Gateway presents a client
certificate signed by the Root CA, and Core verifies it. Core's certificate
includes the `game_id` in its Subject Alternative Name, which the Gateway
validates to ensure it is connecting to the right server.

Certificates are stored under `$XDG_CONFIG_HOME/holomush/certs/`.

## Further Reading

The proto definition is at `api/proto/holomush/core/v1/core.proto`. For
field-level details on every request and response message, see the
[gRPC API Reference](../reference/grpc-api.md).
