<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Web Client Theme Redesign & shadcn-svelte Migration

**Status:** Draft
**Date:** 2026-04-02
**Scope:** Web client (`web/`) only — documentation site (`site/`) unchanged

## Summary

Redesign the web client's visual identity to align with the documentation site's
deep-orange/amber palette, integrate shadcn-svelte as the component library for
non-terminal UI, bump font sizes for readability, and preserve theme
choosability with 4 built-in themes plus a terminal background override.

## Goals

1. Default theme aligns visually with the documentation site (deep-orange
   primary, amber accent, warm backgrounds)
2. Themes MUST remain choosable — users pick their preferred look
3. Font sizes increase for readability (+2pt content, +1pt chrome)
4. shadcn-svelte replaces hand-rolled components for forms, buttons, dialogs,
   and structural layout — terminal content rendering stays custom
5. Theme store designed for future server-side persistence (clean serializable
   preferences object)

## Non-Goals

- Documentation site changes (Zensical stays as-is)
- Server-side player preferences storage (separate follow-up — see Future
  Considerations)
- Custom theme editor UI
- MUSH message color changes (say, pose, system, ooc, pemit colors are
  unchanged)

---

## 1. Color Palette Strategy

### Principle

Chrome (backgrounds, surfaces, borders, inputs, sidebar, status bar) and the
primary accent color shift to deep-orange/amber warmth matching the doc site's
`primary = "deep-orange"` / `accent = "amber"` palette. MUSH message-type
colors (say, pose, system, ooc, pemit, arrive, leave, command) remain unchanged
— they are functional, already well-tuned for terminal readability, and not part
of the brand identity.

### Default Dark Theme Palette

#### Chrome tokens (changed)

| Token                | Current (cyan/navy) | Proposed (warm)  |
| -------------------- | ------------------- | ---------------- |
| `background`         | `#0d0d1a`           | `#1a1210`        |
| `surface`            | `#12122a`           | `#1e1612`        |
| `border`             | `#1a1a3e`           | `#3e2a1a`        |
| `input.background`   | `#0a0a16`           | `#16100c`        |
| `input.prompt`       | `#4fc3f7`           | `#ff7043`        |
| `input.text`         | `#e0e0e0`           | `#e0d6cf`        |
| `status.text`        | `#555555`           | `#666055`        |
| `status.background`  | `#12122a`           | `#1e1612`        |
| `sidebar.background` | `#12122a`           | `#1e1612`        |
| `scrollback.indicator` | `#ffb74d`         | `#ffb74d`        |

#### MUSH tokens (unchanged)

| Token              | Value     |
| ------------------ | --------- |
| `say.speaker`      | `#4fc3f7` |
| `say.speech`       | `#ffffff` |
| `pose.actor`       | `#81c784` |
| `pose.action`      | `#aaaaaa` |
| `system`           | `#ffb74d` |
| `arrive`           | `#888888` |
| `leave`            | `#888888` |
| `command.output`    | `#e0e0e0` |
| `command.error`     | `#e57373` |
| `ooc`              | `#9575cd` |
| `pemit`            | `#80cbc4` |

### Default Light Theme Palette

Same principle — structural colors shift to warm slate/cream tones, accent from
blue (`#0277bd`) to deep-orange (`#e64a19`). MUSH message colors stay as-is
(same values as current `default-light.json`).

| Token                | Current (blue/white) | Proposed (warm)  |
| -------------------- | -------------------- | ---------------- |
| `background`         | `#fafafa`            | `#faf5f0`        |
| `surface`            | `#f0f0f0`            | `#f0ebe5`        |
| `border`             | `#e0e0e0`            | `#d4c4b0`        |
| `input.background`   | `#ffffff`            | `#fffcf8`        |
| `input.prompt`       | `#0277bd`            | `#e64a19`        |
| `input.text`         | `#1a1a1a`            | `#1a1510`        |
| `status.text`        | `#616161`            | `#6b6055`        |
| `status.background`  | `#f0f0f0`            | `#f0ebe5`        |
| `sidebar.background` | `#f5f5f5`            | `#f5f0ea`        |
| `scrollback.indicator` | `#e65100`           | `#e65100`        |

### Classic Themes

The current cyan/navy (dark) and blue/white (light) palettes are preserved as
`classic-dark` and `classic-light` for users who prefer the original look. These
use the existing color values with the new shadcn tokens added.

---

## 2. Unified Theme Schema

