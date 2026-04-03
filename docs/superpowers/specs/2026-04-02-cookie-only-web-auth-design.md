# Cookie-Only Web Auth Design

> Move player session tokens to HttpOnly cookie-only authentication for web RPCs.

**Status:** Draft
**Date:** 2026-04-02
**Issue:** holomush-qhkw

## Problem

The durable 24-hour player session token is exposed in two ways that expand XSS
blast radius:

1. **Proto response bodies** — `WebAuthenticatePlayerResponse` and
   `WebCreatePlayerResponse` return `player_session_token` in the response body,
   visible to any JavaScript running in the page.
2. **sessionStorage** — The SvelteKit client stores the raw token in
   `sessionStorage` under `holomush-player`, readable by any script in the same
   origin.

The HttpOnly cookie (`holomush_session`) already carries the token via
`CookieMiddleware`, making the body/storage exposure unnecessary.

## Solution

Remove `player_session_token` from all web-facing proto messages. Web RPCs
MUST authenticate exclusively via the HttpOnly cookie, which `CookieMiddleware`
injects into the `X-Session-Token` request header. The SvelteKit client MUST
NOT store or send the raw token. A new `WebCheckSession` RPC validates the
cookie on page reload so the client knows whether the player is authenticated
without client-side guessing.

## Design

### Proto Changes

#### web.proto — Field Removal

Remove `player_session_token` from these messages. Clean removal — no
`reserved` lines since there are no released versions:

| Message                          | Field removed              | Number |
| -------------------------------- | -------------------------- | ------ |
| `WebAuthenticatePlayerResponse`  | `player_session_token`     | 2      |
| `WebSelectCharacterRequest`      | `player_session_token`     | 1      |
| `WebCreatePlayerResponse`        | `player_session_token`     | 2      |
| `WebCreateCharacterRequest`      | `player_session_token`     | 1      |
| `WebListCharactersRequest`       | `player_session_token`     | 1      |
| `WebLogoutRequest`               | `player_session_token`     | 1      |

Remaining fields in each message MUST keep their existing field numbers. When
a removed field was number 1, subsequent fields MUST NOT be renumbered.

#### web.proto — New RPC

```protobuf
rpc WebCheckSession(WebCheckSessionRequest) returns (WebCheckSessionResponse);

message WebCheckSessionRequest {}

message WebCheckSessionResponse {
  string player_name = 1;
}
```

No `valid` field — authentication failure returns a ConnectRPC
`CodeUnauthenticated` error. Success returns the player name for display.

The request is empty. The cookie provides the credential via the
`X-Session-Token` header injected by `CookieMiddleware`.

#### core.proto — New RPC

```protobuf
rpc CheckPlayerSession(CheckPlayerSessionRequest) returns (CheckPlayerSessionResponse);

message CheckPlayerSessionRequest {
  string player_session_token = 1;
}

message CheckPlayerSessionResponse {
  string player_name = 1;
}
```

The core RPC accepts the token in the request body, consistent with other core
RPCs (used by telnet and internal callers). The web gateway reads the token from
the header and passes it here.

core.proto is unchanged otherwise — all existing `player_session_token` fields
in core messages remain (they serve telnet and internal flows).

### Server-Side Changes

#### Token Extraction Helper (`internal/web/auth_handlers.go`)

A shared helper extracts the player token from the request header:

```go
func playerTokenFromHeader(h http.Header) (string, error) {
    token := h.Get(headerInjectSessionToken)
    if token == "" {
        return "", connect.NewError(connect.CodeUnauthenticated,
            fmt.Errorf("no player session"))
    }
    return token, nil
}
```

All authenticated web handlers call this instead of reading from the proto
message body. This provides:

- One extraction point with one error shape.
- Gateway-level validation: missing cookie returns `CodeUnauthenticated` before
  any core RPC fires.
- Prevents the empty-string-hash bug where `resolvePlayerSession("")` produces
  a confusing domain error.

#### Web Handler Changes (`internal/web/auth_handlers.go`)

**Handlers reading token from message body → read from header:**

- `WebSelectCharacter` — `playerTokenFromHeader(req.Header())` replaces
  `req.Msg.GetPlayerSessionToken()`. Token forwarded to core
  `SelectCharacter` RPC in the request body (unchanged).
- `WebCreateCharacter` — same pattern.
- `WebListCharacters` — same pattern.
- `WebLogout` — same pattern.

**Handlers setting token in response body → stop:**

- `WebAuthenticatePlayer` — removes `PlayerSessionToken` from the response
  struct. The `X-Set-Session-Token` header (→ `Set-Cookie` via
  `CookieMiddleware`) remains the only delivery mechanism.
- `WebCreatePlayer` — same change.

**New handler:**

- `WebCheckSession` — reads token from header via `playerTokenFromHeader`,
  calls core `CheckPlayerSession` RPC, returns `{player_name}` on success or
  `CodeUnauthenticated` on failure.

#### Core Handler Changes (`internal/grpc/auth_handlers.go`)

