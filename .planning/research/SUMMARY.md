# Project Research Summary

**Project:** HoloMUSH — v0.13 Web Portal: Identity & Admin Foundations
**Domain:** Character identity surfaces + role-gated admin shell on a mature Go/gRPC + SvelteKit brownfield platform with default-deny ABAC
**Researched:** 2026-07-31
**Confidence:** HIGH for in-tree claims (every one cites a `path:line` read during research); MEDIUM for feature-landscape and extensibility-rot findings

> Detail lives in `STACK.md`, `FEATURES.md`, `ARCHITECTURE.md`, `PITFALLS.md`. This file synthesizes,
> adjudicates the conflicts between them, and states what the SPEC phase must decide.

---

## Executive Summary

v0.13 is **not** a greenfield build. Three of the four capabilities are largely *assembly of shipped
substrate*, and the single most valuable research result is the size correction: per-field privacy
already exists end-to-end (`entity_properties` per-row `visibility`/`visible_to`/`excluded_from`,
`PropertyProvider`, six seed policies, and a fail-closed per-property filter loop in
`world.Service.ListPropertiesByParent`), two of the three "missing" character mutations already exist
and are already ABAC-gated at the domain layer (`UpdateCharacterDescription`, `DeleteCharacter`), and
the BFF facade pattern the milestone needs is shipped and traceable end to end in `SceneAccessService`.
The genuinely new work is: **rename + soft-retire domain commands**, a **`CharacterAccessService`**
facade, an **`AdminPortalService`** facade, **one new seed policy** for off-location public profile
reads, a **character lifecycle column**, a **unique index on a normalized name**, and the **web UI**.

The recommended approach is therefore *copy the shipped path, do not invent a parallel one*:
`WebService` proxy -> core-side facade (`resolveAndGate` -> `ownedCharacter`) -> `world.Service`
(`checkAccess` -> version-guarded CAS -> same-transaction outbox). Profile fields and media references
are `entity_properties` rows, not new columns — which makes the "1 primary + 10 gallery without a later
migration" requirement literally true (an `INSERT`, zero DDL) *and* gives every image its own ABAC
handle for free. Admin authorization is ABAC (`admin_section:` resource + one seed policy), never a
bare `PlayerHasRole` lookup, and never a gateway or route-guard decision.

The risks are concentrated and mostly **verification-shaped**, in the exact way v0.12's audit warned
about: the natural test for every privacy and authorization property here passes while the property is
false. A private-field test passes on an empty fixture; a per-endpoint leak test cannot detect the
endpoint nobody wrote a test for; a denial test passes when its subject would have been denied anyway;
a "reserved section" nav test passes vacuously because no sections exist. The mitigations are the same
shape throughout — **census/set-equality tests, paired positive controls, marshaled-bytes assertions,
and gates demonstrated RED against the pre-fix state**. Layered on top are four in-tree hazards this
milestone is the first to load: player-wide role semantics, an unindexed name-uniqueness race that
`Rename` doubles the writers into, display names denormalized into immutable public archives, and a
public profile page that default-deny currently *denies*.

---

## Key Findings

### Recommended Stack

Nothing in the platform stack changes. The delta is a small set of **web-side additions** plus vendored
shadcn-svelte source. Versions were verified against the npm registry on 2026-07-31.

**Core technologies (additions only):**

- **`zod` 4.4.3** — single client-side validation contract mirroring the authoritative `buf.validate`
  constraints already used on `web.proto`. Cheap, ecosystem-standard, TS 5.5+ compatible.
- **`sveltekit-superforms` 2.30.2 + `formsnap` 2.0.1** — per-field errors, `$constraints`, and correct
  `aria-describedby` wiring for a profile sheet where every field carries a paired visibility control.
  **SPA mode (`SPA: true`) is load-bearing** — `onUpdate` is the submit handler and calls ConnectRPC
  directly, which is required because this app uses `adapter-static`. **Conditional:** if the SPEC lands
  the profile under ~8 fields, the existing hand-rolled flow-module pattern (`$lib/scenes/createFlow.ts`)
  wins and superforms is ceremony. This is the one stack recommendation the SPEC may overturn.
- **`@tanstack/table-core` 8.21.3** — headless sort/filter/paginate/column-visibility for the admin
  character list. Zero peer deps. Amortizes across four of the six deferred admin sections, which are all
  list-search surfaces.
- **shadcn-svelte registry components** (vendored source, not deps): `table`/`data-table`, `form`,
  `select`, `switch`, `avatar`, `alert-dialog`, `pagination`, `tabs`, `breadcrumb`, `skeleton`.

**Explicitly not added:** `@tanstack/svelte-table` (Svelte-4 store wrapper; shadcn's `data-table` ships
its own runes wrapper), `@testing-library/svelte` (the repo already has a working `mount`/`unmount`
component-test project with 17 files), `vitest-browser-svelte` (a real improvement but a whole-suite
migration — file it separately), any query-cache layer (creates a second source of truth against the
live `StreamEvents` push feed), any DB client in `web/src/` (gateway-boundary violation), and any upload
library (deferred to 999.16).

