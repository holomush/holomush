# Auth Flows Design

**Status:** Draft
**Date:** 2026-03-22
**Bead:** holomush-qve.6 (Auth flows — Login, character select)
**Depends on:** [Auth & Identity Architecture](2026-01-25-auth-identity-design.md) (data model, security parameters)

## Overview

This spec defines the web and telnet authentication flows for HoloMUSH: login,
registration, character selection, password reset, and the site layout that
frames them. It builds on the existing auth domain layer (`internal/auth/`)
which already implements services, domain types, and Postgres repositories.

### Goals

- Two-phase login shared between web and telnet (authenticate → select character)
- Site-wide thin top bar with logo, auth-state-aware navigation
- httpOnly cookie-based session tokens for the web client
- Account registration with optional email (verification deferred)
- Password reset with stubbed email delivery
- Guest access preserved (existing single-phase Login RPC)
- Dark/light theme via existing `prefers-color-scheme` system

### Non-Goals

- Email verification (schema ready, not wired)
- Cloudflare Turnstile (proto fields planned, not implemented)
- OAuth/social login (proto extensibility planned, not implemented)
- Public content pages (landing hero, wiki, directory, roster)
- Character portraits (icon slot in UI, placeholder only)
- MFA/2FA

## Site Structure

### Route Map

All pages share a thin persistent top bar. Routes use SvelteKit layout groups
for auth gating.

```text
Public (anonymous):
  /              → Landing page (stub for now, future hero/game info)
  /login         → Login form
  /register      → Registration form
  /reset         → Password reset request
  /reset/confirm → Password reset confirmation (token in URL)

Authenticated (auth guard redirects to /login if no session):
  /characters    → Character select / create
  /terminal      → Game terminal (existing, moved into auth group)

Future public routes (not in scope):
  /wiki/*        → Game wiki/lore
  /help          → Help pages
  /directory     → Characters in play (public, mixed-visibility fields)
  /roster        → Character roster (public, mixed-visibility fields)

Future authenticated routes (not in scope):
  /scenes        → Scene logs
  /who           → Who's online
  /comms         → Comms hub
```

### SvelteKit Layout

```text
web/src/routes/
  +layout.svelte              ← top bar component (all pages)
  +page.svelte                ← landing page (stub)
  login/+page.svelte          ← login form
  register/+page.svelte       ← registration form
  reset/
    +page.svelte              ← request reset form
    confirm/+page.svelte      ← confirm reset form
  (authed)/                   ← layout group with auth guard
    +layout.ts                ← checks session cookie, redirects if missing
    characters/+page.svelte   ← character select/create
    terminal/+page.svelte     ← existing terminal page
```

The `(authed)` layout group does not affect URLs. `/terminal` remains
`/terminal` in the browser.

### Top Bar

A thin (32px) persistent bar across all pages. Three states:

**Anonymous:**

- Left: logo icon (20×20 slot) + "HoloMUSH" text
- Right: "Login" link + "Register" link

**Authenticated, no character selected:**

- Left: logo icon + "HoloMUSH"
- Right: player username + logout icon

**Authenticated, character selected:**

- Left: logo icon + "HoloMUSH" · character name
- Right: switch-character icon + logout icon

Icons MUST use a lightweight icon library (lucide-svelte). The logo slot
accepts a 20×20 SVG or image; a placeholder initial is used until a logo
exists.

All top bar styling MUST use the existing theme system (`themeStore` + CSS
variables). No hardcoded colors.

## Authentication Architecture

### Shared Auth Path

Authentication is shared between web and telnet. All auth RPCs live on the
core gRPC server, not the gateway. The gateway is a protocol translation
layer only.

```text
Web gateway    ─┐
                ├─→ Core server auth RPCs ─→ auth.Service + Postgres repos
Telnet gateway ─┘
```

Web-specific concern: cookie management (set/clear httpOnly cookies wrapping
the player token and session token).

Telnet-specific concern: text-based command parsing (`connect`, `play`,
`create`).

### Two-Phase Login Flow

**Phase 1 — Authenticate player:**

```text
Client sends credentials (username + password)
  → Core AuthenticatePlayer RPC
  → auth.Service.Login validates credentials
  → PlayerToken created (short-lived, in-memory on client)
  → Returns: player_token + character list
```

