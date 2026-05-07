<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Event Payload Cryptography — Phase 5 Sub-epic A: TOTP Substrate

## Status

Draft.

## Authors

Sean Brandt; brainstormed with Claude.

## Date

2026-05-07

## Context

This spec defines sub-epic A of the Phase 5 decomposition
(`docs/superpowers/specs/2026-05-07-event-payload-crypto-phase5-decomposition.md`):
the **TOTP substrate** for break-glass auth.

### What this spec ships

A per-player TOTP enrollment, verification, and recovery service plus the
minimum-viable admin CLIs for first-admin bootstrap, self-enrollment, and
lost-device recovery. The Go API is the substrate that sub-epic D
(`OperatorAuthProvider` + dual-control) consumes for break-glass auth.

### Problem statement

The Phase 5 break-glass operator auth flow (master spec §5.9) requires
TOTP as a hard-required second factor (per [decomposition spec][decomp]
Decision 3). HoloMUSH has no TOTP infrastructure today: no library, no
schema, no enrollment UX, no verification API. Without this substrate,
no Phase 5 work can proceed past sub-epic D's design.

### Pinned by the decomposition spec

| Item | Pinned value | Source |
|---|---|---|
| Library | `github.com/pquerna/otp` (RFC 6238) | [decomp][decomp] §"Sub-epic A — TOTP substrate" |
| Storage | Separate `player_totp` table (not columns on `players`) | Same |
| Bootstrap | Once-only; first admin enrolls with no operator auth (host-shell only) | Same |
| Recovery | 10 single-use Argon2id-hashed codes per enrollment | Same |
| Solo-admin recovery | Recovery code clears TOTP; player re-enrolls | Same |
| Multi-admin recovery | `holomush admin totp reset <player>` deferred to sub-epic D | Same |

### Pinned during this brainstorm (additional decisions)

| Decision | Value | Why |
|---|---|---|
| TOTP secret at rest | KEK-wrapped via existing `internal/eventbus/crypto/kek.Provider` (BYTEA + `wrap_key_id`) | Defends against master-spec row 134 with DB read but no KEK access (e.g., leaked PG backup). Reuses existing infra; no new key lifecycle. |
| CLI scope | `bootstrap-enroll`, `enroll`, `recover` ship in A; `reset` ships in D | A is genuinely independent of D; `reset` requires admin auth which is D's deliverable. Avoids duplication and a partial-auth `reset` that risks misuse. |
| Brute-force protection | Per-player TOTP lockout (`failed_attempts`, `locked_until` columns); 5 failures → 15 min lockout | RFC 6238 §5.2 and master-spec threat-model row 134 (shell + creds adversary brute-forcing TOTP). Keeps TOTP failure state separate from password failure state. |
| Replay defense | Track `last_used_step` per player; reject codes whose matched step ≤ `last_used_step` | RFC 6238 §5.2 ("a direct verification with the prover's secret key SHOULD NOT be re-used"). |
| TOTP code parameters | 6 digits, 30s period, HMAC-SHA1, skew=1 | RFC 6238 defaults; authenticator-app-compatible. |
| Bootstrap closure mechanism | Two mechanisms: `crypto_bootstrap_state` row (race-free enforcement gate via INSERT ON CONFLICT DO NOTHING) + `crypto.totp_bootstrap_completed` audit event (durable trail) | Row is cheap and atomic; event survives DB restore and integrates into sub-epic D's chain. Both must be defeated for an attacker to silently re-bootstrap. |
| Recovery code format | 16 hex chars (64 bits entropy), formatted `xxxx-xxxx-xxxx-xxxx` | 64 bits is impractical to brute-force; format is human-typable. |
| Recovery code rate-limit | None in v1 | 64-bit codes are infeasible to brute-force absent a side channel; defer to follow-up bead if needed. |

[decomp]: 2026-05-07-event-payload-crypto-phase5-decomposition.md

### Out of scope

- `holomush admin totp reset <player>` CLI (admin operation requiring
  full break-glass auth) — sub-epic D.
- Web/UI enrollment for general players (player-tier TOTP for web
  login) — future sub-epic.
- TOTP for non-player principals (plugins, services) — not currently
  needed.
- Hardware-token (WebAuthn / FIDO2) enrollment — out of scope per
  [decomp][decomp].
- Recovery-code rate limiting / lockout — deferred (see Defaults).
- Migration of existing TOTP enrollments from any prior system — there
  is no prior system.

## Goals

1. Provide a per-player TOTP enrollment, verification, and recovery
   service whose API surface is small enough to be consumed by sub-epic
   D's `OperatorAuthProvider` without further abstraction.
2. Land bootstrap-enroll, self-enrollment, and lost-device recovery as
   working operator UX in this PR — substrate without consumers tends to
   drift.
3. Enforce master-spec Decision 3 (TOTP hard-required for break-glass)
   in the Go API: D's `OperatorAuthProvider` calls
   `totp.Service.IsEnrolled` + `Verify`; both gates pass before
   break-glass proceeds.
4. Defend against the master-spec row 134 adversary (curious operator
   with shell + DB access) by KEK-wrapping the TOTP secret at rest.
5. Defend against brute-force code-guessing via per-player lockout.
6. Defend against same-window replay via `last_used_step` tracking.
7. Ship the CLI surface scoped to what A can fully test in isolation;
   defer `reset` to D where its full-admin-auth dependency lands.

## Architecture

### Package layout

