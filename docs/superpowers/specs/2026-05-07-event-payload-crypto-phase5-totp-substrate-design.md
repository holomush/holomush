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
| Audit subject namespace | `events.<game>.system.crypto_totp.*` (reserved; not actually emitted by sub-epic A — see next row) | The subject namespace is reserved here so that ABAC seed policies, sub-epic D, and any future emitter target a stable, consistent name. `audit.<game>.system.*` was rejected because `JetStreamPublisher.Publish` validates the `events.` prefix at `internal/eventbus/types.go:163-192` and the EVENTS stream binds only `events.>` (`internal/eventbus/subsystem.go:24-27`). |
| Audit emission ownership | **Deferred to caller layer with eventbus access — sub-epic D / future UDS callers — NOT sub-epic A.** Sub-epic A's host-shell CLIs run as separate processes that cannot reach the holomush server's embedded NATS (`internal/eventbus/subsystem.go:Start()` boots NATS in-process via `DontListen: true`). Sub-epic A's `Service` methods write only to PG; the `Service` interface MAY expose audit-relevant return metadata so callers with eventbus access (sub-epic D's `OperatorAuthProvider`, future server-internal callers reached via sub-epic C's UDS) can emit on behalf of A. | Bootstrap-enroll, enroll, recover, verify, lockout — all five lifecycle events spec'd in §"Audit events emitted" — are produced inside `Service` methods, but emission to JetStream / `events_audit` happens at the calling layer that has the publisher. For sub-epic A's three host-shell CLIs (bootstrap-enroll, enroll, recover), this means **no audit event is emitted** at the time of the operation; the operator's host-shell access is the trust path (master spec §1 already concedes root-on-host bypasses audit). Sub-epic D's `Verify` callsite (the OperatorAuthProvider) WILL emit `crypto.totp_locked` and the per-invocation auth audit because D runs server-side with eventbus access. INV-A10 is RETIRED from sub-epic A (see §"Invariants"); the deny ABAC seed policies (INV-A16) remain because they're forward-looking defense-in-depth for future emitters and subscribers. |
| Atomicity of `BootstrapEnroll` | Single PG transaction wrapping `INSERT crypto_bootstrap_state` + `INSERT player_totp` + `INSERT player_totp_recovery_codes×10`. No JetStream-PG coordination is needed because audit emission is deferred (see "Audit emission ownership" row). On any error → ROLLBACK; bootstrap_state row is NOT claimed. | Trivial PG transactional atomicity — `Repository.BootstrapEnrollAtomic(ctx, claim, enrollment, codes)` opens one txn, runs all inserts, COMMITs on success, ROLLBACKs on any error. INV-A15 is rephrased to reflect this (was: "publish error → rollback"; now: "any insert error → rollback, no row claimed"). The original publish-before-COMMIT contract was load-bearing on JetStream access that sub-epic A's standalone CLIs do not have. |

[decomp]: 2026-05-07-event-payload-crypto-phase5-decomposition.md

### Out of scope

- `holomush admin totp reset <player>` CLI (admin operation requiring
  full break-glass auth) — sub-epic D.
