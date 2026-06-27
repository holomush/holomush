<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Scenes Web — Membership Actions (Invite/Kick/Transfer/Leave) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface the scene membership verbs (Invite, Kick, Transfer-ownership, Leave) in the web client so a participant can manage a scene's roster from the browser with no telnet — built on a new, reusable character-directory picker.

**Architecture:** Two phases under one epic (`holomush-5rh.24`).

- **Phase 1 (precursor substrate) — Character directory.** A net-new "list all characters" read surface (no such directory exists today). Backend `ListAll` repo query (**fetch-all id+name — no pagination**; payload is two columns, tiny) → `CoreService.ListAllCharacters` RPC → `WebListAllCharacters` BFF → a shadcn-svelte multi-select combobox (`CharacterMultiSelect`) that fetches the full list once and filters client-side. Returns **id + name only**; connection/online state is deliberately excluded (a separately-gated permission). **Authorization (ABAC, not authentication-only — design-review fix):** the read is gated by a dedicated ABAC action `list_character_directory` on a new singleton resource `character_directory`, seeded with a **default-permit policy for any authenticated `character` principal (registered OR guest)**. This keeps the codebase invariant that every game-state read RPC is policy-configurable/auditable (ADR `holomush-lp65`, mirrored from `list_presence`) while still admitting guests. It lives on `CoreService` (which guests reach), NOT the guest-denying `SceneAccessService` facade. The ABAC subject is the acting alt (`character_id`, ownership-verified server-side); the permit is unconditional so any valid character passes. The names-enumerable/connection-state-gated split is registered as **INV-ACCESS-9**. A server-side `name_prefix` search is the deferred scale follow-up.
- **Phase 2 (uses) — Membership verbs.** Typed BFF stack per ADR `holomush-v4qmu` (web → `WebService` → `SceneAccessService` facade → `SceneService`), mirroring the shipped lifecycle slice. The four `SceneService` membership handlers gain an ABAC self-gate (advances **INV-SCENE-65**), the `invite` Cedar policy is relaxed to participant-wide, and the roster gets a kebab (Transfer/Kick), a Leave action, and an Invite picker that consumes Phase 1.

**Tech Stack:** Go (gRPC/Connect, `buf`), protobuf, SvelteKit 5 + `@connectrpc/connect`, shadcn-svelte (nova, on bits-ui) + `@lucide/svelte`, vitest, Playwright. Build via `task` (never raw `go`/`buf`/`pnpm dlx` outside the documented step).

**Spec:** `docs/superpowers/specs/2026-06-24-scenes-web-management-actions-design.md` (Membership = slice 3 of 4).

**Overlap / strawman note:** Phase 1's character directory is an **MVP/strawman** id↔name listing. It overlaps the in-flight scene-character-name-resolution work (`holomush-5rh.25` / design `holomush-vdy2z`, plan `docs/superpowers/plans/2026-06-20-scene-character-name-resolution.md`), which may later supersede or extend this directory with richer resolution/scoping. Because the Invite picker selects from an id+name list, **no name→id resolver is needed** — Invite carries the picked character IDs directly. A `bd` note MUST be filed against `holomush-5rh.25` recording that this slice ships a minimal directory it may absorb.

---

## File Structure

### Phase 1 — Character directory

| File | Responsibility | Action |
| --- | --- | --- |
| `internal/auth/character_service.go` | Add `ListAll(ctx)` to the `CharacterRepository` interface (no pagination) | Modify |
| `internal/world/postgres/character_repo.go` | Implement `ListAll` (`SELECT id, name FROM characters ORDER BY name` — fetch-all) | Modify |
| `internal/access/prefix.go` | New `character_directory` resource type (prefix + valid-list + helper) | Modify |
| `internal/access/policy/seed.go` / `seed_test.go` | New `list_character_directory` action + default-permit-authenticated seed | Modify |
| `docs/architecture/invariants.yaml` (+ generated `.md`) | Register + bind `INV-ACCESS-9` | Modify |
| `internal/world/postgres/character_repo_test.go` | `ListAll` repo test (testcontainer) | Modify |
| `internal/auth/mocks/mock_CharacterRepository.go` | Regenerated mock (`authmocks`, via `mockery`) | Generated |
| `api/proto/holomush/core/v1/core.proto` | `ListAllCharacters` RPC + `CharacterDirectoryEntry`/req/resp messages | Modify |
| `api/proto/holomush/web/v1/web.proto` | `WebListAllCharacters` RPC + messages | Modify |
| `internal/grpc/auth_handlers.go` | `CoreServer.ListAllCharacters` handler (verify acting-char ownership → ABAC `list_character_directory` → `ListAll`) | Modify |
| `internal/grpc/auth_handlers_test.go` | Handler tests (valid token incl. guest; missing token rejected) | Modify |
| `internal/grpc/client.go` | `Client.ListAllCharacters` wrapper | Modify |
| `internal/web/handler.go` | `CoreClient` interface — add `ListAllCharacters` | Modify |
| `internal/web/auth_handlers.go` | `Handler.WebListAllCharacters` BFF handler | Modify |
| `internal/web/auth_handlers_test.go` | BFF handler test + `mockCoreClient` method | Modify |
| `cmd/holomush/deps.go` / `deps_test.go` | Add `ListAllCharacters` to `GRPCClient` iface + `mockGRPCClient` if reached (compile gate) | Modify |
| `web/src/lib/components/ui/command/**`, `popover/**` | shadcn-svelte primitives (added via CLI) | Create |
| `web/src/lib/scenes/directoryClient.ts` | `listAllCharacters()` Web RPC wrapper | Create |
| `web/src/lib/components/scenes/CharacterMultiSelect.svelte` | Multi-select, type-ahead character picker | Create |
| `web/src/lib/components/scenes/CharacterMultiSelect.svelte.test.ts` | Component tests | Create |

### Phase 2 — Membership verbs

| File | Responsibility | Action |
| --- | --- | --- |
| `api/proto/holomush/sceneaccess/v1/sceneaccess.proto` | 4 facade RPCs (Invite/Kick/Transfer/Leave) + messages | Modify |
| `api/proto/holomush/web/v1/web.proto` | 4 BFF RPCs (`WebInviteToScene` etc.) + messages | Modify |
| `plugins/core-scenes/service.go` | Self-gate the 4 membership handlers | Modify |
| `plugins/core-scenes/service_test.go` | Deny-path tests; wire `allowEvaluator` into existing membership tests | Modify |
| `plugins/core-scenes/plugin.yaml` | Relax `invite-to-scene` policy owner→participant | Modify |
| `plugins/core-scenes/plugin_test.go` (or policy test) | Invite-policy participant-wide test | Modify |
| `internal/grpc/sceneaccess_service.go` | 4 facade methods (forward; empty responses) | Modify |
| `internal/grpc/sceneaccess_service_test.go` | Facade table tests (guest-deny / not-owned / forward) | Modify |
| `internal/grpc/client.go` | 4 concrete `Client` wrappers | Modify |
| `cmd/holomush/deps.go` / `deps_test.go` | 4 methods on `GRPCClient` iface + `mockGRPCClient` (compile gate) | Modify |
| `internal/web/handler.go` | 4 methods on `SceneAccessClient` iface | Modify |
| `internal/web/scene_handlers.go` | 4 BFF handlers | Modify |
| `internal/web/scene_handlers_test.go` | `mockSceneAccessClient` methods + handler tests | Modify |
| `web/src/lib/scenes/client.ts` | 4 client wrappers (invite/kick/transfer/leave) | Modify |
| `web/src/lib/scenes/membershipFlow.ts` | Action orchestration (mutate → refetch via `workspaceStore.select`) | Create |
| `web/src/lib/scenes/membershipFlow.test.ts` | Flow unit tests | Create |
| `web/src/lib/components/scenes/SceneContextRail.svelte` | Roster kebab (Transfer/Kick), Leave, Invite picker | Modify |
| `web/src/lib/components/scenes/SceneContextRail.svelte.test.ts` | Roster-action visibility/wiring tests | Modify |
| `web/e2e/scenes-membership.spec.ts` | Telnet-free invite+kick E2E (prefix `wmb`) | Create |

Generated stubs regenerated by `task proto && task web:generate` (commit them): `pkg/proto/holomush/{core,sceneaccess,web}/v1/*.pb.go`, `web/src/lib/connect/holomush/{core,web}/v1/*_pb.ts`.

**Ordering invariants:**

- **Phase 1 lands before Phase 2's Invite UI** — the Invite picker imports `CharacterMultiSelect` + `listAllCharacters`. Kick/Transfer/Leave do not depend on Phase 1 and could ship first, but the plan keeps phases sequential for a coherent epic.
- **Spec §5.4 gate-with-exposure:** the self-gate (Task 8) MUST land no later than the facade/BFF tasks — never expose a `Web*` membership RPC that reaches an ungated handler. Task 8 precedes the facade/client/BFF tasks (10–12).
- **INV-SCENE-65 stays `binding: pending`.** This slice self-gates 4 of the 5 remaining handlers (Invite/Kick/Transfer/Leave); `UpdateScene` (the 8th, Settings slice) is still ungated, so the registry MUST NOT flip to `bound` here. Flipping is owned by `holomush-5rh.24.9` once all 8 gate. Do **not** add a `// Verifies: INV-SCENE-65` annotation that would imply a complete binding.

---

## Phase 1: Character directory (precursor substrate)

### Task 1: Store — `CharacterRepository.ListAll`

**Files:**

- Modify: `internal/auth/character_service.go:17` (interface), `internal/world/postgres/character_repo.go`
- Test: `internal/world/postgres/character_repo_test.go`
- Generated: `internal/auth/mocks/mock_CharacterRepository.go` (the `authmocks` package, via `mockery`)

