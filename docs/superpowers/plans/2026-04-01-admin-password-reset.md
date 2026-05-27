<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Admin Password Reset Command — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `resetpassword` command that lets admins reset a player's password with tiered ABAC capabilities for generated vs explicit passwords and optional session kick.

**Architecture:** Closure-injected command handler following the existing shutdown handler pattern. The factory function captures auth dependencies (`PlayerRepository`, `PasswordHasher`, `WebSessionRepository`, `PasswordResetRepository`, `CharacterLister`) and returns a `CommandHandler`. Game session access uses `exec.Services().Session()`. ABAC is handled by the dispatcher for the base capability and conditionally by the handler for elevated capabilities.

**Tech Stack:** Go, oops (structured errors), slog (audit logging), crypto/rand (password generation), testify + mockery (unit tests), Ginkgo/Gomega (integration tests), Playwright (E2E tests)

**Spec:** `docs/superpowers/specs/2026-04-01-admin-password-reset-design.md`

---

## File Map

| File | Action | Responsibility |
| --- | --- | --- |
| `internal/command/handlers/resetpassword.go` | Create | Handler factory, arg parsing, password generation, lockout clearing, audit logging |
| `internal/command/handlers/resetpassword_test.go` | Create | Unit tests with mocked dependencies |
| `internal/command/handlers/register.go` | Modify | Add `RegisterAdmin` function for closure-injected handlers |
| `internal/command/errors.go` | Modify | Add `RESET_PASSWORD_FAILED` error code + constructor |
| `cmd/holomush/core.go` | Modify | Hoist `authSessionRepo`/`authResetRepo`/`authHasher` and wire into `RegisterAdmin` |
| `test/integration/command/resetpassword_integration_test.go` | Create | Integration tests with real PostgreSQL via testcontainers |
| `compose.e2e.yaml` | Modify | Set `HOLOMUSH_ADMIN_USERNAME`/`HOLOMUSH_ADMIN_PASSWORD` for E2E admin login |
| `web/e2e/helpers/db.ts` | Modify | Add `getPlayerPasswordHash`, `getWebSessionsByPlayerId` helpers |
| `web/e2e/admin.spec.ts` | Create | E2E tests for admin password reset with DB validation |
| `site/docs/reference/access-control.md` | Modify | Document new capabilities |

**ABAC note:** No new seed policies needed. The existing `seed:admin-full-access` policy (`permit(principal is character, action, resource) when { "admin" in principal.character.roles }`) is a blanket grant that already covers all three new capabilities for admin-role characters. Non-admin players are denied by default (no matching `permit` policy).

---

## Chunk 1: Handler Implementation + Unit Tests

### Task 1: Add error code for password reset failures

**Files:**

- Modify: `internal/command/errors.go`

- [ ] **Step 1: Add `RESET_PASSWORD_FAILED` code and constructor**

In `internal/command/errors.go`, add after the `CodeNilServices` constant:

```go
CodeResetPasswordFailed = "RESET_PASSWORD_FAILED"
```

Add a constructor after `ErrNilServices`:

```go
// ErrResetPasswordFailed creates an error for password reset failures.
func ErrResetPasswordFailed(cause error) error {
	return oops.Code(CodeResetPasswordFailed).
		With("message", "Password reset failed. Please try again.").
		Wrap(cause)
}
```

Add a case in the `PlayerMessage` switch:

```go
case CodeResetPasswordFailed:
	if msg, ok := oopsErr.Context()["message"].(string); ok {
		return msg
	}
	return "Password reset failed."
```

- [ ] **Step 2: Run tests**

Run: `task test -- -run TestPlayerMessage -v ./internal/command/...`
Expected: existing tests PASS (new code is additive)

- [ ] **Step 3: Commit**

```text
feat(command): add RESET_PASSWORD_FAILED error code
```

---

### Task 2: Create resetpassword handler with generated password support

**Files:**

- Create: `internal/command/handlers/resetpassword.go`

- [ ] **Step 1: Create the handler file**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/world"
)

// CharacterLister provides character lookup by player ID.
// Narrow interface (ISP) — only the method the handler needs.
type CharacterLister interface {
	ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*world.Character, error)
}

