---
title: "Authentication System"
---

This document describes the HoloMUSH authentication architecture for contributors.

## Overview

HoloMUSH uses a three-layer authentication architecture:

```mermaid
flowchart TB
    subgraph Protocol["Protocol Layer"]
        GH[GatewayHandler]
        WH["WebHandler (future)"]
    end

    subgraph gRPC["gRPC Boundary"]
        CC[CoreClient]
    end

    subgraph Core["Core Server"]
        AS[AuthService]
        CS[CharacterService]
        PRS[PasswordResetService]
    end

    subgraph Repo["Repository Layer"]
        PR[PlayerRepository]
        WSR[WebSessionRepository]
        PRR[PasswordResetRepository]
    end

    GH --> CC
    WH --> CC
    CC --> AS
    CC --> CS
    AS --> PR
    AS --> WSR
    CS --> PR
    PRS --> PR
    PRS --> PRR
```

## Service Responsibilities

### AuthService

Handles core authentication operations.

| Method            | Purpose                             | Source                              |
| ----------------- | ----------------------------------- | ----------------------------------- |
| `Login`           | Authenticate player, create session | `internal/auth/auth_service.go:81`  |
| `Logout`          | Invalidate session                  | `internal/auth/auth_service.go:197` |
| `ValidateSession` | Verify token, update last-seen      | `internal/auth/auth_service.go:215` |
| `SelectCharacter` | Bind character to session           | `internal/auth/auth_service.go:254` |

### CharacterService

Manages character creation with validation.

| Method                    | Purpose                          | Source                                  |
| ------------------------- | -------------------------------- | --------------------------------------- |
| `Create`                  | Create character (default limit) | `internal/auth/character_service.go:56` |
| `CreateWithMaxCharacters` | Create with custom limit         | `internal/auth/character_service.go:61` |

Character creation validates:

- Name format (2-32 chars, letters and spaces, normalized to Initial Caps)
- Name uniqueness (case-insensitive)
- Player character limit (default: 5)
- Starting location assignment

### PasswordResetService

Handles password reset flow.

| Method          | Purpose                              | Source                               |
| --------------- | ------------------------------------ | ------------------------------------ |
| `RequestReset`  | Generate reset token for email       | `internal/auth/reset_service.go:80`  |
| `ValidateToken` | Verify token validity and expiration | `internal/auth/reset_service.go:118` |
| `ResetPassword` | Update password, invalidate tokens   | `internal/auth/reset_service.go:148` |

## Security Implementation

### Timing Attack Prevention

The auth service uses a dummy hash for non-existent users to prevent username enumeration:

```text
Login(username, password):
  1. Look up user by username
  2. If not found -> use dummy hash (still verify to maintain constant time)
  3. Verify password against hash (real or dummy)
  4. Return same error for "user not found" and "wrong password"
```

See `internal/auth/auth_service.go:67-76` for the dummy hash constant and `internal/auth/auth_service.go:81-130`
for the timing-safe implementation.

### Password Hashing

Passwords are hashed using argon2id with OWASP-recommended parameters:

| Parameter   | Value             |
| ----------- | ----------------- |
| Time        | 1 iteration       |
| Memory      | 64 MB             |
| Parallelism | 4 threads         |
| Salt        | 16 bytes (random) |
| Output      | 32 bytes          |

Implementation: `internal/auth/hasher.go:30-36`

### Constant-Time Token Comparison

Session token verification uses `crypto/subtle.ConstantTimeCompare` to prevent
timing attacks that could leak token prefixes.

See `internal/auth/session.go:115-125` for the implementation with security comments.

### Session Token Design

Sessions use opaque random tokens rather than signed JWTs. Key design choices:

| Aspect         | Implementation                 |
| -------------- | ------------------------------ |
| Token size     | 32 bytes (256 bits of entropy) |
| Storage        | SHA256 hash in database        |
| Expiry         | TTL after detach (per-role, default 24h) |
| Revocation     | Instant (delete database row)  |
| Character bind | Mutable (update session row)   |

See [ADR 0001](https://github.com/holomush/holomush/blob/main/docs/adr/0001-opaque-session-tokens.md) for the rationale behind
choosing opaque tokens over signed JWTs.

## Authentication Flows

### Telnet Flow

```mermaid
sequenceDiagram
    participant C as Client
    participant GH as GatewayHandler
    participant CC as CoreClient (gRPC)
    participant A as AuthService

    C->>GH: TCP connect
    GH->>C: Welcome banner

    C->>GH: connect <user> <pass>
    GH->>CC: AuthenticatePlayer(username, password)
    CC->>A: AuthenticatePlayer
    A->>CC: Session + Characters
    CC->>GH: Session + Characters
    GH->>C: Character list

    alt Has characters
        C->>GH: play <charname>
        GH->>CC: SelectCharacter(sessionID, charID)
        CC->>A: SelectCharacter
        GH->>C: Enter world
    else No characters
        C->>GH: create <name>
        GH->>CC: CreateCharacter(playerID, name)
        CC->>A: CreateCharacter
        GH->>C: Enter world
    end
```

### Key Protocol Handler Methods

The telnet handler calls through the gRPC `CoreClient` interface, not service methods directly:

| Command   | Handler Method  | gRPC RPC             | Source                               |
| --------- | --------------- | -------------------- | ------------------------------------ |
| `connect` | `handleConnect` | `AuthenticatePlayer` | `internal/telnet/gateway_handler.go` |
| `create`  | `handleCreate`  | `CreateCharacter`    | `internal/telnet/gateway_handler.go` |
| `play`    | `handlePlay`    | `SelectCharacter`    | `internal/telnet/gateway_handler.go` |
| `quit`    | `handleQuit`    | `Logout`             | `internal/telnet/gateway_handler.go` |

## Repository Interfaces

### PlayerRepository

```text
internal/auth/player.go:128-151
```

Manages player account persistence including username lookups, email lookups,
and password updates.

### WebSessionRepository

```text
internal/auth/session.go:127-156
```

Manages session persistence including token hash lookups, last-seen updates,
character binding, and expiration cleanup.

### PasswordResetRepository

```text
internal/auth/reset.go
```

Manages reset token persistence with token hash lookups and player-based cleanup.

## Related Documentation

- [Design Spec](https://github.com/holomush/holomush/blob/main/docs/specs/2026-01-25-auth-identity-design.md) - Full authentication design
- [ADR 0001](https://github.com/holomush/holomush/blob/main/docs/adr/0001-opaque-session-tokens.md) - Token design decision
- [Operator Guide](/operating/authentication/) - Deployment and configuration
