# Multi-Tab Session Isolation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix `holomush-9q8n` — opening a second guest tab in the same browser silently breaks the first tab. Second guest creation overwrites the shared `holomush_session` cookie, leaving the first tab's commands rejected by `ValidateSessionOwnership`.

**Architecture:** Keep the HttpOnly cookie as the only credential (no XSS exposure increase). Make `WebCreateGuest`, `WebAuthenticatePlayer`, and `WebCreatePlayer` refuse with `ALREADY_AUTHENTICATED` when called with a valid cookie (gateway-only check). Extend the existing `CheckPlayerSession` / `WebCheckSession` success response with `player_id` / `is_guest` / `characters` so the landing/login/register pages can render an authenticated branch. Multi-tab support for both same-character (reattach + multi-connection Subscribe) and different-character (one session per character) is already implemented in the architecture — the spec's claim is that we are no longer breaking it.

**Tech Stack:** Go 1.22+ (gRPC + Connect-Go), protobuf (Buf-managed), SvelteKit 5 (Svelte runes), TypeScript, gopher-lua (unaffected here), gotestsum, Ginkgo/Gomega for integration tests, Playwright for E2E (lives at `web/e2e/`).

**Spec:** `docs/superpowers/specs/2026-04-25-multi-tab-session-isolation-design.md` (v3, change `mw 04`).

**Plan-review fixes from v1:** v2 reflects the plan-reviewer's blocking findings (mockery → hand-rolled `mockCoreClient`, `web/e2e/` not `test/e2e/playwright/`, no `@testing-library/svelte`, unconditional `error_code` proto fields, direct ConnectRPC handler invocation for integration tests instead of an invented `WithSameCookieJar`, explicit client-side §4.4.4 pre-gate, TTL refresh grounded in `internal/grpc/auth_handlers.go:121`, etc.).

---

## File Structure

| Layer | Files | Responsibility |
| --- | --- | --- |
| Proto | `api/proto/holomush/core/v1/core.proto` | Extend `CheckPlayerSessionResponse` (additive: `player_id`, `is_guest`, `characters`). |
| Proto | `api/proto/holomush/web/v1/web.proto` | Extend `WebCheckSessionResponse` (same additions). Extend `WebCreateGuestResponse`, `WebAuthenticatePlayerResponse`, `WebCreatePlayerResponse` with `current_player_name` and (new) `error_code`. |
| Generated | `pkg/proto/...`, `web/src/lib/connect/...` | Regenerated via `task proto`. |
| Core RPC | `internal/grpc/auth_handlers.go` | Extend `CheckPlayerSession` to populate the new fields on the success path (TTL refresh inherited from `resolvePlayerSession` at line 121). **No** changes to `CreateGuest`, `AuthenticatePlayer`, `CreatePlayer`, `Logout`. |
| Gateway | `internal/web/auth_handlers.go` | Map new `WebCheckSessionResponse` fields. Add `checkCookieCollision` helper and apply it to `WebCreateGuest`, `WebAuthenticatePlayer`, `WebCreatePlayer`. |
| Auth state | `web/src/lib/stores/authStore.ts` | Extend `AuthState` with `playerId`, `isGuest`, `characters`. |
| Routing | `web/src/routes/+page.svelte` (modify), `web/src/routes/+page.ts` (create), `web/src/routes/login/+page.svelte` (modify), `web/src/routes/login/+page.ts` (create), `web/src/routes/register/+page.svelte` (modify), `web/src/routes/register/+page.ts` (create) | Two-branch render gated on `webCheckSession()` via SvelteKit `load()`. |
| Authed gate | `web/src/routes/(authed)/+layout.ts` | Read additive new fields into the auth store on success. Failure path (throw → redirect) unchanged. |
| Terminal | `web/src/routes/(authed)/terminal/+page.svelte`, `web/src/lib/backfill/streamBackfill.ts` | Uniform `SESSION_NOT_FOUND` handling at the four call sites in spec §4.4.5. |
| Tests | `internal/grpc/auth_handlers_test.go`, `internal/web/auth_handlers_test.go`, `internal/web/handler_test.go` (extend `mockCoreClient`), `test/integration/auth/multi_tab_test.go` (new) | Coverage per spec §5. |
| E2E | `web/e2e/multi-tab-session.spec.ts` (new) | Cmux/Playwright two-tab repro guard. |

Each task makes self-contained changes that pass `task lint` and `task test` independently. **Every "Commit" step is preceded by `task lint` and `task fmt`.**

---

## Test setup conventions (read once, apply to every Go test in this plan)

This section ground-truths the test patterns so the per-task code blocks are uniform and compile-ready.

**Web gateway tests (`internal/web/auth_handlers_test.go`):** the package uses a hand-rolled mock `mockCoreClient` defined at `internal/web/handler_test.go:36-97`. Tests assemble it as `client := &mockCoreClient{...}; h := NewHandler(client)` and call handlers directly. There are no `EXPECT()` / `Once()` / `mock.Anything` patterns. To support this plan we extend the mock with one captured-request pointer and four call counters. **Counters use `atomic.Int32`** (not plain `int`) so the concurrent test in Task 7 step 5 doesn't trip the race detector under `-race -count=1`:

```go
// In internal/web/handler_test.go, inside `type mockCoreClient struct { ... }`
// Add `import "sync/atomic"` to the file's import block.
checkSessionReq    *corev1.CheckPlayerSessionRequest // captured for assertion
checkSessionCalls  atomic.Int32                      // call counter; atomic for use under -race
createGuestCalls   atomic.Int32
authPlayerCalls    atomic.Int32
createPlayerCalls  atomic.Int32
```

Test assertions read counters via `.Load()`: e.g. `assert.Equal(t, int32(1), client.checkSessionCalls.Load())`, `assert.Equal(t, int32(0), client.createGuestCalls.Load(), "CreateGuest MUST NOT be called")`. Mutations use `.Add(1)`.

…and update the corresponding methods (`CheckPlayerSession`, `CreateGuest`, `AuthenticatePlayer`, `CreatePlayer`) to capture the request and increment the counter via `Add(1)`:

```go
func (m *mockCoreClient) CheckPlayerSession(_ context.Context, req *corev1.CheckPlayerSessionRequest) (*corev1.CheckPlayerSessionResponse, error) {
    m.checkSessionReq = req
    m.checkSessionCalls.Add(1)
    return m.checkSessionResp, m.checkSessionErr
}
// Similarly: m.createGuestCalls.Add(1), m.authPlayerCalls.Add(1), m.createPlayerCalls.Add(1).
```

This extension is part of Task 0 below.

**Core handler tests (`internal/grpc/auth_handlers_test.go`):** the package uses mockery-generated mocks (see `internal/auth/mocks/`) and constructs `&CoreServer{...}` literals. Look at the existing `TestAuthenticatePlayer*` tests for the pattern — they wire `playerSessionRepo`, `playerRepo`, `charRepo`, `authService` etc. as testify mocks via `mocks.NewMockPlayerSessionRepository(t)`.

**Web component tests:** the project does NOT have `@testing-library/svelte` installed (verified against `web/package.json`). Component-level UI assertions for Tasks 11/12/13 are done in **Playwright** (Task 24), not Vitest component tests. Vitest is used for plain TypeScript unit tests like `authStore.test.ts`.

**Integration tests (`test/integration/auth/`):** the suite at `test/integration/auth/auth_suite_test.go` builds `auth.Service` directly. For multi-tab scenarios that need ConnectRPC handlers and a shared "cookie token", we use **direct handler invocation with a shared `X-Session-Token` header** rather than spinning up an HTTP listener + cookie jar. This is appropriate because the cookie middleware (`internal/web/cookie.go`) translates cookie → header before the handler sees it; testing the handler with the header set is equivalent to testing it with the cookie present, modulo the middleware itself which is already covered by `internal/web/cookie_test.go`. The actual cookie-write behavior is verified end-to-end by Task 24.

