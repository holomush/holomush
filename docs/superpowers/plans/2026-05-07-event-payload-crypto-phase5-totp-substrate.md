<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Event Payload Cryptography — Phase 5 Sub-epic A: TOTP Substrate Implementation Plan (v2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the per-player TOTP substrate (Service + Repository + admin CLIs + ABAC seed denies) that sub-epic D's `OperatorAuthProvider` and future server-side callers consume. PG-only side effects; **no audit emission** (per R5 Option Y — see spec §"Audit events emitted" / "Emission ownership and the host-shell-CLI gap").

**Architecture:** New `internal/totp/` package with a `Service` over a Postgres `Repository`. Repository follows the existing `internal/world/postgres/transactor.go` `Transactor.InTransaction(ctx, fn)` pattern with `txKey{}` context-stash + `execerFromCtx` fallback. TOTP secrets KEK-wrapped via existing `internal/eventbus/crypto/kek.Provider`; recovery codes Argon2id-hashed via `internal/auth.PasswordHasher`. CLIs in `cmd/holomush/cmd_admin*.go` running standalone (DATABASE_URL + KEK file). New ABAC forbid seeds in `internal/access/policy/seed.go` for INV-A16.

**Tech Stack:** Go 1.22+, `github.com/pquerna/otp` (RFC 6238, net-new), `github.com/spf13/cobra` (existing), pgxpool (existing), Ginkgo/Gomega for E2E (existing), in-tree `Clock` interface (no third-party clock dep).

**Tracking bead:** `holomush-jxo8.3`. Spec: `docs/superpowers/specs/2026-05-07-event-payload-crypto-phase5-totp-substrate-design.md` (READY at design-reviewer R5, 2026-05-07).

---

## Spec coverage

| Spec section | Tasks |
|---|---|
| §"Architecture" — package layout, migration | T1, T2 |
| §"Go API surface" — Service, result structs, Clock | T3 (types + clock), T6 (Service skeleton) |
| §"Verify mechanics" — replay + lockout + skew | T9 |
| §"CLI commands" — bootstrap-enroll / enroll / recover | T11–T14 |
| §"Audit events emitted" — subjects reserved (no emission in A) | T4 (helpers reserved for D), T16 (ABAC seeds) |
| §"Bootstrap closure mechanism" — PG-only atomicity | T5 (Repository), T7 (Service.BootstrapEnroll) |
| §"Threat-model coverage" | T7 (KEK-wrap), T9 (lockout, replay) |
| §"Failure modes" | T2 (FK CASCADE), T5 (txn rollback), T9 (concurrent lockout) |
| §"Dependencies" — kek docstring generalization | T15 |
| INV-A1, A2 | T7 (mock + real-PG) |
| INV-A3, A4, A5, A12, A14 | T9 |
| INV-A6, A7, A8 | T10 |
| INV-A9, A11, A13, A15 | T7, T2 (CASCADE), T5 |
| INV-A10 (retired) | T6/T7/T9/T10 (result-struct metadata tests) |
| INV-A16 | T16 |

---

## File structure

```text
internal/totp/                     # NEW
    clock.go                        # Clock + RealClock + FakeClock
    errors.go                       # Typed errors via oops.Code
    types.go                        # Service interface, Repository interface, result structs, ClearReason
    provisioning.go                 # generateSecret, buildProvisioningURI, generateRecoveryCodes
    audit.go                        # Audit subject builders + event-type consts (RESERVED for D)
    service.go                      # Service implementation
    repo.go                         # Postgres Repository (Transactor + execerFromCtx pattern)

    clock_test.go
    errors_test.go
    provisioning_test.go
    audit_test.go
    service_isenrolled_test.go
    service_bootstrap_test.go       # INV-A1, A9, A11, A13, A15
    service_enroll_test.go
    service_verify_test.go          # INV-A3, A4, A5, A12, A14
    service_recovery_test.go        # INV-A6, A7, A8
    repo_integration_test.go        # build tag: integration; INV-A2 (real PG)

internal/store/migrations/
    000019_create_player_totp.up.sql      # NEW — three tables
    000019_create_player_totp.down.sql

internal/access/policy/
    seed.go                         # MODIFY — add 2 forbid seeds for INV-A16
    seed_test.go                    # MODIFY — add 2 seed-existence tests

cmd/holomush/
    cmd_admin.go                    # NEW — `admin` parent
    cmd_admin_totp.go               # NEW — `admin totp` parent + 3 subcommand handlers
    cmd_admin_totp_deps.go          # NEW — bootstrap helper that builds Service from env+config
    cmd_admin_totp_test.go          # NEW — cobra-tree tests
    root.go                         # MODIFY — register NewAdminCmd

test/integration/
    totp_e2e_test.go                # NEW — Ginkgo lifecycle (PG + KEK; no eventbus)

internal/eventbus/crypto/kek/
    provider.go                     # MODIFY — generalize package + Wrap docstring (per spec §Dependencies)

go.mod                              # MODIFY — add github.com/pquerna/otp
```

---

## Conventions

- TDD: every task RED → GREEN → COMMIT.
- Use `task` for build/test/lint (`task test`, `task lint`, `task fmt`, `task test:int`, `task pr-prep`). Never call `go test`/`golangci-lint` directly.
- Probe over rg for semantic lookups; rg only as fallback.
- License headers: `.go`, `.sh`, `.proto`, `.sql` files MUST include `// SPDX-License-Identifier: Apache-2.0` (lefthook auto-applies on commit).
- ULIDs: entity PKs use `idgen.New()`; recovery code IDs are entity PKs.
- Errors: wrap with `oops.Code("...").Wrap(err)` at API boundaries.
- Random: `crypto/rand` only.
- Constant-time compare: `crypto/subtle.ConstantTimeCompare`.
- jj: `jj describe -m "..."` to set message; `jj new` to start a fresh commit on top.
- Migrations: `IF NOT EXISTS`, no triggers/functions.
- **Mockery (v3, config-driven):** repo uses mockery v3 with `.mockery.yaml` at the repo root (verified). To generate a mock for a new interface: (1) add a `packages:` entry pointing at the interface's package and listing the interface name(s); (2) run `task mocks:generate`. Generated files land at `internal/<pkg>/mocks/mock_<Iface>.go` (snake_case `mock_` prefix); generated constructor is `NewMock<Iface>(t)`. The CLI fallback `mockery --name ...` is mockery v2 syntax and **does not work** here. Pattern reference: `internal/auth/mocks/mock_PlayerRepository.go` produced by the entry at `.mockery.yaml` (existing).

---

## Task 1: Package skeleton — `Clock` + typed errors

**Files:** `go.mod` (modify), `internal/totp/clock.go` + `_test.go` (new), `internal/totp/errors.go` + `_test.go` (new)

- [ ] **1.1** Add dep: `go get github.com/pquerna/otp@latest`. Verify build: `go build ./internal/...` (note: NOT `task lint -- ./...` — scope to package once it exists, see step 1.7).

- [ ] **1.2** Write failing test `internal/totp/clock_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRealClockReturnsCurrentTime(t *testing.T) {
	c := NewRealClock()
	before := time.Now()
	got := c.Now()
	after := time.Now()
	assert.True(t, !got.Before(before) && !got.After(after))
}

func TestFakeClockReturnsAndAdvances(t *testing.T) {
	t0 := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	c := NewFakeClock(t0)
	assert.Equal(t, t0, c.Now())
	c.Advance(45 * time.Second)
	assert.Equal(t, t0.Add(45*time.Second), c.Now())
}
```

- [ ] **1.3** Run `task test -- ./internal/totp/...` → RED (package missing).

- [ ] **1.4** Implement `internal/totp/clock.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package totp provides per-player TOTP enrollment, verification, and
// recovery for Phase 5 break-glass auth. PG-only side effects; audit
// emission is the calling layer's responsibility per spec §"Audit events
// emitted" / "Emission ownership and the host-shell-CLI gap".
package totp

import "time"

// Clock abstracts time.Now for testability. Avoids a third-party clock
// dependency to keep the package's go.mod surface small.
type Clock interface {
	Now() time.Time
}

func NewRealClock() Clock { return realClock{} }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// FakeClock is a test double — NOT goroutine-safe.
type FakeClock struct{ t time.Time }

func NewFakeClock(start time.Time) *FakeClock { return &FakeClock{t: start} }
func (c *FakeClock) Now() time.Time           { return c.t }
func (c *FakeClock) Advance(d time.Duration)  { c.t = c.t.Add(d) }
```

- [ ] **1.5** Run `task test -- ./internal/totp/...` → GREEN.

- [ ] **1.6** Write failing test `internal/totp/errors_test.go` for the typed errors below; assert codes via `errutil.AssertErrorCode`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp_test

import (
	"testing"
	"time"

	"github.com/holomush/holomush/internal/errutil"
	"github.com/holomush/holomush/internal/totp"
)

func TestErrorCodesPresent(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{totp.ErrBootstrapAlreadyConsumed, "TOTP_BOOTSTRAP_CONSUMED"},
		{totp.ErrAlreadyEnrolled, "TOTP_ALREADY_ENROLLED"},
		{totp.ErrNotEnrolled, "TOTP_NOT_ENROLLED"},
		{totp.ErrInvalidRecoveryCode, "TOTP_INVALID_RECOVERY_CODE"},
	}
	for _, tc := range cases {
		errutil.AssertErrorCode(t, tc.err, tc.code)
	}
}

func TestNewErrTOTPLockedCarriesUntil(t *testing.T) {
	until := time.Date(2026, 5, 7, 12, 30, 0, 0, time.UTC)
	err := totp.NewErrTOTPLocked(until)
	errutil.AssertErrorCode(t, err, "TOTP_LOCKED")
	errutil.AssertErrorContext(t, err, "until", until)
}
```

- [ ] **1.7** Implement `internal/totp/errors.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp

import (
	"time"

	"github.com/samber/oops"
)

var (
	ErrBootstrapAlreadyConsumed = oops.Code("TOTP_BOOTSTRAP_CONSUMED").
		Errorf("TOTP bootstrap already consumed")
	ErrAlreadyEnrolled = oops.Code("TOTP_ALREADY_ENROLLED").
		Errorf("TOTP already enrolled for this player")
	ErrNotEnrolled = oops.Code("TOTP_NOT_ENROLLED").
		Errorf("TOTP not enrolled for this player")
	ErrInvalidRecoveryCode = oops.Code("TOTP_INVALID_RECOVERY_CODE").
		Errorf("invalid recovery attempt")
)

func NewErrTOTPLocked(until time.Time) error {
	return oops.Code("TOTP_LOCKED").
		With("until", until).
		Errorf("TOTP verification locked until %s", until.Format(time.RFC3339))
}
```

(Note: `ErrInvalidCode` and `ErrCodeReuse` from spec are surfaced as `VerifyOutcome` enum values on `VerifyResult`, not as sentinel errors — see T3 types.go and T9.)

- [ ] **1.8** Run `task test -- ./internal/totp/...` → GREEN. Run `task lint -- ./internal/totp/...` → no findings.

- [ ] **1.9** Commit: `jj describe -m "feat(totp): package skeleton — Clock + typed errors (holomush-jxo8.3)"; jj new -m "wip: T2 migration"`.

---

## Task 2: Migration 000019 — three tables

**Files:** `internal/store/migrations/000019_create_player_totp.{up,down}.sql` (new)

- [ ] **2.1** Verify next migration number: `ls internal/store/migrations/ | tail -3` → confirm 000018 is highest.

- [ ] **2.2** Write `000019_create_player_totp.up.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

