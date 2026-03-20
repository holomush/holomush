<script lang="ts">
  import { sidebarExpanded, toggleSidebar, location, exits, presence } from '$lib/stores/sidebarStore';
  import RoomInfo from './RoomInfo.svelte';
  import ExitList from './ExitList.svelte';
  import PresenceList from './PresenceList.svelte';

  interface Props {
    onExitClick: (direction: string) => void;
    overlay?: boolean;
  }

  let { onExitClick, overlay = false }: Props = $props();
</script>

{#if overlay && $sidebarExpanded}
  <button class="overlay-backdrop" onclick={toggleSidebar} aria-label="Close sidebar"></button>
{/if}

<aside class="sidebar" class:expanded={$sidebarExpanded} class:overlay>
  {#if $sidebarExpanded}
    <div class="sidebar-content">
      <RoomInfo />
      <ExitList {onExitClick} />
      <PresenceList />
    </div>
    <button class="toggle" onclick={toggleSidebar} title="Collapse sidebar">&#x25b6;</button>
  {:else}
    <div class="icon-strip">
      <div class="icon" title={$location?.name ?? 'Unknown'}>&#x1f4cd;</div>
      <div class="badge">{$location?.name?.slice(0, 4) ?? '\u2014'}</div>
      <div class="icon" title="{$exits.length} exits">&#x1f6aa;</div>
      <div class="badge">{$exits.length}</div>
      <div class="icon" title="{$presence.length} present">&#x1f465;</div>
      <div class="badge">{$presence.length}</div>
      <div class="spacer"></div>
      <button class="toggle" onclick={toggleSidebar} title="Expand sidebar">&#x25c0;</button>
    </div>
  {/if}
</aside>

<style>
  .sidebar {
    background: var(--color-sidebar-background);
    border-left: 1px solid var(--color-border);
    transition: width 150ms ease;
    display: flex;
    flex-direction: column;
  }
  .sidebar:not(.expanded) { width: 36px; }
  .sidebar.expanded { width: 220px; }
  .sidebar.overlay {
    position: absolute;
    right: 0;
    top: 0;
    bottom: 0;
    z-index: 10;
  }
  .overlay-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0,0,0,0.5);
    border: none;
    z-index: 9;
  }
  .sidebar-content {
    flex: 1;
    padding: 12px;
    overflow-y: auto;
    font-size: 11px;
    line-height: 1.6;
  }
  .icon-strip {
    display: flex;
    flex-direction: column;
    align-items: center;
    padding: 8px 0;
    gap: 2px;
  }
  .icon { font-size: 14px; }
  .badge { font-size: 8px; color: var(--color-status-text); margin-bottom: 4px; }
  .spacer { flex: 1; }
  .toggle {
    background: none;
    border: 1px solid var(--color-border);
    color: var(--color-status-text);
    cursor: pointer;
    border-radius: 4px;
    width: 28px;
    height: 28px;
    font-size: 11px;
    margin: 4px;
  }
</style>