### Expected Features

**Must have (table stakes):**
- Character creation collecting a **structured identity card** (name, pronouns as its own field, concept,
  species, age, faction) — not the current name-only stub
- Prose fields (appearance/description, personality, background) with **server-enforced length caps**
- **Rename**, edit-profile, and **soft retire** — plus multi-alt management from one place
- **Public profile page** at a stable URL, rendering correctly for a logged-out visitor, with blank fields
  hiding themselves and an initial-letter avatar placeholder
- **Profile != sheet as separate surfaces** — ship the split, ship the sheet **empty** (mechanical stats
  need a system that does not exist)
- **Per-field visibility**, server-enforced, with sane defaults and a whole-profile master switch
- **`RoleAdmin`-gated `/admin`** with a dashboard of section-contributed cards, character list+search,
  character detail with admin edit, and disable/delete reusing the *same* lifecycle states as player retire
- **Permission-filtered nav as a registry contract**, not template `{#if}`
- **Audit emission on every admin mutation** — the viewer is deferred; the emission is not

**Should have (competitive, low cost, strong pull-in candidates):**
- **Rumors / RP-hooks field** — the highest value-to-cost item in the whole milestone; it is what turns
  profile reading into scenes
- **A volatile "Currently" status line** — one short public field, constantly used, makes profiles feel live
- **OOC RP-preferences block** (style, availability, content limits, walk-up-friendly)
- **Privacy presets** above the per-field matrix; **searchable character directory** built from the
  structured fields

**Defer (and the seam each needs):**
- Sheet contents (needs a stats system); avatar/gallery upload (999.16 — schema only, zero behavior);
  rostering/transfer (999.6 — must be a *distinct transition out of* retired); remaining six admin
  sections (999.8 — registered `planned` entries); wiki (qve.8 — external-link seam); web DMs (qve.17 —
  a named empty slot in the profile action bar, **not** a dead button); offline/PWA (qve.7)

**Named anti-features:** freeform HTML/CSS in profiles (Samy-worm class; CSS alone still exfiltrates under
CSP), over-granular privacy matrices, per-character-pair visibility (IC-knowledge modelling wearing a
privacy hat), relationship graphs (consent + staleness + N^2 privacy-filtered reads), raw DB/SQL console in
the admin panel (bypasses every ABAC gate and audit emission), hardcoded break-glass admin identifiers,
admin impersonation (launders the audit trail), hard-delete on retire, dashboard-first MVP with stub
sections, and >2-level nav nesting.

### Architecture Approach

Copy the shipped scenes path exactly. `WebService` RPCs in `internal/web` are **pure proxies** (nil-client
guard -> read `X-Session-Token` -> one forwarding call -> pass gRPC status errors through unwrapped). A
core-side **facade** owns identity (`resolveAndGate` -> `PlayerSession`, guest rejected; `ownedCharacter`
-> `NotFound` on non-owned, so denial hides as absence). **`world.Service.checkAccess` owns
authorization.** Never invert those two.

**Major components:**
1. **`CharacterAccessService`** (`internal/grpc/characteraccess_service.go`, NEW) — character profile
   read/write BFF facade. **Extract `resolveAndGate`/`ownedCharacter` into a shared file first** — two
   copies of a guest gate is a security-drift hazard.
2. **`AdminPortalServer`** (`internal/grpc/adminportal_service.go`, NEW) — one home for all seven admin
   sections, and exactly one place to apply the admin-authorization decorator. **Must not ride `admin.v1`**
   (UDS-only by design, TOTP+password auth model, break-glass crypto surface — three independent reasons).
3. **`world.Service`** (MODIFY) — `+RenameCharacter`, `+RetireCharacter` (soft). Both must land a
   `writeCommands` census row and a taxonomy kind **in the same change** (`internal/world/mutator.go:78-100`).
4. **`entity_properties`** (UNCHANGED, reused) — profile fields *and* media refs as rows, each with its
   own `visibility`/`visible_to`/`excluded_from`.
5. **ABAC vocabulary** (MODIFY) — `ResourceAdminSection = "admin_section:"` + `AdminSectionResource()`
   panic-on-empty helper; `seed:admin-section-access`; `seed:profile-public-read`.
6. **`web/src/lib/nav/sections.ts`** (MODIFY) + a parallel `ADMIN_SECTIONS` registry — the `as const
   satisfies` pattern already makes a section without an icon a **compile** failure.

Two shipped seeds already cover the hard cases for free: self-edit matches `seed:player-self-access`,
admin-edit-anyone matches `seed:admin-full-access`. **Exactly one new read policy is required**, because
`seed:player-character-colocation` currently *denies* an off-location profile read.

