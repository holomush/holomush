<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { Badge } from '$lib/components/ui/badge';
  import { Button } from '$lib/components/ui/button/index.js';
  import { goto } from '$app/navigation';
  import { watchScene } from '$lib/scenes/client';
  import type { SceneInfo } from '$lib/connect/holomush/scene/v1/scene_pb';

  let { scene, sessionId, characterId, lastActivityMs = 0n }: {
    scene: SceneInfo;
    sessionId: string;
    /** Acting character for Watch; empty string when no alt session is active. */
    characterId: string;
    /** Epoch-ms of most-recent IC activity; 0 when unavailable. */
    lastActivityMs?: bigint;
  } = $props();

  /** Relative time string from epoch-ms. Falls back to empty string. */
  function relativeTime(ms: bigint): string {
    if (!ms) return '';
    const diffMs = Date.now() - Number(ms);
    const diffMin = Math.floor(diffMs / 60_000);
    if (diffMin < 2) return 'just now';
    if (diffMin < 60) return `${diffMin}m ago`;
    const diffH = Math.floor(diffMin / 60);
    if (diffH < 24) return `${diffH}h ago`;
    return `${Math.floor(diffH / 24)}d ago`;
  }

  /** Returns a coloured dot class for the scene state. */
  function stateColor(state: string): string {
    if (state === 'active') return 'bg-green-500';
    if (state === 'paused') return 'bg-yellow-400';
    return 'bg-muted-foreground'; // ended, archived
  }

  let watching = $state(false);
  let error = $state('');

  async function handleWatch() {
    if (!characterId) {
      // No active alt — navigate to workspace where the user can pick an alt.
      goto(`/scenes?watch=${scene.id}`);
      return;
    }
    watching = true;
    error = '';
    try {
      await watchScene(sessionId, { characterId, sceneId: scene.id });
      goto(`/scenes?watch=${scene.id}`);
    } catch (e) {
      error = e instanceof Error ? e.message : 'Watch failed';
    } finally {
      watching = false;
    }
  }

  function handleJoin() {
    // Join navigates to the workspace where the composer's Join CTA handles
    // character selection and the scene join command. We do not issue
    // sendSceneCommand here because on the board the viewer may not yet
    // have an alt session for this scene.
    goto(`/scenes?join=${scene.id}`);
  }
</script>

<article
  class="flex items-start gap-3 px-4 py-3 border-b border-border hover:bg-muted/20 transition-colors"
>
  <!-- Status dot -->
  <span
    class="mt-1.5 size-2.5 shrink-0 rounded-full {stateColor(scene.state)}"
    aria-label="Status: {scene.state}"
  ></span>

  <!-- Main content -->
  <div class="min-w-0 flex-1">
    <div class="flex items-center gap-2 flex-wrap">
      <a
        href="/scenes/{scene.id}"
        class="font-semibold text-sm text-foreground hover:underline truncate max-w-xs"
      >
        {scene.title || '(untitled)'}
      </a>
      {#each scene.tags as tag (tag)}
        <Badge variant="secondary" class="text-[10px] py-0 px-1.5">{tag}</Badge>
      {/each}
    </div>

    <div class="flex items-center gap-3 mt-0.5 text-xs text-muted-foreground flex-wrap">
      {#if scene.locationId}
        <span>{scene.locationId}</span>
      {/if}
      {#if scene.participants?.length}
        <span>{scene.participants.length} here</span>
      {/if}
      {#if lastActivityMs}
        <span>{relativeTime(lastActivityMs)}</span>
      {/if}
    </div>

    {#if error}
      <p class="text-xs text-destructive mt-1">{error}</p>
    {/if}
  </div>

  <!-- Actions -->
  <div class="flex gap-2 shrink-0">
    {#if scene.visibility === 'open' && (scene.state === 'active' || scene.state === 'paused')}
      <Button
        variant="outline"
        size="sm"
        class="h-6 text-xs"
        onclick={handleWatch}
        disabled={watching}
        aria-label="Watch scene {scene.title}"
      >
        {watching ? 'Watching…' : 'Watch'}
      </Button>
      <Button
        variant="default"
        size="sm"
        class="h-6 text-xs"
        onclick={handleJoin}
        aria-label="Join scene {scene.title}"
      >
        Join
      </Button>
    {:else}
      <Button href="/scenes/{scene.id}" variant="outline" size="sm" class="h-6 text-xs">
        View
      </Button>
    {/if}
  </div>
</article>
