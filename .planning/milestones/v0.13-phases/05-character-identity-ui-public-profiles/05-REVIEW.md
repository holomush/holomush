---
phase: 05-character-identity-ui-public-profiles
reviewed: 2026-08-12T00:00:00Z
depth: standard
files_reviewed: 78
files_reviewed_list:
  - api/proto/holomush/characteraccess/v1/characteraccess.proto
  - api/proto/holomush/core/v1/core.proto
  - api/proto/holomush/web/v1/web.proto
  - cmd/holomush/deps.go
  - cmd/holomush/deps_test.go
  - cmd/holomush/gateway.go
  - cmd/holomush/sub_grpc.go
  - docs/architecture/invariants.md
  - docs/architecture/invariants.yaml
  - internal/access/profilevis/profilevis_test.go
  - internal/auth/player.go
  - internal/auth/postgres/player_repo.go
  - internal/auth/postgres/player_repo_test.go
  - internal/auth/reset_service_logging_test.go
  - internal/bootstrap/admin_test.go
  - internal/grpc/auth_errors.go
  - internal/grpc/auth_handlers.go
  - internal/grpc/characteraccess_create.go
  - internal/grpc/characteraccess_create_test.go
  - internal/grpc/characteraccess_directory_test.go
  - internal/grpc/characteraccess_owner.go
  - internal/grpc/characteraccess_owner_test.go
  - internal/grpc/characteraccess_profile_test.go
  - internal/grpc/characteraccess_service.go
  - internal/grpc/characteraccess_viewer_test.go
  - internal/grpc/characteraccess_write.go
  - internal/grpc/characteraccess_write_test.go
  - internal/grpcclient/client.go
  - internal/testsupport/integrationtest/harness.go
  - internal/web/auth_handlers.go
  - internal/web/auth_handlers_logging_test.go
  - internal/web/auth_handlers_test.go
  - internal/web/character_handlers.go
  - internal/web/character_handlers_test.go
  - internal/web/handler.go
  - internal/web/status_interceptor_test.go
  - test/integration/access/access_suite_test.go
  - test/integration/access/character_directory_test.go
  - test/integration/access/character_profile_read_test.go
  - test/integration/access/character_readtime_floor_test.go
  - test/integration/access/character_write_test.go
  - test/integration/access/media_schema_test.go
  - test/meta/character_rpc_census_test.go
  - test/meta/characteraccess_routing_census_test.go
  - web/e2e/admin.spec.ts
  - web/e2e/auth.spec.ts
  - web/e2e/character-switcher.spec.ts
  - web/e2e/characters-create.spec.ts
  - web/e2e/characters-roster.spec.ts
  - web/e2e/helpers/fixtures.ts
  - web/e2e/negative-journeys.spec.ts
  - web/e2e/public-profile.spec.ts
  - web/e2e/session-security.spec.ts
  - web/src/lib/characters/client.test.ts
  - web/src/lib/characters/client.ts
  - web/src/lib/characters/createFlow.test.ts
  - web/src/lib/characters/createFlow.ts
  - web/src/lib/characters/createdNotice.ts
  - web/src/lib/components/characters/ByteCounter.svelte
  - web/src/lib/components/characters/ByteCounter.svelte.test.ts
  - web/src/lib/components/characters/CharacterPortrait.svelte
  - web/src/lib/components/characters/CharacterRoster.svelte
  - web/src/lib/components/characters/CharacterRoster.svelte.test.ts
  - web/src/lib/components/characters/CreateCharacterForm.svelte
  - web/src/lib/components/characters/CreateCharacterForm.svelte.test.ts
  - web/src/lib/components/characters/ProfileMedia.svelte
  - web/src/lib/components/characters/ProfileMedia.svelte.test.ts
  - web/src/lib/components/characters/ProfileSection.svelte
  - web/src/lib/components/characters/ProfileSection.svelte.test.ts
  - web/src/lib/components/characters/PublicProfile.svelte
  - web/src/lib/components/characters/PublicProfile.svelte.test.ts
  - web/src/lib/components/characters/RosterCard.svelte
  - web/src/lib/components/characters/RosterCard.svelte.test.ts
  - web/src/lib/connect/errors.test.ts
  - web/src/lib/connect/errors.ts
  - web/src/routes/(authed)/+layout.ts
  - web/src/routes/(authed)/characters/+page.svelte
  - web/src/routes/(authed)/characters/[id]/+page.svelte
  - web/src/routes/(authed)/characters/new/+page.svelte
  - web/src/routes/c/[id]/+page.svelte
findings:
  critical: 1
  warning: 8
  info: 0
  total: 9
status: issues_found
---

# Phase 05: Code Review Report

