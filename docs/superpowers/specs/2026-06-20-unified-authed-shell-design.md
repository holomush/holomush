<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Unified Authed Workspace Shell — Design

- **Bead:** `holomush-q41kr` (Web: unify authed workspace chrome — shared `(authed)` shell + rail-as-section-switcher)
- **Theme:** `theme:web-portals` (`holomush-sz0h3`)
- **Status:** Design (brainstorming) — pending `design-reviewer`
- **Date:** 2026-06-20
- **Author:** Sean Brandt (with Claude)

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are
to be interpreted as in RFC 2119 / RFC 8174.

---

## 1. Problem & scope

The authed web client renders as two different apps. `/terminal` has the full
workspace chrome — a left icon **Rail**, a rich **TopBar** (`Char @ Location`,
`⌘K` hint, connection pill, sidebar toggle), a right **Sidebar**
(location/exits/presence), and a **footer hotkey bar**. `/scenes` has none of
it: no rail, a stripped TopBar (logo + theme + identity + swap + logout only),
no footer, and its own bespoke 3-pane. Switching between them feels like
leaving one app and entering another.

The governing product principle (memory `a4baea60`, decision `holomush-sz0h3`)
is that the web client is a **superset of telnet**: `/terminal` is the
first-class on-grid surface, and a growing set of subsystems (scenes, DMs,
forums, wiki, character profiles, settings, admin) each get a rich web GUI.
For that to read as **one workspace**, every authed section MUST share a
persistent frame, and moving between sections MUST be clean and effortless.

**Root cause (grounded).**

- There is **no shared authed shell**. `web/src/routes/(authed)/+layout.ts`
  is load-only (`web/src/routes/(authed)/+layout.ts:10-31`); there is no
  `(authed)/+layout.svelte`. `web/src/routes/(authed)/scenes/+layout.svelte`
  is a bare `{@render children()}` pass-through (lines `8,11`).
- The **Rail** is rendered only by the terminal page
  (`web/src/routes/(authed)/terminal/+page.svelte:701`), inside
  `.terminal-layout` — so `/scenes` has no rail at all. Its items are
  placeholders: `Room` is hardcoded `is-active`
  (`web/src/lib/components/terminal/Rail.svelte:34`); `DM/Map/Notes` are
  disabled "coming soon" (`Rail.svelte:38-61`); there is no `Scenes` entry, so
  the rail cannot act as a section switcher.
- The **TopBar** is already global (rendered in the root layout,
  `web/src/routes/+layout.svelte:105`) but conditionally hides its
  terminal-context bits via `onTerminal = $page.route.id?.includes('/terminal')`
  (`web/src/lib/components/TopBar.svelte:41`, gates at `:61-87,:124-133`) —
  which is why `/scenes` looks sparse.
- The **footer hotkey bar** lives inside the terminal's `CommandInput`
  (`web/src/lib/components/terminal/CommandInput.svelte`) — terminal-only.

**In scope:** the persistent shell, the rail-as-section-switcher (route-aware),
a persistent footer bar, mobile nav, and de-duplication of chrome that the
unification makes redundant.

**Out of scope (unchanged from the bead):** enabling future placeholder
sections (DMs/Forum/Wiki etc. land with their own work), terminal
game/stream logic, scene CRUD, and the terminal's internal right-Sidebar
mobile behavior (a separate concern).

---

## 2. Decisions

Settled during brainstorming (traces on `holomush-q41kr`):

