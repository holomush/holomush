# Phase 5: Character Identity UI & Public Profiles - Context

**Gathered:** 2026-08-11
**Status:** Ready for planning

<domain>
## Phase Boundary

Phase 5 delivers the **player-facing character identity surface in the web
client**: a structured creation card replacing the name-only stub, multi-alt
roster management including the default-character control, an owner authoring
surface for the twelve `profile.*` fields and the in-world description, and a
**public profile page a logged-out visitor can read at a stable URL** — plus the
media-schema proof (EXT-05) with no uploader.

**Requirements:** IDENT-01, IDENT-05, PROFILE-01, PROFILE-02, PROFILE-06,
PROFILE-07, PROFILE-08, PROFILE-09, PROFILE-10a, PROFILE-12, EXT-05, EXT-08.

**`01-SPEC.md` is normative and locks most of this phase.** §7 fixes the twelve
`profile.*` fields and the media naming; §8 fixes the tier ladder, absence
semantics and opacity; §9 fixes the whole RPC surface, `expected_version`, the
mask contract and the eight error codes. Decisions below settle only what those
sections left open, what the sketches left open, or what has drifted since.

**This phase is NOT UI-only.** The ROADMAP's research posture ("Phase 5 is
established shadcn/runes patterns … skip research") is accurate for the
rendering work and **wrong about the surface area**. Verified against the tree:
`api/proto/holomush/characteraccess/v1/characteraccess.proto` declares six RPCs
(`GetCharacterProfile`, `ListMyCharacters`, `GetMyCharacter`,
`UpdateCharacterProfile`, `UpdateCharacterDescription`,
`ListCharacterDirectory`) plus six `Web*` proxies. Phase 5 adds **two more
RPCs** (`CreateCharacter` reshape, `SetDefaultCharacter`), each with its proxy,
its §3 inventory row and its census entry per D-72.

**Not in this phase:**

- **Admin RPCs and the `/admin` shell** — Phase 6, ADMIN-* requirements.
- **Owner-facing `RetireCharacter` / `UnretireCharacter`** — REQUIREMENTS marks
  player self-retire *"deferred beyond v0.13"*; Phase 3 closed IDENT-04 as a
  **domain** capability with the admin half in Phase 6 (ADMIN-05). PROFILE-12's
  retirement half follows it — see D-91.
- **`RenameCharacter`** — out of the milestone (Phase 3 D-44 → backlog 999.20).
  Its absence is load-bearing here; see D-88.
- **Any visibility editing surface** — §8.12 ships the model plus seeded
  defaults only, and the SPEC's reviewer MUST verify no v0.13 PR adds one.
- **An image uploader, storage backend, or media-serving path** — §7.3. EXT-05
  proves the schema, nothing more.
