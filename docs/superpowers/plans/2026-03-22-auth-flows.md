# Auth Flows Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement two-phase login (authenticate → select character) shared between web and telnet, with site-wide top bar, httpOnly cookies, registration, and password reset.

**Architecture:** New auth RPCs on CoreService (shared by both gateways). Web gateway adds cookie middleware wrapping core RPCs. SvelteKit auth pages under layout groups with auth guards. Existing auth domain layer (`internal/auth/`) provides all business logic — this plan wires it into the server.

**Tech Stack:** Go (gRPC, ConnectRPC), Protocol Buffers, SvelteKit 2, TypeScript, Playwright, PostgreSQL

**Spec:** `docs/specs/2026-03-22-auth-flows-design.md`

---

## File Structure

### New Files

| File | Responsibility |
| ---- | -------------- |
| `internal/grpc/auth_handlers.go` | Core server auth RPC handlers (AuthenticatePlayer, SelectCharacter, CreatePlayer, etc.) |
| `internal/grpc/auth_handlers_test.go` | Unit tests for auth RPC handlers |
| `internal/auth/registration.go` | CreatePlayer service method + password validation |
| `internal/auth/registration_test.go` | Tests for registration |
| `internal/web/cookie.go` | Cookie middleware (set/clear/validate httpOnly cookies) |
| `internal/web/cookie_test.go` | Cookie middleware tests |
| `internal/web/auth_handlers.go` | Web gateway auth proxy handlers (Web-prefixed RPCs) |
| `internal/web/auth_handlers_test.go` | Tests for web auth handlers |
| `web/src/lib/stores/authStore.ts` | Client-side auth state (player token, session, character) |
| `web/src/lib/components/TopBar.svelte` | Thin top bar component (3 auth states) |
| `web/src/routes/+layout.svelte` | Root layout with top bar |
| `web/src/routes/login/+page.svelte` | Login form |
| `web/src/routes/register/+page.svelte` | Registration form |
| `web/src/routes/reset/+page.svelte` | Password reset request |
| `web/src/routes/reset/confirm/+page.svelte` | Password reset confirmation |
| `web/src/routes/(authed)/+layout.ts` | Auth guard (redirects to /login if no session) |
| `web/src/routes/(authed)/characters/+page.svelte` | Character select/create |
| `web/tests/auth.spec.ts` | Playwright E2E for auth flows |

### Modified Files

| File | Changes |
| ---- | ------- |
| `api/proto/holomush/core/v1/core.proto` | Add 8 new RPCs + messages (AuthenticatePlayer, SelectCharacter, CreatePlayer, CreateCharacter, ListCharacters, RequestPasswordReset, ConfirmPasswordReset, Logout) |
| `api/proto/holomush/web/v1/web.proto` | Remove 4 stub RPCs, add 8 Web-prefixed RPCs, update CharacterSummary |
| `internal/grpc/server.go` | Add auth service fields to CoreServer, new options |
| `internal/grpc/client.go` | Add client methods for new core RPCs |
| `internal/auth/reset_service.go` | Add WebSessionRepository dependency for session invalidation |
| `internal/web/handler.go` | Remove stub methods, add auth service deps to Handler |
| `internal/web/server.go` | Wire cookie middleware into HTTP stack |
| `cmd/holomush/core.go` | Wire auth repos + services into CoreServer at startup |
| `cmd/holomush/gateway.go` | Pass auth config to web handler |
| `internal/telnet/gateway_handler.go` | Two-phase connect flow, PLAY/CREATE commands |
| `web/src/routes/terminal/+page.svelte` | Move to `(authed)/terminal/`, remove inline login |
| `web/src/routes/+page.svelte` | Update landing page with auth links |
| `web/src/routes/+layout.ts` | Keep SSR=false, remove prerender |
| `web/src/lib/transport.ts` | Add cookie credentials to transport |
| `web/package.json` | Add lucide-svelte dependency |

---

## Chunk 1: Core Proto & Auth Service Extensions

### Task 1: Core Proto — New Auth RPCs and Messages

**Files:**

- Modify: `api/proto/holomush/core/v1/core.proto`

This task adds all new auth RPC definitions and messages to the core proto.
After editing, run `task generate` to regenerate Go code.

- [ ] **Step 1: Add CharacterSummary message and auth RPCs to core.proto**

Add after the `GetCommandHistory` RPC in the `CoreService` block (after line 39):

```protobuf
  // Two-phase login: authenticate player credentials.
  rpc AuthenticatePlayer(AuthenticatePlayerRequest) returns (AuthenticatePlayerResponse);

  // Two-phase login: select a character, creating or reattaching a game session.
  rpc SelectCharacter(SelectCharacterRequest) returns (SelectCharacterResponse);

  // Create a new player account.
  rpc CreatePlayer(CreatePlayerRequest) returns (CreatePlayerResponse);

  // Create a new character for an authenticated player.
  rpc CreateCharacter(CreateCharacterRequest) returns (CreateCharacterResponse);

  // List characters for an authenticated player.
  rpc ListCharacters(ListCharactersRequest) returns (ListCharactersResponse);

  // Request a password reset (email stubbed).
  rpc RequestPasswordReset(RequestPasswordResetRequest) returns (RequestPasswordResetResponse);

  // Confirm a password reset with token.
  rpc ConfirmPasswordReset(ConfirmPasswordResetRequest) returns (ConfirmPasswordResetResponse);

  // End a web session.
  rpc Logout(LogoutRequest) returns (LogoutResponse);
```

Add the messages after the existing `GetCommandHistoryResponse` (after line 128):