| # | Decision | Rationale |
| - | -------- | --------- |
| D1 | **Navigation = persistent left icon Rail** (route-aware section switcher) **plus** a `⌘K` "go to…" palette accelerator. | Rail matches the terminal aesthetic the user set as the shell baseline and is already built; palette is the keyboard power-path. Rejected top-tabs (eats vertical space, crowds past ~5 sections) and palette-only (undiscoverable). |
| D2 | **Shell boundary:** a new `(authed)/+layout.svelte` owns Rail + persistent footer + the section slot; TopBar stays global in the root layout; each section keeps its own inner layout. | Idiomatic SvelteKit nested layout (context7 `/sveltejs/kit`); the section-local inner content (terminal panes, scenes 3-pane) does not move. |
| D3 | **Footer = persistent shell-owned bar** with **section-filled contents**. | The frame must always be visually closed ("don't fall off into nowhere"), but the hotkey contents stay terminal-specific. |
| D4 | **Mobile (<768px) = slide-over drawer** (shadcn `Sheet`) hosting the same Rail. | Zero permanent vertical space (terminal keyboard concern); reuses the Rail + an existing primitive; one mental model with desktop. |
| D5 | **Rail items come from a declarative section registry** that also feeds the palette. | "Real sections only" now (Room, Scenes) with a *defined* growth zone, so adding a section is one entry and rail/palette never drift. |
| D6 | **`Room` is always present**; a no-session click lands on `/terminal`'s existing Sign-In/Reconnect screen. | Keeps "scenes-only player is first-class" (memory `a4baea60`) without special gating. |

---

## 3. Architecture

### 3.1 Layout nesting

```text
root +layout.svelte                      (unchanged ownership)
├── TopBar                               global — incl. mobile ☰ (authed, <768px)
├── <main>
│   └── (authed)/+layout.svelte          NEW — the persistent authed shell
│       ├── Rail (desktop, full height)  route-aware switcher
│       └── section column
│           ├── {@render children()}     the active section's page
│           └── ShellFooter              persistent bar (section-filled)
├── Composer                             global overlay (unchanged)
└── CommandPalette                       global overlay (+ "go to" entries)
```

- The shell renders only for authed routes, so the Rail/footer never appear on
  `/login` or `/register`. The TopBar stays in the **root** layout because it
  already renders pre-auth states (`TopBar.svelte:135-137`).
- Desktop shell body is a horizontal row: **Rail (full height) | section
  column**. The section column is vertical: **page slot (flex:1) + ShellFooter**.
  The footer therefore spans the section content width (below both the terminal
  column and its right Sidebar), giving a single closed bottom edge.
- **The shell owns viewport height.** The shell sizes to
  `calc(100vh - var(--topbar-h))`, the page slot is `flex:1`, and the footer is
  a fixed-height bar below it. Each section page MUST therefore size to its
  container (`height:100%` / `flex:1`) and MUST NOT recompute viewport height
  itself; the terminal page sheds its own `calc(100vh - …)` (§4) or it will
  overflow behind the footer (§6 no-overflow check).
- Active-section derivation MUST use a **route prefix match** against
  `$page.url.pathname` (`$app/stores`, matching the existing TopBar convention),
  so `/scenes`, `/scenes/browse`, and `/scenes/[id]` all resolve to the Scenes
  item. Exact-equality matching is insufficient and MUST NOT be used.

### 3.2 The section registry

A new module (e.g. `web/src/lib/nav/sections.ts`) exports a typed, ordered list
— the **single source of truth** for both the Rail and the palette's "go to…"
entries:

```ts
export interface WorkspaceSection {
  id: string;            // 'room' | 'scenes' | …
  label: string;        // 'Room', 'Scenes'  (tooltip on desktop, label in drawer)
  icon: Component;      // lucide icon
  href: string;         // '/terminal', '/scenes'
  match: (pathname: string) => boolean;  // prefix predicate for active state
}
```

- Today the registry contains exactly **Room → `/terminal`** and
  **Scenes → `/scenes`**, in that order.
- Items render in the Rail's **top group** (the defined growth zone, above the
  spacer). The bottom `⚙` view-prefs control is pinned **below** the spacer and
  is NOT a registry entry.
- Adding a future section is one registry entry plus its route; it appears in
  the Rail and the palette automatically. The Rail and palette MUST NOT
  hardcode section lists independently.
- The vestigial `RailView` type (`web/src/lib/stores/uiPrefsStore.ts:7`,
  `'room'` with a `// future: 'dm' | 'map' | 'notes'` comment) is retired by
  this change.

### 3.3 The persistent footer (bridge)

