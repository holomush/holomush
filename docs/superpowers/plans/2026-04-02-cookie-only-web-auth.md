# Cookie-Only Web Auth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove `player_session_token` from web-facing proto messages so web RPCs authenticate exclusively via the HttpOnly cookie.

**Architecture:** The CookieMiddleware already injects the cookie value into `X-Session-Token` on inbound requests. We remove the token from proto messages, add a `playerTokenFromHeader` helper for web handlers to read from the header, add a `CheckPlayerSession` core RPC for page-reload validation, and update the SvelteKit client to use an explicit `isPlayerAuthenticated` boolean instead of storing the raw token.

**Tech Stack:** Protobuf, ConnectRPC, Go, SvelteKit/TypeScript, Playwright E2E

**Spec:** `docs/superpowers/specs/2026-04-02-cookie-only-web-auth-design.md`

---

## Task 1: Proto Changes — Remove Fields and Add RPCs

**Files:**

- Modify: `api/proto/holomush/web/v1/web.proto:139-197` (remove 6 fields, add RPC + messages)
- Modify: `api/proto/holomush/core/v1/core.proto:25-64` (add RPC to service block)
- Modify: `api/proto/holomush/core/v1/core.proto:245-249` (add messages after LogoutResponse)

- [ ] **Step 1: Remove `player_session_token` from web.proto messages**

Edit `api/proto/holomush/web/v1/web.proto`:

```protobuf
// WebAuthenticatePlayerResponse — remove field 2 (player_session_token)
message WebAuthenticatePlayerResponse {
  bool success = 1;
  string error_message = 3;
  repeated CharacterSummary characters = 4;
  string default_character_id = 5;
}

// WebSelectCharacterRequest — remove field 1 (player_session_token)
message WebSelectCharacterRequest {
  string character_id = 2;
}

// WebCreatePlayerResponse — remove field 2 (player_session_token)
message WebCreatePlayerResponse {
  bool success = 1;
  repeated CharacterSummary characters = 3;
  string error_message = 4;
}

// WebCreateCharacterRequest — remove field 1 (player_session_token)
message WebCreateCharacterRequest {
  string character_name = 2;
}

// WebListCharactersRequest — remove field 1 (player_session_token), now empty
message WebListCharactersRequest {}

// WebLogoutRequest — remove field 1 (player_session_token), now empty
message WebLogoutRequest {}
```

Field numbers on remaining fields stay the same. Do NOT renumber.

- [ ] **Step 2: Add WebCheckSession RPC and messages to web.proto**

Add to the `WebService` service block (after `WebConfirmPasswordReset`):

```protobuf
  // Validate player session from cookie. Returns player info or Unauthenticated error.
  rpc WebCheckSession(WebCheckSessionRequest) returns (WebCheckSessionResponse);
```

Add messages at the end of the web auth messages section (before the content messages):

```protobuf
message WebCheckSessionRequest {}

message WebCheckSessionResponse {
  string player_name = 1;
}
```

- [ ] **Step 3: Add CheckPlayerSession RPC and messages to core.proto**

Add to the `CoreService` service block (after `Logout`):

```protobuf
  // Validate a player session token. Used by web gateway for cookie-based auth checks.
  rpc CheckPlayerSession(CheckPlayerSessionRequest) returns (CheckPlayerSessionResponse);
```

Add messages after `LogoutResponse`:

```protobuf
message CheckPlayerSessionRequest {
  string player_session_token = 1;
}

message CheckPlayerSessionResponse {
  string player_name = 1;
}
```

- [ ] **Step 4: Regenerate Go and TypeScript proto code**

Run:

```bash
task proto && task web:generate
```

Expected: Both commands succeed. Generated files in `pkg/proto/` and `web/src/lib/connect/` update.

- [ ] **Step 5: Verify proto generation compiled cleanly**

Run:

```bash
task build 2>&1 | tail -5
```

Expected: Build will FAIL because handlers reference removed fields. This is expected — we fix them in Tasks 2-4.

- [ ] **Step 6: Commit**

```bash
jj --no-pager describe -m "proto(web): remove player_session_token fields, add CheckSession RPCs"
jj --no-pager new
```

