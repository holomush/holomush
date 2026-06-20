<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Unified Authed Workspace Shell Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every authed web section share one persistent workspace frame — a route-aware left Rail (+ `⌘K` palette nav) and a persistent footer — so `/terminal` and `/scenes` read as one app instead of two.

**Architecture:** A new SvelteKit nested layout `web/src/routes/(authed)/+layout.svelte` owns the persistent chrome (Rail + footer) and renders each section in its slot. A declarative section registry feeds both the Rail and the palette. The terminal's footer hotkeys move into a shell-owned footer via a `footerBridge` store (mirroring the existing `composerBridge`). Mobile collapses the Rail into a `Sheet` drawer.

**Tech Stack:** SvelteKit 2, Svelte 5 runes, Tailwind v4, shadcn-svelte (`Sheet`, `DropdownMenu`), bits-ui, lucide-svelte, vitest (`mount`/`unmount`), Playwright.

**Spec:** `docs/superpowers/specs/2026-06-20-unified-authed-shell-design.md`

**Working dir:** all paths are under `web/`. Run commands from `web/` (`cd web` first).

**Scope guard:** chrome/shell only. NOT enabling future sections, terminal game logic, or scene CRUD. Per spec §9: global key handlers **stay in the root layout** (out of scope to relocate); the Rail file **is renamed** `terminal/Rail.svelte` → `shell/SectionRail.svelte`; the footer spans the section-content width.

**Test commands:** `pnpm test:unit` (vitest, both projects), `pnpm check` (svelte-check), `pnpm exec playwright test e2e/<file>` (E2E). Per-file unit run: `pnpm test:unit -- src/lib/nav/sections.test.ts`.

---

## Phase 1: Foundation stores & registry

### Task 1: Section registry (`nav/sections.ts`)

The single source of truth for Rail items and palette nav. Kept **pure** (no Svelte/icon imports) so it runs in the plain `*.test.ts` vitest project. Icons are mapped in the Rail component (Task 4).

**Files:**

- Create: `web/src/lib/nav/sections.ts`
- Test: `web/src/lib/nav/sections.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// web/src/lib/nav/sections.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { describe, expect, it } from 'vitest';
import { SECTIONS, activeSectionId, activeSectionLabel, sectionNavEntries } from './sections';

describe('section registry', () => {
  it('lists Room then Scenes with their routes', () => {
    expect(SECTIONS.map((s) => s.id)).toEqual(['room', 'scenes']);
    expect(SECTIONS.map((s) => s.href)).toEqual(['/terminal', '/scenes']);
  });
});

describe('activeSectionId uses prefix match', () => {
  it('marks Room active on /terminal', () => {
    expect(activeSectionId('/terminal')).toBe('room');
  });
  it('marks Scenes active on /scenes and nested routes', () => {
    expect(activeSectionId('/scenes')).toBe('scenes');
    expect(activeSectionId('/scenes/browse')).toBe('scenes');
    expect(activeSectionId('/scenes/01HZN3XS')).toBe('scenes');
  });
  it('does not false-match a sibling prefix', () => {
    expect(activeSectionId('/scenesfoo')).toBeNull();
  });
  it('returns null for an unregistered route', () => {
    expect(activeSectionId('/characters')).toBeNull();
  });
});

describe('activeSectionLabel', () => {
  it('returns the active section label', () => {
    expect(activeSectionLabel('/scenes/x')).toBe('Scenes');
    expect(activeSectionLabel('/characters')).toBeNull();
  });
});

describe('sectionNavEntries', () => {
  it('derives palette go-to entries from the same registry', () => {
    expect(sectionNavEntries()).toEqual([
      { id: 'nav.room', label: 'Go to Room', href: '/terminal' },
      { id: 'nav.scenes', label: 'Go to Scenes', href: '/scenes' },
    ]);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm test:unit -- src/lib/nav/sections.test.ts`
Expected: FAIL — `Cannot find module './sections'`.

- [ ] **Step 3: Write the implementation**