**Phase 2 — Select character:**

```text
Client sends player_token + character_id
  → Core SelectCharacter RPC
  → Validates player token
  → Creates or reattaches game session
  → Returns: session_id + character_name
```

**Auto-skip logic** (two independent conditions, either triggers auto-skip):

1. Player has exactly one character → auto-select it (no preference needed)
2. Player has a `default_character_id` set → auto-select that character
   regardless of how many characters they have

In both cases the client calls SelectCharacter automatically and redirects
to `/terminal`, skipping `/characters`. Players with multiple characters
and no default always see the character select page.

### Guest Flow

The existing single-phase `Authenticate` RPC stays for guest access. Guests
bypass the two-phase flow entirely:

```text
"Try as Guest" → existing Login RPC → themed guest name → /terminal
```

No changes to guest authentication logic.

## New Core Proto RPCs

These RPCs are added to `CoreService` in `api/proto/holomush/core/v1/core.proto`,
making them available to both web and telnet gateways.

### AuthenticatePlayer

```protobuf
rpc AuthenticatePlayer(AuthenticatePlayerRequest) returns (AuthenticatePlayerResponse);

message AuthenticatePlayerRequest {
  string username = 1;
  string password = 2;
  string captcha_token = 3;  // reserved for Turnstile, ignored for now
  bool remember_me = 4;      // extend session to 30 days
}

message AuthenticatePlayerResponse {
  bool success = 1;
  string player_token = 2;
  string error_message = 3;
  repeated CharacterSummary characters = 4;
}

// Note: This supersedes the existing CharacterSummary in web.proto (fields 1-4).
// The web.proto CharacterSummary MUST be updated to include last_location and
// last_played_at, or the web gateway must enrich the response when proxying.
message CharacterSummary {
  string character_id = 1;
  string character_name = 2;
  bool has_active_session = 3;
  string session_status = 4;  // "active", "detached", ""
  string last_location = 5;
  int64 last_played_at = 6;
}
```

### SelectCharacter

```protobuf
rpc SelectCharacter(SelectCharacterRequest) returns (SelectCharacterResponse);

message SelectCharacterRequest {
  string player_token = 1;
  string character_id = 2;
}

message SelectCharacterResponse {
  bool success = 1;
  string session_id = 2;
  string character_name = 3;
  bool reattached = 4;
  string error_message = 5;
}
```

### CreatePlayer

```protobuf
rpc CreatePlayer(CreatePlayerRequest) returns (CreatePlayerResponse);

message CreatePlayerRequest {
  string username = 1;
  string password = 2;
  string email = 3;            // optional, for password recovery
  string captcha_token = 4;    // reserved for Turnstile
}

message CreatePlayerResponse {
  bool success = 1;
  string player_token = 2;
  repeated CharacterSummary characters = 3;  // empty for new player
  string error_message = 4;
}
```

### CreateCharacter

```protobuf
rpc CreateCharacter(CreateCharacterRequest) returns (CreateCharacterResponse);

message CreateCharacterRequest {
  string player_token = 1;
  string character_name = 2;
}

message CreateCharacterResponse {
  bool success = 1;
  string character_id = 2;
  string character_name = 3;  // normalized
  string error_message = 4;
}
```

### ListCharacters

```protobuf
rpc ListCharacters(ListCharactersRequest) returns (ListCharactersResponse);

message ListCharactersRequest {
  string player_token = 1;
}

message ListCharactersResponse {
  repeated CharacterSummary characters = 1;
}
```

### Password Reset

```protobuf
rpc RequestPasswordReset(RequestPasswordResetRequest) returns (RequestPasswordResetResponse);
rpc ConfirmPasswordReset(ConfirmPasswordResetRequest) returns (ConfirmPasswordResetResponse);

message RequestPasswordResetRequest {
  string email = 1;
}

message RequestPasswordResetResponse {
  bool success = 1;  // always true to prevent enumeration
}

message ConfirmPasswordResetRequest {
  string token = 1;
  string new_password = 2;
}

message ConfirmPasswordResetResponse {
  bool success = 1;
  string error_message = 2;
}
```

### Logout

```protobuf
rpc Logout(LogoutRequest) returns (LogoutResponse);

message LogoutRequest {
  string session_id = 1;  // ULID, consistent with auth.Service.Logout
}

message LogoutResponse {}
```