---

## Task 2: Core Server — Add CheckPlayerSession Handler

**Files:**

- Modify: `internal/grpc/auth_handlers.go:446` (add handler after `Logout`)
- Modify: `internal/grpc/client.go:215` (add client method after `Logout`)
- Modify: `internal/web/handler.go:36-51` (add method to `CoreClient` interface)
- Test: `internal/grpc/auth_handlers_test.go`

- [ ] **Step 1: Write failing tests for CheckPlayerSession**

Add to `internal/grpc/auth_handlers_test.go` (after the existing `resolvePlayerSession` tests around line 873):

```go
// --- CheckPlayerSession ---

func TestCheckPlayerSession_Success(t *testing.T) {
	playerID := ulid.Make()
	ps := makePlayerSession(playerID)

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	tokenHash := auth.HashSessionToken(validToken)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, tokenHash).Return(ps, nil)
	sessionRepo.EXPECT().RefreshTTL(mock.Anything, ps.ID, auth.PlayerSessionTTL).Return(nil)

	playerRepo := authmocks.NewMockPlayerRepository(t)
	playerRepo.EXPECT().GetByID(mock.Anything, playerID).Return(&auth.Player{
		ID:       playerID,
		Username: "alice",
	}, nil)

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
		playerRepo:        playerRepo,
	}

	resp, err := server.CheckPlayerSession(context.Background(), &corev1.CheckPlayerSessionRequest{
		PlayerSessionToken: validToken,
	})
	require.NoError(t, err)
	assert.Equal(t, "alice", resp.GetPlayerName())
}

func TestCheckPlayerSession_InvalidToken(t *testing.T) {
	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	tokenHash := auth.HashSessionToken("bad-token")
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, tokenHash).
		Return(nil, auth.ErrNotFound)

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
	}

	resp, err := server.CheckPlayerSession(context.Background(), &corev1.CheckPlayerSessionRequest{
		PlayerSessionToken: "bad-token",
	})
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestCheckPlayerSession_RepoNotConfigured(t *testing.T) {
	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore()),
		sessionStore: session.NewMemStore(),
		// playerSessionRepo is nil
	}

	resp, err := server.CheckPlayerSession(context.Background(), &corev1.CheckPlayerSessionRequest{
		PlayerSessionToken: "some-token",
	})
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestCheckPlayerSession_PlayerRepoNotConfigured(t *testing.T) {
	playerID := ulid.Make()
	ps := makePlayerSession(playerID)

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	tokenHash := auth.HashSessionToken(validToken)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, tokenHash).Return(ps, nil)
	sessionRepo.EXPECT().RefreshTTL(mock.Anything, ps.ID, auth.PlayerSessionTTL).Return(nil)

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
		// playerRepo is nil
	}

	resp, err := server.CheckPlayerSession(context.Background(), &corev1.CheckPlayerSessionRequest{
		PlayerSessionToken: validToken,
	})
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
task test -- -run 'TestCheckPlayerSession' ./internal/grpc/
```

Expected: FAIL — `CheckPlayerSession` method does not exist yet.

- [ ] **Step 3: Implement CheckPlayerSession on CoreServer**

Add to `internal/grpc/auth_handlers.go` after the `Logout` method:

```go
// CheckPlayerSession validates a player session token and returns the player name.
func (s *CoreServer) CheckPlayerSession(ctx context.Context, req *corev1.CheckPlayerSessionRequest) (*corev1.CheckPlayerSessionResponse, error) {
	slog.DebugContext(ctx, "grpc: CheckPlayerSession")

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

	return &corev1.CheckPlayerSessionResponse{
		PlayerName: player.Username,
	}, nil
}
```

- [ ] **Step 4: Add CheckPlayerSession to gRPC client**

Add to `internal/grpc/client.go` after the `Logout` method (around line 215):

```go
// CheckPlayerSession validates a player session token.
func (c *Client) CheckPlayerSession(ctx context.Context, req *corev1.CheckPlayerSessionRequest) (*corev1.CheckPlayerSessionResponse, error) {
	resp, err := c.client.CheckPlayerSession(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "CheckPlayerSession").Wrap(err)
	}
	return resp, nil
}
```

