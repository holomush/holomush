<script lang="ts">
  import { presence } from '$lib/stores/sidebarStore';
</script>

{#if $presence.length > 0}
  <div class="presence-list">
    <div class="section-title">Present</div>
    {#each $presence as char}
      <div class="character" class:idle={char.idle}>
        &bull; {char.name}
        {#if char.idle}<span class="idle-tag">idle</span>{/if}
      </div>
    {/each}
  </div>
{/if}

<style>
  .section-title { color: var(--color-input-prompt); font-weight: bold; margin-bottom: 4px; font-size: 11px; }
  .character { padding: 1px 0; }
  .idle { opacity: 0.6; }
  .idle-tag { font-size: 9px; color: var(--color-status-text); }
</style>
