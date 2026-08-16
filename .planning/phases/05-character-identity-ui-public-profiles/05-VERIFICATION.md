---
phase: 05-character-identity-ui-public-profiles
verified: 2026-08-12T21:20:00Z
re_verified: 2026-08-13T08:20:00Z
status: passed
score: 5/5 must-haves verified
behavior_unverified: 0
overrides_applied: 0
re_verification:
  performed: 2026-08-13T08:20:00Z
  scope: >-
    Criterion 2 ONLY — targeted re-score against amended wording. No other criterion re-derived; no
    test suite re-run (build / test / lint / test:e2e / test:int gates already established at this
    HEAD by exit code). One single-file behavioral check was run, named below.
  previous_status: gaps_found
  previous_score: 4/5
  maintainer_ruling:
    date: 2026-08-13
    verdict: >-
      01-SPEC.md is authoritative over the ROADMAP criterion. Phase 5's implementation is correct as
      built. NO CODE CHANGE MADE.
    rationale: >-
      The ROADMAP criterion overclaimed. Widening what a logged-out stranger can read — notably the
      OOC RP-preferences block and time zone — is a privacy decision, and it must not happen by
      criterion wording drifting away from a locked spec. The criterion is the artifact being
      amended, not the seed corpus.
    amendment: "amendment 5 on issue #4963"
    amended_criterion_2: >-
      A logged-out visitor loads a character's public profile at a stable URL and sees the in-world
      description alongside the anonymous-tier `profile.*` fields (pronouns), with the richer public
      block — rumors / RP-hooks, the volatile "Currently" line, the OOC RP-preferences block, and
      time zone — becoming visible at the `guest` tier per `01-SPEC.md:1741-1744`; blank fields hide
      themselves and an initial-letter avatar placeholder appears where no image exists.
    roadmap_note: >-
      `.planning/ROADMAP.md` still shows the OLD criterion-2 wording. No GSD verb can rewrite a
      phase's success criteria and the ROADMAP must not be hand-edited, so the amended text lives in
      #4963 pending action. THIS REPORT EVALUATES CRITERION 2 AGAINST THE AMENDED WORDING, and says
      so in the body.
  gaps_closed:

    - >-
      Criterion 2 — re-scored ✓ VERIFIED against the amended wording. The gap was a
      ROADMAP-vs-SPEC contradiction, now settled in the SPEC's favour by maintainer ruling; the
      code was never at fault and is unchanged.
  gaps_remaining: []
  regressions: []
deferred:

  - truth: "The retirement flow states in the UI that privacy is not retroactive over already-published history (ROADMAP criterion 4, second clause; PROFILE-12 retirement half)."
    addressed_in: "Phase 6"
    evidence: >-
      LOCKED decision D-91 (05-CONTEXT.md:204-215): there is no player-facing retirement flow in
      v0.13 — IDENT-04 records player self-retire as deferred beyond v0.13, and the only retire
      path is `AdminRetireCharacter` in Phase 6. Phase 6 SC 2 carries "admin disable/delete moves a
      character through the same lifecycle states as player-initiated retire". Recorded as item 2
      of filed issue #4963 (OPEN, verified via `gh issue view`). WARNING: Phase 6's `Requirements`
      line in ROADMAP.md does NOT list PROFILE-12, so without the #4963 amendment this clause has
      no roadmap-level home and will be silently dropped.
