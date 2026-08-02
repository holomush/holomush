---
name: sketch-findings-holomush
description: Validated design decisions, CSS patterns, and visual direction from HoloMUSH sketch experiments — the v0.13 web portal, both halves. Admin side (shell/nav, dense tables, gated-and-planned sections, field-masked edit forms, destructive actions, the mutation loop, the phone band) and player side (public profiles under viewer tiers, character roster with retired characters, character creation and the name pipeline, the ordinary not-found page). Use when implementing or planning any v0.13 web UI, when choosing layout/breakpoint/nav treatment, or before drawing an admin table, a profile, a roster, an empty state, a not-found page, or an edit or create form.
---

<context>
## Project: holomush

**The admin half.** The HoloMUSH admin portal is a **gated console living inside the game
client**, not a separate product. It inherits the app's existing dark, terminal-adjacent
language — cyan (`#3dd6f7`) as the only accent, amber reserved exclusively for the cursor,
`#0b0c0e` ground, hairline `#1d2a33` borders — and reuses the persistent `SectionRail` so an
operator is never more than one click from the game. Inside that frame it must feel
**consequential**: actions here touch characters the operator does not own, and are audited.
Its distinctive design problem is that **six of its seven sections have no handler yet** and
must read as deliberate reserved capacity rather than an unfinished app.

**The player half.** Public profiles, the character roster, and character creation are the
player's **own** surfaces. Their distinctive design problem is the inverse: the profile is
**structurally forbidden from explaining why it is short** (§7.5/§8.9 make a blank field and
a withheld field indistinguishable), so sparseness has to read as intentional without any
copy saying so.

Reference points are **grounded in-repo**, not borrowed from outside products:
`shell/SectionRail.svelte`, `nav/sections.ts` (the `as const satisfies` registry pattern the
SPEC says to mirror), `(authed)/characters/+page.svelte`, `web/svelte.config.js`, and
`.planning/phases/01-portal-spec/01-SPEC.md`.

Sketch sessions wrapped: **2026-08-01** — round 1 (001–004, admin) and round 2 (005–010, two
consistency sketches + four frontier sketches opening the player-facing half). **All ten
included in full.**
Milestone: **v0.13 — Web Portal: Identity & Admin Foundations**.
Target build phases: **2** (name pipeline), **3** (roster, rename), **5** (profiles),
**6** (admin portal, not-found).
</context>

<design_direction>
## Overall Direction

- **Palette.** One accent: cyan `#3dd6f7`. Amber `#ffb300` is the **cursor only** —
  `.claude/rules/branding.md` INV-1. Ground `#0b0c0e`, surfaces `#101418`, hairline borders
  `#1d2a33`. Tints via `color-mix`, never hardcoded.
- **Layout.** Three columns: 48px section rail (persists) + 232px admin nav + content.
  Breakpoints are **container queries** (`@container vp`), not media queries.
- **Collapse.** At 768–1023px the admin nav merges **into** the rail below a divider; below
  768px both go to zero and a `.mobilebar` + drawer takes over. The drawer holds rail **and**
  admin sections under two group labels.
- **Two idioms, deliberately.** Dense data table for **operator** surfaces; Card grid for
  **the player's own things**. Convergence was weighed and rejected.
- **One geometry, one override.** The edit Sheet is a 380px right drawer everywhere, plus
  exactly one `@container vp (max-width: 767px)` block flipping it to `side="bottom"`. It
  stays an **overlay, never a route**.
- **Nav is derived from the core-authoritative registry**, filtered by permission — never a
  template `{#if}`.
- **Restraint over narration — except on audited writes.** Sketches 001, 003 and 007 each
  rejected implementation detail in the user's face. The exceptions are principled: the edit
  form's excluded fields (a missing field is actively confusing) and the mutation toast
  (naming the wire call is *accountability* when an admin acts on a character they do not own
  and it is audited under their player id).
- **Absence is designed, not defaulted.** Planned sections, denied sections, empty results,
  zero rows, never-active characters, withheld profile fields and four kinds of not-found
  each have a deliberate, distinct treatment — and several are required to be
  *indistinguishable from each other*.
- **Destructive means reversible.** There is no delete in this portal; the destructive
  action is Retire, and the name stays **reserved**.
</design_direction>

<findings_index>
## Design Areas

