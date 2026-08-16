# Phase 5: Character Identity UI & Public Profiles - Research

**Researched:** 2026-08-12
**Domain:** SvelteKit 5 web client + Go/ConnectRPC facade extension (two RPCs) + ABAC-gated public read
**Confidence:** HIGH for the server surface and the test inventory; MEDIUM for the create-transaction shape (one genuine open design question, see Open Questions Q1)

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

Copied verbatim from `05-CONTEXT.md` `## Implementation Decisions` (D-84…D-98). The planner MUST NOT re-litigate any of these.

- **D-84:** **The public profile lives at root-level `/c/[id]`; `/characters*` stays the owner namespace under `(authed)`.** No path prefix spans two auth postures. Grounded: every route today except `/login`, `/register`, `/reset` sits under `(authed)`, whose `load()` redirects to `/login` on session failure (`web/src/routes/(authed)/+layout.ts:26-30`), so a public profile cannot live there. `/c/[id]` becomes a sibling of `/login` and inherits the root layout exactly as those do — no new route group, no new chrome component. The URL key is the **character id, never the name**.
- **D-85:** **Phase 5 ships no new signed-out chrome.** `TopBar.svelte:141-144` already ships an unconditional anonymous branch. That pair **discharges 007-C's invitation constraint by construction**. **No profile-local sign-in notice is ever needed, and none may be added.**
- **D-86:** **Full reshape; the old shape dies in the same change.** New `CharacterAccessService.CreateCharacter` taking the structured identity card (name, pronouns, concept, species, age, faction) and returning `OwnCharacter`, plus its `WebCreateCharacter` proxy. Its §3 inventory row and census entry land in the **same commit** per D-72. The roster's inline `createCharacter()` is rewritten to the new client.
- **D-87:** **Creation is a route, `/characters/new`.** The roster's dashed create card becomes a link, not an inline input.
- **D-88:** **Create honesty: post-submit echo plus static rule copy.** The created display name is shown in the success path and the form carries one line stating the *class* of rewrite. Carry sketch 009's constraints forward unchanged: the confusable message **MUST NOT name the colliding character**, and the invisible-only case needs its own wording. Rejected: a pure `NormalizeCharacterName` RPC; a client-side TypeScript mirror of the pipeline; a two-step confirm.
- **D-89:** **A new `CharacterAccessService.SetDefaultCharacter` ships**, owner audience, gated on session resolution + ownership, with its `Web*` proxy. It targets a `players` row, so **§9.4's `expected_version` requirement does not reach it**. No `CHARACTER_VERSION_REQUIRED` branch applies.
- **D-90:** **It returns the owner's full roster** (`ListMyCharactersResponse`-shaped). Character-shaped ⇒ **a §2.6 census member** with an `owner` audience verdict and its own §3 inventory row, projected by `projectOwner` per §2.3 — never a struct literal.
- **D-91:** **PROFILE-12's retirement half moves to Phase 6.** Phase 5 ships only the authoring-surface half of the notice on `/characters/[id]`.
- **D-92:** **`/characters/[id]`, sectioned, edit in place**, with a `View public profile →` link to `/c/[id]`. One `GetMyCharacter` call feeds the whole page, and `version` lives in exactly one place.
- **D-93:** **Per-section save, with the in-world description as its own section.** Each section owns its Save and its mask; each response returns the post-write `OwnCharacter` carrying the fresh `version`. A conflict scopes to one section.
- **D-94:** **The absence rule is scoped by viewer-variance.** The discriminating test: *Does this element's presence or absence vary with who is looking?* Yes → absence rule (render nothing). No → named-slot rule (name the reserved capacity).
- **D-95:** **The sheet ships as a named empty section on the profile, not a route.** **And `/c/[id]` renders its not-found inline.** No `+error.svelte` in this phase. **Constraint carried to Phase 6:** when 010-B ships, `/c/[id]` MUST adopt it.
- **D-96:** **The roster's `Not playable` section renders expanded, with the count chip as a collapse control.** Relabel away from "hidden". `idle` is unreachable in v0.13 — write no copy assuming a player sees it. A non-`active` lifecycle **suppresses the session badge entirely**.
- **D-97:** **Criterion 4's "next load" is amended to name the poller.** Reword to *"on the next load after the policy cache reloads (poller interval, default 10s)"*, and let the test drive `Reload()` explicitly rather than sleeping.
- **D-98:** **Criteria 4 and 5 add exactly two integration tests. Existing coverage is cited, not reproved.** The two genuine deltas are (1) criterion 4's corpus-mutation + `Reload()` + same-anonymous-viewer re-read with no write to the character; (2) EXT-05's eleven real media names through the real read path. **Keep them two tests, not one.**

### Claude's Discretion

- Section grouping within the twelve `profile.*` fields (short facts vs long-form prose), and the exact copy of the not-retroactive notice and the name-normalization rule line.
- Which shadcn components to add. Currently installed: `badge`, `button`, `card`, `checkbox`, `command`, `dialog`, `dropdown-menu`, `input`, `input-group`, `label`, `popover`, `resizable`, `scroll-area`, `separator`, `sheet`, `textarea`, `tooltip`. **Not installed** and plausibly needed: `avatar`, `field`, `sonner`, `select`, `skeleton`. Note the initial-letter portrait is pure CSS in 007-C and may need no `avatar` at all.
- Whether the `/c/[id]` page and the `/characters/[id]` authoring page share a presentational component for the identity card.

> **The UI-SPEC has since narrowed the second bullet to zero.** `05-UI-SPEC.md` (approved, checker 6/6) declares **zero new shadcn components** and explicitly declines `avatar`, `skeleton`, `sonner`, `select` and `field` with reasons. That is a *narrowing within* Claude's discretion, not a contradiction of CONTEXT — and it is binding on the planner: "the planner MUST NOT add these."

### Deferred Ideas (OUT OF SCOPE)

- **Game display name on the web client** — #4905. Flagged because `/c/[id]` is the first public, shareable, indexable page the platform ships.
- **The shared `+error.svelte` not-found page** (sketch 010-B) — #4903, Phase 6.
- **Owner-facing `RetireCharacter` / `UnretireCharacter`** — IDENT-04 defers player self-retire beyond v0.13.
- **`RenameCharacter` + the approval dimension** — backlog 999.20.
- **A pre-submit `NormalizeCharacterName` RPC** — rejected in D-88 as disproportionate.
- **An image uploader, storage backend, and media-serving path** — §7.3.
- **A conditional sign-in invitation on profiles** — permanently forbidden as a which-profiles-are-populated oracle.
- **An operator-facing viewer-tier preview** — if ever built, MUST derive distinct outcomes from the live floor set.
- **A populated-corpus re-run of the exposure audit** — #4937, `awaiting-precursor`.
- **A name length cap** — *resolved by this research*: a cap **does** exist (see Pitfall 8).
- **`profile.currently` freshness signal** — 007 open question 3. Not v0.13.
</user_constraints>

---

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| IDENT-01 | Structured identity card creation (name, pronouns, concept, species, age, faction) replacing the name-only stub | §"The `CreateCharacter` reshape" — proto shape, the 12-path allowlist the 5 profile fields reuse, the create pipeline's error-code inventory, and **Open Question Q1** (atomicity of character + 5 property rows) |
| IDENT-05 | Manage all characters from one place, including which is default | §"The `SetDefaultCharacter` net-new write path" — `auth.PlayerRepository.Update` exists but is a full-row update (Pitfall 3); recommend a narrow repo method |
| PROFILE-01 | Public profile page at a stable URL, logged-out-readable, blank fields hide, initial-letter avatar | §"The `/c/[id]` routing seam"; `GetCharacterProfile` + `WebGetCharacterProfile` already work end-to-end server-side for the anonymous rung |
| PROFILE-02 | Profile and sheet are separate surfaces; sheet ships empty | D-95 named empty section; no new route, no new RPC — pure render work |
| PROFILE-06 | Rumors / RP-hooks field | `profile.rumors` already in `UpdateCharacterProfileRequest` field 12 and the mask allowlist — **no new profile field ships** |
| PROFILE-07 | Volatile "Currently" status line | `profile.currently` field 13, allowlist entry, `world.MaxNameLength` (100-byte) cap |
| PROFILE-08 | OOC RP-preferences block | `profile.rp_preferences` field 14, `world.MaxDescriptionLength` (4000-byte) cap. **MUST NOT be written to `characters.preferences`** |
| PROFILE-09 | Time zone field | `profile.timezone` field 15, 100-byte cap |
| PROFILE-10a | Public profile also renders `characters.description` | Already on the wire: `PublicCharacter.description` (`characteraccess.proto:109`), gated by the `read_description` action, not a per-attribute floor |
| PROFILE-12 | Not-retroactive notice in the UI | D-91 scopes to the authoring surface only; pure copy on `/characters/[id]` |
| EXT-05 | 1 primary + 10 gallery rows through the real schema; an 11th primary rejected | §"Validation Architecture" test 2. `projectPublic`/`projectOwner` **already route media rows** to `primary_image`/`gallery`; the UNIQUE constraint is already proven by `property_repo_test.go:430` and MUST NOT be reproved |
| EXT-08 | Named empty slot for web DMs, not a dead affordance | D-94 named-slot rule; pure render work |
</phase_requirements>

---

## Summary

**Phase 5 is a two-sided phase whose Go half is small but load-bearing, and whose test half is almost entirely already written.** The `CharacterAccessService` facade shipped in Phase 4 with six RPCs; Phase 5 adds two (`CreateCharacter` reshaped, `SetDefaultCharacter` net-new) plus their `Web*` proxies. Every server-side mechanism the public profile needs — anonymous rung resolution, reachability gate, per-attribute tier floors, the `PublicCharacter` projection including media routing — is **shipped and tested**. The frontend is where the volume is: four routes, of which one (`/c/[id]`) sits outside `(authed)` for the first time in the app's history.

**The three highest-risk seams, in order.** (1) **`CreateCharacter`'s transaction shape** — creation goes through `auth.CharacterService.CreateBound` → `CharacterGenesisService.Create`, which commits character + binding + genesis envelope atomically; the five profile fields are `entity_properties` rows written by a *different* domain command that demands `expected_version >= 1`. Nothing in the tree writes both in one transaction. That is a real design decision the plan must take, not inherit (Open Question Q1). (2) **`CoreService.CreateCharacter` must survive the reshape** — `internal/telnet/gateway_handler.go:570` drives it from the telnet `CREATE <name>` verb; D-86's "the old shape dies" reaches `WebCreateCharacter` only. (3) **Two census meta-tests are set-equality gates that go RED the moment the proto compiles**, so their rows land in the same commit as the proto.

**On verification, the phase's own scope discipline is its biggest lever.** D-98's audit is correct and I re-verified every citation: the UNIQUE constraint, the eleven-media-name enumeration, the synthetic-fourth-rung ordinal guard and the two-evaluations-per-attribute proof are all live and green. The genuine delta is exactly two integration specs, and both fit inside the *existing* `test/integration/access/` harness (`profileCorpusStore` + `newCorpusEngine` + `insertProperty`) with one small helper extension. Criterion 4's new test is also the assertion that would bind **INV-ACCESS-10**, which currently ships `binding: pending`.

