<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import type { PageData } from './$types';
  import SceneBoardRow from '$lib/components/scenes/SceneBoardRow.svelte';
  import TagFilter from '$lib/components/scenes/TagFilter.svelte';
  import { listScenes } from '$lib/scenes/client';
  import type { SceneInfo } from '$lib/connect/holomush/scene/v1/scene_pb';

  let { data }: { data: PageData } = $props();

  // The (authed)/scenes/+layout.ts load provides { playerId, characters }.
  // webListScenes.sessionId is the player-session ID (from the cookie's
  // X-Session-Token header that CookieMiddleware injects); we pass playerId as
  // a best-effort stand-in — the middleware authorises from the cookie, so the
  // body field is advisory.
  let sessionId = $derived(data.playerId ?? '');

  // Primary character (first in roster) used as the watcher for Watch v1.
  // The player may have multiple characters; we default to characters[0] as the
  // acting watcher. Future iterations can add a character-picker UX.
  let primaryCharId = $derived((data.characters ?? [])[0]?.characterId ?? '');

  let scenes = $state<SceneInfo[]>([]);
  let loading = $state(true);
  let fetchError = $state('');
  let titleQuery = $state('');
  let activeTags = $state<string[]>([]);

  // All unique tags from the loaded scene list for the tag filter UI.
  let availableTags = $derived(
    [...new Set(scenes.flatMap((s) => s.tags ?? []))].sort(),
  );

  // Client-side title filter applied on top of the server tag filter.
  let filteredScenes = $derived(
    titleQuery.trim()
      ? scenes.filter((s) =>
          s.title.toLowerCase().includes(titleQuery.toLowerCase()),
        )
      : scenes,
  );

  async function fetchScenes(tags: string[] = []) {
    loading = true;
    fetchError = '';
    try {
      scenes = await listScenes(sessionId, { tags, characterId: primaryCharId });
    } catch (e) {
      fetchError = e instanceof Error ? e.message : 'Failed to load scenes';
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    fetchScenes();
  });

  function handleTagsChange(tags: string[]) {
    activeTags = tags;
    fetchScenes(tags);
  }
</script>

<main class="flex flex-col h-full">
  <header class="px-4 pt-4 pb-2">
    <h1 class="text-lg font-semibold">Scene Board</h1>
    <p class="text-xs text-muted-foreground">Active and recent scenes</p>
  </header>

  <TagFilter
    bind:titleQuery
    bind:activeTags
    {availableTags}
    onTagsChange={handleTagsChange}
  />

  {#if loading}
    <div class="flex-1 flex items-center justify-center text-muted-foreground text-sm">
      Loading scenes…
    </div>
  {:else if fetchError}
    <div class="flex-1 flex items-center justify-center text-destructive text-sm px-4">
      {fetchError}
    </div>
  {:else if filteredScenes.length === 0}
    <div class="flex-1 flex items-center justify-center text-muted-foreground text-sm">
      No scenes found.
    </div>
  {:else}
    <ul class="flex-1 overflow-y-auto" aria-label="Scene list">
      {#each filteredScenes as scene (scene.id)}
        <li>
          <SceneBoardRow {scene} {sessionId} characterId={primaryCharId} lastActivityMs={scene.lastActivityMs} />
        </li>
      {/each}
    </ul>
  {/if}
</main>
