# Sketch Manifest

## Design Direction

The HoloMUSH admin portal is a **gated console living inside the game client**,
not a separate product. It inherits the app's existing dark, terminal-adjacent
language — cyan (`#3dd6f7`) as the only accent, amber reserved exclusively for
the cursor, `#0b0c0e` ground, hairline `#1d2a33` borders — and reuses the
persistent `SectionRail` so an operator is never more than one click from the
game. Inside that frame it must feel *consequential*: actions here touch
characters the operator does not own, and are audited. The distinctive design
problem is that **six of its seven sections have no handler yet** and must read
as deliberate reserved capacity rather than an unfinished app.

## Reference Points

Grounded in-repo rather than borrowed from outside products:

- `web/src/lib/components/shell/SectionRail.svelte` — rail geometry, active-bar treatment, drawer variant
- `web/src/lib/nav/sections.ts` — the `as const satisfies` registry pattern the SPEC says to mirror (§10.1)
- `web/src/routes/(authed)/characters/+page.svelte` — the existing Card + Badge character idiom
- `.planning/phases/01-portal-spec/01-SPEC.md` §10 — the normative admin registry, descriptor, and gating contract

## Target Stack

SvelteKit 2.69 · Svelte 5.56 (runes) · Tailwind 4.3 · shadcn-svelte style `nova`
(baseColor `slate`) · `bits-ui` 2.18 · icons `@lucide/svelte`.
Sketches are plain HTML. `themes/default.css` carries **34 of `web/src/app.css`'s 39
`--color-*` tokens at byte-identical values** (verified 2026-08-01; the 5 absent ones are
unused by every sketch) — but it is **not** a verbatim mirror: it restructures `@theme` into
plain `:root` and drops `@layer base`, the density tokens and the reduced-motion keyframes.
Trust it for color; it is not an `app.css` substitute.

## Locked Decisions (intake, 2026-08-01)

| Decision | Choice |
| --- | --- |
| Shell relationship | Rail persists + dedicated admin nav column (three-column) |
| Planned sections | Navigable → honest empty state (round-trips the real gate) |
| Character list | Dense data table |
| Component budget | Open — add whatever makes it genuinely good; log adds per sketch |
| Row actions | Inline on hover (sketch 002, variant A). No multi-select, no bulk operations |
| Admin edit surface | Sheet, two groups: **Managed elsewhere first and collapsed**, then Editable here (sketch 004, variant C). `version` is header metadata, not a row — never editable, never actionable |
| Status control | Click → **transition** picker → confirm. Sends `AdminRetireCharacter` / `AdminUnretireCharacter`, **never a status value** (§10.6 keeps the lifecycle vocabulary off the wire). `idle` is shown but never selectable |
| Planned-section state | Minimal (sketch 003, variant A) — glyph, name, "Registered and gated. No handler yet." No trace, no scope preview |
| `/admin` visibility | **Invisible without permission** — no rail icon, no nav entry, and a deep link renders the **ordinary not-found page** (the same one any unknown path gets), never a redirect and never a bespoke "forbidden" page. `adapter-static` + `fallback: 'index.html'` means every route is already HTTP 200 + `index.html`, so indistinguishability is structural. Route guard is UX only; the ABAC gate on `admin_section:*` remains the boundary. **Requires a `+error.svelte`, which does not exist yet.** |
| Narrow-viewport collapse | Admin nav collapses **against the section rail**, merging its sections **into** the rail below a divider (sketch 001, variant C2) |
| Merged-collapse hierarchy | The Admin rail button becomes `is-context` (tint, no active bar) only once merged; identity + `⌘K` relocate to the rail foot |

## Sketches