**Primary recommendation:** Take the proto + census + facade + wiring as Wave 0 (it unblocks everything and breaks 8 constructor call sites at once), settle Q1 explicitly in the plan before any create code is written, build the four routes against the already-generated clients, and add exactly the two integration specs D-98 names — citing, never reproving, the five clauses already covered.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Anonymous profile read (`/c/[id]`) | **API / Backend** (`CharacterAccessService.GetCharacterProfile`) | Frontend Client (render only) | §9.1: the facade holds every decision; the gateway proxies bytes. `resolveViewerIdentity` degrades an unresolvable token to `anonymous` (least-privileged) rather than erroring [VERIFIED: internal/grpc/characteraccess_service.go:437-452]. |
| Viewer-tier floor evaluation | **API / Backend** (`internal/access/profilevis` + ABAC engine) | — | §8.9 forbids client-side enforcement. Absence is a **wire** property; the client MUST NOT hold an expected-field list and diff against it (UI-SPEC Absence Contract). |
| Not-found opacity (unreachable vs nonexistent) | **API / Backend** | Frontend Client (single branch on `NotFound`) | §9.6 returns one code for two causes deliberately; a second client-side branch reconstructs the disclosure. |
| Character creation + name admission | **API / Backend** (`internal/charname` gate → `auth.CharacterService` → `CharacterGenesisService`) | — | D-88 forbids a client-side normalizer mirror: it would duplicate a security-adjacent normalizer (NFKC → `Cf` strip → whitespace collapse → case-fold) in a second language. |
| `players.default_character_id` write | **Database / Storage** (new narrow repo method) → **API / Backend** (`SetDefaultCharacter`) | — | The write path does not exist as a narrow operation today; only a full-row `Update` and a retire-time `NULL` clear do (Pitfall 3). |
| Per-section save + optimistic concurrency | **API / Backend** (`expected_version` CAS in the domain `UPDATE` predicate) | Frontend Client (holds one `version`, resends it) | The handler deliberately does NOT re-read and substitute the current version — that would convert every stale client into last-write-wins [VERIFIED: internal/grpc/characteraccess_write.go:238-248]. |
| Byte-cap feedback on short fields | **API / Backend** (authoritative) | Frontend Client (advisory counter only) | Caps are enforced in the facade in **bytes**; a client counter using `.length` disagrees with the server on any multi-byte value (Pitfall 7). |
| Route auth posture | **Frontend Server (SSR)** — n/a here; **Frontend Client** routing | — | `adapter-static` with `fallback: 'index.html'` and `ssr = false`: every path is already HTTP 200 + the SPA shell, so route-level indistinguishability is structural [VERIFIED: web/svelte.config.js:5-10, web/src/routes/+layout.ts:1-2]. |
| Static asset / SPA delivery | **CDN / Static** (embedded `internal/web/dist`) | — | `task web:embed` copies `web/build` for `go:embed` [VERIFIED: Taskfile.yaml:710-716]. |

---

## Standard Stack

Phase 5 introduces **no new dependency in either ecosystem**. Every table below is the *existing* stack, verified in-tree, listed so the planner can cite versions without guessing.

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go | `1.26.5` | Server | `go.mod:3` — `go 1.26.5` [VERIFIED: go.mod:1-3] |
| SvelteKit 2 / Svelte 5 (runes) | in `web/package.json` | Web client | Shipped app framework [CITED: web/CLAUDE.md "Tech Stack"] |
| shadcn-svelte (style `nova`, baseColor `slate`) on `bits-ui` 2.x | pre-existing `web/components.json` | Component layer | `05-UI-SPEC.md` Design System table |
| `@connectrpc/connect` + `connect-web` | in `web/package.json` | gRPC-Web transport | Shipped; `createClient(WebService, transport)` is the app-wide idiom [VERIFIED: web/src/routes/(authed)/characters/+page.svelte:7-19] |
| Tailwind CSS v4 | `@tailwindcss/vite` plugin | Layout/spacing utilities | [VERIFIED: web/vite.config.ts:2,6] |
| `@lucide/svelte` | `1.25.0` | Icons | [VERIFIED: web/package.json:16] |
| `vitest` | `4.1.10` | Web unit + component tests | [VERIFIED: web/package.json:42] |
| `jsdom` | `29.1.1` | vitest DOM environment | [VERIFIED: web/package.json:34] |
| `@playwright/test` | `1.61.1` | E2E | [VERIFIED: web/package.json:24] |

### Supporting (server, all already imported by the touched packages)

| Library | Purpose | When to Use |
|---------|---------|-------------|
| `github.com/samber/oops` | Structured error codes | Every new error site. Note the pinned-version gotcha in Pitfall 10. |
| `google.golang.org/grpc/status` + `codes` | Wire status at the handler boundary | `status.Errorf(codes.X, "<static literal>")` is wrapcheck-allowlisted; `status.Error` is not (needs a line-scoped `//nolint:wrapcheck`) [CITED: .claude/rules/grpc-errors.md §"Linter compliance"] |
| `google.golang.org/protobuf/types/known/fieldmaskpb` (via generated code) | The `update_mask` at field 99 | Already carried by `UpdateCharacterProfileRequest` |
| `github.com/oklog/ulid/v2` | Id parsing | Already used for `character_id` parse-then-opaque-NotFound |
| Ginkgo/Gomega | Full-stack integration specs (`//go:build integration`) | Both new integration specs |
| `testify` `require`/`assert` | Unit + meta tests | Census edits |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Extending the 8-arg positional `NewCharacterAccessServer` | A functional-options constructor | Options would avoid touching 8 call sites — but the constructor's own doc comment states *"Every dependency is required; there are no optional setters"* [VERIFIED: internal/grpc/characteraccess_service.go:211-213]. Converting it is a refactor with its own blast radius. **Recommend: add positional params and update the 8 sites.** |
| A pure-CSS initial-letter portrait | shadcn `avatar` | UI-SPEC declines `avatar` with a reason; in v0.13 the portrait is *never* an image, so the primitive would exist to render one styled letter. **Recommend: pure CSS.** |
| A toast for the create echo | A persistent inline `role="status"` region | UI-SPEC's "Deliberate refinement of D-88": no toast infrastructure exists (zero `toast`/`sonner` refs), a 4-second toast is a poor carrier for "the server rewrote your name", and a persistent region is directly Playwright-assertable. **Recommend: the inline region.** |
| Container queries (`@container vp`) | Media query at 768px | UI-SPEC records the deviation: Phase 5 ships no shell, so the content box and the viewport are the same thing. **Recommend: `@media`.** |

**Installation:** none. No `npm install`, no `go get`, no `pnpm add` is required by this phase.

---

## Package Legitimacy Audit

**This phase installs zero external packages**, so the legitimacy gate has no members.

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| *(none)* | — | — | — | — | — | — |

**Packages removed due to `[SLOP]` verdict:** none.
**Packages flagged as suspicious `[SUS]`:** none.

Grounding: `05-UI-SPEC.md` §"Component inventory — **zero new shadcn components**" and §"Registry Safety" (*"No third-party registry is declared for this phase, so the registry vetting gate does not apply"*). The Go side reuses packages already in `go.mod` and already imported by `internal/grpc`, `internal/web`, `internal/auth` and `internal/world`. **If the planner finds it needs a package after all, the gate re-arms and the package MUST be run through `gsd-tools query package-legitimacy check` before it enters a plan.**

---

## Architecture Patterns

### System Architecture Diagram

```
ANONYMOUS VISITOR                          AUTHENTICATED OWNER
      │                                            │
      │ GET /c/<charULID>                          │ GET /characters, /characters/new, /characters/[id]
      ▼                                            ▼
┌─────────────────────────┐              ┌──────────────────────────┐
│ SPA shell (index.html)  │              │ (authed) +layout.ts      │
│ adapter-static fallback │              │  webCheckSession()       │
│ root +layout: TopBar    │              │  fail ⇒ redirect /login  │
│  anonymous branch ships │              └───────────┬──────────────┘
│  Login / Register       │                          │
└───────────┬─────────────┘                          │
            │                                        │
            └──────────────┬─────────────────────────┘
                           ▼
              ┌───────────────────────────────┐
              │ ConnectRPC → WebService       │  (the ONLY surface the browser sees)
              │ CookieMiddleware injects      │
              │ X-Session-Token header        │
              └───────────────┬───────────────┘
                              ▼
              ┌───────────────────────────────────────────────┐
              │ internal/web Handler (gateway)                │
              │  • nil-client guard                           │
              │  • token from HEADER, never request body      │
              │  • field-by-field forward, computes NOTHING   │
              └───────────────┬───────────────────────────────┘
                              ▼ gRPC
    ┌───────────────────────────────────────────────────────────────────┐
    │ CharacterAccessServer  (internal/grpc) — THE DECISION LAYER       │
    │                                                                   │
    │  PUBLIC audience              │  OWNER audience                   │
    │  resolveViewerIdentity        │  resolveAndGate (denies guests)   │
    │   empty/expired ⇒ anonymous   │   → ownedCharacter (NotFound)     │
    │  ↓                            │   → ownedCharacterForMutation     │
    │  profileVis.Reachable ────────┼──── deny ⇒ NotFound (uniform)     │
    │  ↓ permit                     │   → requireGuardedVersion         │
    │  world.GetCharacterDescription│   → mask allowlist (12 paths)     │
    │  ↓                            │   → per-field BYTE caps           │
    │  ListPropertiesByParent(viewer│   ↓                               │
    │    caller)  [term B]          │  worldMutator.Update…Attributes   │
    │  ↓                            │  worldMutator.Update…Description  │
    │  VisibleAttributes            │   ↓                               │
    │   [reachability + term A      │  ownerMutationResponse (re-read,  │
    │    + term B, per row]         │   fresh version)                  │
    │  ↓                            │                                   │
    │  projectPublic ───────────────┴──── projectOwner                  │
    │   (SOLE constructors; media rows route to primary_image/gallery)  │
    └────────────┬──────────────────────────────────┬───────────────────┘
                 ▼                                  ▼
    ┌──────────────────────────┐      ┌──────────────────────────────────┐
    │ ABAC engine              │      │ world.Service  (checkAccess)     │
    │  policy.Cache (immutable │      │  → CharacterRepository (CAS on   │
    │   compiled Snapshot)     │      │     characters.version)          │
    │  ← Poller, 10s default   │      │  → PropertyRepository            │
    │    Interval; Reload()    │      │  → same-tx outbox (INV-WORLD-1)  │
    └──────────────────────────┘      └──────────────────────────────────┘
                                                    │
    ┌───────────────────────────────────────────────┴──────────────────┐
    │ PostgreSQL                                                        │
    │  characters(name, description, version, status, normalized_name)  │
    │  entity_properties(parent_type,parent_id,name,value,visibility)   │
    │    UNIQUE(parent_type,parent_id,name)  ← exactly-one-primary      │
    │  players(default_character_id)  ← the NEW write target            │
    └───────────────────────────────────────────────────────────────────┘

CREATE PATH (separate, and this is the open question):
  CreateCharacter → charname.Gate.Check (NFKC, Cf strip, syntax, block list,
     mixed script, skeleton) → CharacterService.CreateBound
     → CharacterGenesisService.Create  [character + binding + envelope, ONE tx]
     → ??? five profile.* rows  [a DIFFERENT command, a SECOND tx]   ← Q1
```

### Recommended Project Structure

```
api/proto/holomush/
├── characteraccess/v1/characteraccess.proto   # +2 rpc, +4 messages
└── web/v1/web.proto                           # +2 rpc, +4 messages (reshape WebCreateCharacter*)

internal/grpc/
├── characteraccess_write.go        # + CreateCharacter, + SetDefaultCharacter (or a sibling file)
├── characteraccess_service.go      # constructor gains 1-2 deps
└── characteraccess_projection.go   # UNCHANGED — projectOwner already does the job

internal/web/
└── character_handlers.go           # + WebCreateCharacter (moved here from auth_handlers.go),
                                    #   + WebSetDefaultCharacter

internal/auth/postgres/player_repo.go   # + a NARROW UpdateDefaultCharacter (Pitfall 3)
internal/auth/player.go                 # + its interface method

cmd/holomush/sub_grpc.go            # constructor call site

test/meta/
├── character_rpc_census_test.go            # + 4 inventory rows; MOVE WebCreateCharacter
└── characteraccess_routing_census_test.go  # + 2 guest-gate, +1 ownership, +2 web-proxy

test/integration/access/
├── character_profile_read_test.go   # host for BOTH new specs (or one new sibling file)
└── (new) media_schema_test.go       # EXT-05, if kept separate per D-98

web/src/routes/
├── c/[id]/+page.svelte              # NEW — root level, anonymous-readable
├── (authed)/characters/+page.svelte # rewritten: sectioned roster, new client
├── (authed)/characters/new/+page.svelte    # NEW
└── (authed)/characters/[id]/+page.svelte   # NEW

web/src/lib/characters/             # NEW — the flow layer, mirroring web/src/lib/scenes/
├── client.ts                        # typed WebService wrappers
├── createFlow.ts                    # mirrors scenes/createFlow.ts's authoritative-RPC idiom
└── *.test.ts                        # vitest units for the flows

web/e2e/public-profile.spec.ts      # NEW — the first genuinely logged-out E2E
```

### Pattern 1: The web proxy is a five-move shape, copied verbatim

**What:** Every character proxy in `internal/web/character_handlers.go` is: nil-client guard → token from header → bounded context → field-by-field forward → log-then-pass-through.
**When to use:** both new proxies.
**Example:**

