# Sketch Wrap-Up Summary

**Date:** 2026-08-01
**Sketches processed:** 4 (4 included, 0 excluded)
**Design areas:** Shell & Navigation · Data Tables & List States · Gating & Absence ·
Forms & Destructive Actions · Foundations · Anti-patterns
**Skill output:** `.claude/skills/sketch-findings-holomush/`
**Milestone:** v0.13 — Web Portal: Identity & Admin Foundations · target build phase **6**

## Included Sketches

| # | Name | Winner | Design Area |
|---|------|--------|-------------|
| 001 | admin-shell-frame | **C2** — Command Deck, merged collapse | Shell & Navigation |
| 002 | admin-character-table | **A** — inline row actions | Data Tables & List States |
| 003 | planned-section-empty | **A** — minimal | Gating & Absence |
| 004 | character-edit-destructive | **C** — two groups (refined) | Forms & Destructive Actions |

## Excluded Sketches

None. All four were included in full at the curation checkpoint.

## Design Direction

The admin portal is a **gated console living inside the game client**, not a separate
product. It inherits the app's dark, terminal-adjacent language — cyan `#3dd6f7` as the only
accent, amber `#ffb300` reserved exclusively for the cursor (INV-1), `#0b0c0e` ground,
hairline `#1d2a33` borders — and reuses the persistent `SectionRail` so an operator is never
more than one click from the game. Inside that frame it must feel **consequential**: actions
touch characters the operator does not own, and are audited.

The distinctive design problem is that **six of its seven sections have no handler yet** and
must read as deliberate reserved capacity rather than an unfinished app.

A consistent thread emerged across all four sketches: **restraint over narration.** Three
sketches independently rejected implementation detail in the operator's face — 001's
registry-contract footer ("Registry Ledger"), 003's authorization trace ("Gate provenance"),
and 003's speculative scope panels. The single place explanation *is* load-bearing is 004's
excluded form fields, because a missing field is actively confusing in a way a missing trace
is not — and "incomplete" is what a well-meaning implementer *fixes*.

## Key Decisions

**Layout.** Three columns — 48px section rail (persists) + 232px admin nav + content.
Breakpoints via **container queries** (`container-type: inline-size; container-name: vp`),
not media queries. At 768–1023px the admin nav merges **into** the rail below a divider
(reclaiming 48px over the two-icon-column alternative); below 768px both collapse to zero
and a `.mobilebar` + drawer takes over, with the drawer holding rail **and** admin sections
together.

**Merged-collapse hierarchy.** Merging cost an explicit hierarchy device that two columns
gave for free. `.rail-btn.is-context` — scoped **inside** the `max-width: 1023px` container
query — lets Admin keep its primary tint while surrendering the active bar to the section
you are actually on. Identity + `⌘K` relocate to `.rail-identity` at the collapsed
breakpoint only, so they never duplicate the nav's own copies at ≥1024.

**Nav derivation.** Sections come from the core-authoritative registry, filtered by
permission — never a template `{#if}`. Mirrors the existing `as const satisfies` pattern at
`nav/sections.ts:41-47`; no library added.

**Tables.** Dense, inline hover row actions, no multi-select and no bulk operations (§9's
admin RPCs are all singular). Click-header sort only — no sort dropdown, no facet panel,
which §11.3 names as the specific warning sign. Four distinct non-data states, with
`no results` and `zero characters` deliberately **not** collapsed into one. `total_count` is
safe here only because the admin list is not privacy-partitioned.

**Gating.** `/admin` is invisible without permission — no rail icon, no nav entry — and a
deep link renders the **ordinary not-found page**, never a redirect and never a bespoke
forbidden page. A redirect is distinctive and would confirm the route family exists;
`adapter-static` + `fallback: 'index.html'` makes `/admin/moderation` and `/blahblah`
identical at the HTTP layer **by construction**. The route guard is UX; the ABAC gate on
`admin_section:*` remains the boundary.

**Forms.** Sheet with two groups — `Managed elsewhere` first and collapsed (~30px summary
line, expandable), then `Editable here` (the 13 maskable paths). `version` is demoted to
header metadata beside the id, because it is never editable *and* never actionable. Status
is a **transition picker wearing a state picker's clothes**: selecting sends
`AdminRetireCharacter` / `AdminUnretireCharacter`, **never a status value**, keeping the
lifecycle vocabulary off the wire so `idle` stays unreachable. `idle` is shown and never
selectable. `update_mask` is surfaced; an empty mask is a no-op, so Save is inert until
something changes.

**Destructive action.** There is no delete in this portal — the destructive action is
**Retire, which is reversible**, and the name stays reserved.

## Corrections the sketches produced

| Correction | Raised by |
| --- | --- |
| **There is no delete in the admin portal.** §9.3's census has update / retire / unretire and no `AdminDeleteCharacter`; §4.4 and §10.6 both forbid wiring `world.Service.DeleteCharacter` to an admin button. Earlier hand-off notes calling 004 "the irreversible delete" were wrong. | 004 |
| **`characters` has no `last seen` column** and presence is current-only, so 001's first draft fabricated one. Corrected to `version`. | 002 |
| **The sketch theme is not a verbatim `app.css` mirror.** Verified during this wrap-up: 34 of 39 color tokens at byte-identical values (colors trustworthy), but `@theme` restructured to `:root` and `@layer base`, density tokens and reduced-motion keyframes dropped. Both the theme's own header comment and MANIFEST.md overstate it. | wrap-up |

## Carried-forward blockers

| Id | What | Status |
|---|---|---|
| **A1** | `characters.last_active_at` — durable column + write path at session start (never lease refresh) + a §11.3 row; `0` renders `never` and sorts to the END both ways | Unsanctioned SPEC amendment |
| **A2** | Sort by `players.username` — add a new §11.3 row; leave the `player_id` row as written | Unsanctioned SPEC amendment |
| **A3** | `AdminSearchCharacters` extended to player usernames | Unsanctioned SPEC amendment |
| **D1** | §10.3 vs §10.4 — distinguishable denial codes form a registry-enumeration oracle; no invariant pins it though `INV-PRIVACY-9` does the same job for profiles | SPEC defect — [#4904](https://github.com/holomush/holomush/issues/4904), route to `abac-reviewer` |
| — | No `+error.svelte` under `web/src/routes/` — the not-found page the gating design depends on does not exist | [#4903](https://github.com/holomush/holomush/issues/4903) |
| — | Does the admin portal expose rename? §9.3's census has no `AdminRenameCharacter` | Open — settle before building the edit form |
| — | `SECTIONS` has no `status` concept; the admin registry needs it as a required field | Open — Phase 6 planning decision |

## Components to install

Ten shadcn-svelte components the sketches exercise are not yet in
`web/src/lib/components/ui/`: `table`, `pagination`, `empty`, `alert`, `avatar`,
`breadcrumb`, `skeleton`, `select`, `field`, `sonner` — plus `alert-dialog` for the retire
confirmation. Style `nova`, baseColor `slate`, per `web/components.json`.

## Routing

Sketch READMEs are not read by any phase workflow. These findings now reach planning via:

| Route | Reaches | Carries |
| --- | --- | --- |
| `.claude/skills/sketch-findings-holomush/` | `discuss-phase:251`, `plan-phase:611,753` as `<prior_decisions>` | the design decisions |
| `**Sketch findings**` lines on ROADMAP Phases 2, 3, 4, 6 | `discuss-phase` + `plan-phase` | the phase-specific questions, verbatim |
| GitHub #4904 | issue lists, `abac-reviewer` routing | defect D1 |
| GitHub #4903 | issue lists | the missing `+error.svelte` |
