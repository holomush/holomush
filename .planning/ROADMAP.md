# Roadmap: HoloMUSH

## Overview

HoloMUSH is a mature, actively-developed platform — the event-sourced core, ABAC access control, dual-protocol
(telnet + web) gateways, two-tier plugin host, and the flagship Scenes/RP subsystem are shipped and running
(full context: `milestones/v0.11-ROADMAP.md` "Shipped Foundation"). The v0.11 milestone (Channels, Scenes
lineage completion, platform hardening) shipped 2026-07-11.

The v0.12 milestone (Foundation Hardening, v0.12 Phases 4–9) shipped 2026-07-28 — the event-model decision and
its integrity fixes, operational hardening and CI assurance gates, architecture decomposition, and the
code-health sweep. Full detail: `milestones/v0.12-ROADMAP.md`.

**Active milestone — v0.13 Web Portal: Identity & Admin Foundations (Phases 1–6):** give web players a
complete character identity surface — creation, management, and public profiles with privacy — and stand up
the admin portal shell that gives character administration a home, with both designed to absorb the deferred
portal surfaces without rework. The milestone **opens by producing the portal SPEC** that PROJECT.md's
Out-of-Scope entry named as its precondition, rather than waiving it. Requirements: `REQUIREMENTS.md`
(50 REQ-IDs across PORTAL / IDENT / PROFILE / ADMIN / EXT). Research basis: `research/SUMMARY.md`.
Remaining not-yet-scheduled scope stays in `## Backlog` — promote entries with `/gsd-review-backlog`.

## Milestones

> **Phase-numbering convention (changed at v0.13).** Phase numbers **restart at 1 for every milestone as of
> v0.13** — a phase id is unique *within its milestone*, not across project history. **v0.11 and v0.12 used
> continuous global numbering** (v0.11 Phases 1–3, v0.12 Phases 4–9) and are **not** renumbered: they shipped
> under those numbers, and the archives, PRs, and `milestones/v0.1x-phases/` directories all reference them.
> Wherever an archived phase is named below, its milestone is stated explicitly (e.g. "v0.12 Phase 4"); a
> bare "Phase N" always means the **active milestone**. The `999.x` backlog entries are parking-lot ids, not
> milestone phases — their numbering is independent of both schemes.

- ✅ **v0.11 Social Spaces & Platform Hardening** — v0.11 Phases 1–3 (shipped 2026-07-11) — [archive](milestones/v0.11-ROADMAP.md) · [audit](milestones/v0.11-MILESTONE-AUDIT.md)
- ✅ **v0.12 Foundation Hardening** — v0.12 Phases 4–9 (shipped 2026-07-28) — [archive](milestones/v0.12-ROADMAP.md) · [audit](milestones/v0.12-MILESTONE-AUDIT.md)
- 🚧 **v0.13 Web Portal: Identity & Admin Foundations** — Phases 1–6 (in progress, started 2026-07-31)

## Phases

<details>
<summary>✅ v0.11 Social Spaces & Platform Hardening (v0.11 Phases 1–3) — SHIPPED 2026-07-11</summary>

> **Numbering:** these are **v0.11** phase numbers under the retired continuous global scheme. They are
> *not* the active milestone's Phases 1–6 and are deliberately left un-renumbered.

- [x] v0.11 Phase 1: Channels Subsystem (10/10 plans) — `core-channels` as the social-spaces substrate's second consumer — completed 2026-07-09
- [x] v0.11 Phase 2: Scenes Lineage Completion (7/7 plans) — notifications + telnet polish (templates descoped to backlog) — completed 2026-07-09
- [x] v0.11 Phase 3: Platform Hardening & Deployment Scaling (9/9 plans) — external/clustered NATS, multi-node crypto invalidation, audit DLQ — completed 2026-07-10

Full phase details, requirements mapping, and success criteria: [milestones/v0.11-ROADMAP.md](milestones/v0.11-ROADMAP.md).
Phase execution artifacts: `milestones/v0.11-phases/`.

</details>

<details>
<summary>✅ v0.12 Foundation Hardening (v0.12 Phases 4–9) — SHIPPED 2026-07-28</summary>

> **Numbering:** these are **v0.12** phase numbers under the retired continuous global scheme. They are
> *not* the active milestone's Phases 1–6 and are deliberately left un-renumbered.

- [x] v0.12 Phase 4: World-Model Resilience Investigation & Decision (F1) (4/4 plans) — resilience/concurrency pass + the event-sourcing-vs-CRUD ADR (decision gate) — completed 2026-07-11
- [x] v0.12 Phase 5: World-Model Integrity Fixes (M2/M12) (16/16 plans) — version guard, transactional outbox, event-sourcing doc correction — completed 2026-07-13
- [x] v0.12 Phase 6: Operational Hardening & Assurance Gates (5/5 plans) — `events_audit` retention, nats CVE + vuln-scan gate, DLQ bridge, coverage reconciliation — completed 2026-07-15
- [x] v0.12 Phase 7: Event-Model & Bootstrap Decomposition (11/11 plans) — `core.Event`/`eventbus.Event` collapse, bootstrap→`lifecycle.Orchestrator`, gateway-boundary imports — completed 2026-07-18
- [x] v0.12 Phase 8: God-Object Decomposition (9/9 plans) — CoreServer + plugin/manager decomposition (behavior-preserving) — completed 2026-07-19
- [x] v0.12 Phase 9: Test-Quality & Code-Health Sweep (21/21 plans) — coverage-chain repair, ABAC fail-open fix, session-lifecycle matrix — completed 2026-07-27

Full phase details, requirements mapping, and success criteria: [milestones/v0.12-ROADMAP.md](milestones/v0.12-ROADMAP.md).
Phase execution artifacts: `milestones/v0.12-phases/`. Completion audit: [milestones/v0.12-MILESTONE-AUDIT.md](milestones/v0.12-MILESTONE-AUDIT.md).

