---
title: "Authentication"
---

This page explains how HoloMUSH authentication behaves as an operator sees it:
what's protected, what behavior to expect, and why the system is designed this
way. For recovery procedures (locked-out accounts, password resets) see
[Authentication recovery](/operating/how-to/authentication-recovery/); for the
log-event, alerting, metrics, and database-table lookups see
[Authentication reference](/operating/reference/authentication/).

## What's protected

HoloMUSH protects player credentials and sessions out of the box. No configuration
is required — the security defaults are production-ready.

| Protection           | Details                                                  |
| -------------------- | -------------------------------------------------------- |
| Password storage     | Argon2id hashing (64 MB memory, resistant to GPU attack) |
| Session tokens       | 256-bit opaque tokens, stored server-side                |
| Username enumeration | Identical error for "not found" and "wrong password"     |
| Token comparison     | Constant-time to prevent timing side-channels            |
| Credential transport | Tokens travel over TLS; no secrets in URLs               |

Operators don't configure password hashing or token generation. The server handles
these with OWASP-recommended parameters.

## Rate limiting

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

## Lockouts

Lockouts expire automatically after 15 minutes. There is no administrative
override — this is intentional so that lockouts remain effective even if an
admin account is compromised. After lockout expires, the player can log in
immediately with the correct password, and the failed-attempt counter resets to
zero on any successful login.

There is no "unlock" command. This is a security design choice, not a missing
feature. To handle a locked-out player report, see
[Recover a locked-out account](/operating/how-to/authentication-recovery/#recover-a-locked-out-account).

## Session management

### Expiry

Active sessions do not expire — a connected player is never kicked by a timer.
When a player disconnects, the session enters a "detached" state and starts a
countdown based on the session's TTL (configured per role via ABAC, defaulting
to 24 hours). If the player reconnects before the TTL expires, the session
resumes exactly where they left off and the timer resets.

Detached sessions that exceed their TTL are reaped automatically by the server.

### What invalidates a session

| Trigger         | Sessions Affected   |
| --------------- | ------------------- |
| Explicit logout | That session only   |
| Password change | All player sessions |
| Password reset  | All player sessions |
| Admin action    | Targeted session(s) |

Players can have multiple concurrent sessions (different devices, different
clients). Each session tracks user agent, IP address, last-seen timestamp,
and currently selected character.

### Instant revocation

Because sessions are stored server-side (not in signed JWTs), you can
invalidate any session instantly by deleting its database row. No key rotation
or token blacklisting required.

## Related pages

- [Authentication recovery](/operating/how-to/authentication-recovery/) — lockout and password-reset procedures
- [Authentication reference](/operating/reference/authentication/) — log events, alerting, metrics, database tables
- [Authentication System (implementation)](/contributing/explanation/authentication/) — how it's built, for contributors
- [Configuration](/operating/reference/configuration/) — Server settings
- [Operations](/operating/how-to/operations/) — Health checks, metrics, and troubleshooting
- [Database](/operating/how-to/database/) — PostgreSQL setup and maintenance
