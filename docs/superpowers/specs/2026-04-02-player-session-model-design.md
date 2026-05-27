<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Player Session Model Design

**Date**: 2026-04-02
**Status**: Draft
**Author**: Claude Opus 4.6 + seanb4t

## Problem

The current auth model uses a `PlayerToken` that is consumed on character
selection (one-time use, 5-minute TTL). This breaks character switching:
after entering the game, `ListCharacters` cannot authenticate the player
because the token was deleted. The `WebSession` table exists but is
underused and disconnected from the token flow.

Three requirements drive this redesign:

1. A **player** logs in, not a character — player identity MUST persist
   across character selection.
2. A player MUST be able to switch between characters without
   re-authenticating.
3. A player MUST be able to be logged in from multiple devices/protocols
   simultaneously, with multiple characters active at once.

## Design

### Entity Hierarchy

```text
Player (permanent)
├── PlayerSession A (browser, 24h sliding TTL)
│   ├── Connection → Character Session "Char One"
│   └── Connection → Character Session "Char Three"
├── PlayerSession B (telnet, 24h sliding TTL)
│   └── Connection → Character Session "Char Two"
└── PlayerSession C (telnet, 24h sliding TTL)
    └── Connection → Character Session "Char One" (shared with A)
```

| Entity | Identity | Lifetime | Multiplicity |
|--------|----------|----------|--------------|
| Player | ULID in `players` | Permanent | One per human |
| PlayerSession | ULID + opaque token | 24h sliding TTL | Many per player (one per login) |
| CharacterSession | ULID in `sessions` | Until disconnect/detach | One per character (shared across connections) |
| Connection | Ephemeral | Until socket closes | Many per character session |

Two connections to the same character (e.g., browser + telnet both playing
"Char One") MUST share one character session — same screen, same activity.

### PlayerSession

Replaces both `PlayerToken` and `WebSession`. Single table, single domain
type, single repository.

**Fields:**

| Column | Type | Description |
|--------|------|-------------|
| `id` | ULID (text PK) | Session identifier |
| `player_id` | ULID (text FK) | Owning player |
| `token_hash` | text | SHA-256 of the opaque token |
| `user_agent` | text | Client identifier |
| `ip_address` | text | Origin IP |
| `expires_at` | timestamptz | Sliding expiry (refreshed on use) |
| `created_at` | timestamptz | Creation time |
| `updated_at` | timestamptz | Last activity |

**Token storage:** The raw token is returned to the client on creation.
The server stores only the SHA-256 hash. Lookup is by hash, not raw token.

**Sliding TTL:** Every authenticated RPC that uses the player session
token MUST refresh `expires_at` to `now() + 24h` and update `updated_at`.

### Auth Flow (Both Protocols)

```text
1. Login/Register
   Client sends credentials
   → Core validates, creates PlayerSession (24h sliding TTL)
   → Returns player_session_token + character list

2. List Characters
   Client sends player_session_token
   → Core looks up PlayerSession by token hash
   → Validates not expired, refreshes TTL
   → Returns characters for that player

3. Select Character
   Client sends player_session_token + character_id
   → Core validates PlayerSession (same as above)
   → Creates or reattaches to character session
   → Returns session_id + character_name
   → Does NOT consume the token

4. Switch Character
   Client sends player_session_token (same token, still valid)
   → Core lists characters (step 2)
   → Client selects different character (step 3)

5. Logout
   Client sends player_session_token
   → Core deletes the PlayerSession
   → Character sessions for connections through this session detach
   → Other player sessions for the same player are unaffected
```

### Proto Changes

Rename `player_token` to `player_session_token` in all messages that
carry it:

- `AuthenticatePlayerResponse.player_session_token`
- `CreatePlayerResponse.player_session_token`
- `ListCharactersRequest.player_session_token`
- `SelectCharacterRequest.player_session_token`
- `CreateCharacterRequest.player_session_token`

`LogoutRequest` changes from `session_id` (game session) to
`player_session_token` (player session). Logout ends the player session,
not a specific game session. For web clients, the cookie middleware
injects the token from the HttpOnly cookie into the request header, so
the client does not need to send the raw token explicitly — the cookie
handles it.

### Web Protocol Details

**Login/Register:** RPC returns `player_session_token`. The web handler
sets the `X-Set-Session-Token` header so the cookie middleware writes an
HttpOnly cookie. The client also stores the token in
`authState.playerSessionToken` via `sessionStorage`.

**Character picker:** `WebListCharacters` sends `player_session_token`.
Core resolves the player, returns characters. Works every time because the
token is not consumed.

**Character switch:** Navigate to `/characters`. Token is still valid.
List populates. Select a different character.

**Page reload:** `restoreSession()` restores from `sessionStorage`. Cookie
middleware injects the token from the HttpOnly cookie as a fallback via
`X-Session-Token` header.

