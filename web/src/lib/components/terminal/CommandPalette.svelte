<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { Command, Dialog } from 'bits-ui';
  import {
    uiPrefs,
    openPalette,
    closePalette,
    toggleRail,
    toggleSidebar,
    toggleScenesList,
    toggleScenesRail,
    toggleComposer,
    toggleDensity,
  } from '$lib/stores/uiPrefsStore';
  import { clearLines } from '$lib/stores/terminalStore';
  import {
    themePreferences,
    setTheme,
    setTerminalBlackBackground,
  } from '$lib/stores/themeStore';
  import { clearAuth, authState } from '$lib/stores/authStore';
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { goto } from '$app/navigation';
  import { sectionNavEntries } from '$lib/nav/sections';

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

  // Guest sessions don't get registered-player-only go-to entries (e.g. Scenes),
  // mirroring the Rail (same registry gate, ADR holomush-stds8). This MUST be
  // reactive: the palette mounts in the persistent root layout before route
  // loads resolve the session, so a one-shot snapshot would freeze the pre-auth
  // isGuest=false and leave "Go to Scenes" in a guest's palette (holomush-5rh.23).
  const navItems: PaletteItem[] = $derived(
    sectionNavEntries({ isGuest: $authState.isGuest }).map((e) => ({
      id: e.id,
      label: e.label,
      run: () => goto(e.href),
    })),
  );

  const items: PaletteItem[] = $derived([
    ...navItems,
    { id: 'theme.default-dark',   label: 'Switch theme: Default Dark',   run: () => setTheme('default-dark') },
    { id: 'theme.default-light',  label: 'Switch theme: Default Light',  run: () => setTheme('default-light') },
    { id: 'theme.warm-dark',      label: 'Switch theme: Warm Dark',      run: () => setTheme('warm-dark') },
    { id: 'theme.warm-light',     label: 'Switch theme: Warm Light',     run: () => setTheme('warm-light') },
    { id: 'ui.rail',              label: 'Toggle rail',                  hint: '⌘B',  run: toggleRail },
    { id: 'ui.sidebar',           label: 'Toggle sidebar',               hint: '⌘.',  run: toggleSidebar },
    { id: 'ui.scenes-list',       label: 'Toggle scene list',            hint: '⌘⇧,', run: toggleScenesList },
    { id: 'ui.scenes-rail',       label: 'Toggle scene context',         hint: '⌘⇧.', run: toggleScenesRail },
    { id: 'ui.composer',          label: 'Toggle composer',              hint: '⌘⇧E', run: toggleComposer },
    { id: 'ui.density',           label: 'Toggle density (cozy/compact)',               run: toggleDensity },
    { id: 'ui.term-black',        label: 'Toggle black terminal background',            run: () => setTerminalBlackBackground(!$themePreferences.terminalBlackBackground) },
    { id: 'term.clear',           label: 'Clear terminal',               hint: '⌘L',  run: clearLines },
    { id: 'auth.sign-out',        label: 'Sign out',                                   run: signOut },
  ]);

  function runAndClose(item: PaletteItem) {
    item.run();
    closePalette();
  }
</script>

<!--
  Controlled-mode: pass `open` one-way and route close through onOpenChange.
  Bind-through to a store-field expression is unreliable on rapid open/close
  in runes mode — the writeback doesn't always settle before the next event
  cycle. Controlled mode keeps the store as the single source of truth
  (holomush-ceon).
-->
<Dialog.Root
  open={$uiPrefs.paletteOpen}
  onOpenChange={(open: boolean) => {
    if (open) openPalette(); else closePalette();
  }}
>
  <Dialog.Portal>
    <Dialog.Overlay class="pl-overlay" />
    <Dialog.Content class="pl-content">
      <Dialog.Title class="sr-only">Command palette</Dialog.Title>
      <Dialog.Description class="sr-only">
        Search and run client commands.
      </Dialog.Description>
      <Command.Root label="Command palette">
        <Command.Input
          name="command-palette-query"
          placeholder="Type a command…"
        />
        <Command.List>
          <Command.Viewport>
            <Command.Empty>No matches</Command.Empty>
            {#each items as item (item.id)}
              <Command.Item value={item.label} onSelect={() => runAndClose(item)}>
                <span class="pl-label">{item.label}</span>
                {#if item.hint}<kbd class="pl-hint">{item.hint}</kbd>{/if}
              </Command.Item>
            {/each}
          </Command.Viewport>
        </Command.List>
      </Command.Root>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<style>
  :global(.pl-overlay) {
    position: fixed;
    inset: 0;
    z-index: 200;
    background: rgba(0,0,0,0.4);
  }
  :global(.pl-content) {
    position: fixed;
    top: 15vh;
    left: 50%;
    transform: translateX(-50%);
    z-index: 201;
    width: min(560px, 92vw);
    background: var(--color-card);
    border: 1px solid var(--color-border);
    border-radius: 8px;
    box-shadow: 0 16px 48px rgba(0,0,0,0.5);
    overflow: hidden;
    font-family: inherit;
    outline: none;
  }
  :global([data-command-root]) {
    display: flex;
    flex-direction: column;
  }
  :global([data-command-input]) {
    width: 100%;
    padding: 12px 14px;
    background: transparent;
    border: none; outline: none;
    color: var(--color-input-text);
    font-family: inherit;
    font-size: 14px;
    border-bottom: 1px solid var(--color-border);
  }
  :global([data-command-list]) {
    max-height: 320px;
    overflow-y: auto;
    padding: 4px;
  }
  :global([data-command-item]) {
    display: flex; align-items: center; justify-content: space-between;
    gap: 8px;
    padding: 8px 10px;
    border-radius: 4px;
    color: var(--color-input-text);
    font-size: 13px;
    cursor: pointer;
  }
  :global([data-command-item][data-selected]) {
    background: color-mix(in srgb, var(--color-primary) 18%, transparent);
  }
  :global([data-command-empty]) {
    padding: 16px;
    text-align: center;
    color: var(--color-status-text);
    font-size: 12px;
  }
  :global(.sr-only) {
    position: absolute;
    width: 1px; height: 1px;
    padding: 0; margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }
  .pl-hint {
    font-family: inherit; font-size: 10px;
    padding: 1px 5px;
    border: 1px solid var(--color-border); border-radius: 3px;
    color: var(--color-status-text);
  }
</style>
