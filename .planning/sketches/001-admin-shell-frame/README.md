---
sketch: 001
name: admin-shell-frame
question: "How does the three-column admin frame read, and how do `available` vs `planned` sections differentiate in nav?"
winner: "C2"
tags: [layout, nav, admin, phase-6, registry, responsive]
---

# Sketch 001: Admin Shell Frame

## Design Question

The admin portal is a **trust boundary** whose nav is *derived from a core-side
registry* (SPEC §10.1). Three things have to be true at once and they pull
against each other:

1. It must feel like part of HoloMUSH, not a bolted-on Django admin.
2. It must feel *consequential* — you are acting on characters you don't own.
3. Six of its seven sections have no handler, and that has to read as
   **deliberate reserved capacity**, not as an unfinished app.

## How to View

```
open .planning/sketches/001-admin-shell-frame/index.html
```

## Variants

- **A: Registry Ledger** — the nav *is* the registry. Sections grouped under
  `Available` / `Planned · gated, no handler`, status dots, and a footer stating
  `7 sections · 1 available · registry: core-authoritative`. Infrastructure-honest.
- **B: Quiet Extension** — the nav reads like the app's existing sidebar. No
  grouping, no badges; planned sections simply recede in color. Lowest ceremony.
- **C: Command Deck** — identity header (`seanb · administrator`), a `⌘K` jump
  affordance wired to the existing command palette, per-row `planned` badges, and
  a standing "you are acting as an administrator" notice above the content.
- **C2: merge into rail ★ WINNER** — C's design in every respect; differs *only*
  in the collapse. Instead of leaving a second icon column, the admin sections
  are absorbed **into** the rail below a divider.

All four render the **same** character table so the nav treatment is the only
variable.

## Round 2 — the collapse (decided)

**Decision: the admin nav collapses against the section rail.** Implemented with
**container queries** (`container-name: vp`) rather than media queries, so the
toolbar's 375 / 768 / 1280 buttons genuinely exercise the breakpoints instead of
statically mocking them.

| Container width | Rail | Admin nav | Content |
| --- | --- | --- | --- |
| **≥ 1024px** | 48px icons | 232px, labels + `planned` badges | full table |
| **768–1023px** | 48px icons | collapses to a 48px icon column flush against the rail; hover tooltips carry the label + status | full table |
| **< 768px** | width 0 | width 0 | `.mobilebar` with hamburger + `Admin › Characters`; `Created` / `Last seen` columns drop |

C and C2 differ **only** in the 768–1023px band — that band is the entire
question:

- **C (two icon columns)** keeps the app/admin hierarchy visible: the rail is
  "where in HoloMUSH", the second column is "where in Admin". Costs 96px of chrome.
- **C2 (merged) — CHOSEN** reclaims 48px and reads as one nav.

The `< 768px` behavior is shared and matches the app's existing pattern
(`(authed)/+layout.svelte` puts the Rail inside a `Sheet` drawer via
`mobileNavOpen`) — so the phone drawer should hold rail sections **and** admin
sections together.

### Two defects the merge exposed — both fixed

Merging is not free, and building it surfaced two concrete problems that would
otherwise have been inherited by Phase 6:

1. **Two active indicators in one column.** The Admin rail button and the
   Characters section button were both `is-active`, each drawing the cyan active
   bar — two "you are here" markers at two different levels of hierarchy. Fixed
   with `.rail-btn.is-context`, scoped **inside** the `max-width: 1023px`
   container query: once merged, Admin keeps the primary tint (you *are* in
   Admin) but surrenders the active bar to the section you're actually on. At
   ≥1024 it keeps its bar, because there it is the only thing telling you where
   you are.
2. **The identity header and `⌘K` vanished.** `.adminnav.is-merge` collapses to
   `width: 0`, taking the `seanb · administrator` block and the jump affordance
   with it. Given the trust-boundary framing, that signal is load-bearing — both
   return to the rail foot (`.rail-identity`), rendered only at the collapsed
   breakpoint so they never duplicate the nav's own copies at ≥1024.

This is the honest cost of C2: the merged column needs an explicit hierarchy
device (`is-context`) that C got for free from having two columns.

## What to Look For

- **Does the 6-planned-to-1-available ratio read as intentional or as vacancy?**
  This is the sketch's core question. A is the most explicit about it; B is the
  most silent; C splits the difference with per-row badges.
- **Is the "trust boundary" legible?** B deliberately does *not* signal it —
  compare how that feels against C's standing notice. If B feels fine, the notice
  is ceremony; if B feels careless, C's notice is load-bearing.
- **Does the middle column earn `--adminnav-w: 232px`?** At 375px viewport the
  rail + nav + content is too much; note what should collapse.
- **A's footer line** — is stating the registry contract in the UI useful
  grounding, or is it leaking implementation detail at the operator?

## Grounding

Drawn from real repo state, not invented:

| Element | Source |
| --- | --- |
| Rail geometry, active-bar, hover colors | `web/src/lib/components/shell/SectionRail.svelte` |
| `--topbar-h: 44px`, `--rail-w: 48px`, every color token | `web/src/app.css` (mirrored verbatim into `themes/default.css`) |
| Seven section ids + `available`/`planned` status | `01-SPEC.md` §10.1 |
| Nav derived from registry, never `{#if}` | ROADMAP Phase 6 SC5 |
| Amber used **only** as the wordmark cursor | `.claude/rules/branding.md` INV-1 |
| `idle` row in the table | SPEC §4.3 — in the vocabulary, unreachable in v0.13; shown to prove the exhaustive switch has a rendering |

## Components this implies adding

Not currently in `web/src/lib/components/ui/`:

| Component | Needed for | Sketch |
| --- | --- | --- |
| `table` | the character list | 001, 002 |
| `pagination` | 412 characters | 002 |
| `empty` | planned-section state | 003 |
| `alert` | C's administrator notice | 001 |
| `avatar` | C's identity header | 001 |
| `breadcrumb` | A/C's `Admin › Characters` | 001 |
| `skeleton` | list loading | 002 |
| `select` | status filter | 002 |
| `field` | the edit surface | 004 |
| `sonner` | mutation confirmations | 004 |

Add with `npx shadcn-svelte@latest add <name>` (style `nova`, per `web/components.json`).

## Open question this sketch surfaces

`SECTIONS` in `web/src/lib/nav/sections.ts` currently has **no status concept** —
every entry is live. The admin registry needs `status: 'available' | 'planned'`
as a *required* field (mirroring §10.2's "no default, no zero value means
allow"). Whether that lives on the existing `WorkspaceSection` shape or a
separate `AdminSection` type is a Phase-6 planning decision this sketch does not
settle.
