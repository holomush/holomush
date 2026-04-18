<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { uiPrefs } from '$lib/stores/uiPrefsStore';
  import RoomInfo from './RoomInfo.svelte';
  import ExitList from './ExitList.svelte';
  import PresenceList from './PresenceList.svelte';
  import RecentCommandsCard from './RecentCommandsCard.svelte';

  interface Props {
    onExitClick: (direction: string) => void;
    onInject: (cmd: string) => void;
    resizable?: boolean;
  }

  let { onExitClick, onInject, resizable = false }: Props = $props();

  let expandedAttr = $derived(!$uiPrefs.sidebarHidden);
</script>

<aside
  class="sidebar"
  class:is-hidden={$uiPrefs.sidebarHidden}
  class:resizable
  data-testid="sidebar"
  data-expanded={expandedAttr}
  aria-label="Room sidebar"
>
  <div class="sidebar-content">
    <RoomInfo />
    <ExitList {onExitClick} />
    <PresenceList />
    <RecentCommandsCard {onInject} />
  </div>
</aside>

<style>
  .sidebar {
    height: 100%;
    background: var(--color-sidebar-background);
    border-left: 1px solid var(--color-border);
    display: flex;
    flex-direction: column;
    transition: width 180ms ease;
    overflow: hidden;
  }
  .sidebar.is-hidden { width: 0; border-left-width: 0; }
  @media (max-width: 767px) {
    .sidebar { width: 0; border-left-width: 0; }
  }
  .sidebar-content {
    flex: 1;
    padding: 8px;
    overflow-y: auto;
    font-size: 12px;
  }
</style>