// AdminDeps holds dependencies for admin command handlers that need
// auth-layer access beyond what command.Services provides.
type AdminDeps struct {
	PlayerRepo  auth.PlayerRepository
	Hasher      auth.PasswordHasher
	WebSessions auth.WebSessionRepository
	ResetRepo   auth.PasswordResetRepository
	CharLister  CharacterLister
}

const (
	resetPasswordCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	resetPasswordLength  = 16
	minPasswordLength    = 8
)

// NewResetPasswordHandler creates a command handler for admin password resets.
// Dependencies are captured via closure injection.
func NewResetPasswordHandler(deps AdminDeps) command.CommandHandler {
	return func(ctx context.Context, exec *command.CommandExecution) error {
		return handleResetPassword(ctx, exec, deps)
	}
}

func handleResetPassword(ctx context.Context, exec *command.CommandExecution, deps AdminDeps) error {
	username, password, kick, err := parseResetArgs(exec.Args)
	if err != nil {
		return err
	}

	// Check elevated capabilities conditionally.
	subject := fmt.Sprintf("character:%s", exec.CharacterID().String())

	if password != "" {
		if capErr := command.CheckCapability(ctx, exec.Services().Engine(),
			subject, "admin:password.set", "resetpassword"); capErr != nil {
			writeOutput(ctx, exec, "resetpassword",
				"Setting explicit passwords requires admin:password.set capability.")
			return oops.Code(command.CodePermissionDenied).
				With("command", "resetpassword").
				With("capability", "admin:password.set").
				Errorf("permission denied for explicit password")
		}
	}

	if kick {
		if capErr := command.CheckCapability(ctx, exec.Services().Engine(),
			subject, "admin:session.kick", "resetpassword"); capErr != nil {
			writeOutput(ctx, exec, "resetpassword",
				"Kicking sessions requires admin:session.kick capability.")
			return oops.Code(command.CodePermissionDenied).
				With("command", "resetpassword").
				With("capability", "admin:session.kick").
				Errorf("permission denied for session kick")
		}
	}

	// Look up target player.
	player, lookupErr := deps.PlayerRepo.GetByUsername(ctx, username)
	if lookupErr != nil {
		writeOutputf(ctx, exec, "resetpassword", "Player '%s' not found.\n", username)
		return command.ErrTargetNotFound(username)
	}

	// Generate or validate password.
	generated := false
	if password == "" {
		password, err = generatePassword()
		if err != nil {
			slog.ErrorContext(ctx, "failed to generate password", "error", err)
			return command.ErrResetPasswordFailed(err)
		}
		generated = true
	} else if len(password) < minPasswordLength {
		return command.ErrInvalidArgs("resetpassword",
			"resetpassword <player> [password] [--kick]")
	}

	// Hash and update.
	hash, hashErr := deps.Hasher.Hash(password)
	if hashErr != nil {
		slog.ErrorContext(ctx, "failed to hash password",
			"target_player_id", player.ID.String(), "error", hashErr)
		return command.ErrResetPasswordFailed(hashErr)
	}

	if updateErr := deps.PlayerRepo.UpdatePassword(ctx, player.ID, hash); updateErr != nil {
		slog.ErrorContext(ctx, "failed to update password",
			"target_player_id", player.ID.String(), "error", updateErr)
		return command.ErrResetPasswordFailed(updateErr)
	}

	// Best-effort: clear account lockout (FailedAttempts + LockedUntil).
	player.RecordSuccess()
	if lockErr := deps.PlayerRepo.Update(ctx, player); lockErr != nil {
		slog.WarnContext(ctx, "failed to clear account lockout",
			"target_player_id", player.ID.String(), "error", lockErr)
	}

	// Best-effort: invalidate outstanding email-based reset tokens.
	if delErr := deps.ResetRepo.DeleteByPlayer(ctx, player.ID); delErr != nil {
		slog.WarnContext(ctx, "failed to delete reset tokens",
			"target_player_id", player.ID.String(), "error", delErr)
	}

	// Best-effort: invalidate web sessions.
	if delErr := deps.WebSessions.DeleteByPlayer(ctx, player.ID); delErr != nil {
		slog.WarnContext(ctx, "failed to delete web sessions",
			"target_player_id", player.ID.String(), "error", delErr)
	}

	// Kick game sessions if requested.
	if kick {
		kickGameSessions(ctx, exec, deps, player.ID)
	}

	// Audit log.
	slog.Info("admin password reset",
		"event", "admin_password_reset",
		"admin_player_id", exec.PlayerID().String(),
		"admin_character_id", exec.CharacterID().String(),
		"admin_character_name", exec.CharacterName(),
		"target_player_id", player.ID.String(),
		"target_username", player.Username,
		"password_generated", generated,
		"sessions_kicked", kick,
	)

	// Output to admin.
	if generated {
		writeOutputf(ctx, exec, "resetpassword",
			"Password for '%s' has been reset.\nNew password: %s\nThis password will not be shown again.\n",
			username, password)
	} else {
		writeOutputf(ctx, exec, "resetpassword",
			"Password for '%s' has been reset.\n", username)
	}

	return nil
}