- **Audit event emission for host-shell CLI invocations.** Per R5 Option
  Y: sub-epic A's three CLIs (`bootstrap-enroll`, `enroll`, `recover`)
  run as standalone processes without eventbus access. They produce no
  audit events. The master spec §1 trust model already concedes
  root-on-host as the trust path; the operational forensic story is
  "PG row without matching audit event = host-shell CLI invocation"
  (see §"Audit events emitted" / "The host-shell-CLI gap is the trust
  path"). When sub-epic D ships, server-side callers WILL emit
  `crypto.totp_locked` and the per-invocation auth audit using
  `VerifyResult.LockoutTransition` and the other returned audit
  metadata.
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

internal/access/policy/
    seed.go                     # ADD two forbid seeds for events.*.system.crypto_totp.* (character + plugin)
    seed_test.go                # ADD two corresponding seed-existence tests

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
                             └────────────────────────┘
                                       │
                                       ▼
                             returns audit-event metadata
                             (BootstrapResult, EnrollResult,
                              VerifyResult, etc.) — caller emits

  D's OperatorAuthProvider ─▶ totp.Service.IsEnrolled, Verify
  (sub-epic D, future)         │
                                ▼
                              audit.Publish (server-side, eventbus access)
```

**Audit emission lives at the calling layer**, not inside `totp.Service`.
Sub-epic A's host-shell CLIs cannot reach the running server's embedded
JetStream — see §"Audit events emitted" / "Emission ownership and the
host-shell-CLI gap." `Service` methods return enough information for a
caller to construct the event; the caller is responsible for actually
emitting it.

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

// Service is the per-player TOTP enrollment, verification, and recovery
// API. PG-only side effects; no audit emission. Callers with eventbus
// access (sub-epic D's OperatorAuthProvider; future server-side flows)
// emit audit events using the returned result structs.
type Service interface {
    // BootstrapEnroll enrolls the FIRST admin's TOTP without operator auth.
    // Refuses with ErrBootstrapAlreadyConsumed if crypto_bootstrap_state
    // already has a 'totp_v1' row. Wraps INSERT bootstrap_state + INSERT
    // player_totp + INSERT recovery_codes×10 in a single PG txn (via
    // Repository.BootstrapEnrollAtomic). Returns BootstrapResult carrying
    // the Enrollment (for stdout printing) and audit-event metadata for
    // server-side callers that want to emit crypto.totp_bootstrap_completed.
    BootstrapEnroll(ctx context.Context, playerID ulid.ULID) (BootstrapResult, error)

    // Enroll enrolls TOTP for a player who is already authenticated by the
    // caller. Refuses with ErrAlreadyEnrolled if the player already has a
    // player_totp row.
    Enroll(ctx context.Context, playerID ulid.ULID) (EnrollResult, error)

    // Verify validates a 6-digit TOTP code.
    // Returns VerifyResult carrying the outcome (success / failure mode)
    // and any state transition (e.g., LockoutTransition: NULL→non-NULL).
    // Server-side callers consume LockoutTransition to emit
    // crypto.totp_locked.
    //
    // Result.Outcome ∈ {OutcomeOK, OutcomeNotEnrolled, OutcomeLocked,
    //                   OutcomeInvalidCode, OutcomeCodeReuse}
    // err is non-nil only for infrastructure failures (PG, KEK unwrap).
    Verify(ctx context.Context, playerID ulid.ULID, code string) (VerifyResult, error)

    // IsEnrolled returns whether the player has a current TOTP enrollment.
    IsEnrolled(ctx context.Context, playerID ulid.ULID) (bool, error)

    // ConsumeRecoveryCode validates a recovery code and marks it consumed.
    // Returns ConsumeRecoveryResult on success; ErrInvalidRecoveryCode on
    // failure (timing-safe — does not leak which step failed).
    // Server-side callers emit crypto.totp_recovery_code_consumed using
    // result.RecoveryCodeID.
    ConsumeRecoveryCode(ctx context.Context, playerID ulid.ULID, code string) (ConsumeRecoveryResult, error)

    // ClearTOTP removes the player's TOTP enrollment and unconsumed
    // recovery codes. Idempotent. Returns ClearResult with cleared_by =
    // the supplied reason. Server-side callers emit crypto.totp_cleared.
    // Does NOT touch crypto_bootstrap_state — recovery does not re-open
    // bootstrap.
    ClearTOTP(ctx context.Context, playerID ulid.ULID, clearedBy ClearReason) (ClearResult, error)
}

// Enrollment is the one-time enrollment material. Carries the TOTP
// secret and recovery codes — printed once by sub-epic A's CLIs to
// stdout, then forgotten in process.
type Enrollment struct {
    Secret          string   // base32-encoded
    ProvisioningURI string   // otpauth://totp/<game>:<username>?secret=...&issuer=holomush
    RecoveryCodes   []string // 10 codes, "xxxx-xxxx-xxxx-xxxx"
}

// BootstrapResult is the return type of Service.BootstrapEnroll. Carries
// the Enrollment (for stdout) and audit metadata for server-side callers.
type BootstrapResult struct {
    Enrollment       Enrollment
    AuditConsumedAt  time.Time // for crypto.totp_bootstrap_completed payload
    AuditPlayerID    ulid.ULID // = consumed_by_player_id
    BootstrapKey     string    // = "totp_v1"
}

// EnrollResult is the return type of Service.Enroll.
type EnrollResult struct {
    Enrollment      Enrollment
    AuditEnrolledAt time.Time // for crypto.totp_enrolled payload
    AuditPlayerID   ulid.ULID
}

// VerifyResult is the return type of Service.Verify.
type VerifyResult struct {
    Outcome           VerifyOutcome
    LockedUntil       *time.Time // set when Outcome == OutcomeLocked OR a lockout transition just fired
    LockoutTransition bool       // true iff this Verify call transitioned the player NULL→locked
    AuditAt           time.Time  // = clock.Now() at call time; for caller-emitted events
}

// VerifyOutcome enumerates the outcomes of Service.Verify.
type VerifyOutcome int

const (
    OutcomeOK            VerifyOutcome = iota // code valid; state advanced
    OutcomeNotEnrolled                        // no row in player_totp
    OutcomeLocked                             // already locked (LockedUntil > now)
    OutcomeInvalidCode                        // code did not match
    OutcomeCodeReuse                          // matched but ≤ last_used_step
)

// ConsumeRecoveryResult is the return type of Service.ConsumeRecoveryCode.
type ConsumeRecoveryResult struct {
    RecoveryCodeID  ulid.ULID // ULID of the consumed row; for audit payload
    AuditConsumedAt time.Time
    AuditPlayerID   ulid.ULID
}

// ClearResult is the return type of Service.ClearTOTP.
type ClearResult struct {
    ClearedBy     ClearReason
    AuditClearedAt time.Time
    AuditPlayerID  ulid.ULID
    WasEnrolled    bool // false if the call was a no-op (idempotent); callers SHOULD skip emit when false
}

// ClearReason is the audit-trail justification for a ClearTOTP call.
type ClearReason string

const (
    ClearReasonRecoveryCode  ClearReason = "recovery_code"  // sub-epic A's recover CLI
    ClearReasonAdminReset    ClearReason = "admin_reset"    // sub-epic D's reset CLI
)

// Constructor — note: NO AuditPublisher parameter. Audit emission is
// the caller's responsibility (see §"Audit events emitted" / "Emission
// ownership and the host-shell-CLI gap").
func NewService(
    repo Repository,
    kekProvider kek.Provider,
    clock Clock,
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
       Return VerifyResult{
         Outcome:           OutcomeInvalidCode,
         LockedUntil:       new locked_until (may be nil),
         LockoutTransition: (returned failed_attempts >= 5 AND prior locked_until WAS NULL),
         AuditAt:           clock.Now(),
       }
  8. If matchedStep <= last_used_step:
       ROLLBACK   // do NOT advance state on replay
       Return VerifyResult{Outcome: OutcomeCodeReuse, AuditAt: clock.Now()}
  9. UPDATE player_totp
       SET last_used_step   = matchedStep,
           last_verified_at = NOW(),
           failed_attempts  = 0,
           locked_until     = NULL
       WHERE player_id = ?
 10. COMMIT
 11. Return VerifyResult{Outcome: OutcomeOK, AuditAt: clock.Now()}
```

Note on the LockedUntil/LockoutTransition fields: these are how the
caller learns that *this Verify call* fired the lockout. A server-side
caller (sub-epic D's `OperatorAuthProvider`) inspects
`result.LockoutTransition` and emits `crypto.totp_locked` when it's
true. Sub-epic A's host-shell CLIs do not consume Verify directly.

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

**Audit emission ordering for `crypto.totp_locked` (caller-side).**
Sub-epic A's `Service.Verify` does not emit. The returned `VerifyResult.LockoutTransition`
flag is true exactly once per lockout (when `failed_attempts` increment
crosses the threshold and `locked_until` becomes non-NULL). Server-side
callers (sub-epic D's `OperatorAuthProvider`) check this flag after
calling `Verify` and emit `crypto.totp_locked` via the eventbus.
Repeated failed verifies while already locked return
`Outcome: OutcomeLocked` with `LockoutTransition: false` so callers do
not re-emit. The "emit-AFTER-COMMIT" ordering is implicit in this
design: the PG COMMIT inside `Verify` happens before the Service
returns; the caller's emit happens after the return; consequently the
audit publish is always post-COMMIT and a transient eventbus failure
cannot roll back the lockout.

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

The CLI MUST flush STDOUT and exit 0 after the PG transaction commits. No
audit event is emitted for host-shell CLI invocations — see
§"Audit events emitted" / "Emission ownership and the host-shell-CLI gap."

## Audit events emitted

### Subject namespace (reserved)

Sub-epic A reserves the following subject namespace for TOTP-lifecycle
audit events. The namespace is defined here so that ABAC seed policies
and future emitters target a stable name. **Sub-epic A does not itself
emit these events** — see "Emission ownership" below.

Subjects use a uniform pattern under
`events.<game>.system.crypto_totp.<scope>.<event>` where `<scope>` is
`bootstrap` for the once-only bootstrap event or `<player_id>` for
per-player events.

**Why `events.>` and not `audit.>`.** The repo's EVENTS JetStream
stream binds only `events.>` subjects (`internal/eventbus/subsystem.go:24-27`);
`JetStreamPublisher.Publish` validates the prefix at
`internal/eventbus/types.go:163-192` and rejects anything not under
`events.`. Master spec §4.6 wrote audit subjects in the `audit.<game>...`
form aspirationally — there is no production code path today that
reaches `events_audit` via an `audit.>` subject. The single existing
path that lands rows in `events_audit` is the `events.>` filter on the
EVENTS stream consumed by `internal/eventbus/audit/projection.go:62-65`.

| Event type | Subject pattern | Produced inside | Payload |
|---|---|---|---|
| `crypto.totp_bootstrap_completed` | `events.<game>.system.crypto_totp.bootstrap.completed` | `Service.BootstrapEnroll` | `consumed_at`, `consumed_by_player_id`, `bootstrap_key="totp_v1"` |
| `crypto.totp_enrolled` | `events.<game>.system.crypto_totp.<player_id>.enrolled` | `Service.Enroll` | `player_id`, `enrolled_at`, `recovery_codes_issued=10` |
| `crypto.totp_cleared` | `events.<game>.system.crypto_totp.<player_id>.cleared` | `Service.ClearTOTP` | `player_id`, `cleared_at`, `cleared_by` (`recovery_code` from A; `admin_reset` from D) |
| `crypto.totp_recovery_code_consumed` | `events.<game>.system.crypto_totp.<player_id>.recovery_consumed` | `Service.ConsumeRecoveryCode` | `player_id`, `consumed_at`, `recovery_code_id` (the row ULID; never the code) |
| `crypto.totp_locked` | `events.<game>.system.crypto_totp.<player_id>.locked` | `Service.Verify` (transition NULL→non-NULL `locked_until`) | `player_id`, `locked_at`, `locked_until`, `reason="brute_force_protection"` |

### Emission ownership and the host-shell-CLI gap

`internal/eventbus/subsystem.go:Start()` boots the embedded NATS server
in-process via `DontListen: true`. There is no external connection
point — a separate process (e.g., a host-shell CLI) cannot reach the
running server's JetStream. Sub-epic A's three CLIs
(`bootstrap-enroll`, `enroll`, `recover`) are short-lived processes
that connect to PG directly and **cannot** publish events into the
running server's JetStream.

Therefore, sub-epic A's `Service` methods produce the events
**conceptually** (carry the lifecycle information needed to construct
them) but **do not emit** them. Emission is the responsibility of
callers that have eventbus access:

- **Sub-epic D's `OperatorAuthProvider`** runs server-side (inside the
  holomush server process via the UDS path that sub-epic C provides).
  It calls `Service.Verify` and emits `crypto.totp_locked` /
  `crypto.totp_recovery_code_consumed` / `crypto.totp_cleared` when it
  observes the corresponding state transitions.
- **Future server-internal callers** (any code path that eventually
  invokes `Service.BootstrapEnroll` / `Enroll` from inside the running
  server — e.g., a future "first-run wizard" web flow) emit
  `crypto.totp_bootstrap_completed` / `crypto.totp_enrolled` from their
  layer.
- **Sub-epic A's host-shell CLIs emit no events.** This is the
  intentional gap.

`Service` API exposes the information needed for the caller to emit:
either via return-value metadata (a `BootstrapResult` carrying the
`consumed_at` / `bootstrap_key`) or via an optional emitter callback
that is nil for standalone CLIs and non-nil when a server-side caller
wires it up. Either shape is acceptable; the plan picks one and tests
both code paths (caller-with-emitter and caller-without).

**Caveat: `crypto.totp_bootstrap_completed` has no production emitter
in Phase 5.** Of the five reserved subjects, four
(`crypto.totp_enrolled`, `crypto.totp_cleared`,
`crypto.totp_recovery_code_consumed`, `crypto.totp_locked`) have a
sub-epic D emitter via `OperatorAuthProvider`'s consumption of
`Service.Verify` and the related break-glass paths. The
`crypto.totp_bootstrap_completed` event has **no Phase-5 emitter** —
sub-epic D's `OperatorAuthProvider` consumes only `Service.Verify`
(not `BootstrapEnroll`), and no other Phase-5 sub-epic invokes
bootstrap. The subject is reserved for a future server-side caller
(e.g., a web first-run wizard or admin-RPC handler) that doesn't yet
exist. Until such a caller exists, EVERY bootstrap-enroll happens via
the host-shell CLI path and produces no audit event — which is
consistent with the trust-path story below.

### The host-shell-CLI gap is the trust path

Master spec §1 threat model already concedes that host-shell access is
**outside the threat model** — the trust path; root on host bypasses
audit integrity at the architectural level (master spec §1, §5.9 lines
1276-1279, §7.5 lines 1714-1716). A host-shell `bootstrap-enroll`
producing no audit event is therefore consistent with what the master
spec already accepts.

Forensic implications:

- `crypto_bootstrap_state` row exists, no `crypto.totp_bootstrap_completed`
  event in `events_audit` → bootstrap was performed via the host-shell
  CLI; operator action; trust path.
- `crypto_bootstrap_state` row exists AND a corresponding event is in
  `events_audit` → bootstrap was performed via a server-side caller
  (sub-epic D or future).
- No row, event present → ghost (server-side caller's PG txn rolled
  back after the event was published; impossible in the simplified
  Option-Y atomicity model since PG and audit happen in the same txn
  for server-side callers, but theoretically possible if a future
  caller diverges from the simple shape).
- No row, no event → bootstrap not consumed (initial state).
- (Row, ≥2 events for the same `bootstrap_key`) → unreachable through
  spec'd code paths (PRIMARY KEY on `crypto_bootstrap_state.key` +
  `INSERT ... ON CONFLICT DO NOTHING` make a second successful claim
  impossible; legitimate server-side callers return
  `ErrBootstrapAlreadyConsumed` on the second call and would not
  publish a second event). Reaching this state indicates either
  audit-table tampering or a future caller violating per-key emission
  idempotency.

Operators auditing TOTP enrollment activity SHOULD treat
`crypto_bootstrap_state` rows without a matching audit event as
evidence of host-shell activity, not as gaps in audit coverage.

### ABAC seed policies for the new subject namespace

Master spec §7.7 establishes ABAC as the authoritative gate that denies
character / plugin principals subscribe access to the `audit.>`
subject namespace (per Phase 3d Decision 4 — NATS-level deny retired).
Two seed policies enforce this for `audit.>`:
`seed:deny-audit-read-character` and `seed:deny-audit-read-plugin` at
`internal/access/policy/seed.go:216-227`.

Sub-epic A introduces a NEW subject namespace (`events.<game>.system.crypto_totp.*`)
that carries operator-tier audit data (`crypto.totp_bootstrap_completed`,
`crypto.totp_locked`, etc.) which character / plugin principals MUST NOT
read — same threat-model rationale as `audit.>`. Sub-epic A's plan MUST
add **two new forbid seed policies** parallel to the existing
`audit.*` denies:

```text
seed:deny-events-system-crypto-totp-read-character
  forbid(principal is character, action in ["read"], resource is stream)
    when { resource.stream.name like "events.*.system.crypto_totp.*" };

seed:deny-events-system-crypto-totp-read-plugin
  forbid(principal is plugin, action in ["read"], resource is stream)
    when { resource.stream.name like "events.*.system.crypto_totp.*" };
```

**Permits:** none added. The ABAC engine evaluates forbid-overrides-permit
(`internal/access/policy/engine.go:541-544`), so the new forbids deny
**all** character principals — including admin characters — and **all**
plugin principals from subscribing to `events.*.system.crypto_totp.*`
streams via the gRPC subscribe path. This matches the existing
`seed:deny-audit-read-character` precedent: admin characters are also
denied subscribe access to `audit.*` streams. Operator-tier audit
access is **out-of-scope for character-principal subscribe**; it flows
through system-principal reads (the audit projection itself; future
admin-RPC handlers running with system identity) or direct PG queries
of `events_audit`. Sub-epic D's `AdminReadStream` will provide the
operator-visible read path.

**Why narrow (`events.*.system.crypto_totp.*`) and not broad
(`events.*.system.*`).** Sub-epic A only introduces TOTP audit subjects
under `events.*.system.crypto_totp.*`. Other potential
`events.*.system.*` subject patterns (e.g., session-lifecycle audit, if
any are migrated from `audit.*`) are out-of-scope for sub-epic A. A
broader-scope deny is sub-epic D's territory (which carries master-spec
amendments) or beyond.