```protobuf
// --- Auth messages ---

message CharacterSummary {
  string character_id = 1;
  string character_name = 2;
  bool has_active_session = 3;
  string session_status = 4;
  string last_location = 5;
  int64 last_played_at = 6;
}

message AuthenticatePlayerRequest {
  string username = 1;
  string password = 2;
  string captcha_token = 3;
  bool remember_me = 4;
}

message AuthenticatePlayerResponse {
  bool success = 1;
  string player_token = 2;
  string error_message = 3;
  repeated CharacterSummary characters = 4;
  string default_character_id = 5;
}

message SelectCharacterRequest {
  string player_token = 1;
  string character_id = 2;
}

message SelectCharacterResponse {
  bool success = 1;
  string session_id = 2;
  string character_name = 3;
  bool reattached = 4;
  string error_message = 5;
}

message CreatePlayerRequest {
  string username = 1;
  string password = 2;
  string email = 3;
  string captcha_token = 4;
}

message CreatePlayerResponse {
  bool success = 1;
  string player_token = 2;
  repeated CharacterSummary characters = 3;
  string error_message = 4;
}

message CreateCharacterRequest {
  string player_token = 1;
  string character_name = 2;
}

message CreateCharacterResponse {
  bool success = 1;
  string character_id = 2;
  string character_name = 3;
  string error_message = 4;
}

message ListCharactersRequest {
  string player_token = 1;
}

message ListCharactersResponse {
  repeated CharacterSummary characters = 1;
}

message RequestPasswordResetRequest {
  string email = 1;
}

message RequestPasswordResetResponse {
  bool success = 1;
}

message ConfirmPasswordResetRequest {
  string token = 1;
  string new_password = 2;
}

message ConfirmPasswordResetResponse {
  bool success = 1;
  string error_message = 2;
}

message LogoutRequest {
  string session_id = 1;
}

message LogoutResponse {}
```

- [ ] **Step 2: Regenerate Go code**

Run: `task generate`
Expected: Clean generation, new Go types in `pkg/proto/holomush/core/v1/`

- [ ] **Step 3: Verify compilation**

