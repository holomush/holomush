---
sketch: 006
name: phone-band-parity
question: "What is the sub-768px band on the surfaces sketch 001 never covered — the planned-section empty state and the edit Sheet — and does 005's winning right-drawer hold up on a phone?"
winner: "B"
tags: [responsive, mobile, drawer, sheet, empty-state, phase-6, consistency]
---

# Sketch 006: Phone Band Parity

## Design Question

A **consistency sketch**, driven by a verified inconsistency: the `<768px` band
degrades across the existing sketches rather than being decided once.

| Sketch | `@container vp (max-width: 767px)` | `.mobilebar` |
| --- | --- | --- |
| 001 admin-shell-frame | **full** — rail *and* adminnav zeroed, table columns dropped | **yes** |
| 002 admin-character-table | **partial** — zeroes only `.rail` | **no** |
| 003 planned-section-empty | **absent entirely** | **no** |
| 004 character-edit-destructive | **absent** (no shell at all) | **no** |

So the phone band was decided **once, on one surface** — the table in 001 — and
every surface since either half-inherited it or ignored it. Two things follow
that nobody has looked at:

1. **The drawer that 001 called for was never drawn.** 001's README says the
   phone drawer "should hold rail sections **and** admin sections together", but
   no sketch renders it. That is a real design question, not a mechanical port:
   at 768–1023 the merged rail gets its hierarchy from `.rail-btn.is-context`,
   and a flat drawer list has no equivalent.
2. **005's winner (A, the 380px right drawer) is untested on a phone.** In a
   390px viewport a 380px right drawer leaves a ~10px sliver. 005 rendered that
   deliberately without correcting it, and handed the question here.

## How to View

```
open .planning/sketches/006-phone-band-parity/index.html
```

**This sketch is phone-first — it opens pinned to 390px and the `375` button is
pre-selected.** Above 768 it deliberately says nothing and tells you so, because
001 and 005 already decided that territory.

The **screen** dropdown walks five screens: character table, nav drawer open,
planned section, edit sheet, and sheet + saved toast. The hamburger and the row
`Edit` buttons are wired, so you can also just drive it.

## Variants

Variants apply to **the Sheet only** — the mobilebar, the drawer, the table and
the empty state are identical in all three, because those are not in question.

- **A: Right drawer (005-A as-is)** — 005's winner carried over with **zero**
  changes. A 380px panel in a 390px viewport. Included as the honest control: if
  A is fine here, 005-A ships everywhere with no override at all.
- **B: Bottom-sheet ★ WINNER** — 84% height, rounded top, grab handle. The
  platform convention, and the specific fallback 005's variant C proposed.
- **C: Full-screen takeover** — the sheet becomes its own screen with a back
  arrow to `‹ Characters` instead of an `×`. Most native-app-like; costs a
  navigation model (the sheet is now a route, not an overlay).

## What to Look For

- **A at 390px is the whole question.** Does the ~10px sliver of table read as a
  deliberate "there is something behind this", or as a layout bug? If it reads as
  a bug, 005-A needs exactly one `@container vp (max-width: 767px)` override and
  stays A everywhere else — which is a much smaller change than adopting 005-C
  wholesale would have been.
- **B's grab handle sets an expectation this sketch does not honor.** A
  bottom-sheet with a grabber implies drag-to-dismiss and usually a
  partial-height detent. Neither is implemented here. Does the affordance still
  help, or is it a promise the Phase-6 implementation would then have to keep?
- **C's back arrow changes the mental model.** `‹ Characters` says "you navigated";
  `×` says "you opened something over the list". C is the only variant where
  hardware/browser Back would be expected to close the sheet. That is a routing
  decision as much as a visual one — and note it interacts with #4903: if the
  sheet is a route, it can be deep-linked, and a deep link to a character an
  admin may not see has to render the ordinary not-found like anything else.
- **The drawer's two group labels.** `HoloMUSH` / `Admin` split by a divider is
  doing the hierarchy work that `.rail-btn.is-context` does at 768–1023. Is a
  label pair enough, or does the merged list need the same tint-not-bar device?
- **Row actions cannot hover on a phone.** 002-A's inline-on-hover actions are
  permanently visible here, and only `Edit` survives — Retire and `⋯` are gone.
  **This is a real divergence from 002's winner, not an oversight.** Is one
  always-visible action per row right, or should the row open a sheet that
  carries all three?
- **The planned-section empty state** (003-A) had no phone handling at all and
  mostly survives narrowing for free — a centred, vertically-anchored block is
  naturally responsive. Confirm that rather than assuming it.

## Grounding

| Element | Source |
| --- | --- |
| Rail + adminnav both zeroed below 768; `.mobilebar` with hamburger + `Admin › …`; columns 4/5 dropped | sketch 001 winner C2 |
| "The phone drawer should hold rail sections **and** admin sections together" | sketch 001 README, round 2 |
| 380px right-drawer Sheet geometry | sketch 005 winner A |
| `Registered and gated. No handler yet.` | sketch 003 winner A |
| Two-group sheet body, `update_mask` footer | sketch 004 winner C |
| Toast copy naming the RPC | sketch 005 |

**Input font-size is 15px, not 12.5px as on desktop.** Any `<16px` font in a
focused input triggers iOS Safari's zoom-on-focus, which then leaves the viewport
scaled after blur. This is a genuine mobile constraint, not a style preference —
Phase 6 should keep it (or go to 16px) rather than inheriting the desktop size.

## Components this implies adding

Nothing new. Exercises `sheet` (installed) in its `side="bottom"` configuration
for variant B, which shadcn-svelte's `Sheet` supports natively — so B is no more
work than A.

## Decision (2026-08-01)

**B wins**, and the combined 005+006 result is the cheap outcome:

> **005-A everywhere, plus exactly one `@container vp (max-width: 767px)` block
> that turns the right drawer into a bottom-sheet.** No routing change, no
> three-treatment branching, no per-band component swap. shadcn-svelte's `Sheet`
> supports `side="bottom"` natively, so this is a prop at a breakpoint rather
> than a second component.

Picking A at 005 and paying only where it broke cost strictly less than adopting
005-C's three-treatment design up front would have — and 006 is what turned "A
might be wrong on a phone" into a bounded, one-block fix instead of an assumption.

**C is rejected, and its rejection settles a routing question early.** A back
arrow would have made the sheet a *route*, which makes it deep-linkable, which
drags #4903 into the edit surface: a deep link to a character the viewer may not
see would have to render the ordinary not-found like any other unreachable path.
B keeps the sheet an **overlay**, so that whole branch stays closed. Record this
as a deliberate scope reduction, not an oversight.

### The grabber is now an obligation, not decoration

B ships a grab handle, and a grab handle **promises drag-to-dismiss** (and
usually a partial-height detent). Neither is implemented in this sketch. Phase 6
must either honor the affordance or drop it — a handle that does not drag is a
worse affordance than no handle at all, because it invites a gesture that then
fails silently. Flagged here so it is a decision rather than an inherited
accident.

## Open questions this sketch surfaces

1. **Is the sheet a route or an overlay?** Variant C forces the question; A and B
   dodge it. It is a Phase-6 routing decision with a deep-link consequence
   (see #4903), not purely visual.
2. **Do the dropped columns need a home?** `Created` and `Ver` vanish below 768.
   `Ver` in particular is load-bearing for the concurrency contract — on a phone
   a stale-version conflict would arrive with no prior sight of the version at
   all. The sheet header still carries it, which may be sufficient.
