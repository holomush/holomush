# gRPC API Reference

This document describes the gRPC API used for communication between Gateway and Core processes.

## Overview

The Core service is the main game service exposed by the Core process. Gateway connects to this service over gRPC with mTLS authentication.

**Protocol:** gRPC over mTLS (TLS 1.3)
**Default Address:** `localhost:9000`
**Proto Package:** `holomush.core.v1`

## Service Definition

```protobuf
service Core {
  rpc Authenticate(AuthRequest) returns (AuthResponse);
  rpc HandleCommand(CommandRequest) returns (CommandResponse);
  rpc Subscribe(SubscribeRequest) returns (stream Event);
  rpc Disconnect(DisconnectRequest) returns (DisconnectResponse);
}
```

## Common Types

### RequestMeta

Metadata included with every request for correlation and debugging.

| Field      | Type                      | Description              |
| ---------- | ------------------------- | ------------------------ |
| request_id | string                    | ULID for log correlation |
| timestamp  | google.protobuf.Timestamp | Request timestamp        |

### ResponseMeta

Metadata echoed back in every response.

| Field      | Type                      | Description                         |
| ---------- | ------------------------- | ----------------------------------- |
| request_id | string                    | Echoed from request for correlation |
| timestamp  | google.protobuf.Timestamp | Response timestamp                  |

## RPCs

### Authenticate

Validates credentials and creates a session for the authenticated character.

**Request:** `AuthRequest`

| Field    | Type        | Description      |
| -------- | ----------- | ---------------- |
| meta     | RequestMeta | Request metadata |
| username | string      | Account username |
| password | string      | Account password |

**Response:** `AuthResponse`

| Field          | Type         | Description                                   |
| -------------- | ------------ | --------------------------------------------- |
| meta           | ResponseMeta | Response metadata                             |
| success        | bool         | Whether authentication succeeded              |
| session_id     | string       | Session ID (ULID) for subsequent requests     |
| character_id   | string       | Character ID (ULID) of the authenticated char |
| character_name | string       | Display name of the character                 |
| error          | string       | Error message if authentication failed        |

**Error Handling:**

| Scenario            | Response                                       |
| ------------------- | ---------------------------------------------- |
| Invalid credentials | `success=false`, `error="Invalid credentials"` |
| Account locked      | `success=false`, `error="Account locked"`      |
| Database error      | gRPC status `INTERNAL`                         |

**Example:**

```go
resp, err := client.Authenticate(ctx, &corev1.AuthRequest{
    Meta: &corev1.RequestMeta{
        RequestId: ulid.Make().String(),
        Timestamp: timestamppb.Now(),
    },
    Username: "testuser",
    Password: "password",
})
if err != nil {
    // Handle gRPC error
}
if !resp.Success {
    // Handle authentication failure
    fmt.Println("Auth failed:", resp.Error)
}
// Use resp.SessionId for subsequent requests
```

### HandleCommand

Processes a game command from an authenticated session.

**Request:** `CommandRequest`

| Field      | Type        | Description                                |
| ---------- | ----------- | ------------------------------------------ |
| meta       | RequestMeta | Request metadata                           |
| session_id | string      | Session ID from Authenticate               |
| command    | string      | The command to execute (e.g., "say Hello") |

**Response:** `CommandResponse`

| Field   | Type         | Description                           |
| ------- | ------------ | ------------------------------------- |
| meta    | ResponseMeta | Response metadata                     |
| success | bool         | Whether command executed successfully |
| output  | string       | Command output to display to user     |
| error   | string       | Error message if command failed       |

**Error Handling:**

| Scenario          | Response                                     |
| ----------------- | -------------------------------------------- |
| Invalid session   | gRPC status `UNAUTHENTICATED`                |
| Unknown command   | `success=false`, `error="Unknown command"`   |
| Permission denied | `success=false`, `error="Permission denied"` |

**Supported Commands:**

| Command         | Description                       |
| --------------- | --------------------------------- |
| `look`          | View current location description |
| `say <message>` | Speak to others in the location   |
| `pose <action>` | Emote an action                   |
| `quit`          | End the session                   |

### Subscribe

Opens a server-streaming connection to receive events for a session.

**Request:** `SubscribeRequest`

| Field      | Type            | Description                             |
| ---------- | --------------- | --------------------------------------- |
| meta       | RequestMeta     | Request metadata                        |
| session_id | string          | Session ID from Authenticate            |
| streams    | repeated string | Stream names to subscribe to (optional) |