- [ ] **Step 5: Add CheckPlayerSession to CoreClient interface**

Add to the `CoreClient` interface in `internal/web/handler.go` (around line 50, after `Logout`):

```go
	CheckPlayerSession(ctx context.Context, req *corev1.CheckPlayerSessionRequest) (*corev1.CheckPlayerSessionResponse, error)
```

- [ ] **Step 6: Add CheckPlayerSession to mockCoreClient**

Add field and method to `internal/web/handler_test.go`:

In the `mockCoreClient` struct (around line 70, after the logout fields):

```go
	checkSessionResp *corev1.CheckPlayerSessionResponse
	checkSessionErr  error
```

Add method (after the `Logout` method, around line 138):

```go
func (m *mockCoreClient) CheckPlayerSession(_ context.Context, _ *corev1.CheckPlayerSessionRequest) (*corev1.CheckPlayerSessionResponse, error) {
	return m.checkSessionResp, m.checkSessionErr
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run:

```bash
task test -- -run 'TestCheckPlayerSession' ./internal/grpc/
```

Expected: All 4 tests PASS.

- [ ] **Step 8: Commit**

```bash
jj --no-pager describe -m "feat(grpc): add CheckPlayerSession core RPC handler"
jj --no-pager new
```

---

## Task 3: Web Gateway — Token Extraction Helper and Handler Updates

**Files:**

- Modify: `internal/web/auth_handlers.go:1-262` (add helper, update 6 handlers, add new handler)
- Test: `internal/web/auth_handlers_test.go`

- [ ] **Step 1: Write tests for playerTokenFromHeader**

Add to `internal/web/auth_handlers_test.go` (at the top, after the import block):

```go
// --- playerTokenFromHeader ---

func TestPlayerTokenFromHeader_Present(t *testing.T) {
	h := http.Header{}
	h.Set(headerInjectSessionToken, "tok-abc")
	token, err := playerTokenFromHeader(h)
	require.NoError(t, err)
	assert.Equal(t, "tok-abc", token)
}

func TestPlayerTokenFromHeader_Missing(t *testing.T) {
	h := http.Header{}
	token, err := playerTokenFromHeader(h)
	assert.Empty(t, token)
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}

func TestPlayerTokenFromHeader_Empty(t *testing.T) {
	h := http.Header{}
	h.Set(headerInjectSessionToken, "")
	token, err := playerTokenFromHeader(h)
	assert.Empty(t, token)
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
task test -- -run 'TestPlayerTokenFromHeader' ./internal/web/
```

Expected: FAIL — `playerTokenFromHeader` does not exist.

- [ ] **Step 3: Implement playerTokenFromHeader**

Add to `internal/web/auth_handlers.go` (after the header constant block, around line 23):

```go
// playerTokenFromHeader extracts the player session token from the
// X-Session-Token header injected by CookieMiddleware. Returns
// CodeUnauthenticated if the header is missing or empty.
func playerTokenFromHeader(h http.Header) (string, error) {
	token := h.Get(headerInjectSessionToken)
	if token == "" {
		return "", connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("no player session"))
	}
	return token, nil
}
```

Add `"net/http"` to the import block if not already present.

- [ ] **Step 4: Run helper tests to verify they pass**

Run:

```bash
task test -- -run 'TestPlayerTokenFromHeader' ./internal/web/
```

Expected: All 3 tests PASS.

- [ ] **Step 5: Write tests for WebCheckSession**

Add to `internal/web/auth_handlers_test.go`:

```go
// --- WebCheckSession ---

func TestWebCheckSession_Success(t *testing.T) {
	client := &mockCoreClient{
		checkSessionResp: &corev1.CheckPlayerSessionResponse{
			PlayerName: "alice",
		},
	}
	h := NewHandler(client)

	req := connect.NewRequest(&webv1.WebCheckSessionRequest{})
	req.Header().Set(headerInjectSessionToken, "tok-abc")
	resp, err := h.WebCheckSession(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "alice", resp.Msg.GetPlayerName())
}