## Bootstrap closure mechanism

The single mechanism that makes bootstrap once-only is the
**`crypto_bootstrap_state` row**. Race-free enforcement via
`INSERT ... ON CONFLICT (key) DO NOTHING RETURNING key`. Cheap, atomic,
queried by every `BootstrapEnroll` attempt.

The corresponding `crypto.totp_bootstrap_completed` audit event is a
**reserved** event type that sub-epic A's host-shell CLIs do not emit
(see §"Audit events emitted" / "Emission ownership and the
host-shell-CLI gap"). Server-side callers that consume
`Service.BootstrapEnroll` and have eventbus access (future server-side
flows; **not** sub-epic D, which only consumes `Service.Verify`) MAY
emit the event after the txn commits. Their emission is by definition consistent with the PG state
(if the txn committed, the row exists; if the txn rolled back, no
emission was reached) — see "Server-side caller atomicity" below.

### Atomicity (PG-only, no JetStream coordination)

Sub-epic A's CLIs are short-lived host processes that cannot reach the
running server's embedded JetStream (`internal/eventbus/subsystem.go`
runs NATS with `DontListen: true`). Therefore the atomicity boundary
for sub-epic A is **strictly PG**:

```text
Repository.BootstrapEnrollAtomic(ctx, claim, enrollment, codes):
  BEGIN
    INSERT crypto_bootstrap_state (...) ON CONFLICT DO NOTHING RETURNING key
      → if no row returned: ROLLBACK; return ErrBootstrapAlreadyConsumed
    INSERT player_totp (...)
    INSERT player_totp_recovery_codes (...) × 10
    COMMIT
  → if any error in the inserts: ROLLBACK; return error
```