The `CoreServer` holds `charRepo auth.CharacterRepository` (`internal/grpc/auth_handlers.go:123-125`); its `ListCharacters` handler (`:540`) calls `s.charRepo.ListByPlayer`. We add a sibling `ListAll` that returns **every** character's id + name — **fetch-all, no pagination** (the picker fetches once and filters client-side; id+name for the whole game is a tiny payload). A server-side `name_prefix` search is the deferred scale follow-up.

- [ ] **Step 1: Write the failing repo test**

Add to `internal/world/postgres/character_repo_test.go`, mirroring an existing repo test that seeds characters (e.g. the `GetByLocation` test). It seeds ≥2 characters owned by different players and asserts `ListAll` returns both, ordered by name:

```go
func TestCharacterRepositoryListAllReturnsEveryCharacterNameAndID(t *testing.T) {
	repo, cleanup := newCharacterRepoTestDB(t) // existing testcontainer helper
	defer cleanup()
	ctx := context.Background()

	p1, p2 := idgen.New(), idgen.New()
	a := &world.Character{ID: idgen.New(), PlayerID: p1, Name: "Alice"}
	b := &world.Character{ID: idgen.New(), PlayerID: p2, Name: "Bob"}
	require.NoError(t, repo.Create(ctx, a))
	require.NoError(t, repo.Create(ctx, b))

	got, err := repo.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2) // fresh test DB: exactly the two seeded characters
	assert.Equal(t, "Alice", got[0].Name) // name-ascending order
	assert.Equal(t, "Bob", got[1].Name)
	assert.Equal(t, a.ID, got[0].ID) // IDs preserved, not dropped/duplicated
	assert.Equal(t, b.ID, got[1].ID)
}
```

(Use the file's existing test-DB bootstrap helper — match its name; do not invent `newCharacterRepoTestDB` if the file names it differently.)

- [ ] **Step 2: Run to verify it fails**

Run: `task test:int -- -run TestCharacterRepositoryListAll ./internal/world/postgres/`
Expected: FAIL — `repo.ListAll` undefined.

- [ ] **Step 3: Add `ListAll` to the interface**

In `internal/auth/character_service.go`, inside `type CharacterRepository interface` (after `ListByPlayer`, `:28`):

```go
	// ListAll returns ALL characters (id + name only) for the directory picker —
	// fetch-all, NO pagination. Names only; no connection state. world is imported
	// as it already is for ListByPlayer's return type.
	ListAll(ctx context.Context) ([]*world.Character, error)
```

- [ ] **Step 4: Implement in postgres**

In `internal/world/postgres/character_repo.go`, mirror `GetByLocation` (`:89`) but with no location filter and no pagination:

```go
// ListAll returns every character ordered by name (id + name only — directory
// surface; other columns are left zero). Fetch-all: no LIMIT/OFFSET.
func (r *CharacterRepository) ListAll(ctx context.Context) ([]*world.Character, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name FROM characters ORDER BY name ASC`)
	if err != nil {
		return nil, oops.Code("CHARACTER_LIST_ALL_FAILED").Wrap(err)
	}
	defer rows.Close()

	var out []*world.Character
	for rows.Next() {
		var id ulid.ULID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, oops.Code("CHARACTER_LIST_ALL_SCAN_FAILED").Wrap(err)
		}
		out = append(out, &world.Character{ID: id, Name: name})
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("CHARACTER_LIST_ALL_FAILED").Wrap(err)
	}
	return out, nil
}
```

- [ ] **Step 5: Regenerate the mock + verify**

Run: `mockery && task test:int -- -run TestCharacterRepositoryListAll ./internal/world/postgres/`
Expected: PASS. (`mockery` regenerates `internal/auth/mocks/mock_CharacterRepository.go` — the `authmocks` package — with `ListAll`.)

- [ ] **Step 6: Commit**

`jj commit -m "feat(world): CharacterRepository.ListAll directory query (holomush-5rh.24)"`

---

### Task 2: Proto — `CoreService.ListAllCharacters` + `WebListAllCharacters`

**Files:**

- Modify: `api/proto/holomush/core/v1/core.proto`, `api/proto/holomush/web/v1/web.proto`
- Generated: `pkg/proto/holomush/{core,web}/v1/*.pb.go`, `web/src/lib/connect/holomush/{core,web}/v1/*_pb.ts`

- [ ] **Step 1: Add the CoreService RPC + messages**

In `core.proto`, after `ListCharacters` (`:99`):

```protobuf
  // ListAllCharacters returns the id+name of every character in the game for
  // the directory picker (fetch-all, no pagination). The handler verifies the
  // acting character is owned by the session, then ABAC-gates on action
  // list_character_directory (resource character_directory), seeded default-permit
  // for any authenticated character (registered OR guest). Connection/online
  // state is NOT included; that is a separately-permissioned attribute.
  rpc ListAllCharacters(ListAllCharactersRequest) returns (ListAllCharactersResponse);
```

At the end of `core.proto`:

```protobuf
// ListAllCharactersRequest authenticates the caller and names the acting alt
// (the ABAC subject). No pagination — the directory is returned in full.
message ListAllCharactersRequest {
  // player_session_token authenticates the caller; any valid session (guest or
  // registered) is accepted. Required.
  string player_session_token = 1;
  // character_id is the acting alt; the handler verifies the session owns it and
  // uses it as the ABAC subject for the list_character_directory action. Required.
  string character_id = 2;
}

// CharacterDirectoryEntry is one directory row: identity + display name only.
message CharacterDirectoryEntry {
  // character_id is the character's ULID.
  string character_id = 1;
  // name is the character's display name.
  string name = 2;
}

// ListAllCharactersResponse carries the directory page.
message ListAllCharactersResponse {
  // characters is the id+name list, name-ascending. No connection state.
  repeated CharacterDirectoryEntry characters = 1;
}
```

- [ ] **Step 2: Add the WebService RPC + messages**

In `web.proto`, after `WebListCharacters` (`:179`):

```protobuf
  // WebListAllCharacters proxies to CoreService.ListAllCharacters. The gateway
  // reads player_session_token from the X-Session-Token cookie; any
  // authenticated caller (guest included) may list character names.
  rpc WebListAllCharacters(WebListAllCharactersRequest) returns (WebListAllCharactersResponse);
```

At the end of `web.proto`:

```protobuf
// WebListAllCharactersRequest proxies to CoreService.ListAllCharacters; token
// is injected from the X-Session-Token cookie.
message WebListAllCharactersRequest {
  // character_id is the acting alt (forwarded as the ABAC subject). Required.
  string character_id = 1;
}

// WebListAllCharactersResponse re-exports the directory page from core.
message WebListAllCharactersResponse {
  // characters is the id+name list from CoreService.ListAllCharacters.
  repeated holomush.core.v1.CharacterDirectoryEntry characters = 1;
}
```

Confirm `web.proto` already imports `holomush/core/v1/core.proto` (it references core types elsewhere); if not, add the import.

- [ ] **Step 3: Regenerate + lint**

Run: `task proto && task web:generate && task lint:proto`
Expected: PASS (every new element documented; name-echo gate satisfied).

- [ ] **Step 4: Commit**

`jj commit -m "proto(core,web): ListAllCharacters directory RPC (holomush-5rh.24)"`

---

### Task 3: ABAC gate + CoreServer `ListAllCharacters` handler — binds INV-ACCESS-9

**Files:**

- Modify: `internal/access/prefix.go` (new `character_directory` resource type + helper)
- Modify: `internal/access/policy/seed.go` (+ `seed_test.go`) — new action + default-permit seed
- Modify: `internal/grpc/auth_handlers.go` (handler, after `ListCharacters` `:540`) (+ `auth_handlers_test.go`)
- Modify: `docs/architecture/invariants.yaml` (flip INV-ACCESS-9 → `bound`) + regenerate `.md`

This is the security-sensitive task — **`abac-reviewer` MUST run.** The directory read is ABAC-gated (not authentication-only) so enumeration stays policy-configurable/auditable, mirroring `list_presence` (ADR `holomush-lp65`). `ListFocusPresence` (`internal/grpc/list_focus_presence.go`) is the engine-gated read precedent to mirror for the engine call + subject resolution.

- [ ] **Step 1: Register the `character_directory` resource type**

In `internal/access/prefix.go`, add the prefix constant (mirroring `ResourceLocation`, `:24`), append it to the valid-prefix list (`:49-56`), and add a helper:

```go
// ResourceCharacterDirectory is the singleton directory resource (no instance id).
ResourceCharacterDirectory = "character_directory:"
```

```go
// CharacterDirectoryResource returns the singleton directory resource ref.
func CharacterDirectoryResource() string { return ResourceCharacterDirectory + "all" }
```

**Resource-type recognition (the most open-ended part of Task 3 — resolve it first):** adding the prefix to `knownPrefixes` (above) is what lets the DSL match `resource is character_directory`. The seed permit has no `when` clause, so **no attribute resolution is needed** — but confirm whether the engine still requires a registered resource-attribute provider for the type to be recognized (the `TestValidateSeedProviderCoverage_*` meta-test in `internal/access/policy` is the authority on what coverage a target-only seed needs). If a provider is required, register a stub returning empty attrs, mirroring the simplest existing one (`location`/`stream`); if target-only seeds are exempt (as the meta-test name suggests), no provider is needed. Verify with that meta-test before moving on.

- [ ] **Step 2: Add the action + default-permit seed**

In `internal/access/policy/seed.go`, add a permit policy (mirroring `seed:player-location-list-presence`, `:170-174`) and bump the seed-count comment (`:14`):

```go
{
    Name:        "seed:directory-list-characters",
    Description: "Any authenticated character (incl. guest) may list the character directory (names only)",
    DSLText:     `permit(principal is character, action in ["list_character_directory"], resource is character_directory);`,
    SeedVersion: 1,
},
```

Add a `seed_test.go` case asserting a plain `character` principal is permitted `list_character_directory` on `character_directory:all` (mirror the `list_presence` seed test). **Also update the two existing count/name assertions that this new seed breaks** (a step-by-step worker will otherwise hit unexpected reds): `TestSeedPoliciesEffectDistribution` (`seed_test.go:74`) asserts an exact permit count — bump it by one (confirm the current value, ~40→41); `TestSeedPoliciesExpectedNames` (`seed_test.go:78`) `ElementsMatch`es a hardcoded name list — add `"seed:directory-list-characters"`. Keep the `seed.go` doc-comment seed count (`:14`) in sync.

- [ ] **Step 3: Write the failing handler tests**

Add to `internal/grpc/auth_handlers_test.go`, mirroring `ListFocusPresence`/`ListCharacters` tests. Cover: permit + owned char → entries; **guest owned char → entries** (guest IS a `character` principal, seed permits); engine deny → `PermissionDenied` (store never called); missing token → `Unauthenticated`; `character_id` not owned by the session → `PermissionDenied`/`NotFound`.

```go
// Verifies: INV-ACCESS-9
func TestListAllCharactersDeniedWhenPolicyDenies(t *testing.T) {
	// engine denies list_character_directory → PermissionDenied; charRepo.ListAll never called.
	server := newTestCoreServer(t, withDenyEngine(), withOwnedChar("char-1"), WithCharacterRepo(/* unused */))
	_, err := server.ListAllCharacters(context.Background(), &corev1.ListAllCharactersRequest{
		PlayerSessionToken: testPlayerSessionToken, CharacterId: "char-1"})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestListAllCharactersReturnsDirectoryForAnyAuthenticatedCaller(t *testing.T) {
	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListAll(mock.Anything).Return([]*world.Character{
		{ID: idgen.New(), Name: "Alice"}, {ID: idgen.New(), Name: "Bob"},
	}, nil).Once()
	// guest session owning char-1 + permit engine
	server := newTestCoreServer(t, withGuestSession(), withOwnedChar("char-1"), withPermitEngine(), WithCharacterRepo(charRepo))
	resp, err := server.ListAllCharacters(context.Background(), &corev1.ListAllCharactersRequest{
		PlayerSessionToken: testPlayerSessionToken, CharacterId: "char-1"})
	require.NoError(t, err)
	require.Len(t, resp.GetCharacters(), 2)
}