- **The shared `+error.svelte` not-found page** (sketch 010-B, #4903) — Phase 6.
  See D-95.

</domain>

<decisions>
## Implementation Decisions

### Routing and the auth boundary

- **D-84:** **The public profile lives at root-level `/c/[id]`; `/characters*`
  stays the owner namespace under `(authed)`.** No path prefix spans two auth
  postures. Grounded: every route today except `/login`, `/register`, `/reset`
  sits under `(authed)`, whose `load()` redirects to `/login` on session failure
  (`web/src/routes/(authed)/+layout.ts:26-30`), so a public profile cannot live
  there. `/c/[id]` becomes a sibling of `/login` and inherits the root layout
  exactly as those do — no new route group, no new chrome component.

  | Path | Group | Audience |
  | --- | --- | --- |
  | `/characters` | `(authed)` | owner roster (ships today) |
  | `/characters/new` | `(authed)` | owner create (D-87) |
  | `/characters/[id]` | `(authed)` | owner authoring (D-92) |
  | `/c/[id]` | root | public profile, anonymous-readable |

  The URL key is the **character id, never the name** — §9.2 settles this, and
  the sketch's "profile URL key … unsettled before Phase 5 routes anything" is
  **stale**: a name-keyed URL breaks on rename and, worse, silently resolves to
  a *different* character after a purge frees the name. §9.2 also forbids adding
  any name-keyed character lookup to this surface in v0.13.
  — **Reversibility:** costly — a shared profile URL is the point of the
  feature; changing the path after links exist breaks them.

- **D-85:** **Phase 5 ships no new signed-out chrome.** The sketch's carried
  gap *"signed-out web chrome is unspecified — 007 invented a `Sign in` / `Play
  as guest` bar"* is **narrower than recorded**: `TopBar.svelte:141-144` already
  ships an unconditional anonymous branch — brand chip, theme picker, `Login`,
  `Register` — rendering today on `/login` and `/register`, which are already
  outside `(authed)`.

  That pair also **discharges 007-C's invitation constraint by construction.**
  007-C rejected variant B's sign-in notice because *"show it only when
  something was withheld"* is a which-profiles-are-populated oracle, and it
  permitted an invitation only if **unconditional**. TopBar's `Login`/`Register`
  is on every page for every anonymous viewer regardless of what any profile
  contains — unconditional by construction, varying with nothing. **No
  profile-local sign-in notice is ever needed, and none may be added.**

### `CreateCharacter` (IDENT-01)

- **D-86:** **Full reshape; the old shape dies in the same change.** New
  `CharacterAccessService.CreateCharacter` taking the structured identity card
  (name, pronouns, concept, species, age, faction) and returning `OwnCharacter`,
  plus its `WebCreateCharacter` proxy. The shipped name-only request/response
  (`api/proto/holomush/web/v1/web.proto:177`, `:656` returns a bare
  `character_name` scalar) is **replaced outright** per §2.5's breaking-change
  posture. Its §3 inventory row and census entry land in the **same commit** per
  D-72 — the census derives from generated service descriptors, so a declared
  RPC is a member the moment the proto compiles.

  Rejected: keeping the old RPC one release (two create paths racing one unique
  index, and §2.5 says replace outright); extending `WebCreateCharacterRequest`
  in place without a facade-side RPC (leaves the one owner-audience mutation not
  on the facade, breaking the §9.1 "the facade holds the decisions" shape the
  other five follow).

  The roster's inline `createCharacter()`
  (`web/src/routes/(authed)/characters/+page.svelte:56-63`) is rewritten to the
  new client.
  — **Reversibility:** costly — a reshaped RPC keeps its census membership
  through the reshape, and deleting its inventory row to "fix" a red census is
  the erosion §3.1 rule 3 forbids.

- **D-87:** **Creation is a route, `/characters/new`.** Six fields plus §6.1's
  rejection reporting need room; sketch 008's create card already links there in
  its markup. The roster's dashed create card becomes a link, not an inline
  input.

- **D-88:** **Create honesty: post-submit echo plus static rule copy.** The
  created display name is shown in the success path — the toast and the new
  roster card — and the form carries one line stating the *class* of rewrite
  ("full-width characters, invisible marks and extra spaces are folded").

  **Why this is a revisit, not a restatement.** Sketch 009 chose submit-and-
  report and recorded its own trigger: *"That dependency is load-bearing. If a
  later release removes or gates rename, revisit this decision — a surface that
  silently rewrites an **irreversible** identifier would need to show its work
  first, and A would become the wrong choice."* Rename left v0.13 on 2026-08-06
  (Phase 3 D-44), **five days after the sketch**. The condition has fired and
  the decision was re-taken, not inherited.

  **The conflation the sketch left behind.** 009 rejected variant B on Finding 1
  — *"a live availability check cannot be honest"*, because check and insert are
  different moments. That is correct and unchanged. But B bundled two different
  promises, and only one is dishonest:

  | Promise | Deterministic | Honest |
  | --- | --- | --- |
  | "available ✓" | No — the corpus moves between check and insert | No |
  | "will be created as `Teodor`" | **Yes** — `charname.Normalize` (`internal/charname/pipeline.go:100`) is pure: NFKC → strip `Cf` → collapse whitespace. No `ctx`, no DB, no I/O | **Yes** |

  Rejecting B discarded both. Only the first deserved it. The echo half is then
  **free**: `CreateCharacterResponse` returns `OwnCharacter`, which already
  carries `name` in its display form — the echo is already on the wire.

  Rejected: a pure `NormalizeCharacterName` RPC for pre-submit echo (a whole
  RPC, proxy and inventory row for one line of feedback — rule `7zy1161fh1`);
  a client-side TypeScript mirror of the pipeline (**duplicates a
  security-adjacent normalizer** — NFKC, `Cf` strip, Unicode full case-fold — in
  a second language, creating two sources of truth for the value the unique
  index depends on); a two-step confirm gated on "did it rewrite" (needs one of
  the two above to know).

  Of the three rewriting steps only NFKC is genuinely surprising, and its
  fullwidth→ASCII fold is usually what the player meant. The **rejections**
  (`NAME_BLOCKED`, mixed-script, skeleton, `23505`) are reported on submit
  regardless. Carry sketch 009's constraints forward unchanged: the confusable
  message **MUST NOT name the colliding character**, and the invisible-only case
  needs its own wording — already handled server-side, which splits one
  `NAME_EMPTY_NORMAL_FORM` code into two messages
  (`internal/charname/pipeline.go:110-128`).

### `SetDefaultCharacter` (IDENT-05)

- **D-89:** **A new `CharacterAccessService.SetDefaultCharacter` ships**, owner
  audience, gated on session resolution + ownership, with its `Web*` proxy.

  **Why a new RPC is unavoidable.** IDENT-05 requires managing every alt
  *"including which is default"*, and the write path **does not exist**:
  `players.default_character_id` is read (`WebCheckSessionResponse`,
  `web/src/routes/login/+page.svelte:60`) and cleared on retire
  (`internal/world/postgres/character_repo.go:539`) — **nothing in the codebase
  ever sets it**, and §9.3's mutation table has no such row. §9.1 is explicit:
  *"If a required operation has no typed RPC in this section, the correct
  response is to add the RPC, not to reach for the command path."*

  It targets a `players` row, so **§9.4's `expected_version` requirement does
  not reach it** — that rule is scoped to "a mutation that targets an existing
  character **row**". No `CHARACTER_VERSION_REQUIRED` branch applies.

  Rejected: descoping "which is default" (leaves a roster showing a default
  nobody can change); folding it into `UpdateCharacterProfile`'s mask (that
  RPC's resource is `character:<id>` and its mask governs `profile.*` rows — the
  default pointer is a `players` column under a different gate).
  — **Reversibility:** reversible — additive.

- **D-90:** **It returns the owner's full roster** (`ListMyCharactersResponse`-
  shaped) so the client re-renders from server truth rather than patching local
  state. Character-shaped ⇒ **a §2.6 census member** with an `owner` audience
  verdict and its own §3 inventory row, exactly like the other five, projected
  by `projectOwner` per §2.3 — never a struct literal.
  — **Reversibility:** costly — the response's audience verdict is pinned by
  the census; changing it later moves an inventory row.

### PROFILE-12 and the retirement flow

- **D-91:** **PROFILE-12's retirement half moves to Phase 6.** Phase 5 ships
  only the authoring-surface half of the notice — that privacy is not
  retroactive over already-published history — on `/characters/[id]`.

  **Why it cannot land here.** Criterion 4 requires *"both the retirement flow
  and the surface where a player authors profile fields state in the UI"*. There
  **is no player-facing retirement flow in v0.13**: IDENT-04 records player
  self-retire as *"deferred beyond v0.13"*, and the only retire path is
  `AdminRetireCharacter` in Phase 6. Attaching the notice to the roster's `Not
  playable` section instead would put a warning where nobody is making a
  decision, which is the opposite of what a warning is for.

  Rejected: shipping owner-facing `RetireCharacter`/`UnretireCharacter` after
  all — a scope reversal of a recorded deferral, not a clarification, on a phase
  already carrying a proto reshape and two new RPCs.

  **Amendment owed** — see "Roadmap amendments owed" below.

### The owner authoring surface

- **D-92:** **`/characters/[id]`, sectioned, edit in place**, with a `View
  public profile →` link to `/c/[id]`. One `GetMyCharacter` call feeds the whole
  page (`OwnCharacter` carries id, name, description, the profile map, media,
  status and `version`), and `version` lives in exactly one place.

  A view/edit split was rejected because **§8.12 ships no visibility controls**,
  so an owner's read view and edit view render the *identical* dataset — two
  routes and two loads for one dataset, with two places `version` can go stale.
  A Sheet overlay was rejected because sketch 008 fixed Cards-for-players /
  dense-table-for-operators as a **deliberate** two-idiom split, and twelve
  sectioned fields do not fit a 380px overlay.

  §7.2 states the field count as a number specifically so this surface picks *"a
  sectioned form, not a single-column stack"* — the sections also carry D-93.

- **D-93:** **Per-section save, with the in-world description as its own
  section.**

  **The problem this solves.** `UpdateCharacterProfile` (twelve
  `entity_properties` rows) and `UpdateCharacterDescription` (the
  `characters.description` column) are separate RPCs that both guard the **same**
  `characters.version`, with **no transaction spanning them**. A whole-form save
  touching both is a two-call chain: call 2 failing `WORLD_CONCURRENT_EDIT`
  (`Aborted`) after call 1 succeeded leaves a partial save and a stale form,
  with no rollback.

  Per-section save dissolves it. Each section owns its Save and its mask; each
  response returns the post-write `OwnCharacter` carrying the fresh `version`,
  so the next save is automatically correct. A conflict scopes to one section —
  the player loses one section of typing, not twelve. Making the description its
  own section means the two-RPC split **shows up in the UI as a section
  boundary** rather than hiding behind one button that silently makes two
  writes: the system's shape and the interface's shape agree, and the
  partial-failure state stops existing.

  This matches the mask contract as written: an **empty mask is a no-op
  success, never "apply every field"**, and an unlisted path is rejected rather
  than ignored (`characteraccess.proto:326-332`).

### Absence, placeholders, and the two deliberate-absence surfaces

- **D-94:** **The absence rule is scoped by viewer-variance.** §8.9's own
  wording is the hinge — *"An **attribute** whose floor the viewer does not
  clear MUST be absent"* — attribute, not chrome. The rule exists for one
  reason: a blank field and a withheld field must be indistinguishable, because
  the difference discloses what **this viewer** may not see.

  **The discriminating test, recorded so a reviewer can apply it:**

  > **Does this element's presence or absence vary with who is looking?**
  > Yes → the absence rule (render nothing).
  > No → the named-slot rule (name the reserved capacity).

  | Governs | Rule | Because |
  | --- | --- | --- |
  | The twelve `profile.*` fields + the in-world description | Absent ⇒ render nothing. No count, lock icon, greyed section, progress indicator or "N hidden" affordance | Presence is a privacy signal (§7.5, §8.9) |
  | The empty sheet (PROFILE-02), the named web-DM slot (EXT-08) | Name the reserved capacity | Identical for every viewer at every tier; discloses only "the platform has not built DMs yet", which is public |

  An owner-only `Edit` affordance passes the test too: it varies by viewer, but
  it tells the owner they are the owner, which leaks nothing. The repo already
  has the settled answer for reserved capacity — EXT-08's *"named empty slot,
  not a dead affordance"* and sketch 003's winner (*"Registered and gated. No
  handler yet."*).

- **D-95:** **The sheet ships as a named empty section on the profile, not a
  route.** No second route means no second §8.7 not-found obligation to keep
  byte-identical with the profile's.

  **And `/c/[id]` renders its not-found inline.** Both causes — character does
  not exist, and profile below its reachability floor — return the same
  `CHARACTER_PROFILE_NOT_FOUND` (§9.6 makes it deliberately one code for two
  causes), so the page renders one identical state either way and
  indistinguishability holds **at the page level with no error boundary
  involved**. `+error.svelte` does not exist (#4903) and sketch 010-B's shared
  not-found page is a Phase 6 build; Phase 5 does not pull it forward.
  **Constraint carried to Phase 6:** when 010-B ships, `/c/[id]` MUST adopt it,
  or the two diverge into distinguishable pages.

- **D-96:** **The roster's `Not playable` section renders expanded, with the
  count chip as a collapse control** — sketch 008's own inclination, on the
  grounds that these are the player's *own* characters. Relabel the chip away
  from "hidden", which presumes the opposite default. `idle` is **unreachable in
  v0.13** (nothing transitions into it, §4.2), so **write no copy assuming a
  player will ever see it**. Sketch 008's badge rule carries unchanged: a
  non-`active` lifecycle **suppresses the session badge entirely** — `Retired ·
  Offline` is meaningless, and the two vocabularies collide on the token
  `active`.

### Verification (criteria 4 and 5)

- **D-97:** **Criterion 4's "next load" is amended to name the poller.** As
  written the criterion is **false**: `internal/access/policy/cache.go` holds a
  compiled snapshot refreshed by a poller on a **10-second default interval**
  (`internal/access/policy/poller.go:35`), so a corpus change is visible on the
  next load *after the cache reloads*, not the next load. The criterion's
  substance — evaluated at read time, never stamped onto a row, so no backfill
  exists — is entirely intact; only the latency claim overclaims. Reword to
  *"on the next load after the policy cache reloads (poller interval, default
  10s)"*, and let the test drive `Reload()` explicitly rather than sleeping past
  a tunable interval.

  **Amendment owed** — see below.

- **D-98:** **Criteria 4 and 5 add exactly two integration tests. Existing
  coverage is cited, not reproved.** Both criteria are noun-phrased — the trap
  rule `7zy1161fh1` and memory `r65waekn3h` exist for. The prior-phase artifacts
  and the tree were searched first, and most of both is already covered:

  | Clause | Already proven by |
  | --- | --- |
  | Clearing is set membership, never ordinal | `internal/access/policy/seed_profile_visibility_test.go:260`; `internal/access/profilevis/tierfloor_test.go:173` (a synthetic 4th rung clears neither shipped floor) |
  | The floor is evaluated per read | `internal/access/profilevis/profilevis_test.go:113` — exactly two evaluations per attribute, separated by the action token |
  | `UNIQUE(parent_type,parent_id,name)` rejects a duplicate | `internal/world/postgres/property_repo_test.go:430` `TestPropertyRepository_ParentNameUniqueness` (integration-tagged) |
  | The policy enumerates exactly eleven media names, not twelve | `internal/access/policy/seed_profile_visibility_test.go:393` |
  | Never stamped onto a row | structurally true — `entity_properties` carries no tier column and no migration writes one |

  **The two genuine deltas:**

  1. **Criterion 4** — mutate the seed corpus mid-test, `Reload()`, and assert
     the **same anonymous viewer's** `GetCharacterProfile` returns a different
     field set **with no write to the character or its rows**. The mutation is
     the discriminating step: without it the test degenerates into "the
     anonymous read omits field X", a shipped-state assertion that passes
     whether or not the floor is read-time.
  2. **EXT-05** — insert `profile.image.primary` + `profile.image.gallery.00`…
     `.09` for one character, read them back **through the viewer-filtered
     path**, and assert a second `profile.image.primary` errors. The value is
     the *real naming scheme* surviving the *real read path* — the
     no-migration-later demonstration. **Reproving the `UNIQUE` constraint would
     be the duplicate-gate anti-pattern**; cite `property_repo_test.go:430`
     instead.

  Keep them **two tests, not one**: a media-schema regression and a
  read-time-evaluation regression must not become one red test with two possible
  causes.

### Claude's Discretion

- Section grouping within the twelve `profile.*` fields (short facts vs
  long-form prose), and the exact copy of the not-retroactive notice and the
  name-normalization rule line.
- Which shadcn components to add. Currently installed: `badge`, `button`,
  `card`, `checkbox`, `command`, `dialog`, `dropdown-menu`, `input`,
  `input-group`, `label`, `popover`, `resizable`, `scroll-area`, `separator`,
  `sheet`, `textarea`, `tooltip`. **Not installed** and plausibly needed:
  `avatar`, `field`, `sonner`, `select`, `skeleton`. Note the initial-letter
  portrait is pure CSS in 007-C and may need no `avatar` at all.
- Whether the `/c/[id]` page and the `/characters/[id]` authoring page share a
  presentational component for the identity card.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The normative SPEC (read first)

- `.planning/phases/01-portal-spec/01-SPEC.md` — the portal SPEC. Sections
  binding on this phase:
  - **§2.3 / §2.6** — one projection function per (row → audience); the census
    compared by **set equality**, derived from generated service descriptors.
  - **§3.1 / §3.4** — inventory membership rules; erosion rule 3 (never delete a
    row to make a census green).
  - **§7.1–§7.5** — where profile data lives; the **twelve** `profile.*` fields;
    media naming (`profile.image.primary`, `gallery.00`…`09`, zero-padded, exact
    bytes); the in-world description always public on the profile; the empty
    profile and absence-not-emptiness.
  - **§8.2.1** — clearing a floor is **set membership, never string ordering**.
  - **§8.6** — the seeded postures. Under seeded defaults **`guest` and `player`
    render identically** (no row seeds `player`).
  - **§8.7** — unreachable profiles are opaque; the **ordinary not-found**.
  - **§8.8** — name and pronouns are a hard floor, guaranteeing the portrait
    always has a letter.
  - **§8.9** — enforcement by absence, never by emptiness, never client-side.
  - **§8.10** — infrastructure failure resolves **DENY**, never "sees nothing".
  - **§8.12** — **no visibility editing surface ships in v0.13.**
  - **§9.1** — every new surface is a typed RPC on the facade; the gateway
    computes nothing; **projection, not assembly**.
  - **§9.2** — the read surface; **the profile URL is keyed on the character id,
    never the name**; no name-keyed lookup may be added.
  - **§9.3** — the mutation surface; `CreateCharacter` is a **reshape, not an
    addition**.
  - **§9.4 / §9.4.2** — the concurrency contract; `CreateCharacter` is the one
    `expected_version` carve-out.
  - **§9.5** — the update-mask contract (closed allowlist, empty mask is a
    no-op success).
  - **§9.6 / §9.6.1** — the eight error codes and **the mandated wire-level
    assertion**; `CHARACTER_PROFILE_NOT_FOUND` is deliberately one code for two
    causes; the differential assertion §8.7 implies.
  - **§12.1** — the six verification-integrity rules (PORTAL-10).

### Prior-phase decisions this phase inherits

- `.planning/phases/04-shared-facade-helpers-characteraccessservice/04-CONTEXT.md`
  — D-69 (the audience split; `entity_properties.owner` is a **scalar**, so the
  ALL rule reduces to "the owner is that player's only character"), **D-72** (the
  proto declares only what ships; each later RPC brings its own inventory row),
  D-73 (criterion 1's routing census is the `owner`-audience RPCs only —
  `GetCharacterProfile` and `ListCharacterDirectory` serve anonymous viewers by
  design and MUST NOT be routed through `resolveAndGate`), D-76 (the
  `read_description` viewer twin).
- `.planning/phases/03-world-character-commands/03-CONTEXT.md` — **D-44**, the
  rename deferral that D-88 revisits.
- `.planning/phases/02-abac-schema-vocabulary/` — the tier-floor policy family,
  the name pipeline, the normalized-name unique index.

### Sketch findings (design decided; do not re-litigate)

- `.claude/skills/sketch-findings-holomush/references/profile-and-viewer-tiers.md`
  — sketch **007-C**, the identity card. **The page may not explain its own
  sparseness.** Under seeded defaults `guest` and `player` render identically.
  The gallery can never contain an image in v0.13 — build the renderer, ship no
  "coming soon" slots.
- `.claude/skills/sketch-findings-holomush/references/player-roster-and-creation.md`
  — sketch **008-B** (sectioned roster; the session-badge suppression rule) and
  **009-A** (submit and report). ⚠ Its *"v0.13 ships rename"* framing is
  **stale** — see D-88.
- `.claude/skills/sketch-findings-holomush/references/gating-and-absence.md` —
  the absence and not-found idioms.
- `.claude/skills/sketch-findings-holomush/references/anti-patterns.md`
- `.planning/sketches/MANIFEST.md` — the locked-decisions table and the
  carried-forward gaps.

### Repo rules that bind this phase

- `.claude/rules/gateway-boundary.md` — §"Structural writes use typed RPCs, not
  the command path". A GUI form is a machine-initiated structural write and MUST
  reach a typed RPC; `sendCommand` is for human conversational verbs only. ADR
  `docs/adr/holomush-v4qmu-typed-rpcs-structural-scene-writes-command-path-human-cli-ve.md`.
- `.claude/rules/grpc-errors.md` — never leak inner errors past the boundary;
  translate at one layer; wire opacity needs top-level code assertions. ⚠ Its
  `oops.AsOops(err).Code()` recommendation is **known-wrong** (#4902) — that
  spelling chain-walks identically to `errutil.AssertErrorCode`. Assert the wire
  per §9.6.1.
- `.claude/rules/abac-providers.md` — omit optional attributes, never sentinel.
- `.claude/rules/invariants.md` — any new `INV-<SCOPE>-N` allocates in an
  existing scope (`ACCESS`, `PRIVACY`) and MUST be hand-registered in
  `docs/architecture/invariants.yaml`; ship `binding: pending` rather than
  fabricating a `// Verifies:`.
- `.claude/rules/terminology.md` — **character**, **player**, **session**,
  **location**. Never "room", "user", "avatar".
- `.claude/rules/branding.md` — INV-6: the brand is the **software**, never the
  game world. Copy on a player-facing page is `Home`, never the platform name.
- `web/CLAUDE.md` — Svelte 5 runes, Tailwind v4 `@theme` build-time gotcha,
  `--color-*` token naming, form `name` attributes for Playwright.

### Roadmap amendments owed (record, do not hand-edit)

Rule `a32nfcekfc` forbids hand-editing `.planning/ROADMAP.md`, and no
`gsd-tools` verb rewrites an existing phase's success criteria (`phase` verbs:
`uat-passed`, `next-decimal`, `add`, `add-batch`, `insert`, `remove`,
`complete`, `list-plans`; `roadmap` verbs: `analyze`, `get-phase`,
`update-plan-progress`, `annotate-dependencies`, `validate`, `upgrade`). Both
amendments below are therefore recorded here and in STATE, and must be reflected
by whoever owns the ROADMAP text:

