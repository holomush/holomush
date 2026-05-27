<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Multi-Tab Session Isolation — Design Spec

**Status:** Draft (v3 — addresses design-reviewer v2 findings 2026-04-25)
**Date:** 2026-04-25
**Tracker:** holomush-9q8n
**Author:** sean@fzymgc.email (with Claude Opus 4.7)

## 1. Background

### 1.1 The bug

Reproduction (verified 2026-04-25 in cmux automation against `local-dev` from
`main`):

1. Tab 1: open `http://localhost:8080`, click **Try as Guest** → connects as
   guest player A (`Jasper Iodine`). `say` and `:pose` work, character is
   "live".
2. Tab 2 in the same browser: open `http://localhost:8080`, click
   **Try as Guest** → connects as guest player B (`Moonstone Zircon`). Tab 2
   sees its own arrival and can send messages.
3. Switch back to Tab 1. The header (character name, location, badges) renders
   visually dimmed. `say` and `:pose` produce no echo and no broadcast,
   though Tab 1 still receives streamed events from the live Subscribe.

Server logs (`docker logs local-dev-core-1`):

```text
level=WARN msg="session ownership mismatch"
  player_id=01KQ2Y7AQGTEDH6TGNPFSSDB19      (player B, from cookie)
  session_id=01KQ2Y5ETW03KJ0HKCQ07ASYF2     (Tab 1's stored session)
  session_owner=01KQ2Y5ETK5957724MGZ2H2TDB  (player A, who actually owns the session)
```

### 1.2 Root cause

| Layer | Storage | Scope |
| --- | --- | --- |
| `holomush_session` cookie holds `player_session_token` (`internal/web/cookie.go:13`) | HTTP cookie, `HttpOnly`, `Path=/` | **Shared across all tabs in the browser** |
| `holomush-session` sessionStorage holds `{sessionId, characterName}` (`web/src/lib/stores/authStore.ts:34`) | `window.sessionStorage` | **Per-tab** |

When Tab 2 calls `webCreateGuest`, the server mints a new player + new
PlayerSession and overwrites the shared cookie. Tab 1's cookie now sends
player B's token; Tab 1's sessionStorage still references session A (owned
by player A); every command from Tab 1 fails the
`ValidateSessionOwnership` gate (`internal/auth/session_ownership.go:75`)
and is rejected with `SESSION_NOT_FOUND`.

The validator is correct and MUST stay. The fix lives in the auth-flow UX
and the `WebCreateGuest`/`AuthenticatePlayer` server contracts.

### 1.3 What the platform already supports

The architecture already supports the model we want; the bug is purely a
UX gap.

- `auth_service.go:154` — `AuthenticatePlayer` mints **a new PlayerSession
  per login**, capped by `maxSessionsPerPlayer`. One player owning multiple
  PlayerSessions is by-design.
- `auth_handlers.go:218-241` (`SelectCharacter`) — **reattach is the default**
  when a session already exists for a (player, character) pair. Two tabs
  selecting the same character land on the same `session_id`.
- `server.go:683-716` (`Subscribe`) — sessions take a `connection_id` per
  caller and register N concurrent connections via
  `sessionStore.AddConnection`. Each connection gets its own event stream
  copy and can submit commands.
- Telnet (`internal/telnet/`) authenticates per TCP connection. Two telnet
  windows authenticating the same player produce two PlayerSessions, both
  reattach to the same character session, both attach as connections, both
  see and send. **Telnet already works.** No telnet code changes needed
  for this fix.

## 2. Goals

1. Two browser tabs in the same browser, opened back-to-back, MUST NOT
   silently break each other.
2. A user MUST be able to operate two characters of the same player
   concurrently — one per tab — without auth interference.
3. A user MUST be able to operate the same character in two tabs (or one
   tab + one telnet, etc.) and have both attached to the same game
   session, both rendering the same scrollback, both able to send commands.
4. A user attempting to "Try as Guest" while already authenticated MUST get
   a clear, non-destructive UX: their existing session is preserved, the
   client tells them they are already signed in.
5. The fix MUST NOT increase XSS exposure. The `player_session_token`
   stays in the `HttpOnly` cookie.
6. Telnet behaviour MUST NOT regress.

## 3. Non-goals

- **Concurrent identities in one browser** (player A + player B, or guest
  - registered player, both alive at once). Out of scope; a user who
  needs this uses a private window, a different browser profile, or a
  different browser.
- **Cross-tab logout broadcast.** Other tabs detecting a peer's logout in
  real time is nice-to-have but not required for v1; SESSION_NOT_FOUND on
  the next RPC is sufficient and routes to login.
