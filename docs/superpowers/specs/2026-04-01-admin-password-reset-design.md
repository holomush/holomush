<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Admin Password Reset Command — Design Spec

**Date:** 2026-04-01
**Issue:** holomush-us6j
**Status:** Approved

## Problem

Admins need to reset a player's password without email. The only existing
reset flow is token-based (`RequestPasswordReset` → email →
`ConfirmPasswordReset`). When email isn't configured, operators must update
the hash directly in PostgreSQL or recreate the account — unacceptable for
production use.

## Solution

An in-game `resetpassword` command with tiered ABAC capabilities:

- Base capability generates a random password (safe for helpdesk staff)
- Elevated capability allows providing an explicit password
- Optional `--kick` flag terminates active game sessions

A control plane RPC (`AdminResetPassword`) is deferred to a follow-up issue
when the control plane expands to include player management.

## Command Syntax

```text
resetpassword <player-username> [newpassword] [--kick]
```

| Form | Capabilities Required | Behavior |
|---|---|---|
| `resetpassword alice` | `admin:password.reset` | Generate 16-char random password, display to admin |
| `resetpassword alice hunter42` | `admin:password.reset` + `admin:password.set` | Use provided password (min 8 chars) |
| `resetpassword alice --kick` | `admin:password.reset` + `admin:session.kick` | Generate password + terminate game sessions |
| `resetpassword alice hunter42 --kick` | All three | Explicit password + kick |

## ABAC Capabilities

| Capability | Grants | Default Role |
|---|---|---|
| `admin:password.reset` | Reset with system-generated password | `admin` |
| `admin:password.set` | Provide explicit password | `admin` |
| `admin:session.kick` | Force-disconnect active game sessions | `admin` |

All three MUST be granted to the `admin` role by default. An operator MAY
create a restricted role (e.g., "helpdesk") with only `admin:password.reset`
for lower-privilege staff.

The dispatcher automatically checks `admin:password.reset` (listed in
`CommandEntryConfig.Capabilities`). The handler checks `admin:password.set`
and `admin:session.kick` conditionally — only when those features are invoked.

## Flow

1. Dispatcher checks `admin:password.reset` capability (automatic)
2. Handler parses args: extract username, optional password, `--kick` flag
3. If explicit password provided, check `admin:password.set` via
   `CheckCapability`
4. If `--kick` flag present, check `admin:session.kick` via `CheckCapability`
5. Look up target player by username via `PlayerRepository.GetByUsername`
6. Generate random password (if none provided) or validate length (min 8)
7. Hash password with `Argon2idHasher`
8. Call `PlayerRepository.UpdatePassword`
9. Call `PasswordResetRepository.DeleteByPlayer` (best-effort — invalidate
   outstanding email-based reset tokens)
10. Call `WebSessionRepository.DeleteByPlayer` (always — invalidate auth
    sessions)
11. If `--kick`: look up player's characters via
    `CharacterRepository.ListByPlayer`, then call
    `exec.Services().Session().DeleteByCharacter` for each (best-effort)
12. Clear account lockout state: set `FailedAttempts` to 0, clear
    `LockedUntil` (best-effort)
13. Log the action via `slog.Info` with admin ID, target player, whether
    kicked
14. Output result to admin (including generated password if applicable)

### Password Generation

Generate 16 characters from `crypto/rand` using the alphanumeric charset
`a-zA-Z0-9`. The generated password MUST be displayed to the admin exactly
once in the command output. It is NOT stored in plaintext anywhere.

## Dependency Injection

The handler needs `PlayerRepository`, `PasswordHasher`,
`WebSessionRepository`, `PasswordResetRepository`, and a character lister.
These are NOT available through `command.Services` and SHOULD NOT be added
there — they are auth-specific concerns that most commands don't need.

`session.Access` and `AccessPolicyEngine` ARE available via
`exec.Services().Session()` and `exec.Services().Engine()` respectively, so
they do NOT need injection.

**Approach:** Closure injection. A factory function constructs the handler:

```go
func NewResetPasswordHandler(
    playerRepo auth.PlayerRepository,
    hasher auth.PasswordHasher,
    webSessions auth.WebSessionRepository,
    resetRepo auth.PasswordResetRepository,
    charLister CharacterLister,
) command.CommandHandler
```