CREATE TABLE IF NOT EXISTS player_totp (
    player_id        TEXT PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
    wrapped_secret   BYTEA NOT NULL,
    wrap_key_id      TEXT NOT NULL,
    enrolled_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_verified_at TIMESTAMPTZ,
    last_used_step   BIGINT,
    failed_attempts  INTEGER NOT NULL DEFAULT 0,
    locked_until     TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS player_totp_recovery_codes (
    id           TEXT PRIMARY KEY,
    player_id    TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    code_hash    TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    consumed_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_pt_recovery_player_active
    ON player_totp_recovery_codes (player_id) WHERE consumed_at IS NULL;

CREATE TABLE IF NOT EXISTS crypto_bootstrap_state (
    key                     TEXT PRIMARY KEY,
    consumed_at             TIMESTAMPTZ NOT NULL,
    consumed_by_player_id   TEXT NOT NULL REFERENCES players(id)
);
```

- [ ] **2.3** Write `000019_create_player_totp.down.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP TABLE IF EXISTS crypto_bootstrap_state;
DROP TABLE IF EXISTS player_totp_recovery_codes;
DROP TABLE IF EXISTS player_totp;
```

- [ ] **2.4** Run `task test:int -- -run TestAutoMigrate ./cmd/holomush/...` (existing migration test auto-discovers new files) → GREEN.

- [ ] **2.5** Commit: `jj describe -m "feat(totp): migration 000019 — player_totp + recovery_codes + bootstrap_state (holomush-jxo8.3)"; jj new -m "wip: T3 types + provisioning"`.

---

## Task 3: Types (Service / Repository / result structs) + provisioning

**Files:** `internal/totp/types.go` (new), `internal/totp/provisioning.go` + `_test.go` (new)

- [ ] **3.1** Write `internal/totp/types.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
)

// Service: per-player TOTP enrollment, verification, recovery. PG-only.
// Audit emission is the caller's responsibility (R5 Option Y).
type Service interface {
	BootstrapEnroll(ctx context.Context, playerID ulid.ULID) (BootstrapResult, error)
	Enroll(ctx context.Context, playerID ulid.ULID) (EnrollResult, error)
	Verify(ctx context.Context, playerID ulid.ULID, code string) (VerifyResult, error)
	IsEnrolled(ctx context.Context, playerID ulid.ULID) (bool, error)
	ConsumeRecoveryCode(ctx context.Context, playerID ulid.ULID, code string) (ConsumeRecoveryResult, error)
	ClearTOTP(ctx context.Context, playerID ulid.ULID, clearedBy ClearReason) (ClearResult, error)
}

type Enrollment struct {
	Secret          string   // base32
	ProvisioningURI string   // otpauth://totp/holomush-<game>:<player>?...
	RecoveryCodes   []string // 10 codes, "xxxx-xxxx-xxxx-xxxx"; printed once
}

type BootstrapResult struct {
	Enrollment         Enrollment
	AuditConsumedAt    time.Time
	AuditPlayerID      ulid.ULID
	BootstrapKey       string
}

type EnrollResult struct {
	Enrollment       Enrollment
	AuditEnrolledAt  time.Time
	AuditPlayerID    ulid.ULID
}

type VerifyOutcome int

const (
	OutcomeOK VerifyOutcome = iota
	OutcomeNotEnrolled
	OutcomeLocked
	OutcomeInvalidCode
	OutcomeCodeReuse
)

type VerifyResult struct {
	Outcome           VerifyOutcome
	LockedUntil       *time.Time // set when Outcome == OutcomeLocked OR a lockout transition just fired
	LockoutTransition bool       // true iff this Verify call transitioned NULL→locked
	AuditAt           time.Time  // = clock.Now()
}

type ConsumeRecoveryResult struct {
	RecoveryCodeID  ulid.ULID
	AuditConsumedAt time.Time
	AuditPlayerID   ulid.ULID
}

type ClearResult struct {
	ClearedBy      ClearReason
	AuditClearedAt time.Time
	AuditPlayerID  ulid.ULID
	WasEnrolled    bool // false if call was a no-op; callers should skip emit
}

type ClearReason string

const (
	ClearReasonRecoveryCode ClearReason = "recovery_code"
	ClearReasonAdminReset   ClearReason = "admin_reset"
)

// Repository: PG persistence. Methods take ctx; if ctx carries an active
// pgx.Tx (via internal/totp.txKey, set by Transactor.InTransaction),
// methods participate in that txn. Otherwise they use the pool.
// Pattern matches internal/world/postgres/transactor.go.
type Repository interface {
	BootstrapClaim(ctx context.Context, key, playerID string, at time.Time) (claimed bool, err error)
	BootstrapEnrollAtomic(ctx context.Context, key, playerID string, rec EnrollmentRecord) error
	PlayerExists(ctx context.Context, playerID string) (bool, error)
	PlayerIDFromUsername(ctx context.Context, username string) (string, error)
	IsEnrolled(ctx context.Context, playerID string) (bool, error)
	InsertEnrollment(ctx context.Context, rec EnrollmentRecord) error
	LoadEnrollment(ctx context.Context, playerID string) (VerifyState, error)
	IncrementFailedAttempts(ctx context.Context, playerID string, lockoutThreshold int, lockoutDuration time.Duration, now time.Time) (postState VerifyState, err error)
	MarkVerified(ctx context.Context, playerID string, step int64, at time.Time) error
	ConsumeRecoveryCode(ctx context.Context, playerID, rawCode string, hasher RecoveryCodeHasher, at time.Time) (consumedID ulid.ULID, err error)
	ClearEnrollment(ctx context.Context, playerID string) (wasEnrolled bool, err error)
	InTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

type EnrollmentRecord struct {
	PlayerID       string
	WrappedSecret  []byte
	WrapKeyID      string
	EnrolledAt     time.Time
	RecoveryCodes  []HashedRecoveryCode
}

type HashedRecoveryCode struct {
	ID        ulid.ULID
	CodeHash  string
	CreatedAt time.Time
}

type VerifyState struct {
	PlayerID       string
	WrappedSecret  []byte
	WrapKeyID      string
	LastUsedStep   *int64
	FailedAttempts int
	LockedUntil    *time.Time
}

// RecoveryCodeHasher: subset of internal/auth.PasswordHasher used at
// verify time. Service uses the full PasswordHasher at enroll time.
type RecoveryCodeHasher interface {
	Verify(rawCode, encodedHash string) (bool, error)
}
```

- [ ] **3.2** Write failing test `internal/totp/provisioning_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp

import (
	"encoding/base32"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateSecretIs32CharBase32(t *testing.T) {
	s, err := generateSecret()
	require.NoError(t, err)
	assert.Len(t, s, 32)
	_, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
	require.NoError(t, err)
}

func TestGenerateSecretIsRandom(t *testing.T) {
	a, _ := generateSecret()
	b, _ := generateSecret()
	assert.NotEqual(t, a, b)
}

func TestBuildProvisioningURI(t *testing.T) {
	u, err := buildProvisioningURI("alice", "default", "JBSWY3DPEHPK3PXP")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(u, "otpauth://totp/"))
	assert.Contains(t, u, "issuer=holomush")
	assert.Contains(t, u, "secret=JBSWY3DPEHPK3PXP")
	assert.Contains(t, u, "alice")
}

func TestGenerateRecoveryCodes(t *testing.T) {
	codes, err := generateRecoveryCodes(10)
	require.NoError(t, err)
	require.Len(t, codes, 10)
	for _, c := range codes {
		assert.Len(t, c, 19) // 16 hex + 3 hyphens
		parts := strings.Split(c, "-")
		assert.Len(t, parts, 4)
		for _, p := range parts {
			assert.Len(t, p, 4)
		}
	}
	// uniqueness
	seen := map[string]bool{}
	for _, c := range codes {
		assert.False(t, seen[c])
		seen[c] = true
	}
}
```

- [ ] **3.3** Implement `internal/totp/provisioning.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"net/url"

	"github.com/samber/oops"
)

const (
	secretBytes               = 20 // 160 bits, RFC 4226 §4
	recoveryCodeBytes         = 8  // 64 bits, formatted xxxx-xxxx-xxxx-xxxx
	recoveryCodesPerEnrollment = 10
)

func generateSecret() (string, error) {
	buf := make([]byte, secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", oops.Code("TOTP_SECRET_GEN_FAILED").Wrap(err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

func buildProvisioningURI(username, gameID, secret string) (string, error) {
	if username == "" || gameID == "" || secret == "" {
		return "", oops.Code("TOTP_URI_INVALID_INPUT").
			Errorf("username, gameID, and secret all required")
	}
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", "holomush")
	account := fmt.Sprintf("holomush-%s:%s", gameID, username)
	return fmt.Sprintf("otpauth://totp/%s?%s", url.PathEscape(account), q.Encode()), nil
}

func generateRecoveryCodes(n int) ([]string, error) {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		buf := make([]byte, recoveryCodeBytes)
		if _, err := rand.Read(buf); err != nil {
			return nil, oops.Code("TOTP_RECOVERY_GEN_FAILED").Wrap(err)
		}
		raw := hex.EncodeToString(buf)
		out[i] = fmt.Sprintf("%s-%s-%s-%s", raw[0:4], raw[4:8], raw[8:12], raw[12:16])
	}
	return out, nil
}
```

- [ ] **3.4** Run `task test -- ./internal/totp/...` → GREEN. Run `task lint -- ./internal/totp/...` → clean.

- [ ] **3.5** Commit: `jj describe -m "feat(totp): types + provisioning helpers (holomush-jxo8.3)"; jj new -m "wip: T4 audit subjects"`.

---

## Task 4: Audit subject helpers (RESERVED — A doesn't emit)

**Files:** `internal/totp/audit.go` + `_test.go` (new)

Per spec §"Audit events emitted" / "Emission ownership": Sub-epic A reserves subjects + payload structs but does NOT emit. Helpers exist so server-side callers (sub-epic D) can construct events from `Service` result structs.

- [ ] **4.1** Write failing test `internal/totp/audit_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSubjectBuilders(t *testing.T) {
	assert.Equal(t, "events.default.system.crypto_totp.bootstrap.completed",
		SubjectBootstrapCompleted("default"))
	assert.Equal(t, "events.default.system.crypto_totp.01HZ.enrolled",
		SubjectEnrolled("default", "01HZ"))
	assert.Equal(t, "events.default.system.crypto_totp.01HZ.cleared",
		SubjectCleared("default", "01HZ"))
	assert.Equal(t, "events.default.system.crypto_totp.01HZ.recovery_consumed",
		SubjectRecoveryConsumed("default", "01HZ"))
	assert.Equal(t, "events.default.system.crypto_totp.01HZ.locked",
		SubjectLocked("default", "01HZ"))
}

func TestEventTypeConstants(t *testing.T) {
	assert.Equal(t, "crypto.totp_bootstrap_completed", EventTypeBootstrapCompleted)
	assert.Equal(t, "crypto.totp_enrolled", EventTypeEnrolled)
	assert.Equal(t, "crypto.totp_cleared", EventTypeCleared)
	assert.Equal(t, "crypto.totp_recovery_code_consumed", EventTypeRecoveryConsumed)
	assert.Equal(t, "crypto.totp_locked", EventTypeLocked)
}
```

- [ ] **4.2** Implement `internal/totp/audit.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Audit subject builders + event-type constants. RESERVED for use by
// callers with eventbus access (sub-epic D's OperatorAuthProvider; future
// server-side flows). Sub-epic A does NOT emit — see spec §"Audit events
// emitted" / "Emission ownership and the host-shell-CLI gap".

package totp

import (
	"fmt"
	"time"
)

const (
	EventTypeBootstrapCompleted = "crypto.totp_bootstrap_completed"
	EventTypeEnrolled           = "crypto.totp_enrolled"
	EventTypeCleared            = "crypto.totp_cleared"
	EventTypeRecoveryConsumed   = "crypto.totp_recovery_code_consumed"
	EventTypeLocked             = "crypto.totp_locked"
)

func SubjectBootstrapCompleted(gameID string) string {
	return fmt.Sprintf("events.%s.system.crypto_totp.bootstrap.completed", gameID)
}

func SubjectEnrolled(gameID, playerID string) string {
	return fmt.Sprintf("events.%s.system.crypto_totp.%s.enrolled", gameID, playerID)
}

func SubjectCleared(gameID, playerID string) string {
	return fmt.Sprintf("events.%s.system.crypto_totp.%s.cleared", gameID, playerID)
}

func SubjectRecoveryConsumed(gameID, playerID string) string {
	return fmt.Sprintf("events.%s.system.crypto_totp.%s.recovery_consumed", gameID, playerID)
}

func SubjectLocked(gameID, playerID string) string {
	return fmt.Sprintf("events.%s.system.crypto_totp.%s.locked", gameID, playerID)
}

// Payload structs (JSON) for caller emission. Field names match spec
// §"Audit events emitted" payload column.
type BootstrapCompletedPayload struct {
	ConsumedAt         time.Time `json:"consumed_at"`
	ConsumedByPlayerID string    `json:"consumed_by_player_id"`
	BootstrapKey       string    `json:"bootstrap_key"`
}

type EnrolledPayload struct {
	PlayerID            string    `json:"player_id"`
	EnrolledAt          time.Time `json:"enrolled_at"`
	RecoveryCodesIssued int       `json:"recovery_codes_issued"`
}

type ClearedPayload struct {
	PlayerID  string      `json:"player_id"`
	ClearedAt time.Time   `json:"cleared_at"`
	ClearedBy ClearReason `json:"cleared_by"`
}

type RecoveryConsumedPayload struct {
	PlayerID       string    `json:"player_id"`
	ConsumedAt     time.Time `json:"consumed_at"`
	RecoveryCodeID string    `json:"recovery_code_id"`
}

type LockedPayload struct {
	PlayerID    string    `json:"player_id"`
	LockedAt    time.Time `json:"locked_at"`
	LockedUntil time.Time `json:"locked_until"`
	Reason      string    `json:"reason"` // "brute_force_protection"
}
```

- [ ] **4.3** Run tests/lint → GREEN/clean. Commit: `jj describe -m "feat(totp): audit subject helpers + payload structs (RESERVED for sub-epic D consumers; holomush-jxo8.3)"; jj new -m "wip: T5 repository"`.

---

## Task 5: Postgres Repository (Transactor pattern + `BootstrapEnrollAtomic`)

**Files:** `internal/totp/repo.go` (new), `internal/totp/repo_integration_test.go` (new, build tag `integration`)

Pattern: follow `internal/world/postgres/transactor.go` and `internal/world/postgres/helpers.go` — repo methods take `ctx`, use `execerFromCtx(ctx, r.pool)` to get either an active txn (stashed by `Transactor.InTransaction`) or the pool. Define a local `txKey{}` and helper functions in this package.

- [ ] **5.1** Write failing integration test for INV-A2 (concurrent BootstrapClaim atomicity, real PG):

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/totp"
)

// TestRepoBootstrapClaimConcurrentExactlyOneSucceeds: INV-A2.
func TestRepoBootstrapClaimConcurrentExactlyOneSucceeds(t *testing.T) {
	pool := newTestPool(t) // helper that spins up testcontainer + applies migrations
	repo := totp.NewRepository(pool)
	ctx := context.Background()

	const N = 8
	players := make([]string, N)
	for i := range players {
		players[i] = ulid.Make().String()
		insertPlayer(t, pool, players[i], "u"+players[i][:4])
	}

	now := time.Now().UTC()
	var (
		wg        sync.WaitGroup
		successes int
		mu        sync.Mutex
	)
	wg.Add(N)
	for i := 0; i < N; i++ {
		pid := players[i]
		go func() {
			defer wg.Done()
			ok, err := repo.BootstrapClaim(ctx, "totp_v1", pid, now)
			require.NoError(t, err)
			if ok {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, 1, successes, "exactly one BootstrapClaim must win (INV-A2)")
}
```

(Helpers `newTestPool` and `insertPlayer` follow the patterns in existing `internal/auth/postgres/*_integration_test.go`. The plan-author MUST locate the existing helpers — likely via probe `mcp__probe__search_code` for `func newTestPool` or look at `internal/world/postgres/postgres_test.go`. If they don't exist as exact symbols, replicate the testcontainer-bootstrap pattern in the test file's `TestMain` / suite setup.)

- [ ] **5.2** Run `task test:int -- -run TestRepoBootstrapClaim ./internal/totp/...` → RED (Repository undefined).

- [ ] **5.3** Implement `internal/totp/repo.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// txKey is the context key for an active pgx.Tx stored by InTransaction.
// Pattern follows internal/world/postgres/helpers.go.
type txKey struct{}

func txFromContext(ctx context.Context) pgx.Tx {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	if !ok {
		return nil
	}
	return tx
}

// querier is what both *pgxpool.Pool and pgx.Tx provide.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconnCommandTagShim, error)
}

// pgconnCommandTagShim avoids importing pgconn here; both pool and tx
// return a pgconn.CommandTag. We don't introspect it, so an `any` shim
// is fine — but for cleanliness, define the interface satisfied by both
// pool.Exec and tx.Exec via go's structural typing.
// (Implementation note: the actual `Exec` returns pgconn.CommandTag, but
// the repo never inspects it; we discard the return.)
type pgconnCommandTagShim = any

func querierFromCtx(ctx context.Context, pool *pgxpool.Pool) querier {
	if tx := txFromContext(ctx); tx != nil {
		return txQuerier{tx: tx}
	}
	return poolQuerier{pool: pool}
}

type txQuerier struct{ tx pgx.Tx }

func (q txQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return q.tx.QueryRow(ctx, sql, args...)
}
func (q txQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return q.tx.Query(ctx, sql, args...)
}
func (q txQuerier) Exec(ctx context.Context, sql string, args ...any) (any, error) {
	return q.tx.Exec(ctx, sql, args...)
}

type poolQuerier struct{ pool *pgxpool.Pool }

func (q poolQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return q.pool.QueryRow(ctx, sql, args...)
}
func (q poolQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return q.pool.Query(ctx, sql, args...)
}
func (q poolQuerier) Exec(ctx context.Context, sql string, args ...any) (any, error) {
	return q.pool.Exec(ctx, sql, args...)
}

type repo struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) Repository { return &repo{pool: pool} }

// InTransaction begins a txn, stores it on context via txKey{}, and
// runs fn. fn returning nil → COMMIT; non-nil → ROLLBACK.
// Pattern follows internal/world/postgres/transactor.go.
func (r *repo) InTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return oops.Code("TOTP_TX_BEGIN_FAILED").Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	txCtx := context.WithValue(ctx, txKey{}, tx)
	if err := fn(txCtx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return oops.Code("TOTP_TX_COMMIT_FAILED").Wrap(err)
	}
	return nil
}

func (r *repo) BootstrapClaim(ctx context.Context, key, playerID string, at time.Time) (bool, error) {
	const q = `INSERT INTO crypto_bootstrap_state (key, consumed_at, consumed_by_player_id)
	           VALUES ($1, $2, $3) ON CONFLICT (key) DO NOTHING RETURNING key`
	var got string
	err := querierFromCtx(ctx, r.pool).QueryRow(ctx, q, key, at, playerID).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, oops.Code("TOTP_REPO_BOOTSTRAP_CLAIM").Wrap(err)
	}
	return true, nil
}

// BootstrapEnrollAtomic wraps BootstrapClaim + InsertEnrollment in ONE
// PG transaction (per spec §"Bootstrap closure mechanism" / "Atomicity").
// Caller does NOT need to invoke InTransaction; this method opens its
// own. Returns ErrBootstrapAlreadyConsumed if the claim fails.
func (r *repo) BootstrapEnrollAtomic(ctx context.Context, key, playerID string, rec EnrollmentRecord) error {
	return r.InTransaction(ctx, func(txCtx context.Context) error {
		claimed, err := r.BootstrapClaim(txCtx, key, playerID, rec.EnrolledAt)
		if err != nil {
			return err
		}
		if !claimed {
			return ErrBootstrapAlreadyConsumed
		}
		return r.InsertEnrollment(txCtx, rec)
	})
}

func (r *repo) PlayerExists(ctx context.Context, playerID string) (bool, error) {
	var x int
	err := querierFromCtx(ctx, r.pool).QueryRow(ctx,
		`SELECT 1 FROM players WHERE id = $1`, playerID).Scan(&x)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, oops.Code("TOTP_REPO_PLAYER_EXISTS").Wrap(err)
	}
	return true, nil
}

func (r *repo) PlayerIDFromUsername(ctx context.Context, username string) (string, error) {
	var id string
	err := querierFromCtx(ctx, r.pool).QueryRow(ctx,
		`SELECT id FROM players WHERE username = $1`, username).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", oops.Code("TOTP_REPO_PLAYER_NOT_FOUND").
			With("username", username).Errorf("player not found")
	}
	if err != nil {
		return "", oops.Code("TOTP_REPO_PLAYER_LOOKUP").Wrap(err)
	}
	return id, nil
}

func (r *repo) IsEnrolled(ctx context.Context, playerID string) (bool, error) {
	var x int
	err := querierFromCtx(ctx, r.pool).QueryRow(ctx,
		`SELECT 1 FROM player_totp WHERE player_id = $1`, playerID).Scan(&x)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, oops.Code("TOTP_REPO_IS_ENROLLED").Wrap(err)
	}
	return true, nil
}

func (r *repo) InsertEnrollment(ctx context.Context, e EnrollmentRecord) error {
	q := querierFromCtx(ctx, r.pool)
	if _, err := q.Exec(ctx,
		`INSERT INTO player_totp (player_id, wrapped_secret, wrap_key_id, enrolled_at)
		 VALUES ($1, $2, $3, $4)`,
		e.PlayerID, e.WrappedSecret, e.WrapKeyID, e.EnrolledAt,
	); err != nil {
		return oops.Code("TOTP_REPO_INSERT_TOTP").Wrap(err)
	}
	const insCode = `INSERT INTO player_totp_recovery_codes (id, player_id, code_hash, created_at)
	                 VALUES ($1, $2, $3, $4)`
	for _, c := range e.RecoveryCodes {
		if _, err := q.Exec(ctx, insCode, c.ID.String(), e.PlayerID, c.CodeHash, c.CreatedAt); err != nil {
			return oops.Code("TOTP_REPO_INSERT_RECOVERY_CODE").Wrap(err)
		}
	}
	return nil
}

// LoadEnrollment uses SELECT FOR UPDATE if invoked inside a txn (caller
// wrapped via InTransaction). Outside a txn, FOR UPDATE has no lasting
// effect; callers expecting concurrency safety MUST wrap.
func (r *repo) LoadEnrollment(ctx context.Context, playerID string) (VerifyState, error) {
	var s VerifyState
	s.PlayerID = playerID
	err := querierFromCtx(ctx, r.pool).QueryRow(ctx,
		`SELECT wrapped_secret, wrap_key_id, last_used_step, failed_attempts, locked_until
		 FROM player_totp WHERE player_id = $1 FOR UPDATE`, playerID,
	).Scan(&s.WrappedSecret, &s.WrapKeyID, &s.LastUsedStep, &s.FailedAttempts, &s.LockedUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return VerifyState{}, ErrNotEnrolled
	}
	if err != nil {
		return VerifyState{}, oops.Code("TOTP_REPO_LOAD_ENROLLMENT").Wrap(err)
	}
	return s, nil
}

func (r *repo) IncrementFailedAttempts(
	ctx context.Context, playerID string,
	threshold int, lockoutDuration time.Duration, now time.Time,
) (VerifyState, error) {
	const q = `
		UPDATE player_totp
		SET failed_attempts = failed_attempts + 1,
		    locked_until    = CASE
		      WHEN failed_attempts + 1 >= $2 THEN $3 + ($4 || ' microseconds')::INTERVAL
		      ELSE locked_until
		    END
		WHERE player_id = $1
		RETURNING wrapped_secret, wrap_key_id, last_used_step, failed_attempts, locked_until`
	var s VerifyState
	s.PlayerID = playerID
	err := querierFromCtx(ctx, r.pool).QueryRow(ctx, q,
		playerID, threshold, now, lockoutDuration.Microseconds(),
	).Scan(&s.WrappedSecret, &s.WrapKeyID, &s.LastUsedStep, &s.FailedAttempts, &s.LockedUntil)
	if err != nil {
		return VerifyState{}, oops.Code("TOTP_REPO_INCREMENT_FAILED").Wrap(err)
	}
	return s, nil
}

func (r *repo) MarkVerified(ctx context.Context, playerID string, step int64, at time.Time) error {
	_, err := querierFromCtx(ctx, r.pool).Exec(ctx,
		`UPDATE player_totp SET last_used_step = $2, last_verified_at = $3,
		   failed_attempts = 0, locked_until = NULL
		 WHERE player_id = $1`, playerID, step, at,
	)
	if err != nil {
		return oops.Code("TOTP_REPO_MARK_VERIFIED").Wrap(err)
	}
	return nil
}

func (r *repo) ConsumeRecoveryCode(
	ctx context.Context, playerID, rawCode string, hasher RecoveryCodeHasher, at time.Time,
) (ulid.ULID, error) {
	var consumedID ulid.ULID
	err := r.InTransaction(ctx, func(txCtx context.Context) error {
		rows, qErr := querierFromCtx(txCtx, r.pool).Query(txCtx,
			`SELECT id, code_hash FROM player_totp_recovery_codes
			 WHERE player_id = $1 AND consumed_at IS NULL FOR UPDATE`, playerID)
		if qErr != nil {
			return oops.Code("TOTP_REPO_RECOVERY_SCAN").Wrap(qErr)
		}
		type cand struct {
			id   string
			hash string
		}
		var cands []cand
		for rows.Next() {
			var c cand
			if err := rows.Scan(&c.id, &c.hash); err != nil {
				rows.Close()
				return oops.Code("TOTP_REPO_RECOVERY_SCAN").Wrap(err)
			}
			cands = append(cands, c)
		}
		rows.Close()
		for _, c := range cands {
			ok, vErr := hasher.Verify(rawCode, c.hash)
			if vErr != nil || !ok {
				continue // timing-safe: continue on any mismatch
			}
			if _, err := querierFromCtx(txCtx, r.pool).Exec(txCtx,
				`UPDATE player_totp_recovery_codes SET consumed_at = $2 WHERE id = $1`,
				c.id, at); err != nil {
				return oops.Code("TOTP_REPO_RECOVERY_CONSUME").Wrap(err)
			}
			parsed, perr := ulid.Parse(c.id)
			if perr != nil {
				return oops.Code("TOTP_REPO_RECOVERY_ULID_PARSE").Wrap(perr)
			}
			consumedID = parsed
			return nil
		}
		return ErrInvalidRecoveryCode
	})
	if err != nil {
		return ulid.ULID{}, err
	}
	return consumedID, nil
}

func (r *repo) ClearEnrollment(ctx context.Context, playerID string) (bool, error) {
	var wasEnrolled bool
	err := r.InTransaction(ctx, func(txCtx context.Context) error {
		// Check enrollment first (so caller can return WasEnrolled).
		x, err := r.IsEnrolled(txCtx, playerID)
		if err != nil {
			return err
		}
		wasEnrolled = x
		if _, err := querierFromCtx(txCtx, r.pool).Exec(txCtx,
			`DELETE FROM player_totp WHERE player_id = $1`, playerID); err != nil {
			return oops.Code("TOTP_REPO_CLEAR_TOTP").Wrap(err)
		}
		if _, err := querierFromCtx(txCtx, r.pool).Exec(txCtx,
			`DELETE FROM player_totp_recovery_codes WHERE player_id = $1 AND consumed_at IS NULL`,
			playerID); err != nil {
			return oops.Code("TOTP_REPO_CLEAR_RECOVERY").Wrap(err)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return wasEnrolled, nil
}
```

- [ ] **5.4** Add minimal one-shot integration tests for `PlayerExists`, `PlayerIDFromUsername`, `InsertEnrollment` round-trip, `LoadEnrollment` ErrNotEnrolled, `BootstrapEnrollAtomic` rolls back on insert error (forced by inserting duplicate ID), `MarkVerified`, `ConsumeRecoveryCode` single-use, `ClearEnrollment` returns wasEnrolled correctly. Each test is short — insert seed, call method, assert observable state.

- [ ] **5.5** Run `task test:int -- ./internal/totp/...` → all GREEN. Lint clean.

- [ ] **5.6** Commit: `jj describe -m "feat(totp): Postgres Repository (Transactor pattern, race-free bootstrap, INV-A2; holomush-jxo8.3)"; jj new -m "wip: T6 Service skeleton + IsEnrolled"`.

---

## Task 6: `Service` skeleton + `IsEnrolled` + mocks

**Files:** `internal/totp/service.go` (new partial), `internal/totp/service_isenrolled_test.go` (new)

- [ ] **6.1** Write `internal/totp/service.go` (skeleton + IsEnrolled only — methods land in T7-T10):

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/samber/oops"
)

// Config bundles tunables.
type Config struct {
	GameID            string        // required
	LockoutThreshold  int           // default 5
	LockoutDuration   time.Duration // default 15min
	SkewSteps         int           // default 1
	RecoveryCodeCount int           // default 10
}

func (c *Config) applyDefaults() {
	if c.LockoutThreshold == 0 {
		c.LockoutThreshold = 5
	}
	if c.LockoutDuration == 0 {
		c.LockoutDuration = 15 * time.Minute
	}
	if c.SkewSteps == 0 {
		c.SkewSteps = 1
	}
	if c.RecoveryCodeCount == 0 {
		c.RecoveryCodeCount = recoveryCodesPerEnrollment
	}
}

// service is the production Service implementation. NO AuditPublisher
// field — emission is the caller's responsibility (R5 Option Y).
type service struct {
	cfg              Config
	repo             Repository
	kek              kek.Provider
	clock            Clock
	verifyHasher     RecoveryCodeHasher
	enrollmentHasher auth.PasswordHasher
}

func NewService(
	cfg Config,
	repo Repository,
	kekProvider kek.Provider,
	clock Clock,
	enrollmentHasher auth.PasswordHasher,
) (Service, error) {
	if cfg.GameID == "" {
		return nil, oops.Code("TOTP_CFG_GAME_ID_REQUIRED").Errorf("Config.GameID is required")
	}
	cfg.applyDefaults()
	return &service{
		cfg:              cfg,
		repo:             repo,
		kek:              kekProvider,
		clock:            clock,
		verifyHasher:     enrollmentHasher, // same hasher serves both roles
		enrollmentHasher: enrollmentHasher,
	}, nil
}

func (s *service) IsEnrolled(ctx context.Context, playerID ulid.ULID) (bool, error) {
	return s.repo.IsEnrolled(ctx, playerID.String())
}

// Other Service methods land in T7 (BootstrapEnroll), T8 (Enroll),
// T9 (Verify), T10 (ConsumeRecoveryCode + ClearTOTP).
```

- [ ] **6.2** Generate Repository mock via mockery v3 (config-driven). Edit `.mockery.yaml`, append a packages entry (alphabetical with surrounding entries):

```yaml
  github.com/holomush/holomush/internal/totp:
    config:
      dir: "{{.InterfaceDir}}/mocks"
      outpkg: mocks
    interfaces:
      Repository:
      RecoveryCodeHasher:
```

Then run `task mocks:generate`. Verify the file `internal/totp/mocks/mock_Repository.go` (snake_case `mock_` prefix) exists and exports `NewMockRepository(t)`. Pattern reference: `internal/auth/mocks/mock_PlayerRepository.go` shows the file shape this generates.

- [ ] **6.3** Write `internal/totp/service_isenrolled_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/totp"
	"github.com/holomush/holomush/internal/totp/mocks"
)

