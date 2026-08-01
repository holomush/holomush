# Requirements: HoloMUSH — v0.13 Web Portal: Identity & Admin Foundations

**Defined:** 2026-07-31
**Core Value:** Players can play HoloMUSH end-to-end — create characters, communicate, and roleplay in
scenes — through either telnet or the web client, with every access-control decision default-deny and
every plugin (Lua or binary) trusted identically by the host.

**Milestone goal:** Give web players a complete character identity surface — creation, management, and
public profiles with privacy — and stand up the admin portal shell that gives character administration a
home, with both designed to absorb the deferred portal surfaces without rework.

**Research basis:** `.planning/research/SUMMARY.md` (+ `STACK.md`, `FEATURES.md`, `ARCHITECTURE.md`,
`PITFALLS.md`), 2026-07-31. Every `path:line` citation below was verified in-tree at
`gsd/v0.13-milestone`.

---

## v1 Requirements

Requirements for milestone v0.13. Each maps to exactly one roadmap phase.

### Portal specification (PORTAL)

The opening phase produces the SPEC that PROJECT.md's Out-of-Scope entry named as its precondition.
Eight of the fourteen catalogued pitfalls are SPEC-phase decisions whose cost explodes once code exists.

- [ ] **PORTAL-01**: The SPEC defines the **audience matrix** (public / owner / admin) and a distinct
      message shape per audience, such that a field a viewer may not see is **absent from the response**
      rather than present-and-hidden by the client.
- [ ] **PORTAL-02**: The SPEC contains a **read-surface inventory** enumerating every character-returning
      RPC — including all **four** existing public export surfaces, the fourth being
      `WebListPublishedScenes` whose `participants_snapshot` is a frozen participant projection
      served unauthenticated in bulk — with the audience each serves.
- [ ] **PORTAL-03**: The SPEC contains a **name-capture surface inventory**, giving each site a
      historical-vs-live verdict (display names are denormalized into immutable event payloads and
      `scene_log`, the latter served publicly by `WebGetPublicSceneArchive`; there is no update path).
- [ ] **PORTAL-04**: The SPEC defines the character **lifecycle states** with `retire`, `idle-out`, and
      `purge` as three distinct operations, and states that **retire MUST NOT release the name**.
- [ ] **PORTAL-05**: The SPEC defines the profile/media data model as `entity_properties` rows
      (`profile.*`, `profile.image.primary`, `profile.image.gallery.00..09`), keeping intrinsic columns
      (`name`, `description`, lifecycle status, `version`) as columns.
- [ ] **PORTAL-06**: The SPEC defines the full new RPC surface with **`expected_version` on every mutation
      request message** (adding it later is a wire-compat change to every caller).
- [ ] **PORTAL-07**: The SPEC states the name-normalization policy for character names and for player
      usernames as **two separate policies** (see IDENT-06/IDENT-07).
- [ ] **PORTAL-08**: The SPEC records an **explicit exclusion**: role mutation is not part of character
      administration in this milestone. An omission is not an exclusion.
- [ ] **PORTAL-09**: The SPEC answers whether **any v0.13 surface sorts or filters on a profile field**
      (recommended answer: no — property rows are not cheaply sortable, and public surfaces should not
      sort or filter on privacy-bearing fields at all).
