# Auth & Identity Architecture Design

**Status:** Draft
**Date:** 2026-01-25
**Epic:** holomush-dwk (Epic 5: Auth & Identity)

## Overview

This document specifies the authentication and identity system for HoloMUSH, enabling
secure player authentication with character selection across telnet and web clients.

### Goals

- Secure player authentication with argon2id password hashing
- Separate auth flows optimized for telnet (classic) and web (modern)
- Player-character separation (one player, multiple characters)
- Database-backed web sessions with signed tokens
- Progressive rate limiting with CAPTCHA escalation (web)

### Non-Goals

- OAuth integration (deferred to future epic)
- Character rostering/transfer (deferred to holomush-gloh)
- MFA/2FA (future consideration)

## Data Model

### Player Schema

Players own accounts and authenticate. Characters are owned by players.

```sql
-- 000009_auth_player_fields.up.sql
ALTER TABLE players ADD COLUMN IF NOT EXISTS email TEXT;
ALTER TABLE players ADD COLUMN IF NOT EXISTS email_verified BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE players ADD COLUMN IF NOT EXISTS failed_attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE players ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ;
ALTER TABLE players ADD COLUMN IF NOT EXISTS default_character_id TEXT REFERENCES characters(id) ON DELETE SET NULL;
ALTER TABLE players ADD COLUMN IF NOT EXISTS preferences JSONB NOT NULL DEFAULT '{}';
ALTER TABLE players ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE UNIQUE INDEX IF NOT EXISTS idx_players_email ON players(email) WHERE email IS NOT NULL;

-- Allow rostered characters (future: holomush-gloh)
ALTER TABLE characters ALTER COLUMN player_id DROP NOT NULL;
```

**Fields:**

| Field                | Type        | Description                                      |
| -------------------- | ----------- | ------------------------------------------------ |
| id                   | TEXT (ULID) | Primary key                                      |
| username             | TEXT        | Unique login identifier (existing)               |
| password_hash        | TEXT        | argon2id hash (upgrade from bcrypt)              |
| email                | TEXT        | Optional, unique when set, for password recovery |
| email_verified       | BOOLEAN     | Tracks email verification status                 |
| failed_attempts      | INTEGER     | Counter for rate limiting                        |
| locked_until         | TIMESTAMPTZ | Lockout timestamp (NULL = not locked)            |
| default_character_id | TEXT        | Auto-select character on login                   |
| preferences          | JSONB       | Extensible player preferences                    |
| created_at           | TIMESTAMPTZ | Account creation time                            |
| updated_at           | TIMESTAMPTZ | Last modification time                           |

**Player Preferences (JSONB):**

- `auto_login`: boolean — skip character select if only one character
- `max_characters`: integer — character limit override (default: 5)
- `theme`: string — future UI preferences

### Web Session Schema