`CharacterLister` is a narrow interface defined locally in the handler file:

```go
type CharacterLister interface {
    ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*world.Character, error)
}
```

This follows ISP — the handler only needs `ListByPlayer`, not the full
`CharacterRepository`. Both `auth.CharacterRepository` and
`world.CharacterRepository` can satisfy it if they have this method.

The returned closure captures these dependencies. The handler is registered at
startup in `internal/command/handlers/register.go` (or a new admin-specific
registration function that receives the dependencies).

## Error Handling

| Condition | Error Code | Message |
|---|---|---|
| No arguments | `CodeInvalidArgs` | "Usage: resetpassword \<player\> [password] [--kick]" |
| Player not found | `CodeTargetNotFound` | "Player '\<username\>' not found" |
| Password too short | `CodeInvalidArgs` | "Password must be at least 8 characters" |
| Missing `admin:password.set` | `CodePermissionDenied` | "Setting explicit passwords requires admin:password.set capability" |
| Missing `admin:session.kick` | `CodePermissionDenied` | "Kicking sessions requires admin:session.kick capability" |
| Hash failure | `RESET_PASSWORD_FAILED` | "Password reset failed" |
| UpdatePassword failure | `RESET_PASSWORD_FAILED` | "Password reset failed" |
| Web session delete failure | Best-effort: log warning, command still succeeds |  |
| Game session delete failure | Best-effort: log warning, command still succeeds |  |

## Audit Logging

The ABAC policy engine automatically logs access decisions for all capability
checks (the audit logger in `internal/access/policy/audit/`). The handler
MUST additionally log:

```go
slog.Info("admin password reset",
    "event", "admin_password_reset",
    "admin_player_id", exec.PlayerID().String(),
    "admin_character_id", exec.CharacterID().String(),
    "admin_character_name", exec.CharacterName(),
    "target_player_id", player.ID.String(),
    "target_username", player.Username,
    "password_generated", !explicitPassword,
    "sessions_kicked", kickFlag,
)
```

## Files to Create/Modify

| File | Change |
|---|---|
| `internal/command/handlers/resetpassword.go` | New: handler + factory |
| `internal/command/handlers/resetpassword_test.go` | New: unit tests |
| `internal/command/handlers/register.go` | Modify: register command (may need new registration pattern for injected deps) |
| ABAC seed/policy | Add 3 capabilities to admin role |
| `test/integration/command/resetpassword_integration_test.go` | New: integration tests |
| `web/e2e/helpers/db.ts` | Add: `getPlayerPasswordHash`, `getWebSessionsByPlayerId` |
| `web/e2e/terminal.spec.ts` | Add: E2E tests with DB validation |

## Testing

### Unit Tests (`internal/command/handlers/resetpassword_test.go`)

All unit tests use mocked dependencies (mockery-generated mocks for
`PlayerRepository`, `PasswordHasher`, `WebSessionRepository`,
`CharacterRepository`, `session.Access`, `AccessPolicyEngine`).

| Test | Validates |
|---|---|
| Generated password reset | Player found, password hashed, `UpdatePassword` called, web sessions deleted, output contains generated password (16 alphanumeric chars) |
| Explicit password reset | `admin:password.set` capability checked and allowed, provided password used for hashing |
| Reset with `--kick` | `admin:session.kick` checked, characters looked up, `DeleteByCharacter` called per character |
| Combined: explicit + kick | All three capabilities checked, both operations performed |
| Explicit password without capability | `CheckCapability` returns deny, handler returns `CodePermissionDenied` |
| `--kick` without capability | `CheckCapability` returns deny, handler returns `CodePermissionDenied` |
| Player not found | `GetByUsername` returns not found, handler returns `CodeTargetNotFound` |
| Explicit password too short (< 8) | Returns `CodeInvalidArgs` |
| No arguments | Returns `CodeInvalidArgs` with usage message |
| Hash failure | `Hasher.Hash` returns error, handler returns `CodeWorldError` |
| UpdatePassword failure | Repo returns error, handler returns `CodeWorldError` |
| Web session delete failure | Logs warning, command still succeeds |
| Game session delete failure (--kick) | Logs warning, command still succeeds |
| Admin cannot reset own player account | Handler SHOULD warn but allow (not a hard block) |
| Reset clears account lockout | `FailedAttempts` set to 0, `LockedUntil` cleared |
| Outstanding reset tokens invalidated | `PasswordResetRepository.DeleteByPlayer` called |