Run: `task build`
Expected: PASS (new proto types exist but aren't used yet)

- [ ] **Step 4: Commit**

```bash
git add api/proto/holomush/core/v1/core.proto pkg/proto/holomush/core/v1/
git commit -m "feat(proto): add auth RPCs to CoreService

AuthenticatePlayer, SelectCharacter, CreatePlayer, CreateCharacter,
ListCharacters, RequestPasswordReset, ConfirmPasswordReset, Logout.
Shared between web and telnet gateways."
```

---

### Task 2: Auth Service Extensions — Registration, Password Validation, Reset Invalidation

**Files:**

- Create: `internal/auth/registration.go`
- Create: `internal/auth/registration_test.go`
- Modify: `internal/auth/reset_service.go`
- Modify: `internal/auth/reset_service_test.go`

This task adds four pieces to the existing auth domain layer:

1. `ValidateCredentials` method on `auth.Service` — validates username/password without creating a session (for two-phase login phase 1)
2. `CreatePlayer` method on `auth.Service` — hashes password, creates player, returns player token
3. Password validation (minimum 8 characters)
4. Session invalidation when password is reset
5. `ListByPlayer` method on `auth.CharacterRepository` interface + mock regeneration

- [ ] **Step 1: Write failing test for password validation**

Create `internal/auth/registration_test.go`:

```go
//go:build !integration

package auth

import "testing"

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"valid 8 chars", "abcd1234", false},
		{"valid long", "a-very-secure-password-123", false},
		{"too short", "abc1234", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.password)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePassword(%q) error = %v, wantErr %v", tt.password, err, tt.wantErr)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestValidatePassword -v`
Expected: FAIL — `ValidatePassword` not defined

- [ ] **Step 3: Implement ValidatePassword and CreatePlayer**

Create `internal/auth/registration.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"context"
	"errors"
	"time"

	"github.com/samber/oops"
)

const (
	// MinPasswordLength is the minimum allowed password length.
	MinPasswordLength = 8

	// PlayerTokenTTL is the lifetime of a player token for two-phase login.
	PlayerTokenTTL = 5 * time.Minute
)

// ValidatePassword checks that a password meets minimum requirements.
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLength {
		return oops.Code("PASSWORD_TOO_SHORT").
			With("min_length", MinPasswordLength).
			Errorf("password must be at least %d characters", MinPasswordLength)
	}
	return nil
}

// CreatePlayer registers a new player account.
// Returns the player and a plaintext player token for immediate two-phase login.
// Email is optional (empty string is valid).
func (s *Service) CreatePlayer(ctx context.Context, username, password, email string) (*Player, *PlayerToken, error) {
	if err := ValidateUsername(username); err != nil {
		return nil, nil, oops.Code("REGISTER_INVALID_USERNAME").Wrap(err)
	}

	if err := ValidatePassword(password); err != nil {
		return nil, nil, oops.Code("REGISTER_INVALID_PASSWORD").Wrap(err)
	}

	// Check if username is already taken
	_, err := s.players.GetByUsername(ctx, username)
	if err == nil {
		return nil, nil, oops.Code("REGISTER_USERNAME_TAKEN").
			With("username", username).
			Errorf("username %q is already taken", username)
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, nil, oops.Code("REGISTER_FAILED").
			With("operation", "check username").Wrap(err)
	}

	// Hash the password
	hashedPassword, err := s.hasher.Hash(password)
	if err != nil {
		return nil, nil, oops.Code("REGISTER_FAILED").
			With("operation", "hash password").Wrap(err)
	}

	// Create the player (NewPlayer takes username, *email, passwordHash)
	var emailPtr *string
	if email != "" {
		emailPtr = &email
	}
	player, err := NewPlayer(username, emailPtr, hashedPassword)
	if err != nil {
		return nil, nil, oops.Code("REGISTER_FAILED").
			With("operation", "create player").Wrap(err)
	}

	if err := s.players.Create(ctx, player); err != nil {
		return nil, nil, oops.Code("REGISTER_FAILED").
			With("operation", "persist player").Wrap(err)
	}

	// Generate a player token for immediate use
	token, err := NewPlayerToken(player.ID, PlayerTokenTTL)
	if err != nil {
		// Player was created but token generation failed.
		// Player can still log in normally.
		s.logger.Warn("player created but token generation failed",
			"player_id", player.ID.String(),
			"error", err.Error(),
		)
		return player, nil, nil
	}

	return player, token, nil
}
```

Note: `NewPlayer` and `ValidateUsername` already exist in `internal/auth/player.go`.

- [ ] **Step 4: Write failing test for CreatePlayer**

Add to `internal/auth/registration_test.go`:

```go
func TestService_CreatePlayer(t *testing.T) {
	// Use existing mock infrastructure from auth_service_test.go
	// Test cases:
	// - successful registration with email
	// - successful registration without email
	// - username already taken
	// - invalid username (too short)
	// - invalid password (too short)
	// Tests use MockPlayerRepository, MockWebSessionRepository, MockPasswordHasher
	// from internal/auth/mocks/
}
```

Follow the test pattern from `internal/auth/auth_service_test.go` — use
`mocks.NewMockPlayerRepository(t)` with `.EXPECT()` chains.

- [ ] **Step 5: Run tests to verify CreatePlayer works**

Run: `go test ./internal/auth/ -run "TestValidatePassword|TestService_CreatePlayer" -v`
Expected: PASS

- [ ] **Step 6: Add session invalidation to PasswordResetService**

Modify `internal/auth/reset_service.go`:

1. Add `sessions WebSessionRepository` field to `PasswordResetService` struct:

```go
type PasswordResetService struct {
	playerRepo PlayerRepository
	resetRepo  PasswordResetRepository
	sessions   WebSessionRepository  // NEW
	hasher     PasswordHasher
	logger     *slog.Logger
}
```

2. Update both constructor functions to accept a `WebSessionRepository`:

```go
func NewPasswordResetService(
    playerRepo PlayerRepository,
    resetRepo PasswordResetRepository,
    sessions WebSessionRepository,
    hasher PasswordHasher,
) (*PasswordResetService, error) {
```

```go
func NewPasswordResetServiceWithLogger(
    playerRepo PlayerRepository,
    resetRepo PasswordResetRepository,
    sessions WebSessionRepository,
    hasher PasswordHasher,
    logger *slog.Logger,
) (*PasswordResetService, error) {
```

In `ResetPassword`, after updating the password, call `s.sessions.DeleteByPlayer(ctx, playerID)`

The key change in `ResetPassword` (after `s.playerRepo.UpdatePassword`):

```go
// Invalidate all active sessions for the player.
// Password change means all sessions should be re-authenticated.
if err := s.sessions.DeleteByPlayer(ctx, playerID); err != nil {
    s.logger.Warn("best-effort session invalidation failed",
        "event", "session_invalidation_failed",
        "player_id", playerID.String(),
        "operation", "delete_sessions",
        "error", err.Error(),
    )
}
```

- [ ] **Step 7: Fix existing PasswordResetService tests**

The constructor now requires a `WebSessionRepository` — add a mock to all
existing test cases in `internal/auth/reset_service_test.go` and
`internal/auth/reset_service_logging_test.go`.

- [ ] **Step 8: Write test for session invalidation on reset**

Add a test case to `reset_service_test.go` that verifies
`DeleteByPlayer` is called after a successful `ResetPassword`.

- [ ] **Step 9: Run all auth tests**

Run: `go test ./internal/auth/... -v`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add internal/auth/registration.go internal/auth/registration_test.go \
       internal/auth/reset_service.go internal/auth/reset_service_test.go \
       internal/auth/reset_service_logging_test.go
git commit -m "feat(auth): add CreatePlayer, password validation, reset session invalidation

CreatePlayer registers accounts with username/password/optional-email.
ValidatePassword enforces 8-char minimum.
ResetPassword now invalidates all active web sessions."
```

---

### Task 3: Core Server Auth Handlers

**Files:**

- Create: `internal/grpc/auth_handlers.go`
- Create: `internal/grpc/auth_handlers_test.go`
- Modify: `internal/grpc/server.go` (add fields and options)
- Modify: `internal/grpc/client.go` (add client methods for new RPCs)

This task implements all 8 new auth RPCs on the CoreServer. Each handler
delegates to the existing auth services.

- [ ] **Step 1: Add auth service fields to CoreServer**

In `internal/grpc/server.go`, add to the `CoreServer` struct (after line 107):

```go
authService      *auth.Service
resetService     *auth.PasswordResetService
characterService *auth.CharacterService
playerTokenRepo  auth.PlayerTokenRepository
playerRepo       auth.PlayerRepository
```

Add corresponding `CoreServerOption` functions:

```go
func WithAuthService(svc *auth.Service) CoreServerOption {
    return func(s *CoreServer) { s.authService = svc }
}

func WithResetService(svc *auth.PasswordResetService) CoreServerOption {
    return func(s *CoreServer) { s.resetService = svc }
}

func WithCharacterService(svc *auth.CharacterService) CoreServerOption {
    return func(s *CoreServer) { s.characterService = svc }
}

func WithPlayerTokenRepo(repo auth.PlayerTokenRepository) CoreServerOption {
    return func(s *CoreServer) { s.playerTokenRepo = repo }
}

func WithPlayerRepo(repo auth.PlayerRepository) CoreServerOption {
    return func(s *CoreServer) { s.playerRepo = repo }
}
```

- [ ] **Step 2: Write failing test for AuthenticatePlayer handler**

Create `internal/grpc/auth_handlers_test.go` with a test using mock repos.
The handler should:

1. Call `authService.Login(ctx, username, password, "web", "")`
2. Create a PlayerToken
3. Store the token via playerTokenRepo
4. List characters for the player
5. Return player_token + character list

- [ ] **Step 3: Implement AuthenticatePlayer handler**

Create `internal/grpc/auth_handlers.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
    "context"

    "github.com/samber/oops"

    "github.com/holomush/holomush/internal/auth"
    corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

func (s *CoreServer) AuthenticatePlayer(ctx context.Context, req *corev1.AuthenticatePlayerRequest) (*corev1.AuthenticatePlayerResponse, error) {
    if s.authService == nil {
        return &corev1.AuthenticatePlayerResponse{
            Success:      false,
            ErrorMessage: "player authentication not configured",
        }, nil
    }

    // Validate credentials WITHOUT creating a full WebSession.
    // auth.Service.Login creates a session — we don't want that here.
    // Instead, validate credentials directly via the player repo + hasher.
    // A lightweight ValidateCredentials method should be added to auth.Service
    // that returns (*Player, error) without creating a session.
    player, err := s.authService.ValidateCredentials(ctx, req.Username, req.Password)
    if err != nil {
        return &corev1.AuthenticatePlayerResponse{
            Success:      false,
            ErrorMessage: "invalid username or password",
        }, nil
    }

    // Create a short-lived player token for two-phase login
    playerToken, err := auth.NewPlayerToken(player.ID, auth.PlayerTokenTTL)
    if err != nil {
        return &corev1.AuthenticatePlayerResponse{
            Success:      false,
            ErrorMessage: "internal error",
        }, nil
    }

    if err := s.playerTokenRepo.Create(ctx, playerToken); err != nil {
        return &corev1.AuthenticatePlayerResponse{
            Success:      false,
            ErrorMessage: "internal error",
        }, nil
    }

    // List characters for this player
    characters, err := s.listCharactersForPlayer(ctx, player.ID)
    if err != nil {
        // Auth succeeded but char listing failed — return token anyway
        return &corev1.AuthenticatePlayerResponse{
            Success:     true,
            PlayerToken: playerToken.Token,
        }, nil
    }

    // Get default character ID
    var defaultCharID string
    if player.DefaultCharacterID != nil {
        defaultCharID = player.DefaultCharacterID.String()
    }

    return &corev1.AuthenticatePlayerResponse{
        Success:            true,
        PlayerToken:        playerToken.Token,
        Characters:         characters,
        DefaultCharacterId: defaultCharID,
    }, nil
}
```

The helper `listCharactersForPlayer` needs a `ListByPlayer(ctx, playerID)`
query. This method does not exist on any current repository. Add it to
`auth.CharacterRepository`:

```go
// In internal/auth/character_service.go, add to CharacterRepository:
ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*world.Character, error)
```

Implement in the `authCharRepoAdapter` (from Task 4):

```go
func (a *authCharRepoAdapter) ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*world.Character, error) {
    rows, err := a.pool.Query(ctx,
        `SELECT id, name, player_id, location_id, description, created_at
         FROM characters WHERE player_id = $1 ORDER BY name`, playerID.String())
    // scan rows into []*world.Character
}
```

The `listCharactersForPlayer` helper on CoreServer calls this method and
cross-references with `sessionStore.ListByPlayer` to determine session
status for each character.

The `SelectCharacter`, `CreatePlayer`, `CreateCharacter`, `ListCharacters`,
`RequestPasswordReset`, `ConfirmPasswordReset`, and `Logout` handlers
follow the same pattern: validate token → delegate to auth service → return
proto response.

- [ ] **Step 4: Implement remaining 7 handlers**

Each follows the pattern: validate input → resolve player token (where
applicable) → delegate to service → build proto response.

Key implementation notes:

- `SelectCharacter`: Validates player token, calls `authService.SelectCharacter`,
  then creates a game session via the existing `sessionStore.Set()` path
  (similar to `Authenticate` at server.go:175-249)
- `CreatePlayer`: Calls `authService.CreatePlayer`, creates player token
- `Logout`: Calls `authService.Logout(ctx, sessionID)`
- `RequestPasswordReset`: Calls `resetService.RequestReset`, logs token to
  slog (stubbed email delivery)

- [ ] **Step 5: Write tests for all handlers**

Table-driven tests for each handler using mocks. Cover:

- Happy path
- Invalid credentials / token
- Token expired
- Service errors

- [ ] **Step 6: Add client methods for new RPCs**

In `internal/grpc/client.go`, add methods matching the `CoreClient` interface
pattern used by the web and telnet gateways.

- [ ] **Step 7: Run all gRPC tests**

Run: `go test ./internal/grpc/... -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/grpc/auth_handlers.go internal/grpc/auth_handlers_test.go \
       internal/grpc/server.go internal/grpc/client.go
