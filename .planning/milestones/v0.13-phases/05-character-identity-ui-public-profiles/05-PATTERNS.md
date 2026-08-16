# Phase 5: Character Identity UI & Public Profiles - Pattern Map

**Mapped:** 2026-08-12
**Files analyzed:** 24 new/modified files
**Analogs found:** 23 / 24

> **Provenance.** `05-RESEARCH.md` already grounds the Go half with verified
> `path:line` citations (Patterns 1–5, Code Examples, Pitfalls 1–13). Those are
> **carried forward here, not re-derived**. This document's own contribution is
> the frontend mapping — the four routes, the `web/src/lib/characters/` flow
> layer, the component tests and the E2E spec — which RESEARCH named but did not
> assign analogs to.

---

## File Classification

### Server / proto

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `api/proto/holomush/characteraccess/v1/characteraccess.proto` (+2 rpc, +4 msg) | proto/API | request-response | the six shipped RPCs in the same file (`UpdateCharacterProfile*`, `ListMyCharacters*`) | exact |
| `api/proto/holomush/web/v1/web.proto` (+2 proxy rpc, reshape `WebCreateCharacter*`) | proto/API | request-response | `WebGetMyCharacter*` / `WebUpdateCharacterProfile*` in the same file | exact |
| `internal/grpc/characteraccess_write.go` — `+CreateCharacter` | controller (facade) | request-response (create) | `UpdateCharacterProfile` (`characteraccess_write.go:257`) for the gate order; `internal/auth/character_service.go:100-232` for the create pipeline | role-match (no create analog on this facade) |
| `internal/grpc/characteraccess_write.go` — `+SetDefaultCharacter` | controller (facade) | request-response (CRUD write) | `UpdateCharacterDescription` / `UpdateCharacterProfile` gate prologue | exact |
| `internal/grpc/characteraccess_service.go` (constructor +1–2 deps) | config/wiring | — | the shipped 8-positional-arg `NewCharacterAccessServer` (`:211-213`) | exact |
| `internal/web/character_handlers.go` (+2 proxies) | middleware/gateway proxy | request-response | `WebGetMyCharacter` (`character_handlers.go:99-122`) | exact |
| `internal/auth/postgres/player_repo.go` (+`UpdateDefaultCharacter`) | model/repository | CRUD (narrow column update) | `(*PlayerRepository).UpdatePassword` (`:230-246`) — **not** the full-row `Update` (`:188-214`) | exact |
| `internal/auth/player.go` (+interface method) | model (interface) | — | `UpdatePassword` / `UpdatePasswordAndClearLockout` decls (`player.go:195-200`) | exact |
| `cmd/holomush/sub_grpc.go:861` + 7 test call sites | config/wiring | — | current call sites (Pitfall 12) | exact |