No JetStream publish is involved. INV-A15 (originally
"publish error → rollback") is rephrased as: "any insert error during
`BootstrapEnrollAtomic` MUST cause ROLLBACK; no `crypto_bootstrap_state`
row exists when the call returns an error." This is trivially testable
with a stubbed Repository that fails on the third INSERT.

### Server-side caller atomicity (forward-looking)

When sub-epic D (or any future server-side caller) wraps both
`Service.BootstrapEnroll` and the corresponding audit emission, that
caller takes responsibility for whatever audit-state consistency model
it wants. The shape we expect:

```text
D.OperatorAuthProvider.HandleBootstrap(...):
  result, err := Service.BootstrapEnroll(...)   // PG-only; returns audit metadata
  if err: return err                             // no audit row; no event
  err = audit.Publish(buildBootstrapEvent(result))  // server-side; eventbus access
  if err:
    log.Warn("bootstrap completed but audit publish failed", ...)
    // operationally accept the gap; PG row exists, no event
  return nil
```

The PG state is never inconsistent (sub-epic A's PG-only atomicity
guarantees that). The only inconsistency surface is "PG row exists, no
audit event" — which is **the same inconsistency surface produced by
the host-shell CLI path** (§"Audit events emitted" / "Emission
ownership"). Operators reading `events_audit` ↔ `crypto_bootstrap_state`
already need to interpret "row without event" as either (a) host-shell
CLI invocation, or (b) server-side caller's post-COMMIT publish failure.
Both are real. Both are within the master spec §1 trust model.

### Per-player events (`enroll`, `recover`, `cleared`, `locked`)

For per-player events, sub-epic A's `Service` methods (`Enroll`,
`ConsumeRecoveryCode`, `ClearTOTP`, `Verify`) likewise own only PG state.
They produce no events; emission is the calling layer's responsibility.