- **Subdomain partitioning** of cookies, refactoring auth to bearer
  tokens, refresh-token rotation, or any change to the credential
  storage model. Cookie stays as the only credential.
- Fixing the related issues `holomush-l2l1` (history leak on arrival) and
  `holomush-pzen` (movement double-render). Those have their own beads
  and may share a PR for triage convenience but are not blocked on this
  spec.

## 4. Design

### 4.1 Identity model

| Concept | Mapping |
| --- | --- |
| Browser instance | One `holomush_session` HttpOnly cookie ⇄ one PlayerSession ⇄ one player (registered or guest). |
| Tab | One sessionStorage `{sessionId, characterName}` entry ⇄ one game session. |
| Connection | Each Subscribe call MAY register a `connection_id`; the server stores the registered set via `sessionStore.AddConnection` (`internal/grpc/server.go:683-716`). The web client today does NOT send `connection_id` (verified 2026-04-25 via `rg connectionId web/src -g '!**/connect/**'` returning no call sites). The multi-tab/multi-client model below relies on `HandleCommand`'s ownership check (`ValidateSessionOwnership` → player_id match), NOT on `connection_id`. Telnet does send `connection_id` so its connection-presence telemetry is more accurate, but command submission and event delivery work for both. |
| Multiple characters of one player | Each tab calls `webSelectCharacter(charX)`. Server creates a new game session per distinct character, reattaches if a session exists. |
| Same character in two tabs / mixed clients | Both reattach to the same `session_id`. Both Subscribe (with or without `connection_id`). Both render the same scrollback, both can send commands — `HandleCommand` validates ownership at the player level, not the connection level (`internal/grpc/server.go` HandleCommand path; ownership via `ValidateSessionOwnership`). |

### 4.2 Server contract changes

#### 4.2.0 Gate location: gateway, not core

The `ALREADY_AUTHENTICATED` short-circuit lives in the **web gateway**
(`internal/web/auth_handlers.go`), not in the core RPC handlers. The
gateway already extracts the player_session_token from the cookie via
`playerTokenFromHeader`; it is the natural place to inspect "is there
already a valid session" before forwarding to core.

Decision rationale:

- Keeps the core proto stable. `CreateGuestRequest`, `AuthenticatePlayerRequest`,
  and `CreatePlayerRequest` do NOT gain a `player_session_token` field.
  The core RPCs retain their existing semantics: "given these credentials,
  mint a player+session." Telnet's `AuthenticatePlayer` calls (which never
  carry a cookie token) bypass the gate naturally — the gate doesn't run
  in telnet's call path at all.
- The gate's only job is preventing the cookie from being clobbered. The
  cookie only exists in the web/HTTP boundary, so the gate sensibly lives
  there.
- A future non-web client speaking gRPC directly to core can mint multiple
  PlayerSessions per player by design (the existing `maxSessionsPerPlayer`
  cap covers it). It SHOULD NOT be subject to a cookie-collision gate
  because it isn't competing for a cookie slot.

For all three guarded RPCs the gate logic is the same. The gate runs
inside the existing ConnectRPC handler, which sees headers (not
`*http.Request`), and reuses the cookie token already injected by
`CookieMiddleware` (`internal/web/cookie.go:77-87`):

```text
1. Read token: req.Header().Get(headerInjectSessionToken). Empty string
   if absent.
2. If non-empty, call h.client.CheckPlayerSession(rpcCtx,
   &corev1.CheckPlayerSessionRequest{PlayerSessionToken: token}).
3. If that call succeeds, the cookie is valid → short-circuit with
   success=false, error_code=ALREADY_AUTHENTICATED,
   current_player_name=resp.PlayerName. Cookie unchanged. Do NOT call
   the original core RPC.
4. If that call returns an Unauthenticated / SESSION_NOT_FOUND error,
   the cookie is invalid (absent / expired / unknown) → forward to
   the existing core RPC as today. Existing Set-Cookie behaviour applies.
5. If that call returns any other error (transport, lookup-failed),
   surface it as the gateway error response — do NOT silently fall
   through to the create path. (Defensive: prevents a flaky core from
   degrading to "always create new player.")
```

The pre-existing `playerTokenFromHeader` (`internal/web/auth_handlers.go:38+`)
errors on empty strings; the gate MUST use the raw header read (or a
new `playerTokenFromHeaderOrEmpty` helper) so an absent cookie cleanly
falls through to step 4.

#### 4.2.1 `WebCreateGuest`

When the request cookie resolves to a non-expired PlayerSession, the
gateway MUST refuse the call with a structured error rather than
forwarding to `corev1.CreateGuest` and overwriting the cookie.