func TestListAllCharactersRejectsMissingToken(t *testing.T) {
	server := newTestCoreServer(t)
	_, err := server.ListAllCharacters(context.Background(), &corev1.ListAllCharactersRequest{CharacterId: "char-1"})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}
```

The `// Verifies: INV-ACCESS-9` annotation on the deny test is what binds the invariant. (`withDenyEngine`/`withPermitEngine`/`withOwnedChar` are illustrative — use the file's real engine-stub + session helpers; read the `ListFocusPresence` test for the deny-engine pattern.)

- [ ] **Step 4: Run to verify failure**

Run: `task test -- -run TestListAllCharacters ./internal/grpc/`
Expected: FAIL — `server.ListAllCharacters` undefined.

- [ ] **Step 5: Implement the handler**

After `ListCharacters` (`:540`). Resolve session (guest-inclusive) → verify the session owns `character_id` → ABAC-evaluate → `ListAll`:

```go
// ListAllCharacters returns the id+name of every character for the directory
// picker (fetch-all). Authorization: the acting character (ownership-verified)
// must be permitted action=list_character_directory on the character_directory
// resource — seeded default-permit for any authenticated character incl. guest
// (INV-ACCESS-9). Connection state is excluded (separately permissioned).
func (s *CoreServer) ListAllCharacters(ctx context.Context, req *corev1.ListAllCharactersRequest) (*corev1.ListAllCharactersResponse, error) {
	ps, err := s.resolvePlayerSession(ctx, req.GetPlayerSessionToken()) // guest-inclusive; Unauthenticated on bad token
	if err != nil {
		return nil, err
	}
	// Verify the acting alt belongs to this session (prevents ABAC-subject spoofing).
	if err := s.verifyOwnedCharacter(ctx, ps.PlayerID, req.GetCharacterId()); err != nil {
		return nil, err // NotFound / PermissionDenied
	}
	// ABAC gate — mirror ListFocusPresence (list_focus_presence.go:116-137).
	accessReq, err := types.NewAccessRequest(
		access.CharacterSubject(req.GetCharacterId()),
		"list_character_directory",
		access.CharacterDirectoryResource(),
		nil, // no extra context attributes
	)
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}
	dec, err := s.accessEngine.Evaluate(ctx, accessReq)
	if err != nil {
		errutil.LogErrorContext(ctx, "core: list-directory ABAC error", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if !dec.IsAllowed() {
		return nil, status.Error(codes.PermissionDenied, "not permitted to list the character directory")
	}
	if s.charRepo == nil {
		return nil, status.Error(codes.Internal, "internal error")
	}
	chars, err := s.charRepo.ListAll(ctx)
	if err != nil {
		errutil.LogErrorContext(ctx, "core: list all characters failed", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	out := make([]*corev1.CharacterDirectoryEntry, 0, len(chars))
	for _, c := range chars {
		out = append(out, &corev1.CharacterDirectoryEntry{CharacterId: c.ID.String(), Name: c.Name})
	}
	return &corev1.ListAllCharactersResponse{Characters: out}, nil
}
```

The ABAC shape above is grounded in `list_focus_presence.go:116-137` (`types.NewAccessRequest(subject, action, resource, nil)` → `s.accessEngine.Evaluate(ctx, req)` → `(types.Decision, error)`, `decision.IsAllowed()`). Ground at impl time only the token→session resolver and the ownership check. For ownership, mirror `SceneAccessServer.ownedCharacter` (`internal/grpc/sceneaccess_service.go:109`): `s.charRepo.ListByPlayer(ctx, ps.PlayerID)` then confirm `character_id` is in the result. Do **not** call `world.CharacterRepository.IsOwnedByPlayer` — `CoreServer.charRepo` is typed `auth.CharacterRepository`, which has no such method (it would not compile). `resolvePlayerSession`/`verifyOwnedCharacter` are illustrative names. Add the `internal/access/policy/types` import for `NewAccessRequest`.

- [ ] **Step 6: Run to verify pass + bind INV-ACCESS-9**

Run: `task test -- -run 'TestListAllCharacters' ./internal/grpc/` and the seed test — both PASS.
Then flip the registry entry (added as `binding: pending` when this design was finalized): in `docs/architecture/invariants.yaml` set INV-ACCESS-9 `binding: bound` + `asserted_by: ["internal/grpc/auth_handlers_test.go"]`, run `go run ./cmd/inv-render`, and confirm `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/`.

- [ ] **Step 7: Commit**

`jj commit -m "feat(core): ABAC-gated ListAllCharacters directory (list_character_directory, INV-ACCESS-9) (holomush-5rh.24)"`

---

### Task 4: Web BFF — `WebListAllCharacters` + client wiring

**Files:**

- Modify: `internal/grpc/client.go`, `internal/web/handler.go` (`CoreClient` iface), `internal/web/auth_handlers.go`, `cmd/holomush/deps.go`/`deps_test.go`
- Test: `internal/web/auth_handlers_test.go`

- [ ] **Step 1: Add the concrete `Client` wrapper**

In `internal/grpc/client.go`, after `ListCharacters` (`:206`), mirroring it:

```go
// ListAllCharacters delegates to CoreService.ListAllCharacters (directory).
func (c *Client) ListAllCharacters(ctx context.Context, req *corev1.ListAllCharactersRequest) (*corev1.ListAllCharactersResponse, error) {
	resp, err := c.client.ListAllCharacters(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "ListAllCharacters").Wrap(err)
	}
	return resp, nil
}
```

- [ ] **Step 2: Add to the `CoreClient` interface (and `GRPCClient` if reached)**

In `internal/web/handler.go`, add to whichever interface the web `Handler` uses for core RPCs (the one already declaring `ListCharacters`):

```go
	ListAllCharacters(ctx context.Context, req *corev1.ListAllCharactersRequest) (*corev1.ListAllCharactersResponse, error)
```

If `cmd/holomush/deps.go`'s `GRPCClient` narrowing interface or `mockGRPCClient` (`deps_test.go`) declares the core RPCs the web handler needs, add the same signature there + a `return nil, nil` mock method (the `.22`/lifecycle compile-gate lesson, spec §6). Verify with `task build`, not just `./internal/web`.

- [ ] **Step 3: Write the failing BFF test**

Add to `internal/web/auth_handlers_test.go`, mirroring `TestWebListCharacters*` (the test exercising `WebListCharacters` near `auth_handlers.go:268`). Extend `mockCoreClient` with a `listAllCharactersResp`/method, then:

```go
func TestWebListAllCharactersForwardsTokenAndReturnsDirectory(t *testing.T) {
	client := &mockCoreClient{
		listAllCharactersResp: &corev1.ListAllCharactersResponse{
			Characters: []*corev1.CharacterDirectoryEntry{{CharacterId: "c1", Name: "Alice"}},
		},
	}
	h := NewHandler(client)
	req := requestWithToken(&webv1.WebListAllCharactersRequest{CharacterId: "char-1"}, "tok-dir")

	resp, err := h.WebListAllCharacters(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetCharacters(), 1)
	assert.Equal(t, "Alice", resp.Msg.GetCharacters()[0].GetName())
	require.NotNil(t, client.listAllCharactersReq)
	assert.Equal(t, "tok-dir", client.listAllCharactersReq.GetPlayerSessionToken())
	assert.Equal(t, "char-1", client.listAllCharactersReq.GetCharacterId())
}
```