### Critical Pitfalls

1. **Return-all-and-hide-in-the-client.** Make absence the enforcement mechanism: separate owner/public
   messages, or one message with `optional` scalars cleared server-side — never a `field_visibility` map
   telling the client what to hide. This is the same omit-don't-sentinel rule `.claude/rules/abac-providers.md`
   already mandates. **The message shape is the fix; it cannot be retrofitted cheaply.**
2. **List/search/export leak what the detail endpoint protects.** `CharacterSummary` already carries
   session/location/last-played state, and `ListAllCharacters`' own doc comment already draws a privacy
   line enforced by nothing but two hand-written field lists. Fix: one projection function per
   (row -> audience), a lint banning character-shaped struct literals outside the projection package, and a
   **census with set equality** over every character-returning RPC — a per-endpoint suite is structurally
   incapable of detecting a missing member of its own set.
3. **Privacy is not retroactive, and the system is already primed for it.** Display names are denormalized
   into immutable event payloads and `scene_log`, served publicly by `WebGetPublicSceneArchive`. There is
   no update path. Decide the scope in the SPEC, **state it in the UI at the toggle and at retirement**,
   and add **no new denormalizations**.
4. **Route-guard-only admin authorization.** Every admin RPC re-asserts its own gate. Copy
   `AssertOperatorAdmin`'s shape exactly (one shared helper, called first at every entry point, typed
   `DENY_*` codes, the "prevents the three sites from drifting" rationale carried into the doc comment).
   Assert the **top-level** oops code via `oops.AsOops(err).Code()` — `errutil.AssertErrorCode` chain-walks
   and passes on double-wrap.

   > **SUPERSEDED — see `01-SPEC.md` §9.6.1, §12.1 rule 5, and §14, row 8.** Both halves of that
   > sentence are false against the pinned `github.com/samber/oops v1.22.0` (`go.mod:32`).
   > `OopsError.Code()` is documented in the dependency as *"returns the error code from the deepest
   > error in the chain"* and is a recursive `getDeepestErrorCode` walk;
   > `errutil.AssertErrorCode` (`pkg/errutil/testing.go:15-20`) is a thin wrapper over `oops.AsOops`
   > plus that same `.Code()`. The two spellings are **behaviorally identical** and **both** pass on
   > a double-wrap, so a test written to this prescription asserts the inner code and passes while
   > the wire leaks. (`oops.AsOops` also returns `(OopsError, bool)`, so the single-expression
   > spelling does not compile.) Assert **over the wire** instead: `status.Code(err)` plus a generic
   > `status.Convert(err).Message()` with no internal code string in it. `errutil.AssertErrorCode`
   > remains correct for asserting *which* internal code was produced. The rest of item 4 — one
   > shared helper, called first at every entry point, typed `DENY_*` codes — stands unchanged.
   > Tracked as issue **#4902**. Annotated 2026-08-01 by plan 01-05.
5. **Reserved capacity that rots.** Reserved room carries an implicit false promise that "the hard part is
   done." The fix is the milestone's highest-leverage single decision — see item 8 under Must Carry Forward.

---

## Adjudicated conflicts

The four researchers worked independently. Where a claim is contradicted by a cited `path:line`, the
cited claim wins.

### CONFLICT 1 (named at kickoff) — storage shape for per-field visibility and the 1+10 media model

| Side | Claim | Evidence cited |
|---|---|---|
| STACK.md | NEW `character_profiles` table with `media JSONB` + `field_visibility JSONB` | Four JSONB precedents: `000001:61`, `000001:318`, `000006:10`, `000039:5-7` |
| ARCHITECTURE.md | `entity_properties` rows. **Zero DDL.** | `000001_baseline.up.sql:350-373` (per-row `visibility` CHECK + `visible_to`/`excluded_from` + `UNIQUE(parent_type,parent_id,name)`); `attribute/property.go:61-147`; `seed.go:110-145`; `world/service.go:1144-1171` |

**ARCHITECTURE.md wins. Decisively.** Reasons, in order of weight:

1. **STACK.md did not know `entity_properties` existed.** Its JSONB precedents are real but they answer a
   different question — "how does this repo absorb evolving *opaque* shape". They are not evidence against
   a shipped per-row privacy subsystem it never cites. ARCHITECTURE cites the table, the provider, the six
   policies, and the fail-closed filter loop by line.
2. **It satisfies the requirement more literally.** STACK's own framing is "the DB is where a later
   `ALTER TABLE` would be required." `entity_properties` requires no `ALTER TABLE` for the new table
   *either* — an eleventh image, a twelfth field, or an entirely new profile section is an `INSERT`.
   STACK's answer needs one migration now; ARCHITECTURE's needs zero, ever.