**Closed as `override_closeout`:** v0.12 Phase 9 verified `gaps_found` (3/4) because QUAL-02/03/05 were
deliberately deferred with tracking (#4860, #4861, #4875, #4876, #4792); all other phases verified
`passed`. Carried forward: #4880 (CLAUDE.md event-construction rule defect), #4881 (no ruleset↔CI
reconciliation), #4882/#4883 (unquarantined flakes).

</details>

### 🚧 v0.13 Web Portal: Identity & Admin Foundations (Phases 1–6) — IN PROGRESS

**Milestone Goal:** Give web players a complete character identity surface — creation, management, and
public profiles with privacy — and stand up the admin portal shell that gives character administration a
home, with both designed to absorb the deferred portal surfaces without rework.

- [x] **Phase 1: Portal SPEC** — settle every shape decision whose cost explodes after code exists, and discharge PROJECT.md's Out-of-Scope precondition — completed 2026-08-01 (6/6 plans; `01-SPEC.md`, 16 sections; 9 amendments applied; 4 issues opened)
- [ ] **Phase 2: ABAC & Schema Vocabulary** — admin-section + public-profile policy, name normalization + unique index, character lifecycle column
- [ ] **Phase 3: World Character Commands** — domain-layer `RenameCharacter` + soft `RetireCharacter`, version-guarded and outbox-emitting
- [ ] **Phase 4: Shared Facade Helpers & `CharacterAccessService`** — one guest/ownership gate; character read/write BFF with privacy enforced by absence
- [ ] **Phase 5: Character Identity UI & Public Profiles** — creation identity card, multi-alt management, public profile page, per-field visibility
- [ ] **Phase 6: Admin Portal Shell & Character Administration** — ABAC-gated `/admin`, character administration, six deferred sections registered and denied-after-gate

## Phase Details

**Dependency spine.** The milestone opens on the **SPEC** (Phase 1) because eight of the fourteen
catalogued pitfalls are shape decisions — message shape, audience matrix, lifecycle vocabulary,
`expected_version` placement — whose cost explodes once code exists, and because PROJECT.md's Out-of-Scope
entry for non-scene portal surfaces named exactly this SPEC as its precondition. **Vocabulary before
surfaces** (Phase 2): privacy and admin-section policy must exist before anything reads or gates on them,
and the **character lifecycle column** and **normalized-name unique index** are each load-bearing for two
later phases, so they land once here rather than being rediscovered per consumer. **Domain before facade
before UI** (Phases 3 → 4 → 5) mirrors the shipped scenes path exactly. **Admin goes last** (Phase 6)
because it consumes the most and because `internal/web/` contains **zero `RoleAdmin` references today** —
a net-new trust boundary with no existing test suite that would notice if it were wrong.

Phase 3 is *planning*-parallelizable with Phase 2, but its `Rename` MUST NOT land before Phase 2's
unique index (adding a second writer to a live check-then-insert race), and its `Retire` needs Phase 2's
lifecycle column.

**Scheduling note (not a dependency):** `WebCheckSessionResponse.roles` (ADMIN-08) can be pulled into
Phase 4's proto work to avoid a second `web.proto` regeneration cycle in Phase 6. Likewise, Phase 4
should land the full SPEC-defined profile field set — including the PROFILE-06..09 fields that Phase 5
verifies — in one regeneration pass.

**Research posture.** Phases 3 and 6 carry `--research-phase` flags (below); Phase 2 needs a narrow
data audit only. Phases 1, 4, and 5 have well-understood patterns and should skip research — Phase 1 is
synthesis of `research/SUMMARY.md` rather than new research, Phase 4 is a verbatim copy of a fully-traced
shipped path (`internal/grpc/sceneaccess_service.go`), and Phase 5 is established shadcn/runes patterns
plus the existing `web/src/lib/scenes/createFlow.ts` idiom.

**PORTAL-10 is binding on every phase.** The six verification-integrity rules (census with set equality;
paired positive controls on every denial test; assertions against marshaled response bytes; gates
demonstrated RED against the pre-fix state; wire-level assertion of every opacity and authorization contract; invariant-scope
discipline) are SPEC content, not a capability — but every phase plan below carries them as acceptance
criteria. v0.12's audit catalogued 17 instances of *"a verification that cannot fail"*, and research found
that the natural test for nearly every privacy and authorization property in this milestone **passes while
the property is false**.

### Phase 1: Portal SPEC

**Goal**: Produce the committed portal SPEC that fixes the audience/message shape, character data and lifecycle model, per-field privacy model, media-ready profile schema, and the full new RPC surface — the precondition PROJECT.md's Out-of-Scope entry demanded, satisfied rather than waived.
**Depends on**: Nothing (opens the milestone)
**Requirements**: PORTAL-01, PORTAL-02, PORTAL-03, PORTAL-04, PORTAL-05, PORTAL-06, PORTAL-07, PORTAL-08, PORTAL-09, PORTAL-10
**Success Criteria** (what must be TRUE):

1. The SPEC names an **audience matrix** (public / owner / admin) with a distinct message shape per audience, such that a field a viewer may not see is **absent from the response** rather than present-and-hidden by the client — and backs it with a **read-surface inventory** enumerating every character-returning RPC, including all **four** existing public export surfaces — the fourth is `WebListPublishedScenes`, whose `participants_snapshot` is a frozen participant projection served unauthenticated in bulk — with the audience each serves.
2. The SPEC fixes the character **lifecycle** as three distinct operations — `retire`, `idle-out`, `purge` — states in normative language that **retire MUST NOT release the name**, and carries a **name-capture surface inventory** giving each denormalized-name site (immutable event payloads, `scene_log` via `WebGetPublicSceneArchive`) a historical-vs-live verdict.
3. The SPEC defines the profile/media data model as `entity_properties` rows (`profile.*`, `profile.image.primary`, `profile.image.gallery.00..09`) with intrinsic values (`name`, `description`, lifecycle status, `version`) staying columns, and states character-name and player-username normalization as **two separate policies**.
4. Every mutation request message in the SPEC's RPC surface carries **`expected_version`**; the SPEC records role mutation as an **explicit exclusion** from character administration; and it answers "does any v0.13 surface sort or filter on a profile field?" with a stated verdict rather than silence.
5. The SPEC mandates the six **verification-integrity rules** (census set-equality, paired positive controls, marshaled-bytes assertions, gates demonstrated RED pre-fix, wire-level opacity assertions, invariant-scope discipline) as binding acceptance criteria that every later phase plan inherits.