func newServiceForTest(t *testing.T) (totp.Service, *mocks.MockRepository) {
	t.Helper()
	repo := mocks.NewMockRepository(t)
	hasher := auth.NewArgon2idHasher()
	svc, err := totp.NewService(totp.Config{GameID: "default"}, repo, nil, totp.NewRealClock(), hasher)
	require.NoError(t, err)
	return svc, repo
}

func TestIsEnrolledReturnsRepoResult(t *testing.T) {
	svc, repo := newServiceForTest(t)
	pid := ulid.Make()
	repo.On("IsEnrolled", mock.Anything, pid.String()).Return(true, nil).Once()
	got, err := svc.IsEnrolled(context.Background(), pid)
	require.NoError(t, err)
	assert.True(t, got)
}

func TestIsEnrolledPropagatesError(t *testing.T) {
	svc, repo := newServiceForTest(t)
	pid := ulid.Make()
	want := errors.New("pg down")
	repo.On("IsEnrolled", mock.Anything, pid.String()).Return(false, want).Once()
	_, err := svc.IsEnrolled(context.Background(), pid)
	assert.ErrorIs(t, err, want)
}
```

- [ ] **6.4** Run `task test -- ./internal/totp/...` → GREEN. Lint clean.

- [ ] **6.5** Commit: `jj describe -m "feat(totp): Service skeleton + IsEnrolled (holomush-jxo8.3)"; jj new -m "wip: T7 BootstrapEnroll"`.

---

## Task 7: `Service.BootstrapEnroll`

**Locks:** INV-A1 (refuses after first), INV-A2 (covered at repo level in T5), INV-A9 (KEK-wrap), INV-A11 (Argon2id codes), INV-A13 (refuses unknown player), INV-A15 (rollback on insert error).

- [ ] **7.1** Add `BootstrapEnroll` and shared helper `buildEnrollment` to `service.go`:

```go
// (append to internal/totp/service.go)