1. **Criterion 4** — "next load" → "next load after the policy cache reloads
   (poller interval, default 10s)" (D-97).
2. **Criterion 4 / PROFILE-12** — strike "both the retirement flow and"; the
   retirement half moves to Phase 6 alongside `AdminRetireCharacter` (D-91).
   REQUIREMENTS' PROFILE-12 row needs the same note.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets

- **`api/proto/holomush/characteraccess/v1/characteraccess.proto`** — six RPCs
  and the message set already shipped: `PublicCharacter` (with
  `map<string,string> profile`, so absence is expressible on the wire),
  `OwnCharacter` (adds `status`, `version`), `ProfileImage`,
  `PublicCharacterSummary`. `UpdateCharacterProfileRequest` already carries
  **all twelve** prose fields (fields 4–15) plus `expected_version` and a
  `FieldMask update_mask` at field 99. Phase 5 writes **no new profile field**.
- **`internal/web/character_handlers.go:18-51`** — `WebGetCharacterProfile`
  resolves the **anonymous rung from an empty token** and returns the public
  projection. The four owner-audience proxies below it copy its shape. The
  anonymous read path already works end-to-end server-side.
- **`internal/charname`** — the whole §6.1 pipeline: `Normalize` (pure, no I/O),
  `Gate.Check` (subsumes the syntactic rules deliberately), `MixedScript`,
  `BlockList`, `skeleton`. The invisible-only case already splits one code into
  two messages (`pipeline.go:110-128`).