func TestWebCheckSession_NoCookie(t *testing.T) {
	client := &mockCoreClient{}
	h := NewHandler(client)

	req := connect.NewRequest(&webv1.WebCheckSessionRequest{})
	// No X-Session-Token header
	_, err := h.WebCheckSession(context.Background(), req)
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}

func TestWebCheckSession_CoreRPCError(t *testing.T) {
	client := &mockCoreClient{
		checkSessionErr: errors.New("session expired"),
	}
	h := NewHandler(client)

	req := connect.NewRequest(&webv1.WebCheckSessionRequest{})
	req.Header().Set(headerInjectSessionToken, "tok-expired")
	_, err := h.WebCheckSession(context.Background(), req)
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}
```

- [ ] **Step 6: Update existing web handler tests to use header instead of message body**

Update tests in `internal/web/auth_handlers_test.go` that set `PlayerSessionToken` in the request message to instead set the header. Examples:

`TestWebSelectCharacter_Success` (and Reattached, RPCError):

```go
// Before:
resp, err := h.WebSelectCharacter(context.Background(), connect.NewRequest(&webv1.WebSelectCharacterRequest{
    PlayerSessionToken: "tok-abc",
    CharacterId: "c1",
}))

// After:
req := connect.NewRequest(&webv1.WebSelectCharacterRequest{
    CharacterId: "c1",
})
req.Header().Set(headerInjectSessionToken, "tok-abc")
resp, err := h.WebSelectCharacter(context.Background(), req)
```

Apply the same pattern to all tests for:

- `WebSelectCharacter` (3 tests: Success, Reattached, RPCError)
- `WebCreateCharacter` (tests that set `PlayerSessionToken`)
- `WebListCharacters` (tests that set `PlayerSessionToken`)
- `WebLogout` (tests that set `PlayerSessionToken`)

For tests asserting `resp.Msg.GetPlayerSessionToken()`, remove those assertions (the field no longer exists in the response).

Update `TestWebAuthenticatePlayer_Success` and `TestWebAuthenticatePlayer_NoRememberMe`: remove assertions on `resp.Msg.GetPlayerSessionToken()`. Keep the `resp.Header().Get(headerSetSessionToken)` assertion — the cookie header is still set.

Update `TestWebCreatePlayer_Success`: remove assertion on `resp.Msg.GetPlayerSessionToken()`. Keep the header assertion.

- [ ] **Step 7: Implement WebCheckSession handler**

Add to `internal/web/auth_handlers.go` (after `WebConfirmPasswordReset`, before `translateCharacterSummaries`):

```go
// WebCheckSession validates the player session from the cookie and returns the player name.
func (h *Handler) WebCheckSession(ctx context.Context, req *connect.Request[webv1.WebCheckSessionRequest]) (*connect.Response[webv1.WebCheckSessionResponse], error) {
	slog.DebugContext(ctx, "web: WebCheckSession")

	token, err := playerTokenFromHeader(req.Header())
	if err != nil {
		return nil, err
	}

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.CheckPlayerSession(rpcCtx, &corev1.CheckPlayerSessionRequest{
		PlayerSessionToken: token,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("session expired or invalid"))
	}

	return connect.NewResponse(&webv1.WebCheckSessionResponse{
		PlayerName: coreResp.GetPlayerName(),
	}), nil
}
```

- [ ] **Step 8: Update existing handlers to read token from header**

In `internal/web/auth_handlers.go`:

**WebAuthenticatePlayer** (line 48-55) — remove `PlayerSessionToken` from response:

```go
	resp := connect.NewResponse(&webv1.WebAuthenticatePlayerResponse{
		Success:            true,
		Characters:         translateCharacterSummaries(coreResp.GetCharacters()),
		DefaultCharacterId: coreResp.GetDefaultCharacterId(),
	})
	resp.Header().Set(headerSetSessionToken, coreResp.GetPlayerSessionToken())
	return resp, nil