## Web Proto Changes

The existing `WebService` in `web/v1/web.proto` changes as follows:

**Remove** the current stub RPCs:

- `AuthenticatePlayer` — replaced by web-prefixed version
- `ListCharacters` — replaced by web-prefixed version
- `SelectCharacter` — replaced by web-prefixed version
- `ListSessions` — removed (session listing is a future feature)

**Keep** unchanged:

- `Login` — stays for guest flow (proxies to core `Authenticate`)
- `SendCommand` — unchanged
- `StreamEvents` — unchanged
- `Disconnect` — unchanged
- `GetCommandHistory` — unchanged

**Add** new web-specific RPCs (thin wrappers around core RPCs with cookie
handling):

- `WebAuthenticatePlayer` — calls core, sets httpOnly cookie with player token
- `WebSelectCharacter` — calls core, sets httpOnly cookie with session token
- `WebCreatePlayer` — calls core, sets httpOnly cookie
- `WebCreateCharacter` — passthrough to core
- `WebListCharacters` — passthrough to core
- `WebLogout` — calls core, clears cookies
- `WebRequestPasswordReset` — passthrough to core
- `WebConfirmPasswordReset` — passthrough to core

## Session Token Storage

### Web Client

**Auth session token:** httpOnly, Secure, SameSite=Strict cookie. Set by the
gateway on successful AuthenticatePlayer. 24-hour expiry by default.

**Remember me:** Extends cookie `Max-Age` from 24 hours to 30 days
(configurable). The `AuthenticatePlayerRequest` gains a `remember_me` bool
field.

**Player token:** Short-lived (5 minutes). Held in the httpOnly cookie
between AuthenticatePlayer and SelectCharacter. Replaced by the session
token once a character is selected.

**Token refresh:** Sliding window — server extends expiry on each validated
request. No client-side refresh logic needed.

**Game session ID:** Stored in `sessionStorage` (existing behavior) for the
terminal page. Derived from the SelectCharacter response.

### Telnet Client

No tokens — the TCP connection IS the session (existing behavior). The
`connect` command triggers AuthenticatePlayer + SelectCharacter in sequence.

## Cookie Middleware

The web gateway adds cookie middleware to:

1. **Set cookies** on auth responses (AuthenticatePlayer, SelectCharacter,
   CreatePlayer)
2. **Clear cookies** on Logout
3. **Validate cookies** on protected RPCs (extract player/session identity)

Cookie parameters:

| Parameter  | Value                          |
| ---------- | ------------------------------ |
| Name       | `holomush_session`             |
| Value      | opaque token (hex-encoded)     |
| HttpOnly   | true                           |
| Secure     | true (relaxed in dev)          |
| SameSite   | Strict                         |
| Path       | /                              |
| Max-Age    | 86400 (24hr) or 2592000 (30d) |
| Domain     | not set (origin only)          |

## Telnet Auth Changes

The telnet gateway updates to use two-phase login:

**Existing flow (stays for guests):**

```text
> connect guest
Welcome! Entering as Sapphire_Flame...
```

**New flow for registered players:**

```text
> connect username password
Welcome back, username! Your characters:
  1. Alaric (last played 2 hours ago) [active]
  2. Beatrix (last played 3 days ago) [detached]
  3. Corwin (last played 2 weeks ago)
Use PLAY <name|number> to select, or CREATE <name> for a new character.

> play 1
Entering world as Alaric...
```

**Auto-enter for single character + default pref:**

```text
> connect username password
Welcome back! Entering as your default character Alaric...
```

**New telnet commands:**

- `PLAY <name|number>` — calls SelectCharacter
- `CREATE <name>` — calls CreateCharacter, then auto-selects

## UI Components

### Login Page (`/login`)

- Centered card with username/password fields
- "Remember me" checkbox
- "Forgot password?" link → `/reset`
- "Sign In" submit button
- Slot below submit for Turnstile widget (hidden until configured)
- "New here? Create an account" link → `/register`
- Separator + "Try as Guest →" link (uses existing Login RPC)
- Slot below separator for social login buttons (hidden until configured)
- Inline error messages for validation failures
- Rate limiting feedback (account locked message with time remaining)