(`requestWithToken` and `mockCoreClient` already exist — see `TestWebListPlayerSessions_*` and the `WebListCharacters` tests.)

- [ ] **Step 4: Implement the BFF handler**

In `internal/web/auth_handlers.go`, after `WebListCharacters` (`:268`), following its token-from-header → core-call → translate shape. Note one deliberate divergence: errors **pass through** as gRPC status (`return nil, err //nolint:wrapcheck`, the scene-handler convention) rather than `WebListCharacters`' `connect.NewError(...)` wrapping — the directory's `Unauthenticated`/`Internal` codes are already correct on the wire:

```go
// WebListAllCharacters returns the full character directory (id+name) for the
// picker. Any authenticated caller (guest included) may list names.
func (h *Handler) WebListAllCharacters(ctx context.Context, req *connect.Request[webv1.WebListAllCharactersRequest]) (*connect.Response[webv1.WebListAllCharactersResponse], error) {
	slog.DebugContext(ctx, "web: WebListAllCharacters")
	token, err := playerTokenFromHeader(req.Header())
	if err != nil {
		return nil, err
	}
	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	coreResp, err := h.client.ListAllCharacters(rpcCtx, &corev1.ListAllCharactersRequest{
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: list all characters RPC failed", err)
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return connect.NewResponse(&webv1.WebListAllCharactersResponse{Characters: coreResp.GetCharacters()}), nil
}
```

- [ ] **Step 5: Run tests + build**

Run: `task test -- -run 'TestWebListAllCharacters' ./internal/web/` then `task build`
Expected: both PASS.

- [ ] **Step 6: Commit**

`jj commit -m "feat(web): WebListAllCharacters BFF + client wiring (holomush-5rh.24)"`

---

### Task 5: Web client wrapper — `listAllCharacters`

**Files:**

- Create: `web/src/lib/scenes/directoryClient.ts`

- [ ] **Step 1: Write the wrapper**

Create `web/src/lib/scenes/directoryClient.ts`, mirroring `client.ts`'s shape (reuse the shared `client`/`transport` if appropriate, or the existing connect client):

```typescript
import { client } from './client';

export interface DirectoryCharacter {
	id: string;
	name: string;
}

/** Lists every character (id + name) for the invite picker. Names only.
 *  characterId is the acting alt (forwarded as the ABAC subject). */
export async function listAllCharacters(characterId: string): Promise<DirectoryCharacter[]> {
	const res = await client.webListAllCharacters({ characterId });
	return res.characters.map((c) => ({ id: c.characterId, name: c.name }));
}
```

- [ ] **Step 2: Typecheck**

Run: `cd web && pnpm check`
Expected: PASS (the generated `webListAllCharacters` method exists from Task 2).

- [ ] **Step 3: Commit**

`jj commit -m "feat(web): listAllCharacters directory client wrapper (holomush-5rh.24)"`

---

### Task 6: `CharacterMultiSelect` picker (shadcn-svelte command + popover)

**Files:**

- Create: `web/src/lib/components/ui/command/**`, `web/src/lib/components/ui/popover/**` (via CLI)
- Create: `web/src/lib/components/scenes/CharacterMultiSelect.svelte`
- Create/Test: `web/src/lib/components/scenes/CharacterMultiSelect.svelte.test.ts`

Per `web/CLAUDE.md`, shadcn components are added via the CLI and consume `--color-*` tokens. The current API (verified via context7, shadcn-svelte nova) builds a combobox from `Command` inside `Popover`.

- [ ] **Step 1: Add the shadcn primitives**

Run: `cd web && pnpm dlx shadcn-svelte@latest add command popover`
Expected: creates `src/lib/components/ui/command/` and `src/lib/components/ui/popover/`. Commit the generated files. (If the CLI prompts, accept defaults matching `components.json` style `nova`.)

- [ ] **Step 2: Write the failing component test**

Create `web/src/lib/components/scenes/CharacterMultiSelect.svelte.test.ts`, using the repo's raw-`svelte`-mount + jsdom idiom (per the lifecycle-slice lesson — the repo does NOT use `@testing-library/svelte`; mirror `SceneListItem.svelte.test.ts`):

```typescript
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { mount, unmount } from 'svelte';
import CharacterMultiSelect from './CharacterMultiSelect.svelte';

vi.mock('$lib/scenes/directoryClient', () => ({
	listAllCharacters: vi.fn(async () => [
		{ id: 'c1', name: 'Alice' },
		{ id: 'c2', name: 'Bob' },
	]),
}));

describe('CharacterMultiSelect', () => {
	beforeEach(() => vi.clearAllMocks());

	it('loads the directory for the acting alt on mount', async () => {
		const onChange = vi.fn();
		const target = document.createElement('div');
		document.body.appendChild(target);
		const comp = mount(CharacterMultiSelect, { target, props: { characterId: 'char-me', selected: [], onChange } });
		// Open + toggle handled via component API; assert directory was fetched with the acting alt.
		const { listAllCharacters } = await import('$lib/scenes/directoryClient');
		expect(listAllCharacters).toHaveBeenCalledWith('char-me');
		unmount(comp);
		target.remove();
	});
});
```