```ts
// web/src/lib/nav/sections.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/**
 * A workspace section reachable from the persistent Rail / palette.
 * Pure data only — the Rail (SectionRail.svelte) maps `id` to a lucide icon,
 * so this module stays free of Svelte imports and runs in the node test project.
 */
export interface WorkspaceSection {
  id: string;
  label: string;
  href: string;
  /** True when `pathname` is within this section (the route or a child of it). */
  match: (pathname: string) => boolean;
}

const prefix = (base: string) => (pathname: string) =>
  pathname === base || pathname.startsWith(base + '/');

/** Ordered registry. Add a section here (+ its route) to grow the Rail + palette. */
export const SECTIONS: WorkspaceSection[] = [
  { id: 'room', label: 'Room', href: '/terminal', match: prefix('/terminal') },
  { id: 'scenes', label: 'Scenes', href: '/scenes', match: prefix('/scenes') },
];

export function activeSectionId(pathname: string): string | null {
  return SECTIONS.find((s) => s.match(pathname))?.id ?? null;
}

export function activeSectionLabel(pathname: string): string | null {
  return SECTIONS.find((s) => s.match(pathname))?.label ?? null;
}

export interface SectionNavEntry {
  id: string;
  label: string;
  href: string;
}

/** Palette "go to <section>" entries, derived from {@link SECTIONS}. */
export function sectionNavEntries(): SectionNavEntry[] {
  return SECTIONS.map((s) => ({ id: `nav.${s.id}`, label: `Go to ${s.label}`, href: s.href }));
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `pnpm test:unit -- src/lib/nav/sections.test.ts`
Expected: PASS (all cases).

- [ ] **Step 5: Commit**

`jj commit -m "feat(web): section registry for the authed workspace shell (holomush-q41kr)"` (append the `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` byline).

---

### Task 2: Footer bridge store (`stores/footerBridge.ts`)

Lets the active section push footer content into the shell's persistent footer. Mirrors `web/src/lib/stores/composerBridge.ts:1-21`. Stores a Svelte `Snippet` so terminal-local reactive state (line count) survives the relocation.

**Files:**

- Create: `web/src/lib/stores/footerBridge.ts`
- Test: `web/src/lib/stores/footerBridge.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// web/src/lib/stores/footerBridge.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { describe, expect, it } from 'vitest';
import { get } from 'svelte/store';
import type { Snippet } from 'svelte';
import { footerContent, setFooter, clearFooter } from './footerBridge';