### Tests (Go)

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/grpc/characteraccess_write_test.go` (+`CreateCharacter`, `SetDefaultCharacter`) | test (unit) | request-response | the shipped fixtures in the same file (`:196` constructor site) | exact |
| `internal/grpc/characteraccess_owner_test.go` (constructor churn) | test (unit) | — | itself (`:137`) | exact |
| `test/meta/character_rpc_census_test.go` (+3 rows, 1 MOVE) | test (meta) | set-equality gate | `characterReadSurfaceInventory()` / `characterNameReachableRPCs()` in-file | exact |
| `test/meta/characteraccess_routing_census_test.go` (+5 rows) | test (meta) | set-equality gate | `characterGuestGateRPCs()` / `characterOwnershipRPCs()` / `characterWebProxyRPCs()` in-file | exact |
| **NEW #1** criterion-4 read-time spec (`test/integration/access/`) | test (integration) | request-response + corpus mutation | `test/integration/access/character_profile_read_test.go` (`profileCorpusStore`, `newCorpusEngine`, `newServer`, `insertProperty`) | exact |
| **NEW #2** `test/integration/access/media_schema_test.go` (EXT-05) | test (integration) | request-response | same file's fixture + `insertProperty` (`:197-203`) | exact |

### Frontend — routes

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `web/src/routes/c/[id]/+page.svelte` **(NEW, outside `(authed)`)** | component (route page) | request-response, anonymous read | **shape:** `web/src/routes/(authed)/scenes/[id]/+page.svelte:5-46` (dynamic `[id]` param + `onMount` fetch + `loading`/`fetchError` state); **auth posture:** `web/src/routes/register/+page.svelte` / `login/+page.ts` (root-level, no `(authed)` guard) | composite — no single file is both |
| `web/src/routes/c/[id]/+page.ts` *(optional; only if a `load()` is wanted)* | route loader | request-response | `web/src/routes/login/+page.ts` (`export const ssr = false` + a `try/catch` that returns state instead of throwing) | role-match |
| `web/src/routes/(authed)/characters/+page.svelte` (rewritten) | component (route page) | CRUD list | itself, `:97-197` (Card grid, initial-letter avatar, badge, dashed create card) — the inline-create branch `:142-193` is **deleted** (D-87) | exact |
| `web/src/routes/(authed)/characters/new/+page.svelte` **(NEW)** | component (form route) | request-response (create) | `web/src/routes/register/+page.svelte:5-90` — the shipped multi-field form idiom: `$state` per field, a `validate()` returning a string, `busy` flag, `error` string, `goto()` on success | exact |
| `web/src/routes/(authed)/characters/[id]/+page.svelte` **(NEW)** | component (form route) | CRUD (per-section save) | `register/+page.svelte` for field/submit/error shape; `scenes/[id]/+page.svelte` for the `[id]` param read | role-match |

### Frontend — lib, components, tests

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `web/src/lib/characters/client.ts` **(NEW)** | service (typed RPC wrappers) | request-response | `web/src/lib/scenes/client.ts:26-70` — one module-level `createClient(WebService, transport)` singleton + one thin exported async fn per RPC with a doc comment | exact |
| `web/src/lib/characters/createFlow.ts` **(NEW)** | service (flow) | request-response | `web/src/lib/scenes/createFlow.ts:22-38` — create RPC authoritative, post-create refresh best-effort | exact |
| `web/src/lib/characters/*.test.ts` **(NEW)** | test (vitest, `server` project) | — | `web/src/lib/scenes/createFlow.test.ts` (sibling of the flow it tests) | exact |
| `web/src/lib/components/characters/*.svelte` (identity card, if shared per CONTEXT discretion) | component (presentational) | — | `web/src/lib/components/scenes/PoseCard.svelte` | role-match |
| `web/src/lib/components/characters/*.svelte.test.ts` (UI-SPEC backstop rows) | test (vitest, `client` project) | — | `web/src/lib/components/scenes/PoseCard.svelte.test.ts:13-22` (`mount`/`unmount`, no testing-library) | exact |
| `web/e2e/public-profile.spec.ts` **(NEW)** | test (E2E) | — | `web/e2e/landing.spec.ts:1-45` (the only anonymous-visit spec) + `web/e2e/helpers/fixtures.ts` `registerAndEnterTerminal` and `helpers/db.ts` `getCharacterByName` / `getCharactersByPlayerId` | composite |
| `web/e2e/characters-create.spec.ts` **(NEW)** | test (E2E) | — | `web/e2e/character-switcher.spec.ts`, `auth.spec.ts` | exact |

---

## Pattern Assignments

### `internal/web/character_handlers.go` — the two new proxies (gateway, request-response)

**Analog:** `WebGetMyCharacter`, same file, `:99-122`. Carried from RESEARCH Pattern 1 — copy verbatim, swap the RPC.

```go
func (h *Handler) WebGetMyCharacter(ctx context.Context, req *connect.Request[webv1.WebGetMyCharacterRequest]) (*connect.Response[webv1.WebGetMyCharacterResponse], error) {
	slog.DebugContext(ctx, "web: WebGetMyCharacter", "character_id", req.Msg.GetCharacterId())
	if h.characterAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("character access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.characterAccess.GetMyCharacter(rpcCtx, &characteraccessv1.GetMyCharacterRequest{
		CharacterId:        req.Msg.GetCharacterId(),
		PlayerSessionToken: token,
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: get my character RPC failed", err, "character_id", req.Msg.GetCharacterId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebGetMyCharacterResponse{
		Character: resp.GetCharacter(),
	}), nil
}
```

**Both census conjuncts are load-bearing:** the body must reference the bare
identifier `headerInjectSessionToken` **and** name its paired facade method
(`test/meta/characteraccess_routing_census_test.go:582-600`).

---

### `internal/grpc/characteraccess_write.go` — `SetDefaultCharacter` (facade controller, CRUD write)

**Analog:** `UpdateCharacterProfile`, same file, `:257-266` — the gate prologue.

```go
func (s *CharacterAccessServer) UpdateCharacterProfile(ctx context.Context, req *characteraccessv1.UpdateCharacterProfileRequest) (*characteraccessv1.UpdateCharacterProfileResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacterForMutation(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	if versionErr := requireGuardedVersion(ctx, req.GetExpectedVersion(), char.ID); versionErr != nil {
		return nil, versionErr
	}
```

**Copy the first two moves; STOP before `requireGuardedVersion`.** D-89:
`SetDefaultCharacter` targets a `players` row, so §9.4 does not reach it. Say
so in a comment so a reviewer does not "fix" the omission (Pitfall 5).
`CreateCharacter` names no existing character id, so it copies **only**
`resolveAndGate` — and correspondingly is **not** a `characterOwnershipRPCs()`
census member (Pitfall 2).

**Mask-path rejection pattern** (same file, `:270-286`) — the closed-allowlist
map lookup that returns `codes.InvalidArgument` with a static literal message
and logs the structured reason via `errutil.LogErrorContext` — is the model for
any new value-validation branch.

---

### `internal/auth/postgres/player_repo.go` — `UpdateDefaultCharacter` (repository, CRUD)

**Analog:** `UpdatePassword`, same file, `:230-246`. This is the *narrow* precedent; the full-row `Update` at `:188-214` is the trap (Pitfall 3).

```go
func (r *PlayerRepository) UpdatePassword(ctx context.Context, id ulid.ULID, passwordHash string) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE players SET password_hash = $2, updated_at = $3
		WHERE id = $1
	`, id.String(), passwordHash, pgnanos.From(time.Now()))
	if err != nil {
		return oops.Code("PLAYER_UPDATE_PASSWORD_FAILED").
			With("operation", "update password").
			With("id", id.String()).
			Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.Code("PLAYER_NOT_FOUND").
			With("id", id.String()).
			Wrap(auth.ErrNotFound)
	}
	// …
}
```

Copy exactly: `pool.Exec` with a two/three-param `UPDATE … WHERE id = $1`,
`pgnanos.From(time.Now())` for `updated_at`, an `oops.Code(...)` per failure
class, and the `RowsAffected() == 0` → `PLAYER_NOT_FOUND` leg.

**Interface declaration** goes beside its siblings — `internal/auth/player.go:195-200`:

```go
	// UpdatePassword updates only the password hash for a player.
	UpdatePassword(ctx context.Context, id ulid.ULID, passwordHash string) error

	// UpdatePasswordAndClearLockout atomically updates the password hash and
	// clears lockout state (failed_attempts = 0, locked_until = NULL).
	UpdatePasswordAndClearLockout(ctx context.Context, id ulid.ULID, passwordHash string) error
```

---

### `web/src/routes/c/[id]/+page.svelte` (route component, anonymous request-response) — **the one composite**

No single existing file is both *dynamic-param* and *outside `(authed)`*. Take two halves:

**Half A — dynamic param + fetch lifecycle.** `web/src/routes/(authed)/scenes/[id]/+page.svelte:5-46`:

```svelte
<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';

  // Route param — could be a scene ID or a published-scene ID …
  let id = $derived($page.params.id);

  let loading = $state(true);
  let fetchError = $state('');

  onMount(async () => {
    await load();
  });

  async function load() {
    loading = true;
    fetchError = '';
    const resolvedId: string = id ?? '';
    if (!resolvedId) {
      fetchError = 'Invalid scene ID.';
      loading = false;
      return;
    }
    …
  }
</script>
```

**Half B — the anonymous route posture.** There is **nothing to copy** and that
is the finding: `/c/[id]` is a sibling of `login`/`register`/`reset`, and the
root loader already supplies the whole posture —
`web/src/routes/+layout.ts` is two lines:

```ts
export const ssr = false;
export const prerender = false;
```

`adapter-static` + `fallback: 'index.html'` means every path is already HTTP 200
+ the SPA shell, so route-level indistinguishability (§8.7) is **structural**;
no `+page.ts` is required and **no `(authed)` guard is inherited**. The
`(authed)/+layout.ts` `redirect(302, '/login')` leg is precisely what must NOT
be reachable from this route — D-84.

**If a `+page.ts` is added anyway,** the analog is `web/src/routes/login/+page.ts`
— the shape that returns state rather than throwing:

```ts
export const ssr = false;

export const load: PageLoad = async () => {
  if (typeof window === 'undefined') return { authenticated: false };
  const client = createClient(WebService, transport);
  try {
    const resp = await client.webCheckSession({});
    …
    return { authenticated: true, … };
  } catch (e) {
    if (isStaleSession(e)) { clearAuth(); }
    return { authenticated: false };
  }
};
```

`/c/[id]` sends **no** session handling of its own: the gateway forwards an
empty `player_session_token` when the header is absent, and the facade degrades
that to the anonymous rung (`characteraccess_service.go:283-296`). The same
`webGetCharacterProfile` call works with or without a cookie.

**Error branch (binding, UI-SPEC):** exactly one condition — gRPC `NotFound` →
the inline not-found render; everything else → the generic render. Do **not**
add a second branch.

---

### `web/src/routes/(authed)/characters/new/+page.svelte` (form route, request-response)

**Analog:** `web/src/routes/register/+page.svelte:5-90` — the shipped multi-field-form idiom.

```svelte
<script lang="ts">
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { goto } from '$app/navigation';
  import { Button } from '$lib/components/ui/button';
  import * as Card from '$lib/components/ui/card';
  import { Input } from '$lib/components/ui/input';
  import { Label } from '$lib/components/ui/label';

  const client = createClient(WebService, transport);

  let username = $state('');
  let email = $state('');
  let error = $state('');
  let busy = $state(false);

  function validate(): string {
    if (!username) return 'Username is required.';
    …
    return '';
  }

  async function handleRegister() {
    const validationError = validate();
    if (validationError) { error = validationError; return; }
    error = '';
    busy = true;
    try {
      const resp = await client.webCreatePlayer({ username, password, email });
      if (resp.success) { goto('/characters'); }
      else { error = resp.errorMessage || 'Registration failed.'; }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Registration failed.';
    } finally {
      busy = false;
    }
  }
</script>
```

Copy: one `$state` per field, `validate()` returning a string, `busy` flag
around the call, `error` string, `goto()` on success, `finally { busy = false }`.

**Two deliberate divergences from this analog:**
1. `busy` drives `disabled` + `aria-busy="true"` on the button with the label
   **unchanged** (UI-SPEC Loading states) — the analog has no such rule.
2. On failure the analog leaves fields intact by construction (they are `$state`
   and nothing clears them) — **preserve that**; UI-SPEC's `partial | /characters/new`
   row makes it a binding requirement, not an accident.
3. The new RPC returns `OwnCharacter`, not a `success`/`errorMessage` pair — the
   error path is a **thrown ConnectError** whose `Code`/message drives the
   UI-SPEC error table. `web/src/lib/connect/errors.ts` is the existing home for
   that classification; extend it rather than string-matching in the component.

---

### `web/src/lib/characters/client.ts` (service, request-response)

**Analog:** `web/src/lib/scenes/client.ts:26-70`.

```ts
import { createClient } from '@connectrpc/connect';
import { WebService, type WebCreateSceneRequest } from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';

/** Singleton Connect client for the web client scenes layer. */
export const client = createClient(WebService, transport);