**Logout:** Invalidates the PlayerSession. Client calls `clearAuth()`.
Cookie cleared.

### Telnet Protocol Details

**`connect player1 password`:** Calls `AuthenticatePlayer`. Gets
`player_session_token`, stored on the `GatewayHandler` struct. Shows
character list if multiple characters exist.

**`PLAY charname`:** Calls `SelectCharacter` with `player_session_token`.
Attaches to character session. Token remains on handler struct — not
cleared.

**Disconnect:** TCP closes. Character session detaches. PlayerSession
survives in the database (24h sliding TTL). Irrelevant for telnet today
since there is no reconnect flow, but the data model is clean.

**Future:** A `SWITCH` command could re-enter select mode using the same
`player_session_token`. Out of scope for this design.

### Web UI Navigation Changes

The current terminal page has several behaviors that assume player
identity is tied to the character session. These MUST change.

**`disconnect()` function (terminal page):** Currently calls `clearAuth()`
(wipes player session token) and navigates to `/`. MUST instead clear
only the character session state (`sessionId`, `characterName`) and
navigate to `/characters`. The player remains authenticated.

**`STREAM_CLOSED` handler (terminal page):** Currently calls `clearAuth()`
when the server sends `STREAM_CLOSED` (e.g., character quit via command).
MUST instead clear only the character session state and navigate to
`/characters`. The player remains authenticated.

**`onDestroy` (terminal page):** Fires `client.disconnect()` when the
component unmounts during SPA navigation (e.g., clicking character
switcher). This is correct behavior — it removes the web connection from
the character session. If it was the last connection, the character
session detaches. No change needed here, but it MUST NOT clear the
player session token.

**New `clearCharacterSession` function:** `authStore.ts` needs a function
that clears `sessionId` and `characterName` without touching
`playerSessionToken` or `playerName`. The existing `clearAuth()` remains
for full logout (player logout button).

**Character switcher button:** Currently just navigates to `/characters`
(which triggers `onDestroy` → connection drop → detach). This is the
desired behavior for switching — the character detaches and can be
reattached later. No change needed.

**Logout button (TopBar):** Currently calls `handleLogout()` which
invokes `webLogout` with the game session ID. MUST instead send the
player session token to the new Logout RPC, clear all auth state, and
navigate to `/`.

### Telnet Navigation Changes

**QUIT in selectMode (character picker):** Currently closes the TCP
connection. MUST instead delete the player session (Logout RPC) and
then close the TCP connection. This is a full player logout.

**QUIT when playing a character:** Currently sends the `quit` command
to the server, waits for `STREAM_CLOSED`, then closes the TCP
connection. MUST instead send the `quit` command, wait for
`STREAM_CLOSED`, then return to selectMode (character picker) instead
of closing the connection. The player session remains valid on the
handler struct.

**New LOGOUT command:** Add a `LOGOUT` command available in both
selectMode and while playing. This deletes the player session and closes
the TCP connection. Distinct from QUIT which only ends the character
session.

### Client Store Changes (SvelteKit)

`authStore.ts` renames `playerToken` to `playerSessionToken`. The
`sessionStorage` keys update accordingly. The `isAuthenticated` derived
store checks `playerSessionToken` instead of `playerToken`.

`setCharacterSession` no longer implies the player token is consumed.
The player session token and character session ID coexist in state.

### Character Session Lifecycle

Character sessions have their own lifecycle that is independent of but
influenced by player sessions. The key principle: **quit is intentional
departure from the world; disconnect is loss of transport.**

**States:**

| Status | Meaning |
|--------|---------|
| `active` | Character is in the world with at least one connection |
| `detached` | Character lost all connections but may reattach (30min TTL) |
| `expired` | Detach TTL elapsed, session reaped |

**Transitions:**

```text
Character quit (QUIT command / web "leave game"):
  Character session → deleted (leave event emitted)
  Player session → unchanged (player stays logged in)

Connection drops (browser close, TCP death):
  Connection removed from character session
  Last connection gone → character session detaches (30min TTL)
  Player session → unchanged

Player logout (explicit):
  Character sessions via this player session → detach
  Player session → deleted

Player session expires (24h inactivity):
  Character sessions via this player session → detach
  Player session → reaped

Character session detach TTL expires (30min):
  Character session reaped (leave event emitted)
  Independent of player session state
```

