---
name: sketch-findings-holomush
description: Validated design decisions, CSS patterns, and visual direction from HoloMUSH sketch experiments — the v0.13 web admin portal (shell/nav, dense admin tables, gated-and-planned sections, field-masked edit forms and destructive actions). Use when implementing or planning any admin-portal UI, when choosing layout/breakpoint/nav treatment for the web client, or before drawing an admin table, empty state, or edit form.
---

<context>
## Project: holomush

The HoloMUSH admin portal is a **gated console living inside the game client**, not a
separate product. It inherits the app's existing dark, terminal-adjacent language — cyan
(`#3dd6f7`) as the only accent, amber reserved exclusively for the cursor, `#0b0c0e`
ground, hairline `#1d2a33` borders — and reuses the persistent `SectionRail` so an operator
is never more than one click from the game.

Inside that frame it must feel **consequential**: actions here touch characters the operator
does not own, and are audited. The distinctive design problem is that **six of its seven
sections have no handler yet** and must read as deliberate reserved capacity rather than an
unfinished app.

Reference points are **grounded in-repo**, not borrowed from outside products:
`shell/SectionRail.svelte`, `nav/sections.ts` (the `as const satisfies` registry pattern the
SPEC says to mirror), `(authed)/characters/+page.svelte`, and
`.planning/phases/01-portal-spec/01-SPEC.md` §10.

Sketch session wrapped: **2026-08-01** (sketches 001–004, all four included in full).
Milestone: **v0.13 — Web Portal: Identity & Admin Foundations**. Target build phase: **6**.
</context>

<design_direction>
## Overall Direction

- **Palette.** One accent: cyan `#3dd6f7`. Amber `#ffb300` is the **cursor only** —
  `.claude/rules/branding.md` INV-1. Ground `#0b0c0e`, surfaces `#101418`, hairline borders
  `#1d2a33`. Tints via `color-mix`, never hardcoded.
- **Layout.** Three columns: 48px section rail (persists) + 232px admin nav + content.
  Breakpoints are **container queries** (`@container vp`), not media queries.
- **Collapse.** At 768–1023px the admin nav merges **into** the rail below a divider; below
  768px both go to zero and a `.mobilebar` + drawer takes over.
- **Nav is derived from the core-authoritative registry**, filtered by permission — never a
  template `{#if}`.
- **Restraint over narration.** Three separate sketches independently rejected
  implementation detail in the operator's face (registry-contract footer, authorization
  trace, speculative scope copy). The one place explanation *is* load-bearing is the edit
  form's excluded fields, because a missing field is actively confusing in a way a missing
  trace is not.
- **Absence is designed, not defaulted.** Planned sections, denied sections, empty results,
  zero rows and never-active characters each have a deliberate, distinct treatment.
- **Destructive means reversible.** There is no delete in this portal; the destructive
  action is Retire.
</design_direction>

<findings_index>
## Design Areas

| Area | Reference | Key Decision |
|------|-----------|--------------|
| Shell & Navigation | `references/shell-and-navigation.md` | Three-column frame; container queries; admin nav **merges into the rail** at 768–1023px, with `.rail-btn.is-context` scoped inside that query so only one active bar shows |
| Data Tables & List States | `references/data-tables.md` | Dense table, **inline hover row actions**, no multi-select/bulk; click-header sort only (no sort dropdown, no facets); four distinct non-data states |
| Gating & Absence | `references/gating-and-absence.md` | Minimal `Registered and gated. No handler yet.`; `/admin` invisible without permission; deep links render the **ordinary not-found**, never a redirect |
| Forms & Destructive Actions | `references/forms-and-destructive-actions.md` | Two groups — `Managed elsewhere` (first, collapsed) then `Editable here`; `version` is header metadata; status is a **transition picker that never sends a status value** |
| Foundations | `references/foundations.md` | Palette tokens + INV-1; the 10 shadcn components to install; what the sketch theme actually is |
| Anti-patterns | `references/anti-patterns.md` | The nine mistakes the sketches actually made or the SPEC warns are reflexive — **read this before drawing anything** |

## Hard constraints (violating these is a bug, not a taste call)

1. **Amber is the cursor only.** Never an accent, link, button, badge, or status color (INV-1).
2. **There is no `AdminDeleteCharacter`.** Wiring `world.Service.DeleteCharacter` to an
   admin button is forbidden by §4.4 *and* §10.6. Admin disable is **Retire**, reversible.
3. **Never send a `status` value.** Send `AdminRetireCharacter` / `AdminUnretireCharacter`.
   A maskable `status` path puts the unreachable `idle` back on the wire (§10.6).
4. **No sort dropdown, no facet panel** — §11.3 names these as the specific warning sign.
5. **UX invisibility is never the boundary.** The ABAC gate on `admin_section:*` is; every
   admin RPC must still deny independently (§10.4).
6. **Deep-link denial renders the *ordinary* not-found.** A redirect, or a bespoke `/admin`
   not-found, destroys the indistinguishability the design rests on.
7. **`characters` has no last-seen column.** Do not draw one (see A1 below).

## ⚠ Carried-forward blockers

| Id | What | Status |
|---|---|---|
| **A1** | `characters.last_active_at` — new durable column + write path at **session start** (never lease refresh) + a §11.3 row | **Unsanctioned SPEC amendment** — must land in `01-SPEC.md` before Phase 6 builds it |
| **A2** | Sorting the admin list by `players.username` (add a new §11.3 row; leave the `player_id` row as written) | **Unsanctioned SPEC amendment** |
| **A3** | `AdminSearchCharacters` extended to player usernames | **Unsanctioned SPEC amendment** |
| **D1** | §10.3 vs §10.4 — distinguishable denial codes are a registry-enumeration oracle | **SPEC defect**, [#4904](https://github.com/holomush/holomush/issues/4904) — route to `abac-reviewer` |
| — | No `+error.svelte` exists anywhere under `web/src/routes/` | **Gap**, [#4903](https://github.com/holomush/holomush/issues/4903) — Phase 6 must build it |
| — | Does the admin portal expose rename at all? §9.3's census has no `AdminRenameCharacter` | **Open** — settle before building the edit form |
| — | `SECTIONS` in `nav/sections.ts` has no `status` concept; the admin registry needs it as a **required** field | **Open** — Phase 6 planning decision |

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
</findings_index>

<metadata>
## Processed Sketches

- 001-admin-shell-frame — winner **C2** (Command Deck, merged collapse)
- 002-admin-character-table — winner **A** (inline row actions)
- 003-planned-section-empty — winner **A** (minimal)
- 004-character-edit-destructive — winner **C** (two groups, refined)
</metadata>