/**
 * Fetches full scene metadata for one scene on behalf of the given character.
 * …
 */
export async function getScene(sessionId: string, characterId: string, sceneId: string) {
	const res = await client.webGetScene({ sessionId, characterId, sceneId });
	return res.scene;
}
```

One module-level singleton, one thin exported async fn per RPC, each with a doc
comment naming what it returns. The shipped `/characters` page instead calls
`createClient` inline (`characters/+page.svelte:19`) — **do not copy that**;
the rewrite moves to this layer.

---

### `web/src/lib/characters/createFlow.ts` (service, flow)

**Analog:** `web/src/lib/scenes/createFlow.ts:22-38` — carried from RESEARCH Pattern 4.

```ts
	const sceneId = scene?.id ?? '';
	// refresh/select are best-effort UI updates: the scene is already created and
	// authoritative, so a post-create failure here MUST NOT propagate as a create
	// failure — that would show "Create failed" to the user and risk a duplicate
	// scene on retry. Swallow and warn; the workspace reconciles on next interaction.
	try {
		await workspaceStore.refresh(opts.characters);
		if (sceneId) { await workspaceStore.select(sceneId, '', opts.characterId); }
	} catch (e) {
		console.warn('[submitCreateScene] post-create refresh/select failed', e);
	}
	return sceneId;
