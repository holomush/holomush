# ADR 0001: Use Opaque Session Tokens Instead of Signed JWTs

**Date:** 2026-02-02
**Status:** Accepted
**Deciders:** HoloMUSH Contributors

## Context

Web sessions require authentication tokens that clients present on each request. Two common
approaches exist:

**Option A: Signed tokens (JWT/HMAC)**

```text
token = base64(payload) + "." + hmac_sha256(payload, secret)
payload = {player_id, character_id, issued_at, expires_at}
```

- Stateless verification (no database lookup)
- Payload embedded in token
- Requires server secret management and rotation
- Cannot revoke individual tokens without maintaining a blacklist

**Option B: Opaque tokens (random bytes + database)**

```text
token = hex(random_32_bytes)
database stores: SHA256(token) → session record
```

- Stateful verification (database lookup required)
- No payload in token
- No secrets to manage
- Instant revocation by deleting database row

## Decision

We chose **opaque tokens with database-backed sessions**.

Session tokens are 32 random bytes (256 bits of entropy). The SHA256 hash is stored in the
database; the plaintext token is sent to the client. Validation requires a database lookup
by token hash.

## Rationale

### 1. Instant session invalidation

HoloMUSH requires immediate session termination on:

- Explicit logout
- Password change (invalidate all sessions)
- Admin action (ban, security response)

With signed tokens, revocation requires maintaining a blacklist that must be checked on every
request—negating the stateless benefit. Opaque tokens support instant invalidation by simply
deleting the database row.

### 2. Session data mutability

The authentication flow is:

1. Player authenticates (username/password)
2. Session created with `character_id = NULL`
3. Player selects character
4. Session updated with `character_id = <selected>`

With signed tokens, changing session data requires issuing a new token. With database-backed
sessions, we update the row in place.

### 3. Simpler implementation

Opaque tokens require no:

- Server secret management
- Secret rotation strategy
- Signature verification logic
- Payload encoding/decoding

The implementation is straightforward: generate random bytes, hash, store, lookup.

### 4. Appropriate scale

HoloMUSH serves hundreds to thousands of concurrent users, not millions. The database lookup
cost (O(1) with hash index) is negligible. We already use PostgreSQL for all persistence, so
no additional infrastructure is required.

## Consequences

### Positive

- Sessions can be revoked instantly
- Session data (character selection) can be updated without new tokens
- No cryptographic secrets to manage or rotate
- Simpler codebase with fewer security-critical components
- Easy to implement "logout all sessions" and "view active sessions" features

### Negative

- Every authenticated request requires a database lookup
- Cannot verify tokens without database availability
- Horizontal scaling requires shared database (not a concern at MUSH scale)

### Neutral

- Token size is similar (64 hex chars vs ~150 chars for minimal JWT)
- Both approaches use constant-time comparison for security

## Alternatives Considered

### Hybrid approach (signed + database)

Use signed tokens for fast validation but also store in database for revocation. This adds
complexity without clear benefit—if we need the database for revocation anyway, the stateless
verification provides little value.

### Short-lived JWTs with refresh tokens

Use short-lived (5-15 min) JWTs with database-backed refresh tokens. This reduces revocation
latency but adds significant complexity (token refresh logic, two token types, refresh token
rotation). Overkill for our use case.

## References

- [JWT vs Opaque Tokens](https://sergiodxa.com/articles/jwt-vs-opaque-tokens) - Tradeoff analysis
- [OWASP Session Management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
- Implementation: `internal/auth/session.go`