### Register Page (`/register`)

- Centered card with username, email (optional), password, confirm password
- "Create Account" submit button
- Slot for Turnstile widget (hidden)
- "Already have an account? Sign in" link → `/login`
- Inline validation: username taken, password mismatch, email format
- On success: auto-login → redirect to `/characters`

### Character Select Page (`/characters`)

- List of character cards, each with:
  - Left: 44×44 icon slot (initial letter placeholder, future portrait)
  - Center: character name, last played time, last location
  - Right: session status badge (active/detached) when applicable
- "Create New Character" card (dashed border, + icon)
- "Auto-enter as default character on login" checkbox
- Clicking a character calls SelectCharacter → redirects to `/terminal`
- Create flow: inline form or modal with name field + validation

### Password Reset Pages

**`/reset`:** Email field + "Send Reset Link" button. Always shows success
message regardless of whether email exists (prevents enumeration). In dev,
the reset token is logged to the server console.

**`/reset/confirm`:** New password + confirm password fields. Token from URL
query parameter. On success: all sessions invalidated (requires adding
`WebSessionRepository.DeleteByPlayer` call to `PasswordResetService.ResetPassword`),
redirect to `/login`.

### Theme Integration

All auth pages and the top bar MUST use the existing theme system:

- `themeStore` for active theme detection (`prefers-color-scheme`)
- `themeToCssVars()` for CSS variable injection
- `default-dark.json` and `default-light.json` theme files
- No hardcoded color values in components

## Error Handling

| Error | Behavior |
| ----- | -------- |
| Invalid credentials | Inline error on login form, no redirect |
| Account locked (7+ failures) | "Account temporarily locked" with time remaining |
| Username taken (register) | Inline field error |
| Email already registered | Same success message (prevents enumeration) |
| Character name taken | Inline error on create form |
| Character limit reached | Error message with current/max count |
| Session expired | Redirect to /login with "Session expired" flash |
| Network error | Toast/banner at top of page |
| Player token expired | Redirect to /login (re-authenticate) |

## Core Server Wiring

The core server (`internal/grpc/server.go`) gains auth service dependencies:

```go
type CoreServer struct {
    // ... existing fields ...
    authService      *auth.Service
    resetService     *auth.PasswordResetService
    characterService *auth.CharacterService
    playerTokenRepo  auth.PlayerTokenRepository
}
```

The `cmd/holomush/core.go` startup wires Postgres repositories from
`internal/auth/postgres/` into these services and injects them into the
core server.

## Implementation Inventory

### Already Built

| Component | Location | Status |
| --------- | -------- | ------ |
| auth.Service (Login/Logout/ValidateSession/SelectCharacter) | `internal/auth/auth_service.go` | Complete with tests (needs CreatePlayer method) |
| auth.PasswordResetService | `internal/auth/reset_service.go` | Complete with tests |
| auth.CharacterService | `internal/auth/character_service.go` | Complete with tests |
| PlayerToken + PlayerTokenRepository | `internal/auth/player_token.go` | Complete with tests |
| Player domain (rate limiting, lockout, hash upgrade) | `internal/auth/player.go` | Complete with tests |
| WebSession (opaque tokens, SHA256) | `internal/auth/session.go` | Complete with tests |
| Password hasher (argon2id) | `internal/auth/hasher.go` | Complete with tests |
| Postgres PlayerRepository | `internal/auth/postgres/player_repo.go` | Complete with tests |
| Postgres WebSessionRepository | `internal/auth/postgres/session_repo.go` | Complete with tests |
| Postgres PasswordResetRepository | `internal/auth/postgres/reset_repo.go` | Complete with tests |
| Postgres PlayerTokenStore | `internal/store/player_token_store.go` | Complete with tests |
| DB migrations (000009-000013) | `internal/store/migrations/` | Applied |
| Theme system (dark/light, prefers-color-scheme) | `web/src/lib/stores/themeStore.ts` | Complete |
| Terminal page with sidebar | `web/src/routes/terminal/` | Complete |

### To Build