```text
internal/totp/
    service.go                  # Service interface + impl
    repo.go                     # PG repository (player_totp + player_totp_recovery_codes + crypto_bootstrap_state)
    provisioning.go             # Generate secret, provisioning URI, recovery codes
    errors.go                   # Typed errors via oops.Code
    service_test.go             # Unit tests with mocks
    repo_integration_test.go    # PG-backed repo tests (build tag: integration)

cmd/holomush/
    cmd_admin.go                # New `holomush admin` parent
    cmd_admin_totp.go           # `holomush admin totp` parent + bootstrap-enroll/enroll/recover handlers
    cmd_admin_totp_test.go      # Cobra-tree tests with mocked Service

internal/store/migrations/
    000019_create_player_totp.up.sql      # All three tables in one migration
    000019_create_player_totp.down.sql

test/integration/
    totp_e2e_test.go            # Ginkgo E2E (build tag: integration)
```

### Migration 000019

One migration adds three tables:

```sql
-- Per-player TOTP enrollment (one row per enrolled player).
CREATE TABLE player_totp (
    player_id        TEXT PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
    wrapped_secret   BYTEA NOT NULL,                         -- KEK-wrapped TOTP secret
    wrap_key_id      TEXT NOT NULL,                          -- KEK key ID at wrap time
    enrolled_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_verified_at TIMESTAMPTZ,
    last_used_step   BIGINT,                                 -- replay defense (RFC 6238 §5.2)
    failed_attempts  INTEGER NOT NULL DEFAULT 0,
    locked_until     TIMESTAMPTZ
);

-- 10 single-use recovery codes per enrollment.
-- Codes are Argon2id-hashed; the raw code never lands in PG.
CREATE TABLE player_totp_recovery_codes (
    id           TEXT PRIMARY KEY,                           -- ULID
    player_id    TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    code_hash    TEXT NOT NULL,                              -- Argon2id hash
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    consumed_at  TIMESTAMPTZ                                 -- NULL = unused
);
CREATE INDEX idx_pt_recovery_player_active
    ON player_totp_recovery_codes (player_id) WHERE consumed_at IS NULL;

-- Closure flags for once-only bootstrap mechanisms.
-- Future bootstraps (e.g., new break-glass mechanisms) reuse this table
-- with different keys.
CREATE TABLE crypto_bootstrap_state (
    key                     TEXT PRIMARY KEY,                -- e.g., 'totp_v1'
    consumed_at             TIMESTAMPTZ NOT NULL,
    consumed_by_player_id   TEXT NOT NULL REFERENCES players(id)
);
```

`ON DELETE CASCADE` on `players` means a deleted player loses TOTP and
recovery codes automatically — the FK enforces that orphan TOTP rows
cannot exist. The `crypto_bootstrap_state.consumed_by_player_id` FK has
no CASCADE: a deleted bootstrap-admin's player record is rare and the
historical record matters more than the FK.

### Component diagram

```text
                             ┌────────────────────────┐
  cmd_admin_totp_*.go ──────▶│  totp.Service          │
  (CLI handlers)             │  - BootstrapEnroll     │
                             │  - Enroll              │
                             │  - Verify              │
                             │  - IsEnrolled          │──▶ kek.Provider (wrap/unwrap)
                             │  - ConsumeRecoveryCode │
                             │  - ClearTOTP           │──▶ totp.Repository (PG)
                             └────────────────────────┘──▶ AuditPublisher (eventbus)

  D's OperatorAuthProvider ─▶ totp.Service.IsEnrolled, Verify
  (sub-epic D, future)
```

## Go API surface

```go
package totp

import (
    "context"
    "time"

    "github.com/oklog/ulid/v2"

    "github.com/holomush/holomush/internal/eventbus/crypto/kek"
)

// Clock abstracts time.Now for testability. The default implementation
// (RealClock) returns time.Now(); tests use FakeClock to advance time
// deterministically. We avoid the third-party clockwork dependency to
// keep the package's go.mod surface small.
type Clock interface {
    Now() time.Time
}

// Service is the per-player TOTP enrollment, verification, and recovery API.
// Consumed by sub-epic D's OperatorAuthProvider for break-glass auth, and by
// sub-epic A's admin CLIs.
type Service interface {
    // BootstrapEnroll enrolls the FIRST admin's TOTP without operator auth.
    // The CLI MUST run on the host (no UDS layer in this sub-epic). Refuses
    // with ErrBootstrapAlreadyConsumed if crypto_bootstrap_state already
    // has a 'totp_v1' row.
    BootstrapEnroll(ctx context.Context, playerID ulid.ULID) (Enrollment, error)

    // Enroll enrolls TOTP for a player who is already authenticated by the
    // CLI (creds via auth.Service.ValidateCredentials). Refuses with
    // ErrAlreadyEnrolled if the player already has a player_totp row.
    Enroll(ctx context.Context, playerID ulid.ULID) (Enrollment, error)

    // Verify validates a 6-digit TOTP code.
    // Returns:
    //   nil                       — code valid, last_used_step + last_verified_at updated
    //   ErrNotEnrolled            — no row in player_totp
    //   ErrTOTPLocked{Until}      — failed_attempts ≥ 5, locked until time X
    //   ErrInvalidCode            — code did not match any window; failed_attempts++
    //   ErrCodeReuse              — code matched but step ≤ last_used_step (replay)
    Verify(ctx context.Context, playerID ulid.ULID, code string) error

    // IsEnrolled returns whether the player has a current TOTP enrollment.
    IsEnrolled(ctx context.Context, playerID ulid.ULID) (bool, error)

    // ConsumeRecoveryCode validates a recovery code and marks it consumed.
    // Argon2id-hashes the input, looks up an unconsumed row, marks it
    // consumed_at = NOW(). Emits crypto.totp_recovery_code_consumed.
    // Returns ErrInvalidRecoveryCode for any failure mode (timing-safe;
    // does not leak which step failed).
    ConsumeRecoveryCode(ctx context.Context, playerID ulid.ULID, code string) error

    // ClearTOTP removes the player's TOTP enrollment and unconsumed
    // recovery codes. Idempotent — no-op if not enrolled. Used by recover
    // (after ConsumeRecoveryCode succeeds) and by sub-epic D's reset CLI.
    // Emits crypto.totp_cleared.
    // Does NOT touch crypto_bootstrap_state — recovery does not re-open
    // bootstrap.
    ClearTOTP(ctx context.Context, playerID ulid.ULID, clearedBy ClearReason) error
}

// Enrollment is the one-time output of BootstrapEnroll / Enroll. The CLI
// presents this to the operator to scan into their authenticator app and
// store the recovery codes out-of-band.
type Enrollment struct {
    Secret          string   // base32-encoded; for manual entry
    ProvisioningURI string   // otpauth://totp/<game>:<username>?secret=...&issuer=holomush
    RecoveryCodes   []string // 10 codes, "xxxx-xxxx-xxxx-xxxx"; printed once
}

// ClearReason is the audit-trail justification for a ClearTOTP call.
type ClearReason string

const (
    ClearReasonRecoveryCode  ClearReason = "recovery_code"  // sub-epic A
    ClearReasonAdminReset    ClearReason = "admin_reset"    // sub-epic D
)

// Constructor.
func NewService(
    repo Repository,
    kekProvider kek.Provider,
    clock Clock,
    audit AuditPublisher,
) Service
```