```go
// Source: internal/web/character_handlers.go:99-122 (WebGetMyCharacter), verbatim shape
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

> The routing census requires **both conjuncts**: the body must reference the bare identifier `headerInjectSessionToken` **and** name its paired facade method [VERIFIED: test/meta/characteraccess_routing_census_test.go:582-600]. A proxy that reads the token from the request body fails the first; one that calls a different facade method fails the second.

### Pattern 2: The owner mutation gate order is normative

**What:** session → ownership → version guard → mask allowlist → empty-mask no-op → value validation → domain write → re-read for the fresh version.
**When to use:** `SetDefaultCharacter` follows the first two and skips the version guard (D-89). `CreateCharacter` follows the first only.
**Example:** `internal/grpc/characteraccess_write.go:257-332` (`UpdateCharacterProfile`). The doc block at `:230-236` states the order is normative and *why* the empty-mask short-circuit sits **after** authorization (so a no-op cannot be used as an existence oracle).

### Pattern 3: Absence is produced at the projection, never at the renderer

**What:** `projectPublic`/`projectOwner` drop an empty value rather than emitting present-and-empty, and route media names to their own fields.
**Example:**

```go
// Source: internal/grpc/characteraccess_projection.go:79-106
	for name, value := range profile {
		if value == "" {
			continue
		}
		if name == profileImagePrimaryName || isProfileGallerySlotName(name) {
			continue
		}
		if out.Profile == nil {
			out.Profile = make(map[string]string, len(profile))
		}
		out.Profile[name] = value
	}

	if mediaID := profile[profileImagePrimaryName]; mediaID != "" {
		out.PrimaryImage = &characteraccessv1.ProfileImage{MediaId: mediaID}
	}

	for _, slot := range profileGallerySlotNames {
		mediaID := profile[slot]
		if mediaID == "" {
			continue
		}
		out.Gallery = append(out.Gallery, &characteraccessv1.ProfileImage{MediaId: mediaID})
	}
```

**Consequence for EXT-05:** the media read-back path is **already built**. The new test proves it against real rows; it does not build it.

### Pattern 4: The create-flow idiom — the RPC is authoritative, post-create refresh is best-effort

```ts
// Source: web/src/lib/scenes/createFlow.ts:22-38
	const sceneId = scene?.id ?? '';
	// refresh/select are best-effort UI updates: the scene is already created and
	// authoritative, so a post-create failure here MUST NOT propagate as a create
	// failure — that would show "Create failed" to the user and risk a duplicate
	// scene on retry. Swallow and warn; the workspace reconciles on next interaction.
	try {
		await workspaceStore.refresh(opts.characters);
		if (sceneId) {
			await workspaceStore.select(sceneId, '', opts.characterId);
		}
	} catch (e) {
		console.warn('[submitCreateScene] post-create refresh/select failed', e);
	}
	return sceneId;
```

Mirror this for character creation: navigate + echo from the **response** the create RPC returned, and never surface a post-create roster-refresh failure as "create failed".

### Pattern 5: Svelte component tests mount directly — there is no testing-library

```ts
// Source: web/src/lib/components/scenes/PoseCard.svelte.test.ts:13-22
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

`@testing-library/svelte` is **not** a dependency [VERIFIED: web/package.json — `rg "testing-library"` returns no match]. Files named `*.svelte.test.ts` run in the vitest `client` project with `resolve.conditions: ['browser']`; plain `*.test.ts` run in the `server` project [VERIFIED: web/vite.config.ts:13-33]. **This is the mechanism for both UI-SPEC backstop rows** (the media renderer and the byte counter).

### Anti-Patterns to Avoid

