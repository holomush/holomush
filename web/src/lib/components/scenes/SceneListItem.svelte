<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { cn } from '$lib/utils.js';
  import type { WorkspaceScene } from '$lib/scenes/types';
  import { workspaceStore } from '$lib/scenes/workspaceStore.svelte';

  let {
    scene,
    onSelect,
    tabindex = 0,
  }: {
    scene: WorkspaceScene;
    onSelect?: (scene: WorkspaceScene) => void;
    tabindex?: number;
  } = $props();

  const isActive = $derived(workspaceStore.selectedSceneId === scene.sceneId);
  const unread = $derived(workspaceStore.unreadBySceneId[scene.sceneId] ?? 0);

  // Accessible name: "scene <title>, as <character>, <n> unread"
  const accessibleName = $derived(
    [
      `scene ${scene.title}`,
      scene.asCharacterName ? `as ${scene.asCharacterName}` : null,
      unread > 0 ? `${unread} unread` : null,
    ]
      .filter(Boolean)
      .join(', '),
  );
</script>

<button
  type="button"
  class={cn(
    'flex w-full items-start gap-2 rounded-md px-3 py-2 text-left transition-colors',
    isActive
      ? 'bg-accent text-accent-foreground'
      : 'hover:bg-muted/60 text-foreground',
  )}
  {tabindex}
  onclick={() => (onSelect ? onSelect(scene) : workspaceStore.select(scene.sceneId, '', scene.asCharacterId))}
  aria-current={isActive ? 'true' : undefined}
  aria-label={accessibleName}
>
  <span class="mt-0.5 size-2 shrink-0 rounded-full bg-[var(--brand-cyan-bright)] mt-1.5"></span>
  <span class="min-w-0 flex-1">
    <span class="flex items-center justify-between gap-1">
      <span class="truncate text-sm font-medium leading-snug">{scene.title}</span>
      {#if unread > 0}
        <Badge variant="default" class="shrink-0 text-[10px] h-4 min-w-4 px-1">
          {unread > 99 ? '99+' : unread}
        </Badge>
      {/if}
    </span>
    {#if scene.asCharacterName}
      <span class="text-[11px] text-muted-foreground truncate block">as {scene.asCharacterName}</span>
    {/if}
  </span>
</button>
