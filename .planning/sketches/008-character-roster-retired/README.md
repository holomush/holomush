---
sketch: 008
name: character-roster-retired
question: "A roster must show characters the player cannot select. Does sketch 004's `idle` idiom (shown, never offered, with a reason) generalize player-facing — and does the roster stay Cards while admin went dense-table?"
winner: "B"
tags: [roster, lifecycle, status, cards, empty-state, phase-3, frontier]
---

# Sketch 008: Character Roster with Retired

## Design Question

The roster already exists — `web/src/routes/(authed)/characters/+page.svelte`, a
Card grid with an initial-letter avatar, `Last played`, an optional `At:
<location>` and a status badge. v0.13 adds `characters.status`
(`active | retired | idle`), and with it a population the roster has never had:
**characters that are visible but cannot be selected.**

Sketch 004 already solved a version of this, for `idle` in the admin status
picker: **shown, never offered, with a stated reason.** The question is whether
that idiom generalizes from a menu item to a whole card in a player-facing grid
— and, separately, whether the player roster should stay Cards now that admin
(sketch 002) went to a dense table.

## How to View

```
open .planning/sketches/008-character-roster-retired/index.html
```

Two pickers: **roster** (mixed / all-retired / single / zero) and **session
badge** (shown / dropped). The second one is the interesting one — see the
finding below.

## Variants

- **A: One grid, in place** — retired and idle cards stay in the grid in registry
  order, dimmed and un-clickable, carrying a `Retired` badge, a one-line reason,
  and a `View profile →` link. The literal generalization of 004's idiom.
- **B: Sectioned ★ WINNER** — `Playable` grid first (with the create card), then a
  `Not playable` section below with a disclosure count. Every card in the top
  grid is uniformly clickable; the second section is a different contract.
- **C: Dense list** — abandons Cards for a compact table with a `Status` column
  and a per-row `Play` / `View profile` action, converging on the idiom sketch
  002 chose for admin.

## ⚠ Finding — two different "status" vocabularies collide on one card

This is the sketch's main result and it is not a matter of taste.

The roster **already ships** a status badge, and it is a **session** status:
`hasActiveSession` → `Active`, else `sessionStatus || 'Offline'`
(`+page.svelte:132-136`). v0.13 adds `characters.status`, which is a **lifecycle**
status: `active | retired | idle`.

They share the word "status" and even share the token **`active`**, while meaning
entirely different things:

| | Vocabulary | Values | Means | Changes |
| --- | --- | --- | --- | --- |
| existing | session | `Active` / `Offline` | is this character connected right now | minute to minute |
| new (Phase 2) | lifecycle | `active` / `retired` / `idle` | may this character be played at all | rarely, deliberately |

Flip **session badge: shown** with any retired character visible. The card reads
**"Retired · Offline"** — and `Offline` is *meaningless* on a character who cannot
be played at all. It is not extra information; it is a second status competing
with the first, and the two are only distinguishable by knowing which vocabulary
each token belongs to.

**Recommendation: a non-`active` lifecycle suppresses the session badge
entirely.** Session state is only meaningful for a character that could be
connected. All three variants implement the collision rather than hiding it, so
the `session badge: dropped` setting shows what the screen looks like once the
rule is applied — compare the two directly before accepting the recommendation.

This is worth a Phase-3 note in its own right: **"status" is now an ambiguous word
in this codebase's UI vocabulary**, exactly the way `.claude/rules/terminology.md`
exists to prevent. The lifecycle values should probably never be rendered as the
bare word "Active" in player-facing UI, precisely because the session badge
already owns that word on the same card.

## Decision (2026-08-01)

**B wins.** The roster splits into `Playable` (with the create card) and
`Not playable`. The decisive property is that **every card in the top grid is
uniformly clickable** — there is no mixed-affordance grid where the player has to
read each card to learn whether it responds. A's dimmed-in-place cards put that
burden on every visit.

B also degrades best: in the all-retired state the `Playable` section says in
**words** that nothing can be played right now, where A leaves a grid of dead
cards and C leaves a table of dimmed rows.

**C is rejected — the player roster stays Cards.** Convergence with admin's dense
table was a real benefit, but this is the player's own roster, not an operator
tool, and it is the idiom that ships today. The portal deliberately carries two
idioms: **dense table for operator surfaces, Cards for the player's own things.**