- **A client-side field allowlist diffed against the response.** Absence is a wire property (§8.9). The client renders what arrived.
- **A second client branch distinguishing "no such character" from "below the floor".** §9.6 returns one code for both deliberately; a second branch reconstructs the disclosure.
- **`sendCommand` for any structural write.** A GUI button is a machine-initiated structural write and MUST reach a typed RPC [CITED: .claude/rules/gateway-boundary.md §"Structural writes use typed RPCs"].
- **Interpolating an inner error into a returned status message.** `status.Errorf(codes.Internal, "…: %v", err)` leaks internals past the trust boundary [CITED: .claude/rules/grpc-errors.md].
- **Adding a name-keyed character lookup.** §9.2 forbids it for v0.13, and the SPEC's reviewer MUST verify no PR adds one.
- **Deleting a census inventory row to make a red census green.** §3.1 rule 3 names this as the erosion the gate exists to catch.
- **`/c/{name}` links.** Sketch 008's HTML shows `href="/c/{c.name}"`; UI-SPEC marks it **stale**. The key is the id.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Character-name normalization / preview | A TypeScript NFKC + `Cf`-strip + case-fold mirror | The server's `charname.Normalize`, echoed post-submit from `OwnCharacter.name` | Two sources of truth for the value the UNIQUE index depends on. Explicitly rejected in D-88. |
| Exactly-one-primary-image | An application-side "does a primary already exist" check | `UNIQUE(parent_type,parent_id,name)` | §7.3: *"a hand-written check beside a database constraint is a second source of truth that can disagree with the first"*. |
| Proving the UNIQUE constraint | A new duplicate-insert test | Cite `TestPropertyRepository_ParentNameUniqueness` [VERIFIED: internal/world/postgres/property_repo_test.go:430-450] | Rule `7zy1161fh1` duplicate-gate anti-pattern; D-98 names it explicitly. |
| Tier-floor ordinal comparison | `tier >= "guest"` anywhere the client touches tiers | Set membership (§8.2.1); the client touches tiers **not at all** | `TestASyntheticFourthRungClearsNeitherShippedTierFloor` [VERIFIED: internal/access/profilevis/tierfloor_test.go:173-176] already pins this server-side. |
| Ownership resolution on a new owner RPC | A fresh `charRepo.ListByPlayer` + loop | `s.ownedCharacter` (promoted from the embedded `playerGate`) or `s.ownedCharacterForMutation` | The routing census's ownership predicate accepts exactly those two names, and the wrapper's integrity is separately asserted [VERIFIED: test/meta/characteraccess_routing_census_test.go:180-188, 516-542]. |
| A shared not-found error boundary | A new `+error.svelte` | Inline not-found on `/c/[id]` (D-95) | Phase 6 owns the single root boundary (#4903), and Phase 6's meta-test will assert **exactly one**. |
| A "saving…" spinner component | A new spinner | `disabled` + `aria-busy="true"` on the same button, label unchanged | UI-SPEC Loading states: a label swap reflows the button. |
| Full-row player update to set the default | `players.Update(ctx, player)` | A narrow `UpdateDefaultCharacter(ctx, playerID, charID)` | Pitfall 3 — read-modify-write of a row carrying `password_hash` with no version guard. |

**Key insight:** almost every "new mechanism" this phase looks like it needs already exists one layer down. The phase's dominant failure mode is *building a second copy of a shipped gate* — which is precisely what rule `7zy1161fh1` and §3.1 rule 3 exist to catch, and precisely what D-98 already caught once.

---

## Runtime State Inventory

> Included because `CreateCharacter` is a **breaking reshape of a shipped, wired RPC**, and a reshape has the same class of blind spot a rename does.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| **Stored data** | None. No table stores an RPC name, a request shape, or a `profile.*` field list. `entity_properties` carries no tier column and no migration writes one (§8.5, structurally true — no `visibility`-tier stamping exists). | none |
| **Live service config** | None. The gateway wires its clients in code (`cmd/holomush/gateway.go:321` — `web.WithCharacterAccessClient(grpcClient)`); there is no runtime route table or externally-stored RPC registry. | none |
| **OS-registered state** | None. | none |
| **Secrets / env vars** | None touched. | none |
| **Build artifacts / generated code** | **`pkg/proto/**/*.pb.go`** and **`web/src/lib/connect/**/*_pb.ts`** are committed generated output. A proto change that does not regenerate + commit both fails CI's stale-diff check. `task proto && task web:generate` [VERIFIED: Taskfile.yaml:576, 686-696; CLAUDE.md §"Generated code"]. | regenerate + commit in the **same** change |
| **Other runtime consumers of the reshaped RPC** | **`internal/telnet/gateway_handler.go:570`** calls `CoreService.CreateCharacter` from the telnet `CREATE <name>` verb, through the `CoreClient` interface at `:66`. `internal/grpcclient/client.go:205` also wraps it. | **`CoreService.CreateCharacter` MUST SURVIVE.** D-86's "the old shape dies" reaches `WebCreateCharacter` only. |

**The canonical question, answered:** after every file is updated, the only runtime systems still holding the old shape are the two committed generated-code trees (handled by regeneration) and the telnet path (handled by *not* deleting the core RPC).

---

## Common Pitfalls

### Pitfall 1: Deleting `CoreService.CreateCharacter` along with the web shape

**What goes wrong:** telnet's `CREATE <name>` stops compiling or stops working.
**Why it happens:** D-86 says "the old shape dies in the same change", and it is natural to read that as covering both halves of the proxy pair. It does not: §9.3's reshape paragraph names `WebCreateCharacter` (`web.proto:177`, response scalar at `:656`) and nothing else.
**How to avoid:** keep `holomush.core.v1.CoreService.CreateCharacter` and its `characterNameReachableRPCs()` census row. Repoint only `WebCreateCharacter`.
**Warning signs:** `internal/telnet/gateway_handler.go` fails to build; `internal/web/handler.go:75`'s `CoreClient` interface loses a method telnet still needs.
[VERIFIED: internal/telnet/gateway_handler.go:560-580; internal/grpcclient/client.go:205; internal/web/handler.go:75]

### Pitfall 2: The two census meta-tests go RED the moment the proto compiles

**What goes wrong:** `task test` fails on `test/meta/` before any handler is written.
**Why it happens:** `character_rpc_census_test.go` derives its set from **registered service descriptors**, so a declared RPC whose response transitively contains `OwnCharacter` is a census member the instant the generated code lands. `characteraccess_routing_census_test.go` additionally asserts an **audience partition**: every exported unary-RPC-shaped method on `CharacterAccessServer` must be in `owner ∪ public`, and the two literals must be disjoint.
**How to avoid:** land proto + generated code + **all** census rows in the same commit (D-72). Concretely:

| File | Function | Add |
|------|----------|-----|
| `character_rpc_census_test.go` | `characterReadSurfaceInventory()` | `…CharacterAccessService.CreateCharacter`, `…CharacterAccessService.SetDefaultCharacter`, `holomush.web.v1.WebService.WebSetDefaultCharacter` |
| `character_rpc_census_test.go` | `characterNameReachableRPCs()` | **MOVE** `holomush.web.v1.WebService.WebCreateCharacter` out (its response becomes type-reachable via `OwnCharacter`); **KEEP** `holomush.core.v1.CoreService.CreateCharacter` (still a bare scalar, still telnet's) |
| `characteraccess_routing_census_test.go` | `characterGuestGateRPCs()` | `CreateCharacter`, `SetDefaultCharacter` |
| `characteraccess_routing_census_test.go` | `characterOwnershipRPCs()` | `SetDefaultCharacter` **only** — `CreateCharacter` names no existing character id, exactly as `ListMyCharacters` does not (that absence is asserted at `:566-569`) |
| `characteraccess_routing_census_test.go` | `characterWebProxyRPCs()` | `WebCreateCharacter`, `WebSetDefaultCharacter` |

**Warning signs:** a symmetric-difference failure naming an "EXTRA member … with no inventory row", or the partition test's *"an RPC carrying NO audience classification at all"*.
[VERIFIED: test/meta/character_rpc_census_test.go:133-216, 276-312; test/meta/characteraccess_routing_census_test.go:190-279, 603-657]

### Pitfall 3: `players.default_character_id` — the write path is *narrower* than "nowhere", and the wide one is a trap

**What goes wrong:** `SetDefaultCharacter` is implemented as `player, _ := playerRepo.GetByID(...); player.DefaultCharacterID = &id; playerRepo.Update(ctx, player)`, and a concurrent password change or lockout update is silently clobbered.
**Why it happens:** the prior-session ground truth said the write path "exists NOWHERE". That is true of the **world** repo (`internal/world/postgres/character_repo.go:539` only clears on retire) and of the facade — but **not** of the auth repo. `auth.PlayerRepository.Update` writes `default_character_id = $8` as one column of a **full-row UPDATE that also writes `password_hash`, `email`, `failed_attempts`, `locked_until` and `preferences`, with no version guard**, and `internal/auth/guest_service.go:176-185` already uses it (best-effort, with a warn on failure).
**How to avoid:** add a narrow repository method beside the existing narrow ones — `UpdatePassword` and `UpdatePasswordAndClearLockout` are the shape precedent [VERIFIED: internal/auth/player.go:195-200] — e.g. `UpdateDefaultCharacter(ctx, playerID, characterID ulid.ULID) error` doing `UPDATE players SET default_character_id = $2, updated_at = $3 WHERE id = $1`. Add it to the `auth.PlayerRepository` interface so the gate's existing `playerRepo` handle (promoted onto the facade) can call it with no new constructor argument.
**Warning signs:** a `GetByID` immediately followed by an `Update` in a new handler.
[VERIFIED: internal/auth/postgres/player_repo.go:188-214 — the UPDATE statement lists `username, password_hash, email, email_verified, failed_attempts, locked_until, default_character_id, preferences, is_guest, updated_at`; internal/auth/guest_service.go:174-185; internal/world/postgres/character_repo.go:536-545]

### Pitfall 4: The SPEC's error code is `CHARACTER_NAME_INVALID`; the shipped code is `CHARACTER_INVALID_NAME`

**What goes wrong:** a test asserts the SPEC spelling and never fires, or the facade re-exports the domain spelling and §9.6's table becomes fiction.
**Why it happens:** the two words are transposed and both read naturally.
**How to avoid:** the facade's `CreateCharacter` owns a mapping table. The complete inventory of what the create path can produce:

| Origin code (verbatim) | Source | §9.6 target | Wire status |
|---|---|---|---|
| `"NAME_INVALID_SYNTAX"` | `internal/charname/gate.go:155` | `CHARACTER_NAME_INVALID` | `InvalidArgument` |
| `"NAME_EMPTY_NORMAL_FORM"` (two messages, one code) | `internal/charname/pipeline.go:121,127` | `CHARACTER_NAME_INVALID` | `InvalidArgument` |
| `"NAME_BLOCKED"` | `internal/charname/gate.go:172` | `CHARACTER_NAME_INVALID` | `InvalidArgument` |
| `"NAME_CONFUSABLE"` | `internal/charname/gate.go:193` | `CHARACTER_NAME_INVALID` | `InvalidArgument` |
| `"NAME_MIXED_SCRIPT"` | `internal/charname/mixedscript.go:148` | `CHARACTER_NAME_INVALID` | `InvalidArgument` |
| `"NAME_UNASSIGNED_SCRIPT"` | `internal/charname/mixedscript.go:102` | `CHARACTER_NAME_INVALID` | `InvalidArgument` |
| `"NAME_SKELETON_LOOKUP_FAILED"` | `internal/charname/gate.go:181` | **not a name error** — an outage | `Internal` (§8.10) |
| `"NAME_SKELETON_UNVERIFIABLE"` | `internal/charname/gate.go:185` | **not a name error** — degraded | `Unavailable` or `Internal` — planner decides |
| `"CHARACTER_INVALID_NAME"` | `internal/auth/character_service.go:303,305,307` (`mapGateError`) | `CHARACTER_NAME_INVALID` | `InvalidArgument` |
| `"CHARACTER_NAME_TAKEN"` | `internal/auth/character_service.go:153,182,222` (pre-check, admitted-key check, **and the 23505 handler**) | `CHARACTER_NAME_TAKEN` | `AlreadyExists` |
| `"CHARACTER_LIMIT_REACHED"` | `internal/auth/character_service.go:193` | **no §9.6 row exists** | planner decides — see Open Question Q3 |
| `"CHARACTER_NO_STARTING_LOCATION"` | `internal/auth/character_service.go:203` | **no §9.6 row exists** | planner decides — see Q3 |
| `"CHARACTER_CREATE_FAILED"` | `internal/auth/character_service.go:150,179,190,209,226` | generic | `Internal` |

**Player-facing message carriage.** UI-SPEC's error table says `CHARACTER_NAME_INVALID` renders the **server-supplied message verbatim**, because the `charname` messages are authored for players (e.g. `"that character name is too similar to an existing one"`, `"that name contains no visible characters; please use letters"`). That is a **deliberate, scoped exception** to `.claude/rules/grpc-errors.md`'s "never leak inner errors": these are not inner errors, they are authored copy. The planner MUST make the carriage explicit (a dedicated status message field is fine; a `%v` of a wrapped error is not) and MUST NOT let the confusable message name the colliding character — `gate.go:188-194` already guarantees it does not, and no client-side enrichment may reintroduce it.
[VERIFIED: all line/code citations above read this session]

### Pitfall 5: `expected_version` — the boundary rejects zero because the repository accepts it

**What goes wrong:** a new mutation forwards `expected_version` without a boundary check, and a client that omits the field performs an **unguarded** write that succeeds.
**Why it happens:** `CharacterRepository.Update` appends the CAS predicate **only when `char.Version > 0`**; at zero the `UPDATE` matches on id alone (§9.4.2, citing `internal/world/postgres/character_repo.go:82-85`). That affordance is correct at its own layer.
**How to avoid:** `requireGuardedVersion` at the entry point [VERIFIED: internal/grpc/characteraccess_write.go:189-199 — `if expectedVersion > 0 { return nil }`, else `codeCharacterVersionRequired`]. For Phase 5: `CreateCharacter` carries **no** `expected_version` field at all (§9.4.2 normative), and `SetDefaultCharacter` targets a `players` row so the rule does not reach it (D-89). **Neither new RPC calls `requireGuardedVersion` — and that is correct, not an omission.** Say so in the plan so a reviewer does not "fix" it.

### Pitfall 6: The shipped roster reads a different RPC than the one this phase must use

**What goes wrong:** the roster rewrite keeps `webListCharacters` and the new `status`/`version` fields never arrive; or it switches and the "Last played" line silently becomes undefined.
**Why it happens:** `/characters` today calls `client.webListCharacters({})` and renders `char.lastPlayedAt`, `char.lastLocation`, `char.hasActiveSession`, `char.sessionStatus` from `web.v1.CharacterSummary`. The Phase-4 owner roster is `webListMyCharacters` returning `OwnCharacter`, which carries **`id, name, description, profile, primary_image, gallery, status, version`** and *no* session or last-played telemetry — and `ListMyCharacters` deliberately passes `nil` for the profile map (it is the roster shape, not a missing profile).
**How to avoid:** decide the roster's data source explicitly. `status` (`active`/`retired`/`idle`) is what drives UI-SPEC's `Playable` / `Not playable` sectioning and the `Retired` badge — and it exists **only** on `OwnCharacter`. The session badge (`Active`/`Offline`) exists **only** on `CharacterSummary`. UI-SPEC's badge matrix needs **both**, so the roster needs either two calls or a decision to drop the session badge. **This is a genuine gap — see Open Question Q2.**
[VERIFIED: web/src/routes/(authed)/characters/+page.svelte:30-33, 136-146; api/proto/holomush/characteraccess/v1/characteraccess.proto:202-233; internal/grpc/characteraccess_owner.go:84-91 — `out = append(out, projectOwner(char, nil))`]

### Pitfall 7: The short-field cap is 100 **bytes**, not 100 characters

**What goes wrong:** a CJK player types ~33 characters into `profile.species`, the client's `.length` counter reads 33/100, and the server rejects at 99 bytes.
**Why it happens:** the caps reuse the shipped world constants, measured in bytes:

```go
// Source: internal/world/validation.go:19-21
const (
	MaxNameLength        = 100
	MaxDescriptionLength = 4000
```

and the facade compares `len(value) > maxBytes` on a Go string, which is a byte length [VERIFIED: internal/grpc/characteraccess_write.go:217].
**How to avoid:** `new TextEncoder().encode(v).length` in the client counter, exactly as UI-SPEC's Accessibility table mandates. UI-SPEC marks this a **backstop** row needing a held-out test at 99/100/101 bytes with a multi-byte value.
**Warning signs:** any `.length` on a `profile.*` value in the web tree.

### Pitfall 8: Character-name length — the cap exists, in **runes**, and it is a different unit again

```go
// Source: internal/charname/syntax/syntax.go:37-42
// Character-name rune-count bounds. These are RUNE counts, not byte lengths,
// so a Cyrillic or accented name is measured the way a player perceives it.
const (
	MinNameLength = 2
	MaxNameLength = 32
)
```

Aliased into the world package as `MinCharacterNameLength` / `MaxCharacterNameLength` [VERIFIED: internal/world/validation.go:33-34]. **This resolves sketch 009 open question 3 and CONTEXT's "A name length cap" deferred item.** Note the collision hazard: `world.MaxNameLength` is **100 bytes** (generic names) and `world.MaxCharacterNameLength` is **32 runes** (character names). They are different constants in the same package. The create form's `name` field is bounded by the second; the five `profile.*` short fields by the first.

### Pitfall 9: ABAC changes are not visible on the next read — and the test must drive `Reload()`

**What goes wrong:** an integration test mutates the policy corpus and sleeps, or asserts on the next call and flakes.
**Why it happens:** `policy.Cache` holds a compiled snapshot; the `Poller` refreshes it on `Interval`, defaulted where `cfg.Interval <= 0` to `10 * time.Second` [VERIFIED: internal/access/policy/poller.go:95-96 — `if cfg.Interval <= 0 { cfg.Interval = 10 * time.Second }`].
**How to avoid:** D-97's amendment, and in the test call `cache.Reload(ctx)` explicitly [VERIFIED: internal/access/policy/cache.go:141]. The existing harness already does exactly this: `newCorpusEngine` calls `Expect(cache.Reload(ctx)).To(Succeed())` [VERIFIED: test/integration/access/character_profile_read_test.go:126-127].
**Also:** UI-SPEC's Anti-Pattern Guard forbids any **copy or acceptance text** claiming a configuration change is visible on the "next load" or "immediately".

### Pitfall 10: `oops` code assertions do not prove what the wire carried

**What goes wrong:** an opacity test asserts an oops code and passes against an implementation that leaks on the wire.
**Why it happens:** under the pinned `samber/oops v1.22.0`, `OopsError.Code()` returns *the deepest* code in the chain, so both `errutil.AssertErrorCode` and the `oops.AsOops(err)` + `.Code()` form resolve the same value and both pass on a double-wrap [CITED: 01-SPEC.md §9.6.1, verified empirically 2026-08-01; tracked as #4902].
**How to avoid:** assert `status.Code(err)` and `status.Convert(err).Message()`, and assert the internal code string does **not** appear in that message. For `CHARACTER_PROFILE_NOT_FOUND` the mandated form is the **differential** one: drive an unreachable profile and a nonexistent id through the same RPC and assert the two responses are identical across status, message and body. `.claude/rules/grpc-errors.md` still recommends the broken spelling; **CONTEXT flags it as known-wrong** and §9.6.1 supersedes it.

### Pitfall 11: The generic not-found message is a shared literal, and the uniformity is the mechanism

```go
// Source: internal/grpc/characteraccess_service.go:143
const characterProfileNotFoundMessage = "character profile not found"
```

All four `GetCharacterProfile` failure legs return `status.Error(codes.NotFound, characterProfileNotFoundMessage)` — malformed id (`:391`), unrecognized rung (`:401`), unreachable (`:409`) [VERIFIED: internal/grpc/characteraccess_service.go:385-410]. A new leg with its own message breaks §8.7 silently.

### Pitfall 12: Constructor churn — 8 call sites, all positional

`NewCharacterAccessServer` takes 8 positional args and is called from **8 places**: `cmd/holomush/sub_grpc.go:861`; `test/integration/access/character_directory_test.go:64`, `character_profile_read_test.go:167`, `character_write_test.go:87`; `internal/grpc/characteraccess_profile_test.go:216` and `:297`, `characteraccess_write_test.go:196`, `characteraccess_owner_test.go:137`. Adding a dependency touches all eight in one commit. Budget for it; do not discover it mid-wave.
[VERIFIED: `rg -n "NewCharacterAccessServer"` this session]

### Pitfall 13: Nothing runs the web unit tests

`web/package.json` declares `"test:unit": "vitest run"`, but **no Taskfile task and no CI workflow invokes it** [VERIFIED: Taskfile.yaml — no `test:unit`/`vitest`/`web:test` target; `.github/workflows/ci.yaml` — no vitest or svelte-check step; `task pr-prep:run` runs bats → schema → luabridge → ebnf → license → plugins → lint → fmt:check → test → test:int → test:e2e, Taskfile.yaml:1242-1281]. `svelte-check` is likewise never run. **Consequence:** any vitest test this phase writes is real coverage a human can run but is **not a gate**. The Validation Architecture below states this honestly rather than claiming a sampling rate that does not exist.

---

## Code Examples

### Reading an owned character's profile rows with the character's own subject

```go
// Source: internal/grpc/characteraccess_owner.go:139-166
func (s *CharacterAccessServer) ownedProfileAttributes(ctx context.Context, characterID ulid.ULID) (map[string]string, error) {
	caller := world.HumanCaller(access.CharacterSubject(characterID.String()))
	rows, err := s.world.ListPropertiesByParent(ctx, caller, characterParentType, characterID)
	if err != nil {
		errutil.LogErrorContext(ctx, "character access: owner profile property enumeration failed", err,
			"character_id", characterID.String())
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck
	}

	attrs := make(map[string]string, len(rows))
	for _, row := range rows {
		if row == nil || row.Value == nil {
			continue
		}
		if !isGovernedProfileName(row.Name) {
			continue
		}
		attrs[row.Name] = *row.Value
	}
	return attrs, nil
}
```

**No handler in `characteraccess_owner.go` may construct a `viewer:` principal** — the file's header comment states this as a hard rule (D-69's audience split, INV-ACCESS-15's other half).

### The domain profile write and its closed name set

```go
// Source: internal/world/service.go:1054-1084 (abridged)
func (s *Service) UpdateCharacterProfileAttributes(
	ctx context.Context, caller Caller, characterID ulid.ULID,
	expectedVersion int, attributes map[string]string,
) error {
	if expectedVersion <= 0 {
		return oops.Code("CHARACTER_VERSION_REQUIRED"). /* … */
			Errorf("a profile write requires a caller-supplied expected_version >= 1")
	}
	// …
	for name := range attributes {
		if _, declared := profileAttributeNames[name]; !declared {
			return oops.Code("CHARACTER_PROFILE_ATTRIBUTE_UNKNOWN"). /* … */
				Errorf("%q is not one of the twelve profile attributes 01-SPEC §7.2 declares", name)
		}
		names = append(names, name)
	}
	// … slices.Sort(names) …
	resource := access.CharacterResource(characterID.String())
	if err := s.checkAccess(ctx, caller, "write", resource, prefixCharacter); err != nil {
		return err
	}
```

**Read this before designing `CreateCharacter`:** the domain command refuses `expectedVersion <= 0`, and a freshly created character is at `version = 1` (migration `000049` `DEFAULT 1`, per §9.4.2). So a post-create profile write is legal with `expectedVersion = 1` — but it is a **second transaction**.

### The corpus-control harness the new criterion-4 spec extends

```go
// Source: test/integration/access/character_profile_read_test.go:66-93, 112-132 (abridged)
type profileCorpusStore struct {
	policystore.PolicyStore
	excluded map[string]bool
	appended []*policystore.StoredPolicy
	removed  int
}

func (s *profileCorpusStore) ListEnabled(ctx context.Context) ([]*policystore.StoredPolicy, error) { /* … */ }

func newCorpusEngine(ctx context.Context, excluded []string, appended ...*policystore.StoredPolicy) *policy.Engine {
	// …
	Expect(len(excluded)+len(appended)).To(BeNumerically(">", 0),
		"a control corpus must differ from the seeded one in at least one direction …")
	corpus := &profileCorpusStore{PolicyStore: env.pStore, excluded: exSet, appended: appended}
	cache := policy.NewCache(corpus, env.compiler)
	Expect(cache.Reload(ctx)).To(Succeed())
	Expect(corpus.removed).To(Equal(len(excluded)), /* disarmed-control refusal */)
	return policy.NewEngine(env.resolver, cache, &noopSessionResolver{}, env.auditLogger)
}
```

**The one extension criterion 4 needs:** `newCorpusEngine` discards the `*policy.Cache`, so the caller cannot reload it a second time. The delta is a sibling that returns `(engine, cache, corpus)` (or that the existing one grows a returned cache), so the spec can mutate `corpus.excluded`/`corpus.appended` and call `cache.Reload(ctx)` **on the same engine the same `srv` already holds**. That is an extension of a shipped helper, not a new harness — which is what rule `7zy1161fh1` asks for.

### Inserting a real property row (both new specs use this)

```go
// Source: test/integration/access/character_profile_read_test.go:197-203
insertProperty := func(ctx context.Context, parentID ulid.ULID, name, value, visibility string) {
	_, err := env.pool.Exec(ctx, `
		INSERT INTO entity_properties (id, parent_type, parent_id, name, value, visibility)
		VALUES ($1, 'character', $2, $3, $4, $5)`,
		core.NewULID().String(), parentID.String(), name, value, visibility)
	Expect(err).NotTo(HaveOccurred())
}
```

Existing call sites pass `"public"` (and once `"system"`) as `visibility` [VERIFIED: same file, lines 427, 455, 513]. `"public"` is what makes the row-keyed term B permit a viewer.

### The eleven media names, verbatim, in emission order

```go
// Source: internal/grpc/characteraccess_projection.go:14, 27-38
const profileImagePrimaryName = "profile.image.primary"

var profileGallerySlotNames = [...]string{
	"profile.image.gallery.00",
	"profile.image.gallery.01",
	"profile.image.gallery.02",
	"profile.image.gallery.03",
	"profile.image.gallery.04",
	"profile.image.gallery.05",
	"profile.image.gallery.06",
	"profile.image.gallery.07",
	"profile.image.gallery.08",
	"profile.image.gallery.09",
}
```

These are the exact bytes EXT-05's test inserts. §7.3: *"`profile.image.gallery.0` and `profile.image.gallery.00` are two different rows that both coexist happily"*.

### The twelve mask paths, verbatim, with their caps

```go
// Source: internal/grpc/characteraccess_write.go:118-155 (keys and caps only)
"profile.pronouns":       world.MaxNameLength         // 100 bytes
"profile.concept":        world.MaxNameLength
"profile.species":        world.MaxNameLength
"profile.age":            world.MaxNameLength
"profile.faction":        world.MaxNameLength
"profile.currently":      world.MaxNameLength
"profile.timezone":       world.MaxNameLength
"profile.appearance":     world.MaxDescriptionLength  // 4000 bytes
"profile.personality":    world.MaxDescriptionLength
"profile.biography":      world.MaxDescriptionLength
"profile.rumors":         world.MaxDescriptionLength
"profile.rp_preferences": world.MaxDescriptionLength
```

UI-SPEC's five authoring sections partition exactly these twelve; the mask each Save sends must be a subset of this map, and an unlisted path is **rejected**, never ignored.

### Where the anonymous rung comes from

```go
// Source: internal/grpc/characteraccess_service.go:283-296 (abridged)
func (s *CharacterAccessServer) resolveViewerIdentity(ctx context.Context, rawToken string) (viewerIdentity, error) {
	anonymous := viewerIdentity{tier: access.ViewerTierAnonymous}

	if rawToken == "" {
		return anonymous, nil
	}
	ps, err := resolvePlayerSessionWithRepo(ctx, s.playerSessionRepo, rawToken)
	if err != nil {
		slog.DebugContext(ctx, "character access: session token did not resolve; reading as anonymous", "error", err)
		return anonymous, nil
	}
	// … playerRepo.GetByID failure is the ONE non-nil error (an outage is not a rung) …
```

**The gateway sends an empty `player_session_token` when the header is absent, and that is the ordinary logged-out case, not an error** [VERIFIED: internal/web/character_handlers.go:18-27]. So `/c/[id]` needs **no special client handling** for the anonymous case: the same `webGetCharacterProfile` call works with or without a cookie.

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `WebListAllCharacters` re-exporting the core roster verbatim | `WebListCharacterDirectory` → `PublicCharacterSummary` (id + name only) | Phase 4, plan 04-07 | Its absence is asserted at the descriptor level [VERIFIED: test/meta/character_rpc_census_test.go:221, 343-354]. Do not resurrect it. |
| Character roster via `webListCharacters` / `core.CharacterSummary` | Owner roster via `webListMyCharacters` / `OwnCharacter` (adds `status`, `version`; drops session telemetry) | Phase 4 | The `/characters` page has not been migrated — Phase 5 does it (Pitfall 6, Q2). |
| `errutil.AssertErrorCode` as evidence of what the wire carried | Wire-level assertion: `status.Code` + `status.Convert(...).Message()` + no-code-in-message | Phase 1, §9.6.1 (#4902) | `.claude/rules/grpc-errors.md` still carries the superseded prescription. |
| Any `profile.*` name unassigned a floor defaults to `guest` | An unenumerated name is **denied, not defaulted** | §8.6 totality rule | An eleventh gallery slot, or a new field without an §8.6 row, is invisible to every viewer — that is the intended behavior. |
| A third `player`-rung tier-floor policy (D-03 said three) | **Two** policies ship: `seed:profile-tier-floor-anonymous` and `…-guest` | §8.6, §14 row 17 (2026-08-05) | Under seeded defaults `guest` and `player` render **identically**. Any tier preview must derive from the live floor set. |

**Deprecated / stale in the corpus:**

- Sketch 008's `href="/c/{c.name}"` — the key is the **id** (§9.2, UI-SPEC).
- Sketch 008/009's *"v0.13 ships rename"* framing — rename left v0.13 on 2026-08-06 (Phase 3 D-44 → backlog 999.20).
- `.claude/rules/references/plan-review-learnings.md`'s claim that `task test:int` does not accept `--` package args — **false**; both `test` and `test:int` interpolate `{{.CLI_ARGS | default "./..."}}` [VERIFIED: Taskfile.yaml:189, 288-289]. That file's own header already flags itself as drift-prone (rule `4th2295d3j`).

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `CreateCharacter` writing the five profile fields in a **second** transaction (character first, rows after) is acceptable, with a partial-failure state the owner can repair by editing | Open Question Q1 | A create that half-lands leaves a character with no pronouns — recoverable, but it contradicts §9.3's *"Every mutation emits through the transactional outbox in the same transaction as its state change"* if read as covering the whole card. **Needs user/planner confirmation before any create code is written.** |
| A2 | The roster keeps its session `Active`/`Offline` badge, which requires a second call (`webListCharacters`) alongside `webListMyCharacters` | Q2 / Pitfall 6 | If wrong, UI-SPEC's badge matrix row 1–2 cannot be built as specified. |
| A3 | `CHARACTER_LIMIT_REACHED` and `CHARACTER_NO_STARTING_LOCATION` map to `FailedPrecondition` with their own player-facing copy | Q3 / Pitfall 4 | §9.6's eight-code table has no row for either; an arbitrary choice becomes a wire contract nobody reviewed. |
| A4 | `SetDefaultCharacter` requires no new ABAC action — session resolution + ownership is the whole gate | §"SetDefaultCharacter" | D-89 says "gated on session resolution + ownership" and §9.3 has no row for it, so there is no ABAC action named for it anywhere. If a reviewer expects an ABAC decision, the plan owes one. |
| A5 | `SetDefaultCharacter` on a **retired** character should be refused | Q4 | The retire path *clears* the pointer in the same transaction (`character_repo.go:536-545`); allowing a set-to-retired would immediately contradict that. Nothing in the SPEC states the rule. |
| A6 | The public profile's `<title>` / OG metadata is out of scope | — | #4905 defers the game display name, and INV-6 forbids hardcoding `HoloMUSH` in player-facing copy. If SEO/sharing metadata is expected, it has nowhere to get a name from. |
| A7 | `WebCreateCharacter` moving from `internal/web/auth_handlers.go` to `character_handlers.go` is correct | Pitfall 2 | The routing census's web universe is scoped by *"references the character facade client field"*, so the file it lives in does not matter to the gate — but leaving it in `auth_handlers.go` beside a now-unrelated `h.client.CreateCharacter` reads as a bug. Cosmetic. |
| A8 | Roster ordering within `Playable` is registry order (what ships today) | UI-SPEC unresolved row | Sketch 008 open question 4; UI-SPEC explicitly hands this to the planner as an assumption. |

---

## Open Questions (RESOLVED)

> All six were pinned during planning. Each `RESOLVED:` line below names the pinned answer and
> the plan that pins it; a later reader must not mistake a settled question for an open one.
> Where the plan **deviates** from the recommendation, that is stated rather than smoothed over.

1. **How does `CreateCharacter` write the five profile fields, and in how many transactions?**
   - *What we know:* creation is `CharacterService.CreateBound` → `CharacterGenesisService.Create`, which commits **character + binding + genesis envelope atomically** [VERIFIED: internal/auth/character_service.go:214-227; internal/auth/character_genesis.go:172]. The five profile fields are `entity_properties` rows written by `world.Service.UpdateCharacterProfileAttributes`, which refuses `expectedVersion <= 0` and makes its **own** ABAC decision on `character:<id>` [VERIFIED: internal/world/service.go:1054-1093]. A fresh row is at `version = 1`, so a follow-up call with `expectedVersion = 1` is legal.
   - *What's unclear:* §9.3 states *"Every mutation emits through the transactional outbox in the same transaction as its state change"* — is "the mutation" the character create, or the whole identity card? No shipped code writes both in one transaction, and extending `CharacterGenesisService` to do so widens a path that carries INV-WORLD-1.
   - *Recommendation:* **Two transactions, stated explicitly, with the create RPC authoritative.** Create the character first; write the profile rows second; on a profile-write failure return **success with the character** (the create landed and is the durable fact) and let the owner fill the fields in on `/characters/[id]`. This matches the `createFlow.ts` idiom, matches §9.3's per-mutation reading (the character create is one mutation and it *is* transactional with its envelope), and avoids a genesis-service change on a phase already carrying a proto reshape and two RPCs. **The planner MUST record this as a decision and the plan MUST say what happens on the second write's failure** — silently swallowing it and reporting success is defensible only if it is written down.
   - **RESOLVED (plan 05-03, § "Pinned decisions this plan settles" → Q1; ratified by that plan's blocking `checkpoint:decision`, option-a):** two transactions, the create authoritative. On a second-write failure the RPC returns **success** with the created `OwnCharacter`, the un-set keys simply absent from its `profile` map, and logs the failure with the character id and the attempted mask paths. Carried as truth 6 of 05-03's `must_haves`, and accepted explicitly as threat `T-05-03-09` (Repudiation / partial identity card, disposition `accept`). Option-c (report failure) was rejected because the resubmit collides with the name the player just reserved. **Plan 05-06 closes the remaining player-visible half:** `createFlow` compares the `profile.*` keys it submitted against those present in the response and, on a mismatch, carries a second `createdNotice` variant that names the character page as the place to add them — the create still returns success, so option-c's hazard is not reintroduced.

2. **Where does the roster's session badge come from after the migration?**
   - *What we know:* `Active`/`Offline` today comes from `CharacterSummary.hasActiveSession`/`.sessionStatus` via `webListCharacters`. `OwnCharacter` carries neither. UI-SPEC's badge matrix needs `status` (only on `OwnCharacter`) **and** the session badge (only on `CharacterSummary`).
   - *What's unclear:* whether two calls are acceptable, or whether the session badge is dropped.
   - *Recommendation:* **two calls on `/characters`** — `webListMyCharacters` for the sectioning/lifecycle truth, `webListCharacters` for the session overlay, joined on character id. It adds one round trip on one page and preserves both UI-SPEC rows. Adding session telemetry to `OwnCharacter` is the wrong direction: `PublicCharacterSummary`'s doc records that removing presence telemetry from the directory was deliberate and *structural*.
   - **RESOLVED (plan 05-01, § "Pinned decisions this plan settles" → Q2, "PINNED, not deferred"):** two calls on `/characters`, joined on the character id, exactly as recommended. Its **corollary** — that `OwnCharacter` carries no is-default field, so the roster alone cannot say which character is default — is pinned in the same section: the initial `Default` marker comes from `default_character_id` on `WebCheckSessionResponse`, propagated through the `(authed)` layout's **existing** `webCheckSession` call, adding no round trip. Implemented by plan 05-07 Task 3 (`listMyCharacters` + `webListCharacters`, issued concurrently; a session-call-only failure renders the roster without session badges rather than failing the page).

3. **What wire status do `CHARACTER_LIMIT_REACHED` and `CHARACTER_NO_STARTING_LOCATION` take?**
   - *What we know:* both are real create-path outcomes with no §9.6 row.
   - *Recommendation:* `FailedPrecondition` for both, with distinct player-facing copy (`"You've reached the maximum number of characters."` / the second is an operator misconfiguration and should read as a generic failure). Record it as a §9.6 amendment owed, in the same register as the two already owed.
   - **RESOLVED (plan 05-03, § "Pinned decisions this plan settles" → Q3, PINNED):** `codes.FailedPrecondition` for both, as recommended — neither is an invalid argument (the name was fine) and neither is an outage worth a blind retry. Asserted by 05-03 Task 1's mapping-table behaviour and criteria. The §9.6 amendment is **assigned**: plan 05-05 Task 3 files it as item 4 of the owed-amendments GitHub issue, alongside `CHARACTER_NOT_PLAYABLE` from plan 05-01.

4. **May a retired character be made default?**
   - *Recommendation:* refuse, with the ownership-generic message. The retire path clears the pointer in the same transaction as the status write [VERIFIED: internal/world/postgres/character_repo.go:536-545], so permitting it creates a state the retire path is written to prevent. Cheap to enforce: `ownedCharacter` already returns the `*world.Character` with its `Status`.
   - **RESOLVED (plan 05-01, § "Pinned decisions this plan settles" → Q4, PINNED):** refuse — as recommended in substance. **Deviation on the message, recorded in the plan rather than smoothed over:** the refusal is `codes.FailedPrecondition` with its own literal (`characterNotPlayableMessage`), **not** the ownership-generic message this recommendation proposed, because ownership has already been proven at that point and returning "no such character on your roster" for a card the player is looking at reads as a bug while buying no opacity — existence was never in question. Carried as truth 6 of 05-01's `must_haves`, asserted with a paired active-character positive control, and mitigated as threat `T-05-01-05`.

5. **Does `SetDefaultCharacter`'s response really need the full roster (D-90), given the roster may need two calls (Q2)?**
   - *Recommendation:* yes — D-90 is locked and the census verdict is pinned to it. If Q2 lands on two calls, the client refreshes the session overlay separately; the `Default` badge move comes from the `SetDefaultCharacter` response, which is what D-90 is for.
   - **RESOLVED (plan 05-01):** yes, as recommended — the response keeps the `ListMyCharactersResponse` shape per D-90, built through the same projection rather than a second copy, and its reversibility is rated `costly` precisely because the §2.6 census pins that shape with an `owner` audience verdict. Q2's two calls do not disturb it: the `Default` badge move comes from the `SetDefaultCharacter` response, and the session overlay refreshes separately (plan 05-07 Task 3).

6. **Is the `WebCreateCharacter` census row moved or duplicated?**
   - *Recommendation:* **moved** out of `characterNameReachableRPCs()`. Leaving it there while its response is also type-reachable keeps the set-equality green (both paths add the same key) but leaves a **self-certifying** entry whose own doc says it exists only for surfaces a type predicate cannot reach. That is exactly the "invisible coverage" the enumeration's exactness test warns about.
   - **RESOLVED (plan 05-03, § "Pinned decisions this plan settles" → Q6):** **moved**, as recommended. Plan 05-03 Task 2 removes `holomush.web.v1.WebService.WebCreateCharacter` from `characterNameReachableRPCs()` and adds it to `characterReadSurfaceInventory()`'s §2.4/§9.2 web-pair block, in the same commit as the reshape that makes its response type-reachable. `holomush.core.v1.CoreService.CreateCharacter` **stays** name-reachable — it still returns a bare scalar and the telnet `CREATE` verb still drives it.

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | everything server-side | ✓ | `go1.26.5 darwin/arm64` (matches `go.mod`'s `go 1.26.5`) | — |
| `go-task` | every build/test/lint verb | ✓ | `3.52.0` | — |
| Docker (daemon running) | `task test:int` (testcontainers), `task test:e2e` | ✓ | `29.7.2`, daemon responding | none — both new integration specs and the E2E require it |
| `pnpm` | `task web:install`, `web:build`, vitest | ✓ | `11.21.0` | — |
| `buf` | `task proto`, `task web:generate` | ✓ | `1.72.0` | none — the proto change cannot land without it |
| Node | vitest / Playwright host | ✓ | `v26.7.0` | — |
| Postgres testcontainer | integration specs | ✓ (via Docker) | pulled at test time | — |
| Embedded NATS (`eventbustest`) | full-stack integration | ✓ (in-process) | — | — |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** none.

---

## Validation Architecture

> `workflow.nyquist_validation` is `true` in `.planning/config.json` [VERIFIED: .planning/config.json — `"nyquist_validation": true`]. `tdd_mode` is also `true`, so RED/GREEN/REFACTOR gates apply per plan task.

### Test Framework

| Property | Value |
|----------|-------|
| Framework (Go unit) | `go test` via `gotestsum`, `testify` assertions, ACE naming |
| Framework (Go integration) | Ginkgo/Gomega, build tag `//go:build integration` |
| Framework (web unit/component) | `vitest 4.1.10` + `jsdom 29.1.1`, two projects (`server` = `*.test.ts`, `client` = `*.svelte.test.ts`); components mounted with Svelte's own `mount`/`unmount` — **no `@testing-library/svelte`** |
| Framework (E2E) | Playwright `1.61.1`, Docker compose stack |
| Config file (Go) | none — `Taskfile.yaml:185-200`, `265-289` |
| Config file (web) | `web/vite.config.ts:6-34` (vitest projects), `web/src/test-setup.ts` |
| Quick run command (Go, scoped) | `task test -- ./internal/grpc/ ./test/meta/` |
| Quick run command (web) | `cd web && pnpm test:unit` — **not gated by any task or CI job** (Pitfall 13) |
| Full suite command | `task pr-prep` (bats → schema → luabridge → ebnf → license → plugin build → lint → fmt:check → test → test:int → test:e2e) |

`task test` and `task test:int` both interpolate `{{.CLI_ARGS | default "./..."}}`, so `--` package scoping works on both [VERIFIED: Taskfile.yaml:189, 288-289]. `task test:cover` does **not** interpolate CLI_ARGS — it always runs whole-repo [VERIFIED: Taskfile.yaml:250-258].

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| IDENT-01 | `CreateCharacter` proto declared, census rows present | meta (set equality) | `task test -- ./test/meta/` | ✅ (rows are the edit) |
| IDENT-01 | `CreateCharacter` reaches the shared guest gate; is **not** an ownership member | meta (routing census) | `task test -- ./test/meta/` | ✅ |
| IDENT-01 | Name-rejection codes map to `CHARACTER_NAME_INVALID` / `CHARACTER_NAME_TAKEN` on the **wire** | unit | `task test -- -run TestCreateCharacter ./internal/grpc/` | ❌ Wave 2 |
| IDENT-01 | Create returns the display name the server stored (the D-88 echo) | unit | same | ❌ Wave 2 |
| IDENT-01 | A rejected submit preserves every entered field | web component | `cd web && pnpm test:unit` | ❌ Wave 3 — **not a gate** |
| IDENT-01 | Structured creation end to end | E2E | `task test:e2e -- e2e/characters-create.spec.ts` | ❌ Wave 4 |
| IDENT-05 | `SetDefaultCharacter` sets `players.default_character_id` and returns the roster | integration | `task test:int -- ./internal/auth/postgres/` + `./test/integration/access/` | ❌ Wave 2 |
| IDENT-05 | Guest denied; non-owner takes the uniform not-owned outcome (**paired positive control**) | unit | `task test -- -run TestSetDefaultCharacter ./internal/grpc/` | ❌ Wave 2 |
| PROFILE-01 | Anonymous rung resolves and the public projection is returned | integration | `task test:int -- ./test/integration/access/` | ✅ `character_profile_read_test.go` |
| PROFILE-01 | A logged-out browser loads `/c/<id>` and sees name + pronouns + description | E2E | `task test:e2e -- e2e/public-profile.spec.ts` | ❌ Wave 4 |
| PROFILE-01 | Blank field absent from marshaled bytes (PORTAL-10 rule 3) | integration | existing sentinel spec | ✅ (`integrationBiographySentinel`) |
| PROFILE-02 / EXT-08 | Named empty sheet + web-DM slot render as non-interactive labelled slots | web component | `pnpm test:unit` | ❌ Wave 3 — **not a gate** |
| PROFILE-06/07/08/09 | The four fields round-trip through the mask | unit | `task test -- ./internal/grpc/` | ✅ `characteraccess_write_test.go` |
| PROFILE-08 | `profile.rp_preferences` is never written to `characters.preferences` | — | structurally true: the mask maps to `entity_properties` names only; no code path reaches the JSONB column | ✅ by construction |
| PROFILE-10a | `characters.description` renders on the public profile | integration | existing | ✅ |
| PROFILE-12 | Notice copy present on `/characters/[id]` | E2E | `task test:e2e` | ❌ Wave 4 |
| **Criterion 4** | Mutating the corpus + `Reload()` changes what the **same anonymous viewer** sees, with **no write** to the character or its rows | **integration (NEW #1)** | `task test:int -- ./test/integration/access/` | ❌ Wave 2 |
| **EXT-05 / Criterion 5** | 1 primary + 10 gallery rows insert through the real schema and read back **through the viewer-filtered path** | **integration (NEW #2)** | `task test:int -- ./test/integration/access/` | ❌ Wave 2 |
| EXT-05 | An 11th primary is rejected by `UNIQUE(parent_type,parent_id,name)` | — | **CITE, do not reprove** | ✅ `property_repo_test.go:430` |

### Existing coverage that MUST be cited, not reproved (D-98, re-verified this session)

| Clause | Proven by | Verified |
|---|---|---|
| Clearing a floor is set membership, never ordinal | `TestNoTierFloorPolicyUsesAnOrdinalTierComparison` | seed_profile_visibility_test.go:260 ✓ |
| A synthetic 4th rung clears neither shipped floor | `TestASyntheticFourthRungClearsNeitherShippedTierFloor` | tierfloor_test.go:173 ✓ |
| The floor is evaluated per read, twice per attribute, separated by the action token | `TestAttributeVisibleIssuesExactlyTwoEvaluationsSeparatedByTheActionToken` | profilevis_test.go:113 ✓ |
| `UNIQUE(parent_type,parent_id,name)` rejects a duplicate (`PROPERTY_DUPLICATE_NAME`) | `TestPropertyRepository_ParentNameUniqueness` | property_repo_test.go:430-450 ✓ |
| The policy enumerates exactly eleven media names, not twelve | `TestTheElevenMediaNamesAreEnumeratedAndTheTwelfthIsNot` | seed_profile_visibility_test.go:393 ✓ |
| Unreachable profile is byte-identical to nonexistent | `INV-PRIVACY-9`, `binding: bound` | invariants.yaml:2164-2172 → character_profile_read_test.go ✓ |
| Never stamped onto a row | structural — `entity_properties` carries no tier column and no migration writes one | ✓ |

### The two new integration specs — concrete shape

**NEW #1 (criterion 4 / read-time evaluation).** Extend `test/integration/access/`'s existing corpus harness so a spec can hold the `*policy.Cache` and the `*profileCorpusStore` and reload them:

1. Build the server once over a mutable corpus engine (`newServer(engine)` from `character_profile_read_test.go:161-185`).
2. Seed one character with a field at the `guest` rung (e.g. `profile.concept`, `visibility='public'`).
3. Read as the **anonymous** viewer (`token = ""`), capture field set **A**, and snapshot `characters.version` + the `entity_properties` row's `id`/`value`.
4. Mutate the corpus (append a tier-floor policy placing that name at `anonymous`, or exclude the guest-rung policy), then `cache.Reload(ctx)`.
5. Read again as the **same** anonymous viewer, assert field set **B ≠ A**.
6. **The discriminating assertions:** `characters.version` is unchanged, and no `entity_properties` row was written (compare `updated_at`/`value` by direct SQL). Without step 6 the spec degenerates into "the anonymous read omits field X", which passes whether or not the floor is read-time.
7. **PORTAL-10 rule 2 pairing:** the same reader must be shown *permitted* under one corpus and *withheld* under the other — that pairing is built into the A/B shape.

**Binding opportunity:** this is the assertion that would flip **`INV-ACCESS-10`** from `binding: pending` to `bound` — its summary is *"The viewer-tier floor governing a profile attribute is evaluated at READ TIME … never stamped onto entity_properties.visibility per row"* [VERIFIED: docs/architecture/invariants.yaml:2305-2311]. Its second clause (*"an infrastructure failure in that evaluation resolves DENY"*) is already covered by `TestVisibleAttributesAbortsTheWholeCallWhenAnyEvaluationFails` (profilevis_test.go:344). **The planner MUST verify the new spec genuinely proves the whole statement before adding a `// Verifies:` — a partial binding is a false green `TestBoundInvariantsAreGenuinelyAsserted` cannot detect** [CITED: .claude/rules/invariants.md].

**NEW #2 (EXT-05 / media schema).** A separate spec (D-98: *"Keep them two tests, not one"*):

1. Insert `profile.image.primary` + `profile.image.gallery.00` … `.09` — **eleven rows**, exact bytes from `profileGallerySlotNames`, `visibility='public'`, each `value` a distinct sentinel media handle.
2. Read the profile as a **guest-rung** viewer (media names are seeded at `guest`, §8.6) — the existing fixture already builds a guest player and a live session token.
3. Assert `PrimaryImage.MediaId` equals the primary sentinel and `Gallery` has **exactly 10** entries **in slot order** `00…09` (the emission order is the slice, not map order).
4. Assert an **anonymous** viewer receives neither (the paired control that proves the guest rung is doing work).
5. **Cite** `property_repo_test.go:430` for the 11th-primary rejection; **do not** insert a second primary here.
6. Optionally assert `profile.image.gallery.10` is **absent** from the response even when the row exists — the §8.6 totality rule's "denied, not defaulted" behaviour, and a genuinely non-obvious property.

### Sampling Rate

- **Per task commit:** `task test -- <touched packages>` plus `task lint`. Both are cheap and are the CLAUDE.md floor.
- **Per wave merge:** `task test` (whole repo, `-race`) and `task test:int -- ./test/integration/access/ ./internal/grpc/ ./internal/auth/postgres/`.
- **Phase gate:** `task pr-prep` green **inline in the parent**, then `/gsd-verify-work`. The E2E lane is inside `pr-prep`'s full body and is a required CI check.
- **Web unit tests:** run `cd web && pnpm test:unit` manually per web task. **State plainly in the plan that this is not a gate** — see Wave 0 gaps.

### Wave 0 Gaps

- [ ] **`newCorpusEngine` cannot be reloaded twice.** It discards the `*policy.Cache` and the `*profileCorpusStore` [VERIFIED: test/integration/access/character_profile_read_test.go:112-132]. Criterion 4's spec needs a sibling returning them, or the existing helper widened. Keep its two guards (the differs-in-one-direction refusal and the `removed` count) — they are what stop a disarmed control.
- [ ] **No Taskfile/CI hook for `pnpm test:unit` or `svelte-check`.** Two options: (a) accept it and mark every web unit test as manual-only in VALIDATION.md, or (b) add a `web:test` task + a `pr-prep` step. **Option (b) is a new gate over a surface no gate covers — it is not a duplicate — but it is also scope the phase did not ask for.** Recommend (a) for Phase 5, with a GitHub issue filed for (b).
- [ ] **No E2E spec exercises a genuinely logged-out visit to an authenticated app path.** `landing.spec.ts` visits `/` anonymously, so the *pattern* exists; the *combination* (create a character while logged in, then read its profile from a fresh context with no cookie) does not. `web/e2e/helpers/db.ts` already exposes `getCharacterByName`, `getCharactersByPlayerId` and `getPlayerByCharacterId`, and `fixtures.ts` exposes `registerAndEnterTerminal` — enough to build it with no new helper.
- [ ] **UI-SPEC backstop row 1 — the media renderer.** `populated + error | /c/[id] media`: primary replaces the portrait, `alt_text` becomes `alt`, non-empty `content_warning` blurs behind a reveal, zero rows render nothing, **and a failed `<img>` load falls back to the identical initial-letter placeholder**. Unreachable by running v0.13 (no row can exist), so it needs a **fixture-driven `*.svelte.test.ts`**.
- [ ] **UI-SPEC backstop row 2 — the byte counter.** A held-out test with a multi-byte value at 99 / 100 / 101 bytes proving the client counter and the server agree.
- [ ] **No Go unit-test fixture for `CreateCharacter`/`SetDefaultCharacter` on the facade.** `internal/grpc/characteraccess_write_test.go` and `characteraccess_owner_test.go` carry the fixtures; both will need the two new constructor arguments regardless.

---

## Security Domain

`security_enforcement` is `true`, `security_asvs_level` is `1` [VERIFIED: .planning/config.json].

### Applicable ASVS Categories

| ASVS Category | Applies | Standard control |
|---------------|---------|-----------------|
| V2 Authentication | yes | Session token resolved **server-side from the `X-Session-Token` header**, never from the request body — the routing census asserts it as one of two required conjuncts on every owner proxy. `/c/[id]` deliberately authenticates **nothing** and degrades to the least-privileged rung. |
| V3 Session Management | yes | Existing `CookieMiddleware` + `auth.PlayerSessionRepository`. Phase 5 adds no session surface. Note the anonymous read path treats an expired cookie as absent — fail-closed by construction (anonymous is least-privileged). |
| V4 Access Control | **yes — the phase's centre of gravity** | ABAC, default-deny. Owner: `resolveAndGate` (guest denial) → `ownedCharacter` (uniform NotFound). Public: `profilevis.Reachable` then per-attribute floors, conjunctive with the row-keyed decision. **A new RPC that reaches neither fails the routing census's audience partition.** §8.10: infrastructure failure resolves DENY, never a silently sparse profile. |
| V5 Input Validation | yes | `charname.Gate.Check` (NFKC → `Cf` strip → syntax → block list → mixed script → skeleton) for names; the closed 12-path mask allowlist + per-field byte caps for profile fields; `ulid.Parse` for ids, with a parse failure taking the same opaque outcome as a nonexistent row. |
| V6 Cryptography | no | Phase 5 touches no crypto surface. No `crypto.emits` change ⇒ `crypto-reviewer` does not fire. |
| V7 Error Handling & Logging | yes | Generic wire messages + `errutil.LogErrorContext` for the structured reason. The `CHARACTER_NAME_INVALID` verbatim-message carriage is a **scoped, deliberate exception** and must be implemented as a chosen player-facing string, never a `%v` of a wrapped error. |
| V14 Configuration | partial | The viewer-tier floors are game configuration; §8.12 ships **no** editing surface in v0.13 and the SPEC's reviewer MUST verify no PR adds one. |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard mitigation |
|---------|--------|---------------------|
| Character-existence enumeration via a distinguishable "private" response | Information disclosure | One `CHARACTER_PROFILE_NOT_FOUND` code for both causes; one shared message literal; the **differential** wire assertion (§9.6.1). |
| Name-enumeration oracle through the confusable message | Information disclosure | `gate.go:188-194` names no colliding character; UI-SPEC restates it so no client-side "helpful" enrichment reintroduces it. |
| Which-profiles-are-populated oracle via a conditional sign-in notice | Information disclosure | Permanently forbidden (D-85 / 007-C); the unconditional `TopBar` pair discharges the need. |
| Ownership enumeration via a distinguishable "exists but not yours" | Information disclosure | `ownedCharacter` resolves both against the caller's own list and returns one NotFound; the mutation surface collapses all three causes to one `PermissionDenied` literal. |
| Lost update on a concurrent profile edit | Tampering | `expected_version` CAS in the domain `UPDATE` predicate; boundary rejects absent/zero; the loser is **surfaced**, never retried. |
| **Lost update on the player row via a full-row `Update`** | Tampering | **Pitfall 3** — use a narrow `UPDATE players SET default_character_id`. |
| Structural write smuggled through the command parser | Elevation of privilege | `.claude/rules/gateway-boundary.md`: GUI ⇒ typed RPC; `sendCommand` is human/CLI only. |
| Client-side privacy filtering | Information disclosure | §8.9: enforcement by absence at the wire; PORTAL-10 rule 3 requires assertions against **marshaled bytes**, and explicitly says a Playwright DOM assertion does not satisfy it. |
| Anonymous ⌘K palette exposing `(authed)` destinations on a public page | Information disclosure (none realised) | Pre-existing; the `(authed)` redirect is the fail-safe working. Filed as **#4962** and explicitly **out of scope** for Phase 5 — the planner carries nothing. |

### PORTAL-10 (§12.1) compliance obligations this phase inherits

1. **Census with set equality** — two exist and both grow (Pitfall 2). The derived side comes from generated descriptors / the AST, never a second hand list.
2. **Paired positive control on every denial test** — every new denial spec needs its permitted twin on the same fixture. If a wave has **no** denial tests, the plan **MUST say so explicitly** rather than omitting the rule.
3. **Assertions against marshaled response bytes** — the media and absence assertions go against the serialized response with a distinctive sentinel, never a populated struct and never the DOM.
4. **Gates demonstrated RED against the pre-fix state** — both new integration specs, and every census row edit, must be observed failing and the observation recorded in the plan's SUMMARY.
5. **Wire-level assertion of every opacity and authorization contract** — `status.Code` + `status.Convert(...).Message()` + no-code-in-message (Pitfall 10).
6. **Invariant-scope discipline** — any new invariant allocates in `ACCESS` or `PRIVACY`, is **hand-registered** in `docs/architecture/invariants.yaml` (a `.planning/` SPEC is outside the orphan-check walk root), and ships `binding: pending` rather than fabricating a `// Verifies:`.

---

## Project Constraints (from CLAUDE.md)

Actionable directives binding this phase. The planner MUST verify compliance; these carry the same authority as locked decisions.

| Constraint | Source | Applies to Phase 5 as |
|---|---|---|
| `main` is protected; feature branch + PR + squash merge | CLAUDE.md §Protected Branch Policy | Already on `v013-phase-03`; **do not create, rename or switch branches** |
| **Tests before implementation** (TDD); `tdd_mode: true` | CLAUDE.md §TDD + config | RED/GREEN/REFACTOR per task |
| Specs live at `.planning/phases/<phase>/<NN>-SPEC.md`; RFC2119 keywords | CLAUDE.md §Spec-Driven Development | `01-SPEC.md` is normative for this phase; no new spec file |
| **MUST use `task`** for build/test/lint/format; never bare `go test`/`golangci-lint` | CLAUDE.md §Commands | every verification command |
| **MUST delegate verbose task runs** to `local-check`; the FINAL `task pr-prep` runs inline in the parent | CLAUDE.md §Commands | plan task shape |
| `task fmt` **mutates files** — commit its edits; run it after touching any aligned Go `const`/`var`/`struct` block | CLAUDE.md §Commands | the census literals and the new const blocks are aligned blocks |
| Regenerate + commit `pkg/proto/**/*.pb.go` and web `*_pb.ts` in the **same change**; `task lint:proto` green | CLAUDE.md §Generated code | the proto change |
| **Every proto element needs a Go-grounded leading doc comment**, no name-echo; enforced unconditionally by buf `COMMENTS` + `test/meta/proto_doc_comments_test.go` | `.claude/rules/proto-doc-comments.md` | 2 RPCs + 4 messages + ~10 fields all need real comments grounded in `internal/grpc` |
| `crypto/rand` only; `idgen.New()` for entity PKs, `core.NewULID()` for event/session ids | CLAUDE.md §RNG/ULID | any new id minting |
| `oops` for structured errors; `errutil.LogError*`; **call accessors with `()`** | CLAUDE.md §Error Handling | every new error site |
| **MUST use `*Context` slog variants** whenever a `ctx` is in scope; enforced by `sloglint` `context: scope` | `.claude/rules/logging.md` | every new log line |
| Never leak inner errors past a trust boundary; translate at **one** layer | `.claude/rules/grpc-errors.md` | the create error mapping (Pitfall 4) |
| AttributeProviders **omit** optional attributes, never sentinel | `.claude/rules/abac-providers.md` | only if a provider is touched (it should not be) |
| Terminology: **character**, **player**, **session**, **location**. Never "room", "user", "avatar" | `.claude/rules/terminology.md` | all copy, all identifiers |
| Branding INV-6: the brand is the **software**, never the game world; no hardcoded `HoloMUSH` in player-facing copy; amber (`#ffb300`) is the **cursor only** | `.claude/rules/branding.md` + UI-SPEC | `/c/[id]` copy and palette |
| SPDX header on every new `.go` / `.sh` / `.proto`; applied by `task fmt`, verified by `task license:check` | CLAUDE.md §License Headers | every new Go file |
| ACE test naming; table-driven; `require` for preconditions, `assert` for the check | `.claude/rules/testing.md` | every new test |
| `task test` does NOT compile integration files — `task test:int` is mandatory on any shared-type refactor | CLAUDE.md §Testing | the constructor change touches 3 integration files |
| Production code MUST NOT import `eventbustest` / `coretest` / `natstest` / `quarantinetest` (depguard) | `.claude/rules/testing.md` | — |
| Structural writes use typed RPCs, not the command path | `.claude/rules/gateway-boundary.md` | every GUI form and button |
| Gateway is protocol translation only — no DB, no repos, no business logic | `.claude/rules/gateway-boundary.md` | both new proxies |
| Invariants: allocate in an existing scope; **hand-register** `.planning/`-origin ids; never fabricate a `// Verifies:` | `.claude/rules/invariants.md` | the INV-ACCESS-10 binding opportunity |
| `dev-flow:grepping` skill loaded before the first response; `mcp__probe__*` → `rg` → `ast-grep`; **never bare `grep`** | `.claude/rules/search-tools.md` | every sub-agent brief |
| Judge command success by **exit code**, never by grepping stdout | `.claude/rules/search-tools.md` | `pr-prep` result reading |
| **NEVER use `[ci skip]`** on any commit on a branch with an open PR | `.claude/rules/landing-the-plane.md` | ship step — decline GSD's default if it suggests one |
| `gh` from a worktree: always pass `-R holomush/holomush` | CLAUDE.md §Session isolation | any issue filing |
| Line-scoped `//nolint:<rule>` only — never widen `.golangci.yaml` | CLAUDE.md §Lint | the new handler boundaries |

---

## Roadmap / SPEC Amendments Owed

Recorded here (rule `a32nfcekfc` forbids hand-editing `.planning/ROADMAP.md`, and no `gsd-tools` verb rewrites an existing phase's success criteria). Carried forward from CONTEXT, plus two this research adds.

1. **Criterion 4** — "next load" → "next load after the policy cache reloads (poller interval, default 10s)" (D-97).
2. **Criterion 4 / PROFILE-12** — strike "both the retirement flow and"; the retirement half moves to Phase 6 (D-91). REQUIREMENTS' PROFILE-12 row needs the same note.
3. **NEW — §9.3's mutation table has no `SetDefaultCharacter` row.** D-89 acknowledges this and §9.1 authorises adding the RPC, but the table is the SPEC's own inventory of the mutation surface. A row is owed.
4. **NEW — §9.6's eight-code table has no row for `CHARACTER_LIMIT_REACHED` or `CHARACTER_NO_STARTING_LOCATION`**, both reachable from the create path (Q3).

---

## Sources

### Primary (HIGH confidence — read in-tree this session)

- `api/proto/holomush/characteraccess/v1/characteraccess.proto` (:1-130, :196-367) — the six shipped RPCs, `PublicCharacter`, `OwnCharacter`, the twelve prose fields, `update_mask` at field 99
- `internal/grpc/characteraccess_write.go` (:1-200, :200-360) — codes, the 12-path allowlist and caps, `requireGuardedVersion`, `ownedCharacterForMutation`, `UpdateCharacterProfile`'s normative gate order, `ownerMutationResponse`
- `internal/grpc/characteraccess_owner.go` (whole) — the audience split, the no-`viewer:`-principal rule, `ownedProfileAttributes`
- `internal/grpc/characteraccess_projection.go` (whole) — `projectPublic` / `projectOwner` / `projectPublicSummary`, the eleven media names, `isGovernedProfileName`
- `internal/grpc/characteraccess_service.go` (:143-157, :172-296, :372-425) — struct, constructor, message literals, `resolveViewerIdentity`, `GetCharacterProfile`
- `internal/grpc/player_gate.go` (:36-126) — `playerGate`, `ownedCharacter`, `resolveAndGate`
- `internal/web/character_handlers.go` (whole) — the five-move proxy shape
- `internal/auth/character_service.go` (:100-232, :290-313) — the create pipeline, `CHARACTER_*` codes, `mapGateError`
- `internal/auth/guest_service.go` (:150-209) — the existing `default_character_id` write, best-effort
- `internal/auth/postgres/player_repo.go` (:175-244) — the full-row `Update`, the narrow `UpdatePassword` precedent
- `internal/auth/player.go` (:175-205) — the `PlayerRepository` interface
- `internal/charname/pipeline.go` (:100-132), `gate.go` (:150-198), `mixedscript.go` (:96-151), `syntax/syntax.go` (:30-48) — the complete `NAME_*` code inventory and the 2/32-rune bounds
- `internal/world/validation.go` (:14-38) — `MaxNameLength = 100`, `MaxDescriptionLength = 4000`, the character-name aliases
- `internal/world/service.go` (:1054-1094) — `UpdateCharacterProfileAttributes`
- `internal/world/postgres/character_repo.go` (:525-556) — the retire-time `default_character_id` clear
- `internal/access/policy/poller.go` (:83-103), `cache.go` (:141) — the 10s default and `Reload`
- `test/meta/character_rpc_census_test.go` (whole) — the descriptor census
- `test/meta/characteraccess_routing_census_test.go` (whole) — the routing census, audience partition, promotion guard, fail-closed classifier
- `test/integration/access/character_profile_read_test.go` (:1-270) — `profileCorpusStore`, `newCorpusEngine`, `newServer`, `insertProperty`, the fixture
- `internal/world/postgres/property_repo_test.go` (:430-450) + header — the UNIQUE-constraint proof, `//go:build integration`
- `internal/access/profilevis/{profilevis,tierfloor}_test.go`, `internal/access/policy/seed_profile_visibility_test.go` — function inventories, cited line numbers re-verified
- `docs/architecture/invariants.yaml` (:2160-2215, :2305-2330) — INV-PRIVACY-9/10/11, INV-ACCESS-10/11/12
- `Taskfile.yaml` (:185-340, :672-730, :1230-1296) — verified task names, CLI_ARGS interpolation, `pr-prep:run` body
- `web/svelte.config.js`, `web/vite.config.ts`, `web/package.json`, `web/src/routes/+layout.{svelte,ts}`, `web/src/routes/(authed)/+layout.ts`, `web/src/routes/login/+page.ts`, `web/src/routes/(authed)/characters/+page.svelte`, `web/src/lib/components/TopBar.svelte` (:130-160), `web/src/lib/scenes/createFlow.ts`, `web/src/lib/components/scenes/PoseCard.svelte.test.ts`, `web/e2e/` listing
- `internal/telnet/gateway_handler.go` (:66, :560-580) — the surviving `CoreService.CreateCharacter` consumer

### Secondary (HIGH confidence — authoritative planning artifacts)

- `.planning/phases/01-portal-spec/01-SPEC.md` §7.2–7.5, §8.6–8.10, §9.1–9.7, §12.1
- `.planning/phases/05-character-identity-ui-public-profiles/05-CONTEXT.md` (D-84…D-98)
- `.planning/phases/05-character-identity-ui-public-profiles/05-UI-SPEC.md` (approved, checker 6/6)
- `.planning/REQUIREMENTS.md` (:94-251), `.planning/STATE.md` (:34-104)
- `CLAUDE.md` and `.claude/rules/{gateway-boundary,grpc-errors,invariants,logging,terminology,branding,testing,abac-providers,proto-doc-comments,search-tools,landing-the-plane}.md`, `web/CLAUDE.md`

### Tertiary (LOW confidence — flagged, not relied on)

- `.claude/rules/references/plan-review-learnings.md` — used only as a hazard list; its `task test:int` claim was **re-verified and found stale** (rule `4th2295d3j`).

**No web search or external documentation lookup was performed.** Every claim in this document is grounded in the repository or in a `.planning/` artifact read this session; no external package or API was consulted, because the phase introduces none.

---

## Metadata

**Confidence breakdown:**

- **Standard stack: HIGH** — zero new dependencies; every version read from `go.mod` / `web/package.json` / tool `--version`.
- **Architecture (server): HIGH** — every handler, gate, projection and census read end to end this session.
- **Architecture (create transaction): MEDIUM** — the two candidate shapes are both buildable and the SPEC does not settle between them (Q1). This is the one place a plan could go wrong without a test catching it.
- **Architecture (frontend): HIGH for the routing seam** (adapter-static fallback + `ssr=false` makes `/c/[id]` structurally trivial); **MEDIUM for the roster's data source** (Q2 is a genuine gap between two shipped message shapes).
- **Pitfalls: HIGH** — all thirteen carry a verified `path:line` and most carry a verbatim quote.
- **Validation architecture: HIGH** — D-98's five "already proven" claims were each re-verified by opening the test; the two deltas are specified against a harness read this session; the "no web-test gate" finding was verified by absence in both `Taskfile.yaml` and `.github/workflows/ci.yaml`.

**Research date:** 2026-08-12
**Valid until:** 2026-09-11 (30 days — the surface is in-repo and stable; re-verify line citations if any Phase-4 file is touched before planning, and re-verify Taskfile claims per rule `4th2295d3j`)
