<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { exits } from '$lib/stores/sidebarStore';

  interface Props { onExitClick: (direction: string) => void; }
  let { onExitClick }: Props = $props();
</script>

{#if $exits.length > 0}
  <section class="card" data-testid="exits-card">
    <header class="card-head">
      <h3>Exits</h3>
      <span class="count">{$exits.length}</span>
    </header>
    <ul class="exit-list">
      {#each $exits as exit}
        <li class="exit-row" class:locked={exit.locked}>
          <button
            class="exit-btn"
            onclick={() => !exit.locked && onExitClick(exit.direction)}
            disabled={exit.locked}
            aria-disabled={exit.locked}
          >
            <span class="arrow" aria-hidden="true">&rarr;</span>
            <span class="dir">{exit.direction}</span>
            <span class="loc">{exit.name}</span>
            {#if exit.locked}<span class="k" title="Locked" aria-label="Locked">&#x1f512;</span>{/if}
          </button>
        </li>
      {/each}
    </ul>
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
  .card-head { display: flex; align-items: baseline; justify-content: space-between; margin-bottom: 6px; }
  .card-head h3 { font-size: 11px; font-weight: 600; color: var(--color-input-prompt); letter-spacing: 1px; text-transform: uppercase; margin: 0; }
  .count { font-size: 10px; color: var(--color-status-text); }
  .exit-list { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 2px; }
  .exit-btn {
    display: flex; align-items: center; gap: 6px;
    width: 100%;
    padding: var(--row-py, 3px) 4px;
    background: none; border: none; border-radius: 4px;
    color: var(--mush-pose-actor);
    font-family: inherit; font-size: 12px;
    cursor: pointer; text-align: left;
  }
  .exit-btn:not(:disabled):hover {
    background: color-mix(in srgb, var(--color-primary) 8%, transparent);
  }
  .exit-row.locked .exit-btn { opacity: 0.45; cursor: not-allowed; }
  .arrow { color: var(--color-status-text); }
  .dir { color: var(--color-primary); font-weight: 600; }
  .loc { color: var(--color-input-text); flex: 1; }
  .k { font-size: 10px; }
</style>
