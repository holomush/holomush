<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { location } from '$lib/stores/sidebarStore';

  interface Props {
    characterName: string;
    connected: boolean;
    onToggleSidebar: () => void;
    onOpenSettings?: () => void;
    showHamburger?: boolean;
  }

  let { characterName, connected, onToggleSidebar, onOpenSettings, showHamburger = false }: Props = $props();
</script>

<div class="status-bar">
  <div class="left">
    <span class="brand">HoloMUSH</span>
    <span class="divider">|</span>
    <span class="character">{characterName}</span>
    {#if $location}
      <span class="loc">@ {$location.name}</span>
    {/if}
  </div>
  <div class="right">
    <span class="connection" class:connected class:disconnected={!connected}>
      {connected ? 'Connected' : 'Disconnected'}
    </span>
    {#if showHamburger}
      <button class="icon-btn" onclick={onToggleSidebar} title="Toggle sidebar">&#9776;</button>
    {/if}
    {#if onOpenSettings}
      <button class="icon-btn" onclick={onOpenSettings} title="Settings">&#9881;</button>
    {/if}
  </div>
</div>

<style>
  .status-bar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 6px 12px;
    background: var(--color-status-background);
    border-bottom: 1px solid var(--color-border);
    font-size: 11px;
  }
  .left, .right { display: flex; align-items: center; gap: 8px; }
  .brand { color: var(--color-input-prompt); }
  .divider { color: var(--color-border); }
  .character { color: var(--color-pose-actor); }
  .loc { color: var(--color-status-text); font-size: 10px; }
  .connected { color: var(--color-status-text); }
  .disconnected { color: var(--color-system); }
  .icon-btn {
    background: none;
    border: none;
    color: var(--color-status-text);
    cursor: pointer;
    font-size: 13px;
  }
</style>