- **`internal/access/profilevis`** — the tier-floor evaluator: `Reachable`,
  `AttributeVisible` (exactly two evaluations separated by the action token),
  `VisibleAttributes` (aborts the whole call on an evaluation error per §8.10).
- **`web/src/lib/scenes/createFlow.ts`** — the create-flow idiom the ROADMAP
  names: the create RPC is authoritative, and post-create refresh/select
  failures are swallowed with a warn so a UI hiccup never surfaces as "create
  failed" and risks a duplicate.
- **`web/src/routes/(authed)/characters/+page.svelte:116-179`** — the shipped
  Card grid, initial-letter avatar, session badge, and the dashed create card.
- **`web/src/lib/components/TopBar.svelte:141-144`** — the unconditional
  anonymous branch (D-85).
- **`web/src/lib/nav/sections.ts`** — the `as const satisfies` registry pattern
  the SPEC says to mirror.

### Established Patterns

- **Two idioms, deliberately:** Cards for the player's own things, dense table
  for operator surfaces (sketch 008). Not an inconsistency.
- **Absence over emptiness**, at the wire (§8.9) and in the renderer (007-C).
- **`ssr = false`** on every route load; `adapter-static` with
  `fallback: 'index.html'` means every path is already HTTP 200 + `index.html`,
  so route-level indistinguishability is structural.