3. **A JSONB array has no per-element ABAC handle.** Per-image and per-field privacy under a JSONB blob
   forces exactly the service-layer projection that ARCHITECTURE rejects and PITFALLS #1 names as the
   critical leak shape. **The two decisions are coupled: choosing columns/JSONB here silently forces the
   wrong answer on privacy.** This is what makes the conflict non-averageable.
4. **PITFALLS independently corroborates.** 14c ("media schema with no consumer rots") says the only
   honest verification is *actually inserting 1 primary + 10 gallery rows through the schema today*.
   `entity_properties` makes that test writable in v0.13 with no uploader — and `UNIQUE(parent_type,
   parent_id,name)` enforces "exactly one primary" at the database level for free.

**Committed shape:** `profile.image.primary` + `profile.image.gallery.00..09` as property rows; all
profile fields as `profile.*` property rows; intrinsic columns (`name`, `description`, lifecycle status,
`version`) stay columns. `characters.description` remains the **in-world "look at" description**
(co-location-gated); everything on the profile page is a property.

**What STACK was right about and must survive:** the *proto* half of the media requirement is genuinely
free and should ship now, empty — `ProfileImage{media_id, alt_text, content_warning}` + `primary_image` +
`repeated gallery [max_items = 10]`. That also pre-answers PITFALLS 14c's warning that alt-text and
content-warning have nowhere to live until moderation arrives.

**Residual for the SPEC (small, but real):** a future *public character directory* wants indexed,
sortable structured fields, which property rows do not give cheaply. That is a deferred feature, and
PITFALLS #4 independently argues public surfaces must not sort or filter on privacy-bearing fields at all.
**Decision the SPEC must make:** does any v0.13 surface sort or filter on a profile field? If the answer
is no (recommended), the directory's indexing need is a later, additive, non-blocking question.

### CONFLICT 2 — where roles live, and what mutation capability exists

STACK.md states roles are at `internal/access/role.go:6-12` with a RoleStore, and that "no mutation RPCs"
exist at any layer. **ARCHITECTURE.md's citations win on both counts:**

- `internal/access/role.go:6-13` holds **only** the three role string constants + `SystemRoles()`. The
  `RoleStore` interface is `internal/store/role_store.go:14-23`, backed by `character_roles`.
- STACK is right at the **RPC** layer but wrong at the **domain** layer:
  `world.Service.UpdateCharacterDescription` (`service.go:799-836`) and `DeleteCharacter`
  (`service.go:745-777`) already exist and are already ABAC-gated. **Only rename and soft-retire are
  genuinely absent.** This materially shrinks the character-mutation phase.
- `DeleteCharacter` cascades `entity_properties` and emits a `character_deleted` tombstone in one
  transaction — it is **not reversible** and must never be wired to a player-facing button.

### CONFLICT 3 — proto field reservation

ARCHITECTURE (S5) suggests `reserved 100 to 199;` ranges but grades the seam "weak — do not lean on it."
STACK argues `reserved` on never-used numbers blocks the author and buys nothing. PITFALLS 14a says
reserve **numbers only, never named fields**, because a reserved non-`optional` field serializes `""` from
day one — a **fail-open placeholder** under absence-means-hidden semantics.

**Resolution:** all three agree on the operative rule and disagree only on whether the hygiene is worth
writing down. Adopt: *reserve numbers only, as a documentation convention, with a comment naming the
deferral issue.* **It MUST NOT appear in a REQ-ID as if it discharged the extensibility constraint** —
the registry, the ABAC namespace, and the property-row media model carry that.

### CONFLICT 4 — how many privacy tiers

FEATURES commits to three (`public` / `members` / `private`). PITFALLS 14d argues to ship only tiers with
a working evaluator, preferring two, and names the sharpest failure: an unimplemented tier whose evaluator
is `switch { case Private: false; default: true }` **fails open**.

**Resolution — PITFALLS wins on the tier count; FEATURES wins on the storage type.** The `entity_properties`
CHECK constraint already fixes the vocabulary (`public`/`private`/`restricted`/`system`/`admin`), and
`restricted` already has real evaluators via `visible_to`/`excluded_from`. FEATURES' `members` tier has
**no membership source in this codebase** and no evaluator. So: **ship `public` and `private` in the v0.13
UI**; do not invent `members`. FEATURES' underlying point stands — the tier is a string enum, not a
boolean, so adding one later is an enum append. Also carry FEATURES' two floors: **profile reachability is
its own facet above the fields** (private => return not-found-equivalent, never "this profile is private",
which leaks existence), and **name/pronouns cannot be set private**. And PITFALLS' rule: exhaustive
`switch` with `default: deny`, applied to >=2 fields at ship time.

