<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { location } from '$lib/stores/sidebarStore';
</script>

{#if $location}
  <section class="card room" data-testid="room-card">
    <header class="card-head">
      <h3 class="room-name">{$location.name}</h3>
      <span class="room-id">#{$location.id.slice(0, 8)}</span>
    </header>
    <p class="room-desc">{$location.description}</p>
    {#if $location.mood}<p class="room-mood"><em>{$location.mood}</em></p>{/if}
  </section>
{/if}

<style>
  .card {
    background: var(--color-card);
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: var(--card-pad, 12px);
    margin-bottom: var(--row-gap, 8px);
  }
  .card.room {
    background: color-mix(in srgb, var(--color-primary) 8%, var(--color-card));
    border-color: color-mix(in srgb, var(--color-primary) 30%, var(--color-border));
  }
  .card-head { display: flex; align-items: baseline; justify-content: space-between; gap: 8px; margin-bottom: 6px; }
  .room-name { font-size: 14px; font-weight: 600; color: var(--color-input-text); margin: 0; }
  .room-id { font-size: 10px; color: var(--color-status-text); font-family: 'JetBrains Mono', monospace; }
  .room-desc { font-size: 11px; color: var(--color-status-text); line-height: 1.5; margin: 0; }
  .room-mood { font-size: 11px; color: var(--color-muted-foreground); margin: 6px 0 0; }
</style>
