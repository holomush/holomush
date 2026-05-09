# Phase 5 Sub-Epic D Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the OperatorAuthProvider, `admin_approvals` dual-control, `crypto.policy_set` hash chain, and TOTP-audit decorator that integrate sub-epics A/B/C into a working break-glass auth substrate (`holomush-jxo8.6`). Unblocks E (Rekey) and F (AdminReadStream).

**Architecture:** Server-side `OperatorAuthProvider` issues 10-min ULID session tokens after a 6-step check sequence (creds → IsEnrolled → Verify → HasPlayerGrant → PlayerHasRole(RoleAdmin) → PeerCred capture). Three new RPCs land on sub-epic C's UDS admin socket: `Authenticate`, `Approve`, `ResetTOTP`. Hash-chained `crypto.policy_set` events live in `events_audit.envelope` BYTEA; verifier in Bootstrap (fail-closed); emitter in new `CryptoPolicySubsystem` after AuditProjection (fail-closed). `AuditingService` decorator wraps `totp.Service` to emit `crypto.totp_*` events on observed state transitions.

**Tech Stack:** Go 1.x, ConnectRPC, PostgreSQL (`pgxpool`), `oklog/ulid/v2`, `samber/oops`, `google.golang.org/protobuf` (deterministic marshal — version pinned), `github.com/cyberphone/json-canonicalization` (RFC 8785 JCS — pseudo-version pinned), `slog`, `mockery v3` for unit-test mocks, Ginkgo for e2e.

**Spec reference:** `docs/superpowers/specs/2026-05-09-event-payload-crypto-phase5-sub-epic-d-design.md`. Each task names the §section + `INV-D*` invariants it satisfies.

**Workflow:** Single isolated jj workspace already in use (`phase5-sub-epic-d`); commit per step using project conventions (no editor — `JJ_EDITOR=true jj --no-pager describe -m "..."` or `jj --no-pager commit -m "..."`); never `git commit`. Run `task lint && task test` after each task; `task test:int` for tasks marked `[INT]`; `task pr-prep` once at the end before push.

---

## Task 1: Migration 000020 — `admin_approvals` table

**Spec:** §5 schema. Invariants: INV-D5 (TTL via expires_at filter), INV-D6 (self-approval rejection done at MarkApproved query level).

**Files:**

- Create: `internal/store/migrations/000020_create_admin_approvals.up.sql`
- Create: `internal/store/migrations/000020_create_admin_approvals.down.sql`
- Test: `internal/store/migrate_integration_test.go` (modify `expectedTables` slice)

- [ ] **Step 1: Write the up migration**

Path: `internal/store/migrations/000020_create_admin_approvals.up.sql`

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Phase 5 sub-epic D: admin_approvals table for dual-control approval rows.
-- Idempotent (project rule per CLAUDE.md): IF NOT EXISTS guards.

CREATE TABLE IF NOT EXISTS admin_approvals (
    request_id              BYTEA PRIMARY KEY,         -- 16-byte ULID
    primary_player_id       BYTEA NOT NULL,
    op_kind                 TEXT NOT NULL,             -- "rekey" | "admin_read_stream"
    op_args_hash            BYTEA NOT NULL,            -- 32-byte SHA-256
    expires_at              TIMESTAMPTZ NOT NULL,
    approved_at             TIMESTAMPTZ NULL,
    approved_by_player_id   BYTEA NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_admin_approvals_pending
    ON admin_approvals (request_id)
    WHERE approved_at IS NULL;
```

- [ ] **Step 2: Write the down migration**

Path: `internal/store/migrations/000020_create_admin_approvals.down.sql`

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP INDEX IF EXISTS idx_admin_approvals_pending;
DROP TABLE IF EXISTS admin_approvals;
```

- [ ] **Step 3: Modify `expectedTables` slice in migration integration test**

In `internal/store/migrate_integration_test.go`, add `"admin_approvals"` to the `expectedTables` slice (alphabetic insertion).

- [ ] **Step 4: Run integration test to verify**

Run: `task test:int -- ./internal/store/`

Expected: PASS — `TestAllTablesPresentAfterFullMigration` (or equivalent) sees `admin_approvals` after migrations apply.

- [ ] **Step 5: Run unit tests as a smoke check**

Run: `task test -- ./internal/store/...`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
jj --no-pager commit -m "feat(crypto): migration 000020 admin_approvals (holomush-jxo8.6 T1)

Phase 5 sub-epic D table for dual-control approval rows. Schema per spec
§5: BYTEA request_id PK, primary_player_id, op_kind, op_args_hash (32-byte
SHA-256), expires_at, approved_at NULL, approved_by_player_id NULL,
created_at. Partial index on (request_id) WHERE approved_at IS NULL
supports the unapproved-not-expired hot path. Idempotent IF NOT EXISTS
guards per project migration rules."
```

---

## Task 2: Add `lifecycle.SubsystemCryptoPolicy` constant

**Spec:** §3 architecture, §6 emitter. Invariants: depended on by INV-D17.

**Files:**

- Modify: `internal/lifecycle/subsystem.go`
- Modify: `internal/lifecycle/subsystem_test.go` (or equivalent constant test)

- [ ] **Step 1: Add the constant**

Add `SubsystemCryptoPolicy` to the SubsystemID const block. Convention: alphabetical-ish; place near `SubsystemAuditProjection` since both are post-EventBus.

- [ ] **Step 2: Update the `String()` method (if hand-rolled enum-string)**

If `SubsystemID.String()` is a switch, add the new case returning `"CryptoPolicy"`.

- [ ] **Step 3: Update any "all subsystems" test**

If a unit test enumerates all subsystem IDs (e.g., `TestSubsystemAdminSocketConstantExists`-like), append the new constant.

- [ ] **Step 4: Run tests**

Run: `task test -- ./internal/lifecycle/...`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj --no-pager commit -m "feat(crypto): add SubsystemCryptoPolicy lifecycle ID (holomush-jxo8.6 T2)

Reserved for the new CryptoPolicySubsystem that emits the
crypto.policy_set chain event after AuditProjection per design spec
\$6. No behavior change yet — the subsystem itself lands in T11."
```

---

## Task 3: Pin `cyberphone/json-canonicalization` and `google.golang.org/protobuf`

**Spec:** §6 chain hash; INV-D13, INV-D18.

**Files:**

- Modify: `go.mod`
- Modify: `go.sum` (auto-regenerated)

- [ ] **Step 1: Add JCS dependency at the pinned pseudo-version**

Run: `go get github.com/cyberphone/json-canonicalization@v0.0.0-20241213102144-19d51d7fe467`

- [ ] **Step 2: Verify the protobuf-go module is present and pin its current version**

Run: `go list -m google.golang.org/protobuf` to discover the current resolved version (e.g., `v1.36.6`). The pin is "whatever is currently resolved at this commit"; the meta-test in T24 locks the resolved version into a test constant.

- [ ] **Step 3: Add a comment to `go.mod` documenting the pins as load-bearing**

Use a `// indirect` style comment block above each pinned require directive:

```
// crypto.policy_set chain hashing: SHA-256 over RFC 8785 JCS-canonicalized
// JSON. Pin pseudo-version is load-bearing — switching libraries is a
// chain-breaking master-spec amendment per INV-D13.
require github.com/cyberphone/json-canonicalization v0.0.0-20241213102144-19d51d7fe467

// op_args_hash cross-binary stability: pin protobuf-go since
// proto.MarshalOptions{Deterministic: true} is documented stable within
// a binary version but not guaranteed across releases (INV-D18).
require google.golang.org/protobuf v<resolved-version>
```

- [ ] **Step 4: Run tidy + build**

Run: `go mod tidy && task build`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj --no-pager commit -m "chore(crypto): pin JCS canonicalizer + protobuf-go (holomush-jxo8.6 T3)

INV-D13 pins github.com/cyberphone/json-canonicalization at the Dec 2024
pseudo-version for crypto.policy_set chain hashing. INV-D18 pins
google.golang.org/protobuf for op_args_hash cross-binary stability. Both
pins are load-bearing on chain-of-custody integrity; meta-tests in T24
will lock them at the import path level."
```

---

## Task 4: `RoleStore.PlayerHasRole` — interface extension

**Spec:** §4 role helper. Invariants: INV-D19, INV-D1 (step 5).

**Files:**

- Modify: `internal/store/role_store.go`
- Modify: `internal/store/role_store_test.go` (unit + integration coverage)
- Modify: `internal/bootstrap/admin_test.go::fakeRoleStore` (add stub method)
- Modify: any other in-tree fake of `RoleStore` (use `mcp__probe__search_code` to find them)

- [ ] **Step 1: Find every fake of `RoleStore` in the tree**

Run via `mcp__probe__search_code`: query `RoleStore fakeRoleStore mock`.

Expected: finds `internal/bootstrap/admin_test.go::fakeRoleStore`. Note any others.

- [ ] **Step 2: Write the unit test for the new method on PostgresRoleStore**

In `internal/store/role_store_test.go`, add:

```go
func TestPlayerHasRoleReturnsTrueWhenAnyCharacterHasRole(t *testing.T) {
    // Integration test — requires a real PG. Wrap in build tag if
    // role_store_test.go is currently unit-only.
    t.Skip("see role_store_integration_test.go for the real test")
}
```

(This stub gates compile-time correctness; the real test in step 4 covers behavior.)

- [ ] **Step 3: Add the method to the `RoleStore` interface**

```go
type RoleStore interface {
    GetRoles(ctx context.Context, characterID string) ([]string, error)
    AddRole(ctx context.Context, characterID, role string) error
    RemoveRole(ctx context.Context, characterID, role string) error
    // PlayerHasRole returns true iff at least one character belonging to
    // playerID has the given role assigned. Used by sub-epic D's
    // OperatorAuthProvider to gate operator authentication.
    PlayerHasRole(ctx context.Context, playerID, role string) (bool, error)
}
```

- [ ] **Step 4: Implement on PostgresRoleStore**

```go
// PlayerHasRole returns true iff any character of playerID has role.
func (s *PostgresRoleStore) PlayerHasRole(ctx context.Context, playerID, role string) (bool, error) {
    var found int
    err := s.pool.QueryRow(ctx, `
        SELECT 1
          FROM character_roles cr
          JOIN characters c ON cr.character_id = c.id
         WHERE c.player_id = $1
           AND cr.role     = $2
         LIMIT 1
    `, playerID, role).Scan(&found)
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return false, nil
        }
        return false, oops.Code("ROLE_PLAYER_HAS_ROLE_FAILED").
            With("player_id", playerID).
            With("role", role).Wrap(err)
    }
    return found == 1, nil
}
```

- [ ] **Step 5: Update `internal/bootstrap/admin_test.go::fakeRoleStore`**

Add a `playerRoles map[string][]string` field; implement:

```go
func (f *fakeRoleStore) PlayerHasRole(_ context.Context, playerID, role string) (bool, error) {
    for _, r := range f.playerRoles[playerID] {
        if r == role {
            return true, nil
        }
    }
    return false, nil
}
```

- [ ] **Step 6: Add the integration test in a new file (or extend the existing one)**

Path: `internal/store/role_store_integration_test.go`

```go
//go:build integration

package store_test

// imports omitted for brevity — see neighboring integration tests for the pattern

func TestPlayerHasRole_ReturnsTrueForPlayerWithAdminCharacter(t *testing.T) {
    pool, cleanup := newTestPool(t)
    defer cleanup()

    // Seed: one player, one character, one admin role.
    playerID := ulid.Make().String()
    charID := ulid.Make().String()
    _, err := pool.Exec(context.Background(), `INSERT INTO players (id, username, password_hash, created_at, updated_at)
        VALUES ($1, $2, $3, now(), now())`, playerID, "alice", "hash")
    require.NoError(t, err)
    _, err = pool.Exec(context.Background(), `INSERT INTO characters (id, player_id, name, created_at, updated_at)
        VALUES ($1, $2, $3, now(), now())`, charID, playerID, "Alice")
    require.NoError(t, err)
    rs := store.NewPostgresRoleStore(pool)
    require.NoError(t, rs.AddRole(context.Background(), charID, access.RoleAdmin))

    has, err := rs.PlayerHasRole(context.Background(), playerID, access.RoleAdmin)
    require.NoError(t, err)
    require.True(t, has)
}

func TestPlayerHasRole_ReturnsFalseForPlayerWithoutAnyAdminCharacter(t *testing.T) {
    // ... same seed but RemoveRole the admin role; assert false
}

func TestPlayerHasRole_ReturnsFalseForUnknownPlayer(t *testing.T) {
    pool, cleanup := newTestPool(t)
    defer cleanup()
    rs := store.NewPostgresRoleStore(pool)
    has, err := rs.PlayerHasRole(context.Background(), ulid.Make().String(), access.RoleAdmin)
    require.NoError(t, err)
    require.False(t, has)
}
```

- [ ] **Step 7: Run integration tests**

Run: `task test:int -- ./internal/store/...`

Expected: PASS — three new tests green.

- [ ] **Step 8: Run full unit suite (catches missed fakes)**

Run: `task test`

Expected: PASS — every implementor of `RoleStore` (production + fakes) compiles.

- [ ] **Step 9: Commit**

```bash
jj --no-pager commit -m "feat(access): RoleStore.PlayerHasRole helper (holomush-jxo8.6 T4)

Adds PlayerHasRole(ctx, playerID, role) to the RoleStore interface,
implemented on PostgresRoleStore via a single character_roles ⨝
characters JOIN on player_id. Used by sub-epic D's OperatorAuthProvider
step 5 (master spec §5.9) to enforce the RoleAdmin AND crypto.operator
conjunction (decomposition spec line 89/177). Updates
internal/bootstrap/admin_test.go::fakeRoleStore to keep test compile
green. INV-D19 named test TestAuthenticateRejectsPlayerWithoutAdminRole
lands in T6."
```

---

## Task 5: `SessionStore` — in-memory ULID session map

**Spec:** §4 SessionStore. Invariants: INV-D2, INV-D3.

**Files:**

- Create: `internal/admin/auth/session.go`
- Create: `internal/admin/auth/session_test.go`
- Create: `internal/admin/auth/types.go` (OperatorIdentity, AuthRequest, etc.)

- [ ] **Step 1: Write `types.go`**

Path: `internal/admin/auth/types.go`

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package adminauth provides the OperatorAuthProvider for sub-epic D's
// break-glass admin authentication path. See docs/superpowers/specs/
// 2026-05-09-event-payload-crypto-phase5-sub-epic-d-design.md.
package adminauth

import (
    "context"
    "time"

    "github.com/holomush/holomush/internal/admin/socket"
)

// AuthRequest is the credential bundle the CLI collected via prompts and
// sends in the Authenticate RPC payload. Per spec §4.
type AuthRequest struct {
    Username string
    Password string
    TOTPCode string
    PeerCred socket.PeerCred // captured by middleware; for audit only
}

// OperatorIdentity is the audit record shape per master spec §4.6 and
// design spec §4. Stored in the SessionStore keyed by a ULID token.
type OperatorIdentity struct {
    PlayerID           string // ULID
    OSUser             string // "uid=1001 (alice)" — captured, never gates
    TOTPVerified       bool   // always true on successful Authenticate
    AuthProviderName   string // "ingame-creds-totp"
    ProviderSpecificID string // empty for in-game provider
}

// OperatorAuthProvider authenticates an operator for destructive or
// information-disclosure admin operations. Pluggable like KEKProvider.
type OperatorAuthProvider interface {
    Name() string
    Authenticate(ctx context.Context, req AuthRequest) (OperatorIdentity, error)
}

// Clock abstracts time.Now for deterministic tests.
type Clock interface {
    Now() time.Time
}
```

