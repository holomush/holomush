# Session Consolidation Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate the in-memory `core.SessionManager` by consolidating all session operations into the existing Postgres-backed `session.Store`.

**Architecture:** Add `SessionAccess` interface (3 methods) to `session` package for command handlers. Add `ErrSessionEnded` sentinel to `command` package for quit/self-boot detection. Remove `core.SessionManager`, `core.Session`, `core.SessionService`, and `Engine.sessions` field. All session state flows through one Postgres implementation.

**Tech Stack:** Go, PostgreSQL (via pgx), testify, oops (structured errors)

**Spec:** `docs/superpowers/specs/2026-03-22-session-consolidation-design.md`

---

## Chunk 1: Foundation — New Interface and Sentinel

### Task 1: Add `SessionAccess` interface and new `Store` methods

**Files:**

- Modify: `internal/session/session.go`
- Modify: `internal/store/session_store.go`
- Modify: `internal/session/memstore.go`
- Test: `internal/store/session_store_test.go` (if exists, else integration tests cover it)

- [ ] **Step 1: Add `SessionAccess` interface to `internal/session/session.go`**

After the `Store` interface, add:

```go
// SessionAccess provides session operations for command handlers.
// This is a narrow subset of Store — only what handlers need.
type SessionAccess interface {
	// ListActive returns all sessions with status=active.
	ListActive(ctx context.Context) ([]*Info, error)

	// FindByCharacter returns the active or detached session for a character.
	FindByCharacter(ctx context.Context, characterID ulid.ULID) (*Info, error)

	// DeleteByCharacter finds and deletes a character's session.
	// Returns the deleted Info for caller use (disconnect hooks, leave events).
	// Returns nil, nil if no session exists.
	DeleteByCharacter(ctx context.Context, characterID ulid.ULID, reason string) (*Info, error)
}
```

- [ ] **Step 2: Add `ListActive`, `DeleteByCharacter`, `UpdateActivity` to `Store` interface**

In the `Store` interface in `internal/session/session.go`, add these three methods:

```go
// ListActive returns all sessions with status=active.
ListActive(ctx context.Context) ([]*Info, error)

// DeleteByCharacter finds and deletes a character's session.
// Returns the deleted Info, or nil if no session exists.
DeleteByCharacter(ctx context.Context, characterID ulid.ULID, reason string) (*Info, error)

// UpdateActivity bumps the updated_at timestamp for a session.
UpdateActivity(ctx context.Context, id string) error
```

- [ ] **Step 3: Implement `ListActive` on `PostgresSessionStore`**

In `internal/store/session_store.go`, add after `ListActiveByLocation`:

```go
func (s *PostgresSessionStore) ListActive(ctx context.Context) ([]*session.Info, error) {
	query := `SELECT ` + sessionSelectColumns + ` FROM sessions WHERE status = 'active' ORDER BY created_at`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, oops.With("operation", "list_active_sessions").Wrap(err)
	}
	defer rows.Close()
	return scanSessions(rows)
}
```

- [ ] **Step 4: Implement `DeleteByCharacter` on `PostgresSessionStore`**

```go
func (s *PostgresSessionStore) DeleteByCharacter(ctx context.Context, characterID ulid.ULID, reason string) (*session.Info, error) {
	info, err := s.FindByCharacter(ctx, characterID)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, nil
	}
	if err := s.Delete(ctx, info.ID, reason); err != nil {
		return nil, err
	}
	return info, nil
}
```

- [ ] **Step 5: Implement `UpdateActivity` on `PostgresSessionStore`**

```go
func (s *PostgresSessionStore) UpdateActivity(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return oops.With("operation", "update_activity").With("session_id", id).Wrap(err)
	}
	return nil
}
```

- [ ] **Step 6: Add compile-time check for `SessionAccess`**

In `internal/store/session_store.go`, after the existing `Store` check:

```go
var _ session.SessionAccess = (*PostgresSessionStore)(nil)
```

- [ ] **Step 7: Add stubs to `MemStore` in `internal/session/memstore.go`**

The `MemStore` implements `Store` for unit tests. Add the three new methods:

```go
func (m *MemStore) ListActive(ctx context.Context) ([]*Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*Info
	for _, info := range m.sessions {
		if info.Status == StatusActive {
			result = append(result, info)
		}
	}
	return result, nil
}

func (m *MemStore) DeleteByCharacter(ctx context.Context, characterID ulid.ULID, reason string) (*Info, error) {
	info, err := m.FindByCharacter(ctx, characterID)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, nil
	}
	if err := m.Delete(ctx, info.ID, reason); err != nil {
		return nil, err
	}
	return info, nil
}

func (m *MemStore) UpdateActivity(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.sessions[id]
	if !ok {
		return oops.Code("SESSION_NOT_FOUND").With("session_id", id).Errorf("session not found")
	}
	info.UpdatedAt = time.Now()
	return nil
}
```