// kickGameSessions terminates all active game sessions for a player's characters.
func kickGameSessions(ctx context.Context, exec *command.CommandExecution, deps AdminDeps, playerID ulid.ULID) {
	chars, err := deps.CharLister.ListByPlayer(ctx, playerID)
	if err != nil {
		slog.WarnContext(ctx, "failed to list characters for kick",
			"target_player_id", playerID.String(), "error", err)
		return
	}
	for _, ch := range chars {
		if _, delErr := exec.Services().Session().DeleteByCharacter(ctx, ch.ID, "admin password reset"); delErr != nil {
			slog.WarnContext(ctx, "failed to kick game session",
				"character_id", ch.ID.String(), "character_name", ch.Name, "error", delErr)
		}
	}
}

// parseResetArgs parses "username [password] [--kick]" from the command args.
// The --kick flag can appear anywhere in the args.
func parseResetArgs(args string) (username, password string, kick bool, err error) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		return "", "", false, command.ErrInvalidArgs("resetpassword",
			"resetpassword <player> [password] [--kick]")
	}

	// Scan for --kick flag and collect non-flag args.
	var positional []string
	for _, p := range parts {
		if p == "--kick" {
			kick = true
		} else {
			positional = append(positional, p)
		}
	}

	if len(positional) == 0 {
		return "", "", false, command.ErrInvalidArgs("resetpassword",
			"resetpassword <player> [password] [--kick]")
	}

	username = positional[0]
	if len(positional) > 1 {
		password = positional[1]
	}

	return username, password, kick, nil
}

// generatePassword creates a random alphanumeric password using crypto/rand.
func generatePassword() (string, error) {
	charset := resetPasswordCharset
	b := make([]byte, resetPasswordLength)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", oops.Wrap(err)
		}
		b[i] = charset[n.Int64()]
	}
	return string(b), nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `task build`
Expected: compiles without errors

- [ ] **Step 3: Commit**

```text
feat(command): add resetpassword handler with closure-injected deps
```

---

### Task 3: Unit tests for the resetpassword handler

**Files:**

- Create: `internal/command/handlers/resetpassword_test.go`

- [ ] **Step 1: Generate mocks if needed**

Check if mocks exist for `auth.PlayerRepository`, `auth.PasswordHasher`, `auth.WebSessionRepository`, `auth.PasswordResetRepository`. If not, run:

```bash
mockery
```

- [ ] **Step 2: Write the unit test file**

The test needs to construct a `CommandExecution` with mocked `Services` (for `Engine()` and `Session()`). Look at how `internal/command/handlers/shutdown_test.go` and other handler tests set up `CommandExecution` — use the same pattern.

Key test cases (table-driven):