- [ ] **PORTAL-10**: The SPEC mandates the **verification-integrity rules** below, and every phase plan
      carries them as acceptance criteria. They are recorded here as SPEC content rather than as
      capability requirements, but they are binding.

  v0.12's audit catalogued 17 instances of *"a verification that cannot fail."* Research found that the
  natural test for nearly every privacy and authorization property in this milestone **passes while the
  property is false** — a private-field test passes on an empty fixture; a per-endpoint leak test cannot
  detect the endpoint nobody wrote a test for; a denial test passes when its subject would have been
  denied anyway; a "reserved section" nav test passes vacuously when no sections exist.

  1. **Census with set equality** over every character-returning RPC — a per-endpoint suite is
     structurally incapable of detecting a missing member of its own set.
  2. **Paired positive control** on every denial test, proving the subject would otherwise have been
     permitted.
  3. **Assertions against marshaled response bytes**, not a populated Go struct — a field cleared only
     in the client must not be able to pass.
  4. **Gates demonstrated RED against the pre-fix state** — specifically, the name-uniqueness gate
     against the current unindexed schema before the index lands.
  5. **Wire-level assertion of every opacity and authorization contract** — the mapped
     `status.Code(err)`, a generic `status.Convert(err).Message()` with no internal code string in
     it, and a differential two-case assertion where the contract is indistinguishability.
     `errutil.AssertErrorCode` stays correct for asserting *which* internal code a handler produced,
     but is not evidence about what the wire carried: under the pinned `samber/oops v1.22.0` both it
     and `oops.AsOops(err).Code()` resolve the **deepest** code in the chain, so both pass on a
     double-wrap. (Amended — see `01-SPEC.md` §12.1 and §14 row 8; issue **#4902**.)
  6. **Invariant-scope discipline** — allocate in an existing scope (`ACCESS`, `PRIVACY`) or declare a
     boundary; never ad-hoc `INV-PROFILE-*` / `INV-ADMIN-*`, and ship `binding: pending` rather than
     fabricating a `// Verifies:`.

### Character identity — creation & management (IDENT)

- [ ] **IDENT-01**: A player can create a character through a **structured identity card** (name,
      pronouns as its own field, concept, species, age, faction), replacing the current name-only stub.
- [ ] **IDENT-02**: A player can edit their character's prose fields — appearance, personality,
      biography — with **server-enforced length caps**.
- [ ] **IDENT-02a**: A player can edit their character's **in-world description** (the "look at" text) —
      the intrinsic `characters.description` column, already served by
      `world.Service.UpdateCharacterDescription` (`internal/world/service.go:799-836`). This is a
      *column*, not a `profile.*` property row, and is distinct from the profile prose fields above.
- [ ] **IDENT-03**: A player can **rename** their own character.
- [ ] **IDENT-04**: A player can **soft-retire** their own character; the character leaves active play,
      its record and name are preserved, and the operation is reversible.
- [ ] **IDENT-05**: A player can manage **all of their characters from one place** (multi-alt
      management), including which is default.
- [ ] **IDENT-06**: Character names permit non-Latin scripts but are normalized with **NFKC**, stripping
      of `Cf` format characters, and a **confusable/mixed-script rule**, so a visually-identical name
      cannot impersonate an existing character.
- [ ] **IDENT-07**: Character names are additionally checked against a **configurable block/disallow list
      of regular expressions**, evaluated server-side at both create and rename.
- [ ] **IDENT-08**: Player usernames remain **ASCII-only** — a regression guard pinning the existing
      `^[a-zA-Z][a-zA-Z0-9_]*$` rule (`internal/auth/player.go:31`), not new validation.
- [ ] **IDENT-09**: A **unique index on a stored normalized character name** lands **before or with**
      `Rename`, closing the check-then-insert race that exists today across one shared existence
      query (`internal/bootstrap/setup/adapters.go:38-50`) and **two** writers —
      `internal/auth/character_service.go:112-121` and `internal/auth/guest_service.go:227`, the
      latter calling the same `ExistsByName` inside the guest-name retry-on-collision loop. `Rename`
      takes the writers from two to three. Pre-existing duplicates are detected and resolved by a
      one-shot job first (migrations forbid in-migration backfills), and that audit MUST cover the
      guest path, which provisions characters automatically and at volume. (Amended — see
      `01-SPEC.md` §6.1.3 and §14 row 7.)
- [ ] **IDENT-10**: Every new character mutation carries **`expected_version`** (migration `000049`) and
      emits through the **transactional outbox in-transaction**, preserving v0.12's MODEL-03/04
      guarantees.

### Public profiles & per-field privacy (PROFILE)

- [ ] **PROFILE-01**: A character has a **public profile page at a stable URL** that renders correctly
      for a logged-out visitor, with blank fields hiding themselves and an initial-letter avatar
      placeholder.
- [ ] **PROFILE-02**: **Profile and sheet are separate surfaces.** The split ships; the sheet ships
      **empty** (mechanical stats require a system that does not exist).
- [ ] **PROFILE-03**: Each profile field carries **server-enforced visibility** of `public` or `private`,
      with sane defaults. Enforcement is by omission from the response, never client-side hiding.
- [ ] **PROFILE-04**: **Profile reachability is its own facet** above the fields: a private profile
      returns a not-found-equivalent, never "this profile is private" (which leaks existence).
- [ ] **PROFILE-05**: **Name and pronouns cannot be set private** — they are the minimum public identity.
- [ ] **PROFILE-06**: The profile carries a **rumors / RP-hooks** field.
- [ ] **PROFILE-07**: The profile carries a short, volatile **"Currently"** status line.
- [ ] **PROFILE-08**: The profile carries an **OOC RP-preferences block** (style, availability, content
      limits, walk-up-friendly).
- [ ] **PROFILE-09**: The profile carries a **time zone** field, supporting the availability half of
      OOC preferences.
- [ ] **PROFILE-10**: The public profile page is built **exclusively** from the viewer-filtered property
      slice; the facade MUST NOT call `PropertyReader.ListByParent` / `PropertyRepository.ListByParent`
      directly (unfiltered by construction).
- [ ] **PROFILE-10a**: The public profile **also renders the character's in-world description** (the
      "look at" text, `characters.description`) alongside the `profile.*` property fields — so a web
      visitor sees what someone standing in the same location would see. Because this is an intrinsic
      column with no per-row `visibility`, its visibility handling is a **SPEC decision** (PORTAL-05):
      either it gains a paired visibility property or it is always public on the profile. It MUST NOT
      default to visible by accident.