The footer mirrors the existing **composer bridge** pattern
(`web/src/lib/stores/composerBridge.ts:1-21` — a `writable` store plus
register/invoke helpers). A new `footerBridge` store lets the active section
push footer content; `ShellFooter.svelte` renders it, falling back to a
baseline when nothing is registered.

- **Mechanism:** the active section registers a footer snippet on mount and
  clears it on destroy (`afterNavigate`/`onDestroy`), exactly as the composer
  bridge registers its submit handler.
- **Terminal:** its hotkey row moves **out of** `CommandInput`
  (`CommandInput.svelte:187-194`) into a footer snippet registered via the
  bridge. The labels are the existing `cmd-hints` bar — at the time of writing
  `↑↓ history · ⇧⏎ newline · Esc clear · ⌘K palette · ⌘B rail · ⌘. sidebar ·
  ⌘⇧E composer` (note: `Esc` clears the draft; `⌘L` clears the output buffer —
  a different action). The plan MUST **relocate the existing snippet from code**,
  not re-type from this prose, so the hints can never drift. Contents remain
  terminal-only.
- **Baseline (any section that registers nothing):** *section name* (left) +
  *`⌘K go to…`* hint + *connection dot* (right). The baseline MUST always
  render so the bar is never empty/absent.

---

## 4. Component changes

| File | Change |
| ---- | ------ |
| `web/src/routes/(authed)/+layout.svelte` | **NEW.** The persistent shell: Rail (desktop) + section slot + `ShellFooter`. Hosts the authed-only key handlers that today live in the root layout for rail/sidebar/composer/clear (`+layout.svelte:49-76`, scope confirmation in §9 Q3); `⌘K` palette stays global in root. |
| `web/src/lib/components/terminal/Rail.svelte` → (rename to a shell location, e.g. `web/src/lib/components/shell/SectionRail.svelte`) | Render items from the registry as real `<a>` links; active state from the prefix predicate; remove the hardcoded `is-active` (`:34`) and the disabled `DM/Map/Notes` placeholders (`:38-61`). Keep the bottom `⚙`, but reduce it to **view prefs** (density, black-terminal-bg); the **theme** submenu is removed here (dedup → TopBar). |
| `web/src/routes/(authed)/terminal/+page.svelte` | Remove `<Rail />` (`:701`); the shell now provides it. Register the hotkey row via `footerBridge` instead of rendering it in `CommandInput`. **Shed the page's own `height: calc(100vh - var(--topbar-h))`** (`:731` login-screen, `:753-756` `.terminal-layout`) — the shell owns viewport height (§3.1), so the terminal sizes to its container (`height:100%`) and does not overflow the `ShellFooter`. |
| `web/src/lib/components/terminal/CommandInput.svelte` | Footer hotkey bar extracted to the shell footer snippet. |
| `web/src/routes/(authed)/scenes/+layout.svelte` | Stays a thin pass-through; the shell supplies rail + footer. Its sparse look resolves automatically. |
| `web/src/lib/components/TopBar.svelte` | Remove the now-redundant Clapperboard "→ Scenes" icon (`:140-142`); add a mobile `☰` drawer toggle (authed + `<768px`). Keep theme/identity/swap/logout and the terminal-gated bits. |
| `web/src/lib/components/terminal/CommandPalette.svelte` | Add "go to <section>" entries from the registry to `items` (`:43`), using the existing `goto` path (`:26`). |
| `web/src/lib/stores/uiPrefsStore.ts` | Retire `RailView` (`:7`); reuse `railHidden` (`:10`) for desktop collapse; add a transient mobile-drawer-open state (need not persist). |
| `web/src/lib/nav/sections.ts`, `web/src/lib/stores/footerBridge.ts`, `web/src/lib/components/shell/ShellFooter.svelte` | **NEW** per §3.2–3.3. |

---

## 5. Responsive / mobile (D4)