**Plans**: 6/6 plans executed
**UI hint**: no

Plans:
**Wave 1**

- [x] 01-01-PLAN.md — SPEC skeleton + profile/media data model + profile visibility model, declared and registered (tracer)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 01-02-PLAN.md — audience matrix + per-audience message shape + exhaustive read-surface inventory

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 01-03-PLAN.md — character lifecycle + name-capture surface inventory + name normalization policy

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 01-04-PLAN.md — full RPC surface with `expected_version` + admin IA and exclusions + sorting/filtering verdict

**Wave 5** *(blocked on Wave 4 completion)*

- [x] 01-05-PLAN.md — verification-integrity rules + amendments/divergences table + applying the nine amendments

**Wave 6** *(blocked on Wave 5 completion)*

- [x] 01-06-PLAN.md — citation verification sweep + grounding trace + spec-location pointer edit + completeness gate

### Phase 2: ABAC & Schema Vocabulary

**Goal**: Land the authorization vocabulary, name policy, and schema primitives every later phase gates on — `admin_section:` + `seed:admin-section-access`, `seed:profile-public-read`, the character **lifecycle column**, and the **normalized-name unique index** — with no UI and no new RPCs, which is where an unverified assumption surfaces cheapest.
**Depends on**: Phase 1
**Requirements**: IDENT-06, IDENT-07, IDENT-08, IDENT-09, PROFILE-11, EXT-07
**Success Criteria** (what must be TRUE):

1. A character name that is visually identical to an existing one — differing only by NFKC-normalizable codepoints, `Cf` format characters, or a mixed-script confusable — is rejected server-side, and a name matching the configurable regex block list is rejected server-side, both at create and at rename.
2. Two concurrent attempts to claim the same normalized name against real Postgres cannot both succeed; the gate is **demonstrated RED against today's unindexed schema before the unique index lands**, and any pre-existing duplicates are detected and resolved by a one-shot job first (migrations forbid in-migration backfills).
3. Player usernames still reject non-ASCII input — the existing `^[a-zA-Z][a-zA-Z0-9_]*$` rule is pinned by a regression guard rather than re-implemented.
4. An off-location viewer can read a character's public properties and in-world description where `seed:player-character-colocation` previously **denied** it — shipped only after an audit establishes exactly which existing `parent_type='character' AND visibility='public'` rows and which existing character descriptions the widened policy exposes.
5. `seed:admin-section-access` permits an admin and denies a builder, a plain player, and a guest across **all seven section ids** — each denial paired with a positive control proving the subject would otherwise have been permitted, and the id list asserted by set equality — and an eighth section added later needs no new policy.

**Plans**: TBD
**UI hint**: no
**Research flag**: narrow slice only — the existing-public-character-property audit is a *data* question, not a design one, but it must be answered before `seed:profile-public-read` merges. Full `--research-phase` is not warranted.

**Sketch findings** (must be answered in this phase): **A1** — `characters.last_active_at` does not exist and cannot be derived (`sessions` rows are reaped; `session_connections.last_seen_at` is a gateway lease). Needs a durable column (epoch-ns `BIGINT`) + a §11.3 row permitting sort/filter. **A2** — the admin list sorts by the joined `players.username`, which §11.3 never enumerates; §11.3's `characters.player_id` row ("never an ordering") stays correct, add a new row. **D1 (DEFECT, route to `abac-reviewer`)** — §10.3 requires a planned-section refusal to reveal nothing about which sections exist, but §10.4 defines two distinguishable denial codes (`DENY_ADMIN_SECTION` vs `DENY_ADMIN_SECTION_UNREGISTERED`), giving a registry-enumeration oracle. §13 pins none of it though `INV-PRIVACY-9` does the same job for profiles. Source: `.planning/sketches/002-*/README.md`, `003-*/README.md`. **Name pipeline UI** (009): §6.1's four steps run before every check, so "is it taken" is asked about the *normalized key*. Three accepted cases still **rewrite** what the player typed; the winner (submit-and-report) accepts that because rename exists. The confusable message MUST NOT name the colliding character, and it is safe **only because names are public at the `anonymous` floor** — if a game raises that floor (§8.6 permits it) the message becomes an oracle. A name of only invisibles looks like an empty box to the player, so "please enter a name" needs different wording.

Plans:

- [ ] TBD (run `/gsd-plan-phase 2`)

### Phase 3: World Character Commands

**Goal**: `world.Service` gains `RenameCharacter` and soft `RetireCharacter` at the domain layer, both version-guarded and emitting through the transactional outbox in-transaction, with the `writeCommands` census row and taxonomy kind landed in the same change.
**Depends on**: Phase 1 (SPEC). Planning parallelizes with Phase 2; execution requires Phase 2's normalized-name unique index before `Rename` and its lifecycle column before `Retire`.
**Requirements**: IDENT-03, IDENT-04, IDENT-10
**Success Criteria** (what must be TRUE):

1. A player can rename their own character through the domain layer: the new name passes Phase 2's normalization and block-list policy, the write is version-guarded, and a `character.renamed` event carrying `{id, old_name, new_name}` reaches the outbox in the same transaction as the state change.
2. A retired character leaves active play with its record intact and **its name still reserved**, and the retirement is reversible — retire, idle-out, and purge stay three distinct operations, and the irreversible `DeleteCharacter` path (which cascades `entity_properties` and emits a tombstone) is untouched by the retire flow.
3. A stale `expected_version` on any new character mutation is rejected with the typed `WORLD_CONCURRENT_EDIT` signal rather than silently overwriting — v0.12's existing two-replica resilience harness, pointed at the new commands, passes.
4. The `writeCommands` census and the mutation taxonomy list the new commands in the same change that introduces them; the census meta-test fails if either is missing.

