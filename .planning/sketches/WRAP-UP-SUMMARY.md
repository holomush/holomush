# Sketch Wrap-Up Summary

**Date:** 2026-08-01 (round 1) · 2026-08-01 (round 2, append-mode re-run)
**Sketches processed:** 10 (10 included, 0 excluded)
**Design areas:** Shell & Navigation · Data Tables & List States · Gating & Absence ·
Forms & Destructive Actions · **Profile & Viewer Tiers** · **Player Roster & Creation** ·
Foundations · Anti-patterns
**Skill output:** `.claude/skills/sketch-findings-holomush/`
**Milestone:** v0.13 — Web Portal: Identity & Admin Foundations
**Target build phases:** 2 (name pipeline) · 3 (roster, rename) · 5 (profiles) · 6 (admin, not-found)

## Included Sketches

### Round 1 — the admin portal

| # | Name | Winner | Design Area |
|---|------|--------|-------------|
| 001 | admin-shell-frame | **C2** — Command Deck, merged collapse | Shell & Navigation |
| 002 | admin-character-table | **A** — inline row actions | Data Tables & List States |
| 003 | planned-section-empty | **A** — minimal | Gating & Absence |
| 004 | character-edit-destructive | **C** — two groups (refined) | Forms & Destructive Actions |

### Round 2 — two consistency sketches, four frontier sketches

| # | Name | Kind | Winner | Design Area |
|---|------|------|--------|-------------|
| 005 | admin-mutation-in-shell | consistency | **A** — overlay | Shell & Navigation · Forms |
| 006 | phone-band-parity | consistency | **B** — bottom-sheet | Shell & Navigation · Forms |
| 007 | public-profile-viewer-tiers | frontier | **C** — identity card | Profile & Viewer Tiers |
| 008 | character-roster-retired | frontier | **B** — sectioned | Player Roster & Creation |
| 009 | character-create-name-collision | frontier | **A** — submit & report | Player Roster & Creation |
| 010 | not-found-page | frontier | **B** — + where you can go | Gating & Absence |

## Excluded Sketches

None. All ten were included in full at their curation checkpoints.

## Design Direction

**The admin half.** A **gated console living inside the game client**, not a separate
product. It inherits the app's dark, terminal-adjacent language — cyan `#3dd6f7` as the only
accent, amber `#ffb300` reserved exclusively for the cursor (INV-1), `#0b0c0e` ground,
hairline `#1d2a33` borders — and reuses the persistent `SectionRail`. Inside that frame it
must feel **consequential**: actions touch characters the operator does not own, and are
audited. Its distinctive problem is that **six of its seven sections have no handler yet**
and must read as deliberate reserved capacity rather than an unfinished app.

**The player half** (opened by round 2) has the inverse problem. The public profile is
**structurally forbidden from explaining why it is short** — §7.5/§8.9 make a blank field and
a withheld field indistinguishable — so sparseness must read as intentional with no copy
saying so.

**The thread across all ten: restraint over narration, with two principled exceptions.**
Sketches 001 (registry-contract footer), 003 (authorization trace, speculative scope) and
007 (the sign-in invitation) each rejected explanation. The exceptions are 004's excluded
form fields — a missing field is actively confusing in a way a missing trace is not, and
"incomplete" is what a well-meaning implementer *fixes* — and 005's mutation toast, where
naming the wire call is **accountability**, because an admin is acting on a character they
do not own and it is audited under their player id.

> **Narration is wrong on read surfaces and right on audited write surfaces.**

## Key Decisions

**Layout.** Three columns — 48px rail (persists) + 232px admin nav + content. Breakpoints via
**container queries**, not media queries. At 768–1023px the admin nav merges **into** the
rail below a divider; below 768px both collapse and a `.mobilebar` + drawer takes over, the
drawer holding rail **and** admin sections under two group labels (round 2 finally drew it).

**Merged-collapse hierarchy.** `.rail-btn.is-context`, scoped **inside** the
`max-width: 1023px` query, lets Admin keep its tint while surrendering the active bar.
Identity + `⌘K` relocate to `.rail-identity` at the collapsed breakpoint only.

