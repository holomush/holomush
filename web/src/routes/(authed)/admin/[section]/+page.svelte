<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { CircleDashed } from '@lucide/svelte';
  import * as Empty from '$lib/components/ui/empty';
  import type { AdminSectionEntry } from '$lib/stores/adminNavStore';

  // Purely presentational, and typed to exactly what it consumes rather than
  // to the whole merged route payload: the load already made every decision.
  let { data }: { data: { entry: AdminSectionEntry } } = $props();
</script>

{#if data.entry.status === 'planned'}
  <Empty.Root class="planned">
    <Empty.Media variant="icon" class="planned-media"><CircleDashed size={16} /></Empty.Media>
    <Empty.Title class="planned-name">{data.entry.displayName}</Empty.Title>
    <Empty.Description class="planned-line">Registered and gated. No handler yet.</Empty.Description>
  </Empty.Root>
{/if}

<style>
  /* Centring and vertical anchoring come from the primitive; the only thing
     added here is the muted colour role and the wrap behaviour.

     The heading WRAPS. A shortened name in this position is a worse failure
     than a two-line one — the name is the only thing on screen telling the
     operator which of these they opened. */
  :global(.planned) {
    border: none;
    padding: 32px;
  }
  :global(.planned-name) {
    font-size: 16px;
    color: var(--color-muted-foreground);
    overflow-wrap: anywhere;
    white-space: normal;
  }
  :global(.planned-line) {
    color: var(--color-muted-foreground);
  }
  :global(.planned-media) {
    background: transparent;
    color: var(--color-muted-foreground);
  }
</style>