- **Session restore in `load()`, not `onMount()`** (`web/CLAUDE.md`) — auth
  guards redirect on reload otherwise.
- **`//go:build integration`** + Ginkgo for full-stack specs; `task test` does
  **not** compile them, so `task test:int` is mandatory on any shared-type
  refactor.

### Integration Points

- `api/proto/holomush/characteraccess/v1/characteraccess.proto` +
  `api/proto/holomush/web/v1/web.proto` — two new RPCs each, then
  `task proto && task web:generate`, committing `pkg/proto/**/*.pb.go` and the
  web `*_pb.ts` **in the same change** or CI fails the stale-diff check.
- The §3 inventory (`01-SPEC.md` §3.3) and the §2.6 census meta-test — each new
  RPC brings its row in the same commit (D-72).
- `internal/grpc` (facade impl) → `internal/world/service.go` for the
  description write; `players.default_character_id` for D-89 — note the world
  repo currently only **clears** it (`character_repo.go:539`), so a setter is
  net-new at the repository layer too.
- `web/src/lib/connect/` — regenerated clients.
- `web/e2e/` — Playwright specs; a logged-out profile visit is a genuine E2E
  case (no session at all), unlike every existing spec.

</code_context>

<specifics>
## Specific Ideas

- **The card must look deliberate with only name + pronouns in it.** That is the
  players-only-posture anonymous view and it is reachable in a real
  configuration (007-C). Do not design for the populated case and hope the
  sparse one degrades.