git commit -m "feat(grpc): implement auth RPC handlers on CoreServer

AuthenticatePlayer, SelectCharacter, CreatePlayer, CreateCharacter,
ListCharacters, RequestPasswordReset, ConfirmPasswordReset, Logout.
All delegate to auth domain services."
```

---

### Task 4: Core Startup Wiring

**Files:**

- Modify: `cmd/holomush/core.go`

Wire the auth repositories and services into the CoreServer at startup.

- [ ] **Step 1: Add auth service wiring after ABAC stack initialization**

In `cmd/holomush/core.go`, after the `worldService` creation (around line 298)
and before TLS setup (line 340), add:

```go
// Wire auth services for two-phase login
authPlayerRepo := authpostgres.NewPlayerRepository(abacPool)
authSessionRepo := authpostgres.NewWebSessionRepository(abacPool)
authResetRepo := authpostgres.NewPasswordResetRepository(abacPool)
authPlayerTokenRepo := store.NewPostgresPlayerTokenStore(abacPool)
authHasher := auth.NewArgon2idHasher()

authService, authErr := auth.NewAuthServiceWithLogger(
    authPlayerRepo, authSessionRepo, authHasher, slog.Default(),
)
if authErr != nil {
    return oops.Code("AUTH_SETUP_FAILED").Wrap(authErr)
}

resetService, resetErr := auth.NewPasswordResetServiceWithLogger(
    authPlayerRepo, authResetRepo, authSessionRepo, authHasher, slog.Default(),
)
if resetErr != nil {
    return oops.Code("AUTH_SETUP_FAILED").Wrap(resetErr)
}

authCharRepo := // implementation of auth.CharacterRepository
// Note: auth.CharacterRepository needs ExistsByName and CountByPlayer
// which may need a thin adapter around worldpostgres.CharacterRepository
authLocRepo := // implementation of auth.LocationRepository
// Note: needs GetStartingLocation — thin adapter returning startLocationID

characterService, charErr := auth.NewCharacterService(authCharRepo, authLocRepo)
if charErr != nil {
    return oops.Code("AUTH_SETUP_FAILED").Wrap(charErr)
}
```

Then pass to CoreServer constructor (around line 398):

```go
coreServer := holoGRPC.NewCoreServer(engine, sessions, sessionStore,
    holoGRPC.WithAuthenticator(guestAuth),
    holoGRPC.WithEventStore(realStore),
    holoGRPC.WithWorldQuerier(worldService),
    holoGRPC.WithAuthService(authService),
    holoGRPC.WithResetService(resetService),
    holoGRPC.WithCharacterService(characterService),
    holoGRPC.WithPlayerTokenRepo(authPlayerTokenRepo),
    holoGRPC.WithPlayerRepo(authPlayerRepo),
    // ... existing options ...
)
```

**Important:** `auth.CharacterRepository` needs `ExistsByName(ctx, name)` and
`CountByPlayer(ctx, playerID)` which `worldpostgres.CharacterRepository` does
not have. `auth.LocationRepository` needs `GetStartingLocation(ctx)`.

Create adapters in `cmd/holomush/` or `internal/auth/postgres/`:

```go
// authCharRepoAdapter wraps worldpostgres.CharacterRepository and adds
// auth-specific queries using the same connection pool.
type authCharRepoAdapter struct {
    pool *pgxpool.Pool
}