**The phone band, settled once.** Round 1 decided `<768px` on **one** surface and every later
sketch half-inherited or ignored it. 006 settles it across all of them, and finds that
hover-only row actions have no meaning there (only `Edit` survives).

**Mutation surface.** One geometry: a 380px right-drawer Sheet at every band, plus **exactly
one `@container vp (max-width: 767px)` block** flipping it to `side="bottom"` — a prop at a
breakpoint, not a second component. The Sheet stays an **overlay, never a route** (which
keeps deep-linking, and #4903, out of the edit surface). The mutation loop as a sequence:
row updates **in place** with a flash, sheet closes before the toast, Undo sends
`AdminUnretireCharacter`.

**Two idioms, deliberately.** Dense data table for **operator** surfaces; Card grid for
**the player's own things**. 008's variant C converged the roster onto the admin table and
was rejected *for* that.

**Tables.** Dense, inline hover row actions, no multi-select and no bulk. Click-header sort
only — no sort dropdown, no facet panel (§11.3's named warning sign). Four distinct non-data
states.

**Gating and not-found.** `/admin` is invisible without permission, and a deep link renders
the **ordinary not-found**. `adapter-static` + `fallback: 'index.html'` makes
`/admin/moderation` and `/blahblah` identical at the HTTP layer **by construction**. The
page itself (010) is minimal head + a list of the viewer's **own** sections, on a **single
root `+error.svelte`** — nesting would kill the property silently. **Indistinguishability is
per-viewer, not global.**

**Profiles.** The identity card makes the sparse view the **native** form rather than a
truncated one. No counts, no locks, no greyed sections, and **no sign-in invitation at all**
— rejecting it closes a disclosure trap by construction.

**Roster and creation.** `Playable` / `Not playable` sections so every top-grid card is
uniformly clickable; a non-`active` lifecycle **suppresses the session badge**. Creation is
**submit-and-report** — it never promises availability it cannot keep.

**Destructive action.** There is no delete in this portal — it is **Retire, which is
reversible**, and the name stays **reserved**.

## Corrections the sketches produced

| Correction | Raised by |
| --- | --- |
| **There is no delete in the admin portal.** §9.3's census has update / retire / unretire and no `AdminDeleteCharacter`. Earlier hand-off notes calling 004 "the irreversible delete" were wrong. | 004 |
| **`characters` has no `last seen` column** — 001's first draft fabricated one. Corrected to `version`. | 002 |
| **The sketch theme is not a verbatim `app.css` mirror.** 34 of 39 color tokens byte-identical (colors trustworthy), but `@theme` restructured to `:root`, and `@layer base` / density tokens / reduced-motion keyframes dropped. | round-1 wrap-up |
| **"Names are permanent" is FALSE** — and it had already propagated into two sketches. v0.13 ships player rename (IDENT-03, `RenameCharacter`, owner-scoped, §9.4.2 line 1805, Phase 3). The true property is **reserved**. 009's winner *depends* on rename existing. | 009 |
| **Do not hardcode `HoloMUSH` in player-facing copy** (INV-6 — the brand is the platform, never the game world). "Back to HoloMUSH" → **`Home`**. | 010 |
| **`guest` and `player` render identically** under v0.13's seeded defaults — not one §8.6 row seeds `player`, so the three-rung ladder collapses to two renderings and the `player` rung is unexercised by the default game. | 007 |
| **The gallery can never contain an image in v0.13** (§7.3 ships zero upload behavior), so the section never renders. Do not build "coming soon" slots. | 007 |
| **Two "status" vocabularies collide** on the roster card — the shipped badge is *session* state, v0.13 adds *lifecycle* state, and they share the token `active`. | 008 |
| **A live availability check cannot be honest** — even with the `UNIQUE` index, check and insert are different moments. | 009 |
| **Indistinguishability is per-viewer, not global.** Requiring global sameness would forbid ever showing an admin their own screen. | 010 |

## Carried-forward blockers and gaps

| Id | What | Status |
|---|---|---|
| **A1** | `characters.last_active_at` — durable column + write at session start (never lease refresh) + a §11.3 row; `0` renders `never` and sorts to the END both ways | Unsanctioned SPEC amendment |
| **A2** | Sort by `players.username` — add a new §11.3 row; leave the `player_id` row as written | Unsanctioned SPEC amendment |
| **A3** | `AdminSearchCharacters` extended to player usernames | Unsanctioned SPEC amendment |
| **D1** | §10.3 vs §10.4 — distinguishable denial codes form a registry-enumeration oracle; no invariant pins it though `INV-PRIVACY-9` does the same job for profiles | SPEC defect — [#4904](https://github.com/holomush/holomush/issues/4904), route to `abac-reviewer` |
| — | No `+error.svelte` under `web/src/routes/` — **three** surfaces now depend on it (003, 007, and the rejected 006-C) | [#4903](https://github.com/holomush/holomush/issues/4903); Phase 6 must also assert **exactly one** |
| — | **Game display name is server-side-only.** `SettingConfig.DisplayName` (`internal/plugin/manifest.go:211`) is **required** yet reaches no web surface; the *optional* `landing.hero.metadata.title` content key renders instead, falling back to the platform brand | **Filed** — [#4905](https://github.com/holomush/holomush/issues/4905) |
| — | **Signed-out web chrome is unspecified** — 007 invented a `Sign in` / `Play as guest` bar; the app shell is `(authed)`-only | Open — Phase 5 |
| — | **Profile URL key** — name-based URLs are the point, but names are not a key (no uniqueness until Phase 2, renameable after) | Open — settle before Phase 5 routes |
| — | Admin rename: **player** rename ships (IDENT-03); §9.3's admin census still has no `AdminRenameCharacter` | Narrowed, still open — Phase 6 |
| — | **The bottom-sheet grabber is an obligation** — it promises drag-to-dismiss, which 006-B does not implement | Open — Phase 6 honors or drops it |
| — | **Confusable-message coupling** — the message is safe *because* names are public at the `anonymous` floor, which §8.6 permits raising | Open — Phase 2 should record it |
| — | `SECTIONS` has no `status` concept; the admin registry needs it as a required field | Open — Phase 6 planning decision |
| — | Sheet-before-or-after-toast · `Not playable` collapsed by default? · dropped phone columns · name length cap · block-list wording · guest retry loop after the unique index | Open — small; listed in the relevant reference files |

## Components to install

Ten shadcn-svelte components the sketches exercise are not yet in
`web/src/lib/components/ui/`: `table`, `pagination`, `empty`, `alert`, `avatar`,
`breadcrumb`, `skeleton`, `select`, `field`, `sonner` — plus `alert-dialog` for the retire
confirmation. **Round 2 added nothing to this list**; it exercises `sheet` in its
`side="bottom"` configuration, which is natively supported. Style `nova`, baseColor `slate`,
per `web/components.json`.

## Routing

Sketch READMEs are not read by any phase workflow. These findings reach planning via:

| Route | Reaches | Carries |
| --- | --- | --- |
| `.claude/skills/sketch-findings-holomush/` | `discuss-phase:251`, `plan-phase:611,753` as `<prior_decisions>` | **all ten sketches'** design decisions |
| `**Sketch findings**` lines on ROADMAP Phases 2, 3, 4, 5, 6 | `discuss-phase` + `plan-phase` | the phase-specific questions, verbatim |
| GitHub #4904 | issue lists, `abac-reviewer` routing | defect D1 |
| GitHub #4903 | issue lists | the missing `+error.svelte` |

## Wrap-up history

| Run | Date | Scope | Skill state after |
| --- | --- | --- | --- |
| 1 | 2026-08-01 | 001–004 | 6 reference files, 4 sources + theme |
| 2 (append) | 2026-08-01 | 005–010 | **8 reference files, 10 sources + theme**; anti-patterns grew from 9 entries to 17 |
