<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  /*
   * RED-PHASE PLACEHOLDER — the shipped roster card, lifted verbatim.
   *
   * This is the implementation the repository actively invites: the solid
   * `bg-primary` plate and the two-way `hasActiveSession ? Active : Offline`
   * branch that `/characters/+page.svelte` ships today, moved into a component
   * and given the new props. It is deliberately WRONG about the lifecycle: it
   * has no switch, so a retired character wears a session word.
   */
  let {
    id,
    name,
    session = undefined,
    isDefault = false,
    savingDefault = false,
    onselect = undefined,
    onmakedefault = undefined,
  }: {
    id: string;
    name: string;
    status: string;
    session?: { hasActiveSession: boolean; sessionStatus: string };
    isDefault?: boolean;
    playable?: boolean;
    savingDefault?: boolean;
    onselect?: (id: string) => void;
    onmakedefault?: (id: string) => void;
  } = $props();
</script>

<div
  class="card"
  role="button"
  tabindex="0"
  data-testid="roster-card"
  onclick={() => onselect?.(id)}
  onkeydown={(e: KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') onselect?.(id);
  }}
>
  <div class="w-11 h-11 bg-primary rounded-md">{name.charAt(0)}</div>
  <div class="body">
    <span class="name" data-testid="char-name">{name}</span>
    {#if session?.hasActiveSession}
      <span class="badge">Active</span>
    {:else}
      <span class="badge">Offline</span>
    {/if}
    {#if isDefault}
      <span class="badge" data-testid="default-badge">Default</span>
    {:else}
      <button
        type="button"
        class="make-default"
        data-testid="make-default"
        disabled={savingDefault}
        aria-busy={savingDefault}
        onclick={(e: MouseEvent) => {
          e.stopPropagation();
          onmakedefault?.(id);
        }}>Make default</button
      >
    {/if}
  </div>
</div>

<style>
  .card {
    display: flex;
    gap: 12px;
    padding: 16px;
    border: 1px solid var(--color-border);
    border-radius: 12px;
    background: var(--color-card);
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  .name {
    font-size: 14px;
    font-weight: 600;
  }
  .badge {
    font-size: 12px;
  }
</style>
