# Foundations — theme, palette, component inventory

Cross-cutting substrate shared by sketches **001–010**.

## Two idioms, deliberately

The portal carries **two** list idioms, and the split is a decision rather than an
inconsistency:

| Surface class | Idiom | Established by |
| --- | --- | --- |
| **Operator** — admin character list, future admin sections | **dense data table**, inline hover row actions | sketch 002 (winner A) |
| **The player's own things** — character roster | **Card grid** | sketch 008 (winner B; variant C converged on the table and was rejected *for* this reason) |

> Convergence on a single idiom is a real benefit and was weighed. It lost because a
> player's own characters should not feel like rows in someone's database.

## Palette — cyan is the accent, amber is the cursor

Per `.claude/rules/branding.md` **INV-1**:

> `--color-cursor` (`#ffb300` amber) is the **CURSOR ONLY**. Never an accent, link, button,
> badge, or status color. **The accent is cyan.**

All ten sketches honor this. Putting amber into `--sl-color-accent*` or any button/link/
badge is a branding bug, not a taste call.

## ⚠ INV-6 — the brand is the platform, never the game

`.claude/rules/branding.md` **INV-6**: the HoloMUSH brand is *"the software/platform only —
never the game world / default setting."*

**Do not hardcode `HoloMUSH` in player-facing copy.** A player is in *a game that runs on*
HoloMUSH. The `>holomush_` wordmark in platform chrome (top bar) is exactly what INV-6
permits; a button reading `Back to HoloMUSH` is not.

**The game's own name is not reachable from the web client.** It exists as
`SettingConfig.DisplayName` — a **required** field on setting-type plugins
(`internal/plugin/manifest.go:211`, enforced at `:494`) — but **no RPC carries it** and no
`Web*` response has the field. Tracked as
[#4905](https://github.com/holomush/holomush/issues/4905).

> ⚠ **Correction (2026-08-02).** Sketch 010's README and the first packaging of this skill
> both claimed *"the only `HoloMUSH` strings under `web/src/` are SPDX copyright headers."*
> **That is false.** Three real render sites exist:
>
> | Site | Renders | Assessment |
> | --- | --- | --- |
> | `lib/components/TopBar.svelte:66` | `<span class="logo-text">HoloMUSH</span>` in `class="logo brand-chip"` | **Fine** — platform wordmark in platform chrome, which INV-6 permits |
> | `routes/+page.svelte:34` | `heroTitle = hero?.metadata?.title ?? 'HoloMUSH'` | **Latent violation** — the platform brand is the *fallback* for game identity |
> | `routes/(authed)/terminal/+page.svelte:689` | `<h1>HoloMUSH</h1>` on the disconnected screen | **Questionable** — no content path behind it at all |
>
> The claim came from an unverified search and propagated into committed docs. It is
> `anti-patterns.md` §1 (fabricating a fact about the tree) committed by this skill itself.

**There are already two competing sources of game identity, and the wrong one is
load-bearing:**

| Source | Required? | Reaches the client? | Scope |
| --- | --- | --- | --- |
| `SettingConfig.DisplayName` | **Yes** | **No** | the game, globally |
| Content key `landing.hero` → `metadata.title` | No (falls back to `'HoloMUSH'`) | **Yes**, via `ContentService.ListContent` (`routes/+page.ts`, `listContent('landing.')`) | landing page only |

So an operator who sets `display_name: My World` and authors no `landing.hero` content item
gets **`HoloMUSH`** on their own landing page.

> **Carried-forward gap:** any player-facing game identity — a title tag, an OG card, a
> welcome line, a "back" target — needs this settled first (#4905). Until then,
> viewer-agnostic copy (`Home`) is the correct answer. **Do not reach for
> `landing.hero.metadata.title` as a general game-name source** — it is optional,
> landing-scoped, and its fallback is the thing INV-6 forbids.

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

Already installed and used: `badge`, `separator`, `checkbox`, `sheet`, `card`, `input`,
`label`, `button`. Also needed: `alert-dialog` (the retire confirmation).

**Sketches 005–010 added nothing to this list.** They exercise what is already on it —
notably `sheet` in its **`side="bottom"`** configuration (natively supported, so the phone
bottom-sheet is *a prop at a breakpoint*, not a second component), plus `alert-dialog` and
`sonner`.

**Component budget is open** — the maintainer's call at intake was "add whatever makes it
genuinely good; log adds per sketch".

## Mobile constraint: 15px inputs, not 12.5px

Any `<16px` font in a **focused input** triggers iOS Safari's zoom-on-focus, which then
leaves the viewport scaled after blur. **This is a platform constraint, not a style
preference** — the phone band uses `15px` (or go to `16px`); do not inherit the desktop
`12.5px`.

```css
.fieldrow input, .fieldrow textarea {
  font-size: 15px;   /* <16px triggers iOS zoom-on-focus */
}
```

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
| Existing Card + Badge character idiom | `web/src/routes/(authed)/characters/+page.svelte:116-153` |
| The existing badge is **session** state, not lifecycle | same file, `:132-136` |
| Mobile drawer pattern (`Sheet` + `mobileNavOpen`) | `web/src/routes/(authed)/+layout.svelte` |
| Every color + layout token | `web/src/app.css` |
| Amber-cursor-only constraint | `.claude/rules/branding.md` INV-1 |
| Platform-brand-is-not-the-game constraint | `.claude/rules/branding.md` INV-6 |
| The game's display name (server-side only) | `internal/plugin/manifest.go:211` |
| Static adapter ⇒ every route is HTTP 200 + `index.html` | `web/svelte.config.js` |
| Normative admin registry, descriptor, gating contract | `.planning/phases/01-portal-spec/01-SPEC.md` §10 |

**`nav/sections.ts` already implements the registry pattern the SPEC says to mirror — do not
add a library for it.** It also already carries `requiresPlayer`, which is the gate the
not-found page's destination list (sketch 010, variant B) flows through — the same gate the
Rail and the command palette use, which is exactly why that list discloses nothing.

## Origin

Synthesized from sketches: **001–010**
Theme file: `sources/themes/default.css`