(Keep assertions to behavior the raw-mount idiom can reach — the directory fetch on mount with the acting alt. The `onChange`/selection contract is **not** unit-testable here: the picker's `Command.Item`s render in a portal that only opens on interaction, which the no-`@testing-library` mount idiom cannot drive. That contract is verified end-to-end by the Task 15 E2E, which opens the picker, selects a character, and invites. Do not fake a selection at the unit level.)

- [ ] **Step 3: Run to verify failure**

Run: `cd web && pnpm test:unit -- CharacterMultiSelect`
Expected: FAIL — component does not exist.

- [ ] **Step 4: Implement the picker**

Create `web/src/lib/components/scenes/CharacterMultiSelect.svelte`, adapting the shadcn-svelte combobox recipe to **multi-select** (track an id array; toggle on select; check-mark selected; trigger shows a count):

```svelte
<script lang="ts">
	import CheckIcon from '@lucide/svelte/icons/check';
	import ChevronsUpDownIcon from '@lucide/svelte/icons/chevrons-up-down';
	import * as Command from '$lib/components/ui/command/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { cn } from '$lib/utils.js';
	import { listAllCharacters, type DirectoryCharacter } from '$lib/scenes/directoryClient';

	let {
		characterId,
		selected = [],
		onChange,
	}: { characterId: string; selected?: string[]; onChange: (ids: string[]) => void } = $props();

	let open = $state(false);
	let chars = $state<DirectoryCharacter[]>([]);
	let loadError = $state(false);

	$effect(() => {
		listAllCharacters(characterId)
			.then((c) => (chars = c))
			.catch(() => (loadError = true));
	});

	function toggle(id: string) {
		const next = selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id];
		onChange(next);
	}
</script>

<Popover.Root bind:open>
	<Popover.Trigger>
		{#snippet child({ props })}
			<Button {...props} variant="outline" role="combobox" aria-expanded={open}
				class="w-full justify-between" name="invite-picker">
				{selected.length ? `${selected.length} selected` : 'Invite characters…'}
				<ChevronsUpDownIcon class="opacity-50" />
			</Button>
		{/snippet}
	</Popover.Trigger>
	<Popover.Content class="w-[260px] p-0">
		<Command.Root>
			<Command.Input placeholder="Search characters…" />
			<Command.List>
				<Command.Empty>{loadError ? 'Failed to load.' : 'No characters found.'}</Command.Empty>
				<Command.Group>
					{#each chars as c (c.id)}
						<Command.Item value={c.name} onSelect={() => toggle(c.id)}>
							<CheckIcon class={cn(!selected.includes(c.id) && 'text-transparent')} />
							{c.name}
						</Command.Item>
					{/each}
				</Command.Group>
			</Command.List>
		</Command.Root>
	</Popover.Content>
</Popover.Root>
```

(`Command.Input` filters by the `value` prop — character name — giving type-ahead for free. Confirm `@lucide/svelte` is the icon import path the repo uses; `TopBar.svelte` imports lucide icons — match that exact package specifier.)

- [ ] **Step 5: Run tests + typecheck**

Run: `cd web && pnpm test:unit -- CharacterMultiSelect && pnpm check`
Expected: PASS.

- [ ] **Step 6: Commit**

`jj commit -m "feat(web): CharacterMultiSelect directory picker (holomush-5rh.24)"`

---

## Phase 2: Membership verbs

### Task 7: Proto — facade + BFF membership RPCs

**Files:**

- Modify: `api/proto/holomush/sceneaccess/v1/sceneaccess.proto`, `api/proto/holomush/web/v1/web.proto`

The `scenev1` membership RPCs/messages already exist (`scene.proto:98-115, 534-588`) — do not touch them. Only the facade + BFF layers are new. Mirror the shipped lifecycle facade/BFF RPCs (`EndScene`/`WebEndScene`).

- [ ] **Step 1: Add the four facade RPCs to `SceneAccessService`**

In `sceneaccess.proto`, after `ResumeScene` (`:78`):

```protobuf
  // InviteToScene resolves the verified acting character from the player session
  // (INV-SCENE-63), rejects guests (INV-SCENE-64), then forwards to
  // SceneService.InviteToScene, which self-enforces the ABAC `invite` policy
  // (participant-wide per the relaxation, INV-SCENE-65).
  rpc InviteToScene(InviteToSceneRequest) returns (InviteToSceneResponse);

  // KickFromScene forwards to SceneService.KickFromScene, which self-enforces the
  // owner-only `kick` policy (INV-SCENE-65). Same identity/guest gating as above.
  rpc KickFromScene(KickFromSceneRequest) returns (KickFromSceneResponse);

  // TransferOwnership forwards to SceneService.TransferOwnership, which
  // self-enforces the owner-only `transfer-ownership` policy (INV-SCENE-65).
  rpc TransferOwnership(TransferOwnershipRequest) returns (TransferOwnershipResponse);

  // LeaveScene forwards to SceneService.LeaveScene, which self-enforces the
  // participant `leave` policy (INV-SCENE-65). The owner cannot leave.
  rpc LeaveScene(LeaveSceneRequest) returns (LeaveSceneResponse);
```

- [ ] **Step 2: Add the facade messages**

At the end of `sceneaccess.proto`, mirroring `EndSceneRequest` (token-authenticated, server-side identity). Invite/Kick add `target_character_id`; Transfer adds `new_owner_character_id`; Leave mirrors EndScene. Responses are empty (the plugin RPCs return empties):

```protobuf
// InviteToSceneRequest authenticates the inviter and names the invitee.
message InviteToSceneRequest {
  // session_id is the client-declared player-session ULID (hint only).
  string session_id = 1;
  // player_session_token authenticates the caller; guests are rejected.
  string player_session_token = 2;
  // character_id selects the acting owned alt (ownership verified server-side).
  string character_id = 3;
  // scene_id identifies the scene to invite into; required.
  string scene_id = 4;
  // target_character_id is the invitee (from the directory picker); required.
  string target_character_id = 5;
}

// InviteToSceneResponse is empty; success is signaled by the absence of error.
message InviteToSceneResponse {}

// KickFromSceneRequest authenticates the acting owner and names the target.
message KickFromSceneRequest {
  // session_id is the client-declared player-session ULID (hint only).
  string session_id = 1;
  // player_session_token authenticates the caller; guests are rejected.
  string player_session_token = 2;
  // character_id selects the acting owned alt (ownership verified server-side).
  string character_id = 3;
  // scene_id identifies the scene; required.
  string scene_id = 4;
  // target_character_id is the member to remove (not the owner); required.
  string target_character_id = 5;
}

// KickFromSceneResponse is empty.
message KickFromSceneResponse {}

// TransferOwnershipRequest authenticates the current owner and names the heir.
message TransferOwnershipRequest {
  // session_id is the client-declared player-session ULID (hint only).
  string session_id = 1;
  // player_session_token authenticates the caller; guests are rejected.
  string player_session_token = 2;
  // character_id selects the acting owned alt (the current owner); verified.
  string character_id = 3;
  // scene_id identifies the scene; required.
  string scene_id = 4;
  // new_owner_character_id is the existing member who becomes owner; required.
  string new_owner_character_id = 5;
}

// TransferOwnershipResponse is empty.
message TransferOwnershipResponse {}

// LeaveSceneRequest authenticates the leaving participant.
message LeaveSceneRequest {
  // session_id is the client-declared player-session ULID (hint only).
  string session_id = 1;
  // player_session_token authenticates the caller; guests are rejected.
  string player_session_token = 2;
  // character_id selects the acting owned alt (the leaver); verified.
  string character_id = 3;
  // scene_id identifies the scene to leave; required.
  string scene_id = 4;
}

// LeaveSceneResponse is empty.
message LeaveSceneResponse {}
```

- [ ] **Step 3: Add the four BFF RPCs to `WebService`**

In `web.proto`, after `WebResumeScene` (`:290`):

```protobuf
  // WebInviteToScene proxies to SceneAccessService.InviteToScene (cookie token).
  rpc WebInviteToScene(WebInviteToSceneRequest) returns (WebInviteToSceneResponse);
  // WebKickFromScene proxies to SceneAccessService.KickFromScene.
  rpc WebKickFromScene(WebKickFromSceneRequest) returns (WebKickFromSceneResponse);
  // WebTransferOwnership proxies to SceneAccessService.TransferOwnership.
  rpc WebTransferOwnership(WebTransferOwnershipRequest) returns (WebTransferOwnershipResponse);
  // WebLeaveScene proxies to SceneAccessService.LeaveScene.
  rpc WebLeaveScene(WebLeaveSceneRequest) returns (WebLeaveSceneResponse);
```

- [ ] **Step 4: Add the BFF messages**

At the end of `web.proto`, mirroring `WebEndSceneRequest` (token injected from cookie; no `player_session_token` field). Empty responses:

```protobuf
// WebInviteToSceneRequest proxies to SceneAccessService.InviteToScene.
message WebInviteToSceneRequest {
  // session_id is forwarded to the facade.
  string session_id = 1;
  // character_id selects the acting owned alt (the inviter).
  string character_id = 2;
  // scene_id identifies the scene; required.
  string scene_id = 3;
  // target_character_id is the invitee from the directory picker; required.
  string target_character_id = 4;
}
// WebInviteToSceneResponse is empty.
message WebInviteToSceneResponse {}

// WebKickFromSceneRequest proxies to SceneAccessService.KickFromScene.
message WebKickFromSceneRequest {
  string session_id = 1;
  // character_id selects the acting owned alt (the owner).
  string character_id = 2;
  // scene_id identifies the scene; required.
  string scene_id = 3;
  // target_character_id is the member to remove; required.
  string target_character_id = 4;
}
// WebKickFromSceneResponse is empty.
message WebKickFromSceneResponse {}

// WebTransferOwnershipRequest proxies to SceneAccessService.TransferOwnership.
message WebTransferOwnershipRequest {
  string session_id = 1;
  // character_id selects the acting owned alt (the current owner).
  string character_id = 2;
  // scene_id identifies the scene; required.
  string scene_id = 3;
  // new_owner_character_id is the existing member who becomes owner; required.
  string new_owner_character_id = 4;
}
// WebTransferOwnershipResponse is empty.
message WebTransferOwnershipResponse {}

// WebLeaveSceneRequest proxies to SceneAccessService.LeaveScene.
message WebLeaveSceneRequest {
  string session_id = 1;
  // character_id selects the acting owned alt (the leaver).
  string character_id = 2;
  // scene_id identifies the scene to leave; required.
  string scene_id = 3;
}
// WebLeaveSceneResponse is empty.
message WebLeaveSceneResponse {}
```

- [ ] **Step 5: Regenerate + lint**

Run: `task proto && task web:generate && task lint:proto`
Expected: PASS.

- [ ] **Step 6: Commit**

`jj commit -m "proto(scenes): facade + BFF membership RPCs (invite/kick/transfer/leave) (holomush-5rh.24)"`

---

### Task 8: Self-gate the SceneService membership handlers — advances INV-SCENE-65

**Files:**

- Modify: `plugins/core-scenes/service.go` — `LeaveScene` (`:1245`), `InviteToScene` (`:1317`), `KickFromScene` (`:1354`), `TransferOwnership` (`:1402`)
- Test: `plugins/core-scenes/service_test.go`

The four handlers currently call the store directly with no evaluator (confirmed: evaluator fires only at end/pause/resume/spectate). The telnet wrapper gates today; the facade path bypasses it. Add the self-gate as the **first** step of each handler, mirroring the shipped lifecycle gate (`service.go:652-662`, the `if s.evaluator == nil` block through the `!dec.Allowed` return). Actions per spec §4.1: `invite` / `kick` / `transfer-ownership` / `leave`.

- [ ] **Step 1: Write the failing deny-path tests**

Add to `plugins/core-scenes/service_test.go` (the `denyEvaluator{}`/`allowEvaluator{}` doubles + `SetHostEvaluator` already exist — used by the lifecycle tests):

```go
func TestSceneServiceInviteToSceneDeniedWhenPolicyDenies(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-1"] = &SceneRow{ID: "scene-1", OwnerID: "char-bob", State: string(SceneStateActive)}
	svc := newTestService(t, store)
	svc.SetHostEvaluator(denyEvaluator{})

	_, err := svc.InviteToScene(context.Background(), &scenev1.InviteToSceneRequest{
		CharacterId: "char-mallory", SceneId: "scene-1", TargetCharacterId: "char-eve"})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestSceneServiceKickFromSceneDeniedWhenPolicyDenies(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-1"] = &SceneRow{ID: "scene-1", State: string(SceneStateActive)}
	svc := newTestService(t, store)
	svc.SetHostEvaluator(denyEvaluator{})
	_, err := svc.KickFromScene(context.Background(), &scenev1.KickFromSceneRequest{
		CharacterId: "char-mallory", SceneId: "scene-1", TargetCharacterId: "char-eve"})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestSceneServiceTransferOwnershipDeniedWhenPolicyDenies(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-1"] = &SceneRow{ID: "scene-1", State: string(SceneStateActive)}
	svc := newTestService(t, store)
	svc.SetHostEvaluator(denyEvaluator{})
	_, err := svc.TransferOwnership(context.Background(), &scenev1.TransferOwnershipRequest{
		CharacterId: "char-mallory", SceneId: "scene-1", NewOwnerCharacterId: "char-eve"})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestSceneServiceLeaveSceneDeniedWhenPolicyDenies(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-1"] = &SceneRow{ID: "scene-1", OwnerID: "char-owner", State: string(SceneStateActive)}
	svc := newTestService(t, store)
	svc.SetHostEvaluator(denyEvaluator{})
	_, err := svc.LeaveScene(context.Background(), &scenev1.LeaveSceneRequest{
		CharacterId: "char-mallory", SceneId: "scene-1"})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}
```

Do **not** add a `// Verifies: INV-SCENE-65` annotation — the invariant covers 8 handlers and `UpdateScene` is still ungated (Settings slice); a binding now would be a false-green partial binding. `holomush-5rh.24.9` owns the flip once all 8 gate.

- [ ] **Step 2: Run to verify failure**

Run: `task test -- -run 'TestSceneService(Invite|Kick|Transfer|Leave).*DeniedWhenPolicyDenies' ./plugins/core-scenes/`
Expected: FAIL — handlers don't gate yet.

- [ ] **Step 3: Add the self-gate to each handler**

In `plugins/core-scenes/service.go`, insert immediately after each handler's `defer span.End()` and **before** its first store call. For `InviteToScene` (after `:1324`, before `:1326`), with action `"invite"`:

```go
	if s.evaluator == nil {
		slog.WarnContext(ctx, "scene.membership.invite evaluator not configured",
			"subject_id", req.GetCharacterId(), "scene_id", req.GetSceneId())
		return nil, status.Error(codes.Internal, "permission check unavailable") //nolint:wrapcheck // fail-closed opaque error
	}
	dec, evalErr := s.evaluator.Evaluate(ctx, "invite", "scene:"+req.GetSceneId())
	if evalErr != nil {
		recordError(span, evalErr)
		errutil.LogErrorContext(ctx, "scene.membership.invite evaluation failed", evalErr)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // opaque Internal per grpc-errors.md
	}
	if !dec.Allowed {
		return nil, status.Error(codes.PermissionDenied, "not permitted to invite to this scene") //nolint:wrapcheck
	}
```

Repeat in `KickFromScene` (after `:1361`, before `:1363`) with action `"kick"` / message `"not permitted to kick from this scene"`; `TransferOwnership` (after `:1409`, before `:1411`) with action `"transfer-ownership"` / message `"not permitted to transfer this scene"`; and `LeaveScene` (after `:1251`, before the `s.store.Get` at `:1257`) with action `"leave"` / message `"not permitted to leave this scene"`. (`errutil`/`slog` already imported.)

Also broaden the `SetHostEvaluator` doc comment (`service.go:231-237`): it currently names only the lifecycle/spectate gates — extend it to note the evaluator now also gates the four membership handlers (`invite`/`kick`/`transfer-ownership`/`leave`).

> Note on `TransferOwnership`: the store layer already maps `SCENE_NOT_OWNER`→`PermissionDenied` (`service.go:1418`). The self-gate is still required — it makes the `transfer-ownership` Cedar policy the canonical gate on the facade path (the store check is defense-in-depth, spec §5.3).

- [ ] **Step 4: Wire `allowEvaluator` into existing membership tests**

The existing handler tests build the service with no evaluator → the fail-closed gate now breaks them. After each `svc := newTestService(t, store)` in the existing `Invite`/`Kick`/`Transfer`/`Leave` tests, add `svc.SetHostEvaluator(allowEvaluator{})`. Locate them: `task test -- -run 'TestSceneService(Invite|Kick|Transfer|Leave)' ./plugins/core-scenes/` and fix each that now fails with `codes.Internal "permission check unavailable"`.

- [ ] **Step 5: Run the package + the telnet command path**

Run: `task test -- ./plugins/core-scenes/`
Expected: PASS — new deny tests pass; allow-wired existing tests pass; the telnet command tests still pass (handler now double-evaluates with the same result, harmless per spec §5.3).

- [ ] **Step 6: Commit**

`jj commit -m "feat(scenes): self-gate ABAC in membership RPC handlers (advances INV-SCENE-65) (holomush-5rh.24)"`

---

### Task 9: Relax the `invite-to-scene` Cedar policy (owner → participant)

**Files:**

- Modify: `plugins/core-scenes/plugin.yaml:321-322`
- Test: the plugin's policy test (mirror an existing `invite` policy test)

Per spec §4.2. **This is a Cedar-policy change — `abac-reviewer` MUST run on this slice.**

- [ ] **Step 1: Edit the policy**

In `plugins/core-scenes/plugin.yaml`, the `invite-to-scene` policy (`:319-322`) currently reads:

```text
permit(principal is character, action in ["invite"], resource is scene) when { resource.scene.owner == principal.id
&& resource.scene.state in ["active", "paused"] };
```

Change the subject clause to participant-wide, **retaining** the state clause (spec §4.2 — the `InviteParticipant` store method does not independently enforce scene state):

```text
permit(principal is character, action in ["invite"], resource is scene) when { principal.id in resource.scene.participants
&& resource.scene.state in ["active", "paused"] };
```

Leave `kick-from-scene` (`:323-326`) and `transfer-ownership` (`:327+`) owner-only. Update the `:255-258` comment block only if it names invite as owner-only.

- [ ] **Step 2: Add/extend the policy test**

Add a test asserting a **non-owner participant** is permitted `invite` while a **non-participant** is denied (mirror the nearest existing scene-policy test). Run the existing scene policy/command suite to confirm no regression in owner-only verbs:

Run: `task test -- -run 'Invite' ./plugins/core-scenes/`
Expected: PASS — participant-invite allowed; non-participant denied; kick/transfer unaffected.

- [ ] **Step 3: Schema/lint**

Run: `task lint`
Expected: PASS (`plugin.yaml` validates against `schemas/plugin.schema.json`).

- [ ] **Step 4: Commit**

`jj commit -m "feat(scenes): relax invite-to-scene policy to participant-wide (holomush-5rh.24)"`

---

### Task 10: Facade methods on `SceneAccessServer`

**Files:**

- Modify: `internal/grpc/sceneaccess_service.go` (after the lifecycle methods)
- Test: `internal/grpc/sceneaccess_service_test.go`

Mirror the shipped `EndScene` facade method (`:359`): `resolveAndGate` (guest deny) → `ownedCharacter` (verify owned) → `beginDispatch` → forward → return. Responses are empty.

- [ ] **Step 1: Write the failing facade tests**

Add to `internal/grpc/sceneaccess_service_test.go`, mirroring `TestSceneAccessEndScene` (the shipped lifecycle facade test). For each verb cover: owned character forwards the verified ids; guest denied (downstream never called); not-owned → NotFound. Example for Invite:

```go
func TestSceneAccessInviteToScene(t *testing.T) {
	// ... same scaffold as TestSceneAccessEndScene ...
	// owned path: expect sceneMock.InviteToScene called with verified CharacterId
	// + the passed TargetCharacterId + SceneId; returns &scenev1.InviteToSceneResponse{}.
}
```

Add `TestSceneAccessKickFromScene`, `TestSceneAccessTransferOwnership`, `TestSceneAccessLeaveScene` likewise (swap the mock expectation method + the extra id field; Transfer uses `NewOwnerCharacterId`; Leave has no target).

- [ ] **Step 2: Run to verify failure**

Run: `task test -- -run 'TestSceneAccess(Invite|Kick|Transfer|Leave)' ./internal/grpc/`
Expected: FAIL — methods undefined.

- [ ] **Step 3: Implement the facade methods**

In `internal/grpc/sceneaccess_service.go`, after the lifecycle methods, mirroring `EndScene` (`:359`). Invite example:

```go
// InviteToScene resolves the verified inviter from the player session and
// forwards to the plugin SceneService (which self-enforces the participant-wide
// `invite` policy, INV-SCENE-65). resolveAndGate enforces the guest gate
// (INV-SCENE-64); ownedCharacter enforces ownership of the acting alt (INV-SCENE-63).
func (s *SceneAccessServer) InviteToScene(ctx context.Context, req *sceneaccessv1.InviteToSceneRequest) (*sceneaccessv1.InviteToSceneResponse, error) {
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

	if _, err := s.sceneClient.InviteToScene(dctx, &scenev1.InviteToSceneRequest{
		CharacterId:       char.ID.String(),
		SceneId:           req.GetSceneId(),
		TargetCharacterId: req.GetTargetCharacterId(),
	}); err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.InviteToSceneResponse{}, nil
}
```

Add `KickFromScene` (target id), `TransferOwnership` (`NewOwnerCharacterId`), `LeaveScene` (no target) identically.

- [ ] **Step 4: Run to verify pass**

Run: `task test -- -run 'TestSceneAccess(Invite|Kick|Transfer|Leave)' ./internal/grpc/`
Expected: PASS.

- [ ] **Step 5: Commit**

`jj commit -m "feat(scenes): facade membership methods (invite/kick/transfer/leave) (holomush-5rh.24)"`

---

### Task 11: Client wrappers + narrowing interfaces (compile gate)

**Files:**

- Modify: `internal/grpc/client.go`, `cmd/holomush/deps.go`, `cmd/holomush/deps_test.go`, `internal/web/handler.go`

The `.22`/lifecycle compile-gate lesson (spec §6): each new `SceneAccessClient` method must also be on `GRPCClient` + `mockGRPCClient` or `cmd/holomush` won't build.

- [ ] **Step 1: Add the four concrete `Client` wrappers**

In `internal/grpc/client.go`, after the lifecycle wrappers, mirroring `EndScene` (`oops.Code("RPC_FAILED").With("method", ...)`): `InviteToScene`, `KickFromScene`, `TransferOwnership`, `LeaveScene` (each forwards to `c.sceneAccessClient.<Verb>`).

- [ ] **Step 2: Add the four signatures to all three interfaces + the mock**

- `cmd/holomush/deps.go` `GRPCClient` interface (after the lifecycle methods)
- `internal/web/handler.go` `SceneAccessClient` interface (after the lifecycle methods)
- `cmd/holomush/deps_test.go` `mockGRPCClient` (4 methods returning `nil, nil`)

Signatures (sceneaccess types):

```go
	InviteToScene(ctx context.Context, req *sceneaccessv1.InviteToSceneRequest) (*sceneaccessv1.InviteToSceneResponse, error)
	KickFromScene(ctx context.Context, req *sceneaccessv1.KickFromSceneRequest) (*sceneaccessv1.KickFromSceneResponse, error)
	TransferOwnership(ctx context.Context, req *sceneaccessv1.TransferOwnershipRequest) (*sceneaccessv1.TransferOwnershipResponse, error)
	LeaveScene(ctx context.Context, req *sceneaccessv1.LeaveSceneRequest) (*sceneaccessv1.LeaveSceneResponse, error)
```

- [ ] **Step 3: Verify the whole build (the landmine check)**

Run: `task build`
Expected: PASS. A failure means an interface/mock method is missing or mis-signatured.

- [ ] **Step 4: Commit**

`jj commit -m "feat(scenes): wire membership facade RPCs through client + narrowing interfaces (holomush-5rh.24)"`

---

### Task 12: BFF handlers on `Handler`

**Files:**

- Modify: `internal/web/scene_handlers.go` (after the lifecycle handlers)
- Test: `internal/web/scene_handlers_test.go`

Mirror `WebEndScene` (shipped). Empty responses; token from `headerInjectSessionToken` cookie; errors pass through.

- [ ] **Step 1: Write the failing handler tests**

Extend `mockSceneAccessClient` with capture fields + methods for the four verbs (mirror the lifecycle capture pattern), then add forward tests + an error-passthrough test per verb. Invite example:

```go
func TestWebInviteToSceneForwardsTokenAndFieldsToFacade(t *testing.T) {
	sc := &mockSceneAccessClient{}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))
	req := connect.NewRequest(&webv1.WebInviteToSceneRequest{
		SessionId: "sess-1", CharacterId: "char-1", SceneId: "scene-123", TargetCharacterId: "char-eve"})
	req.Header().Set(headerInjectSessionToken, "tok-abc")

	_, err := h.WebInviteToScene(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, sc.inviteReq)
	assert.Equal(t, "tok-abc", sc.inviteReq.GetPlayerSessionToken())
	assert.Equal(t, "char-eve", sc.inviteReq.GetTargetCharacterId())
	assert.Equal(t, "scene-123", sc.inviteReq.GetSceneId())
}
```

- [ ] **Step 2: Run to verify failure**

Run: `task test -- -run 'TestWebInviteToScene' ./internal/web/`
Expected: FAIL — `h.WebInviteToScene` undefined.

- [ ] **Step 3: Implement the four BFF handlers**

In `internal/web/scene_handlers.go`, after the lifecycle handlers, mirroring `WebEndScene` (nil-client → Unimplemented; token from header; forward; pass error through). Invite forwards `TargetCharacterId`; Transfer forwards `NewOwnerCharacterId`; Leave forwards no target. Each returns `connect.NewResponse(&webv1.Web<Verb>Response{})`.

- [ ] **Step 4: Run tests + build**

Run: `task test -- -run 'TestWeb(Invite|Kick|Transfer|Leave)' ./internal/web/` then `task build`
Expected: both PASS.

- [ ] **Step 5: Commit**

`jj commit -m "feat(web): BFF membership handlers (invite/kick/transfer/leave) (holomush-5rh.24)"`

---

### Task 13: Web client wrappers + membership flow

**Files:**

- Modify: `web/src/lib/scenes/client.ts`
- Create: `web/src/lib/scenes/membershipFlow.ts`, `web/src/lib/scenes/membershipFlow.test.ts`

Membership responses are **empty** (no `SceneInfo`), so per spec §8 the acting client **refetches** the roster via `workspaceStore.select(sceneId, '', characterId)` (which issues `getScene` for roster enrichment, `workspaceStore.svelte.ts:189`) — it does **not** call `applySceneInfo`.

- [ ] **Step 1: Write the failing flow test**

Create `web/src/lib/scenes/membershipFlow.test.ts`, mirroring `lifecycleFlow.test.ts`:

```typescript
import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('./altSessions.svelte', () => ({ ensureSession: vi.fn(async () => 'sess-1') }));
vi.mock('./client', () => ({
	inviteToScene: vi.fn(async () => {}),
	kickFromScene: vi.fn(async () => {}),
	transferOwnership: vi.fn(async () => {}),
	leaveScene: vi.fn(async () => {}),
}));
vi.mock('./workspaceStore.svelte', () => ({ workspaceStore: { select: vi.fn() } }));

import { inviteCharacters, kickAction, transferAction, leaveAction } from './membershipFlow';
import { inviteToScene, kickFromScene, transferOwnership, leaveScene } from './client';
import { workspaceStore } from './workspaceStore.svelte';

describe('membershipFlow', () => {
	beforeEach(() => vi.clearAllMocks());

	it('invites each selected character then refetches the roster', async () => {
		await inviteCharacters({ sceneId: 'scene-1', characterId: 'char-1', targetIds: ['e1', 'e2'] });
		expect(inviteToScene).toHaveBeenCalledTimes(2);
		expect(inviteToScene).toHaveBeenCalledWith('sess-1', { characterId: 'char-1', sceneId: 'scene-1', targetCharacterId: 'e1' });
		expect(workspaceStore.select).toHaveBeenCalledWith('scene-1', '', 'char-1');
	});

	it('kicks then refetches', async () => {
		await kickAction({ sceneId: 'scene-1', characterId: 'char-1', targetCharacterId: 'e1' });
		expect(kickFromScene).toHaveBeenCalledWith('sess-1', { characterId: 'char-1', sceneId: 'scene-1', targetCharacterId: 'e1' });
		expect(workspaceStore.select).toHaveBeenCalled();
	});

	it('transfers ownership then refetches', async () => {
		await transferAction({ sceneId: 'scene-1', characterId: 'char-1', newOwnerCharacterId: 'e1' });
		expect(transferOwnership).toHaveBeenCalledWith('sess-1', { characterId: 'char-1', sceneId: 'scene-1', newOwnerCharacterId: 'e1' });
		expect(workspaceStore.select).toHaveBeenCalled();
	});

	it('leaves then refetches', async () => {
		await leaveAction({ sceneId: 'scene-1', characterId: 'char-1' });
		expect(leaveScene).toHaveBeenCalledWith('sess-1', { characterId: 'char-1', sceneId: 'scene-1' });
		expect(workspaceStore.select).toHaveBeenCalled();
	});
});
```

- [ ] **Step 2: Run to verify failure**

Run: `cd web && pnpm test:unit -- membershipFlow`
Expected: FAIL — `./membershipFlow` does not exist.

- [ ] **Step 3: Add the client wrappers**

In `web/src/lib/scenes/client.ts`, add the type imports (`WebInviteToSceneRequest` etc.) and four wrappers mirroring `endScene`, but returning `void` (empty responses):

```typescript
export async function inviteToScene(
	sessionId: string,
	opts: Pick<WebInviteToSceneRequest, 'characterId' | 'sceneId' | 'targetCharacterId'>,
): Promise<void> {
	await client.webInviteToScene({ sessionId, ...opts });
}
```

Add `kickFromScene` (`webKickFromScene`, same fields), `transferOwnership` (`webTransferOwnership`, `newOwnerCharacterId`), `leaveScene` (`webLeaveScene`, no target).

- [ ] **Step 4: Create the flow**

Create `web/src/lib/scenes/membershipFlow.ts`:

```typescript
import { ensureSession } from './altSessions.svelte';
import { inviteToScene, kickFromScene, transferOwnership, leaveScene } from './client';
import { workspaceStore } from './workspaceStore.svelte';

type Base = { sceneId: string; characterId: string };

async function refetch(sceneId: string, characterId: string): Promise<void> {
	await workspaceStore.select(sceneId, '', characterId);
}

/** Invites every selected character (sequential), then refetches the roster.
 *  Sequential keeps partial-failure semantics simple: the first error aborts and
 *  surfaces; already-sent invites stand. */
export async function inviteCharacters(
	{ sceneId, characterId, targetIds }: Base & { targetIds: string[] },
): Promise<void> {
	const sessionId = await ensureSession(characterId);
	for (const targetCharacterId of targetIds) {
		await inviteToScene(sessionId, { characterId, sceneId, targetCharacterId });
	}
	await refetch(sceneId, characterId);
}

export async function kickAction(
	{ sceneId, characterId, targetCharacterId }: Base & { targetCharacterId: string },
): Promise<void> {
	const sessionId = await ensureSession(characterId);
	await kickFromScene(sessionId, { characterId, sceneId, targetCharacterId });
	await refetch(sceneId, characterId);
}

export async function transferAction(
	{ sceneId, characterId, newOwnerCharacterId }: Base & { newOwnerCharacterId: string },
): Promise<void> {
	const sessionId = await ensureSession(characterId);
	await transferOwnership(sessionId, { characterId, sceneId, newOwnerCharacterId });
	await refetch(sceneId, characterId);
}

export async function leaveAction({ sceneId, characterId }: Base): Promise<void> {
	const sessionId = await ensureSession(characterId);
	await leaveScene(sessionId, { characterId, sceneId });
	await refetch(sceneId, characterId);
}
```

- [ ] **Step 5: Run flow tests + typecheck**

Run: `cd web && pnpm test:unit -- membershipFlow && pnpm check`
Expected: PASS.

- [ ] **Step 6: Commit**

`jj commit -m "feat(web): scene membership client wrappers + flow (holomush-5rh.24)"`

---

### Task 14: Roster actions in `SceneContextRail` (kebab + Leave + Invite picker)

**Files:**

- Modify: `web/src/lib/components/scenes/SceneContextRail.svelte` (Roster section, `:106-182`)
- Modify/Test: `web/src/lib/components/scenes/SceneContextRail.svelte.test.ts`

Visibility predicates (client-side hide/show is UX only; the facade ABAC is the fence):

- **owner** (`scene.ownerId === scene.asCharacterId`): per-member `⋯` kebab with `Transfer` / `Kick` on each **non-self participant** row; the Invite picker.
- **any participant** (`scene.role === 'owner' || 'member'`): the Invite picker (participant-wide, §4.2).
- **non-owner participant**: a `Leave` action.
- Kick/Transfer are hidden behind the per-row kebab (not always-visible, spec §7).

- [ ] **Step 1: Write/extend the failing component tests**

Add to `SceneContextRail.svelte.test.ts` (raw-`svelte`-mount + jsdom idiom; the lifecycle test established the file). Mock `membershipFlow` and `CharacterMultiSelect`. Cover: owner sees a kebab on a member row but **not** on their own row; member sees Leave + the Invite picker but **no** kebab; observer sees none of these. Assert by querying rendered controls (button labels / `name` attrs). Example shape:

```typescript
vi.mock('$lib/scenes/membershipFlow', () => ({
	inviteCharacters: vi.fn(), kickAction: vi.fn(), transferAction: vi.fn(), leaveAction: vi.fn(),
}));
vi.mock('./CharacterMultiSelect.svelte', () => ({ default: () => ({}) })); // stub child
```

(Match the existing test's mount helper and DOM-query style; assert presence/absence of the kebab trigger, the Leave control, and the invite-picker trigger per role.)

- [ ] **Step 2: Run to verify failure**

Run: `cd web && pnpm test:unit -- SceneContextRail`
Expected: FAIL — roster actions not rendered.

- [ ] **Step 3: Implement the roster actions**

In `SceneContextRail.svelte`:

1. Import the flow + UI: `import * as DropdownMenu from '$lib/components/ui/dropdown-menu';`, `import CharacterMultiSelect from './CharacterMultiSelect.svelte';`, `import { Button } from '$lib/components/ui/button/index.js';`, and the `membershipFlow` actions.
2. Add `$derived` flags reusing the lifecycle pattern: `let isOwner = $derived(!!scene && scene.ownerId === scene.asCharacterId);` and `let isParticipant = $derived(!!scene && (scene.role === 'owner' || scene.role === 'member'));`.
3. In each **participant** row (`:117-128`), when `isOwner && p.id !== scene.asCharacterId`, render a kebab:

```svelte
{#if isOwner && p.id !== scene.asCharacterId}
  <DropdownMenu.Root>
    <DropdownMenu.Trigger>
      {#snippet child({ props })}
        <button {...props} class="ml-auto px-1 text-muted-foreground" aria-label={`Manage ${p.name}`}>⋯</button>
      {/snippet}
    </DropdownMenu.Trigger>
    <DropdownMenu.Content align="end">
      <DropdownMenu.Item onSelect={() => transferAction({ sceneId: scene.sceneId, characterId: scene.asCharacterId, newOwnerCharacterId: p.id })}>
        Transfer ownership
      </DropdownMenu.Item>
      <DropdownMenu.Item onSelect={() => kickAction({ sceneId: scene.sceneId, characterId: scene.asCharacterId, targetCharacterId: p.id })}>
        Kick
      </DropdownMenu.Item>
    </DropdownMenu.Content>
  </DropdownMenu.Root>
{/if}
```

4. Below the Participants list, when `isParticipant`, render the Invite picker + a Leave control for non-owners:

```svelte
{#if isParticipant}
  <div class="mt-2 space-y-1.5">
    <CharacterMultiSelect characterId={scene.asCharacterId} selected={inviteIds} onChange={(ids) => (inviteIds = ids)} />
    {#if inviteIds.length}
      <Button size="sm" class="h-6 text-xs"
        onclick={() => inviteCharacters({ sceneId: scene.sceneId, characterId: scene.asCharacterId, targetIds: inviteIds }).then(() => (inviteIds = []))}>
        Invite {inviteIds.length}
      </Button>
    {/if}
    {#if !isOwner}
      <Button variant="outline" size="sm" class="h-6 text-xs"
        onclick={() => leaveAction({ sceneId: scene.sceneId, characterId: scene.asCharacterId })}>Leave</Button>
    {/if}
  </div>
{/if}
```

with `let inviteIds = $state<string[]>([]);` in the script. Disable the actions once `scene.state === 'ended'` (mirror the lifecycle FSM gating) — invite/kick/transfer require active/paused.

- [ ] **Step 4: Run tests + typecheck**

Run: `cd web && pnpm test:unit -- SceneContextRail && pnpm check`
Expected: PASS.

- [ ] **Step 5: Commit**

`jj commit -m "feat(web): roster membership actions (kebab/leave/invite) in scene rail (holomush-5rh.24)"`

---

### Task 15: E2E — telnet-free membership through the GUI

**Files:**

- Create: `web/e2e/scenes-membership.spec.ts`

Drive invite + kick across **two** characters via the GUI, asserting the roster reflects each change. Prefix `wmb` (≤4 chars, alphanumeric; verified free — taken set includes `slc`/`sld`/`wlc`/etc., not `wmb`). Mirror `web/e2e/scenes-lifecycle.spec.ts` for the create+select scaffold and `web/e2e/helpers/db.ts` for state assertions.

- [ ] **Step 1: Write the E2E**

```typescript
import { test, expect } from '@playwright/test';
import { registerAndEnterTerminal } from './helpers/fixtures';
import { db } from './helpers/db';

test('owner invites and kicks a character from the web GUI with no telnet', async ({ page, browser }) => {
	// Register the invitee first (separate context) so a second character exists in the directory.
	const ctx2 = await browser.newContext();
	const page2 = await ctx2.newPage();
	const invitee = await registerAndEnterTerminal(page2, 'wmb');

	// Owner registers + creates a scene.
	const owner = await registerAndEnterTerminal(page, 'wmo');
	await page.goto('/scenes');
	await page.getByRole('button', { name: /create scene/i }).click();
	await page.fill('input[name="title"]', 'Web Membership Test');
	await page.getByRole('button', { name: /^create$/i }).click();
	await expect(page.getByText('Web Membership Test')).toBeVisible({ timeout: 10000 });
	const scene = await db.getSceneByTitle('Web Membership Test');

	// Invite the second character via the picker.
	await page.getByRole('button', { name: /invite characters/i }).click();
	await page.getByPlaceholder(/search characters/i).fill(invitee.charName);
	await page.getByRole('option', { name: invitee.charName }).click();
	await page.getByRole('button', { name: /^invite 1$/i }).click();

	// Roster reflects the invite; DB shows the participant row.
	const inviteeChar = await db.getCharacterByName(invitee.charName);
	await expect.poll(async () =>
		(await db.getParticipantsBySceneId(scene!.id)).map((p) => p.characterId),
	).toContain(inviteeChar!.id);

	// Kick via the per-member kebab.
	await page.getByLabel(new RegExp(`Manage ${invitee.charName}`)).click();
	await page.getByRole('menuitem', { name: /^kick$/i }).click();
	await expect.poll(async () =>
		(await db.getParticipantsBySceneId(scene!.id)).map((p) => p.characterId),
	).not.toContain(inviteeChar!.id);

	await ctx2.close();
});
```

`db.getParticipantsBySceneId` does not exist yet (Explore confirmed db.ts has `getSceneById`/`getSceneByTitle`/`getCharacterByName` but no participant query). **Add it** to `web/e2e/helpers/db.ts`, mirroring `getSceneById` — a `SELECT character_id, role FROM scene_participants WHERE scene_id = $1`. Confirm the table/column names against `internal/store/migrations/` before writing the query.

- [ ] **Step 2: Run the E2E (Docker)**

Run: `task test:e2e -- scenes-membership.spec.ts`
Expected: PASS.

- [ ] **Step 3: Commit**

`jj commit -m "test(web): E2E telnet-free scene membership via GUI (holomush-5rh.24)"`

---

## Final verification (before PR)

- [ ] `task pr-prep` (fast lane) — green.
- [ ] `task test:int` — facade + directory changes exercised (Phase 1 repo test needs Docker).
- [ ] `task test:e2e -- scenes-membership.spec.ts` — telnet-free membership path green.
- [ ] Registry: confirm INV-SCENE-65 is **still `binding: pending`** — this slice gates 7 of 8 handlers; `UpdateScene` (Settings slice) remains. Do **not** flip to `bound`; that is `holomush-5rh.24.9`'s job once all 8 gate. `task test -- -run 'TestEveryRegistryInvariantHasBinding' ./test/meta/` must pass with the entry pending.
- [ ] Registry: confirm **INV-ACCESS-9** flips `pending → bound` once the `// Verifies: INV-ACCESS-9` deny test lands (Task 3) — `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/`.
- [ ] Directory authz ADR captured (`holomush-kn78o` — ABAC-gated via `list_character_directory`, default-permit any authenticated character incl. guest; names-only; connection state separately gated).
- [ ] `bd note holomush-5rh.25 "Phase-1 character directory (ListAllCharacters) ships an MVP id↔name listing under 5rh.24; may be superseded/extended by scene-character-name-resolution"`.
- [ ] Review gates: `code-reviewer` (mandatory) + `abac-reviewer` (**mandatory** — touches the `invite` Cedar relaxation, the four membership self-gates, AND the new `list_character_directory` action + `character_directory` resource + permit-all-authenticated seed enumeration surface / INV-ACCESS-9).
<!-- adr-capture: sha256=58784a9420f824fc; session=cli; ts=2026-06-27T01:03:16Z; adrs=holomush-kn78o -->