### Extended ThemeColors Interface

The `ThemeColors` interface in `web/src/lib/theme/types.ts` expands to include
shadcn semantic tokens alongside existing MUSH tokens. One theme file defines
everything — no split-brain theming.

#### New shadcn tokens

| Token                      | Purpose                            |
| -------------------------- | ---------------------------------- |
| `primary`                  | Main accent color (deep-orange)    |
| `primary.foreground`       | Text on primary backgrounds        |
| `secondary`                | Secondary actions                  |
| `secondary.foreground`     | Text on secondary backgrounds      |
| `muted`                    | Disabled/subtle backgrounds        |
| `muted.foreground`         | Disabled/subtle text               |
| `accent`                   | Hover highlights (amber)           |
| `accent.foreground`        | Text on accent backgrounds         |
| `destructive`              | Error/danger actions               |
| `destructive.foreground`   | Text on destructive backgrounds    |
| `card`                     | Card backgrounds                   |
| `card.foreground`          | Card text                          |
| `popover`                  | Dropdown/dialog backgrounds        |
| `popover.foreground`       | Dropdown/dialog text               |
| `ring`                     | Focus ring color                   |
| `radius`                   | Border radius value (e.g., `0.5rem`) |

#### Key mappings (default dark)

| shadcn token     | Value     | Relationship                          |
| ---------------- | --------- | ------------------------------------- |
| `primary`        | `#ff7043` | Same as `input.prompt`                |
| `primary.foreground` | `#ffffff` | White text on orange                |
| `secondary`      | `#2a1e16` | Slightly lighter than background      |
| `secondary.foreground` | `#e0d6cf` | Warm light text                  |
| `muted`          | `#2a1e16` | Subtle background                     |
| `muted.foreground` | `#666055` | Same as `status.text`              |
| `accent`         | `#ffb74d` | Same as `system` / `scrollback.indicator` |
| `accent.foreground` | `#1a1210` | Dark text on amber                 |
| `destructive`    | `#e57373` | Same as `command.error`               |
| `destructive.foreground` | `#ffffff` | White text on red            |
| `card`           | `#1e1612` | Same as `surface`                     |
| `card.foreground` | `#e0d6cf` | Same as `input.text`                |
| `popover`        | `#1e1612` | Same as `surface`                     |
| `popover.foreground` | `#e0d6cf` | Same as `input.text`             |
| `ring`           | `#ff7043` | Same as `primary`                     |
| `radius`         | `0.5rem`  | Consistent border radius              |

### Theme File Format

Each theme is a single JSON file defining all tokens:

```json
{
  "name": "default-dark",
  "colors": {
    "say.speaker": "#4fc3f7",
    "say.speech": "#ffffff",
    "...": "... (all MUSH tokens) ...",
    "background": "#1a1210",
    "surface": "#1e1612",
    "...": "... (all chrome tokens) ...",
    "primary": "#ff7043",
    "primary.foreground": "#ffffff",
    "...": "... (all shadcn tokens) ..."
  }
}
```

### Tailwind Bridge

The existing `themeToCssVars()` function converts dot-notation keys to CSS
variables (`primary.foreground` → `--color-primary-foreground`). Tailwind's CSS
config maps these to shadcn's expected variable names so components pick up the
active theme automatically.

---

## 3. Terminal Background Override

A boolean preference that overrides the terminal content area and command input
backgrounds to pure black, independent of the selected theme.

### Behavior

| Token              | Theme default (warm dark) | Black override |
| ------------------ | ------------------------- | -------------- |
| `background`       | `#1a1210`                 | `#000000`      |
| `input.background` | `#16100c`                 | `#0a0a0a`      |

Chrome (TopBar, sidebar, status bar) keeps the theme's colors. Only the
terminal content area and command input are affected.

### Implementation

A `terminalBlackBackground` boolean in the theme store, persisted to
localStorage alongside the theme ID. When enabled, the terminal container
applies override CSS variables on its own element, scoped so they don't leak to
the chrome.

This gives users 4 themes × 2 terminal modes = 8 effective combinations without
8 separate theme files.

---

## 4. shadcn-svelte Integration

### Toolchain Additions

| Package        | Purpose                         |
| -------------- | ------------------------------- |
| Tailwind CSS 4 | Utility-first CSS framework     |
| bits-ui        | Headless component primitives   |
| shadcn-svelte  | Styled component library        |

### Component Migration Plan

#### Migrate to shadcn