| Test name | Args | Setup | Expected |
| --- | --- | --- | --- |
| `generated password reset` | `"alice"` | Player found, hash succeeds, update succeeds | Output contains 16-char password, `UpdatePassword` called |
| `explicit password reset` | `"alice securepass1"` | `admin:password.set` allowed | Provided password hashed, no generated password in output |
| `reset with kick` | `"alice --kick"` | `admin:session.kick` allowed, 2 characters | `DeleteByCharacter` called twice |
| `explicit and kick` | `"alice pass123x --kick"` | Both elevated caps allowed | All three operations performed |
| `explicit without capability` | `"alice securepass1"` | `admin:password.set` denied | `CodePermissionDenied` returned |
| `kick without capability` | `"alice --kick"` | `admin:session.kick` denied | `CodePermissionDenied` returned |
| `player not found` | `"nobody"` | `GetByUsername` returns error | `CodeTargetNotFound` returned |
| `password too short` | `"alice short"` | N/A | `CodeInvalidArgs` returned |
| `no args` | `""` | N/A | `CodeInvalidArgs` returned |
| `hash failure` | `"alice"` | `Hash` returns error | `CodeResetPasswordFailed` returned |
| `update failure` | `"alice"` | `UpdatePassword` returns error | `CodeResetPasswordFailed` returned |
| `web session delete failure` | `"alice"` | `DeleteByPlayer` returns error | Command still succeeds |
| `game session delete failure` | `"alice --kick"` | `DeleteByCharacter` returns error | Command still succeeds |
| `reset token cleanup failure` | `"alice"` | `ResetRepo.DeleteByPlayer` returns error | Command still succeeds |
| `lockout cleared on reset` | `"alice"` | Player has `FailedAttempts > 0`, `LockedUntil` set | `RecordSuccess` called, `Update` called |
| `lockout clear failure` | `"alice"` | `Update` returns error after `RecordSuccess` | Command still succeeds (best-effort) |
| `self-reset warns but allows` | `"adminuser"` | Admin resets own player account | Succeeds with warning in output |

For mock setup patterns, reference existing handler tests in the codebase. Use `command.NewTestExecution` or the equivalent test helper to create `CommandExecution` instances with mocked services.

- [ ] **Step 3: Run all unit tests**

Run: `task test -- -run TestResetPassword -v ./internal/command/handlers/...`
Expected: all tests PASS

- [ ] **Step 4: Commit**

```text
test(command): add unit tests for resetpassword handler
```

---

### Task 4: Register the command and wire dependencies

**Files:**

- Modify: `internal/command/handlers/register.go`
- Modify: `cmd/holomush/core.go`

- [ ] **Step 1: Add `RegisterAdmin` function to `register.go`**

Add a new function that accepts auth dependencies and registers closure-injected handlers. The `AdminDeps` struct is defined in `resetpassword.go` (created in Task 2), so just import and reference it here:

```go
// RegisterAdmin registers admin command handlers that require auth dependencies.
// These handlers use closure injection rather than extending command.Services.
func RegisterAdmin(reg *command.Registry, deps AdminDeps) {
	mustRegister := func(cfg command.CommandEntryConfig) {
		entry, err := command.NewCommandEntry(cfg)
		if err != nil {
			panic("failed to create admin command " + cfg.Name + ": " + err.Error())
		}
		if err := reg.Register(*entry); err != nil {
			panic("failed to register admin command " + cfg.Name + ": " + err.Error())
		}
	}

	mustRegister(command.CommandEntryConfig{
		Name:    "resetpassword",
		Handler: NewResetPasswordHandler(deps),
		Capabilities: []string{"admin:password.reset"},
		Help:         "Reset a player's password",
		Usage:        "resetpassword <player> [password] [--kick]",
		HelpText: `## Reset Password

Reset a player's password. Generates a random password if none provided.

### Usage

- ` + "`resetpassword <player>`" + ` - Generate a new random password
- ` + "`resetpassword <player> <password>`" + ` - Set a specific password (requires admin:password.set)
- ` + "`resetpassword <player> --kick`" + ` - Reset and disconnect active sessions (requires admin:session.kick)

### Capabilities

- ` + "`admin:password.reset`" + ` - Required for all resets
- ` + "`admin:password.set`" + ` - Required to provide an explicit password
- ` + "`admin:session.kick`" + ` - Required to force-disconnect active sessions`,
		Source: "core",
	})
}
```

Add imports for `auth` package at the top of `register.go`.

- [ ] **Step 2: Hoist variables in `core.go`**

In `cmd/holomush/core.go`, the variables `authSessionRepo`, `authResetRepo`, and `authHasher` are declared with `:=` inside the `if hasPool` block (lines 363-366) but need to be accessible at the command registration point (line 554). Hoist them alongside the existing hoisted auth variables (around line 297-304):

```go
var authSessionRepo *authpostgres.WebSessionRepository
var authResetRepo   *authpostgres.PasswordResetRepository
var authHasher      auth.PasswordHasher
```

