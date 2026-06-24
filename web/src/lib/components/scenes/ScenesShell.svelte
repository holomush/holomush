<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<!--
  ScenesShell — the persistent /scenes workspace chrome.

  Owns the three-pane desktop layout (collapsible scene-list sidebar · center ·
  collapsible context rail) plus the mobile header + Sheets, and renders the
  active scenes sub-route as its center column via {@render children()}. Lives in
  scenes/+layout.svelte so EVERY scenes route — /scenes, /scenes/browse,
  /scenes/archive, /scenes/[id] — keeps the sidebar and rail, not just the index
  (holomush-5rh.30). Selection state is shared through the module-singleton
  workspaceStore, so the index page's center reacts to selections made here.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { get } from 'svelte/store';
  import {
    Sheet,
    SheetContent,
    SheetHeader,
    SheetTitle,
    SheetDescription,
  } from '$lib/components/ui/sheet/index.js';
  import { workspaceStore } from '$lib/scenes/workspaceStore.svelte';
  import { createSceneOpen, openCreateScene } from '$lib/scenes/createSceneBridge';
  import SceneListItem from '$lib/components/scenes/SceneListItem.svelte';
  import SceneContextRail from '$lib/components/scenes/SceneContextRail.svelte';
  import CreateSceneSheet from '$lib/components/scenes/CreateSceneSheet.svelte';
  import * as Resizable from '$lib/components/ui/resizable';
  import { PanelLeftClose, PanelLeftOpen, PanelRightClose, PanelRightOpen } from '@lucide/svelte';
  import {
    uiPrefs,
    toggleScenesList,
    toggleScenesRail,
    setScenesListHidden,
    setScenesRailHidden,
  } from '$lib/stores/uiPrefsStore';
  import { isDesktop } from '$lib/hooks/mediaQuery.svelte';
  import { cn } from '$lib/utils';
  import type { PaneAPI } from 'paneforge';

  type Character = { characterId: string; name?: string; characterName?: string };

  let {
    data,
    children,
  }: {
    data: { playerId?: string; characters?: Character[] };
    children: Snippet;
  } = $props();

  const playerSessionId = $derived(data.playerId ?? '');
  const characters = $derived(data.characters ?? []);

  const selectedScene = $derived(
    [...workspaceStore.myScenes, ...workspaceStore.watching].find(
      (s) => s.sceneId === workspaceStore.selectedSceneId,
    ) ?? null,
  );

  const myActiveScenes = $derived(
    workspaceStore.myScenes.filter((s) => s.role !== 'observer'),
  );
  const watchingScenes = $derived(workspaceStore.watching);

  // Mobile sheet state.
  let sceneListSheetOpen = $state(false);
  let contextRailSheetOpen = $state(false);

  // Desktop ⇄ mobile: the three-pane Resizable layout is desktop-only. Below the
  // md breakpoint the mobile header + Sheets (shipped in q41kr) take over, so the
  // panes never mount there.
  const desktop = isDesktop();

  // paneforge imperative handles for the collapsible left/right panes.
  let leftPane = $state<PaneAPI>();
  let rightPane = $state<PaneAPI>();

  // Enable the slide transition only AFTER the first paint, so a pane that was
  // left collapsed (persisted) appears collapsed instantly instead of animating
  // shut on load. Only subsequent user toggles animate.
  let panesAnimated = $state(false);
  onMount(() => {
    let inner = 0;
    const outer = requestAnimationFrame(() => {
      inner = requestAnimationFrame(() => (panesAnimated = true));
    });
    return () => {
      cancelAnimationFrame(outer);
      cancelAnimationFrame(inner);
    };
  });

  // Per-flag deriveds: reading `$uiPrefs.x` directly inside an $effect would
  // re-run it on EVERY uiPrefs mutation (mutate() returns a fresh object each
  // call), spamming collapse()/expand(). A $derived dedupes by value so each
  // effect only re-runs when its own flag actually flips.
  const scenesListHidden = $derived($uiPrefs.scenesListHidden);
  const scenesRailHidden = $derived($uiPrefs.scenesRailHidden);

  // Drive paneforge collapse from the persisted flags. This reuses the shell's
  // collapse idiom (railHidden / sidebarHidden) so the slide and the localStorage
  // persistence match the nav rail and location sidebar. onCollapse / onExpand write
  // back idempotently so dragging a handle past the collapse threshold persists
  // too, without ping-ponging against this effect.
  $effect(() => {
    const pane = leftPane;
    if (!pane) return;
    if (scenesListHidden) pane.collapse();
    else pane.expand();
  });
  $effect(() => {
    const pane = rightPane;
    if (!pane) return;
    if (scenesRailHidden) pane.collapse();
    else pane.expand();
  });

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
  // Desktop and mobile share rovingIndex, but only one list container is mounted at a
  // time: the desktop pane renders under {#if desktop.current}, and the mobile sheet's
  // nav exists only while the sheet is open — so the other ref is null and skipped by the
  // `if (!container) continue` guard. The offsetParent !== null check is a final guard
  // against focusing an element whose subtree is not displayed (a silent no-op per the
  // HTML spec).
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

  function handleSceneSelect(sceneId: string, asCharacterId: string) {
    workspaceStore.select(sceneId, playerSessionId, asCharacterId);
    // Sync rovingIndex to the newly selected scene.
    const idx = allListedScenes.findIndex((s) => s.sceneId === sceneId);
    if (idx >= 0) rovingIndex = idx;
    // Close the scene-list sheet on mobile after selection.
    sceneListSheetOpen = false;
    // Selecting a live scene from a sub-route (browse / archive / a read view)
    // returns to the index so its log + composer render in the center column.
    if (get(page).url.pathname !== '/scenes') {
      goto('/scenes');
    }
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

  // Deep-link selection (?watch=/?join=). This shell lives in the scenes LAYOUT and
  // persists across /scenes/* navigations, so reading the params only in onMount would
  // miss in-layout client-side navigations — clicking Watch/Join on a SceneBoardRow does
  // goto('/scenes?watch=…') without remounting the shell (PR #4520 review). Handle them in
  // a reactive $effect instead, gated on `refreshed` so the acting character still resolves
  // from the populated store on the initial full load.
  let refreshed = $state(false);
  onMount(async () => {
    await workspaceStore.refresh(characters);
    refreshed = true;
  });
  $effect(() => {
    if (!refreshed) return;
    const targetId =
      $page.url.searchParams.get('watch') ?? $page.url.searchParams.get('join');
    if (!targetId) return;
    const target = [...workspaceStore.myScenes, ...workspaceStore.watching].find(
      (s) => s.sceneId === targetId,
    );
    const actingCharId = target?.asCharacterId ?? (data.characters ?? [])[0]?.characterId ?? '';
    workspaceStore.select(targetId, playerSessionId, actingCharId);
    goto('/scenes', { replaceState: true });
  });
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

  <CreateSceneSheet bind:open={$createSceneOpen} {characters} />

  <!-- Scene-list body for the desktop left pane. The mobile Sheet (above) keeps a
       separate copy with its own bind:this ref and SheetTitle/header wrapper, so
       this snippet is desktop-only rather than shared between the two. -->
  {#snippet sceneList()}
    <div class="flex items-center gap-1.5 p-2 border-b border-border/50">
      <button
        type="button"
        aria-label="New scene"
        onclick={openCreateScene}
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
  {/snippet}

  <!-- Center column: the desktop pane toggles plus the active scenes sub-route
       (rendered via children). The toggles live here — not in a page — so they
       persist across every scenes route alongside the panes they control. -->
  {#snippet centerColumn()}
    <div class="flex h-full w-full min-w-0 flex-col overflow-hidden">
      <!-- Desktop pane toggles: collapse/expand the left list and right rail. -->
      <div class="hidden md:flex items-center justify-between px-2 py-1 border-b border-border/50 shrink-0">
        <button
          type="button"
          aria-label={scenesListHidden ? 'Show scene list' : 'Hide scene list'}
          aria-expanded={!scenesListHidden}
          title={scenesListHidden ? 'Show scene list' : 'Hide scene list'}
          onclick={toggleScenesList}
          class="rounded-md p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
        >
          {#if scenesListHidden}
            <PanelLeftOpen size={16} aria-hidden="true" />
          {:else}
            <PanelLeftClose size={16} aria-hidden="true" />
          {/if}
        </button>
        <button
          type="button"
          aria-label={scenesRailHidden ? 'Show scene context' : 'Hide scene context'}
          aria-expanded={!scenesRailHidden}
          title={scenesRailHidden ? 'Show scene context' : 'Hide scene context'}
          onclick={toggleScenesRail}
          class="rounded-md p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
        >
          {#if scenesRailHidden}
            <PanelRightOpen size={16} aria-hidden="true" />
          {:else}
            <PanelRightClose size={16} aria-hidden="true" />
          {/if}
        </button>
      </div>

      <div class="flex-1 min-h-0 overflow-hidden">
        {@render children()}
      </div>
    </div>
  {/snippet}

  <!-- Desktop: resizable three-pane row whose left (scene list) and right (context
       rail) panes collapse with a slide. Below md the mobile header + Sheets above
       own the panel UX, so the panes never mount there. -->
  {#if desktop.current}
    <div class="flex-1 min-h-0">
      <Resizable.PaneGroup
        direction="horizontal"
        autoSaveId="holomush-scenes-panes"
        class={cn('scenes-panes', panesAnimated && 'panes-animated')}
      >
        <Resizable.Pane
          id="scenes-list"
          bind:this={leftPane}
          collapsible
          collapsedSize={0}
          defaultSize={20}
          minSize={12}
          maxSize={32}
          onCollapse={() => setScenesListHidden(true)}
          onExpand={() => setScenesListHidden(false)}
          class="overflow-hidden"
        >
          <nav
            bind:this={listContainerDesktop}
            class="flex h-full flex-col bg-card overflow-y-auto"
            aria-label="Scene list"
          >
            {@render sceneList()}
          </nav>
        </Resizable.Pane>

        <Resizable.Handle withHandle />

        <Resizable.Pane id="scenes-center" defaultSize={56} class="overflow-hidden">
          {@render centerColumn()}
        </Resizable.Pane>

        <Resizable.Handle withHandle />

        <!-- SceneContextRail renders its own <aside aria-label="Scene context"> -->
        <Resizable.Pane
          id="scenes-rail"
          bind:this={rightPane}
          collapsible
          collapsedSize={0}
          defaultSize={24}
          minSize={14}
          maxSize={40}
          onCollapse={() => setScenesRailHidden(true)}
          onExpand={() => setScenesRailHidden(false)}
          class="overflow-hidden"
        >
          <SceneContextRail scene={selectedScene} />
        </Resizable.Pane>
      </Resizable.PaneGroup>
    </div>
  {:else}
    <div class="flex-1 min-h-0 overflow-hidden">
      {@render centerColumn()}
    </div>
  {/if}
</div>

<style>
  /* Slide the collapsible scene panes instead of popping: paneforge sets each
     pane's flex-grow, and it cooperatively waits on the flex-grow transitionend,
     so transitioning flex-grow animates collapse/expand. Gated on .panes-animated
     (added after first paint) so a persisted-collapsed pane appears collapsed
     instantly on load instead of sliding shut. Disabled while a handle is actively
     dragged so resizing tracks the pointer 1:1. Selectors are :global because
     paneforge owns the [data-pane] / [data-pane-resizer] DOM. */
  :global(.scenes-panes.panes-animated [data-pane]) {
    transition: flex-grow 180ms ease;
  }
  :global(.scenes-panes:has([data-pane-resizer][data-active]) [data-pane]) {
    transition: none;
  }
</style>
