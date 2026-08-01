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
| `/gsd-sketch --wrap-up` → `.claude/skills/sketch-findings-*/SKILL.md` | `discuss-phase:251`, `plan-phase:611,753` | the design decisions, as `<prior_decisions>` |

**The wrap-up has not been run yet** — until it is, phase discussions get a
warning that unpackaged sketches exist but none of their content.

## ⚠ Open SPEC amendments raised by sketches

These are maintainer-directed decisions that exceed the SPEC as written. They
MUST be amended into `01-SPEC.md` before Phase 6 implements them.

| Id | Raised by | Amendment |
| --- | --- | --- |
| A1 | 002 | `characters.last_active_at` — new durable column (Phase 2 migration, epoch-ns `BIGINT`), written at **session start** (never on lease refresh), plus a §11.3 row permitting sort + filter. Cannot be derived: `sessions` rows are reaped and `session_connections.last_seen_at` is a gateway lease — both mean "online now". `never` must render as `never` and sort to the END in both directions. |
| A2 | 002 | Sorting the admin list by player. §11.3 forbids ordering `characters.player_id`; what the UI sorts is the joined `players.username`, which §11.3 never enumerates. Justified by §11.3's own test — the admin audience already sees usernames, so the ordering discloses nothing. Leave the `player_id` row as written; add a new one. |
| **D1** ([#4904](https://github.com/holomush/holomush/issues/4904)) | **003** | **SPEC DEFECT, not a widening.** §10.3 requires a planned-section refusal to "reveal nothing about which sections exist", but §10.4 defines two distinguishable denial codes (`DENY_ADMIN_SECTION` vs `DENY_ADMIN_SECTION_UNREGISTERED`). A non-admin probing an arbitrary id versus a real one enumerates the registry. §13's eight invariants pin none of this, though `INV-PRIVACY-9` does exactly this job for profiles. Fix: collapse the codes for unauthorized callers, **and** add an `INV-ACCESS-<n>` mirroring `INV-PRIVACY-9`. **Route to `abac-reviewer`** as a *spec-consistency defect with a latent disclosure channel*, **not** an active registry leak — the section ids are already public (in the SPEC, and shipped in the client bundle for nav). It still matters because §10.3 asserts a property the system lacks, and because the core-side registry may hold sections the client mirror does not. Hiding `/admin` does **not** mitigate it — it concentrates the denial path onto callers deliberately bypassing the UI. |
| A3 | 002 | `AdminSearchCharacters` (§9.2) currently "searches names" (character names). Extend to player usernames. |