**Multi-connection behavior:** When a character has connections from
multiple player sessions (e.g., browser + telnet both playing "Char
One"), losing one connection does not detach the session — only losing
ALL connections triggers detach. Similarly, one player session logging
out only detaches character sessions that have no remaining connections
from other player sessions.

### Error Handling

`ListCharacters` with an invalid or expired token MUST return an error,
not an empty list. The current silent-empty behavior is a bug. The web
handler MUST propagate the error to the client so the UI can show
"Session expired — please log in again" instead of an empty character
picker.

All RPCs that accept `player_session_token` MUST return a clear error
code when the session is invalid or expired:

| Condition | Error Code |
|-----------|-----------|
| Token not found | `PLAYER_SESSION_NOT_FOUND` |
| Token expired | `PLAYER_SESSION_EXPIRED` |
| Player account locked | `AUTH_ACCOUNT_LOCKED` |

## What Gets Removed

- `player_tokens` table and `PlayerToken` domain type
- `PlayerTokenRepository` interface and Postgres implementation
- `web_sessions` table and `WebSession` domain type
- `WebSessionRepository` interface and Postgres implementation
- One-time-use deletion logic in `SelectCharacter`
  (`auth_handlers.go:208,263`)
- 5-minute TTL concept

This is a clean break. No backward compatibility, no migration of
existing tokens or sessions.

## What Gets Added

- `player_sessions` table (migration)
- `player_session_id` column on `connections` table (migration) — tracks
  which player session owns each connection, enabling clean logout
- `PlayerSession` domain type in `internal/auth`
- `PlayerSessionRepository` interface in `internal/auth`
- `PostgresPlayerSessionStore` in `internal/store`
- Updated `AuthService` methods for player session lifecycle
- TTL refresh logic on every authenticated RPC

## What Stays Unchanged

- Character session (`sessions` table) — no changes
- Guest flow — guests skip player auth entirely
- Cookie middleware — same mechanism, stores player session token
- Password hashing, lockout, reset — all unchanged
- ABAC / access control — operates on character identity, not player
  session

## Testing

### Unit Tests

**`internal/auth` — PlayerSession domain:**

- Create session with valid player ID succeeds
- Token generation produces unique, sufficient-entropy values
- `IsExpired()` before and after TTL
- Sliding TTL refresh updates `expires_at`

**`internal/store` — PlayerSession repository:**

- Create, GetByTokenHash, Delete, DeleteByPlayer CRUD
- GetByTokenHash with expired token returns error
- DeleteExpired bulk cleanup removes stale rows
- Concurrent access (parallel creates for same player)

**`internal/grpc` — Core server handlers:**

- `AuthenticatePlayer` creates PlayerSession, returns token
- `CreatePlayer` creates PlayerSession, returns token
- `ListCharacters` with valid session returns characters
- `ListCharacters` with expired session returns
  `PLAYER_SESSION_EXPIRED` error
- `ListCharacters` with invalid token returns
  `PLAYER_SESSION_NOT_FOUND` error
- `SelectCharacter` with valid session creates character session,
  token NOT deleted
- `SelectCharacter` called twice with same token works (character
  switch)
- `Logout` deletes PlayerSession
- Every authenticated RPC refreshes session TTL

**`internal/web` — Web gateway handlers:**

- `WebListCharacters` propagates errors (not silent empty)
- Cookie middleware sets cookie on login
- Cookie middleware clears cookie on logout
- Cookie middleware injects token from cookie on inbound requests

**`internal/telnet` — Telnet handler:**

- `connect` with valid credentials enters select mode, token persisted
  on handler struct
- `PLAY` selects character, token still available on handler
- Token survives across `PLAY` call (not cleared)
- `QUIT` while playing returns to selectMode, does not close connection
- `QUIT` in selectMode sends Logout RPC and closes connection
- `LOGOUT` while playing sends Logout RPC and closes connection
- `LOGOUT` in selectMode sends Logout RPC and closes connection

### Concurrency Tests

- Two connections simultaneously reattach to same detached character
  session — only one succeeds via `ReattachCAS`, other creates new
  session or retries
- Rapid RPCs on same player session — TTL refresh does not corrupt
  `expires_at`
- Player logout while character session has connections from two
  player sessions — only connections from the logged-out session are
  removed; character session stays active via remaining connections
- Telnet QUIT-to-selectMode transition: no stale events from old
  character session leak into selectMode

### Negative / Security Tests

- Expired player session token rejected with clear error
- Random/tampered token rejected
- Token from player A cannot access player B's characters
- Deleted player session causes all RPCs with that token to fail
- Replay of a deleted session's token rejected
- Concurrent logins from same player both valid independently
- Logout from device A does not affect device B

### Integration Tests (testcontainers + Ginkgo)

**Multi-session scenarios:**

- Player logs in from two separate sessions, both list characters
  independently
- Player selects Char One on session A, Char Two on session B — both
  character sessions active simultaneously
- Logout on session A invalidates session A's token; session B
  unaffected
- Player session TTL expires — character sessions detach

**Character sharing:**

- Session A and session B both select Char One — same character session
  ID returned
- Command sent from one connection visible on the other's subscription

**Character session lifecycle:**

- Character quit (QUIT command) deletes character session; player
  session remains valid; player can list and select characters again
- Connection drop (no quit) detaches character session with 30min TTL;
  player session remains valid
- Reconnect within detach TTL reattaches to same character session
- Detach TTL expiry reaps character session (leave event emitted)
- Multi-connection to same character: one connection drops, character
  session stays active (not detached)
- Multi-player-session to same character: one player session logs out,
  character session stays active if connections from other player
  sessions remain
- Quit then switch: player quits Char One, selects Char Two — Char One
  deleted, Char Two session created, player session unchanged throughout

### E2E Tests (Playwright + Docker)

**Web flow:**

- Register → create character → enter game → character switcher →
  existing character visible → select → back in game
- Register → create two characters → switch between them repeatedly
- Login from two browser contexts → independent character selection
  and gameplay
- Logout from one context → other context still functional
- Page reload mid-session → session restored, character picker works
- Quit character (leave game) → lands on character picker → player
  still authenticated → can select or create another character
- Quit character → character no longer shows "Active" status in picker
- STREAM_CLOSED from server → player stays authenticated, lands on
  character picker (not login page)
- Logout button → full logout, lands on home page, player session
  deleted

**Multi-protocol (stretch):**

- Web login + telnet login as same player → both active
- Both select same character → shared character session

## Concurrency Considerations

### Telnet Handler (Single-Goroutine State)

The `GatewayHandler` struct carries mutable state (`playerSessionToken`,
`selectMode`, `sessionID`, `charName`, `authed`). Currently this state
is only mutated from the main select loop goroutine, which processes
one line at a time. This design MUST be preserved:

- The handler's main loop reads lines from a channel and processes them
  sequentially. No concurrent access to handler fields.
- gRPC calls (AuthenticatePlayer, SelectCharacter, etc.) block the
  main loop — this is fine because telnet is inherently sequential.
- The event stream goroutine only writes to `eventRecv` channel; it
  does NOT read or write handler fields.

**New risk with QUIT-returns-to-picker:** When QUIT sends the quit
command and waits for `STREAM_CLOSED`, the handler transitions back to
selectMode. This transition happens in the same goroutine that processes
the `STREAM_CLOSED` frame, so no race. However, the implementation MUST
ensure the event subscription is properly closed before re-entering
selectMode, otherwise stale events from the old character session could
leak into the new state.

**Mitigation:** After receiving `STREAM_CLOSED`, nil out `eventRecv`,
reset `sessionID`/`charName`/`authed`, set `selectMode = true`, refresh
the character list via `ListCharacters`, then resume the select loop.
All mutations happen in the same goroutine.

### Character Session Reattach Race

When two connections simultaneously try to reattach to the same detached
character session, `ReattachCAS` provides atomic compare-and-swap — only
one wins. The loser sees `false` and creates a new session instead.
This existing mechanism is sufficient and unchanged by this design.

### Player Session TTL Refresh Race

Multiple RPCs from the same player session (e.g., rapid commands) may
try to refresh `expires_at` concurrently. This is benign — all set
`expires_at = now() + 24h` and the last write wins. The worst case is
the TTL is a few milliseconds shorter than expected. No mutex needed;
a simple `UPDATE ... SET expires_at = $1, updated_at = $2 WHERE id = $3`
is sufficient.

### Player Logout vs Active Character Sessions

When a player session is deleted (logout), any active character sessions
that have connections from this player session need to detach. This
requires enumerating connections by player session, which the current
schema does not track. Two options:

**Option A (recommended):** Add `player_session_id` to the `connections`
table. On player logout, query connections by player session ID, remove
them, then check if any character sessions lost their last connection
and need to detach. This is a multi-step operation but each step is
idempotent.

**Option B:** Don't track the association. On player logout, rely on the
connection's transport closing (browser navigates away, TCP drops) to
trigger normal disconnect flow. This is simpler but means character
sessions may stay "active" briefly until the transport layer notices.

Option A is more correct. The `connections` table SHOULD include a
`player_session_id` column.

### Disconnect Race (Existing)

The existing race in `Disconnect` (RemoveConnection then CountConnections
as separate operations) is documented in `server.go:881-885`. This
design does not worsen it. The existing TODO to implement
`RemoveConnectionAndCount` as a single transactional operation remains
valid and SHOULD be addressed.

## Security Considerations

- Raw tokens MUST NOT be stored server-side. Store SHA-256 hash only.
- Tokens MUST be generated with `crypto/rand` (32 bytes, hex-encoded).
- The HttpOnly cookie MUST use `SameSite=Strict` (or `Lax` in dev) and
  `Secure` in production.
- Expired session cleanup SHOULD run periodically (existing pattern
  with `DeleteExpired`).
- Rate limiting on `AuthenticatePlayer` and `CreatePlayer` remains
  unchanged.
- Account lockout logic remains unchanged.
