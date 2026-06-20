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
  import {
    Sheet,
    SheetContent,
    SheetHeader,
    SheetTitle,
    SheetDescription,
  } from '$lib/components/ui/sheet/index.js';
  import { workspaceStore } from '$lib/scenes/workspaceStore.svelte';
  import SceneListItem from '$lib/components/scenes/SceneListItem.svelte';
  import PoseCard from '$lib/components/scenes/PoseCard.svelte';
  import PoseOrderStrip from '$lib/components/scenes/PoseOrderStrip.svelte';
  import SceneComposer from '$lib/components/scenes/SceneComposer.svelte';
  import SceneContextRail from '$lib/components/scenes/SceneContextRail.svelte';
  import CreateSceneSheet from '$lib/components/scenes/CreateSceneSheet.svelte';

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

  const myActiveScenes = $derived(
    workspaceStore.myScenes.filter((s) => s.role !== 'observer'),
  );
  const watchingScenes = $derived(workspaceStore.watching);

  let logViewport: HTMLElement | null = $state(null);
  // The role="log" element — tabindex=-1 so we can .focus() it on scene select.
  let logRegion: HTMLElement | null = $state(null);

  // Mobile sheet state.
  let sceneListSheetOpen = $state(false);
  let contextRailSheetOpen = $state(false);
  let createSheetOpen = $state(false);

  // Roving tabindex state: index of the item that holds tabindex=0.
  let rovingIndex = $state(0);

  // Container ref used to query focusable roving items by data-roving-index.
  // Both the desktop pane and the mobile sheet share the same rovingIndex state;
  // whichever container is visible will have the matching element in DOM.
  let listContainerDesktop: HTMLElement | null = $state(null);
  let listContainerMobile: HTMLElement | null = $state(null);

  // All scenes across both lists for roving tabindex navigation.
  const allListedScenes = $derived([...myActiveScenes, ...watchingScenes]);

  // Move DOM focus to the option button at newIndex after reactive tabindex updates settle.
  // Each option is a <div data-roving-index={n}> wrapping a <button> (SceneListItem).
  // We query BOTH containers but only focus the first one whose button is VISIBLE.
  // The desktop container is hidden md:flex (display:none below md) yet stays in the DOM;
  // focusing a display:none element is a silent no-op per the HTML spec, so we must skip it.
  // offsetParent === null is the standard cheap check for display:none subtrees.
  function focusRovingItem(newIndex: number) {
    queueMicrotask(() => {
      for (const container of [listContainerDesktop, listContainerMobile]) {
        if (!container) continue;
        const wrapper = container.querySelector<HTMLElement>(`[data-roving-index="${newIndex}"]`);
        if (!wrapper) continue;
        const btn = wrapper.querySelector<HTMLElement>('button') ?? wrapper;
        if (btn.offsetParent !== null) {
          btn.focus();
          return;
        }
      }
    });
  }

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

  // When a scene is selected by the user, focus the log region.
  let prevSelectedId = $state<string | null>(null);
  $effect(() => {
    const id = workspaceStore.selectedSceneId;
    if (id && id !== prevSelectedId && prevSelectedId !== null) {
      // User switched scenes — move focus to log so screen-reader lands on content.
      queueMicrotask(() => {
        logRegion?.focus();
      });
    }
    prevSelectedId = id ?? null;
  });

  function handleSceneSelect(sceneId: string, asCharacterId: string) {
    workspaceStore.select(sceneId, playerSessionId, asCharacterId);
    // Sync rovingIndex to the newly selected scene.
    const idx = allListedScenes.findIndex((s) => s.sceneId === sceneId);
    if (idx >= 0) rovingIndex = idx;
    // Close the scene-list sheet on mobile after selection.
    sceneListSheetOpen = false;
  }

  // Roving tabindex keydown handler for the scene list container.
  // Handles both My Scenes and Watching as one continuous navigation group.
  function handleListKeydown(e: KeyboardEvent) {
    const len = allListedScenes.length;
    if (len === 0) return;
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      rovingIndex = (rovingIndex + 1) % len;
      focusRovingItem(rovingIndex);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      rovingIndex = (rovingIndex - 1 + len) % len;
      focusRovingItem(rovingIndex);
    } else if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      const scene = allListedScenes[rovingIndex];
      if (scene) handleSceneSelect(scene.sceneId, scene.asCharacterId);
    }
  }

  onMount(async () => {
    await workspaceStore.refresh(characters);

    const params = get(page).url.searchParams;
    const targetId = params.get('watch') ?? params.get('join');
    if (targetId) {
      const allScenes = [...workspaceStore.myScenes, ...workspaceStore.watching];
      const target = allScenes.find((s) => s.sceneId === targetId);
      const actingCharId = target?.asCharacterId ?? (data.characters ?? [])[0]?.characterId ?? '';
      // Mark as initial selection so focus doesn't fire.
      prevSelectedId = targetId;
      workspaceStore.select(targetId, playerSessionId, actingCharId);
      goto('/scenes', { replaceState: true });
    } else {
      // Set prevSelectedId to current so first $effect run doesn't fire focus.
      prevSelectedId = workspaceStore.selectedSceneId ?? null;
    }
  });

  // Shared scene list markup used in both the sidebar and the mobile sheet.
  // We pass whether we're inside the sheet to decide whether to close it.