human_verification:

  - test: "Live create at /characters/new with a full-width-Latin name (e.g. `Ｋａｅｌ`), then observe the roster confirmation."
    expected: "The confirmation names the SERVER-folded ASCII form, not the string typed. A rejected submit preserves all six entered values and moves focus to the name field."
    why_human: "WINDOWS.md row 19 — 05-06 Task 3 human-check recorded UNRUN. Needs a live stack; the E2E covers the happy path but the plan's own walkthrough was never executed."

  - test: "Load /characters with a mixed roster (at least one active, one retired, one default) and exercise the sectioned grid."
    expected: "Playable section first with the create link; Not playable second, expanded, its count chip collapsing the grid out of the flow; the retired card shows Retired and no session word; both echo sites render the server name."
    why_human: "WINDOWS.md row 20 — 05-07 Task 3 human-check recorded UNRUN. Requires a live grid."

  - test: "Open /characters/[id] in two tabs, save a section in tab A, then save a DIFFERENT section in tab B."
    expected: "Tab B's failing section shows the concurrent-edit copy and keeps its typed text; the other four sections are untouched; focus moves to the failed section's first field."
    why_human: "05-04's two-tab conflict walkthrough (D7) recorded unrun. The per-section conflict scoping (D-93) cannot be exercised without two live clients against one row."

  - test: "Confirm the roster ordering expectation for `/characters`."
    expected: "Two consecutive roster loads with no intervening write render the Playable cards in the same order."
    why_human: "Plan 05-01 carried this truth as `verification: backstop` and it is NOT automated-verified. `charRepo.ListByPlayer` has no ORDER BY (WINDOWS.md row 22), so the property holds only by heap-scan accident."

  - test: "Confirm the two UI-SPEC `backstop` truths whose SOLE gate is the ungated web suite — the media renderer and the byte counter."
    expected: "The ProfileMedia renderer and ByteCounter behave as 05-UI-SPEC describes under a live page, independent of the ungated vitest runner."
    why_human: >-
      Both truths are `verification: backstop` and are NOT automated-verified in this report. Their
      only executing gate is the 566-test web suite, which has no Taskfile target and no CI job
      (#4964) — an unwired runner is not a gate, so these remain human items until #4964 lands.
---

# Phase 5: Character Identity UI & Public Profiles — Verification Report

**Phase Goal:** Web players get the whole identity surface — a structured creation card replacing the
name-only stub, one place to manage every alt, and a public profile page a logged-out visitor can
read — plus the media-schema proof with no uploader.

**Verified:** 2026-08-12T21:20:00Z
**Re-verified:** 2026-08-13T08:20:00Z (criterion 2 only)
**Status:** human_needed
**Re-verification:** Yes — targeted re-score of criterion 2 after the 2026-08-13 maintainer ruling
**Tree verified:** HEAD `3b941bd1f`, working tree clean (only untracked `.gsd/`)

## ⚖️ Maintainer ruling — why criterion 2 was re-scored, not silently passed

The initial verification recorded criterion 2 as ✗ FAILED and correctly refused to resolve it,
because the ROADMAP criterion and the locked `01-SPEC.md` contradicted each other about which
viewer rung reaches `profile.rumors` / `profile.currently` / `profile.rp_preferences` /
`profile.timezone`. That was a scope decision, not a code defect.

**On 2026-08-13 the maintainer ruled the SPEC authoritative.** The ROADMAP criterion overclaimed:
widening what a logged-out stranger can read — notably the OOC RP-preferences block and the time
zone — is a privacy decision that must not happen by criterion wording drifting away from a locked
spec. **No code change was made.** The criterion is the artifact being amended, filed as
**amendment 5 on issue #4963**, with this replacement text:

> A **logged-out visitor** loads a character's public profile at a stable URL and sees the in-world
> description alongside the anonymous-tier `profile.*` fields (pronouns), with the richer public
> block — rumors / RP-hooks, the volatile "Currently" line, the OOC RP-preferences block, and time
> zone — becoming visible at the `guest` tier per `01-SPEC.md:1741-1744`; blank fields hide
> themselves and an initial-letter avatar placeholder appears where no image exists.

**This report evaluates criterion 2 against that AMENDED wording, and states so explicitly**
because `.planning/ROADMAP.md` still shows the old text: no GSD verb can rewrite a phase's success
criteria, and the ROADMAP is a tool-owned artifact that must not be hand-edited. A future reader
comparing this report against the ROADMAP will see the divergence — it is tracked in #4963, not an
oversight here.

The ruling settles **which document is authoritative**. It does not instruct a pass, so criterion 2
was re-verified from the codebase below on its own merits.

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Structured six-field creation card replaces the name-only stub; every alt incl. default managed from one place | ✓ VERIFIED | `/characters/new` renders name + pronouns + concept + species + age + faction (`CreateCharacterForm.svelte:136-172`), `type="submit"`, `name` attrs present. Roster is sectioned with `Make default` per playable card (`RosterCard.svelte:107-126`) and the create card is a LINK to `/characters/new` (`CharacterRoster.svelte:117`). 4 E2E specs in `characters-create.spec.ts` + 4 in `characters-roster.spec.ts` pass. `SetDefaultCharacter` integration specs pass (run by me at HEAD). |
| 2 | **(AMENDED — #4963 amdt 5)** Logged-out visitor sees description + anonymous-tier `profile.*` (pronouns); the richer block appears at the `guest` tier per SPEC §8.6; blanks hide; initial-letter avatar | ✓ VERIFIED | **All four clauses hold — see the criterion-2 re-verification section below.** Anonymous reach proven at the policy layer AND end-to-end in a zero-cookie browser context. Guest-tier placement of the richer block matches `seed:profile-tier-floor-guest` exactly. Blank-hiding holds on both wire and client. Avatar placeholder re-confirmed after the code-point change, including an astral initial. |
| 3 | Profile and sheet are separate surfaces; sheet ships empty; action bar carries a NAMED web-DM slot, not a dead button | ✓ VERIFIED | `PublicProfile.svelte:46-49` authors four constants. Sheet = named empty `<section data-testid="sheet">` with heading `Sheet` + body `No sheet system yet.` (`:153-156`). Web-DM slot = two `<span>`s (`Direct messages` / `Not available yet.`) inside a non-interactive `<div data-testid="web-dm">` (`:148-151`) — **not a `<button>`, not a disabled button, not an `<a>`**. Confirmed by reading the markup, not the comment. |
| 4 | Floor evaluated at READ TIME, never stamped onto a row, no backfill; retirement flow + authoring surface state non-retroactivity | ✓ VERIFIED (phase-owned scope) | Read-time half **behaviorally proven by me at HEAD** — see Behavioral Spot-Checks. Authoring-surface statement present once, above the first section, as muted plain text with no icon/border/callout (`characters/[id]/+page.svelte:63-64, 259`, `.standing` style `:348-357`). Retirement clause → `deferred` (Phase 6, D-91, #4963). Latency clause excluded per brief (#4963 item 1). |
| 5 | 1 primary + 10 gallery rows insert through the real schema and read back; an extra primary rejected by `UNIQUE(parent_type,parent_id,name)`; no uploader | ✓ VERIFIED | **Behaviorally proven by me at HEAD** — see Behavioral Spot-Checks. `entity_properties_parent_name_unique UNIQUE(parent_type, parent_id, name)` confirmed at `migrations/000001_baseline.sql:368`; duplicate rejection cited (not reproved) from `TestPropertyRepository_ParentNameUniqueness` (`property_repo_test.go:429-448`), which I read and confirmed genuinely asserts `PROPERTY_DUPLICATE_NAME` against the live constraint. No uploader: the only `upload` hits repo-wide in phase files are three explanatory comments. |

**Score:** 5/5 truths verified (0 present, behavior-unverified)

### Criterion 2 re-verification (against the amended wording)

Four clauses, each verified against the codebase at HEAD `3b941bd1f`.

| Clause | Status | Evidence |
|--------|--------|----------|
| **(a)** Anonymous visitor sees the **in-world description** | ✓ VERIFIED | `seed:viewer-character-description-read` permits `read_description` on `resource is character` when `principal.viewer.tier in ["anonymous", "guest", "player"]` (`internal/access/policy/seed.go:950-956`). Its own comment records that §8.6 seeds the in-world description at the **anonymous** floor (§7.4, §8.11's recorded divergence from strict grid-parity) — so the anonymous rung genuinely clears. `seed:profile-reachable` (`:713`) independently admits all three rungs to the profile at all, so reachability cannot silently deny first. Client: `PublicProfile.svelte:74` renders it gated on `character.description !== ''`. |
| **(b)** Anonymous visitor sees the **anonymous-tier `profile.*` field (pronouns)** | ✓ VERIFIED | `seed:profile-tier-floor-anonymous` (`seed.go:684`) permits `read_profile_attribute` when `tier in ["anonymous","guest","player"] && resource.property.name in ["profile.pronouns"]`. Exactly one name, exactly the one the amended criterion names. Client: `PublicProfile.svelte:65-67` renders it gated on `'profile.pronouns' in p`. |
| **(a)+(b) end-to-end** | ✓ VERIFIED | `web/e2e/public-profile.spec.ts` opens a **brand-new browser context** and asserts `expect(await anon.cookies()).toHaveLength(0)` *before* the read — so a regression that leaked a cookie fails there rather than quietly turning this into an authenticated test. It then asserts the name heading, `getByText(PRONOUNS)` visible, and `getByTestId('description')` equal to the authored description (`:90-96`). This is the first genuinely cookie-less spec in the suite. |
| **(c)** Richer block becomes visible at the **`guest`** tier per `01-SPEC.md:1741-1744` | ✓ VERIFIED | `seed:profile-tier-floor-guest` (`seed.go:690`) is gated `tier in ["guest","player"]` and its name list opens with exactly `profile.rumors`, `profile.currently`, `profile.rp_preferences`, `profile.timezone` (plus concept/species/age/faction/appearance/personality/biography and the eleven media names). The anonymous rung is excluded by construction. This is precisely the placement the amended criterion asserts and that §8.6 locks — the seed corpus and the amended criterion now agree. |
| **(d)** Blank fields **hide themselves** | ✓ VERIFIED | Two independent layers. Server: `projectPublic`/`projectOwner` (`internal/grpc/characteraccess_projection.go`) `continue` on `value == ""`, so a blank never reaches the wire. Client: every element in `PublicProfile.svelte` is conditioned on **key presence** (`'profile.X' in p`) with no expected-field list, nothing iterating the map, no counts, no greyed sections — the absence contract §8.9 requires. Headings live *inside* their body's conditional (`:100-136`), so a bare heading can never leak a count of one. Description uses the `!== ''` twin. A viewer therefore cannot distinguish a field left blank from one the floor withheld. |
| **(e)** **Initial-letter avatar placeholder** where no image exists | ✓ VERIFIED — re-confirmed after the post-review code-point change | `PublicProfile.svelte:56-60`: `{#if character.primaryImage !== undefined}` → `ProfileMedia`, `{:else}` → `CharacterPortrait`. So the placeholder is the *else* branch of image presence — exactly "where no image exists". `CharacterPortrait.svelte` derives `const initial = $derived([...name][0] ?? '')` — spread iteration, so the glyph is the first **code point**, not the first UTF-16 code unit. Behaviorally re-proven at HEAD (below). |

**Behavioral re-check of clause (e).** The brief flagged that `CharacterPortrait` was changed
post-review to index by code point (`[...name][0]`), so presence of the component is not sufficient
evidence that the placeholder still renders. I ran the single covering test file at HEAD:

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Initial-letter placeholder renders, incl. astral initial | `pnpm test:unit --run src/lib/components/characters/CharacterPortrait.svelte.test.ts` | **exit 0**, 1 file / **4 tests passed**, 837ms | ✓ PASS |

The astral spec is genuinely discriminating rather than a presence check. It renders
`U+10400 DESERET CAPITAL LETTER LONG I` + `shan` and asserts three clauses: the text equals the
whole code point, `text.length === 2` (a lone high surrogate is length 1 and would satisfy a bare
equality against `text[0]`), and `text).not.toBe(DESERET_LONG_I.charAt(0))` — the exact regression
the code-point change was made to prevent. The suite also pins the empty-name case to `''` rather
than the string `undefined`, and pins that casing is left to `text-transform` rather than done in
script. I read the assertions before running them; this is not a `Skip`-shaped or vacuous test.

**Conclusion:** criterion 2 holds **as amended**, on the codebase, in all five clauses. It did NOT
hold as originally worded, and this report does not claim otherwise — the difference is the
maintainer's ruling on which document is authoritative, recorded above and in #4963.

### Deferred Items

| # | Item | Addressed In | Evidence |
|---|------|--------------|----------|
| 1 | Criterion 4's retirement-flow non-retroactivity statement (PROFILE-12 retirement half) | Phase 6 | LOCKED D-91: no player-facing retirement flow exists in v0.13; only `AdminRetireCharacter` (Phase 6). Filed as item 2 of **#4963** (verified OPEN). ⚠️ Phase 6's ROADMAP `Requirements` line omits PROFILE-12 — the amendment is required or this clause loses its home. |

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `web/src/routes/c/[id]/+page.svelte` | Public profile route, outside `(authed)` | ✓ VERIFIED | Sibling of `login`/`register`; no `+page.ts`; ONE failure predicate (`isNotFoundError`) collapsing not-found and below-floor into one render. |
| `web/src/lib/components/characters/PublicProfile.svelte` | Props-only absence-contract renderer | ✓ VERIFIED | Every element gated on `'key' in p`. No expected-field list, nothing iterates the map, no counts, no lock icons, no greyed sections. |
| `web/src/lib/components/characters/CharacterPortrait.svelte` | Initial-letter avatar placeholder (criterion 2 clause e) | ✓ VERIFIED | Code-point-indexed initial (`[...name][0] ?? ''`); rendered as the `{:else}` of `primaryImage !== undefined`. 4/4 covering tests pass at HEAD, including the discriminating astral spec. |
| `web/src/routes/(authed)/characters/[id]/+page.svelte` | Five-section authoring surface | ✓ VERIFIED | Five `ProfileSection`s, five distinct Save labels, one `version` cell, per-section masks. Union of sections 1/3/4/5 = the twelve maskable paths, each once. |
| `web/src/routes/(authed)/characters/new/+page.svelte` | Six-field creation card | ✓ VERIFIED | 68 lines, real route. |
| `web/src/lib/components/characters/CharacterRoster.svelte` | Sectioned roster | ✓ VERIFIED | Playable/Not-playable partition expressed as `status === 'active'` vs else; second section omitted entirely when empty; chip carries `aria-expanded` + `aria-controls`. |
| `web/src/lib/components/characters/RosterCard.svelte` | Badge matrix + CR-01 entry point | ✓ VERIFIED | See CR-01 below. |
| `internal/grpc/characteraccess_projection.go` | Sole projector, absence-not-emptiness | ✓ VERIFIED | `if value == "" { continue }` on both `projectPublic` and `projectOwner`; media routed out of the text map; gallery order from a slice, never map iteration. |
| `internal/access/policy/seed.go` | Tier-floor corpus matching SPEC §8.6 | ✓ VERIFIED | `seed:profile-tier-floor-anonymous` = pronouns only; `seed:profile-tier-floor-guest` = the richer block; `seed:viewer-character-description-read` = description at the anonymous floor. Matches the amended criterion 2 and §8.6 exactly. |
| `test/integration/access/media_schema_test.go` | Criterion 5 proof | ✓ VERIFIED | 307 lines, real fixtures, scrambled insert order, marshaled-bytes assertions. |
| `test/integration/access/character_readtime_floor_test.go` | Criterion 4 proof | ✓ VERIFIED | Corpus mutation is the only variable; untouched-second-character targeting control; asserts `characters.version` AND every property tuple unchanged. |
| Retirement-flow UI carrying the PROFILE-12 statement | Criterion 4, clause 2 | ✗ MISSING | No retirement surface exists in `web/src` at all. Deferred (above). |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `RosterCard.svelte` | `/characters/[id]` | unconditional `<a href="/characters/{id}">Edit profile →</a>` | ✓ WIRED | **CR-01 fix confirmed real.** Rendered inside `{#snippet body()}` (`:149-154`), which BOTH the playable and non-playable card branches render (`:195`, `:199`). Not gated on `playable`. `stopPropagation` prevents the card's select handler from firing; the `e.target !== e.currentTarget` keydown guard (`:188`) keeps the link keyboard-activatable. |
| `characters/[id]/+page.svelte` | `/c/[id]` | `href={`/c/${character.id}`}` | ✓ WIRED | Keyed on the ID, never the name (`:256`). |
| `c/[id]/+page.svelte` | `getCharacterProfile` | `$effect` reading `id` + `inFlight` last-request token | ✓ WIRED | WR-03 fix real: load follows the param, not the mount. |
| `PublicProfile.svelte` | wire response | `character.profile` map key presence | ✓ WIRED | No client-side allowlist. |
| `PublicProfile.svelte` | `CharacterPortrait` | `{:else}` of `character.primaryImage !== undefined` | ✓ WIRED | The placeholder is reached exactly when no primary image exists — criterion 2 clause (e). |
| `projectPublic` | admitted pairs only | `resolveVisibleProfile` conjunction | ✓ WIRED | Function holds nothing the §8.5.1 conjunction did not permit — it cannot leak by forgetting a filter. |
| Anonymous viewer | pronouns + in-world description | `seed:profile-tier-floor-anonymous` + `seed:viewer-character-description-read` + `seed:profile-reachable` | ✓ WIRED | All three clear `tier == "anonymous"`. This is the amended criterion 2's anonymous reach, and it is exactly what the zero-cookie E2E observes. |
| Guest viewer | rumors / currently / rp_preferences / timezone | `seed:profile-tier-floor-guest` | ✓ WIRED | Gated `tier in ["guest","player"]`, per SPEC §8.6 and the amended criterion. **Previously recorded as ✗ NOT_WIRED against the un-amended criterion; the wiring never changed — the criterion did.** |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|--------|
| `PublicProfile.svelte` | `character.profile` | `GetCharacterProfile` → `projectPublic` → `entity_properties` rows | ✓ | ✓ FLOWING |
| `PublicProfile.svelte` | `character.description` | `world.GetCharacterDescription` (ABAC-gated on the viewer subject) | ✓ | ✓ FLOWING |
| `PublicProfile.svelte` | `character.gallery` | `profileGallerySlotNames` iteration over admitted media rows | ✓ | ✓ FLOWING (proven by M1 with real DB rows) |
| `CharacterPortrait.svelte` | `initial` | `character.name` ← `characters.name` column, code-point indexed | ✓ | ✓ FLOWING (derived from a real name, not a literal) |
| `characters/[id]` | `version` | `OwnCharacter.version` ← `characters.version` column | ✓ | ✓ FLOWING |
| `CharacterRoster` | `characters` | `ListMyCharacters` + session-bearing list, joined in the route | ✓ | ✓ FLOWING |

### Behavioral Spot-Checks

I ran the integration tier myself during the initial verification because it was **absent from the
established gate evidence** (`task build` / `test` / `lint` / `test:e2e` do not compile or run
`//go:build integration` files, and the two specs that carry criteria 4 and 5 live there). Those
results stand at this HEAD and were NOT re-run for this targeted re-verification.

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Integration package compiles at HEAD | `go vet -tags integration ./test/integration/access/...` | exit 0 | ✓ PASS |
| Whole access suite green at HEAD | `task test:int -- ./test/integration/access/...` | exit 0, 2.647s, coverage 7.5% of `./...` | ✓ PASS |
| **Non-vacuity control** | same, `-ginkgo.focus=ZZZ_NO_SUCH_SPEC_ZZZ` | exit 0, **0.408s, coverage 0.7%** | ✓ PASS — proves the unfocused run really executed specs rather than skipping |
| Criterion 5 — media schema | `-ginkgo.focus=EXT-05: the media naming scheme survives the viewer-filtered read path` | exit 0, 2.099s, coverage **4.7%** (vs 0.7% control) | ✓ PASS |
| Criterion 4 — read-time floor | `-ginkgo.focus=the viewer-tier floor is evaluated at read time and never stamped onto a row` | exit 0, 2.097s, coverage **4.4%** (vs 0.7% control) | ✓ PASS |
| Web unit/component suite at HEAD | `pnpm test:unit --run` | exit 0 — 57 files, **566 tests passed** | ✓ PASS (but UNGATED — #4964) |
| **[re-verification]** Criterion 2 clause (e) — avatar placeholder incl. astral initial | `pnpm test:unit --run src/lib/components/characters/CharacterPortrait.svelte.test.ts` | **exit 0**, 4 tests passed, 837ms | ✓ PASS |

Both criterion-bearing integration specs pass **individually and non-vacuously** at HEAD, after the
ten post-review fix commits. The coverage delta against the empty-focus control is the non-vacuity
proof: a skipped suite cannot execute 4.4–4.7% of the repo.

### Requirements Coverage

All 12 phase requirement IDs are claimed by at least one plan; none is orphaned. Per the brief,
REQUIREMENTS.md's traceability table still reads `Pending` for all 12 rows due to the
`table_unmatched` GSD tooling defect — each ID below is judged against the CODE, not the table.

| Requirement | Source Plan(s) | Status | Evidence |
|-------------|----------------|--------|----------|
| IDENT-01 | 05-03, 05-06, 05-07, 05-08 | ✓ SATISFIED | One-call `CreateCharacter` with the six fields; `/characters/new`; E2E creates a character end-to-end. |
| IDENT-05 | 05-01, 05-07, 05-08 | ✓ SATISFIED | `SetDefaultCharacter` + narrow single-column UPDATE; sectioned roster; E2E moves the Default badge without a reload. |
| PROFILE-01 | 05-02, 05-05, 05-08 | ✓ SATISFIED | Stable `/c/[id]` outside `(authed)`; first genuinely cookie-less E2E in the suite. |
| PROFILE-02 | 05-02 | ✓ SATISFIED | Sheet is a separate named empty section on the profile. |
| PROFILE-06 | 05-02, 05-04 | ✓ SATISFIED | "The profile **carries** a rumors / RP-hooks field" — it does: authored via the owner surface, stored as `profile.rumors`, rendered by `PublicProfile.svelte:122-127` on key presence, admitted at the `guest` floor per §8.6. **Re-scored from ⚠️ PARTIAL:** the partial was driven solely by the un-amended criterion 2's anonymous-reach reading, which the maintainer has now ruled non-authoritative. The requirement text itself says nothing about viewer tier. |
| PROFILE-07 | 05-02, 05-04 | ✓ SATISFIED | As above, for the volatile "Currently" line (`profile.currently`, rendered as a fact pill, `:89-91`). Re-scored from ⚠️ PARTIAL for the same reason. |
| PROFILE-08 | 05-02, 05-04 | ✓ SATISFIED | As above, for the OOC RP-preferences block. Correctly renders LAST under an out-of-character heading (`:130-136`), and writes only via the mask path, never `characters.preferences`. Re-scored from ⚠️ PARTIAL. |
| PROFILE-09 | 05-02, 05-04 | ✓ SATISFIED | As above, for time zone (`profile.timezone`, fact pill `:92-94`). Re-scored from ⚠️ PARTIAL. |
| PROFILE-10a | 05-02 | ✓ SATISFIED | In-world description renders on the public profile and IS at the anonymous floor (`seed.go:950-956`); empty description omitted. |
| PROFILE-12 | 05-04, 05-08 | ⚠️ PARTIAL | Authoring-surface half shipped and E2E-asserted. Retirement half deferred to Phase 6 (D-91, #4963). REQUIREMENTS.md marks it `[x]` — an overclaim while only one half ships. **Unchanged by the ruling.** |
| EXT-05 | 05-05 | ✓ SATISFIED | Proven by me at HEAD; no uploader. |
| EXT-08 | 05-02 | ✓ SATISFIED | Named, non-interactive web-DM slot. |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | `TBD` / `FIXME` / `XXX` | — | **None.** Scanned all 232 phase-modified `.go`/`.ts`/`.svelte`/`.proto`/`.sql` files — zero unreferenced debt markers. |
| — | — | `TODO` / `HACK` / `PLACEHOLDER` | — | **None** outside the legitimate "initial-letter placeholder" avatar vocabulary. |
| `web/src/lib/components/characters/*` | — | Ungated test suite | ⚠️ Warning | 566 passing tests with no Taskfile target and no CI job (#4964). Two UI-SPEC `backstop` truths — the media renderer and the byte counter — have this ungated runner as their SOLE gate, and remain human-verification items for that reason. Note this also covers `CharacterPortrait.svelte.test.ts`, which I ran manually above: it passes, but nothing in CI runs it. |

### Code Review Fix Verification (05-REVIEW.md: 1 blocker + 8 warnings)

Spot-verified the two highest-risk fixes in the code, not the summary:

- **CR-01 (BLOCKER) — real and complete.** The `Edit profile →` link is rendered inside the shared
  `{#snippet body()}`, which both the `{#if playable}` and `{:else}` card branches invoke. It is
  reachable for playable AND non-playable characters, as the task required. The keydown guard at
  `RosterCard.svelte:188` (`e.target !== e.currentTarget`) is what keeps it keyboard-activatable
  rather than mouse-only — a subtlety a naive fix would have missed.

- **WR-08 — real.** `WebCreateCharacterResponse` field reservation confirmed in the proto.
- **WR-03 — real.** Both `[id]` routes now load inside a `$effect` reading `id`, with an `inFlight`
  last-request token guarding every post-await write.

### Gaps Summary

**No gaps remain.** The sole gap from the initial verification — criterion 2 — was a
ROADMAP-vs-SPEC contradiction, not a code defect, and the maintainer has ruled the SPEC
authoritative (2026-08-13, #4963 amendment 5). Re-verified against the amended wording, criterion 2
holds in all five clauses on the codebase at HEAD `3b941bd1f`, with no code change. The status moves
to `human_needed` on the strength of the five outstanding human items below, not on any deficiency
in the implementation.

**The absence contract is genuinely sound.** I probed for any way a viewer could tell a blank field
from a withheld one and found none: the server drops empty values before they reach the wire
(`projectPublic`/`projectOwner`), the projector's input is already the admitted-pairs set so it has
nothing to leak, and the client conditions on key presence with no expected-field list, no counts,
no greyed sections and no placeholder chrome. Not-found and below-floor collapse into one code, one
message literal and one render, with reachability evaluated before the character row is ever read.

**One criterion clause still has no home in the roadmap.** Criterion 4's retirement half is
correctly deferred by locked decision D-91 and filed as #4963 item 2, but Phase 6's `Requirements`
line does not list PROFILE-12. Unless #4963 is actioned, that clause is dropped silently — and
REQUIREMENTS.md already marks PROFILE-12 `[x]` while only half of it ships.

**Verification durability is the standing risk, and it is what keeps this phase at `human_needed`.**
The integration tier that proves criteria 4 and 5 was not part of the phase's gate evidence (I ran
it), and the 566 web tests are ungated entirely (#4964) — including the `CharacterPortrait` file
that carries criterion 2's clause (e), which I had to run by hand for this re-verification. Two
UI-SPEC truths (the media renderer, the byte counter) are `verification: backstop` with that
unwired runner as their only gate, and the roster-ordering truth is `backstop` with no gate at all
(`charRepo.ListByPlayer` has no `ORDER BY`). All three are recorded as human items and none is
counted toward the 5/5. Both defects are filed; neither is fixed.

---

_Verified: 2026-08-12T21:20:00Z_
_Re-verified (criterion 2 only): 2026-08-13T08:20:00Z_
_Verifier: Claude (gsd-verifier)_
