<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import '../app.css';
  import TopBar from '$lib/components/TopBar.svelte';
  import { initTelemetry, startNavigationSpan, endNavigationSpan } from '$lib/telemetry';
  import { initSentry } from '$lib/sentry';
  import { restoreSession } from '$lib/stores/authStore';
  import { activeTheme, themeToCssVars } from '$lib/stores/themeStore';
  import {
    uiPrefs,
    hydrateUiPrefs,
    toggleRail,
    toggleSidebar,
    toggleScenesList,
    toggleScenesRail,
    toggleComposer,
    togglePalette,
  } from '$lib/stores/uiPrefsStore';
  import { clearLines } from '$lib/stores/terminalStore';
  import Composer from '$lib/components/terminal/Composer.svelte';
  import CommandPalette from '$lib/components/terminal/CommandPalette.svelte';
  import {
    composerDraft,
    setComposerDraft,
    invokeComposerSubmit,
  } from '$lib/stores/composerBridge';
  import { beforeNavigate, afterNavigate } from '$app/navigation';
  import { onMount } from 'svelte';

  let { children } = $props();

  function handleGlobalKey(e: KeyboardEvent) {
    // IME composition guard — MUST be first. CJK/Japanese/Korean input uses
    // composition events; treating every keystroke as a shortcut would eat
    // in-progress text.
    if (e.isComposing || e.keyCode === 229) return;

    const mod = e.metaKey || e.ctrlKey;
    if (!mod && e.key !== 'Escape') return;

    // Palette
    if (mod && e.key === 'k' && !e.shiftKey) {
      e.preventDefault();
      e.stopPropagation();
      togglePalette();
      return;
    }
    // Rail
    if (mod && e.key === 'b' && !e.shiftKey) {
      e.preventDefault();
      e.stopPropagation();
      toggleRail();
      return;
    }
    // Sidebar
    if (mod && e.key === '.' && !e.shiftKey) {
      e.preventDefault();
      e.stopPropagation();
      toggleSidebar();
      return;
    }
    // Scenes left list (⌘⇧, → "<"). e.code is layout-/shift-char-independent.
    if (mod && e.shiftKey && e.code === 'Comma') {
      e.preventDefault();
      e.stopPropagation();
      toggleScenesList();
      return;
    }
    // Scenes right context rail (⌘⇧. → ">").
    if (mod && e.shiftKey && e.code === 'Period') {
      e.preventDefault();
      e.stopPropagation();
      toggleScenesRail();
      return;
    }
    // Composer
    if (mod && e.shiftKey && (e.key === 'E' || e.key === 'e')) {
      e.preventDefault();
      e.stopPropagation();
      toggleComposer();
      return;
    }
    // Clear terminal
    if (mod && e.key === 'l' && !e.shiftKey) {
      e.preventDefault();
      e.stopPropagation();
      clearLines();
      return;
    }
    // Esc: no-op at this level — palette is handled by bits-ui Dialog, composer by
    // its own window listener (both installed with capture:true and fire
    // before this handler), and CommandInput's local Esc clears its draft.
  }

  onMount(() => {
    initTelemetry();
    initSentry();
    restoreSession();
    hydrateUiPrefs();
    window.addEventListener('keydown', handleGlobalKey, { capture: true });
    return () => window.removeEventListener('keydown', handleGlobalKey, { capture: true });
  });

  beforeNavigate(({ to }) => {
    startNavigationSpan(to?.url.pathname ?? 'unknown');
  });

  afterNavigate(() => {
    endNavigationSpan();
  });
</script>

<div
  class="app-root"
  data-density={$uiPrefs.density}
  style={themeToCssVars($activeTheme.colors)}
>
  <TopBar />
  <main>{@render children()}</main>
  <Composer
    draft={$composerDraft}
    ondraftChange={setComposerDraft}
    onsubmit={invokeComposerSubmit}
  />
  <CommandPalette />
</div>

<style>
  .app-root {
    display: flex;
    flex-direction: column;
    min-height: 100vh;
    background: var(--color-background);
    color: var(--color-input-text);
  }
  main {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
</style>
