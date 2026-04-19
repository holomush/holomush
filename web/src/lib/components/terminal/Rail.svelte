<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { Home, MessageSquare, Map, NotebookPen, Settings } from 'lucide-svelte';
  import { uiPrefs, toggleDensity } from '$lib/stores/uiPrefsStore';
  import {
    themePreferences,
    setTheme,
    setTerminalBlackBackground,
    getAvailableThemes,
  } from '$lib/stores/themeStore';
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu';

  const availableThemes = getAvailableThemes();
  let themeId = $derived($themePreferences.themeId);

  function displayName(id: string): string {
    return id
      .split('-')
      .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
      .join(' ');
  }
</script>

<aside
  class="rail"
  class:is-hidden={$uiPrefs.railHidden}
  data-testid="rail"
  aria-label="Navigation rail"
>
  <div class="rail-inner">
    <button class="rail-btn is-active" title="Room" aria-label="Room" aria-current="true">
      <Home size={18} />
      <span class="rail-bar" aria-hidden="true"></span>
    </button>
    <button
      class="rail-btn is-disabled"
      title="DM (coming soon)"
      aria-label="DM — coming soon"
      aria-disabled="true"
    >
      <MessageSquare size={18} />
    </button>
    <button
      class="rail-btn is-disabled"
      title="Map (coming soon)"
      aria-label="Map — coming soon"
      aria-disabled="true"
    >
      <Map size={18} />
    </button>
    <button
      class="rail-btn is-disabled"
      title="Notes (coming soon)"
      aria-label="Notes — coming soon"
      aria-disabled="true"
    >
      <NotebookPen size={18} />
    </button>

    <div class="rail-spacer"></div>

    <DropdownMenu.Root>
      <DropdownMenu.Trigger>
        {#snippet child({ props })}
          <button {...props} class="rail-btn" title="Settings" aria-label="Settings">
            <Settings size={18} />
          </button>
        {/snippet}
      </DropdownMenu.Trigger>
      <DropdownMenu.Content align="end" side="right" class="w-56">
        <DropdownMenu.Label>Theme</DropdownMenu.Label>
        <DropdownMenu.Separator />
        <DropdownMenu.RadioGroup value={themeId} onValueChange={(v) => v && setTheme(v)}>
          {#each availableThemes as theme (theme.id)}
            <DropdownMenu.RadioItem value={theme.id}>{displayName(theme.id)}</DropdownMenu.RadioItem>
          {/each}
        </DropdownMenu.RadioGroup>
        <DropdownMenu.Separator />
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

    <div class="rail-hint" aria-hidden="true">
      <kbd>⌘</kbd><kbd>B</kbd>
    </div>
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
  .rail.is-hidden {
    width: 0;
    border-right-width: 0;
  }
  @media (max-width: 767px) {
    .rail:not(.is-hidden) {
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
    position: relative;
    transition:
      background 120ms,
      color 120ms;
  }
  .rail-btn:hover:not(.is-disabled) {
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
  .rail-btn.is-disabled {
    opacity: 0.35;
    cursor: not-allowed;
  }
  .rail-spacer {
    flex: 1;
  }
  .rail-hint {
    margin-top: 4px;
    margin-bottom: 4px;
  }
  .rail-hint kbd {
    display: inline-block;
    padding: 1px 3px;
    font-size: 9px;
    border: 1px solid var(--color-border);
    border-radius: 3px;
    color: var(--color-status-text);
  }
  .rail-hint kbd + kbd {
    margin-left: 1px;
  }
</style>
