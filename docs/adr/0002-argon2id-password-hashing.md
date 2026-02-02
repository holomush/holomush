# ADR 0002: Argon2id Password Hashing

**Date:** 2026-02-02
**Status:** Accepted
**Deciders:** HoloMUSH Contributors

## Context

HoloMUSH requires secure password storage for player authentication. Passwords must be hashed
before storage to protect users in case of database compromise. Several password hashing
algorithms exist, each with different security properties:

**Option A: PBKDF2**

- Iteration-based key stretching
- Wide adoption, FIPS-approved
- Not memory-hard (vulnerable to parallel attacks)

**Option B: bcrypt**

- Fixed memory cost (~4KB working set)
- 20+ years of production use
- Vulnerable to GPU/ASIC parallel attacks due to small memory footprint

**Option C: scrypt**

- Memory-hard algorithm
- Configurable time/memory/parallelism
- Less resistant to side-channel attacks than argon2id

**Option D: argon2id**

- Winner of Password Hashing Competition (2015)
- Hybrid of argon2i (side-channel resistant) and argon2d (GPU resistant)
- Modern, memory-hard design with tunable parameters

## Decision

We chose **argon2id** with the following parameters:

| Parameter   | Value    | Purpose                      |
| ----------- | -------- | ---------------------------- |
| Time        | 1        | Number of iterations         |
| Memory      | 64 MB    | Working memory per hash      |
| Parallelism | 4        | Threads for computation      |
| Salt        | 16 bytes | Random salt per password     |
| Output      | 32 bytes | Final hash length (256 bits) |

Hashes are stored in PHC string format:

```text
$argon2id$v=19$m=65536,t=1,p=4$<base64-salt>$<base64-hash>
```

## Rationale

### Why argon2id over alternatives

**bcrypt limitations:**

bcrypt uses a fixed ~4KB working set per hash computation. Modern GPUs with thousands of cores
can compute many bcrypt hashes in parallel since each core only needs 4KB. A single GPU can
attempt millions of passwords per second against bcrypt hashes.

**scrypt limitations:**

scrypt is memory-hard, addressing bcrypt's parallel attack weakness. However, scrypt's internal
structure uses data-dependent memory access patterns, making it vulnerable to cache-timing
side-channel attacks on shared hardware (cloud VMs, containers).

**PBKDF2 limitations:**

PBKDF2 relies purely on iteration count for security—no memory hardness at all. This makes it
trivially parallelizable on GPUs. Even with high iteration counts, PBKDF2 is the weakest
option for new deployments.

**argon2id advantages:**

argon2id is a hybrid mode combining:

- **argon2i's** data-independent memory access (side-channel resistant)
- **argon2d's** data-dependent memory access (GPU attack resistant)

The first pass uses data-independent access patterns (immune to cache-timing attacks), while
subsequent passes use data-dependent access (maximizing GPU attack resistance).

### Parameter selection

The parameters follow OWASP Password Storage Cheat Sheet (2024) recommendations for server-side
hashing:

- **Memory (64 MB)**: High memory cost forces attackers to provision significant memory per
  hash attempt, drastically reducing parallelism. At 64 MB per attempt, a GPU with 24 GB VRAM
  can only compute ~375 hashes in parallel.

- **Iterations (1)**: With 64 MB memory, a single iteration provides strong resistance.
  Additional iterations increase CPU time linearly, but the memory-hardness provides the
  primary defense.

- **Threads (4)**: Utilizes multi-core CPUs for faster legitimate verification while not
  significantly helping attackers (they're memory-bound, not CPU-bound).

### Timing attack prevention

When verifying passwords, the system must take consistent time regardless of whether the user
exists. Without this, an attacker can enumerate valid usernames by measuring response times.

The solution uses a pre-computed dummy hash with identical argon2id parameters:

```go
const dummyPasswordHash = "$argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>"
```

When a login attempt fails because the user doesn't exist, the system verifies the password
against this dummy hash anyway. This ensures the response time is indistinguishable from a
real user with an incorrect password.

See ADR 0004 for the complete timing attack analysis.

### Legacy hash auto-upgrade

For systems migrating from other algorithms (e.g., bcrypt), the hasher supports automatic
upgrade on successful login:

1. User logs in with correct password
2. System verifies against legacy hash (bcrypt or other)
3. If valid, system re-hashes password with argon2id
4. New hash stored, login succeeds
5. Subsequent logins use argon2id

This allows gradual migration without forcing password resets. The `NeedsUpgrade()` method
detects non-argon2id hashes by prefix:

```go
func (h *Argon2idHasher) NeedsUpgrade(hash string) bool {
    return !strings.HasPrefix(hash, "$argon2id$")
}
```

## Consequences

### Positive

- Strong protection against GPU/ASIC parallel attacks via 64 MB memory requirement
- Resistant to side-channel attacks via argon2id hybrid mode
- Industry standard (PHC winner) with ongoing cryptographic scrutiny
- Constant-time comparison prevents timing attacks on hash verification
- Automatic upgrade path from legacy algorithms

### Negative

- 64 MB memory per concurrent hash limits server throughput
- Slower than bcrypt on legitimate servers (~50ms vs ~10ms)
- Requires careful parameter synchronization between hasher and dummy hash

### Neutral

- Hash format is self-describing (parameters embedded in PHC string)
- Implementation uses Go's `golang.org/x/crypto/argon2` package
- Compatible with password managers that generate argon2id hashes

## Alternatives Considered

### Higher iteration count with lower memory

Trading memory for iterations (e.g., t=3, m=32MB) provides similar verification time but
reduced GPU resistance. Memory is the primary defense; iterations are secondary.

### argon2i or argon2d alone

argon2i is purely side-channel resistant but weaker against GPU attacks. argon2d is purely
GPU resistant but vulnerable to side-channels. The hybrid argon2id provides both protections.

### Hardware security modules (HSM)

HSM-based password verification offloads hashing to dedicated hardware. This adds operational
complexity without clear benefit at MUSH scale. Worth revisiting if we need FIPS compliance.

## References

- Implementation: `internal/auth/hasher.go`
- Dummy hash usage: `internal/auth/auth_service.go:67-76`
- Auto-upgrade mechanism: `internal/auth/auth_service.go:146-159`
- [OWASP Password Storage Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)
- [RFC 9106: Argon2 Memory-Hard Function](https://datatracker.ietf.org/doc/html/rfc9106)
- [Password Hashing Competition](https://www.password-hashing.net/)