func (a *authCharRepoAdapter) Create(ctx context.Context, char *world.Character) error {
    return worldpostgres.NewCharacterRepository(a.pool).Create(ctx, char)
}

func (a *authCharRepoAdapter) ExistsByName(ctx context.Context, name string) (bool, error) {
    var exists bool
    err := a.pool.QueryRow(ctx,
        "SELECT EXISTS(SELECT 1 FROM characters WHERE LOWER(name) = LOWER($1))",
        name,
    ).Scan(&exists)
    return exists, err
}

func (a *authCharRepoAdapter) CountByPlayer(ctx context.Context, playerID ulid.ULID) (int, error) {
    var count int
    err := a.pool.QueryRow(ctx,
        "SELECT COUNT(*) FROM characters WHERE player_id = $1",
        playerID.String(),
    ).Scan(&count)
    return count, err
}
```

Similarly for `auth.LocationRepository`:

```go
type authLocRepoAdapter struct {
    startLocationID ulid.ULID
    pool            *pgxpool.Pool
}

func (a *authLocRepoAdapter) GetStartingLocation(ctx context.Context) (*world.Location, error) {
    return worldpostgres.NewLocationRepository(a.pool).GetByID(ctx, "", a.startLocationID)
}
```

Place adapter code in `cmd/holomush/auth_adapters.go` (not inline in `core.go`)
for testability.

Then in the wiring code in `core.go`:

```go
authCharRepo := &authCharRepoAdapter{pool: abacPool}
authLocRepo := &authLocRepoAdapter{startLocationID: startLocationID, pool: abacPool}
```

After modifying `auth.CharacterRepository` interface (adding `ListByPlayer`),
regenerate mocks:

Run: `mockery`
Expected: Regenerates `internal/auth/mocks/mock_CharacterRepository.go`

- [ ] **Step 2: Add required imports**

Add imports for `internal/auth`, `internal/auth/postgres`, `internal/store`.

- [ ] **Step 3: Verify compilation and test**

Run: `task build && task test`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/holomush/core.go
git commit -m "feat(core): wire auth services into CoreServer startup

Connects PlayerRepository, WebSessionRepository, PasswordResetRepository,
PlayerTokenStore, and auth services to CoreServer for two-phase login."
```

---

## Chunk 2: Web Gateway

### Task 5: Web Proto Updates

**Files:**

- Modify: `api/proto/holomush/web/v1/web.proto`

Remove the existing stub RPCs and add Web-prefixed auth RPCs.

- [ ] **Step 1: Update web.proto**

In the `WebService` block, remove:

- `AuthenticatePlayer` (line 45)
- `ListCharacters` (line 48)
- `SelectCharacter` (line 51)
- `ListSessions` (line 54)

Add Web-prefixed RPCs:

```protobuf
  // Web auth: authenticate player, sets httpOnly cookie.
  rpc WebAuthenticatePlayer(WebAuthenticatePlayerRequest) returns (WebAuthenticatePlayerResponse);

  // Web auth: select character, sets session cookie.
  rpc WebSelectCharacter(WebSelectCharacterRequest) returns (WebSelectCharacterResponse);

  // Web auth: create player account, sets cookie.
  rpc WebCreatePlayer(WebCreatePlayerRequest) returns (WebCreatePlayerResponse);

  // Web auth: create character.
  rpc WebCreateCharacter(WebCreateCharacterRequest) returns (WebCreateCharacterResponse);

  // Web auth: list characters.
  rpc WebListCharacters(WebListCharactersRequest) returns (WebListCharactersResponse);

  // Web auth: logout, clears cookies.
  rpc WebLogout(WebLogoutRequest) returns (WebLogoutResponse);

  // Web auth: request password reset.
  rpc WebRequestPasswordReset(WebRequestPasswordResetRequest) returns (WebRequestPasswordResetResponse);

  // Web auth: confirm password reset.
  rpc WebConfirmPasswordReset(WebConfirmPasswordResetRequest) returns (WebConfirmPasswordResetResponse);
```

Add corresponding messages. These mirror the core proto messages but with
web-specific fields (e.g., `remember_me` on authenticate).

Also update the existing `CharacterSummary` message to include
`last_location` (field 5) and `last_played_at` (field 6).

Remove the old two-phase login messages (`AuthenticatePlayerRequest/Response`,
`ListCharactersRequest/Response`, `SelectCharacterRequest/Response`,
`ListSessionsRequest/Response`, `SessionSummary`).

- [ ] **Step 2: Regenerate and verify**

Run: `task generate && task build`
Expected: Build may fail because `handler.go` references removed types.
That's expected — Task 7 fixes it.

- [ ] **Step 3: Commit**

```bash
git add api/proto/holomush/web/v1/web.proto pkg/proto/holomush/web/v1/
git commit -m "feat(proto): update web.proto with Web-prefixed auth RPCs

Remove old stub RPCs (AuthenticatePlayer, ListCharacters, SelectCharacter,
ListSessions). Add 8 Web-prefixed RPCs with cookie-aware semantics.
Update CharacterSummary with last_location and last_played_at."
```

---

### Task 6: Cookie Middleware

**Files:**

- Create: `internal/web/cookie.go`
- Create: `internal/web/cookie_test.go`

Implements httpOnly cookie management for auth tokens.

- [ ] **Step 1: Write failing test for cookie setting**

```go
func TestSetSessionCookie(t *testing.T) {
    w := httptest.NewRecorder()
    SetSessionCookie(w, "test-token-value", false, true) // secure=true
    cookies := w.Result().Cookies()
    require.Len(t, cookies, 1)
    assert.Equal(t, "holomush_session", cookies[0].Name)
    assert.Equal(t, "test-token-value", cookies[0].Value)
    assert.True(t, cookies[0].HttpOnly)
    assert.True(t, cookies[0].Secure)
    assert.Equal(t, http.SameSiteStrictMode, cookies[0].SameSite)
    assert.Equal(t, 86400, cookies[0].MaxAge) // 24 hours
}

func TestSetSessionCookie_RememberMe(t *testing.T) {
    w := httptest.NewRecorder()
    SetSessionCookie(w, "test-token-value", true, true)
    cookies := w.Result().Cookies()
    assert.Equal(t, 2592000, cookies[0].MaxAge) // 30 days
}

func TestSetSessionCookie_Insecure(t *testing.T) {
    w := httptest.NewRecorder()
    SetSessionCookie(w, "test-token-value", false, false) // dev mode
    cookies := w.Result().Cookies()
    assert.False(t, cookies[0].Secure)
    assert.Equal(t, http.SameSiteLaxMode, cookies[0].SameSite)
}

func TestClearSessionCookie(t *testing.T) {
    w := httptest.NewRecorder()
    ClearSessionCookie(w)
    cookies := w.Result().Cookies()
    require.Len(t, cookies, 1)
    assert.Equal(t, -1, cookies[0].MaxAge)
}

func TestGetSessionToken(t *testing.T) {
    req := httptest.NewRequest("GET", "/", nil)
    req.AddCookie(&http.Cookie{Name: "holomush_session", Value: "my-token"})
    token := GetSessionToken(req)
    assert.Equal(t, "my-token", token)
}
```