> **SUPERSEDED IN PART — see `.planning/phases/01-portal-spec/01-SPEC.md` §14, row 4.** The clause
> *"ship `public` and `private` in the v0.13 UI"* no longer applies to an **owner-facing** UI: v0.13
> ships no player or character agency over profile visibility, so there is no surface on which a
> player selects a tier. **The tier-count decision above survives intact** — two tiers with real
> evaluators, `restricted` present in the CHECK constraint but not surfaced, and the exhaustive
> `switch` with `default: deny`. Only its owner-facing interface expression is superseded; the
> reasoning that produced it (PITFALLS' fail-open-unimplemented-tier argument beating FEATURES'
> three-tier proposal) is reasoning the SPEC still relies on, which is why this record is annotated
> rather than rewritten. Annotated 2026-08-01 by plan 01-05.

### Minor discrepancy

FEATURES says "seven `planned` registry entries"; PROJECT.md and ARCHITECTURE say **seven total** —
`characters` (available) + six planned (stats, players, moderation, audit, config, plugins). Use seven
total / six planned.

---

## Must carry forward (scope- or security-changing)

1. **`PlayerHasRole` is player-wide, not character-wide** (`internal/store/role_store.go:83-103`) — true
   iff *any* character of the player holds the role, and the operator path
   (`internal/admin/auth/ingame.go:116`) relies on it. A role granted to a throwaway alt confers it
   **everywhere**. Any admin surface exposing role mutation is an escalation vector.
2. **Retire != idle-out != purge — three distinct operations.** Conflating them is unrecoverable given FK
   references from `character_roles` (CASCADE — silently drops roles), `scene_participants`,
   `player_character_bindings`, plus `locations.owner_id`/`objects.owner_id` with **no `ON DELETE`**
   (`NO ACTION` => hard delete errors at runtime). **Retire MUST NOT release the name** — that forecloses
   rostering (999.6) and creates an impersonation vector against denormalized historical names.
3. **Character name uniqueness has no database constraint** — check-then-insert TOCTOU across
   `internal/bootstrap/setup/adapters.go:38-50` and `internal/auth/character_service.go:112-121`, no unique
   index, no `LOWER(name)` index, and normalization that does no NFKC / `Cf`-stripping / confusable folding
   (`internal/world/validation.go:114-126`). **Adding `Rename` doubles the writers into that race.** The
   unique index on a stored normalized-name column MUST land **before or with** `Rename`, not after.

   > **SUPERSEDED IN PART — see `01-SPEC.md` §6.1.3 and §14, row 7.** The two sites named above are
   > the shared existence **query** and **one** writer, not two writers. There is a **second**
   > writer: `internal/auth/guest_service.go:227`, calling the same `ExistsByName` inside the
   > guest-name retry-on-collision loop. So `Rename` takes the writers from **two to three**, not
   > from two to four. The conclusion is unchanged and if anything strengthened — but the
   > duplicate-detection audit MUST cover the **guest** path, which provisions characters
   > automatically and at volume and is therefore the likeliest source of pre-existing duplicates.
   > Annotated 2026-08-01 by plan 01-05.
4. **Rename/retire cannot reach denormalized history** — `actor_display_name` in immutable event payloads
   and `CharacterName` in `scene_log`, the latter served publicly via `WebGetPublicSceneArchive`. Enumerate
   every name-capture surface in the SPEC with a historical-vs-live verdict; do **not** mass-update an
   append-only log.
5. **A public profile page is currently DENIED** — `seed:player-character-colocation` requires co-location.
   `seed:profile-public-read` is required, and it **widens read to all existing public character
   properties**, so an audit of current `entity_properties WHERE parent_type='character' AND
   visibility='public'` rows is a prerequisite.
6. **New mutation RPCs MUST carry `expected_version`** (migration `000049`) and emit through the
   transactional outbox **in-transaction**, or they silently regress v0.12's MODEL-03/04. `expected_version`
   must be on the request messages from the SPEC — adding it later is a wire-compat change to every caller.
   Point v0.12's existing two-replica resilience harness at the new RPCs rather than writing fresh
   concurrency tests.
