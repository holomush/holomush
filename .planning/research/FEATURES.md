# Feature Research

**Domain:** Character identity surfaces + staff admin portal for a text-RP (MUSH) community platform
**Researched:** 2026-07-31
**Confidence:** MEDIUM (findings cross-checked across 3+ independent primary sources per topic; see Sources)

**Scope note.** HoloMUSH already ships a web terminal, auth flows, a character *picker*
(`web/src/routes/(authed)/characters/+page.svelte`, 197 lines — select + a bare name-only create),
the full scenes/RP portal, and channels. This document covers ONLY the v0.13 new surfaces:
character creation/management, public profiles + sheets, per-field privacy, and the admin shell.

**Grounding already established in-tree** (used for complexity calls, not re-researched):

| Fact | Location |
|---|---|
| `characters` = `id, player_id, name, description, location_id, created_at, preferences JSONB` | `internal/store/migrations/000001_baseline.up.sql:68-75`, `000045_character_preferences.up.sql` |
| No status / retired / deleted column exists on `characters` | same |
| `characters.id` is FK-referenced (`character_roles`, `scene_participants`, `player_character_bindings`) and is stamped into audit events as actor id | `000001_baseline.up.sql:83-87,158`, `000015`, `000040` |
| Only `CreateCharacter` mutates; `Get/List/ListAll/SelectCharacter` are read-only | `api/proto/holomush/core/v1/core.proto:80-107`, `world/v1/world.proto:30` |
| Web routes today: `(authed)/{characters,scenes,terminal}` — no `/admin` | `web/src/routes/` |
| `reaping_at BIGINT` nullable is the in-tree precedent for a lifecycle-state column | `000051_player_reaping.up.sql` |

---

## Feature Landscape

### Table Stakes (Users Expect These)

Features users assume exist. Missing these = product feels incomplete.

#### A. Character creation + management

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Creation flow collecting a **structured identity card** (name, pronouns, one-line concept, species/race, age, faction/group) — not just a name | Every surveyed MUSH's chargen produces a `+finger` card before anything else. HoloMUSH's current create form asks for a name only; that reads as an unfinished stub. | MEDIUM | ~8 short scalar fields on `characters` (or a `character_profiles` side table). Pronouns MUST be its own field, separate from gender — modern RP games list them separately. |
| **Prose fields**: appearance/description, personality, background | The `@desc` + personality + background triad is universal. `description` already exists; the other two do not. | LOW | Server-side length caps are table stakes, not polish — every game caps app length ("there is a buffer limit on applications"). Pick caps now (suggest: appearance 4k, personality 4k, background 8k). |
| **Rename** an existing character | No mutation RPC exists today. Players expect to fix a typo'd name. | MEDIUM | Name is the identity/lookup key. Rename must be uniqueness-checked and MUST NOT release the old name for immediate re-registration (see Anti-Features). |
| **Edit description / profile fields** after creation | Descriptions change constantly in play (seasons, injuries, outfits). Frozen prose is broken. | LOW | Straight update RPC + ABAC owner check. |
| **Retire** a character as a *soft, reversible-for-a-window* state | Universal MU\* vocabulary. Players expect their retired character's history to survive. | MEDIUM | See "The retire/purge distinction" below — this is the single highest-risk modelling decision in the milestone. |
| **Multi-character (alt) management from one place** — list all my characters with per-character state | Alts are the norm, not the exception; every surveyed game has an alt policy. The picker already lists them; management does not. | LOW | Extend the existing picker page or add a sibling `/characters/manage`. |
| Creation-time **name validation + collision feedback** before submit | Existing flow surfaces errors only after a failed round-trip. | LOW | A `CheckCharacterNameAvailable` RPC, or return a typed error code the UI can render inline. |

#### B. Public profiles + sheets

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| A **public profile page per character** at a stable URL | The `+finger` is the single most-used social command on a MUSH; the web equivalent is a profile page. | MEDIUM | Route + read RPC + privacy filtering. Must render correctly for an anonymous (logged-out) visitor. |
| **Profile ≠ sheet as separate surfaces** | Surveyed games and World Anvil both separate them; World Anvil lets you publish the sheet without publishing the character. Conflating them forces a single visibility decision over two very different data shapes. | LOW *(as an IA decision)* / HIGH *(if the sheet is populated)* | **Ship the split, not the sheet.** Profile = identity card + prose. Sheet = a declared, empty tab for system/mechanical data. Populating it needs a stats system that does not exist. |
| **Blank fields hide themselves** on the rendered profile | Universal expectation (MRP: "blank sections won't appear"). A profile full of "N/A" reads as broken. | LOW | Pure render logic. |
| **Owner-edit affordance** on your own profile | Table stakes for any profile surface. | LOW | Reuse the same mutation RPCs as management. |
| **Created / last-active metadata** on the profile | Already surfaced in the picker (`lastPlayedAt`); expected on the profile too. | LOW | Data already exists. |
| **Initial-letter avatar placeholder** where an image would go | Prevents the profile looking broken pre-image-upload. | LOW | Already implemented in the picker (`+page.svelte:123-125`) — reuse verbatim. |