describe('footerBridge', () => {
  it('stores and clears a footer snippet', () => {
    const fake = (() => {}) as unknown as Snippet;
    clearFooter();
    expect(get(footerContent)).toBeNull();
    setFooter(fake);
    expect(get(footerContent)).toBe(fake);
    clearFooter();
    expect(get(footerContent)).toBeNull();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm test:unit -- src/lib/stores/footerBridge.test.ts`
Expected: FAIL — `Cannot find module './footerBridge'`.

- [ ] **Step 3: Write the implementation**

```ts
// web/src/lib/stores/footerBridge.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import type { Snippet } from 'svelte';
import { writable } from 'svelte/store';

/**
 * Content the active section renders inside the shell's persistent footer.
 * `null` => the shell renders its baseline. Mirrors composerBridge: a section
 * registers on mount and clears on destroy, so the bar never renders a dead
 * snippet.
 */
export const footerContent = writable<Snippet | null>(null);

export function setFooter(snippet: Snippet): void {
  footerContent.set(snippet);
}

export function clearFooter(): void {
  footerContent.set(null);
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `pnpm test:unit -- src/lib/stores/footerBridge.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

`jj commit -m "feat(web): footerBridge store for shell-owned section footer (holomush-q41kr)"` (+ byline).

---

### Task 3: Mobile nav store (`stores/mobileNavStore.ts`)

Transient (never persisted) open-state bridging the TopBar `☰` (root layout) to the drawer `Sheet` (authed layout).

**Files:**

- Create: `web/src/lib/stores/mobileNavStore.ts`
- Test: `web/src/lib/stores/mobileNavStore.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// web/src/lib/stores/mobileNavStore.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { beforeEach, describe, expect, it } from 'vitest';
import { get } from 'svelte/store';
import { mobileNavOpen, openMobileNav, closeMobileNav, toggleMobileNav } from './mobileNavStore';

beforeEach(() => closeMobileNav());

describe('mobileNavStore', () => {
  it('opens, closes, and toggles', () => {
    expect(get(mobileNavOpen)).toBe(false);
    openMobileNav();
    expect(get(mobileNavOpen)).toBe(true);
    toggleMobileNav();
    expect(get(mobileNavOpen)).toBe(false);
    toggleMobileNav();
    expect(get(mobileNavOpen)).toBe(true);
    closeMobileNav();
    expect(get(mobileNavOpen)).toBe(false);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm test:unit -- src/lib/stores/mobileNavStore.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Write the implementation**

```ts
// web/src/lib/stores/mobileNavStore.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { writable } from 'svelte/store';

/** Open-state for the mobile nav drawer. Transient — intentionally NOT persisted. */
export const mobileNavOpen = writable(false);

export const openMobileNav = () => mobileNavOpen.set(true);
export const closeMobileNav = () => mobileNavOpen.set(false);
export const toggleMobileNav = () => mobileNavOpen.update((v) => !v);
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `pnpm test:unit -- src/lib/stores/mobileNavStore.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

`jj commit -m "feat(web): transient mobileNav store for drawer toggle (holomush-q41kr)"` (+ byline).

---

## Phase 2: Shell components

### Task 4: SectionRail component (`components/shell/SectionRail.svelte`)

Renders the registry as real `<a>` links with route-driven active state. Pure: takes `pathname` as a prop (no `$app/stores`), so it unit-tests via `mount`. Supersedes `terminal/Rail.svelte` (deleted in Task 10). Styles adapted from `terminal/Rail.svelte:105-186`; theme submenu dropped (dedup → TopBar), `⚙` keeps view-prefs only.

**Files:**

- Create: `web/src/lib/components/shell/SectionRail.svelte`
- Test: `web/src/lib/components/shell/SectionRail.svelte.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// web/src/lib/components/shell/SectionRail.svelte.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { afterEach, describe, expect, it } from 'vitest';
import { mount, unmount } from 'svelte';
import SectionRail from './SectionRail.svelte';

function render(props: { pathname: string; variant?: 'rail' | 'drawer' }) {
  const target = document.createElement('div');
  document.body.appendChild(target);
  const component = mount(SectionRail, { target, props });
  return { target, component };
}

afterEach(() => document.body.replaceChildren());

describe('SectionRail', () => {
  it('renders one link per registry section, in order, to its href', () => {
    const { target, component } = render({ pathname: '/terminal' });
    const hrefs = [...target.querySelectorAll('a.rail-btn')].map((a) => a.getAttribute('href'));
    expect(hrefs).toEqual(['/terminal', '/scenes']);
    unmount(component);
  });

  it('marks the active section from the pathname (prefix match)', () => {
    const { target, component } = render({ pathname: '/scenes/01HZN' });
    const active = target.querySelector('a.rail-btn.is-active');
    expect(active?.getAttribute('href')).toBe('/scenes');
    expect(active?.getAttribute('aria-current')).toBe('page');
    unmount(component);
  });

  it('shows text labels in the drawer variant', () => {
    const { target, component } = render({ pathname: '/terminal', variant: 'drawer' });
    expect(target.textContent).toContain('Room');
    expect(target.textContent).toContain('Scenes');
    unmount(component);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm test:unit -- src/lib/components/shell/SectionRail.svelte.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Write the implementation**

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { Home, Clapperboard, Settings } from '@lucide/svelte';
  import type { Component } from 'svelte';
  import { SECTIONS } from '$lib/nav/sections';
  import { uiPrefs, toggleDensity } from '$lib/stores/uiPrefsStore';
  import { themePreferences, setTerminalBlackBackground } from '$lib/stores/themeStore';
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu';

  interface Props {
    /** Current route path; drives active state. Passed from the layout. */
    pathname: string;
    /** 'rail' = persistent desktop column; 'drawer' = mobile Sheet (shows labels). */
    variant?: 'rail' | 'drawer';
    /** Called when a section link is clicked (drawer closes itself via this). */
    onnavigate?: () => void;
  }
  let { pathname, variant = 'rail', onnavigate }: Props = $props();

  // id → icon: kept here so nav/sections.ts stays Svelte-free / node-testable.
  const icons: Record<string, Component> = { room: Home, scenes: Clapperboard };
</script>

<aside
  class="rail"
  class:is-drawer={variant === 'drawer'}
  class:is-hidden={variant === 'rail' && $uiPrefs.railHidden}
  data-testid="rail"
  aria-label="Navigation rail"
>
  <div class="rail-inner">
    {#each SECTIONS as section (section.id)}
      {@const Icon = icons[section.id]}
      {@const active = section.match(pathname)}
      <a
        href={section.href}
        class="rail-btn"
        class:is-active={active}
        title={section.label}
        aria-label={section.label}
        aria-current={active ? 'page' : undefined}
        onclick={() => onnavigate?.()}
      >
        <Icon size={18} />
        {#if active}<span class="rail-bar" aria-hidden="true"></span>{/if}
        {#if variant === 'drawer'}<span class="rail-label">{section.label}</span>{/if}
      </a>
    {/each}

    <div class="rail-spacer"></div>

    <DropdownMenu.Root>
      <DropdownMenu.Trigger>
        {#snippet child({ props })}
          <button {...props} class="rail-btn" title="View preferences" aria-label="View preferences">
            <Settings size={18} />
            {#if variant === 'drawer'}<span class="rail-label">Settings</span>{/if}
          </button>
        {/snippet}
      </DropdownMenu.Trigger>
      <DropdownMenu.Content align="end" side="right" class="w-56">
        <DropdownMenu.Label>Density</DropdownMenu.Label>
        <DropdownMenu.CheckboxItem
          checked={$uiPrefs.density === 'compact'}
          onCheckedChange={() => toggleDensity()}
        >
          Compact
        </DropdownMenu.CheckboxItem>
        <DropdownMenu.Separator />
        <DropdownMenu.CheckboxItem
          checked={$themePreferences.terminalBlackBackground}
          onCheckedChange={(v) => setTerminalBlackBackground(v === true)}
        >
          Black terminal background
        </DropdownMenu.CheckboxItem>
      </DropdownMenu.Content>
    </DropdownMenu.Root>

    {#if variant === 'rail'}
      <div class="rail-hint" aria-hidden="true"><kbd>⌘</kbd><kbd>B</kbd></div>
    {/if}
  </div>
</aside>

<style>
  .rail {
    width: var(--rail-w);
    flex-shrink: 0;
    overflow: hidden;
    background: var(--color-sidebar-background);
    border-right: 1px solid var(--color-border);
    transition: width 180ms ease;
  }
  .rail.is-drawer {
    width: 100%;
    border-right: none;
  }
  .rail.is-hidden {
    width: 0;
    border-right-width: 0;
  }
  /* Persistent desktop rail collapses on small screens; the drawer is exempt. */
  @media (max-width: 767px) {
    .rail:not(.is-drawer) {
      width: 0;
      border-right-width: 0;
    }
  }
  .rail-inner {
    display: flex;
    flex-direction: column;
    align-items: center;
    padding: 6px 0 4px;
    height: 100%;
    gap: 4px;
  }
  .rail.is-drawer .rail-inner {
    align-items: stretch;
    padding: 10px 8px;
    gap: 6px;
  }
  .rail-btn {
    width: 36px;
    height: 36px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: none;
    border: none;
    border-radius: 6px;
    cursor: pointer;
    color: var(--color-status-text);
    text-decoration: none;
    position: relative;
    transition: background 120ms, color 120ms;
  }
  .rail.is-drawer .rail-btn {
    width: 100%;
    justify-content: flex-start;
    gap: 10px;
    padding: 0 10px;
  }
  .rail-label {
    font-family: var(--font-sans, system-ui);
    font-size: 13px;
  }
  .rail-btn:hover {
    background: color-mix(in srgb, var(--color-primary) 10%, transparent);
    color: var(--color-input-text);
  }
  .rail-btn.is-active {
    color: var(--color-primary);
  }
  .rail-btn.is-active .rail-bar {
    position: absolute;
    left: -6px;
    top: 6px;
    bottom: 6px;
    width: 2px;
    background: var(--color-primary);
    border-radius: 1px;
  }
  .rail.is-drawer .rail-btn.is-active {
    background: color-mix(in srgb, var(--color-primary) 12%, transparent);
  }
  .rail-spacer { flex: 1; }
  .rail-hint { margin: 4px 0; }
  .rail-hint kbd {
    display: inline-block;
    padding: 1px 3px;
    font-size: 9px;
    border: 1px solid var(--color-border);
    border-radius: 3px;
    color: var(--color-status-text);
  }
  .rail-hint kbd + kbd { margin-left: 1px; }
</style>
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `pnpm test:unit -- src/lib/components/shell/SectionRail.svelte.test.ts`
Expected: PASS (3 cases). If `DropdownMenu` mounting warns under jsdom, the assertions still pass (they target `a.rail-btn`); only investigate on a hard failure.

- [ ] **Step 5: Commit**

`jj commit -m "feat(web): SectionRail — registry-driven, route-aware section switcher (holomush-q41kr)"` (+ byline).

---

### Task 5: ShellFooter component (`components/shell/ShellFooter.svelte`)

Persistent footer bar. Renders the active section's registered snippet, else the baseline (section name + `⌘K go to…` + connection dot). Pure: takes `pathname`.

**Files:**

- Create: `web/src/lib/components/shell/ShellFooter.svelte`
- Test: `web/src/lib/components/shell/ShellFooter.svelte.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// web/src/lib/components/shell/ShellFooter.svelte.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { afterEach, describe, expect, it } from 'vitest';
import { mount, unmount } from 'svelte';
import { clearFooter } from '$lib/stores/footerBridge';
import ShellFooter from './ShellFooter.svelte';

afterEach(() => {
  clearFooter();
  document.body.replaceChildren();
});

describe('ShellFooter baseline', () => {
  it('renders the active section name and a go-to hint when nothing is registered', () => {
    clearFooter();
    const target = document.createElement('div');
    document.body.appendChild(target);
    const component = mount(ShellFooter, { target, props: { pathname: '/scenes' } });
    expect(target.textContent).toContain('Scenes');
    expect(target.textContent?.toLowerCase()).toContain('go to');
    unmount(component);
  });

  it('falls back to a generic label off any registered section', () => {
    clearFooter();
    const target = document.createElement('div');
    document.body.appendChild(target);
    const component = mount(ShellFooter, { target, props: { pathname: '/characters' } });
    expect(target.textContent).toContain('Workspace');
    unmount(component);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm test:unit -- src/lib/components/shell/ShellFooter.svelte.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Write the implementation**

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { footerContent } from '$lib/stores/footerBridge';
  import { connectionStatus } from '$lib/stores/connectionStore';
  import { activeSectionLabel } from '$lib/nav/sections';

  interface Props {
    /** Current route path; selects the baseline section label. */
    pathname: string;
  }
  let { pathname }: Props = $props();

  let label = $derived(activeSectionLabel(pathname) ?? 'Workspace');
</script>

<div class="shell-footer" data-testid="shell-footer">
  {#if $footerContent}
    {@render $footerContent()}
  {:else}
    <span class="sf-section">{label}</span>
    <span class="sf-grow"></span>
    <span class="sf-hint"><kbd>⌘K</kbd> go to…</span>
    <span class="sf-conn" data-status={$connectionStatus} title="Connection">
      <span class="sf-dot" aria-hidden="true"></span>
      {#if $connectionStatus === 'connected'}connected{:else if $connectionStatus === 'syncing'}syncing{:else}offline{/if}
    </span>
  {/if}
</div>

<style>
  .shell-footer {
    flex-shrink: 0;
    min-height: 26px;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 0 12px;
    background: var(--color-background);
    border-top: 1px solid var(--color-border);
    color: var(--color-status-text);
    font-size: 12px;
  }
  .sf-grow { flex: 1; }
  .sf-hint kbd {
    font-family: inherit;
    padding: 1px 5px;
    border: 1px solid var(--color-border);
    border-radius: 3px;
    font-size: 11px;
  }
  .sf-conn { display: inline-flex; align-items: center; gap: 5px; font-size: 11px; }
  .sf-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--color-status-text); }
  .sf-conn[data-status='connected'] .sf-dot { background: var(--color-status-online); }
  .sf-conn[data-status='syncing'] .sf-dot { background: var(--color-accent); }
  .sf-conn[data-status='disconnected'] .sf-dot { background: var(--color-status-offline); }
</style>
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `pnpm test:unit -- src/lib/components/shell/ShellFooter.svelte.test.ts`
Expected: PASS (2 cases).

- [ ] **Step 5: Commit**

`jj commit -m "feat(web): ShellFooter — persistent bar with section-filled slot + baseline (holomush-q41kr)"` (+ byline).

---

## Phase 3: Assemble the shell & migrate the terminal

### Task 6: Authed shell layout + terminal switch

Create the persistent shell and switch the terminal page off its own Rail/height in the **same task** (avoids a double-rail intermediate). After this, `/terminal` and `/scenes` both show the Rail + footer (terminal footer shows the baseline until Task 7 registers its hotkeys).

**Files:**

- Create: `web/src/routes/(authed)/+layout.svelte`
- Modify: `web/src/routes/(authed)/terminal/+page.svelte` (remove `import Rail` `:30`; remove `<Rail />` `:701`; height CSS `:731`, `:753-756`)

- [ ] **Step 1: Create the shell layout**

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { page } from '$app/stores';
  import SectionRail from '$lib/components/shell/SectionRail.svelte';
  import ShellFooter from '$lib/components/shell/ShellFooter.svelte';
  import { Sheet, SheetContent, SheetTitle, SheetDescription } from '$lib/components/ui/sheet';
  import { mobileNavOpen, openMobileNav, closeMobileNav } from '$lib/stores/mobileNavStore';

  let { children } = $props();
  let pathname = $derived($page.url.pathname);
</script>

<div class="shell">
  <SectionRail {pathname} variant="rail" />
  <div class="section-col">
    <div class="section-slot">{@render children()}</div>
    <ShellFooter {pathname} />
  </div>
</div>

<!-- Mobile drawer: same Rail, controlled by the shared store (controlled mode
     per holomush-ceon — do not bind:open through a store expression). -->
<Sheet open={$mobileNavOpen} onOpenChange={(o: boolean) => (o ? openMobileNav() : closeMobileNav())}>
  <SheetContent side="left" class="p-0 w-[260px]">
    <SheetTitle class="sr-only">Navigation</SheetTitle>
    <SheetDescription class="sr-only">Switch workspace section</SheetDescription>
    <SectionRail {pathname} variant="drawer" onnavigate={closeMobileNav} />
  </SheetContent>
</Sheet>

<style>
  .shell {
    display: flex;
    height: calc(100vh - var(--topbar-h));
    min-height: 0;
  }
  .section-col {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }
  .section-slot {
    flex: 1;
    min-height: 0;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
</style>
```

> The `class="sr-only"` on `SheetTitle`/`SheetDescription` uses Tailwind v4's
> built-in `sr-only` utility — do **not** redeclare it in this component's
> `<style>` (avoids the duplicate the plan-reviewer flagged).

- [ ] **Step 2: Remove the Rail import from the terminal page**

In `web/src/routes/(authed)/terminal/+page.svelte`, delete line 30:

```ts
  import Rail from '$lib/components/terminal/Rail.svelte';
```

- [ ] **Step 3: Remove the `<Rail />` render**

In the same file, delete the `<Rail />` line (currently `:701`) so the markup begins:

```svelte
  <div class="terminal-layout" style={$themePreferences.terminalBlackBackground ? terminalBlackOverrideVars() : ''}>
    <div class="main-area" bind:this={paneGroupEl}>
```

- [ ] **Step 4: Shed the page's viewport-height CSS**

The shell now owns viewport height (`§3.1`). Change both rules so the page fills its container instead of recomputing the viewport:

```css
  /* .login-screen — was: height: calc(100vh - var(--topbar-h)); */
  .login-screen {
    height: 100%;
    /* …rest unchanged… */
  }

  /* .terminal-layout — was: height: calc(100vh - var(--topbar-h)); */
  .terminal-layout {
    height: 100%;
    /* …rest unchanged… */
  }
```

- [ ] **Step 5: Type-check and eyeball both sections**

Run: `pnpm check`
Expected: no errors (no remaining `Rail` reference).
Run: `pnpm dev`, visit `/terminal` and `/scenes` — both show the left Rail + a bottom footer; the active Rail item matches the route; terminal stream/sidebar/input still work. (Terminal footer shows the baseline for now — fixed in Task 7.)

- [ ] **Step 6: Commit**

`jj commit -m "feat(web): shared (authed) shell — Rail + footer + mobile drawer; terminal uses it (holomush-q41kr)"` (+ byline).

---

### Task 7: Move the terminal hotkey bar into the shell footer

Relocate `CommandInput`'s `.cmd-hints` (`CommandInput.svelte:187-199`) into a snippet registered via `footerBridge`. The snippet closes over `lineCount` / `nearMax` / `$uiPrefs`, so the live line-count feedback is preserved.

**Files:**

- Modify: `web/src/lib/components/terminal/CommandInput.svelte` (import bridge; wrap hints in `{#snippet}`; register/clear; remove inline render)

- [ ] **Step 1: Import the bridge**

Add to the import block:

```ts
  import { setFooter, clearFooter } from '$lib/stores/footerBridge';
```

- [ ] **Step 2: Register the hints snippet (and clear on destroy)**

Add an effect in the `<script>` (after the existing `onDestroy`):

```ts
  // Publish the terminal hotkey bar into the shell footer while mounted.
  $effect(() => {
    setFooter(cmdHints);
    return () => clearFooter();
  });
```

- [ ] **Step 3: Convert the inline hint bar into a snippet**

Replace the existing `<div class="cmd-hints"> … </div>` block (`:187-199`) with a snippet of the same markup (so it renders inside `ShellFooter` instead of under the input):

```svelte
{#snippet cmdHints()}
  <div class="cmd-hints">
    <span><kbd>↑↓</kbd> history</span>
    <span><kbd>⇧⏎</kbd> newline</span>
    <span><kbd>Esc</kbd> clear</span>
    <span><kbd>⌘K</kbd> palette</span>
    <span><kbd>⌘B</kbd> rail</span>
    <span><kbd>⌘.</kbd> sidebar</span>
    <span><kbd>⌘⇧E</kbd> composer</span>
    <span class="line-count">{lineCount} line{lineCount === 1 ? '' : 's'}</span>
    {#if nearMax && !$uiPrefs.composerOpen}
      <span class="composer-nudge">Press ⌘⇧E for a bigger editor</span>
    {/if}
  </div>
{/snippet}
```

Leave the `.cmd-hints` styles in this component's `<style>` — Svelte 5 applies the declaring component's scoped styles to a snippet wherever it is `{@render}`ed. **Verify in Step 5;** if the bar renders unstyled inside the footer, lift the `.cmd-hints` rules to `:global(.cmd-hints)` in this file.

- [ ] **Step 4: Run unit tests**

Run: `pnpm test:unit -- src/lib/stores/footerBridge.test.ts`
Expected: PASS (bridge unchanged). Run `pnpm check` — no errors.

- [ ] **Step 5: Manual verify**

`pnpm dev` → `/terminal`: the hotkey bar (`↑↓ history … ⌘⇧E composer`, line count) now renders in the bottom shell footer and updates as you type. Navigate to `/scenes`: footer reverts to the baseline. Back to `/terminal`: hotkeys return.

- [ ] **Step 6: Commit**

`jj commit -m "feat(web): relocate terminal hotkey bar into the shell footer via footerBridge (holomush-q41kr)"` (+ byline).

---

## Phase 4: Chrome wiring

### Task 8: TopBar — mobile drawer toggle + drop redundant Scenes icon

**Files:**

- Modify: `web/src/lib/components/TopBar.svelte` (lucide import `:6`; add `☰`; remove Clapperboard link `:140-142`)

- [ ] **Step 1: Update the lucide import**

Change line 6 — drop `Clapperboard`, add `Menu`:

```ts
  import { LogOut, ArrowLeftRight, Palette, PanelRightOpen, Command as CommandIcon, Menu } from '@lucide/svelte';
```

- [ ] **Step 2: Import the mobile-nav action and derive auth state**

Add to the script:

```ts
  import { toggleMobileNav } from '$lib/stores/mobileNavStore';
  let isAuthed = $derived($authState.isPlayerAuthenticated || !!$authState.sessionId);
```

- [ ] **Step 3: Add the `☰` button (mobile-only, authed) at the start of `.left`**

Just inside `<div class="left">`, before the logo `<a>`:

```svelte
    {#if isAuthed}
      <button class="icon-btn mobile-only" onclick={toggleMobileNav} title="Menu" aria-label="Open navigation">
        <Menu size={18} />
      </button>
    {/if}
```

- [ ] **Step 4: Remove the now-redundant Scenes link**

Delete the Clapperboard anchor (`:140-142`):

```svelte
      <a href="/scenes" class="icon-btn" title="Scenes" aria-label="Scenes">
        <Clapperboard size={16} />
      </a>
```

(The Rail's Scenes item replaces it.)

- [ ] **Step 5: Add the `mobile-only` style**

In the `<style>` block:

```css
  .mobile-only { display: inline-flex; }
  @media (min-width: 768px) {
    .mobile-only { display: none; }
  }
```

- [ ] **Step 6: Verify**

Run: `pnpm check` (no unused `Clapperboard`). `pnpm dev`: at desktop width no `☰`; at <768px the `☰` appears and opens the drawer; the old Scenes header icon is gone.

- [ ] **Step 7: Commit**

`jj commit -m "feat(web): TopBar mobile drawer toggle; drop redundant Scenes icon (holomush-q41kr)"` (+ byline).

---

### Task 9: Palette — "Go to <section>" entries from the registry

**Files:**

- Modify: `web/src/lib/components/terminal/CommandPalette.svelte` (import registry `:26`-area; prepend nav items to `items` `:43`)

- [ ] **Step 1: Import the registry helper**

Add to the imports:

```ts
  import { sectionNavEntries } from '$lib/nav/sections';
```

- [ ] **Step 2: Build nav items and prepend them to `items`**

Just above `const items: PaletteItem[] = [`:

```ts
  const navItems: PaletteItem[] = sectionNavEntries().map((e) => ({
    id: e.id,
    label: e.label,
    run: () => goto(e.href),
  }));
```

Then change the array head to spread them first:

```ts
  const items: PaletteItem[] = [
    ...navItems,
    { id: 'theme.default-dark', label: 'Switch theme: Default Dark', run: () => setTheme('default-dark') },
    // …rest unchanged…
  ];
```

- [ ] **Step 3: Verify**

Run: `pnpm check`. `pnpm dev` → press `⌘K`, type "Go to" → "Go to Room" / "Go to Scenes" appear; selecting "Go to Scenes" navigates to `/scenes`. (Asserted in the E2E task.)

- [ ] **Step 4: Commit**

`jj commit -m "feat(web): palette go-to-section entries sourced from the registry (holomush-q41kr)"` (+ byline).

---

## Phase 5: Cleanup

### Task 10: Delete the old Rail; retire `RailView`

**Files:**

- Delete: `web/src/lib/components/terminal/Rail.svelte`
- Modify: `web/src/lib/stores/uiPrefsStore.ts` (`RailView` `:7`; `railView` field `:18`,`:36`)

- [ ] **Step 1: Confirm nothing imports the old Rail or `railView`**

Run: `rg -n "terminal/Rail\.svelte|railView|RailView" web/src`
Expected: only the definitions in `uiPrefsStore.ts` (the terminal page stopped importing Rail in Task 6; SectionRail uses `railHidden`, not `railView`). If any other consumer appears, stop and reassess.

- [ ] **Step 2: Delete the file**

```bash
rm web/src/lib/components/terminal/Rail.svelte
```

- [ ] **Step 3: Remove `RailView` and the `railView` field**

In `uiPrefsStore.ts`: delete the `export type RailView = 'room';` line (`:7`), the `railView: RailView;` field in `UiPrefs` (`:18`), and `railView: 'room',` in `DEFAULTS` (`:36`). `hydrateUiPrefs`' `...parsed` tolerates a stale `railView` key in older localStorage (ignored), so no migration is needed.

- [ ] **Step 4: Verify**

Run: `pnpm check` (no dangling references). Run: `pnpm test:unit` (full suite green).

- [ ] **Step 5: Commit**

`jj commit -m "refactor(web): remove superseded Rail.svelte and vestigial RailView (holomush-q41kr)"` (+ byline).

---

## Phase 6: End-to-end verification

### Task 11: E2E — frame persistence, active state, palette nav, mobile drawer

Promote the local `registerAndEnterTerminal` helper to a shared fixture (it is copy-used 8× in `scenes.spec.ts`), then add the new spec.

**Files:**

- Modify: `web/e2e/helpers/fixtures.ts` (export `registerAndEnterTerminal` + `uniqueSceneUser`, moved from `scenes.spec.ts:217-…`)
- Modify: `web/e2e/scenes.spec.ts` (import the helpers from `./helpers/fixtures` instead of the local definitions)
- Create: `web/e2e/authed-shell.spec.ts`

- [ ] **Step 1: Extract the helper to the shared fixture**

Move the `uniqueSceneUser(...)` and `registerAndEnterTerminal(page, prefix)` function definitions verbatim from `web/e2e/scenes.spec.ts` (currently at `:217`) into `web/e2e/helpers/fixtures.ts`, add `export` to each, and import them back into `scenes.spec.ts`:

```ts
// scenes.spec.ts — replace the local defs with:
import { test, expect, db, getClientSessionId, registerAndEnterTerminal, uniqueSceneUser } from './helpers/fixtures';
```

Keep the prefix constraint (memory): prefixes MUST be ≤4 chars, alphanumeric, no hyphen.

- [ ] **Step 2: Verify the extraction is behavior-neutral**

Run: `pnpm exec playwright test e2e/scenes.spec.ts`
Expected: PASS — same as before the move.

- [ ] **Step 3: Write the new shell spec**

```ts
// web/e2e/authed-shell.spec.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { test, expect, registerAndEnterTerminal } from './helpers/fixtures';

test.describe('unified authed shell', () => {
  test('rail + footer persist across /terminal and /scenes with correct active state', async ({ page }) => {
    await registerAndEnterTerminal(page, 'shl');

    const rail = page.getByTestId('rail').first();
    await expect(rail).toBeVisible();
    await expect(page.getByTestId('shell-footer')).toBeVisible();
    // Room active on the terminal.
    await expect(rail.getByRole('link', { name: 'Room' })).toHaveAttribute('aria-current', 'page');

    // Navigate to Scenes via the rail.
    await rail.getByRole('link', { name: 'Scenes' }).click();
    await expect(page).toHaveURL(/\/scenes/);
    await expect(page.getByTestId('rail').first()).toBeVisible();
    await expect(page.getByTestId('shell-footer')).toBeVisible();
    await expect(page.getByTestId('rail').first().getByRole('link', { name: 'Scenes' }))
      .toHaveAttribute('aria-current', 'page');
  });

  test('command palette navigates between sections', async ({ page }) => {
    await registerAndEnterTerminal(page, 'pal');
    await page.keyboard.press('Meta+k');
    await page.getByPlaceholder('Type a command…').fill('Go to Scenes');
    await page.getByRole('option', { name: 'Go to Scenes' }).click();
    await expect(page).toHaveURL(/\/scenes/);
  });

  test('mobile: hamburger opens the drawer and navigates', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await registerAndEnterTerminal(page, 'mnv');

    // Persistent rail is collapsed on mobile.
    await expect(page.getByRole('button', { name: 'Open navigation' })).toBeVisible();
    await page.getByRole('button', { name: 'Open navigation' }).click();

    // The drawer is a dialog (labelled by its SheetTitle "Navigation").
    const drawer = page.getByRole('dialog', { name: 'Navigation' });
    await expect(drawer).toBeVisible();
    // Click the Scenes link INSIDE the drawer (the persistent rail also has a
    // Scenes link in the DOM at mobile width — width:0/hidden — so scope to the
    // drawer; do NOT assert a global link count).
    await drawer.getByRole('link', { name: 'Scenes' }).click();
    await expect(page).toHaveURL(/\/scenes/);
    await expect(drawer).toHaveCount(0); // drawer unmounted on navigate
  });

  test('terminal hotkey bar renders in the shell footer (no regression)', async ({ page }) => {
    await registerAndEnterTerminal(page, 'ftr');
    const footer = page.getByTestId('shell-footer');
    await expect(footer).toContainText('history');
    await expect(footer).toContainText('composer');
  });
});
```

- [ ] **Step 4: Run the new spec**

Run: `pnpm exec playwright test e2e/authed-shell.spec.ts`
Expected: PASS (4 tests). If the palette option role differs, inspect with `--debug`; the bits-ui `Command.Item` renders as `[data-command-item]` — adjust the selector to `page.locator('[data-command-item]', { hasText: 'Go to Scenes' })` if `getByRole('option')` does not resolve.

- [ ] **Step 5: Full gate**

Run: `pnpm check && pnpm test:unit && pnpm exec playwright test e2e/terminal.spec.ts e2e/scenes.spec.ts e2e/authed-shell.spec.ts`
Expected: all green (no terminal/scenes regression).

- [ ] **Step 6: Commit**

`jj commit -m "test(web): e2e for unified authed shell — frame, active state, palette nav, mobile drawer (holomush-q41kr)"` (+ byline).

---

## Done criteria (maps to spec §7 + bead acceptance)

- Navigating `/terminal` ↔ `/scenes` (and nested `/scenes/*`) preserves the Rail + footer; active item tracks the route (Task 6, 11).
- Rail is the registry-driven section switcher; palette mirrors it (Task 1, 4, 9).
- Persistent footer: terminal hotkeys via bridge, baseline elsewhere (Task 5, 7).
- Mobile drawer reuses the Rail (Task 6, 8, 11).
- No terminal regression; `pnpm check` + unit + E2E green (Task 11 Step 5).
- Chrome dedup: theme only in TopBar; redundant Scenes icon removed; `RailView` retired (Task 4, 8, 10).
<!-- adr-capture: sha256=82666128a5ccca9c; session=3b5de5d0; ts=2026-06-20T13:53:43Z; adrs=holomush-stds8,holomush-828tt,holomush-xhz3s,holomush-8p5rx -->