- [ ] **PROFILE-11**: One new seed policy (`seed:profile-public-read`) permits off-location profile
      reads, which `seed:player-character-colocation` currently **denies**. Its scope covers **both**
      public `entity_properties` rows **and** the `characters.description` column (PROFILE-10a). It ships
      only after an audit of existing rows where `parent_type='character' AND visibility='public'`, and
      of existing character descriptions, because the policy widens read access to all of them.
- [ ] **PROFILE-12**: The retirement flow and the surface where a player authors profile fields
      **state in the UI** that privacy is not retroactive over already-published history. (Amended —
      the visibility toggle this originally named does not exist; visibility is game configuration,
      not an owner control. See `01-SPEC.md` §14 row 3. The retirement half is unchanged.)

### Admin portal shell & character administration (ADMIN)

- [ ] **ADMIN-01**: `/admin` exists, is **`RoleAdmin`-gated via ABAC** (`admin_section:` resource + seed
      policy) — never a bare `PlayerHasRole` lookup, and never a route-guard or gateway decision.
- [ ] **ADMIN-02**: **Every admin RPC re-asserts its own authorization gate** through one shared helper
      called first at every entry point, with typed `DENY_*` codes.
- [ ] **ADMIN-03**: An admin can **list and search characters**, view character detail, and edit
      character fields.
- [ ] **ADMIN-04**: The admin character-edit surface uses an explicit **field-mask allowlist that
      excludes roles**. Role mutation is out of scope for this milestone (PORTAL-08).
- [ ] **ADMIN-05**: Admin disable/delete reuses the **same lifecycle states** as player-initiated retire;
      the irreversible `DeleteCharacter` path (which cascades `entity_properties` and emits a tombstone
      in one transaction) is **never wired to a player-facing button**.
- [ ] **ADMIN-06**: **Every admin mutation emits an audit envelope** in the same transaction as its
      state change, recording **before-values** and the acting **player** id (not only the
      character). The `events_audit` row is **projected** from that envelope by the asynchronous
      audit projection, which is the only writer to that table — an admin mutation MUST NOT insert
      into `events_audit` directly. (Amended — see `01-SPEC.md` §10.7 and §14 row 9.)
- [ ] **ADMIN-07**: Admin navigation is **permission-filtered by registry contract**, not by template
      `{#if}` blocks.
- [ ] **ADMIN-08**: `WebCheckSessionResponse` exposes roles for **nav hiding only** — never as the
      authorization boundary.

### Extensibility headroom (EXT)

The milestone's defining constraint, made structural. These are separate REQ-IDs specifically so they
cannot be dropped as "nice to have" during planning.

- [ ] **EXT-01**: The admin section registry ships **seven entries** — `characters` available, and six
      **`planned`**: stats, players, moderation, audit, config, plugins.
- [ ] **EXT-02**: The six deferred sections ship **registered, role-gated, and returning
      `NOT_IMPLEMENTED` *after* the gate**, so wiring one later replaces a handler body rather than
      requiring someone to remember to add a check.
- [ ] **EXT-03**: A registry entry requires an **authorization descriptor with no default and no zero
      value meaning allow**; a section registered without one fails at compile time or at boot.
- [ ] **EXT-04**: A **meta-test asserts set equality** between the section registry and the descriptor
      set, so the extensibility guarantee is non-vacuous from day one.