// generate kek-mock for tests at this point — see step 7.3.

// BootstrapEnroll: per spec §"CLI commands" / "bootstrap-enroll" + §"Bootstrap closure mechanism".
// Per R5 Option Y: PG-only; returns BootstrapResult for caller emission.
func (s *service) BootstrapEnroll(ctx context.Context, playerID ulid.ULID) (BootstrapResult, error) {
	exists, err := s.repo.PlayerExists(ctx, playerID.String())
	if err != nil {
		return BootstrapResult{}, err
	}
	if !exists {
		return BootstrapResult{}, oops.Code("TOTP_PLAYER_NOT_FOUND").
			With("player_id", playerID.String()).Errorf("player not found")
	}

	now := s.clock.Now().UTC()
	enr, rec, err := s.buildEnrollment(ctx, playerID.String(), now)
	if err != nil {
		return BootstrapResult{}, err
	}
	if err := s.repo.BootstrapEnrollAtomic(ctx, "totp_v1", playerID.String(), rec); err != nil {
		return BootstrapResult{}, err // includes ErrBootstrapAlreadyConsumed
	}
	return BootstrapResult{
		Enrollment:      enr,
		AuditConsumedAt: now,
		AuditPlayerID:   playerID,
		BootstrapKey:    "totp_v1",
	}, nil
}

// buildEnrollment generates a fresh secret + URI + recovery codes,
// wraps the secret with KEK, hashes the codes with Argon2id, and
// returns the public Enrollment + persistable EnrollmentRecord.
func (s *service) buildEnrollment(ctx context.Context, playerID string, now time.Time) (Enrollment, EnrollmentRecord, error) {
	secret, err := generateSecret()
	if err != nil {
		return Enrollment{}, EnrollmentRecord{}, err
	}
	wrapped, kekKeyID, err := s.kek.Wrap(ctx, []byte(secret))
	if err != nil {
		return Enrollment{}, EnrollmentRecord{}, oops.Code("TOTP_KEK_WRAP_FAILED").Wrap(err)
	}
	uri, err := buildProvisioningURI(playerID, s.cfg.GameID, secret) // playerID as account label; see spec
	if err != nil {
		return Enrollment{}, EnrollmentRecord{}, err
	}
	codes, err := generateRecoveryCodes(s.cfg.RecoveryCodeCount)
	if err != nil {
		return Enrollment{}, EnrollmentRecord{}, err
	}
	hashed := make([]HashedRecoveryCode, len(codes))
	for i, c := range codes {
		h, hErr := s.enrollmentHasher.Hash(c)
		if hErr != nil {
			return Enrollment{}, EnrollmentRecord{}, oops.Code("TOTP_RECOVERY_HASH_FAILED").Wrap(hErr)
		}
		hashed[i] = HashedRecoveryCode{ID: ulid.Make(), CodeHash: h, CreatedAt: now}
	}
	return Enrollment{Secret: secret, ProvisioningURI: uri, RecoveryCodes: codes},
		EnrollmentRecord{
			PlayerID: playerID, WrappedSecret: wrapped, WrapKeyID: kekKeyID,
			EnrolledAt: now, RecoveryCodes: hashed,
		}, nil
}
```

- [ ] **7.2** Generate `kek.Provider` mock. Add a packages entry to `.mockery.yaml`:

```yaml
  github.com/holomush/holomush/internal/eventbus/crypto/kek:
    config:
      dir: "{{.InterfaceDir}}/mocks"
      outpkg: mocks
    interfaces:
      Provider:
```

Then run `task mocks:generate`. Verify `internal/eventbus/crypto/kek/mocks/mock_Provider.go` exists exporting `NewMockProvider(t)`.

- [ ] **7.3** Write `internal/totp/service_bootstrap_test.go` covering INV-A1, A9, A11, A13, A15. Use `auth.NewArgon2idHasher()` directly (real hasher; tests verify the hash round-trips). Mock `Repository` and `kek.Provider`. Each test is ~10-15 lines following the pattern:

```go
// (skeleton — full test file follows the same shape for all six cases below)

package totp_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	kekMocks "github.com/holomush/holomush/internal/eventbus/crypto/kek/mocks"
	"github.com/holomush/holomush/internal/totp"
	"github.com/holomush/holomush/internal/totp/mocks"
)

func newBootstrapFixture(t *testing.T, fakeNow time.Time) (totp.Service, *mocks.MockRepository, *kekMocks.MockProvider, *totp.FakeClock) {
	t.Helper()
	repo := mocks.NewMockRepository(t)
	kp := kekMocks.NewMockProvider(t)
	clk := totp.NewFakeClock(fakeNow)
	hasher := auth.NewArgon2idHasher()
	svc, err := totp.NewService(totp.Config{GameID: "default"}, repo, kp, clk, hasher)
	require.NoError(t, err)
	return svc, repo, kp, clk
}

// INV-A13: refuses unknown player.
func TestBootstrapEnrollRefusesUnknownPlayer(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, _, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()
	repo.On("PlayerExists", mock.Anything, pid.String()).Return(false, nil)
	_, err := svc.BootstrapEnroll(context.Background(), pid)
	assert.ErrorContains(t, err, "player not found")
}

// INV-A1: refuses after first success.
func TestBootstrapEnrollRefusesAfterFirstSuccess(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()
	repo.On("PlayerExists", mock.Anything, pid.String()).Return(true, nil)
	kp.On("Wrap", mock.Anything, mock.Anything).Return([]byte("w"), "kek-v1", nil)
	repo.On("BootstrapEnrollAtomic", mock.Anything, "totp_v1", pid.String(), mock.Anything).
		Return(totp.ErrBootstrapAlreadyConsumed)
	_, err := svc.BootstrapEnroll(context.Background(), pid)
	assert.ErrorIs(t, err, totp.ErrBootstrapAlreadyConsumed)
}

// INV-A9: KEK-wrapped secret.
func TestBootstrapEnrollWrapsSecretWithKEK(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()
	repo.On("PlayerExists", mock.Anything, pid.String()).Return(true, nil)
	wrapped := []byte("wrapped-bytes")
	kp.On("Wrap", mock.Anything, mock.Anything).Return(wrapped, "kek-v1", nil)
	var captured totp.EnrollmentRecord
	repo.On("BootstrapEnrollAtomic", mock.Anything, "totp_v1", pid.String(),
		mock.MatchedBy(func(r totp.EnrollmentRecord) bool { captured = r; return true })).
		Return(nil)
	res, err := svc.BootstrapEnroll(context.Background(), pid)
	require.NoError(t, err)
	assert.Equal(t, wrapped, captured.WrappedSecret)
	assert.Equal(t, "kek-v1", captured.WrapKeyID)
	assert.NotEmpty(t, res.Enrollment.Secret)
}

// INV-A11: recovery codes Argon2id-hashed.
func TestBootstrapEnrollHashesRecoveryCodes(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()
	repo.On("PlayerExists", mock.Anything, pid.String()).Return(true, nil)
	kp.On("Wrap", mock.Anything, mock.Anything).Return([]byte("w"), "kek-v1", nil)
	var captured totp.EnrollmentRecord
	repo.On("BootstrapEnrollAtomic", mock.Anything, "totp_v1", pid.String(),
		mock.MatchedBy(func(r totp.EnrollmentRecord) bool { captured = r; return true })).
		Return(nil)
	res, err := svc.BootstrapEnroll(context.Background(), pid)
	require.NoError(t, err)
	require.Len(t, res.Enrollment.RecoveryCodes, 10)
	require.Len(t, captured.RecoveryCodes, 10)
	hasher := auth.NewArgon2idHasher()
	for i, h := range captured.RecoveryCodes {
		raw := res.Enrollment.RecoveryCodes[i]
		assert.NotEqual(t, raw, h.CodeHash)
		ok, _ := hasher.Verify(raw, h.CodeHash)
		assert.True(t, ok)
	}
}

// INV-A15: any error in BootstrapEnrollAtomic propagates; no partial state.
// (Real-PG rollback verified at the repo layer in T5; here we verify the
// service propagates the error without further partial writes.)
func TestBootstrapEnrollPropagatesAtomicError(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()
	repo.On("PlayerExists", mock.Anything, pid.String()).Return(true, nil)
	kp.On("Wrap", mock.Anything, mock.Anything).Return([]byte("w"), "kek-v1", nil)
	want := errors.New("pg insert failed")
	repo.On("BootstrapEnrollAtomic", mock.Anything, "totp_v1", pid.String(), mock.Anything).Return(want)
	_, err := svc.BootstrapEnroll(context.Background(), pid)
	assert.ErrorIs(t, err, want)
}

// Result-struct metadata population (replaces retired INV-A10 audit-emit tests).
func TestBootstrapResultCarriesAuditMetadata(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()
	repo.On("PlayerExists", mock.Anything, pid.String()).Return(true, nil)
	kp.On("Wrap", mock.Anything, mock.Anything).Return([]byte("w"), "kek-v1", nil)
	repo.On("BootstrapEnrollAtomic", mock.Anything, "totp_v1", pid.String(), mock.Anything).Return(nil)
	res, err := svc.BootstrapEnroll(context.Background(), pid)
	require.NoError(t, err)
	assert.Equal(t, now, res.AuditConsumedAt)
	assert.Equal(t, pid, res.AuditPlayerID)
	assert.Equal(t, "totp_v1", res.BootstrapKey)
}
```

- [ ] **7.4** Run `task test -- ./internal/totp/...` → GREEN. Lint clean.

- [ ] **7.5** Commit: `jj describe -m "feat(totp): BootstrapEnroll w/ KEK-wrap, hashed codes, atomic insert (INV-A1/A9/A11/A13/A15; holomush-jxo8.3)"; jj new -m "wip: T8 Enroll"`.

---

## Task 8: `Service.Enroll`

- [ ] **8.1** Append `Enroll` to `service.go`:

```go
func (s *service) Enroll(ctx context.Context, playerID ulid.ULID) (EnrollResult, error) {
	enrolled, err := s.repo.IsEnrolled(ctx, playerID.String())
	if err != nil {
		return EnrollResult{}, err
	}
	if enrolled {
		return EnrollResult{}, ErrAlreadyEnrolled
	}
	now := s.clock.Now().UTC()
	enr, rec, err := s.buildEnrollment(ctx, playerID.String(), now)
	if err != nil {
		return EnrollResult{}, err
	}
	if err := s.repo.InsertEnrollment(ctx, rec); err != nil {
		return EnrollResult{}, err
	}
	return EnrollResult{Enrollment: enr, AuditEnrolledAt: now, AuditPlayerID: playerID}, nil
}
```

- [ ] **8.2** Write `service_enroll_test.go` with three tests: refuses if already enrolled (returns ErrAlreadyEnrolled), succeeds for unenrolled player (verify EnrollResult metadata), propagates InsertEnrollment error. Same fixture pattern as T7.3.

- [ ] **8.3** Run tests/lint, commit: `jj describe -m "feat(totp): Enroll for self-enrollment (holomush-jxo8.3)"; jj new -m "wip: T9 Verify"`.

---

## Task 9: `Service.Verify` — replay defense + lockout + skew

**Locks:** INV-A3 (replay), A4 (lockout), A5 (success resets), A12 (skew window), A14 (failure paths don't mutate success fields).

- [ ] **9.1** Append `Verify` to `service.go`. Note: loop uses `tryStep` (not `s` — that shadows the receiver) and iterates all three steps unconditionally (no `break`):

```go
import (
	// add to existing imports:
	"crypto/subtle"
	"github.com/pquerna/otp/hotp"
)

const totpStepSeconds = 30