#### C. Per-field privacy

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| **Per-field visibility control** (not one global toggle) | Every comparable platform (BuddyPress, Nextcloud, Circle, Orgo, Jive) offers per-field, and a spec that started with a single `profile_public` boolean documented it as "too coarse for the realities of how observers want to share". | MEDIUM | Recommended granularity below. |
| **Server-side enforcement**; client visibility is a UX hint only | A privacy control that filters in the browser is not a privacy control. | MEDIUM | Maps cleanly onto HoloMUSH's existing default-deny ABAC: visibility tier becomes a **resource attribute** on `character-profile`, evaluated by the engine, not a `WHERE` clause in the UI. |
| **A whole-profile master switch** ("make my profile private") | Present in Orgo, Rastrum, BuddyPress. Users want one panic button, not 12 toggles. | LOW | Derived: sets every field to `private` and the profile-reachability facet to `members`. |
| **Sane defaults** so a new character is not accidentally exposed | Repeated advice: "When in doubt, default to private. It's easier to open up than to lock down after members have already been exposed." | LOW | Suggested defaults: name/pronouns/concept `public`; appearance/personality `public`; background `members`; OOC/contact fields `private`. |
| **Disclosure that privacy is display-only** | BuddyPress states it plainly: data stays in the DB and remains admin-visible. Silence here is a GDPR-shaped problem. | LOW | One sentence in the privacy UI + the docs. |

#### D. Admin portal

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| **`RoleAdmin`-gated `/admin` route** that is invisible to non-staff | Baseline. | LOW | `RoleAdmin` already exists (`internal/config/config.go`, `internal/admin/auth/ingame.go`). |
| **Dashboard landing with cards** | Universal across every surveyed game-server panel (GameAP, CFTools, HybridCore, mc-manage-panel). A bare redirect-to-first-section reads as unfinished. | LOW | Cards are contributed by registered sections; a section with no implementation contributes no card (see IA below). |
| **Character list with search** | The core admin verb for this milestone. `ListAllCharacters` already exists read-only. | MEDIUM | Needs pagination + search params; the current RPC likely returns everything. |
| **Character detail ("360° view") with admin edit** | HybridCore's phrase; the convergent shape is one page showing everything staff needs plus the actions. | MEDIUM | Reuses profile read + admin-scoped mutation RPCs. |
| **Disable / delete a character as staff** | The moderation minimum. | MEDIUM | Must reuse the same lifecycle states as player-initiated retire — do NOT mint a parallel staff-only deletion path. |
| **Every admin mutation emits an audit event** | Called out as the accountability core by CFTools ("complete audit trail of all admin actions") and MonoSuite ("Dispute? Click the log, see who, why, and what happened next"). | LOW *(now)* / HIGH *(retrofitted)* | **The audit *viewer* is deferred; the audit *emission* must not be.** If v0.13 ships admin mutations that emit nothing, the deferred viewer launches with an empty history and the gap is unrecoverable. Treat this as a v0.13 requirement. |
| **Permission-filtered navigation** | Every registry pattern surveyed carries a `perm`/`permissions` list on the nav entry. Showing a staffer a section they cannot open is a bug. | LOW | Make it part of the section-registry contract, not template `{#if}` logic. |

---

### Differentiators (Competitive Advantage)