| Current                     | shadcn replacement          | Pages affected                           |
| --------------------------- | --------------------------- | ---------------------------------------- |
| Hand-rolled `<input>`       | `Input`                     | login, register, reset, reset/confirm    |
| Hand-rolled `<button>`      | `Button`                    | login, register, reset, landing page     |
| Hand-rolled form layouts    | `Card` + `Label`            | login, register, reset                   |
| `FeatureCard.svelte`        | `Card`                      | landing page                             |
| TopBar nav links            | `Button` variant=ghost      | all pages                                |
| Terminal container          | `Card`                      | terminal page                            |
| Scrollback area             | `ScrollArea`                | terminal page                            |
| Terminal ↔ sidebar split    | `Resizable.PaneGroup`       | terminal page                            |
| Sidebar container           | `Resizable.Pane`            | terminal page                            |
| Mobile sidebar slide-out    | `Sheet`                     | terminal page (mobile)                   |
| Theme picker                | `DropdownMenu`              | TopBar                                   |
| Future dialogs              | `Dialog`, `AlertDialog`     | as needed                                |
| Future dropdowns            | `Select`                    | as needed                                |

#### Stay custom (shadcn conventions)

These components remain custom but MUST follow shadcn conventions: use the
`cn()` utility for class composition, pull colors from the shared CSS variable
tokens, use consistent focus rings (`ring-ring`), and expose a `class` prop for
extension.

| Component                    | Reason                                           |
| ---------------------------- | ------------------------------------------------ |
| `CommandInput.svelte`        | Custom prompt, history, hints — domain-specific  |
| `CommunicationRenderer`      | Semantic MUSH message formatting                 |
| `SystemRenderer`             | System message formatting                        |
| `MovementRenderer`           | Arrival/departure formatting                     |
| `CommandRenderer`            | Command output formatting                        |
| `EventRenderer`              | Event dispatch — routes to specific renderers     |
| `AnsiRenderer`               | ANSI escape code rendering                       |
| `FallbackRenderer`           | Unknown event type fallback                      |
| `StatusBar.svelte`           | Tight terminal-specific layout                   |
| `RoomInfo.svelte`            | Game-state display                               |
| `ExitList.svelte`            | Game-state display                               |
| `PresenceList.svelte`        | Game-state display                               |

Sidebar children MAY adopt shadcn `Tooltip` and `Badge` components for
interactive elements and status indicators.

---

## 5. Typography

### Font Size Changes

Content surfaces get +2pt, chrome gets +1pt:

| Element            | Current | New   | Category      |
| ------------------ | ------- | ----- | ------------- |
| Terminal text      | 13px    | 15px  | Content (+2)  |
| Command input      | 13px    | 15px  | Content (+2)  |
| Landing hero title | 36px    | 38px  | Content (+2)  |
| Buttons            | 13px    | 14px  | Chrome (+1)   |
| Header/TopBar      | 12px    | 13px  | Chrome (+1)   |
| Sidebar content    | 11px    | 12px  | Chrome (+1)   |
| Status bar         | 11px    | 12px  | Chrome (+1)   |
| Labels/placeholders | 11px   | 12px  | Chrome (+1)   |
| Landing subtitle   | 14px    | 15px  | Chrome (+1)   |
| Separator text     | 10px    | 11px  | Chrome (+1)   |
| Badges/icons       | 8-9px   | 9-10px | Chrome (+1)  |

### Font Family Split

| Surface              | Font                                              |
| -------------------- | ------------------------------------------------- |
| Terminal + command    | `'JetBrains Mono', 'Fira Code', 'SF Mono', monospace` |
| App chrome (forms, dialogs, nav) | System sans-serif (Inter / system-ui stack) |

shadcn components use the sans-serif stack by default. Terminal rendering
continues using the monospace stack. This provides natural visual separation
between the game content and application UI.

---

## 6. Theme Switching

### Built-in Themes

| ID              | Description                                     | Default for            |
| --------------- | ----------------------------------------------- | ---------------------- |
| `default-dark`  | Warm deep-orange/amber chrome, dark background  | Dark preference        |
| `default-light` | Warm deep-orange/amber chrome, light background | Light preference       |
| `classic-dark`  | Original cyan/navy palette                      | —                      |
| `classic-light` | Original blue/white palette                     | —                      |

### Selection Logic

1. Check localStorage for saved theme ID
2. Fall back to system `prefers-color-scheme` → `default-dark` or
   `default-light`