### Verify mechanics

`pquerna/otp`'s public surface is `totp.Validate(code, secret) bool` and
`totp.ValidateCustom(code, secret, t, opts) (bool, error)` — neither
returns which time-step matched. To capture `matchedStep` (required for
replay defense via `last_used_step`), the verify path computes the
expected code per-step across the skew window using
`hotp.GenerateCode(secret, step)` and compares directly:

```text
Verify(playerID, code):
  1. BEGIN transaction (Read Committed isolation)
  2. SELECT wrapped_secret, wrap_key_id, last_used_step,
            failed_attempts, locked_until
     FROM player_totp WHERE player_id = ?
     FOR UPDATE
     → if no row: ROLLBACK; return ErrNotEnrolled
  3. If locked_until > NOW(): ROLLBACK; return ErrTOTPLocked{Until: locked_until}
  4. secret := kek.Unwrap(wrapped_secret, wrap_key_id)
  5. step    := floor(clock.Now().Unix() / 30)
  6. matchedStep := -1
     for s in [step-1, step, step+1]:                   // skew=1 window
       expected := hotp.GenerateCode(secret, uint64(s))  // 6-digit
       if constantTimeEqual(code, expected) == 1:
         matchedStep = s                                  // do NOT break
                                                          // (compute all 3 to
                                                          // avoid timing-leak
                                                          // of matched step)
  7. If matchedStep == -1:                              // no match
       UPDATE player_totp
         SET failed_attempts = failed_attempts + 1,
             locked_until = CASE
               WHEN failed_attempts + 1 >= 5
               THEN NOW() + INTERVAL '15 minutes'
               ELSE locked_until
             END
         WHERE player_id = ?
         RETURNING failed_attempts, locked_until
       COMMIT
       If returned failed_attempts >= 5 AND prior locked_until WAS NULL:
         emit crypto.totp_locked
       Return ErrInvalidCode
  8. If matchedStep <= last_used_step:
       ROLLBACK   // do NOT advance state on replay
       Return ErrCodeReuse
  9. UPDATE player_totp
       SET last_used_step   = matchedStep,
           last_verified_at = NOW(),
           failed_attempts  = 0,
           locked_until     = NULL
       WHERE player_id = ?
 10. COMMIT
 11. Return nil
```

**Concurrency primitive.** `SELECT ... FOR UPDATE` (step 2) takes a
row-level lock for the duration of the txn. Concurrent Verify calls
serialize on the row, eliminating lost-update races on the
`failed_attempts` counter. The success-path `UPDATE` and the
failure-path `UPDATE ... RETURNING` (step 7) both run inside the same
locked txn.

**Constant-time equality.** Use `crypto/subtle.ConstantTimeCompare` for
the per-step value comparison. The loop iterates all three skew steps
unconditionally so wall-clock time does not leak which step matched
(or whether none matched).

**Residual timing accepted.** The conditional assignment to `matchedStep`
inside the loop is a branch-on-result and a sufficiently-precise
inter-iteration timing observer could distinguish "step s matched" from
"step s did not match." The matched step is an audit-logged datum
(`last_used_step`, plus emitted in `crypto.totp_enrolled` /
`crypto.totp_recovery_code_consumed` payloads), so its leakage adds no
new information to an attacker who already has wall-clock precision.
Sub-epic A explicitly accepts this residual leak rather than forcing a
`subtle.ConstantTimeSelect`-driven assignment for cleanliness alone.

**`failed_attempts` reset on success.** Step 9's UPDATE clears the
counter and unlocks. INV-A4 / INV-A5 verify both directions.