- **The description is doing all the work of making the anonymous view worth
  having** (§8.11). Under the seeded posture an anonymous viewer sees exactly
  three things: `name`, `profile.pronouns`, and the in-world `description`.
- **`profile.rp_preferences` is not `characters.preferences`.** The latter is a
  shipped `JSONB` settings column (migration `000045`). The `rp_` qualifier
  exists so the two cannot be conflated by name alone. Phase 5 **MUST NOT** write
  the RP block into the settings column.
- **Media names are compared as exact bytes.** `profile.image.gallery.0` and
  `.00` are two different rows that coexist happily; there is no normalization
  anywhere in the read path. The zero-padded two-digit form is fixed.
- **Test the `player` rung directly.** No seeded row places anything at `player`,
  so a bug in the `player` clearing path **would not show up by running the
  default game** (007 Finding 1).

</specifics>

<deferred>
## Deferred Ideas

- **Game display name on the web client** — #4905. `TopBar.svelte:66` hardcodes
  `HoloMUSH` on what becomes a player-facing public page (branding INV-6).
  `SettingConfig.DisplayName` is *required* on setting plugins
  (`internal/plugin/manifest.go:211`) but reaches no web surface. Any
  player-facing game identity — title tag, OG card, a "back" target — needs this
  settled first. Not Phase 5's to build; **flagged because `/c/[id]` is the
  first public, shareable, indexable page the platform ships.**
