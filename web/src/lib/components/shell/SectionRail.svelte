<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { Home, Clapperboard, Settings, ShieldCheck } from '@lucide/svelte';
  import type { Component } from 'svelte';
  import { visibleSections, type SectionId } from '$lib/nav/sections';
  import { uiPrefs, toggleDensity } from '$lib/stores/uiPrefsStore';
  import { themePreferences, setTerminalBlackBackground } from '$lib/stores/themeStore';
  import { adminNavSections } from '$lib/stores/adminNavStore';
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
  import { cn } from '$lib/utils';

  interface Props {
    /** Current route path; drives active state. Passed from the layout. */
    pathname: string;
    /** 'rail' = persistent desktop column; 'drawer' = mobile Sheet (shows labels). */
    variant?: 'rail' | 'drawer';
    /** True for an ephemeral guest session — hides registered-player-only sections. */
    isGuest?: boolean;
    /**
     * The roles the session response carried. Drives the single `Admin` entry
     * and nothing else. This is a NAV HINT, never a control: a viewer who
     * forges a role gets an entry leading to a route whose every RPC denies
     * them, which is why hiding it is a courtesy. `/admin` is deliberately NOT
     * a member of nav/sections.ts — a client-side mirror of the server's admin
     * registry is the drift hazard the server-side projection exists to close.
     */
    roles?: string[];
    /** Called when a section link is clicked (drawer closes itself via this). */
    onnavigate?: () => void;
  }
  let { pathname, variant = 'rail', isGuest = false, roles = [], onnavigate }: Props = $props();

  // id → icon: kept here so nav/sections.ts stays Svelte-free / node-testable.
  // Keyed by SectionId so a new section without an icon is a compile error.
  const icons: Record<SectionId, Component> = { room: Home, scenes: Clapperboard };

  // Guest sessions never see registered-player-only sections (e.g. Scenes);
  // the registry's visibleSections is the single gate the palette shares.
  let sections = $derived(visibleSections({ isGuest }));

  let showAdmin = $derived(roles.includes('admin'));
  let adminActive = $derived(pathname === '/admin' || pathname.startsWith('/admin/'));
  // Group labels are a drawer-only hierarchy device: the persistent 48px column
  // is icons-only and a label there would break its geometry. The Admin group
  // draws only when the store is non-empty, so a non-admin route's drawer
  // carries neither an Admin label nor an orphan empty group.
  let showAdminGroup = $derived(variant === 'drawer' && $adminNavSections.length > 0);
</script>

<aside
  class={cn('rail', {
    'is-drawer': variant === 'drawer',
    'is-hidden': variant === 'rail' && $uiPrefs.railHidden,
  })}
  data-testid="rail"
  aria-label="Navigation rail"
>
  <div class="rail-inner">
    {#if variant === 'drawer'}
      <span class="rail-group-label">Workspace</span>
    {/if}
    {#each sections as section (section.id)}
      {@const Icon = icons[section.id]}
      {@const active = section.match(pathname)}
      <a
        href={section.href}
        class={cn('rail-btn', { 'is-active': active })}
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

    {#if showAdmin}
      <a
        href="/admin"
        class={cn('rail-btn', { 'is-active': adminActive, 'is-context': adminActive })}
        title="Admin"
        aria-label="Admin"
        aria-current={adminActive ? 'page' : undefined}
        onclick={() => onnavigate?.()}
      >
        <ShieldCheck size={18} />
        {#if adminActive}<span class="rail-bar" aria-hidden="true"></span>{/if}
        {#if variant === 'drawer'}<span class="rail-label">Admin</span>{/if}
      </a>
    {/if}

    {#if showAdminGroup}
      <div class="rail-divider" aria-hidden="true"></div>
      <span class="rail-group-label">Admin</span>
      {#each $adminNavSections as entry (entry.id)}
        {@const active = pathname === `/admin/${entry.id}`}
        <a
          href={`/admin/${entry.id}`}
          class={cn('rail-btn', 'rail-admin-item', { 'is-active': active })}
          title={entry.displayName}
          aria-label={entry.displayName}
          aria-current={active ? 'page' : undefined}
          onclick={() => onnavigate?.()}
        >
          <span class="rail-label">{entry.displayName}</span>
        </a>
      {/each}
    {/if}

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
    font-size: 12px;
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
  /* Drawer-only hierarchy device. At <768px both columns are at width 0, so
     .is-context is unavailable and the two group labels do that work instead. */
  .rail-group-label {
    font-family: var(--font-sans, system-ui);
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--color-status-text);
    padding: 4px 10px 2px;
  }
  .rail-divider {
    height: 1px;
    align-self: stretch;
    margin: 6px 10px 2px;
    background: var(--color-border);
  }

  /* Once the admin nav has narrowed to a rail-width strip beside this column,
     the Admin entry becomes CONTEXT rather than a peer target: it keeps the
     primary tint (you ARE in Admin) but surrenders the active bar to the
     section you are actually on. Two "you are here" bars in one visual column
     at two hierarchy levels reads as a bug. Scoped INSIDE the query on
     purpose — a global rule would strip the bar at full width, where it is the
     only location signal. The 1023px literal is the shipped rail's own
     mechanism and its sibling literal, so the two collapse by construction. */
  @media (max-width: 1023px) {
    .rail:not(.is-drawer) .rail-btn.is-context {
      opacity: 0.7;
    }
    .rail:not(.is-drawer) .rail-btn.is-context .rail-bar {
      display: none;
    }
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
