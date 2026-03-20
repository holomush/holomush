<script lang="ts">
  import { location } from '$lib/stores/sidebarStore';
</script>

{#if $location}
  <div class="room-info">
    <div class="room-name">{$location.name}</div>
    <div class="room-desc">{$location.description}</div>
  </div>
{/if}

<style>
  .room-info { margin-bottom: 12px; }
  .room-name { color: var(--color-system); font-weight: bold; margin-bottom: 4px; }
  .room-desc { color: var(--color-status-text); font-size: 10px; line-height: 1.4; }
</style>
