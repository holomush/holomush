<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import TopBar from '$lib/components/TopBar.svelte';
  import { initTelemetry, startNavigationSpan, endNavigationSpan } from '$lib/telemetry';
  import { restoreSession } from '$lib/stores/authStore';
  import { beforeNavigate, afterNavigate } from '$app/navigation';
  import { onMount } from 'svelte';

  let { children } = $props();

  onMount(() => {
    initTelemetry();
    restoreSession();
  });

  beforeNavigate(({ to }) => {
    startNavigationSpan(to?.url.pathname ?? 'unknown');
  });

  afterNavigate(() => {
    endNavigationSpan();
  });
</script>

<TopBar />
<main>{@render children()}</main>

<style>
  main {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
</style>