```

**WebSelectCharacter** (line 59-89) — read token from header:

```go
func (h *Handler) WebSelectCharacter(ctx context.Context, req *connect.Request[webv1.WebSelectCharacterRequest]) (*connect.Response[webv1.WebSelectCharacterResponse], error) {
	slog.DebugContext(ctx, "web: WebSelectCharacter", "character_id", req.Msg.GetCharacterId())

	token, err := playerTokenFromHeader(req.Header())
	if err != nil {
		return nil, err
	}

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.SelectCharacter(rpcCtx, &corev1.SelectCharacterRequest{
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
	})
	if err != nil {
		slog.Error("web: select character RPC failed", "error", err)
		return connect.NewResponse(&webv1.WebSelectCharacterResponse{
			Success: false, ErrorMessage: "character selection error",
		}), nil
	}
	if !coreResp.GetSuccess() {
		return connect.NewResponse(&webv1.WebSelectCharacterResponse{
			Success: false, ErrorMessage: coreResp.GetErrorMessage(),
		}), nil
	}

	resp := connect.NewResponse(&webv1.WebSelectCharacterResponse{
		Success:       true,
		SessionId:     coreResp.GetSessionId(),
		CharacterName: coreResp.GetCharacterName(),
		Reattached:    coreResp.GetReattached(),
	})
	resp.Header().Set(headerSetSessionToken, coreResp.GetSessionId())
	return resp, nil
}
```

**WebCreatePlayer** (line 93-123) — remove `PlayerSessionToken` from response, no header read needed (this handler creates the session):

```go
	resp := connect.NewResponse(&webv1.WebCreatePlayerResponse{
		Success:    true,
		Characters: translateCharacterSummaries(coreResp.GetCharacters()),
	})
	resp.Header().Set(headerSetSessionToken, coreResp.GetPlayerSessionToken())
	return resp, nil
```

**WebCreateCharacter** (line 126-153) — read token from header:

```go
func (h *Handler) WebCreateCharacter(ctx context.Context, req *connect.Request[webv1.WebCreateCharacterRequest]) (*connect.Response[webv1.WebCreateCharacterResponse], error) {
	slog.DebugContext(ctx, "web: WebCreateCharacter", "character_name", req.Msg.GetCharacterName())

	token, err := playerTokenFromHeader(req.Header())
	if err != nil {
		return nil, err
	}

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.CreateCharacter(rpcCtx, &corev1.CreateCharacterRequest{
		PlayerSessionToken: token,
		CharacterName:      req.Msg.GetCharacterName(),
	})
	if err != nil {
		slog.Error("web: create character RPC failed", "error", err)
		return connect.NewResponse(&webv1.WebCreateCharacterResponse{
			Success: false, ErrorMessage: "character creation error",
		}), nil
	}
	if !coreResp.GetSuccess() {
		return connect.NewResponse(&webv1.WebCreateCharacterResponse{
			Success: false, ErrorMessage: coreResp.GetErrorMessage(),
		}), nil
	}

	return connect.NewResponse(&webv1.WebCreateCharacterResponse{
		Success:       true,
		CharacterId:   coreResp.GetCharacterId(),
		CharacterName: coreResp.GetCharacterName(),
	}), nil
}
```

**WebListCharacters** (line 156-173) — read token from header:

```go
func (h *Handler) WebListCharacters(ctx context.Context, req *connect.Request[webv1.WebListCharactersRequest]) (*connect.Response[webv1.WebListCharactersResponse], error) {
	slog.DebugContext(ctx, "web: WebListCharacters")

	token, err := playerTokenFromHeader(req.Header())
	if err != nil {
		return nil, err
	}

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.ListCharacters(rpcCtx, &corev1.ListCharactersRequest{
		PlayerSessionToken: token,
	})
	if err != nil {
		slog.Error("web: list characters RPC failed", "error", err)
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("session expired or invalid"))
	}

	return connect.NewResponse(&webv1.WebListCharactersResponse{
		Characters: translateCharacterSummaries(coreResp.GetCharacters()),
	}), nil
}
```

**WebLogout** (line 176-191) — read token from header:

```go
func (h *Handler) WebLogout(ctx context.Context, req *connect.Request[webv1.WebLogoutRequest]) (*connect.Response[webv1.WebLogoutResponse], error) {
	slog.DebugContext(ctx, "web: WebLogout")

	token, _ := playerTokenFromHeader(req.Header())

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	if token != "" {
		if _, err := h.client.Logout(rpcCtx, &corev1.LogoutRequest{
			PlayerSessionToken: token,
		}); err != nil {
			slog.Error("web: logout RPC failed", "error", err)
		}
	}

	resp := connect.NewResponse(&webv1.WebLogoutResponse{})
	resp.Header().Set(headerClearSession, "true")
	return resp, nil
}
```

Note: `WebLogout` ignores the `playerTokenFromHeader` error — logout should clear the cookie even if no valid session exists.

- [ ] **Step 9: Run all web handler tests**

Run:

```bash
task test -- ./internal/web/
```

Expected: All tests PASS.

- [ ] **Step 10: Run full Go build and test suite**

Run:

```bash
task build && task test
```

Expected: Both PASS. The Go side is now complete.

- [ ] **Step 11: Commit**

```bash
jj --no-pager describe -m "feat(web): read player token from cookie header, add WebCheckSession handler"
jj --no-pager new
```

---

## Task 4: SvelteKit Client — Auth Store and Page Updates

**Files:**

- Modify: `web/src/lib/stores/authStore.ts`
- Modify: `web/src/routes/(authed)/+layout.ts`
- Modify: `web/src/routes/login/+page.svelte`
- Modify: `web/src/routes/register/+page.svelte`
- Modify: `web/src/routes/(authed)/characters/+page.svelte`
- Modify: `web/src/lib/components/TopBar.svelte`

- [ ] **Step 1: Update authStore.ts**

Replace the contents of `web/src/lib/stores/authStore.ts`:

```typescript
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable, derived } from 'svelte/store';
import { trace } from '@opentelemetry/api';