**Audit emission ordering for `crypto.totp_locked`.** Emitted AFTER the
COMMIT of the failure-path txn (step 7) but only on the transition
NULL → non-NULL `locked_until` (i.e., the lock-out has just fired).
Repeated failed verifies while already locked do NOT re-emit.

## CLI commands

Three CLIs under `holomush admin totp`. All run on the host with direct
PG access via `internal/config` config-loading (same pattern as
`holomush migrate`). No UDS layer — that lands in sub-epic C; this
sub-epic ships ahead of C and is host-shell-only.

### `holomush admin totp bootstrap-enroll <player-username>`

```text
Usage: holomush admin totp bootstrap-enroll <player-username> [--config <path>]

ONCE-ONLY first-admin enrollment. Requires:
  - Host shell access (the trust path; see master spec §1).
  - <player-username> exists in the players table.

Note on role enforcement.  This sub-epic does NOT verify that the target
player holds RoleAdmin.  The host-shell trust path treats the operator
as authoritative for choosing an appropriate player.  The admin-role
check is enforced by sub-epic D's OperatorAuthProvider at break-glass
time: a player who got bootstrap-enrolled but does not hold an admin
role at any of their bound characters cannot complete break-glass
auth.  See Out of scope below for why this lands in D.

Effect:
  - Begin transaction.
  - INSERT INTO crypto_bootstrap_state ('totp_v1', NOW(), <player-id>)
    ON CONFLICT (key) DO NOTHING. If no row affected → ErrBootstrapAlreadyConsumed.
  - Generate TOTP secret (20 random bytes from crypto/rand; base32 encoded).
  - kek.Wrap(secret) → (wrapped_bytes, wrap_key_id).
  - INSERT INTO player_totp.
  - Generate 10 recovery codes, Argon2id-hash, INSERT into player_totp_recovery_codes.
  - Publish crypto.totp_bootstrap_completed audit event.
  - On publish error → ROLLBACK; return error.
  - Commit.
  - Print Enrollment to STDOUT (see output template below).

Refuses if:
  - <player-username> not found → "player not found".
  - crypto_bootstrap_state already has 'totp_v1' row → "TOTP bootstrap already
    consumed by player <consumed_by_player_id> at <consumed_at>; use
    `holomush admin totp recover` if you have a recovery code, or contact
    another admin (after sub-epic D ships) for reset."
```

### `holomush admin totp enroll [--username <name>]`

```text
Usage: holomush admin totp enroll [--username <name>] [--config <path>]

Self-enrollment: any player can enroll their own TOTP.
Authentication: prompts for username (or reads --username) and password from
stdin (no echo for password). Verifies via auth.Service.ValidateCredentials.

Refuses if:
  - Credentials invalid → "invalid username or password" (timing-safe;
    auth.Service.ValidateCredentials already constant-time).
  - Player already enrolled → "TOTP already enrolled. Use
    `holomush admin totp recover --username <name>` (with a recovery code)
    or contact another admin to reset (sub-epic D)."

Effect:
  - Generate secret, wrap with KEK, store.
  - Generate 10 recovery codes, hash, store.
  - Emit crypto.totp_enrolled audit event.
  - Print Enrollment to STDOUT.
```

### `holomush admin totp recover [--username <name>]`

```text
Usage: holomush admin totp recover [--username <name>] [--config <path>]

Lost-device recovery using a recovery code.
NO PASSWORD REQUIRED — the recovery code is the proof of possession.

Authentication: prompts for username (or reads --username) and recovery
code (no echo).

Effect:
  - ConsumeRecoveryCode: Argon2id-hash input, look up unconsumed row for
    player, mark consumed.
  - On success: ClearTOTP(playerID, ClearReasonRecoveryCode) — DELETE
    player_totp row + remaining unconsumed recovery codes for this player.
  - Emit crypto.totp_recovery_code_consumed and crypto.totp_cleared events.
  - Print: "TOTP cleared for <username>. Run `holomush admin totp enroll
    --username <username>` to re-enroll. Re-enrollment will issue a fresh
    set of recovery codes."

Refuses if:
  - Username not found → "invalid recovery attempt" (timing-safe;
    do not leak which step failed).
  - Recovery code does not match any unconsumed row → "invalid recovery attempt".
```

### Enrollment output template

Used by both `bootstrap-enroll` and `enroll`:

```text
TOTP enrolled for <username> (player_id=<ulid>).

Provisioning URI (scan into authenticator app):
  otpauth://totp/holomush-<game_id>:<username>?secret=<base32>&issuer=holomush

Manual entry secret (if QR scanning unavailable):
  <base32 with spaces every 5 chars>

Recovery codes — STORE THESE OFFLINE NOW (each is single-use):
  1.  xxxx-xxxx-xxxx-xxxx
  2.  xxxx-xxxx-xxxx-xxxx
  ...
  10. xxxx-xxxx-xxxx-xxxx

This output WILL NOT be shown again. Lose your authenticator and these
codes, and you may be permanently locked out of break-glass operations.
```

The CLI MUST flush STDOUT and exit 0 only after the audit event commits.

## Audit events emitted

All five events durable in `events_audit` via the existing audit
projection (master spec §4.6 / §8.1). Subjects use a uniform pattern
under the `audit.<game>.system.crypto_totp.<scope>.<event>` namespace,
where `<scope>` is `bootstrap` for the once-only bootstrap event or
`<player_id>` for per-player events.

