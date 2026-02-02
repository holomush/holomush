# ADR 0004: Timing-Attack Resistant Authentication

**Date:** 2026-02-02
**Status:** Accepted
**Deciders:** HoloMUSH Contributors

## Context

Authentication systems are vulnerable to timing attacks that leak information about user accounts.
Without mitigation, an attacker can enumerate valid usernames by measuring response times:

**The timing attack threat:**

1. **Non-existent user**: Login returns quickly (no password hash computation needed)
2. **Existing user, wrong password**: Login takes ~50ms (argon2id verification)
3. **Locked account (naive check)**: Login returns quickly (lockout checked before hashing)

By measuring these response time differences, an attacker can:

- Build a list of valid usernames for credential stuffing attacks
- Identify locked accounts (indicating successful previous attacks)
- Target specific users for spear phishing with known-valid addresses
- Reduce brute force search space dramatically

Even small timing differences (milliseconds) are exploitable with statistical analysis over
multiple requests.

## Decision

We implement three coordinated mitigations that together eliminate timing-based information leakage:

### 1. Dummy hash verification for non-existent users

When login fails because the username doesn't exist, we verify the password against a
pre-computed dummy hash anyway:

```go
const dummyPasswordHash = "$argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>"

if userNotFound {
    targetHash = dummyPasswordHash  // Still perform verification
} else {
    targetHash = player.PasswordHash
}
hasher.Verify(password, targetHash)  // Always executed
```

### 2. Lockout check AFTER password verification

Account lockout status is checked after password verification, never before:

```go
// Always verify password first (constant-time operation)
valid, err := hasher.Verify(password, targetHash)

// THEN check lockout
if player.IsLocked() {
    return ErrAccountLocked
}
```

### 3. Constant-time comparison for all sensitive data

All comparisons of security-sensitive data use `crypto/subtle.ConstantTimeCompare`:

```go
subtle.ConstantTimeCompare(computedHash, expectedHash)
subtle.ConstantTimeCompare([]byte(computedToken), []byte(storedHash))
```

## Rationale

### Why dummy hash verification works

The dummy hash uses identical argon2id parameters as real password hashes:

| Parameter   | Value    | Match with real hashes |
| ----------- | -------- | ---------------------- |
| Algorithm   | argon2id | Yes                    |
| Memory      | 64 MB    | Yes                    |
| Iterations  | 1        | Yes                    |
| Parallelism | 4        | Yes                    |

Since argon2id's runtime is dominated by memory operations with these parameters, verifying
against the dummy hash takes the same time as verifying against any real hash. The response
time is indistinguishable whether the user exists or not.

**Critical invariant**: The dummy hash parameters MUST match the real hasher configuration.
See ADR 0002 for parameter details.

### Why lockout must be checked after verification

Consider the naive approach:

```go
// WRONG: This leaks information
if player.IsLocked() {
    return ErrAccountLocked  // Returns in ~1ms
}
valid := hasher.Verify(password, hash)  // Takes ~50ms
```

An attacker can distinguish:

- **Locked account**: Response in ~1ms
- **Unlocked account**: Response in ~50ms

This reveals which accounts are locked (likely due to prior attack attempts), letting attackers
identify valuable targets.

By checking lockout after verification, both locked and unlocked accounts take the same ~50ms
for password verification.

### Why constant-time comparison matters

Standard string comparison (`==`) uses early-exit optimization:

```go
// WRONG: Leaks information about matching prefix length
if computedHash == storedHash { ... }
```

Early-exit returns `false` as soon as a byte mismatch is found. An attacker measuring response
times can determine how many leading bytes matched, reducing the search space for each
subsequent attempt.

`subtle.ConstantTimeCompare` always examines every byte before returning, taking the same time
regardless of where (or if) the inputs differ.

## Code Locations

These patterns MUST be maintained in the following locations:

| Location                  | Pattern                                     |
| ------------------------- | ------------------------------------------- |
| `auth_service.go:67-76`   | Dummy hash constant and documentation       |
| `auth_service.go:84-101`  | Dummy hash selection for non-existent users |
| `auth_service.go:132-141` | Lockout check after password verification   |
| `session.go:115-125`      | Constant-time token hash comparison         |
| `hasher.go:139`           | Constant-time password hash comparison      |

## Consequences

### If dummy hash is removed or parameters mismatch

An attacker can enumerate valid usernames by timing login requests:

- Non-existent user: Fast response (no hash computation)
- Existing user: Slow response (~50ms for argon2id)

With a list of valid usernames, attackers can:

- Conduct targeted credential stuffing
- Send convincing spear phishing emails
- Focus brute force attempts on known accounts

### If lockout is checked before verification

An attacker can identify locked accounts:

- Locked account: Fast response (no verification)
- Unlocked account: Slow response (~50ms)

This reveals which accounts were recently under attack, indicating valuable targets.

### If constant-time comparison is removed

An attacker can recover token or hash values byte-by-byte:

- Measure response time for each possible first byte
- Fastest byte is wrong; slowest is correct
- Repeat for each subsequent byte position

This attack is harder but still exploitable, especially for session tokens.

### Positive

- Login response time is constant regardless of user existence
- Account lockout status is not leaked via timing
- Token/hash comparison resistant to timing analysis
- Defense in depth: multiple mitigations work together

### Negative

- Non-existent user logins still consume ~50ms of CPU/memory
- Could enable minor DoS amplification (attacker sends fake usernames)
- Parameter synchronization between hasher and dummy hash is error-prone

### Neutral

- Performance impact is negligible for legitimate traffic
- Rate limiting mitigates DoS concerns (see `internal/auth/ratelimit.go`)
- Code review should verify parameter consistency

## References

- [ADR 0002: Argon2id Password Hashing](0002-argon2id-password-hashing.md) - Parameter
  configuration
- [OWASP Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html) -
  Timing attack guidance
- [A Lesson In Timing Attacks](https://codahale.com/a-lesson-in-timing-attacks/) - Why
  constant-time matters
- Implementation: `internal/auth/auth_service.go`
- Implementation: `internal/auth/session.go`
- Implementation: `internal/auth/hasher.go`