- [ ] **EXT-05**: The media model is proven by **inserting 1 primary + 10 gallery property rows through
      the real schema** in v0.13, with no uploader — demonstrating the "no migration later" claim rather
      than asserting it. `UNIQUE(parent_type,parent_id,name)` enforces exactly-one-primary in the
      database.
- [ ] **EXT-06**: The proto ships the media shape now, empty — `ProfileImage{media_id, alt_text,
      content_warning}` + `primary_image` + `repeated gallery [max_items = 10]` — giving alt-text and
      content-warning somewhere to live before moderation exists.
- [ ] **EXT-07**: `seed:admin-section-access` covers all seven sections **and every future section at
      zero additional policy cost**.
- [ ] **EXT-08**: Deferred surfaces get a **named empty slot, not a dead affordance** — specifically, no
      "message this character" button on the profile until web DMs (`qve.17`) exist.

---

## v2 Requirements

Deferred. Tracked, with the seam v0.13 must leave for each.

### Profile & media

- **Avatar/gallery upload** (backlog 999.16) — v0.13 ships the schema and proto shape only, zero upload
  behavior.
- **Character sheet contents** — needs a mechanical stats system that does not exist.
- **Searchable character directory** — wants indexed, sortable structured fields, which property rows do
  not give cheaply. Additive and non-blocking provided PORTAL-09 answers "no".
- **Privacy presets** above the per-field matrix.
- **A `members` / `restricted` privacy tier in the UI** — the `entity_properties` CHECK already permits
  `restricted`, and `visible_to`/`excluded_from` already evaluate it; the tier is a string enum so
  surfacing it later is an enum append.

### Identity

- **Rostering & character transfer** (backlog 999.6) — must be modelled as a distinct transition *out of*
  retired, which is why retire must not release the name.

### Admin

- **The six deferred admin sections** (backlog 999.8) — stats, player management, moderation, audit
  viewer, config editor, plugin management. Registered and gated in v0.13; handler bodies later.
