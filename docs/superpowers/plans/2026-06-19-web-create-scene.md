<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Web Create-Scene Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a web create-scene affordance backed by a typed `WebCreateScene` RPC, restoring the create capability the E9.5 portal dropped (`holomush-5rh.22`).

**Architecture:** A full-stack vertical slice mirroring the existing `WatchScene` path — proto (`SceneAccessService.CreateScene` + `WebService.WebCreateScene`) → facade (`internal/grpc/sceneaccess_service.go`, `resolveAndGate`→`ownedCharacter`→`beginDispatch`→`sceneClient.CreateScene`) → BFF (`internal/web/scene_handlers.go`) → web client → Svelte UI (left-rail toolbar + empty-state CTA → slide-over Sheet with a title + optional-description form). Structural scene writes use typed RPCs, not the command path (supersedes E9.5 D4 for structural writes); the command path stays for human/CLI conversational verbs.

**Tech Stack:** Go (gRPC/Connect, buf, mockery, testify), protobuf, SvelteKit + Svelte 5 runes, shadcn-svelte (`Sheet`/`input`/`label`), pnpm + vitest, Playwright.

**Spec:** `docs/superpowers/specs/2026-06-19-web-create-scene-design.md`

---

## File structure

| File | Responsibility | Task |
| --- | --- | --- |
| `api/proto/holomush/sceneaccess/v1/sceneaccess.proto` | `CreateScene` facade RPC + messages | 1 |
| `api/proto/holomush/web/v1/web.proto` | `WebCreateScene` BFF RPC + messages | 1 |
| `internal/grpc/sceneaccess_service.go` | `SceneAccessServer.CreateScene` facade method | 2 |
| `internal/grpc/sceneaccess_service_test.go` | facade unit tests | 2 |
| `internal/web/handler.go` | add `CreateScene` to `SceneAccessClient` interface | 3 |
| `internal/grpc/client.go` | `Client.CreateScene` gRPC passthrough | 3 |
| `internal/web/scene_handlers.go` | `Handler.WebCreateScene` BFF handler | 3 |
| `internal/web/scene_handlers_test.go` | mock method + BFF handler tests | 3 |
| `web/src/lib/scenes/client.ts` | `createScene()` RPC wrapper | 4 |
| `web/src/lib/scenes/createFlow.ts` | `submitCreateScene()` orchestration | 4 |
| `web/src/lib/scenes/createFlow.test.ts` | orchestration unit test | 4 |
| `web/src/lib/components/scenes/CreateSceneForm.svelte` | the create form (portal-free, testable) | 5 |
| `web/src/lib/components/scenes/CreateSceneForm.svelte.test.ts` | form render-state + submit test | 5 |
| `web/src/lib/components/scenes/CreateSceneSheet.svelte` | `Sheet` wrapper around the form | 6 |
| `web/src/routes/(authed)/scenes/+page.svelte` | toolbar + empty-state CTA + mount Sheet | 6 |
| `web/e2e/scenes.spec.ts` | no-telnet create E2E | 7 |

---

## Task 1: Proto — `CreateScene` + `WebCreateScene` RPCs

**Files:**

- Modify: `api/proto/holomush/sceneaccess/v1/sceneaccess.proto` (rpc after `:53`, messages after `:232`)
- Modify: `api/proto/holomush/web/v1/web.proto` (rpc after `:273`, messages after `:1020`)

- [ ] **Step 1: Add the facade RPC to `sceneaccess.proto`**

Insert after the `WatchScene` rpc (currently line 53, inside `service SceneAccessService`):

```proto
  // CreateScene creates a new scene owned by the verified player's owned
  // character and returns its full metadata. The facade resolves the acting
  // character from the player session (INV-SCENE-63) and rejects guests
  // (INV-SCENE-64), then forwards a CreateScene call to the plugin SceneService
  // with the server-verified character_id. Unlike WatchScene it requires no
  // existing game session — creation does not touch focus.
  rpc CreateScene(CreateSceneRequest) returns (CreateSceneResponse);
```

- [ ] **Step 2: Add the facade messages to `sceneaccess.proto`**

Insert after `WatchSceneResponse` (currently ends line 232):