- **≥768px:** Rail is a persistent full-height column; `⌘B` toggles
  `railHidden` (existing pref). Desktop items are **icon-only with a tooltip**
  (today's behavior).
- **<768px:** the persistent Rail collapses (today it is already `width:0`,
  `Rail.svelte:118-123`). A `☰` button in the TopBar opens the **same** Rail
  inside a shadcn `Sheet` left drawer (precedent: `Sheet` primitives at
  `web/src/lib/components/ui/sheet/*`, used by `CreateSceneSheet.svelte`).
  Drawer items show **icon + label**.
- The drawer MUST close on selection (navigation), on scrim tap, and on `Esc`
  (the `Sheet` primitive provides scrim/Esc).

---

## 6. Edge cases & no-regression

- **No active game session:** the `Room` item is always present; clicking it
  navigates to `/terminal`, which renders its existing Sign-In/Reconnect screen
  (`terminal/+page.svelte:688-698`). No section gating; scenes-only players are
  unaffected.
- **Active-state correctness:** nested routes under a section MUST highlight
  that section (prefix match, §3.1).
- **Layout height / no overflow:** with the `ShellFooter` below the section
  slot, the terminal MUST size to its container, not the viewport — verify it
  does not overflow behind the footer (the §4 height-shed). Applies to any
  section that previously assumed full viewport height.
- **Keyboard shortcuts:** `⌘K` (palette), `⌘B` (rail), `⌘.` (sidebar), `⌘⇧E`
  (composer), `⌘L` (clear) MUST continue to work with no behavior change. Moving
  their handlers from root into the authed shell MUST NOT drop any binding.
- **Terminal UX:** stream, composer, right Sidebar, resizable panes, and all
  hotkeys MUST be unchanged — only the Rail's owner and the footer's host move.

---

## 7. Testing

Per `.claude/rules/testing.md` (ACE names; vitest unit + Playwright E2E).

**Unit (vitest):**

- The registry renders the expected Rail items in order; each is a real link to
  its `href`.
- Active state resolves via prefix: `/scenes`, `/scenes/browse`, `/scenes/abc`
  all mark Scenes active; `/terminal` marks Room active.
- `footerBridge` baseline renders when no section registers content; a
  registered snippet replaces the baseline and is cleared on navigation.
- Palette "go to…" entries are derived from the same registry (no second list).

**E2E (Playwright):**

- Navigating `/terminal` ↔ `/scenes` preserves the Rail and footer; the active
  Rail item tracks the route.
- `⌘K` → "go to Scenes" switches sections.
- Narrow viewport: `☰` opens the drawer, selecting a section navigates and
  closes it.
- No terminal regression: existing `web/e2e/terminal.spec.ts` and
  `scenes.spec.ts` pass. New `registerAndEnterTerminal` callers, if any, MUST
  use a ≤4-char alphanumeric (no-hyphen) prefix (memory `a600e10f`).

**Gate:** `cd web && pnpm check` clean; relevant vitest + the two E2E specs green.

---

## 8. Invariants

No **new registry invariant** (`docs/architecture/invariants.yaml`) is minted.
The durable guarantee here — "every authed section preserves the persistent
frame and the Rail tracks the route" — is a **product/UX requirement** verified
by the §7 E2E acceptance, not a system-level invariant in the registry sense
(consistent with prior web-frontend work, which kept TS-store/UI properties out
of the registry; memory note on the bare-`INV-6` web-store overload). Per
`.claude/rules/invariants.md`, a local feature requirement is not an invariant,
and no ad-hoc invariant family is introduced.

---

## 9. Open questions for the plan phase

1. **Rail component home/rename.** Moving `terminal/Rail.svelte` →
   `shell/SectionRail.svelte` is the clean boundary, but it widens the diff and
   any test importing the old path. Acceptable, or keep the path and just
   relocate the render site? (Plan-phase call.)
2. **Key-handler relocation.** Moving rail/sidebar/composer/clear handlers from
   the root layout into the authed shell is cleaner but is a behavioral-surface
   move; confirm it is in scope for this change rather than a follow-up.

**Resolved during review:** *Footer span* — the footer spans the **section
content width** (right of the Rail), per §3.1, matching today's terminal footer
placement. Full-shell-width (VS Code-style, under the Rail too) is a viable
plan-time alternative if preferred, but the default stands.