Then change lines 363-366 from `:=` to `=`:

```go
authSessionRepo = authpostgres.NewWebSessionRepository(abacPool)
authResetRepo = authpostgres.NewPasswordResetRepository(abacPool)
authHasher = auth.NewArgon2idHasher()
```

- [ ] **Step 3: Wire `RegisterAdmin` in `core.go`**

After the existing `handlers.RegisterAll(cmdRegistry)` call (around line 554), add:

```go
handlers.RegisterAdmin(cmdRegistry, handlers.AdminDeps{
	PlayerRepo:  authPlayerRepo,
	Hasher:      authHasher,
	WebSessions: authSessionRepo,
	ResetRepo:   authResetRepo,
	CharLister:  authCharRepo,
})
```

Note: `authCharRepo` is the `authCharRepoAdapter` created at line 380. Verify it has a `ListByPlayer` method that satisfies `CharacterLister`. If it doesn't, add a delegating method.

- [ ] **Step 4: Verify compilation**

Run: `task build`
Expected: compiles without errors

- [ ] **Step 5: Run full test suite**

Run: `task test`
Expected: all tests PASS

- [ ] **Step 6: Run lint**

Run: `task lint`
Expected: no new warnings

- [ ] **Step 7: Commit**

```text
feat(command): register resetpassword and wire auth deps
```

---

## Chunk 2: Integration Tests

### Task 5: Integration tests with real PostgreSQL

**Files:**

- Create: `test/integration/command/resetpassword_integration_test.go`

- [ ] **Step 1: Write integration test file**

Use Ginkgo/Gomega with testcontainers (PostgreSQL). Follow the patterns in `test/integration/` for container setup. BDD-style feature specs with `//go:build integration` tag.

Key test cases:

| Test | Validates |
| --- | --- |
| Full reset flow | Create player with known password, call handler, verify `players.password_hash` changed, old password fails `Verify`, new password passes `Verify` |
| Web session invalidation | Create player + web session rows, reset password, verify `web_sessions` rows deleted for that player |
| ABAC deny for non-admin | Create non-admin character, attempt `resetpassword`, verify `CodePermissionDenied` returned |
| ABAC allow for admin | Create admin character with admin role, attempt `resetpassword`, verify success |
| Lockout cleared on reset | Create player with `FailedAttempts > 0` and `LockedUntil` set, reset password, verify both cleared |

- [ ] **Step 2: Run integration tests**

Run: `task test:int`
Expected: all integration tests PASS

- [ ] **Step 3: Commit**

```text
test(command): add resetpassword integration tests
```

---

## Chunk 3: E2E Tests with DB Validation

### Task 6: Configure E2E admin credentials

**Files:**

- Modify: `compose.e2e.yaml`

- [ ] **Step 1: Set admin credentials in compose.e2e.yaml**

Add environment variables to the `core` service so the admin bootstrapper creates a predictable admin account for E2E tests:

```yaml
core:
  environment:
    DATABASE_URL: postgres://holomush:holomush@postgres:5432/holomush_test?sslmode=disable
    HOLOMUSH_ADMIN_USERNAME: e2e_admin
    HOLOMUSH_ADMIN_PASSWORD: e2e_admin_pass123
```

This ensures the first-boot admin bootstrap creates an account with known credentials that E2E tests can log into.

- [ ] **Step 2: Commit**

```text
feat(e2e): set admin credentials for E2E testing
```

---

### Task 7: Add E2E DB helpers

**Files:**

- Modify: `web/e2e/helpers/db.ts`

- [ ] **Step 1: Add `getPlayerPasswordHash` and `getWebSessionsByPlayerId`**

```typescript
// ── Password hash queries ──────────────────────────────────────

export async function getPlayerPasswordHash(playerId: string): Promise<string | null> {
  const { rows } = await getPool().query<{ password_hash: string }>(
    'SELECT password_hash FROM players WHERE id = $1',
    [playerId],
  );
  return rows[0]?.password_hash ?? null;
}

// ── Web session queries ────────────────────────────────────────

export interface DbWebSession {
  id: string;
  player_id: string;
  expires_at: Date;
}

export async function getWebSessionsByPlayerId(playerId: string): Promise<DbWebSession[]> {
  const { rows } = await getPool().query<DbWebSession>(
    'SELECT id, player_id, expires_at FROM web_sessions WHERE player_id = $1',
    [playerId],
  );
  return rows;
}
```

