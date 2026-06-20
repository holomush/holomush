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
