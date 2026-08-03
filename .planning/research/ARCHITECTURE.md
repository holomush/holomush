# Architecture Research

**Domain:** Identity + admin surfaces integrated into an existing Go/gRPC + SvelteKit MUSH platform
**Milestone:** v0.13 Web Portal — Identity & Admin Foundations
**Researched:** 2026-07-31
**Confidence:** HIGH — every claim below cites a file:line read during this research. Where the milestone brief was inaccurate, the correction is flagged inline.

---

## Corrections to the research brief

Two premises in the brief are wrong against the tree. Both change the shape of the answer, so they lead.

| Brief said | Reality | Impact |
|---|---|---|
| "`RoleAdmin`/`RoleBuilder`/`RolePlayer` + RoleStore (AddRole/RemoveRole/PlayerHasRole) at `internal/access/role.go`" | `internal/access/role.go:6-13` holds **only** the three role string constants and `SystemRoles()` (`:16-18`). The `RoleStore` interface is at `internal/store/role_store.go:14-23`, backed by the `character_roles` table (`internal/store/migrations/000001_baseline.up.sql:82-87`). Roles are **character-scoped**, not player-scoped; `PlayerHasRole` (`role_store.go:19-22`) is a derived "any character of this player has the role" query added for the admin-socket operator gate. | Admin gating in the web portal must resolve an **acting character**, not just a player session. |
| "NO character MUTATION RPCs (no rename, no set-description, no retire/delete)" | Correct at the **RPC** layer. But at the **domain** layer two of the three already exist and are ABAC-gated: `world.Service.UpdateCharacterDescription` (`internal/world/service.go:799-836`) and `world.Service.DeleteCharacter` (`internal/world/service.go:745-777`). Rename genuinely does not exist (`Character.SetName` validator exists at `internal/world/character.go:74-80` but no service command calls it), and soft-retire does not exist. | The gap is **RPC surface + one or two domain commands**, not a from-scratch mutation subsystem. This materially shrinks the character-mutation phase. |

A third finding reframes question 3 entirely: **the per-field privacy model is already built and shipped.** See §3.

---

## Standard Architecture (as it exists today, with v0.13 additions marked)

```
┌──────────────────────────────────────────────────────────────────────────┐
│  Browser — SvelteKit PWA (web/)                                          │
│  (authed) layout → SectionRail ← nav/sections.ts registry                │
│  routes: /terminal  /scenes  /characters       [NEW: /admin/*]           │
├──────────────────────────────────────────────────────────────────────────┤
│  Gateway — internal/web (PROTOCOL TRANSLATION ONLY)                      │
│  WebService (ConnectRPC). Holds gRPC CLIENTS, never services or a pool.  │
│  Handler{client, contentClient, sceneAccess}  [NEW: charAccess, admin]   │
└───────┬──────────────────────────────────────────────────────────────────┘
        │ gRPC — cookie token forwarded as X-Session-Token header
┌───────▼──────────────────────────────────────────────────────────────────┐
│  Core gRPC services (internal/grpc), registered in cmd/holomush/sub_grpc │
│  ┌────────────┐ ┌──────────────┐ ┌──────────────┐  ┌──────────────────┐ │
│  │CoreService │ │ContentService│ │SceneAccess   │  │[NEW]             │ │
│  │(session/   │ │(read-only)   │ │Service       │  │CharacterAccess   │ │
│  │ command/   │ │              │ │== BFF FACADE │  │Service (facade)  │ │
│  │ stream)    │ │              │ │  PRECEDENT   │  │AdminPortalService│ │
│  └────────────┘ └──────────────┘ └──────┬───────┘  └────────┬─────────┘ │
│      facade duties: resolveAndGate → ownedCharacter → delegate           │
└──────────────────────────────┬──────────────────────┬────────────────────┘
                               │ plugin gRPC          │ in-process Go call
┌──────────────────────────────▼──────┐  ┌────────────▼────────────────────┐
│ core-scenes plugin SceneService     │  │ world.Service (host-owned)      │
│ (self-enforces ABAC per RPC)        │  │ checkAccess() before every op   │
└─────────────────────────────────────┘  └────────────┬────────────────────┘
                                                      │
┌─────────────────────────────────────────────────────▼────────────────────┐
│ ABAC — AccessPolicyEngine (default-deny, Cedar-aligned DSL)              │
│ attribute providers: character / property / player / location / ...      │
│ 49 seed policies (internal/access/policy/seed.go)                        │
├──────────────────────────────────────────────────────────────────────────┤
│ PostgreSQL — characters, character_roles, entity_properties, world_outbox│
└──────────────────────────────────────────────────────────────────────────┘
```

### The precedent path, verified end to end

The scenes portal is the template every v0.13 surface should copy. Traced:

1. **Proto**: `WebCreateScene` / `WebUpdateScene` / `WebEndScene` on `WebService` (`api/proto/holomush/web/v1/web.proto:287`, `:315`, `:292`). The service doc-comment states the doctrine explicitly — "protocol-translation layer ONLY: every game-state operation proxies to the corresponding CoreService gRPC RPC … the gateway computes no business logic" (`web.proto:115-123`).
2. **Gateway handler**: `internal/web/scene_handlers.go:157-182` (`WebCreateScene`). The whole body is: nil-client guard → read `X-Session-Token` header → 1 forwarding call → pass gRPC status errors through unwrapped (`//nolint:wrapcheck // gRPC status errors pass through as-is`, `:178`). No identity resolution, no authorization, no aggregation. `WebUpdateScene` (`:334-364`) additionally forwards an `update_mask` verbatim.
3. **Facade** (the layer the brief's question 1 is really about): `SceneAccessService` (`api/proto/holomush/sceneaccess/v1/sceneaccess.proto:24-171`, 23 RPCs), implemented by `SceneAccessServer` at `internal/grpc/sceneaccess_service.go:42-68`. Its doc-comment names its job: "host-side facade that owns player authentication, server-side identity resolution (INV-SCENE-63), and guest-player rejection (INV-SCENE-64)". Three reusable helpers carry it:
   - `resolveAndGate(ctx, rawToken)` (`:130-148`) — token → `PlayerSession`, then load player and reject guests.
   - `ownedCharacter(ctx, playerID, charIDStr)` (`:109-125`) — parse ULID, `charRepo.ListByPlayer`, membership check. Returns `codes.NotFound` on a non-owned character (denial hidden as absence).
   - `beginDispatch(ctx, verifiedChar, playerID)` (`:152-160`) — stamps `core.Actor{Kind: ActorCharacter}` and opens the plugin dispatch scope.
   Every RPC is the same four lines (see `ListScenesForViewer`, `:163-189`).
4. **Wiring**: `cmd/holomush/sub_grpc.go:772-805` — resolve the plugin service from the registry, construct the facade, attach optional collaborators (`WithSceneDEKAdder` `:795-797`, `WithCharacterNameResolver` `:798`), register. On plugin absence it registers `UnimplementedSceneAccessServiceServer` (`:803`) so every RPC returns `Unimplemented` rather than nil-panicking.
5. **Authorization**: owned by the **innermost** layer — the plugin self-enforces ABAC per RPC. The facade enforces only *identity* (who you are, which characters you own, not-a-guest).

### Component responsibilities (as they will stand after v0.13)

| Component | Owns | Status |
|---|---|---|
| `web/src/lib/nav/sections.ts` | The single nav manifest both the Rail and the command palette read (`:63-67`, `:81-87`) | **MODIFY** |
| `internal/web.Handler` | HTTP↔gRPC framing, cookie↔token translation. Holds `client`, `contentClient`, `sceneAccess` (`internal/web/handler.go:141-152`) | **MODIFY** (2 new client fields + options, mirroring `:161-168`) |
| `internal/grpc.CoreServer` | Session/command/stream/auth. `CreateCharacter` (`internal/grpc/auth_handlers.go:465-507`) is account-lifecycle-shaped: resolves a player session, delegates to `characterService.CreateBound`, **runs no ABAC check** | **UNCHANGED** (see §1 rationale) |
| `internal/grpc.SceneAccessServer` | Scene BFF facade | **UNCHANGED** — used as the template |
| **`internal/grpc.CharacterAccessServer`** | Character profile read/write BFF facade | **NEW** |
| **`internal/grpc.AdminPortalServer`** | Admin-portal BFF facade, one home for all 7 admin sections | **NEW** |
| `internal/world.Service` | ABAC-gated host-entity commands. `checkAccess` (`internal/world/service.go:209-254`) is the chokepoint; it returns `ErrAccessEvaluationFailed` (not deny) on infra failure so callers can distinguish | **MODIFY** (+rename, +retire) |
| `internal/world.EntityProperty` + `entity_properties` | Per-field values **with per-field visibility** | **UNCHANGED — reused as-is** |
| `internal/access/policy/seed.go` | 49 seed policies | **MODIFY** (+3 new seeds) |
| `internal/access/prefix.go` | ABAC subject/resource namespace vocabulary (`:12-33`, `knownPrefixes` `:45-61`) | **MODIFY** (+1 resource prefix) |
| `api/proto/holomush/admin/v1` (`AdminService`) | Break-glass crypto ops over a UNIX socket | **UNCHANGED — must not be touched.** See §2 |

---

## §1 — Character mutation RPCs

### Recommendation

**A new `holomush.characteraccess.v1.CharacterAccessService` facade in `internal/grpc/characteraccess_service.go`, fronted by `WebCharacter*` RPCs on the existing `WebService`.** Do not extend `core.v1`. Do not extend `world.v1`.

**Rejected: `core.v1.CoreService`.** Its character RPCs are account-lifecycle operations that authorize by *player-session ownership alone* — `CreateCharacter` (`internal/grpc/auth_handlers.go:465-507`) resolves a session then calls `characterService.CreateBound` with no `engine.Evaluate` anywhere in the path. Profile mutation is a **world-entity write** and must pass `world.Service.checkAccess` (`internal/world/service.go:209`). Mixing the two authorization models in one service is exactly the confusion the facade split exists to prevent, and `CoreServer` is already a 200+-field struct (`internal/grpc/server.go:145-200`) that v0.12's ARCH decomposition worked to shrink.

**Rejected: `world.v1.WorldService`.** Its own doc-comment scopes it: "read-only world model queries **for binary plugins** … served on an in-process gRPC connection registered in the plugin service registry" (`api/proto/holomush/world/v1/world.proto:12-21`), registered at `internal/plugin/setup/world_conn.go:21`. It takes a bare `subject_id` string supplied by a *trusted in-process plugin* (`world.proto:130`, `:149`) — there is no session token and no ownership proof. Exposing it to the gateway would let the network layer assert any subject identity it liked. This is a trust-boundary violation, not a stylistic preference.

**Rejected: the command path.** Locked decision — `.claude/rules/gateway-boundary.md:40-56`, ADR `holomush-v4qmu`. Rename/description/retire are form-driven structural writes.

### New components

| Component | Path | Notes |
|---|---|---|
| `characteraccess.v1.CharacterAccessService` proto | `api/proto/holomush/characteraccess/v1/characteraccess.proto` | Mirrors `sceneaccess.proto`'s request shape: every request carries `session_id`, `player_session_token`, `character_id` (the *acting* character) |
| `CharacterAccessServer` | `internal/grpc/characteraccess_service.go` | Copies `resolveAndGate`/`ownedCharacter` verbatim from `sceneaccess_service.go:109-148`. **Extract them to a shared helper file first** — two copies of a guest gate is a security-drift hazard |
| `WebCharacter*` handlers | `internal/web/character_handlers.go` | New file, sibling of `scene_handlers.go`. One pure-proxy func per RPC |
| `world.Service.RenameCharacter` | `internal/world/service.go` | New command; uses `Character.SetName` (`internal/world/character.go:74-80`) for validation |
| `world.Service.RetireCharacter` | `internal/world/service.go` | New command; **soft** retire (see below) |

### Modified components

| Component | Change |
|---|---|
| `api/proto/holomush/web/v1/web.proto` | +`WebGetCharacterProfile`, `WebUpdateCharacterProfile`, `WebRetireCharacter` on `WebService` (`:124-361`) |
| `internal/web/handler.go:141-152` | +`charAccess CharacterAccessClient` field; +`WithCharacterAccessClient` option following `:165-168` |
| `cmd/holomush/sub_grpc.go` | +facade construction/registration next to `:805` |
| `internal/world/mutator.go:78-100` | **Hard gate.** `writeCommands` is "the EXPLICIT, closed set of world.Service write commands and the single taxonomy kind each emits … a new Service write command that isn't registered here has no declared kind, so the census fails". Every new command MUST land here, and its kind must mirror `internal/world/outbox/taxonomy.go` |
| `internal/access/policy/seed.go` | +`seed:profile-public-read` (see §3) |

### Key design calls

**Retire = soft, and modelled as `write`, not a new action.** Adding a `retire` action would require a new seed policy on `resource is character`, because the shipped self-access seed only lists two verbs: `permit(principal is character, action in ["read", "write"], resource is character) when { resource.character.id == principal.character.id }` (`internal/access/policy/seed.go:38-43`). Modelling retire as a `retired` flag set through the same masked `write` costs zero new policy and inherits both the self-edit permit and `seed:admin-full-access` (`seed.go:104-109`) for free. `world.Service.DeleteCharacter` (`:745-777`) stays as the hard-delete/cascade path for admin use only — note it cascades entity_properties and emits a `character_deleted` tombstone in one transaction (`:762-775`), so it is *not* reversible and must not be wired to a player-facing button.

**No new ABAC policy is needed for self-edit or admin-edit.** The facade resolves the acting character via `ownedCharacter`, then passes `character:<self>` as the subject to `world.Service`. Editing your own character matches `seed:player-self-access` (`seed.go:38-43`); an admin editing anyone matches `seed:admin-full-access` (`seed.go:104-109`). Verified — this is the single largest simplification available to the milestone.

**One new policy IS needed for the public profile page.** Reading a character you are not co-located with is currently **denied**: `seed:player-character-colocation` permits `read` only when `resource.character.location == principal.character.location` (`seed.go:50-55`), and `seed:player-self-access` covers only yourself. Do **not** widen `read` — `read` is the in-world "look at" gate used by `world.Service.GetCharacter` (`:780-796`) and widening it leaks the in-world description globally. Instead the profile page reads **properties**, which have their own policy family (§3).

**Field mask, following the shipped precedent.** `WebUpdateScene` already forwards an `update_mask` through gateway (`internal/web/scene_handlers.go:356`) and facade — copy that shape rather than inventing per-field RPCs.

### Data flow (new)

```
[form submit]
  → WebService.WebUpdateCharacterProfile        (internal/web/character_handlers.go — proxy only)
  → CharacterAccessService.UpdateCharacterProfile
      resolveAndGate(token)      → PlayerSession, guest rejected
      ownedCharacter(playerID, character_id) → *world.Character   (NotFound if not owned)
      → world.Service.RenameCharacter / UpdateCharacterProfile / SetProperty
            checkAccess(character:<self>, "write", character:<target>)   ← ABAC, default-deny
            mutator.updateCharacter(intent, char)                        ← version-guarded CAS
            └─ same-tx outbox envelope (kind: character_updated)
  ← *characteraccess.CharacterProfile
```

---

## §2 — Admin surface RPCs

### Recommendation

**A new `AdminPortalService` core-side facade + `WebAdmin*` RPCs on the existing `WebService`. The web admin portal MUST NOT ride `admin.v1`, and the enforcement point is the facade, using ABAC — not a bare `RoleStore` lookup.**

### Why `admin.v1` is off the table (three independent reasons, all cited)

1. **Wrong transport, by explicit design.** `AdminService` "is served exclusively over a UNIX domain socket (admin.sock) and **is never exposed over the network**" (`api/proto/holomush/admin/v1/admin.proto:14-18`). Routing browser traffic to it would delete a deliberate trust boundary.
2. **Wrong authentication model.** Its `Authenticate` takes username + password + TOTP and issues a 10-minute opaque token (`admin.proto:34`, `:122-151`), with the password sent in plaintext *because* "the socket path is a trust boundary and the connection is never exposed to the network" (`:128-131`). The web portal authenticates with a signed session cookie translated by `CookieMiddleware`. These are not composable.
3. **Wrong operational shape.** Its surface is break-glass crypto: `Approve` two-person signoff enforcing INV-CRYPTO-72/73/74 (`:36-44`), `Rekey`/`RekeyResume`/`RekeyAbort`/`RekeyStatus`/`RekeyList` (`:55-96`), `AdminReadStream` with dual-control (`:98-105`), `ResetTOTP` (`:46-53`). None of it is "list and edit characters". Adding CRUD there would force every routine admin action through the crypto-operator capability gate.

### Why a separate facade rather than folding admin into `CharacterAccessService`

The milestone's defining constraint is that six more admin sections (stats, player management, moderation, audit viewer, config editor, plugin management) must have declared room. A single `AdminPortalService` gives each a home without ever touching the player-facing surface, and gives exactly one place to apply the admin-authorization decorator. Folding admin RPCs into the character facade would mean re-deciding the placement question five more times.

### Enforcement point, and why it is ABAC not `PlayerHasRole`

Default-deny ABAC is a locked decision. `seed:admin-full-access` already expresses the rule declaratively: `permit(principal is character, action, resource) when { "admin" in principal.character.roles }` (`internal/access/policy/seed.go:104-109`), and `CharacterProvider` already loads `roles` into the attribute bag (`internal/access/policy/attribute/character.go:113-130`, schema at `:190`). `RoleStore.PlayerHasRole` (`internal/store/role_store.go:19-22`) exists specifically as the *admin-socket* operator gate on a different subject namespace (player, not character) and bypasses the policy engine — using it in the web path would create a second, undocumented authorization surface.

So `AdminPortalServer` runs, per RPC:

```
resolveAndGate(token)                             // shared helper, from sceneaccess
ownedCharacter(playerID, acting_character_id)     // shared helper
engine.CanPerformAction(character:<acting>, "access", "admin_section:<id>")   // coarse pre-flight
engine.Evaluate(character:<acting>, "<verb>", "<resource>")                   // per-object
```

This mirrors the shipped two-layer command pattern (`.planning/codebase/ARCHITECTURE.md:124-125`). The coarse layer is what makes §4's section seam work.

### New components

| Component | Path |
|---|---|
| `adminportal.v1.AdminPortalService` proto | `api/proto/holomush/adminportal/v1/adminportal.proto` |
| `AdminPortalServer` | `internal/grpc/adminportal_service.go` |
| `WebAdmin*` handlers | `internal/web/admin_handlers.go` |
| `access.AdminSectionResource(id)` + `ResourceAdminSection` | `internal/access/prefix.go` |
| `/admin` route tree + 6 planned stubs | `web/src/routes/(authed)/admin/` |

### Modified components

| Component | Change |
|---|---|
| `api/proto/holomush/web/v1/web.proto:733-746` | **`WebCheckSessionResponse` carries no roles today** (`player_name`, `player_id`, `is_guest`, `characters`). Add `repeated string roles` so the client can gate nav. Display-only — the server gate is the facade |
| `web/src/lib/stores/authStore.ts:19-20` | Store holds `playerId` + `isGuest` but no roles. Add `roles: string[]` |
| `internal/access/prefix.go:22-33`, `:45-61` | +`ResourceAdminSection = "admin_section:"`, added to `knownPrefixes` |
| `internal/access/policy/seed.go` | +`seed:admin-section-access` |

---

## §3 — Per-field privacy model

### RECOMMENDATION (committed)

**Model profile fields as `entity_properties` rows with `parent_type = 'character'`, and enforce per-field visibility through the existing `property:` ABAC resource. Add no new resource type, no service-layer projection, and no plugin attribute resolver.**

This is not a proposal — it is a description of a subsystem that already ships. Four pieces, all verified:

1. **Storage with per-row visibility.** `entity_properties` (`internal/store/migrations/000001_baseline.up.sql:350-373`) carries `visibility TEXT NOT NULL DEFAULT 'public' CHECK (visibility IN ('public','private','restricted','system','admin'))` plus `visible_to JSONB` and `excluded_from JSONB`, with `UNIQUE(parent_type, parent_id, name)` and CHECK constraints binding the lists to `restricted` visibility (`:364-370`). `world.EntityProperty` (`internal/world/property.go:16-29`) mirrors it, and the Go doc already names `"character"` as a valid `ParentType` (`:19`).
2. **An attribute provider.** `PropertyProvider.ResolveResource` (`internal/access/policy/attribute/property.go:61-147`) emits `visibility`, `owner`, `parent_type`, `parent_id`, `name`, `visible_to`, `excluded_from`, `parent_location` — and does so under the omit-don't-sentinel discipline of ADR `holomush-ti1b` (`:88-141`), so an unresolved field can never fail open. It resolves parent location for character parents through `ParentLocationResolver` (`:161-193`). Schema declared at `:196-220`.
3. **Six seed policies, including a `forbid`.** `seed:property-public-read`, `-private-read`, `-admin-read`, `-owner-write`, `-restricted-visible-to`, and the `forbid`-shaped `-restricted-excluded` (`internal/access/policy/seed.go:110-145`).
4. **A fail-closed per-field filter loop.** `world.Service.ListPropertiesByParent` (`internal/world/service.go:1144-1171`) evaluates `checkAccess(subject, "read", property:<id>)` **once per property** and: appends on permit, **silently drops on `ErrPermissionDenied`**, and **aborts the whole call on `ErrAccessEvaluationFailed`** — the comment names the invariant, "INV-2b: no ghost-data" (`:1161-1163`). A default branch treats any unrecognised error as an infra failure (`:1164-1167`).

Nothing about per-field character privacy needs to be built. It needs to be **used**.

### Justification against the alternatives

**vs. a new `character.field` / `character_field:` ABAC resource.** Requires: a new prefix constant + `knownPrefixes` entry (`internal/access/prefix.go:22-33`, `:45-61`), a new `AttributeProvider` with its own `Namespace()`/`ResolveResource()`/`Schema()`, registration in the resolver, and five-to-six new seed policies that would be near-verbatim copies of `seed.go:110-145`. It buys nothing the property model does not already do — including the `visible_to`/`excluded_from` allow/deny lists, which are non-trivial to re-derive correctly. It is pure duplication of a subsystem with a shipped ADR lineage.

**vs. a projection/filter in the core service.** This is the tempting one and it is the wrong answer under a default-deny mandate. A hand-written Go projector's default is whatever the author typed — there is no engine-level guarantee, no policy-audit surface, and no way to change a visibility rule without a redeploy. It also invisibly diverges from the telnet path, which reaches properties through `ListPropertiesByParent`; two code paths with two privacy models is precisely the class of bug the ABAC engine exists to prevent. And the shipped loop already *is* the projection — it just delegates the decision to the engine instead of to an `if`.

**vs. a plugin-owned attribute resolver.** Characters are host-owned (`internal/world`), not plugin-owned. Making a host entity's privacy depend on a plugin's load order inverts the trust direction: the host would be trusting a plugin to decide whether host data is visible. It would also draw plugin-runtime-symmetry review (`.claude/rules/plugin-runtime-symmetry.md`) for no benefit, since both runtimes would reach the same host chokepoint anyway.

### How the BFF avoids leaking fields it must not even fetch

The gateway structurally cannot leak them: `internal/web.Handler` holds `client`, `contentClient`, `sceneAccess` — three gRPC clients and two duration knobs, no pool, no repo, no service (`internal/web/handler.go:141-152`). It has nothing to fetch *with*.

The real rule is one level in, and must be written into the SPEC as a MUST:

> `CharacterAccessService.GetCharacterProfile` MUST build its response **exclusively** from the slice returned by `world.Service.ListPropertiesByParent(viewerSubject, "character", id)`. It MUST NOT call `PropertyReader.ListByParent` / `PropertyRepository.ListByParent` directly.

Rationale: `ListPropertiesByParent` is the *only* path that runs the per-property `checkAccess`; the repository interface below it (`internal/world/property.go:31-42`) is unfiltered by construction. Denied fields are then never materialised into the response proto and never cross the process boundary — not redacted downstream, simply absent. The codebase already uses reader/writer interface splits as a compile-time fence for exactly this class of mistake (`PropertyReader` vs `PropertyRepository`, `property.go:31-42`, and the `world.Service` write fence noted at `:31-34`), so the SPEC should consider a viewer-scoped reader type that makes the unfiltered call untypeable from the facade.

### Two consequences the SPEC must decide

**(a) Intrinsic columns are not properties.** `characters.name` and `characters.description` are columns (`internal/store/migrations/000001_baseline.up.sql:68-75`) gated only by the whole-entity `read` action — there is no per-column visibility. Recommendation: keep `characters.description` as the **in-world "look at" description** (public to co-located characters, governed by `seed:player-character-colocation`) and put **every profile field** (bio, pronouns, OOC notes, links, images) in `entity_properties`. This needs no data migration and gives a clean conceptual split between "what you see in the room" and "what you see on the profile page".

**(b) `seed:property-public-read` requires co-location.** Its `when` clause is `resource.property.visibility == "public" && principal.character.location == resource.property.parent_location` (`seed.go:111-115`). A web profile page is viewed from anywhere, so that seed does not permit it. A new seed is required. Because `parent_type` is already in the attribute bag (`attribute/property.go:82`) and declared in the schema (`:202`), the narrowest correct form is:

```
seed:profile-public-read
permit(principal is character, action in ["read"], resource is property)
when { resource.property.visibility == "public"
    && resource.property.parent_type == "character" };
```

Flag for the SPEC phase: this widens read access to **all existing public character properties**, not just new profile ones. An audit of current `entity_properties WHERE parent_type='character' AND visibility='public'` rows is a prerequisite, and a `profile.`-prefixed naming convention (§4) may let the policy be narrowed further if the DSL supports prefix matching — verify before assuming it does.

---

## §4 — Extensibility seams

Four seams. Each is a concrete artifact with a stated test that fails if the room is not actually reserved. The fifth (proto reservation) is included with an honest assessment of its weakness.

### Seam S1 — Admin section registry (the nav manifest)

**The seam already exists and is already the single source of truth for two surfaces.** `web/src/lib/nav/sections.ts` defines `WorkspaceSection` with a visibility flag (`requiresPlayer?: boolean`, `:16-23`), the ordered `SECTIONS` registry (`:41-44`, currently `room` + `scenes`), the gate `visibleSections(viewer)` (`:63-67`), and `sectionNavEntries(viewer)` (`:81-87`). Its own doc-comment states the property that makes it the right seam: it is "the single gate both the Rail and the palette go-to entries flow through, so a section is never shown in one surface but hidden in the other (ADR `holomush-stds8`)". `SectionRail.svelte` consumes it at `:8` / `:32`, and the authed layout wires it at `web/src/routes/(authed)/+layout.svelte:6-19`.

**Concrete change:**
- Add `requiresAdmin?: boolean` alongside `requiresPlayer?`, and extend `SectionVisibility` with `isAdmin: boolean` fed from the new `authStore.roles`.
- Add a parallel `ADMIN_SECTIONS` registry for `/admin/*` sub-nav with **all seven entries present from day one**, each carrying `status: 'available' | 'planned'`:
  `characters` (available) + `stats`, `players`, `moderation`, `audit`, `config`, `plugins` (all `planned`).
- Each `planned` section gets a real route rendering a "Coming soon" stub — not a 404.

**Why this is not aspirational:** the `as const satisfies` pattern already in use (`:41-44`) keeps the literal `id` union so the Rail's icon map is exhaustively keyed — "a section without an icon then fails to compile rather than crashing the rail at runtime" (`:36-40`). Adding the six planned ids therefore forces six icons at compile time.

**Test (the file already exists — `web/src/lib/nav/sections.test.ts`):**
1. `expect(ADMIN_SECTIONS.map(s => s.id)).toEqual([...7 ids])` — set equality, so both "no fewer" and "no more".
2. `visibleSections({isGuest:false, isAdmin:false})` contains no `requiresAdmin` section.
3. A route-existence test asserting every `ADMIN_SECTIONS[].href` resolves to a `+page.svelte` on disk.

Test 3 is the one that converts "declared room" from prose into a build failure.

### Seam S2 — ABAC resource namespace that scales to the six sections

Add to `internal/access/prefix.go`:
- `ResourceAdminSection = "admin_section:"` in the resource-prefix block (`:22-33`),
- the same constant in `knownPrefixes` (`:45-61`),
- `AdminSectionResource(id string) string` following the panic-on-empty pattern used by every sibling helper (e.g. `CharacterResource`, `:102-108`) — the panic is load-bearing: an empty resource string would bypass access control.

One seed policy covers all seven sections and every future one:

```
seed:admin-section-access
permit(principal is character, action in ["access"], resource is admin_section)
when { "admin" in principal.character.roles };
```

Every future section then costs **zero policy work** — `AdminPortalServer` calls `CanPerformAction(subject, "access", "admin_section:moderation")` and it works. Narrowing later (e.g. a moderator role that gets `moderation` but not `config`) is a `forbid` policy or a per-section permit, with no code change.

**Test:** table-driven over the seven section ids × {admin character, builder character, plain player, guest} asserting permit only for admin. Set-equality on the id list so a new section cannot be added without appearing in the test.

### Seam S3 — Media schema that absorbs 1 primary + 10 gallery images with no migration

**Store images as `entity_properties` rows, not as columns or a JSONB blob.**

- `profile.image.primary` — one row.
- `profile.image.gallery.00` … `profile.image.gallery.09` — ten rows.
- The row's `value` holds the blob key/URL once an upload path exists (deferred to 999.16).

Why this satisfies "no later migration" *literally*: `entity_properties` is already a key/value table with `UNIQUE(parent_type, parent_id, name)` (`internal/store/migrations/000001_baseline.up.sql:364`). Adding an eleventh image, or a twelfth field, or an entirely new profile section is an `INSERT`. **Zero DDL, ever.** The unique constraint enforces "exactly one primary" at the database level for free. And because each image is its own row, each carries its own `visibility` / `visible_to` / `excluded_from` — per-image privacy falls out of §3 with no additional design.

**Explicitly rejected: `ALTER TABLE characters ADD COLUMN primary_image TEXT, gallery JSONB`.** A JSONB array has no per-element ABAC handle, so per-image privacy would then require the service-layer projection rejected in §3 — the two decisions are coupled, and choosing columns here silently forces the wrong answer there.

**Test, runnable in v0.13 with no upload path:** seed 11 property rows for a character; assert `GetCharacterProfile` returns one primary + ten gallery slots; assert a twelfth insert with a duplicate `name` is rejected by the unique constraint; assert a `visibility='private'` gallery row is absent (not empty, **absent**) from a non-owner viewer's response. This is the proof that the media model needs no migration — it demonstrates the storage working before the uploader exists.

### Seam S4 — World write-command census (a gate, not a seam — but it governs build order)

`internal/world/mutator.go:78-100` declares `writeCommands` as "the EXPLICIT, closed set of world.Service write commands and the single taxonomy kind each emits", enforced by a census that "fails" if a new write command is unregistered or a kind has no producer. Current character entries: `DeleteCharacter → character_deleted`, `UpdateCharacterDescription → character_updated`, `MoveCharacter → character_moved`, `UpdateCharacterPreferences → character_preferences_update` (`:96-99`). Kinds are declared at `internal/world/service.go:51-54` and must mirror `internal/world/outbox/taxonomy.go` exactly (`mutator.go:71-75`).

**Consequence for §1:** rename and retire each need a descriptor row and a taxonomy kind landed in the *same* change as the command. **Open question the plan phase must resolve before writing code:** the census comment describes a *bijection* over the boundary, which suggests two commands may not share one kind. If a masked `UpdateCharacterProfile` is preferred over separate `RenameCharacter`/`SetDescription` commands, the cleanest route is to **rename** the existing `UpdateCharacterDescription` descriptor rather than add a second producer of `character_updated`. Verify the census's exact bijection semantics before committing either way — this repo has a documented history of plans failing on unverified seam assumptions.

### Seam S5 — Proto field reservation (weak; include, but do not lean on)

In `characteraccess.v1.CharacterProfile`, reserve ranges for the deferred surfaces: `reserved 100 to 199;` (media), `reserved 200 to 299;` (stats/sheet). `task lint:proto` (buf breaking-change detection) then rejects a reuse.

**Honest assessment:** reservation prevents *field-number collisions*. It does not create room, is not testable beyond "buf still passes", and does nothing for the admin IA. S1–S3 are the seams that carry the milestone's extensibility requirement; S5 is hygiene. Do not let it appear in a REQ-ID as if it discharged the constraint.

---

## Data Flow — three new paths

**Profile read (public, privacy-filtered):**
```
GET /characters/<id>  →  WebGetCharacterProfile          [gateway: proxy only]
  → CharacterAccessService.GetCharacterProfile
      resolveAndGate → ownedCharacter (viewer's acting character)
      → world.Service.GetCharacter(viewerSubj, id)                 // intrinsic: name
      → world.Service.ListPropertiesByParent(viewerSubj,"character",id)
            per property: checkAccess(read, property:<pid>)
              permit → include | deny → DROP | infra-fail → ABORT   // INV-2b
  ← CharacterProfile built ONLY from the filtered slice
```

**Profile write (owner):** as §1's flow. `write` on `character:<self>` (intrinsics) and `write` on `property:<pid>` (`seed:property-owner-write`, `seed.go:128-133`) for fields.

**Admin section entry:**
```
GET /admin/characters  →  WebAdminListCharacters         [gateway: proxy only]
  → AdminPortalService.ListCharacters
      resolveAndGate → ownedCharacter(acting)
      CanPerformAction(character:<acting>, "access", "admin_section:characters")   ← S2
      Evaluate(character:<acting>, "read", "character:<each>")                     ← seed:admin-full-access
  ← rows
```

Nav visibility is a *second, independent* client-side filter fed by `WebCheckSessionResponse.roles`; it hides dead-end affordances and is never the security boundary — the same posture `requiresPlayer` already takes (`sections.ts:16-23`: "the route itself still guards server-side … this flag only removes the dead-end nav affordance").

---

## Suggested build order

| # | Phase | Delivers | Depends on | Why here |
|---|---|---|---|---|
| 1 | **Portal SPEC** | Admin IA + character data model + privacy model + media schema + full RPC surface | — | Already the milestone's opener; satisfies the Out-of-Scope precondition (`PROJECT.md:195-203`) |
| 2 | **ABAC + schema vocabulary** | `ResourceAdminSection` + `AdminSectionResource()`; `seed:admin-section-access`; `seed:profile-public-read`; the `profile.*` property naming convention; the existing-public-character-property audit | 1 | **Privacy model before profile page** — every later phase's authorization is expressed in this vocabulary. No UI, no RPCs; pure policy + tests |
| 3 | **World character commands** | `RenameCharacter`, `RetireCharacter` (soft); `writeCommands` census rows + taxonomy kinds; unit tests | 1 | Domain layer only. Gated by the census (S4), which is where an unverified assumption would surface cheapest |
| 4 | **Shared facade helpers + `CharacterAccessService`** | Extract `resolveAndGate`/`ownedCharacter` out of `sceneaccess_service.go:109-148`; new facade; `WebCharacter*` proxies; `handler.go` client field; `sub_grpc.go` wiring | 2, 3 | **Mutation RPCs before management UI.** The extraction is first so there is never a second copy of the guest gate |
| 5 | **Character creation + management UI + public profile page** | `/characters` flows; profile page reading the filtered property set; owner edit; per-field public/private toggles; the 11-image schema test (S3) | 2, 4 | First user-visible slice. Ships the media schema proof with no uploader |
| 6 | **Admin portal shell + character administration** | `WebCheckSessionResponse.roles` + `authStore.roles`; `AdminPortalService`; `WebAdmin*`; `/admin` route tree; `ADMIN_SECTIONS` with 7 entries and 6 stubs (S1); character admin surface | 2, 4 | Reuses phase 4's facade helpers and phase 2's `admin_section:` vocabulary. Last because it depends on the most |

Phases 2 and 3 are independent of each other and can run in parallel. Phase 6's `roles` proto field could be pulled into phase 4 to avoid a second `web.proto` regeneration cycle — a scheduling optimisation, not a dependency.

---

## Anti-patterns to guard against in this milestone

**Reaching for `sendCommand` for an admin action.** Every admin mutation is machine-initiated and needs a typed RPC (`.claude/rules/gateway-boundary.md:40-56`). The temptation is highest for bulk operations where a command string looks cheaper.

**Putting the admin role check in the gateway.** `internal/web` may read `roles` off a response to hide nav, but must never *decide* on it. The gateway holds clients only (`handler.go:141-152`); an authorization branch there is business logic and breaks the boundary.

**Building a `character_field:` resource type.** §3. It re-implements `entity_properties` + six shipped seed policies with no gain.

**Fetching the unfiltered property list "and filtering in the handler".** The engine call is per-property inside `ListPropertiesByParent` (`internal/world/service.go:1144-1171`) and it fails *closed* on infra error. A handler-side filter loses the `ErrAccessEvaluationFailed` abort and silently degrades to "no visible properties" — the exact ghost-data scenario INV-2b was written to prevent.

**Wiring the admin portal to `admin.v1`.** §2, three reasons.

**Storing gallery images as a JSONB array on `characters`.** Kills per-image ABAC and forces the rejected projection model.

---

## Integration points

### Internal boundaries

| Boundary | Communication | Notes |
|---|---|---|
| SvelteKit ↔ `internal/web` | ConnectRPC over HTTP, session cookie | Cookie→`X-Session-Token` header translated by `CookieMiddleware`; every scene handler reads it at `headerInjectSessionToken` (`scene_handlers.go:30`) |
| `internal/web` ↔ core facades | gRPC clients only | Status errors pass through unwrapped (`scene_handlers.go:178`) — do not double-translate (`.claude/rules/grpc-errors.md`) |
| Facade ↔ `world.Service` | In-process Go call | Facade owns identity; `world.Service.checkAccess` owns authorization. Never invert |
| Facade ↔ plugin services | gRPC via `serviceRegistry.Resolve` + `BeginServiceDispatch` | Scene-only in v0.13; characters are host-owned so `CharacterAccessService` has no plugin hop |
| `world.Service` ↔ Postgres | Version-guarded CAS + same-tx outbox | Concurrent edits surface `WORLD_CONCURRENT_EDIT` (`internal/world/service.go:827-829`) — the profile edit UI must handle it |

### Surfaces deliberately NOT touched

| Surface | Why |
|---|---|
| `admin.v1.AdminService` | UDS-only break-glass (`admin.proto:14-18`) |
| `world.v1.WorldService` | In-process plugin-only read surface (`world.proto:12-21`, `internal/plugin/setup/world_conn.go:21`) |
| `content.v1.ContentService` | Read-only, 2 RPCs (`content.proto:11-17`). A future admin *config editor* section would need write RPCs here — noted for the deferred section, out of scope now |
| `core.v1.CoreService` | Session/command/stream. Its `CreateCharacter` (`internal/grpc/auth_handlers.go:465-507`) stays as the account-lifecycle path |

---

## Sources

All primary — files read in this repository at `gsd/v0.13-milestone`, 2026-07-31:

- `.planning/PROJECT.md`, `.planning/codebase/ARCHITECTURE.md`
- `.claude/rules/gateway-boundary.md`
- `api/proto/holomush/{web,core,world,admin,content,sceneaccess}/v1/*.proto`
- `internal/web/{handler.go,scene_handlers.go,auth_handlers.go}`
- `internal/grpc/{sceneaccess_service.go,server.go,auth_handlers.go}`
- `internal/world/{service.go,character.go,property.go,mutator.go}`
- `internal/access/{prefix.go,role.go}`, `internal/access/policy/seed.go`, `internal/access/policy/attribute/{property.go,character.go}`
- `internal/store/role_store.go`, `internal/store/migrations/000001_baseline.up.sql`, `000045_character_preferences.up.sql`
- `cmd/holomush/sub_grpc.go`
- `web/src/lib/nav/sections.ts`, `web/src/lib/stores/authStore.ts`, `web/src/routes/(authed)/+layout.svelte`, `web/src/routes/`

---
*Architecture research for: v0.13 Web Portal — Identity & Admin Foundations*
*Researched: 2026-07-31*
