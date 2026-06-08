<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import type { PageData } from './$types';
  import { Badge } from '$lib/components/ui/badge';
  import { listPublishedScenes } from '$lib/scenes/client';
  import type { PublicSceneArchive } from '$lib/connect/holomush/scene/v1/scene_pb';

  let { data }: { data: PageData } = $props();
  let sessionId = $derived(data.playerId ?? '');

  let archives = $state<PublicSceneArchive[]>([]);
  let loading = $state(true);
  let fetchError = $state('');

  function formatDate(unixNs: bigint): string {
    if (!unixNs) return '';
    return new Date(Number(unixNs / 1_000_000n)).toLocaleDateString([], {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
    });
  }

  onMount(async () => {
    try {
      archives = await listPublishedScenes(sessionId);
    } catch (e) {
      fetchError = e instanceof Error ? e.message : 'Failed to load archive';
    } finally {
      loading = false;
    }
  });
</script>

<main class="flex flex-col h-full">
  <header class="px-4 pt-4 pb-3">
    <h1 class="text-lg font-semibold">Scene Archive</h1>
    <p class="text-xs text-muted-foreground">Published scenes available to read and download</p>
  </header>

  {#if loading}
    <div class="flex-1 flex items-center justify-center text-muted-foreground text-sm">
      Loading archive…
    </div>
  {:else if fetchError}
    <div class="flex-1 flex items-center justify-center text-destructive text-sm px-4">
      {fetchError}
    </div>
  {:else if archives.length === 0}
    <div class="flex-1 flex items-center justify-center text-muted-foreground text-sm">
      No published scenes yet.
    </div>
  {:else}
    <ul class="flex-1 overflow-y-auto" aria-label="Published scene archive">
      {#each archives as archive (archive.id)}
        <li class="flex items-start gap-3 px-4 py-3 border-b border-border hover:bg-muted/20 transition-colors">
          <div class="min-w-0 flex-1">
            <a
              href="/scenes/{archive.id}"
              class="font-semibold text-sm text-foreground hover:underline"
            >
              {archive.titleSnapshot || '(untitled)'}
            </a>
            <div class="flex flex-wrap items-center gap-2 mt-0.5 text-xs text-muted-foreground">
              {#if archive.participantsSnapshot?.length}
                <span>{archive.participantsSnapshot.join(', ')}</span>
              {/if}
              {#if archive.publishedAtUnixNs}
                <span>{formatDate(archive.publishedAtUnixNs)}</span>
              {/if}
              {#each (archive.tags ?? []) as tag (tag)}
                <Badge variant="secondary" class="text-[10px] py-0 px-1.5">{tag}</Badge>
              {/each}
            </div>
          </div>
          <a
            href="/scenes/{archive.id}"
            class="text-xs px-2.5 py-1 rounded border border-border hover:bg-accent transition-colors shrink-0"
            aria-label="Read {archive.titleSnapshot}"
          >
            Read
          </a>
        </li>
      {/each}
    </ul>
  {/if}
</main>
