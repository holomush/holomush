<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<!--
  /scenes index — the live workspace center column. The persistent chrome
  (scene-list sidebar, context rail, mobile header/sheets) lives in
  scenes/+layout.svelte's ScenesShell; this page renders only the selected
  scene's log + composer, sharing selection state through the module-singleton
  workspaceStore (holomush-5rh.30).
-->
<script lang="ts">
  import type { PageData } from './$types';
  import { ScrollArea } from '$lib/components/ui/scroll-area/index.js';
  import { workspaceStore } from '$lib/scenes/workspaceStore.svelte';
  import { sceneStateDotClass } from '$lib/scenes/stateStyle';
  import { openCreateScene } from '$lib/scenes/createSceneBridge';
  import PoseCard from '$lib/components/scenes/PoseCard.svelte';
  import PoseOrderStrip from '$lib/components/scenes/PoseOrderStrip.svelte';
  import SceneComposer from '$lib/components/scenes/SceneComposer.svelte';
  import { cn } from '$lib/utils';

  let { data }: { data: PageData } = $props();

  const playerSessionId = $derived(data.playerId ?? '');
  const characters = $derived(data.characters ?? []);

  const selectedScene = $derived(
    [...workspaceStore.myScenes, ...workspaceStore.watching].find(
      (s) => s.sceneId === workspaceStore.selectedSceneId,
    ) ?? null,
  );

  const logs = $derived(
    workspaceStore.selectedSceneId
      ? (workspaceStore.logsBySceneId[workspaceStore.selectedSceneId] ?? [])
      : [],
  );

  let logViewport: HTMLElement | null = $state(null);
  // The role="log" element — tabindex=-1 so we can .focus() it on scene select.
  let logRegion: HTMLElement | null = $state(null);

  // Auto-scroll to bottom when new entries arrive.
  $effect(() => {
    const _len = logs.length;
    if (logViewport) {
      queueMicrotask(() => {
        if (logViewport) {
          logViewport.scrollTop = logViewport.scrollHeight;
        }
      });
    }
  });

  // When a scene is selected by the user, focus the log region so a screen
  // reader lands on content. prevSelectedId starts null, so the first reactive
  // run (initial selection / page mount) sets the baseline without stealing
  // focus; only subsequent switches move focus.
  let prevSelectedId = $state<string | null>(null);
  $effect(() => {
    const id = workspaceStore.selectedSceneId;
    if (id && id !== prevSelectedId && prevSelectedId !== null) {
      queueMicrotask(() => {
        logRegion?.focus();
      });
    }
    prevSelectedId = id ?? null;
  });
</script>

<main class="flex h-full w-full min-w-0 flex-col overflow-hidden">
  {#if selectedScene}
    <!-- Scene title bar -->
    <header class="flex items-center gap-3 px-4 py-2 border-b border-border bg-card/50 shrink-0">
      <span class="font-semibold text-sm truncate">{selectedScene.title}</span>
      <span class="flex items-center gap-1.5 text-xs text-muted-foreground shrink-0">
        <span
          class={cn('size-2 shrink-0 rounded-full', sceneStateDotClass(selectedScene.state))}
          aria-hidden="true"
        ></span>
        {selectedScene.state}
      </span>
    </header>

    <!-- Log: ARIA live region (spec §5.4 + Task 16 a11y requirement) -->
    <div class="flex-1 min-h-0 overflow-hidden">
      <ScrollArea
        class="h-full"
        bind:viewportRef={logViewport}
      >
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <!-- tabindex=-1 is intentional: allows programmatic focus on scene switch -->
        <div
          bind:this={logRegion}
          role="log"
          aria-live="polite"
          aria-label="scene log"
          aria-atomic="false"
          tabindex={-1}
          class="py-2 outline-none"
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
      <div class="text-center space-y-3">
        <p class="text-muted-foreground">Select a scene from the list to begin</p>
        <button
          type="button"
          aria-label="New scene"
          onclick={openCreateScene}
          class="inline-flex items-center gap-1 rounded-md bg-primary px-3 py-1.5 text-sm font-semibold text-primary-foreground hover:opacity-90"
        >
          + New scene
        </button>
        <p>
          <a href="/scenes/browse" class="text-sm text-primary hover:underline">
            Browse open scenes →
          </a>
        </p>
      </div>
    </div>
  {/if}
</main>