3. No saved preference + no system preference → `default-dark`

### Theme Picker UI

A shadcn `DropdownMenu` in the TopBar showing:

- Theme name
- Small color swatch preview (background + primary + accent)
- Checkmark on active theme
- Toggle for terminal black background override

### Preferences Object

All theme-related preferences MUST be stored as a single serializable object to
simplify future server-side persistence:

```typescript
interface ThemePreferences {
  themeId: string;
  terminalBlackBackground: boolean;
}
```

Persisted to localStorage key `'holomush-theme-prefs'` (replaces the current
`'holomush-theme'` string).

---

## 7. File Changes Summary

### New files

| File                                    | Purpose                          |
| --------------------------------------- | -------------------------------- |
| `web/src/lib/theme/default-dark.json`   | Updated warm dark palette        |
| `web/src/lib/theme/default-light.json`  | Updated warm light palette       |
| `web/src/lib/theme/classic-dark.json`   | Preserved original dark palette  |
| `web/src/lib/theme/classic-light.json`  | Preserved original light palette |
| `web/src/lib/components/ui/`            | shadcn-svelte generated components |
| `web/src/app.css`                       | Tailwind base + theme variable bridge |
| `web/tailwind.config.*`                 | Tailwind configuration           |

### Modified files

| File                                     | Change                                    |
| ---------------------------------------- | ----------------------------------------- |
| `web/src/lib/theme/types.ts`            | Add shadcn tokens to ThemeColors           |
| `web/src/lib/stores/themeStore.ts`       | ThemePreferences object, black bg override |
| `web/src/lib/components/TopBar.svelte`   | shadcn Button + DropdownMenu, theme picker |
| `web/src/routes/login/+page.svelte`      | shadcn Card + Input + Button               |
| `web/src/routes/register/+page.svelte`   | shadcn Card + Input + Button               |
| `web/src/routes/reset/+page.svelte`      | shadcn Card + Input + Button               |
| `web/src/routes/reset/confirm/+page.svelte` | shadcn Card + Input + Button            |
| `web/src/routes/+page.svelte`            | shadcn Card + Button, sans-serif font      |
| `web/src/routes/+layout.svelte`          | Tailwind base, theme var injection         |
| `web/src/routes/(authed)/terminal/+page.svelte` | Card + ScrollArea + Resizable      |
| `web/src/lib/components/sidebar/Sidebar.svelte` | Resizable.Pane / Sheet (mobile)    |
| `web/src/lib/components/terminal/TerminalView.svelte` | ScrollArea wrapper           |
| `web/src/lib/components/terminal/CommandInput.svelte` | cn() utility, theme tokens   |
| `web/src/lib/components/terminal/StatusBar.svelte`    | Font size bump               |
| `web/src/lib/components/FeatureCard.svelte`           | Replace with shadcn Card     |
| `web/src/lib/components/sidebar/RoomInfo.svelte`      | Font size bump, optional Tooltip |
| `web/src/lib/components/sidebar/ExitList.svelte`      | Font size bump, optional Tooltip |
| `web/src/lib/components/sidebar/PresenceList.svelte`  | Font size bump, optional Badge   |
| `web/package.json`                       | New dependencies                           |

### Deleted files

| File                                    | Reason                                    |
| ----------------------------------------------- | --------------------------------- |
| `web/src/lib/components/FeatureCard.svelte`     | Replaced by shadcn Card           |

---

## 8. Testing Considerations

- All existing E2E tests MUST continue to pass after migration
- Form inputs MUST retain `name` attributes for Playwright testability
- Buttons that submit forms MUST have `type="submit"`
- Theme switching MUST be verifiable — E2E test that switches theme and asserts
  CSS variable values change
- Terminal black background override MUST be testable independently
- Visual regression testing is RECOMMENDED but not required for this iteration

---

## Future Considerations

### Server-Side Player Preferences

The `ThemePreferences` interface is designed as a clean serializable object
specifically to simplify future migration from localStorage to server-side
storage. The follow-up work includes:

- A gRPC `PreferencesService` for get/set/defaults
- Database schema for player preferences
- Client-side sync (localStorage as cache, server as source of truth)
- Decision on which preferences sync server-side vs stay local

This is tracked as a separate bead with this spec as context.

### Custom Theme Import

The `applyOverrides()` mechanism in the theme store already supports adding
themes at runtime. A future settings page could allow importing custom theme
JSON files. Out of scope for this work.