```proto
// CreateSceneRequest is the facade request to create a scene. player_session_token
// authenticates the calling player (session_id is a reserved client hint, not
// consulted); the facade resolves the acting character SERVER-SIDE from the
// player's owned-character set (INV-SCENE-63) and overrides any client identity.
message CreateSceneRequest {
  // session_id is the client-declared player-session ULID, retained as a
  // forward-looking hint; authentication is performed SOLELY against
  // player_session_token (holomush-5rh.8.23 decision).
  string session_id = 1;
  // player_session_token is the raw bearer token; the facade looks up the
  // session by token hash and rejects unauthenticated callers
  // (codes.Unauthenticated) and guests (codes.PermissionDenied, INV-SCENE-64).
  string player_session_token = 2;
  // character_id selects which owned alt becomes the scene owner; the facade
  // verifies ownership before passing the server-verified ID downstream
  // (codes.NotFound when not owned, INV-SCENE-63).
  string character_id = 3;
  // title is the scene title; required. The plugin handler rejects empty or
  // whitespace-only titles after trim.
  string title = 4;
  // description is an optional scene synopsis; empty omits it.
  string description = 5;
}

// CreateSceneResponse wraps the created scene's full metadata projection.
message CreateSceneResponse {
  // scene is the newly created scene; the verified character is its owner and
  // first participant.
  holomush.scene.v1.SceneInfo scene = 1;
}
```

- [ ] **Step 3: Add the BFF RPC to `web.proto`**

Insert after the `WebWatchScene` rpc (currently line 273, inside `service WebService`):

```proto
  // WebCreateScene creates a new scene owned by the verified player's owned
  // character and returns its metadata. Proxies to
  // SceneAccessService.CreateScene; player_session_token is read from the HTTP
  // cookie by gateway middleware.
  rpc WebCreateScene(WebCreateSceneRequest) returns (WebCreateSceneResponse);
```

- [ ] **Step 4: Add the BFF messages to `web.proto`**

Insert after `WebWatchSceneResponse` (currently ends line 1020):

```proto
// WebCreateSceneRequest proxies to SceneAccessService.CreateScene.
// player_session_token is injected from the X-Session-Token cookie.
message WebCreateSceneRequest {
  // session_id is the client-declared player-session ULID forwarded to the facade.
  string session_id = 1;
  // character_id selects which owned alt becomes the scene owner, forwarded to
  // the facade for server-side ownership verification.
  string character_id = 2;
  // title is the scene title; required.
  string title = 3;
  // description is an optional scene synopsis; empty omits it.
  string description = 4;
}

// WebCreateSceneResponse re-exports the created scene's metadata from the facade.
message WebCreateSceneResponse {
  // scene is the newly created scene's full metadata projection.
  holomush.scene.v1.SceneInfo scene = 1;
}
```

- [ ] **Step 5: Regenerate Go + TS bindings**

Run: `task proto && task web:generate`
Expected: regenerates `pkg/proto/.../sceneaccess` + `.../web` Go, the internal Connect handlers, and `web/src/lib/connect/holomush/web/v1/web_pb.ts` (adds `webCreateScene` client method + `WebCreateSceneRequest`/`Response` types). No errors.

- [ ] **Step 6: Verify proto lint (doc comments)**

Run: `task lint:proto`
Expected: PASS (every new element has a grounded, non-name-echo comment).

- [ ] **Step 7: Commit**

`jj commit -m "feat(proto): WebCreateScene + SceneAccessService.CreateScene RPCs (holomush-5rh.22)"`

---

## Task 2: Facade — `SceneAccessServer.CreateScene`

**Files:**

- Modify: `internal/grpc/sceneaccess_service.go` (add method after `WatchScene`, ~`:269`)
- Test: `internal/grpc/sceneaccess_service_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/grpc/sceneaccess_service_test.go` (uses the existing helpers `buildSATestPS`, `buildSASessionRepo`, `newTestSceneAccessServer`, `testSAToken`, `scenemocks`, `authmocks`, `sessionmocks`, `stubPluginManager`, `stubFocusCoordinator`):