| Component | Location | Description |
| --------- | -------- | ----------- |
| Core auth proto RPCs | `api/proto/core/v1/core.proto` | New messages and RPCs |
| Registration service method | `internal/auth/auth_service.go` | CreatePlayer: hash password, create player, return token |
| Session invalidation on reset | `internal/auth/reset_service.go` | Add WebSessionRepository dependency + DeleteByPlayer call to ResetPassword |
| Password validation | `internal/auth/` | Minimum 8 characters, enforced at service layer |
| Core server auth handlers | `internal/grpc/server.go` | Wire auth services, implement RPCs |
| Core server DB wiring | `cmd/holomush/core.go` | Connect auth repos at startup |
| Web cookie middleware | `internal/web/cookie.go` | Set/clear/validate httpOnly cookies |
| Web gateway auth proxies | `internal/web/handler.go` | Replace stubs, add cookie wrapping |
| Web proto updates | `api/proto/web/v1/web.proto` | New web-specific RPCs |
| Top bar component | `web/src/lib/components/TopBar.svelte` | Three-state top bar |
| Auth store | `web/src/lib/stores/authStore.ts` | Client-side auth state |
| Login page | `web/src/routes/login/+page.svelte` | Login form |
| Register page | `web/src/routes/register/+page.svelte` | Registration form |
| Character select page | `web/src/routes/(authed)/characters/+page.svelte` | Character cards |
| Password reset pages | `web/src/routes/reset/` | Request + confirm |
| Auth guard layout | `web/src/routes/(authed)/+layout.ts` | Session validation |
| Root layout update | `web/src/routes/+layout.svelte` | Add top bar |
| Terminal page move | `web/src/routes/(authed)/terminal/` | Move into auth group |
| Telnet two-phase auth | `internal/telnet/gateway_handler.go` | connect → char select → play |
| Telnet PLAY command | `internal/telnet/` | New command handler |
| Telnet CREATE command | `internal/telnet/` | New command handler |
| Unit tests (core RPCs) | `internal/grpc/server_test.go` | Mock repos |
| Unit tests (cookie middleware) | `internal/web/cookie_test.go` | Set/clear/validate |
| Unit tests (web proxies) | `internal/web/handler_test.go` | Mock core client |
| E2E tests (auth flows) | `web/tests/` | Playwright: login, register, char select |
| E2E tests (guest flow) | `web/tests/` | Verify existing guest path works |

## Future Work

### Email Verification

Schema fields exist (`email_verified` on players table). Implementation
adds: verification token generation, email sending, `/verify?token=xxx`
endpoint, verification gate before character creation (configurable).

### Cloudflare Turnstile

Proto `captcha_token` fields exist on AuthenticatePlayer and CreatePlayer.
Implementation adds: Turnstile widget in login/register forms, server-side
token validation via Turnstile API, rate limiting escalation trigger
(failures 4-6 require captcha).

### OAuth/Social Login

Player table supports linking via future `oauth_providers` table. Login
page has a slot for OAuth buttons. Implementation adds: Discord/Google
OAuth flow, account linking, `oneof` credentials in AuthenticatePlayer.

### Character Portraits

Character select cards have a 44×44 icon slot. Implementation adds: image
upload, storage, CDN serving, display in character cards and potentially
in-game.

## Acceptance Criteria

- [ ] Login form with validation, rate limiting feedback
- [ ] Account creation with username/password, optional email
- [ ] Character list displayed after login with icon slots
- [ ] Character selection creates/reattaches game session
- [ ] New character creation with name validation (Initial Caps, no numbers)
- [ ] httpOnly secure cookie for session tokens
- [ ] Token refresh via sliding window (server-side)
- [ ] Logout clears session and cookies
- [ ] Password reset flow (stubbed email delivery, token logged)
- [ ] Remember me extends session to 30 days
- [ ] Guest flow preserved via existing Login RPC
- [ ] Thin top bar with logo slot, three auth states
- [ ] Dark/light theme via existing themeStore system
- [ ] Auth shared between web and telnet (core RPCs)
- [ ] Telnet: connect → character list → play/create
- [ ] Proto fields reserved for Turnstile (captcha_token)
- [ ] UI slots reserved for OAuth buttons (hidden)
- [ ] Auth guard on /characters and /terminal routes
- [ ] Error handling for all failure modes
- [ ] Unit tests for core auth RPCs
- [ ] Unit tests for cookie middleware
- [ ] Playwright E2E for auth flows