**Plans**: TBD
**UI hint**: no
**Research flag**: `--research-phase` recommended — the `writeCommands` census bijection semantics (`internal/world/mutator.go:78-100`) are genuinely unverified, and this repo has a documented history of plans failing on unverified seam assumptions.

**Sketch findings** (must be answered in this phase): **Where `last_active_at` is written** — session-store create is the seam; it MUST NOT be the lease-refresh path (`internal/session/session.go:485` `RefreshConnection`), which would make every character a hot write every lease interval. **Can admins rename at all?** §9.3's admin census has update/retire/unretire and no rename — if admins cannot, sketch 004's `Rename…` affordance is a dead end and the locked row must say so; if they can, that is a census addition. Source: `.planning/sketches/002-*/README.md`, `004-*/README.md`. **Rename is load-bearing for 009-A** — sketch 009 corrected "names are permanent" (FALSE; IDENT-03 ships rename) and the create UI's chosen shape depends on it; if rename slips or is gated, revisit 009. **Roster:** a non-`active` lifecycle MUST suppress the shipped session badge (`Active`/`Offline`), which is a *different vocabulary* from `characters.status`. Player self-retire is **not** specified — every retire path sketched is `AdminRetireCharacter`.

Plans:

- [ ] TBD (run `/gsd-plan-phase 3`)

### Phase 4: Shared Facade Helpers & `CharacterAccessService`

**Goal**: Extract `resolveAndGate`/`ownedCharacter` into one shared place, then build the `CharacterAccessService` BFF facade and its `WebCharacter*` proxies so character read and write reach the web with unauthorized fields absent from the marshaled response by construction.
**Depends on**: Phase 2 (ABAC vocabulary + `profile.*` convention), Phase 3 (domain commands)
**Requirements**: IDENT-02, IDENT-02a, PROFILE-03, PROFILE-04, PROFILE-05, PROFILE-10, EXT-06
**Success Criteria** (what must be TRUE):

1. The guest gate and ownership check exist in **exactly one place**, and a census with **set equality** over every character-returning RPC proves each one routes through it — so an RPC added later that skips the gate fails the test rather than shipping.
2. A field the viewer may not see is **absent from the marshaled response bytes** — asserted against the wire, not a populated Go struct — and a character whose profile is unreachable returns a not-found-equivalent, never "this profile is private".
3. The **game-configured, per-attribute viewer-tier floor** governs every profile field — visibility is configuration, not an owner control (v0.13 ships no player or character agency over it) — and the configuration cannot raise `name` or `pronouns` above the profile's own reachability floor; an unrecognized tier is denied by an exhaustive `switch` with `default: deny`.
4. An owner can edit prose profile fields and the in-world `characters.description` over the web, with over-cap input rejected server-side and the description write reaching the existing `world.Service.UpdateCharacterDescription` rather than a parallel path.
5. The profile read path is built **exclusively** from the viewer-filtered property slice — a direct `PropertyReader.ListByParent`/`PropertyRepository.ListByParent` call from the facade fails the build or the test — and the proto ships the media shape now, empty: `ProfileImage{media_id, alt_text, content_warning}` + `primary_image` + `repeated gallery [max_items = 10]`.

**Plans**: TBD
**UI hint**: no

**Sketch findings** (must be answered in this phase): **A3** — `AdminSearchCharacters` (§9.2) currently "searches names" (character names); the admin list needs it extended to player usernames. **A2's RPC half** — the list RPC must accept a sort key for the joined `players.username`. **Admin rename census decision** (see Phase 3). Source: `.planning/sketches/002-*/README.md`.

Plans:

- [ ] TBD (run `/gsd-plan-phase 4`)

### Phase 5: Character Identity UI & Public Profiles

**Goal**: Web players get the whole identity surface — a structured creation card replacing the name-only stub, one place to manage every alt, and a public profile page a logged-out visitor can read — plus the media-schema proof with no uploader.
**Depends on**: Phase 4
**Requirements**: IDENT-01, IDENT-05, PROFILE-01, PROFILE-02, PROFILE-06, PROFILE-07, PROFILE-08, PROFILE-09, PROFILE-10a, PROFILE-12, EXT-05, EXT-08
**Success Criteria** (what must be TRUE):

1. A player creates a character through a **structured identity card** (name, pronouns as its own field, concept, species, age, faction) instead of the name-only stub, and manages every one of their characters — including which is default — from one place.
2. A **logged-out visitor** loads a character's public profile at a stable URL and sees the in-world description alongside the public `profile.*` fields — rumors / RP-hooks, the volatile "Currently" line, the OOC RP-preferences block, and time zone — with blank fields hiding themselves and an initial-letter avatar placeholder where no image exists.
3. Profile and sheet are **separate surfaces** and the sheet ships **empty**; the profile action bar carries a named empty slot for web DMs rather than a dead "message this character" button.
4. A change to the game's viewer-tier configuration is what a logged-out visitor sees on the **next load** — the floor is evaluated at read time and never stamped onto a row, so no backfill exists; and both the retirement flow and the surface where a player authors profile fields **state in the UI** that privacy is not retroactive over already-published history.
5. One primary plus ten gallery image property rows insert through the **real schema** and read back, and an eleventh primary is rejected by `UNIQUE(parent_type,parent_id,name)` — demonstrating the "no migration later" claim rather than asserting it, with no uploader.