- [ ] **Step 8: Run tests to verify compilation**

Run: `task test`
Expected: All existing tests pass. New methods compile.

- [ ] **Step 9: Commit**

```text
jj describe -m "feat(session): add SessionAccess interface and new Store methods

Add ListActive, DeleteByCharacter, UpdateActivity to session.Store.
Add SessionAccess interface (3-method subset) for command handlers.
Implement on PostgresSessionStore and MemStore."
jj new
```

### Task 2: Add `ErrSessionEnded` sentinel to command package

**Files:**

- Modify: `internal/command/errors.go` (or wherever command errors are defined)

- [ ] **Step 1: Find where command errors are defined**

Search for existing error variables in the command package:

```bash
rg "var Err" internal/command/ --type go
```

- [ ] **Step 2: Add `ErrSessionEnded`**

In the appropriate file (likely `internal/command/errors.go` or `internal/command/types.go`):

```go
// ErrSessionEnded signals that the handler ended the session gracefully.
// Server-side teardown (leave event, PG delete, hooks) happens in the
// gRPC server, not the handler.
var ErrSessionEnded = errors.New("session ended")
```

Ensure `"errors"` is in the imports.

- [ ] **Step 3: Run tests**

Run: `task test`
Expected: PASS (adding a variable doesn't break anything)

- [ ] **Step 4: Commit**

```text
jj describe -m "feat(command): add ErrSessionEnded sentinel for quit detection

Handlers return this instead of calling EndSession directly.
Server-side teardown checks errors.Is(err, ErrSessionEnded)."
jj new
```

---

## Chunk 2: Wire `SessionAccess` Into Command Layer

> **Compilation warning:** Chunks 2 and 3 form an atomic unit. After Task 3
> changes the `Services.Session()` type, handler code that calls
> `ListActiveSessions()` or `EndSession()` will not compile until Tasks 6-9
> update those call sites. `task test` will fail until Chunk 3 is complete.
> This is expected — proceed through Tasks 3-9 in order.

### Task 3: Update `command.Services` to use `session.SessionAccess`

**Files:**

- Modify: `internal/command/types.go`
- Modify: `internal/command/types_test.go`

- [ ] **Step 1: Change `ServicesConfig.Session` field type**

In `internal/command/types.go`, change:

```go
Session          core.SessionService      // session management
```

to:

```go
Session          session.SessionAccess    // session management
```

Add `"github.com/holomush/holomush/internal/session"` to imports. Remove the `core` import if no longer used (check other fields first).

- [ ] **Step 2: Change `Services.session` field type**

```go
session          core.SessionService      // session management
```

to:

```go
session          session.SessionAccess    // session management
```

- [ ] **Step 3: Change `Session()` getter return type**

```go
func (s *Services) Session() core.SessionService { return s.session }
```

to:

```go
func (s *Services) Session() session.SessionAccess { return s.session }
```

- [ ] **Step 4: Update validation in `NewServices`**

The validation checks `cfg.Session == nil`. This still works since the type changed but the nil check is valid for interfaces.

- [ ] **Step 5: Update `types_test.go` mock**

Replace the `mockSessionService` with a `mockSessionAccess`:

```go
type mockSessionAccess struct{}

func (m *mockSessionAccess) ListActive(_ context.Context) ([]*session.Info, error) {
	return nil, nil
}
func (m *mockSessionAccess) FindByCharacter(_ context.Context, _ ulid.ULID) (*session.Info, error) {
	return nil, nil
}
func (m *mockSessionAccess) DeleteByCharacter(_ context.Context, _ ulid.ULID, _ string) (*session.Info, error) {
	return nil, nil
}
```

Update all test references from `&mockSessionService{}` to `&mockSessionAccess{}`.

- [ ] **Step 6: Update `dispatcher_test.go` stub**

In `internal/command/dispatcher_test.go`, replace `stubSessionService` with:

```go
type stubSessionAccess struct{}

func (s *stubSessionAccess) ListActive(_ context.Context) ([]*session.Info, error) {
	return nil, nil
}
func (s *stubSessionAccess) FindByCharacter(_ context.Context, _ ulid.ULID) (*session.Info, error) {
	return nil, nil
}
func (s *stubSessionAccess) DeleteByCharacter(_ context.Context, _ ulid.ULID, _ string) (*session.Info, error) {
	return nil, nil
}
```

Update reference from `Session: &stubSessionService{}` to `Session: &stubSessionAccess{}`.

- [ ] **Step 7: Run tests**

Run: `task test`
Expected: Compilation errors in handler tests and anywhere else using `core.SessionService` via `Services`. These are expected — we fix them in the next tasks.

Note: If compilation errors prevent running tests, that's fine. Proceed to the next tasks to fix all callers.

- [ ] **Step 8: Commit (even if not compiling yet)**

```text
jj describe -m "refactor(command): switch Services.Session from core.SessionService to session.SessionAccess

Narrows the session interface exposed to command handlers.
Handler call sites updated in subsequent commits."
jj new
```

### Task 4: Update testutil `ServicesBuilder`

**Files:**

- Modify: `internal/command/handlers/testutil/services.go`

- [ ] **Step 1: Change `WithSession` parameter type**

```go
func (b *ServicesBuilder) WithSession(session core.SessionService) *ServicesBuilder {
```

to:

```go
func (b *ServicesBuilder) WithSession(sa session.SessionAccess) *ServicesBuilder {
	b.config.Session = sa
```

Add `"github.com/holomush/holomush/internal/session"` to imports. Remove `core` import if no longer needed.

- [ ] **Step 2: Change default in `NewServicesBuilder`**

Replace:

```go
Session: core.NewSessionManager(),
```

with a default mock. Create a `defaultSessionAccess` in the same file:

```go
// defaultSessionAccess is a no-op SessionAccess for tests that don't need sessions.
type defaultSessionAccess struct{}

func (d *defaultSessionAccess) ListActive(_ context.Context) ([]*session.Info, error) {
	return nil, nil
}
func (d *defaultSessionAccess) FindByCharacter(_ context.Context, _ ulid.ULID) (*session.Info, error) {
	return nil, nil
}
func (d *defaultSessionAccess) DeleteByCharacter(_ context.Context, _ ulid.ULID, _ string) (*session.Info, error) {
	return nil, nil
}
```

Use it: `Session: &defaultSessionAccess{},`

- [ ] **Step 3: Commit**

```text
jj describe -m "refactor(testutil): update ServicesBuilder to use session.SessionAccess"
jj new
```

### Task 5: Add `mockSessionAccess` test helper

**Files:**

- Create: `internal/command/handlers/testutil/mock_session.go`

- [ ] **Step 1: Create the mock**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package testutil

import (
	"context"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/session"
)

// MockSessionAccess implements session.SessionAccess for handler tests.
// It holds a slice of session.Info and provides simple lookup/delete.
type MockSessionAccess struct {
	mu       sync.Mutex
	Sessions []*session.Info
}

// NewMockSessionAccess creates a MockSessionAccess with the given sessions.
func NewMockSessionAccess(sessions ...*session.Info) *MockSessionAccess {
	return &MockSessionAccess{Sessions: sessions}
}

// ListActive returns all sessions with status=active.
func (m *MockSessionAccess) ListActive(_ context.Context) ([]*session.Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*session.Info
	for _, s := range m.Sessions {
		if s.Status == session.StatusActive {
			result = append(result, s)
		}
	}
	return result, nil
}

// FindByCharacter returns the session for a character, or nil.
func (m *MockSessionAccess) FindByCharacter(_ context.Context, charID ulid.ULID) (*session.Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.Sessions {
		if s.CharacterID == charID {
			return s, nil
		}
	}
	return nil, nil
}

// DeleteByCharacter removes and returns the session for a character.
func (m *MockSessionAccess) DeleteByCharacter(_ context.Context, charID ulid.ULID, _ string) (*session.Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, s := range m.Sessions {
		if s.CharacterID == charID {
			m.Sessions = append(m.Sessions[:i], m.Sessions[i+1:]...)
			return s, nil
		}
	}
	return nil, nil
}

// AddSession adds a session to the mock (helper for test setup).
func (m *MockSessionAccess) AddSession(charID ulid.ULID, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Sessions = append(m.Sessions, &session.Info{
		ID:            ulid.Make().String(),
		CharacterID:   charID,
		CharacterName: name,
		Status:        session.StatusActive,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	})
}
```

- [ ] **Step 2: Verify compilation**

Run: `task test` (may still have errors in handler tests — that's expected)

- [ ] **Step 3: Commit**

```text
jj describe -m "test(testutil): add MockSessionAccess for handler tests"
jj new
```

---

## Chunk 3: Migrate Command Handlers

> **Note:** Each handler task (6-9) includes BOTH production code AND test updates for that handler. Handler tests are updated alongside their handler, not deferred to a separate task. This keeps each task self-contained and independently testable.

### Task 6: Migrate quit handler

**Files:**

- Modify: `internal/command/handlers/quit.go`
- Modify: `internal/command/handlers/quit_test.go`

- [ ] **Step 1: Update quit handler to return `ErrSessionEnded`**

Replace the entire `QuitHandler` body in `quit.go`:

```go
func QuitHandler(ctx context.Context, exec *command.CommandExecution) error {
	writeOutput(ctx, exec, "quit", "Goodbye!")

	return oops.Code("SESSION_ENDED").Wrap(command.ErrSessionEnded)
}
```

The handler no longer calls `EndSession`. Server-side teardown happens in `executeViaDispatcher`.

Add `"github.com/samber/oops"` to imports if not present. Remove `"github.com/holomush/holomush/internal/command"` from import only if nothing else uses it (it's still used for `command.CommandExecution`).

- [ ] **Step 2: Update quit tests**

In `quit_test.go`, the tests create `core.NewSessionManager()` and check session removal. Replace with:

- Tests should verify `QuitHandler` returns `command.ErrSessionEnded`
- Tests should verify "Goodbye!" output
- Tests should NOT check session removal (that's the server's job now)

Update each test case's `setup` to return `*testutil.MockSessionAccess` instead of `*core.SessionManager`. Update assertions to check `errors.Is(err, command.ErrSessionEnded)`.

- [ ] **Step 3: Run quit tests**

Run: `task test` (or targeted: `go test ./internal/command/handlers/ -run TestQuit -v`)
Expected: PASS

- [ ] **Step 4: Commit**

```text
jj describe -m "refactor(quit): return ErrSessionEnded instead of calling EndSession

Server-side teardown (leave event, PG delete, hooks) now happens
in executeViaDispatcher, not the handler."
jj new
```

### Task 7: Migrate who handler

**Files:**

- Modify: `internal/command/handlers/who.go`
- Modify: `internal/command/handlers/who_test.go`

- [ ] **Step 1: Update `WhoHandler` to use `ListActive(ctx)`**

In `who.go`, change:

```go
sessions := exec.Services().Session().ListActiveSessions()
```

to:

```go
sessions, err := exec.Services().Session().ListActive(ctx)
if err != nil {
	slog.ErrorContext(ctx, "who: failed to list active sessions", "error", err)
	writeOutput(ctx, exec, "who", "Unable to retrieve player list. Please try again.")
	return nil
}
```

- [ ] **Step 2: Update session field references**

The who handler accesses `session.CharacterID` and `session.LastActivity`. After migration, sessions are `*session.Info`. The field names change:

- `session.CharacterID` stays the same (both types have it)
- `session.LastActivity` becomes `session.UpdatedAt` (session.Info uses UpdatedAt)

Update line:

```go
idleTime := now.Sub(session.LastActivity)
```

to:

```go
idleTime := now.Sub(session.UpdatedAt)
```

- [ ] **Step 3: Update who tests**

All who tests create `core.NewSessionManager()` and call `sessionMgr.Connect(charID, connID)` to set up sessions. Replace with `testutil.MockSessionAccess`:

For each test, replace:

```go
sessionMgr := core.NewSessionManager()
sessionMgr.Connect(charID, connID)
```

with:

```go
mockSA := testutil.NewMockSessionAccess(&session.Info{
	ID:            ulid.Make().String(),
	CharacterID:   charID,
	CharacterName: "TestCharacter",
	Status:        session.StatusActive,
	UpdatedAt:     time.Now(),
})
```

And pass `mockSA` via `ServicesBuilder.WithSession(mockSA)`.

This is a mechanical replacement across ~25 test functions. Each test's session setup changes from `Connect()` to creating `session.Info` structs directly.

- [ ] **Step 4: Run who tests**

Run: `task test` (or targeted: `go test ./internal/command/handlers/ -run TestWho -v`)
Expected: PASS

- [ ] **Step 5: Commit**

```text
jj describe -m "refactor(who): use session.SessionAccess.ListActive instead of in-memory ListActiveSessions"
jj new
```

### Task 8: Migrate wall handler

**Files:**

- Modify: `internal/command/handlers/wall.go`
- Modify: `internal/command/handlers/wall_test.go`

- [ ] **Step 1: Update `WallHandler` to use `ListActive(ctx)`**

Change:

```go
sessions := exec.Services().Session().ListActiveSessions()
```

to:

```go
sessions, err := exec.Services().Session().ListActive(ctx)
if err != nil {
	slog.ErrorContext(ctx, "wall: failed to list active sessions", "error", err)
	return oops.Code(command.CodeWorldError).
		With("message", "Unable to broadcast message. Please try again.").
		Wrap(err)
}
```

- [ ] **Step 2: Update session field references**

The wall handler accesses `session.CharacterID`. This field name is the same on `session.Info`, so no change needed.

- [ ] **Step 3: Update wall tests**

Same mechanical replacement as who tests — swap `core.NewSessionManager()` + `Connect()` for `testutil.MockSessionAccess` with `session.Info` structs.

- [ ] **Step 4: Run wall tests**

Run: `task test` (or targeted: `go test ./internal/command/handlers/ -run TestWall -v`)
Expected: PASS

- [ ] **Step 5: Commit**

```text
jj describe -m "refactor(wall): use session.SessionAccess.ListActive instead of in-memory ListActiveSessions"
jj new
```

### Task 9: Migrate boot handler

**Files:**

- Modify: `internal/command/handlers/boot.go`
- Modify: `internal/command/handlers/boot_test.go`

- [ ] **Step 1: Update `findCharacterByName` to use `ListActive(ctx)`**

In `boot.go`, change:

```go
sessions := exec.Services().Session().ListActiveSessions()
```

to:

```go
sessions, err := exec.Services().Session().ListActive(ctx)
if err != nil {
	slog.ErrorContext(ctx, "boot: failed to list active sessions", "error", err)
	return ulid.ULID{}, "", command.WorldError("Unable to search for player due to a system error. Please try again shortly.", err)
}
```

- [ ] **Step 2: Update `BootHandler` — self-boot returns `ErrSessionEnded`**

Replace the session ending block. Currently:

```go
if err := exec.Services().Session().EndSession(targetCharID); err != nil {
	return oops.Code(command.CodeWorldError).
		With("message", "Unable to boot player. Session may have already ended.").
		Wrap(err)
}
```

becomes:

```go
if isSelfBoot {
	return oops.Code("SESSION_ENDED").Wrap(command.ErrSessionEnded)
}

// Admin boot: delete target session directly.
if _, err := exec.Services().Session().DeleteByCharacter(ctx, targetCharID, formatBootMessage(exec.CharacterName(), reason, false)); err != nil {
	return oops.Code(command.CodeWorldError).
		With("message", "Unable to boot player. Session may have already ended.").
		Wrap(err)
}
```

The current flow is:
1. Notify target (system message, line 62)
2. EndSession (line 65)
3. Log admin boot (line 72)
4. Notify executor (writeOutput, line 83-90)

For self-boot, "Disconnecting..." is written at step 4 (line 85). If we return
`ErrSessionEnded` at step 2, we lose the output. So restructure: move the self-boot
return to AFTER the executor notification. Replace the `EndSession` block (lines 65-69)
AND the notification block (lines 83-92) with:

```go
// End session or signal quit
if isSelfBoot {
	// Notify executor before signaling session end
	writeOutput(ctx, exec, "boot", "Disconnecting...")
	return oops.Code("SESSION_ENDED").Wrap(command.ErrSessionEnded)
}

// Admin boot: delete target session directly.
if _, err := exec.Services().Session().DeleteByCharacter(ctx, targetCharID,
	formatBootMessage(exec.CharacterName(), reason, false)); err != nil {
	return oops.Code(command.CodeWorldError).
		With("message", "Unable to boot player. Session may have already ended.").
		Wrap(err)
}

// Log admin boots
if !isSelfBoot {
	slog.Info("admin boot", ...)
}

// Notify the executor
switch {
case reason != "":
	writeOutputf(ctx, exec, "boot", "%s has been booted. Reason: %s\n", targetCharName, reason)
default:
	writeOutputf(ctx, exec, "boot", "%s has been booted.\n", targetCharName)
}
```

Note: the self-boot case writes output and returns early, so it never reaches the
admin-only notification block below.

- [ ] **Step 3: Fix loop variable shadowing in `findCharacterByName`**

The loop variable `session` in `findCharacterByName` (line 119 of boot.go) now
shadows the `session` package import. Rename loop variables from `session` to `sess`:

```go
for _, sess := range sessions {
	char, err := exec.Services().World().GetCharacter(ctx, subjectID, sess.CharacterID)
```

`CharacterID` is the same field name on `session.Info`, so no other changes needed.

- [ ] **Step 4: Update boot tests**

Same mechanical replacement. Additionally, the `mockSessionManagerWithEndSessionError` type needs to be replaced with a `MockSessionAccess` that can simulate `DeleteByCharacter` errors.

For the EndSession error test: create a custom mock that returns an error from `DeleteByCharacter` for a specific character ID.

- [ ] **Step 5: Run boot tests**

Run: `task test` (or targeted: `go test ./internal/command/handlers/ -run TestBoot -v`)
Expected: PASS

- [ ] **Step 6: Commit**

```text
jj describe -m "refactor(boot): use SessionAccess + ErrSessionEnded for self-boot

Self-boot returns ErrSessionEnded like quit.
Admin-boot calls DeleteByCharacter directly."
jj new
```

---

## Chunk 4: Migrate CoreServer and Remove Dead Code

### Task 10: Update `executeViaDispatcher` — sentinel error replaces `hadSessionBefore`

**Files:**

- Modify: `internal/grpc/server.go`

- [ ] **Step 1: Remove `hadSessionBefore` guard, add sentinel check**

In `executeViaDispatcher` (around line 408), remove:

```go
hadSessionBefore := s.sessions.GetSession(info.CharacterID) != nil
```

After the `dispatchErr` check and output emit, replace the post-quit cleanup block (lines 462-476) with:

```go
// Quit/self-boot detection: handler signals intent, server does teardown.
if errors.Is(dispatchErr, command.ErrSessionEnded) {
	if dcErr := s.engine.HandleDisconnect(ctx, char, "quit"); dcErr != nil {
		slog.WarnContext(ctx, "leave event failed", "error", dcErr)
	}
	if delErr := s.sessionStore.Delete(ctx, info.ID, "Goodbye!"); delErr != nil {
		slog.WarnContext(ctx, "session delete failed", "error", delErr)
	}
	s.runDisconnectHooks(ctx, *info)
	return nil
}
```

Also update the `isError` check for output emit:

```go
isError := dispatchErr != nil && !errors.Is(dispatchErr, command.ErrSessionEnded)
```

Add `"errors"` to imports if not already present.

- [ ] **Step 2: Commit**

```text
jj describe -m "refactor(grpc): replace hadSessionBefore guard with ErrSessionEnded sentinel

Quit and self-boot handlers return ErrSessionEnded.
executeViaDispatcher catches the sentinel and performs
server-side teardown (leave event, PG delete, hooks)."
jj new
```

### Task 10b: Add test for `ErrSessionEnded` in `executeViaDispatcher`

**Files:**

- Modify: `internal/grpc/dispatcher_test.go`

- [ ] **Step 1: Write test for quit sentinel detection**

Add a test that:
1. Creates a CoreServer with a dispatcher that has a quit handler returning `ErrSessionEnded`
2. Calls `HandleCommand` with "quit"
3. Verifies the command response event contains "Goodbye!"
4. Verifies the session is deleted from `sessionStore`
5. Verifies the leave event was emitted

This is the most critical behavioral change — the sentinel replaces the `hadSessionBefore` guard. The test proves the quit flow works end-to-end through the dispatcher.

- [ ] **Step 2: Run the test**

Run: `task test`
Expected: PASS

- [ ] **Step 3: Commit**

```text
jj describe -m "test(grpc): add test for ErrSessionEnded sentinel in executeViaDispatcher"
jj new
```

### Task 11: Remove `CoreServer.sessions` field and all in-memory session calls

**Files:**

- Modify: `internal/grpc/server.go`
- Modify: `internal/grpc/auth_handlers.go`

- [ ] **Step 1: Remove `sessions` field from `CoreServer` struct**

In `server.go`, remove:

```go
sessions        *core.SessionManager
```

- [ ] **Step 2: Update `NewCoreServer` constructor**

Change signature from:

```go
func NewCoreServer(engine *core.Engine, sessions *core.SessionManager, sessionStore session.Store, opts ...CoreServerOption) *CoreServer {
```

to:

```go
func NewCoreServer(engine *core.Engine, sessionStore session.Store, opts ...CoreServerOption) *CoreServer {
```

Remove `sessions: sessions,` from the struct literal.

- [ ] **Step 3: Remove `s.sessions.Connect()` calls in `auth_handlers.go`**

There are 3 call sites (lines 205, 257, and server.go:285). Remove each `s.sessions.Connect(...)` line. The session is already created via `sessionStore.Set` + `sessionStore.AddConnection` in the same code paths.

- [ ] **Step 4: Remove `s.sessions.Disconnect()` calls in `server.go`**

Remove from:
- `executeViaSwitch` quit path (line 528): `s.sessions.Disconnect(info.CharacterID, ulid.ULID{})`
- Disconnect RPC guest path (line 977): `s.sessions.Disconnect(info.CharacterID, ulid.ULID{})`
- Disconnect RPC detach path (line 1004): `s.sessions.Disconnect(info.CharacterID, ulid.ULID{})`

- [ ] **Step 5: Remove `s.sessions.EndSession()` call in `server.go`**

Remove from Disconnect RPC guest path (line 978): `if err := s.sessions.EndSession(info.CharacterID); err != nil { ... }`

- [ ] **Step 6: Remove `s.sessions.UpdateCursor()` call in `server.go`**

Remove from Subscribe replay loop (line 702): `s.sessions.UpdateCursor(info.CharacterID, ev.Stream, ev.ID)`

The `persistCursorAsync` call on the next line already writes cursors to PG.

- [ ] **Step 7: Remove `s.sessions.Connect()` call in `Authenticate` (server.go:285)**

Already covered in step 3 but verify this specific one in `server.go`.

- [ ] **Step 8: Remove `core` import if no longer used**

Check if `internal/grpc/server.go` still imports `core` for anything besides `SessionManager`. It still uses `core.Engine`, `core.EventStore`, `core.CharacterRef`, `core.NewULID`, `core.Event*` — so keep the import.

- [ ] **Step 9: Commit**

```text
jj describe -m "refactor(grpc): remove CoreServer.sessions field and all in-memory session calls

All session state now flows exclusively through sessionStore (Postgres).
Connection tracking, cursors, and lifecycle managed by session.Store."
jj new
```

### Task 12: Remove Engine session dependency

**Files:**

- Modify: `internal/core/engine.go`
- Modify: `internal/core/engine_test.go`

- [ ] **Step 1: Remove `sessions` field from Engine**

```go
type Engine struct {
	store    EventStore
	sessions *SessionManager
}
```

becomes:

```go
type Engine struct {
	store EventStore
}
```

- [ ] **Step 2: Update `NewEngine` constructor**

```go
func NewEngine(store EventStore, sessions *SessionManager) *Engine {
	return &Engine{
		store:    store,
		sessions: sessions,
	}
}
```

becomes:

```go
func NewEngine(store EventStore) *Engine {
	return &Engine{store: store}
}
```

- [ ] **Step 3: Remove `ReplayEvents` method**

Delete the entire `ReplayEvents` method (lines 150-162).

- [ ] **Step 4: Update engine tests**

In `engine_test.go`:
- Remove `TestEngine_ReplayEvents`, `TestEngine_ReplayEvents_WithCursor`, `TestEngine_ReplayEvents_StoreError`
- Update all `NewEngine(store, sessions)` calls to `NewEngine(store)` — remove the `sessions` argument

- [ ] **Step 5: Run engine tests**

Run: `task test` (or targeted: `go test ./internal/core/ -v`)
Expected: PASS

- [ ] **Step 6: Commit**

```text
jj describe -m "refactor(engine): remove session dependency and dead ReplayEvents method

Engine is now a pure event emitter. ReplayEvents had no production
callers — the Subscribe handler reads cursors from session.Store directly."
jj new
```

### Task 13: Delete `core.SessionManager`, `core.Session`, `core.SessionService`

**Files:**

- Delete contents from: `internal/core/session.go`

- [ ] **Step 1: Delete `internal/core/session.go`**

Remove the entire file. It contains:
- `Session` struct
- `SessionService` interface
- `copySession` helper
- `SessionManager` struct and all methods

- [ ] **Step 2: Update `cmd/holomush/core.go`**

Remove:

```go
sessions := core.NewSessionManager()
```

Change:

```go
engine := core.NewEngine(realStore, sessions)
```

to:

```go
engine := core.NewEngine(realStore)
```

Change:

```go
holoGRPC.NewCoreServer(engine, sessions, sessionStore, ...)
```

to:

```go
holoGRPC.NewCoreServer(engine, sessionStore, ...)
```

Change:

```go
Session:    sessions,
```

to:

```go
Session:    sessionStore,
```

(in the `command.ServicesConfig` struct literal)

- [ ] **Step 3: Run full test suite**

Run: `task test`
Expected: PASS. If there are compilation errors, they indicate missed callers — fix them.

- [ ] **Step 4: Commit**

```text
jj describe -m "refactor(core): delete SessionManager, Session, SessionService

The in-memory session system is fully replaced by session.Store (Postgres).
All callers migrated in prior commits."
jj new
```

---

## Chunk 5: Update Tests

### Task 14: Update CoreServer tests

**Files:**

- Modify: `internal/grpc/server_test.go`
- Modify: `internal/grpc/dispatcher_test.go`
- Modify: `internal/grpc/auth_handlers_test.go`

- [ ] **Step 1: Update `NewCoreServer` calls in tests**

All test files that call `NewCoreServer` pass `sessions *core.SessionManager` as the second argument. Remove this argument from all call sites.

Search: `rg "NewCoreServer\(" internal/grpc/ --type go`

For each call, remove the `sessions` parameter (typically `core.NewSessionManager()` or a variable).

- [ ] **Step 2: Remove `core.NewSessionManager()` from test helpers**

Any test helper that creates a `SessionManager` for test setup needs updating. The tests should use `sessionStore` (the `MemStore` or mock) exclusively.

- [ ] **Step 3: Update dispatcher tests**

In `internal/grpc/dispatcher_test.go`, the `newDispatcherTestServer` helper returns
`(*CoreServer, *core.SessionManager)`. Update both the helper's return signature
(remove `*core.SessionManager`) and all callers that destructure the return value
(e.g., `server, sessions := newDispatcherTestServer(...)` → `server := newDispatcherTestServer(...)`).
Any test setup that used `sessions.Connect()` must use `sessionStore.Set()` instead.

- [ ] **Step 4: Run all gRPC tests**

Run: `task test` (or targeted: `go test ./internal/grpc/ -v`)
Expected: PASS

- [ ] **Step 5: Commit**

```text
jj describe -m "test(grpc): update CoreServer tests for removed sessions parameter"
jj new
```

### Task 15: Update integration tests

**Files:**

- Modify: `test/integration/session/session_persistence_integration_test.go`
- Modify: `test/integration/telnet/e2e_test.go`
- Modify: `test/integration/phase1_5_test.go`

These files create `core.NewSessionManager()` and pass it to `core.NewEngine()` and
`NewCoreServer()`. They must be updated after the constructor signatures change.

- [ ] **Step 1: Find all integration test callers**

```bash
rg "core\.NewSessionManager\(\)|core\.NewEngine\(.*sessions" test/ --type go -n
```

- [ ] **Step 2: Update each file**

For each file:
- Remove `sessions := core.NewSessionManager()` (or equivalent)
- Change `core.NewEngine(store, sessions)` to `core.NewEngine(store)`
- Change `NewCoreServer(engine, sessions, sessionStore, ...)` to `NewCoreServer(engine, sessionStore, ...)`

- [ ] **Step 3: Run integration tests**

Run: `task test:int`
Expected: PASS

- [ ] **Step 4: Commit**

```text
jj describe -m "test(integration): update integration tests for removed SessionManager parameter"
jj new
```

---

## Chunk 6: Final Verification

### Task 16: Full test suite and lint

- [ ] **Step 1: Run full test suite**

Run: `task test`
Expected: ALL PASS

- [ ] **Step 2: Run linter**

Run: `task lint`
Expected: No new warnings. Fix any issues.

- [ ] **Step 3: Run formatter**

Run: `task fmt`

- [ ] **Step 4: Verify no remaining references to removed types**

```bash
rg "core\.SessionManager|core\.SessionService|core\.Session\b|core\.NewSessionManager|\.ListActiveSessions\(\)" internal/ cmd/ --type go
```

Expected: No matches (only docs/specs/plans may reference them).

- [ ] **Step 5: Verify `session.go` has no `core` import cycle**

The `session` package must not import `core`. Verify:

```bash
rg '"github.com/holomush/holomush/internal/core"' internal/session/
```

Expected: No matches.

- [ ] **Step 6: Run integration tests**

Run: `task test:int`
Expected: PASS

- [ ] **Step 7: Run E2E tests**

Run: `task test:e2e`
Expected: PASS

- [ ] **Step 8: Final commit (if any fmt/lint fixes)**

```text
jj describe -m "chore: fix lint and formatting after session consolidation"
jj new
```

### Task 17: Close bead and clean up

- [ ] **Step 1: Close the bead**

```bash
bd close holomush-a3a7.9 --reason="Consolidated in-memory SessionManager into Postgres session.Store. Removed core.SessionManager, core.Session, core.SessionService, Engine.sessions, hadSessionBefore guard. Added SessionAccess interface, ErrSessionEnded sentinel."
```

- [ ] **Step 2: Create follow-up bead for boot leave event gap**

```bash
bd create --title="Emit leave event when admin-booting a player" --description="When an admin boots another player, no leave event is emitted for the target. The boot handler calls DeleteByCharacter which triggers STREAM_CLOSED, but the leave event (Engine.HandleDisconnect) is only called in the quit/self-boot path via ErrSessionEnded. This is a pre-existing gap that predates the session consolidation." --type=bug --priority=3
```

- [ ] **Step 3: Set bookmark and prepare for PR**

```bash
jj bookmark set session-consolidation -r @-
```