7. **Audit EMISSION on admin mutations lands in v0.13** even though the audit VIEWER is deferred —
   otherwise the viewer ships later with no history behind it. Ride `events_audit` (retention, never-drop
   DLQ, replay CLI come free), not a bespoke table. **Before-values are the whole point**; an audit row
   saying "admin X updated character Y" answers nothing. Record the acting **player** id, not just the
   character (see #1).
8. **Reserved capacity shape (the milestone's defining constraint, made structural):** the six deferred
   admin sections ship **registered, role-gated, and returning `NOT_IMPLEMENTED` AFTER the gate**. Then
   wiring one later replaces a handler body rather than requiring someone to remember a check. The registry
   entry requires an authorization descriptor with **no default and no zero value meaning allow**; a
   section registered without one fails at compile or at boot. A meta-test asserts **set equality** between
   the registry and the descriptor set. This makes the extensibility REQ non-vacuous from day one.

---

## Implications for Roadmap

### Phase 1: Portal SPEC
**Rationale:** Already the milestone's opener and the precondition PROJECT.md's Out-of-Scope entry demanded.
More importantly, **eight of the fourteen catalogued pitfalls are SPEC-phase decisions whose cost explodes
after code exists** — message shape, audience matrix, read-surface inventory, lifecycle column,
`expected_version` placement, normalization policy, tier count, and the history-scope promise.
**Delivers:** admin IA + the seven-entry registry contract (incl. the mandatory authorization descriptor);
character data model incl. the **lifecycle column** and the property-row profile/media shape (per
Conflict 1); the **audience matrix** (public / owner / admin) and per-audience message shapes; the
**read-surface inventory** — every character-returning RPC including the three existing public export
surfaces; the **name-capture surface inventory** with historical-vs-live verdicts; name normalization
policy; the full new RPC surface with `expected_version` on every mutation request.
**Addresses:** the whole FEATURES MVP list, by fixing shapes rather than building.
**Avoids:** Pitfalls 1, 2, 3, 4, 5, 10, 11, 12, 13, 14a, 14d — all named as Phase-1-SPEC prevention points.
**Explicit SPEC exclusion to write down:** **role mutation is not part of character administration in this
milestone.** An omission is not an exclusion.

> **SUPERSEDED IN PART — see `.planning/phases/01-portal-spec/01-SPEC.md` §14, row 6.** The count in
> **Delivers** — *"every character-returning RPC including the **three** existing public export
> surfaces"* — is short by one: the tree carries **four**. The three this record reaches are
> `WebExportScene`, `WebGetPublicSceneArchive` (`api/proto/holomush/web/v1/web.proto:345`) and
> `WebDownloadPublicSceneArchive`. The fourth is `WebListPublishedScenes` (`web.proto:339`), which
> returns the same frozen `participants_snapshot` column in bulk — one entry per published scene —
> and is a census member on the same grounds as the other three. **The deliverable itself is
> unchanged** — an exhaustive read-surface inventory — and the reasoning that produced it stands;
> only the count is superseded, which is why this record is annotated rather than rewritten. §3.3
> carries the corrected enumeration and names the fourth explicitly. Annotated 2026-08-01 by the
> phase-1 gap-closure pass.

### Phase 2: ABAC + schema vocabulary
**Rationale:** Every later phase's authorization is expressed in this vocabulary. No UI, no RPCs — pure
policy plus tests, which is where an unverified assumption surfaces cheapest.
**Delivers:** `ResourceAdminSection` + `AdminSectionResource()`; `seed:admin-section-access` (covers all
seven sections **and every future one at zero policy cost**); `seed:profile-public-read` + the
existing-public-character-property audit; the `profile.*` property naming convention; the character
lifecycle column + normalized-name unique index migration (with a duplicate-detection step, since the
race has been live).
**Verification that can fail:** table-driven over 7 section ids x {admin, builder, plain player, guest},
set-equality on the id list; the concurrent name race against real Postgres, **demonstrated RED against
the current unindexed schema first**.

### Phase 3: World character commands
**Rationale:** Domain layer only, and gated by the `writeCommands` census (`mutator.go:78-100`) — the
highest-risk unverified seam in the milestone. Independent of Phase 2; can run in parallel.
**Delivers:** `RenameCharacter`, `RetireCharacter` (soft, modelled as masked `write` so it costs **zero**
new policy and inherits both self-edit and admin-full-access); census rows + taxonomy kinds landed in the
same change; a `character.renamed` event carrying `{id, old_name, new_name}` on the outbox.
**Open question the plan must resolve before writing code:** the census comment describes a *bijection*
over the boundary, which may forbid two commands sharing one kind. Verify the exact semantics — renaming
the existing `UpdateCharacterDescription` descriptor may be cleaner than adding a second producer of
`character_updated`.

### Phase 4: Shared facade helpers + `CharacterAccessService`
**Rationale:** Mutation RPCs before management UI. **The extraction of `resolveAndGate`/`ownedCharacter`
comes first**, so there is never a second copy of the guest gate.
**Delivers:** shared helper file; `CharacterAccessService`; `WebCharacter*` pure proxies;
`handler.go` client field + option; `sub_grpc.go` wiring with an `Unimplemented*` fallback.
**Uses:** the `WebUpdateScene` `update_mask` shape verbatim; `optional` scalars per Pitfall 1.

### Phase 5: Character creation + management UI + public profile page
**Rationale:** First user-visible slice, and it ships the media-schema proof with no uploader.
**Delivers:** creation identity-card flow replacing the name-only stub; alt management; public profile page
built **exclusively** from `ListPropertiesByParent`'s filtered slice; owner edit; per-field public/private
toggles; the 1-primary + 10-gallery insert test.
**Uses:** zod (+ superforms/formsnap if the field count justifies it), `avatar`, `switch`, `form`, `tabs`.
**Pull-in candidates from FEATURES P2** (each is one prose field with outsized value): **rumors/RP-hooks**
and the **"Currently" status line**.
**Hard rule for the SPEC to state as a MUST:** the facade MUST NOT call `PropertyReader.ListByParent` /
`PropertyRepository.ListByParent` directly — those are unfiltered by construction. Consider a
viewer-scoped reader type that makes the unfiltered call *untypeable* from the facade.

### Phase 6: Admin portal shell + character administration
**Rationale:** Depends on the most (phase 2's `admin_section:` vocabulary, phase 4's facade helpers).
**The authorization gate must be the first thing built in this phase**, before any section, so every
subsequent section inherits it.
**Delivers:** `WebCheckSessionResponse.roles` + `authStore.roles` (nav hiding only, never the boundary);
`AssertWebAdmin` shared helper; `AdminPortalService`; `WebAdmin*` proxies; `/admin` route tree;
`ADMIN_SECTIONS` with 7 entries and 6 **gated `NOT_IMPLEMENTED`** stubs; character list/search/detail/edit
with an explicit **field-mask allowlist excluding roles**; **audit emission with before-values, in-transaction**.
**Scheduling optimization:** the `roles` proto field could be pulled into Phase 4 to avoid a second
`web.proto` regeneration cycle. Not a dependency.

### Phase Ordering Rationale

- **SPEC first because the expensive mistakes are shape mistakes.** Message shape, audience matrix, and
  `expected_version` placement are all wire-compat changes after the fact.
- **Vocabulary before surfaces.** Privacy and admin-section policy must exist before anything reads or
  gates on them; getting it wrong is a redeploy, not a refactor.
- **Domain before facade before UI**, mirroring the shipped scenes path exactly.
- **Admin last** because it consumes the most and because its net-new trust boundary
  (**zero `RoleAdmin` references exist under `internal/web/` today**) has no existing test suite that would
  notice if it is wrong. Building it on proven helpers is strictly safer than building it early.
- **The lifecycle column and the name index are load-bearing for two phases each**, so they land in
  Phase 2 rather than being rediscovered per-consumer.

### Research Flags

Phases likely needing `/gsd-plan-phase --research-phase` during planning:
- **Phase 3 (world commands)** — the `writeCommands` census bijection semantics are genuinely unverified,
  and this repo has a documented history of plans failing on unverified seam assumptions.
- **Phase 6 (admin shell)** — no in-repo precedent for the web gateway making an admin decision; the
  `AssertOperatorAdmin` pattern must be transposed across a different auth model, and the reserved-section
  descriptor mechanism needs a concrete fail-at-boot design.
- **Phase 2, narrow slice only** — the existing-public-character-property audit is a data question, not a
  design one, but it must be answered before `seed:profile-public-read` merges.

Phases with well-understood patterns (skip research):
- **Phase 4** — a verbatim copy of a fully-traced shipped path (`sceneaccess_service.go`).
- **Phase 5** — established shadcn/superforms/runes patterns plus the existing `createFlow.ts` idiom.
- **Phase 1** — synthesis of this research, not new research.

---

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Versions and peer deps verified directly against the npm registry 2026-07-31; SPA-mode and data-table behavior cross-checked against Context7. The superforms-vs-plain-runes call is *conditional*, and the condition (final field count) is honestly stated. |
| Features | MEDIUM | Every finding corroborated across 3+ independent primary sources (MUSH policy pages, BuddyPress/Nextcloud/World Anvil/Rastrum, six game-server admin panels). Genre-convention evidence, not authoritative spec. Its privacy-tier recommendation is superseded here by PITFALLS. |
| Architecture | HIGH | Every claim cites a `path:line` read at `gsd/v0.13-milestone`. It corrected two wrong premises in its own brief and traced the precedent path end to end. Its `entity_properties` finding is the milestone's largest single simplification. |
| Pitfalls | HIGH (in-tree) / MEDIUM (section 14 rot patterns) | The in-tree hazards each cite a file; section 14's extensibility-rot modes are pattern-level. Its inverted-test framing is the most operationally useful artifact of the whole pass. |

**Overall confidence: HIGH** — with the caveat that confidence is high about the *substrate* and only
medium about the *product surface* (field list, tier UX, directory need), which is exactly what Phase 1
exists to settle.

### Gaps to Address

- **Public directory indexability** (Conflict 1 residual). Property rows are not cheaply sortable/filterable.
  *Handle:* Phase 1 answers "does any v0.13 surface sort or filter on a profile field?" If no (recommended,
  and independently argued by Pitfall 4), this is a later additive question for a deferred feature.
- **`/admin` gated per-acting-character or per-player?** Roles are stored per character; `PlayerHasRole` is
  player-wide; `WebCheckSessionResponse` carries neither. These give materially different UX with an alt
  switcher and different message shapes. *Handle:* Phase 1 decides **before** the proto is written.
- **`writeCommands` census bijection semantics.** *Handle:* verify in code before Phase 3 commits to either
  masked-update or separate commands.
- **Final profile field count.** Decides superforms vs. plain runes and is reversible per-form at low cost.
  *Handle:* Phase 1; not a blocker.
- **Name normalization policy** (does the game permit non-Latin names?) — a product decision with a security
  consequence. *Handle:* Phase 1 states the policy; Phase 2 implements NFKC + `Cf`-stripping + the chosen
  confusable/script rule.
- **Duplicate names may already exist** from the live TOCTOU race, which would make `CREATE UNIQUE INDEX`
  fail. Migration rules forbid in-migration backfills. *Handle:* detection query + a one-shot job + the
  migration, sequenced in Phase 2.
- **Invariant ids.** Do not mint `INV-PROFILE-*` / `INV-ADMIN-*` ad hoc. *Handle:* allocate in an existing
  scope (`ACCESS`, `PRIVACY`) or declare a boundary in `invariants.yaml`; ship `binding: pending` rather
  than fabricating a `// Verifies:`.

---

## Sources

### Primary (HIGH confidence)
- **Repository files at `gsd/v0.13-milestone`, read 2026-07-31** — `internal/store/migrations/000001_baseline.up.sql`
  (`:68-76` characters DDL, `:80-99,140-160` FK inventory, `:82-87` character_roles, `:350-373` entity_properties),
  `000045`, `000049`, `000051`; `internal/world/{service.go,character.go,property.go,mutator.go,validation.go}`;
  `internal/grpc/{sceneaccess_service.go,server.go,auth_handlers.go}`; `internal/web/{handler.go,scene_handlers.go}`;
  `internal/access/{prefix.go,role.go,grants.go}`, `policy/seed.go`, `policy/attribute/{property.go,character.go}`;
  `internal/store/role_store.go`; `internal/admin/auth/{operator_admin.go,ingame.go}`;
  `internal/auth/character_service.go`; `internal/bootstrap/setup/adapters.go`;
  `api/proto/holomush/{web,core,world,admin,content,sceneaccess}/v1/*.proto`; `cmd/holomush/sub_grpc.go`;
  `plugins/core-scenes/{poseorder.go,service.go,commands_emit_test.go}`;
  `web/{package.json,components.json,vite.config.ts}`, `web/src/lib/nav/sections.ts`,
  `web/src/lib/stores/authStore.ts`, `web/src/routes/`, `web/e2e/`.
- **npm registry** (queried 2026-07-31) — authoritative `latest` + `peerDependencies` for superforms 2.30.2,
  formsnap 2.0.1, zod 4.4.3, `@tanstack/table-core` 8.21.3, shadcn-svelte 1.4.2, bits-ui 2.18.1.
- **`unpkg.com/sveltekit-superforms@2.30.2/dist/adapters/index.d.ts`** — verified `zod4`/`zod4Client` exports.
- **Repo rules** — `.claude/rules/{gateway-boundary,abac-providers,grpc-errors,invariants,database-migrations,plugin-runtime-symmetry}.md`;
  `.planning/MILESTONES.md:15,24`; `.planning/PROJECT.md`.

### Secondary (MEDIUM confidence)
- **Context7** `/websites/superforms_rocks` (SPA mode, `onUpdate` as external-API handler, `defaults()` without
  a load), `/websites/shadcn-svelte` (data-table + `createSvelteTable`/`FlexRender`).
- **MUSH/text-RP primary sources** (~14 games) — chargen field lists, `+finger` conventions, retire/idle-out/purge
  vocabulary, name-release-as-purge-purpose (Tapestries, Steel & Stone, Arx, Fulcrum, DragonRealms).
- **Web platform privacy models** — BuddyPress xProfile, Nextcloud profile visibility, Rastrum
  `25-profile-privacy.md`, Orgo, Jive, Circle, World Anvil.
- **Admin IA registry patterns** — `django_admin_adapter`, `django-admin-global-sidebar`, `dj-control-room`,
  thoughtbot/administrate; game panels GameAP, CFTools, MonoSuite, HybridCore (and their anti-patterns:
  raw SQL console, hardcoded back door, impersonation).
- **Freeform HTML/CSS anti-feature** — Samy worm analysis; 2026 CSS-injection bug-bounty writeups exfiltrating
  under CSP; Reddit disabling plain HTML in posts.

### Tertiary (LOW confidence — needs validation)
- Migration-cost accounts for `vitest-browser-svelte` (blog/community posts) — sufficient to justify
  *deferring* the migration, not to plan one.
- Section 14 extensibility-rot failure modes — pattern-level, not code-grounded. Treated as design guidance;
  the concrete mitigations (registered-and-denied sections, insert-11-rows test) are independently verifiable.

---
*Research completed: 2026-07-31*
*Ready for roadmap: yes*