`crypto.totp_locked` is conceptually a defensive signal that should
land **after** the lockout txn commits (so a transient publish failure
cannot roll back the lockout itself — see master spec threat-model row
134, brute-force defense). Sub-epic D's `OperatorAuthProvider`, which
calls `Service.Verify`, gets this for free under Option Y: the PG txn
inside `Verify` commits the failed-attempt increment / lockout
unconditionally; D's caller observes the post-COMMIT state and emits
the event from server-side. No publish-before-COMMIT coordination is
needed because the publish is a separate concern at a different layer.

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
| INV-A10 | **Retired in R5 (Option Y).** Service methods do not emit audit events; emission is the calling layer's responsibility (see §"Audit events emitted" / "Emission ownership and the host-shell-CLI gap"). The `VerifyResult` / `BootstrapResult` / `EnrollResult` / `ClearResult` / `ConsumeRecoveryResult` structs carry the audit metadata callers need; tests that the metadata is correctly populated take the place of audit-emission tests. | `TestVerifyResultPopulatesLockoutTransitionOnFirstLock`, `TestBootstrapResultCarriesAuditMetadata`, etc. |
| INV-A11 | Recovery codes MUST be stored as Argon2id hashes; raw codes MUST NOT appear in PG. | `TestRecoveryCodesStoredAsArgon2idHashes` |
| INV-A12 | `Verify` with skew=1 MUST accept codes from the previous, current, and next 30s step. | `TestVerifyAcceptsAdjacentTimeSteps` (uses in-tree `Clock` interface) |
| INV-A13 | `BootstrapEnroll` MUST refuse if the target player does not exist in the `players` table. (Admin-role enforcement is sub-epic D's responsibility — see Out of scope.) | `TestBootstrapEnrollRefusesUnknownPlayer` |
| INV-A14 | `Verify` failure paths (invalid code, locked, reuse) MUST NOT update `last_used_step` or `last_verified_at`. | `TestVerifyFailurePathsDoNotMutateSuccessFields` |
| INV-A15 | Any error during `Repository.BootstrapEnrollAtomic` (failed `INSERT crypto_bootstrap_state`, `INSERT player_totp`, or `INSERT player_totp_recovery_codes`) MUST cause the single PG transaction to roll back: no row exists in `crypto_bootstrap_state` and no row exists in `player_totp` when the call returns an error. (Per R5 Option Y: no JetStream coordination is involved; the txn is PG-only. The original publish-before-COMMIT INV-A15 wording was load-bearing on JetStream access that sub-epic A's standalone CLIs do not have.) | `TestBootstrapEnrollAtomicRollsBackOnInsertError` |
| INV-A16 | Two ABAC seed forbid policies MUST exist denying character and plugin principals from reading streams matching `events.*.system.crypto_totp.*`. | `TestSeedPoliciesIncludesEventsSystemCryptoTotpDenyForCharacter` and `TestSeedPoliciesIncludesEventsSystemCryptoTotpDenyForPlugin` (parallel to existing `TestSeedPoliciesIncludesAuditSubscribeDenyFor*` at `internal/access/policy/seed_test.go:240-268`) |

## Testing approach

| Layer | File | What it tests |
|---|---|---|
| Unit | `internal/totp/service_test.go` | Service methods, error branches, lockout/replay logic, result-struct metadata population. Mocks: `Repository`, `kek.Provider`, `Clock` (in-tree `FakeClock`). No `AuditPublisher` mock under R5 Option Y — Service does not emit. Table-driven where useful. |
| Repo integration | `internal/totp/repo_integration_test.go` (build tag `integration`) | Repo methods against real PG via testcontainers. INV-A2 specifically requires this layer. |
| CLI | `cmd/holomush/cmd_admin_totp_test.go` | Cobra command tree wiring, flag parsing, prompt handling. Mocks `totp.Service`. |
| E2E | `test/integration/totp_e2e_test.go` (build tag `integration`, Ginkgo/Gomega) | Bootstrap → enroll → verify → recover → re-enroll cycle. Real PG, real KEK file. No audit-publisher assertions for sub-epic A (CLIs emit nothing under Option Y); audit-table assertions for these flows live in sub-epic D's E2E once D ships. |

`task pr-prep` runs `task test` (unit + CLI) and `task test:int`
(repo integration + E2E). Both MUST pass green for the PR.

## Failure modes

| Failure | Detection | Behavior |
|---|---|---|
| KEK unavailable at enroll time | `kek.Wrap` returns error | Enroll fails; no row inserted; CLI prints error and exits non-zero. |
| KEK unavailable at verify time | `kek.Unwrap` returns error | Verify returns wrapped error; `OperatorAuthProvider` (sub-epic D) treats as `ErrInternal`; break-glass refused with operator-friendly message. |
| `wrap_key_id` not found in current KEK provider (KEK rotation drift) | `kek.Unwrap(wrapped, wrap_key_id)` returns `ErrKEKNotFound` | Verify fails. Operator must re-enroll TOTP after a KEK rotation that retired the wrap key — or sub-epic D's KEK-rotation work re-wraps existing TOTP secrets. v1: re-enroll. |
| PG unavailable | Repository methods return error | All TOTP service calls fail; CLI prints error and exits non-zero. |
| `Repository.BootstrapEnrollAtomic` insert error mid-txn | `INSERT player_totp` or `INSERT player_totp_recovery_codes` returns error | Containing PG transaction rolls back. No `crypto_bootstrap_state` row, no `player_totp` row committed. INV-A15. |
| Host-shell CLI: no audit event emitted | sub-epic A's CLIs run as standalone processes without eventbus access (see §"Audit events emitted" / "Emission ownership"). | **Intentional gap.** Operators reading `events_audit` ↔ `crypto_bootstrap_state` see "row without event" → host-shell CLI invocation; trust path. Master spec §1 already concedes root-on-host bypasses audit at the architectural level. |
| Server-side caller: post-`Service` audit publish error | sub-epic D's caller invokes `audit.Publish` after `Service.BootstrapEnroll` returns; publish fails | PG state stands (txn already committed). Caller logs warn; operationally treated as the "row without event" surface — same as host-shell-CLI gap. |
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
- **`internal/eventbus.Publisher`:** **NOT consumed by sub-epic A** under R5 Option Y. Sub-epic A's `Service` is PG-only and does not publish audit events. The interface is consumed by sub-epic D's `OperatorAuthProvider` and other server-side callers, where eventbus access exists. See §"Audit events emitted" / "Emission ownership and the host-shell-CLI gap".

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