Response shape (the new `current_player_name` field is added to the
existing response):

```proto
message WebCreateGuestResponse {
  // Existing fields:
  bool   success                     = 1;
  string error_message               = 2;
  repeated CharacterSummary characters = 3;
  string default_character_id        = 4;

  // NEW:
  string error_code                  = 5;
  string current_player_name         = 6;  // populated only when error_code = ALREADY_AUTHENTICATED
}
```

On gate hit:

- `success = false`
- `error_code = "ALREADY_AUTHENTICATED"`
- `error_message` = human-readable, e.g. `"Already signed in as Jasper Iodine."`
- `current_player_name` = the existing player's display name
  (`Player.Username`), read from the same `CheckPlayerSession` call
  the gate uses to detect the active session. No second round trip.

The cookie MUST NOT be modified on this path.

(Server-side `oops.With("current_player_name", ...)` for log/observability
is fine but is not the wire-level mechanism — the explicit field is.)

#### 4.2.2 `WebAuthenticatePlayer` (registered login)

Same gate, same response-shape extension applied to
`WebAuthenticatePlayerResponse` — gain a `string current_player_name = N`
field on the existing response, populated only when
`error_code = "ALREADY_AUTHENTICATED"`.

The core `AuthenticatePlayer` RPC and its proto are unchanged. The cap
semantics (`maxSessionsPerPlayer`, `CreateWithCap` eviction at
`internal/auth/auth_service.go:154-254`) are unchanged. The gate fires
BEFORE the core RPC is called, so a gate hit never enters
`CreateWithCap` and never evicts; cap behaviour is preserved exactly.

#### 4.2.3 `WebCreatePlayer` (registration)

Same gate, same response-shape extension. A logged-in user submitting
the registration form in another tab MUST get `ALREADY_AUTHENTICATED`
rather than minting a new account and clobbering the existing cookie.

If the team explicitly wants registration to be a "log out + create new
account" flow, that's a UX decision; the gate enforces a hard server-side
no without taking a stance on the UX, and the client-side §4.4.2
treatment hides the form anyway. So the gate is conservatively-correct
either way.

#### 4.2.4 Telnet path

No change. Telnet's auth flow runs in `internal/telnet/`, never goes
through the web gateway, and never carries a cookie. The gate is a
gateway-only addition. Two telnet windows authenticating the same
player continue to mint two PlayerSessions by design (capped via
`maxSessionsPerPlayer`), both reattach to the same character session
on `SelectCharacter`, both work concurrently. No regressions.

#### 4.2.5 Race-window characterisation

The gate is a "check-then-act" sequence and is NOT atomic with player
creation. Two concurrent `WebCreateGuest` calls with the same cookie
state can both pass the gate and both create players — but only in
specific cookie states:

| Cookie state | N concurrent WebCreateGuest calls | Outcome |
| --- | --- | --- |
| Valid + non-expired | All N | All N short-circuit with `ALREADY_AUTHENTICATED`. No new players, no cookie writes. **Gate is reliable here.** |
| Absent (no cookie) | All N | All N pass the gate, all N create new players, all N `Set-Cookie` — last write wins. New tabs are stuck with whichever guest's cookie won the race. **Pre-existing race; design does not narrow it.** |
| Expired | All N | All N pass the gate (expired = same as absent for the gate's purposes), same outcome as above. **Pre-existing race; design does not narrow it.** |

The pre-existing race is a real but narrow window: the user has to
double-click "Try as Guest" or open two fresh tabs simultaneously. The
design does not introduce additional race exposure — it strictly
narrows the failure modes by adding a deterministic short-circuit on
the "valid cookie" branch, which is the case the original P1 bug
hits. Closing the no-cookie / expired-cookie race fully would require
either single-flight per browser (BroadcastChannel client-side) or a
distributed lock keyed on the cookie token (not yet present), and is
explicitly out of scope (see §3 non-goals).

The test plan (§5) MUST include concurrency assertions that pin this
behaviour; see Finding 4 resolution.

### 4.3 Extend `CheckPlayerSession` / `WebCheckSession`

`CheckPlayerSession` already exists (`internal/grpc/auth_handlers.go:506-527`)
and is exposed to the web as `WebCheckSession`
(`internal/web/auth_handlers.go:233-254`,
`api/proto/holomush/web/v1/web.proto:230-234`). It validates the cookie
and returns `player_name`. It is the natural home for "am I signed in"
probing.

We **extend** the existing RPC rather than introducing a parallel
`WhoAmI`. The change is purely additive on the success path; the
auth-failure contract is unchanged.

1. **Request shape unchanged.** No new fields.
2. **Response shape gains additive fields, no semantic change to existing
   ones:**

   ```proto
   message WebCheckSessionResponse {
     string player_name                   = 1;  // existing — stays as Player.Username
     string player_id                     = 2;  // NEW — Player.ID (ULID string)
     bool   is_guest                      = 3;  // NEW — Player.IsGuest
     repeated CharacterSummary characters = 4;  // NEW — buildCharacterSummaries(playerID)
   }
   ```

   The same additive shape applies to the core `CheckPlayerSessionResponse`.

3. **Auth-failure contract unchanged.** On absent / unknown / expired /
   ownership-mismatch / lookup-failed cookies, both core
   `CheckPlayerSession` and gateway `WebCheckSession` continue to return
   the existing error response (core: `nil, err` with
   `PLAYER_SESSION_NOT_FOUND` / `PLAYER_SESSION_EXPIRED`; gateway:
   `connect.CodeUnauthenticated` per
   `internal/web/auth_handlers.go:236-249`). **No `authenticated` boolean
   in the response.** Callers continue to use try/catch (or the gRPC
   status code on the core side) to differentiate.

   Rationale: `web/src/routes/(authed)/+layout.ts:18-25` is a load-bearing
   caller that relies on the throw to redirect to `/login`. A boolean
   flag in the success body would silently break it for any pre-deploy
   tab. Keeping the throw preserves existing client semantics, eliminates
   the BC risk, and the new auth pages can use the same pattern: try
   `webCheckSession`, catch as "unauthenticated."

#### Field semantics (pinned)

| Field | Source | Notes |
| --- | --- | --- |
| `player_name` | `Player.Username` | Same as today. Guest players' `Username` is the generated guest display name (`internal/auth/player.go`), so this works for both registered and guest. |
| `player_id` | `Player.ID` (ULID string) | Returned only on the success path (alongside `player_name`). |
| `is_guest` | `Player.IsGuest` | Returned only on the success path. |
| `characters` | Reuse the existing `buildCharacterSummaries(playerID)` helper at `internal/grpc/auth_handlers.go:685+` (same helper called by `AuthenticatePlayerResponse`). | Empty list is valid (e.g. registered player who deleted all characters). For a guest, the invariant from `CreateGuest` (`internal/grpc/auth_handlers.go:530-561`) is exactly one character; if that invariant ever breaks, this RPC is not the place to repair it — return what's there. |

#### Behaviour

| Cookie state | Response |
| --- | --- |
| Absent | Existing throw / `Unauthenticated` error. Cookie not modified. (No new code path.) |
| Present but unknown / expired / ownership-mismatch / lookup-failed | Existing throw / `Unauthenticated` error. Cookie not modified. |
| Present and valid | Success response with all fields populated. PlayerSession TTL refreshed (see §4.3.1). |

The handler MUST NOT clear the cookie on any negative path. Clearing is
an explicit `Logout` action.

#### 4.3.1 Security contract — enumeration safety, timing, TTL, logging

Because auth failures continue to flow through the existing throw
(core `nil, err`; gateway `connect.CodeUnauthenticated`), the existing
enumeration-safety invariants documented at
`internal/auth/session_ownership.go:18-20` and enforced by
`oops.Code("SESSION_NOT_FOUND")` collapse at lines 42-85 are
**inherited unchanged**. The extended `CheckPlayerSession` introduces
no new negative-path observability surface.

| Property | Required behaviour |
| --- | --- |
| **Response shape on failure** | Identical to today. Single error response shape for absent / unknown / expired / lookup-failed cookies. No new fields, no new error subtypes. |
| **Timing** | Today's `resolvePlayerSession` path hits `playerSessions.GetByTokenHash` for any non-empty token, so unknown vs. expired are already timing-equivalent. Absent-cookie short-circuits at the gateway via `playerTokenFromHeader` returning empty — same as today. **No change.** The plan does NOT need to re-engineer constant-time DB hits; the existing behaviour is the one we are preserving. |
| **TTL refresh** | On the success path, refresh the PlayerSession TTL via `playerSessions.RefreshTTL` — matching the existing `resolvePlayerSession`-callers' best-effort refresh pattern (`internal/grpc/auth_handlers.go:111-123`, `571-602`). A user who only ever loads landing pages MUST NOT see their session expire faster than today's behaviour. The current `CheckPlayerSession` already calls `resolvePlayerSession` which already refreshes; new code reuses that path. |
| **Logging** | No new WARN logs on negative paths. The existing `session ownership mismatch` WARN at `internal/auth/session_ownership.go:76` continues to fire if it ever triggers from this RPC; that's existing behaviour, unchanged. Do NOT add a per-call INFO/WARN that distinguishes negative subcases. |

This RPC becomes the single source of truth for "is the user already
signed in" on every page load. Landing, Login, and Register pages call
it on mount.

### 4.4 Client UX changes

#### 4.4.1 Landing page (`web/src/routes/+page.svelte`)

On mount, call `webCheckSession()` inside a try/catch (mirroring the
existing `(authed)/+layout.ts` pattern). Two render branches:

- **Catch fired (unauthenticated)** — current UI (Login / Register /
  Try as Guest).
- **Success (authenticated)** — hide Login / Register / Try as Guest.
  Show:
  - "Signed in as **{resp.playerName}**"
  - **Continue** button (jumps to `/characters` if multiple, or
    `/terminal` after auto-`SelectCharacter` if exactly one default —
    using `resp.characters` to decide).
  - **Log out** button (calls existing logout handler).

To avoid a flash of the unauthenticated UI for a returning authenticated
user, the probe SHOULD run in a `+page.ts` `load()` function (mirroring
`(authed)/+layout.ts`'s pattern) so SvelteKit blocks render until the
RPC resolves. An in-component `onMount` probe is acceptable as a fallback
but produces a brief flash; the spec does not require eliminating it.

#### 4.4.2 Login + Register pages (`web/src/routes/login`, `web/src/routes/register`)

Same try/catch `webCheckSession()` probe on mount (or in `+page.ts`
`load()` per §4.4.1). If success (authenticated), the form is hidden and
replaced with the same "Signed in as X — Continue / Log out" affordance.
Optional: surface a small notice — "You're already signed in as X.
Logging in here will replace that session." — but that's a v2 polish.

#### 4.4.3 New-tab flow

A user already authenticated in one tab opens a new tab and lands on
`/` (or any deep link):

1. App boots, `webCheckSession()` succeeds (existing `(authed)/+layout.ts` flow).
2. If the route is `/terminal` and sessionStorage has a `sessionId`, the
   terminal page renders normally and Subscribe attaches the new
   connection. If that sessionId is stale (e.g. it was for a different
   player who logged out and a new one logged in), Subscribe returns
   `SESSION_NOT_FOUND`, which falls through to §4.4.5.
3. If the route is `/terminal` and sessionStorage is empty (fresh tab),
   the page redirects to `/characters`. User picks a character →
   `SelectCharacter` reattaches (same character) or creates (different
   character) → terminal renders.
4. If the route is `/`, the authenticated landing branch shows the
   Continue / Log out affordances.

#### 4.4.4 Cookie-collision defence in depth

The client SHOULD also gate `webCreateGuest` and `authenticatePlayer`
calls on `webCheckSession()` throwing (unauthenticated). If a stale UI somehow
triggers them anyway, the new server-side `ALREADY_AUTHENTICATED` error
is the backstop. The client MUST handle this error by:

- Not clearing sessionStorage.
- Routing to the authenticated landing page.
- Surfacing the `current_player_name` in a toast: "You're already signed
  in as {name}."

#### 4.4.5 Stale `sessionStorage` after logout-elsewhere

Tab 1 logged out → cookie cleared (or replaced with a different player's
cookie via the new login flow's logout-then-login dance). Tab 2 still
has stale sessionStorage `{sessionId}`. Existing flow: next RPC against
the stale sessionId fails with `SESSION_NOT_FOUND`.

The audit covers these specific call sites in the terminal page and its
shared client utilities, which all need a uniform "stale-session →
clear sessionStorage → redirect to `/`" path:

| Call site | Path | Today's behaviour |
| --- | --- | --- |
| `Subscribe` stream | `web/src/routes/(authed)/terminal/+page.svelte` Subscribe `try/catch` block | Sets `error = 'Connection lost…'` and stops; does NOT clear sessionStorage. |
| Backfill via `WebQueryStreamHistory` | `web/src/lib/backfill/streamBackfill.ts` `fetchOneStream` | Returns `{ ok: false }`; surrounding code logs and swallows. |
| `WebListSessionStreams` | terminal `+page.svelte` enumeration call | Logs and skips backfill. |
| `HandleCommand` | input submit handler in terminal | Per-call error toast. |

For each, on `SESSION_NOT_FOUND` (or `SESSION_EXPIRED`): call
`clearCharacterSession()` from `authStore.ts`, navigate to `/`. The
landing page's `webCheckSession()` probe (success or throw) then resolves
the (possibly new) auth state. No BroadcastChannel coordination in v1.

### 4.5 Components and units

| Unit | Responsibility | Boundaries |
| --- | --- | --- |
| `CheckPlayerSession` RPC (extended; `internal/grpc/auth_handlers.go:506-527`) | Read-only player+characters lookup keyed off cookie token. Adds `player_id`, `is_guest`, `characters` to the success response per §4.3. Failure contract unchanged. | Pure read. TTL refresh on success (via existing `resolvePlayerSession`). No cookie mutation. Enumeration-safety inherited unchanged per §4.3.1. |
| `WebCheckSession` gateway proxy (extended; `internal/web/auth_handlers.go:233-254`) | Forwards `player_session_token` header → core `CheckPlayerSession` → maps response shape. | Standard proxy. Surfaces the new fields on the web response. |
| Cookie-collision gate (gateway-only; `internal/web/auth_handlers.go`) | In `WebCreateGuest`, `WebAuthenticatePlayer`, `WebCreatePlayer`: read cookie → call `CheckPlayerSession` → if authenticated, short-circuit `ALREADY_AUTHENTICATED + current_player_name`. | Gate runs **before** the core RPC call. Cookie not modified on gate hit. Core RPCs unchanged. |
| Web client `authStore` (`web/src/lib/stores/authStore.ts`) | Holds reactive auth state. | Extend with `playerId`, `playerName`, `isGuest`, `characters`. Populated from `WebCheckSession`. |
| Landing/Login/Register pages (`web/src/routes/+page.svelte`, `.../login/+page.svelte`, `.../register/+page.svelte`) | Branch on auth state. | Each page calls `WebCheckSession` on mount and renders one of two branches. |
| Terminal page (`web/src/routes/(authed)/terminal/+page.svelte`) | Existing logic + sessionStorage-empty redirect to `/characters`. | One small additional guard. |

### 4.6 Error model

| Code | Where | Meaning |
| --- | --- | --- |
| `ALREADY_AUTHENTICATED` | `WebCreateGuest`, `AuthenticatePlayer` | Cookie carries a valid PlayerSession token. The current player's name is in `current_player_name`. |
| `SESSION_NOT_FOUND` | All cookie-validating handlers | Existing. Routes the client to the unauthenticated landing page. Cookie should be cleared client-side via the existing logout flow. |

`ALREADY_AUTHENTICATED` is a soft error: success=false in a normal
response, not a Connect/gRPC status code. This matches the existing
"login failed" / "guest creation failed" patterns and keeps the
client's error handling uniform.

## 5. Test plan

| Layer | Test | Asserts |
| --- | --- | --- |
| Unit | Extended `CheckPlayerSession` with no cookie | Returns existing `Unauthenticated` / `nil, err` (no behaviour change). Cookie NOT cleared. |
| Unit | Extended `CheckPlayerSession` with expired cookie | Returns existing `Unauthenticated` / `nil, err`. Cookie NOT cleared. No new WARN logs. |
| Unit | Extended `CheckPlayerSession` with unknown-token cookie | Returns existing `Unauthenticated` / `nil, err`. Byte-identical to expired path. |
| Unit | Extended `CheckPlayerSession` with valid cookie | Returns success response with `player_name`, `player_id`, `is_guest`, characters list populated. PlayerSession TTL refreshed (assert via `playerSessions.GetByID` `expires_at` advanced). |
| Unit | Pre-deploy `(authed)/+layout.ts` regression guard | Old client expects `webCheckSession` to throw on auth failure; v3 server does. Also expects `resp.playerName` on success; v3 server populates it. Test asserts: throw-on-failure + success-shape backward compat. |
| Unit | `WebCreateGuest` gate with no cookie | Existing happy path; new player + `Set-Cookie` issued. |
| Unit | `WebCreateGuest` gate with valid cookie | Returns `success=false`, `error_code=ALREADY_AUTHENTICATED`, `current_player_name` set. Cookie unchanged. No new player created. No call to `corev1.CreateGuest`. |
| Unit | `WebCreateGuest` gate with expired cookie | Falls through to `corev1.CreateGuest`. New player created. (Documents the race window from §4.2.5.) |
| Unit | `WebAuthenticatePlayer` gate with valid cookie | Same shape as `WebCreateGuest` gate-hit. Cookie unchanged. `maxSessionsPerPlayer` cap untouched (assert via no eviction WARN, no new PlayerSession row). |
| Unit | `WebAuthenticatePlayer` gate with expired cookie + valid creds | Falls through to `corev1.AuthenticatePlayer`. Existing cap eviction semantics apply. |
| Unit | `WebCreatePlayer` gate with valid cookie | Same shape as `WebCreateGuest` gate-hit. No registration occurs. |
| Unit | Concurrent gate (valid cookie) | N=10 concurrent `WebCreateGuest` calls from one client (shared cookie value). All N return `ALREADY_AUTHENTICATED`. Zero new players. Zero `Set-Cookie` writes. |
| Unit | Concurrent gate (no cookie / expired) | N=2 concurrent `WebCreateGuest` calls with no cookie. Pin behaviour: both create players, last `Set-Cookie` wins. Test exists to document the pre-existing race per §4.2.5, not to fix it. |
| Integration (Ginkgo) | Two-tab guest scenario | Tab 1 creates guest A. Tab 2's `WebCreateGuest` returns `ALREADY_AUTHENTICATED`. Tab 2 follows `WebCheckSession` → `SelectCharacter` of guest A's default character → both tabs Subscribe and both receive each other's `say`. |
| Integration | Two-tab same-character scenario | Both tabs end up on the same `session_id`. Both Subscribe streams receive the same events. Both `HandleCommand` calls succeed (ownership validates against player_id, not connection_id). |
| Integration | Two-tab different-character scenario | Player has chars X and Y. Tab 1 selects X (session_X), Tab 2 selects Y (session_Y). Both alive, no ownership-mismatch logs. |
| Integration | Tab + telnet same character | Web tab attached, telnet attached. Both Subscribe / both receive each other's emits. Telnet's connection_id is registered in `sessionStore.connections`; web's is not (documents §4.1 reality). |
| Integration | Browser cookie + concurrent telnet auth (parity) | Browser is signed in as User A (cookie present). Telnet connects, authenticates as User A via username+password. Telnet succeeds (gate is gateway-only, telnet bypasses). Two PlayerSessions exist, character session reattached. |
| Integration | Logout in tab 1, action in tab 2 | Tab 2's next RPC returns `SESSION_NOT_FOUND`. Client clears sessionStorage and routes to `/`. Each of the four call sites listed in §4.4.5 audit table is exercised by at least one assertion. |
| Integration | Pre-deploy client behaviour (regression guard) | Simulate an old client (calls `WebCreateGuest` directly with valid cookie). Server returns `ALREADY_AUTHENTICATED`; old client surfaces as a generic "guest creation failed" error. Test exists to document the deploy-window UX trade-off per §6. |
| E2E | Cmux/Playwright two-tab repro | The exact sequence in §1.1 no longer reproduces — Tab 1 stays "live", Tab 2's create-guest path is replaced by the authenticated landing branch. |

`task pr-prep` MUST pass with zero failures. The integration suite lives
under `test/integration/auth/` (create the file if needed; check
existing structure first).

## 6. Migration / rollout

- **No data migration.** Schema unchanged; this is auth-flow surgery only.
- **Proto change.** Adding fields to existing response messages is
  wire-compatible (new fields default to zero on old clients). No
  breaking change for clients that don't read them.
- **No flag.** The fix is correctness on a P1 bug; rolling it behind a
  flag would just create another state to forget about.
- **Telnet:** no behavioural change. Telnet integration tests + the new
  "browser cookie + concurrent telnet auth" test serve as the
  regression backstop.
- **Existing in-flight web sessions** at deploy time keep working.
  Cookies are still valid; the extended `WebCheckSession` resolves them;
  the landing/login pages will render the new authenticated branch on
  next page load. Steady-state tabs that never reload continue to work
  via their existing Subscribe.
- **Pre-deploy clients vs. new server contract.** A tab still running
  pre-deploy JS that calls `WebCreateGuest` / `WebAuthenticatePlayer` /
  `WebCreatePlayer` with a valid cookie will see `success=false` /
  `error_code=ALREADY_AUTHENTICATED`. Pre-deploy clients don't know
  this code; their existing error-handling renders it as a generic
  "guest creation failed" / "login failed" toast. The user must
  refresh to pick up the new client behaviour. **Acceptable trade-off
  for a P1 fix shipped without a flag — flagging this here so
  post-deploy support tickets in this shape can be triaged as
  expected.**
- **`WebCheckSession` is purely additive.** The success response gains
  fields (`player_id`, `is_guest`, `characters`); the failure contract
  (throw / `Unauthenticated`) is unchanged. Pre-deploy callers like
  `web/src/routes/(authed)/+layout.ts:18-25` that rely on the throw
  to redirect continue to work without modification.
- **Sandbox / CI:** none beyond running the new tests.

## 7. Open questions

- Should `Continue` on the authenticated landing page auto-`SelectCharacter`
  the player's default character (one click → terminal), or always show
  the character list? Current preference: auto-select if exactly one
  character exists, otherwise show the list. Not blocking; default
  behaviour can be tuned later. **Eviction interaction (acknowledged):**
  for a registered player whose default character's session was
  recently evicted by `maxSessionsPerPlayer` cap-trim from a different
  device, an auto-select `SelectCharacter` will create a fresh session
  (since `FindByCharacter` returns SESSION_NOT_FOUND) — that's correct
  behaviour, the user just lands on a fresh terminal. Documented so
  "tune later" doesn't bake a worse default by accident.
- For registered players with `>maxSessionsPerPlayer` PlayerSessions,
  `AuthenticatePlayer` already trims the oldest via `CreateWithCap`. The
  new `ALREADY_AUTHENTICATED` short-circuit happens BEFORE the trim
  (in fact, before the core RPC is called at all), so
  `maxSessionsPerPlayer` semantics are unchanged. Documented here to
  prevent surprise.

## 8. Files touched

| Area | Files |
| --- | --- |
| Proto (extend existing messages, no new RPCs) | `api/proto/holomush/core/v1/core.proto` — extend `CheckPlayerSessionResponse` with `player_id`, `is_guest`, `characters` (additive on success path; failure contract unchanged). `api/proto/holomush/web/v1/web.proto` — extend `WebCheckSessionResponse` with the same fields; extend `WebCreateGuestResponse`, `WebAuthenticatePlayerResponse`, `WebCreatePlayerResponse` with `current_player_name`. |
| Generated code | `pkg/proto/...` regen via `task proto`. |
| Core RPC | `internal/grpc/auth_handlers.go` — extend `CheckPlayerSession` to populate the new fields, refresh PlayerSession TTL on success, reuse `buildCharacterSummaries`. **No changes to** `CreateGuest`, `AuthenticatePlayer`, `CreatePlayer`. |
| Gateway proxies + gates | `internal/web/auth_handlers.go` — extend `WebCheckSession` to map the new fields; add cookie-collision gate to `WebCreateGuest`, `WebAuthenticatePlayer`, `WebCreatePlayer`. |
| Auth state | `web/src/lib/stores/authStore.ts` — extend `AuthState` with `playerId`, `playerName`, `isGuest`, `characters`. Populate from `webCheckSession` success response. |
| Authed layout (additive) | `web/src/routes/(authed)/+layout.ts` — optionally read the new `player_id` / `is_guest` / `characters` fields into the auth store on success. The throw-then-redirect failure path is **unchanged**. |
| Landing | `web/src/routes/+page.svelte` — `webCheckSession` on mount, branch UI. |
| Login | `web/src/routes/login/+page.svelte` — same. |
| Register | `web/src/routes/register/+page.svelte` — same. |
| Terminal | `web/src/routes/(authed)/terminal/+page.svelte` — sessionStorage-empty redirect; uniform `SESSION_NOT_FOUND` handling at the four call sites in §4.4.5. |
| Backfill error path | `web/src/lib/backfill/streamBackfill.ts` — surface SESSION_NOT_FOUND distinctly so the terminal page can route. |
| Tests | `internal/grpc/auth_handlers_test.go`, `internal/web/auth_handlers_test.go`, `test/integration/auth/multi_tab_test.go` (new), web component tests for the auth state branches. |

## 9. References

- Beads: `holomush-9q8n` (P1 bug)
- Related (NOT in scope): `holomush-l2l1`, `holomush-pzen`, `holomush-zxmo`
- `internal/auth/session_ownership.go:18-85` — the validator + the
  enumeration-safety contract this design preserves
- `internal/auth/auth_service.go:154-254` — `AuthenticatePlayer` +
  `CreateWithCap` cap eviction; unchanged by this design
- `internal/web/cookie.go:13` — single shared cookie name; unchanged
- `internal/grpc/auth_handlers.go:175-241` — `SelectCharacter` reattach
  semantics, the load-bearing existing behaviour this design relies on
- `internal/grpc/auth_handlers.go:506-527` — `CheckPlayerSession` (the
  RPC this design extends rather than parallels)
- `internal/grpc/auth_handlers.go:530-561` — `CreateGuest` (target of
  the `WebCreateGuest` gate)
- `internal/grpc/auth_handlers.go:295-338` — `CreatePlayer` (target of
  the `WebCreatePlayer` gate)
- `internal/grpc/server.go:683-716` — `Subscribe` connection-id-optional
  model

### Design review

This spec was reviewed by the `design-reviewer` adversarial reviewer
(2026-04-25). Eleven findings (4 blocking, 7 non-blocking) were
addressed in v2. The reviewer's full report is persisted at
`.claude/agent-memory/design-reviewer/reports/2026-04-25-1200-multi-tab-session-isolation-design.md`.
