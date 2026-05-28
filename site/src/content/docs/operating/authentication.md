---
title: "Authentication"
---

This guide covers what you need to know about HoloMUSH authentication as an operator:
what's protected, what behavior to expect, and how to recover when things go wrong.

## What's Protected

HoloMUSH protects player credentials and sessions out of the box. No configuration
is required -- the security defaults are production-ready.

| Protection                    | Details                                                  |
| ----------------------------- | -------------------------------------------------------- |
| Password storage              | Argon2id hashing (64 MB memory, resistant to GPU attack) |
| Session tokens                | 256-bit opaque tokens, stored server-side                |
| Username enumeration          | Identical error for "not found" and "wrong password"     |
| Token comparison              | Constant-time to prevent timing side-channels            |
| Credential transport          | Tokens travel over TLS; no secrets in URLs               |

Operators don't configure password hashing or token generation. The server handles
these with OWASP-recommended parameters.

## Rate Limiting

Rate limiting is per-username and kicks in automatically after failed login attempts.

| Failed Attempts | Delay Imposed | CAPTCHA (web only) |
| --------------- | ------------- | ------------------ |
| 1               | 1s            | No                 |
| 2               | 2s            | No                 |
| 3               | 4s            | No                 |
| 4               | 8s            | Yes                |
| 5               | 16s           | Yes                |
| 6               | 32s           | Yes                |
| 7+              | Locked out    | 15-minute lockout  |

The delays are exponential and applied server-side. Players see progressively
longer waits before the server responds to another login attempt.

## Lockout Recovery

Lockouts expire automatically after 15 minutes. There is no administrative
override -- this is intentional so that lockouts remain effective even if an
admin account is compromised.

After lockout expires, the player can log in immediately with the correct
password. The failed-attempt counter resets to zero on any successful login.

**If a player reports being locked out:**

1. Confirm the lockout is active (check logs for `account_locked` events)
2. Wait for the 15-minute window to pass
3. Have the player try again with the correct credentials

There is no "unlock" command. This is a security design choice, not a missing feature.

## Session Management

### Expiry

Active sessions do not expire — a connected player is never kicked by a timer.
When a player disconnects, the session enters a "detached" state and starts a
countdown based on the session's TTL (configured per role via ABAC, defaulting
to 24 hours). If the player reconnects before the TTL expires, the session
resumes exactly where they left off and the timer resets.

Detached sessions that exceed their TTL are reaped automatically by the server.

### What Invalidates a Session

| Trigger         | Sessions Affected    |
| --------------- | -------------------- |
| Explicit logout | That session only    |
| Password change | All player sessions  |
| Password reset  | All player sessions  |
| Admin action    | Targeted session(s)  |

Players can have multiple concurrent sessions (different devices, different
clients). Each session tracks user agent, IP address, last-seen timestamp,
and currently selected character.

### Instant Revocation

Because sessions are stored server-side (not in signed JWTs), you can
invalidate any session instantly by deleting its database row. No key rotation
or token blacklisting required.

## Password Reset

### With Email Configured

1. Player requests reset via their email address
2. Server sends a one-time token (1-hour expiry)
3. Player confirms reset with the token and a new password
4. Existing sessions are invalidated on a best-effort basis

!!! note
    Session invalidation failures are logged rather than blocking the
    password reset. Operators should monitor for session invalidation
    warnings in the logs.

### Without Email

!!! warning "Planned Feature"

    The admin password reset command is not yet implemented. This is tracked
    as a priority feature. In the interim, contact the development team for
    assistance with password recovery.

## Monitoring

### Key Log Events

| Event             | Log Level | What It Means                  |
| ----------------- | --------- | ------------------------------ |
| `login_failed`    | INFO      | Wrong credentials (normal)     |
| `account_locked`  | WARN      | 7+ failures, possible attack   |
| `session_expired` | DEBUG     | Normal lifecycle               |
| `password_reset`  | INFO      | Password was changed           |

### What to Alert On

Set up alerts for these patterns:

- **High rate of `account_locked` events** -- Potential brute-force attack.
  A burst of lockouts across multiple accounts is especially suspicious.
- **Multiple `password_reset` events for the same player** -- Could indicate
  account takeover attempts.
- **Unusual IP addresses or user agents** -- Watch for logins from unexpected
  geolocations or automated tooling signatures.

### Prometheus Metrics

Both processes expose Prometheus metrics. Point your scraper at the metrics
endpoints (default ports 9100 and 9101). Authentication-specific events surface
through the structured logs rather than dedicated metrics, so log-based alerting
(Loki, Elasticsearch, CloudWatch) is the primary monitoring path here.

## Database Requirements

Authentication depends on these tables, all created by `holomush migrate up`:

| Table             | Purpose              |
| ----------------- | -------------------- |
| `players`         | Player accounts      |
| `web_sessions`    | Active sessions      |
| `password_resets` | Pending reset tokens |
| `characters`      | Player characters    |

If you're restoring from backup or setting up a fresh instance, run
`holomush migrate up` before starting the server to ensure these tables exist.

## Related Pages

- [Configuration](configuration.md) -- Server settings
- [Operations](operations.md) -- Health checks, metrics, and troubleshooting
- [Database](database.md) -- PostgreSQL setup and maintenance