### Integration Tests (`test/integration/command/`)

Use Ginkgo/Gomega with testcontainers (PostgreSQL). BDD-style feature specs.

| Test | Validates |
|---|---|
| Full reset flow | Create player with known password, reset, verify `players.password_hash` changed, old password fails `Verify`, new password passes `Verify` |
| Web session invalidation | Create player + web session rows, reset password, verify `web_sessions` rows deleted for that player |
| ABAC deny for non-admin | Create non-admin character, attempt `resetpassword`, verify `CodePermissionDenied` returned |
| ABAC allow for admin | Create admin character, attempt `resetpassword`, verify success |

### E2E Tests (`web/e2e/terminal.spec.ts`)

Requires an admin-capable character in the E2E environment. **Fixture
requirement:** the E2E test setup MUST provide a way to create or
pre-configure an admin character. Options:

1. **Bootstrap seed**: add an admin player/character created during DB
   bootstrap (e.g., `admin`/`admin123` with admin role)
2. **Test helper**: a DB helper that grants admin role to a character

Option 1 is recommended — a known admin account in the test DB simplifies
E2E setup and matches how operators bootstrap production.

#### DB Helpers Needed (`web/e2e/helpers/db.ts`)

```typescript
getPlayerPasswordHash(playerId: string): Promise<string | null>
getWebSessionsByPlayerId(playerId: string): Promise<DbWebSession[]>
```

#### E2E Test Cases

| Test | Validates |
|---|---|
| Admin resets player password (generated) | Admin logs in, sends `resetpassword <player>`, output contains generated password. DB: `players.password_hash` differs from before. DB: `web_sessions` for target deleted. |
| Admin resets with explicit password | Admin sends `resetpassword <player> newpass123`. DB: `players.password_hash` changed. |
| Admin resets with `--kick` | Admin sends `resetpassword <player> --kick`. DB: target's `sessions` row deleted. |
| Non-admin denied | Non-admin registered user sends `resetpassword <player>`, receives permission denied in terminal output. |
| Full round-trip: reset then login | Admin resets player's password. Player logs out, logs back in with new password. DB: new session created, player authenticated. |

## Argument Parsing

The handler scans all args for the `--kick` flag (position-independent).
Remaining non-flag args are treated positionally as `[username] [password]`.
This means `resetpassword alice --kick` and `resetpassword alice --kick` are
equivalent, and `resetpassword alice --kick hunter42` treats `hunter42` as the
password.

## Concurrency

Concurrent resets for the same player are last-writer-wins. Both admins would
see a generated/provided password, but only the last `UpdatePassword` call's
hash persists. This is acceptable for an admin tool — concurrent resets for
the same player are rare and self-correcting (the admin whose password didn't
stick will notice when the player reports it doesn't work).

## Non-Goals

- Control plane RPC — deferred to follow-up issue
- "Force change on next login" flag — future enhancement
- Email notification to the player — requires email infrastructure
- Password complexity rules beyond min length — YAGNI for now

## Security Considerations

- Generated passwords use `crypto/rand`, never `math/rand`
- Passwords MUST NOT appear in structured log fields — only in the admin's
  command output
- The `admin:password.set` capability gate prevents lower-privilege admins
  from setting weak passwords
- Best-effort session invalidation: if `DeleteByPlayer` fails, the password
  is still changed — the player's existing web sessions will expire naturally
  (default TTL). This matches the existing email-based reset pattern in
  `PasswordResetService.ResetPassword` and is acceptable because the window
  is bounded by session TTL
- Outstanding email-based reset tokens are invalidated to prevent a player
  from using a previously-requested token to override the admin-set password
- Admin commands are subject to the existing command rate limiter — no
  additional throttling is needed