**Sketch findings** (design decided in sketches 007, 008, 009; read `.planning/sketches/MANIFEST.md` round-2 findings before planning): **profile = identity card, not a long-form page** (007-C) — a bounded card carrying portrait/name/pronouns/concept/description that is complete at any fill level, with long-form sections growing *below* it and simply absent when withheld. **The page MUST NOT explain its own sparseness** — §7.5 + §8.9 make a blank field and a withheld field indistinguishable, so no counts, no lock icons, no greyed sections; a sign-in invitation is legal only if **unconditional**, and 007-C ships none. **Under the seeded defaults `guest` and `player` render identically** (no §8.6 row seeds `player`), so any tier preview must derive distinct outcomes from the live floor set, not offer a hardcoded three-way toggle. **The gallery never renders in v0.13** — §7.3 ships the media model with zero upload behavior, so build the renderer but ship no empty "coming soon" slots. **Roster is sectioned** (008-B): `Playable` grid first with the create card, then `Not playable`; every card in the top grid is uniformly clickable. **A non-`active` lifecycle MUST suppress the session badge** — the shipped `Active`/`Offline` badge is *session* state and collides with `characters.status`. **Creation is submit-and-report** (009-A), no live availability check — it cannot be honest across check-and-insert. Source: `.planning/sketches/007-*/README.md`, `008-*/README.md`, `009-*/README.md`.

**Plans**: TBD
**UI hint**: yes

Plans:

- [ ] TBD (run `/gsd-plan-phase 5`)

### Phase 6: Admin Portal Shell & Character Administration

**Goal**: Stand up `/admin` as an ABAC-gated trust boundary with character administration as its working section, six deferred sections registered / gated / refusing **after** the gate, and audit emission with before-values on every admin mutation.
**Depends on**: Phase 2 (`admin_section:` vocabulary + `seed:admin-section-access`), Phase 4 (shared facade helpers). The authorization gate is the **first thing built in this phase**, before any section, so every subsequent section inherits it.
**Requirements**: ADMIN-01, ADMIN-02, ADMIN-03, ADMIN-04, ADMIN-05, ADMIN-06, ADMIN-07, ADMIN-08, EXT-01, EXT-02, EXT-03, EXT-04
**Success Criteria** (what must be TRUE):

1. A non-admin calling an admin RPC **directly, bypassing the route entirely**, is denied — the decision is ABAC on an `admin_section:` resource, never a bare `PlayerHasRole` lookup and never a route-guard or gateway decision — with the denial asserted **over the wire** — the mapped `status.Code(err)` plus a generic `status.Convert(err).Message()` in which no internal code string appears — the specific typed `DENY_*` code asserted with `errutil.AssertErrorCode`, and a paired positive control proving an admin would have been permitted.
2. An admin lists, searches, opens, and edits characters; the edit surface accepts only an explicit **field-mask allowlist that excludes roles**, and admin disable/delete moves a character through the **same lifecycle states** as player-initiated retire — the irreversible `DeleteCharacter` path is reachable from no player-facing button.
3. Every admin mutation emits its audit envelope **in the same transaction** as the state change, carrying the **before-values** and the acting **player** id, not only the character; the `events_audit` row is projected from that envelope by the asynchronous audit projection, which is the only writer to that table.
4. All six deferred sections (stats, players, moderation, audit, config, plugins) are registered, role-gated, and return `NOT_IMPLEMENTED` **after** the gate — a non-admin hitting one is *denied*, not told it is unimplemented — and a meta-test asserts **set equality** between the section registry and the authorization-descriptor set, so a section registered without a descriptor fails at compile time or at boot.
5. Admin navigation is filtered from the **registry contract**, not template `{#if}` blocks; the roles exposed on `WebCheckSessionResponse` change only what is drawn, and drawing a link the viewer may not use still results in a denial at the RPC.

**Plans**: TBD
**UI hint**: yes
**Research flag**: `--research-phase` recommended — there is no in-repo precedent for the web gateway making an admin decision (`internal/web/` has zero `RoleAdmin` references); `AssertOperatorAdmin`'s shape must be transposed across a different auth model, and the reserved-section descriptor mechanism needs a concrete fail-at-boot design.

