<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import TopBar from '$lib/components/TopBar.svelte';
  import { initTelemetry } from '$lib/telemetry';
  import { restoreSession } from '$lib/stores/authStore';
  import { beforeNavigate, afterNavigate } from '$app/navigation';
  import { trace } from '@opentelemetry/api';
  import { onMount } from 'svelte';
  import type { Span } from '@opentelemetry/api';

  let { children } = $props();
  const tracer = trace.getTracer('holomush-web');
  let navSpan: Span | null = null;

  onMount(() => {
    initTelemetry();
    restoreSession();
  });

  beforeNavigate(({ to }) => {
    navSpan = tracer.startSpan('navigation', {
      attributes: { 'navigation.to': to?.url.pathname ?? 'unknown' },
    });
  });

  afterNavigate(() => {
    navSpan?.end();
    navSpan = null;
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