- [ ] **Step 2: Implement cookie functions**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import "net/http"

const (
    cookieName       = "holomush_session"
    cookieMaxAge     = 86400   // 24 hours
    cookieMaxAgeLong = 2592000 // 30 days
)

// SetSessionCookie sets an httpOnly session cookie.
// secure should be true in production (HTTPS) and false in local dev (HTTP).
func SetSessionCookie(w http.ResponseWriter, token string, rememberMe, secure bool) {
    maxAge := cookieMaxAge
    if rememberMe {
        maxAge = cookieMaxAgeLong
    }
    sameSite := http.SameSiteStrictMode
    if !secure {
        sameSite = http.SameSiteLaxMode // Strict doesn't work without Secure
    }
    http.SetCookie(w, &http.Cookie{
        Name:     cookieName,
        Value:    token,
        Path:     "/",
        MaxAge:   maxAge,
        HttpOnly: true,
        Secure:   secure,
        SameSite: sameSite,
    })
}

func ClearSessionCookie(w http.ResponseWriter) {
    http.SetCookie(w, &http.Cookie{
        Name:     cookieName,
        Value:    "",
        Path:     "/",
        MaxAge:   -1,
        HttpOnly: true,
        Secure:   true,
        SameSite: http.SameSiteStrictMode,
    })
}

