<script lang="ts">
  import { exits } from '$lib/stores/sidebarStore';

  interface Props {
    onExitClick: (direction: string) => void;
  }

  let { onExitClick }: Props = $props();
</script>

{#if $exits.length > 0}
  <div class="exit-list">
    <div class="section-title">Exits</div>
    {#each $exits as exit}
      <button class="exit" onclick={() => onExitClick(exit.direction)}>
        &rarr; <span class="exit-name">{exit.name}</span>
        {#if exit.locked}<span class="lock-icon" aria-label="locked">&#x1f512;</span>{/if}
      </button>
    {/each}
  </div>
{/if}

<style>
  .exit-list { margin-bottom: 12px; }
  .section-title { color: var(--color-input-prompt); font-weight: bold; margin-bottom: 4px; font-size: 11px; }
  .exit {
    display: block;
    background: none;
    border: none;
    color: var(--color-pose-actor);
    cursor: pointer;
    font-family: inherit;
    font-size: inherit;
    padding: 1px 0;
    text-align: left;
  }
  .exit:hover { text-decoration: underline; }
</style>
