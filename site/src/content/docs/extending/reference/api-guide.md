---
title: "Core gRPC API Reference"
description: "Reference for the CoreService gRPC API — connection lifecycle, RPCs, message shapes, and error handling for gateway and client developers."
---

**Who this is for:** developers building custom clients, protocol gateways, or
integration tests against HoloMUSH. If you are writing a plugin (Lua or binary),
you do not call CoreService directly — see the
[Plugin Guide](/extending/tutorials/plugin-guide/) instead.

HoloMUSH exposes a gRPC service (`CoreService`) that gateways and clients use
to authenticate, send commands, and receive events. The full proto definition is
at `api/proto/holomush/core/v1/core.proto`. For field-level message details,
see the [gRPC API Reference](/reference/grpc-api/).

## Architecture

Core is the single source of truth for game state. Gateways — telnet, web, or
anything you build — are thin protocol translators that forward requests to Core
over gRPC with mTLS.

```text
Client  -->  Gateway  --[gRPC/mTLS]-->  Core  -->  EventBus / WorldService
```

A gateway never accesses the database directly. It authenticates players, relays
commands, subscribes to event streams, and translates between its native protocol
(telnet escape codes, WebSocket frames, etc.) and gRPC.

## CoreService RPCs

`CoreService` (defined in `api/proto/holomush/core/v1/core.proto`) exposes the
following RPCs. The runtime data path — the three RPCs most clients call in a
session — is:

```protobuf
service CoreService {
  rpc HandleCommand(HandleCommandRequest) returns (HandleCommandResponse);
  rpc Subscribe(SubscribeRequest) returns (stream SubscribeResponse);
  rpc Disconnect(DisconnectRequest) returns (DisconnectResponse);
}
```

The full service also includes two-phase login, account management, and session
management RPCs (see the [gRPC API Reference](/reference/grpc-api/) for the
complete list).

### Two-phase login

Login is split into two RPCs so registered players can choose between multiple
characters:

1. `AuthenticatePlayer` — validates credentials and returns a
   `player_session_token` plus the list of characters owned by the player.
2. `SelectCharacter` — creates or reattaches a game session for the chosen
   character and returns a `session_id`.

For ephemeral guest play, `CreateGuest` short-circuits step 1 by issuing a guest
player, character, and token in a single call; the client then calls
`SelectCharacter` the same way.

### HandleCommand

Pass the `session_id` and the raw command string (e.g., `"say Hello everyone!"`).

The response indicates whether the command was accepted. Command output does not
come back in the response — it arrives as events on the Subscribe stream. The
event stream is the single channel for all game output.

### Subscribe

Opens a server-streaming connection that delivers events in real time. The
request includes the `session_id` and optionally a list of `streams` to
subscribe to. If you omit streams, the server uses defaults based on the
character's current location.

### Disconnect

Ends a session and cleans up server-side state. After this call, the `session_id`
is no longer valid.

## Connection lifecycle

A typical client session:

```text
1. Connect to Core (gRPC + mTLS)
2. AuthenticatePlayer (or CreateGuest)  →  player_session_token
3. SelectCharacter                       →  session_id
4. Subscribe                             →  start receiving events (stream)
5. HandleCommand                         →  send commands as the player types
6. Disconnect                            →  clean up when done
```

Steps 4 and 5 run concurrently. Subscribe is a long-lived stream; HandleCommand
calls happen on demand alongside it.

## SubscribeResponse frames

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

## Reconnection and cursors

If a client disconnects and reconnects (network blip, Core restart), the server
restores the session's subscribed streams and replays recent events via
`FocusCoordinator.RestoreFocus`. Track the `cursor` field of each `EventFrame`
you receive — the server uses the session's stored cursor position to avoid
re-delivering events the client already processed.

## Error handling

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

## Request metadata

Every request includes a `RequestMeta` with a `request_id` (ULID) and
`timestamp`. The server echoes the `request_id` back in `ResponseMeta`. Use it
for log correlation — when something goes wrong, the request ID traces the full
path through Gateway and Core logs.

## Security

All connections require mutual TLS (mTLS). The Gateway presents a client
certificate signed by the Root CA, and Core verifies it. Core's certificate
includes the `game_id` in its Subject Alternative Name, which the Gateway
validates to ensure it is connecting to the right server.

Certificates are stored under `$XDG_CONFIG_HOME/holomush/certs/`.

## See also

- [gRPC API Reference](/reference/grpc-api/) — field-level details for every request/response message
- [Plugin Guide](/extending/tutorials/plugin-guide/) — if you are building a plugin rather than a client
- [Lua Plugin Guide](/extending/tutorials/lua-plugins/) and [Binary Plugin Guide](/extending/tutorials/binary-plugins/) — plugin author guides
