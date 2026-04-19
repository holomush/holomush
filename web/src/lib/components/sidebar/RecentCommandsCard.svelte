<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { commandHistory } from '$lib/stores/commandHistoryStore';

  interface Props {
    onInject: (cmd: string) => void;
  }
  let { onInject }: Props = $props();

  const MAX_SHOWN = 8;
  let recent = $derived($commandHistory.entries.slice(-MAX_SHOWN).reverse());
</script>

{#if recent.length > 0}
  <section class="card" data-testid="recent-card">
    <header class="card-head">
      <h3>Recent</h3>
    </header>
    <ul class="rows">
      {#each recent as cmd}
        <li>
          <button class="recent-btn" onclick={() => onInject(cmd)} title="Inject into input">
            <code>{cmd}</code>
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
  .card-head h3 { font-size: 11px; font-weight: 600; color: var(--color-input-prompt); letter-spacing: 1px; text-transform: uppercase; margin: 0 0 6px; }
  .rows { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 1px; }
  .recent-btn {
    display: block; width: 100%;
    background: none; border: none; border-radius: 3px;
    padding: var(--row-py, 3px) 4px;
    text-align: left; cursor: pointer;
  }
  .recent-btn:hover { background: color-mix(in srgb, var(--color-primary) 8%, transparent); }
  .recent-btn code {
    font-family: 'JetBrains Mono', monospace;
    font-size: 11px;
    color: var(--color-input-text);
  }
</style>
