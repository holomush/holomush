# Foundations — theme, palette, component inventory

Cross-cutting substrate shared by sketches **001–004**.

## Palette — cyan is the accent, amber is the cursor

Per `.claude/rules/branding.md` **INV-1**:

> `--color-cursor` (`#ffb300` amber) is the **CURSOR ONLY**. Never an accent, link, button,
> badge, or status color. **The accent is cyan.**

All four sketches honor this. Putting amber into `--sl-color-accent*` or any button/link/
badge is a branding bug, not a taste call.

| Token | Value | Role |
| --- | --- | --- |
| `--color-primary` / `--color-accent` / `--color-ring` | `#3dd6f7` | the single accent |
| `--color-background` | `#0b0c0e` | ground |
| `--color-surface` / `--color-card` / `--color-popover` / `--color-sidebar` | `#101418` | raised surfaces |
| `--color-border` / `--color-input` | `#1d2a33` | hairline borders |
| `--color-foreground` | `#e8edf2` | body text |
| `--color-muted-foreground` / `--color-status-text` | `#9aa7b2` | secondary text |
| `--color-destructive` / `--color-status-offline` | `#fc7f7f` | destructive + offline |
| `--color-status-online` | `#7fd98f` | online |
| `--color-cursor` | `#ffb300` | **cursor only — INV-1** |
| `--radius` | `0.5rem` | corner radius |

Layout tokens: `--topbar-h: 44px`, `--rail-w: 48px`, `--adminnav-w: 232px`.

Tints are derived with `color-mix`, never hardcoded:

```css
background: color-mix(in srgb, var(--color-primary) 14%, transparent);
border-color: color-mix(in srgb, var(--color-primary) 45%, transparent);
color: var(--color-primary);
```

## ⚠ What `sources/themes/default.css` actually is

Its own header comment and the sketch MANIFEST both say it *"mirrors `web/src/app.css`
verbatim"*. **Verified 2026-08-01 — that claim is not accurate**, and the difference matters
if you treat the file as an app.css substitute.

What is true:

- It carries **34 of app.css's 39 `--color-*` tokens at byte-identical values.** Every
  shared token agrees. **The color values are trustworthy.**
- **5 tokens are absent**, all unused by every sketch (verified zero references):
  `--color-scrollback-indicator`, `--color-scrollback-replayed`,
  `--color-sidebar-accent-foreground`, `--color-sidebar-primary-foreground`,
  `--color-sidebar-ring`.
- It **restructures** `@theme { … }` into a plain `:root { … }` (necessary — the sketches
  are plain HTML with no Tailwind build).
- It **drops** `@layer base` (the border-color reset, the body font stack), the density
  tokens (`.app-root[data-density="cozy"|"compact"]`), and the `prefers-reduced-motion`
  animation keyframes (`dot-pulse`, `just-arrived`, `composer-slide-up`).
- It **adds** ~180 lines of sketch-only scaffolding classes (`.badge`, `.btn`, `.rail`,
  `.dot`, `.kbd`, …) that have no production counterpart.

**Practical consequence:** trust it for **color values**; do **not** treat it as the app's
styling contract. In particular, the sketches inherit no density tokens and no
reduced-motion gating — production code must apply both.

## Components to install

Ten shadcn-svelte components the sketches exercise are **not currently in
`web/src/lib/components/ui/`**:

| Component | Needed for | Sketch |
| --- | --- | --- |
| `table` | the character list | 001, 002 |
| `pagination` | 412 characters | 002 |
| `empty` | planned-section state | 003 |
| `alert` | administrator notice, no-scope panel | 001, 003 |
| `avatar` | identity header / rail foot | 001 |
| `breadcrumb` | `Admin › Characters` | 001 |
| `skeleton` | list loading state | 002 |
| `select` | status filter | 002 |
| `field` | the edit surface (`Field.FieldGroup` / `Field.Field`) | 004 |
| `sonner` | post-mutation toasts | 004 |

Install with `npx shadcn-svelte@latest add <name>`.

Already installed and used: `badge`, `separator`, `checkbox`, `sheet`. Also needed:
`alert-dialog` (the retire confirmation).

**Component budget is open** — the maintainer's call at intake was "add whatever makes it
genuinely good; log adds per sketch".

## Target stack

SvelteKit 2.69 · Svelte 5.56 (runes) · Tailwind 4.3 · shadcn-svelte style **`nova`**
(baseColor `slate`) · `bits-ui` 2.18 · icons `@lucide/svelte`.

Per `web/components.json`. Sketches are plain HTML — production is Svelte 5 runes.

## In-repo reference points

These are the grounding sources; prefer them over anything borrowed from outside products.

| Element | Source |
| --- | --- |
| Rail geometry, active-bar, hover colors, drawer variant | `web/src/lib/components/shell/SectionRail.svelte` |
| The `as const satisfies` section-registry pattern (derived union + visibility gate) | `web/src/lib/nav/sections.ts:41-47` |
| Existing Card + Badge character idiom | `web/src/routes/(authed)/characters/+page.svelte` |
| Mobile drawer pattern (`Sheet` + `mobileNavOpen`) | `web/src/routes/(authed)/+layout.svelte` |
| Every color + layout token | `web/src/app.css` |
| Amber-cursor-only constraint | `.claude/rules/branding.md` INV-1 |
| Normative admin registry, descriptor, gating contract | `.planning/phases/01-portal-spec/01-SPEC.md` §10 |

**`nav/sections.ts` already implements the registry pattern the SPEC says to mirror — do not
add a library for it.** It also already carries `requiresPlayer`, which is the right handle
for the viewer-dependent "← Back to HoloMUSH" target on the not-found page.

## Origin

Synthesized from sketches: **001, 002, 003, 004**
Theme file: `sources/themes/default.css`
