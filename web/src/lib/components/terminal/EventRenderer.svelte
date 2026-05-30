<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import CommunicationRenderer from './CommunicationRenderer.svelte';
  import MovementRenderer from './MovementRenderer.svelte';
  import CommandRenderer from './CommandRenderer.svelte';
  import SystemRenderer from './SystemRenderer.svelte';
  import FallbackRenderer from './FallbackRenderer.svelte';

  interface Props {
    event: {
      type: string;
      category: string;
      format: string;
      actor: string;
      text: string;
      metadata?: Record<string, unknown>;
    };
  }

  let { event }: Props = $props();
</script>

<div>
  {#if event.category === 'communication'}
    <CommunicationRenderer {event} />
  {:else if event.category === 'movement'}
    <MovementRenderer {event} />
  {:else if event.category === 'command'}
    <CommandRenderer {event} />
  {:else if event.category === 'system'}
    <SystemRenderer {event} />
  {:else if event.category === 'state'}
    <!-- state events route to sidebar, never rendered in scrollback -->
  {:else}
    <FallbackRenderer {event} />
  {/if}
</div>

<style>
</style>