Features that set the product apart. Not required, but valuable.

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| **Rumors / RP-hooks section** on the profile | The single highest-leverage RP-specific field. Both RP Repository and 'Souls stress it independently: hooks and (true *and false*) rumors are what actually generate scenes — "gives other characters many jumping-off points for RP." Nothing in the generic-social-platform playbook has this. | LOW | One more prose field with its own visibility tier. Enormous value-to-cost ratio; **strongest single differentiator in this milestone.** |
| **A volatile "Currently" status line** | Borrowed from WoW's MRP; a one-line "what's true right now" ("arm in a cast", "looking lost in the Bazaar"). Cheap, constantly used, and it makes profiles feel live rather than archival. | LOW | Short field, `public` by default, no privacy tier needed. |
| **OOC RP-preferences block** (style, availability, content limits, walk-up-friendly) | Directly addresses the #1 friction in text RP: finding a compatible partner. Surveyed guides treat it as best practice; few platforms make it a first-class field. | LOW | 3–4 short fields. Should default to `members` (it is OOC info). |
| **Searchable / filterable character directory** built from the structured profile fields | BuddyPress's insight: profile fields earn their keep by powering the directory. Turns identity data into discovery. Also gives the admin character-list and the public directory a shared query layer. | MEDIUM | Depends on the structured fields being real columns/indexes, not JSONB blobs. **Design the schema for this even if the public directory ships later.** |
| **Privacy presets** ("Open book" / "Standard" / "Private") above the per-field matrix | Rastrum's pattern. Most users never touch a 12-row matrix; a preset gets them to a sane state in one click and the matrix stays for the minority who want it. | LOW | Presets are just named field→tier maps. |
| **Retire with a written send-off** (an in-character exit note attached to the retired character) | Retirement is emotionally significant in RP communities — surveyed games negotiate it with staff and write characters out narratively. Making it a first-class, dignified action rather than a destructive one is genuinely differentiating. | LOW | One prose field on the retire mutation. |
| **Operator-level privacy defaults that players can only tighten** | Orgo's rule: "members can only restrict, never expand." Lets a game operator set theme-appropriate floors (e.g. "OOC contact info is never public on this game") without per-player work. | MEDIUM | Needs a settings surface + a floor-vs-preference evaluation. Fits the existing `setting` plugin type. Could be deferred to v0.14 — but the *evaluation order* (floor ∧ preference) should be in the model now. |
| **Section registry with declared-but-unimplemented sections** | This is the milestone's defining constraint made concrete: a section can be registered in a `planned` state — it appears in the IA and in docs, but renders a "coming in a future release" stub rather than 404ing or silently not existing. | MEDIUM | See the IA section. Directly satisfies the "named room for deferred sections" requirement in a testable way. |

---

### Anti-Features (Commonly Requested, Often Problematic)

Features that seem good but create problems.

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| **Freeform HTML / CSS in profiles** | Nostalgia for MySpace/SpaceHey-era expression; World Anvil ships it as "Hero Profile CSS". | Direct, historically-proven RCE-class exposure. The Samy worm (2005) rode exactly this feature: the blocklist stripped `<script>` and `javascript:` but permitted inline CSS, and CSS expressions + newline-splitting gave arbitrary JS and self-propagation to ~1M profiles. **CSS alone is still enough**: 2026 bug-bounty writeups exfiltrate CSRF tokens character-by-character via CSS-3 attribute selectors + `url()` background loads, *under a CSP that blocks JS*. Reddit disabled plain HTML in posts in 2026 for the same reasons. | Constrained markdown subset, server-side **allowlist** sanitization (never blocklist), strict CSP (`style-src 'self'`, `img-src 'self' data:`), and a small set of structured block types. If theming is ever wanted, ship a fixed palette of themes — not a CSS text box. |
| **Over-granular privacy matrix** (4+ tiers, or per-viewer / per-relationship rules) | "More control is more privacy." | Nextcloud is the case study: 3 profile-visibility settings × 4 property scopes, with its own docs conceding "effective visibility is the most restrictive result of all applicable controls." Nobody configures that correctly, and support cannot reason about it. Rastrum deliberately deferred a 4th tier it could have shipped for free, because "3 segments fit in a row; 4 wraps on narrow screens." | **3 tiers, per field.** See the recommendation below. Store the tier as an **enum/string, never a boolean**, so a 4th value is additive later. |
| **Per-character-pair visibility** ("only characters who have met mine can see this") | Feels like the right IC-fidelity answer. | It is IC-knowledge modelling wearing a privacy hat. It needs a met/knows graph (does not exist), it is unauditable, it makes every profile read a graph traversal, and the surveyed community advice is that publishing IC secrets *at all* is the mistake — not that they need finer gating. | Do not model IC knowledge in privacy. Keep the tiers OOC-audience-based (`public`/`members`/`private`) and advise players not to put IC secrets on profiles. |
| **Relationship-web visualization** (character graph / family tree) | Visually striking; obvious in an RP context. | Three compounding problems: (1) **consent** — character A asserting "sister of B" without B's agreement is a live dispute generator, so it needs bidirectional confirmation, i.e. a whole invite/accept subsystem; (2) it goes stale the moment a relationship changes IC and nobody updates it; (3) it is an N² read surface that must be privacy-filtered per edge *and* per endpoint, multiplying the privacy model's blast radius. | If relationship data is wanted at all: an **unlinked, freeform "Ties" prose field** on the profile (self-asserted, no claims about other records, no graph). Revisit a real graph only with an explicit consent design. |
| **Raw DB browser / SQL console in the admin panel** | Genuinely convenient for staff; a shipped FiveM panel offers it. | That panel then has to blocklist `DROP`/`TRUNCATE`/`ALTER` and force `LIMIT 1` on every `UPDATE` — a blocklist defending the whole database. Worse for HoloMUSH: it **bypasses every ABAC gate and every audit emission the platform is built on**, and violates the gateway-boundary rule outright. | Typed admin RPCs, one per operation, each ABAC-gated and each emitting an audit event. If bulk work is needed, a CLI subcommand on the admin socket — not the web panel. |
| **Hardcoded "emergency back door" admin identifiers** | Lockout fear is real; a shipped panel offers this as a documented third auth layer. | A permanent, unauditable, un-revocable credential in config. Flatly contradicts default-deny ABAC. | Break-glass via the existing admin socket path (`internal/admin/socket/`), which is already peer-cred-gated and audited. |
| **Admin impersonation / "log in as this player"** | Excellent for support reproduction; HybridCore ships it. | Launders the audit trail — actions land attributed to the player, destroying exactly the accountability the audit log exists for. | Defer. If ever built, every emitted event needs an `on_behalf_of` field alongside `actor`, designed in from the start — which is a reason to leave room in the audit actor shape now. |
| **Hard-deleting a character on retire** | "Retire" sounds terminal; it frees the name. | `characters.id` is FK-referenced by `character_roles`, `scene_participants`, and `player_character_bindings`, and is stamped as the actor on audit events. A hard delete orphans scene history and audit provenance. The community vocabulary is also explicit that these are *three different operations* (retire ≠ idle-out ≠ purge). | Status column. Retire = soft-hide. Name release is a **separate, later, staff-initiated purge** decision — out of scope for v0.13, but the status enum should have room for it. |
| **Dashboard-first MVP** (build the shell + cards, wire sections later) | It demos beautifully. | A dashboard whose every card links to a stub is a screenshot, not a portal — and it defers all the hard integration questions (ABAC on admin actions, audit emission, pagination) past the point where the IA is frozen around them. | Ship the shell **plus one fully-working section** (character administration). The registry is proven by having one real entry and N planned ones. |
| **Three-or-more-level nav nesting** | Anticipating many future sections. | Both surveyed registry implementations cap at two levels and one *explicitly rejects* nesting ("nested dropdowns are not supported"). Deeper trees hide things and break mobile. | Two levels: group → section. If a group exceeds ~7 sections, split the group. |
| **Unbounded prose fields** | "Let people write as much as they want." | Every surveyed game caps application length in practice and staff routinely send apps back for being too long. Uncapped fields also break list/preview rendering, blow up payload sizes, and become a DoS surface. Community advice is also explicit that huge histories belong on a wiki, not a profile. | Per-field server-enforced caps, with the count shown in the editor. Leave a `wiki_url` / external-link seam for the deferred wiki. |