**Running narrow integration tests:** `task test:int` runs the whole suite (it doesn't take `{{.CLI_ARGS}}` package args — verified against `Taskfile.yaml:93-111`). For iteration during development, run the single package directly: `go test -race -tags=integration -count=1 -v ./test/integration/auth/...`. Final gate is still `task test:int` (and `task pr-prep`).

**License headers:** every new `.go` / `.ts` / `.svelte` / `.proto` file MUST start with the SPDX header per the project CLAUDE.md. Each Task that creates a new file shows the header explicitly.

---

## Task 0: Extend `mockCoreClient` with request capture + call counters

**Files:**

- Modify: `internal/web/handler_test.go` (struct definition at lines 36-97 + receiver methods)

- [ ] **Step 1: Add the four new fields**

In the `mockCoreClient` struct, add (group them near the related `*Resp`/`*Err` fields for readability):

```go
checkSessionReq   *corev1.CheckPlayerSessionRequest // captured for assertion
checkSessionCalls int                               // call counter
createGuestCalls  int
authPlayerCalls   int
createPlayerCalls int
```

- [ ] **Step 2: Update the four receiver methods**

Find `func (m *mockCoreClient) CheckPlayerSession`, `CreateGuest`, `AuthenticatePlayer`, `CreatePlayer` in the same file. Update each to capture and count:

```go
func (m *mockCoreClient) CheckPlayerSession(_ context.Context, req *corev1.CheckPlayerSessionRequest) (*corev1.CheckPlayerSessionResponse, error) {
    m.checkSessionReq = req
    m.checkSessionCalls++
    return m.checkSessionResp, m.checkSessionErr
}

func (m *mockCoreClient) CreateGuest(_ context.Context, req *corev1.CreateGuestRequest) (*corev1.CreateGuestResponse, error) {
    m.createGuestCalls++
    // existing body — preserve any req-capture and resp/err return
    return m.createGuestResp, m.createGuestErr
}

func (m *mockCoreClient) AuthenticatePlayer(_ context.Context, req *corev1.AuthenticatePlayerRequest) (*corev1.AuthenticatePlayerResponse, error) {
    m.authPlayerCalls++
    return m.authPlayerResp, m.authPlayerErr
}

func (m *mockCoreClient) CreatePlayer(_ context.Context, req *corev1.CreatePlayerRequest) (*corev1.CreatePlayerResponse, error) {
    m.createPlayerCalls++
    return m.createPlayerResp, m.createPlayerErr
}
```

If those methods already capture the request into existing `*Req` fields, preserve that behavior — only ADD the counter increment.

- [ ] **Step 3: Verify existing tests still compile and pass**

Run: `task test -- ./internal/web/`
Expected: PASS — pure additive change to the mock.

- [ ] **Step 4: Lint, then commit**

```bash
task fmt && task lint
```

Commit message:

```text
test(web): extend mockCoreClient with request capture and call counters

Adds checkSessionReq + checkSessionCalls/createGuestCalls/authPlayerCalls/
createPlayerCalls fields. Used by upcoming cookie-collision-gate tests
that need "exactly N calls" / "MUST NOT be called" assertions.

Refs holomush-9q8n.
```

---

## Phase 1 — Proto + core handler (additive only)

### Task 1: Extend core `CheckPlayerSessionResponse` proto

**Files:**

- Modify: `api/proto/holomush/core/v1/core.proto:320-322`

- [ ] **Step 1: Edit the proto**

Replace `message CheckPlayerSessionResponse { string player_name = 1; }` with:

```proto
message CheckPlayerSessionResponse {
  string player_name = 1;
  // NEW (additive on the success path; failure path still returns nil, err
  // so these fields are absent on PLAYER_SESSION_NOT_FOUND / PLAYER_SESSION_EXPIRED
  // — preserves the enumeration-safety contract documented at
  // internal/auth/session_ownership.go:18-20).
  string player_id = 2;
  bool is_guest = 3;
  repeated CharacterSummary characters = 4;
}
```

- [ ] **Step 2: Lint, regen, verify build**

```bash
task lint:proto && task proto && task test -- ./internal/grpc/...
```

Expected: PASS — existing tests still green because new fields default to zero.

- [ ] **Step 3: Lint full + commit**

```bash
task fmt && task lint
```

```text
feat(proto): extend CheckPlayerSessionResponse with player_id, is_guest, characters

Additive on the success path. Failure path still returns nil, err — the
new fields are unset on PLAYER_SESSION_NOT_FOUND / PLAYER_SESSION_EXPIRED.
Enumeration-safety contract preserved.

Refs holomush-9q8n.
```

---

### Task 2: Extend `WebCheckSessionResponse` proto

**Files:**

- Modify: `api/proto/holomush/web/v1/web.proto:230-234`

- [ ] **Step 1: Edit the proto**

Replace `message WebCheckSessionResponse { string player_name = 1; }` with:

```proto
message WebCheckSessionResponse {
  string player_name = 1;
  // NEW (additive on the success path; failure path still returns
  // connect.CodeUnauthenticated so web/src/routes/(authed)/+layout.ts:18-25
  // continues to redirect on throw — no contract break).
  string player_id = 2;
  bool is_guest = 3;
  repeated CharacterSummary characters = 4;
}
```

- [ ] **Step 2: Lint + regen + verify build**

```bash
task lint:proto && task proto && task test -- ./internal/web/...
```

Expected: all PASS.

- [ ] **Step 3: Lint full + commit**

```bash
task fmt && task lint
```

```text
feat(proto): extend WebCheckSessionResponse with player_id, is_guest, characters

Additive on the success path; the connect.CodeUnauthenticated failure
contract is unchanged so web/src/routes/(authed)/+layout.ts:18-25 keeps
working without modification.

Refs holomush-9q8n.
```

---

### Task 3: Add `current_player_name` and `error_code` to the three create/auth response messages

**Files:**

- Modify: `api/proto/holomush/web/v1/web.proto` — `WebAuthenticatePlayerResponse`, `WebCreatePlayerResponse`, `WebCreateGuestResponse`

The repo verification confirmed: none of these three messages currently has an `error_code` field. We add both `current_player_name` and `error_code` unconditionally with the field numbers below.

- [ ] **Step 1: Edit `WebAuthenticatePlayerResponse`**

Replace:

```proto
message WebAuthenticatePlayerResponse {
  bool success = 1;
  string error_message = 3;
  repeated CharacterSummary characters = 4;
  string default_character_id = 5;
}
```

with:

```proto
message WebAuthenticatePlayerResponse {
  bool success = 1;
  // field 2 is reserved (was player_session_token before the cookie cutover)
  string error_message = 3;
  repeated CharacterSummary characters = 4;
  string default_character_id = 5;
  // NEW: machine-readable error code. Values: "" on success, "ALREADY_AUTHENTICATED"
  // when the cookie-collision gate fires, others reserved for future use.
  string error_code = 6;
  // NEW: populated only when error_code = "ALREADY_AUTHENTICATED". Holds the
  // existing player's display name so the client renders the right
  // "you are already signed in as X" UI without a second round trip.
  string current_player_name = 7;
}
```

- [ ] **Step 2: Edit `WebCreatePlayerResponse`**

Replace:

```proto
message WebCreatePlayerResponse {
  bool success = 1;
  repeated CharacterSummary characters = 3;
  string error_message = 4;
}
```

with:

```proto
message WebCreatePlayerResponse {
  bool success = 1;
  // field 2 is reserved (was player_session_token)
  repeated CharacterSummary characters = 3;
  string error_message = 4;
  // NEW: see WebAuthenticatePlayerResponse for semantics.
  string error_code = 5;
  string current_player_name = 6;
}
```

- [ ] **Step 3: Edit `WebCreateGuestResponse`**

Replace:

```proto
message WebCreateGuestResponse {
  bool success = 1;
  string error_message = 2;
  repeated CharacterSummary characters = 3;
  string default_character_id = 4;
}
```

with:

```proto
message WebCreateGuestResponse {
  bool success = 1;
  string error_message = 2;
  repeated CharacterSummary characters = 3;
  string default_character_id = 4;
  // NEW: see WebAuthenticatePlayerResponse for semantics.
  string error_code = 5;
  string current_player_name = 6;
}
```

- [ ] **Step 4: Lint + regen + verify build**

```bash
task lint:proto && task proto && task test -- ./internal/web/...
```

Expected: all PASS.

- [ ] **Step 5: Lint full + commit**

```bash
task fmt && task lint
```

```text
feat(proto): add error_code + current_player_name to web auth responses

WebAuthenticatePlayerResponse, WebCreatePlayerResponse, WebCreateGuestResponse
gain `error_code` (machine-readable) and `current_player_name`, populated
only when the gateway short-circuits with ALREADY_AUTHENTICATED. Field
numbers chosen as the next available slot per message; no collisions.

Refs holomush-9q8n.
```

---

### Task 4: Populate the new fields in core `CheckPlayerSession`

**Files:**

- Modify: `internal/grpc/auth_handlers.go:506-527` (`CheckPlayerSession`)
- Test: `internal/grpc/auth_handlers_test.go`

Note: `resolvePlayerSession` at `internal/grpc/auth_handlers.go:111-123` already calls `RefreshTTL` (line 121, best-effort). The new code reuses `resolvePlayerSession`, so TTL refresh on the success path is automatic — no extra call needed in the handler.

- [ ] **Step 1: Locate the existing `CheckPlayerSession` test harness**

```bash
rg -n 'TestCheckPlayerSession|CheckPlayerSession.*test' internal/grpc/auth_handlers_test.go
```

Note the helper functions used to construct the `CoreServer` (e.g. `newAuthTestServer`, `setupCoreServerForAuth`, or whatever the file uses). If no existing `CheckPlayerSession` test exists, model the new test on the closest neighbor (e.g. an `AuthenticatePlayer` test) which already constructs `&CoreServer{...}` with mockery-generated repo mocks.

- [ ] **Step 2: Write the failing test**

Append to `internal/grpc/auth_handlers_test.go`:

```go
func TestCheckPlayerSessionPopulatesPlayerIDIsGuestAndCharactersOnSuccess(t *testing.T) {
    ctx := context.Background()

    // Construct the CoreServer the same way an existing AuthenticatePlayer
    // test in this file does. The helper name and exact wiring may differ;
    // match whatever the neighbor test uses.
    server, deps := newAuthTestCoreServer(t)
    // deps holds the mockery mocks (playerSessionRepo, playerRepo, charRepo, etc.)

    // Arrange: a guest player with a default character.
    playerID := ulid.Make()
    sessionID := ulid.Make()
    rawToken := "raw-token-stub"
    tokenHash := auth.HashSessionToken(rawToken)

    deps.playerSessionRepo.EXPECT().
        GetByTokenHash(mock.Anything, tokenHash).
        Return(&auth.PlayerSession{
            ID: sessionID, PlayerID: playerID, ExpiresAt: time.Now().Add(time.Hour),
        }, nil)
    deps.playerSessionRepo.EXPECT().
        RefreshTTL(mock.Anything, sessionID, mock.Anything).
        Return(nil) // best-effort; handler ignores the error
    deps.playerRepo.EXPECT().
        GetByID(mock.Anything, playerID).
        Return(&auth.Player{ID: playerID, Username: "Jasper Iodine", IsGuest: true}, nil)
    deps.charRepo.EXPECT().
        ListByPlayer(mock.Anything, playerID).
        Return([]*world.Character{{ID: ulid.Make(), Name: "Jasper Iodine", PlayerID: playerID}}, nil)

    // Act
    resp, err := server.CheckPlayerSession(ctx, &corev1.CheckPlayerSessionRequest{
        PlayerSessionToken: rawToken,
    })

    // Assert
    require.NoError(t, err)
    assert.Equal(t, "Jasper Iodine", resp.GetPlayerName())
    assert.Equal(t, playerID.String(), resp.GetPlayerId())
    assert.True(t, resp.GetIsGuest())
    require.Len(t, resp.GetCharacters(), 1)
    assert.Equal(t, "Jasper Iodine", resp.GetCharacters()[0].GetCharacterName())
}

func TestCheckPlayerSessionFailureContractUnchanged(t *testing.T) {
    ctx := context.Background()
    server, deps := newAuthTestCoreServer(t)

    deps.playerSessionRepo.EXPECT().
        GetByTokenHash(mock.Anything, mock.Anything).
        Return(nil, oops.Code("PLAYER_SESSION_NOT_FOUND").Errorf("unknown token"))

    resp, err := server.CheckPlayerSession(ctx, &corev1.CheckPlayerSessionRequest{
        PlayerSessionToken: "bad-token",
    })

    assert.Nil(t, resp, "failure path returns nil response")
    require.Error(t, err)
    oopsErr, ok := oops.AsOops(err)
    require.True(t, ok)
    code, _ := oopsErr.Code().(string)
    assert.Equal(t, "PLAYER_SESSION_NOT_FOUND", code)
}
```

If `newAuthTestCoreServer` doesn't exist, replicate the neighbor pattern inline: construct `&CoreServer{...}` with the mockery mocks the neighbor uses, and return them as a `*authTestDeps` struct of your own creation (single-use is fine; or extract a helper that subsequent tasks reuse).

- [ ] **Step 3: Run, verify failure**

```bash
task test -- -run TestCheckPlayerSessionPopulatesPlayerIDIsGuestAndCharactersOnSuccess ./internal/grpc/
```

Expected: FAIL — `resp.GetPlayerId()` is empty, `resp.GetCharacters()` is empty.

- [ ] **Step 4: Implement**

Replace the existing `CheckPlayerSession` body (currently `internal/grpc/auth_handlers.go:506-527`) with:

```go
// CheckPlayerSession validates a player session token and returns the player +
// characters. Failure-path contract (nil, err with PLAYER_SESSION_NOT_FOUND /
// PLAYER_SESSION_EXPIRED) is preserved exactly. See spec §4.3 / §4.3.1.
func (s *CoreServer) CheckPlayerSession(ctx context.Context, req *corev1.CheckPlayerSessionRequest) (*corev1.CheckPlayerSessionResponse, error) {
    slog.DebugContext(ctx, "grpc: CheckPlayerSession")

    // resolvePlayerSession (line 111-123) does the token-hash lookup and the
    // best-effort RefreshTTL — TTL refresh on the success path is inherited.
    ps, err := s.resolvePlayerSession(ctx, req.GetPlayerSessionToken())
    if err != nil {
        return nil, err
    }

    if s.playerRepo == nil {
        return nil, oops.Code("NOT_CONFIGURED").Errorf("player repository not configured")
    }

    player, err := s.playerRepo.GetByID(ctx, ps.PlayerID)
    if err != nil {
        return nil, oops.Code("PLAYER_LOOKUP_FAILED").Wrap(err)
    }

    characters, err := s.buildCharacterSummaries(ctx, player.ID)
    if err != nil {
        return nil, oops.Code("CHARACTER_LOOKUP_FAILED").Wrap(err)
    }

    return &corev1.CheckPlayerSessionResponse{
        PlayerName: player.Username,
        PlayerId:   player.ID.String(),
        IsGuest:    player.IsGuest,
        Characters: characters,
    }, nil
}
```

`buildCharacterSummaries` is defined at `internal/grpc/auth_handlers.go:684-685` (or thereabouts — verify with `rg -n 'buildCharacterSummaries' internal/grpc/auth_handlers.go`) and is already used by `AuthenticatePlayer`. Reuse it directly.

- [ ] **Step 5: Run, verify pass**

```bash
task test -- -run TestCheckPlayerSession ./internal/grpc/
```

Expected: both new tests PASS; all neighbor `CheckPlayerSession`-touching tests still PASS.

- [ ] **Step 6: Run the full grpc package**

```bash
task test -- ./internal/grpc/...
```

Expected: PASS.

- [ ] **Step 7: Lint + commit**

```bash
task fmt && task lint
```

```text
feat(core): populate player_id, is_guest, characters in CheckPlayerSession

Success-path additive only. Failure-path nil, err with
PLAYER_SESSION_NOT_FOUND / PLAYER_SESSION_EXPIRED is preserved verbatim,
inheriting the enumeration-safety contract documented at
internal/auth/session_ownership.go:18-20. TTL refresh on success is
inherited from resolvePlayerSession (auth_handlers.go:121).

Refs holomush-9q8n.
```

---

### Task 5: Map the new fields in gateway `WebCheckSession`

**Files:**

- Modify: `internal/web/auth_handlers.go:233-254` (`WebCheckSession`)
- Test: `internal/web/auth_handlers_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/web/auth_handlers_test.go`:

```go
func TestWebCheckSessionForwardsPlayerIDIsGuestAndCharacters(t *testing.T) {
    client := &mockCoreClient{
        checkSessionResp: &corev1.CheckPlayerSessionResponse{
            PlayerName: "Jasper Iodine",
            PlayerId:   "01KQ2Y5ETK5957724MGZ2H2TDB",
            IsGuest:    true,
            Characters: []*corev1.CharacterSummary{
                {CharacterId: "01KQ2Y5ETW03KJ0HKCQ07ASYF2", CharacterName: "Jasper Iodine"},
            },
        },
    }
    h := NewHandler(client)

    req := connect.NewRequest(&webv1.WebCheckSessionRequest{})
    req.Header().Set(headerInjectSessionToken, "valid-token")

    resp, err := h.WebCheckSession(context.Background(), req)
    require.NoError(t, err)
    assert.Equal(t, "Jasper Iodine", resp.Msg.GetPlayerName())
    assert.Equal(t, "01KQ2Y5ETK5957724MGZ2H2TDB", resp.Msg.GetPlayerId())
    assert.True(t, resp.Msg.GetIsGuest())
    require.Len(t, resp.Msg.GetCharacters(), 1)
    assert.Equal(t, "01KQ2Y5ETW03KJ0HKCQ07ASYF2", resp.Msg.GetCharacters()[0].GetCharacterId())
}

func TestWebCheckSessionFailureContractUnchanged(t *testing.T) {
    client := &mockCoreClient{
        checkSessionErr: oops.Code("PLAYER_SESSION_NOT_FOUND").Errorf("expired"),
    }
    h := NewHandler(client)

    req := connect.NewRequest(&webv1.WebCheckSessionRequest{})
    req.Header().Set(headerInjectSessionToken, "expired-token")

    _, err := h.WebCheckSession(context.Background(), req)
    require.Error(t, err)
    var connectErr *connect.Error
    require.True(t, errors.As(err, &connectErr))
    assert.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}
```

- [ ] **Step 2: Run, verify failure**

```bash
task test -- -run TestWebCheckSession ./internal/web/
```

Expected: the success-mapping test FAILS (today's gateway only forwards `PlayerName`); the failure-contract test should already pass against the existing handler.

- [ ] **Step 3: Update the gateway handler**

Find the existing `WebCheckSession` (`internal/web/auth_handlers.go:233-254`). Replace the mapping at the return statement so the new fields flow through:

```go
return connect.NewResponse(&webv1.WebCheckSessionResponse{
    PlayerName: coreResp.GetPlayerName(),
    PlayerId:   coreResp.GetPlayerId(),
    IsGuest:    coreResp.GetIsGuest(),
    Characters: convertCharacterSummariesCoreToWeb(coreResp.GetCharacters()),
}), nil
```

If a `convertCharacterSummariesCoreToWeb`-style helper already exists in the package (search `rg -n 'convertCharacter|CharacterSummary' internal/web/auth_handlers.go`), reuse it. Otherwise inline the conversion from `[]*corev1.CharacterSummary` → `[]*webv1.CharacterSummary` matching the pattern used by `WebAuthenticatePlayer` and `WebCreateGuest`.

- [ ] **Step 4: Run, verify pass**

```bash
task test -- -run TestWebCheckSession ./internal/web/
```

Expected: both tests PASS.

- [ ] **Step 5: Lint + commit**

```bash
task fmt && task lint
```

```text
feat(web): forward player_id, is_guest, characters from CheckPlayerSession

Additive — failure path (connect.CodeUnauthenticated) unchanged so
(authed)/+layout.ts still redirects on throw as today.

Refs holomush-9q8n.
```

---

## Phase 2 — Gateway cookie-collision gate

### Task 6: Add the shared `checkCookieCollision` helper

**Files:**

- Modify: `internal/web/auth_handlers.go` (add helper)
- Test: `internal/web/auth_handlers_test.go`

- [ ] **Step 1: Write the four failing tests**

Append to `internal/web/auth_handlers_test.go`:

```go
func TestCheckCookieCollisionGatedOnValidCookie(t *testing.T) {
    client := &mockCoreClient{
        checkSessionResp: &corev1.CheckPlayerSessionResponse{
            PlayerName: "Jasper Iodine",
            PlayerId:   "01KQ2Y5ETK5957724MGZ2H2TDB",
            IsGuest:    true,
        },
    }
    h := NewHandler(client)

    headers := http.Header{}
    headers.Set(headerInjectSessionToken, "valid-token")

    name, gated, err := h.checkCookieCollision(context.Background(), headers)
    require.NoError(t, err)
    assert.True(t, gated, "valid cookie MUST trip the gate")
    assert.Equal(t, "Jasper Iodine", name)
    assert.Equal(t, int32(1), client.checkSessionCalls.Load())
}

func TestCheckCookieCollisionPassesThroughOnAbsentCookie(t *testing.T) {
    client := &mockCoreClient{}
    h := NewHandler(client)

    headers := http.Header{}
    // No token header.

    name, gated, err := h.checkCookieCollision(context.Background(), headers)
    require.NoError(t, err)
    assert.False(t, gated, "absent cookie MUST NOT trip the gate")
    assert.Empty(t, name)
    assert.Equal(t, int32(0), client.checkSessionCalls.Load(), "absent cookie MUST NOT touch the core RPC")
}

func TestCheckCookieCollisionPassesThroughOnAuthFailure(t *testing.T) {
    client := &mockCoreClient{
        checkSessionErr: oops.Code("PLAYER_SESSION_NOT_FOUND").Errorf("expired or unknown"),
    }
    h := NewHandler(client)

    headers := http.Header{}
    headers.Set(headerInjectSessionToken, "expired-token")

    name, gated, err := h.checkCookieCollision(context.Background(), headers)
    require.NoError(t, err, "auth-failure errors MUST NOT propagate; they're normal-case fall-through")
    assert.False(t, gated)
    assert.Empty(t, name)
}

func TestCheckCookieCollisionSurfacesUnexpectedErrors(t *testing.T) {
    client := &mockCoreClient{
        checkSessionErr: oops.Code("PLAYER_LOOKUP_FAILED").Errorf("transport flake"),
    }
    h := NewHandler(client)

    headers := http.Header{}
    headers.Set(headerInjectSessionToken, "some-token")

    _, _, err := h.checkCookieCollision(context.Background(), headers)
    require.Error(t, err, "non-auth errors MUST surface, not silently fall through to the create path")
}
```

- [ ] **Step 2: Run, verify failure**

```bash
task test -- -run TestCheckCookieCollision ./internal/web/
```

Expected: FAIL — `checkCookieCollision` does not exist.

- [ ] **Step 3: Implement**

Add to `internal/web/auth_handlers.go` (above the existing handlers; group near `playerTokenFromHeader`):

```go
// checkCookieCollision implements the cookie-collision gate documented in
// docs/superpowers/specs/2026-04-25-multi-tab-session-isolation-design.md §4.2.0.
//
// Returns:
//   - name: the existing player's display name (only meaningful when gated=true).
//   - gated: true if the request carries a valid PlayerSession cookie and the
//     caller MUST short-circuit with ALREADY_AUTHENTICATED.
//   - err: non-nil iff the cookie validation hit an unexpected error
//     (transport / lookup-failed). Auth-failure (PLAYER_SESSION_NOT_FOUND /
//     PLAYER_SESSION_EXPIRED) is a normal case and returns gated=false, err=nil.
func (h *Handler) checkCookieCollision(ctx context.Context, headers http.Header) (string, bool, error) {
    token := headers.Get(headerInjectSessionToken)
    if token == "" {
        return "", false, nil
    }

    rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
    defer cancel()

    resp, err := h.client.CheckPlayerSession(rpcCtx, &corev1.CheckPlayerSessionRequest{
        PlayerSessionToken: token,
    })
    if err != nil {
        if isPlayerSessionAuthFailure(err) {
            return "", false, nil
        }
        return "", false, oops.Code("COOKIE_GATE_LOOKUP_FAILED").Wrap(err)
    }
    return resp.GetPlayerName(), true, nil
}

// isPlayerSessionAuthFailure reports whether err is one of the documented
// auth-failure codes that mean "cookie is invalid; treat as no-cookie".
// Unknown error types and non-string codes return false (caller surfaces).
func isPlayerSessionAuthFailure(err error) bool {
    oopsErr, ok := oops.AsOops(err)
    if !ok {
        return false
    }
    code, ok := oopsErr.Code().(string)
    if !ok {
        return false
    }
    return code == "PLAYER_SESSION_NOT_FOUND" ||
        code == "PLAYER_SESSION_EXPIRED" ||
        code == "SESSION_NOT_FOUND"
}
```

`rpcTimeout` is already defined in the package; `headerInjectSessionToken` is at `internal/web/auth_handlers.go:35`. Verify both with `rg -n 'rpcTimeout|headerInjectSessionToken' internal/web/auth_handlers.go internal/web/cookie.go`.

- [ ] **Step 4: Run, verify pass**

```bash
task test -- -run TestCheckCookieCollision ./internal/web/
```

Expected: all four PASS.

- [ ] **Step 5: Lint + commit**

```bash
task fmt && task lint
```

```text
feat(web): add cookie-collision gate helper

Shared helper used by WebCreateGuest, WebAuthenticatePlayer, and
WebCreatePlayer in subsequent tasks. Fail-closed on unexpected errors
(transport / lookup-failed); auth-failure errors fall through normally.

Refs holomush-9q8n.
```

---

### Task 7: Apply gate to `WebCreateGuest`

**Files:**

- Modify: `internal/web/auth_handlers.go` (`WebCreateGuest`)
- Test: `internal/web/auth_handlers_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/web/auth_handlers_test.go`:

```go
func TestWebCreateGuestReturnsAlreadyAuthenticatedWhenCookieValid(t *testing.T) {
    client := &mockCoreClient{
        checkSessionResp: &corev1.CheckPlayerSessionResponse{PlayerName: "Jasper Iodine"},
    }
    h := NewHandler(client)

    req := connect.NewRequest(&webv1.WebCreateGuestRequest{})
    req.Header().Set(headerInjectSessionToken, "valid-token")

    resp, err := h.WebCreateGuest(context.Background(), req)
    require.NoError(t, err, "gate hit returns success=false in body, not an RPC error")
    assert.False(t, resp.Msg.GetSuccess())
    assert.Equal(t, "ALREADY_AUTHENTICATED", resp.Msg.GetErrorCode())
    assert.Equal(t, "Jasper Iodine", resp.Msg.GetCurrentPlayerName())
    assert.Contains(t, resp.Msg.GetErrorMessage(), "Jasper Iodine")
    assert.Equal(t, int32(0), client.createGuestCalls.Load(), "CreateGuest MUST NOT be called")
}

func TestWebCreateGuestProceedsWhenCookieAbsent(t *testing.T) {
    client := &mockCoreClient{
        createGuestResp: &corev1.CreateGuestResponse{
            Success:            true,
            PlayerSessionToken: "fresh-token",
            Characters:         []*corev1.CharacterSummary{{CharacterId: "c1", CharacterName: "Alice"}},
            DefaultCharacterId: "c1",
        },
    }
    h := NewHandler(client)

    req := connect.NewRequest(&webv1.WebCreateGuestRequest{})
    // No token header.

    resp, err := h.WebCreateGuest(context.Background(), req)
    require.NoError(t, err)
    assert.True(t, resp.Msg.GetSuccess())
    assert.Empty(t, resp.Msg.GetCurrentPlayerName())
    assert.Empty(t, resp.Msg.GetErrorCode())
    assert.Equal(t, int32(1), client.createGuestCalls.Load())
    assert.Equal(t, int32(0), client.checkSessionCalls.Load(), "absent cookie short-circuits before CheckPlayerSession")
}

func TestWebCreateGuestProceedsWhenCookieExpired(t *testing.T) {
    client := &mockCoreClient{
        checkSessionErr: oops.Code("PLAYER_SESSION_EXPIRED").Errorf("expired"),
        createGuestResp: &corev1.CreateGuestResponse{Success: true, PlayerSessionToken: "fresh"},
    }
    h := NewHandler(client)

    req := connect.NewRequest(&webv1.WebCreateGuestRequest{})
    req.Header().Set(headerInjectSessionToken, "expired-token")

    resp, err := h.WebCreateGuest(context.Background(), req)
    require.NoError(t, err)
    assert.True(t, resp.Msg.GetSuccess())
    assert.Equal(t, int32(1), client.checkSessionCalls.Load())
    assert.Equal(t, int32(1), client.createGuestCalls.Load())
}
```

- [ ] **Step 2: Run, verify failure**

```bash
task test -- -run TestWebCreateGuest ./internal/web/
```

Expected: FAIL — gate not applied; `CreateGuest` runs even with a valid cookie.

- [ ] **Step 3: Apply the gate**

In `WebCreateGuest` (find via `rg -n 'func .*WebCreateGuest' internal/web/auth_handlers.go`), insert at the top of the function:

```go
func (h *Handler) WebCreateGuest(ctx context.Context, req *connect.Request[webv1.WebCreateGuestRequest]) (*connect.Response[webv1.WebCreateGuestResponse], error) {
    if name, gated, err := h.checkCookieCollision(ctx, req.Header()); err != nil {
        return nil, oops.Wrap(err)
    } else if gated {
        return connect.NewResponse(&webv1.WebCreateGuestResponse{
            Success:           false,
            ErrorCode:         "ALREADY_AUTHENTICATED",
            ErrorMessage:      fmt.Sprintf("Already signed in as %s.", name),
            CurrentPlayerName: name,
        }), nil
    }

    // ... existing body unchanged ...
}
```

Make sure `fmt` is already imported (it almost certainly is — verify with `rg -n '"fmt"' internal/web/auth_handlers.go`).

- [ ] **Step 4: Run, verify pass**

```bash
task test -- -run TestWebCreateGuest ./internal/web/
```

Expected: all three PASS.

- [ ] **Step 5: Add the concurrent-call test**

Append:

```go
func TestWebCreateGuestConcurrentValidCookieAllGate(t *testing.T) {
    client := &mockCoreClient{
        checkSessionResp: &corev1.CheckPlayerSessionResponse{PlayerName: "Jasper Iodine"},
    }
    h := NewHandler(client)

    var wg sync.WaitGroup
    var gatedCount atomic.Int32
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            req := connect.NewRequest(&webv1.WebCreateGuestRequest{})
            req.Header().Set(headerInjectSessionToken, "valid-token")
            resp, err := h.WebCreateGuest(context.Background(), req)
            require.NoError(t, err)
            if resp.Msg.GetErrorCode() == "ALREADY_AUTHENTICATED" {
                gatedCount.Add(1)
            }
        }()
    }
    wg.Wait()
    assert.Equal(t, int32(10), gatedCount.Load(), "all 10 concurrent calls MUST hit the gate")
    assert.Equal(t, int32(0), client.createGuestCalls.Load(), "zero CreateGuest calls MUST occur")
}
```

The `mockCoreClient` is shared across goroutines. Reads of `checkSessionResp` are safe (immutable in the test); the call counters are `atomic.Int32` (per the Test setup conventions section), so concurrent `.Add(1)` and `.Load()` are race-free under `-race`.

- [ ] **Step 6: Run with the race detector**

```bash
task test -- -race -run TestWebCreateGuestConcurrent ./internal/web/
```

Expected: PASS, no races.

- [ ] **Step 7: Lint + commit**

```bash
task fmt && task lint
```

```text
feat(web): refuse WebCreateGuest with ALREADY_AUTHENTICATED when cookie valid

Closes the cookie-collision footgun in the multi-tab guest flow without
modifying the cookie. Concurrent-valid-cookie test pins the
deterministic short-circuit. The no-cookie / expired-cookie race is
documented as out of scope per spec §4.2.5.

Refs holomush-9q8n.
```

---

### Task 8: Apply gate to `WebAuthenticatePlayer`

**Files:**

- Modify: `internal/web/auth_handlers.go` (`WebAuthenticatePlayer`)
- Test: `internal/web/auth_handlers_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestWebAuthenticatePlayerReturnsAlreadyAuthenticatedWhenCookieValid(t *testing.T) {
    client := &mockCoreClient{
        checkSessionResp: &corev1.CheckPlayerSessionResponse{PlayerName: "Real Player"},
    }
    h := NewHandler(client)

    req := connect.NewRequest(&webv1.WebAuthenticatePlayerRequest{
        Username: "real_player",
        Password: "correct horse battery staple",
    })
    req.Header().Set(headerInjectSessionToken, "valid-token")

    resp, err := h.WebAuthenticatePlayer(context.Background(), req)
    require.NoError(t, err)
    assert.False(t, resp.Msg.GetSuccess())
    assert.Equal(t, "ALREADY_AUTHENTICATED", resp.Msg.GetErrorCode())
    assert.Equal(t, "Real Player", resp.Msg.GetCurrentPlayerName())
    assert.Contains(t, resp.Msg.GetErrorMessage(), "Real Player")
    assert.Equal(t, int32(0), client.authPlayerCalls.Load(), "AuthenticatePlayer MUST NOT run; cap eviction stays untouched")
    assert.Empty(t, resp.Header().Get(headerSetSessionToken), "no Set-Cookie on gate hit")
}

func TestWebAuthenticatePlayerProceedsWhenCookieAbsent(t *testing.T) {
    client := &mockCoreClient{
        authPlayerResp: &corev1.AuthenticatePlayerResponse{
            Success: true, PlayerSessionToken: "fresh-token",
        },
    }
    h := NewHandler(client)

    req := connect.NewRequest(&webv1.WebAuthenticatePlayerRequest{Username: "u", Password: "p"})

    resp, err := h.WebAuthenticatePlayer(context.Background(), req)
    require.NoError(t, err)
    assert.True(t, resp.Msg.GetSuccess())
    assert.Equal(t, int32(1), client.authPlayerCalls.Load())
}
```

- [ ] **Step 2: Run, verify failure**

```bash
task test -- -run TestWebAuthenticatePlayer ./internal/web/
```

Expected: FAIL on the gate-hit test.

- [ ] **Step 3: Apply the gate**

```go
func (h *Handler) WebAuthenticatePlayer(ctx context.Context, req *connect.Request[webv1.WebAuthenticatePlayerRequest]) (*connect.Response[webv1.WebAuthenticatePlayerResponse], error) {
    if name, gated, err := h.checkCookieCollision(ctx, req.Header()); err != nil {
        return nil, oops.Wrap(err)
    } else if gated {
        return connect.NewResponse(&webv1.WebAuthenticatePlayerResponse{
            Success:           false,
            ErrorCode:         "ALREADY_AUTHENTICATED",
            ErrorMessage:      fmt.Sprintf("Already signed in as %s.", name),
            CurrentPlayerName: name,
        }), nil
    }

    // ... existing body unchanged ...
}
```

- [ ] **Step 4: Run, verify pass**

```bash
task test -- -run TestWebAuthenticatePlayer ./internal/web/
```

Expected: PASS, including the existing `TestWebAuthenticatePlayer_Success` / `_CoreFailure` / `_RPCError` / `_NoRememberMe` tests (they don't set the cookie token header, so they bypass the gate naturally).

- [ ] **Step 5: Lint + commit**

```bash
task fmt && task lint
```

```text
feat(web): refuse WebAuthenticatePlayer with ALREADY_AUTHENTICATED when cookie valid

Gate fires before the core AuthenticatePlayer RPC, so maxSessionsPerPlayer
cap eviction (auth_service.go:154-254 CreateWithCap) is preserved exactly
for the non-gate path.

Refs holomush-9q8n.
```

---

### Task 9: Apply gate to `WebCreatePlayer`

**Files:**

- Modify: `internal/web/auth_handlers.go` (`WebCreatePlayer`)
- Test: `internal/web/auth_handlers_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestWebCreatePlayerReturnsAlreadyAuthenticatedWhenCookieValid(t *testing.T) {
    client := &mockCoreClient{
        checkSessionResp: &corev1.CheckPlayerSessionResponse{PlayerName: "Existing Player"},
    }
    h := NewHandler(client)

    req := connect.NewRequest(&webv1.WebCreatePlayerRequest{
        Username: "new_player", Password: "x", Email: "new@example.com",
    })
    req.Header().Set(headerInjectSessionToken, "valid-token")

    resp, err := h.WebCreatePlayer(context.Background(), req)
    require.NoError(t, err)
    assert.False(t, resp.Msg.GetSuccess())
    assert.Equal(t, "ALREADY_AUTHENTICATED", resp.Msg.GetErrorCode())
    assert.Equal(t, "Existing Player", resp.Msg.GetCurrentPlayerName())
    assert.Equal(t, int32(0), client.createPlayerCalls.Load())
}
```

- [ ] **Step 2: Run, fail, implement**

```bash
task test -- -run TestWebCreatePlayer ./internal/web/
```

First run expected: FAIL.

Apply:

```go
func (h *Handler) WebCreatePlayer(ctx context.Context, req *connect.Request[webv1.WebCreatePlayerRequest]) (*connect.Response[webv1.WebCreatePlayerResponse], error) {
    if name, gated, err := h.checkCookieCollision(ctx, req.Header()); err != nil {
        return nil, oops.Wrap(err)
    } else if gated {
        return connect.NewResponse(&webv1.WebCreatePlayerResponse{
            Success:           false,
            ErrorCode:         "ALREADY_AUTHENTICATED",
            ErrorMessage:      fmt.Sprintf("Already signed in as %s.", name),
            CurrentPlayerName: name,
        }), nil
    }

    // ... existing body unchanged ...
}
```

Run again; PASS expected.

- [ ] **Step 3: Run the full web test package**

```bash
task test -- ./internal/web/
```

Expected: all PASS.

- [ ] **Step 4: Lint + commit**

```bash
task fmt && task lint
```

```text
feat(web): refuse WebCreatePlayer with ALREADY_AUTHENTICATED when cookie valid

Defense in depth: a logged-in user submitting the registration form in
another tab no longer overwrites their cookie.

Refs holomush-9q8n.
```

---

## Phase 3 — Web client UX

### Task 10: Extend `authStore`

**Files:**

- Modify: `web/src/lib/stores/authStore.ts`
- Test: `web/src/lib/stores/authStore.test.ts` (create if absent)

- [ ] **Step 1: Inspect existing vitest test layout**

```bash
ls web/src/lib/stores/ && rg -n 'vitest|describe.*authStore' web/src/lib/stores/ 2>/dev/null
```

Note whether there's already a `*.test.ts` for `authStore`. Match its structure.

- [ ] **Step 2: Write the failing test**

Create or extend `web/src/lib/stores/authStore.test.ts`:

```typescript
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, beforeEach } from 'vitest';
import { authState, setPlayerProfile, clearAuth } from './authStore';
import { get } from 'svelte/store';

describe('authStore.setPlayerProfile', () => {
  beforeEach(() => {
    sessionStorage.clear();
    clearAuth();
  });

  it('stores playerId, playerName, isGuest, characters', () => {
    setPlayerProfile({
      playerId: '01KQ2Y5ETK5957724MGZ2H2TDB',
      playerName: 'Jasper Iodine',
      isGuest: true,
      characters: [{ characterId: '01KQ', name: 'Jasper Iodine' }],
    });
    const s = get(authState);
    expect(s.playerId).toBe('01KQ2Y5ETK5957724MGZ2H2TDB');
    expect(s.playerName).toBe('Jasper Iodine');
    expect(s.isGuest).toBe(true);
    expect(s.characters).toHaveLength(1);
    expect(s.isPlayerAuthenticated).toBe(true);
  });

  it('clearAuth resets all profile fields', () => {
    setPlayerProfile({
      playerId: '01KQ',
      playerName: 'X',
      isGuest: false,
      characters: [],
    });
    clearAuth();
    const s = get(authState);
    expect(s.playerId).toBeNull();
    expect(s.playerName).toBeNull();
    expect(s.isGuest).toBe(false);
    expect(s.characters).toEqual([]);
    expect(s.isPlayerAuthenticated).toBe(false);
  });
});
```

- [ ] **Step 3: Run, verify failure**

```bash
cd web && npx vitest run src/lib/stores/authStore.test.ts
```

Expected: FAIL — `setPlayerProfile` does not exist.

- [ ] **Step 4: Extend `authStore.ts`**

Replace `web/src/lib/stores/authStore.ts` with:

```typescript
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable, derived } from 'svelte/store';
import { trace } from '@opentelemetry/api';

const tracer = trace.getTracer('holomush-web');

export interface CharacterSummary {
  characterId: string;
  name?: string;
}

interface AuthState {
  isPlayerAuthenticated: boolean;
  sessionId: string | null;
  characterName: string | null;
  playerName: string | null;
  playerId: string | null;
  isGuest: boolean;
  characters: CharacterSummary[];
}

const initial: AuthState = {
  isPlayerAuthenticated: false,
  sessionId: null,
  characterName: null,
  playerName: null,
  playerId: null,
  isGuest: false,
  characters: [],
};

export const authState = writable<AuthState>(initial);
export const isAuthenticated = derived(authState, ($s) => $s.isPlayerAuthenticated || !!$s.sessionId);
export const hasCharacter = derived(authState, ($s) => !!$s.sessionId && !!$s.characterName);

export function setPlayerAuth(playerName: string) {
  sessionStorage.removeItem('holomush-player');
  authState.update((s) => ({ ...s, isPlayerAuthenticated: true, playerName }));
}

export function setPlayerProfile(profile: {
  playerId: string;
  playerName: string;
  isGuest: boolean;
  characters: CharacterSummary[];
}) {
  sessionStorage.removeItem('holomush-player');
  authState.update((s) => ({
    ...s,
    isPlayerAuthenticated: true,
    playerId: profile.playerId,
    playerName: profile.playerName,
    isGuest: profile.isGuest,
    characters: profile.characters,
  }));
}

export function setCharacterSession(sessionId: string, characterName: string) {
  authState.update((s) => ({ ...s, sessionId, characterName }));
  sessionStorage.setItem('holomush-session', JSON.stringify({ sessionId, characterName }));
}

export function clearAuth() {
  authState.set(initial);
  sessionStorage.removeItem('holomush-session');
}

export function clearCharacterSession() {
  authState.update((s) => ({ ...s, sessionId: null, characterName: null }));
  sessionStorage.removeItem('holomush-session');
}

export function restoreSession(): void {
  const span = tracer.startSpan('session.restore');
  try {
    sessionStorage.removeItem('holomush-player');
    const saved = sessionStorage.getItem('holomush-session');
    if (saved) {
      try {
        const { sessionId, characterName } = JSON.parse(saved);
        if (sessionId) authState.update((s) => ({ ...s, sessionId, characterName }));
      } catch {
        /* ignore corrupt data */
      }
    }
  } finally {
    span.end();
  }
}
```

- [ ] **Step 5: Run, verify pass**

```bash
cd web && npx vitest run
```

Expected: all PASS — `setPlayerAuth` is preserved so existing callers still work.

- [ ] **Step 6: Lint + commit**

```bash
cd web && npm run check && npm run lint
cd ..
task fmt && task lint
```

```text
feat(web/auth-store): extend AuthState with playerId, isGuest, characters

Adds setPlayerProfile() consumed by webCheckSession callers in subsequent
tasks. setPlayerAuth() kept for back-compat with the existing login flow.

Refs holomush-9q8n.
```

---

### Task 11: Landing page authenticated branch + client-side §4.4.4 pre-gate

**Files:**

- Create: `web/src/routes/+page.ts`
- Modify: `web/src/routes/+page.svelte`

This task implements both spec §4.4.1 (authenticated branch) and §4.4.4 (client-side pre-gate before any `webCreateGuest` / `authenticatePlayer` call). Visual coverage is via Playwright in Task 24 — no Vitest component tests (no `@testing-library/svelte` installed).

- [ ] **Step 1: Create `+page.ts`**

```typescript
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { createClient } from '@connectrpc/connect';
import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';
import { setPlayerProfile, clearAuth } from '$lib/stores/authStore';
import type { PageLoad } from './$types';

export const ssr = false;

export const load: PageLoad = async ({ parent }) => {
  const baseData = await parent();

  if (typeof window === 'undefined') {
    return { ...baseData, authenticated: false };
  }

  const client = createClient(WebService, transport);
  try {
    const resp = await client.webCheckSession({});
    setPlayerProfile({
      playerId: resp.playerId,
      playerName: resp.playerName,
      isGuest: resp.isGuest,
      characters: resp.characters.map((c) => ({ characterId: c.characterId, name: c.characterName })),
    });
    return {
      ...baseData,
      authenticated: true,
      playerName: resp.playerName,
      characters: resp.characters,
    };
  } catch {
    clearAuth();
    return { ...baseData, authenticated: false };
  }
};
```

If the existing `+page.ts` exists with a `load()` already returning `{hero, pitch, features, connectInfo}`, MERGE the new logic — call `await parent()` if it's nested under a layout-level loader, or invoke the existing fetcher inline. Run `cat web/src/routes/+page.ts 2>/dev/null` to see what's there before writing.

- [ ] **Step 2: Modify `+page.svelte`**

Replace the existing `+page.svelte`:

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { goto } from '$app/navigation';
  import MarkdownContent from '$lib/components/MarkdownContent.svelte';
  import { Button } from '$lib/components/ui/button';
  import * as Card from '$lib/components/ui/card';
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { clearAuth, setCharacterSession } from '$lib/stores/authStore';
  import type { ContentItem } from '$lib/stores/contentStore';

  let { data }: {
    data: {
      hero?: ContentItem;
      pitch?: ContentItem;
      features?: ContentItem[];
      connectInfo?: ContentItem;
      authenticated: boolean;
      playerName?: string;
      characters?: { characterId: string; characterName?: string }[];
    };
  } = $props();

  const hero = $derived(data.hero);
  const pitch = $derived(data.pitch);
  const features = $derived(data.features ?? []);
  const connectInfo = $derived(data.connectInfo);
  const heroTitle = $derived(hero?.metadata?.title ?? 'HoloMUSH');
  const heroTagline = $derived(hero?.metadata?.tagline ?? 'A modern MUSH platform');
  const hasContent = $derived(!!hero || !!pitch || features.length > 0 || !!connectInfo);

  const client = createClient(WebService, transport);
  let busy = $state(false);
  let error = $state('');

  // Spec §4.4.4 client-side pre-gate: probe webCheckSession before any
  // create/auth call. If the throw doesn't fire, the user is already signed
  // in and we route to the authenticated landing branch instead of clobbering
  // the cookie.
  async function isAlreadySignedIn(): Promise<boolean> {
    try {
      await client.webCheckSession({});
      return true;
    } catch {
      return false;
    }
  }

  async function handleGuest() {
    error = '';
    busy = true;
    try {
      if (await isAlreadySignedIn()) {
        // Defense in depth — load() should already have rendered the
        // authenticated branch. Reload to pick it up.
        location.reload();
        return;
      }
      const resp = await client.webCreateGuest({});
      if (resp.success) {
        const charId = resp.defaultCharacterId || resp.characters[0]?.characterId;
        if (charId) {
          const selectResp = await client.webSelectCharacter({ characterId: charId });
          if (selectResp.success) {
            setCharacterSession(selectResp.sessionId, selectResp.characterName);
            goto('/terminal');
            return;
          }
        }
        goto('/characters');
      } else if (resp.errorCode === 'ALREADY_AUTHENTICATED') {
        // Server-side backstop fired — same handling as the pre-gate.
        location.reload();
      } else {
        error = resp.errorMessage || 'Guest login failed.';
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Guest login failed.';
    } finally {
      busy = false;
    }
  }

  async function handleContinue() {
    busy = true;
    try {
      const chars = data.characters ?? [];
      if (chars.length === 0) {
        goto('/characters');
        return;
      }
      if (chars.length === 1) {
        const selectResp = await client.webSelectCharacter({ characterId: chars[0].characterId });
        if (selectResp.success) {
          setCharacterSession(selectResp.sessionId, selectResp.characterName);
          goto('/terminal');
          return;
        }
        error = selectResp.errorMessage || 'Could not resume session.';
        return;
      }
      goto('/characters');
    } finally {
      busy = false;
    }
  }

  async function handleLogout() {
    busy = true;
    try {
      await client.webLogout({});
    } catch {
      /* swallow */
    }
    clearAuth();
    busy = false;
    location.reload();
  }
</script>

<div class="flex flex-col items-center min-h-[calc(100vh-36px)] px-6 pb-12" data-testid="landing">
  <section class="flex flex-col items-center justify-center gap-4 py-16 pb-12" data-testid="hero">
    <h1 class="text-[38px] font-bold tracking-wider text-primary" data-testid="hero-title">{heroTitle}</h1>
    <p class="text-[15px] text-muted-foreground" data-testid="hero-tagline">{heroTagline}</p>

    {#if error}
      <p class="text-sm text-destructive" data-testid="hero-error">{error}</p>
    {/if}

    {#if data.authenticated}
      <div class="flex flex-col items-center gap-2 mt-2" data-testid="hero-actions-authenticated">
        <p class="text-sm">Signed in as <strong>{data.playerName}</strong></p>
        <div class="flex gap-3 flex-wrap justify-center">
          <Button onclick={handleContinue} disabled={busy} data-testid="continue-button">Continue</Button>
          <Button variant="ghost" onclick={handleLogout} disabled={busy} data-testid="logout-button">Log out</Button>
        </div>
      </div>
    {:else}
      <div class="flex gap-3 mt-2 flex-wrap justify-center" data-testid="hero-actions">
        <Button href="/login" data-testid="login-link">Login</Button>
        <Button variant="outline" href="/register" data-testid="register-link">Register</Button>
        <Button variant="ghost" onclick={handleGuest} disabled={busy} data-testid="guest-button">
          {busy ? 'Connecting…' : 'Try as Guest'}
        </Button>
      </div>
    {/if}
  </section>

  <!-- Preserve existing pitch / features / connectInfo sections that follow the hero. -->
</div>
```

When pasting, preserve every section after the hero verbatim from the existing `+page.svelte` (the script above intentionally omits the bottom half so an implementer doesn't accidentally drop existing content — read the current file first and concatenate).

- [ ] **Step 3: Type-check + lint**

```bash
cd web && npm run check && npm run lint
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd ..
task fmt && task lint
```

```text
feat(web): landing page authenticated branch + client-side §4.4.4 pre-gate

webCheckSession runs in +page.ts load() so render is gated and there is
no flash of "Try as Guest" for returning authenticated users. handleGuest
also calls webCheckSession before webCreateGuest as a client-side
pre-gate, with the server-side ALREADY_AUTHENTICATED error as the
backstop.

Refs holomush-9q8n.
```

---

### Task 12: Login page authenticated branch + pre-gate

**Files:**

- Create: `web/src/routes/login/+page.ts`
- Modify: `web/src/routes/login/+page.svelte`

- [ ] **Step 1: Create `+page.ts`**

```typescript
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { createClient } from '@connectrpc/connect';
import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';
import { setPlayerProfile, clearAuth } from '$lib/stores/authStore';
import type { PageLoad } from './$types';

export const ssr = false;

export const load: PageLoad = async () => {
  if (typeof window === 'undefined') return { authenticated: false };

  const client = createClient(WebService, transport);
  try {
    const resp = await client.webCheckSession({});
    setPlayerProfile({
      playerId: resp.playerId,
      playerName: resp.playerName,
      isGuest: resp.isGuest,
      characters: resp.characters.map((c) => ({ characterId: c.characterId, name: c.characterName })),
    });
    return { authenticated: true, playerName: resp.playerName, characters: resp.characters };
  } catch {
    clearAuth();
    return { authenticated: false };
  }
};
```

- [ ] **Step 2: Read the existing login page**

```bash
cat web/src/routes/login/+page.svelte
```

Note the existing form structure, `handleLogin` shape, and any imports already in place. Preserve all of them when rewriting.

- [ ] **Step 3: Modify `+page.svelte`**

Replace with (preserve any existing form details — only the branching wrapper, `isAlreadySignedIn`, `handleContinue`, and `handleLogout` are new):

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { goto } from '$app/navigation';
  import { Button } from '$lib/components/ui/button';
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { clearAuth, setCharacterSession } from '$lib/stores/authStore';
  // ...preserve existing imports the original file used (Card, MarkdownContent, etc.)...

  let { data }: {
    data: {
      authenticated: boolean;
      playerName?: string;
      characters?: { characterId: string; characterName?: string }[];
    };
  } = $props();

  const client = createClient(WebService, transport);
  let busy = $state(false);
  let error = $state('');
  let username = $state('');
  let password = $state('');

  async function isAlreadySignedIn(): Promise<boolean> {
    try {
      await client.webCheckSession({});
      return true;
    } catch {
      return false;
    }
  }

  async function handleLogin() {
    error = '';
    busy = true;
    try {
      if (await isAlreadySignedIn()) {
        location.reload();
        return;
      }
      const resp = await client.webAuthenticatePlayer({ username, password });
      if (resp.success) {
        // ...existing post-login routing...
        const charId = resp.defaultCharacterId || resp.characters[0]?.characterId;
        if (charId) {
          const selectResp = await client.webSelectCharacter({ characterId: charId });
          if (selectResp.success) {
            setCharacterSession(selectResp.sessionId, selectResp.characterName);
            goto('/terminal');
            return;
          }
        }
        goto('/characters');
      } else if (resp.errorCode === 'ALREADY_AUTHENTICATED') {
        location.reload();
      } else {
        error = resp.errorMessage || 'Login failed.';
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Login failed.';
    } finally {
      busy = false;
    }
  }

  async function handleContinue() {
    busy = true;
    try {
      const chars = data.characters ?? [];
      if (chars.length === 0) {
        goto('/characters');
        return;
      }
      if (chars.length === 1) {
        const selectResp = await client.webSelectCharacter({ characterId: chars[0].characterId });
        if (selectResp.success) {
          setCharacterSession(selectResp.sessionId, selectResp.characterName);
          goto('/terminal');
          return;
        }
        error = selectResp.errorMessage || 'Could not resume session.';
        return;
      }
      goto('/characters');
    } finally {
      busy = false;
    }
  }

  async function handleLogout() {
    busy = true;
    try {
      await client.webLogout({});
    } catch {
      /* swallow */
    }
    clearAuth();
    busy = false;
    location.reload();
  }
</script>

<div class="flex flex-col items-center" data-testid="login-page">
  {#if data.authenticated}
    <div class="flex flex-col items-center gap-2 mt-12" data-testid="login-actions-authenticated">
      <p class="text-sm">Signed in as <strong>{data.playerName}</strong></p>
      <div class="flex gap-3 flex-wrap justify-center">
        <Button onclick={handleContinue} disabled={busy} data-testid="continue-button">Continue</Button>
        <Button variant="ghost" onclick={handleLogout} disabled={busy} data-testid="logout-button">Log out</Button>
      </div>
    </div>
  {:else}
    <!-- Existing login form preserved verbatim. Bind the existing inputs to
         the `username` / `password` $state variables; route the existing
         submit handler to handleLogin(). If the original file had additional
         fields (Remember me, etc.) preserve them. -->
    <form onsubmit={(e) => { e.preventDefault(); handleLogin(); }} data-testid="login-form">
      <!-- preserve original form markup; only wire submit to handleLogin -->
    </form>
  {/if}

  {#if error}
    <p class="text-sm text-destructive" data-testid="login-error">{error}</p>
  {/if}
</div>
```

- [ ] **Step 4: Type-check + lint + commit**

```bash
cd web && npm run check && npm run lint
cd ..
task fmt && task lint
```

```text
feat(web): login page authenticated branch + pre-gate

Same load()-blocking + branch pattern as the landing page. Authenticated
users see Continue / Log out instead of the login form. handleLogin
checks webCheckSession first; ALREADY_AUTHENTICATED from the server is
the backstop.

Refs holomush-9q8n.
```

---

### Task 13: Register page authenticated branch + pre-gate

**Files:**

- Create: `web/src/routes/register/+page.ts`
- Modify: `web/src/routes/register/+page.svelte`

- [ ] **Step 1: Create `+page.ts`**

Identical to Task 12's `+page.ts` (copy verbatim — different file path):

```typescript
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { createClient } from '@connectrpc/connect';
import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';
import { setPlayerProfile, clearAuth } from '$lib/stores/authStore';
import type { PageLoad } from './$types';

export const ssr = false;

export const load: PageLoad = async () => {
  if (typeof window === 'undefined') return { authenticated: false };

  const client = createClient(WebService, transport);
  try {
    const resp = await client.webCheckSession({});
    setPlayerProfile({
      playerId: resp.playerId,
      playerName: resp.playerName,
      isGuest: resp.isGuest,
      characters: resp.characters.map((c) => ({ characterId: c.characterId, name: c.characterName })),
    });
    return { authenticated: true, playerName: resp.playerName, characters: resp.characters };
  } catch {
    clearAuth();
    return { authenticated: false };
  }
};
```

- [ ] **Step 2: Read the existing register page**

```bash
cat web/src/routes/register/+page.svelte
```

- [ ] **Step 3: Modify `+page.svelte`**

Mirror Task 12 step 3 with these substitutions:

- `webAuthenticatePlayer({ username, password })` → `webCreatePlayer({ username, password, email })`
- Add `let email = $state('')` to the `<script>` runes
- Bind the existing register form's email input to `email`
- The authenticated branch is identical to Task 12's (Continue / Log out)

`handleRegister` body:

```typescript
async function handleRegister() {
  error = '';
  busy = true;
  try {
    if (await isAlreadySignedIn()) {
      location.reload();
      return;
    }
    const resp = await client.webCreatePlayer({ username, password, email });
    if (resp.success) {
      // Post-register: a fresh registered player has no characters yet; route
      // to /characters where the player can create their first character. If
      // resp.characters has any entries (it shouldn't on a fresh register),
      // auto-select the first one for parity with handleLogin.
      const firstChar = resp.characters?.[0];
      if (firstChar) {
        const selectResp = await client.webSelectCharacter({ characterId: firstChar.characterId });
        if (selectResp.success) {
          setCharacterSession(selectResp.sessionId, selectResp.characterName);
          goto('/terminal');
          return;
        }
      }
      goto('/characters');
    } else if (resp.errorCode === 'ALREADY_AUTHENTICATED') {
      location.reload();
    } else {
      error = resp.errorMessage || 'Registration failed.';
    }
  } catch (e) {
    error = e instanceof Error ? e.message : 'Registration failed.';
  } finally {
    busy = false;
  }
}
```

- [ ] **Step 4: Type-check + lint + commit**

```bash
cd web && npm run check && npm run lint
cd ..
task fmt && task lint
```

```text
feat(web): register page authenticated branch + pre-gate

Mirrors the login page. Registration form hidden for already-signed-in
users; handleRegister checks webCheckSession first.

Refs holomush-9q8n.
```

---

### Task 14: Terminal page uniform `SESSION_NOT_FOUND` handling

**Files:**

- Modify: `web/src/routes/(authed)/terminal/+page.svelte`
- Modify: `web/src/lib/backfill/streamBackfill.ts`

- [ ] **Step 1: Audit the four call sites listed in spec §4.4.5**

```bash
rg -n 'webQueryStreamHistory|webListSessionStreams|handleCommand|client\.subscribe' "web/src/routes/(authed)/terminal/+page.svelte"
```

Identify the Subscribe IIFE error block, the backfill call, the WebListSessionStreams call, and the command-submit handler.

- [ ] **Step 2: Add a shared stale-session helper at the top of `+page.svelte`'s `<script>`**

```typescript
import { ConnectError, Code } from '@connectrpc/connect';
import { goto } from '$app/navigation';
import { clearCharacterSession, clearAuth } from '$lib/stores/authStore';

function isStaleSession(e: unknown): boolean {
  if (e instanceof ConnectError) {
    if (e.code === Code.Unauthenticated) return true;
    return e.message.includes('SESSION_NOT_FOUND') || e.message.includes('SESSION_EXPIRED');
  }
  if (e instanceof Error) {
    return e.message.includes('SESSION_NOT_FOUND') || e.message.includes('SESSION_EXPIRED');
  }
  return false;
}

async function handleStaleSession() {
  clearCharacterSession();
  clearAuth();
  await goto('/');
}
```

Verify the import path matches the project convention by `rg -n "from '@connectrpc/connect'" web/src/` — copy whatever shape the existing terminal page uses.

- [ ] **Step 3: Wire each of the four call sites**

For each catch block, add:

```typescript
if (isStaleSession(e)) {
  await handleStaleSession();
  return;
}
// existing per-call error handling continues here
```

- [ ] **Step 4: Surface stale errors from `streamBackfill.ts`**

In `web/src/lib/backfill/streamBackfill.ts:229-258` (`fetchOneStream`), distinguish stale-session errors from generic transport errors. Either:

- Re-throw stale errors so the caller can route, or
- Return `{ ok: false, kind: 'stale', error: e }` and update `BackfillResult` consumers in `+page.svelte` to check `kind === 'stale'`.

The simpler choice is the re-throw — change the existing `return { ok: false, error: e }` to:

```typescript
if (isStaleSession(e)) throw e;  // let the caller route to /
return { ok: false, error: e };
```

Move `isStaleSession` into a shared module (`web/src/lib/util/stale.ts`) so both files use the same predicate.

- [ ] **Step 5: Type-check + lint + run web tests + commit**

```bash
cd web && npm run check && npm run lint && npx vitest run
cd ..
task fmt && task lint
```

```text
feat(web/terminal): uniform stale-session handling at the four call sites

Subscribe, WebQueryStreamHistory, WebListSessionStreams, and HandleCommand
all route to clearCharacterSession + clearAuth + goto('/') on
SESSION_NOT_FOUND / SESSION_EXPIRED. Other errors keep the existing
per-call toast.

Refs holomush-9q8n.
```

---

### Task 15: `(authed)/+layout.ts` reads new fields additively

**Files:**

- Modify: `web/src/routes/(authed)/+layout.ts`

- [ ] **Step 1: Update the `load()`**

Read the current file (verified contents at `web/src/routes/(authed)/+layout.ts:1-26`). Replace the success-path body:

```typescript
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { isRedirect, redirect } from '@sveltejs/kit';
import { createClient } from '@connectrpc/connect';
import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';
import { clearAuth, setPlayerProfile, restoreSession } from '$lib/stores/authStore';

export const ssr = false;

export async function load() {
  if (typeof window === 'undefined') return;

  restoreSession();

  const client = createClient(WebService, transport);
  try {
    const resp = await client.webCheckSession({});
    setPlayerProfile({
      playerId: resp.playerId,
      playerName: resp.playerName,
      isGuest: resp.isGuest,
      characters: resp.characters.map((c) => ({ characterId: c.characterId, name: c.characterName })),
    });
  } catch (e) {
    if (isRedirect(e)) throw e;
    clearAuth();
    redirect(302, '/login');
  }
}
```

The throw / redirect path is unchanged. `setPlayerProfile` populates BOTH `playerName` and the new fields, so old call sites that read `playerName` keep working.

- [ ] **Step 2: Type-check + lint + commit**

```bash
cd web && npm run check && npm run lint
cd ..
task fmt && task lint
```

```text
feat(web/authed-layout): read new player_id/is_guest/characters fields additively

setPlayerProfile populates the full auth state from the extended
webCheckSession response. The throw-then-redirect failure path is
unchanged so pre-deploy tabs continue to work without modification.

Refs holomush-9q8n.
```

---

## Phase 4 — Integration tests (Ginkgo, build tag `integration`)

The auth integration suite at `test/integration/auth/auth_suite_test.go` constructs `auth.Service` directly. The multi-tab tests live in a NEW file `multi_tab_test.go` in the same package. Phase 4 first builds the test harness (Tasks 15.5 + 15.6) and then layers seven scenarios on top.

**Approach:** for each scenario, build the gateway `*web.Handler` against an in-process `*grpc.CoreServer` wired to the test repos, and invoke the handlers as `connect.Request` objects with explicit `X-Session-Token` header values. This skips the cookie middleware (already covered by `internal/web/cookie_test.go`) and exercises the gate logic + reattach + multi-connection model end-to-end.

**Iteration command (works only after Tasks 15.5 + 15.6 land):**

```bash
go test -race -tags=integration -count=1 -v ./test/integration/auth/...
```

`task test:int` is the canonical CI gate; run it after the suite is complete (Task 22 step 3).

---

### Task 15.5: Export header constants from `internal/web`

**Files:**

- Modify: `internal/web/auth_handlers.go` (add exported aliases for two unexported constants)

The integration tests (Tasks 16-22) need to set the `X-Session-Token` header that the existing `CookieMiddleware` would set in production. The constants `headerInjectSessionToken` and `headerSetSessionToken` are unexported. Export them once, in a tiny prep commit.

- [ ] **Step 1: Locate the constants**

```bash
rg -n 'headerInjectSessionToken|headerSetSessionToken|headerSetSessionMaxAge|headerClearSession' internal/web/
```

Expected: definitions in `internal/web/auth_handlers.go` (or `cookie.go`); references throughout the package.

- [ ] **Step 2: Add exported aliases at the same definition site**

In the same file as the constants, append (after the unexported `const (...)` block):

```go
// Exported aliases of the wire-level header names so integration tests can
// thread the same header values without duplicating string literals. These
// MUST stay in sync with the unexported constants above.
const (
    HeaderInjectSessionToken = headerInjectSessionToken
    HeaderSetSessionToken    = headerSetSessionToken
    HeaderSetSessionMaxAge   = headerSetSessionMaxAge
    HeaderClearSession       = headerClearSession
)
```

If `headerSetSessionMaxAge` / `headerClearSession` don't exist in the package, omit the corresponding aliases — the integration tests in this plan only need the first two.

- [ ] **Step 3: Verify**

```bash
task test -- ./internal/web/...
```

Expected: PASS — pure additive change.

- [ ] **Step 4: Lint + commit**

```bash
task fmt && task lint
```

```text
chore(web): export header-name aliases for integration tests

Tests in test/integration/auth/multi_tab_test.go thread the same
X-Session-Token header that production cookie middleware injects.
Aliases keep the literal value in one place.

Refs holomush-9q8n.
```

---

### Task 15.6: Build the integration test harness

**Files:**

- Modify: `test/integration/auth/auth_suite_test.go` (extend `testEnv` with `coreServer` + `webHandler`; wire them in `setupTestEnv`)
- Create: `test/integration/auth/core_client_shim.go` (in-process adapter from `*grpc.CoreServer` to the `web.CoreClient` interface)

The plan up to this point assumes a `gw *web.Handler` and an `env *testEnv` exist. They don't. This task builds them.

- [ ] **Step 1: Inspect the existing suite to confirm the shape**

```bash
cat test/integration/auth/auth_suite_test.go
rg -n 'type CoreClient interface' internal/web/
```

Note: the existing `testEnv` likely holds `playerSessionStore`, `playerRepo`, `charRepo`, `locRepo`, `sessionStore`, `eventStore`, `authService`, `hasher`. The `web.CoreClient` interface is at `internal/web/handler.go` (search `rg -n 'type CoreClient' internal/web/handler.go`).

- [ ] **Step 2: Create the in-process shim**

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
    "context"

    corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
    holoGRPC "github.com/holomush/holomush/internal/grpc"
)

// coreClientShim adapts an in-process *grpc.CoreServer to the web.CoreClient
// interface so integration tests can stand up the full gateway+core stack
// without HTTP transport. Each method delegates directly to the corresponding
// CoreServer method; gRPC framing is bypassed entirely.
//
// Add new methods here whenever web.CoreClient gains an RPC.
type coreClientShim struct {
    s *holoGRPC.CoreServer
}

func (c *coreClientShim) AuthenticatePlayer(ctx context.Context, req *corev1.AuthenticatePlayerRequest) (*corev1.AuthenticatePlayerResponse, error) {
    return c.s.AuthenticatePlayer(ctx, req)
}

func (c *coreClientShim) CreateGuest(ctx context.Context, req *corev1.CreateGuestRequest) (*corev1.CreateGuestResponse, error) {
    return c.s.CreateGuest(ctx, req)
}

func (c *coreClientShim) CreatePlayer(ctx context.Context, req *corev1.CreatePlayerRequest) (*corev1.CreatePlayerResponse, error) {
    return c.s.CreatePlayer(ctx, req)
}

func (c *coreClientShim) CheckPlayerSession(ctx context.Context, req *corev1.CheckPlayerSessionRequest) (*corev1.CheckPlayerSessionResponse, error) {
    return c.s.CheckPlayerSession(ctx, req)
}

func (c *coreClientShim) SelectCharacter(ctx context.Context, req *corev1.SelectCharacterRequest) (*corev1.SelectCharacterResponse, error) {
    return c.s.SelectCharacter(ctx, req)
}

func (c *coreClientShim) Logout(ctx context.Context, req *corev1.LogoutRequest) (*corev1.LogoutResponse, error) {
    return c.s.Logout(ctx, req)
}

// Add additional CoreClient methods (HandleCommand, Subscribe, Disconnect,
// QueryStreamHistory, ListSessionStreams, ListPlayerSessions,
// RevokePlayerSession, etc.) by mirroring the pattern. Use
// `rg -n 'type CoreClient interface' internal/web/` to enumerate the
// required method set; each maps 1:1 to a *holoGRPC.CoreServer method.
```

For methods like `Subscribe` whose return type is a streaming gRPC client, the shim needs a small adapter. The auth multi-tab tests in this plan don't call `Subscribe` directly — Tasks 17 and 19 verify reattach and command delivery via direct `CoreServer` calls (`HandleCommand`), not via the gateway's `Subscribe`. Add only the methods the Phase 4 tests actually need; verify by searching the new test file for `gw.Web*` and `coreServer.<X>` calls before considering this task done.

- [ ] **Step 3: Extend `testEnv` and `setupTestEnv` using `NewCoreServer` + option setters**

The `CoreServer` struct's fields are unexported (`internal/grpc/server.go:121-171`), so direct struct-literal construction won't compile from `package auth_test`. Use the existing constructor `NewCoreServer(engine, sessionStore, dispatcher, cmdServices, opts...)` (`internal/grpc/server.go:260`) with the existing option-setters:

| Setter | Source | Wires |
| --- | --- | --- |
| `WithAuthService(svc)` | `internal/grpc/auth_handlers.go:43` | `env.authService` |
| `WithPlayerSessionRepo(repo)` | `:64` | `env.playerSessionStore` |
| `WithPlayerRepo(repo)` | `:71` | `env.playerRepo` |
| `WithCharacterRepo(repo)` | `:78` | `env.charRepo` |
| `WithGuestService(svc)` | `:85` | guest service if the suite has one; nil otherwise |
| `WithSessionStore(store)` | `internal/grpc/server.go:177` | `env.sessionStore` |
| `WithEventStore(store)` | `:192` | `env.eventStore` |

`NewCoreServer` panics on nil `dispatcher` / `cmdServices` (`server.go:273-275`). Auth-flow tests (Tasks 16, 20, 22) don't need them; HandleCommand-flow tests (Tasks 17, 18, 19, 21) do. Two-step setup:

1. Build a minimal `dispatcher` and `cmdServices` using the project's existing test pattern. Likely candidates:
   - Look for a helper in `test/integration/command/` (which is the canonical HandleCommand integration package): `rg -n 'NewCoreServer|command.NewDispatcher|command.NewServices' test/integration/`. If a `setupCoreServer(t, env)` helper or similar exists, copy its construction recipe verbatim.
   - If no such helper exists, use the production wiring from `cmd/holomush/`: `rg -n 'command.NewDispatcher|command.NewServices' cmd/holomush/` and mirror the `dispatcher := command.NewDispatcher(...); cmdServices := &command.Services{...}` lines (replacing repos with the test repos).
   - The `engine *core.Engine` argument can be passed as `nil` at construction — `NewCoreServer` only stores it; auth-path code paths don't dereference it.

2. Wire the `testEnv` struct + `setupTestEnv` body:

```go
type testEnv struct {
    // ... existing fields ...
    coreServer *holoGRPC.CoreServer
    webHandler *web.Handler
}

// In setupTestEnv (or BeforeSuite), AFTER the existing repo + authService wiring:
dispatcher := buildIntegrationDispatcher(env)         // see step above; copy from test/integration/command/ pattern
cmdServices := buildIntegrationCmdServices(env)       // same pattern

env.coreServer = holoGRPC.NewCoreServer(
    nil,                  // engine: not exercised by auth/HandleCommand paths in this suite
    env.sessionStore,
    dispatcher,
    cmdServices,
    holoGRPC.WithAuthService(env.authService),
    holoGRPC.WithPlayerSessionRepo(env.playerSessionStore),
    holoGRPC.WithPlayerRepo(env.playerRepo),
    holoGRPC.WithCharacterRepo(env.charRepo),
    holoGRPC.WithSessionStore(env.sessionStore),
    holoGRPC.WithEventStore(env.eventStore),
)

shim := &coreClientShim{s: env.coreServer}
env.webHandler = web.NewHandler(shim)
```

The `buildIntegrationDispatcher` / `buildIntegrationCmdServices` helpers are local to the test file; their bodies copy whatever pattern the existing command-integration suite uses. If that suite doesn't exist or is too tangled to copy from, **Tasks 17, 18, 19, 21 fall back** to direct `auth.Service` and `WebSelectCharacter`-only assertions (no `HandleCommand`) — see the per-task notes in those tasks for the fallback shape, and update the §5 coverage matrix accordingly.

- [ ] **Step 4: Verify the suite still loads**

```bash
go test -race -tags=integration -count=1 -v ./test/integration/auth/... -ginkgo.dry-run
```

Expected: PASS — no specs run yet, but the package must compile.

- [ ] **Step 5: Lint + commit**

```bash
task fmt && task lint
```

```text
test(integration/auth): build in-process gateway+core harness for multi-tab tests

Adds testEnv.coreServer + testEnv.webHandler wired against the existing
test repos. coreClientShim adapts *grpc.CoreServer to web.CoreClient
without HTTP transport. Required by Tasks 16-22.

Refs holomush-9q8n.
```

After Tasks 15.5 + 15.6 land, every Task 16-22 reference to `gw` resolves to `env.webHandler` and every reference to `coreServer` resolves to `env.coreServer`. Use the shorthand inside test bodies:

```go
var _ = Describe("...", func() {
    var env *testEnv
    var gw *web.Handler
    BeforeEach(func() {
        env = setupTestEnv() // existing helper
        gw = env.webHandler
    })
    // ...
})
```

### Task 16: Two-tab guest scenario

**Files:**

- Create: `test/integration/auth/multi_tab_test.go`

- [ ] **Step 1: Write the new test file**

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
    "context"

    "connectrpc.com/connect"
    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    "github.com/holomush/holomush/internal/web"
    webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

var _ = Describe("Multi-tab session isolation — two-tab guest scenario", func() {
    var env *testEnv
    var gw *web.Handler

    BeforeEach(func() {
        env = setupTestEnv() // existing suite helper
        gw = env.webHandler  // built in Task 15.6
    })

    It("the second WebCreateGuest returns ALREADY_AUTHENTICATED and the first stays live", func() {
        ctx := context.Background()

        // Tab 1: WebCreateGuest with no cookie. Mints guest player A.
        tab1Resp, err := gw.WebCreateGuest(ctx, connect.NewRequest(&webv1.WebCreateGuestRequest{}))
        Expect(err).NotTo(HaveOccurred())
        Expect(tab1Resp.Msg.GetSuccess()).To(BeTrue())

        tab1Token := tab1Resp.Header().Get(web.HeaderSetSessionToken)
        Expect(tab1Token).NotTo(BeEmpty(), "WebCreateGuest must signal Set-Cookie via the X-Set-Session-Token header")

        // Tab 2: same browser, same cookie token. Cookie middleware would inject this in production;
        // here we simulate by setting the X-Session-Token header that CookieMiddleware would set.
        tab2Req := connect.NewRequest(&webv1.WebCreateGuestRequest{})
        tab2Req.Header().Set(web.HeaderInjectSessionToken, tab1Token)

        tab2Resp, err := gw.WebCreateGuest(ctx, tab2Req)
        Expect(err).NotTo(HaveOccurred())
        Expect(tab2Resp.Msg.GetSuccess()).To(BeFalse())
        Expect(tab2Resp.Msg.GetErrorCode()).To(Equal("ALREADY_AUTHENTICATED"))
        Expect(tab2Resp.Msg.GetCurrentPlayerName()).NotTo(BeEmpty())
        Expect(tab2Resp.Header().Get(web.HeaderSetSessionToken)).To(BeEmpty(),
            "gate hit MUST NOT signal a Set-Cookie")
    })
})
```

- [ ] **Step 2: Run**

```bash
go test -race -tags=integration -count=1 -v ./test/integration/auth/... -ginkgo.focus="two-tab guest scenario"
```

Expected: PASS.

- [ ] **Step 3: Lint + commit**

```bash
task fmt && task lint
```

```text
test(integration/auth): two-tab guest scenario gates the second create

Replicates the cmux-verified bug repro at spec §1.1 in Ginkgo at the
gateway-handler layer. Cookie middleware is exercised separately by
internal/web/cookie_test.go; this test threads the X-Session-Token
header directly to focus on gate semantics.

Refs holomush-9q8n.
```

---

### Task 17: Two-tab same-character scenario

**Files:**

- Modify: `test/integration/auth/multi_tab_test.go`

- [ ] **Step 1: Find or write a player+character seeder**

The existing suite at `test/integration/auth/auth_suite_test.go` likely has a helper to create a registered player with a password (search via `rg -n 'CreatePlayer|seedPlayer|hasher' test/integration/auth/auth_suite_test.go`). Use it. If not, the simplest approach is to call the existing `env.authService.CreatePlayer(ctx, username, password, email)` (matches `internal/auth/auth_service.go` API; verify exact signature with `rg -n 'func.*CreatePlayer' internal/auth/`), then `env.charRepo.Create(ctx, &world.Character{...})` to add a character.

- [ ] **Step 2: Add the Describe**

Append to `test/integration/auth/multi_tab_test.go`:

```go
var _ = Describe("Multi-tab session isolation — same character in two tabs", func() {
    var env *testEnv
    var gw *web.Handler

    BeforeEach(func() {
        env = setupTestEnv()
        gw = env.webHandler
    })

    It("both tabs reattach to one session and dual-tab command submission succeeds", func() {
        ctx := context.Background()

        // Seed: a registered player + character. CreatePlayer returns
        // (*Player, *PlayerSession, rawToken string, error) — see
        // internal/auth/registration.go:128.
        username := "two_tab_player"
        password := "correct horse battery staple"
        _, _, rawToken, err := env.authService.CreatePlayer(ctx, username, password, username+"@test.local")
        Expect(err).NotTo(HaveOccurred(), "seed: create player")
        Expect(rawToken).NotTo(BeEmpty())

        player, err := env.playerRepo.GetByUsername(ctx, username)
        Expect(err).NotTo(HaveOccurred())
        loc := createTestLocation(ctx, "seed-loc-"+username)
        char := createTestCharacter(ctx, player.ID, "char-"+username, loc.ID)
        charID := char.ID.String()

        // Tab 1: select the character.
        selReq1 := connect.NewRequest(&webv1.WebSelectCharacterRequest{CharacterId: charID})
        selReq1.Header().Set(web.HeaderInjectSessionToken, rawToken)
        selResp1, err := gw.WebSelectCharacter(ctx, selReq1)
        Expect(err).NotTo(HaveOccurred())
        Expect(selResp1.Msg.GetSuccess()).To(BeTrue())
        Expect(selResp1.Msg.GetSessionId()).NotTo(BeEmpty())

        // Tab 2: same cookie token, same character → reattach.
        selReq2 := connect.NewRequest(&webv1.WebSelectCharacterRequest{CharacterId: charID})
        selReq2.Header().Set(web.HeaderInjectSessionToken, rawToken)
        selResp2, err := gw.WebSelectCharacter(ctx, selReq2)
        Expect(err).NotTo(HaveOccurred())
        Expect(selResp2.Msg.GetSuccess()).To(BeTrue())
        Expect(selResp2.Msg.GetReattached()).To(BeTrue(), "tab 2 MUST reattach, not create a new session")
        Expect(selResp2.Msg.GetSessionId()).To(Equal(selResp1.Msg.GetSessionId()),
            "both tabs MUST land on the same session_id")

        // Both tabs can submit commands. HandleCommand validates ownership at
        // the player level (ValidateSessionOwnership → player_id), not by
        // connection_id, so both succeed.
        cmdReq1 := &corev1.HandleCommandRequest{
            SessionId:          selResp1.Msg.GetSessionId(),
            PlayerSessionToken: rawToken,
            Command:            "look",
        }
        cmdResp1, err := env.coreServer.HandleCommand(ctx, cmdReq1)
        Expect(err).NotTo(HaveOccurred())
        Expect(cmdResp1.GetSuccess()).To(BeTrue(), "tab 1 command MUST succeed")

        cmdReq2 := &corev1.HandleCommandRequest{
            SessionId:          selResp2.Msg.GetSessionId(), // same session_id
            PlayerSessionToken: rawToken,
            Command:            "look",
        }
        cmdResp2, err := env.coreServer.HandleCommand(ctx, cmdReq2)
        Expect(err).NotTo(HaveOccurred())
        Expect(cmdResp2.GetSuccess()).To(BeTrue(), "tab 2 command MUST succeed against the shared session")
    })
})

```

Update the test file's import block:

```go
import (
    "context"

    "connectrpc.com/connect"
    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    "github.com/holomush/holomush/internal/web"
    corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
    webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)
```

The seed pattern (`CreatePlayer` + `GetByUsername` + `createTestLocation` + `createTestCharacter`) is repeated in Tasks 18-21 verbatim. If readability suffers, extract a small `seedPlayerAndCharacter(ctx, env, username)` helper file-locally — but each task block below shows the inlined version for self-containment.

The `HandleCommand` assertions in this task and Tasks 18-21 require Task 15.6's dispatcher wiring. If that wiring fell back to "auth-only" mode, drop the `env.coreServer.HandleCommand(...)` lines and rely on the E2E test (Task 23) for end-to-end command coverage. The reattach + session-ID assertions remain meaningful regardless.

- [ ] **Step 3: Run**

```bash
go test -race -tags=integration -count=1 -v ./test/integration/auth/... -ginkgo.focus="same character in two tabs"
```

Expected: PASS.

- [ ] **Step 4: Lint + commit**

```bash
task fmt && task lint
```

```text
test(integration/auth): two-tab same-character reattach + dual command submission

Refs holomush-9q8n.
```

---

### Task 18: Two-tab different-character scenario

**Files:**

- Modify: `test/integration/auth/multi_tab_test.go`

- [ ] **Step 1: Add a slog capture helper if one doesn't exist**

```bash
rg -n 'slog.NewTextHandler|slog.SetDefault' test/integration/
```

If a helper exists, reuse it. Otherwise add to `multi_tab_test.go`:

```go
import (
    "bytes"
    "log/slog"
    "sync"
)

type captureHandler struct {
    buf *bytes.Buffer
    mu  sync.Mutex
    sub slog.Handler
}

func newCaptureHandler() (*captureHandler, *bytes.Buffer) {
    buf := &bytes.Buffer{}
    h := &captureHandler{buf: buf, sub: slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})}
    return h, buf
}

func (c *captureHandler) Enabled(ctx context.Context, l slog.Level) bool { return c.sub.Enabled(ctx, l) }
func (c *captureHandler) Handle(ctx context.Context, r slog.Record) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.sub.Handle(ctx, r)
}
func (c *captureHandler) WithAttrs(a []slog.Attr) slog.Handler { return &captureHandler{buf: c.buf, sub: c.sub.WithAttrs(a)} }
func (c *captureHandler) WithGroup(g string) slog.Handler      { return &captureHandler{buf: c.buf, sub: c.sub.WithGroup(g)} }
```

- [ ] **Step 2: Add the Describe**

```go
var _ = Describe("Multi-tab session isolation — two characters of one player", func() {
    var env *testEnv
    var gw *web.Handler
    var prevDefault *slog.Logger
    var logBuf *bytes.Buffer

    BeforeEach(func() {
        env = setupTestEnv()
        gw = env.webHandler

        prevDefault = slog.Default()
        h, buf := newCaptureHandler()
        slog.SetDefault(slog.New(h))
        logBuf = buf
    })

    AfterEach(func() {
        slog.SetDefault(prevDefault)
    })

    It("creates two distinct sessions and produces no ownership-mismatch warnings", func() {
        ctx := context.Background()

        // Seed: a registered player with two characters X and Y.
        username := "two_char_player"
        password := "correct horse battery staple"
        _, _, rawToken, err := env.authService.CreatePlayer(ctx, username, password, username+"@test.local")
        Expect(err).NotTo(HaveOccurred(), "seed: create player")

        player, err := env.playerRepo.GetByUsername(ctx, username)
        Expect(err).NotTo(HaveOccurred())
        loc := createTestLocation(ctx, "seed-loc-"+username)
        charX := createTestCharacter(ctx, player.ID, "char-X-"+username, loc.ID)
        charY := createTestCharacter(ctx, player.ID, "char-Y-"+username, loc.ID)
        charXID := charX.ID.String()
        charYID := charY.ID.String()
        Expect(charXID).NotTo(Equal(charYID))

        // Tab 1: select X.
        selReq1 := connect.NewRequest(&webv1.WebSelectCharacterRequest{CharacterId: charXID})
        selReq1.Header().Set(web.HeaderInjectSessionToken, rawToken)
        selResp1, err := gw.WebSelectCharacter(ctx, selReq1)
        Expect(err).NotTo(HaveOccurred())
        Expect(selResp1.Msg.GetSuccess()).To(BeTrue())

        // Tab 2: select Y (same cookie).
        selReq2 := connect.NewRequest(&webv1.WebSelectCharacterRequest{CharacterId: charYID})
        selReq2.Header().Set(web.HeaderInjectSessionToken, rawToken)
        selResp2, err := gw.WebSelectCharacter(ctx, selReq2)
        Expect(err).NotTo(HaveOccurred())
        Expect(selResp2.Msg.GetSuccess()).To(BeTrue())

        Expect(selResp2.Msg.GetSessionId()).NotTo(Equal(selResp1.Msg.GetSessionId()),
            "different characters MUST have distinct sessions")

        // Both sessions are alive and accept commands.
        for _, sessionID := range []string{selResp1.Msg.GetSessionId(), selResp2.Msg.GetSessionId()} {
            cmdReq := &corev1.HandleCommandRequest{
                SessionId:          sessionID,
                PlayerSessionToken: rawToken,
                Command:            "look",
            }
            cmdResp, err := env.coreServer.HandleCommand(ctx, cmdReq)
            Expect(err).NotTo(HaveOccurred())
            Expect(cmdResp.GetSuccess()).To(BeTrue())
        }

        Expect(logBuf.String()).NotTo(ContainSubstring("session ownership mismatch"),
            "two characters under one player MUST NOT trigger ownership-mismatch logs")
    })
})
```

- [ ] **Step 3: Run, commit**

```bash
go test -race -tags=integration -count=1 -v ./test/integration/auth/... -ginkgo.focus="two characters of one player"
task fmt && task lint
```

```text
test(integration/auth): two-tab different-character produces no ownership-mismatch logs

Refs holomush-9q8n.
```

---

### Task 19: Tab + telnet same character

**Files:**

- Modify: `test/integration/auth/multi_tab_test.go` (uses the existing telnet helpers if available)
- Reference: `test/integration/telnet/` (search for `telnetClient`, `dialTelnet`, or similar helpers)

- [ ] **Step 1: Inspect telnet helpers**

```bash
ls test/integration/telnet/
rg -n 'func.*[Tt]elnet|net\.Dial' test/integration/telnet/ | head
```

If a telnet client helper exists (e.g. `dialTelnet(addr) (*TelnetClient, error)`), import it. If telnet integration tests live under their own package, this Task may need to live there instead — choose the package whose harness is closest to what we need (telnet auth + character selection + send/receive).

- [ ] **Step 2: Add the Describe**

If reusing a telnet helper:

```go
var _ = Describe("Multi-tab session isolation — tab + telnet same character", func() {
    var env *testEnv
    var gw *web.Handler

    BeforeEach(func() {
        env = setupTestEnv()
        gw = env.webHandler
    })

    It("both connections attach to one session and see each other's emits", func() {
        ctx := context.Background()

        // Seed a player + one character (mirror Task 17).
        username := "tab_telnet_player"
        password := "correct horse battery staple"
        _, _, rawToken, err := env.authService.CreatePlayer(ctx, username, password, username+"@test.local")
        Expect(err).NotTo(HaveOccurred(), "seed: create player")

        player, err := env.playerRepo.GetByUsername(ctx, username)
        Expect(err).NotTo(HaveOccurred())
        loc := createTestLocation(ctx, "seed-loc-"+username)
        char := createTestCharacter(ctx, player.ID, "char-"+username, loc.ID)
        charID := char.ID.String()

        // Web tab attaches.
        selReq := connect.NewRequest(&webv1.WebSelectCharacterRequest{CharacterId: charID})
        selReq.Header().Set(web.HeaderInjectSessionToken, rawToken)
        selResp, err := gw.WebSelectCharacter(ctx, selReq)
        Expect(err).NotTo(HaveOccurred())
        sessionID := selResp.Msg.GetSessionId()

        // Telnet attaches as the same player + character. The telnet client
        // helper depends on the existing test infrastructure; if telnet auth
        // tests live in test/integration/telnet/, copy the dial+auth pattern
        // from there. The expected end state is:
        //   sessionStore.CountConnections(sessionID) ≥ 1 (telnet registered)
        // The web tab does NOT register a connection_id (verified at
        // web/src/routes/(authed)/terminal/+page.svelte: no connectionId set
        // on Subscribe), so the count reflects telnet's attachment only.
        telnet := dialTelnetAndAuth(env, username, password) // uses existing helper or inline pattern
        defer telnet.Close()
        Expect(telnet.SelectCharacter(charID)).To(Succeed())

        // sessionStore observability: verify the telnet connection registered.
        connCount, err := env.sessionStore.CountConnections(ctx, sessionID)
        Expect(err).NotTo(HaveOccurred())
        Expect(connCount).To(BeNumerically(">=", 1), "telnet connection MUST register on the shared session")

        // Web sends "say from web" → telnet receives it.
        _, err = env.coreServer.HandleCommand(ctx, &corev1.HandleCommandRequest{
            SessionId:          sessionID,
            PlayerSessionToken: rawToken,
            Command:            "say from web",
        })
        Expect(err).NotTo(HaveOccurred())
        Eventually(telnet.Output).Should(ContainSubstring("from web"))

        // Telnet sends "say from telnet" — assertion shape depends on the
        // telnet helper's send method; mirror the existing telnet integration
        // tests' send pattern.
        Expect(telnet.Send("say from telnet")).To(Succeed())

        // Web-side observation: subscribing or QueryHistory through the
        // gateway is heavy to wire here. For a minimal assertion, query the
        // events_audit table directly (test/integration/auth/auth_suite_test.go
        // already has a *sql.DB on env — verify name with `rg -n 'env\\.db|env\\.pool' test/integration/auth/`).
        // If the suite has a higher-level event-query helper, prefer it. If
        // not, drop the web-side observation and rely on Task 23's E2E to
        // cover bidirectional delivery.
    })
})
```

If a telnet helper doesn't exist or telnet integration is heavy to build for one test, **drop this scenario from Phase 4 and rely on the existing telnet integration suite + the web-only Tasks 16/17/18 for the multi-tab guarantee**. State that explicitly in the commit message.

- [ ] **Step 3: Run, commit**

```bash
go test -race -tags=integration -count=1 -v ./test/integration/auth/... -ginkgo.focus="tab \\+ telnet"
task fmt && task lint
```

```text
test(integration): tab + telnet attached to same character — both see each other

Refs holomush-9q8n.
```

If skipped: commit message body explains why and points to the existing telnet integration coverage that already verifies the shared-session multi-connection model.

---

### Task 20: Browser cookie + concurrent telnet auth (parity)

**Files:**

- Same package as Task 19

- [ ] **Step 1: Add the Describe**

```go
var _ = Describe("Multi-tab session isolation — browser cookie + concurrent telnet auth", func() {
    var env *testEnv
    var gw *web.Handler

    BeforeEach(func() {
        env = setupTestEnv()
        gw = env.webHandler
    })

    It("telnet auth bypasses the gateway gate; both PlayerSessions exist; reattach holds", func() {
        ctx := context.Background()

        // Seed a registered player.
        username := "parity_player"
        password := "correct horse battery staple"
        _, _, _, err := env.authService.CreatePlayer(ctx, username, password, username+"@test.local")
        Expect(err).NotTo(HaveOccurred(), "seed: create player")

        // Web: WebAuthenticatePlayer — cookie set, PlayerSession_W exists.
        webResp, err := gw.WebAuthenticatePlayer(ctx, connect.NewRequest(&webv1.WebAuthenticatePlayerRequest{
            Username: username,
            Password: password,
        }))
        Expect(err).NotTo(HaveOccurred())
        Expect(webResp.Msg.GetSuccess()).To(BeTrue())
        webToken := webResp.Header().Get(web.HeaderSetSessionToken)
        Expect(webToken).NotTo(BeEmpty())

        // Telnet: authenticate with the same username+password. This calls
        // env.authService.AuthenticatePlayer directly (telnet does not go
        // through the web gateway). The gate doesn't apply.
        telnetToken, _, err := env.authService.AuthenticatePlayer(ctx, username, password, "telnet-ua", "127.0.0.1")
        Expect(err).NotTo(HaveOccurred())
        Expect(telnetToken).NotTo(BeEmpty())
        Expect(telnetToken).NotTo(Equal(webToken), "telnet's PlayerSession is distinct from web's")

        // ListPlayerSessions reports ≥ 2 active sessions for this player.
        // Use whichever query the suite provides; if none, query the repo directly.
        // Find the player ID from the username.
        player, err := env.playerRepo.GetByUsername(ctx, username)
        Expect(err).NotTo(HaveOccurred())
        sessions, err := env.playerSessionStore.ListByPlayer(ctx, player.ID)
        Expect(err).NotTo(HaveOccurred())
        Expect(len(sessions)).To(BeNumerically(">=", 2),
            "browser + telnet MUST coexist as two PlayerSessions")
    })
})
```

- [ ] **Step 2: Run, commit**

```bash
go test -race -tags=integration -count=1 -v ./test/integration/auth/... -ginkgo.focus="browser cookie \\+ concurrent telnet"
task fmt && task lint
```

```text
test(integration): browser cookie + telnet auth parity — both succeed independently

Pins the §4.2.4 invariant: telnet auth never goes through the gateway,
so the cookie-collision gate does not impede telnet at all.

Refs holomush-9q8n.
```

---

### Task 21: Logout in tab 1, action in tab 2

**Files:**

- Modify: `test/integration/auth/multi_tab_test.go`

- [ ] **Step 1: Add the Describe**

```go
var _ = Describe("Multi-tab session isolation — logout in tab 1, action in tab 2", func() {
    var env *testEnv
    var gw *web.Handler

    BeforeEach(func() {
        env = setupTestEnv()
        gw = env.webHandler
    })

    It("each call site from spec §4.4.5 returns SESSION_NOT_FOUND after logout", func() {
        ctx := context.Background()

        // Seed: registered player with one character.
        username := "logout_player"
        password := "correct horse battery staple"
        _, _, rawToken, err := env.authService.CreatePlayer(ctx, username, password, username+"@test.local")
        Expect(err).NotTo(HaveOccurred(), "seed: create player")

        player, err := env.playerRepo.GetByUsername(ctx, username)
        Expect(err).NotTo(HaveOccurred())
        loc := createTestLocation(ctx, "seed-loc-"+username)
        char := createTestCharacter(ctx, player.ID, "char-"+username, loc.ID)
        charID := char.ID.String()

        // Tab 1 + Tab 2 both select the character. Both have sessionID.
        selReq := connect.NewRequest(&webv1.WebSelectCharacterRequest{CharacterId: charID})
        selReq.Header().Set(web.HeaderInjectSessionToken, rawToken)
        selResp, err := gw.WebSelectCharacter(ctx, selReq)
        Expect(err).NotTo(HaveOccurred())
        sessionID := selResp.Msg.GetSessionId()

        // Tab 1: WebLogout. Token revoked.
        _, err = env.coreServer.Logout(ctx, &corev1.LogoutRequest{
            PlayerSessionToken: rawToken,
        })
        Expect(err).NotTo(HaveOccurred())

        // Tab 2 retries each of the four call sites with the (now-stale) token.

        // 1. WebQueryStreamHistory.
        qReq := connect.NewRequest(&webv1.WebQueryStreamHistoryRequest{
            SessionId: sessionID,
            Stream:    "character:" + charID,
            Count:     10,
        })
        qReq.Header().Set(web.HeaderInjectSessionToken, rawToken)
        _, err = gw.WebQueryStreamHistory(ctx, qReq)
        Expect(err).To(HaveOccurred(), "WebQueryStreamHistory MUST surface stale-session error")

        // 2. WebListSessionStreams.
        lReq := connect.NewRequest(&webv1.WebListSessionStreamsRequest{SessionId: sessionID})
        lReq.Header().Set(web.HeaderInjectSessionToken, rawToken)
        _, err = gw.WebListSessionStreams(ctx, lReq)
        Expect(err).To(HaveOccurred(), "WebListSessionStreams MUST surface stale-session error")

        // 3. HandleCommand on the core server.
        _, err = env.coreServer.HandleCommand(ctx, &corev1.HandleCommandRequest{
            SessionId:          sessionID,
            PlayerSessionToken: rawToken,
            Command:            "look",
        })
        Expect(err).To(HaveOccurred(), "HandleCommand MUST reject stale token")

        // 4. Subscribe — only verifies that opening the stream errors quickly.
        // We can't easily consume the stream here without the gateway's
        // server-streaming machinery; instead, verify ValidateSessionOwnership
        // rejects the token directly:
        _, err = env.coreServer.GetCommandHistory(ctx, &corev1.GetCommandHistoryRequest{
            SessionId:          sessionID,
            PlayerSessionToken: rawToken,
        })
        Expect(err).To(HaveOccurred(), "any cookie-validating RPC MUST reject the revoked token")
    })
})
```

If `GetCommandHistory` doesn't exist or doesn't validate via the same path, substitute another small RPC that runs `ValidateSessionOwnership` (e.g. `ListPlayerSessions`).

- [ ] **Step 2: Run, commit**

```bash
go test -race -tags=integration -count=1 -v ./test/integration/auth/... -ginkgo.focus="logout in tab 1"
task fmt && task lint
```

```text
test(integration/auth): post-logout stale tab returns SESSION_NOT_FOUND on each call site

Pins the §4.4.5 audit: each call site must surface the stale-session
error rather than silently succeeding.

Refs holomush-9q8n.
```

---

### Task 22: Pre-deploy `WebCheckSession` regression guard

**Files:**

- Modify: `test/integration/auth/multi_tab_test.go`

- [ ] **Step 1: Add the Describe**

```go
Describe("Pre-deploy WebCheckSession contract", func() {
    It("still throws / returns Unauthenticated on auth failure", func() {
        ctx := context.Background()
        req := connect.NewRequest(&webv1.WebCheckSessionRequest{})
        // No token set.
        _, err := gw.WebCheckSession(ctx, req)
        Expect(err).To(HaveOccurred(), "auth-failure path MUST still return an error response")
        var connectErr *connect.Error
        Expect(errors.As(err, &connectErr)).To(BeTrue())
        Expect(connectErr.Code()).To(Equal(connect.CodeUnauthenticated))
    })

    It("still populates player_name on success and now also player_id, is_guest, characters", func() {
        ctx := context.Background()

        // Mint a guest, capture the token, call WebCheckSession.
        guestResp, err := gw.WebCreateGuest(ctx, connect.NewRequest(&webv1.WebCreateGuestRequest{}))
        Expect(err).NotTo(HaveOccurred())
        token := guestResp.Header().Get(web.HeaderSetSessionToken)

        req := connect.NewRequest(&webv1.WebCheckSessionRequest{})
        req.Header().Set(web.HeaderInjectSessionToken, token)
        resp, err := gw.WebCheckSession(ctx, req)
        Expect(err).NotTo(HaveOccurred())
        Expect(resp.Msg.GetPlayerName()).NotTo(BeEmpty())
        Expect(resp.Msg.GetPlayerId()).NotTo(BeEmpty())
        Expect(resp.Msg.GetIsGuest()).To(BeTrue())
        Expect(resp.Msg.GetCharacters()).To(HaveLen(1))
    })
})
```

Add `"errors"` to the test file's import block if it isn't already present.
```

- [ ] **Step 2: Run the full integration suite**

```bash
task test:int
```

Expected: PASS.

- [ ] **Step 3: Lint + commit**

```bash
task fmt && task lint
```

```text
test(integration): WebCheckSession back-compat — throw on failure + player_name on success

Pins the §6 migration claim that pre-deploy (authed)/+layout.ts callers
keep working without modification, and that the new fields populate too.

Refs holomush-9q8n.
```

---

## Phase 5 — End-to-end (Playwright)

### Task 23: Cmux/Playwright two-tab E2E test

**Files:**

- Create: `web/e2e/multi-tab-session.spec.ts`

The Playwright test directory is `web/e2e/` (verified via `ls web/e2e/`). Test runner is `task test:e2e` which boots a Docker compose stack — first run takes ~2 minutes for the compose build.

- [ ] **Step 1: Inspect the existing Playwright pattern**

```bash
ls web/e2e/
cat web/e2e/auth.spec.ts | head -80
```

Note imports, test fixtures, and the way existing specs hit the dev server.

- [ ] **Step 2: Write the spec**

Create `web/e2e/multi-tab-session.spec.ts`:

```typescript
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test, expect } from '@playwright/test';

test('multi-tab guest creation no longer breaks tab 1', async ({ browser }) => {
  // Single browser context = shared cookie jar across tabs (matches a real browser).
  const ctx = await browser.newContext();

  const tab1 = await ctx.newPage();
  await tab1.goto('/');
  await tab1.getByTestId('guest-button').click();
  await expect(tab1).toHaveURL(/\/terminal$/, { timeout: 15_000 });

  const tab2 = await ctx.newPage();
  await tab2.goto('/');
  // Tab 2 should land on the authenticated landing branch.
  await expect(tab2.getByTestId('continue-button')).toBeVisible({ timeout: 15_000 });
  await expect(tab2.getByTestId('guest-button')).not.toBeVisible();

  // Tab 1 still able to send.
  // Use the existing terminal helpers from web/e2e/helpers/ if available;
  // otherwise type into the input and assert the echo appears.
  await tab1.bringToFront();
  // ... existing terminal interaction helpers, mirroring web/e2e/terminal.spec.ts ...

  await ctx.close();
});
```

The base URL is configured by `web/playwright.config.ts` and points at the dev server stood up by `task test:e2e`. Don't hardcode `localhost:8080`.

- [ ] **Step 3: Make sure the docker dev stack is built**

```bash
task docker:build
```

(Or whatever the project's E2E pre-step is — check `Taskfile.yaml` `test:e2e` deps.)

- [ ] **Step 4: Run**

```bash
task test:e2e -- --grep "multi-tab"
```

Expected: PASS. Runtime ≈ 30s after the docker stack is warm; ≈ 3 min on a cold start.

- [ ] **Step 5: Lint + commit**

```bash
task fmt && task lint
```

```text
test(e2e): multi-tab session isolation — repro from §1.1 no longer reproduces

Tab 2 lands on the authenticated landing branch instead of clobbering
tab 1's cookie. Tab 1 stays "live."

Refs holomush-9q8n.
```

---

## Phase 6 — Final gate

### Task 24: `task pr-prep` green

- [ ] **Step 1: Run pr-prep**

```bash
task pr-prep
```

Expected: all jobs PASS — lint, format, schema, license, unit, integration, E2E. Zero failures.

- [ ] **Step 2: Fix any failures in place**

If anything fails: fix, re-run. Do NOT push to a PR branch without a green pr-prep (per CLAUDE.md "Pre-Push Review Gates" + "feedback_pr_prep_must_run" memory).

- [ ] **Step 3: Final commit only if pr-prep wrote anything**

If running pr-prep modified files (formatter run, lint --fix, license headers):

```text
chore: task pr-prep clean-up

Refs holomush-9q8n.
```

---

## Phase 7 — Code review + push

### Task 25: Code review and remediation

- [ ] **Step 1: Run the code-reviewer subagent**

Per CLAUDE.md "Pre-Push Review Gates", the `code-reviewer` agent runs before `bd close`, `jj git push`, or PR creation. Invoke it via `/review-code`.

- [ ] **Step 2: Address findings**

Each fix gets its own commit. Re-run `task pr-prep` after.

- [ ] **Step 3: Close the bead**

```bash
bd close holomush-9q8n
bd dolt push
```

- [ ] **Step 4: Push (jj-colocated)**

Per CLAUDE.md "Landing the Plane":

```bash
jj git fetch
jj rebase -r <change-id> -d main@origin    # targeted, NEVER bare `jj rebase -d main`
jj bookmark set <branch> -r @-
jj git push --branch <branch>
jj st                                       # verify clean
```

- [ ] **Step 5: Open the PR**

```bash
gh pr create --title "fix(web): multi-tab session isolation (holomush-9q8n)" --body "$(cat <<'EOF'
## Summary

Closes holomush-9q8n. Multi-tab guest sessions in the same browser no
longer break each other. Cookie stays HttpOnly; no XSS exposure increase.

- Server: `WebCreateGuest`, `WebAuthenticatePlayer`, `WebCreatePlayer`
  return `ALREADY_AUTHENTICATED` when called with a valid cookie. Cookie
  not modified on gate hit. Core RPCs unchanged. Telnet bypasses
  naturally.
- Server: `CheckPlayerSession` / `WebCheckSession` success response
  gains `player_id`, `is_guest`, `characters`. Failure-path
  `connect.CodeUnauthenticated` contract preserved so existing
  `(authed)/+layout.ts` redirect path keeps working.
- Client: landing/login/register pages probe `webCheckSession()` in
  `+page.ts` `load()` and branch on success/throw. `handleGuest` /
  `handleLogin` / `handleRegister` also pre-gate on `webCheckSession`
  before the create/auth call.
- Client: terminal page routes to `/` on `SESSION_NOT_FOUND` /
  `SESSION_EXPIRED` from any of Subscribe / WebQueryStreamHistory /
  WebListSessionStreams / HandleCommand.

## Spec / plan

- Spec: `docs/superpowers/specs/2026-04-25-multi-tab-session-isolation-design.md`
- Plan: `docs/superpowers/plans/2026-04-25-multi-tab-session-isolation.md`

## Test plan

- [x] `task test` green
- [x] `task test:int` green (new `test/integration/auth/multi_tab_test.go` covers the seven Phase 4 scenarios)
- [x] `task test:e2e` green (new `web/e2e/multi-tab-session.spec.ts` pins the §1.1 repro)
- [x] `task pr-prep` green

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Self-review checklist

Mapping every spec section / requirement to a task:

- Spec §1 (background / repro) → background only; not implemented
- Spec §2 goals (six MUSTs) → all covered: G1-G3 by Tasks 16-21; G4 by Tasks 7-9 + Task 11; G5 by §4.2.0 (gate is gateway-only, cookie not changed); G6 by Tasks 4 + 5 (failure contract preserved) + Task 20 (telnet parity)
- Spec §3 non-goals → respected (no concurrent identities, no cross-tab logout, no subdomain partitioning, related beads deferred)
- Spec §4.1 identity model → §4.5 components map directly to Task 5 (RPC), Task 6-9 (gates), Task 10 (auth state), Tasks 11-15 (UI)
- Spec §4.2.0 gate location → Tasks 6-9
- Spec §4.2.1 `WebCreateGuest` gate → Task 7
- Spec §4.2.2 `WebAuthenticatePlayer` gate → Task 8
- Spec §4.2.3 `WebCreatePlayer` gate → Task 9
- Spec §4.2.4 telnet path unchanged → Task 20 (parity test)
- Spec §4.2.5 race-window characterisation → Task 7 step 5 (concurrent test) + plan-text §4.2.5 cited
- Spec §4.3 `CheckPlayerSession` extension → Tasks 1, 2, 4, 5
- Spec §4.3.1 enumeration safety / TTL refresh → Task 4 (TTL inherited from `resolvePlayerSession:121`) + Task 5 (failure-contract test)
- Spec §4.4.1 landing → Task 11
- Spec §4.4.2 login + register → Tasks 12, 13
- Spec §4.4.3 new-tab flow → Tasks 11, 14
- Spec §4.4.4 client-side defence-in-depth → Task 11 (pre-gate) + Tasks 7-9 (server backstop)
- Spec §4.4.5 stale sessionStorage audit → Task 14
- Spec §5 test plan → Tasks 4, 5, 7, 8, 9 (unit/gate); Tasks 16-22 (integration); Task 23 (E2E)
- Spec §6 migration → Task 22 (regression guard) + Task 24 (pr-prep)
- Spec §7 open questions → Task 11's `handleContinue` implements "auto-select if exactly one character"; eviction interaction noted in spec
- Spec §8 files-touched → all listed in this plan's File Structure table

**Placeholder scan:** no TBD / TODO / FIXME / "implement later" / "similar to Task N" without showing the code. Every test snippet shows the actual code; every implementation step shows the actual code. Conditional steps (e.g. Task 11 step 1 inspection of an existing `+page.ts`) are followed by concrete instructions for both branches.

**Type / name consistency:**

- `checkCookieCollision(ctx, headers) → (string, bool, error)` — defined in Task 6; called identically in Tasks 7, 8, 9.
- `setPlayerProfile({playerId, playerName, isGuest, characters})` — defined in Task 10; called identically in Tasks 11, 12, 13, 15.
- `isStaleSession(e: unknown) → boolean` — defined in Task 14; reused in `streamBackfill.ts` per same task.
- `handleStaleSession()` — defined in Task 14.
- `isPlayerSessionAuthFailure(err) → bool` — defined in Task 6 (only used internally by `checkCookieCollision`).
- `mockCoreClient.checkSessionCalls / createGuestCalls / authPlayerCalls / createPlayerCalls / checkSessionReq` — added in Task 0; referenced by Tasks 5-9 tests.

**Phase ordering:** every task only depends on earlier tasks. Task 0 (mock extension) precedes Tasks 5-9 (which use the new mock fields). Task 1-3 (proto) precede Task 4-9 (which use the new generated types). Task 6 (helper) precedes Tasks 7-9 (which call it). Task 10 (`setPlayerProfile`) precedes Tasks 11-15. Phase 4 integration tests come after the implementation phases. Phase 5 E2E comes after Phase 4. Phase 6 pr-prep is the gate. Phase 7 is review + push.