```

Echo the **server-returned** `OwnCharacter.name` (D-88) from the create
response, never a locally-held input string; a post-create roster refresh
failure never surfaces as "create failed".

---

### `web/src/routes/(authed)/characters/+page.svelte` (route component, CRUD list) — rewrite

**Analog: itself.** Preserve `:116-140` (the grid, the 44px initial-letter
avatar, the badge slot); delete `:142-193` (the inline-create branch → a link
to `/characters/new`, D-87).

```svelte
      <div class="grid grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-3">
        {#each characters as char (char.characterId)}
          <Card.Root
            class="cursor-pointer hover:border-primary transition-colors"
            onclick={() => selectCharacter(char.characterId)}
          >
            <Card.Content class="flex items-start gap-3 px-4 py-4">
              <div class="w-11 h-11 bg-primary text-primary-foreground rounded-md flex items-center justify-center text-xl font-bold shrink-0">
                {char.characterName.charAt(0).toUpperCase()}
              </div>
              <div class="flex flex-col gap-0.5 min-w-0">
                <span class="text-sm font-semibold" data-testid="char-name">{char.characterName}</span>
                …
                {#if char.hasActiveSession}
                  <Badge class="text-[10px] w-fit mt-0.5">Active</Badge>
                {:else}
                  <Badge variant="outline" class="text-[10px] w-fit mt-0.5">{char.sessionStatus || 'Offline'}</Badge>
                {/if}
```

Three UI-SPEC-mandated deltas against this analog:
- `bg-primary` solid plate → the 16% `color-mix` tint (UI-SPEC, portrait unified).
- The session badge is **suppressed entirely** on any non-`active` lifecycle
  (the `{#if}`/`{:else}` above becomes a three-way template branch).
- `char.characterName`/`char.characterId` (`CharacterSummary`) become
  `OwnCharacter.name`/`.id`; the session fields survive only if Q2 lands on
  two calls.

---

### `web/src/lib/components/characters/*.svelte.test.ts` (test, component)

**Analog:** `web/src/lib/components/scenes/PoseCard.svelte.test.ts:13-22` — RESEARCH Pattern 5.

```ts
function render(e: LogEntry): { text: string; html: string } {
  const target = document.createElement('div');
  document.body.appendChild(target);
  const component = mount(PoseCard, { target, props: { entry: e } });
  const text = (target.textContent ?? '').replace(/\s+/g, ' ').trim();
  const html = target.innerHTML;
  unmount(component);
  target.remove();
  return { text, html };
}
```

`@testing-library/svelte` is **not** a dependency. `*.svelte.test.ts` → vitest
`client` project; plain `*.test.ts` → `server` project (`web/vite.config.ts:13-33`).
Both UI-SPEC backstop rows (media renderer, byte counter) use this shape — and
neither is gated by CI (Pitfall 13).

---

### `web/e2e/public-profile.spec.ts` (test, E2E) — the second composite

**Anonymous-visit half:** `web/e2e/landing.spec.ts:1-12`.

```ts
import { test, expect, db } from './helpers/fixtures';

test.describe('Landing Page — Content', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
  });
```

**Setup half:** `web/e2e/helpers/fixtures.ts` `registerAndEnterTerminal` (:91),
plus `web/e2e/helpers/db.ts` `getCharacterByName` (:85) /
`getCharactersByPlayerId` (:93) to recover the character **id** for the `/c/<id>`
URL. No new helper is needed. The genuinely new combination — create while
logged in, then read from a **fresh browser context with no cookie** — has no
in-tree precedent; use Playwright's `browser.newContext()`.

---

## Shared Patterns

### Error handling at the gRPC boundary
**Source:** `internal/grpc/characteraccess_write.go:277-283` + `characteraccess_service.go:143`
**Apply to:** both new facade handlers.
Log the structured reason with `errutil.LogErrorContext` + an `oops.Code(...)`
carrying `With(...)` context; return `status.Error(codes.X, <package-level const literal>)`.
Never interpolate the inner error. `characterProfileNotFoundMessage` is a single
shared literal precisely so all four not-found legs are byte-identical (Pitfall 11) —
a new leg with its own message breaks §8.7 silently.

### Repository error codes
**Source:** `internal/auth/postgres/player_repo.go:236-244`
**Apply to:** `UpdateDefaultCharacter`.
`oops.Code("<NOUN>_<VERB>_FAILED").With("operation", …).With("id", …).Wrap(err)`,
plus a `RowsAffected() == 0` → `PLAYER_NOT_FOUND` leg wrapping `auth.ErrNotFound`.

### SPDX headers
**Source:** every file above.
`.go`/`.proto`: `// SPDX-License-Identifier: Apache-2.0` + copyright.
`.svelte`: the HTML-comment form at `characters/+page.svelte:1-4`.
`.ts`: the `//` form at `scenes/client.ts:1-2`. Applied by `task fmt`.

### Connect client construction (web)
**Source:** `web/src/lib/scenes/client.ts:29`
**Apply to:** `web/src/lib/characters/client.ts`.
One module-level `createClient(WebService, transport)`. The
per-component `createClient` at `characters/+page.svelte:19` and
`register/+page.svelte:27` is the older idiom and is not the target.

### Integration-spec harness
**Source:** `test/integration/access/character_profile_read_test.go` —
`profileCorpusStore` (:66-93), `newCorpusEngine` (:112-132), `newServer` (:161-185),
`insertProperty` (:197-203)
**Apply to:** both new integration specs. `newCorpusEngine` must grow a sibling
returning `(engine, cache, corpus)` so criterion 4 can `cache.Reload(ctx)` a
second time — keep both of its existing guards (the differs-in-one-direction
refusal and the `removed` count).

### Census row edits
**Source:** `test/meta/character_rpc_census_test.go` + `characteraccess_routing_census_test.go`
**Apply to:** the proto commit. Both are set-equality gates that go RED the
instant the generated code lands (Pitfall 2) — the rows are part of the same
commit, and `task fmt` must run after touching these aligned literal blocks.

---

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `web/src/routes/c/[id]/` (as a **whole route**) | route | anonymous request-response | No existing route is simultaneously dynamic-param and outside `(authed)`. The two halves are cited above; the composition is net-new. Its inline not-found render likewise has no analog — `+error.svelte` does not exist (#4903, Phase 6). |

Two further near-misses, mapped but flagged:

- **`CreateCharacter` on the facade** — the facade has no create-shaped handler.
  The gate prologue comes from `UpdateCharacterProfile`; the *pipeline* and its
  error inventory come from `internal/auth/character_service.go:100-232` /
  `mapGateError` (`:290-313`), a different layer. Open Question Q1 (one
  transaction or two) is a decision the plan takes, not one an analog supplies.
- **The per-section save surface (`/characters/[id]`)** — five independent
  dirty/save/status/error units on one page. Each *unit* copies the
  `register/+page.svelte` form idiom; the five-way composition is net-new.

---

## Metadata

**Analog search scope:** `internal/grpc/`, `internal/web/`, `internal/auth/`,
`web/src/routes/`, `web/src/lib/`, `web/e2e/`, `test/meta/`,
`test/integration/access/`
**Pattern extraction date:** 2026-08-12
**Citations carried forward from `05-RESEARCH.md`:** Patterns 1–5, Code Examples,
Pitfalls 1–13 — re-verified only where this document quotes them directly.