---

## Feature Dependencies

```
[Character lifecycle status column]
    └──required-by──> [Retire / un-retire]
    └──required-by──> [Admin disable/delete]
    └──required-by──> [Profile visibility of retired characters]

[Structured profile fields on characters]
    └──required-by──> [Public profile page]
    └──required-by──> [Per-field privacy]   (nothing to gate without fields)
    └──enables─────> [Character directory / admin search]

[Per-field visibility tier enum]
    └──required-by──> [Public profile page for anonymous viewers]
    └──requires────> [ABAC resource attributes for character-profile]

[Admin section registry]
    └──required-by──> [Admin dashboard cards]
    └──required-by──> [Permission-filtered nav]
    └──required-by──> [Declared-but-planned deferred sections]

[Admin mutation RPCs]
    └──must-emit───> [Admin audit events]  (even though the VIEWER is deferred)

[Profile media schema: 1 primary + ≤10 gallery refs]
    └──deliberately-NOT-required-by──> anything in v0.13
        (columns exist; no v0.13 requirement may read, write, or render them)

[Retire]  ──MUST NOT imply──>  [Roster / character transfer]   (deferred, 999.6)
[Profile] ──MUST NOT link to──> [Web DMs]                       (deferred, qve.17)
[Profile prose] ──MUST NOT assume──> [Wiki]                     (deferred, qve.8)
```

### Dependency Notes

- **Per-field privacy requires the profile fields first.** A privacy model built before the field
  list is fixed will either over-generalize (a JSONB free-for-all that cannot be indexed for the
  directory) or under-generalize (hardcoded per-field logic). Fix the field list, *then* the tiers.
- **Retire requires the status column, and the status column constrains everything downstream.**
  Deciding it late means the profile page, the admin list, the picker, and the directory each grow
  their own ad-hoc "is this character still real?" predicate.
- **Admin audit *emission* must land with the admin mutations**, not with the deferred audit viewer.
  This is the one place where a deferred feature imposes a v0.13 obligation.
- **`retire` conflicts with `roster`** in the sense that the naive implementation forecloses it.
  Rostering (deferred, 999.6) is "this character becomes available for another player to pick up" —
  a *transfer of ownership*. If retire is implemented as "release ownership", rostering has nowhere
  to go and retire becomes irreversible. Retire must be `owner unchanged, visibility hidden,
  playability off`. Rostering later adds a distinct transition out of that state.