New `CheckPlayerSession` method on `CoreServer`:

- Calls existing `resolvePlayerSession(ctx, rawToken)` to validate the token
  and refresh its TTL.
- Looks up the player name via `playerRepo.GetByID(ctx, session.PlayerID)`.
- Returns `CheckPlayerSessionResponse{PlayerName: player.Username}`.
- Returns an appropriate error if the session is invalid or expired.

No new domain logic. The handler composes two existing operations.

#### gRPC Client (`internal/grpc/client.go`)

Add a `CheckPlayerSession` method to the gRPC client, following the existing
pattern used by `AuthenticatePlayer`, `SelectCharacter`, etc. The web gateway
calls this method to reach the core server.

### Client-Side Changes

#### Auth Store (`web/src/lib/stores/authStore.ts`)

**Remove `playerSessionToken` from state:**

```typescript
interface AuthState {
  isPlayerAuthenticated: boolean;
  playerName: string | null;
  sessionId: string | null;
  characterName: string | null;
  isGuest: boolean;
}
```

`isPlayerAuthenticated` is an explicit boolean, not derived from the presence
of any other field.

**`isAuthenticated` derivation:**

```typescript
export const isAuthenticated = derived(
  authState,
  ($s) => $s.isPlayerAuthenticated || !!$s.sessionId
);
```

**Remove `holomush-player` from sessionStorage.** `WebCheckSession` provides
`playerName` on page reload. `sessionStorage` only stores `holomush-session`
(`sessionId` + `characterName`) for game session continuity.

**Updated functions:**

- `setPlayerAuth(playerName: string)` — sets `isPlayerAuthenticated: true` and
  `playerName`. No token argument.
- `clearAuth()` — resets all fields and removes `holomush-session` from
  sessionStorage.
- `restoreSession()` — restores only `sessionId` + `characterName` from
  `holomush-session`. MUST NOT restore player auth from sessionStorage (the
  `holomush-player` key is removed entirely).

#### Auth Guard (`web/src/routes/(authed)/+layout.ts`)

The layout load function becomes async and validates player auth server-side:

```typescript
export async function load() {
  // Restore game session from sessionStorage (no server call).
  restoreSession();

  // Validate player auth via server.
  const client = createClient(WebService, transport);
  try {
    const resp = await client.webCheckSession({});
    setPlayerAuth(resp.playerName);
  } catch {
    redirect(302, '/login');
  }
}
```

The server is the single source of truth. No client-side auth guessing.

#### Page Changes

**Login (`web/src/routes/login/+page.svelte`):**

- `handleLogin` stops reading `resp.playerSessionToken`. Calls
  `setPlayerAuth(username)` on success (cookie is set by the browser from the
  `Set-Cookie` response header).
- `webSelectCharacter` call removes `playerSessionToken` from the request body.

**Register (`web/src/routes/register/+page.svelte`):**

- Stops reading `resp.playerSessionToken`. Calls `setPlayerAuth(username)` on
  success.

**Characters (`web/src/routes/(authed)/characters/+page.svelte`):**

- Auth guard changes from `$authState.playerSessionToken` to
  `$authState.isPlayerAuthenticated`.
- All RPC calls (`webListCharacters`, `webSelectCharacter`,
  `webCreateCharacter`) remove `playerSessionToken` from request bodies. The
  cookie carries auth implicitly.

**TopBar (`web/src/lib/components/TopBar.svelte`):**

- Conditional rendering replaces `$authState.playerSessionToken` with
  `$authState.isPlayerAuthenticated`.
- `webLogout` call removes `playerSessionToken` from the request body.

### Testing

**Unit tests (`internal/grpc/auth_handlers_test.go`):**

- Add tests for `CheckPlayerSession`: valid session, expired session, repo not
  configured.
- Existing tests for `AuthenticatePlayer`, `SelectCharacter`, etc. are
  unchanged — they test core server handlers that still accept
  `player_session_token` in the request body.

**Unit tests (`internal/web/auth_handlers_test.go`):**

- Update web handler tests to set `X-Session-Token` on the request header
  instead of `player_session_token` in the message body.
- Add tests for `WebCheckSession`: valid cookie, missing cookie, expired
  session.
- Add tests for `playerTokenFromHeader`: present, empty, missing.

**E2E tests:**

- Update login/register/character flows to verify the token is NOT in response
  bodies.
- Add page-reload test: login → reload → verify still authenticated via
  `WebCheckSession`.

### Out of Scope

- **`sessionId` in sessionStorage** — The game session ID is still stored
  client-side and functions as a bearer token for game commands. This is a
  separate, lower-blast-radius concern (single character, shorter lifetime).
  Track as a follow-up issue if warranted.
- **core.proto changes** — All `player_session_token` fields in core proto
  messages remain. They serve telnet and internal flows that have no cookie
  mechanism.
- **CSRF protection** — The HttpOnly `SameSite=Strict` cookie provides
  baseline CSRF mitigation. Full CSRF token support is a separate concern.