**The session-badge finding is orthogonal and still stands** — it applies to B as
much as to A or C: **a non-`active` lifecycle suppresses the session badge.**
Adopting B does not resolve it, and Phase 3 must apply it explicitly.

### Two things B leaves for Phase 3

1. **Is the `Not playable` section collapsed by default?** This sketch always
   renders it expanded and treats the `N hidden` chip as decoration. These are the
   player's **own** characters, so the sketch's inclination is default-**expanded**
   with the chip as a collapse control — but that is a decision, not an inherited
   default, and the chip's current label ("hidden") presumes the opposite.
2. **The section labels.** `Playable` / `Not playable` deliberately avoid the word
   **status** and avoid reusing `Active`, because of the vocabulary collision
   above. Phase 3 should keep that constraint even if it rewords the labels.

## What to Look For

- **`roster: all retired`** is the state that separates the variants hardest. In
  A the entire grid is un-clickable and the create card is the only live thing on
  screen. In B the `Playable` section is empty and says so in words. In C every
  row is dimmed with a `View profile` action. Which of the three still tells you
  what to do?
- **Does A read as "these are yours, some are closed" or as "this page is
  broken"?** That is 004's idiom under its hardest load. In a menu, one dimmed
  item among three reads as deliberate; in a grid, three dimmed cards out of six
  is a much larger share of the screen.
- **B's disclosure count (`2 hidden`).** Retired characters are *the player's
  own* — hiding them behind a disclosure is very different from hiding someone
  else's. Does collapsing them respect the screen, or does it hide something the
  player has an ongoing relationship with?
- **C against the existing app.** The dense list is more information-efficient and
  matches admin — but the Card grid is what ships today, and this is the
  *player's* roster, not an operator tool. Convergence on one idiom is a real
  benefit; so is not making a player's own characters feel like rows in someone's
  database.
- **`View profile →` on a locked card.** Memory records that a retired character's
  profile still resolves. Making that reachable from the roster is the affordance
  that keeps "retired" from reading as "deleted" — check it lands that way.
- **The create card's subtitle** originally read *"Names are permanent once
  taken."* **Sketch 009 tested that claim and it was false** — v0.13 ships player
  rename (IDENT-03, `CharacterAccessService.RenameCharacter`, owner-scoped,
  Phase 3). Corrected here to *"Names are reserved once taken,"* which is the
  property that actually holds (§4.4, §4.5). Recorded rather than silently fixed,
  because it is a worked example of the fabricated-copy anti-pattern: plausible
  UI text that no source supports, caught only because a later sketch went
  looking.

## Grounding

| Element | Source |
| --- | --- |
| Card grid, `minmax(200px,1fr)`, initial-letter avatar, `Last played`, optional `At:`, dashed create card | `web/src/routes/(authed)/characters/+page.svelte:116-153` |
| The existing badge is **session** state (`hasActiveSession` / `sessionStatus`) | same file, `:132-136` |
| `characters.status` = `active` / `retired` / `idle`, all three shipped; `idle` unreachable in v0.13 | Phase-1 CONTEXT decision, SPEC §4.3 |
| Retire is reversible; **the name stays reserved** | SPEC §4.4, §4.5 |
| Retired roster visible-unselectable, profile still resolves | Phase-1 CONTEXT |
| "Shown, never offered, with a reason" for `idle` | sketch 004 winner C |
| Dense-table idiom for admin | sketch 002 winner A |

## Components this implies adding

Nothing beyond the running list. Uses `card` and `badge` (both installed);
variant C would use `table` (already on 001's list).

## Open questions this sketch surfaces

1. **Can a player retire their own character?** This sketch shows retired
   characters but offers **no retire action** — every retire path in the sketches
   so far is `AdminRetireCharacter`, an operator action. §9.3's admin census does
   not imply a player-facing equivalent. If players cannot self-retire, then in
   v0.13 a character only becomes retired by operator action, and the roster's
   copy should not imply otherwise.
2. **Does `idle` ever appear here?** It is unreachable in v0.13 (nothing sets it),
   so a real v0.13 roster shows only `active` and `retired`. The sketch renders an
   idle card to prove the exhaustive switch has a rendering — the same reason
   sketch 001 put an idle row in the admin table — but Phase 3 should not build
   copy that assumes players will see it.
3. **Ordering.** The sketch keeps registry order. Should retired characters sort
   last regardless of `Last played`? A retired character with a recent
   `Last played` currently floats to the top of a chronological sort, which is
   probably wrong — but sorting is not specified for this surface at all.