- [ ] **Step 2: Write the failing test for SessionStore**

Path: `internal/admin/auth/session_test.go`

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package adminauth_test

import (
    "errors"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/admin/auth"
    "github.com/holomush/holomush/internal/errutil"
)

// fakeClock is a deterministic Clock implementation for TTL tests.
type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

// TestSessionStoreEmptiedOnConstruction — INV-D3.
func TestSessionStoreEmptiedOnConstruction(t *testing.T) {
    fc := &fakeClock{t: time.Unix(1700000000, 0)}
    s := adminauth.NewSessionStore(fc, 10*time.Minute)
    _, err := s.Get("any-token-value")
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "DENY_SESSION_INVALID")
}

// TestSessionStoreIssueAndGetReturnsIdentity — happy path.
func TestSessionStoreIssueAndGetReturnsIdentity(t *testing.T) {
    fc := &fakeClock{t: time.Unix(1700000000, 0)}
    s := adminauth.NewSessionStore(fc, 10*time.Minute)
    id := adminauth.OperatorIdentity{PlayerID: "01HZA", AuthProviderName: "ingame-creds-totp", TOTPVerified: true}

    token, expires, err := s.Issue(id)
    require.NoError(t, err)
    require.NotEmpty(t, token)
    require.True(t, expires.After(fc.t))

    got, err := s.Get(token)
    require.NoError(t, err)
    assert.Equal(t, id, got)
}

// TestSessionStoreRejectsExpiredToken — INV-D2.
func TestSessionStoreRejectsExpiredToken(t *testing.T) {
    fc := &fakeClock{t: time.Unix(1700000000, 0)}
    s := adminauth.NewSessionStore(fc, 10*time.Minute)
    id := adminauth.OperatorIdentity{PlayerID: "01HZA"}

    token, _, err := s.Issue(id)
    require.NoError(t, err)

    fc.Advance(11 * time.Minute) // beyond 10-min TTL

    _, err = s.Get(token)
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "DENY_SESSION_EXPIRED")

    // Cleanup-on-Get: subsequent lookup is INVALID, not EXPIRED.
    _, err = s.Get(token)
    var oopsErr interface{ Code() string }
    require.True(t, errors.As(err, &oopsErr))
    assert.Equal(t, "DENY_SESSION_INVALID", oopsErr.Code())
}

// TestSessionStoreRevoke removes a token.
func TestSessionStoreRevoke(t *testing.T) {
    fc := &fakeClock{t: time.Unix(1700000000, 0)}
    s := adminauth.NewSessionStore(fc, 10*time.Minute)
    id := adminauth.OperatorIdentity{PlayerID: "01HZA"}
    token, _, _ := s.Issue(id)

    require.NoError(t, s.Revoke(token))
    _, err := s.Get(token)
    errutil.AssertErrorCode(t, err, "DENY_SESSION_INVALID")
}

// TestSessionStoreConcurrentIssueAndGet — race-detector clean.
func TestSessionStoreConcurrentIssueAndGet(t *testing.T) {
    fc := &fakeClock{t: time.Unix(1700000000, 0)}
    s := adminauth.NewSessionStore(fc, 10*time.Minute)
    var tokens []string
    for i := 0; i < 100; i++ {
        tk, _, err := s.Issue(adminauth.OperatorIdentity{PlayerID: "01HZA"})
        require.NoError(t, err)
        tokens = append(tokens, tk)
    }
    done := make(chan struct{})
    go func() {
        for _, tk := range tokens {
            _, _ = s.Get(tk)
        }
        close(done)
    }()
    for i := 0; i < 100; i++ {
        _, _, _ = s.Issue(adminauth.OperatorIdentity{PlayerID: "01HZB"})
    }
    <-done
}
```

- [ ] **Step 3: Run test (expect compile failure)**

Run: `task test -- ./internal/admin/auth/`

Expected: FAIL — `NewSessionStore`, `Issue`, `Get`, `Revoke` undefined.

- [ ] **Step 4: Implement `session.go`**

Path: `internal/admin/auth/session.go`

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package adminauth

import (
    "crypto/rand"
    "sync"
    "time"

    "github.com/oklog/ulid/v2"
    "github.com/samber/oops"
)

// SessionStore is the in-memory map of issued operator session tokens.
// Per spec §4 / INV-D2 / INV-D3:
//   - Tokens are ULIDs.
//   - TTL is per-construction (production: 10 min).
//   - The map is per-process; restart loses all sessions by design.
//   - Get is cleanup-on-access: expired tokens are deleted in-place.
type SessionStore interface {
    Issue(identity OperatorIdentity) (token string, expiresAt time.Time, err error)
    Get(token string) (OperatorIdentity, error)
    Revoke(token string) error
}

type sessionEntry struct {
    Identity  OperatorIdentity
    ExpiresAt time.Time
}

type memSessionStore struct {
    clock Clock
    ttl   time.Duration
    mu    sync.RWMutex
    m     map[string]sessionEntry
}

// NewSessionStore constructs an in-memory SessionStore with the given TTL.
func NewSessionStore(clock Clock, ttl time.Duration) SessionStore {
    return &memSessionStore{clock: clock, ttl: ttl, m: make(map[string]sessionEntry)}
}

func (s *memSessionStore) Issue(id OperatorIdentity) (string, time.Time, error) {
    now := s.clock.Now()
    entropy := ulid.Monotonic(rand.Reader, 0)
    tokenULID, err := ulid.New(ulid.Timestamp(now), entropy)
    if err != nil {
        return "", time.Time{}, oops.Code("SESSION_TOKEN_MINT_FAILED").Wrap(err)
    }
    token := tokenULID.String()
    expiresAt := now.Add(s.ttl)

    s.mu.Lock()
    s.m[token] = sessionEntry{Identity: id, ExpiresAt: expiresAt}
    s.mu.Unlock()
    return token, expiresAt, nil
}

func (s *memSessionStore) Get(token string) (OperatorIdentity, error) {
    s.mu.RLock()
    entry, ok := s.m[token]
    s.mu.RUnlock()
    if !ok {
        return OperatorIdentity{}, oops.Code("DENY_SESSION_INVALID").Errorf("session token not found")
    }
    if !s.clock.Now().Before(entry.ExpiresAt) {
        s.mu.Lock()
        delete(s.m, token)
        s.mu.Unlock()
        return OperatorIdentity{}, oops.Code("DENY_SESSION_EXPIRED").Errorf("session token expired")
    }
    return entry.Identity, nil
}

func (s *memSessionStore) Revoke(token string) error {
    s.mu.Lock()
    delete(s.m, token)
    s.mu.Unlock()
    return nil
}
```

- [ ] **Step 5: Run tests**

Run: `task test -- ./internal/admin/auth/`

Expected: PASS — 5 tests green; race detector clean.

- [ ] **Step 6: Commit**

```bash
jj --no-pager commit -m "feat(crypto): SessionStore for OperatorAuthProvider (holomush-jxo8.6 T5)

In-memory ULID-keyed session map per spec §4. 10-min TTL (production
default; tests use injected fakeClock). Cleanup-on-Get expires entries
in-place. INV-D2 (TTL enforced via DENY_SESSION_EXPIRED) and INV-D3
(per-process; lost on restart) covered by named tests."
```

---

## Task 6: `InGameCredentialsProvider` — 6-step check sequence

**Spec:** §4 6-step sequence. Invariants: INV-D1, INV-D4, INV-D15, INV-D19.

**Files:**

- Create: `internal/admin/auth/ingame.go`
- Create: `internal/admin/auth/ingame_test.go`

- [ ] **Step 1: Write the failing tests**

Path: `internal/admin/auth/ingame_test.go`

Tests (one per DENY path + happy path; uses mockery mocks for `auth.Service`, `totp.Service`, `access.SubjectResolver`, `store.RoleStore`):

- `TestInGameAuthenticateHappyPath` — all 6 steps succeed; returns OperatorIdentity.
- `TestInGameAuthenticateRejectsInvalidCredentials` — step 1 fails → `DENY_INVALID_CREDENTIALS`; subsequent mocks not called.
- `TestInGameAuthenticateRejectsNotEnrolled` — step 2 returns false → `DENY_NOT_ENROLLED`.
- `TestInGameAuthenticateRejectsBadTOTP` — step 3 returns Outcome != OK → `DENY_BAD_TOTP`.
- `TestInGameAuthenticateRejectsLocked` — step 3 Outcome=OutcomeLocked → `DENY_LOCKED`.
- `TestInGameAuthenticateRejectsNonOperator` — step 4 returns false → `DENY_NOT_OPERATOR`.
- `TestInGameAuthenticateRejectsPlayerWithoutAdminRole` — step 5 returns false → `DENY_NOT_ADMIN_ROLE`. (INV-D19)
- `TestInGameAuthenticateIgnoresPeerCredForGating` — step 6 PeerCred surfaces in OperatorIdentity.OSUser; same input twice with different PeerCred values returns identical outcomes. (INV-D4)
- `TestInGameAuthenticateStepOrderFixedOnFailure` — for each of the 5 failing-step branches, assert mocks for later steps received zero calls. (INV-D1)