```go
func TestSceneAccessCreateScene(t *testing.T) {
	ctx := context.Background()
	playerID := idgen.New()
	char := &world.Character{ID: idgen.New(), PlayerID: playerID, Name: "Alice"}
	ps := buildSATestPS(t, playerID)

	t.Run("owned character creates scene with verified id, title, description", func(t *testing.T) {
		playerRepo := authmocks.NewMockPlayerRepository(t)
		playerRepo.EXPECT().GetByID(mock.Anything, playerID).Return(&auth.Player{ID: playerID, IsGuest: false}, nil).Maybe()
		charRepo := authmocks.NewMockCharacterRepository(t)
		charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return([]*world.Character{char}, nil).Maybe()
		sceneMock := scenemocks.NewMockSceneServiceClient(t)
		want := &scenev1.SceneInfo{Id: idgen.New().String(), Title: "The Manor"}
		sceneMock.EXPECT().CreateScene(mock.Anything, mock.MatchedBy(func(req *scenev1.CreateSceneRequest) bool {
			return req.GetCharacterId() == char.ID.String() &&
				req.GetTitle() == "The Manor" && req.GetDescription() == "dusk"
		})).Return(&scenev1.CreateSceneResponse{Scene: want}, nil).Once()

		srv := newTestSceneAccessServer(t, buildSASessionRepo(t, ps), playerRepo, charRepo,
			sessionmocks.NewMockStore(t), &stubFocusCoordinator{}, sceneMock, &stubPluginManager{})

		resp, err := srv.CreateScene(ctx, &sceneaccessv1.CreateSceneRequest{
			PlayerSessionToken: testSAToken, CharacterId: char.ID.String(),
			Title: "The Manor", Description: "dusk",
		})
		require.NoError(t, err)
		assert.Equal(t, want.GetId(), resp.GetScene().GetId())
	})

	// Verifies: INV-SCENE-64
	t.Run("guest is denied and downstream is never called", func(t *testing.T) {
		playerRepo := authmocks.NewMockPlayerRepository(t)
		playerRepo.EXPECT().GetByID(mock.Anything, playerID).Return(&auth.Player{ID: playerID, IsGuest: true}, nil).Maybe()
		sceneMock := scenemocks.NewMockSceneServiceClient(t)
		srv := newTestSceneAccessServer(t, buildSASessionRepo(t, ps), playerRepo,
			authmocks.NewMockCharacterRepository(t), sessionmocks.NewMockStore(t),
			&stubFocusCoordinator{}, sceneMock, &stubPluginManager{})

		_, err := srv.CreateScene(ctx, &sceneaccessv1.CreateSceneRequest{
			PlayerSessionToken: testSAToken, CharacterId: char.ID.String(), Title: "X",
		})
		st, _ := status.FromError(err)
		assert.Equal(t, codes.PermissionDenied, st.Code())
		sceneMock.AssertNotCalled(t, "CreateScene")
	})

	t.Run("character not owned returns NotFound", func(t *testing.T) {
		playerRepo := authmocks.NewMockPlayerRepository(t)
		playerRepo.EXPECT().GetByID(mock.Anything, playerID).Return(&auth.Player{ID: playerID, IsGuest: false}, nil).Maybe()
		charRepo := authmocks.NewMockCharacterRepository(t)
		charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return([]*world.Character{char}, nil).Maybe()
		sceneMock := scenemocks.NewMockSceneServiceClient(t)
		srv := newTestSceneAccessServer(t, buildSASessionRepo(t, ps), playerRepo, charRepo,
			sessionmocks.NewMockStore(t), &stubFocusCoordinator{}, sceneMock, &stubPluginManager{})

		_, err := srv.CreateScene(ctx, &sceneaccessv1.CreateSceneRequest{
			PlayerSessionToken: testSAToken, CharacterId: idgen.New().String(), Title: "X",
		})
		st, _ := status.FromError(err)
		assert.Equal(t, codes.NotFound, st.Code())
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test -- -run TestSceneAccessCreateScene ./internal/grpc/`
Expected: FAIL — `srv.CreateScene` undefined.

- [ ] **Step 3: Implement `CreateScene`**

Add to `internal/grpc/sceneaccess_service.go` after `WatchScene` (after `:269`):

```go
// CreateScene creates a new scene owned by the verified player's owned character
// and returns its metadata. Unlike WatchScene it requires no existing game
// session — creation does not touch focus or sessions. resolveAndGate enforces
// the guest gate (INV-SCENE-64); ownedCharacter enforces ownership (INV-SCENE-63).
func (s *SceneAccessServer) CreateScene(ctx context.Context, req *sceneaccessv1.CreateSceneRequest) (*sceneaccessv1.CreateSceneResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)
	if err != nil {
		return nil, err
	}
	defer release()

	resp, err := s.sceneClient.CreateScene(dctx, &scenev1.CreateSceneRequest{
		CharacterId: char.ID.String(),
		Title:       req.GetTitle(),
		Description: req.GetDescription(),
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.CreateSceneResponse{Scene: resp.GetScene()}, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `task test -- -run TestSceneAccessCreateScene ./internal/grpc/`
Expected: PASS (all three subtests).

- [ ] **Step 5: Commit**

`jj commit -m "feat(grpc): SceneAccessServer.CreateScene facade (holomush-5rh.22)"`

---

## Task 3: BFF — `Handler.WebCreateScene`

**Files:**

- Modify: `internal/web/handler.go` (add to `SceneAccessClient` interface, `:105-115`)
- Modify: `internal/grpc/client.go` (add `Client.CreateScene`, mirror `WatchScene`)
- Modify: `internal/web/scene_handlers.go` (add `WebCreateScene` after `WebWatchScene`, ~`:146`)
- Test: `internal/web/scene_handlers_test.go` (mock method + two tests)

- [ ] **Step 1: Add `CreateScene` to the `SceneAccessClient` interface**

In `internal/web/handler.go`, inside `type SceneAccessClient interface` (after the `WatchScene` line):

```go
	CreateScene(ctx context.Context, req *sceneaccessv1.CreateSceneRequest) (*sceneaccessv1.CreateSceneResponse, error)