**Sketch findings** (design decided in sketches 001–004; read `.planning/sketches/MANIFEST.md` locked-decisions table before planning): three-column frame with the admin nav **merging into the rail** at 768–1023px (001/C2); inline row actions, **no multi-select** (002/A); planned sections **navigable to a minimal empty state**, no gate trace, no scope preview (003/A); edit surface = Sheet with **managed-elsewhere first and collapsed**, `version` as header metadata, status as a **transition picker that never sends a status value** (004/C). **There is no delete in this portal** — §9.3 has no `AdminDeleteCharacter` and §4.4 forbids wiring purge to an admin button. **Open:** `+error.svelte` does not exist (#4903) and the not-found page must stay the *ordinary* one, or `/admin`'s indistinguishability is lost. Ten shadcn components need adding (`table`, `pagination`, `empty`, `alert`, `avatar`, `breadcrumb`, `skeleton`, `select`, `field`, `sonner`). **Round 2 (005, 006, 010):** the edit Sheet is a **380px right overlay** (005-A) at every band **except** `<768`, where one `@container vp (max-width:767px)` block turns it into a **bottom-sheet** (006-B) — `Sheet side="bottom"`, a prop at a breakpoint, not a second component. Its **grab handle promises drag-to-dismiss**: honor it or drop it. The Sheet stays an **overlay, never a route** — that deliberately keeps deep-linkable edit surfaces (and their not-found obligation) out of scope. The mutation loop ends in a **toast naming the RPC**, and the row updates **in place** rather than refetching. **Not-found** (010-B): one page for four kinds of miss, offering the viewer's *own* sections; copy is **`Home`** — never "Back to HoloMUSH" (branding INV-6; the game's name exists as `SettingConfig.DisplayName` but reaches no web surface). Indistinguishability is **per-viewer**, not global. Ship a meta-test asserting **exactly one** `+error.svelte` under `web/src/routes/` — a second boundary kills the property with nothing failing.

Plans:

- [ ] TBD (run `/gsd-plan-phase 6`)

## Progress

> **Reading this table.** Phase numbers **restart at 1 per milestone as of v0.13**; v0.11 and v0.12 used the
> retired continuous global numbering and are **not** renumbered. The `Phase` column is therefore only
> unique *within* a milestone — always read it together with the `Milestone` column (rows `1.`–`9.` under
> v0.11/v0.12 are archived phases; rows `1.`–`6.` under v0.13 are the active milestone).

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Channels Subsystem | v0.11 | 6/6 | In Progress|  |
| 2. Scenes Lineage Completion | v0.11 | 7/7 | Complete | 2026-07-09 |
| 3. Platform Hardening & Deployment Scaling | v0.11 | 9/9 | Complete | 2026-07-10 |
| 4. World-Model Resilience Investigation & Decision (F1) | v0.12 | 4/4 | Complete    | 2026-07-11 |
| 5. World-Model Integrity Fixes (M2/M12) | v0.12 | 16/16 | Complete    | 2026-07-13 |
| 6. Operational Hardening & Assurance Gates | v0.12 | 5/5 | Complete    | 2026-07-15 |
| 7. Event-Model & Bootstrap Decomposition | v0.12 | 11/11 | Complete    | 2026-07-18 |
| 8. God-Object Decomposition | v0.12 | 9/9 | Complete   | 2026-07-19 |
| 9. Test-Quality & Code-Health Sweep | v0.12 | 21/21 [^p9] | Complete | 2026-07-27 |
| 1. Portal SPEC | v0.13 | 0/TBD | Not started | - |
| 2. ABAC & Schema Vocabulary | v0.13 | 0/TBD | Not started | - |
| 3. World Character Commands | v0.13 | 0/TBD | Not started | - |
| 4. Shared Facade Helpers & CharacterAccessService | v0.13 | 0/TBD | Not started | - |
| 5. Character Identity UI & Public Profiles | v0.13 | 0/TBD | Not started | - |
| 6. Admin Portal Shell & Character Administration | v0.13 | 0/TBD | Not started | - |

[^p9]: All 21 plans executed, but plan 09-21 produced no SUMMARY (it performed the phase's only
unconditional push and opened PR #4874). Hence "20/21" appears in some records and "21/21" in others —
neither is exactly right: *executed* and *documented* diverge by one. Its five must_haves were re-derived
live and are recorded in `milestones/v0.12-phases/09-.../09-VERIFICATION.md`.

## Deferred (Not in This Roadmap)

See `milestones/v0.11-REQUIREMENTS.md` "v2 Requirements" for full detail. Deferred strategic
clusters now live as first-class parking-lot entries in the `## Backlog`
section below (Forums → 999.4, Discord → 999.5, non-scene web-portal
surfaces → 999.1/999.8) — route each through `/gsd-spec-phase` before
roadmapping.

## Backlog

Strategic clusters consolidated from the beads → GitHub Issues migration
(2026-07-09). Member-level detail: [`.planning/archive/beads/TRIAGE.md`](archive/beads/TRIAGE.md).
Promote an entry with `/gsd-review-backlog` when ready.

The 2026-07-11 L7 architecture review (PR #4807) filed 23 discrete issues #4784–#4806
(epic E1 #4806) that overlap the foundation clusters below; per-cluster `**Related
issues:**` lines cross-link them. The issues track the discrete work; these clusters carry
the strategic frame. Reviewed 2026-07-28 (`/gsd-review-backlog`): 18 entries kept, **999.9 removed** (see below).
Re-reviewed 2026-07-31 (`/gsd-review-backlog`): **17 entries kept, 999.3 removed** (see below).

> **2026-07-31 review — scope reconciliation.** This pass verified cluster contents against the
> tree rather than re-reading goal prose, and three entries were wrong:
>
> - **999.3 is REMOVED.** Its only real content was scene templates (SCENEFWD-01, descoped
>   2026-07-08). Filed standalone as **#4897** and dropped from the backlog as someday/maybe —
>   a one-item cluster, the same disposition 999.9's last item got via #4886. `holomush-5rh`'s
>   other children were already separately filed (#4643, #4728) or shipped (`.19` → v0.11 Phase 2).
> - **999.2 was 8 items and is actually 1.** Seven were Epic 10's *pre-implementation* breakdown
>   and shipped in v0.11 Phase 1; only full-text search remains. See its entry.
> - **999.11's per-scope counts had drifted** and buried CRYPTO (102 pending, 40% of the total)
>   as a "long tail". Recounted in its entry.
>
> **Generalized lesson — beads-migrated item counts are an UPPER bound, not a residual.** Epics
> were migrated as whole clusters *including* their build-the-schema/build-the-commands children,
> so an epic that was subsequently implemented still lists them as pending. The count is exactly
> the number that inflates when sizing a milestone. Re-verify against the tree before promoting
> any 999.x cluster sourced from the beads migration.

> **v0.13 spine (selected 2026-07-31, PARTIALLY PROMOTED 2026-07-31):** **999.1 Web Client Portal
> completion** was the only player-facing cluster with substantial *verified* unbuilt scope (6
> concrete items: offline/PWA #4803, wiki/help, character profiles, character creation UI, admin
> portal, DM web surface). `/gsd-new-milestone` promoted **two and a half of the six** into milestone
> v0.13 (Phases 1–6): character creation/management UI (`qve.15`, non-roster), public character
> profiles (`qve.9`, no avatars), and the **shell only** of the admin portal (`qve.10`) — taken so
> character administration has a home. **Still in the backlog:** offline/PWA (`qve.7`), wiki
> (`qve.8`), web DMs (`qve.17`), and the six non-character admin sections (which overlap 999.8).
> See the 999.1 entry below for the residual.

> **v0.12 outcome (shipped 2026-07-28):** milestone **v0.12 Foundation Hardening** (Phases 4–9, archived at
> `milestones/v0.12-ROADMAP.md`) pulled the bulk of **999.9** (architecture decomposition → ARCH-01..05) and
> **999.10** (code health & test quality → QUAL-01..05), and addressed the arch-review operational Highs
> (→ OPS-01..05) plus the F1 event-model decision (→ MODEL-01..04).
>
> **999.9 is now REMOVED — exhausted.** v0.12 delivered five of its six named goals; the sixth
> (focus-redirect hot-path cache) was split out to #4886 rather than keeping a one-item cluster alive. That
> issue is explicit that the path is **unprofiled**, and that closing it unmeasured is an acceptable outcome
> — a cache on an unmeasured path trades invalidation surface for an unmeasured gain.
>
> **999.10 is kept and is now the larger residual of the two.** QUAL-02/03/05 shipped only partially
> (#4860, #4861, #4792), and the repo-wide coverage gate QUAL-01 aimed at **does not exist** — no codecov
> context is required on `main` (#4875, #4876). Detail in `milestones/v0.12-REQUIREMENTS.md`.
>
> **999.13's arch-review overlap also closed** (#4785/#4786/#4787/#4790, all shipped as OPS-01..04); its
> DR/backup/KMS core is untouched and the cluster stays.

### Phase 999.1: Web Client Portal completion (BACKLOG — PARTIALLY PROMOTED to v0.13)

**Goal:** Round out the web portal beyond scenes: offline support, wiki/help pages, character profiles + creation/management UI, admin portal, and a web surface for 1:1 direct messages.
**Source:** beads migration — 7 item(s) incl. epic(s) `holomush-qve`; member list in TRIAGE.md
**Related issues:** arch-review F6 PWA/offline #4803 (overlaps the offline-support + web-surface goals).
**Promoted to v0.13 (2026-07-31):** `qve.15` character creation + management UI (non-roster scope — roster integration stays deferred to 999.6), `qve.9` public character profiles (per-field privacy; avatars deferred to 999.16), and `qve.10` admin portal **shell + character administration only**.
**Residual (still backlog):** `qve.7` offline support / PWA (#4803), `qve.8` wiki portal, `qve.17` web surface for 1:1 direct messages, and the six non-character admin sections — stats, player management, moderation, audit viewer, config editor, plugin management — which overlap 999.8 and ship in v0.13 as *registered, role-gated, `NOT_IMPLEMENTED`-after-the-gate* stubs so wiring one later replaces a handler body rather than adding a check.
**Requirements:** v0.13 portion → IDENT-*/PROFILE-*/ADMIN-*/EXT-* (see `REQUIREMENTS.md`); residual TBD
**Plans:** 0 plans (residual)

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.2: Channels — remaining scope (BACKLOG)

**Goal:** Full-text search over channel message history. **This is the only member of the
original 8-item cluster that v0.11 Phase 1 did not deliver.**
**Scope reconciled 2026-07-31** (the entry's own "verify against what v0.11 Phase 1 delivered"
caution, finally acted on). The other 7 beads members were Epic 10's *pre-implementation*
breakdown and shipped in v0.11 Phase 1 (10/10 plans, 2026-07-09), verified against the tree:
`.3` schema → `migrations/000001_channels.up.sql`; `.4` join/leave/list/say →
`commands.go:63-85`; `.5` channel types → `types.go`; `.6` moderation mute/ban/ops →
`mute`/`ban`/`kick`/`transfer` subcommands + `moderate-own-channel` policy; `.7` history +
replay-on-join → `history` subcommand; `.2` implementation plan → consumed by v0.11 Phase 1.
Only `.8` (full-text search) remains: no `tsvector`, `GIN`, or `pg_trgm` exists in
`plugins/core-channels/migrations/` (earlier greps for "search" matched `search_path`, the
Postgres schema setting — a false positive worth not repeating).
**Sizing caution:** beads epics were migrated as whole clusters *including* their
pre-implementation task breakdowns, so an implemented epic still lists "build the schema"
children. Item counts on any 999.x cluster sourced from beads migration are an UPPER bound,
not a residual — re-verify against the tree before sizing a milestone off them.
**Source:** beads migration — 8 item(s) incl. epic(s) `holomush-0sc`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.4: Forums (BACKLOG)

**Goal:** Forum boards/threads/posts with web UI, moderation, notifications, and in-game integration. No design exists yet — needs brainstorm + spec before planning (theme:social-spaces).
**Source:** beads migration — 9 item(s) incl. epic(s) `holomush-djj`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.5: Discord Integration (BACKLOG)

**Goal:** Discord bridge plugin: bot, channel bridging, OAuth account linking, notifications, presence sync. Depends on channels substrate + an unbuilt OAuth substrate (theme:social-spaces).
**Source:** beads migration — 8 item(s) incl. epic(s) `holomush-aqq`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.6: Character Rostering & Transfer (BACKLOG)

**Goal:** Roster characters and transfer them between players (epic holomush-gloh).
**Source:** beads migration — 1 item(s) incl. epic(s) `holomush-gloh`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.7: Inventory & Object Manipulation (BACKLOG)

**Goal:** Inventory and object-interaction model; design task first (epic holomush-ni99).
**Source:** beads migration — 2 item(s) incl. epic(s) `holomush-ni99`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.8: Admin Web UI & Config (BACKLOG — substrate consumed by v0.13)

**Goal:** Operator tools: /admin route, server stats, player management, config surface (epics holomush-g4pb + holomush-7nub; overlaps the web-portal admin page — consolidate at design time).
**Source:** beads migration — 3 item(s) incl. epic(s) `holomush-g4pb`; member list in TRIAGE.md
**Consumed by v0.13 (2026-07-31):** the `/admin` route, its ABAC trust boundary (`admin_section:` resource + `seed:admin-section-access`, which covers every future section at zero policy cost), the section registry, and the shared authorization helper. The "consolidate at design time" note this entry carried is now discharged — v0.13's Phase 1 SPEC is that design point.
**Residual (still backlog):** the six section *bodies* — server stats, player management, moderation, audit log viewer, config editor, plugin management. v0.13 ships each **registered, role-gated, returning `NOT_IMPLEMENTED` after the gate**, so implementing one later replaces a handler body rather than standing up a surface. Audit *emission* (with before-values) also lands in v0.13, so the deferred audit viewer inherits real history rather than launching empty.
**Requirements:** TBD (residual)
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.10: Code health & test-quality program (BACKLOG)

**Goal:** Codebase humanization/de-slop, ACE naming violations, weak/skeleton tests, security polish batch, coverage backfill on Phase-1.5 infra packages, session-lifecycle test matrix.
**Source:** beads migration — 8 item(s) incl. epic(s) `holomush-ec22`, `holomush-89o9`; member list in TRIAGE.md
**Related issues:** arch-review F7 coverage #4804 (overlaps the coverage-backfill goal).
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.11: Invariant registry backfill program (BACKLOG)

**Goal:** Bind pending INV-* registry entries per scope, migrate INV-DOCS/INV-BRANDING scopes, reclassify entries that fail the invariant bar (epic holomush-hz0v4).
**Scope (recounted 2026-07-31 from `docs/architecture/invariants.yaml` — 243 pending, 103 bound, 346 total):**
CRYPTO 102 · SCENE 60 · PLUGIN 29 · EVENTBUS 19 · ACCESS 8 · STORE 8 · TELEMETRY 8 ·
COMMAND 3 · SESSION 3 · CLUSTER 1 · COMM 1 · PRESENCE 1. CHANNEL/PRIVACY/WORLD are fully bound.
(Sum checks: 102+60+29+19+8+8+8+3+3+1+1+1 = 243.) INV-BRANDING and INV-DOCS are *declared*
scopes carrying `status: pending` with ZERO numbered entries — they contribute nothing to the
count above; migrating them is separate work from binding entries, hence both goals.
**CRYPTO is 42% of the remaining work** and is the correct entry point, not a long-tail
afterthought — an earlier version of this entry led with SCENE and buried crypto, and also
carried stale PLUGIN/EVENTBUS counts (39/28) that had since been partially backfilled.
Recount before scoping; these numbers move under normal test work. **Anchor the pattern**
(`^\s+binding:\s*pending`) — an unanchored count also matches 11 YAML comments that discuss
pending bindings in prose, which is how this entry first shipped "254".
**Source:** beads migration — 11 item(s) incl. epic(s) `holomush-hz0v4`, `holomush-s6wp`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.12: Observability & vendor-neutral telemetry (BACKLOG)

**Goal:** Vendor-neutral error/telemetry/metrics abstraction at every seam (epic holomush-ionvr), error-event seam design, signal-hygiene so benign conditions stop masquerading as ERROR/WARN.
**Source:** beads migration — 3 item(s) incl. epic(s) `holomush-ionvr`, `holomush-yxfbi`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.13: Ops & deployment resilience (BACKLOG)

**Goal:** Disaster recovery + backup/restore guides, background DB sync to object storage, gateway-survival deploy strategy, Tailscale admin access, remote KMS substrate (VaultTransitProvider + rotation CLIs).
**Source:** beads migration — 6 item(s) incl. epic(s) `holomush-aub5`; member list in TRIAGE.md
**Related issues:** none open. The arch-review overlap (F2 gateway OOM #4785, F3 DLQ #4787, F4 events_audit unbounded #4786, F8 nats CVE #4790) all shipped in v0.12 as OPS-01..04 and is closed — this cluster's remaining scope is the DR/backup/object-storage/Tailscale/KMS core, which is untouched.
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.14: Platform & security design seeds (BACKLOG)

**Goal:** Design-needed platform work: load/perf harness + SLOs, feature-flag system, audit-backfill CLI, audit drift detector, KEK fail-closed decision, plugin scene-metadata privacy decision, comm event-type extensibility, plugin hostfunc authorization, ABAC fair-share timeout + debug endpoint.
**Source:** beads migration — 9 item(s); member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.15: Documentation program (BACKLOG)

**Goal:** Comprehensive features/usage/admin/operator/player docs under site/docs, consolidated system-design documentation, session-lifecycle diagram, unified in-game + website help system.
**Source:** beads migration — 4 item(s) incl. epic(s) `holomush-k7qy`, `holomush-rm9g`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.16: Feature wishlist (BACKLOG)

**Goal:** Player/operator-facing capabilities awaiting prioritization: rich text (markdown + emoji), operator-defined color themes, interface-backed content/blob storage, plugin-authoring Claude Code skill.
**Source:** beads migration — 4 item(s); member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.17: iOS Client (stretch) (BACKLOG)

**Goal:** Native iOS client (Epic 13) — stretch goal; depends on stable web/API surface.
**Source:** beads migration — 1 item(s) incl. epic(s) `holomush-5g6`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.18: Release process coherence (BACKLOG)

**Goal:** Review release procedures end-to-end and make them coherent: consider restoring
release-please (or keeping cog — evaluate, don't assume), align the release flow with GSD
practices/idioms (milestone close ↔ release cut, labels tracking cog-computed semver per
PROJECT.md Key Decisions), and produce better release notes than the current
GoReleaser-generated ones. Not necessarily all one tool, but something coherent.
**Source:** captured 2026-07-11 at v0.11 milestone close (milestone-relabel session — the
v1.0/v0.11 label drift and the GSD-tagging/cog collision motivated this review)
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.19: Restore lefthook + speed up the inner loop (BACKLOG)

**Goal:** Now that the repo is back on native git only (jj retired), restore lefthook git
hooks (worktree creation currently warns "no lefthook config found") and look for further
inner-loop speedups. Investigate: reinstate a lefthook config so `task workspace:new`
worktrees auto-install hooks (pre-commit fmt/lint, commit-msg conventional-commit check to
match CI's PR-title gate), and profile the `task pr-prep` fast lane / `task lint` / `task
test` cycle for wins (caching, scoping, parallelism). Aim: tighter edit→check feedback.
**Source:** captured 2026-07-11 at v0.11 milestone close (multiple worktree sessions this
day emitted "No lefthook config" warnings on every commit)
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)