- **Audit log viewer** — the emission lands in v0.13 (ADMIN-06) precisely so the viewer has history.
- **Player-wide vs per-character role semantics** (**#4899**) — `PlayerHasRole`
  (`internal/store/role_store.go:83-93`) returns true iff *any* character of the player holds the role,
  so a role on a throwaway alt confers it everywhere. This is **deliberate and documented** in the
  operator/break-glass path (`internal/admin/auth/ingame.go:115` — *"RoleAdmin (any character)"*), not a
  defect; v0.13 is simply the first work that would load those semantics onto a new surface. Excluded
  from v0.13 by PORTAL-08/ADMIN-04, and tracked in #4899 rather than left as a silent omission. The
  decision must land before any admin surface exposes role mutation, because
  `WebCheckSessionResponse` needs a role field shaped to match and adding it later is a wire-compat
  change to every caller.

### Portal

- **Wiki portal** (`qve.8`) — external-link seam only in v0.13.
- **Web direct messages** (`qve.17`) — named empty slot in the profile action bar (EXT-08).
- **Offline / PWA support** (`qve.7`).

---

## Out of Scope

Explicitly excluded, with reasoning. Named anti-features come from the research pass.

| Feature | Reason |
|---------|--------|
| Freeform HTML/CSS in profiles | Samy-worm class; CSS alone still exfiltrates under CSP |
| Over-granular privacy matrices | Nextcloud's own docs concede their 3×4 cross-product confuses users |
| Per-character-pair visibility | IC-knowledge modelling wearing a privacy hat; not a privacy control |
| Relationship-web graphs | Consent, staleness, and N² privacy-filtered reads |
| Raw DB/SQL console in the admin panel | Bypasses every ABAC gate and all audit emission |
| Hardcoded break-glass admin identifiers | Unauditable standing privilege |
| Admin impersonation | Launders the audit trail — actions attribute to the wrong actor |
| Hard-delete on retire | Irreversible; FK references from `character_roles` (CASCADE), `scene_participants`, `player_character_bindings`, and `locations.owner_id`/`objects.owner_id` (no `ON DELETE` — errors at runtime) |
| Dashboard-first MVP with stub sections | A dashboard of empty cards is the rot pattern EXT-02 exists to prevent |
| Nav nesting deeper than 2 levels | Both surveyed admin-IA libraries explicitly reject it |
| Role mutation in character administration | PORTAL-08 — excluded while `PlayerHasRole` is player-wide |
| World/building editing surfaces | Still SPEC-less; unchanged from the PROJECT.md Out-of-Scope entry |
| `@testing-library/svelte`, `vitest-browser-svelte` | Repo already has a working `mount`/`unmount` component-test project (17 files); the latter is a whole-suite migration, filed separately |
| Any query-cache layer in the web client | Creates a second source of truth against the live `StreamEvents` push feed |
| Proto `reserved` ranges as an extensibility claim | All three researchers agree it is hygiene only; it MUST NOT appear in a REQ-ID as if it discharged the extensibility constraint |

---

## Traceability

Which phases cover which requirements. Filled during roadmap creation.

**Coverage: 50/50 v1 requirements mapped — every requirement maps to exactly one phase; no orphans, no duplicates.**

| Phase | Name | Requirements |
|-------|------|--------------|
| 1 | Portal SPEC | 10 (PORTAL-01..10) |
| 2 | ABAC & Schema Vocabulary | 6 (IDENT-06..09, PROFILE-11, EXT-07) |
| 3 | World Character Commands | 3 (IDENT-03, IDENT-04, IDENT-10) |
| 4 | Shared Facade Helpers & `CharacterAccessService` | 7 (IDENT-02, IDENT-02a, PROFILE-03/04/05/10, EXT-06) |
| 5 | Character Identity UI & Public Profiles | 12 (IDENT-01, IDENT-05, PROFILE-01/02/06/07/08/09/10a/12, EXT-05, EXT-08) |
| 6 | Admin Portal Shell & Character Administration | 12 (ADMIN-01..08, EXT-01..04) |

| Requirement | Phase | Status |
|-------------|-------|--------|
| PORTAL-01 | Phase 1 | Pending |
| PORTAL-02 | Phase 1 | Pending |
| PORTAL-03 | Phase 1 | Pending |
| PORTAL-04 | Phase 1 | Pending |
| PORTAL-05 | Phase 1 | Pending |
| PORTAL-06 | Phase 1 | Pending |
| PORTAL-07 | Phase 1 | Pending |
| PORTAL-08 | Phase 1 | Pending |
| PORTAL-09 | Phase 1 | Pending |
| PORTAL-10 | Phase 1 | Pending |
| IDENT-01 | Phase 5 | Pending |
| IDENT-02 | Phase 4 | Pending |
| IDENT-02a | Phase 4 | Pending |
| IDENT-03 | Phase 3 | Pending |
| IDENT-04 | Phase 3 | Pending |
| IDENT-05 | Phase 5 | Pending |
| IDENT-06 | Phase 2 | Pending |
| IDENT-07 | Phase 2 | Pending |
| IDENT-08 | Phase 2 | Pending |
| IDENT-09 | Phase 2 | Pending |
| IDENT-10 | Phase 3 | Pending |
| PROFILE-01 | Phase 5 | Pending |
| PROFILE-02 | Phase 5 | Pending |
| PROFILE-03 | Phase 4 | Pending |
| PROFILE-04 | Phase 4 | Pending |
| PROFILE-05 | Phase 4 | Pending |
| PROFILE-06 | Phase 5 | Pending |
| PROFILE-07 | Phase 5 | Pending |
| PROFILE-08 | Phase 5 | Pending |
| PROFILE-09 | Phase 5 | Pending |
| PROFILE-10 | Phase 4 | Pending |
| PROFILE-10a | Phase 5 | Pending |
| PROFILE-11 | Phase 2 | Pending |
| PROFILE-12 | Phase 5 | Pending |
| ADMIN-01 | Phase 6 | Pending |
| ADMIN-02 | Phase 6 | Pending |
| ADMIN-03 | Phase 6 | Pending |
| ADMIN-04 | Phase 6 | Pending |
| ADMIN-05 | Phase 6 | Pending |
| ADMIN-06 | Phase 6 | Pending |
| ADMIN-07 | Phase 6 | Pending |
| ADMIN-08 | Phase 6 | Pending |
| EXT-01 | Phase 6 | Pending |
| EXT-02 | Phase 6 | Pending |
| EXT-03 | Phase 6 | Pending |
| EXT-04 | Phase 6 | Pending |
| EXT-05 | Phase 5 | Pending |
| EXT-06 | Phase 4 | Pending |
| EXT-07 | Phase 2 | Pending |
| EXT-08 | Phase 5 | Pending |