| Area | Reference | Key Decision |
|------|-----------|--------------|
| Shell & Navigation | `references/shell-and-navigation.md` | Three-column frame; container queries; admin nav **merges into the rail** at 768–1023px with `.rail-btn.is-context` scoped inside that query; the `<768px` band settled across **every** admin surface |
| Data Tables & List States | `references/data-tables.md` | Dense table, **inline hover row actions**, no multi-select/bulk; click-header sort only; four distinct non-data states |
| Gating & Absence | `references/gating-and-absence.md` | Minimal `Registered and gated. No handler yet.`; `/admin` invisible without permission; **the ordinary not-found** with a single root `+error.svelte`; indistinguishability is **per-viewer, not global** |
| Forms & Destructive Actions | `references/forms-and-destructive-actions.md` | Two groups — `Managed elsewhere` (first, collapsed) then `Editable here`; `version` is header metadata; status is a **transition picker that never sends a status value**; Sheet = overlay + one phone override; the mutation loop as a sequence |
| Profile & Viewer Tiers | `references/profile-and-viewer-tiers.md` | **Identity card** — the sparse view is the *native* form, not a truncated one. No counts, no locks, no conditional invitation. `guest` and `player` render **identically** under seeded defaults |
| Player Roster & Creation | `references/player-roster-and-creation.md` | Roster splits `Playable` / `Not playable`, stays **Cards**; a non-`active` lifecycle **suppresses the session badge**; creation is **submit-and-report** — never promise availability |
| Foundations | `references/foundations.md` | Palette tokens + INV-1 + **INV-6**; the two idioms; the components to install; 15px phone inputs; what the sketch theme actually is |
| Anti-patterns | `references/anti-patterns.md` | The **17** mistakes the sketches actually made or the SPEC warns are reflexive — **read this before drawing anything** |

## Hard constraints (violating these is a bug, not a taste call)

1. **Amber is the cursor only.** Never an accent, link, button, badge, or status color (INV-1).
2. **Never hardcode `HoloMUSH` in player-facing copy.** The brand is the platform, never the
   game world (INV-6). The game's own name is **server-side-only** — see gaps below.
3. **There is no `AdminDeleteCharacter`.** Wiring `world.Service.DeleteCharacter` to an
   admin button is forbidden by §4.4 *and* §10.6. Admin disable is **Retire**, reversible.
4. **Never send a `status` value.** Send `AdminRetireCharacter` / `AdminUnretireCharacter`.
   A maskable `status` path puts the unreachable `idle` back on the wire (§10.6).
5. **Names are *reserved*, not permanent.** v0.13 ships rename (IDENT-03,
   `RenameCharacter`, owner-scoped, Phase 3). Any copy saying "permanent" is false.
6. **A profile may not explain its own sparseness.** No counts, no lock icons, no greyed
   sections, no copy that varies with how much was withheld (§7.5, §8.9). A sign-in
   invitation is legal **only if unconditional** — and 007 ships none at all.
7. **A non-`active` lifecycle suppresses the session badge.** `Retired · Offline` is
   meaningless; two vocabularies share the token `active`.
8. **Deep-link denial renders the *ordinary* not-found.** A redirect, a bespoke `/admin`
   not-found, or a "you don't have permission" page destroys the indistinguishability three
   surfaces rest on. **One root `+error.svelte`, asserted by a meta-test.**
9. **Indistinguishability is per-viewer, not global.** An admin seeing their own gated
   section resolve is the gate working, not a leak.
10. **UX invisibility is never the boundary.** The ABAC gate on `admin_section:*` is; every
    admin RPC must still deny independently (§10.4).
11. **Tier clearing is set membership, never ordinal compare** (§8.2.1). The DSL's
    `compareStrings` is Go byte order — an ordinal ladder holds only by alphabetical accident.
12. **No sort dropdown, no facet panel** — §11.3 names these as the specific warning sign.
13. **`characters` has no last-seen column.** Do not draw one (A1 below).

## ⚠ Carried-forward blockers and gaps

**Unsanctioned SPEC amendments** — must land in `01-SPEC.md` before Phase 6 builds them:

| Id | What |
|---|---|
| **A1** | `characters.last_active_at` — new durable column + write path at **session start** (never lease refresh) + a §11.3 row |
| **A2** | Sorting the admin list by `players.username` (add a new §11.3 row; leave the `player_id` row as written) |
| **A3** | `AdminSearchCharacters` extended to player usernames |