- **The media schema is a pure-schema dependency in the reverse direction:** the columns must exist
  so no migration is needed later, and *nothing* may depend on them. Any requirement phrased "profile
  shows the character's image" is a scope violation — the correct phrasing is "profile reserves a
  media slot rendered as an initial-letter placeholder."

---

## Recommended privacy granularity (committed)

**Recommendation: three tiers, per field, stored as a string enum, enforced by ABAC.**

```
public   — anyone, including logged-out visitors and crawlers
members  — any authenticated player of this game
private  — the owning player (and staff, per the display-only caveat)
```

**Why three, and why these three:**

1. **Four wraps.** A 3-way segmented control fits one row on mobile; a 4th tier forces a dropdown or
   a wrapped control across every row of the matrix. This is the stated reason a comparable spec
   deferred a 4th tier it could have shipped for free.
2. **HoloMUSH has no social graph.** A `friends`/`followers` tier — the usual 4th — would require
   inventing a friend/follow subsystem that is in neither this milestone nor the deferred list.
   Borrowing scene participation or channel membership as a proxy would make visibility depend on
   in-game activity, which is IC-knowledge modelling (see Anti-Features).
3. **`public` vs `members` is the load-bearing distinction**, and the only one users reliably
   understand: "can a stranger with a link see this?" Everything finer is guessed at.
4. **The tier is a string enum, not a boolean** — this is the extensibility hook. Adding
   `friends` or `staff_only` later is a new enum value plus a new ABAC rule; no schema migration,
   no data backfill, no UI rewrite (the segmented control becomes a select).
5. **Enforcement belongs in ABAC, not in the query.** The tier becomes a resource attribute on a
   `character-profile` resource; the existing default-deny engine decides. This inherits fail-closed
   behavior for free and keeps the gateway a translation layer. The Svelte side filters only for
   presentation.

**Explicitly rejected:** 5-tier (BuddyPress-style, needs a friends component), scope×visibility
cross-products (Nextcloud-style, self-admittedly confusing), per-viewer relationship rules,
and per-character-pair rules.

**Two facets deserve care beyond the field matrix:**

- **Profile reachability** is its own facet, above the fields. If it is `private` and the viewer is
  not the owner, return a not-found-equivalent — not an empty profile. Rendering "this profile is
  private" leaks the character's existence and name.
- **Name and pronouns should not be settable to `private`.** A character that appears in a scene
  with an unreadable name breaks the game. Enforce a floor (comparable platforms do exactly this for
  display name and email).

---

## Recommended admin IA (committed, with named patterns)

**Pattern: a validated section registry + two-level permission-filtered nav + a dashboard of
section-owned cards.** Concretely:

### 1. Section registry (the extensibility mechanism)

A declarative registry — one entry per admin section — is what makes "named room for deferred
sections" a testable artifact rather than a comment. Each entry carries:

| Field | Purpose |
|---|---|
| `id` | stable key (`characters`, `players`, `moderation`, `audit`, `stats`, `config`, `plugins`) |
| `group` | the top-level nav grouping it belongs to |
| `label`, `icon`, `order` | presentation |
| `route` | the SvelteKit path it owns |
| `required_role` / capability | drives nav filtering — **a registry contract, not template `{#if}`** |
| `state` | `available` \| `planned` |

**Validation at registration time is the point.** The comparable Django implementations raise at
init if an entry references a non-existent view/model, or if a sub-section names a parent group that
does not exist. Adopt the same: a registry entry whose `route` has no handler and whose `state` is
`available` is a startup/lint failure. A `planned` entry renders a stub. This makes the deferred
sections *declared* (they show in the IA and in generated docs) and *non-lying* (they cannot 404 or
silently vanish), which is precisely the milestone's extensibility requirement.

The motivating problem this solves is worth quoting from the survey: without a registry, "every new
app required updating `sidebar.html` … frequent Git merge conflicts … adding new apps required
touching unrelated files."

### 2. Two-level nav, grouped

Both surveyed registry libraries cap at two levels and one explicitly rejects nesting. Proposed
grouping, with v0.13 reality marked:

```
Overview
  └─ Dashboard                      [available — v0.13]

People
  ├─ Characters                     [available — v0.13, the one real section]
  ├─ Players                        [planned  — 999.8]
  └─ Moderation                     [planned  — 999.8]

Insight
  ├─ Stats                          [planned  — 999.8]
  └─ Audit viewer                   [planned  — 999.8]

System
  ├─ Configuration                  [planned  — 999.8]
  └─ Plugins                        [planned  — 999.8]
```

Four groups, ≤3 sections each — well inside the "split a group past ~7" heuristic, with headroom.
Group names are deliberately generic (`People`, `Insight`, `System`) so a future section lands in an
existing group rather than forcing a nav restructure.

### 3. Dashboard = cards contributed by sections