- [ ] **Step 2: Commit**

```text
feat(e2e): add password hash and web session DB helpers
```

---

### Task 8: Write E2E tests for admin password reset

**Files:**

- Create: `web/e2e/admin.spec.ts`

- [ ] **Step 1: Write the E2E test file**

The test needs to:

1. Log in as the admin account (`e2e_admin` / `e2e_admin_pass123`)
2. Register a target player to reset
3. Send `resetpassword <target>` in the terminal
4. Verify the generated password appears in terminal output
5. DB: verify `players.password_hash` changed
6. DB: verify `web_sessions` for target are deleted

Additional test cases:

- Non-admin sends `resetpassword` → receives permission denied in output
- Full round-trip: admin resets password, target logs in with new password

**Important considerations:**

- The admin character name is generated by the star-name theme during bootstrap, so you need to look it up. Use the admin player's username (`e2e_admin`) to find the player, then find their character.
- The admin login flow uses the same Register/Login pages as regular users. Log in with credentials `e2e_admin` / `e2e_admin_pass123`.
- After sending `resetpassword`, the output event should contain the generated password. Use `page.locator('[data-testid="event"]')` to capture it.
- For the non-admin test, use a freshly registered non-admin player.

Reference the existing E2E test patterns in `auth.spec.ts` and `terminal.spec.ts` for page interaction patterns (form fills, URL waits, terminal input).

- [ ] **Step 2: Run E2E tests**

Run: `task test:e2e`
Expected: all tests PASS (existing 41 + new admin tests)

- [ ] **Step 3: Commit**

```text
feat(e2e): add admin password reset E2E tests with DB validation
```

---

## Chunk 4: Documentation + Cleanup

### Task 9: Update access control reference docs

**Files:**

- Modify: `site/docs/reference/access-control.md`

- [ ] **Step 1: Add new capabilities to the reference table**

Find the capabilities table in `access-control.md` and add:

```markdown
| `admin:password.reset` | admin | `resetpassword` |
| `admin:password.set`   | admin | `resetpassword` (explicit password) |
| `admin:session.kick`   | admin | `resetpassword --kick` |
```

- [ ] **Step 2: Run docs lint**

Run: `task lint:markdown`
Expected: no errors

- [ ] **Step 3: Commit**

```text
docs: add admin password reset capabilities to reference
```

---

### Task 10: Create follow-up issue for control plane RPC

- [ ] **Step 1: Create beads issue**

```bash
bd create "Control plane RPC: AdminResetPassword" \
  --description="Add AdminResetPassword RPC to the Control service for CLI/API-driven password resets without being in-game. Deferred from holomush-us6j (admin password reset command) to avoid expanding the control plane scope in this iteration." \
  -t feature -p 3 --deps discovered-from:holomush-us6j
```

- [ ] **Step 2: Close the original issue**

```bash
bd close holomush-us6j --reason "resetpassword command implemented with tiered ABAC, E2E tests, and DB validation"
```

- [ ] **Step 3: Final commit with all changes**

Run the full pre-PR gate:

```bash
task test && task lint && task test:int && task test:e2e
```

---

## Post-Implementation Checklist

- [ ] All unit tests pass (`task test`)
- [ ] All linters pass (`task lint`)
- [ ] All integration tests pass (`task test:int`)
- [ ] All E2E tests pass (`task test:e2e`)
- [ ] Error code `RESET_PASSWORD_FAILED` added and handled in `PlayerMessage`
- [ ] Handler uses `crypto/rand` (not `math/rand`) for password generation
- [ ] Passwords never appear in structured log fields
- [ ] Best-effort session cleanup matches existing pattern
- [ ] Reset tokens invalidated after admin reset
- [ ] Account lockout cleared (FailedAttempts + LockedUntil) after reset
- [ ] Integration tests pass with real PostgreSQL (`task test:int`)
- [ ] ABAC capabilities documented in access-control reference
- [ ] Follow-up issue created for control plane RPC
- [ ] Original issue closed