func (s *service) Verify(ctx context.Context, playerID ulid.ULID, code string) (VerifyResult, error) {
	now := s.clock.Now().UTC()
	var result VerifyResult
	result.AuditAt = now

	txErr := s.repo.InTransaction(ctx, func(txCtx context.Context) error {
		state, err := s.repo.LoadEnrollment(txCtx, playerID.String())
		if err != nil {
			if errors.Is(err, ErrNotEnrolled) {
				result.Outcome = OutcomeNotEnrolled
				return nil
			}
			return err
		}
		if state.LockedUntil != nil && state.LockedUntil.After(now) {
			result.Outcome = OutcomeLocked
			result.LockedUntil = state.LockedUntil
			return nil
		}
		secret, err := s.kek.Unwrap(txCtx, state.WrappedSecret, state.WrapKeyID)
		if err != nil {
			return oops.Code("TOTP_KEK_UNWRAP_FAILED").Wrap(err)
		}
		step := now.Unix() / totpStepSeconds
		matchedStep := int64(-1)
		for offset := -s.cfg.SkewSteps; offset <= s.cfg.SkewSteps; offset++ {
			tryStep := step + int64(offset)
			expected, err := hotp.GenerateCode(string(secret), uint64(tryStep))
			if err != nil {
				continue
			}
			if subtle.ConstantTimeCompare([]byte(code), []byte(expected)) == 1 {
				matchedStep = tryStep
				// do NOT break — iterate all steps to avoid timing-leak
			}
		}
		if matchedStep == -1 {
			post, err := s.repo.IncrementFailedAttempts(txCtx,
				playerID.String(), s.cfg.LockoutThreshold, s.cfg.LockoutDuration, now)
			if err != nil {
				return err
			}
			result.Outcome = OutcomeInvalidCode
			result.LockedUntil = post.LockedUntil
			result.LockoutTransition = (state.LockedUntil == nil &&
				post.LockedUntil != nil && post.LockedUntil.After(now))
			return nil
		}
		if state.LastUsedStep != nil && matchedStep <= *state.LastUsedStep {
			result.Outcome = OutcomeCodeReuse
			return errCodeReuseRollback // typed sentinel: triggers ROLLBACK without surfacing as a real error
		}
		if err := s.repo.MarkVerified(txCtx, playerID.String(), matchedStep, now); err != nil {
			return err
		}
		result.Outcome = OutcomeOK
		return nil
	})
	if errors.Is(txErr, errCodeReuseRollback) {
		return result, nil
	}
	if txErr != nil {
		return VerifyResult{}, txErr
	}
	return result, nil
}
```

Add the typed sentinel near the top of `service.go` (or in `errors.go`):

```go
// errCodeReuseRollback is an unexported sentinel returned from inside
// Repository.InTransaction to force a ROLLBACK on replay detection
// (Service.Verify, OutcomeCodeReuse path). Never returned by Service to
// callers — the caller-facing surface is VerifyResult.Outcome.
var errCodeReuseRollback = errors.New("totp: rollback for code reuse")
```

Use `errors.Is(txErr, errCodeReuseRollback)` (not `txErr.Error() == ...`) so error wrapping by `oops` does not break the check.

- [ ] **9.2** Mock `Repository.InTransaction` to invoke `fn` directly without real txn semantics. Use mockery v3's `EXPECT().Method(...).RunAndReturn(...)` form — this is the canonical pattern in this codebase (see `internal/auth/guest_service_test.go:25-30` for a working example):

```go
// helper in test file
func runInTxn(repo *mocks.MockRepository) {
    repo.EXPECT().InTransaction(mock.Anything, mock.AnythingOfType("func(context.Context) error")).
        RunAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
            return fn(ctx) // mirrors real Transactor: fn nil → COMMIT, fn err → ROLLBACK
        })
}
```

`RunAndReturn` invokes `fn` exactly **once** and returns its result as the mocked method's return value. **DO NOT use `Run(...).Return(func(...))`** — that double-invokes `fn` because the mockery-generated dispatch detects function-typed `Return` values and calls them, on top of `Run`'s explicit invocation. Each Verify test calls `runInTxn(repo)` once before the per-test mocks for `LoadEnrollment` / `IncrementFailedAttempts` / `MarkVerified`.

- [ ] **9.3** Write `service_verify_test.go` covering:
  - Replay → OutcomeCodeReuse (INV-A3)
  - 5 failures → LockoutTransition: true, LockedUntil set (INV-A4)
  - Already locked → OutcomeLocked (INV-A4 partial)
  - Success after lockout expiry → OutcomeOK + counter reset assertion via repo.MarkVerified expectation (INV-A5)
  - Skew=1 accepts step-1 / step / step+1 (INV-A12) — three subtests
  - Failure paths don't call MarkVerified (INV-A14)
  - ErrNotEnrolled → OutcomeNotEnrolled, no error returned

- [ ] **9.4** Tests/lint/commit: `jj describe -m "feat(totp): Verify w/ replay defense, lockout, skew window (INV-A3/A4/A5/A12/A14; holomush-jxo8.3)"; jj new -m "wip: T10 recovery + clear"`.

---

## Task 10: `ConsumeRecoveryCode` + `ClearTOTP`

**Locks:** INV-A6 (recovery code single-use, repo level), INV-A7 (ClearTOTP deletes both tables, repo level), INV-A8 (ClearTOTP doesn't touch bootstrap_state).

- [ ] **10.1** Append to `service.go`:

```go
func (s *service) ConsumeRecoveryCode(ctx context.Context, playerID ulid.ULID, code string) (ConsumeRecoveryResult, error) {
	now := s.clock.Now().UTC()
	id, err := s.repo.ConsumeRecoveryCode(ctx, playerID.String(), code, s.verifyHasher, now)
	if err != nil {
		return ConsumeRecoveryResult{}, err
	}
	return ConsumeRecoveryResult{
		RecoveryCodeID:  id,
		AuditConsumedAt: now,
		AuditPlayerID:   playerID,
	}, nil
}

func (s *service) ClearTOTP(ctx context.Context, playerID ulid.ULID, clearedBy ClearReason) (ClearResult, error) {
	wasEnrolled, err := s.repo.ClearEnrollment(ctx, playerID.String())
	if err != nil {
		return ClearResult{}, err
	}
	now := s.clock.Now().UTC()
	return ClearResult{
		ClearedBy:      clearedBy,
		AuditClearedAt: now,
		AuditPlayerID:  playerID,
		WasEnrolled:    wasEnrolled,
	}, nil
}
```

- [ ] **10.2** Write `service_recovery_test.go` covering:
  - Bad code → ErrInvalidRecoveryCode (INV-A6 propagation)
  - Good code → result.RecoveryCodeID populated
  - ClearTOTP delegates to repo.ClearEnrollment; result.ClearedBy matches input; WasEnrolled propagates (INV-A7 via repo)
  - ClearTOTP MUST NOT call any bootstrap-state-related repo methods (INV-A8: assert mock expectations don't include BootstrapClaim or similar)

- [ ] **10.3** Tests/lint/commit: `jj describe -m "feat(totp): ConsumeRecoveryCode + ClearTOTP (INV-A6/A7/A8; holomush-jxo8.3)"; jj new -m "wip: T11 admin CLI scaffolding"`.

---

## Task 11: `holomush admin` CLI scaffolding

**Files:** `cmd/holomush/cmd_admin.go` (new), `cmd/holomush/cmd_admin_totp.go` (new — parent only), `cmd/holomush/root.go` (modify), `cmd/holomush/cmd_admin_test.go` (new)

- [ ] **11.1** Create `cmd/holomush/cmd_admin.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import "github.com/spf13/cobra"

func NewAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Operator break-glass and admin commands (host-shell only)",
	}
	cmd.AddCommand(NewAdminTOTPCmd())
	return cmd
}
```

- [ ] **11.2** Create `cmd/holomush/cmd_admin_totp.go` (parent stub; subcommands land in T12-T14):

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import "github.com/spf13/cobra"

func NewAdminTOTPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "totp",
		Short: "TOTP enrollment, verification, recovery (host-shell only)",
	}
	// T12: cmd.AddCommand(newAdminTOTPBootstrapEnrollCmd())
	// T13: cmd.AddCommand(newAdminTOTPEnrollCmd())
	// T14: cmd.AddCommand(newAdminTOTPRecoverCmd())
	return cmd
}
```

- [ ] **11.3** Edit `cmd/holomush/root.go` `NewRootCmd`: append `cmd.AddCommand(NewAdminCmd())` after the existing `NewPluginCmd` line.

- [ ] **11.4** Smoke test `cmd/holomush/cmd_admin_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAdminCmdRegistered(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"admin"})
	assert.NoError(t, err)
	assert.Equal(t, "admin", cmd.Name())
}

func TestAdminTOTPCmdRegistered(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"admin", "totp"})
	assert.NoError(t, err)
	assert.Equal(t, "totp", cmd.Name())
}
```

- [ ] **11.5** Tests/lint/commit: `jj describe -m "feat(cli): admin parent + admin totp parent (holomush-jxo8.3)"; jj new -m "wip: T12-T14 CLI deps + subcommands"`.

---

## Task 12: CLI dependency wiring (`cmd_admin_totp_deps.go`)

The CLIs need: PG pool, KEK provider, TOTP Service, and (for `enroll`) the auth.Service for `ValidateCredentials`. Pin actual repo helpers — not the stubs the v1 plan invented.

**Verified actual helpers** (from probe research):

- **`DATABASE_URL`** — read via `getDatabaseURL()` defined at `cmd/holomush/migrate.go:292-298` (returns from `os.Getenv("DATABASE_URL")`). REUSE this; do NOT duplicate.
- **PG pool** — there is NO existing `store.OpenPool(...)` helper. Direct `pgxpool.New(ctx, url)` is the pattern (the `holomush core` startup builds the pool inline; see `cmd/holomush/core.go` for reference).
- **Config** — `config.Load(configFile, cmd, &cfg, "section")` per `cmd/holomush/core.go:108-122`. The `cmd` argument may be a `*cobra.Command` for CLI flag overrides; for the admin CLIs that don't bind config flags, pass `nil` (verified — the section-only path does not require cmd).
- **KEK** — construct via `kek.NewLocalAEADProvider(ctx, source, db)` where `db` is the pool (used for INV-33 startup integrity check). `source` is built from a `KEKSource` such as `kek.NewFileSource(path, passphraseFunc)` at `internal/eventbus/crypto/kek/source_file.go`. Step 12.0 below directs the plan-author to read `cmd/holomush/core.go`'s actual KEK construction sequence and replicate it.
- **`auth.Service`** — real symbol is `auth.NewAuthService(playerRepo, playerSessions, hasher, opts...)` per `internal/auth/auth_service.go:51`. **Rejects nil `playerSessions`** (line 60). Use `store.NewPostgresPlayerSessionStore(pool)` per `internal/store/player_session_store.go:24`. The CLI only needs `ValidateCredentials` (defined at `internal/auth/registration.go:45`), but the constructor still requires the session store dep.
- **PlayerRepository:** `authpg.NewPlayerRepository(pool)` per `internal/auth/postgres/player_repo.go:26`.

**Files:** `cmd/holomush/cmd_admin_totp_deps.go` (new)

- [ ] **12.0** Read `cmd/holomush/core.go` in full and locate where `runCoreWithDeps` constructs the KEK provider. Copy the exact sequence (typically: load `kek` config section via `config.Load(configFile, cmd, &kekCfg, "kek")`, build a `kek.KEKSource` via `kek.NewFileSource(path, passphraseFunc)` or env-source variant, then `kek.NewLocalAEADProvider(ctx, source, pool)`). Inline a comment block in `cmd_admin_totp_deps.go` showing the exact call sequence the production server uses, so the CLI's KEK is byte-identical. Failure to match means TOTP secrets KEK-wrapped under one source can't be unwrapped under the other (server-vs-CLI inconsistency).

- [ ] **12.1** Probe-locate the KEK construction in `core.go`:

```bash
# from the worktree root:
mcp__probe__search_code path=. query='kek.NewLocalAEADProvider source=' (or similar)
# OR
rg -n 'kek.NewLocalAEADProvider' cmd/ internal/
```

Read the construction site; copy the shape.

- [ ] **12.2** Implement `cmd/holomush/cmd_admin_totp_deps.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/holomush/holomush/internal/auth"
	authpg "github.com/holomush/holomush/internal/auth/postgres"
	"github.com/holomush/holomush/internal/config"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/totp"
)

// adminTOTPDeps bundles dependencies the admin totp CLIs need.
type adminTOTPDeps struct {
	pool     *pgxpool.Pool
	totpSvc  totp.Service
	totpRepo totp.Repository
	authSvc  *auth.Service // for `enroll` CLI's ValidateCredentials
	gameID   string
}

func buildAdminTOTPDeps(ctx context.Context, configPath string) (*adminTOTPDeps, func(), error) {
	url, err := getDatabaseURL()
	if err != nil {
		return nil, nil, err
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, nil, fmt.Errorf("pgxpool.New: %w", err)
	}

	// Load game config for GameID.
	var gameCfg config.GameConfig
	if err := config.Load(configPath, nil, &gameCfg, "game"); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("config.Load(game): %w", err)
	}

	// Construct KEK provider — MUST match the server's KEK source byte-for-byte
	// or wrapped TOTP secrets won't unwrap. Replicate the exact construction
	// sequence read in step 12.0 from cmd/holomush/core.go. Sketch:
	//   var kekCfg kek.Config              // or whatever struct the section binds to
	//   if err := config.Load(configPath, nil, &kekCfg, "kek"); err != nil { ... }
	//   source, err := kek.NewFileSource(kekCfg.SourceFile, passphraseFromEnv())
	//   if err != nil { ... }
	//   kekProvider, err := kek.NewLocalAEADProvider(ctx, source, pool)
	// (Confirm exact field names + the right passphrase-source helper at T12.1.)
	kekProvider, err := buildKEKProviderFromConfig(ctx, configPath, pool)
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("kek setup: %w", err)
	}

	// Auth service for ValidateCredentials. Real constructor:
	//   auth.NewAuthService(players, playerSessions, hasher, opts...) (*Service, error)
	// rejects nil playerSessions (internal/auth/auth_service.go:60).
	playerRepo := authpg.NewPlayerRepository(pool)
	sessionStore := store.NewPostgresPlayerSessionStore(pool)
	hasher := auth.NewArgon2idHasher()
	authSvc, err := auth.NewAuthService(playerRepo, sessionStore, hasher)
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("auth.NewAuthService: %w", err)
	}

	totpRepo := totp.NewRepository(pool)
	totpSvc, err := totp.NewService(
		totp.Config{GameID: gameCfg.ID},
		totpRepo, kekProvider, totp.NewRealClock(), hasher,
	)
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("totp.NewService: %w", err)
	}

	cleanup := func() { pool.Close() }
	return &adminTOTPDeps{
		pool:     pool,
		totpSvc:  totpSvc,
		totpRepo: totpRepo,
		authSvc:  authSvc,
		gameID:   gameCfg.ID,
	}, cleanup, nil
}

// buildKEKProviderFromConfig is a thin wrapper around the production
// KEK construction. Inline the body using the sequence verified at
// T12.0; this function exists so both buildAdminTOTPDeps and the E2E
// test fixture can call the same code path.
func buildKEKProviderFromConfig(ctx context.Context, configPath string, pool *pgxpool.Pool) (kek.Provider, error) {
	// PLAN AUTHOR: body comes from T12.0 inline — DO NOT ship with a
	// stub. After T12.0, replace this comment with the actual N-line
	// sequence verified against cmd/holomush/core.go.
	return nil, fmt.Errorf("buildKEKProviderFromConfig: not implemented; complete via T12.0")
}
```