The landing page is a card grid; each card is owned by a registered section and rendered only when
that section is `available` *and* the viewer passes its permission check. A `planned` section
contributes no card. This means the dashboard grows automatically as sections land — no separate
dashboard-composition file to keep in sync, and no "coming soon" card graveyard.

### 4. What v0.13 actually ships

Shell + registry + nav + dashboard + **one fully-working section** (Characters: list/search, detail,
edit, disable/delete), with every mutation ABAC-gated and audit-emitting. Seven `planned` entries
prove the registry generalizes.

---

## MVP Definition

### Launch With (v0.13)

- [ ] **Character lifecycle status column** (`active` / `retired`, with enum headroom for
      `purged`/`transferred`) — everything else depends on it, and getting it wrong is the one
      unrecoverable modelling error in the milestone
- [ ] **Structured profile fields + prose fields** on characters, with server-enforced length caps —
      the substrate for profiles, privacy, and directory/search alike
- [ ] **Media reference columns** (1 primary + ≤10 gallery, opaque refs, nullable) — schema only,
      zero behavior; satisfies the "no later migration" requirement
- [ ] **Mutation RPCs**: rename, set-profile-fields, retire, un-retire — the entire management story
      is blocked without them (only `CreateCharacter` mutates today)
- [ ] **Creation flow** collecting the identity card, replacing the name-only stub
- [ ] **Management surface** for a player's own characters (alts are the norm)
- [ ] **Public profile page** rendering for anonymous and authenticated viewers, blank-fields-hidden,
      placeholder avatar
- [ ] **Per-field 3-tier privacy**, ABAC-enforced, with defaults + a master private switch
- [ ] **Admin shell**: `/admin`, `RoleAdmin`-gated, section registry with startup validation,
      two-level permission-filtered nav, dashboard-of-cards
- [ ] **Character administration section**, fully working (list/search, detail, edit, disable/delete)
- [ ] **Audit emission on every admin mutation** — cheap now, unrecoverable if skipped
- [ ] **Seven `planned` registry entries** for the deferred sections — the extensibility requirement
      made testable

### Add After Validation (v0.13.x / v0.14)

- [ ] **Rumors / RP-hooks field** — trigger: profiles are being read; this is the field that turns
      reading into scenes. Strong candidate to pull *into* v0.13 given its low cost.
- [ ] **"Currently" status line** — trigger: same. Also low cost.
- [ ] **OOC RP-preferences block** — trigger: players start asking "how do I find compatible partners"
- [ ] **Privacy presets** — trigger: support load on the per-field matrix
- [ ] **Public character directory** (browse/filter) — trigger: enough profiles exist to browse
- [ ] **Operator-level privacy floors** — trigger: an operator asks for a game-wide policy
- [ ] **Retire send-off note** — trigger: first retirement

### Future Consideration (v2+ / explicitly deferred)

- [ ] **Character sheet contents** — defer: needs a stats/system model that does not exist. Ship the
      empty tab; do not design the schema behind it yet.
- [ ] **Avatar / gallery upload + rendering** — defer: 999.16 blob storage. Columns exist; behavior
      does not.
- [ ] **Rostering / character transfer** — defer: 999.6. Must remain a *distinct transition out of*
      retired, never conflated with it.
- [ ] **Remaining admin sections** (players, moderation, stats, audit viewer, config, plugins) —
      defer: 999.8. Declared as `planned` registry entries.
- [ ] **Wiki integration** — defer: qve.8. Leave an external-link seam on the profile.
- [ ] **Web DMs from the profile** — defer: qve.17. Leave a named slot in the profile action bar;
      do NOT render a dead button.
- [ ] **Relationship graph** — defer indefinitely; see Anti-Features. Freeform "Ties" prose if
      anything.
- [ ] **Admin impersonation** — defer; requires `on_behalf_of` in the audit actor shape first.

---

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| Character lifecycle status column | MEDIUM | LOW | **P1** — low value on its own, but everything blocks on it and it is unrecoverable if wrong |
| Structured + prose profile fields | HIGH | MEDIUM | P1 |
| Mutation RPCs (rename / edit / retire) | HIGH | MEDIUM | P1 |
| Creation flow (identity card) | HIGH | MEDIUM | P1 |
| Public profile page | HIGH | MEDIUM | P1 |
| Per-field 3-tier privacy (ABAC-enforced) | HIGH | MEDIUM | P1 |
| Media reference columns (schema only) | LOW | LOW | **P1** — trivial now, a migration later |
| Admin shell + section registry | MEDIUM | MEDIUM | P1 |
| Character administration section | HIGH | MEDIUM | P1 |
| Audit emission on admin mutations | MEDIUM | LOW | **P1** — cheap now, unrecoverable later |
| `planned` registry entries for deferred sections | LOW | LOW | **P1** — the milestone's defining constraint, made testable |
| Rumors / RP-hooks field | HIGH | LOW | **P2, pull-in candidate** |
| "Currently" status line | MEDIUM | LOW | P2 |
| Privacy presets | MEDIUM | LOW | P2 |
| OOC RP-preferences block | MEDIUM | LOW | P2 |
| Character directory (public browse) | HIGH | MEDIUM | P2 |
| Operator privacy floors | MEDIUM | MEDIUM | P2 |
| Retire send-off note | LOW | LOW | P3 |
| Character sheet contents | MEDIUM | HIGH | P3 (blocked on a stats system) |
| Avatar upload + rendering | HIGH | HIGH | P3 (blocked on 999.16) |
| Relationship graph | MEDIUM | HIGH | **P3 / anti-feature** |

