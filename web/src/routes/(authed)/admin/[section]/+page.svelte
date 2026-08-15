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
{:else}
  <!--
    LOUD, NOT BLANK. An `available` entry reaching the parameterised route means
    no concrete route claimed it, which is a routing bug rather than a state the
    operator caused. The load has already resolved the entry and answered 200,
    so without this branch the frame, the rail and the breadcrumb all render
    around an entirely empty content column — no copy, no error, nothing to
    attribute. Today `characters` is the only available id and its own route
    shadows this one; the registry's own comment anticipates a second flipping
    as an ordinary PR.

    No action: there is nowhere to send them.
  -->
  <Empty.Root class="planned" data-section-state="unhandled">
    <Empty.Media variant="icon" class="planned-media"><CircleDashed size={16} /></Empty.Media>
    <Empty.Title class="planned-name">{data.entry.displayName}</Empty.Title>
    <Empty.Description class="planned-line">This section is not available here.</Empty.Description>
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