The `buildKEKProviderFromConfig` body is the only piece that requires per-execution research (T12.0 directs the plan-author to read `cmd/holomush/core.go` and replicate the exact sequence). Every other symbol (`auth.NewAuthService`, `store.NewPostgresPlayerSessionStore`, `authpg.NewPlayerRepository`, `pgxpool.New`, `getDatabaseURL`, `config.Load`, `totp.NewService`) is verified to exist with the named signature.

- [ ] **12.3** Build check: `go build ./cmd/holomush/...` → must compile (will surface if any constructor signatures are wrong, fix in place).

- [ ] **12.3.5** **Stub-replacement gate.** Run `rg "buildKEKProviderFromConfig: not implemented" cmd/holomush/`. If this returns ANY hits, the T12.0/T12.2 KEK construction stub was not replaced — STOP, do not commit, complete T12.0 first.

- [ ] **12.4** Commit (no tests yet — the CLIs in T13-T14 use this; deps integration is exercised by E2E in T17): `jj describe -m "feat(cli): admin totp dep wiring (PG + KEK + auth + totp.Service; holomush-jxo8.3)"; jj new -m "wip: T13 bootstrap-enroll CLI"`.

---

## Task 13: `holomush admin totp bootstrap-enroll <username>`

- [ ] **13.1** Add subcommand handler. Append to `cmd/holomush/cmd_admin_totp.go`:

```go
import (
	// add to existing:
	"context"
	"fmt"
	"io"

	"github.com/oklog/ulid/v2"
	"github.com/holomush/holomush/internal/totp"
)

func newAdminTOTPBootstrapEnrollCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "bootstrap-enroll <username>",
		Short: "Once-only first-admin TOTP enrollment (host-shell only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			deps, cleanup, err := buildAdminTOTPDeps(ctx, configPath)
			if err != nil {
				return err
			}
			defer cleanup()
			username := args[0]
			pid, err := deps.totpRepo.PlayerIDFromUsername(ctx, username)
			if err != nil {
				return err
			}
			pidULID, err := ulid.Parse(pid)
			if err != nil {
				return err
			}
			res, err := deps.totpSvc.BootstrapEnroll(ctx, pidULID)
			if err != nil {
				return err
			}
			return printEnrollment(cmd.OutOrStdout(), username, pid, res.Enrollment)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "config file path")
	return cmd
}

func printEnrollment(w io.Writer, username, playerID string, enr totp.Enrollment) error {
	if _, err := fmt.Fprintf(w, `TOTP enrolled for %s (player_id=%s).

Provisioning URI (scan into authenticator app):
  %s

Manual entry secret (if QR scanning unavailable):
  %s

Recovery codes — STORE THESE OFFLINE NOW (each is single-use):
`, username, playerID, enr.ProvisioningURI, formatSecretForDisplay(enr.Secret)); err != nil {
		return err
	}
	for i, c := range enr.RecoveryCodes {
		if _, err := fmt.Fprintf(w, "  %d.  %s\n", i+1, c); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w, `
This output WILL NOT be shown again. Lose your authenticator and these
codes, and you may be permanently locked out of break-glass operations.

NOTE (R5 Option Y): no audit event is emitted for this host-shell
invocation. The crypto_bootstrap_state row in PG is the durable record.
See spec §"Audit events emitted" / "Emission ownership and the
host-shell-CLI gap".`)
	return err
}

func formatSecretForDisplay(s string) string {
	var out []rune
	for i, r := range s {
		if i > 0 && i%5 == 0 {
			out = append(out, ' ')
		}
		out = append(out, r)
	}
	return string(out)
}
```

Update `NewAdminTOTPCmd` to call `cmd.AddCommand(newAdminTOTPBootstrapEnrollCmd())`.

- [ ] **13.2** Smoke test in `cmd_admin_test.go`:

```go
func TestAdminTOTPBootstrapEnrollExists(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"admin", "totp", "bootstrap-enroll"})
	assert.NoError(t, err)
	assert.Equal(t, "bootstrap-enroll <username>", cmd.Use)
}
```

- [ ] **13.3** Tests/lint/commit: `jj describe -m "feat(cli): admin totp bootstrap-enroll (holomush-jxo8.3)"; jj new -m "wip: T14 enroll + recover CLIs"`.

---

## Task 14: `enroll` and `recover` CLIs

Pattern mirrors T13. Both CLIs prompt for input via `bufio.NewReader(cmd.InOrStdin())` and `golang.org/x/term.ReadPassword` for secrets.

- [ ] **14.1** Add `newAdminTOTPEnrollCmd` to `cmd_admin_totp.go`. Flow: prompt username (or read `--username`), prompt password, call `deps.authSvc.ValidateCredentials(ctx, username, password)`, then `deps.totpSvc.Enroll(ctx, player.ID)`, print enrollment.

- [ ] **14.2** Add `newAdminTOTPRecoverCmd`. Flow: prompt username (or `--username`), prompt recovery code (no echo), look up player_id via `deps.totpRepo.PlayerIDFromUsername` (timing-safe: on lookup failure return generic ErrInvalidRecoveryCode), call `deps.totpSvc.ConsumeRecoveryCode`, then `deps.totpSvc.ClearTOTP(playerID, ClearReasonRecoveryCode)`, print "TOTP cleared. Run `holomush admin totp enroll --username <user>` to re-enroll."

- [ ] **14.3** Register both in `NewAdminTOTPCmd`. Add tree-presence smoke tests.

- [ ] **14.4** Tests/lint/commit: `jj describe -m "feat(cli): admin totp enroll + recover (holomush-jxo8.3)"; jj new -m "wip: T15 kek docstring + T16 ABAC seeds"`.

---

## Task 15: kek.Provider docstring generalization

**File:** `internal/eventbus/crypto/kek/provider.go` (modify)

- [ ] **15.1** Update package doc + `Wrap` doc comment per spec §"Dependencies" `kek.Provider` row. The package now wraps DEKs (Phase 2) AND TOTP secrets (Phase 5). Replace "DEK bytes" framing with "opaque secret bytes" framing. Pattern is documentary-only; no API change.

- [ ] **15.2** Run `task test -- ./internal/eventbus/crypto/kek/...` → existing tests still GREEN.

- [ ] **15.3** Commit: `jj describe -m "docs(kek): generalize provider docstring for non-DEK secret reuse (holomush-jxo8.3)"; jj new -m "wip: T16 ABAC seeds"`.

---

## Task 16: ABAC seed policies (INV-A16)

**Files:** `internal/access/policy/seed.go` (modify), `internal/access/policy/seed_test.go` (modify)

Per spec §"Audit events emitted" / "ABAC seed policies for the new subject namespace": two new forbid seeds parallel to the existing `seed:deny-audit-read-{character,plugin}`.

- [ ] **16.1** Append two new `SeedPolicy` entries to `SeedPolicies()` in `internal/access/policy/seed.go`. Place adjacent to the existing `seed:deny-audit-read-*` entries (after line ~227):

```go
{
    Name:        "seed:deny-events-system-crypto-totp-read-character",
    Description: "Characters MUST NOT read events.*.system.crypto_totp.* streams (Phase 5 sub-epic A; parallel to seed:deny-audit-read-character)",
    DSLText:     `forbid(principal is character, action in ["read"], resource is stream) when { resource.stream.name like "events.*.system.crypto_totp.*" };`,
    SeedVersion: 1,
},
{
    Name:        "seed:deny-events-system-crypto-totp-read-plugin",
    Description: "Plugins MUST NOT read events.*.system.crypto_totp.* streams (Phase 5 sub-epic A; parallel to seed:deny-audit-read-plugin)",
    DSLText:     `forbid(principal is plugin, action in ["read"], resource is stream) when { resource.stream.name like "events.*.system.crypto_totp.*" };`,
    SeedVersion: 1,
},
```

- [ ] **16.2** Update `TestSeedPoliciesCount` in `seed_test.go`: change `assert.Len(t, seeds, 28, ...)` to `30` (add comment noting "+2 phase-5 sub-epic A events.*.system.crypto_totp.* deny seeds").

- [ ] **16.3** Update `TestSeedPoliciesExpectedNames` in `seed_test.go`: append two entries to `expectedNames`:

```go
"seed:deny-events-system-crypto-totp-read-character",
"seed:deny-events-system-crypto-totp-read-plugin",
```

- [ ] **16.4** Update `TestSeedPoliciesForbidPoliciesAreExpected`: append the two names to the `expectedForbids` map.

- [ ] **16.5** Add the two parallel seed-existence tests for INV-A16 (place adjacent to the existing `TestSeedPoliciesIncludesAuditSubscribeDenyFor*` tests at lines 240-268):

```go
func TestSeedPoliciesIncludesEventsSystemCryptoTotpDenyForCharacter(t *testing.T) {
    seeds := SeedPolicies()
    var found bool
    for _, s := range seeds {
        if s.Name == "seed:deny-events-system-crypto-totp-read-character" {
            found = true
            assert.Contains(t, s.DSLText, "forbid")
            assert.Contains(t, s.DSLText, "events.*.system.crypto_totp.*")
            assert.Contains(t, s.DSLText, "principal is character")
            break
        }
    }
    assert.True(t, found, "events.*.system.crypto_totp.* deny seed for character MUST be present (INV-A16)")
}