---

## Competitor Feature Analysis

| Feature | MUSH convention (Fulcrum / Arx / Steel & Stone / Tapestries) | Web platform convention (BuddyPress / World Anvil / Nextcloud / Circle) | Our Approach |
|---------|---|---|---|
| Creation | Staff-approved multi-stage application: concept → stats → questionnaire → approval | Self-serve form, immediate | Self-serve immediate (no approval workflow in scope), but collect the identity card the application would have produced |
| Profile | `+finger` — short structured public card; `@desc` for appearance | Field groups with per-field types, powering a directory | `+finger`-shaped structured card + prose fields; schema indexed so a directory is additive |
| Sheet | Separate mechanical stats; often staff-gated; some fields "not publicly visible" | World Anvil: separate tab; sheet publishable independently of the character | Separate declared tab, **empty in v0.13** — the split is the deliverable, not the contents |
| Retire | Player-requested; 2-week login grace; XP partially transfers; may go to roster | Account deactivation (reversible) vs deletion (terminal) | Soft status change, owner retained, name retained, history intact; roster transition added later |
| Name release | Explicit separate *purge* whose stated purpose is freeing names | N/A — usernames are near-permanent | **Not in v0.13.** Enum headroom only |
| Privacy | Blunt: a field is IC-public or it is not on the profile | 2–5 tiers, per field, admin defaults, sometimes lockable | 3 tiers per field, ABAC-enforced, string enum for headroom, presets later |
| Custom presentation | `@desc` prose; wiki pages for long content | World Anvil ships per-profile CSS; SpaceHey ships raw HTML | **Neither.** Markdown subset + allowlist sanitization + CSP; external-link seam for a wiki |
| Admin | In-game staff commands (`+request`, `@nuke`), no web console | Registry-driven admin sites (Django/Administrate); game panels with dashboard + users + bans + audit | Registry-driven web shell mirroring the panel convention, with in-game commands remaining the operational path |
| Admin auth | Wizard/staff bits in-game | Role systems; some ship hardcoded back doors and impersonation | `RoleAdmin` + existing default-deny ABAC; **no** back door, **no** impersonation |

---

## Deferred-dependency audit (requirements must NOT assume these)

Explicit per the quality gate — each deferred item, and the shape of the seam left for it:

| Deferred item | Tracking | What v0.13 may NOT depend on | Seam to leave |
|---|---|---|---|
| **Avatar / gallery image upload** | 999.16 | Rendering, uploading, validating, resizing, or serving any image. No acceptance criterion may include a visible character image. | Nullable `primary_image_ref` + `gallery_image_refs` (≤10) columns as opaque refs; UI renders the existing initial-letter placeholder |
| **Rostering / character transfer** | 999.6 (`holomush-gloh`); named as a dependency by `qve.15`'s bead | Retire meaning "released to the pool" or "ownership transferred". No transfer RPC, no roster listing, no availability flag. | Status enum with headroom; retire keeps `player_id` unchanged so a future transfer is a distinct transition |
| **Wiki portal** | `qve.8` | Profile prose being unbounded "because the wiki will handle overflow", or any link into a wiki route | Server-enforced length caps now; a nullable external-link field is a trivial later addition |
| **Offline / PWA** | `qve.7` | Any profile or admin behavior contingent on cached/offline reads | None needed — profile and admin are read-mostly and online-only by design |
| **Web DMs (page/whisper)** | `qve.17` | A "message this character" button on the profile (it would be a dead affordance) | A named, currently-empty slot in the profile action bar |
| **Remaining admin sections** | 999.8 | Any v0.13 admin feature reading data those sections would own (player records beyond what character admin needs, moderation queues, stats aggregates) | `planned` registry entries + reserved routes |
| **Audit *viewer*** | 999.8 | The viewer existing | **Inverted dependency:** audit *emission* is a v0.13 obligation so the deferred viewer has history to show |
| **Character sheet contents / stats system** | none (no spec) | Any field, schema, or RPC for mechanical stats | The sheet tab exists and is empty; do not pre-design its schema |

