<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import type { PageData } from './$types';
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { get } from 'svelte/store';
  import { ScrollArea } from '$lib/components/ui/scroll-area/index.js';
  import { workspaceStore } from '$lib/scenes/workspaceStore.svelte';
  import SceneListItem from '$lib/components/scenes/SceneListItem.svelte';
  import PoseCard from '$lib/components/scenes/PoseCard.svelte';
  import PoseOrderStrip from '$lib/components/scenes/PoseOrderStrip.svelte';
  import SceneComposer from '$lib/components/scenes/SceneComposer.svelte';
  import SceneContextRail from '$lib/components/scenes/SceneContextRail.svelte';

  let { data }: { data: PageData } = $props();

  // The layout (scenes/+layout.ts) returns { playerId, characters } from webCheckSession.
  // characters carries { characterId, characterName } (CharacterSummary from web_pb).
  // refresh() fans out ListMyScenes across all alts using per-alt ensureSession sessionIds.
  // playerSessionId is kept for SceneComposer's refresh-after-join call signature.
  const playerSessionId = $derived(data.playerId ?? '');
  const characters = $derived(data.characters ?? []);

  // The selected scene from the store.
  const selectedScene = $derived(
    [...workspaceStore.myScenes, ...workspaceStore.watching].find(
      (s) => s.sceneId === workspaceStore.selectedSceneId,
    ) ?? null,
  );

  // Log entries for the currently selected scene.
  const logs = $derived(
    workspaceStore.selectedSceneId
      ? (workspaceStore.logsBySceneId[workspaceStore.selectedSceneId] ?? [])
      : [],
  );

  // Separate my-scenes and watching lists.
  const myActiveScenes = $derived(
    workspaceStore.myScenes.filter((s) => s.role !== 'observer'),
  );
  const watchingScenes = $derived(workspaceStore.watching);

  let logViewport: HTMLElement | null = $state(null);

  // Auto-scroll to bottom when new entries arrive.
  $effect(() => {
    // Touch logs.length to re-run on new entries.
    const _len = logs.length;
    if (logViewport) {
      // Schedule after DOM paint.
      queueMicrotask(() => {
        if (logViewport) {
          logViewport.scrollTop = logViewport.scrollHeight;
        }
      });
    }
  });

  onMount(async () => {
    // Seed the workspace by fanning out across all owned alts.
    await workspaceStore.refresh(characters);

    // Consume ?watch=<id> or ?join=<id> — select that scene so the user lands
    // in it immediately after the board's Watch/Join navigation.
    // Both params select the scene; for ?join the composer's Join CTA handles
    // the actual `scene join` command once the scene is selected.
    const params = get(page).url.searchParams;
    const targetId = params.get('watch') ?? params.get('join');
    if (targetId) {
      // Find the scene in the refreshed store to get its asCharacterId.
      const allScenes = [...workspaceStore.myScenes, ...workspaceStore.watching];
      const target = allScenes.find((s) => s.sceneId === targetId);
      const actingCharId = target?.asCharacterId ?? (data.characters ?? [])[0]?.characterId ?? '';
      workspaceStore.select(targetId, playerSessionId, actingCharId);
      // Clear the query param so a refresh doesn't re-select.
      goto('/scenes', { replaceState: true });
    }
  });
</script>

<div class="flex h-full overflow-hidden" data-testid="scenes-workspace">
  <!-- Left pane: scene lists (260px) -->
  <nav
    class="w-[260px] shrink-0 flex flex-col border-r border-border bg-card overflow-y-auto"
    aria-label="Scene lists"
  >
    <div class="p-3 pb-1">
      <h1 class="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
        My Scenes
        {#if myActiveScenes.length > 0}
          <span class="ml-1 text-muted-foreground/60">({myActiveScenes.length})</span>
        {/if}
      </h1>
    </div>
    <ul class="flex flex-col gap-0.5 px-2 pb-2" role="listbox" aria-label="My scenes">
      {#each myActiveScenes as scene (scene.sceneId)}
        <li role="option" aria-selected={workspaceStore.selectedSceneId === scene.sceneId}>
          <SceneListItem
            {scene}
            onSelect={(s) => workspaceStore.select(s.sceneId, playerSessionId, s.asCharacterId)}
          />
        </li>
      {:else}
        <li class="px-3 py-2 text-sm text-muted-foreground italic">No active scenes</li>
      {/each}
    </ul>

    {#if watchingScenes.length > 0}
      <div class="px-3 pt-2 pb-1 border-t border-border/50">
        <h2 class="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
          Watching
          <span class="ml-1 text-muted-foreground/60">({watchingScenes.length})</span>
        </h2>
      </div>
      <ul class="flex flex-col gap-0.5 px-2 pb-2" role="listbox" aria-label="Watching scenes">
        {#each watchingScenes as scene (scene.sceneId)}
          <li role="option" aria-selected={workspaceStore.selectedSceneId === scene.sceneId}>
            <SceneListItem
              {scene}
              onSelect={(s) => workspaceStore.select(s.sceneId, playerSessionId, s.asCharacterId)}
            />
          </li>
        {/each}
      </ul>
    {/if}

    <!-- Footer actions -->
    <div class="mt-auto border-t border-border/50 p-3">
      <a href="/scenes/browse" class="text-xs text-muted-foreground hover:text-foreground transition-colors">
        + browse · ⌕ archive
      </a>
    </div>
  </nav>

  <!-- Center pane: log + composer -->
  <main class="flex-1 min-w-0 flex flex-col overflow-hidden">
    {#if selectedScene}
      <!-- Scene title bar -->
      <header class="flex items-center gap-3 px-4 py-2 border-b border-border bg-card/50 shrink-0">
        <span class="font-semibold text-sm truncate">{selectedScene.title}</span>
        <span class="text-[11px] text-muted-foreground shrink-0">● {selectedScene.state}</span>
      </header>

      <!-- Log: ARIA live region (spec §5.4 + Task 16 a11y requirement) -->
      <div class="flex-1 min-h-0 overflow-hidden">
        <ScrollArea
          class="h-full"
          bind:viewportRef={logViewport}
        >
          <div
            role="log"
            aria-live="polite"
            aria-label="scene log"
            aria-atomic="false"
            class="py-2"
          >
            {#if logs.length === 0}
              <p class="px-4 py-8 text-center text-sm text-muted-foreground italic">
                No events yet. Start posing!
              </p>
            {:else}
              {#each logs as entry (entry.id)}
                <PoseCard {entry} />
              {/each}
            {/if}
          </div>
        </ScrollArea>
      </div>

      <!-- Pose order strip -->
      <PoseOrderStrip
        scene={selectedScene}
        actingCharacterId={selectedScene.asCharacterId}
      />

      <!-- Composer -->
      <SceneComposer
        scene={selectedScene}
        {playerSessionId}
        {characters}
      />
    {:else}
      <!-- Empty state -->
      <div class="flex-1 flex items-center justify-center">
        <div class="text-center space-y-2">
          <p class="text-muted-foreground">Select a scene from the list to begin</p>
          <a href="/scenes/browse" class="text-sm text-primary hover:underline">
            Browse open scenes →
          </a>
        </div>
      </div>
    {/if}
  </main>

  <!-- Right pane: context rail (300px) -->
  <div class="w-[300px] shrink-0">
    <SceneContextRail scene={selectedScene} />
  </div>
</div>