const tracer = trace.getTracer('holomush-web');

interface AuthState {
  isPlayerAuthenticated: boolean;
  sessionId: string | null;
  characterName: string | null;
  playerName: string | null;
  isGuest: boolean;
}

const initial: AuthState = {
  isPlayerAuthenticated: false,
  sessionId: null,
  characterName: null,
  playerName: null,
  isGuest: false,
};

export const authState = writable<AuthState>(initial);
export const isAuthenticated = derived(authState, ($s) => $s.isPlayerAuthenticated || !!$s.sessionId);
export const hasCharacter = derived(authState, ($s) => !!$s.sessionId && !!$s.characterName);

export function setPlayerAuth(playerName: string) {
  authState.update((s) => ({ ...s, isPlayerAuthenticated: true, playerName, isGuest: false }));
}

export function setCharacterSession(sessionId: string, characterName: string) {
  authState.update((s) => ({ ...s, sessionId, characterName }));
  sessionStorage.setItem('holomush-session', JSON.stringify({ sessionId, characterName }));
}

export function setGuestSession(sessionId: string, characterName: string) {
  authState.update((s) => ({
    ...s,
    sessionId,
    characterName,
    isGuest: true,
    playerName: characterName,
  }));
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

Key changes:

- `playerSessionToken` removed from `AuthState`
- `isPlayerAuthenticated: boolean` added
- `setPlayerAuth` takes only `playerName` (no token)
- `restoreSession` no longer reads `holomush-player` from sessionStorage
- `clearAuth` no longer removes `holomush-player`

- [ ] **Step 2: Update the auth layout guard**

Replace `web/src/routes/(authed)/+layout.ts`:

```typescript
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { redirect } from '@sveltejs/kit';
import { createClient } from '@connectrpc/connect';
import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';
import { setPlayerAuth, restoreSession } from '$lib/stores/authStore';

export const ssr = false;

export async function load() {
  if (typeof window === 'undefined') return;

  // Restore game session (sessionId/characterName) from sessionStorage.
  restoreSession();

  // Validate player auth via server — cookie provides the credential.
  const client = createClient(WebService, transport);
  try {
    const resp = await client.webCheckSession({});
    setPlayerAuth(resp.playerName);
  } catch {
    redirect(302, '/login');
  }
}
```

- [ ] **Step 3: Update login page**

In `web/src/routes/login/+page.svelte`, update `handleLogin`:

```typescript
  async function handleLogin() {
    if (!username || !password) {
      error = 'Username and password are required.';
      return;
    }
    error = '';
    loading = true;
    try {
      const resp = await client.webAuthenticatePlayer({ username, password });
      if (resp.success) {
        setPlayerAuth(username);
        const autoCharId = resp.defaultCharacterId || (resp.characters.length === 1 ? resp.characters[0].characterId : '');
        if (autoCharId) {
          const selectResp = await client.webSelectCharacter({
            characterId: autoCharId,
          });
          if (selectResp.success) {
            setCharacterSession(selectResp.sessionId, selectResp.characterName);
            goto('/terminal');
            return;
          }
        }
        goto('/characters');
      } else {
        error = resp.errorMessage || 'Login failed.';
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Login failed.';
    } finally {
      loading = false;
    }
  }
```

Changes:

- `setPlayerAuth(resp.playerSessionToken, username)` → `setPlayerAuth(username)`
- `webSelectCharacter` call: remove `playerSessionToken` from request object

- [ ] **Step 4: Update register page**

In `web/src/routes/register/+page.svelte`, update `handleRegister`:

```typescript
      const resp = await client.webCreatePlayer({ username, password, email });
      if (resp.success) {
        setPlayerAuth(username);
        goto('/characters');
      } else {
```

Change: `setPlayerAuth(resp.playerSessionToken, username)` → `setPlayerAuth(username)`

- [ ] **Step 5: Update characters page**

In `web/src/routes/(authed)/characters/+page.svelte`:

Remove the `onMount` auth guard — the layout now handles auth validation. Update all RPC calls to remove `playerSessionToken` from request bodies:

```typescript
  onMount(async () => {
    try {
      const resp = await client.webListCharacters({});
      characters = [...resp.characters];
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load characters.';
    } finally {
      loading = false;
    }
  });

  async function selectCharacter(charId: string) {
    try {
      const resp = await client.webSelectCharacter({
        characterId: charId,
      });
      if (resp.success) {
        setCharacterSession(resp.sessionId, resp.characterName);
        goto('/terminal');
      } else {
        error = resp.errorMessage || 'Failed to select character.';
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to select character.';
    }
  }

  async function createCharacter() {
    if (!newCharName.trim()) {
      createError = 'Character name is required.';
      return;
    }
    createError = '';
    try {
      const resp = await client.webCreateCharacter({
        characterName: newCharName.trim(),
      });
      if (resp.success) {
        if (autoDefault) {
          const selectResp = await client.webSelectCharacter({
            characterId: resp.characterId,
          });
          if (selectResp.success) {
            setCharacterSession(selectResp.sessionId, selectResp.characterName);
            goto('/terminal');
          } else {
            createError = selectResp.errorMessage || 'Failed to enter game.';
          }
        } else {
          const listResp = await client.webListCharacters({});
          characters = [...listResp.characters];
          creating = false;
          newCharName = '';
        }
      } else {
        createError = resp.errorMessage || 'Failed to create character.';
      }
    } catch (e) {
      createError = e instanceof Error ? e.message : 'Failed to create character.';
    }
  }
```

Key changes:

- Remove `if (!$authState.playerSessionToken)` guards (layout handles this)
- Remove `playerSessionToken` from all RPC request objects
- Remove `$authState` reactivity checks that referenced the token

- [ ] **Step 6: Update TopBar**

In `web/src/lib/components/TopBar.svelte`:

Update the logout call and conditional rendering:

```typescript
  async function handleLogout() {
    try {
      await client.webLogout({});
    } catch {
      /* best effort */
    }
    clearAuth();
    goto('/');
  }
```

Update the template conditionals:

```svelte
    {#if !$authState.isPlayerAuthenticated && !$authState.sessionId}
      <a href="/login" class="nav-link">Login</a>
      <a href="/register" class="nav-link accent">Register</a>
    {:else if $authState.sessionId && $authState.characterName}
      <span class="char-name">{$authState.characterName}</span>
      <!-- ... existing buttons ... -->
    {:else if $authState.isPlayerAuthenticated}
      <span class="player-name">{$authState.playerName}</span>
      <!-- ... existing logout button ... -->
    {/if}
```

- [ ] **Step 7: Verify TypeScript compiles**

Run:

```bash
cd web && npx svelte-check 2>&1 | tail -20
```

Expected: No errors.

- [ ] **Step 8: Commit**

```bash
jj --no-pager describe -m "feat(web-client): cookie-only auth, remove playerSessionToken from client state"
jj --no-pager new
```

---

## Task 5: E2E Test Updates

**Files:**

- Modify: `web/e2e/auth.spec.ts`
- Modify: `web/e2e/character-switcher.spec.ts` (if it sends token in requests)

- [ ] **Step 1: Add page-reload auth persistence test**

Add to `web/e2e/auth.spec.ts`:

```typescript
  test('authenticated session persists across page reload', async ({ page }) => {
    // Register a new user
    await page.goto('/register');
    const username = `reload-${Date.now()}`;
    await page.fill('input[name="username"]', username);
    await page.fill('input[name="email"]', `${username}@example.com`);
    await page.fill('input[name="password"]', 'password123');
    await page.fill('input[name="confirmPassword"]', 'password123');
    await page.locator('button[type="submit"]').click();
    await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });

    // Reload the page — should stay on /characters (cookie validates via WebCheckSession)
    await page.reload();
    await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });
  });
```

- [ ] **Step 2: Verify existing E2E tests still work**

Run:

```bash
task test:e2e
```

Expected: All E2E tests PASS. The cookie-based auth is transparent to Playwright because the browser handles `Set-Cookie` automatically.

- [ ] **Step 3: Commit**

```bash
jj --no-pager describe -m "test(e2e): add page-reload auth persistence test"
jj --no-pager new
```

---

## Task 6: Final Verification and Cleanup

- [ ] **Step 1: Run full CI check**

Run:

```bash
task test && task lint && task test:e2e
```

Expected: All pass.

- [ ] **Step 2: Verify token is not in response bodies**

Manually confirm by searching generated proto code:

```bash
rg 'playerSessionToken' web/src/lib/connect/holomush/web/v1/web_pb.ts
```

Expected: No matches in web proto types for request/response messages that were changed. (The field will still exist in `core_pb.ts` — that's expected and correct.)

- [ ] **Step 3: Squash commits for PR**

```bash
jj log --no-graph -r 'ancestors(@, 6) & !main' --template 'change_id.short() ++ " " ++ description.first_line() ++ "\n"'
```

Review the commits, then squash into a single commit for the PR:

```bash
JJ_EDITOR=true jj --no-pager squash --from 'ancestors(@-, 5) & !main' --into @- -m "feat(auth): cookie-only web auth, remove token from web proto responses

Remove player_session_token from all web-facing proto messages. Web RPCs
now authenticate exclusively via the HttpOnly cookie. Add WebCheckSession
RPC for page-reload auth validation. SvelteKit client uses explicit
isPlayerAuthenticated boolean instead of storing the raw token.

Closes holomush-qhkw

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 4: Close the beads issue**

```bash
bd close holomush-qhkw --reason "Implemented: cookie-only web auth with WebCheckSession RPC"
```

---

## Post-Implementation Checklist

- [ ] All proto fields removed from web.proto (6 fields)
- [ ] `WebCheckSession` RPC added to web.proto and core.proto
- [ ] Core handler for `CheckPlayerSession` implemented and tested
- [ ] Web handlers read token from `X-Session-Token` header, not message body
- [ ] `playerTokenFromHeader` helper validates presence at gateway boundary
- [ ] `authStore.ts` uses `isPlayerAuthenticated` boolean, no `playerSessionToken`
- [ ] `holomush-player` sessionStorage key removed
- [ ] Auth layout guard calls `WebCheckSession` on page load
- [ ] All pages/components stop sending `playerSessionToken` in RPC requests
- [ ] Unit tests pass (`task test`)
- [ ] Lint passes (`task lint`)
- [ ] E2E tests pass (`task test:e2e`)
- [ ] Page-reload test confirms cookie-based auth persistence