---

## Sources

Retrieved 2026-07-31 via `exa` per the `research-plan` seam; digests cached under
`gsd-tools query research-store`. Confidence MEDIUM — every finding below is corroborated across
three or more independent primary sources; no single-source claim is presented as authoritative.

**Character creation, profiles, retirement (MUSH/text-RP primary sources):**
- Fulcrum MUSH — NewCharacters, Activity (retirement, idle-nuke, XP transfer)
- World Tree MUSH — Application Guide
- Common Descent MUSH — Chargen Guide (`+view` field list, buffer limits)
- Multiverse Crisis MUSH — Character Application (public vs non-public sections)
- Dream Chasers MUSH — Character Application (`+finger` field list, pronouns)
- Radiant Heart MUSH — Character Application (`+finger/set` required fields)
- Pokemorph MUSH — Application (profile field list, pronouns separate from gender)
- USS Chimaera — application (public-vs-private background)
- Steel and Stone — Policy (idle purge at 30 days; retired-character 2-week grace then purge)
- Tapestries MUCK — Policy:Purge (three purge criteria; **stated purpose is freeing names**)
- Arx, After the Reckoning — Retiring Characters (roster vs permanent freeze; XP carry)
- City of Hope MUSH — Policies (alts; `+request` retirement)
- LA: A House Divided — (In)Activity Policies (idle-out → unapproved → nuked)
- DragonRealms / Elanthipedia — Policy:Character purges (90-day purge, nothing restored)

**Profile vs sheet, profile field guidance:**
- 'Souls Wiki — Forum Profile Guide
- RP Repository — Ideas for Kick-butt Character Profiles; "suggested format for character pages" forum thread
- Springhole.net — Writing Character Profiles & Bios
- World Anvil — Character Manager (Basic/Extended profile, Sheet, Gallery, Privacy tabs)
- Mizahar — Character sheet
- Casual Raiding — MRP profile guide ("Currently" field; blank sections hide)

**Per-field privacy models:**
- BuddyPress — xProfile admin docs + BuddyXTheme guide + BuddyDev extension tutorial (5 levels, lockable defaults, display-only caveat)
- Nextcloud — Profile configuration admin manual (3 visibility × 4 scopes; "most restrictive result")
- Rastrum — `docs/specs/modules/25-profile-privacy.md` (3 tiers + presets; explicit 4th-tier deferral rationale; server-side `can_see_facet()`)
- Orgo — Privacy Settings (2 tiers; "members can only restrict, never expand")
- Jive REST API — ProfileFieldPrivacy entity
- Circle — Manage custom profile fields (per-page-view field visibility)

**Admin IA patterns:**
- `AlkiviadisAleiferis/django_admin_adapter` — `sidebar_registry`, three entry types, init-time validation, "nested dropdowns are not supported"
- dev.to — "Building a Scalable Sidebar Architecture in Django Using Decorators, Registries, and Context Processors" (per-app `menu.py`, `perm` lists, `ImproperlyConfigured` on missing parent section)
- `django-admin-global-sidebar` — two-level menus, per-item `permissions`
- `yassi/dj-control-room` — grouped panel namespace + per-panel standalone opt-in
- `thoughtbot/administrate` — resource-dashboard generation philosophy
- django-admin-deux — sidebar widget plugin hooks (ordering, conditions, identifiers)

**Game-server admin consoles (feature convergence + anti-patterns):**
- GameAP, CFTools Cloud, MonoSuite, HybridCore, `mwlih28/mc-manage-panel`, SS-Admin,
  `RedDragonElite/rde_admin` (raw DB CRUD + hardcoded back door), `bolod2006/AdminRustServer`

**Freeform HTML/CSS anti-feature:**
- PurpleSec — "The Samy Worm: Dissecting the Fastest-Spreading XSS Worm in History"
- Medium (Y. Safwat, 2026-05) — "CSS Injection in Real Bug Bounty Engagements: … Custom Profile Features"
- Medium (D. Lohani, 2026-06) — CSS injection in a link-in-bio profile background field
- dev.to — "Hardening Open Source Apps: Preventing Stored XSS in User-Injected Code"
- Tech Trend Trove (2026-07) — Reddit disabling plain HTML in posts
- Simply Sound Society — "CSS Instead of Raw HTML" (safe-zone HTML blocks vs raw HTML)

**In-tree grounding** (read directly, HIGH confidence): `.planning/PROJECT.md`,
`.planning/archive/beads/TRIAGE.md`, `web/src/routes/(authed)/characters/+page.svelte`,
`internal/store/migrations/{000001,000045,000051}_*.up.sql`,
`api/proto/holomush/{core,web,world}/v1/*.proto`.

---
*Feature research for: character identity surfaces + admin portal shell (HoloMUSH v0.13)*
*Researched: 2026-07-31*