**Defects and gaps:**

| Id | What | Status |
|---|---|---|
| **D1** | §10.3 vs §10.4 — distinguishable denial codes are a registry-enumeration oracle | **SPEC defect**, [#4904](https://github.com/holomush/holomush/issues/4904) — route to `abac-reviewer` |
| — | No `+error.svelte` exists anywhere under `web/src/routes/` | **Gap**, [#4903](https://github.com/holomush/holomush/issues/4903) — Phase 6 must build it, and assert **exactly one** |
| — | **The game's display name is server-side-only.** `SettingConfig.DisplayName` (`internal/plugin/manifest.go:211`) reaches **no** web surface | **Gap** — blocks any player-facing game identity (title tag, OG card, welcome line, "back" target). Worth its own issue |
| — | **Signed-out web chrome is unspecified.** 007 invented a `Sign in` / `Play as guest` bar; no SPEC section describes logged-out chrome and the app shell is `(authed)`-only | **Open** — real Phase 5 gap |
| — | **Profile URL key.** Name-based URLs are the point of a shareable profile, but names are not a key (no uniqueness until Phase 2; renameable after) | **Open** — settle before Phase 5 routes anything |
| — | Does the admin portal expose rename? **Player** rename ships (IDENT-03, Phase 3); §9.3's admin census still has **no `AdminRenameCharacter`** | **Narrowed, still open** — settle before Phase 6 builds the edit form |
| — | **The bottom-sheet grabber is an obligation.** A grab handle promises drag-to-dismiss; 006-B ships the handle and not the gesture | **Open** — Phase 6 must honor it or drop it |
| — | **Confusable-message coupling.** "too easily confused with an existing character" is safe *because* names are public at the `anonymous` floor; §8.6 permits raising it | **Open** — Phase 2 should record the coupling |
| — | `SECTIONS` in `nav/sections.ts` has no `status` concept; the admin registry needs it as a **required** field | **Open** — Phase 6 planning decision |
| — | Does the Sheet close before or after the toast? Is `Not playable` collapsed by default? Do dropped phone columns need a home? | **Open** — small, listed in the relevant reference files |

## Theme

The sketch theme is at `sources/themes/default.css`. It carries **34 of `web/src/app.css`'s
39 color tokens at byte-identical values** — trustworthy for color. It is **not** a verbatim
mirror despite its own header comment: it drops `@layer base`, density tokens and
reduced-motion keyframes, and adds sketch-only scaffolding. See `references/foundations.md`.

## Source Files

Original sketch HTML is preserved in `sources/` for complete reference — each is a
self-contained page with live variant tabs, viewport buttons (375 / 768 / 1280) that
genuinely exercise the container-query breakpoints, and state pickers:

```
open .claude/skills/sketch-findings-holomush/sources/001-admin-shell-frame/index.html
```

Three carry interactive devices worth driving rather than reading about:

- **005** — a **ten-step mutation sequence** across two paths (edit, then retire). Drive it
  at 1280, then re-drive at 768 and 375; the answer changes by band.
- **007** — a **`⇄ compare tiers`** toggle rendering all three viewer tiers side by side.
  Under the seeded posture the middle and right panels are *the same page* — a finding that
  is visible rather than argued.
- **010** — opens in **compare mode** with all four not-found paths fingerprinted. Press
  **`☣ inject leak`** to watch the opacity check go red against the single most natural
  thing an implementer would write.
</findings_index>

<metadata>
## Processed Sketches

**Round 1 — the admin portal (2026-08-01):**

- 001-admin-shell-frame — winner **C2** (Command Deck, merged collapse)
- 002-admin-character-table — winner **A** (inline row actions)
- 003-planned-section-empty — winner **A** (minimal)
- 004-character-edit-destructive — winner **C** (two groups, refined)

**Round 2 — two consistency sketches + four frontier sketches (2026-08-01):**

- 005-admin-mutation-in-shell — winner **A** (overlay; the Sheet composed with the C2 shell)
- 006-phone-band-parity — winner **B** (bottom-sheet; the `<768px` band settled)
- 007-public-profile-viewer-tiers — winner **C** (identity card)
- 008-character-roster-retired — winner **B** (sectioned `Playable` / `Not playable`)
- 009-character-create-name-collision — winner **A** (submit & report)
- 010-not-found-page — winner **B** (+ where you can go)
</metadata>