- **The shared `+error.svelte` not-found page** (sketch 010-B) — #4903, Phase 6.
  Carries the constraint from D-95 that `/c/[id]` must adopt it when it lands,
  and Phase 6's meta-test asserting **exactly one** `+error.svelte` under
  `web/src/routes/`.
- **Owner-facing `RetireCharacter` / `UnretireCharacter`** — §9.3 names both;
  IDENT-04 defers player self-retire beyond v0.13. Revisit when a product
  requirement appears.
- **`RenameCharacter` + the approval dimension** — backlog 999.20, linked to
  999.6 Rostering. It reopens the profile-URL-key question §9.2 settled for
  v0.13, and it would restore sketch 009-A's original premise (D-88).
- **A pre-submit `NormalizeCharacterName` RPC** — rejected in D-88 as
  disproportionate. Reconsider only if the post-submit echo proves insufficient
  in practice.
- **An image uploader, storage backend, and media-serving path** — §7.3;
  moderation must precede it (that is what `alt_text` and `content_warning` are
  waiting for).
- **A conditional sign-in invitation on profiles** — permanently forbidden as a
  which-profiles-are-populated oracle. If a later phase reintroduces any
  invitation it **MUST be unconditional**, and that constraint MUST be stated
  where the component lives (007-C).
- **An operator-facing viewer-tier preview** — if ever built, it MUST derive
  distinct outcomes from the **live floor set**, never offer a hardcoded
  three-way toggle: under seeded defaults two of the three panels are identical
  (007 Finding 1).
