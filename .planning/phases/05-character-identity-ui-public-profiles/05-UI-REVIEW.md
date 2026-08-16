# Phase 5 — UI Review

**Audited:** 2026-08-13
**Baseline:** `05-UI-SPEC.md` (approved design contract)
**Method:** code audit (subagent) **+ a live rendered pass** against a real `docker compose` stack at
`localhost:8080`, driven with Playwright in both Default Light and Default Dark, at 1280px and 375px.
The live pass RESOLVED the three items the code audit could not settle without a browser — one of which
it disproved. See [Live Rendered Verification](#live-rendered-verification).
**Scope:** 4 routes + 8 components under `web/src/lib/components/characters/`

---

## Pillar Scores

| Pillar | Score | Key Finding |
|--------|-------|-------------|
| 1. Copywriting | 4/4 | Every contracted string matches verbatim; the five Saves are distinct with zero `aria-label` overrides anywhere in the phase. One off-family error string. |
| 2. Visuals | 3/4 | Structure and absence contract are exact. The contracted roster-card hover affordance was never implemented. |
| 3. Color | 2/4 | Three uses outside the closed accent reserved-for list, one of them a page `<h1>`, plus a phase-introduced green token the contract does not admit. |
| 4. Typography | 3/4 | 4 sizes / 2 weights held almost everywhere; roster card controls ship an undeclared 12px/400 pair. |
| 5. Spacing | 2/4 | `/characters` ships no 768px breakpoint and a 24px gutter that is not on the scale; three sibling pages carry three different page paddings; badge padding `1px 6px`; roster controls fall ~17px under the 44px target floor. |
| 6. Experience Design | **3/4** (was 4/4) | Per-section save, one version cell, last-request tokens, focus management, live regions pre-mounted — all genuinely strong. Lowered by the live pass, which found two things code review could not see: a signed-in player is shown signed-out chrome on `/c/[id]`, and `ByteCounter` announces neither its appearance at 80% nor its over-cap flip at 100%. |

**Overall: 17/24** — code audit scored 18/24; the live rendered pass lowered Experience Design from
4/4 to 3/4 on two findings invisible to code review, and withdrew one Visuals NIT it disproved (the
withdrawal did not raise Visuals, whose score rests on the un-built roster-card hover).

---

## Top 3 Priority Fixes

1. **BLOCKER — `/characters/new` heading is painted accent.** `web/src/routes/(authed)/characters/new/+page.svelte:61` sets `h1 { color: var(--color-primary) }`. UI-SPEC:134-149 makes the accent list closed and six items long; a page heading is on neither that list nor the explicit not-accent list. Worse, the file's own comment (`:27-28`) claims "same heading treatment" as the roster — but the roster's `.title` is `--color-foreground` (`characters/+page.svelte:235`) and `/characters/[id]`'s `h1` sets no colour at all (`[id]/+page.svelte:324-329`). **Fix:** delete the `color` declaration; the heading inherits `--color-foreground` like its two siblings.
2. **BLOCKER — `/characters` has no responsive gutter.** `characters/+page.svelte:219-225` is a flat `padding: 32px 24px` with no `@media (min-width: 768px)` block anywhere in the file. UI-SPEC:235-237 fixes one breakpoint at 768px, gutters `lg`(16) below and `3xl`(48) above; 24px is not a declared value and is not on the exception table (UI-SPEC:82-89). The two sibling routes implement it correctly (`c/[id]/+page.svelte:128-138`, `[id]/+page.svelte:304-318`), so the roster is the odd one and the divergence is visible as a gutter jump when navigating between them. **Fix:** adopt `padding: 32px 16px` + the 768px `32px 48px` override.
3. **WARNING — the contracted roster-card hover never shipped.** UI-SPEC:145 reserves accent item 4 for "The hover border on a clickable roster card (`border-color: var(--color-primary)`)", and UI-SPEC:489 specifies it as a `border-color` transition ≤150ms. `RosterCard.svelte:217-223` defines `.card-playable` with `cursor: pointer` and a `:focus-visible` ring and **no `:hover` rule and no `transition`**. The create card got it (`CharacterRoster.svelte:228-230`) — the real card did not, so the one grid whose decisive property is uniform clickability gives a mouse user no feedback that it is clickable. **Fix:** add `.card-playable:hover { border-color: var(--color-primary); }` plus `transition: border-color 150ms` inside the existing reduced-motion posture.

---

## Detailed Findings

### Pillar 1: Copywriting (4/4)

Genuinely strong; the contract's copy table is reproduced essentially verbatim. Spot-verified:

- The **five Saves** carry distinct visible verb+noun labels: `Save identity` / `Save in-world description` / `Save appearance & history` / `Save hooks & current` / `Save out of character` (`[id]/+page.svelte:263, 271, 279, 287, 296`), each threaded into `ProfileSection.svelte:179-181` as the button's only text node. `rg aria-label` across every non-test file in this phase returns **zero hits** — the iteration-1 blocker is fully repaired and there is no visible-vs-accessible-name divergence anywhere.
- **PROFILE-12** appears exactly once in the codebase (`[id]/+page.svelte:63-64`), byte-identical to UI-SPEC:404, rendered above section 1 as muted body text with no border/icon/callout (`.standing`, `:351-357`). Not duplicated, not reworded.
- Error map matches: `That name is taken. Try another.` (`CreateCharacterForm.svelte:57`), verbatim server passthrough for `InvalidArgument` via `rawMessage` with the `That name can't be used here.` fallback (`:58, 99-103`), concurrent-edit copy verbatim (`ProfileSection.svelte:92-93`), not-found `Not found` / `We couldn't find that page.` / `Home` with no path echo and no reason (`c/[id]/+page.svelte:108-112`).
- Forbidden strings absent: no `HoloMUSH` in phase copy, no "permanent", no rename implication (`Names are reserved once taken.`, `CharacterRoster.svelte:121`), no "private"/"access denied", no "hidden" on the collapse chip (`Hide 3 characters` / `Show 3 characters`, `:69-73`).

Findings:

- **WARNING — one string is off-family.** `characters/+page.svelte:139`: `'Failed to select character. Try again.'`. Every other authored failure in the phase opens `Couldn't …` (`Couldn't save.`, `Couldn't create the character.`, `Couldn't load your characters.`, `Couldn't load this character.`). `Failed to <verb>` is the system reporting on itself; the rest of the phase speaks to the player. Suggest `Couldn't start that character. Try again.`
- **WARNING — an uncontracted success variant.** `characters/+page.svelte:192-193` renders `Created {name}. Some details didn't save — add them on the character page.` UI-SPEC:463 declares only `Created {name}.`. The variant is defensible (it makes a partial write a one-click repair) but it is copy this phase authored past its own contract; it should be back-filled into the copy table rather than left as a code-only string.
- **NIT — leaked field vocabulary.** `[id]/+page.svelte:128` labels the OOC textarea `RP preferences`. Everywhere else the phase spells this concept out in player words (`Out of character`, UI-SPEC:262). `RP preferences` is the wire name (`profile.rp_preferences`) with an underscore removed.
- **NIT — the create CTA drops its glyph from the accessible name.** UI-SPEC:379 specifies `+ Create a character`; `CharacterRoster.svelte:117-122` renders the `+` in an `aria-hidden` plate, so the accessible name is `Create a character`. Defensible and arguably better; recording it as a deliberate divergence.

### Pillar 2: Visuals (3/4)

The hard structural rules all hold. Verified against the contract:

- **Absence contract (UI-SPEC:201-228) is exact.** `PublicProfile.svelte` conditions every element on `'profile.x' in p` and holds no field list to diff against (`:66-138`); each heading lives *inside* its body's conditional, so a bare heading is unreachable. `ProfileMedia.svelte:98-169` renders no wrapper, no heading and no slot at zero rows, and a broken primary degrades to the identical initial-letter plate (`:98-99`) rather than a broken-image glyph. No sibling reintroduces a placeholder — the only always-on elements are the two named slots the contract sanctions (`PublicProfile.svelte:148-156`).
- **Retired card leaves no gap** (audit item 4). `RosterCard.svelte:95-104`: the `.badges` flex row is always present and the non-active branch substitutes a single `Retired` badge in the same slot — one badge instead of one-or-two, no reserved space, no misalignment. Correct.
- **Public profile order** matches UI-SPEC:250-268 exactly: card → Appearance → Personality → Biography → Rumours & hooks → Out of character (last) → gallery → web-DM slot → Sheet.
- **Empty roster** (audit item 6) is specified and implemented: `CharacterRoster.svelte:77-79` renders `No characters yet` / `Create one to step into the world.` and still renders the create card below, matching UI-SPEC:418.

Findings:

- **WARNING (Top-3 #3) — no hover on the clickable roster card.** `RosterCard.svelte:217-223`.
- **WARNING — a revealed gallery tile stays covered.** `ProfileMedia.svelte:159-164` gives the gallery reveal button no `class:revealed`, unlike the primary's (`:120-122`). `.reveal` is `position: absolute; inset: 0` (`:207-213`), and `.reveal.revealed` — the rule that shrinks it to a bottom strip (`:225-228`) — can therefore never apply to a tile. After a viewer reveals a tile, the blur lifts but a full-bleed translucent overlay carrying the warning text remains over the image. Unreachable in v0.13 (no media rows exist) but this renderer is exactly what the UI-SPEC:526 backstop row says ships now with the data arriving later.
- **~~NIT — an always-present empty status paragraph.~~ WITHDRAWN — disproven in the browser.** The code audit predicted that `ProfileSection.svelte:187`'s unconditional `<p class="status" role="status">` would defeat its own `.status:empty` rule (`:283-285`), because Svelte emits an empty *text node* into that element — leaving a permanent 16px gap in all five sections. **This is not what happens.** Measured live on `/characters/[id]`: all five `.status` elements report `matches(':empty') === true`, `display: none`, `height: 0`, each with exactly one zero-length text-node child. The premise "CSS `:empty` does not match an element with a text-node child" is false as stated — `:empty` ignores *zero-length* text nodes, so Svelte's placeholder does not defeat it. No gap exists and no fix is owed. Recorded rather than deleted because the reasoning is plausible and would otherwise be re-derived by the next reader.

### Pillar 3: Color (2/4)

Positives worth stating: **zero hardcoded hex** in any phase file (`rg '#[0-9a-fA-F]{3,8}'` over the four routes and eight components → no matches), **zero amber** anywhere, no bare `--primary`-style names, no `var()` inside `@theme`. The portrait tint is unified across both sizes through one component (`CharacterPortrait.svelte:61-63`), which is exactly what UI-SPEC:151-153 asked for.

The score is dragged down by the accent list, which UI-SPEC:134 declares **closed** ("Anything not on it is not accent"). Four violations:

1. **BLOCKER — `new/+page.svelte:61`** `h1 { color: var(--color-primary) }`. See Top-3 #1.
2. **WARNING — `c/[id]/+page.svelte:179`** colours both the not-found `Home` link and the generic-error `Try again` button `--color-primary`. UI-SPEC:149 explicitly names "the not-found page" in the **not-accent** list.
3. **WARNING — `[id]/+page.svelte:391`** the load-failure `Try again` button is `--color-primary`. Same class as (2); the roster's equivalent retry is correctly `--color-foreground` (`characters/+page.svelte:263`), so the three retry controls in this phase are not even consistent with each other.
4. **WARNING — undeclared green on the session badge.** `RosterCard.svelte:260-263` uses `--color-status-online` (`#7fd98f`, `app.css:44`). UI-SPEC:147 lists the session badge under **not accent** and the colour table (UI-SPEC:126-132) admits no green role at all. `git log -S` confirms this token entered RosterCard in this phase's own commit `fdcd459c3`, so it is a phase-introduced colour, not inherited. Mitigating: the badge carries the word `Active`/`Offline`, so colour is not the sole carrier and the contrast is comfortable — this is a contract-conformance finding, not an accessibility one. **Confirmed live:** `--color-status-online` resolves to `#7fd98f` against the Default Dark ground for a measured **12.22:1**, so the legibility half is settled and only the contract question remains — amend UI-SPEC's colour table to admit a status-green role, or drop the token.

`--color-destructive` is used correctly and only for error text and error-region borders (`ProfileSection.svelte:292-301`, `CreateCharacterForm.svelte:251-262`, `characters/+page.svelte:249-251`), including the ByteCounter `over` state (`ByteCounter.svelte:53-55`) — audit item 2's colour half passes with no hardcoded red.

### Pillar 4: Typography (3/4)

The 4-size / 2-weight scale (UI-SPEC:100-107) holds across the great majority of declarations: 20/600 headings, 14/400 body, 12/600 labels and section headings with `.06em` uppercase tracking, 32/600 for the portrait letter only (`CharacterPortrait.svelte:38` — 20/600 at the 44px plate, also a declared pair). No monospace anywhere in the phase. No sub-12px sizes.

Findings:

- **WARNING — undeclared 12px/400 pair, twice.** `RosterCard.svelte:278-293` sets the `Make default` / `Edit profile →` / `View profile →` controls to `font-size: 12px; font-weight: 400`, and `CharacterRoster.svelte:259-264` does the same for `.create-sub`. UI-SPEC:104 assigns 12px exclusively to the Label role at weight **600**; 14/400 is Body. 12/400 is a fifth pair the contract does not declare, and it lands on the roster's three most-used controls.
- **NIT — a size outside the scale.** `CharacterRoster.svelte:244` sets the create plate's `+` glyph to `font-size: 20px`. That is the Heading size being used as an icon metric rather than as a heading; the portrait plate does the same thing but is explicitly sanctioned (UI-SPEC:107 Display, and `CharacterPortrait.svelte:36-38` documents the 20px roster case). The create plate is undocumented. Low impact — it is one aria-hidden glyph.

### Pillar 5: Spacing (2/4)

Most values are on the 4px scale and the intent is clearly present (16px card padding, 8px label-to-control, 12px grid gap, 24px between sections, `repeat(auto-fill, minmax(200px, 1fr))` above 768px — `CharacterRoster.svelte:209-213, 265-269`). Three real divergences and one accessibility-grade one:

- **BLOCKER — `/characters` ships no breakpoint.** See Top-3 #2. `characters/+page.svelte:219-225`.
- **WARNING — three sibling pages, three page paddings.** `/c/[id]` and `/characters/[id]` both do `32px 16px` → `32px 48px` (correct). `/characters/new` does `16px` → `48px` (`new/+page.svelte:47, 63-67`) — its top padding is 16px below the breakpoint and 48px above, against UI-SPEC:79's `2xl` (32px) page top padding. `/characters` does `32px 24px` flat. Navigating the three owner surfaces produces three different content-top positions.
- **WARNING — roster controls are ~27px tall, against a 44px floor.** `RosterCard.svelte:278-294`: `padding: 4px 8px` at `12px/1.4` ⇒ ≈16.8 + 8 + 2 border ≈ **27px**. UI-SPEC:88 makes 44×44 an unqualified floor: "Every button, link-card and collapse chip has ≥44px of hit area, padding included, **at every band**." The collapse chip honours it (`CharacterRoster.svelte:189`), the Saves honour it (`ProfileSection.svelte:261`), the retries honour it — the three controls a player touches most on the roster do not. **Fix:** add `min-height: 44px` to the shared `.make-default, .edit-character, .view-profile` rule.
- **NIT — off-scale badge padding.** `RosterCard.svelte:244` `padding: 1px 6px`. Neither value is a multiple of 4 and neither is on the exception table (UI-SPEC:82-89), whose fact-pill analogue is explicitly `4px`. The public profile's pill gets it right (`PublicProfile.svelte:212` `padding: 4px 8px`), so the two badge-ish primitives in the phase disagree.

### Pillar 6: Experience Design (4/4)

The strongest pillar, and the state coverage is real rather than nominal:

- **Loading / error / empty** are covered on all four routes with the contracted copy, and the loading state is a single `role="status"` line with no skeleton on `/c/[id]` (`c/[id]/+page.svelte:96-101`) exactly as UI-SPEC:429 requires.
- **In-flight submits** keep their label and gain `disabled` + `aria-busy` with no spinner and no label swap (`ProfileSection.svelte:179`, `CreateCharacterForm.svelte:175`, `RosterCard.svelte:114-125`) — matches UI-SPEC:432 precisely.
- **Live regions are pre-mounted**, so the first message is announced (`ProfileSection.svelte:184-187`, `characters/+page.svelte:169-199`). Error regions are `role="alert"`, confirmations `role="status"`.
- **Focus management** on failure: `ProfileSection.svelte:130` focuses the section's first field, `CreateCharacterForm.svelte:121` returns focus to the name field — UI-SPEC:487 satisfied.
- **Partial-state preservation**: a rejected create resets nothing (`CreateCharacterForm.svelte:116-122`), and a failed save keeps `working` untouched (`ProfileSection.svelte:121-130`). The UI-SPEC:530 precondition holds.
- **Per-section save** is implemented as contracted: one version cell (`[id]/+page.svelte:56`), each Save sending only its own paths (`ProfileSection.svelte:115-118`), a conflict scoped to one section, Save disabled when not dirty via a loaded-vs-working diff rather than an input flag (`:96`).
- **ByteCounter display rule** (audit item 2) is `shown = bytes >= maxBytes * 0.8` (`ByteCounter.svelte:36`), matching UI-SPEC:491, with `over` mirroring the server's strict `>` (`:35`) and `TextEncoder` byte measurement (`:34`).
- Keyboard: the playable card's `keydown` handler guards on `e.target !== e.currentTarget` (`RosterCard.svelte:188`) so nested controls keep their own activation — a subtle bug that was actually avoided.

Two small gaps, neither structural:

- **NIT — the name counter is unconditional.** `CreateCharacterForm.svelte:142` renders `{nameRunes} / 32` from first paint, while the five sibling fields render nothing until 80% of cap. The name cap is in runes, not bytes, so it is technically outside UI-SPEC:491's rule — but the result is that an untouched create form shows `0 / 32` under exactly one of six fields, which is the numeric chrome the 80% rule exists to suppress and sits oddly beside UI-SPEC:423 ("Authoring form, all fields blank | No copy"). Suggest applying the same 80% gate.
- **NIT — no `prefers-reduced-motion` posture.** The phase ships no transitions at all, so nothing violates UI-SPEC:489 today; but the fix for Top-3 #3 adds one, and it must land inside the existing `@media (prefers-reduced-motion: no-preference)` block rather than bare.

---

## Registry Safety

`web/components.json` exists (shadcn-svelte initialized), and UI-SPEC:544-553 declares **no third-party registry** and **zero blocks added** for this phase. Confirmed against the code: all eight components are hand-authored Svelte with scoped `<style>`, importing only `$lib/utils`'s `cn` and generated protobuf types — no `$lib/components/ui/*` import appears in any phase file. **Registry audit: 0 third-party blocks, no flags. Gate not owed.**

---

## Live Rendered Verification

Run against a real stack (`docker compose up -d`, gateway serving the embedded SvelteKit bundle at
`localhost:8080`), Playwright-driven, both shipped themes, 1280px and 375px. Everything below is a
**measurement**, not a reading of the source.

### Code-audit claims the browser CONFIRMED

| Claim | Measured |
|---|---|
| Top-3 #1 — `/characters/new`'s `h1` is accent-painted while its siblings are not | **Confirmed.** `/characters/new` h1 computes `rgb(61,214,247)` = `--color-primary` `#3dd6f7`; `/characters/[id]` h1 computes `#e8edf2` = `--color-foreground`. The two sibling headings genuinely disagree at runtime |
| Top-3 #2 — `/characters` ships no 768px breakpoint | **Confirmed and measured.** At a 375px viewport: `/characters/new` = 16px gutter, `/characters/[id]` = 16px, `/characters` = **24px** flat. Navigating between them at mobile width visibly jumps the gutter. No horizontal overflow at 375px on any of the three, so this is polish, not breakage |
| Pillar 6 NIT — the name counter is unconditional | **Confirmed.** An untouched `/characters/new` renders exactly one counter, `data-testid="name-counter"`, reading `0 / 32`, while all five `ByteCounter` siblings render nothing below 80% of cap |
| Pillar 2 — the absence contract holds | **Confirmed on a deliberately sparse profile.** `/c/[id]` for a character with only identity fields set mentions **none** of `appearance`, `personality`, `biography`, `description`, `rumours`, `currently`, `preferences`, `time zone`; the only headings present are the character name and `Sheet`; no empty section wrapper, no placeholder |
| Pillar 1 — five distinct Save labels, no `aria-label` divergence | **Confirmed rendered.** All five visible labels distinct in the accessibility tree; the accessible name equals the visible text in each case |

### Claims the browser DISPROVED

| Claim | Measured |
|---|---|
| Pillar 3 NIT — `.status:empty` never fires, leaving a permanent 16px gap ×5 | **Disproven.** All five report `matches(':empty') === true`, `display: none`, `height: 0`. `:empty` ignores zero-length text nodes. Finding withdrawn above |

### Findings only the live pass could produce

- **PASS — brand rule 1 holds in both themes.** A full-document sweep of `color`, `background-color`,
  `border-*-color`, `outline-color` and `fill` on every element found **zero** uses of amber `#ffb300`
  on any surface, in Default Light or Default Dark. Amber remains cursor-only as
  `.claude/rules/branding.md` requires.
- **PASS — the accent swaps correctly by theme.** `--color-primary` resolves to `#1565c0`
  (`--brand-cyan-deep`) in Default Light and `#3dd6f7` (`--brand-cyan-bright`) in Default Dark;
  `--color-background` resolves to `#0b0c0e` (`--brand-ink`) in dark. These match the branding table
  exactly. The theme arrives as inline custom properties on `.app-root`, which is the architecture
  `web/CLAUDE.md` documents — no `var()`-inside-`@theme` build-time trap was hit.
- **PASS — dark-mode contrast is comfortable.** Headings and links 17.83:1, secondary text 8.54:1
  against the page ground — both above WCAG AAA (7:1).
- **WARNING — the documented 14px chrome base is applied nowhere.** `web/CLAUDE.md` specifies app
  chrome at a 14px base with 12px labels. Measured, the cascade is **16px at every level** —
  `html`, `body`, `.app-root`, `main`, `.page`, `.column` — with components self-sizing on top
  (`h1` 20px, `h2` 12px, `button` 13px, `a` 13px). Nothing inherits the documented base, so the
  13px control text in Pillar 4's finding is not a one-off drift but the absence of a base scale.
  Either apply the 14px base or amend `web/CLAUDE.md` to describe what actually ships.
- **WARNING — a signed-in player sees signed-out chrome on `/c/[id]`.** Viewing a public profile
  while authenticated as `uatpilot`, the TopBar renders `Login` / `Register` and the username is
  absent from the page entirely (`/uatpilot/.test(body.innerText) === false`). This is a structural
  consequence of D-85 placing `/c/[id]` outside `(authed)`: the layout that loads session state never
  runs, so the chrome cannot know. **This is safe to fix and does not touch T-05-02-03**, whose
  requirement is that the chrome not vary with *profile content* — varying with the viewer's own auth
  state discloses nothing about the profile being viewed. But the fix is not cosmetic: the public
  route must learn session state without re-entering `(authed)`.
- **WARNING — `ByteCounter`'s over-cap transition is never announced.** The over state is carried by
  colour (`--color-destructive` → `#fc7f7f`, measured 8.48:1, correct token, no hardcoded red) plus
  the numerals themselves, so WCAG 1.4.1 is arguably satisfied — a screen reader reading `101 / 100`
  gets the state. The real gap is that the element carries no `aria-live` and no `role`: the counter
  *materialises* at 80% of cap and *changes state* at 100%, and neither event is announced, so a
  screen-reader user typing into the field hears nothing at either boundary. Suggest
  `aria-live="polite"` on the counter.
- **NIT — uneven card weight in the roster grid.** Grid cells are equal-height, but a default
  character's card carries one action row (`Edit profile →`) against a non-default card's two
  (`Make default`, `Edit profile →`), so the default card renders with trailing whitespace. Visible
  in both themes at 1280px.
- **NOTE — the deferred named slots dominate a sparse profile.** `DIRECT MESSAGES — Not available yet.`
  and `SHEET — No sheet system yet.` are correct under the named-slot rule rather than the absence
  rule (they vary with nothing, so they are chrome, not viewer-varying attributes). Worth knowing
  anyway: on a profile with only identity fields set, two "not yet" panels occupy more vertical space
  above the fold than the character's actual content.
- **NOTE — `/login` handles the already-signed-in case explicitly**, offering `Continue` / `Sign out`
  rather than blind-redirecting. Not contracted by this phase; good behaviour worth keeping.

---

## Files Audited

- `web/src/routes/(authed)/characters/+page.svelte`
- `web/src/routes/(authed)/characters/new/+page.svelte`
- `web/src/routes/(authed)/characters/[id]/+page.svelte`
- `web/src/routes/c/[id]/+page.svelte`
- `web/src/lib/components/characters/{RosterCard,CharacterRoster,CreateCharacterForm,ProfileSection,ByteCounter,PublicProfile,ProfileMedia,CharacterPortrait}.svelte`
- `web/src/app.css` (token definitions)
- `.planning/phases/05-character-identity-ui-public-profiles/05-UI-SPEC.md` (baseline)