func TestSeedPoliciesIncludesEventsSystemCryptoTotpDenyForPlugin(t *testing.T) {
    seeds := SeedPolicies()
    var found bool
    for _, s := range seeds {
        if s.Name == "seed:deny-events-system-crypto-totp-read-plugin" {
            found = true
            assert.Contains(t, s.DSLText, "forbid")
            assert.Contains(t, s.DSLText, "events.*.system.crypto_totp.*")
            assert.Contains(t, s.DSLText, "principal is plugin")
            break
        }
    }
    assert.True(t, found, "events.*.system.crypto_totp.* deny seed for plugin MUST be present (INV-A16)")
}
```

- [ ] **16.6** Update `TestBootstrapSetsCorrectPolicyEffect` in `internal/access/policy/bootstrap_test.go` (line ~336): change `expectedForbids` map and `forbidCount` assertion (was 3, now 5).

- [ ] **16.6.5** Update `TestSeedPoliciesEffectDistribution` in `internal/access/policy/seed_test.go` (line ~75): change `assert.Equal(t, 3, forbidCount, "expected 3 forbid policies")` to `assert.Equal(t, 5, forbidCount, "expected 5 forbid policies (+2 phase-5 sub-epic A events.*.system.crypto_totp.* denies)")`. (Plan-reviewer R2 caught this — there are THREE forbid-count assertions, not two: `TestSeedPoliciesCount` updated in 16.2 (count 28→30), `TestBootstrapSetsCorrectPolicyEffect` in 16.6, and this one.)

- [ ] **16.7** Run `task test -- ./internal/access/...` → all GREEN.

- [ ] **16.8** Commit: `jj describe -m "feat(access): ABAC forbid seeds for events.*.system.crypto_totp.* (INV-A16; holomush-jxo8.3)"; jj new -m "wip: T17 E2E"`.

---

## Task 17: E2E lifecycle test

**File:** `test/integration/totp_e2e_test.go` (new, build tag `integration`)

Per spec §"Testing approach" E2E row: real PG, real KEK file, **no audit-publisher assertions** (sub-epic A doesn't emit; audit-table assertions live in sub-epic D's E2E once D ships).

- [ ] **17.1** Locate existing E2E suite pattern: read `test/integration/*_test.go` to find the testcontainer/KEK-file fixture (likely a `BeforeSuite` that builds `testPool`, `testKEK`).

- [ ] **17.2** Write `totp_e2e_test.go`:

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package integration_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/oklog/ulid/v2"
	"github.com/pquerna/otp/hotp"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/totp"
)

var _ = Describe("TOTP substrate E2E (PG + KEK; no eventbus)", func() {
	var (
		ctx    context.Context
		svc    totp.Service
		repo   totp.Repository
		alice  string
		bob    string
	)

	BeforeEach(func() {
		ctx = context.Background()
		repo = totp.NewRepository(testPool) // suite-provided
		hasher := auth.NewArgon2idHasher()
		var err error
		svc, err = totp.NewService(
			totp.Config{GameID: "default"}, repo, testKEK, totp.NewRealClock(), hasher,
		)
		Expect(err).NotTo(HaveOccurred())
		alice = ulid.Make().String()
		bob = ulid.Make().String()
		insertPlayer(ctx, testPool, alice, "alice")
		insertPlayer(ctx, testPool, bob, "bob")
	})

	AfterEach(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM player_totp_recovery_codes`)
		_, _ = testPool.Exec(ctx, `DELETE FROM player_totp`)
		_, _ = testPool.Exec(ctx, `DELETE FROM crypto_bootstrap_state`)
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id IN ($1, $2)`, alice, bob)
	})

	It("supports the full bootstrap → enroll → verify → recover → re-enroll cycle", func() {
		By("bootstrap-enrolling alice")
		alicePID := ulid.MustParse(alice)
		bRes, err := svc.BootstrapEnroll(ctx, alicePID)
		Expect(err).NotTo(HaveOccurred())
		Expect(bRes.Enrollment.Secret).NotTo(BeEmpty())
		Expect(bRes.Enrollment.RecoveryCodes).To(HaveLen(10))
		Expect(bRes.BootstrapKey).To(Equal("totp_v1"))

		By("refusing a second bootstrap-enroll")
		_, err = svc.BootstrapEnroll(ctx, ulid.MustParse(bob))
		Expect(err).To(MatchError(totp.ErrBootstrapAlreadyConsumed))

		By("verifying alice's TOTP at the current step")
		now := time.Now().UTC()
		code, err := hotp.GenerateCode(bRes.Enrollment.Secret, uint64(now.Unix()/30))
		Expect(err).NotTo(HaveOccurred())
		vRes, err := svc.Verify(ctx, alicePID, code)
		Expect(err).NotTo(HaveOccurred())
		Expect(vRes.Outcome).To(Equal(totp.OutcomeOK))

		By("rejecting code reuse")
		vRes2, err := svc.Verify(ctx, alicePID, code)
		Expect(err).NotTo(HaveOccurred())
		Expect(vRes2.Outcome).To(Equal(totp.OutcomeCodeReuse))

		By("recovering alice with a recovery code")
		consRes, err := svc.ConsumeRecoveryCode(ctx, alicePID, bRes.Enrollment.RecoveryCodes[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(consRes.RecoveryCodeID).NotTo(Equal(ulid.ULID{}))
		clrRes, err := svc.ClearTOTP(ctx, alicePID, totp.ClearReasonRecoveryCode)
		Expect(err).NotTo(HaveOccurred())
		Expect(clrRes.WasEnrolled).To(BeTrue())
		ok, err := svc.IsEnrolled(ctx, alicePID)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())

		By("re-enrolling alice via Enroll")
		eRes, err := svc.Enroll(ctx, alicePID)
		Expect(err).NotTo(HaveOccurred())
		Expect(eRes.Enrollment.RecoveryCodes).To(HaveLen(10))
		Expect(eRes.Enrollment.RecoveryCodes).NotTo(ContainElement(bRes.Enrollment.RecoveryCodes[0]))

		// NOTE: no events_audit assertions — sub-epic A emits nothing per Option Y.
	})
})
```

(Adjust `testPool`, `testKEK`, `insertPlayer` symbols to match the actual suite fixture names.)

- [ ] **17.3** Run `task test:int -- ./test/integration/...` → GREEN.

- [ ] **17.4** Commit: `jj describe -m "test(totp): E2E lifecycle (PG + KEK; no audit-publisher assertions per Option Y; holomush-jxo8.3)"; jj new -m "wip: T18 pr-prep"`.

---

## Task 18: `task pr-prep` and bead update

- [ ] **18.1** Run `task pr-prep` → all green (lint, format, schema, license, unit, integration, E2E). Fix anything that fails at root cause; never `--no-verify`.

- [ ] **18.2** Update bead with plan reference: `bd update holomush-jxo8.3 --notes "Plan: docs/superpowers/plans/2026-05-07-event-payload-crypto-phase5-totp-substrate.md (R5 spec; v2 plan landed against R5)"`.

- [ ] **18.3** Final commit / squash: `jj log -r 'main..@'` → confirm 18 commits with coherent descriptions. Plan ends here; user pushes.

---

## Bead chain structure

```text
holomush-jxo8.3                      (existing epic — Phase 5 sub-epic A: TOTP substrate)
├── jxo8.3.1   Package skeleton — Clock + typed errors                       (T1)
├── jxo8.3.2   Migration 000019 — three tables                                (T2)
├── jxo8.3.3   Types (Service / Repository / result structs) + provisioning  (T3; deps: .3.1)
├── jxo8.3.4   Audit subject helpers + payload structs (RESERVED for D)      (T4)
├── jxo8.3.5   Postgres Repository — Transactor + BootstrapEnrollAtomic       (T5; deps: .3.2, .3.3)
├── jxo8.3.6   Service skeleton + IsEnrolled + mockery setup                  (T6; deps: .3.3)
├── jxo8.3.7   Service.BootstrapEnroll                                        (T7; deps: .3.5, .3.6)
├── jxo8.3.8   Service.Enroll                                                 (T8; deps: .3.7)
├── jxo8.3.9   Service.Verify — replay + lockout + skew                       (T9; deps: .3.5, .3.6)
├── jxo8.3.10  Service.ConsumeRecoveryCode + ClearTOTP                        (T10; deps: .3.5, .3.6)
├── jxo8.3.11  `holomush admin` + `admin totp` CLI scaffolding                (T11)
├── jxo8.3.12  CLI dependency wiring (cmd_admin_totp_deps.go)                 (T12; deps: .3.6)
├── jxo8.3.13  `holomush admin totp bootstrap-enroll` CLI                     (T13; deps: .3.7, .3.11, .3.12)
├── jxo8.3.14  `holomush admin totp enroll` + `recover` CLIs                  (T14; deps: .3.8, .3.10, .3.11, .3.12)
├── jxo8.3.15  kek.Provider docstring generalization                          (T15; documentary)
├── jxo8.3.16  ABAC forbid seeds for INV-A16                                  (T16)
└── jxo8.3.17  E2E lifecycle test (PG + KEK; no audit emission)               (T17; deps: .3.13, .3.14)

T18 (`task pr-prep` + bead update) is housekeeping — no bead.
```

### `bd create` commands

All beads are P2 tasks (sub-epic A is itself an epic; its children are individual implementation tasks).
Common shape (description follows the 8-section convention from CLAUDE.md / AGENTS.md):

```bash
bd create \
  --title "<title>" \
  --type task \
  --priority 2 \
  --parent holomush-jxo8.3 \
  --description "$(cat <<'EOF'
**Goal:** <one-sentence>

**Design reference:** docs/superpowers/specs/2026-05-07-event-payload-crypto-phase5-totp-substrate-design.md (READY at design-reviewer R5)
**Plan reference:** docs/superpowers/plans/2026-05-07-event-payload-crypto-phase5-totp-substrate.md § Task <Tn>

**TDD acceptance criteria:**
<task-specific>

**Verification steps:**
- task lint -- ./<scope>/...
- task test -- ./<scope>/...
- (final bead: task pr-prep)

**Files touched:**
<task-specific>

**Dependencies:** <bead IDs or "None">

**Out of scope:** <task-specific>
EOF
)"
```

Per-bead specifics below. The `bead-chain-from-plan` skill materializes
each bead by interpolating these fields. To minimize duplication, "common"
sections (Design reference, Plan reference, top-level Verification) are
shown once above; per-bead blocks below carry the task-specific fields only.

#### jxo8.3.1 — Package skeleton (Clock + typed errors)

```text
Goal: Add `internal/totp/` package skeleton with Clock interface (RealClock + FakeClock) and typed errors via oops.Code.
TDD acceptance criteria:
  - TestRealClockReturnsCurrentTime
  - TestFakeClockReturnsAndAdvances
  - TestErrorCodesPresent (TOTP_BOOTSTRAP_CONSUMED, TOTP_ALREADY_ENROLLED, TOTP_NOT_ENROLLED, TOTP_INVALID_RECOVERY_CODE)
  - TestNewErrTOTPLockedCarriesUntil
Files touched:
  - go.mod (add github.com/pquerna/otp)
  - internal/totp/clock.go (new)
  - internal/totp/clock_test.go (new)
  - internal/totp/errors.go (new)
  - internal/totp/errors_test.go (new)
Dependencies: None.
Out of scope: Service / Repository / provisioning helpers (later beads).
```

#### jxo8.3.2 — Migration 000019

```text
Goal: Create `player_totp`, `player_totp_recovery_codes`, `crypto_bootstrap_state` tables via migration 000019.
TDD acceptance criteria:
  - Existing TestAutoMigrate integration test applies the new migration cleanly (pg testcontainer)
Files touched:
  - internal/store/migrations/000019_create_player_totp.up.sql (new)
  - internal/store/migrations/000019_create_player_totp.down.sql (new)
Dependencies: None.
Out of scope: Repo wiring, Go-side migration logic (none — migrations are SQL-only).
```

#### jxo8.3.3 — Types + provisioning helpers

```text
Goal: Define Service/Repository interfaces, BootstrapResult/EnrollResult/VerifyResult/ConsumeRecoveryResult/ClearResult result structs, ClearReason, EnrollmentRecord/HashedRecoveryCode/VerifyState; implement generateSecret/buildProvisioningURI/generateRecoveryCodes.
TDD acceptance criteria:
  - TestGenerateSecretIs32CharBase32, TestGenerateSecretIsRandom
  - TestBuildProvisioningURI
  - TestGenerateRecoveryCodes (count, format, uniqueness)
Files touched:
  - internal/totp/types.go (new)
  - internal/totp/provisioning.go (new)
  - internal/totp/provisioning_test.go (new)
Dependencies: jxo8.3.1.
Out of scope: Repository implementation, Service implementation, CLI.
```

#### jxo8.3.4 — Audit subject helpers (RESERVED)

```text
Goal: Define audit-event subject builders (events.<game>.system.crypto_totp.<scope>.<event>) and payload structs reserved for future server-side callers (sub-epic D). Sub-epic A's Service does NOT emit (R5 Option Y).
TDD acceptance criteria:
  - TestSubjectBuilders (5 subject patterns)
  - TestEventTypeConstants (5 event-type strings)
Files touched:
  - internal/totp/audit.go (new)
  - internal/totp/audit_test.go (new)
Dependencies: None.
Out of scope: Audit event emission (deferred to D); JSON marshaling tests for payload structs (kept simple — payload struct shape is verified by Go's JSON encoder, not by test).
```

#### jxo8.3.5 — Postgres Repository

```text
Goal: Implement totp.Repository against Postgres using existing internal/world/postgres/transactor.go pattern (txKey context-stash + querierFromCtx fallback). Add BootstrapEnrollAtomic (single PG txn wrapping bootstrap_state + player_totp + recovery_codes inserts).
TDD acceptance criteria:
  - TestRepoBootstrapClaimConcurrentExactlyOneSucceeds (real PG; INV-A2)
  - TestRepoBootstrapEnrollAtomicRollsBackOnInsertError (real PG; INV-A15)
  - Per-method one-shot integration tests for PlayerExists, PlayerIDFromUsername, IsEnrolled, InsertEnrollment round-trip, LoadEnrollment ErrNotEnrolled, IncrementFailedAttempts (lockout transition), MarkVerified, ConsumeRecoveryCode (single-use; INV-A6 substrate), ClearEnrollment (returns wasEnrolled correctly; INV-A7 substrate)
Files touched:
  - internal/totp/repo.go (new)
  - internal/totp/repo_integration_test.go (new; build tag integration)
Dependencies: jxo8.3.2, jxo8.3.3.
Out of scope: Service-layer logic; race conditions beyond INV-A2 (e.g., concurrent recovery-code consumption — covered by FOR UPDATE in ConsumeRecoveryCode but not separately tested at this bead).
```

#### jxo8.3.6 — Service skeleton + IsEnrolled + mockery

```text
Goal: Service interface + Config + service struct + NewService constructor (NO AuditPublisher param per R5 Option Y); implement IsEnrolled. Add mockery v3 packages entry to .mockery.yaml; generate mock_Repository.go via task mocks:generate.
TDD acceptance criteria:
  - TestIsEnrolledReturnsRepoResult
  - TestIsEnrolledPropagatesError
Files touched:
  - internal/totp/service.go (new — skeleton only)
  - internal/totp/service_isenrolled_test.go (new)
  - internal/totp/mocks/mock_Repository.go (generated)
  - .mockery.yaml (modify — add packages entry)
Dependencies: jxo8.3.3.
Out of scope: Service.BootstrapEnroll/Enroll/Verify/ConsumeRecoveryCode/ClearTOTP (later beads).
```

#### jxo8.3.7 — Service.BootstrapEnroll

```text
Goal: Implement Service.BootstrapEnroll using Repository.BootstrapEnrollAtomic (single PG txn) + KEK-wrapping the secret + Argon2id-hashing recovery codes. Returns BootstrapResult carrying audit metadata for caller emission.
TDD acceptance criteria:
  - TestBootstrapEnrollRefusesUnknownPlayer (INV-A13)
  - TestBootstrapEnrollRefusesAfterFirstSuccess (INV-A1; mock-level)
  - TestBootstrapEnrollWrapsSecretWithKEK (INV-A9)
  - TestBootstrapEnrollHashesRecoveryCodes (INV-A11)
  - TestBootstrapEnrollPropagatesAtomicError (INV-A15)
  - TestBootstrapResultCarriesAuditMetadata
Files touched:
  - internal/totp/service.go (modify — append BootstrapEnroll + buildEnrollment helper)
  - internal/totp/service_bootstrap_test.go (new)
  - internal/eventbus/crypto/kek/mocks/mock_Provider.go (generated)
  - .mockery.yaml (modify — add kek.Provider packages entry)
Dependencies: jxo8.3.5, jxo8.3.6.
Out of scope: Audit emission (deferred to D); Enroll/Verify/Recovery (later beads).
```

#### jxo8.3.8 — Service.Enroll

```text
Goal: Implement Service.Enroll for self-enrollment (refuses if already enrolled; otherwise generates fresh material via shared buildEnrollment helper).
TDD acceptance criteria:
  - TestEnrollRefusesIfAlreadyEnrolled
  - TestEnrollSucceedsForUnenrolledPlayer
  - TestEnrollPropagatesInsertError
Files touched:
  - internal/totp/service.go (modify — append Enroll)
  - internal/totp/service_enroll_test.go (new)
Dependencies: jxo8.3.7.
Out of scope: Verify, Recovery (later beads).
```

#### jxo8.3.9 — Service.Verify (replay + lockout + skew)

```text
Goal: Implement Service.Verify with constant-time per-step hotp.GenerateCode comparison across the skew=1 window, replay defense via last_used_step, lockout at 5 failures (15min), and result-struct return carrying Outcome + LockoutTransition + LockedUntil for caller-side audit emission.
TDD acceptance criteria:
  - TestVerifyReturnsErrCodeReuseOnReplay (INV-A3)
  - TestVerifyLocksAfterFiveFailures (INV-A4)
  - TestVerifyReturnsOutcomeLockedWhenLocked (INV-A4 partial)
  - TestVerifySuccessClearsFailedAttempts (INV-A5)
  - TestVerifyAcceptsAdjacentSteps (INV-A12; three subtests for offset -1/0/+1)
  - TestVerifyFailurePathsDoNotMutateSuccessFields (INV-A14)
  - TestVerifyReturnsOutcomeNotEnrolled
  - TestVerifyResultPopulatesLockoutTransitionOnFirstLock
Files touched:
  - internal/totp/service.go (modify — append Verify, errCodeReuseRollback sentinel)
  - internal/totp/service_verify_test.go (new — uses runInTxn helper with EXPECT().InTransaction(...).RunAndReturn(...) per canonical mockery v3 pattern)
Dependencies: jxo8.3.5, jxo8.3.6.
Out of scope: Audit emission of crypto.totp_locked (deferred to D, which inspects VerifyResult.LockoutTransition).
```

#### jxo8.3.10 — Service.ConsumeRecoveryCode + ClearTOTP

```text
Goal: Implement Service.ConsumeRecoveryCode (delegates to repo's atomic consume; returns ConsumeRecoveryResult with the consumed RecoveryCodeID for caller audit) and Service.ClearTOTP (delegates to repo.ClearEnrollment; returns ClearResult with WasEnrolled flag).
TDD acceptance criteria:
  - TestConsumeRecoveryCodePropagatesInvalidCode (INV-A6 propagation)
  - TestConsumeRecoveryCodeReturnsConsumedID
  - TestClearTOTPCallsClearEnrollment (INV-A7 delegation)
  - TestClearTOTPDoesNotTouchBootstrapState (INV-A8 — assert mock expectations exclude any bootstrap-state-related calls)
  - TestClearTOTPReturnsWasEnrolledFlag
Files touched:
  - internal/totp/service.go (modify — append ConsumeRecoveryCode, ClearTOTP)
  - internal/totp/service_recovery_test.go (new)
Dependencies: jxo8.3.5, jxo8.3.6.
Out of scope: Audit emission (deferred to D).
```

#### jxo8.3.11 — `holomush admin` CLI scaffolding

```text
Goal: Add `holomush admin` parent command and `holomush admin totp` parent stub; register NewAdminCmd in root.go.
TDD acceptance criteria:
  - TestAdminCmdRegistered
  - TestAdminTOTPCmdRegistered
Files touched:
  - cmd/holomush/cmd_admin.go (new)
  - cmd/holomush/cmd_admin_totp.go (new — parent only)
  - cmd/holomush/cmd_admin_test.go (new)
  - cmd/holomush/root.go (modify — register NewAdminCmd)
Dependencies: None.
Out of scope: Subcommand handlers (T13/T14 beads).
```

#### jxo8.3.12 — CLI dependency wiring

```text
Goal: Implement cmd_admin_totp_deps.go: wire pg pool (pgxpool.New + getDatabaseURL), KEK provider (matching server's construction sequence per T12.0), auth.Service (NewAuthService + NewPostgresPlayerSessionStore + NewPlayerRepository), totp.Service. Includes stub-replacement gate (rg "buildKEKProviderFromConfig: not implemented" cmd/holomush/ must return zero hits before commit).
TDD acceptance criteria:
  - go build ./cmd/holomush/... compiles green (no test code at this bead)
Files touched:
  - cmd/holomush/cmd_admin_totp_deps.go (new)
Dependencies: jxo8.3.6.
Out of scope: Subcommand handlers; runtime exercise of the wiring (covered by T13/T14 + T17 E2E).
```

#### jxo8.3.13 — `bootstrap-enroll` CLI

```text
Goal: Implement `holomush admin totp bootstrap-enroll <username>` subcommand. Calls totp.Service.BootstrapEnroll; prints Enrollment to stdout (provisioning URI + secret + 10 recovery codes); notes the no-audit-event gap per Option Y.
TDD acceptance criteria:
  - TestAdminTOTPBootstrapEnrollExists (cobra tree presence)
Files touched:
  - cmd/holomush/cmd_admin_totp.go (modify — add subcommand handler + printEnrollment + formatSecretForDisplay helpers)
  - cmd/holomush/cmd_admin_test.go (modify — add presence test)
Dependencies: jxo8.3.7, jxo8.3.11, jxo8.3.12.
Out of scope: enroll, recover (T14 bead); E2E (T17 bead).
```

#### jxo8.3.14 — `enroll` + `recover` CLIs

```text
Goal: Implement `holomush admin totp enroll [--username]` (creds-authenticated via auth.ValidateCredentials → totp.Enroll) and `holomush admin totp recover [--username]` (recovery-code-authenticated → totp.ConsumeRecoveryCode → totp.ClearTOTP). Use bufio + golang.org/x/term.ReadPassword for secret input.
TDD acceptance criteria:
  - TestAdminTOTPEnrollIsRegistered
  - TestAdminTOTPRecoverIsRegistered
  - (Behavioral tests deferred to T17 E2E; cobra unit tests cover wiring only)
Files touched:
  - cmd/holomush/cmd_admin_totp.go (modify — add enroll + recover handlers)
  - cmd/holomush/cmd_admin_test.go (modify — add presence tests)
Dependencies: jxo8.3.8, jxo8.3.10, jxo8.3.11, jxo8.3.12.
Out of scope: E2E (T17 bead).
```

#### jxo8.3.15 — kek.Provider docstring generalization

```text
Goal: Update internal/eventbus/crypto/kek/provider.go's package + Wrap docstring to reflect that the provider wraps "opaque secret bytes" (DEKs in Phase 2; TOTP secrets in Phase 5; future tier in subsequent phases). Documentary-only; no API change.
TDD acceptance criteria:
  - Existing kek tests pass unchanged (no behavior change)
Files touched:
  - internal/eventbus/crypto/kek/provider.go (modify — package + interface doc comments)
Dependencies: None.
Out of scope: Any API change to the kek.Provider interface.
```

#### jxo8.3.16 — ABAC forbid seeds (INV-A16)

```text
Goal: Add two forbid seed policies (seed:deny-events-system-crypto-totp-read-character/-plugin) parallel to existing audit.* denies, denying character/plugin principals from reading streams matching events.*.system.crypto_totp.*. Update existing seed-count and forbid-count assertions across three tests.
TDD acceptance criteria:
  - TestSeedPoliciesIncludesEventsSystemCryptoTotpDenyForCharacter (INV-A16)
  - TestSeedPoliciesIncludesEventsSystemCryptoTotpDenyForPlugin (INV-A16)
  - TestSeedPoliciesCount (updated 28→30)
  - TestSeedPoliciesEffectDistribution (updated forbid count 3→5)
  - TestBootstrapSetsCorrectPolicyEffect (updated forbid count 3→5)
  - TestSeedPoliciesExpectedNames (updated to include the two new names)
  - TestSeedPoliciesForbidPoliciesAreExpected (updated expectedForbids map)
Files touched:
  - internal/access/policy/seed.go (modify — append two new SeedPolicy entries)
  - internal/access/policy/seed_test.go (modify — counts + names + new tests)
  - internal/access/policy/bootstrap_test.go (modify — forbid count 3→5)
Dependencies: None.
Out of scope: Broader events.*.system.* ABAC denies (D's territory).
```

#### jxo8.3.17 — E2E lifecycle test

```text
Goal: Ginkgo lifecycle test exercising bootstrap → enroll → verify → recover → re-enroll using real PG + real KEK file. NO audit-publisher assertions (sub-epic A doesn't emit per Option Y; audit-row assertions live in sub-epic D's E2E once D ships).
TDD acceptance criteria:
  - "supports the full bootstrap → enroll → verify → recover → re-enroll cycle" (Ginkgo It)
  - Asserts: BootstrapEnroll succeeds; second BootstrapEnroll returns ErrBootstrapAlreadyConsumed; Verify with current-step code returns Outcome=OutcomeOK; replay returns Outcome=OutcomeCodeReuse; ConsumeRecoveryCode succeeds; ClearTOTP succeeds; IsEnrolled false after clear; Enroll re-enrolls with fresh codes
Files touched:
  - test/integration/totp_e2e_test.go (new; build tag integration)
Dependencies: jxo8.3.13, jxo8.3.14.
Out of scope: Audit-table assertions; sub-epic D's break-glass flow.
```

### Closing-out operations

- **Existing beads to close on completion:** none (none of jxo8.3's siblings under jxo8 represent superseded work).
- **Existing beads to update:** `holomush-jxo8.3` itself — set `--notes "Plan: docs/superpowers/plans/2026-05-07-event-payload-crypto-phase5-totp-substrate.md (R5 spec; v2 plan landed; chain materialized as jxo8.3.1..17)"` after chain materialization succeeds (per T18.2).
- **Follow-up beads to file (P3):**
  - "Sub-epic A: in-game/admin grant UX for crypto.operator capability" — referenced in spec §"Pinned by the decomposition spec" Defaults sub-epic A but deferred per the decomposition spec.
  - "Sub-epic A: TOTP recovery-code rate limiting" — currently no rate limit per spec Defaults; file as a P3 for future tightening if attack data warrants.

### `bd dep add` edges

```bash
bd dep add holomush-jxo8.3.3  holomush-jxo8.3.1   # types use Clock/errors
bd dep add holomush-jxo8.3.5  holomush-jxo8.3.2   # repo needs migration
bd dep add holomush-jxo8.3.5  holomush-jxo8.3.3   # repo uses types
bd dep add holomush-jxo8.3.6  holomush-jxo8.3.3   # Service skeleton uses types
bd dep add holomush-jxo8.3.7  holomush-jxo8.3.5   # BootstrapEnroll → Repo.BootstrapEnrollAtomic
bd dep add holomush-jxo8.3.7  holomush-jxo8.3.6   # BootstrapEnroll on Service skeleton
bd dep add holomush-jxo8.3.8  holomush-jxo8.3.7   # Enroll shares buildEnrollment
bd dep add holomush-jxo8.3.9  holomush-jxo8.3.5   # Verify → Repo
bd dep add holomush-jxo8.3.9  holomush-jxo8.3.6   # Verify on Service skeleton
bd dep add holomush-jxo8.3.10 holomush-jxo8.3.5   # Recovery → Repo
bd dep add holomush-jxo8.3.10 holomush-jxo8.3.6   # Recovery on Service skeleton
bd dep add holomush-jxo8.3.12 holomush-jxo8.3.6   # CLI deps need Service constructor
bd dep add holomush-jxo8.3.13 holomush-jxo8.3.7   # bootstrap-enroll → BootstrapEnroll
bd dep add holomush-jxo8.3.13 holomush-jxo8.3.11  # CLI subcommand → parent
bd dep add holomush-jxo8.3.13 holomush-jxo8.3.12  # CLI subcommand → deps
bd dep add holomush-jxo8.3.14 holomush-jxo8.3.8   # enroll → Enroll
bd dep add holomush-jxo8.3.14 holomush-jxo8.3.10  # recover → Recovery + Clear
bd dep add holomush-jxo8.3.14 holomush-jxo8.3.11  # CLI subcommand → parent
bd dep add holomush-jxo8.3.14 holomush-jxo8.3.12  # CLI subcommand → deps
bd dep add holomush-jxo8.3.17 holomush-jxo8.3.13  # E2E exercises bootstrap-enroll
bd dep add holomush-jxo8.3.17 holomush-jxo8.3.14  # E2E exercises enroll + recover
```

T15 (kek docstring) and T16 (ABAC seeds) have no `bd dep add` edges — they touch unrelated files and can land at any point in the implementation order.

---

## Self-Review

### Spec coverage check

| Spec section / invariant | Covered by | Notes |
|---|---|---|
| §"Architecture" — package + migration | T1, T2 | + ABAC seeds added to package layout |
| §"Go API surface" — Service + result structs | T3, T6, T7-T10 | Returns BootstrapResult/EnrollResult/VerifyResult/ConsumeRecoveryResult/ClearResult |
| §"Verify mechanics" — replay + lockout + skew + hotp.GenerateCode loop | T9 | tryStep loop var (M1); subtle.ConstantTimeCompare; emit-AFTER-COMMIT semantics encoded in LockoutTransition flag |
| §"CLI commands" — bootstrap-enroll / enroll / recover | T11 (parent), T13, T14 | T12 adds dep wiring |
| §"Audit events emitted" — subject namespace + emission deferral | T4 (helpers RESERVED), T16 (ABAC seeds) | No emission in any sub-epic A task |
| §"Bootstrap closure mechanism" — PG-only atomicity | T5 (BootstrapEnrollAtomic), T7 (Service.BootstrapEnroll) | One PG txn wraps row + enrollment + codes |
| §"`crypto.totp_locked` is the exception" — caller-side emit | T9 returns LockoutTransition | Consumer is sub-epic D (out of scope here) |
| §"Threat-model coverage" | T7 (KEK-wrap), T9 (lockout, replay) | — |
| §"Failure modes" | T2 (FK CASCADE), T5 (txn rollback), T7 (atomic propagation) | — |
| §"Dependencies" — kek docstring | T15 | Documentary-only |
| INV-A1 (refuse after first) | T7 (mock-level), T5 (real-PG via BootstrapEnrollAtomic) | — |
| INV-A2 (concurrent atomicity) | T5 (real-PG INV-A2 test) | — |
| INV-A3 (replay) | T9 | — |
| INV-A4 (lockout) | T9 | — |
| INV-A5 (success resets) | T9 | — |
| INV-A6 (recovery single-use) | T5 (repo) + T10 (service propagation) | — |
| INV-A7 (Clear deletes both) | T5 (repo) + T10 | — |
| INV-A8 (Clear doesn't touch bootstrap) | T10 | — |
| INV-A9 (KEK-wrap) | T7 | — |
| INV-A10 (retired in R5) | T7-T10 result-struct metadata tests replace this | Was: audit-emission tests |
| INV-A11 (Argon2id codes) | T7 | — |
| INV-A12 (skew window) | T9 | — |
| INV-A13 (refuse unknown player) | T7 | — |
| INV-A14 (failure paths don't mutate success fields) | T9 | — |
| INV-A15 (PG-only atomicity rollback) | T5 + T7 propagation | — |
| INV-A16 (ABAC seeds) | T16 | — |

### Placeholder scan

- T12 step 12.2 has explicit `/* PLAN AUTHOR: replace */` comments because the actual `kek.NewFileSource` arguments and `auth.NewService` arguments depend on signatures the plan-author must read at execution time — but the step explicitly directs them to do that and forbids shipping with placeholder markers. Acceptable.
- T9 step 9.1 uses the sentinel-error `__rollback_for_code_reuse` to convey "rollback this txn" through `Repository.InTransaction`. Step 9.1's prose acknowledges this and offers a refactor path. Acceptable; documents the trade-off.

### Type consistency

- `Repository` interface (T3) declares all methods used in T5 (impl) and T7-T10 (service callers). Method names + arg types match: `BootstrapClaim`, `BootstrapEnrollAtomic`, `PlayerExists`, `PlayerIDFromUsername`, `IsEnrolled`, `InsertEnrollment`, `LoadEnrollment`, `IncrementFailedAttempts`, `MarkVerified`, `ConsumeRecoveryCode`, `ClearEnrollment`, `InTransaction`. ✓
- `EnrollmentRecord` fields (T3) match T5's `InsertEnrollment` SQL and T7's `buildEnrollment` constructor. ✓
- `VerifyState` fields (T3) match T5's `LoadEnrollment` / `IncrementFailedAttempts` SCAN destinations and T9's reads. ✓
- `BootstrapResult` / `EnrollResult` / `VerifyResult` / `ConsumeRecoveryResult` / `ClearResult` field names (T3) match the prose references in spec §"Audit events emitted" and the audit-payload structs in T4. ✓
- `Clock` interface (T1) used by `service` (T6) and exercised by `FakeClock` in T7-T10 tests. ✓

No type drift detected.

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-05-07-event-payload-crypto-phase5-totp-substrate.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach?**
