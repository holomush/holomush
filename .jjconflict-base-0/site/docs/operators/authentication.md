# Authentication

This guide covers authentication configuration and security for HoloMUSH operators.

## Overview

HoloMUSH authentication uses:

- **Argon2id** password hashing with OWASP-recommended parameters
- **Opaque session tokens** with 24-hour expiry
- **Progressive rate limiting** with lockout protection
- **Constant-time operations** to prevent timing attacks

## Security Considerations

### Password Storage

Passwords are hashed using argon2id, the recommended algorithm for password storage.
The parameters (64 MB memory, 1 iteration, 4 threads) are chosen to be:

- Resistant to GPU/ASIC attacks (high memory cost)
- Performant on modern servers (sub-second hashing)
- Compliant with OWASP Password Storage recommendations

Operators do not need to configure password hashing - parameters are built into
the server.

### Session Tokens

Sessions use opaque random tokens (32 bytes / 256 bits of entropy) rather than
signed JWTs. This design choice provides:

| Benefit                | Description                                   |
| ---------------------- | --------------------------------------------- |
| Instant revocation     | Delete database row to invalidate immediately |
| No secret management   | No signing keys to rotate or protect          |
| Session data updates   | Character selection updates in-place          |
| Simpler implementation | Reduced attack surface                        |

See [ADR 0001](../../../docs/adr/0001-opaque-session-tokens.md) for the full rationale.

### Timing Attack Prevention

The authentication system is hardened against timing attacks:

- Non-existent usernames still trigger password verification (using a dummy hash)
- Login returns the same error for "user not found" and "wrong password"
- Token comparison uses constant-time algorithms

This prevents attackers from enumerating valid usernames by measuring response times.

## Rate Limiting

### Behavior

Rate limiting is per-username and protects against brute-force attacks:

| Failed Attempts | Delay | CAPTCHA (web only) |
| --------------- | ----- | ------------------ |
| 1               | 1s    | No                 |
| 2               | 2s    | No                 |
| 3               | 4s    | No                 |
| 4               | 8s    | Yes                |
| 5               | 16s   | Yes                |
| 6               | 32s   | Yes                |
| 7+              | N/A   | 15-minute lockout  |

### Lockout Recovery

Lockouts expire automatically after 15 minutes. There is no administrative
override - this is intentional to ensure lockouts remain effective against
attackers even if an admin account is compromised.

### Counter Reset

Failed attempt counters reset to zero on successful login. A locked account
can still be accessed immediately after the lockout expires if the correct
password is provided.

## Session Management

### Session Expiry

- Sessions expire 24 hours after creation
- Each authenticated request updates the "last seen" timestamp
- Expired sessions are cleaned up automatically

### Session Invalidation

Sessions are invalidated on:

| Trigger         | Scope               |
| --------------- | ------------------- |
| Explicit logout | Single session      |
| Password change | All player sessions |
| Password reset  | All player sessions |
| Admin action    | Targeted session(s) |

### Active Sessions

Players can have multiple concurrent sessions (different devices/browsers).
Each session tracks:

- User agent (client identification)
- IP address (security auditing)
- Last seen timestamp
- Currently selected character

## Password Reset

### Email-Based Reset

When email is configured:

1. Player requests reset via email address
2. Server generates secure token (32 bytes)
3. Token sent via email (1-hour expiry)
4. Player confirms reset with token and new password
5. All existing sessions invalidated

### Out-of-Band Recovery

If email is not configured, password recovery requires administrator
intervention. Options include:

- Database-level password hash update
- Account recreation

Operators should document their recovery procedures and communicate them
to players.

### Token Security

Reset tokens use the same security properties as session tokens:

- 32 bytes of cryptographic randomness
- SHA256 hash stored in database
- 1-hour expiry (shorter than session tokens)
- Single-use (deleted after successful reset)

## Database Requirements

Authentication requires these tables (created by migrations):

| Table             | Purpose              |
| ----------------- | -------------------- |
| `players`         | Player accounts      |
| `web_sessions`    | Active sessions      |
| `password_resets` | Pending reset tokens |
| `characters`      | Player characters    |

Run `holomush migrate up` to ensure all tables exist.

## Monitoring

### Key Metrics

Monitor these authentication-related events:

| Event             | Log Level | Indicates                    |
| ----------------- | --------- | ---------------------------- |
| `login_failed`    | INFO      | Normal (wrong credentials)   |
| `account_locked`  | WARN      | Possible brute-force attempt |
| `session_expired` | DEBUG     | Normal session lifecycle     |
| `password_reset`  | INFO      | Password change event        |

### Security Alerts

Consider alerting on:

- High rate of `account_locked` events (potential attack)
- Multiple `password_reset` events for same player
- Unusual IP addresses or user agents

## Related Documentation

- [Configuration](configuration.md) - Server configuration options
- [Operations](operations.md) - General operational guidance
- [Database](database.md) - Database setup and maintenance
