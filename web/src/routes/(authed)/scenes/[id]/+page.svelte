<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import type { PageData } from './$types';
  import PoseCard from '$lib/components/scenes/PoseCard.svelte';
  import { getPublicSceneArchive, downloadPublicSceneArchive, getScene, exportScene } from '$lib/scenes/client';
  import { jsonlToLogEntries } from '$lib/scenes/types';
  import { downloadBlob } from '$lib/scenes/download';
  import type { LogEntry } from '$lib/scenes/types';

  let { data }: { data: PageData } = $props();

  // Route param — could be a scene ID or a published-scene ID (publication attempt ID).
  let id = $derived($page.params.id);
  let sessionId = $derived(data.playerId ?? '');
  // Use first character's ID as "act as" selector for participant-gated RPCs.
  let characterId = $derived((data.characters?.[0] as { characterId?: string })?.characterId ?? '');

  // State for the page
  let loading = $state(true);
  let fetchError = $state('');

  // Metadata
  let title = $state('');
  let participants = $state<string[]>([]);
  let publishedAtNs = $state(0n);
  let isPublished = $state(false);
  let isParticipant = $state(false);
  // The published-scene ID (= id when loaded from archive; derived from scene when exported)
  let publishedSceneId = $state('');
  // The scene ID for exportScene (participant path)
  let sceneId = $state('');
  // Filename base for downloads
  let fileBase = $state('scene');

  let entries = $state<LogEntry[]>([]);

  let exporting = $state(false);
  let exportError = $state('');

  onMount(async () => {
    await load();
  });

  async function load() {
    loading = true;
    fetchError = '';
    entries = [];

    const resolvedId: string = id ?? '';
    const resolvedCharId: string = characterId ?? '';

    if (!resolvedId) {
      fetchError = 'Invalid scene ID.';
      loading = false;
      return;
    }

    // Strategy:
    // 1. Try webGetPublicSceneArchive(id) — works when id is a published-scene ID.
    //    This path serves the archive read page (no auth gate beyond player session).
    // 2. If that fails (not a published scene, or not found), try webGetScene(id) —
    //    works when id is a scene ID and the caller is a participant.
    // 3. If both fail, show appropriate error.

    // Try published-archive path first.
    try {
      const archive = await getPublicSceneArchive(sessionId, resolvedId);
      isPublished = true;
      publishedSceneId = resolvedId;
      title = archive.titleSnapshot;
      participants = archive.participantsSnapshot ?? [];
      publishedAtNs = archive.publishedAtUnixNs ?? 0n;
      fileBase = slugify(title || 'scene');

      // Load content as JSONL.
      const dl = await downloadPublicSceneArchive(sessionId, resolvedId, 'jsonl');
      entries = jsonlToLogEntries(dl.content);
      loading = false;
      return;
    } catch {
      // Fall through — not a published scene or not found under this ID.
    }

    // Try scene-participant export path.
    try {
      const sceneInfo = await getScene(sessionId, resolvedCharId, resolvedId);
      if (!sceneInfo) {
        fetchError = 'Scene not found.';
        loading = false;
        return;
      }
      sceneId = sceneInfo.id;
      title = sceneInfo.title;
      fileBase = slugify(title || 'scene');
      participants = [
        ...(sceneInfo.participants ?? []).map((p) => p.characterName),
        ...(sceneInfo.observers ?? []).map((p) => p.characterName),
      ];

      // Attempt export as participant (any role).
      isParticipant = true;
      const exported = await exportScene(sessionId, {
        characterId: resolvedCharId,
        sceneId: resolvedId,
        format: 'jsonl',
      });
      entries = jsonlToLogEntries(exported.content);
      loading = false;
      return;
    } catch (e) {
      // Not published and not a participant — or some other error.
    }

    fetchError = 'This scene is not available. It may be private or not published yet.';
    loading = false;
  }

  function slugify(s: string): string {
    return s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'scene';
  }

  function formatDate(ns: bigint): string {
    if (!ns) return '';
    return new Date(Number(ns / 1_000_000n)).toLocaleDateString([], {
      year: 'numeric', month: 'long', day: 'numeric',
    });
  }

  async function handleDownload(format: 'jsonl' | 'markdown') {
    exporting = true;
    exportError = '';
    try {
      const mime = format === 'jsonl' ? 'application/jsonl' : 'text/markdown';
      const ext = format === 'jsonl' ? '.jsonl' : '.md';
      const filename = fileBase + ext;

      if (isPublished) {
        const dl = await downloadPublicSceneArchive(sessionId, publishedSceneId, format);
        downloadBlob(dl.content, mime, filename);
      } else if (isParticipant) {
        const exp = await exportScene(sessionId, {
          characterId: characterId ?? '',
          sceneId,
          format,
        });
        downloadBlob(exp.content, exp.mimeType || mime, exp.filename || filename);
      }
    } catch (e) {
      exportError = e instanceof Error ? e.message : 'Export failed';
    } finally {
      exporting = false;
    }
  }
</script>

<main class="flex flex-col h-full">
  {#if loading}
    <div class="flex-1 flex items-center justify-center text-muted-foreground text-sm">
      Loading scene…
    </div>
  {:else if fetchError}
    <div class="flex-1 flex items-center justify-center text-sm px-4">
      <div class="text-center max-w-sm">
        <p class="text-muted-foreground">{fetchError}</p>
        <a href="/scenes/archive" class="text-xs text-[var(--brand-cyan-bright)] hover:underline mt-2 block">
          ← Browse archive
        </a>
      </div>
    </div>
  {:else}
    <!-- Scene header -->
    <header class="px-4 pt-4 pb-3 border-b border-border">
      <h1 class="text-lg font-semibold">{title || '(untitled)'}</h1>
      {#if participants.length > 0}
        <p class="text-xs text-muted-foreground mt-0.5">
          {participants.join(', ')}
        </p>
      {/if}
      {#if isPublished && publishedAtNs}
        <p class="text-xs text-muted-foreground">Published {formatDate(publishedAtNs)}</p>
      {/if}

      <!-- Export buttons -->
      <div class="flex gap-2 mt-2">
        <button
          onclick={() => handleDownload('jsonl')}
          disabled={exporting}
          aria-label="Download scene as JSONL"
          class="text-xs px-2.5 py-1 rounded border border-border hover:bg-accent transition-colors disabled:opacity-50"
        >
          ↓ JSONL
        </button>
        <button
          onclick={() => handleDownload('markdown')}
          disabled={exporting}
          aria-label="Download scene as Markdown"
          class="text-xs px-2.5 py-1 rounded border border-border hover:bg-accent transition-colors disabled:opacity-50"
        >
          ↓ Markdown
        </button>
        {#if exportError}
          <span class="text-xs text-destructive self-center">{exportError}</span>
        {/if}
      </div>
    </header>

    <!-- Log entries — read-only, no composer -->
    {#if entries.length === 0}
      <div class="flex-1 flex items-center justify-center text-muted-foreground text-sm">
        No content in this scene.
      </div>
    {:else}
      <div
        class="flex-1 overflow-y-auto"
        role="log"
        aria-label="Scene log"
        aria-live="off"
      >
        {#each entries as entry (entry.id)}
          <PoseCard {entry} />
        {/each}
      </div>
    {/if}
  {/if}
</main>
