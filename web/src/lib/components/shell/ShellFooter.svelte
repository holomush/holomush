<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { footerContent } from '$lib/stores/footerBridge';
  import { connectionStatus } from '$lib/stores/connectionStore';
  import { activeSectionLabel } from '$lib/nav/sections';

  interface Props {
    /** Current route path; selects the baseline section label. */
    pathname: string;
  }
  let { pathname }: Props = $props();

  let label = $derived(activeSectionLabel(pathname) ?? 'Workspace');
</script>

<div class="shell-footer" data-testid="shell-footer">
  {#if $footerContent}
    {@render $footerContent()}
  {:else}
    <span class="sf-section">{label}</span>
    <span class="sf-grow"></span>
    <span class="sf-hint"><kbd>⌘K</kbd> go to…</span>
    <span class="sf-conn" data-status={$connectionStatus} title="Connection">
      <span class="sf-dot" aria-hidden="true"></span>
      {#if $connectionStatus === 'connected'}connected{:else if $connectionStatus === 'syncing'}syncing{:else}offline{/if}
    </span>
  {/if}
</div>

<style>
  .shell-footer {
    flex-shrink: 0;
    min-height: 26px;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 0 12px;
    background: var(--color-background);
    border-top: 1px solid var(--color-border);
    color: var(--color-status-text);
    font-size: 12px;
  }
  .sf-grow { flex: 1; }
  .sf-hint kbd {
    font-family: inherit;
    padding: 1px 5px;
    border: 1px solid var(--color-border);
    border-radius: 3px;
    font-size: 11px;
  }
  .sf-conn { display: inline-flex; align-items: center; gap: 5px; font-size: 11px; }
  .sf-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--color-status-text); }
  .sf-conn[data-status='connected'] .sf-dot { background: var(--color-status-online); }
  .sf-conn[data-status='syncing'] .sf-dot { background: var(--color-accent); }
  .sf-conn[data-status='disconnected'] .sf-dot { background: var(--color-status-offline); }
</style>