| Event type | Subject pattern | Emitted from | Payload |
|---|---|---|---|
| `crypto.totp_bootstrap_completed` | `audit.<game>.system.crypto_totp.bootstrap.completed` | `BootstrapEnroll` (once per server) | `consumed_at`, `consumed_by_player_id`, `bootstrap_key="totp_v1"` |
| `crypto.totp_enrolled` | `audit.<game>.system.crypto_totp.<player_id>.enrolled` | `Enroll` (per-player) | `player_id`, `enrolled_at`, `recovery_codes_issued=10` |
| `crypto.totp_cleared` | `audit.<game>.system.crypto_totp.<player_id>.cleared` | `ClearTOTP` (recovery path or D's reset) | `player_id`, `cleared_at`, `cleared_by` (`recovery_code` from A; `admin_reset` from D) |
| `crypto.totp_recovery_code_consumed` | `audit.<game>.system.crypto_totp.<player_id>.recovery_consumed` | `ConsumeRecoveryCode` | `player_id`, `consumed_at`, `recovery_code_id` (the row ULID; never the code) |
| `crypto.totp_locked` | `audit.<game>.system.crypto_totp.<player_id>.locked` | `Verify` (transition NULL→non-NULL `locked_until`) | `player_id`, `locked_at`, `locked_until`, `reason="brute_force_protection"` |

## Bootstrap closure mechanism

Two cooperating mechanisms:

1. **`crypto_bootstrap_state` row** — race-free enforcement gate via
   `INSERT ... ON CONFLICT (key) DO NOTHING RETURNING key`. Cheap, atomic,
   queried by every `BootstrapEnroll` attempt.
2. **`crypto.totp_bootstrap_completed` audit event** — durable trail.
   Persisted in `events_audit` via the existing crypto-blind cold-tier
   projection (byte-equality with JetStream). Survives DB restore.

### Ordering and atomicity (PG ↔ JetStream)

A PG transaction cannot encompass a JetStream publish (publishes go to
NATS, not PG). The bootstrap-enroll path therefore uses a
**publish-before-COMMIT** ordering with explicit ack waiting:

```text
BootstrapEnroll(playerID):
  BEGIN
    INSERT crypto_bootstrap_state (...) ON CONFLICT DO NOTHING RETURNING key
      → if no row returned: ROLLBACK; return ErrBootstrapAlreadyConsumed
    INSERT player_totp (...)
    INSERT player_totp_recovery_codes (...) × 10
    audit.Publish(crypto.totp_bootstrap_completed)   ── waits for JetStream ack
      → if Publish error: ROLLBACK; return error
    COMMIT
  → if COMMIT error: GHOST CASE (see below); return error
```

**INV-A15 in this model.** Publish error before COMMIT MUST cause
ROLLBACK; no `crypto_bootstrap_state` row exists if publish failed. This
is testable by simulating publish failure and asserting the row is
absent.

### The ghost case (publish-success then COMMIT-fail)

If the publish ack lands but the COMMIT then fails (PG crash, network
partition between server and PG, txn abort), the audit event records a
bootstrap that did not actually take effect. This is a **known
artifact** of bridging PG and JetStream without a distributed
transaction:

- The `events_audit` cold tier shows a `crypto.totp_bootstrap_completed`
  event.
- `crypto_bootstrap_state` has no row.
- A subsequent `BootstrapEnroll` attempt **succeeds** (since the row is
  missing) and emits a second `crypto.totp_bootstrap_completed` event.

**Forensic detection.** A consistency check (manual or future tooling)
SHOULD cross-reference `events_audit` ↔ `crypto_bootstrap_state`:

- N events for `crypto_totp.bootstrap.completed`, no row in
  `crypto_bootstrap_state` → all N are ghost events; bootstrap is in
  fact still un-consumed (anomalous; investigate).
- N events, 1 row → exactly one ghost (or two distinct successful
  bootstraps, if the row was manually inserted out-of-band).
- 0 events, 1 row → DB row exists without audit trail; either an
  out-of-band INSERT or `events_audit` was tampered with.

**Why we accept this gap (vs. an outbox pattern).** Bootstrap-enroll
runs once per server lifetime (this is the design intent — Decision
"once-only" in `decomposition spec`). An outbox table that pumps
audit events asynchronously eliminates the gap but adds permanent
infrastructure for a once-per-server event. The ghost case is rare
(both publish success AND COMMIT failure within the same call),
forensically detectable, and the **operational impact** is bounded
(operator gets `ErrBootstrapAlreadyConsumed` on retry or successfully
re-bootstraps; either way, the system reaches a consistent state).
The decomposition spec's threat-model row 134 ("curious operator with
shell + DB access") is the only adversary class that could exploit
this gap, and they already have full access to the bootstrap state
itself.

For the per-player `enroll`, `recover`, and `cleared` events, the
**same publish-before-COMMIT** pattern applies. Their ghost cases are:

- `enroll` ghost: audit event present, `player_totp` row absent → next
  enroll attempt succeeds (no row to reject); operator sees one extra
  audit event.
- `recover` ghost: audit event present, recovery code marked unconsumed
  in PG → recovery code can be re-used until next ConsumeRecoveryCode
  finalizes consumption (or operator runs `recover` again with the same
  code).
- `cleared` ghost: audit event present, `player_totp` still has the
  enrollment row → next `Verify` succeeds (TOTP still works); the
  audit event is a false alarm.

These ghost cases are forensically detectable and do not weaken the
authentication contract beyond the operator's awareness of "audit ↔
DB consistency" being eventual rather than transactional. Master spec
§1 already concedes that PG superuser + host shell can defeat audit
integrity entirely; the ghost case is a strict subset.

### `crypto.totp_locked` is the exception: emit-AFTER-COMMIT

`crypto.totp_locked` is **not** included in the publish-before-COMMIT
pattern above. Per §"Verify mechanics" §"Audit emission ordering for
`crypto.totp_locked`", the lockout audit event is emitted **after** the
failure-path txn COMMITs. The asymmetry is deliberate:

- Lockout is a **defensive control** against brute-force code guessing
  (master spec threat-model row 134). If publish-before-COMMIT applied,
  a transient NATS unavailability during a lockout-triggering Verify
  would roll back the lockout txn — `failed_attempts` never reaches 5,
  the player is not locked, the attacker keeps trying. That is
  fail-open and the worse failure mode for a brute-force defense.
- Emit-after-COMMIT means the lockout takes effect even when audit
  emission is unavailable. The failure mode is "silent lockout" — the
  player IS locked in PG (`failed_attempts ≥ 5, locked_until > NOW()`)
  but no `crypto.totp_locked` audit event was emitted.
- The silent-lockout case is forensically detectable: a periodic
  consistency check (manual or future tooling) can scan `player_totp`
  for `failed_attempts ≥ 5` rows whose `locked_at` has no matching
  `crypto.totp_locked` event in `events_audit`. Operators investigate
  the gap.
- This is the **only** event in sub-epic A that uses emit-after-COMMIT.
  Every other host-emitted event is a state-progression event whose
  audit trail must hold (publish-before-COMMIT); `crypto.totp_locked`
  is a defensive signal whose enforcement must hold (emit-after-COMMIT).

**Forward-compat with sub-epic D's chaining (Decision 7).** D
introduces hash-chained `crypto.policy_set` events for site-policy
state. The `crypto.totp_bootstrap_completed` event is structurally
similar (point-in-time policy-state event); D MAY include it in the
chain by computing `policy_hash` for it and recording `prev_hash`
referencing the prior `policy_set` event for `policy_name`. The
closure check (the row) does NOT depend on the chain — chain integrity
is purely audit-trail, not enforcement.

**Why two mechanisms.** The row is fast and atomic; the event survives
DB restore and integrates into D's chain. If an attacker with PG
superuser silently deletes the row, the event still exists in
`events_audit`. If the event is silently deleted from `events_audit`,
the row still blocks re-bootstrap. Both must be defeated (consistently)
for an attacker to silently re-bootstrap.

**What this does NOT defend against.** An attacker with PG superuser
AND host shell who edits both the row AND `events_audit` AND the
JetStream-side stream consistently. Master spec §1 already concedes
root-on-host is the trust path.

## Threat-model coverage

| Adversary class (master spec §1) | What this sub-epic defends against | What it does not |
|---|---|---|
| Row 134: Curious operator with shell + DB | KEK-wrapped secret defends against PG-only access (e.g., dump exfiltration without KEK). | Operator with both shell and KEK can extract any TOTP secret. Master spec accepts this. |
| Row 137: Compromised admin without shell | Localhost-host-only enrollment CLIs (no remote API in this sub-epic) deny them reach. | (Per [decomp][decomp] Decision 6: this is topology, not an authentication factor; once D ships and exposes break-glass over UDS, row-137 can attempt break-glass remotely if they reach the host shell separately. Single-control mode is two-factor against row 137 once reach is achieved — see [decomp][decomp] Decision 6.) |
| Brute-force code guesser (row 134, having creds) | 5-failure lockout (15 min); 6-digit code space + lockout makes brute-force infeasible. | A determined attacker with multiple stolen TOTP secrets could rate-shift across players. Out of scope (ABAC + per-source rate limiting belong elsewhere). |
| Replay attacker capturing a valid TOTP code in transit | `last_used_step` rejects same-step reuse. | Different step in same skew window — consciously accepted, RFC 6238 default. |
| Lost-device attacker with creds but no TOTP | Cannot Verify; cannot complete break-glass. | If they also have a recovery code, recover succeeds — that is the design (recovery is the escape valve). |

## Invariants (testable)

| ID | Statement | Test |
|---|---|---|
| INV-A1 | `BootstrapEnroll` MUST refuse if `crypto_bootstrap_state` has a `totp_v1` row. | `TestBootstrapEnrollRefusesAfterFirstSuccess` |
| INV-A2 | Concurrent `BootstrapEnroll` invocations MUST result in exactly one success (PG row-level atomicity). | `TestBootstrapEnrollConcurrentInvocationsExactlyOneSucceeds` (real PG) |
| INV-A3 | `Verify` with a previously-used valid code MUST return `ErrCodeReuse`. The matched step is computed by per-step `hotp.GenerateCode` comparison across the skew window (since `pquerna/otp` does not expose the matched step from its public Validate API). | `TestVerifyRefusesReplayWithinSameStep` |
| INV-A4 | After 5 failed `Verify` attempts within an unlock window, `locked_until` MUST be set to NOW+15min and subsequent verifies MUST return `ErrTOTPLocked`. The increment uses `UPDATE ... RETURNING failed_attempts` inside a `SELECT FOR UPDATE`-locked txn to avoid lost-update races. | `TestVerifyLocksAfterFiveFailures` |
| INV-A5 | After lockout expires, a successful `Verify` MUST reset `failed_attempts` to 0 and clear `locked_until`. | `TestVerifySuccessAfterLockoutExpiryClearsCounter` |
| INV-A6 | `ConsumeRecoveryCode` MUST mark the matched row consumed and refuse second use. | `TestConsumeRecoveryCodeIsSingleUse` |
| INV-A7 | `ClearTOTP` MUST delete the `player_totp` row + all unconsumed recovery codes for the player. | `TestClearTOTPRemovesEnrollmentAndUnconsumedCodes` |
| INV-A8 | `ClearTOTP` MUST NOT touch `crypto_bootstrap_state` — recovery does not re-open bootstrap. | `TestClearTOTPDoesNotResetBootstrapState` |
| INV-A9 | TOTP secret MUST be KEK-wrapped before storage; raw secret MUST NOT appear in PG. | `TestEnrollStoresKEKWrappedSecretNotPlaintext` |
| INV-A10 | All five audit events MUST be emitted on their respective code paths. | `TestAuditEmissionForEachLifecycleEvent` (table-driven) |
| INV-A11 | Recovery codes MUST be stored as Argon2id hashes; raw codes MUST NOT appear in PG. | `TestRecoveryCodesStoredAsArgon2idHashes` |
| INV-A12 | `Verify` with skew=1 MUST accept codes from the previous, current, and next 30s step. | `TestVerifyAcceptsAdjacentTimeSteps` (uses in-tree `Clock` interface) |
| INV-A13 | `BootstrapEnroll` MUST refuse if the target player does not exist in the `players` table. (Admin-role enforcement is sub-epic D's responsibility — see Out of scope.) | `TestBootstrapEnrollRefusesUnknownPlayer` |
| INV-A14 | `Verify` failure paths (invalid code, locked, reuse) MUST NOT update `last_used_step` or `last_verified_at`. | `TestVerifyFailurePathsDoNotMutateSuccessFields` |
| INV-A15 | Audit publish error in `BootstrapEnroll` MUST cause the txn to roll back: no row exists in `crypto_bootstrap_state` after a failed publish. The publish-success-then-COMMIT-fail "ghost case" is acknowledged as a known artifact (see Bootstrap closure mechanism). | `TestBootstrapEnrollRollsBackOnPublishFailure` |

## Testing approach

| Layer | File | What it tests |
|---|---|---|
| Unit | `internal/totp/service_test.go` | Service methods, error branches, lockout/replay logic, audit emission. Mocks: `Repository`, `kek.Provider`, `Clock` (in-tree `FakeClock`), `AuditPublisher`. Table-driven where useful. |
| Repo integration | `internal/totp/repo_integration_test.go` (build tag `integration`) | Repo methods against real PG via testcontainers. INV-A2 specifically requires this layer. |
| CLI | `cmd/holomush/cmd_admin_totp_test.go` | Cobra command tree wiring, flag parsing, prompt handling. Mocks `totp.Service`. |
| E2E | `test/integration/totp_e2e_test.go` (build tag `integration`, Ginkgo/Gomega) | Bootstrap → enroll → verify → recover → re-enroll cycle. Real PG, real KEK file, real audit publisher. |

`task pr-prep` runs `task test` (unit + CLI) and `task test:int`
(repo integration + E2E). Both MUST pass green for the PR.

## Failure modes

| Failure | Detection | Behavior |
|---|---|---|
| KEK unavailable at enroll time | `kek.Wrap` returns error | Enroll fails; no row inserted; CLI prints error and exits non-zero. |
| KEK unavailable at verify time | `kek.Unwrap` returns error | Verify returns wrapped error; `OperatorAuthProvider` (sub-epic D) treats as `ErrInternal`; break-glass refused with operator-friendly message. |
| `wrap_key_id` not found in current KEK provider (KEK rotation drift) | `kek.Unwrap(wrapped, wrap_key_id)` returns `ErrKEKNotFound` | Verify fails. Operator must re-enroll TOTP after a KEK rotation that retired the wrap key — or sub-epic D's KEK-rotation work re-wraps existing TOTP secrets. v1: re-enroll. |
| PG unavailable | Repository methods return error | All TOTP service calls fail; CLI prints error and exits non-zero. |
| Audit publish error before COMMIT (publish-before-COMMIT events) | `AuditPublisher.Publish` returns error | Containing PG transaction rolls back. No `crypto_bootstrap_state` / `player_totp` rows committed. INV-A15. |
| Audit publish ack lands but PG COMMIT fails (ghost case) | Detected by cross-referencing `events_audit` ↔ `crypto_bootstrap_state`/`player_totp` rows | Acknowledged known artifact (see Bootstrap closure mechanism). Operator retries; system reaches consistent state. |
| Audit publish error during `crypto.totp_locked` emit (post-COMMIT) | `failed_attempts ≥ 5, locked_until > NOW()` rows in `player_totp` whose `locked_at` has no matching `crypto.totp_locked` event in `events_audit` | Lockout has taken effect in PG (defensive control intact); audit gap. Forensically detectable via reconciliation. See §"`crypto.totp_locked` is the exception: emit-AFTER-COMMIT". |
| Password failure during `enroll` CLI's `ValidateCredentials` step | `internal/auth/registration.go:86-99` records the failed attempt against `players.failed_attempts` | **Intentional coupling.** A fumbled password on TOTP enrollment counts against the player's web-login lockout. Same lockout policy applies; we do not split the surfaces. |
| Concurrent `BootstrapEnroll` race | Two goroutines, one wins ON CONFLICT | INV-A2: exactly one succeeds; the other gets `ErrBootstrapAlreadyConsumed`. |
| Same TOTP code submitted twice in same step | `last_used_step >= matchedStep` check | `ErrCodeReuse`. |
| Player deleted while their TOTP row exists | FK `ON DELETE CASCADE` on `players.id` | TOTP rows + recovery codes auto-deleted. No orphan rows possible. |

## Dependencies and prerequisites

- **Master spec:** `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` §5.9 (TOTP requirement), §1 (threat model), §4.6 (audit shapes — sub-epic A's events follow the same structural conventions).
- **Decomposition spec:** `docs/superpowers/specs/2026-05-07-event-payload-crypto-phase5-decomposition.md` Decision 3, Defaults sub-epic A.
- **`internal/eventbus/crypto/kek.Provider`:** existing; reused for TOTP secret wrap/unwrap. Loaded at server startup via the existing master-KEK file path. The interface's doc comment (`internal/eventbus/crypto/kek/provider.go:15-42`) describes `Wrap` as "encrypts dek under the current KEK version"; the byte-stream is opaque-by-design and works equally for any sensitive secret. Sub-epic A's plan SHOULD generalize the `kek.Provider` package docstring to "wraps opaque secret bytes — used for DEKs in Phase 2 and TOTP secrets in Phase 5" to reflect the broader use.
- **`internal/auth.Service.ValidateCredentials`:** existing (`internal/auth/registration.go:45`); reused by the `enroll` CLI for the credentials leg of self-enrollment. **Coupling note:** this call records `failed_attempts` on the `players` row; a fumbled password during TOTP enrollment increments the same counter as a fumbled web login. Intentional — see Failure modes.
- **`internal/auth.Argon2idHasher`:** existing; reused for recovery-code hashing.
- **`internal/access.RoleAdmin`:** existing role constant. **Not directly checked by sub-epic A's CLIs** (see Out of scope); enforcement lives in sub-epic D's `OperatorAuthProvider`.
- **`github.com/pquerna/otp`:** **net-new dependency.** Add to `go.mod`. RFC 6238 compliant. The `hotp` sub-package's `GenerateCode(secret, step)` is used in the `Verify` per-step comparison loop (see Verify mechanics).
- **In-tree `Clock` interface:** new in `internal/totp/service.go`. Avoids a third-party clock library; satisfied by `realClock{}` (calls `time.Now()`) in production and a small `FakeClock` in tests. Pattern is consistent with internal taste for keeping dependencies tight.
- **`internal/eventbus.Publisher`:** existing (`internal/eventbus/bus.go:15-17`); used for audit event emission. Publish is synchronous and waits for JetStream ack before returning — see Bootstrap closure mechanism for ordering.

## Out of scope

- `holomush admin totp reset <player>` CLI — sub-epic D.
- Web/UI TOTP enrollment for players — future sub-epic.
- Hardware-token enrollment (WebAuthn / FIDO2) — out of scope per [decomp][decomp].
- Recovery-code rate-limiting / lockout — deferred (see Defaults).
- `OperatorAuthProvider` integration — sub-epic D consumes this sub-epic's `Service`.
- Audit-chain integrity for `crypto.totp_bootstrap_completed` (D's Decision 7 work).
- TOTP secret re-wrapping on KEK rotation — sub-epic E or Phase 6.
- Per-source / per-IP rate limiting at the CLI layer.
- **Admin-role enforcement at bootstrap-enroll / enroll / recover.** The
  HoloMUSH role taxonomy stores roles per-character (in `character_roles`),
  not per-player; a player owns zero or more characters and "player holds
  RoleAdmin" requires resolving "any of this player's bound characters
  holds RoleAdmin." This resolution shape is itself a Phase-5 substrate
  question (master spec §5.9 step 5 says "verify player holds the wizard
  role"). Sub-epic A defers this to sub-epic D's `OperatorAuthProvider`,
  which is the natural integration point for the role-and-capability
  check. Sub-epic A's CLIs are host-shell trust-path operations: the
  operator is responsible for choosing an appropriate player. A
  bootstrap-enrolled non-admin player simply cannot complete break-glass
  auth in D, so no security gap is opened.

## Open questions

1. **Player-level role resolution shape (resolved by sub-epic D, not by
   A).** Master spec §5.9 step 5 says "verify player holds the wizard
   role" but the codebase stores roles per-character (`character_roles`
   table; verified at `internal/store/role_store.go`). Sub-epic D MUST
   pin one of: (a) "any of the player's bound characters holds the role",
   (b) require operator to specify `--character <name>` for break-glass,
   (c) introduce a player-level role surface, or (d) bind break-glass
   identity to character ID rather than player ID. Sub-epic A inherits D's
   answer at the `Service` level (the `playerID ulid.ULID` argument might
   become `characterID` if option (d) wins). Plan-writers SHOULD note this
   in sub-epic A's plan as an integration risk with D, and A's CLIs
   SHOULD avoid wording the bootstrap/enroll/recover flow in role-claim
   terms ("you are an admin") in v1 prose.

## References

- `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` — master spec.
- `docs/superpowers/specs/2026-05-07-event-payload-crypto-phase5-decomposition.md` — decomposition spec; sub-epic A scope, defaults, build order.
- RFC 6238 — TOTP: Time-Based One-Time Password Algorithm.
- RFC 4226 — HOTP, the parent algorithm.
- `github.com/pquerna/otp` — the Go library.
- `internal/auth/registration.go:45` — `ValidateCredentials` (existing Argon2id-verified credential check).
- `internal/eventbus/crypto/kek/provider.go` — `kek.Provider` (existing, used for DEK-wrap; reused here for TOTP secret wrap).