Web sessions persist authentication state with opaque tokens (see [Token Design](#web-token-design)).

```sql
-- 000012_web_sessions_schema_update.up.sql (updates 000010)
CREATE TABLE IF NOT EXISTS web_sessions (
    id TEXT PRIMARY KEY,
    player_id TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    character_id TEXT REFERENCES characters(id) ON DELETE SET NULL,  -- nullable until character selected
    token_hash TEXT NOT NULL,      -- SHA256 of random token
    user_agent TEXT NOT NULL,      -- client identification
    ip_address TEXT NOT NULL,      -- client IP for security logging
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_web_sessions_token ON web_sessions(token_hash);
CREATE INDEX IF NOT EXISTS idx_web_sessions_player ON web_sessions(player_id);
CREATE INDEX IF NOT EXISTS idx_web_sessions_expires ON web_sessions(expires_at);

-- 000013_web_sessions_player_index.up.sql
CREATE INDEX IF NOT EXISTS idx_web_sessions_player_created
    ON web_sessions(player_id, created_at DESC);
```

**Key differences from classic JWT approach:**

- `character_id` is nullable — player authenticates first, selects character second
- `token_hash` stores SHA256 of token, not a signature
- `user_agent` and `ip_address` enable security auditing

**Note:** Telnet has no session table — the TCP connection IS the session.

### Password Reset Schema

```sql
-- 000011_password_resets.up.sql
CREATE TABLE IF NOT EXISTS password_resets (
    id TEXT PRIMARY KEY,
    player_id TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_password_resets_player ON password_resets(player_id);
CREATE INDEX IF NOT EXISTS idx_password_resets_expires ON password_resets(expires_at);
```

## Authentication

### Password Hashing

All passwords MUST be hashed with argon2id using OWASP-recommended parameters:

| Parameter   | Value             |
| ----------- | ----------------- |
| Time        | 1 iteration       |
| Memory      | 64 MB             |
| Parallelism | 4 threads         |
| Salt        | 16 bytes (random) |
| Output      | 32 bytes          |

Existing bcrypt hashes SHOULD be upgraded to argon2id on successful login.

### Login Credentials

- **Username**: Required for login (primary identifier)
- **Email**: Optional, required only for password recovery
- **Password**: Owned by player, not character

This differs from classic MUSH where each character has a password.

## Connection & Session Model

| Concept           | Telnet                  | Web                         |
| ----------------- | ----------------------- | --------------------------- |
| Transport         | TCP socket (TLS)        | WebSocket                   |
| Auth method       | `connect <user> <pass>` | Login form → signed token   |
| Character binding | Bound to connection     | Bound to token              |
| Session storage   | Connection IS session   | Database + cookie           |
| Reconnect         | Re-auth required        | Token auto-reconnects       |
| Multi-character   | One per connection      | One "frontmost", can switch |

### Web Token Design

Web sessions use **opaque tokens** (random bytes) rather than signed JWTs. This is a deliberate
design choice that prioritizes instant revocation over stateless verification.

```text
token = hex(random_32_bytes)           # 64 hex characters, sent to client
hash  = SHA256(token)                  # stored in database
```

**Token lifecycle:**

1. On login: generate 32 random bytes, store SHA256 hash in `web_sessions`
2. On request: client sends token, server hashes and looks up session
3. On logout/revoke: delete session row (instant invalidation)

**Why opaque tokens instead of HMAC/JWT:**

| Factor            | HMAC/JWT           | Opaque (chosen)      |
| ----------------- | ------------------ | -------------------- |
| Revocation        | Requires blacklist | Instant (delete row) |
| Session updates   | New token needed   | Update DB row        |
| Secret management | Rotation required  | None                 |
| Complexity        | Higher             | Lower                |

For HoloMUSH, instant revocation on logout/password-change and the ability to update
session data (character selection) without issuing new tokens outweighs the benefit
of stateless verification. The DB lookup cost is negligible at MUSH scale.

**Token parameters:**

- Size: 32 bytes (256 bits of entropy)
- Expiry: 24 hours
- Storage: httpOnly, secure cookie (web) or returned directly (API)
- Comparison: constant-time via `crypto/subtle`

## Authentication Flows

### Telnet Flow

```text
Client                          Server
  |                               |
  |-------- TCP connect --------->|
  |<------- Welcome banner -------|
  |                               |
  |-- connect <user> <pass> ----->|
  |<------ Auth result -----------|
  |<------ Character list --------|
  |                               |
  |-- play <charname> ----------->|
  |<------ Enter world -----------|
```

### Telnet Auth Scenarios

**New player, no character:**

```text
> connect newuser mypassword
Welcome, newuser! You have no characters.
Use CREATE <name> to create your first character.

> create Alaric
Character 'Alaric' created.
Entering world as Alaric...
```

**Returning player, one character:**

```text
> connect returning mypassword
Welcome back! Entering as your character Alaric...
[Auto-enters world]
```

**Returning player, multiple characters:**

```text
> connect returning mypassword
Welcome back! Your characters:
  1. Alaric (last played 2 hours ago)
  2. Beatrix (last played 3 days ago)
Use PLAY <name> or PLAY <number> to select.

> play 1
Entering world as Alaric...
```

**Returning player, default preference set:**

```text
> connect returning mypassword
Welcome back! Entering as your default character Alaric...
[Auto-enters world]
```

### Web Flow

```text
Browser                         Server
  |                               |
  |-- POST /api/auth/login ------>|  {username, password}
  |<----- 200 + character list ---|  Set-Cookie: session=<signed-token>
  |                               |
  |-- POST /api/auth/select ----->|  {character_id}
  |<----- 200 + session ready ----|  Update cookie with character
  |                               |
  |-- WS /api/game/connect ------>|  Cookie auth
  |<----- Game events ------------|
```

## Character Creation

**Commands:**

- Telnet: `CREATE <name>`
- Web: `POST /api/characters {name}`

**Name Validation:**

| Rule       | Description                                      |
| ---------- | ------------------------------------------------ |
| Length     | 2-32 characters                                  |
| Characters | Letters and spaces only (no numbers)             |
| Format     | Normalized to Initial Caps ("alaric" → "Alaric") |
| Uniqueness | Case-insensitive unique                          |
| Whitespace | No leading/trailing spaces                       |

**Initial Placement:**

New characters start in the seeded first room (not NULL location).

**Character Limits:**

- Default: 5 characters per player
- Configurable per-player via preferences JSONB

## Rate Limiting & Security

### Rate Limiting (per-username)

| Failures | Web Action                     | Telnet Action                  |
| -------- | ------------------------------ | ------------------------------ |
| 1-3      | Progressive delay (1s, 2s, 4s) | Same                           |
| 4-6      | Cloudflare Turnstile required  | Delay continues (8s, 16s, 32s) |
| 7+       | 15-minute lockout              | Same                           |

Counters reset on successful login.

### Password Reset Flow

1. User requests reset: `POST /api/auth/reset-request {email}`
2. Server generates 32-byte random token, stores SHA-256 hash
3. Email sent with reset link (1-hour expiry)
4. User confirms: `POST /api/auth/reset-confirm {token, new_password}`
5. Server verifies token, updates password (argon2id)
6. All existing sessions invalidated
7. Reset token deleted

**No email configured:** Contact admin for out-of-band recovery.

### Security Summary

| Aspect               | Implementation                                     |
| -------------------- | -------------------------------------------------- |
| Password hash        | argon2id (t=1, m=64MB, p=4)                        |
| Session token        | 32 bytes random, SHA-256 stored, 24hr expiry       |
| Reset token          | 32 bytes random, SHA-256 stored, 1hr expiry        |
| Rate limiting        | Progressive delay → Turnstile (web) → lockout      |
| Lockout duration     | 15 minutes after 7 failures                        |
| Session invalidation | On password change, on explicit logout (immediate) |
| Token comparison     | Constant-time via crypto/subtle                    |

## API Endpoints

### Authentication

| Method | Endpoint                | Description                  |
| ------ | ----------------------- | ---------------------------- |
| POST   | /api/auth/login         | Authenticate player          |
| POST   | /api/auth/logout        | End session                  |
| POST   | /api/auth/select        | Select character for session |
| POST   | /api/auth/reset-request | Request password reset       |
| POST   | /api/auth/reset-confirm | Confirm password reset       |

### Characters

| Method | Endpoint            | Description              |
| ------ | ------------------- | ------------------------ |
| GET    | /api/characters     | List player's characters |
| POST   | /api/characters     | Create new character     |
| GET    | /api/characters/:id | Get character details    |
| DELETE | /api/characters/:id | Delete character         |

### Player Preferences

| Method | Endpoint                      | Description           |
| ------ | ----------------------------- | --------------------- |
| GET    | /api/player/preferences       | Get preferences       |
| PATCH  | /api/player/preferences       | Update preferences    |
| PUT    | /api/player/default-character | Set default character |

## Future Work

### Character Rostering (holomush-gloh)

Deferred to separate epic. Design discussion captured:

- `player_id` nullable to support rostered (unclaimed) characters
- Players can release (roster) their own characters
- Claiming a rostered character requires admin approval
- Direct player-to-player transfer (admin-mediated initially)
- Possible `roster` table for metadata (notes, restrictions, available_at)
- Audit trail for ownership changes

Epic 5 prepares for this by making `player_id` nullable.

### OAuth Integration

Deferred. Would add:

- `oauth_providers` table linking external accounts
- Discord, Google login options
- Account linking flow

### MFA/2FA

Future consideration for high-security deployments.

## Acceptance Criteria

- [ ] Player accounts with username/password work
- [ ] Optional email field for password recovery
- [ ] argon2id hashing with OWASP parameters
- [ ] Telnet: `connect <user> <pass>` → character select → play
- [ ] Web: login form → character select → WebSocket game connection
- [ ] Signed session tokens for web clients
- [ ] Character creation with name validation (Init Caps, no numbers)
- [ ] New characters placed in seeded first room
- [ ] Rate limiting: progressive delay → Turnstile (web) → lockout
- [ ] Password reset via email (when configured)
- [ ] Player preferences with default character support
- [ ] player_id nullable on characters (rostering prep)
