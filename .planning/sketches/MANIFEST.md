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
Sketches are plain HTML; `themes/default.css` mirrors `web/src/app.css` verbatim.

## Locked Decisions (intake, 2026-08-01)

| Decision | Choice |
| --- | --- |
| Shell relationship | Rail persists + dedicated admin nav column (three-column) |
| Planned sections | Navigable → honest empty state (round-trips the real gate) |
| Character list | Dense data table |
| Component budget | Open — add whatever makes it genuinely good; log adds per sketch |
| Narrow-viewport collapse | Admin nav collapses **against the section rail** (sketch 001 round 2) |

## Sketches

| # | Name | Design Question | Winner | Tags |
|---|------|----------------|--------|------|
| 001 | admin-shell-frame | How does the three-column frame read, and how do available vs planned sections differentiate? | **C — Command Deck** | layout, nav, registry, responsive |
| 002 | admin-character-table | What does a dense admin table look like in this dark theme, and how are row actions surfaced? | _not built_ | table, density |
| 003 | planned-section-empty | What does "registered and gated, no handler yet" look like without reading as a dead end? | _not built_ | empty-state, extensibility |
| 004 | character-edit-destructive | How do the field-mask edit surface and the irreversible delete read? | _not built_ | forms, destructive, audit |
