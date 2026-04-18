<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import '../app.css';
  import TopBar from '$lib/components/TopBar.svelte';
  import { initTelemetry, startNavigationSpan, endNavigationSpan } from '$lib/telemetry';
  import { restoreSession } from '$lib/stores/authStore';
  import { activeTheme, themeToCssVars } from '$lib/stores/themeStore';
  import { uiPrefs, hydrateUiPrefs } from '$lib/stores/uiPrefsStore';
  import Composer from '$lib/components/terminal/Composer.svelte';
  import {
    composerDraft,
    setComposerDraft,
    invokeComposerSubmit,
  } from '$lib/stores/composerBridge';
  import { beforeNavigate, afterNavigate } from '$app/navigation';
  import { onMount } from 'svelte';

  let { children } = $props();

  onMount(() => {
    initTelemetry();
    restoreSession();
    hydrateUiPrefs();
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