```

- [ ] **Step 2: Add the gRPC passthrough to the real client**

In `internal/grpc/client.go`, mirror the existing `WatchScene` wrapper:

```go
func (c *Client) CreateScene(ctx context.Context, req *sceneaccessv1.CreateSceneRequest) (*sceneaccessv1.CreateSceneResponse, error) {
	resp, err := c.sceneAccessClient.CreateScene(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "CreateScene").Wrap(err)
	}
	return resp, nil
}
```

- [ ] **Step 3: Add the mock method + write the failing tests**

In `internal/web/scene_handlers_test.go`, add a `CreateScene` method to `mockSceneAccessClient` (mirror its `WatchScene` method — record the request, return a configurable response/error), e.g.:

```go
func (m *mockSceneAccessClient) CreateScene(_ context.Context, req *sceneaccessv1.CreateSceneRequest) (*sceneaccessv1.CreateSceneResponse, error) {
	m.createSceneReq = req
	if m.createSceneErr != nil {
		return nil, m.createSceneErr
	}
	return &sceneaccessv1.CreateSceneResponse{Scene: &scenev1.SceneInfo{Id: "scene-123"}}, nil
}
```

Add the `createSceneReq *sceneaccessv1.CreateSceneRequest` and `createSceneErr error` fields to the `mockSceneAccessClient` struct. Then add the tests (mirror `TestWebWatchSceneForwardsTokenAndOpFieldsToFacade` / `...PassesStatusErrorThroughAsIs`):

```go
func TestWebCreateSceneForwardsTokenAndOpFieldsToFacade(t *testing.T) {
	sc := &mockSceneAccessClient{}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	req := connect.NewRequest(&webv1.WebCreateSceneRequest{
		SessionId: "sess-1", CharacterId: "char-1", Title: "The Manor", Description: "dusk",
	})
	req.Header().Set(headerInjectSessionToken, "tok-abc")

	resp, err := h.WebCreateScene(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "scene-123", resp.Msg.GetScene().GetId())
	require.NotNil(t, sc.createSceneReq)
	assert.Equal(t, "tok-abc", sc.createSceneReq.GetPlayerSessionToken())
	assert.Equal(t, "char-1", sc.createSceneReq.GetCharacterId())
	assert.Equal(t, "The Manor", sc.createSceneReq.GetTitle())
	assert.Equal(t, "dusk", sc.createSceneReq.GetDescription())
}

func TestWebCreateScenePassesStatusErrorThroughAsIs(t *testing.T) {
	wantErr := status.Error(codes.PermissionDenied, "guests cannot access scenes")
	sc := &mockSceneAccessClient{createSceneErr: wantErr}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	req := connect.NewRequest(&webv1.WebCreateSceneRequest{SessionId: "s", CharacterId: "c", Title: "X"})
	req.Header().Set(headerInjectSessionToken, "tok")

	_, err := h.WebCreateScene(context.Background(), req)
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}
```

> Match the exact `NewHandler` / `WithSceneAccessClient` construction the sibling `TestWebWatchScene*` tests use in this file; adjust the constructor call if it differs.

- [ ] **Step 4: Run the tests to verify they fail**

Run: `task test -- -run TestWebCreateScene ./internal/web/`
Expected: FAIL — `h.WebCreateScene` undefined.

- [ ] **Step 5: Implement `WebCreateScene`**

Add to `internal/web/scene_handlers.go` after `WebWatchScene` (~`:146`):

```go
// WebCreateScene proxies to SceneAccessService.CreateScene. The gateway reads
// the player_session_token from the X-Session-Token cookie header and forwards
// it with character_id, title, and description. Authorization and identity
// resolution are owned entirely by the facade.
func (h *Handler) WebCreateScene(ctx context.Context, req *connect.Request[webv1.WebCreateSceneRequest]) (*connect.Response[webv1.WebCreateSceneResponse], error) {
	slog.DebugContext(ctx, "web: WebCreateScene", "session_id", req.Msg.GetSessionId(), "character_id", req.Msg.GetCharacterId())

	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.sceneAccess.CreateScene(rpcCtx, &sceneaccessv1.CreateSceneRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
		Title:              req.Msg.GetTitle(),
		Description:        req.Msg.GetDescription(),
	})
	if err != nil {
		slog.ErrorContext(ctx, "web: create scene RPC failed", "session_id", req.Msg.GetSessionId(), "error", err)
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebCreateSceneResponse{Scene: resp.GetScene()}), nil
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `task test -- -run TestWebCreateScene ./internal/web/`
Expected: PASS. The `var _ webv1connect.WebServiceHandler = (*Handler)(nil)` compile-check (`handler.go`) confirms the new RPC is implemented.