- **A populated-corpus re-run of the exposure audit** — #4937, open and
  `awaiting-precursor` (Phase 4 D-77). Phase 5 creates real profile rows for the
  first time, so it may become the precursor — but creating that corpus is not a
  Phase 5 deliverable.
- **A name length cap** — sketch 009 open question 3: §6.1 states no cap for
  character names, and an unbounded name is a rendering problem for the roster
  card, the admin table **and the profile header**. `syntax.ValidateName`
  imposes rune-length bounds (`internal/charname/syntax`) — the planner should
  confirm what they are rather than assume none exists.
- **`profile.currently` freshness signal** — 007 open question 3. §7.2 says it
  *"carries no history"* and changes often; a stale "Currently" is worse than
  none, but a timestamp is a new governed attribute needing its own §8.6 floor
  row. Not v0.13.

### Reviewed Todos (not folded)

Five todos matched Phase 5 by `gsd-tools todo match-phase`, all scoring on the
`auth` / `docs` **area heuristic** rather than on semantics. All five are
`world.Caller` / INV-WORLD internals from Phases 02.1–02.2; none touches
character identity UI, profiles, or the web client. Folding any would be scope
creep.

- *Guard the caller attribute channel before Phase 02.2 populates it* (auth,
  0.9) — world caller model, not this phase.
- *Harden `world.Caller` bypass visibility and opacity guarantees* (auth, 0.9) —
  same.
- *Fix INV-WORLD-6 false rename claim and coverage gap* (docs, 0.6) — invariant
  documentation; **note** it concerns the rename claim D-88 also touches, but
  the fix is to an INV-WORLD entry, not to this phase's surfaces.
- *Should INV-WORLD-4 distinguish operational writers from world-state writers*
  (docs, 0.6) — invariant scoping question.
- *Clean up `world.Caller` naming and doc drift* (general, 0.6) — naming
  hygiene.

</deferred>

---

*Phase: 5-Character Identity UI & Public Profiles*
*Context gathered: 2026-08-11*