| # | Name | Design Question | Winner | Tags |
|---|------|----------------|--------|------|
| 001 | admin-shell-frame | How does the three-column frame read, and how do available vs planned sections differentiate? | **C2 — Command Deck, merged collapse** | layout, nav, registry, responsive |
| 002 | admin-character-table | How should the dense admin list surface row actions and its non-data states? | **A — Inline actions** ⚠ needs 3 SPEC amendments | table, density, row-actions, empty-state, spec-amendment |
| 003 | planned-section-empty | What does "registered and gated, no handler yet" look like without reading as a dead end? | **A — Minimal** ⚠ raises SPEC defect D1 | empty-state, extensibility, abac, spec-defect |
| 004 | character-edit-destructive | How does a form that deliberately cannot edit name/status/version avoid reading as broken — and what is the destructive action, given there is no delete? | **C — Two groups** (refined) | forms, field-mask, destructive, audit, concurrency |
| 005 | admin-mutation-in-shell | Does the edit Sheet survive contact with the C2 shell at every band — and what is the mutation loop as a *sequence*? | **A — Overlay** | layout, forms, sheet, responsive, toast, consistency |
| 006 | phone-band-parity | What is `<768` on the surfaces 001 never covered, and does 005-A's right drawer hold up on a phone? | **B — Bottom-sheet** | responsive, mobile, drawer, sheet, consistency |
| 007 | public-profile-viewer-tiers | One profile, three viewer tiers, and the page may not say what is missing. How does the sparse view avoid reading as broken? | **C — Identity card** | profile, privacy, abac, viewer-tiers, phase-5 |
| 008 | character-roster-retired | How does a roster show characters that cannot be selected — and does the player roster stay Cards while admin went dense-table? | **B — Sectioned** | roster, lifecycle, status, cards, phase-3 |
| 009 | character-create-name-collision | What does the player see when §6.1's pipeline rewrites, or refuses, what they typed? | **A — Submit & report** | forms, validation, unicode, confusables, phase-2 |
| 010 | not-found-page | Four kinds of "nothing here" must be indistinguishable to one viewer. What is the ordinary not-found, and where does it point? | **B — + where you can go** | not-found, privacy, opacity, routing, phase-6 |

## Corrections made by sketches

| Correction | Raised by |
| --- | --- |
| **There is no delete in the admin portal.** §9.3's census has `AdminUpdateCharacter` / `AdminRetireCharacter` / `AdminUnretireCharacter` and **no** `AdminDeleteCharacter`; §4.4 and §10.6 both forbid wiring `world.Service.DeleteCharacter` to an admin button. The destructive action is **Retire, which is reversible**. Earlier hand-off notes calling 004 "the irreversible delete" were wrong. | 004 |
| **`characters` has no `last seen` column** and presence is current-only, so sketch 001's first draft fabricated one. Corrected to `version`. | 002 |

## Where these findings are routed

Sketch READMEs are not read by any phase workflow. `discuss-phase` only checks
whether `.planning/sketches/MANIFEST.md` **exists** and warns if the findings are
unpackaged — it does not load their content. So every finding above is also
routed somewhere a phase workflow or a human will actually encounter it:

| Route | Reaches | Carries |
| --- | --- | --- |
| `**Sketch findings**` lines on ROADMAP Phases **2, 3, 4, 6** | `discuss-phase` and `plan-phase` both read the phase's ROADMAP section | the phase-specific questions, verbatim |
| GitHub [#4904](https://github.com/holomush/holomush/issues/4904) | issue lists, `abac-reviewer` routing | defect D1 |
| GitHub [#4903](https://github.com/holomush/holomush/issues/4903) | issue lists | missing `+error.svelte` |
| `.claude/skills/sketch-findings-holomush/SKILL.md` | `discuss-phase:251`, `plan-phase:611,753` | the design decisions, as `<prior_decisions>` |

**Wrap-up run 2026-08-01** — all four sketches included in full and packaged into
`.claude/skills/sketch-findings-holomush/` (6 reference files + winning sources + theme).
See [WRAP-UP-SUMMARY.md](WRAP-UP-SUMMARY.md).

## ⚠ Open SPEC amendments raised by sketches

These are maintainer-directed decisions that exceed the SPEC as written. They
MUST be amended into `01-SPEC.md` before Phase 6 implements them.

| Id | Raised by | Amendment |
| --- | --- | --- |
| A1 | 002 | `characters.last_active_at` — new durable column (Phase 2 migration, epoch-ns `BIGINT`), written at **session start** (never on lease refresh), plus a §11.3 row permitting sort + filter. Cannot be derived: `sessions` rows are reaped and `session_connections.last_seen_at` is a gateway lease — both mean "online now". `never` must render as `never` and sort to the END in both directions. |
| A2 | 002 | Sorting the admin list by player. §11.3 forbids ordering `characters.player_id`; what the UI sorts is the joined `players.username`, which §11.3 never enumerates. Justified by §11.3's own test — the admin audience already sees usernames, so the ordering discloses nothing. Leave the `player_id` row as written; add a new one. |
| **D1** ([#4904](https://github.com/holomush/holomush/issues/4904)) | **003** | **SPEC DEFECT, not a widening.** §10.3 requires a planned-section refusal to "reveal nothing about which sections exist", but §10.4 defines two distinguishable denial codes (`DENY_ADMIN_SECTION` vs `DENY_ADMIN_SECTION_UNREGISTERED`). A non-admin probing an arbitrary id versus a real one enumerates the registry. §13's eight invariants pin none of this, though `INV-PRIVACY-9` does exactly this job for profiles. Fix: collapse the codes for unauthorized callers, **and** add an `INV-ACCESS-<n>` mirroring `INV-PRIVACY-9`. **Route to `abac-reviewer`** as a *spec-consistency defect with a latent disclosure channel*, **not** an active registry leak — the section ids are already public (in the SPEC, and shipped in the client bundle for nav). It still matters because §10.3 asserts a property the system lacks, and because the core-side registry may hold sections the client mirror does not. Hiding `/admin` does **not** mitigate it — it concentrates the denial path onto callers deliberately bypassing the UI. |
| A3 | 002 | `AdminSearchCharacters` (§9.2) currently "searches names" (character names). Extend to player usernames. |

## Round 2 (sketches 005–010) — findings

Two consistency sketches closed verified composition gaps; four frontier sketches
opened the player-facing half of the milestone, which round 1 never touched.

| Finding | Raised by |
| --- | --- |
| **"Names are permanent" is FALSE.** Both 008 and 009 shipped that copy. v0.13 ships player rename — IDENT-03 / `CharacterAccessService.RenameCharacter`, owner-scoped, Phase 3 (§9.4.2). Corrected in both to **"reserved"**, which is the property that holds (§4.4, §4.5). 009's winner A *depends* on rename existing: a surface that silently rewrites an irreversible identifier would need to show its work first. | 009 |
| **Under v0.13's seeded defaults, `guest` and `player` render identically.** Not one row in §8.6's seeded column is `player`, so the three-rung ladder collapses to two distinct renderings, and the `player` rung is unexercised by the default game. A three-way tier preview would show two identical panels. | 007 |
| **The page may not explain its own sparseness.** §7.5 + §8.9 make a blank field and a withheld field indistinguishable — so no counts, no lock icons, no greyed sections. A sign-in invitation is legal **only if unconditional**; the natural "show it only when something was withheld" improvement is a which-profiles-are-populated oracle. 007-C avoids the channel entirely by not shipping the notice. | 007 |
| **Two "status" vocabularies collide on the roster card.** The shipped badge is *session* state (`hasActiveSession` → `Active`/`Offline`); v0.13 adds *lifecycle* `characters.status`. They share the word **and the token `active`**. A retired card reads "Retired · Offline", where `Offline` is meaningless. **A non-`active` lifecycle MUST suppress the session badge.** | 008 |
| **The gallery can never contain an image in v0.13.** §7.3 ships the media model with zero upload behavior, so no media rows exist and the section never renders. Do **not** build empty "coming soon" slots — same speculative-scope mistake as 003's variant C. | 007 |
| **Do not hardcode the platform brand in player-facing copy.** "Back to HoloMUSH" violates branding INV-6 (the brand is the software, never the game world). The game's name exists as `SettingConfig.DisplayName` (`internal/plugin/manifest.go:211`, required on setting plugins) but is **not exposed to the web client by any RPC**. Copy is `Home`; the exposure gap is carried forward. | 010 |
| **Indistinguishability is per-viewer, not global.** An admin *should* see their own gated section resolve — that is the gate permitting them. Requiring global sameness would forbid ever showing an admin their own screen. | 010 |
| **A live availability check cannot be honest.** Even with §6.1.3's `UNIQUE` index, check-and-insert are different moments, so a green tick means "probably". 009-A and 009-C never make the promise. | 009 |
| **The `<768` band was decided once and never promoted.** 001 implemented it fully, 002 partially (no mobilebar), 003 and 004 not at all. 006 settles it across every admin surface. | 006 |
| **004 was the only sketch built standalone** — no shell, no rail, no container query. The edit Sheet had never rendered inside the frame it will live in. | 005 |

### Carried-forward gaps (new)

| Gap | Detail |
| --- | --- |
| **Game display name is server-side-only** | `SettingConfig.DisplayName` is required on setting plugins but reaches no web surface. Any player-facing game identity — title tag, OG card, welcome line, a "back" target — needs it exposed first. Not specific to one page; worth its own issue. |
| **`+error.svelte` boundary count** | 010-B rests on a **single root** `+error.svelte`. A second boundary (e.g. `routes/admin/+error.svelte`) silently destroys indistinguishability with no test failing. Phase 6 should ship a meta-test asserting exactly one. |
| **Signed-out web chrome is unspecified** | 007 invented a `Sign in` / `Play as guest` bar. No SPEC section describes logged-out chrome, and the app shell is `(authed)`-only. |
| **Profile URL key** | Name-based URLs are the point of a shareable profile, but names are not a key (no uniqueness constraint until Phase 2, and renameable after). Unsettled before Phase 5 routes anything. |
| **Confusable-message coupling** | 009's "too easily confused with an existing character" is safe *because* names are public at the `anonymous` floor. If a game raises that floor (§8.6 permits it), the message becomes a real oracle. |
| **B's grab handle is an obligation** | 006-B ships a bottom-sheet grabber, which promises drag-to-dismiss. Phase 6 must honor it or drop it. |
