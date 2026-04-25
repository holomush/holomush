<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { Command } from 'cmdk-sv';
  import {
    uiPrefs,
    openPalette,
    closePalette,
    toggleRail,
    toggleSidebar,
    toggleComposer,
    toggleDensity,
  } from '$lib/stores/uiPrefsStore';
  import { clearLines } from '$lib/stores/terminalStore';
  import {
    themePreferences,
    setTheme,
    setTerminalBlackBackground,
  } from '$lib/stores/themeStore';
  import { clearAuth } from '$lib/stores/authStore';
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { goto } from '$app/navigation';

  const client = createClient(WebService, transport);

  interface PaletteItem {
    id: string;
    label: string;
    hint?: string;
    run: () => void | Promise<void>;
  }

  async function signOut() {
    try { await client.webLogout({}); } catch { /* best effort */ }
    clearAuth();
    goto('/');
  }

  const items: PaletteItem[] = [
    { id: 'theme.default-dark',   label: 'Switch theme: Default Dark',   run: () => setTheme('default-dark') },
    { id: 'theme.default-light',  label: 'Switch theme: Default Light',  run: () => setTheme('default-light') },
    { id: 'theme.classic-dark',   label: 'Switch theme: Classic Dark',   run: () => setTheme('classic-dark') },
    { id: 'theme.classic-light',  label: 'Switch theme: Classic Light',  run: () => setTheme('classic-light') },
    { id: 'ui.rail',              label: 'Toggle rail',                  hint: '⌘B',  run: toggleRail },
    { id: 'ui.sidebar',           label: 'Toggle sidebar',               hint: '⌘.',  run: toggleSidebar },
    { id: 'ui.composer',          label: 'Toggle composer',              hint: '⌘⇧E', run: toggleComposer },
    { id: 'ui.density',           label: 'Toggle density (cozy/compact)',               run: toggleDensity },
    { id: 'ui.term-black',        label: 'Toggle black terminal background',            run: () => setTerminalBlackBackground(!$themePreferences.terminalBlackBackground) },
    { id: 'term.clear',           label: 'Clear terminal',               hint: '⌘L',  run: clearLines },
    { id: 'auth.sign-out',        label: 'Sign out',                                   run: signOut },
  ];

  function runAndClose(item: PaletteItem) {
    item.run();
    closePalette();
  }
</script>

<!--
  Controlled-mode: pass `open` one-way and route close through onOpenChange.
  Two-way `bind:open={$store.field}` through cmdk-sv (Svelte 4 compat) into
  bits-ui v2 ($bindable) is unreliable on rapid open/close — the writeback
  to the runes-mode store-field expression doesn't always settle before the
  next event cycle. Controlled mode keeps the store as the single source
  of truth (holomush-ceon).
-->
<Command.Dialog
  open={$uiPrefs.paletteOpen}
  label="Command palette"
  onOpenChange={(open: boolean) => {
    if (open) openPalette(); else closePalette();
  }}
>
  <!-- autofocus={true} kicks cmdk-sv's 10ms focus action; combined with
       FocusScope's rAF auto-focus this gives two paths to focus the input
       before user keystrokes can race past dialog open. -->
  <Command.Input
    name="command-palette-query"
    placeholder="Type a command…"
    autofocus={true}
  />
  <Command.List>
    <Command.Empty>No matches</Command.Empty>
    {#each items as item (item.id)}
      <Command.Item value={item.label} onSelect={() => runAndClose(item)}>
        <span class="pl-label">{item.label}</span>
        {#if item.hint}<kbd class="pl-hint">{item.hint}</kbd>{/if}
      </Command.Item>
    {/each}
  </Command.List>
</Command.Dialog>

<style>
  :global([data-cmdk-dialog]) {
    position: fixed;
    inset: 0;
    z-index: 200;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding-top: 15vh;
    background: rgba(0,0,0,0.4);
  }
  :global([data-cmdk-root]) {
    width: min(560px, 92vw);
    background: var(--color-card);
    border: 1px solid var(--color-border);
    border-radius: 8px;
    box-shadow: 0 16px 48px rgba(0,0,0,0.5);
    overflow: hidden;
    font-family: inherit;
  }
  :global([data-cmdk-input]) {
    width: 100%;
    padding: 12px 14px;
    background: transparent;
    border: none; outline: none;
    color: var(--color-input-text);
    font-family: inherit;
    font-size: 14px;
    border-bottom: 1px solid var(--color-border);
  }
  :global([data-cmdk-list]) {
    max-height: 320px;
    overflow-y: auto;
    padding: 4px;
  }
  :global([data-cmdk-item]) {
    display: flex; align-items: center; justify-content: space-between;
    gap: 8px;
    padding: 8px 10px;
    border-radius: 4px;
    color: var(--color-input-text);
    font-size: 13px;
    cursor: pointer;
  }
  :global([data-cmdk-item][data-selected="true"]) {
    background: color-mix(in srgb, var(--color-primary) 18%, transparent);
  }
  :global([data-cmdk-empty]) {
    padding: 16px;
    text-align: center;
    color: var(--color-status-text);
    font-size: 12px;
  }
  .pl-hint {
    font-family: inherit; font-size: 10px;
    padding: 1px 5px;
    border: 1px solid var(--color-border); border-radius: 3px;
    color: var(--color-status-text);
  }
</style>