**Reviewed:** 2026-08-12
**Depth:** standard
**Files Reviewed:** 78 (generated `pkg/proto/**`, `web/src/lib/connect/holomush/**`, `internal/auth/mocks/**` excluded per scope)
**Status:** issues_found

## Summary

The server half of this phase is unusually solid against the repo's own hard rules. I
specifically probed and could **not** break the following, and want that on record so the
findings below are read as the residue rather than as a general verdict:

- **gRPC opacity.** No `status.Errorf(codes.X, "...%v", err)` anywhere in
  `internal/grpc/characteraccess_*.go` or `internal/web/character_handlers.go`; every wire
  message is an authored constant, and `TestCreateCharacterNeverReturnsAnInternalCodeStringOnTheWire`
  asserts the negative across the whole mapping table.
- **Deepest-code hazard (#4902).** `classifyCharacterCreateError`
  (`internal/grpc/characteraccess_create.go:264`) keys on `oops.AsOops(err).Code()`. Its
  justification checks out against the real producers: every `CHARACTER_NAME_TAKEN`,
  `CHARACTER_LIMIT_REACHED` and remapped gate verdict in
  `internal/auth/character_service.go` is built with `Errorf` (unwrapped), and the two
  `.Wrap`-ing arms degrade to the identical `(Internal, msgCharacterCreateFailed)` pair the
  default arm returns — which the shadowing row at `characteraccess_create_test.go:489`
  pins. `mapProfileError` correctly keys on typed sentinels instead.
- **Gateway boundary.** `internal/web/character_handlers.go` is pure proxy — no repo, no DB,
  no computation, and every structural write is a typed facade RPC (no `sendCommand`).
- **Byte-vs-rune discipline.** `ByteCounter` uses `TextEncoder`, the name counter uses
  `[...name].length`, and `SHORT`/`LONG` match `world.MaxNameLength`/`MaxDescriptionLength`.
- **INV-ACCESS-10 binding.** `test/integration/access/character_readtime_floor_test.go:254`
  and `internal/access/profilevis/profilevis_test.go:114,345` genuinely assert all three
  clauses; the `binding: bound` flip is honest, not a false-green.

What survived the probe is concentrated on the **client** side: one shipped feature with no
route into it, one UTF-16 unit bug of exactly the class this phase already fixed twice, and
one remaining server-string-to-player leak.

---

## Critical Issues

### CR-01: The `/characters/[id]` authoring surface has no navigational entry point

**File:** `web/src/lib/components/characters/RosterCard.svelte:98-121`, `web/src/routes/(authed)/characters/+page.svelte:174`

**Issue:** The five-section authoring surface at `/characters/[id]` — the surface that owns
12 of the 13 editable profile fields, the in-world description, the version cell and the
PROFILE-12 not-retroactive notice — is unreachable from the running app in the ordinary case.

Exhaustive grep of every `href=` / `goto()` in `web/src` that names a character route yields
exactly three producers:

| Target | Producer | Condition |
|---|---|---|
| `/characters/new` | `CharacterRoster.svelte:112` | always |
| `/c/{id}` (public) | `RosterCard.svelte:119` | **only when `!playable`** (retired/idle) |
| `/characters/{id}` | `characters/+page.svelte:174` | **only when `createdNotice.profileIncomplete`** |

`RosterCard` renders `Make default` (playable && !isDefault) and the public-profile link
(`!playable`) — no owner-edit link at any lifecycle state — and the whole playable card's
click target is `select()` (`RosterCard.svelte:124-144`), which enters the terminal. So a
player whose create succeeded cleanly (the common path — the second transaction only fails
on an outage) has no way to open their own character's edit page. The only link is the
one-shot repair link, which is gated on a *partial-write failure* and cleared by
`takeCreatedNotice()` after one render.

This is not a client-only miss: `05-UI-SPEC.md`'s copywriting table (lines 379-387) enumerates
`Make default` and `View profile →` as the roster card's only controls and never contracts an
`Edit` affordance, even though line 213 explicitly *permits* one ("The owner-only `Edit` /
`View public profile` affordance … Leaks nothing. Permitted."). So the fix may need a SPEC
amendment as well as code.

**Fix:** add an owner-edit affordance to the playable card (and keep it on the not-playable
card, where a retired character's fields are still editable), stopping propagation so it does
not also select the character:

```svelte
<!-- RosterCard.svelte, inside {#snippet body()} -->
<a
  class="view-profile"
  data-testid="edit-character"
  href="/characters/{id}"
  onclick={(e: MouseEvent) => e.stopPropagation()}>Edit →</a>
```

and record the new control in `05-UI-SPEC.md`'s copywriting table so the two do not disagree.

---

## Warnings

### WR-01: `CharacterPortrait` indexes the name by UTF-16 code unit, not code point

**File:** `web/src/lib/components/characters/CharacterPortrait.svelte:34`

**Issue:** `{name.charAt(0)}` returns one UTF-16 code unit. `internal/charname/syntax/syntax.go:47`
admits any `\p{L}` rune (`^[\p{L}]+( [\p{L}]+)*$`), and Go's `\p{L}` includes astral-plane
letters (Deseret, Adlam, Osage, Gothic, …). For a name whose first character is astral,
`charAt(0)` yields a lone high surrogate, which renders as U+FFFD in the 80px public-profile
plate and the 44px roster plate.

This directly contradicts the component's own doc block at lines 16-18 ("`name` is rendered
UNMUTATED: the first character goes into the DOM as the stored bytes carried it"), and it is
the same unit-system class the phase already corrected twice elsewhere (`ByteCounter`'s
`TextEncoder`, `CreateCharacterForm`'s `[...name].length`) — this one site was missed.

**Fix:**

```svelte
<!-- Spread-then-index yields the first CODE POINT; charAt yields a UTF-16 code unit,
     which is a lone surrogate for any astral-plane letter \p{L} admits. -->
{[...name][0] ?? ''}
```

### WR-02: The roster renders a server-supplied `error_message` verbatim to the player

**File:** `web/src/routes/(authed)/characters/+page.svelte:138`

**Issue:** `actionError = resp.errorMessage || 'Failed to select character.'` forwards a wire
string straight onto a player-facing region. The producers are
`internal/grpc/auth_handlers.go:251,262,282,307`: `"invalid or expired player session"`,
`"invalid character_id"`, `"character does not belong to this player"`, `"character is not
available for play"`. The third is an ownership disclosure phrased in wire vocabulary, in a
phase whose entire mutation surface collapses ownership causes into one authored literal
(`characterNotOwnedMessage`), and whose sibling handlers in the very same file author their own
copy (`makeDefault`'s `"Couldn't save. Try again."` at :124, `ProfileSection`'s two constants).

`selectCharacter` was rewritten in this phase (the diff replaces the old `error = …` body), so
this is code the phase touched, not untouched legacy.

**Fix:** author the copy client-side, as every sibling path in this file already does:

```ts
} else {
  actionError = 'Failed to select character. Try again.';
}
```

### WR-03: Both `[id]` routes fetch only in `onMount`, so a param change shows stale data

**File:** `web/src/routes/c/[id]/+page.svelte:50-52`, `web/src/routes/(authed)/characters/[id]/+page.svelte:142-144`

**Issue:** `id` is `$derived($page.params.id ?? '')` but `load()` is invoked only from
`onMount`. SvelteKit reuses the same component instance across a client-side navigation
between two params of the same route, so `/c/A` → `/c/B` leaves `character` holding A's
profile while the URL and `id` say B. On the public route that is a wrong-character render;
on `/characters/[id]` it is worse, because `version` and every section's `loaded` snapshot
still belong to A while `saveProfile` sends `characterId: id` (= B) — a save would be
predicated on A's version against B's row.

Today the path is not reachable (no in-app link goes from one `[id]` page to another sibling),
but CR-01's fix and Phase 6's nav shell are both likely to create one, and the failure is
silent.

**Fix:** react to the param instead of mounting once:

```ts
$effect(() => {
  const current = id;          // read to establish the dependency
  void load(current);
});
```

(and thread the captured `current` through `load` so a late response for A cannot overwrite
B's state).

### WR-04: `TestCreateCharacterRendersBothNameTakenProducersIdentically` tests one producer twice

**File:** `internal/grpc/characteraccess_create_test.go:312-319`

**Issue:** The map's two entries — keyed `"the §6.1.1 uniqueness pre-check"` and
`"the 23505 handler on characters_normalized_name_key"` — are byte-identical expressions
(same code, same `With`, same `Errorf` format and args). The test therefore drives the same
error twice and cannot fail for the property it names ("a differential between them would
report which one won a race"). It is a green that carries no information about the two-producer
contract.

The two real producers (`internal/auth/character_service.go:153` and `:222`) do happen to be
identically shaped today, which is exactly what makes the divergence worth pinning — a future
edit that wraps the 23505 path (`.Wrap(pgErr)` instead of `.Errorf`) would shadow the code
under `PG_*` per #4902 and silently reroute it to `Internal`/`msgCharacterCreateFailed`, and
this test would still pass.

**Fix:** make the second fixture the shape the 23505 path could plausibly drift to, so the
subtest can actually fail:

```go
"the 23505 handler on characters_normalized_name_key": oops.Code("CHARACTER_NAME_TAKEN").
    With("name", "Ada Lovelace").
    Wrap(&pgconn.PgError{Code: "23505", ConstraintName: characterNormalizedNameConstraint}),
```

and assert both still render `(codes.AlreadyExists, msgCharacterNameTaken)`.

### WR-05: Census comment count disagrees with the set it documents

**File:** `test/meta/characteraccess_routing_census_test.go:283-284`

**Issue:** `// characterWebProxyRPCs … Five members, one per owner-audience facade RPC
(04-04, 04-06, 05-01)` sits above a map with **six** entries (`WebListMyCharacters`,
`WebGetMyCharacter`, `WebUpdateCharacterProfile`, `WebUpdateCharacterDescription`,
`WebSetDefaultCharacter`, `WebCreateCharacter`). The sibling sets in the same file were both
updated correctly (`characterGuestGateRPCs` says six and has six; `characterOwnershipRPCs`
says four and has four), so this is drift, not a convention. In a repo where these counts are
the human-readable half of a set-equality gate, a wrong count is the thing a future reviewer
reconciles *against*.

**Fix:** `// … Six members, one per owner-audience facade RPC (04-04, 04-06, 05-01, 05-03).`

### WR-06: `sessionStatus` is plumbed three layers deep and never read

**File:** `web/src/routes/(authed)/characters/+page.svelte:25,70,82`, `web/src/lib/components/characters/CharacterRoster.svelte:27`, `web/src/lib/components/characters/RosterCard.svelte:40`

**Issue:** `SessionOverlay.sessionStatus` is declared, populated from `s.sessionStatus`, joined
into every `Row`, forwarded through `CharacterRoster` and accepted by `RosterCard` — where the
badge is derived from `hasActiveSession` alone (`RosterCard.svelte:75`). The field has no
reader anywhere.

This is not ordinary dead code: `char.sessionStatus` forwarded onto a badge is one of the two
bugs this phase explicitly fixed, and leaving the raw server token in scope on the component
that renders the badge is the shortest possible path back to it. Deleting it makes the
regression a compile-time impossibility rather than a review finding.

**Fix:** narrow the overlay to what is read:

```ts
type SessionOverlay = { hasActiveSession: boolean };
```

and drop `sessionStatus` from the `Row`/`session` prop types in both components.

### WR-07: Gallery images have no error fallback while the primary image does

**File:** `web/src/lib/components/characters/ProfileMedia.svelte:115` (cf. `:76`, `:98`)

**Issue:** The primary image carries `onerror={() => (primaryFailed = true)}` and degrades to
`CharacterPortrait`; the gallery tile at `:115` carries none, so a failed fetch renders the
browser's broken-image glyph. The component's own doc (lines 48-50) states the rationale for
the primary's fallback — "rather than to a broken-image glyph that would read as 'this profile
has an image you cannot see'" — and that reasoning applies identically to a tile. `mediaSrc`
points at `/media/<id>`, for which **no serving endpoint exists in v0.13**, so every gallery
tile would break if a row ever appeared.

**Fix:** give the tile the same degradation (a per-row `failed` set keyed by `mediaId`, dropping
the `<li>` when the fetch fails), or state in the doc block why a tile may show the glyph when
the portrait may not.

### WR-08: `WebCreateCharacterResponse` reuses field 1 with a new wire type and reserves nothing

**File:** `api/proto/holomush/web/v1/web.proto:703-716`

**Issue:** Field 1 changed from `bool success` (varint) to
`holomush.characteraccess.v1.OwnCharacter character` (length-delimited), and fields 2/3/4
(`character_id`, `character_name`, `error_message`) were deleted. There is no `reserved 2, 3, 4;`
and no `reserved "character_id", "character_name", "error_message";`. `WebCreateCharacterRequest`
has the same gap for its retired field 1 and for the `character_name` → `name` rename at field 2.

A clean break is defensible for an undeployed codebase, and I confirmed every Go and TS caller
of the dropped fields is gone (`rg` over non-generated sources finds only the new shape). The
residual hazard is forward-looking: nothing stops a future field from taking number 2, 3 or 4
with a different meaning, at which point a stale client silently mis-decodes rather than failing.

**Fix:**

```proto
message WebCreateCharacterResponse {
  reserved 2, 3, 4;
  reserved "success_unused", "character_id", "character_name", "error_message";
  holomush.characteraccess.v1.OwnCharacter character = 1;
}
```

(and the analogous `reserved 1;` on `WebCreateCharacterRequest`, whose comment already asserts
field 1 is "retired" without declaring it so).

---

_Reviewed: 2026-08-12_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