- [ ] **Step 7: Commit**

`jj commit -m "feat(web): WebCreateScene BFF handler + client passthrough (holomush-5rh.22)"`

---

## Task 4: Web client `createScene` + `submitCreateScene` flow

**Files:**

- Modify: `web/src/lib/scenes/client.ts` (add `createScene`)
- Create: `web/src/lib/scenes/createFlow.ts`
- Test: `web/src/lib/scenes/createFlow.test.ts`

- [ ] **Step 1: Add the `createScene` RPC wrapper**

In `web/src/lib/scenes/client.ts`, extend the `web_pb` import with `type WebCreateSceneRequest` and add:

```ts
/**
 * Creates a new scene owned by the given character and returns its SceneInfo.
 * The player_session_token rides the X-Session-Token cookie (set by the
 * gateway); only session_id/character_id/title/description are sent. Used by
 * the create-scene form — NOT the command path (structural write = typed RPC).
 */
export async function createScene(
	sessionId: string,
	opts: Pick<WebCreateSceneRequest, 'characterId' | 'title' | 'description'>,
) {
	const res = await client.webCreateScene({ sessionId, ...opts });
	return res.scene;
}
```

- [ ] **Step 2: Write the failing orchestration test**

Create `web/src/lib/scenes/createFlow.test.ts`:

```ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('./altSessions.svelte', () => ({
	ensureSession: vi.fn(async () => 'sess-1'),
}));
vi.mock('./client', () => ({
	createScene: vi.fn(async () => ({ id: 'scene-new' })),
}));
vi.mock('./workspaceStore.svelte', () => ({
	workspaceStore: { refresh: vi.fn(async () => {}), select: vi.fn(async () => {}) },
}));

import { submitCreateScene } from './createFlow';
import { ensureSession } from './altSessions.svelte';
import { createScene } from './client';
import { workspaceStore } from './workspaceStore.svelte';

const chars = [{ characterId: 'char-1', name: 'Alice' }];

describe('submitCreateScene', () => {
	beforeEach(() => vi.clearAllMocks());

	it('ensures the alt session, creates, refreshes, and selects the new scene', async () => {
		const id = await submitCreateScene({
			characterId: 'char-1', title: 'The Manor', description: 'dusk', characters: chars,
		});
		expect(id).toBe('scene-new');
		expect(ensureSession).toHaveBeenCalledWith('char-1');
		expect(createScene).toHaveBeenCalledWith('sess-1', {
			characterId: 'char-1', title: 'The Manor', description: 'dusk',
		});
		expect(workspaceStore.refresh).toHaveBeenCalledWith(chars);
		expect(workspaceStore.select).toHaveBeenCalledWith('scene-new', '', 'char-1');
	});

	it('skips select when no scene id is returned', async () => {
		vi.mocked(createScene).mockResolvedValueOnce(undefined);
		const id = await submitCreateScene({
			characterId: 'char-1', title: 'X', description: '', characters: chars,
		});
		expect(id).toBe('');
		expect(workspaceStore.refresh).toHaveBeenCalledWith(chars);
		expect(workspaceStore.select).not.toHaveBeenCalled();
	});
});
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd web && pnpm test:unit -- createFlow`
Expected: FAIL — `./createFlow` not found.

- [ ] **Step 4: Implement the orchestration**

Create `web/src/lib/scenes/createFlow.ts`:

```ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { ensureSession } from './altSessions.svelte';
import { createScene } from './client';
import { workspaceStore } from './workspaceStore.svelte';

/**
 * Orchestrates web scene creation: ensure the acting alt's session, create the
 * scene via the typed RPC, refresh My Scenes so the new (owner) scene appears,
 * then focus it. Returns the new scene id ('' when the server returned none).
 * The create RPC is authoritative; refresh/select are best-effort UI updates.
 */
export async function submitCreateScene(opts: {
	characterId: string;
	title: string;
	description: string;
	characters: { characterId: string; name?: string; characterName?: string }[];
}): Promise<string> {
	const sessionId = await ensureSession(opts.characterId);
	const scene = await createScene(sessionId, {
		characterId: opts.characterId,
		title: opts.title,
		description: opts.description,
	});
	const sceneId = scene?.id ?? '';
	await workspaceStore.refresh(opts.characters);
	if (sceneId) {
		await workspaceStore.select(sceneId, '', opts.characterId);
	}
	return sceneId;
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd web && pnpm test:unit -- createFlow`
Expected: PASS (both cases).