func GetSessionToken(r *http.Request) string {
    cookie, err := r.Cookie(cookieName)
    if err != nil {
        return ""
    }
    return cookie.Value
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/web/ -run TestSetSessionCookie -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/web/cookie.go internal/web/cookie_test.go
git commit -m "feat(web): httpOnly cookie middleware for auth sessions

SetSessionCookie, ClearSessionCookie, GetSessionToken.
24hr default, 30 days with remember_me. HttpOnly, Secure, SameSiteStrict."
```

---

### Task 7: Web Gateway Auth Handlers

**Files:**

- Create: `internal/web/auth_handlers.go`
- Create: `internal/web/auth_handlers_test.go`
- Modify: `internal/web/handler.go` (remove stubs, update Handler struct)
- Modify: `internal/web/server.go` (wire cookie middleware)

Implements all 8 Web-prefixed RPC handlers. Each calls the corresponding
core RPC and wraps the response with cookie management.

- [ ] **Step 1: Update Handler struct and CoreClient interface**

In `handler.go`, update `CoreClient` to include the new core auth methods.
Remove the 4 stub methods (`AuthenticatePlayer`, `ListCharacters`,
`SelectCharacter`, `ListSessions`) and `errUnimplemented`.

Add `authService *auth.Service` to the Handler if needed for session
validation, or keep it pure proxy.

- [ ] **Step 2: Implement WebAuthenticatePlayer**

In `auth_handlers.go`:

```go
func (h *Handler) WebAuthenticatePlayer(
    ctx context.Context,
    req *connect.Request[webv1.WebAuthenticatePlayerRequest],
) (*connect.Response[webv1.WebAuthenticatePlayerResponse], error) {
    // Call core AuthenticatePlayer RPC
    coreResp, err := h.client.AuthenticatePlayer(ctx, &corev1.AuthenticatePlayerRequest{
        Username:   req.Msg.GetUsername(),
        Password:   req.Msg.GetPassword(),
        RememberMe: req.Msg.GetRememberMe(),
    })
    if err != nil {
        // Return error as response, not RPC error
        return connect.NewResponse(&webv1.WebAuthenticatePlayerResponse{
            Success:      false,
            ErrorMessage: "authentication error",
        }), nil
    }

    if !coreResp.GetSuccess() {
        return connect.NewResponse(&webv1.WebAuthenticatePlayerResponse{
            Success:      false,
            ErrorMessage: coreResp.GetErrorMessage(),
        }), nil
    }

    // Set cookie with player token
    // ConnectRPC: set cookie via response header
    resp := connect.NewResponse(&webv1.WebAuthenticatePlayerResponse{
        Success:            true,
        PlayerToken:        coreResp.GetPlayerToken(),
        Characters:         translateCharacterSummaries(coreResp.GetCharacters()),
        DefaultCharacterId: coreResp.GetDefaultCharacterId(),
    })

    // Note: ConnectRPC responses don't directly expose http.ResponseWriter.
    // Cookie setting requires HTTP interceptor middleware wrapping the handler.
    // The cookie value is passed in a response header that the middleware reads.
    resp.Header().Set("X-Set-Session-Token", coreResp.GetPlayerToken())
    resp.Header().Set("X-Remember-Me", fmt.Sprintf("%t", req.Msg.GetRememberMe()))

    return resp, nil
}
```

Note: ConnectRPC doesn't expose `http.ResponseWriter` directly. Cookies
must be set via HTTP middleware that intercepts the response headers. The
handler sets internal headers (`X-Set-Session-Token`) that the middleware
reads and converts to `Set-Cookie`.

- [ ] **Step 3: Implement remaining 7 handlers**

Following the same proxy + header pattern. `WebLogout` sets
`X-Clear-Session: true` header for the middleware.

- [ ] **Step 4: Implement HTTP cookie middleware in server.go**

In `server.go`, wrap the ConnectRPC handler with middleware that:

1. On response, checks for `X-Set-Session-Token` header → calls `SetSessionCookie`
2. On response, checks for `X-Clear-Session` header → calls `ClearSessionCookie`
3. On request, reads session cookie → sets `X-Session-Token` request header
   for handlers to read

- [ ] **Step 5: Update CORS middleware for credentials**

In `internal/web/cors.go`, add `Access-Control-Allow-Credentials: true`
header. This is REQUIRED for `credentials: 'include'` to work — browsers
reject responses without it.

```go
w.Header().Set("Access-Control-Allow-Credentials", "true")
```

Add this line after the existing `Access-Control-Allow-Origin` header set
(line 36 of cors.go). Also add `"Cookie"` to the `connectHeaders` slice
so cookies pass the preflight check.

Update `cors_test.go` to verify the new header is present.

- [ ] **Step 6: Update transport.ts for credentials**

The ConnectRPC transport in `web/src/lib/transport.ts` needs
`credentials: 'include'` to send cookies cross-origin.

```typescript
export const transport = createConnectTransport({
  baseUrl,
  credentials: 'include',
});
```

- [ ] **Step 6: Write tests**

Test each handler with mock core client. Verify:

- Correct core RPC is called
- Cookie headers are set on success
- Error responses don't set cookies
- Cookie is cleared on logout

- [ ] **Step 7: Run tests**

Run: `go test ./internal/web/... -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/web/auth_handlers.go internal/web/auth_handlers_test.go \
       internal/web/handler.go internal/web/server.go \
       web/src/lib/transport.ts
git commit -m "feat(web): implement auth proxy handlers with cookie middleware

8 Web-prefixed auth RPC handlers proxying to core server.
HTTP middleware converts response headers to Set-Cookie.
Transport updated with credentials: include for CORS cookies."
```

---

## Chunk 3: Web Client

### Task 8: Auth Store and Top Bar

**Files:**

- Create: `web/src/lib/stores/authStore.ts`
- Create: `web/src/lib/components/TopBar.svelte`
- Create: `web/src/routes/+layout.svelte`
- Modify: `web/src/routes/+layout.ts`
- Modify: `web/package.json` (add lucide-svelte)

- [ ] **Step 1: Install lucide-svelte**

Run: `cd web && pnpm add lucide-svelte && cd ..`

- [ ] **Step 2: Create authStore**

`web/src/lib/stores/authStore.ts`:

```typescript
import { writable, derived } from 'svelte/store';

interface AuthState {
  playerToken: string | null;
  sessionId: string | null;
  characterName: string | null;
  playerName: string | null;
  isGuest: boolean;
}

const initial: AuthState = {
  playerToken: null,
  sessionId: null,
  characterName: null,
  playerName: null,
  isGuest: false,
};

export const authState = writable<AuthState>(initial);

export const isAuthenticated = derived(authState, ($s) => !!$s.playerToken || !!$s.sessionId);
export const hasCharacter = derived(authState, ($s) => !!$s.sessionId && !!$s.characterName);

export function setPlayerAuth(playerToken: string, playerName: string) {
  authState.update((s) => ({ ...s, playerToken, playerName, isGuest: false }));
}

export function setCharacterSession(sessionId: string, characterName: string) {
  authState.update((s) => ({ ...s, sessionId, characterName }));
  sessionStorage.setItem('holomush-session', JSON.stringify({ sessionId, characterName }));
}

export function setGuestSession(sessionId: string, characterName: string) {
  authState.update((s) => ({
    ...s, sessionId, characterName, isGuest: true, playerName: characterName,
  }));
  sessionStorage.setItem('holomush-session', JSON.stringify({ sessionId, characterName }));
}

export function clearAuth() {
  authState.set(initial);
  sessionStorage.removeItem('holomush-session');
}

export function restoreSession() {
  const saved = sessionStorage.getItem('holomush-session');
  if (saved) {
    try {
      const { sessionId, characterName } = JSON.parse(saved);
      if (sessionId) {
        authState.update((s) => ({ ...s, sessionId, characterName }));
      }
    } catch { /* ignore corrupt data */ }
  }
}
```

- [ ] **Step 3: Create TopBar component**

`web/src/lib/components/TopBar.svelte` — uses `authState`, `isAuthenticated`,
`hasCharacter` stores to render three states. Uses `lucide-svelte` icons
(LogOut, ArrowLeftRight) for logout and switch-character actions.

Uses `themeToCssVars` from the existing theme system for all colors.

- [ ] **Step 4: Create root layout**

`web/src/routes/+layout.svelte`:

```svelte
<script lang="ts">
  import TopBar from '$lib/components/TopBar.svelte';
  import { restoreSession } from '$lib/stores/authStore';
  import { onMount } from 'svelte';

  onMount(() => restoreSession());
</script>

<TopBar />

<main>
  {@render children()}
</main>
```

Update `+layout.ts`: keep `ssr = false`, remove `prerender = true` (auth
pages need dynamic behavior).

- [ ] **Step 5: Verify terminal still works**

Run: `cd web && pnpm dev` — navigate to /terminal, verify it loads.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/stores/authStore.ts web/src/lib/components/TopBar.svelte \
       web/src/routes/+layout.svelte web/src/routes/+layout.ts web/package.json \
       web/pnpm-lock.yaml
git commit -m "feat(web): auth store, top bar component, root layout

authStore tracks player/session/character state.
TopBar renders three auth states with lucide icons.
Root layout wraps all pages with TopBar."
```

---

### Task 9: Login Page

**Files:**

- Create: `web/src/routes/login/+page.svelte`

- [ ] **Step 1: Implement login page**

Form with username, password, remember-me checkbox. On submit, calls
`WebAuthenticatePlayer` via ConnectRPC client. On success, updates
`authStore` and redirects to `/characters` (or `/terminal` if auto-skip
applies). Guest link calls existing `Login` RPC.

Uses theme CSS variables for all styling. Inline error messages for
validation failures.

- [ ] **Step 2: Verify in browser**

Navigate to `/login`, check form renders, theme works in dark/light.

- [ ] **Step 3: Commit**

---

### Task 10: Register Page

**Files:**

- Create: `web/src/routes/register/+page.svelte`

- [ ] **Step 1: Implement register page**

Form with username, email (optional), password, confirm password. Calls
`WebCreatePlayer`. On success, auto-login → redirect to `/characters`.
Client-side validation: password match, minimum length.

- [ ] **Step 2: Commit**

---

### Task 11: Character Select Page

**Files:**

- Create: `web/src/routes/(authed)/characters/+page.svelte`
- Create: `web/src/routes/(authed)/+layout.ts`

- [ ] **Step 1: Create auth guard layout**

`(authed)/+layout.ts`: Since httpOnly cookies are not readable by
JavaScript, the auth guard checks `authStore` state (which is restored
from `sessionStorage` on page load via `restoreSession()`). If `authStore`
has no `sessionId` and no `playerToken`, redirect to `/login`.

On page reload, `sessionStorage` restores the session state. If the
session has actually expired server-side, the first RPC call will fail
and the error handler redirects to `/login`.

```typescript
import { get } from 'svelte/store';
import { redirect } from '@sveltejs/kit';
import { isAuthenticated } from '$lib/stores/authStore';

export function load() {
  if (!get(isAuthenticated)) {
    throw redirect(302, '/login');
  }
}
```

- [ ] **Step 2: Implement character select page**

Displays character cards with:

- 44×44 icon slot (initial letter + accent color)
- Character name, last played, last location
- Session status badge (active/detached)
- "Create New Character" dashed card
- "Auto-enter as default" checkbox

On character click: calls `WebSelectCharacter`, sets session in authStore,
redirects to `/terminal`.

Create character: inline form that calls `WebCreateCharacter`, then
auto-selects the new character.

- [ ] **Step 3: Commit**

---

### Task 12: Password Reset Pages

**Files:**

- Create: `web/src/routes/reset/+page.svelte`
- Create: `web/src/routes/reset/confirm/+page.svelte`

- [ ] **Step 1: Implement reset request page**

Email field + submit. Always shows success message. Calls
`WebRequestPasswordReset`.

- [ ] **Step 2: Implement reset confirm page**

Reads token from URL query params. New password + confirm fields. Calls
`WebConfirmPasswordReset`. On success, redirect to `/login`.

- [ ] **Step 3: Commit**

---

### Task 13: Terminal Page Migration and Landing Update

**Files:**

- Move: `web/src/routes/terminal/` → `web/src/routes/(authed)/terminal/`
- Modify: `web/src/routes/(authed)/terminal/+page.svelte` (remove inline login)
- Modify: `web/src/routes/+page.svelte` (update landing page)

- [ ] **Step 1: Move terminal into auth group**

Move the terminal directory into `(authed)/`. URL stays `/terminal`.

- [ ] **Step 2: Remove inline guest login from terminal page**

The terminal page currently calls `client.login()` directly. Replace with
reading `sessionId` and `characterName` from `authStore`. If no session,
the auth guard layout redirects to `/login` before this page loads.

- [ ] **Step 3: Update landing page**

Update `/` to show Login/Register links and a "Try as Guest" button. The
guest button calls the existing `Login` RPC, sets `authStore`, redirects
to `/terminal`.

- [ ] **Step 4: Verify full flow in browser**

1. Navigate to `/` — see landing with auth links
2. Click "Login" → `/login` page
3. Login with valid credentials → `/characters`
4. Select character → `/terminal` with game events
5. Click logout icon → back to `/login`

- [ ] **Step 5: Commit**

```bash
git add web/src/routes/
git commit -m "feat(web): auth pages, terminal migration, landing update

Login, register, character select, password reset pages.
Terminal moved to (authed) layout group.
Landing page updated with auth links and guest access."
```

---

## Chunk 4: Telnet Auth & E2E Tests

### Task 14: Telnet Two-Phase Auth

**Files:**

- Modify: `internal/telnet/gateway_handler.go`

- [ ] **Step 1: Update handleConnect for two-phase flow**

When `connect <username> <password>` is received (not guest):

1. Call `AuthenticatePlayer` core RPC
2. If success and 1 character + auto_login or default_character: auto-select
3. If success and multiple characters: print character list, enter
   "character selection" mode
4. In selection mode, handle `PLAY <name|number>` and `CREATE <name>`

The guest flow (`connect guest`) stays unchanged — uses existing
`Authenticate` RPC.

- [ ] **Step 2: Add PLAY command handler**

When in character-selection mode and user types `play alaric` or `play 1`:

1. Resolve character by name or index from the displayed list
2. Call `SelectCharacter` core RPC
3. On success, subscribe to events and enter game

- [ ] **Step 3: Add CREATE command handler**

When in character-selection mode and user types `create Alaric`:

1. Call `CreateCharacter` core RPC
2. On success, auto-select the new character
3. Subscribe to events and enter game

- [ ] **Step 4: Write tests**

Test the new command parsing and flow transitions. Use mock core client.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/telnet/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/telnet/gateway_handler.go internal/telnet/*_test.go
git commit -m "feat(telnet): two-phase login with PLAY and CREATE commands

connect <user> <pass> now uses AuthenticatePlayer RPC.
Character selection mode with PLAY <name|number> and CREATE <name>.
Guest flow via 'connect guest' unchanged."
```

---

### Task 15: Playwright E2E Tests

**Files:**

- Create: `web/tests/auth.spec.ts`

- [ ] **Step 1: Write E2E tests**

Tests require the full Docker stack running (`task dev`). Test scenarios:

1. **Guest login**: Landing → "Try as Guest" → terminal loads
2. **Register → login → select character → terminal**: Full flow
3. **Login with wrong password**: Error message shown
4. **Session persistence**: Login → close tab → reopen → still in terminal
5. **Logout**: Terminal → logout icon → redirected to login
6. **Character creation**: Login → characters → create → auto-enters

Pattern from existing `web/tests/`:

```typescript
import { test, expect } from '@playwright/test';

test.describe('Auth Flows', () => {
  test('guest can enter terminal', async ({ page }) => {
    await page.goto('/');
    await page.click('text=Try as Guest');
    await expect(page.locator('[data-testid="terminal-view"]')).toBeVisible();
  });

  // ... more tests
});
```

- [ ] **Step 2: Run E2E tests**

Run: `task test:e2e`
Expected: PASS (requires Docker stack via `task dev`)

- [ ] **Step 3: Commit**

```bash
git add web/tests/auth.spec.ts
git commit -m "test(e2e): Playwright tests for auth flows

Guest login, registration, character selection, logout,
session persistence, error handling."
```

---

## Post-Implementation Checklist

- [ ] `task test` — all unit tests pass
- [ ] `task lint` — no lint errors
- [ ] `task build` — builds cleanly
- [ ] `task test:e2e` — E2E tests pass (with Docker stack)
- [ ] `task fmt` — code formatted
- [ ] `bd close holomush-qve.6` — mark bead complete
- [ ] Request code review via `pr-review-toolkit:review-pr`
