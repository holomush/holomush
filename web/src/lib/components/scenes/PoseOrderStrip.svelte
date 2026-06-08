<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import type { WorkspaceScene } from '$lib/scenes/types';

  /**
   * PoseOrderStrip renders the horizontal participant strip beneath the log.
   *
   * Pose order data is not yet exposed on WorkspaceScene (the dedicated
   * PoseOrder RPC is a follow-up bead). This component renders the known
   * acting participants (role != 'observer') as an ordered placeholder strip
   * using the scene data already in the workspace store.
   *
   * Deviation: rendered as a roster strip, not a strict pose-order sequence,
   * because no pose-order surface is available from the data layer at Task 16.
   */
  let {
    scene,
    /** Optional: character ID acting in this scene (for "you" highlight). */
    actingCharacterId = '',
  }: {
    scene: WorkspaceScene;
    actingCharacterId?: string;
  } = $props();

  // Derive a participant name list from the enriched scene data.
  // Uses scene.participants (populated by select() → getScene) when available;
  // falls back to the acting alt as placeholder until .8.25 lands.
  const participantNames = $derived(() => {
    if (scene.participants && scene.participants.length > 0) {
      return scene.participants.map((p) => p.name);
    }
    if (scene.asCharacterName) return [scene.asCharacterName];
    return [];
  });
</script>

{#if participantNames().length > 0}
  <div
    class="flex items-center gap-1.5 px-4 py-1.5 border-t border-border/50 bg-muted/20 overflow-x-auto"
    aria-label="Pose order"
  >
    <span class="text-[10px] text-muted-foreground shrink-0 mr-1">pose order:</span>
    {#each participantNames() as name, i (name)}
      <span
        class="inline-flex items-center gap-1 rounded-full border border-border px-2 py-0.5 text-[11px]"
        class:border-[var(--brand-cyan-bright)]={name === scene.asCharacterName && actingCharacterId !== ''}
        class:text-[var(--brand-cyan-bright)]={name === scene.asCharacterName && actingCharacterId !== ''}
      >
        {name}
        {#if name === scene.asCharacterName && actingCharacterId !== ''}
          <span class="text-[9px] text-muted-foreground">(you)</span>
        {/if}
      </span>
      {#if i < participantNames().length - 1}
        <span class="text-muted-foreground/50 text-xs" aria-hidden="true">→</span>
      {/if}
    {/each}
  </div>
{/if}