- [ ] **Step 6: Commit**

`jj commit -m "feat(web): createScene client wrapper + submitCreateScene flow (holomush-5rh.22)"`

---

## Task 5: `CreateSceneForm.svelte`

The portal-free form (so it is unit-testable in jsdom without the `Sheet`'s bits-ui portal). The `Sheet` wrapper is Task 6.

**Files:**

- Create: `web/src/lib/components/scenes/CreateSceneForm.svelte`
- Test: `web/src/lib/components/scenes/CreateSceneForm.svelte.test.ts`

- [ ] **Step 1: Write the failing render-state test**

Create `web/src/lib/components/scenes/CreateSceneForm.svelte.test.ts` (mirrors the `mount`/`unmount` idiom of `web/src/lib/components/terminal/MovementRenderer.svelte.test.ts`):

```ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount, unmount } from 'svelte';
import CreateSceneForm from './CreateSceneForm.svelte';

vi.mock('$lib/scenes/createFlow', () => ({ submitCreateScene: vi.fn(async () => 'scene-new') }));

afterEach(() => document.body.replaceChildren());

function render(characters: { characterId: string; name?: string }[]) {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const onDone = vi.fn();
	const component = mount(CreateSceneForm, { target, props: { characters, onDone } });
	return { target, onDone, component };
}

describe('CreateSceneForm', () => {
	it('disables Create until a title is entered', () => {
		const { target } = render([{ characterId: 'c1', name: 'Alice' }]);
		const create = target.querySelector<HTMLButtonElement>('button[aria-label="Create scene"]')!;
		expect(create.disabled).toBe(true);
	});

	it('hides the character selector with a single alt', () => {
		const { target } = render([{ characterId: 'c1', name: 'Alice' }]);
		expect(target.querySelector('select[aria-label="Create scene as"]')).toBeNull();
	});

	it('shows the character selector with multiple alts', () => {
		const { target } = render([
			{ characterId: 'c1', name: 'Alice' },
			{ characterId: 'c2', name: 'Bob' },
		]);
		expect(target.querySelector('select[aria-label="Create scene as"]')).not.toBeNull();
	});
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && pnpm test:unit -- CreateSceneForm`
Expected: FAIL — component not found.

- [ ] **Step 3: Implement the form**

Create `web/src/lib/components/scenes/CreateSceneForm.svelte`:

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { cn } from '$lib/utils.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { submitCreateScene } from '$lib/scenes/createFlow';

  let {
    characters = [],
    onDone,
  }: {
    characters?: { characterId: string; name?: string; characterName?: string }[];
    onDone?: () => void;
  } = $props();

  let title = $state('');
  let description = $state('');
  let actingCharacterId = $state(characters[0]?.characterId ?? '');
  let submitting = $state(false);
  let errorMsg = $state('');

  function charLabel(c: { characterId: string; name?: string; characterName?: string }): string {
    return c.characterName ?? c.name ?? c.characterId;
  }

  async function create() {
    const t = title.trim();
    if (!t || submitting) return;
    submitting = true;
    errorMsg = '';
    try {
      await submitCreateScene({
        characterId: actingCharacterId,
        title: t,
        description: description.trim(),
        characters,
      });
      onDone?.();
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : 'Create failed';
    } finally {
      submitting = false;
    }
  }
</script>

<form class="flex flex-col gap-3 px-4 py-3" onsubmit={(e) => { e.preventDefault(); create(); }}>
  {#if characters.length > 1}
    <div class="flex flex-col gap-1">
      <Label for="create-scene-as">Create as</Label>
      <select
        id="create-scene-as"
        aria-label="Create scene as"
        bind:value={actingCharacterId}
        class="rounded-md border border-input bg-background px-3 py-2 text-sm"
      >
        {#each characters as c (c.characterId)}
          <option value={c.characterId}>{charLabel(c)}</option>
        {/each}
      </select>
    </div>
  {/if}

  <div class="flex flex-col gap-1">
    <Label for="create-scene-title">Title</Label>
    <Input id="create-scene-title" bind:value={title} placeholder="Scene title…" disabled={submitting} />
  </div>

  <div class="flex flex-col gap-1">
    <Label for="create-scene-desc">Description <span class="text-muted-foreground">(optional)</span></Label>
    <textarea
      id="create-scene-desc"
      bind:value={description}
      disabled={submitting}
      rows={3}
      placeholder="What's this scene about?"
      class={cn(
        'w-full min-h-[72px] resize-y rounded-md border border-input bg-background px-3 py-2',
        'text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50',
        'disabled:opacity-50',
      )}
    ></textarea>
  </div>

  <div class="flex items-center justify-end gap-2">
    <Button type="button" variant="ghost" size="sm" onclick={() => onDone?.()} disabled={submitting}>
      Cancel
    </Button>
    <Button type="submit" variant="default" size="sm" aria-label="Create scene" disabled={submitting || !title.trim()}>
      {submitting ? 'Creating…' : 'Create scene'}
    </Button>
  </div>

  {#if errorMsg}
    <p class="text-xs text-destructive">{errorMsg}</p>
  {/if}
</form>
```

> If `Input` is not yet present under `web/src/lib/components/ui/`, add it with the shadcn-svelte skill (`/shadcn-svelte` → add `input`) — `input` was confirmed present at spec time; verify before implementing.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd web && pnpm test:unit -- CreateSceneForm`
Expected: PASS (all three).

- [ ] **Step 5: Type-check**

Run: `cd web && pnpm check`
Expected: no errors in the new files.

- [ ] **Step 6: Commit**

`jj commit -m "feat(web): CreateSceneForm component (holomush-5rh.22)"`

---

## Task 6: `CreateSceneSheet` wrapper + wire into `/scenes`

**Files:**

- Create: `web/src/lib/components/scenes/CreateSceneSheet.svelte`
- Modify: `web/src/routes/(authed)/scenes/+page.svelte` (toolbar in `<nav>` `:288`; empty state `:406-415`; remove footer link `:347-352`; mount Sheet)

- [ ] **Step 1: Implement the Sheet wrapper**

Create `web/src/lib/components/scenes/CreateSceneSheet.svelte`:

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import {
    Sheet,
    SheetContent,
    SheetHeader,
    SheetTitle,
    SheetDescription,
  } from '$lib/components/ui/sheet/index.js';
  import CreateSceneForm from './CreateSceneForm.svelte';

  let {
    open = $bindable(false),
    characters = [],
  }: {
    open?: boolean;
    characters?: { characterId: string; name?: string; characterName?: string }[];
  } = $props();
</script>

<Sheet bind:open>
  <SheetContent side="right" class="p-0 flex flex-col">
    <SheetHeader class="px-4 pt-4 pb-2">
      <SheetTitle>New scene</SheetTitle>
      <SheetDescription class="sr-only">Create a new scene you own</SheetDescription>
    </SheetHeader>
    <CreateSceneForm {characters} onDone={() => (open = false)} />
  </SheetContent>
</Sheet>
```

- [ ] **Step 2: Add toolbar + state + Sheet to `+page.svelte`**

In the `<script>` block add state:

```ts
  let createSheetOpen = $state(false);
```

Import the Sheet component near the other scene-component imports (after `SceneContextRail` import, ~`:24`):

```ts
  import CreateSceneSheet from '$lib/components/scenes/CreateSceneSheet.svelte';
```

Insert the toolbar as the first child of the desktop `<nav>` (immediately after `<nav ...>` at `:288`, before the `<div class="p-3 pb-1">` My Scenes header):

```svelte
    <div class="flex items-center gap-1.5 p-2 border-b border-border/50">
      <button
        type="button"
        aria-label="New scene"
        onclick={() => (createSheetOpen = true)}
        class="inline-flex items-center gap-1 rounded-md bg-primary px-2.5 py-1 text-xs font-semibold text-primary-foreground hover:opacity-90"
      >
        + New scene
      </button>
      <a href="/scenes/browse" class="rounded-md border border-border px-2.5 py-1 text-xs text-muted-foreground hover:text-foreground hover:border-primary">
        Browse
      </a>
      <a href="/scenes/browse#archive" class="rounded-md border border-border px-2.5 py-1 text-xs text-muted-foreground hover:text-foreground hover:border-primary">
        Archive
      </a>
    </div>
```

Remove the now-redundant footer link block (`:347-352`):

```svelte
    <!-- DELETE this block (replaced by the top toolbar) -->
    <div class="mt-auto border-t border-border/50 p-3">
      <a href="/scenes/browse" class="text-xs text-muted-foreground hover:text-foreground transition-colors">
        + browse · ⌕ archive
      </a>
    </div>
```

Mount the Sheet once near the end of the markup (sibling of the existing mobile `<Sheet>` blocks, after `:282`):

```svelte
  <CreateSceneSheet bind:open={createSheetOpen} {characters} />
```

- [ ] **Step 3: Add the empty-state CTA**

Replace the empty-state block (`:406-415`) with a version that includes the create button:

```svelte
    {:else}
      <!-- Empty state -->
      <div class="flex-1 flex items-center justify-center">
        <div class="text-center space-y-3">
          <p class="text-muted-foreground">Select a scene from the list to begin</p>
          <button
            type="button"
            aria-label="New scene"
            onclick={() => (createSheetOpen = true)}
            class="inline-flex items-center gap-1 rounded-md bg-primary px-3 py-1.5 text-sm font-semibold text-primary-foreground hover:opacity-90"
          >
            + New scene
          </button>
          <p>
            <a href="/scenes/browse" class="text-sm text-primary hover:underline">
              Browse open scenes →
            </a>
          </p>
        </div>
      </div>
    {/if}
```

- [ ] **Step 4: Type-check + build**

Run: `cd web && pnpm check`
Expected: no errors. (`createSheetOpen`, `CreateSceneSheet`, `characters` all resolve.)

- [ ] **Step 5: Manual smoke (optional but recommended)**

Run: `task dev` (separate terminal), open `/scenes` as a registered player, confirm the toolbar **+ New scene** opens the Sheet and a title-only create lands a scene in *My Scenes*.

- [ ] **Step 6: Commit**

`jj commit -m "feat(web): create-scene toolbar + empty-state CTA + Sheet on /scenes (holomush-5rh.22)"`

---

## Task 7: E2E — create a scene from the web with no telnet

**Files:**

- Modify: `web/e2e/scenes.spec.ts` (add a test in the `'Scenes workspace (E9.5)'` describe, `:314`)

- [ ] **Step 1: Write the E2E test**

Add to the `describe('Scenes workspace (E9.5)', …)` block. Reuse this file's existing registration flow (a fresh registered — non-guest — player; mirror the `page.goto('/register')` registration used at `web/e2e/scenes.spec.ts:222`). Do NOT use `createSceneViaTerminal` — the whole point is the GUI path with no terminal command:

```ts
test('registered player creates a scene from the web GUI with no telnet', async ({ page }) => {
  // Register a fresh (non-guest) player via the existing onboarding helper
  // (web/e2e/scenes.spec.ts:217). "No telnet" means the SCENE is created via the
  // GUI below — never a typed `scene create` command; normal registration is fine.
  await registerAndEnterTerminal(page, 'create-web');

  await page.goto('/scenes');

  // Empty workspace → create affordance is present.
  const newSceneBtn = page.getByRole('button', { name: 'New scene' }).first();
  await expect(newSceneBtn).toBeVisible({ timeout: 10000 });
  await newSceneBtn.click();

  const title = `Web Made ${Date.now()}`;
  await page.getByLabel('Title').fill(title);
  await page.getByRole('button', { name: 'Create scene' }).click();

  // The new scene appears in My Scenes and becomes the focused scene.
  await expect(page.getByRole('listbox', { name: 'My scenes' }).getByText(title)).toBeVisible({ timeout: 10000 });
  await expect(page.getByRole('log', { name: 'scene log' })).toBeVisible({ timeout: 10000 });
});
```

> `registerAndEnterTerminal(page, prefix)` (`web/e2e/scenes.spec.ts:217`) is the existing helper the sibling E9.5 tests use (e.g. `registerAndEnterTerminal(page, 'brw')`); it registers a real non-guest player. Reuse it directly with a fresh prefix — do not invent a new helper.

- [ ] **Step 2: Run the E2E test**

Run: `task test:e2e -- scenes.spec.ts`
Expected: the new test PASSES (needs Docker; the task spins up the Playwright + stack compose).

- [ ] **Step 3: Commit**

`jj commit -m "test(e2e): web create-scene with no telnet (holomush-5rh.22)"`

---

## Final verification

- [ ] **Run the full fast lane**

Run: `task pr-prep`
Expected: `✓ All PR checks passed.` (exit 0). Run as the sole command; read the exit code, not the log tail.

- [ ] **Run scenes integration tests** (capability/manifest regression guard)

Run: `task test:int -- ./test/integration/scenes/...`
Expected: PASS. (No `plugins/core-scenes/plugin.yaml` change is expected — `CreateScene` is a method on the already-provided `SceneAccessService` and uses the already-declared `eval` capability — but verify nothing regressed.)

- [ ] **Confirm the D4 disposition is recorded** in the spec (`§8`, resolved-by-evolution) — no R1 code is built.

<!-- adr-capture: sha256=5ae236fcb4d0da90; session=535177b8; ts=2026-06-20T00:18:51Z; adrs=holomush-v4qmu,holomush-x8swp -->
