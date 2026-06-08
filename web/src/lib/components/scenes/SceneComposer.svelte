<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { cn } from '$lib/utils.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import type { WorkspaceScene } from '$lib/scenes/types';
  import { sendSceneCommand } from '$lib/scenes/client';
  import { ensureSession, awaitConnectionId } from '$lib/scenes/altSessions.svelte';
  import { workspaceStore } from '$lib/scenes/workspaceStore.svelte';

  let {
    scene,
    /** Player's primary session ID (kept for signature compat; not used for refresh). */
    playerSessionId,
    /** Full character roster; passed to workspaceStore.refresh() after join. */
    characters = [],
  }: {
    scene: WorkspaceScene;
    playerSessionId: string;
    characters?: { characterId: string; characterName?: string; name?: string }[];
  } = $props();

  const isObserver = $derived(scene.role === 'observer');
  const draftKey = $derived(`scene-draft-${scene.sceneId}`);

  // Restore draft from localStorage when scene changes.
  let draftText = $state('');
  $effect(() => {
    // Re-run when scene.sceneId changes.
    const _id = scene.sceneId;
    draftText = typeof localStorage !== 'undefined'
      ? (localStorage.getItem(`scene-draft-${_id}`) ?? '')
      : '';
  });

  // Persist draft on every keystroke.
  $effect(() => {
    if (typeof localStorage !== 'undefined') {
      localStorage.setItem(draftKey, draftText);
    }
  });

  let sending = $state(false);
  let joinPending = $state(false);
  let errorMsg = $state('');

  async function resolveSessionAndConnection(): Promise<{ sessionId: string; connectionId: string }> {
    const sessionId = await ensureSession(scene.asCharacterId);
    const connectionId = await awaitConnectionId(scene.asCharacterId);
    return { sessionId, connectionId };
  }

  async function send(verb: 'pose' | 'say' | 'ooc') {
    const text = draftText.trim();
    if (!text) return;
    sending = true;
    errorMsg = '';
    try {
      const { sessionId, connectionId } = await resolveSessionAndConnection();
      await sendSceneCommand(sessionId, connectionId, `scene ${verb} ${text}`);
      // Clear draft on success.
      draftText = '';
      if (typeof localStorage !== 'undefined') {
        localStorage.removeItem(draftKey);
      }
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : 'Send failed';
    } finally {
      sending = false;
    }
  }

  async function joinScene() {
    joinPending = true;
    errorMsg = '';
    try {
      const { sessionId, connectionId } = await resolveSessionAndConnection();
      await sendSceneCommand(sessionId, connectionId, `scene join #${scene.sceneId}`);
      // Refresh workspace so role updates from observer→member. Fan out across alts.
      await workspaceStore.refresh(characters);
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : 'Join failed';
    } finally {
      joinPending = false;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
      e.preventDefault();
      send('pose');
    }
  }
</script>

<div class="flex flex-col gap-2 border-t border-border px-4 py-3">
  {#if isObserver}
    <!-- Observer: show Join CTA, composer disabled -->
    <div class="flex flex-col items-center gap-2 py-2">
      <p class="text-sm text-muted-foreground text-center">
        You are watching as <span class="font-medium">{scene.asCharacterName}</span>.
      </p>
      <Button
        variant="default"
        onclick={joinScene}
        disabled={joinPending}
        aria-label="Join scene as {scene.asCharacterName}"
      >
        {joinPending ? 'Joining…' : 'Join scene'}
      </Button>
    </div>
  {:else}
    <!-- Active participant: composer enabled -->
    <div class="flex items-center gap-2 mb-1">
      <span class="text-[11px] text-muted-foreground">posing as</span>
      <span
        class="inline-flex items-center rounded-full border border-[var(--brand-cyan-bright)]/40 px-2 py-0.5 text-[11px] font-medium text-[var(--brand-cyan-bright)]"
      >
        {scene.asCharacterName}
      </span>
    </div>
    <label for="scene-composer-{scene.sceneId}" class="sr-only">Compose a pose, say, or OOC</label>
    <textarea
      id="scene-composer-{scene.sceneId}"
      name="scene-composer"
      class={cn(
        'w-full min-h-[80px] resize-y rounded-md border border-input bg-background px-3 py-2',
        'text-sm text-foreground placeholder:text-muted-foreground',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50',
        'disabled:opacity-50 disabled:cursor-not-allowed',
      )}
      placeholder="Write your pose…"
      bind:value={draftText}
      disabled={sending}
      onkeydown={handleKeydown}
      rows={3}
    ></textarea>
    <div class="flex items-center gap-2">
      <Button
        variant="default"
        size="sm"
        onclick={() => send('pose')}
        disabled={sending || !draftText.trim()}
        aria-label="Send pose"
      >
        Pose
      </Button>
      <Button
        variant="outline"
        size="sm"
        onclick={() => send('say')}
        disabled={sending || !draftText.trim()}
        aria-label="Send say"
      >
        Say
      </Button>
      <Button
        variant="ghost"
        size="sm"
        onclick={() => send('ooc')}
        disabled={sending || !draftText.trim()}
        aria-label="Send OOC"
      >
        OOC
      </Button>
      <span class="ml-auto text-[10px] text-muted-foreground hidden sm:inline">⌘↵ to pose</span>
    </div>
  {/if}
  {#if errorMsg}
    <p class="text-xs text-destructive">{errorMsg}</p>
  {/if}
</div>