(Each test follows the project's mockery pattern — `mocks.NewMockAuthService(t)`, `.EXPECT().ValidateCredentials(...).Return(...)`, etc. Mock interfaces will be regenerated in step 3.)

- [ ] **Step 2: Run test (expect compile failure)**

Run: `task test -- ./internal/admin/auth/`

Expected: FAIL — `NewInGameCredentialsProvider` undefined plus mock packages missing.

- [ ] **Step 3: Add mockery configuration for the wrapped interfaces**

In `mockery.yml` (project root, see existing pattern), add `internal/admin/auth/` package:

```yaml
packages:
  github.com/holomush/holomush/internal/admin/auth:
    interfaces:
      OperatorAuthProvider:
```

Also depend on existing mocks in `internal/auth/mocks` (PasswordHasher, PlayerRepository), `internal/totp/mocks` (Service), and add a new mock for `RoleStore` if not already present.

Run: `task generate` (or whatever the project mockery command is — see `Taskfile.yml`).

- [ ] **Step 4: Implement `ingame.go`**

Path: `internal/admin/auth/ingame.go`

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package adminauth

import (
    "context"

    "github.com/oklog/ulid/v2"
    "github.com/samber/oops"

    "github.com/holomush/holomush/internal/access"
    "github.com/holomush/holomush/internal/auth"
    "github.com/holomush/holomush/internal/store"
    "github.com/holomush/holomush/internal/totp"
)

// CredentialValidator is the narrow surface InGameCredentialsProvider
// requires from auth.Service. Decoupling for testability.
type CredentialValidator interface {
    ValidateCredentials(ctx context.Context, username, password string) (*auth.Player, error)
}

// EnrollmentChecker is the narrow surface for "is this player TOTP-enrolled?".
type EnrollmentChecker interface {
    IsEnrolled(ctx context.Context, playerID ulid.ULID) (bool, error)
    Verify(ctx context.Context, playerID ulid.ULID, code string) (totp.VerifyResult, error)
}

// InGameCredentialsProvider implements OperatorAuthProvider with the
// 6-step sequence per master spec §5.9 (amended) and design spec §4.
type InGameCredentialsProvider struct {
    creds      CredentialValidator
    totp       EnrollmentChecker
    resolver   access.SubjectResolver
    roleStore  store.RoleStore
}

// NewInGameCredentialsProvider constructs a provider with the named
// dependencies. None may be nil.
func NewInGameCredentialsProvider(
    creds CredentialValidator,
    totp EnrollmentChecker,
    resolver access.SubjectResolver,
    roleStore store.RoleStore,
) (*InGameCredentialsProvider, error) {
    if creds == nil {
        return nil, oops.Code("INGAME_NIL_CREDS").Errorf("CredentialValidator is required")
    }
    if totp == nil {
        return nil, oops.Code("INGAME_NIL_TOTP").Errorf("EnrollmentChecker is required")
    }
    if resolver == nil {
        return nil, oops.Code("INGAME_NIL_RESOLVER").Errorf("access.SubjectResolver is required")
    }
    if roleStore == nil {
        return nil, oops.Code("INGAME_NIL_ROLESTORE").Errorf("store.RoleStore is required")
    }
    return &InGameCredentialsProvider{creds: creds, totp: totp, resolver: resolver, roleStore: roleStore}, nil
}

// Name returns the provider name, persisted in audit metadata.
func (p *InGameCredentialsProvider) Name() string { return "ingame-creds-totp" }

// Authenticate runs the 6-step check sequence per design spec §4.
// Steps later than a failure MUST NOT execute (INV-D1).
func (p *InGameCredentialsProvider) Authenticate(ctx context.Context, req AuthRequest) (OperatorIdentity, error) {
    // Step 1: credentials.
    player, err := p.creds.ValidateCredentials(ctx, req.Username, req.Password)
    if err != nil {
        return OperatorIdentity{}, oops.Code("DENY_INVALID_CREDENTIALS").
            With("username", req.Username).Wrap(err)
    }

    // Step 2: TOTP enrolled?
    enrolled, err := p.totp.IsEnrolled(ctx, player.ID)
    if err != nil {
        return OperatorIdentity{}, oops.Code("INGAME_TOTP_LOOKUP_FAILED").
            With("player_id", player.ID.String()).Wrap(err)
    }
    if !enrolled {
        return OperatorIdentity{}, oops.Code("DENY_NOT_ENROLLED").
            With("player_id", player.ID.String()).
            Errorf("player has not enrolled TOTP")
    }

    // Step 3: TOTP verify.
    res, err := p.totp.Verify(ctx, player.ID, req.TOTPCode)
    if err != nil {
        return OperatorIdentity{}, oops.Code("INGAME_TOTP_VERIFY_FAILED").
            With("player_id", player.ID.String()).Wrap(err)
    }
    switch res.Outcome {
    case totp.OutcomeOK:
        // Continue.
    case totp.OutcomeLocked:
        return OperatorIdentity{}, oops.Code("DENY_LOCKED").
            With("player_id", player.ID.String()).
            With("locked_until", res.LockedUntil).
            Errorf("player TOTP is locked")
    default:
        return OperatorIdentity{}, oops.Code("DENY_BAD_TOTP").
            With("player_id", player.ID.String()).
            With("outcome", string(res.Outcome)).
            Errorf("TOTP verify failed")
    }

    // Step 4: capability allow-list.
    hasCap, err := access.HasPlayerGrant(ctx, p.resolver, player.ID.String(), access.CapabilityCryptoOperator)
    if err != nil {
        return OperatorIdentity{}, oops.Code("INGAME_GRANT_LOOKUP_FAILED").
            With("player_id", player.ID.String()).Wrap(err)
    }
    if !hasCap {
        return OperatorIdentity{}, oops.Code("DENY_NOT_OPERATOR").
            With("player_id", player.ID.String()).
            Errorf("player lacks crypto.operator capability")
    }

    // Step 5: RoleAdmin (any character).
    isAdmin, err := p.roleStore.PlayerHasRole(ctx, player.ID.String(), access.RoleAdmin)
    if err != nil {
        return OperatorIdentity{}, oops.Code("INGAME_ROLE_LOOKUP_FAILED").
            With("player_id", player.ID.String()).Wrap(err)
    }
    if !isAdmin {
        return OperatorIdentity{}, oops.Code("DENY_NOT_ADMIN_ROLE").
            With("player_id", player.ID.String()).
            Errorf("no character of player has admin role")
    }

    // Step 6: PeerCred capture (audit only).
    osUser := req.PeerCred.OSUser // may be empty if middleware did not capture
    return OperatorIdentity{
        PlayerID:         player.ID.String(),
        OSUser:           osUser,
        TOTPVerified:     true,
        AuthProviderName: p.Name(),
    }, nil
}
```

(Note: the `socket.PeerCred` type's `OSUser` field name is taken from sub-epic C; double-check via probe and adjust if the field is `Username` or similar.)

- [ ] **Step 5: Run tests**

Run: `task test -- ./internal/admin/auth/`

Expected: PASS — all 9 tests green; mock-call assertions confirm step ordering.

- [ ] **Step 6: Commit**

```bash
jj --no-pager commit -m "feat(crypto): InGameCredentialsProvider 6-step sequence (holomush-jxo8.6 T6)

Default OperatorAuthProvider per design spec §4. Six steps in fixed
order: ValidateCredentials → IsEnrolled → Verify → HasPlayerGrant →
PlayerHasRole(RoleAdmin) → PeerCred capture. Each failure short-circuits
with a typed DENY code. PeerCred is captured for audit only and never
gates (INV-D4). Master spec §5.9 amendments: ordering canonicalized,
role check reified as RoleStore.PlayerHasRole — preserves the
RoleAdmin AND crypto.operator conjunction.

Tests cover all 5 DENY paths + step-ordering invariant + happy path
(INV-D1, INV-D4, INV-D15, INV-D19)."
```

---

## Task 7: `approval.Repo` — types + Postgres impl

**Spec:** §5 dual-control. Invariants: INV-D5 (TTL filter), INV-D6 (self-approval rejection).

**Files:**

- Create: `internal/admin/approval/types.go`
- Create: `internal/admin/approval/repo.go`
- Create: `internal/admin/approval/repo_test.go` (unit, mocked pool — minimal)
- Create: `internal/admin/approval/repo_integration_test.go` (real PG)

- [ ] **Step 1: Write `types.go`**

Path: `internal/admin/approval/types.go`

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package approval

import "time"

// RequestID is the 16-byte ULID PK of an admin_approvals row.
type RequestID [16]byte

// String returns the ULID-formatted string.
func (r RequestID) String() string { /* ulid.ULID(r).String() — see ulid pkg */ ... }

// OpenRequest is the minimal input to create a pending approval row.
type OpenRequest struct {
    PrimaryPlayerID string
    OpKind          string
    OpArgsHash      []byte
}

// Approval is a snapshot of an admin_approvals row.
type Approval struct {
    RequestID            RequestID
    PrimaryPlayerID      string
    OpKind               string
    OpArgsHash           []byte
    ExpiresAt            time.Time
    ApprovedAt           *time.Time
    ApprovedByPlayerID   string
    CreatedAt            time.Time
}
```

- [ ] **Step 2: Write the failing integration test**

Path: `internal/admin/approval/repo_integration_test.go`

Tests (real PG):

- `TestRepoOpenAndGet` — Open returns request_id; Get returns the inserted row with non-zero CreatedAt.
- `TestRepoReadFiltersExpired` — insert a row with expires_at in the past; Get returns ErrNotFound. (INV-D5)
- `TestRepoMarkApproved` — Open, MarkApproved with different player_id; Get returns approved.
- `TestRepoMarkApprovedRejectsSelfApproval` — Open with primary X; MarkApproved with same player X → DENY_DUAL_CONTROL_SELF; Get still pending. (INV-D6)
- `TestRepoMarkApprovedRejectsExpiredRow` — Open + advance clock; MarkApproved → DENY_APPROVAL_EXPIRED.
- `TestRepoMarkApprovedRejectsAlreadyApproved` — Open, MarkApproved, MarkApproved again → DENY_APPROVAL_ALREADY_APPROVED.
- `TestRepoConcurrentMarkApproved` — fan-out 50 goroutines all calling MarkApproved on the same row; exactly one succeeds; 49 get DENY_APPROVAL_ALREADY_APPROVED.

- [ ] **Step 3: Implement `repo.go`**

Path: `internal/admin/approval/repo.go`

Includes:
- `Repo` interface: `Open`, `Get`, `MarkApproved`, `WaitForApproval`.
- `PostgresRepo` impl with `pgxpool.Pool` and a `Clock`.
- `Open`: generates fresh ULID via `ulid.Make`; inserts row with `expires_at = clock.Now() + 5*time.Minute`.
- `Get`: SELECT with `WHERE request_id = $1 AND expires_at >= now()`; returns oops.Code("APPROVAL_NOT_FOUND") on no rows.
- `MarkApproved`: atomic UPDATE in a single statement with WHERE predicates:
  ```sql
  UPDATE admin_approvals
     SET approved_at = now(), approved_by_player_id = $2
   WHERE request_id = $1
     AND approved_at IS NULL
     AND expires_at >= now()
     AND primary_player_id != $2
  RETURNING approved_at
  ```
  Differentiate the failure cases by re-querying the row to determine which predicate failed (self / already approved / expired / not found).
- `WaitForApproval`: poll-based loop calling Get every 500ms until `approved_at IS NOT NULL` or `deadline` reached.

- [ ] **Step 4: Run tests**

Run: `task test:int -- ./internal/admin/approval/`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj --no-pager commit -m "feat(crypto): admin_approvals Postgres repo (holomush-jxo8.6 T7)

Approval Repo per spec §5: Open/Get/MarkApproved/WaitForApproval. ULID
request_id, 5-min TTL, single-statement atomic MarkApproved with WHERE
predicates so concurrent approves can't race. Self-approval rejection
at the SQL layer (primary_player_id != \$2). Read-time filter on
expires_at >= now() per INV-D5. WaitForApproval polls Get every 500ms
until deadline.

Integration tests cover INV-D5/D6 + concurrent MarkApproved race."
```

---

## Task 8: Chain payload + JCS canonicalize + hash

**Spec:** §6 hash algorithm. Invariants: INV-D12, INV-D13.

**Files:**

- Create: `internal/admin/policy/chain.go`
- Create: `internal/admin/policy/chain_test.go`
- Create: `internal/admin/policy/jcs_meta_test.go` (INV-D13 lock)

- [ ] **Step 1: Write `chain.go` types**

```go
package policy

import "time"

// PolicySetPayload is the body of a crypto.policy_set audit event.
// Stored as JSON inside Event.Payload (the inner field of the marshaled
// eventbusv1.Event envelope written to events_audit.envelope).
type PolicySetPayload struct {
    PolicyName        string         `json:"policy_name"`
    PolicySnapshot    map[string]any `json:"policy_snapshot"`
    PolicyHash        []byte         `json:"policy_hash"`        // base64; computed; excluded from canon-input
    PrevHash          []byte         `json:"prev_hash"`          // null at genesis
    ServerStartULID   string         `json:"server_start_ulid"`
    ServerIdentity    string         `json:"server_identity"`
    Timestamp         time.Time      `json:"timestamp"`
}

// ComputePolicyHash returns SHA-256 over JCS-canonicalized JSON of payload
// with the policy_hash field excluded.
func ComputePolicyHash(payload *PolicySetPayload) ([]byte, error) { ... }
```

- [ ] **Step 2: Write the failing tests**

Path: `internal/admin/policy/chain_test.go`

- `TestComputePolicyHashGoldenValue` — canned input → fixed expected SHA-256 hex string. (INV-D12)
- `TestComputePolicyHashExcludesPolicyHashField` — same payload with different `PolicyHash` values produces same canonicalized bytes (i.e., the hash field doesn't bleed into its own input).
- `TestComputePolicyHashStableUnderJSONFieldReorder` — same struct values via two construction paths produce the same hash (JCS sorts).

- [ ] **Step 3: Implement `ComputePolicyHash`**

```go
import (
    "crypto/sha256"
    "encoding/json"

    jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
    "github.com/samber/oops"
)

func ComputePolicyHash(payload *PolicySetPayload) ([]byte, error) {
    canon := *payload
    canon.PolicyHash = nil
    raw, err := json.Marshal(&canon)
    if err != nil {
        return nil, oops.Code("POLICY_HASH_JSON_MARSHAL_FAILED").Wrap(err)
    }
    canonical, err := jsoncanonicalizer.Transform(raw)
    if err != nil {
        return nil, oops.Code("POLICY_HASH_JCS_FAILED").Wrap(err)
    }
    sum := sha256.Sum256(canonical)
    return sum[:], nil
}
```

- [ ] **Step 4: Run tests**

Expected: PASS.

- [ ] **Step 5: Write the JCS meta-test (INV-D13)**

Path: `internal/admin/policy/jcs_meta_test.go`

```go
package policy

import (
    "os"
    "strings"
    "testing"
)

// TestJCSCanonicalizationLockedToVendoredImpl asserts the JCS canonicalizer
// pin in go.mod. INV-D13: switching libraries / pseudo-versions is a
// chain-breaking change requiring a master-spec amendment.
func TestJCSCanonicalizationLockedToVendoredImpl(t *testing.T) {
    data, err := os.ReadFile("../../../go.mod")
    if err != nil { t.Fatalf("read go.mod: %v", err) }
    src := string(data)
    if !strings.Contains(src, "github.com/cyberphone/json-canonicalization v0.0.0-20241213102144-19d51d7fe467") {
        t.Fatalf("go.mod must pin cyberphone/json-canonicalization at v0.0.0-20241213102144-19d51d7fe467 (INV-D13)")
    }
}
```

- [ ] **Step 6: Run tests**

Run: `task test -- ./internal/admin/policy/`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
jj --no-pager commit -m "feat(crypto): policy_set payload + JCS hash (holomush-jxo8.6 T8)

PolicySetPayload struct + ComputePolicyHash per spec §6. Hash =
SHA-256 over JCS-canonicalized JSON of payload (RFC 8785) with the
policy_hash field zeroed out. Golden-vector tests + meta-test locking
the cyberphone/json-canonicalization pseudo-version pin (INV-D12,
INV-D13)."
```

---

## Task 9: Chain verifier — proto + JSON two-step decode

**Spec:** §6 verifier. Invariants: INV-D10, INV-D11.

**Files:**

- Create: `internal/admin/policy/verifier.go`
- Create: `internal/admin/policy/verifier_test.go` (unit — fake `events_audit` rows)
- Create: `internal/admin/policy/verifier_integration_test.go` (real PG)

- [ ] **Step 1: Write the unit tests**

Tests in `verifier_test.go`:

- `TestVerifyChainAcceptsEmptyChain` — no rows → no error (genesis written by emitter).
- `TestVerifyChainAcceptsValidGenesis` — single row with `prev_hash = nil` → no error.
- `TestVerifyChainAcceptsValidExtension` — two rows; second's `prev_hash = computeHash(first)`; no error.
- `TestVerifyChainRejectsBrokenGenesis` — single row with non-null `prev_hash` → POLICY_CHAIN_BROKEN_GENESIS. (INV-D11)
- `TestVerifyChainRejectsBrokenLink` — three rows; corrupt second's `prev_hash` → POLICY_CHAIN_BROKEN_LINK.
- `TestVerifyChainRejectsHashMismatch` — payload bytes corrupted (recomputed hash != stored) → POLICY_CHAIN_HASH_MISMATCH.
- `TestVerifyChainDecodesEnvelopeAndJSON` — rows with `proto.Marshal(eventbusv1.Event)` envelopes containing JSON `PolicySetPayload`. Verifier decodes correctly.

(Mock the loader to return canned `chainEntry` slices; the SQL query lives in T9-T-integration.)

- [ ] **Step 2: Implement `verifier.go`**

```go
package policy

import (
    "bytes"
    "context"
    "encoding/json"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/samber/oops"
    "google.golang.org/protobuf/proto"

    eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// chainEntry is one decoded row from events_audit for the chain subject.
type chainEntry struct {
    Seq     int64
    Payload PolicySetPayload
}

// VerifyChain validates the integrity of the policy_set chain for one
// policy_name. Per INV-D10/D11/D12. Reads events_audit ordered by
// js_seq, decodes envelope → JSON payload, walks the chain, and recomputes
// each event's policy_hash to catch payload tampering.
//
// Returns nil if the chain is empty (fresh DB; CryptoPolicySubsystem will
// emit the genesis row). Returns a typed POLICY_CHAIN_* error on any
// integrity failure.
func VerifyChain(ctx context.Context, pool *pgxpool.Pool, subject, policyName string) error {
    entries, err := loadChainEntries(ctx, pool, subject)
    if err != nil {
        return oops.Code("POLICY_CHAIN_LOAD_FAILED").
            With("subject", subject).Wrap(err)
    }
    if len(entries) == 0 {
        return nil
    }
    if entries[0].Payload.PrevHash != nil {
        return oops.Code("POLICY_CHAIN_BROKEN_GENESIS").
            With("subject", subject).
            With("js_seq", entries[0].Seq).
            Errorf("first event has non-null prev_hash")
    }
    for i := 1; i < len(entries); i++ {
        prevHash, err := ComputePolicyHash(&entries[i-1].Payload)
        if err != nil {
            return oops.Code("POLICY_CHAIN_HASH_RECOMPUTE_FAILED").
                With("policy_name", policyName).Wrap(err)
        }
        if !bytes.Equal(entries[i].Payload.PrevHash, prevHash) {
            return oops.Code("POLICY_CHAIN_BROKEN_LINK").
                With("policy_name", policyName).
                With("js_seq", entries[i].Seq).
                Errorf("prev_hash does not match predecessor's policy_hash")
        }
        recomputed, err := ComputePolicyHash(&entries[i].Payload)
        if err != nil {
            return oops.Code("POLICY_CHAIN_HASH_RECOMPUTE_FAILED").Wrap(err)
        }
        if !bytes.Equal(entries[i].Payload.PolicyHash, recomputed) {
            return oops.Code("POLICY_CHAIN_HASH_MISMATCH").
                With("policy_name", policyName).
                With("js_seq", entries[i].Seq).
                Errorf("policy_hash does not match canonicalized payload")
        }
    }
    return nil
}

// loadChainEntries reads events_audit rows for the given subject ordered
// by js_seq and decodes each envelope.
func loadChainEntries(ctx context.Context, pool *pgxpool.Pool, subject string) ([]chainEntry, error) {
    rows, err := pool.Query(ctx, `
        SELECT envelope, js_seq
          FROM events_audit
         WHERE subject = $1
         ORDER BY js_seq ASC
    `, subject)
    if err != nil { return nil, err }
    defer rows.Close()

    var out []chainEntry
    for rows.Next() {
        var envelopeBytes []byte
        var seq int64
        if err := rows.Scan(&envelopeBytes, &seq); err != nil {
            return nil, err
        }
        var ev eventbusv1.Event
        if err := proto.Unmarshal(envelopeBytes, &ev); err != nil {
            return nil, oops.Code("POLICY_CHAIN_ENVELOPE_DECODE_FAILED").
                With("js_seq", seq).Wrap(err)
        }
        var payload PolicySetPayload
        if err := json.Unmarshal(ev.Payload, &payload); err != nil {
            return nil, oops.Code("POLICY_CHAIN_PAYLOAD_DECODE_FAILED").
                With("js_seq", seq).Wrap(err)
        }
        out = append(out, chainEntry{Seq: seq, Payload: payload})
    }
    return out, rows.Err()
}
```

- [ ] **Step 3: Run unit tests**

Run: `task test -- ./internal/admin/policy/`

Expected: PASS.

- [ ] **Step 4: Write the integration test**

Path: `internal/admin/policy/verifier_integration_test.go`

`TestVerifierAgainstRealEventsAudit`:
- Direct-insert 3 valid chain rows into `events_audit` (with marshaled envelopes containing JSON payloads).
- VerifyChain → no error.
- Corrupt second row's payload bytes (UPDATE the envelope column).
- VerifyChain → POLICY_CHAIN_HASH_MISMATCH or POLICY_CHAIN_BROKEN_LINK.

- [ ] **Step 5: Run integration tests**

Run: `task test:int -- ./internal/admin/policy/`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
jj --no-pager commit -m "feat(crypto): policy_set chain verifier (holomush-jxo8.6 T9)

VerifyChain reads events_audit rows for the chain subject ORDER BY
js_seq, two-step decodes envelope (proto) → payload (JSON), walks the
chain forward checking prev_hash + recomputed policy_hash. INV-D10/D11/D12
covered. Genesis-empty path returns nil; emitter writes the genesis row
in T10. Chain subject MUST be IdentityCodec-bound (spec §6 codec
constraint)."
```

---

## Task 10: Chain emitter — write current snapshot

**Spec:** §6 emitter. Invariants: INV-D10 (emit-side genesis), INV-D17 (publish failure → fail-closed).

**Files:**

- Create: `internal/admin/policy/emitter.go`
- Create: `internal/admin/policy/emitter_test.go`

- [ ] **Step 1: Write `emitter.go`**

Includes:

```go
type CryptoEffectiveConfig struct {
    DualControlRequired []string  // sorted, deduped
}

type EmitDeps struct {
    GameID          string
    ServerStartULID string
    ServerIdentity  string
    Pool            *pgxpool.Pool
    Publisher       eventbus.Publisher
    Clock           Clock
    Config          CryptoEffectiveConfig
}

// EmitCurrentSnapshot reads the latest events_audit row for the chain
// subject, computes the new event's prev_hash from it (or null if empty),
// builds the PolicySetPayload, computes its policy_hash, builds the
// envelope, and Publishes it. On Publish error, returns the wrapped error
// so Subsystem.Start fails (INV-D17). Idempotent on no-change.
func EmitCurrentSnapshot(ctx context.Context, deps EmitDeps, policyName string) error { ... }
```

The actual envelope construction follows the existing publisher pattern in `internal/eventbus/publisher.go`: build `eventbusv1.Event` with `Subject`, `Type`, `Timestamp`, `Payload` (the JSON of `PolicySetPayload`). Publisher takes care of marshaling and JS publish.

- [ ] **Step 2: Write the failing tests**

Tests in `emitter_test.go`:

- `TestEmitCurrentSnapshotGenesis` — empty `events_audit`; emitter publishes one event with `prev_hash = nil`. Mock Publisher; assert subject + JSON-decoded payload.
- `TestEmitCurrentSnapshotExtension` — pre-seed one row; emitter reads it, computes hash, publishes new event with `prev_hash = computeHash(seeded)`. Mock Publisher.
- `TestEmitCurrentSnapshotIdempotentOnNoChange` — pre-seed one event with the same effective config as `Config`; emitter recognizes equality (post-JCS canon), no publish call.
- `TestEmitCurrentSnapshotFailsOnPublishError` — mock Publisher returns error; emitter returns wrapped error; no PG side effects (audit projection won't see anything). (INV-D17)

- [ ] **Step 3: Implement and iterate**

Mock the `eventbus.Publisher` interface (it already exists in `internal/eventbus`); use mockery if not already mocked. Mock the chain reader (`loadChainEntries` from T9 — extract to a small interface or inject the function for testability).

- [ ] **Step 4: Run tests**

Run: `task test -- ./internal/admin/policy/`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj --no-pager commit -m "feat(crypto): policy_set chain emitter (holomush-jxo8.6 T10)

EmitCurrentSnapshot per spec §6: reads latest events_audit row for chain
subject, computes its hash as new event's prev_hash (or null at genesis),
builds and publishes the new event. Idempotent on no-change (post-JCS
byte-equal). Publish error → wrapped error from EmitCurrentSnapshot —
caller (Subsystem.Start) fails-closed per INV-D17."
```

---

## Task 11: `CryptoPolicySubsystem` lifecycle wrapper

**Spec:** §6 emitter subsystem. Invariants: INV-D17.

**Files:**

- Create: `internal/admin/policy/subsystem.go`
- Create: `internal/admin/policy/subsystem_test.go`

- [ ] **Step 1: Implement `subsystem.go`**

```go
package policy

type CryptoPolicySubsystemConfig struct {
    EmitDeps  EmitDeps
    PolicyNames []string  // v1: ["dual_control_required"]
}

type CryptoPolicySubsystem struct { cfg CryptoPolicySubsystemConfig }

func NewCryptoPolicySubsystem(cfg CryptoPolicySubsystemConfig) *CryptoPolicySubsystem {
    return &CryptoPolicySubsystem{cfg: cfg}
}

func (s *CryptoPolicySubsystem) ID() lifecycle.SubsystemID {
    return lifecycle.SubsystemCryptoPolicy
}

func (s *CryptoPolicySubsystem) DependsOn() []lifecycle.SubsystemID {
    return []lifecycle.SubsystemID{lifecycle.SubsystemAuditProjection}
}

func (s *CryptoPolicySubsystem) Start(ctx context.Context) error {
    for _, name := range s.cfg.PolicyNames {
        if err := EmitCurrentSnapshot(ctx, s.cfg.EmitDeps, name); err != nil {
            return oops.Code("CRYPTO_POLICY_EMIT_FAILED").
                With("policy_name", name).Wrap(err)
        }
    }
    return nil
}

func (s *CryptoPolicySubsystem) Stop(ctx context.Context) error { return nil }
```

- [ ] **Step 2: Write tests**

Tests:

- `TestCryptoPolicySubsystemIDReturnsCryptoPolicy`
- `TestCryptoPolicySubsystemDependsOnAuditProjection`
- `TestCryptoPolicySubsystemStartEmitsAllPolicyNames` — mocked deps; assert one Publish per name in order.
- `TestCryptoPolicySubsystemFailsStartOnPublishError` — Publish returns error; Start returns wrapped error. (INV-D17)
- `TestCryptoPolicySubsystemStopIsNoOp`

- [ ] **Step 3: Run tests**

Run: `task test -- ./internal/admin/policy/`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
jj --no-pager commit -m "feat(crypto): CryptoPolicySubsystem lifecycle (holomush-jxo8.6 T11)

Lifecycle subsystem that emits the current effective snapshot for each
known policy_name on Start. DependsOn AuditProjection. Start fails-closed
on Publish error (INV-D17) — no silent chain breaks. Wired into
productionSubsystems in T22."
```

---

## Task 12: Bootstrap-subsystem chain verifier wiring

**Spec:** §6 verifier (Bootstrap subsystem). Invariants: INV-D11.

**Files:**

- Modify: `internal/bootstrap/setup/subsystem.go::Start` — add VerifyChain calls.
- Test: `internal/bootstrap/setup/subsystem_test.go` — new test asserting VerifyChain failures fail-close startup.

- [ ] **Step 1: Add chain-verifier call into Bootstrap Start**

Inside `BootstrapSubsystem.Start`, after the existing INV-32/33/37 + orphan checks but before returning success, iterate over known policy names and call `policy.VerifyChain(ctx, pool, subject, name)`. Subject = `events.<gameID>.system.crypto_policy.<name>`.

- [ ] **Step 2: Write the test**

`TestBootstrapRefusesStartOnPolicyChainVerifyFailure`:
- Seed events_audit with a deliberately-broken chain (e.g., second row's `prev_hash` zeroed).
- Run BootstrapSubsystem.Start.
- Assert the returned error wraps POLICY_CHAIN_BROKEN_LINK (or whichever code applies).

- [ ] **Step 3: Run integration test (Bootstrap touches PG)**

Run: `task test:int -- ./internal/bootstrap/...`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
jj --no-pager commit -m "feat(crypto): wire policy_set chain verifier into Bootstrap (holomush-jxo8.6 T12)

BootstrapSubsystem.Start now calls policy.VerifyChain for each known
policy_name after INV-32/33/37 + orphan check. Fails-closed on any
chain integrity violation (INV-D11). Verifier reads events_audit pre-
EventBus; race story for graceful/crash regimes is covered in spec §6
projection-async-write subsection."
```

---

## Task 13: `AuditingService` decorator

**Spec:** §7 decorator. Invariants: INV-D14.

**Files:**

- Create: `internal/admin/totp_audit/auditing.go`
- Create: `internal/admin/totp_audit/auditing_test.go`

- [ ] **Step 1: Write the failing tests**

Tests (mock `totp.Service` + `eventbus.Publisher`):

- `TestAuditingServiceVerifyEmitsLockedOnTransition` — Verify returns `LockoutTransition: true`; Publisher receives one Publish for `crypto.totp_locked` subject; Verify result is propagated.
- `TestAuditingServiceVerifyDoesNotEmitWithoutTransition` — Verify returns `LockoutTransition: false`; Publisher receives zero calls.
- `TestAuditingServiceConsumeRecoveryCodeEmitsRecoveryConsumed`
- `TestAuditingServiceClearTOTPEmitsCleared`
- `TestAuditingServiceRecoverAndClearEmitsBoth` — RecoverAndClear success → exactly two Publish calls in order: recovery_consumed then cleared (with `cleared_by=recovery_code`).
- `TestAuditingServiceLogsAndContinuesOnPublishError` — Publisher returns error; AuditingService returns inner result + nil error from the inner method (operation succeeded; emit failure logged). Use a `slog.Logger` with a captured `slog.Handler` to assert the warning. (INV-D14)
- `TestAuditingServiceWrapsAllStateTransitionMethods` — table test asserting every `totp.Service` method has an emission helper or pass-through.

- [ ] **Step 2: Implement `auditing.go`** — uses subject builders + payload structs + EventType constants from `internal/totp/audit.go`.

- [ ] **Step 3: Run tests**

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
jj --no-pager commit -m "feat(crypto): AuditingService decorator for totp.Service (holomush-jxo8.6 T13)

Decorator wraps totp.Service to emit crypto.totp_* lifecycle events on
state transitions. Subjects + payloads from internal/totp/audit.go (sub-
epic A's reserved namespace). Publish failure logs via slog.Warn and
continues — INV-D14. RecoverAndClear emits both recovery_consumed and
cleared (in order); §7 partial-emit window covered by the spec."
```

---

## Task 14: `admin.proto` extensions — Authenticate, Approve, ResetTOTP RPCs

**Spec:** §3 wire surface.

**Files:**

- Modify: `api/proto/holomush/admin/v1/admin.proto` — add three new RPCs and their request/response messages.
- Generated: `pkg/proto/holomush/admin/v1/*` (regenerated by `task proto:generate` or equivalent).

- [ ] **Step 1: Add the new RPCs and messages**

In `admin.proto`, add:

```proto
service AdminService {
  rpc Status(StatusRequest) returns (StatusResponse);
  rpc Authenticate(AuthenticateRequest) returns (AuthenticateResponse);
  rpc Approve(ApproveRequest) returns (ApproveResponse);
  rpc ResetTOTP(ResetTOTPRequest) returns (ResetTOTPResponse);
}

message AuthenticateRequest {
  string username = 1;
  string password = 2;
  string totp_code = 3;
}
message AuthenticateResponse {
  string session_token = 1;
  google.protobuf.Timestamp expires_at = 2;
  string player_id = 3;
}

message ApproveRequest {
  string session_token = 1;
  bytes request_id = 2;  // 16-byte ULID
}
message ApproveResponse {}

message ResetTOTPRequest {
  string session_token = 1;
  string target_player_id = 2;
}
message ResetTOTPResponse {
  bool cleared = 1;
}
```

- [ ] **Step 2: Regenerate proto bindings**

Run: `task proto:generate` (or whatever the project task is).

Expected: regenerated files in `pkg/proto/holomush/admin/v1/` and `pkg/proto/holomush/admin/v1/adminv1connect/`.

- [ ] **Step 3: Run tests + build**

Run: `task lint && task test && task build`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
jj --no-pager commit -m "feat(crypto): admin.proto Authenticate+Approve+ResetTOTP RPCs (holomush-jxo8.6 T14)

Three new unary RPCs on AdminService for sub-epic D's UDS surface:
Authenticate (creds+TOTP→session_token), Approve (second-op signoff
on a pending admin_approvals row), ResetTOTP (admin reset of a target
player's enrollment). Both grpc-go and ConnectRPC bindings regenerate
per repo convention."
```

---

## Task 15: `Authenticate` RPC handler

**Spec:** §3 wire surface, §4 sequence.

**Files:**

- Create: `internal/admin/auth/handler.go`
- Create: `internal/admin/auth/handler_test.go`

- [ ] **Step 1: Write the failing test**

Test the handler: PeerCred from ctx, calls OperatorAuthProvider.Authenticate, on success Issue session token and return. On DENY error, return ConnectRPC code with the typed oops code.

- [ ] **Step 2: Implement the handler**

Includes peer-cred extraction from `socket.PeerCredFromContext(ctx)`, error code translation (`oops.Code("DENY_*")` → `connect.CodePermissionDenied` etc), and structured response building.

- [ ] **Step 3: Run tests**

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
jj --no-pager commit -m "feat(crypto): Authenticate RPC handler (holomush-jxo8.6 T15)

Handler runs OperatorAuthProvider.Authenticate, captures PeerCred from
ctx via sub-epic C's middleware, mints a session token via SessionStore
on success, and returns AuthenticateResponse{session_token, expires_at,
player_id}. DENY errors map to connect codes with the typed oops Code()
preserved in the error metadata."
```

---

## Task 16: `Approve` RPC handler

**Spec:** §5 dual-control. Invariants: INV-D6, INV-D7, INV-D16.

**Files:**

- Create: `internal/admin/approval/handler.go`
- Create: `internal/admin/approval/handler_test.go`

- [ ] **Step 1: Write the failing tests**

- `TestApproveHandlerRequiresValidSession` → DENY_SESSION_INVALID.
- `TestApproveHandlerRequiresExpiredSessionRejected` → DENY_SESSION_EXPIRED.
- `TestApproveHandlerRequiresCapabilityOnHandler` → revoke capability mid-session; DENY_NOT_OPERATOR. (INV-D16)
- `TestApproveHandlerRequiresAdminRoleOnHandler` → revoke role mid-session; DENY_NOT_ADMIN_ROLE. (INV-D16)
- `TestApproveHandlerCallsRepoMarkApproved` → happy path; calls Repo.MarkApproved with session.PlayerID.
- `TestApproveHandlerSurfacesSelfApprovalDenial` → MarkApproved returns DENY_DUAL_CONTROL_SELF; handler propagates.
- `TestApproveHandlerSurfacesAlreadyApprovedDenial` and `Expired` similarly.

- [ ] **Step 2: Implement the handler**

The handler resolves the session, re-asserts capability + role (re-running steps 4 + 5 from the auth provider), then calls `approval.Repo.MarkApproved`. Re-uses the same `HasPlayerGrant` and `RoleStore.PlayerHasRole` helpers.

- [ ] **Step 3: Run tests**

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
jj --no-pager commit -m "feat(crypto): Approve RPC handler (holomush-jxo8.6 T16)

Handler resolves session_token via SessionStore, re-asserts capability
+ role (defense in depth, INV-D16), then calls Repo.MarkApproved.
Repo's atomic UPDATE rejects self-approval, expired rows, and already-
approved rows with typed errors that the handler propagates."
```

---

## Task 17: `ResetTOTP` RPC handler + admin reset flow

**Spec:** §3 wire surface. Invariants: INV-D16.

**Files:**

- Create: `internal/admin/auth/reset_handler.go` (or co-locate in `handler.go`)
- Create: `internal/admin/auth/reset_handler_test.go`

- [ ] **Step 1: Write the failing tests**

- `TestResetTOTPHandlerRequiresValidSession`.
- `TestResetTOTPRequiresCapabilityOnHandler`. (INV-D16)
- `TestResetTOTPRequiresAdminRoleOnHandler`. (INV-D16)
- `TestResetTOTPHandlerCallsClearTOTPThroughDecorator` — via `AuditingService.ClearTOTP(targetPlayerID, ClearReasonAdminReset)`; assert decorator emits `crypto.totp_cleared` event.

- [ ] **Step 2: Implement the handler**

Resolves session, re-asserts capability + role, calls `auditingTotp.ClearTOTP(targetPID, totp.ClearReasonAdminReset)`. Decorator handles emission.

- [ ] **Step 3: Run tests**

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
jj --no-pager commit -m "feat(crypto): ResetTOTP RPC handler (holomush-jxo8.6 T17)

Admin-reset path for a target player's TOTP enrollment. Handler resolves
session, re-asserts capability + role, calls AuditingService.ClearTOTP
(ClearReasonAdminReset). Decorator emits crypto.totp_cleared with
cleared_by='admin_reset' on success."
```

---

## Task 18: Wire handlers into `buildMux`

**Spec:** §3 — registration on sub-epic C's mux.

**Files:**

- Modify: `internal/admin/socket/server.go::buildMux` — register the three new handlers.
- Modify: `internal/admin/socket/server_test.go` — extend the existing `TestServerServesStatusRPCOverUDS` pattern to assert the new endpoints exist.

- [ ] **Step 1: Pass the handlers in via `Config`**

Extend `Config` (the struct used by `NewServer`) with fields for the three handlers (interface types for testability):
- `AuthenticateHandler adminauth.AuthenticateHandler`
- `ApproveHandler approval.ApproveHandler`
- `ResetTOTPHandler adminauth.ResetTOTPHandler`

(`buildMux` then registers each via the generated `adminv1connect.NewAdmin*Handler` builder.)

- [ ] **Step 2: Update `NewServer` callers**

Find every caller (cmd/holomush/sub_admin_socket.go or similar — use probe). Update each to pass the new handlers (they may need stub implementations until subsystem wiring lands in T22).

- [ ] **Step 3: Run tests**

Run: `task test -- ./internal/admin/socket/`

Expected: PASS — extended status test, plus build green.

- [ ] **Step 4: Commit**

```bash
jj --no-pager commit -m "feat(crypto): register Authenticate/Approve/ResetTOTP handlers on UDS mux (holomush-jxo8.6 T18)

Sub-epic C's buildMux now also wires up sub-epic D's three new handlers.
NewServer Config gains three handler-interface fields; existing callers
updated to pass them."
```

---

## Task 19: `CryptoConfig.DualControlRequired` + lax+warn validation

**Spec:** §9 validation.

**Files:**

- Modify: `internal/config/config.go` — add `DualControlRequired []string` field to `CryptoConfig`.
- Create: `cmd/holomush/crypto_dual_control_validation.go`
- Create: `cmd/holomush/crypto_dual_control_validation_test.go`

- [ ] **Step 1: Add the config field**

In `internal/config/config.go::CryptoConfig`:

```go
type CryptoConfig struct {
    // ... existing ...
    DualControlRequired []string `yaml:"dual_control_required"`
}
```

- [ ] **Step 2: Implement the validator** (mirrors `crypto_operator_validation.go`)

```go
package main

func validateDualControlRequired(ops []string, logger *slog.Logger) []string {
    known := map[string]struct{}{"rekey": {}, "admin_read_stream": {}}
    valid := make([]string, 0, len(ops))
    for _, op := range ops {
        if _, ok := known[op]; !ok {
            logger.Warn("crypto.dual_control_required references unknown op_kind; ignoring",
                "op_kind", op,
                "known_ops", []string{"rekey", "admin_read_stream"})
            continue
        }
        valid = append(valid, op)
    }
    return valid
}
```

- [ ] **Step 3: Tests**

- `TestValidateDualControlRequired_FiltersUnknownOps`
- `TestValidateDualControlRequired_PreservesKnownOps`
- `TestValidateDualControlRequired_AcceptsEmpty`

- [ ] **Step 4: Run tests**

Run: `task test -- ./cmd/holomush/...`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj --no-pager commit -m "feat(crypto): crypto.dual_control_required config + lax+warn validation (holomush-jxo8.6 T19)

CryptoConfig.DualControlRequired []string per spec §5 layer-2 + §9
validation pattern. Mirrors sub-epic B's crypto.operators lax+warn:
unknown op_kind → slog.Warn and exclude from enforcement; known ops
preserved. Server starts on misconfig with operator-visible warning."
```

---

## Task 20: `holomush admin totp reset` CLI

**Spec:** §11 deferred from sub-epic A.

**Files:**

- Create: `cmd/holomush/cmd_admin_totp_reset.go`
- Create: `cmd/holomush/cmd_admin_totp_reset_test.go`

- [ ] **Step 1: Add the cobra subcommand**

```bash
holomush admin totp reset <player_id>
```

Flow: prompt operator for username, password, TOTP code (use the `prompt`-package pattern from sub-epic A's admin CLIs); open ConnectRPC over the UDS socket path (config-driven); call `Authenticate` → session token; call `ResetTOTP{session_token, target_player_id: <player_id>}`; print result.

- [ ] **Step 2: Tests** — table-driven CLI tests with a fake server (httptest UDS) responding with canned ResetTOTPResponse / DENY error.

- [ ] **Step 3: Run tests**

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
jj --no-pager commit -m "feat(crypto): holomush admin totp reset CLI (holomush-jxo8.6 T20)

Admin-reset CLI deferred from sub-epic A. Prompts for operator credentials,
opens ConnectRPC over the UDS socket, calls Authenticate then ResetTOTP."
```

---

## Task 21: `holomush admin approve` CLI

**Spec:** §5 dual-control second-op.

**Files:**

- Create: `cmd/holomush/cmd_admin_approve.go`
- Create: `cmd/holomush/cmd_admin_approve_test.go`

- [ ] **Step 1: Add the cobra subcommand**

```bash
holomush admin approve <request_id>
```

Flow identical to T20 but ends in `Approve{session_token, request_id}`.

- [ ] **Step 2: Tests** — same shape as T20.

- [ ] **Step 3: Run tests**

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
jj --no-pager commit -m "feat(crypto): holomush admin approve CLI (holomush-jxo8.6 T21)

Second-op signoff CLI per spec §5. Prompts for the second operator's
credentials, authenticates, calls Approve(session_token, request_id)."
```

---

## Task 22: Wire `CryptoPolicySubsystem` into `productionSubsystems`

**Spec:** §6 startup ordering.

**Files:**

- Modify: `cmd/holomush/core.go::productionSubsystems` — add the new subsystem.
- Modify: `cmd/holomush/deps.go` — add the constructor parameters.
- Modify: `cmd/holomush/core_subsystems_test.go` — assert the new subsystem present.

- [ ] **Step 1: Build a `CryptoPolicySubsystem` instance in `runCoreWithDeps`**

Wire in the EmitDeps from existing dependencies (DB pool, eventbus.Publisher, GameID, server-start ULID, server identity, current effective config).

- [ ] **Step 2: Append to the slice**

Modify `productionSubsystems()` to accept and emit the new subsystem in the right order:

```go
return []lifecycle.Subsystem{
    dbSub, abacSub, authSub, worldSub,
    sessionSub, pluginSub, bootstrapSub,
    eventBusSub, clusterSub, auditSub,
    cryptoPolicySub,  // <-- new; after audit projection, before grpc
    grpcSub, adminSub,
}
```

- [ ] **Step 3: Update the test**

Extend `TestProductionSubsystemsIncludesAdminSocket`-like to assert `SubsystemCryptoPolicy` is present and that the count is 13 (was 12 after C).

- [ ] **Step 4: Run tests**

Run: `task test -- ./cmd/holomush/...`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj --no-pager commit -m "feat(crypto): wire CryptoPolicySubsystem into productionSubsystems (holomush-jxo8.6 T22)

Subsystem slice grows from 12 to 13. Order: ...AuditProjection,
CryptoPolicySubsystem, gRPC, AdminSocket. CryptoPolicySubsystem.Start
runs after AuditProjection drains its initial backlog (per
DependsOn) and emits the genesis or extension policy_set event for
each known policy_name."
```

---

## Task 23: Master-spec amendments — text edits + meta-test

**Spec:** §10 amendments table.

**Files:**

- Modify: `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` — apply the 7 amendment rows.
- Modify: `internal/access/spec_amendments_test.go` — extend `TestSpecAmendmentsLanded` (or add `TestSpecAmendmentsLandedSubEpicD`) with substring asserts for D's amendments.

- [ ] **Step 1: Apply the master-spec edits**

For each row in §10 amendments table of the design spec, locate the relevant master-spec section and apply the edit. Specific edits:

- §5.9 interface block: replace `Authenticate(ctx, prompt PromptFunc)` with `Authenticate(ctx, AuthRequest) (OperatorIdentity, error)`. Drop `RequireDualControl` method.
- §5.9 default-impl steps 1-6: rewrite as the canonical 6-step sequence (creds → IsEnrolled → Verify → HasPlayerGrant → PlayerHasRole(RoleAdmin) → PeerCred capture). Reify step 5 as `RoleStore.PlayerHasRole`.
- §6.3.1 `op_args_hash`: pin to `SHA-256(proto.MarshalDeterministic(args))` + cite INV-D18.
- §4.6 chain subject: pin to `events.<game>.system.crypto_policy.<policy_name>`.
- §4.6 chain payload storage: state JSON-inside-Event.Payload-inside-marshaled-envelope.
- §10 DENY codes: append `DENY_NOT_ADMIN_ROLE`, `DENY_SESSION_INVALID`, `DENY_SESSION_EXPIRED`, `DENY_DUAL_CONTROL_SELF`, `DENY_APPROVAL_EXPIRED`, `DENY_APPROVAL_ALREADY_APPROVED`.

- [ ] **Step 2: Extend the substring test**

In `internal/access/spec_amendments_test.go`, add a new test or extend `TestSpecAmendmentsLanded`:

```go
func TestSpecAmendmentsLandedSubEpicD(t *testing.T) {
    spec := readMasterSpec(t)
    cases := map[string]string{
        "D1": "Authenticate(ctx context.Context, req AuthRequest) (OperatorIdentity, error)",
        "D2": "RoleStore.PlayerHasRole",
        "D3": "events.<game>.system.crypto_policy",
        "D4": "SHA-256(proto.MarshalOptions{Deterministic: true}.Marshal(args))",
        "D5": "DENY_NOT_ADMIN_ROLE",
        "D6": "DENY_SESSION_EXPIRED",
        "D7": "DENY_DUAL_CONTROL_SELF",
    }
    for name, substr := range cases {
        t.Run(name, func(t *testing.T) {
            require.Contains(t, spec, substr, "amendment %s missing", name)
        })
    }
    // NEGATE: assert removed substrings are gone.
    removed := []string{
        "RequireDualControl(ctx context.Context, primary",
        "prompt PromptFunc",
    }
    for _, sub := range removed {
        require.NotContains(t, spec, sub, "removed substring still present: %s", sub)
    }
}
```

- [ ] **Step 3: Run tests**

Run: `task test -- ./internal/access/...`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
jj --no-pager commit -m "docs(crypto): master-spec amendments for Phase 5 sub-epic D (holomush-jxo8.6 T23)

Apply the 7 amendments from D's design spec §10:
- §5.9 interface reshape (server-side AuthRequest, drop PromptFunc)
- §5.9 default-impl 6-step ordering canonicalized
- §5.9 step 5 reified as RoleStore.PlayerHasRole
- §6.3.1 op_args_hash + protobuf-go pin
- §4.6 chain subject (events.>) + storage shape (JSON inside envelope)
- §10 DENY codes table additions

TestSpecAmendmentsLandedSubEpicD substring-asserts each row + NEGATEs
removed strings (PromptFunc / RequireDualControl) so future edits can't
silently revert."
```

---

## Task 24: Protobuf-go pin meta-test

**Spec:** INV-D18.

**Files:**

- Create: `internal/admin/approval/proto_meta_test.go`

- [ ] **Step 1: Write the meta-test**

```go
package approval

import (
    "os"
    "regexp"
    "strings"
    "testing"
)

// TestProtoDeterministicMarshalLockedToVendoredProtobuf locks the
// google.golang.org/protobuf module pin per INV-D18. The pin is
// load-bearing on op_args_hash cross-binary stability (INV-D8).
func TestProtoDeterministicMarshalLockedToVendoredProtobuf(t *testing.T) {
    data, err := os.ReadFile("../../../go.mod")
    if err != nil { t.Fatalf("read go.mod: %v", err) }
    src := string(data)
    re := regexp.MustCompile(`google\.golang\.org/protobuf v[0-9]+\.[0-9]+\.[0-9]+`)
    if !re.MatchString(src) {
        t.Fatalf("go.mod must pin google.golang.org/protobuf to a specific semver per INV-D18")
    }
    // Negate: no replace directive without explicit master-spec amendment.
    if strings.Contains(src, "replace google.golang.org/protobuf") {
        t.Fatalf("replace directive on protobuf-go is a chain-breaking change; treat as master-spec amendment")
    }
}
```

- [ ] **Step 2: Run test**

Run: `task test -- ./internal/admin/approval/`

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
jj --no-pager commit -m "test(crypto): protobuf-go pin meta-test (holomush-jxo8.6 T24)

INV-D18 lock: TestProtoDeterministicMarshalLockedToVendoredProtobuf
asserts go.mod pins google.golang.org/protobuf at a specific semver.
Mirrors INV-D13's JCS lock. Without this, a silent dep bump can break
op_args_hash cross-binary equality (INV-D8/D9) producing a phantom
DENY_APPROVAL_ARGS_MISMATCH on dual-control proceed."
```

---

## Task 25: E2E — `admin authenticate lifecycle`

**Spec:** §8 e2e #1, #2, #3.

**Files:**

- Create: `test/integration/admin_authenticate_test.go` (Ginkgo, build-tag `integration`).

- [ ] **Step 1: Boot the full server** with `crypto.operators=[playerA_ULID]`, an admin character bound to playerA, and a TOTP enrollment for playerA (use sub-epic A's bootstrap-enroll fixtures).

- [ ] **Step 2: Authenticate happy path** — open UDS client, call Authenticate; assert session_token returned; assert no `crypto.totp_locked` event in `events_audit` for playerA.

- [ ] **Step 3: TOTP lockout flow** — call Authenticate × 5 with bad TOTP; assert 5th attempt produces a `crypto.totp_locked` row in `events_audit`; 6th attempt returns DENY_LOCKED.

- [ ] **Step 4: Reset cleared flow** — Authenticate as operator; call ResetTOTP for a separate playerB; assert `events_audit` contains `crypto.totp_cleared` with `cleared_by=admin_reset`; subsequent Authenticate as playerB → DENY_NOT_ENROLLED.

- [ ] **Step 5: Run e2e**

Run: `task test:int -- ./test/integration/`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
jj --no-pager commit -m "test(crypto): e2e admin authenticate lifecycle (holomush-jxo8.6 T25)

Three Ginkgo specs covering the Authenticate happy path, the TOTP
lockout flow (5-fail → crypto.totp_locked emitted), and the admin-
reset flow (ResetTOTP → crypto.totp_cleared emitted with
cleared_by=admin_reset). Asserts events_audit projections directly."
```

---

## Task 26: E2E — `admin dual_control`

**Spec:** §8 e2e #4, #5, #6.

**Files:**

- Create: `test/integration/admin_dual_control_test.go`

- [ ] **Step 1: Happy-path approval** — primary opens approval (via internal Repo.Open in the test harness; alternatively, a stub Rekey-shape RPC); second-op CLI calls Approve; primary's `WaitForApproval` returns the approved row.

- [ ] **Step 2: Self-approval rejection** — second-op == primary → DENY_DUAL_CONTROL_SELF.

- [ ] **Step 3: No-capability rejection** — second-op without crypto.operator → DENY_NOT_OPERATOR.

- [ ] **Step 4: No-admin-role rejection** — second-op without admin role → DENY_NOT_ADMIN_ROLE.

- [ ] **Step 5: Expired approval** — Open + advance clock past 5 min; Approve → DENY_APPROVAL_EXPIRED.

- [ ] **Step 6: Args mismatch** — Open with hashA; "proceed" path with hashB → DENY_APPROVAL_ARGS_MISMATCH (verify in the test harness; full Rekey path is sub-epic E's).

- [ ] **Step 7: Run e2e + commit**

```bash
jj --no-pager commit -m "test(crypto): e2e admin dual_control (holomush-jxo8.6 T26)

Six Ginkgo specs covering the dual-control happy path + every DENY
branch (DUAL_CONTROL_SELF, NOT_OPERATOR, NOT_ADMIN_ROLE, APPROVAL_EXPIRED,
APPROVAL_ARGS_MISMATCH). Validates INV-D5/D6/D7/D9 against the full
stack."
```

---

## Task 27: E2E — `admin policy_chain` (genesis + extension + tamper)

**Spec:** §8 e2e #7.

**Files:**

- Create: `test/integration/admin_policy_chain_test.go`

- [ ] **Step 1: Genesis on first boot** — fresh DB; boot server; assert `events_audit` has 1 `crypto.policy_set` row for `dual_control_required`; decode envelope + payload; assert `prev_hash IS NULL`.

- [ ] **Step 2: Chain-extend on second boot** — stop server; change config (e.g., add "rekey" to `crypto.dual_control_required`); boot again; assert second `policy_set` event with `prev_hash == ComputeHash(first)`.

- [ ] **Step 3: Fail-closed on tamper** — direct PG mutation: replace second event's `envelope` bytes with a hand-crafted invalid payload (e.g., wrong policy_hash). Attempt third boot; assert server refuses to start with POLICY_CHAIN_HASH_MISMATCH or POLICY_CHAIN_BROKEN_LINK.

- [ ] **Step 4: Run e2e + commit**

```bash
jj --no-pager commit -m "test(crypto): e2e admin policy_chain integrity (holomush-jxo8.6 T27)

Three Ginkgo specs covering: genesis on fresh DB (prev_hash IS NULL),
chain-extend across reboot with config change (prev_hash chains to
first), and fail-closed startup on chain tamper (POLICY_CHAIN_HASH_MISMATCH
or POLICY_CHAIN_BROKEN_LINK). Validates INV-D10/D11/D12 against the
real events_audit projection path."
```

---

## Task 28: Final `task pr-prep` gate

**Spec:** project rule (CLAUDE.md "Pre-push review gates" + "task pr-prep before push").

- [ ] **Step 1: Run the full PR gate**

Run: `task pr-prep`

Expected: PASS — lint, format, schema, license, unit, integration, e2e all green.

- [ ] **Step 2: If failures occur**

Investigate root cause; fix; commit; re-run. Never `jj git push --no-verify` or skip gates.

- [ ] **Step 3: Once green, the work is ready for the pre-push review chain**

Per CLAUDE.md: `crypto-reviewer` (FIRST, since D touches `eventbus.Publisher` + emits to `events_audit`) → `abac-reviewer` (alongside, since D touches `internal/access/` paths) → `code-reviewer` (last). All must report READY before push.

- [ ] **Step 4: Commit the workspace state if needed (no-op task)**

(Nothing to commit; this task is a procedural gate.)

---

## Self-review notes

- Spec coverage: each of the 19 INV-D invariants maps to at least one task above (T1=INV-D5/D6 schema; T4=INV-D19 helper; T5=INV-D2/D3; T6=INV-D1/D4/D15/D19; T7=INV-D5/D6; T8=INV-D12/D13; T9=INV-D10/D11; T10=INV-D17; T11=INV-D17; T12=INV-D11; T13=INV-D14; T16=INV-D6/D7/D16; T17=INV-D16; T22=startup wiring; T23=amendments meta; T24=INV-D18; T25-27=e2e).
- Placeholder scan: every task has explicit file paths + commands + commit messages. Code-block depth matches the spec's "what's load-bearing": full code where shape matters (T5 SessionStore, T6 InGameCredentialsProvider, T8 ComputePolicyHash, T9 VerifyChain), abbreviated for high-leverage tests (T6 lists test names; T15-17 sketch handler shape) since the actual Go code is small once the dependencies are wired.
- Type consistency: `CryptoPolicySubsystemConfig`, `EmitDeps`, `SessionStore`, `OperatorIdentity`, `Approval`, `PolicySetPayload`, `chainEntry` are referenced consistently across tasks.
- Spec-section traceability: every task header cites the §section + INV-D* it satisfies.
- Risks / sequencing: T14 (proto changes) might force re-running mock generation in dependent packages — call out in any T14 commit message that the regen propagates.

---

## Bead chain structure

```text
holomush-jxo8                       (existing epic — Phase 5: Rekey + AdminReadStream + OperatorAuthProvider)
└── holomush-jxo8.6                 (existing epic — OperatorAuthProvider + dual-control + admin_approvals)
    ├── jxo8.6.1   Migration 000020 admin_approvals
    ├── jxo8.6.2   SubsystemCryptoPolicy + lifecycle wrapper          (merged T2+T11)
    ├── jxo8.6.3   Pin JCS + protobuf-go + meta-tests                 (merged T3+T24)
    ├── jxo8.6.4   RoleStore.PlayerHasRole helper
    ├── jxo8.6.5   SessionStore (in-memory ULID, 10-min TTL)
    ├── jxo8.6.6   InGameCredentialsProvider 6-step sequence
    ├── jxo8.6.7   admin_approvals Postgres repo
    ├── jxo8.6.8   crypto.policy_set payload + JCS hash
    ├── jxo8.6.9   crypto.policy_set chain verifier
    ├── jxo8.6.10  crypto.policy_set chain emitter
    ├── jxo8.6.11  Bootstrap-subsystem chain verifier wiring
    ├── jxo8.6.12  AuditingService decorator wrapping totp.Service
    ├── jxo8.6.13  admin.proto Authenticate+Approve+ResetTOTP RPCs
    ├── jxo8.6.14  Authenticate RPC handler
    ├── jxo8.6.15  Approve RPC handler
    ├── jxo8.6.16  ResetTOTP RPC handler
    ├── jxo8.6.17  Register handlers in admin-socket buildMux
    ├── jxo8.6.18  crypto.dual_control_required config + lax+warn
    ├── jxo8.6.19  holomush admin totp reset CLI
    ├── jxo8.6.20  holomush admin approve CLI
    ├── jxo8.6.21  Wire CryptoPolicySubsystem into productionSubsystems
    ├── jxo8.6.22  Master-spec amendments + meta-test
    ├── jxo8.6.23  E2E admin authenticate lifecycle
    ├── jxo8.6.24  E2E admin dual_control
    └── jxo8.6.25  E2E admin policy_chain integrity
```

All 25 beads use parent `holomush-jxo8.6`, type `task`, priority `2`. Plan task `T28` (final `task pr-prep` gate) is procedural and has no bead.

#### `jxo8.6.1` — Migration 000020 admin_approvals

**Goal:** Land migration 000020 creating the `admin_approvals` table for dual-control approval rows.

**Design reference:** §5 dual-control schema.

**Plan reference:** § Task 1.

**TDD acceptance criteria:** `expectedTables` slice in `internal/store/migrate_integration_test.go` includes `"admin_approvals"`; full migration suite green.

**Verification steps:** `task lint`; `task test -- ./internal/store/...`; `task test:int -- ./internal/store/...`.

**Files touched:**
- `internal/store/migrations/000020_create_admin_approvals.up.sql` — new
- `internal/store/migrations/000020_create_admin_approvals.down.sql` — new
- `internal/store/migrate_integration_test.go` — add to expectedTables

**Dependencies:** none.

**Out of scope:** Repo Go API (`jxo8.6.7`); reaper for expired rows (deferred per decomp spec sub-epic D row).

#### `jxo8.6.2` — `SubsystemCryptoPolicy` constant + lifecycle wrapper (merged T2+T11)

**Goal:** Add `lifecycle.SubsystemCryptoPolicy` constant and the `CryptoPolicySubsystem` lifecycle wrapper; subsystem.Start emits per-policy chain snapshot, fail-closed on Publish error.

**Design reference:** §3 architecture, §6 emitter; INV-D17.

**Plan reference:** § Task 2 + § Task 11.

**TDD acceptance criteria:** `TestCryptoPolicySubsystemIDReturnsCryptoPolicy`; `TestCryptoPolicySubsystemDependsOnAuditProjection`; `TestCryptoPolicySubsystemStartEmitsAllPolicyNames`; `TestCryptoPolicySubsystemFailsStartOnPublishError` (INV-D17); `TestCryptoPolicySubsystemStopIsNoOp`.

**Verification steps:** `task lint`; `task test -- ./internal/lifecycle/... ./internal/admin/policy/...`.

**Files touched:**
- `internal/lifecycle/subsystem.go` — add `SubsystemCryptoPolicy` constant
- `internal/lifecycle/subsystem_test.go` — extend constants test
- `internal/admin/policy/subsystem.go` — new
- `internal/admin/policy/subsystem_test.go` — new

**Dependencies:** `jxo8.6.10` (subsystem.Start calls emitter).

**Out of scope:** wiring into `productionSubsystems` (`jxo8.6.21`).

#### `jxo8.6.3` — Pin JCS + protobuf-go + meta-tests (merged T3+T24)

**Goal:** Pin `github.com/cyberphone/json-canonicalization` at `v0.0.0-20241213102144-19d51d7fe467` (INV-D13) and `google.golang.org/protobuf` at the currently-resolved semver (INV-D18). Lock both pins via meta-tests.

**Design reference:** §6 chain hash; INV-D13, INV-D18; §10 amendment row 4.

**Plan reference:** § Task 3 + § Task 24.

**TDD acceptance criteria:** `TestJCSCanonicalizationLockedToVendoredImpl`; `TestProtoDeterministicMarshalLockedToVendoredProtobuf`.

**Verification steps:** `task lint`; `task test -- ./internal/admin/policy/... ./internal/admin/approval/...`; `go mod tidy && task build`.

**Files touched:**
- `go.mod` — add cyberphone/json-canonicalization at pinned pseudo-version; ensure protobuf-go pinned at specific semver; add comments explaining load-bearing role
- `go.sum` — auto-regenerated
- `internal/admin/policy/jcs_meta_test.go` — new
- `internal/admin/approval/proto_meta_test.go` — new

**Dependencies:** none.

**Out of scope:** consuming the canonicalizer (`jxo8.6.8`); consuming proto deterministic marshal (`jxo8.6.7`/`jxo8.6.15`).

#### `jxo8.6.4` — `RoleStore.PlayerHasRole` helper

**Goal:** Extend `store.RoleStore` with `PlayerHasRole(ctx, playerID, role)` and Postgres impl using `character_roles ⨝ characters` JOIN on `player_id`.

**Design reference:** §4 "Role helper"; INV-D19.

**Plan reference:** § Task 4.

**TDD acceptance criteria:** `TestPlayerHasRole_ReturnsTrueForPlayerWithAdminCharacter`; `TestPlayerHasRole_ReturnsFalseForPlayerWithoutAnyAdminCharacter`; `TestPlayerHasRole_ReturnsFalseForUnknownPlayer`. Compile-time enforcement of fakes (`internal/bootstrap/admin_test.go::fakeRoleStore` updated).

**Verification steps:** `task lint`; `task test`; `task test:int -- ./internal/store/...`.

**Files touched:**
- `internal/store/role_store.go` — add interface method + Postgres impl
- `internal/store/role_store_integration_test.go` — new
- `internal/bootstrap/admin_test.go` — update `fakeRoleStore`

**Dependencies:** none.

**Out of scope:** caller integration into Authenticate (`jxo8.6.6`); ABAC seed policies for the new method.

#### `jxo8.6.5` — `SessionStore` (in-memory ULID, 10-min TTL)

**Goal:** Per-process in-memory session map keyed by ULID token; cleanup-on-Get TTL enforcement; `Issue`/`Get`/`Revoke` methods.

**Design reference:** §4 SessionStore; INV-D2, INV-D3.

**Plan reference:** § Task 5.

**TDD acceptance criteria:** `TestSessionStoreEmptiedOnConstruction` (INV-D3); `TestSessionStoreIssueAndGetReturnsIdentity`; `TestSessionStoreRejectsExpiredToken` (INV-D2); `TestSessionStoreRevoke`; `TestSessionStoreConcurrentIssueAndGet` (race-detector clean).

**Verification steps:** `task lint`; `task test -race -- ./internal/admin/auth/`.

**Files touched:**
- `internal/admin/auth/types.go` — new (OperatorIdentity, AuthRequest, OperatorAuthProvider interface, Clock)
- `internal/admin/auth/session.go` — new
- `internal/admin/auth/session_test.go` — new

**Dependencies:** none.

**Out of scope:** provider impl (`jxo8.6.6`); RPC handler (`jxo8.6.14`).

#### `jxo8.6.6` — `InGameCredentialsProvider` 6-step sequence

**Goal:** Default `OperatorAuthProvider` impl. 6-step ordered sequence: ValidateCredentials → IsEnrolled → Verify → HasPlayerGrant → PlayerHasRole(RoleAdmin) → PeerCred capture. Each failure short-circuits with a typed DENY code; later steps not reached on earlier failure.

**Design reference:** §4 6-step sequence; INV-D1, INV-D4, INV-D15, INV-D19.

**Plan reference:** § Task 6.

**TDD acceptance criteria:** `TestInGameAuthenticateHappyPath`; `TestInGameAuthenticateRejectsInvalidCredentials`; `TestInGameAuthenticateRejectsNotEnrolled`; `TestInGameAuthenticateRejectsBadTOTP`; `TestInGameAuthenticateRejectsLocked`; `TestInGameAuthenticateRejectsNonOperator` (INV-D15); `TestAuthenticateRejectsPlayerWithoutAdminRole` (INV-D19); `TestAuthenticateIgnoresPeerCredForGating` (INV-D4); `TestAuthenticateStepOrderFixedOnFailure` (INV-D1).

**Verification steps:** `task lint`; `task test -- ./internal/admin/auth/`.

**Files touched:**
- `internal/admin/auth/ingame.go` — new
- `internal/admin/auth/ingame_test.go` — new
- `mockery.yml` — register `internal/admin/auth/` interfaces; regenerate mocks via `task generate`

**Dependencies:** `jxo8.6.4` (PlayerHasRole), `jxo8.6.5` (SessionStore types), `jxo8.6.12` (AuditingService.Verify).

**Out of scope:** RPC handler (`jxo8.6.14`); session minting (handler's job).

#### `jxo8.6.7` — `admin_approvals` Postgres repo

**Goal:** `Repo` interface (Open/Get/MarkApproved/WaitForApproval) plus Postgres impl with atomic single-statement MarkApproved that rejects self-approval, expired rows, and already-approved rows at the SQL layer.

**Design reference:** §5 dual-control; INV-D5, INV-D6.

**Plan reference:** § Task 7.

**TDD acceptance criteria:** `TestRepoOpenAndGet`; `TestRepoReadFiltersExpired` (INV-D5); `TestRepoMarkApproved`; `TestRepoMarkApprovedRejectsSelfApproval` (INV-D6); `TestRepoMarkApprovedRejectsExpiredRow`; `TestRepoMarkApprovedRejectsAlreadyApproved`; `TestRepoConcurrentMarkApproved` (race serialization).

**Verification steps:** `task lint`; `task test -- ./internal/admin/approval/`; `task test:int -- ./internal/admin/approval/`.

**Files touched:**
- `internal/admin/approval/types.go` — new (RequestID, OpenRequest, Approval)
- `internal/admin/approval/repo.go` — new (interface + Postgres impl)
- `internal/admin/approval/repo_test.go` — new
- `internal/admin/approval/repo_integration_test.go` — new

**Dependencies:** `jxo8.6.1` (migration), `jxo8.6.3` (proto deterministic marshal pin used downstream by Open's hash callers).

**Out of scope:** RPC handler (`jxo8.6.15`).

#### `jxo8.6.8` — `crypto.policy_set` payload + JCS hash

**Goal:** `PolicySetPayload` struct + `ComputePolicyHash` helper computing SHA-256 over JCS-canonicalized JSON of payload with `policy_hash` field excluded.

**Design reference:** §6 hash algorithm; INV-D12, INV-D13.

**Plan reference:** § Task 8.

**TDD acceptance criteria:** `TestComputePolicyHashGoldenValue`; `TestComputePolicyHashExcludesPolicyHashField`; `TestComputePolicyHashStableUnderJSONFieldReorder`.

**Verification steps:** `task lint`; `task test -- ./internal/admin/policy/`.

**Files touched:**
- `internal/admin/policy/chain.go` — new
- `internal/admin/policy/chain_test.go` — new

**Dependencies:** `jxo8.6.3` (JCS pin).

**Out of scope:** verifier (`jxo8.6.9`); emitter (`jxo8.6.10`).

#### `jxo8.6.9` — `crypto.policy_set` chain verifier

**Goal:** `VerifyChain(ctx, pool, subject, policyName)` reads `events_audit` rows for chain subject ordered by `js_seq`, two-step decodes (proto envelope → JSON payload), walks chain checking `prev_hash` continuity and recomputed `policy_hash`. Fails-closed on any integrity violation.

**Design reference:** §6 verifier; INV-D10, INV-D11.

**Plan reference:** § Task 9.

**TDD acceptance criteria:** `TestVerifyChainAcceptsEmptyChain`; `TestVerifyChainAcceptsValidGenesis`; `TestVerifyChainAcceptsValidExtension`; `TestVerifyChainRejectsBrokenGenesis` (INV-D11); `TestVerifyChainRejectsBrokenLink`; `TestVerifyChainRejectsHashMismatch`; `TestVerifyChainDecodesEnvelopeAndJSON`; integration `TestVerifierAgainstRealEventsAudit`.

**Verification steps:** `task lint`; `task test -- ./internal/admin/policy/`; `task test:int -- ./internal/admin/policy/`.

**Files touched:**
- `internal/admin/policy/verifier.go` — new
- `internal/admin/policy/verifier_test.go` — new
- `internal/admin/policy/verifier_integration_test.go` — new

**Dependencies:** `jxo8.6.8`.

**Out of scope:** Bootstrap subsystem wiring (`jxo8.6.11`).

#### `jxo8.6.10` — `crypto.policy_set` chain emitter

**Goal:** `EmitCurrentSnapshot` reads latest existing event for policy_name, builds new `PolicySetPayload` with `prev_hash = ComputePolicyHash(previous)` (or null at genesis), publishes via `eventbus.Publisher`. Idempotent on no-change (post-JCS byte-equal). Publish failure returns wrapped error so `Subsystem.Start` fails-closed.

**Design reference:** §6 emitter; INV-D17.

**Plan reference:** § Task 10.

**TDD acceptance criteria:** `TestEmitCurrentSnapshotGenesis`; `TestEmitCurrentSnapshotExtension`; `TestEmitCurrentSnapshotIdempotentOnNoChange`; `TestEmitCurrentSnapshotFailsOnPublishError` (INV-D17).

**Verification steps:** `task lint`; `task test -- ./internal/admin/policy/`.

**Files touched:**
- `internal/admin/policy/emitter.go` — new
- `internal/admin/policy/emitter_test.go` — new

**Dependencies:** `jxo8.6.8`, `jxo8.6.9` (reuses chain reader).

**Out of scope:** subsystem wrapper (`jxo8.6.2`).

#### `jxo8.6.11` — Bootstrap-subsystem chain verifier wiring

**Goal:** `BootstrapSubsystem.Start` calls `policy.VerifyChain` for each known policy_name after INV-32/33/37 + orphan check. Fails-closed on any chain integrity violation.

**Design reference:** §6 verifier (Bootstrap); INV-D11.

**Plan reference:** § Task 12.

**TDD acceptance criteria:** `TestBootstrapRefusesStartOnPolicyChainVerifyFailure` (integration; seeds broken chain in `events_audit`).

**Verification steps:** `task lint`; `task test -- ./internal/bootstrap/...`; `task test:int -- ./internal/bootstrap/...`.

**Files touched:**
- `internal/bootstrap/setup/subsystem.go` — extend Start with chain-verify loop
- `internal/bootstrap/setup/subsystem_integration_test.go` — new test

**Dependencies:** `jxo8.6.9`.

**Out of scope:** emitter wiring (`jxo8.6.21` via `productionSubsystems`).

#### `jxo8.6.12` — `AuditingService` decorator

**Goal:** Wrap `totp.Service` to emit `crypto.totp_*` lifecycle events on observed state transitions (locked / recovery_consumed / cleared); RecoverAndClear emits both events. Publish failure logs via `slog.Warn` and continues — does NOT roll back inner Service PG state.

**Design reference:** §7 decorator; INV-D14.

**Plan reference:** § Task 13.

**TDD acceptance criteria:** `TestAuditingServiceVerifyEmitsLockedOnTransition`; `TestAuditingServiceVerifyDoesNotEmitWithoutTransition`; `TestAuditingServiceConsumeRecoveryCodeEmitsRecoveryConsumed`; `TestAuditingServiceClearTOTPEmitsCleared`; `TestAuditingServiceRecoverAndClearEmitsBoth`; `TestAuditingServiceLogsAndContinuesOnPublishError` (INV-D14); `TestAuditingServiceWrapsAllStateTransitionMethods`.

**Verification steps:** `task lint`; `task test -- ./internal/admin/totp_audit/`.

**Files touched:**
- `internal/admin/totp_audit/auditing.go` — new
- `internal/admin/totp_audit/auditing_test.go` — new

**Dependencies:** none (uses sub-epic A's `internal/totp/audit.go` reserved subjects + payloads + EventType constants, all already merged).

**Out of scope:** consumer wiring (`jxo8.6.6`/`jxo8.6.16`).

#### `jxo8.6.13` — `admin.proto` Authenticate/Approve/ResetTOTP RPCs

**Goal:** Extend `api/proto/holomush/admin/v1/admin.proto` with three new unary RPCs and their request/response messages; regenerate both grpc-go and ConnectRPC bindings per repo convention.

**Design reference:** §3 wire surface.

**Plan reference:** § Task 14.

**TDD acceptance criteria:** generated bindings compile; `task lint && task test && task build` green; smoke test of `adminv1connect.NewAdmin*Handler` constructors via existing `internal/admin/socket/status_handler_test.go` pattern.

**Verification steps:** `task lint`; `task proto:generate`; `task test`; `task build`.

**Files touched:**
- `api/proto/holomush/admin/v1/admin.proto` — modify
- `pkg/proto/holomush/admin/v1/*` — regenerated
- `pkg/proto/holomush/admin/v1/adminv1connect/*` — regenerated

**Dependencies:** none.

**Out of scope:** handler implementations (`jxo8.6.14`/`.15`/`.16`); mux registration (`jxo8.6.17`).

#### `jxo8.6.14` — Authenticate RPC handler

**Goal:** ConnectRPC handler runs `OperatorAuthProvider.Authenticate`, captures `PeerCred` from ctx via sub-epic C's middleware, mints session token via `SessionStore.Issue` on success, returns `AuthenticateResponse{session_token, expires_at, player_id}`. DENY errors map to ConnectRPC codes with typed `oops.Code()` preserved.

**Design reference:** §3 wire surface, §4 sequence.

**Plan reference:** § Task 15.

**TDD acceptance criteria:** `TestAuthenticateHandlerHappyPath`; `TestAuthenticateHandlerSurfacesEachDENYcode` (table test for the 6 DENY paths); `TestAuthenticateHandlerCapturesPeerCredIntoIdentity`.

**Verification steps:** `task lint`; `task test -- ./internal/admin/auth/`.

**Files touched:**
- `internal/admin/auth/handler.go` — new
- `internal/admin/auth/handler_test.go` — new

**Dependencies:** `jxo8.6.5` (SessionStore), `jxo8.6.6` (Provider), `jxo8.6.13` (proto types).

**Out of scope:** mux registration (`jxo8.6.17`); CLI (`jxo8.6.20`).

#### `jxo8.6.15` — Approve RPC handler

**Goal:** Handler resolves session via `SessionStore.Get`, re-asserts capability + role checks (defense in depth), then calls `approval.Repo.MarkApproved(request_id, session.PlayerID)`. Repo's atomic UPDATE rejects self-approval / expired / already-approved with typed errors propagated.

**Design reference:** §5 dual-control; INV-D6, INV-D7, INV-D16.

**Plan reference:** § Task 16.

**TDD acceptance criteria:** `TestApproveHandlerRequiresValidSession`; `TestApproveHandlerRejectsExpiredSession`; `TestApproveHandlerRequiresCapabilityOnHandler` (INV-D16); `TestApproveHandlerRequiresAdminRoleOnHandler` (INV-D16); `TestApproveHandlerCallsRepoMarkApproved`; `TestApproveHandlerSurfacesSelfApprovalDenial`; `TestApproveHandlerSurfacesAlreadyApprovedDenial`; `TestApproveHandlerSurfacesExpiredApprovalDenial`.

**Verification steps:** `task lint`; `task test -- ./internal/admin/approval/`.

**Files touched:**
- `internal/admin/approval/handler.go` — new
- `internal/admin/approval/handler_test.go` — new

**Dependencies:** `jxo8.6.4` (PlayerHasRole), `jxo8.6.5` (SessionStore), `jxo8.6.7` (Repo), `jxo8.6.13` (proto types).

**Out of scope:** mux registration (`jxo8.6.17`); CLI (`jxo8.6.20`).

#### `jxo8.6.16` — ResetTOTP RPC handler

**Goal:** Handler resolves session, re-asserts capability + role, calls `AuditingService.ClearTOTP(targetPID, totp.ClearReasonAdminReset)`. Decorator emits `crypto.totp_cleared` with `cleared_by=admin_reset` on success.

**Design reference:** §3 wire surface; INV-D16.

**Plan reference:** § Task 17.

**TDD acceptance criteria:** `TestResetTOTPHandlerRequiresValidSession`; `TestResetTOTPRequiresCapabilityOnHandler` (INV-D16); `TestResetTOTPRequiresAdminRoleOnHandler` (INV-D16); `TestResetTOTPHandlerCallsClearTOTPThroughDecorator`.

**Verification steps:** `task lint`; `task test -- ./internal/admin/auth/`.

**Files touched:**
- `internal/admin/auth/reset_handler.go` — new
- `internal/admin/auth/reset_handler_test.go` — new

**Dependencies:** `jxo8.6.4`, `jxo8.6.5`, `jxo8.6.12` (AuditingService), `jxo8.6.13` (proto types).

**Out of scope:** mux registration (`jxo8.6.17`); CLI (`jxo8.6.19`).

#### `jxo8.6.17` — Register handlers in admin-socket `buildMux`

**Goal:** Extend `internal/admin/socket/server.go::buildMux` to register Authenticate / Approve / ResetTOTP handlers; extend `Server.Config` with the three handler fields; update `NewServer` callers.

**Design reference:** §3 wire surface.

**Plan reference:** § Task 18.

**TDD acceptance criteria:** Existing `TestServerServesStatusRPCOverUDS` pattern extended to assert each new endpoint receives requests; build green for all callers.

**Verification steps:** `task lint`; `task test -- ./internal/admin/socket/`; `task build`.

**Files touched:**
- `internal/admin/socket/server.go` — extend Config + buildMux
- `internal/admin/socket/server_test.go` — extend
- `cmd/holomush/sub_admin_socket.go` (or equivalent NewServer caller) — pass new handlers

**Dependencies:** `jxo8.6.13`, `jxo8.6.14`, `jxo8.6.15`, `jxo8.6.16`.

**Out of scope:** subsystem wiring (`jxo8.6.21`).

#### `jxo8.6.18` — `crypto.dual_control_required` config + lax+warn validation

**Goal:** Add `DualControlRequired []string` field to `internal/config/config.go::CryptoConfig`; implement `validateDualControlRequired` in `cmd/holomush/` mirroring sub-epic B's `crypto.operators` lax+warn pattern.

**Design reference:** §5 layer-2 enforcement; §9 validation.

**Plan reference:** § Task 19.

**TDD acceptance criteria:** `TestValidateDualControlRequired_FiltersUnknownOps`; `TestValidateDualControlRequired_PreservesKnownOps`; `TestValidateDualControlRequired_AcceptsEmpty`.

**Verification steps:** `task lint`; `task test -- ./cmd/holomush/...`.

**Files touched:**
- `internal/config/config.go` — add field
- `cmd/holomush/crypto_dual_control_validation.go` — new
- `cmd/holomush/crypto_dual_control_validation_test.go` — new

**Dependencies:** none.

**Out of scope:** server-side enforcement of the policy (lives in E's RekeyHandler / F's AdminReadStreamHandler).

#### `jxo8.6.19` — `holomush admin totp reset` CLI

**Goal:** Cobra subcommand prompts operator for credentials + TOTP, opens ConnectRPC over UDS, calls `Authenticate` then `ResetTOTP` for the target player_id arg.

**Design reference:** §11 deferred from sub-epic A.

**Plan reference:** § Task 20.

**TDD acceptance criteria:** Table-driven CLI tests with fake server (httptest UDS) responding with canned ResetTOTPResponse / DENY errors.

**Verification steps:** `task lint`; `task test -- ./cmd/holomush/...`.

**Files touched:**
- `cmd/holomush/cmd_admin_totp_reset.go` — new
- `cmd/holomush/cmd_admin_totp_reset_test.go` — new

**Dependencies:** `jxo8.6.16` (handler the CLI calls).

**Out of scope:** dual-control over reset (single-control by spec; deferred).

#### `jxo8.6.20` — `holomush admin approve` CLI

**Goal:** Second-op signoff CLI. Same prompting flow as the totp-reset CLI but ends in `Approve(session_token, request_id)`.

**Design reference:** §5 dual-control second-op.

**Plan reference:** § Task 21.

**TDD acceptance criteria:** Table-driven CLI tests with fake server.

**Verification steps:** `task lint`; `task test -- ./cmd/holomush/...`.

**Files touched:**
- `cmd/holomush/cmd_admin_approve.go` — new
- `cmd/holomush/cmd_admin_approve_test.go` — new

**Dependencies:** `jxo8.6.15` (handler the CLI calls).

**Out of scope:** approval issuance (E's RekeyHandler creates the row; second-op CLI just consumes it).

#### `jxo8.6.21` — Wire `CryptoPolicySubsystem` into `productionSubsystems`

**Goal:** `productionSubsystems()` slice grows from 12 to 13; CryptoPolicySubsystem inserted between AuditProjection and gRPC; constructor wired in `runCoreWithDeps` with all required deps (DB pool, Publisher, GameID, server-start ULID, server identity, current effective config).

**Design reference:** §6 startup ordering.

**Plan reference:** § Task 22.

**TDD acceptance criteria:** `TestProductionSubsystemsIncludesCryptoPolicy` (parallel to existing AdminSocket test); subsystem-count assertion updated to 13.

**Verification steps:** `task lint`; `task test -- ./cmd/holomush/...`.

**Files touched:**
- `cmd/holomush/core.go` — modify productionSubsystems + runCoreWithDeps wiring
- `cmd/holomush/deps.go` — extend CoreDeps with CryptoPolicy fields
- `cmd/holomush/core_subsystems_test.go` — extend

**Dependencies:** `jxo8.6.2`, `jxo8.6.18` (effective config from validation).

**Out of scope:** Bootstrap-side verifier wiring (`jxo8.6.11`).

#### `jxo8.6.22` — Master-spec amendments + meta-test

**Goal:** Apply the 7 amendment rows from D's design spec §10 to the master crypto spec; extend `TestSpecAmendmentsLanded` (or add `TestSpecAmendmentsLandedSubEpicD`) with positive substring-asserts for new text and NEGATE substring-asserts for removed text (PromptFunc / RequireDualControl).

**Design reference:** §10 amendments table.

**Plan reference:** § Task 23.

**TDD acceptance criteria:** `TestSpecAmendmentsLandedSubEpicD` with one sub-test per amendment row; sub-tests for removed-substring NEGATE asserts.

**Verification steps:** `task lint`; `task test -- ./internal/access/...`; `task lint:markdown`.

**Files touched:**
- `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` — apply 7 amendments
- `internal/access/spec_amendments_test.go` — extend with D's substrings

**Dependencies:** none (docs change; can land any time, but the meta-test fails until amendments are committed — typically lands alongside or after the implementation beads).

**Out of scope:** decomposition spec edits (B already shipped its scope; D's amendments are master-spec only).

#### `jxo8.6.23` — E2E admin authenticate lifecycle

**Goal:** Three Ginkgo specs over the full server stack: Authenticate happy path; TOTP lockout flow (5-fail → `crypto.totp_locked` emitted); admin-reset flow (ResetTOTP → `crypto.totp_cleared` with `cleared_by=admin_reset`).

**Design reference:** §8 e2e #1, #2, #3.

**Plan reference:** § Task 25.

**TDD acceptance criteria:** Specs in `test/integration/admin_authenticate_test.go` covering the three flows; assertions on `events_audit` projection.

**Verification steps:** `task lint`; `task test:int -- ./test/integration/`.

**Files touched:**
- `test/integration/admin_authenticate_test.go` — new

**Dependencies:** `jxo8.6.17` (mux), `jxo8.6.18` (config), `jxo8.6.19` (totp reset CLI).

**Out of scope:** dual-control flows (`jxo8.6.24`); chain integrity (`jxo8.6.25`).

#### `jxo8.6.24` — E2E admin dual_control

**Goal:** Six Ginkgo specs covering dual-control happy path + every DENY branch (DUAL_CONTROL_SELF, NOT_OPERATOR, NOT_ADMIN_ROLE, APPROVAL_EXPIRED, APPROVAL_ARGS_MISMATCH).

**Design reference:** §8 e2e #4, #5, #6.

**Plan reference:** § Task 26.

**TDD acceptance criteria:** Specs in `test/integration/admin_dual_control_test.go`; harness uses `approval.Repo` directly (or a stub Rekey-shape RPC) since the actual Rekey lives in E.

**Verification steps:** `task lint`; `task test:int -- ./test/integration/`.

**Files touched:**
- `test/integration/admin_dual_control_test.go` — new

**Dependencies:** `jxo8.6.15` (Approve handler), `jxo8.6.20` (approve CLI).

**Out of scope:** Rekey invocation flow (sub-epic E owns).

#### `jxo8.6.25` — E2E admin policy_chain integrity

**Goal:** Three Ginkgo specs: genesis on fresh DB (`prev_hash IS NULL`); chain-extend across reboot with config change (`prev_hash` chains to first); fail-closed startup on tamper (`POLICY_CHAIN_HASH_MISMATCH` or `POLICY_CHAIN_BROKEN_LINK`).

**Design reference:** §8 e2e #7.

**Plan reference:** § Task 27.

**TDD acceptance criteria:** Specs in `test/integration/admin_policy_chain_test.go` validating INV-D10/D11/D12 against real `events_audit` projection path.

**Verification steps:** `task lint`; `task test:int -- ./test/integration/`.

**Files touched:**
- `test/integration/admin_policy_chain_test.go` — new

**Dependencies:** `jxo8.6.10` (emitter), `jxo8.6.21` (productionSubsystems wiring).

**Out of scope:** Rekey/AdminReadStream-side `policy_hash` embedding (E/F own).

### Closing-out operations

- **Existing beads to close:** none (all sub-epic D children are NEW; nothing predates).
- **Existing beads to update:** parent `holomush-jxo8.6` — once all 25 children close, parent transitions from in-progress to closed.
- **Follow-up beads to file:** none required by D's scope. (Open seams already deferred to E/F: Rekey/AdminReadStream RPCs, `policy_hash` embedding into their audit events, dual-control config hot-reload — these are NOT D's territory.)

### `bd dep add` edges

```bash
bd dep add holomush-jxo8.6.6 holomush-jxo8.6.4
bd dep add holomush-jxo8.6.6 holomush-jxo8.6.5
bd dep add holomush-jxo8.6.6 holomush-jxo8.6.12
bd dep add holomush-jxo8.6.7 holomush-jxo8.6.1
bd dep add holomush-jxo8.6.7 holomush-jxo8.6.3
bd dep add holomush-jxo8.6.8 holomush-jxo8.6.3
bd dep add holomush-jxo8.6.9 holomush-jxo8.6.8
bd dep add holomush-jxo8.6.10 holomush-jxo8.6.8
bd dep add holomush-jxo8.6.10 holomush-jxo8.6.9
bd dep add holomush-jxo8.6.2 holomush-jxo8.6.10
bd dep add holomush-jxo8.6.11 holomush-jxo8.6.9
bd dep add holomush-jxo8.6.14 holomush-jxo8.6.5
bd dep add holomush-jxo8.6.14 holomush-jxo8.6.6
bd dep add holomush-jxo8.6.14 holomush-jxo8.6.13
bd dep add holomush-jxo8.6.15 holomush-jxo8.6.4
bd dep add holomush-jxo8.6.15 holomush-jxo8.6.5
bd dep add holomush-jxo8.6.15 holomush-jxo8.6.7
bd dep add holomush-jxo8.6.15 holomush-jxo8.6.13
bd dep add holomush-jxo8.6.16 holomush-jxo8.6.4
bd dep add holomush-jxo8.6.16 holomush-jxo8.6.5
bd dep add holomush-jxo8.6.16 holomush-jxo8.6.12
bd dep add holomush-jxo8.6.16 holomush-jxo8.6.13
bd dep add holomush-jxo8.6.17 holomush-jxo8.6.13
bd dep add holomush-jxo8.6.17 holomush-jxo8.6.14
bd dep add holomush-jxo8.6.17 holomush-jxo8.6.15
bd dep add holomush-jxo8.6.17 holomush-jxo8.6.16
bd dep add holomush-jxo8.6.19 holomush-jxo8.6.16
bd dep add holomush-jxo8.6.20 holomush-jxo8.6.15
bd dep add holomush-jxo8.6.21 holomush-jxo8.6.2
bd dep add holomush-jxo8.6.21 holomush-jxo8.6.18
bd dep add holomush-jxo8.6.23 holomush-jxo8.6.17
bd dep add holomush-jxo8.6.23 holomush-jxo8.6.18
bd dep add holomush-jxo8.6.23 holomush-jxo8.6.19
bd dep add holomush-jxo8.6.24 holomush-jxo8.6.15
bd dep add holomush-jxo8.6.24 holomush-jxo8.6.20
bd dep add holomush-jxo8.6.25 holomush-jxo8.6.10
bd dep add holomush-jxo8.6.25 holomush-jxo8.6.21
```

---

## Execution handoff

**Plan complete and saved to `docs/superpowers/plans/2026-05-09-phase5-sub-epic-d.md`. Two execution options:**

1. **Subagent-Driven (recommended)** — dispatch a fresh subagent per task, review between tasks, fast iteration via `superpowers:subagent-driven-development`.

2. **Inline Execution** — execute tasks in this session via `superpowers:executing-plans` with batch checkpoints.

Per CLAUDE.md, before invoking either: `plan-reviewer` MUST run on this plan and return READY; then `bead-chain-design` writes the `## Bead chain structure` section into this plan; then `bead-chain-from-plan` materializes the bd issues.