</script>

<div class="flex flex-col h-full overflow-hidden" data-testid="scenes-workspace">

  <!-- Mobile header bar (below md:) -->
  <div class="md:hidden flex items-center gap-2 px-3 py-2 border-b border-border bg-card shrink-0 z-10">
    <button
      type="button"
      aria-label="Open scene list"
      class="flex items-center gap-1.5 rounded-md px-2 py-1.5 text-sm text-muted-foreground hover:bg-muted transition-colors"
      onclick={() => (sceneListSheetOpen = true)}
    >
      <!-- Hamburger icon -->
      <svg aria-hidden="true" width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
        <rect y="3" width="16" height="1.5" rx="0.75"/>
        <rect y="7.25" width="16" height="1.5" rx="0.75"/>
        <rect y="11.5" width="16" height="1.5" rx="0.75"/>
      </svg>
      <span class="text-xs font-medium truncate max-w-[120px]">
        {selectedScene ? selectedScene.title : 'Scenes'}
      </span>
    </button>

    <span class="flex-1"></span>

    <button
      type="button"
      aria-label="Open scene context"
      class="flex items-center gap-1 rounded-md px-2 py-1.5 text-sm text-muted-foreground hover:bg-muted transition-colors"
      onclick={() => (contextRailSheetOpen = true)}
    >
      <!-- Info icon -->
      <svg aria-hidden="true" width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
        <circle cx="8" cy="8" r="7" fill="none" stroke="currentColor" stroke-width="1.5"/>
        <rect x="7.25" y="7" width="1.5" height="5" rx="0.75"/>
        <circle cx="8" cy="4.5" r="0.875"/>
      </svg>
      <span class="text-xs">Info</span>
    </button>
  </div>

  <!-- Mobile sheet: scene list (slides from left) -->
  <Sheet bind:open={sceneListSheetOpen}>
    <SheetContent side="left" class="p-0 flex flex-col md:hidden">
      <SheetHeader class="px-4 pt-4 pb-2">
        <SheetTitle>My Scenes</SheetTitle>
        <SheetDescription class="sr-only">Browse and select your active scenes</SheetDescription>
      </SheetHeader>
      <!-- bind:this on the nav so focusRovingItem can query items when the sheet is open.
           Both desktop and mobile share rovingIndex; only the visible container has
           matching elements in the DOM at any given breakpoint. -->
      <nav bind:this={listContainerMobile} aria-label="Scene list" class="flex-1 flex flex-col overflow-y-auto">
        <div
          role="listbox"
          aria-label="My scenes"
          tabindex={myActiveScenes.length > 0 ? 0 : -1}
          class="flex flex-col gap-0.5 px-2 pb-2 outline-none"
          onkeydown={handleListKeydown}
        >
          {#each myActiveScenes as scene, i (scene.sceneId)}
            <div role="option" aria-selected={workspaceStore.selectedSceneId === scene.sceneId} data-roving-index={i}>
              <SceneListItem
                {scene}
                tabindex={rovingIndex === i ? 0 : -1}
                onSelect={(s) => handleSceneSelect(s.sceneId, s.asCharacterId)}
              />
            </div>
          {:else}
            <p class="px-3 py-2 text-sm text-muted-foreground italic">No active scenes</p>
          {/each}
        </div>

        {#if watchingScenes.length > 0}
          <div class="px-3 pt-2 pb-1 border-t border-border/50">
            <h2 class="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              Watching
              <span class="ml-1 text-muted-foreground/60">({watchingScenes.length})</span>
            </h2>
          </div>
          <div
            role="listbox"
            aria-label="Watching scenes"
            tabindex={-1}
            class="flex flex-col gap-0.5 px-2 pb-2 outline-none"
            onkeydown={handleListKeydown}
          >
            {#each watchingScenes as scene, i (scene.sceneId)}
              <div role="option" aria-selected={workspaceStore.selectedSceneId === scene.sceneId} data-roving-index={myActiveScenes.length + i}>
                <SceneListItem
                  {scene}
                  tabindex={rovingIndex === myActiveScenes.length + i ? 0 : -1}
                  onSelect={(s) => handleSceneSelect(s.sceneId, s.asCharacterId)}
                />
              </div>
            {/each}
          </div>
        {/if}

        <div class="mt-auto border-t border-border/50 p-3">
          <a href="/scenes/browse" class="text-xs text-muted-foreground hover:text-foreground transition-colors">
            + browse · ⌕ archive
          </a>
        </div>
      </nav>
    </SheetContent>
  </Sheet>

  <!-- Mobile sheet: context rail (slides from right) -->
  <Sheet bind:open={contextRailSheetOpen}>
    <SheetContent side="right" class="p-0 flex flex-col md:hidden">
      <SheetHeader class="px-4 pt-4 pb-2">
        <SheetTitle>Scene Context</SheetTitle>
        <SheetDescription class="sr-only">Details, roster, and activity for the selected scene</SheetDescription>
      </SheetHeader>
      <div class="flex-1 overflow-y-auto">
        <SceneContextRail scene={selectedScene} />
      </div>
    </SheetContent>
  </Sheet>

  <CreateSceneSheet bind:open={createSheetOpen} {characters} />

  <!-- Three-pane row: fills remaining height below the mobile header -->
  <div class="flex flex-1 min-h-0 overflow-hidden">

  <!-- Left pane: scene lists (desktop 260px; hidden on mobile) -->
  <nav
    bind:this={listContainerDesktop}
    class="hidden md:flex w-[260px] shrink-0 flex-col border-r border-border bg-card overflow-y-auto"
    aria-label="Scene list"
  >
    <div class="flex items-center gap-1.5 p-2 border-b border-border/50">
      <button
        type="button"
        aria-label="New scene"
        onclick={() => (createSheetOpen = true)}
        class="inline-flex items-center gap-1 rounded-md bg-primary px-2.5 py-1 text-xs font-semibold text-primary-foreground hover:opacity-90"
      >
        + New scene
      </button>
      <a href="/scenes/browse" class="rounded-md border border-border px-2.5 py-1 text-xs text-muted-foreground hover:text-foreground hover:border-primary">
        Browse
      </a>
      <a href="/scenes/browse#archive" class="rounded-md border border-border px-2.5 py-1 text-xs text-muted-foreground hover:text-foreground hover:border-primary">
        Archive
      </a>
    </div>
    <div class="p-3 pb-1">
      <h1 class="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
        My Scenes
        {#if myActiveScenes.length > 0}
          <span class="ml-1 text-muted-foreground/60">({myActiveScenes.length})</span>
        {/if}
      </h1>
    </div>
    <div
      role="listbox"
      aria-label="My scenes"
      tabindex={myActiveScenes.length > 0 ? 0 : -1}
      class="flex flex-col gap-0.5 px-2 pb-2 outline-none"
      onkeydown={handleListKeydown}
    >
      {#each myActiveScenes as scene, i (scene.sceneId)}
        <div role="option" aria-selected={workspaceStore.selectedSceneId === scene.sceneId} data-roving-index={i}>
          <SceneListItem
            {scene}
            tabindex={rovingIndex === i ? 0 : -1}
            onSelect={(s) => handleSceneSelect(s.sceneId, s.asCharacterId)}
          />
        </div>
      {:else}
        <p class="px-3 py-2 text-sm text-muted-foreground italic">No active scenes</p>
      {/each}
    </div>

    {#if watchingScenes.length > 0}
      <div class="px-3 pt-2 pb-1 border-t border-border/50">
        <h2 class="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
          Watching
          <span class="ml-1 text-muted-foreground/60">({watchingScenes.length})</span>
        </h2>
      </div>
      <div
        role="listbox"
        aria-label="Watching scenes"
        tabindex={-1}
        class="flex flex-col gap-0.5 px-2 pb-2 outline-none"
        onkeydown={handleListKeydown}
      >
        {#each watchingScenes as scene, i (scene.sceneId)}
          <div role="option" aria-selected={workspaceStore.selectedSceneId === scene.sceneId} data-roving-index={myActiveScenes.length + i}>
            <SceneListItem
              {scene}
              tabindex={rovingIndex === myActiveScenes.length + i ? 0 : -1}
              onSelect={(s) => handleSceneSelect(s.sceneId, s.asCharacterId)}
            />
          </div>
        {/each}
      </div>
    {/if}

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
            onclick={() => (createSheetOpen = true)}
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

  <!-- Right pane: context rail (desktop 300px; hidden on mobile)
       SceneContextRail renders its own <aside aria-label="Scene context"> -->
  <div class="hidden md:block w-[300px] shrink-0">
    <SceneContextRail scene={selectedScene} />
  </div>

  </div><!-- end three-pane row -->
</div>