If `streams` is empty, the server subscribes to default streams based on the character's current location and subscriptions.

**Response:** `stream Event`

The server streams events as they occur. Each event contains:

| Field      | Type                      | Description                               |
| ---------- | ------------------------- | ----------------------------------------- |
| id         | string                    | Event ID (ULID)                           |
| stream     | string                    | Stream name (e.g., "location:42")         |
| type       | string                    | Event type (e.g., "say", "pose")          |
| timestamp  | google.protobuf.Timestamp | When the event occurred                   |
| actor_type | string                    | Type of actor (character, system, plugin) |
| actor_id   | string                    | Actor identifier                          |
| payload    | bytes                     | JSON-encoded event payload                |

**Stream Types:**

| Stream Pattern   | Description                        |
| ---------------- | ---------------------------------- |
| `location:<id>`  | Events in a location (says, poses) |
| `channel:<name>` | Channel messages                   |
| `char:<id>`      | Private messages, notifications    |

**Event Types:**

| Type     | Payload Fields                                |
| -------- | --------------------------------------------- |
| `say`    | `{"message": "...", "character_name": "..."}` |
| `pose`   | `{"action": "...", "character_name": "..."}`  |
| `arrive` | `{"character_name": "...", "from": "..."}`    |
| `leave`  | `{"character_name": "...", "to": "..."}`      |
| `system` | `{"message": "..."}`                          |

**Error Handling:**

| Scenario        | Behavior                               |
| --------------- | -------------------------------------- |
| Invalid session | gRPC status `UNAUTHENTICATED`          |
| Core restart    | Stream closes, client should reconnect |

### Disconnect

Ends a session and cleans up resources.

**Request:** `DisconnectRequest`

| Field      | Type        | Description       |
| ---------- | ----------- | ----------------- |
| meta       | RequestMeta | Request metadata  |
| session_id | string      | Session ID to end |

**Response:** `DisconnectResponse`

| Field   | Type         | Description                  |
| ------- | ------------ | ---------------------------- |
| meta    | ResponseMeta | Response metadata            |
| success | bool         | Whether disconnect succeeded |

## Error Handling

The API uses standard gRPC status codes for transport-level errors:

| Code                | Description                     |
| ------------------- | ------------------------------- |
| `OK`                | Request succeeded               |
| `UNAUTHENTICATED`   | Invalid or expired session      |
| `INTERNAL`          | Server error (database, etc.)   |
| `UNAVAILABLE`       | Service temporarily unavailable |
| `DEADLINE_EXCEEDED` | Request timed out               |

Application-level errors are returned in the response's `error` field with `success=false`.

## OpenTelemetry Integration

All RPC calls propagate OpenTelemetry trace context automatically via gRPC metadata. This enables distributed tracing across Gateway and Core.

### Trace Context

Logs from both Gateway and Core include:

| Field      | Description                    |
| ---------- | ------------------------------ |
| `trace_id` | OpenTelemetry trace identifier |
| `span_id`  | OpenTelemetry span identifier  |

Enable trace export with the `--otlp-endpoint` flag on both processes.

## Security

### mTLS Requirements

All connections require mutual TLS authentication:

1. Gateway presents client certificate signed by Root CA
2. Core verifies Gateway certificate against Root CA
3. Gateway verifies Core certificate contains expected `game_id` in SAN

### Certificate Validation

| Validator | Checks                                         |
| --------- | ---------------------------------------------- |
| Gateway   | Core cert signed by Root CA, SAN has `game_id` |
| Core      | Gateway cert signed by Root CA                 |

## Connection Lifecycle

### Gateway Startup

```text
1. Load TLS certificates from $XDG_CONFIG_HOME/holomush/certs/
2. Read game_id from $XDG_CONFIG_HOME/holomush/gateway.yaml
3. Connect to Core at --core-addr (default: localhost:9000)
4. Verify Core certificate SAN contains expected game_id
5. Begin accepting client connections
```

### Reconnection

Gateway automatically reconnects to Core when:

- Core restarts
- Network interruption occurs
- gRPC connection drops

During reconnection:

- Client connections remain open
- Commands queue until Core is available
- Subscribe streams automatically re-establish

## Proto File Location

The complete proto definition is at:

```text
api/proto/holomush/core/v1/core.proto
```

Generated Go code is in:

```text
internal/proto/holomush/core/v1/
```
